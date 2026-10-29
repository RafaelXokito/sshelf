// Package hosts defines the saved SSH host entries managed by sshelf.
package hosts

import (
	"net"
	"strconv"
)

// Host is a single saved SSH target. Secrets (password, key passphrase)
// live in the encrypted vault and are referenced by ID, never stored here.
type Host struct {
	ID       string
	Label    string
	Hostname string
	Port     int
	User     string
	KeyPath  string
	Group    string
	Tags     []string
}

// Address returns host:port, defaulting the port to 22.
func (h Host) Address() string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(h.Hostname, strconv.Itoa(port))
}
