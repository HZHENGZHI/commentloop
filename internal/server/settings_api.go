package server

import (
	"net/http"
	"strings"

	"github.com/hzhengzhi/commentloop/internal/lsp"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.settings.get())
}

func (s *Server) handleGetDefaultSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, defaultAppSettings())
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var input appSettings
	if !bindJSON(w, r, 256*1024, &input) {
		return
	}
	settings, err := validateSettings(input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.settingsStore.save(settings); err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errFailedSaveSettings, err.Error())
		return
	}
	s.settings.set(settings)
	s.lsp.Reset()
	writeJSON(w, http.StatusOK, settings)
}

type checkCommandRequest struct {
	Command string `json:"command"`
}

func (s *Server) handleCheckCommand(w http.ResponseWriter, r *http.Request) {
	var req checkCommandRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "error": "命令为空"})
		return
	}
	path, err := lsp.FindCommand(command)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "path": path})
}
