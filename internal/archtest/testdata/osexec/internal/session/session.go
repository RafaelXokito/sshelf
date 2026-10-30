// Package session deliberately imports os/exec, which only internal/ui/editor may.
package session

import _ "os/exec"
