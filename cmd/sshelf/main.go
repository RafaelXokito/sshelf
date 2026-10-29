package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RafaelXokito/sshelf/internal/hosts"
	"github.com/RafaelXokito/sshelf/internal/ui"
)

func main() {
	// Placeholder data until the encrypted vault is implemented.
	demo := []hosts.Host{
		{ID: "1", Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu"},
		{ID: "2", Label: "db-staging", Hostname: "10.0.1.20", Port: 2222, User: "admin"},
	}

	if _, err := tea.NewProgram(ui.New(demo), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		os.Exit(1)
	}
}
