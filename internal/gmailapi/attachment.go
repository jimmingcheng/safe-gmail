package gmailapi

import (
	"strings"

	"google.golang.org/api/gmail/v1"

	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

// FindAttachment locates one attachment metadata entry by attachment ID.
func FindAttachment(part *gmail.MessagePart, attachmentID string) (rpc.AttachmentMeta, bool) {
	attachmentID = strings.TrimSpace(attachmentID)
	if attachmentID == "" {
		return rpc.AttachmentMeta{}, false
	}

	for _, attachment := range collectAttachments(part) {
		if attachment.AttachmentID == attachmentID {
			return attachment, true
		}
	}
	return rpc.AttachmentMeta{}, false
}
