package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jimmingcheng/safe-gmail/internal/gmailapi"
	"github.com/jimmingcheng/safe-gmail/internal/rpc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("safe-gmail", flag.ContinueOnError)
	socketPath := fs.String("socket", os.Getenv("SAFE_GMAIL_SOCKET"), "Path to broker Unix socket")
	jsonOut := fs.Bool("json", false, "Print raw JSON response")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		usage(os.Stderr)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Global flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	switch {
	case len(rest) >= 1 && rest[0] == "system":
		return runSystem(*socketPath, *jsonOut, rest[1:])
	case len(rest) >= 1 && rest[0] == "labels":
		return runLabels(*socketPath, *jsonOut, rest[1:])
	case len(rest) >= 1 && rest[0] == "search":
		return runSearch(*socketPath, *jsonOut, rest[1:])
	case len(rest) >= 1 && rest[0] == "get":
		return runGet(*socketPath, *jsonOut, rest[1:])
	case len(rest) >= 1 && rest[0] == "thread":
		return runThread(*socketPath, *jsonOut, rest[1:])
	case len(rest) >= 1 && rest[0] == "attachment":
		return runAttachment(*socketPath, *jsonOut, rest[1:])
	default:
		usage(os.Stderr)
		return 2
	}
}

func runSystem(socketPath string, jsonOut bool, args []string) int {
	if len(args) != 1 {
		usage(os.Stderr)
		return 2
	}

	var method string
	switch args[0] {
	case "ping":
		method = rpc.MethodSystemPing
	case "info":
		method = rpc.MethodSystemInfo
	default:
		usage(os.Stderr)
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: method,
		Params: json.RawMessage(`{}`),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if jsonOut {
		return printJSONResponse(resp)
	}

	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	switch method {
	case rpc.MethodSystemPing:
		fmt.Fprintln(os.Stdout, "pong")
	case rpc.MethodSystemInfo:
		data, err := json.Marshal(resp.Result)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		var info rpc.SystemInfo
		if err := json.Unmarshal(data, &info); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "service\t%s\n", info.Service)
		fmt.Fprintf(os.Stdout, "instance\t%s\n", info.Instance)
		fmt.Fprintf(os.Stdout, "account\t%s\n", info.AccountEmail)
		if info.SearchQuerySyntax != "" {
			fmt.Fprintf(os.Stdout, "search_query_syntax\t%s\n", info.SearchQuerySyntax)
		}
		if info.LabelQueryMode != "" {
			fmt.Fprintf(os.Stdout, "label_query_mode\t%s\n", info.LabelQueryMode)
		}
		if info.LabelListMethod != "" {
			fmt.Fprintf(os.Stdout, "label_list_method\t%s\n", info.LabelListMethod)
		}
		if info.LabelListScope != "" {
			fmt.Fprintf(os.Stdout, "label_list_scope\t%s\n", info.LabelListScope)
		}
		fmt.Fprintf(os.Stdout, "methods\t%s\n", strings.Join(info.Methods, ","))
	}

	return 0
}

func runLabels(socketPath string, jsonOut bool, args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "list":
		return runLabelsList(socketPath, jsonOut, args[1:])
	default:
		usage(os.Stderr)
		return 2
	}
}

func runGet(socketPath string, jsonOut bool, args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	includeBody := fs.Bool("body", false, "Include body text")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printCommandUsage(os.Stderr, "safe-gmail get [--body] <message-id>",
			"Fetches one visible message. This always prints one line per attachment as attachment<TAB><attachment-id><TAB><filename><TAB><mime-type><TAB><size>. For a reliable attachment workflow: search for a message_id, run get <message-id> to collect attachment_id values, then run attachment get <message-id> <attachment-id>.",
			fs)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	params, err := json.Marshal(rpc.GmailGetMessageParams{
		MessageID:   gmailapi.NormalizeMessageID(fs.Arg(0)),
		IncludeBody: *includeBody,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: rpc.MethodGmailGetMessage,
		Params: params,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		return printJSONResponse(resp)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	var result rpc.GmailGetMessageResult
	if err := decodeResult(resp.Result, &result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	message := result.Message
	fmt.Fprintf(os.Stdout, "message_id\t%s\n", message.MessageID)
	fmt.Fprintf(os.Stdout, "thread_id\t%s\n", message.ThreadID)
	fmt.Fprintf(os.Stdout, "from\t%s\n", message.From)
	fmt.Fprintf(os.Stdout, "to\t%s\n", strings.Join(message.To, ","))
	fmt.Fprintf(os.Stdout, "cc\t%s\n", strings.Join(message.Cc, ","))
	fmt.Fprintf(os.Stdout, "bcc\t%s\n", strings.Join(message.Bcc, ","))
	fmt.Fprintf(os.Stdout, "subject\t%s\n", message.Subject)
	fmt.Fprintf(os.Stdout, "received_at\t%s\n", message.ReceivedAt)
	fmt.Fprintf(os.Stdout, "label_ids\t%s\n", strings.Join(message.LabelIDs, ","))
	for _, attachment := range message.Attachments {
		fmt.Fprintf(os.Stdout, "attachment\t%s\t%s\t%s\t%d\n", attachment.AttachmentID, attachment.Filename, attachment.MimeType, attachment.Size)
	}
	if message.BodyTruncated != nil {
		fmt.Fprintf(os.Stdout, "body_truncated\t%t\n", *message.BodyTruncated)
		if message.BodyText != "" {
			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, message.BodyText)
		}
	}

	return 0
}

func runSearch(socketPath string, jsonOut bool, args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	includeBody := fs.Bool("body", false, "Include message bodies")
	includeAttachments := fs.Bool("attachments", false, "Include attachment metadata without downloading bytes")
	limit := fs.Int("limit", 0, "Maximum number of results")
	pageToken := fs.String("page-token", "", "Opaque page token")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printCommandUsage(os.Stderr, "safe-gmail search [--body] [--attachments] [--limit N] [--page-token TOKEN] <query>",
			"<query> uses Gmail search syntax. If you omit an in: operator, the broker defaults to in:anywhere; for example: label:vip newer_than:7d or from:alice@example.com has:attachment. Use --attachments to include attachment_id, filename, mime type, and size without downloading bytes. Without --body or --attachments, search returns message summaries only.",
			fs)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	params, err := json.Marshal(rpc.GmailSearchMessagesParams{
		Query:              strings.Join(fs.Args(), " "),
		Limit:              *limit,
		PageToken:          *pageToken,
		IncludeBody:        *includeBody,
		IncludeAttachments: *includeAttachments,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: rpc.MethodGmailSearchMessages,
		Params: params,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		return printJSONResponse(resp)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	if *includeBody || *includeAttachments {
		var result rpc.GmailSearchMessagesResultDetail
		if err := decodeResult(resp.Result, &result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for i, message := range result.Messages {
			if i > 0 {
				fmt.Fprintln(os.Stdout)
			}
			printMessageDetail(os.Stdout, message)
		}
		fmt.Fprintf(os.Stdout, "next_page_token\t%s\n", result.NextPageToken)
		return 0
	}

	var result rpc.GmailSearchMessagesResultSummary
	if err := decodeResult(resp.Result, &result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for i, message := range result.Messages {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printMessageSummary(os.Stdout, message)
	}
	fmt.Fprintf(os.Stdout, "next_page_token\t%s\n", result.NextPageToken)
	return 0
}

func runLabelsList(socketPath string, jsonOut bool, args []string) int {
	fs := flag.NewFlagSet("labels list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printCommandUsage(os.Stderr, "safe-gmail labels list",
			"Lists Gmail labels directly from the mailbox. This label inventory is not filtered by the broker visibility policy; query labels later by name with label:<name> and cache the result locally.",
			fs)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	params, err := json.Marshal(rpc.GmailListLabelsParams{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: rpc.MethodGmailListLabels,
		Params: params,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		return printJSONResponse(resp)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	var result rpc.GmailListLabelsResult
	if err := decodeResult(resp.Result, &result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for i, label := range result.Labels {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printLabelInfo(os.Stdout, label)
	}
	return 0
}

func runThread(socketPath string, jsonOut bool, args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "search":
		return runThreadSearch(socketPath, jsonOut, args[1:])
	case "get":
		return runThreadGet(socketPath, jsonOut, args[1:])
	default:
		usage(os.Stderr)
		return 2
	}
}

func runThreadSearch(socketPath string, jsonOut bool, args []string) int {
	fs := flag.NewFlagSet("thread search", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "Maximum number of results")
	pageToken := fs.String("page-token", "", "Opaque page token")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printCommandUsage(os.Stderr, "safe-gmail thread search [--limit N] [--page-token TOKEN] <query>",
			"<query> uses Gmail search syntax. If you omit an in: operator, the broker defaults to in:anywhere; thread results are filtered to visible messages only, and label queries use label names such as label:vip.",
			fs)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	params, err := json.Marshal(rpc.GmailSearchThreadsParams{
		Query:     strings.Join(fs.Args(), " "),
		Limit:     *limit,
		PageToken: *pageToken,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: rpc.MethodGmailSearchThreads,
		Params: params,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		return printJSONResponse(resp)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	var result rpc.GmailSearchThreadsResult
	if err := decodeResult(resp.Result, &result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for i, thread := range result.Threads {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printThreadSummary(os.Stdout, thread)
	}
	fmt.Fprintf(os.Stdout, "next_page_token\t%s\n", result.NextPageToken)
	return 0
}

func runThreadGet(socketPath string, jsonOut bool, args []string) int {
	fs := flag.NewFlagSet("thread get", flag.ContinueOnError)
	includeBodies := fs.Bool("bodies", false, "Include message bodies")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printCommandUsage(os.Stderr, "safe-gmail thread get [--bodies] <thread-id>",
			"Fetches one visible thread. Without --bodies, output is message summaries only. With --bodies, each visible message includes attachment lines in the same format as get, so an agent can collect attachment_id values and then call attachment get.",
			fs)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	params, err := json.Marshal(rpc.GmailGetThreadParams{
		ThreadID:      gmailapi.NormalizeThreadID(fs.Arg(0)),
		IncludeBodies: *includeBodies,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: rpc.MethodGmailGetThread,
		Params: params,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		return printJSONResponse(resp)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	if *includeBodies {
		var result rpc.GmailGetThreadResultDetail
		if err := decodeResult(resp.Result, &result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "thread_id\t%s\n", result.Thread.ThreadID)
		for _, message := range result.Thread.Messages {
			fmt.Fprintln(os.Stdout)
			printMessageDetail(os.Stdout, message)
		}
		return 0
	}

	var result rpc.GmailGetThreadResultSummary
	if err := decodeResult(resp.Result, &result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "thread_id\t%s\n", result.Thread.ThreadID)
	for _, message := range result.Thread.Messages {
		fmt.Fprintln(os.Stdout)
		printMessageSummary(os.Stdout, message)
	}
	return 0
}

func runAttachment(socketPath string, jsonOut bool, args []string) int {
	if len(args) == 0 || args[0] != "get" {
		usage(os.Stderr)
		return 2
	}

	fs := flag.NewFlagSet("attachment get", flag.ContinueOnError)
	outputPath := fs.String("output", "", "Write decoded bytes to a file path")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printCommandUsage(os.Stderr, "safe-gmail attachment get [--output PATH] <message-id> <attachment-id>",
			"Downloads one attachment from one visible message. Obtain attachment_id from get <message-id> or thread get --bodies <thread-id>. If --output is omitted, raw attachment bytes are written to stdout; for agent workflows, prefer --output PATH or --json.",
			fs)
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return 2
	}
	if err := requireSocket(socketPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	params, err := json.Marshal(rpc.GmailGetAttachmentParams{
		MessageID:    gmailapi.NormalizeMessageID(fs.Arg(0)),
		AttachmentID: strings.TrimSpace(fs.Arg(1)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	req := rpc.Request{
		V:      rpc.Version1,
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: rpc.MethodGmailGetAttachment,
		Params: params,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := rpc.Call(ctx, socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		return printJSONResponse(resp)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		return 1
	}

	var result rpc.GmailGetAttachmentResult
	if err := decodeResult(resp.Result, &result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	data, err := base64.StdEncoding.DecodeString(result.Attachment.ContentBase64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if strings.TrimSpace(*outputPath) == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if err := writeFileAtomic(*outputPath, data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "path\t%s\n", *outputPath)
	fmt.Fprintf(os.Stdout, "bytes\t%d\n", len(data))
	return 0
}

func exitCode(resp rpc.Response) int {
	if resp.OK {
		return 0
	}
	return 1
}

func decodeResult(value any, dst any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func printJSONResponse(resp rpc.Response) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return exitCode(resp)
}

func printMessageSummary(w io.Writer, message rpc.MessageSummary) {
	fmt.Fprintf(w, "message_id\t%s\n", message.MessageID)
	fmt.Fprintf(w, "thread_id\t%s\n", message.ThreadID)
	fmt.Fprintf(w, "from\t%s\n", message.From)
	fmt.Fprintf(w, "to\t%s\n", strings.Join(message.To, ","))
	fmt.Fprintf(w, "cc\t%s\n", strings.Join(message.Cc, ","))
	fmt.Fprintf(w, "bcc\t%s\n", strings.Join(message.Bcc, ","))
	fmt.Fprintf(w, "subject\t%s\n", message.Subject)
	fmt.Fprintf(w, "received_at\t%s\n", message.ReceivedAt)
	fmt.Fprintf(w, "label_ids\t%s\n", strings.Join(message.LabelIDs, ","))
}

func printMessageDetail(w io.Writer, message rpc.MessageDetail) {
	printMessageSummary(w, message.MessageSummary)
	for _, attachment := range message.Attachments {
		fmt.Fprintf(w, "attachment\t%s\t%s\t%s\t%d\n", attachment.AttachmentID, attachment.Filename, attachment.MimeType, attachment.Size)
	}
	if message.BodyTruncated != nil {
		fmt.Fprintf(w, "body_truncated\t%t\n", *message.BodyTruncated)
		if message.BodyText != "" {
			fmt.Fprintln(w)
			fmt.Fprintln(w, message.BodyText)
		}
	}
}

func printThreadSummary(w io.Writer, thread rpc.ThreadSummary) {
	fmt.Fprintf(w, "thread_id\t%s\n", thread.ThreadID)
	fmt.Fprintf(w, "subject\t%s\n", thread.Subject)
	fmt.Fprintf(w, "participants\t%s\n", strings.Join(thread.Participants, ","))
	fmt.Fprintf(w, "snippet\t%s\n", thread.Snippet)
	fmt.Fprintf(w, "visible_message_count\t%d\n", thread.VisibleMessageCount)
	fmt.Fprintf(w, "last_message_at\t%s\n", thread.LastMessageAt)
}

func printLabelInfo(w io.Writer, label rpc.LabelInfo) {
	fmt.Fprintf(w, "label_id\t%s\n", label.LabelID)
	fmt.Fprintf(w, "label_name\t%s\n", label.LabelName)
	if label.LabelType != "" {
		fmt.Fprintf(w, "label_type\t%s\n", label.LabelType)
	}
	if label.LabelListVisibility != "" {
		fmt.Fprintf(w, "label_list_visibility\t%s\n", label.LabelListVisibility)
	}
	if label.MessageListVisibility != "" {
		fmt.Fprintf(w, "message_list_visibility\t%s\n", label.MessageListVisibility)
	}
	fmt.Fprintf(w, "messages_total\t%d\n", label.MessagesTotal)
	fmt.Fprintf(w, "messages_unread\t%d\n", label.MessagesUnread)
	fmt.Fprintf(w, "threads_total\t%d\n", label.ThreadsTotal)
	fmt.Fprintf(w, "threads_unread\t%d\n", label.ThreadsUnread)
}

func printCommandUsage(w io.Writer, command, note string, fs *flag.FlagSet) {
	fmt.Fprintf(w, "Usage:\n  %s\n", command)
	if strings.TrimSpace(note) != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, note)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags apply before the command. Set --socket /path/to.sock or SAFE_GMAIL_SOCKET, and use --json for machine-readable output.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fs.PrintDefaults()
}

func requireSocket(socketPath string) error {
	if strings.TrimSpace(socketPath) == "" {
		return fmt.Errorf("missing --socket or SAFE_GMAIL_SOCKET")
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("missing output path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(path), ".safe-gmail-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func usage(w *os.File) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock system ping")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock system info")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock labels list")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock search [--body] [--attachments] [--limit N] [--page-token TOKEN] <query>")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock get [--body] <message-id>")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock thread search [--limit N] [--page-token TOKEN] <query>")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock thread get [--bodies] <thread-id>")
	fmt.Fprintln(w, "  safe-gmail --socket /path/to.sock attachment get [--output PATH] <message-id> <attachment-id>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Notes:")
	fmt.Fprintln(w, "  search queries use Gmail query syntax.")
	fmt.Fprintln(w, "  If you omit an in: operator, searches default to in:anywhere.")
	fmt.Fprintln(w, "  Query labels by name, for example: label:vip or label:\"Kids/School\".")
	fmt.Fprintln(w, "  labels list is mailbox-wide and is not filtered by the broker visibility policy.")
	fmt.Fprintln(w, "  Bulk attachment workflow for agents: search --attachments -> attachment get <message-id> <attachment-id>.")
	fmt.Fprintln(w, "  get always prints attachment lines with attachment_id values when attachments are visible.")
	fmt.Fprintln(w, "  search without --body or --attachments does not include attachment_id values.")
	fmt.Fprintln(w, "  attachment get writes raw bytes to stdout unless you pass --output PATH.")
	fmt.Fprintln(w, "  Prefer --json for machine-readable automation.")
}
