package store

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

// extras are the fields of config.json this build does not know: fields a
// newer sshelf added (D-027). Save writes them back, so that running an
// older release never drops what a newer one wrote (FR-CFG-011). Fields
// of an object in a list are kept by the object's id; they go when the
// object goes.
type extras struct {
	top, settings             map[string]json.RawMessage
	groups, identities, hosts map[string]map[string]json.RawMessage // by id
}

// Known keys of each object, from the json tags of the file structs.
var (
	topKeys      = jsonKeys(reflect.TypeFor[fileConfig]())
	settingsKeys = jsonKeys(reflect.TypeFor[fileSettings]())
	groupKeys    = jsonKeys(reflect.TypeFor[fileGroup]())
	identityKeys = jsonKeys(reflect.TypeFor[fileIdentity]())
	hostKeys     = jsonKeys(reflect.TypeFor[fileHost]())
)

func jsonKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := range t.NumField() {
		if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}

// unknown returns the members of obj whose keys are not in known, or nil.
func unknown(obj map[string]json.RawMessage, known map[string]bool) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	for k, v := range obj {
		if !known[k] {
			if out == nil {
				out = map[string]json.RawMessage{}
			}
			out[k] = v
		}
	}
	return out
}

// readExtras returns the unknown fields of the config.json at path. A
// missing, oversized or unparsable file has none.
func readExtras(path string) extras {
	f, err := os.Open(path) //nolint:gosec // path comes from internal/paths
	if err != nil {
		return extras{}
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil || len(b) > MaxFileSize {
		return extras{}
	}
	return parseExtras(b)
}

func parseExtras(b []byte) extras {
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) != nil {
		return extras{}
	}
	ex := extras{top: unknown(top, topKeys)}
	var settings map[string]json.RawMessage
	if json.Unmarshal(top["settings"], &settings) == nil {
		ex.settings = unknown(settings, settingsKeys)
	}
	byID := func(key string, known map[string]bool) map[string]map[string]json.RawMessage {
		var list []map[string]json.RawMessage
		if json.Unmarshal(top[key], &list) != nil {
			return nil
		}
		out := map[string]map[string]json.RawMessage{}
		for _, obj := range list {
			var id string
			if json.Unmarshal(obj["id"], &id) != nil || id == "" {
				continue
			}
			if u := unknown(obj, known); u != nil {
				out[id] = u
			}
		}
		return out
	}
	ex.groups = byID("groups", groupKeys)
	ex.identities = byID("identities", identityKeys)
	ex.hosts = byID("hosts", hostKeys)
	return ex
}

// withExtra appends the members of extra, sorted by key, to the compact
// JSON object b.
func withExtra(b []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return b, nil
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := bytes.TrimSuffix(b, []byte("}"))
	for _, k := range keys {
		if len(out) > 1 {
			out = append(out, ',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		var v bytes.Buffer
		if err := json.Compact(&v, extra[k]); err != nil {
			return nil, err
		}
		out = append(append(append(out, kb...), ':'), v.Bytes()...)
	}
	return append(out, '}'), nil
}

// rawConfig is fileConfig with each object already encoded, extras
// included.
type rawConfig struct {
	Version    int               `json:"version"`
	Settings   json.RawMessage   `json:"settings"`
	Groups     []json.RawMessage `json:"groups"`
	Identities []json.RawMessage `json:"identities"`
	Hosts      []json.RawMessage `json:"hosts"`
}

// encodeWith is encode with the unknown fields ex written back.
func encodeWith(f fileConfig, ex extras) ([]byte, error) {
	obj := func(v any, extra map[string]json.RawMessage) (json.RawMessage, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return withExtra(b, extra)
	}
	r := rawConfig{Version: f.Version, Groups: []json.RawMessage{}, Identities: []json.RawMessage{}, Hosts: []json.RawMessage{}}
	var err error
	if r.Settings, err = obj(f.Settings, ex.settings); err != nil {
		return nil, err
	}
	for _, g := range f.Groups {
		b, err := obj(g, ex.groups[g.ID])
		if err != nil {
			return nil, err
		}
		r.Groups = append(r.Groups, b)
	}
	for _, i := range f.Identities {
		b, err := obj(i, ex.identities[i.ID])
		if err != nil {
			return nil, err
		}
		r.Identities = append(r.Identities, b)
	}
	for _, h := range f.Hosts {
		b, err := obj(h, ex.hosts[h.ID])
		if err != nil {
			return nil, err
		}
		r.Hosts = append(r.Hosts, b)
	}
	b, err := obj(r, ex.top)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}
