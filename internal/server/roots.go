package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hzhengzhi/commentloop/internal/watcher"
)

const maxRoots = 32

const defaultRemotePollInterval = 15 * time.Second

var errTooManyRoots = errors.New("too many roots")

const (
	rootKindLocal  = "local"
	rootKindRemote = "remote"
)

// root is one directory (local or remote) added to the workspace.
type root struct {
	ID      string
	Kind    string
	Path    string // local absolute path or remote absolute path
	Name    string
	Primary bool
	Remote  *remoteConfig
	Offline string

	remoteFS *remoteFS
	lister   *remoteFS

	pollStop chan struct{}
	pollOnce sync.Once
	pollWG   sync.WaitGroup
}

func (ro *root) stopPoll() {
	if ro.pollStop == nil {
		return
	}
	ro.pollOnce.Do(func() { close(ro.pollStop) })
	ro.pollWG.Wait()
}

type rootInfo struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Remote  string `json:"remote,omitempty"`
	Primary bool   `json:"primary"`
	Offline string `json:"offline,omitempty"`
}

type addRootRequest struct {
	Path string `json:"path"`
}

type addRemoteRootRequest struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"keyPath"`
	Path    string `json:"path"`
}

type addRootResponse struct {
	Root    rootInfo `json:"root"`
	Warning string   `json:"warning,omitempty"`
}

type stateRoot struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Name    string `json:"name,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	User    string `json:"user,omitempty"`
	KeyPath string `json:"keyPath,omitempty"`
}

// initRoots loads the primary root, CLI extras and the persisted workspace,
// keeping the directory list free of duplicates.
func (s *Server) initRoots(primary string, extras []string) error {
	resolvedPrimary, err := resolveDirectory(primary)
	if err != nil {
		return fmt.Errorf("primary root: %w", err)
	}
	s.primaryPath = resolvedPrimary

	if _, _, err := s.registerLocalRoot(resolvedPrimary, true); err != nil {
		return err
	}
	for _, extra := range extras {
		if strings.TrimSpace(extra) == "" {
			continue
		}
		if _, _, err := s.registerLocalRoot(extra, false); err != nil {
			log.Printf("commentloop: 无法添加目录 %q: %v", extra, err)
		}
	}

	// Restore every root from the previous session, including remote servers,
	// and CLI --add-dir roots.
	for _, def := range s.persistedRoots {
		switch def.Kind {
		case rootKindRemote:
			cfg := remoteConfigFromState(def)
			if _, _, err := s.registerRemoteRoot(cfg, false); err != nil {
				log.Printf("commentloop: 无法恢复远程目录 %s:%s: %v（保留为离线目录）", cfg.Host, cfg.Path, err)
				s.addOfflineRemoteRoot(cfg, err)
			}
		default:
			if def.Path == "" {
				continue
			}
			if _, _, err := s.registerLocalRoot(def.Path, false); err != nil {
				log.Printf("commentloop: 跳过已保存但不可用的目录 %q: %v", def.Path, err)
			}
		}
	}

	// Persist the merged workspace (canonical paths, deduplicated).
	if err := s.saveWorkspace(); err != nil {
		log.Printf("commentloop: 保存目录列表失败: %v", err)
	}
	return nil
}

func (s *Server) registerLocalRoot(path string, primary bool) (*root, bool, error) {
	resolved, err := resolveDirectory(path)
	if err != nil {
		return nil, false, err
	}

	s.rootMu.Lock()
	if existing := s.localRootByPathLocked(resolved); existing != nil {
		s.rootMu.Unlock()
		return existing, false, nil
	}
	if len(s.rootList) >= maxRoots {
		s.rootMu.Unlock()
		return nil, false, errTooManyRoots
	}

	id := uniqueRootID(resolved, s.roots)
	ro := &root{
		ID:      id,
		Kind:    rootKindLocal,
		Path:    resolved,
		Name:    filepath.Base(resolved),
		Primary: primary,
	}
	w, err := watcher.New(resolved, func(ev watcher.Event) {
		s.onFSChange(ro, ev)
	})
	if err != nil {
		s.rootMu.Unlock()
		return nil, false, err
	}
	s.roots[id] = ro
	s.rootList = append(s.rootList, ro)
	s.watchers[id] = w
	s.rootMu.Unlock()
	return ro, true, nil
}

func (s *Server) addOfflineRemoteRoot(cfg remoteConfig, cause error) *root {
	cfg.Path = strings.TrimSpace(cfg.Path)
	key := remoteKey(cfg)
	s.rootMu.Lock()
	if existing := s.remoteRootByKeyLocked(key); existing != nil {
		if existing.Offline == "" {
			existing.Offline = cause.Error()
		}
		s.rootMu.Unlock()
		return existing
	}
	if len(s.rootList) >= maxRoots {
		s.rootMu.Unlock()
		return nil
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = path.Base(cfg.Path)
		if name == "." || name == "/" {
			name = cfg.Host
		}
	}
	ro := &root{
		ID: uniqueRootID(key, s.roots), Kind: rootKindRemote,
		Path: cfg.Path, Name: name, Remote: &cfg, Offline: cause.Error(),
	}
	s.roots[ro.ID] = ro
	s.rootList = append(s.rootList, ro)
	s.rootMu.Unlock()
	return ro
}

func (s *Server) registerRemoteRoot(cfg remoteConfig, primary bool) (*root, bool, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	fs, err := newRemoteFS(cfg)
	if err != nil {
		return nil, false, err
	}
	cfg = fs.cfg
	key := remoteKey(cfg)

	lister, lerr := newRemoteFS(cfg)
	if lerr != nil {
		log.Printf("commentloop: 远程目录 %s 的轮询连接建立失败，暂用 IO 连接: %v", cfg.Host, lerr)
	}

	s.rootMu.Lock()
	if existing := s.remoteRootByKeyLocked(key); existing != nil {
		if existing.Offline != "" && existing.remoteFS == nil {
			existing.Path = cfg.Path
			existing.Remote = &cfg
			existing.Offline = ""
			existing.remoteFS = fs
			existing.lister = lister
			s.rootMu.Unlock()
			s.startRemotePoll(existing)
			return existing, true, nil
		}
		s.rootMu.Unlock()
		_ = fs.Close()
		if lister != nil {
			_ = lister.Close()
		}
		return existing, false, nil
	}
	if len(s.rootList) >= maxRoots {
		s.rootMu.Unlock()
		_ = fs.Close()
		if lister != nil {
			_ = lister.Close()
		}
		return nil, false, errTooManyRoots
	}
	id := uniqueRootID(key, s.roots)
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = path.Base(cfg.Path)
		if name == "." || name == "/" {
			name = cfg.Host
		}
	}
	ro := &root{
		ID:       id,
		Kind:     rootKindRemote,
		Path:     cfg.Path,
		Name:     name,
		Primary:  primary,
		Remote:   &cfg,
		remoteFS: fs,
		lister:   lister,
	}
	s.roots[id] = ro
	s.rootList = append(s.rootList, ro)
	s.rootMu.Unlock()

	s.startRemotePoll(ro)
	return ro, true, nil
}

func remoteKey(cfg remoteConfig) string {
	return "remote|" + cfg.User + "@" + cfg.Host + ":" +
		strconv.Itoa(cfg.Port) + "|" + cfg.Path
}

func (s *Server) handleAddRoot(w http.ResponseWriter, r *http.Request) {
	var req addRootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入目录路径"})
		return
	}
	resolved, err := resolveDirectory(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": rootError(err)})
		return
	}
	if existing := s.localRootByPath(resolved); existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "目录已在工作区中",
			"id":    existing.ID,
		})
		return
	}

	ro, _, err := s.registerLocalRoot(resolved, false)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errTooManyRoots) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": rootError(err)})
		return
	}
	s.finishAddRoot(w, ro, "")
}

func (s *Server) handleAddRemoteRoot(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req addRemoteRootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	cfg := remoteConfig{
		Name:    strings.TrimSpace(req.Name),
		Host:    strings.TrimSpace(req.Host),
		Port:    req.Port,
		User:    strings.TrimSpace(req.User),
		KeyPath: strings.TrimSpace(req.KeyPath),
		Path:    strings.TrimSpace(req.Path),
	}
	ro, created, err := s.registerRemoteRoot(cfg, false)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errTooManyRoots) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if !created {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "该远程目录已在工作区中",
			"id":    ro.ID,
		})
		return
	}
	warning := ""
	if err := s.primeRemoteCache(ro); err != nil {
		warning = "已连接，但读取远程文件失败：" + err.Error()
	}
	s.finishAddRoot(w, ro, warning)
}

func (s *Server) finishAddRoot(w http.ResponseWriter, ro *root, warning string) {
	s.hub.broadcast(outMessage{Type: "tree"})
	if err := s.saveWorkspace(); err != nil {
		log.Printf("commentloop: 保存目录列表失败: %v", err)
		if warning == "" {
			warning = "目录已添加，但写入本地配置失败"
		} else {
			warning += "；写入本地配置失败"
		}
	}
	writeJSON(w, http.StatusOK, addRootResponse{Root: publicRoot(ro), Warning: warning})
}

func (s *Server) handleRemoveRoot(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少目录 id"})
		return
	}

	s.rootMu.Lock()
	ro := s.roots[id]
	if ro == nil {
		s.rootMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "目录不存在"})
		return
	}
	rootWatcher := s.watchers[id]
	delete(s.roots, id)
	delete(s.watchers, id)
	for i, item := range s.rootList {
		if item.ID == id {
			s.rootList = append(s.rootList[:i], s.rootList[i+1:]...)
			break
		}
	}
	s.rootMu.Unlock()

	// The directory disappears from the workspace immediately. Closing a
	// remote poller can take a while if it is in the middle of a large scan,
	// so finish resource cleanup in the background and do not block the UI.
	s.forgetRemoteTree(ro.ID)
	go func() {
		ro.stopPoll()
		if rootWatcher != nil {
			_ = rootWatcher.Close()
		}
		if ro.remoteFS != nil {
			_ = ro.remoteFS.Close()
		}
		if ro.lister != nil {
			_ = ro.lister.Close()
		}
	}()
	if err := s.saveWorkspace(); err != nil {
		log.Printf("commentloop: 保存目录列表失败: %v", err)
	}
	s.hub.broadcast(outMessage{Type: "tree"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func publicRoot(ro *root) rootInfo {
	info := rootInfo{
		ID: ro.ID, Kind: ro.Kind, Name: ro.Name, Path: ro.Path, Primary: ro.Primary,
		Offline: ro.Offline,
	}
	if ro.Kind == rootKindRemote && ro.Remote != nil {
		info.Remote = ro.Remote.User + "@" + ro.Remote.Host
	}
	return info
}

func (s *Server) rootsSnapshot() []*root {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	out := make([]*root, len(s.rootList))
	copy(out, s.rootList)
	return out
}

func (s *Server) rootByID(id string) *root {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	return s.roots[id]
}

func (s *Server) localRootByPath(path string) *root {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	return s.localRootByPathLocked(path)
}

func (s *Server) localRootByPathLocked(path string) *root {
	for _, ro := range s.rootList {
		if ro.Kind == rootKindLocal && ro.Path == path {
			return ro
		}
	}
	return nil
}

func (s *Server) remoteRootByKeyLocked(key string) *root {
	for _, ro := range s.rootList {
		if ro.Kind == rootKindRemote && ro.Remote != nil && remoteKey(*ro.Remote) == key {
			return ro
		}
	}
	return nil
}

func (s *Server) currentRoot(id string) *root {
	return s.rootByID(id)
}

func (s *Server) rootCount() int {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	return len(s.rootList)
}

// PrimaryDocPath returns the canonical API path for a document relative to
// the primary root. Used to disambiguate the CLI file argument when multiple
// roots contain files with the same name.
func (s *Server) PrimaryDocPath(rel string) string {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	for _, ro := range s.rootList {
		if ro.Primary {
			return ro.ID + "/" + strings.TrimPrefix(rel, "/")
		}
	}
	return rel
}

func (s *Server) closeRoots() {
	s.rootMu.Lock()
	watchers := make([]*watcher.Watcher, 0, len(s.watchers))
	for _, w := range s.watchers {
		watchers = append(watchers, w)
	}
	roots := make([]*root, len(s.rootList))
	copy(roots, s.rootList)
	s.watchers = make(map[string]*watcher.Watcher)
	s.roots = make(map[string]*root)
	s.rootList = nil
	s.rootMu.Unlock()

	for _, ro := range roots {
		ro.stopPoll()
		if ro.remoteFS != nil {
			_ = ro.remoteFS.Close()
		}
		if ro.lister != nil {
			_ = ro.lister.Close()
		}
	}
	for _, w := range watchers {
		_ = w.Close()
	}
}

func remoteConfigFromState(def stateRoot) remoteConfig {
	return remoteConfig{
		Host: def.Host, Port: def.Port, User: def.User,
		KeyPath: def.KeyPath, Path: def.Path, Name: def.Name,
	}
}

func (s *Server) saveWorkspace() error {
	if s.db == nil {
		return nil
	}
	s.rootMu.RLock()
	defs := make([]stateRoot, 0, len(s.rootList))
	for _, ro := range s.rootList {
		if ro.Kind == rootKindRemote && ro.Remote != nil {
			defs = append(defs, stateRoot{
				Kind: rootKindRemote, Path: ro.Path, Name: ro.Name,
				Host: ro.Remote.Host, Port: ro.Remote.Port,
				User: ro.Remote.User, KeyPath: ro.Remote.KeyPath,
			})
			continue
		}
		defs = append(defs, stateRoot{Kind: rootKindLocal, Path: ro.Path})
	}
	s.rootMu.RUnlock()
	return saveRootsDB(s.db, defs)
}

func resolveDirectory(path string) (string, error) {
	expanded, err := expandHome(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if expanded == "" {
		return "", errors.New("目录路径为空")
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("路径不是目录")
	}
	return resolved, nil
}

func expandHome(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func rootError(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "目录不存在"
	case errors.Is(err, os.ErrPermission):
		return "没有权限访问该目录"
	case errors.Is(err, errTooManyRoots):
		return fmt.Sprintf("最多只能添加 %d 个目录", maxRoots)
	default:
		if err != nil && err.Error() == "路径不是目录" {
			return "路径不是目录"
		}
		return "无法添加该目录：" + err.Error()
	}
}

func uniqueRootID(key string, existing map[string]*root) string {
	for n := 4; n <= 32; n += 4 {
		sum := sha256.Sum256([]byte(key))
		if id := "r" + hex.EncodeToString(sum[:n]); existing[id] == nil {
			return id
		}
	}
	// Practically unreachable; keep the ID unique.
	sum := sha256.Sum256([]byte(key))
	for i := 0; ; i++ {
		id := "r" + hex.EncodeToString(sum[:]) + fmt.Sprintf("-%d", i)
		if existing[id] == nil {
			return id
		}
	}
}

type remoteSig struct {
	size int64
	mod  int64
}

func remoteEntryFiles(ro *root, files []remoteFile) []fileEntry {
	out := make([]fileEntry, 0, len(files))
	for _, file := range files {
		dir := path.Dir(file.Rel)
		if dir == "." {
			dir = ""
		}
		out = append(out, fileEntry{
			Path: ro.ID + "/" + file.Rel, RelPath: file.Rel, Name: file.Name,
			Dir: dir, RootID: ro.ID, RootName: ro.Name,
			Size: file.Size, ModTime: file.ModTime,
		})
	}
	return out
}

func (s *Server) storeRemoteTree(ro *root, files []remoteFile) {
	entries := remoteEntryFiles(ro, files)
	s.remoteTreeMu.Lock()
	s.remoteTree[ro.ID] = entries
	s.remoteTreeTruncated[ro.ID] = len(files) >= maxRemoteFiles
	s.remoteTreeLoaded[ro.ID] = true
	s.remoteTreeMu.Unlock()
}

func (s *Server) cachedRemoteTree(id string) ([]fileEntry, bool, bool) {
	s.remoteTreeMu.Lock()
	defer s.remoteTreeMu.Unlock()
	files := s.remoteTree[id]
	out := make([]fileEntry, len(files))
	copy(out, files)
	return out, s.remoteTreeTruncated[id], s.remoteTreeLoaded[id]
}

func (s *Server) forgetRemoteTree(id string) {
	s.remoteTreeMu.Lock()
	delete(s.remoteTree, id)
	delete(s.remoteTreeTruncated, id)
	delete(s.remoteTreeLoaded, id)
	s.remoteTreeMu.Unlock()
}

func (s *Server) updateRemoteTreeEntry(ro *root, rel string, info os.FileInfo) {
	if ro.Kind != rootKindRemote || info == nil {
		return
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	entry := fileEntry{
		Path: ro.ID + "/" + rel, RelPath: rel, Name: path.Base(rel), Dir: dir,
		RootID: ro.ID, RootName: ro.Name, Size: info.Size(), ModTime: info.ModTime(),
	}
	s.remoteTreeMu.Lock()
	files := s.remoteTree[ro.ID]
	replaced := false
	for i := range files {
		if files[i].RelPath == rel {
			files[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		files = append(files, entry)
	}
	s.remoteTree[ro.ID] = files
	s.remoteTreeMu.Unlock()
}

func (s *Server) removeRemoteTreeEntry(ro *root, rel string) {
	s.remoteTreeMu.Lock()
	files := s.remoteTree[ro.ID]
	out := files[:0]
	for _, file := range files {
		if file.RelPath != rel {
			out = append(out, file)
		}
	}
	s.remoteTree[ro.ID] = out
	s.remoteTreeMu.Unlock()
}

func (s *Server) primeRemoteCache(ro *root) error {
	fs := ro.lister
	if fs == nil {
		fs = ro.remoteFS
	}
	if fs == nil {
		return fmt.Errorf("远程目录未连接")
	}
	files, err := fs.list()
	if err != nil {
		return err
	}
	s.storeRemoteTree(ro, files)
	return nil
}

func (s *Server) startRemotePoll(ro *root) {
	if ro.remoteFS == nil {
		return
	}
	ro.pollStop = make(chan struct{})
	ro.pollWG.Add(1)
	go s.pollRemoteRoot(ro)
}

func (s *Server) pollRemoteRoot(ro *root) {
	defer ro.pollWG.Done()

	pollFS := ro.lister
	if pollFS == nil {
		pollFS = ro.remoteFS
	}
	files, err := pollFS.list()
	last := make(map[string]remoteSig)
	if err == nil {
		if s.currentRoot(ro.ID) != ro {
			return
		}
		last = remoteSignatures(files)
		s.storeRemoteTree(ro, files)
		s.hub.broadcast(outMessage{Type: "tree"})
	} else {
		log.Printf("commentloop: 远程目录 %s 初始扫描失败: %v", ro.Name, err)
	}

	ticker := time.NewTicker(s.remotePollInterval)
	defer ticker.Stop()
	lastErr := ""
	for {
		select {
		case <-ro.pollStop:
			return
		case <-ticker.C:
			if s.currentRoot(ro.ID) != ro {
				return
			}
			files, err := pollFS.list()
			if err != nil {
				if err.Error() != lastErr {
					log.Printf("commentloop: 远程目录 %s 轮询失败: %v", ro.Name, err)
					lastErr = err.Error()
				}
				if rerr := pollFS.reconnect(); rerr == nil {
					if files, err = pollFS.list(); err == nil {
						lastErr = ""
					}
				}
				if err != nil {
					continue
				}
			}
			lastErr = ""
			next := remoteSignatures(files)
			changed := false
			for rel, sig := range next {
				old, existed := last[rel]
				if existed && old == sig {
					continue
				}
				changed = true
				canonical := ro.ID + "/" + rel
				if len(s.hub.viewers(canonical)) == 0 {
					continue
				}
				data, _, err := ro.remoteFS.read(rel, maxRemoteDoc)
				if err != nil {
					continue
				}
				res := s.renderer.Render(data, canonical)
				s.mu.Lock()
				same := s.diskHash[canonical] == res.Revision
				s.diskHash[canonical] = res.Revision
				s.mu.Unlock()
				if same {
					continue
				}
				s.hub.broadcastPath(canonical, outMessage{
					Type: "doc", Path: canonical, Content: string(data),
					HTML: res.HTML, Revision: res.Revision, Origin: "external",
				})
			}
			for rel := range last {
				if _, ok := next[rel]; ok {
					continue
				}
				changed = true
				canonical := ro.ID + "/" + rel
				s.mu.Lock()
				delete(s.diskHash, canonical)
				s.mu.Unlock()
				s.hub.broadcastPath(canonical, outMessage{Type: "gone", Path: canonical})
			}
			if changed {
				s.storeRemoteTree(ro, files)
				s.hub.broadcast(outMessage{Type: "tree"})
			}
			last = next
		}
	}
}

func remoteSignatures(files []remoteFile) map[string]remoteSig {
	out := make(map[string]remoteSig, len(files))
	for _, file := range files {
		out[file.Rel] = remoteSig{size: file.Size, mod: file.ModTime.UnixNano()}
	}
	return out
}
