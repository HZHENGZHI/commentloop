// Package agentclient is a small client for mdlive's local HTTP API. It is
// used by the agent CLI and the MCP server so coding agents can inspect and
// edit the same local/remote workspace as the mdlive UI.
package agentclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"mdlive/internal/discovery"
)

type Root struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Remote  string      `json:"remote,omitempty"`
	Primary bool        `json:"primary"`
	Files   []FileEntry `json:"files"`
}

type FileEntry struct {
	Path     string    `json:"path"`
	RelPath  string    `json:"relPath"`
	Name     string    `json:"name"`
	Dir      string    `json:"dir"`
	RootID   string    `json:"rootId"`
	RootName string    `json:"rootName"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"modTime"`
}

type Tree struct {
	Roots     []Root      `json:"roots"`
	Files     []FileEntry `json:"files"`
	Truncated bool        `json:"truncated"`
}

type Doc struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	HTML     string    `json:"html"`
	Revision string    `json:"revision"`
	ModTime  time.Time `json:"modTime"`
	Size     int64     `json:"size"`
}

type Config struct {
	Root        string `json:"root"`
	RootName    string `json:"rootName"`
	Version     string `json:"version"`
	PreviewOnly bool   `json:"previewOnly"`
	Debug       bool   `json:"debug"`
	HotReload   bool   `json:"hotReload"`
	RootCount   int    `json:"rootCount"`
}

type Client struct {
	base string
	http *http.Client
}

type Resolved struct {
	Path    string
	RootID  string
	RelPath string
	Exists  bool
	File    *FileEntry
}

// DefaultURL returns explicit URL, MDLIVE_URL, the discovery file, or the
// default mdlive port, in that order.
func DefaultURL(explicit string) string {
	if explicit != "" {
		return normalizeURL(explicit)
	}
	if env := strings.TrimSpace(os.Getenv("MDLIVE_URL")); env != "" {
		return normalizeURL(env)
	}
	if info, err := discovery.Read(); err == nil && info.URL != "" {
		return normalizeURL(info.URL)
	}
	return "http://127.0.0.1:4571"
}

func normalizeURL(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "http://" + value
	}
	return strings.TrimRight(value, "/")
}

func New(baseURL string) *Client {
	return &Client{
		base: normalizeURL(baseURL),
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) BaseURL() string { return c.base }

func (c *Client) Config() (Config, error) {
	var cfg Config
	err := c.getJSON("/api/config", &cfg)
	return cfg, err
}

func (c *Client) Refresh() error {
	return c.postJSON("/api/agent/refresh", nil, &struct{}{})
}

func (c *Client) Tree(refresh bool) (Tree, error) {
	if refresh {
		if err := c.Refresh(); err != nil {
			return Tree{}, err
		}
	}
	var tree Tree
	err := c.getJSON("/api/tree", &tree)
	return tree, err
}

func (c *Client) Read(path string, refresh bool) (Doc, Resolved, error) {
	tree, err := c.Tree(refresh)
	if err != nil {
		return Doc{}, Resolved{}, err
	}
	resolved, err := Resolve(tree, path, false)
	if err != nil {
		return Doc{}, Resolved{}, err
	}
	var doc Doc
	if err := c.getJSON("/api/doc?path="+url.QueryEscape(resolved.Path), &doc); err != nil {
		return Doc{}, resolved, err
	}
	return doc, resolved, nil
}

func (c *Client) Write(path, content string) (Doc, Resolved, error) {
	tree, err := c.Tree(false)
	if err != nil {
		return Doc{}, Resolved{}, err
	}
	resolved, err := Resolve(tree, path, true)
	if err != nil {
		return Doc{}, Resolved{}, err
	}
	var doc Doc
	err = c.putJSON("/api/doc", map[string]string{"path": resolved.Path, "content": content}, &doc)
	if err != nil {
		return Doc{}, resolved, err
	}
	return doc, resolved, nil
}

func (c *Client) Search(query string, refresh bool) ([]FileEntry, error) {
	tree, err := c.Tree(refresh)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]FileEntry, 0)
	for _, file := range tree.Files {
		haystack := strings.ToLower(file.RootName + "/" + file.RelPath)
		if q == "" || strings.Contains(haystack, q) {
			out = append(out, file)
		}
	}
	return out, nil
}

func (c *Client) Changes(since time.Time, refresh bool) ([]FileEntry, error) {
	tree, err := c.Tree(refresh)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0)
	for _, file := range tree.Files {
		if file.ModTime.After(since) {
			out = append(out, file)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// Resolve accepts a canonical path (`<rootID>/<rel>`), a friendly
// `<rootName>/<rel>`, a unique relative path/name, or (when allowNew is true)
// a path for a new file under the only/root directory.
func Resolve(tree Tree, input string, allowNew bool) (Resolved, error) {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "./")
	if input == "" {
		return Resolved{}, errors.New("路径不能为空")
	}
	if strings.Contains(input, "..") {
		for _, part := range strings.Split(input, "/") {
			if part == ".." {
				return Resolved{}, errors.New("路径不能包含 ..")
			}
		}
	}

	for _, file := range tree.Files {
		if file.Path == input {
			f := file
			return Resolved{Path: f.Path, RootID: f.RootID, RelPath: f.RelPath, Exists: true, File: &f}, nil
		}
	}

	// Root ID or root name prefix. This also supports writing new files.
	for _, root := range tree.Roots {
		prefix := root.ID + "/"
		if strings.HasPrefix(input, prefix) {
			rel := strings.TrimPrefix(input, prefix)
			if rel == "" {
				return Resolved{}, errors.New("路径缺少文件名")
			}
			return resolveUnderRoot(tree, root, rel, allowNew)
		}
		if root.Name != "" && strings.HasPrefix(input, root.Name+"/") {
			rel := strings.TrimPrefix(input, root.Name+"/")
			return resolveUnderRoot(tree, root, rel, allowNew)
		}
	}

	// Absolute filesystem / remote path under a root path.
	for _, root := range tree.Roots {
		if root.Path == "" {
			continue
		}
		cleanRoot := strings.TrimRight(root.Path, "/")
		if input == cleanRoot {
			continue
		}
		if strings.HasPrefix(input, cleanRoot+"/") {
			return resolveUnderRoot(tree, root, strings.TrimPrefix(input, cleanRoot+"/"), allowNew)
		}
	}

	var matches []FileEntry
	for _, file := range tree.Files {
		if file.RelPath == input || file.Name == input {
			matches = append(matches, file)
		}
	}
	if len(matches) == 1 {
		f := matches[0]
		return Resolved{Path: f.Path, RootID: f.RootID, RelPath: f.RelPath, Exists: true, File: &f}, nil
	}
	if len(matches) > 1 {
		return Resolved{}, fmt.Errorf("路径 %q 在多个目录中存在，请使用 <目录名>/<相对路径> 或 canonical path", input)
	}

	if allowNew && len(tree.Roots) == 1 {
		return resolveUnderRoot(tree, tree.Roots[0], input, true)
	}
	if allowNew {
		return Resolved{}, fmt.Errorf("找不到 %q；写入新文件请使用 <目录名>/<相对路径>，可选目录：%s", input, rootNames(tree))
	}
	return Resolved{}, fmt.Errorf("找不到文件 %q；可用 mdlive agent list 查看路径", input)
}

func resolveUnderRoot(tree Tree, root Root, rel string, allowNew bool) (Resolved, error) {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return Resolved{}, errors.New("路径缺少文件名")
	}
	canonical := root.ID + "/" + rel
	for _, file := range tree.Files {
		if file.Path == canonical {
			f := file
			return Resolved{Path: canonical, RootID: root.ID, RelPath: rel, Exists: true, File: &f}, nil
		}
	}
	if !allowNew {
		return Resolved{}, fmt.Errorf("文件不存在：%s", canonical)
	}
	return Resolved{Path: canonical, RootID: root.ID, RelPath: rel}, nil
}

func rootNames(tree Tree) string {
	names := make([]string, 0, len(tree.Roots))
	for _, root := range tree.Roots {
		names = append(names, root.Name)
	}
	return strings.Join(names, ", ")
}

func (c *Client) getJSON(path string, out any) error {
	res, err := c.http.Get(c.base + path)
	if err != nil {
		return fmt.Errorf("无法连接 mdlive 服务 %s：%w（请先启动 ./bin/mdlive）", c.base, err)
	}
	defer res.Body.Close()
	return decodeResponse(res, out)
}

func (c *Client) postJSON(path string, body, out any) error {
	return c.sendJSON(http.MethodPost, path, body, out)
}

func (c *Client) putJSON(path string, body, out any) error {
	return c.sendJSON(http.MethodPut, path, body, out)
}

func (c *Client) sendJSON(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Mark requests made by the Agent CLI/MCP. In preview/review mode the
	// frontend stays read-only while the background agent may still write.
	req.Header.Set("X-Mdlive-Agent", "1")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接 mdlive 服务 %s：%w", c.base, err)
	}
	defer res.Body.Close()
	if out == nil {
		out = &struct{}{}
	}
	return decodeResponse(res, out)
}

func decodeResponse(res *http.Response, out any) error {
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &payload)
		if payload.Error != "" {
			return fmt.Errorf("mdlive 返回错误：%s", payload.Error)
		}
		return fmt.Errorf("mdlive 返回 HTTP %d", res.StatusCode)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 mdlive 响应失败：%w", err)
	}
	return nil
}

type CommentMessage struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	Before    string    `json:"before,omitempty"`
	After     string    `json:"after,omitempty"`
	VersionID string    `json:"versionId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Comment struct {
	ID               string           `json:"id"`
	Path             string           `json:"path"`
	Quote            string           `json:"quote"`
	GroupID          string           `json:"groupId,omitempty"`
	Line             int              `json:"line"`
	Resolved         bool             `json:"resolved"`
	Orphaned         bool             `json:"orphaned"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	DiscussionIDs    []string         `json:"discussionIds,omitempty"`
	PromptTemplateID string           `json:"promptTemplateId,omitempty"`
	Messages         []CommentMessage `json:"messages"`
}

func (c *Client) ListComments(path string) ([]Comment, error) {
	target := "/api/comments"
	if path != "" {
		target += "?path=" + url.QueryEscape(path)
	}
	var response struct {
		Comments []Comment `json:"comments"`
	}
	if err := c.getJSON(target, &response); err != nil {
		return nil, err
	}
	if response.Comments == nil {
		response.Comments = []Comment{}
	}
	return response.Comments, nil
}

func (c *Client) ReplyComment(path, id, body, before, after, beforeSnapshot, snapshot string) (Comment, error) {
	var comment Comment
	err := c.postJSON("/api/comments/reply", map[string]any{
		"path": path, "id": id, "author": "agent", "body": body,
		"before": before, "after": after,
		"beforeSnapshot": beforeSnapshot, "snapshot": snapshot,
	}, &comment)
	return comment, err
}

type CodeRepo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Branch  string   `json:"branch,omitempty"`
	Head    string   `json:"head,omitempty"`
	Dirty   bool     `json:"dirty,omitempty"`
	Changed []string `json:"changed,omitempty"`
	Linked  bool     `json:"linked,omitempty"`
}

type CodeFileEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

type CodeFileList struct {
	Files     []CodeFileEntry `json:"files"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated"`
}

type CodeReadResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	TotalLines int    `json:"totalLines"`
	Size       int64  `json:"size"`
	Truncated  bool   `json:"truncated"`
}

type CodeMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type CodeSearchResult struct {
	Matches   []CodeMatch `json:"matches"`
	Scanned   int         `json:"scanned"`
	Truncated bool        `json:"truncated"`
}

func (c *Client) DocCodeRepos(path string) ([]CodeRepo, error) {
	var response struct {
		Repos []CodeRepo `json:"repos"`
	}
	if err := c.getJSON("/api/doc/code-repos?path="+url.QueryEscape(path), &response); err != nil {
		return nil, err
	}
	if response.Repos == nil {
		response.Repos = []CodeRepo{}
	}
	return response.Repos, nil
}

func (c *Client) CodeRepos() ([]CodeRepo, error) {
	var response struct {
		Repos []CodeRepo `json:"repos"`
	}
	if err := c.getJSON("/api/code/repos", &response); err != nil {
		return nil, err
	}
	if response.Repos == nil {
		response.Repos = []CodeRepo{}
	}
	return response.Repos, nil
}

func (c *Client) CodeInfo(repo string) (CodeRepo, error) {
	var out CodeRepo
	err := c.getJSON("/api/code/info?repo="+url.QueryEscape(repo), &out)
	return out, err
}

func (c *Client) CodeFiles(repo, query string, limit int) (CodeFileList, error) {
	var out CodeFileList
	params := url.Values{}
	params.Set("repo", repo)
	params.Set("query", query)
	if limit > 0 {
		params.Set("limit", fmt.Sprint(limit))
	}
	err := c.getJSON("/api/code/files?"+params.Encode(), &out)
	return out, err
}

func (c *Client) CodeRead(repo, path string, start, end int) (CodeReadResult, error) {
	var out CodeReadResult
	params := url.Values{}
	params.Set("repo", repo)
	params.Set("path", path)
	if start > 0 {
		params.Set("start", fmt.Sprint(start))
	}
	if end > 0 {
		params.Set("end", fmt.Sprint(end))
	}
	err := c.getJSON("/api/code/file?"+params.Encode(), &out)
	return out, err
}

func (c *Client) CodeSearch(repo, query string, limit int) (CodeSearchResult, error) {
	var out CodeSearchResult
	params := url.Values{}
	params.Set("repo", repo)
	params.Set("q", query)
	if limit > 0 {
		params.Set("limit", fmt.Sprint(limit))
	}
	err := c.getJSON("/api/code/search?"+params.Encode(), &out)
	return out, err
}
