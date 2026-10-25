#!/bin/sh
# On "purge" (dpkg) the settings written by the installer go away. The data in
# /var/lib/regulus and the system user are left alone on purpose: they hold the
# accounts and the layouts, and deleting them is the administrator's decision.
set -e

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi

case "$1" in
	purge)
		rm -f /etc/regulus/regulus.env
		rmdir /etc/regulus 2>/dev/null || true
		echo "Regulus: the data in /var/lib/regulus and the user 'regulus' were kept."
		echo "To delete them: rm -rf /var/lib/regulus && deluser regulus"
		;;
esac
