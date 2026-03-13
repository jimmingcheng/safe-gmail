package auth

import (
	"context"
	"testing"
)

func TestExchangeRedirectRequiresState(t *testing.T) {
	t.Parallel()

	flow, err := NewManualFlow(OAuthClient{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURIs: []string{"http://127.0.0.1/oauth2/callback"},
	}, "", []string{"scope-a"}, false)
	if err != nil {
		t.Fatalf("NewManualFlow() error = %v", err)
	}

	_, err = flow.ExchangeRedirect(context.Background(), "http://127.0.0.1/oauth2/callback?code=test-code")
	if err != ErrStateMismatch {
		t.Fatalf("ExchangeRedirect() error = %v, want %v", err, ErrStateMismatch)
	}
}
