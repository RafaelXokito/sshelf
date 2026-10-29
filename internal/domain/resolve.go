package domain

import "time"

// Built-in defaults used by Resolve.
const (
	DefaultPort           = 22
	DefaultConnectTimeout = 10 * time.Second
)

// Resolve turns a host into the Target sshclient connects to (design §7.2).
//
// This is the minimal version (T-004): hostname, port (host, then 22), user
// (host, then identity), key path (identity) and connect timeout (Settings,
// then 10 s). Group inheritance and the $USER fallback come later (T-012).
func Resolve(h Host, groups []Group, identities []Identity, s Settings) Target {
	_ = groups // group chain inheritance is not implemented yet
	t := Target{
		HostID:         h.ID,
		Label:          h.Label,
		Hostname:       h.Hostname,
		Port:           h.Port,
		User:           h.User,
		IdentityID:     h.IdentityID,
		ConnectTimeout: s.ConnectTimeout,
	}
	if t.Port == 0 {
		t.Port = DefaultPort
	}
	if t.ConnectTimeout == 0 {
		t.ConnectTimeout = DefaultConnectTimeout
	}
	for _, id := range identities {
		if id.ID != h.IdentityID || h.IdentityID == "" {
			continue
		}
		t.KeyPath = id.KeyPath
		if t.User == "" {
			t.User = id.Username
		}
		break
	}
	return t
}
