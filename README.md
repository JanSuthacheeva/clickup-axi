# clickup-axi

A minimal ClickUp CLI for AI agents, following the [AXI](https://axi.md) design
principles: token-efficient output, combined operations, structured errors, and
contextual next-step hints.

Deliberately small for now - it covers the two flows agents need most:

```sh
# a task by ID, with its newest comments inline (one invocation, two API calls)
clickup-axi task view 86c2x1a

# change a task's status; an invalid status echoes the list's valid ones
clickup-axi task edit 86c2x1a --status "in review"
```

## Setup

```sh
go build -o clickup-axi .
echo -n pk_... | clickup-axi auth login   # token: ClickUp Settings -> Apps
```

`auth login` reads the token from stdin only (never argv), validates it
against the API, and stores it at `~/.config/clickup-axi/token` (mode 600).
`auth logout` removes it and is a no-op when already logged out. A
`CLICKUP_TOKEN` environment variable, when set, takes precedence over the
stored token.

Running `clickup-axi` with no arguments shows live state (user, workspaces) instead
of help text. `clickup-axi task --help` has flags and examples.

## Behavior contract (AXI)

- stdout carries structured data and errors; stderr is for diagnostics only
- exit codes: 0 success (including idempotent no-ops), 1 error, 2 usage error
- long descriptions are truncated with a total size hint; `--full` lifts it
- zero results are stated explicitly, never silent
- no interactive prompts; auth is env var or stdin-piped login only

## Tests

```sh
go test ./...
```
