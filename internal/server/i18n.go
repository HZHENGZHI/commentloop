package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

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

func (w *apiErrorWriter) WriteHeader(status int) {
	w.status = status
	if status >= 400 {
		w.capture = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiErrorWriter) Write(p []byte) (int, error) {
	if w.capture {
		return w.body.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *apiErrorWriter) finish() {
	if !w.capture {
		return
	}
	body := annotateErrorJSON(w.body.Bytes(), w.status, w.lang)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(body)
}

// annotateErrorJSON guarantees every JSON error body carries a stable `code`
// and, for English clients, an English `error` message.
func annotateErrorJSON(body []byte, status int, lang string) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	message, _ := payload["error"].(string)
	code, _ := payload["code"].(string)
	var entry *apiError
	if code != "" {
		entry = apiErrorsByCode[code]
	}
	if entry == nil && message != "" {
		entry = lookupAPIError(message)
	}
	switch {
	case entry != nil:
		payload["code"] = entry.Code
		if message != "" {
			payload["error"] = entry.localize(message, lang)
		}
	case code == "":
		payload["code"] = fallbackErrorCode(status)
	}
	if entry == nil && lang == "en" && message != "" {
		payload["error"] = localizeErrorMessage(message, status)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func localizeErrorMessage(message string, status int) string {
	if e := lookupAPIError(message); e != nil {
		return e.localize(message, "en")
	}
	if strings.IndexFunc(message, func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff }) >= 0 {
		switch status {
		case http.StatusBadRequest:
			return "Request failed. Check the input and try again."
		case http.StatusForbidden:
			return "This action is not allowed."
		case http.StatusNotFound:
			return "The requested resource was not found."
		default:
			return "Request failed."
		}
	}
	return message
}
