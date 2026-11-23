# Coverage generator

The coverage pages in [docs/coverage/](../coverage/README.md) are generated from the feature catalog
and the specification by one Go program, [internal/tools/coverage](../../internal/tools/coverage/main.go)
(roadmap WP0.9, exit criterion E0.8). The same program writes the
[index of decided questions](../decisions.md#decisions-by-the-development-agent) in `docs/decisions.md`
([below](#the-index-of-decided-questions)). It uses only the standard library. The rules it enforces and
the numbers it produces are described for readers in
[How the numbers are produced](../coverage/README.md#how-the-numbers-are-produced); this page is
for people who change or run it.

## Modes

| Command | What it does | Exit code |
|---|---|---|
| `go run ./internal/tools/coverage` | Regenerates [checklist.md](../coverage/checklist.md), the four marked blocks of [coverage/README.md](../coverage/README.md) and the index of decided questions in [decisions.md](../decisions.md#decisions-by-the-development-agent). It renders what the specification covers and no implementation status: `main` holds no evidence record, and the pages point to the record on the branch `coverage` ([below](#the-record-on-the-branch-coverage)). It refuses a tree that holds `docs/coverage/implemented.txt` or `.github/badges/coverage.json`. | 0, or 1 on a rule violation or a record or badge in the tree |
| `go run ./internal/tools/coverage -check` | Writes nothing. Fails when a generated file differs from what would be generated, and names each question whose row in the index is missing, has no block or no longer matches its block; refuses a record or a badge in the tree like the plain run. This is the `git diff --exit-code` of E0.8 without needing git. | 0, 1 rule violation, 2 stale file |
| `go run ./internal/tools/coverage -tests <report> [-carried <report>] [-previous <implemented.txt>]` | The pipeline's form: writes `implemented.txt` from the reports and `checklist.md` and the README blocks rendered from it, with the implementation status. With `-previous` (the record on the branch `coverage`) it lists the requirements the record adds (`newly proven`) and the ones it drops (`no longer proven`), each with its reason; it fails on neither ([Q-ARCH-811](../spec/architecture.md#q-arch-811--the-pipeline-writes-the-evidence-record-and-the-coverage-badge-onto-the-branch-coverage-main-holds-neither)). Locally it shows what a set of reports proves; its output is never committed to `main`. | 0, 1 rule violation, unknown ID or cut report |
| `go run ./internal/tools/coverage -links` | Checks every relative Markdown link and `#anchor` in the repository. Replaces the interim `check_links.py` and `check_anchors.py`. | 0, or 1 on a broken link |

Flags:

| Flag | Meaning |
|---|---|
| `-repo <dir>` | Repository root. Default: the nearest parent directory of the working directory that holds `go.mod`. |
| `-check` | Compare only, write nothing. Does not combine with `-tests`. |
| `-tests <report>` | A test report; repeatable. File arguments after the flags count as reports too. `-` reads standard input. |
| `-carried <report>` | A report of an earlier run of a leg that has no whole report in this one; repeatable; needs `-tests`. It proves and vetoes like a `-tests` report; the output says when only its failing test drops a requirement. |
| `-previous <implemented.txt>` | The record published last; needs `-tests`. The output lists what the new record adds and drops. |
| `-links` | Link and anchor check instead of generation. Does not combine with `-check`, `-tests`, `-carried` or `-previous`. |

Usage errors exit with 64. The generator prints the counts per priority, the number of `Covers:`
references, the implemented counts and every heading it skipped inside a code fence.

## Inputs and rules

| Input | Read for |
|---|---|
| `docs/features/**/*.md` | `### <ID> · <Title>` with `Priority: **<P>**` directly below. Page order follows the page table of [features/README.md](../features/README.md). |
| `docs/spec/**/*.md` | `### R-… · <Title>` with `Priority: **<P>** · Covers: <ID>, …` or `Covers: quader-only` directly below. |
| `docs/**/*.md` except `features/` | `### Q-… · …` and `### H-<n> · …` headings, for the uniqueness check; each `Q-` heading with its block for the index of decided questions. |
| `docs/decisions.md` | The hand-written page around the marker pair `<!-- coverage-gen:begin decisions -->` and `<!-- coverage-gen:end decisions -->`, kept untouched; its entries `### ADR-<n> · …` for the index's ADR column. |
| Test reports (`-tests`, `-carried`) | The pipeline's form only: the requirements a passing test names. `main` holds no `docs/coverage/implemented.txt`; the record lives on the branch `coverage`. |
| `docs/coverage/README.md` | The prose around the marker pairs `<!-- coverage-gen:begin NAME -->` and `<!-- coverage-gen:end NAME -->`, kept untouched. |

The program exits with 1 and prints every problem (not only the first) for:

- a catalog entry or requirement whose priority line is missing or whose value is not one of
  `MVP`, `P1`, `P2`, `Later`, `Out of scope`;
- a `Covers:` ID that is not in the catalog, a token that is neither an ID nor `quader-only`, an
  ID listed twice, `quader-only` mixed with IDs;
- a duplicate catalog ID, requirement ID, `Q-` ID or `H-` number;
- an unclosed code fence;
- a test name that carries a requirement ID no spec page defines, plainly or with the marker `-unbuilt`;
- a `go test -json` report in which a package started and did not end (cut: its job was stopped while the package ran);
- without `-tests`, a `docs/coverage/implemented.txt` or `.github/badges/coverage.json` in the tree;
- a missing marker pair, an audience page without entries;
- a row of the index of decided questions (`| [Q-…](…) |`) in `docs/decisions.md` outside its marker pair.

Lines inside fenced code blocks are ignored by all of these, which is why the format example in
[spec/README.md](../spec/README.md) can reuse `R-NPM-012`. Each fixture for a rule is a small docs
tree under [testdata/](../../internal/tools/coverage/testdata/), and the test that uses it is named
after E0.8 in [coverage_test.go](../../internal/tools/coverage/coverage_test.go).

## Implementation status: the test-naming rule

This is the rule of [Q-ROAD-4](../coverage/roadmap.md#q-road-4--how-the-coverage-generator-learns-implementation-status),
with one decision on top (below).

- A test names a requirement by carrying its ID in the test name. In Go, put it in the subtest
  name; the testing package turns spaces into underscores, and the generator reads
  `TestSomething/R-ARCH-002_forbidden_import_names_the_edge` as naming `R-ARCH-002`. A function
  name cannot hold a hyphen, so the generator also reads the underscore form
  ([Q-ARCH-251](../spec/architecture.md#q-arch-251--the-coverage-generator-reads-a-requirement-id-in-a-go-function-name-in-its-underscore-form)):
  `TestR_API_053_TheGuideOfAFormat` names `R-API-053`.
- A requirement is **implemented** when at least one passing test names it and **no** failing
  test names it. A skipped test is neither.
- A catalog feature is **implemented** when at least one requirement covers it and every
  requirement that covers it is implemented. Otherwise it stays *specified*.
- The ID must stand alone in the name. The characters next to it may be anything but a letter,
  digit or hyphen: `R-ARCH-002_x`, `/R-ARCH-002 x` and `R-ARCH-002/2` name it; `PR-ARCH-002` and
  `R-ARCH-002-x` do not. Only `R-` IDs count.
- The underscore form is `R_<AREA>_<digits>` (`idsIn`, `underscoreID` in
  [evidence.go](../../internal/tools/coverage/evidence.go)): an upper-case area of one segment and one
  number, so `TestR_AUTH_027_E1_2_Token` names `R-AUTH-027` and nothing else. It follows the same
  standalone rule, and may also stand directly after `Test`, `Benchmark`, `Fuzz` or `Example` at the
  start of a function name or a subtest, and next to underscores (`TestName_R_API_053`).
  `PR_API_053`, `TestPR_API_053`, `TestR_API_053x` and `TestR_api_053` name nothing. An ID the spec
  does not define is reported as unknown, in either form. `TestIDsInTestNames` in
  [coverage_test.go](../../internal/tools/coverage/coverage_test.go) holds the cases. A criterion suffix such as `/2` is ignored today;
  Q-ROAD-4 names it as the answer if one name per requirement proves too coarse.

```go
func TestImportRules(t *testing.T) {
	t.Run("R-ARCH-002 forbidden import names the edge", func(t *testing.T) {
		// ...
	})
}
```

A test that verifies several requirements carries several IDs in one name
(`R-HA-002 R-HA-023 worker count`). The test names of the generator's own tests must not contain
sample IDs, because CI feeds them to the generator too.

**A test that holds a refusal** names the requirement it holds plainly and the requirement that is not built with the marker
`-unbuilt` directly after the ID, `_unbuilt` in a Go function name
([Q-ARCH-811](../spec/architecture.md#q-arch-811--the-pipeline-writes-the-evidence-record-and-the-coverage-badge-onto-the-branch-coverage-main-holds-neither)):
the search case of the npm client matrix is `search R-NPM-037-unbuilt R-NPM-043 R-NPM-045`, so it proves R-NPM-043 (an unimplemented
endpoint answers predictably) and R-NPM-045, and keeps R-NPM-037 (search) out of the record while it passes. A failing refusal test says
nothing about the marked requirement (its refusal may be gone because the requirement was built); a marked ID the specification does not
define fails like any unknown ID. When the requirement is built, the test changes: it asserts the behaviour and names the ID plainly.
`unbuiltIn` in [evidence.go](../../internal/tools/coverage/evidence.go) reads the marker; the cases are in `TestEvidence`.

### Why a failing test vetoes

Decision by the development agent: a requirement with a passing and a failing test that name it is
not implemented.

| Option | Effect | For | Against |
|---|---|---|---|
| Passing evidence alone | One green test marks the requirement. | Simple. | A regression hides behind an older green test, which is the drift Q-ROAD-4 exists to prevent. |
| **A failing test vetoes** | Any red test that names the ID withdraws the mark. | Status follows the worst evidence. A red `main` shows as a drop in the numbers. | A flaky test flickers the status. |

Revisit if flaky tests make the numbers unusable; the fix then belongs in the test, not in the
generator.

## Report formats

`-tests` detects the format from the first non-blank character of each file.

| Format | Detected by | Reading |
|---|---|---|
| `go test -json` stream | anything but `<` | One event per line. `pass` and `fail` events with a `Test` field count; `skip` does not. Lines that are not JSON objects (build output mixed in) are skipped; a file without a single event is an error. |
| JUnit XML | `<` | Every `<testcase>` (any nesting, `<testsuites>` or `<testsuite>` at the root). `<failure>` or `<error>` child: failed. `<skipped>` child: skipped. Otherwise passed. IDs are searched in `classname` and `name`. |

What CI produces, by tier:

| Tier | Report | Written by | Artifact |
|---|---|---|---|
| Go unit | `go test -json` | `make test` | `test-unit` |
| Angular unit (Vitest) | JUnit, `.out/test-web.xml` | `ng test` through the reporters in [angular.json](../../web/angular.json) | `test-web` |
| Angular lint rules (R-UI-046) | JUnit, `.out/test-web-lint.xml` | `npm run test:lint` ([lint-rules.test.mjs](../../web/scripts/lint-rules.test.mjs), `node --test`) | `test-web` |
| Chart and promtool | `go test -json` | `make chart-test` | `test-chart` |
| Integration | `go test -json` | `make test-integration` | `test-integration-a`, `-b`, `-c`, `-d` |
| Kind legs (Go e2e sets, with the Playwright journeys as subtests named by their titles) | `go test -json`, `.out/test-e2e.json` | `make e2e SET=…` ([e2e.sh](../../deploy/dev/e2e.sh)) | `e2e-report-<set>`, `e2e-<group>-report-<set>`, `e2e-backup-report` |
| Nightly latency gates | `go test -json` | `make test-performance` | `test-performance` |

The Playwright journeys are run by a Go test (`TestE1_11Journeys` and the bastion, vulnerability, mirror and upgrade counterparts), which
reads Playwright's JSON report and turns every spec into a subtest named by the spec's title. The Playwright configurations therefore need no
reporter of their own, and a journey that names a requirement in its title is evidence once the Go test that ran it is in a report. The
kind-leg reports are kept 30 days.

Pass **all** reports of a run in one invocation. `implemented.txt` is recomputed from exactly the
reports given, so a run that omits the e2e report drops the requirements only e2e proves.

## How CI feeds it

| Where | Command | Purpose |
|---|---|---|
| Every pull request | `go run ./internal/tools/coverage -check` | Rule violations, generated files that the change did not regenerate, and a record or a badge added to the tree. |
| Every pull request | `go run ./internal/tools/coverage -links` | Broken relative links and anchors. |
| After the test jobs and the kind legs (`coverage evidence`) | `go run ./internal/tools/coverage -tests …` over the unit, Angular, chart and integration reports and the whole kind-leg reports of the run, `-carried …` over the report of every kind leg without one, and `-previous` over the record on the branch `coverage` | The record written from the reports, uploaded as `coverage-evidence` (14 days); the job summary lists what it adds to and drops from the branch's record, with the reasons. |
| After the release (`Semantic Release`) | [publish-coverage-record.sh](../../tools/publish-coverage-record.sh) | Publishes the newest record and badge onto the branch `coverage` ([below](#the-record-on-the-branch-coverage)). |

The job `coverage evidence` runs whenever unit, Angular, chart and every integration shard passed, whatever became of the kind legs: a
leg that was skipped, red or cancelled does not cost the run its record
([Q-ARCH-811](../spec/architecture.md#q-arch-811--the-pipeline-writes-the-evidence-record-and-the-coverage-badge-onto-the-branch-coverage-main-holds-neither)).
The four tiers it requires run on every push, so a red one costs one record and the next run brings it.

The kind legs are path-filtered per change and the long legs run in the nightly workflow, so a run holds only some of the end-to-end
reports. [fetch-e2e-reports.sh](../../tools/fetch-e2e-reports.sh) fills the rest
([Q-ROAD-11](../coverage/roadmap.md#q-road-11--the-evidence-record-reads-the-reports-of-every-test-tier), Q-ARCH-811):

- **This run's reports that are not whole are set aside.** A report is whole when every package it started also ended; a leg stopped
  while a package ran (its job's limit, a lost runner) uploads the tests it reached, and the ones it did not reach would leave the record.
  Its leg is then carried like a leg that did not run. The generator refuses a cut report too, as a second guard.
- **A carried report is the one made for the nearest commit.** The script lists the last 40 completed runs of Test and Release and the
  last 14 of the nightly on `main` through the Actions API (the job holds `actions: read` and checks out the whole history), keeps those of
  this repository started by a push, a schedule or a dispatch and not cancelled (a person can cancel a run of `main`, and its legs cut off
  mid-set uploaded partial reports), and of those the runs whose commit is this run's commit or an ancestor of it. For each leg without a
  whole report here it takes the report of the nearest such commit, of one commit the newest run, and passes over one that is not whole.
  Not the newest by creation time: with runs of `main` side by side an older commit's run can finish later, and a newer commit's report
  (run 45159763759 carried the vulnerability report of the later commit of 45160021052) must not enter an older commit's record.
- **A carried report counts for a week** (`QUADER_E2E_REPORT_MAX_AGE_HOURS`, 168 hours since its run started). The nightly runs every leg
  daily; a week leaves room for nightlies lost in a row and for the legs that run on a dispatch alone. A leg whose reports are all older
  proves nothing until it runs again, and the output names it (`too old: …`). So a requirement whose test was deleted leaves the record
  with the next run of its leg, at the latest a week later.

A failing test in a carried report vetoes its requirement as in a report of this run, and so does a failing test of a red leg of this
run; the job fails on neither, so a red long leg holds nothing (owner rule of 2026-11-01). The job summary lists what was carried, from
which run and commit and how many commits back, and each requirement the record drops with its reason: a failing test of this run, a
failing test of a carried report, a passing test that holds its refusal (`-unbuilt`), no longer a requirement, or no report that names it
in a passing test (the test was removed, renamed or skipped, or its leg has no report young enough). To see the effect locally, download
the reports of a run (`gh run download <run> -p 'test-*' -D <dir>`) and the carried ones with
`tools/fetch-e2e-reports.sh <dir of this run's e2e reports> <destination>` in a checkout with the history (it needs `GITHUB_TOKEN`,
`GITHUB_REPOSITORY`, `GITHUB_RUN_ID` and, for another commit than `HEAD`, `GITHUB_SHA`), then run the generator with the `-tests` and
`-carried` flags in a scratch copy of the tree: its output is never committed to `main`.

### The record on the branch `coverage`

Until [Q-ARCH-457](../spec/architecture.md#q-arch-457--until-the-first-stable-release-a-minimal-set-of-jobs-gates-semantic-release)
(2026-11-10) Semantic Release committed the record, `checklist.md`, the generated blocks of the coverage README and the badge to `main`
with the release (`chore(release): <version> [skip ci]`); the last such commit is 0.34.1 (d3cf8d660). From then to 2026-11-23 no release
committed them, the committed file moved only by hand (eleven commits of 2026-11-19 to 2026-11-22 added 38 IDs and removed one), and it held
657 requirements against the 1,059 of the pipeline's record of Test and Release 45133371730.

Since [Q-ARCH-811](../spec/architecture.md#q-arch-811--the-pipeline-writes-the-evidence-record-and-the-coverage-badge-onto-the-branch-coverage-main-holds-neither)
(2026-11-23) the pipeline is the only source of the record and the badge, and `main` holds neither:

- **The job Semantic Release publishes them after the release**, on every push run of `main` that reaches it, and waits for neither:
  [publish-coverage-record.sh](../../tools/publish-coverage-record.sh) takes the artifact `coverage-evidence` and the artifact
  `coverage-badge`, each from the completed push run of Test and Release whose commit is the released one or its nearest ancestor that has
  it. The release's own run is still going, so the published record is usually that of the push before. A run of a commit that is no
  ancestor is never a source, so the record never runs ahead of the commit it is published for.
- **The branch gets one commit per newer record or badge**: subject `chore(coverage): the evidence record of <commit> and the coverage
  badge of <commit> [skip ci]`, trailers `Record-Commit`, `Record-Run`, `Badge-Commit`, `Badge-Run` and `Released-Commit`; its tree is the
  record's commit with `docs/coverage/implemented.txt`, `checklist.md`, `README.md` and `.github/badges/coverage.json`, so every relative
  link of the pages resolves on the branch; its parents are the branch's head and the record's commit, so the push is a fast-forward. It is
  never forced. Only what is newer than what the branch holds is published, by ancestry, so an older run's job never moves it back.
- **Nothing holds or fails the release.** A missing, older or unreadable record and an API that does not answer leave the branch as it is
  and say so in the job's log; a push refused because another job pushed first is decided again on the new head, three times at most,
  then given up with a warning for the next release; the step has `continue-on-error`.
- **Landings change nothing.** The pipeline never commits to `main`, so no landing is rejected by it and no rebase meets it. A builder does
  not add or edit `docs/coverage/implemented.txt` or `.github/badges/coverage.json`: the generator refuses either in the tree, in the plain
  run and in the gating `-check`. A change to the catalog or the spec reruns `go run ./internal/tools/coverage` (`make coverage`), which
  renders `main`'s pages without an implementation status. A test that is removed or renamed on purpose needs nothing more: its
  requirement leaves the record with the next run that does not prove it, and the job summary names it.
- **Reading the record:** `https://github.com/guided-traffic/quader/blob/coverage/docs/coverage/README.md`, or in a checkout
  `git fetch origin coverage` and then `git show origin/coverage:docs/coverage/implemented.txt`. The README's badge reads
  `.github/badges/coverage.json` of the branch.

### Why CI writes the record

Decided by the owner (2026-11-07), on the development agent's proposal.

| Option | Effect | For | Against |
|---|---|---|---|
| The committed record must equal the reports of the run (`-check -tests`) | A push whose tests prove a requirement for the first time fails until the record is refreshed and pushed again | The record is exact on every commit | Every such push costs a second pipeline run, and the refresh needs every test tier locally (2.5 h on 2026-11-07, and still stale: the test binary was older than the last commit). 4 of 11 red runs of 2026-11-06/07 were this job |
| **CI writes the record, Semantic Release commits it; the check fails only on an unproven claim (`-claims`)** | The record is written from the run that releases; a claim without a passing test fails the run | No local evidence run, no second pipeline run; the record never claims more than the tests prove | The record lags behind a push that makes no release, until the next release |
| CI commits the record on every push | Exact after every push | — | A bot commit after every push, and a pipeline run for each |

The first half of the chosen row holds. Its second half is replaced by Q-ARCH-811: the release job publishes the record onto the branch
`coverage` instead of committing it to `main`, and no check fails on a claim, because `main` holds none to compare.

## The index of decided questions

The part of [decisions.md](../decisions.md#decisions-by-the-development-agent) between the marker pair of the block
`decisions` is generated, in the same run as the coverage pages ([decisions.go](../../internal/tools/coverage/decisions.go)).
It is not a flag of its own: a builder already runs `go run ./internal/tools/coverage` (or `make coverage`), CI's
`-check` already compares every generated file, and a conflict in the generated part after a rebase is resolved by that
one command on the rebased tree, whichever file the conflict is in.

- **One row per block.** Every `### Q-<AREA>-<n> · <title>` heading outside a code fence on a page below `docs/`, except
  the catalog, gets one row: the ID linked to the heading, the title, the status, the choice and the ADRs.
- **Order.** One section `### Q-<AREA>` per area, the areas in byte order, the rows by number (`Q-UI-9` before
  `Q-UI-10`); a section of more than 15 rows is collapsed in `<details>`. Two runs give the same bytes.
- **Status.** The value of the block's `Status:` line: its bold part with the words that continue it (`**Superseded** by
  [Q-…]`), or without a bold part the text before its first dash or sentence end; `—` for a block without a status line.
- **Choice.** The first sentence of the block's `**Choice:**` paragraph, up to its end or the next bold label such as
  `**Rationale:**`. When that sentence names only option letters (`(b).`, `(b) and (iii).`), the row shows those options'
  first cells from the block's options table, without `(chosen)`. Without a choice line, a status line that names the
  option (`— option (a), as recommended`) is read the same way, and else the options row marked `(chosen)`; `—` when the
  block has none of these (a table of several decisions, a superseded question).
- **ADR.** Every `ADR-<n>` the block names, and every entry of the hand-written part whose text names the question, linked
  to the entry, in number order; `—` for none. A block that belongs to an entry and is not named by it says so in a line
  of its own (`Its entry in the decision log: [ADR-…](…).`).
- **Links.** A relative link of the block is rewritten to resolve from `docs/decisions.md`; a link into `decisions.md`
  becomes its anchor. Inline code is left as it is, and a pipe is escaped.

What a row says comes from the block only: a correction, a later revision or an ADR that belongs to a question is written
on the question's page, and the next run carries it into the row. `-check` prints, before the stale file, one line per
differing row (at most 20, then a count), for example:

```text
docs/decisions.md: Q-SIG-145 (docs/spec/signatures.md:5632) has no row in the index of decided questions
docs/decisions.md: the index is generated: run `go run ./internal/tools/coverage` and commit the result
stale: docs/decisions.md (run the generator and commit the result)
```

## Format changes against the interim script

The output is byte-identical to the interim Python script's, with two intended differences.

- The *Implemented* row of the headline table in [coverage/README.md](../coverage/README.md) has the
  kind "measured from passing tests that name requirements (Q-ROAD-4)" where the script wrote "fact,
  not computed: the repository holds no code yet", which stopped being true with the first Go file.
- Once a requirement is implemented, `checklist.md` marks it with ` (implemented)`, adds an
  *Implemented* column to the page table and rewrites the status paragraph. With an empty
  record none of this appears. Without a record (`main`, Q-ARCH-811) the status paragraph and the headline's
  *Implemented* row point to the record on the branch `coverage`, and the README's two *Implemented* columns are left out.

The link check differs from the scripts in three ways: it skips fenced code and inline code per line
instead of with a multi-line regular expression, numbers repeated headings the way the Markdown
hosts do (`#same`, `#same-1`), and does not descend into `node_modules`, `.git` or `testdata`.

## Files

| File | Responsibility |
|---|---|
| [main.go](../../internal/tools/coverage/main.go) | Flags, modes, exit codes, writing and comparing files. |
| [parse.go](../../internal/tools/coverage/parse.go) | Fence-aware reading of catalog and spec, the rule checks, ID uniqueness. |
| [model.go](../../internal/tools/coverage/model.go) | Pages, counts, feature status, page order, heading anchors. |
| [evidence.go](../../internal/tools/coverage/evidence.go) | Test reports and whether one is cut, ID extraction from test names and the marker `-unbuilt`, `implemented.txt`, the reason a requirement leaves the record. |
| [render.go](../../internal/tools/coverage/render.go) | `checklist.md`. |
| [readme.go](../../internal/tools/coverage/readme.go) | The four marked blocks of `coverage/README.md`, the audience page lists. |
| [links.go](../../internal/tools/coverage/links.go) | The `-links` mode, heading anchors. |
| [decisions.go](../../internal/tools/coverage/decisions.go) | The index of decided questions in `decisions.md` and the rows `-check` names. |

To change the audience page lists, edit `audiences()` in `readme.go`; the generator fails when a
listed page has no entries.
