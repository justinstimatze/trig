package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/justinstimatze/trig/internal/linear"
	"github.com/justinstimatze/trig/internal/posthog"
)

// Exit codes, distinct per failure class so a script or an agent can
// branch on $? instead of string-matching stderr.
const (
	exitOK        = 0
	exitError     = 1 // unexpected/unclassified failure
	exitUsage     = 2 // bad arguments
	exitNotLinked = 3 // trig status run before trig link
	exitAuth      = 4 // API key unauthorized or missing a required scope
	exitNotFound  = 5 // ticket, flag, or label doesn't exist
	exitPartial   = 6 // trig sweep: at least one ticket failed, others may have succeeded
)

// fail prints a classified, actionable message for err and exits with the
// matching code. Every subcommand's fatal error path goes through this
// instead of ad hoc fmt.Fprintln+os.Exit, so failure classes — and their
// exit codes — stay consistent across link/status/flags/unlink.
func fail(cmd string, err error) {
	var phAuth *posthog.AuthError
	var lnAuth *linear.AuthError
	var phNotFound *posthog.NotFoundError
	var lnNotFound *linear.NotFoundError
	switch {
	case errors.As(err, &phAuth):
		fmt.Fprintf(os.Stderr, "trig %s: %v\ncheck the PostHog key's scopes (needs feature_flag:read, and feature_flag:write for `trig link`/`trig unlink`)\n", cmd, err)
		os.Exit(exitAuth)
	case errors.As(err, &lnAuth):
		fmt.Fprintf(os.Stderr, "trig %s: %v\ncheck the Linear key's permissions at linear.app/settings/account/security\n", cmd, err)
		os.Exit(exitAuth)
	case errors.As(err, &phNotFound), errors.As(err, &lnNotFound):
		fmt.Fprintln(os.Stderr, "trig "+cmd+":", err)
		os.Exit(exitNotFound)
	default:
		fmt.Fprintln(os.Stderr, "trig "+cmd+":", err)
		os.Exit(exitError)
	}
}
