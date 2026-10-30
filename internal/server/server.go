// Package server implements commentloop's HTTP API, live preview WebSocket and
// static frontend hosting.
package server

import (
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

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/hzhengzhi/commentloop/internal/config"
	"github.com/hzhengzhi/commentloop/internal/ignore"
	"github.com/hzhengzhi/commentloop/internal/lsp"
	"github.com/hzhengzhi/commentloop/internal/render"
	"github.com/hzhengzhi/commentloop/internal/watcher"
	"gorm.io/gorm"
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
	// CLI/MCP writes marked with X-Commentloop-Agent and review rollbacks remain
	// available so the frontend can be a review surface.
	PreviewOnly bool
	// Debug enables asset hot reload (used together with WebDir).
	Debug bool
	// Config carries tunables; zero values fall back to config.Default.
	Config config.Config
	// WebDir is a frontend dist directory served instead of the embedded build.
	WebDir string
}

// Server is the commentloop HTTP application.
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
	engine      *gin.Engine
	mu          sync.Mutex
	diskHash    map[string]string
	closeOnce   sync.Once
	assetDone   chan struct{}
	assetWG     sync.WaitGroup

	remoteSessions *remoteSessionStore
	db             *gorm.DB
	persistedRoots []stateRoot
	comments       *commentStore
	prompts        *promptTemplateStore
	discussions    *discussionStore
	discussionSync *discussionSyncService
	agentCaps      *agentCapabilityStore
	agentJobs      *agentJobStore
	consensus      *consensusScheduler
	codeRepos      *codeRepoStore
	settingsStore  *settingsStore
	settings       *settingsSnapshot
	lsp            *lsp.Manager
	agentRunner    *agentRunner
	agentSessions  *agentSessionStore
	stateDir       string
	agentBaseURL   string

	cfg config.Config
	// overflowRoots are persisted roots skipped because of limits.roots.
	overflowRoots []stateRoot
	localTrees    localTreeCache

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
	cfg := opts.Config.WithDefaults()

	db, err := openStateDB(opts.StateDir)
	if err != nil {
		return nil, err
	}
	persistedRoots, err := loadRootsDB(db)
	if err != nil {
		_ = closeStateDB(db)
		return nil, err
	}
	settingsStore := newSettingsStore(db)
	loadedSettings, settingsErr := settingsStore.load()
	if settingsErr != nil {
		loadedSettings = defaultAppSettings()
	}

	prompts := newPromptTemplateStore(db)
	if err := prompts.ensureDefaults(); err != nil {
		_ = closeStateDB(db)
		return nil, fmt.Errorf("初始化提示词模板失败: %w", err)
	}
	s := &Server{
		db:                  db,
		comments:            newCommentStore(db),
		prompts:             prompts,
		discussions:         newDiscussionStore(db),
		agentCaps:           newAgentCapabilityStore(),
		agentJobs:           newAgentJobStore(db),
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
		remotePollInterval:  cfg.Remote.PollInterval,
		cfg:                 cfg,
	}
	if err := s.initRoots(opts.Root, opts.ExtraRoots); err != nil {
		s.closeRoots()
		_ = closeStateDB(db)
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
	s.engine = s.routes()
	s.agentRunner = newAgentRunner(s)
	s.consensus = newConsensusScheduler(s)
	s.discussionSync = newDiscussionSyncService(s)
	return s, nil
}

// Close stops background work.
// SetAgentBaseURL tells automatically spawned agents which commentloop instance to
// connect to through COMMENTLOOP_URL, even when multiple instances are running.
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
			_ = closeStateDB(s.db)
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
			writeError(w, http.StatusForbidden, errCrossSiteRequestRejected)
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
	s.localTrees.invalidate(ro)
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
