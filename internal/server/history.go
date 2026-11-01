package server

import (
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"
)

func (s *Server) saveDocumentVersion(path, content string, touch bool) {
	if s.db == nil {
		return
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		_, err := saveDocVersion(tx, path, content, time.Now(), touch, "doc")
		return err
	}); err != nil {
		slog.Warn("failed to save document version", "path", path, "err", err)
	}
}

func (s *Server) handleListDocVersions(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能读取关联文档") {
		return
	}
	if _, err := s.statRootFile(ro, rel); err != nil {
		writeError(w, http.StatusNotFound, errDocumentNotFound)
		return
	}
	versions, err := s.comments.listVersions(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handleGetDocVersion(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能读取关联文档") {
		return
	}
	if _, err := s.statRootFile(ro, rel); err != nil {
		writeError(w, http.StatusNotFound, errDocumentNotFound)
		return
	}
	version, ok := s.comments.version(r.URL.Query().Get("id"))
	if !ok || version.Path != canonical {
		writeError(w, http.StatusNotFound, errDocVersionNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": version.ID, "path": version.Path, "content": version.Content,
		"createdAt": version.CreatedAt,
	})
}

type restoreDocVersionRequest struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

func (s *Server) handleRestoreDocVersion(w http.ResponseWriter, r *http.Request) {
	if s.rejectReadOnly(w, r) {
		return
	}
	var req restoreDocVersionRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	version, ok := s.comments.version(req.ID)
	if !ok || version.Path != canonical {
		writeError(w, http.StatusNotFound, errDocVersionNotFound)
		return
	}
	current, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		writeError(w, http.StatusNotFound, errDocumentNotFound)
		return
	}
	// Keep the content that is about to be replaced in the timeline even when
	// it came from an external edit that bypassed the normal save path.
	s.saveDocumentVersion(canonical, string(current), false)
	doc, err := s.persistDocument(ro, rel, canonical, version.Content, "restore", true)
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errFailedWriteFile, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
