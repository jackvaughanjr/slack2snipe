# slack2snipe

[![Latest Release](https://img.shields.io/github/v/release/jackvaughanjr/slack2snipe)](https://github.com/jackvaughanjr/slack2snipe/releases/latest) [![Go Version](https://img.shields.io/github/go-mod/go-version/jackvaughanjr/slack2snipe)](go.mod) [![License](https://img.shields.io/github/license/jackvaughanjr/slack2snipe)](LICENSE) [![Build](https://github.com/jackvaughanjr/slack2snipe/actions/workflows/release.yml/badge.svg)](https://github.com/jackvaughanjr/slack2snipe/actions/workflows/release.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/jackvaughanjr/slack2snipe)](https://goreportcard.com/report/github.com/jackvaughanjr/slack2snipe) [![Downloads](https://img.shields.io/github/downloads/jackvaughanjr/slack2snipe/total)](https://github.com/jackvaughanjr/slack2snipe/releases)

Syncs active [Slack](https://slack.com) workspace members into [Snipe-IT](https://snipeitapp.com)
as license seat assignments.

Full members and multi-channel guests (the two billable Slack member types) are
synced into a single license record. Single-channel guests, bots, and deleted
accounts are excluded. Each seat's notes field records the member type
(`full member` or `multi-channel guest`) so asset managers can audit usage.

> Part of the [\*2snipe](https://github.com/jackvaughanjr?tab=repositories&q=2snipe) integration family, inspired by [CampusTech](https://github.com/CampusTech)'s Snipe-IT integrations.

## How it works

1. Fetches all billable members from your Slack workspace via `users.list`
2. Finds or creates a license in Snipe-IT named `Slack <Plan> (<domain>)` — e.g. `Slack Business+ (gallatin-ai)`
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
   - `team:read`
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

The license name is auto-resolved as `Slack <Plan> (<domain>)` — for example,
`Slack Business+ (your-workspace)`. The Slack API does not return the billing
plan, so set `slack.plan: "Business+"` in settings.yaml to include it. Without
it the name falls back to `Slack (your-workspace)`. Override the name entirely
with `snipe_it.license_name`.

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

## Building a container image

A `Dockerfile` is included for containerized deployments. To build locally:

```bash
docker build -t slack2snipe:latest .
```

For automated scheduling via Cloud Run Jobs, use [snipemgr](https://github.com/jackvaughanjr/2snipe-manager) — it handles image publishing, secret storage, and scheduling. See the [snipemgr README](https://github.com/jackvaughanjr/2snipe-manager#building-container-images-for-cloud-run-jobs) for complete GCP setup instructions.

---

## Version History

| Version | Key changes |
|---------|-------------|
| v1.2.0 | Make Snipe-IT API rate limit configurable via `sync.rate_limit_ms` and `SNIPE_RATE_LIMIT_MS` env var |
| v1.1.4 | Pre-compact documentation cleanup |
| v1.1.3 | Documented plan auto-detection investigation as a TODO |
| v1.1.2 | Fixed license name — added `slack.plan` config key; `team.info` does not return billing plan for paid workspaces |
| v1.1.1 | Added missing `team:read` OAuth scope |
| v1.1.0 | Auto find/create Salesforce as supplier in Snipe-IT when none configured |
| v1.0.1 | Changed license name format to `Slack <Plan> (<slug>)` |
| v1.0.0 | Initial scaffold — sync Slack workspace members into Snipe-IT license seats |
