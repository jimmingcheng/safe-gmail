# safe-gmail

`safe-gmail` lets you give an untrusted local user or AI agent read-only access to a restricted slice of one Gmail account.

The trusted user runs `safe-gmaild`, owns the Gmail OAuth token, and defines the mail policy. The untrusted user runs `safe-gmail` and can only use the broker's filtered Gmail API over a local Unix socket.

The broker gives the untrusted side a small read-only Gmail surface:

- `system ping`
- `system info`
- message search
- thread search
- message get
- thread get
- attachment get

Filtering is server-side and based on fixed message metadata. You can expose mail by:

- one configured Gmail visibility label
- optional `allow_owner_sent` for everything sent by the owner account

A message is visible if it has the configured visibility label, or if it is owner-sent when that option is enabled. Thread results are filtered message-by-message.

It does not expose Gmail settings, unrestricted Gmail API access, or direct OAuth credentials.

## Security Model

This project is only useful as a security boundary if the Gmail owner and the agent run as different Unix users.

- The owner user runs `safe-gmaild` and owns the Gmail token, config, and policy files.
- The agent user runs `safe-gmail` and connects to a Unix socket.
- The broker verifies the connecting Unix UID before serving requests.
- The policy decides which messages are visible.

Do not treat this as a hard boundary if any of these are true:

- the agent runs as the same Unix user as the owner
- the agent user can `sudo`
- the agent user can run commands as the owner
- the agent user can read or edit the owner's files, service config, or secret store

Recommended deployment:

- owner user: a normal non-root account that owns the Gmail account
- agent user: a separate non-sudo account used by the AI agent or automation
- service: `systemd --user` on Ubuntu/Linux, `launchd` on macOS

## Ubuntu Quick Start

This is the recommended setup for a real boundary on Ubuntu.

Example names used below:

- owner user: `mailowner`
- agent user: `agentuser`
- broker instance: `default`
- Gmail account: `you@gmail.com`

### 1. Install the binaries

Requirements:

- Go `1.25.8` or later
- `make`

```sh
git clone https://github.com/jimmingcheng/safe-gmail.git
cd safe-gmail
make build
sudo make install PREFIX=/usr/local
```

This installs:

- `/usr/local/bin/safe-gmail`
- `/usr/local/bin/safe-gmaild`

### 2. Create a shared socket directory

Run this once as a sudo-capable admin:

```sh
sudo groupadd --system safe-gmail || true
sudo usermod -aG safe-gmail mailowner
sudo usermod -aG safe-gmail agentuser
sudo install -d -o mailowner -g safe-gmail -m 2750 /var/tmp/safe-gmail
```

Important:

- log out and back in after changing group membership
- do not put a cross-user socket under `/run/user/<uid>/...`
- do not put a cross-user socket inside the owner's private home directory

### 3. Create the owner config files

Run the rest of the setup as the owner user.

Create a config directory:

```sh
mkdir -p ~/.config/safe-gmail/default
chmod 700 ~/.config/safe-gmail ~/.config/safe-gmail/default
```

Find the agent UID:

```sh
id -u agentuser
```

Save your Google OAuth client JSON as:

```text
~/.config/safe-gmail/default/oauth-client.json
```

Use a standard Google OAuth client JSON file with the Gmail API enabled. The least surprising option is a Google Cloud OAuth client for a Desktop app.

Create `~/.config/safe-gmail/default/broker.json`:

```json
{
  "instance": "default",
  "account_email": "you@gmail.com",
  "client_uid": 1001,
  "socket_path": "/var/tmp/safe-gmail/default.sock",
  "socket_mode": "0660",
  "oauth_client_path": "/home/mailowner/.config/safe-gmail/default/oauth-client.json",
  "policy_path": "/home/mailowner/.config/safe-gmail/default/policy.json"
}
```

Replace:

- `you@gmail.com` with the Gmail account the broker should use
- `1001` with the output of `id -u agentuser`
- `/home/mailowner/...` with the real home directory of the owner user

Create `~/.config/safe-gmail/default/policy.json`:

```json
{
  "gmail": {
    "owner": "you@gmail.com",
    "allow_owner_sent": true,
    "visibility_label": "safe-gmail-visible"
  }
}
```

Policy rules:

- `visibility_label`: the Gmail label that marks messages as safe to expose through the broker
- `allow_owner_sent`: keeps copies of mail you sent visible without requiring the visibility label

Important:

- `visibility_label` is a normal Gmail label name, so values like `safe-gmail-visible`, `donna`, or `Kids/School` are valid
- apply the visibility label to every message you want the broker to expose, typically with Gmail filters
- other Gmail labels can still exist for your own organization, but the broker only treats `visibility_label` as the safety gate
- `allow_owner_sent` is the safer way to keep your sent mail visible

### 4. Validate the config

```sh
safe-gmaild config validate --config ~/.config/safe-gmail/default/broker.json
```

### 5. Authorize Gmail once

```sh
safe-gmaild auth login --config ~/.config/safe-gmail/default/broker.json
```

The command prints a Google auth URL. Open it in a browser, log into the same Gmail account as `account_email`, copy the final redirect URL, and paste it back into the terminal.

If you authorize the wrong Gmail account, the login fails instead of silently storing the wrong token.

### 6. Install the user service

```sh
mkdir -p ~/.config/systemd/user
safe-gmaild service print-systemd \
  --config ~/.config/safe-gmail/default/broker.json \
  --binary /usr/local/bin/safe-gmaild \
  > ~/.config/systemd/user/safe-gmaild@default.service
systemctl --user daemon-reload
systemctl --user enable --now safe-gmaild@default.service
```

If you want the broker to survive reboots before you log in again:

```sh
loginctl enable-linger "$USER"
```

### 7. Verify from the agent user

Log in as the agent user and set the socket path:

```sh
export SAFE_GMAIL_SOCKET=/var/tmp/safe-gmail/default.sock
```

Test the connection:

```sh
safe-gmail system ping
safe-gmail system info
```

Try a search:

```sh
safe-gmail search "newer_than:7d"
safe-gmail thread search "from:alice@example.com"
```

Fetch a message or thread:

```sh
safe-gmail get <message-id>
safe-gmail get --body <message-id>
safe-gmail thread get <thread-id>
safe-gmail thread get --bodies <thread-id>
safe-gmail attachment get --output ./file.bin <message-id> <attachment-id>
```

## For AI Agents

The cleanest setup is to give the agent user this environment variable:

```sh
export SAFE_GMAIL_SOCKET=/var/tmp/safe-gmail/default.sock
```

For machine-readable responses, use `--json`:

```sh
safe-gmail --json system info
safe-gmail --json search "label:vip newer_than:7d"
safe-gmail --json get --body <message-id>
safe-gmail --json thread get --bodies <thread-id>
```

Practical guidance for agents:

- treat the broker as the only allowed Gmail interface
- do not ask for raw Gmail OAuth tokens or browser cookies
- prefer `--json` when another tool will parse the output
- expect policy filtering: search results may omit messages that exist in Gmail

## Updating An Existing Install

On the owner machine:

```sh
cd /path/to/safe-gmail
git pull
make build
sudo make install PREFIX=/usr/local
```

If you are upgrading specifically for owner-sent visibility:

- replace legacy `addresses`, `domains`, and `labels` policy entries with `"visibility_label": "safe-gmail-visible"` or another Gmail label name you control
- add `"allow_owner_sent": true` to `policy.json` if you want sent mail visible without the visibility label
- create or update Gmail filters so the visibility label is applied to all mail you want exposed

Then restart the service:

```sh
systemctl --user restart safe-gmaild@default.service
```

Replace `default` if your instance name is different.

## Common Mistakes

`connect: permission denied`

- the socket directory is not traversable by the agent user
- use a shared directory like `/var/tmp/safe-gmail`
- do not use `/run/user/<uid>/...` for cross-user access

`unauthorized_peer`

- the process is running as the wrong Unix user
- `client_uid` in `broker.json` does not match the real agent UID

`authorized as X, expected Y`

- you logged into the wrong Gmail account during `auth login`
- fix `account_email` or re-run login with the correct account

`open keyring timed out`

- Linux Secret Service is not available for the owner user
- switch `broker.json` to `"auth_store": {"backend": "file", "file_dir": "/home/mailowner/.local/state/safe-gmail/default/keyring"}`
- set `SAFE_GMAIL_KEYRING_PASSWORD` before running `auth login` and the service
- keep that directory owner-only

Agent user is a sudoer

- the boundary is not meaningful
- use a separate non-sudo user if you actually want protection

## macOS Note

The same security model applies on macOS, but use `launchd` instead of `systemd`:

```sh
mkdir -p ~/Library/LaunchAgents
safe-gmaild service print-launchd \
  --config ~/.config/safe-gmail/default/broker.json \
  --binary /usr/local/bin/safe-gmaild \
  > ~/Library/LaunchAgents/com.safe-gmail.default.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.safe-gmail.default.plist
launchctl enable gui/$(id -u)/com.safe-gmail.default
```

If you care about the security boundary on macOS, the agent user should still be a separate non-admin user.
