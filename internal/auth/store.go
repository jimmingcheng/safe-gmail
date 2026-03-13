package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/99designs/keyring"
	"golang.org/x/oauth2"

	"github.com/jimmingcheng/safe-gmail/internal/config"
)

const (
	// FileBackendPasswordEnv supplies the passphrase for the encrypted keyring
	// file backend.
	FileBackendPasswordEnv = "SAFE_GMAIL_KEYRING_PASSWORD"
	keyringOpenTimeout     = 5 * time.Second
	tokenRefreshTimeout    = 30 * time.Second
)

var (
	// ErrTokenNotFound indicates that no broker token is stored for the instance.
	ErrTokenNotFound = errors.New("broker token not found")
	openKeyringFn    = keyring.Open
)

// TokenStore persists one OAuth token per broker instance/account.
type TokenStore interface {
	Save(instance, email string, tok *oauth2.Token) error
	Load(instance, email string) (*oauth2.Token, error)
}

type tokenRecord struct {
	AccessToken  string    `json:"access_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

// KeyringStore implements TokenStore using github.com/99designs/keyring.
type KeyringStore struct {
	ring        keyring.Keyring
	serviceName string
}

// OpenTokenStore opens the configured broker-owned token store.
func OpenTokenStore(cfg config.AuthStoreConfig) (TokenStore, error) {
	serviceName := strings.TrimSpace(cfg.Service)
	if serviceName == "" {
		serviceName = "safe-gmail"
	}

	ringCfg := keyring.Config{
		ServiceName: serviceName,
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Backend)) {
	case "", "system":
		backends, err := systemBackends()
		if err != nil {
			return nil, err
		}
		ringCfg.AllowedBackends = backends
	case "file":
		password := os.Getenv(FileBackendPasswordEnv)
		if password == "" {
			return nil, fmt.Errorf("%s is required when auth_store.backend=file", FileBackendPasswordEnv)
		}
		ringCfg.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
		ringCfg.FileDir = cfg.FileDir
		ringCfg.FilePasswordFunc = func(_ string) (string, error) {
			return password, nil
		}
	default:
		return nil, fmt.Errorf("unsupported keyring backend %q", cfg.Backend)
	}

	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	ring, err := openKeyring(ringCfg, backend == "" || backend == "system")
	if err != nil {
		return nil, err
	}
	return &KeyringStore{ring: ring, serviceName: serviceName}, nil
}

func openKeyring(cfg keyring.Config, useTimeout bool) (keyring.Keyring, error) {
	if useTimeout && runtime.GOOS == "linux" {
		return openKeyringWithTimeout(cfg, keyringOpenTimeout)
	}
	ring, err := openKeyringFn(cfg)
	if err != nil {
		return nil, fmt.Errorf("open keyring: %w", err)
	}
	return ring, nil
}

func openKeyringWithTimeout(cfg keyring.Config, timeout time.Duration) (keyring.Keyring, error) {
	type result struct {
		ring keyring.Keyring
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		ring, err := openKeyringFn(cfg)
		ch <- result{ring: ring, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("open keyring: %w", res.err)
		}
		return res.ring, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("open keyring timed out after %s; on Linux, try auth_store.backend=file", timeout)
	}
}

func systemBackends() ([]keyring.BackendType, error) {
	switch runtime.GOOS {
	case "darwin":
		return []keyring.BackendType{keyring.KeychainBackend}, nil
	case "linux":
		return []keyring.BackendType{keyring.SecretServiceBackend}, nil
	default:
		return nil, fmt.Errorf("unsupported OS for system keyring: %s", runtime.GOOS)
	}
}

// Save stores the token under the broker's instance/account key.
func (s *KeyringStore) Save(instance, email string, tok *oauth2.Token) error {
	if tok == nil {
		return fmt.Errorf("missing token")
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return fmt.Errorf("missing refresh token")
	}

	record := tokenRecord{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	item := keyring.Item{
		Key:   storeKey(instance, email),
		Data:  data,
		Label: s.serviceName,
	}
	if err := s.ring.Set(item); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

// Load reads the stored token for the broker instance/account.
func (s *KeyringStore) Load(instance, email string) (*oauth2.Token, error) {
	item, err := s.ring.Get(storeKey(instance, email))
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, fmt.Errorf("load token: %w", err)
	}

	var record tokenRecord
	if err := json.Unmarshal(item.Data, &record); err != nil {
		return nil, fmt.Errorf("parse stored token: %w", err)
	}
	if strings.TrimSpace(record.RefreshToken) == "" {
		return nil, fmt.Errorf("stored token is missing refresh_token")
	}

	return &oauth2.Token{
		AccessToken:  record.AccessToken,
		TokenType:    record.TokenType,
		RefreshToken: record.RefreshToken,
		Expiry:       record.Expiry,
	}, nil
}

func storeKey(instance, email string) string {
	return fmt.Sprintf("%s:%s", strings.TrimSpace(instance), strings.ToLower(strings.TrimSpace(email)))
}

// TokenSource returns a refresh-capable token source that persists token
// updates back into the broker-owned store.
func TokenSource(ctx context.Context, client OAuthClient, store TokenStore, instance, email string, scopes []string) (oauth2.TokenSource, error) {
	tok, err := store.Load(instance, email)
	if err != nil {
		return nil, err
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: tokenRefreshTimeout})
	base := client.Config(scopes, "").TokenSource(ctx, tok)
	return &persistingTokenSource{
		base:     base,
		store:    store,
		instance: instance,
		email:    email,
		last:     cloneToken(tok),
	}, nil
}

type persistingTokenSource struct {
	base     oauth2.TokenSource
	store    TokenStore
	instance string
	email    string

	mu   sync.Mutex
	last *oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh oauth token: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	normalized := cloneToken(tok)
	if strings.TrimSpace(normalized.RefreshToken) == "" && p.last != nil {
		normalized.RefreshToken = p.last.RefreshToken
	}

	if tokensEqual(p.last, normalized) {
		return normalized, nil
	}
	_ = p.store.Save(p.instance, p.email, normalized)
	p.last = cloneToken(normalized)
	return normalized, nil
}

func tokensEqual(a, b *oauth2.Token) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.AccessToken == b.AccessToken &&
			a.TokenType == b.TokenType &&
			a.RefreshToken == b.RefreshToken &&
			a.Expiry.Equal(b.Expiry)
	}
}

func cloneToken(tok *oauth2.Token) *oauth2.Token {
	if tok == nil {
		return nil
	}
	return &oauth2.Token{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}
}
