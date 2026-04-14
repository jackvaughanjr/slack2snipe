# slack2snipe

Syncs active [Slack](https://slack.com) workspace members into [Snipe-IT](https://snipeitapp.com)
as license seat assignments.

Full members and multi-channel guests (the two billable Slack member types) are
synced into a single license record. Single-channel guests, bots, and deleted
accounts are excluded. Each seat's notes field records the member type
(`full member` or `multi-channel guest`) so asset managers can audit usage.

## How it works

1. Fetches all billable members from your Slack workspace via `users.list`
2. Finds or creates a license in Snipe-IT (named after your workspace by default)
3. Checks out seats for active members — creating Snipe-IT users if `--create-users` is set
4. Checks in seats for members who have left the workspace
5. Records each member's type in the seat notes field

## Installation

**Download a pre-built binary** from the [latest release](https://github.com/jackvaughanjr/slack2snipe/releases/latest):

    # macOS (Apple Silicon)
    curl -L https://github.com/jackvaughanjr/slack2snipe/releases/latest/download/slack2snipe-darwin-arm64 -o slack2snipe
    chmod +x slack2snipe

    # Linux (amd64)
    curl -L https://github.com/jackvaughanjr/slack2snipe/releases/latest/download/slack2snipe-linux-amd64 -o slack2snipe
    chmod +x slack2snipe

    # Linux (arm64)
    curl -L https://github.com/jackvaughanjr/slack2snipe/releases/latest/download/slack2snipe-linux-arm64 -o slack2snipe
    chmod +x slack2snipe

Or build from source:

    git clone https://github.com/jackvaughanjr/slack2snipe
    cd slack2snipe
    go build -o slack2snipe .

## Setup

### 1. Create a Slack app

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and create a new app
   (choose **From scratch**, select your workspace).
2. Under **OAuth & Permissions → Scopes → Bot Token Scopes**, add:
   - `users:read`
   - `users:read.email`
3. Click **Install to Workspace** and copy the **Bot User OAuth Token** (`xoxb-...`).

### 2. Configure

Copy `settings.example.yaml` to `settings.yaml` and fill in your values:

```yaml
slack:
  bot_token: "xoxb-your-token-here"

snipe_it:
  url: "https://your-snipe-it-instance.example.com"
  api_key: "your-snipe-it-api-key"
  license_category_id: 1  # ID of the Software category in Snipe-IT
```

All values can be set via environment variables — see `settings.example.yaml`
for the full list.

### License name

The license defaults to your workspace name (fetched from the Slack API).
Set `slack.include_workspace_slug: true` to append the workspace domain, e.g.
`Acme Corp (acme-corp)`. Override the name entirely with `snipe_it.license_name`.

## Usage

Validate your configuration and API connections:

```
./slack2snipe test
```

Preview a sync without making changes:

```
./slack2snipe sync --dry-run --verbose
```

Run a full sync:

```
./slack2snipe sync
```

Sync a single user:

```
./slack2snipe sync --email user@example.com
```

### Global flags

| Flag | Description |
|------|-------------|
| `--config` | Path to config file (default: `settings.yaml`) |
| `-v, --verbose` | INFO-level logging |
| `-d, --debug` | DEBUG-level logging |
| `--log-file` | Append logs to a file |
| `--log-format` | `text` (default) or `json` |

### Sync flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Simulate without making changes |
| `--force` | Re-sync even if seat notes appear up to date |
| `--email` | Sync a single user by email address |
| `--create-users` | Create Snipe-IT accounts for unmatched Slack members |
| `--no-slack` | Suppress Slack notifications for this run |
