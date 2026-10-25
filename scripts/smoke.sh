#!/usr/bin/env bash
# End-to-end smoke test of the built binary: starts the hub with a temporary
# data directory, checks the health endpoint and the embedded dashboard, then
# checks that SIGTERM stops it cleanly.
#
# Usage: scripts/smoke.sh [path/to/regulus]   (default: bin/regulus)
set -euo pipefail

bin=${1:-bin/regulus}
tmp=$(mktemp -d)
log=$tmp/hub.log
pid=

cleanup() {
	if [[ -n $pid ]] && kill -0 "$pid" 2>/dev/null; then
		kill -KILL "$pid" 2>/dev/null || true
	fi
	rm -rf "$tmp"
}
trap cleanup EXIT

fail() {
	echo "smoke test FAILED: $*" >&2
	echo "--- hub log ---" >&2
	cat "$log" >&2 || true
	exit 1
}

# Port 0 lets the system pick a free port; the hub logs the real address.
"$bin" hub --listen 127.0.0.1:0 --data-dir "$tmp/data" >"$log" 2>&1 &
pid=$!

addr=
for _ in $(seq 1 100); do
	addr=$(sed -n 's/.*hub listening.*addr=\([0-9.]*:[0-9]*\).*/\1/p' "$log" | head -n1)
	[[ -n $addr ]] && break
	kill -0 "$pid" 2>/dev/null || fail "hub exited during startup"
	sleep 0.1
done
[[ -n $addr ]] || fail "hub did not report its listen address within 10s"
base=http://$addr

health=$(curl -fsS "$base/healthz") || fail "/healthz request failed"
[[ $health == ok ]] || fail "/healthz answered '$health', want 'ok'"

page=$(curl -fsS "$base/") || fail "/ request failed (is the dashboard embedded? run make build)"
grep -q '<div id="root">' <<<"$page" || fail "/ does not serve the dashboard (is it embedded? run make build)"

# The dashboard bundle referenced by the page must be served too.
script=$(sed -n 's/.*<script[^>]*src="\([^"]*\)".*/\1/p' <<<"$page" | head -n1)
[[ -n $script ]] || fail "no script found in the dashboard page"
curl -fsS -o /dev/null "$base$script" || fail "dashboard bundle $script is not served"

[[ -s $tmp/data/regulus.db ]] || fail "database file was not created"

kill -TERM "$pid"
status=0
wait "$pid" || status=$?
pid=
[[ $status -eq 0 ]] || fail "hub exited with status $status after SIGTERM"
grep -q 'shutting down' "$log" || fail "hub did not log a graceful shutdown"

echo "smoke test passed ($addr)"
