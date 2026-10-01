package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Brand and header styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#7D56F4")). // Charm Purple
			Padding(0, 1)

	// Panes
	rightPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)

	detailBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginTop(1)

	// Inspector elements
	inspectorTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5F87")). // Coral Pink
			MarginBottom(1)

	cardLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00D7D7")). // Vibrant Cyan
			Width(13)

	cardValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EEEEEE"))

	cmdPreviewStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5FD787")). // Soft Green
			Background(lipgloss.Color("#262626")).
			Padding(0, 1).
			MarginTop(1).
			MarginBottom(1)

	// Badges
	keyBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#5FD787")).
			Bold(true).
			Padding(0, 1)

	passwordBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFAF00")).
			Bold(true).
			Padding(0, 1)

	defaultBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5F87AF")).
			Bold(true).
			Padding(0, 1)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#767676"))
)

type customDelegate struct {
	list.DefaultDelegate
}

func newCustomDelegate() customDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#FF5F87")).
		BorderLeftForeground(lipgloss.Color("#7D56F4")).
		Bold(true)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(lipgloss.Color("#FFFFFF")).
		BorderLeftForeground(lipgloss.Color("#7D56F4"))
	return customDelegate{DefaultDelegate: d}
}

type listKeyMap struct {
	connect key.Binding
	add     key.Binding
	delete  key.Binding
	copyID  key.Binding
	edit    key.Binding
}

func newListKeyMap() *listKeyMap {
	return &listKeyMap{
		connect: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "connect"),
		),
		add: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add host"),
		),
		delete: key.NewBinding(
			key.WithKeys("d", "x"),
			key.WithHelp("d", "delete"),
		),
		copyID: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "ssh-copy-id"),
		),
		edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit config"),
		),
	}
}

type model struct {
	list     list.Model
	keys     *listKeyMap
	choice   string
	action   string
	width    int
	height   int
	quitting bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if m.width >= 100 {
			listWidth := m.width * 52 / 100
			m.list.SetSize(listWidth, m.height-3)
		} else {
			m.list.SetSize(m.width, m.height/2)
		}
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, m.keys.connect):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "connect"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.add):
			m.action = "add"
			return m, tea.Quit

		case key.Matches(msg, m.keys.delete):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "delete"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.copyID):
			if selected, ok := m.list.SelectedItem().(HostItem); ok {
				m.choice = selected.Alias
				m.action = "copy-id"
				return m, tea.Quit
			}

		case key.Matches(msg, m.keys.edit):
			m.action = "edit"
			return m, tea.Quit

		case msg.String() == "q" || msg.String() == "ctrl+c":
			m.quitting = true
			m.action = "quit"
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	selected, hasSelection := m.list.SelectedItem().(HostItem)
	if !hasSelection {
		return m.list.View()
	}

	inspectorContent := renderInspector(selected)

	// Responsive layout: Side-by-side when terminal width >= 100 columns
	if m.width >= 100 {
		listWidth := m.width * 52 / 100
		rightWidth := m.width - listWidth - 6
		if rightWidth < 35 {
			rightWidth = 35
		}

		leftView := m.list.View()
		rightView := rightPaneStyle.
			Width(rightWidth).
			Height(m.height - 4).
			Render(inspectorContent)

		return lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
	}

	// Stacked layout for smaller terminals
	stackedBox := detailBoxStyle.Width(m.width - 4).Render(inspectorContent)
	return lipgloss.JoinVertical(lipgloss.Left, m.list.View(), stackedBox)
}

func renderInspector(h HostItem) string {
	var sb strings.Builder

	// Title
	sb.WriteString(inspectorTitle.Render(fmt.Sprintf("📡 Host: %s", h.Alias)))
	sb.WriteString("\n")

	// Command preview
	cmdStr := fmt.Sprintf("ssh %s", h.Alias)
	sb.WriteString(cmdPreviewStyle.Render(cmdStr))
	sb.WriteString("\n\n")

	// Target line
	targetStr := h.HostName
	if h.User != "" {
		targetStr = h.User + "@" + targetStr
	}
	if h.Port > 0 && h.Port != 22 {
		targetStr += fmt.Sprintf(":%d", h.Port)
	}
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Target:"), cardValue.Render(targetStr))

	// HostName & User
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("HostName:"), cardValue.Render(h.HostName))
	if h.User != "" {
		fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("User:"), cardValue.Render(h.User))
	}
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Port:"), cardValue.Render(fmt.Sprintf("%d", h.Port)))

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
	relFile := h.ConfigFile
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(h.ConfigFile, home) {
		relFile = "~" + h.ConfigFile[len(home):]
	}
	fmt.Fprintf(&sb, "%s %s\n", cardLabel.Render("Config File:"), dimStyle.Render(relFile))

	sb.WriteString("\n" + dimStyle.Render("── Quick Actions ────────────────────────"))
	fmt.Fprintf(&sb, "\n%s  %s  %s  %s  %s",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#5FD787")).Render("[Enter] Connect"),
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7D7")).Render("[a] Add"),
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAF00")).Render("[c] Copy Key"),
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F87")).Render("[d] Delete"),
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8A8A8A")).Render("[e] Edit"),
	)

	return sb.String()
}

// RunTUI launches the interactive host browser and returns the selected alias and requested action.
func RunTUI(homeDir string) (selectedAlias string, action string, err error) {
	hosts, err := LoadAllHosts(homeDir)
	if err != nil {
		return "", "", err
	}

	items := make([]list.Item, len(hosts))
	for i, h := range hosts {
		items[i] = h
	}

	delegate := newCustomDelegate()
	l := list.New(items, delegate, 80, 20)
	l.Title = "sshx"
	l.Styles.Title = titleStyle

	keys := newListKeyMap()
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{keys.connect, keys.add, keys.delete, keys.copyID, keys.edit}
	}
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{keys.connect, keys.add, keys.delete, keys.copyID, keys.edit}
	}

	m := model{
		list: l,
		keys: keys,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
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

// DeleteHostPrompt prompts for confirmation and strips a host from ~/.ssh/config.
func DeleteHostPrompt(alias, homeDir string) error {
	var confirm bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Delete host block for '%s' from ~/.ssh/config?", alias)).
				Description("This action permanently removes the Host configuration entry").
				Value(&confirm),
		).Title("Confirm Host Deletion"),
	).WithTheme(customHuhTheme())

	if err := form.Run(); err != nil || !confirm {
		return nil
	}

	configPath := filepath.Join(homeDir, ".ssh", "config")
	data, err := os.ReadFile(filepath.Clean(configPath)) //nolint:gosec // user SSH config file path
	if err != nil {
		return err
	}

	cleaned := RemoveHost(string(data), alias)
	if err := AtomicWrite(configPath, []byte(cleaned), 0600); err != nil {
		return err
	}

	delBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#FF5F87")).
		Padding(0, 1).
		Render(" DELETED ")
	fmt.Printf("\n%s Removed '%s' from %s\n\n", delBadge, alias, configPath)
	return nil
}
