package ui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Scrolling multi-line checklist
// ---------------------------------------------------------------------------

// ListItem is one entry of SelectList: a headline and any number of detail
// lines shown below it. Detail lines are wrapped to the terminal width, so
// nothing is cut off.
type ListItem struct {
	Title   string
	Details []string
}

const (
	listHeaderLines = 2 // title + blank
	listFooterLines = 3 // blank + selection/status + key hints
	listIndent      = 6 // cursor and checkbox column
	listDefaultW    = 80
	listDefaultH    = 24
)

type listModel struct {
	title   string
	items   []ListItem
	checked []bool
	cursor  int
	offset  int // first visible item
	width   int
	height  int

	// copy receives the indices of the checked items, in list order, and
	// returns the text to show as status.
	copy   func(selected []int) (string, error)
	status string

	done      bool
	cancelled bool
}

func newListModel(title string, items []ListItem, copy func([]int) (string, error)) listModel {
	return listModel{
		title: title, items: items, checked: make([]bool, len(items)),
		width: listDefaultW, height: listDefaultH, copy: copy,
	}
}

func (m listModel) Init() tea.Cmd { return nil }

func (m listModel) selected() []int {
	sel := []int{}
	for i, c := range m.checked {
		if c {
			sel = append(sel, i)
		}
	}
	return sel
}

// block returns the rendered lines of item i, including its blank separator.
func (m listModel) block(i int) []string {
	it := m.items[i]
	w := max(m.width, 20)

	box := checkUncheckedStyle.Render("[ ]")
	if m.checked[i] {
		box = checkCheckedStyle.Render("[✓]")
	}
	title := pickNormalStyle.Render(ansi.Truncate(it.Title, w-listIndent, "…"))
	marker := "  "
	if i == m.cursor {
		marker = pickCursorStyle.Render("▶ ")
		title = pickActiveStyle.Render(ansi.Truncate(it.Title, w-listIndent, "…"))
	}
	lines := []string{marker + box + " " + title}

	pad := strings.Repeat(" ", listIndent)
	for _, d := range it.Details {
		for _, l := range strings.Split(ansi.Wrap(d, w-listIndent, " "), "\n") {
			lines = append(lines, pad+checkHintStyle2.Render(l))
		}
	}
	return append(lines, "")
}

func (m listModel) available() int {
	return max(m.height-listHeaderLines-listFooterLines, 4)
}

// scrollToCursor moves offset so the cursor item is fully visible.
func (m *listModel) scrollToCursor() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.offset < m.cursor {
		used := 0
		for i := m.offset; i <= m.cursor; i++ {
			used += len(m.block(i))
		}
		if used <= m.available() {
			break
		}
		m.offset++
	}
}

func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
	case tea.InterruptMsg:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyPressMsg:
		m.status = ""
		last := len(m.items) - 1
		switch msg.String() {
		case "up", "k":
			m.cursor = max(m.cursor-1, 0)
		case "down", "j":
			m.cursor = min(m.cursor+1, last)
		case "pgup":
			m.cursor = max(m.cursor-5, 0)
		case "pgdown":
			m.cursor = min(m.cursor+5, last)
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = max(last, 0)
		case "space":
			if len(m.items) > 0 {
				m.checked[m.cursor] = !m.checked[m.cursor]
			}
		case "a":
			// Same rule as the other pickers: anything unchecked checks all,
			// otherwise uncheck all.
			any := false
			for _, c := range m.checked {
				any = any || !c
			}
			for i := range m.checked {
				m.checked[i] = any
			}
		case "c", "enter":
			sel := m.selected()
			if len(sel) == 0 {
				m.status = "nothing selected: press space on an item first"
				break
			}
			text, err := m.copy(sel)
			if err != nil {
				m.status = "copy failed: " + err.Error()
				break
			}
			m.status = text
			if msg.String() == "enter" {
				m.done = true
				return m, tea.Quit
			}
		case "esc", "q":
			m.done = true
			return m, tea.Quit
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
		m.scrollToCursor()
	}
	return m, nil
}

func (m listModel) render() string {
	if m.done || m.cancelled {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(pickTitleStyle.Render(m.title))
	sb.WriteString("\n\n")

	used := 0
	for i := m.offset; i < len(m.items); i++ {
		b := m.block(i)
		if used+len(b) > m.available() && used > 0 {
			break
		}
		sb.WriteString(strings.Join(b, "\n"))
		sb.WriteString("\n")
		used += len(b)
	}
	// Pad so the footer stays at the bottom while the list is short.
	sb.WriteString(strings.Repeat("\n", max(m.available()-used, 0)))

	n := len(m.selected())
	line := fmt.Sprintf("%d of %d selected", n, len(m.items))
	if m.status != "" {
		line += "  •  " + checkCheckedStyle.Render(m.status)
	}
	sb.WriteString("\n" + line + "\n")
	sb.WriteString(checkHintStyle2.Render(
		"↑/↓ move  •  space select  •  a all/none  •  c copy  •  enter copy & quit  •  q quit"))
	return sb.String()
}

func (m listModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// SelectList shows items as a scrolling checklist, all unchecked. Pressing c
// calls copy with the indices of the checked items and shows the text it
// returns as status; enter does the same and quits. q or Esc quit without
// error, Ctrl+C returns ErrCancelled.
func SelectList(title string, items []ListItem, copy func(selected []int) (string, error)) error {
	result, err := tea.NewProgram(newListModel(title, items, copy)).Run()
	if err != nil {
		return cancellationOf(err)
	}
	if m, ok := result.(listModel); ok && m.cancelled {
		return ErrCancelled
	}
	return nil
}

// CopyToClipboard puts text on the system clipboard.
func CopyToClipboard(text string) error { return clipboard.WriteAll(text) }

// IsInteractive reports whether stdin and stdout are both terminals, i.e.
// whether a picker can be shown.
func IsInteractive() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		fi, err := f.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}
