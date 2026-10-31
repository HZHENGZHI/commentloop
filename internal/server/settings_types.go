package server

import (
	"sync"

	"github.com/hzhengzhi/commentloop/internal/lsp"
	"gorm.io/gorm"
)

// Settings domain types.

type appSettings struct {
	LSPServers []lsp.ServerConfig `json:"lspServers"`
	Agent      codeAgentSettings  `json:"agent"`
}

type codeAgentSettings struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Runner  string   `json:"runner"`
	AutoRun bool     `json:"autoRun"`
}

type settingsSnapshot struct {
	mu       sync.RWMutex
	settings appSettings
}

type settingsStore struct {
	db *gorm.DB
}

type checkCommandRequest struct {
	Command string `json:"command"`
}
