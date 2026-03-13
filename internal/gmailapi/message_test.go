package gmailapi

import (
	"encoding/base64"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestBuildMessageDetailShapesBodyAndAttachments(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		Id:           "msg-1",
		ThreadId:     "thread-1",
		Snippet:      "hello",
		InternalDate: 1710267600000,
		LabelIds:     []string{"INBOX"},
		Payload: &gmail.MessagePart{
			MimeType: "multipart/mixed",
			Headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: "Alice <alice@example.com>"},
				{Name: "To", Value: "Owner <owner@example.com>"},
				{Name: "Subject", Value: "Status"},
			},
			Parts: []*gmail.MessagePart{
				{
					MimeType: "text/plain",
					Body: &gmail.MessagePartBody{
						Data: encodeBody("hello world"),
					},
				},
				{
					Filename: "report.pdf",
					MimeType: "application/pdf",
					Body: &gmail.MessagePartBody{
						AttachmentId: "att-1",
						Size:         12345,
					},
				},
			},
		},
	}

	detail, err := BuildMessageDetail(msg, true, 5)
	if err != nil {
		t.Fatalf("BuildMessageDetail() error = %v", err)
	}

	if detail.From != "alice@example.com" {
		t.Fatalf("detail.From = %q, want alice@example.com", detail.From)
	}
	if len(detail.To) != 1 || detail.To[0] != "owner@example.com" {
		t.Fatalf("detail.To = %#v, want [owner@example.com]", detail.To)
	}
	if detail.BodyText != "hello" {
		t.Fatalf("detail.BodyText = %q, want hello", detail.BodyText)
	}
	if detail.BodyTruncated == nil || !*detail.BodyTruncated {
		t.Fatalf("detail.BodyTruncated = %#v, want true", detail.BodyTruncated)
	}
	if len(detail.Attachments) != 1 || detail.Attachments[0].AttachmentID != "att-1" {
		t.Fatalf("detail.Attachments = %#v, want one attachment", detail.Attachments)
	}
	if detail.ReceivedAt == "" {
		t.Fatal("detail.ReceivedAt is empty")
	}
}

func TestBuildMessageSummaryKeepsBaseFields(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		Id:           "msg-2",
		ThreadId:     "thread-1",
		Snippet:      "hello",
		InternalDate: 1710267600000,
		LabelIds:     []string{"INBOX"},
		Payload: &gmail.MessagePart{
			Headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: "Alice <alice@example.com>"},
				{Name: "To", Value: "Owner <owner@example.com>"},
				{Name: "Subject", Value: "Status"},
			},
		},
	}

	summary, err := BuildMessageSummary(msg)
	if err != nil {
		t.Fatalf("BuildMessageSummary() error = %v", err)
	}

	if summary.MessageID != "msg-2" {
		t.Fatalf("summary.MessageID = %q, want msg-2", summary.MessageID)
	}
	if summary.From != "alice@example.com" {
		t.Fatalf("summary.From = %q, want alice@example.com", summary.From)
	}
	if len(summary.To) != 1 || summary.To[0] != "owner@example.com" {
		t.Fatalf("summary.To = %#v, want [owner@example.com]", summary.To)
	}
}

func encodeBody(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
