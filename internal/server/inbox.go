package server

import (
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// inboxDoc is the review state of one document, aggregated across its comment
// threads. It is what the sidebar and the cross-document inbox render.
type inboxDoc struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	RelPath      string    `json:"relPath"`
	RootID       string    `json:"rootId"`
	RootName     string    `json:"rootName"`
	Status       string    `json:"status"`
	Comments     int       `json:"comments"`
	Unresolved   int       `json:"unresolved"`
	Awaiting     int       `json:"awaiting"`
	PendingAgent int       `json:"pendingAgent"`
	AgentWorking int       `json:"agentWorking"`
	Orphaned     int       `json:"orphaned"`
	LastActivity time.Time `json:"lastActivity"`
}

type inboxCounts struct {
	Awaiting         int `json:"awaiting"`
	PendingAgent     int `json:"pendingAgent"`
	AgentWorking     int `json:"agentWorking"`
	Draft            int `json:"draft"`
	InReview         int `json:"inReview"`
	ChangesRequested int `json:"changesRequested"`
	Approved         int `json:"approved"`
}

type inboxResponse struct {
	Docs   []inboxDoc  `json:"docs"`
	Counts inboxCounts `json:"counts"`
}

func (s *Server) handleInbox(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.buildInbox())
}

func (s *Server) buildInbox() inboxResponse {
	docs := make(map[string]*inboxDoc)
	reviews, _ := s.reviews.all()

	var commentRows []commentRow
	_ = s.db.Select("id", "path", "resolved", "orphaned", "created_at", "updated_at").Find(&commentRows).Error

	lastAuthor := make(map[string]string, len(commentRows))
	lastMessageAt := make(map[string]time.Time, len(commentRows))
	var messageRows []commentMessageRow
	_ = s.db.Select("comment_id", "author", "created_at").Order("created_at, id").Find(&messageRows).Error
	for _, row := range messageRows {
		lastAuthor[row.CommentID] = row.Author
		lastMessageAt[row.CommentID] = parseTime(row.CreatedAt)
	}

	workingComments := make(map[string]bool)
	var jobRows []agentJobRow
	_ = s.db.Select("comment_id").Where("status IN ?", []string{"queued", "running"}).Find(&jobRows).Error
	for _, row := range jobRows {
		if row.CommentID != "" {
			workingComments[row.CommentID] = true
		}
	}

	for _, row := range commentRows {
		doc := ensureInboxDoc(docs, row.Path)
		if doc == nil {
			continue
		}
		doc.Comments++
		activity := parseTime(row.UpdatedAt)
		if messageAt, ok := lastMessageAt[row.ID]; ok && messageAt.After(activity) {
			activity = messageAt
		}
		if activity.After(doc.LastActivity) {
			doc.LastActivity = activity
		}
		if row.Resolved != 0 {
			continue
		}
		doc.Unresolved++
		if row.Orphaned != 0 {
			doc.Orphaned++
		}
		switch {
		case workingComments[row.ID]:
			doc.AgentWorking++
		case lastAuthor[row.ID] == "agent" || lastAuthor[row.ID] == "system":
			doc.Awaiting++
		case lastAuthor[row.ID] == "user":
			doc.PendingAgent++
		}
	}

	for docPath, review := range reviews {
		doc := ensureInboxDoc(docs, docPath)
		if doc == nil {
			continue
		}
		if review.UpdatedAt.After(doc.LastActivity) {
			doc.LastActivity = review.UpdatedAt
		}
	}

	list := make([]inboxDoc, 0, len(docs))
	for docPath, doc := range docs {
		review, ok := reviews[docPath]
		switch {
		case ok:
			doc.Status = review.Status
		case doc.Comments > 0:
			doc.Status = reviewStatusInReview
		default:
			doc.Status = reviewStatusDraft
		}
		ro := s.rootByID(doc.RootID)
		if ro == nil {
			continue
		}
		if ro.Kind == rootKindLocal && !pathExists(filepath.Join(ro.Path, filepath.FromSlash(doc.RelPath))) {
			continue
		}
		doc.RootName = ro.Name
		list = append(list, *doc)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].LastActivity.Equal(list[j].LastActivity) {
			return list[i].Path < list[j].Path
		}
		return list[i].LastActivity.After(list[j].LastActivity)
	})
	var counts inboxCounts
	for _, doc := range list {
		switch doc.Status {
		case reviewStatusDraft:
			counts.Draft++
		case reviewStatusInReview:
			counts.InReview++
		case reviewStatusChangesRequested:
			counts.ChangesRequested++
		case reviewStatusApproved:
			counts.Approved++
		}
		counts.Awaiting += doc.Awaiting
		counts.PendingAgent += doc.PendingAgent
		counts.AgentWorking += doc.AgentWorking
	}
	return inboxResponse{Docs: list, Counts: counts}
}

// ensureInboxDoc resolves the canonical path to its root metadata. Unknown
// paths (for example a removed root) are dropped instead of leaking stale rows.
func ensureInboxDoc(docs map[string]*inboxDoc, docPath string) *inboxDoc {
	if doc, ok := docs[docPath]; ok {
		return doc
	}
	slash := strings.IndexByte(docPath, '/')
	if slash <= 0 || slash == len(docPath)-1 {
		return nil
	}
	doc := &inboxDoc{
		Path: docPath, RootID: docPath[:slash], RelPath: docPath[slash+1:],
	}
	doc.Name = path.Base(doc.RelPath)
	docs[docPath] = doc
	return doc
}
