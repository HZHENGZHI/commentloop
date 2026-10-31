package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Review status describes where a document sits in the human/agent review
// loop. It is metadata about the document, not a property of its comments.
const (
	reviewStatusDraft            = "draft"
	reviewStatusInReview         = "in_review"
	reviewStatusChangesRequested = "changes_requested"
	reviewStatusApproved         = "approved"
)

var reviewStatuses = map[string]bool{
	reviewStatusDraft:            true,
	reviewStatusInReview:         true,
	reviewStatusChangesRequested: true,
	reviewStatusApproved:         true,
}

type docReview struct {
	Path             string    `json:"path"`
	Status           string    `json:"status"`
	Summary          string    `json:"summary,omitempty"`
	SummaryUpdatedAt time.Time `json:"summaryUpdatedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type reviewStore struct {
	db *gorm.DB
}

func newReviewStore(db *gorm.DB) *reviewStore {
	return &reviewStore{db: db}
}

func (s *reviewStore) get(path string) (docReview, bool) {
	var row docReviewRow
	if err := s.db.Take(&row, "doc_path = ?", path).Error; err != nil {
		return docReview{}, false
	}
	return docReview{
		Path: row.DocPath, Status: row.Status, Summary: row.Summary,
		SummaryUpdatedAt: parseTime(row.SummaryUpdatedAt),
		UpdatedAt:        parseTime(row.UpdatedAt),
	}, true
}

// effective returns the explicit status when the user has set one. Documents
// with review activity but no explicit status are considered in review;
// everything else is still a draft.
func (s *reviewStore) effective(path string) (docReview, error) {
	review, ok := s.get(path)
	if ok && review.Status != "" {
		return review, nil
	}
	status, err := s.inferredStatus(path)
	if err != nil {
		return docReview{}, err
	}
	if ok {
		review.Status = status
		return review, nil
	}
	return docReview{Path: path, Status: status}, nil
}

// inferredStatus derives the status used by documents whose review row only
// carries a summary (no explicit status).
func (s *reviewStore) inferredStatus(path string) (string, error) {
	var comments int64
	if err := s.db.Model(&commentRow{}).Where("path = ?", path).Count(&comments).Error; err != nil {
		return "", err
	}
	if comments > 0 {
		return reviewStatusInReview, nil
	}
	return reviewStatusDraft, nil
}

func (s *reviewStore) set(path, status string) (docReview, error) {
	if !reviewStatuses[status] {
		return docReview{}, errors.New("无效的审阅状态")
	}
	now := time.Now()
	row := docReviewRow{DocPath: path, Status: status, UpdatedAt: formatTime(now)}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "doc_path"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return docReview{}, err
	}
	if saved, ok := s.get(path); ok {
		return saved, nil
	}
	return docReview{Path: path, Status: status, UpdatedAt: now}, nil
}

// clear removes the explicit review status of one document. It is used when
// the document itself is deleted, so a later document created at the same
// path does not inherit a stale approval.
func (s *reviewStore) clear(path string) error {
	return s.db.Where("doc_path = ?", path).Delete(&docReviewRow{}).Error
}

func (s *reviewStore) setSummary(path, summary string) (docReview, error) {
	summary = strings.TrimSpace(summary)
	if len(summary) > 12000 {
		return docReview{}, errors.New("审阅摘要过长")
	}
	current, err := s.effective(path)
	if err != nil {
		return docReview{}, err
	}
	now := time.Now()
	// Do not materialize an inferred status: the row only needs the summary so
	// a later comment can still move the document from draft to in-review.
	row := docReviewRow{
		DocPath: path, Status: "", Summary: summary,
		SummaryUpdatedAt: formatTime(now), UpdatedAt: formatTime(now),
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "doc_path"}},
		DoUpdates: clause.AssignmentColumns([]string{"summary", "summary_updated_at", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return docReview{}, err
	}
	return docReview{
		Path: path, Status: current.Status, Summary: summary,
		SummaryUpdatedAt: now, UpdatedAt: now,
	}, nil
}

// noteCommentAdded records that a human added review feedback. An approval
// stops being current as soon as the reviewer asks for more work.
func (s *reviewStore) noteCommentAdded(path string) (docReview, bool, error) {
	review, ok := s.get(path)
	if !ok || review.Status != reviewStatusApproved {
		return docReview{}, false, nil
	}
	updated, err := s.set(path, reviewStatusChangesRequested)
	return updated, err == nil, err
}

// noteDocChanged records that the document changed. A previous approval or a
// change request is no longer current until the reviewer looks again.
func (s *reviewStore) noteDocChanged(path string) (docReview, bool, error) {
	review, ok := s.get(path)
	if !ok || (review.Status != reviewStatusApproved && review.Status != reviewStatusChangesRequested) {
		return docReview{}, false, nil
	}
	updated, err := s.set(path, reviewStatusInReview)
	return updated, err == nil, err
}

// noteReviewCommentAdded broadcasts a status change caused by new human
// feedback, so every open inbox reflects the new state without a reload.
func (s *Server) noteReviewCommentAdded(path string) {
	review, changed, err := s.reviews.noteCommentAdded(path)
	if err == nil && changed {
		s.hub.broadcast(outMessage{Type: "reviews", Path: path, Status: review.Status})
	}
}

// noteReviewDocChanged invalidates an approval once the document is edited.
func (s *Server) noteReviewDocChanged(path string) {
	review, changed, err := s.reviews.noteDocChanged(path)
	if err == nil && changed {
		s.hub.broadcast(outMessage{Type: "reviews", Path: path, Status: review.Status})
	}
}

func (s *reviewStore) all() (map[string]docReview, error) {
	var rows []docReviewRow
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]docReview, len(rows))
	for _, row := range rows {
		out[row.DocPath] = docReview{
			Path: row.DocPath, Status: row.Status, Summary: row.Summary,
			SummaryUpdatedAt: parseTime(row.SummaryUpdatedAt),
			UpdatedAt:        parseTime(row.UpdatedAt),
		}
	}
	return out, nil
}

type setReviewRequest struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type setReviewSummaryRequest struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

func (s *Server) handleGetDocReview(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能读取关联文档") {
		return
	}
	review, err := s.reviews.effective(canonical)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternalServer)
		return
	}
	writeJSON(w, http.StatusOK, review)
}

func (s *Server) handleSetDocReviewSummary(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCommentReply(w, r) {
		return
	}
	var req setReviewSummaryRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if _, known := s.agentGrantForRequest(r); known || r.Header.Get("X-Commentloop-Agent") == "1" {
		if s.rejectAgentDocPath(w, r, canonical, "当前任务只能更新关联文档的审阅摘要") {
			return
		}
	}
	review, err := s.reviews.setSummary(canonical, req.Summary)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.hub.broadcast(outMessage{Type: "reviews", Path: canonical, Status: review.Status})
	writeJSON(w, http.StatusOK, review)
}

func (s *Server) handleSetDocReview(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentWrite(w, r) {
		return
	}
	if _, known := s.agentGrantForRequest(r); known || r.Header.Get("X-Commentloop-Agent") == "1" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "审阅状态只能由用户设置"})
		return
	}
	var req setReviewRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	if !reviewStatuses[req.Status] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的审阅状态"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	review, err := s.reviews.set(canonical, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errInternalServer)
		return
	}
	s.hub.broadcast(outMessage{Type: "reviews", Path: canonical, Status: review.Status})
	writeJSON(w, http.StatusOK, review)
}
