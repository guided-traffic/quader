#!/usr/bin/env bash
# Prints the tag of the images this checkout builds and the development profiles run: `dev` in a repository's main checkout (and
# in a tree without git, as on a CI runner without history), `dev-<name>` in a git worktree, <name> the worktree's directory in
# lower case with every character a tag may not hold replaced by `-`. QUADER_DEV_TAG overrides it.
#
#   deploy/dev/dev-tag.sh              the tag
#   deploy/dev/dev-tag.sh --cluster    the name of the checkout's kind cluster, whatever QUADER_DEV_TAG says (Q-HA-491): `quader` in
#                                      a main checkout, quader-<name> in a worktree, cut to 49 characters so that its node
#                                      <cluster>-control-plane stays a host name of 63; read by lib.sh and cluster-logs.sh
#
# Why: the images of the profiles live in the one Docker daemon of the machine under fixed names (guidedtraffic/quader:dev,
# quader-e2e-driver:dev, the stubs' and the clients' :dev), so a builder's worktree and the main checkout that built at the same
# time tagged over each other, and each profile ran whichever image was tagged last (F-059). With a tag per checkout two checkouts
# never run each other's image; the main checkout keeps `dev`, so the owner's names, the pipeline's and every document's stay.
#
# Read by deploy/dev/lib.sh (DEV_TAG, and the cluster's name), the Makefile (DEV_TAG) and
# .github/scripts/client-images.sh; one definition, so the image a target builds is the image a profile runs.
set -euo pipefail
worktree_name() {
  local root git_dir common
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  git_dir="$(git -C "$root" rev-parse --absolute-git-dir 2>/dev/null || true)"
  common="$(git -C "$root" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  if [ -z "$git_dir" ] || [ -z "$common" ] || [ "$git_dir" = "$common" ]; then
    return 0
  fi
  # A tag holds at most 128 characters; `dev-` and 100 of the name leave room for a suffix such as -marked.
  local name
  name="$(basename "$root" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9.\n-' '-')"
  printf '%s\n' "${name:0:100}"
}
if [ "${1:-}" = --cluster ]; then
  name="$(worktree_name)"
  if [ -z "$name" ]; then
    echo quader
    exit 0
  fi
  cluster="quader-${name:0:42}"
  while :; do
    case "$cluster" in *- | *.) cluster="${cluster%?}" ;; *) break ;; esac
  done
  printf '%s\n' "$cluster"
  exit 0
fi
if [ -n "${QUADER_DEV_TAG:-}" ]; then
  printf '%s\n' "$QUADER_DEV_TAG"
  exit 0
fi
name="$(worktree_name)"
if [ -z "$name" ]; then
  echo dev
else
  printf 'dev-%s\n' "$name"
fi
