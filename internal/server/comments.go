package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hzhengzhi/commentloop/internal/anchor"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxDocVersionsPerPath = 30

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

func (s *commentStore) createAs(author, path, quote, prefix, suffix, body, severity, content string, discussionIDs []string, promptTemplateID string) (*Comment, error) {
	quote = strings.TrimSpace(quote)
	body = strings.TrimSpace(body)
	severity, err := normalizeSeverity(severity)
	if err != nil {
		return nil, err
	}
	if author != "agent" {
		author = "user"
	}
	if quote == "" {
		return nil, fmt.Errorf("请选择要评论的文本")
	}
	if body == "" {
		return nil, fmt.Errorf("评论内容不能为空")
	}
	if len(quote) > 4000 || len(body) > 8000 || len(prefix) > 500 || len(suffix) > 500 {
		return nil, fmt.Errorf("评论内容过长")
	}
	start, end, line, orphaned := anchor.LocateQuote(content, quote, prefix, suffix, 0)
	now := time.Now()
	comment := Comment{
		ID: commentID(), Path: path, Quote: quote, Prefix: prefix, Suffix: suffix,
		Start: start, End: end, Line: line, Orphaned: orphaned,
		Anchor: quote, CreatedAt: now, UpdatedAt: now, Severity: severity,
	}
	message := CommentMessage{ID: commentID(), Author: author, Body: body, CreatedAt: now}

	row := commentRow{
		ID: comment.ID, Path: path, Quote: quote, Anchor: quote, Prefix: prefix, Suffix: suffix,
		Line: line, StartOffset: start, EndOffset: end, Orphaned: boolInt(orphaned),
		Severity: severity, CreatedAt: formatTime(now), UpdatedAt: formatTime(now),
		DiscussionIDs: discussionIDsJSON(discussionIDs), PromptTemplateID: promptTemplateID,
	}
	messageRow := commentMessageRow{
		ID: message.ID, CommentID: comment.ID, Author: author, Body: body, CreatedAt: formatTime(now),
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
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

func (s *commentStore) setReaction(path, id, reaction string) (Comment, error) {
	if reaction != "" && reaction != "👍" && reaction != "✅" && reaction != "👀" {
		return Comment{}, fmt.Errorf("无效的表情回复")
	}
	if _, ok := s.get(path, id); !ok {
		return Comment{}, os.ErrNotExist
	}
	if err := s.db.Model(&commentRow{}).Where("id = ? AND path = ?", id, path).Updates(map[string]any{
		"reaction": reaction, "updated_at": formatTime(time.Now()),
	}).Error; err != nil {
		return Comment{}, err
	}
	comment, ok := s.get(path, id)
	if !ok {
		return Comment{}, os.ErrNotExist
	}
	return comment, nil
}

func (s *commentStore) setSuggestion(path, id, quote, text, mode string) (Comment, error) {
	quote = strings.TrimSpace(quote)
	text = strings.TrimSpace(text)
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "replace"
	}
	if quote == "" || text == "" {
		return Comment{}, fmt.Errorf("建议内容不能为空")
	}
	if mode != "replace" && mode != "append" {
		return Comment{}, fmt.Errorf("无效的建议类型")
	}
	if _, ok := s.get(path, id); !ok {
		return Comment{}, os.ErrNotExist
	}
	if err := s.db.Model(&commentRow{}).Where("id = ? AND path = ?", id, path).Updates(map[string]any{
		"suggestion_quote": quote, "suggestion_text": text,
		"suggestion_mode": mode, "suggestion_status": "pending",
		"updated_at": formatTime(time.Now()),
	}).Error; err != nil {
		return Comment{}, err
	}
	comment, ok := s.get(path, id)
	if !ok {
		return Comment{}, os.ErrNotExist
	}
	return comment, nil
}

func (s *commentStore) reanchorAfterSuggestion(path, id, quote string, start, end, line int) error {
	return s.db.Model(&commentRow{}).Where("id = ? AND path = ?", id, path).Updates(map[string]any{
		"quote": quote, "anchor": quote, "start_offset": start, "end_offset": end,
		"line": line, "prefix": "", "suffix": "", "orphaned": 0,
		"updated_at": formatTime(time.Now()),
	}).Error
}

func (s *commentStore) setSuggestionStatus(path, id, status string) (Comment, error) {
	if status != "accepted" && status != "rejected" {
		return Comment{}, fmt.Errorf("无效的建议状态")
	}
	if _, ok := s.get(path, id); !ok {
		return Comment{}, os.ErrNotExist
	}
	if err := s.db.Model(&commentRow{}).Where("id = ? AND path = ?", id, path).Updates(map[string]any{
		"suggestion_status": status, "updated_at": formatTime(time.Now()),
	}).Error; err != nil {
		return Comment{}, err
	}
	comment, ok := s.get(path, id)
	if !ok {
		return Comment{}, os.ErrNotExist
	}
	return comment, nil
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
				start, end, line, orphaned := anchor.LocateQuote(content, candidate, comments[i].Prefix, comments[i].Suffix, comments[i].Line)
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
				if line, ok := anchor.LocateCompact(content, comments[i].Quote, comments[i].Line); ok {
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
		PromptTemplateID: r.PromptTemplateID, Severity: r.Severity, Reaction: r.Reaction,
	}
	if r.SuggestionQuote != "" {
		comment.Suggestion = &CommentSuggestion{
			Quote: r.SuggestionQuote, Text: r.SuggestionText,
			Mode: r.SuggestionMode, Status: r.SuggestionStatus,
		}
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
	anchorText := ""
	if strings.TrimSpace(after) != "" {
		anchorText = anchor.PlainText(after)
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		versionID := ""
		beforeVersionID := ""
		sectionStart, sectionEnd := 0, 0
		var err error
		if author == "agent" && strings.TrimSpace(snapshot) != "" {
			versionID, err = saveDocVersion(tx, path, snapshot, now, false, "comment")
			if err != nil {
				return err
			}
			if start, end, ok := anchor.SectionRange(snapshot, after, comment.Quote, comment.Line); ok {
				sectionStart, sectionEnd = start, end
			}
		}
		if author == "agent" && strings.TrimSpace(beforeSnapshot) != "" {
			beforeVersionID, err = saveDocVersion(tx, path, beforeSnapshot, now, false, "comment")
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
		if anchorText != "" {
			changes["anchor"] = anchorText
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

func saveDocVersion(tx *gorm.DB, path, content string, now time.Time, touch bool, source string) (string, error) {
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	var existing docVersionRow
	if err := tx.Select("id", "source").Take(&existing, "path = ? AND hash = ?", path, hash).Error; err == nil {
		updates := map[string]any{}
		if touch {
			updates["created_at"] = formatTime(now)
		}
		if source == "doc" && existing.Source != "doc" {
			updates["source"] = "doc"
		}
		if len(updates) > 0 {
			if err := tx.Model(&docVersionRow{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return "", err
			}
		}
		return existing.ID, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", err
	}
	// Keep rollback history bounded. Older snapshots (and rollback links on
	// their messages) are pruned before adding a genuinely new snapshot.
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
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&docVersionRow{
		ID: "ver_" + commentID(), Path: path, Hash: hash, Content: content, Source: source, CreatedAt: formatTime(now),
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

func (s *commentStore) listVersions(path string) ([]docVersionInfo, error) {
	var rows []docVersionRow
	if err := s.db.Select("id", "content", "created_at").Where("path = ?", path).
		Order("created_at DESC, rowid DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]docVersionInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, docVersionInfo{ID: row.ID, CreatedAt: parseTime(row.CreatedAt), Size: int64(len(row.Content))})
	}
	return out, nil
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
	// Drop snapshots that only belonged to the deleted comment and were never
	// promoted to a normal document revision. Document history (source=doc)
	// survives comment deletion.
	_ = s.db.Where("path = ? AND source != ? AND id NOT IN (?) AND id NOT IN (?)", path, "doc",
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
		section, ok := anchor.SectionFromVersion(
			version.Content, message.After, message.SectionStart, message.SectionEnd,
			comment.Quote, comment.Line,
		)
		if !ok {
			continue
		}
		start, end, line, orphaned := anchor.LocateQuote(content, section, "", "", comment.Line)
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
			add(anchor.PlainText(message.After))
		}
	}
	return out
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
		if section, ok := anchor.SectionFromVersion(
			version.Content, message.After, message.SectionStart, message.SectionEnd,
			comment.Quote, comment.Line,
		); ok {
			if strings.Contains(content, section) {
				return section, true
			}
		}
	}
	start, end, ok := anchor.SectionRange(content, comment.Anchor, comment.Quote, comment.Line)
	if !ok {
		return "", false
	}
	return strings.TrimRight(content[start:end], "\r\n"), true
}

func truncateForComment(value string) string {
	value = strings.TrimSpace(value)
	if runes := []rune(value); len(runes) > 600 {
		return string(runes[:600])
	}
	return value
}
