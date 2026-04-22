package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeDir returns the root Claude configuration directory, honoring the CLAUDE_CONFIG_DIR environment variable.
func ClaudeDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Clean(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve claude config directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}
