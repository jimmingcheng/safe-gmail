package rpc

import (
	"encoding/json"
	"fmt"
	"strings"
)

const Version1 = 1

const (
	MethodSystemPing          = "system.ping"
	MethodSystemInfo          = "system.info"
	MethodGmailListLabels     = "gmail.list_labels"
	MethodGmailSearchThreads  = "gmail.search_threads"
	MethodGmailSearchMessages = "gmail.search_messages"
	MethodGmailGetMessage     = "gmail.get_message"
	MethodGmailGetThread      = "gmail.get_thread"
	MethodGmailGetAttachment  = "gmail.get_attachment"
	MethodGmailCreateDraft    = "gmail.create_draft"
)

// Request is a single RPC request envelope.
type Request struct {
	V      int             `json:"v"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Response is a single RPC response envelope.
type Response struct {
	V      int        `json:"v"`
	ID     string     `json:"id"`
	OK     bool       `json:"ok"`
	Result any        `json:"result,omitempty"`
	Error  *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is the machine-readable error portion of a failed response.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// SystemInfo is the typed payload returned by system.info.
type SystemInfo struct {
	Service            string   `json:"service"`
	ProtocolVersion    int      `json:"protocol_version"`
	Instance           string   `json:"instance"`
	AccountEmail       string   `json:"account_email"`
	MaxBodyBytes       int      `json:"max_body_bytes"`
	MaxAttachmentBytes int      `json:"max_attachment_bytes"`
	MaxSearchResults   int      `json:"max_search_results"`
	SearchQuerySyntax  string   `json:"search_query_syntax,omitempty"`
	LabelQueryMode     string   `json:"label_query_mode,omitempty"`
	LabelListMethod    string   `json:"label_list_method,omitempty"`
	LabelListScope     string   `json:"label_list_scope,omitempty"`
	Methods            []string `json:"methods"`
}

// LabelInfo is the broker-owned Gmail label metadata shape.
type LabelInfo struct {
	LabelID               string `json:"label_id"`
	LabelName             string `json:"label_name"`
	LabelType             string `json:"label_type,omitempty"`
	LabelListVisibility   string `json:"label_list_visibility,omitempty"`
	MessageListVisibility string `json:"message_list_visibility,omitempty"`
	MessagesTotal         int64  `json:"messages_total"`
	MessagesUnread        int64  `json:"messages_unread"`
	ThreadsTotal          int64  `json:"threads_total"`
	ThreadsUnread         int64  `json:"threads_unread"`
}

// AttachmentMeta is the exposed attachment metadata for a message.
type AttachmentMeta struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size"`
}

// AttachmentContent is the broker-owned attachment payload shape.
type AttachmentContent struct {
	AttachmentMeta
	ContentBase64 string `json:"content_base64"`
}

// ThreadSummary is the broker-owned thread summary shape.
type ThreadSummary struct {
	ThreadID            string   `json:"thread_id"`
	Subject             string   `json:"subject"`
	Participants        []string `json:"participants"`
	Snippet             string   `json:"snippet"`
	VisibleMessageCount int      `json:"visible_message_count"`
	LastMessageAt       string   `json:"last_message_at"`
}

// MessageSummary is the broker-owned message summary shape.
type MessageSummary struct {
	MessageID  string   `json:"message_id"`
	ThreadID   string   `json:"thread_id"`
	From       string   `json:"from"`
	To         []string `json:"to"`
	Cc         []string `json:"cc"`
	Bcc        []string `json:"bcc"`
	Subject    string   `json:"subject"`
	Snippet    string   `json:"snippet"`
	ReceivedAt string   `json:"received_at"`
	LabelIDs   []string `json:"label_ids"`
}

// MessageDetail is the broker-owned message detail shape for gmail methods.
type MessageDetail struct {
	MessageSummary
	BodyText      string           `json:"body_text,omitempty"`
	BodyTruncated *bool            `json:"body_truncated,omitempty"`
	Attachments   []AttachmentMeta `json:"attachments"`
}

// DraftSummary is the broker-owned draft shape returned after draft creation.
type DraftSummary struct {
	DraftID   string   `json:"draft_id"`
	MessageID string   `json:"message_id"`
	ThreadID  string   `json:"thread_id"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	Subject   string   `json:"subject"`
}

// ThreadDetailSummary is the thread detail result without bodies.
type ThreadDetailSummary struct {
	ThreadID string           `json:"thread_id"`
	Messages []MessageSummary `json:"messages"`
}

// ThreadDetail is the thread detail result with bodies.
type ThreadDetail struct {
	ThreadID string          `json:"thread_id"`
	Messages []MessageDetail `json:"messages"`
}

// GmailSearchMessagesParams is the request payload for gmail.search_messages.
type GmailSearchMessagesParams struct {
	Query              string `json:"query"`
	Limit              int    `json:"limit,omitempty"`
	PageToken          string `json:"page_token,omitempty"`
	IncludeBody        bool   `json:"include_body,omitempty"`
	IncludeAttachments bool   `json:"include_attachments,omitempty"`
}

// GmailSearchThreadsParams is the request payload for gmail.search_threads.
type GmailSearchThreadsParams struct {
	Query     string `json:"query"`
	Limit     int    `json:"limit,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

// GmailListLabelsParams is the request payload for gmail.list_labels.
type GmailListLabelsParams struct{}

// GmailGetMessageParams is the request payload for gmail.get_message.
type GmailGetMessageParams struct {
	MessageID   string `json:"message_id"`
	IncludeBody bool   `json:"include_body,omitempty"`
}

// GmailGetThreadParams is the request payload for gmail.get_thread.
type GmailGetThreadParams struct {
	ThreadID      string `json:"thread_id"`
	IncludeBodies bool   `json:"include_bodies,omitempty"`
}

// GmailGetAttachmentParams is the request payload for gmail.get_attachment.
type GmailGetAttachmentParams struct {
	MessageID    string `json:"message_id"`
	AttachmentID string `json:"attachment_id"`
}

// GmailCreateDraftParams is the request payload for gmail.create_draft.
type GmailCreateDraftParams struct {
	To               []string `json:"to,omitempty"`
	Cc               []string `json:"cc,omitempty"`
	Bcc              []string `json:"bcc,omitempty"`
	Subject          string   `json:"subject,omitempty"`
	BodyText         string   `json:"body_text,omitempty"`
	ReplyToMessageID string   `json:"reply_to_message_id,omitempty"`
	ReplyToThreadID  string   `json:"reply_to_thread_id,omitempty"`
	ReplyAll         bool     `json:"reply_all,omitempty"`
}

// GmailSearchMessagesResultSummary is the result payload for gmail.search_messages without bodies.
type GmailSearchMessagesResultSummary struct {
	Messages      []MessageSummary `json:"messages"`
	NextPageToken string           `json:"next_page_token"`
}

// GmailSearchMessagesResultDetail is the result payload for gmail.search_messages
// when attachment metadata and/or bodies are requested.
type GmailSearchMessagesResultDetail struct {
	Messages      []MessageDetail `json:"messages"`
	NextPageToken string          `json:"next_page_token"`
}

// GmailSearchThreadsResult is the result payload for gmail.search_threads.
type GmailSearchThreadsResult struct {
	Threads       []ThreadSummary `json:"threads"`
	NextPageToken string          `json:"next_page_token"`
}

// GmailListLabelsResult is the result payload for gmail.list_labels.
type GmailListLabelsResult struct {
	Labels []LabelInfo `json:"labels"`
}

// GmailGetMessageResult is the result payload for gmail.get_message.
type GmailGetMessageResult struct {
	Message MessageDetail `json:"message"`
}

// GmailGetThreadResultSummary is the result payload for gmail.get_thread without bodies.
type GmailGetThreadResultSummary struct {
	Thread ThreadDetailSummary `json:"thread"`
}

// GmailGetThreadResultDetail is the result payload for gmail.get_thread with bodies.
type GmailGetThreadResultDetail struct {
	Thread ThreadDetail `json:"thread"`
}

// GmailGetAttachmentResult is the result payload for gmail.get_attachment.
type GmailGetAttachmentResult struct {
	Attachment AttachmentContent `json:"attachment"`
}

// GmailCreateDraftResult is the result payload for gmail.create_draft.
type GmailCreateDraftResult struct {
	Draft DraftSummary `json:"draft"`
}

// Validate enforces method-specific invariants.
func (p GmailSearchMessagesParams) Validate() error {
	if p.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	return nil
}

// Validate enforces method-specific invariants.
func (p GmailSearchThreadsParams) Validate() error {
	if p.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	return nil
}

// Validate enforces method-specific invariants.
func (p GmailListLabelsParams) Validate() error {
	return nil
}

// Validate enforces method-specific invariants.
func (p GmailGetMessageParams) Validate() error {
	if strings.TrimSpace(p.MessageID) == "" {
		return fmt.Errorf("missing message_id")
	}
	return nil
}

// Validate enforces method-specific invariants.
func (p GmailGetThreadParams) Validate() error {
	if strings.TrimSpace(p.ThreadID) == "" {
		return fmt.Errorf("missing thread_id")
	}
	return nil
}

// Validate enforces method-specific invariants.
func (p GmailGetAttachmentParams) Validate() error {
	if strings.TrimSpace(p.MessageID) == "" {
		return fmt.Errorf("missing message_id")
	}
	if strings.TrimSpace(p.AttachmentID) == "" {
		return fmt.Errorf("missing attachment_id")
	}
	return nil
}

// Validate enforces method-specific invariants.
func (p GmailCreateDraftParams) Validate() error {
	replyToMessage := strings.TrimSpace(p.ReplyToMessageID)
	replyToThread := strings.TrimSpace(p.ReplyToThreadID)
	if replyToMessage != "" && replyToThread != "" {
		return fmt.Errorf("set only one of reply_to_message_id or reply_to_thread_id")
	}
	if p.ReplyAll && replyToMessage == "" && replyToThread == "" {
		return fmt.Errorf("reply_all requires reply_to_message_id or reply_to_thread_id")
	}
	if strings.TrimSpace(p.Subject) == "" && strings.TrimSpace(p.BodyText) == "" && len(p.To) == 0 && len(p.Cc) == 0 && len(p.Bcc) == 0 {
		return fmt.Errorf("draft must include a recipient, subject, or body_text")
	}
	return nil
}

func NewSuccess(id string, result any) Response {
	return Response{
		V:      Version1,
		ID:     id,
		OK:     true,
		Result: result,
	}
}

func NewError(id, code, message string, retryable bool) Response {
	return Response{
		V:  Version1,
		ID: id,
		OK: false,
		Error: &ErrorBody{
			Code:      code,
			Message:   message,
			Retryable: retryable,
		},
	}
}
