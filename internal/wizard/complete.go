package wizard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/vehkiya/sshx/internal/sshconfig"
)

// maxSuggestions bounds the suggestions for one path, so a huge folder
// doesn't slow typing down. Huh shows one at a time anyway.
const maxSuggestions = 100

// pathCompleter suggests completions for a path being typed.
type pathCompleter struct {
	homeDir, cwd string
	keyFiles     bool // offer key files (DiscoverKeys) as well as folders
	ignoreCase   bool // the filesystem ignores case, as on macOS and Windows
}

func newPathCompleter(homeDir string, keyFiles bool) pathCompleter {
	cwd, _ := os.Getwd()
	return pathCompleter{
		homeDir:    homeDir,
		cwd:        cwd,
		keyFiles:   keyFiles,
		ignoreCase: runtime.GOOS == "darwin" || runtime.GOOS == "windows",
	}
}

// suggest completes the last element of a path: each suggestion is typed
// with that element finished, so it starts with exactly what was typed, as
// Huh needs. Folders end in a separator, so completing can go on inside
// them. With keyFiles, keys come first; "~/.ssh/" leads in the home folder.
// Hidden entries show once a "." is typed.
func (c pathCompleter) suggest(typed string) []string {
	switch typed {
	case "":
		return nil
	case "~":
		return []string{"~/"}
	}
	dirPart, prefix, sep := "", typed, "/"
	if i := strings.LastIndexAny(typed, `/`+string(filepath.Separator)); i >= 0 {
		dirPart, prefix, sep = typed[:i+1], typed[i+1:], typed[i:i+1]
	}
	dir := sshconfig.ExpandHome(dirPart, c.homeDir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(c.cwd, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	isKey := map[string]bool{}
	if c.keyFiles {
		keys, _ := sshconfig.DiscoverKeys(dir)
		for _, k := range keys {
			isKey[filepath.Base(k)] = true
		}
	}
	inHome := c.keyFiles && filepath.Clean(dir) == filepath.Clean(c.homeDir)
	var first, keys, folders []string
	for _, e := range entries {
		name := e.Name()
		if !c.startsWith(name, prefix) {
			continue
		}
		if inHome && name == ".ssh" {
			first = append(first, dirPart+name+sep)
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		switch {
		case isKey[name]:
			keys = append(keys, dirPart+name)
		case isDir(filepath.Join(dir, name)):
			folders = append(folders, dirPart+name+sep)
		}
	}
	all := append(append(first, keys...), folders...)
	if len(all) > maxSuggestions {
		all = all[:maxSuggestions]
	}
	return all
}

// startsWith compares names as the filesystem does. Huh keeps the typed
// text when it accepts a suggestion, so on a case-sensitive filesystem a
// suggestion that differed in case would complete to a path that isn't there.
func (c pathCompleter) startsWith(name, prefix string) bool {
	if c.ignoreCase {
		return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
	}
	return strings.HasPrefix(name, prefix)
}

// isDir reports whether path is a folder, following a symlink.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// completion is an input that completes paths: where its text is, and what
// it suggests for that text.
type completion struct {
	value   *string
	suggest func(typed string) []string
}

// completePaths makes a wizard input complete paths as they're typed, shown
// as ghost text that Tab accepts (completeOnTab), as in a shell. Accessible
// prompts stay plain.
func (w *hostWizard) completePaths(in *huh.Input, value *string, c pathCompleter) *huh.Input {
	if accessible() {
		return in
	}
	if w.completions == nil {
		w.completions = map[string]completion{}
	}
	w.completions[in.GetKey()] = completion{value, c.suggest}
	// Huh runs the func again whenever the binding changes; it follows the
	// pointer, so every keystroke counts.
	return in.SuggestionsFunc(func() []string { return c.suggest(*value) }, value)
}

// acceptSuggestion is the key Huh accepts a suggestion with.
var acceptSuggestion = tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}

// completeOnTab makes Tab accept the suggestion a path input shows. Without
// one, Tab moves on as usual. Huh can't tell the two apart itself: Tab would
// both accept the suggestion and leave the field.
func (w *hostWizard) completeOnTab(msg tea.Msg) tea.Msg {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || k.String() != "tab" || w.form == nil {
		return msg
	}
	field := w.form.GetFocusedField()
	if field == nil {
		return msg
	}
	c, ok := w.completions[field.GetKey()]
	if !ok {
		return msg
	}
	typed := *c.value
	for _, s := range c.suggest(typed) {
		if len(s) > len(typed) && strings.HasPrefix(strings.ToLower(s), strings.ToLower(typed)) {
			return acceptSuggestion
		}
	}
	return msg
}
