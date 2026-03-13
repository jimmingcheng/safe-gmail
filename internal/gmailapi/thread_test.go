package gmailapi

import (
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestBuildThreadSummaryUsesOnlyVisibleMessages(t *testing.T) {
	t.Parallel()

	summary, err := BuildThreadSummary("thread-1", []*gmail.Message{
		{
			Id:           "msg-1",
			ThreadId:     "thread-1",
			Snippet:      "visible snippet",
			InternalDate: 1710267600000,
			Payload: &gmail.MessagePart{
				Headers: []*gmail.MessagePartHeader{
					{Name: "From", Value: "Alice <alice@example.com>"},
					{Name: "To", Value: "Owner <owner@example.com>"},
					{Name: "Subject", Value: "Visible"},
				},
			},
		},
		{
			Id:           "msg-2",
			ThreadId:     "thread-1",
			Snippet:      "latest visible snippet",
			InternalDate: 1710354000000,
			Payload: &gmail.MessagePart{
				Headers: []*gmail.MessagePartHeader{
					{Name: "From", Value: "Bob <bob@example.com>"},
					{Name: "To", Value: "Owner <owner@example.com>"},
					{Name: "Subject", Value: "Latest Visible"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildThreadSummary() error = %v", err)
	}

	if summary.ThreadID != "thread-1" {
		t.Fatalf("summary.ThreadID = %q, want thread-1", summary.ThreadID)
	}
	if summary.Subject != "Latest Visible" {
		t.Fatalf("summary.Subject = %q, want Latest Visible", summary.Subject)
	}
	if summary.Snippet != "latest visible snippet" {
		t.Fatalf("summary.Snippet = %q, want latest visible snippet", summary.Snippet)
	}
	if summary.VisibleMessageCount != 2 {
		t.Fatalf("summary.VisibleMessageCount = %d, want 2", summary.VisibleMessageCount)
	}
	if len(summary.Participants) != 3 {
		t.Fatalf("len(summary.Participants) = %d, want 3", len(summary.Participants))
	}
	if summary.Participants[0] != "alice@example.com" || summary.Participants[1] != "bob@example.com" || summary.Participants[2] != "owner@example.com" {
		t.Fatalf("summary.Participants = %#v, want sorted unique participants", summary.Participants)
	}
}
