# Safe Gmail Broker V1

This document turns the high-level broker proposal into a concrete v1 design.

This v1 is scoped to:

- Linux and macOS
- one Gmail account per broker instance
- one allowed client user per broker instance
- a small client CLI only
- address, domain, and label allowlists
- persistence across reboots via system service support

This implementation should live in a new repository, not in this fork.

## Product Decision

The product should be a separate local service, tentatively named `safe-gmail`.

It has two binaries:

- `safe-gmaild`: the broker daemon run by the trusted account owner
- `safe-gmail`: the untrusted client CLI used by the agent user

The broker is the product. The client is just a transport wrapper around a typed API.

## Key V1 Decisions

### Account Model

V1 supports one Gmail account per broker instance.

That keeps the trust model and filesystem layout simple:

- one broker instance
- one service account identity in the OS
- one policy file
- one credential set
- one allowed client user

If multiple Gmail accounts are needed later, run multiple broker instances rather than multiplexing them into one server.

### Client Model

V1 supports one allowed client user per broker instance.

The broker authenticates the peer using Unix credentials and compares the peer UID against the configured allowed UID.

### Credential Store Decision

V1 should use a separate credential store, not the existing `gogcli` store.

This is the safer default.

Reasons:

- separates the broker lifecycle from the general-purpose CLI
- avoids surprising cross-tool interactions on token refresh or config changes
- avoids sharing mutable auth state with a much larger program surface
- makes filesystem permissions and operator expectations clearer
- allows the broker repo to evolve without being coupled to `gogcli` config layout

Recommended compromise:

- use a broker-owned credential store in v1
- provide an optional one-time import command from `gogcli` credentials later

That gives you safety and operational clarity without forcing duplicate manual OAuth setup forever.

## Reuse Strategy

Use `gogcli` as a code source, not as a runtime dependency.

V1 should vendor or copy only selected pieces:

- Gmail OAuth client creation
- token refresh logic
- MIME and RFC 5322 compose helpers
- Gmail address parsing helpers
- selected label lookup helpers

Do not reuse:

- the general CLI tree
- the current proxy model
- command dispatch logic
- generic config plumbing

Every reused file should carry provenance metadata:

- original repo
- original path
- upstream commit
- local modifications

## Runtime Model

### Trusted Side

Trusted side is user A.

User A:

- owns Gmail credentials
- owns the broker config and policy files
- runs `safe-gmaild`
- decides which client UID is allowed

### Untrusted Side

Untrusted side is user B.

User B:

- runs `safe-gmail`
- may run LLM agents or automation
- can only use the broker's typed Gmail methods
- never receives refresh tokens or direct Gmail credentials

### Primary Trust Boundary

Primary trust boundary is Unix user separation plus peer credential verification.

The broker must verify:

- Linux: `SO_PEERCRED`
- macOS: `getpeereid`

The broker should reject any connection if:

- peer UID does not match the configured `client_uid`
- peer credential lookup fails
- requested method is not in the allowlist

### Nonce

V1 may support an optional instance nonce, but it is not the primary authentication mechanism.

Use it only for:

- accidental cross-instance protection
- operator sanity checking
- debugging and environment validation

Do not depend on it for actual security.

## Socket Layout

The socket must live in a directory controlled by user A and accessible to user B.

Recommended default patterns:

- Linux: `/run/safe-gmail/<instance>/broker.sock`
- macOS: `/Users/<userA>/Library/Application Support/safe-gmail/<instance>/broker.sock`

Avoid user-private runtime paths such as `/run/user/<uid>/...` for cross-user Linux deployments unless you intentionally grant access to user B. A private runtime dir will usually cause `connect: permission denied` before the broker can perform peer-UID checks.

Likewise on macOS, a socket under user A's private home directory is not suitable for cross-user access unless the parent directories grant user B explicit ACL traversal.

The parent directory should:

- be owned by user A
- have restrictive permissions
- grant user B access via group membership or ACL

Suggested effective permissions:

- parent dir: `0750` or tighter with ACLs
- socket: `0660`

The broker still must verify peer UID even if filesystem permissions already limit access.

## Filesystem Layout

V1 broker-owned layout:

```text
safe-gmail/
  instances/
    work/
      broker.json
      policy.json
      oauth-client.json
      state.json
      logs/
```

Platform-specific base dirs:

- Linux config: `/etc/safe-gmail` or `~/.config/safe-gmail` for user-managed mode
- Linux state/runtime: `/var/lib/safe-gmail`, `/run/safe-gmail`
- macOS config/state: `~/Library/Application Support/safe-gmail`

For v1, user-managed per-account deployment is fine as long as the ownership model is explicit.

## Config Model

### Broker Config

Suggested `broker.json`:

```json
{
  "instance": "work",
  "account_email": "you@example.com",
  "client_uid": 502,
  "socket_path": "/run/safe-gmail/work/broker.sock",
  "log_format": "json",
  "max_body_bytes": 65536,
  "max_attachment_bytes": 26214400,
  "max_search_results": 100,
  "oauth_client_path": "/etc/safe-gmail/work/oauth-client.json",
  "policy_path": "/etc/safe-gmail/work/policy.json",
  "state_path": "/var/lib/safe-gmail/work/state.json",
  "auth_store": {
    "backend": "system"
  }
}
```

Notes:

- `client_uid` is the single allowed client user for v1
- the broker should fail fast if paths or ownership are unsafe
- `auth_store.backend` should support `system` first

### Policy File

Suggested `policy.json`:

```json
{
  "gmail": {
    "owner": "you@example.com",
    "addresses": [
      "alice@example.com",
      "bob@company.com"
    ],
    "domains": [
      "company.com"
    ],
    "labels": [
      "vip",
      "clients"
    ]
  }
}
```

Policy semantics:

- addresses are normalized to lowercase email addresses
- domains are normalized and stored without leading `@`
- labels are normalized to lowercase names for lookup
- label allowlist is an override for message visibility

## Auth Storage

### Recommended V1 Backend

Use the platform credential manager where possible:

- macOS: Keychain
- Linux: Secret Service if available

If system credential storage is not available:

- support an encrypted file backend owned by user A

The broker should own its own service/account naming in the credential store.

Example logical key:

- service: `safe-gmail`
- account: `<instance>:<email>`

### Initial Login Flow

V1 can ship a trusted-side command:

- `safe-gmaild auth login --instance work`

That command should:

- read the broker-owned OAuth client file
- perform OAuth as user A
- store refresh token in the broker-owned auth backend
- verify the token works for Gmail

### Optional Future Import

Future helper:

- `safe-gmaild auth import-gogcli`

That command should be optional and explicit. It should not silently share stores.

## Service Management

V1 should support reboot persistence.

### Linux

Support:

- `systemd --user` first
- system-wide `systemd` second if needed

Recommended initial target:

- user service under user A

Benefits:

- simpler secret ownership
- no root-owned broker required
- survives reboots when user services are enabled

Suggested unit name:

- `safe-gmaild@work.service`

### macOS

Support:

- `launchd` user agent under user A

Suggested label:

- `com.safe-gmail.work`

### Service Commands

Trusted-side CLI should help install or print service definitions:

- `safe-gmaild service print-systemd --instance work`
- `safe-gmaild service print-launchd --instance work`
- optional later: `install-service`

Printing service manifests first is safer than trying to auto-install them in v1.

## Public API

Use a small JSON protocol over Unix domain sockets.

Each request is one object:

```json
{
  "id": "req-123",
  "method": "gmail.get_message",
  "params": {
    "message_id": "18c..."
  }
}
```

Each response is one object:

```json
{
  "id": "req-123",
  "ok": true,
  "result": {
    "message": {}
  }
}
```

Error response:

```json
{
  "id": "req-123",
  "ok": false,
  "error": {
    "code": "policy_denied",
    "message": "message contains restricted participants"
  }
}
```

V1 transport framing options:

- newline-delimited JSON
- length-prefixed JSON

Recommendation:

- use length-prefixed JSON

That avoids newline ambiguity and is easier to harden.

## V1 Methods

### `gmail.search_threads`

Request:

```json
{
  "query": "from:alice@example.com",
  "limit": 20,
  "page_token": ""
}
```

Response:

```json
{
  "threads": [
    {
      "thread_id": "thread-1",
      "snippet": "hello",
      "visible_message_count": 2,
      "participants": ["alice@example.com", "you@example.com"],
      "subject": "Status",
      "last_message_at": "2026-03-12T18:20:00Z"
    }
  ],
  "next_page_token": ""
}
```

Server rules:

- fetch threads from Gmail
- fetch enough metadata to authorize visible messages
- filter restricted messages
- summarize only from visible messages

### `gmail.search_messages`

Request:

```json
{
  "query": "newer_than:7d",
  "limit": 20,
  "page_token": "",
  "include_body": false
}
```

Response:

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

Server rules:

- auth must use fixed metadata fetches
- body retrieval, if supported, happens only after auth
- enforce body byte cap

### `gmail.get_message`

Request:

```json
{
  "message_id": "msg-1",
  "include_body": true
}
```

Response:

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
    "received_at": "2026-03-12T18:20:00Z",
    "snippet": "hello",
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

Server rules:

- first fetch fixed metadata for auth
- deny if policy blocks visibility
- then optionally fetch full content

### `gmail.get_thread`

Request:

```json
{
  "thread_id": "thread-1",
  "include_bodies": false
}
```

Response:

```json
{
  "thread": {
    "thread_id": "thread-1",
    "messages": [
      {
        "message_id": "msg-1",
        "from": "alice@example.com",
        "to": ["you@example.com"],
        "subject": "Status"
      }
    ]
  }
}
```

Server rules:

- fetch thread metadata first
- authorize each message independently
- remove restricted messages
- fetch full message content only for already-visible messages when `include_bodies=true`
- if none remain, return `policy_denied`

### `gmail.list_drafts`

Request:

```json
{
  "limit": 20,
  "page_token": ""
}
```

Response:

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

Server rules:

- fetch drafts
- load enough metadata to validate recipients
- omit drafts that fail policy

### `gmail.get_draft`

Request:

```json
{
  "draft_id": "draft-1",
  "include_body": true
}
```

Response shape mirrors `gmail.get_message`, but with `draft_id`.

Server rules:

- load the draft
- validate recipients against policy
- deny access if restricted

### `gmail.create_draft`

Request:

```json
{
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Hello",
  "body_text": "hello",
  "reply_to_message_id": "",
  "attachments": []
}
```

Response:

```json
{
  "draft": {
    "draft_id": "draft-1",
    "message_id": "msg-1",
    "thread_id": ""
  }
}
```

Server rules:

- validate all recipients first
- if replying, authorize the referenced message or thread
- compose using vendored MIME helpers

### `gmail.update_draft`

Request mirrors create plus `draft_id`.

Server rules:

- validate updated recipients
- if fields are omitted, preserve existing values in a reviewed way
- never allow hidden recipient expansion to bypass policy

### `gmail.send_draft`

Request:

```json
{
  "draft_id": "draft-1"
}
```

Response:

```json
{
  "sent": {
    "message_id": "msg-1",
    "thread_id": "thread-1"
  }
}
```

Server rules:

- reload draft metadata from Gmail
- validate recipients again
- send only if policy still allows it

### `gmail.delete_draft`

Request:

```json
{
  "draft_id": "draft-1"
}
```

Response:

```json
{
  "deleted": true
}
```

Server rules:

- authorize draft access before delete

### `gmail.send_message`

Request:

```json
{
  "to": ["alice@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Hello",
  "body_text": "hello",
  "reply_to_message_id": "",
  "reply_all": false,
  "attachments": []
}
```

Response:

```json
{
  "sent": {
    "message_id": "msg-1",
    "thread_id": "thread-1"
  }
}
```

Server rules:

- expand reply or reply-all recipients on the server
- authorize referenced message/thread before reply
- validate all final recipients including `Bcc`

### `gmail.get_attachment`

Request:

```json
{
  "message_id": "msg-1",
  "attachment_id": "att-1"
}
```

Response options:

- return bytes directly with size cap
- or stream to a broker-side temp file and return metadata

Recommendation for v1:

- return bytes only up to the configured max size
- reject larger attachments with `too_large`

Response:

```json
{
  "attachment": {
    "attachment_id": "att-1",
    "filename": "report.pdf",
    "mime_type": "application/pdf",
    "size": 12345,
    "content_base64": "..."
  }
}
```

Server rules:

- authorize the parent message first
- enforce attachment byte cap

## Methods Not In V1

These are intentionally unavailable:

- `gmail.watch.*`
- `gmail.history`
- `gmail.labels.modify`
- `gmail.filters.*`
- `gmail.forwarding.*`
- `gmail.autoforward.*`
- `gmail.delegates.*`
- `gmail.sendas.*`
- `gmail.vacation.*`

The broker should respond with `method_not_allowed` for these.

## Client CLI

V1 client CLI should be intentionally small.

Suggested commands:

- `safe-gmail search <query>`
- `safe-gmail get <message-id>`
- `safe-gmail thread search <query>`
- `safe-gmail thread get <thread-id>`
- `safe-gmail attachment get <message-id> <attachment-id>`
- `safe-gmail drafts list`
- `safe-gmail drafts get <draft-id>`
- `safe-gmail drafts create ...`
- `safe-gmail drafts update ...`
- `safe-gmail drafts send <draft-id>`
- `safe-gmail drafts delete <draft-id>`
- `safe-gmail send ...`

The CLI should:

- talk only to the broker
- support `--json`
- avoid exposing broker-internal complexity

## Daemon CLI

Trusted-side daemon CLI should support:

- `safe-gmaild run --instance work`
- `safe-gmaild auth login --instance work`
- `safe-gmaild auth status --instance work`
- `safe-gmaild service print-systemd --instance work`
- `safe-gmaild service print-launchd --instance work`
- `safe-gmaild config validate --instance work`

## Package Layout

Suggested Go module layout:

```text
cmd/
  safe-gmail/
  safe-gmaild/

internal/
  broker/
    server.go
    dispatch.go
    peercred_linux.go
    peercred_darwin.go
    framing.go
  config/
    broker.go
    policy.go
    validate.go
  auth/
    login.go
    token_store.go
    keychain_darwin.go
    secretservice_linux.go
    file_fallback.go
  gmailapi/
    service.go
    labels.go
    messages.go
    threads.go
    drafts.go
    send.go
    attachments.go
  policy/
    model.go
    normalize.go
    authorize_message.go
    authorize_thread.go
    authorize_draft.go
    authorize_send.go
  rpc/
    request.go
    response.go
    methods.go
    schema.go
  methods/
    search_threads.go
    search_messages.go
    get_message.go
    get_thread.go
    list_drafts.go
    get_draft.go
    create_draft.go
    update_draft.go
    send_draft.go
    delete_draft.go
    send_message.go
    get_attachment.go
  mimeutil/
  logging/
  service/
    systemd.go
    launchd.go
  vendored/
    gogcli/
```

Rules:

- `broker/` owns transport and caller authentication
- `methods/` owns typed RPC handlers only
- `policy/` owns all authorization logic
- `gmailapi/` wraps Gmail API calls and nothing else
- `vendored/gogcli/` contains only copied code slices

## Authorization Implementation Rules

These rules should be enforced in code review:

1. Every handler must call an authorization helper before returning protected data.
2. Authorization helpers must use fixed metadata fetches.
3. Client-selected output shape must not affect authorization inputs.
4. All reply and reply-all flows must include `Bcc` in final recipient validation.
5. Thread mutation methods are not exposed until thread authorization is explicitly designed.
6. No method may return or log OAuth secrets.

## Logging

Use JSON logs by default.

Recommended fields:

- timestamp
- instance
- peer_uid
- method
- request_id
- result
- policy_reason
- gmail_account
- latency_ms

Do not log:

- access tokens
- refresh tokens
- message body text
- attachment content

## Limits

Recommended v1 defaults:

- max body bytes: `65536`
- max attachment bytes: `26214400`
- max search results per request: `100`
- request size cap: `1 MiB`
- response size cap: `32 MiB`
- server read timeout: low seconds
- Gmail API timeout: explicit bounded transport timeouts

These should all be configurable by user A.

## Testing Plan

### Unit Tests

- policy normalization
- label resolution
- message authorization
- thread filtering
- draft recipient validation
- reply/reply-all recipient expansion
- request validation

### Platform Tests

- peer credential check on Linux
- peer credential check on macOS
- socket ownership validation

### Integration Tests

- broker started as user A
- client runs as user B
- allowed user succeeds
- wrong UID is denied
- restricted message is denied
- restricted draft is hidden
- restricted recipient send is denied
- excluded methods return `method_not_allowed`

### Security Regression Suite

Include explicit tests for:

- `raw` and metadata-shaping bypass attempts
- missing header bypass attempts
- label whitelist authorization
- `Bcc` authorization on reply and send
- attachment access through restricted messages
- socket access from wrong UID

## Delivery Order

### Milestone 1

- repo skeleton
- broker config loading
- Unix socket transport
- peer credential verification
- request/response framing

### Milestone 2

- broker-owned auth store
- OAuth login flow
- Gmail client construction
- address/domain/label policy parsing

### Milestone 3

- `gmail.search_messages`
- `gmail.get_message`
- `gmail.get_thread`
- `gmail.get_attachment`

### Milestone 4

- draft methods
- send methods
- reply/reply-all logic

### Milestone 5

- client CLI
- `systemd --user` output
- `launchd` output
- deployment docs

## Recommendation

Proceed with:

- a separate `safe-gmail` repo
- a broker-owned credential store
- optional later import from `gogcli`
- one account and one client UID per broker instance
- a tiny typed Gmail method surface
- peer credential checks as the real trust boundary
- service support via `systemd --user` and `launchd`

That gives you a design that matches your two-user setup while staying small enough to secure.
