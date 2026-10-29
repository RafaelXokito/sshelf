package domain

// Data is everything stored in config.json (design §6.1).
type Data struct {
	Version    int
	Hosts      []Host
	Groups     []Group
	Identities []Identity
	Snippets   []Snippet
	Forwards   []ForwardRule
	Settings   Settings
}
