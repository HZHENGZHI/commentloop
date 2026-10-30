package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterUnknownAPI(t *testing.T) {
	app, _ := newTestApp(t)
	rec := doJSON(t, app, "GET", "/api/nope", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "接口不存在") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP header")
	}
}

func TestRouterMethodNotAllowed(t *testing.T) {
	app, _ := newTestApp(t)
	rec := doJSON(t, app, "PATCH", "/api/doc", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestRouterForbiddenHost(t *testing.T) {
	app, _ := newTestApp(t)
	app.allowAny = false
	req := httptest.NewRequest("GET", "http://evil.example/api/health", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestRouterHealthAndStatic(t *testing.T) {
	app, _ := newTestApp(t)
	if rec := doJSON(t, app, "GET", "/api/health", nil); rec.Code != http.StatusOK {
		t.Fatalf("health: got %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1/", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("static: got 404 %q", rec.Body.String())
	}
}
