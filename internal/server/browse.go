package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxBrowseEntries = 2000

type browseEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Hidden bool   `json:"hidden"`
}

type browseShortcut struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type browseResponse struct {
	Path      string           `json:"path"`
	Parent    string           `json:"parent"`
	Entries   []browseEntry    `json:"entries"`
	Shortcuts []browseShortcut `json:"shortcuts"`
	Truncated bool             `json:"truncated"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	dir, err := browseDirectory(r.URL.Query().Get("path"))
	if err != nil {
		status := http.StatusBadRequest
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		} else if os.IsPermission(err) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": browseError(err)})
		return
	}

	showHidden := r.URL.Query().Get("hidden") == "1"
	entries, truncated, err := listDirectories(dir, showHidden)
	if err != nil {
		status := http.StatusInternalServerError
		if os.IsPermission(err) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]string{"error": browseError(err)})
		return
	}

	parent := ""
	if dir != filepath.Dir(dir) {
		parent = filepath.Dir(dir)
	}
	writeJSON(w, http.StatusOK, browseResponse{
		Path:      dir,
		Parent:    parent,
		Entries:   entries,
		Shortcuts: browseShortcuts(),
		Truncated: truncated,
	})
}

func browseDirectory(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			raw = home
		} else {
			raw = string(filepath.Separator)
		}
	}
	expanded, err := expandHome(raw)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errNotDirectory
	}
	return abs, nil
}

func listDirectories(dir string, showHidden bool) ([]browseEntry, bool, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}
	entries := make([]browseEntry, 0, len(items))
	truncated := false
	for _, item := range items {
		name := item.Name()
		hidden := strings.HasPrefix(name, ".")
		if hidden && !showHidden {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		entries = append(entries, browseEntry{Name: name, Path: full, Hidden: hidden})
		if len(entries) >= maxBrowseEntries {
			truncated = true
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a == b {
			return entries[i].Name < entries[j].Name
		}
		return a < b
	})
	return entries, truncated, nil
}

func browseShortcuts() []browseShortcut {
	home, _ := os.UserHomeDir()
	candidates := []browseShortcut{
		{Name: "根目录", Path: string(filepath.Separator)},
		{Name: "外接磁盘", Path: "/Volumes"},
		{Name: "挂载点", Path: "/mnt"},
		{Name: "媒体", Path: "/media"},
	}
	if home != "" {
		candidates = append([]browseShortcut{
			{Name: "主目录", Path: home},
			{Name: "桌面", Path: filepath.Join(home, "Desktop")},
			{Name: "文稿", Path: filepath.Join(home, "Documents")},
			{Name: "下载", Path: filepath.Join(home, "Downloads")},
		}, candidates...)
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]browseShortcut, 0, len(candidates))
	for _, item := range candidates {
		if item.Path == "" {
			continue
		}
		info, err := os.Stat(item.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, ok := seen[item.Path]; ok {
			continue
		}
		seen[item.Path] = struct{}{}
		out = append(out, item)
	}
	return out
}

var errNotDirectory = &browsePathError{message: "路径不是目录"}

type browsePathError struct{ message string }

func (e *browsePathError) Error() string { return e.message }

func browseError(err error) string {
	switch {
	case err == nil:
		return ""
	case os.IsNotExist(err):
		return "目录不存在"
	case os.IsPermission(err):
		return "没有权限访问该目录"
	default:
		if err == errNotDirectory {
			return err.Error()
		}
		return "无法读取目录：" + err.Error()
	}
}
