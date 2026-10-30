package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const dbFileName = "commentloop.db"

func openStateDB(stateDir string) (*sql.DB, error) {
	dsn := ":memory:"
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return nil, err
		}
		dsn = filepath.Join(stateDir, dbFileName)
	}
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn), gormlogger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, err
	}
	db, err := gdb.DB()
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
	if err := migrateStateSchema(gdb); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite schema: %w", err)
	}
	if stateDir != "" {
		if err := migrateLegacyRoots(db, stateDir); err != nil {
			slog.Error("failed to migrate roots.json", "err", err)
		}
		if err := repairLegacyCodeRepoPaths(db, stateDir); err != nil {
			slog.Error("failed to repair legacy code repository paths", "err", err)
		}
	}
	return db, nil
}

// migrateStateSchema creates missing tables, columns and indexes from
// stateModels. Unlike AutoMigrate it never alters existing columns: SQLite can
// only do that by rebuilding the table, which is risky for user state.
func migrateStateSchema(gdb *gorm.DB) error {
	m := gdb.Migrator()
	for _, model := range stateModels {
		if !m.HasTable(model) {
			if err := m.CreateTable(model); err != nil {
				return err
			}
			continue
		}
		stmt := &gorm.Statement{DB: gdb}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" || m.HasColumn(model, field.DBName) {
				continue
			}
			if err := m.AddColumn(model, field.DBName); err != nil {
				return fmt.Errorf("add %s.%s: %w", stmt.Schema.Table, field.DBName, err)
			}
		}
		for _, idx := range stmt.Schema.ParseIndexes() {
			if m.HasIndex(model, idx.Name) {
				continue
			}
			if err := m.CreateIndex(model, idx.Name); err != nil {
				return fmt.Errorf("create index %s: %w", idx.Name, err)
			}
		}
	}
	return nil
}

func repairLegacyCodeRepoPaths(db *sql.DB, stateDir string) error {
	oldState := filepath.Join(filepath.Dir(stateDir), "mdlive")
	if oldState == stateDir {
		return nil
	}
	rows, err := db.Query(`SELECT id, path FROM code_repos`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type repoPath struct{ id, path string }
	var repos []repoPath
	for rows.Next() {
		var item repoPath
		if err := rows.Scan(&item.id, &item.path); err != nil {
			return err
		}
		repos = append(repos, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, repo := range repos {
		next := rewriteLegacyRepoPath(repo.path, oldState, stateDir)
		if next == repo.path {
			continue
		}
		if _, err := db.Exec(`UPDATE code_repos SET path=? WHERE id=?`, next, repo.id); err != nil {
			slog.Error("failed to repair code repository", "repo", repo.id, "err", err)
		}
	}
	return nil
}

func rewriteLegacyRepoPath(path, oldState, newState string) string {
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, oldState+string(filepath.Separator)) {
		next := newState + clean[len(oldState):]
		if pathExists(next) && !pathExists(clean) {
			return next
		}
	}
	if filepath.Base(clean) == "mdlive" {
		next := filepath.Join(filepath.Dir(clean), "commentloop")
		if pathExists(next) && !pathExists(clean) {
			return next
		}
	}
	return path
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
