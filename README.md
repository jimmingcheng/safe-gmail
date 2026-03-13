# safe-gmail

`safe-gmail` is a Gmail-only broker for local two-user setups.

The intended model is:

- user A runs `safe-gmaild` and owns Gmail credentials plus policy files
- user B runs `safe-gmail` and can only access the broker's typed Gmail API
- the broker authenticates the caller using Unix peer credentials

This repository is intentionally separate from `gogcli`. It may vendor small pieces of `gogcli`, but it is not a fork of the full CLI.

## Current Status

This repo is in an early but functional read-only stage.

Currently implemented:

- framed Unix socket transport
- peer UID verification on Linux and macOS
- `system.ping`
- `system.info`
- trusted-side config loading and validation
- trusted-side OAuth login with broker-owned credential storage
- broker-side policy loading and label resolution
- `gmail.search_threads`
- `gmail.search_messages`
- `gmail.get_message`
- `gmail.get_thread`
- `gmail.get_attachment`
- service manifest generation for `systemd --user` and `launchd`

Not implemented yet:

- Gmail draft/send methods

## Layout

- `cmd/safe-gmail/`: untrusted client CLI
- `cmd/safe-gmaild/`: trusted daemon CLI
- `internal/broker/`: socket server and peer credential checks
- `internal/config/`: broker config loading and validation
- `internal/rpc/`: wire protocol, framing, and client transport
- `docs/`: design and bootstrap documents

## Install

`safe-gmail` currently installs from source. There is not yet a Homebrew formula or system package.

Requirements:

- Go `1.25.8` or later
- `make`
- a standard `install` tool

Recommended shared install for a two-user setup:

```sh
make build
sudo make install PREFIX=/usr/local
```

That installs:

- `/usr/local/bin/safe-gmail`
- `/usr/local/bin/safe-gmaild`

Per-user install without `sudo`:

```sh
make build
make install PREFIX="$HOME/.local"
```

That installs into `~/.local/bin`. Make sure that directory is on your `PATH`.

For this project, install the binaries to a stable absolute path before generating `systemd` or `launchd` service files. The printed service manifests should point at the installed `safe-gmaild`, not at a binary inside a git checkout.

## Socket Permissions

In a two-user deployment, `socket_mode` only controls the socket file itself. User B also needs permission to traverse the parent directory that contains the socket.

For Linux, do not put a cross-user socket under `/run/user/<uid>/...`. That directory is normally private to user A, so user B will get `connect: permission denied` before the broker can even check peer UID.

Recommended Linux pattern:

- create a dedicated shared parent directory
- make user A the owner
- grant user B access via a shared group or ACL
- keep the socket itself at `0660`

Example:

```sh
sudo groupadd --system safe-gmail
sudo usermod -aG safe-gmail userA
sudo usermod -aG safe-gmail userB
sudo install -d -o userA -g safe-gmail -m 2750 /var/tmp/safe-gmail
```

Then set:

```json
{
  "socket_path": "/var/tmp/safe-gmail/work.sock",
  "socket_mode": "0660"
}
```

If you change group membership, log out and back in before testing from user B.

On macOS, the same rule applies: do not put a cross-user socket inside a private per-user directory unless you also grant user B ACL access to the parent directories. Prefer a shared location such as `/Users/Shared/safe-gmail` with explicit ACLs.

## Example Config

```json
{
  "instance": "work",
  "account_email": "you@example.com",
  "client_uid": 501,
  "socket_path": "/var/tmp/safe-gmail/work.sock",
  "socket_mode": "0660",
  "max_body_bytes": 65536,
  "max_attachment_bytes": 26214400,
  "max_search_results": 100,
  "oauth_client_path": "/Users/you/.config/safe-gmail/work/oauth-client.json",
  "policy_path": "/Users/you/.config/safe-gmail/work/policy.json",
  "state_path": "/Users/you/.local/state/safe-gmail/work/state.json",
  "auth_store": {
    "backend": "system"
  }
}
```

Example `policy.json`:

```json
{
  "gmail": {
    "owner": "you@example.com",
    "addresses": [
      "alice@example.com"
    ],
    "domains": [
      "company.com"
    ],
    "labels": [
      "vip"
    ]
  }
}
```

Auth store note:

- `auth_store.backend = "system"` uses Keychain on macOS and Secret Service on Linux
- `auth_store.backend = "file"` uses an encrypted keyring directory and requires `SAFE_GMAIL_KEYRING_PASSWORD`

## Bootstrap Run

Before first start in a cross-user Linux setup, create the socket parent directory with the correct group or ACLs. If you let `safe-gmaild` create the parent directory inside a user-private runtime area, user B may not be able to connect at all.

Log in once as the trusted broker owner:

```sh
safe-gmaild auth login --config /path/to/broker.json
```

Start the daemon:

```sh
safe-gmaild run --config /path/to/broker.json
```

Print a persistent user service:

```sh
safe-gmaild service print-systemd --config /path/to/broker.json > ~/.config/systemd/user/safe-gmaild@work.service
systemctl --user daemon-reload
systemctl --user enable --now safe-gmaild@work.service
```

If you want the broker to keep running across reboots even before you log in again on Linux, enable user lingering once:

```sh
loginctl enable-linger "$USER"
```

On macOS:

```sh
safe-gmaild service print-launchd --config /path/to/broker.json > ~/Library/LaunchAgents/com.safe-gmail.work.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.safe-gmail.work.plist
launchctl enable gui/$(id -u)/com.safe-gmail.work
```

From the allowed client UID:

```sh
safe-gmail --socket /var/tmp/safe-gmail/work.sock system ping
safe-gmail --socket /var/tmp/safe-gmail/work.sock system info
safe-gmail --socket /var/tmp/safe-gmail/work.sock search newer_than:7d
safe-gmail --socket /var/tmp/safe-gmail/work.sock thread search from:alice@example.com
safe-gmail --socket /var/tmp/safe-gmail/work.sock get --body 18c...
safe-gmail --socket /var/tmp/safe-gmail/work.sock thread get 18c...
safe-gmail --socket /var/tmp/safe-gmail/work.sock attachment get --output ./report.pdf 18c... att-1
```

## Docs

The main design docs are:

- `docs/safe-gmail-broker-design.md`
- `docs/safe-gmail-broker-v1.md`
- `docs/safe-gmail-rpc-schema.md`
- `docs/safe-gmail-repo-bootstrap.md`
