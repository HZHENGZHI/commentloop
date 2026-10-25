// Package agentrunner claims commentloop comment jobs from a local or remote
// commentloop server and runs one configured coding Agent CLI on the host.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hzhengzhi/commentloop/internal/discovery"
	"github.com/hzhengzhi/commentloop/internal/lsp"
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
	fs := flag.NewFlagSet("commentloop agent-runner", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	urlFlag := fs.String("url", "", "Commentloop server URL (defaults to discovery / COMMENTLOOP_URL)")
	runnerID := fs.String("id", "", "Runner ID (defaults to hostname-pid)")
	agentType := fs.String("agent-type", "pi", "Agent type to claim: pi / claude-code / codex / cursor / generic")
	command := fs.String("command", "", "Agent executable (defaults by agent-type)")
	promptMode := fs.String("prompt-mode", "arg", "Prompt delivery: arg / stdin")
	timeout := fs.Duration("timeout", 15*time.Minute, "Per-job timeout")
	poll := fs.Duration("poll", 2*time.Second, "Queue poll interval")
	once := fs.Bool("once", false, "Process one job and exit")
	var extraArgs stringSlice
	fs.Var(&extraArgs, "arg", "Append agent arguments (repeatable)")
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
		u = strings.TrimSpace(os.Getenv("COMMENTLOOP_URL"))
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
		return errors.New("--agent-type cannot be empty")
	}
	if opts.PromptMode != "arg" && opts.PromptMode != "stdin" {
		return errors.New("--prompt-mode must be arg or stdin")
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
			fmt.Fprintf(os.Stderr, "commentloop agent-runner: claim: %v\n", err)
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
			return errors.New("No jobs available")
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
	req.Header.Set("Accept-Language", "en")
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
		return nil, errors.New("Server did not return a job capability token")
	}
	return &job, nil
}

func runJob(ctx context.Context, opts Options, job Job) {
	fmt.Fprintf(os.Stderr, "commentloop agent-runner: job %s doc=%s type=%s\n", job.ID, job.DocPath, job.AgentType)
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
	tokenPath, tokenErr := writeAgentToken(job.Token)
	if tokenErr != nil {
		finish(context.Background(), opts, job, tokenErr)
		return
	}
	defer os.Remove(tokenPath)
	urlPath, urlErr := writeAgentURL(opts.URL)
	if urlErr == nil {
		defer os.Remove(urlPath)
	}
	cmd.Env = append(os.Environ(),
		"COMMENTLOOP_URL="+opts.URL,
		"COMMENTLOOP_AGENT_TOKEN="+job.Token,
		"COMMENTLOOP_AGENT_TOKEN_FILE="+tokenPath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	heartbeatDone := startHeartbeat(runCtx, opts, job.ID)
	err = cmd.Run()
	heartbeatDone()
	if runCtx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("Agent execution timed out")
	}
	if err != nil && stderr.Len() > 0 {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		err = fmt.Errorf("%w\n%s", err, detail)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "commentloop agent-runner: job %s failed: %v\n", job.ID, err)
	} else {
		fmt.Fprintf(os.Stderr, "commentloop agent-runner: job %s done\n", job.ID)
	}
	finish(context.Background(), opts, job, err)
}

func writeAgentToken(token string) (string, error) {
	path, err := discovery.AgentTokenPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func writeAgentURL(raw string) (string, error) {
	path, err := discovery.AgentURLPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(raw+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func agentCommand(opts Options) (string, []string, error) {
	command := opts.Command
	args := append([]string{}, opts.Args...)
	var presetArgs []string
	switch opts.AgentType {
	case "pi":
		if command == "" {
			command = "pi"
		}
		presetArgs = []string{"--print", "--no-builtin-tools"}
	case "claude-code":
		if command == "" {
			command = "claude"
		}
		presetArgs = []string{"--print"}
	case "codex":
		if command == "" {
			command = "codex"
		}
		presetArgs = []string{"exec"}
	case "cursor":
		if command == "" {
			command = "cursor-agent"
		}
		// Do not pass --force: MCP tool permissions are handled by Cursor's
		// own permissions file. --approve-mcps lets the commentloop MCP server load.
		presetArgs = []string{"--print", "--approve-mcps", "--trust", "--sandbox", "enabled"}
	default:
		if command == "" {
			return "", nil, fmt.Errorf("agent-type=%s requires --command", opts.AgentType)
		}
	}
	args = append(presetArgs, args...)
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
				req.Header.Set("Accept-Language", "en")
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
	req.Header.Set("Accept-Language", "en")
	if res, err := http.DefaultClient.Do(req); err == nil {
		_ = res.Body.Close()
	}
}
