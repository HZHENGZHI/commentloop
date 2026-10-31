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

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	// The inbox is a workspace-wide UI surface. Managed Agent tasks only have
	// access to their granted document, so they must not enumerate the rest.
	if _, known := s.agentGrantForRequest(r); known {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "当前任务不能读取收件箱"})
		return
	}
	resp, err := s.buildInbox()
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) buildInbox() (inboxResponse, error) {
	docs := make(map[string]*inboxDoc)
	reviews, err := s.reviews.all()
	if err != nil {
		return inboxResponse{}, err
	}

	var commentRows []commentRow
	if err := s.db.Select("id", "path", "resolved", "orphaned", "created_at", "updated_at").Find(&commentRows).Error; err != nil {
		return inboxResponse{}, err
	}

	lastAuthor := make(map[string]string, len(commentRows))
	lastMessageAt := make(map[string]time.Time, len(commentRows))
	var messageRows []commentMessageRow
	if err := s.db.Select("comment_id", "author", "created_at").Order("created_at, id").Find(&messageRows).Error; err != nil {
		return inboxResponse{}, err
	}
	for _, row := range messageRows {
		lastAuthor[row.CommentID] = row.Author
		lastMessageAt[row.CommentID] = parseTime(row.CreatedAt)
	}

	workingComments := make(map[string]bool)
	var jobRows []agentJobRow
	if err := s.db.Select("comment_id").Where("status IN ?", []string{"queued", "running"}).Find(&jobRows).Error; err != nil {
		return inboxResponse{}, err
	}
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
		if workingComments[row.ID] {
			doc.AgentWorking++
		}
		// Keep the latest-message classification aligned with the comment
		// panel: a running Agent job is additional state, not a fifth status.
		switch lastAuthor[row.ID] {
		case "agent", "system":
			doc.Awaiting++
		case "user":
			if !workingComments[row.ID] {
				doc.PendingAgent++
			}
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

	remoteTrees := make(map[string]map[string]bool)
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
		switch ro.Kind {
		case rootKindLocal:
			if !pathExists(filepath.Join(ro.Path, filepath.FromSlash(doc.RelPath))) {
				continue
			}
		case rootKindRemote:
			known, ok := remoteTrees[ro.ID]
			if !ok {
				known = remoteTreePaths(s, ro)
				remoteTrees[ro.ID] = known
			}
			if known != nil && !known[doc.RelPath] {
				continue
			}
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
	return inboxResponse{Docs: list, Counts: counts}, nil
}

// remoteTreePaths returns the cached set of existing remote files. A nil set
// means the cache is not complete yet (or truncated); callers keep entries in
// that case instead of hiding documents that may still exist.
func remoteTreePaths(s *Server, ro *root) map[string]bool {
	files, truncated, loaded := s.cachedRemoteTree(ro.ID)
	if !loaded || truncated {
		return nil
	}
	out := make(map[string]bool, len(files))
	for _, file := range files {
		out[file.RelPath] = true
	}
	return out
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
