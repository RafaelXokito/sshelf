// Package domain deliberately imports store, which design §5.2 forbids.
package domain

import _ "example.com/forbidden/internal/store"
