package server

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestReviewStatusTracksReviewLoop(t *testing.T) {
	db, err := openStateDB("")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStateDB(db)
	reviews := newReviewStore(db)
	comments := newCommentStore(db)
	const docPath = "r1/a.md"
	const content = "Alpha beta.\n"

	review, err := reviews.effective(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != reviewStatusDraft {
		t.Fatalf("status without comments = %q, want draft", review.Status)
	}

	if _, err := comments.create(docPath, "Alpha", "", "", "fix", content, nil, ""); err != nil {
		t.Fatal(err)
	}
	review, err = reviews.effective(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != reviewStatusInReview {
		t.Fatalf("status with comments = %q, want in_review", review.Status)
	}

	if _, err := reviews.set(docPath, reviewStatusApproved); err != nil {
		t.Fatal(err)
	}
	updated, changed, err := reviews.noteCommentAdded(docPath)
	if err != nil || !changed || updated.Status != reviewStatusChangesRequested {
		t.Fatalf("comment after approval = %+v, changed=%v err=%v", updated, changed, err)
	}

	updated, changed, err = reviews.noteDocChanged(docPath)
	if err != nil || !changed || updated.Status != reviewStatusInReview {
		t.Fatalf("edit after change request = %+v, changed=%v err=%v", updated, changed, err)
	}

	if _, err := reviews.set(docPath, reviewStatusChangesRequested); err != nil {
		t.Fatal(err)
	}
	updated, changed, err = reviews.noteDocChanged(docPath)
	if err != nil || !changed || updated.Status != reviewStatusInReview {
		t.Fatalf("edit after changes_requested = %+v, changed=%v err=%v", updated, changed, err)
	}

	if _, _, err := reviews.noteCommentAdded(docPath); err != nil {
		t.Fatal(err)
	}
	if _, err := reviews.set(docPath, "not-a-status"); err == nil {
		t.Fatal("invalid status should be rejected")
	}
}

func TestInboxAggregatesReviewState(t *testing.T) {
	app, root := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	content := "# Hello\n\n- [x] done\n"

	first, err := app.comments.create(docPath, "Hello", "", "", "please fix", content, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.comments.reply(docPath, first.ID, "agent", "done", "", "Hello2", "", content, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	second, err := app.comments.create(docPath, "done", "", "", "also this", content, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.agentJobs.create(AgentJob{
		DocPath: docPath, CommentID: second.ID, AgentType: "pi", Prompt: "review",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.reviews.set(docPath, reviewStatusChangesRequested); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, app, "GET", "/api/inbox", nil)
	if rec.Code != 200 {
		t.Fatalf("inbox: %d %s", rec.Code, rec.Body.String())
	}
	var resp inboxResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Docs) != 1 {
		t.Fatalf("docs = %+v", resp.Docs)
	}
	doc := resp.Docs[0]
	if doc.Path != docPath || doc.Name != "hello.md" {
		t.Fatalf("metadata = %+v", doc)
	}
	if doc.RootName != filepath.Base(root) {
		t.Fatalf("root name = %q, want %q", doc.RootName, filepath.Base(root))
	}
	if doc.Comments != 2 || doc.Unresolved != 2 {
		t.Fatalf("comment counts = %+v", doc)
	}
	if doc.Awaiting != 1 || doc.AgentWorking != 1 || doc.PendingAgent != 0 {
		t.Fatalf("thread states = %+v", doc)
	}
	if doc.Status != reviewStatusChangesRequested {
		t.Fatalf("status = %q", doc.Status)
	}
	if resp.Counts.Awaiting != 1 || resp.Counts.AgentWorking != 1 || resp.Counts.ChangesRequested != 1 {
		t.Fatalf("counts = %+v", resp.Counts)
	}
}

func TestReviewStatusAPIAndAutoReset(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"

	rec := doJSON(t, app, "POST", "/api/doc/review", map[string]string{
		"path": docPath, "status": reviewStatusApproved,
	})
	if rec.Code != 200 {
		t.Fatalf("set review: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, app, "GET", "/api/doc/review?path="+docPath, nil)
	if rec.Code != 200 {
		t.Fatalf("get review: %d %s", rec.Code, rec.Body.String())
	}
	var review docReview
	if err := json.Unmarshal(rec.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Status != reviewStatusApproved {
		t.Fatalf("status = %q", review.Status)
	}

	rec = doJSON(t, app, "POST", "/api/doc/review", map[string]string{
		"path": docPath, "status": "nope",
	})
	if rec.Code != 400 {
		t.Fatalf("invalid status code = %d", rec.Code)
	}

	rec = doJSON(t, app, "PUT", "/api/doc", map[string]string{
		"path": docPath, "content": "# Hello\n\n- [x] done\n\nmore\n",
	})
	if rec.Code != 200 {
		t.Fatalf("save doc: %d %s", rec.Code, rec.Body.String())
	}
	review, _ = app.reviews.get(docPath)
	if review.Status != reviewStatusInReview {
		t.Fatalf("status after edit = %q, want in_review", review.Status)
	}
}

func TestInboxIncludesStatusOnlyDocuments(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/notes/sub/deep.md"
	if _, err := app.reviews.set(docPath, reviewStatusApproved); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, app, "GET", "/api/inbox", nil)
	if rec.Code != 200 {
		t.Fatalf("inbox: %d %s", rec.Code, rec.Body.String())
	}
	var resp inboxResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, doc := range resp.Docs {
		if doc.Path != docPath {
			continue
		}
		found = true
		if doc.Status != reviewStatusApproved || doc.Comments != 0 || doc.Name != "deep.md" {
			t.Fatalf("doc = %+v", doc)
		}
	}
	if !found {
		t.Fatalf("status-only document missing from inbox: %+v", resp.Docs)
	}
	if resp.Counts.Approved != 1 {
		t.Fatalf("approved count = %d", resp.Counts.Approved)
	}
}
