package server

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxSessionFileSize = 32 << 20
	maxSessionMessage  = 20000
	maxConsensusSize   = 20000
)

type Discussion struct {
	ID           string    `json:"id"`
	DocPath      string    `json:"-"`
	Title        string    `json:"title"`
	SourcePath   string    `json:"sourcePath"`
	SourceKind   string    `json:"sourceKind"`
	Agent        string    `json:"agent"`
	AgentName    string    `json:"agentName"`
	Cwd          string    `json:"cwd,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	MessageCount int       `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	ImportedAt   time.Time `json:"importedAt"`
}

type DiscussionMessage struct {
	Seq       int       `json:"seq"`
	Role      string    `json:"role"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Consensus struct {
	Content   string    `json:"content"`
	Source    string    `json:"source,omitempty"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SessionCandidate struct {
	Path         string    `json:"path"`
	Title        string    `json:"title"`
	SourceKind   string    `json:"sourceKind"`
	Agent        string    `json:"agent"`
	AgentName    string    `json:"agentName"`
	Cwd          string    `json:"cwd,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	MessageCount int       `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Imported     bool      `json:"imported,omitempty"`
}

type sessionMessage struct {
	Role      string
	Body      string
	CreatedAt time.Time
}

type parsedSession struct {
	SessionID string
	Cwd       string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Messages  []sessionMessage
}

type discussionStore struct {
	db *sql.DB
}

func newDiscussionStore(db *sql.DB) *discussionStore {
	return &discussionStore{db: db}
}

func (s *discussionStore) list(docPath string) ([]Discussion, error) {
	rows, err := s.db.Query(`SELECT id,doc_path,title,source_path,source_kind,agent,cwd,session_id,message_count,created_at,imported_at
		FROM doc_discussions WHERE doc_path=? ORDER BY imported_at,id`, docPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Discussion{}
	for rows.Next() {
		var item Discussion
		var createdAt, importedAt string
		if err := rows.Scan(&item.ID, &item.DocPath, &item.Title, &item.SourcePath, &item.SourceKind,
			&item.Agent, &item.Cwd, &item.SessionID, &item.MessageCount, &createdAt, &importedAt); err != nil {
			return nil, err
		}
		item.AgentName = sessionAgentName(item.Agent)
		item.CreatedAt = parseTime(createdAt)
		item.ImportedAt = parseTime(importedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *discussionStore) get(docPath, id string) (Discussion, bool) {
	var item Discussion
	var createdAt, importedAt string
	err := s.db.QueryRow(`SELECT id,doc_path,title,source_path,source_kind,agent,cwd,session_id,message_count,created_at,imported_at
		FROM doc_discussions WHERE id=? AND doc_path=?`, id, docPath).
		Scan(&item.ID, &item.DocPath, &item.Title, &item.SourcePath, &item.SourceKind,
			&item.Agent, &item.Cwd, &item.SessionID, &item.MessageCount, &createdAt, &importedAt)
	if err != nil {
		return Discussion{}, false
	}
	item.AgentName = sessionAgentName(item.Agent)
	item.CreatedAt = parseTime(createdAt)
	item.ImportedAt = parseTime(importedAt)
	return item, true
}

func (s *discussionStore) bySource(docPath, sourcePath string) (Discussion, bool) {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM doc_discussions WHERE doc_path=? AND source_path=?`, docPath, sourcePath).Scan(&id); err != nil {
		return Discussion{}, false
	}
	return s.get(docPath, id)
}

func discussionID(sourcePath string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	return "disc_" + hex.EncodeToString(sum[:8])
}

func (s *discussionStore) importSession(docPath string, parsed parsedSession, sourcePath, sourceKind, agent string) (Discussion, error) {
	if existing, ok := s.bySource(docPath, sourcePath); ok {
		return existing, nil
	}
	now := time.Now()
	item := Discussion{
		ID: discussionID(docPath + "\x00" + sourcePath), DocPath: docPath,
		Title: parsed.Title, SourcePath: sourcePath, SourceKind: sourceKind, Agent: agent,
		Cwd: parsed.Cwd, SessionID: parsed.SessionID,
		MessageCount: len(parsed.Messages),
		CreatedAt:    parsed.CreatedAt, ImportedAt: now,
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.Title == "" {
		item.Title = filepath.Base(sourcePath)
	}
	item.AgentName = sessionAgentName(agent)
	tx, err := s.db.Begin()
	if err != nil {
		return Discussion{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO doc_discussions
		(id,doc_path,title,source_path,source_kind,agent,cwd,session_id,message_count,created_at,imported_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.DocPath, item.Title, item.SourcePath, item.SourceKind, item.Agent, item.Cwd,
		item.SessionID, item.MessageCount, formatTime(item.CreatedAt), formatTime(item.ImportedAt)); err != nil {
		return Discussion{}, err
	}
	for i, message := range parsed.Messages {
		if _, err := tx.Exec(`INSERT INTO discussion_messages
			(id,discussion_id,seq,role,body,created_at) VALUES (?,?,?,?,?,?)`,
			fmt.Sprintf("%s_%d", item.ID, i), item.ID, i, message.Role, message.Body,
			formatTime(message.CreatedAt)); err != nil {
			return Discussion{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Discussion{}, err
	}
	return item, nil
}

func (s *discussionStore) delete(docPath, id string) error {
	result, err := s.db.Exec(`DELETE FROM doc_discussions WHERE id=? AND doc_path=?`, id, docPath)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *discussionStore) messages(discussionID string) ([]DiscussionMessage, error) {
	rows, err := s.db.Query(`SELECT seq,role,body,created_at FROM discussion_messages
		WHERE discussion_id=? ORDER BY seq`, discussionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscussionMessage{}
	for rows.Next() {
		var message DiscussionMessage
		var createdAt string
		if err := rows.Scan(&message.Seq, &message.Role, &message.Body, &createdAt); err != nil {
			return nil, err
		}
		message.CreatedAt = parseTime(createdAt)
		out = append(out, message)
	}
	return out, rows.Err()
}

func (s *discussionStore) consensus(docPath string) (Consensus, bool) {
	var item Consensus
	var updatedAt string
	err := s.db.QueryRow(`SELECT content,source,version,updated_at FROM doc_consensus WHERE doc_path=?`, docPath).
		Scan(&item.Content, &item.Source, &item.Version, &updatedAt)
	if err != nil {
		return Consensus{}, false
	}
	item.UpdatedAt = parseTime(updatedAt)
	return item, true
}

func (s *discussionStore) saveConsensus(docPath, content, source string) (Consensus, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Consensus{}, fmt.Errorf("共识内容不能为空")
	}
	if len(content) > maxConsensusSize {
		return Consensus{}, fmt.Errorf("共识内容过长")
	}
	existing, ok := s.consensus(docPath)
	version := 1
	if ok {
		version = existing.Version + 1
	}
	now := time.Now()
	_, err := s.db.Exec(`INSERT INTO doc_consensus (doc_path,content,source,version,updated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(doc_path) DO UPDATE SET content=excluded.content,source=excluded.source,
		version=excluded.version,updated_at=excluded.updated_at`,
		docPath, content, source, version, formatTime(now))
	if err != nil {
		return Consensus{}, err
	}
	return Consensus{Content: content, Source: source, Version: version, UpdatedAt: now}, nil
}

func (s *discussionStore) hasContext(docPath string) bool {
	if _, ok := s.consensus(docPath); ok {
		return true
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM doc_discussions WHERE doc_path=?`, docPath).Scan(&count)
	return count > 0
}

func (s *discussionStore) importedSources(docPath string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT source_path FROM doc_discussions WHERE doc_path=?`, docPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err == nil {
			out[source] = true
		}
	}
	return out, rows.Err()
}

func (s *discussionStore) excerpts(docPath string, ids []string, quote, body string) string {
	if len(ids) == 0 {
		return ""
	}
	var parts []string
	total := 0
	for _, id := range ids {
		item, ok := s.get(docPath, id)
		if !ok {
			continue
		}
		messages, err := s.messages(id)
		if err != nil || len(messages) == 0 {
			continue
		}
		selected := selectDiscussionMessages(messages, quote+" "+body, 10)
		var b strings.Builder
		b.WriteString("### " + item.Title + "\n")
		b.WriteString("Source: " + item.SourcePath + "\n")
		for _, message := range selected {
			text := strings.TrimSpace(message.Body)
			if text == "" {
				continue
			}
			if len(text) > 1800 {
				text = text[:1800] + "…"
			}
			if total+len(text) > 12000 {
				break
			}
			total += len(text)
			b.WriteString(fmt.Sprintf("[%s] %s: %s\n", message.CreatedAt.Format("2006-01-02 15:04"), message.Role, text))
		}
		if b.Len() > 0 {
			parts = append(parts, b.String())
		}
		if total >= 12000 {
			break
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func selectDiscussionMessages(messages []DiscussionMessage, query string, limit int) []DiscussionMessage {
	if len(messages) <= limit {
		return messages
	}
	// Keep the latest messages (where conclusions usually are), plus a few
	// early messages that often establish goals and constraints.
	out := make([]DiscussionMessage, 0, limit)
	out = append(out, messages[:2]...)
	start := len(messages) - (limit - 2)
	if start < 2 {
		start = 2
	}
	out = append(out, messages[start:]...)
	return out
}

// parsePiSession reads pi's JSONL transcript and keeps user/assistant text.
// Tool calls, tool results, thinking blocks, and system prompts are omitted.
func parsePiSession(path string) (parsedSession, error) {
	file, err := os.Open(path)
	if err != nil {
		return parsedSession{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return parsedSession{}, err
	}
	if info.Size() > maxSessionFileSize {
		return parsedSession{}, fmt.Errorf("会话文件过大")
	}
	parsed := parsedSession{}
	scanner := bufio.NewScanner(io.LimitReader(file, maxSessionFileSize))
	scanner.Buffer(make([]byte, 64*1024), maxSessionFileSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Timestamp json.RawMessage `json:"timestamp"`
			Cwd       string          `json:"cwd"`
			Message   *struct {
				Role      string          `json:"role"`
				Content   json.RawMessage `json:"content"`
				Timestamp json.RawMessage `json:"timestamp"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		switch raw.Type {
		case "session":
			parsed.SessionID = raw.ID
			parsed.Cwd = raw.Cwd
			parsed.CreatedAt = parseSessionTimestamp(raw.Timestamp)
		case "message":
			if raw.Message == nil {
				continue
			}
			role := raw.Message.Role
			if role != "user" && role != "assistant" {
				continue
			}
			body := sessionText(raw.Message.Content)
			if strings.TrimSpace(body) == "" {
				continue
			}
			if len(body) > maxSessionMessage {
				body = body[:maxSessionMessage] + "…"
			}
			at := parseSessionTimestamp(raw.Message.Timestamp)
			if at.IsZero() {
				at = parseSessionTimestamp(raw.Timestamp)
			}
			parsed.Messages = append(parsed.Messages, sessionMessage{Role: role, Body: body, CreatedAt: at})
			if parsed.Title == "" && role == "user" {
				if title := piSessionTitle(body); title != "" {
					parsed.Title = title
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedSession{}, err
	}
	if len(parsed.Messages) == 0 {
		return parsedSession{}, fmt.Errorf("会话中没有可用的对话内容")
	}
	parsed.UpdatedAt = info.ModTime()
	if parsed.CreatedAt.IsZero() {
		parsed.CreatedAt = parsed.UpdatedAt
	}
	if parsed.SessionID == "" {
		parsed.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if parsed.Title == "" {
		parsed.Title = "Pi 会话"
	}
	return parsed, nil
}

func sessionText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func sessionTitle(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if runes := []rune(body); len(runes) > 72 {
		body = string(runes[:72]) + "…"
	}
	return body
}

func piSessionTitle(body string) string {
	if isHumanPrompt(body) {
		return sessionTitle(body)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "You are the background Code Agent for mdlive.") {
		return ""
	}
	doc := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "Document: ") {
			doc = strings.TrimSpace(strings.TrimPrefix(line, "Document: "))
			break
		}
	}
	quote := ""
	if start := strings.Index(body, "Quote:\n"); start >= 0 {
		quote = body[start+len("Quote:\n"):]
		if end := strings.Index(quote, "\n\n"); end >= 0 {
			quote = quote[:end]
		}
	}
	quote = strings.TrimSpace(quote)
	if quote != "" {
		return "评论 · " + sessionTitle(quote)
	}
	if doc != "" {
		return "评论 · " + filepath.Base(doc)
	}
	return "评论任务"
}

func parseSessionTimestamp(raw json.RawMessage) time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err == nil {
			return parsed
		}
		return time.Time{}
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil && number > 0 {
		seconds := int64(number)
		nanos := int64((number - float64(seconds)) * 1e9)
		if number > 1e11 {
			// Pi writes message timestamps as Unix milliseconds.
			seconds = int64(number / 1000)
			nanos = int64(number-math.Trunc(number/1000)*1000) * int64(time.Millisecond)
		}
		return time.Unix(seconds, nanos)
	}
	return time.Time{}
}
