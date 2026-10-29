package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxIncludeDepth matches OpenSSH's READCONF_MAX_DEPTH. A deeper chain is
// almost always an Include cycle, which ssh itself also refuses.
const maxIncludeDepth = 16

// Options carries the environment the parser needs, so tests never read the
// real home directory.
type Options struct {
	// Home is the user's home directory. "~" expands to it and relative
	// Include paths resolve against Home/.ssh. Required.
	Home string
	// LocalUser stands in for %u, and for %r when no User is configured.
	LocalUser string
}

// Location is a line in a config file.
type Location struct {
	File string // absolute path
	Line int    // 1-based
}

// Config is a parsed ssh_config with every Include already read. Parsing
// only reads files; nothing in a config is ever executed.
type Config struct {
	opts    Options
	root    *file
	matches []Location // skipped Match blocks
}

type file struct {
	path  string
	lines []line
}

type line struct {
	num      int
	name     string // keyword as written
	key      string // keyword lowercased
	args     []string
	included []*file // Include only
}

// DefaultPath returns the user's ssh config path under home.
func DefaultPath(home string) string {
	return filepath.Join(home, ".ssh", "config")
}

// Load reads the ssh config at path and every file it includes.
func Load(path string, opts Options) (*Config, error) {
	if opts.Home == "" {
		return nil, errors.New("sshconfig: Options.Home is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("sshconfig: %w", err)
	}
	c := &Config{opts: opts}
	if c.root, err = c.load(abs, 0); err != nil {
		return nil, err
	}
	return c, nil
}

// parse parses data as the top-level config named name.
func parse(name string, data []byte, opts Options) (*Config, error) {
	c := &Config{opts: opts}
	var err error
	if c.root, err = c.parseFile(name, data, 0); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) load(path string, depth int) (*file, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading the user's own ssh config is the purpose
	if err != nil {
		return nil, fmt.Errorf("sshconfig: %w", err)
	}
	return c.parseFile(path, data, depth)
}

func (c *Config) parseFile(path string, data []byte, depth int) (*file, error) {
	f := &file{path: path}
	for i, raw := range strings.Split(string(data), "\n") {
		ln, ok, err := parseLine(raw)
		if err != nil {
			return nil, fmt.Errorf("sshconfig: %s line %d: %w", path, i+1, err)
		}
		if !ok {
			continue
		}
		ln.num = i + 1
		switch ln.key {
		case "match":
			c.matches = append(c.matches, Location{File: path, Line: ln.num})
		case "include":
			if ln.included, err = c.include(ln.args, depth); err != nil {
				return nil, fmt.Errorf("sshconfig: %s line %d: %w", path, ln.num, err)
			}
		}
		f.lines = append(f.lines, ln)
	}
	return f, nil
}

// include expands each Include argument (~, relative to ~/.ssh, globs) and
// parses the files it names, in sorted order. A glob matching nothing is not
// an error, as in OpenSSH.
func (c *Config) include(args []string, depth int) ([]*file, error) {
	if depth+1 > maxIncludeDepth {
		return nil, fmt.Errorf("include nested more than %d levels (cycle?)", maxIncludeDepth)
	}
	var files []*file
	for _, arg := range args {
		pattern := c.expandTilde(arg)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(c.opts.Home, ".ssh", pattern)
		}
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("include %q: %w", arg, err)
		}
		for _, p := range paths {
			if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			child, err := c.load(p, depth+1)
			if err != nil {
				return nil, err
			}
			files = append(files, child)
		}
	}
	return files, nil
}

func (c *Config) expandTilde(s string) string {
	if s == "~" {
		return c.opts.Home
	}
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(c.opts.Home, s[2:])
	}
	return s
}

// parseLine splits one config line the way OpenSSH's readconf does: the
// keyword ends at whitespace or a single "=", arguments may be quoted, and a
// word starting with "#" begins a comment. ok is false for blank and comment
// lines.
func parseLine(raw string) (ln line, ok bool, err error) {
	s := strings.Trim(raw, " \t\r\f")
	if s == "" || s[0] == '#' {
		return line{}, false, nil
	}
	end := strings.IndexAny(s, " \t=")
	if end < 0 {
		return line{}, false, fmt.Errorf("no argument after keyword %q", s)
	}
	name, rest := s[:end], strings.TrimLeft(s[end:], " \t")
	if strings.HasPrefix(rest, "=") { // at most one "=" between keyword and value
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	args, err := splitArgs(rest)
	if err != nil {
		return line{}, false, err
	}
	if len(args) == 0 {
		return line{}, false, fmt.Errorf("no argument after keyword %q", name)
	}
	return line{name: name, key: strings.ToLower(name), args: args}, true, nil
}

// splitArgs follows OpenSSH's argv_split: whitespace separates words, single
// and double quotes group, a backslash escapes a quote, a backslash or (outside
// quotes) a space, and an unquoted word starting with "#" ends the line.
func splitArgs(s string) ([]string, error) {
	var (
		args   []string
		cur    strings.Builder
		inWord bool
		quote  byte
	)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote == 0 && (ch == ' ' || ch == '\t') {
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
			continue
		}
		if quote == 0 && !inWord && ch == '#' {
			break
		}
		inWord = true
		switch {
		case ch == '\\' && i+1 < len(s) && (s[i+1] == '\'' || s[i+1] == '"' || s[i+1] == '\\' || (quote == 0 && s[i+1] == ' ')):
			i++
			cur.WriteByte(s[i])
		case quote == 0 && (ch == '\'' || ch == '"'):
			quote = ch
		case quote != 0 && ch == quote:
			quote = 0
		default:
			cur.WriteByte(ch)
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}
