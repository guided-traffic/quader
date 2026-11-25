#!/usr/bin/env bash
# The accounts of the upstream registries for the two development profiles (Q-ARCH-1066). The owner keeps the account of a
# registry in <main checkout>/.dev/credentials/upstream-registries.env (mode 0600 in a directory of mode 0700, ignored by git and by
# the image build: .gitignore and .dockerignore exclude .dev), one KEY=value per line, and the profiles hand it to Quader through the
# product's own mechanism from outside: the UI profile through repositories.upstream_accounts of its quader.yaml (R-REPO-077), the
# kind profile through a Secret that the chart's secrets.upstreamAccounts names (R-HA-073). Nothing of this is in the product, its
# configuration keys, the chart or an image, and nothing listens.
#
#   files <dir> <main|none>      writes <dir>/<key>/username and <dir>/<key>/password (files 0600, directories 0700) for every
#                                registry of the table below whose keys hold an account in the source file, and removes them where
#                                none is held, saying so in one line. The source file is the one UPSTREAM_ACCOUNTS_FILE names; else,
#                                with main, the main checkout's .dev/credentials/upstream-registries.env when this checkout is the
#                                main checkout and none in a git worktree (Q-ARCH-1067); with none, no file.
#   secret <context> <ns> <dir>  applies the Secret quader-upstream-<key> (keys username and token) from those files, server side, or
#                                deletes it where the files are not there.
#   dockerconfig <dir>           prints a Docker configuration that holds the Docker Hub account of those files, for the kubelets of
#                                the kind nodes (deploy/dev/up.sh, node_registry_credentials); exits 3 without one.
#
# A value never becomes a shell word (Q-ARCH-1066): sed reads it from the source file into a file, and kubectl, base64 and the
# checks read that file, so bash -x, a process list or a log shows key names and paths only. The source file is never sourced. A
# value that is empty or starts with REPLACE_WITH_ (the placeholder) is no account. One pair of enclosing quotes and a carriage
# return are dropped; the product drops one trailing line break and refuses a line break or a NUL inside, more than 8 KiB, and a
# colon in a username (R-REPO-077), which this script refuses before it writes.
# docs/developer/local-profile.md#the-docker-hub-account
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# One line per registry of the project upstream: its key, the variable of the username (- for a bearer token), the variable of the
# password or token. Adding a registry is one line here (and, for the kind profile, its entry in values-upstream-accounts.yaml).
registries=(
  "docker-hub DOCKERHUB_USERNAME DOCKERHUB_TOKEN"
)

usage() {
  echo "usage: $0 files <dir> <main|none> | secret <context> <namespace> <dir> | dockerconfig <dir>" >&2
  exit 2
}

# main_file prints the main checkout's file, or nothing in a git worktree.
main_file() {
  local git_dir common
  git_dir="$(git -C "$repo" rev-parse --absolute-git-dir 2>/dev/null || true)"
  common="$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  if [ -n "$git_dir" ] && [ -n "$common" ] && [ "$git_dir" != "$common" ]; then
    return 0
  fi
  printf '%s\n' "$repo/.dev/credentials/upstream-registries.env"
}

# extract <source> <variable> <file> writes the variable's last value in the source to the file, without a line break, readable by
# the caller only; the value goes from sed to the file and is never a word of this shell.
extract() {
  (
    umask 077
    sed -n "s/^$2=//p" "$1" | tail -n 1 | tr -d '\r\n' | sed -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'\$/\1/" > "$3"
  )
}

# usable <file> succeeds when the file holds a value that is not the placeholder.
usable() {
  [ -s "$1" ] && ! grep -q '^REPLACE_WITH_' "$1"
}

files() {
  local out="$1" mode="$2" src line key user_var pass_var dir tmp why
  case "$mode" in main | none) ;; *) usage ;; esac
  src="${UPSTREAM_ACCOUNTS_FILE:-}"
  if [ -z "$src" ] && [ "$mode" = main ]; then
    src="$(main_file)"
  fi
  mkdir -p "$out"
  chmod 0700 "$out"
  for line in "${registries[@]}"; do
    read -r key user_var pass_var <<<"$line"
    dir="$out/$key"
    why="" tmp=""
    if [ -z "$src" ]; then
      if [ "$mode" = main ]; then
        why="this checkout is a git worktree, which reads an account only from the file UPSTREAM_ACCOUNTS_FILE names"
      else
        why="this instance reads an account only from the file UPSTREAM_ACCOUNTS_FILE names"
      fi
    elif [ ! -f "$src" ]; then
      why="$src does not exist"
    fi
    if [ -z "$why" ]; then
      tmp="$(mktemp -d "$out/.$key.XXXXXX")"
      chmod 0700 "$tmp"
      extract "$src" "$pass_var" "$tmp/password"
      if [ "$user_var" != - ]; then
        extract "$src" "$user_var" "$tmp/username"
      fi
      if ! usable "$tmp/password" || { [ "$user_var" != - ] && ! usable "$tmp/username"; }; then
        if [ "$user_var" = - ]; then
          why="$src holds no account for it ($pass_var: empty or REPLACE_WITH_...)"
        else
          why="$src holds no account for it ($user_var, $pass_var: empty or REPLACE_WITH_...)"
        fi
      elif [ "$user_var" != - ] && grep -q ':' "$tmp/username"; then
        why="the username of $src ($user_var) holds a colon, which a basic account cannot carry"
      elif [ "$(($(wc -c < "$tmp/password")))" -gt 8192 ] || { [ "$user_var" != - ] && [ "$(($(wc -c < "$tmp/username")))" -gt 8192 ]; }; then
        why="a value of $src for it is longer than 8 KiB"
      fi
    fi
    if [ -n "$why" ]; then
      [ -z "$tmp" ] || rm -rf "$tmp"
      rm -rf "$dir"
      echo "upstream/$key pulls without an account: $why"
      continue
    fi
    # Each file is renamed into place, so a backend that reads them while they change reads the old or the new value, never none.
    mkdir -p "$dir"
    chmod 0700 "$dir"
    mv -f "$tmp/password" "$dir/password"
    if [ "$user_var" = - ]; then
      rm -f "$dir/username"
    else
      mv -f "$tmp/username" "$dir/username"
    fi
    rmdir "$tmp"
    echo "upstream/$key pulls with the account of $src"
    if [ -n "$(find "$src" -maxdepth 0 \( -perm -040 -o -perm -004 \) 2>/dev/null)" ]; then
      echo "warning: $src is readable by others than its owner; chmod 600 it"
    fi
  done
}

secret() {
  local ctx="$1" ns="$2" out="$3" line key user_var pass_var name
  for line in "${registries[@]}"; do
    read -r key user_var pass_var <<<"$line"
    name="quader-upstream-$key"
    if [ -f "$out/$key/password" ]; then
      # Applied server side: a client-side apply would keep the whole object, data included, in the annotation
      # kubectl.kubernetes.io/last-applied-configuration. The manifest goes through the pipe, never to a log or a file.
      if [ "$user_var" = - ]; then
        kubectl --context "$ctx" -n "$ns" create secret generic "$name" --from-file=token="$out/$key/password" --dry-run=client -o yaml \
          | kubectl --context "$ctx" -n "$ns" apply --server-side --force-conflicts --field-manager=quader-dev-upstream-accounts -f - >/dev/null
      else
        kubectl --context "$ctx" -n "$ns" create secret generic "$name" --from-file=username="$out/$key/username" \
          --from-file=token="$out/$key/password" --dry-run=client -o yaml \
          | kubectl --context "$ctx" -n "$ns" apply --server-side --force-conflicts --field-manager=quader-dev-upstream-accounts -f - >/dev/null
      fi
      echo "the Secret $name holds the account of upstream/$key"
    else
      kubectl --context "$ctx" -n "$ns" delete secret "$name" --ignore-not-found >/dev/null
    fi
  done
}

dockerconfig() {
  local dir="$1/docker-hub"
  [ -f "$dir/username" ] && [ -f "$dir/password" ] || exit 3
  printf '{"auths":{"https://index.docker.io/v1/":{"auth":"'
  { cat "$dir/username"; printf ':'; cat "$dir/password"; } | base64 | tr -d '\n'
  printf '"}}}\n'
}

case "${1:-}" in
  files) [ "$#" = 3 ] || usage; files "$2" "$3" ;;
  secret) [ "$#" = 4 ] || usage; secret "$2" "$3" "$4" ;;
  dockerconfig) [ "$#" = 2 ] || usage; dockerconfig "$2" ;;
  *) usage ;;
esac
