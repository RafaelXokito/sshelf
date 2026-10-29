package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Version is the config.json schema version this build reads and writes.
const Version = 1

// MaxFileSize caps how much of config.json is read (credential-storage
// §3.2).
const MaxFileSize = 16 << 20

// Load errors. A *LoadError wraps one of them.
var (
	// ErrCorrupt means config.json could not be parsed.
	ErrCorrupt = errors.New("config file is corrupt")
	// ErrNewerVersion means config.json was written by a newer sshelf. It is
	// refused, never downgraded.
	ErrNewerVersion = errors.New("config file is from a newer sshelf")
)

// LoadError says why config.json could not be loaded and where (FR-CFG-003).
type LoadError struct {
	Path    string
	Line    int // 1-based; 0 when the problem has no position
	Column  int // 1-based; 0 when the problem has no position
	Problem string
	Err     error // ErrCorrupt or ErrNewerVersion
}

func (e *LoadError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.Path, e.Line, e.Column, e.Problem)
	}
	return e.Path + ": " + e.Problem
}

func (e *LoadError) Unwrap() error { return e.Err }

// Snapshot is config.json as loaded.
type Snapshot struct {
	Data     domain.Data
	FirstRun bool // config.json did not exist
	// Warnings are problems that did not stop the load, such as unknown
	// setting keys (FR-CFG-003). Each is one line.
	Warnings []string
}

// Load reads config.json at path. It never writes. A missing file returns
// an empty Data with FirstRun set. Unknown fields are ignored, and unknown
// setting keys produce a warning. A file that cannot be parsed, is larger
// than MaxFileSize, or has a newer version returns a *LoadError.
func Load(path string) (*Snapshot, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from internal/paths
	if errors.Is(err, fs.ErrNotExist) {
		return &Snapshot{Data: domain.Data{Version: Version}, FirstRun: true}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFileSize {
		return nil, &LoadError{Path: path, Problem: "file is larger than 16 MiB", Err: ErrCorrupt}
	}
	return decode(path, b)
}

// decode parses config.json contents. path is used in errors only.
func decode(path string, b []byte) (*Snapshot, error) {
	var f fileConfig
	if err := json.Unmarshal(b, &f); err != nil {
		e := &LoadError{Path: path, Problem: err.Error(), Err: ErrCorrupt}
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syn):
			e.Line, e.Column = position(b, syn.Offset)
		case errors.As(err, &typ):
			e.Line, e.Column = position(b, typ.Offset)
		}
		return nil, e
	}
	if f.Version > Version {
		return nil, &LoadError{
			Path:    path,
			Problem: fmt.Sprintf("written by a newer sshelf (version %d; this build reads version %d)", f.Version, Version),
			Err:     ErrNewerVersion,
		}
	}
	s := &Snapshot{Data: f.toDomain()}
	// Unknown setting keys are ignored with a warning (FR-CFG-003).
	var probe struct {
		Settings map[string]any `json:"settings"`
	}
	_ = json.Unmarshal(b, &probe) // b already parsed above
	var unknown []string
	for k := range probe.Settings {
		if !slices.Contains(settingKeys, k) {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		s.Warnings = append(s.Warnings, fmt.Sprintf("config.json: unknown setting %q ignored", k))
	}
	return s, nil
}

// position returns the 1-based line and column of the byte before offset
// off: encoding/json reports the offset just past the offending byte.
func position(b []byte, off int64) (line, col int) {
	i := int(min(max(off-1, 0), int64(len(b))))
	before := b[:i]
	return 1 + bytes.Count(before, []byte("\n")), i - bytes.LastIndexByte(before, '\n')
}

// Save writes s to config.json at path atomically with mode 0600 (WriteFile,
// SEC-FILE-003). When Save returns nil the change is on disk (FR-CFG-002).
// The directory must exist (paths.EnsureDir).
func Save(path string, s *Snapshot) error { return save(osFS{}, path, s) }

func save(fsys fileSystem, path string, s *Snapshot) error {
	b, err := encode(s.Data)
	if err != nil {
		return err
	}
	return writeFile(fsys, path, b)
}

func encode(d domain.Data) ([]byte, error) {
	b, err := json.MarshalIndent(fromDomain(d), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// The on-disk schema (security/credential-storage.md §3.3). It has no field
// that can hold a secret (SEC-CRED-001).
type fileConfig struct {
	Version    int            `json:"version"`
	Settings   fileSettings   `json:"settings"`
	Groups     []fileGroup    `json:"groups"`
	Identities []fileIdentity `json:"identities"`
	Hosts      []fileHost     `json:"hosts"`
}

// settingKeys are the setting keys this build knows.
var settingKeys = []string{"theme"}

type fileSettings struct {
	Theme string `json:"theme,omitempty"`
}

type fileGroup struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"` // null at the top level
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
	// Hidden marks a per-host identity made from the host form
	// (FR-HOST-010); PublicKey is the stored key's authorized_keys line,
	// shown as its type and fingerprint (FR-IDENT-003). Neither is secret.
	Hidden    bool   `json:"hidden,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
}

type fileHost struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Hostname     string   `json:"hostname"`
	Port         int      `json:"port"`
	User         string   `json:"user"`
	IdentityID   string   `json:"identity_id"`
	GroupID      string   `json:"group_id"`
	Tags         []string `json:"tags"`
	JumpHostIDs  []string `json:"jump_host_ids"`
	ForwardAgent bool     `json:"forward_agent"`
}

func (f fileConfig) toDomain() domain.Data {
	d := domain.Data{Version: Version}
	d.Settings.Theme = f.Settings.Theme
	for _, g := range f.Groups {
		dg := domain.Group{Meta: domain.Meta{ID: domain.ID(g.ID)}, Name: g.Name}
		if g.ParentID != nil {
			dg.ParentID = domain.ID(*g.ParentID)
		}
		d.Groups = append(d.Groups, dg)
	}
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
			Hidden:        i.Hidden,
			PublicKey:     i.PublicKey,
		})
	}
	for _, h := range f.Hosts {
		dh := domain.Host{
			Meta:       domain.Meta{ID: domain.ID(h.ID)},
			Label:      h.Label,
			Hostname:   h.Hostname,
			Port:       h.Port,
			User:       h.User,
			IdentityID: domain.ID(h.IdentityID),
			GroupID:    domain.ID(h.GroupID),
			Tags:       h.Tags,
		}
		for _, j := range h.JumpHostIDs {
			dh.JumpHostIDs = append(dh.JumpHostIDs, domain.ID(j))
		}
		dh.Options.ForwardAgent = h.ForwardAgent
		d.Hosts = append(d.Hosts, dh)
	}
	return d
}

func fromDomain(d domain.Data) fileConfig {
	f := fileConfig{
		Version:    Version,
		Settings:   fileSettings{Theme: d.Settings.Theme},
		Groups:     []fileGroup{},
		Identities: []fileIdentity{},
		Hosts:      []fileHost{},
	}
	for _, g := range d.Groups {
		fg := fileGroup{ID: string(g.ID), Name: g.Name}
		if g.ParentID != "" {
			p := string(g.ParentID)
			fg.ParentID = &p
		}
		f.Groups = append(f.Groups, fg)
	}
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
			Hidden:           i.Hidden,
			PublicKey:        i.PublicKey,
		})
	}
	for _, h := range d.Hosts {
		fh := fileHost{
			ID:           string(h.ID),
			Label:        h.Label,
			Hostname:     h.Hostname,
			Port:         h.Port,
			User:         h.User,
			IdentityID:   string(h.IdentityID),
			GroupID:      string(h.GroupID),
			Tags:         append([]string{}, h.Tags...),
			JumpHostIDs:  []string{},
			ForwardAgent: h.Options.ForwardAgent,
		}
		for _, j := range h.JumpHostIDs {
			fh.JumpHostIDs = append(fh.JumpHostIDs, string(j))
		}
		f.Hosts = append(f.Hosts, fh)
	}
	return f
}
