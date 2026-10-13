#!/usr/bin/env bash
set -euo pipefail

dpkg-query -W -f='${Status}\n' cpu-night-mode
/usr/bin/cpu-night-mode version
/usr/bin/cpu-night-mode check-config
unit="$(dpkg-query -L cpu-night-mode | grep '/cpu-night-mode.service$')"
systemd-analyze verify "$unit"
root="$(dirname "$(dirname "$(realpath "$0")")")"
bash "$root/scripts/check-lifecycle.sh" /usr/bin/cpu-night-mode
printf 'Installed package checks OK\n'
