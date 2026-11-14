#!/bin/sh
# Installs Regulus on Ubuntu Server (22.04 or newer) or another Debian-based
# system with systemd: it downloads the .deb of a release, checks its checksum
# and signature and installs it. As the hub (the default) it waits until the
# hub answers; as an agent (--role agent) it enrolls the server to a hub and
# starts the agent.
#
#   curl -fsSL https://github.com/Stellatinus/Regulus/releases/latest/download/install.sh | sh
#
# The address above serves the script of the latest stable release. Every
# release also carries its own copy, at .../releases/download/<tag>/install.sh.
#
# Options go after "sh -s --", for example:
#   ... | sh -s -- --no-builtin-agent
#   ... | sh -s -- --role agent --hub hub.example:8443 --token TOKEN --ca-fingerprint sha256:...
#
# Read the script before running it: it asks for root through sudo.

set -eu

REPO=Stellatinus/Regulus
DEFAULT_BASE_URL="https://github.com/$REPO/releases/download"
LATEST_URL="https://github.com/$REPO/releases/latest"
API_URL="https://api.github.com/repos/$REPO/releases"
CONFIG_DIR=/etc/regulus
DATA_DIR=/var/lib/regulus

usage() {
	cat <<'EOF'
Usage: install.sh [options]

  --role ROLE            hub (default) or agent: manage this server from a hub
  --hub HOST:PORT        agent role: the hub's agent listener (port 8443 if omitted)
  --token TOKEN          agent role: the one-time token made by the dashboard
                         (or set REGULUS_AGENT_TOKEN, to keep it off the command line)
  --ca-fingerprint FP    agent role: the hub's CA fingerprint, sha256:... as shown with
                         the token; the agent sends the token only to a hub that has it
  --version VERSION      install this version, such as 0.2.0 or 0.3.0-beta.1
                         (default: the latest release of the channel)
  --channel CHANNEL      stable (default), beta or alpha: stable offers releases only,
                         beta also betas and release candidates, alpha every release
  --beta, --alpha        the same as --channel beta, --channel alpha
  --builtin-agent        include the built-in agent (manage the hub's own server)
  --no-builtin-agent     run only the hub
  --no-self-update       do not set up installing updates from the dashboard or, for an
                         agent, from the hub (by default a systemd unit is set up that
                         installs, as root, only updates whose signature it checks again)
  --database TYPE        hub role: sqlite (default, nothing to set up) or postgres, a
                         PostgreSQL server you prepared (see docs/database.md). It is
                         chosen here and kept by later runs; moving data to the other one
                         is a separate feature
  --db-host HOST         PostgreSQL: host name or address (required with postgres)
  --db-port PORT         PostgreSQL: port (default 5432)
  --db-name NAME         PostgreSQL: database name (default regulus)
  --db-user USER         PostgreSQL: user (default regulus)
  --db-local             hub role: install the distribution's PostgreSQL here if it is not
                         there, create the role and the database with a generated password
                         (written only to /etc/regulus/regulus.env) and enable citext, then
                         check it like any PostgreSQL. Implies --database postgres. It uses
                         127.0.0.1 and leaves the other databases of the server alone, and
                         refuses when the role or the database already exists
  --db-reuse             with --db-local: the role and the database already exist, use them
                         (give the password with --database-password-file or
                         REGULUS_DATABASE_PASSWORD; it is not generated or changed)
  --db-sslmode MODE      PostgreSQL: disable, allow, prefer (default), require,
                         verify-ca or verify-full
  --db-sslrootcert FILE  PostgreSQL: CA file, for verify-ca and verify-full
  --database-password-file FILE
                         PostgreSQL: file whose first line is the password. The password is
                         never an option: it would show in the process list and the shell
                         history. Without the file the installer uses REGULUS_DATABASE_PASSWORD
                         from the environment, or asks on the terminal without echo. It is
                         written only to /etc/regulus/regulus.env (0640, root:regulus)
  --yes, -y              do not ask anything: the defaults are the built-in agent and SQLite
  --require-signature    fail if the signature cannot be verified (needs cosign)
  --base-url URL         download from URL/v<version>/ instead of GitHub Releases
  -h, --help             show this help

Without --builtin-agent or --no-builtin-agent, or without --database, the
installer asks, when it has a terminal to ask on; without one, or with --yes,
the answers are the built-in agent and SQLite. The agent role never asks
anything and has no database.

With --database postgres the installer connects to a PostgreSQL that already
exists; it does not install PostgreSQL or create the role and the database.
With --db-local it installs PostgreSQL here and creates them. Either way it
checks the connection (regulus db check) before it installs Regulus.
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

# channel_of VERSION prints the channel of a version, as the hub decides it:
# stable for a release, beta for -beta and -rc pre-releases, alpha for any
# other pre-release.
channel_of() {
	case "$1" in
		*-*) ;;
		*)
			echo stable
			return
			;;
	esac
	pre=${1#*-}
	case "$(printf '%s' "${pre%%.*}" | tr '[:upper:]' '[:lower:]')" in
		beta | rc) echo beta ;;
		*) echo alpha ;;
	esac
}

# offers CHANNEL VERSION succeeds when CHANNEL offers VERSION: stable only
# releases, beta releases and betas, alpha everything.
offers() {
	case "$1:$(channel_of "$2")" in
		stable:stable | beta:stable | beta:beta | alpha:*) return 0 ;;
	esac
	return 1
}

# newest_on_channel CHANNEL reads tags (v1.2.3, one per line) and prints the
# newest version CHANNEL offers, without the leading v, in SemVer order
# (a pre-release sorts before its release).
newest_on_channel() {
	while IFS= read -r t; do
		v=${t#v}
		valid_version "$v" && offers "$1" "$v" && printf '%s\n' "$v"
	done | sed 's/-/~/' | sort -V | sed 's/~/-/' | tail -n 1
}

# latest_version prints the newest version on $channel (without the leading v).
latest_version() {
	if [ "$channel" != stable ]; then
		have curl || die "curl is needed to look up the latest $channel release"
		tag=$(curl -fsSL "$API_URL?per_page=100" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | newest_on_channel "$channel")
	else
		have curl || die "curl is needed to look up the latest release"
		# /releases/latest redirects to /releases/tag/<tag>.
		tag=$(curl -fsSIL -o /dev/null -w '%{url_effective}' "$LATEST_URL" | sed -n 's|.*/tag/||p')
	fi
	[ -n "$tag" ] || die "could not find a $channel release; use --version, or --channel beta or alpha if only pre-releases exist"
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

# normalize_hub prints host:port for the hub address given by the user: it
# accepts a URL form (https://hub.example:8443/) and adds the default port.
normalize_hub() {
	addr=${1#https://}
	addr=${addr#http://}
	addr=${addr%%/*}
	[ -n "$addr" ] || return 1
	case "$addr" in
		*[!]0-9A-Za-z.:_[-]*) return 1 ;;
	esac
	case "$addr" in
		\[*\]) addr="$addr:8443" ;;
		\[*\]:*) ;;
		\[*) return 1 ;;
		*:*:*) return 1 ;; # an IPv6 address needs brackets
		*:*) ;;
		*) addr="$addr:8443" ;;
	esac
	port=${addr##*:}
	case "$port" in
		"" | *[!0-9]*) return 1 ;;
	esac
	[ "$port" -ge 1 ] && [ "$port" -le 65535 ] || return 1
	[ "${addr%:*}" != "" ] || return 1
	echo "$addr"
}

# valid_token accepts what the hub makes: base64url text.
valid_token() {
	case "$1" in
		*[!0-9A-Za-z_-]* | "") return 1 ;;
	esac
}

# valid_fingerprint accepts sha256: followed by 64 hex digits.
valid_fingerprint() {
	case "$1" in
		sha256:*) ;;
		*) return 1 ;;
	esac
	hex=${1#sha256:}
	[ "${#hex}" -eq 64 ] || return 1
	case "$hex" in
		*[!0-9a-fA-F]*) return 1 ;;
	esac
}

# check_agent_args validates the options of the agent role and normalizes
# $hub, so a typo is caught before anything is downloaded.
check_agent_args() {
	[ -n "$hub" ] || die "--hub is needed for the agent role: the address of the hub's agent listener, such as hub.example:8443"
	hub=$(normalize_hub "$hub") || die "invalid --hub: use HOST:PORT, such as hub.example:8443"
	[ -n "$token" ] || die "--token is needed for the agent role: make one in the dashboard, under Servers > Add a server"
	valid_token "$token" || die "invalid --token: it has characters a token never has"
	[ -n "$ca_fingerprint" ] || die "--ca-fingerprint is needed for the agent role: the dashboard shows it with the token. It lets this server check that it is talking to your hub before it sends the token"
	valid_fingerprint "$ca_fingerprint" || die "invalid --ca-fingerprint: use sha256: followed by 64 hex digits"
}

# set_role records the role where the package scripts read it.
set_role() {
	if [ "$role" = agent ]; then
		$SUDO mkdir -p "$CONFIG_DIR"
		printf 'agent\n' | $SUDO tee "$CONFIG_DIR/role" >/dev/null
	elif [ -f "$CONFIG_DIR/role" ]; then
		$SUDO rm -f "$CONFIG_DIR/role"
	fi
}

# as_service_user runs a command as the user the services run as.
as_service_user() {
	if [ "$(id -u)" -eq 0 ]; then
		if have runuser; then
			runuser -u regulus -- "$@"
		else
			su -s /bin/sh regulus -c "$(printf '%s ' "$@")"
		fi
	else
		sudo -u regulus "$@"
	fi
}

# setup_agent enrolls this server to the hub and starts the agent service.
setup_agent() {
	tmp_conf=$(mktemp)
	printf 'hub: %s\ndata_dir: /var/lib/regulus-agent\n' "$hub" >"$tmp_conf"
	[ "$self_update" != 1 ] || printf 'updates: true\n' >>"$tmp_conf"
	$SUDO install -m 0640 -o root -g regulus "$tmp_conf" "$CONFIG_DIR/agent.yaml"
	rm -f "$tmp_conf"

	say "Enrolling with the hub at $hub..."
	if ! as_service_user /usr/bin/regulus agent --config "$CONFIG_DIR/agent.yaml" \
		--token "$token" --ca-fingerprint "$ca_fingerprint" --enroll-only; then
		die "enrollment failed. Check the address, the fingerprint and that the token is unused and under an hour old; tokens work once, so make a new one in the dashboard and run this again"
	fi

	if [ -d /run/systemd/system ]; then
		$SUDO systemctl enable --now regulus-agent.service || die "the agent service could not be started; see: journalctl -u regulus-agent"
		i=0
		while [ "$i" -lt 10 ]; do
			systemctl is-active --quiet regulus-agent.service && break
			i=$((i + 1))
			sleep 1
		done
	else
		warn "systemd is not running: start the agent with: regulus agent --config $CONFIG_DIR/agent.yaml"
	fi
}

# report_agent tells the user how to check the result.
report_agent() {
	say ""
	if [ -d /run/systemd/system ] && systemctl is-active --quiet regulus-agent.service; then
		say "Regulus agent $version is installed, enrolled to $hub and running."
		say "It shows as online under Servers in the dashboard within a few seconds."
	else
		warn "the agent is not running; see: journalctl -u regulus-agent"
	fi
	say "Logs: journalctl -u regulus-agent"
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

# quiet_trace turns off `set -x`, so a password is never printed by it.
quiet_trace() {
	case $- in
		*x*) set +x ;;
	esac
}

# can_ask succeeds when the user can be asked: there is a terminal and --yes
# was not given. The subshell keeps a missing terminal from ending the script.
can_ask() {
	[ "$assume_yes" != 1 ] && ( : </dev/tty ) 2>/dev/null
}

# ask_value VAR LABEL DEFAULT sets VAR to the answer when it is empty.
ask_value() {
	eval "cur=\${$1}"
	[ -z "$cur" ] || return 0
	if [ -n "$3" ]; then
		printf '%s [%s]: ' "$2" "$3" >/dev/tty
	else
		printf '%s: ' "$2" >/dev/tty
	fi
	read -r ans </dev/tty || ans=
	[ -n "$ans" ] || ans=$3
	eval "$1=\$ans"
}

# valid_db_host accepts a host name, an IPv4 address or an IPv6 address.
valid_db_host() {
	case "$1" in
		"" | *[!0-9A-Za-z.:_-]*) return 1 ;;
		-* | .*) return 1 ;;
	esac
}

# valid_db_port accepts 1 to 65535.
valid_db_port() {
	case "$1" in
		"" | *[!0-9]*) return 1 ;;
	esac
	[ "${#1}" -le 5 ] && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}

# valid_db_ident accepts a database or role name that is safe in the
# environment file: letters, digits, underscore, dot, dollar and hyphen.
valid_db_ident() {
	case "$1" in
		"" | *[!-0-9A-Za-z_.$]*) return 1 ;;
		-*) return 1 ;;
	esac
}

# valid_db_sslmode accepts the libpq modes.
valid_db_sslmode() {
	case "$1" in
		disable | allow | prefer | require | verify-ca | verify-full) return 0 ;;
	esac
	return 1
}

# valid_db_path accepts an absolute path without characters that need quoting.
valid_db_path() {
	case "$1" in
		/*) ;;
		*) return 1 ;;
	esac
	case "$1" in
		*[!0-9A-Za-z_./-]*) return 1 ;;
	esac
}

# check_database_args validates the database options of the hub role, so a typo
# is caught before anything is downloaded.
check_database_args() {
	db_options=$db_host$db_port$db_name$db_user$db_sslmode$db_sslrootcert$db_password_file
	if [ "$role" = agent ]; then
		[ -z "$database$db_options" ] && [ "$db_local" = 0 ] && [ "$db_reuse" = 0 ] || die "--database, --db-local and the --db-* options are for the hub role: the agent has no database"
		return 0
	fi
	if [ "$db_local" = 1 ]; then
		[ -n "$database" ] || database=postgres
		[ "$database" = postgres ] || die "--db-local is for --database postgres"
		[ -z "$db_host" ] || die "--db-local uses PostgreSQL on this server (127.0.0.1): do not give --db-host; to use another server, leave out --db-local"
		[ -z "$db_sslrootcert" ] || die "--db-local connects to this server: --db-sslrootcert is for a remote PostgreSQL"
		if [ "$db_reuse" != 1 ] && [ -n "$db_password_file" ]; then
			die "--db-local generates the password: --database-password-file is only used with --db-reuse"
		fi
	else
		[ "$db_reuse" = 0 ] || die "--db-reuse is for --db-local"
	fi
	case "$database" in
		"" | sqlite | postgres) ;;
		*) die "invalid --database $database: use sqlite or postgres" ;;
	esac
	if [ "$database" = sqlite ] && [ -n "$db_options" ]; then
		die "the --db-* options and --database-password-file are for --database postgres"
	fi
	if [ -z "$database" ] && [ -n "$db_options" ]; then
		die "the --db-* options and --database-password-file need --database postgres"
	fi
	[ -z "$db_host" ] || valid_db_host "$db_host" || die "invalid --db-host: use a host name or an IP address"
	[ -z "$db_port" ] || valid_db_port "$db_port" || die "invalid --db-port: use a number from 1 to 65535"
	[ -z "$db_name" ] || valid_db_ident "$db_name" || die "invalid --db-name: use letters, digits and _ . \$ - only"
	[ -z "$db_user" ] || valid_db_ident "$db_user" || die "invalid --db-user: use letters, digits and _ . \$ - only"
	[ -z "$db_sslmode" ] || valid_db_sslmode "$db_sslmode" || die "invalid --db-sslmode $db_sslmode: use disable, allow, prefer, require, verify-ca or verify-full"
	[ -z "$db_sslrootcert" ] || valid_db_path "$db_sslrootcert" || die "invalid --db-sslrootcert: use the absolute path of the CA file"
	[ -z "$db_password_file" ] || [ -f "$db_password_file" ] || die "--database-password-file: $db_password_file is not a file"
	[ -z "$db_password_file" ] || [ -r "$db_password_file" ] || die "--database-password-file: cannot read $db_password_file"
}

# env_file_value KEY prints the value KEY has in the service's environment
# file, without quotes. The file is readable by root and the regulus group.
env_file_value() {
	[ -f "$CONFIG_DIR/regulus.env" ] || return 0
	$SUDO cat "$CONFIG_DIR/regulus.env" 2>/dev/null | sed -n "s/^$1=//p" | tail -n 1 | sed 's/^"\(.*\)"$/\1/'
}

# existing_backend prints the database an earlier installation of the hub
# uses (sqlite or postgres), or none when there is no hub installed here.
existing_backend() {
	if [ -r "$CONFIG_DIR/role" ] && [ "$(head -n 1 "$CONFIG_DIR/role")" = agent ]; then
		echo none
		return 0
	fi
	found=0
	if have dpkg && dpkg -s regulus 2>/dev/null | grep -q '^Status: install ok installed'; then
		found=1
	elif $SUDO test -e "$DATA_DIR/regulus.db"; then
		found=1
	elif [ -f "$CONFIG_DIR/regulus.env" ] && [ -n "$(env_file_value REGULUS_DATABASE_TYPE)" ]; then
		found=1
	fi
	if [ "$found" = 0 ]; then
		echo none
		return 0
	fi
	current=$(env_file_value REGULUS_DATABASE_TYPE)
	if [ -z "$current" ] && [ -r "$CONFIG_DIR/config.yaml" ]; then
		current=$(sed -n '/^database:/,/^[^ #]/s/^[ ]*type: *"\{0,1\}\([a-z]*\).*/\1/p' "$CONFIG_DIR/config.yaml" | head -n 1)
	fi
	case "$current" in
		postgres) echo postgres ;;
		*) echo sqlite ;;
	esac
}

# ask_database asks which database to use, and the PostgreSQL connection data
# that was not given as options, when the user can be asked.
ask_database() {
	[ "$db_local" != 1 ] || return 0
	if [ -z "$database" ]; then
		can_ask || return 0
		printf 'Database: SQLite (default, nothing to set up) or PostgreSQL that you already prepared? [S/p] ' >/dev/tty
		read -r answer </dev/tty || answer=
		case "$answer" in
			[pP]*) database=postgres ;;
			*) database=sqlite ;;
		esac
	fi
	[ "$database" = postgres ] || return 0
	if can_ask; then
		ask_value db_host "PostgreSQL host" ""
		ask_value db_port "PostgreSQL port" 5432
		ask_value db_name "Database name" regulus
		ask_value db_user "User" regulus
		ask_value db_sslmode "TLS mode (disable, allow, prefer, require, verify-ca, verify-full)" prefer
	fi
}

# read_password_file FILE prints the first line of FILE.
read_password_file() {
	quiet_trace
	IFS= read -r pw <"$1" || true
	pw=${pw%"$(printf '\r')"}
	printf '%s' "$pw"
}

# load_database_password sets $db_password from the file, the environment or
# the terminal. It never takes it from a command-line argument.
load_database_password() {
	quiet_trace
	db_password=
	if [ -n "$db_password_file" ]; then
		mode=$(stat -c %a "$db_password_file" 2>/dev/null || true)
		case "$mode" in
			*[1-7]) warn "$db_password_file can be read by other users: restrict it (chmod 600)" ;;
		esac
		db_password=$(read_password_file "$db_password_file")
		[ -n "$db_password" ] || die "--database-password-file: $db_password_file is empty"
	elif [ -n "${REGULUS_DATABASE_PASSWORD:-}" ]; then
		db_password=$REGULUS_DATABASE_PASSWORD
		# Not for apt and the package scripts to inherit.
		unset REGULUS_DATABASE_PASSWORD
	elif [ "$db_keep" != 1 ] && can_ask; then
		db_password=$(
			trap 'stty echo </dev/tty 2>/dev/null' EXIT INT TERM
			printf 'Password of %s on %s (not shown): ' "$db_user" "$db_host" >/dev/tty
			stty -echo </dev/tty
			IFS= read -r pw </dev/tty || pw=
			printf '%s' "$pw"
		)
		printf '\n' >/dev/tty
	fi
	case "$db_password" in
		*[![:print:]]*) die "the database password has a character that cannot be stored (control characters and line breaks are not allowed)" ;;
	esac
	if [ -z "$db_password" ] && [ "$db_keep" != 1 ] && [ "$existing" = none ]; then
		die "the PostgreSQL password is needed: use --database-password-file FILE, set REGULUS_DATABASE_PASSWORD, or run this on a terminal to be asked"
	fi
}

# prepare_database decides the database of this installation before anything
# is downloaded: it asks what was not given, fills the defaults, and refuses to
# change the backend of an installation that already has data.
prepare_database() {
	db_keep=0
	existing=$(existing_backend)
	if [ "$existing" != none ]; then
		if [ -z "$database" ]; then
			database=$existing
		elif [ "$database" != "$existing" ]; then
			die "this server already uses $existing, and --database $database would change it. Nothing was changed. Moving the data to another database is a separate feature (#124), not something a second run of the installer does. Run the installer without --database to keep $existing"
		fi
		if [ "$database" = postgres ] && [ -z "$db_options" ]; then
			db_keep=1
		fi
	else
		ask_database
		[ -n "$database" ] || database=sqlite
	fi
	[ "$database" = postgres ] || return 0

	# --db-local on a server that already has an installation is a re-run: the
	# installation keeps the database it has; nothing is created again.
	db_local_new=0
	if [ "$db_local" = 1 ]; then
		if [ "$existing" = none ]; then
			db_local_new=1
			db_host=127.0.0.1
		else
			say "This server already has a Regulus installation with PostgreSQL: --db-local is ignored, and the database it uses is kept."
		fi
	fi

	if [ "$db_keep" != 1 ]; then
		# Defaults only for a new installation. Changing an existing one
		# touches only what was given.
		if [ "$existing" = none ]; then
			[ -n "$db_port" ] || db_port=5432
			[ -n "$db_name" ] || db_name=regulus
			[ -n "$db_user" ] || db_user=regulus
			[ -n "$db_sslmode" ] || db_sslmode=prefer
			[ -n "$db_host" ] || die "--db-host is needed with --database postgres: the host of the PostgreSQL server you prepared"
		fi
		[ -z "$db_host" ] || valid_db_host "$db_host" || die "invalid PostgreSQL host"
		[ -z "$db_port" ] || valid_db_port "$db_port" || die "invalid PostgreSQL port: use a number from 1 to 65535"
		[ -z "$db_name" ] || valid_db_ident "$db_name" || die "invalid database name: use letters, digits and _ . \$ - only"
		[ -z "$db_user" ] || valid_db_ident "$db_user" || die "invalid user: use letters, digits and _ . \$ - only"
		[ -z "$db_sslmode" ] || valid_db_sslmode "$db_sslmode" || die "invalid TLS mode $db_sslmode"
		case "$db_sslmode" in
			verify-ca | verify-full)
				[ -n "$db_sslrootcert" ] || warn "$db_sslmode without --db-sslrootcert uses the system's CA list"
				;;
		esac
	fi
	# A password that is generated is not asked for.
	if [ "$db_local_new" = 1 ] && [ "$db_reuse" != 1 ]; then
		return 0
	fi
	load_database_password
}

# quote_env_value prints VALUE for a systemd environment file: in double
# quotes, with backslash and double quote escaped.
quote_env_value() {
	quiet_trace
	printf '"%s"' "$(printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g')"
}

# precheck_database tries the PostgreSQL settings with the new binary taken
# out of the verified package, before anything is installed or written. The
# password goes in the environment of that one process, never in its arguments.
precheck_database() {
	[ "$database" = postgres ] || return 0
	[ "$db_keep" != 1 ] || return 0
	[ -n "$db_password" ] || return 0 # an earlier password stays: checked after the install
	have dpkg-deb || die "dpkg-deb not found"
	mkdir -p "$work/pkg"
	dpkg-deb -x "$work/$file" "$work/pkg" || die "could not read $file"
	bin=$work/pkg/usr/bin/regulus
	[ -x "$bin" ] || die "$file has no /usr/bin/regulus"
	"$bin" db -h >/dev/null 2>&1 || die "Regulus $version cannot use PostgreSQL: choose a release that has it, or use --database sqlite"
	say "Checking the PostgreSQL connection..."
	quiet_trace
	if ! out=$(
		REGULUS_DATABASE_TYPE=postgres REGULUS_DATABASE_HOST=$db_host REGULUS_DATABASE_PORT=$db_port \
			REGULUS_DATABASE_NAME=$db_name REGULUS_DATABASE_USER=$db_user REGULUS_DATABASE_SSLMODE=$db_sslmode \
			REGULUS_DATABASE_SSLROOTCERT=$db_sslrootcert REGULUS_DATABASE_PASSWORD=$db_password \
			"$bin" db check --env-file /dev/null 2>&1
	); then
		printf '%s\n' "$out" >&2
		if [ "$db_local_new" = 1 ]; then
			die "PostgreSQL and the database were set up and the settings saved in $CONFIG_DIR/regulus.env, but Regulus is not installed yet. Fix the cause above (see docs/database.md), then run the installer again: it keeps these settings and does not create anything twice"
		fi
		die "the database cannot be used with these settings, so nothing was installed or changed. Fix the cause above (see docs/database.md for what to prepare) and run the installer again"
	fi
	say "$out"
}

# The small wrappers below are what --db-local does to the system; the tests
# replace them.

# as_postgres runs a command as the PostgreSQL system user.
as_postgres() {
	if [ "$(id -u)" -eq 0 ]; then
		if have runuser; then
			runuser -u postgres -- "$@"
		else
			su -s /bin/sh postgres -c "$(printf '%s ' "$@")"
		fi
	else
		sudo -u postgres "$@"
	fi
}

# pg_sql [DATABASE] runs the SQL on its standard input as the postgres user,
# over the local socket. SQL goes through the standard input so a password is
# never in a process list.
pg_sql() {
	as_postgres psql -X -q -t -A -v ON_ERROR_STOP=1 -p "$db_port" -d "${1:-postgres}"
}

# pg_install installs the distribution's PostgreSQL.
pg_install() {
	$SUDO env DEBIAN_FRONTEND=noninteractive apt-get update -qq || return 1
	$SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql
}

# pg_ready succeeds when PostgreSQL answers on $db_port.
pg_ready() {
	have pg_isready && pg_isready -q -p "$db_port"
}

# pg_start asks systemd to start PostgreSQL.
pg_start() {
	$SUDO systemctl start postgresql
}

# pg_wait_ready waits up to 30 seconds for PostgreSQL, starting it once.
pg_wait_ready() {
	i=0
	started=0
	while [ "$i" -lt 30 ]; do
		pg_ready && return 0
		if [ "$started" = 0 ]; then
			started=1
			pg_start >/dev/null 2>&1 || true
		fi
		i=$((i + 1))
		sleep 1
	done
	return 1
}

# generate_db_password prints 32 random letters and digits: nothing in it needs
# quoting in SQL or in the environment file.
generate_db_password() {
	quiet_trace
	LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32
}

# setup_local_database is --db-local: it installs PostgreSQL if it is missing
# and creates the role and the database for Regulus with a generated password
# (kept in $db_password, written later to the environment file only). It
# creates only these, so other databases of the server are not touched, and it
# refuses when they already exist unless --db-reuse was given.
setup_local_database() {
	quiet_trace
	say "Setting up PostgreSQL on this server..."
	if ! have psql || ! have pg_isready; then
		say "Installing PostgreSQL from the distribution..."
		pg_install || die "PostgreSQL could not be installed with apt. Nothing else was changed"
	fi
	pg_wait_ready || die "PostgreSQL does not answer on port $db_port. Check it with: systemctl status postgresql (nothing of Regulus was installed)"

	role_found=$(printf "SELECT 1 FROM pg_roles WHERE rolname = '%s';\n" "$db_user" | pg_sql) || die "could not ask PostgreSQL about the role $db_user (is it running on port $db_port, and does the postgres user have access over the local socket?)"
	db_found=$(printf "SELECT 1 FROM pg_database WHERE datname = '%s';\n" "$db_name" | pg_sql) || die "could not ask PostgreSQL about the database $db_name"

	if [ "$db_reuse" = 1 ]; then
		[ -n "$role_found" ] || die "--db-reuse: the PostgreSQL role $db_user does not exist"
		[ -n "$db_found" ] || die "--db-reuse: the database $db_name does not exist"
		say "Using the existing role $db_user and database $db_name."
	else
		[ -z "$role_found" ] || die "the PostgreSQL role $db_user already exists, and this installer does not change an existing role. Choose another with --db-user, or use --db-reuse with its password. Nothing was changed"
		[ -z "$db_found" ] || die "the PostgreSQL database $db_name already exists, and this installer does not change an existing database. Choose another with --db-name, or use --db-reuse. Nothing was changed"
		db_password=$(generate_db_password)
		[ "${#db_password}" -eq 32 ] || die "could not generate a password (is /dev/urandom readable?)"
		{
			printf "CREATE ROLE \"%s\" LOGIN PASSWORD '%s';\n" "$db_user" "$db_password"
			printf 'CREATE DATABASE "%s" OWNER "%s";\n' "$db_name" "$db_user"
		} | pg_sql || die "could not create the role and the database"
		say "Created the role $db_user and the database $db_name."
	fi
	printf 'CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public;\n' | pg_sql "$db_name" || die "could not enable the citext extension in $db_name"
}

# check_database_installed runs the check again with the installed binary and
# the settings the service reads, and says what to do when it fails.
check_database_installed() {
	[ "$database" = postgres ] || return 0
	if out=$($SUDO /usr/bin/regulus db check 2>&1); then
		printf '%s\n' "$out" | sed -n 's/^Database: /  Database:    /p'
		return 0
	fi
	printf '%s\n' "$out" >&2
	say "" >&2
	die "the hub is installed but cannot use its PostgreSQL database. Fix the cause above (settings are in $CONFIG_DIR/regulus.env), check with 'sudo regulus db check', then run 'sudo systemctl restart regulus'"
}

# set_env_value stores KEY=VALUE in the environment file the service reads,
# replacing an earlier value of KEY. Run it once per key, with $tmp_env set up
# by write_settings.
set_env_value() {
	grep -v "^$1=" "$tmp_env" >"$tmp_env.new" || true
	printf '%s=%s\n' "$1" "$2" >>"$tmp_env.new"
	mv "$tmp_env.new" "$tmp_env"
}

# write_settings stores the answers where the service reads them. The built-in
# agent answer is left out when it is the default, so config.yaml stays in
# charge; the update helper is switched on unless it was declined.
write_settings() {
	write_db=0
	[ "$database" = postgres ] && [ "$db_keep" != 1 ] && write_db=1
	[ "$builtin_agent" = default ] && [ "$self_update" != 1 ] && [ "$write_db" = 0 ] && return 0
	$SUDO mkdir -p "$CONFIG_DIR"
	tmp_env=$(mktemp)
	if [ -f "$CONFIG_DIR/regulus.env" ]; then
		$SUDO cat "$CONFIG_DIR/regulus.env" >"$tmp_env"
	fi
	[ "$builtin_agent" = default ] || set_env_value REGULUS_BUILTIN_AGENT "$builtin_agent"
	[ "$self_update" != 1 ] || set_env_value REGULUS_UPDATES_APPLY true
	[ "$write_db" = 0 ] || write_database_settings
	$SUDO install -m 0640 "$tmp_env" "$CONFIG_DIR/regulus.env"
	rm -f "$tmp_env" "$tmp_env.new"
}

# write_database_settings stores the PostgreSQL choice in $tmp_env. The
# password is quoted and goes into this file only, never into config.yaml.
write_database_settings() {
	quiet_trace
	set_env_value REGULUS_DATABASE_TYPE postgres
	[ -z "$db_host" ] || set_env_value REGULUS_DATABASE_HOST "$db_host"
	[ -z "$db_port" ] || set_env_value REGULUS_DATABASE_PORT "$db_port"
	[ -z "$db_name" ] || set_env_value REGULUS_DATABASE_NAME "$db_name"
	[ -z "$db_user" ] || set_env_value REGULUS_DATABASE_USER "$db_user"
	[ -z "$db_sslmode" ] || set_env_value REGULUS_DATABASE_SSLMODE "$db_sslmode"
	[ -z "$db_sslrootcert" ] || set_env_value REGULUS_DATABASE_SSLROOTCERT "$db_sslrootcert"
	if [ -n "$db_password" ]; then
		set_env_value REGULUS_DATABASE_PASSWORD "$(quote_env_value "$db_password")"
	fi
}

# enable_self_update starts watching for the update requests of the hub. The
# unit files come with the package.
enable_self_update() {
	[ "$self_update" = 1 ] || return 0
	[ -d /run/systemd/system ] || return 0
	unit=regulus-update.path
	[ "$role" != agent ] || unit=regulus-agent-update.path
	$SUDO systemctl enable --now "$unit" || warn "could not enable $unit: updates from the hub will not install until it is enabled"
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
	role=hub
	hub=
	token=
	ca_fingerprint=
	database=
	db_host=
	db_port=
	db_name=
	db_user=
	db_sslmode=
	db_sslrootcert=
	db_password_file=
	db_password=
	db_keep=0
	db_local=0
	db_reuse=0
	db_local_new=0
	assume_yes=0
	self_update=1
	require_signature=${REGULUS_REQUIRE_SIGNATURE:-0}
	base_url=$DEFAULT_BASE_URL

	while [ $# -gt 0 ]; do
		case "$1" in
			--version)
				[ $# -ge 2 ] || die "--version needs a value"
				version=${2#v}
				shift
				;;
			--role)
				[ $# -ge 2 ] || die "--role needs a value"
				role=$2
				shift
				;;
			--hub)
				[ $# -ge 2 ] || die "--hub needs a value"
				hub=$2
				shift
				;;
			--token)
				[ $# -ge 2 ] || die "--token needs a value"
				token=$2
				shift
				;;
			--ca-fingerprint)
				[ $# -ge 2 ] || die "--ca-fingerprint needs a value"
				ca_fingerprint=$2
				shift
				;;
			--channel)
				[ $# -ge 2 ] || die "--channel needs a value"
				channel=$2
				shift
				;;
			--beta) channel=beta ;;
			--alpha) channel=alpha ;;
			--database)
				[ $# -ge 2 ] || die "--database needs a value"
				database=$2
				shift
				;;
			--db-host)
				[ $# -ge 2 ] || die "--db-host needs a value"
				db_host=$2
				shift
				;;
			--db-port)
				[ $# -ge 2 ] || die "--db-port needs a value"
				db_port=$2
				shift
				;;
			--db-name)
				[ $# -ge 2 ] || die "--db-name needs a value"
				db_name=$2
				shift
				;;
			--db-user)
				[ $# -ge 2 ] || die "--db-user needs a value"
				db_user=$2
				shift
				;;
			--db-sslmode)
				[ $# -ge 2 ] || die "--db-sslmode needs a value"
				db_sslmode=$2
				shift
				;;
			--db-sslrootcert)
				[ $# -ge 2 ] || die "--db-sslrootcert needs a value"
				db_sslrootcert=$2
				shift
				;;
			--database-password-file)
				[ $# -ge 2 ] || die "--database-password-file needs a value"
				db_password_file=$2
				shift
				;;
			--db-local) db_local=1 ;;
			--db-reuse) db_reuse=1 ;;
			--builtin-agent) builtin_agent=true ;;
			--no-builtin-agent) builtin_agent=false ;;
			--no-self-update) self_update=0 ;;
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

	case "$channel" in
		stable | beta | alpha) ;;
		*) die "invalid --channel $channel: use stable, beta or alpha" ;;
	esac

	check_database_args

	case "$role" in
		hub)
			[ -z "$hub$token$ca_fingerprint" ] || die "--hub, --token and --ca-fingerprint are for --role agent"
			;;
		agent)
			[ -n "$token" ] || token=${REGULUS_AGENT_TOKEN:-}
			[ -z "$builtin_agent" ] || die "--builtin-agent and --no-builtin-agent are for the hub role"
			check_agent_args
			;;
		*) die "invalid --role $role: use hub or agent" ;;
	esac

	check_platform

	SUDO=
	if [ "$(id -u)" -ne 0 ]; then
		have sudo || die "run this as root, or install sudo"
		SUDO=sudo
	fi

	if [ "$role" = hub ]; then
		prepare_database
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

	if [ "$role" = hub ]; then
		ask_builtin_agent
		if [ "$db_local_new" = 1 ]; then
			# The password was just generated and exists nowhere else: keep it
			# before anything can fail, so a second run finds it.
			setup_local_database
			write_settings
			precheck_database
		else
			precheck_database
			write_settings
		fi
	fi
	set_role

	say "Installing..."
	# The package may be owned by the current user: apt (running as _apt)
	# needs to read it.
	chmod 0644 "$work/$file"
	chmod 0755 "$work"
	$SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y "$work/$file"

	if [ "$role" = agent ]; then
		setup_agent
		enable_self_update
		report_agent
	else
		enable_self_update
		check_database_installed
		report
	fi
}

# Tests source this file with REGULUS_INSTALL_SOURCED=1 to call the functions.
if [ "${REGULUS_INSTALL_SOURCED:-}" != 1 ]; then
	main "$@"
fi
