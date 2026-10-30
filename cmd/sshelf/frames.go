//go:build !sshelf_faultinject

package main

import tea "charm.land/bubbletea/v2"

// recordFrame does nothing outside the fault-injection test build.
func recordFrame(tea.View, tea.Model) {}
