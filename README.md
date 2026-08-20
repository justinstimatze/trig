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
it edits the same attachment in place instead of piling up duplicates. `trig flags [SEARCH]` lists
PostHog flags read-only, for finding the key you want, and `trig unlink` forgets a pairing. Every
subcommand takes `-h` for its full usage and exit codes; `status` adds `--dry-run` to preview a
write without making one, and `--json` for when you want the exit code, not the sentence.

`trig sweep [--env V] [--json] [--dry-run]` is `status` run against every linked ticket at once —
no ticket argument, it finds them all from PostHog's own tag data. Built to run unattended on a
schedule; see `DESIGN.md`'s "Trigger model" section for the credential and workflow file it still
needs before that's actually wired up anywhere.
