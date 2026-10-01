package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// customHuhTheme returns an aligned Lip Gloss theme matching the sshx design system.
func customHuhTheme() *huh.Theme {
	t := huh.ThemeBase()

	var (
		purple    = lipgloss.Color("#7D56F4")
		coral     = lipgloss.Color("#FF5F87")
		cyan      = lipgloss.Color("#00D7D7")
		green     = lipgloss.Color("#5FD787")
		dim       = lipgloss.Color("#767676")
		darkGray  = lipgloss.Color("#262626")
		lightGray = lipgloss.Color("#EEEEEE")
		white     = lipgloss.Color("#FFFFFF")
		red       = lipgloss.Color("#FF4672")
	)

	// Left border indicator on focused fields
	t.Focused.Base = t.Focused.Base.BorderForeground(purple)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle().Bold(true).Foreground(coral)
	t.Focused.Description = lipgloss.NewStyle().Foreground(dim)
	t.Focused.Directory = lipgloss.NewStyle().Foreground(cyan)

	// Validation errors
	t.Focused.ErrorIndicator = lipgloss.NewStyle().Bold(true).Foreground(red).SetString(" ✘")
	t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(red)

	// Select / Options
	t.Focused.SelectSelector = lipgloss.NewStyle().Bold(true).Foreground(coral).SetString("› ")
	t.Focused.NextIndicator = lipgloss.NewStyle().Foreground(coral).MarginLeft(1).SetString("→")
	t.Focused.PrevIndicator = lipgloss.NewStyle().Foreground(coral).MarginRight(1).SetString("←")
	t.Focused.Option = lipgloss.NewStyle().Foreground(lightGray)

	// Multi-select / Checkboxes
	t.Focused.MultiSelectSelector = lipgloss.NewStyle().Bold(true).Foreground(coral).SetString("› ")
	t.Focused.SelectedOption = lipgloss.NewStyle().Bold(true).Foreground(green)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Bold(true).Foreground(green).SetString("✔ ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(dim).SetString("• ")
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(dim)

	// Confirm buttons
	button := lipgloss.NewStyle().
		Padding(0, 2).
		MarginRight(1).
		Bold(true)
	t.Focused.FocusedButton = button.Foreground(white).Background(purple)
	t.Focused.BlurredButton = button.Foreground(dim).Background(darkGray)

	// Text Inputs
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(cyan)
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(dim)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Bold(true).Foreground(cyan).SetString("› ")
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(lightGray)

	// Blurred field styles (when navigating between inputs in a group)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = lipgloss.NewStyle().Foreground(dim)
	t.Blurred.Description = lipgloss.NewStyle().Foreground(dim)
	t.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(dim).SetString("  ")
	t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(lightGray)
	t.Blurred.Option = lipgloss.NewStyle().Foreground(dim)

	// Group Title & Description
	t.Group.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(purple).
		MarginBottom(1)
	t.Group.Description = lipgloss.NewStyle().
		Foreground(dim).
		MarginBottom(1)

	// Help styles
	t.Help.ShortKey = lipgloss.NewStyle().Bold(true).Foreground(cyan)
	t.Help.ShortDesc = lipgloss.NewStyle().Foreground(dim)
	t.Help.ShortSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	t.Help.FullKey = lipgloss.NewStyle().Bold(true).Foreground(cyan)
	t.Help.FullDesc = lipgloss.NewStyle().Foreground(dim)
	t.Help.FullSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))

	return t
}

func renderWizardHeader() {
	badge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1).
		Render("🚀 SSHX")
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FF5F87")).
		Render(" Add New SSH Connection")
	desc := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#767676")).
		Render("Configure and register a remote host in ~/.ssh/config")

	fmt.Printf("\n%s%s\n%s\n\n", badge, title, desc)
}

func highlightConfigBlock(block string) string {
	var lines []string
	kwHost := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F87"))
	valHost := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	kwDirective := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7D7"))
	valDirective := lipgloss.NewStyle().Foreground(lipgloss.Color("#EEEEEE"))

	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, " ", 2)
		if len(parts) == 1 {
			lines = append(lines, line)
			continue
		}
		if strings.HasPrefix(line, "Host ") || (!strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")) {
			lines = append(lines, fmt.Sprintf("%s %s", kwHost.Render(parts[0]), valHost.Render(parts[1])))
		} else {
			lines = append(lines, fmt.Sprintf("    %s %s", kwDirective.Render(parts[0]), valDirective.Render(parts[1])))
		}
	}
	return strings.Join(lines, "\n")
}

// AddHostWizard runs the interactive Huh wizard to configure and add a host.
// Returns the added alias and whether the user asked to connect immediately.
func AddHostWizard(posTarget, posAlias, homeDir string, promptConnect bool) (alias string, connectNow bool, err error) {
	renderWizardHeader()

	defaultUsername := os.Getenv("USER")
	if defaultUsername == "" {
		if u, err := user.Current(); err == nil {
			defaultUsername = u.Username
		}
	}

	targetUser, targetHost, targetPort := ParseTarget(posTarget)

	hostName := targetHost
	userName := targetUser
	if userName == "" {
		userName = defaultUsername
	}

	port := targetPort
	if port == 0 {
		port = 22
	}
	portStr := strconv.Itoa(port)

	alias = posAlias
	if alias == "" && hostName != "" {
		alias = hostName
	}

	theme := customHuhTheme()
	sshDir := filepath.Join(homeDir, ".ssh")
	configPath := filepath.Join(sshDir, "config")

	authMethod := "key"
	identityFile := ""

	// Step 1: Host configuration form
	formHost := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("HostName / IP").
				Description("FQDN or IP address of the remote host").
				Placeholder("e.g. 192.168.1.100 or server.example.com").
				Value(&hostName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("hostname is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Host Alias").
				Description("Nickname used with ssh <alias> and sshx").
				Placeholder("e.g. prod-server").
				Value(&alias).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("host alias is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("User").
				Description("Remote login username").
				Placeholder(defaultUsername).
				Value(&userName),
			huh.NewInput().
				Title("Port").
				Description("SSH port (standard is 22)").
				Placeholder("22").
				Value(&portStr).
				Validate(func(s string) error {
					p, err := strconv.Atoi(strings.TrimSpace(s))
					if err != nil || p <= 0 || p > 65535 {
						return errors.New("port must be between 1 and 65535")
					}
					return nil
				}),
		).Title("Step 1: Host Details").Description("Enter target server connection parameters"),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Authentication Method").
				Description("Choose how you connect to this server").
				Options(
					huh.NewOption("SSH Key (IdentityFile) [Recommended]", "key"),
					huh.NewOption("Password only (disables pubkey & agent to prevent lockouts)", "password"),
					huh.NewOption("Standard / Default (agent keys with password fallback)", "default"),
				).
				Value(&authMethod),
		).Title("Step 2: Authentication").Description("Select your authentication strategy"),
	).WithTheme(theme)

	if err := formHost.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", false, nil
		}
		return "", false, err
	}

	if p, err := strconv.Atoi(strings.TrimSpace(portStr)); err == nil && p > 0 {
		port = p
	}

	// Step 2: Handle key selection if key authentication was chosen
	if authMethod == "key" {
		discovered, _ := DiscoverKeys(sshDir)
		var keyOptions []huh.Option[string]

		for _, k := range discovered {
			rel := k
			if strings.HasPrefix(k, homeDir) {
				rel = "~" + k[len(homeDir):]
			}
			keyOptions = append(keyOptions, huh.NewOption(rel, k))
		}
		keyOptions = append(keyOptions,
			huh.NewOption("Custom key path...", "custom"),
			huh.NewOption("Generate new Ed25519 key on the fly...", "generate"),
		)

		selectedKey := ""
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
			if errors.Is(err, huh.ErrUserAborted) {
				return "", false, nil
			}
			return "", false, err
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
				if errors.Is(err, huh.ErrUserAborted) {
					return "", false, nil
				}
				return "", false, err
			}
			identityFile = expandHome(strings.TrimSpace(customPath), homeDir)
		case "generate":
			cleanAlias := strings.Fields(alias)[0]
			defaultPath := filepath.Join(sshDir, "id_ed25519_"+cleanAlias)
			genPath := "~" + defaultPath[len(homeDir):]

			formGen := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("New Key File Path").
						Description("Path where the new Ed25519 key will be created").
						Value(&genPath),
				).Title("Generate Ed25519 Key").Description("New high-security elliptic curve key pair"),
			).WithTheme(theme)

			if err := formGen.Run(); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					return "", false, nil
				}
				return "", false, err
			}

			expandedGenPath := expandHome(strings.TrimSpace(genPath), homeDir)
			comment := fmt.Sprintf("%s@%s", userName, hostName)

			keygenBadge := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#00D7D7")).
				Padding(0, 1).
				Render(" KEYGEN ")

			fmt.Printf("\n%s Generating Ed25519 key at %s...\n\n", keygenBadge, expandedGenPath)
			cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-C", comment, "-f", expandedGenPath) //nolint:gosec // intentional key generation
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "ssh-keygen failed: %v\n", err)
			}
			identityFile = expandedGenPath
		default:
			identityFile = selectedKey
		}

		// Offer ssh-copy-id if public key exists
		if identityFile != "" {
			pubKey := identityFile + ".pub"
			if _, err := os.Stat(filepath.Clean(pubKey)); err == nil { //nolint:gosec // public key existence check
				var copyKey bool
				formCopy := huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title(fmt.Sprintf("Copy public key to %s@%s with ssh-copy-id?", userName, hostName)).
							Description("Uploads public key to remote authorized_keys for passwordless login").
							Value(&copyKey),
					).Title("Deploy Public Key"),
				).WithTheme(theme)

				if err := formCopy.Run(); err == nil && copyKey {
					copyKeyCmd(pubKey, port, userName, hostName)
				}
			}
		}
	}

	// Step 3: Format and save configuration
	pubkeyAuth := true
	passwordAuth := false

	switch authMethod {
	case "password":
		pubkeyAuth = false
		passwordAuth = true
		identityFile = ""
	case "default":
		pubkeyAuth = true
		passwordAuth = false
		identityFile = ""
	case "key":
		pubkeyAuth = true
		passwordAuth = false
	}

	formattedKey := identityFile
	if strings.HasPrefix(identityFile, homeDir) {
		formattedKey = "~" + identityFile[len(homeDir):]
	}

	entry := HostEntry{
		Alias:          alias,
		HostName:       hostName,
		User:           userName,
		Port:           port,
		IdentityFile:   formattedKey,
		IdentitiesOnly: formattedKey != "",
		PubkeyAuth:     pubkeyAuth,
		PasswordAuth:   passwordAuth,
	}

	var existingContent string
	if data, err := os.ReadFile(filepath.Clean(configPath)); err == nil { //nolint:gosec // user SSH config path
		existingContent = string(data)
	}

	if HasHost(existingContent, alias) {
		var overwrite bool
		formOverwrite := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Host alias '%s' already exists in ~/.ssh/config. Overwrite?", alias)).
					Description("Existing configuration for this alias will be replaced").
					Value(&overwrite),
			).Title("Host Conflict Warning"),
		).WithTheme(theme)

		if err := formOverwrite.Run(); err != nil || !overwrite {
			return "", false, nil
		}
		existingContent = RemoveHost(existingContent, alias)
	}

	formattedBlock := entry.Format()
	newContent := InsertHost(existingContent, formattedBlock)

	if err := AtomicWrite(configPath, []byte(newContent), 0600); err != nil {
		return "", false, fmt.Errorf("failed writing to %s: %w", configPath, err)
	}

	successBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#5FD787")).
		Padding(0, 1).
		Render(" ✔ SAVED ")

	successTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#5FD787")).
		Render(fmt.Sprintf(" Successfully written to %s", configPath))

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 2).
		MarginTop(1).
		MarginBottom(1).
		Render(highlightConfigBlock(formattedBlock))

	fmt.Println()
	fmt.Printf("%s%s\n", successBadge, successTitle)
	fmt.Println(card)

	if promptConnect {
		connectTarget := strings.Fields(alias)[0]
		formConnect := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Connect to '%s' now?", connectTarget)).
					Description(fmt.Sprintf("Starts interactive session: ssh %s", connectTarget)).
					Value(&connectNow),
			).Title("Quick Connect"),
		).WithTheme(theme)

		if err := formConnect.Run(); err == nil && connectNow {
			return connectTarget, true, nil
		}
	}

	return alias, false, nil
}

func expandHome(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	if path == "~" {
		return homeDir
	}
	return path
}

func copyKeyCmd(pubKey string, port int, user, host string) {
	args := []string{"-i", pubKey}
	if port > 0 && port != 22 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	target := fmt.Sprintf("%s@%s", user, host)
	args = append(args, target)

	badge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#FFAF00")).
		Padding(0, 1).
		Render(" SSH-COPY-ID ")

	binary := "ssh-copy-id"
	if _, err := exec.LookPath("ssh-copy-id"); err != nil {
		binary = "ssh"
	}

	fmt.Printf("\n%s Running: %s %s\n\n", badge, binary, strings.Join(args, " "))
	cmd := exec.Command(binary, args...) //nolint:gosec // intentional copy-id invocation
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
