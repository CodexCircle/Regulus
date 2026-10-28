#!/bin/sh
# Prints the next version to publish (without the leading v), given the
# existing tags on standard input, one per line (git tag -l 'v*').
#
# Usage: git tag -l 'v*' | scripts/next-version.sh CHANNEL BUMP [VERSION]
#
#   CHANNEL  stable, beta or alpha
#   BUMP     patch, minor or major: which part to raise when a new version
#            starts (ignored while a version is in alpha or beta, or with VERSION)
#   VERSION  X.Y.Z to publish, instead of the computed one
#
# Rules, so that the order of the tags matches SemVer:
#   - A version starts from the latest stable release, raised by BUMP. While a
#     newer version has alphas or betas, it continues: 0.3.0-alpha.1,
#     0.3.0-alpha.2, 0.3.0-beta.1, ..., then the release 0.3.0.
#   - Pre-releases are numbered per channel: 0.3.0-beta.1, 0.3.0-beta.2.
#   - No alpha once the version has a beta (it would sort before the beta),
#     and no pre-release of a version already released.
#
# Set NEXT_VERSION_SOURCED=1 to only define the functions (for tests).

# semver_sort sorts versions in SemVer order, oldest first: a pre-release
# sorts before its release, and numbers compare as numbers.
semver_sort() {
	sed 's/-/~/' | sort -V | sed 's/~/-/'
}

# newer A B succeeds when version A is strictly newer than version B.
newer() {
	[ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | semver_sort | tail -n 1)" = "$1" ]
}

# is_core succeeds for X.Y.Z with no leading zeros.
is_core() {
	printf '%s\n' "$1" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
}

# bump VERSION PART raises PART (patch, minor, major) of X.Y.Z.
bump() {
	major=${1%%.*}
	rest=${1#*.}
	minor=${rest%%.*}
	patch=${rest#*.}
	case "$2" in
		patch) echo "$major.$minor.$((patch + 1))" ;;
		minor) echo "$major.$((minor + 1)).0" ;;
		major) echo "$((major + 1)).0.0" ;;
		*) return 1 ;;
	esac
}

# next_version CHANNEL BUMP [VERSION] reads tags and prints the next version.
next_version() {
	channel=$1
	part=$2
	wanted=${3:-}
	case "$channel" in stable | beta | alpha) ;; *)
		echo "error: channel must be stable, beta or alpha, not '$channel'" >&2
		return 1
		;;
	esac
	case "$part" in patch | minor | major) ;; *)
		echo "error: bump must be patch, minor or major, not '$part'" >&2
		return 1
		;;
	esac

	# Keep only tags this workflow understands: vX.Y.Z and vX.Y.Z-(alpha|beta|rc).N.
	versions=$(sed -n 's/^v//p' | grep -E '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.[1-9][0-9]*)?$' | semver_sort || true)
	stable=$(printf '%s\n' "$versions" | grep -Ev -e '-' | grep . | tail -n 1 || true)
	[ -n "$stable" ] || stable=0.0.0
	newest=$(printf '%s\n' "$versions" | grep . | tail -n 1 || true)

	if [ -n "$wanted" ]; then
		is_core "$wanted" || {
			echo "error: version must be X.Y.Z, such as 0.3.0, not '$wanted'" >&2
			return 1
		}
		core=$wanted
	elif [ -n "$newest" ] && [ "${newest%%-*}" != "$newest" ] && newer "${newest%%-*}" "$stable"; then
		core=${newest%%-*} # a version in alpha or beta continues
	else
		core=$(bump "$stable" "$part")
	fi

	if printf '%s\n' "$versions" | grep -qx "$core"; then
		echo "error: $core is already released; choose a newer version" >&2
		return 1
	fi
	newer "$core" "$stable" || {
		echo "error: $core is not newer than the latest release, $stable" >&2
		return 1
	}

	if [ "$channel" = stable ]; then
		echo "$core"
		return 0
	fi
	if [ "$channel" = alpha ] && printf '%s\n' "$versions" | grep -Eq "^$(printf '%s' "$core" | sed 's/\./\\./g')-(beta|rc)\."; then
		echo "error: $core already has a beta, and an alpha would sort before it; publish a beta, or a newer version with VERSION" >&2
		return 1
	fi
	last=$(printf '%s\n' "$versions" | sed -n "s/^$(printf '%s' "$core" | sed 's/\./\\./g')-$channel\.//p" | sort -n | tail -n 1)
	echo "$core-$channel.$((${last:-0} + 1))"
}

if [ "${NEXT_VERSION_SOURCED:-0}" != 1 ]; then
	set -eu
	[ $# -ge 2 ] && [ $# -le 3 ] || {
		sed -n '5,10s/^# \{0,1\}//p' "$0" >&2
		exit 2
	}
	next_version "$@"
fi
