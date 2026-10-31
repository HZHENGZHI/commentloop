package server

import (
	"net/http"
	"strings"
)

func (s *Server) handleAgentRunnerClaim(w http.ResponseWriter, r *http.Request) {
	var req agentRunnerClaimRequest
	if !bindJSON(w, r, defaultJSONBodyLimit, &req) {
		return
	}
	req.RunnerID = strings.TrimSpace(req.RunnerID)
	req.AgentType = strings.TrimSpace(req.AgentType)
	if req.RunnerID == "" || req.AgentType == "" {
		writeError(w, http.StatusBadRequest, errRunnerIDAgentTypeRequired)
		return
	}
	job, ok, err := s.agentJobs.claim(req.AgentType, req.RunnerID, s.cfg.Agent.JobLease)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.agentCaps.setToken(job.Token, agentGrant{
		Capabilities: job.Capabilities,
		DocPath:      job.DocPath,
		RepoIDs:      job.RepoIDs,
		JobID:        job.ID,
	})
	s.broadcastAgent(job.DocPath, "working", "外部 Agent 正在处理评论…")
	writeJSON(w, http.StatusOK, agentClaimResponse{AgentJob: job, Token: job.Token})
}

func (s *Server) handleAgentRunnerFinish(w http.ResponseWriter, r *http.Request) {
	var req agentRunnerFinishRequest
	if !bindJSON(w, r, defaultJSONBodyLimit, &req) {
		return
	}
	job, known := s.agentJobs.get(req.ID)
	if !known {
		writeError(w, http.StatusNotFound, errJobNotFound)
		return
	}
	if err := s.agentJobs.finish(req.ID, req.RunnerID, req.Success, req.Error); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if known {
		s.agentCaps.clearToken(job.Token)
		s.broadcastAgent(job.DocPath, map[bool]string{true: "done", false: "error"}[req.Success],
			func() string {
				if req.Success {
					return "外部 Agent 已完成任务"
				}
				if req.Error != "" {
					return "外部 Agent 失败：" + req.Error
				}
				return "外部 Agent 任务失败"
			}())
		if req.Success && s.consensus != nil {
			s.consensus.schedule(job.DocPath, true)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentRunnerHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req agentRunnerHeartbeatRequest
	if !bindJSON(w, r, defaultJSONBodyLimit, &req) {
		return
	}
	if err := s.agentJobs.heartbeat(req.ID, req.RunnerID, s.cfg.Agent.JobLease); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAgentJobs(w http.ResponseWriter, _ *http.Request) {
	items, err := s.agentJobs.recent(50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": items})
}
