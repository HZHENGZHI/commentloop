package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

type checklistResponse struct {
	Items     []checklistItem     `json:"items"`
	Playbooks []checklistPlaybook `json:"playbooks"`
}

func TestReviewChecklistPlaybook(t *testing.T) {
	app, _ := newTestApp(t)
	rootID := app.rootsSnapshot()[0].ID
	docPath := rootID + "/hello.md"

	rec := doJSON(t, app, http.MethodPost, "/api/doc/checklist/apply", applyChecklistRequest{
		Path: docPath, Playbook: "prd",
	})
	if rec.Code != 200 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	var checklist checklistResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &checklist); err != nil {
		t.Fatal(err)
	}
	if len(checklist.Items) != 5 || len(checklist.Playbooks) == 0 {
		t.Fatalf("checklist = %+v", checklist)
	}
	first := checklist.Items[0]
	rec = doJSON(t, app, http.MethodPatch, "/api/doc/checklist/item", toggleChecklistRequest{
		Path: docPath, ID: first.ID, Done: true,
	})
	if rec.Code != 200 {
		t.Fatalf("toggle: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &checklist); err != nil {
		t.Fatal(err)
	}
	if !checklist.Items[0].Done {
		t.Fatal("item was not checked")
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc/checklist/apply", applyChecklistRequest{
		Path: docPath, Playbook: "prd",
	})
	if err := json.Unmarshal(rec.Body.Bytes(), &checklist); err != nil {
		t.Fatal(err)
	}
	if len(checklist.Items) != 5 {
		t.Fatalf("duplicate apply added items: %+v", checklist.Items)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/doc/checklist/item", addChecklistRequest{
		Path: docPath, Title: "补充发布回滚步骤",
	})
	if rec.Code != 200 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &checklist); err != nil {
		t.Fatal(err)
	}
	if len(checklist.Items) != 6 {
		t.Fatalf("custom item missing: %+v", checklist.Items)
	}
	custom := checklist.Items[5]
	rec = doJSON(t, app, http.MethodDelete, "/api/doc/checklist/item?path="+url.QueryEscape(docPath)+"&id="+url.QueryEscape(custom.ID), nil)
	if rec.Code != 200 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &checklist); err != nil {
		t.Fatal(err)
	}
	if len(checklist.Items) != 5 {
		t.Fatalf("custom item not deleted: %+v", checklist.Items)
	}
}
