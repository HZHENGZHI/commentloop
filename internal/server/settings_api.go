package server

import (
	"encoding/json"
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
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var input appSettings
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	settings, err := validateSettings(input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.settingsStore.save(settings); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存设置失败：" + err.Error()})
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
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req checkCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
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
