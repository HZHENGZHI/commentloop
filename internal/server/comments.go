package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxDocVersionsPerPath = 30

var (
	imageRE      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	linkRE       = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	htmlTagRE    = regexp.MustCompile(`<[^>]+>`)
	listMarkerRE = regexp.MustCompile(`^(?:[-*+]|\d+\.)\s+(?:\[[ xX]\]\s*)?`)
)

func newCommentStore(db *gorm.DB) *commentStore {
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

	row := commentRow{
		ID: comment.ID, Path: path, Quote: quote, Anchor: quote, Prefix: prefix, Suffix: suffix,
		Line: line, StartOffset: start, EndOffset: end, Orphaned: boolInt(orphaned),
		CreatedAt: formatTime(now), UpdatedAt: formatTime(now),
		DiscussionIDs: discussionIDsJSON(discussionIDs), PromptTemplateID: promptTemplateID,
	}
	messageRow := commentMessageRow{
		ID: message.ID, CommentID: comment.ID, Author: "user", Body: body, CreatedAt: formatTime(now),
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Create(&messageRow).Error
	})
	if err != nil {
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
	var ids []string
	if err := s.db.Model(&commentRow{}).Order("created_at").Pluck("id", &ids).Error; err != nil {
		return nil
	}
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
	var row commentRow
	if err := s.db.Take(&row, "id = ?", id).Error; err != nil {
		return Comment{}, false
	}
	comment := row.comment()
	messages, err := s.messages(comment.ID)
	if err != nil {
		return Comment{}, false
	}
	comment.Messages = messages
	return comment, true
}

func (s *commentStore) query(path string) ([]Comment, error) {
	var rows []commentRow
	if err := s.db.Where("path = ?", path).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	// One query for every thread on the document instead of one per comment.
	var messageRows []commentMessageRow
	if err := s.db.
		Where("comment_id IN (?)", s.db.Model(&commentRow{}).Select("id").Where("path = ?", path)).
		Order("created_at").Find(&messageRows).Error; err != nil {
		return nil, err
	}
	byComment := make(map[string][]CommentMessage, len(rows))
	for _, row := range messageRows {
		byComment[row.CommentID] = append(byComment[row.CommentID], row.message())
	}
	out := make([]Comment, 0, len(rows))
	for _, row := range rows {
		comment := row.comment()
		comment.Messages = byComment[comment.ID]
		out = append(out, comment)
	}
	return out, nil
}

func (r commentRow) comment() Comment {
	comment := Comment{
		ID: r.ID, Path: r.Path, Quote: r.Quote, Anchor: r.Anchor, GroupID: r.GroupID,
		Prefix: r.Prefix, Suffix: r.Suffix,
		Line: r.Line, Start: r.StartOffset, End: r.EndOffset,
		Resolved: r.Resolved != 0, Orphaned: r.Orphaned != 0,
		CreatedAt: parseTime(r.CreatedAt), UpdatedAt: parseTime(r.UpdatedAt),
		PromptTemplateID: r.PromptTemplateID,
	}
	if r.DiscussionIDs != "" && r.DiscussionIDs != "null" {
		_ = json.Unmarshal([]byte(r.DiscussionIDs), &comment.DiscussionIDs)
	}
	return comment
}

func (r commentMessageRow) message() CommentMessage {
	return CommentMessage{
		ID: r.ID, Author: r.Author, Body: r.Body, Before: r.BeforeText, After: r.AfterText,
		VersionID: r.VersionID, BeforeVersionID: r.BeforeVersionID,
		SectionStart: r.SectionStart, SectionEnd: r.SectionEnd, CreatedAt: parseTime(r.CreatedAt),
	}
}

func (s *commentStore) messages(commentID string) ([]CommentMessage, error) {
	var rows []commentMessageRow
	if err := s.db.Where("comment_id = ?", commentID).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []CommentMessage
	for _, row := range rows {
		out = append(out, row.message())
	}
	return out, nil
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
	resolved := comment.Resolved
	switch author {
	case "user":
		// A user reply reopens the review thread unless it explicitly resolves
		// it in the same request.
		resolved = resolve
	case "system":
		resolved = false
	}
	anchor := ""
	if strings.TrimSpace(after) != "" {
		anchor = plainTextAnchor(after)
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		versionID := ""
		beforeVersionID := ""
		sectionStart, sectionEnd := 0, 0
		var err error
		if author == "agent" && strings.TrimSpace(snapshot) != "" {
			versionID, err = saveDocVersion(tx, path, snapshot, now)
			if err != nil {
				return err
			}
			if start, end, ok := sectionRange(snapshot, after, comment.Quote, comment.Line); ok {
				sectionStart, sectionEnd = start, end
			}
		}
		if author == "agent" && strings.TrimSpace(beforeSnapshot) != "" {
			beforeVersionID, err = saveDocVersion(tx, path, beforeSnapshot, now)
			if err != nil {
				return err
			}
		}
		if err := tx.Create(&commentMessageRow{
			ID: commentID(), CommentID: id, Author: author, Body: body,
			BeforeText: before, AfterText: after, VersionID: versionID, BeforeVersionID: beforeVersionID,
			SectionStart: sectionStart, SectionEnd: sectionEnd, CreatedAt: formatTime(now),
		}).Error; err != nil {
			return err
		}
		changes := map[string]any{"updated_at": formatTime(now), "resolved": boolInt(resolved)}
		if anchor != "" {
			changes["anchor"] = anchor
		}
		if discussionIDs != nil {
			// nil means the client omitted the field (keep the old context); an
			// explicit empty list clears the thread's discussion references.
			changes["discussion_ids"] = discussionIDsJSON(discussionIDs)
		}
		if promptTemplateID != "" {
			changes["prompt_template_id"] = promptTemplateID
		}
		return tx.Model(&commentRow{}).Where("id = ?", id).Updates(changes).Error
	})
	if err != nil {
		return nil, err
	}
	updated, _ := s.get(path, id)
	return &updated, nil
}

func saveDocVersion(tx *gorm.DB, path, content string, now time.Time) (string, error) {
	// Keep rollback history bounded. Older snapshots (and rollback links on
	// their messages) are pruned first so a re-created identical snapshot gets
	// a fresh row.
	newest := tx.Model(&docVersionRow{}).Select("id").Where("path = ?", path).
		Order("created_at DESC, rowid DESC").Limit(maxDocVersionsPerPath - 1)
	if err := tx.Where("path = ? AND id NOT IN (?)", path, newest).Delete(&docVersionRow{}).Error; err != nil {
		return "", err
	}
	for _, column := range []string{"version_id", "before_version_id"} {
		if err := tx.Model(&commentMessageRow{}).
			Where(column+" != '' AND "+column+" NOT IN (?)", tx.Model(&docVersionRow{}).Select("id")).
			Update(column, "").Error; err != nil {
			return "", err
		}
	}
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&docVersionRow{
		ID: "ver_" + commentID(), Path: path, Hash: hash, Content: content, CreatedAt: formatTime(now),
	}).Error; err != nil {
		return "", err
	}
	var row docVersionRow
	if err := tx.Select("id").Take(&row, "path = ? AND hash = ?", path, hash).Error; err != nil {
		return "", err
	}
	return row.ID, nil
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
	var row docVersionRow
	if err := s.db.Select("id", "path", "content", "created_at").Take(&row, "id = ?", id).Error; err != nil {
		return docVersion{}, false
	}
	return docVersion{ID: row.ID, Path: row.Path, Content: row.Content, CreatedAt: parseTime(row.CreatedAt)}, true
}

func (s *commentStore) resolve(path, id string, resolved bool) (*Comment, error) {
	comment, ok := s.get(path, id)
	if !ok {
		return nil, os.ErrNotExist
	}
	now := time.Now()
	if err := s.db.Model(&commentRow{}).Where("id = ?", id).Updates(map[string]any{
		"resolved": boolInt(resolved), "updated_at": formatTime(now),
	}).Error; err != nil {
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
	result := s.db.Where("id = ? AND path = ?", id, path).Delete(&commentRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return os.ErrNotExist
	}
	s.cleanupGroup(path, comment.GroupID)
	// Drop snapshots that only belonged to the deleted comment. Versions shared
	// by other comments remain available.
	_ = s.db.Where("path = ? AND id NOT IN (?) AND id NOT IN (?)", path,
		s.db.Model(&commentMessageRow{}).Select("version_id").Where("version_id != ''"),
		s.db.Model(&commentMessageRow{}).Select("before_version_id").Where("before_version_id != ''"),
	).Delete(&docVersionRow{}).Error
	return nil
}

// setGroup links comments as one coordinated review request. The selected
// comment and memberIDs become the complete membership of one group; any
// previous group members that were not selected leave the group.
func (s *commentStore) setGroup(path, id string, memberIDs []string) error {
	var rows []commentRow
	if err := s.db.Select("id", "group_id").Where("path = ?", path).Find(&rows).Error; err != nil {
		return err
	}
	groups := make(map[string]string, len(rows))
	for _, row := range rows {
		groups[row.ID] = row.GroupID
	}
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
		if err := s.db.Model(&commentRow{}).Where("path = ? AND id = ?", path, id).
			Update("group_id", "").Error; err != nil {
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
	return s.db.Transaction(func(tx *gorm.DB) error {
		for groupID := range oldGroups {
			if err := tx.Model(&commentRow{}).
				Where("path = ? AND group_id = ? AND id NOT IN ?", path, groupID, desired).
				Update("group_id", "").Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&commentRow{}).Where("path = ? AND id IN ?", path, desired).
			Update("group_id", target).Error; err != nil {
			return err
		}
		for groupID := range oldGroups {
			if err := dissolveSmallGroup(tx, path, groupID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *commentStore) cleanupGroup(path, groupID string) {
	if groupID == "" {
		return
	}
	_ = dissolveSmallGroup(s.db, path, groupID)
}

// dissolveSmallGroup clears a group that no longer links at least two comments.
func dissolveSmallGroup(db *gorm.DB, path, groupID string) error {
	var count int64
	if err := db.Model(&commentRow{}).Where("path = ? AND group_id = ?", path, groupID).
		Count(&count).Error; err != nil {
		return err
	}
	if count >= 2 {
		return nil
	}
	return db.Model(&commentRow{}).Where("path = ? AND group_id = ?", path, groupID).
		Update("group_id", "").Error
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
