package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// ErrCancelled is returned by every interactive helper in this package when the
// user interrupts the operation (Ctrl+C or Esc).
//
// A bubbletea program puts the terminal into raw mode, so Ctrl+C arrives as a
// key press rather than as a signal: the process is NOT killed and the helper
// returns to its caller like any other call. Callers must therefore propagate
// this error and must never fall through to act on results that the cancelled
// work never filled in. Returning it up the stack is enough — Execute turns it
// into "Cancelled." and exit code 130.
var ErrCancelled = errors.New("cancelled")

// IsCancelled reports whether err is, or wraps, ErrCancelled.
func IsCancelled(err error) bool { return errors.Is(err, ErrCancelled) }

// cancellationOf maps the ways a bubbletea program can end early onto
// ErrCancelled: Run returns ErrInterrupted when its own signal handler catches
// SIGINT, and ErrProgramKilled when the program's context is cancelled.
func cancellationOf(runErr error) error {
	if errors.Is(runErr, tea.ErrInterrupted) || errors.Is(runErr, tea.ErrProgramKilled) {
		return ErrCancelled
	}
	return runErr
}

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

	// finished records that the work reported completion, so err is meaningful.
	// Quitting without it — Ctrl+C, a signal, a killed program — is a
	// cancellation, and the runners turn that into ErrCancelled.
	finished bool
	err      error

	// cancel stops the work the spinner is waiting on. It is called before
	// quitting so the child process dies with the spinner instead of outliving
	// the command that started it.
	cancel context.CancelFunc
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
		m.finished = true
		m.err = msg.err
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.InterruptMsg:
		return m.quit()
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m.quit()
		}
	}
	return m, nil
}

// quit tears down the spinner and the work behind it. finished stays false, so
// the runner reports ErrCancelled.
func (m streamModel) quit() (tea.Model, tea.Cmd) {
	m.done = true
	if m.cancel != nil {
		m.cancel()
	}
	return m, tea.Quit
}

func (m streamModel) View() string {
	if m.done {
		return ""
	}
	return m.spinner.View() + " " + m.label
}

// RunSpinner runs fn in a goroutine while displaying a spinner labelled label.
// Use this for blocking operations that produce no streamable output (e.g. az API calls).
//
// fn receives a context derived from ctx that is cancelled as soon as the user
// interrupts the spinner; build subprocesses with exec.CommandContext so they
// die with it. RunSpinner then returns ErrCancelled and fn's result is
// discarded — a cancelled call never reports success, so callers can rely on
// "err == nil" meaning fn really ran to completion.
func RunSpinner(ctx context.Context, label string, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ErrCancelled
	}

	fnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	// tea.WithContext lets a caller whose ctx is cancelled elsewhere tear the
	// spinner down too; Run then reports ErrProgramKilled, which maps to
	// ErrCancelled.
	prog := tea.NewProgram(streamModel{spinner: s, label: label, cancel: cancel}, tea.WithContext(ctx))

	go func() {
		// The result travels in the message rather than through a captured
		// variable: reading a variable the goroutine may still be writing was
		// what let a cancelled fetch look like a successful one.
		prog.Send(streamDoneMsg{err: fn(fnCtx)})
	}()

	final, runErr := prog.Run()
	if runErr != nil {
		return cancellationOf(runErr)
	}
	m, ok := final.(streamModel)
	if !ok || !m.finished {
		return ErrCancelled
	}
	return m.err
}

// RunStreaming runs cmd, streaming its output above a spinner labelled label.
// Returns the command's exit error (or nil on success).
//
// Interrupting the spinner kills cmd and returns ErrCancelled: a half-finished
// push or install is reported as cancelled, never as success.
func RunStreaming(ctx context.Context, label string, cmd *exec.Cmd) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ErrCancelled
	}

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

	started := make(chan struct{})
	killed := make(chan struct{})
	kill := func() {
		close(killed)
		// Wait for Start to have run before touching cmd.Process, otherwise an
		// interrupt in the first milliseconds would miss the child entirely.
		<-started
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}

	prog := tea.NewProgram(streamModel{spinner: s, label: label, cancel: kill}, tea.WithContext(ctx))

	go func() {
		startErr := cmd.Start()
		close(started)
		if startErr != nil {
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

		prog.Send(streamDoneMsg{err: cmd.Wait()})
	}()

	final, runErr := prog.Run()
	if runErr != nil {
		select {
		case <-killed:
		default:
			kill()
		}
		return cancellationOf(runErr)
	}
	m, ok := final.(streamModel)
	if !ok || !m.finished {
		return ErrCancelled
	}
	return m.err
}

// ---------------------------------------------------------------------------
// Interactive list picker
// ---------------------------------------------------------------------------

var (
	pickTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	pickCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	pickActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
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
			line := fmt.Sprintf("> %s", item)
			sb.WriteString(pickCursorStyle.Render(line))
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
// selected item. Cancelling (Esc / Ctrl+C / q) returns -1 and ErrCancelled, so
// a caller that only checks the error can never mistake a cancellation for a
// selection.
func PickOne(title string, items []string) (int, error) {
	m := pickModel{title: title, items: items, selected: -1}
	result, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return -1, cancellationOf(err)
	}
	final, ok := result.(pickModel)
	if !ok || !final.done || final.selected < 0 {
		return -1, ErrCancelled
	}
	return final.selected, nil
}

// ---------------------------------------------------------------------------
// Multi-select checkbox picker
// ---------------------------------------------------------------------------

var (
	checkCheckedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	checkUncheckedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	checkHintStyle2     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	previewBoxStyle     = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8")).
				Padding(0, 1)
)

type multiSelectModel struct {
	title     string
	items     []string
	checked   []bool
	cursor    int
	confirmed bool
	cancelled bool
	// preview renders the overlay body for the item at the given index.
	// nil disables the overlay and its key hint.
	preview     func(int) string
	showPreview bool
}

func newMultiSelectModel(title string, items []string, preChecked []bool, preview func(int) string) multiSelectModel {
	checked := make([]bool, len(items))
	for i := range checked {
		if i < len(preChecked) {
			checked[i] = preChecked[i]
		}
	}
	return multiSelectModel{title: title, items: items, checked: checked, preview: preview}
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
		case "p":
			if m.preview != nil && len(m.items) > 0 {
				m.showPreview = !m.showPreview
			}
		case "enter":
			// While the overlay is open, enter closes it rather than confirming —
			// reading a preview should never be one keystroke away from deleting.
			if m.showPreview {
				m.showPreview = false
				return m, nil
			}
			m.confirmed = true
			return m, tea.Quit
		case "esc", "q":
			if m.showPreview {
				m.showPreview = false
				return m, nil
			}
			m.cancelled = true
			return m, tea.Quit
		case "ctrl+c":
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

	// The overlay takes the place of the list so its height stays bounded
	// regardless of how many branches are listed.
	if m.showPreview {
		sb.WriteString(previewBoxStyle.Render(m.preview(m.cursor)))
		sb.WriteString("\n\n")
		sb.WriteString(checkHintStyle2.Render("↑/↓  other item  •  space  toggle  •  p/esc  close preview"))
		return sb.String()
	}

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
	hint := "↑/↓  move  •  space  toggle  •  a  all/none  •  enter  confirm  •  esc  cancel"
	if m.preview != nil {
		hint = "↑/↓  move  •  space  toggle  •  a  all/none  •  p  preview  •  enter  confirm  •  esc  cancel"
	}
	sb.WriteString(checkHintStyle2.Render(hint))
	return sb.String()
}

// MultiSelect shows a checkbox list and returns the indices of checked items.
// preChecked sets the initial checked state for each item (nil = all unchecked).
// Cancelling returns ErrCancelled; confirming with nothing checked returns an
// empty selection and a nil error, so the two cases stay distinguishable.
func MultiSelect(title string, items []string, preChecked []bool) ([]int, error) {
	return MultiSelectWithPreview(title, items, preChecked, nil)
}

// MultiSelectWithPreview is MultiSelect with a "p"-toggled overlay showing preview(i)
// for the item under the cursor. A nil preview behaves exactly like MultiSelect.
func MultiSelectWithPreview(title string, items []string, preChecked []bool, preview func(int) string) ([]int, error) {
	m := newMultiSelectModel(title, items, preChecked, preview)
	result, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, cancellationOf(err)
	}
	final, ok := result.(multiSelectModel)
	if !ok || !final.confirmed {
		return nil, ErrCancelled
	}
	selected := []int{}
	for i, c := range final.checked {
		if c {
			selected = append(selected, i)
		}
	}
	return selected, nil
}

// ---------------------------------------------------------------------------
// Multi-line text editor
// ---------------------------------------------------------------------------

type editModel struct {
	title     string
	area      textarea.Model
	confirmed bool
	cancelled bool
}

func (m editModel) Init() tea.Cmd { return textarea.Blink }

func (m editModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.area.SetWidth(max(msg.Width-2, 20))
		m.area.SetHeight(max(msg.Height-5, 3))
		return m, nil
	case tea.InterruptMsg:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+s":
			m.confirmed = true
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.area, cmd = m.area.Update(msg)
	return m, cmd
}

func (m editModel) View() string {
	if m.confirmed || m.cancelled {
		return ""
	}
	return pickTitleStyle.Render(m.title) + "\n\n" + m.area.View() + "\n" +
		pickHintStyle.Render("ctrl+s  save  •  esc  cancel")
}

// EditText opens a full-screen multi-line editor pre-filled with initial and
// returns the edited text. Cancelling (Esc / Ctrl+C) returns ErrCancelled.
func EditText(title, initial string) (string, error) {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.SetWidth(80)
	ta.SetHeight(20)
	ta.SetValue(initial)
	ta.Focus()

	result, err := tea.NewProgram(editModel{title: title, area: ta}, tea.WithAltScreen()).Run()
	if err != nil {
		return "", cancellationOf(err)
	}
	final, ok := result.(editModel)
	if !ok || !final.confirmed {
		return "", ErrCancelled
	}
	return final.area.Value(), nil
}
