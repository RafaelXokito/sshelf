package domain

import "time"

// Meta is embedded in every entity.
type Meta struct {
	ID        ID
	CreatedAt time.Time // UTC
	UpdatedAt time.Time // UTC
}

// Host is a saved SSH target.
type Host struct {
	Meta
	Label           string // display name, may contain spaces (D-105)
	Hostname        string // OpenSSH HostName
	Port            int    // 0 = inherit (group, then 22)
	User            string // "" = identity username, then group, then $USER
	GroupID         ID     // "" = top level
	Tags            []string
	Notes           string
	IdentityID      ID   // "" = inherit from group; the only link to credentials
	JumpHostIDs     []ID // ordered ProxyJump chain (v1)
	ForwardIDs      []ID // ForwardRules started with this host (v1)
	Options         HostOptions
	Favorite        bool
	LastConnectedAt *time.Time
	Import          *ImportSource // nil when created in sshelf
}

// HostOptions are per-host connection options. The zero value inherits from
// the group, then from Settings.
type HostOptions struct {
	ForwardAgent        bool // v1; default false
	ServerAliveInterval *time.Duration
	ServerAliveCountMax *int
	ConnectTimeout      *time.Duration
	RemoteCommand       string // "" = login shell
	SetEnv              map[string]string
	Term                string // "" = $TERM, else xterm-256color
}

// ImportSource records where an imported host came from.
type ImportSource struct {
	File        string // absolute path of the config file the block came from
	Alias       string // the concrete alias imported
	Line        int
	ImportedAt  time.Time
	Unsupported []string // e.g. "ProxyCommand", "Match", "ControlMaster"
}

// Group groups hosts and gives them defaults.
type Group struct {
	Meta
	Name     string
	ParentID ID // "" = top level; max depth 4
	Defaults GroupDefaults
}

// GroupDefaults are inherited by the hosts in a group.
type GroupDefaults struct {
	User        string
	Port        int
	IdentityID  ID
	JumpHostIDs []ID
	Options     HostOptions
}

// Identity holds no secret material. Which secrets exist is recorded as flags,
// so the UI can render "set" / "not set" (SEC-LOG-005) without touching the
// keychain.
type Identity struct {
	Meta
	Label         string
	Username      string // optional; a host's User wins
	Hidden        bool   // per-host identity created from the host form (FR-HOST-010)
	HasPassword   bool   // secrets.Ref{ID, Password} exists
	HasKey        bool   // secrets.Ref{ID, PrivateKey} exists
	HasPassphrase bool   // secrets.Ref{ID, KeyPassphrase} exists
	KeyPath       string // alternative to HasKey: key file on disk, read but never copied
	PublicKey     string // authorized_keys line, for display and copying
	UseAgent      bool   // v1
}

// ForwardRule is a port forward (v1).
type ForwardRule struct {
	Meta
	Label     string
	Kind      string // "local" | "remote" | "dynamic"
	BindAddr  string // default "127.0.0.1"
	BindPort  int
	DestHost  string // unused for dynamic
	DestPort  int
	AutoStart bool
}

// Snippet is a saved command (v1).
type Snippet struct {
	Meta
	Label string
	Body  string // command run on one or more hosts (FR-SNIP-003/004)
	Tags  []string
}

// Settings are the global defaults.
type Settings struct {
	ServerAliveInterval time.Duration // default 30s
	ServerAliveCountMax int           // default 3
	ConnectTimeout      time.Duration // default 10s (FR-CONN-006)
	EscapeChar          string        // default "~"; "none" disables
	Theme               string        // v1
	HostListSort        string        // "label" | "recent"
}
