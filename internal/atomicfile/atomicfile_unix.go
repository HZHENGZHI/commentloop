//go:build !windows

package atomicfile

import (
	"os"

	"github.com/google/renameio/v2"
)

// WriteFile replaces filename with data. An existing file keeps its
// permissions; a new file gets perm exactly, ignoring the process umask, so a
// restrictive umask cannot silently make shared documents unreadable.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	return renameio.WriteFile(filename, data, perm, renameio.IgnoreUmask())
}
