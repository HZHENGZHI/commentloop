package server

import (
	"sync"
	"time"

	"github.com/hzhengzhi/commentloop/internal/render"
)

// File and document domain types: tree listings, browsing, editor and LSP payloads.

type docResponse struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	HTML     string    `json:"html"`
	Revision string    `json:"revision"`
	ModTime  time.Time `json:"modTime"`
	Size     int64     `json:"size"`
}

type fileEntry struct {
	Path     string    `json:"path"`    // canonical: <rootID>/<relative-path>
	RelPath  string    `json:"relPath"` // path inside its directory
	Name     string    `json:"name"`
	Dir      string    `json:"dir"`
	RootID   string    `json:"rootId"`
	RootName string    `json:"rootName"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"modTime"`
}

type treeRoot struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Remote  string      `json:"remote,omitempty"`
	Primary bool        `json:"primary"`
	Offline string      `json:"offline,omitempty"`
	Loading bool        `json:"loading,omitempty"`
	Files   []fileEntry `json:"files"`
}

type treeResponse struct {
	Roots     []treeRoot  `json:"roots"`
	Files     []fileEntry `json:"files"`
	Truncated bool        `json:"truncated"`
}

type saveRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type browseEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Hidden bool   `json:"hidden"`
}

type browseShortcut struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type browseResponse struct {
	Path      string           `json:"path"`
	Parent    string           `json:"parent"`
	Entries   []browseEntry    `json:"entries"`
	Shortcuts []browseShortcut `json:"shortcuts"`
	Truncated bool             `json:"truncated"`
}

type browsePathError struct{ message string }

type localTreeEntry struct {
	files     []fileEntry
	truncated bool
}

// localTreeCache memoizes the Markdown listing of local roots. Walking a large
// directory on every /api/tree request is slow, especially on Windows; the
// watcher and our own writes invalidate the cached listing instead.
type localTreeCache struct {
	mu      sync.Mutex
	entries map[*root]localTreeEntry
	gen     map[*root]uint64
}

type docSnapshot struct {
	path    string
	content string
	result  render.Result
	modTime time.Time
	size    int64
}

type lspDefinitionRequest struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}
