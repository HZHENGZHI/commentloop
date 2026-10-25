// Package lsp runs a language server over stdio and exposes the small subset
// needed for go-to-definition from the code viewer.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Location struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`      // 1-based
	Character int    `json:"character"` // 1-based
	External  bool   `json:"external,omitempty"`
}

type Manager struct {
	mu      sync.Mutex
	clients map[string]*client
}

func NewManager() *Manager {
	return &Manager{clients: make(map[string]*client)}
}

func (m *Manager) Close() {
	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]*client)
	m.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
}

type ServerConfig struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Extensions []string `json:"extensions"`
	LanguageID string   `json:"languageId"`
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	Enabled    bool     `json:"enabled"`
}

func DefaultServers() []ServerConfig {
	return []ServerConfig{
		{ID: "go", Label: "Go", Extensions: []string{".go"}, LanguageID: "go", Command: "gopls", Args: []string{}, Enabled: true},
		{ID: "typescript", Label: "TypeScript", Extensions: []string{".ts"}, LanguageID: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}, Enabled: true},
		{ID: "typescriptreact", Label: "TypeScript React", Extensions: []string{".tsx"}, LanguageID: "typescriptreact", Command: "typescript-language-server", Args: []string{"--stdio"}, Enabled: true},
		{ID: "javascript", Label: "JavaScript", Extensions: []string{".js", ".mjs", ".cjs"}, LanguageID: "javascript", Command: "typescript-language-server", Args: []string{"--stdio"}, Enabled: true},
		{ID: "javascriptreact", Label: "JavaScript React", Extensions: []string{".jsx"}, LanguageID: "javascriptreact", Command: "typescript-language-server", Args: []string{"--stdio"}, Enabled: true},
		{ID: "python", Label: "Python", Extensions: []string{".py"}, LanguageID: "python", Command: "pyright-langserver", Args: []string{"--stdio"}, Enabled: true},
		{ID: "rust", Label: "Rust", Extensions: []string{".rs"}, LanguageID: "rust", Command: "rust-analyzer", Args: []string{}, Enabled: true},
	}
}

func MatchServer(configs []ServerConfig, path string) (ServerConfig, error) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, config := range configs {
		if !config.Enabled {
			continue
		}
		for _, candidate := range config.Extensions {
			if strings.EqualFold(candidate, ext) {
				return config, nil
			}
		}
	}
	return ServerConfig{}, fmt.Errorf("当前文件类型没有启用 LSP 语言服务器")
}

func FindCommand(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "go", "bin", name),
		filepath.Join(home, ".local", "bin", name),
		filepath.Join("/opt/homebrew/bin", name),
		filepath.Join("/usr/local/bin", name),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("未找到 %s", name)
}

func (m *Manager) Definition(ctx context.Context, repo, relPath, content string, line, character int, config ServerConfig) ([]Location, error) {
	if strings.TrimSpace(config.Command) == "" {
		return nil, fmt.Errorf("LSP 服务器命令为空")
	}
	key := repo + "\x00" + config.ID
	var err error
	m.mu.Lock()
	c := m.clients[key]
	if c != nil && !c.alive() {
		delete(m.clients, key)
		c = nil
	}
	if c == nil {
		c, err = startClient(repo, config)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		m.clients[key] = c
	}
	m.mu.Unlock()
	return c.definition(ctx, relPath, content, line, character)
}

func (m *Manager) Reset() {
	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]*client)
	m.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    *rpcError
}

type client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	writeMu   sync.Mutex
	requestMu sync.Mutex
	pendingMu sync.Mutex
	pending   map[int]chan response
	nextID    int
	closed    chan struct{}
	closeOnce sync.Once
	root      string
	spec      ServerConfig
	open      map[string]openFile
}

type openFile struct {
	version int
	content string
}

func startClient(root string, spec ServerConfig) (*client, error) {
	command, err := FindCommand(spec.Command)
	if err != nil {
		return nil, fmt.Errorf("未找到 %s 语言服务器（%s）。请先安装并确保在 PATH 中", spec.Label, spec.Command)
	}
	cmd := exec.Command(command, spec.Args...)
	cmd.Dir = root
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	c := &client{
		cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout),
		pending: make(map[int]chan response), closed: make(chan struct{}),
		root: root, spec: spec, open: make(map[string]openFile),
	}
	go c.readLoop()
	initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	params := map[string]any{
		"processId":  os.Getpid(),
		"clientInfo": map[string]any{"name": "commentloop"},
		"rootUri":    fileURI(root),
		"rootPath":   root,
		"workspaceFolders": []map[string]string{{
			"uri": fileURI(root), "name": filepath.Base(root),
		}},
		"capabilities": map[string]any{
			"workspace": map[string]any{"workspaceFolders": true, "configuration": true},
			"textDocument": map[string]any{
				"definition":      map[string]any{"linkSupport": true},
				"synchronization": map[string]any{"dynamicRegistration": false},
			},
		},
	}
	if _, err := c.call(initCtx, "initialize", params); err != nil {
		c.close()
		return nil, fmt.Errorf("初始化 %s 失败：%w", spec.Label, err)
	}
	_ = c.notify("initialized", map[string]any{})
	return c, nil
}

func (c *client) alive() bool {
	select {
	case <-c.closed:
		return false
	default:
		return true
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.pendingMu.Lock()
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		c.pendingMu.Unlock()
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
	})
}

func (c *client) readLoop() {
	defer c.close()
	for {
		body, err := readFrame(c.stdout)
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			c.respond(msg.ID, msg.Method, msg.Params)
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		var id int
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.pendingMu.Unlock()
		if ch != nil {
			ch <- response{result: msg.Result, err: msg.Error}
			close(ch)
		}
	}
}

func (c *client) respond(id json.RawMessage, method string, params json.RawMessage) {
	var result any
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		result = make([]any, len(p.Items))
	case "workspace/workspaceFolders":
		result = []map[string]string{{"uri": fileURI(c.root), "name": filepath.Base(c.root)}}
	case "workspace/applyEdit":
		result = map[string]any{"applied": false}
	default:
		result = nil
	}
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_ = c.writeFrame(data)
}

func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	select {
	case <-c.closed:
		return nil, fmt.Errorf("语言服务器已退出")
	default:
	}
	c.writeMu.Lock()
	c.nextID++
	id := c.nextID
	c.writeMu.Unlock()
	ch := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err := c.writeFrame(data); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}
	select {
	case res, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("语言服务器已退出")
		}
		if res.err != nil {
			return nil, fmt.Errorf("%s", res.err.Message)
		}
		return res.result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *client) notify(method string, params any) error {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	return c.writeFrame(data)
}

func (c *client) writeFrame(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err := c.stdin.Write(data)
	return err
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("invalid LSP frame")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func (c *client) definition(ctx context.Context, relPath, content string, line, character int) ([]Location, error) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	select {
	case <-c.closed:
		return nil, fmt.Errorf("语言服务器已退出")
	default:
	}
	abs := filepath.Join(c.root, filepath.FromSlash(relPath))
	uri := fileURI(abs)
	version := 1
	if current, ok := c.open[uri]; ok {
		version = current.version
		if current.content != content {
			version++
			c.open[uri] = openFile{version: version, content: content}
			if err := c.notify("textDocument/didChange", map[string]any{
				"textDocument":   map[string]any{"uri": uri, "version": version},
				"contentChanges": []map[string]string{{"text": content}},
			}); err != nil {
				return nil, err
			}
		}
	} else {
		c.open[uri] = openFile{version: version, content: content}
		if err := c.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": c.spec.LanguageID, "version": version, "text": content,
			},
		}); err != nil {
			return nil, err
		}
	}
	if line < 1 {
		line = 1
	}
	if character < 1 {
		character = 1
	}
	params := map[string]any{
		"textDocument": map[string]string{"uri": uri},
		"position":     map[string]int{"line": line - 1, "character": character - 1},
	}
	var result json.RawMessage
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		result, err = c.call(ctx, "textDocument/definition", params)
		if err != nil {
			return nil, err
		}
		if len(result) > 0 && string(result) != "null" && string(result) != "[]" {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return c.parseLocations(result)
}

func (c *client) parseLocations(raw json.RawMessage) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []Location{}, nil
	}
	items := make([]json.RawMessage, 0, 1)
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	} else {
		items = append(items, raw)
	}
	out := make([]Location, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		var loc struct {
			URI                  string   `json:"uri"`
			Range                lspRange `json:"range"`
			TargetURI            string   `json:"targetUri"`
			TargetRange          lspRange `json:"targetRange"`
			TargetSelectionRange lspRange `json:"targetSelectionRange"`
		}
		if err := json.Unmarshal(item, &loc); err != nil {
			continue
		}
		uri := loc.TargetURI
		if uri == "" {
			uri = loc.URI
		}
		r := loc.TargetSelectionRange
		if r.Start.Line == 0 && r.Start.Character == 0 && r.End.Line == 0 && r.End.Character == 0 {
			r = loc.TargetRange
		}
		if r.Start.Line == 0 && r.Start.Character == 0 && r.End.Line == 0 && r.End.Character == 0 {
			r = loc.Range
		}
		parsed, err := url.Parse(uri)
		if err != nil || parsed.Scheme != "file" {
			continue
		}
		path := filepath.FromSlash(parsed.Path)
		rel, err := filepath.Rel(c.root, path)
		external := false
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			external = true
			rel = path
		} else {
			rel = filepath.ToSlash(rel)
		}
		location := Location{Path: rel, Line: r.Start.Line + 1, Character: r.Start.Character + 1, External: external}
		key := fmt.Sprintf("%s:%d:%d", location.Path, location.Line, location.Character)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, location)
	}
	return out, nil
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
