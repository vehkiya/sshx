package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// HighlightConfigBlock syntax-highlights an OpenSSH configuration snippet.
func HighlightConfigBlock(block string) string {
	var lines []string
	kwHost := lipgloss.NewStyle().Bold(true).Foreground(ColorCoral)
	valHost := lipgloss.NewStyle().Bold(true).Foreground(ColorWhite)
	kwDirective := lipgloss.NewStyle().Bold(true).Foreground(ColorCyan)
	valDirective := lipgloss.NewStyle().Foreground(ColorLightGray)
	commentStyle := lipgloss.NewStyle().Italic(true).Foreground(ColorDim)

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
