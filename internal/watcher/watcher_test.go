package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemovedDirectoryIsReported(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "notes")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := make(chan Event, 16)
	w, err := New(root, func(ev Event) { events <- ev })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := os.Rename(dir, filepath.Join(root, "archive")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Rel == "notes" && ev.IsDir && ev.Removed {
				return
			}
		case <-deadline:
			t.Fatal("no removal event for renamed directory")
		}
	}
}
