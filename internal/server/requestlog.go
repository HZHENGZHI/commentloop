package server

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// statusRecorder captures the response status and size for request logging
// while still supporting WebSocket upgrades and streaming.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	r.status = http.StatusSwitchingProtocols
	r.wroteHeader = true
	return h.Hijack()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// requestLogLevel keeps routine reads at debug so the default info level only
// shows mutations, client errors and server errors.
func requestLogLevel(method, path string, status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	case strings.HasPrefix(path, "/api/") && method != http.MethodGet && method != http.MethodHead:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

func logRequest(r *http.Request, rec *statusRecorder, elapsed time.Duration) {
	level := requestLogLevel(r.Method, r.URL.Path, rec.status)
	ctx := r.Context()
	if !slog.Default().Enabled(ctx, level) {
		return
	}
	slog.Default().LogAttrs(ctx, level, "http request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", rec.status),
		slog.Int64("bytes", rec.bytes),
		slog.Duration("duration", elapsed),
	)
}
