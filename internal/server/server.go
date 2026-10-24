// Package server implements mdlive's HTTP API, live preview WebSocket and
// static frontend hosting.
package server

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"mdlive/internal/ignore"
	"mdlive/internal/lsp"
	"mdlive/internal/render"
	"mdlive/internal/watcher"
)

// Options configures a Server.
type Options struct {
	// Root is the primary directory exposed by the app.
	Root string
	// ExtraRoots are additional directories exposed at startup (CLI --add-dir).
	ExtraRoots []string
	// StateDir stores persisted extra roots. Empty disables persistence.
	StateDir string
	// WebFS contains the built frontend, rooted at the dist directory.
	WebFS fs.FS
	// Version is reported by /api/config.
	Version string
	// AllowRemote disables the loopback Host header check. Set it when the
	// server is intentionally bound to a non-loopback interface.
	AllowRemote bool
	// PreviewOnly hides the editor and blocks frontend saves/deletes. Agent
	// CLI/MCP writes marked with X-Mdlive-Agent and review rollbacks remain
	// available so the frontend can be a review surface.
	PreviewOnly bool
	// Debug enables asset hot reload (used together with WebDir).
	Debug bool
	// WebDir is a frontend dist directory served instead of the embedded build.
	WebDir string
}

// Server is the mdlive HTTP application.
type Server struct {
	primaryPath string
	web         fs.FS
	version     string
	allowAny    bool
	previewOnly bool
	debug       bool
	webDir      string
	renderer    *render.Renderer
	hub         *Hub
	upgrader    websocket.Upgrader
	mux         *http.ServeMux
	mu          sync.Mutex
	diskHash    map[string]string
	closeOnce   sync.Once
	assetDone   chan struct{}
	assetWG     sync.WaitGroup

	remoteSessions *remoteSessionStore
	db             *sql.DB
	persistedRoots []stateRoot
	comments       *commentStore
	prompts        *promptTemplateStore
	discussions    *discussionStore
	discussionSync *discussionSyncService
	agentCaps      *agentCapabilityStore
	consensus      *consensusScheduler
	codeRepos      *codeRepoStore
	settingsStore  *settingsStore
	settings       *settingsSnapshot
	lsp            *lsp.Manager
	agentRunner    *agentRunner
	agentSessions  *agentSessionStore
	stateDir       string
	agentBaseURL   string

	remoteTreeMu        sync.Mutex
	remoteTree          map[string][]fileEntry
	remoteTreeTruncated map[string]bool
	remoteTreeLoaded    map[string]bool
	remotePollInterval  time.Duration

	rootMu   sync.RWMutex
	roots    map[string]*root
	rootList []*root
	watchers map[string]*watcher.Watcher
}

// New validates the roots, starts filesystem watchers and wires all routes.
func New(opts Options) (*Server, error) {
	if opts.WebFS == nil {
		return nil, errors.New("server: WebFS is required")
	}

	db, err := openStateDB(opts.StateDir)
	if err != nil {
		return nil, err
	}
	persistedRoots, err := loadRootsDB(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	settingsStore := newSettingsStore(db)
	loadedSettings, settingsErr := settingsStore.load()
	if settingsErr != nil {
		loadedSettings = defaultAppSettings()
	}

	prompts := newPromptTemplateStore(db)
	if err := prompts.ensureDefaults(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化提示词模板失败: %w", err)
	}
	s := &Server{
		db:                  db,
		comments:            newCommentStore(db),
		prompts:             prompts,
		discussions:         newDiscussionStore(db),
		agentCaps:           newAgentCapabilityStore(),
		codeRepos:           newCodeRepoStore(db, opts.StateDir),
		agentSessions:       newAgentSessionStore(db),
		stateDir:            opts.StateDir,
		settingsStore:       settingsStore,
		settings:            newSettingsSnapshot(loadedSettings),
		lsp:                 lsp.NewManager(),
		persistedRoots:      persistedRoots,
		web:                 opts.WebFS,
		version:             opts.Version,
		allowAny:            opts.AllowRemote,
		previewOnly:         opts.PreviewOnly,
		debug:               opts.Debug,
		renderer:            render.New(),
		hub:                 newHub(),
		diskHash:            make(map[string]string),
		roots:               make(map[string]*root),
		watchers:            make(map[string]*watcher.Watcher),
		remoteSessions:      newRemoteSessionStore(),
		remoteTree:          make(map[string][]fileEntry),
		remoteTreeTruncated: make(map[string]bool),
		remoteTreeLoaded:    make(map[string]bool),
		remotePollInterval:  defaultRemotePollInterval,
	}
	if err := s.initRoots(opts.Root, opts.ExtraRoots); err != nil {
		s.closeRoots()
		_ = db.Close()
		return nil, err
	}
	if opts.Debug && strings.TrimSpace(opts.WebDir) != "" {
		s.configureDiskAssets(opts.WebDir)
		s.startAssetPoll()
	}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  16 * 1024,
		WriteBufferSize: 16 * 1024,
		CheckOrigin:     s.checkOrigin,
	}
	s.mux = s.routes()
	s.agentRunner = newAgentRunner(s)
	s.consensus = newConsensusScheduler(s)
	s.discussionSync = newDiscussionSyncService(s)
	return s, nil
}

// Close stops background work.
// SetAgentBaseURL tells automatically spawned agents which mdlive instance to
// connect to through MDLIVE_URL, even when multiple instances are running.
func (s *Server) SetAgentBaseURL(baseURL string) {
	s.agentBaseURL = strings.TrimSpace(baseURL)
}

func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.stopAssetPoll()
		if s.remoteSessions != nil {
			s.remoteSessions.closeAll()
		}
		s.closeRoots()
		if s.lsp != nil {
			s.lsp.Close()
		}
		if s.agentRunner != nil {
			s.agentRunner.Close()
		}
		if s.discussionSync != nil {
			s.discussionSync.close()
		}
		if s.consensus != nil {
			s.consensus.close()
		}
		if s.db != nil {
			_ = s.db.Close()
		}
		s.remoteTreeMu.Lock()
		s.remoteTree = make(map[string][]fileEntry)
		s.remoteTreeTruncated = make(map[string]bool)
		s.remoteTreeLoaded = make(map[string]bool)
		s.remoteTreeMu.Unlock()
	})
	return err
}

// Root returns the resolved primary root directory.
func (s *Server) Root() string { return s.primaryPath }

// RootCount returns how many directories are currently in the workspace.
func (s *Server) RootCount() int { return s.rootCount() }

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/tree", s.handleTree)
	mux.HandleFunc("GET /api/doc", s.handleGetDoc)
	mux.HandleFunc("PUT /api/doc", s.requireSameOrigin(s.handleSaveDoc))
	mux.HandleFunc("POST /api/doc", s.requireSameOrigin(s.handleSaveDoc))
	mux.HandleFunc("DELETE /api/doc", s.requireSameOrigin(s.handleDeleteDoc))
	mux.HandleFunc("GET /api/highlight.css", s.handleHighlightCSS)
	mux.HandleFunc("GET /api/browse", s.requireSameOrigin(s.handleBrowse))
	mux.HandleFunc("POST /api/render", s.requireSameOrigin(s.handleRender))
	mux.HandleFunc("POST /api/roots", s.requireSameOrigin(s.handleAddRoot))
	mux.HandleFunc("POST /api/roots/remote", s.requireSameOrigin(s.handleAddRemoteRoot))
	mux.HandleFunc("POST /api/agent/refresh", s.requireSameOrigin(s.handleAgentRefresh))
	mux.HandleFunc("GET /api/comments", s.handleGetComments)
	mux.HandleFunc("POST /api/comments", s.requireSameOrigin(s.handleCreateComment))
	mux.HandleFunc("POST /api/comments/reply", s.requireSameOrigin(s.handleReplyComment))
	mux.HandleFunc("POST /api/comments/resolve", s.requireSameOrigin(s.handleResolveComment))
	mux.HandleFunc("POST /api/comments/group", s.requireSameOrigin(s.handleGroupComment))
	mux.HandleFunc("POST /api/comments/rollback", s.requireSameOrigin(s.handleRollbackComment))
	mux.HandleFunc("POST /api/comments/trigger", s.requireSameOrigin(s.handleTriggerCommentAgent))
	mux.HandleFunc("GET /api/code/repos", s.handleListCodeRepos)
	mux.HandleFunc("DELETE /api/code/repos", s.requireSameOrigin(s.handleDeleteCodeRepo))
	mux.HandleFunc("GET /api/doc/code-repos", s.handleDocCodeRepos)
	mux.HandleFunc("POST /api/doc/code-repos", s.requireSameOrigin(s.handleAddDocCodeRepo))
	mux.HandleFunc("DELETE /api/doc/code-repos", s.requireSameOrigin(s.handleRemoveDocCodeRepo))
	mux.HandleFunc("GET /api/code/info", s.handleCodeRepoInfo)
	mux.HandleFunc("GET /api/code/files", s.handleCodeRepoFiles)
	mux.HandleFunc("GET /api/code/file", s.handleCodeRepoFile)
	mux.HandleFunc("GET /api/code/search", s.handleCodeRepoSearch)
	mux.HandleFunc("GET /api/code/branches", s.handleCodeRepoBranches)
	mux.HandleFunc("POST /api/code/branch", s.requireSameOrigin(s.handleSwitchCodeRepoBranch))
	mux.HandleFunc("POST /api/lsp/definition", s.requireSameOrigin(s.handleLSPDefinition))
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("GET /api/settings/defaults", s.handleGetDefaultSettings)
	mux.HandleFunc("PUT /api/settings", s.requireSameOrigin(s.handleSaveSettings))
	mux.HandleFunc("POST /api/settings/check-command", s.requireSameOrigin(s.handleCheckCommand))
	mux.HandleFunc("GET /api/prompt-templates", s.handleListPromptTemplates)
	mux.HandleFunc("POST /api/prompt-templates", s.requireSameOrigin(s.handleCreatePromptTemplate))
	mux.HandleFunc("PUT /api/prompt-templates", s.requireSameOrigin(s.handleUpdatePromptTemplate))
	mux.HandleFunc("DELETE /api/prompt-templates", s.requireSameOrigin(s.handleDeletePromptTemplate))
	mux.HandleFunc("POST /api/prompt-templates/reset", s.requireSameOrigin(s.handleResetPromptTemplate))
	mux.HandleFunc("POST /api/prompt-templates/optimize", s.requireSameOrigin(s.handleOptimizePromptTemplate))
	mux.HandleFunc("GET /api/doc/discussions", s.handleDocDiscussions)
	mux.HandleFunc("GET /api/doc/discussions/available", s.handleAvailableDiscussionSessions)
	mux.HandleFunc("POST /api/doc/discussions", s.requireSameOrigin(s.handleImportDiscussion))
	mux.HandleFunc("DELETE /api/doc/discussions", s.requireSameOrigin(s.handleDeleteDiscussion))
	mux.HandleFunc("POST /api/doc/consensus/refresh", s.requireSameOrigin(s.handleRefreshConsensus))
	mux.HandleFunc("GET /api/doc/agent-sessions", s.handleGetAgentSessions)
	mux.HandleFunc("POST /api/doc/agent-sessions", s.requireSameOrigin(s.handleCreateAgentSession))
	mux.HandleFunc("POST /api/doc/agent-sessions/active", s.requireSameOrigin(s.handleSetActiveAgentSession))
	mux.HandleFunc("PATCH /api/doc/agent-sessions", s.requireSameOrigin(s.handleRenameAgentSession))
	mux.HandleFunc("DELETE /api/comments", s.requireSameOrigin(s.handleDeleteComment))
	mux.HandleFunc("POST /api/remote/session", s.requireSameOrigin(s.handleCreateRemoteSession))
	mux.HandleFunc("GET /api/remote/browse", s.requireSameOrigin(s.handleBrowseRemote))
	mux.HandleFunc("DELETE /api/remote/session", s.requireSameOrigin(s.handleCloseRemoteSession))
	mux.HandleFunc("DELETE /api/roots", s.requireSameOrigin(s.handleRemoveRoot))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/raw/", s.handleRaw)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "接口不存在"})
	})
	mux.HandleFunc("/", s.handleStatic)
	return mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: blob: https: http:; font-src 'self' data:; "+
			"connect-src 'self' ws: wss:; object-src 'none'; base-uri 'self'; "+
			"form-action 'none'; frame-ancestors 'none'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Cache-Control", "no-store")
	}
	if !s.hostAllowed(r) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) hostAllowed(r *http.Request) bool {
	if s.allowAny {
		return true
	}
	return isLoopbackHost(r.Host)
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	// Vite's dev server proxies from localhost:5173 to 127.0.0.1:4571, so the
	// ports differ. Any loopback-to-loopback pair is safe: a remote website
	// can never present a loopback Origin.
	return isLoopbackHost(u.Host) && isLoopbackHost(r.Host)
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) requireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkOrigin(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "跨站请求已被拒绝"})
			return
		}
		next(w, r)
	}
}

// resolveRef maps a canonical "<rootID>/<relative-path>" path to its root and
// validated relative path.
func (s *Server) resolveRef(canonical string) (*root, string, error) {
	if canonical == "" || strings.ContainsRune(canonical, 0) || strings.HasPrefix(canonical, "/") {
		return nil, "", errBadPath
	}
	slash := strings.IndexByte(canonical, '/')
	if slash <= 0 || slash == len(canonical)-1 {
		return nil, "", errBadPath
	}
	ro := s.rootByID(canonical[:slash])
	if ro == nil {
		return nil, "", errBadPath
	}

	rel := canonical[slash+1:]
	slashed := strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(slashed) {
		return nil, "", errBadPath
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, "", errBadPath
	}
	if ro.Kind == rootKindRemote {
		return ro, clean, nil
	}

	abs := filepath.Join(ro.Path, filepath.FromSlash(clean))
	if !within(ro.Path, abs) {
		return nil, "", errBadPath
	}

	// Resolve the deepest existing ancestor so symlinks cannot point outside.
	probe := abs
	var suffix string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			target := filepath.Join(resolved, suffix)
			if !within(ro.Path, target) {
				return nil, "", errBadPath
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, "", errBadPath
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return nil, "", errBadPath
		}
		suffix = filepath.Join(filepath.Base(probe), suffix)
		probe = parent
	}
	return ro, clean, nil
}

func (s *Server) resolveDocRef(canonical string) (*root, string, error) {
	if !ignore.IsMarkdown(canonical) {
		return nil, "", errBadPath
	}
	ro, rel, err := s.resolveRef(canonical)
	if err != nil {
		return nil, "", err
	}
	return ro, rel, nil
}

func localFile(ro *root, rel string) (string, error) {
	abs := filepath.Join(ro.Path, filepath.FromSlash(rel))
	if !within(ro.Path, abs) {
		return "", errBadPath
	}
	return abs, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (s *Server) onFSChange(ro *root, ev watcher.Event) {
	if s.currentRoot(ro.ID) != ro {
		return
	}
	if ev.IsDir {
		s.hub.broadcast(outMessage{Type: "tree"})
		return
	}
	if !ignore.IsMarkdown(ev.Rel) {
		return
	}
	canonical := ro.ID + "/" + ev.Rel
	if ev.Removed {
		s.mu.Lock()
		delete(s.diskHash, canonical)
		s.mu.Unlock()
		s.hub.broadcastPath(canonical, outMessage{Type: "gone", Path: canonical})
		s.hub.broadcast(outMessage{Type: "tree"})
		return
	}

	data, _, err := s.readRootFile(ro, ev.Rel, maxDocSize)
	if err != nil {
		return
	}
	res := s.renderer.Render(data, canonical)

	s.mu.Lock()
	same := s.diskHash[canonical] == res.Revision
	s.diskHash[canonical] = res.Revision
	s.mu.Unlock()
	if same {
		return
	}

	s.hub.broadcastPath(canonical, outMessage{
		Type: "doc", Path: canonical, Content: string(data),
		HTML: res.HTML, Revision: res.Revision, Origin: "external",
	})
	s.hub.broadcast(outMessage{Type: "tree"})
}

type docSnapshot struct {
	path    string
	content string
	result  render.Result
	modTime time.Time
	size    int64
}

func (s *Server) snapshot(canonical string) (docSnapshot, error) {
	ro, rel, err := s.resolveDocRef(canonical)
	if err != nil {
		return docSnapshot{}, err
	}
	fullCanonical := ro.ID + "/" + rel
	data, modTime, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		return docSnapshot{}, err
	}
	res := s.renderer.Render(data, fullCanonical)
	s.mu.Lock()
	s.diskHash[fullCanonical] = res.Revision
	s.mu.Unlock()
	return docSnapshot{
		path: fullCanonical, content: string(data), result: res,
		modTime: modTime, size: int64(len(data)),
	}, nil
}
