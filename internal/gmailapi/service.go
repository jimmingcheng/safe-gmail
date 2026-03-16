package gmailapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	apigoogle "google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/jimmingcheng/safe-gmail/internal/auth"
	"github.com/jimmingcheng/safe-gmail/internal/config"
)

const requestTimeout = 30 * time.Second

// Service is the minimal Gmail surface the broker needs in v1.
type Service interface {
	ProfileEmail(ctx context.Context) (string, error)
	ListLabels(ctx context.Context) ([]Label, error)
	ListLabelNameToID(ctx context.Context) (map[string]string, error)
	SearchThreads(ctx context.Context, query string, limit int64, pageToken string) (SearchThreadsResult, error)
	SearchMessages(ctx context.Context, query string, limit int64, pageToken string) (SearchMessagesResult, error)
	GetMessageMetadata(ctx context.Context, messageID string) (*gmail.Message, error)
	GetMessageFull(ctx context.Context, messageID string) (*gmail.Message, error)
	GetThreadMetadata(ctx context.Context, threadID string) (*gmail.Thread, error)
	GetAttachmentData(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

// Label is the normalized Gmail label metadata the broker uses for policy and discovery.
type Label struct {
	ID   string
	Name string
	Type string
}

// SearchThreadsResult is the broker-side search page returned from Gmail.
type SearchThreadsResult struct {
	Threads       []*gmail.Thread
	NextPageToken string
}

// SearchMessagesResult is the broker-side search page returned from Gmail.
type SearchMessagesResult struct {
	Messages      []*gmail.Message
	NextPageToken string
}

// Client implements Service using the Gmail API client.
type Client struct {
	svc *gmail.Service
}

// New opens the broker-owned Gmail API client from the broker config.
func New(ctx context.Context, cfg config.Config) (Service, error) {
	oauthClient, err := auth.LoadOAuthClient(cfg.OAuthClientPath)
	if err != nil {
		return nil, err
	}

	store, err := auth.OpenTokenStore(cfg.AuthStore)
	if err != nil {
		return nil, err
	}

	tokenSource, err := auth.TokenSource(ctx, oauthClient, store, cfg.Instance, cfg.AccountEmail, []string{gmail.GmailReadonlyScope})
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
			Base:   http.DefaultTransport,
		},
	}

	svc, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}
	return &Client{svc: svc}, nil
}

// ProfileEmailFromToken verifies the token by asking Gmail for the authorized profile.
func ProfileEmailFromToken(ctx context.Context, tok *oauth2.Token) (string, error) {
	if tok == nil {
		return "", fmt.Errorf("missing oauth token")
	}
	httpClient := &http.Client{
		Transport: &oauth2.Transport{
			Source: oauth2.StaticTokenSource(tok),
			Base:   http.DefaultTransport,
		},
	}
	svc, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return "", fmt.Errorf("create gmail service: %w", err)
	}
	return (&Client{svc: svc}).ProfileEmail(ctx)
}

// StatusCode extracts the upstream Google API status code when available.
func StatusCode(err error) int {
	var apiErr *apigoogle.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return 0
}

// ProfileEmail returns the Gmail address of the broker-owned token.
func (c *Client) ProfileEmail(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	profile, err := c.svc.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("get gmail profile: %w", err)
	}
	return strings.TrimSpace(profile.EmailAddress), nil
}

// ListLabelNameToID returns a lowercased name-to-ID map for label allowlists.
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	resp, err := c.svc.Users.Labels.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list gmail labels: %w", err)
	}

	result := make([]Label, 0, len(resp.Labels))
	for _, label := range resp.Labels {
		if label == nil || label.Id == "" || label.Name == "" {
			continue
		}
		result = append(result, Label{
			ID:   strings.TrimSpace(label.Id),
			Name: strings.TrimSpace(label.Name),
			Type: strings.ToLower(strings.TrimSpace(label.Type)),
		})
	}
	return result, nil
}

// ListLabelNameToID returns a lowercased name-to-ID map for label allowlists.
func (c *Client) ListLabelNameToID(ctx context.Context) (map[string]string, error) {
	labels, err := c.ListLabels(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(labels))
	for _, label := range labels {
		if label.ID == "" || label.Name == "" {
			continue
		}
		result[strings.ToLower(label.Name)] = label.ID
	}
	return result, nil
}

// SearchThreads searches Gmail and returns a page of matching thread IDs.
func (c *Client) SearchThreads(ctx context.Context, query string, limit int64, pageToken string) (SearchThreadsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	call := c.svc.Users.Threads.List("me").Context(ctx)
	if strings.TrimSpace(query) != "" {
		call = call.Q(query)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(strings.TrimSpace(pageToken))
	}
	if limit > 0 {
		call = call.MaxResults(limit)
	}

	resp, err := call.Fields("threads(id),nextPageToken").Do()
	if err != nil {
		return SearchThreadsResult{}, fmt.Errorf("search gmail threads: %w", err)
	}

	result := SearchThreadsResult{
		Threads:       resp.Threads,
		NextPageToken: strings.TrimSpace(resp.NextPageToken),
	}
	if result.Threads == nil {
		result.Threads = []*gmail.Thread{}
	}
	return result, nil
}

// SearchMessages searches Gmail and returns a page of matching message IDs.
func (c *Client) SearchMessages(ctx context.Context, query string, limit int64, pageToken string) (SearchMessagesResult, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	call := c.svc.Users.Messages.List("me").Context(ctx)
	if strings.TrimSpace(query) != "" {
		call = call.Q(query)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(strings.TrimSpace(pageToken))
	}
	if limit > 0 {
		call = call.MaxResults(limit)
	}

	resp, err := call.Fields("messages(id,threadId),nextPageToken").Do()
	if err != nil {
		return SearchMessagesResult{}, fmt.Errorf("search gmail messages: %w", err)
	}

	result := SearchMessagesResult{
		Messages:      resp.Messages,
		NextPageToken: strings.TrimSpace(resp.NextPageToken),
	}
	if result.Messages == nil {
		result.Messages = []*gmail.Message{}
	}
	return result, nil
}

// GetMessageMetadata fetches the fixed metadata used for authorization.
func (c *Client) GetMessageMetadata(ctx context.Context, messageID string) (*gmail.Message, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return c.svc.Users.Messages.Get("me", NormalizeMessageID(messageID)).
		Format("metadata").
		MetadataHeaders("From", "To", "Cc", "Bcc", "Subject", "Date").
		Fields("id,threadId,labelIds,snippet,internalDate,payload(headers)").
		Context(ctx).
		Do()
}

// GetMessageFull fetches the full Gmail message after authorization succeeds.
func (c *Client) GetMessageFull(ctx context.Context, messageID string) (*gmail.Message, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return c.svc.Users.Messages.Get("me", NormalizeMessageID(messageID)).
		Format("full").
		Context(ctx).
		Do()
}

// GetThreadMetadata fetches fixed metadata for all messages in a thread.
func (c *Client) GetThreadMetadata(ctx context.Context, threadID string) (*gmail.Thread, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return c.svc.Users.Threads.Get("me", NormalizeThreadID(threadID)).
		Format("metadata").
		MetadataHeaders("From", "To", "Cc", "Bcc", "Subject", "Date").
		Fields("id,messages(id,threadId,labelIds,snippet,internalDate,payload(headers))").
		Context(ctx).
		Do()
}

// GetAttachmentData fetches the decoded bytes for one attachment.
func (c *Client) GetAttachmentData(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := c.svc.Users.Messages.Attachments.Get("me", NormalizeMessageID(messageID), strings.TrimSpace(attachmentID)).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("get gmail attachment: %w", err)
	}
	if body == nil || strings.TrimSpace(body.Data) == "" {
		return nil, fmt.Errorf("get gmail attachment: empty attachment data")
	}

	data, err := base64.RawURLEncoding.DecodeString(body.Data)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(body.Data)
		if err != nil {
			return nil, fmt.Errorf("decode gmail attachment: %w", err)
		}
	}
	return data, nil
}
