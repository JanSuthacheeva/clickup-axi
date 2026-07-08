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

func (c *Client) GetList(id string) (*List, *APIError) {
	var l List
	if err := c.do(http.MethodGet, "/list/"+id, nil, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (c *Client) GetTeamTasks(teamID string, assigneeID int64) ([]Task, *APIError) {
	q := url.Values{}
	q.Set("assignees[]", strconv.FormatInt(assigneeID, 10))
	q.Set("subtasks", "true")
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	if err := c.do(http.MethodGet, "/team/"+teamID+"/task?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
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
