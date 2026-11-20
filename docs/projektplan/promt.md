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
- **This plan is kept current, always** (owner, 2026-11-20: "halte bitte immer die docs/projektplan/promt.md auf Stand"): every
  decision of his, every landing, every start and end of a builder's or planner's work and every finding is written into this
  file in the same step, and what no longer holds is taken out in that step. The file in the checkout is current even while a
  push has to wait for a release (step 6); it is pushed with the next push that may go.
- **Housekeeping** (owner, 2026-11-20: "Alte Worktrees die nicht mehr gebraucht werden können abgeräumt werden. Immer.
  Automatisch."): a worktree that is no longer needed is removed without asking, as soon as its work has landed or was given
  up, and the rest is swept at the start and the end of a run. Its local branch goes with it when every commit of it is on
  `main`; a branch with commits that are not on `main` stays. A worktree with uncommitted changes, or one locked by another
  session that still runs, is left alone and named.
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
- **The walk's items are built by subagents at high effort or above, at most four at a time, each in its own worktree, with
  no review agent after them** (the owner's frame for the walk of 2026-11-18, with rule 10); the development agent looks at
  every screen itself before it lands. **Which model a subagent runs on is the owner's to say** (2026-11-20: not the largest
  one; a subagent that has just started on another is stopped and started again, one that is nearly done may finish). Rule 1
  keeps the model's name out of the repository; it stands in the development agent's notes.
- **The repository of the runners' image takes no commit without the owner's word** (`container-images`, its own `CLAUDE.md`;
  he released one commit and push for the image on 2026-11-19): ask again each time.

## 1 · Find the next phase

1. `git status` must be clean; then `git pull --rebase origin main`.
2. During the alpha (rule 10) a run works on two fronts at once: the first open task of
   [section 10](#10--owner-tasks-of-2026-11-07-the-next-registries), cut into work packages with an agent and a branch each, and
   every open finding of section 8 with an agent each. Open there now (2026-11-20, 16:20): no registry task; the two features the owner decided on that day, the built-in project `upstream` and the role `scanner`, specified on `main` and being built in five work packages (section 11); the list [With the owner](#with-the-owner) and section 12; then the next unit of item 3. The open items of BETA.md phase 7 are
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
- The shared error notice's **Retry** sits 5.5 px off its line in the error state of every list (seen in the renders of the
  page **Upstream registries** on 2026-11-20; the notice is shared code, not that page's).

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

W-1 to W-27 are built and on `main`; their decisions are in the specification. What the owner has to decide or look at is in the
list [With the owner](#with-the-owner); what is untested is in section 12. Open as work:

Nothing of the walk is open as work: W-27 (the page **Set up client** as editor and terminal windows, with less text, the
owner's choice of variant A on 2026-11-20) is built; what he has to look at is in [With the owner](#with-the-owner).

## 11 · Run record

What a run did, in short, so the next run starts where it stopped. The plan items, findings and decisions carry the details. Past
runs are in `git log`; this section holds the current state.

**State on 2026-11-20**, 16:20.

- **Landed and released on 2026-11-20** (the last release cut is the one of `05d10d01`): the measurement of what a format
  costs ([test-cost.md](../developer/test-cost.md)); W-14 (the audit log's warnings); W-20 (a deleted repository leaves
  **Recently opened**); the wiki's visibility behind a fold; W-27 (**Set up client** in variant A) with the fix of the two
  jobs it turned red; the limits' package (the built-in profiles from a measurement, a refused request not counted, waiting
  downloads capped, a 429 not counting toward the block); the format legs of the pipeline (a `ci:` commit, no release);
  the bounded wait for Valkey's verdict before the first readiness (Q-HA-293, 30 s by default, the gaps H-1050 and
  H-1051); the level `internal` by the owner's definition (the gap H-1052); one colour scale of the severities.
- **The pipeline at 14:06:** Test and Release 44735324156 of `b261949f` was green in every job, the first run with the new
  format legs: the deciding job took 30 s, the shards ran 35.4, 39.1 and 39.3 minutes (modelled 37.2, 38.1 and 38.8), and
  `UI layout checks` and `e2e (access-long)`, red from W-27 at 12:15, are green again. Every leg ran, because that commit
  touched every leg's job: that a leg which does not apply takes no runner shows in the next push of shared code. Each
  shard has room for about three more formats: a fourth shard, or moving the formats' repeated integration tests into the
  kit, comes next (section 12 A). `TestR_DEB_025` of section 12 D was green in this run. **In the window from 17:00:** the
  runs of the afternoon's pushes.
- **The level `internal`, the owner's definition of 2026-11-20, in work on `main`:** "internal sind alle angemeldeten User
  (auch technische) außer User und seine Tokens die das external-flag tragen. Internal ist einfach eine Sichtbarkeit für
  ein Projekt. Ist das Projekt privat, braucht man eine spezifische Einladung um das Projekt zu sehen. Ist es intern, kann
  man es als nicht externer und angemeldeter User direkt sehen und lesen. Für Schreiben braucht es wieder eine gesonderte
  Berechtigung. Ist es public, darf jeder lesen, auch nicht angemeldete User, Schreiben ist nicht möglich." What was built
  (R-AUTHZ-030, Q-AUTHZ-268, the development agent's choice of 2026-11-11 that waited for his review) admitted only people.
  He corrected the development agent on two points the same day: a request through a Bastion reads by `internal` like any
  other, so Q-BAS-148 goes; and a project's service account is no account of an external party and carries no flag ("Ein
  Service-Account für ein Projekt ist IMMER ein direkter Zugang zum Projekt. Mit genau den Berechtigungen die der
  Service-Account hat."). The development agent had taken both from the specification as settled and put a question on top
  of them; they were its own earlier readings, not his. Built on 2026-11-20: a service-account token and a CI token read
  what is `internal`, the Bastion's mark on a subject is gone, and a Bastion's own registration is not internal (Q-AUTHZ-320,
  the development agent's: the Bastion reads nothing on its own credential). What it opens is H-1052 on
  [visibility.md](../security/visibility.md): a technical or CI token reads every `internal` repository at every door; live
  where a CI issuer is public, where a mapping admits fork pipelines to read, and for forks on a GitLab older than 18.4.
  **To check before the owner's installation takes the release:** whether one of the three is configured there.
- **The built-in project `upstream`, specified on `main`** on 2026-11-20
  ([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-066 to R-REPO-074, ADR-209 and ADR-211, the gaps
  H-1021 to H-1026), **built in three work packages:** (1) the project, the reconcile and the admin API (migration 00086);
  (2) membership across the project line, placeholders and usage (migration 00087); (3) the UI (the admin page, the switch,
  the member picker, the placeholder, the project's first place in a list and the arrow in its mark, Q-UI-837). Package
  1's contract commit exists and the three packages are built on it at once (**In work**, below). **Landing:** the three
  land together as one feature, package 1 first; none of it is on `main` before the admin page and the refusals are, so
  that no release carries a project `upstream` that nobody can administer.
- **The role `scanner` and the analyser's database in a repository, specified on `main`** on 2026-11-20
  ([vulnerability-scanning.md](../spec/vulnerability-scanning.md) R-VULN-041 to R-VULN-046, ADR-212 to ADR-214, the gaps
  H-1035 to H-1039 and H-1041 to H-1044), reconciled with the project `upstream` by its planner: the database's image is
  `aquasec/trivy-db` at `mirror.gcr.io`, the cache cleanup never removes the layer in use, the two system subjects stand in
  [authorization.md](../spec/authorization.md), the endpoint `scan` and its listener in
  [endpoints.md](../spec/endpoints.md); the specification counts 1,013 requirements. **Built in two work packages:** (A) the
  scanner role with its token, its listener, its chart and its tests (no migration, nothing of `upstream`); (B) the
  database in a repository (migration 00088, the task that fetches it, the sources screen), which needs
  `upstream/scanner-trivy` of package 1. **Order of landing:** migration numbers run without a gap
  (`internal/meta/postgres/steps.go`), so package 1, then 2, then B; a package that lands out of this order takes the next
  free number. **Checked by the development agent of what the planner could not verify** (2026-11-20): Docker Hub serves
  `aquasec/trivy-db` publicly; `mirror.gcr.io/aquasec/trivy-db:2` answers an OCI manifest with one layer of 141,735,981
  bytes, where the plan had assumed about 60 MB (corrected in Q-VULN-175: the 48 hours keep about 1.1 GB of older layers).
  Not checked: what the module download of the fidelity test adds to the job `container-scan`; the certificate of the
  owner's installation (section 12 E).
- **In work** (16:25), four builders; packages 2 and 3 of `upstream` have reported and each is resumed for a follow-up,
  nothing of the feature is landed:
  - **Package 1 of `upstream`, second stage.** Its contract commit is `ceca6d08` on its branch, on top of `main` (not
    landed). It builds the reconcile, the refusals, the handlers, the archive keys of Debian and Ubuntu, the order of
    `GET /api/v1/projects` (Q-UI-837) and the integration area `upstream`. **Added on 2026-11-20, 16:20, from the other two
    packages' reports,** two changes of the API's shape that the UI showed missing: (a) the item of R-REPO-073 says who
    lists a registry as a number only, where R-UI-187's question before a switch-off names "3 virtual repositories in 2
    projects" and R-UI-188 lists them by name: the item gets the number of projects and, on the read of one registry, the
    names; (b) no read tells the Owner of a team's project the state or the level of a well-known repository, which the
    member picker of R-UI-189 shows: a read of its own for a signed-in caller, without address, account or who lists it.
    It discloses nothing new: package 2's resolution explanation and the warning `member_inactive` already tell whoever
    writes a member list. The builder chooses the shapes and writes the decision blocks. From package 2: its switch
    notifies `repo_config` in its transaction, and its `main_test.go` replaces package 2's.
  - **Package 2 of `upstream`, reported:** `99daf572` on `ceca6d08`, every gate green (the unit run 2:08, the integration
    areas `upstream` 3.6 s, `limits` 121 s, `virtual` and `repository` once each because the members' SQL changed).
    Migration 00087. **What it cost:** unit tests under a second each; one integration test of about 3 s in the area
    `upstream` and one of 2.8 s in `limits`, both in shard b: about 8 s more there (the plan said 12 s). **Outside the
    plan's cut:** the format contract got `VirtualPlan.NotFound` and `VirtualPlan.Unknown`, an addition, so that the
    404 of an OCI virtual repository can carry the sentence (OCI walks its members itself). **Resumed at 16:20 for
    H-1080,** which it had found and left because `internal/authz` was closed to it: the grant index names a member of
    `upstream` in the team's project, so a token whose scope names the virtual repository does not read through the
    member, an anonymous caller gets no 401 for a member that is not public, and a Bastion takes the virtual repository's
    reach as partial. It only ever narrowed access, but pipelines with a scoped token are the main use of the central
    registries, so the feature does not land with it. The fix is amended into the package's commit.
  - **Package 3 of `upstream`, reported:** `8de3769c` on `ceca6d08`: the page **Admin area › Upstream registries** with
    the new switch, a registry's own page, the member picker's group and the placeholder, the built-in project in the
    main area with the arrow in its mark and its first place in every list of projects. Gates green (`make web-test`
    1,771 tests 54.8 s, `make web-lint`, `make ui-layout-test`, `make web-embed`). **What it cost:** about 4 s of
    component tests, no browser journey, no leg. Built against fixtures of its own, because the handlers answered 501:
    **not yet run against the real API**, and the line in `web/e2e/renders/screens.ts` is missing for the same reason.
    Not built: the sentence of **Set up client** for `scanner-trivy` (R-UI-190). **Rendered and looked at by the
    development agent** (rule 11; 210 pictures of 29 screens, both schemes, 1440, 1024 and 390 px, four roles): the
    screens hold together. **Resumed at 16:25** for items 1, 2, 3, 5 and 6 below and the missing sentence of **Set up
    client**; items 4 and 7, the two shape changes, the run against the real API and the render line wait for the
    merged tree:
    1. the project `upstream` shows **Public** in the lists of projects and a globe beside its name in the side bar,
       because that is the stored level (Q-AUTHZ-301); by the owner's definition Public means everyone reads, which is
       not true of this project. It gets an en dash and no level icon (the development agent's);
    2. on a registry's page the heads of the sections (State and visibility, Address, Account) are as large as the
       labels of the fields under them;
    3. the settings page of `scanner-trivy` says "Only members of the project and the principals a policy names" and
       "Managed in Admin area", where the level is fixed and the project has no members;
    4. a well-known repository's settings page shows the fold **Wiki visibility**, which R-REPO-069 does not name;
    5. the archive keys' fingerprints stand in the browser's own monospace face, not the product's;
    6. in the picture of a filter without a match the page's title carries the focus ring: whether the page takes the
       focus from the filter while one types has to be checked;
    7. **Explain a path** says "Disabled, so not asked" for a placeholder; package 2's `upstreamState` says why.
    Seen and left: the groups stand in the order of the format catalog, Ansible Galaxy first and OCI last (alphabetical
    by display name, as the specification says); the project switcher lists Upstream under **Your projects**; the
    shared error notice's **Retry** sits 5.5 px off its line in every list (not this package's, section 8).
  - **Package A of the scanner**, in two stages: everything up to the integration tier and the chart tests with the e2e
    case written, then, when the host is quiet and it is told to, the one run of that case on a kind cluster of its own.
  **Waiting:** package B of the scanner, for `KindSystem` of package A's first stage and for `upstream/scanner-trivy`.
  **Cut between package 1 and package B**, set by the development agent where the two plans overlapped: package 1 declares
  `scanner-trivy` (its 48 hours, its include pattern, the 404 for every client) and leaves the "in use" function empty,
  so the admin page says "Not in use" until package B supplies it; the system's own reading through the repository, the
  actor of its fills and the cleanup's exemption for the layer in use are package B's. **Landing the three packages:**
  the development agent merges them on one branch in the order 1, 2, 3; both package 1 and package 3 wrote
  `docs/manual/admin/upstream-registries.md`, and four spec and documentation pages are touched by more than one package.
  On the merged tree a builder replaces package 2's SQL helper `virtSwitchOn` by package 1's operation, adds the case
  that lists `upstream/debian` (refused with `deb_member_unverified` until the presets carry package 1's keys), and the
  UI builder runs its screens against the real API and adds the render line; then the full gates, then `main`.
  **Numbers the builders hold** for decisions and gaps of their own: package 1 Q-REPO-390, Q-AUTHZ-305 to 309, Q-API-363
  to 369, H-1053 to H-1069, ADR-217, SQLSTATE AI011; package 2 Q-REPO-400 onward, Q-AUTHZ-310, Q-LIM-75, H-1080 onward,
  SQLSTATE AI015; package 3 Q-UI-838 to 849, H-1090; package A Q-VULN-180, Q-ARCH-550, Q-HA-340, Q-OCI-235, Q-AUTHZ-330,
  Q-LIM-70, Q-OPS-220, Q-EP-90, H-1070, ADR-215. The next builder gets ranges above those.
- **One colour scale of the severities, chosen by the owner on 2026-11-20** ("Option C", from three candidates of a designer,
  `.out/sev/scale.html`, not committed): the bar of the counts stays exactly as it is, the trend takes the bar's fills for
  its bands with an edge in a mark tone of the same hue, a badge's icon takes that mark tone and its word stays in the
  text's colour, Unknown is the bar's grey hatch everywhere and None a neutral grey, and the usage charts get a token of
  their own. He asked why the example showed no Critical finding: the mockup's bar had Critical at 0 and its trend at 2 to
  6 of 500, the development agent's brief had asked for a segment with 0 and the designer put it there; shown again with
  27. Built on 2026-11-20 (Q-UI-860 to 862); in the dark scheme a very thin Critical band at 390 px is one faint row of
  pixels, which the designer had named.
- **Decided, not built now:** W-12, an invitation by e-mail, later; the logos of npm and Ansible Galaxy, he asks their
  owners later; presets of the installation's own registries, after the project `upstream`.
- **Housekeeping:** nine worktrees are locked by another session of 2026-11-18 that still runs and were left alone.
- **To check first:** the pipelines from 17:00; F-165 in the next full nightly; then section 12.

### With the owner

Everything that waits for the owner, one line each, with where the detail is. Put to him one at a time (standing rules).

**Questions he has not answered**

- **The binding of a host to a repository of `upstream`** at its root (`docker pull mirror.example.com/library/alpine`, a Docker
  Engine's `registry-mirrors`; R-EP-006, R-OCI-029): named to him on 2026-11-20 as the second step after the project is
  built; today it is a configuration at start, not a click of the admin area. An address without the project segment, which
  he had thought of ("den Root unserer API"), the development agent advised against: the path would be ambiguous between a
  project's key and a repository's (Q-REPO-212). He did not come back to it.
- **The next lever of the test suite** (section 12 A): each integration shard has room for about three more formats; a fourth
  shard, or moving the formats' repeated integration tests into the format contract's kit. And the other candidates of the
  measurement: the fixed cost of every kind leg, the fault scenarios repeated per client in the nightly, the behaviour
  repeated inside the formats' e2e legs.
- **What frees a release once the full gate returns** with the first stable release, when a format's client runs only on its
  own change and in the nightly (said to him on 2026-11-20 as a question for later; a green nightly on the commit is one way).

**Decided by the owner on 2026-11-20, in work or waiting to be built**

- **H-812** ([image-scanning.md](../security/image-scanning.md)), **decided by the owner on 2026-11-20** ("Perfekt, machen wir wie
  du analysiert hast", after he asked for a look at how Harbor's scanner adapter does it): the analysis of an image moves into
  a role `scanner` of the same binary with a Deployment of its own and no secret of the database, the object store or Valkey.
  The worker keeps the job and calls the scanner over HTTPS with the repository, the digest and a token of minutes for that
  one manifest; the scanner pulls the image through a listener of `packages`, checks the layers, runs the analyser and
  answers the package list. The detection stays in the worker, so the scanner needs neither the vulnerability database nor
  the internet; the scanner's pull counts as no download and against no limit. Specified on `main`
  ([vulnerability-scanning.md](../spec/vulnerability-scanning.md) R-VULN-041 to R-VULN-046, ADR-212 to ADR-214), being
  built (run record).
- **The built-in project `upstream` for the public registries**, **decided by the owner on 2026-11-20** in five answers; his
  words are in the decision blocks of the specification on `main`
  ([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-066 to R-REPO-074, ADR-209 and ADR-211), being built
  (run record). In short: a built-in project with the key `upstream` holds the product's well-known remote repositories, one
  per upstream preset of the formats' catalog (twelve), each off until an administrator switches it on; they are the
  product's and cannot be deleted, and nothing can be created in the project. A page of its own in the admin area switches
  each on or off and sets its visibility, with one button that switches all on and one that sets the visibility of all. An
  administrator may change a repository's upstream address (marked as differing from the product's, with a reset) and store
  an account for it, and sets the six settings that only narrow what it serves (Q-REPO-378). A virtual repository may list
  repositories of its own project and of `upstream`, and of no other; a reference never protects what it names: a member
  whose repository is off or gone stays as a placeholder and takes effect again when the repository is back. A project's
  own remote repositories stay possible. The Debian and Ubuntu repositories carry their archive keys (Q-REPO-379).
  **Its place and its mark** (owner, 2026-11-20, 15:20, with a picture of the list **Projects**; Q-UI-837): the project stands
  first in the list whenever it is to be seen, and its mark carries an arrow pointing up where every other project's carries
  its initial. The development agent's reading, for his walkthrough: it holds in every list of projects (the API returns it
  first) and under every sort, and not in the lists ordered by time or relevance (**Recently opened**, the search's results).
  **The analyser's database** lies in `upstream/scanner-trivy`: always private, only for the scanner, not an
  administrator's to switch, in use exactly while the image scanning reads through it; there is no second way that falls
  back to the public source, and an installation without the internet names a local OCI repository of its own in the
  scanning's setting. Open with him: the binding of a host (first list).
- **Format logos** (Q-UI-225 to 227), **decided by the owner on 2026-11-20** ("b, aber das machen wir später"): he asks npm, Inc.
  and Red Hat for permission to show the logos of npm and Ansible Galaxy, later; the development agent prepares the two
  texts when he says so, and until an answer both keep the neutral glyph. Still at the walkthrough: the logos in the running
  app, light and dark, in Firefox and Safari (only Chrome on a static page was looked at).
- **Presets of the installation's own registries** (T-21b), **decided by the owner on 2026-11-20** ("Auf jeden Fall b"): an
  administrator keeps a list per format of the installation's own registries (name, address, token service), and the form of
  a new remote repository offers them as buttons in every project. A preset holds no credential: each project enters its own.
  Built as a small task of its own after the project `upstream`, whose admin page touches the same place; with `upstream` a
  project's own remotes are mostly private registries, which is what changed the development agent's earlier
  recommendation of (a).
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
- W-27 (2026-11-20), beside his choice of variant A (Q-UI-800): an accent per format that is used only inside the pictures,
  coral for npm and sky for OCI as he saw them, violet for NuGet, raspberry for Debian, teal for Ansible Galaxy and amber for
  generic chosen by the builder (Q-UI-803); the client-setup answer names the parts of a snippet that are the reader's, and the
  format contract gained two fields for it (Q-UI-802, ADR-208); the OCI pull names `myapp:1.0` as the push does; one sentence
  per snippet at most, a second way of a step behind a fold, and the snippets that need a credential hidden from an anonymous
  visitor (Q-UI-804); a closed fold lost 12 px of padding on every screen (Q-UI-805). Seen by the development agent on the
  render: where a snippet has nothing to run for the reader, a terminal window holds only a comment (Ansible Galaxy, a
  repository open to anonymous readers).
- The limits (2026-11-20), built on his four decisions (larger bursts from a measurement; a refused request is not counted;
  podman's wait stays and is documented; waiting downloads are capped): the built-in profiles' new figures, requests a second
  / burst / an hour: Anonymous 50 / 1,000 / 20,000, Restricted 100 / 1,000 / 25,000, Standard 200 / 4,000 / 100,000,
  Technical 1,000 / 20,000 / 1,000,000 (Q-LIM-59; measured: a pull of 40 layers is 48 requests in 0.28 s); the request
  windows are decided and counted by one script in the round trip of before, so **Quader's Valkey user needs `EVALSHA` and
  `EVAL`** (Q-LIM-57; his installation runs as the operator's `default` user, H-72, and is not affected); every rate limit
  of the installation counts nothing when it refuses (Q-LIM-61); waiting downloads may hold a quarter of a replica's request
  slots, 1,024 of 4,096 (Q-LIM-60). **Decided by the owner on 2026-11-20** ("Option B: Ein 429 zählt nicht zur Sperre"): a 429 of a
  request figure is not a refused request, as before the package; the builder had made it one (Q-LIM-58) on the development
  agent's wrong word that it already was, and a client with many connections would have reached the block of 15 minutes in
  one overflow. A caller that keeps sending is held to its figure's rate and nothing more; the block stays for the refusals
  it was built for. Taken back on 2026-11-20.
- The format legs of the pipeline (2026-11-20, beside his decision Q-ARCH-540): npm is the reference client (Q-ARCH-541); what
  shared code is, as a list, and that a path on no list starts the reference leg (Q-ARCH-542); a change under `web/` alone
  starts no format's leg; one job at the start decides, and a pull request is decided like a push (Q-ARCH-543); the rule
  that puts a package into an integration shard (Q-ARCH-544); the formats' client sets in the nightly's third lane
  (Q-ARCH-545).
- The first readiness and Valkey (2026-11-20, beside his decision Q-HA-293): the wait stands at step 10 of the start
  (Q-HA-330); what an answer is: an accepted or refused credential, a missing right on the probe write or the take script,
  a certificate that is not trusted; what is silence: a deadline, a refused connection, a failover in progress, a host name
  that does not resolve (Q-HA-331, the gaps H-1050 and H-1051); the bound is the setting `valkey.startup_answer_timeout`,
  30 s by default (Q-HA-332); a Bastion does not wait (Q-HA-333).
- The level `internal` (2026-11-20, beside his definition): a Bastion's own registration is not internal (Q-AUTHZ-320); the
  Bastion's mark is gone from the subject (Q-AUTHZ-321); the sentence of the UI, "Everyone signed in, service accounts and
  CI jobs included, except external users".
- The colour scale of the severities (2026-11-20, beside his choice Q-UI-860): a band's edge is drawn on every day with at
  least one finding, and the heights stay exact (Q-UI-861); at 365 days the hairline between the columns goes (Q-UI-862).
- The folds of **Set up client** (2026-11-20): a fold's label has at most 32 characters in English and German (Q-UI-810).
- The project `upstream` (2026-11-20, its planner's decisions beside the owner's): Q-REPO-374 to 377 and 380 to 389,
  Q-AUTHZ-300, 301, 303 and 304, Q-API-360 and 361, Q-UI-830 to 836, Q-LIM-62 and 63. Its first builder's, in the
  contract commit (not on `main` yet): a caller without the right gets 403 on the admin API's list and on a single
  registry, not 404, because the keys are the same in every installation and a 404 would hide nothing (Q-API-362); the
  well-known repositories are declared in the order of the format registry (Ansible Galaxy, Debian, npm, NuGet, OCI),
  and the admin page orders its groups itself. Package 2's (not on `main` yet): the 404's sentence with several
  placeholders, none of it on an endpoint with exposure rules, and OCI's own codes kept (Q-REPO-400); a member list
  may not hold `npmjs` and `upstream/npmjs` at once, because Debian's member paths, Ansible's role ids and OCI's merged
  tag list name a member by its key (Q-REPO-401); the usage row names the team's virtual repository only for a
  request a member of `upstream` served (Q-LIM-75). Package 3's (not on `main` yet): the entry's icon in the admin
  area's side bar (Q-UI-838); a row's level as a select with the level's icon beside it (Q-UI-839); what a team
  learns of a registry's state without the admin read (Q-UI-840) and "Listed by" as a number (Q-UI-841), both until
  package 1's two shape changes replace them; which settings of a well-known repository have a field on its settings
  page (Q-UI-842: the vulnerability settings and the cache-fill action stay with the API); the creation form opens
  at the class its address names (Q-UI-843).
- The role `scanner` and the analyser's database (2026-11-20, its planner's decisions beside the owner's): Q-VULN-161 to 176,
  Q-ARCH-527 to 531, Q-OCI-226 to 230, Q-HA-316 to 322, Q-LIM-66, Q-OPS-215, Q-UI-850. The ones to look at first: a listener
  of its own on `packages` for the scanner's pull (Q-ARCH-528); mutual TLS between worker and scanner with the chart's
  certificate, which must allow client authentication (Q-VULN-163; not checked on his installation, section 12 E); the worker
  writes the scanner's package list anew before its own analyser reads it (Q-VULN-164), and a unit test with the pinned
  analyser shows in the job `container-scan` that both documents detect the same (Q-VULN-176: about 10 s in a job that gates
  the release; its fixture database needs a generator and with it a new module, `go.etcd.io/bbolt`, used by that tool and
  the test only); the database's repository never loses the layer in use and lets the older ones go after 48 hours
  (Q-VULN-175: about eight older layers, 1.1 GB, at the 142 MB measured on 2026-11-20); the two system subjects
  `system:image-scanner` and `system:image-database` (R-AUTHZ-002); an uncached layer of a remote, for a reader who may not
  fill the cache, answers 403 `populate_not_permitted`, which the scanner reports as `layer_missing`; an installation that
  wants the database from another registry under another image name sets up a remote in a project of its own and names
  repository and image in the configuration file (R-VULN-044).
- The wiki's visibility behind a fold (2026-11-20): a wiki without a level of its own follows the repository's level up as
  well as down, so raising a repository to Public makes its wiki public (Q-UI-771; before, the wiki kept its level); a
  repository deleted by someone else stays in **Recently opened** until its entry is opened once (Q-UI-769).
- W-14 (2026-11-20): no break-glass event is a success, sign-in, lock and unlock are warnings, and the model has no severity
  (Q-AUTH-423); a skipped group name is an event when the set of skipped names changes (Q-AUTH-422); a backup that completed
  with warnings is a warning (Q-BAK-61).
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
  **Vulnerabilities**.
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
- Branches, deleted only on his word: the origin branches `p7-*`. Local branches whose commits are not on `main` are kept
  without a worktree (18 on 2026-11-20, among them `stab/oom-2`, `stab/access-long`, `stab/stall-2` and two with work that
  never landed: `worktree-agent-a135bce1ea0ac051f`, the clients-first test of the mirror, and
  `worktree-agent-aa6a3feeee93a6b37`, the slow npm publish test of F-105). Nine worktrees are locked by another session of
  2026-11-18 that still runs and were left alone.

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
  a large cost: the three integration stacks (3.0 minutes per push). **Cut first, by the owner's decision of 2026-11-20:** a
  format's leg runs on a push only when its own code changed (built; a push of shared code alone is about 308 runner
  minutes instead of 396, about 508 instead of 939 with 25 formats, computed). The rest is not decided.
- **A builder in a worktree cannot run `make ui-renders`** without replacing the running containers of the UI development
  profile ([web-ui.md](../developer/web-ui.md), found on 2026-11-20): two builders rendered by hand with a dev server of
  their own. Nothing automates it.
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
- **Left by the limits' package of 2026-11-20** (its builder's list): the upstream-request figure of a remote's budget check
  (`MayFetch`) still counts a request it refuses, the decision named the request windows only; an endpoint's own rate class
  runs before the request check, so a request it admits and the check refuses still counts there; the profile editor's help
  text on waiting downloads was not touched; the client cases of Debian, npm and Ansible Galaxy were not run again; every run
  on the kind cluster was on `linux/arm64`. `package_write` (burst 400) would not hold a push of two platforms with more than
  about 65 distinct layers each (Q-LIM-56).
- **Refactor, named by the builders:** the class `package_write` is a stand-in until upload figures exist (Q-LIM-27); a
  screen cannot change a technical token's description or delete one (API only); the usage list's cursor is an offset
  (Q-LIM-45).

### C · The walk (W-12 to W-24) and what landed on 2026-11-20

- **The colour scale of the severities:** rendered in Chromium only, not in Firefox or Safari; the seed holds one day of
  findings, so the trend was judged on fed data; no finding rated None is in the seed, so its badge was not rendered.
- **The level `internal`:** the e2e case of the Bastion (it now expects 200) and the integration areas other than `ci` were
  not run by the builder; the first pipeline after the landing shows them.
- **The first readiness and Valkey:** no real Sentinel failover during a start was run; the 30 s are computed from the
  Sentinel settings and the measured failovers. The nightly shows the cases its builder read (the drain variant that holds a
  primary lost, the outage with a replaced pod, the clone without Sentinels).
- **Set up client (W-27):** the journeys `08-packages` and `09-guides` have new selectors and were not run on a cluster; the
  German page and the Bastion target were not rendered; the renders ran against answers rewritten in the builder's dev
  server, not against the changed backend (its side is unit-tested).
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
  `member_unverified` in the second the replica shut down; no limit in it, first record of it). **Again** in Test and
  Release 44715970992 of b74d0e98 (2026-11-20, `integration (shard c)`, 14.58 s; nothing of that push touches the Debian
  virtual repository): two occurrences in a day, to be read at its cause next, not re-run.
- **F-160** `drain-load-e` on latency alone; not reproduced in two later runs.
- **F-126** `TestConcurrentWritersOnOnePath` refused one writer once; **F-127** `e2e (push)` with a certificate the job's own
  CA did not verify, the secrets operator suspected and its kept log not read; **F-124** tarballs of 1 to 5 MB stalling at the
  edge for about 40 s; **the 502 bursts at the edge (H-795)**, seen again as one upload probe per drain leg on 2026-11-19
  and 20; the stall of the whole cluster in the set `push`; V-7b of the vulnerability scanning performance once above its
  bound, the runner's load not measured.
- **The internal secrets operator of the local kind cluster crash-loops** (hundreds of restarts on 2026-11-10 and 11).

### E · Never run where it matters

- **On the owner's installation, before it takes the releases of 2026-11-20:** whether a CI issuer is public or a mapping
  admits fork pipelines to read (each of them now reads everything `internal`, H-1052); whether the chart's certificate
  allows client authentication, which the role `scanner` will need (Q-VULN-163).
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
