package server

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type agentSession struct {
	ID          string    `json:"id"`
	DocPath     string    `json:"docPath"`
	PiSessionID string    `json:"-"`
	Title       string    `json:"title"`
	Active      bool      `json:"active,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type agentSessionStore struct {
	db *sql.DB
}

func newAgentSessionStore(db *sql.DB) *agentSessionStore {
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

func (s *agentSessionStore) getByID(id string) (agentSession, bool) {
	var item agentSession
	var createdAt, updatedAt string
	err := s.db.QueryRow(`SELECT id,doc_path,pi_session_id,title,created_at,updated_at
		FROM agent_sessions WHERE id=?`, id).
		Scan(&item.ID, &item.DocPath, &item.PiSessionID, &item.Title, &createdAt, &updatedAt)
	if err != nil {
		return agentSession{}, false
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, true
}

func (s *agentSessionStore) activeID(docPath string) string {
	var id string
	if err := s.db.QueryRow(`SELECT session_id FROM doc_agent_active WHERE doc_path=?`, docPath).Scan(&id); err != nil {
		return ""
	}
	return id
}

func (s *agentSessionStore) ensureDefault(docPath string) (agentSession, error) {
	if id := s.activeID(docPath); id != "" {
		if item, ok := s.getByID(id); ok && item.DocPath == docPath {
			return item, nil
		}
	}
	var id string
	err := s.db.QueryRow(`SELECT id FROM agent_sessions WHERE doc_path=? ORDER BY created_at LIMIT 1`, docPath).Scan(&id)
	if err == sql.ErrNoRows {
		item, createErr := s.create(docPath, "默认会话", true)
		return item, createErr
	}
	if err != nil {
		return agentSession{}, err
	}
	if err := s.setActive(docPath, id); err != nil {
		return agentSession{}, err
	}
	item, ok := s.getByID(id)
	if !ok {
		return agentSession{}, os.ErrNotExist
	}
	return item, nil
}

func (s *agentSessionStore) create(docPath, title string, makeActive bool) (agentSession, error) {
	if title == "" {
		var count int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM agent_sessions WHERE doc_path=?`, docPath).Scan(&count)
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
	_, err := s.db.Exec(`INSERT INTO agent_sessions
		(id,doc_path,pi_session_id,title,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
		item.ID, item.DocPath, item.PiSessionID, item.Title, formatTime(now), formatTime(now))
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
	_, err := s.db.Exec(`INSERT INTO doc_agent_active (doc_path,session_id,updated_at)
		VALUES (?,?,?)
		ON CONFLICT(doc_path) DO UPDATE SET session_id=excluded.session_id, updated_at=excluded.updated_at`,
		docPath, id, now)
	return err
}

func (s *agentSessionStore) list(docPath string) ([]agentSession, string, error) {
	if _, err := s.ensureDefault(docPath); err != nil {
		return nil, "", err
	}
	active := s.activeID(docPath)
	rows, err := s.db.Query(`SELECT id,doc_path,pi_session_id,title,created_at,updated_at
		FROM agent_sessions WHERE doc_path=? ORDER BY created_at`, docPath)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]agentSession, 0)
	for rows.Next() {
		var item agentSession
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.DocPath, &item.PiSessionID, &item.Title, &createdAt, &updatedAt); err != nil {
			return nil, "", err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.Active = item.ID == active
		out = append(out, item)
	}
	return out, active, rows.Err()
}

func (s *agentSessionStore) rename(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("会话名称不能为空")
	}
	result, err := s.db.Exec(`UPDATE agent_sessions SET title=?, updated_at=? WHERE id=?`,
		title, formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return os.ErrNotExist
	}
	return nil
}

type agentSessionRequest struct {
	Path  string `json:"path"`
	ID    string `json:"id"`
	Title string `json:"title"`
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	if err := s.agentSessions.setActive(canonical, req.ID); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "会话不存在"})
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
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "会话不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
