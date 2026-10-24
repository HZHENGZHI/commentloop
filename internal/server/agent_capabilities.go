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
type agentCapabilities struct {
	WriteDocs     bool
	ReadCode      bool
	ReplyComments bool
}

type agentCapabilityStore struct {
	mu     sync.RWMutex
	byTask map[string]agentCapabilities
}

func newAgentCapabilityStore() *agentCapabilityStore {
	return &agentCapabilityStore{byTask: make(map[string]agentCapabilities)}
}

func (s *agentCapabilityStore) set(session string, capabilities agentCapabilities) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	s.mu.Lock()
	s.byTask[session] = capabilities
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

func (s *agentCapabilityStore) lookup(session string) (agentCapabilities, bool) {
	session = strings.TrimSpace(session)
	if session == "" {
		return agentCapabilities{WriteDocs: true, ReadCode: true, ReplyComments: true}, false
	}
	s.mu.RLock()
	capabilities, known := s.byTask[session]
	s.mu.RUnlock()
	return capabilities, known
}

// rejectAgentWrite blocks document mutations during an analysis-only task.
// Requests without a managed session header (manual Agent CLI/MCP) keep the
// existing Agent permission behavior.
func (s *Server) rejectAgentWrite(w http.ResponseWriter, r *http.Request) bool {
	return s.rejectAgentCapability(w, r, func(c agentCapabilities) bool { return c.WriteDocs }, "当前任务模板不允许修改文档")
}

func (s *Server) rejectAgentCodeRead(w http.ResponseWriter, r *http.Request) bool {
	return s.rejectAgentCapability(w, r, func(c agentCapabilities) bool { return c.ReadCode }, "当前任务模板不允许读取代码仓库")
}

func (s *Server) rejectAgentCommentReply(w http.ResponseWriter, r *http.Request) bool {
	return s.rejectAgentCapability(w, r, func(c agentCapabilities) bool { return c.ReplyComments }, "当前任务模板不允许回复评论")
}

func (s *Server) rejectAgentCapability(w http.ResponseWriter, r *http.Request, allowed func(agentCapabilities) bool, message string) bool {
	session := strings.TrimSpace(r.Header.Get("X-Mdlive-Agent-Session"))
	if session == "" {
		return false
	}
	capabilities, known := s.agentCaps.lookup(session)
	if known && !allowed(capabilities) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
		return true
	}
	return false
}

func (s *Server) commentCapabilities(comment Comment) agentCapabilities {
	if comment.PromptTemplateID == "" {
		template, _ := s.prompts.get(defaultPromptTemplateID)
		return agentCapabilities{
			WriteDocs: template.AllowDocumentEdit, ReadCode: template.AllowCodeRead,
			ReplyComments: template.AllowCommentReply,
		}
	}
	template, ok := s.prompts.get(comment.PromptTemplateID)
	if !ok {
		// A deleted custom template fails closed for writes, but keeps the
		// ability to read code and reply so the analysis can still land.
		return agentCapabilities{WriteDocs: false, ReadCode: true, ReplyComments: true}
	}
	return agentCapabilities{
		WriteDocs: template.AllowDocumentEdit, ReadCode: template.AllowCodeRead,
		ReplyComments: template.AllowCommentReply,
	}
}
