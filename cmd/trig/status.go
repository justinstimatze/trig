package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/justinstimatze/trig/internal/config"
	"github.com/justinstimatze/trig/internal/linear"
	"github.com/justinstimatze/trig/internal/posthog"
)

const (
	flaggedLabel = "posthog-flag"
	// legacyLiveLabel and legacyDarkLabel are the pre-namespaced pair every
	// run superseded — a single "posthog-live"/"posthog-dark" swap with no
	// indication of which --env produced it, which read as "live for users"
	// regardless of whether the checked env was actually production. Every
	// run unconditionally removes both (see runTicket) so a ticket a
	// preview-only sweep once mislabeled self-heals on its next real run,
	// without a separate migration step.
	legacyLiveLabel = "posthog-live"
	legacyDarkLabel = "posthog-dark"
	envPropertyKey  = "env"
)

// stateLabel names the Linear label for one environment's rollout state,
// e.g. "posthog-preview:custom". Namespacing by envValue means two --env
// values never fight over the same label, and a label's name alone tells a
// board-scanning human which environment it's actually claiming something
// about — the gap that motivated this over a single unqualified pair.
func stateLabel(envValue string, state posthog.RolloutState) string {
	return fmt.Sprintf("posthog-%s:%s", envValue, state)
}

// aggregateState picks one ticket-wide rollout state across every matched
// flag for the tracked environment: live if any flag is live there, else
// custom if any flag is custom, else dark. The most-informative-flag-wins
// rule mirrors the pre-namespaced label's any-live-wins semantics,
// generalized from 2 states to 3.
func aggregateState(states []posthog.RolloutState) posthog.RolloutState {
	best := posthog.StateDark
	for _, s := range states {
		if s == posthog.StateLive {
			return posthog.StateLive
		}
		if s == posthog.StateCustom {
			best = posthog.StateCustom
		}
	}
	return best
}

const statusUsage = `usage: trig status TICKET-ID [--env VALUE] [--json] [--dry-run]

Finds every PostHog flag tagged linear:TICKET-ID, renders its rollout state
for the tracked environment (default "production"), and writes it to the
Linear ticket: the generic "posthog-flag" label (always), one of
"posthog-VALUE:dark" / "posthog-VALUE:custom" / "posthog-VALUE:live"
(ticket-wide, namespaced by the checked --env — exactly one of the three,
never more than one at once) reflecting whether any matched flag has no
effect, a partial/conditional effect, or an unconditional 100% effect in
that environment, and one create-or-update attachment per flag titled
"PostHog: FLAG [env=VALUE] — SUMMARY". Linear's own attachmentCreate upserts
by (issue, URL), so there is exactly one trig attachment per flag — only the
most recently checked environment's state is kept there. Running with a
different --env than last time REPLACES the previous env's attachment
record; trig prints this switch explicitly (and reports it in --json as
switched_env_from) rather than doing it silently. The ticket-wide label is
unaffected by that switch — each --env owns its own label, so checking
preview today and production tomorrow leaves both labels intact and honest.

  --env VALUE   Which environment's release conditions to render and use for
                the ticket-wide posthog-VALUE:{dark,custom,live} label.
                Matched against each release condition group's "env"
                property. Default: production.
  --json        Print one JSON document to stdout instead of prose. Errors
                still go to stderr as text regardless of this flag.
  --dry-run     Print what would be written; make no Linear writes at all
                (no label create/apply/remove, no attachment create/update).

Exit codes: 0 ok, 2 bad arguments, 3 no flag tagged linear:TICKET-ID yet
(run trig link first), 4 API key unauthorized/missing scope, 5 not found,
1 other failure.
`

type flagResult struct {
	FlagKey                string `json:"flag_key"`
	Active                 bool   `json:"active"`
	EffectivelyFullRollout bool   `json:"effectively_full_rollout"`
	MaxRolloutPercentage   int    `json:"max_rollout_percentage"`
	Conditions             string `json:"conditions"`
	IsLive                 bool   `json:"is_live"`
	RolloutState           string `json:"rollout_state"`               // dark | custom | live, for this flag alone in the tracked env
	AttachmentAction       string `json:"attachment_action"`           // created | updated | would_create | would_update
	SwitchedEnvFrom        string `json:"switched_env_from,omitempty"` // set when this run replaced a different env's last-known state
}

type statusOutput struct {
	Ticket     string       `json:"ticket"`
	Env        string       `json:"env"`
	DryRun     bool         `json:"dry_run"`
	State      string       `json:"state"`       // dark | custom | live, aggregated across every matched flag
	StateLabel string       `json:"state_label"` // e.g. "posthog-preview:custom" — the Linear label State maps to
	CheckedAt  string       `json:"checked_at"`
	Flags      []flagResult `json:"flags"`
}

// cmdStatus is trig's core command: trig status TICKET-ID [--env VALUE]
// [--json] [--dry-run]. See statusUsage for the full contract.
func cmdStatus(args []string) {
	ticketID, envValue, jsonOut, dryRun := parseStatusArgs(args)

	cfg, err := config.Load()
	if err != nil {
		fail("status", err)
	}
	phClient := posthog.NewClient(cfg)
	lnClient := linear.NewClient(cfg)

	// PostHog lowercases tags server-side ("linear:CUR-515" round-trips as
	// "linear:cur-515"), so comparison must be case-insensitive.
	tag := "linear:" + ticketID
	flags, err := phClient.ListFlags()
	if err != nil {
		fail("status", err)
	}
	var matched []posthog.FeatureFlag
	for _, f := range flags {
		for _, t := range f.Tags {
			if strings.EqualFold(t, tag) {
				matched = append(matched, f)
				break
			}
		}
	}
	if len(matched) == 0 {
		fmt.Fprintf(os.Stderr, "trig status: no flag tagged %s — run `trig link %s FLAG-KEY` first (see `trig flags` to find FLAG-KEY)\n", tag, ticketID)
		os.Exit(exitNotLinked)
	}

	checkedAt := time.Now().UTC().Format(time.RFC3339)
	out, err := runTicket(phClient, lnClient, ticketID, matched, envValue, dryRun, jsonOut, checkedAt)
	if err != nil {
		fail("status", err)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fail("status", err)
		}
	}
}

// runTicket applies trig's status logic for one ticket against an
// already-discovered set of matched flags: resolves the Linear issue,
// ensures the "posthog-flag" label, creates or updates one attachment per
// flag, and sets the ticket-wide posthog-VALUE:{dark,custom,live} label
// from the aggregate rollout state across every matched flag. Shared by
// cmdStatus (one ticket named on the command line) and cmdSweep (every
// ticket discovered from PostHog tags) — callers own error handling, since
// a failure aborts cmdStatus but should only skip one ticket in cmdSweep.
func runTicket(phClient *posthog.Client, lnClient *linear.Client, ticketID string, matched []posthog.FeatureFlag, envValue string, dryRun, jsonOut bool, checkedAt string) (statusOutput, error) {
	issue, err := lnClient.GetIssueByIdentifier(ticketID)
	if err != nil {
		return statusOutput{}, err
	}

	if !dryRun {
		if err := ensureLabelApplied(lnClient, issue, flaggedLabel); err != nil {
			return statusOutput{}, err
		}
	}

	if !jsonOut {
		fmt.Printf("%d flag(s) tagged linear:%s\n", len(matched), issue.Identifier)
	}

	results := []flagResult{}
	var states []posthog.RolloutState
	for i, flag := range matched {
		summary := flag.Rollout()
		conditions := posthog.RenderConditions(flag.Filters.Groups, envPropertyKey, envValue)
		isLive := flag.IsLiveIn(envPropertyKey, envValue)
		state := flag.StateIn(envPropertyKey, envValue)
		states = append(states, state)

		if !jsonOut {
			if i > 0 {
				fmt.Println("---")
			}
			fmt.Printf("%s: active=%v effectively_full_rollout=%v max_rollout_percentage=%d%% live_in_%s=%v state_in_%s=%s\n",
				flag.Key, summary.Active, summary.EffectivelyFullRollout, summary.MaxRolloutPercentage, envValue, isLive, envValue, state)
			fmt.Println(conditions)
		}

		title := fmt.Sprintf("PostHog: %s [env=%s] — %s", flag.Key, envValue, titleSummary(summary, conditions))
		subtitle := posthog.TitleConditions(flag.Filters.Groups, envPropertyKey, envValue)
		metadata := map[string]interface{}{
			"flag_key":                 flag.Key,
			"active":                   summary.Active,
			"effectively_full_rollout": summary.EffectivelyFullRollout,
			"max_rollout_percentage":   summary.MaxRolloutPercentage,
			"tracked_env":              envValue,
			"is_live":                  isLive,
			"rollout_state":            string(state),
			"conditions":               conditions,
			"checked_at":               checkedAt,
		}
		flagURL := phClient.FlagURL(flag.ID)

		// Linear's attachmentCreate upserts on (issueId, url), so there is
		// exactly one trig attachment per flag regardless of envValue.
		// Matching by URL mirrors that real key so create-vs-update is
		// never mispredicted.
		var existing *linear.Attachment
		for a := range issue.Attachments {
			if issue.Attachments[a].URL == flagURL {
				existing = &issue.Attachments[a]
				break
			}
		}
		switchedEnvFrom := ""
		if existing != nil {
			if prevEnv, ok := existing.Metadata["tracked_env"].(string); ok && prevEnv != envValue {
				switchedEnvFrom = prevEnv
			}
		}

		action := ""
		if dryRun {
			if existing != nil {
				action = "would_update"
			} else {
				action = "would_create"
			}
			if !jsonOut {
				fmt.Printf("[dry-run] %s attachment: %q\n", action, title)
				if subtitle != "" {
					fmt.Printf("[dry-run] subtitle: %q\n", subtitle)
				}
				if switchedEnvFrom != "" {
					fmt.Printf("[dry-run] would switch tracked env from %q to %q — the previous env's last-known state won't be kept\n", switchedEnvFrom, envValue)
				}
			}
		} else {
			if switchedEnvFrom != "" && !jsonOut {
				fmt.Printf("switching tracked env from %q to %q — the previous env's last-known state won't be kept\n", switchedEnvFrom, envValue)
			}
			if existing != nil {
				if _, err := lnClient.UpdateAttachment(existing.ID, title, subtitle, metadata); err != nil {
					return statusOutput{}, err
				}
				action = "updated"
				if !jsonOut {
					fmt.Printf("updated attachment on %s\n", issue.Identifier)
				}
			} else {
				if _, err := lnClient.CreateAttachment(issue.ID, title, subtitle, flagURL, metadata); err != nil {
					return statusOutput{}, err
				}
				action = "created"
				if !jsonOut {
					fmt.Printf("created attachment on %s\n", issue.Identifier)
				}
			}
		}

		results = append(results, flagResult{
			FlagKey:                flag.Key,
			Active:                 summary.Active,
			EffectivelyFullRollout: summary.EffectivelyFullRollout,
			MaxRolloutPercentage:   summary.MaxRolloutPercentage,
			Conditions:             conditions,
			IsLive:                 isLive,
			RolloutState:           string(state),
			AttachmentAction:       action,
			SwitchedEnvFrom:        switchedEnvFrom,
		})
	}

	// Ticket-wide state label, computed once across all matched flags (not
	// per-flag) so two flags in different states on the same ticket can't
	// thrash each other's label on and off within a single run. Namespaced
	// by envValue so two --env values each own their own label instead of
	// fighting over one unqualified pair.
	ticketState := aggregateState(states)
	wantLabel := stateLabel(envValue, ticketState)
	var otherLabels []string
	for _, s := range []posthog.RolloutState{posthog.StateDark, posthog.StateCustom, posthog.StateLive} {
		if s != ticketState {
			otherLabels = append(otherLabels, stateLabel(envValue, s))
		}
	}
	// The pre-namespaced pair is fully superseded and cleaned up on every
	// run regardless of ticketState — see legacyLiveLabel's doc comment.
	otherLabels = append(otherLabels, legacyLiveLabel, legacyDarkLabel)

	if dryRun {
		if !jsonOut {
			fmt.Printf("[dry-run] would ensure label %q applied, %v removed\n", wantLabel, otherLabels)
		}
	} else {
		if err := ensureLabelApplied(lnClient, issue, wantLabel); err != nil {
			return statusOutput{}, err
		}
		for _, l := range otherLabels {
			if err := ensureLabelRemoved(lnClient, issue, l); err != nil {
				return statusOutput{}, err
			}
		}
	}

	return statusOutput{
		Ticket:     issue.Identifier,
		Env:        envValue,
		DryRun:     dryRun,
		State:      string(ticketState),
		StateLabel: wantLabel,
		CheckedAt:  checkedAt,
		Flags:      results,
	}, nil
}

// titleSummary renders the one line that's actually visible in Linear's
// Resources row without clicking through — e.g. "inactive", "fully rolled
// out", "100% rollout", or "100% rollout, no matching condition". The
// tracked environment itself is already in the title's "[env=...]" prefix,
// so it isn't repeated here.
func titleSummary(s posthog.RolloutSummary, conditions string) string {
	if !s.Active {
		return "inactive"
	}
	if s.EffectivelyFullRollout {
		return "fully rolled out"
	}
	if strings.HasPrefix(conditions, "no release condition targets") {
		return fmt.Sprintf("%d%% rollout, no matching condition", s.MaxRolloutPercentage)
	}
	return fmt.Sprintf("%d%% rollout", s.MaxRolloutPercentage)
}

func parseStatusArgs(args []string) (ticketID, envValue string, jsonOut, dryRun bool) {
	envValue = "production"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(statusUsage)
			os.Exit(exitOK)
		case "--env":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "trig status: --env requires a value")
				os.Exit(exitUsage)
			}
			envValue = args[i+1]
			i++
		case "--json":
			jsonOut = true
		case "--dry-run":
			dryRun = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "trig status: unknown flag %q\n\n%s", args[i], statusUsage)
				os.Exit(exitUsage)
			}
			if ticketID != "" {
				fmt.Fprintf(os.Stderr, "trig status: unexpected argument %q\n\n%s", args[i], statusUsage)
				os.Exit(exitUsage)
			}
			ticketID = args[i]
		}
	}
	if ticketID == "" {
		fmt.Print(statusUsage)
		os.Exit(exitUsage)
	}
	return ticketID, envValue, jsonOut, dryRun
}
