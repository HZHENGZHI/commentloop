package main

import (
	"flag"
	"io"
	"testing"
)

func TestReorderArgs(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addDir := fs.String("add-dir", "", "")
	noOpen := fs.Bool("no-open", false, "")
	addr := fs.String("addr", "127.0.0.1:4571", "")
	var calls int
	fs.Func("tag", "", func(string) error { calls++; return nil })

	args := reorderArgs(fs, []string{
		"/notes", "--add-dir", "/extra", "--no-open",
		"--addr=127.0.0.1:8080", "--tag", "a", "--tag", "b", "file.md",
	})
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *addDir != "/extra" || !*noOpen || *addr != "127.0.0.1:8080" {
		t.Fatalf("flags: add=%q open=%v addr=%q", *addDir, *noOpen, *addr)
	}
	if calls != 2 {
		t.Fatalf("repeated flag calls = %d, want 2", calls)
	}
	got := fs.Args()
	if len(got) != 2 || got[0] != "/notes" || got[1] != "file.md" {
		t.Fatalf("positionals = %v", got)
	}
}

func TestReorderArgsTerminator(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_ = fs.Bool("no-open", false, "")
	args := reorderArgs(fs, []string{"--no-open", "--", "--not-a-flag", "-file.md"})
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	got := fs.Args()
	if len(got) != 2 || got[0] != "--not-a-flag" || got[1] != "-file.md" {
		t.Fatalf("positionals = %v", got)
	}
}
