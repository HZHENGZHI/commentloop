package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestFullTextSearch(t *testing.T) {
	app, _ := newTestApp(t)
	rec := doJSON(t, app, http.MethodGet, "/api/search?q="+url.QueryEscape("done"), nil)
	if rec.Code != 200 {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Results []searchResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) == 0 {
		t.Fatal("expected a result for done")
	}
	if response.Results[0].Path == "" || response.Results[0].Line < 1 {
		t.Fatalf("result = %+v", response.Results[0])
	}
	rec = doJSON(t, app, http.MethodGet, "/api/search?q="+url.QueryEscape("does-not-exist-xyz"), nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("unexpected results: %+v", response.Results)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/search?q=", nil)
	if rec.Code != 200 {
		t.Fatalf("empty search: %d", rec.Code)
	}
}
