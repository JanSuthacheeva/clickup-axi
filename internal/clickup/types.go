package clickup

import (
	"strconv"
	"strings"
	"time"
)

// MsEpoch holds a millisecond epoch that ClickUp returns as either a JSON
// string, a number, or null.
type MsEpoch string

func (m *MsEpoch) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*m = MsEpoch(s)
	return nil
}

func (m MsEpoch) Date() string {
	if m == "" {
		return ""
	}
	n, err := strconv.ParseInt(string(m), 10, 64)
	if err != nil {
		return string(m)
	}
	return time.UnixMilli(n).UTC().Format("2006-01-02")
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Task struct {
	ID          string `json:"id"`
	CustomID    string `json:"custom_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TextContent string `json:"text_content"`
	Status      struct {
		Status string `json:"status"`
	} `json:"status"`
	Priority *struct {
		Priority string `json:"priority"`
	} `json:"priority"`
	DueDate   MsEpoch `json:"due_date"`
	URL       string  `json:"url"`
	Assignees []User  `json:"assignees"`
	List      struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"list"`
}

type Comment struct {
	ID   string  `json:"id"`
	Text string  `json:"comment_text"`
	User User    `json:"user"`
	Date MsEpoch `json:"date"`
}

type List struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Statuses []struct {
		Status string `json:"status"`
	} `json:"statuses"`
}

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TeamTasksPageSize is the fixed page size of the filtered team tasks
// endpoint; a full page means more pages may exist.
const TeamTasksPageSize = 100

// CommentsPageSize is the most comments the comments endpoint returns
// without pagination.
const CommentsPageSize = 25
