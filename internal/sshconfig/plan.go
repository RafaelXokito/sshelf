package sshconfig

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Plan is what an import would create. It holds no IDs and no timestamps;
// applying it (T-025) assigns them. Key files are referenced by path, never
// read.
type Plan struct {
	Hosts []HostPlan
	// Identities are deduplicated by user plus key file and labelled
	// "user (keyfile)". Only Label, Username and KeyPath are set.
	Identities []domain.Identity
	// Matches are the Match blocks that were skipped. Because sshelf cannot
	// tell which hosts they would apply to, every host also lists "Match" in
	// Import.Unsupported when there is at least one.
	Matches []Location
}

// HostPlan is one host to create.
type HostPlan struct {
	// Host has Label, Hostname, Port (0 = not set), User and Import (File,
	// Alias, Line, Unsupported) filled in.
	Host domain.Host
	// Identity indexes Plan.Identities, or is -1 when the host sets no
	// IdentityFile.
	Identity int
}

// Unsupported entries that are not plain keywords.
const (
	unsupportedMatch          = "Match"
	unsupportedExtraKeys      = "IdentityFile (only the first is imported)"
	unsupportedInvalidPort    = "Port (invalid value)"
	unsupportedUnknownPercent = "IdentityFile (unknown % token)"
)

// Plan builds the import plan: one host per concrete alias, in the order the
// aliases first appear (wildcard and negated patterns create no host).
func (c *Config) Plan() Plan {
	var p Plan
	p.Matches = append(p.Matches, c.matches...)
	identities := map[string]int{} // user + "\x00" + key path
	labels := map[string]bool{}

	for _, a := range c.aliases() {
		r := c.Resolve(a.alias)
		h := domain.Host{
			Label:    a.alias,
			Hostname: expandHostName(r.HostName, a.alias),
			User:     r.User,
			Import: &domain.ImportSource{
				File:        a.loc.File,
				Alias:       a.alias,
				Line:        a.loc.Line,
				Unsupported: append([]string(nil), r.Other...),
			},
		}
		if r.Port != "" {
			if n, err := strconv.Atoi(r.Port); err == nil && n > 0 && n < 65536 {
				h.Port = n
			} else {
				h.Import.Unsupported = append(h.Import.Unsupported, unsupportedInvalidPort)
			}
		}
		if len(c.matches) > 0 {
			h.Import.Unsupported = append(h.Import.Unsupported, unsupportedMatch)
		}

		hp := HostPlan{Host: h, Identity: -1}
		if len(r.IdentityFiles) > 0 {
			keyPath, ok := c.expandIdentityFile(r.IdentityFiles[0], a.alias, h)
			if !ok {
				hp.Host.Import.Unsupported = append(hp.Host.Import.Unsupported, unsupportedUnknownPercent)
			}
			if len(r.IdentityFiles) > 1 {
				hp.Host.Import.Unsupported = append(hp.Host.Import.Unsupported, unsupportedExtraKeys)
			}
			key := r.User + "\x00" + keyPath
			idx, found := identities[key]
			if !found {
				idx = len(p.Identities)
				identities[key] = idx
				label := identityLabel(r.User, filepath.Base(keyPath))
				if labels[label] {
					label = identityLabel(r.User, keyPath)
				}
				labels[label] = true
				p.Identities = append(p.Identities, domain.Identity{Label: label, Username: r.User, KeyPath: keyPath})
			}
			hp.Identity = idx
		}
		p.Hosts = append(p.Hosts, hp)
	}
	return p
}

func identityLabel(user, key string) string {
	if user == "" {
		return key
	}
	return fmt.Sprintf("%s (%s)", user, key)
}

// expandIdentityFile resolves "~" and the %d %h %n %p %r %u %% tokens of an
// IdentityFile. ok is false when an unsupported token was left as written.
func (c *Config) expandIdentityFile(v, alias string, h domain.Host) (string, bool) {
	port, user := "22", h.User
	if h.Port != 0 {
		port = strconv.Itoa(h.Port)
	}
	if user == "" {
		user = c.opts.LocalUser
	}
	return expandTokens(c.expandTilde(v), map[byte]string{
		'd': c.opts.Home, 'h': h.Hostname, 'n': alias, 'p': port, 'r': user, 'u': c.opts.LocalUser,
	})
}

// expandHostName applies HostName's only token, %h, and lowercases the
// result as ssh does. An unset HostName means the alias itself.
func expandHostName(v, alias string) string {
	if v == "" {
		v = "%h"
	}
	out, _ := expandTokens(v, map[byte]string{'h': alias})
	return strings.ToLower(out)
}

// expandTokens replaces %x with vals[x] and %% with %. Unknown tokens are
// kept as written and make ok false.
func expandTokens(s string, vals map[byte]string) (out string, ok bool) {
	ok = true
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		if s[i] == '%' {
			b.WriteByte('%')
		} else if v, ok := vals[s[i]]; ok {
			b.WriteString(v)
		} else {
			ok = false
			b.WriteByte('%')
			b.WriteByte(s[i])
		}
	}
	return b.String(), ok
}

type aliasRef struct {
	alias string
	loc   Location
}

// aliases lists the concrete aliases of every Host line in file order,
// following Includes, first occurrence wins.
func (c *Config) aliases() []aliasRef {
	var out []aliasRef
	seen := map[string]bool{}
	var walk func(f *file)
	walk = func(f *file) {
		for _, ln := range f.lines {
			switch ln.key {
			case "host":
				for _, a := range ln.args {
					if isConcrete(a) && !seen[a] {
						seen[a] = true
						out = append(out, aliasRef{alias: a, loc: Location{File: f.path, Line: ln.num}})
					}
				}
			case "include":
				for _, inc := range ln.included {
					walk(inc)
				}
			}
		}
	}
	walk(c.root)
	return out
}
