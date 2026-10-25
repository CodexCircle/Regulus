#!/bin/sh
# Creates the system user and group the hub runs as. It has no login shell, no
# home directory and no sudo rights.
set -e

if getent passwd regulus >/dev/null 2>&1; then
	exit 0
fi

if [ -f /etc/debian_version ] && command -v adduser >/dev/null 2>&1; then
	adduser --system --group --no-create-home --home /var/lib/regulus --gecos "Regulus hub" regulus
else
	getent group regulus >/dev/null 2>&1 || groupadd --system regulus
	useradd --system --gid regulus --no-create-home --home-dir /var/lib/regulus --shell /usr/sbin/nologin --comment "Regulus hub" regulus
fi
