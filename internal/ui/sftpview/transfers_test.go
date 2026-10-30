package sftpview

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
)

func TestFR_SFTP_014_DropsCountsThenBar(t *testing.T) {
	tr := app.Transfer{From: app.Remote, Name: "backup.tar.gz", State: app.Running, Done: 412_000_000, Total: 840_000_000, Rate: 12_000_000}
	full := "↓ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s  +1 running, 3 queued"
	for _, tc := range []struct {
		width int
		want  string
	}{
		{200, full},
		{ansi.StringWidth(full), full},
		{ansi.StringWidth(full) - 1, "↓ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s"},
		{45, "↓ backup.tar.gz  412/840 MB  49%  12 MB/s"},
		{36, "↓ backup.tar.gz  412/840 MB  49%"},
		{30, "↓ backup.tar…  412/840 MB  49%"},
	} {
		got := progressText(tr, 1, 3, tc.width)
		if got != tc.want {
			t.Errorf("width %d: %q, want %q", tc.width, got, tc.want)
		}
		if ansi.StringWidth(got) > tc.width {
			t.Errorf("width %d: %q is wider", tc.width, got)
		}
	}
	up := tr
	up.From = app.Local
	if got := progressText(up, 0, 0, 200); got != "↑ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s" {
		t.Errorf("upload: %q", got)
	}
	if got := BytesPair(3_400_000, 9_000_000); got != "3.4/9 MB" {
		t.Errorf("BytesPair = %q", got)
	}
}

// TestFR_SFTP_001_SummaryCountsFiles: a copied tree's directory rows are
// not counted as copied files.
func TestFR_SFTP_001_SummaryCountsFiles(t *testing.T) {
	got := batchSummary([]app.Transfer{
		{Name: "photos", State: app.Done, Dir: true},
		{Name: "photos/a.jpg", State: app.Done},
		{Name: "photos/link", State: app.Skipped, Reason: "symbolic link"},
	})
	if got != "copied 1 file, skipped 1" {
		t.Fatalf("summary = %q", got)
	}
}

// TestSEC_SFTP_004_SummaryCountsTooDeep: a directory skipped as too deep
// is reported in the summary (AC2).
func TestSEC_SFTP_004_SummaryCountsTooDeep(t *testing.T) {
	got := batchSummary([]app.Transfer{
		{Name: "deep", State: app.Done, Dir: true},
		{Name: "deep/d", State: app.Skipped, Reason: "too many nested directories", Dir: true},
	})
	if got != "copied 0 files, skipped 1" {
		t.Fatalf("summary = %q", got)
	}
}
