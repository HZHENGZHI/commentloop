package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/hzhengzhi/commentloop/internal/coderepo"
	"github.com/hzhengzhi/commentloop/internal/lsp"
)

func (s *Server) handleLSPDefinition(w http.ResponseWriter, r *http.Request) {
	var req lspDefinitionRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	item, ok := s.codeRepos.get(req.Repo)
	if !ok {
		writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
		return
	}
	rel := strings.TrimSpace(req.Path)
	if rel == "" {
		writeError(w, http.StatusBadRequest, errMissingCodeFilePath)
		return
	}
	content, err := coderepo.ReadFull(item.Path, rel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Line < 1 || req.Character < 1 {
		writeError(w, http.StatusBadRequest, errLineColumnMustStartAt)
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
