# Safe Gmail Broker Design

This document describes a replacement for the current fork-based approach.

The recommended direction is:

- build a separate `safe-gmail` broker in its own repository
- do not fork `gogcli` as the main product surface
- reuse only selected Gmail and auth components from `gogcli`
- expose a small, typed Gmail-only API over a Unix socket
- use Unix user separation plus peer credential checks as the primary trust boundary

This doc lives in this repo only as a design reference. The implementation target should be a new repository.

## Summary

The current model tries to turn a broad CLI into a safe capability system by forwarding CLI arguments through a proxy and then relying on command-level filtering. That is the wrong boundary.

The replacement should be a narrow Gmail broker:

- one trusted broker process runs as user A
- user A owns OAuth credentials, refresh tokens, and policy files
- one or more untrusted clients run as user B
- clients talk to the broker over a Unix socket
- the broker authenticates the caller using Unix peer credentials
- the broker exposes only a fixed allowlist of Gmail methods
- the broker enforces policy on every request and every response

This keeps the trust boundary small enough to reason about and test.

## Why A New Repo Instead Of A Fork

The safe system and the general-purpose CLI have different goals.

`gogcli` is optimized for:

- broad product coverage
- flexible command surfaces
- direct human and scripting use
- fast feature expansion

A safe Gmail broker needs:

- a tiny capability surface
- typed requests instead of arbitrary CLI argument forwarding
- a single obvious trust boundary
- security review on every exposed method
- behavior that stays stable under adversarial input

Trying to make one codebase serve both purposes creates permanent drag:

- every upstream Gmail feature becomes another security review surface
- docs, release flow, and naming split between upstream compatibility and a separate security product
- policy enforcement gets distributed across handlers instead of being centralized

The right model is:

- new repo for the broker
- selected code copied or vendored from `gogcli`
- explicit tracking of what was reused and from which upstream commit

## Goals

- Provide a safe Gmail-only capability surface for untrusted local clients.
- Keep all credentials and refresh tokens owned by user A.
- Allow user B to use only filtered Gmail functionality.
- Support a two-user local setup using a Unix socket.
- Reuse stable Gmail/OAuth code where it saves work.
- Make the server-side authorization model simple enough to audit.

## Non-Goals

- Safe access to all `gogcli` commands.
- Arbitrary passthrough to upstream `gog`.
- Exposing Gmail settings/admin surfaces to untrusted clients.
- Preserving CLI compatibility with upstream `gogcli`.
- Supporting arbitrary remote callers in v1.

## Threat Model

### Trusted

- user A
- the broker process started by user A
- credential storage owned by user A
- policy configuration owned by user A

### Untrusted

- user B
- any program started by user B, including LLM agents
- any request payload received from the client

### Protected Assets

- OAuth client credentials
- refresh tokens and access tokens
- unrestricted Gmail account access
- restricted messages, threads, drafts, and attachments
- broker configuration and policy files

### Security Objective

If user B or software running as user B is compromised, the attacker should gain only the explicitly allowed filtered Gmail capabilities, not direct access to credentials or unrestricted Gmail functionality.

## Trust Boundary

### Primary Boundary: Unix User Separation

Yes, Unix users are a decent primary trust boundary for this setup.

The core model is:

- user A owns secrets and runs the broker
- user B does not read those secrets directly
- user B can only interact through the broker socket
- the broker verifies the connecting peer identity at the OS level

This is much stronger than treating a nonce file as the main security control.

### Peer Credential Verification

The broker should verify the caller using Unix peer credentials:

- Linux: `SO_PEERCRED`
- macOS and BSD: `getpeereid`

The broker should map each allowed client instance to one or more accepted UIDs.

The broker should reject requests when:

- the peer UID is not in the allowlist
- peer credential lookup fails
- the request method is not allowed

### Nonce: Optional Defense-In-Depth

A nonce can still be useful, but it should not be the primary trust boundary.

Use it only as an extra layer:

- binds a client session to a broker instance
- helps prevent accidental cross-instance connections
- can make operator mistakes noisier

Do not rely on it as the main authentication mechanism, because a readable nonce file is just a bearer secret.

### Socket Permissions

The socket should live in a directory with tight permissions.

Recommended pattern:

- directory owned by user A
- socket writable only by a dedicated group or ACL that includes user B
- broker still verifies peer UID even when filesystem permissions already restrict access

This gives two independent checks:

- filesystem access control
- peer identity verification

That is the right shape for defense-in-depth.

## High-Level Architecture

```text
user A
  owns:
    - OAuth credentials
    - refresh tokens
    - policy files
  runs:
    - safe-gmail broker

user B
  runs:
    - agent or automation client
  can:
    - connect to broker socket
    - call allowed Gmail methods
  cannot:
    - read user A keychain or token files
    - run arbitrary gogcli functionality through the broker

broker
  responsibilities:
    - authenticate peer UID
    - parse typed requests
    - call Gmail API
    - enforce access policy
    - return filtered structured responses
```

## API Shape

Do not forward CLI arguments.

The broker should expose typed methods over a small JSON RPC protocol or another similarly simple structured protocol.

Example v1 methods:

- `gmail.search_threads`
- `gmail.search_messages`
- `gmail.get_message`
- `gmail.get_thread`
- `gmail.list_drafts`
- `gmail.get_draft`
- `gmail.create_draft`
- `gmail.update_draft`
- `gmail.send_draft`
- `gmail.delete_draft`
- `gmail.send_message`
- `gmail.get_attachment`

Each method should have:

- a typed request schema
- a typed response schema
- fixed server-side authorization logic
- fixed output shaping

The client should not be allowed to request arbitrary Gmail API fields or arbitrary `format` values unless those shapes were explicitly designed and reviewed.

## Capability Surface

### Allowed In V1

- Search for messages and threads
- Read messages and threads
- Read and manage drafts
- Send mail to allowed recipients
- Download attachments from allowed messages

### Explicitly Excluded In V1

- watch
- history
- labels modify
- filters
- delegates
- forwarding
- send-as management
- vacation settings
- autoforward
- any arbitrary Gmail settings surface

These surfaces are too easy to turn into data exfiltration or policy bypasses.

## Policy Model

Start with a simple allowlist model:

- allowed email addresses
- allowed domains
- optional label whitelist
- account owner address
- optional owner-sent visibility flag

The broker enforces policy server-side for every method.

### Read Policy

A message is visible only if:

- it has an explicitly allowed label, or
- owner-sent visibility is enabled and the message was sent by the broker-owned account, or
- at least one non-owner participant is allowed

Participants should always be derived from a fixed metadata fetch that includes:

- `From`
- `To`
- `Cc`
- `Bcc`
- `labelIds`

Important: authorization must not depend on a client-selected output shape.

That means:

- do not authorize based on a raw Gmail API response requested for display
- do not let `raw` or custom metadata headers weaken the auth view
- do auth first using a fixed metadata fetch
- only then fetch or shape the output for the client

### Thread Policy

For thread reads:

- authorize each message independently
- drop restricted messages from the thread view
- return `not_found` or `restricted` if no visible messages remain

### Draft Policy

For drafts:

- filter draft listing by recipient authorization
- block get/send/update/delete if the stored draft recipients are restricted
- validate all new recipients on create and update

### Send Policy

For sending:

- validate `To`, `Cc`, and `Bcc`
- reject if any recipient is restricted
- apply the same rule to reply and reply-all after recipient expansion

## Authorization Strategy By Method

The broker should use method-specific authorization helpers with fixed behavior.

Examples:

- `authorize_message_read(message_id)`
- `authorize_thread_read(thread_id)`
- `authorize_thread_mutation(thread_id)`
- `authorize_draft_access(draft_id)`
- `authorize_send_recipients(to, cc, bcc)`

These helpers should be the only place where authorization rules live.

Handlers should not implement ad hoc checks inline.

## Use Of `gogcli` As A Component

### Recommended Reuse Model

Use `gogcli` as a source of implementation material, not as the product boundary.

That means:

- copy or vendor selected code into the new repo
- keep the copied scope intentionally small
- adapt it to the broker architecture
- stop thinking in terms of preserving the upstream CLI tree

### What "Vendoring" Means Here

In this context, vendoring means copying the code you need into your own repository so your project builds against that local copy.

That is likely better than importing `gogcli` directly because:

- much of the useful code lives under `internal/`
- the upstream repo is not structured as a stable public library
- the broker only needs a small subset

### Good Candidates To Reuse

- Gmail client construction
- OAuth token loading and refresh logic
- RFC 5322 and MIME compose helpers
- Gmail address parsing helpers
- label lookup helpers
- selected output normalization logic

### Bad Candidates To Reuse

- the full CLI tree
- global parser and flag plumbing
- current proxy model
- generic passthrough execution flow
- broad command registry

### Provenance And Update Strategy

Every vendored package should carry:

- original source path
- upstream repo URL
- upstream commit SHA
- short note about local modifications

Updates should be deliberate and narrow, not a blind sync.

## Process Model

### Broker

The broker runs as a long-lived daemon under user A.

Responsibilities:

- open Gmail service clients
- load policy files
- authenticate client peers
- serve typed requests
- log auditable method calls
- apply rate limits and size limits

### Client

The client can be:

- a small CLI
- a library
- an agent adapter

The client should:

- connect to the socket
- send typed requests
- display structured responses

The client should not contain any policy logic. All enforcement belongs on the server.

## Cross-User Setup

The two-user model is reasonable and should work well if the socket is configured carefully.

### Example Setup

- user A: `mailowner`
- user B: `agentuser`
- shared group: `safe-gmail-clients`
- broker runs as `mailowner`
- socket path owned by `mailowner:safe-gmail-clients`
- socket mode `0660`
- broker allowlists peer UID for `agentuser`

Example layout:

- Linux: `/var/run/safe-gmail/work.sock`
- macOS: a dedicated shared directory with ACLs or group permissions

The exact path matters less than:

- who owns the directory
- who can traverse it
- who can read or write the socket
- whether peer UID is verified after connect

### Why This Is Decent

This is a decent approach because it uses the OS as the main isolation mechanism:

- secrets remain with user A
- user B does not get direct filesystem access to those secrets
- the broker can make decisions based on the caller's real UID

It is not sufficient by itself if the broker exposes too much functionality. The narrow API is still mandatory.

## Defense-In-Depth

The system should not trust one control alone.

Recommended layers:

- separate Unix users
- socket directory permissions
- peer credential verification
- method allowlist
- server-side access policy enforcement
- size limits and timeouts
- structured request validation
- audit logging
- optional nonce or instance token

If one layer fails, the others still reduce blast radius.

## Data Model

Suggested top-level broker config:

```json
{
  "instance": "work",
  "account": "you@example.com",
  "allowed_client_uids": [501, 502],
  "socket_path": "/var/run/safe-gmail/work.sock",
  "policy_path": "/etc/safe-gmail/work-policy.json",
  "gmail": {
    "include_body_default": false,
    "max_body_bytes": 65536,
    "max_search_results": 100,
    "max_attachment_bytes": 24117248
  }
}
```

Policy file can stay close to the current allowlist shape, but the broker should own the semantics.

## Error Model

Use stable structured errors.

Examples:

- `unauthorized_peer`
- `method_not_allowed`
- `policy_denied`
- `not_found`
- `invalid_request`
- `gmail_api_error`
- `rate_limited`
- `internal_error`

Do not leak implementation-only details when returning errors to user B.

## Logging And Audit

The broker should log:

- peer UID
- method name
- account
- high-level target IDs
- allow or deny result
- policy reason category

The broker should not log:

- refresh tokens
- access tokens
- full message bodies by default
- attachment contents

## Testing Strategy

### Unit Tests

- policy matching
- message authorization
- thread filtering
- recipient validation
- peer credential authorization logic
- request validation

### Integration Tests

- two-user local socket tests
- allowed user B can call allowed methods
- unallowed UID is rejected
- restricted messages are filtered
- restricted recipients are blocked
- excluded methods are rejected

### Security Regression Tests

Every exposed method should have a test for:

- direct unauthorized access
- output-shape bypasses
- label whitelist behavior
- thread-level partial visibility
- reply and reply-all authorization

## Migration Plan

### Phase 1: New Repo Skeleton

- create new repo
- define config format
- define socket protocol
- define method schemas
- implement broker lifecycle

### Phase 2: Reuse Selected `gogcli` Code

- copy or vendor Gmail auth/client code
- copy or vendor MIME compose helpers
- isolate reused code into broker-owned packages

### Phase 3: Implement Safe V1 Methods

- search threads
- search messages
- get message
- get thread
- list/get/create/update/send/delete drafts
- send message
- get attachment

### Phase 4: Two-User Deployment Support

- peer UID allowlist
- socket permission model
- service scripts or launch configuration
- audit logging

### Phase 5: Review Before Expanding Surface

Do not add new Gmail methods by default.

Every new method should require:

- explicit threat review
- explicit auth design
- explicit regression tests

## Design Rules

These rules should be treated as hard constraints:

1. No arbitrary CLI passthrough.
2. No generic "run gog" mode.
3. No unreviewed Gmail admin surfaces.
4. Authorization must use fixed server-side metadata fetches.
5. Client-selected output shape must never weaken authorization.
6. Peer UID is primary auth; nonce is optional.
7. All policy enforcement happens on the server.
8. Vendored code is allowed, but only in small reviewed slices.

## Recommendation

Proceed with a separate Gmail-only broker.

Keep `gogcli` as an implementation source, not as the product boundary.

The two-user Unix setup is a good foundation, but only if:

- the broker uses peer credential checks
- the broker exposes a very small typed API
- the broker never forwards arbitrary `gogcli` functionality

That combination is much more likely to be understandable, testable, and actually safe.
