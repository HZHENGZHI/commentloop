package server

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Discussion domain types: imported sessions, consensus and their API payloads.

type Discussion struct {
	ID           string    `json:"id"`
	DocPath      string    `json:"-"`
	Title        string    `json:"title"`
	SourcePath   string    `json:"sourcePath"`
	SourceKind   string    `json:"sourceKind"`
	Agent        string    `json:"agent"`
	AgentName    string    `json:"agentName"`
	Cwd          string    `json:"cwd,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	MessageCount int       `json:"messageCount"`
	SourceMTime  int64     `json:"-"`
	SourceSize   int64     `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	ImportedAt   time.Time `json:"importedAt"`
}

type DiscussionMessage struct {
	Seq       int       `json:"seq"`
	Role      string    `json:"role"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Consensus struct {
	Content   string    `json:"content"`
	Source    string    `json:"source,omitempty"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SessionCandidate struct {
	Path         string    `json:"path"`
	Title        string    `json:"title"`
	SourceKind   string    `json:"sourceKind"`
	Agent        string    `json:"agent"`
	AgentName    string    `json:"agentName"`
	Cwd          string    `json:"cwd,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	MessageCount int       `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Imported     bool      `json:"imported,omitempty"`
}

type parsedSession struct {
	SessionID string
	Cwd       string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Messages  []sessionMessage
}

type sessionMessage struct {
	Role      string
	Body      string
	CreatedAt time.Time
}

type discussionStore struct {
	db *gorm.DB
}

type discussionSyncService struct {
	srv  *Server
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

type consensusScheduler struct {
	srv     *Server
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	running map[string]bool
	pending map[string]bool // true = regenerate without the old consensus
	wg      sync.WaitGroup
}

type discussionListResponse struct {
	Path        string       `json:"path"`
	Consensus   *Consensus   `json:"consensus,omitempty"`
	Discussions []Discussion `json:"discussions"`
}

type availableSessionsResponse struct {
	Sessions []SessionCandidate `json:"sessions"`
	Agents   []agentInfo        `json:"agents"`
}

type importDiscussionRequest struct {
	Path        string `json:"path"`
	SessionPath string `json:"sessionPath"`
}

type importDiscussionResponse struct {
	Discussion     Discussion `json:"discussion"`
	Consensus      *Consensus `json:"consensus,omitempty"`
	ConsensusError string     `json:"consensusError,omitempty"`
}

type refreshConsensusRequest struct {
	Path string `json:"path"`
}
