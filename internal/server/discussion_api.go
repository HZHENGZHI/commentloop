package server

import (
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
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
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
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
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
	var req importDiscussionRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	canonical, err := s.canonicalDoc(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	s.broadcastConsensus(canonical, "queued", "正在导入会话…")
	location, err := s.resolveSessionPath(req.SessionPath)
	if err != nil {
		s.broadcastConsensus(canonical, "error", err.Error())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	parsed, err := parseAgentSession(location.Agent, location.Path)
	if err != nil {
		s.broadcastConsensus(canonical, "error", "读取会话失败："+err.Error())
		writeErrorDetail(w, http.StatusBadRequest, errFailedReadSessions, err.Error())
		return
	}
	item, err := s.discussions.importSession(canonical, parsed, location.Path, location.SourceKind, location.Agent)
	if err != nil {
		s.broadcastConsensus(canonical, "error", "导入会话失败："+err.Error())
		writeErrorDetail(w, http.StatusBadRequest, errFailedImportSession, err.Error())
		return
	}
	consensus, genErr := s.generateConsensus(r.Context(), canonical, "import", true)
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
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if err := s.discussions.delete(canonical, id); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errDiscussionSessionNotFound)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	remaining, err := s.discussions.list(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// The deleted transcript may have contributed decisions to the old
	// consensus. Clear it first, then rebuild from what remains; otherwise a
	// failed rebuild would silently keep the deleted session's influence.
	s.broadcastConsensus(canonical, "saving", "正在移除旧共识…")
	if err := s.discussions.clearConsensus(canonical); err != nil {
		s.broadcastConsensus(canonical, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(remaining) == 0 {
		s.broadcastConsensus(canonical, "done", "讨论已删除，共识已清空")
	} else {
		s.broadcastConsensus(canonical, "queued", "讨论已删除，正在从剩余讨论重建共识…")
		if s.consensus != nil {
			s.consensus.schedule(canonical, false)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshConsensus(w http.ResponseWriter, r *http.Request) {
	var req refreshConsensusRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	canonical, err := s.canonicalDoc(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	consensus, err := s.generateConsensus(r.Context(), canonical, "manual", true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, consensus)
}
