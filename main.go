package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/vehkiya/sshx/internal/probe"
	"github.com/vehkiya/sshx/internal/sshconfig"
	"github.com/vehkiya/sshx/internal/tui"
	"github.com/vehkiya/sshx/internal/ui"
	"github.com/vehkiya/sshx/internal/update"
	"github.com/vehkiya/sshx/internal/wizard"
)

// subcommands are the CLI verbs; any other first argument is treated as a host to connect to.
var subcommands = map[string]bool{
	"add": true, "ls": true, "list": true, "rm": true, "delete": true, "remove": true,
	"clone": true, "dup": true, "duplicate": true, "probe": true, "ping": true,
	"update": true, "upgrade": true, "check-update": true, "edit": true, "connect": true,
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving home directory: %v\n", err)
		os.Exit(1)
	}

	args := os.Args[1:]
	binName := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if binName == "ssh-add-host" || binName == "fssh-add" {
		args = append([]string{"add"}, args...)
	}

	update.RemoveStaleBinary()
	os.Exit(run(args, homeDir))
}

// run dispatches the command line and returns the process exit code.
func run(args []string, homeDir string) int {
	if len(args) == 0 {
		return runTUILoop(homeDir)
	}

	cmd := strings.ToLower(args[0])
	switch cmd {
	case "-v", "--version", "version":
		fmt.Printf("sshx %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
		return 0
	case "-h", "--help", "help":
		printUsage()
		return 0
	}
	if subcommands[cmd] && len(args) > 1 && (args[1] == "-h" || args[1] == "--help") {
		printUsage()
		return 0
	}

	switch cmd {
	case "add":
		target := ""
		alias := ""
		if len(args) > 1 {
			target = args[1]
		}
		if len(args) > 2 {
			alias = args[2]
		}
		connectTarget, connectNow, err := wizard.AddHostWizard(target, alias, homeDir, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		if connectNow && connectTarget != "" {
			return connectSSH(connectTarget)
		}
		return 0

	case "ls", "list":
		return listHosts(os.Stdout, homeDir)

	case "rm", "delete", "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshx rm <alias>")
			return 2
		}
		if err := wizard.DeleteHostPrompt(args[1], homeDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting host: %v\n", err)
			return 1
		}
		return 0

	case "clone", "dup", "duplicate":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshx clone <alias>")
			return 2
		}
		if _, err := wizard.CloneHostWizard(args[1], homeDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error cloning host: %v\n", err)
			return 1
		}
		return 0

	case "probe", "ping":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshx probe <alias>")
			return 2
		}
		return probeHostCLI(args[1], homeDir)

	case "update", "upgrade":
		return cmdUpdate(args[1:])

	case "check-update":
		return cmdCheckUpdate(args[1:])

	case "edit":
		if len(args) > 1 {
			if _, err := wizard.EditHostWizard(args[1], homeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error editing host: %v\n", err)
				return 1
			}
			return 0
		}
		openEditor(homeDir)
		return 0

	case "connect":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshx connect <alias|target> [command...]")
			return 2
		}
		return connectSSH(args[1], args[2:]...)
	}

	if strings.HasPrefix(cmd, "-") {
		fmt.Fprintf(os.Stderr, "sshx: unknown option %q\nRun 'sshx --help' for usage.\n", args[0])
		return 2
	}

	hosts, _ := sshconfig.LoadAllHosts(homeDir)
	if h, ok := sshconfig.FindHost(hosts, args[0]); ok {
		return connectSSH(h.Alias, args[1:]...)
	}
	// Anything that looks like a target (user@host, an FQDN or IP) goes straight to ssh,
	// which may still match it against Host patterns.
	if strings.ContainsAny(args[0], "@.:") {
		return connectSSH(args[0], args[1:]...)
	}

	fmt.Fprintf(os.Stderr, "sshx: unknown command or host alias %q\nRun 'sshx ls' to list hosts, or 'sshx connect %s' to pass it to ssh as-is.\n", args[0], args[0])
	return 2
}

// runTUILoop shows the host browser, runs the action it returns, and reopens it until the user connects or quits.
func runTUILoop(homeDir string) int {
	lastSelected := ""
	for {
		choice, action, err := tui.RunTUI(homeDir, Version, lastSelected)
		if err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			return 1
		}
		if choice != "" {
			lastSelected = choice
		}

		switch action {
		case "connect":
			if choice != "" {
				return connectSSH(choice)
			}
			return 0

		case "add":
			addedAlias, _, err := wizard.AddHostWizard("", "", homeDir, false)
			if reportWizardError(err) {
				continue
			}
			if addedAlias != "" {
				lastSelected = addedAlias
			}

		case "edit-host":
			if choice != "" {
				editedAlias, err := wizard.EditHostWizard(choice, homeDir)
				if reportWizardError(err) {
					continue
				}
				if editedAlias != "" {
					lastSelected = editedAlias
				}
			}

		case "clone-host":
			if choice != "" {
				clonedAlias, err := wizard.CloneHostWizard(choice, homeDir)
				if reportWizardError(err) {
					continue
				}
				if clonedAlias != "" {
					lastSelected = clonedAlias
				}
			}

		case "copy-id":
			if choice != "" {
				runCopyIDForHost(choice, homeDir)
				pausePrompt()
			}

		case "edit-config", "edit":
			openEditor(homeDir)

		case "upgrade":
			installed, err := update.Perform(Version, os.Stdout, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nUpdate error: %v\n", err)
				pausePrompt()
			} else if installed != "" {
				updatedBadge := ui.Badge(" UPDATED ", ui.ColorBlack, ui.ColorGreen)
				infoStyle := lipgloss.NewStyle().Foreground(ui.ColorCyan).Bold(true)
				_, _ = lipgloss.Printf("\n%s Successfully updated sshx to %s\n\n", updatedBadge, installed)
				_, _ = lipgloss.Printf("%s Restart sshx to apply the update.\n\n", infoStyle.Render("➜"))
				return 0
			} else {
				pausePrompt()
			}

		default:
			return 0
		}
	}
}

// reportWizardError shows a wizard failure before the TUI redraws over it. It reports whether there was one.
func reportWizardError(err error) bool {
	if err == nil {
		return false
	}
	fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
	pausePrompt()
	return true
}

func pausePrompt() {
	fmt.Print("\nPress Enter to return to sshx...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
}

const updateUsage = "sshx update [--check] [--force]"

func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var check, force bool
	fs.BoolVar(&check, "check", false, "only say whether a newer release exists")
	fs.BoolVar(&check, "c", false, "only say whether a newer release exists")
	fs.BoolVar(&force, "force", false, "reinstall the latest release even when up to date")
	fs.BoolVar(&force, "f", false, "reinstall the latest release even when up to date")
	if err := fs.Parse(args); err != nil || len(fs.Args()) != 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s\n", updateUsage)
		return 2
	}
	if check {
		rel, newer, err := update.Latest(Version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		if newer {
			fmt.Printf("sshx %s is available (installed: %s). Run `sshx update` to install it.\n", rel.TagName, Version)
		} else {
			fmt.Printf("sshx is up to date (%s)\n", Version)
		}
		return 0
	}
	installed, err := update.Perform(Version, os.Stdout, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if installed != "" {
		updatedBadge := ui.Badge(" UPDATED ", ui.ColorBlack, ui.ColorGreen)
		infoStyle := lipgloss.NewStyle().Foreground(ui.ColorCyan).Bold(true)
		_, _ = lipgloss.Printf("\n%s Successfully updated sshx to %s\n\n", updatedBadge, installed)
		_, _ = lipgloss.Printf("%s Restart sshx to apply the update.\n\n", infoStyle.Render("➜"))
	}
	return 0
}

func cmdCheckUpdate(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Usage: sshx check-update\n")
		return 2
	}
	rel, newer, err := update.Latest(Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if newer {
		fmt.Printf("sshx %s is available (installed: %s). Run `sshx update` to install it.\n", rel.TagName, Version)
	} else {
		fmt.Printf("sshx is up to date (%s)\n", Version)
	}
	return 0
}

func printUsage() {
	header := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorCoral).Render("sshx — TUI SSH Connection Manager")
	_, _ = lipgloss.Printf("%s\n\n", header)
	fmt.Println("Usage:")
	fmt.Println("  sshx                          Launch interactive host browser")
	fmt.Println("  sshx <alias> [command...]     Connect to host (optionally run a remote command)")
	fmt.Println("  sshx connect <target> [cmd]   Pass any target straight to ssh")
	fmt.Println("  sshx add [target] [alias]     Interactively add a new SSH host")
	fmt.Println("  sshx ls                       List all configured SSH hosts")
	fmt.Println("  sshx rm <alias>               Remove a host from its SSH config file")
	fmt.Println("  sshx edit [alias]             Edit host in wizard, or open ~/.ssh/config in $EDITOR")
	fmt.Println("  sshx clone <alias>            Duplicate / clone an existing host")
	fmt.Println("  sshx probe <alias>            Probe TCP reachability / ping host")
	fmt.Println("  sshx update [--check]         Check for and install latest release update")
	fmt.Println()
	fmt.Println("TUI Keybindings:")
	fmt.Println("  Enter      Connect to selected host")
	fmt.Println("  /          Filter / search hosts")
	fmt.Println("  a          Add new host (launches wizard)")
	fmt.Println("  e          Edit selected host (launches wizard)")
	fmt.Println("  D          Duplicate / clone selected host")
	fmt.Println("  d, x       Delete selected host")
	fmt.Println("  c          Copy public key to host (ssh-copy-id)")
	fmt.Println("  y          Yank SSH connect command to clipboard")
	fmt.Println("  p          Probe TCP reachability / ping host")
	fmt.Println("  v          Toggle raw OpenSSH config view")
	fmt.Println("  U          Upgrade sshx to latest release")
	fmt.Println("  E          Open ~/.ssh/config in $EDITOR")
	fmt.Println("  Tab        Toggle details inspector (on compact displays)")
	fmt.Println("  q, Esc     Quit")
}

func listHosts(w io.Writer, homeDir string) int {
	hosts, err := sshconfig.LoadAllHosts(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading hosts: %v\n", err)
		return 1
	}

	if len(hosts) == 0 {
		_, _ = fmt.Fprintln(w, "No configured SSH hosts found in ~/.ssh/config.")
		return 0
	}
	writeHostTable(w, hosts)
	return 0
}

// writeHostTable prints hosts as aligned columns, padding by display width so colours and wide characters line up.
func writeHostTable(w io.Writer, hosts []sshconfig.HostItem) {
	headers := []string{"ALIAS", "TARGET", "PORT", "AUTH"}
	rows := make([][]string, 0, len(hosts))
	for _, h := range hosts {
		target := h.HostName
		if target == "" {
			target = h.Alias
		}
		if h.User != "" {
			target = h.User + "@" + target
		}
		auth := "Default"
		if !h.PubkeyAuth || h.PasswordAuth {
			auth = "Password"
		} else if h.IdentityFile != "" {
			auth = filepath.Base(h.IdentityFile)
		}
		rows = append(rows, []string{h.Alias, target, strconv.Itoa(h.Port), auth})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}

	render := func(cells []string, style *lipgloss.Style) string {
		out := make([]string, len(cells))
		for i, c := range cells {
			if i < len(cells)-1 {
				c += strings.Repeat(" ", widths[i]-lipgloss.Width(c))
			}
			if style != nil {
				c = style.Render(c)
			}
			out[i] = c
		}
		return strings.Join(out, "  ")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorPurple)
	total := 2 * (len(widths) - 1)
	for _, wd := range widths {
		total += wd
	}

	_, _ = lipgloss.Fprintln(w, render(headers, &headerStyle))
	_, _ = fmt.Fprintln(w, strings.Repeat("─", total))
	for _, r := range rows {
		_, _ = fmt.Fprintln(w, render(r, nil))
	}
}

// connectSSH runs ssh against target, forwarding any remote command, and returns ssh's exit code.
func connectSSH(target string, command ...string) int {
	if len(command) == 0 {
		fmt.Fprintf(os.Stderr, "Connecting to %s...\n\n", target)
	}
	cmd := exec.Command("ssh", append([]string{target}, command...)...) //nolint:gosec // intentional user SSH connection
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if code := exitErr.ExitCode(); code > 0 {
				return code
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "sshx: failed to run ssh: %v\n", err)
		return 1
	}
	return 0
}

// openEditor opens ~/.ssh/config in $VISUAL or $EDITOR. On Unix the editor
// command runs through sh, like git does, so values such as "code -w" work.
func openEditor(homeDir string) {
	configPath := filepath.Join(homeDir, ".ssh", "config")
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		fallbacks := []string{"nvim", "vim", "vi", "nano"}
		if runtime.GOOS == "windows" {
			fallbacks = []string{"notepad"}
		}
		for _, candidate := range fallbacks {
			if _, err := exec.LookPath(candidate); err == nil {
				editor = candidate
				break
			}
		}
	}
	if editor == "" {
		fmt.Fprintln(os.Stderr, "No editor found; set $EDITOR to edit your SSH config.")
		return
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		fields := strings.Fields(editor)
		cmd = exec.Command(fields[0], append(fields[1:], configPath)...) //nolint:gosec // intentional editor launch
	} else {
		cmd = exec.Command("sh", "-c", editor+` "$1"`, "sh", configPath) //nolint:gosec // intentional editor launch
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Editor exited with an error: %v\n", err)
	}
}

func runCopyIDForHost(alias, homeDir string) {
	hosts, _ := sshconfig.LoadAllHosts(homeDir)
	targetHost, ok := sshconfig.FindHost(hosts, alias)
	if !ok {
		fmt.Printf("Host '%s' not found.\n", alias)
		return
	}

	key := targetHost.IdentityFile
	if key == "" {
		keys, _ := sshconfig.DiscoverKeys(filepath.Join(homeDir, ".ssh"))
		if len(keys) > 0 {
			key = keys[0]
		}
	}
	if key == "" {
		fmt.Println("No SSH key found to copy; add one with 'sshx edit' or ssh-keygen.")
		return
	}

	pubKey := sshconfig.ExpandHome(key, homeDir) + ".pub"
	if _, err := os.Stat(pubKey); err != nil {
		fmt.Printf("Public key %s not found.\n", pubKey)
		return
	}

	if err := wizard.CopyPublicKey(pubKey, targetHost.Alias); err != nil {
		fmt.Fprintf(os.Stderr, "Copying public key failed: %v\n", err)
	}
}

func probeHostCLI(alias, homeDir string) int {
	hosts, err := sshconfig.LoadAllHosts(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading hosts: %v\n", err)
		return 1
	}

	targetHost, ok := sshconfig.FindHost(hosts, alias)
	if !ok {
		fmt.Fprintf(os.Stderr, "Host '%s' not found in SSH configuration.\n", alias)
		return 1
	}

	addr, via := probe.Address(targetHost, hosts)
	if via != "" {
		fmt.Printf("%s is behind ProxyJump %s; probing the jump host (%s)...\n", targetHost.Alias, via, addr)
	} else {
		fmt.Printf("Probing TCP reachability for %s (%s)...\n", targetHost.Alias, addr)
	}

	latency, err := probe.TCP(addr, 2*time.Second)
	if err != nil {
		_, _ = lipgloss.Printf("\n%s Failed to connect to %s: %v\n", ui.Badge(" UNREACHABLE ", ui.ColorWhite, ui.ColorRed), addr, err)
		return 1
	}
	_, _ = lipgloss.Printf("\n%s Connected to %s in %dms\n", ui.Badge(" REACHABLE ", ui.ColorBlack, ui.ColorGreen), addr, latency.Milliseconds())
	return 0
}
