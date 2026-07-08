package clickup

import (
	"fmt"
	"os"
	"strings"
)

// WorkspaceEnv pins the workspace (ClickUp team) that workspace-scoped
// calls operate in. It becomes required once more than one workspace is
// visible to the token; with a single workspace it is optional.
const WorkspaceEnv = "CLICKUP_AXI_WORKSPACE"

// WorkspaceIDFromEnv returns the workspace id pinned by
// CLICKUP_AXI_WORKSPACE, or "" when unset.
func WorkspaceIDFromEnv() string {
	return strings.TrimSpace(os.Getenv(WorkspaceEnv))
}

// SelectTeam picks the workspace to operate in: the one pinned by
// CLICKUP_AXI_WORKSPACE when set (validated against the teams visible
// to the token), otherwise the single visible team. Every failure
// inlines the visible id,name pairs so the agent can recover in one
// retry without a follow-up call.
func (c *Client) SelectTeam() (*Team, *APIError) {
	teams, err := c.GetTeams()
	if err != nil {
		return nil, err
	}
	if want := WorkspaceIDFromEnv(); want != "" {
		for i := range teams {
			if teams[i].ID == want {
				return &teams[i], nil
			}
		}
		return nil, &APIError{Message: fmt.Sprintf(
			"%s=%q does not match any workspace visible to this token (visible: %s)",
			WorkspaceEnv, want, workspaceList(teams))}
	}
	switch len(teams) {
	case 0:
		return nil, &APIError{Message: "no workspaces are visible to this token"}
	case 1:
		return &teams[0], nil
	}
	return nil, &APIError{Message: fmt.Sprintf(
		"%d workspaces are visible; set %s to one of: %s",
		len(teams), WorkspaceEnv, workspaceList(teams))}
}

// workspaceList renders teams as `9001 "BUZZWOO", 9002 "Personal"` for
// inlining into error messages.
func workspaceList(teams []Team) string {
	if len(teams) == 0 {
		return "none"
	}
	parts := make([]string, len(teams))
	for i, t := range teams {
		parts[i] = fmt.Sprintf("%s %q", t.ID, t.Name)
	}
	return strings.Join(parts, ", ")
}
