package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

type promptTemplateRequest struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Body              string `json:"body"`
	AllowDocumentEdit bool   `json:"allowDocumentEdit"`
	AllowCodeRead     bool   `json:"allowCodeRead"`
	AllowCommentReply bool   `json:"allowCommentReply"`
}

type optimizePromptRequest struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	Body              string `json:"body"`
	AllowDocumentEdit bool   `json:"allowDocumentEdit"`
	AllowCodeRead     bool   `json:"allowCodeRead"`
	AllowCommentReply bool   `json:"allowCommentReply"`
}

func promptLanguage(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language"))), "en") {
		return "en"
	}
	return "zh"
}

func (s *Server) handleListPromptTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := s.prompts.list()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	lang := promptLanguage(r)
	for i := range items {
		items[i] = localizePromptTemplate(items[i], lang)
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": items})
}

func (s *Server) handleCreatePromptTemplate(w http.ResponseWriter, r *http.Request) {
	var req promptTemplateRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	item, err := s.prompts.create(req.Name, req.Description, req.Body,
		req.AllowDocumentEdit, req.AllowCodeRead, req.AllowCommentReply)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, localizePromptTemplate(item, promptLanguage(r)))
}

func (s *Server) handleUpdatePromptTemplate(w http.ResponseWriter, r *http.Request) {
	var req promptTemplateRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少模板 ID"})
		return
	}
	item, err := s.prompts.update(req.ID, req.Name, req.Description, req.Body,
		req.AllowDocumentEdit, req.AllowCodeRead, req.AllowCommentReply)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "模板不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, localizePromptTemplate(item, promptLanguage(r)))
}

func (s *Server) handleDeletePromptTemplate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少模板 ID"})
		return
	}
	if err := s.prompts.delete(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "模板不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOptimizePromptTemplate(w http.ResponseWriter, r *http.Request) {
	var req optimizePromptRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	body, err := s.optimizePromptTemplate(r.Context(), promptLanguage(r), req.Name, req.Description, req.Body,
		req.AllowDocumentEdit, req.AllowCodeRead, req.AllowCommentReply)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"body": body})
}

func (s *Server) handleResetPromptTemplate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req struct {
		ID string `json:"id"`
	}
	if !bindJSON(w, r, defaultJSONBodyLimit, &req) {
		return
	}
	item, err := s.prompts.reset(strings.TrimSpace(req.ID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "模板不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, localizePromptTemplate(item, promptLanguage(r)))
}
