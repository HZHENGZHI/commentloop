package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	if _, err := comments.createAs("user", docPath, "Alpha", "", "", "fix", "", content, nil, ""); err != nil {
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

	first, err := app.comments.createAs("user", docPath, "Hello", "", "", "please fix", "", content, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.comments.reply(docPath, first.ID, "agent", "done", "", "Hello2", "", content, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	second, err := app.comments.createAs("user", docPath, "done", "", "", "also this", "", content, nil, "")
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

func TestInboxKeepsAwaitingWhileAgentRetries(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	content := "# Hello\n\n- [x] done\n"

	comment, err := app.comments.createAs("user", docPath, "Hello", "", "", "please fix", "", content, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.comments.reply(docPath, comment.ID, "agent", "done", "", "Hello2", "", content, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.agentJobs.create(AgentJob{
		DocPath: docPath, CommentID: comment.ID, AgentType: "pi", Prompt: "retry",
	}); err != nil {
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
	if doc.Awaiting != 1 || doc.AgentWorking != 1 {
		t.Fatalf("thread states = %+v", doc)
	}
}

func TestUnchangedSaveKeepsApproval(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	const content = "# Hello\n\n- [x] done\n"

	if rec := doJSON(t, app, "GET", "/api/doc?path="+docPath, nil); rec.Code != 200 {
		t.Fatalf("read doc: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := app.reviews.set(docPath, reviewStatusApproved); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, app, "PUT", "/api/doc", map[string]string{"path": docPath, "content": content})
	if rec.Code != 200 {
		t.Fatalf("save doc: %d %s", rec.Code, rec.Body.String())
	}
	if review, ok := app.reviews.get(docPath); !ok || review.Status != reviewStatusApproved {
		t.Fatalf("status after unchanged save = %+v, %v", review, ok)
	}
}

func TestInboxRejectsManagedAgents(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	granted := rootID + "/hello.md"
	app.agentCaps.setToken("agent-token", agentGrant{DocPath: granted})

	rec := doJSONHeaders(t, app, "GET", "/api/inbox", nil, map[string]string{
		"X-Commentloop-Agent-Token": "agent-token",
	})
	if rec.Code != 403 {
		t.Fatalf("inbox with agent token = %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSONHeaders(t, app, "GET", "/api/inbox", nil, map[string]string{
		"X-Commentloop-Agent": "1",
	})
	if rec.Code != 403 {
		t.Fatalf("inbox with agent header = %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSONHeaders(t, app, "GET", "/api/doc/review?path="+rootID+"/notes/sub/deep.md", nil, map[string]string{
		"X-Commentloop-Agent-Token": "agent-token",
	})
	if rec.Code != 403 {
		t.Fatalf("review of another document = %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSONHeaders(t, app, "GET", "/api/doc/review?path="+granted, nil, map[string]string{
		"X-Commentloop-Agent-Token": "agent-token",
	})
	if rec.Code != 200 {
		t.Fatalf("review of granted document = %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSONHeaders(t, app, "POST", "/api/doc/review", map[string]string{
		"path": granted, "status": reviewStatusApproved,
	}, map[string]string{"X-Commentloop-Agent": "1"})
	if rec.Code != 403 {
		t.Fatalf("status change by manual agent = %d %s", rec.Code, rec.Body.String())
	}
}

func TestRemoteTreePathsOnlyTrustsCompleteCache(t *testing.T) {
	app, _ := newTestApp(t)
	ro := &root{ID: "remote", Kind: rootKindRemote, Name: "remote"}
	app.storeRemoteTree(ro, []remoteFile{{Rel: "a.md"}, {Rel: "b.md"}})
	paths := remoteTreePaths(app, ro)
	if paths == nil || !paths["a.md"] || !paths["b.md"] {
		t.Fatalf("paths = %+v", paths)
	}
	app.remoteTreeTruncated[ro.ID] = true
	if paths := remoteTreePaths(app, ro); paths != nil {
		t.Fatalf("truncated cache = %+v, want nil", paths)
	}
}

func TestInboxDropsDeletedRemoteDocuments(t *testing.T) {
	remoteRoot := t.TempDir()
	mustWrite(t, filepath.Join(remoteRoot, "kept.md"), "# Kept\n")
	srv := startTestSSHServer(t, remoteRoot)

	primary := t.TempDir()
	mustWrite(t, filepath.Join(primary, "local.md"), "# Local\n")
	app, err := New(Options{
		Root: primary, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	app.remotePollInterval = 100 * time.Millisecond

	rec := doJSON(t, app, http.MethodPost, "/api/roots/remote", addRemoteRootRequest{
		Name: "remote-notes", Host: srv.host, Port: srv.port,
		User: "tester", KeyPath: srv.keyPath, Path: remoteRoot,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add remote root: %d %s", rec.Code, rec.Body.String())
	}
	var remoteID string
	for _, ro := range app.rootsSnapshot() {
		if ro.Kind == rootKindRemote {
			remoteID = ro.ID
		}
	}
	if remoteID == "" {
		t.Fatal("remote root not registered")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, loaded := app.cachedRemoteTree(remoteID); loaded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote tree did not load")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := app.reviews.set(remoteID+"/kept.md", reviewStatusApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := app.reviews.set(remoteID+"/gone.md", reviewStatusApproved); err != nil {
		t.Fatal(err)
	}
	resp, err := app.buildInbox()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, doc := range resp.Docs {
		seen[doc.Path] = true
	}
	if !seen[remoteID+"/kept.md"] {
		t.Fatal("existing remote document missing from inbox")
	}
	if seen[remoteID+"/gone.md"] {
		t.Fatal("deleted remote document still in inbox")
	}
}

func TestRemotePollDoesNotRevokeApprovalAfterSave(t *testing.T) {
	remoteRoot := t.TempDir()
	mustWrite(t, filepath.Join(remoteRoot, "doc.md"), "# Original\n")
	srv := startTestSSHServer(t, remoteRoot)

	primary := t.TempDir()
	mustWrite(t, filepath.Join(primary, "local.md"), "# Local\n")
	app, err := New(Options{
		Root: primary, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	app.remotePollInterval = 100 * time.Millisecond

	rec := doJSON(t, app, http.MethodPost, "/api/roots/remote", addRemoteRootRequest{
		Name: "remote-notes", Host: srv.host, Port: srv.port,
		User: "tester", KeyPath: srv.keyPath, Path: remoteRoot,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("add remote root: %d %s", rec.Code, rec.Body.String())
	}
	var remoteID string
	for _, ro := range app.rootsSnapshot() {
		if ro.Kind == rootKindRemote {
			remoteID = ro.ID
		}
	}
	if remoteID == "" {
		t.Fatal("remote root not registered")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, loaded := app.cachedRemoteTree(remoteID); loaded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote tree did not load")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ro := app.rootByID(remoteID)
	if ro == nil {
		t.Fatal("remote root missing")
	}
	canonical := remoteID + "/doc.md"
	if _, err := app.persistDocument(ro, "doc.md", canonical, "# Changed\n", "save", true); err != nil {
		t.Fatal(err)
	}
	if _, err := app.reviews.set(canonical, reviewStatusApproved); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	review, ok := app.reviews.get(canonical)
	if !ok || review.Status != reviewStatusApproved {
		t.Fatalf("status after remote save poll = %+v, %v", review, ok)
	}
}

func TestAgentCreatesSevCommentAndReviewSummary(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath:      docPath,
		Capabilities: agentCapabilities{ReadCode: true, ReplyComments: true},
	})
	headers := map[string]string{"X-Commentloop-Agent-Token": "agent-token"}

	spoof := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/comment", createCommentRequest{
		Path: docPath, Quote: "Hello", Body: "spoof", Severity: "nit",
	}, map[string]string{"X-Commentloop-Agent": "1"})
	if spoof.Code != http.StatusForbidden {
		t.Fatalf("header-only agent comment = %d %s", spoof.Code, spoof.Body.String())
	}

	rec := doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/comment", createCommentRequest{
		Path: docPath, Quote: "Hello", Body: "This claim needs a citation.", Severity: "blocking",
		PromptTemplateID: defaultPromptTemplateID,
	}, headers)
	if rec.Code != 200 {
		t.Fatalf("agent comment: %d %s", rec.Code, rec.Body.String())
	}
	var comment Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	if comment.Severity != "blocking" || len(comment.Messages) != 1 || comment.Messages[0].Author != "agent" {
		t.Fatalf("comment = %+v", comment)
	}
	if comment.PromptTemplateID != reviewPromptTemplateID {
		t.Fatalf("review comment template = %q, want %q", comment.PromptTemplateID, reviewPromptTemplateID)
	}

	rec = doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/summary", setReviewSummaryRequest{
		Path: docPath, Summary: "One blocking issue.",
	}, headers)
	if rec.Code != 200 {
		t.Fatalf("summary: %d %s", rec.Code, rec.Body.String())
	}
	review, err := app.reviews.effective(docPath)
	if err != nil || review.Summary != "One blocking issue." {
		t.Fatalf("review = %+v, err=%v", review, err)
	}

	rec = doJSONHeaders(t, app, http.MethodPost, "/api/doc/review/request", requestDocReview{Path: docPath}, headers)
	if rec.Code != 403 {
		t.Fatalf("agent review request = %d %s", rec.Code, rec.Body.String())
	}
}

func TestBuildDocReviewPrompt(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	prompt, caps, _, err := app.buildDocReviewPrompt(rootID+"/hello.md", "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"full-document review", "create_comment", "write_review_summary", "blocking"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if caps.WriteDocs || !caps.ReplyComments || !caps.ReadCode {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestRequestDocReviewQueuesExternalJob(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	settings := app.settings.get()
	settings.Agent.Runner = "external"
	settings.Agent.Type = "pi"
	app.settings.set(settings)

	rec := doJSON(t, app, http.MethodPost, "/api/doc/review/request", requestDocReview{Path: docPath})
	if rec.Code != 200 {
		t.Fatalf("review request: %d %s", rec.Code, rec.Body.String())
	}
	jobs, err := app.agentJobs.recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].DocPath != docPath || jobs[0].CommentID != "" {
		t.Fatalf("jobs = %+v", jobs)
	}
	if jobs[0].PromptTemplateID != reviewPromptTemplateID || !strings.Contains(jobs[0].Prompt, "create_comment") {
		t.Fatalf("job = %+v", jobs[0])
	}
	review, err := app.reviews.effective(docPath)
	if err != nil || review.Status != reviewStatusInReview {
		t.Fatalf("review = %+v err=%v", review, err)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc/review/request", requestDocReview{Path: rootID + "/missing.md"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing document review = %d %s", rec.Code, rec.Body.String())
	}
}
