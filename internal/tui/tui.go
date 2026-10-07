package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/vehkiya/sshx/internal/probe"
	"github.com/vehkiya/sshx/internal/sshconfig"
	"github.com/vehkiya/sshx/internal/ui"
	"github.com/vehkiya/sshx/internal/update"
)

type HostItem = sshconfig.HostItem

var (
	colorPurple    = ui.ColorPurple
	colorCoral     = ui.ColorCoral
	colorCyan      = ui.ColorCyan
	colorGreen     = ui.ColorGreen
	colorAmber     = ui.ColorAmber
	colorRed       = ui.ColorRed
	colorWhite     = ui.ColorWhite
	colorBlack     = ui.ColorBlack
	colorLightGray = ui.ColorLightGray
	colorGray      = ui.ColorGray
	colorDim       = ui.ColorDim
	colorDarkGray  = ui.ColorDarkGray
	colorSlate     = ui.ColorSlate
)

func highlightConfigBlock(block string) string {
	return ui.HighlightConfigBlock(block)
}

func probeAddress(h HostItem, hosts []HostItem) (addr, via string) {
	return probe.Address(h, hosts)
}

func probeTCP(addr string, timeout time.Duration) (time.Duration, error) {
	return probe.TCP(addr, timeout)
}

var (
	LoadAllHosts             = sshconfig.LoadAllHosts
	ResolveHostConfigFile    = sshconfig.ResolveHostConfigFile
	DeleteHostFromConfigFile = sshconfig.DeleteHostFromConfigFile
	HasHost                  = sshconfig.HasHost
	shortenHome              = sshconfig.ShortenHome
)

var (
	// Brand and header styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(colorPurple). // Charm Purple
			Padding(0, 1)

	// Panes
	rightPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Padding(1, 2)

	detailBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Padding(0, 1).
			MarginTop(1)

	// Inspector elements
	inspectorTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCoral). // Coral Pink
			MarginBottom(1)

	cardLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan). // Vibrant Cyan
			Width(13)

	cardValue = lipgloss.NewStyle().
			Foreground(colorLightGray)

	cmdPreviewStyle = lipgloss.NewStyle().
			Foreground(colorGreen). // Soft Green
			Background(colorDarkGray).
			Padding(0, 1).
			MarginTop(1).
			MarginBottom(1)

	// Badges
	keyBadge = lipgloss.NewStyle().
			Foreground(colorBlack).
			Background(colorGreen).
			Bold(true).
			Padding(0, 1)

	passwordBadge = lipgloss.NewStyle().
			Foreground(colorBlack).
			Background(colorAmber).
			Bold(true).
			Padding(0, 1)

	defaultBadge = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorSlate).
			Bold(true).
			Padding(0, 1)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)
)

type customDelegate struct {
	list.DefaultDelegate
}

func newCustomDelegate(isDark bool) customDelegate {
	d := list.NewDefaultDelegate()
	d.Styles = list.NewDefaultItemStyles(isDark)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(colorCoral).
		BorderLeftForeground(colorPurple).
		Bold(true)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(colorWhite).
		BorderLeftForeground(colorPurple)
	return customDelegate{DefaultDelegate: d}
}

// styleList gives the host list the sshx palette over the defaults for a dark
// or light terminal. It starts dark and changes once the terminal says
// otherwise (tea.BackgroundColorMsg).
func styleList(l *list.Model, isDark bool) {
	l.SetDelegate(newCustomDelegate(isDark))
	l.Styles = list.DefaultStyles(isDark)
	l.Styles.Title = titleStyle
}

// copyToClipboard copies text to the system clipboard via OSC 52 and CLI tools.
// It is a variable so tests can avoid touching the real clipboard.
var copyToClipboard = func(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)

	if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd := exec.Command("wl-copy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	} else if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	} else if _, err := exec.LookPath("xsel"); err == nil {
		cmd := exec.Command("xsel", "--clipboard", "--input")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	} else if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	} else if _, err := exec.LookPath("clip.exe"); err == nil {
		cmd := exec.Command("clip.exe")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	}
}

type pingResultMsg struct {
	alias   string
	via     string // jump host probed in place of a host behind ProxyJump
	latency time.Duration
	err     error
}

func checkReachabilityCmd(alias, addr, via string) tea.Cmd {
	return func() tea.Msg {
		latency, err := probeTCP(addr, 1500*time.Millisecond)
		return pingResultMsg{alias: alias, via: via, latency: latency, err: err}
	}
}

// pingStatusText describes a probe result for the inspector.
func pingStatusText(msg pingResultMsg) string {
	subject := "Reachable"
	if msg.via != "" {
		subject = fmt.Sprintf("Jump host %s reachable", msg.via)
	}
	switch {
	case msg.err == nil:
		return fmt.Sprintf("🟢 %s (%dms)", subject, msg.latency.Milliseconds())
	case os.IsTimeout(msg.err) || strings.Contains(strings.ToLower(msg.err.Error()), "timeout"):
		if msg.via != "" {
			return fmt.Sprintf("🔴 Jump host %s timeout (>1.5s)", msg.via)
		}
		return "🔴 Timeout (>1.5s)"
	case msg.via != "":
		return fmt.Sprintf("🔴 Jump host %s unreachable", msg.via)
	default:
		return "🔴 Unreachable"
	}
}

func hostsFromItems(items []list.Item) []HostItem {
	hosts := make([]HostItem, 0, len(items))
	for _, it := range items {
		if h, ok := it.(HostItem); ok {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

type clearStatusMsg struct{}

func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

type updateCheckMsg struct {
	latestVersion string
	isAvailable   bool
}

func checkUpdateCmd(currentVersion, homeDir string) tea.Cmd {
	if update.CheckDisabled(currentVersion) {
		return nil
	}
	return func() tea.Msg {
		latest, isNewer, err := update.CheckLatestReleaseCached(currentVersion, homeDir)
		if err != nil || !isNewer {
			return updateCheckMsg{latestVersion: latest, isAvailable: false}
		}
		return updateCheckMsg{latestVersion: latest, isAvailable: true}
	}
}

type listKeyMap struct {
	connect    key.Binding
	add        key.Binding
	edit       key.Binding
	clone      key.Binding
	openEditor key.Binding
	delete     key.Binding
	copyID     key.Binding
	yank       key.Binding
	ping       key.Binding
	viewRaw    key.Binding
	toggleTab  key.Binding
	upgrade    key.Binding
}

func newListKeyMap() *listKeyMap {
	return &listKeyMap{
		connect: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "connect"),
		),
		add: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add"),
		),
		edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		clone: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "clone"),
		),
		openEditor: key.NewBinding(
			key.WithKeys("E"),
			key.WithHelp("E", "$EDITOR"),
		),
		delete: key.NewBinding(
			key.WithKeys("d", "x"),
			key.WithHelp("d", "delete"),
		),
		copyID: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "key"),
		),
		yank: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "yank"),
		),
		ping: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "ping"),
		),
		viewRaw: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "raw"),
		),
		toggleTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "toggle view"),
		),
		upgrade: key.NewBinding(
			key.WithKeys("U"),
			key.WithHelp("U", "upgrade"),
		),
	}
}

type model struct {
	list            list.Model
	keys            *listKeyMap
	choice          string
	action          string
	width           int
	height          int
	quitting        bool
	showDetails     bool
	showRaw         bool
	confirmDelete   bool
	statusMessage   string
	pingStatus      map[string]string
	updateAvailable string
	homeDir         string
	version         string
}

// Init asks for the terminal's background color and checks for a newer release.
func (m model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, checkUpdateCmd(m.version, m.homeDir))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		h := m.height - 3
		if h < 5 {
			h = 5
		}
		if m.width >= 100 {
			listWidth := m.width * 52 / 100
			m.list.SetSize(listWidth, h)
		} else {
			listWidth := m.width - 2
			if listWidth < 20 {
				listWidth = 20
			}
			m.list.SetSize(listWidth, h)
		}
		return m, nil

	case pingResultMsg:
		if m.pingStatus == nil {
			m.pingStatus = make(map[string]string)
		}
		m.pingStatus[msg.alias] = pingStatusText(msg)
		return m, nil

	case updateCheckMsg:
		if msg.isAvailable {
			m.updateAvailable = msg.latestVersion
		}
		return m, nil

	case clearStatusMsg:
		m.statusMessage = ""
		return m, nil

	case tea.BackgroundColorMsg:
		styleList(&m.list, msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		if m.confirmDelete {
			switch msg.String() {
			case "y", "Y":
				m.confirmDelete = false
				selected, ok := m.list.SelectedItem().(HostItem)
				if !ok {
					return m, nil
				}
				configPath, err := ResolveHostConfigFile(selected.Alias, m.homeDir, selected.ConfigFile)
				if err == nil {
					err = DeleteHostFromConfigFile(strings.Join(selected.AllAliases, " "), configPath)
				}
				if err != nil {
					m.statusMessage = fmt.Sprintf("✘ Error deleting '%s': %v", selected.Alias, err)
					return m, clearStatusCmd()
				}
				m.statusMessage = fmt.Sprintf("✔ Deleted '%s'", selected.Alias)
				// RemoveItem is unreliable while a filter is applied, so rebuild the
				// items and let the list re-run the active filter.
				idx := m.list.GlobalIndex()
				items := append([]list.Item(nil), m.list.Items()...)
				items = append(items[:idx], items[idx+1:]...)
				return m, tea.Batch(m.list.SetItems(items), clearStatusCmd())
			case "n", "N", "esc", "q":
				m.confirmDelete = false
				m.statusMessage = "Deletion cancelled"
				return m, clearStatusCmd()
			case "ctrl+c":
				m.quitting = true
				m.action = "quit"
				return m, tea.Quit
			default:
				return m, nil
			}
		}

		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, m.keys.toggleTab):
			if m.width < 100 {
				m.showDetails = !m.showDetails
				return m, nil
			}

		case key.Matches(msg, m.keys.connect):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "connect"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.add):
			m.action = "add"
			return m, tea.Quit

		case key.Matches(msg, m.keys.edit):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "edit-host"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.clone):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "clone-host"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.openEditor):
			m.action = "edit-config"
			return m, tea.Quit

		case key.Matches(msg, m.keys.delete):
			if len(m.list.Items()) > 0 {
				m.confirmDelete = true
				return m, nil
			}

		case key.Matches(msg, m.keys.copyID):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "copy-id"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.yank):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				cmdStr := fmt.Sprintf("ssh %s", selected.Alias)
				copyToClipboard(cmdStr)
				m.statusMessage = fmt.Sprintf("✔ Copied '%s' to clipboard", cmdStr)
				return m, clearStatusCmd()
			}

		case key.Matches(msg, m.keys.ping):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				if m.pingStatus == nil {
					m.pingStatus = make(map[string]string)
				}
				m.pingStatus[selected.Alias] = "⏳ Probing..."
				addr, via := probeAddress(selected, hostsFromItems(m.list.Items()))
				return m, checkReachabilityCmd(selected.Alias, addr, via)
			}

		case key.Matches(msg, m.keys.viewRaw):
			m.showRaw = !m.showRaw
			return m, nil

		case key.Matches(msg, m.keys.upgrade):
			m.action = "upgrade"
			return m, tea.Quit

		case msg.String() == "q" || msg.String() == "ctrl+c" || (msg.String() == "esc" && m.list.FilterState() == list.Unfiltered):
			m.quitting = true
			m.action = "quit"
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View draws the browser on the alternate screen.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	if m.quitting {
		return ""
	}

	// 1. Empty state
	if len(m.list.Items()) == 0 {
		cardContent := lipgloss.NewStyle().Bold(true).Foreground(colorCoral).Render("📡 No SSH Hosts Found\n\n") +
			lipgloss.NewStyle().Foreground(colorLightGray).Render("No configured hosts found in ~/.ssh/config.\n\n") +
			lipgloss.NewStyle().Foreground(colorCyan).Render("[a] Add your first host\n") +
			lipgloss.NewStyle().Foreground(colorAmber).Render("[E] Open ~/.ssh/config in $EDITOR\n")
		if m.updateAvailable != "" {
			cardContent += lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render(fmt.Sprintf("[U] Upgrade sshx to %s\n", m.updateAvailable))
		}
		cardContent += lipgloss.NewStyle().Foreground(colorDim).Render("[q] Quit")

		if m.statusMessage != "" {
			cardContent = statusStyle(m.statusMessage).Render(m.statusMessage+"\n\n") + cardContent
		}

		emptyCard := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Padding(2, 4).
			Align(lipgloss.Center).
			Render(cardContent)

		if m.width > 0 && m.height > 0 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, emptyCard)
		}
		return emptyCard
	}

	selected, hasSelection := m.list.SelectedItem().(HostItem)
	if !hasSelection {
		return m.list.View()
	}

	// Confirmation banner if deleting
	if m.confirmDelete {
		displayFile := shortenHome(selected.ConfigFile, m.homeDir)
		delPrompt := lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(colorRed).
			Padding(0, 2).
			Render(fmt.Sprintf("⚠️  Delete host '%s' from %s? [y/N]", selected.Alias, displayFile))

		return lipgloss.JoinVertical(lipgloss.Left, m.list.View(), "\n"+delPrompt)
	}

	inspectorContent := renderInspector(selected, m.homeDir, m.showRaw, m.pingStatus[selected.Alias], m.statusMessage, m.updateAvailable)

	// Responsive layout: Side-by-side when terminal width >= 100 columns
	if m.width >= 100 {
		listWidth := m.width * 52 / 100
		rightWidth := m.width - listWidth - 6
		if rightWidth < 35 {
			rightWidth = 35
		}

		maxH := m.height - 3
		if maxH < 5 {
			maxH = 5
		}

		leftView := m.list.View()
		rightView := rightPaneStyle.
			Width(rightWidth).
			MaxHeight(maxH).
			Render(inspectorContent)

		return lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
	}

	// Compact mode (< 100 cols): Tab toggles between list view and inspector view
	if m.showDetails {
		maxH := m.height - 3
		if maxH < 5 {
			maxH = 5
		}
		header := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("⇥ Press [Tab] to return to Host List\n\n")
		return detailBoxStyle.Width(m.width - 4).MaxHeight(maxH).Render(header + inspectorContent)
	}

	if m.statusMessage != "" {
		toast := statusStyle(m.statusMessage).Render("  " + m.statusMessage)
		return lipgloss.JoinVertical(lipgloss.Left, m.list.View(), toast)
	}

	return m.list.View()
}

// statusStyle colours a status toast: red for failures, green otherwise.
func statusStyle(msg string) lipgloss.Style {
	color := colorGreen
	if strings.HasPrefix(msg, "✘") {
		color = colorRed
	}
	return lipgloss.NewStyle().Bold(true).Foreground(color)
}

func renderInspector(h HostItem, homeDir string, showRaw bool, pingStatus, statusMsg, updateAvailable string) string {
	var sb strings.Builder

	// Title
	sb.WriteString(inspectorTitle.Render(fmt.Sprintf("📡 Host: %s", h.Alias)))
	sb.WriteString("\n")

	// Update Notification Banner if a new version is available
	if updateAvailable != "" {
		updateBadge := lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlack).
			Background(colorCyan).
			Padding(0, 1).
			Render(fmt.Sprintf("↑ UPDATE %s AVAILABLE", updateAvailable))
		sb.WriteString(updateBadge + " " + dimStyle.Render("Press [U] to upgrade sshx") + "\n\n")
	}

	// If Raw mode is toggled, show the syntax-highlighted raw OpenSSH configuration block!
	if showRaw {
		sb.WriteString(dimStyle.Render("── Raw OpenSSH Configuration ───────────\n"))
		rawText := strings.Join(h.RawLines, "\n")
		if strings.TrimSpace(rawText) == "" {
			rawText = strings.TrimRight(h.Entry().Format(), "\n")
		}
		sb.WriteString(highlightConfigBlock(rawText))
		sb.WriteString("\n\n" + dimStyle.Render("── Quick Actions ────────────────────────\n"))
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("[v] Formatted View"))
		return sb.String()
	}

	// Command preview
	cmdStr := fmt.Sprintf("ssh %s", h.Alias)
	sb.WriteString(cmdPreviewStyle.Render(cmdStr))
	if statusMsg != "" {
		sb.WriteString("  " + statusStyle(statusMsg).Render(statusMsg))
	}
	sb.WriteString("\n\n")

	// Notes/Comments if present
	if h.Notes != "" {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Notes:"), lipgloss.NewStyle().Italic(true).Foreground(colorAmber).Render(h.Notes))
	}

	// Target line
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Target:"), cardValue.Render(h.Target()))

	// HostName & User
	if h.HostName != "" {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("HostName:"), cardValue.Render(h.HostName))
	} else {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("HostName:"), dimStyle.Render("not set (ssh connects to the alias)"))
	}
	if h.User != "" {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("User:"), cardValue.Render(h.User))
	}
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Port:"), cardValue.Render(fmt.Sprintf("%d", h.Port)))

	// Reachability / Ping
	pingDisplay := "[p] Probe TCP"
	if pingStatus != "" {
		pingDisplay = pingStatus
	}
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Reachability:"), cardValue.Render(pingDisplay))

	// Auth Badge & Details
	var badge string
	var authDetail string
	if !h.PubkeyAuth || h.PasswordAuth {
		badge = passwordBadge.Render("PASSWORD")
		authDetail = "Pubkey disabled (prevents agent lockouts)"
	} else if h.IdentityFile != "" {
		badge = keyBadge.Render("KEY")
		authDetail = h.IdentityFile
	} else {
		badge = defaultBadge.Render("DEFAULT")
		authDetail = "Agent keys with password fallback"
	}
	fmt.Fprintf(&sb, "%s %s %s\n", cardLabel.Render("Auth:"), badge, dimStyle.Render(authDetail))

	if h.IdentitiesOnly {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Identities:"), cardValue.Render("IdentitiesOnly yes"))
	}

	if h.ProxyJump != "" {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("ProxyJump:"), cardValue.Render(h.ProxyJump))
	}

	if len(h.AllAliases) > 1 {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Aliases:"), cardValue.Render(strings.Join(h.AllAliases, ", ")))
	}

	// Config source file
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Config File:"), dimStyle.Render(shortenHome(h.ConfigFile, homeDir)))

	sb.WriteString("\n" + dimStyle.Render("── Quick Actions ────────────────────────"))
	fmt.Fprintf(&sb, "\n%s  %s  %s  %s  %s  %s  %s  %s  %s",
		lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("[Enter] Connect"),
		lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("[a] Add"),
		lipgloss.NewStyle().Bold(true).Foreground(colorPurple).Render("[e] Edit"),
		lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("[D] Clone"),
		lipgloss.NewStyle().Bold(true).Foreground(colorAmber).Render("[c] Key"),
		lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("[y] Yank"),
		lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("[p] Ping"),
		lipgloss.NewStyle().Bold(true).Foreground(colorCoral).Render("[v] Raw"),
		lipgloss.NewStyle().Bold(true).Foreground(colorGray).Render("[E] Config"),
	)
	if updateAvailable != "" {
		fmt.Fprintf(&sb, "  %s", lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("[U] Upgrade"))
	}

	return sb.String()
}

// RunTUI launches the interactive host browser and returns the selected alias and requested action.
func RunTUI(homeDir string, version string, initialAlias ...string) (selectedAlias string, action string, err error) {
	hosts, err := LoadAllHosts(homeDir)
	if err != nil {
		return "", "", err
	}

	targetAlias := ""
	if len(initialAlias) > 0 {
		targetAlias = initialAlias[0]
	}

	items := make([]list.Item, len(hosts))
	selectedIdx := 0
	for i, h := range hosts {
		items[i] = h
		if targetAlias != "" && strings.EqualFold(h.Alias, targetAlias) {
			selectedIdx = i
		}
	}

	l := list.New(items, newCustomDelegate(true), 80, 20)
	l.Title = "sshx"
	styleList(&l, true)

	keys := newListKeyMap()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{keys.connect, keys.add, keys.edit, keys.clone, keys.yank, keys.ping}
	}
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{keys.connect, keys.add, keys.edit, keys.clone, keys.delete, keys.copyID, keys.yank, keys.ping, keys.viewRaw, keys.openEditor, keys.toggleTab, keys.upgrade}
	}

	l.KeyMap.Quit.SetKeys("q", "esc")
	l.KeyMap.Quit.SetHelp("q/esc", "quit")

	if selectedIdx > 0 && selectedIdx < len(items) {
		l.Select(selectedIdx)
	}

	m := model{
		list:       l,
		keys:       keys,
		homeDir:    homeDir,
		version:    version,
		width:      80,
		height:     20,
		pingStatus: make(map[string]string),
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", "", err
	}

	fm, ok := finalModel.(model)
	if !ok {
		return "", "quit", nil
	}

	return fm.choice, fm.action, nil
}
