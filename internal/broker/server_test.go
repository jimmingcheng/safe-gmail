package broker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
	apigoogle "google.golang.org/api/googleapi"

	"github.com/jimmingcheng/safe-gmail/internal/config"
	"github.com/jimmingcheng/safe-gmail/internal/gmailapi"
	"github.com/jimmingcheng/safe-gmail/internal/policy"
	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

func TestHandleGetMessageAuthorizesBeforeFullFetch(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "full"),
		},
		metadataErr: map[string]error{},
		fullErr:     map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicy(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-1",
		Method: rpc.MethodGmailGetMessage,
		Params: []byte(`{"message_id":"msg-1","include_body":true}`),
	})
	if resp.OK {
		t.Fatalf("dispatch() ok = true, want false")
	}
	if resp.Error == nil || resp.Error.Code != "policy_denied" {
		t.Fatalf("resp.Error = %#v, want policy_denied", resp.Error)
	}
	if len(service.fullCalls) != 0 {
		t.Fatalf("fullCalls = %#v, want none", service.fullCalls)
	}
	if len(service.metadataCalls) != 1 || service.metadataCalls[0] != "msg-1" {
		t.Fatalf("metadataCalls = %#v, want [msg-1]", service.metadataCalls)
	}
}

func TestHandleGetMessageAllowsVisibilityLabel(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		labels: map[string]string{"vip": "Label_1"},
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"Label_1"}, "full"),
		},
		metadataErr: map[string]error{},
		fullErr:     map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return &policy.Policy{
				Owner:           "owner@example.com",
				VisibilityLabel: "vip",
			}, nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-1",
		Method: rpc.MethodGmailGetMessage,
		Params: []byte(`{"message_id":"msg-1","include_body":true}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}
	if service.labelCalls != 1 {
		t.Fatalf("labelCalls = %d, want 1", service.labelCalls)
	}
	if len(service.fullCalls) != 1 || service.fullCalls[0] != "msg-1" {
		t.Fatalf("fullCalls = %#v, want [msg-1]", service.fullCalls)
	}

	var result rpc.GmailGetMessageResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if result.Message.MessageID != "msg-1" {
		t.Fatalf("result.Message.MessageID = %q, want msg-1", result.Message.MessageID)
	}
}

func TestHandleGetMessageMapsNotFound(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{},
		full:     map[string]*gmail.Message{},
		metadataErr: map[string]error{
			"msg-1": &apigoogle.Error{Code: 404},
		},
		fullErr: map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return nil, nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-1",
		Method: rpc.MethodGmailGetMessage,
		Params: []byte(`{"message_id":"msg-1"}`),
	})
	if resp.OK {
		t.Fatalf("dispatch() ok = true, want false")
	}
	if resp.Error == nil || resp.Error.Code != "not_found" {
		t.Fatalf("resp.Error = %#v, want not_found", resp.Error)
	}
}

func TestHandleSearchMessagesFiltersRestrictedAndFetchesBodiesOnlyForVisibleMessages(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchPages: map[string]gmailapi.SearchMessagesResult{
			"": {
				Messages: []*gmail.Message{
					{Id: "msg-1", ThreadId: "thread-1"},
					{Id: "msg-2", ThreadId: "thread-2"},
					{Id: "msg-3", ThreadId: "thread-3"},
				},
				NextPageToken: "page-2",
			},
			"page-2": {
				Messages: []*gmail.Message{
					{Id: "msg-4", ThreadId: "thread-4"},
				},
			},
		},
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-2": testMessage("msg-2", "thread-2", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-3": testMessage("msg-3", "thread-3", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-4": testMessage("msg-4", "thread-4", "owner@example.com", []string{"friend@example.com"}, []string{"SENT"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full body"),
			"msg-2": testMessage("msg-2", "thread-2", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full body"),
			"msg-3": testMessage("msg-3", "thread-3", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full body"),
			"msg-4": testMessage("msg-4", "thread-4", "owner@example.com", []string{"friend@example.com"}, []string{"SENT"}, "full body"),
		},
		metadataErr: map[string]error{},
		fullErr:     map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicyWithOwnerSent(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-search",
		Method: rpc.MethodGmailSearchMessages,
		Params: []byte(`{"query":"newer_than:7d","include_body":true,"limit":2}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailSearchMessagesResultDetail
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("len(result.Messages) = %d, want 2", len(result.Messages))
	}
	if result.Messages[0].MessageID != "msg-1" {
		t.Fatalf("result.Messages[0].MessageID = %q, want msg-1", result.Messages[0].MessageID)
	}
	if result.Messages[1].MessageID != "msg-2" {
		t.Fatalf("result.Messages[1].MessageID = %q, want msg-2", result.Messages[1].MessageID)
	}
	if result.NextPageToken == "" {
		t.Fatalf("result.NextPageToken = empty, want opaque broker token")
	}
	cursor, err := decodeSearchPageToken(result.NextPageToken, searchPageKindMessages, "(newer_than:7d) (label:donna OR in:sent)")
	if err != nil {
		t.Fatalf("decodeSearchPageToken() error = %v", err)
	}
	if len(cursor.PendingIDs) != 1 || cursor.PendingIDs[0] != "msg-3" {
		t.Fatalf("cursor.PendingIDs = %#v, want [msg-3]", cursor.PendingIDs)
	}
	if cursor.GmailPageToken != "page-2" {
		t.Fatalf("cursor.GmailPageToken = %q, want page-2", cursor.GmailPageToken)
	}
	if service.searchCalls != 1 {
		t.Fatalf("searchCalls = %d, want 1", service.searchCalls)
	}
	if len(service.searchPageTokens) != 1 || service.searchPageTokens[0] != "" {
		t.Fatalf("searchPageTokens = %#v, want [\"\"]", service.searchPageTokens)
	}
	if len(service.searchQueries) != 1 || service.searchQueries[0] != `(newer_than:7d) (label:donna OR in:sent)` {
		t.Fatalf("searchQueries = %#v, want filtered query", service.searchQueries)
	}
	if len(service.fullCalls) != 2 || service.fullCalls[0] != "msg-1" || service.fullCalls[1] != "msg-2" {
		t.Fatalf("fullCalls = %#v, want [msg-1 msg-2]", service.fullCalls)
	}

	resp = srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-search-next",
		Method: rpc.MethodGmailSearchMessages,
		Params: []byte(fmt.Sprintf(`{"query":"newer_than:7d","include_body":true,"limit":2,"page_token":%q}`, result.NextPageToken)),
	})
	if !resp.OK {
		t.Fatalf("dispatch(next) ok = false, want true: %#v", resp.Error)
	}

	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult(next) error = %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("len(result.Messages next) = %d, want 2", len(result.Messages))
	}
	if result.Messages[0].MessageID != "msg-3" {
		t.Fatalf("result.Messages[0].MessageID next = %q, want msg-3", result.Messages[0].MessageID)
	}
	if result.Messages[1].MessageID != "msg-4" {
		t.Fatalf("result.Messages[1].MessageID next = %q, want msg-4", result.Messages[1].MessageID)
	}
	if result.NextPageToken != "" {
		t.Fatalf("result.NextPageToken next = %q, want empty", result.NextPageToken)
	}
	if service.searchCalls != 2 {
		t.Fatalf("searchCalls = %d, want 2", service.searchCalls)
	}
	if len(service.searchPageTokens) != 2 || service.searchPageTokens[1] != "page-2" {
		t.Fatalf("searchPageTokens = %#v, want second token page-2", service.searchPageTokens)
	}
	if len(service.fullCalls) != 4 || service.fullCalls[2] != "msg-3" || service.fullCalls[3] != "msg-4" {
		t.Fatalf("fullCalls = %#v, want [msg-1 msg-2 msg-3 msg-4]", service.fullCalls)
	}
}

func TestHandleSearchThreadsOmitsHiddenThreadsAndSanitizesSummary(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchThreadPages: map[string]gmailapi.SearchThreadsResult{
			"": {
				Threads: []*gmail.Thread{
					{Id: "thread-1"},
				},
			},
		},
		threads: map[string]*gmail.Thread{
			"thread-1": {
				Id: "thread-1",
				Messages: []*gmail.Message{
					testMessageAt("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "visible snippet", 1710267600000, "Visible Subject"),
					testMessageAt("msg-2", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "hidden snippet", 1710354000000, "Hidden Subject"),
				},
			},
			"thread-2": {
				Id: "thread-2",
				Messages: []*gmail.Message{
					testMessageAt("msg-3", "thread-2", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "fully hidden", 1710440400000, "Hidden"),
				},
			},
		},
		threadErr:   map[string]error{},
		metadata:    map[string]*gmail.Message{},
		full:        map[string]*gmail.Message{},
		metadataErr: map[string]error{},
		fullErr:     map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicy(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-thread-search",
		Method: rpc.MethodGmailSearchThreads,
		Params: []byte(`{"query":"in:anywhere","limit":1}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailSearchThreadsResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if len(result.Threads) != 1 {
		t.Fatalf("len(result.Threads) = %d, want 1", len(result.Threads))
	}
	if result.NextPageToken != "" {
		t.Fatalf("result.NextPageToken = %q, want empty", result.NextPageToken)
	}
	thread := result.Threads[0]
	if thread.ThreadID != "thread-1" {
		t.Fatalf("thread.ThreadID = %q, want thread-1", thread.ThreadID)
	}
	if thread.Subject != "Visible Subject" {
		t.Fatalf("thread.Subject = %q, want Visible Subject", thread.Subject)
	}
	if thread.Snippet != "visible snippet" {
		t.Fatalf("thread.Snippet = %q, want visible snippet", thread.Snippet)
	}
	if thread.VisibleMessageCount != 1 {
		t.Fatalf("thread.VisibleMessageCount = %d, want 1", thread.VisibleMessageCount)
	}
	if len(thread.Participants) != 2 || thread.Participants[0] != "alice@example.com" || thread.Participants[1] != "owner@example.com" {
		t.Fatalf("thread.Participants = %#v, want filtered participants", thread.Participants)
	}
	if service.searchThreadCalls != 1 {
		t.Fatalf("searchThreadCalls = %d, want 1", service.searchThreadCalls)
	}
	if len(service.searchThreadPageTokens) != 1 || service.searchThreadPageTokens[0] != "" {
		t.Fatalf("searchThreadPageTokens = %#v, want [\"\"]", service.searchThreadPageTokens)
	}
	if len(service.searchThreadQueries) != 1 || service.searchThreadQueries[0] != `(in:anywhere) (label:donna)` {
		t.Fatalf("searchThreadQueries = %#v, want filtered query", service.searchThreadQueries)
	}
	if len(service.threadCalls) != 1 || service.threadCalls[0] != "thread-1" {
		t.Fatalf("threadCalls = %#v, want [thread-1]", service.threadCalls)
	}
}

func TestHandleGetThreadFiltersRestrictedMessages(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		threads: map[string]*gmail.Thread{
			"thread-1": {
				Id: "thread-1",
				Messages: []*gmail.Message{
					testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "visible"),
					testMessage("msg-2", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "hidden"),
				},
			},
		},
		threadErr:   map[string]error{},
		metadata:    map[string]*gmail.Message{},
		full:        map[string]*gmail.Message{},
		metadataErr: map[string]error{},
		fullErr:     map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicy(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-thread",
		Method: rpc.MethodGmailGetThread,
		Params: []byte(`{"thread_id":"thread-1"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailGetThreadResultSummary
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if len(result.Thread.Messages) != 1 {
		t.Fatalf("len(result.Thread.Messages) = %d, want 1", len(result.Thread.Messages))
	}
	if result.Thread.Messages[0].MessageID != "msg-1" {
		t.Fatalf("result.Thread.Messages[0].MessageID = %q, want msg-1", result.Thread.Messages[0].MessageID)
	}
	if len(service.threadCalls) != 1 || service.threadCalls[0] != "thread-1" {
		t.Fatalf("threadCalls = %#v, want [thread-1]", service.threadCalls)
	}
	if len(service.fullCalls) != 0 {
		t.Fatalf("fullCalls = %#v, want none", service.fullCalls)
	}
}

func TestHandleGetThreadReturnsPolicyDeniedWhenNoMessagesVisible(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		threads: map[string]*gmail.Thread{
			"thread-1": {
				Id: "thread-1",
				Messages: []*gmail.Message{
					testMessage("msg-1", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "hidden"),
				},
			},
		},
		threadErr:   map[string]error{},
		metadata:    map[string]*gmail.Message{},
		full:        map[string]*gmail.Message{},
		metadataErr: map[string]error{},
		fullErr:     map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicy(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-thread-hidden",
		Method: rpc.MethodGmailGetThread,
		Params: []byte(`{"thread_id":"thread-1"}`),
	})
	if resp.OK {
		t.Fatalf("dispatch() ok = true, want false")
	}
	if resp.Error == nil || resp.Error.Code != "policy_denied" {
		t.Fatalf("resp.Error = %#v, want policy_denied", resp.Error)
	}
}

func TestHandleGetAttachmentReturnsContent(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessageWithAttachment("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full", "report.pdf", "application/pdf", "att-1", 5),
		},
		attachmentData: map[string][]byte{
			attachmentKey("msg-1", "att-1"): []byte("hello"),
		},
		threadErr:     map[string]error{},
		metadataErr:   map[string]error{},
		fullErr:       map[string]error{},
		attachmentErr: map[string]error{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicy(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-attachment",
		Method: rpc.MethodGmailGetAttachment,
		Params: []byte(`{"message_id":"msg-1","attachment_id":"att-1"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailGetAttachmentResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if result.Attachment.AttachmentID != "att-1" {
		t.Fatalf("result.Attachment.AttachmentID = %q, want att-1", result.Attachment.AttachmentID)
	}
	if result.Attachment.Filename != "report.pdf" {
		t.Fatalf("result.Attachment.Filename = %q, want report.pdf", result.Attachment.Filename)
	}
	if result.Attachment.ContentBase64 != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("result.Attachment.ContentBase64 = %q, want base64 hello", result.Attachment.ContentBase64)
	}
	if len(service.attachmentCalls) != 1 || service.attachmentCalls[0] != attachmentKey("msg-1", "att-1") {
		t.Fatalf("attachmentCalls = %#v, want [msg-1:att-1]", service.attachmentCalls)
	}
}

func TestHandleGetAttachmentRejectsOversizedAttachmentBeforeDownload(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessageWithAttachment("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full", "archive.zip", "application/zip", "att-big", 65),
		},
		threadErr:      map[string]error{},
		metadataErr:    map[string]error{},
		fullErr:        map[string]error{},
		attachmentErr:  map[string]error{},
		attachmentData: map[string][]byte{},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return testResolvedVisibilityPolicy(), nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return service, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-attachment-big",
		Method: rpc.MethodGmailGetAttachment,
		Params: []byte(`{"message_id":"msg-1","attachment_id":"att-big"}`),
	})
	if resp.OK {
		t.Fatalf("dispatch() ok = true, want false")
	}
	if resp.Error == nil || resp.Error.Code != "too_large" {
		t.Fatalf("resp.Error = %#v, want too_large", resp.Error)
	}
	if len(service.attachmentCalls) != 0 {
		t.Fatalf("attachmentCalls = %#v, want none", service.attachmentCalls)
	}
}

type fakeGmailService struct {
	labels                 map[string]string
	labelErr               error
	searchThreadsResult    gmailapi.SearchThreadsResult
	searchThreadPages      map[string]gmailapi.SearchThreadsResult
	searchThreadsErr       error
	searchResult           gmailapi.SearchMessagesResult
	searchPages            map[string]gmailapi.SearchMessagesResult
	searchErr              error
	threads                map[string]*gmail.Thread
	threadErr              map[string]error
	metadata               map[string]*gmail.Message
	metadataErr            map[string]error
	full                   map[string]*gmail.Message
	fullErr                map[string]error
	attachmentData         map[string][]byte
	attachmentErr          map[string]error
	metadataCalls          []string
	fullCalls              []string
	threadCalls            []string
	attachmentCalls        []string
	searchThreadQueries    []string
	searchThreadPageTokens []string
	searchQueries          []string
	searchPageTokens       []string
	labelCalls             int
	searchThreadCalls      int
	searchCalls            int
}

func (f *fakeGmailService) ProfileEmail(context.Context) (string, error) {
	return "owner@example.com", nil
}

func (f *fakeGmailService) ListLabelNameToID(context.Context) (map[string]string, error) {
	f.labelCalls++
	return f.labels, f.labelErr
}

func (f *fakeGmailService) SearchThreads(_ context.Context, query string, _ int64, pageToken string) (gmailapi.SearchThreadsResult, error) {
	f.searchThreadCalls++
	f.searchThreadQueries = append(f.searchThreadQueries, query)
	f.searchThreadPageTokens = append(f.searchThreadPageTokens, pageToken)
	if f.searchThreadsErr != nil {
		return gmailapi.SearchThreadsResult{}, f.searchThreadsErr
	}
	if f.searchThreadPages != nil {
		if result, ok := f.searchThreadPages[pageToken]; ok {
			return result, nil
		}
		return gmailapi.SearchThreadsResult{}, nil
	}
	return f.searchThreadsResult, f.searchThreadsErr
}

func (f *fakeGmailService) SearchMessages(_ context.Context, query string, _ int64, pageToken string) (gmailapi.SearchMessagesResult, error) {
	f.searchCalls++
	f.searchQueries = append(f.searchQueries, query)
	f.searchPageTokens = append(f.searchPageTokens, pageToken)
	if f.searchErr != nil {
		return gmailapi.SearchMessagesResult{}, f.searchErr
	}
	if f.searchPages != nil {
		if result, ok := f.searchPages[pageToken]; ok {
			return result, nil
		}
		return gmailapi.SearchMessagesResult{}, nil
	}
	return f.searchResult, f.searchErr
}

func (f *fakeGmailService) GetMessageMetadata(_ context.Context, messageID string) (*gmail.Message, error) {
	f.metadataCalls = append(f.metadataCalls, messageID)
	if err := f.metadataErr[messageID]; err != nil {
		return nil, err
	}
	return f.metadata[messageID], nil
}

func (f *fakeGmailService) GetMessageFull(_ context.Context, messageID string) (*gmail.Message, error) {
	f.fullCalls = append(f.fullCalls, messageID)
	if err := f.fullErr[messageID]; err != nil {
		return nil, err
	}
	return f.full[messageID], nil
}

func (f *fakeGmailService) GetThreadMetadata(_ context.Context, threadID string) (*gmail.Thread, error) {
	f.threadCalls = append(f.threadCalls, threadID)
	if err := f.threadErr[threadID]; err != nil {
		return nil, err
	}
	return f.threads[threadID], nil
}

func (f *fakeGmailService) GetAttachmentData(_ context.Context, messageID, attachmentID string) ([]byte, error) {
	key := attachmentKey(messageID, attachmentID)
	f.attachmentCalls = append(f.attachmentCalls, key)
	if err := f.attachmentErr[key]; err != nil {
		return nil, err
	}
	return f.attachmentData[key], nil
}

func testConfig() config.Config {
	return config.Config{
		Instance:           "work",
		AccountEmail:       "owner@example.com",
		ClientUID:          501,
		SocketPath:         "/tmp/safe-gmail.sock",
		MaxBodyBytes:       64,
		MaxAttachmentBytes: 64,
		MaxSearchResults:   100,
		OAuthClientPath:    "/tmp/oauth-client.json",
		PolicyPath:         "/tmp/policy.json",
	}
}

func testResolvedVisibilityPolicy() *policy.Policy {
	return &policy.Policy{
		Owner:             "owner@example.com",
		VisibilityLabel:   "donna",
		VisibilityLabelID: "Label_1",
	}
}

func testResolvedVisibilityPolicyWithOwnerSent() *policy.Policy {
	p := testResolvedVisibilityPolicy()
	p.AllowOwnerSent = true
	return p
}

func testMessage(id, threadID, from string, to []string, labels []string, body string) *gmail.Message {
	return testMessageAt(id, threadID, from, to, labels, body, 1710267600000, "Status")
}

func testMessageAt(id, threadID, from string, to []string, labels []string, body string, internalDate int64, subject string) *gmail.Message {
	return &gmail.Message{
		Id:           id,
		ThreadId:     threadID,
		Snippet:      body,
		InternalDate: internalDate,
		LabelIds:     labels,
		Payload: &gmail.MessagePart{
			Headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: from},
				{Name: "To", Value: joinAddresses(to)},
				{Name: "Subject", Value: subject},
			},
			Parts: []*gmail.MessagePart{
				{
					MimeType: "text/plain",
					Body: &gmail.MessagePartBody{
						Data: "aGVsbG8",
					},
				},
			},
		},
	}
}

func testMessageWithAttachment(id, threadID, from string, to []string, labels []string, body, filename, mimeType, attachmentID string, attachmentSize int64) *gmail.Message {
	msg := testMessage(id, threadID, from, to, labels, body)
	msg.Payload.Parts = append(msg.Payload.Parts, &gmail.MessagePart{
		Filename: filename,
		MimeType: mimeType,
		Body: &gmail.MessagePartBody{
			AttachmentId: attachmentID,
			Size:         attachmentSize,
		},
	})
	return msg
}

func attachmentKey(messageID, attachmentID string) string {
	return messageID + ":" + attachmentID
}

func joinAddresses(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ", ")
}

func decodeResult(value any, dst any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
