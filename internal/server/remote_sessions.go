package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const remoteSessionTTL = 15 * time.Minute

type remoteBrowseSession struct {
	fs       *remoteFS
	home     string
	lastUsed time.Time
}

type remoteSessionRequest struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"keyPath"`
}

type remoteSessionResponse struct {
	Session string `json:"session"`
	Home    string `json:"home"`
}

type remoteSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*remoteBrowseSession
}

func newRemoteSessionStore() *remoteSessionStore {
	return &remoteSessionStore{sessions: make(map[string]*remoteBrowseSession)}
}

func (st *remoteSessionStore) create(fs *remoteFS, home string) string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	id := hex.EncodeToString(buf[:])
	st.mu.Lock()
	now := time.Now()
	for key, session := range st.sessions {
		if now.Sub(session.lastUsed) > remoteSessionTTL {
			_ = session.fs.Close()
			delete(st.sessions, key)
		}
	}
	st.sessions[id] = &remoteBrowseSession{fs: fs, home: home, lastUsed: now}
	st.mu.Unlock()
	return id
}

func (st *remoteSessionStore) get(id string) (*remoteBrowseSession, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	session, ok := st.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Since(session.lastUsed) > remoteSessionTTL {
		_ = session.fs.Close()
		delete(st.sessions, id)
		return nil, false
	}
	session.lastUsed = time.Now()
	return session, true
}

func (st *remoteSessionStore) remove(id string) {
	st.mu.Lock()
	session := st.sessions[id]
	delete(st.sessions, id)
	st.mu.Unlock()
	if session != nil {
		_ = session.fs.Close()
	}
}

func (st *remoteSessionStore) closeAll() {
	st.mu.Lock()
	sessions := st.sessions
	st.sessions = make(map[string]*remoteBrowseSession)
	st.mu.Unlock()
	for _, session := range sessions {
		_ = session.fs.Close()
	}
}

func (s *Server) handleCreateRemoteSession(w http.ResponseWriter, r *http.Request) {
	var req remoteSessionRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	cfg := remoteConfig{
		Host: req.Host, Port: req.Port, User: req.User,
		KeyPath: req.KeyPath, Path: ".",
	}
	fs, err := newRemoteFS(cfg)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	home := fs.cfg.Path
	id := s.remoteSessions.create(fs, home)
	writeJSON(w, http.StatusOK, remoteSessionResponse{Session: id, Home: home})
}

func (s *Server) handleBrowseRemote(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session")
	session, ok := s.remoteSessions.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "远程浏览会话已过期，请重新连接"})
		return
	}
	showHidden := r.URL.Query().Get("hidden") == "1"
	resp, err := session.fs.browse(r.URL.Query().Get("path"), showHidden)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprint(err)})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCloseRemoteSession(w http.ResponseWriter, r *http.Request) {
	s.remoteSessions.remove(r.URL.Query().Get("session"))
	w.WriteHeader(http.StatusNoContent)
}
