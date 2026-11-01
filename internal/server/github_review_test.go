package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubInlineReviewAndFallback(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	app.githubTokens = &fakeGitHubTokens{token: "secret"}

	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: docPath, Quote: "Hello", Body: "Please tighten this.",
	})
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}

	var reviewPayload map[string]any
	var issueBody string
	reviewStatus := http.StatusCreated
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/1":
			writeJSON(w, http.StatusOK, map[string]any{
				"html_url": "https://github.com/o/r/pull/1", "state": "open",
				"head": map[string]any{"sha": "abc123"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/pulls/1/reviews":
			_ = json.NewDecoder(r.Body).Decode(&reviewPayload)
			if reviewStatus >= 400 {
				writeJSON(w, reviewStatus, map[string]any{"message": "line not in diff"})
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"html_url": "https://github.com/o/r/pull/1#review-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
			var body struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			issueBody = body.Body
			writeJSON(w, http.StatusCreated, map[string]any{"html_url": "https://github.com/o/r/issues/1#issuecomment-2", "id": 2})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()
	app.githubAPIBase = mock.URL

	rec = doJSON(t, app, http.MethodPut, "/api/github/config", putGitHubConfigRequest{
		Repo: "o/r", Issue: 1, FilePath: "docs/hello.md", Token: "secret",
	})
	if rec.Code != 200 {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, app, http.MethodPost, "/api/github/review", githubReviewRequest{Path: docPath})
	if rec.Code != 200 {
		t.Fatalf("review: %d %s", rec.Code, rec.Body.String())
	}
	var result githubReviewResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Mode != "review" || result.Inline != 1 {
		t.Fatalf("result = %+v", result)
	}
	if reviewPayload["commit_id"] != "abc123" {
		t.Fatalf("commit = %v", reviewPayload["commit_id"])
	}
	comments, _ := reviewPayload["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("review comments = %+v", reviewPayload["comments"])
	}
	first, _ := comments[0].(map[string]any)
	if first["path"] != "docs/hello.md" || first["line"] != float64(1) {
		t.Fatalf("inline comment = %+v", first)
	}
	if !strings.Contains(first["body"].(string), "tighten") {
		t.Fatalf("inline body = %v", first["body"])
	}

	reviewStatus = http.StatusUnprocessableEntity
	rec = doJSON(t, app, http.MethodPost, "/api/github/review", githubReviewRequest{Path: docPath})
	if rec.Code != 200 {
		t.Fatalf("fallback: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Mode != "comment" || result.Warning == "" {
		t.Fatalf("fallback result = %+v", result)
	}
	if !strings.Contains(issueBody, "Commentloop review") {
		t.Fatalf("issue fallback body = %q", issueBody)
	}
}
