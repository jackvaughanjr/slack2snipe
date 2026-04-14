package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackvaughanjr/slack2snipe/internal/slackapi"
	"github.com/jackvaughanjr/slack2snipe/internal/snipeit"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Validate API connections and report current state",
	RunE:  runTest,
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	botToken := viper.GetString("slack.bot_token")
	if botToken == "" {
		return fatal("slack.bot_token is required in settings.yaml (or SLACK_BOT_TOKEN env var)")
	}
	snipeURL := viper.GetString("snipe_it.url")
	if snipeURL == "" {
		return fatal("snipe_it.url is required in settings.yaml (or SNIPE_URL env var)")
	}
	snipeKey := viper.GetString("snipe_it.api_key")
	if snipeKey == "" {
		return fatal("snipe_it.api_key is required in settings.yaml (or SNIPE_TOKEN env var)")
	}

	slackClient := slackapi.NewClient(botToken)
	snipeClient := snipeit.NewClient(snipeURL, snipeKey)

	// --- Slack ---
	fmt.Println("=== Slack ===")
	if err := slackClient.ValidateToken(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Slack error: %v\n", err)
		return err
	}
	fmt.Println("Token: valid")

	slog.Info("fetching workspace info")
	info, err := slackClient.GetWorkspaceInfo(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Slack error: %v\n", err)
		return err
	}
	fmt.Printf("Workspace: %s (domain: %s, plan: %s)\n", info.Name, info.Domain, info.Plan)

	slog.Info("fetching active members")
	users, err := slackClient.ListActiveUsers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Slack error: %v\n", err)
		return err
	}

	var fullMembers, multiChannelGuests int
	for _, u := range users {
		if u.IsRestricted {
			multiChannelGuests++
		} else {
			fullMembers++
		}
	}
	fmt.Printf("Billable members: %d (full: %d, multi-channel guests: %d)\n",
		len(users), fullMembers, multiChannelGuests)

	// --- Snipe-IT ---
	fmt.Println("\n=== Snipe-IT ===")
	licenseName := viper.GetString("snipe_it.license_name")
	if licenseName == "" {
		licenseName = info.Name
		if viper.GetBool("slack.include_workspace_slug") && info.Domain != "" {
			licenseName = fmt.Sprintf("%s (%s)", licenseName, info.Domain)
		}
	}

	slog.Info("looking up license in Snipe-IT", "license", licenseName)
	lic, err := snipeClient.FindLicenseByName(ctx, licenseName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Snipe-IT error: %v\n", err)
		return err
	}
	if lic == nil {
		fmt.Printf("License %q: not found\n", licenseName)
	} else {
		slog.Debug("license detail", "id", lic.ID, "seats", lic.Seats, "free", lic.FreeSeatsCount)
		fmt.Printf("License %q: id=%d seats=%d free=%d\n",
			lic.Name, lic.ID, lic.Seats, lic.FreeSeatsCount)
	}

	return nil
}
