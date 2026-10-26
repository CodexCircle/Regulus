#!/bin/sh
# Tests the functions of packaging/install.sh without touching the system:
# version and checksum checks, option handling and the settings file.
#
# Usage: scripts/test-install.sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
failures=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

REGULUS_INSTALL_SOURCED=1
export REGULUS_INSTALL_SOURCED
# shellcheck disable=SC1091
. "$here/../packaging/install.sh"
set +e # the tests check exit statuses themselves

pass() { printf 'ok   %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1"
	failures=$((failures + 1))
}

# check NAME COMMAND...: the command must succeed.
check() {
	name=$1
	shift
	if "$@" >/dev/null 2>&1; then pass "$name"; else fail "$name"; fi
}

# check_not NAME COMMAND...: the command must fail.
check_not() {
	name=$1
	shift
	if "$@" >/dev/null 2>&1; then fail "$name"; else pass "$name"; fi
}

# equal NAME ACTUAL EXPECTED
equal() {
	if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (got '$2', want '$3')"; fi
}

# in_subshell runs a function that may call die (and so exit).
in_subshell() ( "$@" )

check "accepts a stable version" valid_version 1.2.3
check "accepts a beta version" valid_version 0.3.0-beta.1
check "accepts a snapshot version" valid_version 0.0.0-SNAPSHOT-ade16df
check_not "rejects a word" valid_version latest
check_not "rejects an empty version" valid_version ""
check_not "rejects shell characters" valid_version '1.2.3;rm'
check_not "rejects a path" valid_version ../1.2.3

# checksums
dir=$tmp/dl
mkdir -p "$dir"
printf 'package' >"$dir/regulus_1.0.0_linux_amd64.deb"
sum=$(cd "$dir" && sha256sum regulus_1.0.0_linux_amd64.deb | cut -d' ' -f1)
printf '%s  regulus_1.0.0_linux_amd64.deb\n' "$sum" >"$dir/checksums.txt"
check "accepts a matching checksum" in_subshell verify_checksum "$dir" regulus_1.0.0_linux_amd64.deb
printf 'tampered' >"$dir/regulus_1.0.0_linux_amd64.deb"
check_not "rejects a changed file" in_subshell verify_checksum "$dir" regulus_1.0.0_linux_amd64.deb
check_not "rejects a file missing from checksums.txt" in_subshell verify_checksum "$dir" regulus_1.0.0_linux_arm64.deb

# signature: without cosign only --require-signature turns it into an error
require_signature=0
# shellcheck disable=SC2329 # called by verify_signature
have() { [ "$1" != cosign ] && command -v "$1" >/dev/null 2>&1; }
check "warns but goes on without cosign" in_subshell verify_signature "$dir"
# shellcheck disable=SC2034
require_signature=1
check_not "fails without cosign when a signature is required" in_subshell verify_signature "$dir"
unset -f have
have() { command -v "$1" >/dev/null 2>&1; }

# architecture
# shellcheck disable=SC2329 # called by detect_arch
uname() { if [ "${1:-}" = -m ]; then echo "$fake_arch"; else command uname "$@"; fi; }
fake_arch=x86_64
equal "x86_64 is amd64" "$(detect_arch)" amd64
fake_arch=aarch64
equal "aarch64 is arm64" "$(detect_arch)" arm64
fake_arch=armv7l
check_not "rejects 32-bit arm" detect_arch
unset -f uname

# the answer about the built-in agent
builtin_agent=true
# shellcheck disable=SC2034 # read by ask_builtin_agent
assume_yes=0
ask_builtin_agent
equal "keeps an answer given by flag" "$builtin_agent" true
builtin_agent=
# shellcheck disable=SC2034
assume_yes=1
ask_builtin_agent
equal "uses the default without asking" "$builtin_agent" default

# settings file
# shellcheck disable=SC2034 # read by write_settings
SUDO=
CONFIG_DIR=$tmp/etc
builtin_agent=default
self_update=0
write_settings
check_not "the default writes nothing" test -e "$CONFIG_DIR/regulus.env"
printf 'REGULUS_LOG_LEVEL=debug\nREGULUS_BUILTIN_AGENT=true\n' >"$tmp/seed"
mkdir -p "$CONFIG_DIR"
cp "$tmp/seed" "$CONFIG_DIR/regulus.env"
builtin_agent=false
write_settings
equal "replaces the answer and keeps other settings" "$(cat "$CONFIG_DIR/regulus.env")" "REGULUS_LOG_LEVEL=debug
REGULUS_BUILTIN_AGENT=false"

builtin_agent=default
self_update=1
write_settings
equal "switches the update helper on and keeps the rest" "$(cat "$CONFIG_DIR/regulus.env")" "REGULUS_LOG_LEVEL=debug
REGULUS_BUILTIN_AGENT=false
REGULUS_UPDATES_APPLY=true"
write_settings
equal "does not repeat a key" "$(grep -c UPDATES_APPLY "$CONFIG_DIR/regulus.env")" 1
SUDO=
(systemctl() { echo called; }; self_update=0; enable_self_update; ) >/dev/null
rm -f "$CONFIG_DIR/regulus.env"

# the port to wait for
printf 'listen: "127.0.0.1:9090" # comment\nbuiltin_agent: true\n' >"$CONFIG_DIR/config.yaml"
equal "reads the port from config.yaml" "$(REGULUS_LISTEN="" listen_port)" 9090
equal "the environment wins for the port" "$(REGULUS_LISTEN=:7000 listen_port)" 7000

# the agent role
equal "adds the default port" "$(normalize_hub hub.example)" hub.example:8443
equal "keeps the given port" "$(normalize_hub hub.example:9000)" hub.example:9000
equal "accepts a URL" "$(normalize_hub https://hub.example:9000/)" hub.example:9000
equal "accepts an IPv4 address" "$(normalize_hub 10.0.0.5:8443)" 10.0.0.5:8443
equal "accepts an IPv6 address in brackets" "$(normalize_hub '[::1]:8443')" '[::1]:8443'
equal "adds the port to a bracketed IPv6 address" "$(normalize_hub '[fd00::1]')" '[fd00::1]:8443'
check_not "rejects an empty address" normalize_hub ""
check_not "rejects a port of zero" normalize_hub hub.example:0
check_not "rejects a port that is too large" normalize_hub hub.example:70000
check_not "rejects a port that is not a number" normalize_hub hub.example:http
check_not "rejects a missing host" normalize_hub :8443
check_not "rejects an IPv6 address without brackets" normalize_hub fd00::1:8443
check_not "rejects shell characters in the address" normalize_hub 'hub;rm:8443'
check_not "rejects spaces in the address" normalize_hub 'hub example:8443'

check "accepts a token" valid_token Ab3_-xYz0123456789
check_not "rejects an empty token" valid_token ""
check_not "rejects shell characters in a token" valid_token 'abc;rm -rf /'
fp=sha256:$(printf 'a%.0s' $(seq 1 64))
check "accepts a fingerprint" valid_fingerprint "$fp"
check_not "rejects a fingerprint without the prefix" valid_fingerprint "${fp#sha256:}"
check_not "rejects a short fingerprint" valid_fingerprint sha256:abcd
check_not "rejects a fingerprint that is not hex" valid_fingerprint "sha256:$(printf 'z%.0s' $(seq 1 64))"

# shellcheck disable=SC2034 # read by check_agent_args
hub=hub.example
# shellcheck disable=SC2034
token=abc
# shellcheck disable=SC2034
ca_fingerprint=$fp
check "accepts complete agent options" in_subshell check_agent_args
hub=
check_not "needs the hub address" in_subshell check_agent_args
# shellcheck disable=SC2034
hub=hub.example
token=
check_not "needs a token" in_subshell check_agent_args
# shellcheck disable=SC2034
token=abc
ca_fingerprint=
check_not "needs the CA fingerprint" in_subshell check_agent_args
# shellcheck disable=SC2034
ca_fingerprint=sha256:00
check_not "rejects a bad CA fingerprint" in_subshell check_agent_args

# the role file read by the package scripts
# shellcheck disable=SC2034 # read by set_role
SUDO=
CONFIG_DIR=$tmp/etc-role
# shellcheck disable=SC2034
role=agent
set_role
equal "the agent role is recorded" "$(cat "$CONFIG_DIR/role")" agent
# shellcheck disable=SC2034
role=hub
set_role
check_not "the hub role removes the record" test -e "$CONFIG_DIR/role"

# options
# The script is sourced here, so the children must not inherit that switch.
run_script() { env -u REGULUS_INSTALL_SOURCED sh "$here/../packaging/install.sh" "$@"; }
check "shows the help" run_script --help
check_not "rejects an unknown option" run_script --nope
check_not "needs a value for --version" run_script --version
check_not "rejects an unknown role" run_script --role server
check_not "the agent role needs a hub" run_script --role agent --token abc --ca-fingerprint "$fp"
check_not "the agent role needs a token" run_script --role agent --hub hub.example --ca-fingerprint "$fp"
check_not "the agent role needs a fingerprint" run_script --role agent --hub hub.example --token abc
check_not "the hub role takes no token" run_script --role hub --token abc
check_not "the agent role takes no built-in agent flag" run_script --role agent --hub hub.example --token abc --ca-fingerprint "$fp" --no-builtin-agent

if [ "$failures" -ne 0 ]; then
	printf '%d test(s) failed\n' "$failures"
	exit 1
fi
echo "install script tests passed"
