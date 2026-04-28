package gmailapi

import (
	"encoding/base64"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestBuildDraftRawBuildsPlainTextMessage(t *testing.T) {
	t.Parallel()

	raw, err := BuildDraftRaw(DraftCreateInput{
		From:     "Owner <owner@example.com>",
		To:       []string{"Alice <alice@example.com>, bob@example.com"},
		Cc:       []string{"carol@example.com"},
		Subject:  "Hello",
		BodyText: "line 1\nline 2",
	})
	if err != nil {
		t.Fatalf("BuildDraftRaw() error = %v", err)
	}

	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	msg := string(data)
	for _, want := range []string{
		"From: owner@example.com\r\n",
		"To: alice@example.com, bob@example.com\r\n",
		"Cc: carol@example.com\r\n",
		"Subject: Hello\r\n",
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n",
		"\r\nline 1\r\nline 2",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("raw message = %q, missing %q", msg, want)
		}
	}
}

func TestDraftReplyContextFromMessageBuildsReferences(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		ThreadId: "thread-1",
		Payload: &gmail.MessagePart{
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: "Status"},
				{Name: "Message-ID", Value: "<msg-2@example.com>"},
				{Name: "References", Value: "<msg-1@example.com>"},
			},
		},
	}

	ctx := DraftReplyContextFromMessage(msg)
	if ctx.ThreadID != "thread-1" {
		t.Fatalf("ctx.ThreadID = %q, want thread-1", ctx.ThreadID)
	}
	if ctx.Subject != "Re: Status" {
		t.Fatalf("ctx.Subject = %q, want Re: Status", ctx.Subject)
	}
	if ctx.InReplyTo != "<msg-2@example.com>" {
		t.Fatalf("ctx.InReplyTo = %q, want <msg-2@example.com>", ctx.InReplyTo)
	}
	if got, want := strings.Join(ctx.References, " "), "<msg-1@example.com> <msg-2@example.com>"; got != want {
		t.Fatalf("References = %q, want %q", got, want)
	}
}

func TestDraftReplyRecipientsExcludesOwner(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		Payload: &gmail.MessagePart{
			Headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: "alice@example.com"},
				{Name: "To", Value: "owner@example.com, bob@example.com"},
				{Name: "Cc", Value: "carol@example.com"},
			},
		},
	}

	to, cc, err := DraftReplyRecipients(msg, "owner@example.com", true)
	if err != nil {
		t.Fatalf("DraftReplyRecipients() error = %v", err)
	}
	if got, want := strings.Join(to, ","), "alice@example.com,bob@example.com"; got != want {
		t.Fatalf("to = %q, want %q", got, want)
	}
	if got, want := strings.Join(cc, ","), "carol@example.com"; got != want {
		t.Fatalf("cc = %q, want %q", got, want)
	}
}
