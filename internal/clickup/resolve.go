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
	id              string
	custom          bool
	teamID          string
	includeSubtasks bool
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
	return c.getTaskByID(id, false)
}

// GetTaskWithSubtasksByID resolves a user-supplied id like GetTaskByID,
// while asking ClickUp to include the task's children. It is deliberately
// separate so mutation and validation lookups do not pay for relationship
// data they never render.
func (c *Client) GetTaskWithSubtasksByID(id string) (*Task, *APIError) {
	return c.getTaskByID(id, true)
}

func (c *Client) getTaskByID(id string, includeSubtasks bool) (*Task, *APIError) {
	c.DateLocation()
	if CustomIDsForced() {
		return c.getTaskByCustomID(id, includeSubtasks)
	}
	t, err := c.getTask(taskRef{id: id, includeSubtasks: includeSubtasks})
	if err == nil {
		return t, nil
	}
	if err.Status != http.StatusNotFound && err.Status != http.StatusUnauthorized {
		return nil, err
	}
	t, customErr := c.getTaskByCustomID(id, includeSubtasks)
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
func (c *Client) getTaskByCustomID(id string, includeSubtasks bool) (*Task, *APIError) {
	team, err := c.SelectTeam()
	if err != nil {
		return nil, err
	}
	return c.getTask(taskRef{id: strings.ToUpper(id), custom: true, teamID: team.ID, includeSubtasks: includeSubtasks})
}

// maxParentHops bounds the ancestor walk in ParentWouldCycle. ClickUp
// nests subtasks only a handful of levels deep; the cap is a safety net
// against an unexpected or malformed chain, not a real limit.
const maxParentHops = 32

// ParentWouldCycle reports whether making proposedParent the parent of
// the task identified by internalID would create a cycle - that is,
// proposedParent already sits below that task in the subtask tree. It
// walks proposedParent's ancestor chain, which is addressed by internal
// id (Task.Parent is always internal), one GET per hop, looking for
// internalID. The direct case (proposedParent IS the task) is caught by
// the caller before this runs.
func (c *Client) ParentWouldCycle(internalID string, proposedParent *Task) (bool, *APIError) {
	for cur, hops := proposedParent, 0; cur.Parent != "" && hops < maxParentHops; hops++ {
		if cur.Parent == internalID {
			return true, nil
		}
		next, err := c.getTask(taskRef{id: cur.Parent})
		if err != nil {
			return false, err
		}
		cur = next
	}
	return false, nil
}

func (c *Client) getTask(ref taskRef) (*Task, *APIError) {
	q := url.Values{}
	// The markdown source of the description only comes along on
	// request; edits that append to the body need it.
	q.Set("include_markdown_description", "true")
	if ref.includeSubtasks {
		q.Set("include_subtasks", "true")
	}
	if ref.custom {
		q.Set("custom_task_ids", "true")
		q.Set("team_id", ref.teamID)
	}
	path := "/task/" + url.PathEscape(ref.id) + "?" + q.Encode()
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
