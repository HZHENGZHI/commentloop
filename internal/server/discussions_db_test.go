package server

import (
	"fmt"
	"testing"
	"time"
)

func TestDiscussionStoreRoundTrip(t *testing.T) {
	db, err := openStateDB("")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStateDB(db)
	store := newDiscussionStore(db)
	session := func(n int, prefix string) parsedSession {
		parsed := parsedSession{Title: "t", CreatedAt: time.Now()}
		for i := 0; i < n; i++ {
			parsed.Messages = append(parsed.Messages, sessionMessage{Role: "user", Body: fmt.Sprintf("%s%d", prefix, i)})
		}
		return parsed
	}
	// More messages than one INSERT batch.
	item, err := store.importSession("a.md", session(discussionMessageBatch+7, "m"), "/s/one.jsonl", "pi", "pi")
	if err != nil {
		t.Fatal(err)
	}
	messages, err := store.messages(item.ID)
	if err != nil || len(messages) != discussionMessageBatch+7 {
		t.Fatalf("messages = %d, %v", len(messages), err)
	}
	if messages[0].Body != "m0" || messages[len(messages)-1].Seq != discussionMessageBatch+6 {
		t.Fatalf("order = %+v .. %+v", messages[0], messages[len(messages)-1])
	}

	again, err := store.importSession("a.md", session(2, "n"), "/s/one.jsonl", "pi", "pi")
	if err != nil || again.ID != item.ID || again.MessageCount != 2 {
		t.Fatalf("reimport = %+v, %v", again, err)
	}
	if messages, _ = store.messages(item.ID); len(messages) != 2 || messages[1].Body != "n1" {
		t.Fatalf("reimported messages = %+v", messages)
	}
	if sources, err := store.importedSources("a.md"); err != nil || !sources["/s/one.jsonl"] || len(sources) != 1 {
		t.Fatalf("sources = %v, %v", sources, err)
	}
	if !store.hasContext("a.md") || store.hasContext("b.md") {
		t.Fatal("hasContext")
	}

	first, err := store.saveConsensus("b.md", "one", "user")
	if err != nil || first.Version != 1 {
		t.Fatalf("consensus = %+v, %v", first, err)
	}
	second, err := store.saveConsensus("b.md", "two", "agent")
	if err != nil || second.Version != 2 {
		t.Fatalf("consensus = %+v, %v", second, err)
	}
	if got, ok := store.consensus("b.md"); !ok || got.Content != "two" || got.Source != "agent" || got.Version != 2 {
		t.Fatalf("stored consensus = %+v", got)
	}
	if err := store.clearConsensus("b.md"); err != nil || store.hasContext("b.md") {
		t.Fatalf("clear: %v", err)
	}

	if err := store.delete("a.md", item.ID); err != nil {
		t.Fatal(err)
	}
	if messages, _ = store.messages(item.ID); len(messages) != 0 {
		t.Fatalf("messages not cascaded: %d", len(messages))
	}
	if err := store.delete("a.md", item.ID); err == nil {
		t.Fatal("second delete should report not found")
	}
}
