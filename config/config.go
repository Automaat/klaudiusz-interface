package config

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// ConfigData holds all configuration values
type ConfigData struct {
	Server struct {
		Port            string        `yaml:"port"`
		ReadTimeout     time.Duration `yaml:"read_timeout"`
		WriteTimeout    time.Duration `yaml:"write_timeout"`
		IdleTimeout     time.Duration `yaml:"idle_timeout"`
		ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	} `yaml:"server"`

	Claude struct {
		Path             string        `yaml:"path"`
		WorkingDir       string        `yaml:"working_dir"`
		ExecutionTimeout time.Duration `yaml:"execution_timeout"`
		MaxPromptLength  int           `yaml:"max_prompt_length"`
	} `yaml:"claude"`

	Session struct {
		Timeout         time.Duration `yaml:"timeout"`
		CleanupInterval time.Duration `yaml:"cleanup_interval"`
	} `yaml:"session"`

	Telegram struct {
		Enabled          bool   `yaml:"enabled"`
		BotToken         string `yaml:"bot_token"`
		GroupSessionMode string `yaml:"group_session_mode"`

		Voice struct {
			Enabled         bool          `yaml:"enabled"`
			MaxFileSize     int64         `yaml:"max_file_size"`
			DownloadTimeout time.Duration `yaml:"download_timeout"`
		} `yaml:"voice"`

		Photo struct {
			Enabled         bool          `yaml:"enabled"`
			MaxFileSize     int64         `yaml:"max_file_size"`
			DownloadTimeout time.Duration `yaml:"download_timeout"`
		} `yaml:"photo"`
	} `yaml:"telegram"`

	Deepgram struct {
		APIKey   string `yaml:"api_key"`
		Language string `yaml:"language"`
		Model    string `yaml:"model"`
	} `yaml:"deepgram"`

	Memory struct {
		Enabled bool   `yaml:"enabled"`
		DBPath  string `yaml:"db_path"`

		Extraction struct {
			Interval         time.Duration `yaml:"interval"`
			Timeout          time.Duration `yaml:"timeout"`
			MaxConversations int           `yaml:"max_conversations"`
			FactLimit        int           `yaml:"fact_limit"`
			AdminTimeout     time.Duration `yaml:"admin_timeout"`
		} `yaml:"extraction"`
	} `yaml:"memory"`
}

// Config provides thread-safe access to configuration with live reload
type Config struct {
	mu       sync.RWMutex
	current  *ConfigData
	watcher  *fsnotify.Watcher
	path     string
	stopChan chan struct{}
}

// Get returns current config snapshot (thread-safe read)
func (c *Config) Get() *ConfigData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// reload updates config from file (thread-safe write)
func (c *Config) reload() error {
	newCfg, err := LoadFromFile(c.path)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	// Apply env overrides before validation (preserves secrets during hot reload)
	newCfg.LoadEnvOverrides()

	if err := newCfg.Validate(); err != nil {
		return errors.Wrap(err, "config validation failed")
	}

	c.mu.Lock()
	c.current = newCfg
	c.mu.Unlock()

	return nil
}

// Close stops the file watcher
func (c *Config) Close() error {
	if c.watcher != nil {
		close(c.stopChan)
		return c.watcher.Close()
	}
	return nil
}

// LoadFromFile reads and parses YAML config file
func LoadFromFile(path string) (*ConfigData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read config file")
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, errors.Wrap(err, "failed to parse YAML")
	}

	return cfg, nil
}

// DefaultConfig returns config with sensible defaults
func DefaultConfig() *ConfigData {
	cfg := &ConfigData{}

	// Server defaults
	cfg.Server.Port = "8742"
	cfg.Server.ReadTimeout = 15 * time.Second
	cfg.Server.WriteTimeout = 15 * time.Second
	cfg.Server.IdleTimeout = 60 * time.Second
	cfg.Server.ShutdownTimeout = 10 * time.Second

	// Claude defaults
	cfg.Claude.Path = "~/.local/bin/claude"
	cfg.Claude.WorkingDir = ""
	cfg.Claude.ExecutionTimeout = 2 * time.Minute
	cfg.Claude.MaxPromptLength = 100000

	// Session defaults
	cfg.Session.Timeout = 5 * time.Minute
	cfg.Session.CleanupInterval = 1 * time.Minute

	// Telegram defaults
	cfg.Telegram.Enabled = false
	cfg.Telegram.GroupSessionMode = "per_user"
	cfg.Telegram.Voice.Enabled = true
	cfg.Telegram.Voice.MaxFileSize = 20 * 1024 * 1024 // 20MB
	cfg.Telegram.Voice.DownloadTimeout = 30 * time.Second
	cfg.Telegram.Photo.Enabled = true
	cfg.Telegram.Photo.MaxFileSize = 20 * 1024 * 1024
	cfg.Telegram.Photo.DownloadTimeout = 30 * time.Second

	// Deepgram defaults
	cfg.Deepgram.Language = "pl"
	cfg.Deepgram.Model = "nova-3"

	// Memory defaults
	cfg.Memory.Enabled = true
	cfg.Memory.DBPath = "~/.klaudiusz/memory.db"
	cfg.Memory.Extraction.Interval = 10 * time.Minute
	cfg.Memory.Extraction.Timeout = 2 * time.Minute
	cfg.Memory.Extraction.MaxConversations = 20
	cfg.Memory.Extraction.FactLimit = 10
	cfg.Memory.Extraction.AdminTimeout = 2 * time.Minute

	return cfg
}

// Validate checks config values are valid
func (c *ConfigData) Validate() error {
	// Server validation
	if c.Server.Port == "" {
		return errors.New("server.port cannot be empty")
	}
	if c.Server.ReadTimeout <= 0 {
		return errors.New("server.read_timeout must be positive")
	}
	if c.Server.WriteTimeout <= 0 {
		return errors.New("server.write_timeout must be positive")
	}
	if c.Server.IdleTimeout <= 0 {
		return errors.New("server.idle_timeout must be positive")
	}
	if c.Server.ShutdownTimeout <= 0 {
		return errors.New("server.shutdown_timeout must be positive")
	}

	// Claude validation
	if c.Claude.Path == "" {
		return errors.New("claude.path cannot be empty")
	}
	if c.Claude.WorkingDir == "" {
		return errors.New("claude.working_dir cannot be empty")
	}
	if c.Claude.ExecutionTimeout <= 0 {
		return errors.New("claude.execution_timeout must be positive")
	}
	if c.Claude.MaxPromptLength <= 0 {
		return errors.New("claude.max_prompt_length must be positive")
	}

	// Session validation
	if c.Session.Timeout <= 0 {
		return errors.New("session.timeout must be positive")
	}
	if c.Session.CleanupInterval <= 0 {
		return errors.New("session.cleanup_interval must be positive")
	}

	// Telegram validation
	if c.Telegram.Enabled && c.Telegram.BotToken == "" {
		return errors.New("telegram.bot_token required when telegram.enabled=true")
	}
	if c.Telegram.GroupSessionMode != "" &&
		c.Telegram.GroupSessionMode != "per_user" &&
		c.Telegram.GroupSessionMode != "shared" {
		return errors.New("telegram.group_session_mode must be 'per_user' or 'shared'")
	}
	if c.Telegram.Voice.MaxFileSize <= 0 || c.Telegram.Voice.MaxFileSize > 1024*1024*1024 {
		return errors.New("telegram.voice.max_file_size must be 0 < size < 1GB")
	}
	if c.Telegram.Voice.DownloadTimeout <= 0 {
		return errors.New("telegram.voice.download_timeout must be positive")
	}
	if c.Telegram.Photo.MaxFileSize <= 0 || c.Telegram.Photo.MaxFileSize > 1024*1024*1024 {
		return errors.New("telegram.photo.max_file_size must be 0 < size < 1GB")
	}
	if c.Telegram.Photo.DownloadTimeout <= 0 {
		return errors.New("telegram.photo.download_timeout must be positive")
	}

	// Deepgram validation
	if c.Telegram.Enabled && c.Deepgram.APIKey == "" {
		return errors.New("deepgram.api_key required when telegram.enabled=true")
	}

	// Memory validation
	if c.Memory.Enabled && c.Memory.DBPath == "" {
		return errors.New("memory.db_path required when memory.enabled=true")
	}
	if c.Memory.Extraction.Interval <= 0 {
		return errors.New("memory.extraction.interval must be positive")
	}
	if c.Memory.Extraction.Timeout <= 0 {
		return errors.New("memory.extraction.timeout must be positive")
	}
	if c.Memory.Extraction.MaxConversations <= 0 {
		return errors.New("memory.extraction.max_conversations must be positive")
	}
	if c.Memory.Extraction.FactLimit <= 0 {
		return errors.New("memory.extraction.fact_limit must be positive")
	}
	if c.Memory.Extraction.AdminTimeout <= 0 {
		return errors.New("memory.extraction.admin_timeout must be positive")
	}

	return nil
}

// LoadEnvOverrides applies environment variable overrides (secrets only)
func (c *ConfigData) LoadEnvOverrides() {
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		c.Telegram.BotToken = token
	}
	if key := os.Getenv("DEEPGRAM_API_KEY"); key != "" {
		c.Deepgram.APIKey = key
	}
	if path := os.Getenv("CLAUDE_PATH"); path != "" {
		c.Claude.Path = path
	}
	if dir := os.Getenv("WORKING_DIR"); dir != "" {
		c.Claude.WorkingDir = dir
	}
	if path := os.Getenv("MEMORY_DB_PATH"); path != "" {
		c.Memory.DBPath = path
	}
}

// FindConfigFile searches for config in standard locations
func FindConfigFile(flagPath string) (string, error) {
	// Priority 1: --config flag
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err == nil {
			return flagPath, nil
		}
		return "", errors.Newf("config file specified via --config not found: %s", flagPath)
	}

	// Priority 2: CONFIG_PATH env var
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
		return "", errors.Newf("config file specified via CONFIG_PATH not found: %s", envPath)
	}

	// Priority 3-5: Standard locations
	home, _ := os.UserHomeDir()
	searchPaths := []string{
		"./config.yaml",
		filepath.Join(home, ".klaudiusz", "config.yaml"),
		"/etc/klaudiusz/config.yaml",
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// No config file found - use defaults
	return "", nil
}

// New creates Config with optional file watching
func New(path string, watch bool) (*Config, error) {
	var cfg *ConfigData

	if path == "" {
		// No config file - use defaults
		cfg = DefaultConfig()
	} else {
		// Load from file
		var err error
		cfg, err = LoadFromFile(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to load config")
		}

		if err := cfg.Validate(); err != nil {
			return nil, errors.Wrap(err, "config validation failed")
		}
	}

	// Apply env overrides
	cfg.LoadEnvOverrides()

	c := &Config{
		current:  cfg,
		path:     path,
		stopChan: make(chan struct{}),
	}

	// Start file watcher if requested and path exists
	if watch && path != "" {
		if err := c.startWatcher(); err != nil {
			return nil, errors.Wrap(err, "failed to start config watcher")
		}
	}

	return c, nil
}

// startWatcher begins watching config file for changes
func (c *Config) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Wrap(err, "failed to create watcher")
	}

	if err := watcher.Add(c.path); err != nil {
		watcher.Close()
		return errors.Wrap(err, "failed to watch config file")
	}

	c.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					if err := c.reload(); err != nil {
						// Log error but keep old config
						// In real implementation, use proper logger
						_ = err
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				// Log error
				_ = err
			case <-c.stopChan:
				return
			}
		}
	}()

	return nil
}
