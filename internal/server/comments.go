package server

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type CommentMessage struct {
	ID              string    `json:"id"`
	Author          string    `json:"author"` // user | agent | system
	Body            string    `json:"body"`
	Before          string    `json:"before,omitempty"`
	After           string    `json:"after,omitempty"`
	VersionID       string    `json:"versionId,omitempty"`
	BeforeVersionID string    `json:"beforeVersionId,omitempty"`
	SectionStart    int       `json:"-"`
	SectionEnd      int       `json:"-"`
	CreatedAt       time.Time `json:"createdAt"`
}

type docVersion struct {
	ID        string
	Path      string
	Content   string
	CreatedAt time.Time
}

const maxDocVersionsPerPath = 30

type Comment struct {
	ID               string           `json:"id"`
	Path             string           `json:"path"`
	Quote            string           `json:"quote"`
	Anchor           string           `json:"anchor,omitempty"`
	GroupID          string           `json:"groupId,omitempty"`
	Prefix           string           `json:"prefix,omitempty"`
	Suffix           string           `json:"suffix,omitempty"`
	Line             int              `json:"line"`
	Start            int              `json:"start"`
	End              int              `json:"end"`
	Resolved         bool             `json:"resolved"`
	Orphaned         bool             `json:"orphaned"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	DiscussionIDs    []string         `json:"discussionIds,omitempty"`
	PromptTemplateID string           `json:"promptTemplateId,omitempty"`
	Messages         []CommentMessage `json:"messages"`
}

var (
	imageRE      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	linkRE       = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	htmlTagRE    = regexp.MustCompile(`<[^>]+>`)
	listMarkerRE = regexp.MustCompile(`^(?:[-*+]|\d+\.)\s+(?:\[[ xX]\]\s*)?`)
)

type commentStore struct {
	db *sql.DB
}

func newCommentStore(db *sql.DB) *commentStore {
	return &commentStore{db: db}
}

func commentID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func discussionIDsJSON(ids []string) string {
	data, _ := json.Marshal(normalizeDiscussionIDs(ids))
	return string(data)
}

func normalizeDiscussionIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *commentStore) create(path, quote, prefix, suffix, body, content string, discussionIDs []string, promptTemplateID string) (*Comment, error) {
	quote = strings.TrimSpace(quote)
	body = strings.TrimSpace(body)
	if quote == "" {
		return nil, fmt.Errorf("请选择要评论的文本")
	}
	if body == "" {
		return nil, fmt.Errorf("评论内容不能为空")
	}
	if len(quote) > 4000 || len(body) > 8000 || len(prefix) > 500 || len(suffix) > 500 {
		return nil, fmt.Errorf("评论内容过长")
	}
	start, end, line, orphaned := locateQuote(content, quote, prefix, suffix, 0)
	now := time.Now()
	comment := Comment{
		ID: commentID(), Path: path, Quote: quote, Prefix: prefix, Suffix: suffix,
		Start: start, End: end, Line: line, Orphaned: orphaned,
		Anchor: quote, CreatedAt: now, UpdatedAt: now,
	}
	message := CommentMessage{ID: commentID(), Author: "user", Body: body, CreatedAt: now}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	discussionJSON := discussionIDsJSON(discussionIDs)
	if _, err := tx.Exec(`INSERT INTO comments
		(id,path,quote,anchor,group_id,prefix,suffix,line,start_offset,end_offset,resolved,orphaned,created_at,updated_at,discussion_ids,prompt_template_id)
		VALUES (?,?,?,?,'',?,?,?,?,?,0,?,?,?,?,?)`,
		comment.ID, path, quote, quote, prefix, suffix, line, start, end, boolInt(orphaned),
		formatTime(now), formatTime(now), discussionJSON, promptTemplateID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO comment_messages
		(id,comment_id,author,body,before_text,after_text,created_at)
		VALUES (?,?,?,?, '', '', ?)`,
		message.ID, comment.ID, "user", body, formatTime(now)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	comment.Messages = []CommentMessage{message}
	comment.DiscussionIDs = normalizeDiscussionIDs(discussionIDs)
	comment.PromptTemplateID = promptTemplateID
	return &comment, nil
}

func (s *commentStore) list(path, content string) []Comment {
	comments, err := s.query(path)
	if err != nil {
		return nil
	}
	if content != "" {
		versions := make(map[string]docVersion)
		for i := range comments {
			candidates := commentAnchorCandidates(comments[i])
			found := false
			for _, candidate := range candidates {
				start, end, line, orphaned := locateQuote(content, candidate, comments[i].Prefix, comments[i].Suffix, comments[i].Line)
				if !orphaned {
					comments[i].Anchor = candidate
					comments[i].Start, comments[i].End, comments[i].Line, comments[i].Orphaned = start, end, line, false
					found = true
					break
				}
			}
			if !found {
				found = s.reanchorFromVersion(&comments[i], content, versions)
			}
			if !found {
				if line, ok := locateCompact(content, comments[i].Quote, comments[i].Line); ok {
					comments[i].Anchor = comments[i].Quote
					comments[i].Start, comments[i].End = 0, 0
					comments[i].Line = line
					comments[i].Orphaned = false
					found = true
				}
			}
			if !found {
				comments[i].Orphaned = true
				comments[i].Start, comments[i].End = 0, 0
				if len(candidates) > 0 {
					comments[i].Anchor = candidates[len(candidates)-1]
				}
			}
		}
	}
	// Comments are a chronological review thread. Anchor/line changes caused by
	// Agent edits must not reorder the panel.
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].CreatedAt.Equal(comments[j].CreatedAt) {
			return comments[i].ID < comments[j].ID
		}
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
	return comments
}

func (s *commentStore) all() []Comment {
	rows, err := s.db.Query(`SELECT id FROM comments ORDER BY created_at`)
	if err != nil {
		return nil
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	out := make([]Comment, 0, len(ids))
	for _, id := range ids {
		if comment, ok := s.getByID(id); ok {
			out = append(out, comment)
		}
	}
	return out
}

func (s *commentStore) get(path, id string) (Comment, bool) {
	comment, ok := s.getByID(id)
	if !ok || comment.Path != path {
		return Comment{}, false
	}
	return comment, true
}

func (s *commentStore) getByID(id string) (Comment, bool) {
	row := s.db.QueryRow(`SELECT id,path,quote,anchor,group_id,prefix,suffix,line,start_offset,end_offset,
		resolved,orphaned,created_at,updated_at,discussion_ids,prompt_template_id FROM comments WHERE id=?`, id)
	comment, err := scanComment(row)
	if err != nil {
		return Comment{}, false
	}
	messages, err := s.messages(comment.ID)
	if err != nil {
		return Comment{}, false
	}
	comment.Messages = messages
	return comment, true
}

func (s *commentStore) query(path string) ([]Comment, error) {
	rows, err := s.db.Query(`SELECT id,path,quote,anchor,group_id,prefix,suffix,line,start_offset,end_offset,
		resolved,orphaned,created_at,updated_at,discussion_ids,prompt_template_id FROM comments WHERE path=? ORDER BY created_at,id`, path)
	if err != nil {
		return nil, err
	}
	var out []Comment
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, comment)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		messages, err := s.messages(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Messages = messages
	}
	return out, nil
}

type commentScanner interface {
	Scan(dest ...any) error
}

func scanComment(scanner commentScanner) (Comment, error) {
	var comment Comment
	var resolved, orphaned int
	var createdAt, updatedAt, discussionIDs, promptTemplateID string
	err := scanner.Scan(
		&comment.ID, &comment.Path, &comment.Quote, &comment.Anchor, &comment.GroupID,
		&comment.Prefix, &comment.Suffix,
		&comment.Line, &comment.Start, &comment.End, &resolved, &orphaned,
		&createdAt, &updatedAt, &discussionIDs, &promptTemplateID,
	)
	if err != nil {
		return Comment{}, err
	}
	comment.Resolved = resolved != 0
	comment.Orphaned = orphaned != 0
	comment.CreatedAt = parseTime(createdAt)
	comment.UpdatedAt = parseTime(updatedAt)
	if discussionIDs != "" && discussionIDs != "null" {
		_ = json.Unmarshal([]byte(discussionIDs), &comment.DiscussionIDs)
	}
	comment.PromptTemplateID = promptTemplateID
	return comment, nil
}

func (s *commentStore) messages(commentID string) ([]CommentMessage, error) {
	rows, err := s.db.Query(`SELECT id,author,body,before_text,after_text,version_id,before_version_id,section_start,section_end,created_at
		FROM comment_messages WHERE comment_id=? ORDER BY created_at`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommentMessage
	for rows.Next() {
		var message CommentMessage
		var createdAt string
		if err := rows.Scan(&message.ID, &message.Author, &message.Body, &message.Before, &message.After,
			&message.VersionID, &message.BeforeVersionID, &message.SectionStart, &message.SectionEnd, &createdAt); err != nil {
			return nil, err
		}
		message.CreatedAt = parseTime(createdAt)
		out = append(out, message)
	}
	return out, rows.Err()
}

func (s *commentStore) reply(path, id, author, body, before, after, beforeSnapshot, snapshot string, resolve bool, discussionIDs []string, promptTemplateID string) (*Comment, error) {
	if author != "user" {
		// Resolving a review is a user decision. An agent reply may add another
		// revision, but it must never close the thread.
		resolve = false
	}
	if author == "system" {
		// A rollback is also not a user confirmation; keep it open for review.
		resolve = false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("回复内容不能为空")
	}
	if len(body) > 8000 || len(before) > 12000 || len(after) > 12000 {
		return nil, fmt.Errorf("回复内容过长")
	}
	if len(snapshot) > maxDocSize || len(beforeSnapshot) > maxDocSize {
		return nil, fmt.Errorf("快照内容过长")
	}
	comment, ok := s.get(path, id)
	if !ok {
		return nil, os.ErrNotExist
	}
	now := time.Now()
	resolved := comment.Resolved || resolve
	if author == "system" {
		resolved = false
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	versionID := ""
	beforeVersionID := ""
	sectionStart, sectionEnd := 0, 0
	if author == "agent" && strings.TrimSpace(snapshot) != "" {
		versionID, err = saveDocVersion(tx, path, snapshot, now)
		if err != nil {
			return nil, err
		}
		if start, end, ok := sectionRange(snapshot, after, comment.Quote, comment.Line); ok {
			sectionStart, sectionEnd = start, end
		}
	}
	if author == "agent" && strings.TrimSpace(beforeSnapshot) != "" {
		beforeVersionID, err = saveDocVersion(tx, path, beforeSnapshot, now)
		if err != nil {
			return nil, err
		}
	}
	message := CommentMessage{
		ID: commentID(), Author: author, Body: body,
		Before: before, After: after, VersionID: versionID,
		BeforeVersionID: beforeVersionID,
		SectionStart:    sectionStart, SectionEnd: sectionEnd, CreatedAt: now,
	}
	if _, err := tx.Exec(`INSERT INTO comment_messages
		(id,comment_id,author,body,before_text,after_text,version_id,before_version_id,section_start,section_end,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		message.ID, id, author, body, before, after, versionID, beforeVersionID,
		sectionStart, sectionEnd, formatTime(now)); err != nil {
		return nil, err
	}
	anchor := ""
	if strings.TrimSpace(after) != "" {
		anchor = plainTextAnchor(after)
	}
	if anchor != "" {
		if _, err := tx.Exec(`UPDATE comments SET updated_at=?, resolved=?, anchor=? WHERE id=?`,
			formatTime(now), boolInt(resolved), anchor, id); err != nil {
			return nil, err
		}
	} else if _, err := tx.Exec(`UPDATE comments SET updated_at=?, resolved=? WHERE id=?`,
		formatTime(now), boolInt(resolved), id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	updated, _ := s.get(path, id)
	return &updated, nil
}

func saveDocVersion(tx *sql.Tx, path, content string, now time.Time) (string, error) {
	// Keep rollback history bounded. Older snapshots (and rollback links on
	// their messages) are pruned first so a re-created identical snapshot gets
	// a fresh row.
	if _, err := tx.Exec(`DELETE FROM doc_versions
		WHERE path=? AND id NOT IN (
			SELECT id FROM doc_versions WHERE path=?
			ORDER BY created_at DESC, rowid DESC LIMIT ?
		)`, path, path, maxDocVersionsPerPath-1); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE comment_messages SET version_id=''
		WHERE version_id != '' AND version_id NOT IN (SELECT id FROM doc_versions)`); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE comment_messages SET before_version_id=''
		WHERE before_version_id != '' AND before_version_id NOT IN (SELECT id FROM doc_versions)`); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	id := "ver_" + commentID()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO doc_versions
		(id,path,hash,content,created_at) VALUES (?,?,?,?,?)`,
		id, path, hash, content, formatTime(now)); err != nil {
		return "", err
	}
	if err := tx.QueryRow(`SELECT id FROM doc_versions WHERE path=? AND hash=?`,
		path, hash).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

type versionTarget struct {
	Comment Comment
	Message CommentMessage
	Round   int
}

// versionTargets returns one message per comment that has a rollback link to
// this snapshot, choosing the latest linked revision in each thread.
func (s *commentStore) versionTargets(path, versionID string) ([]versionTarget, error) {
	comments, err := s.query(path)
	if err != nil {
		return nil, err
	}
	out := make([]versionTarget, 0)
	for _, comment := range comments {
		round := 0
		foundRound := 0
		var found *CommentMessage
		for i := range comment.Messages {
			message := &comment.Messages[i]
			if message.Author != "agent" {
				continue
			}
			round++
			if message.VersionID == versionID {
				copied := *message
				found = &copied
				foundRound = round
			}
		}
		if found == nil {
			continue
		}
		out = append(out, versionTarget{Comment: comment, Message: *found, Round: foundRound})
	}
	return out, nil
}

func (s *commentStore) version(id string) (docVersion, bool) {
	var version docVersion
	var createdAt string
	err := s.db.QueryRow(`SELECT id,path,content,created_at FROM doc_versions WHERE id=?`, id).
		Scan(&version.ID, &version.Path, &version.Content, &createdAt)
	if err != nil {
		return docVersion{}, false
	}
	version.CreatedAt = parseTime(createdAt)
	return version, true
}

func (s *commentStore) resolve(path, id string, resolved bool) (*Comment, error) {
	comment, ok := s.get(path, id)
	if !ok {
		return nil, os.ErrNotExist
	}
	now := time.Now()
	if _, err := s.db.Exec(`UPDATE comments SET resolved=?, updated_at=? WHERE id=?`,
		boolInt(resolved), formatTime(now), id); err != nil {
		return nil, err
	}
	comment.Resolved = resolved
	comment.UpdatedAt = now
	return &comment, nil
}

func (s *commentStore) delete(path, id string) error {
	comment, ok := s.get(path, id)
	if !ok {
		return os.ErrNotExist
	}
	result, err := s.db.Exec(`DELETE FROM comments WHERE id=? AND path=?`, id, path)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return os.ErrNotExist
	}
	s.cleanupGroup(path, comment.GroupID)
	// Drop snapshots that only belonged to the deleted comment. Versions shared
	// by other comments remain available.
	_, _ = s.db.Exec(`DELETE FROM doc_versions
		WHERE path=? AND id NOT IN (
			SELECT version_id FROM comment_messages WHERE version_id != ''
			UNION SELECT before_version_id FROM comment_messages WHERE before_version_id != ''
		)`, path)
	return nil
}

// setGroup links comments as one coordinated review request. The selected
// comment and memberIDs become the complete membership of one group; any
// previous group members that were not selected leave the group.
func (s *commentStore) setGroup(path, id string, memberIDs []string) error {
	rows, err := s.db.Query(`SELECT id,group_id FROM comments WHERE path=?`, path)
	if err != nil {
		return err
	}
	groups := make(map[string]string)
	for rows.Next() {
		var commentID, groupID string
		if err := rows.Scan(&commentID, &groupID); err != nil {
			_ = rows.Close()
			return err
		}
		groups[commentID] = groupID
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if _, ok := groups[id]; !ok {
		return os.ErrNotExist
	}
	desired := []string{id}
	seen := map[string]struct{}{id: {}}
	for _, memberID := range memberIDs {
		memberID = strings.TrimSpace(memberID)
		if memberID == "" {
			continue
		}
		if _, ok := groups[memberID]; !ok {
			return fmt.Errorf("关联的评论不存在或不属于当前文档")
		}
		if _, ok := seen[memberID]; ok {
			continue
		}
		seen[memberID] = struct{}{}
		desired = append(desired, memberID)
	}
	if len(desired) < 2 {
		groupID := groups[id]
		if groupID == "" {
			return nil
		}
		if _, err := s.db.Exec(`UPDATE comments SET group_id='' WHERE path=? AND id=?`, path, id); err != nil {
			return err
		}
		s.cleanupGroup(path, groupID)
		return nil
	}
	target := groups[id]
	if target == "" {
		for _, memberID := range desired[1:] {
			if groupID := groups[memberID]; groupID != "" {
				target = groupID
				break
			}
		}
	}
	if target == "" {
		target = "grp_" + commentID()
	}
	oldGroups := make(map[string]struct{})
	for _, desiredID := range desired {
		if groupID := groups[desiredID]; groupID != "" {
			oldGroups[groupID] = struct{}{}
		}
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(desired)), ",")
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for groupID := range oldGroups {
		args := make([]any, 0, len(desired)+2)
		args = append(args, path, groupID)
		for _, desiredID := range desired {
			args = append(args, desiredID)
		}
		if _, err := tx.Exec(`UPDATE comments SET group_id=''
			WHERE path=? AND group_id=? AND id NOT IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}
	args := make([]any, 0, len(desired)+2)
	args = append(args, target, path)
	for _, desiredID := range desired {
		args = append(args, desiredID)
	}
	if _, err := tx.Exec(`UPDATE comments SET group_id=?
		WHERE path=? AND id IN (`+placeholders+`)`, args...); err != nil {
		return err
	}
	for groupID := range oldGroups {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM comments
			WHERE path=? AND group_id=?`, path, groupID).Scan(&count); err != nil {
			return err
		}
		if count < 2 {
			if _, err := tx.Exec(`UPDATE comments SET group_id='' WHERE path=? AND group_id=?`, path, groupID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *commentStore) cleanupGroup(path, groupID string) {
	if groupID == "" {
		return
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM comments
		WHERE path=? AND group_id=?`, path, groupID).Scan(&count); err != nil {
		return
	}
	if count < 2 {
		_, _ = s.db.Exec(`UPDATE comments SET group_id='' WHERE path=? AND group_id=?`, path, groupID)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// locateCompact falls back to letters/digits only. It handles rendered
// selections such as "Hello world" matching "**Hello** world" without needing
// a raw source position.
func locateCompact(content, quote string, nearLine int) (int, bool) {
	needle := make([]rune, 0, len(quote))
	for _, r := range quote {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			needle = append(needle, unicode.ToLower(r))
		}
	}
	if len(needle) < 2 {
		return 0, false
	}
	haystack := make([]rune, 0, len(content))
	lineOf := make([]int, 0, len(content))
	line := 1
	for _, r := range content {
		if r == '\n' {
			line++
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			haystack = append(haystack, unicode.ToLower(r))
			lineOf = append(lineOf, line)
		}
	}
	bestLine := 0
	bestDistance := int(^uint(0) >> 1)
	for i := 0; i+len(needle) <= len(haystack); i++ {
		matches := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		candidateLine := lineOf[i]
		if nearLine <= 0 {
			return candidateLine, true
		}
		distance := candidateLine - nearLine
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			bestDistance = distance
			bestLine = candidateLine
		}
	}
	if bestLine == 0 {
		return 0, false
	}
	return bestLine, true
}

// reanchorFromVersion is a fallback for comments whose stored quote/anchor no
// longer matches the raw Markdown literally (formatting changed, a previous
// partial rollback, or an agent rewrote the sentence). It looks for the raw
// section captured with an agent revision.
func (s *commentStore) reanchorFromVersion(comment *Comment, content string, versions map[string]docVersion) bool {
	for i := len(comment.Messages) - 1; i >= 0; i-- {
		message := comment.Messages[i]
		if message.Author != "agent" || message.VersionID == "" {
			continue
		}
		version, ok := versions[message.VersionID]
		if !ok {
			version, ok = s.version(message.VersionID)
			if !ok {
				continue
			}
			versions[version.ID] = version
		}
		section, ok := sectionFromVersion(version.Content, message, comment.Quote, comment.Line)
		if !ok {
			continue
		}
		start, end, line, orphaned := locateQuote(content, section, "", "", comment.Line)
		if orphaned {
			continue
		}
		comment.Anchor = section
		comment.Start, comment.End, comment.Line = start, end, line
		comment.Orphaned = false
		return true
	}
	return false
}

func commentAnchorCandidates(comment Comment) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 3)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(comment.Anchor)
	add(comment.Quote)
	for i := len(comment.Messages) - 1; i >= 0; i-- {
		message := comment.Messages[i]
		if message.Author == "agent" && message.After != "" {
			add(plainTextAnchor(message.After))
		}
	}
	return out
}

// plainTextAnchor converts a Markdown excerpt into a plain-text anchor that can
// be relocated in the rendered preview after an agent edit.
func plainTextAnchor(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			continue
		}
		line = imageRE.ReplaceAllString(line, "$1")
		line = linkRE.ReplaceAllString(line, "$1")
		line = htmlTagRE.ReplaceAllString(line, "")
		line = listMarkerRE.ReplaceAllString(line, "")
		line = strings.TrimLeft(line, "#> ")
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "`*_~")
		line = strings.ReplaceAll(line, "`", "")
		cleaned = append(cleaned, line)
	}
	text := strings.Join(cleaned, "\n")
	paragraphs := strings.Split(text, "\n\n")
	best := ""
	bestLen := 0
	for _, paragraph := range paragraphs {
		paragraph = strings.Join(strings.Fields(paragraph), " ")
		length := len([]rune(paragraph))
		if length > bestLen {
			best, bestLen = paragraph, length
		}
	}
	if bestLen > 400 {
		best = string([]rune(best)[:400])
	}
	return strings.TrimSpace(best)
}

// locateQuote maps a (possibly rendered/plain-text) quote back to the Markdown
// source. Prefix/suffix help with duplicate text; the stored line is a fallback.
func locateQuote(content, quote, prefix, suffix string, nearLine int) (start, end, line int, orphaned bool) {
	if content == "" || quote == "" {
		return 0, 0, 0, true
	}
	all := make([]int, 0, 2)
	offset := 0
	for {
		index := strings.Index(content[offset:], quote)
		if index < 0 {
			break
		}
		all = append(all, offset+index)
		offset += index + maxInt(1, len(quote))
		if len(all) > 200 {
			break
		}
	}
	if len(all) == 0 {
		return 0, 0, 0, true
	}
	best := all[0]
	bestScore := -1
	for _, index := range all {
		lineNo := 1 + strings.Count(content[:index], "\n")
		score := 0
		if nearLine > 0 {
			distance := lineNo - nearLine
			if distance < 0 {
				distance = -distance
			}
			score -= distance
		}
		if prefix != "" {
			startPrefix := index - len(prefix)
			if startPrefix >= 0 && content[startPrefix:index] == prefix {
				score += 20
			}
		}
		if suffix != "" {
			endSuffix := index + len(quote) + len(suffix)
			if endSuffix <= len(content) && content[index+len(quote):endSuffix] == suffix {
				score += 20
			}
		}
		if score > bestScore {
			best, bestScore = index, score
		}
	}
	return best, best + len(quote), 1 + strings.Count(content[:best], "\n"), false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// sectionRange returns the byte range of the blank-line-delimited Markdown
// block containing marker (an agent after excerpt) or, as a fallback, line.
// The block never includes the surrounding blank lines.
func sectionRange(content, marker, quote string, line int) (int, int, bool) {
	if content == "" {
		return 0, 0, false
	}
	index := -1
	for _, candidate := range []string{strings.TrimSpace(marker), strings.TrimSpace(quote)} {
		if candidate == "" {
			continue
		}
		if at := strings.Index(content, candidate); at >= 0 {
			index = at
			break
		}
	}
	if index < 0 {
		lines := strings.Split(content, "\n")
		if len(lines) == 0 {
			return 0, 0, false
		}
		lineNo := line
		if lineNo < 1 {
			lineNo = 1
		}
		if lineNo > len(lines) {
			lineNo = len(lines)
		}
		chosen := lineNo - 1
		if strings.TrimSpace(lines[chosen]) == "" {
			found := -1
			for delta := 1; delta < len(lines); delta++ {
				if next := chosen + delta; next < len(lines) && strings.TrimSpace(lines[next]) != "" {
					found = next
					break
				}
				if prev := chosen - delta; prev >= 0 && strings.TrimSpace(lines[prev]) != "" {
					found = prev
					break
				}
			}
			if found < 0 {
				return 0, 0, false
			}
			chosen = found
		}
		index = 0
		for i := 0; i < chosen; i++ {
			index += len(lines[i]) + 1
		}
	}
	return expandSection(content, index)
}

func expandSection(content string, index int) (int, int, bool) {
	if index < 0 || index >= len(content) {
		return 0, 0, false
	}
	lineStart := strings.LastIndex(content[:index], "\n") + 1
	start := lineStart
	for start > 0 {
		previousEnd := start - 1
		previousStart := strings.LastIndex(content[:previousEnd], "\n") + 1
		if strings.TrimSpace(content[previousStart:previousEnd]) == "" {
			break
		}
		start = previousStart
	}
	end := len(content)
	if relative := strings.Index(content[index:], "\n"); relative >= 0 {
		end = index + relative
	}
	for end < len(content) {
		nextStart := end + 1
		if nextStart >= len(content) {
			end = len(content)
			break
		}
		nextEnd := len(content)
		if relative := strings.Index(content[nextStart:], "\n"); relative >= 0 {
			nextEnd = nextStart + relative
		}
		if strings.TrimSpace(content[nextStart:nextEnd]) == "" {
			break
		}
		end = nextEnd
	}
	for end > start && (content[end-1] == '\n' || content[end-1] == '\r') {
		end--
	}
	if end <= start {
		return 0, 0, false
	}
	return start, end, true
}

func sectionFromVersion(content string, message CommentMessage, quote string, line int) (string, bool) {
	if message.SectionStart >= 0 && message.SectionEnd > message.SectionStart && message.SectionEnd <= len(content) {
		if text := strings.TrimRight(content[message.SectionStart:message.SectionEnd], "\r\n"); strings.TrimSpace(text) != "" {
			return text, true
		}
	}
	start, end, ok := sectionRange(content, message.After, quote, line)
	if !ok {
		return "", false
	}
	return strings.TrimRight(content[start:end], "\r\n"), true
}

func (s *commentStore) currentSectionFor(comment Comment, content string, versions map[string]docVersion) (string, bool) {
	for i := len(comment.Messages) - 1; i >= 0; i-- {
		message := comment.Messages[i]
		if message.Author != "agent" || message.VersionID == "" {
			continue
		}
		version, ok := versions[message.VersionID]
		if !ok {
			version, ok = s.version(message.VersionID)
			if !ok {
				continue
			}
			versions[version.ID] = version
		}
		if section, ok := sectionFromVersion(version.Content, message, comment.Quote, comment.Line); ok {
			if strings.Contains(content, section) {
				return section, true
			}
		}
	}
	start, end, ok := sectionRange(content, comment.Anchor, comment.Quote, comment.Line)
	if !ok {
		return "", false
	}
	return strings.TrimRight(content[start:end], "\r\n"), true
}

func replaceSection(content, current, target string, line int) (string, bool) {
	if current == "" {
		return content, false
	}
	positions := make([]int, 0, 1)
	offset := 0
	for {
		index := strings.Index(content[offset:], current)
		if index < 0 {
			break
		}
		positions = append(positions, offset+index)
		offset += index + len(current)
	}
	if len(positions) == 0 {
		return content, false
	}
	chosen := positions[0]
	if line > 0 && len(positions) > 1 {
		bestDistance := int(^uint(0) >> 1)
		for _, position := range positions {
			distance := strings.Count(content[:position], "\n") + 1 - line
			if distance < 0 {
				distance = -distance
			}
			if distance < bestDistance {
				bestDistance = distance
				chosen = position
			}
		}
	}
	return content[:chosen] + target + content[chosen+len(current):], true
}

func truncateForComment(value string) string {
	value = strings.TrimSpace(value)
	if runes := []rune(value); len(runes) > 600 {
		return string(runes[:600])
	}
	return value
}
