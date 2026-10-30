package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/sshconfig"
	"github.com/RafaelXokito/sshelf/internal/store"
)

// ErrNoSSHConfig means there is no ~/.ssh/config to import.
var ErrNoSSHConfig = errors.New("no ~/.ssh/config found")

// ImportReport is the result of Service.ImportSSHConfig (FR-IMPORT-003).
// Every string in it is sanitised.
type ImportReport struct {
	File       string   // the ssh config that was read
	Created    []string // labels of the hosts created, in config order
	Identities []string // labels of the identities created
	Skipped    []SkippedAlias
	// NotImported lists, per created host, the directives that were not
	// imported, such as ProxyCommand or Match.
	NotImported []HostDirectives

	// AllHosts and AllIdentities are the whole config after the import,
	// hosts in last-use order and identities by label, for the UI to show.
	AllHosts      []domain.Host
	AllIdentities []domain.Identity
}

// SkippedAlias is an alias the import did not create a host for.
type SkippedAlias struct {
	Alias  string
	Reason string
}

// HostDirectives lists the directives of one host that were not imported.
type HostDirectives struct {
	Label      string
	Directives []string
}

// SkipLabelExists is the reason for an alias whose label is already used.
const SkipLabelExists = "label already exists"

// ImportSSHConfig implements Service (FR-IMPORT-001, D-103). It reads
// ~/.ssh/config with its Includes (nothing in it is executed) and copies
// it into config.json once:
//
//   - one host per concrete alias, unless a host with that label exists
//     (case-insensitive), which is reported as skipped; an alias that fails
//     FR-HOST-003 validation is skipped with the reason;
//   - one identity per distinct user and key file used by a created host,
//     reusing an existing identity with the same user and key path. Key
//     files are referenced by path and never read or copied into the
//     credential store (ADR-010);
//   - every imported string is sanitised (SEC-SESSION-003).
//
// config.json is written once; memory changes only after that write
// succeeded. sshelf never writes to ~/.ssh/config.
func (a *App) ImportSSHConfig(ctx context.Context) (ImportReport, error) {
	if err := ctx.Err(); err != nil {
		return ImportReport{}, err
	}
	home, err := a.home()
	if err != nil {
		return ImportReport{}, err
	}
	file := sshconfig.DefaultPath(home)
	cfg, err := sshconfig.Load(file, sshconfig.Options{Home: home, LocalUser: localUser()})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ImportReport{}, ErrNoSSHConfig
		}
		return ImportReport{}, err
	}
	plan := cfg.Plan()

	a.mu.Lock()
	defer a.mu.Unlock()
	data := a.data
	data.Hosts = slices.Clone(a.data.Hosts)
	data.Identities = slices.Clone(a.data.Identities)
	rep := ImportReport{File: file}
	now := time.Now().UTC()
	planIDs := make([]domain.ID, len(plan.Identities)) // identity created or reused per plan index

	for _, hp := range plan.Hosts {
		h := sanitizeHost(hp.Host)
		alias := h.Import.Alias
		if slices.ContainsFunc(data.Hosts, func(o domain.Host) bool { return strings.EqualFold(strings.TrimSpace(o.Label), h.Label) }) {
			rep.Skipped = append(rep.Skipped, SkippedAlias{Alias: alias, Reason: SkipLabelExists})
			continue
		}
		if err := domain.ValidateHost(h, data); err != nil {
			rep.Skipped = append(rep.Skipped, SkippedAlias{Alias: alias, Reason: skipReason(err)})
			continue
		}
		if hp.Identity >= 0 {
			if planIDs[hp.Identity] == "" {
				id, created := findOrAddIdentity(&data, sanitizeIdentity(plan.Identities[hp.Identity]), now)
				planIDs[hp.Identity] = id.ID
				if created {
					rep.Identities = append(rep.Identities, id.Label)
				}
			}
			h.IdentityID = planIDs[hp.Identity]
		}
		h.ID = domain.NewID()
		h.CreatedAt, h.UpdatedAt = now, now
		h.Import.ImportedAt = now
		data.Hosts = append(data.Hosts, h)
		rep.Created = append(rep.Created, h.Label)
		if len(h.Import.Unsupported) > 0 {
			rep.NotImported = append(rep.NotImported, HostDirectives{Label: h.Label, Directives: slices.Clone(h.Import.Unsupported)})
		}
	}

	if len(rep.Created) > 0 {
		cfgFile := paths.ConfigFile(a.cfg.Dir)
		if err := store.Save(cfgFile, &store.Snapshot{Data: data}); err != nil {
			slog.Debug("import: save failed", "err", err)
			return ImportReport{}, fmt.Errorf("could not save %s: %w", filepath.Base(cfgFile), err)
		}
		a.data = data
	}
	slog.Debug("import: done", "file", file, "hosts", len(rep.Created), "identities", len(rep.Identities), "skipped", len(rep.Skipped))
	rep.AllHosts = sortedHosts(a.data.Hosts)
	rep.AllIdentities = slices.Clone(a.data.Identities)
	slices.SortStableFunc(rep.AllIdentities, func(x, y domain.Identity) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return rep, nil
}

// home is the directory ~ stands for.
func (a *App) home() (string, error) {
	if a.cfg.Home != "" {
		return a.cfg.Home, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return h, nil
}

// localUser is the login name %u and %r stand for in an IdentityFile.
func localUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func sanitizeHost(h domain.Host) domain.Host {
	h.Label = strings.TrimSpace(domain.Sanitize(h.Label))
	h.Hostname = domain.Sanitize(h.Hostname)
	h.User = domain.Sanitize(h.User)
	src := *h.Import
	src.File = domain.Sanitize(src.File)
	src.Alias = domain.Sanitize(src.Alias)
	src.Unsupported = make([]string, len(h.Import.Unsupported))
	for i, u := range h.Import.Unsupported {
		src.Unsupported[i] = domain.Sanitize(u)
	}
	h.Import = &src
	return h
}

func sanitizeIdentity(id domain.Identity) domain.Identity {
	id.Label = strings.TrimSpace(domain.Sanitize(id.Label))
	id.Username = domain.Sanitize(id.Username)
	id.KeyPath = domain.Sanitize(id.KeyPath)
	return id
}

// findOrAddIdentity returns the shared identity with want's user and key
// path, adding want to d when there is none. A new identity whose label is
// taken gets " (2)", " (3)", … appended.
func findOrAddIdentity(d *domain.Data, want domain.Identity, now time.Time) (domain.Identity, bool) {
	if i := slices.IndexFunc(d.Identities, func(o domain.Identity) bool {
		return !o.Hidden && !o.HasKey && o.Username == want.Username && o.KeyPath == want.KeyPath
	}); i >= 0 {
		return d.Identities[i], false
	}
	taken := func(l string) bool {
		return slices.ContainsFunc(d.Identities, func(o domain.Identity) bool { return strings.EqualFold(o.Label, l) })
	}
	label := want.Label
	for n := 2; taken(label); n++ {
		label = want.Label + " (" + strconv.Itoa(n) + ")"
	}
	want.Label = label
	want.ID = domain.NewID()
	want.CreatedAt, want.UpdatedAt = now, now
	d.Identities = append(d.Identities, want)
	return want, true
}

// skipReason is the report text for a host that failed validation.
func skipReason(err error) string {
	ve, ok := errors.AsType[*domain.ValidationError](err)
	if !ok {
		return domain.Sanitize(err.Error())
	}
	var parts []string
	for _, f := range []domain.Field{domain.FieldLabel, domain.FieldHostname, domain.FieldPort, domain.FieldIdentity} {
		if msg, ok := ve.Fields[f]; ok {
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}
