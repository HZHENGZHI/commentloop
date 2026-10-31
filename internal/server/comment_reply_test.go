package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestManagedAgentCannotSpoofUserReply(t *testing.T) {
	app, path, _ := newAgentTestApp(t)
	comment := createUserComment(t, app, path)
	app.agentCaps.setToken("reply-token", agentGrant{
		Capabilities: agentCapabilities{WriteDocs: false, ReadCode: false, ReplyComments: false},
		DocPath:      path, JobID: "job-reply",
	})

	rec := doJSONHeaders(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "spoofed",
	}, tokenHeaders("reply-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed user reply status=%d body=%s", rec.Code, rec.Body)
	}

	app.agentCaps.setToken("reply-token", agentGrant{
		Capabilities: agentCapabilities{WriteDocs: false, ReadCode: false, ReplyComments: true},
		DocPath:      path, JobID: "job-reply",
	})
	rec = doJSONHeaders(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "still the agent",
	}, tokenHeaders("reply-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("agent reply status=%d body=%s", rec.Code, rec.Body)
	}
	updated := decode[Comment](t, rec)
	last := updated.Messages[len(updated.Messages)-1]
	if last.Author != "agent" {
		t.Fatalf("managed agent reply author = %q, want agent", last.Author)
	}
}

func TestFailedReplyKeepsResolvedState(t *testing.T) {
	app, path, _ := newAgentTestApp(t)
	comment := createUserComment(t, app, path)
	rec := doJSON(t, app, http.MethodPost, "/api/comments/resolve", resolveCommentRequest{
		Path: path, ID: comment.ID, Resolved: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", rec.Code, rec.Body)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "   ",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty reply status=%d body=%s", rec.Code, rec.Body)
	}
	rec = doJSON(t, app, http.MethodGet, "/api/comments?path="+url.QueryEscape(path), nil)
	comments := decode[commentsResponse](t, rec).Comments
	if len(comments) != 1 || !comments[0].Resolved {
		t.Fatalf("failed reply reopened the thread: %+v", comments)
	}

	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "one more change",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("valid reply status=%d body=%s", rec.Code, rec.Body)
	}
	if updated := decode[Comment](t, rec); updated.Resolved {
		t.Fatalf("user reply did not reopen the thread: %+v", updated)
	}
}

func TestReplyUpdatesReviewContext(t *testing.T) {
	app, path, _ := newAgentTestApp(t)
	seed := func(source string) Discussion {
		parsed := parsedSession{
			Title: "session " + source, CreatedAt: time.Now(),
			Messages: []sessionMessage{{Role: "user", Body: "context"}},
		}
		item, err := app.discussions.importSession(path, parsed, source, "pi", "pi")
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	first := seed("/tmp/commentloop-first.jsonl")
	second := seed("/tmp/commentloop-second.jsonl")

	rec := doJSON(t, app, http.MethodPost, "/api/comments", createCommentRequest{
		Path: path, Quote: "quoted text", Body: "please change",
		DiscussionIDs: []string{first.ID}, PromptTemplateID: "builtin-requirements",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	comment := decode[Comment](t, rec)

	// An MCP/CLI reply omits the context fields; they must be preserved.
	app.agentCaps.setToken("ctx-token", agentGrant{
		Capabilities: agentCapabilities{ReplyComments: true}, DocPath: path, JobID: "job-ctx",
	})
	rec = doJSONHeaders(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "agent", Body: "edited",
	}, tokenHeaders("ctx-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("agent reply status=%d body=%s", rec.Code, rec.Body)
	}
	if updated := decode[Comment](t, rec); len(updated.DiscussionIDs) != 1 ||
		updated.DiscussionIDs[0] != first.ID || updated.PromptTemplateID != "builtin-requirements" {
		t.Fatalf("omitted reply context cleared the thread: %+v", updated)
	}

	// A user reply can pick new discussions and a different task template.
	rec = doJSON(t, app, http.MethodPost, "/api/comments/reply", replyCommentRequest{
		Path: path, ID: comment.ID, Author: "user", Body: "try another angle",
		DiscussionIDs: []string{second.ID}, PromptTemplateID: "builtin-bug-triage",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("user reply status=%d body=%s", rec.Code, rec.Body)
	}
	updated := decode[Comment](t, rec)
	if len(updated.DiscussionIDs) != 1 || updated.DiscussionIDs[0] != second.ID ||
		updated.PromptTemplateID != "builtin-bug-triage" {
		t.Fatalf("reply context was not persisted: %+v", updated)
	}
}
