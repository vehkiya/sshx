package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
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
func customHuhTheme() *huh.Theme {
	t := huh.ThemeBase()

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

func renderWizardHeader(label string, fg, bg lipgloss.Color, title, desc string) {
	fmt.Printf("\n%s%s\n%s\n\n",
		badge(label, fg, bg),
		lipgloss.NewStyle().Bold(true).Foreground(colorCoral).Render(" "+title),
		lipgloss.NewStyle().Foreground(colorDim).Render(desc),
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

// runHostForm shows the connection and authentication steps shared by all host wizards.
func runHostForm(mode wizardMode, v *hostFormValues, theme *huh.Theme) error {
	text := hostFormTexts[mode]

	hostInput := huh.NewInput().
		Title("HostName / IP").
		Description(text.hostDesc).
		Placeholder(text.hostPlaceholder).
		Value(&v.hostName).
		Validate(validateHostName)
	aliasInput := huh.NewInput().
		Title(text.aliasTitle).
		Description(text.aliasDesc).
		Placeholder(text.aliasPlaceholder).
		Value(&v.alias).
		Validate(validateAlias)
	userInput := huh.NewInput().
		Title("User").
		Description("Remote login username").
		Placeholder(currentUsername()).
		Value(&v.user).
		Validate(validateText)
	portInput := huh.NewInput().
		Title("Port").
		Description("SSH port (standard is 22)").
		Placeholder("22").
		Value(&v.port).
		Validate(validatePort)
	proxyInput := huh.NewInput().
		Title("ProxyJump").
		Description("Optional jump host or bastion (e.g. bastion.lan)").
		Placeholder("leave blank for direct connection").
		Value(&v.proxyJump).
		Validate(validateText)

	fields := []huh.Field{hostInput, aliasInput, userInput, portInput, proxyInput}
	if mode == wizardClone {
		fields[0], fields[1] = aliasInput, hostInput
	}

	return huh.NewForm(
		huh.NewGroup(fields...).Title(text.step).Description(text.stepDesc),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Authentication Method").
				Description("Choose how you connect to this server").
				Options(
					huh.NewOption("SSH Key (IdentityFile) [Recommended]", authKey),
					huh.NewOption("Password only (disables pubkey & agent to prevent lockouts)", authPassword),
					huh.NewOption("Standard / Default (agent keys with password fallback)", authDefault),
				).
				Value(&v.auth),
		).Title("Step 2: Authentication").Description(text.authDesc),
	).WithTheme(theme).Run()
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

// ignoreAbort treats a cancelled form as a clean exit.
func ignoreAbort(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return nil
	}
	return err
}

// confirm asks a yes/no question; an aborted prompt counts as "no".
func confirm(group, title, description string, theme *huh.Theme) bool {
	var ok bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Value(&ok),
		).Title(group),
	).WithTheme(theme).Run()
	return err == nil && ok
}

func confirmOverwrite(alias, configPath, homeDir string, theme *huh.Theme) bool {
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

	fmt.Println()
	fmt.Printf("%s%s\n", badge(label, colorBlack, colorGreen), lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(" "+message))
	fmt.Println(card)
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
	if err := runHostForm(wizardAdd, &v, theme); err != nil {
		return "", false, ignoreAbort(err)
	}

	identityFile := ""
	if v.auth == authKey {
		if identityFile, err = selectOrGenerateKey(homeDir, "", v, theme); err != nil {
			return "", false, ignoreAbort(err)
		}
	}
	entry := buildEntry(v, identityFile, homeDir, nil)

	configPath := filepath.Join(homeDir, ".ssh", "config")
	if owner, ok := FindAliasOwner(homeDir, entry.Alias, "", ""); ok {
		if !confirmOverwrite(entry.Alias, owner, homeDir, theme) {
			return "", false, nil
		}
		configPath = owner
	}

	content := InsertHost(RemoveHost(readConfig(configPath), entry.Alias), entry.Format())
	if err := WriteConfigFile(configPath, []byte(content)); err != nil {
		return "", false, fmt.Errorf("failed writing to %s: %w", configPath, err)
	}
	printSavedCard("✔ SAVED", fmt.Sprintf("Successfully written to %s", shortenHome(configPath, homeDir)), entry.Format())

	alias = primaryAlias(entry.Alias)
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
	if err := runHostForm(wizardEdit, &v, theme); err != nil {
		return "", ignoreAbort(err)
	}

	identityFile := ""
	if v.auth == authKey {
		if identityFile, err = selectOrGenerateKey(homeDir, prev.IdentityFile, v, theme); err != nil {
			return "", ignoreAbort(err)
		}
	}
	next := buildEntry(v, identityFile, homeDir, &prev)

	configPath := target.ConfigFile
	if configPath == "" {
		configPath = filepath.Join(homeDir, ".ssh", "config")
	}
	content := readConfig(configPath)

	if !strings.EqualFold(next.Alias, prev.Alias) {
		if owner, ok := FindAliasOwner(homeDir, next.Alias, configPath, prev.Alias); ok {
			if !confirmOverwrite(next.Alias, owner, homeDir, theme) {
				return "", nil
			}
			if owner == configPath {
				content = RemoveHost(content, next.Alias)
			} else if err := DeleteHostFromConfigFile(next.Alias, owner); err != nil {
				return "", err
			}
		}
	}

	updated, err := UpdateHost(content, prev.Alias, prev, next)
	if err != nil {
		return "", err
	}
	if err := WriteConfigFile(configPath, []byte(updated)); err != nil {
		return "", fmt.Errorf("failed writing to %s: %w", configPath, err)
	}

	newAlias := primaryAlias(next.Alias)
	printSavedCard("✔ UPDATED", fmt.Sprintf("Successfully updated %s in %s", newAlias, shortenHome(configPath, homeDir)), hostSection(updated, newAlias))
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
	if err := runHostForm(wizardClone, &v, theme); err != nil {
		return "", ignoreAbort(err)
	}

	identityFile := ""
	if v.auth == authKey {
		if identityFile, err = selectOrGenerateKey(homeDir, prev.IdentityFile, v, theme); err != nil {
			return "", ignoreAbort(err)
		}
	}
	next := buildEntry(v, identityFile, homeDir, &prev)

	configPath := source.ConfigFile
	if configPath == "" {
		configPath = filepath.Join(homeDir, ".ssh", "config")
	}
	content := readConfig(configPath)
	block := CloneHost(content, prev, next)

	if owner, ok := FindAliasOwner(homeDir, next.Alias, "", ""); ok {
		if !confirmOverwrite(next.Alias, owner, homeDir, theme) {
			return "", nil
		}
		if owner == configPath {
			content = RemoveHost(content, next.Alias)
		} else if err := DeleteHostFromConfigFile(next.Alias, owner); err != nil {
			return "", err
		}
	}

	if err := WriteConfigFile(configPath, []byte(InsertHost(content, block))); err != nil {
		return "", fmt.Errorf("failed writing to %s: %w", configPath, err)
	}

	newAlias := primaryAlias(next.Alias)
	printSavedCard("✔ CLONED", fmt.Sprintf("Successfully created %s in %s", newAlias, shortenHome(configPath, homeDir)), block)
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

	fmt.Printf("\n%s Removed '%s' from %s\n\n", badge(" DELETED ", colorWhite, colorCoral), alias, displayPath)
	return nil
}

// offerCopyID offers to install the host's public key on the remote server once the host is saved.
func offerCopyID(alias, identityFile, homeDir string, theme *huh.Theme) {
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
		fmt.Printf("\n%s Running: ssh-copy-id -i %s %s\n\n", label, pubKey, alias)
		cmd = exec.Command("ssh-copy-id", "-i", pubKey, alias) //nolint:gosec // intentional copy-id invocation
		cmd.Stdin = os.Stdin
	} else {
		key, err := os.ReadFile(filepath.Clean(pubKey)) //nolint:gosec // user public key path
		if err != nil {
			return err
		}
		fmt.Printf("\n%s ssh-copy-id not found; appending %s to ~/.ssh/authorized_keys on %s\n\n", label, pubKey, alias)
		cmd = exec.Command("ssh", alias, remoteAppendKey) //nolint:gosec // intentional key installation over ssh
		cmd.Stdin = bytes.NewReader(key)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// selectOrGenerateKey asks which private key the host should use, offering
// discovered keys, a custom path, or a newly generated Ed25519 key.
func selectOrGenerateKey(homeDir, initialKey string, v hostFormValues, theme *huh.Theme) (string, error) {
	sshDir := filepath.Join(homeDir, ".ssh")
	discovered, _ := DiscoverKeys(sshDir)
	var keyOptions []huh.Option[string]

	selectedKey := ""
	if initialKey != "" {
		selectedKey = expandHome(initialKey, homeDir)
		keyOptions = append(keyOptions, huh.NewOption(shortenHome(selectedKey, homeDir)+" (Current)", selectedKey))
	}

	for _, k := range discovered {
		if k == selectedKey {
			continue
		}
		keyOptions = append(keyOptions, huh.NewOption(shortenHome(k, homeDir), k))
	}
	keyOptions = append(keyOptions,
		huh.NewOption("Custom key path...", "custom"),
		huh.NewOption("Generate new Ed25519 key on the fly...", "generate"),
	)

	if selectedKey == "" && len(discovered) > 0 {
		selectedKey = discovered[0]
	}

	formKey := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Private Key").
				Description("Choose an existing key in ~/.ssh or generate a new one").
				Options(keyOptions...).
				Value(&selectedKey),
		).Title("Step 3: Private Key Selection").Description("Select which SSH key to use for authentication"),
	).WithTheme(theme)

	if err := formKey.Run(); err != nil {
		return "", err
	}

	switch selectedKey {
	case "custom":
		var customPath string
		formCustom := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Private Key Path").
					Description("Path to your private key file").
					Placeholder("~/.ssh/id_custom").
					Value(&customPath).
					Validate(func(s string) error {
						exp := expandHome(strings.TrimSpace(s), homeDir)
						if _, err := os.Stat(exp); err != nil {
							return fmt.Errorf("file does not exist: %s", exp)
						}
						return nil
					}),
			).Title("Custom Key Path").Description("Specify the absolute or ~-relative path to your key"),
		).WithTheme(theme)

		if err := formCustom.Run(); err != nil {
			return "", err
		}
		return expandHome(strings.TrimSpace(customPath), homeDir), nil

	case "generate":
		genPath := shortenHome(filepath.Join(sshDir, "id_ed25519_"+primaryAlias(v.alias)), homeDir)

		formGen := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("New Key File Path").
					Description("Path where the new Ed25519 key will be created").
					Value(&genPath),
			).Title("Generate Ed25519 Key").Description("New high-security elliptic curve key pair"),
		).WithTheme(theme)

		if err := formGen.Run(); err != nil {
			return "", err
		}

		keyPath := expandHome(strings.TrimSpace(genPath), homeDir)
		comment := fmt.Sprintf("%s@%s", strings.TrimSpace(v.user), strings.TrimSpace(v.hostName))

		fmt.Printf("\n%s Generating Ed25519 key at %s...\n\n", badge(" KEYGEN ", colorBlack, colorCyan), keyPath)
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-C", comment, "-f", keyPath) //nolint:gosec // intentional key generation
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// ssh-keygen also exits non-zero when the user declines to overwrite an existing key,
			// in which case that key is still usable.
			if _, statErr := os.Stat(keyPath); statErr != nil {
				return "", fmt.Errorf("ssh-keygen failed: %w", err)
			}
		}
		return keyPath, nil

	default:
		return selectedKey, nil
	}
}
