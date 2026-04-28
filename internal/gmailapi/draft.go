package gmailapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// OAuthScopes returns the Gmail scopes required by the broker's implemented API.
func OAuthScopes() []string {
	return []string{
		gmail.GmailReadonlyScope,
		gmail.GmailComposeScope,
	}
}

// DraftCreateInput is the normalized content used to create a Gmail draft.
type DraftCreateInput struct {
	From       string
	To         []string
	Cc         []string
	Bcc        []string
	Subject    string
	BodyText   string
	ThreadID   string
	InReplyTo  string
	References []string
}

// DraftCreateResult contains Gmail IDs returned after creating a draft.
type DraftCreateResult struct {
	DraftID   string
	MessageID string
	ThreadID  string
}

// DraftReplyContext contains the threading headers derived from a visible message.
type DraftReplyContext struct {
	ThreadID   string
	Subject    string
	InReplyTo  string
	References []string
}

// NormalizeAddressList parses comma-separated or repeated address inputs into
// lowercase mailbox addresses.
func NormalizeAddressList(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		addrs, err := mail.ParseAddressList(value)
		if err != nil {
			return nil, fmt.Errorf("invalid email address %q: %w", value, err)
		}
		for _, addr := range addrs {
			email := strings.ToLower(strings.TrimSpace(addr.Address))
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			result = append(result, email)
		}
	}
	return nonNilStrings(result), nil
}

// DraftReplyContextFromMessage derives RFC 5322 reply headers from a Gmail message.
func DraftReplyContextFromMessage(msg *gmail.Message) DraftReplyContext {
	if msg == nil {
		return DraftReplyContext{}
	}

	messageID := sanitizeHeaderValue(headerValue(msg.Payload, "Message-ID"))
	references := splitReferences(headerValue(msg.Payload, "References"))
	if len(references) == 0 {
		references = splitReferences(headerValue(msg.Payload, "In-Reply-To"))
	}
	if messageID != "" && !containsReference(references, messageID) {
		references = append(references, messageID)
	}

	return DraftReplyContext{
		ThreadID:   NormalizeThreadID(msg.ThreadId),
		Subject:    ReplySubject(headerValue(msg.Payload, "Subject")),
		InReplyTo:  messageID,
		References: references,
	}
}

// ReplySubject returns a conventional reply subject while preserving existing
// reply prefixes.
func ReplySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

// DraftReplyRecipients derives reply or reply-all recipients from a visible message.
func DraftReplyRecipients(msg *gmail.Message, owner string, replyAll bool) ([]string, []string, error) {
	if msg == nil {
		return nil, nil, fmt.Errorf("missing reply message")
	}

	owner = strings.ToLower(strings.TrimSpace(owner))
	parts := MessageParticipants(msg)
	add := func(values []string, seen map[string]bool, result *[]string) {
		for _, value := range values {
			email := strings.ToLower(strings.TrimSpace(value))
			if email == "" || email == owner || seen[email] {
				continue
			}
			seen[email] = true
			*result = append(*result, email)
		}
	}

	seen := map[string]bool{}
	to := []string{}
	cc := []string{}

	replyTo, _ := NormalizeAddressList([]string{headerValue(msg.Payload, "Reply-To")})
	replyAddress := parts.From
	if len(replyTo) > 0 {
		replyAddress = replyTo[0]
	}

	if !strings.EqualFold(strings.TrimSpace(replyAddress), owner) {
		add([]string{replyAddress}, seen, &to)
	}
	if replyAll || len(to) == 0 {
		add(parts.To, seen, &to)
	}
	if replyAll {
		add(parts.Cc, seen, &cc)
	}

	return nonNilStrings(to), nonNilStrings(cc), nil
}

// BuildDraftRaw creates a base64url RFC 5322 payload for Gmail drafts.
func BuildDraftRaw(input DraftCreateInput) (string, error) {
	from, err := NormalizeAddressList([]string{input.From})
	if err != nil {
		return "", err
	}
	if len(from) != 1 {
		return "", fmt.Errorf("missing from address")
	}

	to, err := NormalizeAddressList(input.To)
	if err != nil {
		return "", err
	}
	cc, err := NormalizeAddressList(input.Cc)
	if err != nil {
		return "", err
	}
	bcc, err := NormalizeAddressList(input.Bcc)
	if err != nil {
		return "", err
	}

	var raw bytes.Buffer
	writeHeader(&raw, "From", from[0])
	writeAddressHeader(&raw, "To", to)
	writeAddressHeader(&raw, "Cc", cc)
	writeAddressHeader(&raw, "Bcc", bcc)
	writeHeader(&raw, "Subject", encodeHeaderValue(input.Subject))
	writeHeader(&raw, "MIME-Version", "1.0")
	writeHeader(&raw, "Content-Type", `text/plain; charset="UTF-8"`)
	writeHeader(&raw, "Content-Transfer-Encoding", "8bit")
	writeHeader(&raw, "In-Reply-To", sanitizeHeaderValue(input.InReplyTo))
	writeHeader(&raw, "References", sanitizeHeaderValue(strings.Join(input.References, " ")))
	raw.WriteString("\r\n")
	raw.WriteString(normalizeBody(input.BodyText))

	return base64.RawURLEncoding.EncodeToString(raw.Bytes()), nil
}

// CreateDraft creates a Gmail draft without sending it.
func (c *Client) CreateDraft(ctx context.Context, input DraftCreateInput) (DraftCreateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	raw, err := BuildDraftRaw(input)
	if err != nil {
		return DraftCreateResult{}, err
	}

	msg := &gmail.Message{
		Raw: raw,
	}
	if strings.TrimSpace(input.ThreadID) != "" {
		msg.ThreadId = NormalizeThreadID(input.ThreadID)
	}

	draft, err := c.svc.Users.Drafts.Create("me", &gmail.Draft{Message: msg}).
		Context(ctx).
		Fields(googleapi.Field("id,message(id,threadId)")).
		Do()
	if err != nil {
		return DraftCreateResult{}, fmt.Errorf("create gmail draft: %w", err)
	}

	result := DraftCreateResult{
		DraftID: strings.TrimSpace(draft.Id),
	}
	if draft.Message != nil {
		result.MessageID = strings.TrimSpace(draft.Message.Id)
		result.ThreadID = strings.TrimSpace(draft.Message.ThreadId)
	}
	if result.ThreadID == "" {
		result.ThreadID = NormalizeThreadID(input.ThreadID)
	}
	return result, nil
}

func writeAddressHeader(raw *bytes.Buffer, name string, values []string) {
	if len(values) == 0 {
		return
	}
	writeHeader(raw, name, strings.Join(values, ", "))
}

func writeHeader(raw *bytes.Buffer, name, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	raw.WriteString(name)
	raw.WriteString(": ")
	raw.WriteString(value)
	raw.WriteString("\r\n")
}

func encodeHeaderValue(value string) string {
	value = sanitizeHeaderValue(value)
	if value == "" || isASCII(value) {
		return value
	}
	return mime.QEncoding.Encode("utf-8", value)
}

func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeBody(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func splitReferences(value string) []string {
	fields := strings.Fields(sanitizeHeaderValue(value))
	if len(fields) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

func containsReference(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

// NewestMessage returns the message with the latest Gmail internal date.
func NewestMessage(messages []*gmail.Message) *gmail.Message {
	var newest *gmail.Message
	var newestTime time.Time
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		t := time.UnixMilli(msg.InternalDate)
		if newest == nil || t.After(newestTime) {
			newest = msg
			newestTime = t
		}
	}
	return newest
}
