package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSuggestionAcceptAndReject(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath:      docPath,
		Capabilities: agentCapabilities{ReplyComments: true, ReadCode: true},
	})
	headers := map[string]string{"X-Commentloop-Agent-Token": "agent-token"}

	rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/suggestion", proposeChangeRequest{
		Path: docPath, Quote: "Hello", Text: "Hello world", Mode: "replace",
		Body: "Make the greeting specific.", Severity: "should_fix",
	}, headers)
	if rec.Code != 200 {
		t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
	}
	var proposed Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &proposed); err != nil {
		t.Fatal(err)
	}
	if proposed.Suggestion == nil || proposed.Suggestion.Status != "pending" || proposed.Suggestion.Text != "Hello world" {
		t.Fatalf("suggestion = %+v", proposed.Suggestion)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/comments/suggestion", resolveSuggestionRequest{
		Path: docPath, ID: proposed.ID, Accept: true,
	})
	if rec.Code != 200 {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	snapshot, err := app.snapshot(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.content, "Hello world") {
		t.Fatalf("accepted suggestion not applied: %q", snapshot.content)
	}
	accepted, _ := app.comments.get(docPath, proposed.ID)
	if accepted.Suggestion == nil || accepted.Suggestion.Status != "accepted" {
		t.Fatalf("accepted suggestion = %+v", accepted.Suggestion)
	}

	rec = doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/suggestion", proposeChangeRequest{
		Path: docPath, Quote: "Hello world", Text: "Appended paragraph", Mode: "append",
		Body: "Add context.", Severity: "nit",
	}, headers)
	if rec.Code != 200 {
		t.Fatalf("append propose: %d %s", rec.Code, rec.Body.String())
	}
	var appended Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &appended); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/suggestion", resolveSuggestionRequest{
		Path: docPath, ID: appended.ID, Accept: false,
	})
	if rec.Code != 200 {
		t.Fatalf("reject: %d %s", rec.Code, rec.Body.String())
	}
	afterReject, _ := app.snapshot(docPath)
	if strings.Contains(afterReject.content, "Appended paragraph") {
		t.Fatal("rejected suggestion was applied")
	}
	rejected, _ := app.comments.get(docPath, appended.ID)
	if rejected.Suggestion == nil || rejected.Suggestion.Status != "rejected" {
		t.Fatalf("rejected suggestion = %+v", rejected.Suggestion)
	}
}

func TestSuggestionRejectsOrphanedQuote(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath: docPath, Capabilities: agentCapabilities{ReplyComments: true},
	})
	rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/suggestion", proposeChangeRequest{
		Path: docPath, Quote: "Hello", Text: "Hi", Mode: "replace", Body: "Shorter.",
	}, map[string]string{"X-Commentloop-Agent-Token": "agent-token"})
	if rec.Code != 200 {
		t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
	}
	var proposed Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &proposed); err != nil {
		t.Fatal(err)
	}
	// Replace the quoted text behind the suggestion's back.
	if _, err := app.persistDocument(app.rootByID(rootID), "hello.md", docPath, "# Replaced\n", "save", true); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/suggestion", resolveSuggestionRequest{
		Path: docPath, ID: proposed.ID, Accept: true,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("orphaned accept = %d %s", rec.Code, rec.Body.String())
	}
}
