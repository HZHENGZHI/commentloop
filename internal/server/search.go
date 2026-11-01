package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type searchResult struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	RelPath  string `json:"relPath"`
	RootName string `json:"rootName"`
	Line     int    `json:"line"`
	Snippet  string `json:"snippet"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"query": "", "results": []searchResult{}})
		return
	}
	needle := strings.ToLower(query)
	results := make([]searchResult, 0, 20)
	for _, root := range s.rootsSnapshot() {
		if root.Kind != rootKindLocal {
			continue
		}
		_ = filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || len(results) >= 50 {
				return nil
			}
			name := entry.Name()
			if entry.IsDir() {
				if path != root.Path && (name == ".git" || name == "node_modules" || name == ".obsidian" || strings.HasPrefix(name, ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !isMarkdownName(name) {
				return nil
			}
			info, statErr := entry.Info()
			if statErr != nil || info.Size() > 2*1024*1024 {
				return nil
			}
			rel, relErr := filepath.Rel(root.Path, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			canonical := root.ID + "/" + rel
			if strings.Contains(strings.ToLower(rel), needle) {
				results = append(results, searchResult{
					Path: canonical, Name: name, RelPath: rel, RootName: root.Name,
					Line: 0, Snippet: rel,
				})
				if len(results) >= 50 {
					return filepath.SkipAll
				}
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			for index, line := range strings.Split(string(data), "\n") {
				if !strings.Contains(strings.ToLower(line), needle) {
					continue
				}
				snippet := strings.TrimSpace(line)
				if len(snippet) > 240 {
					snippet = snippet[:240] + "…"
				}
				results = append(results, searchResult{
					Path: canonical, Name: name, RelPath: rel, RootName: root.Name,
					Line: index + 1, Snippet: snippet,
				})
				if len(results) >= 50 {
					break
				}
			}
			return nil
		})
		if len(results) >= 50 {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": query, "results": results})
}

func isMarkdownName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}
