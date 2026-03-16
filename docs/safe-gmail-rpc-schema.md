# Safe Gmail RPC Schema

This document defines the exact v1 wire contract for the `safe-gmail` broker.

It is intentionally narrow:

- local Unix domain socket only
- one request, one response
- typed JSON messages
- no arbitrary passthrough to upstream tools

This schema is the contract between:

- `safe-gmaild`: trusted daemon
- `safe-gmail`: untrusted client CLI

## Protocol Scope

V1 covers:

- `system.ping`
- `system.info`
- `gmail.list_labels`
- `gmail.search_threads`
- `gmail.search_messages`
- `gmail.get_message`
- `gmail.get_thread`
- `gmail.get_attachment`

All other methods must return `method_not_allowed`.

## Transport

### Socket Type

- Unix domain stream socket
- local machine only

### Framing

Each message frame is:

- 4-byte unsigned big-endian payload length
- UTF-8 JSON payload

The payload length limit is broker-configurable.

Recommended v1 default:

- request frame max: `1 MiB`
- response frame max: `32 MiB`

### Message Ordering

V1 is request/response over a connection-oriented stream.

Rules:

- client may send one request at a time per connection
- server returns exactly one response per request
- client should close idle connections promptly

No multiplexing is required in v1.

`id` still exists for correlation and logging.

## Versioning

Every request and response includes a protocol version field:

- `v`

V1 value:

- `1`

If the server does not support the requested version, it must return:

- `unsupported_version`

## Common Envelope

### Request

```json
{
  "v": 1,
  "id": "req-123",
  "method": "gmail.get_message",
  "params": {
    "message_id": "18c..."
  }
}
```

Fields:

- `v`
  - required
  - integer
  - must be `1`
- `id`
  - required
  - string
  - client-generated correlation ID
  - max length: `64`
- `method`
  - required
  - string
  - exact method name
- `params`
  - required
  - JSON object
  - method-specific

### Success Response

```json
{
  "v": 1,
  "id": "req-123",
  "ok": true,
  "result": {
    "message": {}
  }
}
```

Fields:

- `v`
  - required
  - integer
- `id`
  - required
  - echoes request `id`
- `ok`
  - required
  - boolean
  - must be `true`
- `result`
  - required
  - JSON object
  - method-specific

### Error Response

```json
{
  "v": 1,
  "id": "req-123",
  "ok": false,
  "error": {
    "code": "policy_denied",
    "message": "message contains restricted participants",
    "retryable": false
  }
}
```

Fields:

- `v`
  - required
  - integer
- `id`
  - required
  - echoes request `id` when available
- `ok`
  - required
  - boolean
  - must be `false`
- `error`
  - required
  - object

Error object fields:

- `code`
  - required
  - stable machine-readable string
- `message`
  - required
  - human-readable summary
- `retryable`
  - required
  - boolean
- `details`
  - optional
  - JSON object with structured extra fields

## Standard Types

### `timestamp`

- string
- RFC3339 in UTC

Example:

```json
"2026-03-12T18:20:00Z"
```

### `email_address`

- string
- valid RFC 5322 mailbox address
- request input may contain display names
- server normalizes to lowercase bare address form in stored and returned values

Example:

```json
"alice@example.com"
```

### `label_name`

- string
- Gmail label name
- request input may use mixed case
- server normalizes lookup case-insensitively by name
- returned `label_ids` remain case-sensitive opaque Gmail IDs

### `page_token`

- string
- opaque
- empty string means first page

### `body_text`

- string
- plain-text body
- UTF-8

### `body_html`

- string
- HTML body
- UTF-8

### `bytes_base64`

- string
- base64-encoded bytes

## Standard Error Codes

These codes are stable protocol values.

- `unsupported_version`
- `invalid_request`
- `invalid_params`
- `unauthorized_peer`
- `method_not_allowed`
- `not_found`
- `policy_denied`
- `too_large`
- `rate_limited`
- `gmail_api_error`
- `internal_error`

Expected meanings:

- `unsupported_version`
  - request `v` not supported
- `invalid_request`
  - malformed envelope or framing-level issue
- `invalid_params`
  - params object has missing, extra, or invalid fields
- `unauthorized_peer`
  - peer UID is not allowed
- `method_not_allowed`
  - method is outside the exposed allowlist
- `not_found`
  - object does not exist or has no visible content
- `policy_denied`
  - object exists but is blocked by policy
- `too_large`
  - body or attachment exceeds configured size limit
- `rate_limited`
  - local broker rate limit hit
- `gmail_api_error`
  - upstream Gmail/API failure
- `internal_error`
  - unexpected server failure

## System Methods

### `system.ping`

Request:

```json
{
  "v": 1,
  "id": "req-1",
  "method": "system.ping",
  "params": {}
}
```

Success result:

```json
{
  "pong": true
}
```

### `system.info`

Purpose:

- let the client confirm protocol compatibility
- surface broker limits for CLI behavior

Request:

```json
{
  "v": 1,
  "id": "req-2",
  "method": "system.info",
  "params": {}
}
```

Success result:

```json
{
  "service": "safe-gmaild",
  "protocol_version": 1,
  "instance": "work",
  "account_email": "you@example.com",
  "max_body_bytes": 65536,
  "max_attachment_bytes": 26214400,
  "max_search_results": 100,
  "search_query_syntax": "gmail",
  "label_query_mode": "name",
  "label_list_method": "gmail.list_labels",
  "label_list_scope": "mailbox",
  "methods": [
    "system.ping",
    "system.info",
    "gmail.list_labels",
    "gmail.search_threads",
    "gmail.search_messages",
    "gmail.get_message",
    "gmail.get_thread",
    "gmail.get_attachment"
  ]
}
```

## Common Gmail Shapes

### `attachment_meta`

```json
{
  "attachment_id": "att-1",
  "filename": "report.pdf",
  "mime_type": "application/pdf",
  "size": 12345
}
```

Fields:

- `attachment_id`
  - Gmail attachment ID
- `filename`
  - filename as presented to user
- `mime_type`
  - MIME type string
- `size`
  - byte count

### `message_summary`

```json
{
  "message_id": "msg-1",
  "thread_id": "thread-1",
  "from": "alice@example.com",
  "to": ["you@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Status",
  "snippet": "hello",
  "received_at": "2026-03-12T18:20:00Z",
  "label_ids": ["INBOX"]
}
```

### `message_detail`

```json
{
  "message_id": "msg-1",
  "thread_id": "thread-1",
  "from": "alice@example.com",
  "to": ["you@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Status",
  "snippet": "hello",
  "received_at": "2026-03-12T18:20:00Z",
  "label_ids": ["INBOX"],
  "body_text": "hello world",
  "body_truncated": false,
  "attachments": [
    {
      "attachment_id": "att-1",
      "filename": "report.pdf",
      "mime_type": "application/pdf",
      "size": 12345
    }
  ]
}
```

Rules:

- `body_text` omitted unless requested
- `body_truncated` omitted unless `body_text` is present
- `attachments` may be empty but should still be present in detail shapes

### `thread_summary`

```json
{
  "thread_id": "thread-1",
  "subject": "Status",
  "participants": ["alice@example.com", "you@example.com"],
  "snippet": "hello",
  "visible_message_count": 2,
  "last_message_at": "2026-03-12T18:20:00Z"
}
```

### `thread_detail`

```json
{
  "thread_id": "thread-1",
  "messages": [
    {
      "message_id": "msg-1",
      "thread_id": "thread-1",
      "from": "alice@example.com",
      "to": ["you@example.com"],
      "cc": [],
      "bcc": [],
      "subject": "Status",
      "snippet": "hello",
      "received_at": "2026-03-12T18:20:00Z",
      "label_ids": ["INBOX"]
    }
  ]
}
```

### `draft_summary`

```json
{
  "draft_id": "draft-1",
  "message_id": "msg-1",
  "thread_id": "thread-1",
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Draft"
}
```

### `draft_detail`

```json
{
  "draft_id": "draft-1",
  "message": {
    "message_id": "msg-1",
    "thread_id": "thread-1",
    "from": "you@example.com",
    "to": ["alice@example.com"],
    "cc": [],
    "bcc": [],
    "subject": "Draft",
    "snippet": "",
    "received_at": "2026-03-12T18:20:00Z",
    "label_ids": [],
    "body_text": "hello",
    "body_truncated": false,
    "attachments": []
  }
}
```

## Gmail Methods

### `gmail.list_labels`

Request params:

```json
{}
```

Fields:

- no fields in v1

Success result:

```json
{
  "labels": [
    {
      "label_id": "Label_1",
      "label_name": "vip",
      "label_type": "user",
      "label_list_visibility": "labelShow",
      "message_list_visibility": "show",
      "messages_total": 12,
      "messages_unread": 1,
      "threads_total": 9,
      "threads_unread": 1
    },
    {
      "label_id": "INBOX",
      "label_name": "INBOX",
      "label_type": "system",
      "label_list_visibility": "labelShow",
      "message_list_visibility": "show",
      "messages_total": 42,
      "messages_unread": 3,
      "threads_total": 31,
      "threads_unread": 2
    }
  ]
}
```

Rules:

- list the mailbox's direct Gmail label inventory
- this method is not filtered by the broker visibility policy
- this method is intended for occasional inventory and local caching, not per-query use
- future label queries should use `label_name`, not returned `label_id`

### `gmail.search_threads`

Request params:

```json
{
  "query": "from:alice@example.com",
  "limit": 20,
  "page_token": ""
}
```

Fields:

- `query`
  - required
  - string
  - Gmail query syntax
  - if no `in:` mailbox operator is present, the broker adds `in:anywhere`
- `limit`
  - optional
  - integer
  - default: `20`
  - max: broker `max_search_results`
- `page_token`
  - optional
  - string
  - default: `""`

Success result:

```json
{
  "threads": [
    {
      "thread_id": "thread-1",
      "subject": "Status",
      "participants": ["alice@example.com", "you@example.com"],
      "snippet": "hello",
      "visible_message_count": 2,
      "last_message_at": "2026-03-12T18:20:00Z"
    }
  ],
  "next_page_token": ""
}
```

Server requirements:

- must authorize visible messages using fixed metadata
- must not leak participants from filtered messages
- if a thread has no visible messages, it must be omitted from results

### `gmail.search_messages`

Request params:

```json
{
  "query": "newer_than:7d",
  "limit": 20,
  "page_token": "",
  "include_body": false
}
```

Fields:

- `query`
  - required
  - string
  - Gmail query syntax
  - if no `in:` mailbox operator is present, the broker adds `in:anywhere`
- `limit`
  - optional
  - integer
  - default: `20`
- `page_token`
  - optional
  - string
  - default: `""`
- `include_body`
  - optional
  - boolean
  - default: `false`

Success result:

```json
{
  "messages": [
    {
      "message_id": "msg-1",
      "thread_id": "thread-1",
      "from": "alice@example.com",
      "to": ["you@example.com"],
      "cc": [],
      "bcc": [],
      "subject": "Status",
      "snippet": "hello",
      "received_at": "2026-03-12T18:20:00Z",
      "label_ids": ["INBOX"]
    }
  ],
  "next_page_token": ""
}
```

Rules:

- if `include_body` is `true`, returned items use `message_detail`
- auth must happen before body retrieval
- `body_text` may be truncated to `max_body_bytes`

### `gmail.get_message`

Request params:

```json
{
  "message_id": "msg-1",
  "include_body": true
}
```

Fields:

- `message_id`
  - required
  - string
- `include_body`
  - optional
  - boolean
  - default: `false`

Success result:

```json
{
  "message": {
    "message_id": "msg-1",
    "thread_id": "thread-1",
    "from": "alice@example.com",
    "to": ["you@example.com"],
    "cc": [],
    "bcc": [],
    "subject": "Status",
    "snippet": "hello",
    "received_at": "2026-03-12T18:20:00Z",
    "label_ids": ["INBOX"],
    "body_text": "hello world",
    "body_truncated": false,
    "attachments": [
      {
        "attachment_id": "att-1",
        "filename": "report.pdf",
        "mime_type": "application/pdf",
        "size": 12345
      }
    ]
  }
}
```

Rules:

- auth fetch must always use fixed headers:
  - `From`
  - `To`
  - `Cc`
  - `Bcc`
  - `labelIds`
- client cannot ask for raw Gmail payloads in v1

### `gmail.get_thread`

Request params:

```json
{
  "thread_id": "thread-1",
  "include_bodies": false
}
```

Fields:

- `thread_id`
  - required
  - string
- `include_bodies`
  - optional
  - boolean
  - default: `false`

Success result:

```json
{
  "thread": {
    "thread_id": "thread-1",
    "messages": [
      {
        "message_id": "msg-1",
        "thread_id": "thread-1",
        "from": "alice@example.com",
        "to": ["you@example.com"],
        "cc": [],
        "bcc": [],
        "subject": "Status",
        "snippet": "hello",
        "received_at": "2026-03-12T18:20:00Z",
        "label_ids": ["INBOX"]
      }
    ]
  }
}
```

Rules:

- returned `messages` array contains only visible messages
- if no visible messages remain, return `policy_denied`
- if `include_bodies=true`, fetch full message content only after visibility is established

### `gmail.list_drafts`

Request params:

```json
{
  "limit": 20,
  "page_token": ""
}
```

Fields:

- `limit`
  - optional
  - integer
  - default: `20`
- `page_token`
  - optional
  - string

Success result:

```json
{
  "drafts": [
    {
      "draft_id": "draft-1",
      "message_id": "msg-1",
      "thread_id": "thread-1",
      "to": ["alice@example.com"],
      "cc": [],
      "bcc": [],
      "subject": "Draft"
    }
  ],
  "next_page_token": ""
}
```

Rules:

- broker must revalidate stored recipients before returning each draft
- restricted drafts must be omitted

### `gmail.get_draft`

Request params:

```json
{
  "draft_id": "draft-1",
  "include_body": true
}
```

Fields:

- `draft_id`
  - required
  - string
- `include_body`
  - optional
  - boolean
  - default: `false`

Success result:

```json
{
  "draft": {
    "draft_id": "draft-1",
    "message": {
      "message_id": "msg-1",
      "thread_id": "thread-1",
      "from": "you@example.com",
      "to": ["alice@example.com"],
      "cc": [],
      "bcc": [],
      "subject": "Draft",
      "snippet": "",
      "received_at": "2026-03-12T18:20:00Z",
      "label_ids": [],
      "body_text": "hello",
      "body_truncated": false,
      "attachments": []
    }
  }
}
```

Rules:

- broker must validate recipients before returning the draft

### `gmail.create_draft`

Request params:

```json
{
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Hello",
  "body_text": "hello",
  "body_html": "<p>hello</p>",
  "reply_to_message_id": "",
  "reply_strategy": "manual",
  "attachments": []
}
```

Fields:

- `to`
  - required
  - array of `email_address`
- `cc`
  - optional
  - array of `email_address`
  - default: `[]`
- `bcc`
  - optional
  - array of `email_address`
  - default: `[]`
- `subject`
  - required
  - string
- `body_text`
  - optional
  - string
- `body_html`
  - optional
  - string
- `reply_to_message_id`
  - optional
  - string
  - default: `""`
- `reply_strategy`
  - optional
  - string enum:
    - `none`
    - `manual`
    - `reply_all`
  - default: `none`
- `attachments`
  - optional
  - array of `send_attachment`
  - default: `[]`

`send_attachment` shape:

```json
{
  "filename": "note.txt",
  "mime_type": "text/plain",
  "content_base64": "aGVsbG8="
}
```

Rules:

- at least one of `body_text` or `body_html` must be present
- `reply_strategy = reply_all` requires `reply_to_message_id`
- when `reply_strategy = reply_all`, client-supplied `to` and `cc` must be empty
- broker derives final reply-all recipients server-side
- all final recipients including derived `Bcc`-relevant auth context must be validated before creating the draft
- each attachment payload must fit within request size limits

Success result:

```json
{
  "draft": {
    "draft_id": "draft-1",
    "message_id": "msg-1",
    "thread_id": "thread-1"
  }
}
```

### `gmail.update_draft`

V1 update semantics are full replacement of mutable content.

The client must send the full intended draft content.

Request params:

```json
{
  "draft_id": "draft-1",
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Updated",
  "body_text": "updated body",
  "body_html": "",
  "reply_to_message_id": "",
  "reply_strategy": "manual",
  "attachments": []
}
```

Fields:

- same as `gmail.create_draft`
- plus `draft_id` required

Rules:

- server may fetch existing draft for thread context
- recipient validation happens against the full replacement content
- omitted arrays default to empty
- omitted body fields are treated as empty
- at least one of `body_text` or `body_html` must remain present after validation

Success result:

```json
{
  "draft": {
    "draft_id": "draft-1",
    "message_id": "msg-1",
    "thread_id": "thread-1"
  }
}
```

### `gmail.send_draft`

Request params:

```json
{
  "draft_id": "draft-1"
}
```

Success result:

```json
{
  "sent": {
    "message_id": "msg-1",
    "thread_id": "thread-1"
  }
}
```

Rules:

- broker must reload the stored draft from Gmail
- recipients must be revalidated immediately before send

### `gmail.delete_draft`

Request params:

```json
{
  "draft_id": "draft-1"
}
```

Success result:

```json
{
  "deleted": true
}
```

Rules:

- broker must authorize draft access before delete

### `gmail.send_message`

Request params:

```json
{
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Hello",
  "body_text": "hello",
  "body_html": "<p>hello</p>",
  "reply_to_message_id": "",
  "reply_strategy": "none",
  "attachments": []
}
```

Fields:

- same shape as `gmail.create_draft`, except no `draft_id`

Rules:

- at least one final recipient must exist after server-side reply expansion
- `reply_strategy = reply_all` requires `reply_to_message_id`
- broker derives reply-all recipients on the server
- broker validates final `To`, `Cc`, and `Bcc`
- auth for the replied-to message must use fixed metadata including `Bcc`

Success result:

```json
{
  "sent": {
    "message_id": "msg-1",
    "thread_id": "thread-1"
  }
}
```

### `gmail.get_attachment`

Request params:

```json
{
  "message_id": "msg-1",
  "attachment_id": "att-1"
}
```

Fields:

- `message_id`
  - required
  - string
- `attachment_id`
  - required
  - string

Success result:

```json
{
  "attachment": {
    "attachment_id": "att-1",
    "filename": "report.pdf",
    "mime_type": "application/pdf",
    "size": 12345,
    "content_base64": "JVBERi0xLjQK..."
  }
}
```

Rules:

- broker must authorize the parent message before fetching the attachment
- if attachment size exceeds `max_attachment_bytes`, return `too_large`

## Validation Rules

The broker must reject unknown top-level envelope keys only if strict mode is enabled internally. V1 recommendation:

- ignore unknown envelope keys
- reject unknown `params` keys with `invalid_params`

That gives future evolution room while keeping per-method contracts strict.

Method-level validation rules:

- arrays must not contain duplicate normalized recipients
- empty recipient strings are invalid
- invalid email syntax is `invalid_params`
- `subject` may be empty only if explicitly supported
  - v1 recommendation: require non-empty `subject` for create/send/update
- attachment `content_base64` must decode successfully

## Policy-Related Semantics

These are wire-visible expectations:

- if the object exists but is hidden by policy:
  - server should prefer `policy_denied`
- if the object is absent or all visible content is gone:
  - server may return `not_found`

The implementation should choose stable behavior and document it for operators.

Recommended v1 behavior:

- direct object lookup blocked by policy: `policy_denied`
- thread with zero visible messages: `policy_denied`
- search methods: omit blocked objects instead of surfacing errors

## Logging Requirements

For every request, the broker should log:

- `id`
- `method`
- peer UID
- result code
- latency

For policy-denied requests, log at least:

- policy reason category
- target object identifier if available

Never log:

- attachment bytes
- message body text
- refresh or access tokens

## Compatibility Rules

Any change to:

- required fields
- field types
- field meaning
- error codes
- transport framing

must bump the protocol version.

Adding:

- optional response fields
- new methods
- new `system.info` metadata

is allowed within v1 if old clients still work.
