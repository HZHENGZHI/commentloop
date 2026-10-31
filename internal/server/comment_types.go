package server

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Comment domain types: threads, messages, doc versions and their API payloads.

type Comment struct {
	ID               string             `json:"id"`
	Path             string             `json:"path"`
	Quote            string             `json:"quote"`
	Anchor           string             `json:"anchor,omitempty"`
	GroupID          string             `json:"groupId,omitempty"`
	Prefix           string             `json:"prefix,omitempty"`
	Suffix           string             `json:"suffix,omitempty"`
	Line             int                `json:"line"`
	Start            int                `json:"start"`
	End              int                `json:"end"`
	Resolved         bool               `json:"resolved"`
	Orphaned         bool               `json:"orphaned"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	DiscussionIDs    []string           `json:"discussionIds,omitempty"`
	PromptTemplateID string             `json:"promptTemplateId,omitempty"`
	Severity         string             `json:"severity,omitempty"`
	Reaction         string             `json:"reaction,omitempty"`
	Suggestion       *CommentSuggestion `json:"suggestion,omitempty"`
	Messages         []CommentMessage   `json:"messages"`
}

type CommentSuggestion struct {
	Quote  string `json:"quote"`
	Text   string `json:"text"`
	Mode   string `json:"mode"`
	Status string `json:"status"`
}

var commentSeverities = map[string]bool{
	"":           true,
	"blocking":   true,
	"should_fix": true,
	"nit":        true,
	"question":   true,
}

func normalizeSeverity(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !commentSeverities[value] {
		return "", fmt.Errorf("无效的评论级别")
	}
	return value, nil
}

type CommentMessage struct {
	ID              string    `json:"id"`
	Author          string    `json:"author"` // user | agent | system
	Body            string    `json:"body"`
	Before          string    `json:"before,omitempty"`
	After           string    `json:"after,omitempty"`
	VersionID       string    `json:"versionId,omitempty"`
	BeforeVersionID string    `json:"beforeVersionId,omitempty"`
	SectionStart    int       `json:"-"`
	SectionEnd      int       `json:"-"`
	CreatedAt       time.Time `json:"createdAt"`
}

type docVersion struct {
	ID        string
	Path      string
	Content   string
	CreatedAt time.Time
}

type docVersionInfo struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
}

type versionTarget struct {
	Comment Comment
	Message CommentMessage
	Round   int
}

type commentStore struct {
	db *gorm.DB
}

type createCommentRequest struct {
	Path             string   `json:"path"`
	Quote            string   `json:"quote"`
	Prefix           string   `json:"prefix"`
	Suffix           string   `json:"suffix"`
	Body             string   `json:"body"`
	Severity         string   `json:"severity"`
	DiscussionIDs    []string `json:"discussionIds"`
	PromptTemplateID string   `json:"promptTemplateId"`
}

type replyCommentRequest struct {
	Path             string   `json:"path"`
	ID               string   `json:"id"`
	Author           string   `json:"author"`
	Body             string   `json:"body"`
	Before           string   `json:"before"`
	After            string   `json:"after"`
	BeforeSnapshot   string   `json:"beforeSnapshot"`
	Snapshot         string   `json:"snapshot"`
	Resolve          bool     `json:"resolve"`
	DiscussionIDs    []string `json:"discussionIds"`
	PromptTemplateID string   `json:"promptTemplateId"`
}

type resolveCommentRequest struct {
	Path     string `json:"path"`
	ID       string `json:"id"`
	Resolved bool   `json:"resolved"`
}

type groupCommentRequest struct {
	Path      string   `json:"path"`
	ID        string   `json:"id"`
	MemberIDs []string `json:"memberIds"`
}

type rollbackCommentRequest struct {
	Path      string `json:"path"`
	ID        string `json:"id"`
	MessageID string `json:"messageId"`
	Side      string `json:"side"`
}

type rollbackCommentResponse struct {
	Round   int         `json:"round"`
	Comment Comment     `json:"comment"`
	Doc     docResponse `json:"doc"`
}

type commentsResponse struct {
	Path     string    `json:"path"`
	Comments []Comment `json:"comments"`
}

type triggerCommentRequest struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}
