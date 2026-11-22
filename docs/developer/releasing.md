# Releasing

Every push to `main` and every pull request that changes more than documentation runs the checks ([CI](#ci)). Once the jobs that gate it passed ([Q-ARCH-457](../spec/architecture.md#q-arch-457--until-the-first-stable-release-a-minimal-set-of-jobs-gates-semantic-release)),
Semantic Release publishes a GitHub release ([release flow](#release-flow)),
and [`build.yml`](../../.github/workflows/build.yml) builds the product from that tag: the image
`guidedtraffic/quader` on Docker Hub, and the Helm charts `quader` and `quader-bastion` in the chart repository on GitHub Pages
([the release pipeline](#the-release-pipeline)). What the release contains and how the image is
built: [`Containerfile`](../../Containerfile).

## Until the first stable release

Quader is pre-alpha and a release must not wait on a slow or intermittent leg. By the owner's decisions of 2026-11-10 and 2026-11-20, until the
first stable release:

| Rule | What it means | Decision |
|---|---|---|
| A minimal set of jobs gates Semantic Release | The static checks, unit, web, chart, image, the scans, `workflows`, `coverage` and `release-tooling`; the list is the `needs` of the job `semantic-release` in [release.yml](../../.github/workflows/release.yml) | [Q-ARCH-457](../spec/architecture.md#q-arch-457--until-the-first-stable-release-a-minimal-set-of-jobs-gates-semantic-release) |
| `integration`, `s3-conformance`, every `e2e` leg and the UI layout checks gate nothing | They still run (a format's e2e leg when the push changed its paths, below) and do not hold a release ([Q-ARCH-510](../spec/architecture.md#q-arch-510--the-layout-checks-of-the-ui-run-on-every-push-in-test-and-release-and-gate-no-release-until-the-first-stable-release) for `ui-layout`, which joins the gate with the first stable release). A red one is still worked on: its cause is found and fixed, also after the release is out (owner, 2026-11-10). The tests that matter for a change run locally | Q-ARCH-457 |
| A release can carry a defect those legs found | The price of the rule; it is accepted while Quader has no users | Q-ARCH-457 |
| The coverage badge and the evidence record stand still | Their jobs need the legs that no longer gate, so a release commits neither | Q-ARCH-457 |
| Nothing runs against published artifacts | Tests belong before a release, where a failure can still change the outcome | [Q-HA-281](../spec/ha-and-helm.md#q-ha-281--tests-run-before-a-release-not-after-it) |
| The tools are limited, not the pods | Both workflows set `GOMAXPROCS`, `NG_BUILD_MAX_WORKERS` and `VITEST_MAX_WORKERS` to 8. The pods keep their requests and have no CPU limit, so without this every tool would start 32 threads on a shared node | owner, 2026-11-10 |
| Names of jobs and steps say what they do | No requirement codes, no tool versions, no "on kind"; an e2e job is `e2e (<set>)` | owner, 2026-11-10 |
| An e2e set is cut by what it tests | What a format does belongs to that format's set, not to a set named after a feature | owner, 2026-11-10 |
| A format's e2e leg runs on a push only when that format's own code changed | Shared code runs the unit and integration tiers and the reference leg npm; every format's set runs in the nightly, where what only another format's client shows of a change of shared code is found ([which e2e legs a change runs](#which-e2e-legs-a-change-runs)) | [Q-ARCH-540](../spec/architecture.md#q-arch-540--a-formats-e2e-leg-runs-on-a-push-only-when-that-formats-own-code-changed) |

With the first stable release the full gate returns and reliability comes first.

## CI

[`release.yml`](../../.github/workflows/release.yml) ("Test and Release") runs on every push to `main`,
every pull request to `main` and on demand. A push or pull request that changes documentation alone starts no run
(owner, 2026-11-17): everything under `docs/` except `docs/manual/`, which is embedded in the binary, and the Markdown
files at the root. The guides under `internal/formats/*/guide/` are embedded as well and start a run like any code. What
this leaves out: the jobs that read the documentation (`coverage`, the links and rules of the coverage generator) see such a
change only with the next run, and a red one then belongs to the earlier documentation commit. A newer push cancels the
running run of its ref. Every
check is a job of its own, with its own make target, so it runs the same locally. A job runs on one of the
organisation's two self-hosted runner classes or, where it needs more disk than they have, on a GitHub-hosted runner
(owner, 2026-11-05; the table below). Both self-hosted classes start one ephemeral pod per job and discard it
afterwards: the runner (actions-runner 2.338.2, Ubuntu 24.04) and a privileged Docker-in-Docker sidecar (docker 29.9.2-dind)
share the pod's budget, without `make` or a C compiler.

| Class | Label | Pods | CPU request / limit | RAM request / limit (runner and dind together) | Disk: `_work` / Docker | Jobs |
|---|---|---|---|---|---|---|
| wds18-small | `runs-on: self-hosted` | 0–24 | 1 / none | 4 GiB / 10 GiB | no limit | every job of Test and Release except the kind legs, Release Docker & Helm, Renovate |
| wds18-large | `runs-on: large` | 0–6 | 10 / none | 8 GiB / 28 GiB (the kind nodes included) | no limit | the kind legs `e2e` (push, access-long), `e2e-npm`, `e2e-oci`, `e2e-deb` and `e2e-ansible` of Test and Release; `e2e` (failover, drain-roles), `e2e-npm` (npm-ha, ha-suite), `npm-ha-soak`, `e2e-drain-pooler` (drain-load-pooler), `e2e-oci` (oci-long), `format-oci`, `format-deb`, `format-ansible` and `format-npm` (the formats' sets), `e2e-nuget` (nuget-ha, drain-load-b, -c, -e, [Q-HA-102](../spec/ha-and-helm.md#q-ha-102--the-cpu-of-the-nuget-drain-and-where-its-leg-runs)) and, after all of them, `latency` and `vuln-perf` ([Q-ARCH-325](../spec/architecture.md#q-arch-325--where-do-the-perf-jobs-run-relative-to-the-kind-legs)) of HA and Performance. The nightly bounds its own large legs to three in three lanes ([Q-ARCH-431](../spec/architecture.md#q-arch-431--how-many-large-legs-of-the-nightly-run-start-at-once-and-in-what-order)); Test and Release starts up to six large legs on a push that changes every format, unbounded, and is not bounded yet ([Q-ARCH-432](../spec/architecture.md#q-arch-432--does-test-and-release-bound-its-large-legs-the-same-way)); a push of shared code alone starts three (push, access-long, npm) |
| GitHub-hosted | `runs-on: ubuntu-24.04` | GitHub's | 4 vCPU | 16 GB | 108 GB free on `/` after the job deletes the preinstalled SDKs | the kind legs that load the NuGet clients image: `e2e-nuget` of Test and Release and `format-nuget` of HA and Performance, `e2e-ci`, `e2e-bastion`, `e2e-vuln`, in both workflows; `e2e-upgrade` (HA and Performance only, the sets `upgrade` and `upgrade-release`, which load both clients images) |

The measured basis of the nightly assignment ([Q-ARCH-326](../spec/architecture.md#q-arch-326--which-runner-class-runs-which-leg-the-runner-review-of-the-nightly-run)):
the six nightly runs of 2026-11-03 to 2026-11-05 (`gh run view <id> --json jobs`, the `runner_name` and the labels of each job), per leg the class that ran it and the
result. `self` is `self-hosted` (the CPU-limited pods of that time), `hosted` is `ubuntu-24.04`, `large` is `large`; `RED` is a failed job, `–` a leg that
the run did not have. The code changed between the runs (F-43; the drain per role was added in 41902592855), so the table shows what happened, not a cause.

| Leg | 41739280012 (11-03, dispatch) | 41748522448 (11-04 00:40, dispatch) | 41804996351 (11-04 09:08, schedule) | 41902592855 (11-04 20:01, dispatch) | 42002617340 (11-05 09:28, schedule) | 42126196109 (11-05 23:00, dispatch) |
|---|---|---|---|---|---|---|
| `e2e` failover | self ok | self ok | self RED | hosted ok | hosted ok | large ok |
| `e2e` drain-roles | – | – | – | hosted ok | hosted ok | large ok |
| `e2e-npm` npm-ha | self RED | self RED | self RED | hosted RED | hosted RED | large ok |
| `npm-ha-soak` | self ok | self RED | self ok | hosted ok | hosted ok | large ok |
| `e2e-nuget` nuget-ha | self ok | self ok | self ok | hosted ok | hosted ok | hosted ok |
| `e2e-ci` ci-ha | self ok | self ok | self ok | hosted ok | hosted ok | hosted ok |
| `e2e-bastion` bastion | self RED | self RED | self ok | hosted ok | hosted RED (lost, no test run) | hosted ok |
| `e2e-bastion` bastion-long | self ok | self RED | self ok | hosted ok | hosted ok | hosted ok |
| `e2e-vuln` vuln | self ok | self ok | self ok | hosted ok | hosted RED (one `EOF` of the npm test upstream in setup) | hosted ok |
| `e2e-vuln` vuln-long | self RED | self RED | self ok | hosted ok | hosted ok | hosted ok |
| `latency` (P-13, P-14) | self ok | self ok | self ok | hosted ok | hosted ok | large ok |
| `vuln-perf` (R-VULN-028) | self RED | self RED | self RED | hosted RED | hosted RED | large RED |

Per class over the six runs: `self` 20 of 33 jobs green, `hosted` 25 of 31, `large` 6 of 7 (its one red job is `vuln-perf`). Reds that no test decided:
the `bastion` of 42002617340 was lost after eleven minutes inside `make e2e SET=bastion`, with no step after it and no log (a runner loss; F-40 in the project plan),
and the `vuln` of 42002617340 failed its first test in the environment's setup, on one `EOF` of `POST /-/stub/publish` to the in-cluster npm test upstream;
the later tests of the set then reported that the environment did not finish. `npm-ha` was red in five of five runs off `large` and green once on `large` (42126196109).

What the assignment rests on, measured on 2026-11-05 on a developer machine (linux/arm64 containers with 6 CPUs, a cold
start, the cgroup's `memory.peak`, page cache included): `lint` 4.7 GiB, `unit` 4.6 GiB, `generate` 3.9 GiB, `web` 3.2 GiB,
`chart` 2.3 GiB, `govulncheck` 2.3 GiB, `build-no-node` 2.1 GiB, `malware-scan` 1.3 GiB; the set `drain-roles` under the npm load
held 9.9 GiB in Docker (kind nodes 7.8 GiB, k6 2.2 GiB) and about 13 GiB of images in the kind nodes. The legs that load
the NuGet clients image (2.3 GB, which `kind load docker-image` copies into each of the four nodes) were evicted on large in
run 42046280787 (`Usage of EmptyDir volume "docker-storage" exceeds the limit "30Gi"`), so they run on GitHub-hosted
runners, where they passed (runs 41894949089, 41902592855); there every such job first deletes the runner's preinstalled
SDKs, without which run 41871333853 ran out of disk. The RAM is
overbooked: the scheduler places a pod by its request, and a job above it (the kind legs, `lint`, `unit`) can be evicted by
the kubelet when several jobs on one node peak at once; such a red job is the node's capacity, not a test. The self-hosted classes run on three nodes of 32 cores, 64 GiB of RAM and about 450 GiB of disk. Neither class has a disk limit since 2026-11-09: the former 30 GiB of `docker-storage` evicted the kind legs of `large` (`The self-hosted runner lost communication with the server`, no log and no artifact; read `kubectl get events -n arc-runners` within the hour), and the kubelet's eviction at 10% free disk on the node is the guard that remains. Since 2026-11-18 the Docker daemon of a `large` runner starts with the pod's cgroup as its parent (`kubepods/burstable/pod<uid>/docker`): the kind nodes, k6 and the image builds of a leg share the pod's CPU request and count against its memory limit, so a leg above 28 GiB is OOM-killed inside its pod (F-133 in [promt.md](../projektplan/promt.md) has the measurement and what is not verified yet). Before that, and still on `self-hosted`, a job's containers run in the host's cgroup `/docker/<id>`, outside the runner pod and its limits. The integration
shards, `compat`, the image builds and the performance jobs were not measured. A job therefore installs what it calls: `make` and the C compiler (`build-essential`), `file`
and `jq` where a step uses them, through [build-tools](../../.github/actions/build-tools/action.yml), which installs only what the runner lacks, so a GitHub-hosted runner, whose image has them, does not depend on the Ubuntu archive (on 2026-11-11 the archive did not resolve there for over an hour; the self-hosted image still installs them, F-117), the Docker Compose plugin for the integration stack, Go from `go.mod` and the modules of `go.sum` (from the cache, [go-modules](../../.github/actions/go-modules/action.yml)),
Node.js from `web/.nvmrc`, and the pinned helm, kubectl, kind and Docker Buildx; the chart job also fetches promtool
at its pin (`make promtool`, below). Every OS package among them is installed by [apt-install.sh](../../.github/scripts/apt-install.sh),
which bounds the install in time ([OS packages in the jobs](#os-packages-in-the-jobs)).

| Job | Make target | Runs |
|---|---|---|
| versions | `make versions-check` | go, node (`web/.nvmrc`), sqlc, oapi-codegen, golangci-lint, oasdiff (when built), promtool (when fetched), kind, kubectl, helm, k6 and docker buildx against their pins. The job installs the pinned helm, kubectl and kind first, so the runner's copies are not the ones checked; buildx comes from [install-buildx](../../.github/actions/install-buildx/action.yml), whose probe keeps the runner's file only when its SHA-256 is the pin, so the buildx checked is the one the CLI resolves |
| Code Linting | `make lint` | golangci-lint (pinned; its binary comes from [go-tool](../../.github/actions/go-tool/action.yml), cached by version and Go version) with the linters of [.golangci.yml](../../.golangci.yml), then `go vet` |
| GoSec Security Scan | `make gosec` | golangci-lint with only gosec enabled, the settings and the accepted findings of [.golangci.yml](../../.golangci.yml) |
| Vulnerability Check | `make vuln` | govulncheck (pinned) over the module |
| Cyclomatic Complexity | `make cyclo` | gocyclo (pinned), fails above `CYCLO_THRESHOLD`; the job summary lists the 20 most complex functions with `make -s cyclo-report` |
| generate | `make generate-check`, then `make nuget-reference-check` | `go generate`, then `git diff --exit-code` and a check for generated files that are not committed; then the NuGet reference solution of [perf/nuget](../../perf/README.md), regenerated from the counting upstream's package set and compared with the committed files (`go run ./test/nugetstub/cmd/nugetstub reference -check`, [Q-ARCH-279](../spec/architecture.md#q-arch-279--how-are-the-nuget-sdk-pins-and-the-reference-solution-kept-in-step)) |
| nuget vectors | `make nuget-vectors-check` | regenerates the golden vectors of the NuGet version, range and ID rules with NuGet.Versioning in the pinned .NET SDK image (`NUGETVECTORS_SDK_IMAGE`, `NUGET_VERSIONING_VERSION`) and compares them with the committed file; needs Docker |
| build without Node.js | `make build` | with every Node.js executable off `PATH`; `file` must say *statically linked*, `go version -m` must record `CGO_ENABLED=0` |
| unit tests | `make test` | the race detector; uploads `.out/test-unit.json` (`test-unit`) and `.out/coverage/unit.out` (`profile-unit`) |
| web (Angular) | `make web-ci`, `make web-lint`, `make web-test`, `make web-embed` | `npm ci` (fails when `package.json` and the lockfile disagree), ESLint and the R-UI-046 rules, the Angular unit tests, the production build embedded into `internal/webui/dist`, then `QUADER_UI_BUILT=1 go test -run TestR_UI_001_ThirdPartyNotices ./internal/webui/` on that build (the notices of the bundled software, Q-UI-144) |
| UI layout checks (`ui-layout`) | `make web-ci`, `make ui-layout-test`, `make ui-renders`, `make ui-dev-down PURGE=1` | the layout checks of the UI ([web-ui.md](web-ui.md#renders-of-every-screen), R-UI-170 to R-UI-176): first on their fixture pages, then every screen of the render set as every kind of user, light and dark, at 1,440, 1,024 and 390 px, against the job's own UI development profile (three service containers in the runner's Docker, Quader with the sign-in provider, the Angular dev server); a layout finding, an error notice, a failed call of the app's API or a console error fails it. One leg on `self-hosted`, 75 minutes ([Q-ARCH-511](../spec/architecture.md#q-arch-511--the-layout-checks-run-in-one-leg-on-the-standard-runners); a full run took 22 minutes on the development machine, not measured on a runner yet). Every push and pull request, gates nothing until the first stable release (Q-ARCH-510); on failure uploads `ui-layout-findings` (`failures.txt`, `report.json`, the pictures of the failed screens), on success `ui-layout-report` (`report.json`), 7 days ([Q-ARCH-512](../spec/architecture.md#q-arch-512--a-red-run-of-the-layout-checks-keeps-its-findings-and-the-pictures-of-its-failed-screens-for-a-week)) |
| Malware Scan (source code) | `make malware-scan-db`, `make malware-scan` | `freshclam` and `clamscan --recursive --infected` of `CLAMAV_IMAGE` over the tree, without `.git`, `node_modules` and the build outputs, the database from a cache of the day; any infected file fails; uploads `clamav-report` |
| chart | `make chart-lint`, `make chart-test` | `helm lint` and `helm template` over every value set of both charts (`deploy/helm/quader/ci`, `deploy/helm/quader-bastion/ci`, the dev profile), the render assertions of [test/chart](../../test/chart) for both charts, and the `promtool check rules` / `promtool test rules` run over the Bastion's PrometheusRule ([rules_test.yaml](../../deploy/helm/quader-bastion/tests/rules_test.yaml)); `make chart-test` fetches promtool into `.bin` at `PROMTOOL_VERSION` of [tools/versions.env](../../tools/versions.env), checked against the SHA-256 of the platform; uploads `.out/test-chart.json` (`test-chart`) |
| image | `make image IMAGE=guidedtraffic/quader:dev VERSION=dev` | the [Containerfile](../../Containerfile) for the runner's architecture, loaded into Docker; the image must run as `65532:65532` (R-HA-033); then `docker save` and the artifact `quader-image` (one day), which every kind leg of the run loads instead of building the image itself ([Q-ARCH-522](../spec/architecture.md#q-arch-522--the-kind-legs-load-the-quader-image-the-job-image-of-their-run-built-and-need-that-job)) |
| SBOM generator (pinned syft, test image) | `make sbom-generator-check` | builds the SBOM generator of the release image and a two-platform test image with it, and asserts that both attestations name the pinned syft; needed by Semantic Release ([below](#sbom-generator)) |
| Container Scan (Trivy, built image) | `make image IMAGE=quader:scan`, `make trivy`, then Trivy | Trivy (the binary `make trivy` takes out of `TRIVY_IMAGE`, the scanner pods' own analyser, one version in the repository, Q-HA-381) on the built image, which carries no analyser: fixed OS and library vulnerabilities of CRITICAL or HIGH, secrets and misconfigurations, less the findings accepted for a time ([below](#accepted-findings-of-the-container-scan)); SARIF uploaded as `trivy-container-report` |
| integration (shard a, b, c, d) | `make integration-up`, `make test-integration SHARD=<a\|b\|c\|d>`, `make integration-down` | the integration tier against the compose stack on TLS, on Valkey 9.1 only (no older line is guaranteed while Quader is in development); four legs with a stack each, by a rule on the area's name that places a new package without anyone editing it ([Q-ARCH-565](../spec/architecture.md#q-arch-565--the-integration-tests-run-in-four-shards-the-fourth-takes-the-virtual-areas-and-the-security-checks), after [Q-ARCH-544](../spec/architecture.md#q-arch-544--the-integration-shards-are-cut-by-a-rule-that-spreads-a-formats-areas-over-all-three)): the suffix decides first, every `<format>local` in shard `a`, every `<format>remote` in shard `c`, every `<format>virtual` in shard `d`; then shard `a` the packages `coordination`, `mirror`, `npm`, `nuget` and `proxy`, shard `c` the packages `api`, `backup*`, `restore*`, `storage*`, `postgres`, `blob`, `valkey` and `bastion*` and the OCI conformance suite, shard `d` the security checks `ci`, `vuln*`, `malware*` and `sign*`, shard `b` every other one (every new platform area among them). From the package times of run 44948103795 about 32, 31, 33 and 31 of the 45 minutes (a, b, c, d; with three shards that run took 38, 43 and 42); a shard that is stopped by its limit or passes 43.5 minutes is cut again by the rule, the limit is not raised. Uploads `test-integration-<shard>` and `profile-integration-<shard>` |
| S3 conformance | `make silo-up`, `make s3-conformance`, `make silo-down` | the conformance tests of R-STOR-035 against Silo (the image `SILO_IMAGE`), served over TLS only; the server log is uploaded on failure |
| which e2e legs the change runs (`e2e-scope`) | [.github/scripts/e2e-legs.sh](../../.github/scripts/e2e-legs.sh) | reads the changed paths of the push once and outputs, per leg (`npm`, `nuget`, `oci`, `deb`, `ansible`, `bastion`, `vuln`), whether it runs ([below](#which-e2e-legs-a-change-runs)); on `self-hosted`, about a quarter of a minute (a checkout of the whole history and the script); the legs it decides need it and run unless it said `false` |
| e2e (npm) | the image of the run, the npm clients image of the cache, `make dev-up`, `make e2e SET=npm`, `make dev-down` | the npm acceptance matrix (eight real clients, [R-NPM-045](../spec/formats/npm.md#r-npm-045--client-acceptance-matrix)), the npm side of the remote full mirror (R-REPO-055, R-REPO-059, Q-REPO-306) and E2.2 to E2.12 on the scatter profile with the counting test upstream; the reference leg: runs when the push or pull request changed npm's own paths or shared code ([below](#which-e2e-legs-a-change-runs)), and on every dispatch; nightly as `format-npm`; own cluster name and edge port (8082). A dispatch with `closing=true` adds the leg `npm-closing` (port 8083): the matrix and the cold install with Valkey stopped, and the read-only subset against the public registry ([Q-NPM-46](../spec/formats/npm.md#q-npm-46--what-marks-the-run-that-closes-the-phase)); the legs `npm-ha` and `npm-ha-crash` left this job on 2026-11-01 (owner): `npm-ha` runs nightly and on dispatch in [HA and Performance (nightly)](#ha-and-performance-nightly) (port 8087), `npm-ha-crash` (three force-deletes) runs by hand only; for them the step budget is 80 minutes and the job 120, and they are E2.11: drain variant A, the PostgreSQL switchover and the Valkey failover under the npm load, and the pod crash with three force-deletes ([e2e.md](e2e.md#drain-and-failover-under-npm-load)); a closing dispatch runs both as one leg `npm-ha-closing` with the 20-minute pod crash, the R-ARCH-027 report and the 5-minute Valkey outage ([Q-HA-67](../spec/ha-and-helm.md#q-ha-67--where-and-how-often-the-scenarios-under-npm-load-run)); uploads `e2e-npm-report-<set>`, `e2e-npm-artifacts-<set>` and on failure the cluster state |
| fuzz npm (closing run only) | `make fuzz-npm FUZZ=<target> FUZZTIME=30m` | three legs (`FuzzR_ARCH_028_Match`, `FuzzR_ARCH_028_Inspect`, `FuzzMatch`) of 30 minutes each; runs only on a dispatch with `closing=true`; uploads the failing input |
| fuzz nuget (closing run only) | `make fuzz-nuget NUGET_FUZZ_TARGETS=<pkg>:<target> NUGET_FUZZTIME=30m` | eight legs of 30 minutes, one per target of `NUGET_FUZZ_TARGETS` (the package below `internal/`): the readers `formats/nuget/nupkg:FuzzR_ARCH_028_Inspect` and `formats/nuget/nuspec:FuzzR_NUGET_005_Nuspec`, the version and ID rules `formats/nuget/version:FuzzNormalize`, `…:FuzzCompare`, `…:FuzzRange`, `formats/nuget/id:FuzzID`, and the reader of a NuGet signature `signatures/scheme/nugetcms:FuzzSignatureP7s` and `signatures/scheme/rawkey/pki:FuzzDefinite`; they run side by side ([Q-ARCH-278](../spec/architecture.md#q-arch-278--how-is-the-nuget-fuzz-run-of-the-closing-run-laid-out)); runs only on a dispatch with `closing=true`; uploads `fuzz-nuget-<target>` with the failing input |
| e2e (nuget) | the NuGet clients image of the cache, the image of the run, `make dev-up`, `make e2e SET=nuget`, `make dev-down` | the NuGet acceptance suite (R-NUGET-027, E3.1 to E3.8 as they land): the .NET 10 SDK and nuget.exe on Mono in the client pods, the scatter profile and the counting NuGet test upstream; runs when the push or pull request changed NuGet's own paths, and on every dispatch; nightly as `format-nuget`; own cluster name and edge port (8088). The clients image comes from [client-images](../../.github/actions/client-images/action.yml) and is not built again by the set (`QUADER_E2E_NUGET_CLIENTS_IMAGE_BUILD=0`). A dispatch with `closing=true` adds the leg `nuget-closing` (port 8089): the tests named `TestNuGetClosing...`, the additions of the closing run. The nightly leg `nuget-ha` (port 8090) is in [HA and Performance (nightly)](#ha-and-performance-nightly). A set whose expression names no test yet passes with a notice, as `nuget-closing` and `nuget-ha` do today ([Q-ARCH-276](../spec/architecture.md#q-arch-276--how-does-an-e2e-set-that-names-no-test-pass-and-when-does-it-fail)); uploads `e2e-nuget-report-<set>`, `e2e-nuget-artifacts-<set>` and on failure the cluster state ([e2e.md](e2e.md#the-nuget-clients)) |
| e2e (oci) | the OCI clients images of the cache, the image of the run, `make dev-up`, `make e2e SET=oci`, `make dev-down` | the OCI acceptance suite (R-OCI-035, T-16 WP11): the Docker Engine with Buildx and Podman in privileged client pods of the namespace `quader-e2e-clients`, the scatter profile and the counting OCI test upstream ([the OCI tests](e2e.md#the-oci-tests)); runs when the push or pull request changed OCI's own paths (the registry root and its token service among them), and on every dispatch (a closing dispatch adds the leg `oci-closing`); nightly as `format-oci`; on a large runner, own cluster name and edge ports (8104, 8105). The two clients images come from [client-images](../../.github/actions/client-images/action.yml) and are not built again by the set (`QUADER_E2E_OCI_CLIENTS_IMAGE_BUILD=0`). The nightly leg `oci-long` (edge port 8106) is job `e2e-oci` in performance.yml: a push cut by the drain of the receiving pod |
| e2e (deb) | the APT clients images of the cache, the image of the run, `make dev-up`, `make e2e SET=deb`, `make dev-down` | the Debian (APT) acceptance suite (R-DEB-035): a real APT of Debian 13, Debian 12, Ubuntu 24.04 and Ubuntu 22.04 in client pods, the scatter profile and the counting Debian archive test upstream ([the Debian tests](e2e.md#the-debian-tests)); runs when the push or pull request changed Debian's own paths (`internal/openpgp` among them), and on every dispatch (a closing dispatch adds `deb-closing`); nightly as `format-deb`; on a large runner, edge ports 8107 and 8108; uploads `e2e-deb-report-<set>`, `e2e-deb-artifacts-<set>` and on failure the cluster state |
| e2e (ansible) | the Ansible clients images of the cache, Node.js and the browser, the image of the run, `make dev-up`, `make e2e SET=ansible`, `make dev-down` | the Ansible Galaxy acceptance suite (R-ANS-032): `ansible-galaxy` of ansible-core 2.17 and 2.21 in client pods and the browser journey of the collection documentation, the scatter profile and the counting Galaxy test upstream ([the Ansible tests](e2e.md#the-ansible-tests)); runs when the push or pull request changed Ansible's own paths (its browser journey under `web/e2e/ansible-docs` among them), and on every dispatch (a closing dispatch adds `ansible-closing`); nightly as `format-ansible`; on a large runner, edge ports 8117 and 8118; uploads `e2e-ansible-report-<set>`, `e2e-ansible-artifacts-<set>` and on failure the cluster state |
| e2e (bastion) | the npm and NuGet clients images of the cache, the image of the run, `make dev-up`, `make e2e SET=bastion`, `make dev-down` | the short scenarios of R-BAS-049 with two Bastions and the browser journey of the Bastions screen ([the Bastion tests](e2e.md#the-bastion-tests)); runs when the push or pull request changed the Bastion's paths (its list in [e2e-legs.sh](../../.github/scripts/e2e-legs.sh), unchanged) or its job, and on every dispatch; otherwise the job is skipped and takes no runner; edge port 8093; the long set `bastion-long` is nightly |
| e2e (ci) | the npm and NuGet clients images of the cache, the image of the run, `make dev-up`, `make e2e SET=ci`, `make dev-down` | the reference pipeline of GitLab CI OIDC (R-CI-028, E4.1 and E4.4 to E4.6): the steps of the Examples with the real npm and .NET clients in the client pods against the issuer double, the scatter profile; runs on every change, own cluster name and edge port (8091). Both clients images come from [client-images](../../.github/actions/client-images/action.yml) (`QUADER_E2E_NPM_CLIENTS_IMAGE_BUILD=0`, `QUADER_E2E_NUGET_CLIENTS_IMAGE_BUILD=0`). The long legs are the set `ci-ha` of [HA and Performance (nightly)](#ha-and-performance-nightly); uploads `e2e-ci-report-ci`, `e2e-ci-artifacts-ci` (with the job logs `ci-job-*.log`) and on failure the cluster state ([e2e.md](e2e.md#the-ci-tests)) |
| e2e (vuln) | the npm and NuGet clients images of the cache, the image of the run, `make dev-up`, `make e2e SET=vuln`, `make dev-down` | the vulnerability scanning on kind (R-VULN-029, E4b.4 to E4b.6, the short half of E4b.8): the `vuln` profile (three `web`, two `worker` replicas, the scatter edge, the test advisory source), `npm audit`, `pnpm audit`, `yarn npm audit` and the blocked installs of every gate row, `dotnet restore` of the .NET 8, 9 and 10 SDKs, the replica criteria with Valkey stopped and the browser journeys with axe (the job installs Node.js and the browser); runs when the push or pull request changed the scanning's paths (its list in [e2e-legs.sh](../../.github/scripts/e2e-legs.sh), the scanning, the core paths a blocked download goes through and the e2e harness) or its job, and on every dispatch; otherwise the job is skipped and takes no runner; own cluster name and edge port (8095). The long legs are the set `vuln-long` of [HA and Performance (nightly)](#ha-and-performance-nightly); uploads `e2e-vuln-report-vuln`, `e2e-vuln-artifacts-vuln`, `e2e-vuln-playwright-vuln` and on failure the cluster state ([e2e.md](e2e.md#the-vulnerability-tests)) |
| e2e (push, access-long) | the image of the run, `make dev-up`, `make e2e SET=<set>`, `make dev-down` | the dev profile on a kind cluster, one leg per set, every change: `push` and `access-long` (E1.1, E1.11 with the Playwright journeys, E1.13; the only leg that installs Node.js and the browser). The set `failover` (E0.13, E1.8) left this job on 2026-11-01 (owner) and runs nightly and on dispatch in [HA and Performance (nightly)](#ha-and-performance-nightly); the legs and every other kind leg load the image of the job `image` and start after it (Q-ARCH-522); each leg has its own cluster name and edge port; uploads the reports, `e2e-artifacts-<set>`, and on failure the cluster state |
| workflow files (actionlint) | `actionlint` (pinned `ACTIONLINT_VERSION`) | every file of `.github/workflows`, with [.github/actionlint.yaml](../../.github/actionlint.yaml) |
| compatibility with the latest release | `make compat-check` | the OpenAPI document against the latest `v0.*` release with the pinned oasdiff (`--fail-on ERR`), the migration files: only additions that continue the numbering, and no change of a migration that `main` has committed (the history of `main`); needs the history and the tags (`fetch-depth: 0`, `fetch-tags: true`); see [Compatibility](#compatibility). Switched off until the first stable release (`if: false`, not among the needs of Semantic Release; [the switches](#switched-off-until-the-first-stable-release)) |
| coverage generator (links and rules) | `go run ./internal/tools/coverage -check`, then `-links` | the generated pages match their rules; every relative link and anchor resolves |
| coverage evidence (implemented.txt) | `go run ./internal/tools/coverage -claims -tests …` over the unit, Angular, chart, integration and kind-leg reports of this run, and `-carried` over the newest report of each kind leg without one (Q-ROAD-11) | [implemented.txt](../coverage/implemented.txt) claims no requirement the reports do not prove; writes the record and the pages rendered from it, lists the requirements proven for the first time in the job summary and uploads the files as `coverage-evidence` for Semantic Release; needs `unit`, `web`, the `integration` shards, `chart`, `image` and the kind legs, runs when none of them failed or was cancelled (a leg the change did not start is skipped and does not stop it; its newest report is carried), and holds `actions: read` for the reports of earlier runs; a failing test in such a report withdraws its requirement from the record without failing the job (Q-ROAD-12) |
| Combined Coverage Report | `make coverage-report` | merges the unit and integration profiles; the job summary and the badge `coverage-badge` |
| Release Tooling | `make test-release-tooling` | `npm ci`, `npm audit signatures`, and [hack/verify-release-tooling.mjs](../../hack/verify-release-tooling.mjs), which renders release notes with the installed plugins |
| Semantic Release | – | needs the static checks, unit, web, chart, image, the scans, `workflows`, `coverage` and `release-tooling`; the integration, S3 conformance and kind legs and the two coverage jobs that need them still run and do not gate it until the first stable release (Q-ARCH-457); runs only on a push to `main`; publishes the release ([release flow](#release-flow)). A gating job that failed or was cancelled at its limit skips it; re-running the failed jobs of the run runs it after them ([when a gating job hung](#when-a-gating-job-failed-on-the-network)) |

A kind leg that fails keeps its cluster state as the artifact `e2e-cluster-state-<set>` (14 days): per namespace the pods, their
descriptions and the events, and the logs of every pod of `quader` and of the edge (`ingress-controller`) whole, with the kubelet's
timestamps, compressed, plus the previous instance of every container that restarted ([deploy/dev/cluster-logs.sh](../../deploy/dev/cluster-logs.sh),
called by every such step). The logs are whole because [kind.yaml](../../deploy/dev/kind.yaml) raises the kubelet's `containerLogMaxSize`
to 200Mi (`kubectl logs` reads only the current file, so the default 10Mi rotation cut the window of a failure out of the log), and the edge writes an
access line per request with HAProxy's termination state ([haproxy-values.yaml](../../deploy/dev/haproxy-values.yaml)), whose flags name the side
that ended a connection. Read a log with `zcat log-<ns>-<pod>.txt.gz`. The size is the logs' size, about a tenth of it compressed; it is bounded by the
run, not by a tail.

The workflows pin their actions by major version tag (`actions/checkout@v7`), the way the
organisation's other repositories do. Two actions are pinned by a release tag instead:
`aquasecurity/trivy-action@v0.36.0` and `renovatebot/github-action@v46.3.10`. Third-party actions that
run with the release secrets are pinned by tag too: the owner decided so on 2026-10-31 after the risk
was named ([ADR-130](../decisions.md#adr-130--the-organisations-ci-and-release-standard)). A tag can be
moved, and a moved tag runs in the job that names it with that job's secrets
([H-325](../security/data-and-secret-flow.md#h-325--a-moved-action-tag-runs-in-a-job-that-holds-release-secrets)).

### Which e2e legs a change runs

By the owner's decision of 2026-11-20 ([Q-ARCH-540](../spec/architecture.md#q-arch-540--a-formats-e2e-leg-runs-on-a-push-only-when-that-formats-own-code-changed)):
a format's e2e leg runs on a push only when that format's own code changed. A change of shared code runs the tiers below it (the unit tests
with the format contract's kit for every format, the integration shards) and the leg of one reference client, npm
([Q-ARCH-541](../spec/architecture.md#q-arch-541--npm-is-the-reference-leg-of-shared-code)). Every format's set runs in the nightly
([below](#ha-and-performance-nightly)), so what only another format's client shows of a change of shared code is found there, within a day.
Until the first stable release no e2e leg gates the release anyway ([Q-ARCH-457](../spec/architecture.md#q-arch-457--until-the-first-stable-release-a-minimal-set-of-jobs-gates-semantic-release)).

One job decides for the run ([Q-ARCH-543](../spec/architecture.md#q-arch-543--one-job-on-the-standard-runners-decides-which-e2e-legs-run)):
`e2e-scope` ("which e2e legs the change runs", `self-hosted`) runs [.github/scripts/e2e-legs.sh](../../.github/scripts/e2e-legs.sh) on the
changed paths of the push (`git diff --no-renames --name-only <before> <sha>`), or of a pull request (its head against the merge base with
its base, so that commits the base branch got since do not count), and outputs `true` or `false` per leg; its job summary has
the table, its log the legs each path started. Every leg it decides `needs: [image, e2e-scope]` and runs
`if: !cancelled() && needs.image.result == 'success' && needs.e2e-scope.outputs.<leg> != 'false'`: a leg that does not apply is a skipped
job and takes no runner (before, `e2e (bastion)` took a GitHub-hosted runner for 130 s, 117 of them freeing its disk, and found nothing to
do), and a decision that failed or wrote nothing runs the leg.

| Leg | What starts it on a push or a pull request |
|---|---|
| `e2e (npm)` | npm's own paths, or shared code (every path that is no format's alone, not on the list of paths that start no format leg and not under `web/`) |
| `e2e (nuget)`, `e2e (oci)`, `e2e (deb)`, `e2e (ansible)` | that format's own paths |
| `e2e (bastion)` | the Bastion's list, as the job had it: `internal/bastion`, `internal/bastionmain`, `deploy/helm/quader-bastion`, `docs/spec/bastion.md`, its e2e profile, suite and browser journey; and its own job in release.yml |
| `e2e (vuln)` | the scanning's list, as the job had it (the scanning, the parts of npm and NuGet that answer an audit, `internal/formathost`, `internal/items`, `internal/files`, `internal/proxy`, its queries and API, its test advisory source and profile, `deploy/dev/e2e.sh`, the NuGet clients, its screens and journeys, its spec); and its own job in release.yml. Two patterns of the old list never matched a file (`queries/vuln_` anchored at the root, `internal/api/v1/vuln_api`); they stand as the paths they meant |
| `e2e (push)`, `e2e (access-long)`, `e2e (ci)` | every change, not decided here |

A pull request is decided like a push, by the same lists: a dependency bot's pull requests then take the legs their change concerns and
not every format's leg on the shared runners, which has starved `main` before. What a dependency pull request starts (from the test below): a
bump of `go.mod` and `go.sum` is shared code and starts npm alone; a bump of one client pin in `tools/versions.env` starts that format's leg
alone (its own line and the Renovate comment above it); a bump of an action in release.yml that every job uses, such as `actions/checkout`,
changes every leg's job and starts every leg (as it should: each leg's own steps changed), the same bump in `performance.yml` alone starts
none; the weekly group `non-major dependencies` starts the union of what its files start. A dispatch and the first push of a branch (a
`before` of zeros or one that is not in the clone) run every leg, and so do a pull request whose base or head is not in the clone and an
error of the script, which fails open. `npm` and `nuget` ran on every pull request before, and the other five legs filtered it by their old
lists.

**A format's own paths** (the patterns are `format_paths` in the script; why each is the format's):

| Kind of path | Pattern | Why it is the format's |
|---|---|---|
| its package | `internal/formats/<format>/` | its protocol, handlers and guide |
| its data port | `internal/formatdata/<format>/` | its tables and jobs |
| its files in shared packages | a file under `internal/` or `cmd/` whose name carries the format's name as a word (`internal/config/oci.go`, `internal/audit/events_npm.go`, `internal/app/wire_deb_index.go`, `internal/importer/ansible.go`, `internal/api/v1/deb_keys.go`, `internal/backup/fixup/nuget.go`, `cmd/quader/admin_oci.go`, `internal/meta/postgres/queries/deb_local.sql`, the migrations named after it) | they exist for that format only |
| its test upstream and manifests | `test/<format>stub/`, `deploy/dev/<format>stub.yaml`, and the client manifests `deploy/dev/oci-clients.yaml`, `apt-clients.yaml`, `ansible-clients.yaml` | they serve its set only |
| its e2e tests and client images | `test/e2e/<format>_`, and `test/e2e/clients/` (npm, without `clients/fwd`), `nugetclients/`, `ociclients/`, `aptclients/`, `ansibleclients/` | its set runs them, no other |
| its part of release.yml | the lines of its job `e2e-<format>` and the comment above it | the leg itself |
| its pins | the lines of `tools/versions.env` that set `E2E_CLIENTS_*` (npm), `E2E_NUGETCLIENTS_*`, `NUGETVECTORS_*`, `NUGET_VERSIONING_*` (NuGet), `E2E_OCICLIENTS_*`, `E2E_APTCLIENTS_*` (Debian), `E2E_ANSIBLECLIENTS_*`, with the comment above each | its client images and vectors |
| per format | NuGet: `tools/nugetvectors/`, `perf/nuget/`; OCI: `internal/api/registry/` (the registry root), `internal/auth/registrytoken/` (its token service), `internal/app/wire_registry.go`, `internal/audit/events_registry_token.go`; Debian: `internal/openpgp/` (every importer is Debian code); Ansible, the one format leg that runs a browser journey (`TestAnsibleDocsJourney`): `web/e2e/ansible-docs/` and `web/e2e/playwright.ansible-docs.config.ts`, what the journey runs on (`web/e2e/playwright.config.ts`, `global-setup.ts`, `support.ts`, `tsconfig.json`, and `web/package.json`, `package-lock.json`, `.nvmrc`, which the leg installs) and the screens it opens (the files of `web/src` named `ansible-*`, not their `.spec.ts`) | |

**Shared code** ([Q-ARCH-542](../spec/architecture.md#q-arch-542--shared-code-is-every-path-that-is-no-formats-alone-a-path-on-no-list-is-shared)) is every path
that is on no format's list and not in the list below, so a path that is on no list at all starts npm, never nothing. Today that is:
`internal/` but the formats' paths (the server's core: `httpserver`, `formathost`, `formats/*.go`, `formats/all`, `formats/formattest`, `mirror`,
`proxy`, `virtual`, `uploads`, `items`, `files`, `jobs`, `auth`, `authz`, `app`, `meta`, `cluster`, `limits`, `vuln`, `bastion`, ...), `cmd/`, `api/`,
`deploy/helm/quader/`, `deploy/dev/` (`up.sh`, `e2e.sh`, `lib.sh`, `kind.yaml`, `pki.yaml`, the profile's values and
manifests) but the formats' stubs and clients, test/e2e's harness (`env_test.go`, `m1_env_test.go`, `mirror_env_test.go`,
`limits_clients_env_test.go`, every e2e test of no format, `cmd/driver`, `k6/`, `clients/fwd`, `Dockerfile`), the other stubs under `test/`,
`.github/actions/` and `.github/scripts/`, the `Makefile`, the `Containerfile`, `go.mod` and `go.sum`, `perf/` but NuGet's, `tools/` but NuGet's
vectors, the pins of `tools/versions.env` that are no format's, the manual under `docs/manual/` (it is embedded in the binary), and in
release.yml the header and the jobs `image` (the legs load its image) and `e2e-scope`.

**No format leg** (`no_format_leg` and `web_no_format_leg`; for each, no format leg builds, deploys or reads it): `web/` but the paths on
Ansible's list (the format legs drive the clients, not the UI; the UI is what `access-long`, which runs on every change, the UI layout checks
and the web unit tests see, and the built UI embedded in the image is no reason, as no other format leg opens it; revisited when a format's
leg starts to exercise the UI beyond its list), the documentation but `docs/manual/`, the Markdown
files at the root, `test/integration/` (nothing outside it imports it), Go test files outside `test/e2e/` (Go builds a `_test.go` file into
its own package's test binary only), `internal/tools/` and `internal/archtest/` (neither the binaries nor the e2e tests import them),
`test/chart/`, `test/compat/`, `test/coverage/`, the root `package.json` and `package-lock.json` and `hack/` (semantic-release), the test
advisory source and the CI issuer double (`test/osvstub/`, `test/oidcstub/` and their manifests: `deploy/dev/e2e.sh` starts them for the
vuln sets and for ci, access-long and backup only), the Bastion's chart, `deploy/ui-dev/` and `test/devidp/` (the layout checks), the
workflows `performance.yml`, `build.yml` and `renovate.yml`, `.github/actions/select-leg/`, the configuration of golangci-lint, Trivy,
Renovate, semantic-release and actionlint, and in release.yml every job that is not a leg's, `image` or `e2e-scope`. A compile error in a
file of this list that a leg builds is found by `lint`, which builds every tag.

To see the tree as the rules see it: `git ls-files | .github/scripts/e2e-legs.sh --classify` (the format legs, `shared` or `none`, and
`bastion` and `vuln`); `.github/scripts/e2e-legs.sh --lists` prints every pattern.
[e2elegs_test.go](../../internal/tools/ciworkflows/e2elegs_test.go) runs the script against git repositories of its own (about 21 s, no
network) through these cases, each with the legs it runs: documentation only (none; such a push starts no run at all), only
`internal/formats/oci/` (oci), only `internal/formats/npm/` (npm), only `internal/httpserver/` (npm), a new directory under `internal/` on no
list (npm), a path outside every list (npm), only `web/` (none), a screen the Ansible journey opens (ansible), its `.spec.ts` (none), the web
workspace's lockfile (ansible), a format's e2e test (that format), a format's unit test and an integration
test (none), a format's file in a shared package (that format), the forwarder of the client images (npm), npm's client image (npm),
Ansible's journey (ansible), two formats (both), a file moved out of a format (npm and that format), the Bastion's code (npm and bastion),
the Bastion's chart (bastion), `deploy/dev/e2e.sh` (npm and vuln), the scanning's queries (npm and vuln), a line of the OCI job, the comment
above the Debian job, a line of the unit job, the header and a line of the Bastion job of release.yml (oci, deb, none, npm, bastion), an OCI
client pin and a pin every leg uses (oci, npm); as pull requests: only a format's paths (that format), only shared code (npm), only
documentation (none), a bump of `go.mod` (npm), a bump of one client pin (oci), a bump of `actions/checkout` in release.yml (every leg), a
change of `performance.yml` alone (none), and a pull request whose base branch moved on (its own change only, from the merge base); and a
dispatch, a pull request whose base is not in the clone, a first push, a push without `before`, a `before` and a head that are not in the
clone (every leg). It also checks that every pattern matches a file of the tree, and how the workflow wires the legs.

What a push costs under the rule, from the job records of Test and Release (runner minutes; `e2e-scope` estimated at a quarter of a minute,
not measured): run 44693590111, which changed each format's e2e test, 396.1 before and about 394 under the rule (only `e2e (bastion)`, 2.2,
is not started); run 44646960056, which changed npm, NuGet, Debian and Ansible code and shared code, 384.9 and about 370 (`e2e (oci)`, 13.4,
and `e2e (bastion)` not started); a push of shared code alone, such as `internal/httpserver/`, about 308 from the job times of run
44693590111 (NuGet, OCI, Debian, Ansible, vuln and bastion not started). With 25 formats that push would cost about 508 instead of the
939 of [test-cost.md](test-cost.md#44-mean-and-projection-to-25-formats-under-todays-structure): the formats' own legs leave it, their
integration packages and screens stay.

### Pinned tools in the jobs

A pin in [tools/versions.env](../../tools/versions.env) is the one source for the jobs and for `make versions-check`. Where a
job downloads a pinned archive from a GitHub release, the download can fail on the release host (504 in run 42465102401 on
2026-11-08, verified in its log; run 42464736969 reported the same), so the Docker Buildx of every job is installed by [.github/actions/install-buildx](../../.github/actions/install-buildx/action.yml)
([Q-ARCH-355](../spec/architecture.md#q-arch-355--how-do-the-ci-jobs-get-their-pinned-tools-without-a-live-release-asset-download-per-job)):

1. the file the Docker CLI resolves (`docker info`: the buildx plugin's `Path`), when its version is the pin and its SHA-256 is the
   pinned digest of the platform: nothing is installed;
2. otherwise the archive cached under `.bin/buildx-<pin>/` (the cache key holds the pin and the digest of the platform);
3. otherwise a download of the release asset with [tools/github-download.sh](../../tools/github-download.sh): with the job's token
   (`GITHUB_TOKEN`, sent to github.com only, [Q-ARCH-356](../spec/architecture.md#q-arch-356--how-do-ci-jobs-download-github-release-assets-while-the-release-host-is-degraded)),
   the bounds of the script: a hard budget of 900 s for the whole download (each curl call is limited to the time left; the step has a
   16-minute timeout, and the `versions` and `chart` jobs, which are 20 minutes long, give the download 600 s), up to three rounds for a
   signed redirect that expires, and an attempt that stalls under 50 KiB/s for 30 s is aborted;
4. the archive is used only when its SHA-256 equals `BUILDX_SHA256_LINUX_AMD64` or `_ARM64` of `tools/versions.env`;
5. the plugin is installed into `${DOCKER_CONFIG:-$HOME/.docker}/cli-plugins`, and the CLI is checked to resolve that file.

The step is skipped where the job's setup-buildx step is skipped (the legs of a dispatch, the scope of a release job), and the
composite refuses an empty version.

The `docker/setup-buildx-action` step that follows runs with `cache-binary: false` and without `version`, so it never
downloads or caches the binary. Its cache of the binary is the entry whose paths broke the restore of run 42465102401: the
action stores paths relative to the workspace, so an entry saved in one workspace layout does not extract in another.

Every download of both workflows, what determines it and what caches it, is in
[What the jobs fetch and what is cached](#what-the-jobs-fetch-and-what-is-cached).

`make dev-up` downloads no tool: `deploy/dev/lib.sh` `check_tools` checks kind, kubectl and Helm against their pins and prints the
install command when one is missing. The charts of its releases go into `.dev/charts`: `make dev-charts` ([deploy/dev/charts.sh](../../deploy/dev/charts.sh))
fetches the haproxy-ingress and cloudnative-pg archives, which are GitHub release assets, through `chart_release_tgz` (the release URL of the version,
the digest of [tools/versions.env](../../tools/versions.env), and the job's token when `GITHUB_TOKEN` is set). In CI that is its own step, before the
`make dev-up` step, which does not hold the token; `make dev-up` reads the archives from `.dev/charts`, and fetches a missing one itself, without the token.
The others go through `chart_tgz`, which runs `helm pull`
([deploy/dev/lib.sh](../../deploy/dev/lib.sh)). A failed pull is retried, 20 s apart, up to seven attempts and until 600 s have passed since the first
(`CHART_PULL_ATTEMPTS`, `CHART_PULL_SECONDS`); a version the repository does not list is not retried. The deadline is checked between attempts, so an attempt that is running may end after it. The haproxy-ingress and cloudnative-pg chart archives are
GitHub release assets (the chart indexes point there); in run 42465102401 their pulls got 504 (haproxy after about a minute,
cloudnative-pg after about 5 minutes). Their digests are pinned ([Q-ARCH-356](../spec/architecture.md#q-arch-356--how-do-ci-jobs-download-github-release-assets-while-the-release-host-is-degraded),
[H-589](../security/data-and-secret-flow.md#h-589--release-downloads-send-the-job-token-to-githubcom-two-residuals) for the cloudnative-pg one). The cache of those archives in CI is not in place yet
([Q-ARCH-355](../spec/architecture.md#q-arch-355--how-do-the-ci-jobs-get-their-pinned-tools-without-a-live-release-asset-download-per-job)).

### Accepted findings of the container scan

The container scan gates the release, and it scans everything in the image: Quader's own binary on the distroless base. Until
2026-11-21 the image also carried the analyser of the image scanning, the upstream release archive at its pin, a binary Quader does
not build; since R-HA-070 the scanner pods copy the analyser from its own image (`TRIVY_IMAGE`), which this job does not scan
([H-1003](../security/image-scanning.md#h-1003--the-analyser-is-an-upstream-build-with-the-vulnerabilities-of-its-day)). When the
vulnerability database learns of a finding in a binary of the image Quader does not build, the gate turns red on a commit that changed
nothing, and stays red until upstream releases a new build. [.trivyignore.yaml](../../.trivyignore.yaml) is the place for such a
finding, and only for it:

- an entry names **one identifier**, the **path or paths in the image** it is accepted at (never the whole image), the **day it
  ends** (`expired_at`) and, in `statement` and the comment above it, **why the finding does not apply** where it is accepted;
  `TestAcceptedFindingsAreScopedAndEnd` ([internal/tools/ciworkflows](../../internal/tools/ciworkflows)) refuses an entry
  without one of them, and `TestBothScanStepsReadTheAcceptedFindings` that the report's scan reads the same file as the gate's;
- the same identifier at any other path fails the scan as before, and an entry past its day fails it again (both shown with
  the pinned Trivy on 2026-11-19: accepted at the named path, exit 1 at another path, exit 1 with a day in the past);
- a finding in Quader's own binary or in the base image is never entered here: it is fixed (the toolchain, the dependency, the
  base image's pin);
- the entry goes in the commit that raises the pin of the binary it names.

The file holds no entry since 2026-11-21. Its first entry (2026-11-19 to 2026-11-21) was CVE-2026-97054, a finding of `net/http`'s
server in the analyser built upstream with Go 1.27.3, at `usr/local/bin/trivy`, which the release image no longer has; Quader runs the
analyser as a command for one scan and never as a server, and the finding stays with the pinned analyser
([image-scanning.md](../security/image-scanning.md#h-1003--the-analyser-is-an-upstream-build-with-the-vulnerabilities-of-its-day)).

## OS packages in the jobs

A step that needs an Ubuntu package calls [.github/scripts/apt-install.sh](../../.github/scripts/apt-install.sh) with the package
names; the [build-tools](../../.github/actions/build-tools/action.yml) action calls it for what the runner lacks. No file under
`.github/` runs apt itself: `TestNothingUnderGithubRunsAptGetItself` of
[internal/tools/ciworkflows](../../internal/tools/ciworkflows/workflows_test.go) fails on any `apt-get` there, and the unit job runs it.

Why: in Test and Release 44384406557 (2026-11-18) the gating job `chart` started `apt-get update` at 19:50:58, apt gave up one
request to `archive.ubuntu.com` at 19:51:28 (`Ign:1`, apt's own 30 s), started two more to the same host, and printed nothing
again until GitHub cancelled the job at its 20-minute limit at 20:10:06. Semantic Release was skipped and the commit went
without a release. The bare `apt-get update || warning` of every call site guarded an update that fails, not one that hangs.

What the script does, in order:

1. A package that is installed (`dpkg-query`) is skipped; when all are, the script ends at once without sudo or the network.
2. The package indexes are read once per job: a successful update leaves a stamp in `RUNNER_TEMP`, and a later call of the job
   does not update again.
3. The update reads the Ubuntu sources only, when the image has `/etc/apt/sources.list.d/ubuntu.sources` (the Ubuntu 24.04
   layout): `-o Dir::Etc::sourcelist=<that file> -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0`. The runner image adds the
   git PPA, which answered after six seconds in the hung run and with 503 on 2026-11-11 (Release Tooling of 43063590899), and no job
   installs anything from it. Without that file every source is read.
4. The packages are downloaded first (`install --download-only`), then unpacked from the cache (`install --no-download`), so the
   bound that ends a stalled download never stops dpkg in the middle of a package.
5. Each of the three calls runs under `timeout` (TERM, then KILL 10 s later) and with `Acquire::http::Timeout=20` and
   `Acquire::https::Timeout=20`.
6. The update runs with `Acquire::Retries=0`, so that it ends by itself: apt gives up a source that does not answer, keeps the
   indexes of the sources that did and exits 0. An update that the bound ends keeps nothing, also not what it had read. The retry
   is the script's: an update that did not read every source (a request whose last line is `Err`), that failed or that the bound
   ended runs once more, on a new connection. Only an update that read every source leaves the stamp of step 2.
7. The download runs with `Acquire::Retries=3`. A download that the bound ended runs once more; one that failed by itself does
   not, since apt has retried each request already.
8. An update that is incomplete after its second run is a warning, and the install goes on from the indexes present. A failed
   download or unpacking fails the step with `::error::`, naming the packages and the hosts apt was waiting on.

Why the update retries nothing itself: in HA and Performance 44453781574 (2026-11-19, `e2e (nuget-ha)`) `security.ubuntu.com`
did not answer during the first update, which had read all of `archive.ubuntu.com` after 51 s, and `archive.ubuntu.com` did not
answer during the second, which had read all of `security.ubuntu.com` after 24 s. apt printed its first `Ign` for the silent
host 41 s after the start both times; with three retries it would have needed about 164 s to give the host up, so the bound
of 60 s ended both updates, each lost what it had read, and the install failed with `Unable to locate package build-essential`
after 120 s. Measured in an `ubuntu:24.04` container beside a source that accepts the connection and never answers: apt gives
it up after 41 s with no retry, 82 s with one, 168 s with three, each time with exit 0 and the indexes of the other sources.
One silent source therefore fits into the bound only without a retry. Three suites on one silent host (`archive.ubuntu.com`
carries `noble`, `noble-updates` and `noble-backports`) are asked one after the other and still exceed it; the indexes a
complete earlier update left stay in place then.

The numbers, measured in the green run before it (Test and Release 44372205911, the logs of the 28 jobs that called apt-get,
`gh api repos/guided-traffic/quader/actions/jobs/<id>/logs`, 33 updates):

| Bound | Value | Measured in the green run | Why this value |
|---|---|---|---|
| A request without a byte (`Acquire::http::Timeout`, `https`) | 20 s | apt gave up a stalled request to `archive.ubuntu.com` after 30 s (as in the hung run) and its retry succeeded at once (`web (Angular)`: 33 s for 10 kB of `libatomic1`); another update waited 22 s on the same host; the slowest answer of a healthy host was the PPA's, about 6 s | ends such a stall 10 s sooner and stays at three times the slowest healthy answer |
| Retries of a failed request in the update (`Acquire::Retries`) | 0 | a silent source: 41 s without a retry, 82 s with one, 168 s with three (container, 2026-11-19; 41 s to the first `Ign` in `nuget-ha` of 44453781574) | the update must end inside its 60 s to keep the indexes it read; the script's second update is the retry |
| Retries of a failed request in the download (`Acquire::Retries`) | 3 | – | apt's own retry is what recovered the stall |
| `apt-get update` | 60 s | 3 to 35 s, median 8 s; 34 MB of indexes; the 35 s held the stall of 22 s | about 1.7 times the slowest, which itself held a stall |
| Download of the packages | 90 s | 2 to 14 s for 82 to 102 MB (`build-essential` is 87 packages, with ClamAV 99) | more than six times the slowest; 1 MB/s still finishes |
| Unpacking | 120 s | the whole install step, update and download included, took 7 to 45 s | several times the longest whole step |
| Step `timeout-minutes` of a package step in a gating job | 9 | – | above the script's worst case, 2 × 70 + 2 × 100 + 130 = 470 s, so the script's own message comes first; below the 15 minutes of the shortest job that installs packages (`build without Node.js`, `Release Tooling`) |

Not chosen: `Acquire::ForceIPv4`. The hung run's log names no address family, and the outage of F-117 broke IPv6 and IPv4
alike; forcing IPv4 would take away the family that works when IPv4 is the broken one.

What it was tested against. The unit test [aptinstall_test.go](../../internal/tools/ciworkflows/aptinstall_test.go) runs the script
with `dpkg-query`, `sudo`, `timeout` and `apt-get` stubbed (no root, no network): the installed case, the bounds and options of each
call, the one update per job, an update that hangs once and twice, an update that ends without one source once and twice, a
failed update, a download that hangs twice and one that fails,
and an `awk` that crashes while the error is worded (the exit status stays apt's; macOS awk did crash now and then when the
environment held a value with newlines, which is why the stubs read their output from files).
By hand, on 2026-11-18, in `ubuntu:24.04` containers (arm64, so `ports.ubuntu.com`): a healthy install, the update that does not
contact an extra source in `sources.list.d`, and a source that answers with headers and then one byte every 5 seconds. apt's own
timeout never fired on that source; the script ended the update at 60 s, read again, gave up at 120 s and failed the install of
`jq` with `Unable to locate package` at 121 s, because an update that never finished leaves a runner image without indexes.
By hand, on 2026-11-19, in the same container beside a source that accepts the connection and never sends a byte: the script
before the change ended both updates at 60 s and failed with `Unable to locate package jq` after 120 s, as in `nuget-ha`; the
script after it let each of the two updates end by itself after 41 s, warned twice (`could not read dead.example`) and installed
`jq` from the other sources, 85 s in all. In the same setup an update that the bound ended left the 15 index files of a complete
earlier update in place, and three suites on the silent host took 121 s without a bound.

#### The steps that download in the gating jobs

Every job among the `needs` of Semantic Release, and Semantic Release itself, gives each step that downloads outside its main
make target a step timeout, so a host that does not answer fails that step in minutes and names it, instead of holding the job
until its limit: 9 minutes for a package step (above), 5 for `actions/setup-go`, `actions/setup-node`, `azure/setup-helm`,
`azure/setup-kubectl`, `freshclam` and `npm ci` (the slowest of them in the green run: `actions/setup-node` with
the npm cache, 92 s), 10 for the Go modules ([go-modules](../../.github/actions/go-modules/action.yml), whose download is bounded at
300 s by [go-mod-download.sh](../../.github/scripts/go-mod-download.sh)) and for a Go tool ([go-tool](../../.github/actions/go-tool/action.yml),
`go install` bounded at 300 s; 8 in the 10-minute job `workflows`). `TestTheGatingJobsBoundTheirDownloads` holds this, and that a
package step's bound lies between the script's worst case and the job's limit. The modules are downloaded in that step and nowhere
else: it comes right after the Go setup, before any step that runs make or go (`TestEveryGoJobRestoresTheCompleteModuleCache`), and a
make target after it finds every module of `go.sum` in the cache (F-156: before, `make chart-test` downloaded 75 modules inside the
target, and one stalled for 16 minutes). Left to the job's limit: `actions/checkout`, the artifact and cache actions,
`npx semantic-release`, and the make targets themselves (the image builds and the scans).
Outside the gating jobs the script bounds itself; the Playwright steps of the kind legs, whose `--with-deps` runs apt itself
on a runner that lacks the browser's libraries, have `timeout-minutes: 25`: the step took 101 s in a green run (`npm ci` included), and over 10 minutes on the night of
2026-11-18, when the archive's mirror delivered the fonts at about 50 kB/s without ever stalling; a bound of 10 minutes failed
two legs of the nightly there that had 90 minutes and more to spare. The bound is against a hang, not against a slow night.
The reachability check of the chart repositories
runs `curl` with `--connect-timeout 20 --max-time 60`.

#### When a gating job failed on the network

A gating job that failed or was cancelled at its limit skips Semantic Release. Read the job's log: a step that ends in
`::error::the download of … did not finish` or `::error::apt-get could not fetch …` names the packages and the hosts; before the
script, a hung index looked like `Get:` lines of `archive.ubuntu.com` followed by nothing until `The operation was canceled.`
Then re-run the failed jobs of that run (`gh run rerun <run id> --failed`, or "Re-run failed jobs" on the run's page): GitHub
runs the failed and cancelled jobs again, and Semantic Release after them once they pass. If a newer push to `main` has
started its own run, re-run nothing: that run releases every commit since the last tag. Jobs are never re-run automatically.

The runner image is the lasting fix: an image that carries the packages makes every such step end at the `dpkg-query` check.
The upstream runner image lacks `build-essential` (`make`, `gcc`), `file`, `libatomic1`, `docker-compose-v2`, and for the
malware scan `clamav` and `clamav-daemon` (the scan still downloads the ClamAV database with `freshclam`), and the browser
libraries of Playwright.
The self-hosted runners start such an image since 2026-11-19: `guidedtraffic/actions-runner`, defined in the repository
`guided-traffic/container-images` (directory `actions-runner`: the upstream runner image plus the packages above and
Chromium's libraries and fonts as Playwright 1.64 lists them), named in the runner's repository by the tag that does not move
with the weekly rebuild (`<runner version>-<build time>`). In the first job on it (`integration (shard a)`, re-run of Test and
Release 44491248224) `Install build tools` printed "make, gcc are on the runner; nothing to install" and took one second. A
package a job needs and the image lacks is still installed by the job through the script; add it to the image then.

The Playwright steps are ready for it. Each calls [playwright-install.sh](../../.github/scripts/playwright-install.sh), which
asks Playwright first (`npx playwright install-deps --dry-run chromium` simulates the install without the network and exits 0
only when no library is missing) and then fetches the browser alone; on a runner that lacks a library it installs with
`--with-deps`, as before. The list of libraries is Playwright's own, of the locked version. `TestEveryBrowserInstallGoesThroughTheScript`
of [internal/tools/ciworkflows](../../internal/tools/ciworkflows/playwrightinstall_test.go) fails on a workflow step that
runs `playwright install` itself. Run by hand on 2026-11-19 with Playwright 1.64.1 (arm64 containers): in the image with the
libraries the script fetched the browser in 18 s and read no package index; in the upstream image the dry run failed and the
install read 104 index files and installed libraries and browser in 30 s.

### What the jobs fetch and what is cached

A job should not depend on a host outside for something that does not change between runs (F-156, F-158 in
[promt.md](../projektplan/promt.md)). The inventory below lists, for both workflows, what a job fetches at run time that its pinned
inputs determine, from where, and what caches it; the development agent read it from the workflows, the Makefile and the scripts on
2026-11-19, and the hosts from the logs of Test and Release 44511234820 where a row says so. The databases that change by themselves
(ClamAV's signatures, Trivy's and govulncheck's vulnerability databases) are fetched on purpose and are not in it.

| What | Jobs | Host | Determined by | Before 2026-11-19 | Now |
|---|---|---|---|---|---|
| Go modules of `go.sum` | every job that builds Go: the static checks, unit, generate, chart, web, ui-layout, integration, s3-conformance, the coverage jobs, the fuzz legs, every kind leg, latency and vuln-perf (the jobs without it are named with their reason in `goModulesExempt` of [caches_test.go](../../internal/tools/ciworkflows/caches_test.go)) | proxy.golang.org (run 44511234820: 74 to 85 files per job, 13 to 34 s) | `go.sum` | setup-go's cache: a hit, but holding what the first job to finish needed (24 MB on the self-hosted runners) | [go-modules](../../.github/actions/go-modules/action.yml): the complete download cache, nothing fetched after a hit ([Q-ARCH-523](../spec/architecture.md#q-arch-523--the-go-module-cache-the-download-cache-keyed-by-gosum-completed-by-every-job-saved-by-the-first-that-missed)) |
| Go build cache | the same | – | the code | in setup-go's entry, partly | not cached (Q-ARCH-523) |
| golangci-lint, govulncheck, gocyclo | lint, gosec, govulncheck, cyclomatic-complexity (all gating) | proxy.golang.org, inside the make target | pin and Go version | `lint` only | [go-tool](../../.github/actions/go-tool/action.yml) ([Q-ARCH-525](../spec/architecture.md#q-arch-525--the-go-tools-built-with-go-install-come-from-the-cache-keyed-by-their-pin-and-the-go-version)) |
| kind, actionlint | versions, every kind leg, the platform matrix; workflows | proxy.golang.org | pin and Go version | none | go-tool |
| The Quader image (`make image`) | every kind leg (10 per push of Test and Release, 25 in a scheduled nightly) | Docker Hub (`node`, `golang`, `alpine`), the npm registry and proxy.golang.org inside the build, gcr.io (since 2026-11-21 no analyser: the image is 163 MB smaller) | the commit | none: built in every leg | built once per run by the job `image`, loaded from the artifact `quader-image` ([Q-ARCH-522](../spec/architecture.md#q-arch-522--the-kind-legs-load-the-quader-image-the-job-image-of-their-run-built-and-need-that-job)); `image` and `container-scan` (gating) still build it, each once |
| The clients images (npm, NuGet, OCI, APT, Ansible) | the legs whose sets start client pods ([deploy/dev/e2e.sh](../../deploy/dev/e2e.sh)) | the Ubuntu and Debian archives, the npm registry, PyPI, dist.nuget.org, mcr.microsoft.com, Docker Hub, quay.io (F-158: archive.ubuntu.com) | the build context, the pins, the forwarder | none: built in every leg, no layer cache | [client-images](../../.github/actions/client-images/action.yml) ([Q-ARCH-520](../spec/architecture.md#q-arch-520--the-client-images-of-the-acceptance-suites-come-from-the-github-actions-cache-as-the-tar-of-docker-save-keyed-by-their-inputs)) |
| npm packages of `web/` | web and ui-layout; the kind legs with browser journeys (access-long, ansible, bastion, vuln; nightly e2e, bastion, vuln, upgrade) | registry.npmjs.org | `web/package-lock.json` | setup-node's npm cache in web and ui-layout | the same cache in those legs too |
| npm packages of the release tooling | Release Tooling, Semantic Release | registry.npmjs.org | `package-lock.json` | none | setup-node's npm cache in Release Tooling; Semantic Release unchanged (it holds the release secrets) |
| The upgrade leg's second image (`make image-marked`) | nightly `upgrade` | as the Quader image | the commit | none | unchanged: one leg |
| Stub and driver images | every kind leg, inside `make e2e` | gcr.io (the distroless base, about 2 MB) | the commit | none | unchanged ([Q-ARCH-526](../spec/architecture.md#q-arch-526--what-stays-fetched-in-every-run-and-why)) |
| kind node image, the dev profile's images, k6, the integration tools image | every kind leg, the platform matrix | Docker Hub, quay.io, ghcr.io, registry.k8s.io (by digest) | the pins | none | unchanged (Q-ARCH-526) |
| The dev profile's charts | every kind leg | github.com release assets, charts.jetstack.io, guided-traffic.github.io | the pins | none in CI ([Q-ARCH-355](../spec/architecture.md#q-arch-355--how-do-the-ci-jobs-get-their-pinned-tools-without-a-live-release-asset-download-per-job)) | unchanged; bounded retries (`make dev-charts`, `chart_tgz`) |
| The integration stack's images, Silo | integration, s3-conformance, ui-layout | Docker Hub | the pins | none | unchanged |
| The SDK image of the NuGet vectors; syft, busybox and registry of the SBOM generator | nuget vectors; SBOM generator | mcr.microsoft.com; Docker Hub | the pins | none | unchanged |
| Go toolchain | every job with Go | github.com (`actions/go-versions` release asset) | the `go` line of `go.mod` | none (the pods are new) | unchanged; 5-minute step bound in gating jobs (F-153) |
| Node.js, Helm, kubectl | the jobs that use them | the actions' own hosts (Helm: get.helm.sh, kubectl: dl.k8s.io) | the pins | none | unchanged; 5-minute step bounds in gating jobs |
| Docker Buildx | every job that builds an image | the runner's copy, else github.com | the pin | the archive cached by pin and digest | unchanged ([Pinned tools in the jobs](#pinned-tools-in-the-jobs)) |
| promtool | chart | github.com release assets | the pin | none | unchanged ([tools/github-download.sh](../../tools/github-download.sh), bounded) |
| Trivy (`make trivy`) | container-scan; the `vuln` legs load the same image into kind | Docker Hub (`TRIVY_IMAGE`, by digest; 58.5 MB for arm64, 63.4 MB for amd64) | the pin | none | the binary out of the analyser's image since 2026-11-21 (R-HA-070), no GitHub download |
| Playwright's browser | ui-layout, the kind legs with browser journeys | Playwright's download host | the locked Playwright | none | unchanged (Q-ARCH-526) |
| OS packages | through [apt-install.sh](../../.github/scripts/apt-install.sh) | the Ubuntu archive | the runner image | the runner image carries them since 2026-11-19 | unchanged ([OS packages in the jobs](#os-packages-in-the-jobs)) |

#### The caches

All entries live in the GitHub Actions cache of the repository: 10 GB, entries not read for 7 days are deleted, and beyond 10 GB the
least recently used go first. A run on a pull request reads the entries of `main` and writes its own, which only that pull request
reads. The self-hosted runners have no `zstd`, so the cache compresses with gzip there and with zstd on the GitHub-hosted runners: the
same key is then two entries, one per runner kind, and each fills on its own first miss.

| Cache | Key | Path | Measured size | On a miss | On a hit, not contacted |
|---|---|---|---|---|---|
| Go modules | `go-modules-<SHA-256 of go.sum>` | `$GOMODCACHE/cache/download` (the zips and `go.mod` files, not the unpacked tree) | 216 MB on disk, 165 MB gzip, 161 MB zstd (114 modules, development machine) | `go mod download` fetches the whole graph (477 files, 21 to 31 s from the development machine), and the job saves at once; jobs that miss together all download, the first save wins | proxy.golang.org; 13 to 34 s of downloads per job in run 44511234820, against a restore of 165 MB (15 to 40 s at the 4 to 12 MB/s the self-hosted runners restored entries of that size in that run, 2 to 5 s on GitHub-hosted runners) plus unpacking every module (16 s on macOS, not measured on Linux) |
| Go tools | `go-tool-.bin/<name>-<pin>-<go version>-<os>-<arch>` | that file | golangci-lint 32 MB (the entry of the former `lint` cache); the others not measured | `go install` within 300 s, saved at once | proxy.golang.org for the tool's module graph |
| Clients images | `e2e-clients-<set>-linux-<arch>-<SHA-256 of the inputs>` ([Q-ARCH-521](../spec/architecture.md#q-arch-521--the-key-of-a-clients-image-the-build-context-the-pins-the-build-reads-the-forwarder-binary-the-script-and-the-architecture)) | `.out/client-images/<set>.tar`, deleted after the load | gzip / zstd: npm 139 / 125 MB, NuGet 803 / 737 MB, OCI 335 / 320 MB, APT 181 / 157 MB, Ansible 125 / 112 MB (linux/arm64 builds; amd64 not measured) | the build of `make <set>-clients-image` as before, `docker save`, saved at once | every host of the build: in run 44511234820 the builds took 21 s (npm) and 64 to 87 s (NuGet) on GitHub-hosted runners. A hit there restores 125 or 737 MB in seconds (416 MB took 2 to 5 s in that run) plus `docker load` (7 s and 30 s on the development machine, with the layers already present; a fresh runner not measured). On the self-hosted runners the NuGet tar takes about 70 to 200 s to restore at the speeds above |
| npm packages | setup-node's (`node-cache-<os>-<arch>-npm-<hash of the lockfile>`) | `~/.npm` | 101 MB for `web/` (run 44511234820) | `npm ci` from the registry; setup-node saves the cache in its post step | registry.npmjs.org for the tarballs; whether `npm ci` sends no request at all after a hit is not verified |

What a job does when the cache does not answer: `actions/cache/restore` gives up a stalled segment after 3 minutes for the modules and
the tools (`SEGMENT_DOWNLOAD_TIMEOUT_MINS`; the clients images keep the action's default of 10 minutes: a segment is 128 MiB, as the restores of run
44511234820 advanced, and takes about two minutes at the slowest speed seen there, 1 MB/s) and treats any failed restore as a miss, so the job builds or downloads as before. A clients
tar that does not load (a partial entry) is a warning: the script deletes it and builds. A restored tool binary whose build information
does not name the pin is built again.

Invalidate an entry by changing an input: `go.sum` for the modules, the pin or the Go version for a tool, any file of a set's directory,
one of its pins or the forwarder for a clients image. To drop one by hand: `gh cache list --key e2e-clients-nuget` and `gh cache delete
<key>` (all of its versions, on every ref). A change of [client-images.sh](../../.github/scripts/client-images.sh) rebuilds every set
once, because the script is one of the inputs.

The budget, with one entry per key and runner kind: the Go modules 0.33 GB, the clients images about 2.4 GB (on GitHub-hosted runners
npm and NuGet, 0.86 GB zstd; on the self-hosted runners all five, 1.58 GB gzip), the Go tools about 0.1 GB, the npm caches about 0.3 GB,
the buildx archives 0.04 GB, Trivy's daily database 0.13 GB a day for up to a week. About 4 GB in all; a change of every input at once
adds about 3 GB until the old entries age out. Before the change the repository held 4.4 GB, 2.5 GB of it setup-go entries, which are no
longer written and age out within a week.

### Coverage

The unit tier and each integration line write a profile to `.out/coverage`: `unit.out` and
`integration-<line>.out`, with `-covermode=atomic -coverpkg=./...`, so each counts for every package
it reaches. [`test/coverage/report.sh`](../../test/coverage/report.sh) merges them: a block counts once,
covered when any profile covered it. It writes `coverage.txt` (`go tool cover -func`), `summary.md`
for the job summary, and [`coverage.json`](../../.github/badges/coverage.json) (the README badge; green
from 80 %, brightgreen from 90 %, yellow from 70 %, orange from 60 %, red below). Semantic Release
committed the badge with the release until Q-ARCH-457; the badge is not updated by a release until the first stable release.

The requirement evidence of [docs/coverage](../coverage/README.md) is a different thing: the
`coverage evidence` job writes `implemented.txt`, `checklist.md` and the generated blocks of the coverage README from the
test reports of the run. Since Q-ARCH-457 no release commits them, so the committed evidence stands still until the first stable release. Nobody refreshes them by
hand; the job fails only when the committed record claims a requirement that no passing test of the run names
([coverage.md](coverage.md#how-ci-feeds-it)). A push that makes no release leaves the record as it is until the next release.

### Cyclomatic complexity

`CYCLO_THRESHOLD` in the [Makefile](../../Makefile) is a ratchet, as in cogpit: it is the complexity of
the most complex function of the module, so `make cyclo` passes today and a change that adds a function
above it fails. The threshold of valkey-operator is 15. Today 40 functions of the module are above 15
(`make cyclo-report` lists them; tests and the generated code are not counted). A change that
simplifies the most complex function lowers the threshold to the new maximum in the same commit.

### Malware scan

ClamAV scans the source tree with the product's ClamAV, `CLAMAV_IMAGE` of `tools/versions.env`
([R-MAL-031](../spec/malware-scanning.md#r-mal-031--the-source-trees-malware-scan-runs-the-products-clamav)): `make malware-scan-db` runs
`freshclam` of that image into `.out/clamav-db`, then `make malware-scan` runs its `clamscan` over the tree mounted read-only. The job keeps
`.out/clamav-db` in a cache whose key is the UTC day (`clamav-db-<day>`, the older days as restore keys), so a run fetches the day's
differences and ClamAV's network sees one whole download per eviction of the cache
([Q-MAL-86](../spec/malware-scanning.md#q-mal-86--the-source-trees-scan-keeps-the-real-database-in-a-cache-of-the-day)); the job logs in
to Docker Hub for the pull. The scan excludes `.git`, `node_modules` and the build outputs `.out`, `.bin`, `.dev` and `bin`. The report is
`.out/clamav-report.txt`; the job fails when it does not end with `Infected files: 0`, so a scan that did not finish is a failure too.
Measured on the development machine on 2026-11-20: 64 s over 5,598 files, 11 s for a first `freshclam` download.

This job checks Quader's own source and is not the malware scanning of stored artifacts
([malware-scanning.md](../spec/malware-scanning.md)).

### SBOM generator

The job `sbom-generator` runs `make sbom-generator-check` ([check.sh](../../tools/sbom-generator/check.sh)) on every push and pull request, and Semantic Release needs it. It builds the
generator of the release image from `SYFT_IMAGE`, builds a two-platform test image with it as the BuildKit SBOM generator, writes the image to a file (nothing is pushed) and
asserts that both SBOM attestations name `syft-${SYFT_VERSION#v}`. It uses a registry container and a builder of its own on the default bridge network and removes them at the end; its test image has a `RUN` step that resolves `proxy.golang.org`, so a builder whose `RUN` steps cannot resolve names fails here and not in a release. A syft bump
that breaks the generator therefore breaks a pull request or push run, never a release ([Q-HA-166](../spec/ha-and-helm.md#q-ha-166--the-sbom-attestation-of-the-image-comes-from-a-generator-that-runs-the-pinned-syft)).
Locally: `make sbom-generator-check` (Docker with buildx and `jq`).

### Lint and GoSec

Both run through the same pinned golangci-lint (`GOLANGCI_LINT_VERSION`), with the linter settings of
[.golangci.yml](../../.golangci.yml). An accepted gosec finding carries its reason in the code
(`//nolint:gosec // reason`) or in the configuration; the standalone gosec binary does not read those
annotations, so the repository runs gosec through golangci-lint and has one gosec version.

### Compatibility

**Switched off until the first stable release** ([Q-ARCH-340](../spec/architecture.md#q-arch-340--no-backward-compatibility-until-the-first-stable-release), `CLAUDE.md`): the job `compat` has `if: false` and is not
among the needs of Semantic Release, so neither a breaking change of `/api/v1` nor an edited migration fails CI. `make compat-check`
and [test/compat](../../test/compat) stay: lint and vet build them with the tag `compat`, Renovate keeps `OASDIFF_VERSION` fresh,
and the target runs by hand. The rest of this section describes the job as it runs once it is switched on
([the switches](#switched-off-until-the-first-stable-release)).

`make compat-check` runs the tests of [test/compat](../../test/compat) (build tag `compat`). They hold two promises of
the specification to the latest release:

| Requirement | What the check accepts | Implemented by |
|---|---|---|
| [R-ARCH-025](../spec/architecture.md#r-arch-025--versioning-and-compatibility-policy) | `api/openapi.yaml` has no error-level change against the baseline: `oasdiff breaking --fail-on ERR` | oasdiff, pinned as `OASDIFF_VERSION` in [tools/versions.env](../../tools/versions.env) and built into `.bin/` by the Makefile; the fixtures of [testdata/openapi](../../test/compat/testdata/openapi) show that a removed operation fails and an added one passes |
| [R-ARCH-026](../spec/architecture.md#r-arch-026--schema-migrations-and-the-compatibility-gate) | Since the baseline, every change of `internal/meta/postgres/migrations` is an addition (`git diff --name-status --no-renames`), and the new numbers continue the sequence | [migrations_test.go](../../test/compat/migrations_test.go): fixtures for an edited, a renamed and a removed file, and for the numbering |
| [R-ARCH-026](../spec/architecture.md#r-arch-026--schema-migrations-and-the-compatibility-gate), [Q-ARCH-283](../spec/architecture.md#q-arch-283--is-a-migration-on-main-immutable-and-what-does-the-check-hold-to) | On the history of `main`, a migration file that `main` has committed is never changed, removed or renamed after its first commit on `main`; a pull request is checked on its own branch too. Only the commits named in `historicalEdits` are exceptions | [migrations_history_test.go](../../test/compat/migrations_history_test.go): `migrationHistoryProblems` on a fixture repository (an edit, a removal, a rename, a branch edit, an allowlisted and an unlisted edit, a stale entry, a branch that predates a file) and on the repository itself (`TestR_ARCH_026_MigrationsOnMainAreNeverEdited`) |

The baseline is the newest `v0.*` tag whose commit is not HEAD's. On the release commit itself HEAD would be compared with
itself, so the check then compares with the release before it ([ADR-139](../decisions.md#adr-139--compatibility-is-checked-against-the-latest-release-in-ci)).
A clone without tags has no baseline, and the tests fail rather than pass. Locally run `git fetch --tags` first. The
`compat` job of [release.yml](../../.github/workflows/release.yml) checks out with `fetch-depth: 0` and `fetch-tags: true`,
and Semantic Release needs it, so a breaking change or a changed migration blocks the release.

The history check reads `main` (the local branch, or `origin/main` where the checkout has none) and the checked-out HEAD. A migration that `main` has committed may not change after its first commit on `main`, so a change in a pull request fails
on the pull request's own branch, and a push to `main` fails on `main`. The exceptions are the commits that `historicalEdits` in
[migrations_history_test.go](../../test/compat/migrations_history_test.go) names, two of them today, from before the rule. A
failure names the file, the commit and the rule: fix forward with a new migration ([database.md](database.md#how-to-fix-a-migration-that-is-on-main)).

There is no waiver. R-ARCH-025 promises additive changes only within `/api/v1`, so a breaking change there is a new API
version, not a flag in CI. The checker's own `--version` reports `main` for a module install, so the test reads the module
version with `go version -m` and compares it with the pin: a binary of another version fails the test.

Not covered by this job: the other surfaces of R-ARCH-025 (configuration keys, Helm values, metric names, error codes), which
nothing in CI checks, and its acceptance test of a `helm upgrade` from N-1 to N and a `helm rollback`, which is the set
`upgrade-release` of the nightly workflow, run on dispatch until the first stable release ([below](#ha-and-performance-nightly)).

### Switched off until the first stable release

Quader keeps no backward compatibility until its first stable release ([Q-ARCH-340](../spec/architecture.md#q-arch-340--no-backward-compatibility-until-the-first-stable-release), `CLAUDE.md`). The mechanisms that hold
compatibility are switched off, not removed, so that each comes back with the edit below. Their code and tests stay built,
linted and run: `test/compat` under the tag `compat`, the strict migration rules in `TestParseSteps`.

| Switch | Where | Off now | To switch on |
|---|---|---|---|
| The job `compat` (R-ARCH-025, R-ARCH-026) | [release.yml](../../.github/workflows/release.yml), job `compat` | `if: false` | Delete the line `if: false` and the comment above it that names Q-ARCH-340 |
| `compat` among the needs of the release | [release.yml](../../.github/workflows/release.yml), job `semantic-release` | `compat` is missing from `needs:`, because a skipped need would skip the release | Put `compat` back into `needs:` after `workflows`, and delete the comment above the job that names Q-ARCH-340 |
| actionlint's acceptance of `if: false` | [.github/actionlint.yaml](../../.github/actionlint.yaml) | the message `constant expression "false" in condition` is ignored in `release.yml` | Delete that ignore line and its comment paragraph |
| The expand/contract rules of the migration files (R-HA-018) | [steps.go](../../internal/meta/postgres/steps.go), `EnforceMigrationPhases` | `false`: every header optional, no statement check by phase, `RequiredSchema()` the highest step | Set it to `true`. Every embedded file must then carry one `-- quader:phase` header and no `DROP`, `RENAME` or `SET NOT NULL` in an expand step, or `TestEmbeddedSteps` fails and `RequiredSchema()` panics; `RequiredSchema()` becomes the newest expand step |
| The nightly upgrade from the published release (R-HA-020, [Q-HA-314](../spec/ha-and-helm.md#q-ha-314--the-nightly-run-runs-the-rolling-upgrade-of-the-commit-the-upgrade-from-the-published-release-stays-on-dispatch)) | [performance.yml](../../.github/workflows/performance.yml), job `e2e-upgrade`, the matrix entry `upgrade-release` | `nightly: "false"`: runs only on a dispatch with `-f sets=upgrade-release` or `-f sets=e2e-upgrade` | Set its `nightly` to `"true"`, and delete the sentences that name Q-ARCH-340 in the job's comment and in the header of the file |
| The skip of a commit that needs a new database (R-HA-020, [Q-HA-312](../spec/ha-and-helm.md#q-ha-312--a-commit-that-cannot-be-upgraded-to-without-a-new-database-skips-the-upgrade-from-the-release-until-the-first-stable-release)) | [upgrade_env_test.go](../../test/e2e/upgrade_env_test.go), `requireUpgradable` | skips while `EnforceMigrationPhases` is `false` | Nothing of its own: it fails once the switch of the migration rules above is on |

Before the job `compat` is switched on, three things need a decision, because the checks read what happened while they were off:

- The history check reads all of `main`: every migration edited while it was off fails it. Each such commit is named in
  `historicalEdits` of [migrations_history_test.go](../../test/compat/migrations_history_test.go), or the check is anchored at
  the first stable release's tag.
- The baseline of both release checks is the newest `v0.*` tag whose commit is not HEAD's (`releaseTagPattern` in
  [helpers_test.go](../../test/compat/helpers_test.go)). Switched on right after the first stable release, that release is the
  baseline; if it leaves major version 0, the pattern names the new major.
- The checksum message of `verifyApplied` in [migrate.go](../../internal/meta/postgres/migrate.go) names the remedy before the
  first stable release, a new database; with the checks on, the remedy is a new step that fixes forward.

The notes that say "before the first stable release" in R-ARCH-025, R-ARCH-026, R-HA-018 to R-HA-021,
[database.md](database.md#migrations), [upgrades.md](../manual/admin/upgrades.md#before-the-first-stable-release) and
[limitations.md](../manual/admin/limitations.md) change with the switches, in the same change.

### Running a job locally

Run from the repository root. Each line is the command the job runs.

```sh
make versions-check                     # the tools on PATH against the pins
make lint gosec vuln cyclo generate-check build
make test                               # unit tests, race detector, .out/coverage/unit.out
make web-ci web-lint web-test web-embed # Node.js from web/.nvmrc
make malware-scan                       # needs docker: freshclam and clamscan of CLAMAV_IMAGE
make chart-lint chart-test              # both charts; chart-test fetches promtool at its pin into .bin
make image IMAGE=quader:ci               # the Containerfile, host architecture
make integration-up test-integration integration-down
make silo-up s3-conformance silo-down # needs Docker
make coverage-report                    # merges .out/coverage
make test-release-tooling               # npm ci, npm audit signatures, the release notes check
go run ./internal/tools/coverage -check -links
git fetch --tags && make compat-check   # R-ARCH-025 and R-ARCH-026 against the latest v0.* release
make image VERSION=dev && make dev-up && make e2e SET=push && make dev-down
make image VERSION=dev && make dev-up && make e2e SET=npm && make dev-down   # QUADER_DEV_HTTP_PORT=8080 is the port of the committed reference lockfile; any port works
make fuzz-npm FUZZ=FuzzMatch FUZZTIME=1m
make nuget-vectors-check                # needs Docker; make nuget-vectors writes the file
make nuget-reference-check              # perf/nuget/reference against a regeneration (no Docker)
make fuzz-nuget NUGET_FUZZTIME=1m       # the eight NuGet fuzz targets, one minute each (CI: 30 minutes each, closing run only)
```

### The run that closes a phase

The roadmap reserves a few checks for "the full run of Test and Release on the commit that closes the phase"
(M2: the npm scenario list and the cold-cache install with Valkey stopped, the read-only subset against the public registry,
the 30-minute fuzz run of npm's `Match` and `Inspect`). Nothing in a push or a pull request marks that commit, so a person starts
the run ([Q-NPM-46](../spec/formats/npm.md#q-npm-46--what-marks-the-run-that-closes-the-phase)):
*Actions, Test and Release, Run workflow*, the closing commit as the ref, `closing` ticked; or
`gh workflow run release.yml --ref main -f closing=true`. Every job of the workflow runs, except that the `e2e` job no longer has the `failover` set (owner, 2026-11-01): the closing run does not start [HA and Performance (nightly)](#ha-and-performance-nightly), so a closing commit whose criteria need the failover set needs that run too. Plus the legs `e2e-npm (npm-closing)` and
the three `fuzz npm` legs, the leg `e2e-nuget (nuget-closing)` and the eight `fuzz nuget` legs of 30 minutes each ([Q-ARCH-278](../spec/architecture.md#q-arch-278--how-is-the-nuget-fuzz-run-of-the-closing-run-laid-out)), and the legs `e2e-npm (npm-ha)` and `e2e-npm (npm-ha-crash)` run as one leg `npm-ha-closing`, with the 20-minute pod crash, the R-ARCH-027 report and the Valkey outage under npm load. A dispatch does not release (Semantic Release runs on `push`), and a failed closing leg means the phase
is not closed. Not in this workflow yet: the run of the NuGet criteria against nuget.org that R-NUGET-027 names for the closing
run (the remote and symbol criteria); the closing run covers the NuGet legs above and nothing against the public registry for NuGet.

`make generate-check` runs `git diff --exit-code`, so it fails on a tree with uncommitted changes for
reasons unrelated to generation. `make dev-up` needs an amd64 host or the emulator of
the arm64 note of [local-profile.md](local-profile.md).

### HA and Performance (nightly)

The long tests run here and not on every change (owner, 2026-11-01), so a slow leg no longer holds the release. Per change,
Test and Release runs `push`, `access-long` and `ci`, the set of each format whose own paths the push changed and the reference set `npm`
for a change of shared code ([which e2e legs a change runs](#which-e2e-legs-a-change-runs)). This workflow runs every format's set each
night (owner, 2026-11-20; [Q-ARCH-545](../spec/architecture.md#q-arch-545--the-formats-sets-run-in-lane-3-of-the-nightly-and-nuget-outside-the-lanes)):
jobs `format-oci`, `format-deb`, `format-ansible` and `format-npm` (the sets `oci`, `deb`, `ansible` and `npm`, edge ports 8109, 8119, 8120 and
8098, on `large`, in lane 3) and `format-nuget` (the set `nuget`, edge port 8099, on a GitHub-hosted runner outside the lanes), each the leg
of Test and Release with select-leg in front and the same artifact names (`e2e-<format>-report-<format>`), so that the coverage evidence of a
push that did not start a format's leg carries the newest report of that set from either workflow. A dispatch names them by set
(`-f sets=npm,oci`) or by job key (`-f sets=format-npm`). It also runs the
`failover` set (job `e2e`, E0.13 and E1.8), the drain per role (job `e2e`, set `drain-roles`, E6a.6, edge port 8097: the evictions run
under the npm load of `npm-ha` too, so the set starts the npm client pods and the counting test upstream,
[Q-HA-94](../spec/ha-and-helm.md#q-ha-94--the-load-of-the-drain-per-role)), the `npm-ha` leg (job `e2e-npm`, E2.11), `npm-ha-soak` and
the `ha-suite`, `ha-faults` and `ha-pooler` legs (job `e2e-npm`, the failure suite under the npm load and its additions of E5.10), the `upgrade` leg (job `e2e-upgrade`, below; its second leg `upgrade-release` is switched off in the nightly run until the first stable release, [the switches](#switched-off-until-the-first-stable-release)), the `nuget-ha` leg (job `e2e-nuget`,
Q-ARCH-277, which starts the npm client pods too), and last the perf jobs `latency` and `vuln-perf`, which start after every kind leg
([Q-ARCH-325](../spec/architecture.md#q-arch-325--where-do-the-perf-jobs-run-relative-to-the-kind-legs)). Dispatch it with `gh workflow run "HA and Performance (nightly)" --ref main`, or one leg of it with `-f sets=` ([below](#dispatching-one-leg)).
Locally, from the repository root, after `make image VERSION=dev` and `make dev-up`: `make e2e SET=failover`, `make e2e SET=drain-roles`, `make e2e SET=npm-ha`,
`make e2e SET=npm-ha-crash` (the three-force-delete crash, which has no CI job), `make e2e SET=npm-ha-soak`, `make e2e SET=nuget-ha`, `make e2e SET=ha-suite`, `make e2e SET=ha-faults`, `make e2e SET=ha-pooler`, `make e2e SET=upgrade` or `make e2e SET=upgrade-release`,
then `make dev-down`; the latency tests need `make integration-up && make test-performance`.
A red long leg does not hold the release: it is recorded as an open finding instead (owner, 2026-11-01).

#### Dispatching a branch

`gh workflow run "HA and Performance (nightly)" --ref <branch> -f sets=<set>` runs the workflow file of the branch, and the run is in
the concurrency group of its ref (`${{ github.workflow }}-${{ github.ref }}`, [Q-ARCH-433](../spec/architecture.md#q-arch-433--is-the-concurrency-of-the-nightly-run-per-workflow-or-per-ref)),
so it runs beside main's run instead of replacing main's pending run. Each run bounds its own large legs to three ([Q-ARCH-431](../spec/architecture.md#q-arch-431--how-many-large-legs-of-the-nightly-run-start-at-once-and-in-what-order)):
a branch dispatch adds its own large legs to those of a run on main, and the development agent coordinates the dispatches it starts. Not verified on GitHub yet
that a branch run starts beside a run on main.

#### Dispatching one leg

The input `sets` of a dispatch is a comma-separated list of set names or job keys, without spaces: `gh workflow run "HA and Performance (nightly)" --ref main -f sets=upgrade-release,ha-suite`
runs the leg `upgrade-release` and the leg `ha-suite` and no other. A job key selects every leg of its job (`-f sets=e2e-npm` runs `npm-ha` and `ha-suite`), and the perf jobs are
selected by their keys `latency` and `vuln-perf`, the fuzz job by `fuzz-stored-format`. An empty list runs everything, as the schedule does, except the leg `upgrade-release`, which runs only when the
list names it or its job `e2e-upgrade` until the first stable release (the input `nightly` of select-leg, [the switches](#switched-off-until-the-first-stable-release)); the perf jobs, which need the job, run all the same,
because their condition starts with `!cancelled()`. A matrix leg that the list does not name shows as green with its
steps skipped, and its job summary says so; a job without a matrix that the list does not name is skipped. A list with a space fails every matrix leg with an error.
The composite action [select-leg](../../.github/actions/select-leg/action.yml) decides for every leg, in its script
[select-leg.sh](../../.github/actions/select-leg/select-leg.sh) ([Q-ARCH-324](../spec/architecture.md#q-arch-324--how-is-one-leg-of-the-nightly-run-dispatched));
the jobs without a matrix (`npm-ha-soak`, `latency`, `vuln-perf`, `fuzz-stored-format`) call it too, for the check [below](#a-scheduled-run-after-a-full-run).
The input exists on `main` once the change is merged, because `gh` runs the workflow file of the ref.
The run name says what a run is: `HA and Performance (all legs)` for the schedule and a dispatch without `sets`,
`HA and Performance (sets: upgrade,vuln-long)` for a dispatch of a part. It is what the list of runs shows (`gh run list --workflow "HA and Performance (nightly)"`).

#### A scheduled run after a full run

A scheduled run skips its legs when a dispatch of all legs (run name `HA and Performance (all legs)`) on the same branch ended
less than **12 hours** before with `success`, `failure` or `timed_out`. Red legs do not matter: a full run that ended red is still
the night's run, and repeating it at 10:00 fixes nothing. A cancelled dispatch, one that did not start, one still in progress and a
dispatch of a part do not count, and neither does a scheduled run: one that deferred carries the same run name, and scheduled
runs are a day apart anyway.

Why: on each of the twelve days up to 2026-11-18 GitHub created the scheduled run (`17 2 * * *`, UTC) between 08:44 and 10:41 UTC,
6.5 to 8.5 hours late (`gh run list --workflow "HA and Performance (nightly)" --event schedule --limit 12 --json createdAt`). On the
nights the development agent works it dispatches the full run by hand on a quiet `main` (on 2026-11-18: run 44414813006, 22:46 to
03:20 UTC), and the late scheduled run then repeated it in the middle of the owner's working day, beside the Test and Release runs of
the pushes to `main`; the owner cancelled it on 2026-11-18 for that reason. Why GitHub delivers the event that late is not known.
The cron line stays where it is: a moved one might fire on time and land in the evening. **Revisit** when GitHub delivers the
schedule on time again, or when the nightly gets a trigger of its own.

How: the step `Select the leg` of every job ([select-leg.sh](../../.github/actions/select-leg/select-leg.sh)) makes one call of the
Actions API on a scheduled run, for a leg that would run: the completed dispatches of this workflow on the branch
(`GET /repos/<repo>/actions/workflows/performance.yml/runs?branch=<branch>&event=workflow_dispatch&status=completed&per_page=100`,
with the job's token, so the workflow grants `actions: read`; the answer is read with `python3`, which the runner images carry).
A dispatch never calls the API and decides by its `sets` as before. A skipped leg is green with its steps skipped, and its job
summary and log say `Leg <set> of job <job> is skipped: this scheduled run defers to run #<number> (<url>), which ran every leg of
this workflow on main and ended at <time>, <hours> hours ago.` The check fails open: a call that fails or an answer that cannot be
read runs the leg, with a warning that says why, because a night without its run is worse than a run twice. Each leg checks for
itself. A skipped scheduled run still starts a runner pod per job for the checkout and the decision; it is expected to end
within minutes, which no run has shown yet.
[selectleg_test.go](../../internal/tools/ciworkflows/selectleg_test.go) runs the script with `curl` stubbed through these cases:
a dispatch (no call), a full run that ended 3 hours ago (skip), one that ended red (skip), one that ended 13 hours ago, only a
partial run, a cancelled full run, one still in progress, one that did not start, a scheduled run of all legs, no run (each: run),
a failed call, an answer that is no JSON, one without `workflow_runs`, a run without its fields and an unreadable time (each: run,
with a warning).

#### Order and wall time

The large legs run in three lanes, so at most three of them are active at once, one per worker node of the class
([Q-ARCH-431](../spec/architecture.md#q-arch-431--how-many-large-legs-of-the-nightly-run-start-at-once-and-in-what-order)):
lane 1 is `npm-ha`, `ha-suite`, `ha-faults` then `ha-pooler` (job `e2e-npm`, one leg at a time, [Q-HA-250](../spec/ha-and-helm.md#q-ha-250--where-the-scenarios-of-e510-run-and-in-which-lane)) then `nuget-ha`, `drain-load-b`, `-c`, `-e` and `cold-crash` (job `e2e-nuget`, one leg at a time); lane 2 is `drain-roles` then
`failover` (job `e2e`) then `npm-ha-soak`, then `e2e-edge`, then `drain-load-pooler` (job `e2e-drain-pooler`); lane 3 is the job `e2e-vuln` (its `vuln-pooler` leg; its GitHub-hosted legs run beside the lanes), then
`backup`, then `oci-long`, then the formats' sets `oci`, `deb`, `ansible` and `npm` (jobs `format-oci` to `format-npm`, Q-ARCH-545). From the job
times of the nightly 44625277245 and of Test and Release 44693590111 the lanes hold about 244, 133 and 52 minutes of legs before the formats'
sets and 244, 133 and 121 after them: the 68 minutes the four sets add (oci 16, deb 12, ansible 16, npm 24) end lane 3 before lane 1, so the
night is not longer; with `format-nuget` (22, GitHub-hosted) the night costs about 91 runner minutes more. About 230 minutes of lanes 2 and 3
stay free below lane 1: the sets of about 13 more formats of the mean size (18 minutes) fit before a night grows. Not measured yet: a night
with the formats' sets. Each lane job waits for its predecessor with `!cancelled()`,
so an unselected or failed leg does not stop its lane. The estimate, from the durations of run 42571414393 (measured under six-way contention): the lanes took
about 87, 82 and 86 minutes, against 58 minutes for the kind legs before the change; `drain-load-b`, `-c` and `-e` (variants B, C and E of R-HA-059 under the full load, about 35 minutes each) and `drain-load-pooler` (variant D, about 25) add to lanes 1 and 2 ([Q-HA-242](../spec/ha-and-helm.md#q-ha-242--the-variants-under-the-full-load-run-in-the-nightly-in-five-legs); both estimated, not measured). Not measured yet: the wall time in this order.
The perf jobs start after all of the kind legs have finished: `latency` (P-13 and P-14) after every kind job (`e2e-oci` and the five
`format-*` jobs among them since 2026-11-20; `e2e-oci` was missing from the list before, so `latency` could start beside `oci-long`), and `vuln-perf` after
`latency` ([Q-ARCH-325](../spec/architecture.md#q-arch-325--where-do-the-perf-jobs-run-relative-to-the-kind-legs)). So neither perf job shares the large VM with a kind cluster of this
workflow or with the other perf job (a Test and Release run that starts meanwhile can still share it). The cost is wall time: in run 42126196109 the two jobs took 5 min 17 s and 7 min 22 s, so a nightly ends about 13 minutes after its longest kind
leg. Not measured yet: the wall time of a whole nightly in this order, and the V-4 and V-7b results under it.

The longest kind leg is `e2e-bastion (bastion-long)`: the Bastion's drain, its floods and its failure suite (a replica lost on either backend, the main installation failing over, a rolling upgrade of either side; [Q-BAS-142](../spec/bastion.md#q-bas-142--the-failure-suite-runs-one-load-with-a-miss-loop-and-one-fault-after-the-other)) take about 80 minutes of tests, so its `make e2e` step has 120 minutes and `QUADER_E2E_TIMEOUT=110m`, and the job 180 minutes; the other legs keep 80 and 90.

[`performance.yml`](../../.github/workflows/performance.yml) ("HA and Performance (nightly)") runs every night at 02:17 UTC and on demand
(`workflow_dispatch`, with the input `sets`, [above](#dispatching-one-leg)), on the large runners and, for the legs that load the NuGet clients image, on GitHub-hosted runners ([CI](#ci)). It brings up the integration stack (`make integration-up`), runs
`make test-performance` and stops the stack. That target runs `TestChangePropagation`, `TestJobPickup`, `TestR_NPM_010_ThousandVersions` and
`TestIdempotentCreate` and `TestR_DEB_010_DelayBound` (the reference load of 10,000 package files and ten uploads per second, with and without Valkey, p99 of the delay of a build, Q-DEB-59) with `QUADER_PERF_GATE=1`, which enforces the p99 bounds of P-13 (100 ms) and P-14 (1 s) with Valkey available, R-NPM-010's
200 ms for a miss, 10 ms for a cached hit and 50 ms for a `304` of a document of 1,000 versions ([Q-ARCH-293](../spec/architecture.md#q-arch-293--where-a-wall-clock-target-of-a-functional-test-is-enforced)) and R-API-014's 1.5 s for a concurrent duplicate; it then runs `TestR_BAS_026_AFeedPageOfAThousandEntriesIsAppliedAgainstTwoHundredThousandAnswersWithinTheBound` of `internal/bastion/engine` with the gate (the 5 s of a burst page, R-BAS-026; per change the page is bounded by 100 matching passes over the same answers, [Q-BAS-140](../spec/bastion.md#q-bas-140--where-the-5-s-of-a-burst-page-is-enforced)) and `TestR_EP_013_CeilingPerformance` of `internal/authz` the same way
(the 1 ms p99 of one decision with 1,000 exposure rules, in the CPU time of the deciding thread, [Q-ARCH-250](../spec/architecture.md#q-arch-250--a-p99-bound-in-microseconds-is-a-nightly-gate-and-the-per-change-test-asserts-allocations-and-the-median)); the per-change integration job asserts delivery, the p50 and looser bounds only
([Q-ARCH-220](../spec/architecture.md#q-arch-220--p-13-starts-its-clock-at-the-commit-and-its-p99-is-enforced-in-the-nightly-performance-run)).
The report `test-performance` is kept for 30 days. Beside it, the job `npm-ha-soak` loads the image of the job `image`, runs `make dev-up` and
`make e2e SET=npm-ha-soak` on a kind cluster: R-HA-052's pod crash with ten force-deletes over 20 minutes under the npm load, then the npm
load scripts of R-ARCH-027 against one replica (`TestNPMPerf`, reported, not gated); the three-force-delete crash runs by hand only, and the closing run runs the ten force-deletes
in its leg `npm-ha-closing` ([Q-HA-67](../spec/ha-and-helm.md#q-ha-67--where-and-how-often-the-scenarios-under-npm-load-run)). Its reports
`e2e-npm-report-npm-ha-soak` and `e2e-npm-artifacts-npm-ha-soak` are kept for 30 days. A failed night is not wired to an issue or a
notification, except the job `vuln-perf` (below): look at the Actions list. The other targets of R-ARCH-027 (`perf/`) are not part of this workflow.

Job `vuln-perf` runs, after `latency` and every kind leg ([Q-ARCH-325](../spec/architecture.md#q-arch-325--where-do-the-perf-jobs-run-relative-to-the-kind-legs)), the performance run of the vulnerability scanning (R-VULN-028, scenarios V-1 to V-9, [perf/vuln](../../perf/vuln)) against the
integration stack with `QUADER_PERF_GATE=1`: it seeds 100,000 stored versions in SQL, imports the 231,000-record npm dump of the test advisory source,
evaluates, and measures; it uploads `perf-vuln.json`, the `go test -json` stream and, when the run failed, the replicas' whole logs
(`perf-vuln-logs/`) as `vuln-perf` (30 days) and, when a scenario is above its target or the run fails, opens an issue that lists the misses
(the job has `issues: write`, no other job has). A scenario that did not finish is listed as `not finished`, and a scenario that needs it
as `not measured` (V-2 and V-9 need V-1, V-3 to V-8 need V-1 and V-2; [perf/README.md](../../perf/README.md#reading-a-run)). It is the only job of the workflow that
opens an issue ([Q-VULN-57](../spec/vulnerability-scanning.md#q-vuln-57--the-performance-run-is-a-test-package-on-the-integration-stack-gated-and-reported-by-the-nightly-workflow)).

Job `e2e-nuget` also runs the leg `cold-crash` (`make e2e SET=cold-crash`, edge port 8110, cluster `quader-cold-crash-<run id>`, after `nuget-ha`, `max-parallel: 1`): `TestColdCrash`, a packages replica force-deleted during a cold `npm ci` and a cold `dotnet restore` of three clients each, then `quader admin storage verify` (E5.2, [Q-HA-220](../spec/ha-and-helm.md#q-ha-220--the-cold-install-crash-of-exit-criterion-e52-kills-one-packages-replica-once-the-upstream-has-served-a-share-of-the-packages-and-the-clients-go-through-the-edge)); it needs the same two clients images and runs under the same budgets.

Job `e2e-nuget` runs `make e2e SET=nuget-ha` on a kind cluster (edge port 8090, cluster `quader-nuget-ha-<run id>`, reports
`e2e-nuget-report-nuget-ha` and `e2e-nuget-artifacts-nuget-ha`, kept 14 days): `TestNuGetHA`, drain variant A, the PostgreSQL switchover
and the Valkey failover under the npm and NuGet load (R-HA-059, R-ARCH-024). It runs on the large class, because the load needs about 16 cores
on the dev cluster ([Q-HA-102](../spec/ha-and-helm.md#q-ha-102--the-cpu-of-the-nuget-drain-and-where-its-leg-runs)); the set loads the
clients images into the control-plane node alone, so the 2.3 GB NuGet image no longer fills the Docker storage of a large pod four times over.
The job takes both clients images from [client-images](../../.github/actions/client-images/action.yml) in its own steps (the cache, or a build when their inputs changed), and the set loads them, because the set starts the npm
client pods too ([Q-ARCH-327](../spec/architecture.md#q-arch-327--how-do-the-legs-that-load-both-client-images-get-them-and-who-builds-them)). The nightly run therefore
costs a cluster bring-up and proves no NuGet behavior under load until the drain and failover tests land.

Job `e2e-upgrade` has two legs, cut by what they test ([Q-HA-314](../spec/ha-and-helm.md#q-ha-314--the-nightly-run-runs-the-rolling-upgrade-of-the-commit-the-upgrade-from-the-published-release-stays-on-dispatch)),
each running `make e2e SET=<set>` on a kind cluster of its own (cluster `quader-<set>-<run id>`, reports and artifacts `e2e-upgrade-report-<set>`,
`e2e-upgrade-artifacts-<set>` and `e2e-upgrade-playwright-<set>`, kept 14 days; on failure `e2e-upgrade-cluster-state-<set>`) under the npm and NuGet client loops
and a browser journey ([the upgrade tests](e2e.md#the-upgrade-tests)):

- `upgrade` (edge port 8101, nightly): the commit with its second UI bundle (`make image-marked`) rolled to the image built from the commit. It needs no published artifact.
- `upgrade-release` (edge port 8107): the newest release before the commit that <https://guided-traffic.github.io/quader/> lists, with its image from Docker Hub,
  upgraded to the image built from the commit and rolled back. It runs only on a dispatch that names it or the job until the first stable release
  ([Q-ARCH-340](../spec/architecture.md#q-arch-340--no-backward-compatibility-until-the-first-stable-release)); a commit that changed a migration the release applied,
  or added a contract step, skips the test with the files named (Q-HA-312).

The legs run on GitHub-hosted runners with the disk space freed, because they load both clients images; their budgets are 120 minutes for the job and 90 for the set.
On 2026-11-06 the job took 30 to 48 minutes, most of it the bring-up.

Set `ha-suite` (job `e2e-npm`, edge port 8100, the budgets of `npm-ha`: 120 minutes for the job, 80 for the set) runs the failure suite under the npm load: the S3 outage,
the loss of a kind worker node with `docker stop`, the loss of the PostgreSQL and of the Valkey primary, and the npm-audit drain. Not verified on this commit: the set is not
in [deploy/dev/e2e.sh](../../deploy/dev/e2e.sh) yet, so its scenarios are the ones the plan names, not the ones the code runs.

Sets `ha-faults` (edge port 8112) and `ha-pooler` (edge port 8111, the pooler profile through `QUADER_POOLER=1`) are legs of the same job with the same budgets: the database at 0 instances for 2 minutes, the Sentinel quorum loss, `FLUSHALL` and a slow Valkey and a Valkey credential mismatch; and a pooler instance deleted, gracefully and forced, and a PostgreSQL switchover behind the poolers. [The failure suite ha-faults](e2e.md#the-failure-suite-ha-faults) describes them.

Job `e2e-ci` runs `make e2e SET=ci-ha` on a kind cluster

Job `e2e-ci` runs `make e2e SET=ci-ha` on a kind cluster (edge port 8092, cluster `quader-ci-ha-<run id>`, reports `e2e-ci-report-ci-ha` and
`e2e-ci-artifacts-ci-ha`, kept 14 days): E4.7, the reference pipeline while the node of a `web` replica is drained and again with Valkey
stopped (every replica ready, `"degraded":["valkey"]`), single-use exchanges across a forced Sentinel failover and `FLUSHALL`, and a `web`
pod deleted gracefully under 50 exchanging jobs ([e2e.md](e2e.md#the-ci-tests)). By hand: `make dev-up && make e2e SET=ci-ha`.

Job `e2e-vuln` runs the sets `vuln`, `vuln-long` and `vuln-pooler` (edge ports 8095, 8096 and 8103, clusters `quader-vuln-<run id>`, `quader-vuln-long-<run id>` and `quader-vuln-pooler-<run id>`, reports and
artifacts `e2e-vuln-report-<set>`, `e2e-vuln-artifacts-<set>`, `e2e-vuln-playwright-<set>`, kept 14 days): the short set as in Test and Release, and the long
set, R-VULN-029's drain and E4b.8: a full synchronization of 100,000 npm records drained mid-run (`kubectl drain` of the node that runs the job: it is `queued`
again with its attempt unchanged and another `worker` completes it from its checkpoint), and the client tests once more with Valkey stopped, and E5.13's scanning under failures: a worker killed in the middle of a full synchronization, a PostgreSQL switchover during an evaluation, four blocked versions probed through a node drain (variant A) and the Valkey and PostgreSQL failovers and through drain variants B, C and E of R-HA-059, and the restore drill of R-VULN-026 (a backup of an installation with blocked, dismissed and malicious findings restored into the namespace `quader-restore`, which reads the test advisory source and runs two `packages` replicas, judged before and after its first synchronization). `vuln` and `vuln-long` run on a GitHub-hosted runner (the NuGet clients image, Q-ARCH-326), `vuln-pooler` on a `large` one: it starts no client pods and installs no Node.js. `vuln-pooler` is the block of the same four versions through drain variant D, on the cluster with the poolers of R-HA-058 (`QUADER_POOLER=1`, which `e2e.sh` exports for the set). A red long leg is
a finding and holds nothing ([e2e.md](e2e.md#the-vulnerability-tests)). By hand: `make dev-up && make e2e SET=vuln-long` (and `SET=vuln-pooler`; the cluster stays in the pooler overlay until the next `make dev-up`).

Job `e2e-backup` runs the set `backup` (edge port 8102, cluster `quader-backup-<run id>`, reports and artifacts `e2e-backup-report` (`.out/test-e2e.json`, the set `backup`), `e2e-backup-pitr-report` (`.out/test-e2e-backup-pitr.json`, the set `backup-pitr` that follows it as a step of the job, [the point-in-time drill](e2e.md#the-point-in-time-restore-drill)) and `e2e-backup-artifacts`, kept 14 days, on a `large` runner):
R-BAK-029 and the drill of R-HA-052 on a kind cluster in the profile `backup`: a backup taken while a load publishes, deletes, trashes and collects with a grace period of 0s, restored with the restore Job
into the namespace `quader-restore` (a database, a Valkey and a bucket of their own), two overlapping Jobs, a node drain and a `kill -9` of a backup and of a restore, and the revocation journal across a restore, with E5.12's CI bullets (the leg starts the issuer double, `QUADER_E2E_OIDCSTUB=1`: a disabled CI mapping and a single-use subject token across the restore)
(E5.1, E5.11, E5.12). It takes about 25 minutes and is a long leg: nightly and on dispatch, not per change (owner, 2026-11-01; Q-HA-154). The artifact `e2e-backup-artifacts` holds `backup-timings.json`, the timings of
scenario 9. By hand: `make dev-up && make e2e SET=backup` ([e2e.md](e2e.md#the-backup-tests)).

Job `e2e-edge` runs the set `edge` (edge port 8104, cluster `quader-edge-<run id>`, artifacts `e2e-edge-report` and `e2e-edge-artifacts`, kept 14 days, on a `large` runner
because the 6 GiB file, the spool and the object store need its disk; the end of lane 2 of Q-ARCH-431, after `npm-ha-soak`): R-HA-039's 6 GiB upload, 17-minute download and 1 GiB NuGet push, and R-HA-038's
criteria, through the chart's Ingress and through its HTTPRoute on Envoy Gateway (E5.5). It takes about 90 minutes and is a long leg. By hand: `make dev-up && make e2e SET=edge`
([e2e.md](e2e.md#the-edge-and-alert-tests)). Job `e2e-alerts` runs the set `alerts` (edge port 8105, cluster `quader-alerts-<run id>`, artifacts `e2e-alerts-report` and `e2e-alerts-artifacts`, on a GitHub-hosted
runner with the disk space freed, outside the lanes): the alerts of R-HA-045 in their R-HA-052 scenarios against a Prometheus, and "Maintenance stays quiet" (E5.5). By hand: `make dev-up && make e2e SET=alerts`.
A dispatch names them by set or by job key (`-f sets=edge,alerts`).

Job `fuzz-stored-format` ("fuzz the stored format's readers") runs `make fuzz-stored-format STORED_FORMAT_FUZZTIME=5m`: the five fuzz targets
of the readers of the stored format of an encrypted connection, five minutes each, one after the other, on a self-hosted runner beside the
lanes, since it starts no cluster ([storage.md](../spec/storage.md) R-STOR-091, Q-STOR-319). It is the first fuzz job of the nightly: the fuzz
legs of npm and NuGet run in the closing run of Test and Release only, and the closing run does not start this one
([Q-STOR-323](../spec/storage.md#q-stor-323--the-closing-run-does-not-start-the-fuzz-job-of-the-stored-format)). About 27 runner minutes a
night (estimated); a crash fails the job and uploads its input as `fuzz-stored-format` (14 days). Its seeds, the vector files, run in every unit
job ([stored-content-encryption.md](stored-content-encryption.md#tests-and-their-cost)). By hand: `make fuzz-stored-format STORED_FORMAT_FUZZTIME=1m`.

The remote full mirror has no job of its own ([Q-REPO-306](../spec/repositories-and-proxy.md#q-repo-306--the-mirror-tests-are-part-of-the-formats-and-of-the-nightly-ha-sets-the-worker-pool-is-added-by-the-tests)). Its nightly part is in the legs of the format and of the failure suite: `npm-ha-soak` enforces the install-time ratio of R-REPO-055 (`QUADER_PERF_GATE=1`, on `large` since
Q-REPO-211: on the 4 vCPUs of a GitHub-hosted runner the pass's own work took the installs' CPU), `npm-ha` runs R-REPO-058's drain (`kubectl drain` of the node of the process of the
pool `mirror` that runs a pass of 300 packages: the job is `queued` again with its attempt unchanged, the other process of the pool ends the pass, and the test upstream served every
tarball, `concurrency` of them twice at most), the status from every replica, two claimants and a pass with Valkey stopped, followed by the failure suite of R-HA-052 on passes of 100 packages (the process of the pass force-deleted, its node stopped and frozen, the upstream throttling across a drain and a kill, a PostgreSQL switchover and a Valkey failover; about 40 minutes more), `nuget-ha` the status of a NuGet mirror and `oci-long` the drain of the node of an OCI pass. A red long leg is a finding and holds nothing ([e2e.md](e2e.md#the-mirror-tests)). By hand: `make dev-up && make e2e SET=npm-ha`.

#### The platform matrix

Job `k8s-matrix` of the nightly is the install half of R-HA-049's acceptance ([Q-HA-274](../spec/ha-and-helm.md#q-ha-274--the-platform-matrix-is-a-light-kind-leg-per-kubernetes-version-and-helm-major-that-installs-the-chart-without-starting-its-pods)):
four legs on a GitHub-hosted runner (`ubuntu-24.04`), `k8s-min-helm4`, `k8s-min-helm3`, `k8s-max-helm4` and `k8s-max-helm3`, each a one-node kind cluster of its own that
[deploy/dev/k8s-matrix.sh](../../deploy/dev/k8s-matrix.sh) creates and removes. The oldest Kubernetes is `K8S_MIN_KIND_NODE_IMAGE` (1.34), the newest `DEV_KIND_NODE_IMAGE`
(1.37, the dev profile's node), Helm 4 is `HELM_VERSION` and Helm 3 `HELM3_VERSION` ([tools/versions.env](../../tools/versions.env); Renovate keeps the minimum on 1.34 and Helm 3 on 3.x).
A leg checks that the API server and the helm are the versions the leg names and lints the chart. The chart's TLS has two paths ([Q-HA-347](../spec/ha-and-helm.md#q-ha-347--tlscertmanager-issues-a-certificate-per-kind-tlsexistingsecrets-names-a-secret-per-kind-instead)):
the standard one, `tls.certManager.issuerRef`, renders a cert-manager `Certificate` per kind, and `tls.existingSecrets` names a Secret per kind and needs no CRD. The leg has the API server of the bare cluster admit
the second path first (`helm install --dry-run=server -f ci/ha-ingress.yaml`), then applies the CRDs of the cert-manager chart the dev profile installs (`DEV_CERT_MANAGER_CHART_VERSION`; the CRDs
only, no controller and no webhook, which is all a server-side dry run and an install without pods need) and has it admit six sets of values on the standard path (the roles and pools,
the three sizing profiles of R-HA-050, the backup). It installs the medium profile with the backup on without waiting (the migration hook is off, no database or image is there and no pod starts),
reads back the fields R-HA-049 names as the reason for Kubernetes 1.31 (PDB `unhealthyPodEvictionPolicy`, `nodeTaintsPolicy`, CronJob `timeZone`, the HPA) and the issuer of the `Certificate` of every kind,
upgrades to the small profile and uninstalls. A leg takes about 35 s on a laptop with the node image cached; the TLS path without cert-manager and the CRDs add about 2 s. `gh workflow run "HA and Performance (nightly)" --ref main -f sets=k8s-matrix` runs the four legs, `-f sets=k8s-min-helm3` one. By hand, with the pinned `kind`, `kubectl` and a Helm of the major asked for:
`deploy/dev/k8s-matrix.sh "$K8S_MIN_KIND_NODE_IMAGE" 3` (`QUADER_K8S_MATRIX_CLUSTER` names the cluster instead of `quader-k8s-<minor>-helm<major>`). **Not covered:** no pod starts, so the smoke subset of R-HA-052 does not run on these legs; Kubernetes 1.37 with Helm 4 is the only pair the
failure suite runs on ([H-805](../spec/ha-and-helm.md#h-805--the-platform-matrix-installs-the-chart-and-starts-no-pod)).

### Renovate

[`renovate.yml`](../../.github/workflows/renovate.yml) runs self-hosted Renovate every day at 02:00
Berlin time, with an installation token of the organisation's GitHub App (secrets `APP_CLIENT_ID` and
`APP_PRIVATE_KEY`). The configuration is [renovate.json](../../renovate.json).

Every Renovate branch runs the whole Test and Release pipeline on the organisation's shared runners ([CI](#ci)), so the
number of branches is bounded (owner, 2026-10-31):

- Branches are created and updated only before 6:00 on Mondays (`schedule`, `updateNotScheduled: false`); the daily
  run outside that window changes no branch, but it still closes pull requests whose update is gone.
- Every minor, patch and digest update that no other rule groups is in one pull request, `non-major dependencies`;
  the groups below keep their own. At most three Renovate branches and pull requests exist at a time, at most two
  are opened per hour.
- A major update gets its branch only after it is ticked in the Dependency Dashboard issue.
- Security updates are not held by the schedule (`vulnerabilityAlerts`, `osvVulnerabilityAlerts`).

- Go modules, the Go version (`go.mod`, grouped with `golang.org/x/*`) and GitHub Actions merge on
  their own once CI passes; majors wait for approval and review.
- The pins of [tools/versions.env](../../tools/versions.env) are read by custom managers: a
  `# renovate:` comment directly above a tool or chart pin names its datasource and package; image lines
  are read by their digest, and tag and digest move together. The k6 image and `K6_VERSION` are one
  group, because [deploy/dev/e2e.sh](../../deploy/dev/e2e.sh) refuses a mismatch.
- The two .NET SDK pins of the NuGet tests, `NUGETVECTORS_SDK_IMAGE` and `E2E_NUGETCLIENTS_SDK_IMAGE` (both
  `mcr.microsoft.com/dotnet/sdk`), are one group, `.NET SDK (NuGet tests)`, so one pull request moves both; `make versions-check`
  fails when they differ ([Q-ARCH-279](../spec/architecture.md#q-arch-279--how-are-the-nuget-sdk-pins-and-the-reference-solution-kept-in-step)).
- The release tooling of [package.json](../../package.json) (semantic-release and its presets) is one
  group and never merges on its own. The Angular packages of `web/package.json` are one group, reviewed.
- Every automerged dependency update is a `fix` commit (valkey-operator's policy), so under
  [.releaserc.json](../../.releaserc.json) each of them is a patch release. See
  [Release flow](#release-flow).
- TypeScript is held below 6.1 by a rule of its own (`allowedVersions`, [ADR-140](../decisions.md#adr-140--typescript-is-held-below-61-until-angulars-peer-range-admits-7x)):
  the Angular 22 packages and typescript-eslint admit no 7.x, so Renovate does not open that update.
- The Containerfile has no literal image or toolchain version; its build arguments come from
  `go.mod`, [web/.nvmrc](../../web/.nvmrc) and `tools/versions.env`, so there is nothing for the
  dockerfile manager to update.
- Trivy (`TRIVY_IMAGE` of `tools/versions.env` and `trivy.image` of the chart, held equal by a chart test) is the group `trivy`: a new
  digest of the same version merges by itself, a new version (patch, minor or major) is a pull request labelled `trivy-version` to
  review, because it makes the scanners analyse every stored image again and may read another database schema
  ([Q-HA-381](../spec/ha-and-helm.md#q-ha-381--one-pin-by-digest-the-charts-value-held-equal-by-a-chart-test-and-one-renovate-group-that-reviews-a-new-version)).

## Release flow

[`.releaserc.json`](../../.releaserc.json) (preset `conventionalcommits`, releases from `main`), the
tooling pinned in [package.json](../../package.json) and [package-lock.json](../../package-lock.json):

1. Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `feat:` makes a
   minor release, `fix:` and `perf:` a patch release, other types none. A breaking change (`!` or a
   `BREAKING CHANGE:` footer) makes a minor release too, while Quader is on major version 0
   (`releaseRules`).
2. Once every job of a push run on `main` passed, the job Semantic Release takes a token of the
   organisation's GitHub App (`permission-contents: write`), runs `npm ci` and `npm audit signatures`,
   runs `npx semantic-release`: the
   version from the commits since the last tag, the notes, the badge committed as
   `chore(release): <version> [skip ci]` with the notes in its body, the tag `v<version>` on that commit,
   and the GitHub release. `[skip ci]` keeps the release commit from starting a run.
3. The release's `published` event starts [the release pipeline](#the-release-pipeline), because the
   App's token made the release: one made with `GITHUB_TOKEN` starts no workflow.

A release that commits the badge moves `main`: `git pull --rebase` before the next push.

**Releases stay on major version 0** until the owner decides on 1.0 ([ADR-124](../decisions.md#adr-124--releases-stay-on-major-version-0)).

### When no release comes

Semantic Release needs every job of the run: one failed or cancelled job skips it. A newer push cancels
the running run (`cancel-in-progress`), which loses nothing: the next green run releases every commit
since the last tag.

- **A job failed:** `gh run list --branch main --limit 3`, then for the newest commit's run
  `gh run view <id>` and `gh run rerun <id> --failed`. Nothing re-runs by itself.
- **A commit was pushed while the run was in progress, with `[skip ci]` in its message:** that push starts
  no run and cancels none, so the running run passes, but Semantic Release then finds `main` ahead of its
  commit ("The local branch main is behind the remote one") and publishes nothing. GitHub reads the marker
  anywhere in the message. A dispatched run (`gh run … workflow_dispatch`) does not release either: the job
  runs on a push only. Push a new commit without the marker (an empty `ci:` commit when nothing else is due).
- **The release is published but its image or chart is not:** the run of that tag in
  `Release Docker & Helm` (`gh run list --workflow "Release Docker & Helm"`), then
  `gh run rerun <id> --failed`. The job pushes the same tags again.
- **semantic-release stopped after the tag** (a tag without a GitHub release, no pipeline): a re-run
  publishes nothing, its commit being behind `main`. Finish by hand, as Semantic Release would:

  ```sh
  git notes --ref semantic-release-v<version> add -f -m '{"channels":[null]}' v<version>
  git push origin refs/notes/semantic-release-v<version>
  git log -1 --format=%B <release commit> | tail -n +3 > notes.md
  gh release create v<version> --verify-tag --title v<version> --notes-file notes.md --latest
  ```

  A person's token starts the pipeline as the App's does, with that person as the release's author.

## The release pipeline

[`build.yml`](../../.github/workflows/build.yml) ("Release Docker & Helm") runs on `release: published`.
The tag is the version: `v0.1.0` gives the image the tags `0.1.0`, `0.1` and `0` (the `latest` tag
belongs to pushes to `main`, not to releases), and the `version` and `appVersion` `0.1.0` of both charts. Every
job checks out the tag.

| Job | Runner | What it does |
|---|---|---|
| Build Docker Image | `self-hosted` ([CI](#ci)) | [docker/build-push-action](https://github.com/docker/build-push-action) builds [Containerfile](../../Containerfile) for `linux/amd64,linux/arm64` and pushes `guidedtraffic/quader` to Docker Hub with the tags of [docker/metadata-action](https://github.com/docker/metadata-action); provenance and SBOM attestations (the SBOM from the generator image of [tools/sbom-generator](../../tools/sbom-generator), built in this job from `SYFT_IMAGE` and served by digest from a throwaway registry container on the default bridge network (reached by its IP; the buildx builder stays on that network too), [Q-HA-166](../spec/ha-and-helm.md#q-ha-166--the-sbom-attestation-of-the-image-comes-from-a-generator-that-runs-the-pinned-syft); the image is pushed by digest without a tag, a step asserts that both platforms' attestations name the pinned syft, and only then `docker buildx imagetools create` adds the tags); the build arguments are the pins (`NODE_VERSION` from [web/.nvmrc](../../web/.nvmrc), `GO_VERSION` from [go.mod](../../go.mod), `RUNTIME_BASE_IMAGE` from [tools/versions.env](../../tools/versions.env)) and the version, the commit and the commit's date for the binary's version line |
| Scan and attach the SBOM | same | the SPDX SBOM written by the image `SYFT_IMAGE` on the release tag (`docker run`, no download from GitHub, [Q-HA-204](../spec/ha-and-helm.md#q-ha-204--the-spdx-file-of-the-release-is-written-by-the-pinned-syft-image-from-the-registry-tag)), uploaded to the release; a Docker Scout CVE report |
| Release Helm Chart to GitHub Pages | same | `deploy/helm/quader` and `deploy/helm/quader-bastion` with `version` and `appVersion` set to the release (and the image tag in the `values.yaml` of `quader`), packaged together with one `helm package`, added to the chart repository's `index.yaml` on the `gh-pages` branch in one push, and `quader-<version>.tgz`, `quader-bastion-<version>.tgz` and `index.yaml` attached to the release |

**The Containerfile** has three stages. `web` runs `npm ci` and `npm run build:embed` on the Node.js of
`web/.nvmrc`; `build` cross-compiles the binary for the target platform on the build platform
(`CGO_ENABLED=0`), so neither architecture is emulated; the final stage is the distroless base of
`RUNTIME_BASE_IMAGE`, the binary and nothing else, as UID 65532 (83.3 MB on arm64; 246.5 MB with the analyser it carried until
2026-11-21, which the scanner pods now copy from `TRIVY_IMAGE`, R-HA-070). `make image` builds the same file for the host architecture.

**The chart repository** is `https://guided-traffic.github.io/quader/`, served from the `gh-pages`
branch: every release adds the packages of both charts and the merged `index.yaml`. The release's assets are the
two packages of that release and the index. The Bastion chart has no version of its own: its `version` and `appVersion`
are the Quader version of the release (R-ARCH-025, R-BAS-045), so one release publishes `quader` and `quader-bastion` at the same
number, and a Bastion chart is installed with the image of its `appVersion` (its `image.tag` is empty, which means
`appVersion`; the release job leaves that line alone). The chart's defaults point at the image: `image.registry:
docker.io`, `image.repository: guidedtraffic/quader` ([values.yaml](../../deploy/helm/quader/values.yaml)),
checked by [test/chart/image_test.go](../../test/chart/image_test.go). The release job rewrites the line
`  tag:` of the `values.yaml` of the chart `quader`; the test fails when a second line of that shape appears.

**One syft.** `SYFT_VERSION` in [tools/versions.env](../../tools/versions.env) writes both SBOMs of the image: the SPDX file of the release
(the image `SYFT_IMAGE`, run on the registry tag, [Q-HA-204](../spec/ha-and-helm.md#q-ha-204--the-spdx-file-of-the-release-is-written-by-the-pinned-syft-image-from-the-registry-tag)) and the attestation in the image index (the generator of `tools/sbom-generator`, whose
[Containerfile](../../tools/sbom-generator/Containerfile) takes the syft binary from `SYFT_IMAGE`, the same version by digest, and has no version of its own). The scanner that
BuildKit bundles, `docker/buildkit-syft-scanner`, ships an older syft (v1.51.0 in its newest release, 1.12.1, checked on 2026-11-07), which is why it is not used.
`make versions-check` runs [check-pins.sh](../../tools/sbom-generator/check-pins.sh) and fails on a `SYFT_IMAGE` that is not `SYFT_VERSION` by digest, on a default or a
syft image of its own anywhere else in the repository, on a build.yml that reads another version or sets `sbom` to anything but the generator, and on a `syft` on `PATH` that
is not the pin. The generator is served from a registry container of the run (`quader-sbom-registry-<run id>-<attempt>`) on the default bridge network and reached by its IP, with plain HTTP allowed for that address only; the buildx builder takes no network option, because a builder on a user-defined network cannot resolve names in its `RUN` steps (release v0.25.1, run 42433259845) and `network=host` would put them into the runner's namespace. The image is pushed by digest without a tag, and a step runs
[creators.sh](../../tools/sbom-generator/creators.sh) on that digest before `imagetools create` adds the tags (version, major.minor, major, sha, and `latest` on main; the attestations are part of the index and stay); a failure moves no tag, publishes no chart and
attaches no SPDX file, and leaves an untagged image by digest in the repository ([Q-HA-166](../spec/ha-and-helm.md#q-ha-166--the-sbom-attestation-of-the-image-comes-from-a-generator-that-runs-the-pinned-syft)). What keeps that from
happening on a release is the job `sbom-generator` of Test and Release ([below](#sbom-generator)). The `build` job itself first runs on the next release: if its generator step
fails, the release stops before the chart is published, and `gh run rerun <id> --failed` runs it again once the cause is fixed.

Not in the pipeline: a chart SBOM, a chart attestation, and any signature. Only the image carries
attestations: a provenance and an SBOM attestation in its image index.

## Secrets and settings

The pipeline uses three secrets of the organisation, which reach this repository without a setting here:
`DOCKERHUB_PAT` (the Docker Hub access token of the user `guidedtraffic`) for the image, and
`APP_CLIENT_ID` and `APP_PRIVATE_KEY` for the GitHub App that Semantic Release and Renovate act as.

One setting is the owner's to make: the `gh-pages` branch that holds the chart repository, which GitHub
Pages serves at `https://guided-traffic.github.io/quader/`.

## Not in place yet

- **No signatures.** The image carries a provenance and an SBOM attestation, not a keyless signature. The
  chart carries neither a signature nor an attestation.
- **Nothing runs against the published artifacts.** Tests run before the release, on the image and chart built from the commit ([Q-HA-281](../spec/ha-and-helm.md#q-ha-281--tests-run-before-a-release-not-after-it)); that an image or chart can actually be pulled from Docker Hub or GitHub Pages after publishing is not checked.
- **Docker Buildx of the local machine** is the one of Docker Desktop, not the pinned version, until it
  is replaced ([make versions-check](../../Makefile) reports it).
- **Dependency updates are patch releases.** Each automerged update is a `fix` commit.
- **The cyclomatic complexity threshold** is the ratchet of today, not the 15 of valkey-operator.
