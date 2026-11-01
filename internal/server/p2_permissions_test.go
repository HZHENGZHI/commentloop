package server

import (
	"net/http"
	"testing"
)

func TestP2Permissions(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	otherPath := rootID + "/other.md"
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath:      docPath,
		Capabilities: agentCapabilities{ReadCode: true},
	})
	agentHeaders := map[string]string{"X-Commentloop-Agent-Token": "agent-token"}

	for _, tc := range []struct {
		name   string
		method string
		target string
		body   any
	}{
		{"search", http.MethodGet, "/api/search?q=Hello", nil},
		{"checklist apply", http.MethodPost, "/api/doc/checklist/apply", applyChecklistRequest{Path: docPath, Playbook: "prd"}},
		{"checklist read other doc", http.MethodGet, "/api/doc/checklist?path=" + otherPath, nil},
		{"github config", http.MethodGet, "/api/github/config", nil},
		{"github review", http.MethodPost, "/api/github/review", githubReviewRequest{Path: docPath}},
	} {
		rec := doJSONHeaders(t, app, tc.method, tc.target, tc.body, agentHeaders)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s = %d %s, want 403", tc.name, rec.Code, rec.Body.String())
		}
	}

	// Same-origin guard still applies to user-only P2 writes.
	rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/checklist/apply",
		applyChecklistRequest{Path: docPath, Playbook: "prd"},
		map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin checklist = %d %s", rec.Code, rec.Body.String())
	}
}
