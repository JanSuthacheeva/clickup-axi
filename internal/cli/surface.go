package cli

// command is one entry of the CLI's public surface. The table below is
// the single source of truth for the top-level help and the Commands
// section of the generated agent skill (skills/clickup-axi/SKILL.md):
// adding a command here updates both, and TestCommittedSkillIsFresh plus
// `clickup-axi skill --check` fail until the skill is regenerated.
type command struct {
	usage   string // top-level help: usage column ("" = not listed there)
	summary string // top-level help: one-line summary
	note    string // top-level help: extra continuation line under the summary
	skill   string // generated skill: full command line ("" = maintainer-only)
	comment string // generated skill: trailing # comment
}

var surface = []command{
	{
		skill:   "clickup-axi",
		comment: "who am I + workspaces (auth check)",
	},
	{
		usage:   "tasks",
		summary: "List open tasks (yours by default)",
		note:    "(--assignee <who> for a teammate, --space to narrow, --page N for more, --fields for extra columns)",
		skill:   "clickup-axi tasks",
		comment: "open tasks assigned to the user",
	},
	{
		skill:   `clickup-axi tasks --assignee "<who>" --space "<space>"`,
		comment: "a teammate's open tasks; names resolve case-insensitively",
	},
	{
		skill:   "clickup-axi tasks --fields assignees,priority",
		comment: "extra columns on tasks and search listings: assignees, priority, tags, list, url",
	},
	{
		usage:   "tasks <id>",
		summary: "Show one task with its newest comments",
		note:    "(internal id like 86ey3tx8m or custom like HGAI-2316)",
		skill:   "clickup-axi tasks <id>",
		comment: "one task: metadata, description, newest comments",
	},
	{
		skill:   "clickup-axi tasks <id> --full",
		comment: "complete description and all fetched comments; --fields url adds the task URL",
	},
	{
		usage:   `search "<query>"`,
		summary: "Find your tasks by words in the title or description",
		skill:   `clickup-axi search "<query>"`,
		comment: "find YOUR tasks by words in the title or description",
	},
	{
		usage:   "spaces",
		summary: "List active spaces (projects) in the workspace",
		skill:   "clickup-axi spaces",
		comment: "active spaces (projects) available in the workspace",
	},
	{
		usage:   "lists --space <name|id>",
		summary: "List active Lists in one space",
		note:    "(--archived shows archived Lists; folder context is included)",
		skill:   `clickup-axi lists --space "<space>"`,
		comment: "Lists in one space, including folder context; names resolve case-insensitively",
	},
	{
		skill:   `clickup-axi lists --space "<space>" --archived`,
		comment: "archived Lists in the selected space",
	},
	{
		skill:   `clickup-axi search "<query>" --assignee all --space "<space>"`,
		comment: "widen beyond your tasks; space and assignee resolve by name",
	},
	{
		skill:   `clickup-axi search "<query>" --updated-after -1week`,
		comment: "date bounds accept YYYY-MM-DD or signed day/week offsets",
	},
	{
		usage:   `tasks create "<name>"`,
		summary: "Create a task in a list (--list <name|id>)",
		note:    "(--space scopes a list name; --parent makes a subtask; --status/--assignee/--priority/--due/--body/--tag set fields)",
		skill:   `clickup-axi tasks create "<name>" --list "<list>" --space "<space>"`,
		comment: "create a task; a list name needs --space, a numeric list id works alone",
	},
	{
		skill:   `clickup-axi tasks create "<name>" --list <id> --assignee me --due <date>`,
		comment: `due: YYYY-MM-DD or +3days/-1week; --status, --priority, --body "<markdown>", --tag too`,
	},
	{
		skill:   `clickup-axi tasks create "<name>" --parent <task id>`,
		comment: "create a subtask; the list comes from the parent, no --list needed",
	},
	{
		usage:   "tasks edit <id>",
		summary: "Change status, assignees, priority, name, due date, description, tags",
		skill:   `clickup-axi tasks edit <id> --status "<status>"`,
	},
	{
		skill:   `clickup-axi tasks edit <id> --assignee <who> --unassign <who>`,
		comment: "reassign; --assignee/--unassign are repeatable and comma-separated; who = me | name | id",
	},
	{
		skill:   `clickup-axi tasks edit <id> --priority <p> --due <date> --name "<title>"`,
		comment: "priority: urgent|high|normal|low|none; due: YYYY-MM-DD, +3days/-1week, or none; fields combine in one call",
	},
	{
		skill:   `clickup-axi tasks edit <id> --append-body "<markdown>" --add-tag <tag>`,
		comment: "--body replaces the description, --append-body adds below it; tags must already exist in the space",
	},
	{
		usage:   "tasks comment <id>",
		summary: `Add a comment to a task (--text "<text>")`,
		skill:   `clickup-axi tasks comment <id> --text "<text>"`,
	},
	{
		usage:   "auth login",
		summary: "Store a personal API token (read from stdin)",
	},
	{
		usage:   "auth logout",
		summary: "Remove the stored token",
	},
	{
		usage:   "setup",
		summary: "Install the session-start hook (Claude Code, Codex, OpenCode)",
		note:    "(--global or --project; --remove uninstalls)",
		skill:   "clickup-axi setup --global",
		comment: "install the session-start dashboard hook (only after user consent)",
	},
	{
		usage:   "context",
		summary: "Session-start dashboard printed by the installed hook",
	},
	{
		usage:   "update",
		summary: "Update the binary to the latest release",
		skill:   "clickup-axi update",
		comment: "self-update to the latest release (only after user consent)",
	},
	{
		usage:   "skill",
		summary: "Generate or verify the agent skill (maintainer command)",
	},
}
