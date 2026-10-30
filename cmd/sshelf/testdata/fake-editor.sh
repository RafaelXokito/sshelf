#!/bin/sh
# A fake $EDITOR for sshelf's edit-locally E2E tests (FR-SFTP-019): it never
# opens a real editor. The test copies it into FAKE_EDITOR_DIR and points
# EDITOR at the copy. The file "mode" there says what to do with the copy,
# whose path is the last argument:
#
#   append   append a line (also the default)
#   same     change nothing
#   fail     change nothing and exit 1
#   remote   append a line and change the remote file named in "remote"
#   wait     append a line after "wait" seconds (a file holding a number)
#
# It appends "start <copy>" and "end" to the file "log" and each argument
# on a line of its own to "args".
d=$FAKE_EDITOR_DIR
last=
for a in "$@"; do
	last=$a
	printf '%s\n' "$a" >> "$d/args"
done
printf 'start %s\n' "$last" >> "$d/log"
mode=$(cat "$d/mode" 2>/dev/null)
case $mode in
same) ;;
fail)
	printf 'end\n' >> "$d/log"
	exit 1
	;;
remote)
	printf 'edited by the fake editor\n' >> "$last"
	printf 'changed on the server\n' >> "$(cat "$d/remote")"
	;;
wait)
	sleep "$(cat "$d/wait")"
	printf 'edited by the fake editor\n' >> "$last"
	;;
*) printf 'edited by the fake editor\n' >> "$last" ;;
esac
printf 'end\n' >> "$d/log"
