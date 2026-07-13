package cli

import (
	"strings"
	"testing"
)

func TestMembersListsWorkspaceMembers(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", `{"teams": [{"id": "9018", "name": "Buzzwoo", "members": [
		{"user": {"id": 189, "username": "Ting Nguyen", "email": "ting@buzzwoo.de"}},
		{"user": {"id": 42, "username": "jan", "email": "jan@buzzwoo.de"}},
		{"user": {"id": 205, "username": "", "email": "bot@buzzwoo.de"}}
	]}]}`)

	out, code := runCLI(t, c, "members")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `count: 3 members in workspace 9018 "Buzzwoo"`) {
		t.Errorf("missing count line\noutput:\n%s", out)
	}
	if !strings.Contains(out, "members[3]{id,name,email}:") {
		t.Errorf("missing array header\noutput:\n%s", out)
	}
	// Name-sorted (case-insensitive), an empty name first; a member
	// without a username still shows an id and email to act on.
	want := "members[3]{id,name,email}:\n" +
		"  205,,bot@buzzwoo.de\n" +
		"  42,jan,jan@buzzwoo.de\n" +
		"  189,Ting Nguyen,ting@buzzwoo.de\n"
	if !strings.Contains(out, want) {
		t.Errorf("rows wrong or unsorted\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Run `clickup-axi tasks --assignee \"<name|id>\"` for a member's open tasks") {
		t.Errorf("missing next-step hint\noutput:\n%s", out)
	}
}

func TestMembersZeroIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", `{"teams": [{"id": "9018", "name": "Buzzwoo"}]}`)
	out, code := runCLI(t, c, "members")
	if code != 0 || !strings.Contains(out, `members: 0 members in workspace 9018 "Buzzwoo"`) {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestMembersRejectsUnknownFlags(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "members", "--space")
	if code != 2 || !strings.Contains(out, "unknown flag") {
		t.Errorf("exit %d output:\n%s", code, out)
	}
}

func TestMembersUnpinnedMultiWorkspaceErrors(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.meWithTeams(t, 42, "jan", twoTeamsJSON)
	out, code := runCLI(t, c, "members")
	if code != 1 || !strings.Contains(out, "9001") || !strings.Contains(out, "9002") {
		t.Errorf("workspace-pin error missing\nexit %d output:\n%s", code, out)
	}
}
