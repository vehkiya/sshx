package main

import "github.com/charmbracelet/lipgloss"

// The sshx palette, as defined in AGENTS.md.
var (
	colorPurple = lipgloss.Color("#7D56F4") // brand / accent
	colorCoral  = lipgloss.Color("#FF5F87") // headers / selections
	colorCyan   = lipgloss.Color("#00D7D7") // prompts / cursors / keys
	colorGreen  = lipgloss.Color("#5FD787") // success / badges
	colorAmber  = lipgloss.Color("#FFAF00") // warnings / password
	colorRed    = lipgloss.Color("#FF4672") // errors / destructive actions

	colorWhite     = lipgloss.Color("#FFFFFF")
	colorBlack     = lipgloss.Color("#000000")
	colorLightGray = lipgloss.Color("#EEEEEE")
	colorGray      = lipgloss.Color("#8A8A8A")
	colorDim       = lipgloss.Color("#767676")
	colorSeparator = lipgloss.Color("#444444")
	colorDarkGray  = lipgloss.Color("#262626")
	colorSlate     = lipgloss.Color("#5F87AF")
)

// badge renders a bold label on a solid background.
func badge(text string, fg, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Bold(true).Foreground(fg).Background(bg).Padding(0, 1).Render(text)
}
