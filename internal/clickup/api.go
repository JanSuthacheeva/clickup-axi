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

func (c *Client) SetTaskStatus(taskID, status string) *APIError {
	body := map[string]string{"status": status}
	return c.do(http.MethodPut, "/task/"+taskID, body, nil)
}

func (c *Client) UpdateTaskAssignees(taskID string, add, rem []int64) *APIError {
	if add == nil {
		add = []int64{}
	}
	if rem == nil {
		rem = []int64{}
	}
	body := map[string]any{"assignees": map[string][]int64{"add": add, "rem": rem}}
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
