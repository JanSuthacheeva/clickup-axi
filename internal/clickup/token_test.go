package clickup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenPrefersEnvOverFile(t *testing.T) {
	// Relocate the user config directory into a temp dir and clear
	// CLICKUP_TOKEN so the test never touches the real environment.
	// os.UserConfigDir derives from XDG_CONFIG_HOME on Linux but from
	// HOME elsewhere (~/Library/Application Support on macOS), so both
	// must move - and the expected path must come from TokenFilePath,
	// not be rebuilt by hand.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("CLICKUP_TOKEN", "")
	tokenPath, err := TokenFilePath()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("pk_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := resolveToken(); got != "pk_file" {
		t.Errorf("resolveToken() = %q, want stored token %q", got, "pk_file")
	}

	t.Setenv("CLICKUP_TOKEN", "pk_env")
	if got := resolveToken(); got != "pk_env" {
		t.Errorf("resolveToken() = %q, want env token %q", got, "pk_env")
	}
}
