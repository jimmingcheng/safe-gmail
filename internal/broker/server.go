package broker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"

	"github.com/jimmingcheng/safe-gmail/internal/auth"
	"github.com/jimmingcheng/safe-gmail/internal/config"
	"github.com/jimmingcheng/safe-gmail/internal/gmailapi"
	"github.com/jimmingcheng/safe-gmail/internal/policy"
	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

const requestMaxBytes = 1 << 20
const defaultSearchLimit = 20
const searchPageTokenPrefix = "sgp1:"

const (
	searchPageTokenVersion = 1
	searchPageKindMessages = "messages"
	searchPageKindThreads  = "threads"
)

type searchPageCursor struct {
	V              int      `json:"v"`
	Kind           string   `json:"kind"`
	Query          string   `json:"query"`
	PendingIDs     []string `json:"pending_ids,omitempty"`
	GmailPageToken string   `json:"gmail_page_token,omitempty"`
}

// Dependencies allows tests to inject fake policy and Gmail clients.
type Dependencies struct {
	LoadPolicy      func(path, owner string) (*policy.Policy, error)
	NewGmailService func(ctx context.Context, cfg config.Config) (gmailapi.Service, error)
}

// Server is the trusted-side Unix socket broker.
type Server struct {
	cfg  config.Config
	deps Dependencies
}

type gmailRuntime struct {
	policy *policy.Policy
	client gmailapi.Service
}

func New(cfg config.Config) (*Server, error) {
	return NewWithDeps(cfg, Dependencies{})
}

// NewWithDeps constructs a broker with optionally injected runtime dependencies.
func NewWithDeps(cfg config.Config, deps Dependencies) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.LoadPolicy == nil {
		deps.LoadPolicy = policy.Load
	}
	if deps.NewGmailService == nil {
		deps.NewGmailService = gmailapi.New
	}
	return &Server{cfg: cfg, deps: deps}, nil
}

// Run starts the broker and serves until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	parentDir := filepath.Dir(s.cfg.SocketPath)
	if err := os.MkdirAll(parentDir, 0o750); err != nil {
		return fmt.Errorf("ensure socket dir: %w", err)
	}

	if err := os.Remove(s.cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(s.cfg.SocketPath)

	mode, err := s.cfg.SocketFileMode()
	if err != nil {
		return err
	}
	if err := os.Chmod(s.cfg.SocketPath, mode); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return fmt.Errorf("accept connection: %w", err)
		}

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		_ = writeResponse(conn, rpc.NewError("", "internal_error", "expected unix connection", false))
		return
	}

	uid, err := peerUID(unixConn)
	if err != nil {
		_ = writeResponse(conn, rpc.NewError("", "internal_error", "failed to read peer credentials", false))
		return
	}
	if uid != s.cfg.ClientUID {
		_ = writeResponse(conn, rpc.NewError("", "unauthorized_peer", "peer uid is not allowed", false))
		return
	}

	payload, err := rpc.ReadFrame(conn, requestMaxBytes)
	if err != nil {
		_ = writeResponse(conn, rpc.NewError("", "invalid_request", err.Error(), false))
		return
	}

	var req rpc.Request
	if err := json.Unmarshal(payload, &req); err != nil {
		_ = writeResponse(conn, rpc.NewError("", "invalid_request", "request must be valid JSON", false))
		return
	}
	if err := rpc.ValidateRequest(req); err != nil {
		code := "invalid_request"
		var validationErr *rpc.RequestValidationError
		if errors.As(err, &validationErr) {
			code = validationErr.Code
		}
		_ = writeResponse(conn, rpc.NewError(req.ID, code, err.Error(), false))
		return
	}

	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	resp := s.dispatch(req)
	_ = writeResponse(conn, resp)
}

func (s *Server) dispatch(req rpc.Request) rpc.Response {
	switch req.Method {
	case rpc.MethodSystemPing:
		return rpc.NewSuccess(req.ID, map[string]any{"pong": true})
	case rpc.MethodSystemInfo:
		return rpc.NewSuccess(req.ID, rpc.SystemInfo{
			Service:            "safe-gmaild",
			ProtocolVersion:    rpc.Version1,
			Instance:           s.cfg.Instance,
			AccountEmail:       s.cfg.AccountEmail,
			MaxBodyBytes:       s.cfg.MaxBodyBytes,
			MaxAttachmentBytes: s.cfg.MaxAttachmentBytes,
			MaxSearchResults:   s.cfg.MaxSearchResults,
			Methods: []string{
				rpc.MethodSystemPing,
				rpc.MethodSystemInfo,
				rpc.MethodGmailSearchThreads,
				rpc.MethodGmailSearchMessages,
				rpc.MethodGmailGetMessage,
				rpc.MethodGmailGetThread,
				rpc.MethodGmailGetAttachment,
			},
		})
	case rpc.MethodGmailSearchThreads:
		return s.handleSearchThreads(req)
	case rpc.MethodGmailSearchMessages:
		return s.handleSearchMessages(req)
	case rpc.MethodGmailGetMessage:
		return s.handleGetMessage(req)
	case rpc.MethodGmailGetThread:
		return s.handleGetThread(req)
	case rpc.MethodGmailGetAttachment:
		return s.handleGetAttachment(req)
	default:
		return rpc.NewError(req.ID, "method_not_allowed", "method is not exposed by this broker", false)
	}
}

func (s *Server) handleSearchThreads(req rpc.Request) rpc.Response {
	var params rpc.GmailSearchThreadsParams
	if err := rpc.DecodeParams(req.Params, &params); err != nil {
		return rpc.NewError(req.ID, "invalid_params", fmt.Sprintf("invalid gmail.search_threads params: %v", err), false)
	}
	if err := params.Validate(); err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, errResp := s.openGmailRuntime(ctx, req.ID)
	if errResp != nil {
		return *errResp
	}

	limit := normalizeSearchLimit(params.Limit, s.cfg.MaxSearchResults)
	batchSize := searchBatchSize(limit, s.cfg.MaxSearchResults)
	cursor, err := decodeSearchPageToken(params.PageToken, searchPageKindThreads, params.Query)
	if err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	pendingIDs := append([]string(nil), cursor.PendingIDs...)
	gmailPageToken := cursor.GmailPageToken
	threads := make([]rpc.ThreadSummary, 0, limit)
	for len(threads) < limit {
		if len(pendingIDs) == 0 {
			currentPageToken := gmailPageToken
			page, err := rt.client.SearchThreads(ctx, params.Query, int64(batchSize), currentPageToken)
			if err != nil {
				return mapGmailError(req.ID, err)
			}
			pendingIDs = extractThreadIDs(page.Threads)
			gmailPageToken = page.NextPageToken
			if len(pendingIDs) == 0 {
				if strings.TrimSpace(gmailPageToken) == "" || strings.TrimSpace(gmailPageToken) == strings.TrimSpace(currentPageToken) {
					gmailPageToken = ""
					break
				}
				continue
			}
		}

		threadID := pendingIDs[0]
		pendingIDs = pendingIDs[1:]
		if strings.TrimSpace(threadID) == "" {
			continue
		}

		thread, err := rt.client.GetThreadMetadata(ctx, threadID)
		if err != nil {
			return mapGmailError(req.ID, err)
		}
		visible := rt.visibleThreadMessages(thread)
		if len(visible) == 0 {
			continue
		}
		summary, err := gmailapi.BuildThreadSummary(thread.Id, visible)
		if err != nil {
			return rpc.NewError(req.ID, "internal_error", "failed to shape thread summary", false)
		}
		threads = append(threads, summary)
	}

	nextPageToken, err := encodeSearchPageToken(searchPageCursor{
		Kind:           searchPageKindThreads,
		Query:          params.Query,
		PendingIDs:     pendingIDs,
		GmailPageToken: gmailPageToken,
	})
	if err != nil {
		return rpc.NewError(req.ID, "internal_error", "failed to encode search page token", false)
	}

	return rpc.NewSuccess(req.ID, rpc.GmailSearchThreadsResult{
		Threads:       threads,
		NextPageToken: nextPageToken,
	})
}

func (s *Server) handleGetMessage(req rpc.Request) rpc.Response {
	var params rpc.GmailGetMessageParams
	if err := rpc.DecodeParams(req.Params, &params); err != nil {
		return rpc.NewError(req.ID, "invalid_params", fmt.Sprintf("invalid gmail.get_message params: %v", err), false)
	}
	if err := params.Validate(); err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, errResp := s.openGmailRuntime(ctx, req.ID)
	if errResp != nil {
		return *errResp
	}

	meta, err := rt.client.GetMessageMetadata(ctx, params.MessageID)
	if err != nil {
		return mapGmailError(req.ID, err)
	}
	if !rt.allowsMessage(meta) {
		return rpc.NewError(req.ID, "policy_denied", "message is not visible under broker policy", false)
	}

	full, err := rt.client.GetMessageFull(ctx, params.MessageID)
	if err != nil {
		return mapGmailError(req.ID, err)
	}

	message, err := gmailapi.BuildMessageDetail(full, params.IncludeBody, s.cfg.MaxBodyBytes)
	if err != nil {
		return rpc.NewError(req.ID, "internal_error", "failed to shape message response", false)
	}

	return rpc.NewSuccess(req.ID, rpc.GmailGetMessageResult{Message: message})
}

func (s *Server) handleSearchMessages(req rpc.Request) rpc.Response {
	var params rpc.GmailSearchMessagesParams
	if err := rpc.DecodeParams(req.Params, &params); err != nil {
		return rpc.NewError(req.ID, "invalid_params", fmt.Sprintf("invalid gmail.search_messages params: %v", err), false)
	}
	if err := params.Validate(); err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, errResp := s.openGmailRuntime(ctx, req.ID)
	if errResp != nil {
		return *errResp
	}

	limit := normalizeSearchLimit(params.Limit, s.cfg.MaxSearchResults)
	batchSize := searchBatchSize(limit, s.cfg.MaxSearchResults)
	cursor, err := decodeSearchPageToken(params.PageToken, searchPageKindMessages, params.Query)
	if err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	pendingIDs := append([]string(nil), cursor.PendingIDs...)
	gmailPageToken := cursor.GmailPageToken
	visibleMessages := make([]*gmail.Message, 0, limit)
	for len(visibleMessages) < limit {
		if len(pendingIDs) == 0 {
			currentPageToken := gmailPageToken
			page, err := rt.client.SearchMessages(ctx, params.Query, int64(batchSize), currentPageToken)
			if err != nil {
				return mapGmailError(req.ID, err)
			}
			pendingIDs = extractMessageIDs(page.Messages)
			gmailPageToken = page.NextPageToken
			if len(pendingIDs) == 0 {
				if strings.TrimSpace(gmailPageToken) == "" || strings.TrimSpace(gmailPageToken) == strings.TrimSpace(currentPageToken) {
					gmailPageToken = ""
					break
				}
				continue
			}
		}

		messageID := pendingIDs[0]
		pendingIDs = pendingIDs[1:]
		if strings.TrimSpace(messageID) == "" {
			continue
		}

		meta, err := rt.client.GetMessageMetadata(ctx, messageID)
		if err != nil {
			return mapGmailError(req.ID, err)
		}
		if !rt.allowsMessage(meta) {
			continue
		}
		visibleMessages = append(visibleMessages, meta)
	}

	nextPageToken, err := encodeSearchPageToken(searchPageCursor{
		Kind:           searchPageKindMessages,
		Query:          params.Query,
		PendingIDs:     pendingIDs,
		GmailPageToken: gmailPageToken,
	})
	if err != nil {
		return rpc.NewError(req.ID, "internal_error", "failed to encode search page token", false)
	}

	if params.IncludeBody {
		messages := make([]rpc.MessageDetail, 0, len(visibleMessages))
		for _, meta := range visibleMessages {
			full, err := rt.client.GetMessageFull(ctx, meta.Id)
			if err != nil {
				return mapGmailError(req.ID, err)
			}
			message, err := gmailapi.BuildMessageDetail(full, true, s.cfg.MaxBodyBytes)
			if err != nil {
				return rpc.NewError(req.ID, "internal_error", "failed to shape message response", false)
			}
			messages = append(messages, message)
		}
		return rpc.NewSuccess(req.ID, rpc.GmailSearchMessagesResultDetail{
			Messages:      messages,
			NextPageToken: nextPageToken,
		})
	}

	messages := make([]rpc.MessageSummary, 0, len(visibleMessages))
	for _, meta := range visibleMessages {
		message, err := gmailapi.BuildMessageSummary(meta)
		if err != nil {
			return rpc.NewError(req.ID, "internal_error", "failed to shape message response", false)
		}
		messages = append(messages, message)
	}
	return rpc.NewSuccess(req.ID, rpc.GmailSearchMessagesResultSummary{
		Messages:      messages,
		NextPageToken: nextPageToken,
	})
}

func (s *Server) handleGetThread(req rpc.Request) rpc.Response {
	var params rpc.GmailGetThreadParams
	if err := rpc.DecodeParams(req.Params, &params); err != nil {
		return rpc.NewError(req.ID, "invalid_params", fmt.Sprintf("invalid gmail.get_thread params: %v", err), false)
	}
	if err := params.Validate(); err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, errResp := s.openGmailRuntime(ctx, req.ID)
	if errResp != nil {
		return *errResp
	}

	thread, err := rt.client.GetThreadMetadata(ctx, params.ThreadID)
	if err != nil {
		return mapGmailError(req.ID, err)
	}

	visible := rt.visibleThreadMessages(thread)
	if len(visible) == 0 {
		return rpc.NewError(req.ID, "policy_denied", "thread is not visible under broker policy", false)
	}

	if params.IncludeBodies {
		messages := make([]rpc.MessageDetail, 0, len(visible))
		for _, meta := range visible {
			full, err := rt.client.GetMessageFull(ctx, meta.Id)
			if err != nil {
				return mapGmailError(req.ID, err)
			}
			message, err := gmailapi.BuildMessageDetail(full, true, s.cfg.MaxBodyBytes)
			if err != nil {
				return rpc.NewError(req.ID, "internal_error", "failed to shape thread response", false)
			}
			messages = append(messages, message)
		}
		return rpc.NewSuccess(req.ID, rpc.GmailGetThreadResultDetail{
			Thread: rpc.ThreadDetail{
				ThreadID: thread.Id,
				Messages: messages,
			},
		})
	}

	messages := make([]rpc.MessageSummary, 0, len(visible))
	for _, meta := range visible {
		message, err := gmailapi.BuildMessageSummary(meta)
		if err != nil {
			return rpc.NewError(req.ID, "internal_error", "failed to shape thread response", false)
		}
		messages = append(messages, message)
	}
	return rpc.NewSuccess(req.ID, rpc.GmailGetThreadResultSummary{
		Thread: rpc.ThreadDetailSummary{
			ThreadID: thread.Id,
			Messages: messages,
		},
	})
}

func (s *Server) handleGetAttachment(req rpc.Request) rpc.Response {
	var params rpc.GmailGetAttachmentParams
	if err := rpc.DecodeParams(req.Params, &params); err != nil {
		return rpc.NewError(req.ID, "invalid_params", fmt.Sprintf("invalid gmail.get_attachment params: %v", err), false)
	}
	if err := params.Validate(); err != nil {
		return rpc.NewError(req.ID, "invalid_params", err.Error(), false)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, errResp := s.openGmailRuntime(ctx, req.ID)
	if errResp != nil {
		return *errResp
	}

	meta, err := rt.client.GetMessageMetadata(ctx, params.MessageID)
	if err != nil {
		return mapGmailError(req.ID, err)
	}
	if !rt.allowsMessage(meta) {
		return rpc.NewError(req.ID, "policy_denied", "message is not visible under broker policy", false)
	}

	full, err := rt.client.GetMessageFull(ctx, params.MessageID)
	if err != nil {
		return mapGmailError(req.ID, err)
	}
	attachment, ok := gmailapi.FindAttachment(full.Payload, params.AttachmentID)
	if !ok {
		return rpc.NewError(req.ID, "not_found", "attachment was not found on visible message", false)
	}
	if s.cfg.MaxAttachmentBytes > 0 && attachment.Size > int64(s.cfg.MaxAttachmentBytes) {
		return rpc.NewError(req.ID, "too_large", "attachment exceeds broker size limit", false)
	}

	data, err := rt.client.GetAttachmentData(ctx, params.MessageID, params.AttachmentID)
	if err != nil {
		return mapGmailError(req.ID, err)
	}
	if s.cfg.MaxAttachmentBytes > 0 && len(data) > s.cfg.MaxAttachmentBytes {
		return rpc.NewError(req.ID, "too_large", "attachment exceeds broker size limit", false)
	}

	return rpc.NewSuccess(req.ID, rpc.GmailGetAttachmentResult{
		Attachment: rpc.AttachmentContent{
			AttachmentMeta: attachment,
			ContentBase64:  base64.StdEncoding.EncodeToString(data),
		},
	})
}

func (s *Server) openGmailRuntime(ctx context.Context, id string) (*gmailRuntime, *rpc.Response) {
	pol, err := s.deps.LoadPolicy(s.cfg.PolicyPath, s.cfg.AccountEmail)
	if err != nil {
		resp := rpc.NewError(id, "internal_error", "failed to load broker policy", false)
		return nil, &resp
	}

	client, err := s.deps.NewGmailService(ctx, s.cfg)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTokenNotFound):
			resp := rpc.NewError(id, "internal_error", "broker is not authenticated", false)
			return nil, &resp
		default:
			resp := rpc.NewError(id, "internal_error", "failed to initialize Gmail client", false)
			return nil, &resp
		}
	}

	if pol != nil && len(pol.Labels) > 0 {
		labelMap, err := client.ListLabelNameToID(ctx)
		if err != nil {
			resp := mapGmailError(id, err)
			return nil, &resp
		}
		pol.ResolveLabelNames(labelMap)
	}

	return &gmailRuntime{
		policy: pol,
		client: client,
	}, nil
}

func (r *gmailRuntime) allowsMessage(msg *gmail.Message) bool {
	if r == nil || r.policy == nil {
		return true
	}
	return r.policy.AllowsMessage(gmailapi.MessageParticipants(msg))
}

func (r *gmailRuntime) visibleThreadMessages(thread *gmail.Thread) []*gmail.Message {
	if thread == nil || len(thread.Messages) == 0 {
		return nil
	}

	visible := make([]*gmail.Message, 0, len(thread.Messages))
	for _, msg := range thread.Messages {
		if r.allowsMessage(msg) {
			visible = append(visible, msg)
		}
	}
	return visible
}

func normalizeSearchLimit(limit, max int) int {
	if max <= 0 {
		max = defaultSearchLimit
	}
	switch {
	case limit <= 0:
		if defaultSearchLimit < max {
			return defaultSearchLimit
		}
		return max
	case limit > max:
		return max
	default:
		return limit
	}
}

func searchBatchSize(limit, max int) int {
	if max <= 0 {
		max = defaultSearchLimit
	}
	if limit < defaultSearchLimit {
		limit = defaultSearchLimit
	}
	if limit > max {
		return max
	}
	return limit
}

func extractThreadIDs(threads []*gmail.Thread) []string {
	ids := make([]string, 0, len(threads))
	for _, thread := range threads {
		if thread == nil || strings.TrimSpace(thread.Id) == "" {
			continue
		}
		ids = append(ids, thread.Id)
	}
	return ids
}

func extractMessageIDs(messages []*gmail.Message) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		if message == nil || strings.TrimSpace(message.Id) == "" {
			continue
		}
		ids = append(ids, message.Id)
	}
	return ids
}

func decodeSearchPageToken(raw, kind, query string) (searchPageCursor, error) {
	cursor := searchPageCursor{
		V:     searchPageTokenVersion,
		Kind:  kind,
		Query: query,
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cursor, nil
	}
	if !strings.HasPrefix(raw, searchPageTokenPrefix) {
		cursor.GmailPageToken = raw
		return cursor, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, searchPageTokenPrefix))
	if err != nil {
		return searchPageCursor{}, fmt.Errorf("invalid page_token")
	}
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return searchPageCursor{}, fmt.Errorf("invalid page_token")
	}
	if cursor.V != searchPageTokenVersion {
		return searchPageCursor{}, fmt.Errorf("unsupported page_token version")
	}
	if cursor.Kind != kind {
		return searchPageCursor{}, fmt.Errorf("page_token is not valid for this search")
	}
	if cursor.Query != query {
		return searchPageCursor{}, fmt.Errorf("page_token does not match query")
	}
	return cursor, nil
}

func encodeSearchPageToken(cursor searchPageCursor) (string, error) {
	if len(cursor.PendingIDs) == 0 && strings.TrimSpace(cursor.GmailPageToken) == "" {
		return "", nil
	}
	cursor.V = searchPageTokenVersion
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return searchPageTokenPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func mapGmailError(id string, err error) rpc.Response {
	switch status := gmailapi.StatusCode(err); status {
	case 404:
		return rpc.NewError(id, "not_found", "gmail object was not found", false)
	case 429:
		return rpc.NewError(id, "rate_limited", "gmail api rate limited the request", true)
	case 500, 502, 503, 504:
		return rpc.NewError(id, "gmail_api_error", "gmail api request failed", true)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return rpc.NewError(id, "gmail_api_error", "gmail api request timed out", true)
	}
	return rpc.NewError(id, "gmail_api_error", "gmail api request failed", false)
}

func writeResponse(conn net.Conn, resp rpc.Response) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return rpc.WriteFrame(conn, payload)
}
