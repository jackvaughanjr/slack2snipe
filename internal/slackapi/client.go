package slackapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	apiBase  = "https://slack.com/api"
	pageSize = 200
)

// Client is a Slack Web API client.
// users.list is Tier 2 (≤20 req/min); the rate limiter targets 1 req/3s.
type Client struct {
	token      string
	httpClient *http.Client
	limiter    *rate.Limiter
}

// NewClient returns a new Client using the provided bot token (xoxb-...).
func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    rate.NewLimiter(rate.Every(3*time.Second), 1),
	}
}

// WorkspaceInfo contains the workspace name, domain slug, and plan.
type WorkspaceInfo struct {
	Name   string
	Domain string
	Plan   string
}

// User represents a Slack workspace member.
type User struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	IsBot             bool    `json:"is_bot"`
	Deleted           bool    `json:"deleted"`
	IsRestricted      bool    `json:"is_restricted"`
	IsUltraRestricted bool    `json:"is_ultra_restricted"`
	Profile           Profile `json:"profile"`
}

// Profile contains user contact and display info returned by users.list.
type Profile struct {
	Email     string `json:"email"`
	RealName  string `json:"real_name"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// LicenseName returns the canonical Snipe-IT license name for a workspace.
// If Plan is set: "Slack <Plan> (<domain>)" — e.g. "Slack Business+ (gallatin-ai)".
// If Plan is empty: "Slack (<domain>)" — e.g. "Slack (gallatin-ai)".
//
// The Slack team.info API does not reliably return the billing plan for paid
// workspaces. Set slack.plan in settings.yaml to include it in the license name.
func LicenseName(info *WorkspaceInfo) string {
	if info.Plan == "" {
		return fmt.Sprintf("Slack (%s)", info.Domain)
	}
	displayPlan := strings.ToUpper(info.Plan[:1]) + info.Plan[1:]
	return fmt.Sprintf("Slack %s (%s)", displayPlan, info.Domain)
}

// MemberType returns the human-readable billing membership type for a user.
// Only call this for users that passed the billable filter (not ultra_restricted).
func MemberType(u User) string {
	if u.IsRestricted {
		return "multi-channel guest"
	}
	return "full member"
}

// ValidateToken calls auth.test to confirm the bot token is valid.
func (c *Client) ValidateToken(ctx context.Context) error {
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.get(ctx, apiBase+"/auth.test", &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("Slack token invalid: %s", resp.Error)
	}
	return nil
}

// GetWorkspaceInfo fetches workspace name, domain slug, and plan via team.info.
func (c *Client) GetWorkspaceInfo(ctx context.Context) (*WorkspaceInfo, error) {
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Team  struct {
			Name   string `json:"name"`
			Domain string `json:"domain"`
			Plan   string `json:"plan"`
		} `json:"team"`
	}
	if err := c.get(ctx, apiBase+"/team.info", &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("Slack team.info failed: %s", resp.Error)
	}
	return &WorkspaceInfo{
		Name:   resp.Team.Name,
		Domain: resp.Team.Domain,
		Plan:   resp.Team.Plan,
	}, nil
}

// ListActiveUsers returns all billable workspace members: full members and
// multi-channel guests. The following are excluded:
//   - Deleted accounts (deleted: true)
//   - Bots (is_bot: true, or name == "slackbot")
//   - Single-channel guests (is_ultra_restricted: true) — not billed by Slack
//
// Uses cursor-based pagination; each page fetches up to 200 members.
func (c *Client) ListActiveUsers(ctx context.Context) ([]User, error) {
	var all []User
	cursor := ""
	for {
		endpoint := fmt.Sprintf("%s/users.list?limit=%d", apiBase, pageSize)
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}

		var resp struct {
			OK               bool   `json:"ok"`
			Error            string `json:"error"`
			Members          []User `json:"members"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.get(ctx, endpoint, &resp); err != nil {
			return nil, err
		}
		if !resp.OK {
			return nil, fmt.Errorf("Slack users.list failed: %s", resp.Error)
		}

		for _, u := range resp.Members {
			// Exclude bots (is_bot covers most; slackbot has is_bot=false in some workspaces)
			if u.IsBot || u.Name == "slackbot" {
				continue
			}
			// Exclude deleted accounts
			if u.Deleted {
				continue
			}
			// Exclude single-channel guests — not billed, not synced
			if u.IsUltraRestricted {
				continue
			}
			all = append(all, u)
		}

		cursor = resp.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}
	return all, nil
}

func (c *Client) get(ctx context.Context, endpoint string, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack GET %s: HTTP %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
