# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `tasks comment <id> --text` - add a comment to a task. Ids resolve
  like everywhere else (internal first, custom fallback); the text is
  validated as non-empty before any API call.

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

[Unreleased]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/JanSuthacheeva/clickup-axi/releases/tag/v0.1.0
