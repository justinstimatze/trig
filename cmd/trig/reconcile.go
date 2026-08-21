package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/justinstimatze/trig/internal/config"
	"github.com/justinstimatze/trig/internal/posthog"
)

const reconcileUsage = `usage: trig reconcile REGISTRY-FILE [--json]

Reads a flag registry — a JSON array of {"key": FLAG-KEY, "ticket": TICKET-ID}
— from REGISTRY-FILE (pass "-" to read stdin), and reports every registry
entry with no matching live PostHog flag. Catches a gap none of trig's other
commands can see: a flag key declared and shipped in application code, tests
passing, that nobody ever actually created in PostHog — invisible to
lint/test/build because none of them talk to PostHog.

Fails loud, never creates: a missing flag means a human still has to decide
its rollout scope (env condition, tester group, which plane reads it) — that
judgment call belongs to a person, so trig only reports the gap.

  --json   Print one JSON report to stdout instead of prose.

Exit codes: 0 every registry entry found live, 7 at least one entry has no
live flag (or only a deleted one), 2 bad arguments, 4 API key
unauthorized/missing scope, 1 other failure.
`

type registryEntry struct {
	Key    string `json:"key"`
	Ticket string `json:"ticket"`
}

type missingFlag struct {
	Key    string `json:"key"`
	Ticket string `json:"ticket"`
	Reason string `json:"reason"` // "not_found" or "deleted"
}

type reconcileOutput struct {
	CheckedAt     string        `json:"checked_at"`
	RegistryCount int           `json:"registry_count"`
	Missing       []missingFlag `json:"missing"`
	OK            bool          `json:"ok"`
}

// cmdReconcile is trig's registry-vs-reality check: given a source-of-truth
// list of flag keys a codebase declares, it names every one PostHog doesn't
// actually have — the gap that let agent-artifact-download and
// better-auth-signin ship with a dead flag reference. See reconcileUsage.
func cmdReconcile(args []string) {
	registryPath, jsonOut := parseReconcileArgs(args)

	entries, err := loadRegistry(registryPath)
	if err != nil {
		fail("reconcile", err)
	}
	if len(entries) == 0 && !jsonOut {
		fmt.Fprintln(os.Stderr, "trig reconcile: registry is empty — nothing to check")
	}

	cfg, err := config.Load()
	if err != nil {
		fail("reconcile", err)
	}
	client := posthog.NewClient(cfg)

	flags, err := client.ListFlags()
	if err != nil {
		fail("reconcile", err)
	}

	missing := findMissing(entries, flags)

	out := reconcileOutput{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		RegistryCount: len(entries),
		Missing:       missing,
		OK:            len(missing) == 0,
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fail("reconcile", err)
		}
	} else if len(missing) == 0 {
		fmt.Printf("%d registry entry(s) checked, all found live in PostHog\n", len(entries))
	} else {
		fmt.Printf("%d of %d registry entry(s) have no live PostHog flag:\n", len(missing), len(entries))
		for _, m := range missing {
			fmt.Printf("  %s (%s): %s\n", m.Key, m.Ticket, m.Reason)
		}
	}

	if len(missing) > 0 {
		os.Exit(exitReconcileMissing)
	}
}

// findMissing reports every entry whose Key has no live (non-deleted)
// PostHog flag. A key that exists only as a deleted flag is reported
// distinctly ("deleted") from one that never existed ("not_found") — both
// mean the registry's promise isn't currently kept, but they call for
// different fixes.
func findMissing(entries []registryEntry, flags []posthog.FeatureFlag) []missingFlag {
	live := map[string]bool{}
	deletedOnly := map[string]bool{}
	for _, f := range flags {
		if f.Deleted {
			if !live[f.Key] {
				deletedOnly[f.Key] = true
			}
			continue
		}
		live[f.Key] = true
		delete(deletedOnly, f.Key)
	}

	var missing []missingFlag
	for _, e := range entries {
		if live[e.Key] {
			continue
		}
		reason := "not_found"
		if deletedOnly[e.Key] {
			reason = "deleted"
		}
		missing = append(missing, missingFlag{Key: e.Key, Ticket: e.Ticket, Reason: reason})
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	return missing
}

func parseReconcileArgs(args []string) (registryPath string, jsonOut bool) {
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(reconcileUsage)
			os.Exit(exitOK)
		case "--json":
			jsonOut = true
		default:
			if len(args[i]) > 1 && args[i][0] == '-' {
				fmt.Fprintf(os.Stderr, "unknown flag %q\n\n%s", args[i], reconcileUsage)
				os.Exit(exitUsage)
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 1 {
		fmt.Print(reconcileUsage)
		os.Exit(exitUsage)
	}
	return positional[0], jsonOut
}

// loadRegistry reads and parses a JSON array of {"key","ticket"} from path,
// or stdin when path is "-".
func loadRegistry(path string) ([]registryEntry, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read registry: %w", err)
		}
		defer f.Close()
		r = f
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var entries []registryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	for i, e := range entries {
		if e.Key == "" {
			return nil, fmt.Errorf("registry entry %d has an empty key", i)
		}
	}
	return entries, nil
}
