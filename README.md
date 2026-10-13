# CPU night mode

A small Go service that lowers the maximum CPU frequency during a daily window
and restores the original limits afterward. It manages every CPUFreq policy and
does not collect temperature or fan data or change fan control, governors, energy
preferences, minimum frequencies, or turbo switches.

## Install

Download the `.deb` for your Ubuntu version and architecture from
[Releases](https://github.com/Gitii/cpu-night-mode/releases), then install it:

```fish
sudo apt install ./cpu-night-mode_0.1.0-1~ubuntu26.04_amd64.deb
cpu-night-mode status
systemctl status cpu-night-mode
```

Installation enables and starts the system service. The default window is
**19:00–09:00 Europe/Berlin**, with a **1,600 MHz** ceiling. The CPU can still
scale below this ceiling. Lower existing per-policy ceilings are never raised.
The cap is a starting setting, not a measured acoustic optimum.

Requires Linux CPUFreq policies with writable `scaling_max_freq`, systemd, and
root privileges for hardware writes. The Debian package supplies a static Go
binary with embedded timezone data; no Go compiler is required on the host.
Ubuntu 24.04 and 26.04 builds are tested in Docker. Hardware behavior was checked
on a Dell OptiPlex 5060 with Intel `intel_pstate`; other drivers may round caps.
No additional power-management utility is required.

## Configure

Edit `/etc/cpu-night-mode.conf`:

```ini
[schedule]
start = 19:00
end = 09:00
timezone = Europe/Berlin

[cpu]
max_frequency_mhz = 1600
```

Times use `HH:MM`, repeat daily, and are evaluated in the configured IANA timezone,
independent of the host timezone. Start is inclusive and end exclusive. Same-day
windows are supported; equal start/end times are invalid. Local wall-clock
evaluation handles daylight saving transitions, including the repeated hour.
During a skipped DST hour, a boundary takes effect at the first subsequent
evaluation whose local time is inside/outside the window. Omitted keys use the
defaults above. Unknown keys, duplicate keys, and invalid values are errors.
Only whole positive MHz values up to 100,000 are accepted; a cap below a policy's
current minimum is rejected without lowering that minimum.

```fish
cpu-night-mode check-config
sudo systemctl reload cpu-night-mode
cpu-night-mode status
journalctl -u cpu-night-mode
```

Changes take effect on reload. Invalid syntax on reload retains the last valid
configuration. `status` reads the file currently on disk and actual policy
ceilings; it does not query the daemon's in-memory configuration. The journal
records reloads, rejected reloads, and transitions.

## Lifecycle and recovery

The service checks immediately at startup and every 30 seconds, including after
resume or wall-clock changes. It rechecks all policies and reapplies the ceiling
while inside the window. Newly discovered policies are snapshotted before use.
Do not run another utility that concurrently owns maximum-frequency settings.

Original per-policy ceilings are saved atomically in
`/run/cpu-night-mode/originals.json` **before** hardware writes. State survives
service restarts within a boot, but not reboot. Leaving the window or stopping
the service restores the saved values rather than guessing a daytime default.
Failed application rolls back; failed restoration retains the recovery state.
Readback verification waits up to two seconds per policy for queued kernel
updates. Restoration writes the original value even if the initial readback
matches, so it also supersedes any pending cap.
Missing/offline policies during restoration cause a failure with state retained;
bring them back online and retry restoration. State is validated against the
current boot and only allows CPU policy names, not arbitrary file paths.

systemd's `ExecStopPost` retries restoration after normal stops or crashes,
including SIGKILL. On failure, systemd restarts the daemon after 30 seconds,
limited to five starts per five minutes. Check the journal if it fails. An
exclusive lock prevents simultaneous daemon/recovery writers.

```fish
# Stop and restore, keeping automatic startup enabled:
sudo systemctl stop cpu-night-mode

# Stop, restore, and disable automatic startup:
sudo systemctl disable --now cpu-night-mode

# Retry recovery while the daemon is stopped:
sudo cpu-night-mode restore
```

Package removal stops the service and restores limits through its stop hook.
The package's pre-removal check retries restoration and blocks removal or upgrade
if it fails, keeping the recovery binary available. Removal retains the
configuration file; purge removes it. Recovery state remains under `/run` if
restoration failed so it can be retried.

## Build using Docker

All compilation, Go tests, linting, and package tooling run in Docker. The host
needs Docker and Make; building needs network access to fetch base images and
Ubuntu build dependencies. There are no external Go module dependencies.

```fish
make all                            # build/cpu-night-mode
make check                          # race-enabled tests and lifecycle checks
make lint                           # gofmt, go vet, shellcheck
make deb                            # Ubuntu 26.04 packages in dist/
make deb UBUNTU_VERSION=24.04        # Ubuntu 24.04 packages
make install                        # build and install the local 26.04 package
make check-installed                # installed binary/config/unit and fake-policy tests
make uninstall                      # remove Debian package
make clean                          # remove build artifacts
```

`make format` runs gofmt in Docker. `make deb` runs the tests while packaging,
checks the package with lintian, and builds from an isolated copy so Debian
staging and generated changelog versions do not dirty the checkout. Containers
run as your user during builds; they do not mount the host CPU sysfs or require
privileged mode. Fake-policy tests never change host CPU settings.

The Dockerfile is a tooling image, not a daemon deployment image. It copies the
official Go 1.26 toolchain into the chosen Ubuntu build environment. The build
script supplies that compiler and skips dpkg's build-dependency check; the image
installs the remaining declared build tools. The Debian package is architecture
specific and defaults to the Docker host's native architecture.

CI uses the same Make targets for Ubuntu 24.04/26.04, verifies package
installation/removal, and uploads `.deb` artifacts. Push a `v` tag matching the
upstream version in `debian/changelog` to publish a GitHub release. CI snapshot
packages include the commit and Ubuntu version in their package version.

## Development

CLI commands: `daemon`, `status`, `check-config`, `restore`, and `version`.
Optional flags precede the command: `-config`, `-sysfs`, and `-state-dir`.
Alternate paths are useful for isolated tests. `restore` does not read the
configuration file, so a broken config cannot prevent recovery.

Tests cover overnight/same-day boundaries, Berlin winter/summer time and DST,
malformed config, restart recovery, changing caps, new policies, validation
before writes, partial-write rollback, failed-restoration retention, boot/path
validation, single-writer locking, signal reload, SIGTERM, and SIGKILL recovery.
Queued kernel readback and unconditional recovery writes are covered as well.
