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

	"github.com/alexmt/claude-ls/internal/store"
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
type sessionMovedMsg struct {
	id         string
	newProject string // path
	newDir     string // encoded directory name
}

type newSessionExitMsg struct {
	existingIDs map[string]bool
}

type projectEntry struct {
	Path string // human-readable, for display
	Dir  string // encoded directory name, for file operations
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

	moving       bool
	moveProjects []projectEntry // all candidate projects (excluding current)
	moveCursor   int
	moveOffset   int
	moveFilter   string

	searching   bool
	searchQuery string

	showSettings   bool
	settingsCursor int

	pickingNew        bool
	newPickerProjects []projectEntry // all known projects, CWD at index 0
	newPickerCursor   int
	newPickerOffset   int
	newPickerFilter   string

	confirmNew   bool   // keep-or-delete prompt after a new session exits
	newSessionID string // ID of the session just created

	settings store.Settings

	width, height int
}

func New(sessions []store.Session, settings store.Settings) model {
	return model{sessions: sessions, settings: settings}
}

func (m model) visibleSessions() []store.Session {
	if m.searchQuery == "" {
		return m.sessions
	}
	q := strings.ToLower(m.searchQuery)
	var out []store.Session
	for _, s := range m.sessions {
		if strings.Contains(strings.ToLower(s.DisplayName()), q) ||
			strings.Contains(strings.ToLower(s.LastMsg), q) {
			out = append(out, s)
		}
	}
	return out
}

func (m model) exitSearch() model {
	vs := m.visibleSessions()
	var targetID string
	if m.cursor < len(vs) {
		targetID = vs[m.cursor].ID
	}
	m.searching = false
	m.searchQuery = ""
	if targetID != "" {
		for i, s := range m.sessions {
			if s.ID == targetID {
				m.cursor = i
				break
			}
		}
	} else {
		m.cursor = 0
	}
	return m.clampListOffset()
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
		m = m.handleDeleted(msg.id)
		var cmd tea.Cmd
		m, cmd = m.triggerPreview()
		return m, cmd

	case sessionRenamedMsg:
		return m.handleRenamed(msg.id, msg.name), nil

	case sessionMovedMsg:
		return m.handleMoved(msg.id, msg.newProject, msg.newDir), nil

	case newSessionExitMsg:
		return m.handleNewSessionExit(msg.existingIDs)

	case tea.KeyMsg:
		if m.renaming {
			return m.updateRename(msg)
		}
		if m.confirming {
			return m.updateConfirm(msg)
		}
		if m.moving {
			return m.updateMove(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.pickingNew {
			return m.updateNewPicker(msg)
		}
		if m.confirmNew {
			return m.updateConfirmNew(msg)
		}
		if m.showSettings {
			return m.updateSettings(msg)
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
				m = m.clampListOffset()
				m, cmd := m.triggerPreview()
				return m, cmd
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
				m = m.clampListOffset()
				m, cmd := m.triggerPreview()
				return m, cmd
			}
		} else {
			m.previewScroll++
		}

	case "g":
		if m.focus == paneList && len(m.sessions) > 0 {
			m.cursor = 0
			m = m.clampListOffset()
			m, cmd := m.triggerPreview()
			return m, cmd
		}

	case "G":
		if m.focus == paneList && len(m.sessions) > 0 {
			m.cursor = len(m.sessions) - 1
			m = m.clampListOffset()
			m, cmd := m.triggerPreview()
			return m, cmd
		}

	case "enter":
		if m.focus == paneList && len(m.sessions) > 0 {
			return m, resumeSession(m.sessions[m.cursor], m.settings.DangerouslySkipPermissions)
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

	case "m":
		if m.focus == paneList && len(m.sessions) > 0 {
			m.moveProjects = uniqueProjects(m.sessions, m.sessions[m.cursor].ProjectDir)
			if len(m.moveProjects) > 0 {
				m.moving = true
				m.moveCursor = 0
			}
		}

	case "/":
		if m.focus == paneList {
			m.searching = true
			m.searchQuery = ""
			m.cursor = 0
			m.listOffset = 0
		}

	case "s":
		if m.focus == paneList {
			m.showSettings = true
			m.settingsCursor = 0
		}

	case "n":
		if m.focus == paneList {
			cwd, _ := os.Getwd()
			m.newPickerProjects = buildNewPickerProjects(m.sessions, cwd)
			m.pickingNew = true
			m.newPickerCursor = 0
			m.newPickerOffset = 0
			m.newPickerFilter = ""
		}
	}

	return m, nil
}

func (m model) updateMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredProjects()

	switch msg.String() {
	case "esc":
		m.moving = false
		m.moveFilter = ""
	case "up":
		if m.moveCursor > 0 {
			m.moveCursor--
			m = m.clampMoveOffset()
		}
	case "down":
		if m.moveCursor < len(filtered)-1 {
			m.moveCursor++
			m = m.clampMoveOffset()
		}
	case "backspace":
		if len(m.moveFilter) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.moveFilter)
			m.moveFilter = m.moveFilter[:len(m.moveFilter)-size]
			m.moveCursor = 0
			m.moveOffset = 0
		}
	case "enter":
		if len(filtered) > 0 {
			target := filtered[m.moveCursor]
			s := m.sessions[m.cursor]
			m.moving = false
			m.moveFilter = ""
			return m, moveSession(s, target.Dir, target.Path)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.moveFilter += msg.String()
			m.moveCursor = 0
			m.moveOffset = 0
		}
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vs := m.visibleSessions()
	var currentID string
	if m.cursor < len(vs) {
		currentID = vs[m.cursor].ID
	}

	switch msg.String() {
	case "esc":
		m = m.exitSearch()
		m, cmd := m.triggerPreview()
		return m, cmd
	case "enter":
		vs := m.visibleSessions()
		if m.cursor < len(vs) {
			return m, resumeSession(vs[m.cursor], m.settings.DangerouslySkipPermissions)
		}
		return m, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m = m.clampListOffset()
			m, cmd := m.triggerPreview()
			return m, cmd
		}
		return m, nil
	case "down", "j":
		vs := m.visibleSessions()
		if m.cursor < len(vs)-1 {
			m.cursor++
			m = m.clampListOffset()
			m, cmd := m.triggerPreview()
			return m, cmd
		}
		return m, nil
	case "backspace":
		if len(m.searchQuery) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.searchQuery)
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-size]
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.searchQuery += msg.String()
		}
	}

	// After query change, keep cursor on same session if still visible
	newVs := m.visibleSessions()
	m.cursor = 0
	for i, s := range newVs {
		if s.ID == currentID {
			m.cursor = i
			break
		}
	}
	m = m.clampListOffset()
	m, cmd := m.triggerPreview()
	return m, cmd
}

func (m model) filteredProjects() []projectEntry {
	if m.moveFilter == "" {
		return m.moveProjects
	}
	filter := strings.ToLower(m.moveFilter)
	var out []projectEntry
	for _, p := range m.moveProjects {
		if strings.Contains(strings.ToLower(formatPath(p.Path)), filter) {
			out = append(out, p)
		}
	}
	return out
}

func (m model) clampMoveOffset() model {
	page := m.movePickerPageSize()
	if m.moveCursor < m.moveOffset {
		m.moveOffset = m.moveCursor
	} else if m.moveCursor >= m.moveOffset+page {
		m.moveOffset = m.moveCursor - page + 1
	}
	return m
}

func (m model) movePickerPageSize() int {
	// total height
	// - 1 status bar
	// - 3 preview pane header (session name + path + separator)
	// - 4 overlay header (blank + title + filter + separator)
	return max(1, m.height-1-3-4)
}

func (m model) updateNewPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredNewProjects()

	switch msg.String() {
	case "esc":
		m.pickingNew = false
		m.newPickerFilter = ""
	case "up":
		if m.newPickerCursor > 0 {
			m.newPickerCursor--
			m = m.clampNewPickerOffset()
		}
	case "down":
		if m.newPickerCursor < len(filtered)-1 {
			m.newPickerCursor++
			m = m.clampNewPickerOffset()
		}
	case "backspace":
		if len(m.newPickerFilter) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.newPickerFilter)
			m.newPickerFilter = m.newPickerFilter[:len(m.newPickerFilter)-size]
			m.newPickerCursor = 0
			m.newPickerOffset = 0
		}
	case "enter":
		if len(filtered) > 0 {
			dir := filtered[m.newPickerCursor].Path
			existing := make(map[string]bool, len(m.sessions))
			for _, s := range m.sessions {
				existing[s.ID] = true
			}
			m.pickingNew = false
			m.newPickerFilter = ""
			return m, newSessionCmd(existing, dir, m.settings.DangerouslySkipPermissions)
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.newPickerFilter += msg.String()
			m.newPickerCursor = 0
			m.newPickerOffset = 0
		}
	}
	return m, nil
}

func (m model) filteredNewProjects() []projectEntry {
	if m.newPickerFilter == "" {
		return m.newPickerProjects
	}
	filter := strings.ToLower(m.newPickerFilter)
	var out []projectEntry
	for _, p := range m.newPickerProjects {
		if strings.Contains(strings.ToLower(formatPath(p.Path)), filter) {
			out = append(out, p)
		}
	}
	return out
}

func (m model) clampNewPickerOffset() model {
	page := m.newPickerPageSize()
	if m.newPickerCursor < m.newPickerOffset {
		m.newPickerOffset = m.newPickerCursor
	} else if m.newPickerCursor >= m.newPickerOffset+page {
		m.newPickerOffset = m.newPickerCursor - page + 1
	}
	return m
}

func (m model) newPickerPageSize() int {
	return max(1, m.height-1-3-4)
}

func buildNewPickerProjects(sessions []store.Session, cwd string) []projectEntry {
	seenDir := map[string]bool{}
	var projects []projectEntry

	// CWD is always first; find its encoded dir if it has existing sessions
	var cwdEntry projectEntry
	cwdEntry.Path = cwd
	for _, s := range sessions {
		if s.ProjectPath == cwd && s.ProjectDir != "" {
			cwdEntry.Dir = s.ProjectDir
			seenDir[s.ProjectDir] = true
			break
		}
	}
	projects = append(projects, cwdEntry)

	// Append all other known projects
	for _, s := range sessions {
		if s.ProjectDir != "" && !seenDir[s.ProjectDir] {
			seenDir[s.ProjectDir] = true
			projects = append(projects, projectEntry{Path: s.ProjectPath, Dir: s.ProjectDir})
		}
	}

	return projects
}

func (m model) handleMoved(id, newProject, newDir string) model {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			m.sessions[i].ProjectPath = newProject
			m.sessions[i].ProjectDir = newDir
			m.sessions[i].IsOrphaned = false
			break
		}
	}
	return m
}

func (m model) handleNewSessionExit(existingIDs map[string]bool) (tea.Model, tea.Cmd) {
	// Reload sessions from disk to pick up the newly created session.
	newSessions, _ := store.Load()
	m.sessions = newSessions
	m.searching = false
	m.searchQuery = ""

	// Find the newest session not present before the new session was started.
	var newID string
	var newestTime time.Time
	for _, s := range m.sessions {
		if !existingIDs[s.ID] {
			if newID == "" || s.LastActive.After(newestTime) {
				newID = s.ID
				newestTime = s.LastActive
			}
		}
	}

	if newID != "" {
		m.newSessionID = newID
		m.confirmNew = true
		for i, s := range m.sessions {
			if s.ID == newID {
				m.cursor = i
				break
			}
		}
	} else {
		// Nothing was created (user exited immediately); just refresh.
		if m.cursor >= len(m.sessions) {
			m.cursor = max(0, len(m.sessions)-1)
		}
	}
	m = m.clampListOffset()
	m, cmd := m.triggerPreview()
	return m, cmd
}

func (m model) updateConfirmNew(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter", "esc":
		m.confirmNew = false // keep the session
	case "n", "N":
		m.confirmNew = false
		for _, s := range m.sessions {
			if s.ID == m.newSessionID {
				return m, deleteSession(s)
			}
		}
	}
	return m, nil
}

func (m model) renderConfirmNewOverlay(width int) []string {
	var s *store.Session
	for i := range m.sessions {
		if m.sessions[i].ID == m.newSessionID {
			s = &m.sessions[i]
			break
		}
	}

	lines := []string{
		"",
		previewHeaderStyle.Render("Session ended — keep it?"),
		"",
	}
	if s != nil {
		lines = append(lines,
			"  "+truncate(s.DisplayName(), width-4),
			"  "+dimStyle.Render(formatPath(s.ProjectPath)+" • "+formatAge(s.LastActive)),
			"",
		)
	}
	lines = append(lines,
		"  y / enter  keep",
		dangerStyle.Render("  n          delete"),
		"",
		dimStyle.Render("  esc also keeps"),
	)
	return lines
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
	m = m.clampListOffset()
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
	m = m.clampListOffset()
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

func (m model) triggerPreview() (model, tea.Cmd) {
	vs := m.visibleSessions()
	if len(vs) == 0 {
		return m, nil
	}
	s := vs[m.cursor]
	if s.ID == m.previewID {
		return m, nil
	}
	m.previewLoading = true
	m.previewID = s.ID
	return m, loadPreview(s)
}

func (m model) currentID() string {
	vs := m.visibleSessions()
	if len(vs) == 0 {
		return ""
	}
	return vs[m.cursor].ID
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
// Each session is 3 display lines; separator is 1.
func (m model) listPageSize() int {
	usable := m.height - 1 // minus status bar
	return usable / 3
}

func (m model) clampListOffset() model {
	page := m.listPageSize()
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	} else if m.cursor >= m.listOffset+page {
		m.listOffset = m.cursor - page + 1
	}
	return m
}

func (m model) renderList(width int) string {
	usable := m.height - 1 // display lines available
	vs := m.visibleSessions()

	// find where the named/regular separator falls in visible sessions
	namedCount := 0
	for _, s := range vs {
		if s.IsNamed {
			namedCount++
		} else {
			break
		}
	}

	var displayLines []string
	for i := m.listOffset; i < len(vs); i++ {
		s := vs[i]

		// separator between named and regular sections
		if i == namedCount && namedCount > 0 && i > m.listOffset {
			displayLines = append(displayLines, separatorStyle.Render(strings.Repeat("─", width)))
		}

		row := m.renderSessionRow(s, i == m.cursor, width, m.searchQuery)
		// each row is "line1\nline2\nline3" — split and add individually
		displayLines = append(displayLines, strings.Split(row, "\n")...)

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

func (m model) renderSessionRow(s store.Session, selected bool, width int, query string) string {
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

	snippet := s.LastMsg
	snippetWidth := max(0, width-4)
	if snippetWidth > 0 && len(snippet) > snippetWidth {
		snippet = snippet[:snippetWidth-1] + "…"
	}
	// collapse newlines so multi-line messages stay on one row
	snippet = strings.ReplaceAll(snippet, "\n", " ")

	var row1, row2, row3 string
	if selected {
		row1 = selectedStyle.Render(marker + name + strings.Repeat(" ", padding) + " " + age)
		row2 = selectedStyle.Render("  " + path)
		row3 = selectedStyle.Render("  " + snippet)
	} else {
		nameHL := renderWithHighlight(name, query, lipgloss.NewStyle(), matchStyle)
		snippetHL := renderWithHighlight(snippet, query, dimStyle, matchStyle)
		row1 = marker + nameHL + strings.Repeat(" ", padding) + " " + age
		row2 = "  " + dimStyle.Render(path)
		row3 = "  " + snippetHL
	}

	return row1 + "\n" + row2 + "\n" + row3
}

func (m model) renderPreview(width int) string {
	usable := m.height - 1
	vs := m.visibleSessions()

	if len(vs) == 0 {
		return strings.Repeat(" \n", max(0, usable))
	}

	s := vs[m.cursor]
	header := previewHeaderStyle.Render(truncate(s.DisplayName(), width-1)) + "\n"
	subheader := dimStyle.Render(formatPath(s.ProjectPath)+" • "+formatAge(s.LastActive)) + "\n"
	sep := dimStyle.Render(strings.Repeat("─", max(0, width-1))) + "\n"

	headerLines := 3
	contentHeight := max(0, usable-headerLines)

	var contentLines []string
	if m.pickingNew {
		contentLines = m.renderNewPickerOverlay(width)
	} else if m.confirmNew {
		contentLines = m.renderConfirmNewOverlay(width)
	} else if m.showSettings {
		contentLines = m.renderSettingsOverlay(width)
	} else if m.moving {
		contentLines = m.renderMoveOverlay(width)
	} else if m.confirming {
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

func (m model) renderRenameOverlay(_ int) []string {
	prompt := "Rename: " + m.renameInput + "█"
	return []string{"", prompt, "", dimStyle.Render("enter to confirm, esc to cancel")}
}

func (m model) renderNewPickerOverlay(width int) []string {
	filtered := m.filteredNewProjects()
	cwdPath := ""
	if len(m.newPickerProjects) > 0 {
		cwdPath = m.newPickerProjects[0].Path
	}

	filterDisplay := m.newPickerFilter + "█"
	if m.newPickerFilter == "" {
		filterDisplay = dimStyle.Render("type to filter…") + "█"
	}

	lines := []string{
		"",
		previewHeaderStyle.Render("New session in:"),
		filterDisplay,
		dimStyle.Render(strings.Repeat("─", max(0, width-1))),
	}

	page := m.newPickerPageSize()
	end := m.newPickerOffset + page
	if end > len(filtered) {
		end = len(filtered)
	}

	if len(filtered) == 0 {
		lines = append(lines, dimStyle.Render("  no matches"))
	}

	for i := m.newPickerOffset; i < end; i++ {
		p := filtered[i]
		display := formatPath(p.Path)
		if width > 5 && len(display) > width-4 {
			display = "…" + display[len(display)-(width-5):]
		}
		if p.Path == cwdPath {
			display += dimStyle.Render(" (current dir)")
		}
		row := "  " + display
		if i == m.newPickerCursor {
			row = selectedStyle.Render("> " + display)
		}
		lines = append(lines, row)
	}

	return lines
}

func (m model) renderMoveOverlay(width int) []string {
	filtered := m.filteredProjects()

	filterDisplay := m.moveFilter + "█"
	if m.moveFilter == "" {
		filterDisplay = dimStyle.Render("type to filter…") + "█"
	}

	lines := []string{
		"",
		previewHeaderStyle.Render("Move to project:"),
		filterDisplay,
		dimStyle.Render(strings.Repeat("─", max(0, width-1))),
	}

	page := m.movePickerPageSize()
	end := m.moveOffset + page
	if end > len(filtered) {
		end = len(filtered)
	}

	if len(filtered) == 0 {
		lines = append(lines, dimStyle.Render("  no matches"))
	}

	for i := m.moveOffset; i < end; i++ {
		display := formatPath(filtered[i].Path)
		if width > 5 && len(display) > width-4 {
			display = "…" + display[len(display)-(width-5):]
		}
		row := "  " + display
		if i == m.moveCursor {
			row = selectedStyle.Render("> " + display)
		}
		lines = append(lines, row)
	}

	return lines
}

func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showSettings = false
	case "enter", " ":
		// toggle the currently selected setting and persist
		switch m.settingsCursor {
		case 0:
			m.settings.DangerouslySkipPermissions = !m.settings.DangerouslySkipPermissions
			_ = store.SaveSettings(m.settings)
		}
	}
	return m, nil
}

func (m model) renderSettingsOverlay(_ int) []string {
	checkbox := "[ ]"
	if m.settings.DangerouslySkipPermissions {
		checkbox = "[✓]"
	}

	row := "  " + checkbox + " Dangerously skip permissions"
	if m.settingsCursor == 0 {
		row = selectedStyle.Render("> " + checkbox + " Dangerously skip permissions")
	}

	return []string{
		"",
		previewHeaderStyle.Render("Settings"),
		"",
		row,
		dimStyle.Render("    Passes --dangerously-skip-permissions when resuming sessions"),
		"",
		dimStyle.Render("enter/space toggle   esc close"),
	}
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
	if m.pickingNew {
		keys = "↑/↓ select project  enter start session  esc cancel"
	} else if m.confirmNew {
		keys = "y/enter keep   n delete   esc keep"
	} else if m.searching {
		vs := m.visibleSessions()
		count := fmt.Sprintf("%d/%d", len(vs), len(m.sessions))
		keys = "/ " + m.searchQuery + "█   " + count + "   ↑/↓ navigate  enter resume  esc cancel"
	} else if m.showSettings {
		keys = "enter/space toggle   esc close"
	} else if m.moving {
		keys = "↑/↓ select project  enter move here  esc cancel"
	} else if m.confirming {
		keys = "y delete  n cancel"
	} else if m.renaming {
		keys = "enter confirm  esc cancel"
	} else if m.focus == paneList {
		keys = "n new  / search  enter resume  r rename  m move  d delete  s settings  g/G top/bottom  tab focus  q quit"
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

func newSessionCmd(existingIDs map[string]bool, dir string, skipPerms bool) tea.Cmd {
	args := []string{}
	if skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return newSessionExitMsg{existingIDs: existingIDs}
	})
}

func resumeSession(s store.Session, skipPerms bool) tea.Cmd {
	args := []string{"--resume", s.ID}
	if skipPerms {
		args = append(args, "--dangerously-skip-permissions")
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = s.ResumeDir
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return nil
	})
}

func moveSession(s store.Session, targetDir, targetPath string) tea.Cmd {
	return func() tea.Msg {
		if err := store.MoveSession(s, targetDir); err != nil {
			return nil
		}
		return sessionMovedMsg{id: s.ID, newProject: targetPath, newDir: targetDir}
	}
}

func deleteSession(s store.Session) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		base := filepath.Join(home, ".claude", "projects", s.ProjectDir)
		_ = os.Remove(filepath.Join(base, s.ID+".jsonl"))
		_ = os.RemoveAll(filepath.Join(base, s.ID))
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
	selectedStyle      = lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
	dimStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	separatorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	dividerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	previewHeaderStyle = lipgloss.NewStyle().Bold(true)
	userStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	assistantStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statusStyle        = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("250"))
	dangerStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	matchStyle         = lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("0")).Bold(true)
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

func uniqueProjects(sessions []store.Session, excludeDir string) []projectEntry {
	seen := map[string]bool{excludeDir: true}
	var projects []projectEntry
	for _, s := range sessions {
		if !seen[s.ProjectDir] {
			seen[s.ProjectDir] = true
			projects = append(projects, projectEntry{Path: s.ProjectPath, Dir: s.ProjectDir})
		}
	}
	return projects
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// renderWithHighlight renders text with query matches highlighted using matchSt,
// and non-matching segments styled with base.
func renderWithHighlight(text, query string, base, matchSt lipgloss.Style) string {
	if query == "" {
		return base.Render(text)
	}
	lower := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	var result strings.Builder
	start := 0
	for {
		rel := strings.Index(lower[start:], lowerQ)
		if rel < 0 {
			break
		}
		abs := start + rel
		if abs > start {
			result.WriteString(base.Render(text[start:abs]))
		}
		result.WriteString(matchSt.Render(text[abs : abs+len(lowerQ)]))
		start = abs + len(lowerQ)
	}
	result.WriteString(base.Render(text[start:]))
	return result.String()
}
