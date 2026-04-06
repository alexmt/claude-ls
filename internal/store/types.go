package store

import (
	"io"
	"time"
)

type Session struct {
	ID          string
	ProjectPath string // from cwd field in JSONL, used for display
	ProjectDir  string // actual encoded directory name in ~/.claude/projects/
	ResumeDir   string // directory to run `claude --resume` from
	Slug        string // auto-generated slug
	CustomTitle string // set via /rename, takes precedence over Slug
	FirstMsg    string // from ~/.claude/history.jsonl
	LastActive  time.Time
	IsOrphaned  bool // project path doesn't exist on disk
	IsNamed     bool // has a custom title set via /rename
}

func closeQuietly(c io.Closer) {
	_ = c.Close()
}

// DisplayName returns the custom title if set, otherwise the slug.
func (s Session) DisplayName() string {
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	if s.Slug != "" {
		return s.Slug
	}
	return s.ID[:8]
}
