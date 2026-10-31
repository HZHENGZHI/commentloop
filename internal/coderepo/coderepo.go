// Package coderepo provides read-only access to user-linked code repositories.
package coderepo

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MaxFileSize    = 1 << 20 // 1 MiB per file for read/search
	MaxFiles       = 5000
	MaxReadLines   = 2000
	MaxSearchBytes = 64 << 20
)

type Info struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Branch  string   `json:"branch,omitempty"`
	Head    string   `json:"head,omitempty"`
	Dirty   bool     `json:"dirty"`
	Changed []string `json:"changed,omitempty"`
}

type Entry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

type ListResult struct {
	Files     []Entry `json:"files"`
	Total     int     `json:"total"`
	Truncated bool    `json:"truncated"`
}

type ReadResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	TotalLines int    `json:"totalLines"`
	Size       int64  `json:"size"`
	Truncated  bool   `json:"truncated"`
}

type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchResult struct {
	Matches   []Match `json:"matches"`
	Scanned   int     `json:"scanned"`
	Truncated bool    `json:"truncated"`
}

var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	"dist": true, "build": true, "target": true, ".next": true,
	".nuxt": true, ".venv": true, "venv": true, "__pycache__": true,
	".idea": true, ".vscode": true, ".gradle": true, ".cache": true,
	"coverage": true,
}

var codeExtensions = map[string]bool{
	".md": true, ".markdown": true, ".txt": true,
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".mjs": true, ".cjs": true, ".vue": true, ".svelte": true,
	".py": true, ".rs": true, ".java": true, ".kt": true, ".kts": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".cs": true, ".rb": true, ".php": true, ".swift": true, ".m": true, ".mm": true,
	".scala": true, ".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".sql": true, ".proto": true, ".graphql": true, ".gql": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".conf": true, ".xml": true, ".html": true, ".css": true, ".scss": true,
	".less": true, ".gradle": true, ".properties": true, ".env": true,
}

var codeNames = map[string]bool{
	"Dockerfile": true, "Makefile": true, "makefile": true, "go.mod": true,
	"go.sum": true, "package.json": true, "Cargo.toml": true, "Cargo.lock": true,
	"pom.xml": true, "build.gradle": true, "settings.gradle": true,
	"requirements.txt": true, "Gemfile": true, "Gemfile.lock": true,
	".gitignore": true, ".dockerignore": true, "README": true,
}

// Normalize validates a user-selected code directory and resolves symlinks.
func Normalize(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("代码仓库路径不能为空")
	}
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		raw = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(raw, "~"), "/"))
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("路径不是目录")
	}
	name := filepath.Base(resolved)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = resolved
	}
	return resolved, name, nil
}

func GitInfo(path string) Info {
	info := Info{Path: path, Name: filepath.Base(path)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run := func(args ...string) string {
		cmdArgs := append([]string{"-C", path}, args...)
		out, err := exec.CommandContext(ctx, "git", cmdArgs...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	info.Branch = run("rev-parse", "--abbrev-ref", "HEAD")
	info.Head = run("rev-parse", "--short", "HEAD")
	status := run("status", "--porcelain")
	if status != "" {
		for _, line := range strings.Split(status, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if len(line) > 160 {
				line = line[:160]
			}
			info.Changed = append(info.Changed, line)
			if len(info.Changed) >= 20 {
				break
			}
		}
		info.Dirty = true
	}
	return info
}

func isCodeFile(name string) bool {
	if codeNames[name] {
		return true
	}
	return codeExtensions[strings.ToLower(filepath.Ext(name))]
}

func walkFiles(root string, fn func(rel string, full string, info fs.FileInfo) bool) error {
	return filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if full == root {
				return nil
			}
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || entry.Type()&os.ModeSymlink != 0 || !isCodeFile(name) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return nil
		}
		if !fn(filepath.ToSlash(rel), full, info) {
			return fs.SkipAll
		}
		return nil
	})
}

func List(root, query string, limit int) (ListResult, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > MaxFiles {
		limit = MaxFiles
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := ListResult{Files: make([]Entry, 0, 64)}
	err := walkFiles(root, func(rel, full string, info fs.FileInfo) bool {
		if query != "" && !strings.Contains(strings.ToLower(rel), query) {
			return true
		}
		out.Total++
		if len(out.Files) >= limit {
			out.Truncated = true
			return false
		}
		out.Files = append(out.Files, Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		return true
	})
	sort.Slice(out.Files, func(i, j int) bool {
		return strings.ToLower(out.Files[i].Path) < strings.ToLower(out.Files[j].Path)
	})
	return out, err
}

func resolveFile(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("代码文件路径不能为空")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("代码文件路径必须位于仓库内")
	}
	full := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(root, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("代码文件路径必须位于仓库内")
	}
	return resolved, nil
}

func ReadFull(root, rel string) (string, error) {
	full, err := resolveFile(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("不是普通文件")
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("代码文件超过 1 MiB")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("二进制文件不支持读取")
	}
	return string(data), nil
}

func Read(root, rel string, startLine, endLine int) (ReadResult, error) {
	full, err := resolveFile(root, rel)
	if err != nil {
		return ReadResult{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return ReadResult{}, err
	}
	if !info.Mode().IsRegular() {
		return ReadResult{}, fmt.Errorf("不是普通文件")
	}
	if info.Size() > MaxFileSize {
		return ReadResult{}, fmt.Errorf("代码文件超过 1 MiB，请使用搜索定位或分段读取")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ReadResult{}, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return ReadResult{}, fmt.Errorf("二进制文件不支持读取")
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)
	if total > 0 && lines[total-1] == "" {
		total--
	}
	if total == 0 {
		total = 1
	}
	if startLine <= 0 {
		startLine = 1
	}
	explicitEnd := endLine > 0
	if endLine <= 0 || endLine > total {
		endLine = total
	}
	if startLine > total {
		startLine = total
	}
	if endLine < startLine {
		endLine = startLine
	}
	if explicitEnd && endLine-startLine+1 > MaxReadLines {
		endLine = startLine + MaxReadLines - 1
	}
	content := strings.Join(lines[startLine-1:min(endLine, len(lines))], "\n")
	result := ReadResult{
		Path: filepath.ToSlash(rel), Content: content,
		StartLine: startLine, EndLine: endLine, TotalLines: total, Size: info.Size(),
		Truncated: endLine < total,
	}
	return result, nil
}

func Search(root, query string, limit int) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResult{}, fmt.Errorf("搜索内容不能为空")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	needle := strings.ToLower(query)
	out := SearchResult{Matches: make([]Match, 0, 16)}
	var scanned int64
	err := walkFiles(root, func(rel, full string, info fs.FileInfo) bool {
		if info.Size() > MaxFileSize {
			return true
		}
		if scanned+info.Size() > MaxSearchBytes {
			out.Truncated = true
			return false
		}
		data, err := os.ReadFile(full)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return true
		}
		scanned += info.Size()
		out.Scanned++
		for i, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			text := strings.TrimSpace(line)
			if len([]rune(text)) > 300 {
				text = string([]rune(text)[:300])
			}
			out.Matches = append(out.Matches, Match{Path: rel, Line: i + 1, Text: text})
			if len(out.Matches) >= limit {
				out.Truncated = true
				return false
			}
		}
		return true
	})
	return out, err
}
