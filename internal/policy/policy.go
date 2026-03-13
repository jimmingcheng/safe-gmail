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

// Policy defines a one-account allowlist for Gmail reads.
type Policy struct {
	Owner            string
	AllowOwnerSent   bool
	Addresses        map[string]bool
	Domains          map[string]bool
	Labels           map[string]bool
	ResolvedLabelIDs map[string]bool
}

type file struct {
	Gmail *gmailSection `json:"gmail"`
}

type gmailSection struct {
	Owner          string   `json:"owner"`
	AllowOwnerSent bool     `json:"allow_owner_sent,omitempty"`
	Addresses      []string `json:"addresses"`
	Domains        []string `json:"domains"`
	Labels         []string `json:"labels,omitempty"`
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
		Owner:            owner,
		AllowOwnerSent:   raw.Gmail.AllowOwnerSent,
		Addresses:        make(map[string]bool, len(raw.Gmail.Addresses)),
		Domains:          make(map[string]bool, len(raw.Gmail.Domains)),
		Labels:           make(map[string]bool, len(raw.Gmail.Labels)),
		ResolvedLabelIDs: make(map[string]bool),
	}

	for _, value := range raw.Gmail.Addresses {
		value = NormalizeAddress(value)
		if value != "" {
			p.Addresses[value] = true
		}
	}
	for _, value := range raw.Gmail.Domains {
		value = NormalizeDomain(value)
		if value != "" {
			p.Domains[value] = true
		}
	}
	for _, value := range raw.Gmail.Labels {
		value = NormalizeLabel(value)
		if value != "" {
			p.Labels[value] = true
		}
	}

	return p, nil
}

// ResolveLabelNames populates ResolvedLabelIDs from the lowercased label-name map.
func (p *Policy) ResolveLabelNames(nameToID map[string]string) {
	if p == nil || len(p.Labels) == 0 {
		return
	}
	p.ResolvedLabelIDs = make(map[string]bool, len(p.Labels))
	for label := range p.Labels {
		if id, ok := nameToID[label]; ok && id != "" {
			p.ResolvedLabelIDs[id] = true
		}
	}
}

// AllowsMessage returns true when the fixed message metadata is visible under the policy.
func (p *Policy) AllowsMessage(parts Participants) bool {
	if p == nil {
		return true
	}
	if p.HasWhitelistedLabel(parts.LabelIDs) {
		return true
	}
	if p.IsOwnerSent(parts) {
		return true
	}

	emails := make([]string, 0, 1+len(parts.To)+len(parts.Cc)+len(parts.Bcc))
	if parts.From != "" {
		emails = append(emails, NormalizeAddress(parts.From))
	}
	emails = append(emails, parts.To...)
	emails = append(emails, parts.Cc...)
	emails = append(emails, parts.Bcc...)
	if len(emails) == 0 {
		return true
	}

	sawNonOwner := false
	for _, email := range emails {
		email = NormalizeAddress(email)
		if email == "" || p.IsOwner(email) {
			continue
		}
		sawNonOwner = true
		if p.IsAllowed(email) {
			return true
		}
	}
	return !sawNonOwner
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

// IsAllowed reports whether the email address matches an allowed address or domain.
func (p *Policy) IsAllowed(email string) bool {
	if p == nil {
		return true
	}
	email = NormalizeAddress(email)
	if email == "" {
		return true
	}
	if p.Addresses[email] {
		return true
	}
	if domain := domainOf(email); domain != "" && p.Domains[domain] {
		return true
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

// HasWhitelistedLabel reports whether any runtime-resolved label ID matches.
func (p *Policy) HasWhitelistedLabel(labelIDs []string) bool {
	if p == nil || len(p.ResolvedLabelIDs) == 0 {
		return false
	}
	for _, labelID := range labelIDs {
		if p.ResolvedLabelIDs[labelID] {
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

// NormalizeDomain canonicalizes a domain entry for policy storage.
func NormalizeDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "@")
	return value
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

func domainOf(email string) string {
	if idx := strings.LastIndex(email, "@"); idx >= 0 && idx+1 < len(email) {
		return email[idx+1:]
	}
	return ""
}
