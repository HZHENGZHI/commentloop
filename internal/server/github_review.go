package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

type githubReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

type githubReviewResult struct {
	URL     string `json:"url"`
	Mode    string `json:"mode"`
	Inline  int    `json:"inline"`
	Warning string `json:"warning,omitempty"`
}

type githubReviewRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleGitHubReview(w http.ResponseWriter, r *http.Request) {
	var req githubReviewRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	data, _, err := s.readRootFile(ro, rel, maxDocSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, errCouldNotReadDocumentLocate)
		return
	}
	review, _ := s.reviews.effective(canonical)
	comments := s.comments.list(canonical, string(data))
	row, err := s.loadGitHubSync()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if row.FilePath == "" {
		s.githubFallbackComment(w, row, rel, review, comments, "未配置 PR 文件路径，已回退为 Issue 评论")
		return
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := s.githubRequest(http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", row.Repo, row.Issue), nil, &pr); err != nil {
		s.githubFallbackComment(w, row, rel, review, comments, "无法读取 PR 信息，已回退为 Issue 评论："+err.Error())
		return
	}
	inline := make([]githubReviewComment, 0, len(comments))
	var fullDoc []string
	for _, comment := range comments {
		if comment.Scope == "document" {
			if body := inlineCommentBody(comment); body != "" {
				fullDoc = append(fullDoc, body)
			}
			continue
		}
		if comment.Line <= 0 || comment.Orphaned || comment.Resolved {
			continue
		}
		body := inlineCommentBody(comment)
		if body == "" {
			continue
		}
		inline = append(inline, githubReviewComment{
			Path: row.FilePath, Line: comment.Line, Side: "RIGHT", Body: body,
		})
	}
	if len(inline) == 0 {
		s.githubFallbackComment(w, row, rel, review, comments, "没有可映射到 PR 行的未解决评论，已回退为 Issue 评论")
		return
	}
	reviewBody := strings.TrimSpace(review.Summary)
	if reviewBody == "" {
		reviewBody = "Commentloop review"
	}
	if len(fullDoc) > 0 {
		reviewBody += "\n\n" + strings.Join(fullDoc, "\n\n")
	}
	payload := map[string]any{
		"commit_id": pr.Head.SHA,
		"body":      reviewBody,
		"event":     "COMMENT",
		"comments":  inline,
	}
	var result struct {
		HTMLURL string `json:"html_url"`
	}
	if err := s.githubRequest(http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/reviews", row.Repo, row.Issue), payload, &result); err != nil {
		s.githubFallbackComment(w, row, rel, review, comments, "创建 inline review 失败，已回退为 Issue 评论："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, githubReviewResult{
		URL: result.HTMLURL, Mode: "review", Inline: len(inline),
	})
}

func inlineCommentBody(comment Comment) string {
	var b strings.Builder
	if comment.Severity != "" {
		fmt.Fprintf(&b, "**%s**\n\n", comment.Severity)
	}
	if len(comment.Messages) > 0 {
		b.WriteString(strings.TrimSpace(comment.Messages[0].Body))
	}
	if comment.Quote != "" {
		b.WriteString("\n\n> ")
		b.WriteString(strings.ReplaceAll(comment.Quote, "\n", "\n> "))
	}
	if comment.Suggestion != nil {
		b.WriteString("\n\n```diff\n")
		b.WriteString("- ")
		b.WriteString(strings.ReplaceAll(comment.Suggestion.Quote, "\n", "\n- "))
		b.WriteString("\n+ ")
		b.WriteString(strings.ReplaceAll(comment.Suggestion.Text, "\n", "\n+ "))
		b.WriteString("\n```")
	}
	body := b.String()
	if len(body) > 6000 {
		body = body[:6000] + "\n…"
	}
	return body
}

func (s *Server) githubFallbackComment(w http.ResponseWriter, row githubSyncRow, rel string, review docReview, comments []Comment, warning string) {
	body := githubIssueBody(rel, review, comments)
	var result struct {
		HTMLURL string `json:"html_url"`
		ID      int64  `json:"id"`
	}
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", row.Repo, row.Issue)
	if err := s.githubRequest(http.MethodPost, path, map[string]string{"body": body}, &result); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, githubReviewResult{
		URL: result.HTMLURL, Mode: "comment", Warning: warning,
	})
}

func githubIssueBody(rel string, review docReview, comments []Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Commentloop review — %s\n\n", filepath.Base(rel))
	fmt.Fprintf(&b, "Path: `%s`\n\nStatus: %s\n", rel, review.Status)
	if review.Summary != "" {
		fmt.Fprintf(&b, "\n**Summary:** %s\n", review.Summary)
	}
	b.WriteString("\n### Comments\n")
	for _, comment := range comments {
		where := fmt.Sprintf("Line %d", comment.Line)
		if comment.Scope == "document" {
			where = "Full document"
		}
		meta := []string{}
		if comment.Severity != "" {
			meta = append(meta, "`"+comment.Severity+"`")
		}
		if comment.Resolved {
			meta = append(meta, "resolved")
		}
		fmt.Fprintf(&b, "\n- **%s**", where)
		if len(meta) > 0 {
			fmt.Fprintf(&b, " — %s", strings.Join(meta, " · "))
		}
		b.WriteString("\n")
		if comment.Quote != "" {
			fmt.Fprintf(&b, "  > %s\n", strings.ReplaceAll(comment.Quote, "\n", "\n  > "))
		}
		for _, message := range comment.Messages {
			fmt.Fprintf(&b, "\n  **%s:** %s\n", message.Author, message.Body)
		}
	}
	body := b.String()
	if len(body) > 60000 {
		body = body[:60000] + "\n…"
	}
	return body
}
