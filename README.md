# clickup-axi

> [!IMPORTANT]
> **Still in active development.** Stable to use right now, but the
> command surface keeps evolving - always update to the newest version
> (`clickup-axi update`, or agree when the update notice appears).
> Feature requests are very welcome in the
> [issues](https://github.com/JanSuthacheeva/clickup-axi/issues).

A minimal ClickUp CLI for AI agents, following the [AXI](https://axi.md)
design principles: token-efficient output, structured errors, and
next-step hints on its list, error, and mutation output. It covers the
flows agents need most:
listing open tasks (yours or a teammate's, paged past 100 with
--page, extra columns with --fields), finding a task by words in
its title or description, viewing one task with its comments, creating
a task or subtask with its fields set in one call, editing
its status, assignees, priority, due date, name, description, tags, or parent,
commenting on it, and discovering the spaces and Lists to target.

## Installation

Install the agent skill - that is all. Any agent that supports Agent
Skills (Claude Code, Codex, opencode, ...) downloads the binary itself
on first use:

```sh
npx skills add JanSuthacheeva/clickup-axi --skill clickup-axi -g
```

Then authenticate once in your own terminal (the binary appears in
`~/.local/bin` after the agent's first use):

```sh
clickup-axi auth login     # guides you to a token, hidden paste
```

## Quickstart

Mention a task in a conversation ("what's on my plate?", "summarize
HGAI-2316") - or run the CLI directly:

```sh
clickup-axi                          # who am I + workspaces
clickup-axi tasks                    # your open tasks
clickup-axi tasks --assignee ting    # a teammate's open tasks (names resolve)
clickup-axi tasks --fields assignees,priority   # extra columns, no extra API calls
clickup-axi tasks HGAI-2316          # one task with newest comments
clickup-axi search "oauth redirect"  # find your tasks by title/description text
clickup-axi spaces                   # active projects in the workspace
clickup-axi lists --space "Webshop"  # Lists in a project, with folder context
clickup-axi tasks create "Fix login flow" --list "Sprint 12" --space "Webshop"
clickup-axi tasks create "Fix login flow" --list 901234 --priority high --assignee me
clickup-axi tasks create "Test the redirect" --parent HGAI-2316   # subtask, list comes from the parent
clickup-axi tasks edit HGAI-2316 --status "in review"
clickup-axi tasks edit HGAI-2316 --assignee ting --unassign me   # reassign (names resolve)
clickup-axi tasks edit HGAI-2316 --priority high --due 2026-08-01   # multi-field edit, one atomic call
clickup-axi tasks edit HGAI-2316 --due +3days                       # workspace today plus 3 days
clickup-axi tasks edit HGAI-2316 --append-body "QA notes ..."       # add to the description
clickup-axi tasks edit HGAI-2316 --add-tag qa --remove-tag wip      # existing space tags only
clickup-axi tasks edit HGAI-2316 --parent HGAI-2300                 # make/move a subtask (same list)
clickup-axi tasks comment HGAI-2316 --text "Deployed to staging"
```

`tasks edit --parent <task-id>` can make a task a subtask or change
its parent within the same List. ClickUp's API cannot clear a parent,
so promoting a subtask to a standalone task must be done in ClickUp.

Task ids can be internal (`86ey3tx8m`) or custom (`HGAI-2316`).
`clickup-axi tasks --help` and `clickup-axi search --help` have flags
and examples.

### Search

ClickUp's public API has no text-search endpoint, so `search` filters
tasks server-side and ranks the matches locally (title above
description; every query word must match). To stay bounded and
relevant it searches **only your own tasks by default** and hides the
final `closed` status; each result prints a `scope:` line stating
exactly what was searched. Widen with `--assignee all`, which then
requires at least one bounding filter. Spaces and assignees resolve
by name (case-insensitive), because people think in projects and
names, not ids - and a person searching for a task nearly always
knows which project it is in, so agents are guided to ask for the
project rather than scan widely:

```sh
clickup-axi search invoice --status "in review"
clickup-axi search checkout --assignee ting --space "Webshop"
clickup-axi search migration --assignee all --updated-after 2026-05-01
clickup-axi search migration --updated-after -1week
```

## Session hook

`clickup-axi setup --global` registers a session-start hook in every
detected agent host, so each new session begins with your open tasks
already in the agent's context - no prompt, no tool call:

| Host | Config written |
|---|---|
| Claude Code | `~/.claude/settings.json` (project: `.claude/settings.json`) |
| Codex | `~/.codex/hooks.json` (project: `.codex/hooks.json`) |
| OpenCode | `~/.config/opencode/plugins/clickup-axi.js` (global only) |

The hook runs `clickup-axi context`: a dashboard of your 5 most urgent
open tasks (due-soonest first, total stated) behind a hard 5-second
budget. It is not meant to be run by hand, always exits 0, and
degrades to a one-line reason when tasks are unavailable - a broken
network can never break a session start. Rerunning `setup` repairs a
moved binary path and is otherwise a no-op; only clickup-axi's own
entries are ever touched. Uninstall with:

```sh
clickup-axi setup --global --remove
```

## Environment variables

| Variable | Effect | Default |
|---|---|---|
| `CLICKUP_TOKEN` | API token to use; takes precedence over the token stored by `auth login`. | unset - the stored token is used |
| `CLICKUP_AXI_WORKSPACE` | Pin the workspace (id from `clickup-axi`) that `tasks` and custom-id resolution operate in. Required once the token sees more than one workspace; a pin the token cannot see is an error listing the visible ones. | unset - the single visible workspace is used |
| `CLICKUP_AXI_CUSTOM_IDS` | Resolve task ids as custom ids (`HGAI-2316`) only, skipping the internal-id attempt, and display custom ids everywhere. Any value except `0` or `false` enables it. | unset - internal ids tried first, custom fallback |
| `CLICKUP_AXI_NO_UPDATE_CHECK` | Disable everything update-related: the passive daily check, the `update:` notice, and skill-copy healing. Any non-empty value. | unset - update checks enabled |

## Auto updates

`clickup-axi update` replaces the binary in place with the latest
release (checksum-verified, atomic). Commands also check for a newer
release at most once per day and append a one-line
`update: vX.Y.Z available` notice; agents relay it as a question
instead of updating on their own. Installed skill copies heal from the
binary automatically, so binary and skill never skew.
`CLICKUP_AXI_NO_UPDATE_CHECK=1` disables all of this.
