package domain

import (
	"os"
	"time"
)

// Built-in defaults used by Resolve.
const (
	DefaultPort           = 22
	DefaultConnectTimeout = 10 * time.Second
)

// Resolve turns a host into the Target sshclient connects to (design §7.2).
//
// It resolves the MVP fields. The username follows FR-IDENT-002: the
// host's, then the linked identity's, then $USER. The port is the host's,
// then 22. The connect timeout is the host's option, then Settings, then
// 10 s (FR-CONN-006). The key path and the secret flags come from the
// linked identity. Group inheritance is v1, so groups is not used yet.
func Resolve(h Host, groups []Group, identities []Identity, s Settings) Target {
	_ = groups // group chain inheritance is v1
	t := Target{
		HostID:         h.ID,
		Label:          h.Label,
		Hostname:       h.Hostname,
		Port:           h.Port,
		User:           h.User,
		IdentityID:     h.IdentityID,
		ConnectTimeout: s.ConnectTimeout,
	}
	if h.Options.ConnectTimeout != nil && *h.Options.ConnectTimeout > 0 {
		t.ConnectTimeout = *h.Options.ConnectTimeout
	}
	if t.Port == 0 {
		t.Port = DefaultPort
	}
	if t.ConnectTimeout <= 0 {
		t.ConnectTimeout = DefaultConnectTimeout
	}
	for _, id := range identities {
		if id.ID != h.IdentityID || h.IdentityID == "" {
			continue
		}
		t.IdentityLabel = id.Label
		t.KeyPath = id.KeyPath
		t.HasKey = id.HasKey
		t.HasPassword = id.HasPassword
		t.HasPassphrase = id.HasPassphrase
		if t.User == "" {
			t.User = id.Username
		}
		break
	}
	if t.User == "" {
		t.User = os.Getenv("USER")
	}
	return t
}
