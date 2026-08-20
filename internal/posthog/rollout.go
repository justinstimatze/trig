package posthog

import (
	"fmt"
	"strings"
)

// RolloutSummary is trig's rollout-completeness report — DESIGN.md's
// "effectively_full_rollout, max_rollout_percentage, active" fields, same
// names and semantics as PostHog's own feature-flag-status concept.
// EffectivelyFullRollout is true only when targeted to everyone with no
// conditions; a flag can be at max_rollout_percentage=100 within a group and
// still have EffectivelyFullRollout=false if a condition gates that group.
type RolloutSummary struct {
	Active                 bool
	MaxRolloutPercentage   int
	EffectivelyFullRollout bool
}

// Rollout computes the summary from this flag's release condition groups.
// A group's rollout_percentage is null when unset in the PostHog UI, which
// means no percentage gate — full effect within that group — so nil is
// treated as 100, not 0.
func (f FeatureFlag) Rollout() RolloutSummary {
	var s RolloutSummary
	s.Active = f.Active
	for _, g := range f.Filters.Groups {
		pct := 100
		if g.RolloutPercentage != nil {
			pct = *g.RolloutPercentage
		}
		if pct > s.MaxRolloutPercentage {
			s.MaxRolloutPercentage = pct
		}
		if len(g.Properties) == 0 && pct == 100 {
			s.EffectivelyFullRollout = true
		}
	}
	return s
}

// MatchGroups returns every release condition group that targets
// envKey=envValue (e.g. "env"="production" — DESIGN.md's "one tracked
// environment, config-specified"). Shared by RenderConditions and
// IsLiveIn so the matching rule lives in exactly one place.
func MatchGroups(groups []Group, envKey, envValue string) []Group {
	var matched []Group
	for _, g := range groups {
		for _, p := range g.Properties {
			if p.Key == envKey && p.Operator == "exact" && fmt.Sprintf("%v", p.Value) == envValue {
				matched = append(matched, g)
				break
			}
		}
	}
	return matched
}

// RenderConditions renders, as plain text, every group MatchGroups finds
// for envKey=envValue. Conditions are rendered by key/operator/value rather
// than semantically interpreted — DESIGN.md is explicit that trig doesn't
// attempt to label arbitrary property names across every team's convention.
// Values are shown in full: Linear access implies PostHog access on these
// projects, so there's no narrower audience to protect by hiding them.
// Returns a message saying so when nothing matches (a true, useful answer,
// not an error — a flag with no production-shaped group simply isn't in
// production yet).
func RenderConditions(groups []Group, envKey, envValue string) string {
	matched := MatchGroups(groups, envKey, envValue)
	if len(matched) == 0 {
		return fmt.Sprintf("no release condition targets %s=%s", envKey, envValue)
	}

	var lines []string
	for _, g := range matched {
		var parts []string
		for _, p := range g.Properties {
			parts = append(parts, renderProperty(p))
		}
		pct := 100
		if g.RolloutPercentage != nil {
			pct = *g.RolloutPercentage
		}
		lines = append(lines, fmt.Sprintf("- %s → %d%% rollout", strings.Join(parts, " AND "), pct))
	}
	return strings.Join(lines, "\n")
}

// TitleConditions renders a compact, one-line summary of each matched
// group's non-env properties, for callers that need the condition detail
// to survive somewhere Linear actually renders — the Resources row shows
// only an attachment's title, never its metadata (see DESIGN.md). The env
// property itself is omitted since callers already show it separately
// (trig's title carries a "[env=...]" segment of its own). Empty when
// every matched group's only property is env (e.g. a plain env=X gate with
// nothing else to distinguish).
func TitleConditions(groups []Group, envKey, envValue string) string {
	matched := MatchGroups(groups, envKey, envValue)
	var clauses []string
	for _, g := range matched {
		var parts []string
		for _, p := range g.Properties {
			if p.Key == envKey {
				continue
			}
			parts = append(parts, renderProperty(p))
		}
		if len(parts) > 0 {
			clauses = append(clauses, strings.Join(parts, " AND "))
		}
	}
	return strings.Join(clauses, "; ")
}

// renderProperty describes one release-condition property as key, operator,
// and value.
func renderProperty(p Property) string {
	if p.Value != nil {
		return fmt.Sprintf("%s %s %v", p.Key, p.Operator, p.Value)
	}
	return fmt.Sprintf("%s %s", p.Key, p.Operator)
}

// IsLiveIn reports whether this flag has any real effect for
// envKey=envValue: active, and at least one matching group with non-zero
// rollout. Used to pick trig's posthog-live/posthog-dark Linear label — a
// generic, structurally-computable signal, not an attempt to interpret
// team-specific property semantics.
func (f FeatureFlag) IsLiveIn(envKey, envValue string) bool {
	if !f.Active {
		return false
	}
	for _, g := range MatchGroups(f.Filters.Groups, envKey, envValue) {
		pct := 100
		if g.RolloutPercentage != nil {
			pct = *g.RolloutPercentage
		}
		if pct > 0 {
			return true
		}
	}
	return false
}
