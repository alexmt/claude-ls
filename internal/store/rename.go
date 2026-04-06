package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type customTitleEntry struct {
	Type        string    `json:"type"`
	CustomTitle string    `json:"customTitle"`
	SessionID   string    `json:"sessionId"`
	Timestamp   time.Time `json:"timestamp"`
}

// RenameSession appends a custom-title entry to the session JSONL file.
func RenameSession(s Session, name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	path := filepath.Join(home, ".claude", "projects", s.ProjectDir, s.ID+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer closeQuietly(f)

	entry := customTitleEntry{
		Type:        "custom-title",
		CustomTitle: name,
		SessionID:   s.ID,
		Timestamp:   time.Now().UTC(),
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	_, err = f.Write(append(line, '\n'))
	return err
}
