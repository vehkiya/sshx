package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHighlightConfigBlockWithComments(t *testing.T) {
	raw := `# Top level note
Host test-srv
    # Indented directive comment
    HostName 10.0.0.1
    Port 22`

	highlighted := ansi.Strip(HighlightConfigBlock(raw))
	if !strings.Contains(highlighted, "# Top level note") {
		t.Errorf("expected top level note in output")
	}
	if !strings.Contains(highlighted, "Host test-srv") {
		t.Errorf("expected Host test-srv in output")
	}
	if !strings.Contains(highlighted, "# Indented directive comment") {
		t.Errorf("expected indented comment in output")
	}
}
