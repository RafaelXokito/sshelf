// Command sshelf is a terminal UI SSH host manager.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui"
)

func main() {
	// Demo data until config.json is loaded (T-004).
	demo := []domain.Host{
		{Meta: domain.Meta{ID: domain.NewID()}, Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu"},
		{Meta: domain.Meta{ID: domain.NewID()}, Label: "db-staging", Hostname: "10.0.1.20", Port: 2222, User: "admin"},
	}

	if _, err := tea.NewProgram(ui.New(demo)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		os.Exit(1)
	}
}
