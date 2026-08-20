package main

import (
	"fmt"
	"strings"

	"github.com/justinstimatze/trig/internal/config"
	"github.com/justinstimatze/trig/internal/posthog"
)

const unlinkUsage = `usage: trig unlink TICKET-ID FLAG-KEY [--dry-run]

Removes the linear:TICKET-ID tag from the PostHog flag FLAG-KEY — the
inverse of 'trig link'. Idempotent; a no-op if the tag isn't there. Does
NOT touch anything already written to the Linear ticket (labels,
attachments) — run trig status again after unlinking if those should
reflect the change too.

  --dry-run   Print what would change; make no PostHog write.

Exit codes: 0 ok, 2 bad arguments, 4 API key unauthorized/missing scope
(needs feature_flag:write), 5 flag not found, 1 other failure.
`

func cmdUnlink(args []string) {
	ticketID, flagKey, dryRun := parseTicketFlagArgs(args, unlinkUsage)

	cfg, err := config.Load()
	if err != nil {
		fail("unlink", err)
	}
	client := posthog.NewClient(cfg)

	flag, err := client.GetFlagByKey(flagKey)
	if err != nil {
		fail("unlink", err)
	}

	tag := "linear:" + ticketID
	remaining := []string{}
	found := false
	for _, t := range flag.Tags {
		if strings.EqualFold(t, tag) {
			found = true
			continue
		}
		remaining = append(remaining, t)
	}
	if !found {
		fmt.Printf("%s was not tagged %s\n", flagKey, tag)
		return
	}

	if dryRun {
		fmt.Printf("[dry-run] would remove tag %s from %s\n", tag, flagKey)
		return
	}

	if _, err := client.SetTags(flag.ID, remaining); err != nil {
		fail("unlink", err)
	}
	fmt.Printf("removed tag %s from %s\n", tag, flagKey)
}
