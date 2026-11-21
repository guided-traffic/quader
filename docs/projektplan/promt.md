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
   every open finding of section 8 with an agent each. Open there now (2026-11-20, 21:14): no registry task; a malware scan of every artifact with ClamAV, decided by the owner that day in fourteen answers, specified, its tables and API landed, and in work in three of four packages ([With the owner](#with-the-owner)); the role `scanner`, landed that day with a certificate for each kind of pod, and after it the analyser's database in a repository, in work (section 11); the built-in project `upstream` is landed; the list [With the owner](#with-the-owner) and section 12; then the next unit of item 3. The open items of BETA.md phase 7 are
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

- **The setup of an end-to-end set can run into the write class's rate limit** (2026-11-20, `make e2e SET=vuln` on an idle
  20-core host: `create service account e2e-nuget: 429 rate_limited`, burst 50, then 10 a second). The setup of `vuln`
  now waits out a 429 by its `Retry-After`; the other sets' setups were not looked at, and a fast runner can hit the same.
- **The job `Malware Scan (source code)` takes ClamAV unpinned from the runner's package manager** and the signatures
  from the distribution network on every run (`.github/workflows/release.yml`). Once the product pins a ClamAV image for
  the malware scan of artifacts, that is two versions of one tool: the job then runs the pinned one.
- **Two component tests of the person search wait a fixed 320 ms** (`policy-editor.spec.ts`, "no match "@sam" under
  Principal"; `project-screens.spec.ts`, "Service account of web"): both failed one `make web-test` on 2026-11-20 at a
  host load of 53 and passed when their files ran alone. A fixed wait measures the host; the tests should wait for the
  lookup's answer.
- **F-165 · One instance of the request pooler had no server for 33.7 s after a PostgreSQL switchover** (the full nightly on
  a quiet `main`, 44653040868, `e2e (ha-pooler)`, 2026-11-20 05:15 UTC; `TestHAPooler_Switchover`, R-HA-058, Q-HA-302: the
  bound is `health.database_outage_grace`, 20 s). `quader-db-pooler-rw-…-82mgv` refused 1,248 clients with
  `server_login_retry` from 05:15:07 to 05:15:41; its sibling of the same pool was back after 8.1 s, the two instances of
  the control pooler after 7.3 and 7.8 s. Everything the load asserts held: 16,118 k6 requests with no failure (p99 8.27 s
  over the switchover), 96 `npm ci` and 14 publishes all exit 0, every acknowledged publish installs, no restart, `/readyz`
  200 on every replica. The four pooler faults before it in the same leg were clean (k6 p99 174 to 191 ms). Nothing of the
  download limits is in it. One red run, not re-run: the owner ended the nightly for the day when he saw it. **It
  returned in the nightly of 2026-11-21 (69.4 s on one instance of the control pooler) and was read from both runs'
  logs (02:16); the cause is known, and it is two things.** (1) **The test counted time in which nobody asked.**
  After the switchover every instance fails its first login at the new primary once, because the new primary sets
  the application's password again (Q-HA-302), and PgBouncer tries a server again only when a client arrives after
  `server_login_retry`. Each replica of Quader holds exactly one control connection, and on 2026-11-21 all of them
  had landed on the sibling; the long instance saw one client 0.75 s after its failed login (inside the retry's
  second, so refused from the cached error) and then none for 61 s (its pod at 0.000 cores); the next client was
  refused once more with the old error and the connect it started succeeded 6 ms later. The window from the first
  to the last failure line was 69.4 s; the instance could not serve for 8.6 s. Q-HA-302's reproduction had run
  login loops without a pause and so never saw an idle instance. (2) **On 2026-11-20 a connect hung**: the request
  pooler's long instance started a connect that got no answer and gave up after 15 s, PgBouncer's default
  `server_connect_timeout`, and while an attempt is under way a pool starts no other, so that instance took no new
  login for those seconds; then the same idle minute in small (13.7 s). That one is real: with both instances of a
  pooler hit, every replica would turn unready. Why that connect hung is not known (no packet trace; a connection
  tracked to an address the Service was leaving is the candidate). Ruled out with evidence: a starved runner, the
  primary's memory (561 MiB of 1 GiB; its restart is the demotion). **The fix, one commit held for the end of the
  nightly** (the type `fix`, because the manual, the README and R-HA-058 gain a requirement for an operator's
  pooler): the test ends an instance's window at its last failed server attempt plus its `server_login_retry`,
  read from the rendered configuration, and names later refusals without counting them (Q-HA-370; a package of its
  own with unit tests from lines of both runs; over the eight real logs the windows are 7.3 to 9.1 s, and 21.1 s
  for the instance with the hung connect, which stays a failure as it should); the profile's poolers get
  `server_connect_timeout` 5 (Q-HA-371; worst case of an instance 8.1 s promotion, 5 s hung connect and two retries,
  15.1 s under the bound of 20). **Not verified:** the new setting on a cluster (the operator's webhook allows the
  parameter, read in its source). What remains by design: an idle instance turns its next client away once,
  however late, and Quader retries that answer.
- **F-160 · `drain-load-e` failed its latency condition once, with every functional condition held** (nightly 44595121737 of
  2026-11-19, the code of `ad01d57e`, the last nightly before the download limits). R-HA-059: the k6 p99 from the first
  eviction until two minutes after the drain was 1.48 s over 35,826 requests against a warm-up of 532 ms, the limit being twice
  the warm-up (npm alone 551 ms to 1.698 s, NuGet alone 326 to 737 ms). Held: 449 `npm ci`, 43 publishes, 330 restores and 45
  pushes all exit 0, no SIGKILL, `gc-full` handed over; one upload probe got a 502 once and passed on its second attempt (the
  pattern of H-795). Not reproduced in the two runs after it (warm-up 345 ms, drain 483 ms on 2026-11-20). **Cause
  found on 2026-11-21 from the run's kept artifacts: neighbours on the runner.** No kill of the database (the primary
  peaked at 938 MiB), but 40 s of slow tarballs from 21:12:20 on, spread over the three replicas, while other pods on
  the runner node went from about 2 to 10 to 16 cores at that second, the host ran at 31 of 32 cores (CPU pressure
  about 70 %) and the leg's own pod fell from 23 to 14 to 17 cores. That was before the change of the runners'
  cgroups (F-133). **The nightly of 2026-11-20 (run 44828435420) failed `drain-load-e` for another reason:** the
  development profile's PostgreSQL primary was killed at its 1 GiB, 5 s into the drain (one stall of about 6 s on
  every replica, one 503, p99 4.8 s), the cause of that night's drain per role, which the profile's 3 GiB address.
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
- An opened audit event at 390 px breaks its **Time** over two lines (the layout check's "a time in 2 lines", seen on
  2026-11-20 with a probe; not fixed). **No render opens an audit event**, so its facts never pass the layout check:
  the copy buttons of its two IDs stood 6 px off their line until 2026-11-20. A walk that opens one event closes it.
- **A push to `main` cancels the run of the push before it** (seen on 2026-11-20 at 20:38): what that run had not
  finished is not run again when the next change starts fewer legs. The release of the first run was cut, so the
  rule "no push before the release of the run before" held and still lost six end-to-end legs. Until the workflow
  changes, the development agent looks before a code push at what the run before it has not finished and whether
  the next change starts those jobs again; if it does not, the push waits for them.
- `TestR_STOR_017_ADeployHoldsEveryAncestorOfItsPath` (integration, area `files`) failed once under host load with
  "deadlock detected (SQLSTATE 40P01)" and passed alone (2026-11-20): it deadlocks on purpose and passes only when
  PostgreSQL aborts the deploy, which is retried; under load the test's own DELETE starts waiting later than
  `deadlock_timeout` after the deploy and is the one aborted. A test that depends on which of two transactions the
  deadlock check picks; not fixed.
- The settings answer of the malware scanning counts the unchecked blobs over all items at every read
  (`backlog.unchecked`), costly on a large installation (its builder's note, 2026-11-20; not measured).
- **The check `scanner` on the System page says only its name** (seen on 2026-11-20): Object store and Valkey have a
  sentence that says what a failing check costs, `scanner` has none, so the header says "Degraded" and the row
  "scanner", and a reader cannot tell an analyser that broke from one that was never installed, which is the state
  of every local stack and every render since the scanner role. The specification still names two checks that can be
  degraded; `scanner` is the third. Open: a sentence for it, or the server reporting "not installed" apart from
  "failing".
- On the System page at 1024 and 390 px the header's time wraps when two replicas are lost (the layout check's
  "broken"; seen with an intercepted answer, no render case has that state), and at 390 px the Installation card
  breaks "3rdpartylicenses.txt" and "Answered by" inside the word (the check does not look at links in a `dd`).
  Seen on 2026-11-20, not fixed.
- The development profile's chart values pin the image tag `dev`, so `QUADER_IMAGE` alone does not change what the
  pods of `make dev-up` run; a builder with a cluster of its own has to tag its image `dev` inside its kind nodes
  (seen on 2026-11-20). The case of `TestE12Valkey` with every Sentinel unreachable relies since that day on the
  cluster's CNI enforcing NetworkPolicy, as the cases of the policies do.
- `make lint` ends at once with "parallel golangci-lint is running" while another worktree lints (seen by two
  builders on 2026-11-20); the linter's flag that makes a second run wait would let parallel builders queue.
- `TestNPMCollapse`, the case "100 concurrent misses over two replicas cause one upstream request" (`e2e (npm)`):
  one of the 100 requests was answered 503 ("request 53 answered 503") in the run 44818088308 on 2026-11-20; the
  same leg had passed an hour before on the same product code. Cause not known: which replica answered it and with
  which code of a 503 (the listener's admission, the collapse's wait, the upstream stub) is the first thing to read
  at a recurrence.
- **A PostgreSQL connection of a serving pod costs the primary 6 to 8 MiB** (measured in the nightly of 2026-11-20:
  a `packages` pod with a pool of 20 gave back 120 to 155 MiB at its end). The preflight of R-HA-016 checks the
  connection budget against `max_connections` and says nothing about memory; the chart's README and the manual
  give no figure for sizing the database's memory from the budget, and a rollout runs one pod more per role.
  The development profile was 8 MiB from its limit for at least a day before it was killed.
- Under the nightly's npm load any hold of the database longer than about 3.3 s puts one of the load's tokens over
  the 50 parallel downloads of the built-in profile of a technical token, and its requests get 429 after the
  limit's wait of 10 s (seen in the drain leg on 2026-11-20). A switchover of PostgreSQL holds longer than that;
  the switchover legs were green on 2026-11-19 with the limit enforced, and why was not looked at.
- The nightly's legs record the pods' memory but not the primary's connections by application, the memory of a
  connection or each pool's size, so a growth of the database's memory between two runs cannot be assigned
  (2026-11-20).
- `TestJobs`, the case of 100 coalesced enqueues (integration, area `coordination`), made three executions where
  one or two are allowed, on a loaded machine where enqueues 8 ms apart took 2.6 s; alone it passes (2026-11-21).
  A test whose bound depends on the machine's speed.
- The clients' cases for the download limits (five formats, end to end) wait a fixed three seconds for a new rule
  to reach every replica; a replica that takes longer counts the first document against the instance and the
  archive is admitted where the case expects a 429 (read on 2026-11-21, not seen).
- `TestR_BAS_006` (unit, `internal/bastion/mainclient`) failed once in a full unit run beside an integration build
  and passed alone (2026-11-21); cause not looked at.
- **The area `coordination` fails beside any other run on the stack** (2026-11-21, twice): its tests count the
  granted advisory locks of a key in `pg_locks` without naming the database, so a lease held in another run's
  database counts (`TestLeases`: two holders at once; `TestScheduler`: never exactly one). With the lock that
  does not hold (next entry) that happens whenever two builders work. A builder fixes both since 03:35.
- **The lock of the shared integration stack did not keep two runs apart** (2026-11-21, 02:03): the development
  agent's run of every area took the lock at 01:49 while a builder's three areas ran under the same lock file from
  01:47 to 02:01, each started with `lockf`. Likely cause, not verified: `lockf` without `-k` removes the lock file
  when its command ends, so a process that waited on the old file and one that creates the file anew both hold
  "the" lock. No developer page says which command takes the lock; the builders took it from each other's
  briefs. What two runs on one stack do to each other was not looked at; areas that fail a primary or stop
  Valkey are the ones to suspect when an area fails only beside another run.
- The shared error notice's **Retry** sits 5.5 px off its line in the error state of every list (seen in the renders of the
  page **Upstream registries** on 2026-11-20; the notice is shared code, not that page's).
- **A virtual OCI repository goes on to the next member when one refuses** (found on 2026-11-20 by reading, not
  tested): `isMemberMiss` in `internal/formats/oci/virtual_read.go` counts every `Forbidden` as "this member does not
  have it", so a block of the vulnerability scanning in one member yields the next member's content or a 404, where
  R-VULN-015 says a block ends the resolution. The generic engine ends it.
- **A repository enabled again can lose one of five scan attempts** (the scanner role, not on `main` yet): for about a
  second a replica's grant index still shows it disabled, the scanner's pull is refused and the scan ends
  `layer_missing`; retried after a minute, never read as clean. Written into R-VULN-034 by its builder. A fix is named
  and not built: the scan listener answers a system subject on a disabled repository with a code of its own, and the
  scanner puts that off without an attempt.

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

**State on 2026-11-20**, 21:14, after the landing of the role `scanner` with a certificate for each kind of pod and of the malware scan's tables and API.

- **Landed and released on 2026-11-20** (the last release cut is the one of `781f09d59`, on 2026-11-21 at 04:11, with the image scanning's database in a repository and the two fixes the nightly brought): the measurement of what a format
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
  kit, comes next (section 12 A). `TestR_DEB_025` of section 12 D was green in this run.
- **The pipeline read at 17:00:** Test and Release 44742748588 of `984540a2` (the afternoon's push: the bounded wait
  for Valkey, the level `internal`, the colour scale) released and is red in one job. **A leg that does not apply takes
  no runner:** four legs of formats show as skipped jobs with no start and no runner, and the legs that ran were
  `access-long`, `bastion`, `ci`, `npm`, `push` and `vuln`; the three integration shards ran 36, 37 and 38 minutes.
  **Red: `UI layout checks`,** two of 379 screens, `repository-settings-service-accounts` as `power` and as `admin`: the
  walk selects the section by `hasText: 'Service accounts'` on a container, a match of a substring without regard to
  case, and `730ef6177` put "service accounts" into the sentence of the level Internal, which stands earlier on the
  same page. The check's fault, not the page's. **Measured on the page by the builder of the screens of `upstream`:** the filter
  matched two folds, the inline fold **Wiki visibility** first (a closed fold keeps its body in the page, and the
  wiki's level Internal carries the sentence), and an inline fold's button has no `.toggle`, so the click waited for
  an element that never came. **Fixed and pushed at 17:15** (`cf983131`, a `test:` commit, no release): the walks to
  the sections Service accounts, Visibility and Download limits find a section by its own head; the other selectors
  of `web/e2e` that filter a container by a text were read and cannot take a level's sentence. Verified by the real
  walks at three widths as both users against the page `main` serves (8 passed); the render run itself was not
  started locally, because its setup seeds the stack. **Test and Release 44770761554 of `cf983131`, read at 17:55:
  green in every job,** `UI layout checks` included; a change under `web/e2e` alone ran the legs `push`, `ci` and
  `access-long` and no format's.
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
- **The built-in project `upstream`, landed on 2026-11-20 at 18:40 and released at 18:53** in four commits
  ([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-066 to R-REPO-074, ADR-209 and ADR-211):
  - **Package 1** (migration 00086): the project with its fixed id, one repository per upstream preset of the format
    catalog, brought in line at every migration (created off and Internal, an administrator's address kept, a dropped
    preset retired and later purged), the operations under `/api/v1/upstream-repositories`, every other write to the
    project refused as the product's, the archive keys of Debian and Ubuntu, `upstream` first in `GET /api/v1/projects`,
    `upstream/scanner-trivy` declared and answering no client.
  - **Package 2** (migration 00087): a virtual repository lists `upstream/<key>`; a member that is not switched on
    stays as a placeholder, and the 404 of a name only it would have served says why in each format's error shape; the
    usage row names the team's virtual repository; the grant index names a member of `upstream` in its own project, so
    a token scoped to the virtual repository reads through it.
  - **Package 3:** the page **Admin area › Upstream registries** with the new switch, a registry's own page (address,
    account, who lists it by name), the member picker's group and the placeholder, the built-in project first in every
    list of projects with an arrow in its mark and no level of its own, the settings and **Set up client** of a
    well-known repository.
  - **A fix of its own, found on the way:** a list's search field lost the focus 250 ms after the last key, on every
    list with the shared head (of 2026-11-18). Seen in a browser on the Repositories list of `main`: typed "np",
    paused, the next key went nowhere, the focus stood on the page's title. `FocusOnNavigation` now leaves the focus
    alone when a navigation changes only the query or the fragment, which also keeps it on a tab that writes `?tab=`.
    Shown by a test of the service that fails on the old one; not looked at in a browser after the change.
  **What the API answers that the first cut did not have,** both found by the screens' builder and built by package
  1's: who lists a registry, as the number of virtual repositories and projects and, on every answer but the list, up
  to 100 names (Q-API-363); and `GET /api/v1/formats/{format}/upstream-repositories`, the state and level of a format's
  well-known repositories for whoever holds `repositories:write-config` or is the Owner of some project (Q-API-364).
  Checked on a backend of the merged tree with four users: everyone who can edit a member list is admitted to that
  read, a reader and an auditor are not. **Gates on the landed tree:** by the development agent the unit run (84 s),
  `generate-check`, `cyclo`, `gosec`, lint, the chart tests (95 s), the coverage tool's links and rules, the scans; by
  the screens' builder on the same tree the web gates (`make web-test` 28.6 s, 1,779 tests); by package 1's builder on
  the merged backend the integration areas `upstream`, `repository`, `virtual`, `limits`, `projects`, `authz`.
  **Looked at (rule 11):** 246 pictures against a backend built from the merged tree with a database of its own (four
  roles, both schemes, three widths), and eight in German. Seven differences between the builder's fixtures and the
  real API were followed; two were faults: a registry's page crashed when the read of the repository's document
  failed, and at 390 px the explanation's table broke `upstream/docker-hub` at its hyphen. **What it costs:** shard b
  about 13 s; about 6 s of component tests; two lines in the render run, about 14 s of test time in `UI layout
  checks`; no leg of a format gains anything. **Not verified:** the job `UI layout checks` itself with the new lines
  and the changed seed (the pipeline of the landing shows it); the format read with a token instead of a session; the
  refusal of an old session at an address write; the 409 of the service-account and CI-mapping writers over HTTP; the
  saved upstream test of a well-known repository (by the code it needs `repositories:write-config`); a real client
  printing the 404's sentence; the upgrade test (read: the admin list sorts by project key, and `e2e` sorts before
  `upstream`). **A well-known repository starts off and Internal, the owner's word of 18:05** ("upstream projekte sollen vom Standard
  aber nicht aus und privat sein, sondern aus und internal", after the development agent told him they were created off
  and Private): folded into the four commits by package 1's builder before the landing. `upstream/scanner-trivy` stays
  Private. It replaces the development agent's Q-REPO-388. **What the change showed:** the grant index does not know
  "disabled", so with Internal presets every signed-in internal user got the twelve switched-off repositories in the
  list of repositories and in the search's suggestions, while the project stayed hidden; a repository of `upstream` is
  now listed and suggested to a caller without `repositories:read-config` only while it serves (Q-AUTHZ-306, the
  development agent's, shown by the case that failed with all twelve). **Where an installation restricts the level
  Internal,** the reconcile takes the highest level at or below it that a caller who is no administrator may choose,
  so Private, and never fails `quader migrate` on a restriction (Q-REPO-393, the development agent's: created Internal
  there, a repository administrator's switch would open a level reserved for security administrators). The manual and
  `docs/security/visibility.md` say at the switch what it opens: everyone signed in who is not external, pipelines and
  service accounts included, directly and through every virtual repository that lists it (H-1052). **On the owner's
  installation:** the release creates the 13 repositories, twelve off and Internal and `scanner-trivy` Private; it
  holds no project yet (read on the cluster), so no key collides. **Read on the cluster at 19:20:** the installation took the release (its HelmRelease is ready on the new chart), `quader migrate` applied the two migrations and created the 13 repositories; twelve are Internal and were never switched on, `scanner-trivy` is Private. **Seen and left for the walkthrough:** the groups stand in
  the order of the format catalog, Ansible Galaxy first and OCI last; the project switcher lists Upstream under **Your
  projects**; in German the side-bar entry "Berechtigungsrichtlinien" reaches the side bar's edge (not this feature's).
- **The role `scanner` and the analyser's database in a repository, specified on `main`** on 2026-11-20
  ([vulnerability-scanning.md](../spec/vulnerability-scanning.md) R-VULN-041 to R-VULN-046, ADR-212 to ADR-214, the gaps
  H-1035 to H-1039 and H-1041 to H-1044), reconciled with the project `upstream` by its planner: the database's image is
  `aquasec/trivy-db` at `mirror.gcr.io`, the cache cleanup never removes the layer in use, the two system subjects stand in
  [authorization.md](../spec/authorization.md), the endpoint `scan` and its listener in
  [endpoints.md](../spec/endpoints.md); the specification counts 1,013 requirements. **Built in two work packages:** (A) the
  scanner role with its token, its listener, its chart and its tests (no migration, nothing of `upstream`); (B) the
  database in a repository (a migration, the task that fetches it, the sources screen; the number 00088 it was
  planned with went to the malware scan's first package on 2026-11-20, so it takes the next free one at its start), which needs
  `upstream/scanner-trivy` of package 1. **Order of landing:** migration numbers run without a gap
  (`internal/meta/postgres/steps.go`), so package 1, then 2, then B; a package that lands out of this order takes the next
  free number. **Checked by the development agent of what the planner could not verify** (2026-11-20): Docker Hub serves
  `aquasec/trivy-db` publicly; `mirror.gcr.io/aquasec/trivy-db:2` answers an OCI manifest with one layer of 141,735,981
  bytes, where the plan had assumed about 60 MB (corrected in Q-VULN-175: the 48 hours keep about 1.1 GB of older layers).
  Not checked: what the module download of the fidelity test adds to the job `container-scan`; the certificate of the
  owner's installation (section 12 E).
- **The pipeline of the landing** (run 44785358679): the release was cut at 18:53; **one job red, `UI layout
  checks`**, with one of 381 cases and a defect of the product, not of the check: on the settings of the Debian
  remote `platform/debian` the copy button beside each fingerprint of an upstream key stands 3.5 px above the
  fingerprint's line, at all three widths. The component (`web/src/app/features/repositories/deb-upstream-keys.ts`) is
  old and never had data in the renders; since package 1 the Debian preset carries the archive's keys, so the seeded
  remote shows nine fingerprints for the first time. The run ended at 19:22 with that one job red; every other job is green, the three integration shards and
  `e2e (vuln)` among them. **Fixed and landed at 19:27** by the builder of the `upstream` screens: the row is a flex
  row with the button on the fingerprint's first line by its margins (the convention the digest facts use), so a
  row is as high as its text and a fingerprint that wraps at 390 px keeps the button on its first line; the list
  had no style of its own before (the parent's `.plain` does not reach into the component). **The same defect in
  one more place, fixed with it:** the **Event ID** and the **Trace ID** of an opened audit event, whose button
  stood 6 px below the line; no render opens an audit event, so the check never saw it (section 8). Looked at by
  the development agent: the list in both schemes at 1440 and 390 px, the audit event's facts at 1440 px. Gates of
  the builder: the component tests (1,779 in 28 s), the UI lint, the layout check's own tests, and the render cases
  `repository-settings-deb-remote`, `repository-settings-deb` and `admin-upstream-registry` on a stack of its own,
  without a finding. No test added: the render's layout check is the test of record. **Its pipeline** (run 44792888153) ended green at
  20:09, the job `UI layout checks` among its jobs, and its release was cut at 19:42.
- **What the scanner's landing broke, found at 20:22 by the restart of the UI development backend, fixed and landed
  at 20:34:** `make ui-dev-backend` ended at start with "scan listener: server.internal_tls.ca_file is required for the
  links between quader's pods while server.tls serves TLS (R-ARCH-038)". The links of the image scanning need their trust
  anchor wherever `server.tls` serves TLS, the UI development profile serves TLS with a CA of its own and named none for
  the links, and the builder of the landing had not started that stack (it had named that as not verified). The same
  start is the first step of `make ui-renders`, so the job `UI layout checks` of the landing's pipeline (run
  44801198840) went red at 20:26 with that message, and the integration test `TestListenerRotation` (area `tls`), which
  starts the role `all` with TLS, fails the same way. **The owner's stack** was down for two minutes and ran from
  20:22 with the CA named through the environment, as a stopgap of the development agent. **The fix** (one commit, by
  a builder of its own): the profile's configuration names its CA for the links (its one certificate lists
  `127.0.0.1` with server and client authentication, which is what both loopback links of the role `all` present and
  verify; no names of kinds there, Q-ARCH-553); the configuration is written by `deploy/ui-dev/config.sh`, which
  `up.sh` and `serve.sh` both run, so `make ui-dev-backend` brings a change of the profile's configuration to an
  existing `.dev/ui-dev` without `make ui-dev-up` (it creates no certificate, secret or container, and names a
  missing file); the integration test names the listener's CA. Looked for the same failure and found none: the
  other starts of `serve` in tests and profiles set no TLS or use the chart. Gates of the builder: build, the full
  unit run, lint with the integration build tag, links, the white-label scan, `shellcheck`; the writer run on a copy
  of the state (the only difference to the file in use is the new block), a handshake of both links with the
  profile's certificate, and a start of the role `all` in process that failed without the CA and answered ready
  with it. **Verified by the development agent before the push** (20:36): the owner's backend restarted from the main
  checkout without the environment variable answers ready, its `quader.yaml` carries the new block, the UI and the
  sign-in provider answer. **Not verified at the landing:** the path of a fresh state in the job `UI layout checks`;
  `TestListenerRotation` itself. **The rule taken from it:** after the local merge and before the push the
  development agent restarts the UI development backend from the main checkout; a start that fails is fixed before
  anything is pushed. **A consequence the developer page now states:** on these loopback links any certificate of the profile's
  CA is admitted, the ones of its PostgreSQL and object store among them.
- **The pipelines of the evening.** The scanner landing's run (44801198840) cut its release at 20:34 and was
  **cancelled at 20:38 by the push of the fix** for the UI development backend: a push to `main` cancels the run
  before it, so its three integration shards and its six end-to-end legs (`npm`, `push`, `oci`, `access-long`, `vuln`,
  `bastion`) ended without a result; red in it was `UI layout checks` alone, with the start failure above. The fix's
  run (44803754281) cut its release at 20:51 and, its change being scripts, one test and documentation, started one
  end-to-end leg, `push`, **which failed at 21:04 on `TestE12Valkey`**: both cases in which a new replica with a wrong
  primary name or a wrong password must stay not ready saw `no_sentinel` in three of three attempts. **Cause, found by
  reading:** the scanner landing made the development profile's NetworkPolicies for Valkey name its clients by their
  component (`api`, `packages`, `worker`, `e2e-driver`), because scanner pods carry the serve label too and are no
  client of Valkey (Q-HA-344); the extra replicas that test starts carry the component `e2e-replica`, which the list
  does not hold, so they reach no Sentinel. A change of the landing, so fixed at once by a builder of its own.
  **Confirmed and fixed** (reported at 21:44, one commit of the type `test`, landed with the System page's fix):
  reproduced on a kind cluster of its own with the old list, three attempts of 49 s each ending in `no_sentinel`;
  the profile has one policy of its own for both tiers (`quader-valkey-clients`, ports 16379 and 36379), and its
  list now names `e2e-replica`. The fix is in the policy and not in the test's label, because the extra replica is a
  copy of `packages` that holds the Valkey credential, and the label `packages` would put it under the Services and
  the disruption budgets. **No test had shown that the scanner is cut off from Valkey:** the third case of that test
  (every Sentinel unreachable) now starts its replica with the scanner's component label instead of a loopback
  Sentinel address, so the policy itself makes the Sentinels unreachable; with `scanner` put on the list as a control
  the case failed as it should. No pod and no case more; the whole test took 150.8 s against 147.7 s measured before.
  On that cluster: `TestE12Valkey` passed at its first run, the set `push` passed in 730 s. **Every other pod the
  suite and the profile create, read against the list:** the pools carry the component `worker`, the driver is on
  the list, the restore namespace runs only the release's three kinds, the Jobs and the clients' pods hold no Valkey
  credential, the Bastion has a policy of its own; nothing else is cut off. **Not run, read only, until the nightly:**
  the sets `drain`, `backup`, `bastion`, `alerts`, `upgrade`. **Beside the older finding, not a return of it:** the
  open item of section 12 about the first readiness and Valkey is a matter of timing on a starved runner, rare and
  mixed in its reasons; tonight's was the network dropping the traffic, three of three; the retry built for the old
  one is why it showed as three attempts that could not tell, and not as a failure to reach. **Not verified in CI of the
  scanner landing because of the cancelled run:** the legs `npm`, `oci`, `access-long`, `vuln` and `bastion` and the
  three integration shards on that commit; its builder ran `e2e (vuln)` twice on a cluster of its own and five
  integration areas; the nightly of tonight runs every leg. **The run of the malware scan's first stage
  (44809593635) has no release:** its job `web (Angular)`, which gates the release, went red at 21:20 on
  `audit-labels.spec.ts`: the eight `malware.*` event types the stage registered have no name in the UI, and a
  component test holds every event type of the audit registry to a name of its own. The stage's builder ran no UI
  test and neither did the development agent before the push, though the commit touches `web/` (the generated API
  types and the registry's fixture). **The rule taken from it:** a commit that touches anything under `web/`, a
  generated file included, gets `make web-test` and `make web-lint` before the push, whoever built it. The names
  are being added by the builder of the System page's fix (below) and land with it; the stage is released by that
  push. The fix run before it (44803754281) was cancelled by the stage's push with integration shard a green and
  the shards b and c without a result. **The stage's run ended at 21:58 and was let run to its end before the next
  push:** green `e2e (npm)`, `e2e (access-long)`, `e2e (vuln)`, `e2e (bastion)`, `e2e (ci)` and the integration shards
  a and c, which is the first result of CI for the scanner role's legs; red `web (Angular)` (the names, above), `UI
  layout checks` (the System page's cell alone, 380 of 381 passing), `e2e (push)` (the Valkey policy) and
  **`integration (shard b)`: a fourth miss of the scanner landing.** `TestEndpoints` (the case that a worker opens no
  endpoint listener) and `TestReplicas` of the area `server` start a worker process from a configuration of their
  own, and since the landing a worker is refused at start without `vulnerabilities.images.scanner.url` while the image
  scanning is on, which is the default (R-VULN-041; the chart sets it). The rule stays; a builder gives the tests
  what a real worker has and goes through every other start of a single role without the chart (since 21:59). The
  landing's builder had run five integration areas, not `server`. **What the four misses have in common** (the local
  UI profile, the TLS integration test, the Valkey policy, this one): each is a start or a pod outside the chart that
  the landing's own tests did not exercise; a landing that adds a rule to the configuration or to a policy runs every
  integration area and starts the local UI profile before it lands, which costs minutes against an evening of fixes.
  **The fourth fix, reported at 22:10 and pushed with the other three:** the area `server` builds its processes'
  environment itself and not through the shared harness, which the landing had given the address; one builder of the
  environment for both ways the area starts a process now names for a worker a scanner's address nothing listens on,
  as the shared harness does. Not for the role `all`, which would stop calling its own scanner; and not by switching
  the image scanning off, because a worker with it on is what the chart runs. A worker whose scanner cannot be
  reached starts and is ready; a due scan is put off for a minute with `scanner_unavailable`. **A second defect of
  the same test, older than the landing:** under the race detector, which the developer page's command for the area
  uses and CI's does not, a subtest read a process's state while another goroutine wrote it; it learns of the exit
  through a channel now. The area passed with the race detector in 52 s. **Every other start of a single role
  without the chart, read against the landing's rules:** the shared harness and the command's tests have the address
  since the landing; the TLS stack sets its CA, its names and the scan clients; the area `tls` starts the role `all`;
  the overload area's process opens no scan listener; the configuration loads of migrate, admin and backup name no
  role; the profiles go through the chart. Nothing else fails. `process.scanner_kind` is specified and not in the
  configuration yet. **The run of the four fixes (44818088308, pushed at 22:11) showed each of them:** `web
  (Angular)` green and the release cut at 22:26 with the malware scan's first stage in it; `UI layout checks` green
  with all 381 cases, the first time since the afternoon; `e2e (push)` green; the three integration shards green,
  shard b with the area `server` in CI's own form. **One red job in it, not one of the four and not seen as caused
  by the change:** `e2e (npm)`, `TestNPMCollapse`, the case that 100 concurrent misses over two replicas cause one
  upstream request: "request 53 answered 503" (section 8). The leg had passed an hour earlier on the commit before,
  whose product code for that path is the same; the push changed UI files, the development profile's Valkey list and
  two tests. Re-run once at 22:57 (the first of at most three): **green at 23:21**, so the run stands green and `main`
  is green on every job for the first time since the scanner's landing; the 503 stays a finding without a cause.
- **The nightly, dispatched at 23:22 on the quiet `main`** (run 44828435420, all legs, the commit `6e01d26ff`): the
  scanner role with every format's client and in the long sets that only read against the Valkey list so far
  (`drain`, `backup`, `bastion`, `alerts`, `upgrade`), F-165, the Valkey start cases, `TestR_DEB_025`. Its longest lane
  takes about four hours. **No code push to `main` beside its kind legs** (the standing rule of the night): package B
  of the scanner, if it reports before the nightly ends, is checked and held on its branch until then; the malware
  scan lands from its branch after it in any case. **Its first red legs, at 23:26, a fifth miss of the scanner's
  landing:** the four jobs `Platform matrix` (the oldest and the newest supported Kubernetes with Helm 3 and Helm 4)
  failed at their first value set: the chart's server-side dry run on a plain kind cluster renders the four
  cert-manager Certificates of the kinds of pod, and those clusters have no cert-manager CRDs ("no matches for kind
  Certificate in version cert-manager.io/v1"). The landing changed the chart's CI value sets from a named Secret to
  an issuer and ran the chart tests and the development profile, which installs cert-manager, not the matrix. A
  builder decides what the matrix tries (the standard path with the CRDs installed, the Secrets per kind once),
  goes through every value set and every other install of the chart outside the development profile, the set
  `upgrade-release` among them, and runs two cells on clusters of its own (since 23:27); its commit lands after
  the nightly, and the matrix is dispatched alone then. **Reported at 23:39, held on its branch** (one commit of
  the type `test`; the chart did not change): reproduced on a fresh kind cluster with tonight's exact error; of all
  six value sets the Certificate is the only kind a plain cluster does not know. The matrix now tries the standard
  path on every cell, as Q-HA-347 makes the issuer the plain way: each leg first dry-runs the one set that names a
  Secret per kind on the bare cluster (the path without cert-manager needs no CRD), then applies the CRDs alone of
  the pinned cert-manager chart (the development profile's pin; no controller and no webhook are needed for a dry
  run or for an install whose pods never start), runs the six sets, and after the real install reads the issuer of
  each kind's Certificate. About 2 s more a leg. All four cells passed on clusters of its own in 32 to 41 s each.
  Recorded in the developer page of the matrix, under Q-HA-274 (whose line "none needs a CRD" was no longer true)
  and in the prerequisites of R-HA-049, where the landing had left cert-manager out. **Every other install of the
  chart outside the matrix, read:** the job `chart` and the chart tests render offline; every end-to-end leg,
  the restore namespace and the upgrade tests use the development profile, which installs cert-manager; nothing
  else fails. **The set `upgrade-release`** can pass again since the release of 22:26, whose own values already name
  the issuer; between the landing and that release it would have waited 15 minutes for a Secret nobody issues
  (read, not run). **Not verified until the dispatch:** the pull of the cert-manager chart on a hosted runner, the
  legs' times there. **The second red leg, at 23:48, cause not known:** `e2e (drain-roles)`, `TestDrainRoles`. Every
  role was evicted pod by pod under the npm load; the ping load, the transfers, every npm command and the driver's
  720 changes held without a failure. But for some seconds around 22:38:56 UTC, during the evictions, 168 of 88,360
  requests of the load, all tarballs through the virtual repository `npm-all`, took 17 to 21 seconds and were then
  answered 429, on two `packages` replicas (one being evicted, one a replacement), and the p99 of all npm requests in
  the window was 14 s against 197 ms in the warm-up. The morning's scheduled nightly was green on the commit before
  today's landings. Not known: which layer answered 429 after such a wait (a download limit or a request burst and
  their fallback to shares per replica, the listener's admission, the virtual engine, the test's upstream), whether
  the slow window and the refusals are one cause, and whether a change of the day did it (the scanner role with two
  more pods and a drain script that rolls them, the second usage dimension of `upstream`, the join of the malware
  verdict into the download statement) or a starved runner. A builder reads the leg's artifacts and the code for it
  (since 23:50), with one local reproduction allowed and no repetition loop; a fix if it is a defect, a proposal if
  the limit works as decided and the test's one caller is what it refuses. **Cause found from the leg's artifacts
  (00:08), one cause for both failures, not a change of the day and not a runner:** the development profile's
  PostgreSQL primary ran into its memory limit of 1 GiB and was killed (`OOMKilled`, exit 137, at 22:38:50 UTC; 22
  kills, all in that pod). It had stood at 1,003 to 1,022 MiB for three minutes; the drain of `packages` then ran a
  fourth `packages` pod beside the three for 20 seconds (the evicted one holds its connections while its replacement
  is ready), and a `packages` pod's pool costs the primary 120 to 155 MiB (measured at the pods' ends). PostgreSQL
  took connections again after 2 s, but the Service of the primary had no endpoint until the startup probe's next
  attempt 10 s later, and commits waited for the synchronous standby until 22:39:06: about 16 s in which every
  request that reads the database stood, on every replica (the ping that reads no database stayed under 40 ms).
  **The 429s are the download limit as decided:** all 168 carry the limiter `limits.parallelDownloads` in the mode
  `cluster` (Valkey answered) under the built-in rule of a technical token; the load's three tokens send 15 tarballs
  a second each and do not slow down when answers stop, so about 200 downloads of a token were in flight against the
  50 the built-in profile admits; a request waited for the database, then the 10 s of the limit's wait, then got
  its 429. They came from all four live `packages` replicas; the test's output had named the first few. The p99 is
  the stall itself. **Corrected:** the morning's scheduled nightly was no comparison, it had skipped its legs after
  the dispatch of the night before; the last real run of this leg (2026-11-19) was green with the primary at 1,016
  MiB at the same step, 8 MiB under the limit. Tonight's private memory was 39 MiB higher; whether a commit of the
  753 changed files since then or the pools' growth from run to run did that is **not known**, and the run does not
  record what would tell (the primary's connections by application, the memory of a connection, each pool's size).
  Ruled out with evidence: a starved runner (15 to 19 of 32 CPUs in use before, 2 to 5 during), the scanner (it
  holds no database credential), the limits' fallback, a change of the limit. **The fix, one commit of the type
  `test`, held for the end of the nightly:** the profile's PostgreSQL may use 3 GiB, sized for the connection budget
  the server admits (323 connections at about 7.75 MiB and the shared buffers, 2.6 GiB); a limit reserves nothing.
  No limit of the product was touched. **Not proven until a run uses it.** No local reproduction: the machine's
  load was 30 on 20 CPUs and the evidence settled the cause. **The same kill failed a second leg, `e2e (drain-load-e)`,
  at 03:37** (the primary at 948 MiB before the drain, killed 5 s into it, one stall of 6 s, one request answered 503,
  p99 4.8 s against 660 ms; the runner ruled out: 18.6 of 32 cores, nearly all the leg's own); it is not the older
  finding F-160, whose own run shows neighbours on the runner (section 8). **The third red leg, at 01:18, cause not known yet:**
  `e2e (ansible)`, the case of `TestAnsibleLimits` for `cachedBytesPerHour`: the install that should cross the hour's
  limit and be served was itself refused with 429 ("Failed to download collection tar … HTTP Error 429"), where the
  case expects the refusal at the next install. The set runs only on a change of Ansible's own code and in the
  nightly, so it had not met the landings of 2026-11-20. Suspected, not verified: the background download that a
  probe now starts in a remote repository charges the fill against the caller's `cachedBytesPerHour` before the
  client asks for the archive, or the second usage dimension of `upstream` counts a row twice. A builder reads the
  leg's artifacts and the two specifications for it (since 01:19). If the two features meet in a case neither page
  decides (who pays for the bytes of a background download a probe started), that goes to the owner as a decision
  with what every format does today, and is not picked in code. **Cause found (01:33): a race in the test, no
  defect of the product, and both suspicions were wrong.** The case's repository is a local one, so no probe starts
  a download in it; and the second usage dimension is written to the usage store alone, never to the counters that
  enforce a limit. What happened: the rule allows one cached byte an hour; the first install sent four documents
  (2,411 bytes) and asked for the archive 20 to 60 ms later; each replica adds its bytes to the shared counter at
  its own flush once a second, and the archive's check reads that counter; when one of the three replicas flushed
  inside those milliseconds, the hour was used up and the archive was refused, as R-LIM-017, R-LIM-018 and R-LIM-021
  decide. The case expected the other outcome of the race; by the builder's estimate from the timings it loses one
  run in eight to ten, and the leg had passed eight times on 2026-11-20. The cases of npm, NuGet, OCI and Debian
  read a document, wait two seconds and then run the client, which R-LIM-039 describes; Ansible's now does the same
  (one commit of the type `test`, one install fewer, held for the end of the nightly; compiled and linted, not run
  on a cluster yet). **The question of who pays for a background download a probe starts is decided already**
  (R-LIM-037, Q-LIM-54: the client that probed, as uncached bytes and one upstream request; nothing of
  `cachedBytesPerHour`), so nothing goes to the owner. The other formats' limits cases passed tonight in `oci`,
  `deb` and `nuget`. Left as it is, for every format: the cases wait a fixed three seconds for a new rule to reach
  every replica (section 8). **The fourth red leg, at 01:28, cause not known
  yet:** `e2e (ha-faults)`, `TestHAFaults_ValkeySlow` (Valkey answers every command up to 2 s late for three minutes
  under the npm load). Every npm command exited 0 and `/readyz` stayed 200; but 176 of 37,217 requests of the load,
  all tarballs, were answered 429 after almost exactly 10 s, and the p99 while Valkey was slow was 4.07 s against the
  bound of R-HA-057 (the baseline of 235 ms plus twice `valkey.command_timeout`, 1.235 s). Ten seconds is the wait
  of the limit of parallel downloads, the limiter of the drain leg's refusals; the hold here is Valkey and not
  PostgreSQL. The case failed in the nightlies of 2026-11-11 and 2026-11-18 on the runners' starvation, without a
  429. Not known: which mode answered (the cluster's counter on a slow Valkey or a replica's share after the
  circuit opened), how many round trips to Valkey a download makes for limits and usage since the download limits
  landed, whether those go through the circuit, and what the leg did in the last real run. The builder of the
  drain leg's diagnosis reads it (since 01:30); a decided defect is fixed, an undecided case (may a download wait
  on a slot's bookkeeping in Valkey at all) comes back as a decision, and no limit is raised for a test. **Cause
  found (01:52): a defect of the product in the download limits, latent since they landed on 2026-11-19, and the
  specification decides the answer.** A download holds a slot against the limit of parallel downloads; while Valkey
  cannot be used a replica counts the slots in its own memory against its share. The code gave a slot back only in
  the place the next flush happened to reach. With Valkey slow, a replica's calls go to Valkey in one second and to
  its own count in the next, so a slot was taken in one place and given back in the other, and a finished download
  stayed counted for the slot's full minute; a call that timed out was decided locally while a Valkey that only held
  the command ran it afterwards, so that slot lived in both. Callers far below their limit were refused, which
  R-LIM-019 and R-HA-057 forbid (a figure falls back to the replica's share and never refuses because Valkey
  failed). **Evidence:** all 176 refusals came from one replica in one minute, each after the 10 s of the limit's
  wait; of the 35 whose log line survived, 34 name the limiter of parallel downloads in the mode `fallback`; that
  replica's share is 17 slots a token while only a few downloads a token were in progress; the bad window lasted 56
  s, the slot's lifetime; without that replica's minute the p99 is 554 ms, inside the bound. A download makes two
  round trips to Valkey for the limits (three at a cache miss), the second since the download limits; the usage
  rows and the second dimension of `upstream` cost none. The limits go through the same circuit as every other use
  of Valkey. Ruled out: a starved runner (12.5 of 32 CPUs in use, no kill). The last real run of the leg before
  tonight passed with the limit enforced. **The fix, one commit of the type `fix`, held for the end of the
  nightly:** a slot is given back in both places, locally at once and in Valkey by the flush, retried until it
  arrives or has expired there; renewals too; no round trip more. Five unit cases against a store that is down or
  that runs a batch and then answers with a timeout; four of them fail on the old code. The decision is Q-LIM-95,
  the development agent's. Gates: build, the full unit run, lint, generate-check, links; the areas `limits` (which logs two round trips of
  the limits around a cached download, as before the fix), `coordination` and `npm` passed, once each. **Not known:** why that one replica and that one minute (the circuit's state
  over time and the other replicas' logs before the fault are not kept); whether the leg passes with the fix, which
  the next nightly shows. **The fifth red leg, at 01:51, a return of F-165:** `e2e (ha-pooler)`,
  `TestHAPooler_Switchover`: after a switchover of PostgreSQL one instance of the control pooler logged refused
  clients (`server_login_retry`) for 69.4 s, against the bound of 20 s; its sibling was back after 7.6 s, the two of
  the request pooler after 8 and 9.1 s; the load held (16,733 requests, no failure, every npm command exit 0). The
  first time, on 2026-11-20, it was one instance of the request pooler for 33.7 s. The builder of the Ansible leg's
  diagnosis reads that instance's log and the profile's pooler configuration (since 01:52): whether it had no
  server for 69 s or was only asked rarely, with a last failure line that is late (60 refused clients in 69 s is
  sparse), which would make the test's reading of the log the defect. **Read, and it is so; with a second, real
  component in the first occurrence:** the entry F-165 in section 8.
- **The state cell of a replica on Admin area › System and the names of the malware events in the audit log, landed
  on 2026-11-20 in the evening's fourth code push**, held until the legs of the run before it had ended: the fix's run showed, with the backend
    starting again, one layout finding of 381 cases: a replica that is ready with a failing check that does not gate
    shows "Ready" and a warning with the check's name in its state cell, and the cell's first line stands 20 px below
    the replica's name. Since the scanner landing the role `all` reports the check `scanner` that way wherever the
    analyser is not installed (Q-ARCH-551), which is every render and the owner's UI stack; the cell never had a
    warning in a render before. **Reported at 21:28, not landed yet:** the layout check measures a cell by its first
    line of text outside a badge, "Ready" is a badge, so it measured the check's name on the line below; the cell now
    has the state and beside it the failing checks one under the other, the first on the line of the replica's name,
    and in the stacked table the state starts on the line of its column's name; "Failing checks:database" got its
    space. Looked at by the development agent: 1440 px light and 390 px dark with rows of one, two and three failing
    checks, an unready, a lost and a draining replica and two pools. One new component test of about 50 ms. **The names of the eight
    malware events** (English and German, by the terms the vulnerability scanning's German texts use: Befund, Prüfung,
    gesperrt, Einstellung; a release is "zum Download zulassen", because the glossary keeps "freigeben" for exposure:
    a word for the owner to confirm at the walkthrough) and **the pool headings' missing space** ("Poolbatch") came
    with it in two commits. Gates: the component tests (1,780) and the UI lint by the builder and again by the
    development agent on the rebased commits, the layout check's own tests, the render case `admin-system` on a stack
    of the builder's own without a finding. The longest English name measures 340 px against the 342 px an action
    has at 390 px (computed from the gutter and the cell's padding, not measured on a rendered page); the German
    names wrap to two lines without breaking a word.
- **The nightly of 2026-11-20 (run 44828435420), read to its end:** 26 legs and the latency job green, nine legs
  red, every one explained. The four of the platform matrix: cert-manager's CRDs on a plain cluster (the scanner's
  landing; a commit of the type `test`). `drain-roles` and `drain-load-e`: the development profile's PostgreSQL
  primary killed at its limit of 1 GiB (the profile, `test`). `ansible`: a race in the test (`test`). `ha-faults`: a
  slot of the parallel-download limit not given back while Valkey is slow, a defect of the product since 2026-11-19
  (`fix`). `ha-pooler`: F-165, a window the test counted while nobody asked, and a connect that hangs for 15 s
  without a timeout (`fix`). Green for the first time with the scanner role: the sets of every format but Ansible's,
  `failover`, `npm-ha`, `ha-suite`, `nuget-ha`, the three other drain variants, `cold-crash`, `backup`, `bastion`,
  `alerts`, `upgrade`, `edge`, `vuln` with its long and pooler forms, `oci-long`, `ci-ha`, the soak. **The six commits
  land in one push with package B of the scanner at the nightly's end** (the branch `fixes/nightly`, gates of the
  development agent on it, each exit 0: build, the full unit run, generate-check, chart tests, lint, the UI tests and
  lint, links and check, the tests' packages with their build tags); the legs they fix are dispatched alone after it.
- **Landed on 2026-11-21 at 03:56 and released at 04:11, in one push after the nightly's end:** the six commits of
  the branch `fixes/nightly` (above): package B of the scanner (the image scanning keeps its database in
  `upstream/scanner-trivy`, with the removal of older layers), the fix of the limits' slots, the fix of the poolers'
  connect timeout, and the three commits of the type `test`. Before the push the owner's UI backend was started
  with them from the main checkout (ready, migration 00089 applied, the scheduled fetch of the image database off in
  the UI profile). Its pipeline (run 44858533514) also starts the Ansible leg; the legs the fixes are for are
  dispatched alone when it has ended.
- **The malware scan on its landing branch, 04:17:** `feat/malware` is `main` with the stack's lock fix and the eleven
  commits of the feature on top (rebased by package 3's builder onto what landed, each conflict settled in the commit
  it arose in; one break without a textual conflict found by the area `malware` and fixed: a test set a configuration
  key package B removed). The page of the upstream registries shows both system repositories in use, looked at by
  the development agent at 1440 px dark and 390 px light. **Gates of the development agent on the tip, each exit 0:**
  build, the full unit run (176 packages), generate-check, chart lint and tests, links and check, the UI tests
  (1,803), the UI lint and layout tests, lint, cyclo, gosec; the commits and the diff scanned for names that may not
  appear. **Running since 04:10:** every integration area once, through the new lock script. It lands when that run
  is green and the pipeline on `main` has ended.
- **The stack's lock and the coordination tests, fixed (one commit of the type `test`, first on the landing branch):**
  `test/integration/locked.sh` runs a command under the stack's lock from the main checkout or a worktree, loads the
  stack's environment, never removes the lock file, waits without a timeout and holds the lock as long as the test
  binaries live; the developer page names the commands. **What `lockf` had really done, shown on scratch files, and it
  corrects the development agent's reading:** without `-k` it excludes; a holder without `-k` and a waiter with `-k`
  let a third in, and the development agent's own run of 01:48 used `-k` beside builders who did not, and its later
  word to the builders to use `-k` spread the mix; a `lockf` that is killed frees the lock while its command runs on.
  The integration tests' queries of `pg_locks` and `pg_stat_activity` look at their own database now (twelve places;
  three of them ended sessions by application name and could have ended another run's). The area `coordination` run
  twice at once without the lock fails the lease tests on the old code in both runs and passes them with the fix;
  `TestJobs` (100 coalesced enqueues) failed in both of those double runs with a database timeout and passes under
  the lock, the finding of section 8 seen again. Not verified: the script's path with `flock` on Linux; the changed
  end-to-end queries on a cluster.
- **In work** (04:17), no builder; the development agent's integration run on the landing branch and the pipeline on `main`:
  - **Package 4 of the malware scan, the screens** (since 20:25, started on package 1's first stage and rebased when
    that landed): **Admin area › Settings › Malware scanning**, the malware view of the security dashboard, the status
    at file, version and repository, the repository's stricter-only setting, the manual, the German texts, the render
    cases with seeded verdicts of every state. It changes files under `web/` and the screens' documentation only; a
    shape the API does not give goes back to package 1's builder. **Reported at 21:37** (one commit on its branch,
    no API shape missing): the settings page (the switch as a check box with **Save changes**, as the other Settings
    pages have it, and a question before it is switched on over a backlog; the backlog, the scanners, "Scan everything
    again"; the signature database with its date, Current or Stale, the last failure and, for an installation that
    fetches no database itself, the commands to fill the repository; no find and no release on it, and for the
    auditor a read-only page with a link to the dashboard); the security dashboard with a tab strip **Vulnerabilities
    | Malware**, one row per blob with its verdict, signature, database date, repositories and release, a row that
    opens to every file that holds the bytes, search and filters in the address, the release dialog that lists every
    repository and asks for a reason, the withdrawal; the status as a fact on a file's page and a column **Malware
    scan** in a version's files, a banner on the version for each refused file, grey for "Checking" and green for
    "No malware found" alone; the closed section **Malware scan** in a repository's settings, read-only while the
    installation's switch is on and for an Owner who may not switch it off; the manual's sections for administrator,
    auditor and a refused download; German texts. **Its decisions:** Q-UI-873 to Q-UI-881. **The row of
    `upstream/scanner-clamav`** on the upstream registries page is not built: the API does not list that repository
    until package 3 declares it, and the page's wording then needs the second system repository. **Looked at by the
    development agent** (rule 11): the settings page as seeded in the light scheme and with a stale database, a
    failure and no scanner in the dark one at 1440 px; the dashboard with an opened row at 1440 px light and the
    release dialog with its missing-reason error at 390 px dark; a version with an infected file at 1440 px light;
    an infected file's page at 390 px dark; the repository's section for an Owner at 1440 px light and with the
    installation's switch on at 390 px dark. **One defect seen, sent back:** the page of a file whose download is
    refused still offers **Download** above the fact that says downloads are refused, and so does the row of a
    version's files. **Render cost:** seven new cases, about 46 s of the job's wall time; the builder also found that
    the render configuration never ran the 11 existing cases of the auditor (the project was missing), which now run,
    about 50 s more. Its last render run had no failure and no layout finding. **Finished at 21:49:** a file the gate
    refuses offers no download (Q-UI-882: the button is gone on the file's page and the row of a version's files shows
    a dash, with "Downloads are refused." beside the verdict; the download address stays as a fact, because a client
    that asks gets the refusal with its reason; a pending, a released and a clean file keep their download), looked at
    by the development agent at 390 px light and 1440 px dark; the German texts use the glossary's words (a release is
    "zum Download zulassen", the noun "Zulassung", a find "Malware-Befund"); the render seed's two remotes take the
    stack's own address; the banner of a version with an infected file is the component a blocked version has and
    stays amber beside the red verdict, as there. Gates with exit 0: the component tests (1,796), the UI lint, the
    layout check's tests, the localized build, links and check, the white-label scan; ten render cases without a
    finding. **Its commit is the first on the branch `feat/malware`** (worktree `.claude/worktrees/malware-landing`).
    For the walkthrough: at a version the words "not scanned" of the vulnerability scanning stand beside the malware
    banner, two scans a reader may take for one.
  - **Package 2 of the malware scan, the scanning side** (since 21:36), in two stages: first the two routes of the
    scan listener (a blob for the scanner, the signature database) with their admission, the blob token, the interface
    the database route serves from and the configuration keys, which is what package 3 waits for; then the job
    `malware-scan`, discovery and rescans, the call, the scanner kind `malware` with its unpacking, the `clamd` client
    and an in-process fake of it, the metrics. Bound by this evening's lessons: no default or local start may be
    refused for a missing scanner's address; anything under `web/` gets the UI tests; a check that fails without
    gating gets a name and a sentence for the System page. **First stage reported at 22:29 and put on `feat/malware`
    by the development agent** (five documentation conflicts with the gate's and the screens' commits merged by hand;
    on the branch's tip build, the full unit run of 171 packages, generate-check, links and check are green): on the
    scan listener `GET`/`HEAD /scan/v1/blobs/sha256:<hex>`, which serves one stored file to the malware scanner for a
    blob token of exactly that digest and answers 404 without a reason for a repository that is gone, disabled,
    virtual or a system repository, and `GET /scan/v1/malware-database` with `/<version>/<file>`, admitted by the
    malware scanner's certificate without a token; neither fills a remote, asks a gate or counts a download, and the
    blob read is a usage row of `system` under the token's repository. The blob token names the digest, the holding
    repository and a scan's id and lives as long as `malware.read_timeout`. The interface `malware.DatabaseSource`
    serves a version as soon as the row `malware_database` names files whose blobs are stored, so package 3 writes no
    route. The configuration of R-MAL-027 with its defaults, `process.scanner_kind` and `jobs.workers.malware`.
    **Its decisions:** Q-MAL-60 (no start is refused for a missing malware scanner: without an address the job makes
    no call and a reported check `malware_scanner` that does not gate says why; it replaces the planned refusal of a
    worker, and differs on purpose from the image scanner's rule that cost two red jobs this evening), Q-MAL-61 (the
    planner's unsure Q-MAL-27 holds with one new key, `malware.scan_listener.client_name`, which tells the listener
    which of its admitted names is the malware scanner, so the image scanner, which parses hostile images, reads
    neither route), Q-MAL-62 (a database file is served by the digest its row lists). Gates with exit 0: build, the
    full unit run, generate-check, lint, cyclo, gosec, chart tests, links and check, the white-label scan; the areas
    `malware`, `vulnimage`, `limits`, `authz` and `registry`; the UI profile's configuration validates. **What it
    cost:** about 25 unit cases of under a second, three integration tests of 7.4 s together with files of tens of
    bytes. **Not verified:** the route's answer for a version that a vulnerability finding blocks (the route never
    reaches a format's serve path). **Seen beside it:** the registry routes of the scan listener are not narrowed to
    the image scanner's name as the malware routes are; a route outside the format host must charge the usage itself
    or its row names no repository; the area `vulnimage` sleeps two seconds for a flush. **Second stage reported at 01:21 and put on `feat/malware` by the development agent**
    (two commits, a fast-forward; on the tip build, the full unit run of 175 packages, links and check are green): the
    commit hook inserts a row for every committed file in the commit's transaction and wakes the job (R-MAL-001); the
    job `malware-scan` takes asked rows first, then new, recent and the cycle's, keeps one slot free for a download
    that waits (Q-MAL-64), and many waiters make one call; retries from one minute doubling to six hours, then
    `failed`; the scanner kind `malware` reads the blob with its token, checks digest and size, unpacks `ar`, zstd
    always, gzip and xz above the size ClamAV takes, a decompressed tar entry by entry (Q-MAL-63), and answers
    `unscannable` at a bound; the `clamd` client over a session with an in-process fake of `clamd`; the cycle's marks
    every 30 days and what was downloaded lately after a new database; an infected OCI layer is refused while the image
    scanning runs beside it (R-MAL-017); the metrics of the worker, the scanner and the gate. A worker without a
    scanner's address runs discovery only and reports the check `malware_scanner` with a sentence, in English and
    German, which is what the System page shows on a stack without ClamAV. The interface the loader of package 3
    implements is `scanner.Database`. **Its decisions:** Q-MAL-63 to 65 (the tests flag a marker of their own, not
    the EICAR string, because CI scans the source tree with ClamAV), the gap H-1130 (a worker without a scanner's
    address leaves every artifact unchecked). No migration. **Its run of every integration area** (it held the lock
    when the gates changed, so it finished): 70 areas, 66 passing at once; three failed on its own commit hook, because
    tests of package 1 wrote a verdict with a plain insert and the hook's row was there first: those writes update on
    conflict now, and the three areas pass; one failed under load and passed alone (`coordination`, 100 coalesced
    enqueues made three executions where one or two are allowed; section 8). Gates with exit 0: build, the full unit
    run, generate-check, lint, cyclo, gosec, links and check, the UI tests and lint, the UI profile's configuration,
    the bare starts. **What it cost:** unit tests of under a second a package, about 16 s more in the area `malware`
    (46 s in all), payloads of 68 bytes to 2 KiB, nothing end to end. **Not verified:** the scanner against a real
    ClamAV (package 3's), the check's sentence in a render, what the commit hook adds to a commit. **Seen beside
    it:** the sweep of orphans reads every verdict row at each run; the rescan's answer counts the rows it looked at,
    pending ones it did not mark among them; the leg `e2e (vuln)` has no malware paths in its list yet.
  - **Package 3 of the malware scan, the database and the chart** (since 22:34, on the branch's tip), in two
    stages: first the task `malware-sync-db`, `freshclam` beside the worker and its seed, the declaration of
    `upstream/scanner-clamav` with a class of its own, the chart (the malware scanner's Deployment with `clamd`, its
    certificate as a fifth kind of pod, the `freshclam` container, the alerts, the values), the pinned ClamAV image
    with the CI job on the same pin, the row on the upstream registries page; then, when package 2's scanner process
    is on the branch, the scanner's loader of the database, a kind cluster with the real image, the measurements the
    planner could not make (the image's user and file system, `clamd`'s memory, the time to ready) and the one
    end-to-end case. No test downloads the real database: ClamAV's project allows only its own two tools to fetch
    it, so the tests fill the repository with a database of a few signatures, as an installation without internet
    does. **First stage reported at 01:45 and put on `feat/malware` by the development agent** (three commits,
    rebased by the builder onto package 2; the first two are squashed by the builder at the start of its second
    stage, because the first alone registers a gauge twice and stops a worker at start): the task `malware-sync-db`
    reads the header of each ClamAV file, wants `main` and `daily` and takes `bytecode`, stores a version under
    `<version>/<file>` in `upstream/scanner-clamav` in the transaction that records it and marks what was downloaded
    lately, refuses a lower version, removes old ones but the one in use and the one before, and fills `freshclam`'s
    directory from the version in use at a worker's start; a version a scanner refuses is put back. The repository is
    a local generic system repository (the declaration got its class), fixed in address and account, shown as "In use
    by the malware scan"; an installation without internet deploys the files of ClamAV's mirror tool into it. The
    chart: the malware scanner's Deployment with the containers `quader` and `clamd`, its disruption budget,
    autoscaler, Service, an ingress-only NetworkPolicy and the certificate of a fifth kind of pod; `freshclam` beside
    each worker that claims the queue `malware`; the values with their schema. The pin `CLAMAV_IMAGE` with its
    Renovate rule, and the release-gating job `Malware Scan (source code)` runs the pinned image now and keeps the
    real database in the runner's cache under a key of the day (before, every run fetched it anew, unpinned).
    **Its decisions:** Q-MAL-80 to 88, Q-HA-363, Q-REPO-412, Q-UI-883, the gap H-1140. The one to look at first: the
    image is `clamav/clamav-debian:1.5.5_base` by digest (Q-MAL-80), because the Alpine image exists for amd64 alone
    and Quader ships both architectures, and the feature line 1.5 over the long-term 1.4 because the unpacking table
    was read against 1.5. **Measured with the pinned image:** an unsigned database file is refused by `clamd`, and so
    is one whose index carries no signature (the planner's open H-1107); `freshclam` keeps the detached signatures;
    the real database is 89 MB, 23 MB and 0.3 MB; the first fetch took 11 s; the scan of the source tree 64 s over
    5,598 files; the image is 74 MiB compressed. Gates with exit 0: build, generate-check, lint, cyclo, gosec, chart
    lint and tests, links and check, the UI tests, lint and layout tests, the UI profile's configuration, the three
    bare starts; the areas `malware`, `upstream`, `postgres`, `vulnimage`, `coordination` (119 tests); `versions-check`
    fails on this machine's buildx as known; in the full unit run `TestR_BAS_006` failed once beside an integration
    build and passed alone (section 8). Looked at by the development agent: the section **Used by Quader** of the
    upstream registries page at 1440 px light and the page of `scanner-clamav` at 390 px dark; its badge says
    "Upstream registry" for a local repository, which the builder fixes. **What it cost:** unit and chart tests of
    about a second each, two integration tests of about 4 s; nothing end to end yet. **Not verified:** the changed
    CI job on CI; `clamd` in a pod, its memory, its file system, the time to ready. **Seen beside it:** the batched
    marking of what was downloaded lately reads the verdicts' table from its start at each batch; the settings page
    of a system repository offers access details, download limits and notes. **Second stage reported at 03:04**
    (on its branch; the landing branch moves to it when the development agent's integration run on the earlier tip
    has ended, because that run reads the same worktree): the first stage's two commits are squashed, the tree
    byte-identical. **The loader** of the scanner's database behind package 2's interface: it reads the database
    route with the scanner's own certificate every 5 s until a version is loaded and every minute after, checks each
    file against the listing's size and SHA-256, moves the files `clamd` reads aside, renames the new ones in and
    tells `clamd` to load; a version `clamd` refuses is put back and reported, so that the worker's task puts the
    previous version in use again. **The end-to-end case, one, in the set `vuln`:** with the real `clamd` and a
    database of a few signatures that flag the tests' marker, deployed to a repository of the installation's own as an
    installation without internet does, a file with the marker is refused 403 through `/files/`, a clean one served,
    npm installs nothing of a package whose archive holds the marker, and with the switch on a download not checked
    yet is answered after its scan; 3.1 s in the set once the database is loaded. `make e2e SET=vuln` and `SET=push`
    passed on a kind cluster of its own. **A defect the cluster showed before it could reach `main`:** with the
    scanning on by default the development profile rendered the malware scanner in every profile, and with only the
    earlier packages on the branch that scanner never became ready, so every kind leg's start would have waited ten
    minutes and failed, and `freshclam` would have fetched the real database in every leg; the malware scanning is
    now off in every profile but `vuln`, and there it runs without internet (Q-MAL-90). **Measured once on the
    cluster with the pinned image (arm64), written into the specification:** `clamd` runs as an unprivileged user on
    a read-only root file system with every capability dropped and writes two directories; with ClamAV's whole
    database (113 MB on disk, the copy of the source scan, no new fetch) it holds 994 to 1,000 MiB resident and does
    not double at a reload (1,929 MiB with the concurrent reload on, which is why it stays off, Q-MAL-89); a pod is
    ready 21 s after its start, 9 s with the tests' database; a new version is loaded within a minute in about 9 s,
    the pod briefly unready; a warm scan takes 3 ms for 1 KiB, 5 ms for 1 MiB and 250 ms for 100 MiB (the first of
    each 12 ms, 78 ms and 4.3 s). The real `clamd` answers an encrypted archive and a crossed size bound with the
    names the scanner's table expects. From it: `clamd`'s request goes from 1,536 to 1,280 MiB with the limit at 2
    GiB, and the sizing profiles size the malware scanner (Q-HA-364). **After a refused reload `clamd` holds no
    database at all** and a second reload does not mend it, so the loader puts the files back and restarts `clamd`
    (Q-MAL-89). Also: the four alerts with their rule tests; the page of `scanner-clamav` says "Local repository".
    **Its decisions:** Q-MAL-89 to 91, Q-HA-364, Q-UI-884, the gap H-1141 (a version `clamd` refuses at a pod's
    start leaves the pod unready, and if every scanner starts at once nothing puts it back). Gates with exit 0:
    build, the full unit run, generate-check, lint, cyclo, gosec, chart lint and tests, links and check, the UI
    tests, the UI profile's configuration, the three bare starts, the areas `malware`, `upstream`, `vulnimage` and
    `coordination`. **What it cost:** the loader's unit test 0.03 s; nothing in integration; the end-to-end case 3.1
    s, 15 s on a fresh cluster, and 14 s for the leg `vuln` to load the ClamAV image into its nodes beside the pull of
    74 MiB; no other leg runs a malware scanner. **Not verified:** anything on CI (the image's pull on a runner);
    amd64; the refusal path in the cluster (a fake and by hand in Docker); H-1141. **For the owner's installation**
    (read from its values): the worker's label for the way to the internet is there, so `freshclam` reaches ClamAV's
    servers on 443 (that the cluster's policy admits their network is not verified); the malware scanner needs no
    label; the two pods ask for 1.5 CPU and 3 GiB with limits of 5 GiB, `freshclam` 50 m and 128 MiB beside each
    worker; the values' TLS keys are the change it waits for anyway. **Seen beside it:** the suite builds its test
    upstream's and its clients' images under shared tags on the machine's Docker, which parallel builders can
    overwrite.
  **The run of every integration area on the landing branch** (the development agent's, 01:49 to 03:23, on the tip
  with the screens, the gate, the scanning side and the first stage of the database and the chart): 65 areas
  passing, two failing. **`querybudget`, which looked like a defect of the feature and was the test's harness:** the
  download of a Debian index at its plain path seemed to cost two statements on the branch, `GetLiveItem` and an
  inserted job. Read by package 3's builder (03:42): the download costs one statement on every path with the
  scanning on; the inserted job is the scheduler, which every 10 s offers the due slot of every task at once, 27
  inserts on the branch (25 on `main` and the malware scan's two tasks); the harness counted a statement as the
  requests' when it ran 27 times in a window of 30 downloads, so a scheduler run inside the window looked like a
  statement of each download (and moved from one index path to another between two runs). Neither the gate nor
  the commit hook runs there: an index by its suite's path is a route of the kind Metadata, and a generated item is
  served without a verdict. The harness now counts a statement as the requests' when it ran for nine requests in
  ten and its runs spread over half the window (Q-MAL-92); each budget window first checks that the scanning is on
  and nothing strict, so the budget is shown for a default installation; R-ARCH-027 names the verdict as part of
  the one lookup and the budget of a download that waits. One commit of the type `test` on the branch; the area
  passes, with `malware`, `files`, `packages`, `deblocal` and `debvirtual`. The second place that counts by the old
  rule (`vulnblock`, three inserts under its threshold) follows in the same commit. **`coordination`, not the feature's:** `TestLeases` saw two holders
  of a lease at once while another builder ran the same area on the same stack; rerun at 03:24 it failed another
  case (`TestScheduler`, no single holder of the scheduler's lease within 10 s) while two other builders' test
  processes were on the stack, each "under the lock". Both read `pg_locks` for an advisory lock by its key without
  the database, the view shows the whole server, and every run has a database of its own there, so a holder of
  another run is counted (read in the test, section 8). A builder fixes the lock and those queries (since 03:30);
  the area is run again on the branch when it runs alone. The landing branch stands on the finished tip of package
  3 since 03:24 (the first stage's two commits squashed, the tree checked identical).
  **How the malware scan lands from here** (the development agent's, 2026-11-20): its first stage is on `main`; what
  follows (the gate, the screens, the scanning side, the database and the chart) is collected on a branch
  `feat/malware` and lands together, as the project `upstream` did. Why: with the scanning on by default and no
  scanner in the chart, a release in between would show "Checking" at every file for good and offer a switch that
  answers every download with a 503; and this evening's three red pipelines came from landings in pieces.
  - **Package 1 of the malware scan, the gate and the verdicts** (since 19:20, [the entry below](#with-the-owner)), in
    two stages: first the migration (00088), the store and the whole API contract, so that the screens' builder can
    start on its branch; then the gate in the two serve points, the fills of remote repositories, virtual
    repositories, the walk of an OCI virtual repository, the repository's stricter-only setting, the audit events, the
    Bastion feed and the backup. It has to be landable alone: before a scanner exists, an installation behaves by
    default as it does today.
    **First stage reported at 20:19 and landed at 21:14** (one commit, "an installation keeps a malware verdict for
    every stored file and offers the switch, the finds and the release of a file over the API", released at 22:26).
    **Second stage, the gate, reported at 22:20 and put on the branch `feat/malware` by the development agent** (one
    commit on top of the screens'; four documentation conflicts with the screens' commit merged by hand, the coverage
    checklist regenerated): the gate (`internal/malware/gate`) at both serve points, asked for routes of the kind
    Download and for every request under `/files/`; an infected blob refused always while the scanning is on; "strict"
    is the installation's switch or the setting of the repository addressed or of the one that holds the file; under
    it a blob not checked yet waits until 25 s after the request's arrival and is then answered 503 with
    `Retry-After: 30`; a replica holds at most 512 waits and a draining one answers its waits at once; a strict fill
    of a remote repository is held and served from the stored file through the gate, and a body passed on unstored
    during an outage of the object store is decided by its digest's row without a wait; the pending 503 ends the walk
    of a virtual repository, and the OCI walk ends on any block, the vulnerability scanning's too (the finding of the
    morning is closed by it); the format kit's check 17. **No change of the API contract, no configuration key, no
    migration.** **Its decisions:** Q-MAL-49 to Q-MAL-54 (a refusal names the repository the client addressed, never
    the member; only Download routes and `/files/` are gated, system reads are not), the gaps H-1120 (a reader can
    fill a replica's 512 waits) and H-1121 (a reader's requests move what they ask for to the front of the scans).
    **Landable alone, shown at the serve points:** with the defaults a file without a verdict is served at once
    through `/files/`, the npm route and an npm virtual repository; with the switch on and no scanner a download gets
    its 503 after the configured wait (2 s in the test), its row is asked and the job queued; the bound of 25 s is
    shown by a unit test with an arrival in the past. **Gates on its own base, each exit 0:** build, the full unit
    run, generate-check, lint, cyclo (after one function was split), gosec, chart tests, links and check, the
    white-label scan, and 16 integration areas once each under the lock, all passing: `malware`, `files`, `packages`,
    `proxy`, `virtual`, the four of OCI, `npm`, `nuget`, `deblocal`, `ansiblelocal`, `limits`, `vulnimage`, `bastion`.
    On the branch after the merge: build, links and check by the development agent, the unit run and the UI tests
    running at 22:29. **What it cost:** nine unit tests of under a second together, a kit case of milliseconds per
    format, four integration tests in the area `malware` (about 20 s), one each in `ocivirtual` (2 s) and `bastion`
    (4 s); nothing end to end. **Left open, passed to package 2:** the gate's refusal counter and wait metrics; the
    commit hook's insert, without which R-MAL-001 is not done; the acceptance of R-MAL-017 (an infected OCI image
    with the image scanning beside it). **Not shown:** the path of an unstored body against the stack (unit only);
    range requests; the once-a-minute deduplication of the audit event is per replica. The first stage: the migration 00088 with five
    tables (the verdict of a blob keyed by its digest, the release, the settings with the switch off, the signature
    database in use, what the job last saw of the scanners), all registered for backup; the store behind one
    interface of 17 methods, the one writer of verdicts, releases and the switch (Q-MAL-40); the join of verdict and
    release into the download statement, so every item carries its state without a second query; eight operations
    under `/api/v1/malware/` and the `malware` member on files; the repository's stricter-only setting with its
    weakening rule, pulled forward because the repository's page needs the API to take it; the audit events, the
    replay at a restore and the Bastion feed's entries. **Its decisions:** Q-MAL-40 to Q-MAL-48, among them the gaps
    of the screens closed before the screens are built (Q-MAL-45: an operation `getMalwareSwitch` for every signed-in
    caller, because an Owner on a repository's settings has no `settings:read`; more members on a file's status; a
    find lists at most 100 items and 100 repositories with counts) and that `malware_verdict` is a signal and not a
    change area (Q-MAL-44). **Gates, each exit 0:** build, the full unit run, generate-check, lint, cyclo, gosec,
    links and check, the white-label scan, and under the stack's lock the integration areas `postgres`, `repository`,
    `files`, `packages`, the new `malware` and the Bastion feed. **Rebased on `main` without a conflict, and run before the
    landing because the download statement changed for every format:** every gate again with exit 0, the chart
    tests among them, and under the stack's lock the areas `api`, `proxy`, `virtual`, the four of OCI, `registry`,
    `npm`, `nuget`, the four of Debian, the three of Ansible, `transfer`, `vulnimage`, `limits` and `malware`, all
    passing; in `files` one test failed once under load and passed alone (section 8). By the development agent on
    the commit as landed: build, the full unit run, generate-check, links and check. **What it cost:** about ten unit tests of milliseconds; a new integration area
    `malware` of two tests, 5.1 s together with five files of 8 bytes, which falls into shard b; nothing end to end.
    **Landable alone, shown by two tests:** with the scanning on, the switch off and no setting on a repository the
    gate is strict nowhere, and without a verdict nothing is refused. **What an administrator gets who turns the
    switch on with no scanner deployed, once the gate is in:** every download of content without a verdict waits 25 s
    and is answered 503 with `Retry-After`, again at every retry; the way out is the switch (Q-MAL-36), and the
    settings answer shows the number of unchecked blobs beforehand. No guard was built. **Seen beside it, for the
    packages that follow:** package 2's planned check refuses a worker that claims the queue `malware` without a
    scanner's address, which with the scanning on by default may refuse a default or local start unless the default
    queues leave `malware` out; the count of unchecked blobs is computed over all items at every read of the
    settings, costly on a large installation.
  - **Package B of the scanner, the analyser's database in a repository** (since 21:44; on `main` with package A
    and the malware scan's migration, so that its own takes 00089 without a gap):
    the task that fetches the database into `upstream/scanner-trivy`, the system's own reading through that
    repository, the actor of its fills, the cleanup's exemption for the layer in use, the "in use" line of the admin
    page and the sources screen; a migration with the next free number after the malware scan's 00088.
    **Reported at 23:24, sent back at 23:25** (one commit on its branch, based before the evening's fixes): the task
    `vuln-sync-trivy-db` asks the repository the configuration names (`upstream/scanner-trivy` by default) for
    `aquasec/trivy-db:2` through the worker's own fetching, as the system subject, so the repository fetches the layer
    once and keeps it, and a second run only checks the tag; a local repository is read from storage with no request,
    the path of an installation without internet; the database in use is the layer's digest on the source's row, and
    a worker reads that layer from the repository and checks its hash; nothing falls back to the public registry, a
    failed run records one of six codes and the database in use stays; migration 00089 drops the old table and its
    stored file; the sources screen has a row **Image database** with the repository, the image and the tag, a button
    to fetch it now and, for a local repository, the commands to fill it; the upstream registries page says "In use
    by the image scanning". Its decisions: Q-VULN-190 to 193, Q-API-380, Q-UI-890. It also connected the usage
    tracking of everything a worker fetches in the background, which was never recorded, mirror runs included
    (Q-VULN-192). Looked at by the development agent: the sources screen with a current database at 1440 px light, a
    local repository with a missing database and the commands open at 390 px dark. **Why it went back:** nothing
    removes an older layer (the general cleanup of a remote's cache, R-REPO-046, was never built, and the builder
    shipped that as a gap, H-1160): about 142 MB for every new database, 0.57 GB a day for good, against Q-VULN-175
    (the layer in use, and the older ones for two days). It builds that removal for the database's system repository
    now. It also rebases onto the evening's fixes, runs every integration area and the bare starts (its change
    removes configuration keys and adds a rule), and answers with what it ran: whether any CI leg or local profile
    now pulls the real database of 142 MB, where the removed keys are still written, and what the owner's
    installation does at the upgrade. **It lands after the nightly.** **Second report at 03:36, accepted** (one
    commit, rebased onto the evening's fixes). **The removal:** after a run that succeeds, every layer and manifest of
    the database's image older than the repository's own cache period goes, in batches, through the items' own delete
    path as the system subject, so blobs and audit follow; the layer in use and the manifest the run read stay; a
    failed run removes nothing. In practice that is `upstream/scanner-trivy` with its 48 hours, at most the layer in
    use and two days of builds, about 1.3 GB; a repository of the installation's own keeps every layer and is the
    operator's (Q-VULN-194). H-1160 shrinks to builds that fail the archive's checks, which pile up until a run
    succeeds. **Its three answers:** (a) before this commit every kind cluster and the local UI profile fetched the
    real database of 142 MB from the public mirror whenever they were up at minute 23 of 00, 06, 12 or 18 UTC (the
    old source was on by default and no profile switched it off); with this commit the scheduled run is off in the
    development profile, the restore profile and the UI profile (Q-VULN-195), a run by hand still fetches, and a
    fresh cluster does not fetch at its first scan; (b) no profile, chart value, fixture or example sets a removed key
    or an image the new rule refuses, and the UI profile's configuration validates; (c) the owner's installation
    needs no change of its values: after the migration the first image scan finds the layer missing from the
    repository, wakes the task, and a worker fetches the tag and one blob of 142 MB through the system repository;
    if the tag still names the old digest nothing is detected again. A sync failure of a registered conflict
    answers 409 now. **Its run of every integration area** (67): 66 passing at once, started before the lock was
    known not to hold; `vulnrestore` failed on this commit's own change (the system information lists the image
    database as a source now, as the sources API does), its test follows and it passed alone. The bare starts of a
    worker, `packages` and the role `all` pass. Gates with exit 0: the full unit run, lint, cyclo, gosec,
    generate-check, chart tests, the UI tests, lint and layout tests, links and check. What the retention cost: one
    unit test, one database-only integration test of 1.2 s, about 3 s more in the area `vulnimage`. **Collected with
    the five fixes of the nightly on the branch `fixes/nightly`, whose gates run.**
- **The role `scanner` (package A), landed on 2026-11-20 at 20:19** in two commits: "container images are analysed in
  scanner pods that hold no secret of the installation" and "every kind of pod has a TLS certificate of its own, which
  the chart issues through cert-manager" (the owner's decision, [With the owner](#with-the-owner)). **The
  certificates as built:** four kinds have a pair and a name, `api`, `packages`, `worker` and `scanner`
  (`<fullname>-<kind>.<namespace>.svc`, and every pair also the ops name, so that one ServiceMonitor endpoint still
  works; the cost is H-1071, any kind's certificate passes as any pod's ops listener toward Prometheus); `api` and
  `packages` serve only, `worker` and `scanner` may also present theirs as a client. With `tls.certManager.issuerRef`
  the chart renders one Certificate and Secret per kind (`<fullname>-tls-<kind>`), without it
  `tls.existingSecrets.<kind>` names a Secret each; two kinds on one Secret, or both ways at once, fail the render. A
  pod mounts only its own kind's Secret. The worker requires the scanner's name and the scanner admits only the
  worker's; the scanner requires the name of `packages`, and the scan listener admits the names of
  `server.scan_clients` (a list, so that the malware scanner is one more entry). A name must stand in the certificate
  exactly, a wildcard does not count; the role `all` keeps one certificate and optional names. Each ingress controller
  verifies a backend under its kind's name: HAProxy Ingress from the Service's annotations, Traefik with a
  ServersTransport per kind, ingress-nginx with an Ingress per kind, the Gateway API with a BackendTLSPolicy per
  Service. `tls.secretName`, `certKey`, `keyKey`, `tls.ops`, `scanner.tls` and `backendTLSPolicy.hostname` are gone.
  **Its decisions:** Q-HA-345 (the owner's, with his two sentences; it supersedes Q-HA-317 and the one-certificate
  part of Q-VULN-163), Q-HA-346 to 349, Q-ARCH-552 and 553, Q-VULN-185, ADR-215; H-1035 rewritten with a table of
  what a stolen certificate of each kind still allows, the gaps H-1071 to H-1073 (H-1072: the name check is as strong
  as the control of who may ask the issuer for a certificate). **How a fifth kind is added** stands in
  [helm-chart.md](../developer/helm-chart.md): one entry in the chart's table of kinds, a key of `tls.existingSecrets`,
  the mount in its Deployment. **Seen on a kind cluster of its own, before the rebase:** each Deployment mounts only
  its kind's Secret; the scanner admits the worker's certificate and refuses those of `packages`, `scanner` and `api`;
  the scan listener admits the scanner's and refuses the other three; the ingress answers 503 when the Service of
  `api` is given the name of `packages` and 200 again with its own; `e2e (vuln)` passed (94 tests); the rotation
  drill renewed three certificates, every pod served the new one within 1.7 s, 8,993 requests all answered 200.
  **On the final commit:** `e2e (vuln)` once more (94 tests), and each gate once with exit 0 (lint, vet, gosec,
  cyclo, generate-check, the architecture tests, unit, chart lint and tests, the coverage tool's tests, links and
  check, the white-label scan) and the integration areas `vulnimage`, `authz`, `limits`, `oci` and `packages`; by
  the development agent before the push: the full unit run, the chart tests, links and check. **What it cost:** two
  unit tests of well under a second, chart tests of about 8 s together, five more handshakes in one case of the area
  `vulnimage` (1.7 s); no new end-to-end case, nothing in a format's leg. **Not verified:** the Ingress per kind of
  ingress-nginx and the ServersTransports of Traefik against a live controller (chart tests only); the Gateway API's
  policies against a gateway (the set `edge` was not run); the restore drill with the changed values (the set
  `backup` was not run); a change of the issuer's CA (H-1073); the looks at the two listeners on the final commit
  (the builder's script had reused the first cluster's certificates; unit and `vulnimage` cover the refusals).
  **The owner's installation and this release**, read and rendered on 2026-11-20: its values still name
  `tls.secretName`, which the new chart's schema refuses, so its upgrade fails before anything is applied and the
  installation keeps serving the release before; the other order fails the same way (the released chart's schema
  refuses `tls.certManager`), so there is no order without one refused upgrade. The change of its values is prepared
  as a patch and given to the owner, who commits there (the issuer in place of `tls.secretName`, no `caSecret`, his
  hand-written `Certificate` removed; rendered with the new chart: 28 objects, four Certificates from his issuer).
  Nothing else of his cluster waits on that release (no other Kustomization depends on it). Read on his cluster: the
  issuer is of the CA type and its Secrets carry `ca.crt`; pods reach every endpoint of the cluster without a label,
  which is all the scanner asks. **What the upgrade then does** (worked out from the chart, not run): from the moment
  the Services carry the new names, the ingress verifies the old pods under names their old certificate does not
  have, so requests to an old pod get 503 until the rollout has replaced it; named to the owner. The scanner adds two
  pods that ask for 500 m CPU and 1 GiB each by the chart's default. **Its record from the first stage:**
  - **Package A of the scanner, first stage reported:** `0f23ad8ed` on `7fe52214` (143 files), the role `scanner`
    with its token, its listener on `packages`, the chart's objects and the tests up to the integration tier; no
    migration. Gates green but one: `make versions-check` fails on this machine's Docker buildx (F-17, the owner's
    link), not on the change. **What it cost:** about 25 unit tests in about 4 s; the fidelity test 2 s warm and 18 s
    with a cold build cache in the job `container-scan` (the module download there not measured); six chart tests in
    2 s; eight integration tests in the area `vulnimage`, about 21 s more in shard c; one end-to-end case in
    `e2e (vuln)`, estimated 20 s, **not run yet**. No leg of a format gains anything. **Defects its own tests found,
    fixed:** a scanner that refuses the worker's certificate counted as a lost call and spent an attempt; the rule
    for the scan listener stood in a NetworkPolicy of its own; `spool.type: ephemeral` was ignored; usage rows of
    the system subject lost their credential; the manifest's digest was not checked against the call's; the role
    `all` went unready without the analyser, which would have stopped the UI development backend (Q-ARCH-551: its
    check is reported and does not gate there). **Its own decisions:** Q-VULN-180 to 183, Q-ARCH-550 and 551,
    Q-HA-340 to 343, the gap H-1070 (the scanner pod names no service account of the chart). **Left in place:** a
    scanner outage also fires `QuaderVulnerabilityImageScansFailing` after an hour, as the specification says; a
    repository enabled again can lose one of five attempts while a replica's grant index still shows it disabled.
    **Second stage, done at 17:45:** `9c1925e35` (it replaces `0f23ad8ed`). The end-to-end case passed on a kind
    cluster of its own in 6.9 s, the analysis stored 6.6 s after the task's run, on the third run of
    `make e2e SET=vuln`: the first failed on the case itself (a new manifest waits `scan_delay`, 30 s; the profile
    `vuln` now sets it to 0), the second in the set's setup (its admin writes went past the write class's burst on
    an idle host, section 8). In the third one journey of another feature failed on leftovers of the two runs
    before on the same cluster; a fresh cluster in CI shows it. **Seen on the cluster:** the analyser runs in the
    distroless pod; the pod has no service-account token and no secret of the installation (three files under
    `/var/run/secrets`: the pair and the internal CA); the NetworkPolicy lets the scanner reach the scan listener and
    lets only the worker pool of the queue `vuln` reach the scanner; readiness under `helm --wait`. **Two defects
    the run found, fixed:** the development profile's Valkey policy selected `quader/serve=true` and so admitted
    the scanner (it names its clients now, Q-HA-344); a replica of the role `all` did not start, because the
    scanner made its working directory at the chart's absolute path (an empty `scan_dir` now lies under the spool,
    Q-VULN-184), which would have stopped the UI development backend. The integration areas `authz`, `oci` and
    `packages` passed, `limits` after that fix. **Not verified:** a scanner pod killed in the middle of a call; the
    ServiceMonitor's scrape with the ops pair; the step in the job `container-scan`; the nightly's drain sets; the
    other integration areas. **Resumed at 18:05 for the owner's decision of a certificate for each kind of pod**, which
    became the second commit (above).
  **Housekeeping at 17:20:** twelve local branches without a worktree and without a commit missing on `main` deleted.
  **Why package A waited until the evening:** the owner's installation takes every release within a minute, and its
  one certificate allowed server authentication only, so the scanner pods as first built would have stayed not
  ready; asked for one more usage, the owner decided a certificate for each kind of pod in its place. **Housekeeping of 2026-11-20:** `.bin/trivy-v0.76.1` in the main checkout, the analyser of the pin before,
  removed (the pin is v0.76.2, and the Makefile's path carries the version).
  **Cut between package 1 and package B**, set by the development agent where the two plans overlapped: package 1 declares
  `scanner-trivy` (its 48 hours, its include pattern, the 404 for every client) and leaves the "in use" function empty,
  so the admin page says "Not in use" until package B supplies it; the system's own reading through the repository, the
  actor of its fills and the cleanup's exemption for the layer in use are package B's.   **Numbers a builder holds** for decisions and gaps of its own: package A Q-VULN-180, Q-ARCH-550, Q-HA-340, Q-OCI-235, Q-AUTHZ-330,
  Q-LIM-70, Q-OPS-220, Q-EP-90, H-1070 to H-1079, ADR-215. The packages of `upstream` used Q-REPO-390 to 392, 400
  and 401, Q-AUTHZ-305, Q-API-363 and 364, Q-LIM-75 and Q-UI-838 to 844 (Q-UI-841 was taken back before landing); the
  next builder gets ranges above Q-REPO-401, Q-API-369, Q-UI-849, H-1090 and ADR-217.
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

- **A malware scan of every artifact with ClamAV** (2026-11-20, his words): "Kannst du gleich mal mit mir einen ClamAV
  Scanner nach dem Schema von Trivy vorbereiten. Brauchen wir auch. Alle Artefakte müssen Viren-geprüft werden. Es muss im
  Admin-Bereich einen Schalter geben, der es ermöglicht, dass nur geprüfte Artefakte gedownloaded werden können."
  **Decided by him on 2026-11-20 in a working paper** (three readings of his words and fourteen decisions, each with
  options and the development agent's favourite; read out at 18:45 on his word "Ließ die Antworten aus und lass uns mit
  der Entwicklung fortfahren"). **His answers:** the three readings hold (every stored item a client downloads; one
  setting of the installation, in the database, switched in the UI; "geprüft" means a verdict exists). The verdict
  hangs on the blob, installation-wide (1 a). A scan runs in the background after the commit, with a priority scan
  and a bounded wait when a pending blob is asked for under the switch (2 c). Under the switch a pending blob is held
  up to 25 s, then answered 503 with `Retry-After` (3 a). **The switch is the installation's, plus a setting per
  repository that can only make it stricter, as a detail on demand (4 b, against the development agent's favourite
  of the installation only).** An infected blob is blocked always while the scanning is on, kept, and released per
  digest with a reason (5 a), with his note: "Das ist eindeutig etwas das eher der Instanz-Auditor bewertet im
  Security-Dashboard als der Administrator. Der Administrator hat das Settings, Funde gehen in Security-Dashboard."
  The development agent reads that as: the settings page is the administrator's and holds the switch, the backlog,
  the database and the scanner's state, and no list of finds; a find is assessed and released in the security
  dashboard by whoever holds the instance's auditing rights there. `clamd` runs in a Deployment of its own of the role
  `scanner` (6 b). The scanner reads one blob by digest through a route of the scan listener (7 a). `freshclam` runs
  beside the worker and each database version is kept in the system repository `upstream/scanner-clamav` (8 b). A new
  database reaches what was downloaded lately within a day and the rest in a cycle of 30 days (9 c). The limits go
  toward ClamAV's ceiling of 2 GB a file, the scanner unpacks outer containers itself, and above a size of Quader's
  own an artifact is not scannable (10 b). For OCI the gate sits on the blobs and the two scans are independent
  (11 a). A refusal ends a virtual repository's resolution, a later find goes to the Bastions at once, and the
  window of up to 10 minutes after switching on is stated (12 a). A page **Admin area › Settings › Malware scanning**,
  the status at file and version, a malware view in the security dashboard (13 a). The scanning is on in the chart,
  the switch off (14 a). **Specified on 2026-11-20 at 19:15** (`c5463eced`): a page of its own,
  [malware-scanning.md](../spec/malware-scanning.md) (R-MAL-001 to R-MAL-031, his answers as Q-MAL-1 to Q-MAL-15, the
  planner's decisions as Q-MAL-20 to Q-MAL-36, the gap H-1100), with R-REPO-075 (`upstream/scanner-clamav`), R-HA-068
  (the malware scanner's Deployment) and R-HA-069 (the `freshclam` container), amendments where another page owns the
  mechanism, a security page ([malware-scanning.md](../security/malware-scanning.md), H-1101 to H-1113) and ADR-220 to
  ADR-222; the specification counts 1,047 requirements. Gates on the commit: the coverage tool's tests, links (0 broken)
  and check, the full unit run and the chart tests by the planner, the white-label scan. **Built in four work packages:**
  (1) the gate and the verdicts (migration 00088, the store, the join into the download statement, the gate in the two
  serve points, remote fills, virtual repositories, the OCI walk, the repository's setting, the API, the audit events,
  the Bastion feed, backups), which needs nothing but `main` and lands first, **in work since 19:20**; (2) the scanning
  side (the job `malware-scan`, discovery and rescans, the call, the blob token, the scan listener's routes, the scanner
  kind `malware` with its unpacking and a `clamd` client, metrics), which needs the scanner role on `main` and package
  1's store; (3) the database and the chart (the task `malware-sync-db`, `freshclam`, `upstream/scanner-clamav`, the
  malware scanner's Deployment with `clamd`, the pinned ClamAV image, the CI job on the same pin, the end-to-end case),
  which needs the certificate for each kind of pod and package 2's database route; (4) the screens (**Admin area ›
  Settings › Malware scanning**, the security dashboard's malware view, the status at file and version, the section in
  a repository's settings), which starts on package 1's API contract. **What the planner could not verify:** the 25 s
  wait rests on apt's timeout of 30 s, which is a maintainer's statement on Debian's list and not the manual, and apt's
  handling of a 503 is not known; whether the official ClamAV image runs as an unprivileged user on a read-only root
  file system; `clamd`'s memory with today's database; whether `clamd` checks a `.cld` file against the publisher's
  signature (H-1107); whether `freshclam` keeps the `.sign` files; any terms for passing the signatures on; pulls
  through containerd's image store; every test time is an estimate. **Where the code contradicted the planner's first
  report:** the generic virtual engine already ends its walk on a 403 but goes on after a 503, so the pending answer
  got a rule of its own; a well-known repository had no class, every one being a remote (the declaration gets a
  `Class` member, Q-REPO-411). What the planner had established before, read in the code and in ClamAV's own
  documentation:
  - **One place every download passes** (`itemStore.Serve` in `internal/formathost/ingest.go`, where the block of the
    vulnerability scanning sits; `/files/` has a copy), and it reads the blob's row in the same statement: a verdict per
    blob costs no query there and holds for every format and class, also those to come.
  - **The scanner role is for images only**: its token names one manifest, its listener takes registry paths. A second
    analyser is the case for which ADR-212 and ADR-213 name their own conditions for looking again.
  - **ClamAV cannot go into Quader's image** (distroless, a static binary; `clamd` is a dynamically linked program under
    the GPL-2.0): a container of its own, which is also the licence's boundary.
  - **Quader may not fetch the signature database itself**: ClamAV's project allows `freshclam` and `cvdupdate` and
    refuses scripted clients, so the shape of `upstream/scanner-trivy` cannot be copied; the generic format has only
    the class local.
  - **ClamAV 1.5.5 reads neither `ar` nor zstd**, so a `.deb` and a zstd layer have to be unpacked by the scanner, for
    which Quader has bounded readers.
  - **A signature scan does not find most registry malware**: no measurement of ClamAV against malicious packages was
    found, and in a benchmark of 6,420 malicious npm packages 72 % work through install scripts; the `malicious`
    advisories (npm and NuGet only) find those by identity. The switch should be named for what it is.
  - **Not verified:** `clamd`'s memory with today's smaller database (the documentation's figures, upwards of 1.2 GiB,
    are older); any terms for passing the signatures on; apt's behaviour on a 503.
- **A certificate for each kind of pod, decided by the owner on 2026-11-20** ("Option c"). How it came up: his
  installation takes every release within a minute, and its certificate allows server authentication only, so the
  scanner pods of package A would have stayed not ready and the upgrade would have failed. Asked to add `client auth`,
  he asked back: "Option A, macht es vielleicht Sinn das jeder Pod-kind sein eigenes Cert erhält?" What the development
  agent found in package A's branch and on his cluster: the scanner mounts by default the pair the edge trusts for `api`
  and `packages` (H-1035); the scanner's optional pair of its own must carry the same name (R-HA-066), so by the
  specification's text the edge would accept it as well (a conclusion from the text, not tested); both links admit any
  client certificate of the CA, and his issuer `cluster-ca` has 76 certificates, 28 of which allow client authentication;
  the planner had dismissed a required pair for the operator's effort and named the condition for looking again, the
  chart rendering cert-manager Certificates itself (Q-HA-317). The options as put to him: (a) as built; (b) only the
  scanner gets a pair and a name of its own, required; (c) every kind of pod (`api`, `packages`, `worker`, `scanner`)
  gets a pair and a name of its own, each link checks the name of the other side, and the chart issues the pairs
  through cert-manager from one issuer value, with a Secret's name per kind for an installation without it. Named to
  him with (c): cert-manager becomes the standard path, and over (b) it gains little security for the three kinds that
  hold the database's and the object store's credentials anyway; its gain is one rule and a chart that knows names and
  usages. **Built and landed on 2026-11-20 at 20:19** with the scanner role (run record). **For his installation:** the values
  name the issuer in place of `tls.secretName`, and his hand-written `Certificate` goes; the development agent
  prepared that change as a patch, gave it to him and does not commit it (the rules of that repository). Until he
  commits it, his installation stays on the release before. The one line `client auth` is not needed any more.
- **H-812** ([image-scanning.md](../security/image-scanning.md)), **decided by the owner on 2026-11-20** ("Perfekt, machen wir wie
  du analysiert hast", after he asked for a look at how Harbor's scanner adapter does it): the analysis of an image moves into
  a role `scanner` of the same binary with a Deployment of its own and no secret of the database, the object store or Valkey.
  The worker keeps the job and calls the scanner over HTTPS with the repository, the digest and a token of minutes for that
  one manifest; the scanner pulls the image through a listener of `packages`, checks the layers, runs the analyser and
  answers the package list. The detection stays in the worker, so the scanner needs neither the vulnerability database nor
  the internet; the scanner's pull counts as no download and against no limit. Specified on `main`
  ([vulnerability-scanning.md](../spec/vulnerability-scanning.md) R-VULN-041 to R-VULN-046, ADR-212 to ADR-214), landed
  on 2026-11-20 (run record); the database in a repository follows as package B.
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
  Q-AUTHZ-300, 301, 303 and 304, Q-API-360 and 361, Q-UI-830 to 836, Q-LIM-62 and 63. Its first builder's: a caller without the right gets 403 on the admin API's list and on a single
  registry, not 404, because the keys are the same in every installation and a 404 would hide nothing (Q-API-362); the
  well-known repositories are declared in the order of the format registry (Ansible Galaxy, Debian, npm, NuGet, OCI),
  and the admin page orders its groups itself. Package 2's: the 404's sentence with several
  placeholders, none of it on an endpoint with exposure rules, and OCI's own codes kept (Q-REPO-400); a member list
  may not hold `npmjs` and `upstream/npmjs` at once, because Debian's member paths, Ansible's role ids and OCI's merged
  tag list name a member by its key (Q-REPO-401); the usage row names the team's virtual repository only for a
  request a member of `upstream` served (Q-LIM-75). Package 3's: the entry's icon in the admin
  area's side bar (Q-UI-838); a row's level as a select with the level's icon beside it (Q-UI-839); what a reader
  who may not change a virtual repository learns of a registry's state (Q-UI-840: the presets and **Off**; whoever may
  change it reads the exact state); which settings of a well-known repository have a field on its settings
  page (Q-UI-842: the vulnerability settings and the cache-fill action stay with the API); the creation form opens
  at the class its address names (Q-UI-843); the project shows no level of its own (Q-UI-844). Package 1's second
  stage: Q-REPO-390 to 392, Q-AUTHZ-305, and the two shapes Q-API-363 and Q-API-364, of which
  Q-API-364 decides who may read the state and level of the well-known repositories without the admin read: the
  holders of `repositories:write-config` and the Owners of any project; a repository of `upstream` is listed and
  suggested only while it serves (Q-AUTHZ-306); what the reconcile creates where the level Internal is restricted
  (Q-REPO-393). Q-REPO-388 is the owner's since 2026-11-20 (off and Internal).
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
- The malware scanning (2026-11-20, its planner's decisions beside the owner's fifteen): Q-MAL-20 to Q-MAL-36, Q-REPO-410
  and 411, Q-OCI-240, Q-ARCH-560, Q-EP-95, Q-AUTHZ-340, Q-LIM-80, Q-HA-360 to 362, Q-BAS-164, Q-API-370, Q-UI-870 to 872.
  The ones to look at first: who releases a find (Q-MAL-21: `vulnerabilities:triage` together with
  `content:metadata-read`, so the instance auditor and the administrator, and not the security administrator, who reads
  no repository; it is the reading of the owner's note at decision 5); a virtual repository is strict when it or the
  member that holds the item is (Q-MAL-32); switching a repository's setting off is a weakening an Owner cannot do
  (Q-MAL-31); no report mode and no serving when the scanner is down, switching off is the way out (Q-MAL-36); under
  the switch every remote fill is stored and scanned before it is served (Q-MAL-30); what is not scanned: generated
  items, a remote's metadata documents, the system repositories of `upstream`, blobs without an item (Q-MAL-22). Marked
  unsure by the planner: the scanner loads its database through a route that admits it by its certificate's name
  (Q-MAL-27, which rests on the certificate for each kind of pod); the ClamAV image of the feature line and not the
  long-term one (Q-MAL-33); the sizes of the malware scanner (Q-HA-360 to 362: two replicas of two slots).
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

- **F-165** one instance of a pooler without a server after a switchover (33.7 s on 2026-11-20, 69.4 s on 2026-11-21): cause known since 2026-11-21, a fix waits to land (section 8).
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
