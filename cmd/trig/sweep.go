package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/justinstimatze/trig/internal/config"
	"github.com/justinstimatze/trig/internal/linear"
	"github.com/justinstimatze/trig/internal/posthog"
)

const sweepUsage = `usage: trig sweep [--env VALUE] [--json] [--dry-run]

Finds every PostHog flag tagged linear:TICKET-ID — for any ticket, not one
named on the command line — groups them by ticket, and runs the same report
'trig status' does for each one: rollout state for the tracked environment
(default "production"), the "posthog-flag" label, one of
"posthog-VALUE:dark" / "posthog-VALUE:custom" / "posthog-VALUE:live", and
one create-or-update attachment per flag. Meant to run unattended on a
schedule (e.g. a GitHub Actions cron), not by hand.

Tickets are processed independently: one ticket failing (an archived issue,
a deleted label) is logged and skipped, it doesn't stop the rest. An
unauthorized/missing-scope API key is treated as systemic instead — it will
fail identically for every remaining ticket, so trig aborts the whole sweep
on the first one rather than repeating the same failure N times.

If every successfully-checked ticket comes back dark in the tracked env,
sweep prints a warning (and sets void_warning in --json) instead of staying
quiet: a real flag registry that's uniformly dark in the env you're checking
is the signature of tracking the wrong environment, not of nothing having
shipped yet — this is the check that would have caught a cron pinned to
--env preview well before anyone happened to notice by hand.

Separately, and regardless of --env: for every Linear ticket currently at
state "Merged", sweep checks (via the Linear Release API) whether its code
has actually reached a completed release. If not, the ticket is left alone,
no-op. If so, and it carries no PostHog flag, it moves straight to "Done" —
nothing gates it once it's on main. If it carries a flag, it moves to
"Dark"/"Canary"/"Done" from that flag's production rollout state (always
production, independent of --env above) — Done only when every matched flag
is live, so a ticket carrying one flag deliberately left at a permanent
partial rollout never gets force-closed by a sibling flag going live. This
part of sweep is what closes the gap Linear's own git-merge automations
can't: PR-opened -> "In Review" and PR-merged -> "Merged" are automated by
Linear directly, but nothing past "Merged" can be, since that requires
branching per-issue on whether it's flag-gated.

  --env VALUE   Same as trig status --env. Only affects the rollout-label
                pass above, not the Merged-ticket state promotion, which is
                always production.
  --json        Print one JSON document (a list of per-ticket reports) to
                stdout instead of prose.
  --dry-run     Print what would change; make no Linear writes at all.

Exit codes: 0 every ticket succeeded, 6 partial failure (at least one
ticket failed, others may have succeeded), 4 API key unauthorized/missing
scope (aborted immediately, no tickets processed after the first failure),
2 bad arguments, 1 other failure.
`

type sweepTicketResult struct {
	statusOutput
	Error string `json:"error,omitempty"`
}

type sweepOutput struct {
	Env         string              `json:"env"`
	DryRun      bool                `json:"dry_run"`
	CheckedAt   string              `json:"checked_at"`
	Tickets     []sweepTicketResult `json:"tickets"`
	VoidWarning string              `json:"void_warning,omitempty"`
	Promotions  []promotionResult   `json:"promotions"`
}

// voidWarning reports when every one of states (the ticket-wide state from
// each successfully-checked ticket, in the same order sweep processed them)
// is dark. A pure, testable aggregation over data cmdSweep already
// computes — no interpretation of what the tracked env is "supposed to" be,
// just the structural fact that nothing sweep found is live there.
func voidWarning(states []posthog.RolloutState, envValue string) string {
	if len(states) == 0 {
		return ""
	}
	for _, s := range states {
		if s != posthog.StateDark {
			return ""
		}
	}
	return fmt.Sprintf("sweeping into a void: all %d tracked ticket(s) are dark in env=%s — double check this is the environment you meant to track", len(states), envValue)
}

// cmdSweep is trig's discovery-driven counterpart to cmdStatus: instead of
// being told which ticket to check, it finds every ticket with a linked
// flag on its own. See sweepUsage for the full contract.
func cmdSweep(args []string) {
	envValue, jsonOut, dryRun := parseSweepArgs(args)

	cfg, err := config.Load()
	if err != nil {
		fail("sweep", err)
	}
	phClient := posthog.NewClient(cfg)
	lnClient := linear.NewClient(cfg)

	flags, err := phClient.ListFlags()
	if err != nil {
		fail("sweep", err)
	}
	byTicket := groupByTicket(flags)

	ticketIDs := make([]string, 0, len(byTicket))
	for id := range byTicket {
		ticketIDs = append(ticketIDs, id)
	}
	sort.Strings(ticketIDs) // deterministic order run to run

	checkedAt := time.Now().UTC().Format(time.RFC3339)
	results := []sweepTicketResult{}
	var states []posthog.RolloutState
	failedCount := 0

	if !jsonOut && len(ticketIDs) == 0 {
		fmt.Println("no flags tagged linear:* — nothing to sweep")
	}

	for i, ticketID := range ticketIDs {
		if !jsonOut && i > 0 {
			fmt.Println("===")
		}
		out, err := runTicket(phClient, lnClient, ticketID, byTicket[ticketID], envValue, dryRun, jsonOut, checkedAt)
		if err != nil {
			var phAuth *posthog.AuthError
			var lnAuth *linear.AuthError
			if errors.As(err, &phAuth) || errors.As(err, &lnAuth) {
				fmt.Fprintf(os.Stderr, "trig sweep: aborting — %v\n", err)
				os.Exit(exitAuth)
			}
			failedCount++
			fmt.Fprintf(os.Stderr, "trig sweep: %s: %v\n", ticketID, err)
			results = append(results, sweepTicketResult{
				statusOutput: statusOutput{Ticket: ticketID, Env: envValue, DryRun: dryRun, CheckedAt: checkedAt},
				Error:        err.Error(),
			})
			continue
		}
		results = append(results, sweepTicketResult{statusOutput: out})
		states = append(states, posthog.RolloutState(out.State))
	}

	warning := voidWarning(states, envValue)
	if warning != "" && !jsonOut {
		fmt.Println("===")
		fmt.Println(warning)
	}

	promotions, promoFailed, err := promoteMergedTickets(lnClient, byTicket, dryRun, jsonOut)
	if err != nil {
		var lnAuth *linear.AuthError
		if errors.As(err, &lnAuth) {
			fmt.Fprintf(os.Stderr, "trig sweep: aborting — %v\n", err)
			os.Exit(exitAuth)
		}
		fail("sweep", err)
	}
	failedCount += promoFailed

	if !jsonOut && len(promotions) > 0 {
		moved, pending := 0, 0
		for _, p := range promotions {
			switch {
			case p.Error != "":
			case p.Released:
				moved++
			default:
				pending++
			}
		}
		fmt.Printf("===\n%s: %d moved, %d awaiting release, %d failed\n", mergedStateName, moved, pending, promoFailed)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		out := sweepOutput{Env: envValue, DryRun: dryRun, CheckedAt: checkedAt, Tickets: results, VoidWarning: warning, Promotions: promotions}
		if err := enc.Encode(out); err != nil {
			fail("sweep", err)
		}
	} else if len(ticketIDs) > 0 {
		fmt.Printf("===\nswept %d ticket(s): %d ok, %d failed\n", len(ticketIDs), len(ticketIDs)-failedCount, failedCount)
	}

	if failedCount > 0 {
		os.Exit(exitPartial)
	}
}

// groupByTicket buckets flags by every linear:TICKET-ID tag they carry — a
// flag with more than one such tag (linked to more than one ticket) lands
// in more than one bucket, matching how trig status already treats one
// flag shared across tickets. Bucket keys are whatever case PostHog stored
// the tag in (always lowercase — PostHog lowercases tags server-side), but
// Linear's issue lookup resolves an identifier case-insensitively, so
// runTicket doesn't need the original case to find the right issue.
func groupByTicket(flags []posthog.FeatureFlag) map[string][]posthog.FeatureFlag {
	const prefix = "linear:"
	byTicket := map[string][]posthog.FeatureFlag{}
	for _, f := range flags {
		for _, t := range f.Tags {
			lower := strings.ToLower(t)
			if !strings.HasPrefix(lower, prefix) {
				continue
			}
			ticketID := strings.TrimPrefix(lower, prefix)
			if ticketID == "" {
				continue
			}
			byTicket[ticketID] = append(byTicket[ticketID], f)
		}
	}
	return byTicket
}

func parseSweepArgs(args []string) (envValue string, jsonOut, dryRun bool) {
	envValue = "production"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(sweepUsage)
			os.Exit(exitOK)
		case "--env":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "trig sweep: --env requires a value")
				os.Exit(exitUsage)
			}
			envValue = args[i+1]
			i++
		case "--json":
			jsonOut = true
		case "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(os.Stderr, "trig sweep: unexpected argument %q\n\n%s", args[i], sweepUsage)
			os.Exit(exitUsage)
		}
	}
	return envValue, jsonOut, dryRun
}
