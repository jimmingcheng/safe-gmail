package gmailapi

import (
	"encoding/base64"
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
	if attachment.Meta.Filename != "report.pdf" {
		t.Fatalf("attachment.Meta.Filename = %q, want report.pdf", attachment.Meta.Filename)
	}
	if attachment.Meta.Size != 123 {
		t.Fatalf("attachment.Meta.Size = %d, want 123", attachment.Meta.Size)
	}
}

func TestFindAttachmentReturnsInlineAttachment(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "multipart/related",
		Parts: []*gmail.MessagePart{
			{
				MimeType: "text/html",
				Body: &gmail.MessagePartBody{
					Data: base64.RawURLEncoding.EncodeToString([]byte("<p>hello</p>")),
				},
			},
			{
				MimeType: "image/png",
				Headers: []*gmail.MessagePartHeader{
					{Name: "Content-Disposition", Value: "inline"},
					{Name: "Content-ID", Value: "<cid-1>"},
				},
				Body: &gmail.MessagePartBody{
					Data: base64.RawURLEncoding.EncodeToString([]byte("png-bytes")),
					Size: 9,
				},
			},
		},
	}

	attachment, ok := FindAttachment(part, "sg-inline:1")
	if !ok {
		t.Fatal("FindAttachment() ok = false, want true")
	}
	if attachment.Meta.MimeType != "image/png" {
		t.Fatalf("attachment.Meta.MimeType = %q, want image/png", attachment.Meta.MimeType)
	}
	if attachment.Meta.Filename != "attachment" {
		t.Fatalf("attachment.Meta.Filename = %q, want attachment", attachment.Meta.Filename)
	}
	if !attachment.IsInline() {
		t.Fatal("attachment.IsInline() = false, want true")
	}
	data, err := attachment.Data()
	if err != nil {
		t.Fatalf("attachment.Data() error = %v", err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("attachment.Data() = %q, want png-bytes", data)
	}
}
