package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

type discussionListResponse struct {
	Path        string       `json:"path"`
	Consensus   *Consensus   `json:"consensus,omitempty"`
	Discussions []Discussion `json:"discussions"`
}

type availableSessionsResponse struct {
	Sessions []SessionCandidate `json:"sessions"`
	Agents   []agentInfo        `json:"agents"`
}

type importDiscussionRequest struct {
	Path        string `json:"path"`
	SessionPath string `json:"sessionPath"`
}

type importDiscussionResponse struct {
	Discussion     Discussion `json:"discussion"`
	Consensus      *Consensus `json:"consensus,omitempty"`
	ConsensusError string     `json:"consensusError,omitempty"`
}

type refreshConsensusRequest struct {
	Path string `json:"path"`
}

func (s *Server) canonicalDoc(path string) (string, error) {
	ro, rel, err := s.resolveDocRef(path)
	if err != nil {
		return "", err
	}
	return ro.ID + "/" + rel, nil
}

func (s *Server) handleDocDiscussions(w http.ResponseWriter, r *http.Request) {
	canonical, err := s.canonicalDoc(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	items, err := s.discussions.list(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := discussionListResponse{Path: canonical, Discussions: items}
	if consensus, ok := s.discussions.consensus(canonical); ok {
		resp.Consensus = &consensus
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAvailableDiscussionSessions(w http.ResponseWriter, r *http.Request) {
	canonical, err := s.canonicalDoc(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	sessions, agents, err := s.availableAgentSessions(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, availableSessionsResponse{Sessions: sessions, Agents: agents})
}

func (s *Server) handleImportDiscussion(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req importDiscussionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	canonical, err := s.canonicalDoc(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	location, err := s.resolveSessionPath(req.SessionPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	parsed, err := parseAgentSession(location.Agent, location.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "读取会话失败：" + err.Error()})
		return
	}
	item, err := s.discussions.importSession(canonical, parsed, location.Path, location.SourceKind, location.Agent)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "导入会话失败：" + err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "consensus", Path: canonical})
	consensus, genErr := s.generateConsensus(r.Context(), canonical, "import")
	resp := importDiscussionResponse{Discussion: item}
	if genErr != nil {
		resp.ConsensusError = genErr.Error()
	} else {
		resp.Consensus = &consensus
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteDiscussion(w http.ResponseWriter, r *http.Request) {
	canonical, err := s.canonicalDoc(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if err := s.discussions.delete(canonical, id); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "讨论会话不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "consensus", Path: canonical})
	if s.consensus != nil {
		s.consensus.schedule(canonical)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshConsensus(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req refreshConsensusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	canonical, err := s.canonicalDoc(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	consensus, err := s.generateConsensus(r.Context(), canonical, "manual")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, consensus)
}
