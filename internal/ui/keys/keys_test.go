package keys

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestARCH_UI_004_UniqueKeysWithHelp(t *testing.T) {
	for _, m := range Maps() {
		owner := map[string]string{}
		for _, b := range m.Bindings {
			h := b.Help()
			if h.Key == "" || h.Desc == "" {
				t.Errorf("%s: binding %v has no help text", m.Name, b.Keys())
			}
			for _, k := range b.Keys() {
				if prev, ok := owner[k]; ok {
					t.Errorf("%s: key %q is bound to both %q and %q", m.Name, k, prev, h.Desc)
				}
				owner[k] = h.Desc
			}
		}
	}
}

func TestNFR_UX_001_EveryActionBound(t *testing.T) {
	maps := Maps()
	if len(maps) == 0 {
		t.Fatal("no keymaps")
	}
	for _, m := range maps {
		if len(m.Bindings) == 0 {
			t.Errorf("%s: no bindings", m.Name)
		}
		for _, b := range m.Bindings {
			if len(b.Keys()) == 0 || !b.Enabled() {
				t.Errorf("%s: action %q has no key", m.Name, b.Help().Desc)
			}
		}
	}
	// Every FR-NAV-003 host-list action is in the host-list map.
	var list Map
	for _, m := range maps {
		if m.Name == "host list" {
			list = m
		}
	}
	for _, want := range []string{"up", "k", "down", "j", "g", "G", "/", "enter", "a", "e", "d", "i", "I", "q", "ctrl+c"} {
		if !slices.ContainsFunc(list.Bindings, func(b key.Binding) bool { return slices.Contains(b.Keys(), want) }) {
			t.Errorf("host list has no binding for %q", want)
		}
	}
	// Every mouse action (M2 revision, D-011) has a key binding that does
	// the same, and that binding is in a view's keymap.
	inMaps := func(b key.Binding) bool {
		for _, m := range maps {
			if slices.ContainsFunc(m.Bindings, func(o key.Binding) bool { return slices.Equal(o.Keys(), b.Keys()) }) {
				return true
			}
		}
		return false
	}
	actions := MouseActions()
	if len(actions) == 0 {
		t.Fatal("no mouse actions")
	}
	// The SFTP tab's mouse actions (FR-SFTP-021, FR-SFTP-022) are among
	// them, each with a binding of the SFTP panes' keymap.
	var panes Map
	for _, m := range maps {
		if m.Name == "sftp panes" {
			panes = m
		}
	}
	sftp := 0
	for _, a := range actions {
		if !strings.HasPrefix(a.Name, "sftp: ") {
			continue
		}
		sftp++
		for _, b := range a.Keys {
			if !slices.ContainsFunc(panes.Bindings, func(o key.Binding) bool { return slices.Equal(o.Keys(), b.Keys()) }) {
				t.Errorf("mouse action %q: binding %q is not an SFTP pane key", a.Name, b.Help().Desc)
			}
		}
	}
	if sftp < 6 {
		t.Errorf("%d SFTP mouse actions, want click, border, double-click, wheel, progress line and drag", sftp)
	}
	for _, a := range actions {
		if len(a.Keys) == 0 {
			t.Errorf("mouse action %q has no key", a.Name)
		}
		for _, b := range a.Keys {
			if len(b.Keys()) == 0 || !b.Enabled() || !inMaps(b) {
				t.Errorf("mouse action %q: binding %q is in no keymap", a.Name, b.Help().Desc)
			}
		}
	}
}
