package server

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Remote (SSH/SFTP) domain types.

type remoteConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"keyPath,omitempty"`
	Path    string `json:"path"`
	Name    string `json:"name,omitempty"`
	// KnownHostsPath is only used by tests; production uses ~/.ssh/known_hosts.
	KnownHostsPath string `json:"-"`
}

type remoteFS struct {
	cfg       remoteConfig
	conn      net.Conn
	ssh       *ssh.Client
	sftp      *sftp.Client
	mu        sync.Mutex
	closeOnce sync.Once
}

type remoteFile struct {
	Rel     string
	Name    string
	Size    int64
	ModTime time.Time
}

type remoteBrowseSession struct {
	fs       *remoteFS
	home     string
	lastUsed time.Time
}

type remoteSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*remoteBrowseSession
}

type remoteSessionRequest struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"keyPath"`
}

type remoteSessionResponse struct {
	Session string `json:"session"`
	Home    string `json:"home"`
}

type remoteSig struct {
	size int64
	mod  int64
}
