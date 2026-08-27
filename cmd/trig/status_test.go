package main

import (
	"testing"

	"github.com/justinstimatze/trig/internal/posthog"
)

func TestTitleSummary(t *testing.T) {
	cases := []struct {
		name       string
		summary    posthog.RolloutSummary
		conditions string
		want       string
	}{
		{
			name:    "inactive",
			summary: posthog.RolloutSummary{Active: false},
			want:    "inactive",
		},
		{
			name:    "effectively full rollout",
			summary: posthog.RolloutSummary{Active: true, EffectivelyFullRollout: true},
			want:    "fully rolled out",
		},
		{
			name:       "partial rollout with no matching condition",
			summary:    posthog.RolloutSummary{Active: true, MaxRolloutPercentage: 50},
			conditions: "no release condition targets env=production",
			want:       "50% rollout, no matching condition",
		},
		{
			name:       "partial rollout with a matching condition",
			summary:    posthog.RolloutSummary{Active: true, MaxRolloutPercentage: 25},
			conditions: "- env exact production → 25% rollout",
			want:       "25% rollout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleSummary(tc.summary, tc.conditions); got != tc.want {
				t.Errorf("titleSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseStatusArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantTicket string
		wantEnv    string
		wantJSON   bool
		wantDryRun bool
	}{
		{
			name:       "ticket only defaults to production",
			args:       []string{"CUR-515"},
			wantTicket: "CUR-515",
			wantEnv:    "production",
		},
		{
			name:       "explicit env",
			args:       []string{"CUR-515", "--env", "preview"},
			wantTicket: "CUR-515",
			wantEnv:    "preview",
		},
		{
			name:       "json and dry-run flags",
			args:       []string{"CUR-515", "--json", "--dry-run"},
			wantTicket: "CUR-515",
			wantEnv:    "production",
			wantJSON:   true,
			wantDryRun: true,
		},
		{
			name:       "flags before the positional ticket id",
			args:       []string{"--env", "preview", "CUR-515"},
			wantTicket: "CUR-515",
			wantEnv:    "preview",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ticketID, envValue, jsonOut, dryRun := parseStatusArgs(tc.args)
			if ticketID != tc.wantTicket || envValue != tc.wantEnv || jsonOut != tc.wantJSON || dryRun != tc.wantDryRun {
				t.Errorf("parseStatusArgs(%v) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
					tc.args, ticketID, envValue, jsonOut, dryRun, tc.wantTicket, tc.wantEnv, tc.wantJSON, tc.wantDryRun)
			}
		})
	}
}

func TestStateLabel(t *testing.T) {
	cases := []struct {
		env   string
		state posthog.RolloutState
		want  string
	}{
		{env: "preview", state: posthog.StateCustom, want: "posthog-preview:custom"},
		{env: "production", state: posthog.StateLive, want: "posthog-production:live"},
		{env: "staging", state: posthog.StateDark, want: "posthog-staging:dark"},
	}
	for _, tc := range cases {
		if got := stateLabel(tc.env, tc.state); got != tc.want {
			t.Errorf("stateLabel(%q, %q) = %q, want %q", tc.env, tc.state, got, tc.want)
		}
	}
}

func TestAggregateState(t *testing.T) {
	cases := []struct {
		name   string
		states []posthog.RolloutState
		want   posthog.RolloutState
	}{
		{name: "empty is dark", states: nil, want: posthog.StateDark},
		{name: "all dark is dark", states: []posthog.RolloutState{posthog.StateDark, posthog.StateDark}, want: posthog.StateDark},
		{name: "any custom beats dark", states: []posthog.RolloutState{posthog.StateDark, posthog.StateCustom}, want: posthog.StateCustom},
		{name: "any live beats custom and dark", states: []posthog.RolloutState{posthog.StateCustom, posthog.StateLive, posthog.StateDark}, want: posthog.StateLive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateState(tc.states); got != tc.want {
				t.Errorf("aggregateState(%v) = %q, want %q", tc.states, got, tc.want)
			}
		})
	}
}
