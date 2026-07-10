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

// TokenFilePath is the platform-native token location
// (os.UserConfigDir): ~/.config on Linux, ~/Library/Application
// Support on macOS. Deliberately NOT overridden by XDG_CONFIG_HOME
// outside Linux - macOS shells commonly export it, and honoring it
// would strand tokens stored by earlier versions. Tests isolate by
// relocating HOME instead.
func TokenFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clickup-axi", "token"), nil
}
