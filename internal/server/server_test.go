package server

import (
	"github.com/hzhengzhi/commentloop/internal/config"

	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hzhengzhi/commentloop/internal/coderepo"
	"github.com/hzhengzhi/commentloop/internal/lsp"
)

func mapFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>commentloop test</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('commentloop')")},
	}
}

func newTestApp(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "hello.md"), "# Hello\n\n- [x] done\n")
	mustMkdir(t, filepath.Join(root, "notes", "sub"))
	mustWrite(t, filepath.Join(root, "notes", "sub", "deep.md"), "# Deep\n")

	app, err := New(Options{
		Root:        root,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func doJSON(
	t *testing.T,
	app *Server,
	method, target string,
	payload any,
) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, "http://example.com"+target, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func doJSONHeaders(
	t *testing.T,
	app *Server,
	method, target string,
	payload any,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, "http://example.com"+target, body)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func primaryRootID(app *Server) string {
	for _, ro := range app.rootsSnapshot() {
		if ro.Primary {
			return ro.ID
		}
	}
	roots := app.rootsSnapshot()
	if len(roots) == 0 {
		return ""
	}
	return roots[0].ID
}

func canonical(app *Server, rel string) string {
	return primaryRootID(app) + "/" + rel
}

func canonicalIn(app *Server, rootPath, rel string) string {
	for _, ro := range app.rootsSnapshot() {
		if ro.Path == rootPath {
			return ro.ID + "/" + rel
		}
	}
	return ""
}

func docURL(path string) string {
	return "/api/doc?path=" + url.QueryEscape(path)
}

func rawURL(path string) string {
	return "/raw/" + path
}

func TestDocLifecycle(t *testing.T) {
	app, root := newTestApp(t)
	hello := canonical(app, "hello.md")
	newDoc := canonical(app, "new.md")
	created := canonical(app, "notes/created.md")

	treeRec := doJSON(t, app, http.MethodGet, "/api/tree", nil)
	if treeRec.Code != http.StatusOK {
		t.Fatalf("tree status = %d: %s", treeRec.Code, treeRec.Body)
	}
	tree := decode[treeResponse](t, treeRec)
	if len(tree.Files) != 2 {
		t.Fatalf("tree files = %d, want 2 (%+v)", len(tree.Files), tree.Files)
	}

	rec := doJSON(t, app, http.MethodGet, docURL(hello), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", rec.Code, rec.Body)
	}
	doc := decode[docResponse](t, rec)
	if !strings.Contains(doc.HTML, "<h1") || !strings.Contains(doc.Content, "Hello") {
		t.Fatalf("unexpected doc: %+v", doc)
	}

	updated := "# Updated\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"
	rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: hello, Content: updated})
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", rec.Code, rec.Body)
	}
	saved := decode[docResponse](t, rec)
	if !strings.Contains(saved.HTML, "<table>") {
		t.Fatalf("saved HTML missing table: %s", saved.HTML)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "hello.md"))
	if err != nil || string(onDisk) != updated {
		t.Fatalf("disk content mismatch: %q err=%v", onDisk, err)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc", saveRequest{Path: newDoc, Content: "# New"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "new.md")); err != nil {
		t.Fatalf("new.md not created: %v", err)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc", saveRequest{Path: newDoc, Content: "x"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", rec.Code)
	}

	rec = doJSON(t, app, http.MethodDelete, docURL(newDoc), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d: %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodGet, docURL(newDoc), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted status = %d, want 404", rec.Code)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc", saveRequest{Path: created, Content: "nested"})
	if rec.Code != http.StatusOK {
		t.Fatalf("nested create status = %d: %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "created.md")); err != nil {
		t.Fatalf("nested file not created: %v", err)
	}
}

func TestPathSafety(t *testing.T) {
	app, root := newTestApp(t)
	id := primaryRootID(app)

	for _, target := range []string{
		docURL("../secret.md"),
		docURL("/etc/passwd"),
		docURL("hello.txt"),
		docURL(""),
		docURL(id + "/../secret.md"),
		docURL("unknown/" + "hello.md"),
	} {
		rec := doJSON(t, app, http.MethodGet, target, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", target, rec.Code)
		}
	}

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	mustWrite(t, secret, "top secret")
	if err := os.Symlink(secret, filepath.Join(root, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	link := canonical(app, "link.md")
	rec := doJSON(t, app, http.MethodGet, docURL(link), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("symlink read status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: link, Content: "pwned"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("symlink write status = %d, want 400", rec.Code)
	}
	raw := doJSON(t, app, http.MethodGet, rawURL(link), nil)
	if raw.Code != http.StatusNotFound {
		t.Fatalf("symlink raw status = %d, want 404", raw.Code)
	}
}

func TestRawAndRender(t *testing.T) {
	app, root := newTestApp(t)

	hello := canonical(app, "hello.md")
	rec := doJSON(t, app, http.MethodGet, rawURL(hello), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello") {
		t.Fatalf("raw status = %d body=%q", rec.Code, rec.Body)
	}

	body := saveRequest{Path: hello, Content: "![x](pic.png)\n\n**bold**"}
	rec = doJSON(t, app, http.MethodPost, "/api/render", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("render status = %d: %s", rec.Code, rec.Body)
	}
	rendered := decode[map[string]string](t, rec)
	if !strings.Contains(rendered["html"], "<strong>bold</strong>") {
		t.Fatalf("render HTML = %s", rendered["html"])
	}
	if !strings.Contains(rendered["html"], "/raw/"+primaryRootID(app)+"/pic.png") {
		t.Fatalf("image not rewritten: %s", rendered["html"])
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "hello.md"))
	if err != nil || !strings.Contains(string(onDisk), "Hello") {
		t.Fatalf("render endpoint modified the file: %q err=%v", onDisk, err)
	}
}

func TestStaticAndConfig(t *testing.T) {
	app, _ := newTestApp(t)

	rec := doJSON(t, app, http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "commentloop test") {
		t.Fatalf("index status = %d body=%q", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodGet, "/some/client/route", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "commentloop test") {
		t.Fatalf("spa fallback status = %d body=%q", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/config", nil)
	config := decode[map[string]any](t, rec)
	if config["rootName"] != filepath.Base(app.Root()) || config["version"] != "test" {
		t.Fatalf("config = %+v", config)
	}
}

func TestWebSocketPreviewAndExternalChange(t *testing.T) {
	app, root := newTestApp(t)
	hello := canonical(app, "hello.md")
	hs := httptest.NewServer(app)
	t.Cleanup(hs.Close)

	wsURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err := conn.WriteJSON(inMessage{Type: "subscribe", Path: hello}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	init := readUntil(t, conn, "subscribed")
	if init.Path != hello {
		t.Fatalf("subscribed path = %q, want %q", init.Path, hello)
	}

	if err := conn.WriteJSON(inMessage{Type: "edit", Path: hello, Content: "# Live preview"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	preview := readUntil(t, conn, "preview")
	if !strings.Contains(preview.HTML, "Live preview") {
		t.Fatalf("preview HTML = %s", preview.HTML)
	}

	// An external write (e.g. from another editor) should be pushed live.
	mustWrite(t, filepath.Join(root, "hello.md"), "# Changed outside\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var external outMessage
	for {
		var msg outMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("waiting for external doc: %v", err)
		}
		if msg.Type == "doc" && msg.Origin == "external" {
			external = msg
			break
		}
	}
	if !strings.Contains(external.Content, "Changed outside") {
		t.Fatalf("external content = %q", external.Content)
	}
	if !strings.Contains(external.HTML, "Changed outside") {
		t.Fatalf("external HTML = %q", external.HTML)
	}
}

func readUntil(t *testing.T, conn *websocket.Conn, want string) outMessage {
	t.Helper()
	for {
		var msg outMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if msg.Type == want {
			return msg
		}
	}
}

func TestOutMessageKeepsEmptyFields(t *testing.T) {
	data, err := json.Marshal(outMessage{Type: "doc", Path: "empty.md", Origin: "init"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`"content":""`, `"html":""`} {
		if !strings.Contains(got, want) {
			t.Fatalf("outMessage JSON missing %s: %s", want, got)
		}
	}
}

func TestHostAndOriginGuard(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "# A")

	app, err := New(Options{
		Root:    root,
		WebFS:   mapFS(),
		Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	req := httptest.NewRequest(http.MethodGet, "http://evil.example/api/tree", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback Host status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4571/api/tree", nil)
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback Host status = %d, want 200", rec.Code)
	}

	aPath := canonical(app, "a.md")
	do := func(origin string) int {
		body := bytes.NewBufferString(`{"path":"` + aPath + `","content":"# changed"}`)
		req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:4571/api/doc", body)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do("http://evil.example"); code != http.StatusForbidden {
		t.Fatalf("cross-origin save status = %d, want 403", code)
	}
	if code := do("http://127.0.0.1:4571"); code != http.StatusOK {
		t.Fatalf("same-origin save status = %d, want 200", code)
	}
	// Vite's dev server uses localhost:5173 while the API is on a different
	// loopback port; this pair must be accepted for the dev workflow.
	if code := do("http://localhost:5173"); code != http.StatusOK {
		t.Fatalf("loopback dev-origin save status = %d, want 200", code)
	}
	if code := do("http://localhost.evil.example"); code != http.StatusForbidden {
		t.Fatalf("lookalike origin save status = %d, want 403", code)
	}
}

func TestPreviewOnlyBlocksWrites(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "# A")

	app, err := New(Options{
		Root:        root,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
		PreviewOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	aPath := canonical(app, "a.md")
	newPath := canonical(app, "new.md")
	rec := doJSON(t, app, http.MethodGet, docURL(aPath), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read status = %d, want 200", rec.Code)
	}
	// Editing an existing document remains Agent-only.
	rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: aPath, Content: "# edited"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PUT existing status = %d, want 403", rec.Code)
	}
	// Creating and deleting documents are still available to the review UI.
	rec = doJSON(t, app, http.MethodPost, "/api/doc", saveRequest{Path: newPath, Content: "# new"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST new status = %d, body=%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "new.md")); err != nil {
		t.Fatalf("new document missing: %v", err)
	}
	rec = doJSON(t, app, http.MethodDelete, docURL(aPath), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE existing status = %d, want 204", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "a.md")); !os.IsNotExist(err) {
		t.Fatalf("delete did not remove a.md: %v", err)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/config", nil)
	config := decode[map[string]any](t, rec)
	if config["previewOnly"] != true {
		t.Fatalf("previewOnly = %v, want true", config["previewOnly"])
	}
	// Recreate the original for the WebSocket check; the review edit path must
	// remain closed even though document creation/deletion are allowed.
	mustWrite(t, filepath.Join(root, "a.md"), "# A")

	// The WebSocket edit path is also closed in preview-only mode.
	hs := httptest.NewServer(app)
	t.Cleanup(hs.Close)
	wsURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteJSON(inMessage{Type: "subscribe", Path: aPath}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, conn, "subscribed")
	if err := conn.WriteJSON(inMessage{Type: "edit", Path: aPath, Content: "# edited"}); err != nil {
		t.Fatal(err)
	}
	errMsg := readUntil(t, conn, "error")
	if !strings.Contains(errMsg.Message, "审阅") {
		t.Fatalf("edit error = %q, want review-mode message", errMsg.Message)
	}
}

func TestAutoRunPiOnUserComment(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "agent.md"), "# Agent\n\nOriginal line.\n")
	markerPath := filepath.Join(t.TempDir(), "agent-runs.log")
	scriptPath := filepath.Join(t.TempDir(), "fake-pi")
	script := "#!/bin/sh\n" +
		"while IFS= read -r line; do\n" +
		"  id=$(printf '%s\\n' \"$line\" | sed -n 's/.*\"id\":\"\\([^\"]*\\)\".*/\\1/p')\n" +
		"  printf '%s\\n' \"$line\" >> '" + markerPath + "'\n" +
		"  printf '{\"id\":\"%s\",\"type\":\"response\",\"command\":\"prompt\",\"success\":true,\"data\":{\"disposition\":\"started\"}}\\n' \"$id\"\n" +
		"  printf '{\"type\":\"agent_settled\",\"aborted\":false}\\n'\n" +
		"done\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	rec := doJSON(t, app, http.MethodGet, "/api/settings", nil)
	settings := decode[appSettings](t, rec)
	settings.Agent = codeAgentSettings{
		Type: "pi", Name: "Fake Pi", Command: scriptPath, Args: []string{}, AutoRun: true,
	}
	rec = doJSON(t, app, http.MethodPut, "/api/settings", settings)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", rec.Code, rec.Body)
	}
	path := canonical(app, "agent.md")
	rec = doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Original line.", Body: "请改一下",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	comment := decode[Comment](t, rec)
	waitForAgentRuns(t, markerPath, 1)
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "已经修改",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("agent reply status=%d", rec.Code)
	}
	time.Sleep(300 * time.Millisecond)
	if got := countLines(t, markerPath); got != 1 {
		t.Fatalf("agent reply triggered auto-run: %d runs", got)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "再改一版",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("user reply status=%d", rec.Code)
	}
	waitForAgentRuns(t, markerPath, 2)

	// The manual trigger remains available when auto-run is off.
	settings.Agent.AutoRun = false
	rec = doJSON(t, app, http.MethodPut, "/api/settings", settings)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable auto-run status=%d", rec.Code)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Original line.", Body: "手动处理这条",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("manual comment status=%d body=%s", rec.Code, rec.Body)
	}
	manual := decode[Comment](t, rec)
	time.Sleep(300 * time.Millisecond)
	if got := countLines(t, markerPath); got != 2 {
		t.Fatalf("auto-run off should not start agent; runs=%d", got)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/trigger", triggerCommentRequest{
		Path: path, ID: manual.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("manual trigger status=%d body=%s", rec.Code, rec.Body)
	}
	waitForAgentRuns(t, markerPath, 3)
}

func waitForAgentRuns(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if countLines(t, path) >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d agent runs; got %d", want, countLines(t, path))
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

func TestLocalCodeRepoBranchSwitch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	codeDir := t.TempDir()
	gitRun := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", codeDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "-b", "main")
	mustWrite(t, filepath.Join(codeDir, "branch.txt"), "main branch\n")
	gitRun("add", ".")
	gitRun("commit", "-m", "main")
	gitRun("checkout", "-b", "feature")
	mustWrite(t, filepath.Join(codeDir, "branch.txt"), "feature branch\n")
	gitRun("add", ".")
	gitRun("commit", "-m", "feature")
	gitRun("checkout", "main")

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	docPath := canonical(app, "doc.md")
	rec := doJSON(t, app, http.MethodPost, "/api/doc/code-repos", docCodeRepoRequest{
		DocPath: docPath, RepoPath: codeDir,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("link local repo status=%d body=%s", rec.Code, rec.Body)
	}
	repo := decode[codeRepoRecord](t, rec)
	rec = doJSON(t, app, http.MethodGet, "/api/code/branches?id="+repo.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("branches status=%d body=%s", rec.Code, rec.Body)
	}
	branches := decode[struct {
		Branches []string `json:"branches"`
	}](t, rec)
	if !sameStrings(branches.Branches, []string{"feature", "main"}) {
		t.Fatalf("branches = %v", branches.Branches)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/code/branch", switchBranchRequest{
		ID: repo.ID, Branch: "feature",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch branch status=%d body=%s", rec.Code, rec.Body)
	}
	switched := decode[codeRepoRecord](t, rec)
	if switched.Branch != "feature" {
		t.Fatalf("switched branch = %q", switched.Branch)
	}
	content, err := os.ReadFile(filepath.Join(codeDir, "branch.txt"))
	if err != nil || string(content) != "feature branch\n" {
		t.Fatalf("local branch content = %q, err=%v", content, err)
	}
	// Dirty local repositories are protected from branch switching.
	mustWrite(t, filepath.Join(codeDir, "dirty.txt"), "dirty\n")
	rec = doJSON(t, app, http.MethodPost, "/api/code/branch", switchBranchRequest{
		ID: repo.ID, Branch: "main",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dirty switch status=%d, want 400", rec.Code)
	}
}

func TestAgentJobQueueTokenAndPathACL(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n")
	mustWrite(t, filepath.Join(root, "other.md"), "# Other\n")
	app, err := New(Options{Root: root, StateDir: t.TempDir(), WebFS: mapFS(), Version: "test", AllowRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	docPath := canonical(app, "doc.md")
	otherPath := canonical(app, "other.md")
	job, err := app.agentJobs.create(AgentJob{
		DocPath: docPath, CommentID: "c1", AgentType: "generic", Prompt: "test",
		Capabilities: agentCapabilities{WriteDocs: true, ReadCode: false, ReplyComments: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, app, http.MethodPost, "/api/agent/runner/claim", agentRunnerClaimRequest{
		RunnerID: "runner-1", AgentType: "generic",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", rec.Code, rec.Body)
	}
	claimed := decode[agentClaimResponse](t, rec)
	if claimed.ID != job.ID || claimed.Token == "" {
		t.Fatalf("claimed = %+v", claimed)
	}
	headers := map[string]string{"X-Commentloop-Agent-Token": claimed.Token, "X-Commentloop-Agent": "1"}
	rec = doJSONHeaders(t, app, http.MethodPut, "/api/doc", saveRequest{Path: docPath, Content: "# changed"}, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("current doc write status=%d body=%s", rec.Code, rec.Body)
	}
	invalidHeaders := map[string]string{"X-Commentloop-Agent-Token": "job_invalid", "X-Commentloop-Agent": "1"}
	rec = doJSONHeaders(t, app, http.MethodPut, "/api/doc", saveRequest{Path: docPath, Content: "# invalid"}, invalidHeaders)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invalid token write status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSONHeaders(t, app, http.MethodPut, "/api/doc", saveRequest{Path: otherPath, Content: "# other"}, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other doc write status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSONHeaders(t, app, http.MethodGet, "/api/code/repos", nil, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code read status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/agent/runner/finish", agentRunnerFinishRequest{
		RunnerID: "runner-1", ID: job.ID, Success: true,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("finish status=%d body=%s", rec.Code, rec.Body)
	}
	if _, ok := app.agentCaps.lookupToken(claimed.Token); ok {
		t.Fatal("job capability token was not cleared")
	}
}

func TestAgentAnalysisCapabilityBlocksWrites(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n")
	app, err := New(Options{Root: root, StateDir: t.TempDir(), WebFS: mapFS(), Version: "test", AllowRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "doc.md")
	headers := map[string]string{
		"X-Commentloop-Agent":         "1",
		"X-Commentloop-Agent-Session": "session-analysis",
	}
	app.agentCaps.set("session-analysis", agentCapabilities{
		WriteDocs: false, ReadCode: true, ReplyComments: true,
	})
	rec := doJSONHeaders(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: "# changed"}, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("analysis PUT status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSONHeaders(t, app, http.MethodPost, "/api/doc", saveRequest{Path: canonical(app, "new.md"), Content: "# new"}, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("analysis POST status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSONHeaders(t, app, http.MethodDelete, docURL(path), nil, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("analysis DELETE status=%d body=%s", rec.Code, rec.Body)
	}
	app.agentCaps.set("session-analysis", agentCapabilities{
		WriteDocs: false, ReadCode: false, ReplyComments: true,
	})
	rec = doJSONHeaders(t, app, http.MethodGet, "/api/code/repos", nil, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("analysis code read status=%d body=%s", rec.Code, rec.Body)
	}
	app.agentCaps.set("session-analysis", agentCapabilities{
		WriteDocs: true, ReadCode: true, ReplyComments: true,
	})
	rec = doJSONHeaders(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: "# changed"}, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit PUT status=%d body=%s", rec.Code, rec.Body)
	}
	// An external Agent CLI without the managed session header keeps its
	// existing permission behavior.
	rec = doJSONHeaders(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: "# manual"}, map[string]string{
		"X-Commentloop-Agent": "1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("manual agent PUT status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestPromptTemplatesAndCommentTask(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n\n需要分析需求。\n")
	app, err := New(Options{Root: root, StateDir: t.TempDir(), WebFS: mapFS(), Version: "test", AllowRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	rec := doJSON(t, app, http.MethodGet, "/api/prompt-templates", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list templates status=%d body=%s", rec.Code, rec.Body)
	}
	var list struct {
		Templates []PromptTemplate `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Templates) < 3 {
		t.Fatalf("builtin templates = %+v", list.Templates)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/prompt-templates", promptTemplateRequest{
		Name: "接口评审", Description: "review API", Body: "重点检查接口兼容性和错误码。", AllowDocumentEdit: false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create template status=%d body=%s", rec.Code, rec.Body)
	}
	custom := decode[PromptTemplate](t, rec)
	if custom.Builtin || custom.AllowDocumentEdit {
		t.Fatalf("custom template = %+v", custom)
	}

	script := filepath.Join(t.TempDir(), "fake-pi-optimize")
	mustWrite(t, script, "#!/bin/sh\nprintf '优化后的接口评审提示词，保留 {{doc_path}}。\\n'\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := defaultAppSettings()
	settings.Agent.Command = script
	if err := app.settingsStore.save(settings); err != nil {
		t.Fatal(err)
	}
	app.settings.set(settings)
	rec = doJSON(t, app, http.MethodPost, "/api/prompt-templates/optimize", optimizePromptRequest{
		Name: custom.Name, Description: custom.Description, Body: "优化我", AllowDocumentEdit: false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("optimize status=%d body=%s", rec.Code, rec.Body)
	}
	var optimized struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &optimized); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(optimized.Body, "优化后的接口评审提示词") || !strings.Contains(optimized.Body, "{{doc_path}}") {
		t.Fatalf("optimized = %q", optimized.Body)
	}

	path := canonical(app, "doc.md")
	commentRec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "需要分析需求", Body: "请先拆解", PromptTemplateID: custom.ID,
	})
	if commentRec.Code != http.StatusOK {
		t.Fatalf("comment status=%d body=%s", commentRec.Code, commentRec.Body)
	}
	comment := decode[Comment](t, commentRec)
	if comment.PromptTemplateID != custom.ID {
		t.Fatalf("comment template = %q", comment.PromptTemplateID)
	}
	caps := app.commentCapabilities(comment)
	if caps.WriteDocs || caps.ReadCode || caps.ReplyComments {
		t.Fatalf("custom capabilities = %+v", caps)
	}
	prompt, err := app.buildReviewAgentPrompt(path, comment.ID, "zh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "接口评审") || !strings.Contains(prompt, "重点检查接口兼容性") {
		t.Fatalf("custom template missing from prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Do NOT call write_file") {
		t.Fatalf("analysis mode instruction missing:\n%s", prompt)
	}
}

func TestDiscussionImportAndCommentContext(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n\n使用字段 X。\n")
	stateDir := t.TempDir()
	sessionDir := filepath.Join(stateDir, "pi-sessions")
	mustMkdir(t, sessionDir)
	sessionPath := filepath.Join(sessionDir, "session.jsonl")
	mustWrite(t, sessionPath, strings.Join([]string{
		`{"type":"session","version":3,"id":"s1","timestamp":"2026-10-24T10:00:00Z","cwd":"/tmp/project"}`,
		`{"type":"message","timestamp":1761300001000,"message":{"role":"user","content":[{"type":"text","text":"我们确定使用字段 X，不要再改成 Y。"}]}}`,
		`{"type":"message","timestamp":1761300002000,"message":{"role":"assistant","content":[{"type":"text","text":"好的，字段 X 是最终方案。"}]}}`,
	}, "\n")+"\n")
	script := filepath.Join(t.TempDir(), "fake-pi")
	mustWrite(t, script, "#!/bin/sh\nprintf '## 已确认决策\n- 使用字段 X\n'\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	app, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(), Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	settings := defaultAppSettings()
	settings.Agent.Command = script
	if err := app.settingsStore.save(settings); err != nil {
		t.Fatal(err)
	}
	app.settings.set(settings)

	path := canonical(app, "doc.md")
	rec := doJSON(t, app, http.MethodGet, "/api/doc/discussions/available?path="+url.QueryEscape(path), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("available status=%d body=%s", rec.Code, rec.Body)
	}
	var available availableSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &available); err != nil {
		t.Fatal(err)
	}
	if len(available.Sessions) == 0 || available.Sessions[0].Title == "" {
		t.Fatalf("available sessions = %+v", available.Sessions)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/doc/discussions", importDiscussionRequest{
		Path: path, SessionPath: sessionPath,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body)
	}
	var imported importDiscussionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Consensus == nil || !strings.Contains(imported.Consensus.Content, "字段 X") {
		t.Fatalf("consensus = %+v error=%s", imported.Consensus, imported.ConsensusError)
	}
	if imported.Discussion.MessageCount != 2 {
		t.Fatalf("discussion = %+v", imported.Discussion)
	}
	if imported.Discussion.SourceKind != "commentloop" {
		t.Fatalf("source kind = %q", imported.Discussion.SourceKind)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/doc/discussions/available?path="+url.QueryEscape(path), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("available after import status=%d body=%s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &available); err != nil {
		t.Fatal(err)
	}
	if len(available.Sessions) == 0 || !available.Sessions[0].Imported {
		t.Fatalf("imported session not marked: %+v", available.Sessions)
	}

	commentRec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "使用字段 X", Body: "按讨论继续改", DiscussionIDs: []string{imported.Discussion.ID},
	})
	if commentRec.Code != http.StatusOK {
		t.Fatalf("comment status=%d body=%s", commentRec.Code, commentRec.Body)
	}
	comment := decode[Comment](t, commentRec)
	if len(comment.DiscussionIDs) != 1 {
		t.Fatalf("comment discussionIds = %+v", comment.DiscussionIDs)
	}
	prompt, err := app.buildReviewAgentPrompt(path, comment.ID, "zh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "使用字段 X") || !strings.Contains(prompt, "Document consensus") {
		t.Fatalf("prompt missing consensus/context:\n%s", prompt)
	}
	enPrompt, err := app.buildReviewAgentPrompt(path, comment.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enPrompt, "Task template: Document review") || !strings.Contains(enPrompt, "Edit the document according to the current comment") {
		t.Fatalf("English prompt did not use the English builtin template:\n%s", enPrompt)
	}

	// Continuing the original session is picked up by the poller and updates
	// both the stored transcript and the consensus.
	file, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"message","timestamp":1761300003000,"message":{"role":"user","content":[{"type":"text","text":"补充：字段 X 只用于新接口。"}]}}` + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	app.discussionSync.syncAll()
	deadline := time.Now().Add(4 * time.Second)
	for {
		discussion, ok := app.discussions.get(path, imported.Discussion.ID)
		if ok && discussion.MessageCount >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session changes were not synced")
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline = time.Now().Add(4 * time.Second)
	for {
		consensus, ok := app.discussions.consensus(path)
		if ok && consensus.Version > imported.Consensus.Version {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("consensus was not refreshed automatically")
		}
		time.Sleep(20 * time.Millisecond)
	}

	rec = doJSON(t, app, http.MethodDelete, "/api/doc/discussions?path="+url.QueryEscape(path)+"&id="+url.QueryEscape(imported.Discussion.ID), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete discussion status=%d body=%s", rec.Code, rec.Body)
	}
	if _, ok := app.discussions.consensus(path); ok {
		t.Fatal("consensus survived deleting its last discussion")
	}
}

func TestAgentSessions(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sessions.md"), "# Sessions\n")
	stateDir := t.TempDir()
	app, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := canonical(app, "sessions.md")
	type response struct {
		Path     string         `json:"path"`
		Sessions []agentSession `json:"sessions"`
		ActiveID string         `json:"activeId"`
	}
	get := func(app *Server) response {
		rec := doJSON(t, app, http.MethodGet, "/api/doc/agent-sessions?path="+url.QueryEscape(path), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("sessions status=%d body=%s", rec.Code, rec.Body)
		}
		return decode[response](t, rec)
	}
	first := get(app)
	if len(first.Sessions) != 1 || first.ActiveID == "" || !first.Sessions[0].Active {
		t.Fatalf("initial sessions = %+v", first)
	}
	rec := doJSON(t, app, http.MethodPost, "/api/doc/agent-sessions", agentSessionRequest{Path: path})
	if rec.Code != http.StatusOK {
		t.Fatalf("create session status=%d body=%s", rec.Code, rec.Body)
	}
	second := decode[agentSession](t, rec)
	if second.Title == "" || !second.Active {
		t.Fatalf("second session = %+v", second)
	}
	state := get(app)
	if len(state.Sessions) != 2 || state.ActiveID != second.ID {
		t.Fatalf("after create = %+v", state)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/doc/agent-sessions/active", agentSessionRequest{
		Path: path, ID: first.ActiveID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", rec.Code, rec.Body)
	}
	if got := get(app); got.ActiveID != first.ActiveID {
		t.Fatalf("active session = %s, want %s", got.ActiveID, first.ActiveID)
	}
	_ = app.Close()

	app2, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app2.Close() })
	persisted := get(app2)
	if len(persisted.Sessions) != 2 || persisted.ActiveID != first.ActiveID {
		t.Fatalf("sessions after restart = %+v", persisted)
	}
}

func TestSettingsPersistenceAndValidation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "# A")
	stateDir := t.TempDir()
	app, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, app, http.MethodGet, "/api/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status=%d", rec.Code)
	}
	settings := decode[appSettings](t, rec)
	if len(settings.LSPServers) < 5 || settings.Agent.Name == "" {
		t.Fatalf("default settings = %+v", settings)
	}
	settings.Agent.Name = "Claude Code"
	settings.Agent.Type = "claude-code"
	settings.Agent.Command = "claude"
	for i := range settings.LSPServers {
		if settings.LSPServers[i].ID == "go" {
			settings.LSPServers[i].Command = "gopls"
		}
	}
	rec = doJSON(t, app, http.MethodPut, "/api/settings", settings)
	if rec.Code != http.StatusOK {
		t.Fatalf("save settings status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/config", nil)
	config := decode[map[string]any](t, rec)
	if config["agentName"] != "Claude Code" {
		t.Fatalf("agentName = %v", config["agentName"])
	}
	rec = doJSON(t, app, http.MethodPost, "/api/settings/check-command", checkCommandRequest{Command: "go"})
	if rec.Code != http.StatusOK {
		t.Fatalf("check command status=%d", rec.Code)
	}
	if found := decode[map[string]any](t, rec); found["found"] != true {
		t.Fatalf("go check = %+v", found)
	}

	// Invalid enabled LSP with no command is rejected.
	invalid := settings
	invalid.LSPServers = append([]lsp.ServerConfig(nil), settings.LSPServers...)
	for i := range invalid.LSPServers {
		if invalid.LSPServers[i].ID == "go" {
			invalid.LSPServers[i].Enabled = true
			invalid.LSPServers[i].Command = ""
		}
	}
	rec = doJSON(t, app, http.MethodPut, "/api/settings", invalid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status=%d, want 400", rec.Code)
	}
	_ = app.Close()

	app2, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app2.Close() })
	rec = doJSON(t, app2, http.MethodGet, "/api/settings", nil)
	reloaded := decode[appSettings](t, rec)
	if reloaded.Agent.Name != "Claude Code" || reloaded.Agent.Type != "claude-code" {
		t.Fatalf("settings not persisted: %+v", reloaded.Agent)
	}
}

func TestDocumentCodeRepositories(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "guide.md"), "# Guide\n")
	codeDir := t.TempDir()
	mustWrite(t, filepath.Join(codeDir, "main.go"), `package main

func Hello() string { return "Hello" }
`)
	mustWrite(t, filepath.Join(codeDir, "README.md"), "# Code repo\n")
	mustMkdir(t, filepath.Join(codeDir, "node_modules"))
	mustWrite(t, filepath.Join(codeDir, "node_modules", "ignored.js"), "should not be listed")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "guide.md")

	rec := doJSON(t, app, http.MethodPost, "/api/doc/code-repos", docCodeRepoRequest{
		DocPath: path, RepoPath: codeDir,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("link repo status=%d body=%s", rec.Code, rec.Body)
	}
	repo := decode[codeRepoRecord](t, rec)
	expectedRepoPath, _ := filepath.EvalSymlinks(codeDir)
	if repo.ID == "" || repo.Path != expectedRepoPath {
		t.Fatalf("repo = %+v", repo)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/doc/code-repos?path="+url.QueryEscape(path), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("doc repos status=%d", rec.Code)
	}
	linked := decode[struct {
		Path  string           `json:"path"`
		Repos []codeRepoRecord `json:"repos"`
	}](t, rec)
	if len(linked.Repos) != 1 || linked.Repos[0].ID != repo.ID {
		t.Fatalf("linked repos = %+v", linked.Repos)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/code/files?repo="+repo.ID+"&query=main", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code files status=%d body=%s", rec.Code, rec.Body)
	}
	files := decode[coderepo.ListResult](t, rec)
	if len(files.Files) != 1 || files.Files[0].Path != "main.go" {
		t.Fatalf("files = %+v", files)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/code/file?repo="+repo.ID+"&path=main.go", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read code status=%d body=%s", rec.Code, rec.Body)
	}
	code := decode[coderepo.ReadResult](t, rec)
	if !strings.Contains(code.Content, "func Hello") || code.TotalLines < 3 {
		t.Fatalf("code = %+v", code)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/code/search?repo="+repo.ID+"&q=hello", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body)
	}
	search := decode[coderepo.SearchResult](t, rec)
	if len(search.Matches) != 1 || search.Matches[0].Path != "main.go" || search.Matches[0].Line != 3 {
		t.Fatalf("search = %+v", search)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/code/file?repo="+repo.ID+"&path=../outside.md", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal status=%d, want 400", rec.Code)
	}

	rec = doJSON(t, app, http.MethodDelete,
		"/api/doc/code-repos?path="+url.QueryEscape(path)+"&id="+repo.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unlink status=%d", rec.Code)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/doc/code-repos?path="+url.QueryEscape(path), nil)
	linked = decode[struct {
		Path  string           `json:"path"`
		Repos []codeRepoRecord `json:"repos"`
	}](t, rec)
	if len(linked.Repos) != 0 {
		t.Fatalf("repos after unlink = %+v", linked.Repos)
	}
}

func TestPreviewAllowsAgentWritesAndRollback(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "review.md"), "# Review\n\nInitial.")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true, PreviewOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "review.md")
	agentHeaders := map[string]string{"X-Commentloop-Agent": "1"}

	v1 := "# Review\n\nAgent first."
	rec := doJSONHeaders(t, app, http.MethodPut, "/api/doc",
		saveRequest{Path: path, Content: v1}, agentHeaders)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent write in review mode status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Agent first.", Body: "改这段",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create comment status=%d", rec.Code)
	}
	comment := decode[Comment](t, rec)
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "第一次",
		Before: "Initial.", After: "Agent first.", Snapshot: v1,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("agent reply status=%d body=%s", rec.Code, rec.Body)
	}
	first := decode[Comment](t, rec)

	v2 := "# Review\n\nAgent second.\n\nUnrelated later."
	rec = doJSONHeaders(t, app, http.MethodPut, "/api/doc",
		saveRequest{Path: path, Content: v2}, agentHeaders)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent second write status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "第二次",
		Before: "Agent first.", After: "Agent second.", Snapshot: v2,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("agent second reply status=%d body=%s", rec.Code, rec.Body)
	}

	// The frontend may roll back a reviewed section in review mode even though
	// normal frontend saves stay disabled.
	rec = doJSON(t, app, http.MethodPost, "/api/comments/rollback", rollbackCommentRequest{
		Path: path, ID: comment.ID, MessageID: first.Messages[1].ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback in review mode status=%d body=%s", rec.Code, rec.Body)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "review.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(onDisk)
	if !strings.Contains(got, "Agent first.") || strings.Contains(got, "Agent second.") {
		t.Fatalf("review rollback content = %q", got)
	}
	if !strings.Contains(got, "Unrelated later.") {
		t.Fatalf("review rollback dropped unrelated later content: %q", got)
	}
}

func TestDebugDiskAssetsAndReload(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "# A")

	webDir := t.TempDir()
	mustWrite(t, filepath.Join(webDir, "index.html"), "<!doctype html>DISK_INDEX")
	mustMkdir(t, filepath.Join(webDir, "assets"))
	mustWrite(t, filepath.Join(webDir, "assets", "app.js"), "console.log('disk')")
	mustWrite(t, filepath.Join(webDir, "theme-init.js"), "/* disk */")

	embedded := mapFS()
	embedded["theme-init.js"] = &fstest.MapFile{Data: []byte("/* embedded */")}

	app, err := New(Options{
		Root:        root,
		WebFS:       embedded,
		Version:     "test",
		AllowRemote: true,
		Debug:       true,
		WebDir:      webDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	rec := doJSON(t, app, http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DISK_INDEX") {
		t.Fatalf("debug index status=%d body=%q", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("debug cache-control = %q, want no-store", got)
	}
	rec = doJSON(t, app, http.MethodGet, "/assets/app.js", nil)
	if !strings.Contains(rec.Body.String(), "console.log('disk')") {
		t.Fatalf("disk asset = %q", rec.Body)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/config", nil)
	config := decode[map[string]any](t, rec)
	if config["debug"] != true || config["hotReload"] != true {
		t.Fatalf("config = %+v", config)
	}

	hs := httptest.NewServer(app)
	t.Cleanup(hs.Close)
	wsURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Give the poller a tick to record the initial signature, then rebuild.
	time.Sleep(600 * time.Millisecond)
	mustWrite(t, filepath.Join(webDir, "index.html"), "<!doctype html>DISK_INDEX_V2")

	for {
		var msg outMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("waiting for reload: %v", err)
		}
		if msg.Type == "reload" {
			break
		}
	}
}

func TestMultipleRootsAndPersistence(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	mustWrite(t, filepath.Join(primary, "a.md"), "# A")
	mustWrite(t, filepath.Join(extra, "b.md"), "# B\n\n![pic](img/p.png)")
	mustMkdir(t, filepath.Join(extra, "sub"))
	mustWrite(t, filepath.Join(extra, "sub", "c.md"), "# C")
	stateDir := t.TempDir()

	app, err := New(Options{
		Root:        primary,
		ExtraRoots:  []string{extra},
		StateDir:    stateDir,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	if got := app.RootCount(); got != 2 {
		t.Fatalf("root count = %d, want 2", got)
	}
	treeRec := doJSON(t, app, http.MethodGet, "/api/tree", nil)
	tree := decode[treeResponse](t, treeRec)
	if len(tree.Roots) != 2 || len(tree.Files) != 3 {
		t.Fatalf("tree roots=%d files=%d", len(tree.Roots), len(tree.Files))
	}
	extraID := canonicalIn(app, mustResolve(t, extra), "")
	extraID = strings.TrimSuffix(extraID, "/")
	if extraID == "" {
		t.Fatal("extra root id not found")
	}
	bPath := extraID + "/b.md"
	rec := doJSON(t, app, http.MethodGet, docURL(bPath), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read extra root status=%d body=%s", rec.Code, rec.Body)
	}
	doc := decode[docResponse](t, rec)
	if !strings.Contains(doc.HTML, "B") || !strings.Contains(doc.HTML, "/raw/"+extraID+"/img/p.png") {
		t.Fatalf("extra doc HTML = %s", doc.HTML)
	}
	rec = doJSON(t, app, http.MethodGet, rawURL(bPath), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("raw from extra root status=%d", rec.Code)
	}

	// Add a third directory at runtime; it must be persisted.
	runtimeDir := t.TempDir()
	mustWrite(t, filepath.Join(runtimeDir, "x.md"), "# X")
	rec = doJSON(t, app, http.MethodPost, "/api/roots", addRootRequest{Path: runtimeDir})
	if rec.Code != http.StatusOK {
		t.Fatalf("add root status=%d body=%s", rec.Code, rec.Body)
	}
	added := decode[addRootResponse](t, rec)
	if added.Root.ID == "" || added.Root.Name != filepath.Base(mustResolve(t, runtimeDir)) {
		t.Fatalf("added root = %+v", added.Root)
	}
	xPath := added.Root.ID + "/x.md"
	rec = doJSON(t, app, http.MethodGet, docURL(xPath), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read runtime root status=%d", rec.Code)
	}

	// Duplicate / invalid / file paths are rejected.
	rec = doJSON(t, app, http.MethodPost, "/api/roots", addRootRequest{Path: runtimeDir})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate root status=%d, want 409", rec.Code)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/roots", addRootRequest{Path: filepath.Join(runtimeDir, "x.md")})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("file as root status=%d, want 400", rec.Code)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/roots", addRootRequest{Path: filepath.Join(runtimeDir, "nope")})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing root status=%d, want 400", rec.Code)
	}

	state := loadRootsFromStateDir(t, stateDir)
	resolvedPrimary := mustResolve(t, primary)
	resolvedExtra := mustResolve(t, extra)
	resolvedRuntime := mustResolve(t, runtimeDir)
	wantRoots := []string{resolvedPrimary, resolvedExtra, resolvedRuntime}
	if got := statePaths(state); !sameStrings(got, wantRoots) {
		t.Fatalf("persisted roots = %v, want %v", got, wantRoots)
	}

	// Remove the extra root: files stay on disk, tree shrinks.
	rec = doJSON(t, app, http.MethodDelete, "/api/roots?id="+extraID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove root status=%d body=%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(extra, "b.md")); err != nil {
		t.Fatalf("removing a root must not delete files: %v", err)
	}
	tree = decode[treeResponse](t, doJSON(t, app, http.MethodGet, "/api/tree", nil))
	if len(tree.Roots) != 2 {
		t.Fatalf("roots after remove = %d, want 2", len(tree.Roots))
	}
	rec = doJSON(t, app, http.MethodDelete, "/api/roots?id=does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown remove status=%d, want 404", rec.Code)
	}

	// A new process with the same state directory should load the persisted root.
	app2, err := New(Options{
		Root:        primary,
		StateDir:    stateDir,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app2.Close() })
	tree = decode[treeResponse](t, doJSON(t, app2, http.MethodGet, "/api/tree", nil))
	if len(tree.Roots) != 2 {
		t.Fatalf("reloaded roots = %d, want 2", len(tree.Roots))
	}
	foundX := false
	for _, f := range tree.Files {
		if f.RelPath == "x.md" && f.RootID == added.Root.ID {
			foundX = true
		}
	}
	if !foundX {
		t.Fatal("persisted runtime root was not reloaded")
	}

	// Removing the persisted root leaves only the primary in the state file.
	rec = doJSON(t, app2, http.MethodDelete, "/api/roots?id="+added.Root.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove persisted root status=%d", rec.Code)
	}
	state = loadRootsFromStateDir(t, stateDir)
	if want := []string{resolvedPrimary}; !sameStrings(statePaths(state), want) {
		t.Fatalf("persisted roots after remove = %v, want %v", state, want)
	}
}

func loadRootsFromStateDir(t *testing.T, stateDir string) []stateRoot {
	t.Helper()
	db, err := openStateDB(stateDir)
	if err != nil {
		t.Fatalf("open state DB: %v", err)
	}
	defer closeStateDB(db)
	defs, err := loadRootsDB(db)
	if err != nil {
		t.Fatalf("load roots DB: %v", err)
	}
	return defs
}

func statePaths(defs []stateRoot) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Path)
	}
	return out
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func mustResolve(t *testing.T, path string) string {
	t.Helper()
	resolved, err := resolveDirectory(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}

func TestCLIExtraRootPersistsAcrossRestart(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	mustWrite(t, filepath.Join(primary, "a.md"), "# A")
	mustWrite(t, filepath.Join(extra, "b.md"), "# B")
	stateDir := t.TempDir()

	app1, err := New(Options{
		Root:        primary,
		ExtraRoots:  []string{extra},
		StateDir:    stateDir,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if app1.RootCount() != 2 {
		t.Fatalf("initial root count = %d, want 2", app1.RootCount())
	}
	if err := app1.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart without --add-dir: the extra directory must still be restored.
	app2, err := New(Options{
		Root:        primary,
		StateDir:    stateDir,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app2.Close() })

	tree := decode[treeResponse](t, doJSON(t, app2, http.MethodGet, "/api/tree", nil))
	if len(tree.Roots) != 2 {
		t.Fatalf("restored roots = %d, want 2", len(tree.Roots))
	}
	found := false
	for _, f := range tree.Files {
		if f.RelPath == "b.md" && f.RootName == filepath.Base(mustResolve(t, extra)) {
			found = true
		}
	}
	if !found {
		t.Fatal("extra root from CLI was not restored after restart")
	}
}

func TestBrowseDirectories(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustMkdir(t, filepath.Join(root, "Beta"))
	mustMkdir(t, filepath.Join(root, ".hidden"))
	mustWrite(t, filepath.Join(root, "file.txt"), "x")

	app, err := New(Options{
		Root:        root,
		WebFS:       mapFS(),
		Version:     "test",
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	resolved := mustResolve(t, root)
	rec := doJSON(t, app, http.MethodGet, "/api/browse?path="+url.QueryEscape(resolved), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("browse status = %d: %s", rec.Code, rec.Body)
	}
	resp := decode[browseResponse](t, rec)
	if resp.Path != resolved {
		t.Fatalf("browse path = %q", resp.Path)
	}
	if resp.Parent != filepath.Dir(resolved) {
		t.Fatalf("parent = %q, want %q", resp.Parent, filepath.Dir(resolved))
	}
	names := make([]string, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		names = append(names, entry.Name)
	}
	want := []string{"alpha", "Beta"}
	if !sameStrings(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	if len(resp.Shortcuts) == 0 {
		t.Fatal("expected shortcuts")
	}

	rec = doJSON(t, app, http.MethodGet, "/api/browse?path="+url.QueryEscape(resolved)+"&hidden=1", nil)
	hiddenResp := decode[browseResponse](t, rec)
	foundHidden := false
	for _, entry := range hiddenResp.Entries {
		if entry.Name == ".hidden" && entry.Hidden {
			foundHidden = true
		}
	}
	if !foundHidden {
		t.Fatal("hidden directory missing with hidden=1")
	}

	rec = doJSON(t, app, http.MethodGet, "/api/browse?path="+url.QueryEscape(filepath.Join(resolved, "file.txt")), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("file browse status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/browse?path="+url.QueryEscape(filepath.Join(resolved, "missing")), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing browse status = %d, want 404", rec.Code)
	}
}

func TestRootStateV1Migration(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	mustWrite(t, filepath.Join(primary, "a.md"), "# A")
	mustWrite(t, filepath.Join(extra, "b.md"), "# B")
	stateDir := t.TempDir()
	resolvedExtra := mustResolve(t, extra)
	legacy := fmt.Sprintf("{\n  \"version\": 1,\n  \"roots\": [%q]\n}\n", resolvedExtra)
	if err := os.WriteFile(filepath.Join(stateDir, "roots.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := New(Options{
		Root: primary, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	tree := decode[treeResponse](t, doJSON(t, app, http.MethodGet, "/api/tree", nil))
	if len(tree.Roots) != 2 {
		t.Fatalf("roots after v1 migration = %d, want 2", len(tree.Roots))
	}

	defs := loadRootsFromStateDir(t, stateDir)
	if len(defs) != 2 || defs[1].Kind != rootKindLocal {
		t.Fatalf("migrated roots = %+v", defs)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "commentloop.db")); err != nil {
		t.Fatalf("sqlite database missing: %v", err)
	}
}

func TestTreeServesRemoteCacheWithoutSFTP(t *testing.T) {
	app, _ := newTestApp(t)
	ro := &root{
		ID: "rtreecache", Kind: rootKindRemote, Path: "/remote",
		Name:   "remote-cache",
		Remote: &remoteConfig{Host: "unreachable", Port: 22, User: "tester", Path: "/remote"},
	}
	app.rootMu.Lock()
	app.roots[ro.ID] = ro
	app.rootList = append(app.rootList, ro)
	app.rootMu.Unlock()
	t.Cleanup(func() {
		app.rootMu.Lock()
		delete(app.roots, ro.ID)
		for i, item := range app.rootList {
			if item.ID == ro.ID {
				app.rootList = append(app.rootList[:i], app.rootList[i+1:]...)
				break
			}
		}
		app.rootMu.Unlock()
		app.forgetRemoteTree(ro.ID)
	})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/api/tree", nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		done <- rec
	}()
	var rec *httptest.ResponseRecorder
	select {
	case rec = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tree request blocked on an unavailable remote root")
	}
	tree := decode[treeResponse](t, rec)
	foundRoot := false
	for _, item := range tree.Roots {
		if item.ID == ro.ID {
			foundRoot = true
			if len(item.Files) != 0 {
				t.Fatalf("expected empty cached remote root, got %d files", len(item.Files))
			}
		}
	}
	if !foundRoot {
		t.Fatal("remote root missing from tree response")
	}

	app.storeRemoteTree(ro, []remoteFile{{
		Rel: "cached.md", Name: "cached.md", Size: 3, ModTime: time.Now(),
	}})
	tree = decode[treeResponse](t, doJSON(t, app, http.MethodGet, "/api/tree", nil))
	for _, item := range tree.Roots {
		if item.ID == ro.ID {
			if len(item.Files) != 1 || item.Files[0].RelPath != "cached.md" {
				t.Fatalf("cached remote files = %+v", item.Files)
			}
		}
	}
}

func TestCommentsSQLitePersistence(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	mustWrite(t, filepath.Join(root, "hello.md"), "# Hello\n\n- [x] done\n")
	app, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := canonical(app, "hello.md")

	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Hello", Body: "请优化这段",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create comment status=%d body=%s", rec.Code, rec.Body)
	}
	comment := decode[Comment](t, rec)
	if comment.ID == "" || comment.Line != 1 || len(comment.Messages) != 1 {
		t.Fatalf("comment = %+v", comment)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Body: "改成 Hi",
		Before: "Hello", After: "Hi",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reply status=%d body=%s", rec.Code, rec.Body)
	}
	replied := decode[Comment](t, rec)
	if len(replied.Messages) != 2 || replied.Messages[1].Author != "agent" ||
		replied.Messages[1].Before != "Hello" || replied.Messages[1].After != "Hi" {
		t.Fatalf("reply = %+v", replied)
	}
	if replied.Messages[1].VersionID == "" {
		t.Fatal("an agent reply without an explicit snapshot should capture the current document")
	}

	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "commentloop.db")); err != nil {
		t.Fatalf("sqlite db missing: %v", err)
	}

	app2, err := New(Options{
		Root: root, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app2.Close() })
	rec = doJSON(t, app2, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list comments status=%d", rec.Code)
	}
	list := decode[commentsResponse](t, rec)
	if len(list.Comments) != 1 || len(list.Comments[0].Messages) != 2 {
		t.Fatalf("comments after restart = %+v", list.Comments)
	}

	// The agent can add another revision but cannot close the thread.
	rec = doJSON(t, app2, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "改完一版，但不会替用户关闭",
		Resolve: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("agent reply status=%d body=%s", rec.Code, rec.Body)
	}
	if agentResolved := decode[Comment](t, rec); agentResolved.Resolved {
		t.Fatal("an agent reply must not resolve a comment")
	}

	rec = doJSON(t, app2, http.MethodPost, "/api/comments/resolve", resolveCommentRequest{
		Path: path, ID: comment.ID, Resolved: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d", rec.Code)
	}
	if resolved := decode[Comment](t, rec); !resolved.Resolved {
		t.Fatal("comment was not resolved")
	}

	// A user reply means "not satisfied yet" and reopens the thread.
	rec = doJSON(t, app2, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "还需要再改一版",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("user reply status=%d body=%s", rec.Code, rec.Body)
	}
	reopened := decode[Comment](t, rec)
	if reopened.Resolved {
		t.Fatal("a user reply should reopen a resolved comment")
	}

	rec = doJSON(t, app2, http.MethodDelete, "/api/comments?path="+url.QueryEscape(path)+"&id="+comment.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete comment status=%d", rec.Code)
	}
	rec = doJSON(t, app2, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	if got := decode[commentsResponse](t, rec); len(got.Comments) != 0 {
		t.Fatalf("comments after delete = %+v", got.Comments)
	}
}

func TestCommentGroups(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "group.md"), "# Group\n\nAlpha\n\nBeta\n\nGamma")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "group.md")

	makeComment := func(quote string) Comment {
		rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
			Path: path, Quote: quote, Body: "调整 " + quote,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s status=%d body=%s", quote, rec.Code, rec.Body)
		}
		return decode[Comment](t, rec)
	}
	alpha := makeComment("Alpha")
	beta := makeComment("Beta")
	gamma := makeComment("Gamma")

	group := func(id string, members ...string) {
		rec := doJSON(t, app, http.MethodPost, "/api/comments/group", groupCommentRequest{
			Path: path, ID: id, MemberIDs: members,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("group status=%d body=%s", rec.Code, rec.Body)
		}
	}
	byQuote := func() map[string]Comment {
		rec := doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status=%d", rec.Code)
		}
		out := make(map[string]Comment)
		for _, comment := range decode[commentsResponse](t, rec).Comments {
			out[comment.Quote] = comment
		}
		return out
	}

	group(alpha.ID, beta.ID)
	comments := byQuote()
	if comments["Alpha"].GroupID == "" || comments["Alpha"].GroupID != comments["Beta"].GroupID {
		t.Fatalf("alpha/beta not grouped: %+v", comments)
	}
	if comments["Gamma"].GroupID != "" {
		t.Fatalf("gamma should not be grouped: %+v", comments["Gamma"])
	}

	group(alpha.ID, beta.ID, gamma.ID)
	comments = byQuote()
	if comments["Alpha"].GroupID != comments["Beta"].GroupID ||
		comments["Alpha"].GroupID != comments["Gamma"].GroupID {
		t.Fatalf("three comments not grouped: %+v", comments)
	}

	group(alpha.ID, beta.ID)
	comments = byQuote()
	if comments["Gamma"].GroupID != "" {
		t.Fatalf("gamma should have left the group: %+v", comments["Gamma"])
	}
	if comments["Alpha"].GroupID == "" || comments["Alpha"].GroupID != comments["Beta"].GroupID {
		t.Fatalf("alpha/beta group broken: %+v", comments)
	}

	group(alpha.ID)
	comments = byQuote()
	if comments["Alpha"].GroupID != "" || comments["Beta"].GroupID != "" {
		t.Fatalf("unlink should dissolve the pair: %+v", comments)
	}

	group(alpha.ID, beta.ID)
	rec := doJSON(t, app, http.MethodDelete,
		"/api/comments?path="+url.QueryEscape(path)+"&id="+url.QueryEscape(beta.ID), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete beta status=%d", rec.Code)
	}
	comments = byQuote()
	if comments["Alpha"].GroupID != "" {
		t.Fatalf("group should dissolve after deleting its peer: %+v", comments["Alpha"])
	}
}

func TestCommentsSortByCreationTime(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "order.md"), "# Order\n\nFirst paragraph.\n\nSecond paragraph.\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "order.md")
	create := func(quote string) Comment {
		rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
			Path: path, Quote: quote, Body: "评论 " + quote,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s status=%d", quote, rec.Code)
		}
		return decode[Comment](t, rec)
	}
	// Create the later-line comment first. It must remain first after listing and
	// after being updated.
	secondLine := create("Second paragraph.")
	time.Sleep(2 * time.Millisecond)
	firstLine := create("First paragraph.")
	rec := doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: secondLine.ID, Author: "user", Body: "补充",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reply status=%d", rec.Code)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	comments := decode[commentsResponse](t, rec).Comments
	if len(comments) != 2 || comments[0].ID != secondLine.ID || comments[1].ID != firstLine.ID {
		t.Fatalf("comments not sorted by creation time: %+v", comments)
	}
}

func TestCommentRollbackBefore(t *testing.T) {
	root := t.TempDir()
	initial := "# Before\n\nAlpha original.\n\nStable line.\n"
	mustWrite(t, filepath.Join(root, "before.md"), initial)
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "before.md")
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Alpha original.", Body: "改一下",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d", rec.Code)
	}
	comment := decode[Comment](t, rec)
	v1 := "# Before\n\nAlpha first revision.\n\nStable changed later.\n"
	rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: v1})
	if rec.Code != http.StatusOK {
		t.Fatalf("save status=%d", rec.Code)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "第一次修改",
		Before: "Alpha original.", After: "Alpha first revision.",
		BeforeSnapshot: initial, Snapshot: v1,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reply status=%d body=%s", rec.Code, rec.Body)
	}
	replied := decode[Comment](t, rec)
	message := replied.Messages[len(replied.Messages)-1]
	if message.BeforeVersionID == "" || message.VersionID == "" {
		t.Fatalf("missing snapshots: %+v", message)
	}
	rec = doJSON(t, app, http.MethodPost, "/api/comments/rollback", rollbackCommentRequest{
		Path: path, ID: comment.ID, MessageID: message.ID, Side: "before",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback before status=%d body=%s", rec.Code, rec.Body)
	}
	result := decode[rollbackCommentResponse](t, rec)
	expected := "# Before\n\nAlpha original.\n\nStable changed later.\n"
	onDisk, err := os.ReadFile(filepath.Join(root, "before.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != expected || result.Doc.Content != expected {
		t.Fatalf("rollback before content = %q, want %q", string(onDisk), expected)
	}
	last := result.Comment.Messages[len(result.Comment.Messages)-1]
	if last.Author != "system" || !strings.Contains(last.Body, "修改前") || result.Comment.Resolved {
		t.Fatalf("rollback before record = %+v", last)
	}
}

func TestCommentRollback(t *testing.T) {
	root := t.TempDir()
	initial := "# Rollback\n\nAlpha original.\n\nBeta line.\n"
	mustWrite(t, filepath.Join(root, "rollback.md"), initial)
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "rollback.md")

	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Alpha original.", Body: "请改这一段",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	comment := decode[Comment](t, rec)

	writeDoc := func(content string) {
		rec := doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: content})
		if rec.Code != http.StatusOK {
			t.Fatalf("save status=%d body=%s", rec.Code, rec.Body)
		}
	}
	reply := func(content, beforeContent, after, body string) Comment {
		writeDoc(content)
		rec := doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
			Path: path, ID: comment.ID, Author: "agent", Body: body,
			Before: "Alpha original.", After: after, BeforeSnapshot: beforeContent, Snapshot: content,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("reply status=%d body=%s", rec.Code, rec.Body)
		}
		return decode[Comment](t, rec)
	}
	v1 := "# Rollback\n\nAlpha first revision.\n\nBeta line.\n"
	v2 := "# Rollback\n\nAlpha second revision.\n\nBeta changed later.\n"
	// Only the commented section is restored; the later change to Beta stays.
	expected := "# Rollback\n\nAlpha first revision.\n\nBeta changed later.\n"
	first := reply(v1, initial, "Alpha first revision.", "第一次修改")
	second := reply(v2, v1, "Alpha second revision.", "第二次修改")
	if len(first.Messages) != 2 || first.Messages[1].VersionID == "" {
		t.Fatalf("first reply missing version: %+v", first.Messages)
	}
	if len(second.Messages) != 3 || second.Messages[2].VersionID == "" {
		t.Fatalf("second reply missing version: %+v", second.Messages)
	}
	if first.Messages[1].VersionID == second.Messages[2].VersionID {
		t.Fatal("different revisions should have different snapshots")
	}
	firstVersion := first.Messages[1].VersionID

	// User resolution is reversible by a rollback: the restored content is
	// still a review state awaiting confirmation.
	rec = doJSON(t, app, http.MethodPost, "/api/comments/resolve", resolveCommentRequest{
		Path: path, ID: comment.ID, Resolved: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d", rec.Code)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/comments/rollback", rollbackCommentRequest{
		Path: path, ID: comment.ID, MessageID: first.Messages[1].ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", rec.Code, rec.Body)
	}
	result := decode[rollbackCommentResponse](t, rec)
	if result.Round != 1 || result.Comment.Resolved {
		t.Fatalf("rollback result = %+v", result)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "rollback.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != expected {
		t.Fatalf("rollback content = %q, want %q", string(onDisk), expected)
	}
	if got := result.Doc.Content; got != expected {
		t.Fatalf("rollback response content = %q", got)
	}
	last := result.Comment.Messages[len(result.Comment.Messages)-1]
	if last.Author != "system" || last.VersionID != "" ||
		!strings.Contains(last.Body, "局部回滚") ||
		last.Before == "" || last.After != "Alpha first revision." {
		t.Fatalf("rollback system message = %+v", last)
	}
	if firstVersion == "" {
		t.Fatal("empty version id")
	}
}

func TestFormattedSelectionFallsBackToCompactLocation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "formatted.md"), "# Formatted\n\nThis is **bold** text.\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "formatted.md")
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "This is bold text.", Body: "这里看得见粗体",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	comment := decode[Comment](t, rec)
	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	comments := decode[commentsResponse](t, rec).Comments
	if len(comments) != 1 || comments[0].ID != comment.ID {
		t.Fatalf("comments = %+v", comments)
	}
	if comments[0].Orphaned || comments[0].Line != 3 {
		t.Fatalf("formatted selection not located: %+v", comments[0])
	}
}

func TestCommentReanchorFallsBackToVersionSection(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "reanchor.md"), "# Reanchor\n\nOld text.\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "reanchor.md")
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Old text.", Body: "换一种写法",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d", rec.Code)
	}
	comment := decode[Comment](t, rec)

	content := "# Reanchor\n\nThis is **bold** text.\n"
	rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: content})
	if rec.Code != http.StatusOK {
		t.Fatalf("save status=%d", rec.Code)
	}
	rawAfter := "This is **bold** text."
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "改成粗体",
		Before: "Old text.", After: rawAfter, Snapshot: content,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reply status=%d body=%s", rec.Code, rec.Body)
	}

	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	comments := decode[commentsResponse](t, rec).Comments
	if len(comments) != 1 || comments[0].Orphaned || comments[0].Line != 3 {
		t.Fatalf("formatted comment was not reanchored: %+v", comments)
	}
	if !strings.Contains(comments[0].Anchor, "**bold**") {
		t.Fatalf("anchor = %q, want raw section", comments[0].Anchor)
	}
}

func TestGroupRollbackRecordsAllLinkedComments(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "group-rollback.md"), "# Group\n\nAlpha zero.\n\nBeta zero.\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "group-rollback.md")

	create := func(quote string) Comment {
		rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
			Path: path, Quote: quote, Body: "一起调整",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s status=%d", quote, rec.Code)
		}
		return decode[Comment](t, rec)
	}
	alpha := create("Alpha zero.")
	beta := create("Beta zero.")
	rec := doJSON(t, app, http.MethodPost, "/api/comments/group", groupCommentRequest{
		Path: path, ID: alpha.ID, MemberIDs: []string{beta.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("group status=%d", rec.Code)
	}

	writeDoc := func(content string) {
		rec := doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: content})
		if rec.Code != http.StatusOK {
			t.Fatalf("save status=%d", rec.Code)
		}
	}
	reply := func(id, body, after, snapshot string) Comment {
		rec := doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
			Path: path, ID: id, Author: "agent", Body: body,
			After: after, Snapshot: snapshot,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("reply status=%d body=%s", rec.Code, rec.Body)
		}
		return decode[Comment](t, rec)
	}
	v1 := "# Group\n\nAlpha one.\n\nBeta one.\n\nStable one.\n"
	v2 := "# Group\n\nAlpha two.\n\nBeta two.\n\nOther later.\n"
	// Linked comments restore both of their sections, while unrelated later
	// content remains untouched.
	expected := "# Group\n\nAlpha one.\n\nBeta one.\n\nOther later.\n"
	writeDoc(v1)
	alphaV1 := reply(alpha.ID, "第一轮 Alpha", "Alpha one.", v1)
	reply(beta.ID, "第一轮 Beta", "Beta one.", v1)
	writeDoc(v2)
	reply(alpha.ID, "第二轮 Alpha", "Alpha two.", v2)
	reply(beta.ID, "第二轮 Beta", "Beta two.", v2)

	rec = doJSON(t, app, http.MethodPost, "/api/comments/rollback", rollbackCommentRequest{
		Path: path, ID: alpha.ID, MessageID: alphaV1.Messages[1].ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", rec.Code, rec.Body)
	}
	result := decode[rollbackCommentResponse](t, rec)
	if result.Round != 1 {
		t.Fatalf("round = %d", result.Round)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "group-rollback.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != expected {
		t.Fatalf("rollback content = %q, want %q", string(onDisk), expected)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	comments := decode[commentsResponse](t, rec).Comments
	if len(comments) != 2 {
		t.Fatalf("comments = %+v", comments)
	}
	for _, comment := range comments {
		last := comment.Messages[len(comment.Messages)-1]
		if last.Author != "system" || !strings.Contains(last.Body, "局部回滚") {
			t.Fatalf("linked comment not marked rolled back: %+v", last)
		}
		if comment.Resolved {
			t.Fatal("rollback should keep the review open")
		}
	}
}

func TestDocVersionRetention(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "versions.md"), "# Versions\n\nVersion 0.\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	path := canonical(app, "versions.md")
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "Version 0.", Body: "保留版本",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	comment := decode[Comment](t, rec)
	for i := 1; i <= 31; i++ {
		content := fmt.Sprintf("# Versions\n\nVersion %d.\n", i)
		rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: path, Content: content})
		if rec.Code != http.StatusOK {
			t.Fatalf("save %d status=%d", i, rec.Code)
		}
		rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
			Path: path, ID: comment.ID, Author: "agent", Body: fmt.Sprintf("第 %d 版", i),
			After: fmt.Sprintf("Version %d.", i), Snapshot: content,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("reply %d status=%d body=%s", i, rec.Code, rec.Body)
		}
	}
	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	comments := decode[commentsResponse](t, rec).Comments
	if len(comments) != 1 {
		t.Fatalf("comments = %+v", comments)
	}
	agentMessages := 0
	withVersion := 0
	for _, message := range comments[0].Messages {
		if message.Author != "agent" {
			continue
		}
		agentMessages++
		if message.VersionID != "" {
			withVersion++
		}
	}
	if agentMessages != 31 {
		t.Fatalf("agent messages = %d", agentMessages)
	}
	if withVersion != maxDocVersionsPerPath {
		t.Fatalf("rollback versions = %d, want %d", withVersion, maxDocVersionsPerPath)
	}
	if comments[0].Messages[1].VersionID != "" {
		t.Fatal("oldest snapshot link should have been pruned")
	}
}

func TestOfflineRemoteRootIsRetained(t *testing.T) {
	primary := t.TempDir()
	mustWrite(t, filepath.Join(primary, "a.md"), "# A")
	stateDir := t.TempDir()
	db, err := openStateDB(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	remote := stateRoot{
		Kind: rootKindRemote, Path: "/tmp", Name: "unreachable",
		Host: "127.0.0.1", Port: 1, User: "tester",
	}
	if err := saveRootsDB(db, []stateRoot{remote}); err != nil {
		t.Fatal(err)
	}
	_ = closeStateDB(db)

	app, err := New(Options{
		Root: primary, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	tree := decode[treeResponse](t, doJSON(t, app, http.MethodGet, "/api/tree", nil))
	var offline treeRoot
	for _, item := range tree.Roots {
		if item.Kind == rootKindRemote {
			offline = item
		}
	}
	if offline.ID == "" || offline.Offline == "" {
		t.Fatalf("offline remote root not exposed: %+v", tree.Roots)
	}
	defs := loadRootsFromStateDir(t, stateDir)
	foundRemote := false
	for _, def := range defs {
		if def.Kind == rootKindRemote && def.Host == "127.0.0.1" {
			foundRemote = true
		}
	}
	if !foundRemote {
		t.Fatalf("offline root was not retained in sqlite: %+v", defs)
	}
}

func TestRootLimitKeepsSavedRoots(t *testing.T) {
	root, extraA, extraB, stateDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	open := func(limit int, extras ...string) int {
		t.Helper()
		app, err := New(Options{
			Root: root, ExtraRoots: extras, StateDir: stateDir, WebFS: mapFS(),
			Config: config.Config{Limits: config.Limits{Roots: limit}},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer app.Close()
		return app.RootCount()
	}
	if got := open(0, extraA, extraB); got != 3 {
		t.Fatalf("initial roots = %d, want 3", got)
	}
	if got := open(2); got != 2 {
		t.Fatalf("limited roots = %d, want 2", got)
	}
	if got := open(0); got != 3 {
		t.Fatalf("roots after raising limit = %d, want 3 (saved root lost)", got)
	}
}

func TestSaveWorkspaceDropsLoadedOverflowRoots(t *testing.T) {
	root := t.TempDir()
	app, err := New(Options{Root: root, StateDir: t.TempDir(), WebFS: mapFS()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	resolved, err := resolveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	app.overflowRoots = []stateRoot{{Kind: rootKindLocal, Path: resolved}}
	if err := app.saveWorkspace(); err != nil {
		t.Fatal(err)
	}
	if len(app.overflowRoots) != 0 {
		t.Fatalf("overflow root that is now loaded must be dropped, got %+v", app.overflowRoots)
	}
}
