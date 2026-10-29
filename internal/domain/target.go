package domain

import "time"

// Target is a host resolved for one connection (design §7.2). sshclient
// receives only a Target, never a Host. It holds no secret material.
type Target struct {
	HostID         ID
	Label          string
	Hostname       string
	Port           int
	User           string
	IdentityID     ID
	KeyPath        string // key file referenced by the identity; "" = none
	ConnectTimeout time.Duration
}
