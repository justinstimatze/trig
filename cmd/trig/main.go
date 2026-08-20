// Command trig cross-references a PostHog feature flag's live rollout state
// onto its linked Linear ticket, on demand. See DESIGN.md for the problem
// this solves and what's decided vs. still open.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// version is "dev" by default and baked at release time via
//
//	go install -ldflags "-X main.version=$(git describe --tags --always --dirty)" ./cmd/trig
//
// The git tag is the single source of truth — there is no hand-maintained
// version constant to drift out of sync. buildVersion() resolves it.
var version = "dev"

// buildVersion reports the binary's version, preferring (in order): a
// release value baked in via -ldflags; the module version when installed
// with `go install …@vX.Y.Z`; the embedded VCS commit (+dirty) for local
// `go build`. Falls back to "dev" when none is available (e.g. a tarball
// build outside a git tree).
func buildVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 12 {
				rev = s.Value[:12]
			} else {
				rev = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev != "" {
		return rev + dirty
	}
	return version
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(exitUsage)
	}
	args := os.Args[1:]
	switch args[0] {
	case "link":
		cmdLink(args[1:])
	case "unlink":
		cmdUnlink(args[1:])
	case "status":
		cmdStatus(args[1:])
	case "sweep":
		cmdSweep(args[1:])
	case "flags":
		cmdFlags(args[1:])
	case "version", "--version", "-v":
		fmt.Println("trig", buildVersion())
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "trig: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		os.Exit(exitUsage)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `trig — cross-reference a PostHog flag's rollout state onto its Linear ticket

Usage:
  trig link CUR-198 FLAG-KEY      Tag the PostHog flag linear:CUR-198. One-time
                                   per pair; idempotent, safe to re-run.
  trig unlink CUR-198 FLAG-KEY    Remove that tag. Inverse of link.
  trig status CUR-198 [--env V]   Find the flag(s) tagged linear:CUR-198, render
                                   rollout state for env V (default "production"),
                                   write it to the ticket: the "posthog-flag" label,
                                   a "posthog-live"/"posthog-dark" state label, and a
                                   create-or-update attachment per flag.
  trig sweep [--env V]             Find every ticket with a linked flag on its
                                   own (no ticket named) and run status for
                                   each. Meant for a schedule, not by hand.
  trig flags [SEARCH]             List PostHog flags and their tags (read-only) —
                                   use this to find a FLAG-KEY for link/unlink.
  trig version                    Print version.

Each subcommand takes -h/--help for its full usage, flags, and exit codes.
status, sweep, and link/unlink also take --dry-run to preview writes without
making them; status and sweep also take --json for machine-readable output.
`)
}
