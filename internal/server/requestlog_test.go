package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestLogLevel(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
		want         slog.Level
	}{
		{"GET", "/api/tree", 200, slog.LevelDebug},
		{"GET", "/assets/x.js", 200, slog.LevelDebug},
		{"POST", "/api/comments", 200, slog.LevelInfo},
		{"GET", "/api/doc", 404, slog.LevelWarn},
		{"POST", "/api/render", 500, slog.LevelError},
	}
	for _, c := range cases {
		if got := requestLogLevel(c.method, c.path, c.status); got != c.want {
			t.Errorf("%s %s %d: got %v want %v", c.method, c.path, c.status, got, c.want)
		}
	}
}

func TestLogRequestRecordsStatus(t *testing.T) {
	defer slog.SetDefault(slog.Default())
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	logRequest(httptest.NewRequest("GET", "/api/doc?path=a.md", nil), http.StatusNotFound, 5, time.Millisecond)

	out := buf.String()
	if !strings.Contains(out, "status=404") || !strings.Contains(out, "path=/api/doc") {
		t.Fatalf("unexpected log line %q", out)
	}
}
