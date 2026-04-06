package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// auto-generated slugs are exactly three lowercase words: adj-adj-surname
var autoSlugRe = regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+$`)

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
			if err != nil {
				continue
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
	defer f.Close()

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
	defer f.Close()

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

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
