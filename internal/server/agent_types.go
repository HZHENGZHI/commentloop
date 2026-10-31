package server

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Agent domain types: capabilities, jobs, sessions, runners and the Pi RPC protocol.

// agentCapabilityStore ties write permission to the currently running
// comment task for an agent session. A prompt can ask an analysis-only task
// not to write, but the server must enforce it for requests that carry the
// managed agent session header.
type agentCapabilities struct {
	WriteDocs     bool
	ReadCode      bool
	ReplyComments bool
}

type agentGrant struct {
	Capabilities agentCapabilities
	DocPath      string
	RepoIDs      []string
	JobID        string
}

type agentCapabilityStore struct {
	mu     sync.RWMutex
	byTask map[string]agentGrant
	tokens map[string]agentGrant
}

type AgentJob struct {
	ID               string            `json:"id"`
	DocPath          string            `json:"docPath"`
	CommentID        string            `json:"commentId"`
	AgentType        string            `json:"agentType"`
	Prompt           string            `json:"prompt"`
	PromptTemplateID string            `json:"promptTemplateId,omitempty"`
	Capabilities     agentCapabilities `json:"capabilities"`
	RepoIDs          []string          `json:"repoIds,omitempty"`
	Token            string            `json:"-"`
	Status           string            `json:"status"`
	RunnerID         string            `json:"runnerId,omitempty"`
	LeaseUntil       time.Time         `json:"leaseUntil,omitempty"`
	Error            string            `json:"error,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	StartedAt        time.Time         `json:"startedAt,omitempty"`
	FinishedAt       time.Time         `json:"finishedAt,omitempty"`
}

type agentJobStore struct {
	db *gorm.DB
}

type agentSession struct {
	ID          string    `json:"id"`
	DocPath     string    `json:"docPath"`
	PiSessionID string    `json:"-"`
	Title       string    `json:"title"`
	Active      bool      `json:"active,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type agentSessionStore struct {
	db *gorm.DB
}

type agentSessionRequest struct {
	Path  string `json:"path"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

type agentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Supported bool   `json:"supported"`
	Count     int    `json:"count"`
}

type sessionRoot struct {
	Path       string
	Agent      string
	SourceKind string
}

type sessionLocation struct {
	Path       string
	Agent      string
	SourceKind string
}

type commandLookup struct {
	installed bool
	checked   time.Time
}

type reviewAgentJob struct {
	Path         string
	CommentID    string
	Prompt       string
	Capabilities agentCapabilities
	RepoIDs      []string
}

type sessionAgentWorker struct {
	srv     *Server
	session agentSession

	mu      sync.Mutex
	queue   []reviewAgentJob
	wake    chan struct{}
	stop    chan struct{}
	closed  bool
	proc    *piRPC
	procSig string
	wg      sync.WaitGroup
}

type agentRunner struct {
	srv *Server

	mu      sync.Mutex
	workers map[string]*sessionAgentWorker
	closed  bool
}

type piRecord struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		Disposition string `json:"disposition"`
	} `json:"data"`
	Aborted  bool            `json:"aborted"`
	ToolName string          `json:"toolName"`
	Args     json.RawMessage `json:"args"`
	IsError  bool            `json:"isError"`
	Message  struct {
		Role string `json:"role"`
	} `json:"message"`
}

type piRPC struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	reader   *bufio.Reader
	events   chan piRecord
	closed   chan struct{}
	closeOne sync.Once
	writeMu  sync.Mutex
	promptID int
	stderr   logBuffer
}

type logBuffer struct {
	mu   sync.Mutex
	data []byte
}

type agentClaimResponse struct {
	AgentJob
	Token string `json:"token"`
}

type agentRunnerClaimRequest struct {
	RunnerID  string `json:"runnerId"`
	AgentType string `json:"agentType"`
}

type agentRunnerFinishRequest struct {
	RunnerID string `json:"runnerId"`
	ID       string `json:"id"`
	Success  bool   `json:"success"`
	Error    string `json:"error"`
}

type agentRunnerHeartbeatRequest struct {
	RunnerID string `json:"runnerId"`
	ID       string `json:"id"`
}
