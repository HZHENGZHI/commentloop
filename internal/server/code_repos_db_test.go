package server

import (
	"os"
	"testing"
)

func TestCodeRepoStoreLinks(t *testing.T) {
	db, err := openStateDB("")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStateDB(db)
	store := newCodeRepoStore(db, "")
	dirB, dirA := t.TempDir(), t.TempDir()
	b, err := store.ensure(dirB)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := store.ensure(dirB); err != nil || again.ID != b.ID {
		t.Fatalf("ensure twice = %+v, %v", again, err)
	}
	a, err := store.ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := store.byPath(a.Path); !ok || got.ID != a.ID || got.Kind != "local" {
		t.Fatalf("byPath = %+v, %v", got, ok)
	}
	for _, id := range []string{b.ID, a.ID, a.ID} {
		if err := store.link("doc.md", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.link("doc.md", "repo_missing"); err != os.ErrNotExist {
		t.Fatalf("link missing = %v", err)
	}
	linked, err := store.linked("doc.md")
	if err != nil || len(linked) != 2 || !linked[0].Linked {
		t.Fatalf("linked = %+v, %v", linked, err)
	}
	all, err := store.list()
	if err != nil || len(all) != 2 || all[0].Linked {
		t.Fatalf("list = %+v, %v", all, err)
	}
	if err := store.unlink("doc.md", a.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.unlink("doc.md", a.ID); err != os.ErrNotExist {
		t.Fatalf("second unlink = %v", err)
	}
	if err := store.delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if linked, _ = store.linked("doc.md"); len(linked) != 0 {
		t.Fatalf("link should cascade with repo delete: %+v", linked)
	}
	if err := store.delete(b.ID); err != os.ErrNotExist {
		t.Fatalf("second delete = %v", err)
	}
}
