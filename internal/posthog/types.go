package posthog

// FeatureFlag is the subset of PostHog's feature_flags response shape trig
// needs. Fields PostHog returns but trig has no use for are omitted.
type FeatureFlag struct {
	ID      int      `json:"id"`
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Active  bool     `json:"active"`
	Deleted bool     `json:"deleted"`
	Tags    []string `json:"tags"`
	Filters Filters  `json:"filters"`
}

type Filters struct {
	Groups []Group `json:"groups"`
}

// Group is one release condition: all Properties must match (AND) for a
// user to fall in this group, then RolloutPercentage gates what fraction of
// matching users actually get the flag.
type Group struct {
	Properties        []Property `json:"properties"`
	RolloutPercentage *int       `json:"rollout_percentage"`
	// AggregationGroupID is set when a group targets a PostHog "group"
	// (e.g. organization/company) rather than an individual person — decoded
	// for shape completeness but not read anywhere, since trig's rollout
	// math (Rollout, IsLiveIn) treats every group the same regardless of
	// what it aggregates on.
	AggregationGroupID *int `json:"aggregation_group_type_index"`
}

// Property is one condition within a group, e.g. {key: "env", type:
// "person", value: "preview", operator: "exact"}. Value is untyped because
// its shape depends on Operator — absent for "is_not_set", a string for
// "exact", potentially an array for "in"-style operators.
type Property struct {
	Key      string      `json:"key"`
	Type     string      `json:"type"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value,omitempty"`
}

// flagListResponse is PostHog's paginated list envelope for
// GET /api/projects/{id}/feature_flags/. Next is a full URL to the next
// page, or empty when this is the last page.
type flagListResponse struct {
	Count   int           `json:"count"`
	Next    string        `json:"next"`
	Results []FeatureFlag `json:"results"`
}
