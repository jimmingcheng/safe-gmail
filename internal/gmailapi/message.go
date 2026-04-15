package gmailapi

import (
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/api/gmail/v1"

	"github.com/jimmingcheng/safe-gmail/internal/policy"
	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

// MessageParticipants extracts the fixed authorization metadata from a Gmail message.
func MessageParticipants(msg *gmail.Message) policy.Participants {
	if msg == nil {
		return policy.Participants{}
	}

	fromList := policy.ExtractEmails(headerValue(msg.Payload, "From"))
	parts := policy.Participants{
		To:       nonNilStrings(policy.ExtractEmails(headerValue(msg.Payload, "To"))),
		Cc:       nonNilStrings(policy.ExtractEmails(headerValue(msg.Payload, "Cc"))),
		Bcc:      nonNilStrings(policy.ExtractEmails(headerValue(msg.Payload, "Bcc"))),
		LabelIDs: append([]string(nil), msg.LabelIds...),
	}
	if len(fromList) > 0 {
		parts.From = fromList[0]
	}
	return parts
}

// BuildMessageSummary converts a Gmail API message into the RPC summary shape.
func BuildMessageSummary(msg *gmail.Message) (rpc.MessageSummary, error) {
	if msg == nil {
		return rpc.MessageSummary{}, fmt.Errorf("missing gmail message")
	}

	parts := MessageParticipants(msg)
	return rpc.MessageSummary{
		MessageID:  msg.Id,
		ThreadID:   msg.ThreadId,
		From:       parts.From,
		To:         nonNilStrings(parts.To),
		Cc:         nonNilStrings(parts.Cc),
		Bcc:        nonNilStrings(parts.Bcc),
		Subject:    strings.TrimSpace(headerValue(msg.Payload, "Subject")),
		Snippet:    strings.TrimSpace(msg.Snippet),
		ReceivedAt: receivedAt(msg),
		LabelIDs:   nonNilStrings(append([]string(nil), msg.LabelIds...)),
	}, nil
}

// BuildMessageDetail converts a Gmail API message into the RPC result shape.
func BuildMessageDetail(msg *gmail.Message, includeBody bool, maxBodyBytes int) (rpc.MessageDetail, error) {
	if msg == nil {
		return rpc.MessageDetail{}, fmt.Errorf("missing gmail message")
	}

	summary, err := BuildMessageSummary(msg)
	if err != nil {
		return rpc.MessageDetail{}, err
	}

	detail := rpc.MessageDetail{
		MessageSummary: summary,
		Attachments:    nonNilAttachments(collectAttachments(msg.Payload)),
	}

	if includeBody {
		body, truncated := truncateUTF8(bestBodyText(msg.Payload), maxBodyBytes)
		detail.BodyText = body
		detail.BodyTruncated = boolPtr(truncated)
	}

	return detail, nil
}

func headerValue(part *gmail.MessagePart, name string) string {
	if part == nil {
		return ""
	}
	for _, header := range part.Headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func bestBodyText(part *gmail.MessagePart) string {
	if part == nil {
		return ""
	}
	if text := findPartBody(part, "text/plain"); text != "" {
		return text
	}
	return findPartBody(part, "text/html")
}

func findPartBody(part *gmail.MessagePart, mimeType string) string {
	if part == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(part.MimeType), mimeType) && part.Body != nil && part.Body.Data != "" {
		if decoded, err := decodeBody(part.Body.Data); err == nil {
			return decoded
		}
	}
	for _, child := range part.Parts {
		if decoded := findPartBody(child, mimeType); decoded != "" {
			return decoded
		}
	}
	return ""
}

func decodeBody(data string) (string, error) {
	decoded, err := decodeBase64Data(data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func decodeBase64Data(data string) ([]byte, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(data)
		if err != nil {
			return nil, err
		}
	}
	return decoded, nil
}

func receivedAt(msg *gmail.Message) string {
	if msg == nil {
		return ""
	}

	if msg.InternalDate > 0 {
		return time.UnixMilli(msg.InternalDate).UTC().Format(time.RFC3339)
	}

	dateHeader := strings.TrimSpace(headerValue(msg.Payload, "Date"))
	if dateHeader == "" {
		return ""
	}
	if parsed, err := mail.ParseDate(dateHeader); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return ""
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		if value == "" {
			return "", false
		}
		return "", true
	}
	data := []byte(value)
	if len(data) <= maxBytes {
		return value, false
	}

	data = data[:maxBytes]
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data), true
}

func nonNilStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func nonNilAttachments(values []rpc.AttachmentMeta) []rpc.AttachmentMeta {
	if len(values) == 0 {
		return []rpc.AttachmentMeta{}
	}
	return values
}

func boolPtr(value bool) *bool {
	return &value
}
