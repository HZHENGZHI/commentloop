package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestSetupJSONRespectsLevel(t *testing.T) {
	defer slog.SetDefault(slog.Default())
	var buf bytes.Buffer
	if _, err := Setup(&buf, "warn", "json"); err != nil {
		t.Fatal(err)
	}
	slog.Info("hidden")
	slog.Warn("shown", "k", "v")
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("want one JSON record, got %q: %v", buf.String(), err)
	}
	if rec["msg"] != "shown" || rec["k"] != "v" {
		t.Fatalf("unexpected record %v", rec)
	}
}

func TestSetupRejectsInvalidValues(t *testing.T) {
	if _, err := Setup(&bytes.Buffer{}, "loud", "text"); err == nil {
		t.Fatal("expected invalid level error")
	}
	if _, err := Setup(&bytes.Buffer{}, "info", "xml"); err == nil {
		t.Fatal("expected invalid format error")
	}
}
