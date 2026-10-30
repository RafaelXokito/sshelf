# The README demo of sshelf. Run docs/make-demo.sh, not vhs directly: it
# builds sshelf, starts the two demo servers and writes the demo config.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 12
Set Framerate 12
Set PlaybackSpeed 1.0
Set TypingSpeed 60ms
Set Theme "Catppuccin Mocha"
Set CursorBlink false

Hide
Type `cd "$DEMO_HOME" && HOME="$DEMO_HOME" XDG_CONFIG_HOME="$DEMO_HOME/xdg" "$SSHELF_BIN"` Enter
Sleep 1.5s
Show

# The host list, then a fuzzy search.
Sleep 1.5s
Type "/"
Sleep 300ms
Type "web"
Sleep 1s
Enter
Sleep 1.5s

# A shell inside the frame, then a full-screen app.
Type "echo hello from sshelf"
Enter
Sleep 1.2s
Type "replay htop.bin"
Enter
Sleep 2.5s

# Back to the Hosts tab, and a second host, whose key is not trusted yet.
Ctrl+]
Type "h"
Sleep 1s
Escape
Sleep 300ms
Type "/"
Type "pi"
Sleep 800ms
Enter
Sleep 2.5s
Type "y"
Sleep 1.5s
Type "echo hi from the pi"
Enter
Sleep 1.5s

# Back to tab 1, then an SFTP tab on its connection: copy a remote
# file to the local pane.
Ctrl+]
Type "1"
Sleep 1.2s
Ctrl+]
Type "s"
Sleep 2s
Down
Down
Sleep 600ms
Type "c"
Sleep 2.5s
