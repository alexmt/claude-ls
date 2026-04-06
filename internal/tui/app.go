package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amatyushentsev/claude-ls/internal/store"
)

// layout constants
const (
	leftPaneRatio = 0.40
	minLeftWidth  = 35
)

type pane int

const (
	paneList pane = iota
	panePreview
)

type previewLoadedMsg struct {
	sessionID string
	messages  []store.PreviewMessage
}

type sessionDeletedMsg struct{ id string }
type sessionRenamedMsg struct {
	id   string
	name string
}

type model struct {
	sessions   []store.Session
	cursor     int
	listOffset int // index of first visible session
	focus      pane

	preview        []store.PreviewMessage
	previewID      string // session ID currently loaded
	previewScroll  int
	previewLoading bool

	renaming    bool
	renameInput string

	confirming bool // waiting for delete confirmation

	width, height int
}

func New(sessions []store.Session) model {
	return model{sessions: sessions}
}

func (m model) Init() tea.Cmd {
	if len(m.sessions) > 0 {
		return loadPreview(m.sessions[0])
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case previewLoadedMsg:
		if msg.sessionID == m.currentID() {
			m.preview = msg.messages
			m.previewLoading = false
			m.previewScroll = 0
		}
		return m, nil

	case sessionDeletedMsg:
		return m.handleDeleted(msg.id), nil

	case sessionRenamedMsg:
		return m.handleRenamed(msg.id, msg.name), nil

	case tea.KeyMsg:
		if m.renaming {
			return m.updateRename(msg)
		}
		if m.confirming {
			return m.updateConfirm(msg)
		}
		return m.updateNav(msg)
	}

	return m, nil
}

func (m model) updateNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		if m.focus == paneList {
			m.focus = panePreview
		} else {
			m.focus = paneList
		}

	case "up", "k":
		if m.focus == paneList {
			if m.cursor > 0 {
				m.cursor--
				m.clampListOffset()
				return m, m.triggerPreview()
			}
		} else {
			if m.previewScroll > 0 {
				m.previewScroll--
			}
		}

	case "down", "j":
		if m.focus == paneList {
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
				m.clampListOffset()
				return m, m.triggerPreview()
			}
		} else {
			m.previewScroll++
		}

	case "enter":
		if m.focus == paneList && len(m.sessions) > 0 {
			return m, resumeSession(m.sessions[m.cursor].ID)
		}

	case "r":
		if m.focus == paneList && len(m.sessions) > 0 {
			m.renaming = true
			m.renameInput = ""
		}

	case "d":
		if m.focus == paneList && len(m.sessions) > 0 {
			m.confirming = true
		}
	}

	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.confirming = false
		s := m.sessions[m.cursor]
		return m, deleteSession(s)
	case "n", "N", "esc":
		m.confirming = false
	}
	return m, nil
}

func (m model) handleRenamed(id, name string) model {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			m.sessions[i].CustomTitle = name
			m.sessions[i].IsNamed = true
			break
		}
	}
	// re-sort: named sessions float to top
	sort.Slice(m.sessions, func(i, j int) bool {
		if m.sessions[i].IsNamed != m.sessions[j].IsNamed {
			return m.sessions[i].IsNamed
		}
		return m.sessions[i].LastActive.After(m.sessions[j].LastActive)
	})
	// restore cursor to the renamed session
	for i, s := range m.sessions {
		if s.ID == id {
			m.cursor = i
			break
		}
	}
	m.clampListOffset()
	return m
}

func (m model) handleDeleted(id string) model {
	for i, s := range m.sessions {
		if s.ID == id {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			break
		}
	}
	if m.cursor >= len(m.sessions) {
		m.cursor = max(0, len(m.sessions)-1)
	}
	m.clampListOffset()
	m.preview = nil
	m.previewID = ""
	return m
}

func (m model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.renaming = false
		m.renameInput = ""
	case "enter":
		name := strings.TrimSpace(m.renameInput)
		m.renaming = false
		m.renameInput = ""
		if name != "" && len(m.sessions) > 0 {
			return m, renameSession(m.sessions[m.cursor], name)
		}
	case "backspace":
		if len(m.renameInput) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.renameInput)
			m.renameInput = m.renameInput[:len(m.renameInput)-size]
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.renameInput += msg.String()
		}
	}
	return m, nil
}

func (m model) triggerPreview() tea.Cmd {
	if len(m.sessions) == 0 {
		return nil
	}
	s := m.sessions[m.cursor]
	if s.ID == m.previewID {
		return nil
	}
	m.previewLoading = true
	m.previewID = s.ID
	return loadPreview(s)
}

func (m model) currentID() string {
	if len(m.sessions) == 0 {
		return ""
	}
	return m.sessions[m.cursor].ID
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	leftW := max(int(float64(m.width)*leftPaneRatio), minLeftWidth)
	rightW := m.width - leftW - 1 // 1 for divider

	listPane := m.renderList(leftW)
	previewPane := m.renderPreview(rightW)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		listPane,
		dividerStyle.Render(strings.Repeat("│\n", max(0, m.height-2))+"│"),
		previewPane,
	) + "\n" + m.renderStatusBar()
}

// listPageSize returns how many sessions fit in the list pane.
// Each session is 2 display lines; separator is 1.
func (m model) listPageSize() int {
	usable := m.height - 1 // minus status bar
	return usable / 2
}

func (m model) clampListOffset() {
	page := m.listPageSize()
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	} else if m.cursor >= m.listOffset+page {
		m.listOffset = m.cursor - page + 1
	}
}

func (m model) renderList(width int) string {
	usable := m.height - 1 // display lines available

	// find where the named/regular separator falls
	namedCount := 0
	for _, s := range m.sessions {
		if s.IsNamed {
			namedCount++
		} else {
			break
		}
	}

	var displayLines []string
	for i := m.listOffset; i < len(m.sessions); i++ {
		s := m.sessions[i]

		// separator between named and regular sections
		if i == namedCount && namedCount > 0 && i > m.listOffset {
			displayLines = append(displayLines, separatorStyle.Render(strings.Repeat("─", width)))
		}

		row := m.renderSessionRow(s, i == m.cursor, width)
		// each row is "line1\nline2" — split and add individually
		for _, l := range strings.SplitN(row, "\n", 2) {
			displayLines = append(displayLines, l)
		}

		if len(displayLines) >= usable {
			break
		}
	}

	// pad to fill pane
	for len(displayLines) < usable {
		displayLines = append(displayLines, "")
	}

	return strings.Join(displayLines[:usable], "\n")
}

func (m model) renderSessionRow(s store.Session, selected bool, width int) string {
	marker := "  "
	if s.IsNamed {
		marker = "» "
	} else if s.IsOrphaned {
		marker = "✗ "
	}

	name := s.DisplayName()

	age := formatAge(s.LastActive)
	// name + age right-aligned; marker takes 2 chars
	nameWidth := max(0, width-2-len(age)-2) // 2 for marker, 2 padding
	if nameWidth > 0 && len(name) > nameWidth {
		name = name[:max(0, nameWidth-1)] + "…"
	}
	padding := max(0, nameWidth-len(name))

	path := s.ProjectPath
	if home, err := os.UserHomeDir(); err == nil {
		path = strings.Replace(path, home, "~", 1)
	}
	if width > 5 && len(path) > width-4 {
		path = "…" + path[len(path)-(width-5):]
	}

	row1 := marker + name + strings.Repeat(" ", padding) + " " + age
	row2 := "  " + dimStyle.Render(path)

	if selected {
		row1 = selectedStyle.Render(row1)
		row2 = selectedStyle.Render(row2)
	}

	return row1 + "\n" + row2
}

func (m model) renderPreview(width int) string {
	usable := m.height - 1

	if len(m.sessions) == 0 {
		return strings.Repeat(" \n", max(0, usable))
	}

	s := m.sessions[m.cursor]
	header := previewHeaderStyle.Render(truncate(s.DisplayName(), width-1)) + "\n"
	subheader := dimStyle.Render(formatPath(s.ProjectPath)+" • "+formatAge(s.LastActive)) + "\n"
	sep := dimStyle.Render(strings.Repeat("─", max(0, width-1))) + "\n"

	headerLines := 3
	contentHeight := max(0, usable-headerLines)

	var contentLines []string
	if m.confirming {
		contentLines = m.renderConfirmOverlay(width)
	} else if m.renaming {
		contentLines = m.renderRenameOverlay(width)
	} else if m.previewLoading {
		contentLines = []string{"loading…"}
	} else {
		contentLines = m.renderMessages(width)
	}

	// apply scroll
	maxScroll := max(0, len(contentLines)-contentHeight)
	if m.previewScroll > maxScroll {
		m.previewScroll = maxScroll
	}
	visible := contentLines
	if m.previewScroll < len(visible) {
		visible = visible[m.previewScroll:]
	}
	if len(visible) > contentHeight {
		visible = visible[:contentHeight]
	}
	for len(visible) < contentHeight {
		visible = append(visible, "")
	}

	return header + subheader + sep + strings.Join(visible, "\n")
}

func (m model) renderMessages(width int) []string {
	var lines []string
	for i := len(m.preview) - 1; i >= 0; i-- {
		msg := m.preview[i]
		switch msg.Role {
		case store.RoleUser:
			lines = append(lines, userStyle.Render("You")+":")
		case store.RoleAssistant:
			lines = append(lines, assistantStyle.Render("Claude")+":")
		}
		for _, l := range wrapText(msg.Content, width-3) {
			lines = append(lines, "  "+l)
		}
		lines = append(lines, "")
	}
	return lines
}

func (m model) renderRenameOverlay(width int) []string {
	prompt := "Rename: " + m.renameInput + "█"
	return []string{"", prompt, "", dimStyle.Render("enter to confirm, esc to cancel")}
}

func (m model) renderConfirmOverlay(width int) []string {
	if len(m.sessions) == 0 {
		return nil
	}
	name := m.sessions[m.cursor].DisplayName()
	return []string{
		"",
		dangerStyle.Render("Delete session?"),
		"",
		truncate(name, width-2),
		"",
		dimStyle.Render("y  yes, delete permanently"),
		dimStyle.Render("n  cancel"),
	}
}

func (m model) renderStatusBar() string {
	var keys string
	if m.confirming {
		keys = "y delete  n cancel"
	} else if m.renaming {
		keys = "enter confirm  esc cancel"
	} else if m.focus == paneList {
		keys = "enter resume  r rename  d delete  tab focus  q quit"
	} else {
		keys = "j/k scroll  tab focus  q quit"
	}
	bar := statusStyle.Width(m.width).Render(" " + keys)
	return bar
}

// commands

func loadPreview(s store.Session) tea.Cmd {
	return func() tea.Msg {
		msgs, _ := store.LoadPreview(s)
		return previewLoadedMsg{sessionID: s.ID, messages: msgs}
	}
}

func resumeSession(id string) tea.Cmd {
	return tea.ExecProcess(exec.Command("claude", "--resume", id), func(err error) tea.Msg {
		return nil
	})
}

func deleteSession(s store.Session) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		encoded := store.EncodeProjectPath(s.ProjectPath)
		base := filepath.Join(home, ".claude", "projects", encoded)
		os.Remove(filepath.Join(base, s.ID+".jsonl"))
		os.RemoveAll(filepath.Join(base, s.ID))
		return sessionDeletedMsg{id: s.ID}
	}
}

func renameSession(s store.Session, name string) tea.Cmd {
	return func() tea.Msg {
		if err := store.RenameSession(s, name); err != nil {
			return nil
		}
		return sessionRenamedMsg{id: s.ID, name: name}
	}
}

// styles

var (
	selectedStyle     = lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
	dimStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	separatorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	dividerStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	previewHeaderStyle = lipgloss.NewStyle().Bold(true)
	userStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	assistantStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statusStyle       = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("250"))
	dangerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

// helpers

func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}

func formatPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		p = strings.Replace(p, home, "~", 1)
	}
	return p
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, w := range words {
			if line == "" {
				line = w
			} else if len(line)+1+len(w) <= width {
				line += " " + w
			} else {
				lines = append(lines, line)
				line = w
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
