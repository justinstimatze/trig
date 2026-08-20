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
// for envKey=envValue. Conditions are rendered by key/operator rather than
// semantically interpreted — DESIGN.md is explicit that trig doesn't attempt
// to label arbitrary property names across every team's convention. Property
// *values* are redacted (see renderProperty) since this text is written into
// Linear, whose audience can be broader than the PostHog project's own, and
// release conditions are commonly targeted by email/user-id allowlists.
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
			parts = append(parts, renderProperty(p, envKey))
		}
		pct := 100
		if g.RolloutPercentage != nil {
			pct = *g.RolloutPercentage
		}
		lines = append(lines, fmt.Sprintf("- %s → %d%% rollout", strings.Join(parts, " AND "), pct))
	}
	return strings.Join(lines, "\n")
}

// renderProperty describes one release-condition property without exposing
// its targeting value. The envKey property is the one exception: its value
// is an environment name (e.g. "production"), not an identifier, and is
// already surfaced elsewhere (the attachment title's own "[env=...]"), so
// it's safe and useful to show in full. Every other property shows only its
// key, operator, and how many values it's matching against.
func renderProperty(p Property, envKey string) string {
	if p.Key == envKey {
		if p.Value != nil {
			return fmt.Sprintf("%s %s %v", p.Key, p.Operator, p.Value)
		}
		return fmt.Sprintf("%s %s", p.Key, p.Operator)
	}
	if n := valueCount(p.Value); n > 0 {
		plural := "s"
		if n == 1 {
			plural = ""
		}
		return fmt.Sprintf("%s %s (%d value%s)", p.Key, p.Operator, n, plural)
	}
	return fmt.Sprintf("%s %s", p.Key, p.Operator)
}

// valueCount reports how many literal values p.Value holds — PostHog encodes
// a multi-value condition (e.g. an "in" allowlist) as a JSON array, which
// json.Unmarshal into interface{} always produces as []interface{}.
func valueCount(v interface{}) int {
	switch x := v.(type) {
	case nil:
		return 0
	case []interface{}:
		return len(x)
	default:
		return 1
	}
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
