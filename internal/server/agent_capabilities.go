package server

import (
	"net/http"
	"strings"
	"sync"
)

// agentCapabilityStore ties write permission to the currently running
// comment task for an agent session. A prompt can ask an analysis-only task
// not to write, but the server must enforce it for requests that carry the
// managed agent session header.
type agentCapabilityStore struct {
	mu     sync.RWMutex
	byTask map[string]bool
}

func newAgentCapabilityStore() *agentCapabilityStore {
	return &agentCapabilityStore{byTask: make(map[string]bool)}
}

func (s *agentCapabilityStore) set(session string, allowDocumentEdit bool) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	s.mu.Lock()
	s.byTask[session] = allowDocumentEdit
	s.mu.Unlock()
}

func (s *agentCapabilityStore) clear(session string) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	s.mu.Lock()
	delete(s.byTask, session)
	s.mu.Unlock()
}

func (s *agentCapabilityStore) lookup(session string) (allow, known bool) {
	session = strings.TrimSpace(session)
	if session == "" {
		return true, false
	}
	s.mu.RLock()
	allow, known = s.byTask[session]
	s.mu.RUnlock()
	return
}

// rejectAgentWrite blocks document mutations during an analysis-only task.
// Requests without a managed session header (manual Agent CLI/MCP) keep the
// existing Agent permission behavior.
func (s *Server) rejectAgentWrite(w http.ResponseWriter, r *http.Request) bool {
	session := strings.TrimSpace(r.Header.Get("X-Mdlive-Agent-Session"))
	if session == "" {
		return false
	}
	allow, known := s.agentCaps.lookup(session)
	if known && !allow {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "当前任务模板只允许分析，不能修改文档；请用 reply_comment 回复分析结果",
		})
		return true
	}
	return false
}

func (s *Server) commentAllowsDocumentEdit(comment Comment) bool {
	if comment.PromptTemplateID == "" {
		template, _ := s.prompts.get(defaultPromptTemplateID)
		return template.AllowDocumentEdit
	}
	template, ok := s.prompts.get(comment.PromptTemplateID)
	if !ok {
		// A deleted custom template must fail closed, not fall back to a
		// template that permits writing.
		return false
	}
	return template.AllowDocumentEdit
}
