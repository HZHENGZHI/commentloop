package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

type requestDocReview struct {
	Path string `json:"path"`
}

func (s *Server) handleRequestDocReview(w http.ResponseWriter, r *http.Request) {
	if _, known := s.agentGrantForRequest(r); known || r.Header.Get("X-Commentloop-Agent") == "1" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "只有用户能发起全文审阅"})
		return
	}
	var req requestDocReview
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if _, _, err := s.readRootFile(ro, rel, maxDocSize); err != nil {
		if os.IsNotExist(err) || remoteFileMissing(err) {
			writeError(w, http.StatusNotFound, errFileNotFound)
		} else {
			writeErrorDetail(w, http.StatusBadRequest, errCouldNotReadFileDetail, err.Error())
		}
		return
	}
	if err := s.enqueueDocReview(canonical, promptLanguage(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if review, err := s.reviews.set(canonical, reviewStatusInReview); err == nil {
		s.hub.broadcast(outMessage{Type: "reviews", Path: canonical, Status: review.Status})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) enqueueDocReview(path, language string) error {
	settings := s.settings.get()
	agent := settings.Agent
	prompt, caps, repoIDs, err := s.buildDocReviewPrompt(path, language)
	if err != nil {
		return err
	}
	if agent.Runner == "external" {
		job, err := s.agentJobs.create(AgentJob{
			DocPath: path, AgentType: agent.Type, Prompt: prompt,
			PromptTemplateID: reviewPromptTemplateID,
			Capabilities:     caps, RepoIDs: repoIDs,
		})
		if err != nil {
			return fmt.Errorf("创建 Agent 任务失败：%w", err)
		}
		s.broadcastAgent(path, "queued", fmt.Sprintf("已创建审阅任务 %s", job.ID))
		return nil
	}
	if !strings.EqualFold(agent.Type, "pi") {
		return fmt.Errorf("当前 Agent 类型不支持嵌入审阅；可切换外部 Runner")
	}
	if strings.TrimSpace(agent.Command) == "" {
		return fmt.Errorf("Agent 启动命令为空")
	}
	session, err := s.agentSessions.ensureDefault(path)
	if err != nil {
		return fmt.Errorf("无法创建 Agent 会话：%w", err)
	}
	if s.agentRunner == nil || !s.agentRunner.enqueue(session, reviewAgentJob{
		Path: path, Prompt: prompt, Capabilities: caps, RepoIDs: repoIDs,
	}) {
		return fmt.Errorf("后台 Agent 未启动")
	}
	s.broadcastAgent(path, "queued", fmt.Sprintf("已通知 %s 开始全文审阅", agent.Name))
	return nil
}

func (s *Server) buildDocReviewPrompt(path, language string) (string, agentCapabilities, []string, error) {
	template, ok := s.prompts.get(reviewPromptTemplateID)
	if !ok {
		return "", agentCapabilities{}, nil, fmt.Errorf("审阅模板不存在")
	}
	template = localizePromptTemplate(template, language)
	consensus, _ := s.discussions.consensus(path)
	repos, _ := s.codeRepos.linked(path)
	caps := agentCapabilities{
		WriteDocs:     false,
		ReadCode:      template.AllowCodeRead,
		ReplyComments: template.AllowCommentReply,
	}
	var b strings.Builder
	b.WriteString("You are the background Code Agent for commentloop. The user asked for a full-document review. Act through the commentloop MCP server.\n\n")
	b.WriteString("Document: " + path + "\n")
	b.WriteString("Do not edit the document. Do not resolve comments; only the user can close a review.\n\n")
	if strings.TrimSpace(consensus.Content) != "" {
		b.WriteString("Document consensus (use it to avoid re-litigating settled decisions):\n")
		b.WriteString(truncatePrompt(consensus.Content, 6000))
		b.WriteString("\n\n")
	}
	if len(repos) > 0 && caps.ReadCode {
		b.WriteString("Linked code repositories (read-only):\n")
		for _, repo := range repos {
			fmt.Fprintf(&b, "- id=%s name=%s path=%s\n", repo.ID, repo.Name, repo.Path)
		}
		b.WriteString("\n")
	}
	b.WriteString("Review template: " + template.Name + "\n")
	b.WriteString(template.Body)
	b.WriteString("\n\nInstructions:\n")
	b.WriteString("1. Use commentloop MCP tools only. Do NOT run bash, find, grep, or local filesystem tools.\n")
	b.WriteString("2. Read the document with read_file, then list_comments to avoid duplicating existing threads.\n")
	if caps.ReadCode && len(repos) > 0 {
		b.WriteString("3. If linked code matters, inspect it with list_code_files/search_code/read_code. Code is read-only.\n")
	} else {
		b.WriteString("3. Do not inspect code repositories for this review.\n")
	}
	if caps.ReplyComments {
		b.WriteString("4. Create up to 8 inline comments with create_comment. Quote the shortest unique text, explain the issue concisely, and set severity to blocking, should_fix, nit, or question. Skip pure style preferences.\n")
		b.WriteString("5. After the comments, call write_review_summary with a concise summary: main issues, suggested order, and items that can be ignored. If there are no issues, say so in the summary.\n")
		b.WriteString("6. Do not call write_file or reply_comment with edits. If you cannot proceed, write a summary explaining the blocker.\n")
	} else {
		return "", agentCapabilities{}, nil, fmt.Errorf("审阅模板不允许创建评论")
	}
	return b.String(), caps, linkedRepoIDs(repos), nil
}
