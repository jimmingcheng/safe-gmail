package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesAuthStoreDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "broker.json")
	data := []byte(`{
  "instance": "work",
  "account_email": "owner@example.com",
  "client_uid": 501,
  "socket_path": "/tmp/safe-gmail.sock",
  "oauth_client_path": "/tmp/oauth-client.json",
  "policy_path": "/tmp/policy.json",
  "state_path": "/tmp/state/work/state.json"
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AuthStore.Backend != "system" {
		t.Fatalf("AuthStore.Backend = %q, want system", cfg.AuthStore.Backend)
	}
	if cfg.AuthStore.Service != "safe-gmail" {
		t.Fatalf("AuthStore.Service = %q, want safe-gmail", cfg.AuthStore.Service)
	}
	if cfg.SocketMode != "0660" {
		t.Fatalf("SocketMode = %q, want 0660", cfg.SocketMode)
	}
	if cfg.MaxAttachmentBytes != defaultMaxAttachmentBytes {
		t.Fatalf("MaxAttachmentBytes = %d, want %d", cfg.MaxAttachmentBytes, defaultMaxAttachmentBytes)
	}
}

func TestAuthStoreFileDefaultsFromStatePath(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Instance:        "work",
		AccountEmail:    "owner@example.com",
		ClientUID:       501,
		SocketPath:      "/tmp/safe-gmail.sock",
		OAuthClientPath: "/tmp/oauth-client.json",
		PolicyPath:      "/tmp/policy.json",
		StatePath:       "/var/lib/safe-gmail/work/state.json",
		AuthStore: AuthStoreConfig{
			Backend: "file",
		},
	}
	cfg.applyDefaults()

	want := "/var/lib/safe-gmail/work/keyring"
	if cfg.AuthStore.FileDir != want {
		t.Fatalf("AuthStore.FileDir = %q, want %q", cfg.AuthStore.FileDir, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsFileBackendWithoutDirectory(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Instance:        "work",
		AccountEmail:    "owner@example.com",
		ClientUID:       501,
		SocketPath:      "/tmp/safe-gmail.sock",
		OAuthClientPath: "/tmp/oauth-client.json",
		PolicyPath:      "/tmp/policy.json",
		AuthStore: AuthStoreConfig{
			Backend: "file",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}
