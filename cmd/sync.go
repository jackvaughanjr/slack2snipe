package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackvaughanjr/slack2snipe/internal/slack"
	"github.com/jackvaughanjr/slack2snipe/internal/slackapi"
	"github.com/jackvaughanjr/slack2snipe/internal/snipeit"
	"github.com/jackvaughanjr/slack2snipe/internal/sync"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync active Slack workspace members into Snipe-IT license seats",
	RunE:  runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().Bool("dry-run", false, "simulate without making changes")
	syncCmd.Flags().Bool("force", false, "re-sync even if notes appear up to date")
	syncCmd.Flags().String("email", "", "sync a single user by email address")
	syncCmd.Flags().Bool("create-users", false, "create Snipe-IT accounts for unmatched users")
	syncCmd.Flags().Bool("no-slack", false, "suppress Slack notifications for this run")

	_ = viper.BindPFlag("sync.dry_run", syncCmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("sync.force", syncCmd.Flags().Lookup("force"))
	_ = viper.BindPFlag("sync.create_users", syncCmd.Flags().Lookup("create-users"))
}

func runSync(cmd *cobra.Command, args []string) error {
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
	categoryID := viper.GetInt("snipe_it.license_category_id")
	if categoryID == 0 {
		return fatal("snipe_it.license_category_id is required in settings.yaml")
	}

	slackAPIClient := slackapi.NewClient(botToken)
	snipeClient := snipeit.NewClient(snipeURL, snipeKey)

	if err := slackAPIClient.ValidateToken(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Slack API error: %v\n", err)
		return err
	}

	// Resolve license name: use configured value, or derive from workspace info.
	// Auto-resolved format: "Slack <Plan> (<domain>)" e.g. "Slack Business+ (gallatin-ai)".
	// Set slack.plan in settings.yaml — team.info does not return the billing plan.
	licenseName := viper.GetString("snipe_it.license_name")
	if licenseName == "" {
		info, err := slackAPIClient.GetWorkspaceInfo(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Slack API error: %v\n", err)
			return err
		}
		if p := viper.GetString("slack.plan"); p != "" {
			info.Plan = p
		}
		licenseName = slackapi.LicenseName(info)
		slog.Info("resolved license name from workspace", "name", licenseName)
	}

	emailFilter, _ := cmd.Flags().GetString("email")
	noSlack, _ := cmd.Flags().GetBool("no-slack")

	cfg := sync.Config{
		DryRun:            viper.GetBool("sync.dry_run"),
		Force:             viper.GetBool("sync.force"),
		CreateUsers:       viper.GetBool("sync.create_users"),
		LicenseName:       licenseName,
		LicenseCategoryID: categoryID,
		LicenseSeats:      viper.GetInt("snipe_it.license_seats"),
		ManufacturerID:    viper.GetInt("snipe_it.license_manufacturer_id"),
		SupplierID:        viper.GetInt("snipe_it.license_supplier_id"),
	}

	if cfg.DryRun {
		slog.Info("dry-run mode enabled — no changes will be made")
	}

	notifier := slack.NewClient(viper.GetString("slack.webhook_url"))
	syncer := sync.NewSyncer(slackAPIClient, snipeClient, cfg)
	result, err := syncer.Run(ctx, emailFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync failed: %v\n", err)
		if !cfg.DryRun && !noSlack {
			msg := fmt.Sprintf("slack2snipe sync failed: %v", err)
			if notifyErr := notifier.Send(ctx, msg); notifyErr != nil {
				slog.Warn("slack notification failed", "error", notifyErr)
			}
		}
		return err
	}

	if !cfg.DryRun && !noSlack {
		for _, email := range result.UnmatchedEmails {
			msg := fmt.Sprintf("slack2snipe: no Snipe-IT account found for Slack member — %s", email)
			if notifyErr := notifier.Send(ctx, msg); notifyErr != nil {
				slog.Warn("slack notification failed", "email", email, "error", notifyErr)
			}
		}
		msg := fmt.Sprintf(
			"slack2snipe sync complete — checked out: %d, notes updated: %d, checked in: %d, skipped: %d, users created: %d, warnings: %d",
			result.CheckedOut, result.NotesUpdated, result.CheckedIn, result.Skipped, result.UsersCreated, result.Warnings,
		)
		if notifyErr := notifier.Send(ctx, msg); notifyErr != nil {
			slog.Warn("slack notification failed", "error", notifyErr)
		}
	}

	fmt.Printf("Sync complete: checked_out=%d notes_updated=%d checked_in=%d skipped=%d users_created=%d warnings=%d\n",
		result.CheckedOut, result.NotesUpdated, result.CheckedIn, result.Skipped, result.UsersCreated, result.Warnings)
	return nil
}
