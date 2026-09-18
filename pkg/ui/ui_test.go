package ui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// The spinner model decides whether a command may act on the work's results.
// Only a streamDoneMsg means the work actually finished; every other way out is
// a cancellation, and the runners turn "not finished" into ErrCancelled. These
// tests pin that down: an interrupted "dvo branch create" once fell through to
// `git checkout -b task/-16281` because a cancelled fetch looked successful.

func newTestModel(cancel func()) streamModel {
	return streamModel{spinner: spinner.New(), label: "test", cancel: cancel}
}

func TestStreamModelCtrlCIsNotFinished(t *testing.T) {
	cancelled := false
	m, _ := newTestModel(func() { cancelled = true }).Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	got := m.(streamModel)
	if got.finished {
		t.Error("Ctrl+C marked the work as finished; the runner would report success")
	}
	if !cancelled {
		t.Error("Ctrl+C did not cancel the work's context, so the child process outlives the spinner")
	}
}

func TestStreamModelInterruptIsNotFinished(t *testing.T) {
	cancelled := false
	m, _ := newTestModel(func() { cancelled = true }).Update(tea.InterruptMsg{})

	got := m.(streamModel)
	if got.finished {
		t.Error("an interrupt marked the work as finished")
	}
	if !cancelled {
		t.Error("an interrupt did not cancel the work's context")
	}
}

func TestStreamModelDoneCarriesError(t *testing.T) {
	want := errors.New("az failed")
	m, _ := newTestModel(nil).Update(streamDoneMsg{err: want})

	got := m.(streamModel)
	if !got.finished {
		t.Fatal("completion was not recorded")
	}
	if !errors.Is(got.err, want) {
		t.Errorf("err = %v, want %v", got.err, want)
	}
}

func TestStreamModelDoneWithoutError(t *testing.T) {
	m, _ := newTestModel(nil).Update(streamDoneMsg{})

	got := m.(streamModel)
	if !got.finished || got.err != nil {
		t.Errorf("finished = %v, err = %v; want true, nil", got.finished, got.err)
	}
}

func TestCancellationOf(t *testing.T) {
	other := errors.New("boom")
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"interrupted", tea.ErrInterrupted, ErrCancelled},
		{"killed", tea.ErrProgramKilled, ErrCancelled},
		{"wrapped interrupt", fmt.Errorf("run: %w", tea.ErrInterrupted), ErrCancelled},
		{"unrelated error is passed through", other, other},
		{"nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cancellationOf(tt.in); !errors.Is(got, tt.want) && got != tt.want {
				t.Errorf("cancellationOf(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsCancelledUnwraps(t *testing.T) {
	if !IsCancelled(fmt.Errorf("fetching work item: %w", ErrCancelled)) {
		t.Error("a wrapped ErrCancelled was not recognised; Execute would print it as a failure")
	}
	if IsCancelled(errors.New("az boards work-item show failed")) {
		t.Error("an ordinary error was treated as a cancellation")
	}
}
