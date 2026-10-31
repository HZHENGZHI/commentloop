package server

import "testing"

func TestDeleteCommentDropsOnlyItsSnapshots(t *testing.T) {
	db, err := openStateDB("")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStateDB(db)
	store := newCommentStore(db)
	content := "Alpha beta.\n\nGamma delta.\n"
	first, err := store.createAs("user", "a.md", "Alpha", "", "", "fix", "", content, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.createAs("user", "a.md", "Gamma", "", "", "fix", "", content, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	firstReply, err := store.reply("a.md", first.ID, "agent", "done", "", "Alpha2", "", "one", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	secondReply, err := store.reply("a.md", second.ID, "agent", "done", "", "Gamma2", "", "two", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	firstVersion := firstReply.Messages[1].VersionID
	secondVersion := secondReply.Messages[1].VersionID
	if firstVersion == "" || secondVersion == "" || firstVersion == secondVersion {
		t.Fatalf("versions = %q, %q", firstVersion, secondVersion)
	}
	if err := store.delete("a.md", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.version(firstVersion); ok {
		t.Fatal("snapshot of the deleted comment should be dropped")
	}
	if version, ok := store.version(secondVersion); !ok || version.Content != "two" {
		t.Fatalf("shared snapshot = %+v, %v", version, ok)
	}
	if _, ok := store.getByID(first.ID); ok {
		t.Fatal("deleted comment still present")
	}
	var messages int64
	if err := db.Model(&commentMessageRow{}).Where("comment_id = ?", first.ID).Count(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if messages != 0 {
		t.Fatalf("messages of deleted comment = %d, want cascade delete", messages)
	}
	if err := store.delete("a.md", first.ID); err == nil {
		t.Fatal("second delete should report not found")
	}
}
