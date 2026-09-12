package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// compactModeWidth is the terminal width threshold below which compact layout
// is used: shortened timestamp, no VERSION column, two-line help text.
const compactModeWidth = 110

var (
	// One Dark Pro palette approximations
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptiveColor("73", "6")).
			Underline(true)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(adaptiveColor("59", "8"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptiveColor("114", "10"))

	cursorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2c3d3d"))

	dimStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("59", "8"))

	errorStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("203", "9")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("114", "10")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("59", "8"))

	accentStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("176", "13")).
			Bold(true)

	orangeStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("173", "11"))

	noTitleStyle = lipgloss.NewStyle().
			Foreground(adaptiveColor("59", "8")).
			Italic(true)

	headerBgStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("233")).
			Foreground(adaptiveColor("59", "8"))
)

// adaptiveColor returns a color that adapts to terminal capabilities
// Uses rich 256-color codes on modern terminals, falls back to basic 16 colors otherwise
func adaptiveColor(rich string, fallback string) lipgloss.TerminalColor {
	profile := lipgloss.ColorProfile()
	if profile == termenv.ANSI {
		return lipgloss.Color(fallback)
	}
	return lipgloss.Color(rich)
}

// formatLines formats a line count with thousands separator.
func formatLines(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d", n)
}

// projectHashIndex returns a stable palette index for a project path.
// Hashes the last 2 path segments so similar prefixes get different colors.
func projectHashIndex(project string) uint32 {
	key := project
	if idx := strings.LastIndex(project, "/"); idx > 0 {
		if prev := strings.LastIndex(project[:idx], "/"); prev >= 0 {
			key = project[prev+1:]
		} else {
			key = project[idx+1:]
		}
	}
	const fnvPrime uint32 = 16777619
	h := uint32(2166136261)
	for _, r := range key {
		h ^= uint32(r)
		h *= fnvPrime
	}
	return h % 8
}

// colorForProject returns a muted One Dark Pro accent color for a project.
func colorForProject(project string) lipgloss.Color {
	palette := []lipgloss.Color{"167", "173", "179", "114", "110", "75", "176", "139"}
	return palette[projectHashIndex(project)]
}

// bgTintForProject returns a TrueColor background tinted ~10% toward the project accent color.
// Base background: #282c34 (One Dark Pro).
func bgTintForProject(project string) lipgloss.Color {
	// 10% blend of each accent on #282c34
	tints := []lipgloss.Color{
		"#3a323a", // red   #e06c75
		"#393739", // orange #d19a66
		"#3b3b3b", // yellow #e5c07b
		"#333b3b", // green  #98c379
		"#2c3040", // cornflower #87afd7
		"#2e3947", // blue   #61afef
		"#383445", // purple #c678dd
		"#363640", // mauve  #b48ead
	}
	return tints[projectHashIndex(project)]
}

// Messages
type deleteCompleteMsg struct {
	count int
}

type chatsLoadedMsg struct {
	chats []Chat
}

type errMsg string

type clearCopiedMsg struct {
	id int
}

type clearDeleteMsg struct {
	id int
}

// groupRow represents a single row in the grouped view.
// It is either a project header or a reference to a chat.
type groupRow struct {
	isHeader bool
	project  string // project name (set for both headers and chats)
	chatIdx  int    // index into m.chats (-1 for headers)
}

type model struct {
	cfg           *Config
	chats         []Chat
	cursor        int
	selected      map[int]bool
	confirmDelete bool
	deleting      bool

	// Resume flow: first enter on a chat arms confirmResume and records the
	// target; a second enter quits the TUI so main can launch `claude -r`.
	confirmResume bool
	resumeUUID    string
	resumeDir     string
	deleted       int
	error         string
	width         int
	height        int
	scrollOffset  int
	copiedMsg     string
	deleteTimer   int // Track active delete message timer
	copyTimer     int // Track active copy message timer

	// True when the current m.selected was filled automatically by pressing
	// d with no prior selection. On confirm cancel we revert the auto-selection
	// so the selection state doesn't leak into the next d gesture.
	autoSelected bool

	// Grouped view state
	grouped          bool
	expandedProjects map[string]bool
	groupRows        []groupRow // virtual row list built from chats + expanded state

	// Position to restore after deletion
	postDeleteCursor int

	// Help modal
	showHelp bool

	// Preview modal
	showPreview         bool
	previewRawMsgs      []PreviewMessage
	previewAllLines     []PreviewLine
	previewScrollOffset int
	previewForUUID      string

	// True while chats are being scanned from disk in the background.
	loading bool

	// Search: live filter over title + project path.
	// searching is true while the "/" input line is capturing keystrokes;
	// searchQuery persists after enter so the filter stays applied.
	searching   bool
	searchQuery string
	filteredIdx []int        // ordered indices into m.chats matching searchQuery
	filteredSet map[int]bool // same set, for O(1) membership checks in grouped mode
}

func initialModel(cfg *Config) model {
	grouped := cfg != nil && cfg.GroupByProject
	m := model{
		cfg:              cfg,
		selected:         make(map[int]bool),
		grouped:          grouped,
		expandedProjects: make(map[string]bool),
		loading:          true,
	}
	return m
}

// loadChatsCmd scans the chat history in the background so the TUI can render
// immediately instead of blocking on disk I/O before the first frame.
func loadChatsCmd() tea.Cmd {
	return func() tea.Msg {
		return chatsLoadedMsg{chats: findAllChats()}
	}
}

// rebuildGroupRows creates the virtual row list from chats grouped by project.
// Projects are ordered by the most recent chat timestamp (newest first).
// Chats not matching an active search filter (and projects left with no
// matches) are omitted entirely.
func (m *model) rebuildGroupRows() {
	// Collect unique projects in order of first appearance
	// (chats are already sorted by timestamp desc)
	seen := make(map[string]bool)
	var projects []string
	for i, chat := range m.chats {
		if !m.matchesSearch(i) {
			continue
		}
		if !seen[chat.Project] {
			seen[chat.Project] = true
			projects = append(projects, chat.Project)
		}
	}

	// Build chat index groups
	chatsByProject := make(map[string][]int)
	for i, chat := range m.chats {
		if !m.matchesSearch(i) {
			continue
		}
		chatsByProject[chat.Project] = append(chatsByProject[chat.Project], i)
	}

	var rows []groupRow
	for _, proj := range projects {
		rows = append(rows, groupRow{isHeader: true, project: proj, chatIdx: -1})
		if m.expandedProjects[proj] {
			for _, idx := range chatsByProject[proj] {
				rows = append(rows, groupRow{isHeader: false, project: proj, chatIdx: idx})
			}
		}
	}
	m.groupRows = rows
}

// matchesSearch reports whether chat index i passes the active search filter.
// With no query every chat matches.
func (m model) matchesSearch(i int) bool {
	if m.searchQuery == "" {
		return true
	}
	return m.filteredSet[i]
}

// visibleLen returns the number of chats visible under the active search filter.
func (m model) visibleLen() int {
	if m.searchQuery == "" {
		return len(m.chats)
	}
	return len(m.filteredIdx)
}

// visibleChatIdx maps a position in the visible (filtered) list back to the
// real index into m.chats.
func (m model) visibleChatIdx(pos int) int {
	if m.searchQuery == "" {
		return pos
	}
	return m.filteredIdx[pos]
}

// recomputeSearch rebuilds filteredIdx/filteredSet from the current
// searchQuery without touching cursor/scroll position. Used after the chat
// list itself changes (refresh, delete) so a stale filter doesn't go stale.
func (m *model) recomputeSearch() {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if q == "" {
		m.filteredIdx = nil
		m.filteredSet = nil
	} else {
		m.filteredIdx = nil
		m.filteredSet = make(map[int]bool)
		for i, c := range m.chats {
			if strings.Contains(strings.ToLower(c.Title), q) ||
				strings.Contains(strings.ToLower(decodeProjectPath(c.Project)), q) {
				m.filteredIdx = append(m.filteredIdx, i)
				m.filteredSet[i] = true
			}
		}
	}
	if m.grouped {
		m.rebuildGroupRows()
	}
}

// applySearch recomputes the filter and resets the cursor to the top of the
// (possibly new) visible list. Called on every keystroke while typing.
func (m *model) applySearch() {
	m.recomputeSearch()
	m.cursor = 0
	m.scrollOffset = 0
}

// loadPreviewForCursor loads preview lines for the chat currently under the cursor.
// Uses previewForUUID as a cache key to avoid re-reading the same file.
func (m *model) loadPreviewForCursor() {
	var chatPath, chatUUID string
	if m.grouped {
		if m.cursor < len(m.groupRows) && !m.groupRows[m.cursor].isHeader {
			c := m.chats[m.groupRows[m.cursor].chatIdx]
			chatPath, chatUUID = c.Path, c.UUID
		}
	} else {
		if m.cursor < m.visibleLen() {
			c := m.chats[m.visibleChatIdx(m.cursor)]
			chatPath, chatUUID = c.Path, c.UUID
		}
	}
	if chatUUID == "" || chatUUID == m.previewForUUID {
		return
	}
	m.previewRawMsgs = loadRawPreviewMsgs(chatPath, 200)
	textWidth := m.previewTextWidth()
	m.previewAllLines = renderPreviewMessages(m.previewRawMsgs, textWidth)
	m.previewScrollOffset = 0
	m.previewForUUID = chatUUID
}

// armResume sets up the resume confirmation for a chat. If the project
// directory can't be resolved on disk, it surfaces an error instead so we
// never chdir into a missing path.
func (m *model) armResume(c Chat) {
	dir := resolveProjectDir(c.Project)
	if dir == "" {
		m.error = "Cannot resume: project directory not found on disk"
		return
	}
	m.confirmResume = true
	m.resumeUUID = c.UUID
	m.resumeDir = dir
	m.error = ""
	m.copiedMsg = ""
	m.deleted = 0
}

// chatIndicesForProject returns all chat indices belonging to a project.
func (m model) chatIndicesForProject(project string) []int {
	var indices []int
	for i, chat := range m.chats {
		if chat.Project == project && m.matchesSearch(i) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m model) renderTabBar() string {
	appName := accentStyle.Render("claude chats")
	left := appName
	var stats string
	countStr := fmt.Sprintf("%d chats", len(m.chats))
	if m.searchQuery != "" {
		countStr = fmt.Sprintf("%d/%d chats", m.visibleLen(), len(m.chats))
	}
	if len(m.selected) > 0 {
		badge := lipgloss.NewStyle().
			Background(adaptiveColor("73", "6")).
			Foreground(adaptiveColor("234", "0")).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("%d selected", len(m.selected)))
		stats = dimStyle.Render(countStr) + "  " + badge
	} else {
		stats = dimStyle.Render(countStr)
	}
	width := m.width
	if width < 75 {
		width = 75
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(stats)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + stats
}

func (m model) visibleHeight() int {
	fixed := 9 // tabbar(1) + col-header(1) + sep(1) + bottom-sep(1) + help(1) + status(0-1)
	if m.width < compactModeWidth {
		fixed = 10 // compact: +1 for extra help line
	}
	if m.searching || m.searchQuery != "" {
		fixed++ // search input line
	}

	h := m.height - fixed
	if h < 1 {
		h = 10
	}
	return h
}

func (m model) Init() tea.Cmd {
	return loadChatsCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case chatsLoadedMsg:
		m.chats = msg.chats
		m.loading = false
		if m.grouped {
			m.rebuildGroupRows()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.showPreview && len(m.previewRawMsgs) > 0 {
			m.previewAllLines = renderPreviewMessages(m.previewRawMsgs, m.previewTextWidth())
		}
		return m, nil

	case tea.KeyMsg:
		// Help modal intercepts ALL keys
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// Preview modal intercepts ALL keys
		if m.showPreview {
			contentH := m.previewContentHeight()
			maxScroll := len(m.previewAllLines) - contentH
			if maxScroll < 0 {
				maxScroll = 0
			}
			switch msg.String() {
			case "up", "k":
				if m.previewScrollOffset > 0 {
					m.previewScrollOffset--
				}
			case "down", "j":
				if m.previewScrollOffset < maxScroll {
					m.previewScrollOffset++
				}
			case "f", "pgdown":
				m.previewScrollOffset += contentH
				if m.previewScrollOffset > maxScroll {
					m.previewScrollOffset = maxScroll
				}
			case "b", "pgup":
				m.previewScrollOffset -= contentH
				if m.previewScrollOffset < 0 {
					m.previewScrollOffset = 0
				}
			case "g":
				m.previewScrollOffset = 0
			case "G":
				m.previewScrollOffset = maxScroll
			default:
				m.showPreview = false
				m.previewScrollOffset = 0
			}
			return m, nil
		}

		// Search input intercepts all keys while capturing a query
		if m.searching {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.searching = false
			case "esc":
				m.searching = false
				m.searchQuery = ""
				m.applySearch()
			case "backspace":
				if m.searchQuery != "" {
					r := []rune(m.searchQuery)
					m.searchQuery = string(r[:len(r)-1])
					m.applySearch()
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.searchQuery += string(msg.Runes)
					m.applySearch()
				}
			}
			return m, nil
		}

		// Confirmation dialog intercepts esc before global keys
		if m.confirmDelete {
			switch msg.String() {
			case "enter":
				if m.grouped {
					// In grouped mode cursor is a groupRows index — just clamp it after rebuild
					m.postDeleteCursor = m.cursor
				} else {
					// In flat mode, land on the item right after the deleted block
					minSel := len(m.chats)
					for idx := range m.selected {
						if idx < minSel {
							minSel = idx
						}
					}
					if minSel == len(m.chats) {
						minSel = m.cursor
					}
					m.postDeleteCursor = minSel
				}
				return m, m.deleteSelectedChats()
			case "esc", "n":
				m.confirmDelete = false
				if m.autoSelected {
					m.selected = make(map[int]bool)
					m.autoSelected = false
				}
			}
			return m, nil
		}

		// Resume confirmation: a second enter launches, esc/n cancels.
		if m.confirmResume {
			switch msg.String() {
			case "enter":
				// resumeUUID/resumeDir are already set; quitting hands off to
				// main, which chdirs and runs `claude -r`.
				return m, tea.Quit
			case "esc", "n":
				m.confirmResume = false
				m.resumeUUID = ""
				m.resumeDir = ""
			}
			return m, nil
		}

		// Global keys
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "/":
			m.searching = true
			return m, nil
		}

		// Grouped mode
		if m.grouped {
			return m.updateGrouped(msg)
		}

		// Chats tab normal mode
		switch msg.String() {

		case "enter":
			if m.cursor < m.visibleLen() {
				m.armResume(m.chats[m.visibleChatIdx(m.cursor)])
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustScroll()
			}

		case "down", "j":
			if m.cursor < m.visibleLen()-1 {
				m.cursor++
				m.adjustScroll()
			}

		case "f", "pgdown":
			visibleHeight := m.visibleHeight()
			m.cursor += visibleHeight
			if m.cursor >= m.visibleLen() {
				m.cursor = m.visibleLen() - 1
			}
			m.adjustScroll()

		case "b", "pgup":
			visibleHeight := m.visibleHeight()
			m.cursor -= visibleHeight
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.adjustScroll()

		case "F":
			visibleHeight := m.visibleHeight()
			m.cursor += visibleHeight / 2
			if m.cursor >= m.visibleLen() {
				m.cursor = m.visibleLen() - 1
			}
			m.adjustScroll()

		case "B":
			visibleHeight := m.visibleHeight()
			m.cursor -= visibleHeight / 2
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.adjustScroll()

		case "g", "home":
			m.cursor = 0
			m.adjustScroll()

		case "G", "end":
			if m.visibleLen() > 0 {
				m.cursor = m.visibleLen() - 1
			}
			m.adjustScroll()

		case " ":
			// Explicit toggle — user now owns the selection.
			if m.cursor < m.visibleLen() {
				m.autoSelected = false
				idx := m.visibleChatIdx(m.cursor)
				if m.selected[idx] {
					delete(m.selected, idx)
				} else {
					m.selected[idx] = true
				}
			}

		case "a":
			// Select all / deselect all toggle, scoped to the visible (filtered) chats
			n := m.visibleLen()
			if n == 0 {
				return m, nil // Nothing to select
			}
			m.autoSelected = false
			allSelected := true
			for pos := 0; pos < n; pos++ {
				if !m.selected[m.visibleChatIdx(pos)] {
					allSelected = false
					break
				}
			}
			if allSelected {
				for pos := 0; pos < n; pos++ {
					delete(m.selected, m.visibleChatIdx(pos))
				}
			} else {
				for pos := 0; pos < n; pos++ {
					m.selected[m.visibleChatIdx(pos)] = true
				}
			}

		case "d":
			// Explicit selection wins: if anything is already selected
			// (via Space or a), delete those. Otherwise auto-select the
			// chat under the cursor for this single gesture.
			if len(m.selected) == 0 && m.cursor < m.visibleLen() {
				m.selected[m.visibleChatIdx(m.cursor)] = true
				m.autoSelected = true
			}
			if len(m.selected) > 0 {
				m.confirmDelete = true
			}

		case "m":
			// Toggle group by project
			if m.cfg != nil {
				m.cfg.GroupByProject = !m.cfg.GroupByProject
				m.grouped = m.cfg.GroupByProject
				if m.grouped {
					m.rebuildGroupRows()
				} else {
					m.groupRows = nil
				}
				m.cursor = 0
				m.scrollOffset = 0
				saveConfig(m.cfg)
			}

		case "r":
			// Refresh
			m.chats = findAllChats()
			m.recomputeSearch()
			m.selected = make(map[int]bool)
			m.autoSelected = false
			m.cursor = 0
			m.scrollOffset = 0
			m.error = ""
			m.deleted = 0
			m.copiedMsg = ""
			m.previewForUUID = ""

		case "p":
			if !m.showPreview {
				m.previewForUUID = ""
				m.loadPreviewForCursor()
				m.showPreview = true
			}

		case "c":
			// Copy UUID to clipboard
			if m.cursor < m.visibleLen() {
				uuid := m.chats[m.visibleChatIdx(m.cursor)].UUID
				if err := copyToClipboard(uuid); err != nil {
					m.error = fmt.Sprintf("Failed to copy: %v", err)
				} else {
					m.copyTimer++
					currentTimer := m.copyTimer
					m.copiedMsg = fmt.Sprintf("Chat UUID copied: %s", uuid)
					m.error = ""
					m.deleted = 0
					return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
						return clearCopiedMsg{id: currentTimer}
					})
				}
			}
		}

	case deleteCompleteMsg:
		m.deleting = false
		m.deleted = msg.count
		m.deleteTimer++
		currentTimer := m.deleteTimer
		m.chats = findAllChats()
		m.recomputeSearch()
		m.selected = make(map[int]bool)
		m.autoSelected = false
		m.scrollOffset = 0
		m.confirmDelete = false
		if m.grouped {
			m.cursor = m.postDeleteCursor
			if m.cursor >= len(m.groupRows) {
				m.cursor = len(m.groupRows) - 1
			}
		} else {
			m.cursor = m.postDeleteCursor
			if m.cursor >= m.visibleLen() {
				m.cursor = m.visibleLen() - 1
			}
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		// Clear other status messages
		m.error = ""
		m.copiedMsg = ""
		if len(m.chats) == 0 {
			return m, tea.Quit
		}
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return clearDeleteMsg{id: currentTimer}
		})

	case errMsg:
		m.deleting = false
		m.error = string(msg)

	case clearCopiedMsg:
		if msg.id == m.copyTimer {
			m.copiedMsg = ""
		}

	case clearDeleteMsg:
		if msg.id == m.deleteTimer {
			m.deleted = 0
		}
	}

	return m, nil
}

func (m *model) adjustScroll() {
	visibleHeight := m.visibleHeight()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	} else if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}
	if m.showPreview {
		m.loadPreviewForCursor()
	}
}

// previewTextWidth returns the text content width passed to glamour for word-wrapping.
func (m model) previewTextWidth() int {
	w := m.width
	if w < 40 {
		w = 40
	}
	modalW := w - 4
	if modalW > 140 {
		modalW = 140
	}
	innerW := modalW - 4 // border(1)*2 + padding(1)*2
	textW := innerW - 8  // role prefix area
	if textW < 10 {
		textW = 10
	}
	return textW
}

func (m model) previewContentHeight() int {
	h := m.height - 11 // borders(2) + header(2) + sep(2) + footer(1) + padding
	if h < 3 {
		h = 3
	}
	if h > 60 {
		h = 60
	}
	return h
}

func (m model) viewPreview() string {
	w := m.width
	h := m.height
	if w < 40 {
		w = 40
	}
	if h < 10 {
		h = 10
	}

	modalW := w - 4
	if modalW > 140 {
		modalW = 140
	}
	innerW := modalW - 4 // border(1)*2 + padding(1)*2

	contentH := m.previewContentHeight()

	// Find chat metadata from UUID
	var chat *Chat
	for i := range m.chats {
		if m.chats[i].UUID == m.previewForUUID {
			c := m.chats[i]
			chat = &c
			break
		}
	}

	var s strings.Builder

	// Header block (2 lines)
	if chat != nil {
		projColor := colorForProject(chat.Project)
		titleClean := runewidth.Truncate(chat.Title, innerW-20, "..")
		dateStr := dimStyle.Render(chat.Timestamp)
		titleStyled := accentStyle.Render(titleClean)
		titleW := lipgloss.Width(titleStyled)
		dateW := lipgloss.Width(dateStr)
		gap1 := innerW - titleW - dateW
		if gap1 < 1 {
			gap1 = 1
		}
		s.WriteString(titleStyled + strings.Repeat(" ", gap1) + dateStr + "\n")

		projStr := lipgloss.NewStyle().Foreground(projColor).Render(truncateLeft(decodeProjectPath(chat.Project), 60))
		closeHint := dimStyle.Render("any key · close")
		projW := lipgloss.Width(projStr)
		closeW := lipgloss.Width(closeHint)
		gap2 := innerW - projW - closeW
		if gap2 < 1 {
			gap2 = 1
		}
		s.WriteString(projStr + strings.Repeat(" ", gap2) + closeHint + "\n")
	} else {
		s.WriteString("\n\n")
	}

	// Separator
	s.WriteString(dimStyle.Render(strings.Repeat("─", innerW)) + "\n")

	// Content viewport
	userRoleStyle := lipgloss.NewStyle().Foreground(adaptiveColor("75", "4"))
	asstRoleStyle := lipgloss.NewStyle().Foreground(adaptiveColor("176", "5"))
	toolRoleStyle := dimStyle
	toolTextStyle := lipgloss.NewStyle().Foreground(adaptiveColor("114", "10"))
	// Continuation bar: same color as role, renders ▌ left-bar for multi-line messages
	userBarStyle := userRoleStyle
	asstBarStyle := asstRoleStyle
	toolBarStyle := toolRoleStyle
	maxText := innerW - 8
	if maxText < 10 {
		maxText = 10
	}

	start := m.previewScrollOffset
	end := start + contentH
	if end > len(m.previewAllLines) {
		end = len(m.previewAllLines)
	}

	for i := start; i < end; i++ {
		line := m.previewAllLines[i]
		// glamour already wrapped text — write as-is, no truncation
		text := line.Text
		var row string
		if line.IsFirst {
			switch line.Role {
			case "user":
				row = userRoleStyle.Render("▶ you") + "  " + text
			case "asst":
				row = asstRoleStyle.Render("◆ ai ") + "  " + text
			case "tool":
				row = toolRoleStyle.Render("$ sh ") + "  " + toolTextStyle.Render(text)
			default:
				row = "       " + text
			}
		} else {
			switch line.Role {
			case "user":
				row = userBarStyle.Render("│") + "      " + text
			case "asst":
				row = asstBarStyle.Render("│") + "      " + text
			case "tool":
				row = toolBarStyle.Render("│") + "      " + toolTextStyle.Render(text)
			default:
				row = "       " + text
			}
		}
		// Pad to innerW for stable box width
		rowW := lipgloss.Width(row)
		if rowW < innerW {
			row += strings.Repeat(" ", innerW-rowW)
		}
		s.WriteString(row + "\n")
	}
	// Pad remaining lines
	for i := end - start; i < contentH; i++ {
		s.WriteString(strings.Repeat(" ", innerW) + "\n")
	}

	// Bottom separator
	s.WriteString(dimStyle.Render(strings.Repeat("─", innerW)) + "\n")

	// Footer
	total := len(m.previewAllLines)
	scrollInfo := fmt.Sprintf("%d-%d / %d", start+1, end, total)
	if total == 0 {
		scrollInfo = "no messages"
	}
	footLeft := helpStyle.Render("↑/↓ scroll  g/G top/end  " + scrollInfo)
	footRight := helpStyle.Render("f/b page")
	footGap := innerW - lipgloss.Width(footLeft) - lipgloss.Width(footRight)
	if footGap < 1 {
		footGap = 1
	}
	s.WriteString(footLeft + strings.Repeat(" ", footGap) + footRight)

	// Wrap in rounded border box
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(adaptiveColor("59", "8")).
		Padding(0, 1).
		Render(s.String())

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("0")))
}

// viewHelp renders a modal box listing every keyboard shortcut.
func (m model) viewHelp() string {
	w := m.width
	h := m.height
	if w < 40 {
		w = 40
	}
	if h < 10 {
		h = 10
	}

	modalW := w - 4
	if modalW > 70 {
		modalW = 70
	}
	innerW := modalW - 4 // border(1)*2 + padding(1)*2

	type shortcutGroup struct {
		title string
		rows  [][2]string
	}

	groups := []shortcutGroup{
		{
			title: "Navigation",
			rows: [][2]string{
				{"↑/k  ↓/j", "move cursor"},
				{"f/PgDn  b/PgUp", "page down / up"},
				{"F  B", "half page down / up"},
				{"g/Home  G/End", "jump to top / bottom"},
			},
		},
		{
			title: "Selection",
			rows: [][2]string{
				{"space", "toggle selection"},
				{"a", "select / deselect all"},
				{"d", "delete selected (or item under cursor)"},
			},
		},
		{
			title: "View",
			rows: [][2]string{
				{"enter", "resume chat in its folder"},
				{"m", "toggle group by project"},
				{"p", "preview chat"},
				{"r", "refresh chat list"},
				{"c", "copy chat UUID"},
			},
		},
		{
			title: "Grouped mode",
			rows: [][2]string{
				{"enter", "expand/collapse header · resume chat"},
				{"e", "expand all projects"},
				{"w", "collapse all projects"},
			},
		},
		{
			title: "General",
			rows: [][2]string{
				{"/", "search by title / project"},
				{"?", "toggle this help"},
				{"q/esc/ctrl+c", "quit"},
			},
		},
	}

	var s strings.Builder
	titleStyle := accentStyle
	keyStyle := lipgloss.NewStyle().Foreground(adaptiveColor("75", "4")).Bold(true)

	s.WriteString(titleStyle.Render("Keyboard Shortcuts") + "\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", innerW)) + "\n")

	for gi, g := range groups {
		if gi > 0 {
			s.WriteString("\n")
		}
		s.WriteString(dimStyle.Render(g.title) + "\n")
		for _, row := range g.rows {
			key := keyStyle.Render(row[0])
			pad := 16 - lipgloss.Width(row[0])
			if pad < 1 {
				pad = 1
			}
			s.WriteString("  " + key + strings.Repeat(" ", pad) + row[1] + "\n")
		}
	}

	s.WriteString(dimStyle.Render(strings.Repeat("─", innerW)) + "\n")
	s.WriteString(dimStyle.Render("any key · close"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(adaptiveColor("59", "8")).
		Padding(0, 1).
		Render(s.String())

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("0")))
}

func (m model) View() string {
	if m.showHelp {
		return m.viewHelp()
	}
	if m.showPreview {
		return m.viewPreview()
	}
	if m.grouped {
		return m.viewGrouped()
	}

	if len(m.chats) == 0 && !m.loading {
		return activeTabStyle.Render("No chats found.") + "\n\nPress q to quit.\n"
	}

	// Calculate column widths based on terminal width
	width := m.width
	if width < 75 {
		width = 75 // minimum width
	}

	compact := width < compactModeWidth

	// In compact mode: hide VERSION, shorten TIMESTAMP to "MM-DD HH:MM" (11 chars)
	// Fixed cols: indicator(4) + timestamp + version + lines(6) + gaps
	var timestampWidth int
	var fixedWidth int
	if compact {
		timestampWidth = 11                     // "01-15 14:32"
		fixedWidth = 2 + timestampWidth + 5 + 6 // indicator(2) + ts + lines + gaps
	} else {
		timestampWidth = 19 // "2025-01-15 14:32:10"
		fixedWidth = 34     // indicator(2) + ts(19) + lines(5) + gaps(8)
	}

	linesWidth := 5
	remaining := width - fixedWidth
	titleWidth := remaining * 60 / 100 // 60% for title
	projectWidth := remaining - titleWidth

	if titleWidth < 30 {
		titleWidth = 30
	}
	if projectWidth < 10 {
		projectWidth = 10
	}

	var s strings.Builder

	// Header
	s.WriteString(m.renderTabBar())
	s.WriteString("\n")

	// Column headers with dark background
	headerFmt := fmt.Sprintf("  %%-*s  %%%ds  %%-%ds  %%-%ds", linesWidth, titleWidth, projectWidth)
	header := fmt.Sprintf(headerFmt, timestampWidth, "date", "lines", "title", "project")
	s.WriteString(dimStyle.Render(header))
	s.WriteString("\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)))
	s.WriteString("\n")

	// Chat list
	visibleHeight := m.visibleHeight()
	// confirmDelete dialog replaces help text, no additional space needed

	n := m.visibleLen()
	start := m.scrollOffset
	end := start + visibleHeight
	if end > n {
		end = n
	}

	if m.loading {
		s.WriteString(dimStyle.Render("  Loading chats..."))
		s.WriteString("\n")
	} else if n == 0 && m.searchQuery != "" {
		s.WriteString(dimStyle.Render("  No chats match your search."))
		s.WriteString("\n")
	}
	for pos := start; pos < end; pos++ {
		i := m.visibleChatIdx(pos)
		chat := m.chats[i]

		// Truncate fields using visual width
		var timestamp string
		if compact {
			// "2025-01-15 14:32:10" -> "01-15 14:32"
			if len(chat.Timestamp) >= 16 {
				timestamp = chat.Timestamp[5:16] // "MM-DD HH:MM"
			} else {
				timestamp = runewidth.Truncate(chat.Timestamp, timestampWidth, "")
			}
		} else {
			timestamp = runewidth.Truncate(chat.Timestamp, timestampWidth, "")
		}
		// TODO: msg column - maybe re-enable later
		// msg := fmt.Sprintf("%d", chat.MessageCount)
		// if chat.MessageCount == 0 {
		// 	msg = "-"
		// }
		var lines string
		switch {
		case chat.LineCount == 0:
			lines = "-"
		case chat.LineCount >= 10000:
			lines = fmt.Sprintf("%dk", chat.LineCount/1000)
		default:
			lines = fmt.Sprintf("%d", chat.LineCount)
		}

		titleClean := strings.NewReplacer("\n", " ").Replace(chat.Title)
		title := runewidth.Truncate(titleClean, titleWidth, "..")
		projectClean := strings.ReplaceAll(decodeProjectPath(chat.Project), "\n", " ")
		project := truncateLeft(projectClean, projectWidth-2)

		titlePad := titleWidth - runewidth.StringWidth(title)
		if titlePad < 0 {
			titlePad = 0
		}
		noTitle := strings.HasPrefix(chat.Title, "[No title]")
		projColor := colorForProject(chat.Project)
		if pos == m.cursor {
			// Cursor row: ▌ bar rendered separately with color + cursor bg
			var cursorBar string
			if m.selected[i] {
				cursorBar = lipgloss.NewStyle().Foreground(adaptiveColor("114", "10")).Background(lipgloss.Color("#2c3d3d")).Render("▌")
			} else {
				cursorBar = lipgloss.NewStyle().Foreground(adaptiveColor("73", "6")).Background(lipgloss.Color("#2c3d3d")).Render("▌")
			}
			plainContent := " " +
				fmt.Sprintf("%-*s", timestampWidth, timestamp) + "  " +
				fmt.Sprintf("%*s", linesWidth, lines) + "  " +
				title + strings.Repeat(" ", titlePad) + "  " +
				fmt.Sprintf("%-*s", projectWidth-2, project)
			contentW := runewidth.StringWidth(plainContent)
			if contentW < width-1 {
				plainContent += strings.Repeat(" ", width-1-contentW)
			}
			s.WriteString(cursorBar + cursorStyle.Render(plainContent))
		} else {
			// Normal row: individual part colors
			var indStr string
			if m.selected[i] {
				indStr = selectedStyle.Render("▌") + " "
			} else {
				indStr = "  "
			}
			tsStr := dimStyle.Render(fmt.Sprintf("%-*s", timestampWidth, timestamp))
			var linesStr string
			if chat.LineCount > 500 {
				linesStr = orangeStyle.Render(fmt.Sprintf("%*s", linesWidth, lines))
			} else {
				linesStr = dimStyle.Render(fmt.Sprintf("%*s", linesWidth, lines))
			}
			var titleRendered string
			if noTitle {
				titleRendered = noTitleStyle.Render(title) + strings.Repeat(" ", titlePad)
			} else {
				titleRendered = title + strings.Repeat(" ", titlePad)
			}
			line := indStr + tsStr + "  " + linesStr + "  " + titleRendered + "  " +
				lipgloss.NewStyle().Foreground(projColor).Render(project)
			s.WriteString(line)
		}
		s.WriteString("\n")
	}

	// Scroll indicator
	if n > visibleHeight {
		scrollInfo := fmt.Sprintf("[%d-%d/%d]", start+1, end, n)
		s.WriteString(dimStyle.Render(scrollInfo))
		s.WriteString("\n")
	}

	// Bottom separator
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)))
	s.WriteString("\n")

	// Status messages (below separator)
	if m.error != "" {
		s.WriteString(errorStyle.Render("Error: " + m.error))
		s.WriteString("\n")
	} else if m.deleted > 0 {
		s.WriteString(successStyle.Render(fmt.Sprintf("✓ Deleted %d chat(s)", m.deleted)))
		s.WriteString("\n")
	} else if m.copiedMsg != "" {
		s.WriteString(successStyle.Render("✓ " + m.copiedMsg))
		s.WriteString("\n")
	}

	// Search input line
	if m.searching || m.searchQuery != "" {
		line := accentStyle.Render("/" + m.searchQuery)
		if m.searching {
			line += "▌"
		} else {
			line += helpStyle.Render("  (esc clear · / edit)")
		}
		s.WriteString(line)
		s.WriteString("\n")
	}

	// Help / Confirmation dialog
	if m.confirmDelete {
		s.WriteString(errorStyle.Render(fmt.Sprintf("Delete %d chat(s)?", len(m.selected))))
		s.WriteString(" ")
		s.WriteString(helpStyle.Render("[ENTER=Yes] [ESC=No]"))
		s.WriteString("\n")
	} else if m.confirmResume {
		s.WriteString(accentStyle.Render("Resume this chat in its folder?"))
		s.WriteString(" ")
		s.WriteString(helpStyle.Render("[ENTER=Yes] [ESC=No]"))
		s.WriteString("\n")
	} else if compact {
		s.WriteString(helpStyle.Render("space select │ d delete │ m group │ p preview │ ? help │ q quit"))
		s.WriteString("\n")
		s.WriteString(helpStyle.Render("↑/↓ move │ f/b page │ g/G home/end"))
		s.WriteString("\n")
	} else {
		help := "↑/↓ move │ enter resume │ space select │ d delete │ m group │ p preview │ / search │ ? help │ q quit"
		s.WriteString(helpStyle.Render(help))
		s.WriteString("\n")
	}

	return s.String()
}

// updateGrouped handles key events in grouped view mode.
// cursor indexes into m.groupRows (the virtual row list).
func (m model) updateGrouped(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rowCount := len(m.groupRows)

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.adjustScrollGrouped()
		}

	case "down", "j":
		if m.cursor < rowCount-1 {
			m.cursor++
			m.adjustScrollGrouped()
		}

	case "f", "pgdown":
		m.cursor += m.visibleHeight()
		if m.cursor >= rowCount {
			m.cursor = rowCount - 1
		}
		m.adjustScrollGrouped()

	case "b", "pgup":
		m.cursor -= m.visibleHeight()
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.adjustScrollGrouped()

	case "F":
		m.cursor += m.visibleHeight() / 2
		if m.cursor >= rowCount {
			m.cursor = rowCount - 1
		}
		m.adjustScrollGrouped()

	case "B":
		m.cursor -= m.visibleHeight() / 2
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.adjustScrollGrouped()

	case "g", "home":
		m.cursor = 0
		m.adjustScrollGrouped()

	case "G", "end":
		if rowCount > 0 {
			m.cursor = rowCount - 1
		}
		m.adjustScrollGrouped()

	case "enter":
		if m.cursor < rowCount {
			row := m.groupRows[m.cursor]
			if row.isHeader {
				// Expand/collapse project header
				proj := row.project
				m.expandedProjects[proj] = !m.expandedProjects[proj]
				m.rebuildGroupRows()
				// Keep cursor on the same header
				for i, r := range m.groupRows {
					if r.isHeader && r.project == proj {
						m.cursor = i
						break
					}
				}
				m.adjustScrollGrouped()
			} else if row.chatIdx < len(m.chats) {
				// Chat row: arm resume confirmation
				m.armResume(m.chats[row.chatIdx])
			}
		}

	case " ":
		if m.cursor < rowCount {
			m.autoSelected = false
			row := m.groupRows[m.cursor]
			if row.isHeader {
				// Toggle all chats in this project
				indices := m.chatIndicesForProject(row.project)
				allSelected := true
				for _, idx := range indices {
					if !m.selected[idx] {
						allSelected = false
						break
					}
				}
				if allSelected {
					for _, idx := range indices {
						delete(m.selected, idx)
					}
				} else {
					for _, idx := range indices {
						m.selected[idx] = true
					}
				}
			} else {
				// Toggle individual chat
				chatIdx := row.chatIdx
				if m.selected[chatIdx] {
					delete(m.selected, chatIdx)
				} else {
					m.selected[chatIdx] = true
				}
			}
		}

	case "a":
		// Select all / deselect all toggle, scoped to the visible (filtered) chats
		var visIdx []int
		for i := range m.chats {
			if m.matchesSearch(i) {
				visIdx = append(visIdx, i)
			}
		}
		if len(visIdx) == 0 {
			return m, nil
		}
		m.autoSelected = false
		allSelected := true
		for _, idx := range visIdx {
			if !m.selected[idx] {
				allSelected = false
				break
			}
		}
		if allSelected {
			for _, idx := range visIdx {
				delete(m.selected, idx)
			}
		} else {
			for _, idx := range visIdx {
				m.selected[idx] = true
			}
		}

	case "d":
		// Explicit selection wins: only auto-select when nothing is selected.
		// On a project header we pick every chat in that project (works for
		// both expanded and collapsed groups). On a chat row we pick just it.
		if len(m.selected) == 0 && m.cursor < len(m.groupRows) {
			row := m.groupRows[m.cursor]
			if row.isHeader {
				for _, idx := range m.chatIndicesForProject(row.project) {
					m.selected[idx] = true
				}
			} else {
				m.selected[row.chatIdx] = true
			}
			if len(m.selected) > 0 {
				m.autoSelected = true
			}
		}
		if len(m.selected) > 0 {
			m.confirmDelete = true
		}

	case "m":
		// Toggle group by project (exit group mode)
		if m.cfg != nil {
			m.cfg.GroupByProject = false
			m.grouped = false
			m.groupRows = nil
			m.cursor = 0
			m.scrollOffset = 0
			saveConfig(m.cfg)
		}

	case "r":
		m.chats = findAllChats()
		m.recomputeSearch()
		m.selected = make(map[int]bool)
		m.autoSelected = false
		m.cursor = 0
		m.scrollOffset = 0
		m.error = ""
		m.deleted = 0
		m.copiedMsg = ""
		m.previewForUUID = ""

	case "e":
		for _, chat := range m.chats {
			m.expandedProjects[chat.Project] = true
		}
		m.rebuildGroupRows()

	case "w":
		m.expandedProjects = make(map[string]bool)
		m.rebuildGroupRows()

	case "p":
		if m.cursor < rowCount && !m.groupRows[m.cursor].isHeader {
			if !m.showPreview {
				m.previewForUUID = ""
				m.loadPreviewForCursor()
				m.showPreview = true
			}
		}

	case "c":
		if m.cursor < rowCount && !m.groupRows[m.cursor].isHeader {
			chatIdx := m.groupRows[m.cursor].chatIdx
			if chatIdx < len(m.chats) {
				uuid := m.chats[chatIdx].UUID
				if err := copyToClipboard(uuid); err != nil {
					m.error = fmt.Sprintf("Failed to copy: %v", err)
				} else {
					m.copyTimer++
					currentTimer := m.copyTimer
					m.copiedMsg = fmt.Sprintf("Chat UUID copied: %s", uuid)
					m.error = ""
					m.deleted = 0
					return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
						return clearCopiedMsg{id: currentTimer}
					})
				}
			}
		}
	}

	return m, nil
}

func (m *model) adjustScrollGrouped() {
	visibleHeight := m.visibleHeight()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	} else if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}
}

// selectedCountForProject returns how many chats in a project are selected.
func (m model) selectedCountForProject(project string) (selected, total int) {
	for i, chat := range m.chats {
		if chat.Project == project && m.matchesSearch(i) {
			total++
			if m.selected[i] {
				selected++
			}
		}
	}
	return
}

func (m model) viewGrouped() string {
	if len(m.chats) == 0 && !m.loading {
		return activeTabStyle.Render("No chats found.") + "\n\nPress q to quit.\n"
	}

	width := m.width
	if width < 75 {
		width = 75
	}

	compact := width < compactModeWidth

	// Column widths for chat rows (indented by 2 for nesting)
	var timestampWidth, fixedWidth int
	if compact {
		timestampWidth = 11
		fixedWidth = 2 + 2 + timestampWidth + 5 + 6 // indicator(2) + indent(2) + ts + lines + gaps
	} else {
		timestampWidth = 19
		fixedWidth = 36 // indicator(2) + indent(2) + ts(19) + lines(5) + gaps(8)
	}

	linesWidth := 5
	remaining := width - fixedWidth
	titleWidth := remaining * 65 / 100 // more title space since project is in header
	if titleWidth < 30 {
		titleWidth = 30
	}

	var s strings.Builder

	// Header
	s.WriteString(m.renderTabBar())
	s.WriteString("\n")

	// Column headers with dark background
	gHeaderFmt := fmt.Sprintf("     %%-*s  %%%ds  %%s", linesWidth)
	gHeader := fmt.Sprintf(gHeaderFmt, timestampWidth, "date", "lines", "title")
	s.WriteString(dimStyle.Render(gHeader))
	s.WriteString("\n")
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)))
	s.WriteString("\n")

	// Rows
	visibleHeight := m.visibleHeight()
	rowCount := len(m.groupRows)

	start := m.scrollOffset
	end := start + visibleHeight
	if end > rowCount {
		end = rowCount
	}

	if m.loading {
		s.WriteString(dimStyle.Render("  Loading chats..."))
		s.WriteString("\n")
	} else if rowCount == 0 && m.searchQuery != "" {
		s.WriteString(dimStyle.Render("  No chats match your search."))
		s.WriteString("\n")
	}
	for i := start; i < end; i++ {
		row := m.groupRows[i]

		if row.isHeader {
			// Project header row
			sel, total := m.selectedCountForProject(row.project)
			arrow := "▸"
			if m.expandedProjects[row.project] {
				arrow = "▾"
			}
			projectClean := strings.ReplaceAll(decodeProjectPath(row.project), "\n", " ")
			projColor := colorForProject(row.project)

			// Compute group stats inline
			var totalLines int
			var latestTs string
			for ci, c := range m.chats {
				if c.Project == row.project && m.matchesSearch(ci) {
					totalLines += c.LineCount
					if c.Timestamp > latestTs {
						latestTs = c.Timestamp
					}
				}
			}
			latestDate := ""
			if len(latestTs) >= 10 {
				latestDate = latestTs[:10]
			}

			if i == m.cursor {
				// Cursor row: cyan ▌ bar separately + cursor bg
				cursorBarH := lipgloss.NewStyle().Foreground(adaptiveColor("73", "6")).Background(lipgloss.Color("#2c3d3d")).Render("▌")
				leftPart := arrow + " " + projectClean
				var statsStr string
				if sel > 0 {
					statsStr = fmt.Sprintf("%d chats  │  %d sel  │  %d lines  │  latest %s", total, sel, totalLines, latestDate)
				} else {
					statsStr = fmt.Sprintf("%d chats  │  %d lines  │  latest %s", total, totalLines, latestDate)
				}
				// leftPart excludes the ▌, so account for it in width
				gap := (width - 1) - runewidth.StringWidth(leftPart) - runewidth.StringWidth(statsStr)
				if gap < 2 {
					gap = 2
				}
				plainContent := " " + leftPart + strings.Repeat(" ", gap) + statsStr
				contentW := runewidth.StringWidth(plainContent)
				if contentW < width-1 {
					plainContent += strings.Repeat(" ", width-1-contentW)
				}
				s.WriteString(cursorBarH + cursorStyle.Render(plainContent))
			} else {
				// Normal row: project-tinted background applied per-component
				bg := bgTintForProject(row.project)
				pBg := lipgloss.NewStyle().Foreground(projColor).Background(bg)
				dimBg := lipgloss.NewStyle().Foreground(adaptiveColor("59", "8")).Background(bg)
				orangeBg := lipgloss.NewStyle().Foreground(adaptiveColor("173", "11")).Background(bg)
				selBg := lipgloss.NewStyle().Bold(true).Foreground(adaptiveColor("114", "10")).Background(bg)
				spaceBg := lipgloss.NewStyle().Background(bg)

				barStr := pBg.Render("▌")
				arrowStr := pBg.Render(arrow)
				projStr := pBg.Copy().Bold(true).Render(projectClean)
				leftPart := barStr + spaceBg.Render(" ") + arrowStr + spaceBg.Render(" ") + projStr

				linesTotal := fmt.Sprintf("%s lines", formatLines(totalLines))
				var statsStr string
				if sel > 0 {
					statsStr = dimBg.Render(fmt.Sprintf("%d chats", total)) +
						dimBg.Render(" │ ") + selBg.Render(fmt.Sprintf("%d sel", sel)) +
						dimBg.Render(" │ ") + orangeBg.Render(linesTotal) +
						dimBg.Render(" │ ") + dimBg.Render("latest "+latestDate)
				} else {
					statsStr = dimBg.Render(fmt.Sprintf("%d chats", total)) +
						dimBg.Render(" │ ") + orangeBg.Render(linesTotal) +
						dimBg.Render(" │ ") + dimBg.Render("latest "+latestDate)
				}
				gap := width - lipgloss.Width(leftPart) - lipgloss.Width(statsStr)
				if gap < 2 {
					gap = 2
				}
				s.WriteString(leftPart + spaceBg.Render(strings.Repeat(" ", gap)) + statsStr)
			}
			s.WriteString("\n")
		} else {
			// Chat row (indented under project)
			chat := m.chats[row.chatIdx]
			projColor := colorForProject(row.project)

			var timestamp string
			if compact {
				if len(chat.Timestamp) >= 16 {
					timestamp = chat.Timestamp[5:16]
				} else {
					timestamp = runewidth.Truncate(chat.Timestamp, timestampWidth, "")
				}
			} else {
				timestamp = runewidth.Truncate(chat.Timestamp, timestampWidth, "")
			}

			var lines string
			switch {
			case chat.LineCount == 0:
				lines = "-"
			case chat.LineCount >= 10000:
				lines = fmt.Sprintf("%dk", chat.LineCount/1000)
			default:
				lines = fmt.Sprintf("%d", chat.LineCount)
			}

			titleClean := strings.NewReplacer("\n", " ").Replace(chat.Title)
			title := runewidth.Truncate(titleClean, titleWidth, "..")

			noTitleG := strings.HasPrefix(chat.Title, "[No title]")
			tPad := titleWidth - runewidth.StringWidth(title)
			if tPad < 0 {
				tPad = 0
			}
			// Tree connector colored in dim project color
			treeConnector := lipgloss.NewStyle().
				Foreground(projColor).
				Faint(true).
				Render("├─")
			if i == m.cursor {
				// Cursor row: ▌ bar separately, green if selected else cyan
				var cursorBarG string
				if m.selected[row.chatIdx] {
					cursorBarG = lipgloss.NewStyle().Foreground(adaptiveColor("114", "10")).Background(lipgloss.Color("#2c3d3d")).Render("▌")
				} else {
					cursorBarG = lipgloss.NewStyle().Foreground(adaptiveColor("73", "6")).Background(lipgloss.Color("#2c3d3d")).Render("▌")
				}
				plainContent := " ├─ " + fmt.Sprintf("%-*s  %*s  %-*s",
					timestampWidth, timestamp, linesWidth, lines, titleWidth, title)
				contentW := runewidth.StringWidth(plainContent)
				if contentW < width-1 {
					plainContent += strings.Repeat(" ", width-1-contentW)
				}
				s.WriteString(cursorBarG + cursorStyle.Render(plainContent))
			} else {
				// Normal row: styled parts with tree connector
				var barG string
				if m.selected[row.chatIdx] {
					barG = selectedStyle.Render("▌") + " "
				} else {
					barG = "  "
				}
				tsStr := dimStyle.Render(fmt.Sprintf("%-*s", timestampWidth, timestamp))
				var linesStr string
				if chat.LineCount > 500 {
					linesStr = orangeStyle.Render(fmt.Sprintf("%*s", linesWidth, lines))
				} else {
					linesStr = dimStyle.Render(fmt.Sprintf("%*s", linesWidth, lines))
				}
				var titleRenderedG string
				if noTitleG {
					titleRenderedG = noTitleStyle.Render(title) + strings.Repeat(" ", tPad)
				} else {
					titleRenderedG = title + strings.Repeat(" ", tPad)
				}
				line := barG + treeConnector + " " + tsStr + "  " + linesStr + "  " + titleRenderedG
				s.WriteString(line)
			}
			s.WriteString("\n")
		}
	}

	// Scroll indicator
	if rowCount > visibleHeight {
		scrollInfo := fmt.Sprintf("[%d-%d/%d]", start+1, end, rowCount)
		s.WriteString(dimStyle.Render(scrollInfo))
		s.WriteString("\n")
	}

	// Bottom separator
	s.WriteString(dimStyle.Render(strings.Repeat("─", width)))
	s.WriteString("\n")

	// Status messages
	if m.error != "" {
		s.WriteString(errorStyle.Render("Error: " + m.error))
		s.WriteString("\n")
	} else if m.deleted > 0 {
		s.WriteString(successStyle.Render(fmt.Sprintf("✓ Deleted %d chat(s)", m.deleted)))
		s.WriteString("\n")
	} else if m.copiedMsg != "" {
		s.WriteString(successStyle.Render("✓ " + m.copiedMsg))
		s.WriteString("\n")
	}

	// Search input line
	if m.searching || m.searchQuery != "" {
		line := accentStyle.Render("/" + m.searchQuery)
		if m.searching {
			line += "▌"
		} else {
			line += helpStyle.Render("  (esc clear · / edit)")
		}
		s.WriteString(line)
		s.WriteString("\n")
	}

	// Help / Confirmation dialog
	if m.confirmDelete {
		s.WriteString(errorStyle.Render(fmt.Sprintf("Delete %d chat(s)?", len(m.selected))))
		s.WriteString(" ")
		s.WriteString(helpStyle.Render("[ENTER=Yes] [ESC=No]"))
		s.WriteString("\n")
	} else if m.confirmResume {
		s.WriteString(accentStyle.Render("Resume this chat in its folder?"))
		s.WriteString(" ")
		s.WriteString(helpStyle.Render("[ENTER=Yes] [ESC=No]"))
		s.WriteString("\n")
	} else if compact {
		s.WriteString(helpStyle.Render("space select │ enter expand │ d delete │ p preview │ ? help │ q quit"))
		s.WriteString("\n")
		s.WriteString(helpStyle.Render("↑/↓ move │ f/b page │ g/G home/end"))
		s.WriteString("\n")
	} else {
		help := "↑/↓ move │ enter expand/resume │ space select │ d delete │ m ungroup │ p preview │ / search │ ? help │ q quit"
		s.WriteString(helpStyle.Render(help))
		s.WriteString("\n")
	}

	return s.String()
}

func (m model) deleteSelectedChats() tea.Cmd {
	return func() tea.Msg {
		var toDelete []Chat
		for idx := range m.selected {
			if idx < len(m.chats) {
				toDelete = append(toDelete, m.chats[idx])
			}
		}
		count, err := deleteChats(toDelete)
		if err != nil {
			return errMsg(err.Error())
		}
		return deleteCompleteMsg{count: count}
	}
}
