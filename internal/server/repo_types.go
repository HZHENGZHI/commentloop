package server

import (
	"time"

	"gorm.io/gorm"
)

// Code repository domain types.

type codeRepoRecord struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Kind         string    `json:"kind,omitempty"`
	RemoteURL    string    `json:"remoteUrl,omitempty"`
	RemoteBranch string    `json:"remoteBranch,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	Head         string    `json:"head,omitempty"`
	Dirty        bool      `json:"dirty,omitempty"`
	Changed      []string  `json:"changed,omitempty"`
	Linked       bool      `json:"linked,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type codeRepoStore struct {
	db        *gorm.DB
	cacheRoot string
}

type docCodeRepoRequest struct {
	DocPath      string `json:"docPath"`
	RepoID       string `json:"repoId"`
	RepoPath     string `json:"repoPath"`
	RemoteURL    string `json:"remoteUrl"`
	RemoteBranch string `json:"remoteBranch"`
}

type switchBranchRequest struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
}
