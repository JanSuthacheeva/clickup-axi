package clickup

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTeamsClient serves the given /team payload and isolates the
// workspace pin from the host environment; tests that want a pin set
// the variable after calling this.
func newTeamsClient(t *testing.T, teamsJSON string) *Client {
	t.Helper()
	t.Setenv(WorkspaceEnv, "")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/team", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(teamsJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL+"/api/v2", "pk_test", &http.Client{Timeout: 5 * time.Second})
}

const twoTeamsJSON = `{"teams": [{"id": "9001", "name": "BUZZWOO"}, {"id": "9002", "name": "Personal"}]}`

func TestSelectTeamSingleWorkspaceNeedsNoPin(t *testing.T) {
	c := newTeamsClient(t, `{"teams": [{"id": "9018", "name": "Buzzwoo"}]}`)
	team, err := c.SelectTeam()
	if err != nil {
		t.Fatalf("SelectTeam() error = %q, want nil", err.Message)
	}
	if team.ID != "9018" || team.Name != "Buzzwoo" {
		t.Errorf("SelectTeam() = %+v, want id 9018 name Buzzwoo", team)
	}
}

func TestSelectTeamZeroWorkspacesIsExplicit(t *testing.T) {
	c := newTeamsClient(t, `{"teams": []}`)
	_, err := c.SelectTeam()
	if err == nil {
		t.Fatal("SelectTeam() error = nil, want error")
	}
	if want := "no workspaces are visible to this token"; err.Message != want {
		t.Errorf("SelectTeam() error = %q, want %q", err.Message, want)
	}
}

func TestSelectTeamMultipleWorkspacesListThePins(t *testing.T) {
	c := newTeamsClient(t, twoTeamsJSON)
	_, err := c.SelectTeam()
	if err == nil {
		t.Fatal("SelectTeam() error = nil, want error")
	}
	want := `2 workspaces are visible; set CLICKUP_AXI_WORKSPACE to one of: 9001 "BUZZWOO", 9002 "Personal"`
	if err.Message != want {
		t.Errorf("SelectTeam() error = %q, want %q", err.Message, want)
	}
}

func TestSelectTeamPinPicksAmongMultiple(t *testing.T) {
	c := newTeamsClient(t, twoTeamsJSON)
	t.Setenv(WorkspaceEnv, "9002")
	team, err := c.SelectTeam()
	if err != nil {
		t.Fatalf("SelectTeam() error = %q, want nil", err.Message)
	}
	if team.ID != "9002" || team.Name != "Personal" {
		t.Errorf("SelectTeam() = %+v, want id 9002 name Personal", team)
	}
}

func TestSelectTeamPinIsTrimmed(t *testing.T) {
	c := newTeamsClient(t, twoTeamsJSON)
	t.Setenv(WorkspaceEnv, " 9001 ")
	team, err := c.SelectTeam()
	if err != nil {
		t.Fatalf("SelectTeam() error = %q, want nil", err.Message)
	}
	if team.ID != "9001" {
		t.Errorf("SelectTeam().ID = %q, want 9001", team.ID)
	}
}

func TestSelectTeamInvisiblePinListsVisibleWorkspaces(t *testing.T) {
	c := newTeamsClient(t, twoTeamsJSON)
	t.Setenv(WorkspaceEnv, "1234")
	_, err := c.SelectTeam()
	if err == nil {
		t.Fatal("SelectTeam() error = nil, want error")
	}
	want := `CLICKUP_AXI_WORKSPACE="1234" does not match any workspace visible to this token (visible: 9001 "BUZZWOO", 9002 "Personal")`
	if err.Message != want {
		t.Errorf("SelectTeam() error = %q, want %q", err.Message, want)
	}
}
