package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type createCommentRequest struct {
	Path   string `json:"path"`
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	Body   string `json:"body"`
}

type replyCommentRequest struct {
	Path           string `json:"path"`
	ID             string `json:"id"`
	Author         string `json:"author"`
	Body           string `json:"body"`
	Before         string `json:"before"`
	After          string `json:"after"`
	BeforeSnapshot string `json:"beforeSnapshot"`
	Snapshot       string `json:"snapshot"`
	Resolve        bool   `json:"resolve"`
}

type resolveCommentRequest struct {
	Path     string `json:"path"`
	ID       string `json:"id"`
	Resolved bool   `json:"resolved"`
}

type groupCommentRequest struct {
	Path      string   `json:"path"`
	ID        string   `json:"id"`
	MemberIDs []string `json:"memberIds"`
}

type rollbackCommentRequest struct {
	Path      string `json:"path"`
	ID        string `json:"id"`
	MessageID string `json:"messageId"`
	Side      string `json:"side"`
}

type rollbackCommentResponse struct {
	Round   int         `json:"round"`
	Comment Comment     `json:"comment"`
	Doc     docResponse `json:"doc"`
}

type commentsResponse struct {
	Path     string    `json:"path"`
	Comments []Comment `json:"comments"`
}

func (s *Server) commentDoc(path string) (canonical, content string, err error) {
	ro, rel, err := s.resolveDocRef(path)
	if err != nil {
		return "", "", err
	}
	canonical = ro.ID + "/" + rel
	data, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		return canonical, "", err
	}
	return canonical, string(data), nil
}

func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		comments := s.comments.all()
		if comments == nil {
			comments = []Comment{}
		}
		writeJSON(w, http.StatusOK, commentsResponse{Comments: comments})
		return
	}
	canonical, content, err := s.commentDoc(path)
	if err != nil {
		// Comments remain visible even when a remote file is temporarily
		// unreadable: keep the stored anchors.
		ro, rel, resolveErr := s.resolveDocRef(path)
		if resolveErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
			return
		}
		canonical = ro.ID + "/" + rel
		content = ""
	}
	comments := s.comments.list(canonical, content)
	if comments == nil {
		comments = []Comment{}
	}
	writeJSON(w, http.StatusOK, commentsResponse{
		Path: canonical, Comments: comments,
	})
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req createCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	canonical, content, err := s.commentDoc(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法读取文档，不能定位评论位置"})
		return
	}
	comment, err := s.comments.create(canonical, req.Quote, req.Prefix, req.Suffix, req.Body, content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	_ = s.enqueueAutoReview(canonical, comment.ID, false)
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) handleReplyComment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxDocSize+(1<<20))
	var req replyCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	author := req.Author
	if author != "user" && author != "agent" {
		author = "agent"
	}
	if author == "user" {
		// A user replying with further feedback reopens the review thread.
		_, _ = s.comments.resolve(canonical, req.ID, false)
	}
	snapshot := req.Snapshot
	if author == "agent" && strings.TrimSpace(snapshot) == "" {
		// Older MCP/CLI clients (and manual reply_comment calls) do not send a
		// snapshot. Capture the current disk document so their revisions are
		// still rollbackable.
		if data, _, readErr := s.readRootFile(ro, rel, maxDocSize); readErr == nil {
			snapshot = string(data)
		}
	}
	comment, err := s.comments.reply(canonical, req.ID, author, req.Body,
		req.Before, req.After, req.BeforeSnapshot, snapshot, req.Resolve)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "评论不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	if author == "user" {
		_ = s.enqueueAutoReview(canonical, comment.ID, false)
	}
	writeJSON(w, http.StatusOK, comment)
}

type triggerCommentRequest struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

func (s *Server) handleTriggerCommentAgent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req triggerCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	if _, ok := s.comments.get(canonical, req.ID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "评论不存在"})
		return
	}
	if err := s.enqueueAutoReview(canonical, req.ID, true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	var req resolveCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	comment, err := s.comments.resolve(canonical, req.ID, req.Resolved)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "评论不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) handleGroupComment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var req groupCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.comments.setGroup(canonical, req.ID, req.MemberIDs); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "评论不存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRollbackComment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req rollbackCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	comment, ok := s.comments.get(canonical, req.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "评论不存在"})
		return
	}
	var target *CommentMessage
	round := 0
	for i := range comment.Messages {
		message := &comment.Messages[i]
		if message.Author != "agent" {
			continue
		}
		round++
		if message.ID == req.MessageID {
			target = message
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "找不到这一轮修改"})
		return
	}
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side == "" {
		side = "after"
	}
	if side != "before" && side != "after" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "回滚方向必须是 before 或 after"})
		return
	}
	var version docVersion
	haveAfterVersion := false
	if target.VersionID != "" {
		version, haveAfterVersion = s.comments.version(target.VersionID)
		if haveAfterVersion && version.Path != canonical {
			haveAfterVersion = false
		}
	}
	if side == "after" && !haveAfterVersion {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "这一轮修改没有可恢复的修改后快照"})
		return
	}
	if side == "before" && target.BeforeVersionID == "" && strings.TrimSpace(target.Before) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "这一轮修改没有可恢复的修改前内容"})
		return
	}
	current, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "当前文档不存在，无法回滚"})
		return
	}
	currentComments := s.comments.list(canonical, string(current))
	byID := make(map[string]Comment, len(currentComments))
	for _, item := range currentComments {
		byID[item.ID] = item
	}
	targets := []versionTarget{}
	if haveAfterVersion {
		targets, err = s.comments.versionTargets(canonical, version.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取版本关联评论失败：" + err.Error()})
			return
		}
	}
	filtered := make([]versionTarget, 0, len(targets))
	for _, item := range targets {
		if item.Comment.ID == comment.ID ||
			(comment.GroupID != "" && item.Comment.GroupID == comment.GroupID) {
			filtered = append(filtered, item)
		}
	}
	targets = filtered
	if len(targets) == 0 {
		targets = []versionTarget{{Comment: comment, Message: *target, Round: round}}
	}
	// Put the clicked comment first so, if several related comments resolve to
	// the same paragraph, its version wins.
	ordered := make([]versionTarget, 0, len(targets))
	for _, item := range targets {
		if item.Comment.ID == comment.ID {
			ordered = append(ordered, item)
		}
	}
	for _, item := range targets {
		if item.Comment.ID != comment.ID {
			ordered = append(ordered, item)
		}
	}

	type replacement struct {
		current string
		target  string
		line    int
	}
	type rollbackRecord struct {
		comment Comment
		round   int
		before  string
		after   string
	}
	replacements := make([]replacement, 0, len(ordered))
	records := make([]rollbackRecord, 0, len(ordered))
	seenSections := make(map[string]string)
	versions := map[string]docVersion{}
	if haveAfterVersion {
		versions[version.ID] = version
	}
	for _, item := range ordered {
		targetMessage := item.Message
		targetRound := item.Round
		if item.Comment.ID == comment.ID {
			targetMessage = *target
			targetRound = round
		}
		var targetSection string
		if side == "before" {
			if targetMessage.BeforeVersionID != "" {
				beforeVersion, ok := versions[targetMessage.BeforeVersionID]
				if !ok {
					beforeVersion, ok = s.comments.version(targetMessage.BeforeVersionID)
					if ok && beforeVersion.Path == canonical {
						versions[beforeVersion.ID] = beforeVersion
					} else {
						ok = false
					}
				}
				if ok {
					if start, end, found := sectionRange(beforeVersion.Content,
						targetMessage.Before, item.Comment.Quote, item.Comment.Line); found {
						targetSection = beforeVersion.Content[start:end]
					}
				}
			}
			if strings.TrimSpace(targetSection) == "" {
				targetSection = strings.TrimSpace(targetMessage.Before)
			}
		} else {
			if haveAfterVersion {
				if section, found := sectionFromVersion(
					version.Content, targetMessage, item.Comment.Quote, item.Comment.Line,
				); found {
					targetSection = section
				}
			}
		}
		if strings.TrimSpace(targetSection) == "" {
			if side == "before" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "无法定位这一轮修改前的段落，未做回滚"})
			} else {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "无法定位这一轮修改后的段落，未做回滚"})
			}
			return
		}
		currentComment := item.Comment
		if anchored, ok := byID[item.Comment.ID]; ok {
			currentComment = anchored
		}
		currentSection, ok := s.comments.currentSectionFor(currentComment, string(current), versions)
		if !ok || strings.TrimSpace(currentSection) == "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "无法定位当前评论所在的段落，未做回滚"})
			return
		}
		if chosenTarget, seen := seenSections[currentSection]; seen {
			targetSection = chosenTarget
		} else {
			seenSections[currentSection] = targetSection
			if currentSection != targetSection {
				replacements = append(replacements, replacement{
					current: currentSection,
					target:  targetSection,
					line:    currentComment.Line,
				})
			}
		}
		records = append(records, rollbackRecord{
			comment: item.Comment,
			round:   targetRound,
			before:  truncateForComment(currentSection),
			after:   truncateForComment(targetSection),
		})
	}
	newContent := string(current)
	for _, item := range replacements {
		updated, ok := replaceSection(newContent, item.current, item.target, item.line)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "回滚段落发生冲突，未做回滚"})
			return
		}
		newContent = updated
	}
	_, statErr := s.statRootFile(ro, rel)
	existed := statErr == nil
	doc, err := s.persistDocument(ro, rel, canonical, newContent, "rollback", existed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "回滚写入失败：" + err.Error()})
		return
	}
	var updated *Comment
	for _, item := range records {
		sideLabel := "后"
		if side == "before" {
			sideLabel = "前"
		}
		body := fmt.Sprintf("已局部回滚到第 %d 次修改%s的该评论段落", item.round, sideLabel)
		record, err := s.comments.reply(
			canonical, item.comment.ID, "system", body, item.before, item.after, "", "",
			false,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "文档已局部回滚，但写入回滚记录失败：" + err.Error()})
			return
		}
		if item.comment.ID == comment.ID {
			updated = record
		}
	}
	if updated == nil {
		if fetched, ok := s.comments.get(canonical, comment.ID); ok {
			updated = &fetched
		}
	}
	if updated == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "文档已局部回滚，但评论记录已不存在"})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, rollbackCommentResponse{
		Round: round, Comment: *updated, Doc: doc,
	})
}

func commentExcerpt(content, quote string, line int) string {
	if content == "" {
		return ""
	}
	lineNo := line
	if index := strings.Index(content, quote); quote != "" && index >= 0 {
		lineNo = 1 + strings.Count(content[:index], "\n")
	}
	lines := strings.Split(content, "\n")
	if lineNo < 1 {
		lineNo = 1
	}
	if lineNo > len(lines) {
		lineNo = len(lines)
	}
	index := lineNo - 1
	if strings.TrimSpace(lines[index]) == "" {
		for i := index - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				index = i
				break
			}
		}
	}
	start := index
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	end := index
	for end < len(lines)-1 && strings.TrimSpace(lines[end+1]) != "" {
		end++
	}
	text := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
	if runes := []rune(text); len(runes) > 600 {
		text = string(runes[:600])
	}
	return text
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径无效或不是 Markdown 文件"})
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.comments.delete(canonical, r.URL.Query().Get("id")); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "评论不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	w.WriteHeader(http.StatusNoContent)
}
