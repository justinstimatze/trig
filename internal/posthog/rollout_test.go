package posthog

import (
	"testing"
)

func intPtr(n int) *int { return &n }

func TestRollout(t *testing.T) {
	cases := []struct {
		name string
		flag FeatureFlag
		want RolloutSummary
	}{
		{
			name: "inactive flag",
			flag: FeatureFlag{Active: false},
			want: RolloutSummary{Active: false},
		},
		{
			name: "no groups is not effectively full — nothing targets anyone",
			flag: FeatureFlag{Active: true},
			want: RolloutSummary{Active: true},
		},
		{
			name: "one group, no properties, nil rollout_percentage means everyone",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: nil, RolloutPercentage: nil},
				}},
			},
			want: RolloutSummary{Active: true, MaxRolloutPercentage: 100, EffectivelyFullRollout: true},
		},
		{
			name: "gated by a condition, not effectively full even at 100%",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{
						Properties:        []Property{{Key: "env", Operator: "exact", Value: "production"}},
						RolloutPercentage: intPtr(100),
					},
				}},
			},
			want: RolloutSummary{Active: true, MaxRolloutPercentage: 100, EffectivelyFullRollout: false},
		},
		{
			name: "max across multiple groups",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{RolloutPercentage: intPtr(30)},
					{RolloutPercentage: intPtr(70)},
				}},
			},
			want: RolloutSummary{Active: true, MaxRolloutPercentage: 70, EffectivelyFullRollout: false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.flag.Rollout()
			if got != tc.want {
				t.Errorf("Rollout() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatchGroups(t *testing.T) {
	groups := []Group{
		{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}},
		{Properties: []Property{{Key: "env", Operator: "exact", Value: "preview"}}},
		{Properties: []Property{{Key: "plan", Operator: "exact", Value: "production"}}}, // wrong key
		{Properties: []Property{{Key: "env", Operator: "is_set"}}},                      // wrong operator
	}

	matched := MatchGroups(groups, "env", "production")
	if len(matched) != 1 {
		t.Fatalf("MatchGroups() matched %d groups, want 1", len(matched))
	}
	if matched[0].Properties[0].Value != "production" {
		t.Errorf("matched wrong group: %+v", matched[0])
	}
}

func TestRenderConditions_NoMatch(t *testing.T) {
	got := RenderConditions(nil, "env", "production")
	want := "no release condition targets env=production"
	if got != want {
		t.Errorf("RenderConditions() = %q, want %q", got, want)
	}
}

func TestRenderConditions_ShowsMultiValueProperty(t *testing.T) {
	groups := []Group{
		{
			Properties: []Property{
				{Key: "env", Operator: "exact", Value: "production"},
				{Key: "email", Operator: "in", Value: []interface{}{"a@x.com", "b@x.com", "c@x.com"}},
			},
			RolloutPercentage: nil,
		},
	}

	got := RenderConditions(groups, "env", "production")
	want := "- env exact production AND email in [a@x.com b@x.com c@x.com] → 100% rollout"
	if got != want {
		t.Errorf("RenderConditions() = %q, want %q", got, want)
	}
}

func TestRenderConditions_ShowsScalarValueAndValuelessProperty(t *testing.T) {
	groups := []Group{
		{
			Properties: []Property{
				{Key: "env", Operator: "exact", Value: "production"},
				{Key: "plan", Operator: "exact", Value: "enterprise"},
				{Key: "$initial_referrer", Operator: "is_set", Value: nil},
			},
			RolloutPercentage: intPtr(50),
		},
	}

	got := RenderConditions(groups, "env", "production")
	want := "- env exact production AND plan exact enterprise AND $initial_referrer is_set → 50% rollout"
	if got != want {
		t.Errorf("RenderConditions() = %q, want %q", got, want)
	}
}

func TestTitleConditions_MultipleGroupsJoinedCompactly(t *testing.T) {
	groups := []Group{
		{Properties: []Property{
			{Key: "env", Operator: "exact", Value: "preview"},
			{Key: "tester", Operator: "is_not_set"},
		}},
		{Properties: []Property{
			{Key: "tester", Operator: "exact", Value: "justin"},
			{Key: "env", Operator: "exact", Value: "preview"},
		}},
		{Properties: []Property{
			{Key: "tester", Operator: "exact", Value: "dogfood"},
			{Key: "env", Operator: "exact", Value: "preview"},
		}},
	}

	got := TitleConditions(groups, "env", "preview")
	want := "tester is_not_set; tester exact justin; tester exact dogfood"
	if got != want {
		t.Errorf("TitleConditions() = %q, want %q", got, want)
	}
}

func TestTitleConditions_EmptyWhenOnlyEnvGates(t *testing.T) {
	groups := []Group{
		{Properties: []Property{{Key: "env", Operator: "exact", Value: "preview"}}},
	}

	got := TitleConditions(groups, "env", "preview")
	if got != "" {
		t.Errorf("TitleConditions() = %q, want empty", got)
	}
}

func TestIsLiveIn(t *testing.T) {
	cases := []struct {
		name string
		flag FeatureFlag
		want bool
	}{
		{
			name: "inactive flag is never live regardless of groups",
			flag: FeatureFlag{
				Active: false,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(100)},
				}},
			},
			want: false,
		},
		{
			name: "active, matching group, zero rollout is not live",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(0)},
				}},
			},
			want: false,
		},
		{
			name: "active, matching group, nonzero rollout is live",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(1)},
				}},
			},
			want: true,
		},
		{
			name: "active, no matching group for this env is not live",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "preview"}}, RolloutPercentage: intPtr(100)},
				}},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.flag.IsLiveIn("env", "production"); got != tc.want {
				t.Errorf("IsLiveIn() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStateIn(t *testing.T) {
	cases := []struct {
		name string
		flag FeatureFlag
		want RolloutState
	}{
		{
			name: "inactive flag is dark regardless of groups",
			flag: FeatureFlag{
				Active: false,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(100)},
				}},
			},
			want: StateDark,
		},
		{
			name: "no matching group for this env is dark",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "preview"}}, RolloutPercentage: intPtr(100)},
				}},
			},
			want: StateDark,
		},
		{
			name: "matching group, zero rollout is dark",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(0)},
				}},
			},
			want: StateDark,
		},
		{
			name: "matching group, 100% and only the env property, is live",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: nil},
				}},
			},
			want: StateLive,
		},
		{
			name: "matching group, 100% but gated by another property, is custom",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{
						Properties: []Property{
							{Key: "env", Operator: "exact", Value: "production"},
							{Key: "tester", Operator: "exact", Value: "justin"},
						},
						RolloutPercentage: intPtr(100),
					},
				}},
			},
			want: StateCustom,
		},
		{
			name: "matching group, partial percentage, is custom",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(25)},
				}},
			},
			want: StateCustom,
		},
		{
			name: "one dark group and one fully live group for the env is live",
			flag: FeatureFlag{
				Active: true,
				Filters: Filters{Groups: []Group{
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: intPtr(0)},
					{Properties: []Property{{Key: "env", Operator: "exact", Value: "production"}}, RolloutPercentage: nil},
				}},
			},
			want: StateLive,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.flag.StateIn("env", "production"); got != tc.want {
				t.Errorf("StateIn() = %q, want %q", got, tc.want)
			}
		})
	}
}
