package gmailapi

import (
	"net/url"
	"strings"
	"unicode"
)

// NormalizeMessageID accepts either a raw Gmail message ID or a mail.google.com URL.
func NormalizeMessageID(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return ""
	}

	parsed := parseMaybeURL(value)
	if parsed == nil {
		return value
	}

	host := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(parsed.Host), "www."))
	if host != "mail.google.com" && host != "gmail.google.com" {
		return value
	}

	query := parsed.Query()
	for _, key := range []string{"message_id", "msg"} {
		if id := strings.TrimSpace(query.Get(key)); looksLikeHexID(id) {
			return id
		}
	}
	if raw := strings.TrimSpace(query.Get("permmsgid")); raw != "" {
		if idx := strings.LastIndex(raw, ":"); idx >= 0 && idx+1 < len(raw) {
			raw = raw[idx+1:]
		}
		raw = strings.TrimSpace(raw)
		if looksLikeHexID(raw) {
			return raw
		}
	}

	return value
}

// NormalizeThreadID accepts either a raw Gmail thread ID or a mail.google.com URL.
func NormalizeThreadID(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return ""
	}

	parsed := parseMaybeURL(value)
	if parsed == nil {
		return value
	}

	host := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(parsed.Host), "www."))
	if host != "mail.google.com" && host != "gmail.google.com" {
		return value
	}

	if threadID := strings.TrimSpace(parsed.Query().Get("th")); looksLikeHexID(threadID) {
		return threadID
	}

	fragment := strings.TrimSpace(parsed.Fragment)
	if fragment == "" {
		return value
	}
	fragment = strings.SplitN(fragment, "?", 2)[0]
	parts := strings.Split(strings.Trim(fragment, "/"), "/")
	if len(parts) == 0 {
		return value
	}

	last := strings.TrimSpace(parts[len(parts)-1])
	if looksLikeHexID(last) {
		return last
	}
	return value
}

func parseMaybeURL(value string) *url.URL {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	return parsed
}

func looksLikeHexID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 10 {
		return false
	}
	for _, r := range value {
		switch {
		case unicode.IsDigit(r):
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
