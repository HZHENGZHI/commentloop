package server

import "sync"

// localTreeCache memoizes the Markdown listing of local roots. Walking a large
// directory on every /api/tree request is slow, especially on Windows; the
// watcher and our own writes invalidate the cached listing instead.
type localTreeCache struct {
	mu      sync.Mutex
	entries map[*root]localTreeEntry
	gen     map[*root]uint64
}

type localTreeEntry struct {
	files     []fileEntry
	truncated bool
}

func (c *localTreeCache) get(ro *root, list func(*root) ([]fileEntry, bool, error)) ([]fileEntry, bool, error) {
	c.mu.Lock()
	if entry, ok := c.entries[ro]; ok {
		c.mu.Unlock()
		return cloneFileEntries(entry.files), entry.truncated, nil
	}
	gen := c.gen[ro]
	c.mu.Unlock()

	files, truncated, err := list(ro)
	if err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	// Only store the listing if nothing changed while we were walking.
	if c.gen[ro] == gen {
		if c.entries == nil {
			c.entries = make(map[*root]localTreeEntry)
		}
		c.entries[ro] = localTreeEntry{files: cloneFileEntries(files), truncated: truncated}
	}
	c.mu.Unlock()
	return files, truncated, nil
}

func (c *localTreeCache) invalidate(ro *root) {
	if ro == nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, ro)
	if c.gen == nil {
		c.gen = make(map[*root]uint64)
	}
	c.gen[ro]++
	c.mu.Unlock()
}

func (c *localTreeCache) forget(ro *root) {
	c.mu.Lock()
	delete(c.entries, ro)
	delete(c.gen, ro)
	c.mu.Unlock()
}

func cloneFileEntries(files []fileEntry) []fileEntry {
	out := make([]fileEntry, len(files))
	copy(out, files)
	return out
}
