package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultMaxBodyBytes       = 64 * 1024
	defaultMaxAttachmentBytes = 25 * 1024 * 1024
	defaultMaxSearchResults   = 100
	defaultSocketMode         = "0660"
	defaultAuthStoreBackend   = "system"
	defaultAuthStoreService   = "safe-gmail"
)

// Config is the trusted-side daemon configuration for one broker instance.
type Config struct {
	Instance           string          `json:"instance"`
	AccountEmail       string          `json:"account_email"`
	ClientUID          uint32          `json:"client_uid"`
	SocketPath         string          `json:"socket_path"`
	SocketMode         string          `json:"socket_mode,omitempty"`
	MaxBodyBytes       int             `json:"max_body_bytes,omitempty"`
	MaxAttachmentBytes int             `json:"max_attachment_bytes,omitempty"`
	MaxSearchResults   int             `json:"max_search_results,omitempty"`
	OAuthClientPath    string          `json:"oauth_client_path"`
	PolicyPath         string          `json:"policy_path"`
	StatePath          string          `json:"state_path,omitempty"`
	AuthStore          AuthStoreConfig `json:"auth_store,omitempty"`
}

// AuthStoreConfig controls where the broker stores its refresh token.
type AuthStoreConfig struct {
	Backend string `json:"backend,omitempty"`
	Service string `json:"service,omitempty"`
	FileDir string `json:"file_dir,omitempty"`
}

// Load reads a broker config from JSON.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if strings.TrimSpace(c.SocketMode) == "" {
		c.SocketMode = defaultSocketMode
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = defaultMaxBodyBytes
	}
	if c.MaxAttachmentBytes <= 0 {
		c.MaxAttachmentBytes = defaultMaxAttachmentBytes
	}
	if c.MaxSearchResults <= 0 {
		c.MaxSearchResults = defaultMaxSearchResults
	}
	c.AuthStore.applyDefaults(c.StatePath)
}

func (c *AuthStoreConfig) applyDefaults(statePath string) {
	if strings.TrimSpace(c.Backend) == "" {
		c.Backend = defaultAuthStoreBackend
	}
	if strings.TrimSpace(c.Service) == "" {
		c.Service = defaultAuthStoreService
	}
	if strings.EqualFold(strings.TrimSpace(c.Backend), "file") && strings.TrimSpace(c.FileDir) == "" && strings.TrimSpace(statePath) != "" {
		c.FileDir = filepath.Join(filepath.Dir(statePath), "keyring")
	}
}

// Validate enforces the minimal invariants for a broker instance config.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Instance) == "" {
		return fmt.Errorf("config: instance is required")
	}
	if strings.TrimSpace(c.AccountEmail) == "" {
		return fmt.Errorf("config: account_email is required")
	}
	if strings.TrimSpace(c.SocketPath) == "" {
		return fmt.Errorf("config: socket_path is required")
	}
	if filepath.Clean(c.SocketPath) == "." {
		return fmt.Errorf("config: socket_path must not resolve to current directory")
	}
	if strings.TrimSpace(c.OAuthClientPath) == "" {
		return fmt.Errorf("config: oauth_client_path is required")
	}
	if filepath.Clean(c.OAuthClientPath) == "." {
		return fmt.Errorf("config: oauth_client_path must not resolve to current directory")
	}
	if strings.TrimSpace(c.PolicyPath) == "" {
		return fmt.Errorf("config: policy_path is required")
	}
	if filepath.Clean(c.PolicyPath) == "." {
		return fmt.Errorf("config: policy_path must not resolve to current directory")
	}
	if strings.TrimSpace(c.StatePath) != "" && filepath.Clean(c.StatePath) == "." {
		return fmt.Errorf("config: state_path must not resolve to current directory")
	}
	if c.ClientUID == 0 {
		return fmt.Errorf("config: client_uid must be non-zero")
	}
	if _, err := c.SocketFileMode(); err != nil {
		return err
	}
	if err := c.AuthStore.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate enforces the minimal invariants for auth store configuration.
func (c AuthStoreConfig) Validate() error {
	switch strings.ToLower(strings.TrimSpace(c.Backend)) {
	case "", "system":
		return nil
	case "file":
		if strings.TrimSpace(c.FileDir) == "" {
			return fmt.Errorf("config: auth_store.file_dir is required when auth_store.backend=file")
		}
		if filepath.Clean(c.FileDir) == "." {
			return fmt.Errorf("config: auth_store.file_dir must not resolve to current directory")
		}
		return nil
	default:
		return fmt.Errorf("config: unsupported auth_store.backend %q", c.Backend)
	}
}

// SocketFileMode parses SocketMode as an octal filesystem mode.
func (c Config) SocketFileMode() (os.FileMode, error) {
	value := strings.TrimSpace(c.SocketMode)
	if value == "" {
		value = defaultSocketMode
	}
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("config: invalid socket_mode %q", c.SocketMode)
	}
	return os.FileMode(parsed), nil
}
