// Command sshelf is a terminal UI SSH host manager.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/ui"
)

func main() {
	dir, err := paths.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		os.Exit(1)
	}
	home, _ := os.UserHomeDir() // "" skips ~/.ssh/known_hosts
	svc := app.New(app.Config{
		ConfigPath:      paths.ConfigFile(dir),
		KnownHostsFiles: paths.KnownHostsFiles(dir, home),
	})

	if _, err := tea.NewProgram(ui.New(svc)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		os.Exit(1)
	}
}
