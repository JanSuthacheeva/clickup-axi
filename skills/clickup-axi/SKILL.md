---
name: clickup-axi
description: >
  Manage ClickUp tasks via the clickup-axi CLI - list tasks assigned to
  the user, view a task with its comments and description by id, and
  change a task's status. Use when the user mentions ClickUp, sprint
  tasks, tickets with ids like HGAI-2316 or ECOM-2254, asks what is on
  their plate, or wants a task looked up, summarized, or moved to
  another status.
---

# clickup-axi

An agent-ergonomic ClickUp CLI (AXI). Run it directly; every output ends
with `help[]` next-step suggestions, so follow those rather than
guessing. `clickup-axi tasks --help` exists as a fallback.

## Commands

```sh
clickup-axi                                # who am I + workspaces (auth check)
clickup-axi tasks                          # open tasks assigned to the user
clickup-axi tasks <id>                     # one task: metadata, description, newest comments
clickup-axi tasks <id> --full              # complete description and all fetched comments
clickup-axi tasks edit <id> --status "<status>"
```

Task ids may be custom (HGAI-2316, case-insensitive) or internal
(86ey3tx8m). An invalid status fails with the list's valid statuses
echoed inline - pick one and retry once.

## Behavior contract

- stdout is structured (TOON-style); stderr is diagnostics only
- exit codes: 0 success (including no-op mutations), 1 error, 2 usage error
- zero results are stated explicitly; absence of results is the answer,
  do not retry with different flags
- long descriptions are truncated with a size hint; use `--full` only
  when the truncated preview is insufficient

## Auth

Errors mentioning authentication mean no token is stored. Do NOT ask
the user to paste their token into the conversation. Ask them to run
`clickup-axi auth login` in their own terminal (it guides them and the
paste stays hidden), then retry.
