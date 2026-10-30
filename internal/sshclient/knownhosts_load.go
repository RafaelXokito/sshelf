package sshclient

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"regexp"
	"strconv"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// maxSkippedLines bounds the retry loop in loadKnownHosts; a file with more
// unparseable lines than this is treated as broken rather than skimmed.
const maxSkippedLines = 100

// knownhostsLineErr matches x/crypto's parse error, "knownhosts: <file>:<line>: <reason>".
var knownhostsLineErr = regexp.MustCompile(`^knownhosts: (.+):(\d+): `)

// loadKnownHosts builds a host-key callback from files. Like OpenSSH, it
// skips lines it cannot parse instead of rejecting the whole file
// (FR-KH-003): a single corrupt entry in ~/.ssh/known_hosts must not make
// every host unreachable.
//
// x/crypto's knownhosts.New only reads paths and stops at the first bad
// line, so on a parse error each file is copied to a temporary file with
// the offending lines blanked (line numbers unchanged) and loading is
// retried. knownhosts.New reads the files completely before returning, so
// the copies are removed straight away. Errors returned by the callback
// name the original files.
func loadKnownHosts(files []string) (ssh.HostKeyCallback, error) {
	cb, err := knownhosts.New(files...)
	if err == nil || !knownhostsLineErr.MatchString(err.Error()) {
		return cb, err
	}

	contents := make([][][]byte, len(files))
	for i, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // paths come from Config.KnownHostsFiles
		if err != nil {
			return nil, err
		}
		contents[i] = bytes.Split(b, []byte("\n"))
	}

	for range maxSkippedLines {
		temps, original, err := writeTemps(contents)
		if err != nil {
			return nil, err
		}
		cb, err = knownhosts.New(temps...)
		for _, t := range temps {
			_ = os.Remove(t)
		}
		if err == nil {
			names := make(map[string]string, len(original))
			for tmp, i := range original {
				names[tmp] = files[i]
			}
			return remapFiles(cb, names), nil
		}
		m := knownhostsLineErr.FindStringSubmatch(err.Error())
		if m == nil {
			return nil, err
		}
		idx := indexOf(temps, m[1])
		line, _ := strconv.Atoi(m[2])
		if idx < 0 || line < 1 || line > len(contents[idx]) || len(contents[idx][line-1]) == 0 {
			return nil, err
		}
		slog.Warn("known_hosts: skipping unparseable line", "file", files[idx], "line", line,
			"reason", err.Error()[len(m[0]):])
		contents[idx][line-1] = nil
	}
	return nil, fmt.Errorf("more than %d unparseable lines", maxSkippedLines)
}

// writeTemps writes each file's lines to a new temporary file and returns
// the temporary paths and a map from temporary to original path index.
func writeTemps(contents [][][]byte) (temps []string, original map[string]int, err error) {
	original = make(map[string]int, len(contents))
	for i, lines := range contents {
		f, err := os.CreateTemp("", "sshelf-known_hosts-*")
		if err != nil {
			removeAll(temps)
			return nil, nil, err
		}
		_, werr := f.Write(bytes.Join(lines, []byte("\n")))
		cerr := f.Close()
		temps = append(temps, f.Name())
		if err := errors.Join(werr, cerr); err != nil {
			removeAll(temps)
			return nil, nil, err
		}
		original[f.Name()] = i
	}
	return temps, original, nil
}

// remapFiles wraps cb so that the KnownKey entries in its errors name the
// original files (names maps temporary path to original path) instead of
// the temporary copies, which no longer exist.
func remapFiles(cb ssh.HostKeyCallback, names map[string]string) ssh.HostKeyCallback {
	originalName := func(tmp string) string {
		if n, ok := names[tmp]; ok {
			return n
		}
		return tmp
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if ke, ok := errors.AsType[*knownhosts.KeyError](err); ok {
			for i := range ke.Want {
				ke.Want[i].Filename = originalName(ke.Want[i].Filename)
			}
		}
		if re, ok := errors.AsType[*knownhosts.RevokedError](err); ok {
			re.Revoked.Filename = originalName(re.Revoked.Filename)
		}
		return err
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func removeAll(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}
