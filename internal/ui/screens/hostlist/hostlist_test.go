package hostlist

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

var epoch = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

func TestFR_NAV_018_RelativeTime(t *testing.T) {
	for _, tc := range []struct {
		ago  time.Duration
		want string
	}{
		{-time.Hour, "just now"}, // a clock that went back
		{0, "just now"},
		{59 * time.Second, "just now"},
		{time.Minute, "1m ago"},
		{5*time.Minute + 59*time.Second, "5m ago"},
		{59 * time.Minute, "59m ago"},
		{time.Hour, "1h ago"},
		{23*time.Hour + 59*time.Minute, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{29 * 24 * time.Hour, "29d ago"},
		{30 * 24 * time.Hour, "Sep 30"},
		{(31 + 18) * 24 * time.Hour, "Sep 11"},
		{300 * 24 * time.Hour, "Jan 3"},
		{310 * 24 * time.Hour, "Dec 2025"},
	} {
		got := Ago(epoch.Add(-tc.ago), epoch)
		if got != tc.want {
			t.Errorf("Ago(-%v) = %q, want %q", tc.ago, got, tc.want)
		}
		if len(got) > TimeWidth {
			t.Errorf("Ago(-%v) = %q is wider than %d", tc.ago, got, TimeWidth)
		}
	}
	// Dates are in now's location.
	minus5 := time.FixedZone("UTC-5", -5*3600)
	if got := Ago(time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC), epoch.In(minus5)); got != "Aug 31" {
		t.Errorf("date in now's location = %q, want Aug 31", got)
	}
}

// TestFR_NAV_018_NarrowHidesTime: the time column is right-aligned at
// the list's width, and hidden below TimeMinWidth columns; a never
// connected host shows nothing there.
func TestFR_NAV_018_NarrowHidesTime(t *testing.T) {
	two := epoch.Add(-2 * time.Hour)
	l := New([]domain.Host{
		{Meta: domain.Meta{ID: "a"}, Label: "web", Hostname: "w.example.com", LastConnectedAt: &two},
		{Meta: domain.Meta{ID: "b"}, Label: "never", Hostname: "n.example.com"},
	}).WithClock(func() time.Time { return epoch })
	for _, w := range []int{TimeMinWidth, 80, 120} {
		s, _ := l.Update(tea.WindowSizeMsg{Width: w, Height: 10})
		rows := strings.Split(ansi.Strip(s.View()), "\n")
		if !strings.HasSuffix(rows[0], " 2h ago") || ansi.StringWidth(rows[0]) != w {
			t.Errorf("width %d: row %q (%d columns)", w, rows[0], ansi.StringWidth(rows[0]))
		}
		if strings.Contains(rows[1], "ago") || strings.TrimRight(rows[1], " ") != rows[1] {
			t.Errorf("width %d: never-connected row %q", w, rows[1])
		}
	}
	s, _ := l.Update(tea.WindowSizeMsg{Width: TimeMinWidth - 1, Height: 10})
	if v := ansi.Strip(s.View()); strings.Contains(v, "ago") {
		t.Errorf("width %d shows the time:\n%s", TimeMinWidth-1, v)
	}
}
