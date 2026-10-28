#!/bin/sh
# Tests scripts/next-version.sh, the version the release workflow publishes.
#
# Usage: scripts/test-next-version.sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
failures=0

NEXT_VERSION_SOURCED=1
export NEXT_VERSION_SOURCED
# shellcheck disable=SC1091
. "$here/next-version.sh"

# expect NAME TAGS CHANNEL BUMP VERSION WANT: WANT is the printed version, or
# "error" when it must fail.
expect() {
	if got=$(printf '%s' "$2" | next_version "$3" "$4" "$5" 2>/dev/null); then :; else got=error; fi
	if [ "$got" = "$6" ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s (got %s, want %s)\n' "$1" "$got" "$6"
		failures=$((failures + 1))
	fi
}

nl='
'
expect "first alpha" "" alpha minor "" 0.1.0-alpha.1
expect "first beta" "" beta minor "" 0.1.0-beta.1
expect "first stable" "" stable minor "" 0.1.0
expect "next alpha" "v0.1.0-alpha.1${nl}v0.1.0-alpha.2" alpha minor "" 0.1.0-alpha.3
expect "alpha numbers compare as numbers" "v0.1.0-alpha.9${nl}v0.1.0-alpha.10" alpha minor "" 0.1.0-alpha.11
expect "beta after alphas" "v0.1.0-alpha.2" beta minor "" 0.1.0-beta.1
expect "release after betas" "v0.1.0-alpha.2${nl}v0.1.0-beta.3" stable major "" 0.1.0
expect "no alpha after a beta" "v0.1.0-beta.1" alpha minor "" error
expect "new minor after a release" "v0.1.0" beta minor "" 0.2.0-beta.1
expect "new patch after a release" "v0.1.0" stable patch "" 0.1.1
expect "new major after a release" "v0.1.0" alpha major "" 1.0.0-alpha.1
expect "bump ignored while a version is in beta" "v0.1.0${nl}v0.2.0-beta.1" beta major "" 0.2.0-beta.2
expect "explicit version" "v0.1.0${nl}v0.2.0-beta.1" alpha minor 1.0.0 1.0.0-alpha.1
expect "explicit version already released" "v0.1.0" stable minor 0.1.0 error
expect "explicit version older than the release" "v0.2.0" stable minor 0.1.5 error
expect "explicit version not X.Y.Z" "" stable minor 1.0 error
expect "unknown channel" "" nightly minor "" error
expect "unknown bump" "" stable huge "" error
expect "other tags are ignored" "plugin/x/v1.0.0${nl}v9.9.9-dev.1${nl}v0.1.0" stable minor "" 0.2.0
expect "an rc counts as a beta" "v0.2.0-rc.1" alpha minor "" error

if [ "$failures" -gt 0 ]; then
	printf '%s next-version tests failed\n' "$failures"
	exit 1
fi
echo "next-version tests passed"
