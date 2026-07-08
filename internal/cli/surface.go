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
		summary: "List your open tasks (assigned to you)",
		skill:   "clickup-axi tasks",
		comment: "open tasks assigned to the user",
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
		comment: "complete description and all fetched comments",
	},
	{
		usage:   `search "<query>"`,
		summary: "Find your tasks by words in the title or description",
		skill:   `clickup-axi search "<query>"`,
		comment: "find YOUR tasks by words in the title or description",
	},
	{
		skill:   `clickup-axi search "<query>" --assignee all --status "<status>"`,
		comment: "widen past your own tasks (needs a filter; also --space/--list/--updated-after)",
	},
	{
		usage:   "tasks edit <id>",
		summary: `Change a task's status (--status "<status>")`,
		skill:   `clickup-axi tasks edit <id> --status "<status>"`,
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
