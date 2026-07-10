# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.0] - 2026-07-10

### Added

- `tasks edit <id>` now covers the full field set alongside `--status`
  and `--assignee`/`--unassign`: `--priority urgent|high|normal|low|none`
  (`none` clears), `--due YYYY-MM-DD` or `--due none`, `--name "<title>"`,
  `--body "<markdown>"` (replace the description) or `--append-body
  "<markdown>"` (add below the existing one), and
  `--add-tag`/`--remove-tag` (both repeatable and comma-separated) for
  tags that already exist in the space - an unknown tag fails with the
  space's tags inlined. Every field is validated together in one
  pre-flight pass, so all invalid fields are reported at once; the writes
  then apply tags first and the atomic `PUT` last, rolling the tag ops
  back on a later failure so the edit stays all-or-nothing. Re-applying
  the current state (same value, existing tag) is a stated no-op.
- `tasks <id>` now shows the task's tags.

## [0.4.0] - 2026-07-09

### Added

- `tasks edit <id> --assignee <who>` / `--unassign <who>` change a task's
  assignees; both flags repeat and accept a comma-separated list, and
  `<who>` is `me`, a member name, or an id (resolved case-insensitively).
  They compose with `--status` in a single atomic update, every field is
  validated before anything is written, and re-adding an existing
  assignee or removing an absent one is a stated no-op.
- `tasks --assignee <who>` lists a teammate's open tasks instead of your
  own: `me` (default), `all`, or a member's name/id, resolved
  case-insensitively like `search`. `tasks --space <name|id>` narrows the
  listing to one space (project). `--assignee all` requires `--space` as a
  bound, since a workspace-wide scan is otherwise unbounded.

## [0.3.0] - 2026-07-09

### Added

- `search <query>` finds tasks by title or description text. ClickUp's
  API has no text-search endpoint, so it filters tasks server-side
  (paged and bounded) and ranks matches locally: title above
  description, with every query word required to match. To stay bounded
  and relevant it searches only your own tasks by default and hides the
  final `closed` status; each result prints a `scope:` line stating
  exactly what was searched. `--assignee all` widens the search and then
  requires at least one bounding filter (such as `--status`, `--space`,
  or `--updated-after`).
- Spaces and assignees resolve by name, not just id, across `search`
  (`--space`, `--assignee`): an exact case-insensitive name/email match,
  then a unique substring, with every miss or ambiguity inlining the
  candidate `id,name` pairs for a one-step retry.

## [0.2.1] - 2026-07-08

### Added

- `CLICKUP_AXI_WORKSPACE` pins the workspace that `tasks` and
  custom-id resolution operate in, making the CLI usable with tokens
  that see more than one workspace. The home view echoes the pin and
  hints at setting it; the generated skill gains a "Workspace setup"
  section that asks the user which workspace to pin.

### Fixed

- With several visible workspaces, `tasks` and custom-id lookups no
  longer dead-end: the error now names `CLICKUP_AXI_WORKSPACE` and
  inlines the visible `id,name` pairs so recovery is one retry.

## [0.2.0] - 2026-07-08

### Added

- `tasks comment <id> --text` - add a comment to a task. Ids resolve
  like everywhere else (internal first, custom fallback); the text is
  validated as non-empty before any API call.

### Changed

- Slimmed the README down to five sections (what it is, skill-only
  installation, quickstart, environment variables, auto updates); the
  manual binary install lives in the skill's Install section only.

### Fixed

- An unknown custom task id no longer reports "ClickUp rejected the
  token": ClickUp answers 401 for ids outside the token's scope, which
  now translates to "task not found" when the token itself is valid.

## [0.1.1] - 2026-07-08

### Changed

- Restructured the module into the standard Go `cmd/` + `internal/`
  layout with a hexagonal dependency rule. No behavior change; source
  builds now use `go build ./cmd/clickup-axi`.
- Reworked the README: a new Quick start leads with the skill-only
  install (the agent downloads the binary on first use), the manual
  binary install moved to an Installation section, and the Agent Skill
  section is worded agent-agnostically.

## [0.1.0] - 2026-07-07

### Added

- `tasks` - list open tasks assigned to the user.
- `tasks <id>` - view one task with metadata, description, and newest
  comments; `--full` lifts truncation. Ids resolve as internal first
  with a custom-id fallback (`CLICKUP_AXI_CUSTOM_IDS=1` forces
  custom-only).
- `tasks edit <id> --status` - change a task's status; an invalid
  status echoes the list's valid ones.
- `auth login` / `auth logout` - token stored at
  `~/.config/clickup-axi/token`, `CLICKUP_TOKEN` takes precedence.
- AXI output contract: structured stdout, `help[]` next-step hints,
  exit codes 0/1/2, explicit zero results, translated API errors.
- Generated agent skill (`skill --write` / `--check`) with a CI
  freshness gate.
- Release workflow publishing prebuilt binaries (linux/darwin
  amd64+arm64, windows amd64) with `SHA256SUMS`.
- `update` - checksum-verified atomic self-update from the latest
  release, plus a passive once-per-24h update notice and healing of
  installed skill copies (`CLICKUP_AXI_NO_UPDATE_CHECK=1` disables).

[Unreleased]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/JanSuthacheeva/clickup-axi/releases/tag/v0.1.0
