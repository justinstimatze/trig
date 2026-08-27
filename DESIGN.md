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

Labels, all list/filter-visible in Linear:
- `posthog-flag` (workspace-level, generic) — this ticket ships behind a flag.
- `posthog-VALUE:dark` / `:custom` / `:live` (ticket-wide, one per environment ever checked,
  namespaced by that environment's own `--env` value — never more than one state per environment at
  once). `dark` means no matched flag has any effect there; `live` means at least one matched flag
  targets everyone unconditionally at 100%; `custom` is everything in between — a percentage under
  100, or a group gated by another property alongside the environment one. Computed once across
  every matched flag per run, not per-flag, so two flags in different states on one ticket can't
  fight over the label within a single run — and namespaced by environment so two different `--env`
  runs can't fight over each other's label either, which an earlier, unqualified `posthog-live` /
  `posthog-dark` pair did (see "Environment-qualified labels" below). Every run also removes that
  legacy pair unconditionally, so a ticket it once mislabeled self-heals on its next real run.

**Environment-qualified labels** — the unqualified `posthog-live`/`posthog-dark` pair this replaced
was correct per its own contract but misleading in practice: a consuming project's unattended
`trig sweep --env preview` cron (real, running every 20 minutes since 2026-08-20, discovered
auditing a live Linear board rather than assumed) wrote the same bare `posthog-live` label a
production sweep would, and it reads to a board-scanning human as "live for users" regardless of
which environment actually produced it — 16 tickets carried it despite none having any
production-scoped rollout. Considered and rejected: gating the write to a single privileged env
(e.g. only `production` may touch the label) — that would have silently frozen the preview cron's
labels with no signal anything had changed, the same "check that cannot go red" shape one layer
down. Considered and rejected: copying LaunchDarkly's own per-environment flag-status taxonomy
(`new`/`active`/`launched`/`inactive`, confirmed against LaunchDarkly's docs) — that's a staleness
axis (is this flag still being evaluated), a different question from rollout shape, and would
require per-environment evaluation-request volume PostHog doesn't expose (PostHog's own
`last_called_at` and `active=STALE` filter, confirmed against its API reference, track staleness
per-flag, not per-environment-condition, since PostHog has no first-class per-environment object
the way LaunchDarkly does — "environment" here is always a property condition on one flag, per
"Linking" above). `dark`/`custom`/`live` stays on the rollout-shape question this tool exists to
answer, computed only from `RolloutSummary`/`StateIn`'s existing literal reading of a flag's
release conditions — no new interpretation, no new PostHog surface, just a label that finally
names which environment it's talking about.

**Ticket lifecycle: Merged → released** — aipotluck.org settled on a 5-state Linear ticket
lifecycle to close the same "what does Done mean" ambiguity from the other direction: `In Review ->
Merged -> Dark -> Canary -> Done`. Linear's own git-merge automations own exactly the first two
transitions (`PR-opened -> In Review`, `PR-merged-to-develop -> Merged`) — everything past `Merged`
has to be owned by trig, because Linear's release-completion automation can't conditionally branch
per-issue on whether a ticket carries `posthog-flag`, and that branch is exactly what's needed, so
that automation is deliberately left off rather than misused.

For every ticket at state `Merged`, `trig sweep` checks (via `Issue.releases` in Linear's Release
API — `Release.stage.type == "completed"`) whether its code has actually reached a completed
release on `main`, via aipotluck.org's own `linear-release-action`-driven pipeline. Not yet: left
alone, no-op — engineering finished the PR, the release pipeline hasn't confirmed it's on `main`
yet. Confirmed: no flag means nothing gates it, straight to `Done`; a flag means `Dark`/`Canary`/
`Done` from that flag's *production* rollout state specifically — always production, independent of
whatever `--env` the rest of that sweep run was checking, since a ticket's real lifecycle state is
answering "is this real for users," which only production release data can say.

`Done` requires every matched flag to be live, not any — the opposite rule from the ticket-wide
label's `aggregateState` (any-live-wins). That rule is fine for a label (harmless to overstate,
freely re-computed every run) but was going to force-close a ticket carrying more than one flag,
where one is intentionally parked below 100% forever (CUR-92's own ticket text: turning that flag on
in prod is "Justin's call to make deliberately, not something that should go live as a side
effect") and a sibling flag on the same ticket happens to go live. Once a ticket leaves `Merged`
this logic never revisits it — CUR-92 lands at `Canary` and stays there regardless of what its flag
does afterward, with no heuristic needed to detect that it's meant to be permanent; the state machine
being forward-only-from-`Merged` handles it for free.

Considered and rejected: a sixth state, `Released`, after `Done`. Under this mapping `Done` already
*is* released on both branches — unflagged, `main` is what aipotluck.org's continuous pipeline
deploys from, so merged and released are the same event; flagged, `live` means 100% unconditional
rollout, which is the definition of released, not an approximation of it. A `Released` state would
need a signal true after `Done` but not at the moment `Done` becomes true, and neither PostHog nor
Linear's release data expose one. The only real candidate — GTM/comms announcement timing — isn't
observable from anything trig reads, so automating a transition into it isn't possible, and setting
it by hand reintroduces the exact ambiguity this lifecycle exists to remove.

**CLI** — `trig link TICKET-ID FLAG-KEY` / `trig unlink` manage the tag. `trig status TICKET-ID
[--env V] [--json] [--dry-run]` is the main command. `trig sweep [--env V] [--json] [--dry-run]` is
the same report run against every ticket with a linked flag instead of one named on the command
line — it discovers tickets by listing every `linear:*`-tagged flag rather than being told which
to check, which is what makes it runnable unattended. `trig flags [SEARCH]` lists PostHog flags
read-only. `trig reconcile REGISTRY-FILE [--json]` covers the direction none of the above can see:
a codebase's flag-key registry says a flag exists, PostHog says otherwise. Every subcommand takes
`-h`/`--help`. Exit codes are distinct per failure class (2 bad args, 3 not linked yet, 4
unauthorized/missing scope, 5 not found, 6 sweep partial failure, 7 reconcile found a gap, 1 other)
so a script or an agent can branch on `$?` instead of parsing stderr text — backed by typed
`AuthError`/`NotFoundError` in both API clients. Within `sweep`, one ticket failing doesn't stop
the others — it's logged and skipped — but an `AuthError` aborts the whole run immediately, since
it will fail identically for every remaining ticket.

**Reconciliation** — `trig sweep` answers "is this flag's rollout visible on its ticket," which
presupposes the flag exists. It says nothing about a flag key a codebase *declares* — in a registry
file, a constants module, wherever a project centralizes its flag keys — that nobody ever actually
created in PostHog. That gap shipped twice in one consuming project's history before either was
noticed, both times because nothing in `lint`/`test`/`build` talks to PostHog at all; the only way
to catch it was running `trig flags <key>` by hand. `trig reconcile` takes a JSON array of
`{"key","ticket"}` (a file, or `-` for stdin) and reports every entry with no live PostHog flag —
distinguishing "never created" from "existed once, now deleted," since those call for different
fixes. It's read-only on both sides: no PostHog write, no Linear write. Deliberately not
auto-create-on-gap — a human still has to pick the flag's rollout scope (env condition, tester
group, which plane reads it), and guessing that risks repeating exactly the by-hand mistake this
command exists to catch. The registry format is JSON rather than trig parsing a consuming project's
source directly (e.g. grepping a TypeScript file) — regex/AST-scraping another language's source is
fragile and this project's own convention (see the root `CLAUDE.md`) is against exactly that
pattern for audits; a project wanting `reconcile` in CI adds a small export step that dumps its
registry as JSON, which is also the only shape that works regardless of what language declares the
registry.

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
  mutations: `issueUpdate` (labels, and — via its `stateId` field — the Merged-ticket lifecycle
  promotion), `attachmentCreate`/`attachmentUpdate` (the report), rollout state lives in `metadata`
  (a `JSONObject`), only the `title` is rendered in Linear's UI. `issues(filter: {state: {name:
  {eq:...}}})` is sweep's Merged-ticket discovery query (paginated via `pageInfo`/`after`, same
  convention as `posthog.Client.ListFlags`) — a materially wider read than `issue(id:)`'s
  single-ticket lookup, since it has to find every Merged ticket workspace-wide, not just ones
  already known to carry a flag. `workflowStates(filter: {team: {id:...}, name: {eq:...}})` resolves
  a state name to an ID — team-scoped, since Linear has no workspace-level state, so this needs the
  issue's own `team.id`. `Issue.releases` (a direct connection, no join through `issueToReleases`
  needed) with `Release.stage.type == "completed"` is the release-confirmation check. Every field and
  input name here was confirmed live against Linear's own GraphQL introspection
  (`api.linear.app/graphql` answers named-type introspection with no key required), not assumed.
