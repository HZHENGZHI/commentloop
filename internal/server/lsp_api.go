package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/hzhengzhi/commentloop/internal/coderepo"
	"github.com/hzhengzhi/commentloop/internal/lsp"
)

type lspDefinitionRequest struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

func (s *Server) handleLSPDefinition(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req lspDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	item, ok := s.codeRepos.get(req.Repo)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "代码仓库不存在"})
		return
	}
	rel := strings.TrimSpace(req.Path)
	if rel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少代码文件路径"})
		return
	}
	content, err := coderepo.ReadFull(item.Path, rel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Line < 1 || req.Character < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "行号和列号必须从 1 开始"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	config, err := lsp.MatchServer(s.settings.get().LSPServers, rel)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	locations, err := s.lsp.Definition(ctx, item.Path, rel, content, req.Line, req.Character, config)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if locations == nil {
		locations = []lsp.Location{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": locations})
}
