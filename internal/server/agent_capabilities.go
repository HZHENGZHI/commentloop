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

type agentGrant struct {
	Capabilities agentCapabilities
	DocPath      string
	RepoIDs      []string
	JobID        string
}

type agentCapabilityStore struct {
	mu     sync.RWMutex
	byTask map[string]agentGrant
	tokens map[string]agentGrant
}

func newAgentCapabilityStore() *agentCapabilityStore {
	return &agentCapabilityStore{
		byTask: make(map[string]agentGrant),
		tokens: make(map[string]agentGrant),
	}
}

func (s *agentCapabilityStore) setToken(token string, grant agentGrant) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	s.mu.Lock()
	s.tokens[token] = grant
	s.mu.Unlock()
}

func (s *agentCapabilityStore) clearToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

func (s *agentCapabilityStore) lookupToken(token string) (agentGrant, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return agentGrant{}, false
	}
	s.mu.RLock()
	grant, ok := s.tokens[token]
	s.mu.RUnlock()
	return grant, ok
}

func (s *agentCapabilityStore) set(session string, capabilities agentCapabilities) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	s.mu.Lock()
	s.byTask[session] = agentGrant{Capabilities: capabilities}
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
	grant, known := s.byTask[session]
	s.mu.RUnlock()
	return grant.Capabilities, known
}

// rejectAgentWrite blocks document mutations during an analysis-only task.
// Requests without a managed session header (manual Agent CLI/MCP) keep the
// existing Agent permission behavior.
func (s *agentCapabilityStore) setGrant(session string, grant agentGrant) {
	session = strings.TrimSpace(session)
	if session == "" {
		return
	}
	s.mu.Lock()
	s.byTask[session] = grant
	s.mu.Unlock()
}

func (s *agentCapabilityStore) lookupGrant(session string) (agentGrant, bool) {
	session = strings.TrimSpace(session)
	if session == "" {
		return agentGrant{}, false
	}
	s.mu.RLock()
	grant, ok := s.byTask[session]
	s.mu.RUnlock()
	return grant, ok
}

func (s *Server) agentGrantForRequest(r *http.Request) (agentGrant, bool) {
	if token := strings.TrimSpace(r.Header.Get("X-Mdlive-Agent-Token")); token != "" {
		if grant, ok := s.agentCaps.lookupToken(token); ok {
			return grant, true
		}
	}
	session := strings.TrimSpace(r.Header.Get("X-Mdlive-Agent-Session"))
	if session == "" {
		return agentGrant{}, false
	}
	grant, known := s.agentCaps.lookupGrant(session)
	if !known {
		return agentGrant{}, false
	}
	return grant, true
}

func (s *Server) agentDocPathAllowed(r *http.Request, canonical string) bool {
	grant, known := s.agentGrantForRequest(r)
	if !known || grant.DocPath == "" {
		return true
	}
	return canonical == grant.DocPath
}

func (s *Server) rejectAgentDocPath(w http.ResponseWriter, r *http.Request, canonical, message string) bool {
	if s.agentDocPathAllowed(r, canonical) {
		return false
	}
	s.recordAgentDenial(r, message)
	writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
	return true
}

func (s *Server) guardAgentRepo(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.rejectAgentRepo(w, r, r.URL.Query().Get("repo")) {
			return
		}
		next(w, r)
	}
}

func (s *Server) rejectAgentRepo(w http.ResponseWriter, r *http.Request, repoID string) bool {
	grant, known := s.agentGrantForRequest(r)
	if !known || len(grant.RepoIDs) == 0 || repoID == "" {
		return false
	}
	for _, id := range grant.RepoIDs {
		if id == repoID {
			return false
		}
	}
	message := "当前任务不能访问这个代码仓库"
	s.recordAgentDenial(r, message)
	writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
	return true
}

func (s *Server) recordAgentDenial(r *http.Request, message string) {
	jobID := ""
	grant, _ := s.agentGrantForRequest(r)
	jobID = grant.JobID
	token := strings.TrimSpace(r.Header.Get("X-Mdlive-Agent-Token"))
	s.agentJobs.audit(jobID, token, r.Header.Get("X-Mdlive-MCP-Tool"), r.Method, r.URL.Path, false, message)
}

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
	grant, known := s.agentGrantForRequest(r)
	if !known {
		return false
	}
	if !allowed(grant.Capabilities) {
		s.recordAgentDenial(r, message)
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
