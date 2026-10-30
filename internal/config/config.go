// Package config loads commentloop settings from defaults, an optional YAML
// file, COMMENTLOOP_* environment variables and explicit CLI flags, in that
// order of increasing precedence.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// EnvPrefix prefixes environment overrides, e.g. COMMENTLOOP_LOG_LEVEL=debug
// sets log.level and COMMENTLOOP_REMOTE_POLL_INTERVAL=30s sets
// remote.poll_interval.
const EnvPrefix = "COMMENTLOOP_"

type Config struct {
	Server Server `koanf:"server"`
	Log    Log    `koanf:"log"`
	Limits Limits `koanf:"limits"`
	Remote Remote `koanf:"remote"`
	Agent  Agent  `koanf:"agent"`
}

type Server struct {
	Addr     string `koanf:"addr"`
	StateDir string `koanf:"state_dir"`
}

type Log struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

type Limits struct {
	// TreeFiles caps the Markdown files listed per local root.
	TreeFiles int `koanf:"tree_files"`
	// Roots caps the number of workspace directories.
	Roots int `koanf:"roots"`
}

type Remote struct {
	PollInterval time.Duration `koanf:"poll_interval"`
}

type Agent struct {
	JobLease              time.Duration `koanf:"job_lease"`
	ConsensusTimeout      time.Duration `koanf:"consensus_timeout"`
	PromptOptimizeTimeout time.Duration `koanf:"prompt_optimize_timeout"`
}

func Default() Config {
	return Config{
		Server: Server{Addr: "127.0.0.1:4571"},
		Log:    Log{Level: "info", Format: "text"},
		Limits: Limits{TreeFiles: 5000, Roots: 32},
		Remote: Remote{PollInterval: 15 * time.Second},
		Agent: Agent{
			JobLease:              20 * time.Minute,
			ConsensusTimeout:      2 * time.Minute,
			PromptOptimizeTimeout: 2 * time.Minute,
		},
	}
}

// WithDefaults fills unset (zero or negative) tunables from Default, so
// callers such as tests can pass a partial Config.
func (c Config) WithDefaults() Config {
	d := Default()
	if c.Server.Addr == "" {
		c.Server.Addr = d.Server.Addr
	}
	if c.Log.Level == "" {
		c.Log.Level = d.Log.Level
	}
	if c.Log.Format == "" {
		c.Log.Format = d.Log.Format
	}
	if c.Limits.TreeFiles <= 0 {
		c.Limits.TreeFiles = d.Limits.TreeFiles
	}
	if c.Limits.Roots <= 0 {
		c.Limits.Roots = d.Limits.Roots
	}
	if c.Remote.PollInterval <= 0 {
		c.Remote.PollInterval = d.Remote.PollInterval
	}
	if c.Agent.JobLease <= 0 {
		c.Agent.JobLease = d.Agent.JobLease
	}
	if c.Agent.ConsensusTimeout <= 0 {
		c.Agent.ConsensusTimeout = d.Agent.ConsensusTimeout
	}
	if c.Agent.PromptOptimizeTimeout <= 0 {
		c.Agent.PromptOptimizeTimeout = d.Agent.PromptOptimizeTimeout
	}
	return c
}

type LoadOptions struct {
	// File is an explicitly requested config file; it must exist.
	File string
	// DefaultFile is read only if it exists.
	DefaultFile string
	// Overrides holds explicitly set CLI flags keyed by dotted config path.
	Overrides map[string]any
	// Environ defaults to os.Environ.
	Environ func() []string
}

// Load returns the merged configuration and the config file that was read
// (empty if none).
func Load(o LoadOptions) (Config, string, error) {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		return Config{}, "", err
	}

	path := o.File
	if path == "" && o.DefaultFile != "" {
		if _, err := os.Stat(o.DefaultFile); err == nil {
			path = o.DefaultFile
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Config{}, "", fmt.Errorf("config %s: %w", o.DefaultFile, err)
		}
	}
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return Config{}, "", fmt.Errorf("config %s: %w", path, err)
		}
	}

	if err := k.Load(env.Provider(".", env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: envKey,
		EnvironFunc:   o.Environ,
	}), nil); err != nil {
		return Config{}, "", err
	}
	if len(o.Overrides) > 0 {
		if err := k.Load(confmap.Provider(o.Overrides, "."), nil); err != nil {
			return Config{}, "", err
		}
	}

	var cfg Config
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return Config{}, "", fmt.Errorf("config: %w", err)
	}
	return cfg.WithDefaults(), path, nil
}

// envKey maps COMMENTLOOP_SECTION_SOME_KEY to section.some_key and drops
// variables without a section (e.g. COMMENTLOOP_CONFIG).
func envKey(key, value string) (string, any) {
	key = strings.ToLower(strings.TrimPrefix(key, EnvPrefix))
	section, rest, ok := strings.Cut(key, "_")
	if !ok || section == "" || rest == "" {
		return "", nil
	}
	return section + "." + rest, value
}
