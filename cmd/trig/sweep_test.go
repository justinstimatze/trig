package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/justinstimatze/trig/internal/posthog"
)

func TestGroupByTicket(t *testing.T) {
	flags := []posthog.FeatureFlag{
		{ID: 1, Key: "agent-mode", Tags: []string{"linear:cur-515"}},
		{ID: 2, Key: "other-flag", Tags: []string{"linear:cur-515", "unrelated"}},
		{ID: 3, Key: "shared-flag", Tags: []string{"linear:cur-600", "linear:cur-601"}}, // one flag, two tickets
		{ID: 4, Key: "no-ticket", Tags: []string{"unrelated"}},
		{ID: 5, Key: "empty-tag", Tags: []string{"linear:"}}, // malformed, must be skipped
	}

	got := groupByTicket(flags)

	wantKeys := []string{"cur-515", "cur-600", "cur-601"}
	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("groupByTicket() keys = %v, want %v", gotKeys, wantKeys)
	}

	if len(got["cur-515"]) != 2 {
		t.Errorf("cur-515 bucket = %d flags, want 2", len(got["cur-515"]))
	}
	if len(got["cur-600"]) != 1 || got["cur-600"][0].Key != "shared-flag" {
		t.Errorf("cur-600 bucket = %+v, want just shared-flag", got["cur-600"])
	}
	if len(got["cur-601"]) != 1 || got["cur-601"][0].Key != "shared-flag" {
		t.Errorf("cur-601 bucket = %+v, want just shared-flag", got["cur-601"])
	}
}

func TestParseSweepArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantEnv    string
		wantJSON   bool
		wantDryRun bool
	}{
		{
			name:    "defaults",
			args:    nil,
			wantEnv: "production",
		},
		{
			name:    "explicit env",
			args:    []string{"--env", "preview"},
			wantEnv: "preview",
		},
		{
			name:       "json and dry-run",
			args:       []string{"--json", "--dry-run"},
			wantEnv:    "production",
			wantJSON:   true,
			wantDryRun: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, jsonOut, dryRun := parseSweepArgs(tc.args)
			if env != tc.wantEnv || jsonOut != tc.wantJSON || dryRun != tc.wantDryRun {
				t.Errorf("parseSweepArgs(%v) = (%q, %v, %v), want (%q, %v, %v)",
					tc.args, env, jsonOut, dryRun, tc.wantEnv, tc.wantJSON, tc.wantDryRun)
			}
		})
	}
}
