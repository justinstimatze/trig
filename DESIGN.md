# trig

Cross-references a PostHog feature flag's live rollout state onto its linked Linear ticket.

## The problem

Linear "Done" and "actually visible to a user" are different facts once a feature ships behind a
flag. That gap is common enough that it's a standing rule in at least one codebase's own contributor
guide: "a feature behind a flag/env-var/config toggle isn't finished when the code merges and tests
pass — only when it's actually flipped on in the place that matters." Nothing outside engineering
can check that today without asking someone or reading code.

## Prior art

No native PostHog↔Linear bridge exists — checked PostHog's own MCP tool surface, a GitHub search
across the public ecosystem, and PostHog's own source (`linear.template.ts`); every path found is
one-directional (Linear → PostHog error tracking), nothing flag-aware.

LaunchDarkly's Jira Cloud integration does the equivalent for a different vendor pair, and shaped
this design: links via a named property on the flag (not a separate manifest), tracks one configured
environment per project (not every release condition), discloses staleness explicitly rather than
implying live truth, and is event-driven behind a paid Marketplace app — the cost of the
gold-standard version, and why v1 here doesn't attempt it.

## Design

**Linking** — a `linear:TICKET-ID` tag on the PostHog flag (possibly several flags per ticket).
PostHog lowercases tags server-side, so all tag comparisons are case-insensitive regardless of what
gets written. `trig link`/`trig unlink` manage this tag; both are idempotent.

**What `trig status` reports**, deliberately only the general half of the problem:
- Rollout completeness — `active`, `max_rollout_percentage`, `effectively_full_rollout`.
- Release conditions for one tracked environment (`--env`, default `production`), rendered
  literally as key/operator/value — not an attempt to semantically label arbitrary property names
  across every team's convention.
- A `checked_at` timestamp, always.
- Explicitly out of scope: whether a feature's *own* env var config exists in a given deployment.
  Real and worth having, but repo-specific — trig as a general tool has no way to know a given
  codebase's manifest format.

**Where it posts** — one Linear attachment per flag (not a comment; comment threads get buried on a
busy ticket). Linear's `attachmentCreate` upserts on `(issueId, url)`, so there is exactly one trig
attachment per flag, always reflecting whichever `--env` was checked most recently — switching
`--env` replaces the previous environment's stored state, and trig reports that switch explicitly
(a printed notice, and `switched_env_from` in `--json`) rather than doing it silently. The
attachment's title carries the compact human-readable summary (e.g. `PostHog: agent-mode
[env=preview] — 100% rollout`) — verified live in Linear's UI, not assumed: the Resources row
renders `title` in full-weight text and `subtitle` inline right after it in muted grey on the same
row, both truncated to fit the row's fixed height, both recoverable in full via a native hover
tooltip. `metadata` alone isn't rendered anywhere in that row, tooltip included — it's stored and
API-retrievable but only reachable by querying Linear directly. The per-property condition
breakdown (key, operator, and value — e.g. `tester exact justin`) goes in `subtitle` so it's at
least hover-visible on the ticket page, and is duplicated into `metadata.conditions` for `--json`
consumers. Values are shown in full, unredacted: this project's convention is that Linear access
implies PostHog access, so there's no narrower audience to hide a targeting identifier from.

Two labels, both list/filter-visible in Linear:
- `posthog-flag` (workspace-level, generic) — this ticket ships behind a flag.
- `posthog-live` / `posthog-dark` (ticket-wide, swapped, never both at once) — whether any linked flag has
  a non-zero-rollout condition for the tracked environment. Computed once across every matched flag
  per run, not per-flag, so two flags in different states on one ticket can't fight over the label
  within a single run.

**CLI** — `trig link TICKET-ID FLAG-KEY` / `trig unlink` manage the tag. `trig status TICKET-ID
[--env V] [--json] [--dry-run]` is the main command. `trig sweep [--env V] [--json] [--dry-run]` is
the same report run against every ticket with a linked flag instead of one named on the command
line — it discovers tickets by listing every `linear:*`-tagged flag rather than being told which
to check, which is what makes it runnable unattended. `trig flags [SEARCH]` lists PostHog flags
read-only. Every subcommand takes `-h`/`--help`. Exit codes are distinct per failure class (2 bad
args, 3 not linked yet, 4 unauthorized/missing scope, 5 not found, 6 sweep partial failure, 1 other)
so a script or an agent can branch on `$?` instead of parsing stderr text — backed by typed
`AuthError`/`NotFoundError` in both API clients. Within `sweep`, one ticket failing doesn't stop
the others — it's logged and skipped — but an `AuthError` aborts the whole run immediately, since
it will fail identically for every remaining ticket.

**Trigger model** — on-demand only for v1, matching every other tool in this ecosystem (hindcast,
plancheck, buddy, defn — all invoked, none hosted). Needs zero hosting. `trig sweep` is the
discovery primitive a scheduled tier needs, but the schedule itself isn't wired up anywhere yet —
that's a cron-triggered GitHub Actions workflow calling a pinned trig release, living in the
consuming project's own repo (alongside its own service credential), not in trig's. A fully
event-driven version (reacting to a PostHog flag change directly) isn't available regardless of
hosting: PostHog has no webhook for a flag being changed (`PostHog/posthog#17361`, open since 2023),
so polling on a schedule is the only currently-available way to do this without standing infra.

**Auth** — personal API keys for both PostHog and Linear, same shape as every other CLI tool here.
Not an OAuth app, not multi-tenant. PostHog needs both `feature_flag:read` and `feature_flag:write`
(the latter for `link`/`unlink`'s tag update). Linear needs read+write — it doesn't support
finer-grained scoping.

**Language** — Go, per the house default for this class of tool. Version pattern from
`go-cli-versioning`: git-tag-derived version via `-ldflags`, `buildVersion()` fallback chain,
`Makefile` with `install`/`build` targets.

## What trig actually calls

- **PostHog**: REST, `https://{host}/api/projects/{project_id}/feature_flags/...`. List/search takes
  `?search=` (substring match; trig filters client-side for an exact key). Update is `PATCH
  .../feature_flags/{id}/`. `Authorization: Bearer {key}`.
- **Linear**: GraphQL, single endpoint `https://api.linear.app/graphql`, `Authorization: {key}` (no
  `Bearer` prefix). `issue(id:)` accepts the human-readable identifier (`CUR-515`) directly. Key
  mutations: `issueUpdate` (labels), `attachmentCreate`/`attachmentUpdate` (the report), rollout
  state lives in `metadata` (a `JSONObject`), only the `title` is rendered in Linear's UI.
