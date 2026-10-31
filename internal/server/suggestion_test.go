package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

func TestConcurrentSuggestionAccepts(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath: docPath, Capabilities: agentCapabilities{ReplyComments: true},
	})
	headers := map[string]string{"X-Commentloop-Agent-Token": "agent-token"}
	ids := make([]string, 0, 2)
	for _, tc := range []struct{ quote, text string }{
		{"Hello", "Hi"},
		{"done", "finished"},
	} {
		rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/suggestion", proposeChangeRequest{
			Path: docPath, Quote: tc.quote, Text: tc.text, Mode: "replace", Body: "Edit.",
		}, headers)
		if rec.Code != 200 {
			t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
		}
		var comment Comment
		if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, comment.ID)
	}

	codes := make([]int, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			data, _ := json.Marshal(resolveSuggestionRequest{Path: docPath, ID: id, Accept: true})
			req := httptest.NewRequest(http.MethodPost, "http://example.com/api/comments/suggestion", bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i, id)
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("accept %d = %d", i, code)
		}
	}
	snapshot, err := app.snapshot(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.content, "Hi") || !strings.Contains(snapshot.content, "finished") {
		t.Fatalf("concurrent accepts lost an edit: %q", snapshot.content)
	}
}

func TestAppendSuggestionUsesNearbyLine(t *testing.T) {
	app, root := newTestApp(t)
	mustWrite(t, filepath.Join(root, "append.md"), "# Heading\nbody\n")
	docPath := canonical(app, "append.md")
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath: docPath, Capabilities: agentCapabilities{ReplyComments: true},
	})
	headers := map[string]string{"X-Commentloop-Agent-Token": "agent-token"}
	rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/suggestion", proposeChangeRequest{
		Path: docPath, Quote: "Heading", Text: "extra", Mode: "append", Body: "Add.",
	}, headers)
	if rec.Code != 200 {
		t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
	}
	var comment Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/suggestion", resolveSuggestionRequest{
		Path: docPath, ID: comment.ID, Accept: true,
	})
	if rec.Code != 200 {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	snapshot, err := app.snapshot(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(snapshot.content, "extra") < 0 || strings.Index(snapshot.content, "extra") > strings.Index(snapshot.content, "body") {
		t.Fatalf("append landed at document end: %q", snapshot.content)
	}
	if strings.Contains(snapshot.content, "extrabody") {
		t.Fatalf("append glued onto the next line: %q", snapshot.content)
	}
}

func TestAppendSuggestionKeepsCRLFNextLine(t *testing.T) {
	app, root := newTestApp(t)
	mustWrite(t, filepath.Join(root, "append-crlf.md"), "# Heading\r\nbody\r\n")
	docPath := canonical(app, "append-crlf.md")
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath: docPath, Capabilities: agentCapabilities{ReplyComments: true},
	})
	headers := map[string]string{"X-Commentloop-Agent-Token": "agent-token"}
	rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/suggestion", proposeChangeRequest{
		Path: docPath, Quote: "Heading", Text: "extra", Mode: "append", Body: "Add.",
	}, headers)
	if rec.Code != 200 {
		t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
	}
	var comment Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/suggestion", resolveSuggestionRequest{
		Path: docPath, ID: comment.ID, Accept: true,
	})
	if rec.Code != 200 {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	snapshot, err := app.snapshot(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.content, "extrabody") || !strings.Contains(snapshot.content, "extra\r\n") {
		t.Fatalf("CRLF append corrupted the next line: %q", snapshot.content)
	}
}
