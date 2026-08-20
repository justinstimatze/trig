package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/justinstimatze/trig/internal/config"
	"github.com/justinstimatze/trig/internal/posthog"
)

const flagsUsage = `usage: trig flags [SEARCH]

Lists PostHog flags (key and tags), optionally filtered to keys containing
SEARCH (case-insensitive substring). Read-only. Exists so 'trig link' has a
way to find a FLAG-KEY without leaving the terminal — the "no flag tagged"
error from 'trig status' points here.

Exit codes: 0 ok, 4 API key unauthorized/missing scope, 1 other failure.
`

func cmdFlags(args []string) {
	search := ""
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(flagsUsage)
			os.Exit(exitOK)
		default:
			search = a
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fail("flags", err)
	}
	client := posthog.NewClient(cfg)

	all, err := client.ListFlags()
	if err != nil {
		fail("flags", err)
	}

	var matched []posthog.FeatureFlag
	for _, f := range all {
		if search == "" || strings.Contains(strings.ToLower(f.Key), strings.ToLower(search)) {
			matched = append(matched, f)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Key < matched[j].Key })

	for _, f := range matched {
		tags := "-"
		if len(f.Tags) > 0 {
			tags = strings.Join(f.Tags, ", ")
		}
		fmt.Printf("%s\t%s\n", f.Key, tags)
	}
	fmt.Fprintf(os.Stderr, "%d flag(s)\n", len(matched))
}
