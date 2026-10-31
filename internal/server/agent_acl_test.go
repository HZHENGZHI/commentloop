package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestManagedAgentCannotUseUserOnlyEndpoints(t *testing.T) {
	app, path, _ := newAgentTestApp(t)
	comment := createUserComment(t, app, path)
	app.agentCaps.setToken("ui-token", agentGrant{
		Capabilities: agentCapabilities{WriteDocs: true, ReadCode: true, ReplyComments: true},
		DocPath:      path, JobID: "job-ui",
	})

	cases := []struct {
		method  string
		target  string
		payload any
	}{
		{http.MethodGet, "/api/settings", nil},
		{http.MethodGet, "/api/settings/defaults", nil},
		{http.MethodGet, "/api/agent/jobs", nil},
		{http.MethodPut, "/api/settings", map[string]any{}},
		{http.MethodPost, "/api/prompt-templates/optimize", map[string]any{}},
		{http.MethodPost, "/api/prompt-templates", promptTemplateRequest{Name: "x", Body: "y"}},
		{http.MethodPost, "/api/comments/resolve", resolveCommentRequest{Path: path, ID: comment.ID, Resolved: true}},
		{http.MethodPost, "/api/comments/group", groupCommentRequest{Path: path, ID: comment.ID}},
		{http.MethodPost, "/api/comments/rollback", rollbackCommentRequest{Path: path, ID: comment.ID, MessageID: "missing"}},
		{http.MethodPost, "/api/comments/trigger", triggerCommentRequest{Path: path, ID: comment.ID}},
		{http.MethodDelete, "/api/comments?path=" + url.QueryEscape(path) + "&id=" + url.QueryEscape(comment.ID), nil},
		{http.MethodPost, "/api/code/branch", switchBranchRequest{ID: "missing", Branch: "main"}},
		{http.MethodDelete, "/api/code/repos?id=missing", nil},
		{http.MethodPost, "/api/doc/discussions", importDiscussionRequest{Path: path, SessionPath: "/tmp/x.jsonl"}},
		{http.MethodPost, "/api/doc/consensus/refresh", refreshConsensusRequest{Path: path}},
	}
	for _, tc := range cases {
		rec := doJSONHeaders(t, app, tc.method, tc.target, tc.payload, tokenHeaders("ui-token"))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.target, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "ui_action_agent_forbidden") {
			t.Fatalf("%s %s: unexpected body=%s", tc.method, tc.target, rec.Body)
		}
	}
}

func TestBranchListChecksRepositoryACL(t *testing.T) {
	app, _, _ := newAgentTestApp(t)
	repoPath := t.TempDir()
	repo, err := app.codeRepos.ensure(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	app.agentCaps.setToken("repo-token", agentGrant{
		Capabilities: agentCapabilities{ReadCode: true}, DocPath: "", RepoIDs: nil, JobID: "job-repo",
	})
	// The branch endpoint names the repository with ?id=. The guard must read
	// the same parameter as the handler, otherwise every repo is readable.
	rec := doJSONHeaders(t, app, http.MethodGet, "/api/code/branches?id="+repo.ID, nil, tokenHeaders("repo-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("branch list status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestAgentRunnerRejectsCrossSiteRequests(t *testing.T) {
	app, _, _ := newAgentTestApp(t)
	rec := doJSONHeaders(t, app, http.MethodPost, "/api/agent/runner/claim", agentRunnerClaimRequest{
		RunnerID: "runner", AgentType: "generic",
	}, map[string]string{"Origin": "http://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site claim status=%d body=%s", rec.Code, rec.Body)
	}
	// External runners send no Origin header and must keep working.
	rec = doJSON(t, app, http.MethodPost, "/api/agent/runner/claim", agentRunnerClaimRequest{
		RunnerID: "runner", AgentType: "generic",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("runner claim status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestManagedAgentCannotReadAnotherDocumentsContext(t *testing.T) {
	app, path, otherPath := newAgentTestApp(t)
	app.agentCaps.setToken("read-token", agentGrant{
		Capabilities: agentCapabilities{ReadCode: true}, DocPath: path, JobID: "job-read",
	})
	cases := []string{
		"/api/doc/discussions?path=" + url.QueryEscape(otherPath),
		"/api/doc/agent-sessions?path=" + url.QueryEscape(otherPath),
		"/api/doc/code-repos?path=" + url.QueryEscape(otherPath),
	}
	for _, target := range cases {
		rec := doJSONHeaders(t, app, http.MethodGet, target, nil, tokenHeaders("read-token"))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status=%d body=%s", target, rec.Code, rec.Body)
		}
	}
}
