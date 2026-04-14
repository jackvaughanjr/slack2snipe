# CONTEXT.md — slack2snipe

## Purpose

Syncs billable Slack workspace members into Snipe-IT as license seat assignments.
"Billable members" are full members and multi-channel guests — the two membership
types charged by Slack. Single-channel guests, bots, and deleted accounts are excluded.

A single Snipe-IT license record covers all billable member types. The license name
defaults to the workspace name fetched from the Slack API, and can optionally include
the workspace domain slug in parentheses (e.g. `My Company (my-company)`).

## Auth

A Slack bot token (`xoxb-...`) from an app installed in the target workspace.

Required OAuth scopes:
- `users:read` — enumerate workspace members and their metadata
- `users:read.email` — read email addresses (required for Snipe-IT user matching)

## Slack API endpoints used

| Endpoint     | Tier   | Purpose                                        |
|--------------|--------|------------------------------------------------|
| `auth.test`  | Tier 4 | Validate token on startup                      |
| `team.info`  | Tier 3 | Fetch workspace name, domain slug, and plan    |
| `users.list` | Tier 2 | Paginated member enumeration (pageSize=200)    |

Rate limiting: `users.list` is Tier 2 (≤20 req/min). The client enforces
1 request per 3 seconds, giving comfortable headroom for most workspaces.
Large workspaces (>2,000 members) may take a minute or more to enumerate.

## Member types

| Type                  | `is_restricted` | `is_ultra_restricted` | Billed | Synced |
|-----------------------|-----------------|----------------------|--------|--------|
| Full member           | false           | false                | Yes    | Yes    |
| Multi-channel guest   | true            | false                | Yes    | Yes    |
| Single-channel guest  | true            | true                 | No     | No     |
| Bot / deleted         | —               | —                    | No     | No     |

The `notes` field on each Snipe-IT license seat records the member type:
`member_type: full member` or `member_type: multi-channel guest`.

## License name

The Snipe-IT license name defaults to the workspace name from `team.info`.
Set `slack.include_workspace_slug: true` to append the domain slug:
`My Company (my-company)`. Override entirely with `snipe_it.license_name`.

A single license record covers all billable member types (full + multi-channel guests).

## Config schema

| Key                              | Env var           | Required | Default           | Description                              |
|----------------------------------|-------------------|----------|-------------------|------------------------------------------|
| `slack.bot_token`                | `SLACK_BOT_TOKEN` | Yes      | —                 | xoxb-... bot token                       |
| `slack.include_workspace_slug`   | —                 | No       | false             | Append domain slug to license name       |
| `slack.webhook_url`              | `SLACK_WEBHOOK`   | No       | —                 | Incoming webhook for notifications       |
| `snipe_it.url`                   | `SNIPE_URL`       | Yes      | —                 | Snipe-IT base URL                        |
| `snipe_it.api_key`               | `SNIPE_TOKEN`     | Yes      | —                 | Snipe-IT API key                         |
| `snipe_it.license_name`          | —                 | No       | workspace name    | License name override                    |
| `snipe_it.license_category_id`   | —                 | Yes      | —                 | Category ID for license creation         |
| `snipe_it.license_seats`         | —                 | No       | 0 (= active count)| Purchased seat count override            |
| `snipe_it.license_manufacturer_id` | —               | No       | 0 (= auto)        | Manufacturer ID; auto find/create "Slack"|
| `snipe_it.license_supplier_id`   | —                 | No       | 0                 | Supplier ID; omitted if 0                |
| `sync.dry_run`                   | —                 | No       | false             | Simulate without changes                 |
| `sync.force`                     | —                 | No       | false             | Re-sync unchanged seat notes             |

## File structure

```
main.go
cmd/
  root.go          # cobra root, viper init, logging init (PersistentPreRunE)
  sync.go          # sync command + notification dispatch
  test.go          # test command: validate connections, report state
internal/
  slackapi/
    client.go      # Slack Web API client (auth.test, team.info, users.list)
  slack/
    client.go      # Incoming webhook notification client (verbatim)
  snipeit/
    client.go      # Snipe-IT API client (verbatim)
  sync/
    syncer.go      # core sync logic
    result.go      # Result struct
.github/
  workflows/
    release.yml
go.mod
settings.example.yaml
README.md
CONTEXT.md
.gitignore
```

## Gotchas

- **Cursor-based pagination**: `users.list` uses `response_metadata.next_cursor`.
  An empty string signals the final page. Never request a fixed offset.
- **Bot filtering**: every workspace has a Slackbot system user (`name == "slackbot"`)
  and may have integrated app bots (`is_bot: true`). Both must be excluded.
- **Deleted vs. deactivated**: Slack exposes only `deleted: true` — there is no
  separate "deactivated" state. Any `deleted: true` member is excluded.
- **Email scope**: `profile.email` is only populated when the `users:read.email` scope
  is granted. Without it, all emails are empty strings and Snipe-IT matching fails.
  The sync warns and skips any member with an empty email rather than querying Snipe-IT.
- **Single-channel guest identification**: a user is a single-channel guest when
  both `is_restricted: true` AND `is_ultra_restricted: true`. A multi-channel guest
  has `is_restricted: true` but `is_ultra_restricted: false`.
- **Single workspace only**: one bot token = one workspace. Slack Connect channels
  surface members from foreign workspaces, but `users.list` only returns members of
  the installed workspace.

## TODO

- Enterprise Grid support: Enterprise Grid workspaces use `admin.users.list` (not
  `users.list`), require an org-level token, and span multiple workspaces. A separate
  variant would enumerate workspaces first, then members per workspace, and could
  maintain one license record per workspace or one aggregate license.
