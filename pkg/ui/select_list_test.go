package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func press(m tea.Model, keys ...string) listModel {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m.(listModel)
}

func testItems(n int) []ListItem {
	items := make([]ListItem, n)
	for i := range items {
		items[i] = ListItem{Title: fmt.Sprintf("item %d", i), Details: []string{"detail", "• reason"}}
	}
	return items
}

func TestListStartsUnselectedAndCopiesSelectionInOrder(t *testing.T) {
	var got []int
	m := newListModel("t", testItems(5), func(sel []int) (string, error) {
		got = sel
		return "ok", nil
	})
	if len(m.selected()) != 0 {
		t.Fatal("items must start unselected")
	}

	// select item 0, skip 1, select 2 and 3, then copy; the list stays open.
	m = press(m, "space", "down", "down", "space", "down", "space", "c")
	if fmt.Sprint(got) != "[0 2 3]" {
		t.Errorf("copied %v, want [0 2 3]", got)
	}
	if m.done || m.status != "ok" {
		t.Errorf("c must keep the list open and show the status: done=%v status=%q", m.done, m.status)
	}
}

func TestListCopyWithoutSelectionDoesNotCopy(t *testing.T) {
	called := false
	m := newListModel("t", testItems(3), func([]int) (string, error) { called = true; return "", nil })
	m = press(m, "c")
	if called || !strings.Contains(m.status, "nothing selected") {
		t.Errorf("called=%v status=%q", called, m.status)
	}
	if m = press(m, "enter"); m.done {
		t.Error("enter with nothing selected must not quit")
	}
}

func TestListEnterCopiesAndQuits(t *testing.T) {
	m := newListModel("t", testItems(2), func([]int) (string, error) { return "ok", nil })
	if m = press(m, "space", "enter"); !m.done {
		t.Error("enter should copy and quit")
	}
}

func TestListCopyErrorKeepsListOpen(t *testing.T) {
	m := newListModel("t", testItems(2), func([]int) (string, error) { return "", errors.New("no clipboard") })
	m = press(m, "space", "enter")
	if m.done || !strings.Contains(m.status, "no clipboard") {
		t.Errorf("done=%v status=%q", m.done, m.status)
	}
}

func TestListAllToggleAndCancel(t *testing.T) {
	m := newListModel("t", testItems(3), nil)
	if m = press(m, "a"); len(m.selected()) != 3 {
		t.Error("a should select all")
	}
	if m = press(m, "a"); len(m.selected()) != 0 {
		t.Error("a again should select none")
	}
	if m = press(m, "ctrl+c"); !m.cancelled {
		t.Error("ctrl+c should cancel")
	}
}

func TestListScrollsToKeepCursorVisible(t *testing.T) {
	m := newListModel("t", testItems(30), nil)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = model.(listModel)

	for i := 0; i < 25; i++ {
		m = press(m, "down")
	}
	if m.offset == 0 {
		t.Fatal("list did not scroll")
	}
	out := m.render()
	if !strings.Contains(out, "item 25") {
		t.Errorf("cursor item not visible:\n%s", out)
	}
	if strings.Contains(out, "item 0\n") {
		t.Error("scrolled-away item still rendered")
	}
	if lines := strings.Count(out, "\n") + 1; lines > 20 {
		t.Errorf("rendered %d lines in a 20 line terminal", lines)
	}
}

func TestListWrapsLongDetails(t *testing.T) {
	long := strings.Repeat("word ", 40)
	m := newListModel("t", []ListItem{{Title: "x", Details: []string{long}}}, nil)
	m.width = 40
	if got := len(m.block(0)); got < 5 {
		t.Errorf("long detail was not wrapped: %d lines", got)
	}
}
