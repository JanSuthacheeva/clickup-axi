package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestSpacesRendersSortedInventory(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.spaces(t, "9018", `{"spaces": [
		{"id": "30", "name": "Zoo"},
		{"id": "20", "name": "alpha"},
		{"id": "10", "name": "Alpha"}
	]}`)

	out, code := runCLI(t, c, "spaces")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := "spaces: 3 active spaces in workspace 9018 \"Buzzwoo\"\n" +
		"spaces[3]{id,name}:\n" +
		"  10,Alpha\n" +
		"  20,alpha\n" +
		"  30,Zoo\n" +
		"help[1]: Run `clickup-axi lists --space \"<name|id>\"` to list a space's lists\n"
	if out != want {
		t.Errorf("spaces output =\n%s\nwant:\n%s", out, want)
	}
}

func TestSpacesEmptyStateIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.spaces(t, "9018", `{"spaces": []}`)

	out, code := runCLI(t, c, "spaces")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := "spaces: 0 active spaces in workspace 9018 \"Buzzwoo\"\n" +
		"help[1]: Run `clickup-axi tasks` to see your open tasks\n"
	if out != want {
		t.Errorf("spaces output =\n%s\nwant:\n%s", out, want)
	}
}

func TestListsRendersActiveFolderlessAndFolderListsInOrder(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.spaces(t, "9018", `{"spaces": [{"id": "90121", "name": "Webshop"}]}`)
	f.mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("archived"); got != "false" {
			t.Errorf("folderless archived = %q, want false", got)
		}
		w.Write([]byte(`{"lists": [{"id": "30", "name": "Zoo"}, {"id": "10", "name": "alpha"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("archived"); got != "false" {
			t.Errorf("folders archived = %q, want false", got)
		}
		w.Write([]byte(`{"folders": [{"id": "f2", "name": "B Folder"}, {"id": "f1", "name": "A Folder"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/folder/f1/list", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("archived"); got != "false" {
			t.Errorf("f1 archived = %q, want false", got)
		}
		w.Write([]byte(`{"lists": [{"id": "21", "name": "Next"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/folder/f2/list", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("archived"); got != "false" {
			t.Errorf("f2 archived = %q, want false", got)
		}
		w.Write([]byte(`{"lists": [{"id": "22", "name": "Backlog"}]}`))
	})

	out, code := runCLI(t, c, "lists", "--space", "webshop")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := "lists: 4 active lists in space 90121 \"Webshop\"\n" +
		"lists[4]{id,name,folder}:\n" +
		"  10,alpha,(folderless)\n" +
		"  30,Zoo,(folderless)\n" +
		"  21,Next,A Folder\n" +
		"  22,Backlog,B Folder\n" +
		"help[1]: Run `clickup-axi lists --space \"<name|id>\" --archived` to see archived lists\n"
	if out != want {
		t.Errorf("lists output =\n%s\nwant:\n%s", out, want)
	}
}

func TestListsArchivedTraversesBothFolderStatesAndDeduplicates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("archived"); got != "true" {
			t.Errorf("folderless archived = %q, want true", got)
		}
		w.Write([]byte(`{"lists": [{"id": "11", "name": "Old Inbox"}]}`))
	})
	folderCalls := map[string]int{}
	f.mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		archived := r.URL.Query().Get("archived")
		folderCalls[archived]++
		switch archived {
		case "false":
			w.Write([]byte(`{"folders": [{"id": "f1", "name": "Current"}]}`))
		case "true":
			w.Write([]byte(`{"folders": [{"id": "f1", "name": "Current"}, {"id": "f2", "name": "Old"}]}`))
		default:
			t.Errorf("unexpected archived query %q", archived)
		}
	})
	folderListCalls := map[string]int{}
	for _, id := range []string{"f1", "f2"} {
		id := id
		f.mux.HandleFunc("GET /api/v2/folder/"+id+"/list", func(w http.ResponseWriter, r *http.Request) {
			folderListCalls[id]++
			if got := r.URL.Query().Get("archived"); got != "true" {
				t.Errorf("%s archived = %q, want true", id, got)
			}
			if id == "f1" {
				w.Write([]byte(`{"lists": [{"id": "12", "name": "Current archived"}]}`))
				return
			}
			w.Write([]byte(`{"lists": [{"id": "13", "name": "Old archived"}]}`))
		})
	}

	out, code := runCLI(t, c, "lists", "--space", "90121", "--archived")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if folderCalls["false"] != 1 || folderCalls["true"] != 1 {
		t.Errorf("folder traversal = %#v, want one request for each archive state", folderCalls)
	}
	if folderListCalls["f1"] != 1 || folderListCalls["f2"] != 1 {
		t.Errorf("folder list traversal = %#v, want each unique folder once", folderListCalls)
	}
	want := "lists: 3 archived lists in space 90121\n" +
		"lists[3]{id,name,folder}:\n" +
		"  11,Old Inbox,(folderless)\n" +
		"  12,Current archived,Current\n" +
		"  13,Old archived,Old\n" +
		"help[1]: Run `clickup-axi lists --space \"<name|id>\"` to see active lists\n"
	if out != want {
		t.Errorf("lists output =\n%s\nwant:\n%s", out, want)
	}
}

func TestListsEmptyStateIsExplicit(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"lists": []}`))
	})
	f.mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"folders": []}`))
	})

	out, code := runCLI(t, c, "lists", "--space", "90121")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	want := "lists: 0 active lists in space 90121\n" +
		"help[1]: Run `clickup-axi lists --space \"<name|id>\" --archived` to see archived lists\n"
	if out != want {
		t.Errorf("lists output =\n%s\nwant:\n%s", out, want)
	}
}

func TestListsUsageErrorsAreActionable(t *testing.T) {
	_, c := newFakeClickUp(t)
	cases := [][]string{
		{"lists"},
		{"lists", "--space"},
		{"lists", "--space", "--archived"},
		{"lists", "--space", "one", "--space", "two"},
		{"lists", "--unknown"},
		{"lists", "unexpected"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, code := runCLI(t, c, args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
			}
			if !strings.Contains(out, "Run `clickup-axi lists --space \"<name|id>\"`") {
				t.Errorf("usage error lacks corrective command\noutput:\n%s", out)
			}
		})
	}
}

func TestSpacesUsageErrorAndHelpAreSelfContained(t *testing.T) {
	_, c := newFakeClickUp(t)
	out, code := runCLI(t, c, "spaces", "--archived")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "valid flags: --help") || !strings.Contains(out, "Run `clickup-axi spaces`") {
		t.Errorf("spaces usage error lacks valid flags or corrective command\noutput:\n%s", out)
	}

	out, code = runCLI(t, c, "lists", "--help")
	if code != 0 {
		t.Fatalf("help exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "--space <name|id>") || !strings.Contains(out, "--archived") {
		t.Errorf("lists help lacks its complete flag reference\noutput:\n%s", out)
	}
}

func TestListsSpaceResolutionFailureInlinesCandidates(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.spaces(t, "9018", `{"spaces": [{"id": "90121", "name": "Webshop"}]}`)

	out, code := runCLI(t, c, "lists", "--space", "nope")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{`space "nope" matches none`, `90121 "Webshop"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestListsLateAPIErrorPrintsNoPartialInventory(t *testing.T) {
	f, c := newFakeClickUp(t)
	f.me(t, 42, "jan")
	f.mux.HandleFunc("GET /api/v2/space/90121/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"lists": [{"id": "11", "name": "Would be partial"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/space/90121/folder", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"folders": [{"id": "f1", "name": "Folder"}]}`))
	})
	f.mux.HandleFunc("GET /api/v2/folder/f1/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"err": "broken upstream"}`))
	})

	out, code := runCLI(t, c, "lists", "--space", "90121")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "error: ClickUp rejected the request: broken upstream (HTTP 500)") {
		t.Errorf("translated API error missing\noutput:\n%s", out)
	}
	for _, forbidden := range []string{"lists:", "lists[", "Would be partial"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("partial inventory leaked %q\noutput:\n%s", forbidden, out)
		}
	}
}
