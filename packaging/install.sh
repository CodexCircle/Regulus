#!/bin/sh
# Installs the Regulus hub on Ubuntu Server (22.04 or newer) or another
# Debian-based system with systemd: it downloads the .deb of a release, checks
# its checksum and signature, installs it and waits until the hub answers.
#
#   curl -fsSL https://github.com/CodexCircle/Regulus/releases/latest/download/install.sh | sh
#
# Options go after "sh -s --", for example:
#   ... | sh -s -- --no-builtin-agent
#
# Read the script before running it: it asks for root through sudo.

set -eu

REPO=CodexCircle/Regulus
DEFAULT_BASE_URL="https://github.com/$REPO/releases/download"
LATEST_URL="https://github.com/$REPO/releases/latest"
API_URL="https://api.github.com/repos/$REPO/releases"
CONFIG_DIR=/etc/regulus

usage() {
	cat <<'EOF'
Usage: install.sh [options]

  --version VERSION      install this version, such as 0.2.0 or 0.3.0-beta.1
                         (default: the latest stable release)
  --beta                 install the latest pre-release (beta channel)
  --builtin-agent        include the built-in agent (manage the hub's own server)
  --no-builtin-agent     run only the hub
  --yes, -y              do not ask anything: the default is the built-in agent
  --require-signature    fail if the signature cannot be verified (needs cosign)
  --base-url URL         download from URL/v<version>/ instead of GitHub Releases
  -h, --help             show this help

Without --builtin-agent or --no-builtin-agent the installer asks, when it has
a terminal to ask on.
EOF
}

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

have() { command -v "$1" >/dev/null 2>&1; }

# detect_arch prints the architecture name used in the release files.
detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) echo amd64 ;;
		aarch64 | arm64) echo arm64 ;;
		*) return 1 ;;
	esac
}

# check_platform fails unless this is a Debian-based Linux with systemd.
check_platform() {
	[ "$(uname -s)" = Linux ] || die "this installer supports Linux only"
	arch=$(detect_arch) || die "unsupported CPU architecture $(uname -m) (amd64 and arm64 are supported)"
	have dpkg || die "dpkg not found: this installer is for Ubuntu and Debian. Download the tar.gz from https://github.com/$REPO/releases instead"
	have apt-get || die "apt-get not found"
	if [ -r /etc/os-release ]; then
		# shellcheck disable=SC1091
		. /etc/os-release
		case "${ID:-}${ID_LIKE:-}" in
			*ubuntu* | *debian*) ;;
			*) warn "${PRETTY_NAME:-this system} is not Ubuntu or Debian; trying anyway" ;;
		esac
	fi
	[ -d /run/systemd/system ] || warn "systemd is not running: the package will install but the service will not start"
}

# fetch URL FILE downloads a file, failing on HTTP errors.
fetch() {
	if have curl; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is needed"
	fi
}

# latest_version prints the newest version (without the leading v): the latest
# stable release, or with $channel=beta the newest release of any kind.
latest_version() {
	if [ "$channel" = beta ]; then
		have curl || die "curl is needed to look up the latest beta"
		tag=$(curl -fsSL "$API_URL?per_page=1" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	else
		have curl || die "curl is needed to look up the latest release"
		# /releases/latest redirects to /releases/tag/<tag>.
		tag=$(curl -fsSIL -o /dev/null -w '%{url_effective}' "$LATEST_URL" | sed -n 's|.*/tag/||p')
	fi
	[ -n "$tag" ] || die "could not find the latest release; use --version"
	echo "${tag#v}"
}

# valid_version accepts SemVer such as 1.2.3 or 1.2.3-beta.1.
valid_version() {
	case "$1" in
		*[!0-9A-Za-z.+-]* | "" | .* | -*) return 1 ;;
	esac
	case "$1" in
		[0-9]*.[0-9]*.[0-9]*) return 0 ;;
	esac
	return 1
}

# verify_checksum DIR FILE checks FILE against DIR/checksums.txt.
verify_checksum() {
	line=$(grep "  $2\$" "$1/checksums.txt" || true)
	[ -n "$line" ] || die "$2 is not listed in checksums.txt"
	have sha256sum || die "sha256sum is needed to verify the download"
	(cd "$1" && printf '%s\n' "$line" | sha256sum -c - >/dev/null 2>&1) || die "checksum of $2 does not match: the download is corrupted or was tampered with"
}

# verify_signature DIR checks checksums.txt against its Sigstore signature,
# made by the release workflow of this repository.
verify_signature() {
	if ! have cosign; then
		if [ "$require_signature" = 1 ]; then
			die "cosign is not installed, so the signature cannot be verified (--require-signature)"
		fi
		warn "cosign is not installed: only the checksum was verified, not the signature (https://docs.sigstore.dev/cosign/)"
		return 0
	fi
	if ! fetch "$base_url/v$version/checksums.txt.sigstore.json" "$1/checksums.txt.sigstore.json" 2>/dev/null; then
		[ "$require_signature" = 1 ] && die "the release has no signature (--require-signature)"
		warn "the release has no signature file: only the checksum was verified"
		return 0
	fi
	cosign verify-blob \
		--bundle "$1/checksums.txt.sigstore.json" \
		--certificate-identity-regexp "https://github.com/$REPO/.github/workflows/release.yml@.*" \
		--certificate-oidc-issuer https://token.actions.githubusercontent.com \
		"$1/checksums.txt" >/dev/null 2>&1 || die "the signature of checksums.txt is not valid"
	say "Signature verified."
}

# ask_builtin_agent sets $builtin_agent to true or false, asking on the
# terminal when the answer was not given and the user can be asked.
ask_builtin_agent() {
	[ -n "$builtin_agent" ] && return 0
	# The subshell keeps a missing terminal from ending the script.
	if [ "$assume_yes" = 1 ] || ! ( : </dev/tty ) 2>/dev/null; then
		builtin_agent=default
		return 0
	fi
	printf 'Include the built-in agent, so this server is managed like the others? [Y/n] ' >/dev/tty
	read -r answer </dev/tty || answer=
	case "$answer" in
		[nN]*) builtin_agent=false ;;
		*) builtin_agent=true ;;
	esac
}

# write_settings stores the answer where the service reads it. The file is
# left alone when the answer is the default, so config.yaml stays in charge.
write_settings() {
	[ "$builtin_agent" = default ] && return 0
	$SUDO mkdir -p "$CONFIG_DIR"
	tmp_env=$(mktemp)
	if [ -f "$CONFIG_DIR/regulus.env" ]; then
		grep -v '^REGULUS_BUILTIN_AGENT=' "$CONFIG_DIR/regulus.env" >"$tmp_env" || true
	fi
	printf 'REGULUS_BUILTIN_AGENT=%s\n' "$builtin_agent" >>"$tmp_env"
	$SUDO install -m 0640 "$tmp_env" "$CONFIG_DIR/regulus.env"
	rm -f "$tmp_env"
}

# listen_port prints the TCP port the hub listens on.
listen_port() {
	listen=${REGULUS_LISTEN:-}
	if [ -z "$listen" ] && [ -r "$CONFIG_DIR/config.yaml" ]; then
		listen=$(sed -n 's/^listen: *"\{0,1\}\([^" #]*\).*/\1/p' "$CONFIG_DIR/config.yaml" | head -n 1)
	fi
	echo "${listen##*:}"
}

# wait_for_hub polls /healthz until the hub answers or 30 seconds pass.
wait_for_hub() {
	port=$(listen_port)
	[ -n "$port" ] || port=8080
	have curl || return 0
	i=0
	while [ "$i" -lt 30 ]; do
		if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 1
	done
	return 1
}

# report tells the user how to continue.
report() {
	port=$(listen_port)
	[ -n "$port" ] || port=8080
	host=$(hostname -I 2>/dev/null | awk '{print $1}')
	say ""
	if wait_for_hub; then
		say "Regulus $version is installed and running."
	else
		warn "the hub did not answer within 30 seconds; see: journalctl -u regulus"
	fi
	say "  Dashboard:   http://${host:-<this server>}:$port"
	token=$($SUDO journalctl -u regulus -o cat --no-pager 2>/dev/null | sed -n 's/.*setup_token=\([^ ]*\).*/\1/p' | tail -n 1 || true)
	if [ -n "$token" ]; then
		say "  Setup token: $token"
		say "Open the dashboard and use the token to create the first admin account."
	else
		say "  Setup token: journalctl -u regulus -o cat | grep setup_token"
	fi
	say "The dashboard uses plain HTTP: put a reverse proxy with TLS in front of it before exposing it to the internet."
}

main() {
	version=
	channel=stable
	builtin_agent=
	assume_yes=0
	require_signature=${REGULUS_REQUIRE_SIGNATURE:-0}
	base_url=$DEFAULT_BASE_URL

	while [ $# -gt 0 ]; do
		case "$1" in
			--version)
				[ $# -ge 2 ] || die "--version needs a value"
				version=${2#v}
				shift
				;;
			--beta) channel=beta ;;
			--builtin-agent) builtin_agent=true ;;
			--no-builtin-agent) builtin_agent=false ;;
			--yes | -y) assume_yes=1 ;;
			--require-signature) require_signature=1 ;;
			--base-url)
				[ $# -ge 2 ] || die "--base-url needs a value"
				base_url=${2%/}
				shift
				;;
			-h | --help)
				usage
				exit 0
				;;
			*)
				usage >&2
				die "unknown option: $1"
				;;
		esac
		shift
	done

	check_platform

	SUDO=
	if [ "$(id -u)" -ne 0 ]; then
		have sudo || die "run this as root, or install sudo"
		SUDO=sudo
	fi

	if [ -z "$version" ]; then
		version=$(latest_version)
	fi
	valid_version "$version" || die "invalid version: $version"

	file=regulus_${version}_linux_${arch}.deb
	work=$(mktemp -d)
	trap 'rm -rf "$work"' EXIT INT TERM

	say "Downloading Regulus $version for $arch..."
	fetch "$base_url/v$version/checksums.txt" "$work/checksums.txt" || die "could not download checksums.txt of version $version (does the release exist?)"
	fetch "$base_url/v$version/$file" "$work/$file" || die "could not download $file"
	verify_checksum "$work" "$file"
	say "Checksum verified."
	verify_signature "$work"

	ask_builtin_agent
	write_settings

	say "Installing..."
	# The package may be owned by the current user: apt (running as _apt)
	# needs to read it.
	chmod 0644 "$work/$file"
	chmod 0755 "$work"
	$SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y "$work/$file"

	report
}

# Tests source this file with REGULUS_INSTALL_SOURCED=1 to call the functions.
if [ "${REGULUS_INSTALL_SOURCED:-}" != 1 ]; then
	main "$@"
fi
