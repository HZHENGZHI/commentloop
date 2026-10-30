package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureRequestLogs(t *testing.T) *syncBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	buf := &syncBuffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf
}

func TestRequestLoggerUpgradeHeadersWithoutUpgrade(t *testing.T) {
	app, _ := newTestApp(t)
	logs := captureRequestLogs(t)
	doJSONHeaders(t, app, "GET", "/api/health", nil, map[string]string{
		"Connection": "Upgrade",
		"Upgrade":    "websocket",
	})
	if out := logs.String(); !strings.Contains(out, "path=/api/health status=200") {
		t.Fatalf("unexpected log %q", out)
	}
}

func TestRequestLoggerWebSocketUpgrade(t *testing.T) {
	app, _ := newTestApp(t)
	logs := captureRequestLogs(t)
	hs := httptest.NewServer(app)
	t.Cleanup(hs.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(hs.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "path=/ws status=101") {
		if time.Now().After(deadline) {
			t.Fatalf("no 101 log line in %q", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
