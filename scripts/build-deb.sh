#!/usr/bin/env bash
set -euo pipefail

root="$PWD"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir "$work/src" "$root/dist" -p
tar --exclude=.git --exclude=build --exclude=dist -cf - . | tar -xf - -C "$work/src"
cd "$work/src"

# Go comes from the official toolchain stage, rather than Ubuntu's Go package.
# dpkg's dependency check is skipped only for that supplied toolchain.
. /etc/os-release
upstream="$(dpkg-parsechangelog -S Version | cut -d- -f1)"
if [[ "${GITHUB_REF:-}" == refs/tags/v* ]]; then
    [[ "${GITHUB_REF#refs/tags/v}" == "$upstream" ]] || { printf 'Tag and changelog versions differ\n' >&2; exit 1; }
    debver="${upstream}-1~ubuntu${VERSION_ID}"
elif [[ -n "${GITHUB_SHA:-}" ]]; then
    debver="${upstream}-0+git$(date -u +%Y%m%d).${GITHUB_SHA:0:7}~ubuntu${VERSION_ID}"
else
    debver="${upstream}-1~ubuntu${VERSION_ID}"
fi
export DEBEMAIL=gitii@users.noreply.github.com DEBFULLNAME=Gitii
dch -b -v "$debver" -D "$VERSION_CODENAME" --force-distribution "Build for Ubuntu ${VERSION_ID}."
dpkg-buildpackage -us -uc -b -d
cp "$work"/cpu-night-mode_*.deb "$work"/cpu-night-mode_*.changes "$work"/cpu-night-mode_*.buildinfo "$root/dist/"
lintian --fail-on error --tag-display-limit 0 "$root/dist/cpu-night-mode_${debver}_$(dpkg --print-architecture).deb"
