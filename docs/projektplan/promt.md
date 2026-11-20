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
     review, OCI virtual and the mirror and APT were not started.
11. **A UI change is rendered and looked at before it lands (owner, 2026-11-12).** The builder renders every screen it changed with
   `make ui-renders` (light and dark, wide and narrow, with data that has short and long names, several rows and an empty state),
   opens the pictures itself and judges them against the checklist of [ui-style-guide.md](../developer/ui-style-guide.md#checklist)
   before it commits; what it found and fixed goes into its report with the paths of the pictures. A unit test or a browser test that
   checks a value is not a look at the screen.
   - **Why:** T-20's address fields passed every test and shipped with a different width per row and a list that scrolled sideways;
     the owner found it in the running installation, not the run.

### Standing rules of the owner

How the owner wants the development agent to work, beyond rules 1 to 11 above. `CLAUDE.md` at the repository root holds the
others and is not repeated here: pre-alpha without backward compatibility, CI and releases until the first stable release, tests
stay runnable, simple by default with details on demand ([CLAUDE.md](../../CLAUDE.md)).

- **One question at a time** (the owner's review of 2026-11-06): a new question during the run is asked one at a time, with
  options and a recommendation, and what depends on it is not built before the answer.
- **Housekeeping** (owner, 2026-11-11): finished worktrees are removed at the end of a run, with the local branches of work that
  has landed.
- **The development agent finds such defects itself** (owner, 2026-11-18, about a long repository key that broke away from its
  icon: "Can you not see something like this yourself?"): at every station of a walk the development agent looks at the render
  first and names what it sees before the owner has to.
- **A person is chosen from a search that shows the people it found, everywhere** (owner, 2026-11-18, at the admin area's
  **System roles**, with a picture of the member field of his GitLab): "ne, das muss mit Personensuche passieren. Personensuchen
  immer so mit Anzeige der möglichen Personen. Das will ich überall wo Personensuchen eine Rolle spielen." Wherever a person, a
  group or a service account is named, one shared field searches while one types and lists the matches with avatar, name and
  username (R-UI-150 to R-UI-152).
- **How the work is paced** (owner, 2026-11-18, 12:35): "Can we not go on locally first, without waiting for endless release
  pipelines? I would rather go on looking." And: "You may push, but the result is of no interest in that time." The results of
  the pipeline (watching runs, red jobs, releases) are attended between 17:00 and 20:00 and between 22:00 and 08:00 local time.
  In the rest of the day the development agent runs the local gates, pushes what has landed without waiting for its run, and
  keeps the UI development profile up with the newest local state, so that the owner can look and object. A red job of a run of
  the day is read and fixed in the next window.
- **Night mode** (owner, 2026-11-18, 21:35 CET): "ich erwarte, dass du jetzt im Nacht-Modus selbstständig an der
  Veröffentlichung der aktuellen Version arbeitest und im Anschluss weiter an der CI von "HA and Performance (nightly)"
  arbeitest … Triff die für unser Projekt optimalen Entscheidungen selbstständig."
- **A decision taken at OCI holds for every format** (owner, 2026-11-19): "Alles was wir hier für OCI besprechen gilt auch
  für alle anderen Registries die bereits gebaut sind und für zukünftige Registries. Ich verstehe, dass die API der
  Registries von ihrem Funktionsumfang unterschiedlich sind und deshalb manche Features nicht implementierbar sind. Das
  muss dann im Zweifel noch mit mir besprochen werden wie wir vorgehen. Ich kenne mich mit zum Beispiel Nuget oder NPM
  nicht so richtig aus, und erwarte von dir dass du mich bei den Entscheidungen unterstützt und ein Auge auf anderen
  Registries hast, während wir über OCI sprechen." A behaviour decided at one format is specified for the format contract and
  built for Ansible Galaxy, Debian, generic, npm, NuGet and OCI together; where a protocol cannot do it, that comes back to the
  owner as a decision of its own.
- **A topic of many linked decisions is worked out as an interactive page** (owner, 2026-11-19, at the download limits): "Ich
  erwarte von dir bei dieser Frage bitte eine Ausarbeitung als Claude-Artifact im Browser, ich will hier deutlich detailliertere
  Settings. … Bitte stell mir das einmal zum anschauen bereit und lass uns interaktiv an diesem Download-Rate-Limit arbeiten."
  He chooses and writes notes in the page; "bitte auslesen" means: read his answers back from it. Single decisions stay one at
  a time in the conversation.
- **At night a timer checks every 30 minutes that the work goes on** (owner, 2026-11-19): "stell dir einen timer alle 30min um
  zu checken dass auch wirklich noch gearbeitet wird. Teilweise hören deine Watches auf zu funktionieren, wenn der Cache Cold
  wird. Ich möchte auf keinen Fall morgen aufwachen und du hast vergessen nach dem subagent weiter zu machen." Each check looks
  itself (the builders' worktrees, the pipeline, the nightly) and does not rely on a watcher; it ends when nothing is open.
- **The walk's items are built by subagents of the largest model at high effort, at most four at a time, each in its own
  worktree, with no review agent after them** (the owner's frame for the walk of 2026-11-18, with rule 10); the development
  agent looks at every screen itself before it lands.
- **The repository of the runners' image takes no commit without the owner's word** (`container-images`, its own `CLAUDE.md`;
  he released one commit and push for the image on 2026-11-19): ask again each time.

## 1 · Find the next phase

1. `git status` must be clean; then `git pull --rebase origin main`.
2. During the alpha (rule 10) a run works on two fronts at once: the first open task of
   [section 10](#10--owner-tasks-of-2026-11-07-the-next-registries), cut into work packages with an agent and a branch each, and
   every open finding of section 8 with an agent each. Open there now (2026-11-20): no registry task; the work points of the walk at the end of section 10, the list [With the owner](#with-the-owner) and section 12; then the next unit of item 3. The open items of BETA.md phase 7 are
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
5. Open findings ([section 8](#8--open-findings)): a blocking finding (rule 10) is fixed at once; the others are
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

1. `git push origin main`. A commit that changes only this plan or other documentation ends with `[skip ci]` and starts no run; in a
   push that carries code it stands **below** the code commits, so that the head starts the run, and nothing is pushed between a
   code push and the Semantic Release of its run (a `[skip ci]` head made a release skip its commits once, 2026-11-18). A code
   push while `HA and Performance (nightly)` runs costs that nightly its latency numbers (2026-11-20, a load leg six to nine
   times slower beside a pipeline's ten kind legs).
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
here; remove a finding in the commit that fixes it. What the owner has to decide is in the list
[With the owner](#with-the-owner) of section 11; what is untested or to refactor is in [section 12](#12--to-test-and-to-refactor).

### Tests and tools

Several entries below name **F-133**, closed on 2026-11-19: the stall family on the CI runners. Every kind cluster of a machine
sat in one cgroup of weight 100, the runner pods in one of weight 1250, so beside the small jobs of a Test and Release run a
leg's nodes were left with about two CPUs. Since the change of the runner definitions of 2026-11-18 a leg's kind nodes are
charged to its own pod, and a neighbour costs a loaded leg about a tenth of its CPUs
([Q-HA-305](../spec/ha-and-helm.md#q-ha-305--a-stall-of-the-kind-nodes-is-told-apart-from-a-neighbours-load-and-a-fault-test-does-not-start-on-a-sick-cluster)
records the recorder that tells such a stall apart).

- **F-165 · One instance of the request pooler had no server for 33.7 s after a PostgreSQL switchover** (the full nightly on
  a quiet `main`, 44653040868, `e2e (ha-pooler)`, 2026-11-20 05:15 UTC; `TestHAPooler_Switchover`, R-HA-058, Q-HA-302: the
  bound is `health.database_outage_grace`, 20 s). `quader-db-pooler-rw-…-82mgv` refused 1,248 clients with
  `server_login_retry` from 05:15:07 to 05:15:41; its sibling of the same pool was back after 8.1 s, the two instances of
  the control pooler after 7.3 and 7.8 s. Everything the load asserts held: 16,118 k6 requests with no failure (p99 8.27 s
  over the switchover), 96 `npm ci` and 14 publishes all exit 0, every acknowledged publish installs, no restart, `/readyz`
  200 on every replica. The four pooler faults before it in the same leg were clean (k6 p99 174 to 191 ms). Nothing of the
  download limits is in it. Cause not known (why one of two instances of the same Deployment needed 25 s longer to find the
  new primary); one red run, not re-run: the owner ended the nightly for the day when he saw it. Next: read it in the next
  full nightly.
- **F-160 · `drain-load-e` failed its latency condition once, with every functional condition held** (nightly 44595121737 of
  2026-11-19, the code of `ad01d57e`, the last nightly before the download limits). R-HA-059: the k6 p99 from the first
  eviction until two minutes after the drain was 1.48 s over 35,826 requests against a warm-up of 532 ms, the limit being twice
  the warm-up (npm alone 551 ms to 1.698 s, NuGet alone 326 to 737 ms). Held: 449 `npm ci`, 43 publishes, 330 restores and 45
  pushes all exit 0, no SIGKILL, `gc-full` handed over; one upload probe got a 502 once and passed on its second attempt (the
  pattern of H-795). Not reproduced in the two runs after it (warm-up 345 ms, drain 483 ms on 2026-11-20). Cause not known.
- **F-153 · `Set up Go` ran into its bound of five minutes in a gating job** (2026-11-19, Test and Release 44453761606 of
  6cd1cb09, job `Cyclomatic Complexity`; a full nightly ran beside it on purpose). The step takes 10 to 20 s; what held it for
  five minutes is not read. Open: whether five minutes are too few for a download on a loaded pool; one occurrence is not a
  measurement.
- **F-144 · Two legs did not get their cluster: the database was not ready after ten minutes** (2026-11-18, `e2e (npm)` and
  `e2e (push)` of Test and Release 44358371450, the run of bdbe37c5): `deploy/dev/up.sh` installed the dependencies in
  parallel, HAProxy, the PKI, the secrets and the Valkey operator were ready after 1m 43s, and at 10 minutes "postgres
  FAILED … timed out waiting for the condition on clusters/quader-db" in both legs at the same second; no test ran. Green in the
  run's second attempt. It looks like the stall family of F-133 (several kind legs at once on one machine); not read whether
  it is, the legs' pressure records were not opened.
- **F-141 · `e2e (ci)` was red once for a certificate the test did not trust** (2026-11-18, Test and Release 44346085826, the
  run of 1785aff2): `GET /api/v1/authz/me` of the fixture of `TestCIPipeline` answered "tls: failed to verify certificate:
  x509: certificate signed by unknown authority (possibly because of "x509: ECDSA verification failure" …)"; `TestCITokens`
  and `TestCITasks` failed with it. Nothing of that push touches certificates or the edge. Cause not read; to be read if it
  shows again (compare F-127).
- **F-136, closed by Q-ARCH-503, leaves one point open:** the job worker's heartbeat of running jobs still cancels on stop (no
  job runs by then).
- F-128 · `Semantic Release` failed once in Test and Release 43316531262 (40d37ac2, 2026-11-12): GitHub answered the push of the tag
  v0.55.2 with `! [remote rejected] v0.55.2 -> v0.55.2 (Internal Server Error)`; no tag and no release were made, and the next push
  released the same commits. Read, not reproduced: an error on GitHub's side, nothing in the workflow changed. Done when it does not
  recur; at a recurrence, re-run the job once before anything else.
- **F-127 · `e2e (push)` failed with a certificate the job's own CA did not verify** (Test and Release 43022311289, 9220e777, green
  on its first re-run): about four minutes after `up.sh`'s ping check, `x509: ECDSA verification failure` against the job's own
  `quader dev CA` on every request through the edge and `password authentication failed for user "quader"` in the migration Job of
  `TestE04MigrationFailure`; both are secrets the in-cluster generator writes. **Cause found locally on 2026-11-12:** the
  internal-secrets-operator of the local kind cluster was OOM-killed at every start (limit 128 MiB, 78 helm release Secrets of 23 MB
  in its cache, amd64 emulation on the arm64 host); the dev profile now gives it 512 MiB and keeps two helm revisions per release.
  Not verified: whether the CI legs had the same cause (CI runs a fresh cluster on amd64). The operator's log is kept in the e2e
  evidence (`kept-pod-logs/`) and not yet read.
- **F-126 · `TestConcurrentWritersOnOnePath` R-STOR-003 refused one of the PUTs or DELETEs of six writers on one folder once**
  (Test and Release 43013671879, shard b); the status was not logged. **Read in the night of 2026-11-18, cause not established:** a
  delete never waits on a folder lock (`SKIP LOCKED`), other deadlocks are retried three times; a deploy's link step gives up with
  503 after five rounds, each of which needs another writer to commit an ancestor's removal inside it (not reachable in practice
  with two writers per folder, not excluded; the spec names the limit and its 503); a 503 of the object store or the database, or a
  500, cannot be told from the code. The test now prints the replicas' request log with the first refused request: a 503 "The path
  is being changed by concurrent requests" is the five-round limit, a 503 with a store or database cause is the infrastructure, a
  500 is a fault of the product. Next step: read it in a recurrence.
- **F-124 · Tarballs of 1 to 5 MB stall at the edge for about 40 s in one window** (nightly 42994478570 `drain-roles`, and
  `ha-faults` `ValkeyCredentialMismatch` of 42774548669): 53 to 72 requests for the `blob` package's tarballs hit k6's 45 s timeout
  within about 5 s; HAProxy logs them `CD` with 3.2 to 4.9 MB streamed in 36 to 38 s and about 1,800 connections open. Not the drain.
  Same night, `drain-load-pooler` (variant D): from 11:40:50 until 11:44:10 the edge held 1,500 to 1,800 connections, k6 dropped
  1,501 iterations, `npm ci` took up to 3m35s and every backend lost its servers at 11:42:50 and 11:43:15; not the switchover of
  Q-HA-302. Its recurrence of 2026-11-18 (nightly 44186978088, `ha-faults` `ValkeySlow`) had F-133 as its cause (eight small jobs on
  the same machine). The two runs named first predate the recorder and their node is not known. Open until a recurrence is read
  with the CPU time per cgroup (Q-HA-305); a tarball stall without neighbours would be the leg's own.
- **`ha-faults`** (run 42774548669): `ValkeyFlushAll` answered 503 from 07:53:20, 70 s before the FLUSHALL, while the run shared the
  runners with Test and Release (five small jobs beside it, no other kind leg); `ValkeyCredentialMismatch` had 53 tarball requests
  of the `blob` package at k6's 45 s timeout within 6 s of the pod deletion (the shape of F-124); pod logs of the window are not
  kept. `ValkeySlow` failed in the nightly of 11-11 (p99 5.2 s against a bound of 2.4 s, no failed request, three small jobs beside
  it). Open: the 11-11 and 11-10 windows, read again at a recurrence with the CPU time per cgroup (Q-HA-305).
- **502 bursts at the edge (H-795)**: `npm-ha`, `drain-load-e`, `TestE14Drain` (runs 42885398204 and 42902356913: PUTs of the upload
  loop answered 502 after 17.5 to 18 s, in a stall of the whole CI cluster of about 17 s; HAProxy `SH--` 502s on the API pings as
  well, TLS handshake timeouts on every packages pod, `valkey degraded`), node loss. HAProxy `SH--`, 40 of 41 with a connect time
  over 1 s on three backends within 2 s; k6 measures 12 to 14 s where HAProxy measures 2 to 3 s, so the time may be lost before the
  accept. No reload in the window; dropped SYNs are ruled out (no kernel counter moved on kind during bursts of 1 to 2.7 s). The slow
  requests pause and resume together at a CPU pressure of the shared VM of 450 ms/s on average. Again in nightly 42994478570
  (`e2e (npm-ha)` drain variant A) and in Test and Release 42894886354 (`e2e (access-long)`, a login POST that waited 13 s at the
  edge). The `npm-ha` window of 11-11 had 12 small jobs and 4 other kind legs beside it, which fits F-133; the legs of Test and
  Release always run beside their own small jobs, so overlap does not decide those. Seen again as one upload probe per drain leg on
  2026-11-19 and 20. Open: the `SH--` itself, and each case until a recurrence is read with the CPU time per cgroup (Q-HA-305).
- **A stall of the whole cluster in the set `push`** (`TestE15TLS`, run 42839263211, 15:15:46 to 15:15:56): the edge answered 1 to 15
  requests a second instead of about 55, the api pods 3 to 6 instead of 45 to 60, PostgreSQL statements took 60 to 290 ms, Valkey
  went `circuit_open`; the certificate is not involved. The rotation row now runs a closed loop (Q-HA-288, Q-HA-291). Open: what
  stalls the cluster, and the 7 of about 10,000 answers 502 of run 42818570044, whose evidence was not re-read. Again in run
  42907942036 (24cf0250): `TestE06Lease` got 11 of the 15 samples it wants with a p99 of 6.4 s for P-14, and `TestE15TLS` saw one ops
  listener serve the new certificate 4.6 s after its file changed against a bound of 3 s, every answer 200; both are wall-clock
  bounds in a stalled run, and the reading of what stalled it comes before either bound or the product is touched. Mechanism as in
  F-133; open until a recurrence is read with the CPU time per cgroup (Q-HA-305).
- **F-121 · The nightly performance run misses V-7b on a loaded runner** (runs 42571414393 with p99 314.7 ms and 42595813923 with
  610.957 ms against 300 ms; V-7a 17 to 60 ms in the same runs; both shared the nodes with other kind legs). Not a regression of the
  path that [Q-VULN-81](../spec/vulnerability-scanning.md) decided: on the integration stack the run's 100 requests give a p99 of
  76 ms, every request runs exactly two statements, and the test now asserts them, so a regression of the path fails before a
  runner does. Open: the cause of the two slow requests on the runner is not measured (a neighbour, the replica's garbage
  collection, or the autovacuum of the 100,000 rows the run inserts without an `ANALYZE`). Next step: a run of `vuln-perf` alone on
  the large class (after the kind legs, as [Q-ARCH-325](../spec/architecture.md) set) that logs the slowest five requests with
  their request IDs; revisit if V-7b misses there as well, which would make it the reference environment's number
  ([H-491](../security/vulnerability-scanning.md#h-491--the-performance-run-is-not-the-reference-environment)).
- **F-119 · The transfer loop of the drain tests saw 429 and 502 when the cluster stalled** (Test and Release 42561072166, 2026-11-09,
  and the nightly dispatch 42578594755 of the set `drain-roles`). Fixed for the transfer loop (Q-HA-230); (2) is closed
  ([Q-ARCH-446](../spec/architecture.md#q-arch-446--a-response-to-a-request-whose-body-is-unread-is-sent-first-then-the-body-is-read-and-discarded-within-a-bound)).
  Open: (1) what stalled the whole cluster at 07:50:08 (every `npm ci` took 40 s, the edge's connect time to the pods reached its
  5 s bound, the `packages` pods served a third of their rate), with no pod event, restart or failover in that minute; beside it ran
  one other kind leg (`npm-ha` of the same dispatch) and no small job, so the small jobs of F-133 do not explain it (two kind legs on
  one machine may; their node is not known); open until a recurrence is read with the CPU time per cgroup (Q-HA-305). (3) the 13 of
  11,529 requests answered 502 in the certificate rotation row of 42561072166 and the two later runs of the set `push`
  (42563355283: 10 of 3,245): read in the recurrence 42618038153, the reload is not the cause; the 502s came in the first seconds of
  the open-loop ramp, when the pod was starved of CPU and its listener gave up TLS handshakes at the 10 s `read_header_timeout`;
  no 502 was reproduced behind HAProxy 3.0 with up to 2,500 clients on 0.3 CPU, so the starvation of the kind node is the
  precondition. Next step: measure what starves the pods at the ramp before the product is touched; the rotation row runs `load.js`
  as a closed loop since Q-HA-288. (4) the p99 conditions of `TestDrainRoles` (R-HA-059, Q-HA-94) failed in two nightly dispatches
  that had other large legs running at the same time, while the transfer loop had 0 failures; revisit when a run shows the stall
  again, with the evidence in `.out/e2e/evidence/` ([e2e.md](../developer/e2e.md#reading-a-stall)).
- **F-105 · `TestR_NPM_007_ASlowPublishOf200MiB` fails under load** (seen twice, passes alone). The test asserts that no database
  transaction stays open longer than 1 s while the body of a slow publish arrives. Read on 2026-11-09: the publish holds none while
  the body arrives; the watch counts every connection of the stack's database, so the candidates are a background transaction of
  the replica slowed past 1 s on the loaded host, or the commit's transaction counted because `bodySent` closes only after the
  client goroutine returns from its last write. The test now names the transactions it counted; next step: read that name in a
  recurrence and narrow the watch to it, or measure the request's own transactions instead.
- **F-102 · `TestR_BAS_014_ThousandWritesToOnePathCauseOneProbe` failed once under host load** (passes alone and three times in a
  row). Read on 2026-11-09, no structural defect found: a second probe needs a first flight that failed or was cancelled. The test
  now prints the body of a non-403 answer and every request the main double saw; next step: read those in a recurrence.
- **F-95 · One Angular unit test failed once in 1,080, its name not recorded** (seen once, on a loaded host). Done when the
  failing test is identified and its cause fixed; until then a recurrence is reported with the test's name and output.
- **F-96 · The registry's anonymous test failed once after 85 s, its message not captured** (seen once, on a loaded host; 10 of 10
  later runs passed). The test now logs the settings wait and fails with the replica's last answer
  (`setAnonymous` in `test/integration/registry/anonymous_test.go`); the next step is the recurrence with that message.
- `alerts`, `MaintenanceStaysQuiet` in nightly 42793416589: `valkey-failover.sh` reported "the primary did not move within 60 s" right
  after drain variant B; not found, possibly the Sentinel that variant B restarted had not rebuilt its view (9.6 s locally).
- `bastion`, `TestBastion_R_BAS_049_RoutingByPath`: 0 hits on both replicas, once in twelve runs; again on the long-lived kind
  cluster with T-18 (72 revalidations answered 304, 0 cache hits, no journal trigger in the 15 s window).
- `TestE14Drain` (a packages pod unready for 5 s at the end, Valkey `circuit_open`), R-ARCH-034 (26 held against 24, once),
  `TestR_DEB_010_DelayBound` (503 `database_unavailable`, once in nightly 42677190569; the suspicion on the retry budget of Q-HA-259
  is unverified).
- A job can stay queued for hours when the ARC listener loses its assignment (P-13, 2026-11-10, `assigned job=0` after a broker
  retry); the cause was not established, and a stuck run holds the nightly group until it is cancelled.
- Other integration binaries that take a free port from port 0 can collide as `TestEndpoints` did (`freeAddr` of
  `test/integration/server` no longer hands out an address twice, Q-HA-285).
- `image (host architecture, no push)` was red once in Test and Release 43254986257 (78bb2250): `npm ci` in the Containerfile's web
  stage ended with "Cannot find native binding" (npm's optional-dependency issue); the push changed neither package.json nor the
  lockfile, and re-run 1 was green. The cause is not verified: a failed download of a platform package of an optional dependency is
  the suspect.
- The journey `16-backups` was red on the long-lived kind cluster with T-18: it shows `?state=failed` with both filters on Any and
  no runs.
- `messages.xlf` has drifted from `npm run extract-i18n`: 687 location notes and the unit order differ. No gate checks it.
- Many existing web specs carry decision IDs in their test names (`app.projects.spec.ts`, `app.routes.spec.ts`, `app.spec.ts`), where
  the convention is requirement IDs.
- The `make image` builds of a run leave dangling images behind in the development machine's Docker VM; nothing cleans them (the VM
  was full on 2026-11-12).
- UI development profile: the volumes of `deploy/ui-dev` are shared by the whole machine, but the key ring in `.dev/ui-dev` belongs to
  each checkout, so a second checkout writes a new key into the same database. [web-ui.md](../developer/web-ui.md) describes the
  workaround.
- A disk watcher in the jobs, so that a lost runner can be read, was proposed and not built.
- The 429s of the renders came from the `read` class (`make ui-renders` opens three pages of one user at once); the UI development
  profile admits more. Not verified: restoring many tabs at once may reach the limit.

### Product

- The NuGet README route is still the P1 stub (R-NUGET-024): it answers 501 behind the wiki gate.
- The vulnerability pages (the report, a finding, an advisory, the advisory sources) keep a hand-made header with quiet links instead
  of the shared page header (UI-A-22 of the T-21 audit).
- A plain user still sees the vulnerability report's stale and pending advisory banners (only their link is gated by
  `settings:read`) and the coverage line "internal names only, … waiting for advisory data, … not covered" (UI-A-16 of the T-21
  audit).
- A Details section does not remember whether it was open (Q-UI-319).
- The audit log: the action filter's suggestions are a hand-made prefix list; `class="muted"` styles nothing; the log scrolled 148 px
  sideways at 1280 px (the Source address column).
- The creation form of a local npm repository offers Security audit's "Forward to an advisory service" with Provider URL and Timeout,
  which a local repository ignores (it behaves as Off); the schema could show them on remote and virtual repositories only.
- No integration test checks the 401 challenge of Debian or of the `formats.Base` formats without a credential, and no real apt
  client was run against it.
- No code counts the metrics `quader_deb_remote_parse_total` (R-DEB-033) and `quader_oci_upstream_token_requests_total`
  (R-OCI-034); [deb.md](../developer/deb.md) and [oci.md](../developer/oci.md) say so.
- Ansible Galaxy, open gaps on [formats/ansible.md](../spec/formats/ansible.md) and its security pages: H-861 (digest conflicts
  counted per merge), H-863 (a role lookup is not merged across members), H-851 (the core's archive bounds). The base64 body of a
  publish is held to the system upload limit before `ansible.max_archive_bytes` sees the decoded bytes (Q-ANS-53), not tested.
- H-836: a holder of `tokens:read` alone sees which token owners are administrators; only a custom role is affected, because the
  three system roles with `tokens:read` also hold `identities:read`.
- Image scanning: the thresholds of `QuaderSpoolNearlyFull` were not checked against the larger worker spool of 8 GiB.
- `api-and-ui.md` has Q-API-260 to 263 after its first `## What this does not cover` and a second such heading after them.
- R-UI-002 and `_shell.scss` cite UI-A-48, an item of the T-21 audit that no longer exists.
- The coverage badge and the evidence record stand still until the first stable release (Q-ARCH-457).

**Phase 7, not closed** (BETA.md stays 🟡): E5.9, E5.10 and E5.5 need green runs, E5.1 a run of the nightly job on a fresh cluster (the
drill with the new commands passed on kind on 2026-11-10, Q-HA-289), H-798 is unexplained (drained pods killed after their grace
period), and E5.6 misses 18 MVP IDs. The beta candidate is published only when every criterion is met.

### Decisions of the owner that only this plan records

- **F-151 · The container scan's finding in the image scanning's analyser** (`usr/local/bin/trivy (gobinary)`, `stdlib`
  CVE-2026-97054) is accepted at that one path until 2026-12-19 in `.trivyignore.yaml` (H-1003). Decided by the development agent in
  the owner's absence and confirmed by the owner on 2026-11-19 ("Option A"); H-1003 does not name him. Open: the entry goes with the
  next raise of the analyser's pin.
- **F-154 · The runners' own image** (owner, 2026-11-19, "Option A, ich glaube das ist schon lange überfällig"): it is built in
  `guided-traffic/container-images`; and the tag rule ("Option A"): the runners name the tag that does not move,
  `<runner version>-<build time>`. [releasing.md](../developer/releasing.md) describes both without the owner.

## 10 · Owner tasks of 2026-11-07: the next registries

A registry task of the owner is one phase. It comes before the next unit of [FULL.md](FULL.md) (step 1, item 3) and after the
findings of section 8. Set the matching FULL.md entry to 🟡 when the phase starts and to 🟢 with the evidence when it is done; remove
the task here in the commit that closes it.

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

No registry task is open: T-16 (Docker) and T-17 (APT) are done, and so are the owner's later tasks T-18 to T-22. A new registry task
of the owner is added here.

### Owner walk of 2026-11-18: the frontend

The owner walks the UI screen by screen on the UI development profile, as the plain user `sam`, the power user `priya` and the
administrator `ada`, in his own browser; the development agent looks at the same screen's render and records one objection per
entry. The walk is part of [phase 8](BETA.md#phase-8--walkthrough-and-beta-release). When the list is long enough, builders build
it, one per package, each under rule 11. An entry marked **owner** is the owner's word; an entry marked **seen by the development
agent** is not decided until the owner has said so.

W-1 to W-26 are built and on `main`; their decisions are in the specification. What the owner has to decide or look at is in the
list [With the owner](#with-the-owner); what is untested is in section 12. Open as work:

- **W-14, from the builder's report:** the spec calls two more events warnings that are not written as one: a result of a group
  rule that violates the group-name rule is "skipped with a warning event" (R-AUTH-008), but only logged
  ([claims.go](../../internal/auth/oidc/claims.go)); `system.backup.completed` with `completed_with_warnings` is written as success.
  R-AUTH-041 calls the break-glass events' severity "high", which the model cannot say.
- **W-20:** a repository deleted during a session stays in **Recently opened** until the next sign-in or change of permissions and
  then leads to "not found".
- **The power user's screens** (the development agent's own list, after the owner said on 2026-11-18 that "nothing gross struck
  me"): the wiki's visibility behind a disclosure on the repository's settings is not done; it waited for W-11 in the same forms.
- **W-27 · Set up client is dreary and has too much text** (**owner**, 2026-11-20): "unsere Seiten von 'Set up client' [brauchen]
  mehr Bilder oder [müssen] etwas farbenfroher aufgebaut werden. Diese Code- oder Text-Blöcke sollten in keinen 'VSCode'-Fenster
  aufgelistet sein. Das hier ist so trist. Beim drauf schauen auf diese Anleitung hat man überhaupt keine Lust das mitzumachen.
  Hier muss echt was passieren. Ich wünsche mir, dass ein Subagent der sich damit auskennt, damit mal auseinander setzt." Asked
  how far the page may leave the palette: "Ne, du sollst schon in der Palette bleiben. Aber so wie es jetzt aussieht, ist es
  scheiße, ganz einfach. Zu viel Schrift, keine Lust zusehen."
  And on pictures and the blocks: "Bilder haben keine Einschränkungen der Palette. Wenn du also einen Text, anstatt in einer
  schnöden Box in einem schicken VSCode Texteditor darstellst, ist das in Ordnung."
  - **The owner's frame:** the page's own controls and text stay in the palette (the colours, type and measures of Q-UI-128);
    a picture is not bound by the palette, and a snippet drawn as a smart editor window instead of a plain box counts as one;
    less text; more pictures; a subagent that knows the field works on it.
  - **Seen by the development agent on the renders** (`setup-npm-local`, `setup-oci-local`, `setup-npm-virtual-explained`): the
    page's only colour is the three step discs and one link; it has no picture, and the format is a small glyph in a badge; a
    snippet is a grey framed box of monospace text in which a placeholder (`<package>`, `${QUADER_TOKEN}`) looks like the rest,
    with its copy button outside the box; four controls stand before step 1, and **Create a token** is a default button; nine
    closed sections follow the steps as a list of lines, and opened the page is 9,332 px high at 1,440 px; the right third of
    the page is empty at 1,440 px.
  - **Chosen by the owner** (2026-11-20, from three variants a designer drew as a mockup with renders, `.out/w27/`, not
    committed): "Variante A ist am besten und gefällt mir." Variant A keeps today's order and column. A snippet is a window:
    an editor for a file (a tab with the file's name, line numbers, **Download** and **Copy** in its bar), a terminal for a
    command, light in the light scheme and dark in the dark one, with syntax colours and the placeholders marked. A small
    picture of the flow (the machine, the token, the repository) stands in the header, and a rail joins the steps. The
    credential is one framed row with **Create a token** as the page's one blue button. The sentences are cut (npm: 72 words
    before the end of step 2 become 38; OCI: 157 become 45), a second way of a step (Kubernetes) is folded. The explanation
    stays on the page, as
    [ADR-202](../decisions.md#adr-202--the-client-setup-and-the-guide-are-one-page-of-a-repository) has it, as closed rows
    under **Guide**. One design for every format; a format adds at most an accent inside the pictures and a glyph, and no
    screen to the render set. No logo of npm, Ansible Galaxy or Docker (Q-UI-225 to 227). Not chosen: B (a picture per step,
    the explanation in a drawer), C (a workbench with live substitution).
  - In work: a builder builds variant A under rule 11.

## 11 · Run record

What a run did, in short, so the next run starts where it stopped. The plan items, findings and decisions carry the details. Past
runs are in `git log`; this section holds the current state.

**State on 2026-11-20** (`main` at `38b1d1ec`).

- **In work** (started 2026-11-20, 09:30, nothing landed yet): W-27: a builder builds variant A, the owner's choice; a builder for W-14; a builder for W-20 and
  the wiki's visibility behind a disclosure. W-12 is decided and not built now: the owner chose an invitation by e-mail, later
  ("Option c, aber erst später", 2026-11-20); it stands in [FULL.md](FULL.md#m9--automation-and-integration), M9.
- **To check first:** F-165 in the next full nightly; then section 12.

### With the owner

Everything that waits for the owner, one line each, with where the detail is. Put to him one at a time (standing rules).

**Questions he has not answered**

- **Q-HA-293**, to be discussed, not decided: a replica whose first Valkey attempts get no answer becomes ready degraded (Q-HA-13
  (d)), so a wrong password behind an unanswered first attempt is noticed only later. Should the first readiness wait for a verdict?
  Waiting would stall a replica started during a Valkey failover, which Q-HA-13 (c) rejected ([ha-and-helm.md](../spec/ha-and-helm.md)).
- **H-812** ([image-scanning.md](../security/image-scanning.md)): Trivy shares the file system and network of the worker pod, so a
  crafted image that compromises it can read the secrets mounted there. Close it before images from untrusted remotes are scanned
  (a sandbox or a pod per scan, a further component), or accept it? A dedicated `vuln` worker pool would shrink it; the owner kept
  the plain workers on 2026-11-10 (Q-VULN-137).
- **Format logos** (Q-UI-225 to 227): npm, Ansible Galaxy and the Docker whale may be shown only with their owners' permission, so
  npm and Ansible Galaxy keep the neutral glyph (and Guides show it on the tiles of Ansible Galaxy, Generic and npm). To show them,
  the owner asks npm, Inc. and Red Hat for permission. At the walkthrough: the logos in the running app, light and dark, in Firefox
  and Safari (only Chrome on a static page was looked at).
- **Presets of the installation's own registries** (T-21b): should an administrator be able to add the installation's own
  registries (for example the registry of his GitLab) as preset buttons? (a) No: the formats' public presets only; the own registry
  is typed once per repository, and **Allow** fixes its token service in one click. (b) Yes: a list per format in the general
  settings, served with the format's presets. Recommendation: (a) now, (b) as its own task if he wants it.
- **Palette** (T-21c): should the vulnerability report's trend chart and the grade bar share one palette?
- **What a client does at a limit** (section 12 B), each with the builder's options: docker gives up at once on any 429 where the
  other clients wait (keep; hold a request for seconds on the registry's paths, which changes his decision 4; or larger default
  bursts); a client that retries a second apart is never admitted at a tight request limit because a refused request still counts
  in the window (keep; or do not count refused requests in the request windows); podman holds a byte-limit refusal for about 16
  minutes (document; or a short `Retry-After` there); the new client cases run when their format changes, not when only the limits'
  code changes (keep, as the rule has it; or add the limits' paths to the five legs).
- **Under overload a caller at its parallel figure waits ten seconds for its refusal**, where the server's own shedding answers at
  once (F-163, section 12 B): should a limit answer faster when the replica is already shedding?

**Decisions the development agent or a builder took, for his walkthrough** (each decision block says so)

- T-20: Q-UI-274 gives the address field the editable control's fill instead of the read-only grey; not compared with GitLab's
  clone field in a browser.
- T-21b: Q-UI-353, the title uses the format's display name ("New OCI (Docker) repository"), where he wrote "New OCI repository";
  his question 2 of that plan was (a) the display name, one name per format everywhere (chosen and recommended), or (b) a short title
  name per format, as a second catalog fact.
- T-22: an instance admin is the system role `admin` only (Q-UI-412); "frequently visited" stays in the browser, per user (Q-UI-405);
  "Search in <project>" first inside a project is not part of T-22: (a) the field always searches everything the caller may read,
  and the project is shown on every entry (chosen), (b) inside a project the first entry is "Search in <project>" and the results
  page takes `project=`, recommended after the walkthrough if he misses it.
- The lists (W-4, Q-UI-426): the description stays under the key instead of in a column of its own, so that every repository list
  is built from the same cell; the admin area's repository list keeps its copy button for the address, because his objection named
  the project's list.
- W-24: the column is named **Last download** where his word was "Last fetched" (a version a mirror pass stored was fetched by
  Quader and by no client); **Published** of a version that exists in a remote only is the time it was cached, not the upstream's
  date.
- W-25: a repository can switch the background download off, on by default (Q-REPO-360); the bounds of one background download
  (Q-REPO-363); the include and exclude patterns hold for every background fill (Q-REPO-365); a probe through a virtual repository
  (Q-REPO-367). Gaps: Docker Hub counts the background manifests as pulls (H-1017); an index whose platform manifests are stored and
  some of whose layers are not is not completed (H-1018); a NuGet virtual repository with several remote members may start at a
  member whose upstream lacks the version (H-1019).
- W-26: an Owner does not rotate a technical token, it revokes and issues a new secret (Q-AUTH-419); the limit of a technical token
  is set from its cell, the expiry stands beside the hint (Q-UI-759, Q-UI-761); ended tokens are a table of their own (Q-UI-740); a
  deleted technical token's rules and blocks go with the daily task, its usage stays until retention (Q-LIM-49); a rule's single
  figures start with the profile's values (Q-UI-752); a download of the UI asks for one byte first (Q-UI-753); a project without a
  maximum is read as 200 with `maximum: null` (Q-API-356); a registry request answered with the challenge is not charged
  (Q-API-357); the token command's `--replace` (Q-AUTH-421); the class `package_write` holds on the registry's pushes in
  Enforce, burst 400 (Q-LIM-56).
- Said to him once and not answered: the schedule rule of the nightly (a scheduled run skips its legs when a full dispatched run
  ended less than 12 hours before; [releasing.md](../developer/releasing.md)); Q-ARCH-500 (the send buffer bound); Q-HA-313 (the
  upgrade test across the rename's cut is removed); Q-HA-315 (the bridge of the roles is gone from the chart).
- W-17: `security-admin` holds `vulnerabilities:triage` but not `content:metadata-read`, so that role has no security dashboard
  since the admin area's entry is gone.
- W-19: the row **Last backup** stands in the last card at the bottom of the dashboard, where a missing backup does not catch the
  eye; a project's Owner who follows a repository screen's link to the jobs lands on a page titled "System status" with an empty
  side bar and the breadcrumb "Admin area" (Q-UI-652).
- W-12: the table of a system role's bindings still writes `user:ada`.
- W-20: a repository's header lost its address field; "Address for downloads by path" in a repository page's **Details** and
  "Client address on this endpoint" in the admin area's exposure rule editor stayed.
- W-5: the upload dialog's states in lower case; the start page's empty **Your role** cell; whether the policy editor's selector
  button may wrap at 390 px.
- W-22: whether the public remote for the guidedtraffic images (`platform/guidedtraffic` on the development stack) is also wanted
  on his installation.

**Seen by the development agent in the walk, not decided** (still so in the code on 2026-11-20)

- `login`: the local form of the break-glass account stands first with the primary button, the sign-in provider below it as a
  secondary button; every user except the break-glass administrator signs in through a provider.
- Every screen inside a project: the breadcrumb names the project by its key (`web-frontend`), the side bar by its name.
- `packages`: the project's list of packages has no grade and no column that says which repository holds the package; the tabs
  **Recently opened** and **Saved views** stand in the plain user's main view.
- `vulnerabilities`: the report is one row per finding (1,222 on the seed; the first 60 rows are three packages), so the plain
  user's question, which package and which version is affected and how badly, has no row; the chart **Open findings over time**
  stands above the findings.
- **Needs triage** for a reader: the tile and the status column of the report, and the status on a version's page, show it to a
  reader, who cannot triage; a file's page names a person as "by user:priya".
- `client-setup?client=docker` of a public remote offers a visitor `docker login … --username quader` as its first snippet, though
  the pull that follows needs no credential.
- `repository-settings-*`: several **Save changes** buttons, one under each open section; the header's only button is
  **Vulnerabilities**; the visibility is two groups of three choices, **Artifacts** and **Wiki**, in the main view.
- `roles`: the capabilities are 25 codes in four columns, in no order.
- `dashboard`: a component is called "all".
- A user's page in the admin area is titled with the username, not the name; the **ID** and the origin stand in the main form;
  **Display name** and **Email** are fields with a **Save changes** though both are replaced at the next sign-in; two **Save
  changes** on one page.
- A new CI issuer has **Accepted audiences** and **Pin claims** in the main form, above **More settings**.
- A new permission policy: the links to its sections are underlined blue text, a form the app uses nowhere else.
- The browser's own select box on a group (**Source**) and in a policy's "Try it" (**Action**).
- An address that does not exist under `/ui/admin/` shows "Page not found" without the side bar, the search and the user menu.
- Not established on 2026-11-20 (needs a render or the seed read): **Owners** empty in three of four rows of the admin area's
  projects; the security chart of 90 days as one bar on an installation whose data is a day old; the start page's list of projects
  wider than the page at 1,100 px; the header's menu button alone on a line at 390 px.

**On machines and in repositories outside this one**

- **F-36:** the issuer double's fixtures were recorded on a local GitLab 19.0.1-ee, not on his instance, which cannot create
  projects (its web service dials Gitaly under a name its certificate does not carry). Done when, after the certificate is fixed
  in his GitOps repository, `make oidc-fixtures` against his instance records the fixtures again (the licensed
  `environment_protected: "true"` included) and the double still reproduces them.
- **F-17:** on the development machine `~/.docker/cli-plugins/docker-buildx` points at Docker Desktop's own buildx
  (v0.28.0-desktop.1, pin v0.38.1), so `make versions-check` fails locally on that line; the Homebrew copy is in place
  (`ln -sf /opt/homebrew/bin/docker-buildx ~/.docker/cli-plugins/docker-buildx`, no admin password; Docker Desktop may relink it on
  its next update unless its CLI plugin management is turned off). Checked on 2026-11-08: unchanged.
- The runners: `zstd` in the runners' image would make the CI cache entries smaller and faster (every entry exists twice today);
  a registry or a mirror inside the runners' cluster would remove the downloads of the kind node image, the dev profile's images and
  k6 from every leg.
- Branches, deleted only on his word: the origin branches `p7-*`; the local branches `worktree-agent-a135bce1ea0ac051f` (the
  clients-first test of the mirror) and `worktree-agent-aa6a3feeee93a6b37` (the slow npm publish test of F-105, a temp sweep and an
  S3 cleanup of the test stack), each with a WIP commit of work that never landed; the `stab/*` and `night/*` branches and the
  finished builders' worktrees under `.claude/worktrees`.

## 12 · To test and to refactor

What is known to be untested, unverified or wrongly shaped, in one place (owner, 2026-11-20: "Es wäre schön wenn du alles was
wir mal testen und ggf. refactoren sollten mal in die docs/projektplan/promt.md aufnimmst"). An entry says what is missing, why
it matters and where the details are; it is removed in the commit that closes it. Nothing here is started by being listed. The
rule every entry is built by is "Tests stay runnable" of [CLAUDE.md](../../CLAUDE.md): once, at the cheapest tier that can show
it; a format adds a constant; no more data than the assertion needs. **Collected on 2026-11-20 from this plan** (the findings,
the walk items and the run record); what the code holds beyond the plan has not been searched.

### A · The shape of the test suite (refactor)

- **What a format costs, measured on 2026-11-20** ([test-cost.md](../developer/test-cost.md); two pushes and one full nightly,
  read from the runs' reports, nothing run for it). A push costs 396.0 runner minutes (Test and Release 44693590111), a full
  nightly 682.9 (44625277245). One format with a client costs 21.2 to 38.0 runner minutes per push, 28.6 on average: 12.6
  set-up, 8.7 behaviour that is the same for every format, 7.2 what its own client does. Per nightly npm costs 99.1, NuGet
  31.4, OCI 13.7, Debian 3.5, Ansible and generic nothing. With today's structure 25 formats would be about 939 runner minutes
  per push and about 1,244 per nightly (computed from the mean of the five, not measured). **The nearest limit:** integration
  shard b runs 40.9 of its 45 minutes, and an average format adds 6.0. Not measured: a push that touches one format only (both
  pushes touched shared code), the unit tests per format, a second nightly.
- **Where the minutes are, ranked** (the measured minutes of what would move; what a cheaper tier saves is not measured, and
  nothing is designed): (1) the fixed cost of every kind leg, 95.1 of the 172.6 e2e minutes of a push and 240.6 of 658.1 of a
  nightly, `make dev-up` alone 154 to 186 s per leg; (2) drain, switchover, failover and pod crash under a client's load,
  repeated across 12 nightly legs, 331.2 minutes: the four drain-load legs 87.6, `npm-ha` with `nuget-ha` 78.6 (`nuget-ha` runs
  the three scenarios of `npm-ha` again), `npm-ha-soak` 41.3; (3) behaviour that is the same for every format inside the five
  formats' e2e legs, 21.0 per push, the mirror's clients-first test alone 11.0 across npm, NuGet and OCI; (4) the formats'
  integration packages, 30.2 per push; (5) the render set, 32.7 per push, 54.5 % of its shots are screens named after a format,
  an average format adds 183 s; (6) `vuln-long` and `vuln-pooler`, 50.2 per nightly; (7) client images loaded per leg, 11.9 per
  push and 24.2 per nightly; (8) the leg `e2e (bastion)` takes a runner for 2.2 minutes when it has nothing to run. Refuted as
  a large cost: the three integration stacks (3.0 minutes per push). **Next:** the owner decides which of these is cut first.
- **A builder cannot render on a new UI database**, which CI always has: three landings in one night passed on a filled
  database and stopped on a new one (2026-11-19/20: a migration's checksum, the write budget, a lookup right after a pull;
  a builder may not purge the database, and the development agent's full render set on a new one before the landing found
  each of them). Either builders get a database of their own, or the
  seed is made independent of what the database holds.
- **A code push during a nightly costs the nightly its numbers** (F-164: ten kind legs of Test and Release beside a load leg
  made it six to nine times slower). The latency conditions of the load legs are only as good as the runner is quiet; either
  the nightly holds the runners, or its latency conditions compare against a measurement of the same run.
- **A partial dispatch of the nightly takes a large pod for every leg of a lane it does not run** (2026-11-18); in a full pool one leg waited 40 minutes.
- **The render guard's blind spots** (W-23, the builder's list): a control beside a framed box, absolutely placed elements and
  text drawn by CSS, states the set does not render (open menus, hover, most dialogs, error states), an offset of 1 px or
  less. **The two newest checks** (`stacked-offset`, `label-apart`) were each wrong once on older screens before they were
  right; `joined` has run in one full set.
- **What the render checks of W-5 leave to the eye** (its builder's list): a wrapping column cramped beside a roomy neighbour
  ("3 findings · / 0 fixable" at 1,024 px is one), text cut at the start of a wrapped line, a stacked label off its control,
  two words for one thing across screens.

### B · The download limits (W-26) and a probe starts the download (W-25)

- **A CI token on the request path** is not tested anywhere above the unit test of its subject: the integration stack has no
  issuer.
- **A crashed replica's leases** of parallel downloads (R-LIM-039 names the case): the integration stack can only stop a
  replica gracefully, which releases them.
- **End to end, not verified:** the `RateLimit` policies of the parallel, upstream and refused figures; the charging of a host
  bound to a repository (no test); the daily fold's sums with rows older than 30 days (only that the query runs).
- **`TestR_LIM_001`** failed once in 13 runs of its builder with "1 request charged to an address", not reproduced; it prints
  the rows if it happens again.
- **A multi-platform push** against the write class was not measured (single-platform, 40 layers: Q-LIM-56).
- **The new client cases have run on `linux/arm64` only** when this was written; the first pipeline on the runners shows
  them on `linux/amd64`.
- **W-25 through a virtual repository** has integration tests of its own for OCI and npm; NuGet, Ansible and Debian have unit
  tests only; OCI through a virtual address has no unit test. The background path's own refusal by include and exclude
  patterns is exercised through one case.
- **Under overload a caller at its parallel figure waits ten seconds for its refusal** while the server sheds at once (F-163):
  not a test, a question for the owner, listed here because the capacity test only passes with its tokens exempted.
- **The four behaviours of clients the owner has not decided on** (2026-11-20; the builder's options are in
  [With the owner](#with-the-owner)): docker gives up on any 429;
  a client retrying a second apart is never admitted at a tight request limit; podman's 16 minutes at a byte limit; the
  client cases run on changes of their format only.
- **Refactor, named by the builders:** the class `package_write` is a stand-in until upload figures exist (Q-LIM-27); a
  screen cannot change a technical token's description or delete one (API only); the usage list's cursor is an offset
  (Q-LIM-45).

### C · The walk (W-12 to W-24)

- **The kind test `e1_01_login_test.go`**, changed for the ended session at load (W-21), has not been run on a cluster.
- **`upgrade-release`** (upgrade and rollback from the published release) has not run on a cluster since it was changed: a
  dispatch of the nightly that names `e2e-upgrade` shows it.
- **The render run of the walk** (2026-11-18): `start-recent` as `plain` and `power` ran into the five minutes of the test
  inside `settle`; cause not read. One screen stopped with "Timeout … exceeded" and no more; the report carries more lines
  since, the cause was not seen again.
- **No render walk switches a remote to mirror mode**: the OCI and Debian mirror settings are covered by unit tests only, and a
  row of a remote's list that a mirror pass filled is not in the render set (W-24).
- **Journey 23 (search)** failed once on a long-lived kind cluster and is green on CI's fresh one; not read.

### D · Open findings whose cause is not known (each needs a recurrence read, not a new test)

- **F-165** one instance of the request pooler without a server for 33.7 s after a switchover (one red run).
- **`TestR_DEB_025_AReleaseNoCurrentKeyVerifiesIsNotMerged`** red once by timing (Test and Release 44632375471,
  `integration (shard c)`: "the build after the change left the suite current", while the log shows the build failing with
  `member_unverified` in the second the replica shut down; no limit in it, first record of it).
- **F-160** `drain-load-e` on latency alone; not reproduced in two later runs.
- **F-126** `TestConcurrentWritersOnOnePath` refused one writer once; **F-127** `e2e (push)` with a certificate the job's own
  CA did not verify, the secrets operator suspected and its kept log not read; **F-124** tarballs of 1 to 5 MB stalling at the
  edge for about 40 s; **the 502 bursts at the edge (H-795)**, seen again as one upload probe per drain leg on 2026-11-19
  and 20; the stall of the whole cluster in the set `push`; V-7b of the vulnerability scanning performance once above its
  bound, the runner's load not measured.
- **The internal secrets operator of the local kind cluster crash-loops** (hundreds of restarts on 2026-11-10 and 11).

### E · Never run where it matters

- **On the owner's installation:** the newest release (not reachable from the development machine); an image scan (none seen
  there yet; not run: a push through the whole app with the real binary, a drain or worker loss in a cluster, the release
  image and chart in a cluster).
- **H-829:** the listing of a remote OCI repository matches a regular expression on every tag entry without an index and was
  never measured on a cache with very many tags. W-25 adds rows to that view.
- **Ansible Galaxy:** one name with versions in a local and a remote member is not run with the real clients (H-868); the hub
  exchange is tested against the stub realm only (H-842).
- **The CI caches:** a clients image loaded from the cache instead of built has not been read in a log since they landed
  (Q-ARCH-520 to Q-ARCH-526); every cache entry exists twice without `zstd` on the runners.

### F · Tests a plan named and nobody wrote

- **The documentation of Ansible collections (T-19):** `ansiblelocal` has no integration case for a denying wiki gate or for a
  collection of 6,000 files (both are unit-tested only); no test carries R-ANS-040 (a mirror pass that queues), R-ANS-043,
  R-ANS-044 or R-ANS-045 in its name; `ansiblevirtual` has no member whose wiki the denying fake closes, so a wiki-denied member
  of a virtual repository has no proof; the e2e row **wiki** checks the install and `docs-blob/`, not in a browser that the
  user sees no tab; R-ANS-046 names `test/e2e/testdata/ansible_docs/`, which does not exist (the collection's source is inlined
  in `test/e2e/ansible_docs_test.go`).
- **Ansible Galaxy (F0):** the read-only check against the public Galaxy (`https://galaxy.ansible.com/api/`) in the closing run,
  an exit criterion of F0's plan, has no test and no record.
- **APT (T-17):** the nightly set `deb-long` and the Debian rows of the e2e sets `mirror` and `mirror-long` are not built;
  `member_expired` and the dropping of a member's recorded `Date` are unit rules, not integration cases
  ([deb.md](../developer/deb.md#what-is-built-and-what-is-not)).
- **OCI (T-16):** no browser journey walks the OCI package screens ([web-ui.md](../developer/web-ui.md) names the selectors
  for it and says it has not been written).
