package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amatyushentsev/claude-ls/internal/store"
	"github.com/amatyushentsev/claude-ls/internal/tui"
)

func main() {
	sessions, err := store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading sessions: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(
		tui.New(sessions),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
