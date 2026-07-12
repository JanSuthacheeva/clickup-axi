package clickup

import "net/http"

// StatusMapping pairs a status id in the task's current list with the
// status id it should land on in the target list. The move endpoint
// matches statuses across lists by name on its own, and rejects
// mappings for statuses the target already has, so a mapping is sent
// exactly when the task's current status name is absent from the
// target list.
type StatusMapping struct {
	SourceStatus      string `json:"source_status"`
	DestinationStatus string `json:"destination_status"`
}

// MoveTask changes a task's home list via the v3 move endpoint. Only
// the home list changes: memberships in additional lists (the
// Tasks-in-Multiple-Lists ClickApp) are untouched, and subtasks move
// along with their parent. Custom-field transfer is deliberately not
// requested.
func (c *Client) MoveTask(workspaceID, taskID, listID string, mappings []StatusMapping) *APIError {
	body := map[string]any{}
	if len(mappings) > 0 {
		body["status_mappings"] = mappings
	}
	path := "/workspaces/" + workspaceID + "/tasks/" + taskID + "/home_list/" + listID
	return c.doV3(http.MethodPut, path, body, nil)
}
