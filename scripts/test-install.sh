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

# The database variables main() sets; set -u is on.
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
db_options=
existing=none
role=hub
assume_yes=1

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

# channels
equal "channel of a release" "$(channel_of 1.2.3)" stable
equal "channel of a beta" "$(channel_of 1.3.0-beta.2)" beta
equal "channel of a release candidate" "$(channel_of 1.3.0-RC.1)" beta
equal "channel of an alpha" "$(channel_of 1.4.0-alpha.1)" alpha
equal "channel of another pre-release" "$(channel_of 1.4.0-dev.5)" alpha
check_not "stable does not offer a beta" offers stable 1.3.0-beta.1
check "beta offers a release" offers beta 1.2.3
check_not "beta does not offer an alpha" offers beta 1.4.0-alpha.1
check "alpha offers a beta" offers alpha 1.3.0-beta.1
tags='v0.2.0
v0.3.0-beta.10
v0.3.0-beta.9
v0.4.0-alpha.1
v0.1.0
v0.3.0-beta.2
not-a-version'
equal "newest stable" "$(printf '%s\n' "$tags" | newest_on_channel stable)" 0.2.0
equal "newest beta (numeric order)" "$(printf '%s\n' "$tags" | newest_on_channel beta)" 0.3.0-beta.10
equal "newest alpha" "$(printf '%s\n' "$tags" | newest_on_channel alpha)" 0.4.0-alpha.1
equal "a release is newer than its beta" "$(printf 'v0.3.0-beta.10\nv0.3.0\n' | newest_on_channel beta)" 0.3.0
equal "a beta is newer than an alpha of the same version" "$(printf 'v0.3.0-beta.1\nv0.3.0-alpha.4\n' | newest_on_channel alpha)" 0.3.0-beta.1
equal "no release on the channel" "$(printf 'v0.3.0-alpha.1\n' | newest_on_channel stable)" ""
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

# ---------------------------------------------------------------- database

check "accepts a host name" valid_db_host db.example.com
check "accepts an IPv4 address" valid_db_host 10.0.0.5
check "accepts an IPv6 address" valid_db_host fd00::5
check_not "rejects an empty host" valid_db_host ""
check_not "rejects shell characters in a host" valid_db_host 'db;rm'
check_not "rejects a host that looks like an option" valid_db_host -x
check "accepts port 5432" valid_db_port 5432
check_not "rejects port 0" valid_db_port 0
check_not "rejects port 65536" valid_db_port 65536
check_not "rejects a port that is not a number" valid_db_port 54x2
check_not "rejects a very long port" valid_db_port 0000000005432
check "accepts a database name" valid_db_ident regulus_prod-1.a
check_not "rejects a name with a space" valid_db_ident 'a b'
check_not "rejects a name with a quote" valid_db_ident "a'b"
check_not "rejects a name that looks like an option" valid_db_ident -x
check "accepts verify-full" valid_db_sslmode verify-full
check_not "rejects an unknown sslmode" valid_db_sslmode sometimes
check "accepts an absolute CA path" valid_db_path /etc/ssl/db-ca.pem
check_not "rejects a relative CA path" valid_db_path ca.pem
check_not "rejects spaces in a CA path" valid_db_path '/etc/my ca.pem'

# check_database_args: role, database, host, port, sslmode, password file
args_case() {
	role=$1 database=$2 db_host=$3 db_port=$4 db_sslmode=$5 db_password_file=$6 db_name= db_user= db_sslrootcert=
	( check_database_args )
}
check "no database option is fine" args_case hub "" "" "" "" ""
check "sqlite alone is fine" args_case hub sqlite "" "" "" ""
check "postgres with a host is fine" args_case hub postgres db.example 5433 require ""
check_not "an unknown database is refused" args_case hub mysql "" "" "" ""
check_not "a PostgreSQL option with sqlite is refused" args_case hub sqlite db.example "" "" ""
check_not "a PostgreSQL option without --database is refused" args_case hub "" db.example "" "" ""
check_not "the agent role takes --database" args_case agent postgres "" "" "" ""
check_not "the agent role takes the PostgreSQL options" args_case agent "" db.example "" "" ""
check_not "a bad port is refused" args_case hub postgres db.example 99999 "" ""
check_not "a bad sslmode is refused" args_case hub postgres db.example "" maybe ""
check_not "a missing password file is refused" args_case hub postgres db.example "" "" "$tmp/no-such-file"
role=hub

# prepare_database on a new installation
# shellcheck disable=SC2329 # replaces the real one for the tests below
dpkg() { return 1; }
SUDO=
CONFIG_DIR=$tmp/db-etc
DATA_DIR=$tmp/db-var
mkdir -p "$CONFIG_DIR" "$DATA_DIR"
reset_db() {
	database= db_host= db_port= db_name= db_user= db_sslmode= db_sslrootcert= db_password_file= db_password= db_keep=0 db_options= existing=none
}
reset_db
assume_yes=1
prepare_database
equal "a new installation without answers uses SQLite" "$database" sqlite
equal "SQLite asks for nothing and stores nothing" "$db_password$db_host" ""

secret='p"a\ss $word#1'
printf '%s\n' "$secret" >"$tmp/pw"
chmod 600 "$tmp/pw"
reset_db
database=postgres db_host=db.example db_password_file=$tmp/pw db_options=x
prepare_database
equal "PostgreSQL gets the default port" "$db_port" 5432
equal "PostgreSQL gets the default database" "$db_name" regulus
equal "PostgreSQL gets the default user" "$db_user" regulus
equal "PostgreSQL gets the default TLS mode" "$db_sslmode" prefer
equal "the password comes from the file, first line" "$db_password" "$secret"

reset_db
database=postgres db_host=db.example db_options=x
(REGULUS_DATABASE_PASSWORD=from-env; prepare_database; [ "$db_password" = from-env ] && [ -z "${REGULUS_DATABASE_PASSWORD:-}" ]) >/dev/null 2>&1
equal "the password can come from the environment, and is then unset" "$?" 0
reset_db
database=postgres db_host=db.example db_options=x
check_not "without a password source and without a terminal it stops" in_subshell prepare_database
reset_db
database=postgres db_options=x db_password_file=$tmp/pw
check_not "PostgreSQL without a host stops" in_subshell prepare_database
: >"$tmp/empty"
reset_db
database=postgres db_host=db.example db_options=x db_password_file=$tmp/empty
check_not "an empty password file stops" in_subshell prepare_database
printf 'bad\001pw\n' >"$tmp/ctl"
reset_db
database=postgres db_host=db.example db_options=x db_password_file=$tmp/ctl
check_not "a password with a control character stops" in_subshell prepare_database

# the settings file
reset_db
database=postgres db_host=db.example db_port=5433 db_name=hub db_user=hubuser db_sslmode=verify-full db_sslrootcert=/etc/ssl/ca.pem db_password=$secret
builtin_agent=default
self_update=0
env_file=$CONFIG_DIR/regulus.env
rm -f "$env_file"
write_settings
equal "the environment file is 0640" "$(stat -c %a "$env_file")" 640
equal "the type is stored" "$(grep -c '^REGULUS_DATABASE_TYPE=postgres$' "$env_file")" 1
equal "the host is stored" "$(grep -c '^REGULUS_DATABASE_HOST=db.example$' "$env_file")" 1
equal "the user is stored" "$(grep -c '^REGULUS_DATABASE_USER=hubuser$' "$env_file")" 1
equal "the CA file is stored" "$(grep -c '^REGULUS_DATABASE_SSLROOTCERT=/etc/ssl/ca.pem$' "$env_file")" 1
equal "the password is quoted and escaped" "$(grep '^REGULUS_DATABASE_PASSWORD=' "$env_file")" 'REGULUS_DATABASE_PASSWORD="p\"a\\ss $word#1"'
check_not "config.yaml is not written" test -e "$CONFIG_DIR/config.yaml"
write_settings
equal "a second run does not repeat the password" "$(grep -c '^REGULUS_DATABASE_PASSWORD=' "$env_file")" 1

# an installation that exists keeps its database
rm -f "$env_file"
printf 'REGULUS_LOG_LEVEL=debug\nREGULUS_DATABASE_TYPE=postgres\nREGULUS_DATABASE_HOST=old.example\n' >"$env_file"
equal "an existing PostgreSQL installation is recognised" "$(existing_backend)" postgres
before=$(cksum <"$env_file")
reset_db
database=sqlite
check_not "SQLite over an existing PostgreSQL installation is refused" in_subshell prepare_database
msg=$( (prepare_database) 2>&1 || true)
case "$msg" in
	*"already uses postgres"*"Nothing was changed"*"#124"*) pass "the refusal explains and points to the migration feature" ;;
	*) fail "the refusal explains and points to the migration feature (got: $msg)" ;;
esac
equal "the refused run touched nothing" "$(cksum <"$env_file")" "$before"
reset_db
prepare_database
equal "a re-run without --database keeps PostgreSQL" "$database" postgres
equal "and keeps the choice as it is" "$db_keep" 1
write_settings
equal "the environment file is unchanged by a kept choice" "$(cksum <"$env_file")" "$before"
# only what is given changes on an existing PostgreSQL installation
reset_db
database=postgres db_port=5444 db_options=x
prepare_database
equal "a changed option does not bring the defaults back" "$db_name$db_user$db_sslmode" ""
write_settings
equal "the given option is updated" "$(grep -c '^REGULUS_DATABASE_PORT=5444$' "$env_file")" 1
equal "the other options stay" "$(grep -c '^REGULUS_DATABASE_HOST=old.example$' "$env_file")" 1

rm -f "$env_file"
: >"$DATA_DIR/regulus.db"
equal "an existing SQLite installation is recognised" "$(existing_backend)" sqlite
reset_db
database=postgres db_host=db.example db_options=x db_password_file=$tmp/pw
check_not "PostgreSQL over an existing SQLite installation is refused" in_subshell prepare_database
reset_db
prepare_database
equal "a re-run without --database keeps SQLite" "$database" sqlite
rm -f "$DATA_DIR/regulus.db"
printf 'agent\n' >"$CONFIG_DIR/role"
equal "an agent installation has no database" "$(existing_backend)" none
rm -f "$CONFIG_DIR/role"

# The password never appears in the arguments of a command, in the output, or
# in a trace. The shims record the arguments of the external commands the
# script runs; builtins (printf, read, test) start no process.
shim=$tmp/shim
cmdlog=$tmp/cmds.log
mkdir -p "$shim"
: >"$cmdlog"
for c in grep sed mv install mktemp cat head tail stat tr cut rm chmod; do
	real=$(command -v "$c") || continue
	printf '#!/bin/sh\nprintf "%%s\\n" "%s $*" >>"%s"\nexec %s "$@"\n' "$c" "$cmdlog" "$real" >"$shim/$c"
	chmod +x "$shim/$c"
done
cat >"$shim/dpkg-deb" <<EOF
#!/bin/sh
# dpkg-deb -x PACKAGE DIR: puts the fake binary in DIR
mkdir -p "\$3/usr/bin"
cp "$tmp/fake-regulus" "\$3/usr/bin/regulus"
chmod +x "\$3/usr/bin/regulus"
EOF
chmod +x "$shim/dpkg-deb"
cat >"$tmp/fake-regulus" <<EOF
#!/bin/sh
if [ "\$1 \$2" = "db -h" ]; then exit \${FAKE_HAS_DB:-0}; fi
printf '%s\n' "regulus \$*" >>"$cmdlog"
printf '%s\n' "env-password=\$REGULUS_DATABASE_PASSWORD env-host=\$REGULUS_DATABASE_HOST" >>"$tmp/fake-env.log"
echo "Database: PostgreSQL (fake)"
[ "\${FAKE_CHECK_RC:-0}" = 0 ] || echo "regulus: database check failed: authentication failed: wrong password for this user" >&2
exit \${FAKE_CHECK_RC:-0}
EOF
chmod +x "$tmp/fake-regulus"

work=$tmp/work
mkdir -p "$work"
file=regulus_1.0.0_linux_amd64.deb
version=1.0.0
: >"$work/$file"
leak_driver=$tmp/driver.sh
cat >"$leak_driver" <<EOF
REGULUS_INSTALL_SOURCED=1
. "$here/../packaging/install.sh"
set +e
SUDO=
CONFIG_DIR=$tmp/leak-etc
DATA_DIR=$tmp/leak-var
dpkg() { return 1; }
mkdir -p "\$CONFIG_DIR" "\$DATA_DIR"
assume_yes=1 role=hub
database=postgres db_host=db.example db_port= db_name= db_user= db_sslmode= db_sslrootcert= db_password= db_keep=0
db_password_file=$tmp/pw db_options=x builtin_agent=default self_update=0
work=$work file=$file version=$version
prepare_database
precheck_database
write_settings
printf 'finished\n'
EOF
: >"$tmp/fake-env.log"
out=$(PATH="$shim:$PATH" FAKE_HAS_DB=0 FAKE_CHECK_RC=0 sh -x "$leak_driver" 2>&1)
case "$out" in
	*finished*) pass "the database steps run to the end" ;;
	*) fail "the database steps run to the end ($out)" ;;
esac
case "$out" in
	*'p"a'* | *'ss $word'*) fail "the password is not in the output or the sh -x trace" ;;
	*) pass "the password is not in the output or the sh -x trace" ;;
esac
if grep -q 'ss \$word' "$cmdlog" || grep -q 'p"a' "$cmdlog"; then
	fail "the password is not in the arguments of any command"
else
	pass "the password is not in the arguments of any command"
fi
equal "the checked binary got the password in its environment" "$(grep -c -F "env-password=$secret" "$tmp/fake-env.log")" 1
equal "the binary was run to check" "$(grep -c '^regulus db check' "$cmdlog")" 1
equal "the settings were written after the check" "$(grep -c '^REGULUS_DATABASE_PASSWORD=' "$tmp/leak-etc/regulus.env")" 1
equal "the environment file keeps mode 0640" "$(stat -c %a "$tmp/leak-etc/regulus.env")" 640

# the check before installing
reset_db
SUDO=
database=postgres db_host=db.example db_port=5432 db_name=regulus db_user=regulus db_sslmode=prefer db_password=x
PATH="$shim:$PATH" FAKE_HAS_DB=0 FAKE_CHECK_RC=0 in_subshell precheck_database >/dev/null 2>&1
equal "a good check lets the installation go on" "$?" 0
out=$(PATH="$shim:$PATH" FAKE_HAS_DB=0 FAKE_CHECK_RC=1 in_subshell precheck_database 2>&1)
rc=$?
equal "a failed check stops the installation" "$rc" 1
case "$out" in
	*"wrong password"*"nothing was installed or changed"*) pass "the failure shows the cause and says nothing was changed" ;;
	*) fail "the failure shows the cause and says nothing was changed ($out)" ;;
esac
out=$(PATH="$shim:$PATH" FAKE_HAS_DB=2 in_subshell precheck_database 2>&1)
case "$out" in
	*"cannot use PostgreSQL"*) pass "a release without PostgreSQL support is refused" ;;
	*) fail "a release without PostgreSQL support is refused ($out)" ;;
esac
db_keep=1
PATH="$shim:$PATH" FAKE_CHECK_RC=1 in_subshell precheck_database >/dev/null 2>&1
equal "a kept choice is not checked before the install" "$?" 0
db_keep=0 database=sqlite
PATH="$shim:$PATH" FAKE_CHECK_RC=1 in_subshell precheck_database >/dev/null 2>&1
equal "SQLite is not checked" "$?" 0

# the options on the command line
check_not "needs a value for --database" run_script --database
check_not "rejects an unknown database" run_script --database mysql
check_not "PostgreSQL options with sqlite are refused" run_script --database sqlite --db-host db.example
check_not "PostgreSQL options without --database are refused" run_script --db-host db.example
check_not "the agent role takes no database" run_script --role agent --hub hub.example --token abc --ca-fingerprint "$fp" --database postgres
check_not "the agent role takes no PostgreSQL option" run_script --role agent --hub hub.example --token abc --ca-fingerprint "$fp" --db-host db.example
check_not "a bad port is refused" run_script --database postgres --db-host db.example --db-port 70000
check_not "a bad TLS mode is refused" run_script --database postgres --db-host db.example --db-sslmode sometimes
check_not "a password file that does not exist is refused" run_script --database postgres --db-host db.example --database-password-file "$tmp/none"
check_not "the password is not an option" run_script --database postgres --db-host db.example --db-password hunter2

if [ "$failures" -ne 0 ]; then
	printf '%d test(s) failed\n' "$failures"
	exit 1
fi
echo "install script tests passed"
