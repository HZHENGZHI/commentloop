package server

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenStateDBUpgradesLegacySchemaInPlace(t *testing.T) {
	dir := t.TempDir()
	legacy, err := sql.Open("sqlite", filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	// comments as created by an early release, before anchor/group_id/... existed.
	ddl := `CREATE TABLE comments (
		id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		quote TEXT NOT NULL,
		prefix TEXT NOT NULL DEFAULT '',
		suffix TEXT NOT NULL DEFAULT '',
		line INTEGER NOT NULL DEFAULT 0,
		start_offset INTEGER NOT NULL DEFAULT 0,
		end_offset INTEGER NOT NULL DEFAULT 0,
		resolved INTEGER NOT NULL DEFAULT 0,
		orphaned INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	if _, err := legacy.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO comments(id, path, quote, created_at, updated_at) VALUES ('c1', 'a.md', 'q', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	_ = legacy.Close()

	for range 2 {
		gdb, err := openStateDB(dir)
		if err != nil {
			t.Fatal(err)
		}
		db, _ := gdb.DB()
		var tableSQL, groupID, discussionIDs string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'comments'`).Scan(&tableSQL); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(tableSQL, "CREATE TABLE comments (") {
			t.Fatalf("comments table was rebuilt: %s", tableSQL)
		}
		if err := db.QueryRow(`SELECT group_id, discussion_ids FROM comments WHERE id = 'c1'`).Scan(&groupID, &discussionIDs); err != nil {
			t.Fatal(err)
		}
		if groupID != "" || discussionIDs != "[]" {
			t.Fatalf("defaults: group_id=%q discussion_ids=%q", groupID, discussionIDs)
		}
		_ = db.Close()
	}
}

func TestFreshStateDBCascadesDeletes(t *testing.T) {
	gdb, err := openStateDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, _ := gdb.DB()
	defer db.Close()
	for _, q := range []string{
		`INSERT INTO comments(id, path, quote, created_at, updated_at) VALUES ('c1', 'a.md', 'q', 't', 't')`,
		`INSERT INTO comment_messages(id, comment_id, author, body, created_at) VALUES ('m1', 'c1', 'user', 'hi', 't')`,
		`DELETE FROM comments WHERE id = 'c1'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comment_messages`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("cascade: n=%d err=%v", n, err)
	}
	if _, err := db.Exec(`INSERT INTO doc_versions(id, path, hash, content, created_at) VALUES ('v1','a','h','x','t'), ('v2','a','h','y','t')`); err == nil {
		t.Fatal("unique(path, hash) not enforced")
	}
}
