package wizard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// newTestWizard builds a wizard's form for a host web.lan, alias web, that
// uses a key, with home as the home folder.
func newTestWizard(t *testing.T, mode wizardMode, home, currentKey string) (*hostWizard, *formDriver) {
	t.Helper()
	v := hostFormValues{hostName: "web.lan", alias: "web", user: "me", port: "22", auth: authKey}
	w := newHostWizard(mode, v, home, currentKey)
	return w, newFormDriver(t, w.newForm(customHuhTheme()), w.filter)
}

func TestWizardWalksEveryPage(t *testing.T) {
	home := keyHome(t)
	w, d := newTestWizard(t, wizardAdd, home, "")
	focus := []string{d.focused()}
	for len(focus) < 20 {
		d.press(tea.KeyEnter)
		if d.form.State != huh.StateNormal {
			break
		}
		focus = append(focus, d.focused())
	}
	// No page for a key file, as the first discovered key was chosen.
	want := []string{"hostname", "alias", "user", "port", "proxyjump", "auth", "key", "save"}
	if !slices.Equal(focus, want) {
		t.Errorf("pages visited = %q, want %q", focus, want)
	}
	if d.form.State != huh.StateCompleted || !w.save {
		t.Fatalf("form state %v, save %v; want completed and saved", d.form.State, w.save)
	}
	if got, want := w.identityFile(), filepath.Join(home, ".ssh", "id_ed25519_lab"); got != want {
		t.Errorf("identity file = %q, want %q", got, want)
	}
}

func TestWizardClonePutsTheAliasFirst(t *testing.T) {
	_, d := newTestWizard(t, wizardClone, keyHome(t), "")
	if d.focused() != "alias" {
		t.Errorf("the clone wizard starts on %q, want the alias", d.focused())
	}
}

// SSHX-1: Shift+Tab goes from the key step back to the host details,
// keeping every answer.
func TestWizardGoesBackFromTheKeyStep(t *testing.T) {
	w, d := newTestWizard(t, wizardAdd, keyHome(t), "")
	d.press('u', tea.ModCtrl)
	d.typeText("db.lan")
	d.advanceTo("key")
	d.press(tea.KeyDown) // id_ed25519_work
	for _, want := range []string{"auth", "proxyjump", "port", "user", "alias", "hostname"} {
		d.press(tea.KeyTab, tea.ModShift)
		if got := d.focused(); got != want {
			t.Fatalf("back: focus is on %q, want %q", got, want)
		}
	}
	if w.hostName != "db.lan" || w.alias != "web" || w.user != "me" || w.port != "22" || w.auth != authKey {
		t.Errorf("answers after going back = %+v", w.hostFormValues)
	}
	d.advanceTo("key")
	if !strings.HasSuffix(w.keyChoice, "id_ed25519_work") {
		t.Errorf("key choice after going back and forth = %q, want id_ed25519_work", w.keyChoice)
	}
}

// SSHX-1: Esc cancels from every page, generating no key.
func TestWizardCancelsFromAnyPage(t *testing.T) {
	for _, page := range []string{"hostname", "proxyjump", "auth", "key", "custom-key", "new-key", "save"} {
		t.Run(page, func(t *testing.T) {
			home := keyHome(t)
			_, d := newTestWizard(t, wizardAdd, home, "")
			switch page {
			case "custom-key":
				d.advanceTo("key")
				d.choose(2)
			case "new-key":
				d.advanceTo("key")
				d.choose(3)
			default:
				d.advanceTo(page)
			}
			if d.focused() != page {
				t.Fatalf("focus is on %q, want %q", d.focused(), page)
			}
			d.press(tea.KeyEscape)
			if d.form.State != huh.StateAborted {
				t.Errorf("Esc on %s: form state %v, want aborted", page, d.form.State)
			}
			if _, err := os.Stat(filepath.Join(home, ".ssh", "id_ed25519_web")); err == nil {
				t.Errorf("cancelling on %s generated a key", page)
			}
		})
	}
}

func TestWizardCancelButtonCancels(t *testing.T) {
	w, d := newTestWizard(t, wizardAdd, keyHome(t), "")
	d.advanceTo("save")
	d.press(tea.KeyLeft) // Cancel
	d.press(tea.KeyEnter)
	if d.form.State != huh.StateCompleted || w.save {
		t.Errorf("form state %v, save %v; want completed without saving", d.form.State, w.save)
	}
}

func TestWizardSkipsTheKeyPagesWithoutKeyAuth(t *testing.T) {
	w, d := newTestWizard(t, wizardAdd, keyHome(t), "")
	d.advanceTo("auth")
	d.choose(1) // Password only
	if w.auth != authPassword || d.focused() != "save" {
		t.Errorf("auth %q, focus on %q; want password, then the Save page", w.auth, d.focused())
	}
	if w.identityFile() != "" {
		t.Errorf("identity file %q for password auth", w.identityFile())
	}
}

// DOP-18: going back works whatever the page holds; moving on doesn't.
func TestWizardGoesBackFromAnUnfinishedPage(t *testing.T) {
	t.Run("key file", func(t *testing.T) {
		for _, typed := range []string{"", "~/no-such-key"} {
			_, d := newTestWizard(t, wizardAdd, keyHome(t), "")
			d.advanceTo("key")
			d.choose(2) // Custom key path...
			d.typeText(typed)
			d.press(tea.KeyEnter)
			if d.focused() != "custom-key" {
				t.Fatalf("moved on from key file %q; focus is on %q", typed, d.focused())
			}
			d.press(tea.KeyTab, tea.ModShift)
			if got := d.focused(); got != "key" {
				t.Errorf("Shift+Tab from key file %q went to %q, want the key choice", typed, got)
			}
		}
	})
	t.Run("port", func(t *testing.T) {
		_, d := newTestWizard(t, wizardAdd, keyHome(t), "")
		d.advanceTo("port")
		d.press('u', tea.ModCtrl)
		d.typeText("99999")
		d.press(tea.KeyEnter)
		if d.focused() != "port" {
			t.Fatalf("moved on from port 99999; focus is on %q", d.focused())
		}
		d.press(tea.KeyTab, tea.ModShift)
		if got := d.focused(); got != "user" {
			t.Errorf("Shift+Tab from a bad port went to %q, want the user", got)
		}
	})
}

// A key file left unfinished on a page that's then hidden doesn't count.
func TestWizardIgnoresAHiddenPagesAnswer(t *testing.T) {
	home := keyHome(t)
	w, d := newTestWizard(t, wizardAdd, home, "")
	d.advanceTo("key")
	d.choose(2) // Custom key path...
	d.typeText("~/no-such-key")
	d.backTo("key")
	d.press(tea.KeyUp)
	d.press(tea.KeyEnter) // id_ed25519_work
	if d.focused() != "save" {
		t.Fatalf("after choosing a found key, focus is on %q; want the Save page", d.focused())
	}
	d.press(tea.KeyEnter)
	if d.form.State != huh.StateCompleted {
		t.Fatalf("form state %v; saving was refused:\n%s", d.form.State, d.view())
	}
	if got, want := w.identityFile(), filepath.Join(home, ".ssh", "id_ed25519_work"); got != want {
		t.Errorf("identity file = %q, want %q", got, want)
	}
}

func TestWizardSaveRefusesAnswersThatNoLongerFit(t *testing.T) {
	w, d := newTestWizard(t, wizardAdd, keyHome(t), "")
	d.advanceTo("save")
	// No page path leads here with a bad answer, as moving on checks each
	// field; Save is the last line of defence.
	w.port = "0"
	d.press(tea.KeyEnter)
	if view := d.view(); d.form.State == huh.StateCompleted || !strings.Contains(view, "Port: port must be between") {
		t.Errorf("saved answers that don't fit (state %v):\n%s", d.form.State, view)
	}
}

// DOP-19: the generated key's default path follows the alias, even after
// going back to change it.
func TestWizardNewKeyFollowsTheAlias(t *testing.T) {
	home := keyHome(t)
	w, d := newTestWizard(t, wizardAdd, home, "")
	d.advanceTo("key")
	d.choose(3) // Generate
	if view := d.view(); !strings.Contains(view, "~/.ssh/id_ed25519_web") {
		t.Fatalf("the new key page doesn't offer a key named after the alias:\n%s", view)
	}
	d.backTo("alias")
	d.press('u', tea.ModCtrl)
	d.typeText("db")
	d.advanceTo("new-key")
	if view := d.view(); !strings.Contains(view, "~/.ssh/id_ed25519_db") || strings.Contains(view, "id_ed25519_web") {
		t.Errorf("the new key page still shows the old alias:\n%s", view)
	}
	d.advanceTo("save")
	d.press(tea.KeyEnter)
	if got, want := w.identityFile(), filepath.Join(home, ".ssh", "id_ed25519_db"); d.form.State != huh.StateCompleted || got != want {
		t.Errorf("state %v, identity file %q; want completed, %q", d.form.State, got, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "id_ed25519_db")); err == nil {
		t.Errorf("the form generated the key itself; only generateKey should")
	}
}

func TestWizardOffersTheCurrentKeyFirst(t *testing.T) {
	home := keyHome(t)
	w, d := newTestWizard(t, wizardEdit, home, "~/.ssh/id_ed25519_work")
	d.advanceTo("key")
	if view := d.view(); !strings.Contains(view, "~/.ssh/id_ed25519_work (Current)") {
		t.Errorf("the current key isn't offered:\n%s", view)
	}
	d.advanceTo("save")
	if got, want := w.identityFile(), filepath.Join(home, ".ssh", "id_ed25519_work"); got != want {
		t.Errorf("identity file = %q, want the current key %q", got, want)
	}
}
