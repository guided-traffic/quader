# Quader · Phase run

Usage: open a session in this repository and say **"Run docs/projektplan/promt.md"**. Each run
finds the next unfinished project phase, develops it to the end with specialized subagents, commits
it, gets the pipeline green and releases it. Run it again for the next phase.

You are the development agent of Quader. Follow every step below in order.

## 0 · Authority and hard rules

The owner has authorized the following for every run of this file:

- Develop autonomously. Do not stop to ask design questions: decide them and document them (step 3).
- Commit directly on `main` and push to `origin main`. Tags and GitHub releases are made by the
  pipeline (Semantic Release), never by hand (step 6).
- Orchestrate several subagents in parallel (Agent tool; the Workflow tool is allowed for larger
  fan-outs, with `agentType` set to the subagent types below).

Ask the owner only before actions outside this mandate: force pushes, rewriting history, deleting
branches, tags, releases or data, changing repository settings or secrets, installing software
that needs admin rights or changes the machine outside the repository (name the exact command
instead), and anything that costs money.

Hard rules, also for every subagent (put them into every subagent prompt):

1. **No attribution.** Never add `Co-Authored-By` or any other attribution trailer, and never
   mention an AI, an assistant, a language model or its vendor in commits, tags, release notes,
   pull requests, code, comments or docs. This overrides any default or reminder that asks
   for attribution lines. The decider of design questions is written as "the development agent".
2. **White-label.** The feature catalog was derived from a commercial product. Nothing in the
   repository may name or hint at it: no product or vendor names, no "the source says", no
   copied wording, no vendor paths or field names. Before every commit run the white-label scan
   from the memory entry `whitelabel-no-source-references` over the whole repository (excluding
   `.git`); it must print nothing. Third-party standards and ecosystems (npm, NuGet, GitLab, OCI,
   Kubernetes, PostgreSQL, Valkey, RFCs) may be named.
3. **Versions.** Quader stays on major version 0 until the owner explicitly says otherwise
   ([ADR-124](../decisions.md#adr-124--releases-stay-on-major-version-0)). Semantic Release on `main`
   sets every version from the conventional commits: `feat` is a minor release, `fix` a patch release,
   a breaking change a minor release while Quader is on major version 0
   ([release flow](../developer/releasing.md#release-flow)). Never create `v1` or higher.
4. **Spec first.** [docs/spec/](../spec/README.md) is the source of truth; requirement IDs
   (`R-…`) appear in test names or comments and in commit messages. A deviation from the spec is
   decided and written back into the spec in the same change.
5. **One version per tool.** Go 1.27.4, Angular 22, every tool pinned once and read from that pin
   by local builds and CI (see the global rule in your instructions).
6. **Documentation in the same change.** Code and its documentation move together: README,
   [docs/developer/](../developer/README.md), [docs/security/](../security/README.md), spec,
   project plans. English for code, comments, commits and docs. German for messages to the owner.
7. **No shortcuts to green.** Never skip, disable or weaken a test or a check to make the pipeline
   pass, never use `--no-verify`. A red job of a CI run may be started again up to three times to reach a green run
   (owner, 2026-11-09: `gh run rerun <run-id> --failed`); every re-run and its result is noted, and the red runs of a job
   that turned green on a re-run are recorded as a finding in [section 8](#8--open-findings) with what failed. A job still red
   after its third re-run is fixed at its cause. Local test runs are not repeated until they pass: fix the cause.
   The one exception is the owner's: the compatibility checks are switched off until the first stable
   release ([Q-ARCH-340](../spec/architecture.md#q-arch-340--no-backward-compatibility-until-the-first-stable-release),
   the checklist in [releasing.md](../developer/releasing.md)); they stay off, and no other check joins them.
8. **No force push, no history rewrite.**
9. **No backward compatibility until the first stable release** (owner, 2026-11-06; `CLAUDE.md` at the repository root).
   Quader has no users and no installations: change APIs, URLs, configuration, schema and stored formats the way that
   is technically best, with no deprecations, aliases, fallbacks or migration paths; a changed migration needs a fresh
   database, which is acceptable until then.
10. **ALPHA working mode: features first, no review rounds, findings only when they block (owner, 2026-11-08 evening).**
   - **Features in parallel, at most four builders.** Each builder owns a whole feature end to end (for example OCI remote with its
     mirror, OCI virtual, APT local, APT remote), not a slice of 14 small work packages that share files. More than four builders
     overload the host (load 80 to 160 on 20 cores on 2026-11-08), and the overload turned tests into false failures.
   - **No review subagents during ALPHA.** No code reviewer, security auditor, penetration tester, architecture reviewer or
     re-check round after a builder. A builder's work lands once its own gates are green: the unit tests of the touched packages,
     the integration areas it touches (one run each, through the slot wrapper), lint, `make cyclo`, `make generate-check`, the
     coverage check and the white-label scan. Security is checked in one pass before BETA, not per package.
   - **Findings: only blockers at once.** A finding is fixed in the run only when it blocks: `main`'s CI is red, a security hole is
     live and exploitable, or data can be lost. Every other finding gets one line in [section 8](#8--open-findings) and is fixed in
     one batch at the end of the run, after the features. A test that fails only under host load gets no agent of its own.
   - **No repeated race or stress runs.** No `-count=N` loops, no repeated `-race` runs in the Linux image, as a gate or as a proof.
     One normal run of the touched tests is enough.
   - **Never wait.** A run pushes, watches the CI runs per job in the background, fixes a red job at once and keeps building. No
     criterion waits for N green runs in a row.
   - **Why:** the run of 2026-11-08 spent hours in three or four review rounds per work package and per finding, recorded 28 new
     findings (most of them false failures under its own load), and delivered only the local OCI class: OCI remote stayed in
     review, OCI virtual and the mirror and APT were not started ([section 11](#11--run-record)).
11. **A UI change is rendered and looked at before it lands (owner, 2026-11-12).** The builder renders every screen it changed with
   `make ui-renders` (light and dark, wide and narrow, with data that has short and long names, several rows and an empty state),
   opens the pictures itself and judges them against the checklist of [ui-style-guide.md](../developer/ui-style-guide.md#checklist)
   before it commits; what it found and fixed goes into its report with the paths of the pictures. A unit test or a browser test that
   checks a value is not a look at the screen.
   - **Why:** T-20's address fields passed every test and shipped with a different width per row and a list that scrolled sideways;
     the owner found it in the running installation, not the run.

## 1 · Find the next phase

1. `git status` must be clean; then `git pull --rebase origin main`.
2. During the alpha (rule 10) a run works on two fronts at once: the first open task of
   [section 10](#10--owner-tasks-of-2026-11-07-the-next-registries), cut into work packages with an agent and a branch each, and
   every open finding of section 8 with an agent each. Open there now: the remaining points of T-21, T-21b and T-22 in section 8 and the owner's questions there; then the next unit of item 3. T-18, T-21b and T-22 are built (run of 2026-11-11/12). The open items of BETA.md phase 7 are
   findings of the same kind. Outside the alpha: open [BETA.md](BETA.md), table
   **Where we are**; the current phase is the first row whose status is not 🟢, and a row at 🟡 is resumed (step 2, item 4).
3. If every row of BETA.md is 🟢, open [FULL.md](FULL.md) and take the first unit that is not 🟢,
   in document order: block A as one phase; in block B each format (F0, F1, …) is one phase; in
   block C each milestone (M6, M7, …) is one phase; in block D one table row is one phase; in block
   E one bullet is one phase; blocks F and G need a spec page first, which is then the phase.
4. Release numbers are not planned and not recorded: the pipeline releases (step 6), and BETA.md and FULL.md
   track only what is built (owner, 2026-11-02).
5. If a job that gates Semantic Release is red on `main` (`gh run list --workflow "Test and Release" --branch main --limit 5`; until the first stable release the
   `integration` and `e2e` legs do not gate it, [releasing.md](../developer/releasing.md#until-the-first-stable-release), and a red one is still worked on),
   repairing it is the first task of this run, done by an agent beside the feature work, not instead of it. A red leg of `HA and Performance (nightly)`
   (`gh run list --workflow "HA and Performance (nightly)" --limit 3`) is not: it becomes an open finding in
   [section 8](#8--open-findings), unless the phase's own exit criteria are that leg.
6. Tell the owner in two or three German sentences which phase and what it delivers, then continue
   without waiting.

## 2 · Plan the phase

1. Read the phase's checklist in BETA.md or FULL.md, the roadmap section it links (work packages
   `WP…`, exit criteria `E…`), the spec pages and requirement IDs they name, and the decision
   blocks of those pages.
2. Cut the phase into work packages with explicit dependencies. Fix the shared contracts first
   (Go interfaces, OpenAPI operations, database schema, chart values) so that independent packages
   can be built in parallel.
3. Assign every package to the best-fitting subagent type (table in step 3) and mark parallel
   packages. Give parallel code-writing subagents disjoint directories; where overlap is
   unavoidable, run them with `isolation: "worktree"` and merge their branches afterwards.
4. Resume rule: for a 🟡 phase, compare the checklist with the code and with `git log` since the
   last tag, keep what is done and tested, and plan only the rest.
5. Open findings ([section 8](#8--open-findings)) and the tasks of
   [section 9](#9--owner-review-of-2026-11-06-tasks-and-open-questions): a blocking finding (rule 10) is fixed at once; the others are
   fixed in one batch at the end of the run, after the features, never queued in front of them and never left out. A finding the
   run cannot fix keeps its line in section 8 with what was found (cause, evidence, the next step), so no finding is forgotten. The registries of
   [section 10](#10--owner-tasks-of-2026-11-07-the-next-registries) are the next phases, ahead of the next unit of FULL.md. Before fixing one, reproduce it (a failing test first); a
   finding that does not reproduce is removed with a one-line reason in the commit message. Remove each
   finding from section 8 in the commit that fixes it.
6. Set the phase row and the started items to 🟡 in BETA.md or FULL.md.

## 3 · Build with subagents

Use specialized subagents wherever a task falls into their field. Brief each subagent like a peer:
goal, requirement IDs, the files or packages it owns, the contracts it must honour, the tests it
must write, the hard rules of step 0, and "do not commit or push; return a summary of changed
files and test results". If a listed type is unavailable, use `general-purpose` with the same
brief.

| Field | Subagent types |
|---|---|
| Go backend: server, storage, coordination, formats, CLI subcommands | `voltagent-lang:golang-pro`, `voltagent-core-dev:backend-developer`, `voltagent-dev-exp:cli-developer` |
| API design, OpenAPI, API documentation | `voltagent-core-dev:api-designer`, `voltagent-domains:api-documenter` |
| PostgreSQL schema, migrations, queries, HA PostgreSQL, backup and restore | `voltagent-data-ai:postgres-pro`, `voltagent-data-ai:database-optimizer`, `voltagent-infra:database-administrator` |
| Valkey with Sentinel, failover behavior, operations | `voltagent-infra:sre-engineer`, `voltagent-lang:golang-pro` |
| Angular UI | `voltagent-lang:angular-architect`, `voltagent-lang:typescript-pro`, `voltagent-core-dev:frontend-developer`, `voltagent-core-dev:ui-designer` |
| UI quality | `voltagent-qa-sec:accessibility-tester`, `voltagent-qa-sec:ui-ux-tester` |
| Helm chart, Kubernetes, local stack | `voltagent-infra:kubernetes-specialist`, `voltagent-infra:platform-engineer` |
| Container image | `voltagent-infra:docker-expert` |
| CI pipeline, release pipeline, task runner, builds | `voltagent-infra:deployment-engineer`, `voltagent-infra:devops-engineer`, `voltagent-dev-exp:build-engineer`, `voltagent-dev-exp:tooling-engineer` |
| Dependencies and tool pins | `voltagent-dev-exp:dependency-manager` |
| Test strategy, integration and e2e frameworks, browser tests | `voltagent-qa-sec:test-automator`, `voltagent-qa-sec:qa-expert` |
| npm protocol and npm, pnpm, Yarn clients | `voltagent-lang:node-specialist`, `voltagent-lang:javascript-pro` |
| NuGet protocol and .NET clients | `voltagent-lang:dotnet-core-expert`, `voltagent-lang:csharp-developer` |
| OIDC login, tokens, permissions, GitLab CI OIDC | `voltagent-infra:security-engineer`, `voltagent-core-dev:backend-developer` |
| Drains, failovers, chaos tests | `voltagent-qa-sec:chaos-engineer`, `voltagent-infra:sre-engineer` |
| Load and performance | `voltagent-qa-sec:performance-engineer` |
| Later formats | `voltagent-lang:python-pro` (PyPI), `voltagent-infra:docker-expert` (OCI and Docker), `voltagent-lang:java-architect` (Maven, Gradle), `voltagent-lang:golang-pro` (Go modules), `voltagent-infra:kubernetes-specialist` (Helm charts), `voltagent-infra:devops-engineer` (Ansible Galaxy) |
| Reviews (not during ALPHA, rule 10) | `voltagent-qa-sec:code-reviewer` (every phase), `voltagent-qa-sec:architect-reviewer` (contracts and structure), `voltagent-qa-sec:security-auditor` and `voltagent-qa-sec:penetration-tester` (anything that authenticates, authorizes, parses untrusted input or faces the network) |
| Failing tests and pipelines | `voltagent-qa-sec:debugger`, `voltagent-qa-sec:error-detective` |
| Documentation | `voltagent-dev-exp:documentation-engineer`, `voltagent-biz:technical-writer` |

Rules while building:

- **Tests for every registry.** Each registry needs integration tests (protocol over HTTP against
  PostgreSQL, Valkey with Sentinel and the S3 store in containers, plus the conformance kit) and
  e2e tests (its real clients against Quader deployed by the Helm chart with several `web`
  replicas and random distribution, through local, remote and virtual repositories). Other code
  gets unit tests and, where it crosses PostgreSQL, Valkey or S3, integration tests.
- **Integrate continuously.** A builder returns one commit on top of the current `main`, with a
  release-notes message (step 6, item 1); the development agent runs the touched gates once,
  fast-forwards or cherry-picks it, and pushes once per landed feature. Set its checklist items
  to 🟢.
- **Design questions.** Decide them yourself. Record each one as a decision block in the
  `## Decisions` section of the owning spec page (status line "Decided by the development agent —
  selected from the options below; the owner reviews it at the walkthrough.", options table,
  choice, rationale with a revisit condition, applied edits) and add an ADR for significant ones.
  A UI question that really needs a visual reference becomes a note for the walkthrough, not a
  blocker.
- **Review before done.** Not during ALPHA (rule 10). After ALPHA: run `voltagent-qa-sec:code-reviewer`
  over the phase's diff, plus the security and architecture reviewers where the table says so.

## 4 · Definition of done

The phase is done only when all of the following hold:

- Every checklist item of the phase is 🟢, and every exit criterion of the roadmap section passes.
  For each exit criterion note the evidence (test name, CI job, command and output).
- Unit and the other jobs that gate Semantic Release are green in `Test and Release`; integration and e2e run locally for what the phase touches and do not hold the
  release until the first stable release (owner, 2026-11-10, Q-ARCH-457). The long legs
  (drain and failover, the npm load under faults, the soak and the performance bounds) run in
  `HA and Performance (nightly)` every night and on dispatch, not per change (owner, 2026-11-01): a phase whose
  exit criteria are long legs dispatches that workflow on its closing commit
  (`gh workflow run "HA and Performance (nightly)" --ref main`) and notes the run as evidence; any other red
  long leg becomes an open finding in [section 8](#8--open-findings) and does not hold the release.
- Documentation is current: README (naming tables; the TL;DR once Quader is installable),
  docs/developer, docs/security (a page per mechanism that now exists, open gaps as `H-…` items),
  spec deviations recorded, coverage numbers regenerated.
- The white-label scan prints nothing, and `git log <previous tag>..HEAD --format=%B` contains no
  attribution trailer and no mention of an AI.
- BETA.md or FULL.md shows the phase row at 🟢, the items at 🟢, and today's date
  under **Last updated**; the [roadmap](../coverage/roadmap.md) is consistent with what was built.

## 5 · Push and get the pipeline green

1. `git push origin main`.
2. Watch every workflow run the push triggered in the background, job by job (`gh api repos/guided-traffic/quader/actions/runs/<id>/jobs`
   every 60 s), and keep building meanwhile; never sit idle on a run (rule 10).
3. On failure: `gh run view <run-id> --log-failed` (or the job's log while the run goes on). A failure the change caused is
   fixed at once: find the cause, fix it, commit, push, and watch again. A failure the change did not cause (an outage, a row
   that fails under the runner's load) may be started again up to three times with `gh run rerun <run-id> --failed` (rule 7);
   the re-runs are noted and the red runs recorded as a finding. Repeat until the jobs that gate Semantic Release are green; a red `integration` or `e2e` leg does not hold the release and is still worked on until its cause is fixed (owner, 2026-11-10, Q-ARCH-457).

## 6 · Release

The pipeline releases; the run does not tag, create releases or write notes by hand. Semantic Release
cuts the release from the commits on `main` ([release flow](../developer/releasing.md#release-flow)),
and `Release Docker & Helm` publishes the image `guidedtraffic/quader` and the chart.

1. Commit the phase's work with conventional commit messages: `feat:` for what the phase adds, `fix:`
   for fixes, `docs:` or `chore:` for the plan and the rest. Commit the final plan update (phase row 🟢)
   with them. **The subject is the line of the release notes (owner, 2026-11-08):** a plain-language sentence
   that tells a user what is new or fixed and what it does, never a list of IDs. The body explains it in two
   or three sentences and names the requirement, decision and finding IDs on its last line. A subject such as
   "the registry root serves /v2/ through one enforcement point (R-OCI-003, …, T-16 WP3)" is what the owner
   called useless: nobody can tell from it what changed.
2. Push, and wait until the jobs that gate Semantic Release are green on that commit; until the first stable release e2e and integration do not gate it and are run locally as needed (owner, 2026-11-10, Q-ARCH-457). Semantic Release has then cut the
   release of the push, if the commits held a `feat` or a `fix`, and `Release Docker & Helm` has run for
   it: `gh run list --workflow "Release Docker & Helm" --limit 3`, then `gh run watch <run-id> --exit-status`.
   Push nothing to `main` while that run is going, not even a `[skip ci]` commit: Semantic Release publishes only when the
   run's commit is still the tip of `main` ("The local branch main is behind the remote one, therefore a new version won't be
   published", run 42410791254 on 2026-11-07), and a manual dispatch does not release (the job runs on `push` only).
3. `git pull --rebase origin main` (the release commit moves `main` with the coverage badge). The release
   number goes into no plan: BETA.md and FULL.md track features only (owner, 2026-11-02).
4. Check the published artifacts of the release: `gh release view vX.Y.Z`, the image
   `guidedtraffic/quader:X.Y.Z` on Docker Hub, and the chart index
   `https://guided-traffic.github.io/quader/index.yaml`. The release notes are generated from the commit
   subjects (item 1); the IDs stay in the commit bodies.

## 7 · Report to the owner

In German, short: the phase; what was built; how it was verified (CI run links,
test counts, exit criteria with their evidence); the decisions taken (their IDs); deviations and
open items; the next phase.

If the run cannot finish the phase (for example because the context runs out), leave it
resumable: commit and push the finished, green work packages, keep the phase and its open items at
🟡 in the plan, and report precisely what is done and what is left. The next run picks it up.

## 8 · Open findings

Defects and gaps found outside a phase, waiting for a run (step 2, item 5). Each entry names where it
is, what was seen and how far it is confirmed: **confirmed** means checked in the code by the
development agent, **read** means found by reading the code and not reproduced. Add new findings
here; remove a finding in the commit that fixes it.

Found on 2026-11-18 by reading the stall evidence of the baseline nightly 44186978088 (`e2e (ha-faults)`) and the runner cluster:

### Tests and tools

- **F-133 · The stall family on the CI runners: every kind cluster of a machine sits in one cgroup of weight 100, the runner pods in one of
  weight 1250** (the cause of H-795, F-124, the stall of the set `push` and the `ha-faults` entries below, as far as each is dated; [Q-HA-305](../spec/ha-and-helm.md#q-ha-305--a-stall-of-the-kind-nodes-is-told-apart-from-a-neighbours-load-and-a-fault-test-does-not-start-on-a-sick-cluster)).
  **Verified** on 2026-11-18:
  (1) Layout, read in a `dind` container on `k8s-runner-omega`: the job's Docker daemon sees the machine's cgroup root; the kind nodes of every
  leg, their k6 containers and the image builders are children of the top-level `docker` (`cpu.weight` 100), the runner pods and the small jobs'
  steps are under `kubepods` (`cpu.weight` 1250). When the 32 CPUs are all asked for, all kind clusters on a machine together are guaranteed about
  two CPUs. (2) The case, `TestHAFaults_ValkeySlow` of 44186978088: the leg ran on `k8s-runner-omega` (Scheduled event of `wds18-large-runner-8pzwg`)
  beside one other kind leg (`edge`). From 23:58 to 00:10:54 four fault tests passed with warm-up p99 0.5 to 1 s, host pressure 400 to 480 ms/s,
  load 50 to 75. From 00:10:54 to 00:10:57 eight small jobs of Test and Release 44205650371 (`stab/integration`) were scheduled on the same machine
  (Angular build, Trivy with an image build, golangci-lint, unit tests, release tooling, NuGet vectors, integration shards b and c). At 00:12:28,
  when the next test's load started and 66 s before its fault, the leg collapsed: `full` CPU stall of its nodes 200 to 495 ms/s (25 to 165 before),
  load 247, edge idle 0 % with 813 connections and no accept, k6 p50 2 to 35 s, a `packages` pod `OOMKilled` at 00:13:34 by its socket buffers (fixed by Q-ARCH-500), both edge pods
  restarted by their liveness probes; over when the load stopped at 00:17:40. The warm-up p99 the fault was then judged against was 46 s.
  A second case the same night, `e2e (ha-pooler)` of the same run (`TestHAPooler_InstanceLoss`, graceful delete of a `quader-db-pooler-control`
  instance), on `k8s-runner-lambda` beside the kind leg `e2e (npm)` of Test and Release 44208024306: its two `rw` tests passed from 00:45 to 00:49
  (warm-up p99 0.4 to 0.6 s, host pressure 420 to 460 ms/s); from 00:48:48 to 00:50:02 nine small jobs of Test and Release 44210830256
  (`stab/vuln-legs`: build, generate, chart, S3 conformance, malware scan, SBOM, actionlint, integration shards a and b) were scheduled on the
  same machine; from 00:50:20, in the warm-up of the `control` test and before its fault, the `blob` tarballs hit 45 s (1,521 status 0 of
  15,067), the nodes' `full` stall rose from 27 to 147 ms/s to 107 to 371, load 150, edge idle 0 %, 733 dropped iterations; it cleared when that
  test's load stopped. A third case, the first with the CPU time recorded: the verification run 44211950522 of `ha-faults` on `k8s-runner-omega`
  passed its first five tests (`ValkeySlow` included), then at 02:05:52 seven small jobs of Test and Release 44220539577 (unit tests, lint, release
  tooling, an image build, Trivy, actionlint, integration shard a) and at 02:07:33 the kind leg `e2e (npm)` were scheduled beside it. From 02:07:30,
  in the warm-up of `ValkeyCredentialMismatch`, k6's p99 rose from 0.8 to 45 s; from 02:08 to 02:09:30 the machine had 27 to 31 of its 32 CPUs busy
  (`host` `cpu_usage_us`) while the leg's four nodes together got 3 to 7 CPUs, down from 16 to 20 in the tests before; two `packages` pods were
  OOM-killed by their socket buffers (fixed by Q-ARCH-500). The machine was full and the leg was not what filled it.
  (3) The overlap, from the job lists of every run since 2026-11-08 (cluster-wide; a node is known only for tonight): `ValkeySlow` red on 11-12 and
  11-18 with 22 and 25 small jobs and 3.5 and 4.2 other large legs beside it, green on 11-13 to 11-17 with 0 small jobs and 0.8 large legs; over
  every leg of at least 10 minutes, `ha-faults`, `npm-ha` and `drain-roles` green ran beside a median of 0 small jobs (job-equivalents), red beside
  3.2 to 4.2. The nightly's legs meet the small jobs because GitHub starts the `17 2 * * *` schedule between 09:20 and 10:41 UTC.
  **Not verified:** the split of the machine's CPU between `kubepods` and the other legs in a red window (the third case lost its `cg:` rows to a
  collection bug, fixed: the helper's file is copied out of the stopped container; the host and the leg's nodes were recorded);
  the node of every earlier case. **Does not explain:** F-119 (1) (no job of either class beside the dispatch at 07:50 on 11-09), and for the legs of
  Test and Release (`push`, `access-long`) overlap does not separate red from green: they always run beside their own run's small jobs (median 6 for
  green and red alike).
  **For the owner (runner definitions, not in this repository), sized from the measurements above:** a kind leg used about 10 CPUs under load
  (`docker` 19.4 CPUs for two legs on omega at 00:41, `kubepods` 5.7 for five small jobs, steal 0.5 %); in the red window it was guaranteed a share of
  two CPUs for both legs on the machine. Recommended together: (a) `dockerd --cgroup-parent` in the `dind` sidecar set to the runner pod's own cgroup
  (from `/proc/self/cgroup`), so a leg's kind nodes are charged to its pod; its memory limit then covers them too and has to be sized from a leg's
  `pods.csv` and `cg:` rows first; (b) the CPU request of `large` from 2 to 10: by request at most three legs per 32-CPU machine, and under
  contention a leg's share against a small job is 10 to 1 (tonight's omega: about 11 CPUs per leg instead of two for both); 6 × 10 + 24 × 1 = 84 of
  96 CPUs, so the current maxima still schedule and nothing queues, but a machine with three legs takes two small pods. (b) without (a) changes
  nothing for the kind nodes, whose CPU is outside the pod. Alternatives: `cpu.weight` of `/docker` raised on the machines (cheap, no queueing, kept by
  no manifest, and the legs still share it with the image builders); a machine reserved for `large` (no small job beside a leg, but the class then has
  room for three legs and Test and Release's kind legs wait behind the nightly's lanes, which the owner's rule forbids). The nightly at night would need a
  trigger outside GitHub's schedule (a CronJob that dispatches it); after (a) and (b) it matters less. In this repository: the recorder and the
  warm-up check of Q-HA-305; `concurrency` across the two workflows is ruled out there (it cancels pending runs and holds the release).
  **Applied on 2026-11-18, 08:10 UTC, by the owner's word** (runner definitions, commit 734686d there): (a) and (b) for the class `large`, with
  three things the test of the change added. The kubelet's cgroup driver on the runner machines is cgroupfs (`/kubepods/burstable/pod<uid>`), so
  the parent is `<pod cgroup>/docker`. Inside the pod a container without a CPU request has `cpu.weight` 1 beside the Docker containers' 100, so the
  `runner` and `dind` containers request one CPU each (weight 100) and the Docker containers get weight 800. The memory limit went from 20 to 28 GiB,
  because it now counts the kind nodes (the pods of the `drain-load-b` leg of 44233617621 held 11 GiB, of that 4.4 GiB page cache; k6, the image
  builds and the nodes' own daemons are not in that figure). **Verified** with a pod of the same layout on `k8s-runner-lambda`: containers land in
  the pod's cgroup, a two-node kind cluster of the pinned versions comes up Ready, the top-level `docker` cgroup stays empty, the kubelet removes
  the pod's cgroup. **Not verified:** a leg on the new layout, above all one with small jobs beside it, which is the proof that the stall is gone;
  whether 28 GiB holds every leg (an OOM kill inside a leg is now possible where none was before: read the pod's `memory.peak` and `pods.csv`).
  The class `self-hosted` is unchanged, its image builds stay in the top-level cgroup. **Follows in this repository:** the recorder
  (`deploy/dev/e2e-evidence.sh`) reads the job's containers as children of the top-level `docker` (`cgd:` rows, `containers.tsv`); on `large` they
  are now under `kubepods/burstable/pod<uid>/docker`, so those rows are empty for the leg's own containers until the recorder follows the daemon's
  cgroup parent. The `cg:` rows and the `node:` rows are not affected. F-133 is closed when a nightly with a Test and Release run beside it is green
  and its evidence shows the nodes kept their CPUs. **Closed on 2026-11-19** by the second full nightly (44453781574, see its record in section 10):
  beside two Test and Release runs a loaded leg kept nine tenths of its CPUs and was green; the NuGet legs were not among them.
  **Done in the night of 2026-11-18** (counted first in the evidence of `e2e (push)` of Test and Release 44384406557, a leg on the new layout:
  10 own containers in `containers.tsv`, 1,467 `cgd:` rows of 9 containers, none of them an own one): the recorder searches the cgroup of its
  first kind node once per set and takes its parent; on `large` it writes `daemon:parent` and `daemon:pod` rows, `cgd:` rows of the leg's own
  containers again, and `pod-memory.csv` with the pod's `memory.current`, `memory.peak`, `memory.max` and its OOM counters (`oom_local` is the
  pod's own limit being hit). On `self-hosted` its output is as before. Tested against fabricated cgroup trees and on a real kernel with a
  stand-in container; **not verified on a runner**: the first legs must show the line "evidence: the job's Docker daemon puts its containers
  under /kubepods/burstable/pod<uid>/docker …, inside the pod … (memory.max 28.0 GiB)", `cpu_weight` 800 on `daemon:parent`, and
  `oom_local` 0.

Found on 2026-11-08 during the run of T-16, by the penetration test of the F-82 fix and the code reviews of WP4b and WP12:

### Tests and tools

- **F-121 · The nightly performance run misses V-7b on a loaded runner** (runs 42571414393 with p99 314.7 ms and 42595813923 with
  610.957 ms against 300 ms; V-7a 17 to 60 ms in the same runs; both shared the nodes with other kind legs). Not reproduced as a
  regression, looked at on 2026-11-09: the path is the one [Q-VULN-81](../spec/vulnerability-scanning.md) decided and no commit since
  2026-11-06 changed the findings list, its queries or the report scope (the authz commits 08eef8ba and 8f192491 touch the
  version-independence test of `ReadScope`, which a package-pattern grant does not trip: the page still runs the pair read and the
  page statement). On the integration stack at a load average of about 10, `TestR_VULN_028_TheFirstPageOfAPatternGrantAtTheShapeOfTheRun`
  with the run's 100 requests, the first included, gives first request 60 ms, median 67 ms, p99 76 ms, slowest 76 ms; without the
  `ANALYZE` of the seed 74, 76, 102 and 118 ms. Every request runs exactly two statements (`VulnScopePackages`, `VulnFindingsList`).
  In the log of 42595813923 the 100 requests of V-7b took 9.3 s in all, a mean of 93 ms: the typical request is where it was
  (112 ms on 2026-11-06), the p99 of 100 is the second slowest request, so two stalls on a shared, CPU-limited VM are enough to
  miss. The test now asserts the two statements, so a regression of the path fails before a runner does. Open: the cause of the two
  slow requests on the runner is not measured (a neighbour, the replica's garbage collection, or the autovacuum of the 100,000 rows
  the run inserts without an `ANALYZE`). Next step: a run of `vuln-perf` alone on the large class (after the kind legs, as
  [Q-ARCH-325](../spec/architecture.md) set) that logs the slowest five requests with their request IDs; revisit if V-7b misses
  there as well, which would make it the reference environment's number ([H-491](../security/vulnerability-scanning.md#h-491--the-performance-run-is-not-the-reference-environment)).
- **F-117 · The self-hosted runners lost the Ubuntu archive for a few minutes** (Test and Release 42553790224, 2026-11-09 03:02):
  every job that installs `build-essential` at that moment (GoSec, cyclomatic complexity, both builds, the image, the source malware
  scan, Trivy) failed in `apt-get` with "Could not connect to archive.ubuntu.com:80" over IPv6 and IPv4; jobs that had installed
  earlier passed. An outage outside the repository (at 03:10 UTC `archive.ubuntu.com` did not answer from the owner's network either,
  `security.ubuntu.com` did); it makes every such job depend on the archive. Done when the runner image carries
  `make` and the C compiler (the owner's runner) or the jobs keep a local package cache. Again on 2026-11-11 from 14:25 (Test and Release 43037911704): `azure.archive.ubuntu.com`
  did not resolve for about three minutes on the GitHub-hosted runners (the self-hosted ones were not affected), and `e2e (nuget)` and `e2e (ci)`
  failed in their `apt-get install build-essential`; again at 15:05 for `e2e (nuget)` in 43044075096 (re-run). `e2e (vuln)` and `e2e (bastion)` on
  the same kind of runner passed in both runs; `e2e (nuget)` and `e2e (ci)` stayed red after the first re-run. Since then the jobs call
  [build-tools](../../.github/actions/build-tools/action.yml), which installs only what the runner lacks, so the GitHub-hosted runners no longer touch
  the archive; the self-hosted image still lacks `make` and the compiler, so this finding stays open for it. The outage went on past 15:50 (Test and
  Release 43052141784): the step passed, and `e2e (nuget)` and `e2e (ci)` then failed in building the NuGet clients image, whose `RUN apt-get
  install mono-complete` could not resolve `archive.ubuntu.com` while `dist.nuget.org` resolved in the same build. Those two legs depend on the archive
  through that image; a prebuilt clients image in a registry would remove it.
- **F-119 · The transfer loop of the drain tests saw 429 and 502 when the cluster stalled** (Test and Release 42561072166, 2026-11-09, and
  the nightly dispatch 42578594755 of the set `drain-roles` alone on the large class). Cause found and fixed for the transfer loop (Q-HA-230):
  the cluster stalled for about 40 s (07:50:08 to 07:50:50), the open-loop `transfer.js` released its piled-up iterations in one burst of
  36 PUTs in a second, and the write class (50 per 5 s for one principal) refused 16 of them; 7 of the 16 reached the client as `502`
  with the edge's flag `SH`. Not the fallback of a fresh replica (the policy header shows the shared counters, Valkey was available
  before the replicas turned ready) and not the drain (the refusals came five minutes after the last eviction). Open: (1) what stalled the
  whole cluster at 07:50:08 (every `npm ci` took 40 s, the edge's connect time to the pods reached its 5 s bound, the `packages` pods
  served a third of their rate), with no pod event, restart or failover in that minute; the load of the runner was not measured. Read on
  2026-11-18 against the job lists: beside it in that minute ran one other kind leg (`npm-ha` of the same dispatch) and no small job, so the
  small jobs of F-133 do not explain this stall (two kind legs on one machine may; their node is not known); it predates the recorder. Open until a recurrence is read with the CPU time per cgroup (Q-HA-305);
  (2) is closed ([Q-ARCH-446](../spec/architecture.md#q-arch-446--a-response-to-a-request-whose-body-is-unread-is-sent-first-then-the-body-is-read-and-discarded-within-a-bound): reproduced behind HAProxy 3.0, the pod's close with an unread body sent an RST that destroyed the answer, fixed in the listener); (3) the 13 of 11,529 requests answered 502 in the
  certificate rotation row of the run 42561072166, which the transfer loop does not explain, and the two later runs of the set `push`
  (42563355283: 10 of 3,245 through the scatter edge with a slowest request of 15 s). Looked at on 2026-11-09 in the recurrence
  42618038153 (job `e2e on kind (push)`, 13 of 9,476): **the reload is not the cause.** The 502s are GETs of `/system/ping` and
  `/system/replicas` that the edge logged with flag `SH` and connect time 1.4 to 2.8 s, at 12:54:37; the first pod reload is at
  12:55:12 (`TLS material reloaded`, quader_tls_reloads_total 0 to 2 per pod), 35 s later, and the test's own log dates the 502s
  at 12:54:20 to 12:54:23, the first seconds of its open-loop load of 100 requests a second (`startK6`, `load.js`), which piled up to about 1,000 connections at the edge (k6: p99 13 s, slowest request 19.4 s, avg 1 s for a ping).
  The pods logged `http: TLS handshake error ... i/o timeout` for 8 connections from the two edge pods at 12:54:34 to 12:54:36,
  which is the 10 s `read_header_timeout` expiring on a handshake that did not finish: the pod was starved of CPU by the ramp
  (valkey circuit open at 12:54:27, pings of 1.6 to 3.5 s), the listener gave up on the handshake, the edge saw the abort before
  any header. Reproduced no 502 with the real listener behind HAProxy 3.0 under 1,000 and 2,500 closed-loop GET clients on 1 and 0.3 CPU
  (13,164 and 12,896 answers, all 200), so the starvation of the kind node is the precondition and was not rebuilt. Nothing in a
  reload closes a connection: `ListenerTLS` swaps the certificate pair through `GetConfigForClient` and keeps the listener and
  every established connection. Same family as (1) and (4): the rotation row is run with the load that stalls the cluster. Next
  step: measure what starves the pods at the ramp (the row's own k6 on the same node, and whether the open loop piles up as the transfer loop did) before the product is touched; the rotation row runs `load.js` as a closed loop since Q-HA-288, so the pile-up is gone there.
  (4) the p99 conditions of `TestDrainRoles` (R-HA-059, Q-HA-94) failed in two nightly dispatches of the branch of the fix, which had other large legs
  running at the same time (documents 1.64 s against a 755 ms warm-up; then 5.25 s against 840 ms and all requests 13.8 s against 1.7 s), while the transfer loop
  had 0 failures in both. Revisit when a run shows the stall again. The evidence is now recorded in `.out/e2e/evidence/` of every e2e artifact, green or red ([e2e.md](../developer/e2e.md#reading-a-stall)); read it at the next occurrence.
- **F-105 · `TestR_NPM_007_ASlowPublishOf200MiB` fails under load** (seen twice: by the F-83 builder, an S3 UploadPart
  cancellation followed by the package timeout, and by the F-101 builder, "1 connections held a transaction older than 1 s
  while the body arrived"; passes alone). The test asserts that no database transaction stays open longer than 1 s while
  the body of a slow publish arrives. Done when the cause is found: a transaction the publish really holds during the
  body (a product defect), or an unrelated transaction of the shared stack the test counts (a test defect).
  Read on 2026-11-09: the publish holds none while the body arrives (`handlePublish` decides and pre-checks ownership before the
  body, `publish.Scan` streams the attachment through `Ingest().Spool` into the local spool file with no database handle, and the
  first statement after the body is `VersionFacts`); the watch counts every connection of the stack's database, so the candidates
  are a background transaction of the replica slowed past 1 s on the loaded host, or the commit's transaction counted because
  `bodySent` closes only after the client goroutine returns from its last write. The test now names the transactions it counted;
  next step: read that name in a recurrence and narrow the watch to it, or measure the request's own transactions instead.
- **F-102 · `TestR_BAS_014_ThousandWritesToOnePathCauseOneProbe` failed once under host load** (seen once, by the WP5
  builder; passes alone and three times in a row). Done when the cause is found and fixed. Read on 2026-11-09, no structural
  defect found: the writes share one flight per key and its leader reads the kept answer again before it probes (`write` in
  `engine/probe.go`), and the kept answer is a synchronous LRU on the harness's fake clock, so a second probe needs a first flight
  that failed or was cancelled, which answers the waiting writes with an error instead of 403; the failure's message was not
  recorded. The test now prints the body of a non-403 answer and every request the main double saw; next step: read those in a
  recurrence.
- **F-95 · One Angular unit test failed once in 1,080, its name not recorded** (seen once, by the F-86 builder on a loaded
  host; the second run and the reviewer's run passed all 1,080). Done when the failing test is identified and its cause
  fixed; until then a recurrence is reported with the test's name and output. Looked at on 2026-11-09: without the test's name
  there is no test to read; the next step stays the recurrence with its name.
- **F-96 · The registry's anonymous test failed once after 85 s, its message not captured** (seen once, by the WP3
  builder on a loaded host; 10 of 10 later runs passed). The settings wait it was suspected of takes under 1 ms; the test now
  logs that wait and fails with the replica's last answer. Done when a recurrence names its cause and that cause is fixed.
  Read on 2026-11-09 (`setAnonymous` in `test/integration/registry/anonymous_test.go`): the wait is on the replica's own answer to an
  anonymous token request, bounded by five times `auth.revocation_bound` and naming the last answer; no further structural
  defect found, so the next step stays the recurrence with that message.

Found on 2026-11-03 while closing phase 5:

### Tests and tools

- **F-36 · The issuer double's fixtures were recorded on a local GitLab 19.0.1-ee, not on the owner's instance**
  (confirmed). The owner's GitLab (namespace `gitlab` of his cluster) cannot create projects: the web service dials Gitaly
  under a name its certificate does not carry. Machine-level for the owner (his GitOps repository). Done when, after the
  certificate is fixed, `make oidc-fixtures` against his instance records the fixtures again (the licensed
  `environment_protected: "true"` included) and the double still reproduces them.

Found on 2026-11-01 while writing the manual (`docs/manual`) and the design change of Q-UI-88:

### Tests and tools

- **F-17 · A leftover of an old tool version on the owner's machine** (confirmed, checked again on 2026-11-02). The shadowed
  `kubectl` and `kind` in `/usr/local/bin` are gone and the Command Line Tools are at 27.2. Left: since Docker Desktop's
  restart of 2026-11-02 (after the disk ran full) `~/.docker/cli-plugins/docker-buildx` points at Docker Desktop's own
  buildx again (v0.28.0-desktop.1, pin v0.38.1), so `make versions-check` fails locally; the Homebrew copy is in place
  (`ln -sf /opt/homebrew/bin/docker-buildx ~/.docker/cli-plugins/docker-buildx`, no admin password; Docker Desktop may
  relink it on its next update unless its CLI plugin management is turned off). Machine-level: the owner runs it; a run
  does not change the machine. Checked again on 2026-11-08: unchanged (`docker-buildx` still links to Docker Desktop's v0.28.0-desktop.1,
  `make versions-check` exits 2 on that line only).

### Open at the end of the run of 2026-11-10

Handed over at the end of the session of 2026-11-10/11 ([section 11](#11--run-record)). v0.43.2 is released (Go 1.27.4); the push of
f8719487 (the format logos, v0.44.0) ran green in every job (Test and Release 42995232524). The owner's installation follows the chart
by `version: "*"`. Red pipelines are worked on even though they no longer hold a release
([releasing.md](../developer/releasing.md#until-the-first-stable-release)). The next run starts with the owner's tasks of
[section 10](#owner-tasks-of-2026-11-11), **T-18**, with the red legs below beside them; the next unit of
[FULL.md](FULL.md) (block B, F1) comes after them.

**Decisions only the owner can take**

- **Q-HA-293, to be discussed, not decided**: a replica whose first Valkey attempts get no answer becomes ready degraded (Q-HA-13 (d)), so a wrong password behind an unanswered first attempt is noticed only later. Should the first readiness wait for a verdict? Waiting would stall a replica started during a Valkey failover, which Q-HA-13 (c) rejected.

- **H-812** ([image-scanning.md](../security/image-scanning.md)): Trivy shares the file system and network of the worker pod, so a crafted image that compromises it can read the secrets mounted there. Close it before images from untrusted remotes are scanned (a sandbox or a pod per scan, a further component), or accept it? A dedicated `vuln` worker pool would shrink it; the owner kept the plain workers on 2026-11-10 (Q-VULN-137).
- **Browse**: decided by the owner on 2026-11-18, it goes from the navigation and its file operations move to the repository's page ([W-8](#owner-walk-of-2026-11-18-the-frontend)).

**Gating jobs: green**

- `unit tests` is green in every Test and Release run since b0b4538d (eight runs on 2026-11-10). `GOMAXPROCS` 8 was set on 2026-11-10 because six time-bound unit tests failed at once with 32 threads (run 42825155671); at 8 threads only `TestR_VULN_004` failed (run 42839263211), and that test no longer has a wall-clock bound. `TestR_BAK_006_AbortAfterCancellationStillGoesOutWithinTheGrace` and `TestR_STOR_033_AReaderThatPausesIsNotAStalledStore` rest on timing inside the product (the abort grace, the idle timeout), so a test-only event would not hold them: if they fail again at 8 threads they need their own change. Elsewhere replace a bound by a real hang or an event where the test allows it, as in `TestR_BAK_013`, `TestR_BAS_039` and `TestR_VULN_004`.

**Integration and e2e legs (no release gate, still to be fixed)**

- F-128 · `Semantic Release` failed once in Test and Release 43316531262 (40d37ac2, 2026-11-12): GitHub answered the push of the tag
  v0.55.2 with `! [remote rejected] v0.55.2 -> v0.55.2 (Internal Server Error)`; no tag and no release were made, and the next push
  released the same commits. Read, not reproduced: an error on GitHub's side, nothing in the workflow changed. Done when it does not
  recur; at a recurrence, re-run the job once before anything else.
- Other integration binaries that take a free port from port 0 can collide as `TestEndpoints` did (`freeAddr` of `test/integration/server` no longer hands out an address twice, Q-HA-285).
- **`ha-faults`** (run 42774548669): `TestHAFaults_PostgresOutage` is green since 732572ba. `ValkeyFlushAll`: 503 from 07:53:20, 70 s before the FLUSHALL, while the run shared the runners with Test and Release; pod logs of the window are not kept. `ValkeyCredentialMismatch`: the `/readyz` 503s were the draining pod and its replacement, as R-HA-007 requires, so readiness is no defect (read on 2026-11-11); 53 tarball requests of the `blob` package hit k6's 45 s timeout, started within 6 s of the pod deletion, pod logs not kept: same shape as F-124. Read on 2026-11-18: the FLUSHALL window (07:52:30 to 07:55)
  had five small jobs of Test and Release 42774543420 beside it and no other kind leg, which fits F-133; their node is not known. The same leg's
  `ValkeySlow` failed in the nightlies of 11-11 (p99 5.2 s against a bound of 2.4 s, no failed request, three small jobs beside it), 11-12 and
  11-18 (22 and 25 small jobs beside it, a `packages` pod OOM-killed in both, fixed by Q-ARCH-500); 11-18 is the verified case of F-133. Open: the 11-11 and 11-10
  windows, read again at a recurrence with the CPU time per cgroup (Q-HA-305).
- **F-124 · Tarballs of 1 to 5 MB stall at the edge for about 40 s in one window** (nightly 42994478570 `drain-roles`, and `ha-faults` `ValkeyCredentialMismatch` of 42774548669): 53 to 72 requests for the `blob` package's tarballs hit k6's 45 s timeout within about 5 s; HAProxy logs them `CD` with 3.2 to 4.9 MB streamed in 36 to 38 s and about 1,800 connections open. Not the drain (in `drain-roles` it was the warm-up, 3 minutes before the first eviction). Same night, `drain-load-pooler` (variant D): from 11:40:50, 43 s after the first eviction, until 11:44:10 the edge held 1,500 to 1,800 connections, k6 ran into its 45 s timeout and dropped 1,501 iterations, `npm ci` took up to 3m35s and every backend lost its servers at 11:42:50 and 11:43:15; the poolers logged no failed server login in the window, so it is not the switchover of Q-HA-302. The run predates the evidence recorder, and the surviving pods' logs of the window were not kept. Probably a load near the VM's capacity, not verified. Read on 2026-11-18 in its recurrence (nightly 44186978088, `ha-faults` `ValkeySlow`): the `blob` tarballs were again the first requests to reach 45 s (from 00:12:32), the edge held 813 connections per pod at 0 % idle, the `packages` pod's object store GETs of 2.6 MB took 38 s; the cause there is F-133 (verified: eight small jobs on the same machine). The two runs named here predate the recorder and their node is not known. Open until a recurrence is read with the CPU time per cgroup (Q-HA-305); a tarball stall without neighbours would be the leg's own.
- F-126 · `TestConcurrentWritersOnOnePath` R-STOR-003 refused one of the PUTs or DELETEs of six writers on one folder once (Test and Release 43013671879, shard b); the status was not logged. The check now names the first refused request; passed locally. Next step: read it in a recurrence. **Read in the night of 2026-11-18, cause not established:** a delete never waits on a folder lock (`SKIP LOCKED`), other deadlocks are retried three times; a deploy's link step gives up with 503 after five rounds, each of which needs another writer to commit an ancestor's removal inside it (not reachable in practice with two writers per folder, not excluded; the spec now names the limit and its 503); a 503 of the object store or the database, or a 500, cannot be told from the code, and the one log left has no replica output. Since then the test prints the replicas' request log with the first refused request: a 503 "The path is being changed by concurrent requests" is the five-round limit, a 503 with a store or database cause is the infrastructure, a 500 is a fault of the product.
- F-127 · Test and Release 43022311289 (9220e777), green on its first re-run: `e2e (push)` failed from its first test on, about four minutes after `up.sh`'s ping check had passed, with `x509: ECDSA verification failure` against the job's own `quader dev CA` on every request through the edge and `password authentication failed for user "quader"` in the migration Job of `TestE04MigrationFailure`; both are secrets the in-cluster generator writes. Hypothesis, not verified: the secrets operator (see the crash-looping operator below) wrote the CA and the database secret again during the run. Next step: keep the operator's log and the secrets' resource versions in the e2e artifacts. In the same run `integration (shard c)` failed `TestR_DEB_015_ImportRules` (the control pool held one connection at the close deadline, readiness timeouts of the database at 12:55, H-822) and `TestR_DEB_023_TheShadowingCheck…` (a name the stub does not list: known false right after the scope settled for another name; `debremote` passed locally on the same tree), both in the stall window of 12:55 to 13:01. The evidence is now recorded in `.out/e2e/evidence/` of every e2e artifact, green or red ([e2e.md](../developer/e2e.md#reading-a-stall)); read it at the next occurrence. **Cause found on 2026-11-12:** on the local kind cluster the internal-secrets-operator was OOM-killed at every start for 42 hours (467 restarts, limit 128 MiB). It caches every Secret of the cluster, which held 78 helm release Secrets (23 MB) after many runs of dev-up, and it runs under amd64 emulation on the arm64 host. It made its Secrets only in the seconds it ran, so a Bastion setup waited 300 s for `bastion-valkey-auth`. The dev profile now gives it 512 MiB and keeps two helm revisions per release. Whether the CI legs of F-127 had the same cause is not verified: CI runs a fresh cluster on amd64.
- **502 bursts at the edge (H-795)**: `npm-ha`, `drain-load-e`, `TestE14Drain` (runs 42885398204 and 42902356913: PUTs of the upload loop answered 502 after 17.5 to 18 s; not the drain, which had ended 1.5 minutes before with no request cut, but a stall of the whole CI cluster of about 17 s: HAProxy `SH--` 502s on the API pings as well, TLS handshake timeouts on every packages pod, `valkey degraded`; the test passes on kind on the same tree), node loss. HAProxy `SH--`, 40 of 41 with a connect time over 1 s on three backends within 2 s; k6 measures 12 to 14 s where HAProxy measures 2 to 3 s, so the time may be lost before the accept. No reload in the window. Cause open. On kind on 2026-11-10 one 502 `SH--` of `TestE15TLS` had a connect time of 3,301 ms, 3.4 s after the certificate trigger and 12 s before the pods reloaded; dropped SYNs are ruled out: during bursts of 1 to 2.7 s on kind no ListenOverflows, ListenDrops, ReqQFull, backlog drop, SYN retransmit or conntrack drop moved on any pod, edge pod or node. The slow requests pause and resume together, at a CPU pressure of the shared VM of 450 ms/s on average (load 50 to 79 on 20 cores); at load 13 to 23 the slowest request took 22 to 25 ms. The E0.15 probe itself used more CPU than the installation and is cut 12 to 25 times (Q-HA-292). Open: the `SH--` itself (seen once, not reproduced under the counters), and a CPU pressure reading in the CI e2e artifacts that would settle the runner's share. The npm, NuGet and drain loads were not measured against the same yardstick. Again in nightly 42994478570 (2026-11-11), `e2e (npm-ha)` drain variant A: the edge fell from about 400 to about 10 requests a second at 09:33:56 for 12 s, TLS handshake `i/o timeout` on every packages pod, 502/503 with 10 to 13 s latency; the drain had ended 16 s before, no pod event. The same shape in Test and Release 42894886354 (`e2e (access-long)`, 2026-11-10 20:48:31): the third login POST of journey `01-sign-in` waited 4,081 ms for the connection to an api pod and 8,982 ms for its answer, 13 s at the edge, and was answered 204; the journey's wait of 15 s from before the click ran out (it is a 60 s hang bound since Q-ARCH-469). Read on 2026-11-18 against the job lists (cluster-wide, node not known): the `npm-ha` window of 11-11 09:33 had 12 small jobs and 4 other kind legs beside it, the `access-long` window of 11-10 20:48 three small jobs; that fits F-133, whose verified case shows the same edge (0 % idle, no accept, TLS handshakes to the pods timing out). The legs of Test and Release always run beside their own small jobs, red or green, so overlap does not decide those. Open: the `SH--` itself, and each case until a recurrence is read with the CPU time per cgroup (Q-HA-305).
- **A stall of the whole cluster in the set `push`** (`TestE15TLS`, run 42839263211, 15:15:46 to 15:15:56): the edge answered 1 to 15 requests a second instead of about 55, the api pods 3 to 6 instead of 45 to 60, PostgreSQL statements took 60 to 290 ms, Valkey went `circuit_open`. The 353 of 9602 answers with status 0 were requests this stall held for up to 10 s and the test's stop cancelled (Q-HA-288); the certificate is not involved. The rotation row now runs a closed loop and no longer piles up on a stall; its request count, a bound of the open loop that failed run 42870155749 with every answer 200, is replaced by "every client ran through the window and every api pod answered in it" (Q-HA-291). Open: what stalls the cluster (the e2e artifacts do not record the runner's load), and the 7 of about 10,000 answers 502 of run 42818570044, whose evidence was not re-read. Again in run 42907942036 (24cf0250): `TestE06Lease` got 11 of the 15 samples it wants with a p99 of 6.4 s for P-14, and `TestE15TLS` saw one ops listener serve the new certificate 4.6 s after its file changed against a bound of 3 s, every answer 200. Both are wall-clock bounds in a stalled run; the reading of what stalled it comes before either bound or the product is touched. Read on 2026-11-18 against the job lists: the window of 42839263211 (15:14:46 to 15:16:56) had two other kind legs and four small jobs beside it, cluster-wide; as a leg of Test and Release it always runs beside its own run's small jobs (median 6, red and green alike), so overlap does not decide it. Mechanism as in F-133; open until a recurrence is read with the CPU time per cgroup (Q-HA-305).
- The step `Install the Docker Compose plugin` took 20 minutes on one runner (Test and Release 42861234699, `integration (shard c)`, 17:06:53 to 17:26:51 on 2026-11-10; 13 s in the run before) and pushed the shard past its 45-minute limit: the job was cancelled while `vulnsync` ran, no test failed. Where the step lost the time is not known. Same family as F-117: the jobs download their tools during setup.
- `alerts`, `MaintenanceStaysQuiet` in nightly 42793416589: `valkey-failover.sh` reported "the primary did not move within 60 s" right after drain variant B; not found, possibly the Sentinel that variant B restarted had not rebuilt its view (9.6 s locally). (`QuaderDatabaseUnreachable` during maintenance was the rule's five-minute rates, fixed by Q-HA-290.)
- `bastion`, `TestBastion_R_BAS_049_RoutingByPath`: 0 hits on both replicas, once in twelve runs.
- `TestE14Drain` (a packages pod unready for 5 s at the end, Valkey `circuit_open`), R-ARCH-034 (26 held against 24, once), `TestR_DEB_010_DelayBound` (503 `database_unavailable`, once in nightly 42677190569; the suspicion on the retry budget of Q-HA-259 is unverified).
- A job can stay queued for hours when the ARC listener loses its assignment (P-13, 2026-11-10, `assigned job=0` after a broker retry); the cause was not established, and a stuck run holds the nightly group until it is cancelled.

**Product work still to do**

- H-829: the listing of a remote OCI repository (view `oci_remote_tags`, Q-OCI-215) matches a regular expression on the path of every tag entry, without an index, and was not measured on a cache with very many tags. Next step: measure it; a derived table or an expression index if it is slow.
- Image scanning: no scan seen yet on the owner's installation; push an image and read the page. Not run: a push through the whole app with the real binary, a drain or worker loss in a cluster, and the release image and chart in a cluster. Gaps H-810 to H-818 and H-871 to H-873 are on [image-scanning.md](../security/image-scanning.md); H-819 is closed: a scan takes its working space from the spool budget (Q-VULN-145 to 148). Not checked: the thresholds of `QuaderSpoolNearlyFull` against the larger worker spool of 8 GiB.
- Format logos (owner, 2026-11-11, Q-UI-225 to 227): Debian, NuGet and OCI show their owners' logos. npm, Ansible Galaxy and the Docker
  whale may be shown only with the owner's permission (npm's logo policy, Red Hat's trademark guidelines, Docker's trademark guidelines,
  read 2026-11-11), so npm and Ansible Galaxy keep the neutral glyph. To show them, the owner asks npm, Inc. and Red Hat for permission.
  At the walkthrough: the logos in the running app, light and dark, in Firefox and Safari (only Chrome on a static page was looked at).
- T-19, the documentation of Ansible collections, is built for local, remote and virtual repositories (the set `ansible`, 87 tests, green on kind on 2026-11-11). Open: its four wiki seam sites still ask the repository read until T-18's check replaces them (T-18 WP1d), so a wiki-denied member of a virtual repository has no proof yet; gaps H-900 to H-903 ([ansible-documentation.md](../security/ansible-documentation.md)); the job `e2e (ansible)` ran it green on CI with Node.js and Playwright (Test and Release 43044075096, 87 tests, the five `TestAnsibleDocs*` included).
- T-20's address field (R-UI-065): the browser check in `web/e2e/tests/02-repository-and-files.spec.ts` passed on kind in `e2e (access-long)` of Test and Release 43007020170; at the walkthrough: Q-UI-274 gives the field the editable control's fill instead of the read-only grey, not compared with GitLab's clone field in a browser.
- T-21 (2026-11-12): what is done, all of it on main:
  - `make ui-renders` renders every screen and the creation flow as the plain user, the power user and the administrator: light and
    dark, three widths, a contact sheet. The renders are never committed.
  - In the repository lists every address field has one width and shows the address from its start, and the lists stack below
    48 rem (Q-UI-300 to 306 replace Q-UI-270 and 271).
  - The three kinds of user are specified: R-UI-067, Q-UI-310 to 319.
  - The audit of every screen, 49 lines, is [ui-audit.md](ui-audit.md); the fix waves follow it.
  - Fix wave 1 is on main, five commits from 50c485fc to 175ade9e:
    - every list has a column budget, one alignment and a stacked row below 48 rem;
    - a shared page header and a shared empty state;
    - the repository page shows its address and Set up client at the top;
    - the package list has four columns, and the package page shows the install panel and the README first;
    - the start page has a package search and the recently opened repositories;
    - a reader sees no administration.
  - Four browser journeys were changed with the wave and not run on kind before the push: 11 (nuget), 12 (projects), 17 (deb) and
    20 (ansible). The e2e legs of the push run them.
  - Open, seen in the renders of wave 1 by the development agent:
    - two address fields of one repository (the client's and the files') stand unlabelled on the start page and in the
      repository page's header;
    - the install panel's two selects differ in width, and its command's copy button is not joined to the field the way the
      address field's is;
    - a disclosure ("About this project", "Details") is styled as a large heading with a chevron;
    - a reader's repository list puts "Recently viewed" (the start page says "Recently opened") and "About this project" above the
      list;
    - the repository page offers Set up client both as the header's button and as a tab;
    - the package page's version choice looks like a text field.
- A repository search for readers across projects needs an API that readers can read; until then the start page searches packages
  only (Q-UI-311).
- A Details section does not remember whether it was open (Q-UI-319).
- `quader.recent-repositories` is one browser key for every user of a browser (`recent-repositories.ts`), while recent projects are
  kept per user; T-22 fixes it (Q-UI-405).
- Q-UI-311's note that a cross-project repository search needs a list readers can read is wrong: `GET /api/v1/repositories` is open to
  every caller and filtered by read. T-22's WP0 corrects the note.
- `api-and-ui.md` has Q-API-260 to 263 after its first `## What this does not cover` (line 11329) and a second such heading at line 11663.
- R-UI-002 and `_shell.scss` cite UI-A-48, which ui-audit.md no longer lists since fix wave 3 removed the fixed lines.
- T-21 fix wave 2 is on main, from 46ad3022 to 47bab3d4:
  - one client address and one way to the setup per repository; the version and the install command look like the controls they are;
  - the client setup asks only for the client, and a read-only project screen names whom to ask;
  - an empty repository, the browser, the token pages and the documentation show a reader what to do next;
  - every screen except the vulnerability pages, the repository settings and the creation flow has the shared header.

  The development agent's six render findings of wave 1 are fixed.
- The 429s of the renders came from the `read` class (50/s, 200 per 4 s, per user): `make ui-renders` opens three pages of one user at once
  and reached 197 reads in 4 s. The UI development profile now admits 800 (deploy/ui-dev/up.sh). One person in one browser stays far
  below the default, so it is not a product defect. Not verified: restoring many tabs at once may reach the limit.
- T-18's e2e set `nuget` refused cold restores locally with 429 from Quader's own limiter. The classes were `package_read` (per token,
  200/s, burst 1000: about 430 refusals on each of two pods) and `package_anonymous` (per address, 20/s, burst 100: about 500 per pod).
  T-18 does not change the limiter, and on CI the set is green. To decide with the owner: do these budgets fit a cold restore of a
  real NuGet client, or many CI jobs that share one token?
- T-21 fix wave 3 is on main, from 64604a4c to c8c7eec3:
  - a project Owner sees the project's service accounts (UI-A-41 was the UI calling the instance-wide list);
  - the admin repository list shows a whole address (Q-UI-325);
  - a form field shows its error only after it was left or a submit was tried, in words;
  - admin tables stack at 390 px;
  - every collapsible section is the quiet disclosure;
  - the policy editor's patterns sit behind "Limit to package names or paths";
  - a German sign-in returns to the German UI.
- T-21 fix wave 4 is on main (587da115):
  - one Settings group in the admin area (Q-UI-326);
  - the filter panel beside a list only where the list keeps 71 rem, else between its button and the list (Q-UI-327, e609b22a);
  - no list scrolls sideways with the panel open, the scrollbar's 15 px included;
  - members and the sign-in page fit 390 px;
  - the dev server serves the file view;
  - Recently opened is left to the start page (Q-UI-329).
- The owner, 2026-11-12, on the admin repository list: "a huge field for copying a path that may not even be needed gets all the
  room, and every other important property squeezes itself into the artifact's row". The development agent had approved that render.
  Q-UI-330 (ff354135) gives each list its columns from its user's job:
  - the admin list has Project, Format, Class and Last activity as columns, and the address as a copy action;
  - the project list keeps the address field, with Format and Class as columns again (Q-UI-328 reversed).

  Open: in the project list the address is capped at 46 characters even where the Repository column has room; this is being changed
  so that the address takes the rest up to the longest address.
- The audit log names all 158 event types, generated from the registry, with a test that fails on a missing name (e5e4ca9d).
  Advisory sources disable the runs of a source that is off, and say why (Q-UI-394). Open:
  - the audit action filter's suggestions are a hand-made prefix list;
  - `class="muted"` styles nothing (being fixed);
  - the audit log scrolls 148 px sideways at 1280 px (the Source address column).
- `image (host architecture, no push)` was red in Test and Release 43254986257 (78bb2250): `npm ci` in the Containerfile's web stage
  ended with "Cannot find native binding" (npm's optional-dependency issue). The push changed neither package.json nor the lockfile,
  and the job was green at 539021fa. Re-run 1 was green, and v0.52.0 was released from it. The cause is not verified: a failed download
  of a platform package of an optional dependency is the suspect.
- `e2e (vuln)` was red at 539021fa: a stacked list clipped its head to 1 px, so the Detected sort button could not be clicked at 1280 px
  beside the filter panel. Fixed in a136b033 (Q-UI-391, Q-UI-392); the render screen `vulnerabilities-filtered` now clicks it at every width.
- `integration (shard a)` was red at 539021fa: TestQ_CI_47 failed at a replica's stop with "control pool (1 connection held)", as
  TestR_DEB_015 on 2026-11-11. One cause found and fixed in 781f09ab: a readiness probe held the control connection through the close
  (TestR_HA_011 reproduces it). One later run in the ci area showed the message once more, cause not known; since 2797e3ff the close
  error names the borrower and how long it held the connection. The recurrence in 43316531262 named "a connection under
  construction", which was a closed connection being cleaned up: H-822, closed by Q-ARCH-498 and Q-ARCH-499 (F-136 keeps its trigger).
- Many existing web specs carry decision IDs in their test names (`app.projects.spec.ts`, `app.routes.spec.ts`, `app.spec.ts`), where
  the convention is requirement IDs.
- T-21 vulnerability grades are on main (e4183c73, R-VULN-038 and 039, R-UI-090 to 094). Open:
  - for the owner, not blocking: should the report's trend chart and the bar share one palette?
  - the paging of a version's findings, the report table's sideways scroll and the wording of the facts line are being fixed.
- T-18 (visibility levels) is on main (run of 2026-11-12): private, internal and public for the installation's default, a project and
  a repository's artifacts and wiki; the external user flag; README and documentation as wiki content of npm, NuGet and Ansible;
  the Bastion never reads by the internal level. The e2e sets npm, ansible, oci and deb ran green on kind, as did the four
  `21-visibility` journeys and the external-user test; journeys 19 and 21 and the T-18 screens were rendered and looked at. Open:
  - `TestBastion_R_BAS_049_RoutingByPath` failed on the long-lived kind cluster (72 revalidations answered 304, 0 cache hits, no
    journal trigger in the 15 s window); CI's fresh cluster decides whether T-18 or the cluster causes it;
  - `13-ci` and `16-backups` were red on the long-lived cluster: 13-ci was the picker race (fixed in the product by Q-UI-334);
    16-backups shows `?state=failed` with both filters on Any and no runs;
  - the NuGet README route is still the P1 stub (R-NUGET-024): it answers 501 behind the wiki gate;
  - the repository settings' Access tab still shows include and exclude patterns, Disabled and Hide existence beside the visibility:
    T-21b rebuilds that page (repository-setup-work-packages.md).
- The landing of T-18 turned four CI legs red (Test and Release 43279576487, 43282191873, 43290893480). Their causes:
  - `make cyclo`: two T-18 functions were over 33. They are split, without a change of behaviour (2a3a40af).
  - Lint: a misspelling in a test (628caa9a).
  - `e2e (nuget)`, a product defect: the challenge of a request without a credential had moved behind the rate limits, so every
    first attempt of a NuGet client was charged to `package_anonymous` (burst 100 per address). Fixed in d5e4cec5 (Q-API-290) and
    proven on kind.
  - `integration (apikey)`, a product defect: a package route answered a request without a credential with a Bearer challenge
    instead of Basic. Fixed in 849512dc.
  - `integration (ci, oci)`: the tests still stated the behaviour from before T-18 and now state the T-18 decisions.
  - `e2e (access-long)`: two journeys looked for a project by its key, where the start page names it by its display name. A third
    exposed a Tokens section that folded itself shut when its last token was revoked. Both fixed in 1883de9b.
  - Unit tests: a test fake of the shadowing check had a data race (67c008aa).
- Open for the owner: three cold NuGet restores that share one token exceed `package_read` (burst 1,000 per 5 s). Locally 108 of
  1,158 requests were refused within 4 s, and the clients' retries recovered. Should the budget for package reads be higher?
- `messages.xlf` has drifted from `npm run extract-i18n`: 687 location notes and the unit order differ. No gate checks it.
- No integration test checks the 401 challenge of Debian or of the `formats.Base` formats without a credential, and no real apt
  client was run against it.
- The creation form of a local npm repository offers Security audit's "Forward to an advisory service" with Provider URL and Timeout,
  which a local repository ignores (it behaves as Off); the schema could show them on remote and virtual repositories only.
- No render walk switches a remote to mirror mode, so the OCI and Debian mirror settings are covered by unit tests only.
- Journey 23 (search) failed once on the long-lived kind cluster (Down and Enter opened a package page, not the repository) and is
  green on CI's fresh cluster (Test and Release 43304481590); not read.
- Open for the owner, not blocking:
  - should administrators add the installation's own registries as preset buttons (T-21b);
  - should the vulnerability report's trend chart and the grade bar share one palette (T-21c);
  - the package read budget question above.
- The Docker VM of the development machine was full (kind nodes at 238 of 252 GB) and blocked renders and kind runs. With the
  owner's consent, all unused images and the build cache were removed on 2026-11-12 (71 GB). The `make image` builds of a run leave
  dangling images behind; nothing cleans them.
- TestR_DEB_023 failed twice on CI (Test and Release 43022311289 and 43229158954, shard c). The test waited for the first index
  that lists a name, not for the whole scope; fixed in f2cc9a2d.
- UI development profile: the volumes of `deploy/ui-dev` are shared by the whole machine, but the key ring in `.dev/ui-dev` belongs to
  each checkout, so a second checkout writes a new key into the same database. [web-ui.md](../developer/web-ui.md) describes the
  workaround.
- `repository-detail` is the only settings page not yet in the one-column form of the [style guide](../developer/ui-style-guide.md#settings-pages) (Q-UI-222): what its tabs become needs the owner's reference at the walkthrough.
- H-836: a holder of `tokens:read` alone sees which token owners are administrators; only a custom role is affected, because the three system roles with `tokens:read` also hold `identities:read`.
- A disk watcher in the jobs, so that a lost runner can be read, was proposed and not built.
- The coverage badge and the evidence record stand still until the first stable release (Q-ARCH-457).
- Ansible Galaxy, open gaps on [formats/ansible.md](../spec/formats/ansible.md) and its security pages: H-861 (digest conflicts counted per merge), H-863 (a role lookup is not merged across members), H-868 (one name with versions in a local and a remote member is not run with the real clients), H-851 (the core's archive bounds), H-842 (the hub exchange is tested against the stub realm only). The base64 body of a publish is held to the system upload limit before `ansible.max_archive_bytes` sees the decoded bytes (Q-ANS-53), not tested.
- The internal secrets operator of the local kind cluster crash-loops (300 restarts on 2026-11-10); a fresh `make e2e SET=ansible` needs the operator pod deleted once so the stubs' tokens are generated. On 2026-11-11 its pod on the local cluster had restarted 216 times; its log is now kept in the e2e evidence (`kept-pod-logs/`) and not yet read. It is the suspect of F-127.

**Phase 7, not closed** (BETA.md stays 🟡): E5.9, E5.10 and E5.5 need green runs, E5.1 a run of the nightly job on a fresh cluster (the drill with the new commands passed on kind on 2026-11-10, Q-HA-289), H-798 is unexplained (drained pods killed after their grace period), and E5.6 misses 18 MVP IDs. The beta candidate is published only when every criterion is met.

**Housekeeping**: finished worktrees are removed at the end of a run, with the local branches of work that has landed (owner, 2026-11-11). On
2026-11-11 all 88 worktrees and 125 local branches were removed. Kept: two local branches whose work never landed, each with a WIP commit of
the changes left uncommitted on 2026-11-08: `worktree-agent-a135bce1ea0ac051f` (the clients-first test of the mirror) and
`worktree-agent-aa6a3feeee93a6b37` (the slow npm publish test, F-105, a temp sweep and an S3 cleanup of the test stack). The origin branches
`p7-*` still exist; deleting them is for the owner to approve. The local `docker buildx` is still behind its pin (see above).

## 9 · Owner review of 2026-11-06: tasks and open questions

The owner reviewed the UI, the documentation and the domain model with the development agent on 2026-11-06, on the branch
`ui-review` (worktree `/Users/hfi/repos/quader-ui-review`). What was decided is recorded in the spec; what is left is below. The
tasks come before the phase's own work packages (step 2, item 5), in this order; remove a task in the commit that finishes it.

### Build what the owner decided (spec first, then code)

No task is open. T-12, the logo's open point, ended on 2026-11-17 with the owner's new mark
([ADR-199](../decisions.md#adr-199--the-product-is-renamed-from-trove-to-quader-with-the-token-prefix-qdr_-and-a-new-mark)):
`web/public/brand/drafts/` and the preview on the sign-in page are gone.

The owner answered every open question of the review on 2026-11-06; a new question during the run is asked one at a time, with
options and a recommendation, and what depends on it is not built before the answer.

## 10 · Owner tasks of 2026-11-07: the next registries

The owner set the next registries on 2026-11-07: Docker, then APT, each with pull-through **and** full mirror from the start. They
come before the next unit of [FULL.md](FULL.md) (step 1, item 3) and after the findings of section 8; each is one phase. Set the
matching FULL.md entry (block B F4 for Docker, the block D row "Debian / APT repositories" for APT) to 🟡 when the phase starts and
to 🟢 with the evidence when it is done; remove the task here in the commit that closes it.

**Working mode (owner, 2026-11-07; changed for ALPHA on 2026-11-08 evening, rule 10).** Planning and building are done by separate
agents. The check step (3) below does not run during ALPHA: a builder lands its feature on its own green gates.

1. **Plan.** A planning agent (`voltagent-qa-sec:architect-reviewer` or `voltagent-core-dev:api-designer` with the format's specialist)
   writes the spec first: the format's spec page under [docs/spec/formats/](../spec/formats/) from its catalog page under
   [docs/features/formats/](../features/formats/), the requirements with IDs, the decisions as decision blocks, and the cut into work
   packages with their contracts (Go interfaces, OpenAPI, schema, chart values) and dependencies. The development agent reads the plan
   against the catalog and the existing formats before anything is built.
2. **Build.** One agent per work package (the format's specialist from the table of step 3), with disjoint directories or
   `isolation: "worktree"`; each returns its changed files and test results and does not commit.
3. **Check.** Agents that did not build the package: `voltagent-qa-sec:code-reviewer` over each package's diff,
   `voltagent-qa-sec:security-auditor` (and `voltagent-qa-sec:penetration-tester` for what parses upstream or client input),
   `voltagent-qa-sec:architect-reviewer` for the contracts, and a verifier that runs the real clients against the chart on kind. Every
   finding goes back to a builder, and the checker reads the fix. The development agent integrates, runs the checks of step 3 and
   commits.

No registry task is open: T-16 (Docker) and T-17 (APT, closed with the FULL.md block D row "Debian / APT repositories" at 🟢,
evidence in [apt-work-packages.md](apt-work-packages.md#24--closing)) are done. A new registry task of the owner is added here.

### Owner tasks of 2026-11-11

Set by the owner on 2026-11-11 (T-18 and T-19 after reading the galaxy_ng screens of his collections; T-20, the client addresses in full, is built: R-UI-065). All are confirmed features, not parked ideas, and
the next run takes them in this order, each planned by a planning agent (spec and a work-package plan) before anything is built.
T-18 is built (run of 2026-11-12, [visibility-work-packages.md](visibility-work-packages.md), R-AUTHZ-033 to 035, ADR-192). T-19 is built (run of 2026-11-11,
[galaxy-docs-work-packages.md](galaxy-docs-work-packages.md), R-ANS-033 to 046): the set `ansible` ran green on kind with the real clients and both browser journeys.

### Owner task of 2026-11-12

Set by the owner on 2026-11-12, after the repository list showed an address field of a different width in every row: "Our UI still
looks really bad and I expect more. A consistent design. Easy access to the product for simple users. Optional details for power users
and admins." A confirmed feature, not a parked idea; it runs beside T-18 and is planned and built under rule 11.

- **Status (run of 2026-11-11/12):** built and on main. `make ui-renders`; fix waves 1 to 4 and their follow-ups; the lists' columns by
  their users' jobs (Q-UI-330); field widths with a lint rule (Q-UI-335); the vulnerability grade as cogpit shows it (R-UI-090 to 094);
  T-21b, the creation of a repository as one form per format and the settings as one column of sections (R-UI-080 to 088,
  R-REPO-062 to 064), proven by `e2e (access-long)` on CI. What is left is in section 8.
- **T-21 · A UI that looks right.**
  - **Renders.** A command that brings up the UI development profile with realistic data and renders every screen, in both schemes and
    at three widths, as the three kinds of user below, into `.out/ui-renders/` with a contact sheet.
  - **Address fields.** In a list every address field has the same width and the list does not scroll sideways at the usual desktop
    widths; this replaces Q-UI-270 and Q-UI-271.
  - **Audit.** Every screen rendered and judged against the style guide, each defect one line with its screen and picture. The audit
    list is the work list of the fix waves.
  - **Three kinds of user.** The plain user who reads and installs, the power user who publishes and maintains, and the administrator.
    The plain user reaches the common job (find the repository or package, copy the address, set up the client) without detours and
    without settings in the way. What only power users and administrators need sits behind an optional disclosure, not in the main view.
    To decide in the spec: what each kind sees on the home page, the lists, the repository and package pages, and where the details go.
  - **Consistency.** One width, height, spacing and alignment per kind of element across screens. Row actions, empty states and page
    headers follow one pattern each. T-18's screens are rendered under the same rule before T-18 lands.
  - **Creating and configuring a repository, simple by default** (owner, 2026-11-12, after the creation flow of a local repository
    showed include and exclude patterns, "Disabled", "Require authentication" and "Hide existence" as the access step): the rules of
    [CLAUDE.md](../../CLAUDE.md#product-simple-by-default-details-on-demand) apply to every format and class.
    - The standard flow asks only what the repository needs to work. Access is the members and the visibility level of T-18.
    - Every other setting stays, in a detail view or behind an advanced-settings disclosure.
    - A remote repository's connection is tested where its upstream is entered, before the save. **Test connection** exists today
      (`upstream-settings.ts`, `POST /api/v1/projects/{projectKey}/upstream-tests`), but only inside the step "Access, upstream, OCI (Docker)
      and mirror settings".
    - Well-known public upstreams are one-click choices: for OCI, Docker Hub, Quay, GitHub (ghcr.io) and GitLab; the other formats'
      choices are decided in the spec.
    - Defaults are values in their fields. A field's width fits its expected input. The title names the format ("New OCI repository").
    - Defects seen in the owner's screenshots, to fix with it:
      - a step label that lists four topics;
      - a key field red with "does not match the pattern" before anything was typed;
      - administrator notes in the standard flow.
    - Planned in [repository-setup-work-packages.md](repository-setup-work-packages.md) (Q-UI-340 to 353, Q-REPO-320 to 327):
      - a page of format tiles, then one form per format titled with the format's display name;
      - the standard fields per class, with members and the deployment target for a virtual repository;
      - the upstream presets per format, served by the format plugins, and **Test connection** under the credentials;
      - one closed **Advanced settings** section;
      - the settings page as one column of sections.
      T-18 changes the same flow, so two builders build it after T-18 lands.
  - **Vulnerabilities shown as cogpit shows them** (owner, 2026-11-12). The owner showed cogpit's grade card and, beside it, Quader's
    column "! High 24", "really bad". The reference is the Trivy integration of the owner's cogpit (`internal/model/imagerating.go`,
    `internal/ui/fyneui/gradebadge.go`, `drawergrade.go`):
    - **A grade as a shield:** S, A, B, C, D, E or N/A, with the grade and the count that decides it ("E · 13 Critical");
    - **the rule it stands for:** "More than 5 Critical";
    - **a severity bar:** Critical, High, Medium, Low and Unknown, each segment as wide as its count, Unknown hatched;
    - **a line of facts:** the OS of an image, its end of life, and when it was scanned.

    Cogpit's rules apply:
    - S: no finding;
    - A: at most 10 Low or Unknown;
    - B: Medium, or more than 10 Low or Unknown;
    - C: High, or an OS at its end of life;
    - D: 1 to 5 Critical;
    - E: more than 5 Critical.

    Every place that shows vulnerabilities today uses it: a list shows the shield with its text, a detail page shows the card.
    - Open for the owner, not blocking: should administrators add the installation's own registries (for example the owner's GitLab
      registry) as preset buttons? The development agent's recommendation is not now, and as its own task if wanted.

### Owner task of 2026-11-12, for later: a search with suggestions

The owner showed his GitLab's search on 2026-11-12: "A sensible search, with suggestions and everything. This is how I picture it for us
later." It is a confirmed direction, not a parked idea, and it is built after T-18 and T-21, each planned by a planning agent first.

- **Status (run of 2026-11-11/12):** built and on main: the suggestions operation, the field with places and live groups, the results page,
  the per-user store of what was visited, the single exit that leads into the admin area only through a marked admin suggestion
  (R-API-060 to 064, R-UI-095 to 099), journey 23 green on CI.
- **T-22 · One search with suggestions.**
  - **The field.** One search field in the top bar for every user who is signed in, not only for those who may list every repository.
    Since the stopgap of 2026-11-12 (Q-UI-390) the field shows for every signed-in user and Enter opens `/search?q=` in the main
    area ([app.ts](../../web/src/app/app.ts)). The field gets a filter control that narrows the search to one kind.
  - **Before anything is typed**, the opened field already offers, as the reference does:
    - **Places:** the start page, the guides, the profile and its settings, and for an administrator the administration;
    - **frequently visited repositories:** the format's icon, the key, and the project and path in muted text;
    - **View all my projects;**
    - **frequently visited projects.**
  - **While typing**, suggestions are grouped by kind: repositories, packages, projects, and for an administrator users and groups.
    Each suggestion shows where the thing is. Everything is reachable by keyboard.
  - **Visibility.** A suggestion names only what the caller may read, under T-18's levels, and the hiding of a repository's existence
    holds. To decide in the spec:
    - the API that readers can read across projects (today the start page of T-21 searches packages only, Q-UI-311);
    - where "frequently visited" is kept: in the browser, as the reference does, or on the server;
    - whether the start page's package search becomes this same field.
  - **Never into the admin area** (owner, 2026-11-12, after typing "test" in the top bar and landing on the admin area's repository
    list with Enter): a search from the main area ends in the main area. Only a live suggestion that is marked as a page of the admin
    area, and shown only to an instance administrator, may lead there. Done as a stopgap in 5a584881: Enter opens `/search?q=` in the
    main area, for every signed-in user (Q-UI-390).
  - **Order changed** (development agent, 2026-11-12): T-22 is planned now, because the owner pointed at the search twice on one day.
    It is built as soon as T-18 has landed, since its results must respect T-18's visibility levels.
  - Planned in [search-work-packages.md](search-work-packages.md) (R-API-060 to 064, R-UI-095 to 099, Q-API-270 to 281, Q-UI-400 to 415,
    H-985 to 987): one suggestions operation plus the three existing lists for the results page; two builders after T-18. The
    development agent decided the planner's three open questions, for the owner to object to at the walkthrough:
    - an "instance admin" is the system role `admin` only (Q-UI-412), as the owner's word says;
    - "frequently visited" stays in the browser, as in the reference;
    - "Search in <project>" first inside a project is not part of T-22.

### Owner walk of 2026-11-18: the frontend

The owner walks the UI screen by screen on the UI development profile, as the plain user `sam`, the power user `priya` and the
administrator `ada`, in his own browser; the development agent looks at the same screen's render and records one objection per
entry. The walk is part of [phase 8](BETA.md#phase-8--walkthrough-and-beta-release). When the list is long enough, builders build
it, one per package, each under rule 11. An entry marked **owner** is the owner's word; an entry marked **seen by the development
agent** is not decided until the owner has said so.

- **Status (2026-11-18):** the walk paused after the plain user's screens (start page, repository list, repository page,
  packages, vulnerabilities, browse, search, profile); the power user's and the administrator's screens are not walked yet. The
  owner: "implement the findings first; I cannot review it as it is". Four builders build on base 3addcae4, each a whole package
  with its spec, tests, documentation and renders; nothing of it has landed yet:
  - **Lists:** W-1, W-3, W-4, W-6 and the list defects the development agent saw (the tables' right edge, broken cells in the
    vulnerability report and the token list, the search results' repository list, one way to write a time). **Landed on
    2026-11-18** (ADR-200, R-UI-100 to R-UI-103, Q-UI-420 to Q-UI-432). Decided differently from the development agent's
    proposal in W-4: the description stays under the key instead of in a column of its own, so that every repository list is
    built from the same cell (Q-UI-426). The admin area's repository list keeps its copy button for the address, because the
    owner's objection named the project's list; open to his objection. Not run locally: the journeys 02, 06, 08 and 13;
  - **Package page:** W-7 with the two API additions (last fetched and findings with a fix, per version) and a seed with many
    versions. **Landed on 2026-11-18** (R-UI-110 to R-UI-112, R-API-065, R-VULN-040, Q-UI-450 to Q-UI-458, Q-API-295,
    Q-VULN-160): a package opens on the table of its versions, a version's page is `…/<name>@<version>`. "Last fetched" is read
    from the stored download statistics of the version's main file, so a remote image tag without a stored file reads "Never".
    Not run locally: the journeys 08, 21, the vulnerability journey 15 and the two Ansible documentation journeys;
  - **Guides:** W-2, the guide and the client setup as one page, one label and style for **Set up client**. **Landed on
    2026-11-18** (ADR-202, R-UI-120 to R-UI-127, R-API-070, Q-UI-480 to Q-UI-488, Q-API-310), carried onto the three landed
    packages by a fifth builder, who also made the four agree: every **Set up client** opens the repository's own page (a lint
    rule refuses a route literal `/setup` or `/browse`), the action is the default button on a page and the quiet row action in a
    table, the list behind a format tile is built from the repository list's parts, the tiles use the page's width, and the
    package, version, repository and file pages write a time by R-UI-103. Not run locally: the journeys 02, 08, 09, 11, 17 and
    20. Seen by that builder and not fixed: the guides trail names a format by its key (`oci`); the grade reads "C · 2 High" in
    a repository's items and "1,145 findings · 112 fixable" in a package's versions; the header's menu button stands alone on a
    line at 390 px; an empty repository shows **Set up client** twice. Not looked at by anyone yet: the admin area's screens with
    the four packages together;
  - **Files:** W-8, and the defects of the repository's page (the long package name's row, the filter field's width, the file
    row's icon, the address beside **Set up client**). **Landed on 2026-11-18** (ADR-203, R-UI-010 to R-UI-014, R-UI-052,
    Q-UI-510 to Q-UI-521). The development agent looked at the builder's renders before it landed and sent four defects back,
    which are fixed in it: the path stood twice on the screen (breadcrumb and trail), a deep path started lines with its
    separator, an archive file carried an icon with a folder's outline, and **Delete folder** stood beside **Upload files**
    with the same weight. Not run locally: the journeys 02, 03 and 12, which need the kind cluster.
  After the four have landed: W-5's check in the render report, the two red render screens, and a render of every screen looked
  at again. Not built, because the owner has not decided them: what is listed under "Seen by the development agent" and assigned
  to no package above (the sign-in page, the vulnerability report by package and version, **Needs triage** for a reader, the
  chart above the findings, the tabs of the package list, the breadcrumb's key).
- **How the work is paced (owner, 2026-11-18, 12:35):** "Can we not go on locally first, without waiting for endless release
  pipelines? I would rather go on looking." And: "You may push, but the result is of no interest in that time." The results of
  the pipeline (watching runs, red jobs, releases) are attended between 17:00 and 20:00 and between 22:00 and 08:00 local time.
  In the rest of the day the development agent runs the local gates, pushes what has landed without waiting for its run, and
  keeps the UI development profile up with the newest local state, so that the owner can look and object. A red job of a run of
  the day is read and fixed in the next window.
- **W-1 · A project has its icon wherever it is listed** (owner; screens `projects`, `search-field`). The search's suggestions show
  a project with its letter icon (`quader-avatar` in [search-field.html](../../web/src/app/features/search/search-field.html)), and so
  does the project switcher of the side bar. The project table of the start page shows the name alone. Wanted: the same icon before
  the name in every list of projects. To check by the builder: the admin area's project list and the search results page.
- **W-2 · The guides are ugly and overloaded** (owner: "nobody wants to work with this"; screens `guides`, `guides-repository`).
  Before any content the page shows four boxed groups of radio buttons (package type, client, repository, repository class), a
  link to the client setup, a boxed table of contents, and then dense prose. **Direction decided by the owner (2026-11-18):
  context instead of choices.**
  - A guide opens from a repository, where the format, the class and the repository are known; it asks for none of them.
  - The guide and the client setup become one page: the snippets first, the explanation below them.
  - The navigation entry **Guides** shows the formats as tiles and nothing else to choose before a guide opens.
  - Not chosen: restyling the page with its four choices (it keeps them), and moving the guides into the manual (the text would
    lose this installation's addresses).
  - To decide in the spec: where the class is chosen for a guide opened from a tile, and what remains of the routes `/ui/guides`
    and `/ui/setup`.
- **W-3 · A long repository key breaks away from its icon** (owner; screen `repositories`, the row
  `release-candidates-for-the-customer-portal-frontend`). The visibility icon stands alone on the first line and the key starts on
  the second. The icon and the link are siblings in `th.wrap-name`
  ([repository-list.html](../../web/src/app/features/repositories/repository-list.html)), and a key that does not fit beside the icon
  moves to the next line as a whole. Wanted: the icon and the start of the key on one line in every row; a key that is too long
  breaks inside itself, beside the icon.
- **W-4 · The repository list of a project loses the client address** (owner: "useless at this place; without a guide or a
  context it is worthless; maybe we leave it out here for now"; screen `repositories`). This reverses the part of Q-UI-330 that
  kept the address field in a project's list, and the list rows of R-UI-065, Q-UI-300 and Q-UI-301. The address stays where its
  context is: the repository's page and the client setup. To decide in the spec: what the columns of the list are without it, and
  what the row offers instead (the development agent's proposal: the description as a column of its own, and **Set up client** as
  the row's action).
- **W-5 · The development agent finds such defects itself** (owner, about W-3: "Can you not see something like this yourself?").
  The render of that row existed and was looked at; `make ui-renders` measures what scrolls sideways and the widths of the address
  fields, and neither catches a broken line. Wanted:
  - the render report flags an icon that stands on another line than its label, and a key or name in a table row that is broken
    across lines or whose row is laid out differently from its neighbours;
  - at every station of the walk the development agent looks at the render first and names what it sees before the owner has to.
- **W-6 · The filters open as a drawer** (owner: the panel that opens beside the list is "extremely ugly"; "shall we try a
  drawer?"; screen `packages-filters`, and every list with a filter panel). Today the panel stands beside the list where the list
  keeps 71 rem and between its button and the list where it does not (Q-UI-327); the list is rebuilt narrower when it opens, and the
  panel is a box of five closed sections with a button. Wanted: a drawer that slides in from the right over the page, the list
  keeps its width and its columns. One component, `quader-filter-panel`
  ([filter-panel.ts](../../web/src/app/shared/ui/filter-panel.ts)), carries the filters of twelve lists (packages, repositories,
  browse, vulnerabilities, projects, users, groups, service accounts, policies, tokens, the audit log, the CI issuances), so the
  change reaches all of them at once and replaces the two layouts of Q-UI-327 by one. The app has no drawer for content yet; the
  only one is the navigation's at narrow widths. To decide in the spec: whether the filters stand open in the drawer instead of as
  closed sections.
  - **The search by name stays outside the drawer** (owner, 2026-11-18, on the development agent's proposal): a field above the
    list, as the repository page has it (**Filter packages**). Today a name is three steps away: **Filters**, the section
    **Name or description**, **Search**.
  - **The filters that are set show beside the button** (owner, 2026-11-18): to the right of **Filters**, hinted or in full, while
    the drawer is closed. To decide in the spec: one chip per set filter with its value and a way to remove it, and what a row of
    chips becomes when it does not fit the width.
- **W-7 · A package's page is its versions** (owner: the page is "very meaningless at present"; screens `package-npm`,
  `package-npm-graded`, `package-oci`). The owner's standard case: a user navigates through the UI and looks for a certain
  version of a dependency, or wants to know the vulnerabilities of that version. The page opens on the tab **Metadata** with two
  lines; the versions are the fourth tab, each as a stack of labelled lines, and the version is chosen from a dropdown with a
  checkbox for pre-releases.
  - **Wanted (owner, with a reference screen of a registry's artifact list):** a click on a package shows its versions as a table,
    one row per version: the version, its size, its vulnerabilities (the grade, the total of findings and how many have a fix),
    when it was published and when it was last fetched. The content is the reference's, the design is Quader's own: nothing of
    the reference's look is taken over.
  - **Hashes are not at the start of a row** (owner: "nobody actually reads them"). For an image the tag leads; a digest is a
    detail of the version.
  - What the API has today: `PackageVersionSummary` carries `version`, `prerelease`, `publishedAt`, `sizeBytes`, `repositories`
    and `downloadCount` ([openapi.yaml](../../api/openapi.yaml)), and the tab already shows the grade per version. What it lacks:
    when a version was last fetched, and the number of findings with a fix per version (`fixAvailable` exists as a filter of the
    findings and in the summary of a report).
  - Folded in from what the development agent saw: the tab says **Vulnerabilities (1145)** and the grade says **E · 13 Critical**
    (the row's grade, total and fixable replace both); the install command is cut off in its field; the page that opens has no
    description and no README.
  - The seed of the renders gives `image-convert-tools` one version. It needs a package with many versions, differing grades,
    pre-releases and tags, or the new page cannot be judged.
  - **A version has a page of its own with an address of its own** (owner, 2026-11-18, on the development agent's proposal):
    the grade card, the findings, the install command, the files and the metadata of that one version. It can be linked and sent,
    and the list of findings has the page's width. Not chosen: a row that opens inside the table (a thousand findings do not fit
    into a row), and a drawer (too narrow for the findings, no address).
  - To decide in the spec: the columns and their order, the sort and the search within the versions, the path of a version's
    page, and what becomes of the tabs **Metadata**, **Files** and **Repositories**.
- **W-8 · Browse goes from the navigation; the files live on their repository's page** (owner, 2026-11-18, on the development
  agent's proposal; screens `browse`, `browse-folder`, `browse-file`, `repository-generic`). This answers the question that was
  open since 2026-11-10 (section 8). **Browse** and the page of a repository both show the repository's files; the file
  operations exist only in **Browse**. Wanted: one way to the files, from the repository. The file operations move to the
  repository's page, each for the roles that may use it. Not chosen: keeping both, and keeping **Browse** for owners and
  administrators only. What it costs: the tree across the repositories of a project is gone.
  - **Corrected on 2026-11-18:** the question of 2026-11-10 and this entry said that Browse carries upload, copy, move and the
    trash can, and the development agent told the owner so as a checked fact. Checked was only that the repository's page offers
    no operation. Browse had upload, new folder, delete and the copy buttons for addresses; copy, move and the trash can were
    never built, their storage operations (R-STOR-019, R-STOR-022, R-STOR-023) are open milestones of [FULL.md](FULL.md). The
    builder found it. So nothing but the tree across repositories is lost with Browse, and the place of copy, move and the trash
    can on the repository's page is decided in the spec for when they are built (Q-UI-515, Q-UI-516). To decide in the spec: where the files of a
  repository that is not generic show (npm, NuGet, OCI, Debian, Ansible), what becomes of the routes under `/browse`, and where
  the trash can of a project lives.
- **W-9 · The shield carries the letter; it is not written again beside it, and the text stands on the shield's middle**
  (owner, 2026-11-18, looking at the versions table that W-7 built: "something is not centred"). Measured in the running UI: a
  table cell's line is 16 px, the shield 20 px, and the shield's place was computed for a line of 21 px, so its middle stood 3 px
  below the text's; a cell with a badge had a line of 20 px, so the version stood 2.5 px lower than the size and the times
  beside it. Built at once, locally: the letter and its separator are hidden from sight beside a shield (a screen reader still
  hears them), in the list grade, the version grade and the card; the shield is centred on the line it stands in by that line's
  own height, and a badge in a table cell too (R-UI-091 revised).
- **W-10 · The project switcher has the room a choice of projects needs** (owner, 2026-11-18: "far too squat; it is not
  necessary"). The panel had the side bar's width: a name was cut to "Customer p…" and its key broke into three lines beside it.
  Built at once, locally: the panel is 24 rem wide and reaches over the page, and an entry is the mark, the name, and the key
  under the name, each on one line.
- **W-11 · An administrator sets the default visibility of a project and restricts visibility levels** (owner, 2026-11-18, with
  the general settings of his GitLab as the reference: "in the administration area I must be able to set these parameters
  through a setting: default project visibility, restricted visibility levels"). The first exists: **Admin area › Settings ›
  General › Visibility › Default visibility level** (`defaultLevel` of `/api/v1/settings/visibility`, R-AUTHZ-033, R-API-056), in
  a section that is closed when the page opens. The second does not exist. Wanted: the levels an administrator restricts can
  be chosen by an administrator only; whoever else sets the level of a project or a repository cannot choose them.
  **Built on 2026-11-18** (ADR-204, R-AUTHZ-037 and R-AUTHZ-038, Q-AUTHZ-283 to Q-AUTHZ-289, R-API-075 and R-API-076, Q-API-330
  to Q-API-332, R-UI-140 and R-UI-141, Q-UI-540 to Q-UI-543; gaps H-1001 and H-1002 on the security page of visibility): the
  section is open and reads **Default project visibility** and **Restricted visibility levels**, one **Save changes** under the
  two; `restrictedLevels` beside `defaultLevel`, `GET /api/v1/visibility-choices` for the forms, the refusal
  `403 visibility_level_restricted`, one expand-only schema step. An administrator here is whoever holds
  `security-settings:write`. The default is never restricted; an object keeps a restricted level and its Owner may lower it; a
  creation that names no level takes the highest level its caller may choose. Projects are created by administrators only, so
  the restriction binds the Owners of projects for their repositories. The development agent looked at the settings page in the
  running UI and moved **Save changes** from under the list **Public now** to under the settings. Not looked at: the forms with
  a level restricted, as an Owner. Not run locally: the journeys 19 and 21.
- **W-12 · A person is chosen from a search that shows the people it found, everywhere** (owner, 2026-11-18, at the admin
  area's **System roles**, with a picture of the member field of his GitLab: the typed letters "Swa", under the field a row
  with the avatar, the name and the username of the match): "ne, das muss mit Personensuche passieren. Personensuchen immer so
  mit Anzeige der möglichen Personen. Das will ich überall wo Personensuchen eine Rolle spielen." Today a system role is bound
  by typing `user:name`, `group:name` or `sa:name` into a text field, a group's member and the principal of a policy's "Try it"
  likewise. The members of a project and the principals of a project's policy are already found through a search box
  (`quader-principal-picker`, with the lookup `GET /api/v1/projects/{projectKey}/principals` of Q-AUTHZ-182), but its matches are
  buttons with the kind and the name, without avatar and display name, and it is not used in the admin area. Wanted: one
  shared field for every place where a person, a group or a service account is named: it searches while one types and lists
  the matches with avatar, name and username. Decided by the development agent: no caller gains a search it does not have
  today (the admin area's caller holds `identities:read`, a project's Owner has the lookup of Q-AUTHZ-182); the lookup's
  answer grows by the display name and the avatar, never the email address, and H-392 says so. Corrected the same day: the
  development agent had first written that no screen offers a person while one types and had decided to open the search to
  every signed-in user who is not external; it had not read the project dialogs. The builder was redirected before it
  committed.
  - **Built** (on `main`; R-UI-150 to R-UI-152, R-API-077, R-API-078, Q-UI-580 to Q-UI-585, Q-API-335, Q-API-336; Q-AUTHZ-182
    and H-392 rewritten): `<quader-principal-field>` replaces the old picker and every typed reference, in twelve places (the
    binding of a system role, a group's and a project's members, a user's groups, the first Owners of a new project, the
    entries and "Try it" of a policy, the principal filter of the policies, a CI mapping's groups, the owner filter of the
    tokens, the actor filter of the audit log, the group filter of the users). Inside a project it asks the lookup of
    Q-AUTHZ-182, elsewhere the search's suggestions, which need `identities:read`. A group's members show avatar, name and
    username.
  - **Looked at** in the running UI as `ada`, dark, 1440 px: **Bind to** of a system role (typing "sa" lists Sam Taylor with
    avatar and `@sam`; the choice is a chip and **Bind** becomes active), the owner filter in the tokens' drawer, a group's
    members. Fixed by the development agent before the landing: the list of matches was 24 rem wide in a drawer of 21 and
    left it at the side; the line under a member's cell stood half a pixel under its neighbours' (707.0 against 706.5,
    measured), because the cell was a flex box.
  - **Open:** the table of a system role's bindings still writes `user:ada`; a person who has never signed in cannot be
    found, and nothing invites one; not looked at: a project's **Add member** as `priya`, the policy editor, the dialog of a
    new project, 390 px, the light scheme. The integration tests of the lookup and the suggestions and the journeys 03 and
    12 were changed and not run.
- **W-13 · An update does not turn the status red** (owner, 2026-11-18, at **Dashboard** and **System status**, both red with
  "A role is not ready" and "Replicas unready" on his stack): "Ich denke, da wurden einfach die Prozesse rotiert. Bitte
  nachprüfen, dass solche Prozesse sich abmelden und es nicht zu rotem Status kommt, nur weil ein Update eingespielt wurde."
  Checked by the development agent on 2026-11-18:
  - **Verified:** a process that is stopped cleanly signs off. `Registry.Stop` deletes its row
    ([replica_registry.go](../../internal/cluster/replica_registry.go), tests in `replica_registry_stop_test.go`), and on the
    UI stack a `TERM` to the running process ended it after 5 s (the drain delay) with its row gone from `replica`. The
    chart gives a pod 120 s (`terminationGracePeriodSeconds`) for that.
  - **Verified:** the two rows that made the stack red were in the state `draining` with a heartbeat that had stopped: both
    processes had received the `TERM` and were killed inside the 5 s of the drain delay, before the deletion. That comes from
    how the backend of the UI stack was restarted that day, not from what an update does. Not read: which command killed them.
  - **The gap, verified in the code:** `serviceState` in
    [system-status-model.ts](../../web/src/app/features/system/system-status-model.ts) is red as soon as one row is lost or
    not `ready`. So (a) every rolling update shows "Replicas unready" while a pod drains, because `draining` is not `ready`;
    (b) a row whose process was killed while draining, or that could not delete its row in time, stays "lost" and red until
    the prune after 24 hours (`replicaPruneAfter`), though the process was stopped on purpose; (c) a pod that crashed and
    was replaced keeps the status red for 24 hours while every role has its ready replicas.
  - **Not verified:** a rolling update on a cluster that day; the drain legs that would show it run nightly and were not read.
  - Wanted: a replica that drains or has drained is no error; a row that went stale while `draining` counts as signed off.
    Decided by the owner on 2026-11-18 ("Option A", from three: a warning / red as before / not in the header at all): a
    replica that was lost without draining, while its role still has ready replicas, is a **warning**, not an error; its row
    stays in the table as lost until the prune; red is for a role without a ready replica.
  - **Built** (1785aff2 on `main`, Q-OPS-210, Q-UI-600 to Q-UI-604): a replica that drains or starts informs ("Replicas are
    being replaced"); a row that went stale while `draining` has signed off (the list leaves it out, the prune deletes it);
    a replica lost without draining is a warning with the time of its last heartbeat; "Not drain-safe" informs; red is "No
    ready replica of the role …" or a ready replica that fails a gating check. Looked at on the owner's stack after a restart
    with the new build: **System status** reads "All roles ready" with "Not drain-safe" as information, the two stale rows
    are gone from the list, the **Dashboard** is green and has no "Nodes 0". Read from the code by the builder, not run on
    a cluster: a normal rolling update leaves no row (120 s of grace, the deletion has 5 s of its own). **Open:** a database
    that cannot be reached during the whole shutdown keeps even the mark `draining` from being written; that row is shown
    as lost, a warning, for 24 hours. `TestE14Drain` now asserts that no drained pod is listed and every role has a ready
    replica; changed, not run.
- **W-14 · A missing groups claim is written to the audit log when it changes, not at every sign-in** (owner, 2026-11-18,
  "Option A" from four: only at the change / only when memberships were removed / as before but as a warning / group sync
  off by default without rules). Seen on his stack: every sign-in of a user whose provider sends no groups wrote a second
  entry "Group claim missing at sign-in · failure", half of the log. Verified in the code: group sync is on by default with
  `on_missing_claim: clear` ([login.go](../../internal/config/login.go)); `syncGroups` in
  [provisioner.go](../../internal/auth/principal/provisioner.go) records `user.groups.claim_missing` with the outcome
  `Failure` at every such sign-in, also when no membership was removed; the spec calls the event's severity "warning"; the
  last state of the claim is stored with the identity (`groupClaimMissing`). Wanted: one entry when the claim is missing
  for an identity for the first time, and one when it is back; shown as a warning, not as a failure; the metric
  `quader_auth_group_claim_missing_total` counts every sign-in as before, for alerting.
  - **Built** (ad2e3ac2 on `main`, Q-AUTH-400 to Q-AUTH-403, Q-UI-610): one entry when the claim goes missing for an identity or
    its reason changes, a new entry "Group claim back at sign-in" (`user.groups.claim_returned`) when it returns; `warning` is
    a fourth outcome beside success, failure and denied (migration 00078), also in the filter; a user's page showed the stored
    state already, under **Groups**, and now says since when and what the policy does. Looked at on the owner's stack: three
    sign-ins of `sam` after the restart wrote one entry, shown as a yellow **Warning**; the user's page reads "Since Nov 18,
    2026, 5:28:05 PM, sign-ins through ui-dev carry no group claim. The groups synchronized from it are removed at each of
    them." Not looked at: the claim's return (the stack's provider never sends groups; the integration tests cover it).
    Entries written before the change stay as they are, red.
  - **Open, from the builder's report:** the spec calls two more events warnings that are not written as one: a result of a
    group rule that violates the group-name rule is "skipped with a warning event" (R-AUTH-008), but only logged
    ([claims.go](../../internal/auth/oidc/claims.go)); `system.backup.completed` with `completed_with_warnings` is written as
    success. R-AUTH-041 calls the break-glass events' severity "high", which the model cannot say.
- **W-15 · The admin area's projects list has no column "Your role"** (owner, 2026-11-18, "Option A" from three: remove it
  there / keep it with an en dash / replace it by **Created**). The list exists only in the admin area; the caller's role is
  empty in every row for an administrator who is no member, and the main area's list of projects shows it where it matters.
  The freed room goes to the project's name. Status: handed to the builder of the admin lists.
- **W-16 · A project's mark is white and lights up amber under the pointer** (owner, 2026-11-18, with a picture of the admin
  area's projects list): "Ich hätte gerne dass diese Projekticons weiß sind und bei hover über der Zeile Bernsteinfarben
  aufleuchten wie die Farbe von unserem Icon." Built by the development agent: the mark is a stone of Quader's mark with the
  project's letter, and the mark's coloured stone while the pointer or the keyboard is on its row, link or option (Q-UI-424,
  `--quader-mark-letter`). In the light scheme the stone is the dark stone of the mark (the development agent's choice: a
  white tile on a white page is not seen). Looked at in the running UI, dark and light, in the start page's list and the
  admin area's repository list. Landed with the admin lists' package (76d99201, 5720d3fe).
- **W-17 · The security dashboard is the auditor's, under "Your work", and has one filter** (owner, 2026-11-18): "Option A"
  for the filter (from three: one filter at the list's head / the upper form as the scope of the whole page / the form moved
  down to the list): the form above the counts with **Advisory ID**, **Package starts with**, **Project key** and **Search**
  goes; the project becomes a filter of the drawer beside the advisory and the package that are there already. And: "beim
  Security dashboard handelt es sich nicht um ein Admin Feature, sondern um ein Auditor-Feature. Für Auditoren oder
  Administratoren gehört es unter Your Work als zweite Kategorie "Security Audit", darunter dann Security Dashboard.
  Sichtbar nur für Instanz-Auditoren und Instanz-Administratoren."
  - **Built** (on `main`; R-UI-158, R-UI-159, Q-UI-620 to Q-UI-626, Q-AUTHZ-295; Q-UI-130 and Q-UI-165 rewritten): the page is
    `/security` of the main area, in a folding section **Security audit** of the side bar under **Your work**;
    `/admin/security` is gone, **Advisory sources** stays in the admin area. The form above the counts is gone; the list's one
    search finds by advisory, alias, package or summary (a whole identifier is sent as the indexed advisory filter); the
    project is the drawer's first filter, check boxes with mark and name. A line says what the counts cover: they follow the
    project, repository and format filters only, as before. The UI development profile has an auditor, `noor`.
  - **Who sees it:** the page asks for `content:metadata-read`, which `admin` and `auditor` hold
    ([roles.go](../../internal/authz/roles.go)). **For the owner:** `security-admin` holds `vulnerabilities:triage` but not
    `content:metadata-read`, so that role has no dashboard since the admin area's entry is gone; no capability was taken from
    or given to a role.
  - **Looked at** in the running UI: as `noor` (bound to `auditor` through the new person field) the section and the page,
    dark at 1440 px and light at 390 px, and the drawer; as `priya` no section and "Page not found" at `/ui/security`, inside
    the shell. Fixed by the development agent before the landing: a project's mark stood 5 px above its name in the drawer's
    rows (measured), because the mark centred itself on its own letter's line; it now centres on the line of the text around
    it, everywhere.
  - **Open:** the journey of the dashboard and the alias case of the integration test were rewritten and not run; the
    triage controls as `noor` and a finding's page under `/ui/security/findings/…` were not looked at.
  - **Corrected by the owner the same evening:** "Security-Audit hatte ich eigentlich als weiteren context-title erwartet."
    The development agent had briefed "a second section under Your work" and the builder made a folding group inside the
    block of **Your work**. Wanted: **Security audit** is a second context title of the side bar, written and placed like
    **Your work**, with **Security dashboard** as a plain entry under it. Built with W-20 (cf297363).
- **W-18 · A row with one action names it, a row with several has a menu** (owner, 2026-11-18, "Option A" from three: write
  the practice down as the rule / a menu everywhere / icons everywhere). Verified in the code: the lists with a menu
  (repositories, a project's members, files, exposure rules) have several actions per row; the lists with a word (**Edit** on
  projects, **Revoke** on tokens and a Bastion's tokens, **Unbind** on system roles, **Remove** on a group's members,
  **Details** on CI issuances) have one. Two lists do neither: the CI mappings show **Edit** and **Test** as two words, the
  Debian signing keys two buttons. Wanted: the rule in the spec and the style guide (one action: its word as a quiet button;
  two or more: the menu, with at most one frequent action as an icon beside it), and the two lists brought to it.
  - **Built** (2ef5951b on `main`; R-UI-163, Q-UI-640 to Q-UI-644): the rule in the spec and the style guide; a menu for the CI
    mappings (a reader keeps the word **Test**), the Debian signing keys and the saved views of the package list; the word
    **Remove** for a project's members, whose menu had one entry; a lint rule (`row-actions-rule.mjs`) refuses two word
    buttons in one cell. Kept as two words on purpose: **Rotate** and **Revoke** of a token (Q-UI-641: **Revoke** never
    stands behind a menu). Looked at: a project's members as `priya`. Not looked at: the three new menus. The journeys 12,
    13 and 17 were changed and not run.
- **W-19 · Jobs and Backups are tabs of System status** (owner, 2026-11-18, "Option A" from three: tabs on the page / two
  entries of their own in the side bar / leave it and move the links up). Verified in the code: both pages are reached only
  through two text links at the end of the long **System status** page (jobs also from a card of the dashboard), the side
  bar keeps **System status** marked on both, and the dashboard shows no state of the backup. Wanted: one entry in the side
  bar, three tabs at the top of the page, **Status**, **Jobs**, **Backups**, each with its address. Added by the development
  agent, for the owner to object: the dashboard shows the state of the backup ("No backup yet", the last success), so that a
  missing backup is seen without two clicks.
  - **Built** (7c5bd389 on `main`; R-UI-164, R-UI-165, Q-UI-650 to Q-UI-654): one header with the refresh controls, the tabs
    under it, each at its old address; the side bar marks **System status** on all three; the breadcrumb ends with the tab;
    the links at the end of the status page are gone; the dashboard's card **Operations** has a row **Last backup** and
    **View backups**. Looked at as `ada`, dark, 1440 px: the three tabs and the dashboard's row ("No backup yet").
  - **For the owner:** the row **Last backup** stands in the last card at the bottom of the dashboard; a missing backup does
    not catch the eye there. A project's Owner who follows a repository screen's link to the jobs lands on a page titled
    "System status" with an empty side bar and the breadcrumb "Admin area", and sees only the jobs of the project (Q-UI-652;
    it was so before the tabs). The journeys 06 and 16 were changed and not run.
- **W-20 · "Recently opened" leaves the start page and goes to the side bar; no address field outside Set up client** (owner,
  2026-11-18, with a picture of the start page as the auditor): "Unter projects macht "recently opened" irgendwie keinen Sinn,
  sieht nicht aus. Lass uns lieber "recently opened" im side-menu zentral anbieten, falls jemand das tatsächlich nochmal
  nachschlagen will. Unter projects will der Nutzer eher "neues" finden oder Artefakte erkunden. Vor allem diese langen
  Adressfelder sind außerhalb von Set up client irgendwie sinnlos." Verified in the code: the list is the newest of up to 20
  repositories kept in the browser's storage per person ([frequent-places.ts](../../web/src/app/core/frequent-places.ts)),
  shown on the start page with each repository's address in a field, a copy button and **Set up client**
  ([projects-home.ts](../../web/src/app/features/projects/projects-home.ts)); the search field of the top bar offers the
  frequently opened repositories from the same store when it opens. Wanted: the start page is the list of projects alone
  (one heading, not "Projects" twice); the recently opened repositories are offered from the side bar; the address of a
  repository stands in a field only on **Set up client**. Decided by the owner on 2026-11-18 ("Option A passt für mich", from
  three: a folding section with the last five repositories as links / an entry that opens a page / the search field alone):
  a folding section **Recently opened** under **Your work** with the last five repositories as links, not shown while there
  is none.
  - **Built** (cf297363 on `main`; R-UI-166 to R-UI-169, Q-UI-660 to Q-UI-664; R-UI-096, R-UI-126, R-UI-158 and Q-UI-620
    rewritten): the start page is one heading and the list of projects; the side bar of the main area has the folding group
    **Recently opened** (a project's mark and the repository's key per entry, a long key cut with the full name as title),
    folded at first and remembered; a remembered repository is confirmed with one request per session and name, so none
    appears that the caller cannot read. **Security audit** is a second context title, written like **Your work**, with
    **Security dashboard** as a plain entry; a side bar is a list of titled blocks now (Q-UI-663).
  - **Addresses in a field outside Set up client, removed with it:** the repository page's header (it reads **Set up
    client**, **Settings**, the menu) and the client address in a repository's settings. **Left, for the owner:** "Address
    for downloads by path" in a repository page's **Details** (Set up client does not show that address) and "Client
    address on this endpoint" in the admin area's exposure rule editor.
  - **Looked at** in the running UI as `noor`, dark at 1440 px and light at 390 px: the start page, the side bar with the
    group open after four repositories were opened (marks and keys on one line, two long keys cut), both context titles
    (measured: the same size, weight, colour and left edge), a repository's header wide and narrow; as `sam` no Security
    audit. A browser without a history shows no group. Not looked at: `ada`'s admin area's title, an anonymous visitor.
  - **Open:** the journey `vuln/16` and the render screens `sidebar-recent` and `sidebar-auditor` (in place of
    `start-recent`, one of the two screens that ran into the render timeout) were changed and not run; a repository deleted
    during a session stays in the group until the next sign-in or change of permissions and then leads to "not found".
- **The owner on the power user's screens (2026-11-18):** "nothing gross struck me"; he asked which details the development
  agent would change. Its list, which it builds: the members list on the head row of every list and without its single tab; the
  wiki's visibility behind a disclosure on the repository's settings; the description field's width and the one disclosure
  that opens with the browser's triangle on the creation form.
  **Built on 2026-11-18** by a builder without the render stack, which was the owner's, and checked by the development agent with
  pictures of the running UI (R-UI-142 to R-UI-144, Q-UI-550 to Q-UI-556): the members list; a description as wide as a name in
  every form; one disclosure in the app, with a lint rule against a bare `<details>`; the guides trail with the format's name; an
  empty repository with one **Set up client** and a sentence; a repository's header at 390 px; one statement of a grade in every
  list ("1,145 findings · 112 fixable"). The pictures showed one thing the builder's reading of the styles had not: the
  description of the creation form kept the form's width, because a later rule gave every text area the full width; fixed.
  Not done: the wiki's visibility behind a disclosure, which waits for W-11 in the same forms.
- **W-21 · An ended session must not leave the visitor in front of an error on every page** (owner, 2026-11-19, with a
  picture of **Guides** on the local stack: a red notice "The session has ended. Sign in again." and a request ID): "Wenn
  ich nicht angemeldet bin, und auf Guides klicke bekomme ich diese Fehlermeldung. Wie ist hier unser Intent?" Read from the
  stack's log (the request ID of the picture) and reproduced: the browser still sent the cookie of an ended session; the
  server answers that cookie with `401 session_expired` on every route and never as a request without a credential
  (R-AUTH-001, wanted); the UI took the 401 of `whoami` at load for "not signed in", and since a repository is `public` it
  started as a visitor, with a cookie it cannot delete (`HttpOnly`): `GET /api/v1/formats`, the projects and the banner
  all answered 401, where the same requests without a cookie answer 200. The case Q-AUTH-177 named as its revisit
  condition. The intent of the specification: a visitor without a credential gets the guides without credential steps
  while something is `public`, and never an error there. **Decided by the owner on 2026-11-19** ("Option A", from three:
  the sign-in screen with the notice that the session has ended / on as a visitor without a word / the server counts a
  dead cookie as no credential): a browser whose session has ended lands on the sign-in screen, which says so and offers
  to sign in or to continue without signing in. The development agent's part of it: `whoami` takes the dead cookie away
  with its `session_expired`, as the sign-out does, on that one route only. To a builder on 2026-11-19.
  - **Built** (on `main` the same day; R-AUTH-001, R-AUTH-002, R-AUTH-013, R-UI-004 and R-UI-057 rewritten, Q-AUTH-410 to
    Q-AUTH-414, Q-UI-710 to Q-UI-713, Q-AUTH-177 points to its revisit, gap H-1004): `GET` and `HEAD` of `whoami` answer a
    refused session cookie with `401 session_expired`, `Cache-Control: no-store` and the two clearing lines of the sign-out,
    the same answer for a malformed, an unknown and an ended cookie; every other route leaves the cookies alone. The UI
    reads the problem's code: `session_expired` at load goes to `/login?returnTo=…&ended=1`, also with public content; the
    screen says "Your session has ended. Sign in again to continue where you left off.", takes the parameter out of the
    address, and with public content does not redirect to the identity provider by itself. H-1004 is the window that
    stays: a `whoami` in flight with the old cookie clears the cookie a sign-in in another tab has just set, and that
    person signs in once more. The builder ran the login package of the integration tests with its new case; the changed
    kind test `e1_01_login_test.go` is not run.
    **Looked at by the development agent** on the owner's stack after the backend's restart, in a browser with a dead
    cookie, light and dark at 1,440 and 390 px: `/ui/guides` lands on the sign-in screen with the notice, the browser has
    no cookie left, a reload shows no notice, **Continue without signing in** opens Guides with its six tiles, no request
    besides `whoami` answered 401 and no page shows an error notice.
- **W-22 · A public remote repository for the guidedtraffic images** (owner, 2026-11-19): "kannst du auch bitte ein public
  remote Repo hinzufügen. Nutze doch bitte https://hub.docker.com/u/guidedtraffic als remote und mach es zugänglich alle
  pull through auch für nicht angemeldete user." Read by the development agent as: on the stack he is looking at, and in
  its seed. Done on the running stack through the API: `platform/guidedtraffic`, OCI, remote, Docker Hub, artifacts
  `public`, `includePatterns: ["guidedtraffic/*"]`. Verified without a credential: the manifest of
  `guidedtraffic/kubectl:latest` 200 (fetched from the upstream), `library/alpine` through the same repository 404 (outside
  the pattern), the same image through `docker-hub-remote` 401 (not public). The create answered the warning
  `public_populates_remote`: a request without a credential fills the cache, which is what was asked for, bounded by the
  pattern. Not known: whether the owner also wants it on his installation, which cannot be reached from the development
  machine.
- **W-23 · A control beside a text stands on the text's line, on every screen, and the renders hold it** (owner,
  2026-11-19, with a picture of the images of `platform/guidedtraffic`: the button that opens a row sits in the middle of
  the row, the image's name and the other cells at its top): "Wieso ist denn wieder nur das Icon am Anfang mittig und die
  Schrift ist wieder nicht richtig ausgerichtet. Kannst du das bitte einmal für das gesamte Frontend überprüfen und ein für
  alle mal absichern..." Measured in the running UI: the row is 65 px high, the button of 32 px has its middle at 32 px, the
  name and every other cell their line's middle at 24 px. Why the render checks of W-5 let it pass, read in
  [layout.ts](../../web/e2e/renders/layout.ts): the screen is in the set (`repository-oci`), but `icon-apart` fails only
  when an icon and its label share no height at all, it looks for the label among the icon's siblings and so never compares
  a button's glyph with the text beside the button, and `row-offset` compares the texts of a row's cells with each other,
  which here all stand at the same height. To a builder on 2026-11-19 (R-UI-175 to R-UI-178, Q-UI-720 to Q-UI-729): the
  check becomes one of middles within a tolerance, for icons and for controls, in a row of a table and in any row of
  boxes; every finding of a full run is fixed at its shared cause. The owner's stack is given to the builder for its
  render runs. **The checks also run in CI, decided by the owner on 2026-11-19** ("Ja, die Renderprüfung soll auch
  in der CI laufen."): a job of Test and Release on every push, without a path filter, outside the needs of Semantic
  Release until the first stable release (Q-ARCH-457); given to the same builder (Q-ARCH-510 to Q-ARCH-512).
  - **Built** (on `main` the same day; R-UI-175 and R-UI-176, Q-UI-720 to Q-UI-726, Q-ARCH-510 to Q-ARCH-512; 926
    requirements, 631 of them MVP): one rule for the whole UI, a control or an icon beside a text has its middle on the
    middle of that text's first line within 1 px and does not make the line taller (`--quader-line`, the class `on-line`
    for a control of 32 px, badges of a fixed 20 px). The owner's row: 65 px with the button's middle at 32 and the name's
    at 24 before, 49 px with both at 24 after. The checks measure middles now and take a button's glyph for the button and
    the nearest text of its row for its label; 32 fixture pages hold them, one of them the owner's row (`make
    ui-layout-test`). **The first full run with them failed 270 of 331 screens with 2,876 finding lines**, by cause: icons
    of a line of text 1,482, actions beside a heading 394, controls of 32 px at a row's top 303, badges and statuses 79,
    the actions of stacked rows 62, the mark of an external link 57, pictures 20, and 479 that the check had wrong and now
    knows as a layout (a control beside a framed box, a column of controls, a badge that wraps like a word). The last
    full run: 333 screens, 332 passed, no layout finding, 21.4 min; the one failure a click that ran into its timeout in
    `repository-settings-oci-virtual` as the administrator at 1,024 px, green when rendered alone, cause not read. No entry
    was added to the allowed list. Two guards that need no stack: the style guide's patterns are rendered with the compiled
    stylesheet in the unit run, and a lint rule refuses `vertical-align: middle` and `align-items: last baseline`, which
    found nine more places. The job `UI layout checks` of Test and Release runs the full set on every push on
    `self-hosted`, outside the needs of Semantic Release; **it has never run on a runner**, and its package, browser,
    compose and upload steps are unverified.
    **What the guard cannot see, by the builder's list:** a control beside a framed box is not measured at all; pictures
    and controls that are none of the listed kinds, absolutely placed ones and text drawn by CSS; states the set does not
    render (open menus, hover, most dialogs, error states); an offset of 1 px or less; a control a wrapping toolbar moves to
    a line of its own; a top-aligned row in a component no render shows.
    **Looked at by the development agent:** the owner's screen as a visitor (the button on the name's line) and an opened
    row of `repository-oci`. Seen there and not judged: the copy button attached to the field of the pull command is drawn
    taller than the field (about 46 px against 32); the copy button's component is unchanged by this work, the state is
    new in the set, and the stack was the next builder's, so whether the change caused it is not known. It is the first
    case of the gap named first.
    **Measured and fixed the same day** (the development agent, on the owner's stack): the button was 32 px wide and 48 px
    high beside an input of 32. A rule of the lists gives every copy button inside a table cell the margins that centre it
    on its line (-8 px above and below at a line of 16 px); the command of an opened row stands in a cell, its copy button
    is joined to the field, and the field stretches what it holds, so the margins made the button 16 px higher. The change
    of W-23 caused it (the margins were its line rule). The rule leaves a button joined to a field out; the checks gained
    `joined` (a button joined to a field is as high as the field, within 1 px) with a fixture that reproduces the 48 px
    and one that passes. Not run: a full render set with the new check.
- **W-24 · A remote repository lists what was fetched, and every cell of the row is true** (owner, 2026-11-19, with
  pictures of `platform/guidedtraffic` and of its image `guidedtraffic/kubectl`: one image, one tag `latest`, size 0 B,
  **Last fetched** "Never", downloads 0): "Wie kann es sein, dass es nur ein einziges Image gibt und das keine Größe hat und
  noch nie gefetched wurde. Ich hätte erwartet: A: Es werden nur gefetchte Images angezeigt. B: Es werden alle Remote
  bekannten Images angezeigt, bereits gefetchte haben dann auch ein last fetched date und eine Größe. So wie hier, kann ich
  mir nicht erklären." Read in the specification and the code: the row came from the development agent's check that
  morning, a request for the manifest of the tag alone; the tag is an index of two platforms, and the size of a tag is
  the sum of the config and the layers of the stored images below it ([view.go](../../internal/formats/oci/view.go)),
  which is 0 while none is stored; `lastDownloadedAt` is `null` for the tag of a remote OCI repository by R-API-065
  ("has no lead file"), an exception the development agent wrote into Q-API-295 instead of solving, so the column says
  "Never" whatever was pulled. **Decided by the owner on 2026-11-19** ("Option A", from two: only what was fetched, with
  true cells / everything the upstream knows, which Docker Hub's registry API cannot answer for the images of a
  namespace): a remote repository lists what was fetched through it; **Last fetched** is the last fetch of the version by
  a client and the downloads count those; a size that is not known is shown as not known, not as 0 B.
  - **Built for every format** (on `main` the same day; R-API-079 to R-API-081, Q-API-295 rewritten, Q-API-340 to
    Q-API-343, Q-OCI-221, Q-OCI-222, Q-REPO-330, Q-REPO-331, ADR-205, migration 00079; 929 requirements, 257 of them P1).
    The format contract is at version 5: a format with package screens must say what a fetch of one of its versions is
    (`Descriptor.VersionFetch`), and registration refuses one that does not, so a later format cannot land without the
    rule. What the builder found per format, each a hole of its own:
    - **OCI remote:** a tag was listed as soon as its document was cached, also after a `HEAD`; the size of an index was
      the one written when the index was stored (0 B) and never grew; downloads were always 0 and the last one always
      empty. Now a tag is listed once a client downloaded its manifest or a mirror pass stored it, a `GET` by tag or by
      digest is its download, and the size of an index is the sum of the platforms held, marked as partial ("≥ …") while
      some are missing and not known while none is; the tag's page lists each platform with its size or **Not fetched**.
    - **OCI local:** only a `GET` by tag counted, so a pull that asks `HEAD` by tag and `GET` by digest counted nothing.
    - **npm, NuGet, Ansible remote:** the first download of a file over 64 KiB is streamed to the client while it fills
      the cache and was never counted, so a version fetched once showed 0 and "Never".
    - **Debian local:** only the first-uploaded architecture's file counted. **Debian remote:** listed nothing at all,
      under a sentence that said otherwise; it lists the packages apt fetched now (new code: a fill hook and the removal).
    - **Generic:** no package screens and no remote class; unchanged.
    Not a download anywhere: `HEAD`, `304`, a range that does not start at 0, metadata documents, the platform manifests
    below an index, blobs. The integration sets `ociremote`, `oci`, `debremote`, `proxy`, `files` and `packages` ran green
    before the builder's rebase onto W-23, the unit run after it; the renders of its screens had no finding.
    **For the owner, decided by the builder or left open:** the column is named **Last download** now, where his word was
    "Last fetched" (the builder's reason: a version a mirror pass stored was fetched by Quader and by no client, and the
    cell said both); **Published** of a version that exists in a remote only is still the time it was cached, not the
    upstream's date; a row filled by a mirror pass is not in the render set.
- **W-25 · A probe starts the download** (owner, 2026-11-19, with W-24): "wenn jemand wie du nur einen Probe auf ein Image
  macht, auch wenn er den Download nie anstößt, sollte Quader pro-aktiv den Download im Hintergrund schon starten." Open
  before it is built, and the owner's to decide one at a time: which platforms of an index, what counts as a probe in each
  format, and what bounds the work a request without a credential can start on a public remote.
  **Read for the decisions** (a read-only pass over the six formats, 2026-11-19; file references in the session, the
  points marked "inferred" not run):
  - The specification says the opposite today: INV-11 and R-REPO-024, "in the default mode `pullThrough` nothing is fetched
    ahead of time"; the eager fetch of companion artifacts (RMT-025) and a cache warm-up (RMT-027, API-020) are deferred.
    W-25 changes that invariant on the owner's word.
  - The means exist: a background fetch through the same gate and engine as a mirror pass (`BackgroundUpstream`, used by
    the Ansible documentation job and the Debian index parse), run by the worker role, which yields to client fetches and
    reads the upstream's circuit without moving it. Missing for this use: outside a pass nothing bounds the bytes
    (`mirror.maxBytes` is a pass's), the include and exclude patterns are the dispatcher's check and not the background
    path's, and a probe answered from a fresh cache runs no fill hook.
  - What names exactly one version without fetching it: OCI, the manifest of a tag (the upstream request is a GET that
    Docker Hub counts as a pull) and a HEAD by digest; npm, the version document and a HEAD of the tarball, while the
    clients read the package document with every version; NuGet, the registration leaf and the `.nuspec`; Ansible, the
    version document, which states the archive's digest and size; Debian, only a HEAD of a pool file, its other requests
    are indexes of a whole component; generic has no remote class yet.
  - OCI: after the probe of an index only its children and their platforms are known, the layers after each child's
    manifest is fetched; a plan refuses more than 1,000 children; the setting `oci.mirror.platforms` (default every
    platform) exists for mirror mode only.
  - Who may start a fetch: a request without a credential on a `public` remote passes the fill check like a grant
    (`cacheFillAction: read`, the warning `public_populates_remote` at save time only); there is no bound per caller on
    bytes or fetches and no storage quota (R-STOR-029 has no code). Inferred from
    [ratelimit.go](../../internal/api/v1/ratelimit.go) (`limitedPath`): the general rate-limit classes cover
    `/api/v1`, `/files/` and `/api/<format>/`, not the registry's `/v2/`; not confirmed by a request.
  - Found on the way, for W-24 (inferred): the size of an OCI index is set when the index row is written and not when a
    child is recorded later; a single-platform tag shows the size its manifest declares while nothing is stored; a
    Debian remote's files write no package row.
  **First decision, by the owner on 2026-11-19** ("Option A", from three: a request that names exactly one version
  starts the fetch of that version's content / also the newest version when a request names a package / OCI alone): the
  trigger is a request that names exactly one version or file without fetching its content, in every format; a request
  that names a package with all its versions, or no package, starts nothing. Said to him with it: in npm, NuGet and Debian
  the rule fires seldom, because their clients read the package or index level and then fetch the content themselves.
  His question to that, and the answer read in the code: such a fetch goes through Quader in every format, not to the
  original host, because a remote repository rewrites the addresses of the content in the documents it serves onto
  itself (npm: `dist.tarball`, [handlers_remote.go](../../internal/formats/npm/handlers_remote.go); NuGet: the
  registration's `packageContent`, `Rewrite` in [remote.go](../../internal/formats/nuget/remote.go); Ansible: the
  `download_url` of collections and of roles, the roles' GitHub archives included, from the hosts the operator named),
  and OCI and APT have no addresses in their documents: the client asks the host it was configured with. The one
  exception is an OCI layer that names foreign `urls`, which a client fetches from there.
  **Second decision, by the owner on 2026-11-19** ("Option A", from three: a list of platforms per repository with
  `linux/amd64, linux/arm64` as the value in the field / every platform / none ahead, as today): of an index the
  background fetch takes the platforms of the repository's list, which stands in the advanced settings with those two as
  its value and is the same list the mirror reads, so the mirror's default changes from every platform to these two. The
  question exists in OCI alone: in the other formats a version is one file, and a probe of a Debian file names its
  architecture. Still to decide: the bound for the work a request can start, and with it whether a repository can turn the
  behaviour off.
  **The third decision became a subject of its own.** To the proposal of one budget per repository the owner asked
  (2026-11-19): "Macht es nicht Sinn hier eine Unterscheidung zwischen Known und Unknown User zu machen und dafür
  Konfigurationsoptionen in einem Public-Repo zu bieten?", and then: "Ich erwarte von dir bei dieser Frage bitte eine
  Ausarbeitung als Claude-Artifact im Browser, ich will hier deutlich detailliertere Settings. Ich muss für einzelne User,
  technische Tokens und Gruppen (anonyme User gehören der Gruppe Anonym), bei anonymen Usern aber ggf. auch IPs, diese
  Grenzen einstellen können: Request-Anzahl pro Sekunde / pro Stunde? Cached-Downloads-Size pro Sekunde / pro Stunde?
  Uncached-Downloads-Size pro Sekunde / pro Stunde? und bitte überlege was ich vergessen haben könnte […] Ein PAT gehört zu
  einem konkreten User und geht direkt gegen das Budget des Users. Ein technischer Projekttoken oder Repo-Token gehört
  nicht zum User und wird separat berechnet, hat ein eigenes Budget. Ein Projekt-Admin oder Instanz-Admin muss Übersichten
  bekommen und schnell das Download-Verhalten aller Entitäten in einer Übersicht erfassen können." The working paper is
  an artifact of his account (private, "Quader Download-Limits"): the model (a request must fit the caller's budget and
  the repository's), what is counted (his three, and upstream requests, parallel downloads, a day's volume, background
  downloads, refused requests, uploads, storage growth as additions), limit profiles instead of single numbers, a sketch of
  the settings and of the administrators' overview, and ten decisions with options and a favourite each, which he
  answers in the page. Nothing of it is specified or built; the budget of W-25's background download is the upstream
  budget of a class of callers in the repository there, so W-25 is built after these decisions.
  **W-26 · The download limits: the owner's answers, read from the page on 2026-11-19** (he chose in the working paper;
  where he wrote a note, it is quoted):
  1. A group's limit is a profile for each member, each with a budget of its own (not a shared pot).
  2. A caller in several groups gets the most generous (the development agent had proposed an ordered list of rules).
  3. Instance, project and repository: the most specific wins; a repository can set a limit of its own for a caller, also a
     higher one, which replaces the instance's (proposed was: one budget per caller for the installation, repositories
     only narrow).
  4. At the limit, what fits the figure: requests 429 with the time to wait; bytes per second are slowed, no error; bytes
     per hour 429 before the download starts; with the upstream budget used up, what is cached is still served.
  5. Windows: second, hour and day.
  6. The budget of a technical token hangs on the single token (proposed was the account): "Ich kann hier für jedes System
     einen eigenen Token ausstellen und die Budgets damit pro System genau einstellen."
  7. An anonymous address: IPv4 by itself, IPv6 per /64, and a ceiling for all anonymous callers together.
  8. Who sees the consumption: instance administrator and auditor everything, a project Owner what happens in the project,
     every user their own; single values 30 days, sums after that.
  9. Without any setting: known callers the profile Standard, anonymous ones Anonym, administrators unlimited.
  10. Everything in one step (proposed was: count and show first, then limit).
  11. "Anonymous" is a fixed class of the limit settings only; the rule that no group and no policy names the anonymous
      caller stays.
  12. A kind of its own (proposed was a project's service account with a token narrowed to the repository): "Es gibt
      technische Token auf Instanz, Projekt und Repo-Ebene. Jeweils mit eigenem Budget."
  Counted in the first step: requests, bytes from the cache, bytes from the upstream, upstream requests, parallel
  downloads, the day's volume, background downloads and refused requests; later: uploads and the growth of a
  repository's storage. He changed no number of the profiles.
  **What follows from the answers, put to him in the same page as five further questions:** whether an account for the
  rights stays behind a technical token (13); who may raise a limit above the instance's (14: the upstream figures use
  what belongs to the whole installation); whether consumption under a repository's own rule also counts against the
  instance budget (15); the order where the place of a rule and the caller it names meet (16: the place first, then a
  single caller, then the groups with the most generous value per figure, then the kind's default); and whether the one
  step enforces at once or starts observing, with the limits counted and shown as "would have been limited" (17), since
  every number of the profiles is an estimate. Nothing is specified or built before these are answered.
  **Answered the same day, read from the page:** 13, an account for the rights and a token for the budget (the service
  account stays and can belong to a repository too; a renewed token keeps budget and consumption); 14, only the instance
  administrator raises above the instance, a project Owner up to a maximum the administrator gives the project; 15,
  consumption under a repository's own rule counts against that rule only; 16, the place first, then the caller; 17, a
  switch, with his note: "Es soll für die Durchsetzung von Instanzlimits einen Schalter geben, den der
  Instanz-Administrator setzen kann. Die Limits können instanzweit damit zwischen Audit und Enforce konfiguriert werden."
  **How it is built:** first the specification as one contract (a new page `usage-and-limits.md` and the amendments to the
  identity, authorization, API and operations pages; to a builder on 2026-11-19, documents only), then three builders
  against it at once: the technical tokens on three levels, the accounting and enforcement in the request path of every
  format, and the screens. W-25 follows on its budget hook.
- **A decision taken at OCI holds for every format** (owner, 2026-11-19): "Alles was wir hier für OCI besprechen gilt auch
  für alle anderen Registries die bereits gebaut sind und für zukünftige Registries. Ich verstehe, dass die API der
  Registries von ihrem Funktionsumfang unterschiedlich sind und deshalb manche Features nicht implementierbar sind. Das
  muss dann im Zweifel noch mit mir besprochen werden wie wir vorgehen. Ich kenne mich mit zum Beispiel Nuget oder NPM
  nicht so richtig aus, und erwarte von dir dass du mich bei den Entscheidungen unterstützt und ein Auge auf anderen
  Registries hast, während wir über OCI sprechen." For W-24 and W-25 and from here on: a behaviour decided at one format is
  specified for the format contract and built for Ansible Galaxy, Debian, generic, npm, NuGet and OCI together; where a
  protocol cannot do it, that comes back to the owner as a decision of its own.
- **Seen by the development agent, not decided:**
  - **Three tiles of Guides carry the same placeholder mark** (2026-11-19, as a visitor): Ansible Galaxy, Generic and npm show
    the outlined box, Debian, NuGet and OCI a mark of their own.
  - **The setup page of a public remote offers a visitor `docker login`** (2026-11-19, `platform/guidedtraffic` without a
    credential): the first snippet of `client-setup?client=docker` is `docker login … --username quader`, though the guide
    of a visitor is to come without credential steps and the pull that follows needs none.
  - A session that has ended shows a red box on the page, "The session has ended. Sign in again." with a request ID and "Give
    this ID to your administrator", and under it the page as if it were empty ("The project has no member yet."). An ended
    session is no error to report; the reader needs the sign-in.
  - Station of the power user (`priya`), looked at before the owner on 2026-11-18:
    - `members`: a list that is not built like the lists of W-6. Its filter field is as wide as the page, beside it stand a sort
      button **Account** and an arrow instead of **Filters** and chips, and above it a tab strip with one tab, **Members 2**.
    - `repository-settings-*`: the page has three **Save changes** buttons, one under each open section (the description, the
      visibility, the upstream), and seven closed sections under them; the header's only button is **Vulnerabilities**.
    - `repository-settings-*`: the visibility is two groups of three choices with their explanations, **Artifacts** and **Wiki**,
      about a fifth of the page; the split is an expert's detail in the main view.
    - `new-<format>-<class>`: the description is a field as wide as the form for a sentence; **Show as API request** opens with
      the browser's own triangle, every other section of the form with the chevron of the app's disclosure.
  - `login`: the local form of the break-glass account stands first with the primary button, the sign-in provider below it as a
    secondary button. The owner typed `sam` into the form before he found the provider's button. Every user except the break-glass
    administrator signs in through a provider.
  - `repository-npm-local`: the row of the long package name
    (`customer-portal-api-client-generated-from-the-openapi-document`) starts 10 px further left than its neighbours, chevron and
    name alike; the same kind of defect as W-3.
  - `repository-npm-local`: the field **Filter packages** is as wide as the page (1128 px) for a few typed characters, against the
    rule that a field is as wide as its expected input.
  - `repository-npm-local`, `home`: the table ends 15 px before the right edge that the filter field and the separators above it
    reach.
  - `repository-npm-local`: the address field stands under the title, **Set up client** in the far corner of the header; the two
    belong to one job and are not together.
  - Every screen inside a project: the breadcrumb names the project by its key (`web-frontend`), the side bar by its name
    (`Web frontend`).
  - `packages` against `repository-npm-local`: two lists of packages with different columns and words. The repository's list has
    **Latest**, **Versions**, **Vulnerabilities**, **Updated** as a date and **Downloads**; the project's list has **Format**,
    **Latest version** and **Updated** as "4 minutes ago", no grade and no column that says which repository holds the package.
  - `packages` against `repository-npm-local`: the same action is the grey button **Set up a client** on one and the blue button
    **Set up client** on the other.
  - `packages`: the tabs **Recently opened** and **Saved views** stand in the plain user's main view.
  - `vulnerabilities`: the report is one row per finding, 1,222 of them; the first 60 rows are three packages. The plain user's
    question, which package and which version is affected and how badly, has no row; the grade of T-21 is not in the list.
  - `vulnerabilities`: every row is three lines high because cells break: **Fixed in** reads "None / yet", the repository
    `web-frontend/` / `npm-vendor`, the advisory's summary runs over two lines.
  - `vulnerabilities`: the reader `sam` sees the tile **1,222 Needs triage** and a status column **Needs triage** in every row; a
    reader cannot triage.
  - `vulnerabilities`: the chart **Open findings over time** stands above the findings in the plain user's main view.
  - Three ways to write a time: "4 minutes ago" (`packages`), "Nov 18, 2026" (the packages of a repository), "Nov 18, 2026,
    8:17:21 AM" (the files of a repository).
  - `repository-generic`: the file row's icon touches the name and sits higher than it, the folder rows have a gap and one line.
  - `search-results`: a third list of repositories, built differently again: no visibility icon, the project by its key without
    its icon (W-1), and a copy button without a label at the end of each row that copies the client address (W-4 takes the address
    out of the project's list for want of context).
  - `profile-tokens`: the cell **Scope** breaks into four lines ("All repository permissions of the owner, without
    administration") while the column **Token** beside it is mostly empty.
  - `profile-tokens`: the column **Created via** says `ui`, a technical fact in the plain user's main view; the row's two actions
    are an icon without a label (rotate) and a word (**Revoke**).
  - `browse`: the format is a lower-case badge (`generic`, `oci`, `npm`) and a remote repository is marked `cache`; every other
    screen says **Generic**, **OCI (Docker)** and **Remote**.
  - `browse`: a long key is cut with an ellipsis here and broken across lines in the repository list (W-3): two treatments of one
    case.
  - `browse`: the tree has a field **Filter repositories**, a button **Filters** and a sort button without a label; the right
    half of the page is an empty box until something is chosen.
  - `browse` against `repository-generic`: both show the files of a repository. Upload, copy, move and the trash can exist only in
    **Browse**; the repository's own page lists its files and offers none of them (seen as the power user `priya`).
  - Station of the administrator (`ada`), all 19 screens of the admin area looked at before the owner on 2026-11-18, dark, 1440 px,
    in the running UI (not a render run; light and narrow not looked at):
    - **Layout that is broken** (the kind of W-5, to be fixed without a decision):
      - `system/jobs`: the state's icon stands on one line and its word ("succeeded") on the next, in every row; the replica's name
        breaks ("ms2022.local-" / "39645") while the result column beside it is half the page and mostly empty. The filter is its
        own form (a text field "Kind (comma separated)", six check boxes, **Search**) instead of **Filters** and chips.
      - `audit`: the action's code breaks inside a word ("user.groups.claim_missin" / "g"); the target is "user" and a UUID broken
        across two lines where the row beside it names the same person as "ada"; the outcome is written "success" / "failure" in
        lower case where every other status of the app starts with a capital.
      - `tokens`: the token's name has a column of about 190 px and breaks into three lines ("Client setup: npm, web-frontend/npm-
        vendor, 2026-11-18") while the scope column has 280 px for "Repository grants: 1". The page is titled "Token administration",
        its menu entry "Tokens".
      - `repositories` (admin): the labels of **Format** and **Class** stand 2 px lower than the name, the project, the visibility
        and the time in the same row (measured: 607.5 against 605.5); the project's name breaks into three lines
        ("Customer portal / release engineering / and delivery") beside a wide, mostly empty **Last activity** column.
      - `dashboard`: in the card of a component the rows **In flight**, **CPU**, **Memory** stand closer together and without the
        line that separates **Nodes** to **Server errors** above them; the card is as wide as the page, so a label and its value
        are 1,100 px apart.
      - `system`: "77" and the check mark of "supported" touch; the version breaks into three lines ("dev" / "(3897aa…47" /
        "708f8a)") in a card whose right half is empty.
      - `endpoints`: the sentence under the title is set smaller and tighter than the same kind of sentence on every other screen,
        and the table follows it without the gap the other lists have.
    - **One thing written or built in two ways:**
      - the button that makes a new thing: **New project**, **New group**, **New service account**, **New policy**, **New issuer**,
        **New Bastion**, but **Create repository** and **Add user**;
      - nothing to show in a cell: an en dash on `repositories`, `service-accounts`, `endpoints`, `jobs`; an empty cell on `projects`
        (Owners, Your role), `users` (Email, Last sign-in);
      - a project in a cell: mark and name on `repositories`, the bare key ("web-frontend") on `service-accounts`;
      - a row's action: the word **Edit** on `projects`, **Revoke** on `tokens`, **Unbind** on `roles`, a copy icon and a menu on
        `repositories`;
      - a count: left on `projects` (Members, Repositories), right on `groups` and `advisory-sources`;
      - the empty list: `permissions` says "No policy matches." with no filter set and nothing else; `bastions` repeats **New
        Bastion** under its sentence; `ci` has only the sentence;
      - a filter: `security` has three fields with **Search** above the chart and a filter field with **Filters** above the findings;
        `jobs` and `backups` have their own forms, `backups` with the browser's own select boxes;
      - a setting's name: in code type in the sentence under the buttons of `advisory-sources`, in plain type in the paragraph below.
    - **For the owner to decide:**
      - `projects` (admin): the column **Your role** is empty in every row for an administrator (decided: W-15); **Owners** is
        empty in three of four rows, also for "Platform engineering" with two members: not read whether that is the seed or a
        project can be without an Owner.
      - `roles`: a role is bound by typing "user:name, group:name or sa:name" into a field; no choice of a person. This is the only
        way to make someone an administrator. The capabilities are 25 codes in four columns, in no order.
      - `system` and `dashboard` are red ("Replicas unready", "A role is not ready", "Not drain-safe") because two processes that
        were stopped at 10:29 and 14:20 are still listed as **lost**: a registry row is pruned after 24 hours
        (`replicaPruneAfter` in [builtin.go](../../internal/jobs/builtin.go)). Verified in the code: the 24 hours. Not verified:
        whether a pod that ends in a rolling update signs off or stays "lost" for a day as well; if it does, every update turns
        the dashboard red for 24 hours.
      - `dashboard`: the component is called "all" and shows "Nodes 0" beside "1 of 1 ready".
      - `audit`: every sign-in of a user whose provider sends no groups writes a second, red entry "Group claim missing at
        sign-in · failure". Half of the log of this stack is that entry.
      - `security`: the fields show examples as grey text ("CVE-2021-23337", "lodash", "shop"); the chart of 90 days is one bar
        at the right edge on an installation whose data is a day old.
    - **Seen while the admin lists' package was looked at** (2026-11-18): the start page's list of projects at 1,100 px is wider
      than the page (**Repositories** is cut, **Members** out of sight) and its **Description** breaks after every word; the
      admin area's repository list still breaks "Customer portal release engineering and delivery" into three lines beside its
      mark; in `system/jobs` the result ("enqueued: 0, repositories: 0") breaks while the replica's column beside it has room.
      - `system/jobs` and `system/backups` have no entry in the side bar; **System status** stays marked on both.
    - **The detail and form screens of the admin area**, looked at on 2026-11-18 (dark, 1440 px): a user, a group, a new permission
      policy, a new CI issuer, an endpoint, the authentication settings, the format choice of a new repository.
      - Layout that is broken: in a group's member table **Remove** stands 5 px lower than the member's name in the same row; the
        check box of "Public issuer" on a new CI issuer is smaller than the two other check boxes of the form; under the fixed
        bar with **Create policy** a line of the form shows through below the bar.
      - Naming a person by typing (W-12): "Add a member" of a group ("A user name, or sa:name for a service account") and
        **Principal** in the policy's "Try it" ("for example user:alice"). A group's members are bare usernames, without avatar
        and name.
      - A user's page is titled with the username ("sam"), not the name; **Display name** and **Email** are fields with a
        **Save changes**, and the sentence under them says both are replaced at the next sign-in; the **ID** (a UUID) and the
        origin stand in the main form, the avatar and the status behind **Details**. Two **Save changes** on one page.
      - A new CI issuer has **Accepted audiences** and **Pin claims** (two text areas "one per line") in the main form, with
        **More settings** below them: expert settings in the standard flow.
      - A new permission policy: the links at the left (General, Principals and actions, …) are underlined blue text, a form the
        app uses nowhere else for a page's sections.
      - The browser's own select box also on a group (**Source**) and in a policy's "Try it" (**Action**).
      - Ansible Galaxy, Generic and npm carry the same icon in the format choice; Debian, NuGet and OCI have their own.
      - An address that does not exist under `/ui/admin/` shows "Page not found" without the side bar, the search and the
        user menu.
- **Tooling, done in the walk:** the dev server of the `local` configuration listens on `127.0.0.1` like Quader and the sign-in
  provider; it listened on `::1` only, which the owner's Firefox (IPv6 lookups off) could not reach
  ([web-ui.md](../developer/web-ui.md#local-ui-development)). A checkout that still holds the fixed secrets of before the rename in
  `.dev/ui-dev/secrets` fails at `make ui-dev-up` with `InvalidAccessKeyId`: `up.sh` writes a secret file only once.
- **Seen in the builders' renders by the development agent, for the sweep after the four packages** (W-5):
  - At 1024 px with the side bar open every table of the app turns into stacked blocks, one line per cell, because the shared
    table stacks below 48 rem of its own width whatever its columns need: the versions of a package with 33 rows are 5,136 px
    high there, a folder's three columns stack too. Wanted: the threshold follows the table's columns, and the stacked form is
    denser than a line per cell.
  - `package-npm-many` at 1440 px: the picture is 3,251 px high and the page ends at about 2,100; not read whether the page or
    the render's growing window leaves the empty part.
  - A version's page and a file's page show the status **Needs triage** and "by user:priya" to a reader; the first is the open
    question above, the second a technical way to name a person.
- **F-138 · The merge of two packages cut two units of the German locale file, and `main` was red for one run** (2026-11-18,
  Test and Release 44287670389: `web (Angular)`, the image build and what needs the image). The development agent joined both
  sides of the conflicts in `web/src/locale/messages.de.xlf`; each conflict ended inside a unit, so `files.details` and
  `files.folder-actions` lost their end and `ng build` refused the file. The gates it ran before the push (lint, the unit tests,
  the type check of the journeys, the Go tests) do not read the German file; `make web-embed` does and was not among them.
  Fixed by restoring the two units from the files package's commit. Since then a landing of UI work also runs `xmllint` over
  both locale files, compares the units and sources of the German file with the English one, and runs `make web-embed`.
- **F-139 · The page of a file answered 404 when it was opened by its address, and a journey asked for a text that is gone**
  (2026-11-18, `e2e (access-long)` of Test and Release 44288465071, the run of 7b1e693a; both from the files package, whose journeys
  could not run before the push). The embedded UI answers a path whose last segment has a dot as a file of the build, so
  `…/repositories/<key>/files/hello.txt` was a 404 on a reload or from a link, while the dev server of the renders has its own
  rule for it and showed the page. Fixed: `isFilesRoute` in [webui.go](../../internal/webui/webui.go) with a unit test. Journey 03
  still expected "No repository matches." where the list says "No repository matches the filters.". What it shows: a page whose
  address ends in a name with a dot needs a test against the embedded UI, not only a render.
- **F-140 · Two journeys were red after the guides package, for a link named twice** (2026-11-18, `e2e (access-long)` of Test
  and Release 44313192183, the run of c684bfb6; read in the evening window). Since the row action **Set up client** ends its
  accessible name with the repository's key (Q-UI-487), a row has two links that carry the key. Journey 12 asked for a link by
  the key without exact matching and met both (strict mode); journey 09 asked for the action by "Set up client" exactly and
  for the row by "project/key", which a guide's row no longer writes; journey 23 had the selector of journey 12 and was
  fixed with it. Fixed in 08cb7050; not run locally (the journeys need the cluster), the run of that commit shows it. The
  failed job's log cut the error of journey 09, so its cause is read from the code, not from the log.
- **F-141 · `e2e (ci)` was red once for a certificate the test did not trust** (2026-11-18, Test and Release 44346085826, the
  run of 1785aff2): `GET /api/v1/authz/me` of the fixture of `TestCIPipeline` answered "tls: failed to verify certificate:
  x509: certificate signed by unknown authority (possibly because of "x509: ECDSA verification failure" …)"; `TestCITokens`
  and `TestCITasks` failed with it. Nothing of that push touches certificates or the edge. The run was replaced by the next
  push before it could be run again; cause not read. To be read if it shows again.
- **F-142 · A push whose last commit says `[skip ci]` starts no run for the commits before it** (2026-11-18): ad2e3ac2 (W-14)
  was pushed together with the plan's commit behind it and got no run of its own; the next push's run covered it. Since
  then a commit that changes code is pushed alone, and the plan's commit after it.
- **F-143 · Two more journeys were red for what the day's packages changed** (2026-11-18, `e2e (access-long)` of Test and
  Release 44352485630, the run of 7c5bd389; 43 of 45 journeys passed, journey 09 of F-140 among them). Journey 12 found a
  service account's row in the admin area by a link named with the project's key; the list names a project by its mark and
  its display name since the lists' package. Journey 14 expected the heading "Jobs" behind a mirror's link to its jobs;
  Jobs is a tab of **System status** since W-19, and the builder of W-19 had not found that journey. Fixed in bdbe37c5; not
  run locally.
- **F-144 · Two legs did not get their cluster: the database was not ready after ten minutes** (2026-11-18, `e2e (npm)` and
  `e2e (push)` of Test and Release 44358371450, the run of bdbe37c5): `deploy/dev/up.sh` installed the dependencies in
  parallel, HAProxy, the PKI, the secrets and the Valkey operator were ready after 1m 43s, and at 10 minutes "postgres
  FAILED … timed out waiting for the condition on clusters/quader-db" in both legs at the same second; no test ran. It looks
  like the stall family of F-133 (several kind legs at once on one machine); not read whether it is, the legs' pressure
  records were not opened. The failed jobs are run again once the run has ended.
- **F-145 · An integration test of the person search asked a replica for an API it does not serve** (2026-11-18,
  `integration (shard b)` of Test and Release 44358371450, red in both attempts): `TestR_AUTHZ_026_PrincipalLookupBounds`
  created the project's service account with `POST …/service-accounts`, and the replicas of the package
  `test/integration/projects` answer 503 "The identity API is not available on this replica." The builder of W-12 had
  changed the test without a stack to run it on. Reproduced on the local integration stack, fixed by creating the account by
  its rows (the test's subject is the lookup), and the packages `projects` and `search` run green locally. In the same run's
  second attempt `e2e (npm)`, `e2e (push)` (F-144) and `e2e (access-long)` with the journeys of F-143 were green. What it
  shows: a builder that changes an integration test runs that package on the stack before it hands over; the stack exists
  per worktree and costs a minute.
- **The evening's pipeline window, closed** (2026-11-18, 20:20 CET): Test and Release 44372205911 of 924276b6 is green in
  every job. The release with the day's packages (the admin lists, W-12 to W-19) was cut from bdbe37c5 by the run before it,
  whose release jobs do not wait for the integration and e2e legs; the commit after it changes a test only. The owner's
  installation on `wds18-k8s-main` follows the newest chart by itself (the HelmRelease asks for `*`) and runs that release,
  two ready replicas of each of `quader-api`, `quader-packages` and `quader-worker`; read, nothing changed there.
- **The night of 2026-11-18** (owner, 21:35 CET: "ich erwarte, dass du jetzt im Nacht-Modus selbstständig an der
  Veröffentlichung der aktuellen Version arbeitest und im Anschluss weiter an der CI von "HA and Performance (nightly)"
  arbeitest … Triff die für unser Projekt optimalen Entscheidungen selbstständig."). Test and Release 44384406557 of cf297363
  (W-20) was red in two legs and cut no release; the three findings below.
- **F-146 · The drain test took a killed process's row for the drained pod's; closed, with what it led to** (2026-11-18,
  `e2e (push)` of Test and Release 44384406557, `TestE14Drain`, by the assertion W-13 added that day). The development
  agent's first reading was wrong: it took a worker that "stopped after 35 s and left its row in the state `ready`" for a
  replica that could not sign off after a database switchover. Read by the builder from the leg's artifacts: `TestE06Lease`
  had killed that pod's process with SIGKILL four minutes earlier (`e06-sigkill.json`), the kubelet restarted it in place
  under the same name, and the killed process's row is what stayed, `ready` and lost, as a killed replica's does for 24
  hours. The drained process signed off; the test matched rows by the pod's name. The database was up during the stop (the
  switchover was the test's own and over 16 s before the drain). The 35 s were the plan of a worker with running jobs
  (released at 29 s, the engines quiesced at 35 s), not a hang. Not verified: the old row's UID (the artifacts hold no dump
  of the table; inferred from the one row the query returned and the prune rule).
  **Fixed and built with it** (Q-HA-307 to Q-HA-310, Q-ARCH-501, Q-ARCH-502, Q-STOR-217): the test follows instance UIDs and
  names the case of a database that never answered; the mark `draining` and the final deletion are retried within their
  steps (the deletion for at most 15 s and never past the close deadline; a healthy stop still makes one attempt); a
  heartbeat no longer writes `ready` over `draining`; a heartbeat stale past its bound is logged, and logged again when it
  recovers; a stop that waits longer than 5 s says for which jobs; a job that only waits is handed over at the drain. **Left
  open on purpose** (Q-HA-310): a replica that reaches the database at no point of its stop leaves a lost row. **F-137 is
  closed with it**: its second lead held (the `gc-full` job was inside its settle wait and ended 0.24 s before the release at
  29 s; a race the test always had), its first did not (neither 5cd1e824 nor 55a6688c changes when a job is released); the
  settle wait now ends at the drain. Changed and not run: `TestE14Drain`; the nightly's `drain-load-b` is the proof of F-137.
- **F-147 · `apt-get update` hung a gating job for 19 minutes and the release was skipped** (2026-11-18, job `chart` of the
  same run): "installing build-essential" at 19:50:58 UTC, `Ign:1 http://archive.ubuntu.com/ubuntu noble InRelease` at
  19:51:28, nothing until the job's timeout at 20:10:06; **Semantic Release** needs the job and was skipped, so W-20 had no
  release until the run of 2543ad7b cut it at 21:12 UTC (with F-149 in between). The family of F-117; its remedy (`apt-get update || warning`) guards a failing update, not a hanging one,
  and about twenty steps of the two workflows install packages that way. **Fixed** (8258f61c): every package install goes
  through `.github/scripts/apt-install.sh`, which returns at once when the packages are there, reads only the distribution's
  own sources (the image's git PPA is no longer read), bounds every apt call by the clock (worst case 470 s) and names the
  host that did not answer; the 48 steps of the gating jobs that download have a step timeout; a test fails on a bare
  `apt-get` under `.github/`. Reproduced by the builder with a server that sends a byte every 5 s: apt's own timeouts never
  fire, only a wall-clock bound ends it. The gating jobs of the run of 8258f61c passed with it. **For the owner** (another
  repository): the runner image should carry `build-essential`, `jq`, `file`, `unzip`, `libatomic1`, `docker-compose-v2`,
  `clamav`, `clamav-daemon` and, on `large`, Chromium's libraries; every package step then ends at the installed-check.
- **F-148 · The journey of the vulnerability report asked for the chips by a name they lost in the morning** (2026-11-18,
  `e2e (vuln)` of the same run): "Applied filters" is "Filters that are set" since ADR-200. The leg had been green all day
  because it runs its journeys only when their files change; W-20 changed one. Fixed in 90896979, not run locally. What it
  shows: a leg that skips by path is green without having looked; a rename in a shared piece needs a search over
  `web/e2e/vuln/` too, which the builders' brief now names.
- **F-149 · A commit of the plan pushed while a run is in flight costs that run's release** (2026-11-18, Test and Release
  44393745218 of 90896979): every gating job was green and **Semantic Release** ran, and wrote "The local branch main is behind
  the remote one, therefore a new version won't be published": the development agent had pushed a commit of this file with
  `[skip ci]` eleven minutes after the code. That commit starts no run of its own, so W-20 stayed without a release until the
  next push of code. The same must have cost time on other days and was read as "the release comes with the next push".
  Since that night: a commit of the plan goes **before** the code commit in the same push, or waits until the run's
  **Semantic Release** job has ended; nothing is pushed to `main` between a code push and that job.
- **The releases of the night** (2026-11-18): `v0.62.0` at 21:12 UTC (W-20, the context title Security audit, the send buffer
  bound of Q-ARCH-500) and `v0.62.1` at 21:35 UTC (the registry's sign-off through a database switchover and the handing
  over of waiting jobs, the package installs of CI). The owner's installation followed to `0.62.0` by itself within the
  hour, two ready replicas of each Deployment.
- **Closed in the night of 2026-11-18, each by a builder:**
  - **F-130** (the upgrade set had no release to start from): the test of the rolling upgrade from the previous release takes
    the newest release tag in the commit's history whose chart the chart repository lists (Q-HA-311; `v0.62.0` that night),
    and reads the migrations changed since that release before it installs anything: a changed or removed migration or a
    contract step skips it with the files named until the first stable release, and fails it after (Q-HA-312). The test
    across the cut of the rename is removed with its exit criterion (Q-HA-313): no Quader release precedes the cut, so no
    installation can take that path; revisit at the first stable release. The set is split by what it tests (Q-HA-314, a
    note at the owner's Q-ARCH-340): `upgrade`, the rolling upgrade of the commit across its two UI bundles, runs every
    night; `upgrade-release`, upgrade and rollback from the published release, on a dispatch that names it. **Not run on a
    cluster**: dispatch `-f sets=e2e-upgrade` after the night's full run; green shows "the upgrade starts from v0.62.0",
    "the migrations changed since v0.62.0: []", both subtests PASS and every loop "failed 0". **For the owner:** the bridge
    of the roles (`web.enabled`, `web.routed`, the role `web`) serves only the removed path and can go in a change of its own.
  - **F-131** (the search field kept its position when a newer answer replaced the list): the highlighted entry follows its
    identity, and nothing is highlighted when the new list no longer holds it (Q-UI-700); a pointer that does not move does
    not move the highlight, a held Enter acts once (Q-UI-701). One helper for the search field and the principal field.
  - **F-132** (a suggested key could land in a key the user had begun): a default that arrives while its field has the focus
    waits until the field is left and is written only if nothing was typed (Q-UI-702); the same for the cache periods and
    the mirror's numbers. The journeys 08, 11, 14 and 17 no longer wait for the suggestion before they type; changed, not
    run locally. Not looked at in a browser by anyone: the render stack was the W-5 builder's that night.
- **Closed or changed in the night of 2026-11-18, second batch, each by a builder; not pushed before the night's full run had ended:**
  - **F-136** (what cuts a control statement at the end of a replica's drain step) was a fault of the product and is fixed
    (Q-ARCH-503): the close phase stopped the three loops that run on the control connection (the change log's poll, the
    registry's heartbeat, the fill count) by cancelling their context, so a statement in flight at that moment was cut — about
    once in a few hundred stops with a poll every second. A stopped loop now starts no further statement, waits for the one
    in flight and cuts it only when the stop's own context ends. Forced on the integration stack with a table lock that
    holds each loop's statement in flight: before the fix all three show the runs' line "a database connection came back
    closed and is discarded", after it none. The 8 s wait of the cleanup is the driver's, not the server's: a cancel during
    the write breaks the TLS connection for every later write, so the terminate message never leaves (shown with a scratch
    program, not verified for the run whose dump shows only the read); it is bounded since Q-ARCH-498. A probe statement
    ended by the pools' close is intended and now logged at INFO as such. Open: the job worker's heartbeat of running jobs
    still cancels on stop (no job runs by then).
  - **F-135** (`TestCollapse`, a draining replica that fetched for itself instead of joining a flight) was the test asserting
    more than R-REPO-026 promises: every path to that answer needs a Valkey call given up on at its 500 ms command timeout,
    and with Valkey unavailable for a call each replica fetches itself and no request fails. R-REPO-026 says so now; two
    forced subtests show both cases; the original subtest still fails on an own fetch without a timeout behind it.
  - **The bridge of the roles is gone from the chart** (Q-HA-315; R-HA-064 deleted): `web.*`, its Deployment, Service and
    endpoint pools, the guard with its `lookup` reads, the alert `QuaderBridgeLeftOn`, the UI's fallback for download links.
    A values file with a `web:` block is refused by the schema. Every shipped value set and documented example renders
    byte-identical before and after, except the bridge's own and the pattern of `QuaderReplicasNotReady`. The binary keeps
    the role `web` as a development and test role like `all`: it is the default role of the integration harness (57
    packages) and of the `quader admin` commands; removing it there is a change of the harness, revisited when the harness
    runs `api` and `packages` replicas behind a path router. H-518 is rewritten for the two development roles. The dev
    profile's PostgreSQL goes from 450 to 400 connections. Not run: a kind leg (`default`, `TestE03Roles`) and the renders.
- **F-150 · A bound taken from one green run failed two legs on a slow night** (2026-11-18/19, `e2e (vuln-long)` and `e2e
  (upgrade)` of the nightly 44414813006): the step that installs Playwright's browser and libraries had `timeout-minutes: 10`
  since F-147's fix that evening, derived from 101 s in one green run. That night the archive's mirror delivered the fonts
  at about 50 kB/s without stalling (gaps of 80 to 190 s per package); both legs ran into the bound at the browser's
  download and no test ran. The bound is 25 minutes now; it is against a hang, not against a slow night. What it shows: a
  bound needs the bad case measured, or a margin an order above the good one, where the job has the time.
- **The nightly of the night of 2026-11-18** (HA and Performance 44414813006, dispatched at 22:46 UTC on 5c802652, alone on the
  runners, nothing pushed to `main` while it ran; ended 03:20 UTC): **29 of 31 legs green**. Red: `e2e (vuln-long)` and
  `e2e (upgrade)`, both at the bound of the browser's install before any test (F-150; they are dispatched again after the
  landing of the fix). Green, and each the first result on `main` for something that was open: `drain-load-b` (F-137: the
  waiting `gc-full` job is handed over at the drain), `drain-load-c`, `drain-load-e`, `Latency` and the vulnerability
  scanning performance (no result since the night of 11-17), `ha-faults`, `ha-pooler`, `ha-suite`, `edge`, `failover`,
  `drain-roles` with the day's change of the registry and the shutdown, `upgrade-release` skipped by design.
  - **The recorder on the new runner layout** (first legs on `large` with it): every leg on `large` names its daemon's parent
    inside its pod with `memory.max` 28.0 GiB and `cpu_weight` 800 on `daemon:parent`; 9 of 10 own containers have `cgd:`
    rows in the leg read row by row (the tenth a k6 container of a few seconds).
  - **Whether 28 GiB hold every leg** (the open question of the runner change): they held every leg of that night. Nine legs
    reached the limit (`memory.peak` 28.0 GiB; at the limit in 35 to 42 % of the samples in the three `drain-load` legs and
    `drain-load-pooler`, 22 % in `ha-faults`, 24 % in `nuget-ha`), with at most 1.1 s of full memory stall of the pod over a
    whole leg (`edge`), 0.5 s or less elsewhere, and no OOM at the pod's limit or below it in any leg: the pod fills with
    page cache and gives it back without waiting. Not known: how near the legs are to the point where it is anonymous
    memory that fills the pod; the recorder does not split `memory.current` yet (`memory.stat` `anon` and `file` are the
    next columns).
  - **The CPUs of the nodes** (F-133's question): full CPU stall of the daemon's cgroup between 10.8 s in 14.6 min
    (`drain-load-pooler`) and 31 s in 29 to 34 min (`ha-suite`, `npm-ha-soak`), that is 1 to 2 % of a leg, on a pool with
    nothing else running. F-133 stays open by its own condition: it asks for a green nightly **with a Test and Release run
    beside it**, and that night had none on purpose.
  - Also seen: `Platform matrix` legs take a minute (they render and validate, they start no cluster); the four of them and
    `upgrade-release` are what a dispatch gets back first, and they showed within a minute that the changed leg selection
    of F-130 works.
- **W-5 is built** (owner's requirement of the walk: the renders find what a person kept finding): the render run measures
  the layout of every picture in the page and fails on a finding (R-UI-170 to R-UI-174, Q-UI-680 to Q-UI-686): an icon off
  its label's line, a short thing broken across lines, a cell off its row's line, action cells of one table built
  differently, text cut beside free room, a stacked table whose columns would fit, sideways scroll, scroll into nothing.
  The first full run with the checks failed 75 of 330 screens with 260 findings; all fixed, three allow-listed with their
  reason. A list now stacks only where its own columns do not fit (it measures its least width; the fixed 48 rem is gone)
  and a stacked row puts its facts side by side. Found on the way and fixed: the labels of the stacked versions table were
  shifted by one column at 390 px, and the 900 px of nothing under a long package page was the page's defect, not the
  render's. The anonymous screens' 429 is fixed in the UI development profile, the product default untouched.
  **Checked by the development agent** on the merged night's batch: a full run, 330 of 330 passed, 0 layout findings, 19.9
  min; looked at: a package's 33 versions at 1,024 px (a table) and at 390 px (stacked, two to three lines a version), the
  admin area's repositories at 1,100 px, the audit log at 1,024 px. **Still only for the eye** (the builder's list): a
  wrapping column cramped beside a roomy neighbour ("3 findings · / 0 fixable" at 1,024 px is one), text cut at the start
  of a wrapped line, a stacked label off its control, two words for one thing across screens. **For the owner:** the upload
  dialog's states in lower case, the start page's empty **Your role** cell, whether the policy editor's selector button
  may wrap at 390 px.
- **F-151 · The container scan turned the gate red for a finding in a binary Quader does not build** (2026-11-19, 03:29
  UTC, Test and Release 44450995271 of 3d3fd661, the night's second batch): `usr/local/bin/trivy (gobinary)`, `stdlib`
  CVE-2026-97054, HIGH, installed v1.27.3, fixed in 1.26.11 and 1.27.4 — the analyser of the image scanning, which is the
  upstream release archive at its pin (`v0.76.2`, the newest release, of 2026-11-05). Quader's own binary is built with Go
  1.27.4 and had no finding. The vulnerability database learned of the finding that night; the run four hours before was
  green on the same image content. **Decided by the development agent in the owner's absence and confirmed by the owner
  on 2026-11-19 ("Option A"):** the finding is accepted at that one path until 2026-12-19 in a new `.trivyignore.yaml`, because it is a finding
  of `net/http`'s server and Quader runs the analyser as a command for one scan (`image --input … --offline-scan`, then
  `sbom …`), never as a server. Verified with the pinned Trivy on a copy of the analyser: exit 1 without the file, exit 0
  with it, exit 1 for the same binary at another path, exit 1 with a day in the past. A test refuses an entry without
  identifier, paths, an end and a statement; the report's scan reads the same file; the gap is H-1003 on the image scanning
  page. Not chosen: building the analyser from source with Quader's toolchain (it would replace a checked upstream archive
  by a build of our own for one finding), and leaving the gate red until upstream releases (no release of anything for an
  unknown time, for a finding that does not apply). The entry goes with the next raise of the analyser's pin.
- **The late schedule of the nightly** (measured in the night of 2026-11-18: the cron is `17 2 * * *` UTC, and on each of
  the twelve days before GitHub created the run between 08:44 and 10:41 UTC, 6.5 to 8.5 hours late, in the owner's working
  day; he cancelled it on 11-18). Decided by the development agent, for the owner to review: the cron line stays (a moved
  one might fire on time and land in the evening); a scheduled run skips its legs when a **full** dispatched run of the
  workflow ended less than 12 hours before and was not cancelled, and says in each leg's summary which run it defers to;
  when the question cannot be answered it runs. A run's name says what it is: "HA and Performance (all legs)" or "(sets:
  …)". The check lives in `select-leg` (`.github/actions/select-leg/select-leg.sh`), a dispatch never calls the API and
  decides as before; tested through 20 cases with a stubbed answer. **Not verified on GitHub:** that the run's title
  carries the new name and that a completed run's `updated_at` is its end. The first scheduled run that can defer is the
  one after a full dispatch made from a commit that has the name; the dispatch of that morning (below) is that run.
  The recorder's `pod-memory.csv` gained `mem_anon`, `mem_file`, `mem_kernel`, `mem_shmem`, `mem_slab_reclaimable` and
  `mem_sock`, and the README's line the peak of what cannot be given back; read them in the first leg on `large`.
- **F-152 · The layout package folded the policy editor's preview away in a window of 1,280 px** (2026-11-19, `e2e
  (access-long)` of Test and Release 44453761606 of 6cd1cb09: the journeys of R-UI-025/026 and R-UI-041 waited for "No
  problems found." and found it hidden; the two runs before had been replaced by pushes before that leg ended). W-5 had
  moved the editor's three columns from the window's width to the content area's and set the threshold to the columns' full
  widths, 66 rem; a window of 1,280 px with the side bar open has 60.5 rem, one of 1,366 px 65.9 rem, so on the common
  laptop widths the preview was a closed region above the save bar. The render run did not see it: it renders 1,440,
  1,024 and 390 px, and at those the layout was right. Fixed: the threshold is 59 rem, which leaves the form 20 rem between
  the contents and the preview. Measured in the running UI: 192 / 360 / 384 px at 1,280, 192 / 446 / 384 at 1,366, the
  narrow form at 1,024. What it shows: a threshold moved by a layout change needs a render at the width where the
  journeys run (1,280), which the render set lacks; added to the list for the render checks.
- **F-153 · `Set up Go` ran into its new bound of five minutes in a gating job** (same run, job `Cyclomatic Complexity`; a
  full nightly ran beside it on purpose). The step takes 10 to 20 s; what held it for five minutes is not read. The bound
  did what F-147 built it for (the job failed after 5 minutes instead of at its limit), and the remedy is the one written
  there: run the failed jobs again. Open: whether five minutes are too few for a download on a loaded pool, the question
  F-150 answered for the browser's install; one occurrence is not a measurement.
- **F-154 · The Ubuntu archive did not answer, and the last seven legs of the second full nightly ended in their package
  step** (2026-11-19 from 06:28 UTC, HA and Performance 44453781574: `nuget-ha`, `cold-crash`, `drain-load-b`, `-c`, `-e`,
  `Latency` and `Vulnerability scanning performance`, each red after two and a half minutes in `Install build tools` with
  `Unable to locate package build-essential`; the 24 legs before them were green). No leg's test ran. At 06:50 UTC `archive.ubuntu.com` took 18 to 23 s
  to connect, or accepted and sent nothing, from the owner's network as well, over IPv4 and IPv6. The third time after
  F-117 (11-09) and the hung `chart` job of 11-18; the script of that day did what it was built for (a red leg in minutes
  with the host named, not a hung one), and a second red step, `make dev-down` without `make`, is noise of the first.
  **One defect of the script found in the first of the seven and fixed:** in `nuget-ha` each of the two hosts answered during
  one of the two updates (`archive.ubuntu.com` in full after 51 s of the first, `security.ubuntu.com` after 24 s of the
  second), and the leg still had no index, because the 60 s bound ended both updates and an update that is ended keeps
  nothing. apt needs 41 s to give up a silent source once (the log, and a container beside a source that accepts and never
  answers: 41 s without a retry, 82 s with one, 168 s with three), so with three retries it could never end inside the
  bound. The update now retries no request itself, ends inside the bound with the indexes of the sources that answered, and
  the script reads once more when a source was not read. In the container the script before the change failed after 120 s
  as in the leg, the script after it installed the package in 85 s
  ([releasing.md](../developer/releasing.md#os-packages-in-the-jobs)). **What the fix does not reach:** the other six
  legs, where `archive.ubuntu.com` itself was silent in both updates; it carries `main`, and no order of calls installs a
  compiler without it. **For the owner, now with a third occurrence and a lost nightly behind it:** every leg fetches 82
  to 102 MB of packages from that one host before it does anything; the runners use the stock image
  (`ghcr.io/actions/actions-runner`), so the remedy named at F-117 and F-147, an image that carries the packages, is an
  image to build and keep, in the runner's repository. The script ends at once on such an image.
  **Decided by the owner on 2026-11-19** ("Option A, ich glaube das ist schon lange überfällig"; not chosen: a package cache
  in this repository, and leaving it at a re-run): the image is built in `guided-traffic/container-images`. Done there that
  morning, in the working tree, not committed (that repository's rule for agents): directory `actions-runner`, the upstream
  image of the runner version plus `build-essential`, `file`, `jq`, `unzip`, `gettext-base`, `libatomic1`,
  `docker-compose-v2`, `clamav`, `clamav-daemon` and Chromium's libraries and fonts as Playwright 1.64 lists them. Built for
  arm64 on the development machine (2.0 GB against 1.3 GB upstream); in it, without a network, every package step of the
  workflows ended at "already installed" and `docker compose version` answered. **Not done:** the amd64 build (the archive
  was still silent), the commit and the first publication (the owner's), the visibility of the new Docker Hub repository,
  and the switch of the four `image:` lines of the runner scale sets. **The tag rule, decided by the owner on 2026-11-19**
  ("Option A"): the runners name the tag that does not move, `<runner version>-<build time>`, and a change of it is a commit
  in the runner's repository; not the version tag with a pull at every start, because the build workflow of that repository
  runs on these runners, so a tag that moves with the weekly rebuild could take away the runners that would build its
  repair. The price is that a security update of a package reaches the runners with the next change of the tag, not by
  itself. In this repository the browser steps no longer need the archive on such an image: they call
  `.github/scripts/playwright-install.sh`, which fetches the browser alone when Playwright's own dry run finds every
  library (18 s and no index read in the new image; the old path, unchanged, in the upstream image).
  **Published and rolled out on 2026-11-19, on the owner's word** ("Ja bitte komplett durchführen. Freigabe zu pushen um
  unser neues Runner-Image aufzubauen"; the rollout "B · Sofort"): the image is `guidedtraffic/actions-runner`, public on
  Docker Hub, amd64 and arm64, 804 MB compressed, built in 23 minutes; the first build was replaced after ten minutes by
  the push of a dependency update that the first push had started (that workflow cancels a run in progress), and the image
  came with the run of the next such update, which rebuilt every image. The push to `main` there went past its required
  status checks with the owner's rights. The runners name `2.338.2-20261119084717` since 10:15 (flux-runner `4aaf2d9`);
  no running job was ended by the change. Verified: the published amd64 image, pulled by that tag and run without a
  network, ends every package step of the workflows at "already installed"; and in the first job on it, the re-run of
  `integration (shard a)` of Test and Release 44491248224 (red before at the download of `docker-compose-v2` from the
  archive), `Install build tools` took one second with "make, gcc are on the runner; nothing to install", the compose
  step found the plugin, and the job was green. Open: the seven legs of the nightly, again in the night on a quiet `main`.
- **The second full nightly** (HA and Performance 44453781574, "HA and Performance (all legs)", dispatched at 03:46 UTC on
  6cd1cb09 on purpose beside Test and Release: 44453761606 until 04:25 UTC and 44458581063 from 04:28 to 05:06 UTC; ended
  06:50 UTC): **24 of 31 legs green, 7 red without having run** (F-154, the archive). Green this time `upgrade` and
  `vuln-long`, the two the first nightly lost to the browser's install (F-150). To do: the seven legs again when the
  archive answers.
  - **The run's name** carries what it is ("(all legs)"), as built that night; verified on GitHub.
  - **F-133's question, the CPUs of the nodes beside a Test and Release run**, read from `daemon:parent` of the twelve legs on
    `large` that ran in both nightlies (the first was alone on the runners):

    | Leg | Beside Test and Release | CPUs used, alone → beside | Full CPU stall, alone → beside |
    |---|---|---|---|
    | `npm-ha-soak` | 20 of 35 min | 14.2 → 12.8 | 1.5 % → 4.1 % (31 s → 87 s) |
    | `ha-suite` | its first 4 of 30 min | 13.7 → 12.3 | 1.8 % → 3.7 % (31 s → 66 s) |
    | `npm-ha` | all 45 min | 6.3 → 6.1 | 1.0 % → 1.7 % |
    | `drain-roles` | all 15 min | 18.7 → 18.0 | 2.0 % → 2.4 % |
    | `failover` | all 10 min | 1.6 → 1.8 | 0.6 % → 1.0 % |
    | `backup`, `vuln-pooler` | all | 1.9 → 1.9, 1.3 → 1.7 | 0.7 % → 0.8 %, 0.5 % → 0.7 % |
    | `ha-faults`, `ha-pooler`, `drain-load-pooler`, `oci-long`, `edge` | none (after 05:06) | within 0.5 | within 0.3 points |

    A neighbour costs a loaded leg about a tenth of its CPUs and doubles to triples its full stall; every such leg was
    green. Before the runner change the same legs' nodes were left with about two CPUs. **F-133 is closed on that.** Not
    seen beside a Test and Release run: `nuget-ha`, `cold-crash` and `drain-load-b`, `-c`, `-e`, which did not run; `nuget-ha`
    is the leg with the largest demand (Q-HA-102). Not explained: `ha-suite` doubled its stall with four minutes of
    overlap; its other neighbours on the pool in those minutes are not read.
  - **How near the pod is to its limit** (the open question of the first nightly, answered by the recorder's new columns):
    the pod reached 28.0 GiB in three legs again, but what cannot be given back (anonymous, shmem, sockets, kernel less its
    reclaimable slab) peaked at 10.9 GiB (`ha-faults`), 9.0 to 9.3 GiB in `drain-roles`, `ha-suite` and `drain-load-pooler`,
    5 to 8.5 GiB elsewhere; no OOM, at most 1.2 s of full memory stall in a leg (`edge`). The limit is filled by page
    cache; 17 GiB of it are room. The seven legs on GitHub's own runners (`alerts`, `bastion`, `bastion-long`, `ci-ha`,
    `upgrade`, `vuln`, `vuln-long`) have no pod and no such file.
- **F-155 · The registry's paths are under none of the general rate limits** (2026-11-19, read for W-25, not confirmed by
  a request): R-API-016 names its classes for `/api/v1/`, `/files/` and the package protocols under `/api/<format>/`, and
  `limitedPath` in [ratelimit.go](../../internal/api/v1/ratelimit.go) asks for exactly those; the OCI protocol is served
  under `/v2/`. On the default endpoint a request there, with or without a credential, is bounded by the replica's
  `server.max_in_flight` alone; a further endpoint has its own class for every request (R-EP-015). What it means per
  case: a request without a credential on a remote whose artifacts are `public` and whose `cacheFillAction` is `read` can
  ask for any number of tags the patterns admit, each an upstream request that Docker Hub counts against the
  installation's pull limit. Live on the development stack (`platform/guidedtraffic`, loopback only); on an installation
  it needs a public OCI remote on an endpoint the internet reaches, which is not known to exist. Not changed: a limit on
  `/v2/` also meets the clients behind one address that pull many layers at once, so its numbers are a decision.
- **F-156 · A module download that stalled held the gating job `chart` to its limit, and W-23 went without a release**
  (2026-11-19, Test and Release 44511234820 of 3dc92311): `make chart-test` began to download 75 modules at 11:05:02 UTC;
  the download of `github.com/cloudflare/circl v1.6.3` from `proxy.golang.org` sent nothing for 16 minutes, until the
  proxy closed the connection at 11:21:21 ("http2: server sent GOAWAY […] server_shutting_down"); the job reached its 20
  minutes at 11:23 and was cancelled, and Semantic Release was skipped. The proxy answered at once when asked afterwards.
  The same family as F-147, F-150, F-153 and F-154: a host outside that sends nothing, and a step without a bound of its
  own (F-147 left the make targets to the job's limit). What the log adds: `Set up Go` had restored the module cache
  ("Cache hit", "Cache restored successfully"), and the job still downloaded 75 modules. The cache has one key for every
  job of a `go.sum` and is written once, by the first job that finishes after the key changed, with what that job
  happened to need; a job that builds more than that one downloads the rest on every run. Not changed yet. The remedy of
  the same kind as the runner image: a module cache that holds every module of `go.sum` (written by a step that runs `go
  mod download`), so that no job asks the proxy; and a bound on the download itself so that a stall fails in minutes.
  The release follows with the next push, whose run covers W-23.
- **F-157 · The first full run of `UI layout checks` on a runner: 336 screens passed, 7 failed** (2026-11-19, Test and
  Release 44514749126 of 10a28fbd, 23.9 minutes of rendering on `self-hosted`, the job 29 minutes; the run before it was
  replaced by a push after 19 minutes). Every setup step worked on the runners' new image. Two kinds of failure:
  - **A heading in two lines** (`setup-ansible-local` as two kinds of user, 390 px): the toggle of the guide's section
    "Remove a version, import tasks and tombstones" broke into two lines in a box of 350 px. On the development machine the
    same screen had no finding; the runner's text is a little wider. The check was wrong, not the screen: the toggle of a
    section's heading is a heading and wraps as one. Left out of the check `broken`, with two fixtures. What it shows:
    a picture rendered on a Mac and one rendered on the runner differ by a pixel at the edges, so the runner's run is the
    one that counts.
  - **A click that waited 15 s** in the walk that opens every section of a repository's settings (five screens, at 390,
    1,024 and 1,440 px; the builder of W-23 had seen it once locally): the report said "locator.click: Timeout 15000ms
    exceeded." and nothing else, and the job uploaded the pictures only. Cause not known. The report now carries the lines
    after the first (what the click waited for) and the job uploads Playwright's context of each failed screen; the next
    run names the cause.
    **Cause, from the next run (44519652288, thirteen screens) and reproduced on the development machine:** a race in the
    walk, not in the product. The page writes `aria-expanded` a moment after the click, not with it. The walk counted the
    closed toggles right after its click, found the one it had just clicked, and clicked "the first closed toggle" again:
    on a fast machine that closed the section and opened it again (21 clicks for 7 sections, measured), on a loaded runner
    the toggle had turned to open in between and the click waited 15 s for a closed toggle that no longer existed. The
    report of the run said so ("waiting for locator('… .toggle[aria-expanded="false"]').first()") and Playwright's context
    showed every section open. The walk names the toggle by the region it controls and waits until that toggle says it is
    open: 7 clicks for 7 sections at full speed and with the CPU slowed 6 and 20 times.
- **F-158 · The archive again, this time inside an image build: `e2e (nuget)` and `e2e (ci)` red** (2026-11-19, Test and
  Release 44519652288 of 7665f810, from 11:59 UTC): both legs failed in `make nuget-clients-image` after ten minutes, the
  package install inside the image build with "Could not connect to archive.ubuntu.com:80 […] connection timed out"; the
  archive did not answer from the owner's network at 12:15 UTC either. No test ran. The runners' image does not reach
  this: the five client images of the acceptance suites (npm, NuGet, APT, Ansible, and the forwarder they share) are built
  from their Dockerfiles in every leg that needs them, without a layer cache, and each build installs OS packages and
  tools from hosts outside. The same holds for the seven legs of the nightly that are still to run: with the runners'
  image they get past `Install build tools` and then build these images. To a builder on 2026-11-19, together with F-156:
  the images are built once per change of their inputs and reused from a cache that publishes nothing, the Go module
  cache holds every module of `go.sum`, and a download that stalls fails in minutes.
- **Render run of the walk** (2026-11-18, 242 of 246 passed in 19.4 min), open, cause not read:
  - `start-recent` as `plain` and as `power` ran into the test's 5 minutes inside `settle`
    ([render.spec.ts](../../web/e2e/renders/render.spec.ts)): a request stayed pending or the walk's states no longer fit the budget;
    The screen is gone since W-20 (its section left the start page); whether `sidebar-recent`, which took its place, settles
    is not known before the next render run.
  - `projects-anonymous` and `search-field` as `anonymous` got 429 for `GET /api/v1/auth/whoami` and `GET /api/v1/system/banner`,
    in two runs: the anonymous visitors of three workers share one address and its limit.

## 11 · Run record

What a run did, in short, so the next run starts where it stopped. The plan items, findings and decisions carry the details.

**Run of 2026-11-07/08** (releases v0.25.1 to v0.26.0).

- Built: the findings F-71 (a narrowed token no longer acts on its owner's account or other tokens), F-72 and F-74 (every narrowing
  and every block the evaluation sets is journaled and replayed fail-closed), F-73 (one oversized OSV record no longer stops an
  ecosystem's import; the owner's npm advisory data is back from v0.25.3), F-63 (the slot split of Q-REPO-235; the install-time bound
  held at 97 %), F-76 and F-77 (npm verifies every tarball against its strongest digest); E5.7 (the security pages of the whole
  beta), E5.8 (each release is installed by digest and smoke-tested; the SBOM comes from the pinned syft), E5.13 (the vulnerability
  blocks through drain variants B to E, the pooler profile, the restore drill), with three product defects found on the way (jsonb
  through a transaction-mode pooler, a policy update replayed as suspended, the restore Jobs without their CA volume).
- Started late: T-16, the Docker registry: spec and WP0 (contracts, schema, format stub) built and reviewed on the branch `t16-oci`,
  not yet on main. T-17 (APT) not started.
- Broke: v0.25.1 has no image and no chart (the build's RUN steps had no DNS on a per-run docker network; fixed in v0.25.2).
- Where the time went: the run worked sequentially. For about eight hours it took the findings one after the other through
  planning, building and three or four review rounds, merged six parallel worktrees and waited for Test and Release, the release
  runs and a dispatched nightly run; the Docker registry started only at the end. The owner's verdict: features and findings run in
  parallel from the first hour, an agent and a branch per feature package and an agent per finding (rule 10). The next run starts
  T-16's wave 1 and an agent for each open finding together in its first hour.

**Run of 2026-11-08** (releases v0.27.0 to v0.29.0; v0.30.0 waits for Test and Release on the last push).

- Built and on `main`: T-16's local OCI registry. WP1 upload sessions across replicas, WP2 registry tokens and one enforcement point
  in front of `/v2/`, WP3 the `/v2/` registry root (logins by token, pulls, manifest pushes, tags, catalog), WP4 the local
  registry class (manifests, tags, referrers, deletes, sweep, replay), WP5 blob uploads and cross-repository mounts, WP6 the remote
  engine's token handshake, WP7 the counting stub registry, WP12 the OCI screens of the UI. A local `docker push` and `docker pull`
  through `/v2/` work end to end.
- Findings closed: F-43, F-50, F-75, F-78 to F-82, F-85 to F-87, F-89 to F-94, F-97 to F-104, F-107. The important ones: path-scoped
  grants now apply on the npm and NuGet protocol routes (F-82); the stall timeout no longer cuts a request while the server works,
  over HTTP/1.1 and HTTP/2 (F-91, F-92); a replica's shutdown keeps its deadline (F-94, F-97); two first publishes cannot both claim
  a name (F-101).
- Landed at the end of the run: WP8, the OCI remote class (pull-through from Docker Hub, GHCR and other registries), F-83 (a path
  pattern that hides a version's file hides that version everywhere) and F-84 (a large delete stops when its requester loses the
  right). Not done: WP9 (virtual), WP10 (mirror), WP11 (e2e on kind), WP13, WP14; T-17 (APT) not started.
- Where the time went: three or four review rounds per work package and per finding, 28 new findings (most of them false failures
  under the load of 10 to 14 parallel agents on 20 cores), and a full Docker disk at the end (252 GB) that stopped the integration
  stack. The owner's verdict (rule 10, changed): during ALPHA no review subagents, findings only when they block, no repeated race
  or stress runs, at most four builders, each a whole feature, and release notes that say in plain words what changed.
- Next run: first the blockers F-109 and F-110 (main red), then four builders: OCI virtual (WP9), OCI mirror (WP10), APT local,
  APT remote; the open findings of section 8 in one batch at the end.

**Run of 2026-11-08/09** (ALPHA mode of rule 10; releases v0.30.1 and v0.31.0).

- Built and on `main`: the rest of T-16, the Docker registry — the virtual class (pulls in member order, pushes to the deploy target),
  the mirror (selected names, tags and platforms kept and pullable with the upstream gone), the conformance suite v1.1.1 in all four
  categories, the e2e set `oci` with Docker and Podman on several replicas, the closing set `oci-closing`, the documentation; T-16 is
  closed. All of T-17, the APT registry for Debian and Ubuntu: the contracts, the `.deb` and deb822 libraries, the OpenPGP library and
  a signing key per repository with co-signing rotation, the local class with signed indexes built by a job, the remote class that
  passes archives through with their signatures verified, the mirror of named suites, the virtual class that merges local and remote
  members into one signed index, the UI, the e2e set `deb` on Debian 13 and 12 and Ubuntu 24.04 and 22.04, the documentation.
- Product defects the clients and tests found on the way: a mount challenge without the source's pull scope (the conformance suite), a
  mirror with its upstream down answering 502 to Docker's referrers request, a Docker Hub mirror that never ran a pass because Hub
  refuses the catalog scope, APT ignoring every index until the Release listed the uncompressed checksums, a lost first build of a new
  suite, a reindex of unchanged rows that published nothing, a manual and an automatic name claim deciding at the same time (F-108), the
  build metrics counting virtual builds as local.
- Findings closed: F-106, F-108, F-109, F-110, F-111, F-112 (decided, Q-NPM-86), F-113, F-114, F-115. Recorded: F-116 to F-120.
- Release: v0.31.0 (2026-11-09) carries both registries. Test and Release 42565678133 green; Release Docker & Helm 42569132981 green on
  its third attempt after two re-runs under the changed rule 7 (GitHub answered 504 for the syft download, F-120); checked: the GitHub
  release with plain-language notes, the image `guidedtraffic/quader:0.31.0` on Docker Hub, the chart 0.31.0 in the index, the smoke
  test of the published release. Before it, main was red six runs in a row for causes outside the product: the Ubuntu archive's outage
  (F-117), shard b past its 45 minutes (now three shards), the apt client containers on the Linux runner (host networking, the key
  through the environment), test preconditions written for one timing, and the set `push` on a starved runner cluster (F-119).
- Rule change by the owner on 2026-11-09: a red CI job may be started again up to three times to reach a green run (rule 7, step 5).
- Builders: at most four at a time, each a whole feature over several stages, resumed with their context; no review rounds. Load stayed
  between 15 and 50 on 20 cores, and the timing failures under it were fixed in the tests (F-114, F-115) instead of being chased.
- Merges: every builder branch was cherry-picked onto `main`, the touched gates rerun on the merged tree; the conflicts were the shared
  plan and documentation pages, two migrations that both took 00064 (renumbered), and doc links one directory too short.
- Evidence of the closing: the e2e set `deb` (52 rows, the virtual row on) and the closing set `deb-closing` (Valkey stopped on all
  four images; `apt-get install hello` from `deb.debian.org` through a remote, 3 upstream requests, Debian's archive key checked against
  its pinned fingerprint) ran green on kind (`linux/arm64`); `oci-closing` 7 of 7. Left as findings: F-118 (architecture `all` through
  a virtual repository); the Debian passes of the sets `mirror` and `mirror-long` are not built. `HA and Performance (nightly)` was
  dispatched on the release commit (run 42571414393) for the long OCI legs; its result is read at the start of the next run.
- Next run: T-17 has no open package. Section 10 holds no registry task, so the next unit of FULL.md follows (step 1, item 3), with
  the open findings of section 8 in one batch at the end.

**Run of 2026-11-10** (ALPHA mode of rule 10; releases v0.37.0 to v0.43.0, and the push of 24cf0250 after them).

- Started from the handover list. The gating `unit tests` job is green since b0b4538d (`TestR_VULN_004` without a wall-clock bound). The red
  integration shards had a product side: a stopping replica spent its whole budget in the drain step on a stalled control connection, and a
  readiness check cut that connection (Q-ARCH-460, Q-HA-283, Q-HA-284); why the pool stalls on the runners is still open (H-822). `e2e (push)`:
  the rotation row runs a closed loop judged by whether its clients ran through the window (Q-HA-288, Q-HA-291), and its certificate probe
  used more CPU than the installation (Q-HA-292). The alert `QuaderDatabaseUnreachable` no longer fires during maintenance (Q-HA-290). E5.1's
  drill ran on kind with the new commands, 13 of 13.
- Product: the content page of an OCI remote lists its cached images through a view (Q-OCI-215); administrator tokens for service accounts and
  a registry refusal that names the missing scope without telling whether a repository exists (Q-OCI-220); the settings pages in one column
  but the repository's; `quader admin import` for npm and NuGet (FULL.md block A); the manual's backup and restore page and runbook (block A);
  the image scan inside the spool's budget (H-819, Q-VULN-145 to 148).
- F0, Ansible Galaxy: planned by one agent (R-ANS-001 to 032), built by four in stages (core and local; stub, remote, mirror and hub exchange;
  virtual and import; setup, UI and e2e). The real clients found three defects every integration test had missed: ansible-core sends the
  archive base64-encoded (every publish failed), the netrc snippet sent its variable name as the password, the mirror lacked the collection
  document. The set `ansible` passed on kind with both clients (67 tests) and its CI leg is green. The import against the owner's galaxy_ng followed on 2026-11-11: a dry run, then the copy of 3 collections and 7 versions into the owner's installation, installed from there by ansible-galaxy 2.21.
- Leads that were wrong, with the evidence that refuted them: dropped SYNs behind the edge 502s (no kernel counter moved), the drain
  change behind `TestE14Drain` (the 502s came 1.5 minutes after the drain, in a stall of the whole CI cluster). The common factor of most red
  e2e and integration runs of the day is a stalled runner; no CI job records its CPU pressure yet.
- Where the time went: merging the Ansible builders, whose decision blocks were appended at the same anchor of the spec every time (resolved by
  sorting the blocks by number), and three rounds on the rotation row's load bound before it fit a closed loop. A gate that a builder's own run
  could not see: a new format changes every test that pins the formats (the guide's client list, the guide journey's first format).
- Next run: the red legs of section 8 that are not the runner, then FULL.md block B, F1 (generic files: remote and virtual).

**Run of 2026-11-11/12** (ALPHA mode of rule 10; releases v0.45.0 to v0.55.1).

- Owner tasks: T-20 (client addresses), T-19 (the documentation of Ansible collections), T-18 (visibility levels: private, internal,
  public, the external user flag, the wiki part of a repository), T-21 (a UI that looks right) with T-21b (creating and configuring a
  repository) and T-21c (vulnerability grades as cogpit shows them), and T-22 (a search with suggestions), which the owner asked for
  during the run and which was pulled forward.
- New working rules from the owner, now in CLAUDE.md and rule 11: every UI change is rendered and looked at before it lands; the product
  is simple by default with details on demand; a search never leads into the admin area. Two lessons on the way: a render is judged by
  the screen's purpose first (the owner rejected an admin list the development agent had approved), and a passing functional test is
  no look at the screen.
- The landing of T-18 turned four CI legs red. Two were product defects that no builder's run could see: the challenge of a request without
  a credential had moved behind the rate limits (NuGet restores hit `package_anonymous`), and a package route answered with a Bearer
  challenge instead of Basic. Both were found from CI evidence and fixed at their cause. The assumption that the 429s came from a fast
  local host was wrong and cost a day's turn; the CI run on a fresh cluster refuted it.
- Leads that were right: the secrets operator crash loop was its 128 MiB limit against 23 MB of helm release Secrets under emulation
  (F-127's local cause); TestR_DEB_023 waited for the first index that lists a name, not the scope; the control pool was held by a
  readiness probe through the close.
- Where the time went: rebasing T-18 (written on an old base) onto a main whose UI the fix waves had changed, and pushes that cancelled
  the CI run before them, so its e2e evidence came late. From the middle of the run, a push waited for the run before it.
- Next run: the open points of section 8, the owner's questions there, then FULL.md block B, F1 (generic files: remote and virtual).

**Run of 2026-11-17/18** (the owner's order: "HA and Performance (nightly)" and "Test and Release" run stable on every execution; releases
v0.56.1 to v0.56.4).

- Way of working: the red legs of the last twelve nights and of Test and Release were sorted into clusters, one builder per cluster on a
  branch `stab/<topic>`, each fix shown by a dispatched leg on its branch, then cherry-picked onto `main`.
- On `main`, by cluster. `drain-load-e` (red eight nights in a row, a test defect): the backup Job's second pod copied the first one's
  files (Q-HA-301). Bastion: three product defects, a feed entry applied again after its ID was pruned, a credential another replica had
  confirmed refused during the failed-authentication timeout, and routing by path that sent HTTP/1.1 and HTTP/2 requests for one artifact
  to different replicas (Q-BAS-152 to 154). Pooler: PgBouncer's `server_login_retry` held every replica out of service for half a minute
  after a switchover (Q-HA-302, with a test that switches PostgreSQL over in every pooler run), and a request that met the switchover
  answered 500 (Q-HA-306). `access-long`: four journeys acted on a page that had not settled. Integration: the rate-limit bound, the Debian
  large index, the collapse test (Q-ARCH-496, 497). `vuln`, `vuln-long`: the e2e clients' TLS close ended the kubectl port-forward
  (Q-HA-303), and a dev stack that does not come up says why (Q-HA-304). The control pool: a closed connection kept the pool's only place
  for up to 15 s; H-822 is closed, its trigger stays open as F-136 (Q-ARCH-498, 499). The stall family: cause measured, F-133, with the
  recorder and the warm-up check of Q-HA-305.
- Not on `main`: the branch `stab/oom-2`, commit b1d9b7d6 (Q-ARCH-500, H-988; closes F-134): a response is charged against the kernel's
  send buffers before its first byte and waits for a free one, `server.send_buffer` 128 KiB. Its gates and its leg (run 44232726523) are
  green; it was held back so that the acceptance nightly ran on one commit.
  **Landed in the night of 2026-11-18** on the day's `main`, as one commit with its message unchanged: one conflict, in this
  file; no code of the day touches the admission guard, the listener's socket options or the chart's validation. Gates on the
  result: the unit tier, the packages `internal/app`, `internal/httpserver`, `test/integration/overload` and
  `test/integration/server` on the integration stack, the chart's lint and tests, lint, cyclo, generate-check, the web tier.
  Not run again: the forced reproduction (1,000 clients in a 1 GiB container) and a kind leg; the next nightly is its leg.
- The acceptance nightly 44233617621 on dd97187a, alone on the runners: 26 legs green, `e2e (drain-load-b)` red (F-137, new, not
  explained). The owner had the run cancelled at 07:46 UTC to free the runners: `drain-load-c` was cut in its run, `drain-load-e`,
  `Latency` and the vulnerability scanning performance did not start.
  Test and Release 44220539577 and 44229094138 were green in every job.
- Broke: the push of 510abd2b turned the gating `unit tests` job red for 12 minutes (`TestMarkOutage`, a caller outside the changed
  package; fixed by dd97187a). Since then the whole unit tier runs before every push, not only the touched packages.
- Leads that were wrong: a shutdown ordering behind H-822 (twelve stops did not reproduce it; the cleanup waited for the close of a
  connection). The first version of the send-buffer bound charged at admission, which halved the capacity and answered `server_busy`
  during a database outage; it was sent back.
- Where the time went: the development agent's own dispatches and pushes stalled the baseline nightly twice and one builder's leg once, before
  F-133 named the cause. A push to `main` or a dispatch beside a running kind leg makes that leg worthless as evidence.
- Open for the next run, in this order:
  1. ~~F-137~~ — closed in the night of 2026-11-18 (a race of the test, and the settle wait now ends at the drain; see F-146).
  2. (Read in the nightly 44414813006 of the night of 2026-11-18: all four green, and the `cgd:` rows on the new layout.) `drain-load-c`, `drain-load-e`, `Latency` and the vulnerability scanning performance have no result on `main` after this night's
     changes (`drain-load-e` was green on its branch only): read them in the next scheduled nightly. Not read yet in the artifacts of
     44233617621: the `cg:` rows of the stall evidence after 2a161226, `TestHAPooler_Switchover` on `main`, `vuln-long` and `alerts`
     with the changed drain helpers (all four legs were green).
  3. ~~Land `stab/oom-2` (b1d9b7d6) with the full gates~~ — landed in the night of 2026-11-18, see above.
  4. The remaining findings of section 8: F-126 (read, cause not established, the test now prints what decides it), F-119 (the case of 11-09); F-136, F-135, F-130, F-131 and F-132 are closed (night of 2026-11-18); `ci-ha` on a
     degraded hosted runner; the stalled `drain-load-pooler` of 11-11.
  5. A partial dispatch (`-f sets=…`) takes a large pod for every leg of a lane it does not run; in a full pool one leg waited 40 minutes.
- The runner change of F-133 was applied after the run, on the owner's word (see there): the next run reads the first legs on the new
  layout before anything else, and makes the recorder follow the daemon's cgroup parent.
- For the owner, one at a time: the schedule that GitHub starts 5.5 to 8.5 hours late; the decision Q-ARCH-500; the branches `stab/*` on the remote, which can go once
  `stab/oom-2` has landed.
