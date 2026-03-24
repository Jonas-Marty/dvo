package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Styled printers
// ---------------------------------------------------------------------------

// Printer is a styled line printer.
type Printer struct {
	style lipgloss.Style
}

func (p Printer) Println(a ...interface{}) {
	fmt.Println(p.style.Render(fmt.Sprint(a...)))
}

func (p Printer) Printf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	suffix := ""
	if strings.HasSuffix(s, "\n") {
		s = strings.TrimSuffix(s, "\n")
		suffix = "\n"
	}
	fmt.Print(p.style.Render(s) + suffix)
}

var (
	Success = Printer{style: lipgloss.NewStyle().Foreground(lipgloss.Color("2"))} // green
	Error   = Printer{style: lipgloss.NewStyle().Foreground(lipgloss.Color("1"))} // red
	Warning = Printer{style: lipgloss.NewStyle().Foreground(lipgloss.Color("3"))} // yellow
	Info    = Printer{style: lipgloss.NewStyle().Foreground(lipgloss.Color("6"))} // cyan
)

// OpenBrowser opens the given URL in the default browser.
// On Windows uses `cmd /c start`, on macOS `open`, on Linux `xdg-open`.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ---------------------------------------------------------------------------
// Streaming spinner
// ---------------------------------------------------------------------------
//
// RunStreaming runs cmd and streams its combined stdout+stderr output
// line-by-line *above* a bubbletea spinner that animates below with label.
// When piping to a non-tty, git suppresses its \r progress lines, so output
// is clean newline-delimited text.
//
// Falls back to plain inherited output if the OS pipe cannot be created.

type lineMsg string
type streamDoneMsg struct{ err error }

type streamModel struct {
	spinner spinner.Model
	label   string
	done    bool
}

func (m streamModel) Init() tea.Cmd { return m.spinner.Tick }

func (m streamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case lineMsg:
		// tea.Println prints the line above the current spinner line,
		// permanently scrolling it into the terminal history.
		return m, tea.Println(string(msg))
	case streamDoneMsg:
		m.done = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m streamModel) View() string {
	if m.done {
		return ""
	}
	return m.spinner.View() + " " + m.label
}

// RunSpinner runs fn in a goroutine while displaying a spinner labelled label.
// Use this for blocking operations that produce no streamable output (e.g. az API calls).
func RunSpinner(label string, fn func() error) error {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	type doneMsg struct{ err error }

	prog := tea.NewProgram(streamModel{spinner: s, label: label})

	var fnErr error
	go func() {
		fnErr = fn()
		prog.Send(streamDoneMsg{err: fnErr})
	}()

	if _, runErr := prog.Run(); runErr != nil {
		return runErr
	}
	return fnErr
}

// RunStreaming runs cmd, streaming its output above a spinner labelled label.
// Returns the command's exit error (or nil on success).
func RunStreaming(label string, cmd *exec.Cmd) error {
	// Create a pipe; both ends will be replaced by the OS pipe.
	pr, pw, err := os.Pipe()
	if err != nil {
		// Fallback: just run with inherited I/O.
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	prog := tea.NewProgram(streamModel{spinner: s, label: label})

	var cmdErr error
	go func() {
		if startErr := cmd.Start(); startErr != nil {
			pw.Close()
			pr.Close()
			prog.Send(streamDoneMsg{err: startErr})
			return
		}
		// Close the write-end in the parent process so that scanner sees EOF
		// once the child exits.
		pw.Close()

		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			prog.Send(lineMsg(scanner.Text()))
		}
		pr.Close()

		cmdErr = cmd.Wait()
		prog.Send(streamDoneMsg{err: cmdErr})
	}()

	if _, runErr := prog.Run(); runErr != nil {
		return runErr
	}
	return cmdErr
}

// ---------------------------------------------------------------------------
// Interactive list picker
// ---------------------------------------------------------------------------

var (
	pickTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	pickCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	pickActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	pickNormalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	pickHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type pickModel struct {
	title    string
	items    []string
	cursor   int
	selected int
	done     bool
}

func (m pickModel) Init() tea.Cmd { return nil }

func (m pickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.cursor
			m.done = true
			return m, tea.Quit
		case "ctrl+c", "esc", "q":
			m.selected = -1
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickModel) View() string {
	if m.done {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(pickTitleStyle.Render(m.title))
	sb.WriteString("\n\n")
	for i, item := range m.items {
		if i == m.cursor {
			sb.WriteString(pickCursorStyle.Render("▶ "))
			sb.WriteString(pickActiveStyle.Render(item))
		} else {
			sb.WriteString("  ")
			sb.WriteString(pickNormalStyle.Render(item))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(pickHintStyle.Render("↑/↓ or j/k to move  •  enter to select  •  esc to cancel"))
	return sb.String()
}

// PickOne shows an interactive list and returns the 0-based index of the
// selected item, or -1 if the user cancels (Esc / Ctrl+C / q).
func PickOne(title string, items []string) (int, error) {
	m := pickModel{title: title, items: items, selected: -1}
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return -1, err
	}
	return result.(pickModel).selected, nil
}

// ---------------------------------------------------------------------------
// Multi-select checkbox picker
// ---------------------------------------------------------------------------

var (
	checkCheckedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	checkUncheckedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	checkHintStyle2     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type multiSelectModel struct {
	title     string
	items     []string
	checked   []bool
	cursor    int
	confirmed bool
	cancelled bool
}

func newMultiSelectModel(title string, items []string, preChecked []bool) multiSelectModel {
	checked := make([]bool, len(items))
	for i := range checked {
		if i < len(preChecked) {
			checked[i] = preChecked[i]
		}
	}
	return multiSelectModel{title: title, items: items, checked: checked}
}

func (m multiSelectModel) Init() tea.Cmd { return nil }

func (m multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ":
			m.checked[m.cursor] = !m.checked[m.cursor]
		case "a":
			// Toggle all: if any unchecked, check all; otherwise uncheck all.
			anyUnchecked := false
			for _, c := range m.checked {
				if !c {
					anyUnchecked = true
					break
				}
			}
			for i := range m.checked {
				m.checked[i] = anyUnchecked
			}
		case "enter":
			m.confirmed = true
			return m, tea.Quit
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m multiSelectModel) View() string {
	if m.confirmed || m.cancelled {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(pickTitleStyle.Render(m.title))
	sb.WriteString("\n\n")
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = pickCursorStyle.Render("▶ ")
		} else {
			cursor = "  "
		}
		var checkbox string
		if m.checked[i] {
			checkbox = checkCheckedStyle.Render("[✓]")
		} else {
			checkbox = checkUncheckedStyle.Render("[ ]")
		}
		label := pickNormalStyle.Render(item)
		if i == m.cursor {
			label = pickActiveStyle.Render(item)
		}
		sb.WriteString(cursor + checkbox + " " + label + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(checkHintStyle2.Render("↑/↓  move  •  space  toggle  •  a  all/none  •  enter  confirm  •  esc  cancel"))
	return sb.String()
}

// MultiSelect shows a checkbox list and returns the indices of checked items.
// preChecked sets the initial checked state for each item (nil = all unchecked).
// Returns nil, nil if the user cancels.
func MultiSelect(title string, items []string, preChecked []bool) ([]int, error) {
	m := newMultiSelectModel(title, items, preChecked)
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	final := result.(multiSelectModel)
	if final.cancelled {
		return nil, nil
	}
	var selected []int
	for i, c := range final.checked {
		if c {
			selected = append(selected, i)
		}
	}
	return selected, nil
}
