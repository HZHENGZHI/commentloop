package server

import "sync"

// Workspace root domain types.

// root is one directory (local or remote) added to the workspace.
type root struct {
	ID      string
	Kind    string
	Path    string // local absolute path or remote absolute path
	Name    string
	Primary bool
	Remote  *remoteConfig
	Offline string

	remoteFS *remoteFS
	lister   *remoteFS

	pollStop chan struct{}
	pollOnce sync.Once
	pollWG   sync.WaitGroup
}

type rootInfo struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Remote  string `json:"remote,omitempty"`
	Primary bool   `json:"primary"`
	Offline string `json:"offline,omitempty"`
}

type stateRoot struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Name    string `json:"name,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	User    string `json:"user,omitempty"`
	KeyPath string `json:"keyPath,omitempty"`
}

type tooManyRootsError struct{ limit int }

type addRootRequest struct {
	Path string `json:"path"`
}

type addRemoteRootRequest struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"keyPath"`
	Path    string `json:"path"`
}

type addRootResponse struct {
	Root    rootInfo `json:"root"`
	Warning string   `json:"warning,omitempty"`
}
