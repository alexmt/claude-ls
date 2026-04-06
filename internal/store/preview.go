package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

type PreviewMessage struct {
	Role    MessageRole
	Content string
}

type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"` // for tool_use
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type previewEntry struct {
	Type    string  `json:"type"`
	Message message `json:"message"`
}

func LoadPreview(session Session) ([]PreviewMessage, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// find the JSONL file: ~/.claude/projects/<encoded>/<id>.jsonl
	encoded := encodeProjectPath(session.ProjectPath)
	jsonlPath := filepath.Join(home, ".claude", "projects", encoded, session.ID+".jsonl")

	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []PreviewMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		var e previewEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}

		if e.Type != "user" && e.Type != "assistant" {
			continue
		}

		role := MessageRole(e.Message.Role)
		if role != RoleUser && role != RoleAssistant {
			continue
		}

		var parts []string
		for _, block := range e.Message.Content {
			switch block.Type {
			case "text":
				if t := strings.TrimSpace(block.Text); t != "" {
					parts = append(parts, t)
				}
			case "tool_use":
				parts = append(parts, "[tool: "+block.Name+"]")
			case "tool_result":
				// skip tool results in preview
			}
		}

		if len(parts) == 0 {
			continue
		}

		messages = append(messages, PreviewMessage{
			Role:    role,
			Content: strings.Join(parts, "\n"),
		})
	}

	return messages, scanner.Err()
}

func encodeProjectPath(p string) string {
	return EncodeProjectPath(p)
}

func EncodeProjectPath(p string) string {
	if len(p) == 0 {
		return p
	}
	// /Users/alex/root/myproject -> -Users-alex-root-myproject
	return strings.ReplaceAll(p, "/", "-")
}
