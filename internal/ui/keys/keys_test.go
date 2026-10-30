package keys

import (
	"slices"
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
