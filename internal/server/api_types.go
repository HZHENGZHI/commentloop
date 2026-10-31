package server

import (
	"bytes"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
)

// Shared HTTP API types: localized errors and request logging.

// apiError is a client-facing API error. Code is a stable identifier that
// clients branch on and must not change once shipped; ZH and EN are display
// text. Detail errors are followed by a runtime detail, e.g. "保存设置失败：" + err.
// Templates with %d/%s verbs (same order in ZH and EN) are matched as patterns.
type apiError struct {
	Code   string
	ZH     string
	EN     string
	detail bool
	zhRe   *regexp.Regexp
	enRe   *regexp.Regexp
}

type apiErrorPrefix struct {
	prefix string
	entry  *apiError
}

// apiErrorWriter buffers only error responses so the JSON `error` field can be
// localized without touching every handler. Successful responses are streamed
// through unchanged.
type apiErrorWriter struct {
	http.ResponseWriter
	status  int
	lang    string
	capture bool
	body    bytes.Buffer
}

// hijackTracker records whether the handler took over the connection, which
// is how a successful WebSocket upgrade shows up: the 101 is written to the
// raw conn, so Gin's writer still reports 200.
type hijackTracker struct {
	gin.ResponseWriter
	hijacked bool
}
