#!/usr/bin/env bash
# Regenerates docs/demo.gif from docs/demo.tape with Charm's VHS.
#
# Needs vhs, ttyd and ffmpeg on PATH. Everything sshelf sees lives in a
# fresh temp directory: HOME and XDG_CONFIG_HOME point there, and the binary
# is built with -tags sshelf_faultinject, so credentials go to an in-memory
# store and never to the OS keychain. The two targets are local sshtestd
# demo servers on 127.0.0.1.
#
#	docs/make-demo.sh
set -euo pipefail

cd "$(dirname "$0")/.."
root=$(pwd)

PORT_A=${PORT_A:-2201}
PORT_B=${PORT_B:-2202}

work=$(mktemp -d "${TMPDIR:-/tmp}/sshelf-demo.XXXXXX")
pids=()
cleanup() {
	for p in "${pids[@]+"${pids[@]}"}"; do
		kill "$p" 2>/dev/null || true
		wait "$p" 2>/dev/null || true
	done
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

echo "building into $work/bin"
go build -tags sshelf_faultinject -o "$work/bin/sshelf" ./cmd/sshelf
go build -o "$work/bin/sshtestd" ./internal/testutil/sshtest/cmd/sshtestd

# The servers run from $work/srv so that the demo can type "replay htop.bin".
mkdir -p "$work/srv" "$work/home/xdg/sshelf" "$work/home/notes"
# The tape starts sshelf in the demo HOME, so the SFTP tab's local pane
# shows it.
printf 'todo: rotate certs\n' >"$work/home/notes/todo.txt"
cp internal/session/testdata/replay/htop.bin "$work/srv/"

start_server() { # name port [sshtestd flags...]
	local name=$1 port=$2
	shift 2
	(cd "$work/srv" && exec "$work/bin/sshtestd" -dir "$work/$name" -port "$port" "$@") >"$work/$name.out" 2>&1 &
	pids+=($!)
	for _ in $(seq 100); do
		grep -q '^demo key' "$work/$name.out" 2>/dev/null && return 0
		sleep 0.1
	done
	echo "sshtestd $name did not start:" >&2
	cat "$work/$name.out" >&2
	exit 1
}
# Server A also serves SFTP, from a home with a few files to copy.
remote_home="$work/sftp/home/pi"
mkdir -p "$remote_home/logs"
printf 'server {\n  listen 443 ssl;\n}\n' >"$remote_home/nginx.conf"
printf '#!/bin/sh\necho deploying\n' >"$remote_home/deploy.sh"
head -c 48000 /dev/zero | tr '\0' 'x' >"$remote_home/logs/access.log"
start_server a "$PORT_A" -sftp "$work/sftp"
key="$work/a/demo_ed25519"
# Server B reuses server A's demo key, so one identity logs in to both.
mkdir -p "$work/b"
cp "$key" "$work/b/demo_ed25519"
start_server b "$PORT_B"

cfg="$work/home/xdg/sshelf"
# Trust server A only, so the TOFU prompt shows once, for server B.
grep -A1 '^known_hosts line' "$work/a.out" | tail -1 >"$cfg/known_hosts"
chmod 600 "$cfg/known_hosts"
# db-staging was last used three hours ago (FR-NAV-018), so it heads the list;
# the other two have never been connected. (BSD date first, then GNU date.)
used=$(date -u -v-3H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '3 hours ago' +%Y-%m-%dT%H:%M:%SZ)
cat >"$cfg/config.json" <<EOF
{
  "version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "username": "demo", "key_path": "$key"}],
  "hosts": [
    {"id": "0192a000-0000-7000-8000-000000000011", "label": "web-prod", "hostname": "127.0.0.1", "port": $PORT_A, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001", "tags": ["prod"]},
    {"id": "0192a000-0000-7000-8000-000000000012", "label": "db-staging", "hostname": "127.0.0.1", "port": $PORT_A, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001", "tags": ["staging"], "last_connected_at": "$used"},
    {"id": "0192a000-0000-7000-8000-000000000013", "label": "raspberrypi", "hostname": "127.0.0.1", "port": $PORT_B, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001", "tags": ["home"]}
  ]
}
EOF
chmod 600 "$cfg/config.json"
chmod 700 "$work/home" "$work/home/xdg" "$cfg"

# vhs keeps the real HOME (its browser lives there); the tape starts sshelf
# with HOME and XDG_CONFIG_HOME set to the demo directory.
export DEMO_HOME="$work/home" SSHELF_BIN="$work/bin/sshelf"
vhs docs/demo.tape
ls -l "$root/docs/demo.gif"
