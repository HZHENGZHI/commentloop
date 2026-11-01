package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestFullDocumentComment(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"
	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: docPath, Scope: "document", Body: "整篇文档的方向没问题。",
	})
	if rec.Code != 200 {
		t.Fatalf("document comment: %d %s", rec.Code, rec.Body.String())
	}
	var comment Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	if comment.Scope != "document" || comment.Quote != "" || comment.Line != 0 {
		t.Fatalf("comment = %+v", comment)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+docPath, nil)
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list commentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Comments) != 1 || list.Comments[0].Scope != "document" {
		t.Fatalf("comments = %+v", list.Comments)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: docPath, Body: "没有引用。",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline without quote = %d", rec.Code)
	}
}
