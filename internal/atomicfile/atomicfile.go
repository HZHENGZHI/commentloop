// Package atomicfile replaces files without exposing partial content.
//
// On platforms where an atomic rename is available the write is delegated to
// renameio. Windows cannot atomically replace a file while it may be open, so
// the fallback still writes a complete temporary file before replacing the
// target; a failed write never truncates the previous file.
package atomicfile
