package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// MoveSession moves a session's files from one project directory to another.
func MoveSession(s Session, targetDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	srcBase := filepath.Join(home, ".claude", "projects", s.ProjectDir)
	dstBase := filepath.Join(home, ".claude", "projects", targetDir)

	if err := os.MkdirAll(dstBase, 0755); err != nil {
		return fmt.Errorf("create target project dir: %w", err)
	}

	// move session JSONL
	srcFile := filepath.Join(srcBase, s.ID+".jsonl")
	dstFile := filepath.Join(dstBase, s.ID+".jsonl")
	if err := os.Rename(srcFile, dstFile); err != nil {
		return fmt.Errorf("move session file: %w", err)
	}

	// move subagent directory if present
	srcDir := filepath.Join(srcBase, s.ID)
	if _, err := os.Stat(srcDir); err == nil {
		dstDir := filepath.Join(dstBase, s.ID)
		if err := os.Rename(srcDir, dstDir); err != nil {
			return fmt.Errorf("move session dir: %w", err)
		}
	}

	return nil
}
