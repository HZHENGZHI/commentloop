// Package logging configures the process-wide slog logger.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Setup installs a slog logger writing to w as the default logger. The
// standard library log package is routed through it as well.
// level is debug, info, warn or error; format is text or json.
func Setup(w io.Writer, level, format string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil {
		return nil, fmt.Errorf("invalid log level %q (use debug, info, warn or error)", level)
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		handler = slog.NewTextHandler(w, opts)
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("invalid log format %q (use text or json)", format)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}
