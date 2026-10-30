#!/bin/sh
# A fake editor for the editor package's tests: it never edits anything.
# It writes each argument it received on a line of its own to the file
# named by FAKE_EDITOR_OUT, so a test can see how the arguments arrived.
for a in "$@"; do
	printf '%s\n' "$a" >> "$FAKE_EDITOR_OUT"
done
