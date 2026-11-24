#!/usr/bin/env bash
# The install step of .github/actions/zstd (Q-ARCH-1061): WANT=<version> DIGEST=<SHA-256 of the source archive> DIR=<directory>
# install.sh
#
# Uses DIR/zstd when the cache restored it and it reports WANT; otherwise downloads the release's source archive from github.com
# (tools/github-download.sh, at most 60 s), checks its SHA-256, builds the program with `make zstd-release` (at most 120 s) and
# puts it into DIR, with the links zstdmt and unzstd beside it. Then DIR goes onto PATH for the later steps of the job
# (GITHUB_PATH) and the output built says whether the cache should save it.
#
# It never fails the job: any failure is a warning, the job's cache stays on gzip (or on the runner's own zstd) for this job, and
# the step exits 0. QUADER_ZSTD_SOURCE names a local archive instead of the download (the tests use it).
set -uo pipefail

: "${WANT:?}" "${DIGEST:?}" "${DIR:?}" "${GITHUB_PATH:?}" "${GITHUB_OUTPUT:?}"

give_up() {
  echo "::warning::the pinned zstd ${WANT} is not on PATH ($1): the Actions cache of this job compresses with what the runner has"
  echo "built=false" >> "$GITHUB_OUTPUT"
  exit 0
}

reports_pin() {
  local v
  v="$("$1" --quiet --version 2>/dev/null)" || return 1
  [ "${v#v}" = "$WANT" ]
}

built=false
if [ -x "$DIR/zstd" ] && reports_pin "$DIR/zstd"; then
  echo "zstd ${WANT} restored from the cache into ${DIR}"
else
  tmp="$(mktemp -d)" || give_up "no temporary directory"
  trap 'rm -rf "$tmp"' EXIT
  archive="$tmp/zstd-${WANT}.tar.gz"
  if [ -n "${QUADER_ZSTD_SOURCE:-}" ]; then
    cp "$QUADER_ZSTD_SOURCE" "$archive" || give_up "cannot read $QUADER_ZSTD_SOURCE"
  else
    QUADER_DOWNLOAD_BUDGET=60 "$GITHUB_WORKSPACE/tools/github-download.sh" \
      "https://github.com/facebook/zstd/releases/download/v${WANT}/zstd-${WANT}.tar.gz" "$archive" ||
      give_up "the download of the source archive failed"
  fi
  got="$(sha256sum "$archive" 2>/dev/null || shasum -a 256 "$archive")"
  got="${got%% *}"
  [ "$got" = "$DIGEST" ] || give_up "the source archive has SHA-256 ${got}, tools/versions.env pins ${DIGEST}"
  tar -xzf "$archive" -C "$tmp" || give_up "the source archive does not unpack"
  # At most 8 jobs of make: the runners have no CPU limit and other jobs share the node (GOMAXPROCS of the workflows). The
  # bound is timeout's on a runner; a machine without timeout (macOS, the tests) runs make unbounded.
  bound=()
  if command -v timeout >/dev/null 2>&1; then bound=(timeout -k 10 120); fi
  if ! out="$(${bound[@]+"${bound[@]}"} make -C "$tmp/zstd-${WANT}" -j 8 zstd-release 2>&1)"; then
    printf '%s\n' "$out" | tail -n 20
    give_up "make zstd-release failed"
  fi
  mkdir -p "$DIR" || give_up "cannot create $DIR"
  install -m 0755 "$tmp/zstd-${WANT}/programs/zstd" "$DIR/zstd" || give_up "cannot install the program into $DIR"
  reports_pin "$DIR/zstd" || give_up "the program built does not report ${WANT}"
  built=true
  echo "zstd ${WANT} built from the release's source archive into ${DIR}"
fi
# The names actions/cache runs through tar (zstdmt to write, unzstd to read); the program reads its name.
for name in zstdmt unzstd; do
  ln -sf zstd "$DIR/$name" || give_up "cannot link $name in $DIR"
done
abs="$(cd "$DIR" && pwd)" || give_up "cannot resolve $DIR"
echo "$abs" >> "$GITHUB_PATH"
echo "built=$built" >> "$GITHUB_OUTPUT"
echo "zstd ${WANT} is on PATH for the later steps of this job: ${abs}"
