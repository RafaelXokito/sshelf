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
mkdir -p "$work/srv" "$work/home/xdg/sshelf"
cp internal/session/testdata/replay/htop.bin "$work/srv/"

start_server() { # name port
	(cd "$work/srv" && exec "$work/bin/sshtestd" -dir "$work/$1" -port "$2") >"$work/$1.out" 2>&1 &
	pids+=($!)
	for _ in $(seq 100); do
		grep -q '^demo key' "$work/$1.out" 2>/dev/null && return 0
		sleep 0.1
	done
	echo "sshtestd $1 did not start:" >&2
	cat "$work/$1.out" >&2
	exit 1
}
start_server a "$PORT_A"
key="$work/a/demo_ed25519"
# Server B reuses server A's demo key, so one identity logs in to both.
mkdir -p "$work/b"
cp "$key" "$work/b/demo_ed25519"
start_server b "$PORT_B"

cfg="$work/home/xdg/sshelf"
# Trust server A only, so the TOFU prompt shows once, for server B.
grep -A1 '^known_hosts line' "$work/a.out" | tail -1 >"$cfg/known_hosts"
chmod 600 "$cfg/known_hosts"
cat >"$cfg/config.json" <<EOF
{
  "version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "username": "demo", "key_path": "$key"}],
  "hosts": [
    {"id": "0192a000-0000-7000-8000-000000000011", "label": "web-prod", "hostname": "127.0.0.1", "port": $PORT_A, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001", "tags": ["prod"]},
    {"id": "0192a000-0000-7000-8000-000000000012", "label": "db-staging", "hostname": "127.0.0.1", "port": $PORT_A, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001", "tags": ["staging"]},
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
