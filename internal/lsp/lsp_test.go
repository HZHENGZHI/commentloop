package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestParseLocations(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	c := &client{root: root}
	raw := json.RawMessage(`[
		{"uri":"file:///repo/internal/a.go","range":{"start":{"line":9,"character":4},"end":{"line":9,"character":10}}},
		{"targetUri":"file:///repo/b.go","targetSelectionRange":{"start":{"line":2,"character":1},"end":{"line":2,"character":5}}},
		{"targetUri":"file:///outside/c.go","targetSelectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}
	]`)
	locations, err := c.parseLocations(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 3 {
		t.Fatalf("locations = %+v", locations)
	}
	if locations[0].Path != "internal/a.go" || locations[0].Line != 10 || locations[0].Character != 5 {
		t.Fatalf("location 0 = %+v", locations[0])
	}
	if locations[1].Path != "b.go" || locations[1].Line != 3 {
		t.Fatalf("location 1 = %+v", locations[1])
	}
	if !locations[2].External {
		t.Fatalf("outside location should be external: %+v", locations[2])
	}
}

func TestMatchServer(t *testing.T) {
	configs := DefaultServers()
	for file, want := range map[string]string{
		"main.go": "go", "app.ts": "typescript", "app.tsx": "typescriptreact",
		"script.js": "javascript", "view.jsx": "javascriptreact",
		"tool.py": "python", "lib.rs": "rust",
	} {
		spec, err := MatchServer(configs, file)
		if err != nil {
			t.Errorf("MatchServer(%s): %v", file, err)
			continue
		}
		if spec.ID != want {
			t.Errorf("MatchServer(%s) = %s, want %s", file, spec.ID, want)
		}
	}
	if _, err := MatchServer(configs, "README.unknown"); err == nil {
		t.Fatal("unsupported extension should fail")
	}
}
