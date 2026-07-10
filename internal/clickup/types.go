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

// Date renders the epoch as a local calendar date. ClickUp anchors
// date-only due dates at 04:00 in the workspace's timezone, so a UTC
// rendering shifts the date back by one everywhere east of Greenwich;
// the machine's local zone is the best available proxy for the
// workspace's.
func (m MsEpoch) Date() string {
	if m == "" {
		return ""
	}
	n, err := strconv.ParseInt(string(m), 10, 64)
	if err != nil {
		return string(m)
	}
	return time.UnixMilli(n).Local().Format("2006-01-02")
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type Task struct {
	ID          string `json:"id"`
	CustomID    string `json:"custom_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TextContent string `json:"text_content"`
	// MarkdownDescription is the markdown source of the description,
	// present because getTask always requests it; edits that append to
	// the body build on it.
	MarkdownDescription string `json:"markdown_description"`
	Status              struct {
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
	Tags  []Tag `json:"tags"`
	Space struct {
		ID string `json:"id"`
	} `json:"space"`
}

type Comment struct {
	ID   string  `json:"id"`
	Text string  `json:"comment_text"`
	User User    `json:"user"`
	Date MsEpoch `json:"date"`
}

// Tag is a task tag; only the name matters to the CLI.
type Tag struct {
	Name string `json:"name"`
}

type List struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Statuses []struct {
		Status string `json:"status"`
	} `json:"statuses"`
}

type Team struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Members []TeamMember `json:"members"`
}

// TeamMember wraps the member entries of the GET /team response; the
// workspace's people come along with the team fetch, so member-name
// resolution never needs an extra request.
type TeamMember struct {
	User User `json:"user"`
}

// TeamTasksPageSize is the fixed page size of the filtered team tasks
// endpoint; a full page means more pages may exist.
const TeamTasksPageSize = 100

// CommentsPageSize is the most comments the comments endpoint returns
// without pagination.
const CommentsPageSize = 25
