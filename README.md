# trig

"Just check PostHog" is not a process. It's what people say instead of having one. `trig` writes
the flag's actual rollout state onto the Linear ticket, so nobody has to say it in standup again.

Start with [`DESIGN.md`](DESIGN.md) for the research behind that: what PostHog's API will and
won't do, what Linear's will and won't do, and why the honest answer to "why not just a webhook"
is "there isn't one."

## Status

v1: on-demand CLI, Go, nothing to host.

```
$ make install
$ trig link CUR-198 my-flag-key
$ trig status CUR-198
```

`trig link` remembers which flag belongs to which ticket. `trig status` checks PostHog's current
rollout state for that flag and writes it onto the ticket as a Linear attachment — safe to re-run,
it edits the same attachment in place instead of piling up duplicates. It also writes one
ticket-wide label per environment checked — `posthog-VALUE:dark` / `:custom` / `:live` — so
`--env preview` and `--env production` each own their own honest label instead of fighting over a
single unqualified one. `trig flags [SEARCH]` lists PostHog flags read-only, for finding the key
you want, and `trig unlink` forgets a pairing. Every subcommand takes `-h` for its full usage and
exit codes; `status` adds `--dry-run` to preview a write without making one, and `--json` for when
you want the exit code, not the sentence.

`trig sweep [--env V] [--json] [--dry-run]` is `status` run against every linked ticket at once —
no ticket argument, it finds them all from PostHog's own tag data. If every ticket it checks comes
back dark in the tracked env, it says so instead of staying quiet — the signature of a cron pinned
to the wrong environment, not of nothing having shipped. Meant to run unattended on a schedule; see
`DESIGN.md`'s "Trigger model" section for the credential and workflow file that lives in the
consuming project's own repo.

`sweep` also owns the part of a ticket's lifecycle Linear's own git-merge automations can't: every
ticket at state "Merged" gets checked against the Linear Release API, and once its code has actually
reached a completed release, moved to "Dark"/"Canary"/"Done" (flagged) or straight to "Done"
(unflagged) — always against production, regardless of `--env`. See `DESIGN.md`'s "Ticket lifecycle:
Merged → released" section for the full state machine and why a permanently-partial rollout never
gets force-closed.

`trig reconcile FILE [--json]` catches the other direction of the same lie: a flag key declared in
application code, shipped, tests green, that nobody ever actually created in PostHog. Feed it a
JSON array of `{"key","ticket"}` — a file path or `-` for stdin — and it names every entry with no
live match. It never creates the missing flag; picking rollout scope is a person's call.
