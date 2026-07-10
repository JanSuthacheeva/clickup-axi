# `setup` + `context` session hook - design

Step 4 of `docs/v1.0.0.md`: ambient context per AXI section 7. Two new
commands. `setup` is the installer a user runs once: it registers a
SessionStart hook in each detected agent host (Claude Code, Codex,
OpenCode). `context` is the payload that hook executes at every session
start: a tiny live dashboard (open tasks + hints) whose stdout the
harness injects into the agent's context, so every session begins
knowing the CLI exists and what is on the user's plate.

Reviewed and approved 2026-07-10 (lavish artifact
`.lavish/setup-command-design.html`).

## `context` - the payload

```
clickup-axi context
```

Loads on every session, so the output is ruthlessly minimal: a
discovery line, at most 5 open tasks, `help[]` hints. Reuses the
existing list path (`SelectTeam` -> team tasks, `cmdTasksList`'s
fetch) but renders its own capped view.

Happy path:

```
clickup-axi: ClickUp CLI (tasks, search, edit, comment)
tasks[5/12]{id,title,status,due}:
  86ey3tx8m,Fix auth bug,in progress,2026-07-11
  ...

help[3]:
  Run `clickup-axi tasks` for all 12 open tasks
  Run `clickup-axi tasks <id>` for details and comments
  Run `clickup-axi --help` for all commands
```

- **Cap 5** tasks, ordered due-soonest first (overdue included), then
  no-due. `tasks[5/12]` states shown/total; truncation is explicit.
  With 5 or fewer the header is plain `tasks[3]{...}`.
- **Exit 0 always.** A session start must never break or spam. Every
  degraded path keeps the discovery line and swaps the task block for
  one `tasks: unavailable ...` line plus a recovery hint:
  - no token -> `tasks: unavailable (not authenticated)` + hint to ask
    the user to run `clickup-axi auth login` in their terminal;
  - network/API error or budget breach -> `tasks: unavailable right
    now` + retry hint;
  - multiple workspaces, no `CLICKUP_AXI_WORKSPACE` pin -> unavailable
    line + pin hint (mirrors `SelectTeam` recovery, but exit 0).
- **Time budget:** hard ~3s cap on the whole ClickUp fetch via request
  context; on breach, the degraded output above. No raw API errors ever.
- `CLICKUP_AXI_CUSTOM_IDS` honored: custom ids displayed as everywhere
  else.
- **Post-command lines stay enabled** (update notice, skill heal): the
  session-start injection doubles as the ambient update channel. The
  notice is already capped (1/24h, 500ms budget) and the heal is local.
- Surface: listed in the top-level help usage column ("hook payload;
  installed by `setup`") but **not** in the generated agent skill -
  agents never invoke it themselves.

## `setup` - the installer

```
clickup-axi setup [--global | --project] [--app <host>] [--remove]
```

- **Scope:** on a real TTY (same detection as `auth login`), prompt
  `global (recommended) / project`, Enter = global. Non-TTY: `--global`
  or `--project` is required; bare `setup` exits 2 with both flags
  inlined so an agent can relay the choice and retry in one step.
- **Detect + report:** global scope installs into every host whose
  config dir exists (`~/.claude`, `~/.codex`, `~/.config/opencode`);
  a missing dir reports `skipped (not found)`. Project scope installs
  for claude-code and codex unconditionally, creating `.claude/` or
  `.codex/` in the cwd when needed - the user explicitly chose the
  project, so there is nothing to detect; opencode reports
  `skipped (global only)`. Each host reports one line: `installed`,
  `repaired` (stale binary path updated), `already installed (no-op)`,
  `removed`, `not installed (no-op)`, or `skipped (...)`. `--app`
  limits to one host (`claude-code | codex | opencode`).
- **Exit codes:** 0 for every idempotent outcome incl. all-skipped;
  1 when a host's config exists but cannot be read/parsed/written
  (that host errors, the others still proceed); 2 usage.
- **Binary path rule (AXI section 7):** the hook command is the bare
  `clickup-axi context` when `exec.LookPath("clickup-axi")` resolves
  (symlinks evaluated) to the running executable, else the absolute
  path to the running executable. Re-running `setup` repairs a stale
  path after a reinstall or move.
- **`--remove`** deletes only our entry (never the surrounding config),
  same scope/app selection, idempotent.
- Ends with `help[]`: start a new session to see the dashboard /
  `setup --remove` to uninstall.

Per-host mechanics:

| Host | Global | Project |
|---|---|---|
| claude-code | JSON-merge hook entry into `~/.claude/settings.json` | `.claude/settings.json` in cwd |
| codex | `~/.codex/hooks.json`; verify `[features] hooks = true` in `~/.codex/config.toml`, print a hint when missing - setup never edits TOML | `.codex/hooks.json` in cwd |
| opencode | own managed file `~/.config/opencode/plugins/clickup-axi.js` (marker header, full-file ownership, overwrite freely) | skipped (global only) |

Claude Code / Codex JSON handling: read -> unmarshal to
`map[string]any` -> insert or patch only our entry (recognized by its
command containing `clickup-axi`) -> marshal with 2-space indent.
Foreign keys and entries are preserved. Known cosmetic trade-off:
`encoding/json` map marshaling sorts keys alphabetically, so an
existing file gets re-ordered once. Stdlib-only rules out an
order-preserving JSON library. No file locking in v1; the
read-merge-write window racing the host's own writes is a documented
limitation.

## Architecture

New driven adapter `internal/hostcfg`, sibling to `internal/update`:
host table, detection, and Install/Remove per host, returning a report
the CLI renders. It never imports `cli` and knows nothing about
ClickUp; the resolved hook command string is an input.

```go
type Host struct { Name string; ... }          // claude-code, codex, opencode
type Action int                                 // Installed, Repaired, NoOp, Removed, NotInstalled, Skipped, Failed
type Report struct { Host, Path string; Action Action; Detail string }

func Detect(scope Scope, cwd string) []Host
func Install(h Host, scope Scope, hookCmd string) Report
func Remove(h Host, scope Scope) Report
```

(Exact signatures may shift in the plan; the boundary - strings in,
reports out, no ClickUp imports - is the contract.)

Files:

- `internal/hostcfg/hostcfg.go` - NEW: host table, scope, report types
- `internal/hostcfg/claude.go`, `codex.go`, `opencode.go` - NEW:
  per-host config mechanics
- `internal/hostcfg/*_test.go` - NEW: temp-dir fakes, golden outputs
- `internal/cli/setup.go` - NEW: flags, TTY scope prompt (x/term, like
  `auth login`), report rendering
- `internal/cli/context.go` - NEW: dashboard command
- `internal/cli/cli.go` - dispatch entries for both commands

Dependencies stay stdlib + `golang.org/x/term` (already present).

## Surface / skill / docs (same commit set)

- `internal/cli/surface.go` - `setup` row with a consent comment like
  `update`'s ("only after user consent"); `context` usage-only row,
  no skill line.
- `internal/cli/skill_template.md` - mention that `setup` installs the
  session hook.
- `go run ./cmd/clickup-axi skill --write` - regenerate.
- `README.md` - session-hook section (what it does, how to remove).
- `docs/v1.0.0.md` - tick the checklist row.

## Tests

- `hostcfg` unit (temp-dir HOME per host): fresh install; merge into a
  populated settings.json preserving foreign keys; repair stale path;
  same-path no-op; remove; remove-when-absent; invalid JSON -> Failed
  report; missing dir -> Skipped.
- `cli` context (`newFakeClickUp`): happy path exact bytes; cap + total
  hint (12 tasks -> 5 shown); 5-or-fewer plain header; no token; API
  error; multi-team unpinned; custom-ids mode. Exit 0 asserted on every
  degraded path.
- `cli` setup: non-TTY missing scope flag -> exit 2 with both flags
  inlined; per-host report rendering; `--remove` idempotency; unknown
  `--app` -> exit 2 valid hosts inlined.
- E2E: built binary `context` against the real API (read-only);
  `setup --global` + `--remove` against a temp HOME, never the real
  `~/.claude`; finally one real install on this machine as smoke test.

## Research first (implementation step 0)

The three host integration shapes are cited from the vendored AXI doc
and memory, not verified against current docs. Before writing
`hostcfg`, confirm:

1. Claude Code `hooks.SessionStart[].hooks[]{type,command}` schema in
   settings.json;
2. Codex `hooks.json` schema + the `[features] hooks` flag;
3. OpenCode plugin API: event name and how stdout/returned text becomes
   session context.

If a host turns out to need a different mechanism, only that host's
file in `hostcfg` changes; the command surface and report model hold.

## Out of scope

Skill installation (`npx skills add` covers it); session-end lifecycle
capture (AXI section 7 "richer over time"); per-directory task
filtering; a config file (arrives with `create`, step 6).
