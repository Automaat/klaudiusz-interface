package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "8742", cfg.Server.Port)
	assert.Equal(t, 15*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 5*time.Minute, cfg.Session.Timeout)
	assert.Equal(t, 100000, cfg.Claude.MaxPromptLength)
	assert.Equal(t, int64(20*1024*1024), cfg.Telegram.Voice.MaxFileSize)
	assert.True(t, cfg.Memory.Enabled)
}

func TestLoadFromFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
server:
  port: "9000"
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
  shutdown_timeout: 5s

claude:
  path: /custom/path/claude
  working_dir: /custom/workdir
  execution_timeout: 3m
  max_prompt_length: 50000

session:
  timeout: 10m
  cleanup_interval: 2m

telegram:
  enabled: true
  bot_token: "test-token"
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 10485760
    download_timeout: 15s
  photo:
    enabled: false
    max_file_size: 5242880
    download_timeout: 10s

deepgram:
  api_key: "test-key"
  language: en
  model: nova-2

memory:
  enabled: false
  db_path: /tmp/memory.db
  extraction:
    interval: 5m
    timeout: 1m
    max_conversations: 10
    fact_limit: 5
    admin_timeout: 30s
`

	err := os.WriteFile(cfgPath, []byte(yaml), 0o600)
	require.NoError(t, err)

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, "9000", cfg.Server.Port)
	assert.Equal(t, 30*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 3*time.Minute, cfg.Claude.ExecutionTimeout)
	assert.Equal(t, "/custom/path/claude", cfg.Claude.Path)
	assert.Equal(t, 50000, cfg.Claude.MaxPromptLength)
	assert.Equal(t, 10*time.Minute, cfg.Session.Timeout)
	assert.True(t, cfg.Telegram.Enabled)
	assert.Equal(t, "test-token", cfg.Telegram.BotToken)
	assert.False(t, cfg.Telegram.Photo.Enabled)
	assert.Equal(t, int64(10485760), cfg.Telegram.Voice.MaxFileSize)
	assert.False(t, cfg.Memory.Enabled)
	assert.Equal(t, 5*time.Minute, cfg.Memory.Extraction.Interval)
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
server:
  port: "8742"
  invalid yaml here [[[
`

	err := os.WriteFile(cfgPath, []byte(yaml), 0o600)
	require.NoError(t, err)

	_, err = LoadFromFile(cfgPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestValidate_Success(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Claude.WorkingDir = "/tmp/test" // Set explicit working dir (default is now empty)
	cfg.Telegram.Enabled = true
	cfg.Telegram.BotToken = "test-token"
	cfg.Deepgram.APIKey = "test-key"

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = ""

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server.port cannot be empty")
}

func TestValidate_NegativeTimeout(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*ConfigData)
		expected string
	}{
		{
			name:     "server read timeout",
			mutate:   func(c *ConfigData) { c.Server.ReadTimeout = -1 },
			expected: "server.read_timeout must be positive",
		},
		{
			name:     "server write timeout",
			mutate:   func(c *ConfigData) { c.Server.WriteTimeout = 0 },
			expected: "server.write_timeout must be positive",
		},
		{
			name:     "claude execution timeout",
			mutate:   func(c *ConfigData) { c.Claude.ExecutionTimeout = -5 * time.Second },
			expected: "claude.execution_timeout must be positive",
		},
		{
			name:     "session timeout",
			mutate:   func(c *ConfigData) { c.Session.Timeout = 0 },
			expected: "session.timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Claude.WorkingDir = "/tmp/test" // Set explicit working dir (default is now empty)
			tt.mutate(cfg)

			err := cfg.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestValidate_EmptyPaths(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*ConfigData)
		expected string
	}{
		{
			name:     "claude path",
			mutate:   func(c *ConfigData) { c.Claude.Path = "" },
			expected: "claude.path cannot be empty",
		},
		{
			name:     "working dir",
			mutate:   func(c *ConfigData) { c.Claude.WorkingDir = "" },
			expected: "claude.working_dir cannot be empty",
		},
		{
			name: "memory db path when enabled",
			mutate: func(c *ConfigData) {
				c.Claude.WorkingDir = "/tmp/test" // Set explicit working dir first
				c.Memory.Enabled = true
				c.Memory.DBPath = ""
			},
			expected: "memory.db_path required when memory.enabled=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)

			err := cfg.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestValidate_TelegramEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Claude.WorkingDir = "/tmp/test" // Set explicit working dir (default is now empty)
	cfg.Telegram.Enabled = true
	cfg.Telegram.BotToken = ""

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "telegram.bot_token required")
}

func TestValidate_DeepgramAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Claude.WorkingDir = "/tmp/test" // Set explicit working dir (default is now empty)
	cfg.Telegram.Enabled = true
	cfg.Telegram.BotToken = "test-token"
	cfg.Deepgram.APIKey = ""

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deepgram.api_key required")
}

func TestValidate_GroupSessionMode(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{"per_user valid", "per_user", true},
		{"shared valid", "shared", true},
		{"empty valid", "", true}, // empty is valid (defaults)
		{"invalid value", "invalid_mode", false},
		{"typo", "per-user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Claude.WorkingDir = "/tmp/test"
			cfg.Telegram.GroupSessionMode = tt.value

			err := cfg.Validate()
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "telegram.group_session_mode must be")
			}
		})
	}
}

func TestValidate_FileSizeLimits(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*ConfigData)
		expected string
	}{
		{
			name:     "voice file too large",
			mutate:   func(c *ConfigData) { c.Telegram.Voice.MaxFileSize = 2 * 1024 * 1024 * 1024 },
			expected: "telegram.voice.max_file_size must be 0 < size < 1GB",
		},
		{
			name:     "voice file zero",
			mutate:   func(c *ConfigData) { c.Telegram.Voice.MaxFileSize = 0 },
			expected: "telegram.voice.max_file_size must be 0 < size < 1GB",
		},
		{
			name:     "photo file negative",
			mutate:   func(c *ConfigData) { c.Telegram.Photo.MaxFileSize = -1 },
			expected: "telegram.photo.max_file_size must be 0 < size < 1GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Claude.WorkingDir = "/tmp/test" // Set explicit working dir (default is now empty)
			tt.mutate(cfg)

			err := cfg.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	cfg := DefaultConfig()

	// Set env vars
	os.Setenv("TELEGRAM_BOT_TOKEN", "env-token")
	os.Setenv("DEEPGRAM_API_KEY", "env-key")
	os.Setenv("CLAUDE_PATH", "/env/claude")
	os.Setenv("WORKING_DIR", "/env/workdir")
	os.Setenv("MEMORY_DB_PATH", "/env/memory.db")

	defer func() {
		os.Unsetenv("TELEGRAM_BOT_TOKEN")
		os.Unsetenv("DEEPGRAM_API_KEY")
		os.Unsetenv("CLAUDE_PATH")
		os.Unsetenv("WORKING_DIR")
		os.Unsetenv("MEMORY_DB_PATH")
	}()

	cfg.LoadEnvOverrides()

	assert.Equal(t, "env-token", cfg.Telegram.BotToken)
	assert.Equal(t, "env-key", cfg.Deepgram.APIKey)
	assert.Equal(t, "/env/claude", cfg.Claude.Path)
	assert.Equal(t, "/env/workdir", cfg.Claude.WorkingDir)
	assert.Equal(t, "/env/memory.db", cfg.Memory.DBPath)
}

func TestFindConfigFile_Priority(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	flagPath := filepath.Join(tmpDir, "flag.yaml")
	envPath := filepath.Join(tmpDir, "env.yaml")
	cwdPath := filepath.Join(tmpDir, "config.yaml")

	for _, path := range []string{flagPath, envPath, cwdPath} {
		err := os.WriteFile(path, []byte("server:\n  port: \"8742\"\n"), 0o600)
		require.NoError(t, err)
	}

	// Test flag priority
	path, err := FindConfigFile(flagPath)
	require.NoError(t, err)
	assert.Equal(t, flagPath, path)

	// Test env priority
	os.Setenv("CONFIG_PATH", envPath)

	defer os.Unsetenv("CONFIG_PATH")

	path, err = FindConfigFile("")
	require.NoError(t, err)
	assert.Equal(t, envPath, path)

	// Test no config found
	os.Unsetenv("CONFIG_PATH")

	path, err = FindConfigFile("")
	assert.NoError(t, err)
	assert.Empty(t, path)
}

func TestFindConfigFile_NotFound(t *testing.T) {
	_, err := FindConfigFile("/nonexistent/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config file specified via --config not found")

	os.Setenv("CONFIG_PATH", "/nonexistent/env.yaml")

	defer os.Unsetenv("CONFIG_PATH")

	_, err = FindConfigFile("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config file specified via CONFIG_PATH not found")
}

func TestNew_NoConfigFile(t *testing.T) {
	cfg, err := New("", false)
	require.NoError(t, err)

	defer cfg.Close()

	assert.NotNil(t, cfg.Get())
	assert.Equal(t, "8742", cfg.Get().Server.Port)
}

func TestNew_WithConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
server:
  port: "9999"
  read_timeout: 20s
  write_timeout: 20s
  idle_timeout: 90s
  shutdown_timeout: 8s
claude:
  path: /test/claude
  working_dir: /test/workdir
  execution_timeout: 1m
  max_prompt_length: 80000
session:
  timeout: 3m
  cleanup_interval: 30s
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 15728640
    download_timeout: 25s
  photo:
    enabled: true
    max_file_size: 15728640
    download_timeout: 25s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 8m
    timeout: 90s
    max_conversations: 15
    fact_limit: 8
    admin_timeout: 90s
`

	err := os.WriteFile(cfgPath, []byte(yaml), 0o600)
	require.NoError(t, err)

	cfg, err := New(cfgPath, false)
	require.NoError(t, err)

	defer cfg.Close()

	assert.Equal(t, "9999", cfg.Get().Server.Port)
	assert.Equal(t, 20*time.Second, cfg.Get().Server.ReadTimeout)
	assert.Equal(t, 3*time.Minute, cfg.Get().Session.Timeout)
}

func TestNew_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
server:
  port: ""
  read_timeout: -1s
`

	err := os.WriteFile(cfgPath, []byte(yaml), 0o600)
	require.NoError(t, err)

	_, err = New(cfgPath, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config validation failed")
}

func TestConfig_ThreadSafety(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `
server:
  port: "8742"
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s
  shutdown_timeout: 10s
claude:
  path: /test/claude
  working_dir: /test/workdir
  execution_timeout: 2m
  max_prompt_length: 100000
session:
  timeout: 5m
  cleanup_interval: 1m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
  photo:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 10m
    timeout: 2m
    max_conversations: 20
    fact_limit: 10
    admin_timeout: 2m
`

	err := os.WriteFile(cfgPath, []byte(yaml), 0o600)
	require.NoError(t, err)

	cfg, err := New(cfgPath, false)
	require.NoError(t, err)

	defer cfg.Close()

	// Concurrent reads and writes
	var wg sync.WaitGroup

	errChan := make(chan error, 15)

	// 10 concurrent readers
	for range 10 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 100 {
				port := cfg.Get().Server.Port
				if port == "" {
					errChan <- assert.AnError
					return
				}
			}
		}()
	}

	// 5 concurrent reloaders
	for range 5 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 10 {
				_ = cfg.reload()

				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}
}

func TestConfig_HotReload(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yaml1 := `
server:
  port: "8742"
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s
  shutdown_timeout: 10s
claude:
  path: /test/claude
  working_dir: /test/workdir
  execution_timeout: 2m
  max_prompt_length: 100000
session:
  timeout: 5m
  cleanup_interval: 1m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
  photo:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 10m
    timeout: 2m
    max_conversations: 20
    fact_limit: 10
    admin_timeout: 2m
`

	err := os.WriteFile(cfgPath, []byte(yaml1), 0o600)
	require.NoError(t, err)

	cfg, err := New(cfgPath, true)
	require.NoError(t, err)

	defer cfg.Close()

	assert.Equal(t, "8742", cfg.Get().Server.Port)

	// Modify config file
	yaml2 := `
server:
  port: "9999"
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
  shutdown_timeout: 15s
claude:
  path: /test/claude
  working_dir: /test/workdir
  execution_timeout: 3m
  max_prompt_length: 200000
session:
  timeout: 10m
  cleanup_interval: 2m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: false
    max_file_size: 10485760
    download_timeout: 15s
  photo:
    enabled: false
    max_file_size: 10485760
    download_timeout: 15s
deepgram:
  api_key: ""
  language: en
  model: nova-2
memory:
  enabled: false
  db_path: /tmp/memory.db
  extraction:
    interval: 5m
    timeout: 1m
    max_conversations: 10
    fact_limit: 5
    admin_timeout: 1m
`

	err = os.WriteFile(cfgPath, []byte(yaml2), 0o600)
	require.NoError(t, err)

	// Wait for watcher to detect change
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, "9999", cfg.Get().Server.Port)
	assert.Equal(t, 30*time.Second, cfg.Get().Server.ReadTimeout)
	assert.Equal(t, 10*time.Minute, cfg.Get().Session.Timeout)
	assert.False(t, cfg.Get().Telegram.Voice.Enabled)
	assert.False(t, cfg.Get().Memory.Enabled)
}

func TestConfig_InvalidReloadKeepsOld(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	validYaml := `
server:
  port: "8742"
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s
  shutdown_timeout: 10s
claude:
  path: /test/claude
  working_dir: /test/workdir
  execution_timeout: 2m
  max_prompt_length: 100000
session:
  timeout: 5m
  cleanup_interval: 1m
telegram:
  enabled: false
  bot_token: ""
  group_session_mode: per_user
  voice:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
  photo:
    enabled: true
    max_file_size: 20971520
    download_timeout: 30s
deepgram:
  api_key: ""
  language: pl
  model: nova-3
memory:
  enabled: true
  db_path: ~/.klaudiusz/memory.db
  extraction:
    interval: 10m
    timeout: 2m
    max_conversations: 20
    fact_limit: 10
    admin_timeout: 2m
`

	err := os.WriteFile(cfgPath, []byte(validYaml), 0o600)
	require.NoError(t, err)

	cfg, err := New(cfgPath, true)
	require.NoError(t, err)

	defer cfg.Close()

	assert.Equal(t, "8742", cfg.Get().Server.Port)

	// Write invalid config
	invalidYaml := `
server:
  port: ""
  read_timeout: -1s
`

	err = os.WriteFile(cfgPath, []byte(invalidYaml), 0o600)
	require.NoError(t, err)

	// Wait for watcher
	time.Sleep(200 * time.Millisecond)

	// Should keep old config
	assert.Equal(t, "8742", cfg.Get().Server.Port)
}
