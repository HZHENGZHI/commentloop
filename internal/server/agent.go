package server

import (
	"net/http"
	"sync"
)

// handleAgentRefresh forces a synchronous rescan of remote roots. The normal
// tree endpoint serves remote caches so a slow server cannot block local
// files; agent tools can request a fresh scan explicitly.
func (s *Server) handleAgentRefresh(w http.ResponseWriter, _ *http.Request) {
	roots := s.rootsSnapshot()
	var wg sync.WaitGroup
	for _, ro := range roots {
		if ro.Kind != rootKindRemote {
			continue
		}
		fs := ro.lister
		if fs == nil {
			fs = ro.remoteFS
		}
		if fs == nil {
			continue
		}
		wg.Add(1)
		go func(ro *root, fs *remoteFS) {
			defer wg.Done()
			files, err := fs.list()
			if err != nil {
				return
			}
			s.storeRemoteTree(ro, files)
		}(ro, fs)
	}
	wg.Wait()
	s.hub.broadcast(outMessage{Type: "tree"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
