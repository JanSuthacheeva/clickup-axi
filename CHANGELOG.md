# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0-rc.3] - 2026-07-14

### Changed

- A `search` scoped with `--space`/`--list` that matches nothing now
  re-runs once with the location scope dropped and inlines the top
  matches it hid (id and title, capped at three), so a wrongly guessed
  space is a one-step recovery instead of a dead end. The re-check is
  client-side under the same page budget, returns silently on any API
  error so an optional widening never turns a search into a failure,
  and withholds the "rerun without `--space`/`--list`" hint when
  dropping the flags would leave an unbounded `--assignee all`.
- The session-start dashboard's `default_list:` line now resolves the
  configured value to the concrete list by name (id and source
  alongside) and states plainly that a bare `tasks create "<name>"`
  lands there, so an agent connects the user's words to the default and
  stops asking which space or list. The name lookup shares the
  dashboard's hard 5-second budget and degrades to the raw value on a
  slow or failed resolve.

## [1.0.0-rc.2] - 2026-07-13

### Added

- `members`: a first-class listing of the workspace's members (id,
  name, email) - who tasks can be assigned to. The data comes along
  with the workspace fetch, so discovery costs a single request
  instead of a deliberately failed `--assignee`.
- The session-start dashboard prints the effective `default_list`
  (value and source) when one is configured, so an agent knows a bare
  `tasks create` works without reading the config first.

### Changed

- The `tasks move` status-conflict refusal now suggests the target
  list's entry status as a concrete `--status` value alongside the
  full status vocabulary it already echoed.

## [1.0.0-rc.1] - 2026-07-12

### Changed

- The generated agent skill is a third smaller (14.0KB -> 9.4KB, about
  1,000 tokens): comment alignment padding is gone, near-duplicate
  command examples are merged, and the prose keeps only the behavioral
  guidance the command comments cannot carry. No command changed.
- Two `search` help hints now name the exact flags to rerun with
  instead of describing the adjustment; listing rows escape the id
  cell the way subtask rows already did; the session-start dashboard
  drops its generic `--help` hint (the skill carries the surface).
- Release candidates (hyphenated tags) publish as GitHub prereleases,
  so `releases/latest` - the URL installs and self-update resolve -
  keeps serving the last stable release. RC binaries still show the
  passive update notice when the final release lands.

### Fixed

- Malformed flag input is rejected before any API call everywhere:
  the task view names an unknown single-dash flag instead of treating
  it as a task id, `tasks move` no longer swallows a following flag as
  the value of `--list`/`--space`/`--status`, and `search --list`
  refuses a non-numeric list id instead of forwarding it to the API
  where it would filter to a confident false zero.

### Removed

- The `docs/` folder (v1.0.0 roadmap and design specs): every roadmap
  item shipped. The two post-1.0 intents moved to issues #23
  (`tasks archive`) and #24 (benchmark extension).

### Added

- `tasks <id>` now includes task hierarchy: a subtask shows its immediate
  parent's id, title, and status, while every detail view reports either a
  compact `subtasks[N]{id,title,status}` table of direct children or an
  explicit zero-subtasks state. The relationship fetch is detail-only,
  nested descendants stay out of the default view, and forced custom-id
  workspaces never leak internal relationship ids.
- `tasks move <id> --list <name|id>` moves a task to another list via
  ClickUp's v3 move endpoint (one atomic call; memberships in
  additional lists are untouched and subtasks move with their parent).
  A list name needs `--space`, a numeric id works alone - the same
  rule as create. The task keeps its status when the target list has
  it (statuses match by name); otherwise the move fails with the
  target's statuses inlined and `--status "<status>"` picks the
  landing status explicitly - never a silent remap, and a `--status`
  the move does not need is refused rather than smuggled into a second
  write. Already-home is a stated no-op; the confirmation carries both
  list ids so moving back is one command.
- `tasks close <id>` closes a task by setting the list's closed-type
  status (no need to know its name). As the first destructive op it is
  guarded: without `--yes` it is a dry run stating the exact status
  change and writing nothing; the binary never prompts, so the agent
  relays the dry run and adds `--yes` only after the user confirms.
  Already-closed is a stated no-op, and the confirmation echoes the
  previous status as a one-step reopen hint via `tasks edit --status`.
- `config` command and layered defaults: `config` shows the effective
  values with each one's source, `config set` / `config unset` write
  them. `default_list` makes `--list` optional on `tasks create`;
  precedence is explicit flag > `CLICKUP_AXI_DEFAULT_LIST` > project
  file (`.clickup-axi.toml` at the git root, found from any
  subdirectory) > personal file (`~/.config/clickup-axi/config.toml`),
  both flat TOML. `set` validates the value against the API before
  writing and stores the resolved list id; `--project` targets the
  committable project file. The `folder:<id|name>` value form points
  at a sprint folder: every create derives the folder's current list
  (start/due range containing today, else the newest), so a biweekly
  sprint rollover needs no reconfiguration. Create confirmations
  annotate a defaulted list with its provenance, and stale or
  malformed defaults fail with the config source named and a recovery
  hint inline.

## [0.6.0] - 2026-07-12

### Added

- `tasks edit <id> --parent <task id>` makes a task a subtask or moves
  it under a different parent, sent atomically with any other field
  changes in the same PUT. The parent must be a different task in the
  same list, and a request that would make a task a descendant of itself
  is refused before any write (the proposed parent's ancestor chain is
  walked to catch cycles at any depth). Clearing a parent is unsupported
  by ClickUp's API and reported as such. A task that already has the
  requested parent is a stated no-op.
- `tasks create "<name>" --list <name|id>` creates a task, completing
  the write loop: `--status`, `--assignee`, `--priority`, `--due`,
  `--body`, and `--tag` set fields at creation in one atomic POST, all
  validated together before anything is written (bad values report in
  one aggregated list; tags must already exist in the space and are
  written in their stored casing). A list name needs `--space` because
  list names are only unique within one space; a numeric list id works
  alone. `--parent <task id>` creates a subtask, deriving the list from
  the parent and refusing a contradicting `--list`. The confirmation
  echoes the server-stored id, list, status, and url.
- `--fields` adds columns to `tasks` and `search` listings on request
  (`assignees`, `priority`, `tags`, `list`, `url`), rendered from the
  response already fetched - no extra API calls. The flag is additive
  on top of the default schema, repeatable and comma-separated, and
  shared with the task view, where fields already shown are silently
  absorbed. Unknown names fail before any API call with the vocabulary
  inlined.
- `tasks --page N` reaches listings beyond the first 100 tasks. A full
  page states its position, and the next-page hint carries the current
  filters (and `--fields`) forward.
- `spaces` lists the active spaces in the selected workspace. `lists
  --space <name|id>` discovers active Lists in one space, including
  folder context to distinguish duplicate names; `--archived` instead
  returns archived Lists from both active and archived folders. Missing
  scopes and invalid flags fail with inline recovery commands, and an
  upstream failure never prints a partial inventory.
- `setup` installs a session-start hook for Claude Code
  (`~/.claude/settings.json`), Codex (`~/.codex/hooks.json`), and
  OpenCode (a managed plugin file), so agent sessions begin with the
  user's open tasks already in context. `--global` targets every host
  whose config dir exists; `--project` writes into the current
  directory's `.claude`/`.codex` configs (OpenCode is global-only).
  Rerunning repairs a moved binary path and is otherwise a no-op;
  `--remove` uninstalls, touching nothing but clickup-axi's own
  entries. In a terminal the scope is prompted; on agent paths it must
  be explicit.
- `--due` (on `tasks create` and `tasks edit`) and the `search`
  `--updated-after`/`--updated-before` bounds accept signed day/week
  offsets (`+3days`, `-1week`) alongside `YYYY-MM-DD`. Offsets resolve
  from today in the ClickUp workspace timezone, and due/updated dates
  now render in that timezone too rather than the host's local zone -
  the workspace zone is resolved once per client and cached on disk for
  24 hours (no extra API call on ordinary commands), falling back
  silently to the local zone when it is unknown.
- `context`, the hook's payload: a compact dashboard of the user's 5
  most urgent open tasks (due-soonest first, total stated) behind a
  hard 5-second budget. It always exits 0 and degrades to a one-line
  reason (not authenticated, workspace unpinned, API unreachable) so a
  broken network can never break a session start.

### Changed

- **Breaking:** the task view no longer prints `url:` by default -
  agents almost never browse. Opt back in with `--fields url`.
- Self-contained outputs (task view, counts, confirmations) no longer
  end with static `help[]` hints; only the `--full` escape hatches for
  actually-truncated content remain (AXI section 9).
- Locally decidable bad values (`--priority`, `--due`, `--page`) are
  usage errors (exit 2) caught before any API call, still aggregated
  with each other; server-derived validation keeps exit 1.

### Fixed

- `auth logout --help` no longer performs the logout: auth subcommands
  validate trailing arguments instead of ignoring them.
- Truncated comment text discloses its total size and offers `--full`,
  even when the comment count itself was not cut.
- Network failures print a short translated message instead of Go's
  raw transport error (full URL, DNS internals).
- A numeric `--assignee` id is validated against the workspace's
  members instead of confidently reporting zero tasks for nobody.

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

[1.0.0-rc.3]: https://github.com/JanSuthacheeva/clickup-axi/compare/v1.0.0-rc.2...v1.0.0-rc.3
[1.0.0-rc.2]: https://github.com/JanSuthacheeva/clickup-axi/compare/v1.0.0-rc.1...v1.0.0-rc.2
[1.0.0-rc.1]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.6.0...v1.0.0-rc.1
[0.6.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/JanSuthacheeva/clickup-axi/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/JanSuthacheeva/clickup-axi/releases/tag/v0.1.0
