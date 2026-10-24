// Package ignore centralizes the rules for which files and directories
// mdlive watches, lists and renders.
package ignore

import (
	"path/filepath"
	"strings"
)

// dirNames are skipped wherever they appear in the tree.
var dirNames = map[string]struct{}{
	"node_modules": {},
	"dist":         {},
	"build":        {},
	"out":          {},
	"target":       {},
	"vendor":       {},
	"venv":         {},
	"__pycache__":  {},
	".next":        {},
	".nuxt":        {},
	".cache":       {},
	".git":         {},
	".hg":          {},
	".svn":         {},
	".obsidian":    {},
	".trash":       {},
}

// SkipDir reports whether a directory should be hidden from mdlive.
func SkipDir(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return true
	}
	_, ok := dirNames[strings.ToLower(name)]
	return ok
}

// MarkdownExts is the set of file extensions treated as Markdown documents.
var MarkdownExts = map[string]struct{}{
	".md":       {},
	".markdown": {},
	".mdown":    {},
	".mkd":      {},
}

// IsMarkdown reports whether the path has a supported Markdown extension.
func IsMarkdown(path string) bool {
	_, ok := MarkdownExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

// IsTemp reports whether a path looks like an editor swap/temp file.
func IsTemp(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
		return true
	}
	if strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".swp") ||
		strings.HasSuffix(name, ".swo") || strings.HasSuffix(name, ".tmp") {
		return true
	}
	return false
}
