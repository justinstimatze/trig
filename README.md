# trig

Cross-references a PostHog feature flag's live rollout state onto its linked Linear ticket, on
demand.

Start with [`DESIGN.md`](DESIGN.md) for the problem, the prior-art research that shaped it, and
what's decided vs. still open.

Status: v1 is an on-demand CLI, Go, no hosting required — `trig link CUR-198 FLAG-KEY` once per
pair, then `trig status CUR-198` to report and write the Linear attachment (safe to re-run). See
`DESIGN.md`'s "Trigger model" section for why it's split that way, and for what the later
scheduled/event-driven tiers would each cost if it ever gets there.

```
$ make install
$ trig link CUR-198 my-flag-key
$ trig status CUR-198
```

Every subcommand takes `-h`/`--help` for its full usage and exit codes. `trig flags [SEARCH]` lists
PostHog flags read-only (useful for finding a `FLAG-KEY`); `trig unlink` undoes a `link`. `trig
status` also takes `--dry-run` (preview writes, make none) and `--json` (machine-readable output,
no prose).

`trig sweep [--env V] [--json] [--dry-run]` is `status` without a ticket argument: it finds every
ticket with a linked flag on its own and reports on each. Meant to run unattended on a schedule —
see `DESIGN.md`'s "Trigger model" section for what's still needed (a service credential, a workflow
file) before that's actually wired up anywhere.
