//go:build windows

package atomicfile

import (
	"os"
	"path/filepath"
)

// WriteFile replaces filename with data. Windows has no portable atomic
// replace, so write a complete temporary file and rename it over the target:
// a failed write leaves the previous file untouched. The temporary file is
// chmodded before the rename so new files get perm exactly.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filename)
	tmp, err := os.CreateTemp(dir, ".commentloop-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filename); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
