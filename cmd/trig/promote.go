package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/justinstimatze/trig/internal/linear"
	"github.com/justinstimatze/trig/internal/posthog"
)

const (
	mergedStateName = "Merged"
	doneStateName   = "Done"
	// promotionEnv is fixed regardless of whatever --env cmdSweep was
	// invoked with. The rollout-label pass is deliberately --env-scoped
	// (DESIGN.md), but a ticket's actual Linear lifecycle state answers
	// "is this real for users" — only production release data can say
	// that, so this half of sweep always reads production.
	promotionEnv = "production"
)

// promotionResult is one Merged ticket's outcome from promoteMergedTickets,
// reported in sweep's output alongside the existing per-ticket label pass.
type promotionResult struct {
	Ticket   string `json:"ticket"`
	Released bool   `json:"released"`
	NewState string `json:"new_state,omitempty"`
	Error    string `json:"error,omitempty"`
}

// promotionState reduces every one of a ticket's matched flags' production
// rollout states to one ticket-wide lifecycle state, worst-flag-wins: any
// flag still fully dark holds the whole ticket at Dark, any flag short of
// live (but not dark) holds it at Canary, and Done — Linear's real
// "released" signal to comms — requires every matched flag to be live.
// Deliberately the opposite rule from status.go's aggregateState
// (any-live-wins), which is fine for an informational label but would
// force-close a multi-flag ticket carrying one flag intentionally capped at
// custom (e.g. CUR-92) if reused here.
func promotionState(states []posthog.RolloutState) posthog.RolloutState {
	best := posthog.StateLive
	for _, s := range states {
		if s == posthog.StateDark {
			return posthog.StateDark
		}
		if s == posthog.StateCustom {
			best = posthog.StateCustom
		}
	}
	return best
}

var promotionStateNames = map[posthog.RolloutState]string{
	posthog.StateDark:   "Dark",
	posthog.StateCustom: "Canary",
	posthog.StateLive:   doneStateName,
}

// resolveWorkflowState looks up teamID's state named name, caching by
// (teamID, name) within one sweep run so tickets sharing a team don't each
// pay a separate GraphQL round trip for the same lookup.
func resolveWorkflowState(lnClient *linear.Client, cache map[string]map[string]*linear.WorkflowState, teamID, name string) (*linear.WorkflowState, error) {
	if _, ok := cache[teamID]; !ok {
		cache[teamID] = map[string]*linear.WorkflowState{}
	}
	if s, ok := cache[teamID][name]; ok {
		return s, nil
	}
	s, err := lnClient.GetWorkflowStateByName(teamID, name)
	if err != nil {
		return nil, err
	}
	cache[teamID][name] = s
	return s, nil
}

// promoteOneTicket decides and, unless dryRun, applies issue's next
// lifecycle state: a no-op if its release hasn't completed yet, Done if it
// carries no PostHog flag (nothing gates it once it's on main), or
// Dark/Canary/Done from its matched flags' production rollout state
// otherwise. byTicket is the same PostHog-tag-derived map cmdSweep already
// computed for the label pass — its presence for a ticket IS "has a
// posthog-flag", the identical fact a Linear label round trip would
// otherwise re-derive.
func promoteOneTicket(lnClient *linear.Client, stateCache map[string]map[string]*linear.WorkflowState, issue linear.IssueByState, byTicket map[string][]posthog.FeatureFlag, dryRun, jsonOut bool) (promotionResult, error) {
	if !issue.ReleaseCompleted {
		if !jsonOut {
			fmt.Printf("%s: no completed release yet, left at %s\n", issue.Identifier, mergedStateName)
		}
		return promotionResult{Ticket: issue.Identifier, Released: false}, nil
	}

	targetName := doneStateName
	if flags, flagged := byTicket[strings.ToLower(issue.Identifier)]; flagged {
		states := make([]posthog.RolloutState, len(flags))
		for i, f := range flags {
			states[i] = f.StateIn(envPropertyKey, promotionEnv)
		}
		targetName = promotionStateNames[promotionState(states)]
	}

	state, err := resolveWorkflowState(lnClient, stateCache, issue.TeamID, targetName)
	if err != nil {
		return promotionResult{}, fmt.Errorf("resolve state %q: %w", targetName, err)
	}

	if dryRun {
		if !jsonOut {
			fmt.Printf("[dry-run] would move %s: %s -> %s\n", issue.Identifier, mergedStateName, targetName)
		}
		return promotionResult{Ticket: issue.Identifier, Released: true, NewState: targetName}, nil
	}

	if err := lnClient.SetState(issue.ID, state.ID); err != nil {
		return promotionResult{}, err
	}
	if !jsonOut {
		fmt.Printf("moved %s: %s -> %s\n", issue.Identifier, mergedStateName, targetName)
	}
	return promotionResult{Ticket: issue.Identifier, Released: true, NewState: targetName}, nil
}

// promoteMergedTickets is sweep's second responsibility, independent of and
// additive to the rollout-label pass above: it finds every ticket
// currently sitting at Linear state "Merged" — a population the
// PostHog-tag-driven byTicket map doesn't cover on its own, since most
// Merged tickets carry no flag at all — and moves each one whose code has
// actually reached a completed release to its next state. A ticket not yet
// in a completed release is left alone entirely: engineering finished the
// PR, but aipotluck.org's own release pipeline hasn't confirmed the code is
// on main yet.
//
// One ticket failing (a missing workflow state, an archived issue) is
// logged and skipped, same tolerance as the label pass; an AuthError is
// treated as systemic and aborts immediately, since it will fail
// identically for every remaining ticket.
func promoteMergedTickets(lnClient *linear.Client, byTicket map[string][]posthog.FeatureFlag, dryRun, jsonOut bool) ([]promotionResult, int, error) {
	issues, err := lnClient.ListIssuesByState(mergedStateName)
	if err != nil {
		return nil, 0, err
	}

	stateCache := map[string]map[string]*linear.WorkflowState{}
	results := make([]promotionResult, 0, len(issues))
	failedCount := 0

	for _, issue := range issues {
		result, err := promoteOneTicket(lnClient, stateCache, issue, byTicket, dryRun, jsonOut)
		if err != nil {
			var lnAuth *linear.AuthError
			if errors.As(err, &lnAuth) {
				return results, failedCount, err
			}
			failedCount++
			fmt.Fprintf(os.Stderr, "trig sweep: promote %s: %v\n", issue.Identifier, err)
			results = append(results, promotionResult{Ticket: issue.Identifier, Error: err.Error()})
			continue
		}
		results = append(results, result)
	}
	return results, failedCount, nil
}
