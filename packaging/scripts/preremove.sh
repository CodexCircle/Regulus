#!/bin/sh
# Stops the service when the package is removed. An upgrade keeps it running:
# the new version restarts it in its own postinstall. dpkg passes "remove",
# "upgrade" or "deconfigure"; rpm passes the number of copies left (0 = removed).
set -e

case "$1" in
	remove | 0) ;;
	*) exit 0 ;;
esac

if [ -d /run/systemd/system ]; then
	systemctl disable --now regulus.service || true
	systemctl disable --now regulus-agent.service || true
fi
