package server

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func newAgentSessionStore(db *gorm.DB) *agentSessionStore {
	return &agentSessionStore{db: db}
}

func newPiSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (r agentSessionRow) session() agentSession {
	return agentSession{
		ID: r.ID, DocPath: r.DocPath, PiSessionID: r.PiSessionID, Title: r.Title,
		CreatedAt: parseTime(r.CreatedAt), UpdatedAt: parseTime(r.UpdatedAt),
	}
}

func (s *agentSessionStore) getByID(id string) (agentSession, bool) {
	var row agentSessionRow
	if err := s.db.Take(&row, "id = ?", id).Error; err != nil {
		return agentSession{}, false
	}
	return row.session(), true
}

func (s *agentSessionStore) activeID(docPath string) string {
	var row docAgentActiveRow
	if err := s.db.Take(&row, "doc_path = ?", docPath).Error; err != nil {
		return ""
	}
	return row.SessionID
}

func (s *agentSessionStore) ensureDefault(docPath string) (agentSession, error) {
	if id := s.activeID(docPath); id != "" {
		if item, ok := s.getByID(id); ok && item.DocPath == docPath {
			return item, nil
		}
	}
	var first agentSessionRow
	err := s.db.Select("id").Where("doc_path = ?", docPath).Order("created_at").Take(&first).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		item, createErr := s.create(docPath, "默认会话", true)
		return item, createErr
	}
	if err != nil {
		return agentSession{}, err
	}
	if err := s.setActive(docPath, first.ID); err != nil {
		return agentSession{}, err
	}
	item, ok := s.getByID(first.ID)
	if !ok {
		return agentSession{}, os.ErrNotExist
	}
	return item, nil
}

func (s *agentSessionStore) create(docPath, title string, makeActive bool) (agentSession, error) {
	if title == "" {
		var count int64
		_ = s.db.Model(&agentSessionRow{}).Where("doc_path = ?", docPath).Count(&count).Error
		if count == 0 {
			title = "默认会话"
		} else {
			title = fmt.Sprintf("会话 %d", count+1)
		}
	}
	now := time.Now()
	item := agentSession{
		ID: "as_" + commentID(), DocPath: docPath, PiSessionID: newPiSessionID(),
		Title: title, CreatedAt: now, UpdatedAt: now,
	}
	err := s.db.Create(&agentSessionRow{
		ID: item.ID, DocPath: item.DocPath, PiSessionID: item.PiSessionID, Title: item.Title,
		CreatedAt: formatTime(now), UpdatedAt: formatTime(now),
	}).Error
	if err != nil {
		return agentSession{}, err
	}
	if makeActive {
		if err := s.setActive(docPath, item.ID); err != nil {
			return agentSession{}, err
		}
		item.Active = true
	}
	return item, nil
}

func (s *agentSessionStore) setActive(docPath, id string) error {
	item, ok := s.getByID(id)
	if !ok || item.DocPath != docPath {
		return os.ErrNotExist
	}
	now := formatTime(time.Now())
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "doc_path"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id", "updated_at"}),
	}).Create(&docAgentActiveRow{DocPath: docPath, SessionID: id, UpdatedAt: now}).Error
}

func (s *agentSessionStore) list(docPath string) ([]agentSession, string, error) {
	if _, err := s.ensureDefault(docPath); err != nil {
		return nil, "", err
	}
	active := s.activeID(docPath)
	var rows []agentSessionRow
	if err := s.db.Where("doc_path = ?", docPath).Order("created_at").Find(&rows).Error; err != nil {
		return nil, "", err
	}
	out := make([]agentSession, 0, len(rows))
	for _, row := range rows {
		item := row.session()
		item.Active = item.ID == active
		out = append(out, item)
	}
	return out, active, nil
}

func (s *agentSessionStore) rename(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("会话名称不能为空")
	}
	result := s.db.Model(&agentSessionRow{}).Where("id = ?", id).Updates(map[string]any{
		"title": title, "updated_at": formatTime(time.Now()),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *Server) resolveDocCanonical(path string) (string, error) {
	ro, rel, err := s.resolveDocRef(path)
	if err != nil {
		return "", err
	}
	return ro.ID + "/" + rel, nil
}

func (s *Server) handleGetAgentSessions(w http.ResponseWriter, r *http.Request) {
	canonical, err := s.resolveDocCanonical(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	sessions, active, err := s.agentSessions.list(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": canonical, "sessions": sessions, "activeId": active,
	})
}

func (s *Server) handleCreateAgentSession(w http.ResponseWriter, r *http.Request) {
	var req agentSessionRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	canonical, err := s.resolveDocCanonical(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	item, err := s.agentSessions.create(canonical, req.Title, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	item.Active = true
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleSetActiveAgentSession(w http.ResponseWriter, r *http.Request) {
	var req agentSessionRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	canonical, err := s.resolveDocCanonical(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	if err := s.agentSessions.setActive(canonical, req.ID); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errSessionNotFound)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRenameAgentSession(w http.ResponseWriter, r *http.Request) {
	var req agentSessionRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	if err := s.agentSessions.rename(req.ID, req.Title); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errSessionNotFound)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
