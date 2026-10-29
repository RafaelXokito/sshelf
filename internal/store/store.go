package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Snapshot is config.json as loaded.
type Snapshot struct {
	Data     domain.Data
	FirstRun bool // config.json did not exist
}

// Load reads config.json at path (read-only). A missing file returns an
// empty Data with FirstRun set. Unknown fields are ignored.
//
// Atomic saves, ErrCorrupt with line and column, ErrNewerVersion and the
// read cap come with T-006.
func Load(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path comes from internal/paths
	if errors.Is(err, fs.ErrNotExist) {
		return &Snapshot{Data: domain.Data{Version: Version}, FirstRun: true}, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileConfig
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Snapshot{Data: f.toDomain()}, nil
}

// fileMode is the mode of config.json (SEC-FILE-001).
const fileMode = 0o600

// Save writes s to config.json at path with mode 0600, whatever the umask.
// The directory must exist (paths.EnsureDir).
func Save(path string, s *Snapshot) error {
	b, err := json.MarshalIndent(fromDomain(s.Data), "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode) //nolint:gosec // path comes from internal/paths
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(fileMode); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Version is the config.json schema version this build writes.
const Version = 1

// The on-disk schema (security/credential-storage.md §3.3). Only the fields
// the MVP slice uses are mapped so far.
type fileConfig struct {
	Version    int            `json:"version"`
	Identities []fileIdentity `json:"identities"`
	Hosts      []fileHost     `json:"hosts"`
}

type fileIdentity struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Username         string `json:"username"`
	KeyPath          string `json:"key_path"`
	HasPassword      bool   `json:"has_password"`
	HasPrivateKey    bool   `json:"has_private_key"`
	HasKeyPassphrase bool   `json:"has_key_passphrase"`
	UseAgent         bool   `json:"use_agent"`
}

type fileHost struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Hostname   string   `json:"hostname"`
	Port       int      `json:"port"`
	User       string   `json:"user"`
	IdentityID string   `json:"identity_id"`
	GroupID    string   `json:"group_id"`
	Tags       []string `json:"tags"`
}

func (f fileConfig) toDomain() domain.Data {
	d := domain.Data{Version: f.Version}
	for _, i := range f.Identities {
		d.Identities = append(d.Identities, domain.Identity{
			Meta:          domain.Meta{ID: domain.ID(i.ID)},
			Label:         i.Name,
			Username:      i.Username,
			KeyPath:       i.KeyPath,
			HasPassword:   i.HasPassword,
			HasKey:        i.HasPrivateKey,
			HasPassphrase: i.HasKeyPassphrase,
			UseAgent:      i.UseAgent,
		})
	}
	for _, h := range f.Hosts {
		d.Hosts = append(d.Hosts, domain.Host{
			Meta:       domain.Meta{ID: domain.ID(h.ID)},
			Label:      h.Label,
			Hostname:   h.Hostname,
			Port:       h.Port,
			User:       h.User,
			IdentityID: domain.ID(h.IdentityID),
			GroupID:    domain.ID(h.GroupID),
			Tags:       h.Tags,
		})
	}
	return d
}

func fromDomain(d domain.Data) fileConfig {
	f := fileConfig{Version: Version, Identities: []fileIdentity{}, Hosts: []fileHost{}}
	for _, i := range d.Identities {
		f.Identities = append(f.Identities, fileIdentity{
			ID:               string(i.ID),
			Name:             i.Label,
			Username:         i.Username,
			KeyPath:          i.KeyPath,
			HasPassword:      i.HasPassword,
			HasPrivateKey:    i.HasKey,
			HasKeyPassphrase: i.HasPassphrase,
			UseAgent:         i.UseAgent,
		})
	}
	for _, h := range d.Hosts {
		f.Hosts = append(f.Hosts, fileHost{
			ID:         string(h.ID),
			Label:      h.Label,
			Hostname:   h.Hostname,
			Port:       h.Port,
			User:       h.User,
			IdentityID: string(h.IdentityID),
			GroupID:    string(h.GroupID),
			Tags:       h.Tags,
		})
	}
	return f
}
