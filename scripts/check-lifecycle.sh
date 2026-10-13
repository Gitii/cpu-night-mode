#!/usr/bin/env bash
set -euo pipefail

binary="$(realpath "${1:?binary required}")"
work="$(mktemp -d)"
pid=""
cleanup() {
    if [[ -n "$pid" ]]; then kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
    rm -rf "$work"
}
trap cleanup EXIT
mkdir -p "$work/sysfs/policy0" "$work/sysfs/policy1" "$work/state"
printf '800000\n' > "$work/sysfs/policy0/scaling_min_freq"
printf '800000\n' > "$work/sysfs/policy1/scaling_min_freq"
printf '3700000\n' > "$work/sysfs/policy0/scaling_max_freq"
printf '1400000\n' > "$work/sysfs/policy1/scaling_max_freq"
start="$(date -u +%H:%M)"
end="$(date -u -d '+2 hours' +%H:%M)"
write_config() {
    printf '[schedule]\nstart=%s\nend=%s\ntimezone=%s\n[cpu]\nmax_frequency_mhz=%s\n' "$start" "$end" "$1" "$2" > "$work/config"
}
wait_max() {
    for ((i=0; i<100; i++)); do
        if [[ "$(< "$work/sysfs/policy0/scaling_max_freq")" == "$1" ]]; then return; fi
        sleep 0.05
    done
    printf 'Expected ceiling %s; daemon log:\n' "$1" >&2
    cat "$work/log" >&2
    exit 1
}
launch() {
    "$binary" -config "$work/config" -sysfs "$work/sysfs" -state-dir "$work/state" daemon >> "$work/log" 2>&1 &
    pid=$!
}

write_config UTC 1600
launch
wait_max 1600000
[[ "$(< "$work/sysfs/policy1/scaling_max_freq")" == 1400000 ]]
write_config Invalid/Timezone 2000
kill -HUP "$pid"
sleep 0.2
kill -0 "$pid"
[[ "$(< "$work/sysfs/policy0/scaling_max_freq")" == 1600000 ]]
write_config UTC 2000
kill -HUP "$pid"
wait_max 2000000
kill -KILL "$pid"
wait "$pid" 2>/dev/null || true
pid=""
[[ -f "$work/state/originals.json" ]]
launch
wait_max 2000000
sleep 0.2
kill -TERM "$pid"
wait "$pid"
pid=""
wait_max 3700000
[[ "$(< "$work/sysfs/policy1/scaling_max_freq")" == 1400000 ]]
[[ ! -e "$work/state/originals.json" ]]

# ExecStopPost's recovery command must work independently of broken config.
write_config UTC 1200
launch
wait_max 1200000
kill -KILL "$pid"
wait "$pid" 2>/dev/null || true
pid=""
printf 'broken config\n' > "$work/config"
"$binary" -sysfs "$work/sysfs" -state-dir "$work/state" restore
wait_max 3700000
printf 'Daemon lifecycle and recovery OK\n'
