#!/usr/bin/env bash
# Prints the tag of the images this checkout builds and the development profiles run: `dev` in a repository's main checkout (and
# in a tree without git, as on a CI runner without history), `dev-<name>` in a git worktree, <name> the worktree's directory in
# lower case with every character a tag may not hold replaced by `-`. QUADER_DEV_TAG overrides it.
#
# Why: the images of the profiles live in the one Docker daemon of the machine under fixed names (guidedtraffic/quader:dev,
# quader-e2e-driver:dev, the stubs' and the clients' :dev), so a builder's worktree and the main checkout that built at the same
# time tagged over each other, and each profile ran whichever image was tagged last (F-059). With a tag per checkout two checkouts
# never run each other's image; the main checkout keeps `dev`, so the owner's names, the pipeline's and every document's stay.
#
# Read by deploy/dev/lib.sh (DEV_TAG), the Makefile (DEV_TAG) and .github/scripts/client-images.sh; one definition, so the image a
# target builds is the image a profile runs.
set -euo pipefail
if [ -n "${QUADER_DEV_TAG:-}" ]; then
  printf '%s\n' "$QUADER_DEV_TAG"
  exit 0
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
git_dir="$(git -C "$root" rev-parse --absolute-git-dir 2>/dev/null || true)"
common="$(git -C "$root" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
if [ -z "$git_dir" ] || [ -z "$common" ] || [ "$git_dir" = "$common" ]; then
  echo dev
  exit 0
fi
name="$(basename "$root" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9.\n-' '-')"
# A tag holds at most 128 characters; `dev-` and 100 of the name leave room for a suffix such as -marked.
printf 'dev-%s\n' "${name:0:100}"
