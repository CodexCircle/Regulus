#!/usr/bin/env bash
# End-to-end smoke test of the built binary. It starts the hub with a temporary
# data directory and checks the health endpoint, the embedded dashboard, that
# the API is closed without a login, the first-run setup of the admin, that the
# built-in agent connects (GET /api/agents) and that SIGTERM stops the hub
# cleanly. Then it starts a hub with --builtin-agent=false on the same data and
# checks that, after logging in, no agent shows up and that `hub reset-admin`
# recovers the admin account.
#
# Usage: scripts/smoke.sh [path/to/regulus]   (default: bin/regulus)
set -euo pipefail

bin=${1:-bin/regulus}
tmp=$(mktemp -d)
log=$tmp/hub.log
pid=
addr=
jar=$tmp/cookies
password='smoke-test-password'

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

# start_hub [flags...] runs the hub in the background and sets pid and addr.
# Port 0 lets the system pick a free port; the hub logs the real address.
start_hub() {
	: >"$log"
	"$bin" hub --listen 127.0.0.1:0 --data-dir "$tmp/data" "$@" >"$log" 2>&1 &
	pid=$!

	addr=
	for _ in $(seq 1 100); do
		addr=$(sed -n 's/.*hub listening.*addr=\([0-9.]*:[0-9]*\).*/\1/p' "$log" | head -n1)
		[[ -n $addr ]] && break
		kill -0 "$pid" 2>/dev/null || fail "hub exited during startup"
		sleep 0.1
	done
	[[ -n $addr ]] || fail "hub did not report its listen address within 10s"
}

# stop_hub stops the hub with SIGTERM and checks that it shuts down cleanly.
stop_hub() {
	kill -TERM "$pid"
	local status=0
	wait "$pid" || status=$?
	pid=
	[[ $status -eq 0 ]] || fail "hub exited with status $status after SIGTERM"
	grep -q 'shutting down' "$log" || fail "hub did not log a graceful shutdown"
}

start_hub
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

# The API is closed without a login; the first run asks for the admin, which
# needs the one-time token from the log.
code=$(curl -sS -o /dev/null -w '%{http_code}' "$base/api/agents") || fail "/api/agents request failed"
[[ $code == 401 ]] || fail "/api/agents without a login answered $code, want 401"

setup_token=$(sed -n 's/.*setup_token=\([^ ]*\).*/\1/p' "$log" | head -n1)
[[ -n $setup_token ]] || fail "the hub did not log a setup token"
code=$(curl -sS -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' \
	-d "{\"setup_token\":\"$setup_token\",\"username\":\"admin\",\"password\":\"$password\"}" \
	"$base/api/auth/setup") || fail "setup request failed"
[[ $code == 200 ]] || fail "setup answered $code, want 200"

# The built-in agent must connect through the contract and report a heartbeat.
agents=
for _ in $(seq 1 100); do
	agents=$(curl -fsS -b "$jar" "$base/api/agents") || fail "/api/agents request failed"
	if grep -q '"id":"local"' <<<"$agents" && ! grep -q '"last_heartbeat":null' <<<"$agents"; then
		break
	fi
	agents=
	sleep 0.1
done
[[ -n $agents ]] || fail "built-in agent 'local' did not connect and send a heartbeat within 10s"
grep -q '"status":"connected"' <<<"$agents" || fail "built-in agent is not reported as connected: $agents"

stop_hub

# With the built-in agent disabled no agent must appear.
start_hub --builtin-agent=false
rm -f "$jar"
code=$(curl -sS -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' \
	-d "{\"username\":\"admin\",\"password\":\"$password\"}" "http://$addr/api/auth/login") || fail "login request failed"
[[ $code == 200 ]] || fail "login answered $code, want 200"
sleep 1
agents=$(curl -fsS -b "$jar" "http://$addr/api/agents") || fail "/api/agents request failed"
[[ $agents == '[]' ]] || fail "expected no agents with --builtin-agent=false, got: $agents"

# A locked-out admin gets back in with the reset-admin command, while the hub
# keeps running: the link ends the old session and sets a new password.
link=$("$bin" hub reset-admin --data-dir "$tmp/data" 2>/dev/null) || fail "reset-admin failed"
reset_token=${link##*/#/reset/}
[[ -n $reset_token && $reset_token != "$link" ]] || fail "reset-admin printed an unexpected link: $link"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$jar" "http://$addr/api/agents") || fail "request failed"
[[ $code == 401 ]] || fail "the old session still works after reset-admin (status $code)"
new_password='smoke-new-password'
code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"token\":\"$reset_token\",\"new_password\":\"$new_password\"}" "http://$addr/api/auth/reset") || fail "reset request failed"
[[ $code == 204 ]] || fail "reset answered $code, want 204"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"token\":\"$reset_token\",\"new_password\":\"$new_password\"}" "http://$addr/api/auth/reset") || fail "second reset request failed"
[[ $code == 400 ]] || fail "reusing the reset link answered $code, want 400"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"username\":\"admin\",\"password\":\"$password\"}" "http://$addr/api/auth/login") || fail "login request failed"
[[ $code == 401 ]] || fail "the old password still works after the reset (status $code)"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
	-d "{\"username\":\"admin\",\"password\":\"$new_password\"}" "http://$addr/api/auth/login") || fail "login request failed"
[[ $code == 200 ]] || fail "login with the new password answered $code, want 200"
stop_hub

echo "smoke test passed"
