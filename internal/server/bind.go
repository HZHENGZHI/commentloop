package server

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin/binding"
)

const defaultJSONBodyLimit = 64 << 10

// readJSON caps the request body at limit bytes and decodes it with Gin's JSON
// binding, which also runs `binding` struct-tag validation.
func readJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return binding.JSON.Bind(r, dst)
}

// bindJSON is readJSON plus the standard error response; it reports whether
// the handler should continue.
func bindJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	err := readJSON(w, r, limit, dst)
	if err == nil {
		return true
	}
	if isBodyTooLarge(err) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "请求内容过大"})
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
	}
	return false
}

func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
