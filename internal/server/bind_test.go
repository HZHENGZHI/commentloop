package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postRaw(app *Server, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "http://example.com"+target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestBindJSONMalformed(t *testing.T) {
	app, _ := newTestApp(t)
	rec := postRaw(app, "/api/agent/runner/heartbeat", "{")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "请求格式错误") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestBindJSONTooLarge(t *testing.T) {
	app, _ := newTestApp(t)
	body := `{"id":"` + strings.Repeat("x", defaultJSONBodyLimit) + `"}`
	rec := postRaw(app, "/api/agent/runner/heartbeat", body)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "请求内容过大") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestBindJSONTooLargeEnglish(t *testing.T) {
	app, _ := newTestApp(t)
	body := `{"id":"` + strings.Repeat("x", defaultJSONBodyLimit) + `"}`
	req := httptest.NewRequest("POST", "http://example.com/api/agent/runner/heartbeat", strings.NewReader(body))
	req.Header.Set("Accept-Language", "en")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
