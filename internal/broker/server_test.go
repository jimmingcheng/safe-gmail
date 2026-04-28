package broker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
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

func TestHandleListLabelsReturnsMailboxLabels(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		labelList: []gmailapi.Label{
			{
				ID:                    "Label_2",
				Name:                  "Projects/Alpha",
				Type:                  "user",
				LabelListVisibility:   "labelShowIfUnread",
				MessageListVisibility: "hide",
				MessagesTotal:         12,
				MessagesUnread:        2,
				ThreadsTotal:          9,
				ThreadsUnread:         1,
			},
			{
				ID:                    "INBOX",
				Name:                  "INBOX",
				Type:                  "system",
				LabelListVisibility:   "labelShow",
				MessageListVisibility: "show",
				MessagesTotal:         30,
				MessagesUnread:        4,
				ThreadsTotal:          20,
				ThreadsUnread:         3,
			},
			{
				ID:                    "Label_1",
				Name:                  "vip",
				Type:                  "user",
				LabelListVisibility:   "labelShow",
				MessageListVisibility: "show",
				MessagesTotal:         3,
				MessagesUnread:        1,
				ThreadsTotal:          2,
				ThreadsUnread:         1,
			},
		},
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
		ID:     "req-labels",
		Method: rpc.MethodGmailListLabels,
		Params: []byte(`{}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailListLabelsResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if len(result.Labels) != 3 {
		t.Fatalf("len(result.Labels) = %d, want 3", len(result.Labels))
	}
	if result.Labels[0].LabelID != "INBOX" || result.Labels[0].LabelName != "INBOX" || result.Labels[0].MessagesTotal != 30 {
		t.Fatalf("result.Labels[0] = %#v, want INBOX", result.Labels[0])
	}
	if result.Labels[1].LabelID != "Label_2" || result.Labels[1].LabelName != "Projects/Alpha" || result.Labels[1].LabelListVisibility != "labelShowIfUnread" {
		t.Fatalf("result.Labels[1] = %#v, want Projects/Alpha", result.Labels[1])
	}
	if result.Labels[2].LabelID != "Label_1" || result.Labels[2].LabelName != "vip" || result.Labels[2].MessagesUnread != 1 {
		t.Fatalf("result.Labels[2] = %#v, want vip", result.Labels[2])
	}
	if service.labelCalls != 1 {
		t.Fatalf("labelCalls = %d, want 1", service.labelCalls)
	}
}

func TestHandleListLabelsBypassesVisibilityLabelResolution(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		labelList: []gmailapi.Label{
			{ID: "Label_2", Name: "Projects/Alpha", Type: "user"},
		},
	}

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return &policy.Policy{
				Owner:           "owner@example.com",
				VisibilityLabel: "missing-label",
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
		ID:     "req-labels",
		Method: rpc.MethodGmailListLabels,
		Params: []byte(`{}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}
	if service.labelCalls != 1 {
		t.Fatalf("labelCalls = %d, want 1", service.labelCalls)
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
	cursor, err := decodeSearchPageToken(result.NextPageToken, searchPageKindMessages, "(in:anywhere) (newer_than:7d) (label:donna OR in:sent)")
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
	if len(service.searchQueries) != 1 || service.searchQueries[0] != `(in:anywhere) (newer_than:7d) (label:donna OR in:sent)` {
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

func TestHandleSearchMessagesStopsOnRepeatedNextPageToken(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchPages: map[string]gmailapi.SearchMessagesResult{
			"": {
				Messages: []*gmail.Message{
					{Id: "msg-1", ThreadId: "thread-1"},
				},
				NextPageToken: "page-2",
			},
			"page-2": {
				Messages: []*gmail.Message{
					{Id: "msg-1", ThreadId: "thread-1"},
				},
				NextPageToken: "page-2",
			},
		},
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
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
		ID:     "req-message-repeat",
		Method: rpc.MethodGmailSearchMessages,
		Params: []byte(`{"query":"in:anywhere","limit":2}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailSearchMessagesResultSummary
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("len(result.Messages) = %d, want 1", len(result.Messages))
	}
	if result.Messages[0].MessageID != "msg-1" {
		t.Fatalf("result.Messages[0].MessageID = %q, want msg-1", result.Messages[0].MessageID)
	}
	if result.NextPageToken != "" {
		t.Fatalf("result.NextPageToken = %q, want empty", result.NextPageToken)
	}
	if service.searchCalls != 2 {
		t.Fatalf("searchCalls = %d, want 2", service.searchCalls)
	}
	if len(service.searchPageTokens) != 2 || service.searchPageTokens[0] != "" || service.searchPageTokens[1] != "page-2" {
		t.Fatalf("searchPageTokens = %#v, want [\"\" \"page-2\"]", service.searchPageTokens)
	}
	if len(service.metadataCalls) != 1 || service.metadataCalls[0] != "msg-1" {
		t.Fatalf("metadataCalls = %#v, want [msg-1]", service.metadataCalls)
	}
	if len(service.fullCalls) != 0 {
		t.Fatalf("fullCalls = %#v, want none", service.fullCalls)
	}
}

func TestHandleSearchMessagesIncludesAttachmentsWithoutBodies(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchResult: gmailapi.SearchMessagesResult{
			Messages: []*gmail.Message{
				{Id: "msg-1", ThreadId: "thread-1"},
				{Id: "msg-2", ThreadId: "thread-2"},
			},
		},
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-2": testMessage("msg-2", "thread-2", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessageWithAttachment("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full body", "report.pdf", "application/pdf", "att-1", 5),
			"msg-2": testMessage("msg-2", "thread-2", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full body"),
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
		ID:     "req-search-attachments",
		Method: rpc.MethodGmailSearchMessages,
		Params: []byte(`{"query":"has:attachment","include_attachments":true,"limit":2}`),
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
	if len(result.Messages[0].Attachments) != 1 || result.Messages[0].Attachments[0].AttachmentID != "sg-part:1" {
		t.Fatalf("result.Messages[0].Attachments = %#v, want [sg-part:1]", result.Messages[0].Attachments)
	}
	if result.Messages[0].BodyTruncated != nil || result.Messages[0].BodyText != "" {
		t.Fatalf("result.Messages[0] body = %#v, want no body fields", result.Messages[0])
	}
	if len(result.Messages[1].Attachments) != 0 {
		t.Fatalf("result.Messages[1].Attachments = %#v, want empty", result.Messages[1].Attachments)
	}
	if len(service.fullCalls) != 2 || service.fullCalls[0] != "msg-1" || service.fullCalls[1] != "msg-2" {
		t.Fatalf("fullCalls = %#v, want [msg-1 msg-2]", service.fullCalls)
	}
}

func TestHandleSearchMessagesDeduplicatesReturnedIDsAcrossBrokerPages(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchPages: map[string]gmailapi.SearchMessagesResult{
			"": {
				Messages: []*gmail.Message{
					{Id: "msg-1", ThreadId: "thread-1"},
					{Id: "msg-1", ThreadId: "thread-1"},
					{Id: "msg-2", ThreadId: "thread-2"},
					{Id: "msg-2", ThreadId: "thread-2"},
					{Id: "msg-3", ThreadId: "thread-3"},
					{Id: "msg-3", ThreadId: "thread-3"},
				},
				NextPageToken: "page-2",
			},
			"page-2": {
				Messages: []*gmail.Message{
					{Id: "msg-1", ThreadId: "thread-1"},
					{Id: "msg-2", ThreadId: "thread-2"},
					{Id: "msg-3", ThreadId: "thread-3"},
					{Id: "msg-4", ThreadId: "thread-4"},
					{Id: "msg-4", ThreadId: "thread-4"},
				},
			},
		},
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-2": testMessage("msg-2", "thread-2", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-3": testMessage("msg-3", "thread-3", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
			"msg-4": testMessage("msg-4", "thread-4", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
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
		ID:     "req-search-dedupe-1",
		Method: rpc.MethodGmailSearchMessages,
		Params: []byte(`{"query":"in:anywhere","limit":2}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch(first) ok = false, want true: %#v", resp.Error)
	}

	var first rpc.GmailSearchMessagesResultSummary
	if err := decodeResult(resp.Result, &first); err != nil {
		t.Fatalf("decodeResult(first) error = %v", err)
	}
	if len(first.Messages) != 2 {
		t.Fatalf("len(first.Messages) = %d, want 2", len(first.Messages))
	}
	if first.Messages[0].MessageID != "msg-1" || first.Messages[1].MessageID != "msg-2" {
		t.Fatalf("first.Messages = %#v, want [msg-1 msg-2]", first.Messages)
	}
	if first.NextPageToken == "" {
		t.Fatalf("first.NextPageToken = empty, want opaque broker token")
	}

	cursor, err := decodeSearchPageToken(first.NextPageToken, searchPageKindMessages, "(in:anywhere) (label:donna)")
	if err != nil {
		t.Fatalf("decodeSearchPageToken(first) error = %v", err)
	}
	if len(cursor.PendingIDs) != 1 || cursor.PendingIDs[0] != "msg-3" {
		t.Fatalf("cursor.PendingIDs = %#v, want [msg-3]", cursor.PendingIDs)
	}
	if len(cursor.ReturnedIDs) != 2 || cursor.ReturnedIDs[0] != "msg-1" || cursor.ReturnedIDs[1] != "msg-2" {
		t.Fatalf("cursor.ReturnedIDs = %#v, want [msg-1 msg-2]", cursor.ReturnedIDs)
	}
	if cursor.GmailPageToken != "page-2" {
		t.Fatalf("cursor.GmailPageToken = %q, want page-2", cursor.GmailPageToken)
	}

	resp = srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-search-dedupe-2",
		Method: rpc.MethodGmailSearchMessages,
		Params: []byte(fmt.Sprintf(`{"query":"in:anywhere","limit":2,"page_token":%q}`, first.NextPageToken)),
	})
	if !resp.OK {
		t.Fatalf("dispatch(second) ok = false, want true: %#v", resp.Error)
	}

	var second rpc.GmailSearchMessagesResultSummary
	if err := decodeResult(resp.Result, &second); err != nil {
		t.Fatalf("decodeResult(second) error = %v", err)
	}
	if len(second.Messages) != 2 {
		t.Fatalf("len(second.Messages) = %d, want 2", len(second.Messages))
	}
	if second.Messages[0].MessageID != "msg-3" || second.Messages[1].MessageID != "msg-4" {
		t.Fatalf("second.Messages = %#v, want [msg-3 msg-4]", second.Messages)
	}
	if second.NextPageToken != "" {
		t.Fatalf("second.NextPageToken = %q, want empty", second.NextPageToken)
	}
	if service.searchCalls != 2 {
		t.Fatalf("searchCalls = %d, want 2", service.searchCalls)
	}
	if got, want := service.metadataCalls, []string{"msg-1", "msg-2", "msg-3", "msg-4"}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("metadataCalls = %#v, want %#v", got, want)
	}
}

func TestDispatchSystemInfoIncludesQueryAndLabelDiscoveryHints(t *testing.T) {
	t.Parallel()

	srv, err := NewWithDeps(testConfig(), Dependencies{
		LoadPolicy: func(string, string) (*policy.Policy, error) {
			return nil, nil
		},
		NewGmailService: func(context.Context, config.Config) (gmailapi.Service, error) {
			return &fakeGmailService{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithDeps() error = %v", err)
	}

	resp := srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-info",
		Method: rpc.MethodSystemInfo,
		Params: []byte(`{}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var info rpc.SystemInfo
	if err := decodeResult(resp.Result, &info); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if info.SearchQuerySyntax != "gmail" {
		t.Fatalf("info.SearchQuerySyntax = %q, want gmail", info.SearchQuerySyntax)
	}
	if info.LabelQueryMode != "name" {
		t.Fatalf("info.LabelQueryMode = %q, want name", info.LabelQueryMode)
	}
	if info.LabelListMethod != rpc.MethodGmailListLabels {
		t.Fatalf("info.LabelListMethod = %q, want %q", info.LabelListMethod, rpc.MethodGmailListLabels)
	}
	if info.LabelListScope != "mailbox" {
		t.Fatalf("info.LabelListScope = %q, want mailbox", info.LabelListScope)
	}
	if !containsString(info.Methods, rpc.MethodGmailListLabels) {
		t.Fatalf("info.Methods = %#v, want gmail.list_labels", info.Methods)
	}
	if !containsString(info.Methods, rpc.MethodGmailCreateDraft) {
		t.Fatalf("info.Methods = %#v, want gmail.create_draft", info.Methods)
	}
	if containsString(info.Methods, "gmail.send_draft") || containsString(info.Methods, "gmail.send_message") {
		t.Fatalf("info.Methods = %#v, want no send methods", info.Methods)
	}
}

func TestGmailRuntimeSearchQueryDefaultsToAnywhere(t *testing.T) {
	t.Parallel()

	rt := &gmailRuntime{
		policy: &policy.Policy{
			VisibilityLabel: "donna",
		},
	}

	tests := []struct {
		name      string
		userQuery string
		want      string
	}{
		{
			name:      "empty query",
			userQuery: "",
			want:      "(in:anywhere) (label:donna)",
		},
		{
			name:      "plain query",
			userQuery: "female",
			want:      "(in:anywhere) (female) (label:donna)",
		},
		{
			name:      "explicit mailbox scope",
			userQuery: "in:sent female",
			want:      "(in:sent female) (label:donna)",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := rt.searchQuery(tt.userQuery); got != tt.want {
				t.Fatalf("searchQuery(%q) = %q, want %q", tt.userQuery, got, tt.want)
			}
		})
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

func TestHandleSearchThreadsStopsOnRepeatedNextPageToken(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchThreadPages: map[string]gmailapi.SearchThreadsResult{
			"": {
				Threads: []*gmail.Thread{
					{Id: "thread-1"},
				},
				NextPageToken: "page-2",
			},
			"page-2": {
				Threads: []*gmail.Thread{
					{Id: "thread-1"},
				},
				NextPageToken: "page-2",
			},
		},
		threads: map[string]*gmail.Thread{
			"thread-1": {
				Id: "thread-1",
				Messages: []*gmail.Message{
					testMessageAt("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "visible snippet", 1710267600000, "Visible Subject"),
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
		ID:     "req-thread-repeat",
		Method: rpc.MethodGmailSearchThreads,
		Params: []byte(`{"query":"in:anywhere","limit":2}`),
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
	if result.Threads[0].ThreadID != "thread-1" {
		t.Fatalf("result.Threads[0].ThreadID = %q, want thread-1", result.Threads[0].ThreadID)
	}
	if result.NextPageToken != "" {
		t.Fatalf("result.NextPageToken = %q, want empty", result.NextPageToken)
	}
	if service.searchThreadCalls != 2 {
		t.Fatalf("searchThreadCalls = %d, want 2", service.searchThreadCalls)
	}
	if len(service.searchThreadPageTokens) != 2 || service.searchThreadPageTokens[0] != "" || service.searchThreadPageTokens[1] != "page-2" {
		t.Fatalf("searchThreadPageTokens = %#v, want [\"\" \"page-2\"]", service.searchThreadPageTokens)
	}
	if len(service.threadCalls) != 1 || service.threadCalls[0] != "thread-1" {
		t.Fatalf("threadCalls = %#v, want [thread-1]", service.threadCalls)
	}
}

func TestHandleSearchThreadsDeduplicatesReturnedIDsAcrossBrokerPages(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		searchThreadPages: map[string]gmailapi.SearchThreadsResult{
			"": {
				Threads: []*gmail.Thread{
					{Id: "thread-1"},
					{Id: "thread-1"},
					{Id: "thread-2"},
					{Id: "thread-2"},
					{Id: "thread-3"},
				},
				NextPageToken: "page-2",
			},
			"page-2": {
				Threads: []*gmail.Thread{
					{Id: "thread-1"},
					{Id: "thread-2"},
					{Id: "thread-3"},
					{Id: "thread-4"},
					{Id: "thread-4"},
				},
			},
		},
		threads: map[string]*gmail.Thread{
			"thread-1": {Id: "thread-1", Messages: []*gmail.Message{testMessageAt("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "snippet 1", 1710267600000, "Subject 1")}},
			"thread-2": {Id: "thread-2", Messages: []*gmail.Message{testMessageAt("msg-2", "thread-2", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "snippet 2", 1710267600000, "Subject 2")}},
			"thread-3": {Id: "thread-3", Messages: []*gmail.Message{testMessageAt("msg-3", "thread-3", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "snippet 3", 1710267600000, "Subject 3")}},
			"thread-4": {Id: "thread-4", Messages: []*gmail.Message{testMessageAt("msg-4", "thread-4", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "snippet 4", 1710267600000, "Subject 4")}},
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
		ID:     "req-thread-dedupe-1",
		Method: rpc.MethodGmailSearchThreads,
		Params: []byte(`{"query":"in:anywhere","limit":2}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch(first) ok = false, want true: %#v", resp.Error)
	}

	var first rpc.GmailSearchThreadsResult
	if err := decodeResult(resp.Result, &first); err != nil {
		t.Fatalf("decodeResult(first) error = %v", err)
	}
	if len(first.Threads) != 2 {
		t.Fatalf("len(first.Threads) = %d, want 2", len(first.Threads))
	}
	if first.Threads[0].ThreadID != "thread-1" || first.Threads[1].ThreadID != "thread-2" {
		t.Fatalf("first.Threads = %#v, want [thread-1 thread-2]", first.Threads)
	}

	cursor, err := decodeSearchPageToken(first.NextPageToken, searchPageKindThreads, "(in:anywhere) (label:donna)")
	if err != nil {
		t.Fatalf("decodeSearchPageToken(first) error = %v", err)
	}
	if len(cursor.PendingIDs) != 1 || cursor.PendingIDs[0] != "thread-3" {
		t.Fatalf("cursor.PendingIDs = %#v, want [thread-3]", cursor.PendingIDs)
	}
	if len(cursor.ReturnedIDs) != 2 || cursor.ReturnedIDs[0] != "thread-1" || cursor.ReturnedIDs[1] != "thread-2" {
		t.Fatalf("cursor.ReturnedIDs = %#v, want [thread-1 thread-2]", cursor.ReturnedIDs)
	}
	if cursor.GmailPageToken != "page-2" {
		t.Fatalf("cursor.GmailPageToken = %q, want page-2", cursor.GmailPageToken)
	}

	resp = srv.dispatch(rpc.Request{
		V:      rpc.Version1,
		ID:     "req-thread-dedupe-2",
		Method: rpc.MethodGmailSearchThreads,
		Params: []byte(fmt.Sprintf(`{"query":"in:anywhere","limit":2,"page_token":%q}`, first.NextPageToken)),
	})
	if !resp.OK {
		t.Fatalf("dispatch(second) ok = false, want true: %#v", resp.Error)
	}

	var second rpc.GmailSearchThreadsResult
	if err := decodeResult(resp.Result, &second); err != nil {
		t.Fatalf("decodeResult(second) error = %v", err)
	}
	if len(second.Threads) != 2 {
		t.Fatalf("len(second.Threads) = %d, want 2", len(second.Threads))
	}
	if second.Threads[0].ThreadID != "thread-3" || second.Threads[1].ThreadID != "thread-4" {
		t.Fatalf("second.Threads = %#v, want [thread-3 thread-4]", second.Threads)
	}
	if second.NextPageToken != "" {
		t.Fatalf("second.NextPageToken = %q, want empty", second.NextPageToken)
	}
	if service.searchThreadCalls != 2 {
		t.Fatalf("searchThreadCalls = %d, want 2", service.searchThreadCalls)
	}
	if got, want := service.threadCalls, []string{"thread-1", "thread-2", "thread-3", "thread-4"}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("threadCalls = %#v, want %#v", got, want)
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
		Params: []byte(`{"message_id":"msg-1","attachment_id":"sg-part:1"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailGetAttachmentResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if result.Attachment.AttachmentID != "sg-part:1" {
		t.Fatalf("result.Attachment.AttachmentID = %q, want sg-part:1", result.Attachment.AttachmentID)
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

func TestHandleGetAttachmentUsesCurrentGmailAttachmentTokenForStableBrokerID(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-1": testMessage("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-1": testMessageWithAttachment("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full", "report.pdf", "application/pdf", "att-current", 5),
		},
		threadErr:     map[string]error{},
		metadataErr:   map[string]error{},
		fullErr:       map[string]error{},
		attachmentErr: map[string]error{},
		attachmentData: map[string][]byte{
			attachmentKey("msg-1", "att-current"): []byte("hello"),
		},
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
		ID:     "req-attachment-stable",
		Method: rpc.MethodGmailGetAttachment,
		Params: []byte(`{"message_id":"msg-1","attachment_id":"sg-part:1"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	if got, want := service.attachmentCalls, []string{attachmentKey("msg-1", "att-current")}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("attachmentCalls = %#v, want %#v", got, want)
	}
}

func TestHandleGetAttachmentReturnsInlineContentWithoutGmailFetch(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-inline": testMessage("msg-inline", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-inline": testMessageWithInlineAttachment("msg-inline", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full", "report.pdf", "application/pdf", "inline-bytes"),
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
		ID:     "req-inline-attachment",
		Method: rpc.MethodGmailGetAttachment,
		Params: []byte(`{"message_id":"msg-inline","attachment_id":"sg-inline:1"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}

	var result rpc.GmailGetAttachmentResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if result.Attachment.AttachmentID != "sg-inline:1" {
		t.Fatalf("result.Attachment.AttachmentID = %q, want sg-inline:1", result.Attachment.AttachmentID)
	}
	if result.Attachment.ContentBase64 != base64.StdEncoding.EncodeToString([]byte("inline-bytes")) {
		t.Fatalf("result.Attachment.ContentBase64 = %q, want base64 inline-bytes", result.Attachment.ContentBase64)
	}
	if len(service.attachmentCalls) != 0 {
		t.Fatalf("attachmentCalls = %#v, want none", service.attachmentCalls)
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
		Params: []byte(`{"message_id":"msg-1","attachment_id":"sg-part:1"}`),
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

func TestHandleGetAttachmentRejectsAttachmentsAboveTransportCap(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.MaxAttachmentBytes = transportSafeAttachmentBytes + 1024

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-cap": testMessage("msg-cap", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "meta"),
		},
		full: map[string]*gmail.Message{
			"msg-cap": testMessageWithAttachment("msg-cap", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "full", "archive.zip", "application/zip", "att-cap", transportSafeAttachmentBytes+1),
		},
		threadErr:      map[string]error{},
		metadataErr:    map[string]error{},
		fullErr:        map[string]error{},
		attachmentErr:  map[string]error{},
		attachmentData: map[string][]byte{},
	}

	srv, err := NewWithDeps(cfg, Dependencies{
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
		ID:     "req-attachment-cap",
		Method: rpc.MethodGmailGetAttachment,
		Params: []byte(`{"message_id":"msg-cap","attachment_id":"sg-part:1"}`),
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

func TestHandleCreateDraftCreatesNewDraftWithoutSend(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		createDraftResult: gmailapi.DraftCreateResult{
			DraftID:   "draft-1",
			MessageID: "draft-msg-1",
			ThreadID:  "draft-thread-1",
		},
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
		ID:     "req-draft",
		Method: rpc.MethodGmailCreateDraft,
		Params: []byte(`{"to":["Alice <Alice@Example.com>"],"cc":["bob@example.com, carol@example.com"],"subject":"Hello","body_text":"draft body"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}
	if len(service.createdDrafts) != 1 {
		t.Fatalf("createdDrafts = %#v, want one draft", service.createdDrafts)
	}
	input := service.createdDrafts[0]
	if input.From != "owner@example.com" {
		t.Fatalf("input.From = %q, want owner@example.com", input.From)
	}
	if got, want := input.To, []string{"alice@example.com"}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("input.To = %#v, want %#v", got, want)
	}
	if got, want := input.Cc, []string{"bob@example.com", "carol@example.com"}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("input.Cc = %#v, want %#v", got, want)
	}

	var result rpc.GmailCreateDraftResult
	if err := decodeResult(resp.Result, &result); err != nil {
		t.Fatalf("decodeResult() error = %v", err)
	}
	if result.Draft.DraftID != "draft-1" || result.Draft.MessageID != "draft-msg-1" {
		t.Fatalf("result.Draft = %#v, want returned ids", result.Draft)
	}
}

func TestHandleCreateDraftReplyToThreadAuthorizesAndDerivesThreading(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		threads: map[string]*gmail.Thread{
			"thread-1": {
				Id: "thread-1",
				Messages: []*gmail.Message{
					testMessageWithMessageID("msg-1", "thread-1", "alice@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "older", 1710267600000, "Status", "<msg-1@example.com>", ""),
					testMessageWithMessageID("msg-2", "thread-1", "bob@example.com", []string{"owner@example.com"}, []string{"Label_1"}, "newer", 1710354000000, "Status", "<msg-2@example.com>", "<msg-1@example.com>"),
				},
			},
		},
		threadErr: map[string]error{},
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
		ID:     "req-draft-reply",
		Method: rpc.MethodGmailCreateDraft,
		Params: []byte(`{"reply_to_thread_id":"thread-1","reply_all":true,"body_text":"reply body"}`),
	})
	if !resp.OK {
		t.Fatalf("dispatch() ok = false, want true: %#v", resp.Error)
	}
	if len(service.threadCalls) != 1 || service.threadCalls[0] != "thread-1" {
		t.Fatalf("threadCalls = %#v, want [thread-1]", service.threadCalls)
	}
	if len(service.createdDrafts) != 1 {
		t.Fatalf("createdDrafts = %#v, want one draft", service.createdDrafts)
	}

	input := service.createdDrafts[0]
	if input.ThreadID != "thread-1" {
		t.Fatalf("input.ThreadID = %q, want thread-1", input.ThreadID)
	}
	if input.Subject != "Re: Status" {
		t.Fatalf("input.Subject = %q, want Re: Status", input.Subject)
	}
	if input.InReplyTo != "<msg-2@example.com>" {
		t.Fatalf("input.InReplyTo = %q, want newest message id", input.InReplyTo)
	}
	if got, want := input.References, []string{"<msg-1@example.com>", "<msg-2@example.com>"}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("input.References = %#v, want %#v", got, want)
	}
	if got, want := input.To, []string{"bob@example.com"}; fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("input.To = %#v, want %#v", got, want)
	}
}

func TestHandleCreateDraftReplyRejectsHiddenMessage(t *testing.T) {
	t.Parallel()

	service := &fakeGmailService{
		metadata: map[string]*gmail.Message{
			"msg-hidden": testMessageWithMessageID("msg-hidden", "thread-1", "mallory@example.net", []string{"owner@example.com"}, []string{"INBOX"}, "hidden", 1710267600000, "Hidden", "<hidden@example.net>", ""),
		},
		metadataErr: map[string]error{},
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
		ID:     "req-draft-hidden",
		Method: rpc.MethodGmailCreateDraft,
		Params: []byte(`{"reply_to_message_id":"msg-hidden","body_text":"reply body"}`),
	})
	if resp.OK {
		t.Fatalf("dispatch() ok = true, want false")
	}
	if resp.Error == nil || resp.Error.Code != "policy_denied" {
		t.Fatalf("resp.Error = %#v, want policy_denied", resp.Error)
	}
	if len(service.createdDrafts) != 0 {
		t.Fatalf("createdDrafts = %#v, want none", service.createdDrafts)
	}
}

type fakeGmailService struct {
	labels                 map[string]string
	labelList              []gmailapi.Label
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
	createdDrafts          []gmailapi.DraftCreateInput
	createDraftResult      gmailapi.DraftCreateResult
	createDraftErr         error
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

func (f *fakeGmailService) ListLabels(context.Context) ([]gmailapi.Label, error) {
	f.labelCalls++
	if f.labelErr != nil {
		return nil, f.labelErr
	}
	if f.labelList != nil {
		result := make([]gmailapi.Label, len(f.labelList))
		copy(result, f.labelList)
		return result, nil
	}

	names := make([]string, 0, len(f.labels))
	for name := range f.labels {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]gmailapi.Label, 0, len(names))
	for _, name := range names {
		result = append(result, gmailapi.Label{
			ID:   f.labels[name],
			Name: name,
			Type: "user",
		})
	}
	return result, nil
}

func (f *fakeGmailService) ListLabelNameToID(context.Context) (map[string]string, error) {
	f.labelCalls++
	if f.labelErr != nil {
		return nil, f.labelErr
	}
	if f.labels != nil {
		return f.labels, nil
	}
	result := make(map[string]string, len(f.labelList))
	for _, label := range f.labelList {
		if label.ID == "" || label.Name == "" {
			continue
		}
		result[strings.ToLower(label.Name)] = label.ID
	}
	return result, nil
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

func (f *fakeGmailService) CreateDraft(_ context.Context, input gmailapi.DraftCreateInput) (gmailapi.DraftCreateResult, error) {
	f.createdDrafts = append(f.createdDrafts, input)
	if f.createDraftErr != nil {
		return gmailapi.DraftCreateResult{}, f.createDraftErr
	}
	if f.createDraftResult.DraftID != "" {
		return f.createDraftResult, nil
	}
	return gmailapi.DraftCreateResult{
		DraftID:   "draft-1",
		MessageID: "draft-msg-1",
		ThreadID:  input.ThreadID,
	}, nil
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

func testMessageWithMessageID(id, threadID, from string, to []string, labels []string, body string, internalDate int64, subject, messageID, references string) *gmail.Message {
	msg := testMessageAt(id, threadID, from, to, labels, body, internalDate, subject)
	if strings.TrimSpace(messageID) != "" {
		msg.Payload.Headers = append(msg.Payload.Headers, &gmail.MessagePartHeader{Name: "Message-ID", Value: messageID})
	}
	if strings.TrimSpace(references) != "" {
		msg.Payload.Headers = append(msg.Payload.Headers, &gmail.MessagePartHeader{Name: "References", Value: references})
	}
	return msg
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

func testMessageWithInlineAttachment(id, threadID, from string, to []string, labels []string, body, filename, mimeType, attachmentBody string) *gmail.Message {
	msg := testMessage(id, threadID, from, to, labels, body)
	msg.Payload.Parts = append(msg.Payload.Parts, &gmail.MessagePart{
		Filename: filename,
		MimeType: mimeType,
		Body: &gmail.MessagePartBody{
			Data: base64.RawURLEncoding.EncodeToString([]byte(attachmentBody)),
			Size: int64(len(attachmentBody)),
		},
	})
	return msg
}

func attachmentKey(messageID, attachmentID string) string {
	return messageID + ":" + attachmentID
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
