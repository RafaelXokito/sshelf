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
	dirs, err := paths.Resolve("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		os.Exit(1)
	}
	home, _ := os.UserHomeDir() // "" skips ~/.ssh/known_hosts
	svc := app.New(app.Config{
		Dir:             dirs.Config,
		KnownHostsFiles: paths.KnownHostsFiles(dirs.Config, home),
	})

	p := tea.NewProgram(ui.New(svc))
	svc.SetSend(func(msg any) { p.Send(msg) }) // prompts reach the UI (design §8.3)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		os.Exit(1)
	}
}
