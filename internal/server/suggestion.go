package server

import (
	"net/http"
	"strings"

	"github.com/hzhengzhi/commentloop/internal/anchor"
)

type proposeChangeRequest struct {
	Path     string `json:"path"`
	Quote    string `json:"quote"`
	Text     string `json:"text"`
	Mode     string `json:"mode"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
}

func (s *Server) handleProposeChange(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCommentReply(w, r) {
		return
	}
	if _, known := s.agentGrantForRequest(r); !known {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "只有受管 Agent 可以提交修改建议"})
		return
	}
	var req proposeChangeRequest
	if !bindJSON(w, r, 256*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能评论关联文档") {
		return
	}
	data, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, errCouldNotReadDocumentLocate)
		return
	}
	if strings.TrimSpace(req.Quote) == "" || strings.TrimSpace(req.Text) == "" || strings.TrimSpace(req.Body) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quote、text 和 body 不能为空"})
		return
	}
	if req.Mode == "" {
		req.Mode = "replace"
	}
	created, err := s.comments.createAs("agent", canonical, req.Quote, "", "", req.Body,
		req.Severity, string(data), nil, reviewPromptTemplateID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	comment, err := s.comments.setSuggestion(canonical, created.ID, req.Quote, req.Text, req.Mode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, comment)
}

type resolveSuggestionRequest struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Accept bool   `json:"accept"`
}

func (s *Server) handleResolveSuggestion(w http.ResponseWriter, r *http.Request) {
	var req resolveSuggestionRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	s.suggestionMu.Lock()
	defer s.suggestionMu.Unlock()
	comment, ok := s.comments.get(canonical, req.ID)
	if !ok {
		writeError(w, http.StatusNotFound, errCommentNotFound)
		return
	}
	if comment.Suggestion == nil || comment.Suggestion.Status != "pending" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "这条建议已处理或不存在"})
		return
	}
	if !req.Accept {
		updated, err := s.comments.setSuggestionStatus(canonical, req.ID, "rejected")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
		writeJSON(w, http.StatusOK, updated)
		return
	}
	data, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, errCouldNotReadDocumentLocate)
		return
	}
	content := string(data)
	start, end, _, orphaned := anchor.LocateQuote(content, comment.Suggestion.Quote, comment.Prefix, comment.Suffix, comment.Line)
	if orphaned || start >= end {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "原文已变化，无法应用这条建议"})
		return
	}
	text := comment.Suggestion.Text
	if len(text) > maxDocSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "建议内容过长"})
		return
	}
	var next string
	if comment.Suggestion.Mode == "append" {
		// Put the new paragraph after the quote's paragraph. A blank line
		// already separates the next paragraph: insert before it. Otherwise
		// insert after the quote's line, keeping both sides separated so the
		// following line is not glued to the appended text.
		rest := content[end:]
		if idx := strings.Index(rest, "\n\n"); idx >= 0 {
			at := end + idx
			next = content[:at] + "\n\n" + text + content[at:]
		} else if idx := strings.IndexByte(rest, '\n'); idx >= 0 && end+idx+1 < len(content) {
			at := end + idx + 1
			next = content[:at] + "\n" + text + "\n\n" + content[at:]
		} else {
			next = strings.TrimRight(content, "\r\n") + "\n\n" + text
		}
	} else {
		next = content[:start] + text + content[end:]
	}
	if len(next) > maxDocSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "应用建议后文档过大"})
		return
	}
	if _, err := s.persistDocument(ro, rel, canonical, next, "suggestion", true); err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errFailedWriteFile, err.Error())
		return
	}
	if comment.Suggestion.Mode == "replace" {
		line := 1 + strings.Count(content[:start], "\n")
		if err := s.comments.reanchorAfterSuggestion(canonical, req.ID, text, start, start+len(text), line); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	updated, err := s.comments.setSuggestionStatus(canonical, req.ID, "accepted")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, updated)
}
