package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeGitHubTokens struct {
	token string
}

func (f *fakeGitHubTokens) Get() (string, error) { return f.token, nil }
func (f *fakeGitHubTokens) Set(token string) error {
	f.token = token
	return nil
}
func (f *fakeGitHubTokens) Clear() error {
	f.token = ""
	return nil
}

func TestGitHubSyncConfigAndComment(t *testing.T) {
	app, _ := newTestApp(t)
	tokens := &fakeGitHubTokens{}
	app.githubTokens = tokens
	var postedAuth, postedBody string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/1":
			writeJSON(w, http.StatusOK, map[string]any{
				"title": "Test issue", "html_url": "https://github.com/o/r/issues/1", "state": "open",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments":
			postedAuth = r.Header.Get("Authorization")
			data, _ := io.ReadAll(r.Body)
			postedBody = string(data)
			writeJSON(w, http.StatusCreated, map[string]any{
				"html_url": "https://github.com/o/r/issues/1#issuecomment-1", "id": 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()
	app.githubAPIBase = mock.URL

	rec := doJSON(t, app, http.MethodPut, "/api/github/config", putGitHubConfigRequest{
		Repo: "o/r", Issue: 1, Token: "secret-token",
	})
	if rec.Code != 200 {
		t.Fatalf("put config: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatal("config response leaked the token")
	}
	var config githubSyncConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if !config.Configured || !config.TokenSet || config.Repo != "o/r" {
		t.Fatalf("config = %+v", config)
	}
	rec = doJSON(t, app, http.MethodPut, "/api/github/config", putGitHubConfigRequest{
		Repo: "o/r", Issue: 1, ClearToken: true,
	})
	if rec.Code != 200 {
		t.Fatalf("clear token: %d %s", rec.Code, rec.Body.String())
	}
	var cleared githubSyncConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.TokenSet {
		t.Fatal("token still set after clear")
	}
	if tokens.token != "" {
		t.Fatal("token store was not cleared")
	}
	// Restore the token for the remaining requests.
	if err := tokens.Set("secret-token"); err != nil {
		t.Fatal(err)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/github/test", map[string]any{})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Test issue") {
		t.Fatalf("test: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, app, http.MethodPost, "/api/github/comment", githubCommentRequest{Body: "## Review\n\nLooks good."})
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}
	if postedAuth != "Bearer secret-token" {
		t.Fatalf("auth = %q", postedAuth)
	}
	if !strings.Contains(postedBody, "Looks good.") {
		t.Fatalf("posted body = %q", postedBody)
	}
}

func TestGitHubSyncRejectsBadRepo(t *testing.T) {
	app, _ := newTestApp(t)
	app.githubTokens = &fakeGitHubTokens{token: "x"}
	rec := doJSON(t, app, http.MethodPut, "/api/github/config", putGitHubConfigRequest{
		Repo: "not a repo", Issue: 1,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad repo = %d", rec.Code)
	}
}
