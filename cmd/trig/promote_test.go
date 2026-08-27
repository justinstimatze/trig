package main

import (
	"testing"

	"github.com/justinstimatze/trig/internal/posthog"
)

func TestPromotionState(t *testing.T) {
	cases := []struct {
		name   string
		states []posthog.RolloutState
		want   posthog.RolloutState
	}{
		{name: "single live flag is live", states: []posthog.RolloutState{posthog.StateLive}, want: posthog.StateLive},
		{name: "single custom flag is custom", states: []posthog.RolloutState{posthog.StateCustom}, want: posthog.StateCustom},
		{name: "single dark flag is dark", states: []posthog.RolloutState{posthog.StateDark}, want: posthog.StateDark},
		{
			name:   "all live required for live — one custom among live flags holds it at custom",
			states: []posthog.RolloutState{posthog.StateLive, posthog.StateCustom, posthog.StateLive},
			want:   posthog.StateCustom,
		},
		{
			name:   "any dark flag holds the whole ticket at dark, even with a live sibling",
			states: []posthog.RolloutState{posthog.StateLive, posthog.StateDark},
			want:   posthog.StateDark,
		},
		{
			name:   "dark outranks custom too",
			states: []posthog.RolloutState{posthog.StateCustom, posthog.StateDark},
			want:   posthog.StateDark,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := promotionState(tc.states); got != tc.want {
				t.Errorf("promotionState(%v) = %q, want %q", tc.states, got, tc.want)
			}
		})
	}
}

func TestPromotionStateNames(t *testing.T) {
	cases := []struct {
		state posthog.RolloutState
		want  string
	}{
		{posthog.StateDark, "Dark"},
		{posthog.StateCustom, "Canary"},
		{posthog.StateLive, "Done"},
	}
	for _, tc := range cases {
		if got := promotionStateNames[tc.state]; got != tc.want {
			t.Errorf("promotionStateNames[%q] = %q, want %q", tc.state, got, tc.want)
		}
	}
}
