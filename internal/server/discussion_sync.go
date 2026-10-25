package server

import (
	"log"
	"os"
	"sync"
	"time"
)

const discussionSyncInterval = 5 * time.Second

type discussionSyncService struct {
	srv  *Server
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func newDiscussionSyncService(srv *Server) *discussionSyncService {
	s := &discussionSyncService{
		srv: srv, stop: make(chan struct{}), done: make(chan struct{}),
	}
	go s.loop()
	return s
}

func (s *discussionSyncService) close() {
	s.once.Do(func() {
		close(s.stop)
		<-s.done
	})
}

func (s *discussionSyncService) loop() {
	defer close(s.done)
	ticker := time.NewTicker(discussionSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.syncAll()
		}
	}
}

func (s *discussionSyncService) syncAll() {
	items, err := s.srv.discussions.all()
	if err != nil {
		return
	}
	changedDocs := map[string]bool{}
	rebuildFromScratch := map[string]bool{}
	for _, item := range items {
		info, err := os.Stat(item.SourcePath)
		if err != nil || info.IsDir() {
			continue
		}
		mtime, size := info.ModTime().UnixNano(), info.Size()
		if mtime == item.SourceMTime && size == item.SourceSize {
			continue
		}
		parsed, err := parseAgentSession(item.Agent, item.SourcePath)
		if err != nil {
			// The session may be in the middle of being written. Try again on
			// the next poll rather than dropping the existing snapshot.
			continue
		}
		if err := s.srv.discussions.updateSessionMessages(
			item.ID, item.DocPath, parsed, item.SourcePath, item.SourceKind, item.Agent, mtime, size,
		); err != nil {
			log.Printf("commentloop: sync discussion %s: %v", item.ID, err)
			continue
		}
		changedDocs[item.DocPath] = true
		if size < item.SourceSize {
			rebuildFromScratch[item.DocPath] = true
		}
	}
	for docPath := range changedDocs {
		s.srv.hub.broadcast(outMessage{Type: "discussions", Path: docPath})
		if rebuildFromScratch[docPath] {
			s.srv.broadcastConsensus(docPath, "saving", "检测到会话内容变化，正在重建共识…")
			if err := s.srv.discussions.clearConsensus(docPath); err != nil {
				s.srv.broadcastConsensus(docPath, "error", err.Error())
				continue
			}
			if s.srv.consensus != nil {
				s.srv.consensus.schedule(docPath, false)
			}
		} else {
			s.srv.broadcastConsensus(docPath, "queued", "检测到会话有新内容，准备更新共识…")
			if s.srv.consensus != nil {
				s.srv.consensus.schedule(docPath, true)
			}
		}
	}
}

func (s *discussionStore) all() ([]Discussion, error) {
	rows, err := s.db.Query(`SELECT id,doc_path,title,source_path,source_kind,agent,cwd,session_id,
		message_count,source_mtime,source_size,created_at,imported_at
		FROM doc_discussions ORDER BY imported_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Discussion{}
	for rows.Next() {
		var item Discussion
		var createdAt, importedAt string
		if err := rows.Scan(&item.ID, &item.DocPath, &item.Title, &item.SourcePath, &item.SourceKind,
			&item.Agent, &item.Cwd, &item.SessionID, &item.MessageCount,
			&item.SourceMTime, &item.SourceSize, &createdAt, &importedAt); err != nil {
			return nil, err
		}
		item.AgentName = sessionAgentName(item.Agent)
		item.CreatedAt = parseTime(createdAt)
		item.ImportedAt = parseTime(importedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}
