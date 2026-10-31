package server

import (
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"
)

func (s *Server) saveDocumentVersion(path, content string) {
	if s.db == nil || content == "" {
		return
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		_, err := saveDocVersion(tx, path, content, time.Now())
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
	versions, err := s.comments.listVersions(ro.ID + "/" + rel)
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
	version, ok := s.comments.version(r.URL.Query().Get("id"))
	if !ok || version.Path != ro.ID+"/"+rel {
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
	if _, _, err := s.readRootFile(ro, rel, maxDocSize); err != nil {
		writeError(w, http.StatusNotFound, errDocumentNotFound)
		return
	}
	doc, err := s.persistDocument(ro, rel, canonical, version.Content, "restore", true)
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errFailedWriteFile, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
