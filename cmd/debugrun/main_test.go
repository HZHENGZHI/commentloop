package main

import "testing"

func TestRelevant(t *testing.T) {
	s := &supervisor{}
	cases := map[string]bool{
		"main.go":              true,
		"main_test.go":         false,
		"internal/server/a.go": true,
		"go.mod":               true,
		"go.sum":               true,
		"README.md":            false,
		"web/src/App.tsx":      false,
		".main.go.swp":         false,
	}
	for name, want := range cases {
		if got := s.relevant(name); got != want {
			t.Errorf("relevant(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSkipDir(t *testing.T) {
	for _, name := range []string{"web", "bin", "node_modules", ".git", ".obsidian", "dist"} {
		if !skipDir(name) {
			t.Errorf("skipDir(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"internal", "cmd", "notes"} {
		if skipDir(name) {
			t.Errorf("skipDir(%q) = true, want false", name)
		}
	}
}
