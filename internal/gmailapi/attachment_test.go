package gmailapi

import (
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestFindAttachmentReturnsMatchingMetadata(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gmail.MessagePart{
			{
				Filename: "report.pdf",
				MimeType: "application/pdf",
				Body: &gmail.MessagePartBody{
					AttachmentId: "att-1",
					Size:         123,
				},
			},
		},
	}

	attachment, ok := FindAttachment(part, "att-1")
	if !ok {
		t.Fatal("FindAttachment() ok = false, want true")
	}
	if attachment.Filename != "report.pdf" {
		t.Fatalf("attachment.Filename = %q, want report.pdf", attachment.Filename)
	}
	if attachment.Size != 123 {
		t.Fatalf("attachment.Size = %d, want 123", attachment.Size)
	}
}
