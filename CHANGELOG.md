# Changelog

## Unreleased

(none)

## [0.1.2] — 2026-08-20

- `RenderConditions` shows release-condition property values in full instead of redacting
  non-`env` values to a count. Deliberate call: this project treats Linear access as implying
  PostHog access, so there's no narrower audience to hide a targeting identifier from.
- The per-property condition breakdown now writes to the attachment's `subtitle` instead of
  getting appended onto `title`. Verified live: Linear's Resources row renders both title and
  subtitle (truncated, hover for the full text), but a long condition list made title itself
  truncate unreadably; `metadata` alone is stored but never rendered anywhere in that row.
  Multi-line isn't possible here — tested it, both the row and the hover tooltip collapse
  embedded newlines to spaces.

## [0.1.1] — 2026-08-19 — first public release

- `cmd/trig`: `link`, `unlink`, `status`, `sweep`, `flags` subcommands, git-tag-derived version,
  `Makefile`.
- `internal/config`: personal-key loading, env var first, `~/.config/trig/config.json` fallback.
- `internal/posthog`: `GetFlagByKey`, `ListFlags`, `SetTags`, `Rollout()`, `RenderConditions`,
  `IsLiveIn`, typed `AuthError`/`NotFoundError`.
- `internal/linear`: issue/label/attachment read and write (`GetIssueByIdentifier`,
  `GetLabelByName`, `CreateLabel`, `AddLabel`, `RemoveLabel`, `CreateAttachment`,
  `UpdateAttachment`), typed `AuthError`/`NotFoundError`.
- `trig status`: `--env`, `--json`, `--dry-run`. Writes the `posthog-flag` and
  `posthog-live`/`posthog-dark` labels and a per-flag rollout-state attachment.
- `trig sweep`: same report as `status`, run against every ticket with a linked flag instead of
  one named on the command line. Per-ticket failures are logged and skipped; an unauthorized key
  aborts the whole run immediately.
- Distinct exit codes (2/3/4/5/6/1) via a shared `fail()` helper (`6` is sweep-only: partial
  failure, at least one ticket failed).
- CI (`.github/workflows/ci.yml`): vet, gofmt check, `go test -race`, build, `golangci-lint`
  (errcheck, govet, ineffassign, staticcheck, unused). Release (`release.yml` + `.goreleaser.yaml`)
  builds linux/darwin amd64/arm64 binaries on a `v*` tag push. Dependabot auto-merge and CodeQL
  scanning, matching this project's other Go CLIs.
