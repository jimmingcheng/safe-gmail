# Safe Gmail Repo Bootstrap

This document defines the initial repository shape and build order for the new `safe-gmail` project.

It assumes the decisions already made:

- separate repo
- separate credential store
- Linux and macOS
- one Gmail account per broker instance
- one allowed client UID per broker instance
- small CLI only
- address, domain, and label allowlists
- reboot-persistent services

## Repository Goal

The initial repo should be able to do this end to end:

1. user A configures an instance
2. user A logs in with Gmail OAuth
3. user A starts `safe-gmaild`
4. user B runs `safe-gmail system info`
5. user B can call allowed Gmail read/send/draft methods
6. user B cannot connect if the UID is wrong
7. methods outside the allowlist are impossible to invoke

## Proposed Repository Name

Use a new repository, for example:

- `safe-gmail`

Suggested Go module path:

- `github.com/<owner>/safe-gmail`

Do not depend on `gogcli` as a Go module dependency for core functionality.

## Toolchain

Recommended:

- Go version pinned in `go.mod`
- `gofumpt`
- `goimports`
- `golangci-lint`
- `make`

Use the same basic hygiene model as `gogcli`, but keep the repo smaller.

## Minimal Dependencies

Suggested starting dependencies:

- `google.golang.org/api/gmail/v1`
- `golang.org/x/oauth2`
- `golang.org/x/oauth2/google`
- `github.com/99designs/keyring`

Why these:

- Gmail API client
- OAuth
- cross-platform credential storage with Keychain and Secret Service support

Keep third-party dependencies minimal beyond that.

## Repository Layout

Initial layout:

```text
.
├── cmd/
│   ├── safe-gmail/
│   └── safe-gmaild/
├── internal/
│   ├── auth/
│   ├── broker/
│   ├── config/
│   ├── gmailapi/
│   ├── logging/
│   ├── methods/
│   ├── mimeutil/
│   ├── policy/
│   ├── rpc/
│   ├── service/
│   └── vendored/
│       └── gogcli/
├── docs/
├── scripts/
├── testdata/
├── Makefile
├── go.mod
└── README.md
```

## Package Responsibilities

### `cmd/safe-gmail`

Client CLI only.

Responsibilities:

- parse user-facing CLI flags
- connect to broker socket
- marshal RPC request
- print response in text or JSON

Must not:

- hold OAuth logic
- contain policy logic
- call Gmail APIs directly

### `cmd/safe-gmaild`

Daemon CLI for trusted side.

Responsibilities:

- run broker
- validate config
- perform auth login
- inspect auth status
- print service templates

### `internal/rpc`

Owns protocol types.

Files to start with:

- `envelope.go`
- `errors.go`
- `methods.go`
- `types_common.go`
- `types_gmail.go`

This package should contain only protocol types and validation helpers.

### `internal/broker`

Owns transport and caller authentication.

Files to start with:

- `server.go`
- `listener.go`
- `framing.go`
- `dispatch.go`
- `peercred_linux.go`
- `peercred_darwin.go`

Responsibilities:

- accept socket connections
- read and write frames
- inspect peer credentials
- pass typed requests into method handlers

### `internal/config`

Owns instance config and policy parsing.

Files to start with:

- `broker.go`
- `policy.go`
- `paths_linux.go`
- `paths_darwin.go`
- `validate.go`

Responsibilities:

- load instance config
- normalize and validate paths
- validate secure ownership and permissions where possible

### `internal/auth`

Owns broker credential storage and OAuth login.

Files to start with:

- `login.go`
- `store.go`
- `keyring.go`
- `token.go`

Responsibilities:

- broker-owned refresh token storage
- login flow for user A
- refresh token retrieval for Gmail API client creation

### `internal/gmailapi`

Owns Gmail API calls.

Files to start with:

- `service.go`
- `labels.go`
- `messages.go`
- `threads.go`
- `drafts.go`
- `send.go`
- `attachments.go`

Responsibilities:

- fetch metadata and content from Gmail
- no policy decisions
- no CLI concerns

### `internal/policy`

Owns all authorization and filtering.

Files to start with:

- `model.go`
- `normalize.go`
- `authorize_message.go`
- `authorize_thread.go`
- `authorize_draft.go`
- `authorize_send.go`

This package is the heart of the security model.

Nothing else should implement ad hoc policy checks.

### `internal/methods`

Owns RPC handlers.

Files to start with:

- `system_ping.go`
- `system_info.go`
- `gmail_search_threads.go`
- `gmail_search_messages.go`
- `gmail_get_message.go`
- `gmail_get_thread.go`
- `gmail_list_drafts.go`
- `gmail_get_draft.go`
- `gmail_create_draft.go`
- `gmail_update_draft.go`
- `gmail_send_draft.go`
- `gmail_delete_draft.go`
- `gmail_send_message.go`
- `gmail_get_attachment.go`

Each handler should:

- validate params
- call `gmailapi`
- call `policy`
- return RPC shapes

### `internal/service`

Owns printed service manifests.

Files to start with:

- `systemd.go`
- `launchd.go`

Responsibilities:

- render `systemd --user` unit files
- render `launchd` plist files

### `internal/vendored/gogcli`

Contains copied code slices with provenance notes.

Do not dump large unrelated directories here.

Organize by function:

- `auth/`
- `mime/`
- `gmail/`

Each file should start with a provenance comment.

## What To Vendor First

Likely first candidates from this repo:

- Gmail service setup helpers
- OAuth helper logic
- MIME composition helpers
- address parsing helpers
- label lookup helpers

Good source areas to inspect from the current repo:

- `internal/googleapi/`
- `internal/googleauth/`
- selected parts of `internal/cmd/gmail_mime.go`
- selected parts of `internal/cmd/gmail_compose.go`
- selected parts of `internal/cmd/gmail_send.go`
- selected parts of `internal/accessctl/`

Do not copy them blindly. Extract only the logic that survives the new architecture cleanly.

## Initial Make Targets

Suggested `Makefile` targets:

- `make build`
- `make test`
- `make lint`
- `make fmt`
- `make ci`

Optional:

- `make run-daemon`
- `make run-client`

## Initial README Shape

The new repo README should immediately explain:

- this is a Gmail-only broker
- it is not a safe wrapper around all `gogcli`
- it uses Unix user separation
- it exposes a fixed allowlist of Gmail methods

Keep the README short and operator-focused.

## Config Bootstrap Commands

Trusted-side CLI should eventually support:

- `safe-gmaild init --instance work`
- `safe-gmaild config validate --instance work`
- `safe-gmaild auth login --instance work`
- `safe-gmaild auth status --instance work`
- `safe-gmaild run --instance work`
- `safe-gmaild service print-systemd --instance work`
- `safe-gmaild service print-launchd --instance work`

V1 does not need an interactive TUI.

## Bootstrap Sequence

### PR 1: Repo Skeleton

Deliver:

- repo structure
- `go.mod`
- `Makefile`
- empty binaries
- doc set copied in
- base lint and test commands

Success criteria:

- `make build`
- `make test`
- `make lint`

### PR 2: RPC Core

Deliver:

- framing implementation
- request/response envelopes
- stable error codes
- `system.ping`
- `system.info`

Success criteria:

- client can connect locally
- server returns structured responses
- oversized frames are rejected

### PR 3: Peer Credentials And Socket Hardening

Deliver:

- Linux peer cred support
- macOS peer cred support
- config field `client_uid`
- socket ownership validation

Success criteria:

- allowed UID passes
- wrong UID gets `unauthorized_peer`

### PR 4: Auth Store And Login

Deliver:

- broker-owned keyring integration
- OAuth login flow
- Gmail service construction

Success criteria:

- user A can log in
- broker can create Gmail client using stored refresh token

### PR 5: Policy Core

Deliver:

- policy file format
- normalization
- label resolution
- fixed metadata auth helpers

Success criteria:

- unit tests cover address/domain/label allowlists
- auth helpers do not depend on caller-selected output shape

### PR 6: Message Read Methods

Deliver:

- `gmail.search_messages`
- `gmail.get_message`
- `gmail.get_thread`
- `gmail.search_threads`

Success criteria:

- blocked messages are hidden or denied correctly
- visible summaries never leak restricted participants

### PR 7: Draft And Send Methods

Deliver:

- `gmail.list_drafts`
- `gmail.get_draft`
- `gmail.create_draft`
- `gmail.update_draft`
- `gmail.send_draft`
- `gmail.delete_draft`
- `gmail.send_message`

Success criteria:

- recipient validation works
- reply and reply-all include `Bcc` in server-side auth decisions

### PR 8: Attachment Method

Deliver:

- `gmail.get_attachment`
- size cap handling
- client write-to-file support

Success criteria:

- parent message auth always happens first
- oversized attachments return `too_large`

### PR 9: Client CLI

Deliver:

- small CLI commands
- `--json`
- stable text output

Success criteria:

- all exposed methods usable from CLI
- CLI never talks to Gmail directly

### PR 10: Service Support

Deliver:

- `systemd --user` template printer
- `launchd` plist printer
- deployment docs

Success criteria:

- user A can install a persistent per-instance service on Linux and macOS

## Service Templates

### Linux `systemd --user`

Suggested unit skeleton:

```ini
[Unit]
Description=Safe Gmail broker (%i)
After=network-online.target

[Service]
ExecStart=%h/bin/safe-gmaild run --instance %i
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
```

Notes:

- prefer user service first
- avoid root-owned service in v1 unless there is a strong reason

### macOS `launchd`

Suggested plist fields:

- `Label`
- `ProgramArguments`
- `RunAtLoad`
- `KeepAlive`
- `StandardOutPath`
- `StandardErrorPath`

Keep the daemon running as user A.

## Testing Layout

Suggested test directories:

- package-local `*_test.go`
- `testdata/instances/work/...`
- integration helpers for local socket tests

Need explicit tests for:

- peer credential checks
- strict allowlist behavior
- reply/reply-all recipient handling
- label whitelist behavior
- denied methods

## Security Review Checklist For New Code

Every PR adding a method should answer:

1. What exact capability is exposed?
2. What server-side auth helper is used?
3. Does auth use fixed metadata fetches?
4. Can any caller-controlled output shape weaken auth?
5. Are restricted objects omitted or denied consistently?
6. Does the method expose any admin or exfiltration path?

## First Week Build Goal

At the end of the first implementation week, the repo should have:

- working broker socket
- peer UID enforcement
- broker-owned auth login
- `system.ping`
- `system.info`
- one safe read method, ideally `gmail.get_message`

That is enough to validate the architecture before building the rest.

## Recommendation

Start the new repo with:

- RPC core first
- peer credential checks second
- auth store third
- one read method before any send/draft complexity

That order proves the trust boundary early and prevents the project from becoming another large CLI before the broker core is solid.
