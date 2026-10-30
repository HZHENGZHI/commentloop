package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	SourceMTime  int64     `json:"-"`
	SourceSize   int64     `json:"-"`
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
	db *gorm.DB
}

func newDiscussionStore(db *gorm.DB) *discussionStore {
	return &discussionStore{db: db}
}

func (r docDiscussionRow) discussion() Discussion {
	return Discussion{
		ID: r.ID, DocPath: r.DocPath, Title: r.Title, SourcePath: r.SourcePath, SourceKind: r.SourceKind,
		Agent: r.Agent, AgentName: sessionAgentName(r.Agent), Cwd: r.Cwd, SessionID: r.SessionID,
		MessageCount: r.MessageCount, SourceMTime: r.SourceMtime, SourceSize: r.SourceSize,
		CreatedAt: parseTime(r.CreatedAt), ImportedAt: parseTime(r.ImportedAt),
	}
}

func discussionsFromRows(rows []docDiscussionRow) []Discussion {
	out := make([]Discussion, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.discussion())
	}
	return out
}

func (s *discussionStore) list(docPath string) ([]Discussion, error) {
	var rows []docDiscussionRow
	if err := s.db.Where("doc_path = ?", docPath).Order("imported_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return discussionsFromRows(rows), nil
}

func (s *discussionStore) get(docPath, id string) (Discussion, bool) {
	var row docDiscussionRow
	if err := s.db.Take(&row, "id = ? AND doc_path = ?", id, docPath).Error; err != nil {
		return Discussion{}, false
	}
	return row.discussion(), true
}

func (s *discussionStore) bySource(docPath, sourcePath string) (Discussion, bool) {
	var row docDiscussionRow
	if err := s.db.Take(&row, "doc_path = ? AND source_path = ?", docPath, sourcePath).Error; err != nil {
		return Discussion{}, false
	}
	return row.discussion(), true
}

func discussionID(sourcePath string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	return "disc_" + hex.EncodeToString(sum[:8])
}

func (s *discussionStore) importSession(docPath string, parsed parsedSession, sourcePath, sourceKind, agent string) (Discussion, error) {
	mtime, size := sessionFileStamp(sourcePath)
	if existing, ok := s.bySource(docPath, sourcePath); ok {
		if err := s.updateSessionMessages(existing.ID, existing.DocPath, parsed, sourcePath, sourceKind, agent, mtime, size); err != nil {
			return Discussion{}, err
		}
		item, ok := s.get(existing.DocPath, existing.ID)
		if !ok {
			return Discussion{}, os.ErrNotExist
		}
		return item, nil
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
	row := docDiscussionRow{
		ID: item.ID, DocPath: item.DocPath, Title: item.Title, SourcePath: item.SourcePath,
		SourceKind: item.SourceKind, Agent: item.Agent, Cwd: item.Cwd, SessionID: item.SessionID,
		MessageCount: item.MessageCount, SourceMtime: item.SourceMTime, SourceSize: item.SourceSize,
		CreatedAt: formatTime(item.CreatedAt), ImportedAt: formatTime(item.ImportedAt),
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return createDiscussionMessages(tx, item.ID, parsed.Messages)
	})
	if err != nil {
		return Discussion{}, err
	}
	return item, nil
}

// discussionMessageBatch keeps multi-row INSERTs well below SQLite's
// bound-parameter limit for long sessions.
const discussionMessageBatch = 500

func createDiscussionMessages(tx *gorm.DB, id string, messages []sessionMessage) error {
	if len(messages) == 0 {
		return nil
	}
	rows := make([]discussionMessageRow, len(messages))
	for i, message := range messages {
		rows[i] = discussionMessageRow{
			ID: fmt.Sprintf("%s_%d", id, i), DiscussionID: id, Seq: i,
			Role: message.Role, Body: message.Body, CreatedAt: formatTime(message.CreatedAt),
		}
	}
	return tx.CreateInBatches(&rows, discussionMessageBatch).Error
}

func sessionFileStamp(path string) (int64, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.ModTime().UnixNano(), info.Size()
}

func (s *discussionStore) updateSessionMessages(id, docPath string, parsed parsedSession, sourcePath, sourceKind, agent string, mtime, size int64) error {
	now := time.Now()
	item := Discussion{
		ID: id, DocPath: docPath, Title: parsed.Title, SourcePath: sourcePath,
		SourceKind: sourceKind, Agent: agent, Cwd: parsed.Cwd, SessionID: parsed.SessionID,
		MessageCount: len(parsed.Messages), SourceMTime: mtime, SourceSize: size,
		CreatedAt: parsed.CreatedAt, ImportedAt: now,
	}
	if item.Title == "" {
		item.Title = filepath.Base(sourcePath)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&docDiscussionRow{}).Where("id = ? AND doc_path = ?", id, docPath).
			Updates(map[string]any{
				"title": item.Title, "source_path": item.SourcePath, "source_kind": item.SourceKind,
				"agent": item.Agent, "cwd": item.Cwd, "session_id": item.SessionID,
				"message_count": item.MessageCount, "source_mtime": item.SourceMTime, "source_size": item.SourceSize,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("discussion_id = ?", id).Delete(&discussionMessageRow{}).Error; err != nil {
			return err
		}
		return createDiscussionMessages(tx, id, parsed.Messages)
	})
}

func (s *discussionStore) delete(docPath, id string) error {
	result := s.db.Where("id = ? AND doc_path = ?", id, docPath).Delete(&docDiscussionRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *discussionStore) messages(discussionID string) ([]DiscussionMessage, error) {
	var rows []discussionMessageRow
	if err := s.db.Select("seq", "role", "body", "created_at").
		Where("discussion_id = ?", discussionID).Order("seq").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]DiscussionMessage, 0, len(rows))
	for _, row := range rows {
		out = append(out, DiscussionMessage{
			Seq: row.Seq, Role: row.Role, Body: row.Body, CreatedAt: parseTime(row.CreatedAt),
		})
	}
	return out, nil
}

func (s *discussionStore) consensus(docPath string) (Consensus, bool) {
	var row docConsensusRow
	if err := s.db.Take(&row, "doc_path = ?", docPath).Error; err != nil {
		return Consensus{}, false
	}
	return Consensus{Content: row.Content, Source: row.Source, Version: row.Version, UpdatedAt: parseTime(row.UpdatedAt)}, true
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
	err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "doc_path"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "source", "version", "updated_at"}),
	}).Create(&docConsensusRow{
		DocPath: docPath, Content: content, Source: source, Version: version, UpdatedAt: formatTime(now),
	}).Error
	if err != nil {
		return Consensus{}, err
	}
	return Consensus{Content: content, Source: source, Version: version, UpdatedAt: now}, nil
}

func (s *discussionStore) clearConsensus(docPath string) error {
	return s.db.Where("doc_path = ?", docPath).Delete(&docConsensusRow{}).Error
}

func (s *discussionStore) hasContext(docPath string) bool {
	if _, ok := s.consensus(docPath); ok {
		return true
	}
	var count int64
	_ = s.db.Model(&docDiscussionRow{}).Where("doc_path = ?", docPath).Count(&count).Error
	return count > 0
}

func (s *discussionStore) importedSources(docPath string) (map[string]bool, error) {
	var sources []string
	if err := s.db.Model(&docDiscussionRow{}).Where("doc_path = ?", docPath).
		Pluck("source_path", &sources).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(sources))
	for _, source := range sources {
		out[source] = true
	}
	return out, nil
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
	if !strings.HasPrefix(strings.TrimSpace(body), "You are the background Code Agent for commentloop.") {
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
