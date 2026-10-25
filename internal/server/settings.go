package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"mdlive/internal/lsp"
)

type codeAgentSettings struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Runner  string   `json:"runner"`
	AutoRun bool     `json:"autoRun"`
}

type appSettings struct {
	LSPServers []lsp.ServerConfig `json:"lspServers"`
	Agent      codeAgentSettings  `json:"agent"`
}

func defaultAppSettings() appSettings {
	return appSettings{
		LSPServers: lsp.DefaultServers(),
		Agent: codeAgentSettings{
			Type: "pi", Name: "Code Agent", Command: "pi", Args: []string{},
			Runner: "embedded", AutoRun: false,
		},
	}
}

type settingsStore struct {
	db *sql.DB
}

func newSettingsStore(db *sql.DB) *settingsStore {
	return &settingsStore{db: db}
}

func (s *settingsStore) load() (appSettings, error) {
	out := defaultAppSettings()
	var lspJSON, agentJSON string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key='lsp_servers'`).Scan(&lspJSON)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	err = s.db.QueryRow(`SELECT value FROM settings WHERE key='code_agent'`).Scan(&agentJSON)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	if lspJSON != "" {
		var configs []lsp.ServerConfig
		if err := json.Unmarshal([]byte(lspJSON), &configs); err == nil && len(configs) > 0 {
			out.LSPServers = mergeLSPServers(configs)
		}
	}
	if agentJSON != "" {
		var agent codeAgentSettings
		if err := json.Unmarshal([]byte(agentJSON), &agent); err == nil {
			out.Agent = normalizeAgent(agent)
		}
	}
	return out, nil
}

func mergeLSPServers(saved []lsp.ServerConfig) []lsp.ServerConfig {
	defaults := lsp.DefaultServers()
	byID := make(map[string]lsp.ServerConfig, len(saved))
	for _, item := range saved {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID != "" {
			byID[item.ID] = item
		}
	}
	out := make([]lsp.ServerConfig, 0, len(defaults)+len(byID))
	seen := make(map[string]struct{}, len(defaults))
	for _, item := range defaults {
		seen[item.ID] = struct{}{}
		if saved, ok := byID[item.ID]; ok {
			out = append(out, normalizeLSPConfig(saved))
		} else {
			out = append(out, item)
		}
	}
	for _, item := range saved {
		item = normalizeLSPConfig(item)
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; !ok {
			seen[item.ID] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func normalizeLSPConfig(config lsp.ServerConfig) lsp.ServerConfig {
	config.ID = strings.TrimSpace(config.ID)
	config.Label = strings.TrimSpace(config.Label)
	config.LanguageID = strings.TrimSpace(config.LanguageID)
	config.Command = strings.TrimSpace(config.Command)
	config.Extensions = cleanStrings(config.Extensions)
	config.Args = cleanStrings(config.Args)
	if config.Label == "" {
		config.Label = config.ID
	}
	return config
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func normalizeAgent(agent codeAgentSettings) codeAgentSettings {
	agent.Type = strings.TrimSpace(agent.Type)
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Command = strings.TrimSpace(agent.Command)
	agent.Args = cleanStrings(agent.Args)
	if agent.Type == "" {
		agent.Type = "custom"
	}
	if agent.Name == "" {
		agent.Name = "Code Agent"
	}
	return agent
}

func (s *settingsStore) save(settings appSettings) error {
	lspJSON, err := json.Marshal(settings.LSPServers)
	if err != nil {
		return err
	}
	agentJSON, err := json.Marshal(settings.Agent)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{
		"lsp_servers": string(lspJSON),
		"code_agent":  string(agentJSON),
	} {
		if _, err := tx.Exec(`INSERT INTO settings (key,value) VALUES (?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateSettings(settings appSettings) (appSettings, error) {
	settings.LSPServers = mergeLSPServers(settings.LSPServers)
	for i := range settings.LSPServers {
		settings.LSPServers[i] = normalizeLSPConfig(settings.LSPServers[i])
		config := settings.LSPServers[i]
		if len(config.Extensions) == 0 {
			return settings, fmt.Errorf("%s 没有配置文件扩展名", config.Label)
		}
		if config.LanguageID == "" {
			return settings, fmt.Errorf("%s 缺少 LSP languageId", config.Label)
		}
		if config.Enabled && config.Command == "" {
			return settings, fmt.Errorf("%s 已启用但命令为空", config.Label)
		}
	}
	settings.Agent = normalizeAgent(settings.Agent)
	if len(settings.Agent.Args) > 32 {
		return settings, fmt.Errorf("Agent 参数过多")
	}
	if settings.Agent.AutoRun {
		if settings.Agent.Runner != "external" {
			if !strings.EqualFold(settings.Agent.Type, "pi") {
				return settings, fmt.Errorf("嵌入自动运行目前仅支持 pi Agent；外部 Runner 可使用其他 Agent")
			}
			if settings.Agent.Command == "" {
				return settings, fmt.Errorf("自动运行需要填写 Agent 启动命令")
			}
		}
	}
	return settings, nil
}

type settingsSnapshot struct {
	mu       sync.RWMutex
	settings appSettings
}

func newSettingsSnapshot(settings appSettings) *settingsSnapshot {
	return &settingsSnapshot{settings: settings}
}

func (s *settingsSnapshot) get() appSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.LSPServers = append([]lsp.ServerConfig(nil), s.settings.LSPServers...)
	out.Agent.Args = append([]string(nil), s.settings.Agent.Args...)
	return out
}

func (s *settingsSnapshot) set(settings appSettings) {
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
}
