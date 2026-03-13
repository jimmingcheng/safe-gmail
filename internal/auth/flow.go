package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

var (
	// ErrStateMismatch is returned when the pasted redirect URL does not match
	// the state generated for the flow.
	ErrStateMismatch = errors.New("oauth state mismatch")
)

// ManualFlow is a minimal copy-paste OAuth flow for trusted-side setup.
type ManualFlow struct {
	cfg          *oauth2.Config
	state        string
	forceConsent bool
}

// NewManualFlow constructs a broker-owned manual OAuth flow.
func NewManualFlow(client OAuthClient, redirectURI string, scopes []string, forceConsent bool) (*ManualFlow, error) {
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		defaultRedirect, err := client.DefaultRedirectURI()
		if err != nil {
			return nil, err
		}
		redirectURI = defaultRedirect
	}

	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("generate oauth state: %w", err)
	}

	return &ManualFlow{
		cfg:          client.Config(scopes, redirectURI),
		state:        state,
		forceConsent: forceConsent,
	}, nil
}

// AuthURL returns the Google consent URL for the flow.
func (f *ManualFlow) AuthURL() string {
	options := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline}
	if f.forceConsent {
		options = append(options, oauth2.SetAuthURLParam("prompt", "consent"))
	}
	return f.cfg.AuthCodeURL(f.state, options...)
}

// ExchangeRedirect exchanges a pasted redirect URL for an OAuth token.
func (f *ManualFlow) ExchangeRedirect(ctx context.Context, redirectURL string) (*oauth2.Token, error) {
	code, state, err := parseRedirectURL(redirectURL)
	if err != nil {
		return nil, err
	}
	if state == "" || state != f.state {
		return nil, ErrStateMismatch
	}

	tok, err := f.cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange auth code: %w", err)
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, fmt.Errorf("no refresh token received; retry with --force-consent")
	}
	return tok, nil
}

func parseRedirectURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse redirect url: %w", err)
	}
	code := strings.TrimSpace(parsed.Query().Get("code"))
	if code == "" {
		return "", "", fmt.Errorf("redirect url is missing code")
	}
	return code, strings.TrimSpace(parsed.Query().Get("state")), nil
}

func randomState() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}
