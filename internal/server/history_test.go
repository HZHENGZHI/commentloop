package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
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
	// The restored revision is touched to the top of the timeline rather than
	// reusing the old timestamp.
	rec = doJSON(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(docPath), nil)
	if rec.Code != 200 {
		t.Fatalf("list after restore: %d %s", rec.Code, rec.Body.String())
	}
	list = struct {
		Versions []docVersionInfo `json:"versions"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Versions) == 0 || list.Versions[0].ID != oldest.ID {
		t.Fatalf("restored version not on top: %+v (oldest %s)", list.Versions, oldest.ID)
	}
}

func TestEmptyDocumentVersionAndAgentScope(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/empty.md"
	rec := doJSON(t, app, http.MethodPost, "/api/doc", map[string]any{
		"path": docPath, "content": "",
	})
	if rec.Code != 200 {
		t.Fatalf("create empty: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(docPath), nil)
	if rec.Code != 200 {
		t.Fatalf("list empty: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Versions []docVersionInfo `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Versions) != 1 || list.Versions[0].Size != 0 {
		t.Fatalf("empty versions = %+v", list.Versions)
	}

	other := rootID + "/hello.md"
	app.agentCaps.setToken("agent-token", agentGrant{
		DocPath: docPath, Capabilities: agentCapabilities{ReadCode: true},
	})
	rec = doJSONHeaders(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(other), nil,
		map[string]string{"X-Commentloop-Agent-Token": "agent-token"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unrelated history = %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeletedDocumentHistoryIsPurged(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/deleted-history.md"

	if rec := doJSON(t, app, http.MethodPost, "/api/doc", map[string]any{
		"path": docPath, "content": "# Secret v1\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, app, http.MethodPut, "/api/doc", map[string]any{
		"path": docPath, "content": "# Secret v2\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	rec := doJSON(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(docPath), nil)
	var list struct {
		Versions []docVersionInfo `json:"versions"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Versions) != 2 {
		t.Fatalf("versions = %+v, %v", list.Versions, err)
	}
	oldest := list.Versions[len(list.Versions)-1].ID

	if rec := doJSON(t, app, http.MethodDelete, "/api/doc?path="+url.QueryEscape(docPath), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(docPath), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted versions status = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, app, http.MethodPost, "/api/doc", map[string]any{
		"path": docPath, "content": "# New document\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("recreate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, app, http.MethodPost, "/api/doc/restore", map[string]any{
		"path": docPath, "id": oldest,
	}); rec.Code != http.StatusNotFound {
		t.Fatalf("stale restore status = %d %s", rec.Code, rec.Body.String())
	}
	snapshot, err := app.snapshot(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.content != "# New document\n" {
		t.Fatalf("recreated content changed: %q", snapshot.content)
	}
}

func TestRestoreSnapshotsExternalContent(t *testing.T) {
	app, root := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/external-history.md"

	if rec := doJSON(t, app, http.MethodPost, "/api/doc", map[string]any{
		"path": docPath, "content": "# API v1\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, app, http.MethodPut, "/api/doc", map[string]any{
		"path": docPath, "content": "# API v2\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	rec := doJSON(t, app, http.MethodGet, "/api/doc/versions?path="+url.QueryEscape(docPath), nil)
	var list struct {
		Versions []docVersionInfo `json:"versions"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Versions) != 2 {
		t.Fatalf("versions = %+v, %v", list.Versions, err)
	}
	oldest := list.Versions[len(list.Versions)-1].ID

	// An editor changed the file outside the API; the watcher does not create a
	// version. Restoring must snapshot it before overwriting.
	mustWrite(t, filepath.Join(root, "external-history.md"), "# External work\n")
	if rec := doJSON(t, app, http.MethodPost, "/api/doc/restore", map[string]any{
		"path": docPath, "id": oldest,
	}); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	versions, err := app.comments.listVersions(docPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range versions {
		version, ok := app.comments.version(item.ID)
		if ok && version.Content == "# External work\n" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("external content was not snapshotted: %+v", versions)
	}
}
