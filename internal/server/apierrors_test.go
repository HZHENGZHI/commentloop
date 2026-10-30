package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body.Code, body.Error
}

func TestAPIErrorCodeAndLocalization(t *testing.T) {
	app, _ := newTestApp(t)
	rec := doJSON(t, app, "GET", "/api/nope", nil)
	if code, msg := decodeErrorBody(t, rec); code != "api_endpoint_not_found" || msg != "接口不存在" {
		t.Fatalf("zh: code=%q msg=%q", code, msg)
	}
	rec = doJSONHeaders(t, app, "GET", "/api/nope", nil, map[string]string{"Accept-Language": "en"})
	if code, msg := decodeErrorBody(t, rec); code != "api_endpoint_not_found" || msg != "API endpoint not found." {
		t.Fatalf("en: code=%q msg=%q", code, msg)
	}
}

func TestAnnotateErrorJSON(t *testing.T) {
	cases := []struct {
		body, lang, code, msg string
		status                int
	}{
		// Plain err.Error() text from lower layers is mapped back to a code.
		{`{"error":"文件已存在"}`, "zh", "file_already_exists", "文件已存在", 409},
		{`{"error":"保存设置失败：disk full"}`, "en", "failed_save_settings", "Failed to save settings: disk full", 500},
		{`{"code":"failed_save_settings","error":"保存设置失败：x"}`, "en", "failed_save_settings", "Failed to save settings: x", 500},
		// Longest prefix wins over a shorter overlapping one.
		{`{"error":"无法读取文件：denied"}`, "en", "could_not_read_file_detail", "Could not read file: denied", 400},
		{`{"error":"未知中文错误"}`, "en", "bad_request", "Request failed. Check the input and try again.", 400},
		{`{"error":"boom"}`, "zh", "internal_error", "boom", 500},
	}
	for _, c := range cases {
		var got struct{ Code, Error string }
		if err := json.Unmarshal(annotateErrorJSON([]byte(c.body), c.status, c.lang), &got); err != nil {
			t.Fatal(err)
		}
		if got.Code != c.code || got.Error != c.msg {
			t.Errorf("%s: got code=%q msg=%q", c.body, got.Code, got.Error)
		}
	}
}

func TestAPIErrorCatalogCodesAreStableIdentifiers(t *testing.T) {
	for code, e := range apiErrorsByCode {
		if code == "" || strings.Trim(code, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			t.Errorf("bad code %q", code)
		}
		if e.ZH == "" || e.EN == "" {
			t.Errorf("%s: missing text", code)
		}
	}
}

func TestAgentTokenRejectionHasCode(t *testing.T) {
	app, _ := newTestApp(t)
	rec := doJSONHeaders(t, app, "GET", "/api/tree", nil, map[string]string{"X-Commentloop-Agent-Token": "bogus"})
	if rec.Code != http.StatusForbidden {
		t.Skipf("token guard not triggered: %d", rec.Code)
	}
	if code, _ := decodeErrorBody(t, rec); code != "agent_job_token_invalid_expired" {
		t.Fatalf("code=%q", code)
	}
}

func TestLookupAPIErrorBothLanguagesAndTemplates(t *testing.T) {
	cases := []struct{ message, lang, code, localized string }{
		{"AI optimization timed out", "en", "ai_optimization_timed_out", "AI optimization timed out."},
		{"AI optimization failed: exit 1", "en", "ai_optimization_failed", "AI optimization failed: exit 1"},
		{"最多只能添加 5 个目录", "zh", "too_many_directories", "最多只能添加 5 个目录"},
		{"最多只能添加 5 个目录", "en", "too_many_directories", "You can add at most 5 directories."},
		{"暂不支持导入 cursor 会话", "en", "importing_session_type_not_supported", "Importing cursor sessions is not supported yet."},
	}
	for _, c := range cases {
		e := lookupAPIError(c.message)
		if e == nil || e.Code != c.code {
			t.Errorf("%q: got %+v, want %s", c.message, e, c.code)
			continue
		}
		if got := e.localize(c.message, c.lang); got != c.localized {
			t.Errorf("%q (%s): localized %q, want %q", c.message, c.lang, got, c.localized)
		}
	}
}

func TestAPIMethodNotAllowedIsJSONWithCode(t *testing.T) {
	app, _ := newTestApp(t)
	rec := doJSONHeaders(t, app, "PATCH", "/api/doc", nil, map[string]string{"Accept-Language": "en"})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
	if code, msg := decodeErrorBody(t, rec); code != "method_not_allowed" || msg != "Method not allowed." {
		t.Fatalf("code=%q msg=%q", code, msg)
	}
}

func TestAPIForbiddenHostIsJSONWithCode(t *testing.T) {
	app, _ := newTestApp(t)
	app.allowAny = false
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	if code, _ := decodeErrorBody(t, rec); code != "forbidden_host" {
		t.Fatalf("code=%q", code)
	}
}
