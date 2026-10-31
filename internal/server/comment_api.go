package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hzhengzhi/commentloop/internal/anchor"
)

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
		if _, known := s.agentGrantForRequest(r); known {
			message := "受管 Agent 任务必须指定关联文档"
			s.recordAgentDenial(r, message)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
			return
		}
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
			writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
			return
		}
		canonical = ro.ID + "/" + rel
		content = ""
	}
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能读取关联文档的评论") {
		return
	}
	comments := s.comments.list(canonical, content)
	if comments == nil {
		comments = []Comment{}
	}
	writeJSON(w, http.StatusOK, commentsResponse{
		Path: canonical, Comments: comments,
	})
}

func (s *Server) validatePromptTemplateID(id string, useDefault bool) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if useDefault {
			return defaultPromptTemplateID, nil
		}
		return "", nil
	}
	if _, ok := s.prompts.get(id); !ok {
		return "", fmt.Errorf("提示词模板不存在")
	}
	return id, nil
}

func (s *Server) validateDiscussionIDs(docPath string, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range normalizeDiscussionIDs(ids) {
		if _, ok := s.discussions.get(docPath, id); !ok {
			return nil, fmt.Errorf("讨论会话不属于当前文档")
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentWrite(w, r) {
		return
	}
	var req createCommentRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	canonical, content, err := s.commentDoc(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errCouldNotReadDocumentLocate)
		return
	}
	discussionIDs, err := s.validateDiscussionIDs(canonical, req.DiscussionIDs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	templateID, err := s.validatePromptTemplateID(req.PromptTemplateID, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	comment, err := s.comments.create(canonical, req.Quote, req.Prefix, req.Suffix, req.Body, content, discussionIDs, templateID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	s.noteReviewCommentAdded(canonical)
	_ = s.enqueueAutoReview(canonical, comment.ID, false, promptLanguage(r))
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) handleReplyComment(w http.ResponseWriter, r *http.Request) {
	var req replyCommentRequest
	if !bindJSON(w, r, 2*maxDocSize+(1<<20), &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能回复关联文档的评论") {
		return
	}
	author := req.Author
	if author != "user" && author != "agent" {
		author = "agent"
	}
	if author == "user" {
		// A user replying with further feedback reopens the review thread.
		_, _ = s.comments.resolve(canonical, req.ID, false)
	}
	if author == "agent" && s.rejectAgentCommentReply(w, r) {
		return
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
	discussionIDs, err := s.validateDiscussionIDs(canonical, req.DiscussionIDs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	templateID, err := s.validatePromptTemplateID(req.PromptTemplateID, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	comment, err := s.comments.reply(canonical, req.ID, author, req.Body,
		req.Before, req.After, req.BeforeSnapshot, snapshot, req.Resolve, discussionIDs, templateID)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errCommentNotFound)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	if author == "user" {
		s.noteReviewCommentAdded(canonical)
		_ = s.enqueueAutoReview(canonical, comment.ID, false, promptLanguage(r))
	}
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) handleTriggerCommentAgent(w http.ResponseWriter, r *http.Request) {
	var req triggerCommentRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if _, ok := s.comments.get(canonical, req.ID); !ok {
		writeError(w, http.StatusNotFound, errCommentNotFound)
		return
	}
	if err := s.enqueueAutoReview(canonical, req.ID, true, promptLanguage(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	var req resolveCommentRequest
	if !bindJSON(w, r, defaultJSONBodyLimit, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	comment, err := s.comments.resolve(canonical, req.ID, req.Resolved)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errCommentNotFound)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, comment)
}

func (s *Server) handleGroupComment(w http.ResponseWriter, r *http.Request) {
	var req groupCommentRequest
	if !bindJSON(w, r, 32*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.comments.setGroup(canonical, req.ID, req.MemberIDs); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errCommentNotFound)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRollbackComment(w http.ResponseWriter, r *http.Request) {
	var req rollbackCommentRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	comment, ok := s.comments.get(canonical, req.ID)
	if !ok {
		writeError(w, http.StatusNotFound, errCommentNotFound)
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
		writeError(w, http.StatusBadRequest, errEditRoundNotFound)
		return
	}
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side == "" {
		side = "after"
	}
	if side != "before" && side != "after" {
		writeError(w, http.StatusBadRequest, errRollbackDirectionMustBeforeAfter)
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
		writeError(w, http.StatusBadRequest, errEditRoundHasNoLater)
		return
	}
	if side == "before" && target.BeforeVersionID == "" && strings.TrimSpace(target.Before) == "" {
		writeError(w, http.StatusBadRequest, errEditRoundHasNoEarlier)
		return
	}
	current, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		writeError(w, http.StatusNotFound, errDocumentNoLongerExistsCannot)
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
			writeErrorDetail(w, http.StatusInternalServerError, errFailedReadLinkedCommentVersions, err.Error())
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
					if start, end, found := anchor.SectionRange(beforeVersion.Content,
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
				if section, found := anchor.SectionFromVersion(
					version.Content, targetMessage.After,
					targetMessage.SectionStart, targetMessage.SectionEnd,
					item.Comment.Quote, item.Comment.Line,
				); found {
					targetSection = section
				}
			}
		}
		if strings.TrimSpace(targetSection) == "" {
			if side == "before" {
				writeError(w, http.StatusConflict, errCouldNotLocateOriginalParagraph)
			} else {
				writeError(w, http.StatusConflict, errCouldNotLocateEditedParagraph)
			}
			return
		}
		currentComment := item.Comment
		if anchored, ok := byID[item.Comment.ID]; ok {
			currentComment = anchored
		}
		currentSection, ok := s.comments.currentSectionFor(currentComment, string(current), versions)
		if !ok || strings.TrimSpace(currentSection) == "" {
			writeError(w, http.StatusConflict, errCouldNotLocateParagraphComment)
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
		updated, ok := anchor.ReplaceSection(newContent, item.current, item.target, item.line)
		if !ok {
			writeError(w, http.StatusConflict, errParagraphRollBackHasConflicting)
			return
		}
		newContent = updated
	}
	_, statErr := s.statRootFile(ro, rel)
	existed := statErr == nil
	doc, err := s.persistDocument(ro, rel, canonical, newContent, "rollback", existed)
	if err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errRollbackWriteFailed, err.Error())
		return
	}
	var updated *Comment
	for _, item := range records {
		sideLabel := "后"
		if side == "before" {
			sideLabel = "前"
		}
		body := fmt.Sprintf("已局部回滚到第 %d 次修改%s的该评论段落", item.round, sideLabel)
		if promptLanguage(r) == "en" {
			sideLabel = "after"
			if side == "before" {
				sideLabel = "before"
			}
			body = fmt.Sprintf("Rolled back this comment paragraph to the %s side of revision %d.", sideLabel, item.round)
		}
		record, err := s.comments.reply(
			canonical, item.comment.ID, "system", body, item.before, item.after, "", "",
			false, nil, "",
		)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, errRollbackRecordFailed, err.Error())
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
		writeError(w, http.StatusInternalServerError, errRollbackCommentMissing)
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	writeJSON(w, http.StatusOK, rollbackCommentResponse{
		Round: round, Comment: *updated, Doc: doc,
	})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.comments.delete(canonical, r.URL.Query().Get("id")); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errCommentNotFound)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "comments", Path: canonical})
	w.WriteHeader(http.StatusNoContent)
}
