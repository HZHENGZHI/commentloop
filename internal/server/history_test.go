package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestDocVersionHistoryAndRestore(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/history.md"

	rec := doJSON(t, app, http.MethodPost, "/api/doc", map[string]any{
		"path": docPath, "content": "# One\n",
	})
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, app, http.MethodPut, "/api/doc", map[string]any{
		"path": docPath, "content": "# Two\n",
	})
	if rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(docPath), nil)
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Versions []docVersionInfo `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Versions) != 2 {
		t.Fatalf("versions = %+v", list.Versions)
	}
	oldest := list.Versions[len(list.Versions)-1]

	rec = doJSON(t, app, http.MethodGet, "/api/doc/version?path="+url.QueryEscape(docPath)+"&id="+url.QueryEscape(oldest.ID), nil)
	if rec.Code != 200 {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var version struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if version.Content != "# One\n" {
		t.Fatalf("oldest content = %q", version.Content)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc/restore", map[string]any{
		"path": docPath, "id": oldest.ID,
	})
	if rec.Code != 200 {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	doc, err := app.snapshot(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.content, "# One") {
		t.Fatalf("restored content = %q", doc.content)
	}
}
