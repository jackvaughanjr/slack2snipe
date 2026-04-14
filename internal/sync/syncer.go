package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackvaughanjr/slack2snipe/internal/slackapi"
	"github.com/jackvaughanjr/slack2snipe/internal/snipeit"
)

// Config controls sync behaviour.
type Config struct {
	DryRun      bool
	Force       bool
	CreateUsers bool
	LicenseName       string
	LicenseCategoryID int
	// LicenseSeats is the total purchased seat count. If 0, active member count is used.
	// Seats are never shrunk automatically.
	LicenseSeats int
	// ManufacturerID is optional. If 0, auto find/create "Slack" manufacturer.
	ManufacturerID int
	// SupplierID is optional. If 0, no supplier is set on the license.
	SupplierID int
}

// Syncer orchestrates the Slack → Snipe-IT license sync.
type Syncer struct {
	slack *slackapi.Client
	snipe *snipeit.Client
	cfg   Config
}

func NewSyncer(slack *slackapi.Client, snipe *snipeit.Client, cfg Config) *Syncer {
	return &Syncer{slack: slack, snipe: snipe, cfg: cfg}
}

// Run executes the full sync. emailFilter restricts the checkout pass to one
// user (and skips the checkin pass entirely).
func (s *Syncer) Run(ctx context.Context, emailFilter string) (Result, error) {
	var result Result

	// 1. Fetch billable members (full + multi-channel guests) from Slack.
	slog.Info("fetching active Slack members")
	activeUsers, err := s.slack.ListActiveUsers(ctx)
	if err != nil {
		return result, err
	}
	slog.Info("fetched active members", "count", len(activeUsers))

	// 2. Build active email set (used in the checkin pass).
	activeEmails := make(map[string]struct{}, len(activeUsers))
	for _, u := range activeUsers {
		if e := emailKey(u); e != "" {
			activeEmails[e] = struct{}{}
		}
	}

	// 3. Apply --email filter.
	if emailFilter != "" {
		needle := strings.ToLower(emailFilter)
		filtered := activeUsers[:0]
		for _, u := range activeUsers {
			if emailKey(u) == needle {
				filtered = append(filtered, u)
				break
			}
		}
		activeUsers = filtered
		slog.Info("filtered to single user", "email", emailFilter, "found", len(activeUsers) > 0)
	}

	// 4. No per-user API calls needed; users.list includes all required fields.

	// 5. Resolve manufacturer (auto find/create "Slack" if not configured).
	manufacturerID := s.cfg.ManufacturerID
	if !s.cfg.DryRun && manufacturerID == 0 {
		mfr, err := s.snipe.FindOrCreateManufacturer(ctx, "Slack", "https://slack.com")
		if err != nil {
			return result, fmt.Errorf("resolving manufacturer: %w", err)
		}
		manufacturerID = mfr.ID
	}

	// 6. Resolve target seat count.
	// Priority: config override → active member count (Slack does not expose purchased seat count).
	activeCount := len(activeEmails)
	targetSeats := s.cfg.LicenseSeats
	if targetSeats == 0 {
		targetSeats = activeCount
	} else if targetSeats < activeCount {
		slog.Warn("configured seat count is less than active member count; using active count",
			"license_seats", targetSeats, "active", activeCount)
		targetSeats = activeCount
	}

	// Find or create the license.
	// Dry-run: find only; synthesize a placeholder (id=0) if not found.
	slog.Info("finding or creating license", "name", s.cfg.LicenseName)
	var lic *snipeit.License
	if s.cfg.DryRun {
		lic, err = s.snipe.FindLicenseByName(ctx, s.cfg.LicenseName)
		if err != nil {
			return result, err
		}
		if lic == nil {
			slog.Info("[dry-run] license not found; would be created", "name", s.cfg.LicenseName, "seats", targetSeats)
			lic = &snipeit.License{Name: s.cfg.LicenseName, Seats: targetSeats}
		}
	} else {
		lic, err = s.snipe.FindOrCreateLicense(ctx, s.cfg.LicenseName, targetSeats, s.cfg.LicenseCategoryID, manufacturerID, s.cfg.SupplierID)
		if err != nil {
			return result, err
		}
	}
	slog.Info("license resolved", "id", lic.ID, "seats", lic.Seats, "free", lic.FreeSeatsCount)

	// 7. Expand seats if needed (never shrink automatically).
	if targetSeats > lic.Seats {
		slog.Info("expanding license seats", "current", lic.Seats, "needed", targetSeats)
		if !s.cfg.DryRun {
			lic, err = s.snipe.UpdateLicenseSeats(ctx, lic.ID, targetSeats)
			if err != nil {
				return result, err
			}
		}
	}

	// 7.5. Refresh license so FreeSeatsCount is accurate before ghost detection.
	// Snipe-IT's POST (create) response returns free_seats_count: 0 regardless of
	// seat count; a fresh GET gives the real value.
	if !s.cfg.DryRun && lic.ID != 0 {
		lic, err = s.snipe.FindLicenseByID(ctx, lic.ID)
		if err != nil {
			return result, fmt.Errorf("refreshing license: %w", err)
		}
		slog.Debug("license refreshed", "id", lic.ID, "seats", lic.Seats, "free", lic.FreeSeatsCount)
	}

	// 8. Load current seat assignments; partition into checked-out and free.
	checkedOutByEmail := make(map[string]*snipeit.LicenseSeat)
	var freeSeats []*snipeit.LicenseSeat
	if lic.ID != 0 {
		slog.Info("loading current seat assignments")
		seats, err := s.snipe.ListLicenseSeats(ctx, lic.ID)
		if err != nil {
			return result, err
		}
		for i := range seats {
			seat := &seats[i]
			if seat.AssignedTo != nil && seat.AssignedTo.Email != "" {
				checkedOutByEmail[strings.ToLower(seat.AssignedTo.Email)] = seat
			} else {
				freeSeats = append(freeSeats, seat)
			}
		}

		// Ghost cleanup: seats Snipe-IT tracks as used but with no assigned_user.
		// Must run before the checkout loop to avoid false "no free seats" warnings.
		snipeCheckedOut := lic.Seats - lic.FreeSeatsCount
		ghostCount := snipeCheckedOut - len(checkedOutByEmail)
		if ghostCount > 0 {
			slog.Warn("cleaning up ghost checkouts", "count", ghostCount)
			cleaned := 0
			for i := 0; i < len(freeSeats) && cleaned < ghostCount; i++ {
				if s.cfg.DryRun {
					slog.Info("[dry-run] would check in ghost seat", "seat_id", freeSeats[i].ID)
				} else {
					if err := s.snipe.CheckinSeat(ctx, lic.ID, freeSeats[i].ID); err != nil {
						slog.Warn("failed to check in ghost seat", "seat_id", freeSeats[i].ID, "error", err)
						continue
					}
				}
				cleaned++
			}
			if cleaned < len(freeSeats) {
				freeSeats = freeSeats[cleaned:]
			} else {
				freeSeats = nil
			}
		}
	} else if !s.cfg.DryRun {
		return result, fmt.Errorf("license resolved with id=0 in production mode — check Snipe-IT API permissions and required fields")
	} else {
		slog.Info("[dry-run] skipping seat load for new license")
	}
	slog.Info("seat state loaded", "checked_out", len(checkedOutByEmail), "free", len(freeSeats))

	// 9. Checkout / update loop.
	for _, u := range activeUsers {
		email := emailKey(u)
		if email == "" {
			// users:read.email scope not granted, or profile has no email address.
			slog.Warn("skipping member with no email address", "user_id", u.ID, "name", u.Name)
			result.Warnings++
			continue
		}
		notes := buildNotes(u)

		snipeUser, err := s.snipe.FindUserByEmail(ctx, email)
		if err != nil {
			slog.Warn("error looking up Snipe-IT user", "email", email, "error", err)
			result.Warnings++
			continue
		}
		if snipeUser == nil {
			if !s.cfg.CreateUsers {
				slog.Warn("no Snipe-IT user found for Slack member", "email", email)
				result.UnmatchedEmails = append(result.UnmatchedEmails, email)
				result.Warnings++
				continue
			}
			firstName := u.Profile.FirstName
			lastName := u.Profile.LastName
			if firstName == "" && lastName == "" {
				parts := strings.SplitN(u.Profile.RealName, " ", 2)
				firstName = parts[0]
				if len(parts) > 1 {
					lastName = parts[1]
				}
			}
			if s.cfg.DryRun {
				slog.Info("[dry-run] would create Snipe-IT user", "email", email)
				result.UsersCreated++
				result.CheckedOut++
				continue
			}
			created, err := s.snipe.CreateUser(ctx, firstName, lastName, email, email,
				"Auto-created from Slack via slack2snipe", "")
			if err != nil {
				slog.Warn("failed to create Snipe-IT user", "email", email, "error", err)
				result.Warnings++
				continue
			}
			snipeUser = created
			result.UsersCreated++
		}

		if existing, ok := checkedOutByEmail[email]; ok {
			if existing.Notes == notes && !s.cfg.Force {
				slog.Debug("seat up to date", "email", email)
				result.Skipped++
				continue
			}
			slog.Info("updating seat notes", "email", email, "dry_run", s.cfg.DryRun)
			if !s.cfg.DryRun {
				if err := s.snipe.UpdateSeatNotes(ctx, lic.ID, existing.ID, notes); err != nil {
					slog.Warn("failed to update seat notes", "email", email, "error", err)
					result.Warnings++
					continue
				}
			}
			result.NotesUpdated++
			continue
		}

		if s.cfg.DryRun {
			slog.Info("[dry-run] would check out seat", "email", email, "notes", notes)
			result.CheckedOut++
			continue
		}
		if len(freeSeats) == 0 {
			slog.Warn("no free seats available", "email", email)
			result.Warnings++
			continue
		}
		seat := freeSeats[0]
		freeSeats = freeSeats[1:]

		slog.Info("checking out seat", "email", email, "seat_id", seat.ID)
		if err := s.snipe.CheckoutSeat(ctx, lic.ID, seat.ID, snipeUser.ID, notes); err != nil {
			slog.Warn("failed to checkout seat", "email", email, "error", err)
			freeSeats = append(freeSeats, seat) // return seat on failure
			result.Warnings++
			continue
		}
		result.CheckedOut++
	}

	// 10. Checkin pass — skipped when --email filter is active.
	if emailFilter == "" {
		for email, seat := range checkedOutByEmail {
			if _, active := activeEmails[email]; active {
				continue
			}
			slog.Info("checking in seat for inactive member", "email", email, "seat_id", seat.ID, "dry_run", s.cfg.DryRun)
			if !s.cfg.DryRun {
				if err := s.snipe.CheckinSeat(ctx, lic.ID, seat.ID); err != nil {
					slog.Warn("failed to checkin seat", "email", email, "error", err)
					result.Warnings++
					continue
				}
			}
			result.CheckedIn++
		}
	}

	return result, nil
}

// emailKey returns the canonical (lowercased) email for a Slack user.
// Returns empty string if the profile has no email (users:read.email scope missing).
func emailKey(u slackapi.User) string {
	return strings.ToLower(u.Profile.Email)
}

// buildNotes returns the notes string written to the Snipe-IT license seat.
// Records the member's Slack billing type so asset managers can audit seat usage.
func buildNotes(u slackapi.User) string {
	return "member_type: " + slackapi.MemberType(u)
}
