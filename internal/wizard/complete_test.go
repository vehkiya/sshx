package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// formDriver feeds keys to a Huh form the way a terminal would, running the
// commands each key returns so focus moves as it does for real.
type formDriver struct {
	t    *testing.T
	form *huh.Form
	// filter sees every message before the form, as the wizard's
	// tea.WithFilter does when it runs in a terminal.
	filter func(tea.Model, tea.Msg) tea.Msg
}

func newFormDriver(t *testing.T, form *huh.Form, filter func(tea.Model, tea.Msg) tea.Msg) *formDriver {
	t.Helper()
	d := &formDriver{t: t, form: form, filter: filter}
	d.feed(form.Init())
	return d
}

func (d *formDriver) feed(cmd tea.Cmd) {
	queue := runCmd(cmd)
	for i := 0; len(queue) > 0 && i < 200; i++ {
		msg := queue[0]
		queue = queue[1:]
		if d.filter != nil {
			msg = d.filter(nil, msg) // the wizard's filter doesn't look at the model
		}
		model, next := d.form.Update(msg)
		d.form = model.(*huh.Form)
		queue = append(queue, runCmd(next)...)
	}
}

// runCmd runs a command and returns the messages it produces, unpacking
// batches. Timers, like the cursor blink, are skipped.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(50 * time.Millisecond):
		return nil
	}
	if msg == nil {
		return nil
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := 0; i < v.Len(); i++ {
			out = append(out, runCmd(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func (d *formDriver) typeText(text string) {
	for _, r := range text {
		d.feed(func() tea.Msg { return tea.KeyPressMsg{Code: r, Text: string(r)} })
	}
}

func (d *formDriver) press(code rune, mod ...tea.KeyMod) {
	k := tea.KeyPressMsg{Code: code}
	for _, m := range mod {
		k.Mod |= m
	}
	d.feed(func() tea.Msg { return k })
}

func (d *formDriver) focused() string {
	if f := d.form.GetFocusedField(); f != nil {
		return f.GetKey()
	}
	return ""
}

// advanceTo presses Enter until the form is on the field with the given key.
func (d *formDriver) advanceTo(key string) {
	d.t.Helper()
	for i := 0; i < 20 && d.focused() != key; i++ {
		d.press(tea.KeyEnter)
	}
	if got := d.focused(); got != key {
		d.t.Fatalf("pressing Enter never reached %q; focus is on %q", key, got)
	}
}

// backTo presses Shift+Tab until the form is on the field with the given key.
func (d *formDriver) backTo(key string) {
	d.t.Helper()
	for i := 0; i < 20 && d.focused() != key; i++ {
		d.press(tea.KeyTab, tea.ModShift)
	}
	if got := d.focused(); got != key {
		d.t.Fatalf("pressing Shift+Tab never reached %q; focus is on %q", key, got)
	}
}

// choose picks the option n places below the current one in a select.
func (d *formDriver) choose(n int) {
	for range n {
		d.press(tea.KeyDown)
	}
	d.press(tea.KeyEnter)
}

// view is what the form shows on a tall terminal, without styling.
func (d *formDriver) view() string {
	d.feed(func() tea.Msg { return tea.WindowSizeMsg{Width: 100, Height: 60} })
	return ansi.Strip(d.form.View())
}

// keyHome makes a home folder with two keys and other files in ~/.ssh.
func keyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, f := range []string{".ssh/id_ed25519_lab", ".ssh/id_ed25519_lab.pub", ".ssh/id_ed25519_work", ".ssh/config", ".ssh/known_hosts"} {
		path := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "projects", "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCustomKeyPathCompletes(t *testing.T) {
	w, d := newTestWizard(t, wizardAdd, keyHome(t), "")
	d.advanceTo("key")
	d.choose(2) // id_ed25519_lab, id_ed25519_work, Custom key path...
	if d.focused() != "custom-key" {
		t.Fatalf("choosing a custom key leads to %q, want its page", d.focused())
	}

	// Tab accepts the first matching key and stays on the page.
	d.typeText("~/.ssh/id_")
	d.press(tea.KeyTab)
	if w.customKey != "~/.ssh/id_ed25519_lab" || d.focused() != "custom-key" {
		t.Fatalf("~/.ssh/id_ + Tab: %q, focus on %q; want ~/.ssh/id_ed25519_lab, still asking", w.customKey, d.focused())
	}
	if view := d.view(); !strings.Contains(view, "tab complete") {
		t.Errorf("the help doesn't say Tab completes:\n%s", view)
	}
	// With nothing left to complete, Tab moves on, as it does in any other input.
	d.press(tea.KeyTab)
	if w.customKey != "~/.ssh/id_ed25519_lab" || d.focused() != "save" {
		t.Errorf("Tab on a complete path: %q, focus on %q; want it unchanged, then the Save page", w.customKey, d.focused())
	}
}

func TestNewKeyPathCompletesFolders(t *testing.T) {
	w, d := newTestWizard(t, wizardAdd, keyHome(t), "")
	d.advanceTo("key")
	d.choose(3) // id_ed25519_lab, id_ed25519_work, Custom key path..., Generate
	if d.focused() != "new-key" {
		t.Fatalf("choosing to generate a key leads to %q, want its page", d.focused())
	}
	d.typeText("~/pro")
	d.press(tea.KeyTab)
	if w.newKey != "~/projects/" {
		t.Fatalf("~/pro + Tab = %q, want ~/projects/", w.newKey)
	}
	d.press(tea.KeyTab)
	if w.newKey != "~/projects/keys/" {
		t.Errorf("~/projects/ + Tab = %q, want ~/projects/keys/", w.newKey)
	}
}

func TestPathSuggestions(t *testing.T) {
	home := keyHome(t)
	for _, dir := range []string{"projects/Apps", "projects/.git", "src"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "projects", "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	folders := pathCompleter{homeDir: home, cwd: home}
	keys := pathCompleter{homeDir: home, cwd: home, keyFiles: true}
	sep := string(filepath.Separator)

	cases := []struct {
		c     pathCompleter
		typed string
		want  []string
	}{
		{folders, "", nil},
		{folders, "~", []string{"~/"}},
		{folders, "~/projects/", []string{"~/projects/Apps/", "~/projects/keys/"}}, // no hidden folders or files
		{folders, "~/projects/a", nil},                                             // case counts
		{folders, "~/projects/.", []string{"~/projects/.git/"}},
		{folders, "pro", []string{"projects/"}}, // relative to the current folder; "/" when none was typed
		{folders, filepath.Join(home, "sr"), []string{filepath.Join(home, "src") + sep}},
		{folders, "~/nowhere/", nil},
		{keys, "~/", []string{"~/.ssh/", "~/projects/", "~/src/"}},
		{keys, "~/.ssh/", []string{"~/.ssh/id_ed25519_lab", "~/.ssh/id_ed25519_work"}}, // not config, known_hosts or .pub
		{keys, "~/.ssh/id_ed25519_w", []string{"~/.ssh/id_ed25519_work"}},
	}
	for _, tc := range cases {
		if got := tc.c.suggest(tc.typed); !slices.Equal(got, tc.want) {
			t.Errorf("suggest(%q, keyFiles %v) = %q, want %q", tc.typed, tc.c.keyFiles, got, tc.want)
		}
	}

	folders.ignoreCase = true
	if got := folders.suggest("~/projects/a"); !slices.Equal(got, []string{"~/projects/Apps/"}) {
		t.Errorf("ignoring case: %q", got)
	}

	for i := range maxSuggestions + 20 {
		if err := os.MkdirAll(filepath.Join(home, "many", fmt.Sprintf("d%03d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if got := folders.suggest("~/many/d"); len(got) != maxSuggestions {
		t.Errorf("a huge folder gave %d suggestions, want %d", len(got), maxSuggestions)
	}
}
