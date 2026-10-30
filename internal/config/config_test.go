package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func noEnv() []string { return nil }

func TestLoadDefaults(t *testing.T) {
	cfg, file, err := Load(LoadOptions{Environ: noEnv, DefaultFile: filepath.Join(t.TempDir(), "missing.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if file != "" || cfg != Default() {
		t.Fatalf("got %+v from %q, want defaults", cfg, file)
	}
}

func TestLoadPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "log:\n  level: warn\n  format: json\nremote:\n  poll_interval: 45s\nlimits:\n  roots: 8\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, file, err := Load(LoadOptions{
		DefaultFile: path,
		Environ: func() []string {
			return []string{"COMMENTLOOP_LOG_LEVEL=error", "COMMENTLOOP_AGENT_JOB_LEASE=5m", "COMMENTLOOP_CONFIG=x", "HOME=/"}
		},
		Overrides: map[string]any{"log.level": "debug"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if file != path {
		t.Fatalf("file = %q", file)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Fatalf("log = %+v (flag > env > file)", cfg.Log)
	}
	if cfg.Remote.PollInterval != 45*time.Second || cfg.Limits.Roots != 8 || cfg.Agent.JobLease != 5*time.Minute {
		t.Fatalf("unexpected cfg %+v", cfg)
	}
	if cfg.Limits.TreeFiles != Default().Limits.TreeFiles {
		t.Fatalf("unset value lost its default: %+v", cfg.Limits)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, _, err := Load(LoadOptions{Environ: noEnv, File: filepath.Join(t.TempDir(), "missing.yaml")}); err == nil {
		t.Fatal("explicit missing file should fail")
	}
	if _, _, err := Load(LoadOptions{Environ: func() []string { return []string{"COMMENTLOOP_REMOTE_POLL_INTERVAL=soon"} }}); err == nil {
		t.Fatal("invalid duration should fail")
	}
}
