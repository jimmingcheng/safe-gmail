package policy

import "testing"

func TestParseNormalizesAllowlistEntries(t *testing.T) {
	t.Parallel()

	p, err := Parse([]byte(`{
  "gmail": {
    "owner": "Owner@Example.com",
    "allow_owner_sent": true,
    "addresses": ["Alice@Example.com"],
    "domains": ["@Example.org"],
    "labels": ["VIP"]
  }
}`), "")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !p.IsOwner("owner@example.com") {
		t.Fatal("owner address did not normalize")
	}
	if !p.AllowOwnerSent {
		t.Fatal("allow_owner_sent did not parse")
	}
	if !p.IsAllowed("alice@example.com") {
		t.Fatal("address allowlist did not normalize")
	}
	if !p.IsAllowed("bob@example.org") {
		t.Fatal("domain allowlist did not normalize")
	}

	p.ResolveLabelNames(map[string]string{"vip": "Label_1"})
	if !p.HasWhitelistedLabel([]string{"Label_1"}) {
		t.Fatal("label allowlist did not resolve to label IDs")
	}
}

func TestAllowsMessageRequiresAllowedNonOwnerOrWhitelistedLabel(t *testing.T) {
	t.Parallel()

	p := &Policy{
		Owner:            "owner@example.com",
		Addresses:        map[string]bool{"alice@example.com": true},
		Domains:          map[string]bool{"example.org": true},
		Labels:           map[string]bool{"vip": true},
		ResolvedLabelIDs: map[string]bool{"Label_1": true},
	}

	if !p.AllowsMessage(Participants{
		From: "alice@example.com",
		To:   []string{"owner@example.com"},
	}) {
		t.Fatal("expected allowed sender to pass policy")
	}

	if !p.AllowsMessage(Participants{
		From:     "mallory@example.net",
		To:       []string{"owner@example.com"},
		LabelIDs: []string{"Label_1"},
	}) {
		t.Fatal("expected whitelisted label to override address restrictions")
	}

	if p.AllowsMessage(Participants{
		From: "mallory@example.net",
		To:   []string{"owner@example.com"},
	}) {
		t.Fatal("expected restricted sender to be denied")
	}

	if !p.AllowsMessage(Participants{
		From: "owner@example.com",
		To:   []string{"owner@example.com"},
	}) {
		t.Fatal("expected owner-only message to remain visible")
	}
}

func TestAllowsMessageAllowsOwnerSentWhenEnabled(t *testing.T) {
	t.Parallel()

	p := &Policy{
		Owner:          "owner@gmail.com",
		AllowOwnerSent: true,
	}

	if !p.AllowsMessage(Participants{
		From: "owner@gmail.com",
		To:   []string{"friend@example.net"},
	}) {
		t.Fatal("expected owner-sent message to remain visible")
	}

	if !p.AllowsMessage(Participants{
		From:     "alias@example.net",
		To:       []string{"friend@example.net"},
		LabelIDs: []string{"SENT"},
	}) {
		t.Fatal("expected sent-label message to remain visible")
	}
}

func TestAllowsMessageIgnoresOwnerAddressInDomainMatches(t *testing.T) {
	t.Parallel()

	p := &Policy{
		Owner:   "owner@gmail.com",
		Domains: map[string]bool{"gmail.com": true},
	}

	if p.AllowsMessage(Participants{
		From: "mallory@example.net",
		To:   []string{"owner@gmail.com"},
	}) {
		t.Fatal("expected owner recipient address to not satisfy allowed gmail.com domain")
	}

	if !p.AllowsMessage(Participants{
		From: "mallory@gmail.com",
		To:   []string{"owner@gmail.com"},
	}) {
		t.Fatal("expected non-owner gmail.com sender to satisfy allowed gmail.com domain")
	}
}
