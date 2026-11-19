#!/usr/bin/env bash
# Installs the browser of the locked Playwright version in a CI job: playwright-install.sh <browser>...
# Run in web/ after npm ci.
#
# Playwright's --with-deps installs the browser's system libraries through apt itself, and its first call reads the package
# indexes: a job on a runner that has the libraries still waits on the Ubuntu archive, and fails with it (F-150: a slow
# mirror; F-154: an archive that did not answer for over an hour). So the script asks first. "playwright install-deps
# --dry-run" simulates the install without the network and exits 0 only when nothing is missing; then the browser alone is
# fetched, from Playwright's own host. On a runner that lacks a library the install is the one it was, with --with-deps.
# The list of libraries is Playwright's, of the locked version: the script names none.
set -euo pipefail

[ "$#" -gt 0 ] || { echo "usage: playwright-install.sh <browser>..." >&2; exit 2; }

if npx playwright install-deps --dry-run "$@"; then
  echo "playwright-install: the system libraries of $* are on the runner; fetching the browser only"
  npx playwright install "$@"
else
  echo "playwright-install: system libraries of $* are missing on the runner; installing them with the browser"
  npx playwright install --with-deps "$@"
fi
