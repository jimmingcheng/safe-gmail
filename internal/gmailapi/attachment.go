package gmailapi

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/api/gmail/v1"

	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

const inlineAttachmentIDPrefix = "sg-inline:"

// AttachmentRef is one broker-visible attachment plus enough information to
// resolve its bytes without exposing Gmail internals on the wire.
type AttachmentRef struct {
	Meta           rpc.AttachmentMeta
	inlineBodyData string
}

// IsInline reports whether the attachment bytes are carried directly on the
// message payload instead of requiring a Gmail attachments.get round-trip.
func (a AttachmentRef) IsInline() bool {
	return strings.TrimSpace(a.inlineBodyData) != ""
}

// Data decodes the attachment bytes for inline payload-backed attachments.
func (a AttachmentRef) Data() ([]byte, error) {
	if !a.IsInline() {
		return nil, fmt.Errorf("attachment data is not inline")
	}
	data, err := decodeBase64Data(a.inlineBodyData)
	if err != nil {
		return nil, fmt.Errorf("decode inline attachment: %w", err)
	}
	return data, nil
}

// FindAttachment locates one attachment entry by attachment ID.
func FindAttachment(part *gmail.MessagePart, attachmentID string) (AttachmentRef, bool) {
	attachmentID = strings.TrimSpace(attachmentID)
	if attachmentID == "" {
		return AttachmentRef{}, false
	}

	for _, attachment := range collectAttachmentRefs(part, nil) {
		if attachment.Meta.AttachmentID == attachmentID {
			return attachment, true
		}
	}
	return AttachmentRef{}, false
}

func collectAttachments(part *gmail.MessagePart) []rpc.AttachmentMeta {
	refs := collectAttachmentRefs(part, nil)
	if len(refs) == 0 {
		return nil
	}

	result := make([]rpc.AttachmentMeta, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref.Meta)
	}
	return result
}

func collectAttachmentRefs(part *gmail.MessagePart, path []int) []AttachmentRef {
	if part == nil {
		return nil
	}

	var result []AttachmentRef
	if attachment, ok := attachmentRef(part, path); ok {
		result = append(result, attachment)
	}
	for idx, child := range part.Parts {
		childPath := append(append([]int(nil), path...), idx)
		result = append(result, collectAttachmentRefs(child, childPath)...)
	}
	return result
}

func attachmentRef(part *gmail.MessagePart, path []int) (AttachmentRef, bool) {
	if !isAttachmentPart(part) {
		return AttachmentRef{}, false
	}

	body := part.Body
	attachmentID := strings.TrimSpace(body.AttachmentId)
	ref := AttachmentRef{
		Meta: rpc.AttachmentMeta{
			AttachmentID: attachmentID,
			Filename:     attachmentFilename(part),
			MimeType:     strings.TrimSpace(part.MimeType),
			Size:         body.Size,
		},
	}
	if attachmentID == "" {
		ref.Meta.AttachmentID = inlineAttachmentID(path)
		ref.inlineBodyData = strings.TrimSpace(body.Data)
	}
	return ref, true
}

func isAttachmentPart(part *gmail.MessagePart) bool {
	if part == nil || part.Body == nil {
		return false
	}
	if strings.TrimSpace(part.Body.AttachmentId) != "" {
		return true
	}
	if strings.TrimSpace(part.Body.Data) == "" {
		return false
	}
	if strings.TrimSpace(part.Filename) != "" {
		return true
	}

	disposition := strings.ToLower(strings.TrimSpace(headerValue(part, "Content-Disposition")))
	switch {
	case strings.HasPrefix(disposition, "attachment"):
		return true
	case strings.HasPrefix(disposition, "inline") && !isMessageBodyPart(part):
		return true
	case strings.TrimSpace(headerValue(part, "Content-ID")) != "" && !isMessageBodyPart(part):
		return true
	default:
		return false
	}
}

func isMessageBodyPart(part *gmail.MessagePart) bool {
	mimeType := strings.ToLower(strings.TrimSpace(part.MimeType))
	return mimeType == "text/plain" || mimeType == "text/html"
}

func attachmentFilename(part *gmail.MessagePart) string {
	filename := strings.TrimSpace(part.Filename)
	if filename == "" {
		return "attachment"
	}
	return filename
}

func inlineAttachmentID(path []int) string {
	if len(path) == 0 {
		return inlineAttachmentIDPrefix + "root"
	}

	parts := make([]string, 0, len(path))
	for _, idx := range path {
		parts = append(parts, strconv.Itoa(idx))
	}
	return inlineAttachmentIDPrefix + strings.Join(parts, ".")
}
