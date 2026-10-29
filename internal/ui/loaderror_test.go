package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
)

func TestFR_CFG_003_SyntaxErrorScreen(t *testing.T) {
	svc := &fakeService{loadErr: &app.LoadError{
		Path:    "/home/u/.config/sshelf/config.json",
		Line:    3,
		Column:  13,
		Problem: "invalid character '}' looking for beginning of value",
		Err:     errors.New("corrupt"),
	}}
	d := newDriver(t, svc)
	golden(t, "loaderror", d.view())

	// Enter connects nothing, and q quits.
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 0 || d.quit {
		t.Fatalf("connects = %v, quit = %v", svc.connected, d.quit)
	}
	d.press('q', "q")
	if !d.quit {
		t.Fatal("q did not quit the LoadError screen")
	}
}

func TestLoadErrorWithoutPosition(t *testing.T) {
	d := newDriver(t, &fakeService{loadErr: errors.New("open config.json: permission denied")})
	v := d.view()
	if !strings.Contains(v, "permission denied") || strings.Contains(v, "Line:") {
		t.Fatalf("view:\n%s", v)
	}
}
