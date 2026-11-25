# Build, test and lint

Everything runs through the GNU Makefile at the repository root (GNU Make 3.81 is enough, the version
macOS ships). `make help` lists the targets. This page describes what exists today; targets whose
script another work package provides are marked.

## Tool pins

One version per tool (E0.2, R-ARCH-003). Each pin lives in exactly one place, and the Makefile and CI
read it from there.

| Tool | Pinned in | Notes |
|---|---|---|
| Go | the `go` line of [go.mod](../../go.mod) (`1.27.4`) | `make versions-check` fails when `go version` differs |
| sqlc, oapi-codegen | the `tool` directives of [go.mod](../../go.mod) | run as `go tool sqlc …`, `go tool oapi-codegen …`; `versions-check` compares the running version with the module version |
| golangci-lint, kind, kubectl, Helm, k6, CloudNativePG chart | [tools/versions.env](../../tools/versions.env) | `KEY=value` lines; the Makefile `include`s the file and exports its keys, CI appends it to `$GITHUB_ENV` |
| Container images (PostgreSQL, PgBouncer, Valkey 9.1 (the only line tested while Quader is in development), SeaweedFS, the release base image, the dev-profile images) | [tools/versions.env](../../tools/versions.env), each with its digest | `docker buildx imagetools inspect <tag>` prints the digest to pin |
| openssl | `OPENSSL_IMAGE` in [tools/versions.env](../../tools/versions.env), a container image with its digest | never the host's openssl (LibreSSL on macOS, 3.x on Ubuntu): [certs.sh](../../test/integration/certs.sh) and the e2e probes ([e2e.md](e2e.md)) run it with `docker run --rm --user "$(id -u):$(id -g)" -v <dir>:<dir> -w <dir>`, so the files belong to the caller |
| Angular packages, Node.js | `web/package-lock.json`, `web/.nvmrc` | owned by the web workspace, see [web-ui.md](web-ui.md) |

golangci-lint is built from source with `go install` into
`.bin/golangci-lint-<version>-<go version>`. The file name carries both inputs, so a change of the pin
or of the Go toolchain builds a new binary and a stale one is never reused. `.bin/` is ignored by git.
`make versions-check` also compares the installed kind, kubectl, Helm and k6 (when present on `PATH`)
with their pins and fails on a difference; a tool that is not installed is not an error there.
It also runs [check-pins.sh](../../tools/sbom-generator/check-pins.sh), which keeps one syft version for the release image's two SBOMs (a `syft` on `PATH` must be `SYFT_VERSION`; the generator's Containerfile and build.yml must not carry a version of their own, [Q-HA-166](../spec/ha-and-helm.md#q-ha-166--the-sbom-attestation-of-the-image-comes-from-a-generator-that-runs-the-pinned-syft)).

## Targets

| Target | What it does |
|---|---|
| `build` | `CGO_ENABLED=0 go build -trimpath` into `bin/quader`, with version, commit and date set through `-ldflags` in [internal/buildinfo](../../internal/buildinfo) |
| `generate` | `go generate` over every package: the configuration schema today, sqlc and oapi-codegen as their packages arrive |
| `generate-check` | `generate`, then `git diff --exit-code` and a check for untracked generated files (E0.1) |
| `lint` | pinned golangci-lint over `./...` with the build tags `integration`, `e2e`, `compat` and `chart`, so every test tier is linted (configuration in [.golangci.yml](../../.golangci.yml)), then `go vet`. Its one time limit is `LINT_TIMEOUT` (default 20 minutes; `.golangci.yml` sets none, and golangci-lint v2 has none by default): the CI job took 2.2 to 2.6 minutes, a development machine at a load of 70 more than ten, and 20 stays below the lint job's 25, so a hung run there ends with the linter's own message. It runs with `--allow-serial-runners`: a second golangci-lint on the machine (another worktree, `make gosec`) waits for the lock of the first instead of ending at once with "parallel golangci-lint is running", and make says so before it starts (`LINT_WAIT`). A run started with `--allow-parallel-runners` takes no lock, so one beside it does not wait |
| `gosec` | the same golangci-lint with only gosec enabled, with the same limit and the same wait (CI: GoSec Security Scan) |
| `vuln` | pinned govulncheck over `./...` |
| `cyclo`, `cyclo-report` | pinned gocyclo: fails above `CYCLO_THRESHOLD`; the report lists the most complex functions, tests included |
| `malware-scan-db`, `malware-scan` | `freshclam` of `CLAMAV_IMAGE` into `.out/clamav-db` (the day's differences after the first run), then its `clamscan` over the source tree mounted read-only; needs Docker (R-MAL-031) |
| `versions-check` (continued) | also compares the Node.js of `PATH` with `web/.nvmrc` and `docker buildx` with `BUILDX_VERSION`; and the analyser's one version (Q-HA-381): `TRIVY_IMAGE` carries a digest, no `TRIVY_VERSION` or `TRIVY_SHA256_*` line and no `trivy` stage or `TRIVY_*` build argument in the Containerfile remain, the binary of `make trivy` (on Linux) and a `trivy` on `PATH` say the tag of `TRIVY_IMAGE`; the chart's `trivy.image` is held to the pin by the chart test |
| `fmt` | the formatters of `.golangci.yml` (gofumpt, goimports) over `./...`; this rewrites files, so run it on your own working tree |
| `cores` | prints the cap of the cores every target runs with (`LOCAL_CORES`, default 6; `LOCAL_CORES=` lifts it, [The cap of the cores](#the-cap-of-the-cores)) |
| `versions-check` | the E0.2 checks listed above |
| `test` | `go test -race ./...` with a coverage profile of the unit tier (`.out/coverage/unit.out`) and a limit of `UNIT_TIMEOUT` per test binary (default 20 minutes, below the unit job's own 30; see [The unit run's time](#the-unit-runs-time)); the `go test -json` stream goes to `.out/test-unit.json`, which the coverage generator reads. It runs without the host's AWS configuration (`UNIT_ENV` in the Makefile: no `AWS_*` region, profile or keys, no `~/.aws` files), as on the CI runner; a bare `go test` reads them and can pass where CI fails |
| `unit` | the packages of `test` with the same `UNIT_ENV` and `UNIT_TIMEOUT`, `go test -count=1`, without the race detector and the coverage profile and without the JSON stream: the unit run before a push, minutes where `test` takes about a quarter of an hour. A bare `go test ./internal/... ./cmd/...` reads the host's AWS configuration and leaves out the packages under `test/` and `tools/` that `PKGS` holds |
| `integration-up`, `integration-down` | start and stop the integration stack, `INSTANCE=2..9` another instance beside instance 1, see [Instances and their ports](#instances-and-their-ports) |
| `integration-run` | `ARGS='<packages and flags>'`: one `go test` run on whichever instance of the stack is free, under its lock ([run.sh](../../test/integration/run.sh), see [Integration stack](#integration-stack)) |
| `test-integration` | `go test -tags integration ./test/integration/...` with the environment of the main checkout's `.dev/integration/env`, or of `.dev/integration-<n>/env` with `INSTANCE=<n>` (a worktree uses the stacks the main checkout started; the target takes no lock, so a builder runs it as `test/integration/locked.sh make test-integration`, which takes a free instance, see [Integration stack](#integration-stack)); the JSON stream goes to `.out/test-integration.json`, the profile to `.out/coverage/integration-<line>.out`; `SHARD=a\|b\|c\|d` runs one quarter of the packages (the four CI legs, by a rule on the area's name that places a new package without a list being edited, [Q-ARCH-565](../spec/architecture.md#q-arch-565--the-integration-tests-run-in-four-shards-the-fourth-takes-the-virtual-areas-and-the-security-checks); the suffix decides first: every `<format>local` in `a`, every `<format>remote` in `c`, every `<format>virtual` in `d`; then `a` the areas `coordination`, `mirror`, `npm`, `nuget` and `proxy`, `c` the areas `api`, `backup*`, `restore*`, `storage*`, `postgres`, `blob`, `valkey` and `bastion*`, `d` the security checks `ci`, `vuln*`, `malware*` and `sign*`, `b` everything else, every new platform area among them) and adds `-<shard>` to both names; `internal/tools/ciworkflows` checks that every package runs in exactly one shard of the workflow's matrix |
| `test-performance` | the P-13 and P-14 latency tests of the integration tier, the Debian delay bound `TestR_DEB_010_DelayBound` (R-DEB-010, Q-DEB-59) and `TestR_EP_013_CeilingPerformance` of `internal/authz` and the takeover bound of `TestFlightLockWait` (R-ARCH-033) of `internal/cluster` and the decode time of the slowest accepted avatars (`TestR_API_055_TheWorstCaseInputsAreAccepted` and `TestR_API_055_TheSlowestAcceptedImagesDecodeInsideTheDeadline` of `internal/avatar`, Q-UI-173; both skip under `-race`, so only this target runs them) with `QUADER_PERF_GATE=1`, which enforces their p99 bounds (for the avatar tests only the least CPU time of seven decodes: the wall time of a decode on a shared runner is the runner's load, so it is logged and not judged) (with Valkey for the first; `test-integration` and `make test` log them only); the JSON stream goes to `.out/test-performance.json`. Run by the nightly workflow "HA and Performance (nightly)" ([releasing.md](releasing.md#ha-and-performance-nightly)) |
| `silo-up`, `silo-down`, `s3-conformance` | Silo at `SILO_IMAGE`, served over TLS, and the conformance tests against it ([test/integration/silo.sh](../../test/integration/silo.sh)) |
| `coverage-report` | merges `.out/coverage` into `coverage.txt`, `summary.md` and the badge `coverage.json` ([test/coverage/report.sh](../../test/coverage/report.sh)) |
| `test-release-tooling` | `npm ci`, `npm audit signatures`, and [hack/verify-release-tooling.mjs](../../hack/verify-release-tooling.mjs) |
| `web-ci`, `web-lint`, `web-test`, `web-embed` | `npm ci`, the lint script, the Angular unit tests and `npm run build:embed` in `web/`; `web` is `web-ci` and `web-embed` |
| `web-i18n`, `web-i18n-check` | The translation files against a fresh `ng extract-i18n`: `web-i18n` writes the source file and the German file's units and location notes, `web-i18n-check` only checks ([web-ui.md](web-ui.md)) |
| `image` | builds the release image for `linux/<arch>` (`IMAGE`, default `guidedtraffic/quader:<DEV_TAG>`, `dev` in the main checkout and `dev-<worktree>` in a worktree, [local-profile.md](local-profile.md#one-image-tag-per-checkout)) with the [Containerfile](../../Containerfile): the Angular build, the Go build and the distroless base with the binary alone (no analyser: the scanner pods copy it from `TRIVY_IMAGE`), no Node.js on the host. The binary's version is `VERSION`: the number of a `v0.*` tag on `HEAD` (the `v` stripped), else `dev`, else what the caller passes (CI passes `VERSION=dev`). A commit hash is never a version: the binary would not become ready. The image carries the label `quader.build=make-image`, and after the build the target removes the untagged images with that label, which earlier builds under the same tag left (`docker image prune --filter dangling=true`; images of other builds, and one a container uses, stay; a failed prune does not fail the target) |
| `npm-stub-image` | builds the counting npm test upstream (`test/npmstub/cmd/npmstub`) for `linux/<IMAGE_ARCH>` on the release base image, tag `NPM_STUB_IMAGE` (default `quader-e2e-npmstub:dev`); see [e2e.md](e2e.md#the-counting-npm-test-upstream) |
| `npm-reference`, `npm-reference-check` | write, or check against a regeneration, the reference lockfile `perf/npm/reference` with the npm of the pinned Node image (`E2E_CLIENTS_NODE_IMAGE`) against the stub; need Docker, see [perf/README.md](../../perf/README.md) |
| `oidc-stub-image` | builds the GitLab-shaped OIDC issuer double (`test/oidcstub/cmd/oidcstub`) for `linux/<IMAGE_ARCH>` on the release base image, tag `OIDC_STUB_IMAGE` (default `quader-e2e-oidcstub:dev`); see [e2e.md](e2e.md#the-oidc-issuer-double) |
| `oidc-fixtures` | re-records `test/oidcstub/testdata` from a real GitLab (`QUADER_GITLAB_URL`, `QUADER_GITLAB_TOKEN` of an administrator, optional `QUADER_GITLAB_CA_FILE`), then runs the double's fixture tests; manual, never in CI; see [e2e.md](e2e.md#validating-the-double-against-a-real-gitlab) |
| `nuget-stub-image` | builds the counting NuGet test upstream (`test/nugetstub/cmd/nugetstub`) for `linux/<IMAGE_ARCH>` on the release base image, tag `NUGET_STUB_IMAGE` (default `quader-e2e-nugetstub:dev`); see [e2e.md](e2e.md) |
| `nuget-vectors`, `nuget-vectors-check` | write, or check against a regeneration, the golden vectors of the NuGet version, range and ID rules ([tools/nugetvectors](../../tools/nugetvectors)) with the pinned .NET SDK image and NuGet.Versioning; need Docker (`DOTNET=local` runs a local `dotnet`) |
| `nuget-reference-check` | fails when [perf/nuget/reference](../../perf/README.md) differs from a regeneration by the counting NuGet test upstream (`nugetstub reference -check`, no Docker); CI runs it in the job generate |
| `deb-stub-image` | builds the counting Debian archive test upstream (`test/debstub/cmd/debstub`) for `linux/<IMAGE_ARCH>` on the release base image, tag `DEB_STUB_IMAGE` (default `quader-e2e-debstub:dev`); see [deb.md](deb.md#testdebstub-the-counting-stub-archive) |
| `apt-clients-image` | builds the four APT client images of the Debian e2e set (the pinned Debian 13, Debian 12, Ubuntu 24.04 and Ubuntu 22.04 images with curl and the forwarder, `test/e2e/aptclients/build.sh`) into the local Docker; see [e2e.md](e2e.md#the-apt-clients) |
| `ansible-clients-image` | builds the two Ansible client images of the `ansible` e2e set (the pinned Python 3.11 and 3.13 images with ansible-core 2.17.14 and 2.21.6 under pinned constraints, curl and the forwarder, `test/e2e/ansibleclients/build.sh`) into the local Docker; see [e2e.md](e2e.md#the-ansible-clients) |
| `oci-stub-image` | builds the counting OCI test upstream (`test/ocistub/cmd/ocistub`) for `linux/<IMAGE_ARCH>` on the release base image, tag `OCI_STUB_IMAGE` (default `quader-e2e-ocistub:dev`); see [oci.md](oci.md#the-counting-stub-registry) and [e2e.md](e2e.md#the-counting-oci-test-upstream) |
| `oci-conformance-build`, `oci-conformance` | build the OCI distribution conformance suite (`OCI_CONFORMANCE_TAG`, default `v1.1.1`: the tag is resolved to its commit, the module `github.com/opencontainers/distribution-spec/conformance` is downloaded into `.out/oci-conformance/mod` and compiled with the repository's Go to `.out/oci-conformance/conformance.test`), and run it against a replica of the integration stack (`make integration-up` first) through a local and a virtual repository; the reports go to `.out/oci-conformance/{local,virtual}/` (`junit.xml`, `report.html`, `output.txt`). CI runs it in shard c of the integration job; see [oci.md](oci.md#the-protocol-tests-and-the-conformance-suite) |
| `osv-stub-image` | builds the test advisory source (`test/osvstub/cmd/osvstub`) for `linux/<IMAGE_ARCH>` on the release base image, tag `OSV_STUB_IMAGE` (default `quader-e2e-osvstub:dev`); see [e2e.md](e2e.md#the-test-advisory-source) |
| `osv-real-refresh` | fetches the real OSV records frozen in `test/osvstub/testdata/real` again from `api.osv.dev` (`test/osvstub/cmd/osvfreeze`); needs network, manual, never in CI |
| `fuzz-nuget` | runs each NuGet fuzz target (`NUGET_FUZZ_TARGETS`, `package:Target` pairs, the package below `internal/`) for `NUGET_FUZZTIME` (default 5m): the format's readers and rules under `formats/nuget/`, and the reader of a NuGet signature (`signatures/scheme/nugetcms:FuzzSignatureP7s`, `signatures/scheme/rawkey/pki:FuzzDefinite`, [signatures.md](signatures.md#defects-found)); a failing input stays in the package's `testdata/fuzz`. CI runs one target per leg for 30 minutes, on the closing run only ([releasing.md](releasing.md#ci)). Every fuzz target of the Makefile (`fuzz-npm`, `fuzz-nuget`, `fuzz-stored-format`) shrinks a new input for at most `FUZZMINIMIZETIME` (default 5 s) instead of Go's minute, which stalled a target with large seeds |
| `coverage` | `go run ./internal/tools/coverage`, see [coverage.md](coverage.md) |
| `chart-lint`, `chart-test` | call `test/chart/lint.sh` and `test/chart/test.sh`; provided by the chart work package |
| `trivy`, `trivy-dir` | takes the analyser's binary out of `TRIVY_IMAGE` of [tools/versions.env](../../tools/versions.env) (the image the chart's init container copies into every scanner pod, [R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod)): `docker create --platform linux/<arch>` by the index digest, `docker cp` of `/usr/local/bin/trivy` into `.bin/trivy-<tag>-<12 hex of the digest>/`, and a check that the copy says the pin's tag; Linux only (amd64, arm64), on macOS it refuses with a message that names the image, which then runs as `docker run --rm <TRIVY_IMAGE> …`. `trivy-dir` prints the directory, which the container scan job puts on `PATH`, so the scan of the release image runs the scanner's own analyser |
| `dev-up`, `dev-down`, `dev-failover`, `dev-valkey-failover`, `dev-valkey-kill`, `dev-valkey-outage` (`DURATION=60s`), `dev-drain`, `dev-valkey-stop`, `dev-valkey-start`, `e2e` | call `deploy/dev/<name>.sh` (`up`, `down`, `failover`, `valkey-failover`, `valkey-kill`, `valkey-outage`, `valkey-stop`, `valkey-start`, `drain`, `e2e`); the Valkey scripts are in [deploy/dev/](../../deploy/dev/); the other scripts of the list were not checked in this pass |

Go commands exclude `web/` from `./...` through the `PKGS` variable: npm packages in
`web/node_modules` can ship Go files, which `./...` would otherwise compile. Plain `go test ./...`
on a checkout with an installed `node_modules` therefore picks them up; use `make test`.

## Test tiers

| Tier | Where | Needs | Run with |
|---|---|---|---|
| Unit | `*_test.go` next to the code | nothing | `make test`; before a push `make unit` |
| Architecture | [internal/archtest](../../internal/archtest) | `go` on `PATH` (it runs `go list -deps -json ./...` and loads the packages with type information) | part of `make test` |
| Integration | [test/integration](../../test/integration), build tag `integration` | the stack below | `make integration-up && make test-integration`; one area: `test/integration/run.sh ./test/integration/<area>/` |
| Compatibility | [test/compat](../../test/compat), build tag `compat` | the git history and the release tags (`git fetch --tags`), oasdiff at its pin (the target builds it) | `make compat-check`; its CI job `compat` is switched off until the first stable release ([releasing.md](releasing.md#switched-off-until-the-first-stable-release), Q-ARCH-340) |
| HA and Performance (nightly) | `make test-performance`: the latency tests of [test/integration/coordination](../../test/integration/coordination) and `TestR_EP_013_CeilingPerformance` with the p99 gate; on kind clusters the e2e sets `failover`, `npm-ha`, `npm-ha-soak` and `ci-ha` ([releasing.md](releasing.md#ha-and-performance-nightly)) | the stack below; a kind cluster for the e2e sets | `make integration-up && make test-performance`; `make dev-up && make e2e SET=failover` (or `npm-ha`, `npm-ha-soak`) |
| End to end | `deploy/dev`, `test/e2e` | a kind cluster | `make dev-up && make e2e`; `SET=access` runs the access-core scenarios (`test/e2e/e1_*`, `m1_env_test.go`), `SET=npm` the npm tests and `SET=npm-ha` the drain and failovers under npm load ([e2e.md](e2e.md#drain-and-failover-under-npm-load)). CI runs `SET=push`, `access-long`, `npm`, `nuget` and `ci` per change (`SET=ci` is the GitLab CI reference pipeline, [e2e.md](e2e.md#the-ci-tests)); `failover`, `npm-ha` and `npm-ha-soak` run nightly and on dispatch (owner, 2026-11-01; [releasing.md](releasing.md#ha-and-performance-nightly)), and `npm-ha-crash` by hand only. `make dev-valkey-stop` and `make dev-valkey-start` hold a Valkey outage across a criterion of unknown length |

A test that verifies a requirement carries the requirement ID in a subtest name, for example
`t.Run("R-ARCH-002 forbidden import names the edge", …)`. The coverage generator reads the IDs from the
`go test -json` stream ([coverage.md](coverage.md)). Go replaces spaces by underscores in the name; the
ID survives. A function name may carry it in the underscore form, `TestR_API_053_Name`, which names
`R-API-053` ([coverage.md](coverage.md#implementation-status-the-test-naming-rule)). A test name never carries a
decision's number (`Q-…`, `Q_…`, `ADR-…`): it names the requirement the test holds, or the behaviour alone when it holds
none, and a decision is cited in a comment. `go test ./internal/tools/coverage/` fails on a Go test name that breaks it.

A latency target in microseconds is not a per-change assertion on a shared machine: the per-change run asserts the
allocations, and the median where the race detector is off (`make test` runs with it, so there only the allocations), and the p99 is enforced with `QUADER_PERF_GATE=1` by `make test-performance` (for example
`TestR_EP_013_CeilingPerformance`, [Q-ARCH-250](../spec/architecture.md#q-arch-250--a-p99-bound-in-microseconds-is-a-nightly-gate-and-the-per-change-test-asserts-allocations-and-the-median)).

A wait in a test is a guard or a budget, and only a budget may be tight. A guard bounds a wait for something that happens on its
own (a replica that starts, a writer that retries, a loop that repeats) and fails only when it never happens; it is generous (30 to 60 s:
`startGuard` in [internal/app](../../internal/app/app_test.go), `settle` in [internal/audit](../../internal/audit/writer_test.go)),
because a CI runner with a CPU limit and the race detector took tens of times as long as an idle machine. A test waits for a condition
(poll it) and does not sleep for a fixed time and count what happened in it. A fake clock that a test advances is advanced inside the
wait, because the code under test reads it at a moment the test does not see. The evidence: the start-up tests of `internal/app` failed in
50 of 300 processes run at once on 20 CPUs before this rule and in none after it.

### The cap of the cores

A local gate runs on 6 cores: the Makefile's `LOCAL_CORES ?= 6` exports `GOMAXPROCS` and `-p=6` in `GOFLAGS` to every Go command
of a target and passes `--concurrency 6` to the pinned lint, and [run.sh](../../test/integration/run.sh) does the same for an
integration run called without `make`. On 2026-11-23 four builders' gates beside each other took the development machine to a load
above 100, and tests failed by it; the cap keeps four gates inside the machine's 20 cores. A `GOMAXPROCS` or a `-p` the caller sets
stays (`GOMAXPROCS=1 test/integration/run.sh …` to show that a test holds on a starved host). `make cores` prints what a gate gets.

To lift it for a single run on an idle machine: `LOCAL_CORES= make unit`, `LOCAL_CORES= test/integration/run.sh ./test/integration/<area>/`.
The pipeline's workflows set `LOCAL_CORES: ""`, so a job keeps its own `GOMAXPROCS` (8) and the lint its default;
`TestEveryJobThatRunsACappedTargetLiftsTheCap` ([localcores_test.go](../../internal/tools/ciworkflows/localcores_test.go)) fails on a
job that runs `make` or the integration runner without it, and `TestTheMakefileCapsTheCoresUnlessLifted` holds what `make cores`
prints (milliseconds and about a second).

### Tests that hold on a loaded host

On 2026-11-23 four builders' gates beside each other took the development machine to a load of 30 to 130, and the tests below failed by
it, each green alone. What each waited for, and what it waits for now
([Q-ARCH-702](../spec/architecture.md#q-arch-702--a-test-waits-for-the-state-it-stands-for-and-does-not-depend-on-the-hosts-speed)):

| Test (area) | What a starved host broke | Now |
|---|---|---|
| `TestR_AUTH_038_E1_8_ThrottleAcrossThreeReplicas`, `TestR_AUTH_038_RevocationByValueCountsInTheFlowStartBudget` (`identity`) | a budget of 9 (4) a minute refilled a token every 6.7 s (15 s) of the real clock, and a window of the cluster's count turned once a minute: 401 where 429 was expected | the limiters' clock stands still (`replicaOpts.limiterNow`); Valkey gets 10 s for a count, and a failure prints how often a replica decided on its share |
| `ColdAndWarmCache` (`bastion`) | the warm run came more than `metadata_revalidate_after` (3 s) after the cold one and revalidated | the period is the default minute and the Bastion's clock moves past it (`b.clk.Advance`) |
| `ColdAndWarmCache`, `EveryCallerIsAnsweredAsTheMainInstallationAnswers` (`bastion`) | the harness's link bounds (connect 1 s, unreachable after 3 s, an answer within 10 s): 503 `main_unreachable` and 504 `main_timeout` from a Bastion nobody cut | the product's defaults (5 s, 10 s, 30 s); the short ones only for a Bastion of `bastionOptions{CutsLink: true}`, the tests that cut the link |
| `TestGCKilledAtRandomPoints` (`blob`) | a commit must insert its row within `delete_settle/2` (75 ms with `gcFast`) of its object write; five misses answer "commit did not settle" | the helper `garbage` retries a busy commit as a client does, for at most two minutes, and says the window when it gives up |
| `TestR_HA_011_UploadsDuringARolloutRestart` (`files`) | 300 ms before stopping the server, where the upload had not reached it yet; a commit grace of 4 s against an object write of 1.5 s | the test waits until the listener has the request (`Server.Active`); the committing case runs with a commit grace of a minute |
| `TestR_MAL_014_APendingDownloadIsServedWhenItsVerdictArrives` and its neighbours (`internal/malware/gate`, unit) | the gate's wait of 5 s ended the download before the signal (5.01 s) | the wait is an hour where the wait's bound is not the subject; the test's own bounds tell an answer at once from the whole wait (10 s against 25 s) |
| `TestR_NUGET_004_AWorkerRecomputesTheSemVer2Flag…` (`nuget`) | 30 s for a worker to start, enqueue and run one job | two minutes, and a failure lists the runs and their states |
| `TestInvalidationAtScale` (`repository`) | a million entries seeded in 74 s and folded until the statement timeout | 2,000 entries and a fold batch of 250; the million is a measurement ([test-cost.md](test-cost.md#12-the-million-of-r-repo-045-measured-once-2026-11-23), [Q-REPO-481](../spec/repositories-and-proxy.md#q-repo-481--the-million-entries-of-r-repo-045s-criterion-are-a-measurement-the-test-holds-the-invalidations-shape-with-2000)) |
| `TestInvalidationNegativeRetention`, `TestGarbageCollection` (`repository`, `blob`) | the migration of a new database met the area's minute | the area's database is a clone of the template (`testenv.MigratedDatabase`); so are those of `identity`, `files` and `coordination` |
| `TestR_ARCH_027_*` (`querybudget`) | a window of the database's log counted the readiness check's `GetSystemState` as a download's | the statements that ran with the request's ID ([Q-ARCH-700](../spec/architecture.md#q-arch-700--a-statement-budget-counts-the-statements-that-ran-with-the-requests-id-not-the-databases-log-of-a-window)) |
| `TestR_CI_003_*` (`ci`) | reads of the key set counted from the table's statistics, published up to 1.5 s late; a second as the bound of a lock wait; 4 s for a change to propagate | the replica's own reads counted by a tracer; `FOR SHARE NOWAIT` and an answer while the lock is held; one change-log poll (`PollNow`) |
| `TestJobs`, 100 coalesced enqueues (`coordination`) | a rebuild of a fixed 1.5 s against a burst that took 2.6 s | the rebuild lasts until the burst has returned |
| `TestR_BAS_039_TheDrainSpoolsTheBufferDuringAnOutageAndAnotherReplicaDeliversIt`, `…TheDrainSendsTheBufferWhenTheMainInstallationIsUp` (`internal/bastion/server`, unit) | the test added its events once the link said `Handshaken`; the link says so before its watchers pass `recordDownloads` on to the download buffer, so on a loaded runner the buffer discarded the events and the wait for a gauge of 3 ran out after 20 s (Test and Release 45211254958) | the test waits until the buffer records (`downloads.Reporter.Enabled`); the gauge is then 3 at once, since a failed flush removes nothing |
| `TestR_STOR_017_ADeployHoldsEveryAncestorOfItsPath` (`files`) | the deadlock check picked the test's DELETE when it waited later than a second after the deploy | the test's transaction retries a deadlock as a client does; three rounds for one that shows the recreation |
| `TestR_CI_003_StaleAndUnreachable` (`ci`) | 1.2 s and 6 s of sleep so that a copy of the key set passes `max_stale` and a fetch's throttle passes | the test sets `fetched_at` from its own clock, the replicas' clock, and waits until that instant plus `max_stale`, and until its last fetch plus `unknown_kid_refresh_interval`; a re-read of the stored set fetches nothing from the issuer (12.6 s to 4.1 s) |
| `TestR_CI_017_ARevocationReadsOnlyTheUnexpiredRecords` (`ci`) | the table's statistics read after waits of 2.5 s, against an idle window as long as the revocation | the revocation's transaction reads its own counts (`pg_stat_xact_user_tables`): 1 and 5 tuples where a scan reads 3,001 (16.5 s to 1.5 s) |
| the waits of 3 to 5 s for a change to reach a replica (`ci`: `declared`, `exchange_errors`, `exchange_mappings`, `issuers`, `mappings`, `use`) | a deadline of 3 to 5 s that is no bound of the product, for a poll of 500 ms | one change-log poll (`PollNow`) and the state asserted at once; the grant of a policy waits for each replica's grant index (`grantsSettled`); where the test stands for the bound of Q-CI-51, it waits one `cluster.epoch_poll_interval` after the replica's last read of the issuers and asserts once |
| `TestR_BAS_025_TheBudgetAndCollapsedMisses`, the budget (`bastion`) | the forwards slowed by 400 ms each and a queue timeout of 100 ms: which requests were served depended on the host | the forwards are held at the main installation until the ten requests beyond the two slots have answered `bastion_busy`; then the two are served (0.46 s to 0.19 s) |
| `TestBurstBesideEmptyCaches` (`overload`) | a fixed second before the downloads were read; 300 downloads queued for the five connections under the harness's retry bound of 3 s (150 of 355 waited 0.5 to 1 s at a load of 10) | no sleep: the admission has decided once every download is in flight or answered; the replica runs with the product's `database.retry.max_elapsed` of 30 s; a refused download prints its body and the pool's waits |
| `TestConcurrentWritersOnOnePath`, R-STOR-003 (`files`) | six writers for four seconds: the number of rounds was the host's | 80 rounds per writer (4.0 s to 3.7 s on the development machine) |
| `TestR_REPO_054_SlowDownWithoutAHintBacksOffExponentially` (`mirror`) | the sleep compared with the wait's floor; the engine sleeps what is left of the wait, 1.9986 s of 2 s | the wait's end against its floor, measured from the refused request, and the sleep against its ceiling |
| `TestR_VULN_015_*` (`vulnblock`) | statements counted from a window of the database's log (140 s for the two tests) | by request ID, as `querybudget` (28 s) |
| Every component test that chooses a person (`choosePrincipal` of [testing/screen.ts](../../web/src/app/testing/screen.ts): `policy-editor.spec.ts`, `project-screens.spec.ts`, `ci-editor-screens.spec.ts` and nine more) and the field's own spec (web, unit) | a fixed 320 ms for the search's pause of 250 ms and the answer; at a load of 53 to 120 the answer came later and the test found no match | the field's list is `aria-busy` from the pause until the lookup answers, and the test waits until no list is busy (`waitUntil`, `principalSearchAnswered`); the specs passed under `taskpolicy -c background` |

How a repair was shown to hold: the one test run with `GOMAXPROCS=1` under `taskpolicy -c background` (the lowest CPU class of macOS)
while the other builders' gates ran, never by starving the machine for everyone and never by a `-count` loop.

### Checks of a list or a convention

A check that fails when a change makes a list longer (a new format, a sealed column, a table that references a repository, a
revision column) runs where every builder runs it
([Q-ARCH-701](../spec/architecture.md#q-arch-701--a-check-of-a-list-or-a-convention-runs-where-every-builder-runs-it-the-unit-tier-or-the-area-conventions)):
in the unit tier when it needs no database (`TestR_API_053_EveryFormatNamesItsClients` in `internal/formats/all`,
`TestR_ARCH_021_TheReencryptionWalksEverySealedColumn` in `internal/app`), in the area
[conventions](../../test/integration/conventions/conventions_test.go) when it reads the migrated schema. That area builds like any
other and runs its tests in under a second on one clone of the template: every builder and every landing runs
`test/integration/run.sh ./test/integration/conventions/` beside the areas of the change. A new check of the kind goes there, not into
the area of the format or component that made the list.

### Which areas a changed answer needs

A change of an answer of the management API is followed by the areas whose tests name the changed member, whatever the change's
subject ([Q-ARCH-703](../spec/architecture.md#q-arch-703--a-landing-names-the-areas-whose-tests-name-a-member-the-change-made-to-an-answer)).
[apiareas](../../internal/tools/apiareas/main.go) lists them in about a second:

```sh
go run ./internal/tools/apiareas -base main                         # the working tree against main
go run ./internal/tools/apiareas -base 6b8eae484~1 -head 6b8eae484   # example: a change that landed
```

It prints one line per property a schema of `api/openapi.yaml` gained, lost or changed, with the areas whose test files name it as a
quoted string, and a last line `areas: …` for `run.sh`. On the change that put `disabled` into the consumer view (6b8eae484) it names
`transfer` among eleven areas, the one whose test then failed in shard b. A common name (`kind`, `id`) is named by many areas; the
line says how many, and the landing decides. It does not see a format's own routes, a key a test builds from parts, or an error code;
for those the search of the test tree for the changed field or code stays the rule.

### The unit run's time

`make test` runs every package under the race detector with the coverage of the whole module (`-coverpkg=./...`), eight
packages at a time on the CI runner (`GOMAXPROCS: 8`). Go ends a test binary after `-timeout`, 10 minutes by default; the
Makefile sets `UNIT_TIMEOUT` to 20 minutes. A package's time in that mode is what counts. The slowest packages in the unit job
of Test and Release 44926599027 (2026-11-21), in seconds:

| Package | Seconds | Note |
|---|---|---|
| `internal/api/v1` | 585 | 476 in the morning of the same day; one feature added 98 s, and the next commit took it past the default limit of 600 |
| `internal/formats/formattest` | 156 | |
| `internal/archtest` | 107 | loads every package of the module with types |
| `internal/app` | 105 | |
| `internal/formats/formattest/testformat` | 101 | |
| `internal/authz` | 87 | |

Why `internal/api/v1` cost ten minutes (from the `go test -json` report of that run): its 236 tests ran one after the other,
and 47 of them, each 2 s or more, took 517 of the 582 s. They waited on parsing the OpenAPI document (1.1 MB of YAML), not on
what they checked. A test that built a response validator (`apitest.New`: kin-openapi loads and validates the document) paid
about 8 s for it, and about 25 tests did. Every `newSetup` parsed the document again for the server (`LoadDocument`, about
0.85 s): `TestR_API_021_TheRegisteredRoutesCarryTheGate` built 56 servers and took 49 s. Each of the six signature tests that
came with the feature paid a validator and a parse (about 9 s), which is most of the 98 s the feature added; the other 41 s
were the existing tests' parses of a larger document. With both parses shared per test binary, the package took 56 to 58 s
in three runs in the job's mode on a 20-core workstation (`GOMAXPROCS=8`, load 7 to 9), against 560 s before on the same
machine; its slowest tests are now the ones that load a changed document on purpose and the ones that render the document.

What a test in `internal/api/v1` uses, so a new one costs milliseconds and not seconds:

| For | Use | Not |
|---|---|---|
| The embedded document as the server reads it | `testDocument(t)`: parsed once per test binary, read-only | `LoadDocument(EmbeddedYAML())` |
| A server with its chain | `newSetup(t, opts, authn)`: it takes the shared document | a router and `New` of its own, unless the test needs a different document |
| A response validator | `newValidator(t)`: a validator of the test's own (what it saw, `Uncovered`, is the test's) on a spec parsed once per test binary; `apitest.New(t, EmbeddedYAML())` shares the process's one parse as well ([Q-ARCH-580](../spec/architecture.md#q-arch-580--a-test-process-parses-the-api-document-for-its-response-validators-once)) | `apitest.Parse(EmbeddedYAML())` of its own |
| The document as kin-openapi reads it | `specDocument(t)` | `apitest.LoadDocument(EmbeddedYAML())` |
| A changed document (a load that must fail, an operation removed from the table) | `LoadDocument` or `apitest.LoadDocument` of the changed bytes: it pays a parse, so one per case that needs it | changing the shared document, which every later test reads |

The helpers live in [v1_test.go](../../internal/api/v1/v1_test.go) and [apitest.Spec](../../internal/api/v1/apitest/apitest.go);
the variables that hold the shared parses are in a `_test.go` file, so they are test state and not package state of a replica
([R-ARCH-032](../spec/architecture.md#r-arch-032--no-state-between-requests-on-one-replica-inv-18)). A test still builds a server per
caller where the caller is part of the setup (`authn`). A table of cases runs against one server where no case changes what
the server holds. The package's tests do not run in parallel (`t.Parallel`), because many share fakes and counters by design:
running them in parallel is a change of its own, test by test.

### Architecture test

[internal/archtest](../../internal/archtest) turns the structural rules of the specification into tests.
Each rule is a function over the package graph or the source tree, and each has a test that feeds it a
synthetic violation and asserts that the offending edge is named.

| Rule | Requirement | Function |
|---|---|---|
| A lower layer never imports a higher one; every package under `internal/` has a layer | R-ARCH-002 | `CheckLayers` ([layers.go](../../internal/archtest/layers.go) holds the table) |
| pgx only in `meta/postgres` and `cluster`; valkey-go only in `cluster`; the AWS SDK only in `blob/s3` | R-ARCH-002 | `CheckImportBans` |
| `database/sql` and the pgx stdlib adapter nowhere | R-ARCH-026 | `CheckImportBans` |
| Formats import no infrastructure package | R-ARCH-008 | `CheckModuleImportBans` |
| `internal/api/login` never imports `internal/api/v1` (edges inside one layer: the list `moduleBans` in [imports.go](../../internal/archtest/imports.go)) | R-ARCH-002 | `CheckModuleBans` |
| `http.Client{` and `net.Dial` only in `internal/outbound` and `_test.go` files | R-ARCH-002, R-ARCH-022 | `CheckOutbound` |
| No package-level variable of map, slice, pointer or `sync.Map` type outside `cache`, `cluster`, `observability` and the start-up registries | R-ARCH-032 | `CheckPackageState` |
| Nothing below `internal/blob` imports the Valkey client or uses a session-level advisory lock | R-STOR-046 | `CheckStorageValkey`, `CheckSourceBans` |
| `internal/proxy` and `internal/virtual` use no `pg_advisory*` call | R-ARCH-033 | `CheckSourceBans` |

To add a package: give it a layer in `layerTable`. A package under `internal/` without a layer fails the
test and says so. The trees `internal/archtest`, `internal/tools`, `test`, `tools`, `perf`, `web` and
`deploy` are not checked. The state check also leaves out the test support `internal/api/v1/apitest`, which no production package
imports and which keeps one parse of the API document per test process ([Q-ARCH-580](../spec/architecture.md#q-arch-580--a-test-process-parses-the-api-document-for-its-response-validators-once)).

Two behaviours of the state check are decisions, not accidents. Variables whose type is an interface (an
error sentinel, `var _ I = (*T)(nil)`) are not state of the forbidden kinds. A pointer to `regexp.Regexp`
is exempt, because a compiled expression is immutable; every other pointer is reported. Generated files
and variables with a `go:embed` directive are skipped.

The fixtures are small modules under [internal/archtest/testdata](../../internal/archtest/testdata);
they are real Go modules, so `go list` and the type loader run on them unchanged.

## Integration stack

`make integration-up` runs [test/integration/up.sh](../../test/integration/up.sh), which (for instance 1; another instance
uses its own directory, names and ports, [Instances and their ports](#instances-and-their-ports))

1. creates a throwaway CA and one server certificate per service in `.dev/integration/certs`
   ([certs.sh](../../test/integration/certs.sh); every certificate covers `localhost`, `127.0.0.1`, `::1`
   and the compose service names; openssl runs from `OPENSSL_IMAGE`, the script needs only Docker);
2. starts [test/integration/compose.yaml](../../test/integration/compose.yaml) with `docker compose up --wait`;
3. creates the buckets (`go run ./test/integration/cmd/seed`);
4. writes `.dev/integration/env`.

**Several stacks, each under its own lock.** The stacks belong to the machine: they are started in the main checkout, and every
worktree of the repository uses them. One stack takes one run at a time (see [Why one stack takes one run at a
time](#why-one-stack-takes-one-run-at-a-time)), so several instances of it can run side by side, each with a lock of its own
(see [Instances and their ports](#instances-and-their-ports)). [run.sh](../../test/integration/run.sh) is the one command for
a run, from the main checkout and from any worktree alike: it tests the current directory's code against the main checkout's
stacks.

```sh
make integration-up                    # instance 1, in the main checkout (the pipeline's stack)
make integration-up INSTANCE=2         # a second instance beside it

test/integration/run.sh ./test/integration/coordination/                                  # one area
test/integration/run.sh -run 'TestR_ARCH_027' ./test/integration/npm/ ./test/integration/nuget/   # several, with flags
test/integration/run.sh -timeout 60m ./test/integration/backup/                           # a longer bound
QUADER_TEST_INSTANCE=1 test/integration/run.sh ./test/integration/smoke/                  # pinned to instance 1
make integration-run ARGS=./test/integration/smoke/                                       # the same through make
test/integration/locked.sh make test-integration                                          # all, on a free instance
```

`run.sh` runs `go test -count=1 -p 1 -tags integration -timeout 30m <arguments>` through
[locked.sh](../../test/integration/locked.sh); `-timeout 30m` is left out when the arguments carry a `-timeout` of their own,
and the exit code is `go test`'s. `locked.sh` runs any command the same way. It finds the main checkout as the parent of
`git rev-parse --git-common-dir` and takes as candidates the instances whose env file exists there. It takes the first whose
lock is free without waiting; when every one is taken it tries them all again every two seconds, so it takes whichever becomes
free first, not a fixed one. `QUADER_TEST_INSTANCE=<n>` pins an instance, whose lock it then waits for without a timeout. It
says on stderr which instance it took and how long it waited, for example:

```text
locked.sh: 2026-11-23 04:07:30 took instance 1 (quader-it) after waiting 0 s (pid 12548): go test -count=1 -p 1 -tags integration -timeout 30m ./internal/qname/
locked.sh: 2026-11-23 04:07:32 waiting for a free instance of the integration stack (instances 1 2 are all taken)
locked.sh: 2026-11-23 04:07:40 took instance 2 (quader-it2) after waiting 8 s (pid 12604): go test -count=1 -p 1 -tags integration -timeout 30m ./internal/qname/
```

Then it loads that instance's env file and runs the command in the current directory. An instance's lock is an flock on
`<main checkout>/.dev/integration.lock` (instance 1) or `<main checkout>/.dev/integration-<n>.lock` (flock(1) on Linux,
lockf(1) on the descriptor on macOS); the file is never removed. The command inherits the locked descriptor, so the lock is held
until the command and every process that kept the descriptor (the test binaries and the quader processes they start) have
ended, also when the wrapper is killed. A bare `lockf <file> <command>` is not a substitute: it removes the file when its command
ends, so a process still waiting on the removed file and one that creates it anew both run, and a killed `lockf` leaves its
command running without the lock. A candidate is an instance whose env file exists: `make integration-down` removes it, but a
stack whose containers stopped some other way (a restart of Docker) keeps it and fails the run that takes it.

`make test-integration` reads the env file of `INSTANCE` (default `QUADER_TEST_INSTANCE`, then 1) of the main checkout by
itself, also from a worktree, but takes no lock: the pipeline starts one stack per job and runs it that way, unchanged.
The scripts' own variables carry the suite's prefix, because the tests start quader processes with the environment they
inherited and a quader process refuses every other `QUADER_` variable that is not a setting: `locked.sh` exports
`QUADER_TEST_INSTANCE` (the instance it took, which `make test-integration` and `rotate-cert.sh` then use) and
`QUADER_TEST_LOCK_HELD` (the lock file, so that a command it runs can call it again without waiting on itself), and reads
`QUADER_TEST_STACK_ROOT` (another checkout whose stacks and locks to use instead of the main checkout's). Every env file
carries `QUADER_TEST_INSTANCE` too, so a shell that sourced one by hand is pinned to that instance.

**Every connection is TLS (INV-20, R-HA-063).** PostgreSQL runs with `ssl=on` and a
[pg_hba.conf](../../test/integration/conf/pg_hba.conf) that has `hostssl` lines only, so a client without TLS is
refused. PgBouncer takes clients with `client_tls_sslmode = require` and talks to PostgreSQL with
`server_tls_sslmode = verify-full` against the stack's CA. The SeaweedFS S3 gateway serves HTTPS only
(`-s3.cert.file`, `-s3.key.file`); a plaintext request to its port is answered with a 400 by the TLS
server and never reaches the S3 handler. Valkey and the Sentinels were TLS-only already. The tests verify the
servers from the host (`sslmode=verify-full`, Quader's `ssl_mode: verify-full` with `ssl_root_cert_file`,
`storage.connections[].ca_file`) and no integration config uses `allow_insecure`, `allow_insecure_endpoint` or
`ssl_mode: disable`, with two deliberate exceptions that test the refusal itself: the Valkey test
"R-HA-056 the data node accepts TLS only" configures a client without TLS to prove it never connects,
and the PostgreSQL test "R-HA-015 ssl_mode disable without allow_insecure is refused" proves the
configuration check. Every `TestMain` of a suite calls `testenv.Main`, which fails the whole package before
any test when a `QUADER_TEST_*` endpoint is plaintext or unverified or the CA file is missing; the smoke tests
`TestPostgresIsTLSOnly` and `TestS3IsTLSOnly` prove the refusal against the running stack.

What is not covered: the SeaweedFS master, volume and filer talk to each other plaintext inside the one
container (loopback, not published on the host); the Unix socket of PostgreSQL inside its container
uses `trust` for the image's init and `pg_isready`; and the certificates are RSA 2048 signed for 365 days,
fine for a test stack and not a model for production.

`QUADER_TEST_VALKEY_LINE=9.0` or `9.1` (default `9.1`) selects the Valkey line. Switching the line recreates
the stack, because Sentinel rewrote the configuration of the data nodes.

| Service | Container (instance 1) | Host address (instance 1) | Notes |
|---|---|---|---|
| PostgreSQL 18 (the version under test, Q-ARCH-219) | `quader-it-postgres` | `127.0.0.1:15432` | user `quader`, database `quader`, superuser; data on tmpfs (`PGDATA` set in the compose file); TLS only (`hostssl`) |
| PgBouncer | `quader-it-pgbouncer` | `127.0.0.1:16432` | `pool_mode = transaction`, a catch-all database entry, `max_prepared_statements = 100`; the admin console is the database `pgbouncer`; TLS in (`require`) and out (`verify-full`) |
| Valkey primary, replica | `quader-it-valkey-primary`, `quader-it-valkey-replica` | `127.0.0.1:16379`, `:16380` | `port 0`, `tls-port`, `tls-replication yes`, ACL file with the users below |
| Sentinels | `quader-it-valkey-sentinel-{1,2,3}` | `127.0.0.1:26381` to `:26383` | TLS only, password-protected, monitor name `quader`, quorum 2, `down-after-milliseconds 3000` |
| SeaweedFS (S3 gateway) | `quader-it-seaweedfs` | `https://127.0.0.1:18333` | HTTPS only, path-style, static credentials; buckets `quader-blobs` (versioning on) and `quader-backups`; `-volume.max=100` (a bucket is a collection that grows several volumes, so a low count runs out after a few runs of the 500 MiB upload test; every suite removes what it wrote when its test ends, through `testenv.RemoveObjectsAtCleanup`, so a stack kept up through many runs keeps only what an aborted run left behind, and `-master.vacuumDeleteEmptyAfterSeconds=600` deletes a volume the vacuum left empty, which frees its slot; once all 100 volumes are full, every PUT answers 500 and every package after that point fails: `make integration-down integration-up` starts it empty), set in `compose.yaml` only: `make integration-up` is the one way the stack starts, locally and in CI. |

**How a client on the host follows a failover.** The five Valkey processes run in separate containers
that share one network namespace (the container `quader-it-net`), and every port is published on the host
under the same number. `replica-announce-ip` and `sentinel announce-ip` are `127.0.0.1`, so the addresses
that Sentinel returns are valid inside the stack (replication, monitoring) and on the host (the tests).
There is no name resolution to configure. A process can still be stopped on its own
(`docker stop quader-it-valkey-primary`), which is how a failure is simulated. Stopping `quader-it-net`
takes all five down.

**Valkey users** (test constants, in [conf/users.acl](../../test/integration/conf/users.acl)):
`quader` is the least-privilege user of R-ARCH-031's example rule; `bastion` is the same rule on the keys and channels below
`quader-bastion:` (the Bastion's own Valkey, R-BAS-034; a stack started before that line existed gets the user from
`test/integration/bastionshared`, which runs `ACL SETUSER` with the same rules on both nodes); `ops` may do everything and serves
replication, Sentinel monitoring and the tests' inspection; the `default` user is off. The Sentinels
carry the password from `.dev/integration/env`.

The environment file holds these variables; [test/integration/testenv](../../test/integration/testenv)
reads them and fails the test with a message that names the variable when one is missing. Integration
tests never skip.

| Variable | Meaning |
|---|---|
| `QUADER_TEST_INSTANCE` | the instance the file describes, 1 to 9 |
| `QUADER_TEST_CA_FILE` | absolute path of the stack's CA certificate (`.dev/integration/certs/ca.crt`) |
| `QUADER_TEST_CERT_DIR` | the directory of the stack's certificates and keys |
| `QUADER_TEST_PG_DSN`, `QUADER_TEST_PG_PGBOUNCER_DSN` | PostgreSQL direct and through PgBouncer, `sslmode=verify-full&sslrootcert=<CA>` |
| `QUADER_TEST_PG_HOST`, `_PORT`, `_PGBOUNCER_PORT`, `_USER`, `_PASSWORD`, `_DATABASE` | the same, split up |
| `QUADER_TEST_VALKEY_LINE` | `9.0` or `9.1` |
| `QUADER_TEST_VALKEY_SENTINELS`, `QUADER_TEST_VALKEY_NODES` | comma-separated `host:port` of the Sentinels and the data nodes |
| `QUADER_TEST_VALKEY_PRIMARY_NAME` | the Sentinels' monitor name, `quader` |
| `QUADER_TEST_VALKEY_USERNAME`, `_PASSWORD` | the least-privilege user |
| `QUADER_TEST_VALKEY_SENTINEL_PASSWORD` | the Sentinels' password |
| `QUADER_TEST_VALKEY_ADMIN_USERNAME`, `_ADMIN_PASSWORD` | the unrestricted user |
| `QUADER_TEST_VALKEY_CA_FILE` | absolute path of the CA certificate (the same file as `QUADER_TEST_CA_FILE`) |
| `QUADER_TEST_VALKEY_KEY_PREFIX` | `quader:`, the prefix the ACL rule allows |
| `QUADER_TEST_VALKEY_CONTAINERS` | container names, for `docker stop` and `docker start` |
| `QUADER_TEST_S3_ENDPOINT` (`https://127.0.0.1:18333`), `_REGION`, `_ACCESS_KEY`, `_SECRET_KEY`, `_FORCE_PATH_STYLE` | the S3 gateway |
| `QUADER_TEST_S3_BUCKET_BLOBS`, `QUADER_TEST_S3_BUCKET_BACKUPS` | bucket names |

`QUADER_TEST_` is a reserved prefix of the configuration loader (see
[configuration.md](configuration.md)), so sourcing the file in a shell does not make `quader` reject its
environment.

**Certificate layout** in `.dev/integration/certs` (keys are world-readable on purpose, the stack protects
nothing):

| Files | Used by |
|---|---|
| `ca.crt`, `ca.key` | the CA; `ca.crt` is `QUADER_TEST_CA_FILE` |
| `server.crt`, `server.key` | the five Valkey processes |
| `postgres.crt`, `postgres.key` | PostgreSQL (copied by [conf/postgres.sh](../../test/integration/conf/postgres.sh) to a directory owned by `postgres` with mode 0600, which the server insists on) |
| `pgbouncer.crt`, `pgbouncer.key` | PgBouncer, client side |
| `s3.crt`, `s3.key` | the SeaweedFS S3 gateway |

Every certificate has the SANs `localhost`, `127.0.0.1`, `::1` and the service names, so one name list
verifies from the host and between containers.

**Helpers for tests.** `testenv.CAFile`, `testenv.CertDir`, `testenv.RootCAs`, `testenv.TLSConfig` and
`testenv.HTTPClient` give a client that trusts exactly the stack's CA; `testenv.PgDSN(tb, database)` and
`testenv.PgBouncerDSNFor` build verify-full DSNs; `testenv.SecureDatabase` and `testenv.SecureS3` set
Quader's `database.ssl_mode`/`ssl_root_cert_file` and the object-store client's `endpoint`/`ca_file` (`testenv.Connection` writes them into an element of `storage.connections`). A new suite starts with
`func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }`.

**Rotating a certificate in place.** [rotate-cert.sh](../../test/integration/rotate-cert.sh)
`<postgres|pgbouncer|valkey|s3> [days]` re-issues that service's certificate with a new key and serial from
the existing CA and makes the running service serve it: PostgreSQL by `pg_reload_conf()`, PgBouncer by
SIGHUP, Valkey by `CONFIG SET tls-cert-file` on all five processes, and the S3 gateway by a container restart
(SeaweedFS reads the pair at start only; the script waits for health). The CA does not change, so clients
keep verifying. Pass a short `days` to test expiry. A test shells out to the script from the repository
root, as the Valkey tests do with `docker`. The script acts on the instance `QUADER_TEST_INSTANCE` names (every env file
carries it, and `locked.sh` exports it) and on the certificates of `QUADER_TEST_CERT_DIR`, so a test rotates the certificate of
the stack it runs against.

The smoke tests in [test/integration/smoke](../../test/integration/smoke) check one thing per
dependency: PostgreSQL 18 direct (including a session advisory lock), PgBouncer in transaction mode,
Valkey discovery through Sentinel over TLS with the ACL user, TLS-only ports, the example ACL rule
(`ACL LOG` stays empty, commands outside the rule are refused), two forced failovers that a host-side
client follows, and put, get and versioning in SeaweedFS with the AWS SDK. The failover test returns the
stack to its original layout by failing over twice.

Packages under `test/` may import pgx, valkey-go and the AWS SDK directly: the architecture test does not
check them.

`make integration-down` removes the containers, volumes and `.dev/integration/env` (`INSTANCE=<n>`: those of instance
`n` and `.dev/integration-<n>/env`); the certificates stay. A stack created before the per-service certificates existed is recreated by `make integration-up`
(`certs.sh` sees the missing files and issues a new set).

### Instances and their ports

`make integration-up INSTANCE=<n>` (`test/integration/up.sh <n>`) starts instance `n` of 1 to 9; without the variable,
instance 1. [instance.sh](../../test/integration/instance.sh) derives every name of an instance from its number:

| | Instance 1 | Instance n (2 to 9) |
|---|---|---|
| Compose project, container prefix | `quader-it` (`quader-it-postgres`, ...) | `quader-it<n>` (`quader-it2-postgres`, ...) |
| Volumes | `quader-it_<service>` | `quader-it<n>_<service>` |
| State (certificates, env file, Valkey line) | `.dev/integration/` | `.dev/integration-<n>/` |
| Env file | `.dev/integration/env` | `.dev/integration-<n>/env` |
| Lock | `.dev/integration.lock` | `.dev/integration-<n>.lock` |
| Host ports | the table below | instance 1's plus `(n - 1) * 100` |

Instance 1 is the stack as it was before instances existed: its compose configuration resolves to the same values, so its
containers keep their configuration hash and `make integration-up` does not recreate them. Each instance has its own CA. Start
every instance in the main checkout: `run.sh` looks for env files there only, and a second checkout that starts the same
instance number would take the same container names and ports.

The ports of every local profile of the repository, and the instances' (all on `127.0.0.1` unless noted):

| Profile | Service | Port |
|---|---|---|
| Integration stack, instance 1 | PostgreSQL, PgBouncer | 15432, 16432 |
| | Valkey primary, replica | 16379, 16380 |
| | Sentinels | 26381, 26382, 26383 |
| | SeaweedFS S3 | 18333 |
| Integration stack, instance 2 | PostgreSQL, PgBouncer, Valkey, Sentinels, S3 | 15532, 16532, 16479, 16480, 26481 to 26483, 18433 |
| Integration stack, instance n | the same | 15432, 16432, 16379, 16380, 26381 to 26383, 18333, each plus `(n - 1) * 100` (instance 9: 16232, 17232, 17179, 17180, 27181 to 27183, 19133) |
| UI development profile, instance 1 ([web-ui.md](web-ui.md#local-ui-development)) | Angular dev server, quader, ops listener, sign-in provider | 4200 (`localhost`), 19080, 19081, 19090 |
| | quader's scan listener, `vuln-scanner-trivy`, its ops listener, its loader's ops listener | 19082, 19083, 19084, 19085 |
| | PostgreSQL, Valkey, SeaweedFS S3 | 25432, 26390, 28333 |
| UI development profile, instance n (2 to 8, claimed by a checkout or named by `UI_INSTANCE`, [web-ui.md](web-ui.md#instances-one-per-builder)) | the same | each plus `(n - 1) * 100` (instance 2: 4300, 19180, 19181, 19190, 25532, 26490, 28433; instance 8: 4900, 19780, 19781, 19790, 26132, 27090, 29033) |
| Development profile on kind ([local-profile.md](local-profile.md)) | the edge (HTTPS NodePort 30443 of the kind node) | 8080 (`QUADER_DEV_HTTP_PORT`) in the main checkout, a port kind picks in a worktree ([local-profile.md](local-profile.md#one-cluster-port-and-kubeconfig-per-checkout)), and the kind API server on a port Docker picks |
| S3 conformance (`make silo-up`) | Silo S3, console | 9000, 9001 |
| In-process harness, e2e port-forwards, throwaway Valkey of the `valkey` area | every listener | a port the kernel picks (`:0`) |

The step of 100 keeps the instances apart from each other and from every port above. Instance 10 would take instance 1's
PgBouncer port (15432 + 900 + 100 = 16432), so the number stops at 9. All ports stay below 32768, under the ephemeral range of
Linux (32768 to 60999) and of macOS (49152 to 65535). The Valkey processes listen inside their shared network namespace on the
same numbers as on the host, so the addresses Sentinel announces stay valid on the host; a Sentinel derives the port of its
instance's primary from its own port by the same step ([conf/sentinel.sh](../../test/integration/conf/sentinel.sh)).

**What one instance takes.** Read with `docker stats --no-stream` on 2026-11-23 from instance 1 after two days up: SeaweedFS
1.36 GiB, PostgreSQL 403 MiB (its data directory is a tmpfs and counts here), the two Valkey data nodes 15 and 16 MiB, each
Sentinel 7 MiB, PgBouncer 3 MiB, about 1.8 GiB in all, idle CPU near zero. Docker Desktop's VM on the development machine has
15.8 GiB, and the UI development profile and the kind clusters take their share of it.

### Why one stack takes one run at a time

What a test does to a stack as a whole, so that two runs on one stack act on each other (read in the areas on 2026-11-23):

| Area, test | What it does to the whole stack |
|---|---|
| `valkey` (`TestFailover`, `TestFlightLockFailover`, `helpers_test.go`), `smoke` (`TestValkeyClientFollowsASentinelFailover`) | forces Sentinel failovers of the stack's one Valkey: every other run's clients lose the primary for seconds |
| `valkey` (`breaker_test.go`, `flight_test.go`) | `CLIENT PAUSE 3500 WRITE` on the primary: every write of every run waits |
| `valkey` (`failover_test.go`), `bastionshared`, `bastion` | `ACL SETUSER` on the Sentinels and data nodes; `smoke` resets the `ACL LOG` and expects it empty afterwards |
| `postgres` (`bookkeeping_test.go`), `audit` (`env_test.go`) | create and alter the server-wide role `quader_app` with a fixed name and password |
| every suite with replicas (pools of up to 8 connections each, plus lease sessions) | share the one server's `max_connections` (200, `compose.yaml`); two runs' pools together can use them up, and the next connection is refused |
| every suite | writes to the two buckets of the one SeaweedFS (each under its own prefix, removed at the end); the 100 volumes of SeaweedFS are shared, and a run that fills them fails every run after it |
| every suite on the harness (`testenv.NewStack`) | clones the stack's one migrated template of the day; a run of a checkout with other migration files makes the template of its own files and drops the other, so the next run of the first checkout makes its own again (one migration, about 0.6 s); a run that still clones a dropped template asks for the template again ([template.go](../../test/integration/testenv/template.go)) |
| the latency tests of `make test-performance` (`coordination`, `npm`, `api`, `deblocal`) | measure time on a stack whose containers another run would keep busy; the machine's CPU is shared by every instance, so an instance of its own does not take that away |

None of this but the CPU reaches past its own stack: a test reaches a stack only through the variables of the env file. A Valkey
process is reached through `QUADER_TEST_VALKEY_CONTAINERS`, the
certificates from `QUADER_TEST_CERT_DIR`, and every address from the env file; the throwaway Valkey of the `valkey` area has a
random name and port. So two instances take two runs at once.

## The in-process harness

A suite that exercises more than one component runs the production composition root, `app.New`, in the test
process through [test/integration/testenv](../../test/integration/testenv). The suite then meets the real chain,
the real authentication stage with real tokens, the real decision point and the real wiring; nothing in it is a
test double of production code. A suite of one component keeps its own environment on purpose: the authorization
engine, coordination, audit, PostgreSQL and the blob store (`test/integration/authz`, `coordination`, `audit`,
`postgres`, `blob`) test one component against the stack and have no use for a replica.

| Piece | What it is |
|---|---|
| `testenv.NewStack(tb, StackOptions{Name, Valkey, Env})` ([stack.go](../../test/integration/testenv/stack.go), [template.go](../../test/integration/testenv/template.go)) | One installation: a database `quader_<Name>_<random>`, a clone of the migrated template of the day with identity rows of its own (below), dropped with the test, an S3 prefix `it/<database>` in the stack's bucket (`ObjectPrefix()`), removed with every version when the test ends, the shared environment of every replica (TLS to PostgreSQL, S3 and Valkey; short health and heartbeat timings; no drain delay, `health.shutdown_drain_delay` `0s`, which a test of the drain overrides in `Env`; the management API's rate limits off) and a validator of the OpenAPI document of the stack's own, over the one parse of the document the test process keeps (`apitest.New`, [Q-ARCH-580](../spec/architecture.md#q-arch-580--a-test-process-parses-the-api-document-for-its-response-validators-once)). `Valkey: false` points the Sentinel at `127.0.0.1:1`, the state "Valkey stopped" (degraded mode), and sets `valkey.startup_answer_timeout` to 0, so its replicas do not wait 30 s for an answer that cannot come. `Env` is appended to every replica's environment. With `QUADER_TEST_PG_POOLED=1` the replicas' main pool goes through PgBouncer |
| `Stack.Start(tb, ReplicaOptions{Name, Role, Env, Endpoints, Extra, Database, UI})` ([replica.go](../../test/integration/testenv/replica.go)) | `app.New`, `Start`, waits for `Started` (a bootstrap error ends the wait at once with its text, through `App.Failed`), and shuts the replica down in the cleanup of the test, printing the tail of its log when the test failed. `Role` is `web` (default), `worker`, `scanner` or `all`; a `worker` names a scanner at `http://127.0.0.1:1`, which nothing answers on, unless its `Env` names another (R-VULN-041: a worker without one is refused while the image scanning is on, and a scan it runs is put off with `scanner_unavailable`); `Endpoints` is the JSON of `server.endpoints`; `Extra` registers more routes through `app.Options.Extra` (a route below `/api/v1/` must be in the document, so a test resource lives elsewhere); `Database` adds options to the replica's database pools through `app.Options.Database` (tests only), such as the tracer below; `UI` replaces the UI tree (`app.Options.UIAssets`), and without one the replica serves the committed placeholder page alone, as the pipeline's binary does, also in a checkout that built the UI (`uiassets` passes its own trees). A stop is immediate: the stack's drain delay is `0s`, and `StackOptions.Env` or `ReplicaOptions.Env` set another, which wins ([Q-ARCH-582](../spec/architecture.md#q-arch-582--a-replica-of-the-in-process-harness-drains-for-no-time-and-serves-the-placeholder-ui-unless-its-test-says-otherwise)) |
| `testenv.NewRequestTransactions()`, `Option()`, `Count(on)`, `Of(requestID)`, `PerRequest(ids)`, `SQL(name)` ([requests.go](../../test/integration/testenv/requests.go)) | Counts, inside the replicas given its `Option()` and between `Count(true)` and `Count(false)` (outside that the tracer reads one flag, so the requests a test does not measure are not slowed), the transactions and statements each HTTP request ran, by the request ID its context carries (`httpserver.RequestID`, the answer's `X-Request-Id`). It is the pgx tracer of the request pool and the control connection: a statement of the replica's own loops or batched writers has no request ID and is not counted, and nothing depends on when PostgreSQL publishes its statistics. A transaction is counted as PostgreSQL counts one (an explicit one from `BEGIN` to its end, a statement or a batch outside one), and `Of` returns each with the names of its statements, so a test can take out the transactions of named statements exactly; the pool's liveness ping and the connection set-up are not statements of the tracer. `PerRequest` counts a name as a statement of every request when it ran in nine requests of ten, by its runs per request, and `SQL` gives the text of a name a request ran (the budgets of `querybudget` and `vulnblock`, Q-ARCH-700; no test reads the database's log). The performance run reads V-5b with it ([perf/README.md](../../perf/README.md#reading-a-run)) |
| `Replica.Addr(endpoint)`, `OpsAddr()`, `Metrics(tb)`, `Stop()`, `App` | Listener addresses, the ops listener's `/metrics`, a graceful stop (idempotent; it is how a test removes a replica), and the `*app.App` for the accessors the composition root exposes (`Queue`, `Repositories`, `RepositoryService`, `Valkey`, `Blobs`, `Changes`) |
| `Stack.Do(replica, method, path, token, body, hdr...)`, `DoAt(addr, host, ...)` | One request with a bearer token and the Host of the endpoint; answers of `/api/v1/` are validated against the document and `Validator().Uncovered(...)` lists the operations a suite did not reach |
| `Stack.User(name, groups...)`, `ServiceAccount`, `Bind(role, person)`, `Token(person, scope)`, `Repository(key, class, format, cfg)`, `Policy(...)` ([seed.go](../../test/integration/testenv/seed.go)) | Rows written straight into the database **before** the first replica starts, so that every replica builds its first grant index with them. A token is minted the way `token.Service` stores one (the SHA-256 of the value, never the value) and is a real `qdr_` token, so the request meets the production authentication stage. A token exercises the capabilities its scope names that the owner's roles grant (R-AUTHZ-013), so a scope that should read settings names `settings:read`. `Policy` fails the test when it names no repository or a key that resolves to none (a bare key of another project's repository, a call before the repository exists): such a policy selects nothing and grants nothing, and a test that expects a refusal would pass for that alone |
| `testenv.DefaultProject` (`it`), `Stack.Project(key)`, `Stack.Member(project, person, role)`, `Stack.RepositoryIn(project, key, class, format, cfg)`; `testenv.ProtocolURL(format, project, key)`, `FilesURL(project, key)`, `ManagementURL(project, key)` ([names.go](../../test/integration/testenv/names.go), [seed.go](../../test/integration/testenv/seed.go)) | Every repository belongs to a project: `NewStack` creates the project `it`, and `Repository` puts a bare key into it (a key `<project>/<key>` into that project, which `RepositoryIn` names explicitly; both create a missing project). `Member` writes a membership row with its role (`reader`, `developer`, `maintainer`, `owner`). The URL helpers render `/api/<format>/<project>/<key>/`, `/files/<project>/<key>/` and `/api/v1/projects/<project>/repositories/<key>` |
| `Stack.Exec`, `Text`, `Number`, `Query` | SQL on the installation's database as its owner, for state a test sets up or asserts. Rows the product writes (audit events, journal rows, the change log) are read here instead of through a recorder double |
| `testenv.RemoveObjectsAtCleanup(tb, bucket, prefix)`, `Leftovers(tb, bucket, prefix)` ([cleanup.go](../../test/integration/testenv/cleanup.go)) | The removal of a test's own prefix when the test ends: the unfinished multipart uploads, every object version and every delete marker below `<prefix>/`. The blob bucket keeps every version, so a deletion through Quader's object store only writes a delete marker and the bytes stay; a suite that gives an object-store client a prefix of its own calls it once, before anything writes there, and `NewStack` calls it for `it/<database>`. A removal that fails fails the test. `Leftovers` counts versions, delete markers and uploads below a prefix, for the test of a cleanup |
| `testenv.ChildEnv(extra...)` ([childenv.go](../../test/integration/testenv/childenv.go)), `testenv.FreeAddr(tb)` ([freeaddr.go](../../test/integration/testenv/freeaddr.go)) | The environment of every process a test starts (a quader process, a `go build`, a third party's suite): the test process's own without the caller's `QUADER_` variables, then what the test names; `TestNoIntegrationTestReadsTheCallersWholeEnvironment` fails on any other read of `os.Environ()` in the tree. A loopback address no other call of the process got, for a listener started later; `TestFreeAddrHandsNoAddressOutTwice` |
| `Stack.Environ()` | The environment every replica of the stack shares, for a suite that runs the binary instead of `Stack.Start` ([overload](../../test/integration/overload): a memory limit is per process) |

**The template every stack clones** ([template.go](../../test/integration/testenv/template.go),
[Q-ARCH-581](../spec/architecture.md#q-arch-581--every-stack-of-the-in-process-harness-is-a-clone-of-a-migrated-template-with-identity-rows-of-its-own)).
A stack does not run the migrations: its database is `CREATE DATABASE … TEMPLATE quader_ittpl_<day>_<hash>`, a template the stack's
PostgreSQL holds for the embedded migration files of the day (`<day>` is the UTC date as `YYYYMMDD`, `<hash>` 16 hex digits of the
SHA-256 over the files and a format number of the harness). The first stack of a test process that finds none makes it, under a
session advisory lock in the database `postgres`, so that two test binaries that start at once make it once: it migrates
`<name>_build`, marks it a template that takes no connections and renames it to its name last, so a template of the name is complete
or absent; a build whose migration fails is dropped. It then drops the harness's other templates (another hash, an earlier day, a
build a killed process left), recognised by the whole name and the database's comment, never another database. After the clone,
`NewStack` draws `installation_id`, `keyspace_id` and `incarnation` of `system_state` again, as the first migration draws them: two
stacks never share a Valkey keyspace. What a clone keeps from the template's migration (timestamps up to a day old, the ids of the
built-in limit profiles, the grants to `quader_app`) is [H-1470](../spec/architecture.md#h-1470--a-stack-of-the-in-process-harness-carries-what-the-templates-migration-wrote-with-the-time-of-its-run).
A changed migration during development changes the hash: the next stack makes a new template and the old one is dropped; nothing has
to be cleaned by hand. Between runs each instance's PostgreSQL holds one template, about 17 MB. What tests the migrations or restores
into fresh databases does not use it: the area `postgres`, the restore targets of `backup` (its stacks on the harness clone the template),
and the areas with databases of their own (`uploads` with its own template, `authz`, `audit`, `coordination` and the others the paragraph
above names).

The suites on the harness: [transfer](../../test/integration/transfer) (the HTTP surface of the files transfer and the
repository operations, with the users and grants of `webenv_test.go`),
[endpoints](../../test/integration/endpoints) (three replicas behind a dispatcher, Valkey
running and stopped, requests on the default and the further endpoints), [api](../../test/integration/api) (the
conventions on a test resource registered through `Extra`, the cluster-wide rate limits, the system operations with a
web replica that enqueues and a worker replica that executes, key rings that differ between replicas),
[apikey](../../test/integration/apikey) (the API-key credential on a test format whose push, delete and relist declare
`Route.APIKey`: the stage, the dispatcher's match, the audit, the request log and the metrics), and
[repository](../../test/integration/repository) (the service, the snapshot, the deletion job on a worker). The
repository suite builds one component next to the replicas, the counting reader of `probe_test.go`, because the number
of transactions a reader starts is visible only on its runner. Suites that need a seam inside a component keep the
component environment of their package on purpose: the storage hooks, the counting and delaying object store, the
recorder that fails and the spool of [files](../../test/integration/files) (`commit_test.go`, `races_test.go`, `scan_test.go`, `transfer_test.go`); that
package also registers a format with a data port of its own, which the composition root refuses to start with because
only the root binds data ports, so a replica cannot run in it; the suites that need one are in `transfer`,
the catalog, routing and recorder of the claims and invalidation tests of `repository`, and the upstream-test operations of
`api`, which serve the management API over an upstream client of their own (`component_test.go`). A new format suite uses
only the harness.

[bastion](../../test/integration/bastion) is the harness with a TLS front of its own in front of the web replica and in-process Bastion replicas next to it (the role started from a
configuration file, the volume or the bucket backend, with or without Valkey); [developer/bastion.md](bastion.md#the-integration-suite-r-bas-048) describes it. It runs in
shard `c` of `make test-integration` (about 90 s).

The OCI registry's suites use the harness as well: [oci](../../test/integration/oci) (the data port against PostgreSQL, the protocol
criteria over `/v2/` on three replicas, the action map of R-AUTHZ-007, the registry with Valkey stopped, and the conformance runner behind
the build tag `ociconformance`), [ocilocal](../../test/integration/ocilocal) (tags in pages, referrers, a raw delete with its cascade, the
sweep, the replay and the reinstate), [ociremote](../../test/integration/ociremote) and [ocivirtual](../../test/integration/ocivirtual)
(against the counting stub registry [test/ocistub](../../test/ocistub) in the test process), [registry](../../test/integration/registry)
(the walk through the endpoint's handler, the version check, the catalog, the bound host), [uploads](../../test/integration/uploads) (the
upload sessions against PostgreSQL and S3 on three replicas), and the OCI files of [mirror](../../test/integration/mirror) (`oci_test.go`)
and [identity](../../test/integration/identity) (`registrytoken_test.go`). `ocilocal` and `mirror` run in shard `a`, `ociremote` in shard `c`, `ocivirtual` in shard
`d`, `oci`, `registry`, `uploads` and `identity` in shard `b`; `make oci-conformance` builds and runs the distribution conformance suite after `make test-integration` in shard `c`
([oci.md](oci.md#the-protocol-tests-and-the-conformance-suite)).

The Debian format's suites use the harness too: [deblocal](../../test/integration/deblocal) (upload, coordinates, deletion and replay, the index
builder on the real publication port, three replicas, the time rules, the action map of R-AUTHZ-007, the crafted inputs over HTTP, real `apt-get`
clients in docker, and the delay bound that `make test-performance` enforces), [debkeys](../../test/integration/debkeys) (the signing keys and the
reindex), [debremote](../../test/integration/debremote) (the remote class against the counting stub archive [test/debstub](../../test/debstub) in the test
process) and [itempublish](../../test/integration/itempublish) (the publication port), in shards `a` (`deblocal`), `b` (`debkeys`,
`itempublish`), `c` (`debremote`) and `d` (`debvirtual`), and the Debian files of
[mirror](../../test/integration/mirror) (`deb_test.go`, shard `a`), [querybudget](../../test/integration/querybudget) (`deb_test.go`) and
[postgres](../../test/integration/postgres) (`deb_test.go`, `deb_reseal_test.go`) ([deb.md](deb.md#tests)).

The signing identity's suite [signing](../../test/integration/signing) (signatures.md R-SIG-046 to R-SIG-048, R-SIG-055) starts a web replica
and a worker per test with an announce period and a reload interval of a second: the identity of the files loaded at the worker's start,
rotated and refused by writing new files and swapping a link as the kubelet swaps a volume, and the uploaded identity through the API with
a CA of the test ([signatures.md](signatures.md#stage-2-quader-signs)). It runs in shard `d` by the rule of Q-ARCH-565; the replay of a
withdrawn identity is in [backup](../../test/integration/backup) (`journal_signing_replay_test.go`, shard `c`).

Three things a suite should know. A replica's start is a real start-up: a principal that is named like the break-glass
account (`auth.break_glass.username`, default `admin`) is refused (the `files` and `api` stacks move the account
or name the user otherwise). The audit table can be made to refuse an insert with
a trigger (`failAudit` in the repository suite), which is how a test shows that a change never exists without its audit
record without a double of the recorder. The scheduler of a real worker enqueues a task's first run on its own, so a
test that triggers a task by hand accepts the `409` that names the scheduled run.

## Known gaps

- The CI workflow [release.yml](../../.github/workflows/release.yml) calls these targets; it has not run on
  GitHub yet. See [releasing.md](releasing.md#not-in-place-yet).
- `make lint` reports findings in packages that other work packages own (`internal/tools/coverage`);
  they are not hidden by an exclusion.
