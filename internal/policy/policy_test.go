package policy

import "testing"

func TestParseVisibilityPolicy(t *testing.T) {
	t.Parallel()

	p, err := Parse([]byte(`{
  "gmail": {
    "owner": "Owner@Example.com",
    "allow_owner_sent": true,
    "visibility_label": "Donna"
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
	if p.VisibilityLabel != "Donna" {
		t.Fatalf("VisibilityLabel = %q, want Donna", p.VisibilityLabel)
	}
	if err := p.ResolveVisibilityLabel(map[string]string{"donna": "Label_1"}); err != nil {
		t.Fatalf("ResolveVisibilityLabel() error = %v", err)
	}
	if !p.HasVisibilityLabel([]string{"Label_1"}) {
		t.Fatal("visibility label did not resolve to label ID")
	}
}

func TestParseRejectsLegacyAllowlistFields(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{
  "gmail": {
    "owner": "owner@example.com",
    "addresses": ["alice@example.com"]
  }
}`), "")
	if err == nil {
		t.Fatal("Parse() error = nil, want error")
	}
}

func TestAllowsMessageRequiresVisibilityLabelOrOwnerSent(t *testing.T) {
	t.Parallel()

	p := &Policy{
		Owner:             "owner@example.com",
		VisibilityLabel:   "donna",
		VisibilityLabelID: "Label_1",
	}

	if !p.AllowsMessage(Participants{
		From:     "mallory@example.net",
		To:       []string{"owner@example.com"},
		LabelIDs: []string{"Label_1"},
	}) {
		t.Fatal("expected visibility label to allow message")
	}

	if p.AllowsMessage(Participants{
		From: "mallory@example.net",
		To:   []string{"owner@example.com"},
	}) {
		t.Fatal("expected unlabeled message to be denied")
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

func TestResolveVisibilityLabelRequiresKnownLabel(t *testing.T) {
	t.Parallel()

	p := &Policy{VisibilityLabel: "donna"}
	if err := p.ResolveVisibilityLabel(map[string]string{"other": "Label_2"}); err == nil {
		t.Fatal("ResolveVisibilityLabel() error = nil, want error")
	}
}
