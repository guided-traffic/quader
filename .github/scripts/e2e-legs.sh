#!/usr/bin/env bash
# Which e2e legs of Test and Release a change runs (owner, 2026-11-20; Q-ARCH-540 to Q-ARCH-543,
# docs/developer/releasing.md#which-e2e-legs-a-change-runs). The job e2e-scope of release.yml runs it once per run, on the
# cheapest runner class, and hands the answer to the legs as job outputs, so a leg that does not apply is a skipped job and takes
# no runner.
#
#   EVENT=<github.event_name> BEFORE=<before> HEAD=<head> e2e-legs.sh
#       writes <leg>=true|false for every leg to GITHUB_OUTPUT, and to the log and the job summary why; on a push BEFORE is
#       github.event.before and HEAD github.sha, on a pull request BEFORE is the base commit and HEAD the head commit of the pull
#       request
#   e2e-legs.sh --classify
#       reads paths on standard input and prints "<path><TAB><format legs, shared or none>[ + <bastion, vuln>]" for each;
#       `git ls-files | .github/scripts/e2e-legs.sh --classify` shows the whole tree as the rules see it
#   e2e-legs.sh --lists
#       prints "<list><TAB><pattern>" for every pattern of every list
#
# The legs decided here: npm, nuget, oci, deb and ansible (each format's own leg), bastion and vuln. The legs push, access-long and
# ci run on every change and are not decided here.
#
# 1. A dispatch, a push without a commit to compare with (the first push of a branch, or a BEFORE that is not in the clone) and a
#    pull request whose base or head is not in the clone run every leg. So does any error of this script: it fails open.
# 2. A push is judged by its changed paths (git diff --no-renames --name-only BEFORE HEAD: a renamed file counts at its old and at
#    its new place), a pull request by the paths its head changed since the merge base with its base (git merge-base), by the same
#    rules, so that a dependency bot's pull request takes the legs its change concerns and not every leg:
#    a. A format's leg runs when a path is on that format's list (format_paths).
#    b. A path on no format's list is shared code and runs the reference leg, npm, unless it is on the list of paths that start no
#       format leg (no_format_leg, less no_format_leg_except) or under web/ (web_no_format_leg). There is no third way: a path
#       that is on no list at all is shared code by this rule and starts npm, never nothing.
#    c. Two shared files are judged per changed line, on the old and on the new side: in .github/workflows/release.yml a line
#       belongs to the job it stands in, with the comment block above the job's key (a leg's own job starts that leg; the header and
#       the jobs image and e2e-scope, which every leg depends on, are shared; every other job starts no leg), and in
#       tools/versions.env to the variable it sets or, for a comment or a blank line, to the variable below it (the pins of a
#       format's client images are that format's, every other pin is shared).
#    d. The legs bastion and vuln keep the lists their jobs had (bastion_paths, vuln_paths), each judged on its own; in release.yml
#       their own job starts them too.
#
# Tested by internal/tools/ciworkflows/e2elegs_test.go, against a git repository of its own and without the network. It runs on
# the bash 3.2 of macOS (no associative arrays, no mapfile); the fail-open on an error inside a command substitution needs
# inherit_errexit (bash 4.4), which the runners have.
set -Eeuo pipefail
shopt -s inherit_errexit 2>/dev/null || true

FORMATS="npm nuget oci deb ansible"
LEGS="$FORMATS bastion vuln"
REFERENCE=npm

# The paths that are a format's alone, as extended regular expressions on the path from the repository root, and why each is the
# format's: its package (internal/formats/<format>) and its data port (internal/formatdata/<format>) hold its protocol and its
# tables; a file of a shared package under internal/ or cmd/ whose name carries the format's name as a word (its configuration,
# audit events, wiring, admin command, importer, backup fix-up, queries and migrations) exists for that format only; its counting
# test upstream (test/<format>stub) and the manifests that start the upstream and the client pods serve its set only; its e2e
# tests are named <format>_ and its client images live in their own directory. Per format besides: npm's client matrix image is
# test/e2e/clients; NuGet's golden vectors (tools/nugetvectors) and its reference solution (perf/nuget) are NuGet's; OCI's API
# is the registry root (internal/api/registry) with its token service (internal/auth/registrytoken), their wiring and audit event;
# Debian alone signs with OpenPGP (internal/openpgp: every importer is Debian code); Ansible's leg is the one format leg that runs
# a browser journey (web/e2e/ansible-docs, TestAnsibleDocsJourney), so what that journey runs on is Ansible's among the format legs
# (the base Playwright config, its global setup and support, the tsconfig of web/e2e, the web workspace's package files and Node.js
# pin, which the leg installs) and so are the screens it opens, the files of web/src named ansible-* (spec files are unit tests and
# do not match).
format_paths() {
  case $1 in
    npm)
      cat <<'EOF'
^internal/formats/npm/
^internal/formatdata/npm/
^(internal|cmd)/(.*[/_.])?npm([/_.]|$)
^test/npmstub/
^deploy/dev/npmstub\.yaml$
^test/e2e/npm_
^test/e2e/clients/
EOF
      ;;
    nuget)
      cat <<'EOF'
^internal/formats/nuget/
^internal/formatdata/nuget/
^(internal|cmd)/(.*[/_.])?nuget([/_.]|$)
^test/nugetstub/
^deploy/dev/nugetstub\.yaml$
^test/e2e/nuget_
^test/e2e/nugetclients/
^tools/nugetvectors/
^perf/nuget/
EOF
      ;;
    oci)
      cat <<'EOF'
^internal/formats/oci/
^internal/formatdata/oci/
^(internal|cmd)/(.*[/_.])?oci([/_.]|$)
^internal/api/registry/
^internal/auth/registrytoken/
^internal/app/wire_registry\.go$
^internal/audit/events_registry_token\.go$
^test/ocistub/
^deploy/dev/(ocistub|oci-clients)\.yaml$
^test/e2e/oci_
^test/e2e/ociclients/
EOF
      ;;
    deb)
      cat <<'EOF'
^internal/formats/deb/
^internal/formatdata/deb/
^(internal|cmd)/(.*[/_.])?deb([/_.]|$)
^internal/openpgp/
^test/debstub/
^deploy/dev/(debstub|apt-clients)\.yaml$
^test/e2e/deb_
^test/e2e/aptclients/
EOF
      ;;
    ansible)
      cat <<'EOF'
^internal/formats/ansible/
^internal/formatdata/ansible/
^(internal|cmd)/(.*[/_.])?ansible([/_.]|$)
^test/ansiblestub/
^deploy/dev/(ansiblestub|ansible-clients)\.yaml$
^test/e2e/ansible_
^test/e2e/ansibleclients/
^web/e2e/ansible-docs/
^web/e2e/playwright\.ansible-docs\.
^web/e2e/(playwright\.config|global-setup|support)\.ts$
^web/e2e/tsconfig\.json$
^web/(package|package-lock)\.json$
^web/\.nvmrc$
^web/src/app/(.*/)?ansible-[a-z-]+\.(ts|html|scss)$
EOF
      ;;
  esac
}

# Inside a format's tree but every format's: the forwarder that every client image copies in.
shared_inside_format() {
  cat <<'EOF'
^test/e2e/clients/fwd/
EOF
}

# The paths that start no format leg, because no format leg builds, deploys or reads them: the documentation, the Markdown files
# at the root, the integration tier (nothing outside test/integration imports it), Go test files (Go builds a _test.go file into
# its own package's test binary only, never into the image, the driver or the e2e tests of another package), the developer tools
# and the architecture tests (neither the binaries nor test/e2e import internal/tools or internal/archtest), the chart's render
# tests, the compatibility check, the coverage report, semantic-release's tooling (the root package.json, hack/), the doubles that
# deploy/dev/e2e.sh starts only for other sets (the test advisory source for the vuln sets, QUADER_E2E_OSVSTUB; the CI issuer for
# ci, access-long and backup, QUADER_E2E_OIDCSTUB), the Bastion's chart (deployed by deploy/dev/bastion.sh alone), the UI
# development profile of the layout checks (deploy/ui-dev and its sign-in provider test/devidp), the other workflows and the
# nightly's leg selection, and the configuration of jobs that start no cluster (golangci-lint, Trivy, Renovate, semantic-release,
# actionlint). A compile error in a file here that a leg would build is the lint job's (golangci-lint builds every tag).
no_format_leg() {
  cat <<'EOF'
^docs/
^[^/]+\.md$
^test/integration/
_test\.go$
^internal/(tools|archtest)/
^test/(chart|compat|coverage)/
^(package|package-lock)\.json$
^hack/
^test/(osvstub|oidcstub|devidp)/
^deploy/dev/(osvstub|oidcstub)\.yaml$
^deploy/helm/quader-bastion/
^deploy/ui-dev/
^\.github/workflows/(performance|build|renovate)\.yml$
^\.github/actions/select-leg/
^(\.golangci\.yml|\.trivyignore\.yaml|renovate\.json|\.releaserc\.json|\.github/actionlint\.yaml)$
EOF
}

# The exceptions to no_format_leg: the manual is embedded in the binary, and the e2e tests are what the legs run.
no_format_leg_except() {
  cat <<'EOF'
^docs/manual/
^test/e2e/
EOF
}

# The UI starts no format leg, but where a format's list names a path under web/ (Q-ARCH-542): the format legs drive the clients,
# not the UI. The UI is what access-long (every change), the UI layout checks and the web unit tests see; the built UI embedded in
# the image is no reason, as no other format leg opens it. Revisit when a format's leg starts to exercise the UI beyond its list.
web_no_format_leg() {
  cat <<'EOF'
^web/
EOF
}

# The list of the bastion leg, as its job had it (R-BAS-049): the Bastion's packages, its chart, its spec, its e2e profile and
# suite and its browser journey.
bastion_paths() {
  cat <<'EOF'
^internal/bastion/
^internal/bastionmain/
^deploy/helm/quader-bastion/
^docs/spec/bastion\.md$
^deploy/dev/bastion
^deploy/dev/values-bastion
^test/e2e/bastion
^web/e2e/bastion
^web/e2e/playwright\.bastion
EOF
}

# The list of the vuln leg, as its job had it (R-VULN-029): the scanning, the parts of npm and NuGet that answer an audit, the core
# paths a blocked download goes through, its queries, its API, its test advisory source and e2e profile, the e2e harness, the
# NuGet clients its set uses, its screens and browser journeys, its spec. Two patterns of the old list matched no file
# (queries/vuln_ was anchored at the root; internal/api/v1/vuln_api names no file); they stand here as the paths they meant.
# The role scanner's paths outside internal/vuln/ follow it, each the scanner's alone, so that no shared path starts the leg
# (R-VULN-046): the wiring of the role and of the scanning, the scan token's file, the scanner's chart templates and its chart test.
vuln_paths() {
  cat <<'EOF'
^internal/vuln/
^internal/app/wire_(scanner|vuln)
^internal/auth/registrytoken/scan
^deploy/helm/quader/templates/(scanner|_scanner)
^test/chart/scanner_
^internal/formats/(npm|nuget)/(vuln|handlers_audit|errors)
^internal/formathost/
^internal/items/
^internal/files/
^internal/proxy/
^internal/meta/postgres/queries/vuln_
^internal/api/v1/vulnerabilities_
^test/osvstub/
^deploy/dev/osvstub
^deploy/dev/values-vuln
^deploy/dev/e2e\.sh$
^test/e2e/vuln
^test/e2e/nugetclients/
^web/src/app/features/vulnerabilities/
^web/e2e/vuln/
^web/e2e/playwright\.vuln
^web/e2e/npm-publish
^docs/spec/vulnerability-scanning\.md$
EOF
}

# The owner of a job of release.yml: a leg's own job starts that leg; the header and the jobs every leg depends on (image, whose
# image the legs load, and e2e-scope, this decision) are shared; every other job starts no leg.
workflow_job_owner() {
  case $1 in
    e2e-npm) echo npm ;;
    e2e-nuget) echo nuget ;;
    e2e-oci) echo oci ;;
    e2e-deb) echo deb ;;
    e2e-ansible) echo ansible ;;
    e2e-bastion) echo bastion ;;
    e2e-vuln) echo vuln ;;
    header|image|e2e-scope) echo shared ;;
    *) echo none ;;
  esac
}

# The owner of a pin of tools/versions.env: the pins of a format's client images (and of NuGet's golden vectors) are that
# format's, every other pin is shared.
pin_owner() {
  case $1 in
    E2E_CLIENTS_*) echo npm ;;
    E2E_NUGETCLIENTS_*|NUGETVECTORS_*|NUGET_VERSIONING_*) echo nuget ;;
    E2E_OCICLIENTS_*) echo oci ;;
    E2E_APTCLIENTS_*) echo deb ;;
    E2E_ANSIBLECLIENTS_*) echo ansible ;;
    *) echo shared ;;
  esac
}

# The lists, read once.
L_npm=$(format_paths npm)
L_nuget=$(format_paths nuget)
L_oci=$(format_paths oci)
L_deb=$(format_paths deb)
L_ansible=$(format_paths ansible)
L_shared_inside=$(shared_inside_format)
L_none=$(no_format_leg)
L_none_except=$(no_format_leg_except)
L_web=$(web_no_format_leg)
L_bastion=$(bastion_paths)
L_vuln=$(vuln_paths)

# matches <patterns> <path>: whether one of the patterns (one per line) matches the path.
matches() {
  local re
  while IFS= read -r re; do
    [ -n "$re" ] || continue
    if [[ $2 =~ $re ]]; then
      return 0
    fi
  done <<< "$1"
  return 1
}

# format_list <format>: sets LIST to the patterns of the format.
format_list() {
  case $1 in
    npm) LIST=$L_npm ;;
    nuget) LIST=$L_nuget ;;
    oci) LIST=$L_oci ;;
    deb) LIST=$L_deb ;;
    ansible) LIST=$L_ansible ;;
  esac
}

# classify <path>: sets FORMATS_OF to the format legs the path starts, "shared" or "none", and OTHERS_OF to bastion and vuln where
# their lists name it.
classify() {
  local p=$1 f
  FORMATS_OF=""
  OTHERS_OF=""
  if matches "$L_none" "$p" && ! matches "$L_none_except" "$p"; then
    FORMATS_OF=none
  elif matches "$L_shared_inside" "$p"; then
    FORMATS_OF=shared
  else
    for f in $FORMATS; do
      format_list "$f"
      if matches "$LIST" "$p"; then
        FORMATS_OF="$FORMATS_OF${FORMATS_OF:+ }$f"
      fi
    done
    if [ -z "$FORMATS_OF" ] && matches "$L_web" "$p"; then
      FORMATS_OF=none
    fi
    FORMATS_OF=${FORMATS_OF:-shared}
  fi
  if matches "$L_bastion" "$p"; then OTHERS_OF=bastion; fi
  if matches "$L_vuln" "$p"; then OTHERS_OF="$OTHERS_OF${OTHERS_OF:+ }vuln"; fi
  return 0
}

# line_owners <workflow|pins> <file>: one owner per line of the file, in order.
line_owners() {
  local kind=$1 file=$2 name
  case $kind in
    workflow)
      awk '
        { line[NR] = $0 }
        END {
          jobs = 0
          for (i = 1; i <= NR; i++) if (line[i] ~ /^jobs:[ \t]*$/) { jobs = i; break }
          if (jobs > 0) {
            for (i = jobs + 1; i <= NR; i++) {
              if (line[i] ~ /^  [A-Za-z0-9_-]+:[ \t]*$/) {
                key = line[i]; sub(/^  /, "", key); sub(/:.*/, "", key)
                s = i
                while (s - 1 > jobs && line[s - 1] ~ /^  #/) s--
                start[s] = key
              }
            }
          }
          cur = "header"
          for (i = 1; i <= NR; i++) { if (i in start) cur = start[i]; print cur }
        }' "$file" | while IFS= read -r name; do workflow_job_owner "$name"; done
      ;;
    pins)
      awk '
        { line[NR] = $0 }
        END {
          below = ""
          for (i = NR; i >= 1; i--) {
            if (line[i] ~ /^[A-Za-z_][A-Za-z0-9_]*=/) { name = line[i]; sub(/=.*/, "", name); own[i] = name; below = name }
            else if (line[i] ~ /^[ \t]*(#|$)/) own[i] = below
            else { own[i] = ""; below = "" }
          }
          for (i = 1; i <= NR; i++) print own[i]
        }' "$file" | while IFS= read -r name; do
          if [ -n "$name" ]; then pin_owner "$name"; else echo shared; fi
        done
      ;;
  esac
}

# lines_of <ranges> <owners>: the owners at the lines of the ranges ("<first> <count>" per line, an empty count meaning 1).
lines_of() {
  awk 'NR == FNR { n = ($2 == "" ? 1 : $2); for (i = $1; i < $1 + n; i++) want[i] = 1; next } (FNR in want)' "$1" "$2"
}

# changed_line_owners <workflow|pins> <path>: the owners of the lines the change touched in <path>, old side and new side, one per
# line, each once.
changed_line_owners() {
  local kind=$1 path=$2 dir
  dir=$(mktemp -d)
  git show "$BEFORE:$path" > "$dir/old" 2>/dev/null || : > "$dir/old"
  git show "$HEAD:$path" > "$dir/new" 2>/dev/null || : > "$dir/new"
  git diff --no-color --no-ext-diff -U0 "$BEFORE" "$HEAD" -- "$path" > "$dir/diff"
  sed -n -E 's/^@@ -([0-9]+)(,([0-9]+))? \+([0-9]+)(,([0-9]+))? @@.*/\1 \3/p' "$dir/diff" > "$dir/old.ranges"
  sed -n -E 's/^@@ -([0-9]+)(,([0-9]+))? \+([0-9]+)(,([0-9]+))? @@.*/\4 \6/p' "$dir/diff" > "$dir/new.ranges"
  line_owners "$kind" "$dir/old" > "$dir/old.owners"
  line_owners "$kind" "$dir/new" > "$dir/new.owners"
  { lines_of "$dir/old.ranges" "$dir/old.owners"; lines_of "$dir/new.ranges" "$dir/new.owners"; } | sort -u
  rm -rf "$dir"
}

case "${1:-}" in
  --lists)
    for f in $FORMATS; do
      format_list "$f"
      printf '%s\n' "$LIST" | sed "s/^/$f	/"
    done
    printf '%s\n' "$L_shared_inside" | sed 's/^/shared-inside-format	/'
    printf '%s\n' "$L_none" | sed 's/^/no-format-leg	/'
    printf '%s\n' "$L_none_except" | sed 's/^/no-format-leg-except	/'
    printf '%s\n' "$L_web" | sed 's/^/no-format-leg-unless-listed	/'
    printf '%s\n' "$L_bastion" | sed 's/^/bastion	/'
    printf '%s\n' "$L_vuln" | sed 's/^/vuln	/'
    exit 0
    ;;
  --classify)
    while IFS= read -r p; do
      [ -n "$p" ] || continue
      case $p in
        .github/workflows/release.yml | tools/versions.env)
          printf '%s\t%s\n' "$p" "by changed line"
          continue
          ;;
      esac
      classify "$p"
      printf '%s\t%s%s\n' "$p" "$FORMATS_OF" "${OTHERS_OF:+ + $OTHERS_OF}"
    done
    exit 0
    ;;
  "") ;;
  *)
    echo "usage: e2e-legs.sh [--classify | --lists]" >&2
    exit 2
    ;;
esac

: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is not set}"
EVENT=${EVENT:-}
BEFORE=${BEFORE:-}
HEAD=${HEAD:-}

say() {
  echo "$1"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then echo "$1" >> "$GITHUB_STEP_SUMMARY"; fi
}

# finish <legs that run> <reason>: the outputs, written once, and the summary; then the script ends.
finish() {
  local started=" $1 " leg value
  trap - ERR
  for leg in $LEGS; do
    case $started in *" $leg "*) value=true ;; *) value=false ;; esac
    echo "$leg=$value" >> "$GITHUB_OUTPUT"
  done
  say "$2"
  say ""
  say "| e2e leg | runs |"
  say "|---|---|"
  for leg in $LEGS; do
    case $started in *" $leg "*) value=yes ;; *) value=no ;; esac
    say "| $leg | $value |"
  done
  exit 0
}

# An error anywhere runs every leg. Inside a command substitution the subshell only fails, so that the shell around it fails open
# once.
fail_open() {
  if [ "${BASHPID:-$$}" != "$$" ]; then
    exit 1
  fi
  echo "::warning::the decision which e2e legs run failed ($1): every leg runs"
  finish "$LEGS" "The decision which e2e legs run failed ($1), so every e2e leg runs."
}
trap 'fail_open "line $LINENO: $BASH_COMMAND"' ERR

case $EVENT in
  push | pull_request) ;;
  *) finish "$LEGS" "A run of the event ${EVENT:-<none>} runs every e2e leg: only a push and a pull request are judged by their paths." ;;
esac
if [ -z "$BEFORE" ] || [ "$BEFORE" = "0000000000000000000000000000000000000000" ] || ! git cat-file -e "${BEFORE}^{commit}" 2>/dev/null; then
  finish "$LEGS" "The $EVENT has no commit to compare with (before: ${BEFORE:-<none>}), so every e2e leg runs."
fi
git cat-file -e "${HEAD}^{commit}"
if [ "$EVENT" = pull_request ]; then
  # What the pull request changed: its head against the merge base with its base, not the base branch's own commits since.
  base=$BEFORE
  BEFORE=$(git merge-base "$base" "$HEAD")
  echo "The pull request is judged from the merge base ${BEFORE:0:12} of its base ${base:0:12} and its head ${HEAD:0:12}."
fi

paths=$(git diff --no-renames --name-only "$BEFORE" "$HEAD")
started=""
log=""

# start <legs> <what started them>
start() {
  local leg these=""
  for leg in $1; do
    case " $these " in *" $leg "*) ;; *) these="$these${these:+ }$leg" ;; esac
    case " $started " in *" $leg "*) ;; *) started="$started${started:+ }$leg" ;; esac
  done
  log="$log$2 -> ${these:-no leg}"$'\n'
}

count=0
while IFS= read -r p; do
  [ -n "$p" ] || continue
  count=$((count + 1))
  case $p in
    .github/workflows/release.yml) kind=workflow ;;
    tools/versions.env) kind=pins ;;
    *) kind="" ;;
  esac
  if [ -n "$kind" ]; then
    owners=$(changed_line_owners "$kind" "$p")
    legs=""
    for o in $owners; do
      case $o in
        none) ;;
        shared) legs="$legs $REFERENCE" ;;
        *) legs="$legs $o" ;;
      esac
    done
    start "$legs" "$p (changed lines of: $(echo $owners))"
    continue
  fi
  classify "$p"
  case $FORMATS_OF in
    none) start "$OTHERS_OF" "$p" ;;
    shared) start "$REFERENCE $OTHERS_OF" "$p (shared code)" ;;
    *) start "$FORMATS_OF $OTHERS_OF" "$p" ;;
  esac
done <<< "$paths"

echo "The changed paths and the legs they start:"
printf '%s' "$log"
ordered=""
for leg in $LEGS; do
  case " $started " in *" $leg "*) ordered="$ordered${ordered:+ }$leg" ;; esac
done
finish "$started" "The $EVENT changed $count paths from ${BEFORE:0:12} to ${HEAD:0:12}. The e2e legs that run: ${ordered:-none}. The log of this step names the legs each path starts."
