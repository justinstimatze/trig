package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/justinstimatze/trig/internal/config"
	"github.com/justinstimatze/trig/internal/posthog"
)

const linkUsage = `usage: trig link TICKET-ID FLAG-KEY [--dry-run]

Tags the PostHog flag FLAG-KEY with linear:TICKET-ID. One-time per pair;
idempotent, safe to re-run — a no-op if the tag is already there. Run
'trig status TICKET-ID' next to report and write the Linear attachment.

  --dry-run   Print what would change; make no PostHog write.

Exit codes: 0 ok, 2 bad arguments, 4 API key unauthorized/missing scope
(needs feature_flag:write), 5 flag not found, 1 other failure.
`

// cmdLink establishes the flag→ticket link: trig link TICKET-ID FLAG-KEY
// tags the PostHog flag `linear:TICKET-ID`. Idempotent — a no-op when the
// tag is already there. Separate from `status` so the future scheduled
// tier (DESIGN.md's "Trigger model") can re-run status repeatedly without
// re-touching the flag's tags each time.
func cmdLink(args []string) {
	ticketID, flagKey, dryRun := parseTicketFlagArgs(args, linkUsage)

	cfg, err := config.Load()
	if err != nil {
		fail("link", err)
	}
	client := posthog.NewClient(cfg)

	flag, err := client.GetFlagByKey(flagKey)
	if err != nil {
		fail("link", err)
	}

	// PostHog lowercases tags server-side ("linear:CUR-515" round-trips as
	// "linear:cur-515"), so comparison must be case-insensitive even though
	// the tag we'd write is unchanged.
	tag := "linear:" + ticketID
	for _, t := range flag.Tags {
		if strings.EqualFold(t, tag) {
			fmt.Printf("%s already tagged %s\n", flagKey, tag)
			return
		}
	}

	if dryRun {
		fmt.Printf("[dry-run] would tag %s with %s\n", flagKey, tag)
		return
	}

	newTags := append(append([]string{}, flag.Tags...), tag)
	if _, err := client.SetTags(flag.ID, newTags); err != nil {
		fail("link", err)
	}
	fmt.Printf("tagged %s with %s\n", flagKey, tag)
	fmt.Printf("next: trig status %s\n", ticketID)
}

// parseTicketFlagArgs parses the "TICKET-ID FLAG-KEY [--dry-run]" shape
// shared by link and unlink, printing usage on any parse failure.
func parseTicketFlagArgs(args []string, usage string) (ticketID, flagKey string, dryRun bool) {
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(exitOK)
		case "--dry-run":
			dryRun = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "unknown flag %q\n\n%s", args[i], usage)
				os.Exit(exitUsage)
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		fmt.Print(usage)
		os.Exit(exitUsage)
	}
	return positional[0], positional[1], dryRun
}
