package server

import (
	"net/http"
	"path/filepath"
	"testing"
)

func newAgentTestApp(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n\nquoted text\n")
	mustWrite(t, filepath.Join(root, "other.md"), "# Other\n")
	app, err := New(Options{
		Root: root, StateDir: t.TempDir(), WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, canonical(app, "doc.md"), canonical(app, "other.md")
}

func createUserComment(t *testing.T, app *Server, path string) Comment {
	t.Helper()
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "quoted text", Body: "please change",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create comment status=%d body=%s", rec.Code, rec.Body)
	}
	return decode[Comment](t, rec)
}

func tokenHeaders(token string) map[string]string {
	return map[string]string{"X-Commentloop-Agent-Token": token, "X-Commentloop-Agent": "1"}
}
