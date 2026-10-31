package server

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *hijackTracker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := h.ResponseWriter.Hijack()
	if err == nil {
		h.hijacked = true
	}
	return conn, rw, err
}

func requestLogger(c *gin.Context) {
	start := time.Now()
	tracker := &hijackTracker{ResponseWriter: c.Writer}
	c.Writer = tracker
	c.Next()
	status := c.Writer.Status()
	if tracker.hijacked && status == http.StatusOK {
		status = http.StatusSwitchingProtocols
	}
	logRequest(c.Request, status, int64(max(c.Writer.Size(), 0)), time.Since(start))
}

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

func logRequest(r *http.Request, status int, bytes int64, elapsed time.Duration) {
	level := requestLogLevel(r.Method, r.URL.Path, status)
	ctx := r.Context()
	if !slog.Default().Enabled(ctx, level) {
		return
	}
	slog.Default().LogAttrs(ctx, level, "http request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Int64("bytes", bytes),
		slog.Duration("duration", elapsed),
	)
}
