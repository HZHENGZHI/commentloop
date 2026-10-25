package server

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const assetPollInterval = 400 * time.Millisecond

// configureDiskAssets enables serving the frontend from a directory on disk.
// This is what makes `--debug` useful: rebuilding web/dist updates the app
// without restarting the Go process.
func (s *Server) configureDiskAssets(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		log.Printf("commentloop: debug: invalid web dir %q: %v", dir, err)
		return
	}
	index := filepath.Join(abs, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		log.Printf("commentloop: debug: %s 不存在，回退到内嵌前端（热更新不可用）", index)
		return
	}
	s.webDir = abs
	log.Printf("commentloop: debug: 从磁盘加载前端 %s", abs)
}

// startAssetPoll compares index.html between ticks. Vite writes index.html
// last, so a change means a rebuild is complete and browsers can reload.
func (s *Server) startAssetPoll() {
	if s.webDir == "" {
		return
	}
	s.assetDone = make(chan struct{})
	done := s.assetDone
	s.assetWG.Add(1)
	go func() {
		defer s.assetWG.Done()
		ticker := time.NewTicker(assetPollInterval)
		defer ticker.Stop()

		last := s.assetSignature()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sig := s.assetSignature()
				if sig == "" {
					// index.html is temporarily missing (e.g. a manual clean).
					continue
				}
				if last != "" && sig != last {
					log.Printf("commentloop: debug: 前端资源已更新，通知浏览器刷新")
					s.hub.broadcast(outMessage{Type: "reload", Message: "frontend assets changed"})
				}
				last = sig
			}
		}
	}()
}

func (s *Server) assetSignature() string {
	data, err := os.ReadFile(filepath.Join(s.webDir, "index.html"))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x:%d", sum[:8], len(data))
}

func (s *Server) stopAssetPoll() {
	if s.assetDone == nil {
		return
	}
	close(s.assetDone)
	s.assetDone = nil
	s.assetWG.Wait()
}

// readStatic reads a static asset, preferring the debug directory and falling
// back to the embedded build.
func (s *Server) readStatic(name string) ([]byte, time.Time, bool) {
	if s.webDir != "" && fs.ValidPath(name) {
		full := filepath.Join(s.webDir, filepath.FromSlash(name))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			if data, err := os.ReadFile(full); err == nil {
				return data, info.ModTime(), true
			}
		}
	}
	data, err := fs.ReadFile(s.web, name)
	if err != nil {
		return nil, time.Time{}, false
	}
	return data, time.Time{}, true
}

func (s *Server) staticIsDir(name string) bool {
	if s.webDir == "" || !fs.ValidPath(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(s.webDir, filepath.FromSlash(name)))
	return err == nil && info.IsDir()
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "." {
		p = "index.html"
	}
	if strings.HasPrefix(p, "../") || !fs.ValidPath(p) {
		http.NotFound(w, r)
		return
	}

	data, modTime, ok := s.readStatic(p)
	if !ok || s.staticIsDir(p) {
		// Missing assets stay 404s; only extension-less SPA routes fall back.
		if strings.HasPrefix(p, "assets/") || path.Ext(p) != "" || s.staticIsDir(p) {
			http.NotFound(w, r)
			return
		}
		p = "index.html"
		data, modTime, ok = s.readStatic(p)
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case s.debug:
		w.Header().Set("Cache-Control", "no-store")
	case strings.HasPrefix(p, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, p, modTime, bytes.NewReader(data))
}

// rejectReadOnly blocks frontend document writes when commentloop runs in review
// mode. Requests marked by the Agent CLI/MCP are allowed through.
func (s *Server) rejectReadOnly(w http.ResponseWriter, r *http.Request) bool {
	if !s.previewOnly {
		return false
	}
	if r != nil && r.Header.Get("X-Commentloop-Agent") == "1" {
		return false
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "审阅模式下前台不能修改文件；后台 Agent 使用 MCP/CLI 写入"})
	return true
}
