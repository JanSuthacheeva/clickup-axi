package clickup

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// taskRef addresses a task by internal id (86ey3tx8m) or custom id
// (HGAI-2316); custom ids need the workspace passed along.
type taskRef struct {
	id     string
	custom bool
	teamID string
}

// CustomIDsForced reports whether CLICKUP_AXI_CUSTOM_IDS opts this
// workspace into custom-id-only resolution, skipping the internal-id
// attempt entirely.
func CustomIDsForced() bool {
	switch os.Getenv("CLICKUP_AXI_CUSTOM_IDS") {
	case "", "0", "false":
		return false
	}
	return true
}

// GetTaskByID resolves a user-supplied id. With CLICKUP_AXI_CUSTOM_IDS
// set it goes straight to custom-id resolution; otherwise it tries the
// id as an internal one first and falls back to custom when ClickUp
// does not know it (404, or 401 which ClickUp also returns for ids
// outside the token's scope).
func (c *Client) GetTaskByID(id string) (*Task, *APIError) {
	if CustomIDsForced() {
		return c.getTaskByCustomID(id)
	}
	t, err := c.getTask(taskRef{id: id})
	if err == nil {
		return t, nil
	}
	if err.Status != http.StatusNotFound && err.Status != http.StatusUnauthorized {
		return nil, err
	}
	t, customErr := c.getTaskByCustomID(id)
	if customErr == nil {
		return t, nil
	}
	if customErr.Status == http.StatusNotFound {
		return nil, &APIError{Status: http.StatusNotFound,
			Message: fmt.Sprintf("task %q not found (tried as internal and as custom id)", id)}
	}
	return nil, customErr
}

// getTaskByCustomID resolves ids like HGAI-2316, which ClickUp stores
// uppercase and only matches with the workspace id attached.
func (c *Client) getTaskByCustomID(id string) (*Task, *APIError) {
	team, err := c.SelectTeam()
	if err != nil {
		return nil, err
	}
	return c.getTask(taskRef{id: strings.ToUpper(id), custom: true, teamID: team.ID})
}

func (c *Client) getTask(ref taskRef) (*Task, *APIError) {
	path := "/task/" + url.PathEscape(ref.id)
	if ref.custom {
		q := url.Values{}
		q.Set("custom_task_ids", "true")
		q.Set("team_id", ref.teamID)
		path += "?" + q.Encode()
	}
	var t Task
	if err := c.do(http.MethodGet, path, nil, &t); err != nil {
		// ClickUp answers 401 (not 404) for custom ids outside the
		// token's scope; the token itself was just proven valid by the
		// GetTeams call, so report the task, not the auth.
		if err.Status == http.StatusNotFound || (ref.custom && err.Status == http.StatusUnauthorized) {
			err.Message = fmt.Sprintf("task %q not found", ref.id)
		}
		return nil, err
	}
	return &t, nil
}
