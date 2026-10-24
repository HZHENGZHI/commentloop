package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mdlive/internal/ignore"
)

const (
	maxDocSize   = 8 << 20 // 8 MiB per document / websocket edit
	maxTreeFiles = 5000
)

var errBadPath = errors.New("invalid path")

type docResponse struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	HTML     string    `json:"html"`
	Revision string    `json:"revision"`
	ModTime  time.Time `json:"modTime"`
	Size     int64     `json:"size"`
}

type fileEntry struct {
	Path     string    `json:"path"`    // canonical: <rootID>/<relative-path>
	RelPath  string    `json:"relPath"` // path inside its directory
	Name     string    `json:"name"`
	Dir      string    `json:"dir"`
	RootID   string    `json:"rootId"`
	RootName string    `json:"rootName"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"modTime"`
}

type treeRoot struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Remote  string      `json:"remote,omitempty"`
	Primary bool        `json:"primary"`
	Offline string      `json:"offline,omitempty"`
	Loading bool        `json:"loading,omitempty"`
	Files   []fileEntry `json:"files"`
}

type treeResponse struct {
	Roots     []treeRoot  `json:"roots"`
	Files     []fileEntry `json:"files"`
	Truncated bool        `json:"truncated"`
}

type saveRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.version})
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"root":        s.primaryPath,
		"rootName":    filepath.Base(s.primaryPath),
		"version":     s.version,
		"previewOnly": s.previewOnly,
		"debug":       s.debug,
		"hotReload":   s.webDir != "",
		"rootCount":   s.rootCount(),
		"agentName":   s.settings.get().Agent.Name,
	})
}

func (s *Server) handleTree(w http.ResponseWriter, _ *http.Request) {
	roots := s.rootsSnapshot()
	resp := treeResponse{
		Roots: make([]treeRoot, 0, len(roots)),
		Files: make([]fileEntry, 0, 128),
	}
	for _, ro := range roots {
		entry := treeRoot{
			ID:      ro.ID,
			Kind:    ro.Kind,
			Name:    ro.Name,
			Path:    ro.Path,
			Primary: ro.Primary,
			Files:   make([]fileEntry, 0, 32),
		}
		if ro.Kind == rootKindRemote && ro.Remote != nil {
			entry.Remote = ro.Remote.User + "@" + ro.Remote.Host
			entry.Offline = ro.Offline
		}
		if !resp.Truncated {
			if ro.Kind == rootKindRemote {
				files, truncated, loaded := s.cachedRemoteTree(ro.ID)
				entry.Files = files
				entry.Loading = !loaded && ro.Offline == ""
				resp.Truncated = truncated
			} else {
				files, truncated, err := s.listRootFiles(ro)
				if err == nil {
					entry.Files = files
					resp.Truncated = truncated
				}
			}
		}
		sort.Slice(entry.Files, func(i, j int) bool {
			a, b := strings.ToLower(entry.Files[i].RelPath), strings.ToLower(entry.Files[j].RelPath)
			if a == b {
				return entry.Files[i].RelPath < entry.Files[j].RelPath
			}
			return a < b
		})
		resp.Roots = append(resp.Roots, entry)
		resp.Files = append(resp.Files, entry.Files...)
	}
	sort.Slice(resp.Files, func(i, j int) bool {
		a := strings.ToLower(resp.Files[i].RootName + "/" + resp.Files[i].RelPath)
		b := strings.ToLower(resp.Files[j].RootName + "/" + resp.Files[j].RelPath)
		if a == b {
			return resp.Files[i].Path < resp.Files[j].Path
		}
		return a < b
	})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) listRootFiles(ro *root) ([]fileEntry, bool, error) {
	entry := make([]fileEntry, 0, 32)
	truncated := false
	_ = filepath.WalkDir(ro.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == ro.Path {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if ignore.SkipDir(name) {
				return fs.SkipDir
			}
			return nil
		}
		if ignore.IsTemp(name) || d.Type()&os.ModeSymlink != 0 || !ignore.IsMarkdown(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(ro.Path, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		entry = append(entry, fileEntry{
			Path: ro.ID + "/" + rel, RelPath: rel, Name: name, Dir: dir,
			RootID: ro.ID, RootName: ro.Name, Size: info.Size(), ModTime: info.ModTime(),
		})
		if len(entry) >= maxTreeFiles {
			truncated = true
			return fs.SkipAll
		}
		return nil
	})
	return entry, truncated, nil
}

func (s *Server) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	snap, err := s.snapshot(r.URL.Query().Get("path"))
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法读取文件：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, docResponse{
		Path: snap.path, Content: snap.content, HTML: snap.result.HTML,
		Revision: snap.result.Revision, ModTime: snap.modTime, Size: snap.size,
	})
}

func (s *Server) handleSaveDoc(w http.ResponseWriter, r *http.Request) {
	create := r.Method == http.MethodPost
	// Review mode still allows managing the document list: creating a new
	// document and importing files are frontend actions. Editing an existing
	// document (PUT) remains Agent-only.
	if !create && s.rejectReadOnly(w, r) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDocSize+64*1024)
	var req saveRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文档过大（上限 8 MiB）"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if len(req.Content) > maxDocSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文档过大（上限 8 MiB）"})
		return
	}

	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel

	_, statErr := s.statRootFile(ro, rel)
	existed := statErr == nil
	if create && existed {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "同名文件已存在"})
		return
	}
	if statErr != nil && ro.Kind == rootKindLocal && !os.IsNotExist(statErr) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法访问文件"})
		return
	}
	doc, err := s.persistDocument(ro, rel, canonical, req.Content, "save", existed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入文件失败：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// persistDocument writes content through the same local/remote path as a normal
// save, then broadcasts the rendered document. Rollback uses it with a
// different origin so the client treats the disk content as authoritative.
func (s *Server) persistDocument(ro *root, rel, canonical, content, origin string, existed bool) (docResponse, error) {
	if err := s.writeRootFile(ro, rel, []byte(content)); err != nil {
		return docResponse{}, err
	}
	res := s.renderer.Render([]byte(content), canonical)
	s.mu.Lock()
	s.diskHash[canonical] = res.Revision
	s.mu.Unlock()

	if ro.Kind == rootKindRemote {
		if info, statErr := s.statRootFile(ro, rel); statErr == nil {
			s.updateRemoteTreeEntry(ro, rel, info)
		}
	}
	if !existed {
		s.hub.broadcast(outMessage{Type: "tree"})
	}
	s.hub.broadcastPath(canonical, outMessage{
		Type: "doc", Path: canonical, Content: content,
		HTML: res.HTML, Revision: res.Revision, Origin: origin,
	})

	info, err := s.statRootFile(ro, rel)
	if err != nil {
		return docResponse{
			Path: canonical, Content: content, HTML: res.HTML, Revision: res.Revision,
			Size: int64(len(content)),
		}, nil
	}
	return docResponse{
		Path: canonical, Content: content, HTML: res.HTML, Revision: res.Revision,
		ModTime: info.ModTime(), Size: info.Size(),
	}, nil
}

func (s *Server) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.removeRootFile(ro, rel); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "删除失败：" + err.Error()})
		return
	}
	s.mu.Lock()
	delete(s.diskHash, canonical)
	s.mu.Unlock()
	if ro.Kind == rootKindRemote {
		s.removeRemoteTreeEntry(ro, rel)
	}
	s.hub.broadcast(outMessage{Type: "tree"})
	s.hub.broadcastPath(canonical, outMessage{Type: "gone", Path: canonical})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocSize+64*1024)
	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if len(req.Content) > maxDocSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文档过大（上限 8 MiB）"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	res := s.renderer.Render([]byte(req.Content), ro.ID+"/"+rel)
	writeJSON(w, http.StatusOK, map[string]string{"html": res.HTML, "revision": res.Revision})
}

func (s *Server) handleHighlightCSS(w http.ResponseWriter, r *http.Request) {
	theme := r.URL.Query().Get("theme")
	if theme != "dark" {
		theme = "light"
	}
	css := s.renderer.HighlightCSS(theme)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.WriteString(w, css)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/raw/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	ro, fileRel, err := s.resolveRef(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, modTime, err := s.readRootFile(ro, fileRel, maxRemoteRaw)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, fileRel, modTime, bytes.NewReader(data))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFileAtomic(name string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(name); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(name)
	tmp, err := os.CreateTemp(dir, ".mdlive-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, name); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
