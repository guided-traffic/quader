#!/usr/bin/env bash
# Publishes the newest evidence record and coverage badge onto the branch `coverage` (Q-ARCH-811):
#
#   publish-coverage-record.sh
#
# Run by the job Semantic Release after `npx semantic-release`, in the checkout of the run's commit (the whole history, with a
# credential that may push). The record (the artifact coverage-evidence: implemented.txt, checklist.md and README.md of
# docs/coverage) and the badge (the artifact coverage-badge: coverage.json) are taken each from the completed push run of
# release.yml on main whose commit is the nearest to this one: this commit or its nearest ancestor that has the artifact. A run
# of a commit that is not an ancestor (a newer one, finished earlier) is never taken, so the record never runs ahead of the
# commit it is published for.
#
# The branch gets one commit when the record or the badge is newer than the one it holds (the commit of the branch's record is an
# ancestor of the candidate's); its tree is the record's commit with the four files, and its parents are the branch's head and,
# when the record moved, the record's commit, so every push is a fast-forward. Its message names the sources in trailers
# (Record-Commit, Record-Run, Badge-Commit, Badge-Run, Released-Commit) and carries [skip ci]. main is never written.
#
# Nothing here can fail or hold the release: the release is published before this runs, a missing or older record leaves the
# branch as it is and says so, and a push that is refused because another job pushed first is decided again on the new head, up
# to three times, then given up with a warning. The script exits 0 in every one of these cases; anything else is a defect of the
# script, and the step does not fail the job for it (continue-on-error).
#
# Environment: GITHUB_TOKEN (actions: read, for the runs and the artifacts), GITHUB_REPOSITORY, GITHUB_SHA (default HEAD),
# GITHUB_API_URL (default https://api.github.com), QUADER_RECORD_RUNS (default 40) bounds how many runs are listed, and
# QUADER_RECORD_REMOTE (default origin) is the remote the branch is fetched from and pushed to.
set +x
set -euo pipefail

: "${GITHUB_TOKEN:?GITHUB_TOKEN is not set}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
api="${GITHUB_API_URL:-https://api.github.com}"
remote="${QUADER_RECORD_REMOTE:-origin}"
branch=coverage
head="$(git rev-parse --verify "${GITHUB_SHA:-HEAD}^{commit}")"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

call() {
  printf 'header = "Authorization: Bearer %s"\n' "$GITHUB_TOKEN" |
    curl -q -fsSL --retry 3 --retry-delay 5 --config - -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' "$@"
}

# The completed push runs of release.yml on main of this repository whose commit is this one or an ancestor, nearest first.
if ! call "$api/repos/$GITHUB_REPOSITORY/actions/workflows/release.yml/runs?branch=main&event=push&status=completed&per_page=${QUADER_RECORD_RUNS:-40}" > "$tmp/runs.json"; then
  echo "::warning::the runs of Test and Release could not be listed; the branch $branch stays as it is"
  exit 0
fi
: > "$tmp/ancestors.jsonl"
jq -c --arg repo "$GITHUB_REPOSITORY" '.workflow_runs[] | select(.event == "push" and .head_repository.full_name == $repo)
  | {id, sha: .head_sha, epoch: (.created_at | fromdateiso8601)}' "$tmp/runs.json" |
  while read -r r; do
    sha="$(jq -r .sha <<<"$r")"
    git cat-file -e "$sha^{commit}" 2>/dev/null || continue
    git merge-base --is-ancestor "$sha" "$head" || continue
    jq -c --argjson distance "$(git rev-list --count "$sha..$head")" '. + {distance: $distance}' <<<"$r"
  done > "$tmp/ancestors.jsonl"

# The nearest run that has each artifact: record and badge are chosen apart, a run may have written one and not the other.
record_run="" record_sha="" record_id="" badge_run="" badge_sha="" badge_id=""
while read -r r; do
  run="$(jq -r .id <<<"$r")"
  sha="$(jq -r .sha <<<"$r")"
  call "$api/repos/$GITHUB_REPOSITORY/actions/runs/$run/artifacts?per_page=100" > "$tmp/artifacts.json" || continue
  if [ -z "$record_id" ]; then
    record_id="$(jq -r '[.artifacts[] | select(.name == "coverage-evidence" and (.expired | not))][0].id // empty' "$tmp/artifacts.json")"
    [ -z "$record_id" ] || { record_run="$run"; record_sha="$sha"; }
  fi
  if [ -z "$badge_id" ]; then
    badge_id="$(jq -r '[.artifacts[] | select(.name == "coverage-badge" and (.expired | not))][0].id // empty' "$tmp/artifacts.json")"
    [ -z "$badge_id" ] || { badge_run="$run"; badge_sha="$sha"; }
  fi
  [ -z "$record_id" ] || [ -z "$badge_id" ] || break
done < <(jq -c -s 'sort_by(.distance, -.epoch) | .[]' "$tmp/ancestors.jsonl")

if [ -n "$record_id" ]; then
  echo "newest record: run $record_run, commit ${record_sha:0:9} ($(git rev-list --count "$record_sha..$head") commits before ${head:0:9})"
else
  echo "no completed run of ${head:0:9} or of an ancestor has a record"
fi
if [ -n "$badge_id" ]; then
  echo "newest badge: run $badge_run, commit ${badge_sha:0:9}"
else
  echo "no completed run of ${head:0:9} or of an ancestor has a badge"
fi

# newer <candidate> <held>: the candidate's commit comes after the one the branch holds.
newer() {
  [ -n "$1" ] || return 1
  [ -n "$2" ] || return 0
  [ "$1" != "$2" ] && git merge-base --is-ancestor "$2" "$1"
}

fetch() {
  local id="$1" into="$2"
  mkdir -p "$into"
  call -o "$tmp/artifact.zip" "$api/repos/$GITHUB_REPOSITORY/actions/artifacts/$id/zip" && unzip -q -o "$tmp/artifact.zip" -d "$into"
}
if [ -n "$record_id" ] && ! { fetch "$record_id" "$tmp/record" && [ -s "$tmp/record/implemented.txt" ] && [ -s "$tmp/record/checklist.md" ] && [ -s "$tmp/record/README.md" ]; }; then
  echo "::warning::the record of run $record_run could not be read whole; it is not published"
  record_id=""
fi
if [ -n "$badge_id" ] && ! { fetch "$badge_id" "$tmp/badge" && jq -e '.schemaVersion and .message' "$tmp/badge/coverage.json" >/dev/null 2>&1; }; then
  echo "::warning::the badge of run $badge_run could not be read; it is not published"
  badge_id=""
fi

short() { if [ -n "$1" ]; then echo "${1:0:9}"; else echo none; fi; }
trailer() { git log -1 --format=%B "$1" | sed -n "s/^$2: //p" | head -n1; }
version="$(git tag --points-at "$head" --list 'v*' | head -n1)"

for attempt in 1 2 3; do
  tip=""
  if git fetch -q "$remote" "+refs/heads/$branch:refs/remotes/$remote/$branch" 2>/dev/null; then
    tip="$(git rev-parse "refs/remotes/$remote/$branch")"
  fi
  held_record="" held_badge=""
  if [ -n "$tip" ]; then
    held_record="$(trailer "$tip" Record-Commit)"
    held_badge="$(trailer "$tip" Badge-Commit)"
  fi
  new_record="" new_badge=""
  [ -n "$record_id" ] && newer "$record_sha" "$held_record" && new_record=1
  [ -n "$badge_id" ] && newer "$badge_sha" "$held_badge" && new_badge=1
  if [ -z "$new_record" ] && [ -z "$new_badge" ]; then
    echo "the branch $branch holds the record of $(short "$held_record") and the badge of $(short "$held_badge"); nothing newer to publish"
    exit 0
  fi

  # The tree: the record's commit with the record's files, or the branch's head with the badge alone.
  export GIT_INDEX_FILE="$tmp/index"
  rm -f "$GIT_INDEX_FILE"
  parents=()
  [ -z "$tip" ] || parents+=(-p "$tip")
  if [ -n "$new_record" ]; then
    git read-tree "$record_sha"
    for f in implemented.txt checklist.md README.md; do
      git update-index --add --cacheinfo "100644,$(git hash-object -w "$tmp/record/$f"),docs/coverage/$f"
    done
    parents+=(-p "$record_sha")
    out_record="$record_sha" out_record_run="$record_run"
    if [ -z "$new_badge" ] && [ -n "$tip" ] && git cat-file -e "$tip:.github/badges/coverage.json" 2>/dev/null; then
      git update-index --add --cacheinfo "100644,$(git rev-parse "$tip:.github/badges/coverage.json"),.github/badges/coverage.json"
    fi
  elif [ -n "$tip" ]; then
    git read-tree "$tip"
    out_record="$held_record" out_record_run="$(trailer "$tip" Record-Run)"
  else
    # No branch yet and no record: the badge alone, on the tree of its own commit.
    git read-tree "$badge_sha"
    parents+=(-p "$badge_sha")
    out_record="" out_record_run=""
  fi
  if [ -n "$new_badge" ]; then
    git update-index --add --cacheinfo "100644,$(git hash-object -w "$tmp/badge/coverage.json"),.github/badges/coverage.json"
    out_badge="$badge_sha" out_badge_run="$badge_run"
  else
    out_badge="$held_badge" out_badge_run=""
    [ -z "$tip" ] || out_badge_run="$(trailer "$tip" Badge-Run)"
  fi
  tree="$(git write-tree)"
  unset GIT_INDEX_FILE

  message="chore(coverage): the evidence record of $(short "$out_record") and the coverage badge of $(short "$out_badge") [skip ci]

Written by the job Semantic Release of the run of ${head:0:9}${version:+ (release $version)} from the newest runs of Test and
Release on main that wrote them (Q-ARCH-811).

Record-Commit: $out_record
Record-Run: $out_record_run
Badge-Commit: $out_badge
Badge-Run: $out_badge_run
Released-Commit: $head"
  commit="$(GIT_AUTHOR_NAME='github-actions[bot]' GIT_AUTHOR_EMAIL='41898282+github-actions[bot]@users.noreply.github.com' \
    GIT_COMMITTER_NAME='github-actions[bot]' GIT_COMMITTER_EMAIL='41898282+github-actions[bot]@users.noreply.github.com' \
    git commit-tree "$tree" ${parents[@]+"${parents[@]}"} -m "$message")"

  # A plain push: it is a fast-forward of the head fetched above, or the branch's creation; never forced.
  if git push -q "$remote" "$commit:refs/heads/$branch" 2>"$tmp/push.err"; then
    echo "published to $branch: ${commit:0:9}, the record of $(short "$out_record") (run ${out_record_run:-none}), the badge of $(short "$out_badge") (run ${out_badge_run:-none})"
    exit 0
  fi
  echo "the push to $branch was refused (attempt $attempt): $(head -n3 "$tmp/push.err" | tr '\n' ' ')"
done
echo "::warning::the record and the badge were not published: the branch $branch moved three times while this job decided; the next release publishes them"
exit 0
