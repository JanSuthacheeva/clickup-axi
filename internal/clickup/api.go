package clickup

import (
	"net/http"
	"net/url"
	"strconv"
)

func (c *Client) GetComments(taskID string) ([]Comment, *APIError) {
	var out struct {
		Comments []Comment `json:"comments"`
	}
	if err := c.do(http.MethodGet, "/task/"+taskID+"/comment", nil, &out); err != nil {
		return nil, err
	}
	return out.Comments, nil
}

func (c *Client) CreateComment(taskID, text string) *APIError {
	body := map[string]string{"comment_text": text}
	return c.do(http.MethodPost, "/task/"+taskID+"/comment", body, nil)
}

// TaskEdit is a mutation of a task's fields. Zero values leave a field
// unchanged: "" for Status/Name, nil for Priority/DueDate and the
// assignee lists. Priority 0 and DueDate 0 clear their field (JSON
// null). It maps to a single PUT /task/{id} so all field changes
// commit atomically in one request.
type TaskEdit struct {
	Status       string
	Name         string
	Priority     *int    // nil = unchanged, 0 = clear, 1 (urgent) .. 4 (low)
	DueDate      *int64  // nil = unchanged, 0 = clear, else millisecond epoch
	Body         *string // nil = unchanged, else full markdown replacement
	AddAssignees []int64
	RemAssignees []int64
}

func (c *Client) UpdateTask(taskID string, edit TaskEdit) *APIError {
	body := map[string]any{}
	if edit.Status != "" {
		body["status"] = edit.Status
	}
	if edit.Name != "" {
		body["name"] = edit.Name
	}
	if edit.Priority != nil {
		if *edit.Priority == 0 {
			body["priority"] = nil
		} else {
			body["priority"] = *edit.Priority
		}
	}
	if edit.DueDate != nil {
		if *edit.DueDate == 0 {
			body["due_date"] = nil
		} else {
			body["due_date"] = *edit.DueDate
			// Date-only: the CLI takes and renders dates, not times.
			body["due_date_time"] = false
		}
	}
	if edit.Body != nil {
		body["markdown_content"] = *edit.Body
	}
	if len(edit.AddAssignees) > 0 || len(edit.RemAssignees) > 0 {
		add := edit.AddAssignees
		if add == nil {
			add = []int64{}
		}
		rem := edit.RemAssignees
		if rem == nil {
			rem = []int64{}
		}
		body["assignees"] = map[string][]int64{"add": add, "rem": rem}
	}
	return c.do(http.MethodPut, "/task/"+taskID, body, nil)
}

func (c *Client) GetList(id string) (*List, *APIError) {
	var l List
	if err := c.do(http.MethodGet, "/list/"+id, nil, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// TaskQuery are the server-side filters the filtered-team-tasks endpoint
// applies before a page is returned. Every field the ClickUp API can
// filter on is pushed down here so the candidate set is as small as
// possible before any client-side work. The zero value fetches page 0
// unfiltered.
type TaskQuery struct {
	Assignees     []int64
	Statuses      []string
	SpaceIDs      []string
	ListIDs       []string
	IncludeClosed bool
	// DateUpdatedGt/Lt bound the last-updated time (millisecond epoch,
	// 0 = unset), letting a search target a time window.
	DateUpdatedGt int64
	DateUpdatedLt int64
	// OrderBy names the sort field (e.g. "updated"). A search orders by
	// "updated" so a bounded scan covers the most recently active tasks:
	// the endpoint sorts each order_by field descending by default, so
	// leaving Reverse false yields newest-first.
	OrderBy string
	Reverse bool
	Page    int
}

// GetTeamTasksPage fetches one page (up to TeamTasksPageSize tasks) of
// the team's tasks matching q. lastPage reports whether this was the
// final page, so a caller paging through results can stop without an
// extra request: the endpoint's own last_page flag is honored when
// present, otherwise a short page is the signal.
func (c *Client) GetTeamTasksPage(teamID string, q TaskQuery) (tasks []Task, lastPage bool, err *APIError) {
	v := url.Values{}
	for _, a := range q.Assignees {
		v.Add("assignees[]", strconv.FormatInt(a, 10))
	}
	for _, s := range q.Statuses {
		v.Add("statuses[]", s)
	}
	for _, s := range q.SpaceIDs {
		v.Add("space_ids[]", s)
	}
	for _, l := range q.ListIDs {
		v.Add("list_ids[]", l)
	}
	if q.IncludeClosed {
		v.Set("include_closed", "true")
	}
	if q.DateUpdatedGt > 0 {
		v.Set("date_updated_gt", strconv.FormatInt(q.DateUpdatedGt, 10))
	}
	if q.DateUpdatedLt > 0 {
		v.Set("date_updated_lt", strconv.FormatInt(q.DateUpdatedLt, 10))
	}
	if q.OrderBy != "" {
		v.Set("order_by", q.OrderBy)
	}
	if q.Reverse {
		v.Set("reverse", "true")
	}
	v.Set("subtasks", "true")
	v.Set("page", strconv.Itoa(q.Page))

	var out struct {
		Tasks    []Task `json:"tasks"`
		LastPage *bool  `json:"last_page"`
	}
	if e := c.do(http.MethodGet, "/team/"+teamID+"/task?"+v.Encode(), nil, &out); e != nil {
		return nil, false, e
	}
	last := len(out.Tasks) < TeamTasksPageSize
	if out.LastPage != nil {
		last = *out.LastPage
	}
	return out.Tasks, last, nil
}

func (c *Client) GetUser() (*User, *APIError) {
	var out struct {
		User User `json:"user"`
	}
	if err := c.do(http.MethodGet, "/user", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

func (c *Client) GetTeams() ([]Team, *APIError) {
	var out struct {
		Teams []Team `json:"teams"`
	}
	if err := c.do(http.MethodGet, "/team", nil, &out); err != nil {
		return nil, err
	}
	return out.Teams, nil
}
