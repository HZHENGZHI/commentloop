package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

const dbFileName = "commentloop.db"

func openStateDB(stateDir string) (*gorm.DB, error) {
	dsn := ":memory:"
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return nil, err
		}
		dsn = filepath.Join(stateDir, dbFileName)
	}
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
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
	if err := useExplicitColumnValues(gdb); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateLegacySummaryDrafts(gdb); err != nil {
		_ = db.Close()
		return nil, err
	}
	if stateDir != "" {
		if err := migrateLegacyRoots(gdb, stateDir); err != nil {
			slog.Error("failed to migrate roots.json", "err", err)
		}
		if err := repairLegacyCodeRepoPaths(gdb, stateDir); err != nil {
			slog.Error("failed to repair legacy code repository paths", "err", err)
		}
	}
	return gdb, nil
}

// closeStateDB closes the connection pool behind gdb.
func closeStateDB(gdb *gorm.DB) error {
	db, err := gdb.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

// metaValue reads a meta key; found is false when the key is absent.
func metaValue(db *gorm.DB, key string) (value string, found bool, err error) {
	var row metaRow
	err = db.Take(&row, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return row.Value, err == nil, err
}

func setMeta(db *gorm.DB, key, value string) error {
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&metaRow{Key: key, Value: value}).Error
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

// useExplicitColumnValues drops the parsed `default:` tags from the cached
// model schemas once migration is done. The defaults are only needed for DDL;
// left in place, GORM would write e.g. 1 instead of a false (0) capability
// flag on Create. Afterwards every column is written with the Go value, like
// the hand-written INSERTs did.
func useExplicitColumnValues(gdb *gorm.DB) error {
	for _, model := range stateModels {
		stmt := &gorm.Statement{DB: gdb}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		for _, field := range stmt.Schema.Fields {
			field.HasDefaultValue = false
			field.DefaultValue = ""
			field.DefaultValueInterface = nil
		}
		stmt.Schema.FieldsWithDefaultDBValue = nil
	}
	return nil
}

func repairLegacyCodeRepoPaths(db *gorm.DB, stateDir string) error {
	oldState := filepath.Join(filepath.Dir(stateDir), "mdlive")
	if oldState == stateDir {
		return nil
	}
	var repos []codeRepoRow
	if err := db.Select("id", "path").Find(&repos).Error; err != nil {
		return err
	}
	for _, repo := range repos {
		next := rewriteLegacyRepoPath(repo.Path, oldState, stateDir)
		if next == repo.Path {
			continue
		}
		if err := db.Model(&codeRepoRow{}).Where("id = ?", repo.ID).Update("path", next).Error; err != nil {
			slog.Error("failed to repair code repository", "repo", repo.ID, "err", err)
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

func saveRootsDB(db *gorm.DB, defs []stateRoot) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&rootRow{}).Error; err != nil {
			return err
		}
		if len(defs) == 0 {
			return nil
		}
		rows := make([]rootRow, len(defs))
		for i, def := range defs {
			rows[i] = rootRow{
				Position: i, Kind: def.Kind, Path: def.Path, Name: def.Name,
				Host: def.Host, Port: def.Port, Username: def.User, KeyPath: def.KeyPath,
			}
		}
		return tx.Create(&rows).Error
	})
}

func loadRootsDB(db *gorm.DB) ([]stateRoot, error) {
	var rows []rootRow
	if err := db.Order("position").Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []stateRoot
	for _, row := range rows {
		out = append(out, stateRoot{
			Kind: row.Kind, Path: row.Path, Name: row.Name, Host: row.Host,
			Port: row.Port, User: row.Username, KeyPath: row.KeyPath,
		})
	}
	return out, nil
}

// migrateLegacyRoots imports the old JSON root list once. The JSON file is
// intentionally kept as a backup and is not modified afterwards.
func migrateLegacyRoots(db *gorm.DB, stateDir string) error {
	if migrated, _, _ := metaValue(db, "roots_json_migrated"); migrated == "1" {
		return nil
	}
	var count int64
	if err := db.Model(&rootRow{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return setMeta(db, "roots_json_migrated", "1")
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "roots.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return setMeta(db, "roots_json_migrated", "1")
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
		return setMeta(db, "roots_json_migrated", "1")
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
	return setMeta(db, "roots_json_migrated", "1")
}
