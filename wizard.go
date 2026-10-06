package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Authentication strategies offered by the host wizards.
const (
	authKey      = "key"
	authPassword = "password"
	authDefault  = "default"
)

// remoteAppendKey installs a public key read from stdin into authorized_keys,
// skipping duplicates. It is the fallback when ssh-copy-id is unavailable.
const remoteAppendKey = `exec sh -c 'umask 077; mkdir -p ~/.ssh && k=$(cat) && touch ~/.ssh/authorized_keys && { grep -qxF "$k" ~/.ssh/authorized_keys || printf "%s\n" "$k" >> ~/.ssh/authorized_keys; }'`

// customHuhTheme returns an aligned Lip Gloss theme matching the sshx design system.
// The palette is the same on dark and light backgrounds.
func customHuhTheme() huh.Theme {
	return huh.ThemeFunc(huhStyles)
}

func huhStyles(isDark bool) *huh.Styles {
	t := huh.ThemeBase(isDark)

	// Left border indicator on focused fields
	t.Focused.Base = t.Focused.Base.BorderForeground(colorPurple)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle().Bold(true).Foreground(colorCoral)
	t.Focused.Description = lipgloss.NewStyle().Foreground(colorDim)
	t.Focused.Directory = lipgloss.NewStyle().Foreground(colorCyan)

	// Validation errors
	t.Focused.ErrorIndicator = lipgloss.NewStyle().Bold(true).Foreground(colorRed).SetString(" ✘")
	t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(colorRed)

	// Select / Options
	t.Focused.SelectSelector = lipgloss.NewStyle().Bold(true).Foreground(colorCoral).SetString("› ")
	t.Focused.NextIndicator = lipgloss.NewStyle().Foreground(colorCoral).MarginLeft(1).SetString("→")
	t.Focused.PrevIndicator = lipgloss.NewStyle().Foreground(colorCoral).MarginRight(1).SetString("←")
	t.Focused.Option = lipgloss.NewStyle().Foreground(colorLightGray)

	// Multi-select / Checkboxes
	t.Focused.MultiSelectSelector = lipgloss.NewStyle().Bold(true).Foreground(colorCoral).SetString("› ")
	t.Focused.SelectedOption = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Bold(true).Foreground(colorGreen).SetString("✔ ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(colorDim).SetString("• ")
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colorDim)

	// Confirm buttons
	button := lipgloss.NewStyle().
		Padding(0, 2).
		MarginRight(1).
		Bold(true)
	t.Focused.FocusedButton = button.Foreground(colorWhite).Background(colorPurple)
	t.Focused.BlurredButton = button.Foreground(colorDim).Background(colorDarkGray)

	// Text Inputs
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colorCyan)
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Bold(true).Foreground(colorCyan).SetString("› ")
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(colorLightGray)

	// Blurred field styles (when navigating between inputs in a group)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = lipgloss.NewStyle().Foreground(colorDim)
	t.Blurred.Description = lipgloss.NewStyle().Foreground(colorDim)
	t.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(colorDim).SetString("  ")
	t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(colorLightGray)
	t.Blurred.Option = lipgloss.NewStyle().Foreground(colorDim)

	// Group Title & Description
	t.Group.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPurple).
		MarginBottom(1)
	t.Group.Description = lipgloss.NewStyle().
		Foreground(colorDim).
		MarginBottom(1)

	// Help styles
	t.Help.ShortKey = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	t.Help.ShortDesc = lipgloss.NewStyle().Foreground(colorDim)
	t.Help.ShortSeparator = lipgloss.NewStyle().Foreground(colorSeparator)
	t.Help.FullKey = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	t.Help.FullDesc = lipgloss.NewStyle().Foreground(colorDim)
	t.Help.FullSeparator = lipgloss.NewStyle().Foreground(colorSeparator)

	return t
}

func renderWizardHeader(label string, fg, bg color.Color, title, desc string) {
	dim := lipgloss.NewStyle().Foreground(colorDim)
	_, _ = lipgloss.Printf("\n%s%s\n%s\n%s\n\n",
		badge(label, fg, bg),
		lipgloss.NewStyle().Bold(true).Foreground(colorCoral).Render(" "+title),
		dim.Render(desc),
		dim.Render("Shift+Tab goes back · Esc cancels without saving"),
	)
}

func highlightConfigBlock(block string) string {
	var lines []string
	kwHost := lipgloss.NewStyle().Bold(true).Foreground(colorCoral)
	valHost := lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	kwDirective := lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	valDirective := lipgloss.NewStyle().Foreground(colorLightGray)
	commentStyle := lipgloss.NewStyle().Italic(true).Foreground(colorDim)

	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			lines = append(lines, "")
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			indent := ""
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				indent = "    "
			}
			lines = append(lines, indent+commentStyle.Render(trimmed))
			continue
		}
		idx := strings.IndexAny(trimmed, " \t")
		if idx == -1 {
			lines = append(lines, line)
			continue
		}
		kw := trimmed[:idx]
		val := strings.TrimSpace(trimmed[idx:])
		if strings.EqualFold(kw, "Host") || (!strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")) {
			lines = append(lines, fmt.Sprintf("%s %s", kwHost.Render(kw), valHost.Render(val)))
		} else {
			lines = append(lines, fmt.Sprintf("    %s %s", kwDirective.Render(kw), valDirective.Render(val)))
		}
	}
	return strings.Join(lines, "\n")
}

// wizardMode selects the wording and field order of the shared host form.
type wizardMode int

const (
	wizardAdd wizardMode = iota
	wizardEdit
	wizardClone
)

type hostFormText struct {
	step, stepDesc, authDesc                string
	aliasTitle, aliasDesc, aliasPlaceholder string
	hostDesc, hostPlaceholder               string
}

var hostFormTexts = map[wizardMode]hostFormText{
	wizardAdd: {
		step: "Step 1: Host Details", stepDesc: "Enter target server connection parameters", authDesc: "Select your authentication strategy",
		aliasTitle: "Host Alias", aliasDesc: "Nickname used with ssh <alias> and sshx", aliasPlaceholder: "e.g. prod-server",
		hostDesc: "FQDN or IP address of the remote host", hostPlaceholder: "e.g. 192.168.1.100 or server.example.com",
	},
	wizardEdit: {
		step: "Step 1: Edit Host Details", stepDesc: "Update target connection parameters", authDesc: "Select authentication strategy",
		aliasTitle: "Host Alias", aliasDesc: "Nickname used with ssh <alias> and sshx", aliasPlaceholder: "e.g. prod-server",
		hostDesc: "FQDN or IP address of the remote host", hostPlaceholder: "e.g. 192.168.1.100 or server.example.com",
	},
	wizardClone: {
		step: "Step 1: Duplicate Host Parameters", stepDesc: "Customize details for the cloned host", authDesc: "Select authentication strategy",
		aliasTitle: "New Host Alias", aliasDesc: "Unique nickname for this duplicated host", aliasPlaceholder: "e.g. prod-server-backup",
		hostDesc: "FQDN or IP address of the target server", hostPlaceholder: "e.g. 192.168.1.101",
	},
}

// hostFormValues holds the fields edited by the add, edit and clone wizards.
type hostFormValues struct {
	alias, hostName, user, port, proxyJump, auth string
}

func formValuesFromEntry(e HostEntry) hostFormValues {
	port := e.Port
	if port <= 0 {
		port = 22
	}
	return hostFormValues{
		alias:     e.Alias,
		hostName:  e.HostName,
		user:      e.User,
		port:      strconv.Itoa(port),
		proxyJump: e.ProxyJump,
		auth:      authMethodOf(e),
	}
}

// authMethodOf classifies a host's authentication settings into a wizard option.
func authMethodOf(e HostEntry) string {
	switch {
	case !e.PubkeyAuth || e.PasswordAuth:
		return authPassword
	case e.IdentityFile != "":
		return authKey
	default:
		return authDefault
	}
}

// validateText rejects control characters, which could corrupt the config or inject directives.
func validateText(s string) error {
	if strings.IndexFunc(s, unicode.IsControl) != -1 {
		return errors.New("value cannot contain control characters")
	}
	return nil
}

func validateAlias(s string) error {
	if err := validateText(s); err != nil {
		return err
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return errors.New("host alias is required")
	}
	for _, f := range fields {
		if strings.HasPrefix(f, "-") {
			return fmt.Errorf("alias %q cannot start with '-'", f)
		}
		if !isConcreteAlias(f) || strings.ContainsAny(f, `"'`) {
			return fmt.Errorf("alias %q cannot contain wildcards or quotes", f)
		}
	}
	return nil
}

func validateHostName(s string) error {
	if err := validateText(s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("hostname is required")
	}
	if strings.ContainsAny(s, " \t") {
		return errors.New("hostname cannot contain spaces")
	}
	return nil
}

func validatePort(s string) error {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p <= 0 || p > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

// Choices in the wizard's key step that aren't key paths.
const (
	keyCustom   = "custom"
	keyGenerate = "generate"
)

// hostWizard holds the answers of the add, edit and clone wizards while they
// run. Later pages read them, so they follow earlier answers even after going
// back.
type hostWizard struct {
	hostFormValues
	mode    wizardMode
	homeDir string

	keyChoice  string // a choice above, or a key's path
	customKey  string // the existing key typed for keyCustom
	newKey     string // where keyGenerate creates the key; empty for defaultNewKey
	keyOptions []huh.Option[string]
	save       bool

	goingBack bool // the last key pressed moves back a page (trackDirection)

	form        *huh.Form             // the wizard, for which field has focus
	completions map[string]completion // the inputs that complete paths, by field key
}

// newHostWizard prepares the wizard's answers from v. currentKey is the
// host's IdentityFile, offered first in the key step.
func newHostWizard(mode wizardMode, v hostFormValues, homeDir, currentKey string) *hostWizard {
	w := &hostWizard{hostFormValues: v, mode: mode, homeDir: homeDir, save: true}

	discovered, _ := DiscoverKeys(filepath.Join(homeDir, ".ssh"))
	if currentKey != "" {
		w.keyChoice = expandHome(currentKey, homeDir)
		w.keyOptions = append(w.keyOptions, huh.NewOption(shortenHome(w.keyChoice, homeDir)+" (Current)", w.keyChoice))
	}
	for _, k := range discovered {
		if k != w.keyChoice {
			w.keyOptions = append(w.keyOptions, huh.NewOption(shortenHome(k, homeDir), k))
		}
	}
	// The choices are the same whatever the earlier answers are: when a
	// select's options change, Huh keeps the cursor where it was, which would
	// quietly answer with whichever option took the chosen one's place.
	w.keyOptions = append(w.keyOptions,
		huh.NewOption("Custom key path...", keyCustom),
		huh.NewOption("Generate new Ed25519 key on the fly...", keyGenerate),
	)
	switch {
	case w.keyChoice != "":
	case len(discovered) > 0:
		w.keyChoice = discovered[0]
	default:
		w.keyChoice = keyGenerate
	}
	return w
}

// filter sees every message before the wizard does: Tab accepts a path
// suggestion, and the direction the user moves in is noted.
func (w *hostWizard) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	msg = w.completeOnTab(msg)
	w.trackDirection(msg)
	return msg
}

// trackDirection notes, for every message the form gets, whether the user
// is moving back.
func (w *hostWizard) trackDirection(msg tea.Msg) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		w.goingBack = key.Matches(k, formKeyMap().Input.Prev)
	}
}

// forward makes a field's check apply only when the user moves on. Huh
// checks a field again as it loses focus, and won't leave a page holding an
// error in either direction, so a half-typed answer would otherwise trap the
// user on its page. Moving on still needs a valid answer, and the Save button
// checks every page once more (checkAnswers), in case a page left with a bad
// answer isn't passed again.
func forward[T any](w *hostWizard, check func(T) error) func(T) error {
	return func(v T) error {
		if w.goingBack {
			return nil
		}
		return check(v)
	}
}

// formKeyMap is Huh's keymap with Esc as well as Ctrl+C cancelling. Select
// filtering is off, as Esc would otherwise also clear a filter.
func formKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel"))
	// The wizard's filter turns Tab into Ctrl+E while a suggestion shows
	// (completeOnTab), so that's the key to show.
	km.Input.AcceptSuggestion.SetHelp("tab", "complete")
	km.Select.Filter.SetEnabled(false)
	km.MultiSelect.Filter.SetEnabled(false)
	return km
}

// newForm puts the wizard's pages into one form, so Shift+Tab goes back to
// any earlier page and Esc cancels from any of them. The key pages show only
// when the answers before them ask for a key.
func (w *hostWizard) newForm(theme huh.Theme) *huh.Form {
	text := hostFormTexts[w.mode]

	hostInput := huh.NewInput().Key("hostname").
		Title("HostName / IP").
		Description(text.hostDesc).
		Placeholder(text.hostPlaceholder).
		Value(&w.hostName).
		Validate(forward(w, validateHostName))
	aliasInput := huh.NewInput().Key("alias").
		Title(text.aliasTitle).
		Description(text.aliasDesc).
		Placeholder(text.aliasPlaceholder).
		Value(&w.alias).
		Validate(forward(w, validateAlias))
	userInput := huh.NewInput().Key("user").
		Title("User").
		Description("Remote login username").
		Placeholder(currentUsername()).
		Value(&w.user).
		Validate(forward(w, validateText))
	portInput := huh.NewInput().Key("port").
		Title("Port").
		Description("SSH port (standard is 22)").
		Placeholder("22").
		Value(&w.port).
		Validate(forward(w, validatePort))
	proxyInput := huh.NewInput().Key("proxyjump").
		Title("ProxyJump").
		Description("Optional jump host or bastion (e.g. bastion.lan)").
		Placeholder("leave blank for direct connection").
		Value(&w.proxyJump).
		Validate(forward(w, validateText))

	fields := []huh.Field{hostInput, aliasInput, userInput, portInput, proxyInput}
	if w.mode == wizardClone {
		fields[0], fields[1] = aliasInput, hostInput
	}

	customKeyInput := w.completePaths(huh.NewInput().Key("custom-key").
		Title("Private Key Path").
		Description("Path to your private key file").
		Placeholder("~/.ssh/id_custom").
		Value(&w.customKey).
		Validate(forward(w, w.checkKeyFile)),
		&w.customKey, newPathCompleter(w.homeDir, true))
	// The default path follows the alias, even after going back to change
	// it; the placeholder is bound to the alias alone.
	newKeyInput := w.completePaths(huh.NewInput().Key("new-key").
		Title("New Key File Path").
		Description("Path where the new Ed25519 key will be created; leave empty for the one shown").
		PlaceholderFunc(w.defaultNewKey, &w.alias).
		Value(&w.newKey).
		Validate(forward(w, validateText)),
		&w.newKey, newPathCompleter(w.homeDir, false))

	noKey := func() bool { return w.auth != authKey }

	w.form = huh.NewForm(
		huh.NewGroup(fields...).Title(text.step).Description(text.stepDesc),
		huh.NewGroup(
			huh.NewSelect[string]().Key("auth").
				Title("Authentication Method").
				Description("Choose how you connect to this server").
				Options(
					huh.NewOption("SSH Key (IdentityFile) [Recommended]", authKey),
					huh.NewOption("Password only (disables pubkey & agent to prevent lockouts)", authPassword),
					huh.NewOption("Standard / Default (agent keys with password fallback)", authDefault),
				).
				Value(&w.auth),
		).Title("Step 2: Authentication").Description(text.authDesc),
		huh.NewGroup(
			huh.NewSelect[string]().Key("key").
				Title("Select Private Key").
				Description("Choose an existing key in ~/.ssh or generate a new one").
				Options(w.keyOptions...).
				Value(&w.keyChoice),
		).Title("Step 3: Private Key Selection").Description("Select which SSH key to use for authentication").
			WithHideFunc(noKey),
		huh.NewGroup(customKeyInput).
			Title("Custom Key Path").Description("Specify the absolute or ~-relative path to your key").
			WithHideFunc(func() bool { return noKey() || w.keyChoice != keyCustom }),
		huh.NewGroup(newKeyInput).
			Title("Generate Ed25519 Key").Description("New high-security elliptic curve key pair, created once you save").
			WithHideFunc(func() bool { return noKey() || w.keyChoice != keyGenerate }),
		huh.NewGroup(
			huh.NewConfirm().Key("save").
				Title("Save this host?").
				Description("Nothing is written until you save").
				Affirmative("Save").Negative("Cancel").
				Value(&w.save).
				Validate(forward(w, func(save bool) error {
					if !save {
						return nil
					}
					return w.checkAnswers()
				})),
		).Title("Step 4: Save"),
	).WithTheme(theme).WithKeyMap(formKeyMap()).WithProgramOptions(tea.WithFilter(w.filter))
	return w.form
}

// run asks every page; choosing Cancel on the last page counts as
// cancelling, like Esc.
func (w *hostWizard) run(theme huh.Theme) error {
	if err := w.newForm(theme).Run(); err != nil {
		return err
	}
	if !w.save {
		return huh.ErrUserAborted
	}
	return nil
}

// checkKeyFile checks a typed private key path.
func (w *hostWizard) checkKeyFile(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("enter the path of your private key")
	}
	exp := expandHome(s, w.homeDir)
	if _, err := os.Stat(exp); err != nil {
		return fmt.Errorf("file does not exist: %s", exp)
	}
	return nil
}

// checkAnswers checks every page that's showing once more before saving.
// The pages check their answers only when the user moves on, so one left by
// going back could still hold a bad answer.
func (w *hostWizard) checkAnswers() error {
	checks := []struct {
		page string
		show bool
		err  func() error
	}{
		{"HostName / IP", true, func() error { return validateHostName(w.hostName) }},
		{hostFormTexts[w.mode].aliasTitle, true, func() error { return validateAlias(w.alias) }},
		{"User", true, func() error { return validateText(w.user) }},
		{"Port", true, func() error { return validatePort(w.port) }},
		{"ProxyJump", true, func() error { return validateText(w.proxyJump) }},
		{"Private Key Path", w.auth == authKey && w.keyChoice == keyCustom, func() error { return w.checkKeyFile(w.customKey) }},
		{"New Key File Path", w.auth == authKey && w.keyChoice == keyGenerate, func() error { return validateText(w.newKey) }},
	}
	for _, c := range checks {
		if !c.show {
			continue
		}
		if err := c.err(); err != nil {
			return fmt.Errorf("%s: %w. Go back with Shift+Tab to fix it", c.page, err)
		}
	}
	return nil
}

// defaultNewKey is where a generated key goes unless another path is typed:
// ~/.ssh/id_ed25519_<alias>.
func (w *hostWizard) defaultNewKey() string {
	return shortenHome(filepath.Join(w.homeDir, ".ssh", "id_ed25519_"+primaryAlias(w.alias)), w.homeDir)
}

// identityFile is the private key the answers chose, expanded, or "" when the
// host doesn't use one. A key to generate doesn't exist until generateKey.
func (w *hostWizard) identityFile() string {
	if w.auth != authKey {
		return ""
	}
	switch w.keyChoice {
	case keyCustom:
		return expandHome(strings.TrimSpace(w.customKey), w.homeDir)
	case keyGenerate:
		return expandHome(cmp.Or(strings.TrimSpace(w.newKey), w.defaultNewKey()), w.homeDir)
	}
	return w.keyChoice
}

// generateKey creates the Ed25519 key the answers asked for, if any. It runs
// only once the host is about to be written, so cancelling creates no key.
func (w *hostWizard) generateKey() error {
	if w.auth != authKey || w.keyChoice != keyGenerate {
		return nil
	}
	keyPath := w.identityFile()
	comment := fmt.Sprintf("%s@%s", strings.TrimSpace(w.user), strings.TrimSpace(w.hostName))

	_, _ = lipgloss.Printf("\n%s Generating Ed25519 key at %s...\n\n", badge(" KEYGEN ", colorBlack, colorCyan), keyPath)
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-C", comment, "-f", keyPath) //nolint:gosec // intentional key generation
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// ssh-keygen also exits non-zero when the user declines to overwrite an existing key,
		// in which case that key is still usable.
		if _, statErr := os.Stat(keyPath); statErr != nil {
			return fmt.Errorf("ssh-keygen failed: %w", err)
		}
	}
	return nil
}

// generatedKey is the key generateKey created for the answers, expanded, or
// "" when the answers didn't ask for a new key.
func (w *hostWizard) generatedKey() string {
	if w.auth != authKey || w.keyChoice != keyGenerate {
		return ""
	}
	return w.identityFile()
}

// offerKeychain helps ssh use a key the wizard generated without asking for
// its passphrase on every connection to alias (rememberPassphrase).
func (w *hostWizard) offerKeychain(alias string, theme huh.Theme) {
	rememberPassphrase(os.Stdout, w.generatedKey(), alias, w.homeDir, func(title, description string) bool {
		return confirm("Keychain", title, description, theme)
	})
}

// buildEntry turns form values into a HostEntry. When prev is given and the
// authentication method is unchanged, prev's authentication settings are kept
// so an edit only rewrites what the user actually changed.
func buildEntry(v hostFormValues, identityFile, homeDir string, prev *HostEntry) HostEntry {
	port, _ := strconv.Atoi(strings.TrimSpace(v.port))
	e := HostEntry{
		Alias:     strings.Join(strings.Fields(v.alias), " "),
		HostName:  strings.TrimSpace(v.hostName),
		User:      strings.TrimSpace(v.user),
		Port:      port,
		ProxyJump: strings.TrimSpace(v.proxyJump),
	}

	if prev != nil && v.auth == authMethodOf(*prev) {
		e.IdentityFile = prev.IdentityFile
		e.IdentitiesOnly = prev.IdentitiesOnly
		e.PubkeyAuth = prev.PubkeyAuth
		e.PasswordAuth = prev.PasswordAuth
		e.PreferredAuths = prev.PreferredAuths
		if v.auth == authKey && !sameFile(identityFile, prev.IdentityFile, homeDir) {
			e.IdentityFile = shortenHome(identityFile, homeDir)
		}
		return e
	}

	switch v.auth {
	case authPassword:
		e.PasswordAuth = true
	case authDefault:
		e.PubkeyAuth = true
	case authKey:
		e.PubkeyAuth = true
		e.IdentityFile = shortenHome(identityFile, homeDir)
		e.IdentitiesOnly = e.IdentityFile != ""
	}
	return e
}

func sameFile(a, b, homeDir string) bool {
	return filepath.Clean(expandHome(a, homeDir)) == filepath.Clean(expandHome(b, homeDir))
}

func primaryAlias(alias string) string {
	if fields := strings.Fields(alias); len(fields) > 0 {
		return fields[0]
	}
	return alias
}

func currentUsername() string {
	if name := os.Getenv("USER"); name != "" {
		return name
	}
	if u, err := user.Current(); err == nil {
		// Windows reports DOMAIN\user; ssh expects the bare user name.
		return u.Username[strings.LastIndex(u.Username, `\`)+1:]
	}
	return ""
}

// ignoreAbort treats a cancelled wizard as a clean exit, saying nothing changed.
func ignoreAbort(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		printCancelled()
		return nil
	}
	return err
}

// printCancelled notes that a wizard was cancelled before writing anything.
func printCancelled() {
	_, _ = lipgloss.Printf("\n%s\n\n", lipgloss.NewStyle().Foreground(colorDim).Render("Cancelled. Nothing was changed."))
}

// confirm asks a yes/no question; an aborted prompt counts as "no".
func confirm(group, title, description string, theme huh.Theme) bool {
	var ok bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Value(&ok),
		).Title(group),
	).WithTheme(theme).WithKeyMap(formKeyMap()).Run()
	return err == nil && ok
}

func confirmOverwrite(alias, configPath, homeDir string, theme huh.Theme) bool {
	return confirm("Host Conflict Warning",
		fmt.Sprintf("Host alias '%s' already exists in %s. Overwrite?", alias, shortenHome(configPath, homeDir)),
		"Existing configuration for this alias will be replaced",
		theme)
}

func readConfig(configPath string) string {
	data, err := os.ReadFile(filepath.Clean(configPath)) //nolint:gosec // user SSH config path
	if err != nil {
		return ""
	}
	return string(data)
}

// hostSection returns the text of the first Host section listing alias.
func hostSection(content, alias string) string {
	lines := strings.Split(content, "\n")
	for _, b := range parseHostBlocks(lines) {
		if b.has(alias) {
			return strings.Join(lines[b.start:b.end], "\n")
		}
	}
	return ""
}

func printSavedCard(label, message, block string) {
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPurple).
		Padding(0, 2).
		MarginTop(1).
		MarginBottom(1).
		Render(highlightConfigBlock(block))

	_, _ = lipgloss.Println()
	_, _ = lipgloss.Printf("%s%s\n", badge(label, colorBlack, colorGreen), lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(" "+message))
	_, _ = lipgloss.Println(card)
}

// AddHostWizard runs the interactive Huh wizard to configure and add a host.
// Returns the added alias and whether the user asked to connect immediately.
func AddHostWizard(posTarget, posAlias, homeDir string, promptConnect bool) (alias string, connectNow bool, err error) {
	renderWizardHeader("🚀 SSHX", colorWhite, colorPurple, "Add New SSH Connection", "Configure and register a remote host in ~/.ssh/config")

	targetUser, targetHost, targetPort := ParseTarget(posTarget)
	v := hostFormValues{alias: posAlias, hostName: targetHost, user: targetUser, port: "22", auth: authKey}
	if v.user == "" {
		v.user = currentUsername()
	}
	if targetPort != 0 {
		v.port = strconv.Itoa(targetPort)
	}
	if v.alias == "" {
		v.alias = targetHost
	}

	theme := customHuhTheme()
	w := newHostWizard(wizardAdd, v, homeDir, "")
	if err := w.run(theme); err != nil {
		return "", false, ignoreAbort(err)
	}
	entry := buildEntry(w.hostFormValues, w.identityFile(), homeDir, nil)

	configPath := filepath.Join(homeDir, ".ssh", "config")
	if owner, ok := FindAliasOwner(homeDir, entry.Alias, "", ""); ok {
		if !confirmOverwrite(entry.Alias, owner, homeDir, theme) {
			printCancelled()
			return "", false, nil
		}
		configPath = owner
	}
	if err := w.generateKey(); err != nil {
		return "", false, err
	}

	content := InsertHost(RemoveHost(readConfig(configPath), entry.Alias), entry.Format())
	if err := WriteConfigFile(configPath, []byte(content)); err != nil {
		return "", false, fmt.Errorf("failed writing to %s: %w", configPath, err)
	}
	printSavedCard("✔ SAVED", fmt.Sprintf("Successfully written to %s", shortenHome(configPath, homeDir)), entry.Format())

	alias = primaryAlias(entry.Alias)
	w.offerKeychain(alias, theme)
	offerCopyID(alias, entry.IdentityFile, homeDir, theme)

	if promptConnect && confirm("Quick Connect",
		fmt.Sprintf("Connect to '%s' now?", alias),
		fmt.Sprintf("Starts interactive session: ssh %s", alias),
		theme) {
		return alias, true, nil
	}

	return alias, false, nil
}

// EditHostWizard runs an interactive Huh wizard to modify an existing SSH host in place.
func EditHostWizard(alias, homeDir string) (string, error) {
	hosts, err := LoadAllHosts(homeDir)
	if err != nil {
		return "", err
	}
	target, ok := FindHost(hosts, alias)
	if !ok {
		return "", fmt.Errorf("host '%s' not found in SSH configuration", alias)
	}

	renderWizardHeader("✏️ SSHX", colorWhite, colorPurple, "Edit SSH Connection: "+target.Alias, "Modify and update host parameters in SSH configuration")

	prev := target.Entry()
	v := formValuesFromEntry(prev)
	theme := customHuhTheme()
	w := newHostWizard(wizardEdit, v, homeDir, prev.IdentityFile)
	if err := w.run(theme); err != nil {
		return "", ignoreAbort(err)
	}
	next := buildEntry(w.hostFormValues, w.identityFile(), homeDir, &prev)

	configPath := target.ConfigFile
	if configPath == "" {
		configPath = filepath.Join(homeDir, ".ssh", "config")
	}
	content := readConfig(configPath)

	// The host this one replaces, when it's defined in another file.
	replacedIn := ""
	if !strings.EqualFold(next.Alias, prev.Alias) {
		if owner, ok := FindAliasOwner(homeDir, next.Alias, configPath, prev.Alias); ok {
			if !confirmOverwrite(next.Alias, owner, homeDir, theme) {
				printCancelled()
				return "", nil
			}
			if owner == configPath {
				content = RemoveHost(content, next.Alias)
			} else {
				replacedIn = owner
			}
		}
	}

	updated, err := UpdateHost(content, prev.Alias, prev, next)
	if err != nil {
		return "", err
	}
	if err := w.generateKey(); err != nil {
		return "", err
	}
	if replacedIn != "" {
		if err := DeleteHostFromConfigFile(next.Alias, replacedIn); err != nil {
			return "", err
		}
	}
	if err := WriteConfigFile(configPath, []byte(updated)); err != nil {
		return "", fmt.Errorf("failed writing to %s: %w", configPath, err)
	}

	newAlias := primaryAlias(next.Alias)
	printSavedCard("✔ UPDATED", fmt.Sprintf("Successfully updated %s in %s", newAlias, shortenHome(configPath, homeDir)), hostSection(updated, newAlias))
	w.offerKeychain(newAlias, theme)
	offerCopyID(newAlias, next.IdentityFile, homeDir, theme)

	return newAlias, nil
}

// CloneHostWizard launches an interactive wizard pre-populated with an existing host's configuration
// to create a duplicate or variant host under a new alias.
func CloneHostWizard(alias, homeDir string) (string, error) {
	hosts, err := LoadAllHosts(homeDir)
	if err != nil {
		return "", err
	}
	source, ok := FindHost(hosts, alias)
	if !ok {
		return "", fmt.Errorf("host '%s' not found in SSH configuration", alias)
	}

	renderWizardHeader("📋 CLONE", colorBlack, colorCyan, "Duplicate Connection: "+source.Alias, "Create a new host entry pre-filled with this configuration")

	prev := source.Entry()
	v := formValuesFromEntry(prev)
	v.alias = source.Alias + "-clone"
	theme := customHuhTheme()
	w := newHostWizard(wizardClone, v, homeDir, prev.IdentityFile)
	if err := w.run(theme); err != nil {
		return "", ignoreAbort(err)
	}
	next := buildEntry(w.hostFormValues, w.identityFile(), homeDir, &prev)

	configPath := source.ConfigFile
	if configPath == "" {
		configPath = filepath.Join(homeDir, ".ssh", "config")
	}
	content := readConfig(configPath)
	block := CloneHost(content, prev, next)

	if owner, ok := FindAliasOwner(homeDir, next.Alias, "", ""); ok {
		if !confirmOverwrite(next.Alias, owner, homeDir, theme) {
			printCancelled()
			return "", nil
		}
		if err := w.generateKey(); err != nil {
			return "", err
		}
		if owner == configPath {
			content = RemoveHost(content, next.Alias)
		} else if err := DeleteHostFromConfigFile(next.Alias, owner); err != nil {
			return "", err
		}
	} else if err := w.generateKey(); err != nil {
		return "", err
	}

	if err := WriteConfigFile(configPath, []byte(InsertHost(content, block))); err != nil {
		return "", fmt.Errorf("failed writing to %s: %w", configPath, err)
	}

	newAlias := primaryAlias(next.Alias)
	printSavedCard("✔ CLONED", fmt.Sprintf("Successfully created %s in %s", newAlias, shortenHome(configPath, homeDir)), block)
	w.offerKeychain(newAlias, theme)
	offerCopyID(newAlias, next.IdentityFile, homeDir, theme)

	return newAlias, nil
}

// DeleteHostPrompt confirms and removes a host, with all of its aliases, from the config file that defines it.
func DeleteHostPrompt(alias, homeDir string) error {
	aliases := alias
	configPath := ""
	if hosts, err := LoadAllHosts(homeDir); err == nil {
		if h, ok := FindHost(hosts, alias); ok {
			aliases = strings.Join(h.AllAliases, " ")
			configPath = h.ConfigFile
		}
	}
	if configPath == "" {
		var err error
		if configPath, err = ResolveHostConfigFile(alias, homeDir); err != nil {
			return err
		}
	}
	displayPath := shortenHome(configPath, homeDir)

	description := "This action permanently removes the Host configuration entry"
	if names := strings.Fields(aliases); len(names) > 1 {
		description = fmt.Sprintf("Removes aliases %s", strings.Join(names, ", "))
	}
	description += fmt.Sprintf("\nThe previous file is kept at %s", shortenHome(BackupPath(configPath), homeDir))

	if !confirm("Confirm Host Deletion",
		fmt.Sprintf("Delete host '%s' from %s?", alias, displayPath),
		description,
		customHuhTheme()) {
		return nil
	}

	if err := DeleteHostFromConfigFile(aliases, configPath); err != nil {
		return err
	}

	_, _ = lipgloss.Printf("\n%s Removed '%s' from %s\n\n", badge(" DELETED ", colorWhite, colorCoral), alias, displayPath)
	return nil
}

// offerCopyID offers to install the host's public key on the remote server once the host is saved.
func offerCopyID(alias, identityFile, homeDir string, theme huh.Theme) {
	if identityFile == "" {
		return
	}
	pubKey := expandHome(identityFile, homeDir) + ".pub"
	if _, err := os.Stat(filepath.Clean(pubKey)); err != nil {
		return
	}
	if confirm("Deploy Public Key",
		fmt.Sprintf("Copy public key to '%s' with ssh-copy-id?", alias),
		"Uploads public key to remote authorized_keys for passwordless login",
		theme) {
		if err := copyPublicKey(pubKey, alias); err != nil {
			fmt.Fprintf(os.Stderr, "Copying public key failed: %v\n", err)
		}
	}
}

// copyPublicKey installs pubKey in authorized_keys on the host behind alias.
// Connecting by alias applies the host's User, Port and ProxyJump settings.
// Without ssh-copy-id (e.g. on Windows) the key is appended over plain ssh.
func copyPublicKey(pubKey, alias string) error {
	var cmd *exec.Cmd
	label := badge(" SSH-COPY-ID ", colorBlack, colorAmber)

	if _, err := exec.LookPath("ssh-copy-id"); err == nil {
		_, _ = lipgloss.Printf("\n%s Running: ssh-copy-id -i %s %s\n\n", label, pubKey, alias)
		cmd = exec.Command("ssh-copy-id", "-i", pubKey, alias) //nolint:gosec // intentional copy-id invocation
		cmd.Stdin = os.Stdin
	} else {
		key, err := os.ReadFile(filepath.Clean(pubKey)) //nolint:gosec // user public key path
		if err != nil {
			return err
		}
		_, _ = lipgloss.Printf("\n%s ssh-copy-id not found; appending %s to ~/.ssh/authorized_keys on %s\n\n", label, pubKey, alias)
		cmd = exec.Command("ssh", alias, remoteAppendKey) //nolint:gosec // intentional key installation over ssh
		cmd.Stdin = bytes.NewReader(key)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
