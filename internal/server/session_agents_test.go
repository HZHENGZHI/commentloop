package server

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	mustWrite(t, path, strings.Join([]string{
		`{"timestamp":"2026-10-24T10:00:00Z","type":"session_meta","payload":{"id":"cx1","cwd":"/work/demo"}}`,
		`{"timestamp":"2026-10-24T10:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>noise</environment_context>"}]}}`,
		`{"timestamp":"2026-10-24T10:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"只使用字段 Z"}]}}`,
		`{"timestamp":"2026-10-24T10:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"好的，字段 Z 是最终方案。"}]}}`,
	}, "\n")+"\n")
	parsed, err := parseCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "只使用字段 Z" || parsed.Cwd != "/work/demo" || len(parsed.Messages) != 2 {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.Messages[0].Role != "user" || parsed.Messages[1].Role != "assistant" {
		t.Fatalf("messages = %+v", parsed.Messages)
	}
}

func TestParseCursorSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.jsonl")
	mustWrite(t, path, strings.Join([]string{
		`{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>now</timestamp>\n<user_query>\n把标题改成中文\n</user_query>"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"已修改标题。"},{"type":"tool_use","name":"Edit"}]}}`,
	}, "\n")+"\n")
	parsed, err := parseCursorSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "把标题改成中文" || len(parsed.Messages) != 2 || parsed.Messages[0].Body != "把标题改成中文" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseClaudeSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.jsonl")
	mustWrite(t, path, strings.Join([]string{
		`{"timestamp":"2026-10-24T10:00:00Z","type":"user","sessionId":"cc1","cwd":"/work","message":{"role":"user","content":[{"type":"text","text":"不要改字段 Q"}]}}`,
		`{"timestamp":"2026-10-24T10:00:01Z","type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"明白了。"}]}}`,
	}, "\n")+"\n")
	parsed, err := parseClaudeSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "不要改字段 Q" || parsed.Cwd != "/work" || len(parsed.Messages) != 2 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestAvailableAgentsFromSessionDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "doc.md"), "# Doc\n")
	codexHome := t.TempDir()
	dir := filepath.Join(codexHome, "sessions", "2026", "10", "24")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "rollout-1.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-10-24T10:00:00Z","type":"session_meta","payload":{"id":"cx1","cwd":"/work"}}`,
		`{"timestamp":"2026-10-24T10:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Codex 讨论上下文"}]}}`,
	}, "\n")+"\n")
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	app, err := New(Options{Root: root, StateDir: t.TempDir(), WebFS: mapFS(), Version: "test", AllowRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	sessions, agents, err := app.availableAgentSessions("rfake/doc.md")
	if err != nil {
		t.Fatal(err)
	}
	var codexSession bool
	for _, session := range sessions {
		if session.Agent == "codex" && session.Title == "Codex 讨论上下文" && session.SourceKind == "codex" {
			codexSession = true
		}
	}
	if !codexSession {
		t.Fatalf("codex session missing: %+v", sessions)
	}
	var found bool
	for _, agent := range agents {
		if agent.ID == "codex" && agent.Count == 1 && agent.Installed {
			found = true
		}
	}
	if !found {
		t.Fatalf("agents = %+v", agents)
	}
}
