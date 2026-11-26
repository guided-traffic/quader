# What a format costs in CI today

Measured on 2026-11-20 on two pushes to `main` and one full nightly, as the first step of making the test suite fit about twenty more formats ([CLAUDE.md](../../CLAUDE.md), "Tests stay runnable"). It is a measurement, not a test: it was run once, its numbers stand here, and nothing keeps them current. Nothing was started, re-run or changed for it. The scripts and the raw downloads that produced the numbers were working files and are not kept in the repository; a name in backticks such as `cats.py` or `data/e2e_tests.csv` names one of them, and [section 1](#1-method-repeatable) gives the method to repeat the measurement. "I" in the text is the development agent that measured.

Marks used throughout. **[V]** read directly from a job record, a `go test -json` stream, a Playwright `results.json` or a log. **[I]** computed from [V] values by a stated rule. **[J]** a judgement; the text says what was read to decide it.

## 0. The result in short

All figures are runner minutes (the wall clock of each job, summed) unless stated.

* **Push (run 2, 44693590111, commit 4913688f):** 396.0 runner min in 51.7 min wall [V]. Run 1 (44646960056): 384.9 min in 46.9 min wall [V]. Of run 2: ten e2e legs 172.6, three integration shards 113.3, UI layout 32.7, unit tests 12.3, the other 21 jobs 65.3.
* **Nightly (44625277245, dispatch of all legs):** 682.9 runner min in 299.2 min wall [V]; 25 e2e legs 658.1 [V], plus the four platform-matrix jobs 4.9, the image job 4.0, Latency 9.1 and the vulnerability perf job 6.8 (section 2.2).
* **55 % of every push's e2e minutes are not test bodies** [I]: of the 172.6 e2e min in run 2, 65.9 are set-up steps, 26.6 are `make e2e` outside the listed tests (cluster profile switch, client pods, test upstreams; inferred as step time minus the sum of the top-level tests), 2.7 are teardown and uploads = 95.1 min (unrounded). In the nightly it is 240.6 of 658.1 min (36.6 %). Bringing up the kind cluster and the dev profile alone (`make dev-up`) is 154 to 186 s per leg: 25.8 min per push in 9 legs, 67.5 min per nightly in 25 legs [V].
* **A format costs, per push, 21.2 to 38.0 runner min today** (run 2; mean of the five with a client 28.6, of all six 23.9), and, per nightly, 0 to 99.1 min (mean of six 24.6). Of the 28.6 push minutes of the mean format, 12.6 are infra, 8.7 repeated and only 7.2 client (tables in section 4).
* **The cost does not stay constant:** with the five-format mean a push at 25 formats is about 939 runner min (now 396.0) and the nightly about 1244 (now 682.9); integration shard b, where a new package lands, passes its 45-minute job timeout with the first additional average format (40.9 min now, +6.0 min per format) [I]. Since then the shards are cut by a rule on the area's name, three by Q-ARCH-544 and four by Q-ARCH-565, from the package times of [section 8](#8-integration-shards-on-2026-11-21-run-44948103795).

## 1. Method (repeatable)

1. Run lists and artifact lists: `gh api repos/guided-traffic/quader/actions/runs/<id>/jobs --paginate`, `.../runs/<id>/artifacts --paginate`, `.../runs/<id>` (files `data/jobs_<id>.json`, `art_<id>.json`, `run_<id>.json`). A job's runner minutes are `completed_at - started_at` of the job; a step's are the same on the step; summed over all non-skipped jobs they reproduce the owner's 384 (run 1: 384.9) and 682 (nightly: 682.9) [V]. Queue time (`created_at` to `started_at`) is not counted in runner minutes and is reported separately.
2. Reports: `gh run download <id> -R guided-traffic/quader -p 'test-*' -p 'e2e-*report*' -p 'e2e-report-*' -p 'ui-layout-report*' -p 'oci-conformance-junit' -D artifacts/<id>` (nightly: `e2e-*report*`, `test-performance`, `vuln-perf`); Playwright `results.json` from `e2e-playwright-access-long` and `e2e-vuln-playwright-vuln` of run 2. Nothing had expired. The Go reports are parsed for the `pass`/`fail`/`skip` event of every test (`Elapsed`), top-level tests and their direct subtests (`gotests.py`, `pkgs.py`).
3. Step classes (`split.py`, `cats.py`): **tests** = steps `make e2e SET=...`, `make test*`, `Render every screen...`, `make oci-conformance`, `make s3-conformance`, `make chart-test`, `make web-test`, perf runs; **teardown** = `make dev-down`, `make integration-down`, cluster-state, every `Upload ...`, `Post ...`, `Complete job`; **infra** = every other step plus the seconds between steps (checkout, Go, Buildx, tool pins, disk free, Playwright install, `make dev-up`, `make integration-up`, image loads, client image build/load).
4. Inside the e2e step: step time minus the sum of the top-level test times is the **`make e2e` outside the tests** (`gap`, [I]: `deploy/dev/e2e.sh` builds the driver, starts client pods and test upstreams; `TestMain` switches the profile). Per top-level test, **seconds outside its subtests** = test time minus the sum of its direct subtests, only where the subtests do not overlap [I]; for format tests I read this as set-up inside the test (seeding, waiting for an index to merge), which I confirmed by reading `TestDebVirtual` (`test/e2e/deb_virtual_test.go`, fixture, uploads, a `waitFor` of up to 5 min on the merged index, then four 1-2 s client subtests); for platform tests the remainder can also be real test work, so it is not used as infra there.
5. Classes of a test (**[J]**, rule): **client** = the test's pass condition is a behaviour of one format's own client or content, named by that format's requirement ids (`R-NPM-*`, `R-NUGET-*`, `R-OCI-*`, `R-DEB-*`, `R-ANS-*`, `R-LIM-020` "what the client shows at a refusal"); **repeated** = the pass condition is a format-independent requirement (`R-REPO-*`, `R-ARCH-*`, `R-EP-*`, `R-AUTHZ-*`, `R-HA-*`, the mirror, virtual, replica, drain, failover, pod-crash scenarios), driven by a format's client or as load; **infra** = a smoke test of the client pods; **once** = a platform feature with no format's client as subject, and no copy elsewhere (not a format cost). Each test row carries the note on which the call rests; the rules are in `classify.py`. The format-contract kit (`internal/formats/formattest/kit.go`, checks 1 to 16 incl. merge, authz paths, admission) exists for every format at the unit tier; that is the evidence that e2e virtual and authz cases of a format are the same behaviour.
6. Integration tests (1,645 top-level tests): classes by test name (`integ.py`, [J]): a test whose name carries a generic requirement (`R_AUTHZ`, `R_ARCH`, `R_HA`, `R_STOR`, `R_EP`, `R_BAK`, `R_API`, `Q_REPO`, replica/valkey/switchover/drain words) is repeated, `R_REPO` without a format word is repeated, every other test of a format's package is client. Packages that are not a format's (api, backup, ci, coordination, mirror, proxy, repository, vuln*, bastion* ...) are format-independent and counted once without a per-test call; mirror and proxy contain some format tests (for example `R_ANS_021`), which is not separated (see section 7).
7. Files: `data/e2e_tests.csv` (every top-level e2e test of the three runs with class, owner, note), `data/integration_tests_run2.csv` (every top-level integration test of run 2), `data/format_cost.json`, `data/cats_*.csv`, `data/split_*.csv`.

## 2. What was measured, and what a leg is

| run | id | event / workflow | commit | start (UTC) | wall | result |
|---|---|---|---|---|---:|---|
| run 1 | 44646960056 | push, Test and Release | 072f4949 feat: a client that asks a virtual repository about a version starts its download at the remote member that serves it | 2026-11-20 00:34:35 | 46.9 min | success |
| run 2 | 44693590111 | push, Test and Release | 4913688f docs: the manual says what npm, dotnet, docker, podman, apt-get and ansible-galaxy show when a download limit refuses them | 2026-11-20 07:15:13 | 51.7 min | success |
| nightly | 44625277245 | workflow_dispatch, HA and Performance (all legs) | 2621e9e9 test: the render seed is no longer refused by the write limit on a new database | 2026-11-19 22:05:50 | 299.2 min | failure (e2e backup, npm-ha-soak, drain-load-pooler; the point-in-time step of backup failed too) |

* **Run 2 is "today".** It contains the per-format download-limit cases (`TestNPMLimits`, `TestNuGetLimits`, `TestOCILimits` x3, `TestOCIProbe`, `TestDebLimits`, `TestAnsibleLimits`; the commit that added them is 4913688f, `git log -- test/e2e/npm_limits_test.go`); run 1 does not. Run 1 also ran while the nightly dispatch still held one or two large runners (nightly legs `drain-load-pooler` 00:20 to 00:45, `ha-pooler` 00:35 to 00:59 and `nuget-ha` 00:59 to 01:29 UTC, from the job start times), which is the likely reason that `e2e (npm)` waited 895 s for a runner in run 1 [V: the queue time; I: the cause]. Large-runner legs may be slower in run 1 for the same reason (not measured).
* **A push runs the format legs that its change touches, but both pushes touched everything.** `e2e-oci`, `e2e-deb`, `e2e-ansible`, `e2e-bastion` and `e2e-vuln` start with a step "Does the change touch ...?" (path list in `.github/workflows/release.yml`; `deploy/dev/e2e.sh`, `internal/mirror/`, `internal/httpserver/`, `deploy/helm/quader/` are on every format's list). In both runs oci, deb, ansible and vuln ran ("the change touches ...: the suite runs" in the job logs 135994995908, 136146450866, 136146450889, 136146450821, 135994996089, 136146451003); `e2e (bastion)` did not and still cost 130 s of runner (117 s "Free disk space" on a GitHub-hosted runner) [V]. `e2e-npm`, `e2e-nuget` and `e2e-ci` have no such step and run on every push [V: workflow]. So the push totals below are the cost of a push that touches shared code, not of one that touches a single format [I].
* **Runner classes [V]:** push run 2: large (6 jobs: npm, oci, deb, ansible, push, access-long; 114.9 min), GitHub-hosted ubuntu-24.04 (ci, nuget, bastion, vuln; 57.6 min), self-hosted (26 jobs; 223.5 min). Nightly: large 19 jobs 445.3 min, GitHub-hosted 12 jobs 233.6 min, self-hosted 1 job 4.0 min. A runner minute is not priced here; the three classes differ.
* **Nightly lanes [V: comment block in `.github/workflows/performance.yml`, Q-ARCH-431; I: the sums]:** at most three large legs are active at once. Lane 1: npm-ha, ha-suite, ha-faults, ha-pooler, nuget-ha, drain-load-b, -c, -e, cold-crash = 243.8 min of legs; lane 2: drain-roles, failover, npm-ha-soak, edge, drain-load-pooler = 133.2 min; lane 3: vuln-pooler, backup, oci-long = 52.4 min. The nightly's wall clock (299.2 min) is lane 1 plus queue and the image job (the first lane-1 leg started 16.8 min after the run). The GitHub-hosted legs (alerts 43.8, bastion 39.5, bastion-long 54.2, upgrade 20.6, vuln 17.9, vuln-long 36.1, ci-ha 16.2) run outside the lanes.
* **The nightly measured is a dispatch of all legs.** A scheduled run skips its legs when a dispatch of all legs ended less than 12 hours before (workflow header); `upgrade-release` is left out of the schedule and took 16 s here [V].

<details>
<summary>Every job of the three runs, and where the set-up time of a leg goes (sections 2.1 to 2.3)</summary>

### 2.1 Every job, push runs (seconds; infra / tests / teardown by the step classes of section 1)

| job | runner | run 1: job id | s | infra | tests | teardown | run 2: job id | s | infra | tests | teardown |
|---|---|---|---:|---:|---:|---:|---|---:|---:|---:|---:|
| integration (shard b) | self-hosted | 135992223831 | 2463 | 175 | 2266 | 22 | 136144955342 | 2454 | 185 | 2246 | 23 |
| integration (shard c) | self-hosted | 135992223760 | 2282 | 168 | 2092 | 22 | 136144955308 | 2256 | 187 | 2047 | 22 |
| integration (shard a) | self-hosted | 135992223606 | 2150 | 184 | 1948 | 18 | 136144955369 | 2087 | 182 | 1887 | 18 |
| UI layout checks | self-hosted | 135992223308 | 2116 | 234 | 1868 | 14 | 136144955185 | 1961 | 215 | 1733 | 13 |
| e2e (access-long) | large | 135994996094 | 1553 | 349 | 1190 | 14 | 136146450946 | 1655 | 458 | 1182 | 15 |
| e2e (npm) | large | 135994996091 | 1382 | 289 | 1078 | 15 | 136146450843 | 1428 | 324 | 1087 | 17 |
| e2e (nuget) | gh-hosted | 135994996032 | 1243 | 493 | 718 | 32 | 136146450846 | 1341 | 419 | 898 | 24 |
| e2e (vuln) | gh-hosted | 135994996089 | 1086 | 508 | 545 | 33 | 136146451003 | 1160 | 500 | 633 | 27 |
| e2e (push) | large | 135994996151 | 1150 | 325 | 814 | 11 | 136146450879 | 1148 | 319 | 817 | 12 |
| e2e (ansible) | large | 135994995997 | 821 | 446 | 361 | 14 | 136146450889 | 964 | 567 | 382 | 15 |
| e2e (oci) | large | 135994995908 | 801 | 343 | 443 | 15 | 136146450866 | 961 | 454 | 493 | 14 |
| e2e (ci) | gh-hosted | 135994996065 | 735 | 457 | 269 | 9 | 136146450841 | 827 | 427 | 376 | 24 |
| e2e (deb) | large | 135994995998 | 765 | 364 | 386 | 15 | 136146450821 | 739 | 354 | 373 | 12 |
| unit tests | self-hosted | 135992223595 | 829 | 136 | 665 | 28 | 136144954997 | 737 | 128 | 575 | 34 |
| Combined Coverage Report | self-hosted | 136010308667 | 116 | 112 | 0 | 4 | 136161371063 | 508 | 504 | 0 | 4 |
| chart | self-hosted | 135992223940 | 360 | 97 | 259 | 4 | 136144955294 | 372 | 141 | 228 | 3 |
| Container Scan (Trivy, built image) | self-hosted | 135992223631 | 259 | 246 | 0 | 13 | 136144955424 | 340 | 328 | 0 | 12 |
| web (Angular) | self-hosted | 135992223807 | 356 | 292 | 60 | 4 | 136144955462 | 261 | 213 | 45 | 3 |
| Code Linting | self-hosted | 135992223774 | 251 | 251 | 0 | 0 | 136144954948 | 235 | 235 | 0 | 0 |
| image (host architecture, no push) | self-hosted | 135992223783 | 310 | 287 | 0 | 23 | 136144955256 | 209 | 188 | 0 | 21 |
| S3 conformance | self-hosted | 135992223680 | 169 | 129 | 38 | 2 | 136144955179 | 204 | 166 | 35 | 3 |
| versions | self-hosted | 135992223008 | 283 | 282 | 0 | 1 | 136144954723 | 197 | 197 | 0 | 0 |
| Malware Scan (source code) | self-hosted | 135992223813 | 205 | 203 | 0 | 2 | 136144954886 | 187 | 185 | 0 | 2 |
| generate | self-hosted | 135992223778 | 128 | 127 | 0 | 1 | 136144955106 | 186 | 185 | 0 | 1 |
| GoSec Security Scan | self-hosted | 135992223654 | 318 | 317 | 0 | 1 | 136144954859 | 185 | 184 | 0 | 1 |
| coverage evidence (implemented.txt) | self-hosted | 136011512369 | 98 | 95 | 0 | 3 | 136161371210 | 177 | 173 | 0 | 4 |
| build without Node.js | self-hosted | 135992223611 | 108 | 107 | 0 | 1 | 136144955007 | 169 | 168 | 0 | 1 |
| Vulnerability Check | self-hosted | 135992223684 | 133 | 133 | 0 | 0 | 136144955071 | 142 | 142 | 0 | 0 |
| e2e (bastion) | gh-hosted | 135994996062 | 130 | 130 | 0 | 0 | 136146450996 | 130 | 130 | 0 | 0 |
| coverage generator (links and rules) | self-hosted | 135992223773 | 140 | 139 | 0 | 1 | 136144955463 | 128 | 128 | 0 | 0 |
| Cyclomatic Complexity | self-hosted | 135992224015 | 69 | 68 | 0 | 1 | 136144954974 | 124 | 123 | 0 | 1 |
| Release Tooling | self-hosted | 135992223648 | 93 | 70 | 22 | 1 | 136144955295 | 100 | 81 | 18 | 1 |
| workflow files (actionlint) | self-hosted | 135992224474 | 29 | 28 | 0 | 1 | 136144955613 | 66 | 66 | 0 | 0 |
| nuget vectors | self-hosted | 135992223863 | 66 | 66 | 0 | 0 | 136144955074 | 45 | 45 | 0 | 0 |
| Semantic Release | self-hosted | 135998502449 | 57 | 56 | 0 | 1 | 136149946672 | 43 | 42 | 0 | 1 |
| SBOM generator (pinned syft, test image) | self-hosted | 135992223693 | 41 | 40 | 0 | 1 | 136144955253 | 39 | 38 | 0 | 1 |
| fuzz nuget ${{ matrix.fuzz }} (30 min) | self-hosted | 135992225698 | skipped |  |  |  | 136144957013 | skipped |  |  |  |
| compatibility with the latest release | self-hosted | 135992226392 | skipped |  |  |  | 136144956882 | skipped |  |  |  |
| fuzz npm ${{ matrix.target }} (30 min) | self-hosted | 135992225362 | skipped |  |  |  | 136144956903 | skipped |  |  |  |
| **total** | | | **23095** (384.9 min) | 7746 | 15022 | 327 | | **23765** (396.1 min) | 8381 | 15055 | 329 |

### 2.2 Every job, nightly 44625277245

| job | runner | job id | result | s | infra | tests | teardown |
|---|---|---|---|---:|---:|---:|---:|
| e2e (bastion-long) | gh-hosted | 135919761300 | success | 3255 | 572 | 2648 | 35 |
| e2e (npm-ha) | large | 135919761224 | success | 2954 | 285 | 2653 | 16 |
| e2e (alerts) | gh-hosted | 135919761082 | success | 2628 | 318 | 2295 | 15 |
| e2e (npm-ha-soak) | large | 135949011795 | failure | 2480 | 253 | 2183 | 44 |
| e2e (bastion) | gh-hosted | 135919761110 | success | 2367 | 848 | 1509 | 10 |
| e2e (vuln-long) | gh-hosted | 135919761228 | success | 2169 | 568 | 1567 | 34 |
| e2e (ha-suite) | large | 135919761235 | success | 2140 | 277 | 1849 | 14 |
| e2e (ha-faults) | large | 135919761455 | success | 1951 | 306 | 1621 | 24 |
| e2e (edge) | large | 135970439080 | success | 1787 | 386 | 1384 | 17 |
| e2e (nuget-ha) | large | 136002849245 | success | 1765 | 312 | 1433 | 20 |
| e2e (drain-load-pooler) | large | 135984872568 | failure | 1517 | 524 | 963 | 30 |
| e2e (backup) | large | 135939762897 | failure | 1475 | 267 | 1182 | 26 |
| e2e (ha-pooler) | large | 135919761302 | success | 1450 | 437 | 999 | 14 |
| e2e (drain-load-c) | large | 136002849235 | success | 1258 | 305 | 936 | 17 |
| e2e (drain-load-e) | large | 136002849145 | success | 1248 | 294 | 927 | 27 |
| e2e (drain-roles) | large | 135919761337 | success | 1242 | 260 | 956 | 26 |
| e2e (upgrade) | gh-hosted | 135919761134 | success | 1239 | 781 | 445 | 13 |
| e2e (drain-load-b) | large | 136002849158 | success | 1235 | 302 | 916 | 17 |
| e2e (vuln) | gh-hosted | 135919761372 | success | 1075 | 619 | 442 | 14 |
| e2e (ci-ha) | gh-hosted | 135919761343 | success | 975 | 437 | 529 | 9 |
| e2e (failover) | large | 135919761310 | success | 963 | 322 | 629 | 12 |
| e2e (vuln-pooler) | large | 135919761262 | success | 846 | 442 | 391 | 13 |
| e2e (oci-long) | large | 135952612111 | success | 821 | 349 | 458 | 14 |
| e2e (cold-crash) | large | 136002849164 | success | 630 | 327 | 290 | 13 |
| Latency | large | 136046183602 | success | 548 | 82 | 452 | 14 |
| Vulnerability scanning performance | large | 136049926697 | success | 408 | 79 | 315 | 14 |
| Quader image for the kind legs | self-hosted | 135917312289 | success | 240 | 222 | 0 | 18 |
| Platform matrix (k8s-max-helm3) | gh-hosted | 135917312460 | success | 77 | 30 | 46 | 1 |
| Platform matrix (k8s-min-helm4) | gh-hosted | 135917312618 | success | 74 | 28 | 45 | 1 |
| Platform matrix (k8s-max-helm4) | gh-hosted | 135917312527 | success | 73 | 21 | 51 | 1 |
| Platform matrix (k8s-min-helm3) | gh-hosted | 135917312708 | success | 70 | 20 | 49 | 1 |
| e2e (upgrade-release) | gh-hosted | 135919761207 | success | 16 | 16 | 0 | 0 |
| **total** | | | | **40976** (682.9 min) | 10289 | 30163 | 524 |

### 2.3 Where the infra of the kind legs and shards goes (seconds; step classes of `cats.py`)

The column "test step" is the whole `make e2e` / `make test-integration` / render step, including the `make e2e` overhead outside the listed tests.


**Run 2 (44693590111): kind legs, integration shards, UI layout**

| job | id | wall s | dev-up | kind, helm, charts | Quader image load | client images | Node + Playwright | free disk | integ. stack | checkout, Go, Buildx | other | test step | teardown, uploads |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| UI layout checks | 136144955185 | 1961 | 0 | 0 | 0 | 0 | 110 | 0 | 0 | 91 | 14 | 1733 | 13 |
| integration (shard c) | 136144955308 | 2256 | 0 | 0 | 0 | 0 | 0 | 0 | 55 | 126 | 6 | 2047 | 22 |
| integration (shard b) | 136144955342 | 2454 | 0 | 0 | 0 | 0 | 0 | 0 | 64 | 114 | 7 | 2246 | 23 |
| integration (shard a) | 136144955369 | 2087 | 0 | 0 | 0 | 0 | 0 | 0 | 58 | 118 | 6 | 1887 | 18 |
| e2e (deb) | 136146450821 | 739 | 173 | 19 | 13 | 82 | 0 | 0 | 0 | 61 | 6 | 373 | 12 |
| e2e (ci) | 136146450841 | 827 | 186 | 3 | 4 | 146 | 0 | 55 | 0 | 30 | 3 | 376 | 24 |
| e2e (npm) | 136146450843 | 1428 | 154 | 22 | 14 | 63 | 0 | 0 | 0 | 65 | 6 | 1087 | 17 |
| e2e (nuget) | 136146450846 | 1341 | 178 | 5 | 4 | 107 | 0 | 101 | 0 | 21 | 3 | 898 | 24 |
| e2e (oci) | 136146450866 | 961 | 165 | 9 | 112 | 90 | 0 | 0 | 0 | 72 | 6 | 493 | 14 |
| e2e (push) | 136146450879 | 1148 | 174 | 8 | 67 | 0 | 0 | 0 | 0 | 64 | 6 | 817 | 12 |
| e2e (ansible) | 136146450889 | 964 | 161 | 14 | 109 | 72 | 128 | 0 | 0 | 77 | 6 | 382 | 15 |
| e2e (access-long) | 136146450946 | 1655 | 173 | 26 | 84 | 0 | 110 | 0 | 0 | 58 | 7 | 1182 | 15 |
| e2e (bastion) | 136146450996 | 130 | 0 | 0 | 0 | 0 | 0 | 116 | 0 | 12 | 2 | 0 | 0 |
| e2e (vuln) | 136146451003 | 1160 | 185 | 7 | 7 | 154 | 44 | 57 | 0 | 41 | 5 | 633 | 27 |
| **sum** | | **19111** | **1549** | **113** | **414** | **714** | **392** | **329** | **177** | **950** | **83** | **14154** | **236** |

**Run 1 (44646960056): the same**

| job | id | wall s | dev-up | kind, helm, charts | Quader image load | client images | Node + Playwright | free disk | integ. stack | checkout, Go, Buildx | other | test step | teardown, uploads |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| UI layout checks | 135992223308 | 2116 | 0 | 0 | 0 | 0 | 142 | 0 | 1 | 67 | 24 | 1868 | 14 |
| integration (shard a) | 135992223606 | 2150 | 0 | 0 | 0 | 0 | 0 | 0 | 58 | 120 | 6 | 1948 | 18 |
| integration (shard c) | 135992223760 | 2282 | 0 | 0 | 0 | 0 | 0 | 0 | 47 | 116 | 5 | 2092 | 22 |
| integration (shard b) | 135992223831 | 2463 | 0 | 0 | 0 | 0 | 0 | 0 | 46 | 124 | 5 | 2266 | 22 |
| e2e (oci) | 135994995908 | 801 | 183 | 11 | 16 | 69 | 0 | 0 | 0 | 58 | 6 | 443 | 15 |
| e2e (ansible) | 135994995997 | 821 | 151 | 9 | 21 | 72 | 120 | 0 | 0 | 67 | 6 | 361 | 14 |
| e2e (deb) | 135994995998 | 765 | 168 | 8 | 40 | 70 | 0 | 0 | 0 | 67 | 11 | 386 | 15 |
| e2e (nuget) | 135994996032 | 1243 | 183 | 8 | 7 | 141 | 0 | 108 | 0 | 42 | 4 | 718 | 32 |
| e2e (bastion) | 135994996062 | 130 | 0 | 0 | 0 | 0 | 0 | 117 | 0 | 10 | 3 | 0 | 0 |
| e2e (ci) | 135994996065 | 735 | 167 | 8 | 16 | 124 | 0 | 96 | 0 | 41 | 5 | 269 | 9 |
| e2e (vuln) | 135994996089 | 1086 | 176 | 6 | 4 | 167 | 44 | 70 | 0 | 38 | 3 | 545 | 33 |
| e2e (npm) | 135994996091 | 1382 | 156 | 22 | 14 | 47 | 0 | 0 | 0 | 44 | 6 | 1078 | 15 |
| e2e (access-long) | 135994996094 | 1553 | 163 | 9 | 19 | 0 | 97 | 0 | 0 | 54 | 7 | 1190 | 14 |
| e2e (push) | 135994996151 | 1150 | 163 | 11 | 66 | 0 | 0 | 0 | 0 | 79 | 6 | 814 | 11 |
| **sum** | | **18677** | **1510** | **92** | **203** | **690** | **403** | **391** | **152** | **927** | **97** | **13978** | **234** |

**Nightly (44625277245): kind legs, platform matrix, perf legs**

| job | id | wall s | dev-up | kind, helm, charts | Quader image load | client images | Node + Playwright | free disk | integ. stack | checkout, Go, Buildx | other | test step | teardown, uploads |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Platform matrix (k8s-max-helm3) | 135917312460 | 77 | 0 | 6 | 0 | 0 | 0 | 0 | 0 | 17 | 7 | 46 | 1 |
| Platform matrix (k8s-max-helm4) | 135917312527 | 73 | 0 | 3 | 0 | 0 | 0 | 0 | 0 | 12 | 6 | 51 | 1 |
| Platform matrix (k8s-min-helm4) | 135917312618 | 74 | 0 | 5 | 0 | 0 | 0 | 0 | 0 | 17 | 6 | 45 | 1 |
| Platform matrix (k8s-min-helm3) | 135917312708 | 70 | 0 | 2 | 0 | 0 | 0 | 0 | 0 | 13 | 5 | 49 | 1 |
| e2e (alerts) | 135919761082 | 2628 | 182 | 8 | 4 | 0 | 0 | 97 | 0 | 25 | 2 | 2295 | 15 |
| e2e (bastion) | 135919761110 | 2367 | 159 | 8 | 7 | 48 | 411 | 166 | 0 | 44 | 5 | 1509 | 10 |
| e2e (upgrade) | 135919761134 | 1239 | 165 | 8 | 7 | 48 | 235 | 124 | 0 | 52 | 142 | 445 | 13 |
| e2e (upgrade-release) | 135919761207 | 16 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 13 | 3 | 0 | 0 |
| e2e (npm-ha) | 135919761224 | 2954 | 184 | 9 | 15 | 22 | 0 | 0 | 0 | 50 | 5 | 2653 | 16 |
| e2e (vuln-long) | 135919761228 | 2169 | 204 | 4 | 5 | 69 | 96 | 142 | 0 | 44 | 4 | 1567 | 34 |
| e2e (ha-suite) | 135919761235 | 2140 | 166 | 8 | 15 | 18 | 0 | 0 | 0 | 63 | 7 | 1849 | 14 |
| e2e (vuln-pooler) | 135919761262 | 846 | 184 | 13 | 17 | 164 | 0 | 0 | 0 | 59 | 5 | 391 | 13 |
| e2e (bastion-long) | 135919761300 | 3255 | 202 | 6 | 9 | 73 | 83 | 154 | 0 | 40 | 5 | 2648 | 35 |
| e2e (ha-pooler) | 135919761302 | 1450 | 158 | 19 | 115 | 32 | 0 | 0 | 0 | 107 | 6 | 999 | 14 |
| e2e (failover) | 135919761310 | 963 | 210 | 11 | 17 | 30 | 0 | 0 | 0 | 47 | 7 | 629 | 12 |
| e2e (drain-roles) | 135919761337 | 1242 | 150 | 9 | 17 | 18 | 0 | 0 | 0 | 60 | 6 | 956 | 26 |
| e2e (ci-ha) | 135919761343 | 975 | 171 | 7 | 9 | 97 | 0 | 112 | 0 | 36 | 5 | 529 | 9 |
| e2e (vuln) | 135919761372 | 1075 | 155 | 7 | 8 | 51 | 243 | 110 | 0 | 41 | 4 | 442 | 14 |
| e2e (ha-faults) | 135919761455 | 1951 | 163 | 9 | 17 | 25 | 0 | 0 | 0 | 85 | 7 | 1621 | 24 |
| e2e (backup) | 135939762897 | 1475 | 175 | 8 | 15 | 17 | 0 | 0 | 0 | 46 | 6 | 1182 | 26 |
| e2e (npm-ha-soak) | 135949011795 | 2480 | 149 | 9 | 15 | 22 | 0 | 0 | 0 | 52 | 6 | 2183 | 44 |
| e2e (oci-long) | 135952612111 | 821 | 157 | 9 | 15 | 110 | 0 | 0 | 0 | 53 | 5 | 458 | 14 |
| e2e (edge) | 135970439080 | 1787 | 164 | 12 | 24 | 112 | 0 | 0 | 0 | 65 | 9 | 1384 | 17 |
| e2e (drain-load-pooler) | 135984872568 | 1517 | 186 | 9 | 108 | 112 | 0 | 0 | 0 | 103 | 6 | 963 | 30 |
| e2e (drain-load-e) | 136002849145 | 1248 | 150 | 8 | 14 | 74 | 0 | 0 | 0 | 43 | 5 | 927 | 27 |
| e2e (drain-load-b) | 136002849158 | 1235 | 153 | 8 | 14 | 74 | 0 | 0 | 0 | 46 | 7 | 916 | 17 |
| e2e (cold-crash) | 136002849164 | 630 | 168 | 7 | 16 | 77 | 0 | 0 | 0 | 53 | 6 | 290 | 13 |
| e2e (drain-load-c) | 136002849235 | 1258 | 147 | 8 | 16 | 84 | 0 | 0 | 0 | 44 | 6 | 936 | 17 |
| e2e (nuget-ha) | 136002849245 | 1765 | 151 | 9 | 15 | 78 | 0 | 0 | 0 | 53 | 6 | 1433 | 20 |
| Latency | 136046183602 | 548 | 0 | 0 | 0 | 0 | 0 | 0 | 39 | 38 | 5 | 452 | 14 |
| Vulnerability scanning performance | 136049926697 | 408 | 0 | 0 | 0 | 0 | 0 | 0 | 38 | 36 | 5 | 315 | 14 |
| **sum** | | **40736** | **4053** | **229** | **514** | **1455** | **1068** | **905** | **77** | **1457** | **309** | **30163** | **506** |

Reading, run 2 [I]: of the 318.5 runner min of the 14 jobs in the first table, 235.9 are the test step and 82.6 are not (dev-up 25.8, checkout/Go/Buildx 15.8, client images 11.9, Quader image load 6.9, Node/Playwright 6.5, free disk 5.5, integration stacks 3.0, kind/helm/charts 1.9, other 1.4, teardown 3.9). The three integration shards bring up their stack in 46 to 64 s each; with Go set-up and module download each shard has 182 to 187 s of non-test steps (9.2 min per push in total, 8 % of the shards' 113.3 min).

</details>

## 3. Per test

<details>
<summary>Every test of the e2e legs, the integration shards and the nightly legs (sections 3.1 to 3.7)</summary>


Seconds are the `Elapsed` of the test event in the `go test -json` stream [V]; "seconds outside its subtests" is [I] (section 1, item 4); owner and class are [J] with the note shown. A top-level test with parallel subtests (the npm client matrix) shows its wall time, not the sum of its rows. Subtests of the large tests are in `data/e2e_tests.csv` only for the direct-children sum; the biggest subtests are named where they matter in the notes below.

### 3.1 e2e legs of a push (run 2 first, run 1 for comparison)


#### e2e (npm): run 2 job 136146450843 (1428 s), run 1 job 135994996091 (1382 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestNPMMirror_R-REPO_055_ClientsFirst | 358.9 | 391.2 | npm | repeated | 59.8 | R-REPO-055/059 generic mirror machinery with npm clients |
| TestNPMReplicas | 191.9 | 174.9 | npm | repeated | 0.0 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMCollapse | 128.1 | 132.4 | npm | repeated | 0.0 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMUpstream | 84.3 | 85.5 | npm | repeated | 0.0 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMEndpoint | 79.5 | 73.0 | npm | repeated | 45.1 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMMatrix | 48.1 | 47.8 | npm | client | 0.0 | eight real client versions (npm 10-12, pnpm 10-12, yarn 1/4) |
| TestNPMLimits_R-LIM_020_WhatNpmShowsAtARefusal | 17.0 | - | npm | client | 0.0 | what npm prints at a refusal |
| TestNPMProjectPartner | 14.7 | 14.7 | npm | repeated | 5.9 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMMirror_R-REPO_059_RestoreWithTheUpstreamStopped | 13.1 | 12.7 | npm | repeated | 0.0 | R-REPO-055/059 generic mirror machinery with npm clients |
| TestNPMExternalUser_R-AUTHZ_030 | 7.6 | 8.1 | npm | repeated | 4.9 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMVisibility_R-AUTHZ_034 | 5.7 | 5.6 | npm | repeated | 2.1 | R-REPO/R-ARCH/R-EP/R-AUTHZ behaviour driven by the npm client |
| TestNPMClients | 4.2 | 3.3 | npm | infra | 0.0 | smoke test of the client pods |

Run 2 split of the 1428 s: set-up steps 324, `make e2e` outside the listed tests 134 (inferred: step time minus the sum of the top-level tests), teardown and uploads 17, test classes client 65 / repeated 766 / infra-test 4 / once 0, in-test set-up 118.

#### e2e (nuget): run 2 job 136146450846 (1341 s), run 1 job 135994996032 (1243 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestNuGetMirror_R-REPO_055_ClientsFirst | 126.6 | 110.8 | nuget | repeated | 0.0 | generic behaviour driven by dotnet |
| TestNuGetCollapse | 69.3 | 53.7 | nuget | repeated | 45.7 | generic behaviour driven by dotnet |
| TestNuGetPushRetry | 54.8 | 34.1 | nuget | client | 0.0 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetEndpoint | 43.2 | 31.2 | nuget | repeated | 25.6 | generic behaviour driven by dotnet |
| TestNuGetProjectPartner | 40.3 | 32.7 | nuget | repeated | 11.5 | generic behaviour driven by dotnet |
| TestNuGetPush | 36.1 | 28.1 | nuget | client | 0.1 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetLimits_R-LIM_020_WhatDotnetShowsAtARefusal | 35.0 | - | nuget | client | 0.0 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetUnlistRelistDelete | 34.1 | 25.0 | nuget | client | 8.7 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetVirtual | 32.9 | 22.6 | nuget | repeated | 1.1 | generic behaviour driven by dotnet |
| TestNuGetRestore | 31.3 | 23.0 | nuget | client | 12.5 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetVisibility_R-AUTHZ_034 | 29.2 | 21.7 | nuget | repeated | 13.5 | generic behaviour driven by dotnet |
| TestNuGetRemote | 25.9 | 19.7 | nuget | client | 1.5 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetExe | 16.7 | 12.5 | nuget | client | 3.4 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetClients | 15.2 | 14.5 | nuget | client | 0.0 | dotnet / nuget.exe behaviour (R-NUGET-*) |
| TestNuGetReplicas | 11.0 | 7.9 | nuget | repeated | 10.5 | generic behaviour driven by dotnet |
| TestNuGetMirror_R-REPO_059_RestoreWithTheUpstreamStopped | 9.1 | 4.3 | nuget | repeated | 0.0 | generic behaviour driven by dotnet |
| TestNuGetSearchClaimTiming | 1.9 | 1.2 | nuget | client | 0.0 | dotnet / nuget.exe behaviour (R-NUGET-*) |

Run 2 split of the 1341 s: set-up steps 419, `make e2e` outside the listed tests 285 (inferred: step time minus the sum of the top-level tests), teardown and uploads 24, test classes client 225 / repeated 254 / infra-test 0 / once 0, in-test set-up 134.

#### e2e (oci): run 2 job 136146450866 (961 s), run 1 job 135994995908 (801 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestOCIMirror_R-REPO_055_APassRunsWhileClientsPull | 174.3 | 175.8 | oci | repeated | 0.0 | generic mirror/virtual machinery driven by docker/podman |
| TestOCIEngineMirror | 41.4 | 48.8 | oci | client | 40.6 | docker/podman/buildx/kubelet behaviour |
| TestOCILimits_R-LIM_020_WhatDockerAndPodmanShowAtARefusal | 21.3 | - | oci | client | 0.0 | docker/podman/buildx/kubelet behaviour |
| TestOCILocal | 20.6 | 21.6 | oci | client | 0.0 | docker/podman/buildx/kubelet behaviour |
| TestOCILimits_R_API_016_APushOfManyTinyLayersMeetsTheWriteClass | 18.2 | - | oci | client | 0.0 | docker/podman/buildx/kubelet behaviour |
| TestOCILimits_F161_SignInsAndPullsFromOneAddressMeetNoAnonymousBudget | 16.9 | - | oci | client | 0.0 | docker/podman/buildx/kubelet behaviour |
| TestOCIRemote | 12.2 | 11.5 | oci | client | 3.3 | docker/podman/buildx/kubelet behaviour |
| TestOCIVirtual | 10.1 | 9.9 | oci | repeated | 2.5 | generic mirror/virtual machinery driven by docker/podman |
| TestOCISnippets | 7.5 | 8.0 | oci | client | 0.0 | docker/podman/buildx/kubelet behaviour |
| TestOCIKubernetes | 6.1 | 6.2 | oci | client | 2.7 | docker/podman/buildx/kubelet behaviour |
| TestOCIRemoteMirror | 5.7 | 5.8 | oci | repeated | 2.6 | generic mirror/virtual machinery driven by docker/podman |
| TestOCIProbe_R-REPO_065_ManifestInspectThroughAVirtualRepositoryDownloadsTheImage | 2.0 | - | oci | client | 0.0 | docker/podman/buildx/kubelet behaviour |
| TestOCIClients | 1.3 | 1.3 | oci | infra | 0.0 | smoke test of the client pods |

Run 2 split of the 961 s: set-up steps 454, `make e2e` outside the listed tests 155 (inferred: step time minus the sum of the top-level tests), teardown and uploads 14, test classes client 100 / repeated 185 / infra-test 1 / once 0, in-test set-up 52.

#### e2e (deb): run 2 job 136146450821 (739 s), run 1 job 135994995998 (765 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestDebVirtual | 76.1 | 78.3 | deb | repeated | 64.5 | visibility / mirror / virtual machinery driven by apt |
| TestDebLimits_R-LIM_020_WhatAptShowsAtARefusal | 68.1 | - | deb | client | 45.6 | apt on four distro images, signing, by-hash, rotation |
| TestDebRotation | 60.7 | 67.8 | deb | client | 0.0 | apt on four distro images, signing, by-hash, rotation |
| TestDebLocal | 41.2 | 90.7 | deb | client | 0.8 | apt on four distro images, signing, by-hash, rotation |
| TestDebMirror | 7.7 | 8.0 | deb | repeated | 2.7 | visibility / mirror / virtual machinery driven by apt |
| TestDebAnonymous | 7.0 | 9.9 | deb | repeated | 1.9 | visibility / mirror / virtual machinery driven by apt |
| TestDebRemote | 5.9 | 6.8 | deb | client | 0.4 | apt on four distro images, signing, by-hash, rotation |
| TestDebFlatRoot | 1.9 | 1.9 | deb | client | 0.4 | apt on four distro images, signing, by-hash, rotation |
| TestAPTClients | 1.3 | 1.7 | deb | infra | 0.0 | smoke test of the client pods |

Run 2 split of the 739 s: set-up steps 354, `make e2e` outside the listed tests 103 (inferred: step time minus the sum of the top-level tests), teardown and uploads 12, test classes client 131 / repeated 22 / infra-test 1 / once 0, in-test set-up 116.

#### e2e (ansible): run 2 job 136146450889 (964 s), run 1 job 135994995997 (821 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestAnsibleAuth | 75.7 | 68.6 | ansible | client | 48.5 | ansible-galaxy core 2.17 and 2.21 |
| TestAnsibleLocal | 52.0 | 53.6 | ansible | client | 0.4 | ansible-galaxy core 2.17 and 2.21 |
| TestAnsibleVirtual | 28.5 | 28.0 | ansible | repeated | 5.7 | generic mirror/virtual machinery driven by ansible-galaxy |
| TestAnsibleDocsJourney | 25.9 | 27.3 | ansible | client | 0.0 | collection documentation read from the archive, UI journeys unique to this format |
| TestAnsibleLimits_R-LIM_020_WhatAnsibleGalaxyShowsAtARefusal | 22.3 | - | ansible | client | 0.0 | ansible-galaxy core 2.17 and 2.21 |
| TestAnsibleDocsRemoteScreens | 14.7 | 15.2 | ansible | client | 2.0 | collection documentation read from the archive, UI journeys unique to this format |
| TestAnsibleRemote | 11.7 | 11.4 | ansible | client | 0.5 | ansible-galaxy core 2.17 and 2.21 |
| TestAnsibleMirror | 10.6 | 10.6 | ansible | repeated | 2.6 | generic mirror/virtual machinery driven by ansible-galaxy |
| TestAnsibleSnippets | 8.5 | 7.9 | ansible | client | 0.4 | ansible-galaxy core 2.17 and 2.21 |
| TestAnsibleDocsLocal | 7.2 | 6.9 | ansible | client | 7.0 | collection documentation read from the archive, UI journeys unique to this format |
| TestAnsibleDocsVirtual | 7.0 | 7.3 | ansible | client | 6.9 | collection documentation read from the archive, UI journeys unique to this format |
| TestAnsibleDigest | 6.9 | 6.9 | ansible | client | 0.2 | ansible-galaxy core 2.17 and 2.21 |
| TestAnsibleDocsWiki_R-ANS_046 | 5.5 | 5.0 | ansible | client | 3.5 | collection documentation read from the archive, UI journeys unique to this format |
| TestAnsibleClients | 3.4 | 3.5 | ansible | infra | 0.0 | smoke test of the client pods |
| TestAnsibleDocsRemote | 2.4 | 2.4 | ansible | client | 2.3 | collection documentation read from the archive, UI journeys unique to this format |

Run 2 split of the 964 s: set-up steps 567, `make e2e` outside the listed tests 100 (inferred: step time minus the sum of the top-level tests), teardown and uploads 15, test classes client 168 / repeated 31 / infra-test 3 / once 0, in-test set-up 80.

#### e2e (ci): run 2 job 136146450841 (827 s), run 1 job 135994996065 (735 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestCIPipeline | 70.2 | 53.0 | platform | once | 50.9 | CI OIDC feature; npm and NuGet clients carry it |
| TestCITokens | 37.3 | 34.6 | platform | once | 0.0 | CI OIDC feature; npm and NuGet clients carry it |
| TestCITasks | 0.1 | 2.1 | platform | once | 0.0 | CI OIDC feature; npm and NuGet clients carry it |

Run 2 split of the 827 s: set-up steps 427, `make e2e` outside the listed tests 268 (inferred: step time minus the sum of the top-level tests), teardown and uploads 24, test classes client 0 / repeated 0 / infra-test 0 / once 57, in-test set-up 51.

#### e2e (vuln): run 2 job 136146451003 (1160 s), run 1 job 135994996089 (1086 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestVuln_R-VULN_022_Journeys | 149.8 | 130.6 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_027_AcrossReplicasAndWithoutValkey | 22.1 | 19.8 | platform | once | 0.8 | vulnerability scanning feature |
| TestVuln_R-VULN_015_NPMBlockedInstallInTriage | 19.4 | 12.8 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_017_NPMAuditThroughEveryRepository | 19.3 | 14.0 | npm | client | 19.3 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_011_R-VULN_018_AnAdvisoryAppearsLate | 18.6 | 17.2 | platform | once | 0.4 | vulnerability scanning feature |
| TestVuln_R-VULN_015_NPMBlockedInstallMalicious | 6.4 | 4.2 | npm | client | 6.1 | audit/blocked install wire shape of one client |
| TestVuln_SetupSurvivesABrokenForward | 2.5 | 2.2 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_011_PublishEvaluatesInTheCommit | 1.7 | 1.2 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_018_NuGetVulnerabilityInfoOnTheWire | 0.6 | 0.5 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_017_NPMAuditBody | 0.5 | 0.4 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_015_NuGetBlockedRestoreInTriage | 0.2 | 0.2 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_010_ClaimedNamesAreInternal | 0.1 | 0.1 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_018_NuGetRestoreAudit | 0 | 0 | npm | client | 0.0 | audit/blocked install wire shape of one client |

Run 2 split of the 1160 s: set-up steps 500, `make e2e` outside the listed tests 392 (inferred: step time minus the sum of the top-level tests), teardown and uploads 27, test classes client 21 / repeated 0 / infra-test 0 / once 194, in-test set-up 27.

#### e2e (push): run 2 job 136146450879 (1148 s), run 1 job 135994996151 (1150 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestE14Drain | 191.8 | 178.4 | platform | once | 0.0 | base case of the drain under k6 + generic upload/download |
| TestE12Valkey | 147.5 | 147.7 | platform | once | 0.0 | format-independent platform behaviour |
| TestE1_14Exposure | 125.2 | 118.5 | platform | once | 45.1 | format-independent platform behaviour |
| TestE15TLS | 113.5 | 114.5 | platform | once | 0.0 | format-independent platform behaviour |
| TestE99Scatter | 80 | 81.6 | platform | once | 46.2 | format-independent platform behaviour |
| TestE06Lease | 68.9 | 68.9 | platform | once | 0.2 | format-independent platform behaviour |
| TestE04MigrationFailure | 11.4 | 12.5 | platform | once | 0.0 | format-independent platform behaviour |
| TestE07NetworkPolicy | 10.8 | 12.9 | platform | once | 0.0 | format-independent platform behaviour |
| TestE03Roles | 7.3 | 8.3 | platform | once | 0.0 | format-independent platform behaviour |
| TestE16Endpoint | 4.8 | 5.0 | platform | once | 0.0 | format-independent platform behaviour |
| TestE03DevProfile | 0.6 | 0.5 | platform | once | 0.0 | format-independent platform behaviour |
| TestE07PodSecurity | 0.2 | 0.2 | platform | once | 0.0 | format-independent platform behaviour |

Run 2 split of the 1148 s: set-up steps 319, `make e2e` outside the listed tests 55 (inferred: step time minus the sum of the top-level tests), teardown and uploads 12, test classes client 0 / repeated 0 / infra-test 0 / once 670, in-test set-up 92.

#### e2e (access-long): run 2 job 136146450946 (1655 s), run 1 job 135994996094 (1553 s)

| top-level test | run 2 s | run 1 s | owner | class | seconds in the test outside its subtests | note |
|---|---:|---:|---|---|---:|---|
| TestE1_11Journeys | 333 | 336.0 | platform | once | 0.0 | Playwright journeys (split per spec in the report) |
| TestE1_13Transfers | 291.7 | 281.6 | platform | once | 1.0 | format-independent platform behaviour |
| TestE1_11Scatter | 288.5 | 286.6 | platform | once | 0.1 | format-independent platform behaviour |
| TestE1_01Login | 164.4 | 171.2 | platform | once | 45.6 | format-independent platform behaviour |
| TestExternalUser_R-AUTHZ_030_SeesNothingInternal | 0.9 | 0.8 | platform | once | 0.5 | format-independent platform behaviour |

Run 2 split of the 1655 s: set-up steps 458, `make e2e` outside the listed tests 104 (inferred: step time minus the sum of the top-level tests), teardown and uploads 15, test classes client 0 / repeated 0 / infra-test 0 / once 1031, in-test set-up 47.

Subtests worth naming [V, run 2 unless stated]: `TestNPMMirror_R_REPO_055_ClientsFirst/npm` 288.3 s (run 1) and `/preemption` 34.8 s (run 1); `TestNPMReplicas`: publish on A view on B 34.0, Sentinel failover during the scenario list 49.7, FLUSHALL during the scenario list 49.7, unpublish whose answer the edge drops 32.8 (run 1); `TestNPMUpstream` first publish of a name the upstream serves refused 74.1 (run 1); `TestE12Valkey` two 63.7 s cases (a replica with a wrong password and with a wrong primary name stay not ready), `TestE14Drain` 178.4 (run 1) / 191.8 (run 2), `TestE1_11Scatter` three 55 to 122 s suites (browser logins; FLUSHALL in the middle; Valkey stopped), `TestE1_13Transfers` 137.0 + 74.4 + 68.8 (run 1), `TestE1_11Journeys` 333.0 (45 Playwright journeys, section 3.4).

### 3.2 Integration shards (run 2 job ids; package level; the 1,645 top-level tests are in `data/integration_tests_run2.csv`)


#### integration (shard a): run 2 job 136144955369 (2087 s: set-up 182, tests 1887, teardown 18); run 1 job 135992223606 (2150 s)

| package (test/integration/...) | run 2 s | run 1 s | tests | owner | client s | repeated s | note |
|---|---:|---:|---:|---|---:|---:|---|
| npm | 399.6 | 422.0 | 105 | npm | 229.3 | 170.3 |  |
| ci | 340.7 | 352.7 | 95 | platform | 0.0 | 0.0 | format-independent, once |
| nuget | 272.3 | 251.8 | 93 | nuget | 206.2 | 66.1 |  |
| mirror | 237.1 | 259.6 | 58 | platform | 0.0 | 0.0 | format-independent, once |
| coordination | 232.6 | 237.0 | 28 | platform | 0.0 | 0.0 | format-independent, once |
| proxy | 177.2 | 198.5 | 70 | platform | 0.0 | 0.0 | format-independent, once |
| repository | 170.6 | 174.3 | 37 | platform | 0.0 | 0.0 | format-independent, once |
| **sum** | 1830.0 | 1895.9 | 486 | | | | |

#### integration (shard b): run 2 job 136144955342 (2454 s: set-up 185, tests 2246, teardown 23); run 1 job 135992223831 (2463 s)

| package (test/integration/...) | run 2 s | run 1 s | tests | owner | client s | repeated s | note |
|---|---:|---:|---:|---|---:|---:|---|
| backup | 444.1 | 448.4 | 114 | platform | 0.0 | 0.0 | format-independent, once |
| api | 130.3 | 131.9 | 32 | platform | 0.0 | 0.0 | format-independent, once |
| valkey | 126.3 | 134.6 | 13 | platform | 0.0 | 0.0 | format-independent, once |
| files | 101.3 | 100.2 | 57 | platform | 0.0 | 0.0 | format-independent, once |
| limits | 87.4 | 37.8 | 14 | platform | 0.0 | 0.0 | format-independent, once |
| identity | 87.0 | 87.3 | 64 | platform | 0.0 | 0.0 | format-independent, once |
| postgres | 83.1 | 95.7 | 51 | platform | 0.0 | 0.0 | format-independent, once |
| querybudget | 82.8 | 86.7 | 7 | platform | 0.0 | 0.0 | format-independent, once |
| uploads | 79.7 | 80.3 | 18 | platform | 0.0 | 0.0 | format-independent, once |
| authz | 79.0 | 77.8 | 50 | platform | 0.0 | 0.0 | format-independent, once |
| blob | 69.3 | 68.2 | 18 | platform | 0.0 | 0.0 | format-independent, once |
| packages | 63.9 | 69.6 | 18 | platform | 0.0 | 0.0 | format-independent, once |
| ansibleremote | 61.9 | 59.1 | 24 | ansible | 58.8 | 3.1 |  |
| transfer | 60.0 | 67.6 | 14 | platform | 0.0 | 0.0 | format-independent, once |
| projects | 55.7 | 62.4 | 26 | platform | 0.0 | 0.0 | format-independent, once |
| ansiblelocal | 55.5 | 59.3 | 21 | ansible | 49.8 | 5.7 |  |
| server | 48.6 | 48.9 | 4 | platform | 0.0 | 0.0 | format-independent, once |
| login | 48.0 | 50.7 | 23 | platform | 0.0 | 0.0 | format-independent, once |
| importer | 42.1 | 42.0 | 17 | platform | 0.0 | 0.0 | format-independent, once |
| audit | 36.4 | 36.9 | 22 | platform | 0.0 | 0.0 | format-independent, once |
| ansiblevirtual | 34.5 | 33.9 | 14 | ansible | 31.9 | 2.7 |  |
| uiassets | 28.2 | 30.1 | 2 | platform | 0.0 | 0.0 | format-independent, once |
| smoke | 25.0 | 34.0 | 11 | platform | 0.0 | 0.0 | format-independent, once |
| endpoints | 24.0 | 24.0 | 5 | platform | 0.0 | 0.0 | format-independent, once |
| overload | 21.5 | 26.0 | 3 | platform | 0.0 | 0.0 | format-independent, once |
| roles | 20.8 | 22.6 | 6 | platform | 0.0 | 0.0 | format-independent, once |
| mirrorapi | 16.7 | 18.7 | 8 | platform | 0.0 | 0.0 | format-independent, once |
| restoreversions | 14.3 | 14.9 | 4 | platform | 0.0 | 0.0 | format-independent, once |
| search | 11.7 | 12.7 | 5 | platform | 0.0 | 0.0 | format-independent, once |
| tls | 9.4 | 9.5 | 4 | platform | 0.0 | 0.0 | format-independent, once |
| apikey | 7.6 | 8.1 | 3 | platform | 0.0 | 0.0 | format-independent, once |
| virtual | 5.2 | 6.3 | 6 | platform | 0.0 | 0.0 | format-independent, once |
| storageverify | 3.7 | 4.2 | 2 | platform | 0.0 | 0.0 | format-independent, once |
| testenv | 2.0 | 2.0 | 6 | platform | 0.0 | 0.0 | format-independent, once |
| **sum** | 2067.2 | 2092.4 | 686 | | | | |

#### integration (shard c): run 2 job 136144955308 (2256 s: set-up 187, tests 2047, teardown 22); run 1 job 135992223760 (2282 s)

| package (test/integration/...) | run 2 s | run 1 s | tests | owner | client s | repeated s | note |
|---|---:|---:|---:|---|---:|---:|---|
| debvirtual | 263.0 | 265.7 | 14 | deb | 225.0 | 38.0 |  |
| vulnsync | 240.3 | 254.7 | 61 | platform | 0.0 | 0.0 | format-independent, once |
| deblocal | 209.4 | 207.8 | 26 | deb | 176.2 | 33.2 |  |
| debremote | 152.7 | 154.6 | 27 | deb | 137.5 | 15.2 |  |
| bastion | 139.1 | 140.8 | 27 | platform | 0.0 | 0.0 | format-independent, once |
| vulnfindings | 138.7 | 151.9 | 47 | platform | 0.0 | 0.0 | format-independent, once |
| ociremote | 137.6 | 138.5 | 39 | oci | 86.6 | 51.0 |  |
| vulnapi | 120.9 | 130.9 | 29 | platform | 0.0 | 0.0 | format-independent, once |
| vulnblock | 74.6 | 81.5 | 17 | platform | 0.0 | 0.0 | format-independent, once |
| ocilocal | 46.0 | 44.1 | 16 | oci | 46.0 | 0.0 |  |
| registry | 44.5 | 43.6 | 17 | oci | 42.0 | 2.5 |  |
| bastioniface | 39.6 | 37.6 | 35 | platform | 0.0 | 0.0 | format-independent, once |
| ocivirtual | 38.1 | 36.5 | 15 | oci | 38.1 | 0.0 |  |
| bastionreg | 36.7 | 36.1 | 14 | platform | 0.0 | 0.0 | format-independent, once |
| vulnrestore | 33.9 | 35.6 | 8 | platform | 0.0 | 0.0 | format-independent, once |
| debkeys | 32.6 | 34.0 | 14 | deb | 26.1 | 6.5 |  |
| oci | 30.0 | 30.6 | 13 | oci | 21.9 | 8.1 |  |
| vulnimage | 28.3 | 31.3 | 10 | platform | 0.0 | 0.0 | format-independent, once |
| vulnaudit | 24.4 | 26.6 | 10 | platform | 0.0 | 0.0 | format-independent, once |
| itempublish | 19.9 | 20.4 | 9 | platform | 0.0 | 0.0 | format-independent, once |
| bastioncache | 16.2 | 15.7 | 10 | platform | 0.0 | 0.0 | format-independent, once |
| bastionshared | 4.5 | 3.9 | 13 | platform | 0.0 | 0.0 | format-independent, once |
| vulnflow | 4.0 | 4.6 | 1 | platform | 0.0 | 0.0 | format-independent, once |
| bastionsetup | 2.4 | 2.6 | 1 | platform | 0.0 | 0.0 | format-independent, once |
| **sum** | 1877.4 | 1929.4 | 473 | | | | |

The 30 slowest integration tests of run 2:

| test | package | s | owner | class |
|---|---|---:|---|---|
| TestInvalidationAtScale | repository | 116.8 | platform | once |
| TestR_DEB_019_ASuiteTheUpstreamDoesNotHaveIsRecordedAbsent | debremote | 62.5 | deb | client |
| TestR_BAK_021_Resumption | backup | 50.6 | platform | once |
| TestR_BAK_021_KillNine | backup | 50.1 | platform | once |
| TestFlightLockFailover | valkey | 49.0 | platform | once |
| TestFailover | valkey | 48.5 | platform | once |
| TestChangePropagation | coordination | 45.5 | platform | once |
| TestR_DEB_016_AFreshlyBoundImportedKeySignsOnlyAfterItsSelfSignature | deblocal | 45.1 | deb | client |
| TestR_STOR_011_TheSweepRemovesWhatNoSessionStandsFor | uploads | 41.4 | platform | once |
| TestR_AUTHZ_007_DebActionMapOfTheVirtualClass | debvirtual | 38.0 | deb | repeated |
| TestR_DEB_026_TheDelayOfAVirtualBuildOverALargeIndexStaysWithinItsBound | debvirtual | 37.3 | deb | client |
| TestCollapse | proxy | 36.6 | platform | once |
| TestReplicaLostWithDefaults | coordination | 30.7 | platform | once |
| TestR_ARCH_027_ADownloadCostsOneStatement | querybudget | 29.5 | platform | once |
| TestClusterWideRateLimit | api | 28.2 | platform | once |
| TestJobPickup | coordination | 27.8 | platform | once |
| TestR_DEB_028_UploadsGoToTheDeploymentTarget | debvirtual | 26.6 | deb | client |
| TestR_BAK_020_Preconditions | backup | 25.7 | platform | once |
| TestR_VULN_028_TheEvaluationOfAPublishIsMeasured | vulnfindings | 25.1 | platform | once |
| TestR_VULN_020_TheFirstPageAtScale | vulnapi | 24.8 | platform | once |
| TestDatabaseOutageGrace | server | 24.7 | platform | once |
| TestValkeyClientFollowsASentinelFailover | smoke | 24.6 | platform | once |
| TestR_VULN_015_ScanningAddsNoQueryToADownload | vulnblock | 24.3 | platform | once |
| TestR_DEB_010_DelayBound | deblocal | 23.7 | deb | client |
| TestR_REPO_041_NpmAlwaysVerifiesTheAnnouncedIntegrityWhateverTheChecksumPolicySays | npm | 23.2 | npm | client |
| TestR_ARCH_027_ADocumentOfACallerWithPathPatternsCostsOneStatement | querybudget | 22.5 | platform | once |
| TestR_HA_020_SharedUIAssetsWithoutTheObjectStore | uiassets | 21.9 | platform | once |
| TestR_VULN_028_TheFirstPageOfAPatternGrantAtTheShapeOfTheRun | vulnapi | 21.9 | platform | once |
| TestR_STOR_011_SuccessiveSweepRunsProgressThroughTheStagingArea | uploads | 21.4 | platform | once |
| TestR_BAS_004_AdminCommands | bastionreg | 21.1 | platform | once |

Format packages and their calls [J, by name]: the largest client tests are Debian's (`TestR_DEB_016_AFreshlyBoundImportedKey...` 45.1, `TestR_DEB_019_ASuiteTheUpstreamDoesNotHaveIsRecordedAbsent` 62.5, the by-hash, index and signing tests); the largest repeated ones are npm's replica and concurrency tests (`TestR_NPM_010_PublishThenViewOnAnotherReplica` 19.3 s, `TestR_NPM_030_...OnAnotherReplica` 15.2, `TestR_NPM_024_FiftyConcurrentRequestsOverThreeReplicasCauseOneUpstreamRequest` 6.8), NuGet's `TestR_NUGET_031_FiftyConcurrentRestoresOverThreeReplicasCauseBoundedUpstreamRequests` 12.5 and the per-format `...ActionMap` authz tests (`TestR_AUTHZ_007_DebActionMapOfTheVirtualClass` 38.0 s). I read `test/integration/npm/documents_test.go:53-57` (publish then view on another replica, 100 rounds, Valkey up and down) and `test/integration/mirror/preempt_test.go:54` to confirm that two behaviours of the e2e format legs already have an in-process test (section 5).

OCI conformance (external suite, `make oci-conformance` in shard c): 35 s in run 2, 31 s in run 1 [V]; two suites (local and virtual repository) of 80 specs each (4 skipped) in `junit.xml`, 3.2 s of suite time for the local one in the 35 s step.

### 3.3 Unit tests, web, chart (not per test)

`make test` is 665 s (run 1) of the 829 s job and `unit tests` runs 4,000 top-level tests in 234 packages that overlap on one runner, so package times are not additive [V; the overlap is I]. Package wall times of the per-format packages (run 2, upper bounds of what a format adds, summed over its packages): npm 108.0 s, NuGet 82.9 s, Ansible 68.1 s (of which `collection` 47.6), OCI 31.7 s, Debian 30.3 s, generic 2.4 s; the contract kit `formattest` 153.8 s + `formattest/testformat` 104.4 s run once, not per format [V from `test-unit.json`]. How much of the 12.3 min unit job each format adds cannot be derived (section 7).

### 3.4 Browser journeys inside `TestE1_11Journeys` (Playwright `results.json` of `e2e-playwright-access-long`, run 2, artifact 14611581683)

| spec file | journeys | s | format | class (judgement) |
|---|---:|---:|---|---|
| 01-sign-in | 4 | 12.9 | platform | once |
| 02-repository-and-files | 2 | 11.5 | platform | once |
| 03-access | 1 | 15.7 | platform | once |
| 04-tokens | 1 | 6.1 | platform | once |
| 05-exposure | 1 | 8.5 | platform | once |
| 06-audit-and-system | 2 | 15.5 | platform | once |
| 07-german-dark | 1 | 19.4 | platform | once |
| 08-packages | 3 | 23.5 | npm | repeated (settings, creation form and setup snippets read back per format) |
| 09-guides | 1 | 4.5 | platform | once |
| 10-help | 2 | 20.4 | platform | once |
| 11-nuget | 2 | 10.4 | nuget | repeated (settings, creation form and setup snippets read back per format) |
| 12-projects | 1 | 20.9 | platform | once |
| 13-ci | 1 | 57.0 | platform | once |
| 14-mirror | 2 | 16.7 | platform | once |
| 15-avatar | 1 | 3.6 | platform | once |
| 16-backups | 1 | 3.9 | platform | once |
| 17-deb | 4 | 16.0 | deb | mixed: deb signing-key and Indexes panels are format-only (client), form and snippets repeated |
| 18-virtual-members | 1 | 3.3 | platform | once |
| 19-general-settings | 3 | 8.3 | platform | once |
| 20-ansible | 2 | 10.6 | ansible | mixed: import tasks and tombstones are format-only (client), snippets repeated |
| 21-visibility | 4 | 16.7 | platform | once |
| 22-repository-setup | 1 | 9.2 | platform | once |
| 23-search | 4 | 12.0 | platform | once |
| **sum** | 45 | 326.5 | | Playwright wall 331.3 s of TestE1_11Journeys 333.0 s |

The access-long leg's Playwright part is per journey [V]; the four format specs are 60.5 s of 326.5 s. The vuln leg's six journeys are 50.2 s [V, `e2e-vuln-playwright-vuln`]. Journeys of the ansible leg (`web/e2e/ansible-docs`, `TestAnsibleDocsJourney` 27.3 s in run 1) have no Playwright report in the artifacts [V: not uploaded in the list], so only the Go test time is known.

### 3.5 UI layout checks (378 screens, 4,590 shots; run 2 job 136144955185; `ui-layout-report` 14611337061)

The step "Render every screen and check its layout" is 1,733 s in run 2 and 1,868 s in run 1 [V]. The report holds 378 screens (9 anonymous, 90 plain, 115 power, 164 admin) with 6 to 48 shots each (light and dark at three widths, 12 shots for most), 4,590 shots in all, and has **no timings** [V]. A per-screen time is therefore [I]: step time / shots = 0.378 s per shot in run 2 (0.407 in run 1). Screens whose name carries one format: npm 68 screens / 804 shots, oci 53 / 696, ansible 30 / 384, deb 21 / 294, nuget 19 / 252, generic 9 / 72; 178 screens / 2,088 shots name no format (shared screens and admin). Format-named screens are 200 of 378 screens and 2,502 of 4,590 shots (54.5 %), 945 s of the 1,733 s if every shot costs the same [I]. I class a rendered screen of a format as repeated (the same journey walked per format) [J]; the per-shot assumption is not verified (admin screens may cost more than a list).

### 3.5a Dialog states (`make ui-states`, measured 2026-11-25 on the development machine)

The states of the overlays at the component tier ([web-ui.md](web-ui.md#dialog-states), Q-UI-1140): 69 states and the harness's
two proofs, 13 spec files, each state opened twice (390x844 and 1440x900) and measured by `layoutFindings` and `innerScroll` once per window, 138 measurements
[V]. Measured with `/usr/bin/time` on the 20-core development machine at a load of 15 to 27 (other builders' runs beside it) [V]:

| Part | Time |
|---|---|
| The build (the target's esbuild bundle of the specs; warm cache / empty `.angular` and `node_modules/.vite`) | 1.5 to 2.9 s / 5.6 s |
| The 71 tests in the browser (Vitest's `Duration`) | 12.0 to 13.7 s, about 0.18 s per state with both windows (`innerScroll` added nothing measurable) |
| The whole target, wall | 16.6 to 23.4 s; with `UI_STATES_PICTURES=1` (138 pictures) 18.1 s |

Data: no state sends a byte over a network; each renders one dialog or one component against the in-memory fakes. The job `UI
layout checks` gains this step before its stack starts; the browser is the one the job installs already, and the step adds no
download (the provider package is a few hundred kilobytes of the workspace's `npm ci`). The runner's figure is the first CI run's
[I: about 30 to 60 s on the job's runner of the class `self-hosted`, from the development machine's 17 to 23 s]. Per format: 0 (no state is a
format's; a new format's dialog adds its own states once, a fraction of a second each).

The error notice's seven states (R-UI-216, `shared/error-notice.states.spec.ts`) came later: measured on 2026-11-25 at a load of about
17, 8.6 s for the file alone with pictures (`--include`), and the whole target 23.3 s wall with 78 tests [V], within the range above. They
add about 1.5 s of the target's browser time; with `UI_STATES_PICTURES=1` each state also takes its two pictures in the dark scheme.
The eighth, `notice-trying` (Q-UI-1197: the notice kept in place after Retry, "Trying again." and its failures in a row), came with
S9b: about 0.2 s of browser time; the whole target measured 18.9 s wall with 79 tests on 2026-11-25 [V]. S9b's unit cases are
component and unit tests in jsdom without data, milliseconds each: `paged-resource.spec.ts` (two, 0.06 s), one more case each in
`error-notice.spec.ts`, `problem-messages.spec.ts`, `virtual-members.spec.ts`, `groups.spec.ts`, `package-version.spec.ts`,
`session-store.spec.ts`, and the lint rule's case in `lint-rules.test.mjs` (2 ms); together under 1 s of `make web-test`, nothing per
format.

### 3.5b The checks of overlays and clipping (Q-UI-1170, measured 2026-11-25 on the development machine)

`overlay-out`, `sideways` in an overlay, `cut` and `separator-start` run inside `layoutFindings`, once per light picture; a state
with an open overlay is no longer grown (Q-UI-1171). Measured on the 20-core development machine at a load of 14 to 19 [V]:

| Part | Cost |
|---|---|
| The fixtures (`make ui-layout-test`, three engines, 2 workers) | +30 tests (10 per engine), +5.3 s of summed test time, +2.8 s wall (34.6 s at eb6481b94, 37.4 s after) |
| The new checks per measured picture (timed inside the page) | Over the whole English set (3,478 measured pictures) about 16 s: median 2 ms, the 95th percentile 12 ms; the downloads panel and the findings lists, the pages with the most separators, about 60 and 25 ms once the separators are measured all at once (4 s and 0.5 s when each was replaced alone) |
| The render step as a whole | Not set against a run of the base commit on the same machine [not measured]. The full English run took 44.0 min on the development machine at a load of 14 to 19, beside other builders. 373 light pictures of an open overlay (and their dark twins) are no longer grown, which saves their rounds of growing and their larger screenshots [I: a few seconds to a minute]; within the budget of +60 s |
| `make ui-states` | 71 states and proofs unchanged at about 21 s wall; one proof case more (a table of 900 px in a dialog), a fraction of a second |

Data: no new picture, no new screen; the fixtures are pages of a few elements. Per format: 0 (the checks measure every picture
the same; a format adds no fixture).

### 3.5c The page and shell states (Q-UI-1232, measured 2026-11-25 on the development machine)

The menus, drawers, closed sections, notices and pages of their own that need the full stack ([web-ui.md](web-ui.md#renders-of-every-screen),
`renders/states.ts`). Measured in one full English run on the 20-core development machine at a load of 10 to 20, 3 workers: 559
screen tests, 7,728 s of summed test time, 43.7 min wall, 7,292 pictures [V].

| Part | Cost |
|---|---|
| 27 new screens (one kind each; 8 of them at 1,440 and 390 px only, `only`) | 285.5 s of summed test time, 206 pictures [V]; the longest an Ansible repository's settings with every section and fold opened (32.5 s), the admin area's lists (21.0 s), a Debian repository's keys (20.1 s), a stored policy (19.1 s); a menu-only screen 2 to 8 s |
| 4 states added to three existing walks (`withStates`: two menus of the tab Upstream for two kinds, **Run a pass now**'s menu, a rule's refused save) | 18 pictures [V]; about 20 s of test time [I: about 1.2 s per state and width, from the walks' own times] |
| Together | about 305 s of 7,728 s, 3.9 % of the set's test time and 3.1 % of its pictures [I from the above]; about 1.7 min of the local run's wall time, and about **2.2 min of the CI step** "Render every screen and check its layout" [I: 3.9 % of its 55 min in run 45617683641] |
| `make ui-states` | 2 states more (`dist-tags.states.spec.ts`, a version's npm tags), 1.9 s for the file alone with its build [V]; under 1 s of the target's browser time |

The plan's budget for these states was 5 to 6 min of the CI step; the per-state widths and schemes (Q-UI-1172), the screens loaded at
two widths (Q-UI-1231) and one kind per state keep it at about 2.2. Data: the walks' answers are a few kilobytes changed in the browser,
the seed adds two policies and two CI mappings through the API (a few requests, well under a second). Per format: 0 (no state is a
format's; the command menus of the three formats without a picture are left out on purpose, H-2005 of api-and-ui.md).

### 3.5d The parts of the render set (Q-ARCH-1071, 2026-11-26)

The one leg of the layout checks passed its 75 minutes from 2026-11-26 08:00 on. Its step "Render every screen and check its layout" [V,
the steps' times]: 31.1 min on 2026-11-19 (run 44639156621), 30.3 on 11-20, 39.6 on 11-21, 42.4 on 11-22, 45.5 on 11-23, 51.8 on 11-24,
58.8 on 11-25 and 68.0 on 11-26 (run 45775689673, the last green one): 5.8 min a day for a week. The list reporter's line of each test
carries its time, so since that run the time per pair of screen and kind of user is known, not divided out of the step as in 3.5 [V,
job 139758696223]: 662 pairs, 192.7 min of summed test time in 64.9 min of wall with three browsers; by kind anonymous 10 pairs / 0.8
min, plain 208 / 55.8, power 170 / 52.1, admin 246 / 75.0, auditor 28 / 9.1; the longest pairs `package-version-signatures-oci` as plain
276 s, `profile-usage` as power 144 s, `repository-npm-remote-upstream` as admin 126 s. The leg's part that does not depend on the
screens: 4.5 min of setup, 3.0 of the stack's start and the seed, 0.4 of teardown; the fixtures and the dialog states 1.6 more [V].

The set now renders in five legs of every fifth pair ([releasing.md](releasing.md#ci), [web-ui.md](web-ui.md#renders-of-every-screen)).
Their times [I: the run's per-pair times replayed on three workers in the order Playwright hands them out, the 31 pairs added since at the
mean of 17.5 s; the replay of the run's own 662 pairs gives 64.7 min against the 64.9 measured]:

| Legs | Pairs per leg | Rendering per leg (min) | Job per leg (min) | Runner min per push, with the fixtures' job |
|---:|---|---|---|---:|
| 1 (before) | 693 | 67.8 | 77.3 (fixtures and states inside) | 77 |
| 4 | 173 to 174 | 15.4 to 18.5 | 23.3 to 26.4 | 106 |
| **5 (chosen)** | 138 to 139 | 12.5 to 14.5 | 20.4 to 22.4 | **114** |
| 6 | 115 to 116 | 9.7 to 14.4 | 17.6 to 22.3 | 123 |

Measured [V] on the development machine on 2026-11-26 (instance 7, the stack running, other builders' load): the five parts rendered
139, 139, 139, 138 and 138 pairs, 693 together, each once and none failed, in 14.8, 13.7, 13.6, 11.4 and 11.4 min, the seed included.
`make ui-renders-parts-check` (the parts' lists against the whole pass's, nothing rendered) takes about 5 s and runs in the fixtures' job.

Data: none added; each leg seeds the whole set (the seed is the same for every part) and renders its part's pictures. The cost per push
is about 37 runner minutes more (+48 %), four more stacks of 7.9 min and the fixtures' job of about 6 min; the layout checks end about 22
min after they start instead of 77. A leg grows by about 1.2 min a day at the week's rate and passes the 30 min at which a leg is added in
about 6 days. Per format: a format's screens spread over the five legs; what a format adds to the set (3.5) is divided by five in wall
time and unchanged in runner minutes.

### 3.6 Nightly legs, per test


#### e2e (bastion-long): job 135919761300 (3255 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestBastionLong_R-BAS_049_RollingUpgradeMain | 675.6 | platform | once | 0.0 | feature |
| TestBastionLong_R-BAS_049_Drain | 608.5 | platform | once | 0.0 | feature |
| TestBastionLong_R-BAS_049_RollingUpgradeBastion | 424.5 | platform | once | 0.0 | feature |
| TestBastionLong_R-BAS_049_ReplicaLost | 332.6 | platform | once | 147.8 | feature |
| TestBastionLong_R-BAS_049_MainFailover | 177.0 | platform | once | 0.0 | feature |
| TestBastionLong_R-BAS_049_AnonymousMissFlood | 121.9 | platform | once | 0.0 | feature |

Split: set-up steps 572, `make e2e` outside the listed tests 308, teardown 35; classes client 0 / repeated 0 / once 2192 (seconds outside subtests, 148, are inside these).

#### e2e (npm-ha): job 135919761224 (2954 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestNPMHA | 968.0 | npm | repeated | 83.9 | drain A, PG switchover, Valkey failover under npm load; same three in TestNuGetHA, E13/E14, vuln-long |
| TestNPMHA_R-REPO_053_WorkerLostMidMirrorPass | 654.1 | npm | repeated | 53.8 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |
| TestNPMHA_R-REPO_054_ThrottledUpstreamAcrossAMirrorMove | 366.8 | npm | repeated | 0.0 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |
| TestNPMHA_R-REPO_058_DrainOfTheNodeOfAMirrorPass | 177.2 | npm | repeated | 0.0 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |
| TestNPMHA_R-REPO_058_TwoMirrorClaimantsOneJobPerRepository | 129.3 | npm | repeated | 0.0 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |
| TestNPMHA_R-REPO_053_PostgresSwitchoverAndValkeyFailoverDuringAMirrorPass | 120.4 | npm | repeated | 0.0 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |
| TestNPMHA_R-REPO_058_ValkeyStoppedForAWholeMirrorPass | 66.5 | npm | repeated | 0.0 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |
| TestNPMHA_R-REPO_058_MirrorStatusFromEveryReplica | 25.4 | npm | repeated | 0.0 | mirror worker-pool HA (R-REPO-053/054/058), generic machinery on an npm remote; NuGet/OCI have twins |

Split: set-up steps 285, `make e2e` outside the listed tests 145, teardown 16; classes client 0 / repeated 2370 / once 0 (seconds outside subtests, 138, are inside these).

#### e2e (alerts): job 135919761082 (2628 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestAlerts_R-OPS_046_MaintenanceStaysQuiet | 371.1 | platform | once | 137.9 | feature |
| TestAlerts_R-OPS_046_OneReplicaCutOffFromValkey | 361.5 | platform | once | 0.0 | feature |
| TestAlerts_R-OPS_046_RoleBelowTwoReplicas | 348.2 | platform | once | 0.0 | feature |
| TestAlerts_R-OPS_046_ValkeyOutage | 339.7 | platform | once | 339.1 | feature |
| TestAlerts_R-OPS_046_BackupFailedAndStale | 299.0 | platform | once | 17.9 | feature |
| TestAlerts_R-OPS_046_PostgreSQLOutage | 269.5 | platform | once | 0.0 | feature |
| TestAlerts_R-OPS_046_ValkeyFailoverFlapping | 89.5 | platform | once | 51.7 | feature |
| TestAlerts_R-OPS_045_SlowValkey | 13.1 | platform | once | 0.0 | feature |

Split: set-up steps 318, `make e2e` outside the listed tests 203, teardown 15; classes client 0 / repeated 0 / once 1545 (seconds outside subtests, 547, are inside these).

#### e2e (npm-ha-soak): job 135949011795 (2480 s, failure)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestNPMHACrash | 1436.2 | npm | repeated | 86.5 | pod crash under npm load (also ColdCrash, R_REPO_053 pod crash) |
| TestNPMMirror_R-REPO_055_ClientsFirst | 357.9 | npm | repeated | 61.0 | R-REPO-055/059 generic mirror machinery with npm clients |
| TestNPMPerf (FAILED) | 276.2 | npm | client | 54.2 | measurement with npm-shaped k6 scripts, not an assertion that gates |

Split: set-up steps 253, `make e2e` outside the listed tests 113, teardown 44; classes client 222 / repeated 1647 / once 0 (seconds outside subtests, 202, are inside these).

#### e2e (bastion): job 135919761110 (2367 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestBastion_R-BAS_049_InterfaceMismatch | 520.6 | platform | once | 453.7 | feature |
| TestBastion_R-BAS_049_ColdAndWarmCache | 304.9 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_MainUnreachable | 249.4 | platform | once | 172.3 | feature |
| TestBastion_R-BAS_007_Journey | 134.2 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_FloodingAddress | 25.6 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_NetworkPolicy | 25.1 | platform | once | 0.2 | feature |
| TestBastion_R-BAS_049_TokenRevoked | 10.2 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_008_NeverReadsByInternal | 7.6 | platform | once | 7.5 | feature |
| TestBastion_R-BAS_049_ReadOnly | 7.2 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_RoutingByPath | 4.8 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_FailedAuthenticationTimeout | 4.5 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_EdgeLimit | 1.6 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_HiddenContent | 0.9 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_Setup | 0.2 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_ChartRender | 0.1 | platform | once | 0.0 | feature |
| TestBastion_R-BAS_049_Metrics | 0.1 | platform | once | 0.0 | feature |

Split: set-up steps 848, `make e2e` outside the listed tests 212, teardown 10; classes client 0 / repeated 0 / once 663 (seconds outside subtests, 634, are inside these).

#### e2e (vuln-long): job 135919761228 (2169 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestVulnLong_R-VULN_015_BlockHoldsThroughDrainVariants | 402.8 | platform | repeated | 94.2 | drain variants A-E, switchover and failover again, with vuln blocks |
| TestVulnLong_R-VULN_004_FullSyncDrainedMidRun | 254.9 | platform | once | 0.0 | vulnerability scanning feature |
| TestVulnLong_R-VULN_015_BlockHoldsDuringFailures | 183.5 | platform | repeated | 1.9 | drain variants A-E, switchover and failover again, with vuln blocks |
| TestVulnLong_R-VULN_026_RestoreKeepsBlocksAndTriage | 104.3 | platform | once | 95.4 | vulnerability scanning feature |
| TestVulnLong_R-VULN_011_SwitchoverDuringEvaluation | 96.0 | platform | repeated | 94.4 | drain variants A-E, switchover and failover again, with vuln blocks |
| TestVulnLong_R-VULN_027_ClientsWithValkeyStopped | 93.8 | platform | once | 56.9 | vulnerability scanning feature |
| TestVulnLong_R-VULN_004_FullSyncWorkerKilledMidRun | 87.9 | platform | once | 0.0 | vulnerability scanning feature |

Split: set-up steps 568, `make e2e` outside the listed tests 344, teardown 34; classes client 0 / repeated 492 / once 389 (seconds outside subtests, 343, are inside these).

#### e2e (ha-suite): job 135919761235 (2140 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestHASuite_ObjectStoreOutage | 924.7 | carrier | once | 41.9 | unique fault, npm load |
| TestHASuite_NodeLoss | 398.6 | carrier | repeated | 0.0 | fault also in E13/NPMHA/R_REPO_053/ColdCrash |
| TestHASuite_PostgresPrimaryLoss | 182.5 | carrier | repeated | 0.0 | fault also in E13/NPMHA/R_REPO_053/ColdCrash |
| TestHASuite_ValkeyPrimaryLoss | 158.3 | carrier | repeated | 0.0 | fault also in E13/NPMHA/R_REPO_053/ColdCrash |
| TestHASuite_AuditDrain | 72.8 | carrier | once | 47.0 | unique fault, npm load |

Split: set-up steps 277, `make e2e` outside the listed tests 112, teardown 14; classes client 0 / repeated 739 / once 909 (seconds outside subtests, 89, are inside these).

#### e2e (ha-faults): job 135919761455 (1951 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestHAFaults_ValkeySlow | 338.7 | carrier | once | 0.0 | unique fault, npm load |
| TestHAFaults_PostgresOutage | 308.0 | carrier | once | 0.0 | unique fault, npm load |
| TestHAFaults_ValkeySentinelQuorumLoss | 259.5 | carrier | once | 0.0 | unique fault, npm load |
| TestHAFaults_ValkeyPlannedFailover | 241.3 | carrier | repeated | 89.5 | also NPMHA/NuGetHA failover, NPMReplicas+E1_11 FLUSHALL |
| TestHAFaults_ValkeyCredentialMismatch | 206.1 | carrier | once | 0.0 | unique fault, npm load |
| TestHAFaults_ValkeyFlushAll | 152.0 | carrier | repeated | 0.0 | also NPMHA/NuGetHA failover, NPMReplicas+E1_11 FLUSHALL |

Split: set-up steps 306, `make e2e` outside the listed tests 115, teardown 24; classes client 0 / repeated 304 / once 1112 (seconds outside subtests, 90, are inside these).

#### e2e (edge): job 135970439080 (1787 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestEdge_R-HA_039_TransfersThroughTheIngressAndTheGateway | 1202.3 | platform | once | 807.0 | format-independent platform behaviour |

Split: set-up steps 386, `make e2e` outside the listed tests 182, teardown 17; classes client 0 / repeated 0 / once 395 (seconds outside subtests, 807, are inside these).

#### e2e (nuget-ha): job 136002849245 (1765 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestNuGetHA | 1145.0 | nuget | repeated | 108.0 | same scenarios as TestNPMHA with an added NuGet load |
| TestNuGetHA_R-REPO_058_MirrorStatusFromEveryReplica | 114.8 | nuget | repeated | 0.0 | same scenarios as TestNPMHA with an added NuGet load |

Split: set-up steps 312, `make e2e` outside the listed tests 173, teardown 20; classes client 0 / repeated 1152 / once 0 (seconds outside subtests, 108, are inside these).

#### e2e (drain-load-pooler): job 135984872568 (1517 s, failure)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestDrainLoadPooler (FAILED) | 768.5 | carrier | once | 119.3 | drain variants B,C,E,D under npm+NuGet load; vuln-long/-pooler repeat the variants |

Split: set-up steps 524, `make e2e` outside the listed tests 194, teardown 30; classes client 0 / repeated 0 / once 649 (seconds outside subtests, 119, are inside these).

#### e2e (backup): job 135939762897 (1475 s, failure)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestBackupDrain | 329.3 | platform | once | 122.9 | feature |
| TestBackupPITR_R-HA_025_R-HA_023_R-HA_054_PointInTimeRestoreOfThePlatformLayer (FAILED) | 290.7 | platform | once | 0.0 | feature |
| TestBackupDrill | 208.4 | platform | once | 0.0 | feature |
| TestBackupJournal | 131.2 | platform | once | 96.9 | feature |

Split: set-up steps 267, `make e2e` outside the listed tests 222, teardown 26; classes client 0 / repeated 0 / once 740 (seconds outside subtests, 220, are inside these).

#### e2e (ha-pooler): job 135919761302 (1450 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestHAPooler_InstanceLoss | 611.1 | carrier | once | 1.9 | pooler profile fault, npm load |
| TestHAPooler_Switchover | 281.3 | carrier | once | 116.2 | pooler profile fault, npm load |

Split: set-up steps 437, `make e2e` outside the listed tests 107, teardown 14; classes client 0 / repeated 0 / once 774 (seconds outside subtests, 118, are inside these).

#### e2e (drain-load-c): job 136002849235 (1258 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestDrainLoad | 757.4 | carrier | once | 111.9 | drain variants B,C,E,D under npm+NuGet load; vuln-long/-pooler repeat the variants |

Split: set-up steps 305, `make e2e` outside the listed tests 179, teardown 17; classes client 0 / repeated 0 / once 646 (seconds outside subtests, 112, are inside these).

#### e2e (drain-load-e): job 136002849145 (1248 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestDrainLoad | 760.1 | carrier | once | 111.2 | drain variants B,C,E,D under npm+NuGet load; vuln-long/-pooler repeat the variants |

Split: set-up steps 294, `make e2e` outside the listed tests 167, teardown 27; classes client 0 / repeated 0 / once 649 (seconds outside subtests, 111, are inside these).

#### e2e (drain-roles): job 135919761337 (1242 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestDrainRoles | 849.3 | carrier | once | 83.1 | drain per role under npm load (E14 is the k6 base case) |

Split: set-up steps 260, `make e2e` outside the listed tests 107, teardown 26; classes client 0 / repeated 0 / once 766 (seconds outside subtests, 83, are inside these).

#### e2e (upgrade): job 135919761134 (1239 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestUpgrade_R-HA_020_AcrossTwoBundles | 281.5 | platform | once | 276.1 | feature |

Split: set-up steps 781, `make e2e` outside the listed tests 164, teardown 13; classes client 0 / repeated 0 / once 5 (seconds outside subtests, 276, are inside these).

#### e2e (drain-load-b): job 136002849158 (1235 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestDrainLoad | 752.4 | carrier | once | 111.0 | drain variants B,C,E,D under npm+NuGet load; vuln-long/-pooler repeat the variants |

Split: set-up steps 302, `make e2e` outside the listed tests 164, teardown 17; classes client 0 / repeated 0 / once 641 (seconds outside subtests, 111, are inside these).

#### e2e (vuln): job 135919761372 (1075 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestVuln_R-VULN_022_Journeys | 122.3 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_027_AcrossReplicasAndWithoutValkey | 18.9 | platform | once | 0.4 | vulnerability scanning feature |
| TestVuln_R-VULN_011_R-VULN_018_AnAdvisoryAppearsLate | 15.1 | platform | once | 0.2 | vulnerability scanning feature |
| TestVuln_R-VULN_017_NPMAuditThroughEveryRepository | 11.0 | npm | client | 11.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_015_NPMBlockedInstallInTriage | 9.6 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_015_NPMBlockedInstallMalicious | 3.1 | npm | client | 2.9 | audit/blocked install wire shape of one client |
| TestVuln_SetupSurvivesABrokenForward | 1.6 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_011_PublishEvaluatesInTheCommit | 0.9 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_017_NPMAuditBody | 0.3 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_018_NuGetVulnerabilityInfoOnTheWire | 0.3 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_015_NuGetBlockedRestoreInTriage | 0.2 | npm | client | 0.0 | audit/blocked install wire shape of one client |
| TestVuln_R-VULN_010_ClaimedNamesAreInternal | 0.1 | platform | once | 0.0 | vulnerability scanning feature |
| TestVuln_R-VULN_018_NuGetRestoreAudit | 0 | npm | client | 0.0 | audit/blocked install wire shape of one client |

Split: set-up steps 619, `make e2e` outside the listed tests 259, teardown 14; classes client 11 / repeated 0 / once 158 (seconds outside subtests, 14, are inside these).

#### e2e (ci-ha): job 135919761343 (975 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestCIHASingleUse | 150 | platform | once | 0.0 | CI OIDC feature; npm and NuGet clients carry it |
| TestCIHAPodDelete | 108.0 | platform | once | 0.0 | CI OIDC feature; npm and NuGet clients carry it |
| TestCIHADrain | 58.9 | platform | once | 0.0 | CI OIDC feature; npm and NuGet clients carry it |
| TestCIHAValkeyStopped | 36.5 | platform | once | 0.0 | CI OIDC feature; npm and NuGet clients carry it |

Split: set-up steps 437, `make e2e` outside the listed tests 176, teardown 9; classes client 0 / repeated 0 / once 353 (seconds outside subtests, 0, are inside these).

#### e2e (failover): job 135919761310 (963 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestE13Failover | 383.5 | platform | once | 0.0 | base case of PG/Valkey faults under k6 (no format client) |
| TestE1_08RateLimits | 136.3 | platform | once | 48.1 | format-independent platform behaviour |

Split: set-up steps 322, `make e2e` outside the listed tests 109, teardown 12; classes client 0 / repeated 0 / once 472 (seconds outside subtests, 48, are inside these).

#### e2e (vuln-pooler): job 135919761262 (846 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestVulnPooler_R-VULN_015_BlockHoldsThroughDrainVariantD | 256.5 | platform | repeated | 101.7 | drain variants A-E, switchover and failover again, with vuln blocks |

Split: set-up steps 442, `make e2e` outside the listed tests 134, teardown 13; classes client 0 / repeated 155 / once 0 (seconds outside subtests, 102, are inside these).

#### e2e (oci-long): job 135952612111 (821 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestOCILong_R-REPO_058_DrainOfTheNodeOfAMirrorPass | 266.1 | oci | repeated | 0.0 | twin of TestNPMHA_R_REPO_058_DrainOfTheNodeOfAMirrorPass |
| TestOCILong_R-OCI_011_DrainOfThePodThatReceivesAPush | 61.3 | oci | client | 0.0 | drain of the pod that receives a 300 MiB push (OCI upload session) |

Split: set-up steps 349, `make e2e` outside the listed tests 131, teardown 14; classes client 61 / repeated 266 / once 0 (seconds outside subtests, 0, are inside these).

#### e2e (cold-crash): job 136002849164 (630 s, success)

| top-level test | s | owner | class | seconds outside its subtests | note |
|---|---:|---|---|---:|---|
| TestColdCrash | 151.0 | carrier | repeated | 0.0 | pod crash during a cold install, once per format client (npm, NuGet) |

Split: set-up steps 327, `make e2e` outside the listed tests 139, teardown 13; classes client 0 / repeated 151 / once 0 (seconds outside subtests, 0, are inside these).

#### e2e (upgrade-release): job 135919761207 (16 s, success)

No test report in the run (see Not measured).

### 3.7 Perf legs of the nightly (job 136046183602 Latency 548 s; job 136049926697 Vulnerability scanning performance 408 s)

`test-performance.json` [V] has `TestR_DEB_010_DelayBound` 208.0 s (two 104 s runs, with Valkey and with Valkey stopped; the Debian index delay bound: client class, a measurement), `TestChangePropagation` 44.4, `TestJobPickup` 30.1, `TestR_API_055_...` 22.6 + 16.1, `TestIdempotentCreate` 5.5, `TestR_NPM_010_ThousandVersions` 2.5, the rest under 2 s; step 452 s. `test-vuln-perf.json`: `TestR_VULN_028_PerformanceRun` 205.5 s (V-1 import of the npm archive 61.6, V-5 block check on a cached download 54.7, V-2 39.8, V-7 19.9) and `TestR_VULN_028_TheSeededInstallation` 11.7; the perf step is 315 s. Both legs spend 79 to 82 s on infra (stack, Go) of 408 to 548 s.

</details>

## 4. What a format costs today

### 4.1 Which minutes count for a format

For a format F, the cost of a push is the sum of: (a) **its own e2e leg**, whole (the leg exists only because F exists): set-up steps, `make e2e` outside the tests, teardown and in-test set-up are **infra**, the tests are client / repeated; (b) its **integration packages** (client and repeated by test name; `oci` also the 35 s conformance suite); (c) the format-specific **Playwright journeys** in access-long; (d) its **UI screens**, proportional to shots (repeated, [I][J]); (e) the **client image load** of F in shared legs that run F's client (ci, vuln: npm and NuGet; infra); (f) the cases of F inside shared legs (the vuln leg's npm and NuGet cases). Not included: unit tests (not additive), anything in the shared legs that would exist without F. The generic format has no leg and no client; it has 72 shots of UI.

For the nightly the same rule gives: npm = npm-ha + npm-ha-soak + 81 % of cold-crash (the 122.8 s npm subtest of 151 s; the NuGet subtest is 28.2 s; the split of the leg's infra by that ratio is [I]); NuGet = nuget-ha + 19 % of cold-crash; OCI = oci-long; Debian = the `TestR_DEB_010_DelayBound` 208 s of the Latency leg (the leg's own 340 s of other tests and infra is not Debian's); Ansible and generic = none [V: no leg or test of the nightly exists for them].

**The carriers.** Eight nightly legs (ha-suite 35.7 min, ha-faults 32.5, ha-pooler 24.2, drain-roles 20.7, drain-load-b/-c/-e 20.6 / 21.0 / 20.8, drain-load-pooler 25.3 = 200.7 min) inject platform faults under the npm client's load (drain-load: npm and NuGet together). The fault is not a format's, so I do not charge them to npm or NuGet in the tables; the clients are the carrier. They are the reason the answer to "what does a format cost" has a second reading: if each new format is added to the load as NuGet was (section 5, candidate 2), each of these legs also grows (the client image load of two formats is already 74 to 112 s per drain leg).

### 4.2 Per push (minutes of runner time), run 2 = today

| format | infra min | client min | repeated min | total min |
|---|---:|---:|---:|---:|
| npm | 11.3 | 5.7 | 21.1 | **38.0** |
| nuget | 18.0 | 7.2 | 7.1 | **32.3** |
| oci | 11.3 | 6.2 | 8.5 | **25.9** |
| deb | 9.8 | 11.7 | 3.9 | **25.4** |
| ansible | 12.8 | 5.3 | 3.2 | **21.2** |
| generic | 0.0 | 0.0 | 0.5 | **0.5** |
| **sum of six** | 63.1 | 36.0 | 44.2 | **143.3** |
| **mean of six** | 10.5 | 6.0 | 7.4 | **23.9** |
| **mean of the five with a client** (without generic) | 12.6 | 7.2 | 8.7 | **28.6** |

Composition of the six rows (min; the parts are `fcost.py` output, run 2) [I]:

| format | own e2e leg wall | of which infra | client | repeated | integration client / repeated | Playwright journeys (client / repeated) | UI (shots) | client images in ci + vuln | vuln leg cases | oci conformance |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| npm | 23.8 | 9.9 | 1.1 | 12.8 | 3.8 / 2.8 | 0.0 / 0.4 | 5.1 (804) | 1.3 | 0.8 | 0.0 |
| nuget | 22.4 | 14.4 | 3.7 | 4.2 | 3.4 / 1.1 | 0.0 / 0.2 | 1.6 (252) | 3.7 | 0.0 | 0.0 |
| oci | 16.0 | 11.3 | 1.7 | 3.1 | 3.9 / 1.0 | 0.0 / 0.0 | 4.4 (696) | 0.0 | 0.0 | 0.6 |
| deb | 12.3 | 9.8 | 2.2 | 0.4 | 9.4 / 1.6 | 0.1 / 0.1 | 1.9 (294) | 0.0 | 0.0 | 0.0 |
| ansible | 16.1 | 12.8 | 2.8 | 0.5 | 2.3 / 0.2 | 0.1 / 0.1 | 2.4 (384) | 0.0 | 0.0 | 0.0 |
| generic | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 / 0.0 | 0.0 / 0.0 | 0.5 (72) | 0.0 | 0.0 | 0.0 |

The infra figure of the own leg includes set-up steps, `make e2e` outside the tests, teardown, the in-test set-up (seconds outside subtests) and the smoke test of the client pods. "Client images" are the npm and NuGet client image loads in the ci and vuln legs. The vuln column is the format's own cases in the vuln leg.

Run 1 (44646960056, without the limits cases, with queue contention of the nightly), for the spread:

| format | infra min | client min | repeated min | total min |
|---|---:|---:|---:|---:|
| npm | 10.4 | 5.4 | 21.8 | **37.6** |
| nuget | 18.7 | 5.5 | 6.3 | **30.5** |
| oci | 9.5 | 5.1 | 8.9 | **23.5** |
| deb | 10.4 | 11.6 | 4.0 | **26.0** |
| ansible | 10.7 | 4.9 | 3.4 | **19.0** |
| generic | 0.0 | 0.0 | 0.5 | **0.5** |
| **sum of six** | 59.7 | 32.5 | 44.9 | **137.1** |
| **mean of six** | 10.0 | 5.4 | 7.5 | **22.9** |
| **mean of the five with a client** (without generic) | 11.9 | 6.5 | 8.9 | **27.3** |

Run 1 vs run 2 per format leg (own leg wall, s): npm 1382 / 1428, nuget 1243 / 1341, oci 801 / 961, deb 765 / 739, ansible 821 / 964 [V]; run 2 adds the limits cases (npm 17.0, NuGet 35.0, OCI 56.4 over three tests, Debian 68.1, Ansible 22.3 s) and the OCI probe 2.0 s.

What each leg is made of, run 2, in seconds [V for the steps and tests, I for the gap and the in-test set-up]: **npm** 1428 = set-up steps 324 + `make e2e` outside the tests 134 + teardown 17 + in-test set-up 118 + client tests 65 (matrix 48.1, limits 17.0) + repeated 766 + smoke 4; **nuget** 1341 = 419 + 285 + 24 + 134 + client 225 (push 36.1, push retry 54.8, limits 35.0, restore, remote, unlist/relist/delete, clients, exe) + repeated 254; **oci** 961 = 454 + 155 + 14 + 52 + client 100 + repeated 185 (mirror pass with clients 174.3) + smoke 1; **deb** 739 = 354 + 103 + 12 + 116 + client 131 (local 90.7, rotation 60.7, limits 68.1, before taking out their in-test set-up) + repeated 22 + smoke 1; **ansible** 964 = 567 + 100 + 15 + 80 + client 168 + repeated 31 + smoke 3. The Ansible leg also installs Node and the Playwright browser (120 s) for its docs journey, the NuGet leg frees disk (108 s) and loads a 107 s client image.

### 4.3 Per nightly (44625277245)

| format | infra min | client min | repeated min | total min |
|---|---:|---:|---:|---:|
| npm | 26.4 | 3.7 | 69.0 | **99.1** |
| nuget | 11.7 | 0.0 | 19.7 | **31.4** |
| oci | 8.2 | 1.0 | 4.4 | **13.7** |
| deb | 0.0 | 3.5 | 0.0 | **3.5** |
| ansible | 0.0 | 0.0 | 0.0 | **0.0** |
| generic | 0.0 | 0.0 | 0.0 | **0.0** |
| **sum of six** | 46.4 | 8.2 | 93.1 | **147.6** |
| **mean of six** | 7.7 | 1.4 | 15.5 | **24.6** |
| **mean of the five with a client** (without generic) | 9.3 | 1.6 | 18.6 | **29.5** |

| format | legs (job ids) | wall min |
|---|---|---:|
| npm | e2e (npm-ha) 135919761224; e2e (npm-ha-soak) 135949011795 (red: `TestNPMPerf` failed); 81 % of e2e (cold-crash) 136002849164 | 49.2 + 41.3 + 8.5 = 99.1 |
| nuget | e2e (nuget-ha) 136002849245; 19 % of e2e (cold-crash) | 29.4 + 2.0 = 31.4 |
| oci | e2e (oci-long) 135952612111 | 13.7 |
| deb | the DelayBound test of Latency 136046183602 | 3.5 |
| ansible, generic | none | 0 |

npm and NuGet have no client-class test in these legs except `TestNPMPerf` (276 s, red, npm-shaped k6 scripts; client by my call) and none at all for NuGet: everything else in their nightly legs is drain, switchover, failover, pod crash and mirror HA with the client as load (repeated). Of the 99.1 npm minutes, 26.4 are infra (set-up steps, `make e2e` outside the tests, teardown, in-test set-up; cold-crash's share included).

### 4.4 Mean and projection to 25 formats under today's structure

Model [I]: total(25) = total(now) + 19 x mean, with the mean of the five formats that have a client as the main line (generic has no leg and no client and is not what a new registry type looks like) and the mean of six as the lower line. Today's structure means: every format gets one leg of its own in the push, its integration packages in shard b (the Makefile rule: a new package lands in b), its UI screens in the layout job, and no nightly leg unless it copies npm, NuGet or OCI.

| quantity | now | at 25 formats, mean of five | at 25 formats, mean of six |
|---|---:|---:|---:|
| runner min per push | 396.0 | 939 | 850 |
| runner min per nightly (if new formats follow the mean of what exists) | 682.9 | 1244 | 1150 |
| runner min per nightly if every new format has Debian's or Ansible's shape (no nightly leg) | 682.9 | 683 | 683 |
| runner min per nightly if every new format copies OCI's (oci-long, 13.7) | 682.9 | 943 | |
| runner min per nightly if every new format copies npm's (99.1) | 682.9 | 2566 | |

The push mean of five is 12.6 infra + 7.2 client + 8.7 repeated min. Assumptions to check: the mean carries over to the new formats (the five formats range 21.2 to 38.0 min); legs for the new formats are not skipped by the path filter (both measured pushes touched shared code); the per-shot UI time is uniform; unit tests add nothing.

**Wall clock if the legs keep their shape** [I]:

* **Push e2e legs run in parallel** and each is 12.3 to 23.8 min, so they do not lengthen the push by themselves, but they need runners: a push already holds 6 large and 4 GitHub-hosted legs at once; at 25 formats it is 25 format legs + 5 shared ones. The large pool is the unknown: it served 5 to 6 concurrent legs in the two pushes, queued `e2e (npm)` 895 s in run 1 while the nightly held it, and in the nightly runs three at a time by design (Q-ARCH-431). Not measured: the pool size.
* **Integration shard b is the critical path today** (run 2: 41.1 min, then the coverage jobs to 51.7 min). One average format adds 362.6 s (mean of the five; client+repeated of its packages, `oci` incl. conformance) = 6.0 min, shard b has 45 min of job timeout and 40.9 min of run: the first additional format of average size crosses it; 19 more formats make shard b about 156 min and the push about 166 min wall (shard b ends, then 10.6 min of coverage jobs, as in run 2) unless the shards are cut again. (Deb alone is 658 s, Ansible 152 s: the spread is large.)
* **UI layout** is 32.7 min as a job (run 2; the render step 28.9) with a 75-min job timeout; one format adds 486 shots x 0.378 s = 183 s on average; the job passes 75 min after about 14 more formats and is about 91 min at +19 [I]. It passed 75 min on 2026-11-26 with screens of the existing formats alone and renders in five legs since ([3.5d](#35d-the-parts-of-the-render-set-q-arch-1071-2026-11-26)): a format's 183 s are about 37 s of each leg's wall time.
* **Nightly:** the wall clock is lane 1 (243.8 min of legs, 299.2 min observed). If the new formats' nightly legs are spread over the three lanes the lanes grow by (19 x 29.5)/3 = 187 min each in the mean-of-five case, so the wall goes from 299 to about 486 min; if every new format follows OCI's shape it is +87 min.

## 5. The largest candidates

Ranked by the larger of the push and nightly minutes at stake: the measured minutes of the thing that would move. What is saved depends on the replacement tier, which I did not design or measure. "True" = what would have to be true to move it to a cheaper tier.

| # | candidate | per push (min) | per nightly (min) | evidence | what would have to be true |
|---|---|---:|---:|---|---|
| 1 | **Fixed cost of every kind leg**: set-up steps, `make e2e` outside the tests, teardown (contains `make dev-up`, image loads, client images, Go modules, Playwright install, free disk) | 95.1 of 172.6 e2e min; `dev-up` alone 25.8; mean 596 s per format leg | 240.6 of 658.1; `dev-up` alone 67.5 | section 2.3, `cats_*.csv`; the five format legs spend 469 to 728 s in fixed steps = 33 to 71 % of their wall (OCI 623 of 961 s, Ansible 682 of 964 s, npm 475 of 1428 s) | a format's tests must not need a cluster bring-up of 2.5 to 3.5 min of their own, or sets of several formats must share a cluster without one format's leg growing with another's (the rule "a format adds a constant and nothing to another format's leg"); or the cases of a format move to a tier without a cluster |
| 2 | **Drain, switchover, failover, pod crash under a client load, repeated across 12 legs** (npm-ha, nuget-ha, drain-load-b/-c/-e/-pooler, ha-suite, ha-faults, ha-pooler, drain-roles, cold-crash, npm-ha-soak) | | 331.2 (of which fixed steps 97.6, tests I class repeated 106.0) | drain variant A in `TestE14Drain` 191.8 s (the k6 base case, push), `TestNPMHA` 588.5, `TestNuGetHA` 637.0, `TestDrainRoles` 766.2, `TestVulnLong...DuringFailures` 85.8, `TestCIHADrain` 58.9; the PostgreSQL switchover in `TestE13Failover` 50.0, `TestNPMHA` 152.7, `TestNuGetHA` 184.5, `TestHAPooler_Switchover` 165.2, `TestVulnLong...` 53.9, `TestNPMHA_R_REPO_053...` 120.4; the Valkey failover in `TestNPMHA` 143.0, `TestNuGetHA` 215.4, `TestHAFaults_...PlannedFailover` 151.8, `TestVulnLong...` 41.9, `TestNPMReplicas` 49.7, `TestAlerts...Flapping` 89.5; the header of `test/e2e/nuget_ha_test.go`: "The scenarios are those of TestNPMHA, and so are their pass conditions; the shared harness (drainVariantA, runLoadFault) is in npm_ha_test.go" | one platform leg runs each scenario once with one load; a format's client contributes a one-client case (like the R-LIM-020 cases, 17 to 68 s) |
| 2a | of which **drain-load-b / -c / -e / -pooler**: the same `drainUnderLoad` flow, a different victim, npm and NuGet load, four clusters | | 87.6 (20.6 + 21.0 + 20.8 + 25.3); fixed steps of b, c, e: 482.6 / 500.6 / 487.9 s | test times 752.4, 757.4, 760.1, 768.5 s; `make e2e SET=drain-load` already runs b, c and e in one cluster for local runs (`deploy/dev/e2e.sh`); vuln-long repeats variants B, C, E (96.5 + 123.6 + 88.4 s) and vuln-pooler variant D (154.8 s) with vuln blocks | one cluster for b, c, e would save at most 2 x 490 s = 16.3 min (CI cut it into three legs after the runner of the single leg was lost three times, Q-HA-242, comment in `e2e.sh`) |
| 2b | of which **npm-ha + nuget-ha**: the same three scenarios twice | | 78.6 (49.2 + 29.4) | nuget-ha tests I class repeated 1151.7 s of 1765 s: drain A 637.0, Valkey failover 215.4, switchover 184.5, mirror status 114.8; npm-ha 2370 s, among them seven mirror-pool tests 1539.7 s (R-REPO-053/054/058; OCI has a twin, `TestOCILong_R_REPO_058...` 266.1 s) | R-ARCH-024's criterion is a drain "during 100 npm ci, 20 npm publish, 100 dotnet restore and 20 dotnet nuget push" (`nuget_ha_test.go`): that load can be one platform test, and NuGet's own case shrinks to its client |
| 2c | of which **npm-ha-soak** | | 41.3 | `TestNPMHACrash` 1436.2 s (ten force-deletes over 20 min), `TestNPMMirror_..._ClientsFirst` 357.9 s again, `TestNPMPerf` 276.2 s (red) | pod crash is a platform scenario (also `R_REPO_053` pod crash 189.8 s, `TestColdCrash` 151.0 s); a soak is time-based by design |
| 3 | **Format-independent behaviour inside the five format e2e legs** (class repeated) | 21.0 (npm 12.8, NuGet 4.2, OCI 3.1, Ansible 0.5, Debian 0.4) plus 8.3 of in-test set-up in those legs | the mirror clients-first again in npm-ha-soak 6.0 | `TestNPMMirror_..._ClientsFirst` 358.9 + NuGet 126.6 + OCI 174.3 = 659.8 s (11.0 min) per push; the same behaviour exists in process: `test/integration/mirror/preempt_test.go:54` `TestR_REPO_055_AClientFetchThatHasToWaitPreemptsThePassOfAnotherProcess` 7.7 s; replicas: `TestNPMReplicas` 191.9 s vs `TestR_NPM_010_PublishThenViewOnAnotherReplica` 19.3 s (`documents_test.go:53`, 100 rounds, Valkey up and down) + `TestR_ARCH_012_ReadYourWritesAcrossReplicas` 7.7 s; collapse: `TestNPMCollapse` 128.1 s vs `TestR_NPM_024_FiftyConcurrentRequestsOverThreeReplicasCauseOneUpstreamRequest` 6.8 s + `proxy/TestCollapse` 37.1 s + `TestR_NUGET_031_...` 12.5 s | the in-process or kit case asserts the same thing without a client, and the e2e keeps the one case only a client can show |
| 4 | **Integration packages of the formats**, in the three shards | 30.2 (client 23.5 + repeated 6.7; Debian 658 s, npm 399.6, NuGet 272.3, OCI 331.6, Ansible 151.9 s) | | section 3.2; shard b 40.9 of its 45 min | Debian's index and signing tests are format-only and stay; the repeated ones (action maps, `TestR_AUTHZ_007_DebActionMapOfTheVirtualClass` 38.0 s; replica and concurrency tests) are what the kit (check 13 authz paths, check 5 merge) already runs per format at the unit tier |
| 5 | **UI render set** | 32.7 (the render step 28.9; format-named screens 15.7, 54.5 % of the shots) | | section 3.5 | the render per format covers what the format's page adds (setup, settings, package page); layout of shared components is walked once |
| 6 | **vuln-long + vuln-pooler**: the blocks through drain variants A to E and the failovers, two legs | | 50.2 (36.1 + 14.1); the repeated tests 938.8 s = 15.6 | `TestVulnLong_..._BlockHoldsThroughDrainVariants` 402.8 s, `...DuringFailures` 183.5, `...SwitchoverDuringEvaluation` 96.0, `TestVulnPooler...` 256.5 | the block is asserted at a cheaper tier and the drain is not repeated per feature |
| 7 | **Client images loaded per leg** (`Load or build the ... clients image`) | 11.9 (ci 146 s, vuln 154 s of it) | 24.2 (npm 7.1, NuGet 15.3, OCI 1.8) | cats; NuGet 36 to 146 s per leg | no leg loads an image of a format it does not test; the NuGet image dominates |
| 8 | **Alerts leg** (not format-driven): fault injection again for the alert rules | | 43.8 | `TestAlerts_R_OPS_046_*` 90 to 372 s each | outside this question, listed for completeness |
| 9 | **A skipped leg still runs a runner**: `e2e (bastion)` when the change does not touch the Bastion | 2.2 (130 s, 117 s of it freeing disk on a GitHub-hosted runner) | | run 1 job 135994996062, run 2 job 136146450996 | the "does it apply" check runs before the runner is taken |
| 10 | **Three integration stacks** (a suspect) | 3.0 for `make integration-up`; 9.2 with every non-test step of the shards | | section 2.3 | refuted as a large cost: not worth moving |

### 5.1 The known suspects, confirmed or refuted

* **"The load legs repeat the same drain, failover and switchover scenario with npm and with NuGet": confirmed.** `nuget-ha` (29.4 min) runs the three scenarios of `npm-ha` (49.2 min) with a NuGet load added; the code header says so and the harness is shared (`drainVariantA`, `runLoadFault`). The four drain-load legs differ in the victim, not the load (test times 752.4 to 768.5 s); variants B, C, E and D are repeated again in vuln-long (402.8 s) and vuln-pooler (256.5 s).
* **"Every kind leg brings up its own cluster with the whole dev profile and loads its client images": confirmed.** `make dev-up` is 154 to 186 s in each of the 9 push legs and 147 to 210 s in each nightly leg (25.8 and 67.5 min); client images are 11.9 min per push and 24.2 per nightly; all fixed steps are 55 % of the e2e minutes of a push (37 % of the nightly's).
* **"Each of the three integration shards brings up the stack": true, and not large.** 46 to 64 s of stack per shard (3.0 min of the shards' 113.3 per push); all non-test steps of a shard are 182 to 187 s. The shards' real issue is that b is at 40.9 of 45 min.
* **"The UI render set grows with every screen and every format": confirmed as an inference.** 1,733 s in the render step (run 2); format-named screens are 54.5 % of the shots; one average format adds 183 s; the per-shot time is not measured.

## 6. What a format's own leg is made of, one line each [V/I]

* **npm** (23.8 min): the matrix of 8 client versions (48 s) and the limits case (17 s) are the only client tests; 766 s repeated (mirror clients-first 359 s, replicas 192 s, collapse 128 s, upstream 84 s, endpoint 80 s); infra 10.0 min.
* **NuGet** (22.4 min): client 225 s (push, push retry, limits, restore, remote, unlist/relist/delete), repeated 254 s (mirror 127 s, collapse 69 s, endpoint 43 s, partner 40 s); infra 14.4 min with a 107 s client image and 108 s of disk freeing on the GitHub-hosted runner.
* **OCI** (16.0 min): client 100 s (limits 56, engine mirror 41, local 21), repeated 185 s of which the mirror pass with clients 174 s; infra 11.3 min with the 90 s OCI client images.
* **Debian** (12.3 min): client 131 s (local 91, rotation 61, limits 68), repeated 22 s (virtual 76 s of which about 64 s is seeding and waiting for the index merge); infra 9.8 min.
* **Ansible** (16.1 min): client 168 s (auth 69, local 54, docs 27 + 7 + 15 + 7 + 5), repeated 31 s; infra 12.8 min including Node and Playwright 120 s.

## 7. Not measured

* **Unit tests per format.** `unit tests` is one job of 12.3 min (run 2); 234 packages overlap, so package times do not add; only the package wall times of section 3.3 are known (upper bounds). The share of the unit job that is a format's was not measured. Measured since, from a later run: [section 14](#14-the-three-measurements-section-7-left-open-from-the-runs-of-2026-11-20-to-2026-11-23-read-on-2026-11-24).
* **UI layout per screen.** The report has no timings; the per-screen time is step time divided by shots. Admin vs list screens could differ. Measured since from the job's log, per pair of screen and kind ([3.5d](#35d-the-parts-of-the-render-set-q-arch-1071-2026-11-26)).
* **Playwright per spec** for every leg except access-long and vuln (run 2); the Playwright journeys of the ansible-docs, bastion and upgrade legs are inside their Go test times only; run 1 has no Playwright report downloaded.
* **Integration classes** are by test name for the 14 format packages only (`npm`, `nuget`, `ansible*`, `deb*`, `oci*`, `registry`); I did not read the 1,645 tests. I read two to confirm the e2e duplicates (`documents_test.go:53`, `preempt_test.go:54`). Format tests inside platform packages (`mirror`, `proxy`, `virtual`, `itempublish`, `vuln*`) are counted as platform. The `registry` package is counted as OCI by its name and by the Makefile shard rule (oci%, registry), not by reading it.
* **Class calls of e2e tests** are by rule and by test name with reading of the code only for `TestDebVirtual`, `TestNPMReplicas` (head), `TestNPMUpstream` (subtest names), `TestNuGetHA` (header), the limits tests (`limits_clients_env_test.go`, `oci_limits_test.go` headers) and `e2e.sh`. Mixed tests (npm replicas: ETag/validator parts are npm-document specific, the replica and Valkey parts are not) are called by their larger part; I did not split a test.
* **Subtests** beyond the first level are not summed; a few 100+ s subtests are named in the notes.
* **Skipped legs:** `fuzz npm`, `fuzz nuget` (30 min each) and `compatibility with the latest release` were skipped in both pushes (0 s) and are not in the nightly; their cost when they run is unknown. `upgrade-release` took 16 s because the schedule leaves it out.
* **Billing and runner size.** Minutes are wall time; large, GitHub-hosted and self-hosted minutes are counted alike. CPU, memory and cost per class were not measured.
* **Queues and pool size.** Queue time per job is in the job records but I did not derive the size of the large pool; the nightly's is three by design, the push's is between 5 and 6 concurrent in the two samples.
* **One nightly sample, two push samples.** The nightly was red on `e2e (backup)` (including its point-in-time step), `e2e (npm-ha-soak)` and `e2e (drain-load-pooler)`; their durations are counted as measured, and a green run could be shorter or longer. The nightly is a dispatch of all legs; a scheduled night may skip legs. Spread between the two pushes: e2e legs +11.5 min (161.1 to 172.6); the added limits cases and the OCI probe are 200.8 s (3.3 min) of test time; the rest (and the effect of the nightly dispatch that shared the large runners during run 1) was not isolated [I].
* **A push that touches one format** (computed from the parts of later runs in [section 14](#14-the-three-measurements-section-7-left-open-from-the-runs-of-2026-11-20-to-2026-11-23-read-on-2026-11-24), with a second and a third nightly). Both pushes touched shared code and ran every leg they started (none of the e2e legs gates the release, Q-ARCH-457); the cost of a push that touches only one format's files is not measured. Under the rule of [Q-ARCH-540](../spec/architecture.md#q-arch-540--a-formats-e2e-leg-runs-on-a-push-only-when-that-formats-own-code-changed) such a push runs that format's leg and `push`, `access-long` and `ci`, which run on every change; npm's leg runs for npm's own paths or shared code, and NuGet's is scoped like the others since 08d6f014b ([which e2e legs a change runs](releasing.md#which-e2e-legs-a-change-runs)).
* **Generic format.** No leg, no client; its UI screens (72 shots) are the only cost I can attribute to it, and the `files` integration package is counted as platform.
* **Per-format cost of the carrier legs** is not charged to a format (section 4.1); if one charges drain, failure and pod-crash legs to the client that loads them, npm would carry 99.1 + (ha-suite, ha-faults, ha-pooler, drain-roles) 113.1 = 212.2 min and NuGet would add half of four drain-load legs; I did not compute that variant as a table.
* **Playwright and other artifacts not opened:** the `e2e-ansible-artifacts-ansible`, `e2e-*-artifacts-*` and `profile-*` artifacts (logs, cluster evidence, coverage profiles); the job logs were read only to confirm the path-filter decision of eight legs.
* **Wall-clock projections** rest on three assumptions I could not check: the large pool grows with the number of legs, the per-format test time of a new format equals the mean, and a new format's integration package lands in shard b (Makefile comment, not a test).

## 8. Integration shards on 2026-11-21 (run 44948103795)

Measured once on 2026-11-21 for [Q-ARCH-565](../spec/architecture.md#q-arch-565--the-integration-tests-run-in-four-shards-the-fourth-takes-the-virtual-areas-and-the-security-checks), the cut of the fourth shard, from Test and Release 44948103795 (push of `452cd31f`, the day's last whole run, green). Sources: the job and step records (`gh api repos/guided-traffic/quader/actions/runs/44948103795/jobs --paginate`, `.../actions/jobs/<id>`) and the `go test -json` streams of the artifacts `test-integration-a`, `-b` and `-c`; a package's seconds are the `Elapsed` of its package-level `pass` event, its tests the top-level tests with an end event [V]. The three shards ran by the rule of Q-ARCH-544; "shard now" is where the rule of Q-ARCH-565 puts the area. Nothing was re-run for it.

### 8.1 The jobs, and the part of a shard that does not depend on its packages (seconds)

| shard | job id | runner class | job | job set-up | checkout | Go | Go modules | tools, Compose, Docker login | `make integration-up` | test step | of it: build of the test binaries to the first event | packages | between packages | OCI conformance with its upload | uploads | `make integration-down` | post steps and gaps |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| a | 136979786150 | self-hosted | 2291 | 2 | 7 | 21 | 59 | 1 | 48 | 2129 | 43.4 | 2064.3 | 21.3 | 0 | 7 | 11 | 6 |
| b | 136979786248 | self-hosted | 2601 | 3 | 2 | 31 | 81 | 2 | 53 | 2403 | 97.5 | 2205.7 | 99.8 | 0 | 10 | 11 | 5 |
| c | 136979786105 | self-hosted | 2525 | 3 | 8 | 26 | 78 | 2 | 41 | 2314 | 46.3 | 2218.5 | 49.2 | 29 | 8 | 10 | 6 |

* **The jobs [V]:** a 38.2, b 43.4 and c 42.1 minutes of their 45, 123.6 runner minutes; b stood six seconds under the 43.5 minutes at which the plan cuts a shard (section 12 A of [promt.md](../projektplan/promt.md)). All three ran on the `self-hosted` class (`wds18-small-runner-*`) and started 15 s after the run.
* **The steps outside `make test-integration` [V]:** 162, 198 and 182 s (a, b, c; c without the 29 s of the OCI conformance suite and its upload): checkout and Go with its modules 87 to 114 s, the stack's start 41 to 53 s, uploads 7 to 10 s (b's coverage profile is the largest, 32 MB), teardown 10 to 11 s.
* **Inside the test step [I]:** from the step's start to the first event of the JSON stream is the build of the test binaries with `-coverpkg=./...` and the start of the first package (43 to 98 s, larger with more packages); between the end of one package and the start of the next are 21 to 100 s, about 2.4 s per package.
* **A model [I]:** job ≈ 210 s + 3.7 s per package + the packages' seconds (+ 27 s for the OCI conformance suite on c). It gives 2308, 2575 and 2530 s against the measured 2291, 2601 and 2525 (within 26 s). The 210 s are the steps outside the test step (about 180 s) and the part of the build that does not grow with the packages (about 30 s, fitted from a and b); the 3.7 s per package are its share of the build and its start.
* **What a fourth shard costs per push [I]:** its fixed part, about 210 s = 3.5 runner minutes on the `self-hosted` class; the packages' seconds and the 3.7 s per package do not change with the number of shards. The four shards below hold about 127.0 runner minutes against the model's 123.5 for three. The shards run side by side, so the wall time of the integration tier falls from b's 43.4 to the longest of the four, about 33 minutes; in that run `UI layout checks` ended at 42.2 minutes, so the push's wall time would fall by about a minute and the coverage jobs, which need the shards, could start about ten minutes earlier.

### 8.2 The four shards of Q-ARCH-565, from these package times [I]

| shard | what the rule gives it | packages | packages' seconds | expected job (min) |
|---|---|---:|---:|---:|
| a | every `<format>local`; `coordination`, `mirror`, `npm`, `nuget`, `proxy` | 8 | 1694 | 32.2 |
| b | every other area, every new platform area among them | 32 | 1518 | 30.8 |
| c | every `<format>remote`; `api`, `backup*`, `restore*`, `storage*`, `postgres`, `blob`, `valkey`, `bastion*`; the OCI conformance suite | 16 | 1666 | 32.7 |
| d | every `<format>virtual` (the platform area `virtual` among them); `ci`, `vuln*`, `malware*`, `sign*` | 16 | 1611 | 31.3 |

Every shard keeps 12 to 14 minutes below its limit of 45 and about 11 to 13 below the mark of 43.5. A new format adds one area to each of a, c and d; a new security check adds to d; every other new platform area adds to b.

### 8.3 Every package (seconds of the package-level event)

<details>
<summary>The 72 integration packages of run 44948103795 by their shard then, with their shard now</summary>

| area (test/integration/...) | shard then | s | top-level tests | shard now |
|---|---|---:|---:|---|
| npm | a | 415.4 | 107 | a |
| ci | a | 370.7 | 95 | d |
| mirror | a | 268.5 | 58 | a |
| nuget | a | 255.7 | 93 | a |
| coordination | a | 243.1 | 29 | a |
| deblocal | a | 211.2 | 26 | a |
| proxy | a | 190.3 | 70 | a |
| ansiblelocal | a | 57.1 | 22 | a |
| ocilocal | a | 52.4 | 18 | a |
| **sum of shard a** | | **2064.3** | **518** | |
| debvirtual | b | 271.3 | 14 | d |
| repository | b | 176.2 | 38 | b |
| limits | b | 126.2 | 18 | b |
| files | b | 115.8 | 57 | b |
| postgres | b | 102.6 | 51 | c |
| identity | b | 101.8 | 64 | b |
| querybudget | b | 85.7 | 7 | b |
| malware | b | 80.9 | 19 | d |
| authz | b | 80.1 | 50 | b |
| uploads | b | 78.0 | 18 | b |
| blob | b | 72.8 | 18 | c |
| packages | b | 71.2 | 18 | b |
| transfer | b | 65.6 | 14 | b |
| projects | b | 64.8 | 26 | b |
| login | b | 55.7 | 23 | b |
| server | b | 53.0 | 4 | b |
| ansiblevirtual | b | 46.5 | 14 | d |
| importer | b | 46.0 | 17 | b |
| registry | b | 44.7 | 17 | b |
| ocivirtual | b | 44.5 | 16 | d |
| oci | b | 39.2 | 13 | b |
| audit | b | 38.6 | 22 | b |
| debkeys | b | 38.0 | 14 | b |
| smoke | b | 35.0 | 11 | b |
| signatures | b | 29.3 | 7 | d |
| uiassets | b | 28.7 | 2 | b |
| endpoints | b | 28.0 | 5 | b |
| overload | b | 26.6 | 3 | b |
| itempublish | b | 24.0 | 9 | b |
| roles | b | 21.8 | 6 | b |
| mirrorapi | b | 21.0 | 8 | b |
| restoreversions | b | 17.7 | 4 | c |
| upstream | b | 15.1 | 6 | b |
| tls | b | 13.2 | 4 | b |
| search | b | 12.5 | 5 | b |
| signing | b | 11.8 | 2 | d |
| apikey | b | 9.5 | 3 | b |
| virtual | b | 5.7 | 6 | d |
| storageverify | b | 4.4 | 2 | c |
| testenv | b | 2.2 | 7 | b |
| cmd/seed | b | 0.2 | 0 | b |
| idapi | b | 0.1 | 0 | b |
| stack | b | 0.1 | 0 | b |
| **sum of shard b** | | **2205.7** | **642** | |
| backup | c | 540.6 | 116 | c |
| vulnsync | c | 249.8 | 62 | d |
| ociremote | c | 157.3 | 40 | c |
| debremote | c | 156.3 | 27 | c |
| bastion | c | 154.1 | 28 | c |
| vulnfindings | c | 149.7 | 47 | d |
| api | c | 141.0 | 32 | c |
| valkey | c | 135.0 | 15 | c |
| vulnapi | c | 123.2 | 29 | d |
| vulnimage | c | 82.9 | 20 | d |
| vulnblock | c | 78.9 | 17 | d |
| ansibleremote | c | 61.8 | 24 | c |
| bastioniface | c | 48.2 | 35 | c |
| bastionreg | c | 46.4 | 14 | c |
| vulnrestore | c | 35.4 | 8 | d |
| vulnaudit | c | 25.6 | 10 | d |
| bastioncache | c | 17.6 | 10 | c |
| bastionshared | c | 6.4 | 13 | c |
| vulnflow | c | 4.9 | 1 | d |
| bastionsetup | c | 3.5 | 1 | c |
| **sum of shard c** | | **2218.5** | **549** | |

</details>

Not measured here: the real minutes of the four shards (the pipeline's first run with them shows them); whether the `self-hosted` pool starts a fourth integration job at once beside the other jobs of a push (in this run every job started within 15 s of the run's start); a second sample of the package times (one run, so a package's spread between runs is not known here; the two runs of section 3.2 differ per package by up to about 15 %, more where tests changed between them).

## 9. A case added after the measurement: an image through a Bastion (2026-11-22)

Measured once, locally, not on a runner: the case `TestOCI_R_BAS_051_ThroughABastion` of the set `oci`
([R-BAS-060](../spec/bastion.md#r-bas-060--tests-of-the-feature-and-what-they-cost), [e2e.md](e2e.md#the-bastion-case-of-the-set-oci)), on a kind cluster of the dev
profile on an arm64 workstation (Docker Desktop, 20 CPUs), run alone twice in a row (`QUADER_E2E_RUN`) and once inside a run of the whole set, [V] from the test's own log line and the `go test -json` report.

| Leg | What it adds | Seconds the leg gains [V] | Data |
|---|---|---:|---|
| `e2e (oci)` | one Bastion release (one replica, the volume backend on an emptyDir, installed beside the build and push of the image), `docker login`, a cold and a warm `docker pull`, a `podman pull`, a Kubernetes pod's pull | 16.7 and 16.4 alone (the release ready after 5.6 and 5.5); 16.7 inside a run of the whole set | an image of two files of 3 and 5 KiB |
| every other leg | nothing: no further client image, no further Bastion, no profile switch (the set's own switch installs the profile `oci`) | 0 | none |

The planner's estimate was about 100 s, 60 of them the release [J in R-BAS-060]. Not measured: the seconds on a runner of the `large` class, where the release and
the pulls may take longer than on the workstation; the first run of the leg in CI shows them.

## 10. Cases removed after the measurement: the formats' repeated integration tests, first step (2026-11-23)

The first step of moving what the formats' integration areas repeat into the format contract's kit removes the integration tests whose
behaviour an existing kit check, a shared area or a unit test already shows once, and adds no kit check. Each test was read against its
cover before it went; a test that asserted something more stayed. Measured once, locally on the developer workstation (20 CPUs, load 4 to
43 while other work ran), [V] from `go test -json`: the removed tests on the commit before the change, each area whole after it.

| Area (shard) | Removed or shortened | Seconds the removed part took [V] | The area after [V] |
|---|---|---:|---:|
| `npm` (a) | the raw delete through `/files/` (check 9, `TestR_NPM_017_RawDeleteOfATarballUnpublishesTheVersion`), the re-decision at commit (`internal/formathost`, `transfer`), the raw deploy (the unit test below) | 6.3 | 399.6 |
| `nuget` (a) | the raw delete and the raw deploy, three engine tests of R-NUGET-015 (`proxy`, NuGet's unit tests of its validator), the planner's two (`virtual`), the auditor (`packages`); R-NUGET-031 keeps its leg with Valkey stopped, where up to three fills commit NuGet's rows at once | 37.3 | 199.2 |
| `mirror` (a) | the byte bound of Debian and of OCI (`TestR_REPO_052_*` of the same area) | 5.9 | 247.1 |
| `registry` (b) | the per-image decision of a scoped token, the tag hidden in the package API (check 13, `path_grants_test.go`) | 5.0 | 57.2 |
| `files` (b) | the path of an OCI session start (`TestR_AUTHZ_005_EveryDecisionOfAnUploadRouteNamesAPathOfThePackage`) | 1.3 | 106.0 |
| `querybudget` (b) | the two `.nupkg` routes of a virtual repository in the statement count, NuGet's two cases of the concurrent cold requests (npm's virtual routes hold the walk) | 24.9 | 194.1 |
| `ociremote` (c) | the negative cache of a tag (`proxy/TestNegativeCache`) | 2.1 | 173.1 |
| `debremote` (c) | `TestR_DEB_019_ASuiteTheUpstreamDoesNotHaveIsRecordedAbsent` waits for a not-found period of 2 s instead of 60 s (64.3 s before, 3.8 after) | 60.4 | 77.4 |
| `ansiblevirtual` (d) | the claim and the planner (`virtual`, Ansible's unit tests), the version hidden by a path grant (Ansible's unit test, `internal/authz`); the environment no longer makes the two callers they used | 8.1 | 25.5 |

Per shard: a 49.5 s, b 31.2 s, c 62.6 s, d 8.1 s, 151 s in all. Added: one unit test, `TestR_STOR_007_NoFormatWithAProtocolOfItsOwnTakesARawDeploy`
(`internal/formats/all`, milliseconds, no data), which holds the refusal of a raw deploy for every registered format. Not measured: the same on a
runner; the seconds every `ansiblevirtual` test saves by the smaller environment.

## 11. An integration test's own installation, before and after the harness's three changes (2026-11-23)

Almost every integration test builds an installation of its own (`testenv.NewStack`). The plan of the installation cost measured on the
same morning, with a probe in an area's package, where its time went [V, that plan's runs on the developer workstation, one replica, no
built UI]: about **1.83 s** an installation, of which the response validator's parse of the API document 0.60 s, the migration's steps
into a new database 0.57 to 0.69 s, the replica's start 0.27 s, its stop 0.22 s (0.199 s of it the harness's drain delay of 200 ms) and
seed, first request and cleanup about 0.1 s. Its split for a runner, computed from the per-test minima of two pipeline runs [I]: about
**2.5 s**, of which the validator about 1.25 s, the migration about 0.55 s and start, stop, seed and a small body 0.55 to 0.7 s. A run
from the main checkout paid 1.73 to 1.78 s more per installation for publishing the locally built UI (375 files) [V].

The three changes take those pieces away without touching a test: the validator's document is parsed once per test binary
([Q-ARCH-580](../spec/architecture.md#q-arch-580--a-test-process-parses-the-api-document-for-its-response-validators-once)), every stack is
a clone of a migrated template with identity rows of its own
([Q-ARCH-581](../spec/architecture.md#q-arch-581--every-stack-of-the-in-process-harness-is-a-clone-of-a-migrated-template-with-identity-rows-of-its-own)),
and a replica of the harness drains for no time and serves the committed placeholder UI
([Q-ARCH-582](../spec/architecture.md#q-arch-582--a-replica-of-the-in-process-harness-drains-for-no-time-and-serves-the-placeholder-ui-unless-its-test-says-otherwise)).

Measured once each, locally, from a worktree (so without a built UI either time), with `test/integration/run.sh -json`, one area per
call, on the developer workstation (20 CPUs) while three other builders ran their own tests: before the first change on instance 2
(06:19 to 06:31, load 4 to 55 over one minute), after the third on instance 1 (06:35 to 06:41, load 10 to 24; the template existed, so no
run paid its one migration). [V] from the `go test -json` streams; "least" is the fastest top-level test of the area, and every top-level
test of these four areas builds an installation.

| Area (shard) | Area before [V] s | Area after [V] s | Least test before [V] s | Least test after [V] s | Median test before / after [V] s |
|---|---:|---:|---:|---:|---:|
| `ansiblelocal` (a) | 56.6 | 20.1 | 1.80 | 0.55 | 2.44 / 0.69 |
| `endpoints` (b) | 29.5 | 16.0 | 2.05 | 0.66 | 2.13 / 1.29 |
| `api` (c) | 210.7 | 101.7 | 1.91 | 0.44 | 3.31 / 1.46 |
| `ci` (d) | 399.2 | 175.3 | 1.89 | 0.43 | 2.74 / 0.90 |
| **sum** | **696.0** | **313.1** | | | |

The same test before and after: `TestR_ANS_012_TheReindexOfAnotherFormatIsNotApplicable` 1.80 → 0.55 s, `TestR_EP_019_TheCacheFillPolicyOfAnEndpoint`
2.05 → 0.66 s, `TestR_API_015_ANarrowedTokenCancelsItsOwnersJobsOnlyWithSettingsWrite` 1.91 → 0.61 s, `TestR_CI_009_ADisabledMappingIsNotEvaluated`
1.89 → 0.51 s: 1.25 to 1.39 s less per installation, as the plan's local split predicts (0.60 + 0.57 to 0.69 + 0.2). The median test of each
area saved 1.41 to 1.70 s [I]. The before run of `api` ended at a load of 55 and one of its tests (`TestKeyRingStatus`, which waits up to 15 s for
the replicas' rings to agree) took 7.1 s after against 3.1 before, so the area figures carry the noise of a shared machine; the per-test
minima carry less of it.

**What the change itself adds** (CLAUDE.md, "Say what it costs"): unit tests of milliseconds (`internal/api/v1/apitest`, the template's name and the
refusal to drop a foreign database in `test/integration/testenv`, no data); in the area `testenv` (shard b) `TestStacksAreClonesOfTheTemplate`
(four stacks, 0.4 to 1.2 s), `TestTemplateFollowsTheMigrations` (two template migrations, one of a step added during development by two
sessions at once and the embedded one again for the next stack, and a failing one-step migration; 2.4 s at a load of 25) and
`TestReplicaOfTheHarness` (three stacks, four replicas, 2.8 to 4.3 s); data: none beyond the template of about 17 MB (17.4 million bytes) that each instance's PostgreSQL keeps between runs. The template's one
migration per day and change of a migration file is paid by the first stack of a run that finds none (about 1 s locally).

**Computed for the pipeline [I]**: with the plan's counts of tests that build an installation per shard (a 257, b 171, c 216, d 354, a
name-matching estimate) and its runner split of 1.95 s less per installation (validator 1.25, migration 0.5, drain 0.2, one stop per test), the
shard jobs of Test and Release 45109360386 (a 34.8, b 30.2, c 33.0, d 34.9 minutes) would take about a 26.4, b 24.6, c 26.0, d 23.4 minutes;
with the 1.3 s measured here per installation as the floor, about 29.2, 26.5, 28.3 and 27.2.

**The areas run on the final tree** (the three changes and the replica's document parsed once per process,
[Q-ARCH-583](../spec/architecture.md#q-arch-583--a-process-parses-the-management-api-document-once-whatever-number-of-apps-it-runs)), once each as
the change's gates, 06:46 to 07:19 at a load of 5 to 56 [V]; every one passed, no test changed. For orientation only, the same area's seconds
in the pipeline run 45109360386 before the change (a runner, coverage on, so not comparable one to one):

| Area (shard) | Local, after [V] s | Pipeline, before [V] s | Area (shard) | Local, after [V] s | Pipeline, before [V] s |
|---|---:|---:|---|---:|---:|
| `testenv` (b) | 30.8 | 2.4 | `bastion` (c) | 119.3 | 184.8 |
| `ansiblelocal` (a) | 37.8 | 65.8 | `postgres` (c) | 111.0 | 97.6 |
| `endpoints` (b) | 15.6 | 28.2 | `uiassets` (b) | 26.2 | 25.5 |
| `api` (c) | 71.1 | 162.8 | `authz` (b) | 68.8 | 78.3 |
| `ci` (d) | 259.3 | 411.5 | `npm` (a) | 209.8 | 475.0 |
| `backup` (c) | 513.1 | 467.3 | `mirror` (a) | 161.3 | 285.2 |
| `nuget` (a) | 84.7 | 271.1 | `malware` (d) | 45.1 | 85.6 |
| `vulnsync` (d) | 133.3 | 278.3 | | | |

`testenv` grew by the harness's own tests: 23.5 s of its 30.8 were `TestTemplateFollowsTheMigrations` at a load near 50, then with three
template migrations; cut to two, the area took 9.0 s and the test 2.4 s at a load of 25; `postgres` keeps databases of its own and does not use the template, and `backup` uses it in its 54 tests on the harness and restores into databases of its own; `uiassets` passes its own UI trees. NuGet's data-port
test `TestR_API_029_TheDataPortReadsTheViewFromPostgreSQL`, which builds an installation without a replica, took 1.36 s locally from a
worktree before the change (the kit move's first builder's run, 2026-11-23) and 0.27 s on the final tree [V].

**The prediction made before the change landed:** the fastest data-port test, which builds an
installation without a replica (`oci`'s data port, NuGet's `TestR_API_029_TheDataPortReadsTheViewFromPostgreSQL`), falls from about 1.9 s to about 0.7 s. If it falls much less,
the runner split of the validator (1.25 s, computed from minima and not separated from the object store's cleanup) was wrong.

**What the first pipeline run after the change showed** (Test and Release 45133371730 of 2026-11-23 beside 45126854432, the run before it;
read from the four shards' `go test -json` records, top-level tests only) [V]: the fastest test of `oci`'s data port fell from 1.89 s
(`TestExistingItemsAreTheLiveFilesOfTheRepository`) to 0.10 s (`TestAManifestWithoutAnnotationsOrSubjectStoresNull`; the first of the two
took 0.11 s), and NuGet's `TestR_API_029_TheDataPortReadsTheViewFromPostgreSQL` from 2.28 s to 0.26 s. It fell further than predicted: what stays of an installation on a runner
(start, stop, seed, a small body) is about 0.1 to 0.3 s, not the 0.55 to 0.7 s of the split above [I]. Of the run's 1,721 tests, 472 took
less than 0.7 s; of the 1,716 of the run before, 96 did. No test failed in either. The shard jobs, with their test step in brackets, in
minutes and seconds [V], beside the minutes computed above [I]:

| Shard | Before | After | Computed before the change |
|---|---|---|---|
| a | 35:04 (31:43) | 22:27 (19:06) | 26.4 |
| b | 31:43 (28:21) | 22:55 (19:51) | 24.6 |
| c | 32:11 (28:52) | 24:48 (20:58) | 26.0 |
| d | 35:08 (31:56) | 20:31 (17:40) | 23.4 |

Each shard now keeps 20 to 24 minutes below its limit of 45; before the change it kept 10 to 13. One run each, on runners whose
neighbours differ between runs.

Not measured: the clone under a runner's load apart from the shards' times above; a run from the main checkout with the
built UI after the change (it pays neither the UI's 1.7 s nor the drain, by construction); any other area before and after.

## 12. The million of R-REPO-045, measured once (2026-11-23)

R-REPO-045's first criterion, an invalidation with an empty path that returns within one second over 1,000,000 cached metadata
entries, is a measurement, not a test
([Q-REPO-481](../spec/repositories-and-proxy.md#q-repo-481--the-million-entries-of-r-repo-045s-criterion-are-a-measurement-the-test-holds-the-invalidations-shape-with-2000)).
Run once with `QUADER_TEST_INVALIDATION_ENTRIES=1000000 test/integration/run.sh -run TestInvalidationAtScale -v ./test/integration/repository/`
on the developer workstation (20 CPUs, instance 2 of the integration stack, 15:18 to 15:20, host load 12 to 18, the cap of 6 cores) [V]:

| Step | Time |
|---|---:|
| Seed 1,000,000 entries (one `INSERT … SELECT generate_series`) and `VACUUM (ANALYZE)` | 15.8 s |
| `Invalidate` with an empty path: one row, one count | **46 ms** (the bound is 1 s) |
| The fold with the product's batch of 5,000 (200 batches), every entry expired, the row folded | 70 s |
| The test in all | 86.4 s |

The same test in the pipeline before the change: 116.8 s (section 3.2, shard b). On 2026-11-23, at a host load of 30 to 110, the
seeding took 74 s and the fold met the statement timeout after 207 s. As a test it now holds the shape with 2,000 entries and a fold
batch of 250: 0.5 to 0.9 s [V]. Not measured: the call on PostgreSQL of the reference environment, and the fold beside a cache that
serves requests meanwhile.

## 13. What the repairs of the tests that depended on the host cost (2026-11-23)

The tests a loaded host broke on 2026-11-23 were repaired
([Q-ARCH-702](../spec/architecture.md#q-arch-702--a-test-waits-for-the-state-it-stands-for-and-does-not-depend-on-the-hosts-speed);
the list is in [build-and-test.md](build-and-test.md#tests-that-hold-on-a-loaded-host)). The same selection of tests, run once before
(the base revision 829bd5d0e, 15:20 to 15:25) and once after (15:25 to 15:28), one after the other on instance 2 of the integration
stack, the cap of 6 cores, host load 11 to 16 [V], seconds of the `go test -json` events. No data changed; no test was added.

| Area (shard) | Test | Before | After |
|---|---|---:|---:|
| `identity` (b) | `TestR_AUTH_038_E1_8_ThrottleAcrossThreeReplicas` / `…RevocationByValueCountsInTheFlowStartBudget` | 2.5 / 2.5 | 1.8 / 0.5 |
| `bastion` (c) | `ColdAndWarmCache` (four Bastions) | 17.8 | 5.2 |
| `bastion` (c) | `EveryCallerIsAnsweredAsTheMainInstallationAnswers` / `NegativeCacheOnlyForFullReaders` | 14.0 / 2.8 | 8.9 / 1.4 |
| `blob` (c) | `TestGCKilledAtRandomPoints` / `TestGarbageCollection` | 19.7 / 13.4 | 15.3 / 6.3 |
| `files` (b) | `TestR_HA_011_UploadsDuringARolloutRestart` / `TestR_STOR_017_ADeployHoldsEveryAncestorOfItsPath` | 9.4 / 2.0 | 8.0 / 1.3 |
| `nuget` (a) | `TestR_NUGET_004_AWorkerRecomputesTheSemVer2Flag…` | 11.8 | 10.8 |
| `repository` (b) | `TestInvalidationNegativeRetention` | 1.3 | 0.7 |
| `repository` (b) | `TestInvalidationAtScale` (not in the run: the million, section 12) | 86.4 (116.8 in the pipeline) | 0.5 to 0.9 |
| `querybudget` (b) | the whole area | 81.0 | 29.1 |
| `ci` (d) | `TestR_CI_003_ARotationCausesOneFetchAndRandomKidsCauseNone` / `…AFetchHolds…` / `…StaleAndUnreachable` | 9.2 / 8.2 / 12.0 | 2.8 / 7.8 / 12.0 |
| `coordination` (a) | `TestJobs` | 15.4 | 12.1 |
| **sum of the run** | nine areas, without the million | **235.1** | **135.3** |

Where the time went: the querybudget windows waited 1.3 s each and the Debian case for the first orphan sweep (Q-ARCH-700); the
key reads waited for four flushes of the table's statistics (10 s); `ColdAndWarmCache` slept 3.2 s in each of four Bastions; the
component areas' databases were migrated (0.6 s each), now cloned. What each shard gains, computed from the table [I]: shard b about
52 s (`querybudget`) and 85 to 115 s (`TestInvalidationAtScale`), shard c about 19 s, shards a and d about 3 to 7 s each. Bounds that
are longer now than before and cost nothing on a host that keeps up: the Bastion's link bounds, the commit grace of one case, the waits
of `TestR_MAL_014` and `TestR_NUGET_004`; they are reached only on a host that would have failed before.

## 14. The three measurements section 7 left open, from the runs of 2026-11-20 to 2026-11-23 (read on 2026-11-24)

Read from the Actions API and the artifacts of runs that had already run (the jobs of every Test and Release run on `main` from
2026-11-20 12:00 to 2026-11-23 23:53 UTC, 71 runs; the nightlies 44828435420 and 45008950010; the report `test-unit` of run
45283317395); nothing was started or re-run for them. What they decided is [Q-ARCH-1060](../spec/architecture.md#q-arch-1060--a-push-that-touches-one-format-runs-that-formats-leg-and-the-tiers-below-it-whole).

**A push that touches one format.** None of the 71 pushes did: each started npm's leg (shared code) or no format's leg [V: the
`e2e (<format>)` jobs that ran or were skipped]. Of the 682 commits on `main` since 2026-11-20, classified one by one with
`.github/scripts/e2e-legs.sh --classify` (every changed path; `release.yml` and `versions.env` counted as shared), 464 start no
format's leg (documents, tests, `web/`), 209 are shared code and 9 touch one format alone (Ansible 4, Debian 3, npm 2) [V]. So the
cost is computed from parts [I]: a push that starts no format's leg costs 273 runner minutes (45277825413: the 25 other jobs 125.2,
the four integration shards 92.1, `push` 18.7, `access-long` 25.7, `ci` 10.5, the clients images' lookups 1.2) [V]; a push of one
format adds that format's leg and its clients images' row: Debian 10.8, Ansible 13.1, OCI 19.9, NuGet 19.6, npm 24.2 (45179000559)
plus about half a minute of lookup, so 284 to 298 runner minutes; a push of shared code with npm's leg costs 300 to 330
(45212749706: 302) and one that starts every leg 444 to 453 (45179000559, 45187733127, with clients images built) [V]. A second
format adds nothing to the first's push.

**The unit tests per format** (run 45283317395, `make test` 509 s of wall [V]): 296 packages report 2,308.8 s of package time, so the
run keeps about 4.5 packages busy at once [I]. The packages of the five formats (by the paths of `e2elegs`' lists: the format's
packages, its data port, its stub and its signature scheme) are 478.9 s of it, 20.7 % [V]: npm 152.9 s in 15 packages, NuGet 101.3 in
17, Ansible 78.2 in 13, OCI 76.4 in 11, Debian 70.3 in 14; the format contract's kit (`formattest` 164.2, `testformat` 118.9) runs
once. At the run's parallelism a format is about 21 s of the job's wall (mean 95.8 s / 4.5) [I]; with 25 formats the job would be
about 7 minutes longer than its 8.9 to 15.3 minutes over the 83 runs read (mean 11.6), inside its limit of 30. The slowest package is
no longer `internal/api/v1` (110.8 s in this run, 476 to 585 s on 2026-11-21).

**A second and a third nightly** (each a run of all legs) [V]:

| Nightly | Runner minutes | large / GitHub-hosted / self-hosted | e2e legs | The formats' own sets (Q-ARCH-545) | Wall | Red legs |
|---|---:|---|---|---:|---:|---|
| 44625277245 (2026-11-19, section 2) | 682.9 | 445 / 233 / 4 | 25 = 658 | none yet | 299 min | backup, npm-ha-soak, drain-load-pooler |
| 44828435420 (2026-11-20) | 747 | 496 / 248 / 2 | 30 = 724 | 79.8 (npm 22.7, NuGet 20.9, OCI 12.3, Debian 11.7, Ansible 12.2) | 272 min | drain-roles, ha-faults, ha-pooler, ansible, drain-load-e, the four platform rows |
| 45008950010 (2026-11-22) | 764 | 510 / 250 / 2 | 30 = 739 | 89.3 (npm 23.4, NuGet 22.7, OCI 18.5, Debian 11.6, Ansible 13.1) | 284 min | vuln-pooler, ha-faults, backup, nuget-ha |

Without the formats' sets the night is 667 and 675 runner minutes, within 2 % of the first; the sets added since are the whole
difference. A format's own set costs 11.6 to 23.4 minutes a night (mean 17.9), its whole nightly cost when it has no load leg of its
own. With 25 formats the night would be about 764 + 19 × 17.9 ≈ 1,104 runner minutes [I], and lane 3, which takes the formats' sets
of the `large` class one after the other (Q-ARCH-545), would grow by about 19 × 15 ≈ 285 minutes and become the longest lane, about
6.8 hours of wall instead of lane 1's 4 [I]. That is the next limit of the nightly; it is a revisit condition of Q-ARCH-545, not built.

**Who ran beside the nightly's legs** (45008950010, a scheduled run that GitHub started at 09:43 UTC on a Sunday of landings): 21 of
its 23 `large` legs had kind legs of Test and Release beside them, among them the red ones: `ha-faults` 6 kind legs (78 runner minutes
inside its 28), `backup` 4 (89 inside 31), `nuget-ha` 5 (45 inside 28) [V: the job times of both workflows; computed as the record of
[Q-ARCH-1062](../spec/architecture.md#q-arch-1062--a-nightly-leg-records-the-jobs-of-test-and-release-that-ran-beside-it-and-nothing-holds-a-push)
computes it]. Each leg now writes this itself.
