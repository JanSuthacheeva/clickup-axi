package clickup

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveToken prefers the CLICKUP_TOKEN environment variable and falls
// back to the token stored by `clickup-axi auth login`.
func resolveToken() string {
	if t := os.Getenv("CLICKUP_TOKEN"); t != "" {
		return t
	}
	path, err := TokenFilePath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func TokenFilePath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clickup-axi", "token"), nil
}

// userConfigDir resolves the config root, honoring XDG_CONFIG_HOME on
// every platform - os.UserConfigDir ignores it outside Linux, which
// made tests that relocate the config dir silently touch the real
// ~/Library/Application Support on macOS. Without the override the
// platform default stands, so existing installs keep their paths.
func userConfigDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	return os.UserConfigDir()
}
