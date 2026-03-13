package auth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OAuthClient is the trusted-side Google OAuth client configuration.
type OAuthClient struct {
	ClientID     string
	ClientSecret string
	RedirectURIs []string
	AuthURL      string
	TokenURL     string
}

type oauthClientFile struct {
	Installed *oauthClientSection `json:"installed"`
	Web       *oauthClientSection `json:"web"`
}

type oauthClientSection struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
}

// LoadOAuthClient reads and parses a Google OAuth client JSON file.
func LoadOAuthClient(path string) (OAuthClient, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("read oauth client: %w", err)
	}
	return ParseOAuthClientJSON(data)
}

// ParseOAuthClientJSON parses the standard Google credentials JSON format.
func ParseOAuthClientJSON(data []byte) (OAuthClient, error) {
	var raw oauthClientFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return OAuthClient{}, fmt.Errorf("parse oauth client json: %w", err)
	}

	var section *oauthClientSection
	switch {
	case raw.Installed != nil:
		section = raw.Installed
	case raw.Web != nil:
		section = raw.Web
	default:
		return OAuthClient{}, fmt.Errorf("oauth client json must contain an installed or web client")
	}

	if strings.TrimSpace(section.ClientID) == "" || strings.TrimSpace(section.ClientSecret) == "" {
		return OAuthClient{}, fmt.Errorf("oauth client json is missing client_id or client_secret")
	}

	redirectURIs := make([]string, 0, len(section.RedirectURIs))
	for _, value := range section.RedirectURIs {
		value = strings.TrimSpace(value)
		if value != "" {
			redirectURIs = append(redirectURIs, value)
		}
	}

	return OAuthClient{
		ClientID:     strings.TrimSpace(section.ClientID),
		ClientSecret: strings.TrimSpace(section.ClientSecret),
		RedirectURIs: redirectURIs,
		AuthURL:      strings.TrimSpace(section.AuthURI),
		TokenURL:     strings.TrimSpace(section.TokenURI),
	}, nil
}

// DefaultRedirectURI chooses a usable redirect URI from the client config.
func (c OAuthClient) DefaultRedirectURI() (string, error) {
	for _, raw := range c.RedirectURIs {
		if isLoopbackRedirect(raw) {
			return raw, nil
		}
	}
	if len(c.RedirectURIs) == 1 {
		return c.RedirectURIs[0], nil
	}
	return "", fmt.Errorf("oauth client json has no usable loopback redirect URI; pass --redirect-uri explicitly")
}

// Config returns an oauth2.Config for the provided scopes and redirect URI.
func (c OAuthClient) Config(scopes []string, redirectURI string) *oauth2.Config {
	endpoint := google.Endpoint
	if c.AuthURL != "" || c.TokenURL != "" {
		endpoint = oauth2.Endpoint{
			AuthURL:  endpoint.AuthURL,
			TokenURL: endpoint.TokenURL,
		}
		if c.AuthURL != "" {
			endpoint.AuthURL = c.AuthURL
		}
		if c.TokenURL != "" {
			endpoint.TokenURL = c.TokenURL
		}
	}

	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  redirectURI,
		Endpoint:     endpoint,
		Scopes:       scopes,
	}
}

func isLoopbackRedirect(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
