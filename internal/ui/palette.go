package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The sshx palette, as defined in AGENTS.md.
var (
	ColorPurple = lipgloss.Color("#7D56F4") // brand / accent
	ColorCoral  = lipgloss.Color("#FF5F87") // headers / selections
	ColorCyan   = lipgloss.Color("#00D7D7") // prompts / cursors / keys
	ColorGreen  = lipgloss.Color("#5FD787") // success / badges
	ColorAmber  = lipgloss.Color("#FFAF00") // warnings / password
	ColorRed    = lipgloss.Color("#FF4672") // errors / destructive actions

	ColorWhite     = lipgloss.Color("#FFFFFF")
	ColorBlack     = lipgloss.Color("#000000")
	ColorLightGray = lipgloss.Color("#EEEEEE")
	ColorGray      = lipgloss.Color("#8A8A8A")
	ColorDim       = lipgloss.Color("#767676")
	ColorSeparator = lipgloss.Color("#444444")
	ColorDarkGray  = lipgloss.Color("#262626")
	ColorSlate     = lipgloss.Color("#5F87AF")
)

// Badge renders a bold label on a solid background.
func Badge(text string, fg, bg color.Color) string {
	return lipgloss.NewStyle().Bold(true).Foreground(fg).Background(bg).Padding(0, 1).Render(text)
}
