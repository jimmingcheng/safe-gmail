package gmailapi

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/api/gmail/v1"

	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

// BuildThreadSummary converts visible Gmail thread messages into the RPC summary shape.
func BuildThreadSummary(threadID string, messages []*gmail.Message) (rpc.ThreadSummary, error) {
	if len(messages) == 0 {
		return rpc.ThreadSummary{}, fmt.Errorf("missing visible thread messages")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = strings.TrimSpace(messages[0].ThreadId)
	}
	if threadID == "" {
		return rpc.ThreadSummary{}, fmt.Errorf("missing thread id")
	}

	newest := newestMessage(messages)
	if newest == nil {
		return rpc.ThreadSummary{}, fmt.Errorf("missing visible thread messages")
	}

	participants := collectParticipants(messages)
	sort.Strings(participants)

	return rpc.ThreadSummary{
		ThreadID:            threadID,
		Subject:             strings.TrimSpace(headerValue(newest.Payload, "Subject")),
		Participants:        participants,
		Snippet:             strings.TrimSpace(newest.Snippet),
		VisibleMessageCount: len(messages),
		LastMessageAt:       receivedAt(newest),
	}, nil
}

func newestMessage(messages []*gmail.Message) *gmail.Message {
	var newest *gmail.Message
	var newestStamp string
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		stamp := receivedAt(msg)
		if newest == nil || stamp > newestStamp {
			newest = msg
			newestStamp = stamp
		}
	}
	return newest
}

func collectParticipants(messages []*gmail.Message) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)
	appendIfNew := func(email string) {
		email = strings.TrimSpace(email)
		if email == "" || seen[email] {
			return
		}
		seen[email] = true
		result = append(result, email)
	}

	for _, msg := range messages {
		parts := MessageParticipants(msg)
		appendIfNew(parts.From)
		for _, email := range parts.To {
			appendIfNew(email)
		}
		for _, email := range parts.Cc {
			appendIfNew(email)
		}
		for _, email := range parts.Bcc {
			appendIfNew(email)
		}
	}

	return result
}
