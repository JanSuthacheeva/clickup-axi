package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.clickup.com/api/v2"

const errNoAuth = "not authenticated: CLICKUP_TOKEN is not set and no token is stored"

type client struct {
	base  string
	token string
	http  *http.Client
}

func newClientFromEnv() *client {
	return &client{
		base:  defaultBaseURL,
		token: resolveToken(),
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// resolveToken prefers the CLICKUP_TOKEN environment variable and falls
// back to the token stored by `clickup-axi auth login`.
func resolveToken() string {
	if t := os.Getenv("CLICKUP_TOKEN"); t != "" {
		return t
	}
	path, err := tokenFilePath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func tokenFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clickup-axi", "token"), nil
}

// apiError is a translated ClickUp API failure; raw dependency messages
// never reach stdout directly.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }

// msEpoch holds a millisecond epoch that ClickUp returns as either a JSON
// string, a number, or null.
type msEpoch string

func (m *msEpoch) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*m = msEpoch(s)
	return nil
}

func (m msEpoch) date() string {
	if m == "" {
		return ""
	}
	n, err := strconv.ParseInt(string(m), 10, 64)
	if err != nil {
		return string(m)
	}
	return time.UnixMilli(n).UTC().Format("2006-01-02")
}

type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type task struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TextContent string `json:"text_content"`
	Status      struct {
		Status string `json:"status"`
	} `json:"status"`
	Priority *struct {
		Priority string `json:"priority"`
	} `json:"priority"`
	DueDate   msEpoch `json:"due_date"`
	URL       string  `json:"url"`
	Assignees []user  `json:"assignees"`
	List      struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"list"`
}

type comment struct {
	ID   string  `json:"id"`
	Text string  `json:"comment_text"`
	User user    `json:"user"`
	Date msEpoch `json:"date"`
}

type list struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Statuses []struct {
		Status string `json:"status"`
	} `json:"statuses"`
}

type team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *client) do(method, path string, body any, out any) *apiError {
	if c.token == "" {
		return &apiError{status: 0, message: errNoAuth}
	}
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return &apiError{status: 0, message: "could not encode request body"}
		}
		reqBody = bytes.NewReader(b)
	}
	resp, apiErr := c.send(method, path, reqBody)
	if apiErr != nil {
		return apiErr
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return &apiError{status: resp.StatusCode, message: "ClickUp returned an unreadable response"}
		}
	}
	return nil
}

func (c *client) send(method, path string, body io.Reader) (*http.Response, *apiError) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(method, c.base+path, body)
		if err != nil {
			return nil, &apiError{status: 0, message: "could not build request"}
		}
		req.Header.Set("Authorization", c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, &apiError{status: 0, message: "could not reach the ClickUp API: " + err.Error()}
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 && body == nil {
			delay := 2 * time.Second
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 60 {
					delay = time.Duration(n) * time.Second
				}
			}
			resp.Body.Close()
			time.Sleep(delay)
			continue
		}
		if resp.StatusCode >= 400 {
			defer resp.Body.Close()
			return nil, translateHTTPError(resp)
		}
		return resp, nil
	}
}

func translateHTTPError(resp *http.Response) *apiError {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &apiError{status: resp.StatusCode, message: "ClickUp rejected the token (invalid or expired)"}
	case http.StatusNotFound:
		return &apiError{status: resp.StatusCode, message: "not found"}
	case http.StatusTooManyRequests:
		return &apiError{status: resp.StatusCode, message: "ClickUp rate limit hit (about 100 requests/minute); retry later"}
	}
	var body struct {
		Err   string `json:"err"`
		Ecode string `json:"ECODE"`
	}
	msg := fmt.Sprintf("ClickUp API request failed (HTTP %d)", resp.StatusCode)
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Err != "" {
		msg = fmt.Sprintf("ClickUp rejected the request: %s (HTTP %d)", body.Err, resp.StatusCode)
	}
	return &apiError{status: resp.StatusCode, message: msg}
}

func (c *client) getTask(id string) (*task, *apiError) {
	var t task
	if err := c.do(http.MethodGet, "/task/"+id, nil, &t); err != nil {
		if err.status == http.StatusNotFound {
			err.message = fmt.Sprintf("task %q not found", id)
		}
		return nil, err
	}
	return &t, nil
}

func (c *client) getComments(taskID string) ([]comment, *apiError) {
	var out struct {
		Comments []comment `json:"comments"`
	}
	if err := c.do(http.MethodGet, "/task/"+taskID+"/comment", nil, &out); err != nil {
		return nil, err
	}
	return out.Comments, nil
}

func (c *client) setTaskStatus(taskID, status string) *apiError {
	body := map[string]string{"status": status}
	return c.do(http.MethodPut, "/task/"+taskID, body, nil)
}

func (c *client) getList(id string) (*list, *apiError) {
	var l list
	if err := c.do(http.MethodGet, "/list/"+id, nil, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// teamTasksPageSize is the fixed page size of the filtered team tasks
// endpoint; a full page means more pages may exist.
const teamTasksPageSize = 100

func (c *client) getTeamTasks(teamID string, assigneeID int64) ([]task, *apiError) {
	q := url.Values{}
	q.Set("assignees[]", strconv.FormatInt(assigneeID, 10))
	q.Set("subtasks", "true")
	var out struct {
		Tasks []task `json:"tasks"`
	}
	if err := c.do(http.MethodGet, "/team/"+teamID+"/task?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

func (c *client) getUser() (*user, *apiError) {
	var out struct {
		User user `json:"user"`
	}
	if err := c.do(http.MethodGet, "/user", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

func (c *client) getTeams() ([]team, *apiError) {
	var out struct {
		Teams []team `json:"teams"`
	}
	if err := c.do(http.MethodGet, "/team", nil, &out); err != nil {
		return nil, err
	}
	return out.Teams, nil
}
