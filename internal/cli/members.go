package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/output"
)

const membersHelp = `clickup-axi members

List the members of the selected workspace: the people tasks can be
assigned to. The members come along with the workspace fetch, so this
costs a single request. With more than one workspace visible, set
CLICKUP_AXI_WORKSPACE=<id> first; run ` + "`clickup-axi`" + ` to list the ids.

examples:
  clickup-axi members
  clickup-axi tasks --assignee "Ting Nguyen"`

// cmdMembers renders the workspace's members from the team already
// fetched by SelectTeam - the same data the assignee resolvers match
// against, made discoverable without a deliberately failed --assignee.
func cmdMembers(args []string, c *clickup.Client, out io.Writer) int {
	for _, arg := range args {
		switch arg {
		case "--help", "-h":
			fmt.Fprintln(out, membersHelp)
			return 0
		default:
			kind := "argument"
			if strings.HasPrefix(arg, "-") {
				kind = "flag"
			}
			output.WriteError(out, fmt.Sprintf("unknown %s %q for members\n  valid flags: --help", kind, arg),
				"Run `clickup-axi members`")
			return 2
		}
	}

	team, err := c.SelectTeam()
	if err != nil {
		return renderAPIError(out, err)
	}
	users := make([]clickup.User, len(team.Members))
	for i, m := range team.Members {
		users[i] = m.User
	}
	sort.Slice(users, func(i, j int) bool {
		left, right := strings.ToLower(users[i].Username), strings.ToLower(users[j].Username)
		if left != right {
			return left < right
		}
		return users[i].ID < users[j].ID
	})

	workspace := fmt.Sprintf("workspace %s %q", team.ID, team.Name)
	if len(users) == 0 {
		fmt.Fprintf(out, "members: 0 members in %s\n", workspace)
		output.WriteHelp(out, "Run `clickup-axi tasks` to see your open tasks")
		return 0
	}
	fmt.Fprintf(out, "count: %d member%s in %s\n", len(users), pluralS(len(users)), workspace)
	fmt.Fprintf(out, "members[%d]{id,name,email}:\n", len(users))
	for _, u := range users {
		fmt.Fprintf(out, "  %d,%s,%s\n", u.ID, output.ToonCell(u.Username), output.ToonCell(u.Email))
	}
	output.WriteHelp(out, "Run `clickup-axi tasks --assignee \"<name|id>\"` for a member's open tasks")
	return 0
}
