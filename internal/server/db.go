package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const dbFileName = "mdlive.db"

func openStateDB(stateDir string) (*sql.DB, error) {
	dsn := ":memory:"
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return nil, err
		}
		dsn = filepath.Join(stateDir, dbFileName)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite pragma: %w", err)
	}
	if stateDir != "" {
		if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite WAL: %w", err)
		}
	}
	if err := migrateSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if stateDir != "" {
		if err := migrateLegacyRoots(db, stateDir); err != nil {
			log.Printf("mdlive: 迁移 roots.json 失败: %v", err)
		}
	}
	return db, nil
}

func migrateSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS roots (
			position INTEGER NOT NULL,
			kind TEXT NOT NULL,
			path TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			port INTEGER NOT NULL DEFAULT 0,
			username TEXT NOT NULL DEFAULT '',
			key_path TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS comments (
			id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			quote TEXT NOT NULL,
			anchor TEXT NOT NULL DEFAULT '',
			group_id TEXT NOT NULL DEFAULT '',
			prefix TEXT NOT NULL DEFAULT '',
			suffix TEXT NOT NULL DEFAULT '',
			line INTEGER NOT NULL DEFAULT 0,
			start_offset INTEGER NOT NULL DEFAULT 0,
			end_offset INTEGER NOT NULL DEFAULT 0,
			resolved INTEGER NOT NULL DEFAULT 0,
			orphaned INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_comments_path ON comments(path);`,
		`CREATE TABLE IF NOT EXISTS comment_messages (
			id TEXT PRIMARY KEY,
			comment_id TEXT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
			author TEXT NOT NULL,
			body TEXT NOT NULL,
			before_text TEXT NOT NULL DEFAULT '',
			after_text TEXT NOT NULL DEFAULT '',
			version_id TEXT NOT NULL DEFAULT '',
			before_version_id TEXT NOT NULL DEFAULT '',
			section_start INTEGER NOT NULL DEFAULT 0,
			section_end INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_comment_messages_comment ON comment_messages(comment_id);`,
		`CREATE TABLE IF NOT EXISTS doc_versions (
			id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			hash TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(path, hash)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_doc_versions_path ON doc_versions(path, created_at);`,
		`CREATE TABLE IF NOT EXISTS code_repos (
			id TEXT PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'local',
			remote_url TEXT NOT NULL DEFAULT '',
			remote_branch TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS doc_code_repos (
			doc_path TEXT NOT NULL,
			repo_id TEXT NOT NULL REFERENCES code_repos(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			PRIMARY KEY(doc_path, repo_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_doc_code_repos_doc ON doc_code_repos(doc_path);`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS agent_sessions (
			id TEXT PRIMARY KEY,
			doc_path TEXT NOT NULL,
			pi_session_id TEXT NOT NULL,
			title TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_agent_sessions_doc ON agent_sessions(doc_path, created_at);`,
		`CREATE TABLE IF NOT EXISTS doc_agent_active (
			doc_path TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS doc_discussions (
			id TEXT PRIMARY KEY,
			doc_path TEXT NOT NULL,
			title TEXT NOT NULL,
			source_path TEXT NOT NULL,
			source_kind TEXT NOT NULL,
			agent TEXT NOT NULL DEFAULT 'pi',
			cwd TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			source_mtime INTEGER NOT NULL DEFAULT 0,
			source_size INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			imported_at TEXT NOT NULL,
			UNIQUE(doc_path, source_path)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_doc_discussions_doc ON doc_discussions(doc_path, imported_at);`,
		`CREATE TABLE IF NOT EXISTS discussion_messages (
			id TEXT PRIMARY KEY,
			discussion_id TEXT NOT NULL REFERENCES doc_discussions(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT '',
			UNIQUE(discussion_id, seq)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_discussion_messages_discussion ON discussion_messages(discussion_id, seq);`,
		`CREATE TABLE IF NOT EXISTS prompt_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL,
			allow_document_edit INTEGER NOT NULL DEFAULT 1,
			builtin INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS doc_consensus (
			doc_path TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("sqlite schema: %w", err)
		}
	}
	if err := ensureColumn(db, "comments", "anchor", `ALTER TABLE comments ADD COLUMN anchor TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comments", "group_id", `ALTER TABLE comments ADD COLUMN group_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comment_messages", "version_id", `ALTER TABLE comment_messages ADD COLUMN version_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comment_messages", "before_version_id", `ALTER TABLE comment_messages ADD COLUMN before_version_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "code_repos", "kind", `ALTER TABLE code_repos ADD COLUMN kind TEXT NOT NULL DEFAULT 'local'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "code_repos", "remote_url", `ALTER TABLE code_repos ADD COLUMN remote_url TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "code_repos", "remote_branch", `ALTER TABLE code_repos ADD COLUMN remote_branch TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comment_messages", "section_start", `ALTER TABLE comment_messages ADD COLUMN section_start INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comment_messages", "section_end", `ALTER TABLE comment_messages ADD COLUMN section_end INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comments", "discussion_ids", `ALTER TABLE comments ADD COLUMN discussion_ids TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "comments", "prompt_template_id", `ALTER TABLE comments ADD COLUMN prompt_template_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "doc_discussions", "agent", `ALTER TABLE doc_discussions ADD COLUMN agent TEXT NOT NULL DEFAULT 'pi'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "doc_discussions", "source_mtime", `ALTER TABLE doc_discussions ADD COLUMN source_mtime INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureColumn(db, "doc_discussions", "source_size", `ALTER TABLE doc_discussions ADD COLUMN source_size INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_comments_group ON comments(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comment_messages_version ON comment_messages(version_id)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("sqlite schema index: %w", err)
		}
	}
	return nil
}

func ensureColumn(db *sql.DB, table, name, statement string) error {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var columnName, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &kind, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if columnName == name {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if !found {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("add %s.%s: %w", table, name, err)
		}
	}
	return nil
}

func saveRootsDB(db *sql.DB, defs []stateRoot) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM roots`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO roots
		(position, kind, path, name, host, port, username, key_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, def := range defs {
		if _, err := stmt.Exec(i, def.Kind, def.Path, def.Name, def.Host, def.Port, def.User, def.KeyPath); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func loadRootsDB(db *sql.DB) ([]stateRoot, error) {
	rows, err := db.Query(`SELECT kind, path, name, host, port, username, key_path
		FROM roots ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stateRoot
	for rows.Next() {
		var def stateRoot
		if err := rows.Scan(&def.Kind, &def.Path, &def.Name, &def.Host, &def.Port, &def.User, &def.KeyPath); err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, rows.Err()
}

// migrateLegacyRoots imports the old JSON root list once. The JSON file is
// intentionally kept as a backup and is not modified afterwards.
func migrateLegacyRoots(db *sql.DB, stateDir string) error {
	var migrated string
	_ = db.QueryRow(`SELECT value FROM meta WHERE key = 'roots_json_migrated'`).Scan(&migrated)
	if migrated == "1" {
		return nil
	}
	count := 0
	if err := db.QueryRow(`SELECT COUNT(*) FROM roots`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		_, err := db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('roots_json_migrated', '1')`)
		return err
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "roots.json"))
	if err != nil {
		if os.IsNotExist(err) {
			_, markErr := db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('roots_json_migrated', '1')`)
			return markErr
		}
		return err
	}
	var envelope struct {
		Version int             `json:"version"`
		Roots   json.RawMessage `json:"roots"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	var defs []stateRoot
	if err := json.Unmarshal(envelope.Roots, &defs); err == nil {
		out := make([]stateRoot, 0, len(defs))
		for _, def := range defs {
			if def.Kind == rootKindRemote {
				if strings.TrimSpace(def.Host) != "" && strings.TrimSpace(def.User) != "" && strings.TrimSpace(def.Path) != "" {
					out = append(out, def)
				}
				continue
			}
			if filepath.IsAbs(def.Path) {
				def.Kind = rootKindLocal
				out = append(out, def)
			}
		}
		if err := saveRootsDB(db, out); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('roots_json_migrated', '1')`)
		return err
	}
	var paths []string
	if err := json.Unmarshal(envelope.Roots, &paths); err != nil {
		return err
	}
	out := make([]stateRoot, 0, len(paths))
	for _, path := range paths {
		if path != "" && filepath.IsAbs(path) {
			out = append(out, stateRoot{Kind: rootKindLocal, Path: path})
		}
	}
	if err := saveRootsDB(db, out); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR REPLACE INTO meta(key, value) VALUES ('roots_json_migrated', '1')`)
	return err
}
