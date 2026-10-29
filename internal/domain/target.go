package domain

import "time"

// Target is a host resolved for one connection (design §7.2). sshclient
// receives only a Target, never a Host. It holds no secret material: the
// Has* flags only say which of the identity's secrets to look for.
type Target struct {
	HostID         ID
	Label          string
	Hostname       string
	Port           int
	User           string
	IdentityID     ID
	IdentityLabel  string
	KeyPath        string // key file referenced by the identity; "" = none
	HasKey         bool   // the identity has a private key in the credential store
	HasPassword    bool   // the identity has a password in the credential store
	HasPassphrase  bool   // the identity has a key passphrase in the credential store
	ConnectTimeout time.Duration
}
