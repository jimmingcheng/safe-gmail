package policy

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"strings"
)

// Participants is the fixed metadata used for broker-side read authorization.
type Participants struct {
	From     string
	To       []string
	Cc       []string
	Bcc      []string
	LabelIDs []string
}

// Policy defines the broker-side visibility policy for one Gmail account.
type Policy struct {
	Owner             string
	AllowOwnerSent    bool
	VisibilityLabel   string
	VisibilityLabelID string
}

type file struct {
	Gmail *gmailSection `json:"gmail"`
}

type gmailSection struct {
	Owner           string   `json:"owner"`
	AllowOwnerSent  bool     `json:"allow_owner_sent,omitempty"`
	VisibilityLabel string   `json:"visibility_label,omitempty"`
	Addresses       []string `json:"addresses,omitempty"`
	Domains         []string `json:"domains,omitempty"`
	Labels          []string `json:"labels,omitempty"`
}

// Load reads a policy file from disk and applies the configured owner.
func Load(path, owner string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	return Parse(data, owner)
}

// Parse parses a single-account broker policy file.
func Parse(data []byte, owner string) (*Policy, error) {
	var raw file
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	if raw.Gmail == nil {
		return nil, fmt.Errorf("policy: missing gmail section")
	}

	owner = NormalizeAddress(owner)
	fileOwner := NormalizeAddress(raw.Gmail.Owner)
	switch {
	case owner == "":
		owner = fileOwner
	case fileOwner != "" && fileOwner != owner:
		return nil, fmt.Errorf("policy owner %q does not match config account %q", raw.Gmail.Owner, owner)
	}

	p := &Policy{
		Owner:           owner,
		AllowOwnerSent:  raw.Gmail.AllowOwnerSent,
		VisibilityLabel: strings.TrimSpace(raw.Gmail.VisibilityLabel),
	}
	if len(raw.Gmail.Addresses) > 0 || len(raw.Gmail.Domains) > 0 || len(raw.Gmail.Labels) > 0 {
		return nil, fmt.Errorf("policy: addresses, domains, and labels are no longer supported; use visibility_label")
	}

	return p, nil
}

// ResolveVisibilityLabel resolves the configured Gmail label name to its immutable Gmail label ID.
func (p *Policy) ResolveVisibilityLabel(nameToID map[string]string) error {
	if p == nil || strings.TrimSpace(p.VisibilityLabel) == "" {
		return nil
	}
	if strings.TrimSpace(p.VisibilityLabelID) != "" {
		return nil
	}
	id, ok := nameToID[NormalizeLabel(p.VisibilityLabel)]
	if !ok || strings.TrimSpace(id) == "" {
		return fmt.Errorf("policy visibility_label %q was not found in Gmail", p.VisibilityLabel)
	}
	p.VisibilityLabelID = id
	return nil
}

// AllowsMessage returns true when the fixed message metadata is visible under the policy.
func (p *Policy) AllowsMessage(parts Participants) bool {
	if p == nil {
		return true
	}
	if p.HasVisibilityLabel(parts.LabelIDs) {
		return true
	}
	if p.IsOwnerSent(parts) {
		return true
	}
	return false
}

// IsOwnerSent reports whether the message should be treated as owner-sent.
func (p *Policy) IsOwnerSent(parts Participants) bool {
	if p == nil || !p.AllowOwnerSent {
		return false
	}
	if p.IsOwner(parts.From) {
		return true
	}
	for _, labelID := range parts.LabelIDs {
		if strings.EqualFold(strings.TrimSpace(labelID), "SENT") {
			return true
		}
	}
	return false
}

// IsOwner reports whether the email is the broker-owned Gmail account.
func (p *Policy) IsOwner(email string) bool {
	if p == nil || p.Owner == "" {
		return false
	}
	return NormalizeAddress(email) == p.Owner
}

// HasVisibilityLabel reports whether the configured visibility label is present.
func (p *Policy) HasVisibilityLabel(labelIDs []string) bool {
	if p == nil || strings.TrimSpace(p.VisibilityLabelID) == "" {
		return false
	}
	for _, labelID := range labelIDs {
		if strings.TrimSpace(labelID) == p.VisibilityLabelID {
			return true
		}
	}
	return false
}

// NormalizeAddress canonicalizes addresses for policy storage and matching.
func NormalizeAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	addr, err := mail.ParseAddress(value)
	if err == nil {
		return strings.ToLower(addr.Address)
	}
	if extracted := extractEmail(value); extracted != "" {
		return extracted
	}
	return strings.ToLower(value)
}

// NormalizeLabel canonicalizes a Gmail label name for case-insensitive lookup.
func NormalizeLabel(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ExtractEmails parses addresses from a header value into bare lowercase addresses.
func ExtractEmails(header string) []string {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}

	addrs, err := mail.ParseAddressList(header)
	if err == nil {
		result := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			if normalized := NormalizeAddress(addr.Address); normalized != "" {
				result = append(result, normalized)
			}
		}
		return result
	}

	parts := strings.Split(header, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := NormalizeAddress(part); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func extractEmail(value string) string {
	value = strings.TrimSpace(value)
	if start := strings.LastIndex(value, "<"); start >= 0 {
		if end := strings.LastIndex(value, ">"); end > start {
			candidate := strings.TrimSpace(value[start+1 : end])
			if strings.Contains(candidate, "@") {
				return strings.ToLower(candidate)
			}
		}
	}
	if strings.Contains(value, "@") {
		return strings.ToLower(value)
	}
	return ""
}
