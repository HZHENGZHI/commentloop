package server

import (
	"time"

	"gorm.io/gorm"
)

// Prompt template domain types.

type PromptTemplate struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Body              string    `json:"body"`
	AllowDocumentEdit bool      `json:"allowDocumentEdit"`
	AllowCodeRead     bool      `json:"allowCodeRead"`
	AllowCommentReply bool      `json:"allowCommentReply"`
	Builtin           bool      `json:"builtin"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type promptTemplateStore struct {
	db *gorm.DB
}

type promptTemplateRequest struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Body              string `json:"body"`
	AllowDocumentEdit bool   `json:"allowDocumentEdit"`
	AllowCodeRead     bool   `json:"allowCodeRead"`
	AllowCommentReply bool   `json:"allowCommentReply"`
}

type optimizePromptRequest struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	Body              string `json:"body"`
	AllowDocumentEdit bool   `json:"allowDocumentEdit"`
	AllowCodeRead     bool   `json:"allowCodeRead"`
	AllowCommentReply bool   `json:"allowCommentReply"`
}
