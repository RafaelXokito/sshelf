package main

import (
	"fmt"
	"runtime/debug"
)

// Set by the release build:
//
//	go build -ldflags "-X main.version=v1.2.3 -X main.commit=<sha> -X main.date=<RFC 3339>"
//
// Empty values fall back to the module version and VCS stamp that the Go
// toolchain records in the binary.
var (
	version string
	commit  string
	date    string
)

// versionString is the --version output: version, commit and build date.
func versionString() string {
	info, _ := debug.ReadBuildInfo()
	return formatVersion(version, commit, date, info)
}

func formatVersion(v, c, d string, info *debug.BuildInfo) string {
	stamped := c != ""
	modified := false
	if info != nil {
		if v == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if c == "" {
					c = s.Value
				}
			case "vcs.time":
				if d == "" {
					d = s.Value
				}
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}
	if v == "" {
		v = "dev"
	}
	switch {
	case c == "":
		c = "unknown"
	case modified && !stamped:
		c += "-dirty"
	}
	if d == "" {
		d = "unknown"
	}
	return fmt.Sprintf("sshelf %s (commit %s, built %s)", v, c, d)
}
