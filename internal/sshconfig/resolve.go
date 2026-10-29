package sshconfig

import "strings"

// Resolved holds the values OpenSSH would use for one alias, as written in
// the config (tokens and "~" not yet expanded). Unset fields are "".
type Resolved struct {
	Alias         string
	HostName      string
	User          string
	Port          string
	IdentityFiles []string // every IdentityFile in order, duplicates removed
	// Other lists the remaining keywords that applied to the alias, in order
	// of first appearance; sshelf does not import them.
	Other []string
}

// Resolve applies the config to alias as OpenSSH does: blocks are read top to
// bottom, Include is expanded in place, a Host line matches when any pattern
// matches and no negated pattern does, and the first value obtained for a
// keyword wins (IdentityFile accumulates). Match blocks are skipped.
func (c *Config) Resolve(alias string) Resolved {
	r := Resolved{Alias: alias}
	seen := map[string]bool{}
	c.apply(c.root, alias, &r, seen)
	return r
}

func (c *Config) apply(f *file, alias string, r *Resolved, seen map[string]bool) {
	active := true // lines before the first Host apply to every host
	for _, ln := range f.lines {
		switch ln.key {
		case "host":
			active = hostMatches(ln.args, alias)
		case "match":
			active = false // not evaluated; reported by the plan
		case "include":
			if active {
				for _, inc := range ln.included {
					c.apply(inc, alias, r, seen)
				}
			}
		default:
			if active {
				r.set(ln, seen)
			}
		}
	}
}

func (r *Resolved) set(ln line, seen map[string]bool) {
	if ln.key == "identityfile" {
		for _, v := range r.IdentityFiles {
			if v == ln.args[0] {
				return
			}
		}
		r.IdentityFiles = append(r.IdentityFiles, ln.args[0])
		return
	}
	if seen[ln.key] {
		return // first value wins
	}
	seen[ln.key] = true
	switch ln.key {
	case "hostname":
		r.HostName = ln.args[0]
	case "user":
		r.User = ln.args[0]
	case "port":
		r.Port = ln.args[0]
	default:
		r.Other = append(r.Other, ln.name)
	}
}

// hostMatches implements a Host line: a negated pattern that matches rejects
// the block, otherwise any matching pattern accepts it. Matching is
// case-sensitive, as in OpenSSH.
func hostMatches(patterns []string, alias string) bool {
	found := false
	for _, p := range patterns {
		negated := strings.HasPrefix(p, "!")
		if !wildMatch(strings.TrimPrefix(p, "!"), alias) {
			continue
		}
		if negated {
			return false
		}
		found = true
	}
	return found
}

// wildMatch reports whether s matches pattern, where "*" matches any run of
// characters and "?" exactly one (OpenSSH match_pattern).
func wildMatch(pattern, s string) bool {
	p, i := 0, 0
	starP, starI := -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[i]):
			p++
			i++
		case p < len(pattern) && pattern[p] == '*':
			starP, starI = p, i
			p++
		case starP >= 0:
			starI++
			p, i = starP+1, starI
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// isConcrete reports whether a Host pattern names a single host.
func isConcrete(p string) bool {
	return p != "" && !strings.ContainsAny(p, "*?!")
}
