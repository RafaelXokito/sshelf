package sshconfig

import (
	"strings"
	"testing"
)

func FuzzSSHConfigMapper(f *testing.F) {
	f.Add([]byte("Host web\n  HostName 10.0.0.10\n  User ubuntu\n  IdentityFile ~/.ssh/id_ed25519\n"))
	f.Add([]byte("Host a b !c *.x\nPort=22\nIdentityFile \"%h %p %r %z %\"\nMatch all\nUser m\nHost *\nUser u\n"))
	f.Add([]byte("Include nothing/*\nHost x\n  ProxyCommand ssh -W %h:%p y\n  Port abc\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Home is an empty temp dir, so Include globs match nothing and the
		// fuzzer never reads real files.
		c, err := parse("fuzz", data, Options{Home: t.TempDir(), LocalUser: "me"})
		if err != nil {
			return
		}
		p := c.Plan()
		for _, h := range p.Hosts {
			if h.Host.Label == "" || strings.ContainsAny(h.Host.Label, "*?!") {
				t.Fatalf("bad label %q", h.Host.Label)
			}
			if h.Host.Port < 0 || h.Host.Port > 65535 {
				t.Fatalf("bad port %d", h.Host.Port)
			}
			if h.Identity < -1 || h.Identity >= len(p.Identities) {
				t.Fatalf("identity index %d out of range", h.Identity)
			}
			if h.Host.Import == nil || h.Host.Import.Alias != h.Host.Label {
				t.Fatalf("import source %+v", h.Host.Import)
			}
		}
	})
}
