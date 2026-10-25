#!/bin/sh
# Runs after the files are in place. Called by dpkg with "configure [old
# version]" (the old version is empty on a first install) and by rpm with 1 for
# a first install and 2 for an upgrade.
set -e

env_file=/etc/regulus/regulus.env

# The installer may have written settings here before the package arrived. They
# are not secret, but only the service needs to read them.
if [ -f "$env_file" ]; then
	chown root:regulus "$env_file"
	chmod 0640 "$env_file"
fi

# "agent" when the installer was asked for the agent role (install.sh --role
# agent): then only the agent unit is used, and the installer starts it after
# enrolling. The file holds no secret.
role=hub
if [ -r /etc/regulus/role ]; then
	role=$(head -n 1 /etc/regulus/role)
fi
if [ "$role" = agent ]; then
	install -d -o regulus -g regulus -m 0750 /var/lib/regulus-agent
fi

fresh=false
case "$1" in
	configure)
		if [ -z "$2" ]; then fresh=true; fi
		;;
	1) fresh=true ;;
esac

if [ ! -d /run/systemd/system ]; then
	echo "Regulus: systemd is not running, so the service was not started."
	echo "Start it with: systemctl enable --now regulus"
	exit 0
fi

systemctl daemon-reload || true
if [ "$fresh" = true ] && [ "$role" = agent ]; then
	echo "Regulus agent is installed; the installer enrolls it and starts the service."
elif [ "$fresh" = true ]; then
	systemctl enable --now regulus.service || {
		echo "Regulus: the service could not be started; see: journalctl -u regulus" >&2
		exit 0
	}
	cat <<'MSG'

Regulus is installed and running.
  Dashboard:    http://<this server>:8080
  Setup token:  journalctl -u regulus -o cat | grep setup_token
Open the dashboard and use the token to create the first admin account.
MSG
else
	systemctl try-restart regulus.service regulus-agent.service || true
fi
