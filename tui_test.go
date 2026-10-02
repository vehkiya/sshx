package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func TestModelEscHandling(t *testing.T) {
	items := []list.Item{
		HostItem{Alias: "srv1", HostName: "srv1.example.com", Port: 22},
		HostItem{Alias: "srv2", HostName: "srv2.example.com", Port: 22},
	}
	l := list.New(items, newCustomDelegate(), 80, 20)
	keys := newListKeyMap()

	m := model{
		list: l,
		keys: keys,
	}

	// 1. Esc when unfiltered should quit
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Errorf("expected tea.Quit command on esc when unfiltered, got nil")
	}
	updatedModel, ok := updated.(model)
	if !ok {
		t.Fatalf("expected updated to be model, got %T", updated)
	}
	if !updatedModel.quitting {
		t.Errorf("expected quitting to be true")
	}
	if updatedModel.action != "quit" {
		t.Errorf("expected action 'quit', got %q", updatedModel.action)
	}

	// 2. 'q' should quit
	m2 := model{list: l, keys: keys}
	updated2, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd2 == nil {
		t.Errorf("expected tea.Quit command on 'q', got nil")
	}
	if !updated2.(model).quitting || updated2.(model).action != "quit" {
		t.Errorf("expected quit on 'q'")
	}
}

func TestModelKeyActions(t *testing.T) {
	items := []list.Item{
		HostItem{Alias: "srv1", HostName: "srv1.example.com", Port: 22},
		HostItem{Alias: "srv2", HostName: "srv2.example.com", Port: 22},
	}
	l := list.New(items, newCustomDelegate(), 80, 20)
	keys := newListKeyMap()

	// 1. 'e' triggers edit-host on selected item
	m := model{list: l, keys: keys}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Errorf("expected tea.Quit command on 'e', got nil")
	}
	res := updated.(model)
	if res.action != "edit-host" || res.choice != "srv1" {
		t.Errorf("expected action 'edit-host' and choice 'srv1', got action=%q, choice=%q", res.action, res.choice)
	}

	// 2. 'E' triggers edit-config
	m2 := model{list: l, keys: keys}
	updated2, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	if cmd2 == nil {
		t.Errorf("expected tea.Quit command on 'E', got nil")
	}
	res2 := updated2.(model)
	if res2.action != "edit-config" {
		t.Errorf("expected action 'edit-config', got %q", res2.action)
	}
}

func TestModelResponsiveTabToggle(t *testing.T) {
	items := []list.Item{
		HostItem{Alias: "srv1", HostName: "srv1.example.com", Port: 22},
	}
	l := list.New(items, newCustomDelegate(), 80, 20)
	keys := newListKeyMap()

	// 1. Narrow terminal (< 100 cols): Tab toggles details view
	m := model{
		list:   l,
		keys:   keys,
		width:  80,
		height: 24,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	res := updated.(model)
	if !res.showDetails {
		t.Errorf("expected showDetails true on narrow terminal tab toggle")
	}

	// View should render details box with return hint
	viewStr := res.View()
	if viewStr == "" {
		t.Errorf("expected non-empty view in details mode")
	}

	// Tab again to switch back
	updated2, _ := res.Update(tea.KeyMsg{Type: tea.KeyTab})
	res2 := updated2.(model)
	if res2.showDetails {
		t.Errorf("expected showDetails false after second tab")
	}

	// 2. Wide terminal (>= 100 cols): Tab does not toggle (dual pane view used)
	mWide := model{
		list:   l,
		keys:   keys,
		width:  120,
		height: 30,
	}
	updatedWide, _ := mWide.Update(tea.KeyMsg{Type: tea.KeyTab})
	resWide := updatedWide.(model)
	if resWide.showDetails {
		t.Errorf("expected showDetails false on wide screen")
	}
	wideView := resWide.View()
	if wideView == "" {
		t.Errorf("expected non-empty wide view")
	}
}
