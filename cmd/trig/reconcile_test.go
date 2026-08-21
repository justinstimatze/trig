package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/justinstimatze/trig/internal/posthog"
)

func TestFindMissing(t *testing.T) {
	entries := []registryEntry{
		{Key: "agent-mode", Ticket: "CUR-515"},
		{Key: "never-created", Ticket: "CUR-999"},
		{Key: "retired-flag", Ticket: "CUR-100"},
	}
	flags := []posthog.FeatureFlag{
		{Key: "agent-mode", Deleted: false},
		{Key: "retired-flag", Deleted: true},
		{Key: "unrelated-flag", Deleted: false},
	}

	got := findMissing(entries, flags)

	want := []missingFlag{
		{Key: "never-created", Ticket: "CUR-999", Reason: "not_found"},
		{Key: "retired-flag", Ticket: "CUR-100", Reason: "deleted"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findMissing() = %+v, want %+v", got, want)
	}
}

func TestFindMissing_DeletedThenRecreatedCountsAsLive(t *testing.T) {
	// Same key appears twice in PostHog's list — once deleted, once live
	// (a flag deleted and recreated under the same key). The live copy must
	// win regardless of slice order.
	entries := []registryEntry{{Key: "flappy-flag", Ticket: "CUR-1"}}
	flags := []posthog.FeatureFlag{
		{Key: "flappy-flag", Deleted: true},
		{Key: "flappy-flag", Deleted: false},
	}

	got := findMissing(entries, flags)
	if len(got) != 0 {
		t.Errorf("findMissing() = %+v, want none — a live copy exists alongside the deleted one", got)
	}
}

func TestFindMissing_AllPresentReturnsNil(t *testing.T) {
	entries := []registryEntry{{Key: "agent-mode", Ticket: "CUR-515"}}
	flags := []posthog.FeatureFlag{{Key: "agent-mode", Deleted: false}}

	if got := findMissing(entries, flags); len(got) != 0 {
		t.Errorf("findMissing() = %+v, want none", got)
	}
}

func TestLoadRegistry_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, []byte(`[{"key":"agent-mode","ticket":"CUR-515"}]`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := loadRegistry(path)
	if err != nil {
		t.Fatalf("loadRegistry() error = %v", err)
	}
	want := []registryEntry{{Key: "agent-mode", Ticket: "CUR-515"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadRegistry() = %+v, want %+v", got, want)
	}
}

func TestLoadRegistry_EmptyKeyIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, []byte(`[{"key":"","ticket":"CUR-515"}]`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := loadRegistry(path); err == nil {
		t.Fatal("loadRegistry() error = nil, want an error for an empty key")
	}
}

func TestLoadRegistry_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := loadRegistry(path); err == nil {
		t.Fatal("loadRegistry() error = nil, want a parse error")
	}
}

func TestLoadRegistry_MissingFile(t *testing.T) {
	if _, err := loadRegistry("/nonexistent/path/registry.json"); err == nil {
		t.Fatal("loadRegistry() error = nil, want an error for a missing file")
	}
}

func TestParseReconcileArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantJSON bool
	}{
		{name: "file only", args: []string{"registry.json"}, wantPath: "registry.json"},
		{name: "stdin", args: []string{"-"}, wantPath: "-"},
		{name: "json flag", args: []string{"registry.json", "--json"}, wantPath: "registry.json", wantJSON: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, jsonOut := parseReconcileArgs(tc.args)
			if path != tc.wantPath || jsonOut != tc.wantJSON {
				t.Errorf("parseReconcileArgs(%v) = (%q, %v), want (%q, %v)",
					tc.args, path, jsonOut, tc.wantPath, tc.wantJSON)
			}
		})
	}
}
