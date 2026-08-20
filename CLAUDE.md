# Repository guide for coding agents

`AGENTS.md` is a symlink to this file.

## Start here

[`DESIGN.md`](DESIGN.md) is the source of truth for what this tool is and why — read it before
writing any code. It's not a wishlist; every design choice in it is backed by something actually
checked this repo's founding session (PostHog's own API/source, Linear's tool surface, GitHub
search, LaunchDarkly's real docs), not assumed. The "First things to verify before writing real
logic" section at the bottom names the specific unknowns to resolve before the first real commit.

## Scope discipline

v1 is an on-demand CLI. Don't build the scheduled or event-driven tiers described in DESIGN.md's
"Trigger model" section unless explicitly asked to — they're documented as the next steps, not
part of this pass, and each is a materially bigger commitment (a standing host, for the
event-driven tier) than what's being asked for now.

## Conventions

- Go, per the `go-cli-versioning` pattern: no hand-maintained version const — `git describe` via
  `-ldflags`, a `buildVersion()` fallback chain, a `Makefile` with `install`/`build` targets.
- Personal API keys for both PostHog and Linear (own env vars, own local config) — not an OAuth
  app, not multi-tenant. See DESIGN.md's "Auth" section for why that's a deliberate boundary, not
  a shortcut.
- This is a private, standalone repo (`github.com/justinstimatze/trig`) — it has nothing to do
  with aipotluck.org's own codebase or CI, even though the problem that motivated it came from
  there.
