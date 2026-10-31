package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCommentReaction(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: docPath, Quote: "Hello", Body: "Nice.",
	})
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}
	var comment Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reaction", reactionCommentRequest{
		Path: docPath, ID: comment.ID, Reaction: "👍",
	})
	if rec.Code != 200 {
		t.Fatalf("reaction: %d %s", rec.Code, rec.Body.String())
	}
	var reacted Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &reacted); err != nil {
		t.Fatal(err)
	}
	if reacted.Reaction != "👍" {
		t.Fatalf("reaction = %q", reacted.Reaction)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reaction", reactionCommentRequest{
		Path: docPath, ID: comment.ID, Reaction: "🚀",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported reaction = %d", rec.Code)
	}
}

func TestAgentMentionForcesReviewJob(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	settings := app.settings.get()
	settings.Agent.Runner = "external"
	settings.Agent.Type = "pi"
	settings.Agent.AutoRun = false
	app.settings.set(settings)

	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: docPath, Quote: "Hello", Body: "@agent please tighten this.",
	})
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}
	jobs, err := app.agentJobs.recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].CommentID == "" {
		t.Fatalf("mention did not queue a job: %+v", jobs)
	}

	settings = app.settings.get()
	settings.Agent.AutoRun = false
	app.settings.set(settings)
	rec = doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: docPath, Quote: "done", Body: "No mention here.",
	})
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}
	jobs, _ = app.agentJobs.recent(10)
	if len(jobs) != 1 {
		t.Fatalf("non-mention queued a job: %+v", jobs)
	}
}
