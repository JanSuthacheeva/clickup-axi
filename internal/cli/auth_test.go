package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
)

// isolateConfig relocates the user config directory into a temp dir and
// clears CLICKUP_TOKEN so auth tests never touch the real environment.
// os.UserConfigDir derives from XDG_CONFIG_HOME on Linux but from HOME
// elsewhere (~/Library/Application Support on macOS), so both must
// move - and the expected path must come from TokenFilePath, not be
// rebuilt by hand.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("CLICKUP_TOKEN", "")
	path, err := clickup.TokenFilePath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuthLoginStoresValidatedToken(t *testing.T) {
	f, c := newFakeClickUp(t)
	tokenPath := isolateConfig(t)
	f.mux.HandleFunc("GET /api/v2/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "pk_fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{}`))
			return
		}
		w.Write([]byte(`{"user": {"id": 42, "username": "jan"}}`))
	})

	out, code := runCLIWithStdin(t, c, "pk_fresh\n", "auth", "login")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "auth: logged in as jan (id: 42)") {
		t.Errorf("output missing login confirmation\noutput:\n%s", out)
	}

	b, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("token file not written: %v", err)
	}
	if string(b) != "pk_fresh" {
		t.Errorf("stored token = %q, want %q (trimmed)", b, "pk_fresh")
	}
	info, _ := os.Stat(tokenPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestAuthLoginRejectsInvalidToken(t *testing.T) {
	f, c := newFakeClickUp(t)
	tokenPath := isolateConfig(t)
	f.mux.HandleFunc("GET /api/v2/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{}`))
	})

	out, code := runCLIWithStdin(t, c, "pk_bad", "auth", "login")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "rejected the token") {
		t.Errorf("output missing rejection message\noutput:\n%s", out)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("a rejected token must not be stored")
	}
}

func TestAuthLoginEmptyStdinIsUsageError(t *testing.T) {
	_, c := newFakeClickUp(t)
	isolateConfig(t)

	out, code := runCLIWithStdin(t, c, "  \n", "auth", "login")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "clickup-axi auth login < tokenfile") {
		t.Errorf("output missing by-reference stdin example\noutput:\n%s", out)
	}
	if strings.Contains(out, "echo -n pk_") || strings.Contains(out, "echo pk_") {
		t.Errorf("usage help must not model echoing a literal token\noutput:\n%s", out)
	}
}

// A help request must never trigger the mutation it asks about: before
// the fix, `auth logout --help` removed the token and `auth login
// --help` blocked reading a token from stdin.
func TestAuthSubcommandsHonorHelp(t *testing.T) {
	_, c := newFakeClickUp(t)
	tokenPath := isolateConfig(t)

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("pk_x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, sub := range []string{"login", "logout"} {
		out, code := runCLI(t, c, "auth", sub, "--help")
		if code != 0 {
			t.Fatalf("auth %s --help exit code = %d, want 0\noutput:\n%s", sub, code, out)
		}
		if !strings.Contains(out, "clickup-axi auth <subcommand>") {
			t.Errorf("auth %s --help did not print the auth help\noutput:\n%s", sub, out)
		}
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Errorf("auth logout --help removed the stored token")
	}
}

// Unknown trailing args on auth subcommands are rejected loudly (exit 2)
// instead of silently dropped - a dropped flag would let the command run
// with a meaning the agent did not intend.
func TestAuthSubcommandsRejectUnknownArgs(t *testing.T) {
	_, c := newFakeClickUp(t)
	tokenPath := isolateConfig(t)

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("pk_x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ sub, arg string }{
		{"login", "--bogus"},
		{"logout", "--force"},
		{"logout", "extra"},
	} {
		out, code := runCLI(t, c, "auth", tc.sub, tc.arg)
		if code != 2 {
			t.Fatalf("auth %s %s exit code = %d, want 2\noutput:\n%s", tc.sub, tc.arg, code, out)
		}
		if !strings.Contains(out, tc.arg) {
			t.Errorf("error does not name the rejected argument %q\noutput:\n%s", tc.arg, out)
		}
		if !strings.Contains(out, "--help") {
			t.Errorf("error does not state that only --help is valid\noutput:\n%s", out)
		}
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Errorf("a rejected logout argument must not remove the stored token")
	}
}

func TestAuthLogoutIsIdempotent(t *testing.T) {
	_, c := newFakeClickUp(t)
	tokenPath := isolateConfig(t)

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("pk_x"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, c, "auth", "logout")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "auth: logged out") {
		t.Errorf("output missing logout confirmation\noutput:\n%s", out)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Errorf("token file still exists after logout")
	}

	out, code = runCLI(t, c, "auth", "logout")
	if code != 0 {
		t.Fatalf("second logout exit code = %d, want 0 (no-op)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "already logged out (no-op)") {
		t.Errorf("output missing no-op acknowledgement\noutput:\n%s", out)
	}
}
