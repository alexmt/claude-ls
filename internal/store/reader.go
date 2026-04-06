package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// auto-generated slugs are exactly three lowercase words: adj-adj-surname
type jsonlEntry struct {
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	Slug        string    `json:"slug"`
	SessionID   string    `json:"sessionId"`
	CustomTitle string    `json:"customTitle"`
	Cwd         string    `json:"cwd"`
}

type historyEntry struct {
	Display   string `json:"display"`
	SessionID string `json:"sessionId"`
}

func Load() ([]Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	projectsDir := filepath.Join(home, ".claude", "projects")
	history, _ := loadHistory(filepath.Join(home, ".claude", "history.jsonl"))

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	var sessions []Session

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		files, err := os.ReadDir(filepath.Join(projectsDir, entry.Name()))
		if err != nil {
			continue
		}

		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}

			id := strings.TrimSuffix(f.Name(), ".jsonl")
			jsonlPath := filepath.Join(projectsDir, entry.Name(), f.Name())

			session, err := readSession(jsonlPath, id)
			if err != nil || session.ProjectPath == "" || session.LastActive.IsZero() {
				continue
			}
			session.ProjectDir = entry.Name()
			// ResumeDir: prefer decoded project dir (what Claude uses as its key)
			// fall back to cwd from JSONL if the decoded path doesn't exist
			if decoded := decodePath(entry.Name()); pathExists(decoded) {
				session.ResumeDir = decoded
			} else {
				session.ResumeDir = session.ProjectPath
			}

			if msg, ok := history[id]; ok {
				session.FirstMsg = msg
			}

			sessions = append(sessions, session)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].IsNamed != sessions[j].IsNamed {
			return sessions[i].IsNamed
		}
		return sessions[i].LastActive.After(sessions[j].LastActive)
	})

	return sessions, nil
}

func readSession(path, id string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer closeQuietly(f)

	s := Session{ID: id}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var lastTimestamp time.Time

	for scanner.Scan() {
		line := scanner.Bytes()
		var e jsonlEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}

		// use cwd from first entry that has it as the project path
		if s.ProjectPath == "" && e.Cwd != "" {
			s.ProjectPath = e.Cwd
		}
		if e.Slug != "" {
			s.Slug = e.Slug
		}
		if e.Type == "custom-title" && e.CustomTitle != "" {
			s.CustomTitle = e.CustomTitle
		}
		if !e.Timestamp.IsZero() {
			lastTimestamp = e.Timestamp
		}
	}

	s.IsNamed = s.CustomTitle != ""
	s.IsOrphaned = s.ProjectPath == "" || !pathExists(s.ProjectPath)
	s.LastActive = lastTimestamp
	return s, scanner.Err()
}

func loadHistory(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer closeQuietly(f)

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var e historyEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.SessionID != "" && e.Display != "" {
			result[e.SessionID] = e.Display
		}
	}

	return result, scanner.Err()
}

// decodePath reverses the simple slash encoding: -Users-alex-root-foo -> /Users/alex/root/foo.
// This is only unambiguous for paths with no hyphens, spaces, or tildes in directory names.
func decodePath(dirName string) string {
	if len(dirName) == 0 || dirName[0] != '-' {
		return dirName
	}
	return "/" + strings.ReplaceAll(dirName[1:], "-", "/")
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
