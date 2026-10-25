// Package agentrunner claims mdlive comment jobs from a local or remote
// mdlive server and runs one configured coding Agent CLI on the host.
package agentrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mdlive/internal/lsp"
)

type Job struct {
	ID         string `json:"id"`
	DocPath    string `json:"docPath"`
	CommentID  string `json:"commentId"`
	AgentType  string `json:"agentType"`
	Prompt     string `json:"prompt"`
	Token      string `json:"token"`
	Status     string `json:"status"`
	LeaseUntil string `json:"leaseUntil"`
	Error      string `json:"error"`
}

type Options struct {
	URL        string
	RunnerID   string
	AgentType  string
	Command    string
	Args       []string
	PromptMode string
	Timeout    time.Duration
	Poll       time.Duration
	Once       bool
}

func Run(args []string) error {
	fs := flag.NewFlagSet("mdlive agent-runner", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	urlFlag := fs.String("url", "", "mdlive server URL（默认 discovery / MDLIVE_URL）")
	runnerID := fs.String("id", "", "Runner ID（默认 hostname-pid）")
	agentType := fs.String("agent-type", "pi", "要领取的 Agent 类型：pi / claude-code / codex / generic")
	command := fs.String("command", "", "Agent 可执行文件（默认按 agent-type）")
	promptMode := fs.String("prompt-mode", "arg", "prompt 传递方式：arg / stdin")
	timeout := fs.Duration("timeout", 15*time.Minute, "单任务超时")
	poll := fs.Duration("poll", 2*time.Second, "队列轮询间隔")
	once := fs.Bool("once", false, "只处理一个任务后退出")
	var extraArgs stringSlice
	fs.Var(&extraArgs, "arg", "追加 Agent 参数（可重复）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(*runnerID)
	if id == "" {
		host, _ := os.Hostname()
		id = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	u := strings.TrimSpace(*urlFlag)
	if u == "" {
		u = strings.TrimSpace(os.Getenv("MDLIVE_URL"))
	}
	if u == "" {
		u = "http://127.0.0.1:4571"
	}
	opts := Options{
		URL: strings.TrimRight(u, "/"), RunnerID: id, AgentType: strings.TrimSpace(*agentType),
		Command: strings.TrimSpace(*command), Args: extraArgs,
		PromptMode: strings.TrimSpace(*promptMode), Timeout: *timeout, Poll: *poll, Once: *once,
	}
	if opts.AgentType == "" {
		return errors.New("--agent-type 不能为空")
	}
	if opts.PromptMode != "arg" && opts.PromptMode != "stdin" {
		return errors.New("--prompt-mode 必须是 arg 或 stdin")
	}
	return runLoop(opts)
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, " ") }
func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func runLoop(opts Options) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	seen := false
	for {
		job, err := claim(ctx, opts)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "mdlive agent-runner: claim: %v\n", err)
			if opts.Once {
				return err
			}
		} else if job != nil {
			seen = true
			runJob(ctx, opts, *job)
			if opts.Once {
				return nil
			}
		} else if opts.Once {
			if seen {
				return nil
			}
			return errors.New("没有可领取的任务")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(opts.Poll):
		}
	}
}

func claim(ctx context.Context, opts Options) (*Job, error) {
	body, _ := json.Marshal(map[string]string{"runnerId": opts.RunnerID, "agentType": opts.AgentType})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.URL+"/api/agent/runner/claim", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	var job Job
	if err := json.NewDecoder(res.Body).Decode(&job); err != nil {
		return nil, err
	}
	if job.Token == "" {
		return nil, errors.New("服务端没有返回任务 capability token")
	}
	return &job, nil
}

func runJob(ctx context.Context, opts Options, job Job) {
	fmt.Fprintf(os.Stderr, "mdlive agent-runner: job %s doc=%s type=%s\n", job.ID, job.DocPath, job.AgentType)
	command, args, err := agentCommand(opts)
	if err != nil {
		finish(context.Background(), opts, job, err)
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	var cmd *exec.Cmd
	if opts.PromptMode == "stdin" {
		cmd = exec.CommandContext(runCtx, command, args...)
		cmd.Stdin = strings.NewReader(job.Prompt)
	} else {
		cmd = exec.CommandContext(runCtx, command, append(args, job.Prompt)...)
	}
	cmd.Env = append(os.Environ(),
		"MDLIVE_URL="+opts.URL,
		"MDLIVE_AGENT_TOKEN="+job.Token,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	heartbeatDone := startHeartbeat(runCtx, opts, job.ID)
	err = cmd.Run()
	heartbeatDone()
	if runCtx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("Agent 执行超时")
	}
	if err != nil && stderr.Len() > 0 {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		err = fmt.Errorf("%w\n%s", err, detail)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mdlive agent-runner: job %s failed: %v\n", job.ID, err)
	} else {
		fmt.Fprintf(os.Stderr, "mdlive agent-runner: job %s done\n", job.ID)
	}
	finish(context.Background(), opts, job, err)
}

func agentCommand(opts Options) (string, []string, error) {
	command := opts.Command
	args := append([]string{}, opts.Args...)
	switch {
	case command == "" && opts.AgentType == "pi":
		command, args = "pi", append([]string{"--print", "--no-builtin-tools"}, args...)
	case command == "" && opts.AgentType == "claude-code":
		command, args = "claude", append([]string{"--print"}, args...)
	case command == "" && opts.AgentType == "codex":
		command, args = "codex", append([]string{"exec"}, args...)
	case command == "":
		return "", nil, fmt.Errorf("agent-type=%s 需要 --command", opts.AgentType)
	}
	resolved, err := lsp.FindCommand(command)
	if err != nil {
		return "", nil, err
	}
	return resolved, args, nil
}

func startHeartbeat(ctx context.Context, opts Options, jobID string) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				body, _ := json.Marshal(map[string]string{"runnerId": opts.RunnerID, "id": jobID})
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.URL+"/api/agent/runner/heartbeat", bytes.NewReader(body))
				if err != nil {
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				if res, err := http.DefaultClient.Do(req); err == nil {
					_ = res.Body.Close()
				}
			}
		}
	}()
	return cancel
}

func finish(ctx context.Context, opts Options, job Job, runErr error) {
	payload := map[string]any{"runnerId": opts.RunnerID, "id": job.ID, "success": runErr == nil}
	if runErr != nil {
		payload["error"] = runErr.Error()
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.URL+"/api/agent/runner/finish", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err == nil {
		_ = res.Body.Close()
	}
}
