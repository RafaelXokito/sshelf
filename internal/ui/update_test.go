package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const (
	longNotice  = "update available: v0.4.0 · run sshelf update"
	shortNotice = "v0.4.0 available"
)

func withUpdate(t *testing.T, v string) *driver {
	t.Helper()
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.send(UpdateAvailableMsg{Version: v})
	return d
}

func TestFR_UPD_002_FooterNotice(t *testing.T) {
	d := withUpdate(t, "v0.4.0")
	d.resize(120, 40)
	foot := plain(d.root().footer())
	if !strings.HasSuffix(foot, longNotice) {
		t.Fatalf("120 columns: footer %q lacks the long notice", foot)
	}
	if w := ansi.StringWidth(d.root().footer()); w != 120 {
		t.Errorf("120 columns: footer is %d columns, want the notice right-aligned at 120", w)
	}
	if !strings.HasPrefix(foot, "enter connect • s sftp • / search • a add") || !strings.Contains(foot, "? help") {
		t.Errorf("120 columns: footer %q", foot)
	}

	d.resize(80, 24)
	foot = plain(d.root().footer())
	if !strings.HasSuffix(foot, shortNotice) || strings.Contains(foot, "run sshelf update") {
		t.Fatalf("80 columns: footer %q, want the short notice", foot)
	}

	sawLong, sawShort, sawNone := false, false, false
	for w := 40; w <= 130; w++ {
		d.resize(w, 24)
		f := plain(d.root().footer())
		if ansi.StringWidth(f) > w {
			t.Fatalf("%d columns: footer %q too wide", w, f)
		}
		for _, k := range []string{"enter connect", "/ search", "a add", "q quit", "? help"} {
			if w >= 60 && !strings.Contains(f, k) {
				t.Fatalf("%d columns: footer %q dropped %q for the notice", w, f, k)
			}
		}
		switch {
		case strings.HasSuffix(f, longNotice):
			sawLong = true
		case strings.HasSuffix(f, shortNotice):
			sawShort = true
		default:
			sawNone = true
			if !strings.HasSuffix(f, "? help") {
				t.Fatalf("%d columns: footer %q", w, f)
			}
		}
		lines := strings.Split(plain(d.view()), "\n")
		if last := lines[len(lines)-1]; strings.Contains(last, "…") {
			t.Fatalf("%d columns: footer cut: %q", w, last)
		}
	}
	if !sawLong || !sawShort || !sawNone {
		t.Errorf("long %v, short %v, none %v: want each form at some width", sawLong, sawShort, sawNone)
	}

	// While searching, the search footer only.
	d.resize(120, 40)
	d.press('/', "/")
	if f := plain(d.root().footer()); strings.Contains(f, "available") {
		t.Errorf("search footer %q shows the notice", f)
	}
}

func TestFR_UPD_002_NoNoticeWithoutUpdate(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(120, 40)
	if f := plain(d.root().footer()); strings.Contains(f, "available") {
		t.Fatalf("footer %q", f)
	}
}

func TestSEC_UPD_001_NoticeOnlyShowsAVersion(t *testing.T) {
	for _, v := range []string{"v0.4.0\x1b[2J", "\x1b]52;c;ZXZpbA==\x07", "v0.4.0-rc1", "0.4.0", "v0.4", "v0.4.0 · run rm -rf"} {
		d := withUpdate(t, v)
		d.resize(120, 40)
		if f := d.root().footer(); strings.Contains(f, "available") || strings.Contains(f, "\x1b]") {
			t.Errorf("version %q: footer %q", v, f)
		}
	}
}

func TestFR_UPD_002_NoticeGolden(t *testing.T) {
	golden(t, "hostlist-update", withUpdate(t, "v0.4.0").m)
}

// With no hosts the Welcome screen stands in for the host list
// (FR-NAV-013); its footer shows the notice too.
func TestFR_UPD_002_WelcomeFooterNotice(t *testing.T) {
	d := newDriver(t, &fakeService{})
	d.send(UpdateAvailableMsg{Version: "v0.4.0"})
	const keys = "I import ~/.ssh/config • a add host • q quit"

	d.resize(120, 40)
	foot := plain(d.root().footer())
	if !strings.HasPrefix(foot, keys) || !strings.HasSuffix(foot, longNotice) {
		t.Fatalf("120 columns: footer %q", foot)
	}
	if w := ansi.StringWidth(foot); w != 120 {
		t.Errorf("120 columns: footer is %d columns, want the notice right-aligned at 120", w)
	}

	d.resize(80, 24)
	if foot = plain(d.root().footer()); !strings.HasPrefix(foot, keys) || !strings.HasSuffix(foot, shortNotice) {
		t.Fatalf("80 columns: footer %q, want the short notice", foot)
	}

	for w := 40; w <= 130; w++ {
		d.resize(w, 24)
		f := plain(d.root().footer())
		if w >= 46 && !strings.HasPrefix(f, keys) {
			t.Fatalf("%d columns: footer %q dropped a key for the notice", w, f)
		}
		if strings.Contains(f, "available") && ansi.StringWidth(f) != w {
			t.Fatalf("%d columns: footer %q not right-aligned", w, f)
		}
		if !strings.HasSuffix(f, longNotice) && !strings.HasSuffix(f, shortNotice) && !strings.HasSuffix(f, "q quit") {
			t.Fatalf("%d columns: footer %q", w, f)
		}
	}
}

func TestFR_UPD_002_WelcomeNoNoticeWithoutUpdate(t *testing.T) {
	d := newDriver(t, &fakeService{})
	d.resize(120, 40)
	if f := plain(d.root().footer()); f != "I import ~/.ssh/config • a add host • q quit" {
		t.Fatalf("footer %q", f)
	}
}

func TestFR_UPD_002_WelcomeNoticeGolden(t *testing.T) {
	d := newDriver(t, &fakeService{})
	d.send(UpdateAvailableMsg{Version: "v0.4.0"})
	golden(t, "welcome-update", d.m)
}
