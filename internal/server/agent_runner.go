package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hzhengzhi/commentloop/internal/lsp"
)

func newSessionAgentWorker(srv *Server, session agentSession) *sessionAgentWorker {
	r := &sessionAgentWorker{
		srv:     srv,
		session: session,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

func (r *sessionAgentWorker) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.stop)
	proc := r.proc
	r.proc = nil
	r.mu.Unlock()
	if proc != nil {
		proc.close()
	}
	r.wg.Wait()
}

func (r *sessionAgentWorker) enqueue(job reviewAgentJob) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	r.queue = append(r.queue, job)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return true
}

func (r *sessionAgentWorker) loop() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stop:
			return
		case <-r.wake:
		}
		for {
			r.mu.Lock()
			if len(r.queue) == 0 || r.closed {
				r.mu.Unlock()
				break
			}
			job := r.queue[0]
			r.queue = r.queue[1:]
			r.mu.Unlock()
			r.run(job)
			select {
			case <-r.stop:
				return
			default:
			}
		}
	}
}

func (r *sessionAgentWorker) run(job reviewAgentJob) {
	settings := r.srv.settings.get()
	agent := settings.Agent
	if !strings.EqualFold(agent.Type, "pi") || strings.TrimSpace(agent.Command) == "" {
		return
	}
	command, err := lsp.FindCommand(agent.Command)
	if err != nil {
		r.srv.broadcastAgent(job.Path, "error", fmt.Sprintf("%s 启动失败：%v", agent.Name, err))
		return
	}
	args := append([]string{}, agent.Args...)
	args = removeModeArg(args)
	args = stripSessionArgs(args)
	args = append(args, "--mode", "rpc")
	if r.srv.stateDir != "" {
		sessionDir := filepath.Join(r.srv.stateDir, "pi-sessions")
		_ = os.MkdirAll(sessionDir, 0o700)
		sessionFile := filepath.Join(sessionDir, r.session.PiSessionID+".jsonl")
		args = append(args, "--session", sessionFile)
	} else {
		args = append(args, "--no-session")
	}
	signature := command + "\x00" + strings.Join(args, "\x00")
	proc, err := r.ensureProcess(signature, command, args)
	if err != nil {
		r.srv.broadcastAgent(job.Path, "error", fmt.Sprintf("%s 启动失败：%v", agent.Name, err))
		return
	}
	r.srv.agentCaps.setGrant(r.session.ID, agentGrant{
		Capabilities: job.Capabilities,
		DocPath:      job.Path,
		RepoIDs:      job.RepoIDs,
	})
	defer r.srv.agentCaps.clear(r.session.ID)
	phase := "处理评论"
	if job.CommentID == "" {
		phase = "审阅全文"
	}
	r.srv.broadcastAgent(job.Path, "working", fmt.Sprintf("%s 正在%s…", agent.Name, phase))
	progress := func(message string) {
		r.srv.broadcastAgent(job.Path, "working", message)
	}
	if err := proc.runPrompt(job.Prompt, 15*time.Minute, progress); err != nil {
		r.dropProcess(proc)
		r.srv.broadcastAgent(job.Path, "error", fmt.Sprintf("%s 处理失败：%v", agent.Name, err))
		slog.Error("pi agent failed", "err", err)
		return
	}
	if job.CommentID == "" {
		r.srv.broadcastAgent(job.Path, "done", fmt.Sprintf("%s 已完成全文审阅，等待你确认", agent.Name))
	} else {
		r.srv.broadcastAgent(job.Path, "done", fmt.Sprintf("%s 已完成一轮修改，等待你确认", agent.Name))
	}
	if r.srv.consensus != nil {
		r.srv.consensus.schedule(job.Path, true)
	}
}

func (r *sessionAgentWorker) ensureProcess(signature, command string, args []string) (*piRPC, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("Agent 已停止")
	}
	if r.proc != nil && r.procSig == signature && r.proc.alive() {
		proc := r.proc
		r.mu.Unlock()
		return proc, nil
	}
	old := r.proc
	r.proc = nil
	r.mu.Unlock()
	if old != nil {
		old.close()
	}

	proc, err := startPiRPC(command, args, r.srv.primaryPath, r.srv.agentBaseURL, r.session.ID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		proc.close()
		return nil, fmt.Errorf("Agent 已停止")
	}
	r.proc = proc
	r.procSig = signature
	return proc, nil
}

func (r *sessionAgentWorker) dropProcess(proc *piRPC) {
	r.mu.Lock()
	if r.proc == proc {
		r.proc = nil
		r.procSig = ""
	}
	r.mu.Unlock()
	proc.close()
}

func removeModeArg(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--mode" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--mode=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func newAgentRunner(srv *Server) *agentRunner {
	return &agentRunner{srv: srv, workers: make(map[string]*sessionAgentWorker)}
}

func (r *agentRunner) enqueue(session agentSession, job reviewAgentJob) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	worker := r.workers[session.ID]
	if worker == nil {
		worker = newSessionAgentWorker(r.srv, session)
		r.workers[session.ID] = worker
	}
	r.mu.Unlock()
	return worker.enqueue(job)
}

func (r *agentRunner) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	workers := make([]*sessionAgentWorker, 0, len(r.workers))
	for _, worker := range r.workers {
		workers = append(workers, worker)
	}
	r.workers = make(map[string]*sessionAgentWorker)
	r.mu.Unlock()
	for _, worker := range workers {
		worker.Close()
	}
}

func stripSessionArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-session":
			continue
		case arg == "--session" || arg == "--session-id" || arg == "--session-dir":
			i++
			continue
		case strings.HasPrefix(arg, "--session="), strings.HasPrefix(arg, "--session-id="), strings.HasPrefix(arg, "--session-dir="):
			continue
		default:
			out = append(out, arg)
		}
	}
	return out
}

func progressForTool(record piRecord) string {
	name := strings.ToLower(record.ToolName)
	if index := strings.LastIndex(name, "__"); index >= 0 {
		name = name[index+2:]
	}
	var args struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	_ = json.Unmarshal(record.Args, &args)
	file := filepath.Base(strings.TrimSpace(args.Path))
	switch {
	case strings.Contains(name, "list_comments"):
		return "正在读取评论…"
	case strings.Contains(name, "read_file"):
		if file != "" && file != "." {
			return "正在读取文档：" + file
		}
		return "正在读取文档…"
	case strings.Contains(name, "write_file"):
		if file != "" && file != "." {
			return "正在写入文档：" + file
		}
		return "正在写入文档…"
	case strings.Contains(name, "reply_comment"):
		return "正在回复评论…"
	case strings.Contains(name, "search_code"):
		if args.Query != "" {
			return "正在搜索代码：" + truncatePrompt(args.Query, 40)
		}
		return "正在搜索代码…"
	case strings.Contains(name, "read_code"):
		if file != "" && file != "." {
			return "正在读取代码：" + file
		}
		return "正在读取代码…"
	case strings.Contains(name, "list_code_files"):
		return "正在浏览代码文件…"
	case strings.Contains(name, "code_repo_info"):
		return "正在查看代码仓库…"
	case strings.Contains(name, "workspace_info"):
		return "正在读取工作区…"
	default:
		tool := record.ToolName
		if tool == "" {
			tool = "工具"
		}
		return "正在调用 " + tool + "…"
	}
}

func startPiRPC(command string, args []string, cwd, baseURL, agentSessionID string) (*piRPC, error) {
	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	// Always inherit the server environment; a nil Env would otherwise leave
	// the agent process with only the variables appended below.
	cmd.Env = os.Environ()
	if baseURL != "" {
		cmd.Env = append(cmd.Env, "COMMENTLOOP_URL="+baseURL)
	}
	if agentSessionID != "" {
		cmd.Env = append(cmd.Env, "COMMENTLOOP_AGENT_SESSION="+agentSessionID)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	p := &piRPC{
		cmd: cmd, stdin: stdin, reader: bufio.NewReaderSize(stdout, 64*1024),
		events: make(chan piRecord, 256), closed: make(chan struct{}),
	}
	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	go p.readLoop()
	return p, nil
}

func (p *piRPC) readLoop() {
	defer close(p.events)
	for {
		line, err := p.reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var record piRecord
			if json.Unmarshal(bytes.TrimSpace(line), &record) == nil {
				if record.Type == "extension_ui_request" {
					switch record.Method {
					case "select", "confirm", "input", "editor":
						_ = p.writeJSON(map[string]any{
							"type": "extension_ui_response", "id": record.ID, "cancelled": true,
						})
					}
				}
				select {
				case p.events <- record:
				case <-p.closed:
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *piRPC) writeJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err = fmt.Fprintf(p.stdin, "%s\n", data)
	return err
}

func (p *piRPC) runPrompt(prompt string, timeout time.Duration, progress func(string)) error {
	p.promptID++
	id := fmt.Sprintf("commentloop-review-%d", p.promptID)
	if err := p.writeJSON(map[string]any{
		"id": id, "type": "prompt", "message": prompt,
	}); err != nil {
		return err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	accepted := false
	settled := false
	for {
		select {
		case <-p.closed:
			return fmt.Errorf("pi RPC 进程已退出")
		case record, ok := <-p.events:
			if !ok {
				detail := strings.TrimSpace(p.stderr.String())
				if detail != "" {
					if len(detail) > 1500 {
						detail = detail[len(detail)-1500:]
					}
					return fmt.Errorf("pi RPC 已退出：%s", detail)
				}
				return fmt.Errorf("pi RPC 已退出")
			}
			switch record.Type {
			case "agent_start":
				if progress != nil {
					progress("正在分析评论…")
				}
			case "tool_execution_start":
				if progress != nil {
					progress(progressForTool(record))
				}
			case "tool_execution_end":
				if record.IsError && progress != nil {
					progress("工具执行失败，正在调整方案…")
				}
			case "message_start":
				if record.Message.Role == "assistant" && progress != nil {
					progress("正在生成修改内容…")
				}
			case "agent_end":
				if progress != nil {
					progress("正在整理修改结果…")
				}
			case "response":
				if record.ID != id {
					continue
				}
				if !record.Success {
					return fmt.Errorf("%s", record.Error)
				}
				accepted = true
				if record.Data.Disposition == "handled" {
					return nil
				}
			case "agent_settled":
				settled = true
				if accepted {
					if record.Aborted {
						return fmt.Errorf("Agent 运行被中止")
					}
					return nil
				}
			}
			if accepted && settled {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("等待 Agent 超时")
		}
	}
}

func (p *piRPC) alive() bool {
	select {
	case <-p.closed:
		return false
	default:
		return true
	}
}

func (p *piRPC) close() {
	p.closeOne.Do(func() {
		close(p.closed)
		_ = p.stdin.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		_ = p.cmd.Wait()
	})
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 4096 {
		b.data = b.data[len(b.data)-4096:]
	}
	return len(p), nil
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func (s *Server) enqueueAutoReview(path, commentID string, force bool, language string) error {
	settings := s.settings.get()
	agent := settings.Agent
	if !force && !agent.AutoRun {
		return nil
	}
	comment, ok := s.comments.get(path, commentID)
	if !ok {
		return fmt.Errorf("评论不存在")
	}
	prompt, err := s.buildReviewAgentPrompt(path, commentID, language)
	if err != nil {
		return fmt.Errorf("无法生成 Agent 任务：%w", err)
	}
	repos, _ := s.codeRepos.linked(path)
	if agent.Runner == "external" {
		job, err := s.agentJobs.create(AgentJob{
			DocPath: path, CommentID: commentID, AgentType: agent.Type,
			Prompt: prompt, PromptTemplateID: comment.PromptTemplateID,
			Capabilities: s.commentCapabilities(comment),
			RepoIDs:      linkedRepoIDs(repos),
		})
		if err != nil {
			return fmt.Errorf("创建 Agent 任务失败：%w", err)
		}
		s.broadcastAgent(path, "queued", fmt.Sprintf("已创建外部 Agent 任务 %s", job.ID))
		return nil
	}
	if !strings.EqualFold(agent.Type, "pi") {
		return fmt.Errorf("当前 Agent 类型不支持嵌入自动运行；可切换外部 Runner")
	}
	if strings.TrimSpace(agent.Command) == "" {
		return fmt.Errorf("Agent 启动命令为空")
	}
	session, err := s.agentSessions.ensureDefault(path)
	if err != nil {
		return fmt.Errorf("无法创建 Agent 会话：%w", err)
	}
	if s.agentRunner == nil || !s.agentRunner.enqueue(session, reviewAgentJob{
		Path: path, CommentID: commentID, Prompt: prompt,
		Capabilities: s.commentCapabilities(comment),
		RepoIDs:      linkedRepoIDs(repos),
	}) {
		return fmt.Errorf("后台 Agent 未启动")
	}
	s.broadcastAgent(path, "queued", fmt.Sprintf("已通知 %s 处理评论", agent.Name))
	return nil
}

func (s *Server) buildReviewAgentPrompt(path, commentID, language string) (string, error) {
	comment, ok := s.comments.get(path, commentID)
	if !ok {
		return "", fmt.Errorf("评论不存在")
	}
	groupIDs := []string{comment.ID}
	if comment.GroupID != "" {
		for _, item := range s.comments.list(path, "") {
			if item.GroupID == comment.GroupID && item.ID != comment.ID {
				groupIDs = append(groupIDs, item.ID)
			}
		}
	}
	repos, _ := s.codeRepos.linked(path)
	discussionIDs := append([]string{}, comment.DiscussionIDs...)
	if comment.GroupID != "" {
		for _, item := range s.comments.list(path, "") {
			if item.GroupID == comment.GroupID {
				discussionIDs = append(discussionIDs, item.DiscussionIDs...)
			}
		}
	}
	discussionIDs = normalizeDiscussionIDs(discussionIDs)
	consensus, hasConsensus := s.discussions.consensus(path)
	excerpts := s.discussions.excerpts(path, discussionIDs, comment.Quote, "")
	template, hasTemplate := s.prompts.get(comment.PromptTemplateID)
	if !hasTemplate {
		if comment.PromptTemplateID != "" {
			template = PromptTemplate{
				Name: "已删除的模板", AllowDocumentEdit: false,
				AllowCodeRead: true, AllowCommentReply: true,
				Body: "原任务模板已删除。请只分析当前评论并提供建议，不要修改文档。",
			}
			if language == "en" {
				template.Name = "Deleted template"
				template.Body = "The original task template was deleted. Analyze the current comment only and provide suggestions; do not modify the document."
			}
			hasTemplate = true
		} else {
			template, _ = s.prompts.get(defaultPromptTemplateID)
		}
	}
	template = localizePromptTemplate(template, language)
	allowCodeRead := !hasTemplate || template.AllowCodeRead
	allowCommentReply := !hasTemplate || template.AllowCommentReply
	templateVars := map[string]string{
		"doc_path":            path,
		"comment_quote":       truncatePrompt(comment.Quote, 1500),
		"comment_body":        truncatePrompt(lastUserMessage(comment), 2000),
		"comment_thread":      "",
		"consensus":           consensus.Content,
		"discussion_excerpts": excerpts,
		"group_comment_ids":   strings.Join(groupIDs, ", "),
		"code_repos":          "",
	}
	var threadText strings.Builder
	for _, message := range comment.Messages {
		threadText.WriteString("- " + message.Author + ": " + truncatePrompt(message.Body, 2000) + "\n")
	}
	templateVars["comment_thread"] = strings.TrimSpace(threadText.String())
	var repoText strings.Builder
	for _, repo := range repos {
		fmt.Fprintf(&repoText, "- id=%s name=%s path=%s\n", repo.ID, repo.Name, repo.Path)
	}
	if !allowCodeRead {
		repoText.Reset()
	}
	templateVars["code_repos"] = strings.TrimSpace(repoText.String())
	var b strings.Builder
	b.WriteString("You are the background Code Agent for commentloop. The user just submitted or updated a review comment. Act on it now via the commentloop MCP server.\n\n")
	b.WriteString("Document: " + path + "\n")
	b.WriteString("Comment ID: " + comment.ID + "\n")
	if len(groupIDs) > 1 {
		b.WriteString("Related group comment IDs: " + strings.Join(groupIDs, ", ") + "\n")
	}
	b.WriteString("Quote:\n" + truncatePrompt(comment.Quote, 1500) + "\n\n")
	b.WriteString("Thread:\n")
	for _, message := range comment.Messages {
		body := truncatePrompt(message.Body, 2000)
		b.WriteString("- " + message.Author + ": " + body + "\n")
	}
	if hasConsensus && strings.TrimSpace(consensus.Content) != "" {
		b.WriteString("\nDocument consensus (auto-maintained from prior discussions and edits):\n")
		b.WriteString(truncatePrompt(consensus.Content, 8000))
		b.WriteString("\n")
	}
	if excerpts != "" {
		b.WriteString("\nDiscussion excerpts explicitly selected by the user for this comment:\n")
		b.WriteString(excerpts)
		b.WriteString("\n")
	}
	if hasTemplate && strings.TrimSpace(template.Body) != "" {
		b.WriteString("\nTask template: " + template.Name + "\n")
		b.WriteString(renderPromptTemplate(template.Body, templateVars))
		b.WriteString("\n")
	}
	if allowCodeRead && len(repos) > 0 {
		b.WriteString("\nLinked code repositories:\n")
		for _, repo := range repos {
			fmt.Fprintf(&b, "- id=%s name=%s path=%s\n", repo.ID, repo.Name, repo.Path)
		}
	}
	b.WriteString("\nTask capabilities:\n")
	fmt.Fprintf(&b, "- read_docs: yes\n- read_code: %s\n- reply_comments: %s\n- write_docs: %s\n",
		yesNo(allowCodeRead), yesNo(allowCommentReply), yesNo(hasTemplate && template.AllowDocumentEdit))
	b.WriteString("\nInstructions:\n")
	b.WriteString("1. Use commentloop MCP tools only. Do NOT run bash, find, grep, or local filesystem tools to locate the document or repository. The canonical document path below is directly readable through MCP read_file.\n")
	b.WriteString("2. Read the document and the full comment thread with read_file/list_comments.\n")
	if allowCodeRead {
		b.WriteString("3. If code repositories are linked, inspect relevant code with list_code_files/search_code/read_code and consider Git state. Do not modify code.\n")
	} else {
		b.WriteString("3. Do not inspect linked code repositories for this task.\n")
	}
	if hasTemplate && !template.AllowDocumentEdit {
		b.WriteString("4. This is an analysis or diagnosis task. Do NOT call write_file and do NOT modify the document. Read the relevant document, comments, discussions, and code, then reply with your analysis.\n")
		b.WriteString("5. Reply through reply_comment. Do NOT resolve the comment; only the user can close it.\n")
	} else {
		b.WriteString("4. Make a focused document edit that addresses the latest user feedback and preserves unrelated content. Use the document consensus and selected discussion excerpts to avoid drifting from earlier decisions. If they conflict with the latest user comment, the latest user comment wins; if the conflict is material, explain it instead of guessing.\n")
		b.WriteString("5. Reply through write_file with comment_ids containing ALL related group IDs, plus a short comment. Do NOT resolve the comment; only the user can close it.\n")
	}
	if !allowCommentReply {
		b.WriteString("5b. Do not call reply_comment for this task; the task template disables comment replies.\n")
		b.WriteString("6. If you cannot proceed, stop without modifying comments. Do not ask the user interactively.\n")
	} else {
		b.WriteString("6. If you cannot proceed, reply through reply_comment explaining the blocker. Do not ask the user interactively.\n")
	}
	return b.String(), nil
}

func linkedRepoIDs(repos []codeRepoRecord) []string {
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repo.ID)
	}
	return out
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func lastUserMessage(comment Comment) string {
	for i := len(comment.Messages) - 1; i >= 0; i-- {
		if comment.Messages[i].Author == "user" {
			return comment.Messages[i].Body
		}
	}
	return ""
}

func truncatePrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if runes := []rune(value); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func (s *Server) broadcastAgent(path, status, message string) {
	s.hub.broadcast(outMessage{
		Type: "agent", Path: path, Status: status, Message: message,
	})
}
