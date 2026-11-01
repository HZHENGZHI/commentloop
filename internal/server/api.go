package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hzhengzhi/commentloop/internal/atomicfile"

	"github.com/hzhengzhi/commentloop/internal/ignore"
)

const (
	maxDocSize = 8 << 20 // 8 MiB per document / websocket edit
)

var errBadPath = errors.New("invalid path")

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
				files, truncated, err := s.localTrees.get(ro, s.listRootFiles)
				if s.currentRoot(ro.ID) != ro {
					s.localTrees.forget(ro)
				}
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
		if len(entry) >= s.cfg.Limits.TreeFiles {
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
			writeError(w, http.StatusNotFound, errFileNotFound)
			return
		}
		writeErrorDetail(w, http.StatusBadRequest, errCouldNotReadFileDetail, err.Error())
		return
	}
	if s.rejectAgentDocPath(w, r, snap.path, "当前任务只能读取关联文档") {
		return
	}
	writeJSON(w, http.StatusOK, docResponse{
		Path: snap.path, Content: snap.content, HTML: snap.result.HTML,
		Revision: snap.result.Revision, ModTime: snap.modTime, Size: snap.size,
	})
}

func (s *Server) handleSaveDoc(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentWrite(w, r) {
		return
	}
	create := r.Method == http.MethodPost
	if _, known := s.agentGrantForRequest(r); known && create {
		message := "受管 Agent 任务不能新建文档"
		s.recordAgentDenial(r, message)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
		return
	}
	// Review mode still allows managing the document list: creating a new
	// document and importing files are frontend actions. Editing an existing
	// document (PUT) remains Agent-only.
	if !create && s.rejectReadOnly(w, r) {
		return
	}

	var req saveRequest
	if err := readJSON(w, r, maxDocSize+64*1024, &req); err != nil {
		if isBodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, errDocumentTooLarge)
			return
		}
		writeError(w, http.StatusBadRequest, errInvalidRequest)
		return
	}
	if len(req.Content) > maxDocSize {
		writeError(w, http.StatusRequestEntityTooLarge, errDocumentTooLarge)
		return
	}

	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能修改关联文档") {
		return
	}

	_, statErr := s.statRootFile(ro, rel)
	existed := statErr == nil
	if create && existed {
		writeError(w, http.StatusConflict, errFileNameAlreadyExists)
		return
	}
	if statErr != nil && ro.Kind == rootKindLocal && !os.IsNotExist(statErr) {
		writeError(w, http.StatusInternalServerError, errCouldNotAccessFile)
		return
	}
	doc, err := s.persistDocument(ro, rel, canonical, req.Content, "save", existed)
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errFailedWriteFile, err.Error())
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
	s.saveDocumentVersion(canonical, content, origin == "restore")
	if !existed {
		// A path can be re-created after its document was deleted. Clearing a
		// stale review row only after a successful write closes the window
		// where a failed delete cleanup would hand the old status to the new
		// document, without erasing metadata if the write fails.
		if err := s.reviews.clear(canonical); err != nil {
			slog.Warn("failed to clear review status for new document", "path", canonical, "err", err)
		}
	}
	res := s.renderer.Render([]byte(content), canonical)
	s.mu.Lock()
	previousRevision := s.diskHash[canonical]
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
	if previousRevision != res.Revision {
		s.noteReviewDocChanged(canonical)
	}

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
	if s.rejectAgentWrite(w, r) {
		return
	}
	if _, known := s.agentGrantForRequest(r); known {
		message := "受管 Agent 任务不能删除文档"
		s.recordAgentDenial(r, message)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
		return
	}
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.removeRootFile(ro, rel); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errFileNotFound)
			return
		}
		writeErrorDetail(w, http.StatusInternalServerError, errDeleteFailed, err.Error())
		return
	}
	s.mu.Lock()
	delete(s.diskHash, canonical)
	s.mu.Unlock()
	if ro.Kind == rootKindRemote {
		s.removeRemoteTreeEntry(ro, rel)
	}
	if err := s.reviews.clear(canonical); err != nil {
		slog.Warn("failed to clear review status for deleted document", "path", canonical, "err", err)
	}
	if err := s.comments.purgeVersions(canonical); err != nil {
		slog.Warn("failed to purge document versions", "path", canonical, "err", err)
	}
	s.hub.broadcast(outMessage{Type: "tree"})
	s.hub.broadcastPath(canonical, outMessage{Type: "gone", Path: canonical})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	var req saveRequest
	if !bindJSON(w, r, maxDocSize+64*1024, &req) {
		return
	}
	if len(req.Content) > maxDocSize {
		writeError(w, http.StatusRequestEntityTooLarge, errDocumentTooLarge)
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
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
	return atomicfile.WriteFile(name, data, mode)
}
