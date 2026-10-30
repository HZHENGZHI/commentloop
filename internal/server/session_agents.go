package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type agentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Supported bool   `json:"supported"`
	Count     int    `json:"count"`
}

type sessionRoot struct {
	Path       string
	Agent      string
	SourceKind string
}

type sessionLocation struct {
	Path       string
	Agent      string
	SourceKind string
}

var sessionAgentCatalog = []struct {
	ID        string
	Name      string
	Command   string
	Supported bool
}{
	{"pi", "Pi", "pi", true},
	{"claude", "Claude Code", "claude", true},
	{"codex", "Codex", "codex", true},
	{"cursor", "Cursor", "cursor-agent", true},
	{"gemini", "Gemini CLI", "gemini", false},
	{"opencode", "OpenCode", "opencode", false},
	{"crush", "Crush", "crush", false},
	{"goose", "Goose", "goose", false},
	{"droid", "Droid", "droid", false},
	{"hermes", "Hermes", "hermes", false},
}

func sessionAgentName(id string) string {
	for _, item := range sessionAgentCatalog {
		if item.ID == id {
			return item.Name
		}
	}
	if id == "" {
		return "Pi"
	}
	return id
}

func (s *Server) sessionRootSpecs() []sessionRoot {
	var roots []sessionRoot
	add := func(path, agent, sourceKind string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		roots = append(roots, sessionRoot{Path: path, Agent: agent, SourceKind: sourceKind})
	}
	home, _ := os.UserHomeDir()
	if s.stateDir != "" {
		add(filepath.Join(s.stateDir, "pi-sessions"), "pi", "commentloop")
	}
	piHome := os.Getenv("PI_CODING_AGENT_DIR")
	if piHome == "" {
		piHome = filepath.Join(home, ".pi", "agent")
	}
	add(filepath.Join(piHome, "sessions"), "pi", "pi")
	if dir := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); dir != "" {
		add(dir, "pi", "pi")
	}

	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	add(filepath.Join(claudeHome, "projects"), "claude", "claude")

	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	add(filepath.Join(codexHome, "sessions"), "codex", "codex")
	add(filepath.Join(codexHome, "archived_sessions"), "codex", "codex")
	add(filepath.Join(home, ".cursor", "projects"), "cursor", "cursor")
	return roots
}

func (s *Server) resolveSessionPath(raw string) (sessionLocation, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sessionLocation{}, fmt.Errorf("请选择会话")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return sessionLocation{}, fmt.Errorf("无效的会话路径")
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if !strings.HasSuffix(strings.ToLower(abs), ".jsonl") {
		return sessionLocation{}, fmt.Errorf("只支持 .jsonl 会话文件")
	}
	for _, root := range s.sessionRootSpecs() {
		resolvedRoot, err := filepath.EvalSymlinks(root.Path)
		if err != nil {
			resolvedRoot = root.Path
		}
		if abs == resolvedRoot || within(resolvedRoot, abs) {
			return sessionLocation{Path: abs, Agent: root.Agent, SourceKind: root.SourceKind}, nil
		}
	}
	return sessionLocation{}, fmt.Errorf("会话不在允许的目录中")
}

func parseAgentSession(agent, path string) (parsedSession, error) {
	switch agent {
	case "pi":
		return parsePiSession(path)
	case "claude":
		return parseClaudeSession(path)
	case "codex":
		return parseCodexSession(path)
	case "cursor":
		return parseCursorSession(path)
	default:
		return parsedSession{}, fmt.Errorf("暂不支持导入 %s 会话", sessionAgentName(agent))
	}
}

// exec.LookPath probes every PATH entry (times every PATHEXT suffix on
// Windows), so cache results briefly; the agent list is requested often.
const commandLookupTTL = 30 * time.Second

var commandLookups = struct {
	sync.Mutex
	entries map[string]commandLookup
}{entries: map[string]commandLookup{}}

type commandLookup struct {
	installed bool
	checked   time.Time
}

func commandInstalled(command string) bool {
	if command == "" {
		return false
	}
	commandLookups.Lock()
	cached, ok := commandLookups.entries[command]
	commandLookups.Unlock()
	if ok && time.Since(cached.checked) < commandLookupTTL {
		return cached.installed
	}
	_, err := exec.LookPath(command)
	installed := err == nil
	commandLookups.Lock()
	commandLookups.entries[command] = commandLookup{installed: installed, checked: time.Now()}
	commandLookups.Unlock()
	return installed
}

func (s *Server) agentInfos(sessions []SessionCandidate, roots []sessionRoot) []agentInfo {
	counts := map[string]int{}
	for _, session := range sessions {
		counts[session.Agent]++
	}
	rootInstalled := map[string]bool{}
	for _, root := range roots {
		if info, err := os.Stat(root.Path); err == nil && info.IsDir() {
			rootInstalled[root.Agent] = true
		}
	}
	out := make([]agentInfo, 0, len(sessionAgentCatalog))
	for _, item := range sessionAgentCatalog {
		installed := commandInstalled(item.Command) || rootInstalled[item.ID] || counts[item.ID] > 0
		if !installed {
			continue
		}
		out = append(out, agentInfo{
			ID: item.ID, Name: item.Name, Command: item.Command,
			Installed: true, Supported: item.Supported, Count: counts[item.ID],
		})
	}
	return out
}

func parseClaudeSession(path string) (parsedSession, error) {
	file, info, err := openSessionFile(path)
	if err != nil {
		return parsedSession{}, err
	}
	defer file.Close()
	parsed := parsedSession{UpdatedAt: info.ModTime()}
	seen := map[string]bool{}
	scanner := newSessionScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			Type        string          `json:"type"`
			IsMeta      bool            `json:"isMeta"`
			IsSidechain bool            `json:"isSidechain"`
			Cwd         string          `json:"cwd"`
			SessionID   string          `json:"sessionId"`
			Timestamp   json.RawMessage `json:"timestamp"`
			AITitle     string          `json:"aiTitle"`
			CustomTitle string          `json:"customTitle"`
			Summary     string          `json:"summary"`
			Message     *struct {
				ID      string          `json:"id"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		if parsed.CreatedAt.IsZero() {
			parsed.CreatedAt = parseSessionTimestamp(raw.Timestamp)
		}
		if raw.Cwd != "" && parsed.Cwd == "" {
			parsed.Cwd = raw.Cwd
		}
		if raw.SessionID != "" && parsed.SessionID == "" {
			parsed.SessionID = raw.SessionID
		}
		if title := strings.TrimSpace(firstNonEmpty(raw.CustomTitle, raw.AITitle, raw.Summary)); title != "" && parsed.Title == "" {
			parsed.Title = sessionTitle(title)
		}
		if raw.IsMeta || raw.IsSidechain || raw.Message == nil {
			continue
		}
		role := raw.Message.Role
		if role != "user" && role != "assistant" {
			continue
		}
		body := sessionText(raw.Message.Content)
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		if role == "user" && isClaudeContext(body) {
			continue
		}
		key := role + "\x00" + raw.Message.ID + "\x00" + body
		if raw.Message.ID != "" && seen[key] {
			continue
		}
		seen[key] = true
		at := parseSessionTimestamp(raw.Timestamp)
		parsed.Messages = append(parsed.Messages, sessionMessage{Role: role, Body: limitSessionBody(body), CreatedAt: at})
		if parsed.Title == "" && role == "user" && isHumanPrompt(body) {
			parsed.Title = sessionTitle(body)
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedSession{}, err
	}
	return finishParsedSession(path, parsed, info)
}

func parseCodexSession(path string) (parsedSession, error) {
	file, info, err := openSessionFile(path)
	if err != nil {
		return parsedSession{}, err
	}
	defer file.Close()
	parsed := parsedSession{UpdatedAt: info.ModTime()}
	seen := map[string]bool{}
	scanner := newSessionScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			Type      string          `json:"type"`
			Timestamp json.RawMessage `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		if parsed.CreatedAt.IsZero() {
			parsed.CreatedAt = parseSessionTimestamp(raw.Timestamp)
		}
		if raw.Type == "session_meta" {
			var meta struct {
				ID        string `json:"id"`
				SessionID string `json:"session_id"`
				ThreadID  string `json:"thread_id"`
				Cwd       string `json:"cwd"`
			}
			if json.Unmarshal(raw.Payload, &meta) == nil {
				if parsed.SessionID == "" {
					parsed.SessionID = firstNonEmpty(meta.ID, meta.SessionID, meta.ThreadID)
				}
				if parsed.Cwd == "" {
					parsed.Cwd = meta.Cwd
				}
			}
			continue
		}
		if raw.Type != "response_item" {
			continue
		}
		var payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw.Payload, &payload) != nil || payload.Type != "message" {
			continue
		}
		role := payload.Role
		if role != "user" && role != "assistant" {
			continue
		}
		var parts []string
		for _, part := range payload.Content {
			if part.Type != "input_text" && part.Type != "output_text" && part.Type != "text" {
				continue
			}
			text := strings.TrimSpace(part.Text)
			if text == "" || (role == "user" && isCodexContext(text)) {
				continue
			}
			parts = append(parts, text)
		}
		body := strings.TrimSpace(strings.Join(parts, "\n\n"))
		if body == "" {
			continue
		}
		body = limitSessionBody(body)
		key := role + "\x00" + body
		if seen[key] {
			continue
		}
		seen[key] = true
		at := parseSessionTimestamp(raw.Timestamp)
		parsed.Messages = append(parsed.Messages, sessionMessage{Role: role, Body: body, CreatedAt: at})
		if parsed.Title == "" && role == "user" && isHumanPrompt(body) {
			parsed.Title = sessionTitle(body)
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedSession{}, err
	}
	return finishParsedSession(path, parsed, info)
}

func parseCursorSession(path string) (parsedSession, error) {
	file, info, err := openSessionFile(path)
	if err != nil {
		return parsedSession{}, err
	}
	defer file.Close()
	parsed := parsedSession{UpdatedAt: info.ModTime(), CreatedAt: info.ModTime()}
	scanner := newSessionScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			Role    string `json:"role"`
			Message *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil || raw.Message == nil {
			continue
		}
		role := raw.Role
		if role != "user" && role != "assistant" {
			continue
		}
		body := sessionText(raw.Message.Content)
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		if role == "user" {
			body = extractCursorQuery(body)
			if body == "" {
				continue
			}
		}
		body = limitSessionBody(body)
		parsed.Messages = append(parsed.Messages, sessionMessage{Role: role, Body: body, CreatedAt: info.ModTime()})
		if parsed.Title == "" && role == "user" && isHumanPrompt(body) && !strings.HasPrefix(body, "Project: ") {
			parsed.Title = sessionTitle(body)
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedSession{}, err
	}
	return finishParsedSession(path, parsed, info)
}

func extractCursorQuery(body string) string {
	body = strings.TrimSpace(body)
	if start := strings.Index(body, "<user_query>"); start >= 0 {
		body = body[start+len("<user_query>"):]
		if end := strings.Index(body, "</user_query>"); end >= 0 {
			body = body[:end]
		}
	}
	return strings.TrimSpace(body)
}

func openSessionFile(path string) (*os.File, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if info.Size() > maxSessionFileSize {
		_ = file.Close()
		return nil, nil, fmt.Errorf("会话文件过大")
	}
	return file, info, nil
}

func newSessionScanner(file *os.File) *bufio.Scanner {
	scanner := bufio.NewScanner(io.LimitReader(file, maxSessionFileSize))
	scanner.Buffer(make([]byte, 64*1024), maxSessionFileSize)
	return scanner
}

func finishParsedSession(path string, parsed parsedSession, info os.FileInfo) (parsedSession, error) {
	if len(parsed.Messages) == 0 {
		return parsedSession{}, fmt.Errorf("会话中没有可用的对话内容")
	}
	if parsed.UpdatedAt.IsZero() {
		parsed.UpdatedAt = info.ModTime()
	}
	if parsed.CreatedAt.IsZero() {
		parsed.CreatedAt = parsed.UpdatedAt
	}
	if parsed.SessionID == "" {
		parsed.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if parsed.Title == "" {
		parsed.Title = "未命名会话"
	}
	return parsed, nil
}

func limitSessionBody(body string) string {
	if len(body) > maxSessionMessage {
		return body[:maxSessionMessage] + "…"
	}
	return body
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isHumanPrompt(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	if strings.HasPrefix(body, "You are the background Code Agent for commentloop.") {
		return false
	}
	if strings.HasPrefix(body, "<") {
		return false
	}
	return true
}

func isCodexContext(body string) bool {
	body = strings.TrimSpace(body)
	// Codex puts its environment, app context, plugin catalog and multi-agent
	// instructions in user-shaped response items. They are not typed prompts.
	if strings.HasPrefix(body, "<") {
		return true
	}
	if strings.HasPrefix(body, "# AGENTS.md instructions") {
		return true
	}
	if strings.Contains(body, "<INSTRUCTIONS>") && strings.Contains(body, "AGENTS.md") {
		return true
	}
	if strings.HasPrefix(body, "You are the forked subagent") {
		return true
	}
	for _, prefix := range []string{
		"<environment_context>", "<external_codex_apps_", "<app-context>",
		"<codex_apps_", "<multi_agent_", "<user_instructions>",
	} {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	return false
}

func isClaudeContext(body string) bool {
	body = strings.TrimSpace(body)
	return strings.HasPrefix(body, "<system-reminder>") ||
		strings.HasPrefix(body, "<command-name>") ||
		strings.HasPrefix(body, "<local-command")
}

func (s *Server) availableAgentSessions(docPath string) ([]SessionCandidate, []agentInfo, error) {
	imported, err := s.discussions.importedSources(docPath)
	if err != nil {
		return nil, nil, err
	}
	roots := s.sessionRootSpecs()
	type fileCandidate struct {
		Path  string
		Agent string
		Kind  string
		Mod   time.Time
	}
	var files []fileCandidate
	seen := map[string]bool{}
	for _, root := range roots {
		info, err := os.Stat(root.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		resolvedRoot, err := filepath.EvalSymlinks(root.Path)
		if err != nil {
			resolvedRoot = root.Path
		}
		_ = filepath.WalkDir(resolvedRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				path = resolved
			}
			if seen[path] || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				return nil
			}
			if root.Agent == "cursor" {
				normalized := filepath.ToSlash(path)
				if !strings.Contains(normalized, "/agent-transcripts/") || strings.Contains(normalized, "/subagents/") {
					return nil
				}
			}
			stat, err := entry.Info()
			if err != nil || stat.Size() > maxSessionFileSize {
				return nil
			}
			seen[path] = true
			files = append(files, fileCandidate{Path: path, Agent: root.Agent, Kind: root.SourceKind, Mod: stat.ModTime()})
			return nil
		})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Mod.After(files[j].Mod) })
	if len(files) > 160 {
		files = files[:160]
	}
	candidates := []SessionCandidate{}
	for _, file := range files {
		parsed, err := parseAgentSession(file.Agent, file.Path)
		if err != nil {
			continue
		}
		candidates = append(candidates, SessionCandidate{
			Path: file.Path, Title: parsed.Title, SourceKind: file.Kind,
			Agent: file.Agent, AgentName: sessionAgentName(file.Agent),
			Cwd: parsed.Cwd, SessionID: parsed.SessionID, MessageCount: len(parsed.Messages),
			CreatedAt: parsed.CreatedAt, UpdatedAt: parsed.UpdatedAt,
			Imported: imported[file.Path],
		})
	}
	return candidates, s.agentInfos(candidates, roots), nil
}
