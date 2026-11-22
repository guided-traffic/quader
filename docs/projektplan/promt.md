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
     **Planners and researches do not count against the four** (owner, 2026-11-22, 14:36: "Ja, Planen under Recherchen
     zählen nicht gegen die 4", after he had asked where the rule comes from). The four are builders: agents that run
     tests, the integration stack or a kind cluster. Whatever carries a builder's test load counts as one, whatever it is
     called, and a long integration run of the development agent weighs like one (measured that minute: load 16 to 21 on
     20 cores with three builders, one planner, such a run and 19 containers). Whose number the four was, his or the
     proposal's he chose on 2026-11-08, could not be shown: his recorded words do not hold it.
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
- **Alone from 2026-11-21, 11:17, until he is back: features only** (owner): "ich muss dann jetzt gleich los und du musst
  alleine weiter machen. Gibt es noch etwas dass ich schnell beantworten muss? Bitte setze einfach nur noch die Features
  um, keine Begleichtung vom Nachtlauf. Nur fertig bauen und alles immer dokumentiert halten." The development agent builds
  what is decided, in the order of the run record and of "With the owner", keeps this plan current in the same step as
  every start, landing and finding, and follows every release to his installation (rule above). It neither dispatches nor
  attends the nightly; a red job of a run of its own pushes stays its to fix. The timer of the rule above is set, every 30
  minutes. **What was open with him and how it is built meanwhile, told to him before he left (he was told
  that none of it needs his answer):** what "betrieben" covers of the three ways to a signature: the tests now, real
  signing services on his cluster only when the first stage runs there; a virtual npm repository's keys endpoint serves
  the union of its members' keys (Q-SIG-34, option a); client certificates reach Quader behind his ingress through an
  endpoint of its own with the company's CA, and a valid certificate counts as a reader of the level internal (to be
  specified); the second and the third stage of the signatures are specified and built after the first without asking,
  open points decided by the development agent and documented; the three orphaned Secrets on his cluster stay until he
  deletes them or says so.
- **The walk's items are built by subagents at high effort or above, at most four at a time, each in its own worktree, with
  no review agent after them** (the owner's frame for the walk of 2026-11-18, with rule 10); the development agent looks at
  every screen itself before it lands. **Which model a subagent runs on is the owner's to say** (2026-11-20: not the largest
  one; a subagent that has just started on another is stopped and started again, one that is nearly done may finish). Rule 1
  keeps the model's name out of the repository; it stands in the development agent's notes.
- **After every release the newest version runs on the owner's cluster, and that is the development agent's task**
  (owner, 2026-11-21: "Aktuell kann die Version von Quader auf meinem K8S nicht von Flux geupdatet werden … Es ist noch
  eine Testinstallation ohne Inhalt, eine Löschung der Inhalte und DB ist also im Moment noch nicht tragisch. Es ist deine
  Aufgabe nach jedem Release dafür zu sorgen, dass die neuste Version auf meinem cluster landet."; before it, on
  2026-11-08: "halte die Version aktuell und immer am laufen"). His installation follows every release by itself, through
  the chart version `*` in his deployment repository. The development agent reads its state after `Release Docker & Helm`
  of every release (step 6, item 5) and says which version runs. A change of the chart that his values do not satisfy
  is the development agent's to bring there: his values are rendered against the new chart before the landing, and the
  change of the installation's directory in his deployment repository is committed and pushed by the development agent
  with the release, not handed to him as a patch (confirmed by him the same morning for that directory: "Ja, du darfst
  in apps/quader commiten."; for the rest of that repository its own rule stands, that an agent asks him before a
  commit). The cluster itself is changed through that repository only. A reset of the installation's content or database, where an upgrade needs one, is not
  tragic at this stage (his words) and is named to him first.
- **A unit that runs a tool carries the tool's name** (owner, 2026-11-21, after he saw the two scanner Deployments on
  his cluster: "Die Deployments quader-scanner und quader-malware-scanner sind mir nicht differenziert genug.
  quader-scanner ist trivy? Dann quader-vuln-scanner-trivy. quader-malware-scanner hätte ich auch lieber
  quader-malware-scanner-calmav genannt. Also immer so benennen, dass man sieht um welches Tool es sich handelt und das
  man später evtl mal mehrere Tools nebeneinander laufen lassen kann."; the tool is ClamAV, the name `clamav`). A kind
  of pod that exists to run one third-party tool, and everything named after it, is named
  `<purpose>-scanner-<tool>`: `vuln-scanner-trivy`, `malware-scanner-clamav`. The boundary is the development agent's
  and was told to him: what is the same whichever tool does the work (a job, a queue, the gate, a verdict, a setting,
  the role `scanner`, the worker's one link per purpose) keeps the name of its purpose.
- **The repository of the runners' image takes no commit without the owner's word** (`container-images`, its own `CLAUDE.md`;
  he released one commit and push for the image on 2026-11-19): ask again each time.

## 1 · Find the next phase

1. `git status` must be clean; then `git pull --rebase origin main`.
2. During the alpha (rule 10) a run works on two fronts at once: the first open task of
   [section 10](#10--owner-tasks-of-2026-11-07-the-next-registries), cut into work packages with an agent and a branch each, and
   every open finding of section 8 with an agent each. Open there now (2026-11-21, 06:30): no registry task; the malware scan of every artifact with ClamAV, decided by the owner on 2026-11-20 in fourteen answers, is landed in all four packages ([With the owner](#with-the-owner)); the role `scanner` with a certificate for each kind of pod and the analyser's database in a repository are landed (section 11); the built-in project `upstream` is landed; the list [With the owner](#with-the-owner) and section 12; then the next unit of item 3. The open items of BETA.md phase 7 are
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
- **What a builder's brief carries** (collected on 2026-11-21, each line from a red job or a defect of that
  day; the run record has the cases): no agents of its own; its own processes are stopped by PID or by a
  pattern that holds its worktree's path; a kind cluster gets a name, an image tag and a kubeconfig file of
  its own; a worktree is cut from the pushed `main`, so a base that is merged and not pushed is reached by a
  fast-forward to its hash, which the brief names; a migration is numbered at the landing, gapless, and runs
  the integration areas `postgres` and `upstream`; a change of what a task, a job, a statement or a registry
  walks, counts or lists is followed by a search for that name under `test/integration`, and the areas found
  are run; a unit test's cost is measured in the unit job's mode, and a test of the API's package uses the
  shared parse; the copy of the chart's values file equals the chart's; a new method of the format contract
  joins the developer page's lists; a UI change is rendered at every width of the layout job on a stack of
  its own, and the translation source is extracted as the last step; the linter gets a stated time limit;
  **a specification is landed like code** (the chart tests and the full unit run beside the evidence's checks and
  the count test, because the specification's blocks are inputs of tests: two landings of 2026-11-22 without
  them left the chart gate red for two and a half hours); the evidence record is written by the pipeline and
  never by hand; **the development agent's own gates and integration runs use a worktree of their own, never a
  builder's,** because a builder that is resumed edits its worktree (on 2026-11-22 fourteen integration areas
  of such a run failed to build in the minute a resumed builder changed a signature, and three before them
  ran on a tree in the middle of an edit);
  and **a reader of a third party's format is proven with a fixture the real tool made, before it lands**
  (the measurement of the signatures with real clients found three defects that every test made in process
  had passed).

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
5. Read the owner's installation (standing rule of 2026-11-21): the chart version its release reports, every
   Deployment ready on the new image, the certificates ready, an answer through its ingress. An upgrade that was
   refused or failed is the development agent's to resolve at once, in his deployment repository or in Quader.

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
  15.1 s under the bound of 20). **Landed on 2026-11-21 at 03:56 and seen on a cluster** in the leg dispatched after it (run 44862237399,
  green): the operator took the parameter, all four instances carry `server_connect_timeout 5`, the windows were 7.6
  to 10.5 s. No connect hung in that run, so the 5 s themselves were not exercised. What remains by design: an idle instance turns its next client away once,
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

- **The several connections between their first package and the ones that follow** (found on 2026-11-22 by the
  builder of package A while it corrected the documentation against the code; none of them touches an
  installation with one connection whose identifier is `default`, which is what the owner's is; the manual's
  page of the limitations says each): (1) **four alerts of the chart read series nobody exports yet** (the
  backlog, a damaged copy, the scrub behind, a detached connection), and the alert of a damaged copy points
  to an API that does not exist yet; silent until the reconciliation, the scrub and the surface are built,
  each of which takes its lines out of the chart tests' list. (2) **The journal is shipped to the first
  connection that is not draining, while the restore, the step after a restore and the backup read the first
  element even when it drains;** with `drain: true` on the first element the two differ, so a backup can lack
  new blobs and a restore can miss the journal. Repaired by the rest of package E (the backup reading one copy
  per blob, the journal per connection). (3) **The restore's stand-in records the restored copies on the
  connection `default`,** so a restore into a configuration whose first element has another identifier leaves
  the blobs on a detached connection, unreadable. The rest of package E. (4) **The tasks `scrub` and
  `storage-reconcile` are in the default maintenance plan without a job kind,** so nothing runs: between
  package A and the scrub's package no job reads the stored content for damage (the old deep check went with
  A). The reconciliation (in work) and the scrub's package.
- **Comments and two test names still say `storage.s3`** after the several connections' first package (found by
  its builder on 2026-11-22 and left, because the files belong to packages that are being built: the journal,
  the backup, the operator's verify and restore of versions, a subtest of the TLS area, one npm test and one
  end-to-end test). No message a user sees. Each package corrects its own files; what is left goes into the
  batch.
- **Two commits of 2026-11-22 added a line each to the evidence record by hand** (`docs/coverage/implemented.txt`: the
  kit's check for the Bastion and the declaration for signed links), against the developer page of the evidence,
  which says the pipeline writes that record from the test reports and nobody else does; found by the builder of
  the Bastion's package C, who left the file alone. Both lines are true (their tests exist and pass), so the
  pipeline's own check holds. The cause is the development agent's wording in the builders' briefs ("the evidence
  follows the code"); the briefs since 12:05 say that the record is not edited by hand, and the builders at work
  were told. Nothing to repair in the tree.
- **The Bastion, read on 2026-11-22 by the planner of "a Bastion serves every format"** (in the code, nothing run):
  (1) the specification said a Bastion serves no Debian, and the code forwards every path below `/api/<format>/`,
  so Debian, Ansible Galaxy and the ClamAV definitions reach clients through a Bastion today, while the feed has
  no entry for their withdrawals: a deleted Debian package or a removed collection version stays servable from a
  Bastion's cache until its revalidation, up to ten minutes by default (a malware finding has its entry and is
  not affected); (2) the client setup offers a Bastion as the target of an OCI repository, and its snippets get
  a 404 there. **Both are repaired on 2026-11-22** (the feed's entries by package B at 12:31, the registry's
  paths by B and C at 13:35); package D checks the client setup's snippet. A third finding of that day, that a
  hit from a Bastion's cache carried none of the content headers every other answer has (H-1250), was closed
  by package C with a test.
- **The object store, read on 2026-11-22 for the owner's storage topic** (by a research, in the code; the first point
  checked by the development agent): (1) **the scrub of stored content and the orphan sweep of the content addresses are
  not built** (R-STOR-027, R-STOR-026; no job kind, no list of damaged blobs; the developer page of the blob store says
  so), while the evidence lists R-STOR-027 as implemented, through the claims of its part about a lost object in the
  remote cache; the scrub is what the gap H-11 names as its mitigation, so a changed byte in the bucket is found today
  only when a full download reads it; (2) a repository's `storage.presignedDownloads` is stored and has no effect, also
  on a remote repository that keeps nothing, where R-STOR-014 wants a 422, and `storage.s3.presign.ttl` and `min_size`
  are read by nothing; (3) the object store's own encryption (R-STOR-032) is built on every write and copy, is not
  marked implemented, has no test that names it, and the chart reaches it through the configuration block alone;
  (4) the journal's shipper uses the plain client and not the guarded one, so it passes the circuit breaker and the
  metrics by; (5) the finalize of an upload session checks its server-side copy by size alone (the gap H-591, known).
  None is repaired; (1) and (2) belong to what the storage topic decides, (3) and (4) wait for a batch.
- **The signatures, measured with real clients on 2026-11-21** (confirmed by reproduction; commands and outputs in
  [developer/signatures.md](../developer/signatures.md#defects-found)): (1) no real NuGet signature is readable, neither of `dotnet nuget sign`
  nor of nuget.org, because both name the signer by `subjectKeyIdentifier` and the parser of `nugetcms` takes only issuer and serial; every
  NuGet signature reads **Scheme not supported** and a gated repository refuses every signed package; (2) in a gated OCI repository
  `cosign sign` and `notation sign` are refused on their read of the manifest, so an image pushed there cannot be signed there (needs a
  decision of what passes); (3) a signature posted for a new Ansible version is deleted from the store by the worker's discovery, which
  replaces the attachments of the archive's item, while the version document keeps serving it; (4) a remote OCI image's version view lists
  none of the signatures its fetch stored; (5) `dotnet restore` and `ansible-galaxy` never show the gate's text, and Docker's pending pull
  shows none either; (6) the text of `missing` says "it has no signature" for a package that carries a registry signature; (7) the texts
  name storage paths such as `unsigned/_tags/1.0`.
- Three narrow lists of the signatures' screens (the security dashboard's view, the admin page's project section, a
  project's anchors) stand as blocks even at 1,440 px, as other narrow lists do; the layout checks accept it (found on
  2026-11-21 by the builder of the signatures' screens, left for the owner's look).
- The script that picks the end-to-end legs of a change (`e2e-legs.sh`) starts the npm leg for a change of the analyser's or
  ClamAV's image pin, which loads neither image, and not the leg `vuln`, which loads both. The restore drill's values
  (`deploy/dev/values-restore.yaml`) leave malware scanning on by the chart's default, so the drills run ClamAV and its
  updater, which reaches ClamAV's network, against Q-MAL-90. (Both found on 2026-11-21 by the builder of package 3 of the
  analyser's move, left.)
- In the integration stack a global read policy seeded for an OCI repository answered `deny.no-grant` to a token of the
  default scope; the test of the gate's releases uses a project's reader instead (found on 2026-11-21 by that package's
  builder, the cause not looked into: a fault of the setup or of the policy's evaluation).
- One subtest of the integration area `npm` that stops Valkey got 503 `database_unavailable` at a host load above 30
  while the replica's readiness checks timed out, and passed alone (seen once on 2026-11-21 by the builder of npm's
  signatures; the same area's migrations had timed out for the gate's builder at a load of 80 that day).
- The developer page on secrets (`docs/developer/secrets.md`) still lists the task `secrets-reencrypt` under what is
  deliberately not there and calls the API's steps work of a later milestone; both exist (found on 2026-11-21 by the builder
  that repaired the task's test, left).
- A run of Test and Release stood in the queue for 17 minutes with no job offered to the runners (the push of 2026-11-21,
  23:44): both scale sets' listeners polled without an error and were told of no job, the hosting service's status page
  showed no incident, and one listener had logged a retried request to the service's broker in the minute of the push.
  Cancelled and started again on the same commit at 00:01, the run got 24 runners within seconds. Not looked into
  further; nothing in the runners' cluster was touched. If it recurs: the listeners' logs of the push's minute.
- Two fuzz targets of the nightly's NuGet fuzz job carry a requirement's code in their names
  (`FuzzR_ARCH_028_Inspect`, `FuzzR_NUGET_005_Nuspec`), so two job names and two artifact names do, against the rule that a
  job's name says what it does (found on 2026-11-22 at the landing of two new targets, which were renamed; left). The fuzz
  tool's default of a minute for shrinking each new input stalls a target whose seeds are tens of kilobytes (175
  executions in two minutes against 565,507 with five seconds); the make target of the fuzz legs does not set it.
- `make lint` states no time limit, so the linter stops at its own ten minutes on a loaded machine after it has printed its
  result (seen twice on 2026-11-21 at host loads of about 70, by a builder and at a landing; both passed with
  `--timeout 40m`). The cure is a stated limit in the target, as the unit run got one.
- The UI's tests that choose a person wait a fixed 320 ms for the search's debounce (`choosePrincipal` of the test
  helpers) and fail on a loaded machine; they passed alone each time (seen by three builders on 2026-11-21 at host loads
  of 60 to 120, left). A comment of the UI development profile still says the role `all` calls its own image scanner.
- The image scanner's spool writes of large layers may be killed for memory by dirty pages of the file cache, as its
  database's loader was before it flushed every 32 MiB (not measured; found on 2026-11-21 by the builder of the
  analyser's move, left). A scanner's row of a pod removed in a simultaneous rollout stayed "ready" until it was pruned
  (the same builder, not looked into).
- The client setup of the ClamAV definitions: its table of troubleshooting is squeezed at 390 px, and one sentence shows the
  address with the trailing slash it tells the reader to leave out; the usage overview's page of one caller lists no
  credentials for a certificate holder (found on 2026-11-21 by the builder of the client certificates, left).
- The decisions' index (`docs/decisions.md`) has no rows for the decisions of the signatures' second stage (Q-SIG-53 to
  Q-SIG-74); found on 2026-11-21 by the planner of the third stage, left. Nor for the signing identity's (Q-SIG-145 to
  Q-SIG-155; found the same day by the gate's builder, left). (The index also stops at Q-UI-844, an older entry of this
  list.)
- A ClamAV inside the cluster that names the definitions' endpoint by a host the endpoint does not accept gets 421, which
  `freshclam` counts as a failed attempt without a cool-down; the client setup does not say which host to use from inside the
  cluster (found on 2026-11-21 by the builder of the ClamAV import's measurement, left).
- The German text of **Well-known registries** spells the plural "Registries" where the glossary says "Registrys" (found on
  2026-11-21 by the builder of the presets, left).
- The hint under **Upstream URL** still says "Choose a well-known registry or enter the address of another" when the form
  offers the installation's registries too (found on 2026-11-21 by the builder of the presets, left).
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
- **Twelve files of the integration tests start a quader process with the whole environment of the caller**
  (`append(os.Environ(), …)`; found on 2026-11-21 by the builder who mended the lock script, which had tripped
  over exactly that). The product refuses a variable with the prefix `QUADER_` that is no setting and reads one
  that is, so a variable exported in a developer's shell or by a wrapper either ends every such process at start or
  changes its configuration without a word; the names seen in the repository that would do it are
  `QUADER_PERF_GATE`, `QUADER_IT_NO_DOCKER` and `QUADER_DEB_PROOF_DIR`. Dormant: nothing in the repository exports
  one around those tests today, and CI sets none. The tests should start their processes with the variables they
  name; not fixed.
- **The task `malware-sync-db` fails every 30 minutes where the scanning is on and nothing fetches a database**
  (seen on 2026-11-21 at the start of the UI development backend: "job failed … database_missing: no [main daily]
  container: the set needs main and daily", at the level of an error). A start without a scanner keeps the scan
  job idle and says so in a check that does not gate (Q-MAL-60); the database's task has no such state, so the
  UI stack and any start outside the chart that keeps the defaults (the scanning on, the task every 30 minutes)
  log an error at every tick and record a failed run. An installation of the chart has `freshclam` beside its
  worker and is touched once: on the owner's installation the task's first run came two seconds before `freshclam`
  began its first update, failed with that error and an audit event `malware.database.failed`, and the next
  run 16 s later imported the database. Not looked at: what **Settings › Malware scanning** shows for it on the UI stack.
  Not fixed: either the task is idle while its directory was never filled and no scanner is
  named, or the UI profile switches the scanning's database task off as it does for the image database
  (Q-VULN-195).
- **`freshclam` logs an error at every update: "NotifyClamd: Can't find or parse configuration file no"** (read on
  the owner's installation on 2026-11-21). The chart's configuration says `NotifyClamd no`
  (`deploy/helm/quader/templates/_malware.tpl`, named so in R-HA-069), and the option takes the path of `clamd`'s
  configuration, not a switch: `no` is read as a file name. The update itself succeeds. The line has to go from the
  rendered configuration and from the requirement's list; not fixed. **Taken up by the ClamAV change** (the
  specification of 2026-11-21 renders no such line; its chart package is in work), as is the finding above of the
  task that fails where nothing fetches a database (the task is idle there and waits for a first set).
- **A disabled repository's file page offers Download, which answers 404** through the content route (found on
  2026-11-21 by the builder who removed the same for system repositories; the same kind of defect, not fixed).
- **A version page's header shows the format's key (`oci`) instead of its name** (OCI (Docker)), on every version
  page (seen in the renders of 2026-11-21; not fixed).
- **The index of decisions in `docs/decisions.md` stops at Q-UI-844;** the UI decisions taken since are in the
  specification but not in the index (noticed on 2026-11-21; not fixed).
- **`repository-content.scss` is 227 bytes over its 4 kB warning budget** (a warning of the build, not its 8 kB
  error; 170 bytes of it older than the change of 2026-11-21 that added 57).
- **The server still hands out addresses and client setup for a system repository** (the content and download
  addresses of the API's answers, the client setup operation); the UI leaves them out by the repository's name
  (Q-UI-898, whose other option is the server's answer carrying the repository's kind). Read, not fixed.
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

**State on 2026-11-21**, 06:30, after the landing of the malware scan of every artifact; the role `scanner`, the analyser's database in a repository and the fixes of the nightly landed in the night before it.

- **Landed and released on 2026-11-20** (the last release cut is the one of `a4289d7e6`, on 2026-11-21 at 06:29, with the malware scan of every artifact): the measurement of what a format
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
  the UI profile). **Its pipeline (run 44858533514) ended green on every job at 04:39:** the three integration
  shards, `UI layout checks`, the source tree's malware scan, and the legs `e2e (ansible)` (the changed limits case's
  first run on a cluster), `oci`, `vuln`, `npm`, `push`, `ci` and `access-long`. **The legs the fixes are for were
  dispatched alone at 04:39 and all ended green at 06:04** (run 44862237399: the four of the platform matrix,
  `drain-roles`, `drain-load-e`, `ha-faults`, `ha-pooler`; each ran its steps, none was skipped). In `ha-pooler`
  every instance of both poolers shows `server_connect_timeout 5` in its configuration, and after the switchover
  the four instances were without a server for 7.6, 8.1, 8.9 and 10.5 s against the bound of 20 s; two of them
  turned one or two clients away later, which the test names and does not count (Q-HA-370). One green run each:
  it shows the fixes do what they were built for, not that the latency of `drain-load-e` (F-160) or a hung
  connect (F-165) cannot return. **The owner's installation** showed the upgrade to the new chart refused by the chart's schema, as
  foreseen, and kept serving the release before the scanner role, waiting for the change of its values that was
  given to the owner as a patch on 2026-11-20 at 20:19. **That was the development agent's to bring there, not his**
  (the owner on 2026-11-21, standing rules): ten hours on an old version were the development agent's miss. The
  change was committed and pushed in his deployment repository at 06:22 (the issuer in place of `tls.secretName`, no
  `caSecret`, his hand-written `Certificate` removed), rendered before against the released chart (28 objects, four
  certificates) and against the chart of `main` with the malware scan (35 objects, five certificates, five
  Deployments). **At 06:25 the upgrade had succeeded:** the release reports the newest chart, the four certificates
  of the chart are ready, two pods each of `api`, `packages`, `worker` and `scanner` are ready on the new image, the
  migration hook completed, and the ingress answers (the sign-in redirect on `/`, 401 on the API without a session).
  The old Secret has no owner reference and stayed, so the old pods kept their certificate while they were
  replaced. Not measured: requests during the rollout. Read on the cluster for what comes with the malware scan:
  three nodes of 16 cores and about 61 GiB each, at a quarter to a half of their memory, no quota or limit range in the
  namespace, amd64; the worker carries the label for the way to the internet that `freshclam` needs.
- **The malware scan on its landing branch, 04:17:** `feat/malware` is `main` with the stack's lock fix and the eleven
  commits of the feature on top (rebased by package 3's builder onto what landed, each conflict settled in the commit
  it arose in; one break without a textual conflict found by the area `malware` and fixed: a test set a configuration
  key package B removed). The page of the upstream registries shows both system repositories in use, looked at by
  the development agent at 1440 px dark and 390 px light. **Gates of the development agent on the tip, each exit 0:**
  build, the full unit run (176 packages), generate-check, chart lint and tests, links and check, the UI tests
  (1,803), the UI lint and layout tests, lint, cyclo, gosec; the commits and the diff scanned for names that may not
  appear. **The first run of every integration area on that tip, through the new lock script, was stopped at 04:40
  after 18 areas: the script itself broke three of them.** It exported a variable `QUADER_IT_LOCK_HELD` to know a
  nested call, and the product refuses every environment variable with the prefix `QUADER_` that is no setting, so
  every test that starts a quader process inherited it and the process ended at start ("unknown environment variable
  QUADER_IT_LOCK_HELD"): `backup` (four tests), `bastion` (nearly all), `bastionreg`. The script's author had proven
  it with the area `coordination`, which starts no process. Nothing of the feature. **The lock script's commit is
  taken off the landing branch** and mended on a branch of its own (a builder, since 04:41; it lands by itself when
  it has run the process-starting areas); the landing branch is `main` and the eleven commits, its tree the builder's
  rebased tip. **The run of every area started again at 04:41**, with the lock taken by hand and nothing else on the
  stack, **and ended green at 06:10: all 67 packages that hold tests passed in one run, none run again**, the areas
  `coordination`, `backup`, `bastion`, `bastionreg`, `querybudget`, `malware` and `files` among them, each of which
  had failed in an earlier run of that night for a reason outside the feature. The feature lands on that.
- **The malware scan of every artifact, landed on 2026-11-21 at 06:15 and released at 06:29** in eleven commits
  (the branch rebased onto the plan's commit of 06:11, its tree the tested one but for the plan; links and check
  once more): the screens; the gate; the scan listener's two routes; the interface between scanner and loader; the
  job and the scanner; the database's task, `upstream/scanner-clamav` and the chart; the put-back of a refused
  database; the page of `scanner-clamav`; the loader; the alerts and the sizing; the statement budget's test.
  **Before the push the owner's UI backend was started with it from the main checkout:** ready, the schema at 89
  with nothing to apply (the feature's one migration landed with its first stage), `upstream/scanner-clamav`
  created, the checks `malware_scanner` and `scanner` failing without gating, as Q-MAL-60 and Q-ARCH-551 say for a
  start without scanners. **Seen at that start, a finding (section 8):** the task `malware-sync-db` fails there
  with an error line, because the scanning is on by default and that stack has no `freshclam`.
  **Its pipeline (run 44869974792):** the jobs that gate the release were green and the release was cut at 06:29; at
  that time the three integration shards, `UI layout checks` and the legs `npm`, `oci`, `ci`, `push`, `access-long`
  and `vuln` (the first run of the malware case and of the ClamAV image on a runner) were still running. **It ended
  green on every job at 06:58**, those ten among them; `UI layout checks` ran the malware screens' render cases for
  the first time on a runner.
  **The owner's installation took the release by itself at 06:37**, three minutes after the chart was published
  (read on the cluster, nothing changed there): the release reports the new chart, the fifth certificate is
  ready, two pods each of `api`, `packages`, `worker`, `scanner` and `malware-scanner` are ready on the new image
  without a restart, and the ingress answers. The chain of the signature database ran there for the first time
  outside a test: `freshclam` beside the worker fetched `main`, `daily` and `bytecode` from ClamAV's servers (his
  cluster's policy lets it), the task kept them in `upstream/scanner-clamav` 10 s later, and both malware scanners
  loaded that version into `clamd` within 18 s after it. On amd64 `clamd` holds 958 MiB with the database loaded
  (requested 1,280 MiB, limit 2 GiB), the `quader` container beside it 26 MiB, `freshclam` 10 MiB. **Two findings
  from its logs, neither a blocker (section 8):** `freshclam` writes an error line at every update for the option
  `NotifyClamd no`, and the database's task failed once at the first start, two seconds before `freshclam` began
  its first update. **Not verified:** a scan of a real artifact there (the installation holds no content); the
  loader's refusal path in a cluster; H-1141; the image database's fetch on that installation (not looked for).
- **The stack's lock and the coordination tests (one commit of the type `test` on the branch `fix/it-lock`, mended
  at 04:44, see the malware scan's entry above for what it broke; its four areas are green, landed at 07:00):**
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
  end-to-end queries on a cluster. **The mend:** the script's two variables are `QUADER_TEST_LOCK_HELD` and
  `QUADER_TEST_STACK_ROOT`. The prefix `QUADER_TEST_` is the one the check of the environment lets through for the
  integration suite (`reservedEnv` in `internal/config/load.go`), and the Bastion runs the same check. Shown
  without the stack: a binary built from the branch ends under the first script with "unknown environment
  variable", and under the mended one `admin config validate` passes and the Bastion gets past its check of the
  environment. **Run through the mended script on the stack at 06:10, one call, exit 0:** `bastionreg` (27.5 s), `backup`
  (316.6 s), `bastion` (120.5 s) and `coordination` (238.8 s), the first three being the ones the first script
  broke. A call of the script inside the script passed too (`coordination`, 238.5 s): the inner call found the lock
  it already held and did not wait for itself. Rebased onto the malware scan's landing without a conflict; on that
  commit build, vet with the integration and the end-to-end build tags, `shellcheck` and links are green. **Landed on 2026-11-21 at 07:00 as a commit of its own**, after the malware scan's pipeline had ended (a push
  before that would have cancelled the legs still running), with the full unit run before the push; no restart of
  the owner's UI backend, because the commit holds no product code. **Its pipeline (run 44873483199) ended green on every job at 07:45**: the
  three integration shards with the changed queries, and the legs `push`, `access-long`, `npm` and `ci`, three of
  whose tests the commit touched. The pipeline's integration shards do not call the script (it is the way of a
  developer's machine and of agents on the shared stack), so its `flock` path on Linux stays unverified. Still not verified: the `flock` path on Linux; two runs competing for the lock on
  the real stack with the mended script (shown on scratch files, and by the first author).
- **Landed on 2026-11-21 at 10:02: every scanner pod on the screens, with its tool, its version and its state**
  (R-OPS-048, Q-OPS-223, Q-API-381, Q-UI-897, ADR-224, H-1161; the builder had reported at 09:49 with one commit and
  its own gates each exit 0, among them the integration areas under the lock and the end-to-end set `vuln` on a
  cluster of its own). On the commit as rebased onto `main`, each exit 0: build, the full unit run,
  generate-check, links and check, the UI tests (1,824), the UI lint, the chart's lint; the renders looked at (the
  System page with both kinds and a pod that is starting with its reason, the malware scanning settings with the
  fold of the ClamAV pods, the advisory sources with the image scanners; light and dark, wide and narrow); the
  owner's values rendered against its chart (35 objects; the one new key is set by the chart); the owner's UI
  backend started with it from the main checkout before the push and answers ready (the schema step of the
  scanner rows applied). Its worktree and branch are removed. **Its release was cut at 10:16, image and chart were published at 10:22, and the owner's installation runs it since 10:24** (read on the cluster: the release reports the new chart, five Deployments with two ready pods each on the new image, no restart, the ingress answers; every scanner pod's first report during the rollout was answered 404 by a `packages` pod of the release before, logged as a warning, and reached the listener ten seconds later, which is the upgrade case the builder had not tested). **The landing's pipeline (run 44887645066) ended green on every job at 10:45.** **Not verified, by the builder's
  report:** the German screens in a browser; the mode without TLS beyond unit tests; an upgrade in which old
  `packages` pods answer 404 to a scanner's report; H-1161 names what a taken-over scanner can make an
  administrator read (rows of its own kind only, up to the cap, no markup, nothing decides on them). **What it built, by its report:** every scanner pod reports itself on the
  scan listener (its kind taken from the verified certificate, never from the body), the rows live in a table of
  their own beside the replica registry and follow its rule for lost rows, a read operation lists them, and the
  System page, the malware scanning settings and the advisory sources show each pod with its tool, version, state
  and the reason it is not ready. **An incident of its work, 09:44:58:** it stopped its own render stack with a
  process pattern that was a part of the owner's UI backend's path too, so the owner's backend and its sign-in
  provider stopped; the development agent found it by the backend's exit, started both again from the main
  checkout (at 09:47, answering ready) and told the builder, which has since stopped its processes by their IDs. **For
  every builder's brief from now on:** a builder stops its own processes by process ID or by a pattern that
  holds its worktree's path, never by a pattern that can match the main checkout. **Two more, learnt at the
  landings of 12:55:** the migrations must be numbered without a gap (three tests hold it), so builders that
  work in parallel get numbers that are set anew in the order of landing; and the linter holds one lock per
  machine, so a second run answers "parallel golangci-lint is running" and is run with the option that
  admits parallel runners. **The pipeline of the 09:05
  landing (run 44883240572) ended green on every job at 09:55.**
- **The planner of the image analyser's move into the scanner unit and out of the common image (11:21 to 12:06)
  is done:** its specification is on `main` since 12:07 ("With the owner" holds what it says and what was
  measured; rebased, its two gaps renumbered behind those of the client certificates, links and check exit 0,
  the count test at 1,110 requirements); its three packages are built after the ClamAV change is on `main`,
  because both change the chart and the scanner role. The chart's builder of the ClamAV change had taken a
  decision number the planner took too; the chart's is Q-HA-384 on its branch now. **The planner of the ClamAV endpoint's client certificates (11:20 to 11:39) is done:**
  its amendment is on `main` since 11:40 (rebased, links and check exit 0, the count test at 1,107
  requirements; "With the owner" holds what it says), its worktree and branch are removed; its one package is
  built after the ClamAV change's packages 1 to 4.
- **The planner of the signatures' second stage, Quader signs (12:09 to 12:59), is done:** its specification is
  on `main` since 13:10 ("With the owner" holds what it says, its four questions to the owner and what is
  built meanwhile; rebased, its decision numbers set behind those of the first stage's package 1, the decision
  blocks of both merged, links and check exit 0, the count test at 1,131 requirements); its worktree and branch
  are removed. Its four packages are built after the first stage, in the order it names.
- **The planner of the signatures' third stage, the gate per repository (13:25 to 14:01), is done:** its
  specification is on `main` since 14:02 ("With the owner" holds what it says and its three questions to the
  owner; rebased without a conflict, links and check exit 0, the count test at 1,149 requirements; it does not
  touch the chart's page); its worktree and branch are removed. Its three packages follow the first stage's.
  **Found by it and left (section 8):** the decisions' index has no rows for the second stage's decisions.
- **The pipeline of the ClamAV change's push (run 44910514928) ended at 15:29 with two red jobs, neither of
  which holds a release back; their repair landed at 16:20.** `integration (shard b)`: the area
  `packages` holds the clients the formats list names per format, and the new format `clamav` with its client
  `freshclam` was not in what the test expects (the ClamAV builders had run the areas `malware` and
  `upstream`). `UI layout checks`: the admin area's list of repositories scrolls 7 px sideways at 1,280 px in
  one state; the builders had rendered at 1,440 and 390 px, the job renders more widths, and the run before
  was green there. The purge test of the run before is green in this run (the repair of 14:10 holds). One
  builder repairs both in two commits, looks for further lists of formats that a new format must join, and
  writes both lessons onto the developer pages (the area to run for a new format; a screen is rendered at
  every width of the pipeline's job). **For every builder's brief from now on:** a UI change is rendered at
  every width the pipeline's job renders, and a new format runs the integration area `packages`.
  **The repair, two commits** (its builder worked from 15:30 to 16:16; the signatures' third package had
  landed before it, so its tree carried the two faults as well). The integration test: the formats list is
  right, the test's table of clients per format lacked the new format; it has it now, and the developer page
  for a new format names the table and the area. The layout: the cause was neither the new column nor the
  badge but the format's name, 153 px wide where the widest before was 126 px, so that the list needed ten
  pixels more than its place at 1,280 px; and a fault of the shared data table, which did not measure again
  when only the room inside its scroll region changed. Repaired: the repository column of the admin list
  gives one rem to the columns that never break, and the shared table measures again on that change (a test
  that fails without it). Because the table is every list's, the builder rendered every screen at every
  width of the job on a stack of its own: 421 passed, none failed, no layout finding (30 minutes). **Looked
  at by the development agent:** the admin list at 1,280 px, as it opens and with the filters open. **On the
  two commits as rebased, each exit 0:** build, the full unit run, generate-check, the lint, the complexity
  check, links and check, the UI tests and the UI lint; by the builder the layout checks and under the lock
  the area `packages`. **Its release was cut at 16:36**, image and chart were published at 16:41, and the owner's installation runs it
  since 16:43 (read on the cluster at 16:45: every Deployment ready). **The run of this push (44918638887) ended
  green on every job at 17:05,** the two repaired jobs and the three integration shards with the signatures'
  third package among them; it is the first run since the push of 12:55 that ran to its end, because each
  code push stops the run before it. The next landing was held for a few minutes for that reason. **Written down for the next formats** (the developer page): the one
  list that fails for a missing format (this table), two that do not fail and must be kept by hand, and the
  hand-written lists that need a decision per format. The pipeline's runner confirmed it: the layout job of this push's run is green.
- **Landed on 2026-11-21 at 15:45: package 3 of the signatures' first stage, the formats' wire behaviour**
  (R-SIG-023 to R-SIG-033; its builder worked from 12:56 to 14:43 and brought the commit onto `main` from 14:47
  to 15:40). **OCI:** deleting an image's manifest removes its referrers, its signature tags and the manifests
  only those tags name (one that another tag names stays; deleting a tag alone leaves image and signatures);
  evicting a remote's cached manifest takes the cached items of its signatures; for a remote repository a
  job of the worker fetches the signatures itself when a tag's manifest is stored (the referrers API once,
  then a HEAD of the two signature tags; the referrers tag only where the API answers 404 or 405; at most four
  signature manifests with eight layers each, never an SBOM's; again after 24 hours when a client revalidates
  the tag; at most 600 requests per remote, hour and worker); a mirror pass asks the same for each kept tag and
  mirrors no signature tag as a version; the image scanning leaves signature and attestation manifests out.
  **npm:** the keys endpoint for every class (a remote serves its upstream's keys, a local repository an empty
  list, a virtual one the union of its remote members' keys, which is Q-SIG-34 as option (a)); the attestations
  endpoint (a remote serves its upstream's document, a virtual one the first member that has it, a local one
  answers 404 until package 2 vouches for a bundle). **Ansible Galaxy:** a signature of a version can be posted
  and removed, checked without a key against the version's manifest, served in `signatures[]`, at most sixteen.
  **Debian:** which stored releases of a remote cover a package, for package 2's API. **NuGet and generic:**
  nothing beyond package 1. Not built, because the features do not exist yet: the trash, copy, retention.
  **Decided by its builder:** the fetch is a job of the worker, queued in the storing transaction, with a state
  row per target and an hourly budget per remote (Q-SIG-97); how it asks an upstream (Q-SIG-98); a mirror asks
  on every pass (Q-SIG-99); a client's revalidated tag queues the refresh (Q-SIG-100); an eviction takes the
  signatures' items (Q-SIG-101); a posted Ansible signature is checked without a key, and the permission is
  the collection's deploy and delete (Q-SIG-102); a local npm document names no attestations in this stage
  (Q-SIG-103); the image scanning's exclusion (Q-SIG-104); Debian's coverage is an optional interface of the
  format (Q-SIG-105). **Decided at the landing by the development agent, written by the builder:** one shape of
  the fetch's state table for this stage and for the third stage's text, with the subject, an attempt count
  and a failure time added, and the third stage's requirement rewritten to it; a row goes with its target for
  OCI (Q-SIG-106); posting and removing an Ansible signature writes an audit event each (Q-SIG-107); the
  manual promises no switch for a remote's fetch until package 2 makes the member settable (Q-SIG-108). **On
  the commit as rebased onto `main`, each exit 0:** build, the full unit run, generate-check, the lint, the
  complexity check, links and check, the UI tests and the UI lint (two labels of audit events); by its builder
  under the lock the areas `upstream`, `signatures`, `ocilocal`, `ociremote`, `npm`, `ansiblelocal` and
  `malware`. The owner's UI backend started with it before the push (the schema step applied). **Its release was cut at 16:00**, image and chart were published at 16:06, and the owner's installation runs it
  since 16:09 (read on the cluster at 16:15: two ready pods each of `api`, `packages`, the worker and both
  scanners and the updater's one, no restart, no error line of `api`, `packages` or the worker). **Its tests cost** under 1 s more in the unit job and, in the integration shards, about 4 s (`ocilocal`), 9 s
  (`ociremote`), 2 to 15 s (`ansiblelocal`) and 2 s (`npm`); no end-to-end case. **Open by its text (H-1201):**
  for npm a fetch's state row does not go with its target, so a tarball filled again is not asked again, and
  only the keys are refreshed, not a version's attestations. **Not verified:** a real `cosign`, `notation`,
  `npm audit signatures` or `ansible-galaxy` with a keyring against Quader; whether Docker Hub serves the
  referrers API and how it counts the requests; a registry that answers 400 for the missing referrers route;
  the path that counts a failed fetch has no test. **Left for package 2 to wire** (on the signatures'
  developer page): the verdicts that let a local npm bundle be served, the counter of requests to remotes, the
  configuration of the fetch and the repository member that switches it off. Its worktree and branch are
  removed.
- **The pipeline of the signatures' second package (run 44926599027) ended at 18:31 with one red job,
  `integration (shard b)`, which holds no release back; its repair landed at 19:27.** The area
  `postgres` holds the schema's conventions, among them that every counter column named as a revision, an
  epoch or a version is in the registry of revision columns; the package's migration added two such columns
  unregistered (the epoch a trust row was computed under, and the version of the update repository's snapshot
  a shipped root came from), and its builder had run nine areas, not that one. One builder decides for each
  whether it is a revision in the rule's sense (then it is registered) or not (then it gets a name the rule
  does not claim), runs the whole area, and writes onto the database's developer page that a migration runs
  this area. The two builders in work that add migrations were told. Every other job of the run was green,
  the end-to-end legs among them. **For every builder's brief from now on:** a migration runs the integration
  areas `postgres` and, for a table with a key to repositories, `upstream`. **A third such case today** (the
  purge test, the formats' client list, now this): each time a test that holds a list or a convention sat in
  an area the builder had no reason to run; the briefs name the three now, and section 12 gets the question
  whether such checks can run without the stack, in the unit job.
  **The repair** (its builder worked from 18:32 to 19:24, forty minutes of it waiting for the integration
  stack's lock behind three builders): neither column is a revision, so both are renamed and none is
  registered. The trust row's epoch is a copy of the settings' counter of anchors and is only ever compared
  with it; registered, a restore would raise the copy and not the counter and a row would look current for
  good; it takes the counter's name. The other is the version number of the update repository's snapshot, a
  number from outside; raised by a restore, the stored root would look newer than every snapshot to come and
  never be refreshed; it is named a version number. One migration renames both with their index and
  constraint; the third stage's text names its planned copy of the counter the same way, and the gate's builder
  was told. The database's developer page says that every migration runs the area `postgres`, and that a copied
  counter or a number from outside gets a name the rule does not claim. **On the commit as rebased onto
  `main`, each exit 0:** build, the full unit run, generate-check, the lint, the complexity check, links and
  check; by its builder under the lock the whole areas `postgres` (100 s) and `signatures`. **Released at 19:46** with the screens of the analyser's move, in one push; the owner's installation runs it
  since 19:53 (read on the cluster at 19:55: the schema job complete, so the renames ran on his database, which
  holds rows of both tables; every Deployment ready, no error line). The shard of its own run was not seen: the
  next push stopped that run, and the run of that push shows it. **Not verified:** the migration on a database that holds rows of the tables (the renames ran against the
  objects the earlier migration made, on an empty schema); old pods read the old names for the seconds between
  the schema job and their replacement at an upgrade.
- **Landed on 2026-11-21 at 18:33: package 1 of the image analyser's move, the scanner unit detects as well**
  (R-VULN-048, R-VULN-049 and its part of R-HA-070; what it built, measured and found is in the entry of the
  two builders of 15:16 below, as reported at 17:43; its builder brought it onto `main` from 17:43 to 18:06).
  **Decided at the landing by the development agent, written by the builder (Q-VULN-214), for the owner's
  walkthrough:** the upgrade removes the stored analyses and every image is analysed afresh by the scanner,
  because the stored ones are the worker's rewrite without a count of packages and each image would have
  shown 0 packages. The builder kept one point apart from the brief, with its reason: an image already scanned
  keeps its findings and is analysed afresh at its next detection, not queued at once, because a scanned row
  without its database would count as not detected and its pulls would wait under the switch that admits only
  checked artifacts. The price is one pull per image after the upgrade. **On the commit as rebased onto
  `main`, each exit 0:** build, the full unit run, generate-check, the lint, the complexity check, links and
  check, the chart's lint and tests, the UI tests and the UI lint; by its builder under the lock the areas
  `vulnimage`, `malware`, `coordination`, `signatures` and `ociremote`, and before the rebase the end-to-end
  set `vuln` on a fresh cluster. **The owner's values** render against its chart unchanged in number (39
  objects): each scanner pod gains the container `database` and the volume of the database (5 Gi). **Read on
  his cluster before the landing** (what the specification had left open): the three nodes that carry
  workloads have about 222 GiB of allocatable ephemeral storage each, so two scanner pods with up to 3.4 GiB
  of database each fit. The owner's UI backend started with it before the push (the schema step applied). **Released at 19:13** by the run of the unit job's repair (the entry above; the job `unit tests` of its own run had
  gone red at 18:48 and held the release back), image and chart were published at 19:20, and **the owner's
  installation runs it since 19:23.** Read on the cluster at 19:24: every Deployment ready, no restart, no
  error line; each scanner pod with its two containers. **Seen there, which the builder could not verify:** the
  loader of each pod took the real database through the scan listener (1,807,315,087 bytes in 8.1 s, after
  five warnings five seconds apart while `packages` was still rolling) and, two minutes later, the next one
  (1,809,780,879 bytes in 7.6 s), within its memory limit of 256 MiB and without a restart. **Not seen in the
  pipeline:** the end-to-end leg `vuln` on this change; its own run was stopped by the push of the unit job's
  repair before the leg ended, and that push's run did not start the leg (its change was the Makefile). The
  leg runs with the next change of the image scanning (package 3 of the move). **Not verified:** a detection against the real database in a pod; the worker's transaction that applies a
  detection; amd64; the migration's removal on a table that holds rows (the integration stack's are empty).
  **Left for package 3 of the move:** the analyser from its own image through an init container and out of
  the release image, the pin, the workflows, the development profiles; **package 2,** the database per
  scanner pod on the screens, builds on its report. Its worktree and branch are removed.
- **The pipeline of the analyser's move (run 44930739304) went red at 18:48 on the job `unit tests`, which holds
  the release back; the first part of its repair landed at 18:55 and its run is green there.** No test hangs: the unit run of the API's package as a
  whole passed the limit of ten minutes per test binary that the tool sets when none is given. Its time in
  that job (race detector and coverage over every package), read from the day's runs: 476 s in the morning,
  487 s at 17:06, 585 s with the signatures' second package (98 s more for tests its builder had measured at
  about 5 s without the race detector), and over 600 s now. The next slowest package takes 156 s. One builder
  repairs it in two commits: first a stated limit for the unit run with room, so that the gate is green and
  the analyser's move is released; then where the package's time goes, what the signatures' tests added, and
  the cheap part of the cure (shared fixtures, parallel tests, waits replaced by signals), with the rest named
  as a refactor with an estimate. **This is the owner's rule "Tests stay runnable" at work:** a builder's
  measured seconds were taken on the development machine without the race detector, and the job multiplies
  them. **For every builder's brief from now on:** the cost of a unit test is measured in the job's mode
  (race detector, coverage over every package) for the package it joins, and the API's package gets no test
  that builds a server of its own where a shared one serves.
  **The first commit landed at 18:55** (the Makefile alone: a stated limit of 20 minutes per test binary for the
  unit run, twice the package's time of the morning and below the job's own limit of 30; checked by the
  target's expansion, not measured locally, because the machine stood at a load of 123 on 20 cores with six
  agents at work, two of them the repairs of red jobs beside four builders: over the owner's limit of four,
  not stopped because each was in the middle of its work; nothing further was started until the count was
  down). **Its run (44932718779): the unit job is green,** the API's package took 599.5 s in it, the job 14.4
  minutes; **the release was cut at 19:13,** and with it the analyser's move is released. The second commit
  (where the package's time goes and the cheap part of the cure) is in work and is measured only at a lower
  load.
  **The second commit landed at 19:47** (its builder worked from 18:51 to 19:35): **the API's package takes
  about 57 s in the job's mode where it took about 560 s.** Where the time went, read from the test reports
  of three runs: the package's 236 tests run one after another, and nearly all of their time was the parsing
  of the API document of 1.1 MB, not a check: a response validator costs about 8 s to build and about 25 tests
  built one each, a server's parse costs 0.85 s and one test builds 56 servers (49 s). What the signatures'
  second package had added: 67 s for 13 new tests, six of them with a validator of their own, and 41 s on the
  existing tests, because every parse now reads a larger document; no loop a test waits for. The cure: the
  document is parsed once per test binary and only read, every test keeps a validator of its own that records
  what that test reached; no assertion is weaker and no test is gone. **Measured** on the development machine
  in the job's mode, by the builder (560 and 562 s before, 56 to 58 s after, three runs, all green, loads of 7
  to 37) and by the development agent on the rebased commit (56.4 s at a load of 12). The page on building and
  testing has a section on the unit run's time with the slowest packages and the helper a new test of the API
  uses. **Left with an estimate:** parallel tests in that package (a change test by test, little to gain now);
  the integration tests build a validator per stack and could share the parse the same way (about an hour,
  the gain not measured). **On the pipeline's runner** (run 44937450926): the package took 76.6 s where it had
  taken 599.5 s, and the unit job 8.9 minutes where it had taken 14.4.
- **Landed on 2026-11-22 at 00:44: the repair of the first stage's three defects that the measurement with real
  clients had found** (a commit of the type `fix`; decisions Q-SIG-216 to Q-SIG-219, gap H-1233; what it repairs
  and the fixtures of the real tools are in its report, in the entry below). **Added before the landing, as
  asked:** two fuzz targets for the readers it wrote, the signature file's reader and the conversion of BER
  lengths, seeded with the real tools' files; each ran two minutes locally (565,507 and 7,407,794
  executions; the first run of the signature's target had reached 175, because the tool spends its time
  shrinking inputs of 24 KB by default, which the nightly's legs of thirty minutes will meet too). **One
  change of the reader came from it:** a tag number that is not written in its shortest form is refused,
  as the standard and the language's own parser do. The targets' one finding was in the test's own walk.
  The bounds the reader enforces are written down (256 KiB per object, 32 levels, one primary signer and at
  most 16 countersignatures, 4 timestamps per signer, chains of 8). Both targets are in the nightly's fuzz
  job for NuGet; they were renamed at the development agent's word so that no job's name carries a
  requirement's code (two older targets of that job still do, section 8). **What an installation that was
  hit shows:** nothing checks an old NuGet signature's row again, so a row that read as malformed stays so
  until its package is gone and stored anew; a lost Ansible signature stays lost until it is posted again;
  a remote image's version shows its signatures at once. **By the development agent on the commit as it
  landed, each exit 0:** build, the full unit run, generate-check, the evidence regenerated without a
  difference, links and check, the complexity check, the lint, the check of the workflow files; the diff
  holds no name the white-label rule forbids and no private key; no change of the UI or the chart. The
  owner's UI backend started with it before the push. **Its release was cut at 00:56**; the unit job took 9.7 minutes. Image and chart were published at 01:06, and
  **the owner's installation runs it since 01:08** (read on the cluster at 01:08: every Deployment ready, no
  restart, no error line of `api`, `packages` or the workers). Its run was stopped at its long jobs by the
  push of the gate's screens. **Not verified:** the real clients against
  the repairs (dotnet against a gated repository, `ansible-galaxy`, Docker); nuget.org's countersignature
  under the shipped anchors. Its worktree and branch are removed.
- **Landed on 2026-11-22 at 00:57: the screens of the gate (stage 3, package 3), and with them the third stage
  is whole on `main`** (R-SIG-074; decisions Q-SIG-223 to Q-SIG-232, gaps H-1237 to H-1240; its builder worked
  from 23:46 to 00:47; no change of Go, of the API or of the schema). **The repository's section
  Signatures** (a detail of every repository but a system one, never in a creation flow): the setting with
  its sentence, the warning of Q-SIG-76 where any anchor counts, the anchors that count with kind, source
  and fingerprint, the preview read whenever the section opens ("n of m artifacts pass, k are refused"),
  the question before a narrowing with the numbers per reason and up to twenty examples, an Owner's view
  with the narrowings alone as controls, the sentence for a Debian repository and for an installation whose
  checks are off. **A refused artifact:** a banner at the version in the gate's words, "Refused here" with
  the reason in place of the download at the file, the release for who holds the triage right, and for a
  released one who released it, when and why, with the withdrawal; an image that passes on its index says so
  for each platform. **The security dashboard's view:** the refused downloads with filters, the release and
  the withdrawal, and a repository's releases. **The impact** is asked before an anchor is removed or
  switched off, the project anchors' switch is turned off or a signer is blocked, when it is above nothing.
  The client setup of a gated repository says so and names the anchors. Both languages (114 units).
  **Decided by its builder,** among its ten blocks: the section learns from the preview's refusal that the
  checks are off, because an Owner may not read the installation's settings (Q-SIG-223); a writer who may
  not loosen gets exactly the narrowings as controls (Q-SIG-224); a refused index names no platform's
  decision, because the API judges the tag alone (Q-SIG-226, gap H-1238); the releases are listed one
  repository at a time, as the API lists them (Q-SIG-227, gap H-1239). **Its gates, each exit 0:** the UI's
  install, tests, lint, the layout checks' own tests, the build in both languages, the evidence; the renders
  of every page it changed at 1,440, 1,024 and 390 px, light and dark (its first run over 153 screens had one
  failure, an older walk that opened the new list's filter, corrected; two findings of the layout checks at
  the refused downloads' list, corrected). **Looked at by the development agent** (rule 11): the section with
  chosen anchors, a removed one in red and the preview's sentence as an Owner (wide, light); a refused npm
  version with its banner and "Refused here" (wide, light); the dashboard's refused downloads and releases
  (1,024 px, dark); the question before a narrowing with its twenty examples (narrow, dark); an image that
  passes through its index (wide, dark). They read as the pages around them do. Seen and left: a refused
  version's install box still offers its command beside the banner that says downloads are refused; the
  dashboard's rows show the storage path under the artifact's name, as the file view does. **Done at the
  landing by the development agent:** the specification's decision blocks of both sides kept (the repair's
  and the screens'); on the commit as it landed, each exit 0: the UI's install, the API's types regenerated
  and the translation source extracted without a difference, the UI tests (1,929), lint, layout tests and
  build, the evidence and its count test; the diff holds no name the white-label rule forbids. Pushed
  after the release of the repair before it, whose run it stopped at its long jobs. **Its release was cut at 01:10,** image
  and chart were published at 01:16, and **the owner's installation runs it since 01:19** (read on the
  cluster at 01:19: every Deployment ready, no restart, no error line of `api`, `packages` or the workers;
  the UI answers on his host). **Its tests
  cost** about 0.4 s of UI tests and about 90 s of the layout job (31 states, 186 pictures; the job stood at
  42 of its 75 minutes). **Not verified:** a real save of the setting, a real release and the real refusal
  of a weakening from the screens (every refused, released, preview and impact state of the pictures is an
  answer changed in the browser, because the render profile gates no repository); the German texts in a
  browser; a virtual repository's sentence. Its worktree and branch are removed.
- **The owner is away from 2026-11-22, 11:00, until the evening** ("Ich bin jetzt wieder weg. Wir sehen uns heute Abend
  wenn ich wieder im Büro bin. Viel Erfolg bei der Umsetzung"). The development agent builds on alone: the
  Bastion that serves every format and the storage topic by its stages; the timer of every 30 minutes is set
  again; no measurement against his object storage provider today; the walk through the open questions (16 in
  the list of "With the owner") is for the evening.
- **In work since 2026-11-22, 11:41: package A of the several connections** (the storage topic's first stage:
  the configuration as a list, a client and a guard per connection, the copies' table, commit and reads over
  the copies, the start check per connection; its builder keeps the tree whole for one connection, and the
  chart's part lands with it). **The first stage's specification is on `main` since 11:39** ("With the owner",
  the topic of 2026-11-22, has what it says, the planner's three questions for him and the build plan of six
  packages). **Next as agents become free:** the Bastion's package C resumed when B has landed, then the
  reading of the documentation of the object gateway in the owner's cluster, the Bastion's package D, the
  links' package 2.
- **Package A of the several connections reported at 14:03 and waits for the chart's package, with which it lands
  in one push** (one commit of the type `feat`, 162 files; its builder worked from 11:41 to 14:03; rebased by the
  development agent onto `main` at 14:04, with one row of the specification merged by hand and a link repaired
  that a renamed heading had broken). **Built:** the configuration as a list of one to eight connections with
  its validation, the single block gone; a client and a guard per connection with the connection as a label of
  the breaker and the metrics; object metadata on the store's interface; the migration with the tables of
  connections, copies and the copy queue, in which the blobs that exist become copies of the connection
  `default`, bound to its bucket at the first start (its decision Q-STOR-250); the commit writing to the first
  connections that take it and falling over on a transient error; reads over the copies, falling over before
  the first byte, a damaged copy marked on its own connection; the fixed properties, the marker and the
  command that retires a connection; **the start check per connection that records what the store can do,**
  among it whether a wrong payload checksum is refused and whether a copy onto itself replaces the metadata and
  keeps the bytes (added from the reading of the owner's object gateway, whose earlier release lost such a
  copy's data); readiness per connection; the empty hooks for the packages that follow. Decisions Q-STOR-250 to
  Q-STOR-254, gaps H-1258 to H-1260 (until the reconciliation exists a queued gap is never filled and the
  collection deletes on the first connection alone; two installations that start for the first time on one
  empty location at the same moment both pass the marker's check, because the marker is written without a
  condition). **Its gates:** build, the full unit run, generate-check, the evidence, the complexity check and
  the lint exit 0; under the lock 24 integration areas pass, among them `blob`, `uploads`, `transfer`, `backup`
  with a restore end to end, `proxy`, `files`, `postgres`, `upstream`; **the chart's tests fail as expected,**
  because the chart still renders the old key, which is why it does not land alone: the next release would
  start no pod. **Not run by its builder, run by the development agent on the rebased commit since 14:05:** the
  other integration areas (the formats' own, the API, authorization, vulnerabilities, signatures, limits, the
  Bastion's). **Risks it names:** the marker makes a start over a bucket that another database has used refuse
  (by design; a reset of the owner's installation or of a development stack over a kept bucket meets it,
  question 21 of his list); every download now reads the copies' rows once, whose cost is not measured; the
  shared integration stack keeps one marker object of a test run. **Not verified:** anything on the owner's
  object gateway; a copy above 5 GiB; a server-side copy between two real buckets (the reconciliation's
  package).
- **The chart's package of the several connections is at work again since 14:05** with A's commit: its rebase,
  the tests that pass the rendered configuration to the binary, and the proof of the development profile on a
  kind cluster of its own. **It reported at 14:36** (one commit of the type `feat` on A's commit; rebased with A
  onto `main` by the development agent at 14:37, without a conflict). **Built:** the chart renders the list
  of connections and gives an element without an identifier the identifier `default`, as the binary does; a
  connection's Secrets are one entry of `secrets.storageConnections`, found by `connectionId`, each field a
  volume of its own under `/var/run/secrets/quader/s3-<id>…`; a provisioner's bucket, region and endpoint
  reach a pod as files, so no Secret of the store is an environment variable any more; the backup's pods get
  every connection's location, CA and read login and never its runtime keys; the migration Job and the scanner
  pods mount nothing of a connection; the alerts are per connection (every connection down is critical, one of
  several a warning; backlog, damaged copy, scrub behind, detached), each with a firing and a silent case; the
  chart's value set of the tests has two connections; the values block of the specification is the chart's
  again. **The chart refuses the values of the one store at rendering** with a sentence that names the new
  keys, and every refusal of R-HA-072 names the element by index and identifier. Decisions Q-HA-393 to
  Q-HA-398, among them: the backlog alert leaves out a connection that is still filling, by a gauge the
  reconciliation's package has to export (393); an element without an identifier is `default`, which
  corrected a disagreement between R-HA-072 and R-STOR-050 (394); the backup and the journal replay read the
  first connection alone until the binary knows the others, so their values name the first connection's
  logins (396). **Its gates:** chart lint over 22 value sets, the whole of the chart tests, build, the full
  unit run, generate-check, the evidence regenerated with links, check and the count test, and the lint exit
  0; the wording scan is clean. **The proof on a cluster ran on a reduced installation, not on the development
  profile,** because the machine's container memory was three quarters in use by other clusters: one node, plain
  PostgreSQL, Valkey and an object store, one connection with static keys from an entry; the migration ran,
  `api`, `packages` and `worker` became available, the start check wrote the marker, and a file pushed to a
  generic repository came back the same. **Not verified:** the development profile on a cluster (TLS, the CA
  mount, the backup's login, the operators), which the render tests and the binary's validator over the
  rendered values cover and the pipeline's end-to-end legs run; the alerts whose metrics later packages
  export stay silent until then (a list in the chart's tests names each metric with its package, which
  removes its line).
- **A finding before the landing, in repair since 14:38:** the README, the developer pages, the security page of
  the transport encryption and above all the manual of the administrator (its table of the store's keys, the
  installation, TLS, backup, limitations, operations, the runbook) still describe the keys of the one store as
  current, about 35 places in 13 files; package A's build plan had left the manual to the last package, but
  what a page says of a removed key is wrong the moment the change lands (rule "document immediately"). The
  builder of package A corrects them as one commit of the type `docs` on top of the two, every key read from
  the configuration's code and the chart; the three land in one push once that commit and the development
  agent's integration run (20 areas passed at 14:36, the others run) are done. **Corrected by 15:02** (one commit
  of the type `docs` on top of the chart's, 16 files; every key, default, path, metric, command and refusal
  read from the configuration's code, the chart's templates and the commands; the manual's table of the
  store's keys names what one connection needs in the plain case and links the full reference; its gates,
  the tests that read the manual, the web rule check of the manual's pages, the chart tests, the evidence with
  links and check, exit 0). **Reading the code for it, its builder found six places where the code does not do
  what the specification or a page says.** Two are repaired before the landing, as one commit of the type
  `fix` on top, in work since 15:03: **the object store's error messages still name the removed keys** (an
  operator with a wrong connection would read a key that no longer exists), and **the restore's check of the
  empty prefix refuses the marker** that every start now writes, where R-BAK-030 accepts the marker of the
  backup's own installation, so a restore into a prefix a replica has started on would fail. The other four
  are in "Open findings" (Product) with the package that repairs each: the alerts' series, the journal and
  the backup on the first element against the shipper's first connection without drain, the restore's
  stand-in on `default`, and the two tasks without a job kind. **At 15:02** 29 integration areas on A's commit
  have passed and none failed; the kind cluster of the development profile's proof exists; **the pipeline
  run of the encryption's format (pushed 13:59) ended green, whole,** and the run of the Bastion's package D
  is in progress.
- **In work since 2026-11-22, 14:22: the planner of the handover per blob to a Bastion** (the owner's decision
  12, option b: a short-lived link of the object store and, on an encrypted connection, the data key of that
  one blob, so a Bastion reads large content from the store itself; no account and no key above the blob's own
  in the DMZ). Both conditions of its turn are met: the encryption's specification is on `main`, and a Bastion
  serves every format (packages A to C landed, D at work). It writes the specification and a build plan and no
  code, with a block of identifiers of its own (R-BAS-070 to 089, Q-BAS-230 to 269, R-STOR-095 to 099,
  Q-STOR-340 to 359, ADR-250 to 253, H-1340 to 1369), and it brings back the questions that are the owner's.
  With it four agents are at work: the Bastion's package D, package 5 of the encryption, the chart's package
  of the several connections, and this planner. **It reported at 15:07** (one commit of documents; it worked
  from 14:22 to 15:06; rebased onto `main` by the development agent without a conflict, in the landing gates
  since 15:07; **it is pushed together with the several connections, after the Bastion's package D has had
  its end-to-end legs,** because its commit changes the count test and so starts a run that would end
  them). R-BAS-070 to R-BAS-079, R-STOR-095 and R-STOR-096, Q-BAS-230 to Q-BAS-239, Q-STOR-340 to Q-STOR-347,
  ADR-250, the gaps H-1340 to H-1345 on a new security page, a build plan on a developer page of its own.
  **What it says.** *Who asks:* on a miss a Bastion adds to its ordinary forward a header that offers to take
  a handover, with the largest size it can take. *Who decides:* the main installation decides that forward
  exactly as today (the caller's credential, the gates, the admission, the counting) and only at the serve
  point, behind the admission, answers a handover instead of the bytes: the file's own answer with every
  header and no body, and one header that carries the link, its expiry, the connection, the digest, the sizes
  and, for an encrypted copy alone, the data key. The interface rises to its fifth version. *The conditions,
  in a fixed order:* the Bastion offered and its registration has the switch on (per registration, off unless
  turned on, no chart value: the side the keys leave decides); an artifact; the content sound and every gate
  passed; no range; the size between the links' smallest size and what the Bastion can take; the admission; a
  copy on a connection whose link works, a plain copy before an encrypted one, so that no key leaves where a
  plain copy serves; the data key read from the object; the credential valid for five more seconds; and for
  an encrypted copy **an audit event written in its own transaction before the answer, so that no key leaves
  without its record.** *The Bastion's read:* a client of its own without a credential and without the
  interface's headers, which follows no redirect and refuses Quader's own hosts; the bytes go through the fill
  that exists (a plain copy against its digest, an encrypted one through the format's reader with the handed
  key); the key is destroyed after the read and written nowhere. *Failures:* before the first byte the forward
  is sent again without the offer; after it the fill goes on from the main installation by a range; a digest
  that fails at the end cuts the callers and marks the blob for fills through the main installation; after a
  failure of the network a replica offers nothing for 60 seconds. *Counting:* when the handover is given, as
  for a client's link. No new value in either chart; one alert comes with the build. No end-to-end case, since
  no client behaves differently. **Decided by the planner, open to the owner's objection, the two that weigh
  most:** a repository's opt-out of signed links does not reach handovers (Q-STOR-345: the opt-out exists for
  formats whose clients check a digest, and a Bastion always checks), and a single blob is not rewritten
  under a new key after its key was handed (Q-STOR-347: older versions and the backups keep the old content
  anyway; the audit trail names every key that left). **Its question for the owner** is number 26 of his
  list. **Not verified:** anything against a real object store; whether the integration suite's Bastion
  trusts the stack's CA without more work; the header size the main installation's ingress lets through (the
  handover's header is estimated at about 3 KiB); the throughput of the decryption. **Found by it in the
  documents, repaired by the development agent at the landing:** the index of the architecture decisions
  lacks the rows of four records whose bodies exist, and the security pages' table of gaps lacks the row of
  the signed links' page.
- **The timer's check at 14:21:** nothing has reported since package A; of the integration areas the
  development agent runs on A's rebased commit, the first five passed (the three of Ansible, Debian local and
  remote) and the others run, one lock per area; the owner's installation runs the newest release (read at
  14:17: the chart's release deployed and ready, six Deployments with every replica ready, 19 pods, no
  restart); the pipeline of the last code push is attended in his window from 17:00. **The owner asked at
  14:16 what has been finished since Friday afternoon** and got the overview in the chat (35 releases since
  Friday noon, by topic, with what is on `main` without effect yet, what is built and not landed, and what is
  not verified); no decision in it.
- **Package 5 of the encryption, the Bastion's cache, reported at 15:09 and is on the landing stack** (one commit
  of the type `feat`, 36 files; its builder worked from 14:00 to 15:09; rebased onto `main` by the development
  agent without a conflict, with the row of the manual for the new Secret added from its builder's sentence).
  **Built:** the chart's `secrets.cacheKey`, empty unless set, mounted into the pods; the cache reads the file
  once, derives the storage key, its identifier and the name key, and clears the file's bytes; a file that
  cannot be read stops the start. Only the cache's backend knows of the encryption, the engine and the
  download buffer see plain bytes and the same errors, and every sealed byte goes through the format's
  package. With a key the content, the documents, the entries, the access markers, the audit spool and the
  readiness probe lie under a tree named by the key's identifier, each under a keyed name; sizes, counts and
  times stay visible, since the eviction lists them. Without the key every test that existed passes unchanged.
  **Its decisions, Q-BAS-212 to Q-BAS-215,** among them one that replaces the planner's: the entries and the
  spool's files are objects of the format bound to their names and not sealed under one key of their own,
  because entries are rewritten at every revalidation, so random nonces under one key would near their bound,
  and the seal would be a second construction no vector checks. With a key the volume backend spools its fills
  on the pod's temporary volume, so that the cache volume holds ciphertext alone. Gaps H-1325 to H-1327
  (download events spooled under an old key are lost when the key changes during an outage of the main
  installation; the old tree stays readable to a holder of the old key until it is swept; a pod that is taken
  over holds the key). **Its gates** exit 0: build, the full unit run, generate-check, the evidence, the
  complexity check, chart lint and tests, the lint, the Bastion's two integration areas. **With a key given to
  every Bastion replica of the integration suite, one test fails by design** (it tampers with the plain
  layout directly; the same behaviour with a key is shown by the package's unit and bucket tests), and the
  rest passes. Its builder took each of the cache's ten defences out once; two survived at first and the tests
  were tightened until each was caught, and two real defects showed and are repaired (a moved entry counted as
  a backend error, and the key parser failed on a key of 43 characters). Tests added: nine unit tests of 0.2
  s together, one integration case of 0.18 s, one chart test of 0.37 s, nothing end to end. **Not verified:**
  a deployment of the chart with a real Secret; objects above 70 KiB and the throughput; two keys on one shared
  bucket during a rollout (reasoned through, not run).
- **The landing stack since 15:10, in a worktree of the development agent's own:** on `main` the encryption's
  package 5, the handover's specification (its three conflicts with package 5 in the documents resolved by
  hand: both groups of decision blocks kept, the two status sentences of the Bastion's cache merged; the four
  missing rows of the index of architecture decisions and the missing row of the gaps of the signed links
  added; its own gates on its own commit exit 0), then package A of the several connections, the chart's
  package and the documentation's correction (one link to a renamed heading repaired), and last the repair
  that package A's builder is writing. On the five commits the build, the evidence regenerated without a
  difference, the links, the check and the count test exit 0; generate-check, chart lint, the chart tests and
  the web gates run since 15:12. **They are pushed as one push** once the repair is on top, the full gates and
  the integration areas are through on the final commit, the proof of the development profile has reported,
  and the end-to-end legs of the Bastion's package D have ended. **The repair reported at 15:36 and is the
  sixth commit of the stack** (type `fix`, 36 files; its builder's gates exit 0, the backup's integration area
  on the second run, the first having met the test tool's own limit of ten minutes beside a lint and another
  run, without a failed test). *The error messages:* an object-store error names the connection and the key an
  operator sets, in the one form the list's validation uses (`storage.connections[<n>] (<id>): <key>`), also
  in the start check and in the refusals of a connection; the backup's target and the Bastion's cache bucket
  are named by their own keys. *The restore:* the check of the empty prefix reads the marker; the marker of the
  backup's own installation is accepted, one of another installation fails with both installations named, any
  other object at the prefix root still fails, and, beyond R-BAK-030 and written into it, a marker of the
  same installation for another connection fails as well, since a replica would refuse it anyway. Tests: four
  cases of the check and two of the messages at the unit tier, one case of the sink at the integration tier,
  bytes of data. **Not verified:** a real restore stopped by a marker a replica wrote (the check's decision is
  shown at the unit tier, the sink's reading at the integration tier). **Before it, at 15:16,** the chart tests,
  generate-check, chart lint and the web gates exit 0 on the stack of five, and one gate did not: the
  translation source was stale by the line numbers of the audit labels; extracted again by the development
  agent and folded into package A's commit, after which a second extraction changes nothing. **On the final
  commit since 15:37:** the whole of the gates in one run and, beside it, 21 integration areas under the lock
  (the seventeen that had not validly run, and the store, the Bastion's cache, TLS and the operator's
  verify, which the repair or the Bastion's cache touch), each with the test tool's limit raised to 30
  minutes as the Makefile has it. The scrub's and the reconciliation's builders know of the changed signature.
  **The gates ended at 15:52, all fourteen with exit 0** (build, the full unit run of 203 packages run anew,
  generate-check, the evidence regenerated without a difference, links, check, the count test, the complexity
  check, chart lint, the chart tests, the API types and the translation source without a difference, the vet
  of the end-to-end sources, the lint; the wording scan clean, no line of the evidence record by hand).
  **The integration areas found what the gates cannot: package A breaks a requirement, and the several
  connections are not pushed until it holds.** Of the 21 areas 16 passed by 16:36 and two failed. *The query
  budget (a regression, certain):* a download now runs two database statements where R-ARCH-027 allows one,
  and three where it allows two, because the read over the copies fetches the copies' rows in a statement of
  its own; this is the cost its builder had named as not measured, and neither it nor the planner had set it
  against the budget. *The overload area (cause open):* in the burst beside empty caches many downloads
  answered 503; the run stood beside other load, so the extra statement under a burst or the machine can
  be the cause. **In repair since 16:46 by package A's builder,** as one more commit: a download costs what
  the requirement allows with the two tests of the budget unchanged (the copies arriving with the statement
  a download already runs is the candidate; a cache in the process only if it can say when it is wrong), on
  every path that serves stored bytes; the overload test alone on a quiet machine and, if it still fails, on
  the code of `main` to tell the two apart. **What the development agent takes from it:** the areas a builder
  did not run are run before its package is called ready, not beside the landing; here they ran, and the
  finding came before the push.
- **The landing stack is split at 16:46.** The encryption of the Bastion's cache and the handover's
  specification do not depend on the several connections; they stand in a worktree of their own on `main`,
  their gates run, and they are pushed as soon as those are through (the Bastion's integration areas passed
  on the whole stack at 15:39 to 15:54). The four commits of the several connections wait for the repair.
- **In work since 16:44: package B of the several connections** (sessions on their connection, the collection
  per copy with its release, the orphan sweep per connection with the adoption; Q-STOR-260 to 264, H-1265 to
  1268; it closes the gaps package A left, that the collection deletes on the first connection alone) **and
  the rest of package E** (the backup reading one copy per blob, the restore into the first connection with
  its real fix-up, the journal shipped to and replayed from every connection, the list of the backup's
  sources in the binary with the chart moved from its interim form; Q-BAK-70 to 74, Q-STOR-270 to 274,
  Q-HA-399 to 402, H-1284 to 1289; it repairs three of the open findings of the several connections). Both
  on the top of the landing stack. Builders with a test load: the scrub, package B, the rest of package E,
  and the repair on package A.
- **The development agent's integration run on package A ended at 15:08: 33 areas passed, and the last
  fourteen did not run** (the second Bastion pair, npm, NuGet, the endpoints, the importer, publishing, the
  mirror API, overload, the query budget, the registry, the virtual repositories, Valkey, the CI area): they
  failed to build, because the run used package A's worktree and its builder, resumed at 15:03 for the
  repair, was changing a function's signature in that minute. A mistake of the development agent's, not a
  finding of the product; three areas of the Bastion before them ran on a tree in the middle of that edit and
  count as not run. From 14:38 the tree was package A with the chart's commit on top, whose code is the same.
  **All seventeen run again on the final commit of the landing stack,** with the areas the repair touches. The
  rule from it is in section 3.
- **Package D of the several connections, the scrub, reported at 16:48 and lands after the reconciliation** (one
  commit of the type `feat`; its builder worked from 15:15 to 16:48; every gate exits 0, with seven integration
  areas and the web gates, since the API changed). **Built:** an hourly job with one reader per connection,
  each held to that connection's bytes a second; copies never verified first, then the oldest; a copy is
  judged by its bytes alone, length and SHA-256; a match stamps it verified, a mismatch or a missing object
  sets the mark a download sets; **a damaged copy is replaced in the same run from a present copy of another
  connection, read back whole, and recorded only while its row is still the one found damaged;** a transient
  error leaves the copy due, an open circuit ends that connection's reader for the run. The list of the
  damaged blobs over the API with its two scopes and a recheck that reads every copy now and writes no
  object. `admin storage verify` and `restore-versions` per connection. The gauge of overdue copies, counted
  from the database on every replica, so the scrub's alert reads real series. The tests that named the
  scrub's requirement for other things name what they test; the evidence record was not touched, the scrub's
  own tests keep its line true. Decisions Q-STOR-265 to Q-STOR-269 (a copy counts as overdue a day past its
  period, so a large connection in a steady state does not trip the alert; a blob is lost only on evidence).
  Gaps H-1269 (a store that applies an overwrite late can pass the read-back and still serve old bytes from
  another node), H-1270 (a failed repair waits for the reconciliation), H-1271 (no audit event on a
  detection). **Decided by the development agent on its question:** the audit event of a detected damage is
  built with this package, with its label and translations, and not left to the last package, because an
  auditor reads integrity events and the gap would be the package's own; its brief had allowed no web file
  beside the generated API types, which was too narrow. **Before it lands,** after the reconciliation has been
  moved onto the landing stack: its builder moves it onto that, uses the reconciliation's mark of a damaged
  copy and drops its own (both had written one), adapts two calls of the changed signature, and adds the
  event. **For the owner's installation:** the first run reads every object once (every copy is unverified
  after the migration) at up to 50 MiB a second and writes nothing; an object that is already gone or
  altered in his bucket shows at once as a missing or damaged blob with the critical alert, and with one
  connection nothing repairs it (a remote's item is fetched again, a local one answers 503 until uploaded
  again); **if his gateway answers 404 for objects that exist while a run goes, each such copy is marked
  missing and heals only by a read or the recheck, not by the scrub, since a marked copy is never due
  again** (a behaviour to look at on his cluster after the first run). **Not verified:** anything on his
  gateway; a store that applies overwrites late (simulated); the repair above the multipart threshold against
  a real store; the job run through the job framework from end to end; the new endpoints on a running
  replica. What it was at its start: (the scrub per copy with its repair
  from another connection, the damaged blobs' API with its two scopes, `admin storage verify` and
  `restore-versions` per connection, the renames that make the scrub's evidence true, and the metrics two of
  the chart's alerts wait for). It goes before package B because since package A no job reads the stored
  content for damage. Its builder works on the top of the landing stack; identifiers Q-STOR-265 to 269 and
  H-1269 to 1272 (260 to 264 and H-1265 to 1268 are kept for package B). **The timer's check at 15:14:** the
  pipeline run of the Bastion's package D has its gating jobs, the release step (no release, by the commit's
  type) and the legs `ci` and `push` green; the legs of the Bastion, the registry, npm, the vulnerabilities and
  the long access set, the four integration shards and the layout checks run. Builders with a test load: the
  reconciliation, the scrub, the proof of the development profile, and the repair on package A; the handover's
  planner and the encryption's package 5 have ended.
- **Landed on 2026-11-22 at 13:59: package 1 of the encryption, the format and its tools** (a commit of the type
  `refactor`, no release; R-STOR-076 to R-STOR-079; decisions Q-STOR-321 to Q-STOR-323; its builder worked from
  13:02 to 13:53). A package of its own that uses the standard library alone, held by a test: the stored size
  and its inverse, the range planner, the nonces from a seed per write, a writer that keeps one segment in
  memory and gives a retry the same bytes, the record with its wrap and its commitment, one reader for whole
  reads, ranges and staging tails that releases a segment's bytes only after its tag has passed. Nothing calls
  it yet, and an encrypted connection is still refused at start. **The independent check, in the order asked:**
  its codec reproduced the specification's known answers at its first run; a check in another language that the
  builder wrote rebuilt every positive case of the 35 vector files byte for byte and refused every negative one
  with its reason; only then it opened the planner's implementation and ran it unchanged against the files, 34
  of 34 (the planner's knows no tails, so the tail's case rests on the other two). **Every attack of the
  specification's table has a test and a vector,** each with a reader that counts the bytes released before the
  refusal; the builder removed each of fifteen defences in turn and a test failed for each, which found one
  refusal that was missing (a body longer than the object) and, on reading again, an overflow in a bounds
  check; both are repaired and tested. **Three sentences of the specification it corrected, each with a
  decision:** a tail segment's nonce had no rule; a missing or malformed key identifier is a damaged record and
  not an unknown key, since no import can bring such an identifier back; the fuzz job is not started by the
  closing run, whose switch belongs to another workflow. Five fuzz targets, each run for 30 seconds without a
  finding, and a nightly job for them. **Its gates, each exit 0:** build, the full unit run (its first run had
  failed on the rule against package-level pointer variables, repaired), generate-check, the evidence, the
  complexity check, the lint, the workflows' checks. **By the development agent on the rebased commit, each
  exit 0:** build, the full unit run, generate-check, the evidence regenerated without a difference, links,
  check and the count test, the complexity check, the chart's lint and its tests whole, the lint, the
  workflows' test; the diff holds no name the white-label rule forbids and no hand-made line of the evidence
  record; the wrap of the data key and the key types read by the development agent, which is a reading and no
  review. **Its tests cost** 0.35 s of test time and about 2.4 s of process time in the unit job, nothing
  elsewhere; the nightly job about 27 runner minutes (estimated, it has not run). **Not verified:** the nightly
  job on the pipeline; the format's speed. Its worktree and branch are removed.
- **The long legs of package C's run were ended by the next push, and the Bastion's end-to-end set has not run on
  C in the pipeline** (read at 14:01: the run of 13:35 cut its release at 13:49, and the push of the encryption's
  format at 13:59 ended its integration shards, its layout job and its legs `bastion`, `npm` and `access-long`).
  The shards and the reference leg run again with the run of 13:59 on the same tree; the leg `bastion` is not
  chosen by that push, which touched nothing of the Bastion. It runs with the push of package D (the suite's own
  files) or of the Bastion's cache, and **the development agent lets that run's Bastion and registry legs end
  before the next push of code,** stacking what is ready meanwhile. Until then C is shown by its builder's
  integration area and real clients, not by the pipeline's end-to-end set.
- **The release with package C of the Bastion runs on the owner's installation since 13:58** (cut at 13:49,
  image and chart published at 13:55; read on the cluster at 14:00: every Deployment ready, no restart). His
  installation runs no Bastion, so the feature itself does not show there (number 17 of his list).
 (the codec, the
  record, the keyed name, a reader that runs its checks in order, the vector files checked against a second
  implementation, five fuzz targets with a nightly job; a new package that nothing calls yet), **and a reading
  of the documentation of the object gateway that the owner's cluster runs** (what it supports of metadata, a
  copy that replaces it, checksums, conditional writes, consistency, signed links; nothing is called). Four
  agents run: these two, the Bastion's package C and the first stage's package A; the chart's package waits
  for A.
- **The second stage's specification, encryption, is on `main` since 13:02** (its planner worked from 12:06 to
  12:49; "With the owner", the topic of 2026-11-22, has what it says, its three questions for him and its build
  plan of six packages; its first package, the format with its vectors and fuzz targets, needs nothing of the
  first stage and starts when an agent is free).
- **Reported at 12:49: the planner of the storage topic's second stage, encryption** (the format,
  the write and read paths under it, the storage keys and their rotation by the key ring, keyed names, what the
  reconciliation and the scrub do on an encrypted connection, backup and restore, the Bastion's cache, the
  offline tool; it builds on the first stage's contracts and has a block of identifiers of its own).
- **The chart gate was red on `main` from 10:26 to 12:56, by two landings of specifications, and held back a
  release** (the development agent's fault; found at 12:50 in the run of the Bastion's package B, whose job
  `chart` failed and whose Semantic Release was skipped). Two chart tests hold the values blocks of the
  specification equal to the charts' values. The specification of the Bastion that serves every format (10:26)
  had put `fill_wait_max` into the Bastion's block, and the specification of the several connections (11:39) the
  list of connections and their Secrets into the main chart's, before any chart carried them; both were landed
  with the links, the check, the regenerated evidence and the count test, and without the chart tests. The
  three runs between were of commits that release nothing and were each ended by the next push, so nothing
  showed. **Repaired at 12:56:** both blocks show what the charts hold today (the whole of the chart tests and
  the count test exit 0 on it), and the Bastion's package C and the chart's package of the connections bring
  the new values together with the charts that carry them; both builders were told. A change of documents
  under `docs/` alone starts no run, so the repair went out with the next push that carries a file outside it.
  **The rule from it** (section 3): a specification is landed like code, with the chart tests and the full unit
  run beside the evidence's own checks, because its blocks are inputs of tests; the second stage's
  specification was the first landed so.
- **Landed on 2026-11-22 at 12:31: package B of "a Bastion serves every format", the main installation's side**
  (a commit of the type `fix`; decisions Q-BAS-186 to Q-BAS-190, gap H-1290; its builder worked from 11:18 to
  12:26). **What an operator notices:** a Debian package, a collection version or an image deleted at the main
  installation is withdrawn from a Bastion's cache through the feed within its bound, where a Bastion served
  it for up to ten minutes more (the finding of section 8; Debian and Ansible Galaxy reach a Bastion today, an
  image once package C is out). **Built for C:** the main installation lets a Bastion forward the registry
  root; the token service and the catalog tell the marking their route; a write below the registry root is
  refused in the registry's own form; a registry token's read shape is capped at partial at the marking and at
  the check; a refusal for a missing scope keeps the accepted subject; the handshake carries the table of
  formats and the bound repository; **every download the main installation counts is marked for the Bastion**
  at the moment the answer's status line goes out: the item store's serve, the remote engine's artifact
  answer, the generic download path, and the tag of a remote image. **A fault its builder met there:** the
  registry counted a remote image's tag after writing its answer, so a warm pull through a Bastion would have
  lost the tag's manifest from the audit; the count stands before the answer now, with a test that fails on
  the old order. **The two points the planner had not verified, established by unit tests:** the decision point
  does not see a registry token's grant, and a Bastion's host reaches the token service's check of the
  service name. **Where specification and code disagreed, each settled by a decision of its builder:** the count
  is recorded after the body, too late for a header (the note is made at the status line); the handshake gave
  a Bastion no way to know a bound repository's format (only a binding the registry's match honours is named);
  the check would have refused every registry token on a replica of the role `api` (the check's stage counts
  its paths as served). **A gap it names (H-1290, dormant until C reports entries):** a miss the remote engine
  passes through during an outage of the object store is marked counted although it is not counted, which can
  only report too much. **Its gates, each exit 0:** build, the full unit run, generate-check, the evidence,
  the complexity check, the lint; under the lock the areas `bastioniface`, `bastionreg`, `bastionsetup`,
  `bastionshared`, `registry`, `ociremote`, `ocilocal`, `deblocal`, `debkeys`, `ansiblelocal` and the Bastion's
  own (125 s). **By the development agent on the rebased commit, each exit 0:** build, the full unit run,
  generate-check, the evidence regenerated without a difference, links, check and the count test, the
  complexity check, the UI's install, the API's types regenerated without a difference, the UI tests and
  lint, the lint; the index rows of the decisions' page merged by hand with both sides kept; the diff holds no
  name the white-label rule forbids; **not run by the development agent: the chart tests, which were red on
  `main` by its own earlier landings (the entry above).** The owner's UI backend started with it at the push.
  **Its release was held back by that gate; it was cut at 13:20** with the run of the push at 13:02, image and
  chart were published at 13:24, and **the owner's installation runs it since 13:26** (read on the cluster at
  13:36: every Deployment ready, no restart). **Its tests cost**
  about 0.3 s of the unit job, nothing in the integration or end-to-end legs. **Not verified:** any real client
  through a Bastion (package C); a deployment that runs the role `api` apart (read and a unit test); whether
  `apt update` works through a Bastion today. Its worktree and branch are removed.
- **Landed on 2026-11-22 at 14:50: package D of "a Bastion serves every format", the last of the four. Images,
  Debian and every other format through a Bastion are built, shown with the real clients and described in the
  manual** (one commit of the type `test`, 25 files, no release; its builder worked from 13:37 to 14:42; rebased
  onto `main` by the development agent without a conflict; on the rebased commit build, the full unit run,
  generate-check, the evidence regenerated without a difference with links, check and the count test, the
  complexity check, chart lint and the whole of the chart tests, the vet of the end-to-end sources, the web
  rule check of the manual's pages and the lint exit 0, the wording scan is clean and no line of the evidence
  record is by hand; its worktree and branch are removed; the owner's UI backend runs on it since 14:51).
  **Built:** the case of an image through a Bastion in the end-to-end
  set of the registry, which installs a Bastion release of one replica for itself and removes it: `GET /v2/` at
  the Bastion's host answers the Bastion's own `401` with its own realm; Docker logs in and pulls with the
  snippets of the client setup, unchanged, and holds the pushed digest; a second pull takes no artifact's
  bytes from the main installation (seven conditional requests answered `304`) and the Bastion's counter of
  bytes from its cache rises; Podman pulls the same digest; a pod on the cluster pulls with the unchanged
  pull Secret, the kubelet reports the pull and the node holds the digest. **The case takes about 17 seconds**
  (measured four times, twice on the commit; the planner had estimated 100), which is what the leg of the
  registry gains, and no other leg gains anything; it runs on a push that changes the registry's own paths
  and in the nightly (its decision Q-BAS-207, by the owner's rule of 2026-11-20). **The manual says now that a
  Bastion serves every format:** the Bastion's page of the administrator (what each client names, a mirror of
  the Docker Engine through a Bastion, the sizing for image layers), the two pages of APT, the client setup,
  the two pages of container images, and the README's line on the forwarded paths. No Debian case on a
  cluster: the replay at the integration tier shows what APT meets at a Bastion, and a second leg with a
  Bastion would cost more than it shows. Decisions Q-BAS-204 to Q-BAS-207, no new gap. **Its gates:** build,
  the full unit run, generate-check, the evidence, chart lint and tests, the lint exit 0; the case twice green
  on the commit. **A red test in its one run of the whole set was its own environment** (a test that switches
  the profile dropped the builder's local image tag; green on the rerun with the tag in place; in the pipeline
  the tag is the one the set expects). **Not verified:** the seconds on a runner of the pipeline; the two
  other sets of the registry, which were not run; the images for the other processor architecture.
  **The order of the pushes, decided by the development agent:** package D was pushed alone, and its legs of
  the Bastion and of the registry finish before the several connections are pushed, so that the Bastion's
  end-to-end set, which the pipeline has not yet run on package C, gets a result that no change of the store
  can blur. **The Bastion's end-to-end set is green in the pipeline, read at 15:32:** in the run of this push
  the legs of the Bastion and of the registry (with the new case of an image through a Bastion), of npm, the
  vulnerabilities, the push and the long access set and all four integration shards ended in success; the
  layout checks still ran. With it images, Debian and every other format through a Bastion are shown at every
  tier: the real clients by hand (package C), the replays at the integration tier, and the clients on a
  cluster in the pipeline.
- **In work since 14:43: the proof of the development profile for the several connections,** which the chart's
  builder had to leave out for want of memory and resumes now that the other cluster is gone: the profile as
  the end-to-end legs install it, on a kind cluster of its own, with the set of the reference client, and in
  the running installation the rendered connections, the mounts, the marker and what the start check recorded.
  The several connections are not pushed before it has reported. **Reported at 15:32: the development profile
  comes up and the set of the reference client passes** (four nodes, TLS everywhere, the operators, the object
  store over HTTPS with its CA, the backup on, installed the way the pipeline's legs install it, from the
  chart's commit on package A; the dependencies in 346 s, the release in 43 s; the migration ran and every
  Deployment became ready). **The set: 13 tests and 212 subtests passed, none failed, 1,172 s.** In the running
  installation: every pod's configuration holds one connection `default` with its key files and its CA under
  the new paths, and the binary's own validation reports the same; the pods mount the connection's Secret and
  its CA, the migration Job nothing of the store; the marker object names the installation and the
  connection; the connection's row records the capabilities the start check found (metadata, batch delete, a
  copy that replaces metadata, a wrong payload checksum refused); after the set 3,339 blobs stand with 3,339
  copies on `default`, all present and verified, the queue empty; the backup's pods mount the read login and
  the CA and not the runtime keys, and a backup run by hand copied 1,191 blobs with none damaged (in this
  profile the read login is the same account as the runtime keys, so this shows the wiring, not a narrower
  right). **A red test of its first run was its environment** (the tests' profile switches replace the extra
  Helm arguments and so dropped its own image tag; green with the image under the tag the set expects, which
  is the pipeline's). No defect of the chart or of package A, no new commit; its cluster, image and
  kubeconfig are gone.
- **Package C of the several connections, the reconciliation, reported at 16:18 and lands after the stack that
  waits** (one commit of the type `feat` on the chart's commit; its builder worked from 14:46 to 16:18; no
  migration). **Built:** the job `storage-reconcile`, exclusive, every minute, not scheduled in read-only
  maintenance; an upload that leaves a gap wakes it, at most once a second per replica. The queue takes due
  rows oldest first and a source that is verified before one that is not; the store copies itself only where
  the start check recorded that the pair can, and a refusal sends the pair to streaming for the run; a
  streamed copy checks the digest before the object is completed; a source that is missing or holds other
  bytes is marked and the next one tried; failures back off under five fixed reasons. The pass queues gaps,
  records full passes and when a connection is filled. Drain, retire, the journal's pass, the metrics and the
  audit event of a drained connection are built; **the backlog alert reads real series now.** Decisions
  Q-STOR-255 to Q-STOR-259 (a streamed copy holds one part in memory, 64 MiB per run by default, because the
  worker's spool is too small for a blob of 10 GiB; a gap is queued only where another connection holds a
  present copy, so with one connection the queue stays empty). Gaps: H-1258 is closed; H-1261 (until the scrub
  exists only a download verifies a copy the store made itself, so a drain beside such copies waits); H-1262
  (the backup and the operator's verify read the first connection alone, the finding already in "Open
  findings"). **Its gates** exit 0; the store's integration area failed once in a collection test while its
  own chart test loaded the machine and passed alone and in a second whole run. Tests added: unit tests of
  0.3 s, one integration case of 3.7 s with in-memory stores, one of 1.1 s against two real buckets of the
  integration stack (the store copies across the two buckets and keeps the metadata); about 6 s more for the
  integration job. **For the owner's installation with one connection:** one run a minute that finds nothing
  to copy, one batch query per thousand blobs an hour, and the single maintenance slot taken for a moment
  each minute. **Not verified:** the job on a real worker from end to end; anything on his object gateway.
  **Before it lands** its builder adapts it to the repair below it (one call of the changed signature, its
  own helper for a connection's name), extracts the translation source instead of the two units written by
  hand, and exports the gauge of a detached connection, which nobody exports yet and its state query gives.
  What it was at its start:
  (the job with its queue and
  its pass, copies made by the store and copies streamed through Quader, the bound on inserts, drain, retire,
  the journal's pass, its metrics, and the integration case against two real buckets of the integration
  stack). Its builder works on top of the two commits that are not on `main` yet, which the development agent
  rebases at the landing; the packages B, C and D depend on A alone and own disjoint files, so C, the longest
  of them, starts before A has landed. It exports the two metrics the chart's alerts wait for (the queue's age,
  the gauge of a connection that is still filling) and takes its lines out of the list of metrics of later
  packages in the chart's tests. Identifiers Q-STOR-255 to 259 and H-1261 to 1264. **The timer's check at
  14:44:** of the integration areas on A's commit 24 have passed and none failed; the landing gates of the
  Bastion's package D are through the first nine with exit 0 (the unit run from the cache, 203 packages, no
  failure); the builder of package A, the encryption's package 5 and the planner of the handover are at
  work; builders with a test load now: the encryption's package 5, the proof of the development profile, the
  reconciliation, and beside them the development agent's integration run.
- **Landed on 2026-11-22 at 13:35: package C of "a Bastion serves every format", the Bastion's side: images,
  Debian and every other format are served through a Bastion, shown with the real clients** (a commit of the
  type `feat`, which announces the interface's fourth version; R-BAS-050 to R-BAS-054, R-BAS-056 and the
  reporting of R-BAS-059; decisions Q-BAS-195 to Q-BAS-203, gaps H-1295 and H-1296; its builder worked from
  11:18 to 12:05 and from 12:32 to 13:30). What it built is in the entry of 12:05 below; added since: **two
  faults that only the real clients and an integration test showed.** `ansible-galaxy` sends its token in a
  scheme of its own, which the Bastion did not read, so every install with a token was refused (read now, on
  the formats' paths alone, and kept apart from the same token as a bearer). And neither a forward below the
  registry root nor the handshake saw the binding of the Bastion's own host, so a bound host could not have
  worked (one change in the main installation's wiring, named by its builder). **The real clients, each
  through a Bastion run locally on a stack of the builder's own, with the pinned images and the test CA
  mounted, nothing changed on the host:** the Docker Engine (a cold pull in 0.21 s with nine forwards, a warm
  one in 0.21 s with seven conditional requests answered 304 and no artifact byte from the main
  installation); Podman; crictl with containerd; apt in Debian 13 (update, install, the signature verified)
  and in Ubuntu 24.04 (warm, nothing asked of the main installation); `ansible-galaxy`; `freshclam` (the
  largest definition file sent once by the main installation, the next check answered from the cache). **A
  layer of 286 MiB asked for cold by three clients at once, one of them resuming from its hundred-millionth
  byte:** the main installation sent the layer once. **The planner's two open points:** the image clients take a
  challenge without a scope and ask the token service for the scope themselves; the largest definition file is
  84.9 MiB, above the bound for documents, and takes the spool. **Its gates, each exit 0:** build, the full
  unit run, generate-check, the evidence, the complexity check, the chart's lint and its tests whole, the lint;
  under the lock the areas `bastion` (148 s), `bastioncache` and `bastioniface`. **By the development agent on
  the commit as it landed, each exit 0:** build, the full unit run, generate-check, the evidence regenerated
  without a difference, links, check and the count test, the complexity check, the chart's lint and its tests
  whole (with the values line of the specification that came back with the chart's value), the lint; the diff
  holds no name the white-label rule forbids and no hand-made line of the evidence record. The owner's UI
  backend started with it at the push. **Its tests cost** about 1 s of the unit job, 0.2 s of the chart's, and
  25.7 s in the integration area `bastion` (six cases with payloads of a kilobyte), where the planner had
  estimated 10 s. **Not verified:** a vulnerability block of an image through a Bastion (the integration stack
  has no findings; a deleted manifest was tested instead); a bound host over a remote repository; the area
  `bastionshared`. **Gaps it names:** an expired registry token counts as a failed sign-in on a replica that
  never saw it (H-1295); each run of `freshclam` asks the main installation three times for files that do not
  exist, because their 404 is not kept (H-1296). Its worktree and branch are removed.
- **The chart's part of package E of the several connections reported at 12:50 that it is ready and waits for
  A** (one commit of the type `feat` in its worktree, amended at 12:58 to carry the specification's values block
  with the chart). The chart renders the list of connections; each connection's Secrets are one entry matched by
  its identifier and become files, so no Secret of the store reaches a pod as an environment variable any more;
  the backup pods get a connection's address, CA and read login and never the runtime keys; the alerts per
  connection are in, each with a rule test that fires and one that stays silent; the old values are refused at
  rendering with a sentence that names the new keys, which leaves a running installation's pods in place. Its
  decisions Q-HA-393 to Q-HA-398; **it gives an installation's values before and after, key by key,** which the
  owner's installation is rewritten from. Open until A's commit exists: the tests that pass the rendered
  configuration to the binary, the rest of its gates, and the proof of the development profile on a kind
  cluster of its own. Its render tests add about 3 s to the chart's job (thirty refusals).
- **Package C of "a Bastion serves every format" reported at 12:05 that it is ready and waits for B** (one commit
  of the type `feat` in its worktree; every gate that does not need B exits 0: build, the full unit run,
  generate-check, the evidence, the complexity check, the chart's lint and tests, the lint). **Built:** the
  registry root as a path family of the Bastion with the registry's error format for its own answers; the
  token service and the catalog forwarded and never kept; registry tokens checked by their form, an anonymous
  one counted as no credential, their read shape capped on the Bastion's side too; the handshake's table of
  formats kept and a broken one refused; **a cached answer now carries the core's content headers and the
  format's declared ones, which closes H-1250 with a test;** the registry's entity tag counts as a content
  address; a ranged miss streams from the spool, a second caller follows a running fill, a replica waits for
  another replica's fill up to a new value of the chart (ten minutes by default); a counted download is
  reported at every serve. **Two contradictions it found in the specification and corrected with a decision
  each:** a path below the registry root on a bound host is the bound repository, as the main installation's
  dispatcher reads it (the criterion had said otherwise); and the rule of the held-back last 64 KiB stood
  against its own example of a range of one kilobyte. Decisions Q-BAS-195 to Q-BAS-202, one new gap (H-1295: a
  replica that does not know a registry token's expiry counts an expired one as a failed sign-in). **Depends on
  B:** it no longer reports an artifact that the main installation did not mark as counted, so B must mark
  every format's counted downloads (B's builder was told). **Not done until B is on `main`:** its integration
  area, and every real client (the Docker Engine, podman, crictl, apt in two distributions, ansible-galaxy,
  freshclam). The development agent resumes it with B's commit. Its tests so far cost about 1 s of the unit job
  and 0.2 s of the chart's.
- **In work since 2026-11-22, 12:02: the chart's part of package E of the several connections** (the list of
  connections and their Secrets in the chart, what it refuses, the alerts, the development profile; it builds
  from the specified shape, waits for package A's commit, then runs the chart's gates and proves the profile
  once on a kind cluster of its own; it lands in one push with A).
- **Landed on 2026-11-22 at 12:00: package 1 of the signed links, the format contract's declaration and the kit's
  twentieth check** (a commit of the type `refactor`, no release; R-STOR-071 is implemented; decisions
  Q-STOR-295 and Q-STOR-296; its builder worked from 11:29 to 11:58). A route of a format says whether every
  client of the format checks the bytes against a digest, with what it checks against and the evidence; a
  route that says nothing streams; the registration refuses a declaration on anything but a `GET` of a
  download and one without its evidence. The kit's check serves every content case of a declaring route
  again and holds it to the stored blob. Declared: the registry's blob, npm's tarball, Ansible Galaxy's
  collection artifact; NuGet, Debian, generic and the ClamAV definitions declare nothing. Nothing reads the
  declaration yet, so every download streams as before. **Two disagreements its builder found in the
  specification:** a defect of the test format cannot fail the new check alone, because two older checks
  catch it first (settled by its decision: each defect is tested with the exemption another check grants its
  route); and the rule "a client family nobody read declares nothing" stands against a table that declares the
  registry and npm while Podman, CRI-O, the Docker Engine's older pull path, pnpm and Yarn are not shown, so the
  pages now say that the second package's end-to-end cases show those families before its serve point lands,
  and a family that accepts altered bytes takes its route's declaration back. **By the development agent on
  the rebased commit, each exit 0:** build, the full unit run, generate-check, the evidence regenerated without
  a difference (its one conflict was the generated checklist), links and check, the complexity check, the lint,
  the count test; the kit over every format after the merge; the diff holds no name the white-label rule
  forbids. **Its tests cost** about 0.8 s of the unit job, nothing elsewhere. **Not verified:** whether any real
  client refuses altered bytes, which is the second package's work. Its worktree and branch are removed. The
  push ended the pipeline run of the Bastion's package A at its long jobs; the new run covers both commits.
- **In work since 2026-11-22, 10:51: the planners of the storage topic's first and third stage** (several
  connections with the reconciliation and the scrub; signed links; the third reported at 11:26, the first at
  11:38; both specifications are on `main`). The owner answered all fifteen decisions
  of its working paper by 10:48, each as the development agent's favourite at that time, two of them changed
  that morning by his own objections ("With the owner", the topic of 2026-11-22). The planner of the second
  stage, encryption, starts when the first stage's specification is on `main`.
- **In work since 2026-11-22, 11:18: packages B and C of "a Bastion serves every format", in parallel** (B the
  main installation's side; C the Bastion's side, which builds what needs A alone, waits for B on `main`, then
  runs its integration area and the real clients in containers of its own, with nothing changed on the host).
  D, the client case on kind and the manual, follows them.
- **Landed on 2026-11-22 at 11:16: package A of "a Bastion serves every format", the two contracts and the kit's
  nineteenth check** (a commit of the type `refactor`, so no release; R-BAS-058 is implemented; decisions
  Q-BAS-181 to Q-BAS-185, gaps H-1249 and H-1250; its builder worked from 10:26 to 11:13). **The format
  contract:** a format declares the headers a Bastion replays from its cache besides the core's, and the status
  of a rate limit; the registry declares its four headers, the ClamAV definitions 503; the contract's version
  stays, because it is an addition. **Check 19 of the kit** holds every answer a Bastion keeps to the declared
  headers, a download to its length and to an entity tag that is the digest of its body, a document to an
  entity tag it answers with 304, and a rate limit to the declared status; every format passes, and **five
  document routes of npm and of the registry carry no entity tag yet** and are exempted with their reason
  (H-1249). **The interface between Bastion and main installation is at its fourth version** (the handshake's
  table of formats and the bound repository, the mark of a counted download, a registry token's form): a
  Bastion of an earlier release does not talk to a main installation of this one, which the commit of package C
  announces for the release notes. **Outside its directories, named by its builder:** the API description and
  its generated Go code and UI types (the interface's version stands there), and the count of requirements in
  the evidence tool's test. **A finding of the landing before it, the development agent's:** that test had failed
  on `main` since 10:26, because the specification of the Bastion was landed with the links, the check and the
  regenerated evidence and without the count test (reproduced at 11:14: 1,160 requirements against 1,149
  expected); no pipeline ran in between, the commits being documents; it is green with this commit, the count
  test is part of every landing of a specification again, and both planners at work were told. **By the
  development agent on the rebased commit, each exit 0:** build, the full unit run, generate-check, the evidence
  regenerated without a difference, links and check, the complexity check, the lint; the UI's install, the
  API's types regenerated without a difference, the UI tests and lint; the count test on `main` after the
  merge; the diff holds no name the white-label rule forbids. **Its tests cost** about 2 s of the unit job
  (1.3 s of it the kit's own test with its four defects), 10 to 100 ms per format in the kit, nothing in the
  integration or end-to-end legs; with 25 formats 0.25 to 2.5 s. Its builder ran the integration area
  `bastioniface` under the lock (41 s). **Not verified:** nothing uses the new members at run time yet. **Seen
  and left:** the kit's check 16 still expects 429 for a refused download limit, which a format with download
  routes that declares 503 would contradict. Its worktree and branch are removed.
- **The owner's topic of 2026-11-22, 08:25, is researched and its working paper is with him since 09:02:** the
  encryption and integrity of the stored content, the delivery by signed links, several object stores, a
  Bastion that reads the bucket, and a hierarchy of keys. Four researches ran from 08:30 and ended at
  08:42, 08:47, 08:48 and 08:52 ("With the owner" holds his words, what each found, the paper's verdict and
  the development agent's favourites; two findings of the store's research are in section 8). No builder
  runs; nothing of the product changes until his answers are read back. **The walk through the open questions is moved to the evening
  by his word of 08:25;** they are listed in their order at the top of "With the owner", the first of them
  (Q-SIG-211) with a finding of that morning: a writer's read through the signature gate is recorded
  nowhere.
- **State on 2026-11-22 at 03:40: everything the owner named before he left on 2026-11-21 is built, released
  and running on his installation, and nothing is in work.** The ClamAV change with the client certificates
  of its endpoint; the registries' presets; the image analyser's move into the scanner unit, with the
  analyser out of the release image; signatures on artifacts in their three stages (detect, verify and
  show; Quader signs, with the signing identity, npm's registry signatures and the receipts; the gate per
  repository with its releases, previews and screens). No builder runs, no worktree of this run is left,
  the last pipeline run (44973617548, the repair of the usage counting) ended at 03:40 green on every job
  (the four shards in 34.1, 32.4, 33.5 and 34.2 minutes, shard b green with the repair; the layout job in
  44.0 minutes), and his installation runs that release since 03:15 with every Deployment ready. **What is
  his when he returns** (each is in "With the owner" or section 8, with what was built meanwhile): the
  walkthrough of the new screens (the signatures' settings with the signing identity, the gate in a
  repository's settings, a refused artifact and its release, the security dashboard's signature view, the
  step Verify of the client setup); the open questions of the signatures' second and third stage, among
  them who may read through the gate to sign an image (Q-SIG-211) and whether his CA issues the
  certificate a signing identity needs; what his installation needs before anything is signed or gated
  there (a signing identity; a repository with the gate switched on) and before a ClamAV client updates
  with a client certificate; two things on the development machine (the default kubeconfig's context, a
  Homebrew `trivy` older than the pin). **Not verified anywhere and named in the entries below:** the German
  screens in a browser; the gate's and the identity's screens against real state (their pictures are
  answers changed in the browser, the last package excepted, which its builder saw with a real identity);
  keyless signing; a rotation of the signing identity on a cluster; a real cert-manager and a real company
  CA. **Findings left for a batch** are in section 8 (among them: the linter's missing time limit in the
  make target, the script that picks the end-to-end legs, the restore drill's values, two fuzz jobs' names,
  the UI tests that wait a fixed time for the person search).
- **Landed on 2026-11-22 at 02:54: the cause of two red runs of `integration (shard b)` was a defect of the
  product, in the usage counting since 2026-11-19, and it is repaired** (a commit of the type `fix`; its
  agent worked from 02:28 to 02:50). In two of the three runs that ended since the fourth shard was cut,
  one test of the area `limits` had failed, a different one each time: at 00:35 the test of section 12 B
  (one request charged to an address that is none of the test's own), and at 02:16, in the run of the
  receipt attestations (44967924325), the test that a fetch through a team's virtual repository records
  that repository. **One cause for both, and neither the night's changes nor the fourth shard:** the charge
  buffer wrote a request's count into the usage store at the first flush after the request was admitted,
  under the caller and the repository known at that moment. A flush that fell into the few milliseconds
  before a token request's handler had authenticated its caller put the request under the client's
  address; one that fell before a virtual repository's member had answered put it under the virtual
  repository and left the member's row with the bytes and no request. The windows are 5 to 15 ms of a
  flush every 200 ms, which is the one failure in thirteen runs its first builder had seen and could not
  reproduce. **A second defect beside it:** the step that moves a token request to its caller made a second
  charge, so every token request of a registry client was counted twice in the usage overview, and that
  charge stayed in the replica's buffer for the replica's life (a growth of memory with every token
  request; on the owner's installation too, since 2026-11-19, on every replica that serves the token
  route). **Verified by its agent:** both failures came back on the unrepaired code with the pipeline's own
  messages, the same row and the same byte counts, with the flush forced to a millisecond (for the
  reproduction alone); the double count at the normal flush (three charged for two requests); new unit
  tests that put a flush between the steps of one request fail on the unrepaired code. Inferred, because
  the pipeline's databases are gone: that this is what hit there. Not the tests' isolation (each test's
  stack has a database of its own) and not the runners' load (the area took 123 and 126 s with three
  shards, 127 to 130 s with four, and its measured waits are unchanged). **The repair:** a request's count
  and the limits it met reach the usage store at the flush after it ended, its bytes at every flush as
  before, and the move to the caller moves the request's own charge. The integration test asserts exactly
  two requests of the token where its "at least two" had hidden the double count; the developer page of
  the limits has the rule for test authors (which row a request lands in is tested at the unit tier with a
  flush between its steps, with exact counts). **By the development agent on the commit as it landed, each
  exit 0:** build, the full unit run, generate-check, the evidence, links and check, the complexity check,
  the lint; the area `limits` whole under the lock by its agent (114 s). The owner's UI backend started
  with it before the push. **Its release was cut at 03:07,** image and chart were published at 03:13, and
  **the owner's installation runs it since 03:15** (read on the cluster at 03:15: every Deployment ready, no
  restart, no error line; its replicas started fresh, so the buffers' growth is gone with them). **What the second recurrence taught:** the plan had carried the first
  failure as an open finding whose cause was not known, with the instruction to print the rows at the next
  one; the printed row and the replica's log were what made it solvable in twenty minutes. **Not known:**
  the windows' width on the runners; how much the buffers of long-running replicas had grown. **The rest of
  that run (01:42 to 02:27) is green:** the nine end-to-end legs, among them `oci` with the two cases of the
  real cosign on the pipeline's runner; the layout job in 44.9 minutes; the shards a, c and d in 35.0, 30.5
  and 34.9 minutes, shard b red after 33.4.
- **Landed on 2026-11-22 at 02:29: Quader's own signature and its receipt on the screens (the second half of
  stage 2's package 4), and with it the second stage and the whole of the signatures are on `main`** (the
  rest of R-SIG-055 and R-SIG-056; decisions Q-SIG-233 to Q-SIG-239, gap H-1241; its builder worked from
  01:43 to 02:27; no migration). **The API:** each entry of the signatures' answers that Quader issued
  carries who issued it, the receipt as read back from its stored bundle (the time of receipt, the origin,
  the signatures it names with their states, the malware state with its database, the vulnerability state
  with its grade; no second copy that could disagree, Q-SIG-234) and how it is served, as a referrer or
  through the API, with the reason for an image. **At a version, a file and an image:** npm's signature of
  this installation reads "Origin confirmed by registry · this installation" with its key and a sentence
  that it confirms where a version was published, not who built it; the receipt is a block of its own
  under **Attestations** with when it was issued, the malware verdict it holds, how it is served and why,
  and its download, the rest of the statement one fold further; neither carries a badge, and the sentence
  that no trust anchor knows the signer is gone from Quader's own entries. An artifact without a
  publisher's signature that lists Quader's or a registry's entries says "No signature of the publisher"
  where it had said that none was found (Q-SIG-235). A retired identity's entry says that it is no longer
  served. **The section "Signing identity"** shows the job's backlog in one quiet line. **Seen with a real
  identity:** on a stack of its own the builder imported an identity of a CA it made; the job signed 51 npm
  versions and issued 12 receipts, and the version's page in a browser showed the real entries, the
  receipt of an infected package with "Malware found", and the download of the bundle. **Its gates, each
  exit 0:** build, the full unit run, generate-check, the evidence, the complexity check, the lint, the
  UI's install, tests, lint, layout tests and build; under the lock the areas `signing`, `signatures` and
  `ansiblelocal`; the renders of every signature and signing screen at 1,440, 1,024 and 390 px, light and
  dark, none failed, no layout finding. **Looked at by the development agent** (rule 11): a local npm
  version with a publisher's provenance, Quader's signature and the receipt with its fold open (wide,
  light); an image with an older cosign signature whose receipt is served through the API with the reason
  in a sentence (1,024 px, light); and the builder's picture of the real entries on its stack. The blocks
  read in the order a person asks: who built it, who published it, what this installation saw. **By the
  development agent on the commit as it landed, each exit 0:** build, the full unit run, generate-check,
  the evidence regenerated without a difference, links and check, the complexity check, the lint, the UI
  tests (1,935) and lint, the layout tests, the build in both languages, the API's types and the
  translation source regenerated without a difference; the diff holds no name the white-label rule
  forbids; no change of the chart. The owner's UI backend started with it before the push. **Its release was cut at 02:42,** image
  and chart were published at 02:48, and **the owner's installation runs it since 02:50** (read on the cluster at
  02:50: every Deployment ready, no restart, no error line). Its run was stopped at its long jobs by the push
  of the repair above. **Its
  tests cost** milliseconds of unit tests, about 0.25 s of UI tests, and an estimated 6 s of the layout job
  (two states, 12 pictures; the job stands at 45 of its 75 minutes). **Not verified:** a real receipt
  served as a referrer, and one held back because of an older signature, in a browser (the renders and the
  integration assertions alone); the German texts in a browser. **A gap it names (H-1241):** a receipt
  that still waits for its checks is not shown at the artifact, only counted in the backlog line. Its
  worktree and branch are removed.
- **Landed on 2026-11-22 at 01:42: the receipt attestations (stage 2, package 3)** (R-SIG-044, R-SIG-051 to
  R-SIG-054 and their parts of R-SIG-055 to R-SIG-059; decisions Q-SIG-180 to Q-SIG-192, gaps H-1215 to
  H-1217; migration 00105; the format contract is at version 10; what it built is in its report, in the
  entry below). Its builder brought it onto `main` from 23:44 to 01:20, over four rebases while `main`
  moved. **Changed on the way, as sent back:** one mechanism for the rows Quader issues (its own four write
  statements are gone; a receipt is recorded through the statement npm's signatures use, attached with the
  source `issued`, which no batch names, and written only while signing is on and an identity is current);
  npm's line of the step **Verify** (`npm audit signatures`, for npm's client alone, because pnpm and Yarn
  have no such command); the cosign binary as an image of its own that the set `oci` alone copies onto its
  cluster (Q-SIG-192), so the OCI clients' image is back at 327 MB and the two other OCI sets load nothing
  of it. **Two faults it met in the merge:** its statement that drops an attestation would have taken npm's
  signature of the same tarball with it (limited to the attestation's own row before it existed on `main`);
  and on `main` the trust job's backlog did not leave out the rows Quader issues, so every run of the
  signature check would have queued the trust job again once an npm signature exists (dormant without an
  identity; closed by this commit). **The real tools, on a cluster of the builder's own:** `cosign
  verify-attestation` validates Quader's receipt of a pushed image and refuses another identity; and **the
  real `cosign sign` in a gated repository works** (a reader was refused with the gate's text before the
  signature and served 0.4 s after it), which confirms the repair of Q-SIG-211 that its builder could only
  show with plain requests. The current cosign needs a signing configuration without a transparency log
  for it, which the user manual's page says. `notation sign` was not measured again. Both cases are in
  the set `e2e (oci)` (about 4 and 3 s). **The gate's new readers and the receipts:** the preview counts
  the image and not the receipt's manifests, the file's answer lists the receipt as an attestation of this
  installation without a badge, and the gate names nothing of Quader's as what an artifact passed by
  (asserted in its integration test; the impact count and the refusals list by reading the code). **Its
  gates on the rebased commit, each exit 0:** build, the full unit run, generate-check, the evidence, the
  complexity check, the lint, the chart's lint and tests, the UI's install, tests, lint, layout tests and
  build; under the lock the areas `signing`, `signatures`, `ocilocal`, `npm`, `ansiblelocal`, `ociremote`,
  `malware`, `vulnrestore`, `repository`, `upstream`, `postgres`, `packages` and `backup`. **By the
  development agent on the commit as it landed, each exit 0:** build, the full unit run, generate-check,
  the evidence regenerated without a difference, links and check, the complexity check, the chart's lint
  and tests, the UI tests (1,931) and lint, the lint. **Looked at** (rule 11): the step **Verify** of a local
  npm repository as a power user (wide, light) and as a plain user (narrow, dark): the client's own command
  first, then the three lines that fetch the trusted root and the receipt and verify it offline, the values
  to replace marked, long lines wrapped inside their window. The diff holds no name the white-label rule
  forbids; no change of the chart or of a dependency, and **the owner's values** render as before (39
  objects). The owner's UI backend started with it before the push (the schema step applied). **The run before it (44964287638, the gate's screens) ended at 01:42, green on every job:** the layout job with the gate's 186 pictures in 43.3 minutes of its 75, the four shards in 33.0, 31.8, 31.2 and 32.9 minutes (shard b green: the test of section 12 B passed this time). **Its release was cut at 01:54**; the unit job took 9.9 minutes. Image and chart were
  published at 02:01, and **the owner's installation runs it since 02:02** (read on the cluster at 02:02:
  every Deployment ready, no restart, the migration job applied the schema step, no error line of `api`,
  `packages` or the workers). No signing identity is configured there, so no receipt is made.
  **Its tests cost** about 4 s of the unit job (three new packages' starts), about 9 s in shard d, the two
  cases in the leg `oci`. **Not verified:** an admission controller with a certificate chain;
  `cosign verify-blob-attestation` of a file's receipt with the real tool (the verifier library alone); an
  image's index with its platforms, and a remote or virtual image's receipt, beyond the unit tier and the
  kit; the wait for a pending scan (the integration stack runs without the advisory source). **Gaps it
  names:** a remote or virtual image's receipt is served through the API only (H-1215); a retired
  identity's referrer stays until the job makes it again (H-1216); a virtual repository's list of
  referrers does not leave Quader's out when another member holds an older signature tag (H-1217). Its
  worktree and branch are removed.
- **The reports of the receipt attestations (23:43) and of the repair of the first stage's defects (00:17)** follow
  here; both are landed (the entries above, 00:44 and 01:42), as are the screens of the gate (00:57).
  **The attestations' report** (stage 2, package 3; one commit on the base of 21:47): an in-toto statement
  with a predicate of Quader's own in a bundle with the leaf certificate, made by a kind of the job `signing`
  for every stored file of a format that attests something, after the checks' first verdicts or the bound,
  made again on a malware verdict, a rotation or a retirement, gone with its item; kept in PostgreSQL for
  every format and served by a route of the API, for an image of a local repository also as a referrer in
  the current cosign's form, which a stored older signature tag of the same digest removes in its own
  transaction; the format contract's method for all seven formats and its kit check (contract version 10);
  the client setup's step **Verify**. **The real client:** `cosign verify-attestation` on a cluster of the
  builder's own printed "The cosign claims were validated" for Quader's receipt of a pushed image and
  refused another identity with the mismatch named. **Found out, which the specification had left open:**
  the verifier library takes the trusted root Quader builds, without logs; beside Quader's referrer the
  current cosign fails to verify an older signature tag of the same digest, and one deprecated flag that
  its help does not list gives the older reading back. The cosign binary adds 136 MB to the OCI clients'
  image. **Sent back at 23:44 with:** the rebase onto npm's signatures (one mechanism for the rows Quader
  issues, not two), the gate's second package and the fourth shard; its migration as 00105; npm's line of
  the step **Verify**, which npm's builder had left to it; one more end-to-end case, the real `cosign sign`
  in a gated repository (the read the gate's repair opened); a check that the gate's new readers count no
  receipt as a publisher's signature; and whether cosign reaches the leg in a smaller way than 136 MB in
  the image every case loads. **The screens of the gate** (stage 3, package 3, R-SIG-074): the repository's
  setting with its preview and its warning, a refused artifact with the reason and the release, the
  confirmation with the impact count, the dashboard's lists; rendered at every width, with eight pictures
  named for the development agent.
  **The repair of the first stage's three defects reported at 00:17 of 2026-11-22** with one commit on the
  tip of `main` (decisions Q-SIG-216 to Q-SIG-219, gap H-1233, no migration, no new dependency); it waits for
  the running pipeline to end and gets one addition meanwhile. **NuGet:** the reader of the signature file
  is Quader's own now, on the language's ASN.1 package (the library in use and its two alternatives take a
  signer named by issuer and serial number only, and both real tools name it by its key); it reads both
  forms, the countersignatures and the timestamp, in DER and BER. **Proven with what the real tools wrote:**
  a package signed by `dotnet nuget sign` (7.7 KiB) reads valid and trusted under its CA, and the signature
  file of a public package from nuget.org (24 KiB) yields the author's signature, nuget.org's
  countersignature and a verified timestamp. **Why the vectors had missed it:** every NuGet signature of the
  tests was made in process by the same library that read it, which writes only the one form it reads. **A
  second defect behind the first:** nuget.org's countersignature then read as weakly signed, because its
  chain ends at a root that signed itself with SHA-1 in 2006; a self-signed certificate's own signature is
  no longer judged, only its key (Q-SIG-218; it holds for every scheme that checks chains). **Ansible:** a
  batch replaces the attachments of the sources it names and no others (Q-SIG-217), so discovery's batch no
  longer removes a posted signature; the exception npm's package had made for Quader's own signatures became
  a case of that rule, and npm's provenance is attached as what its gate already takes it for. Its
  integration test fails on the old code. **The remote image's version:** the version's answer and the
  list's badge also read the document a repository lists a version by (Q-SIG-219); the other formats'
  remotes hold a version's file as an item and already worked, and a virtual repository now shows a remote
  member's signatures too. **Its gates, each exit 0:** build, the full unit run, generate-check, the
  evidence, the complexity check, the lint; under the lock the areas `ansiblelocal`, `signatures`, `signing`,
  `nuget`, `ociremote`, `ocilocal`, `npm`, `packages` and `upstream`. **A gap it names (H-1233):** the
  libraries that read the timestamps of the signing service's bundles and of Notation have the same limit;
  dormant, because the tools that write those timestamps use the same libraries. **Asked of it at 00:18,
  before the landing:** fuzz targets for the readers it wrote (they parse bytes of anyone who may upload a
  package or of any remote), seeded with the real-tool fixtures, run for two minutes each, and listed in
  the nightly's fuzz job. **What an installation that was hit shows after the repair:** a posted signature
  that was lost stays lost until it is posted again; a remote image's version shows its signatures at once;
  whether an old NuGet row is checked again is asked of the builder. **Not verified:** the real clients
  against the repairs (dotnet against a gated repository, `ansible-galaxy`, Docker); nuget.org's
  countersignature under the shipped anchors.
- **Landed on 2026-11-21 at 23:44, with the two packages below: a fourth integration shard** (a commit of the
  type `ci`; its builder worked from 23:23 to 23:42; decision Q-ARCH-565). **The rule,** in place of a list:
  the suffix decides first (every `<format>local` in a, every `<format>remote` in c, every
  `<format>virtual` in d, so a format's three areas stay on three shards), then the name (a: coordination,
  mirror, npm, NuGet, proxy; c: api, backup, restore, storage, postgres, blob, valkey, the Bastion's areas
  and the OCI conformance suite; d: `ci`, the vulnerability scanning's areas, `malware`, `signatures`,
  `signing`; b: everything else, so every new platform area). **By the packages' times of the day's last
  whole run** (72 packages; the table is on the test-cost page) the four shards take about 32, 31, 33 and 31
  minutes where three took 38, 43 and 42; the development agent's own candidate (the virtual areas and the
  signatures alone in d) would have left a at 38 and c at 42 and was dropped by the numbers. **What it
  costs:** about 3.5 runner minutes more per push on the small runners (a shard's fixed part is about three
  minutes of checkout, build and stack); the layout job (42 minutes) is then the push's longest job. A new
  test of the pipeline's files asks the Makefile for each shard of the workflow's matrix and fails when a
  package runs in none or in two (about 3 s of the unit job). **Checked at the landing:** that test, the
  check of the workflow files and the evidence on the stack, each exit 0. **Not verified until its first
  run ends:** the real minutes. Its worktree and branch are removed.
- **Landed on 2026-11-21 at 23:44, two packages in one push: npm's registry signatures (stage 2, package 2) and
  the gate's releases, previews and API (stage 3, package 2), with the repair of three findings of the
  measurement.** Both builders reported at 23:21; the three commits were stacked (npm's, then the gate's
  feature and its repair). **Done at the landing by the development agent:** one sentence of the user manual
  merged from both sides (npm's new behaviour and the measured output without an identity), two developer
  pages with both sides kept, and the gate's migration renumbered from 00103 to 00104 behind npm's (the
  file, three pages, one comment in Go, the commit message; no logic).
  **npm's signatures** (R-SIG-050 and the npm half of R-SIG-048 and R-SIG-049; decisions Q-SIG-156 to
  Q-SIG-163, gaps H-1204 and H-1205, migration 00103; what it built and the real client's proof are in the
  entry of its report below). Its builder brought it onto `main` from 22:11 to 23:21; only documentation
  conflicted. **Asked at the send-back and answered:** Quader's own signature never passes the gate, for
  three reasons at once (its role is a registry's, its subject is the signed message and not the tarball,
  and it carries the identity), shown by one more step of its integration test (a gated local repository
  refuses the signed tarball with the reason `missing` while the document still shows Quader's signature;
  0.13 s); the version's tab had shown Quader's signature under "Origin confirmed by registry" but with a
  badge "signer not trusted" of its own, corrected in one line (no badge on an entry Quader issued) with a
  UI test of 22 ms; the sentence "No trust anchor knows this signer" stays on that entry until the screens'
  second half gives it its line. **Its gates on the rebased commit, each exit 0:** build, the full unit run,
  generate-check, the evidence, the complexity check, the lint (484 s), the chart's lint and tests, the UI's
  install, lint and tests (the tests on the second run: one of the person-search tests failed at a load of
  27, section 8); under the lock the areas `signing`, `signatures`, `upstream`, `packages`, `repository`,
  `ansiblelocal`, `ocilocal`, `ociremote`, `postgres` and `backup` green, and **the area `npm` red once:**
  one subtest that stops Valkey got 503 `database_unavailable` while the replica's readiness checks timed
  out at a load above 30 (the area took 959 s where it had taken 364), and passed alone in 15 s; the
  changed publish path writes nothing without an identity.
  **The gate's second package** (R-SIG-070 to R-SIG-073, R-SIG-075, R-SIG-076; decisions Q-SIG-196 to
  Q-SIG-205, gaps H-1223 to H-1225, migration 00104; its builder worked from 21:40 to 23:21): a holder of the
  triage right who may read the repository releases one artifact for that repository's gate with a reason
  and withdraws it (a release can be granted before the gate is switched on and for a pending artifact; it
  is read only where the signatures do not pass, so a passing download costs nothing more; a released index
  passes its platform manifests; the purge of a retired repository keeps releases); the impact of a change
  of an anchor, a block or the switch runs the gate's own decision twice and is bounded, with the bound said
  in the answer; the preview of a repository reads its stored items in batches and answers counts per
  reason with up to twenty examples; the API's lists of refusals and releases and the members `refused` and
  `gate` on the answers the screens need; the events of a release, the Bastion's feed for every change that
  narrows what a gated repository serves; the table in backups and the withdrawal in the journal. **Gaps it
  names:** a release keeps passing its artifact after the signer is blocked later (H-1223); a virtual
  repository's `refused` and the version list show one gate each (H-1224); the impact of blocking an
  identity of a signing service is a lower bound beyond the preview's limit (H-1225). **Its tests cost**
  about 6 s in the integration area `signatures`, nothing measurable elsewhere.
  **The repair of the measurement's findings 2, 6 and 7** (a commit of the type `fix`; decisions Q-SIG-211 to
  Q-SIG-214, gap H-1229, ADR-237): a principal with the right to deploy to the repository reads through the
  signature gate where a format declares that its signing tools read the artifact first, which OCI alone
  does; the right is asked only after a decision that is no pass, the malware gate and the vulnerability
  block are untouched, and the user manual has "Sign an image"; every reason's text speaks of "the
  signature of its publisher or its build"; the answers name the artifact as its client does (`name:tag` or
  the digest reference, `name@version`, an id and a version, a collection and a version, a file's path). The
  format's signature declaration gained two members for it, so the contract's surface changed and its
  version stays 9 (ADR-237). The header NuGet's client might print was not tried (not cheap), and nothing
  is sent in it (Q-SIG-214). **Not verified by its report:** `cosign sign` and `notation sign` were not run
  again after the repair, only the reads they make, with the pusher's token (200) and a reader's (403); the
  new texts were not looked at in a real client; the feed's statement that lists gated repositories runs in
  no test; a preview of a virtual repository and of a remote's fetch state at the unit tier only. **The
  real `cosign sign` in a gated repository** becomes a case of the end-to-end set `oci`, asked of the
  attestations' builder, who brings the cosign binary into that leg.
  **By the development agent on the stack as it landed, each exit 0:** build, the full unit run,
  generate-check, the evidence regenerated without a difference, links and check, the complexity check, the
  chart's lint and tests, the UI tests and lint, the lint (with a stated limit). Under the lock on the stack, each exit 0, the two packages' combination that neither builder had run: the areas `postgres`, `signatures`, `signing`, `upstream`, `ocilocal` and `packages` (23:23 to 23:46, most of it the wait for the lock the builders share). The three diffs hold no
  name the white-label rule forbids; the chart is untouched, so the owner's values render as before. The
  owner's UI backend started with it before the push (both schema steps applied). **Their run stood in the queue for 17 minutes** with no job offered to the runners (section 8); cancelled and started again on the same commit at 00:01 of 2026-11-22, it ran at once. **Their release was cut at 00:13**; the unit job took 9.9 minutes. Image and chart were
  published at 00:19, and **the owner's installation runs it since 00:22.** Read on the cluster at 00:23:
  every Deployment ready, no restart, the migration job applied both schema steps, no error line of `api`,
  `packages` or the workers. No signing identity is configured there, so npm's signatures are not made, and
  no repository has the gate switched on. Their worktrees and branches are removed. **Their run (44958303829,
  its second attempt) ended at 00:44, green on every job but `integration (shard b)`:** the nine end-to-end
  legs green (`npm` 23.4 minutes with the real `npm audit signatures`, `oci` 18.8, `nuget` 20.1, `vuln` 16.5);
  the layout job green in 41.7 minutes; **the four shards in their first run took 33.1, 32.7, 30.0 and 32.8 minutes**
  (a, b, c, d; the model had said 32, 31, 33 and 31), where three had taken 38.2, 43.4 and 42.1. **Shard b
  is red on one test that this push did not touch,** the one of section 12 B that had failed once in
  thirteen runs of its builder: `TestR_LIM_001` found one request charged to an address. It printed the row
  this time (the loopback address, at the instance's level, no repository, not refused), and the replica's
  log of that test shows its three requests each charged to its user or its technical token, so the row is
  none of that test's own requests. Not repeated: the next push runs the shard again.
- **The measurement with real clients ended at 22:42 and landed at 22:43** (one commit of documentation; its
  builder worked from 21:48 to 22:42 with a Quader and a compose project of its own, the clients from the
  end-to-end sets' images, and a kind cluster of one node for the kubelet's lines; nothing of it is left
  running). The commands, the outputs and the exit codes are on the developer page of the signatures
  ("Measured with real clients"); the specification's requirements carry what was measured, and the manual
  is corrected where a real output differed (among them: `--required-valid-signature-count 1` of
  `ansible-galaxy` installs an unsigned collection without a word, `+1` does not). **What works with the real
  tools:** cosign 3.1.4 with a key pair, an attestation and the older tag scheme, Notation 1.3.2, GnuPG and
  `cosign sign-blob` siblings, npm's public registry signatures and provenance: read, verified, trusted once
  the anchor is added, and each client verifies through Quader; Docker pulls a multi-platform image on its
  index's signature through a gated repository; the kubelet shows `denied:` with the gate's text, Docker and
  npm show the text. A certificate chain given to the current cosign makes it write a bundle that cosign
  itself cannot verify (the tool's fault, noted in the admin manual with the flag that avoids it). **Seven
  findings, each reproduced (section 8, Product, and the developer page with the place in the code):** (1) no
  real NuGet signature is readable, neither `dotnet nuget sign`'s nor nuget.org's (they name the signer in a
  way the parser does not take; the unit tests signed the other way); (2) an image pushed into a gated OCI
  repository cannot be signed there, because the signing tools read the manifest first and are refused; (3) a
  signature posted for a new Ansible collection is deleted from the store by the worker's discovery while the
  version's document keeps serving it; (4) a remote image's version view lists none of the signatures its
  fetch stored; (5) `dotnet restore` and `ansible-galaxy` never show the gate's text, and Docker shows none
  for a pending image; (6) the text of the reason `missing` says "no signature" beside a registry's
  signature; (7) the texts name storage paths. **Not measured:** keyless signing (it needs a sign-in or a
  signing service of its own), so npm's pass through the gate rests on public provenance alone; chosen
  anchors and blocked signers, a virtual repository's gate, symbol packages, Notation's second envelope
  format, Podman. **What happens with the findings:** (1), (3) and (4) are defects of the released first
  stage, (3) loses what a user uploaded: **one builder repairs them since 22:44** (the reading of the
  signer for the author's signature, the countersignature and the timestamps, proven with a signature the
  real tool made; a batch replaces the attachments of its own source only, for every format; the version
  view for remote repositories of every format). (2), (6) and (7) went at 22:44 to the builder of the gate's
  second package, who is inside that code, as a second commit. (5) is the clients' behaviour; one untried
  way for NuGet (a header its client prints) is tried by that builder and otherwise left. **Decided by the
  development agent for (2), put to the owner ("With the owner"):** who may write to the repository reads
  through the signature gate where a format's signing tools read the artifact from the repository, which is
  OCI (Q-SIG-211). Its worktree and branch are removed.
- **Started at 21:40 and 21:48, three builders on the stack of 21:47; the gate's second package landed at 23:44 and
  the measurement at 22:43 (the entries above), the attestations are in work** (with npm's signatures that made
  four):
  **package 3 of the signatures' second stage, the receipt attestations** (R-SIG-051 to R-SIG-054: the statement
  and its predicate, the bundle with the trusted root and the chain, the kind of the job `signing` that makes,
  renews and removes them, the API route and an image's referrer where it hides no older signature, the format
  contract's method for every format, which makes the contract version 10 because the gate took 9, the client
  setup's tab **Verify**, and the end-to-end case with the real `cosign verify-attestation`, with what the
  cosign binary adds to the clients' image measured); **package 2 of the third stage, releases, previews and
  the API** (R-SIG-070 to R-SIG-073, R-SIG-075, R-SIG-076: the release of one artifact for one repository, the
  impact count before an anchor or a block changes, the preview before the gate is switched on, the refusals'
  operation, the Bastion's feed, backups; no screen); **the measurement with real clients** that the first
  stage and the gate's package had left as owed (signatures made by cosign with a key and with a CA's
  certificate, by notation, `dotnet nuget sign`, `ansible-galaxy` and gpg, read by Quader and verified by the
  clients through Quader; and each format's client against a refusal, a pass and the pending answer of a
  gated repository, the `denied:` line of Docker and of a kubelet among them): one documentation commit with
  the outputs, and every defect as a finding with its reproduction, fixed by no one before it is read.
  **Learnt at their start:** a builder's worktree is cut from the pushed `main`, so the two that build on the
  stack were cut three commits short while it was merged and not pushed; one stopped at the brief's base
  check and said so, and both were told to fast-forward to the stack's commit. A brief that needs commits
  which are merged and not pushed now says so with that command.
- **The run of the signing identity (44943728576) ended at 21:42, green on every job but `integration (shard b)`,
  which holds no release back; its repair landed with the push of 21:47.** The layout job is green (40.9
  minutes; its first whole run with the seed's repair, and the first that ended since 18:27); the end-to-end
  legs `npm`, `push`, `ci`, `bastion` and `access-long` green; the shards a and c green in 37.8 and 42.2
  minutes of their 45, shard b red after 41.0. **The cause:** one test of the area `repository` expects the
  task that re-encrypts sealed secrets to walk two columns; the signing identity registered a third on
  purpose (the sealed key of an uploaded identity, R-SIG-047), and its builder had run the areas `postgres`,
  `signing`, `signatures` and `backup`, not `repository`. No fault of the product; the new area `signing` is
  green in that shard (11.5 s). **The repair** (one builder, 21:41 to 21:46, a commit of the type `test`): the
  expectation names the three columns (a repository's secrets, a Debian repository's signing key, the
  identity's key), confirmed at the task's handler; two developer pages that still described one column are
  corrected; the test alone and the whole area `repository` under the lock each exit 0 (146 s). No other test
  counts the columns. **This is the fourth check of a list in an integration area that a landing made red
  that day** (section 12 A). **For every builder's brief from now on:** a change of what a task, a job or a
  registry walks, counts or lists is followed by a search for that name under `test/integration`, and the
  areas found are run beside the feature's own.
- **Landed on 2026-11-21 at 21:47, three packages in one push: package 3 of the image analyser's move, package 1 of
  the signatures' third stage (the gate), and the section "Signing identity" of the second stage's screens.**
  One push, because every push stops the run before it and the long jobs had not ended since 18:27: the three
  commits were stacked in the order of their reports (the analyser's at 21:04, the gate at 21:27, the section
  at 21:30; each rebase without a conflict) and pushed when the run of the signing identity had ended at 21:42 (the entry of that run).
  **The gate** (R-SIG-061 to R-SIG-069, R-SIG-073, R-SIG-075 and R-SIG-076 as far as its package goes;
  decisions Q-SIG-131 to Q-SIG-144; migration 00102; the format contract is at version 9; what it built is in
  the entry of the three builders below): its builder brought it onto `main` in two rebases from 19:52 to
  21:27. Added on the way: **Quader's own signatures never pass** (R-SIG-062: the gate reads the column the
  identity brought and skips such rows, tested at the unit tier with three rows; today a second guard,
  because the second stage's own signatures carry roles the gate's statement leaves out already); **one rule
  with the screens for a well-known repository** (Q-SIG-138 with Q-SIG-128: the whole member `signatures` of
  a well-known repository of the project `upstream` is an administrator's, on a system repository the
  product holds it). By its builder on its tip, each exit 0: build, the full unit run (142 s),
  generate-check, the evidence, the complexity check, the chart's lint and tests (173 s), the UI tests and
  lint, the lint with a stated limit (417 s); under the lock the areas `upstream` (60.6 s), `signatures`
  (55.0 s), `postgres` (142.0 s) and `backup` (602.3 s). Red before its second rebase and green on the tip,
  each at a host load of 70 to 82: the lint with a tool error, one test of the area `ociremote` that waits
  15 s for a background fetch, four tests of the area `npm` whose migration timed out. Its tests cost, in
  the job's mode: 0.49 s for nine tests of the gate's package, two defect cases of 1.4 s each in the test
  format's kit, nothing in the API's package (57.9 s as a whole). Owed and not this package's: the
  measurement by hand with real clients (R-SIG-078).
  **The section "Signing identity"** (R-SIG-056's first point; decisions Q-SIG-170 to Q-SIG-176, gap H-1210:
  an imported key passes through the administrator's browser; its builder worked from 21:01 to 21:30; no
  change of Go, of the API or of the schema): every state the specification lists. None, with **Create
  signing key** (a dialog whose fields hold the common name and the base URL as the URI) and the import
  behind **More options**; a pending key with its request in a file window, **Copy**, **Download**, **Upload
  certificate** and **Discard**, there again after a reload; uploaded, with the facts, the validity amber
  from 14 days and red when expired, **Download chain**, **Download trusted root**, **Replace** and
  **Remove**; from the Helm values without controls, a refused pair of files as a red line with its check in
  plain words; previous identities closed, each with **Retire** and its question; read-only without the
  right. Every refusal of the API has plain words. Decided by its builder: every form is a dialog and the
  request stays in the section (Q-SIG-170); one URI and one e-mail address in the form (Q-SIG-171); the
  import is offered beside an uploaded identity too, as a way to renew (Q-SIG-172); a refusal reads as the
  check in plain words, then the server's detail (Q-SIG-173); a write that needs a recent sign-in keeps the
  key request and the certificate as a draft, never an imported key (Q-SIG-174); the renders take each state
  from the profile's real answer changed in the browser, because an uploaded row would need a key sealed by
  the profile's key ring (Q-SIG-175); beyond the specification's point a note while signing is switched off,
  the announced identity, and "Not in use" on a stored upload under the Helm values (Q-SIG-176). Its gates,
  each exit 0: the UI's install, tests, lint, the layout checks' own tests, the build in both languages, the
  evidence; the renders of the section as the administrator and the auditor and of the whole settings page
  at 1,440, 1,024 and 390 px, light and dark: 114 pictures, none failed, no layout finding (its first run had
  found 15: two copy buttons off their line and an icon stretched over a paragraph, corrected). **Looked at
  by the development agent** (rule 11): the uploaded state with two previous identities and "expires in 12
  days" (wide, light), the key request (1,024 px, dark), the refused pair (narrow, light), the dialog with its
  options open (wide, dark) and the Helm state read-only (wide, dark); the section reads as the page's other
  sections do, the facts line up, and the narrow width puts them under their names. Seen and left: a
  previous identity's expiry stands in plain grey where an expired anchor's date on the same page is amber.
  **Corrected at the landing by the development agent:** the extracted translation source was three lines
  behind the template in its location notes (61 lines of the generated file, no text); extracted again into
  the commit. Its tests cost 0.3 s of UI tests and about 30 s of the layout job (54 pictures). **Not
  verified:** the German texts in a browser; a real round trip through the screen (a key request, a real
  CA's certificate, the upload); the Helm state and a refused pair against a pod that mounts a Secret (the
  renders change the answers in the browser); the recent sign-in's way through the dialogs in a browser; the
  two downloads.
  **By the development agent on the stack as it landed, each exit 0:** on the gate's commit over the
  analyser's build, the full unit run, generate-check, the evidence regenerated without a difference, links
  and check, the complexity check, the chart's lint and tests, the UI tests (1,916) and lint, the lint; on the
  section's commit over both the UI's install, the API's types regenerated without a difference, the UI
  tests, lint, layout tests and build, the evidence and its count test. The three diffs hold no name the
  white-label rule forbids. **The owner's values** render against the stack's chart (39 objects; the scanner
  pods gain the analyser's init container and its volume, the gate changes no object). The owner's UI
  backend started with it before the push (the schema step applied). **Their release was cut at 21:58.** On the pipeline's amd64 runner the image's build (3.6 minutes), the versions check and the job that scans the release image with the analyser's binary taken out of the pinned image (4.7 minutes) are green, which the analyser's builder could not verify; the unit job took 10.8 minutes. Image and chart were published at
  22:04, and **the owner's installation runs it since 22:06.** Read on the cluster at 22:06: every Deployment
  ready, no restart, no error line of `api`, `packages` or the workers; the migration job applied the gate's
  schema step. **The analyser's init container on real amd64 nodes with CRI-O, which nobody had run before:**
  on both nodes the copy took a second and ended with exit 0, the scanner's listener was bound 0.2 s after its
  container started, its readiness check `vulnScannerTrivy` passed, and the pods were ready 4 and 2 s after
  the copy began. Both nodes report the analyser's image as present; the pull itself is in no event that is
  still listed, so its duration there is not known. Their worktrees and branches are removed. **Their run
  (44948103795) ended at 22:34, green on every job,** the first run of 2026-11-21 that ended whole and
  green: the end-to-end leg `vuln` (17.2 minutes) with the analyser copied from its own image; the legs of
  every format's client (`oci` 20.6, `npm` 23.6, `nuget` 16.2, `deb` 12.6, `ansible` 14.7 minutes) and `push`,
  `ci` and `access-long` with the gate in every format's serve path; the layout job with the identity's
  section (42.1 minutes of its 75); the integration shards in 38.2, 43.4 and 42.1 minutes of their 45, shard
  b green again with the repaired test. **Shard b stands six seconds under the mark of 43.5 minutes** at
  which a fourth shard is cut (section 12 A), and the packages in work add to it; the fourth shard is
  started as a change of the pipeline as soon as one of the four builders has ended.
- **Landed at 23:44 (the entry of the two landings above; in work since 21:01 on `main` with the signing identity):
  package 2 of the second stage, npm's signatures; the
  section started with it landed at 21:47 (the entry above)** (two builders, with the gate's rebase and package 3
  of the analyser's move that made four): **package 2 of the signatures' second stage, npm's registry
  signatures and the local keys endpoint** (R-SIG-050 and the npm half of R-SIG-048 and R-SIG-049: a signature
  on every version of a local npm repository, the keys endpoint with the announced, the current and the
  previous keys, the kind of the job `signing` that signs at a publish and finds what was stored before an
  identity existed, a virtual repository's keys with its signed local members, and the end-to-end case with
  the real `npm audit signatures` on a cluster of its own); **the first half of package 4, the section
  "Signing identity" of the signatures' settings page** (every state of R-SIG-056's first point: none, a key
  request, uploaded, from the Helm values, a refused pair of files, previous identities; rendered at every
  width of the job, light and dark, with six pictures named for the development agent to look at). **Cut by
  the development agent:** the screens' package is built in two halves, because its section needs the
  identity's API alone and gives an administrator the upload way now (until then that way exists in the API
  only), while Quader's own signature and attestation in the details of a version need packages 2 and 3; the
  second half follows them. **Not started and why:** package 3, the attestations, changes the format
  contract, which the gate changes too; it starts when the gate is on `main`. Each was told the numbers
  reserved for it (decisions, gaps, an ADR), that a migration is numbered behind the gate's at the landing,
  and a stated time limit for the lint.
  **Package 2, npm's signatures, reported at 22:11** with one commit on the base of 21:01; since 22:11 its
  builder brings it onto today's `main` (the gate, whose migration number it had taken locally, the
  analyser's move, the identity's section). **What it built:** every version of a local npm repository gets
  a registry signature (ECDSA P-256 over name, version and integrity) in the package documents, stored as a
  signature row that is marked as Quader's own, attached with a source of its own and never queued for a
  check; a local repository's keys endpoint serves the identity's keys, and a virtual one adds them once for
  its first enabled local member, which closes H-1176 for an installation with an identity; a publish writes
  its work in the commit's transaction, and a pass signs what was published before an identity existed; on
  a change of the identities in use every affected package's revision is raised, 1,000 per transaction, and
  a retired identity's versions are signed again; the signatures' answers of the API name the source and
  the identity; the set in use goes through backup and restore. **The real client:** `npm audit signatures`
  of npm 12 on a cluster of the builder's own, through a virtual repository with a signed local package and
  an upstream one: "2 packages have verified registry signatures" (its case alone, run twice; the client
  first tried the public signing service's key distribution and then fetched the keys from Quader).
  **Decided by its builder (Q-SIG-156 to Q-SIG-163):** the npm format reads keys and signatures through its
  own data port and the format contract stays as it is; one operation signs every due version of a package
  and raises its revision once; a publish writes work only while signing is on and an identity is current;
  a document serves signatures by the identity's state, read in the document's own statement; every run of
  the job compares the served set and raises the affected revisions; npm's signing writes through its own
  store whatever the switch of the first stage says; a signature made again after a retirement removes the
  retired one; the client setup's line for npm comes with the attestations' tab. **Two gaps it names:** a
  cache outside Quader can hold a document with a retired key's signature after the keys endpoint dropped
  the key, and npm then aborts (H-1204); until the job's next run two replicas can serve different
  signature sets under one validator (H-1205). **Its gates:** build, the full unit run, generate-check, the
  evidence, the complexity check and the lint each exit 0; under the lock the areas `signing`, `signatures`,
  `postgres` (310 s), `upstream`, `backup` (442 s), `npm` (364 s) and `packages` green. **Its tests cost**
  under 0.1 s of unit tests, about 5 s in the integration shard b, about 5 s in the leg `npm`; no new leg, no
  client image. **Sent back at 22:11 with:** the rebase (its migration becomes 00103, behind the gate's), a
  check that its own rows never pass the gate, what the screens show today for a signature of Quader's own
  (it must not read as a publisher's or carry the badge before the screens' second half gives it its line),
  the search under `test/integration` for what its change made longer, and the area `repository` with the
  others. **Not verified by its report:** whether `npm audit signatures` works without reaching the
  internet (H-1184 stays open); a rotation seen by a real client; the two new gaps, which are reasoned and
  not reproduced; the raise of revisions beyond one batch of 1,000 packages.
- **Landed on 2026-11-21 at 20:58: package 1 of the signatures' second stage, the signing identity** (R-SIG-045
  to R-SIG-049, R-SIG-055, R-SIG-057 to R-SIG-059, R-HA-071; decisions Q-SIG-145 to Q-SIG-155; what it built is
  in the entry of the three builders below, as reported at 19:58; its builder brought it onto `main` from
  19:58 to 20:38). Nothing is signed with it yet. **The order of the two waiting packages was turned:** the
  gate still had its integration areas to run at 20:38, so the identity landed first with migration 00101
  and the gate takes 00102; the development agent renamed the file and its references (the migration's
  header, two comments in Go, the developer pages, the commit message; no logic), and the gate's builder was
  told at 20:39 and again at the push. **By its builder on the rebased commit, each exit 0:** build, the full
  unit run, generate-check, the lint, the complexity check, links and check, the chart's lint and tests (428
  s under load), the UI tests (1,909) and the UI lint; under the lock the areas `postgres` (179 s; the third
  unregistered column of its first report is the one that area's own test adds, no finding), `signing` (11.3
  s), `signatures` (110.6 s) and `backup` whole (438 s). **Its unit tests cost,** in the job's mode: about 5.4
  s in the three packages of `internal/signing`, and under a second for its two tests of the API's package on
  the shared parse (that package as a whole 73 s). **By the development agent on the commit as it landed,
  each exit 0:** build, the full unit run, generate-check, the evidence regenerated without a difference,
  links and check, the complexity check, the chart's lint and tests, the UI tests and the UI lint. **The lint
  passed the tool's default time limit under a host load of about 70** (it had printed no issue) and exited
  0 with a stated limit of 40 minutes, which every builder's brief now names. The diff holds no name the
  white-label rule forbids. **The owner's values** render against its chart with no difference in any object
  (39 objects): nothing of the identity renders unless a value names one. The owner's UI backend started
  with it before the push (the schema step applied). **Its release was cut at 21:10,** image and chart were
  published at 21:17 (the release workflow had waited seven minutes for a runner), and **the owner's
  installation runs it since 21:19.** Read on the cluster at 21:19: every Deployment ready, no restart, the
  migration job applied the schema step in ten seconds, a worker scheduled the task `signing`, no error line of
  the workers. No identity is configured there, so nothing is signed. **Not verified:** a real cert-manager, a real
  company CA, a rotation on a cluster, a whole backup restored into a fresh installation. Its worktree and
  branch are removed.
- **Landed on 2026-11-21 at 21:47 (the entry of the three landings above; reported at 21:04, in work since 19:45):
  package 3 of the image analyser's move** (on `main` with packages 1 and 2 released): the
  analyser comes from its own image through an init container that copies the binary into a volume the scanner
  mounts read-only, and leaves the release image; one pin by digest in place of the version and the four
  archive digests, the chart's value held equal by a chart test, one Renovate group, the build and release
  workflows and the job that scans the release image, the development profiles that start the scanner units
  only where a leg needs them. Its brief asks for exactness at the pipeline and the release image (the check
  of the workflow files, the image built and looked into, no second pin anywhere), the copy measured in a real
  pod, the end-to-end set `vuln` on a cluster of its own with a kubeconfig file of its own (so that the
  machine's default kubeconfig is not touched again), and a unit test's cost measured in the job's mode. With
  the signing identity and the gate that made three builders; four since 21:01 (the entry above).
  **Its builder reported at 21:04** with one commit; the development agent brought it onto `main` with the
  signing identity (no conflict) and runs its gates there. **What it built:** the release image holds the
  Quader binary alone (246,494,610 bytes before, 83,309,810 after on arm64, the difference the analyser's
  163,184,800 bytes; read from the image's file list); each pod of `vuln-scanner-trivy` gets an init
  container `trivy` from the pinned image that copies the binary into a volume of 256 Mi, which the scanner
  mounts read-only at `/opt/trivy`; one pin `TRIVY_IMAGE` (tag and index digest) in place of the version and
  the four archive digests, held equal to the chart's value by a chart test; the Containerfile, the build
  workflow and the job that scans the release image without the analyser's stage (that job takes the binary
  out of the pinned image); one Renovate group (a new digest merges by itself, a new version does not); the
  development profiles start image scanning in the profile `vuln` alone, which loads the analyser's image.
  New values: `trivy.image`, `trivy.pullPolicy`, `trivy.resources`; the scanner's requested ephemeral storage
  is 1,280 Mi. **Decided by its builder (Q-HA-386 to Q-HA-388):** the platform matrix starts no pod and reads
  the init container back from the API server; the restore drill's installation switches image scanning off;
  `make trivy` keeps the binary under a name with tag and digest, and the versions check refuses a `trivy` of
  another version on the path. **Measured by it on a cluster of its own** (arm64, two scanner pods on nodes
  that had never held the image, host load 30 to 80): the pull 15.6 and 13.5 s, the copy 0.33 and 0.95 s,
  scheduled to ready 21 and 19 s; the init container ran as the unprivileged user on a read-only root file
  system without capabilities; in the end-to-end case the analysis ran 1.45 s after the task with the copied
  binary. Loading the release image into four kind nodes took 41 s where it took 85.7 s. **Its gates:**
  build, the full unit run, generate-check, the complexity check, links and check, the chart's lint and tests,
  the check of the workflow files, `make image`, the area `vulnimage` under the lock each exit 0; the lint
  passed its ten minutes under load and exited 0 with a longer limit; **the end-to-end set `vuln` exit 2:** 91
  cases passed, among them the scanner's and the malware's, and three of six browser journeys timed out at
  the sign-in page under a load of 47 to 87 (no code of the UI or the API changed; not repeated, the
  pipeline's leg shows them); **`make versions-check` exit 2 on two things of the development machine** ("On
  machines and in repositories outside this one" below). **Its tests cost** about 0.2 s more of the chart's
  job and about 10 s more of the leg `vuln` for loading the analyser's image; every other kind leg loads an
  image 163 MB smaller. **Checked by the development agent, which the builder had named as not verified:**
  the pinned digest is an index with amd64 and arm64; the copy as the pod does it (the unprivileged user, a
  read-only root, no capabilities) runs on the amd64 image and yields the pinned version (under emulation on
  the development machine, not on an amd64 node); the analyser accepts the ignore file with an empty list
  (exit 0; a broken file is fatal, so the file is read); the runners' image installs no `trivy`, so the
  versions check's new refusal finds nothing on the runner's path. **The owner's values** render against its
  chart (39 objects; the scanner pods gain the init container and the volume); his five nodes are amd64
  with CRI-O and already pull from Docker Hub, and none of his cluster's admission policies restricts
  registries (read on the cluster). **Not verified before the pipeline runs it:** the multi-architecture
  build and the job that scans the release image on the amd64 runner, both of which hold a release back;
  Renovate's real behaviour; the platform matrix's two read-backs. **Found by it and left (section 8):** the
  script that picks the end-to-end legs, the restore drill's values, and that nothing in the pipeline scans
  the analyser's image any more (H-1003 says so). **The push waits for the run of the signing identity to
  end** (about 21:40): the layout job ended last at 18:27, was stopped four times since by the next push and
  failed once at the seed, and the identity's run is the first with the seed's repair that can end. **On the
  commit as rebased onto `main`, each exit 0** (21:05 to 21:15): build, the full unit run, generate-check, the
  evidence regenerated without a difference, links and check, the complexity check, the chart's lint and
  tests, the lint (with a stated limit), the check of the workflow files and the tests of the pipeline's
  files; `main` changed no file of the pipeline since the package's base, so `make image` was not built
  again. The diff holds no name the white-label rule forbids.
- **Landed on 2026-11-21 at 19:27: package 2 of the image analyser's move, the screens** (R-UI-038, R-OPS-048,
  R-VULN-022 as amended; its builder worked from 18:11 to 19:11; no change of Go or of the API). Each pod of
  `vuln-scanner-trivy` says which database it holds, as a badge of icon and word: **Database in use** in green
  with the time it was built, **Older database** and **No database yet** neutral, the whole digest on
  pointing; on the System page under the tool's name, in the advisory sources' fold of the image scanners as a
  column of its own. For the installation: a line of information on the System page while ready pods lack the
  database in use, and the summary of the advisory sources says how many are on it. **Decided by its builder
  (Q-UI-915):** an older database is never a warning, because every new database (up to four a day) leaves
  every pod on the old one for the 10 to 30 s of the load and a warning would flash on a healthy installation;
  the badge stands under the tool on the System page, because a column of its own made that table stand as
  blocks at 1,440 px (measured: 1,192 px needed of 1,128); the digest only on pointing. To look at again when
  the API reports since when a pod holds a database that is not in use: then a pod that lags for minutes can
  be a warning. **On the commit as rebased onto `main`, each exit 0:** build, the full unit run,
  generate-check, the lint, the complexity check, links and check, the UI tests and the UI lint; by its builder
  the layout checks and the renders of ten screens at 1,440, 1,024 and 390 px, light and dark, none failed, no
  layout finding. **Looked at by the development agent:** the System page with one pod in each state (wide,
  light) and the advisory sources' fold (narrow, dark). **Its release was cut at 19:46,** and the owner's installation runs it since 19:53 (every Deployment ready). **Its tests cost** well under a second of UI tests and an estimated 15 to 30 s of the layout job. **Not
  verified:** the German texts in a browser (the longer German words may make the System page's table stand as
  blocks sooner; the margin at 1,440 px is 52 px in English); the rows against real scanner pods. **Found by it
  and left (section 8):** the UI's tests that search for a person wait a fixed 320 ms and fail under a loaded
  machine (seen three times on 2026-11-21 by three builders); a comment of the UI development profile still
  says the role `all` calls its own scanner. Its worktree and branch are removed.
- **The run of the 19:47 push (44937450926) went red at 19:56 on the job `UI layout checks`, which holds no
  release back; its repair landed at 20:35.** Two changes met on `main`: the repair of the revision
  columns renamed the trust row's epoch, and the screens of the signatures came with a render seed that writes
  rows by SQL under the old name, so the job stops at its seed. The rebase of the screens onto the repair had
  no conflict of text, the renders were not run again after it, and the landing's gates hold no render run.
  One builder corrects the seed, searches the tree for the old names, proves it with a render run on a stack
  of its own and writes the rule onto the UI's developer page. **For the landing from now on:** when a rename
  of the schema lands, the commits that still wait are searched for the old names; and a UI commit that is
  rebased onto a change of the schema has its render seed run again before the push.
  **The repair** (reported ready at 20:09, landed at 20:35, a commit of the type `test` that releases nothing):
  the seed writes the column's new name; no other seed, fixture, query or page still held the old names. Proven
  by its builder with the render setup and seed on a stack of its own: the signatures' and every package and
  version screen, 68 passed, no failure, no layout finding. The UI's developer page says that the seed writes
  rows by SQL, which no compiler checks against the schema. The push waited for the run before it to end,
  because that run was the first to carry the end-to-end leg `vuln` of the analyser's move and all three
  shards with the revision columns' repair. **That run (44937450926) ended at 20:34, green on every job but
  the layout job:** the leg `vuln` green, so the analyser's move is confirmed end to end on the pipeline's
  runner; the three integration shards green in 38.9, 42.1 and 40.9 minutes of their 45 (shard b, which had
  been red three times that day for three different lists, at 42.1). **The repair's own run (44941727841)**
  cut no release, as a commit of the type `test` does not (its release job ended at 20:46); it was stopped at
  20:58 by the push of the signing identity, with the layout job 23 minutes into its render step, past the
  seed at which the run before had stopped after 7 minutes. The job's whole result comes with the
  identity's run. **The builder's full run of every screen on its own stack** (ended at 21:00): 434 passed in
  49 minutes, none failed, no layout finding; eleven pictures listed as sideways are regions that scroll
  inside the page (code blocks and a table of the manual), which the check allows. Its worktree and branch
  are removed.
- **Landed on 2026-11-21 at 19:47: package 4 of the signatures' first stage, the screens** (R-SIG-035 to
  R-SIG-038; its builder worked from 17:37 to 19:22 and brought the commit onto `main` from 19:23 to 19:35).
  With it the first stage is whole: detect, read, verify, show. **What a reader sees:** a shield with one of
  the four states beside a version (in the version list, in the version's header, in a column of its files),
  and a tab **Signatures** with one sentence for the state, who uploaded the version (for a pipeline its
  project, ref, job and commit), and each signature in plain words: what kind, who signed, when and what
  proves the time, which trust anchor knows the signer or which block applies, the original to download, the
  technical details behind a fold; an image's attestations and SBOMs in a table of their own and never as tags;
  a file's signatures on its page; a third view **Signatures** of the security dashboard with invalid
  signatures and blocked signers. **A project Owner,** while the administrator allows it: a closed section
  **Trust anchors** of the project's settings. **An administrator:** the settings page **Signatures** with
  the anchors of every source (the shipped ones switched on and off, the chart's read-only, his own added and
  removed), the switch for project Owners, blocked signers, the public root's state and the backlog; and on a
  remote repository's settings a closed section that switches the fetch of its upstream's signatures off.
  Nothing of it stands in a creation flow. **Decided by its builder:** the details are a tab and the header's
  badge opens it (Q-SIG-124); the shields and that none is shared with the malware scan (Q-SIG-125); a file's
  fact and section, no badge in the browse list (Q-SIG-126); the formats list names each format's kinds of
  signature, an addition to the API (Q-SIG-127); the fetch switch is an administrator's on a well-known
  repository of `upstream` and the product's on a system repository (Q-SIG-128); an anchor's material is
  removed and added again, not edited (Q-SIG-129); the dashboard's view has no actions (Q-SIG-130). **Sent back
  once** for the sentence of a blocked signer, which had said that no anchor knows the signer; it names the
  block now. **The renders looked at by the development agent:** an image's tab with three signatures and its
  attestations, the admin page with every source of anchor (wide, light); the builder rendered every state at
  1,440, 1,024 and 390 px, light and dark, none failed, no layout finding. **On the commit as rebased onto
  `main` with the unit run's cure, each exit 0:** build, the full unit run, generate-check, the lint, the
  complexity check, links and check, the UI tests and the UI lint. **Its release was cut at 20:00,** image and chart were published at 20:05, and the owner's installation runs it
  since 20:06 (read on the cluster at 20:15: every Deployment ready, no error line of `api`, `packages` or the
  worker). **Its tests cost** about 3 s of UI tests and eleven screens more in the layout job (about four to five
  minutes on the development machine across kinds and widths, not measured in the pipeline); its one Go test
  cost 7.2 s in the job's mode with a validator of its own and costs 0.7 s without it, measured by its builder
  by the new rule. **Not verified:** the German texts in a browser; signatures as the worker records them
  (the rendered ones were written with their verdicts); Debian's covering releases on the screen; a version
  with seven signatures. **Found by it and left (section 8):** three narrow lists of the new pages stand as
  blocks even at 1,440 px, which the layout checks accept. Its worktree and branch are removed.
- **In work since 17:37, three builders on `main` with the signatures' package 2** (with package 1 of the analyser's
  move that makes four): **package 4 of the signatures' first stage, the screens** (the badge and the details at a
  version, a file and an image with the origin of an upload, the settings page of the signatures with the anchors,
  the switch and the blocks, a project's anchors, the security dashboard's view, the badge on the version list, the
  detail setting that switches a remote's fetch off; rendered at every width of the pipeline's job); **package 1
  of the second stage, the signing identity** (its states, the two ways to it by the chart and by the admin area's
  back end, the announce and the switch on rotation, the frame of the job `signing`, its API, events, metrics and
  alerts, backups and restores); **package 1 of the third stage, the gate** (the repository's setting, what passes
  state by state, the format contract's method that classifies an item, the gate at the download beside the
  malware gate with one decision and one wait for the three checks, the pending answer with its bounded wait and
  the held fill, the answers per format, the audit event, metrics, alert). Each was told what the day taught: no
  agents of its own, migration numbers set anew at the landing, the area `upstream` for a table with a key to
  repositories, the copy of the values file equal to the chart's values, a new format method joins the developer
  page's list, UI changes rendered at every width of the job. (The screens landed at 19:47, the entry above.)
  **Package 1 of the third stage, the gate, reported at 19:52** with one commit on the base of 17:37; since
  19:52 its builder brings it onto today's `main`, and since 20:58 onto the signing identity, which landed
  before it: its migration becomes 00102, and it builds what its report had named as owed, the exclusion of
  Quader's own signatures from the gate by the column the identity brought; landed at 21:47 (the entry of the
  three landings above). **What it built:** the repository member `signatures` with
  `onlyVerified` and `anchors` (422 for Debian only where the value would do something; a project Owner
  narrows, weakening needs the right the malware gate's weakening needs and is audited as a lifted block; a
  narrowing is journaled and a restore applies the narrower of the two values); the decision state by state
  with its reasons; a method of the format contract by which every format on `main` says what an item is to
  the gate (an image's manifest and index judged and a platform manifest passing on its index, signature,
  attestation and SBOM manifests always served, npm's tarball, NuGet's package and its symbol package,
  Ansible's archives, every generic file with its sibling signature served; Debian and the ClamAV definitions
  never judged); the gate at the format host's serve point, at the file route and at OCI's remote tag
  handler, through a virtual repository its own and the holding member's; a removed anchor or a new block
  acting on every replica before the trust job recomputes; the pending answer after a bounded wait with the
  fetch's priority raised and a remote's fill held; 403 and 503 in each format's existing shapes, never
  cached; a table of refusals, the audit event, two metrics and one alert. **The malware gate is split into
  decide, refuse and wait,** and the three checks decide together before one wait in the fixed order of
  refusals; the one change of behaviour: a vulnerability block no longer waits behind a pending malware scan.
  **Decided by its builder** (fourteen blocks, numbered at the landing behind the screens' decisions): among
  them that the gate reads its rows in a statement of its own only where a gate applies, that any change of
  an anchor's material narrows it for the gate, that an unstored body through a gated remote answers pending,
  and that every refusal is counted in a table while the audit event stays deduplicated. **Its gates:** build,
  the full unit run, generate-check, the lint, the complexity check, links and check, the UI tests and lint,
  the chart's suite each exit 0; under the lock the areas `signatures`, `malware`, `upstream`, `packages`,
  `ocilocal`, `ociremote`, `npm`, `vulnblock` and `backup` green; `postgres` red there on the two columns the
  repair of 19:27 renamed; one known flaky test of the area `files` failed with a deadlock (section 8). Two
  areas and the chart's suite passed the tool's ten minutes under a host load of about 100 and passed with a
  longer limit. **Its tests cost** about 1.5 s of unit tests, 11 s in the integration area `signatures`,
  under a second of the chart's job. **Sent back at 19:52 with:** the rebase (its migration and decision
  numbers behind those `main` holds, the renamed column, the API tests on the shared parse of the unit run's
  cure, one rule with the screens' builder for the member on a well-known repository), and its unit tests'
  cost in the job's mode. **Owed and not this package's:** the measurement by hand with real clients of every
  format against a refusal and the pending answer; the exclusion of Quader's own signatures once the second
  stage's column exists. **Not verified by its report:** the view through a virtual repository and a remote's
  fetch state against the database (unit tier only).
  **Package 1 of the second stage, the signing identity, reported at 19:58** with one commit on the base of
  17:37 and landed at 20:58 (the entry of its landing above). Nothing is signed with it yet; npm's
  signatures, the attestations and the screens are the stage's next packages. **By the chart:** an existing
  Secret or a Certificate the chart asks cert-manager for (ECDSA P-256, the usages digital signature and code
  signing, the base URL as the first URI), mounted into the worker's pods alone, read at the start and every
  minute; a pair that fails the checks is refused once per content and the identity in use keeps signing;
  while the values name an identity an upload answers 409. **By the admin area's operations:** Quader makes
  the key, seals it with the key ring under its row and answers a certificate request; the CA's certificate
  is uploaded and checked against the pending key; an existing key and certificate can be imported; a
  pending key can be discarded. **Rotation:** a new identity is announced for ten minutes, then the job
  switches it under a lock that no signing transaction can cross; remove makes the current upload previous;
  retire takes a previous identity out of npm's keys for good and drops its sealed key; both are journaled
  and applied again after a restore. **The frame for the next packages:** the job `signing` with its work
  table, into which a package registers a kind with its sign and discover steps; helpers for npm's keys
  document, the trusted root and the chain; the column that marks a signature as Quader's own. **The API:**
  the identity's state, key request, certificate upload, import, discard, removal and retirement, and the
  trusted root and the chain for anyone. Seven audit events, five metrics, four alerts. **Decided by its
  builder** (eleven blocks, numbered at the landing): among them that a retired identity never returns, that
  retiring drops the sealed key, that the worker keeps the files' key and the one before it, and that the
  two public answers may be cached for a minute. **Its gates:** build, generate-check, the lint, the
  complexity check, links and check, the chart's lint and tests each exit 0; the full unit run green after
  two gaps of its own were closed; the UI tests failed on the two tests that search for a person under load
  and passed alone; the area `signing` green (11.6 s, joins shard b); the area `backup` passed the tool's
  ten minutes and was not run whole; the area `postgres` red on the two renamed columns and on one more it
  names as not its own, which it looks into at the rebase. **Sent back at 19:58 with:** the rebase (its
  migration behind the gate's, its decisions behind the gate's), the API tests on the shared parse, its unit
  tests' cost in the job's mode, the areas `postgres` green and `backup` whole. **For the owner's
  installation:** nothing renders with the defaults; an identity would come from his issuer by one value;
  whether that issuer issues the usage code signing for such a key and whether its Secret carries the CA is
  not known and shows at the pod or in the API. **Not verified by its report:** a real cert-manager, a real
  company CA, a rotation on a cluster, a whole backup restored into a fresh installation.
- **Landed on 2026-11-21 at 17:48: package 2 of the signatures' first stage, verification and trust** (R-SIG-005
  to R-SIG-021, R-SIG-034, R-SIG-039 to R-SIG-041; what it built is in the entry of the two builders below, as
  reported at 15:50; its builder brought it onto `main` from 15:50 to 17:30, alone, over four rebases while
  `main` moved). **Added on the way, as sent back:** the wiring package 3 had left (a local npm repository
  serves a bundle the trust step vouches for; the counter of requests to remotes; the fetch's configuration,
  whose two bounds reach a format through the run's context, an addition to the format contract (ADR-233); the
  repository member that switches a remote's fetch off, settable and validated, so the manual says it again;
  Debian's covering releases in the API); **the badge on the version list** (one statement per page and one
  count per project among the rows' repositories, no query per row); **the journal's replay into a restore**
  (a removed anchor, a block and the switch, in the area `backup`, about 2 s); the discovery skips system
  repositories and formats without signatures, so the ClamAV definitions cost nothing there and show as
  nothing unchecked. **Its numbers after the rebases:** migration 00098, decisions Q-SIG-109 to Q-SIG-123, gaps
  H-1202 (a project that begins to hold a signature already checked sees the installation's trust until the
  next recomputation) and H-1203 (a break of the object store inside an archive's read is recorded as
  malformed, not tried again), ADR-233. **Corrected in the specification:** a project's ID is a uuid. **On the
  commit as rebased onto `main`, each exit 0:** build, the full unit run, generate-check, the lint, the
  complexity check, links and check, the chart's lint and tests, the UI tests and the UI lint (after the UI's
  packages were installed in its worktree, which had borrowed the main checkout's); by its builder under the
  lock the areas `signatures`, `upstream`, `npm`, `ociremote`, `ansiblelocal`, `debremote`, `packages`,
  `backup` and `malware`, three of them again after the last rebase that changed code. The owner's values
  render against its chart unchanged (39 objects). The owner's UI backend started with it before the push
  (the schema step applied). **Its release was cut at 18:04**, image and chart were published at 18:10, and the owner's installation runs it
  since 18:13 (read on the cluster at 18:15: every Deployment ready, no error line of `api`, `packages` or the
  worker). **Seen there, which the builder could not verify:** the worker's first refresh of the shipped trust
  root reached the signing service's update repository from his cluster (eight answers 200 and one 404, the
  probe for a newer root, which is the protocol's way of finding none) and logged no failure. Its run is left
  to end before the next code push. **Not verified:** signatures made by real clients; a live
  refresh of the shipped root; real timestamp authorities beyond the chains read from twelve nuget.org
  packages; the heap bound of the read is asserted by no test; the wiring of the requests' counter has no
  test; a whole backup and restore of the signatures' tables; the recomputation with the object store stopped.
  Its worktree and branch are removed.
- **Landed on 2026-11-21 at 17:05: a ClamAV installation updates from Quader with its client certificate**
  (R-CLAMAV-010; the owner's "b + zusätzlich Client-Zertifikate"; its builder worked from 15:16 to 16:47).
  **What an operator configures:** a further endpoint with a host of its own, `auth.clientCertificates`, a
  Secret with the admitted CAs and a pair for that host, and either the ingress with `tlsPassthrough` (the
  chart writes the passthrough for HAProxy Ingress and ingress-nginx, refuses traefik, and refuses every
  combination that could not work, naming the value) or a Service of its own. **What an administrator
  sees:** the lines of the certificate in the fold **Visibility** of the definitions' page (the address to use
  with a certificate while an endpoint admits them, a note and a link to the manual while none does), the
  certificate's setup behind **Authenticate**, the plain download in a fold **Without freshclam**, and a kind
  "Client certificate" in the usage overview. **What a ClamAV does:** a stock `freshclam` names the endpoint
  and finds certificate, key and CA in its environment; a valid certificate reads the definitions at the
  levels internal and public and nothing else; a missing, expired or foreign one fails the handshake. The
  client setup shows the four ways. **Decided by its builder:** the guide names only the endpoints on which
  a certificate reads the repository now (Q-CLAMAV-18, with an added field of the format contract, ADR-232); a
  certificate holder takes the technical tokens' default download limits and is a kind of its own in the
  usage overview (Q-LIM-96); where the certificate's setup and the plain download stand on the page
  (Q-UI-914). **On the commit as rebased onto `main`, each exit 0:** build, the full unit run,
  generate-check, the lint, the complexity check, links and check, the chart's lint and tests, the UI tests
  and the UI lint; by its builder the layout checks and under the lock the areas `malware` and `upstream`.
  **The renders looked at by the development agent:** the definitions' page with every fold open and an
  endpoint that admits certificates (wide, light), the client setup with the four ways (narrow, dark); the
  builder rendered at the three widths of the pipeline's job and corrected a file name that ran into its copy
  button at 390 px. The owner's UI backend started with it before the push. **Its release was cut at 17:20**, image and chart were published at 17:26, and the owner's installation runs it
  since 17:29 (read on the cluster at 17:31: every Deployment ready, no error line of `api`, `packages` or the
  worker); its run (44922668978) was left to end before the next code push and ended green on every job at
  17:48. **Measured once with a real client on a cluster of its own** (recorded on the format's developer page; the
  real set of 2026-11-21, the switch on at the level internal, a stock `freshclam` of the pinned image; cluster
  and image removed): with an admitted certificate all three databases fetched and tested, exit 0, and the
  second run up to date, exit 0; without a certificate and with one of a foreign CA "Download failed (56)",
  exit 11, no cool-down; with a token in the URL all three fetched, exit 0, and the token stands in every
  line of its log that names the address; without the token 401; the plain download with `curl` and the
  token on standard input answers 200 for the three files, with a certificate on the certificate endpoint
  200, without one the handshake's alert; at the level private a valid certificate gets 404. The request
  log's line, the usage row and the handshake errors in the process log came out as specified. That closes
  what the first measurement had left out (the token in the URL and the plain download). **Its tests cost**
  under a second of unit tests, 0.6 s of the chart's job and about 1.1 s in the area `malware`; the
  integration harness takes a client CA on a further endpoint as it is. **Not verified:** a passthrough
  through a real HAProxy Ingress (the chart's render alone is tested); an encrypted key with its passphrase
  variable; the unit's name per distribution; the German screens in a browser. One UI test failed once in
  the builder's full run under a host load of about 80 and passed alone; it passed in the landing's run.
  **Found by it and left (section 8):** the guide's table of troubleshooting is squeezed at 390 px; a
  sentence of the guide shows the address with the trailing slash it says to leave out; the usage overview's
  page of one caller lists no credentials for a certificate holder. Its worktree and branch are removed.
- **In work since 15:16, two builders on `main` with the ClamAV change:** **the client certificates of the
  definitions' endpoint** (R-CLAMAV-010; the endpoint key, the certificate subject and its one rule, usage and
  request log, the chart's passthrough per ingress controller, the read-only lines on the registry's page, the
  client setup's ways with a certificate and by plain download; with renders, and with one measurement on a
  cluster of its own: a stock `freshclam` with an admitted certificate, without one and with a foreign one,
  and what the first measurement left out, the token in the client's URL and the plain download); and
  **package 1 of the image analyser's move** (R-VULN-048, R-VULN-049 and its part of R-HA-070: the scanner
  unit detects as well, the worker runs no tool and reads the report under limits, each scanner pod holds the
  database read-only through a second container of the kind `trivy-db`, the rewrite of the analysis and its
  fidelity test go; with the end-to-end set `vuln` and a measurement on a cluster of its own). Both were told
  what the day taught: no agents of their own, migration numbers set anew at the landing, the area `upstream`
  for a table with a key to repositories, the copy of the values file equal to the chart's values. With the
  signatures' packages 2 and 3 that makes four builders. (The client certificates landed at 17:05, the entry
  above.)
  **Package 1 of the analyser's move reported at 17:43** with one commit on the base of 15:16; since 17:43 its
  builder brings it onto today's `main`. **What runs where by its report:** the worker sends the stored
  analysis to the scanner for the detection, checks the answer's headers before it reads the body and parses
  the report under limits; it runs no tool and holds no database. The scanner pod's container `quader` does
  the analysis and the detection with one set of slots and mounts the database read-only; a second container
  `database` (kind `trivy-db`) loads the database in use from the scan listener with the pod's certificate,
  checks the hash while it unpacks and keeps the newest and the one before; both processes are not dumpable;
  the role `all` builds no image scanner. **Found by its measurement on a cluster of its own with the real
  database (1.8 GB unpacked) and repaired in the commit:** the loader was killed five times for memory while
  unpacking, by dirty pages of the file cache and not by its own 22 MB, and now flushes each file every 32 MiB
  (Q-VULN-212); after the database's tag moved back to an older build the pods reported the newer one they no
  longer used and the loader would have removed the one in use first, so "newest" is now the one written
  last (Q-VULN-213). **Measured there** (under a load of 12 to 80 from other clusters, so upper bounds): a
  loader holds the real database 4.4 to 5.6 s after it is recorded, without a restart; the fixture path
  analyses in 0.16 to 1.4 s and detects 0.15 to 0.17 s later; a probe container of the same user cannot read
  the scanner's environment or memory map in a real pod. **The end-to-end set `vuln` passed on a fresh
  cluster** (98 tests, 230 s); its one grown case takes 4.8 to 11.6 s where it took 6.9 s. **Removed with it:**
  the fidelity test of the analysis' rewrite with its step in the release workflow, and two tests of the
  worker's spool. **Its further decisions:** the analysis row keeps its count of packages (Q-VULN-207), the
  database's build time in the source's watermark (Q-VULN-208), the loader's budget a key the chart sets
  (Q-VULN-209), the database routes on a listener without TLS as the malware scanner's (Q-VULN-210), the
  process not dumpable at the entry of `serve` for every scanner kind (Q-VULN-211). **Sent back at 17:43 with:**
  the rebase (its migration behind those `main` holds), and one decision of the development agent for the
  owner's walkthrough: the migration removes the stored analyses, so that every image is analysed afresh by the
  new unit and none shows 0 packages (the builder had kept the rows and named the wrong count; the price is
  one pull per image after the upgrade). **Not verified by its report:** a detection against the real database
  in a pod; the worker's transaction that applies a detection; amd64 and the pipeline's runners. **Found by it
  beside the task (section 8):** the scanner's spool writes of large layers may carry the same risk of dirty
  pages as the loader had; a scanner's row of a pod removed in a rollout stayed ready until pruned.
- **Landed on 2026-11-21 at 14:46 in one push of four commits: ClamAV's definitions are a repository of their
  own, the updater is a Deployment of its own, and an administrator can serve the definitions to every ClamAV
  of the company** (the owner's request of that morning, "With the owner"; R-CLAMAV-001 to R-CLAMAV-009,
  R-MAL-010 to R-MAL-012, R-MAL-032 to R-MAL-034, R-HA-069, R-REPO-075, R-UI-188, R-UI-194; the reports of the
  four builders are in the entry "In work (10:57)" below). The commits: the format `clamav` with the
  repository `upstream/clamav` and its switch; the chart with the Deployment `malware-updater-freshclam`; the
  updater's process and the worker's import with ClamAV's signature checked before anything is stored; the
  screens. **The assembly** (13:44 to 14:27, by package 2's builder): the three commits of the Go side and the
  chart onto `main` behind the presets, the signatures' first package and the purge's repair, the migrations
  as 00094 and 00095, package 1's decision as Q-REPO-420, the copy of the values file equal to the chart's
  values again; then the screens' commit onto that tip by its builder (14:27 to 14:41), which merged the
  presets' and its own blocks and screens and built the section **Updater** the API can feed now. **Decided at
  the assembly, for the owner's walkthrough:** the format `clamav` declares no signatures (Q-SIG-96): ClamAV's
  signature of a container is part of its file format, signs an MD5 with a key of 1,024 bits, is checked by the
  import and shown as the version's verification; as a publisher's signature in the store every version would
  have shown "signer not trusted" beside a verification that passed. While no set is in use the import asks
  the updater again every 30 seconds (Q-MAL-104, the development agent's, after the builder's finding). The
  updater's rows have a section **Updater** on the registry's page and a fold of their own in the malware
  scanning settings, without slots (Q-UI-913). **On the four commits as rebased onto `main`, each exit 0:**
  build, the full unit run, generate-check, the lint, the complexity check, links and check, the chart's lint
  and tests (113 s), the UI tests and the UI lint; by the builders on the same tree the layout checks and,
  under the lock, the whole integration areas `malware` and `upstream`. **The renders looked at by the
  development agent:** the registry's page with the switch on and the section **Updater**, the list of
  databases with and without an outdated `daily`, a version's details, the malware scanning settings with both
  folds (light and dark, wide and narrow). **The owner's values** render against the chart as they are (39
  objects; none of the removed keys is set) and with the updater's label. **Read on his cluster before the
  landing** (what had been unverified): a cluster-wide policy denies every pod the way out of the cluster by
  default, the label `gnp/internet-https` opens TCP 80 and 443 to addresses outside the private ranges, and
  names are resolved by the cluster's resolver, so that one label on the updater's pods is enough. The
  released chart's schema refuses a values key it does not know, so the label is committed under `apps/quader`
  when the new chart is published, not before. The owner's UI backend started with the four commits from the
  main checkout before the push and answers ready (both schema steps applied). The worktrees and branches of
  the four builders and the landing branch are removed. **Its release was cut at 15:02, image and chart were published at 15:08, and the owner's installation
  runs it since 15:10.** Flux began the upgrade within a minute of the chart; the label for the updater's pods
  was committed and pushed under `apps/quader` of his repository at 15:08:51 (after the upgrade had begun,
  because the chart before refuses the key), and a second upgrade brought it to the pod. **Read on the cluster
  at 15:11:** the release is ready on the new chart; two ready pods each of `api`, `packages`, the worker and
  both scanners, one of the updater with its two containers and the label; the schema job complete; no error
  line of any of them. The malware scanners loaded the set in use (`main-63.daily-28190.bytecode-339`, carried
  over from the old repository) eleven seconds after their start, so the scanning had no gap. The updater
  seeded its directory from that set (four files, 0.7 s) and was ready five seconds after its start; its
  `freshclam` then reached ClamAV's network, found `main` and `bytecode` up to date and fetched `daily` 28190
  whole. **The import's first run against this updater was scheduled at 15:30:08 and logged nothing**, neither
  a new set in use nor a failure nor the waiting for a first set: the set's name is the one in use, so by the
  log it took nothing. **Not verified, and looked at when ClamAV's next `daily` arrives:** which file the
  repository holds for `daily` 28190 after the upgrade (the one carried over from the old repository, which
  the updater's `freshclam` did not accept as current, or the whole container fetched since), and with it
  whether the endpoint could serve `daily` to a company's ClamAV before the next set; the first real take of
  a new set on his installation (the line "a new signature database is in use" in the worker's log). The
  scanning itself is not touched by it: both scanners run with the set in use. **Not verified:** the German
  screens in a browser; the updater's rows on the screens against a running updater (the renders used rows
  written by hand); the 503 at a request limit and an external user's 404 in an integration case; a token in
  the client's URL and the plain download with a real client; a new `daily` arriving while the updater runs.
  **Next of this change:** the client certificates of the endpoint (one package, specified).
- **In work since 12:56, two builders on `main` with package 1: the signatures' packages 2 and 3.** Package 2:
  the read, the integrity check of each scheme, time, the trust step, the job `signature-verify`, the four kinds
  of anchor in their scopes with the chart's declared ones, the switch for project Owners, the block of a
  signer, the API, events, metrics and configuration. Package 3: the formats' wire behaviour (OCI's attachments
  in no list and following their image, the background fetch of a remote's signatures, virtual repositories,
  npm's keys and attestations endpoints with Q-SIG-34 as option (a), NuGet, Debian, Ansible's upload of
  signatures, generic, the Bastion). Each names the files it touched outside its area, so that the landing
  orders the two commits; package 4 (the screens) follows package 2's operations.
  **Package 3 reported at 14:43** with one commit on the base of 12:55, its gates exit 0 but the area
  `upstream`, red there by package 1's table alone (repaired on `main` meanwhile). Since 14:47 its builder
  brings the commit onto today's `main`: its migration behind the ClamAV change's (and its replacement of the
  purge's function with the repair's line in it), its decisions behind those the two later stages took, the
  format `clamav` under what it added to the contract, one shape of the fetch's state table for it and for
  the third stage's text, and three small things by the development agent's decision (an audit event for a
  posted or removed Ansible signature; the manual not promising a switch that is not settable yet; a fetch's
  state row going with its target). It reported again at 15:40 and landed (the entry above).
  **Asked back by package 2 at 14:21 and decided by the development agent:** the store's interface carried a
  project's ID as a number, and the specification named a column of that type on a table that does not
  exist; a project's ID is a uuid (read in the schema and the code), so the fields and columns carry the uuid
  and the specification is corrected in the package's commit. **A deviation, seen at 14:22:** package 2's
  builder had started three helpers of its own, so six agents ran where the owner's rule says four; it was
  told to keep it at those three and to report how the work was split; they were not stopped, because two
  other builders were about to finish. **For every builder's brief from now on:** a builder starts no agents
  of its own.
  **Package 2 reported at 15:50** with one commit on the base of 12:55; since 15:50 its builder brings it onto
  today's `main`, alone. **What it built:** the job `signature-verify` reads and checks every due signature in
  the worker under the bounds, a deadline of five seconds and a recovery from a panic (bundles of the signing
  service and cosign's older layout, the two envelopes of Notation, NuGet's signature with its
  countersignatures and timestamps, OpenPGP, npm's registry signatures, raw public keys), stores the result
  with its evidence, and a pass of discovery picks up what was stored before this stage; the trust step judges
  from that evidence alone, in the installation's scope and in every project that has anchors of its own, and
  a job recomputes trust on every change of anchors, blocks or the switch; the four kinds of anchor in the
  scopes shipped (the signing service's public root and npm's keys, refreshed daily, the keyrings of Debian
  and Ubuntu, nuget.org's certificates and timestamping roots; each can be switched off), configuration (the
  chart's values, applied by the migration, read-only in the API), installation and project; the switch for
  project Owners; blocks; the API (the signatures of a version and of a file, the original bytes, settings,
  anchors, blocks, findings, the member with state on an item, a version and a version's files); nine audit
  events, metrics, three alerts, the configuration. **All three of the owner's ways to a signature are
  exercised with real signatures made in the tests:** a signing service of the company's own with its
  certificate authority, log and timestamp authority, a code-signing certificate of the company's CA, and an
  OpenPGP key. **Measured:** the binary grows by 6.30 MiB (8.8 %) and 52 modules for the whole package; a
  trust judgement costs about 0.19 ms with a CA and 0.10 ms with OpenPGP. **Its tests cost** about 15 s in the
  unit job, 13 s in the integration shard and 2 s in the chart's job; no end-to-end case. **How it worked:**
  six helpers in all, at most three at a time, no two on the same file; it wrote the contracts first and the
  corrections after. **Not built by its report:** the badge on the version list (its rows carry no file
  digests), the restore's test, one variant of a test. **Sent back at 15:50 with:** the rebase (its migration
  and its decision and gap numbers behind those `main` holds), the wiring package 3 left for it (the verdicts
  that let a local npm bundle be served, the counter of requests to remotes, the fetch's configuration and
  the repository member that switches it off, Debian's coverage in the API), the badge on the version list if
  it can be had without a query per row, the restore's case if it costs seconds, the chart's tests green with
  the values' copy. **Not verified by its report:** signatures made by real clients; a live refresh of the
  shipped root; real timestamp authorities beyond the chains read from twelve nuget.org packages; the heap
  bound of the read is asserted by no test.
- **The pipeline of the correcting push (run 44902596352) ended at 13:52 with one red job, `integration (shard
  b)`, which holds no release back; its repair landed at 14:10.** One test of the area `upstream` holds
  every table that references a repository by a cascading key in one of two lists, purged with a retired
  repository of the upstream registries or kept; the signatures' first package added such a table (the
  attachments) and its builder had run seventeen areas, not that one. One builder writes the migration that
  lets the purge take the attachments, puts the table into the test's list and writes down the rule on the
  database's developer page; the two builders of the signatures' packages 2 and 3 were told the rule for
  their own tables. Every other job of the run was green, the layout checks with the presets' new states among
  them. **The repair** (its builder worked from 13:53 to 14:08): a migration replaces the purge's function with
  one delete added, for the attachments that hang on no item (a remote's document); those of an item already
  went with the item; signatures and their objects exist once per installation and are left to the cleanup of
  orphans, the origin of an upload goes with its item. The test's list and its purge case hold the table now,
  and the database's developer page tells the next builder to run the area `upstream` for a table with such a
  key. On the commit as rebased, each exit 0: build, the full unit run, generate-check, the lint, the complexity
  check, links and check; by its builder under the lock the areas `upstream` and `signatures`. The owner's UI
  backend started with it before the push (the schema step applied). **Its release was cut at 14:25**, image and chart were published at 14:31, and the owner's installation runs it
  since 14:34 (read on the cluster at 14:42: five Deployments with two ready pods each on the new image, the
  schema job complete, the ingress answers, no error line of `api` or the worker). **Left by it:** nothing calls
  the cleanup of orphaned signatures before the signatures' package 2 is built, so signatures and objects that
  a purge or a deletion leaves without an attachment stay in the database until then; only the function was
  tested, not the whole purge job with attachments that the real hooks recorded. **Caught beside it:** the
  builder that assembles the ClamAV change had met the same red test and begun a repair of its own; it was
  told to drop it, and its two migrations follow this one in number.
- **The pipeline of the 12:55 push went red on the job `chart` at 13:02, which holds the release back; corrected
  and pushed at 13:10.** Cause: a specification commit of the morning (the ClamAV change's) had written values
  that are specified and not built into the copy of the values file under R-HA-031, which a chart test holds
  equal to the chart's values. That commit ran no pipeline (documentation only), and the development agent's
  gates for a specification commit were links, check and the count test, so `main` carried it unseen from
  its commit at 10:53 until the next code push ran the job. The second stage's specification, merged locally at 13:00, had
  done the same for the signing identity. Both corrected in one commit without the skip marker, so that the
  pipeline runs and the two features are released: the copy holds the chart's values again; the updater's
  values return with the chart's commit on the landing branch (the reversed hunks are kept as a patch in the
  scratch directory and applied forward at the assembly if the rebase drops them), the signing identity's
  stand in R-HA-071 until its chart is built. The whole chart suite ran locally before the push, exit 0 (123 s).
  **For every specification commit from now on:** when it touches the chart's page of the specification, the
  chart's tests run before it lands; and a planner's brief says that the copy of the values file changes only
  with the change that builds the values.
- **Landed on 2026-11-21 at 12:55 in one push, two features.** **The presets of the installation's own
  registries** (R-REPO-076, R-API-084, R-UI-195; the builder worked from 11:41 to 12:34 and wrote the
  specification in the same commit): an administrator keeps the company's own registries per format in a section
  **Registries of this installation** of the upstream registries' page (name, address, further hosts with the
  token service, no credential; added, edited and removed in a dialog, the whole list saved under `If-Match`),
  and the form of a new remote repository of every format with a remote class offers them as a second row of
  buttons after the well-known registries; a button sets the address and its hosts and never a credential, and
  nothing follows a preset afterwards. **And package 1 of the signatures' first stage** (R-SIG-002 to R-SIG-004,
  R-SIG-010, R-SIG-022 and the detecting part of each format; the builder worked from 10:58 to 12:54): the
  format contract at version 8 with the declaration of what a format knows as a signature and its detector,
  check 18 of the kit, the store of signature objects, signatures and attachments, the origin of an upload,
  six detectors and a bounded walker of OpenPGP packets, and no version row for an image's attachment tags.
  **On the two commits as rebased, each exit 0:** build, the full unit run (with the migrations gapless at
  00091 and 00092), generate-check, the lint, the complexity check, links and check, and for the presets the
  UI tests (153 files) and the UI lint; the presets' renders looked at (the admin page with presets of two
  formats, the dialog, the forms of a new remote OCI and npm repository; light and dark, wide and narrow); no
  change of the chart; the owner's UI backend started with both from the main checkout before the push and
  answers ready (both schema steps applied). **Sent back once:** the signatures' commit put one function of
  the kit one over the complexity threshold; its builder moved two cases into a helper (two minutes). Both
  worktrees and branches are removed. **Their release was cut at 13:26** by the run of the correcting push (the entry above this one), image and chart
  were published at 13:32, and **the owner's installation runs it since 13:34** (read on the cluster at 13:45:
  five Deployments with two ready pods each on the new image, no restart, the schema job complete, the ingress
  answers, no error line of `api` or the worker). **Decided by the presets' builder, for the owner's
  walkthrough:** a preset holds format, name, address and further hosts and any other member is refused
  (Q-REPO-417); nothing follows a preset and the server fills in no hosts (Q-REPO-418); who may write and read
  the list, and that whoever may create a remote in a project reads that format's presets (Q-REPO-419); the
  list is one document replaced whole (Q-API-383); it is kept in a section of the upstream registries' page
  without reordering (Q-UI-906); the presets are a group of their own in the creation form only (Q-UI-907);
  at most ten per format, a name or address once per format, a well-known registry's address refused.
  **Decided by the signatures' builder:** candidates name paths that the core resolves only in the event's
  repository, and a detector's error never fails a commit (Q-SIG-47); an OCI signature's object is its manifest
  (Q-SIG-48); an object above the size bound is recorded as malformed (Q-SIG-49); npm's registry signature has
  its signed message as subject (Q-SIG-50); where a remote's npm attestations document is stored (Q-SIG-51);
  files Quader derives record no origin (Q-SIG-52). **Their tests cost:** the presets about 1 s of unit tests,
  3 s of integration, 2 s of UI tests and about 30 s of the layout job (five states and an empty state); the
  signatures about 1 s of unit tests and a new integration area of two tests of 1.7 s each; nothing in an
  end-to-end leg. **Not verified:** a real registry behind a preset; the German screens in a browser; real
  clients and real signatures of nuget.org (the NuGet samples are hand-built structures with nothing signed);
  the replacement of an attachment's parts for Debian beyond the generic case. **For the ClamAV change's
  assembly:** its migration takes the next free number, its decision Q-REPO-417 becomes Q-REPO-420, and the
  format `clamav` gets its declaration of signatures and its row in the specification's table, without which
  the contract's version 8 does not build it.
- **In work** (10:57; of these three the chart's and the format's builder have reported, below): three builders, each from a specification that is on `main` (section "With the owner" holds
  what each builds): the format `clamav` with its repository and API (package 1 of the ClamAV change), the chart
  with the updater's Deployment (package 3 of it, without a cluster, because the binary does not know the updater's
  kind before package 2), and the format contract's declaration and the store of signatures (package 1 of the
  signatures' first stage). **The ClamAV change lands as one:** packages 1, 2 and 3 are collected on one branch,
  because the chart's updater cannot start before the binary knows its kind and the import must not stop between
  two releases on the owner's installation; package 2 (the updater process and the import) starts on package 1's
  branch when that reports, the UI packages after their APIs exist. One builder's worktree was created at the
  pushed tip and lacked the two specification commits; it was told to fast-forward before reading.
  **The chart's builder (package 3) reported at 11:26** with one commit that waits for packages 1 and 2. What the
  chart renders now, while malware scanning is on and the database's source is the updater (both defaults): the
  Deployment `<fullname>-malware-updater-freshclam` (one replica, `Recreate`, outside the rule of two, no
  disruption budget, autoscaler or spread; the containers `quader` of role `scanner` and kind `freshclam`, and
  `freshclam`), its ConfigMap with `freshclam.conf` (`ScriptedUpdates no`, the official origin or
  `PrivateMirror`, the proxy lines, no `NotifyClamd`), its Service, its NetworkPolicy (the listener from the
  worker that claims `malware`, the operations port from the ops peers) and its certificate; the `packages`
  policy admits the updater's pods on the scan listener; the worker lost the `freshclam` container, its volume
  and its ConfigMap, and the values `freshclam.*` are refused by the schema; `<fullname>` is limited to 37
  characters; the end-to-end profile of `vuln` takes its database from the repository and renders no updater.
  **What the chart sets and package 2 has to know:** `malware.updater.url`, `malware.updater.server_name`,
  `malware.updater.client_name`, `malware.scan_listener.updater_client_name`, a third entry of
  `server.scan_clients`, the environment `QUADER_PROCESS_SCANNER_KIND=freshclam` and no spool size on the
  updater; it does not set `malware.updater.directory` and relies on the binary's default `/var/lib/clamav`;
  the marker `.quader-seeded` and the version file `.quader-tool-version` in that directory. **Decided by the
  builder (Q-HA-384, renumbered before the landing):** the shape of `privateMirror` and `proxy` is checked so that no value adds a line to
  `freshclam.conf` or puts a credential into a ConfigMap, and a mirror on `clamav.net` itself is refused; a
  replica count of zero fails also during a restore. **Its gates, each exit 0:** the chart's lint, the full unit
  run, links and check, the complexity check; **the chart's tests exit 2 on three tests** that hand the rendered
  configuration to the binary, which does not know the two new keys before package 2 (expected; they are run
  again on the landing branch). The owner's values render against it with five new objects and without the
  worker's ConfigMap, no key of his has to change. **For the landing:** his workers carry the label that opens
  the internet to them, so the updater's pods need the same label in his values (not verified: whether his
  cluster denies egress by default and whether that label covers DNS). **Left to package 2:** the README's
  configuration reference still lists the three keys of the old container, and the operations manual has no
  row for the updater's readiness check. Nothing ran on a cluster.
  **The builder of the format, the repository and the API (package 1) reported at 12:30** with one commit,
  rebased by it onto `main`. What exists: the format `clamav` with the routes `<db>.cvd` (the version marked
  in use) and `<db>-<version>.cvd.sign`, whose refusals answer 404, whose limits answer 503 with `Retry-After`
  and whose 401 asks for Basic, so that no answer is a 403 or a 429; the package view, the client's snippet and
  guide; the format contract's kit with six skips, each with its reason; the fact that a format is the
  system's alone (a create in a project answers 422 `format_system_only`); the parser of a container's header
  and members as a leaf package with a builder of test containers; the table of versions and the column of the
  switch (migration 00091); `upstream/clamav` in place of `scanner-clamav` with the switch (off: as if missing,
  level private; on: public, or internal with a warning where the installation allows no public level; off
  again: back to private, journaled, so that a restore replays it); no deletion of a definition file by hand
  (409), and the malware gate never asks about a system repository's items. **Decided by the builder:** a
  version's row stays as history after the cleanup removed its files (Q-CLAMAV-15); the snippet writes the
  address without its trailing slash, because `freshclam` would otherwise ask for `//daily.cvd`, which the
  dispatcher refuses (Q-CLAMAV-16); a commit without its metadata is refused (Q-CLAMAV-17); switching the
  clients off is a part of the journal of its own (Q-REPO-417). **Today's import** was left in the smallest
  working state: it writes the new layout without verifying and marks nothing in use, so the endpoint answers
  404 until package 2. **Its gates, each exit 0:** build, the full unit run, generate-check, the lint, the
  complexity check (one function was over the threshold once and got a helper), links and check, the UI's
  build, tests and lint, the integration areas `malware` and `upstream` under the lock. **Its tests cost:**
  milliseconds of unit tests with containers of a few hundred bytes; about 2 s in the area `upstream`; about
  7 s in the area `malware` for the replay of a stock client's requests, 5 s of which are two waits for the
  replicas to read the switch (the specification had said 4 s); nothing in an end-to-end leg. **Not
  verified:** the 503 at a request limit and an external user's 404 in an integration case (the unit test
  covers the mapping); the replay of the switch's journal part after a restore; a real `freshclam` against
  the endpoint. Not built, because specified after its start: what the client certificates add.
  **The landing branch `clamav-landing` exists since 12:32:** package 1 and, rebased onto it, the chart's
  commit (three conflicts in documentation merged by the development agent: the decision blocks of the chart
  beside those of the analyser's move, the developer page's row, the security page's sentence); it builds,
  links and check exit 0. Package 1's worktree and branch are removed, its commit is on the landing branch.
  **In work since 12:33 on that branch, two builders:** package 2, the updater's process kind `freshclam` and
  the worker's import with verification, marks and cleanup, the removal of the old container's keys, the three
  chart tests green, and **one measurement on a cluster of its own** (the updater pod with the real databases,
  the import, the scanners, and a stock `freshclam` with `PrivateMirror` against the endpoint; recorded, not a
  test); and package 4, the screens (the switch and the section in the admin area, the packages per database
  with the version in use first, the client setup with and without a token), with renders for the development
  agent's look. The two commits are put onto the landing branch when they report, and the change lands as one.
  **The builder of the screens (package 4) reported at 13:18 and, after three corrections, at 13:24** with one
  commit on the landing branch that waits for package 2. What an administrator sees: a column **Clients** in
  the table of the system repositories with the switch **ClamAV definition updates** in the row of `clamav`
  (a question before switching on names the address and the level, one before switching off names the 404);
  on the registry's page a first section with the switch, the endpoint's address, the level with the fold
  **Visibility**, the warning where the level had to be lowered, and the one line for `freshclam.conf`, plain
  at the level public and with a token below it. What a reader sees: one row per database with the version in
  use, its build time, its age and the number of versions, the set in use and the one before it above the
  table; a database's versions with their state (in use, previous, superseded, files removed); a version's
  files, members and every header field with the verification; the client setup with the snippet and guide.
  While the switch is on, the repository is offered like any repository its reader can read and keeps the
  badge **System**; while it is off, the rules of a system repository hold. **The renders looked at by the
  development agent** (the registry's page, the list of databases, a version's details; light and dark, wide
  and narrow). **Sent back with three corrections:** the list marked `main` and `bytecode` as outdated,
  because the specification put ClamAV's warning of seven days on every database, and ClamAV rebuilds those
  two rarely; decided by the development agent for the owner's walkthrough (Q-UI-912): the warning and the
  badge **Outdated** hold for `daily` alone, which the builder read in ClamAV's source before writing it down
  (the load of a container checks the build time only of a file named `daily.`, at more than 604,800 seconds;
  release 1.5.5 was read, no other); the age beside its label read "Built today" and reads "under a day"
  now; its decision numbers were set behind those the presets took (Q-UI-908 to Q-UI-911, Q-API-384).
  **Decided by the builder:** while the switch is on the repository is offered like any readable one
  (Q-UI-908); the admin page builds the client's line from the address the API gives (Q-UI-909); a database's
  versions take their state from the version list (Q-UI-910, with the one addition to the API, Q-API-384); the
  install panel leads to the client setup instead of a command per version (Q-UI-911). **Its gates, each exit
  0:** build, the full unit run, generate-check, links and check, the complexity check, the UI tests (1,856),
  the UI lint, the layout checks; the lint was red on a test file of package 3 (formatting), which package 2's
  builder corrects in its commit. **Its tests cost** about 0.6 s of UI tests and an estimated 60 to 90 s of
  the layout job (not measured in the pipeline). **Not built, because the API does not report it yet:** the
  section **Updater** with the updater's replicas; and, as briefed, what the client certificates add. **Not
  verified:** the German texts in a browser; the switch against a real updater; the lowered level against a
  server that allows no public level.
  **The builder of the updater's process and the import (package 2) reported at 13:43** with one commit on the
  landing branch. **What runs where:** the updater pod's `freshclam` fetches ClamAV's whole signed files; its
  `quader` container (role `scanner`, kind `freshclam`, no secret of the installation) copies the set in use
  from the scan listener into the shared directory at its start, lists and serves the directory to the worker
  over mutual TLS, reports itself as a scanner replica and exports the age of its newest `daily`; readiness
  check, log field and replica rows carry the unit's name. The worker's import (every 30 minutes and once at a
  worker's start) fetches only what `upstream/clamav` does not hold, checks each container's checksum against
  its header and the header's signature against ClamAV's public key, checks every new file of a set before it
  commits any, records the set and marks it in use in one transaction; the cleanup leaves files only to the
  set in use and the one before it, and the history rows go after the rescan cycle. **The legacy signature**
  was read in ClamAV's source (release 1.5.5) and built with big integers; it verifies the three real
  containers that lay on the machine, and a changed signature character fails. **Configuration:** new are the
  database's source (`updater`, the default, or `repository`) and the updater's address, names and directory;
  gone without an alias are the four keys of the old container; the malware settings of the API name the
  source. **Decided by the builder:** a put-back keeps the refused version until the next set (Q-MAL-102);
  every new file of a set is checked before any is stored (Q-MAL-103); the updater's report carries its
  `daily`'s build time and no slots (Q-OPS-226). **Its gates, each exit 0:** build, the full unit run,
  generate-check, the lint, the complexity check, links and check, the chart's lint and tests (the three that
  had waited for the binary pass), the UI tests and lint, the integration area `upstream`; in the area
  `malware` one test still asserted the old field, was corrected and passed alone, and the whole area runs
  again at the assembly. **Its tests cost** about 1 s more in the unit job and about 3 s in the area `malware`.
  **Measured once on a kind cluster of its own, with ClamAV's real files** (recorded on the format's developer
  page; cluster and image removed afterwards): the updater is ready 5 s after its start; `freshclam` fetched
  about 113 MB from ClamAV's network in 4 s, the detached signature files among them; the import took six
  files, all three containers verified by ClamAV's signature, and marked them in use; both malware scanners
  loaded the set; after a restart the updater seeded the six files from Quader in 0.5 s and `freshclam` fetched
  nothing from ClamAV; the list of scanners shows the updater with its tool's version and the set. **A stock
  `freshclam` with `PrivateMirror` at Quader's endpoint, the switch on at the level public:** per database it
  asked for the `.cld` (404), the `.cvd` with a range (206), the `.cvd` (200) and the signature file (200) and
  ended with exit 0 after 7 s; its second run asked for the `.cld` (404) and the `.cvd` with a range and the
  date (206), said up to date and ended with exit 0. That is the recorded measurement with a real client that
  was owed. **Not measured:** a token in the client's URL; the plain download; a new `daily` arriving while
  the updater runs. **Not verified:** the 503 at a request limit and an external user's 404 in an integration
  case; the database's write of the switch's journal part after a restore. **Found by it:** on a fresh
  installation the worker's first run came three seconds before `freshclam` had finished, and the next run
  would have been up to half an hour later; decided by the development agent for the owner's walkthrough and
  built at the assembly: while no set is in use the import asks again every 30 seconds. Inside a cluster a
  ClamAV must name one of the endpoint's accepted hosts, another name answers 421, which `freshclam` counts as
  a failed attempt (section 8).
  **The assembly is done (13:44 to 14:27; the screens' commit onto it by 14:41) and the change landed at 14:46
  (the entry above).** It was planned as follows, by package 2's builder: the three commits of the Go side and the
  chart onto today's `main` (the migrations renumbered behind those of the presets and the signatures, the
  decision of package 1 renumbered, the format's declaration of signatures for the contract's version 8 with
  its row in the signatures' table, the copy of the values file equal to the chart's again, the 30 seconds
  above), every gate on the tip and the whole area `malware`. The screens' builder puts its commit onto that
  tip afterwards, and the change lands as one push of four commits.
- **Landed on 2026-11-21 at 10:47: a system repository is set apart wherever it is shown** (R-UI-193, Q-UI-898 to
  Q-UI-902): the badge **System** in the admin list and in the search, no address, download or command that would
  answer 404 (the admin list, the file page with one sentence in the address's place, a folder's rows, an opened
  image row, a version's files, the install panel), the heading **System repositories** on the admin page of the
  upstream registries too, and no system repository where someone picks a repository to work with. On the commit
  as rebased, each exit 0: build, the full unit run, generate-check, links and check, the UI tests (1,837), the UI
  lint; the renders looked at (admin list with its menu, the file page, the search field; light and dark, wide and
  narrow); no change of the chart; the owner's UI backend started with it from the main checkout before the push.
  Its worktree and branch are removed. **Its release was cut at 11:01, image and chart were published at 11:07, and the owner's
  installation runs it since 11:09** (read on the cluster: five Deployments with two ready pods each on the new
  image, no restart, no pod outside Running, the ingress answers). **Found by its builder and left (section 8):** a disabled
  repository's file page offers **Download**, which answers 404; the version page's header shows the format's key
  instead of its name; the decisions' index stops at Q-UI-844; one stylesheet is 227 bytes over its warning
  budget. **The pipeline of this landing (run 44891227453) ended green on every job at 11:31,** the layout job
  with the four new render cases among them. **Not verified:** the German screens in a browser.
- **The two builders of 09:22** (both landed, entries above): the scanner replicas on the screens (since 08:08), and **a system repository
  set apart everywhere** (since 09:20; what the builder of the project page's section had reported and left, taken up
  at once by the owner's word of that morning to such reports): no action that leads to a 404 (the admin list's
  **Copy client address**, **Download** and the curl command on a system repository's file page, install commands
  of a package there); one name, "System repositories", on the admin area's page of the upstream registries too;
  not offered where someone picks a repository to work with (the guides' list, the exposure rules' select, the
  security dashboard's filter, the limit explanation's suggestions); still found by the search and in the admin
  list, with a quiet mark "System". Left: **Recently opened**, and the alignment of the section's columns, which
  is with the owner. The choices are the development agent's, by the default that a system repository is never
  offered for a client's work and stays findable for an administrator; the owner sees them in the renders.
  Housekeeping at 09:19: the worktrees and branches of the rename, the section, the names and their landing branch
  are removed, every commit of them on `main`. **Landed on 2026-11-21 at 09:05 in one
  push, after the rename's pipeline had ended green on every job (09:04):** the system repositories' section, the
  names an operator or an auditor reads of a scanner, and the rule's extension in `CLAUDE.md` (pipeline
  44883240572). The two builders' commits were collected on a landing branch without a conflict; on its tip, each
  exit 0: build, the full unit run, generate-check, the UI tests (1,811), the UI lint, links and check; the
  owner's values rendered against its chart (35 objects, his release name passes the new check of the names'
  length); the owner's UI backend started with it from the main checkout and answers ready, its two scanner checks
  under the new names. **The names, as built** (builder from 08:08 to 09:00, one commit of 63 files): the label
  `listener` of a link carries the unit's name at both ends (`vuln-scanner-trivy`, `malware-scanner-clamav`; `scan`
  stays, the scan listener of `packages` runs no tool); the log field `component` of both scanner processes and of
  the signature database's loader, which is a part of its unit; the readiness checks `vulnScannerTrivy` and
  `malwareScannerClamav`, one name per unit on both sides, with a sentence each on the System page that names the
  tool (the image scanner's had none); the system subjects and the usage records' credential
  (`vuln-scanner-trivy`, `malware-scanner-clamav`); the components' names in start, stop and drain messages. The
  chart computes the longest name it builds from its table of kinds and its other suffixes and fails the render
  with the name, its length and what fits (today a `<fullname>` of 40 characters); `quader.fullname` is cut no
  more. Kept on purpose: the error codes `scanner_busy`, `scanner_draining`, `scanner_unavailable` and
  `scanner_lost` (they answer for whichever scanner is called), the worker's task subjects, the table
  `malware_scanners`. **Its decisions:** Q-HA-373 (the owner's sentence, which moves the boundary of Q-HA-372),
  Q-HA-374, Q-OPS-221, Q-OPS-222. **Gates of the builder, each exit 0:** build, the full unit run, generate-check,
  chart lint and tests, lint, links and check, the UI tests and lint, the areas `vulnimage`, `malware`, `limits`,
  `authz`, `server` and `tls`, and the set `vuln` on a cluster of its own with an image tag of its own, the commit
  of a pod read before and after. **Looked at by the development agent** (rule 11, the System page at 1440 px dark,
  rendered from the main checkout): the ready replica shows both warnings with their sentences, "Malware scan: this
  worker has no ClamAV scanner to call …" and "Image scanning: the Trivy scanner of this replica cannot analyse
  images …"; a lost replica's row of before the change still shows the bare old name until the registry drops it.
  **What an installation sees at the upgrade:** dashboards, alert rules and log queries that select the old values
  match nothing and are edited by hand; usage rows written before keep the old credential; a release whose
  `<fullname>` is longer than 40 characters fails the render. **Not verified:** a rollout with both versions at
  once (a token signed by one should be refused by the other: read, not tested); the German sentences in a browser.
  **Released at 09:24, and the owner's installation took it by itself at 09:31** (read on the cluster): the five
  Deployments ready on the new image without a restart, no error in the worker's log, the malware scanner's log
  under its unit's name, the ingress answering. **What the rename's builder reported and left is
  built at once** (the owner, 2026-11-21, to that report: "Vom Builder gemeldet, nicht geändert: direkt nacharbeiten.
  Das ist alles wichtig und muss umgesetzt werden"), in two packages on top of the rename's commit, both since
  08:08. (1) **No name of a scanner unit that an operator reads is left without its tool:** the label `listener` of
  the links' certificate metrics, the log field `component` of both scanner processes, the readiness checks on both
  sides (with the sentence the image scanner's check never had on the System page), the system subjects
  `image-scanner` and `malware-scanner` that an auditor reads as the actor of a scanner's reads (added by the
  development agent: they name the unit too), anything else of that kind its grep finds; and the chart fails the
  render for a release name whose longest derived name is no valid DNS label. The role, the jobs and queues, the
  configuration keys of the worker's one link per purpose and the repositories keep their names. (2) **An
  administrator sees the scanner replicas and their tool:** the admin area's System page lists every scanner
  replica with its kind, the tool's version, its state and since when; **Settings › Malware scanning** names each
  malware scanner with the ClamAV version, the signature database it scans with and its slots; the image scanning
  shows its scanners the same way. The mechanism is the builder's to decide within bounds given to it: a scanner
  holds no secret and writes no table, its kind comes from its verified certificate and not from what it says of
  itself, a gone pod's row goes as the other replicas' rows go, what a scanner reports is bounded, and no end-to-end
  set or leg is added. Both builders run the set `vuln` on a cluster of their own; the image tag of the development
  profile is shared on the machine, so they build and load it under one lock and check the commit a pod runs.
  **The system repositories of `upstream` stand apart on the project's page Repositories, landed at 09:05** (builder from 07:53 to 08:21, one commit of 29 files; the owner, 2026-11-21, with a
  picture of the page on his installation: "Bitte die internen Repos wie: scanner-trivy und scanner-clamav unter die
  Liste packen, am besten als seperate Tabelle die standardmäßig eingeklappt ist. Sowas wie system repositories, oder
  etwas besseres. Ohne den client einrichten button. Einfach bewusst abgesetzt."). **As built:** the list holds the
  twelve registries alone; below it, after a hairline, the shared disclosure **System repositories** with a count,
  closed at every visit, with the sentence "Quader keeps these repositories for itself: only its scanners read them,
  and no client is set up for them.", and inside a table of the list's columns. The filter, the drawer's format,
  class and state included, opens the section where it matches a system repository and counts the matches; the
  section's rows are read by name once per visit and filtered in the browser, so they do not depend on the list's
  page. No **Set up client** for a system repository anywhere (the list, the content page, the guides, a package's
  install panel: the helper that builds the button returns nothing for one, and the type forces every caller to
  handle it). The project's description under the title, on the start page and in the search is a localized text of
  the UI (it was the stored English row). **Found by the builder in its renders and built with it:** the content
  page of a system repository offered **Upload files** and **New folder**, which the server refuses for every caller;
  they are gone there. **The name:** "System repositories", the specification's word since R-REPO-066 and the
  owner's own suggestion; "internal" was not taken because it reads as the visibility level. The admin area's page
  of the upstream registries calls the same two "Used by Quader": two names for one thing, left for the owner.
  **The row menu of a system repository:** Settings, Files, and for `scanner-trivy` Vulnerabilities (always empty,
  the scanning of that repository is off); no delete, no move. **Its decisions:** R-UI-192, R-UI-190 amended,
  Q-UI-891 (the owner's) to Q-UI-896. **Gates of the builder on its commit:** the UI tests (1,811, 29 s), the UI
  lint, the layout check's tests, the localized build, links and check, the renders (no layout finding). **Looked at
  by the development agent** (rule 11): the page closed and opened at 1440 px dark, opened at 390 px light, and with
  the filter "trivy" at 1440 px light (the list says no repository matches, the section is open with one row).
  **Seen in them and left, named to the owner:** the section's columns Format and Class stand about 120 px right of
  the list's, because its table is indented and its action column holds the menu alone. **The development agent's
  full unit run on the commit failed in one test**, the count of requirements in the coverage tool's test (1,048
  against the expected 1,047: the builder had run the manual's Go packages only); corrected by the builder at 08:27,
  the full unit run of 176 packages then exit 0. **Landed at 09:05** (the entry "In work" above). **What it costs:** four component tests (0.3 s), two render cases with 24 pictures, about 19 s of the job
  `UI layout checks`. **A change of shared state, by the documented way of rendering from a worktree:** the render
  seed switched four registries on (`ghcr`, `quay`, `ubuntu`, `debian-security`) in the database of the owner's UI
  stack. **Where a system repository still stands among the repositories a user works with, reported and unchanged,
  to put to the owner one at a time:** the admin area's list of repositories (with **Copy client address** for an
  address that answers 404), the search's suggestions and its tab Repositories, the guides' list (a row with an
  empty action cell now), the repository pickers of the exposure rules, the security dashboard and the limit
  explanation, **Recently opened**, and the file page of a system repository, which still offers **Download** and
  a curl command that answer 404. **Not verified:** the German page in a browser (the German bundle carries the
  texts); the whole render run with four registries on.
  **The two scanner units' rename, landed on 2026-11-21 at 08:12** (builder from 06:48 to 07:54, one commit,
  84 files; pipeline 44879147044): the kinds `vuln-scanner-trivy` and `malware-scanner-clamav` in the chart's table of kinds, from which
  every Deployment, Service, disruption budget, autoscaler, NetworkPolicy, Certificate and Secret follows, with the
  component label, the names in the certificates, `server.scan_clients`, the scanners' addresses and the keys of
  `tls.existingSecrets`; the value blocks `vulnScannerTrivy` and `malwareScannerClamav` (the schema refuses the old
  ones); `process.scanner_kind` takes `trivy` or `clamav`, default `trivy`; the development profile, the
  end-to-end suite's selectors, the integration harness, the list of paths that starts a leg; the specification, six
  security pages, five developer pages, the manual, the README's naming tables, and the rule in `CLAUDE.md`. **Its
  decisions:** Q-HA-372 (the owner's, with his words and the boundary as the development agent's choice), Q-ARCH-561
  (the values of `process.scanner_kind`), ADR-223. **Fixed on the way, each a miss of the malware scan's landing:** the
  manual's example of `tls.existingSecrets` lacked the malware scanner's Secret, so following it failed the render;
  the README's reference of the values had no block for ClamAV, the malware scanner or `freshclam`; the
  specification's alert expression and default policy list missed the malware scanner; a chart test covered the
  malware scanner only by an accidental match of a suffix. **Gates of the builder on its commit, each exit 0:** build,
  the full unit run, generate-check, chart lint and tests (119 s), lint, links and check, the areas `vulnimage`,
  `malware`, `server` and `tls`, the set `vuln` on a cluster of its own (both scanners ran their cases under the new
  names; one browser test failed first on a missing Playwright install of the worktree and passed after it), the
  render with an issuer and with five existing Secrets read for old names. **Of the development agent on the commit
  rebased onto `main`:** build, the full unit run, links and check, each exit 0; the owner's values rendered against
  the new chart (35 objects, the five Deployments and Certificates under the new names, no old name); the set `push`
  on a cluster of its own, because its Valkey case names a component and the builder had not run it (exit 0 at
  08:11 after 719 s, the Valkey case with its three start cases among them); and before the push the owner's UI
  backend started with it from the main checkout and answers ready. **What an operator still sees with the purpose's name, reported by the builder and left for the package
  that moves the detection into the unit:** the label `listener` of the links' certificate metrics (`scanner`,
  `malware-scanner`), the log field `component` of both scanner processes, and the readiness check `scanner` that both
  kinds register. **What the screens show for a scanner:** nothing on the admin area's System page (a scanner writes
  no row of the replica registry, R-HA-002) and only the counts "ready" and "scans running" on **Settings ›
  Malware scanning**; so no screen names a scanner pod or its tool, a point for the walkthrough. **A finding, not
  fixed (section 8):** the Service `<fullname>-malware-scanner-clamav` is a valid DNS label only up to a release
  name of 40 characters, and nothing checks the sum. **What an installation sees at the upgrade:** both Deployments
  and their Services, budgets, policies and Certificates are removed and created under the new names, every pod
  rolls, scanning pauses until the new scanners are ready, and the two certificate Secrets of the old names stay
  behind. The owner's values name none of the renamed keys. **Released at 08:26, and the owner's installation took
  it by itself at 08:34** (read on the cluster): the five Deployments ready on the new image, the two scanners as
  `vuln-scanner-trivy` and `malware-scanner-clamav` with their Services, budgets, policies and certificates, nothing
  of the old names left but Secrets, no restart; both malware scanners loaded the signature database into `clamd`
  again, the image scanners answer ready, the worker logged no error, the ingress answers. **Left behind there, three
  Secrets nothing uses any more:** the two of the old kinds' certificates and the one of the certificate that was
  written by hand before the chart issued one per kind; named to the owner with the command, because his cluster is
  changed through his deployment repository only and no object of it owns them. The comment and the README of the
  installation's directory in his deployment repository name the new kinds (committed there by the development
  agent).
  **Housekeeping at 07:02:** the worktrees and branches of the malware scan's landing, of its third package and of
  the lock script are removed, every commit of them being on `main` (the third package's as the commits of the
  landing: no difference between its tip and the landed tree but this plan); nine worktrees of another session
  stay. No builder of the malware scan is at work; their records, kept until the walkthrough:
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
  owners later; presets of the installation's own registries, after the project `upstream` (built and landed on
  2026-11-21, run record).
- **Housekeeping:** nine worktrees are locked by another session of 2026-11-18 that still runs and were left alone.
- **To check first:** the pipelines from 17:00; F-165 in the next full nightly; then section 12.

### With the owner

Everything that waits for the owner, one line each, with where the detail is. Put to him one at a time (standing rules).

**The open questions, to be put to him one at a time on the evening of 2026-11-22**

He asked on 2026-11-22 at 08:00, away from his desk, to go through the open questions one by one; the first was put
to him, and at 08:25 he moved the walk to the evening: "Das machen wir doch besser wenn ich heute Abend wieder am
Rechner bin. Schreibe die Fragen bitte auf damit wir sie nicht vergessen." The questions, in the order they are put
(by weight; each is built with the choice named, and its options and detail are in the entries below):

1. **Who reads through the signature gate of a gated OCI repository to sign an image (Q-SIG-211).** Built: (a) who may
   write to the repository. Put to him on 2026-11-22 in the answer to his message of 08:00, with a finding from
   reading the code again: a writer's read through the gate leaves no audit event and no metric (`Verdict.Writer` is
   set in `internal/signatures/gate/gate.go` and read nowhere outside the tests), where every refusal leaves an event,
   a metric and a row of the refused downloads. So the question "who pulled an unsigned image out of a gated
   repository" has no answer today. **The recommendation is now (a) with that trace** (an audit event and a metric for
   each such read; a small package, tested at the unit tier); (c), nothing passes and an image is signed elsewhere
   and promoted, stays the strict form for a later promotion flow. Not verified: `notation sign` after the repair;
   whether `cosign copy` into a gated repository works (the way (c) would need). Unanswered.
2. **How strong the gate is without chosen anchors (Q-SIG-76).** Built: (a) whatever shows as verified passes, with a
   warning at the setting.
3. **A gate at the upload as well for npm and NuGet (Q-SIG-79).** Built: (a) no, one behaviour for every format.
4. **Debian and the gate (Q-SIG-80).** Built: (a) the setting is not offered.
5. **Where an image's receipt stands (Q-SIG-66).** Built: (b) as a referrer only where it hides no older signature of a
   publisher, otherwise through the API.
6. **What proves the time of a receipt (Q-SIG-63).** Built: (a) nothing for now.
7. **Whether his company's CA issues ECDSA P-256 code-signing certificates (Q-SIG-53),** a fact only he has, and with it
   what his installation gets as its signing identity (nothing is signed there until it has one).
8. **A registry signature for Ansible Galaxy (Q-SIG-68).** Built: (a) none.
9. **What "betrieben" covers** in his note "Alle 3 Wege sollen von dir gebaut und betrieben werden": the signing
   services on his cluster and in his CI, or the tests alone. Asked on 2026-11-21, unanswered.
10. **A scanner pod whose process is taken over answers false detections (H-1181).** Built: (a) accepted and taken into
    the security pass before BETA.
11. **The binding of a host to a repository of `upstream` at its root** (below, "Questions he has not answered").
12. **The next lever of the test suite** (below; the fourth integration shard is cut since 2026-11-21, 23:44, by the
    development agent; what is left to decide is whether the formats' repeated integration tests move into the format
    contract's kit, and the other candidates of the measurement).
13. **What frees a release once the full gate returns** (below; a question for later).
14. **Image pulls through a Bastion and the main installation's miss limit (Q-BAS-167),** from the planner of
    "a Bastion serves every format" on 2026-11-22: a registry token's read shape is capped at partial, so every warm
    pull costs the main installation one small request per layer, counted against the miss limit of the client's
    address (300 a minute by default; a cluster behind one address can reach it in a rollout). (a) build it so
    now (specified, the planner's recommendation); (b) answers per package at once, which removes those requests
    for images and for callers with restricted rights of npm and NuGet, the larger change.
15. **ClamAV installations with a client certificate through a Bastion (Q-BAS-176):** (a) through a Bastion they
    update anonymously or with a token, and certificates work against the main installation's certificate endpoint
    alone (specified); (b) the Bastion checks a certificate and vouches for it, against his rule that Quader takes
    no certificate from a header; (c) TLS passed through to the main installation, without a cache.
16. **A Docker Engine's mirror address through a Bastion (Q-BAS-178):** (a) one Bastion release per bound host,
    which learns its repository from the main installation (specified); (b) not offered; (c) the bindings in the
    Bastion's chart.
17. **A Bastion on his own installation, to see images and Debian served through it** (he reminded on 2026-11-22 at
    11:19: "Denke dran dass du mir noch Images und Debain über den Bastion schuldest."). His installation runs no
    Bastion (read on the cluster and in his deployment repository the same minute), so the feature is proven
    on a kind cluster and with clients in containers, and nothing of it shows on his cluster. (a) the development
    agent adds the Bastion's chart under `apps/quader` once packages B and C are released, and he names a host
    and issues the Bastion's token in his admin area; (b) not on his cluster for now.
18. **A speed limit keeps nearly every caller away from signed links (Q-LIM-97),** from the planner of the storage
    topic's third stage on 2026-11-22. His decision 11 took the condition "no pace limit applies to the caller" as
    the working paper put it, and every built-in profile but Unlimited sets a speed limit, so on a new
    installation only administrators would get links. (a) keep the condition, the settings page names the
    profiles it keeps away (specified meanwhile); (b) a speed limit does not stop a link: the transfer by a link
    is not slowed, the budgets of an hour and a day are charged when the link is issued (the development agent's
    recommendation: the condition was its own wording in the paper, a speed limit protects Quader's pods, which a
    link's transfer does not pass, and with (a) the switch does next to nothing); (c) the Standard profile loses
    its speed limit.
19. **A connection taken out of the chart while it holds the only copy of some blobs (Q-STOR-225),** from the planner
    of the storage topic's first stage on 2026-11-22: (a) it becomes detached, the replicas start, those blobs
    answer 503, an alert fires, and putting it back restores everything (specified, the planner's
    recommendation); (b) the rollout is refused; (c) it is forgotten at once.
20. **Whether draining a connection deletes its copies (Q-STOR-236):** (a) yes, after its checks, and the journal and
    the marker stay (specified; with (b) switching encryption on would leave the plain copies in place, and the
    same bucket under another prefix is paid twice); (b) no, the operator deletes them.
21. **A fresh database that meets a bucket an installation has used (Q-STOR-224),** which a reset of his own
    installation will meet: (a) refused, with a message that names the object to delete (specified: two
    installations on one prefix delete each other's data through the collection and the sweep); (b) a warning,
    and the marker is taken over; (c) no marker.
22. **Leaving a provider: whether a storage key can be destroyed (Q-STOR-309),** from the planner of the storage
    topic's second stage on 2026-11-22. The development agent had told him that deleting a provider's storage key
    makes what stays there unreadable. Specified: (a) storage keys are never deleted, because old versions of
    objects and the pools of kept backups may still need them; (b) a command that destroys a key and is refused
    while a kept backup names it (the planner's recommendation for the day an installation needs it); (c) deleted
    on its own when the connection goes. So the side effect he was told is not built with (a).
23. **A copy between two encrypted connections (Q-STOR-308):** (a) always a new data key, so a leak at one
    connection never reaches another, at the price of the server-side copy between two encrypted connections of
    one provider (specified); (b) the data key is carried and wrapped under the other storage key; (c) a setting.
24. **Object names in a backup's pool (Q-BAK-65):** (a) the pool keeps digest names, so a reader of the backup's
    target can confirm known files, which adds nothing while the database files beside them are plain
    (specified); (b) keyed names in the pool, together with the backup's own encryption; (c) a plain pool.
25. **The object gateway of his cluster and signed links:** his cluster runs the 19.2.3 release of its object store,
    and the line's release notes name a security defect of signed links for uploads that is repaired in 19.2.6
    and whole in 19.2.7 (read by a research on 2026-11-22, not checked on his cluster). Quader signs links for
    downloads alone; still, (a) he brings the gateway to 19.2.7 or newer before the switch for signed links is
    turned on there (the development agent's advice); (b) he leaves it. Whatever he chooses for Quader, the
    defect concerns every other use of signed upload links on that gateway.
26. **Whether a speed limit stops a handover to a Bastion as it stops a signed link to a client (Q-STOR-343),**
    from the planner of the handover on 2026-11-22; put to him right after question 18, whose twin it is.
    Specified: (a) no; the transfer from the store to a Bastion never passes the `packages` pods, a Bastion's
    answers from its cache were never paced, and the budgets of the hour and the day are charged when the
    handover is given. (b) Yes, as for links; then on a new installation next to no caller gets a handover,
    because every built-in profile but Unlimited sets a speed limit. The planner's and the development agent's
    recommendation is (a). **To be told to him with it, not a decision of Quader's:** the object gateway of his
    cluster has an address inside the cluster alone, so a Bastion of his reads from it only once his network
    gives the gateway an address the DMZ reaches (the same holds for signed links to clients outside).

Not a decision any more, to be told to him in a sentence: what a virtual npm repository's keys endpoint serves before
Quader signs local packages (Q-SIG-34, built as (a)); the second stage signs the local packages of an installation
with a signing identity. **Beside the questions, his when he is back** (run record, the state of 03:40): the walkthrough
of the new screens; what his installation needs before anything is signed or gated there and before a ClamAV client
updates with a client certificate; the two things on the development machine; the three orphaned Secrets on his
cluster.

**A topic he opened on 2026-11-22: encryption and integrity of the stored content, several object stores, a Bastion
that reads the bucket**

- **His words** (08:25): "Lass uns nun über die Verschlüsselung und Integrität der s3 Daten sprechen. Schau dir dazu den
  s3-encryption-proxy v5 an. - Wir brauchen ein Setting um durch den Admin bestimmen zu lassen ob wir unsere Artefakte
  direkt oder durch signierte s3 Links ausliefern lassen - Es gibt ein Flag um die Daten im s3 verschlüsselt
  abzulegen, dann müssen wir natürlich die Daten ausliefern und können das nicht mehr durch s3 links machen - Wir
  wollen mehrere s3-Verbindungen gleichzeitig bieten um gegenüber Ausfällen einzelner Anbieter resilient zu werden. Ob
  verschlüsselt wird, kann pro Verbindung entschieden werden. - Ein Bastion kann auch einen Read-Only Account auf ein
  s3 Bucket bekommen und damit die Hauptinstanz weiter entlasten - Der Instanz KEK muss rotiert werden können - damit
  ein Bastion ggf. Nur Teile der Artefakte aus dem s3 lesen kann, gibt es neben dem Instanz KEK auch noch Projekt-KEK
  und Repository-KEK. Ein Bastion bekommt dann ggf. nur die Schlüssel die er tatsächlich braucht. Ich bin mir nicht
  sicher ob wir den s3 encryption Proxy importieren sollen oder die Funktionalitäten einfach bei uns aufbauen. Alles
  das ist erstmal nur eine grobe Wunschliste. Du sollst evaluieren was davon erfüllbar ist und wie genau wir das
  umsetzen sollten um uns maximal resilient und sicher zu machen." **Nothing of it is decided or specified beyond what
  the specification holds already** (read on 2026-11-22): the client-side encryption of stored content in the format
  of the encryption proxy, imported as a module, decided by him on 2026-11-06 and not built
  ([R-STOR-048](../spec/storage.md#r-stor-048--client-side-encryption-of-stored-content), with eleven points open
  before it is built, among them the seam to Quader's own multipart uploads, the server-side copies, the object names
  that confirm a known file, the scope against the deduplication across repositories, and key custody and rotation);
  the presigned download redirect as a switch per repository, specified and of the second priority
  ([R-STOR-014](../spec/storage.md#r-stor-014--delivery-by-signed-links-of-the-object-store-the-installations-switch)); the object store's own
  encryption (R-STOR-032). **What his list adds or reopens:** import or build (he doubts his decision of 2026-11-06);
  the delivery by signed links as an administrator's setting; several connections with encryption per connection; a
  Bastion with a read-only account of the bucket; a rotation of the installation's key; a key per project and per
  repository. **In work since 08:30: four researches,** each read-only and each with its findings in a file: the
  encryption proxy's fifth version (its stored format and what it withstands, its keys and their rotation, its Go
  packages and what an import brings into Quader, its quality, and the facts for import, rebuild or a format of
  Quader's own); Quader's store as it is (object names, the deduplication and who references a blob, the one
  connection the code assumes, the write and read paths, integrity today, what of the redirect is built, backup and
  restore, the tests' cost); the download path of every format with what a redirect would skip, the Bastion (what it
  serves, how content reaches it, what a direct read would need and bypass) and the keys Quader manages already; and
  the state of the art (clients and redirects for every format and the formats to come, formats of client-side
  encryption and their attacks, key hierarchies and rotation, several object stores, integrity). **The third
  ended at 08:42** (read in the code and the specification, nothing run). *The download path:* no download is
  redirected today; the store can presign and the configuration has the keys, with no caller, and a repository's
  `storage.presignedDownloads` is accepted and has no effect. A redirect would leave the pacing and the limit of
  parallel downloads without a transfer to act on, fill the byte windows only if the size is charged when the
  link is issued, skip the verification of the bytes, outlive a revocation or a malware finding by the link's
  life, and through a Bastion hand the caller the main store's address. An installation-wide switch has a
  precedent (the malware scan's "only checked artifacts": one row, an API, a settings page, at every replica
  within a second), a repository's setting too. *The Bastion:* **his wish of a read-only account reverses a
  decision of the specification** (Q-BAS-10, "never the main installation's object store", rejected then because
  it gives an instance in the DMZ read access to every blob; R-BAS-001 forbids a Bastion pod any request to the
  main bucket), and keys per project and repository are what would answer that decision's reason. **A Bastion
  has no scope today:** it serves whatever the caller's own token may read, and the main installation does not
  know which repositories a Bastion serves, so "only the keys it needs" needs a scope per registration, a new
  concept. It has no database, learns a blob's digest from an answer's entity tag, and keeps its cache in the
  clear with signed entries. *The keys:* the key ring of stored secrets (AES-256-GCM, each value bound to its
  table, column and row) seals three columns, is held by `api`, `packages`, `worker` and the restore job, and is
  not read again without a restart: a rotation is two rollouts, the re-encrypt task and the old key's removal.
  It can derive a key family per purpose. A key per project kept as a sealed column would fit it unchanged (the
  research's inference); a hierarchy, a reload and a way to hand keys to a Bastion do not exist. The backup's
  own encryption covers the database files alone and is not built. **The second ended at 08:47** (the store, read
  in the code at the commit of that morning, nothing run). *One bucket by construction:* one block of the
  configuration, one client, one circuit breaker, one readiness check, metrics without a label for the store,
  and the specification says so too (R-STOR-030; its assumption A4 allows a one-way replication to a passive
  bucket); the research lists seventeen places that would have to learn of a second connection. A blob's row
  holds no location, no key identifier and no wrapped key, and its content state is one value per blob.
  *References:* foreign keys and no counter; which repositories reference a blob is cheap, which blobs a
  repository references is cheap for live items and the remote cache alone; and a reference is added without
  any write to the store (a deduplication hit, a link, an image mounted across repositories, a deploy by
  checksum), which is where a key per repository meets the deduplication. *The seam:* the store's interface
  (thirteen methods) carries no object metadata anywhere, which is where the proxy's format keeps the wrapped
  data key. **Encrypting the content as the store stands would mark every blob corrupt:** the commit, the
  download, the recheck, the verification, the backup and the restore compare the stored object's size with
  the plain size of the row, and the backup's streamed copy, the restore and the deep verification hash the
  stored bytes against the plain digest (the specification's open point 8 names the listing's sizes alone).
  *A rotation meets versioning and backups:* noncurrent versions and the objects of a backup's pool keep the old
  wrap, so a retired key has to outlive both. *A second connection meets* the upload sessions (one multipart
  upload and the staging and its copy in one bucket), the garbage collection (a delete that reaches one store
  of two leaves an orphan nothing sweeps, and one store's open circuit stops the run) and the one content state
  per blob. *A Bastion at the bucket:* the object names carry no project and no repository, so no policy of the
  store can narrow a read-only account to some repositories; the start check writes a probe, which such an
  account cannot; nothing holds a blob back from the garbage collection for a reader outside. The malware
  scanner reads through Quader and would need no key; the backup and a Bastion at the bucket would. *Tests:*
  every wish can be shown at the unit tier with the in-memory store (two stores, its fault hook) and one
  integration case each; none is specific to a format, so none needs an end-to-end leg. **Findings on the way
  are in section 8** (the scrub and the orphan sweep are not built). **The first ended at 08:48** (the encryption
  proxy at its release 5.4.2, read in its code and documents; the attack cases, a part-by-part upload and the
  dependency resolution run in throwaway modules, nothing against a live object store). **What R-STOR-048 says of
  it is outdated in four points:** its module path carries the major version since 5.3.0, so a release can be
  required by its tag; the owner's sketch is a released package of the proxy since 5.4.0, with the interface the
  specification describes; the versions of the object store's client library it needs are newer than named; and
  the format accepts a re-wrap that rewrites only the metadata. *The format:* AES-256-GCM over segments of 64 KiB,
  each with a random nonce, each authenticated together with the format's identifier, the exact object key and
  its index; a sealed trailer with the plain length and a CRC32C; one data key per object, wrapped once, in four
  metadata keys of the object. *It stops* (each run against the codec): truncation, reordering, a dropped
  segment, a segment of another object, a whole object copied to another key, a swapped metadata block. *It does
  not stop:* an older genuine version put back under the same key (its documented gap), and it does not bind
  the bucket; a commitment to the key is addressed nowhere. *Keys:* a plain AES key is the one provider that
  encrypts (a key service is decided there and not built); the key is read once at the start; no job re-wraps
  or re-encrypts, by its decisions; exactly one wrap per object, and its rules forbid a fifth metadata key; no
  hierarchy, no key per bucket or request. In its Go packages the key is an interface a caller implements.
  *As a proxy:* one backend, no replication, the state of a multipart upload in one process's memory, copies
  refused, no link of the backend handed out. *As Go packages:* encrypting, decrypting, a ranged read and the
  authenticated length work without its HTTP layer, and so does an upload driven part by part that another
  replica continues (run). *What it promises:* stored data stays readable within a major version and may break
  at the next without a migration; the Go interface is explicitly no promise, and one signature changed in a
  minor release. *What an import costs:* the codec's packages use the standard library alone (about 90 KB of the
  binary); the released glue package raises eighteen modules of the object store's client library in Quader and
  adds one of a version below 1 (about 380 KB); the same licence family and the same Go version. *Its size and
  quality:* the format's core is 1,497 lines of 18,051; 93.3 % coverage in its pipeline; known-answer vectors for
  short objects as constants in Go; no fuzz target; reading each other's objects is tested in both directions
  on each of its pull requests. **The two facts that constrain the decision most:** the binding to the object
  key rules out Quader's finalize by a server-side copy and the backup's copies unless they re-encrypt, whether
  the format is imported or rebuilt to the same specification; and one wrap per object conflicts with keys per
  project and repository on deduplicated blobs unless the hierarchy lives in Quader's database and not on the
  object. A middle way exists: import the codec alone and bind to a name of Quader's choosing, which the proxy
  then cannot read. **Not verified:** anything against a live object store; whether the upload library resends
  identical bytes for a retried part; whether a missing key commitment can be used in Quader's setting. **The
  fourth ended at 08:52** (the state of the art, from the tools' source, official documents and anonymous requests
  to public registries; each fact marked with its kind of source, seventeen points not verified). *Clients and
  signed links:* the large public registries redirect a blob's `GET` and answer `HEAD` themselves; nearly every
  client drops its credential on a redirect to another host, but the image clients written in Go keep it for a
  subdomain of the registry's host, and an object store refuses a signed link that arrives with a credential, so
  the links' host must not be a subdomain of Quader's own; a link signed for `GET` fails on `HEAD`; a link lives
  no longer than the credential that signed it and cannot be revoked alone; the object store's log names the
  signer and not who used the link. Most formats' clients verify a digest after the download; a plain download
  tool does not. *Client-side encryption:* the object store vendor's own client treats ranged reads as
  unauthenticated and buffers before it releases plain bytes, which a segmented format avoids; **a commitment to
  the key matters wherever somebody who can write the object's metadata can swap the wrapped key**; one reused
  nonce loses the authentication, so a retried part must never reuse nonces with other bytes; a wrapped key in
  the object's metadata can be changed only by copying the object (atomic to 5 GB, in parts above). *Key
  hierarchies:* every key service read rotates a key-encryption key by adding a version and re-wrapping the level
  below, none re-encrypts content. **Bare SHA-256 object names let anyone who may list the bucket confirm that a
  known file is stored,** and no policy of an object store can scope a bucket whose names carry no tenant.
  *Several stores:* **no product that was read replicates one store across providers** (the reference registry has
  one backend, a group of blob stores elsewhere only spreads writes); the providers differ in conditional
  writes, checksum defaults and object lock, and none offers a SHA-256 over a whole multipart object. Recorded
  outages of object stores lasted from one hour to more than seven. *Integrity:* an object store's checksums
  cover transport and storage, not who wrote the bytes; versioning and object lock are what stop a credential
  from replacing an object. **Not verified, among others:** the owner's own provider and five other European
  ones; two rules of signed links that rest on secondary reports; the digest checks of several clients.
  **The working paper is with him since 09:02** as an interactive page he answers in (standing rules; its storage
  was written, read and cleared once by the development agent, the page's own writes from a browser were not
  exercised): his eight wishes as read, each with whether it is satisfiable and at which price; one question to
  him (which stores his own installation shall get); what exists in Quader and in the encryption proxy, the
  clients' behaviour on a redirect, recorded outages, and what is not verified; fourteen decisions with options
  and the development agent's favourite each; a sketch with seven cases today and after the design (a provider's
  outage, a changed object, a rotation, a lost database, a lost key ring, a Bastion taken over, encryption
  switched on later); the stages and what the tests cost. **The verdict in it:** all six wishes can be met; two
  reach their goal more tightly by another way than the one he sketched (the keys per project and repository, the
  account for the Bastion), and the Bastion at the bucket reverses his decision Q-BAS-10. **The development
  agent's advice in it:** build the format in Quader after the model of the proxy's segment chain, bound to the
  content and not to the object name, so that copies in the store, the reconciliation between stores and the
  backup need no re-encryption; three levels of keys (the existing key ring as the root, a storage key per
  connection, a data key per blob), so that a rotation of the root touches no object; several connections first,
  with a row per copy, reads that fall over, and one reconciliation job that is also how encryption is switched
  on, a provider changed and a damaged copy replaced; signed links as one switch of the installation, only where
  the format's client verifies a digest itself and no pace limit applies; no account and no key for the Bastion,
  and if it shall relieve the main installation, a link and the data key per released blob. **The favourites,
  so that a later reading can tell where he deviated,** each option (a) but decision 12, which is (b) since 09:53, and decision 2, which is (e) since 10:07 (below); the
  fifteenth decision, added at 10:07, is (a): (1) a format of Quader's own with test
  vectors, fuzzing and an offline tool, against importing the proxy's package, its codec alone, or extending the
  proxy first; (2) at first the wrapped data key in the database and in the object's metadata, since 10:07 once, at the
  object alone (e); (3) no level of keys per project or repository, with room for one later, against
  wraps per referencing repository or objects per project; (4) the existing key ring as the root and its existing
  rotation; (5) on an encrypted connection an object's name is a keyed MAC of the digest; (6) a new connection is
  not encrypted unless told; (7) a connection's properties are fixed once it holds data, and changing them is a
  new connection that the reconciliation fills; (8) an upload is done when one connection holds it, a minimum of
  copies as a detail; (9) connections are kept in the chart, the admin area shows them; (10) a scrub that reads
  every copy within a period and replaces a damaged one from another connection; (11) the switch for signed
  links at the installation with its conditions, a repository can opt out; (12) at first the Bastion stays away from the
  main store, and since his decision that it serves images the handover per blob, a link and the data key (b);
  (13) the Bastion's cache is encrypted in the same format once it has a key; (14) three stages, the connections
  and the scrub first, encryption second, the links beside the first. **Nothing is specified or built before his
  answers are read back** ("bitte auslesen").
  **He has begun to answer:** decision 1 stands as (a) since 09:21 (read at 09:50; the page's own writes work).
  **His two questions on reading it, at 09:46, and what followed.** (1) Why a Bastion serves neither Debian nor
  images, with his decision that it shall serve everything (the entry "Decided by the owner on 2026-11-22"
  below). With images served, the first fill of a Bastion is layers of gigabytes through the main installation,
  so **the favourite of decision 12 changed from (a) to (b)**, the handover per blob, built after the Bastion
  serves every format on today's path. (2) "Können wir vielleicht s3 Annotations nutzen anstatt bei einer
  neu-verschlüsselung die Daten im s3 zu kopieren?" **Read in the provider's documentation the same hour** (the
  feature exists since June 2026 and was not known to the development agent): named payloads of up to 1 MiB of
  text at an object version, up to 1,000 of them, written, read and deleted by requests of their own without
  changing the object or its entity tag; not settable with the upload, only after it; not versioned, an
  overwrite or a delete is final; **not creatable or changeable on an object under object lock**, also not with
  the bypass right; copied by a plain copy, not by a copy in parts; found at that one provider only (one search
  for the S3-compatible stores, none found, not checked per store). **Answered:** they fit exactly the case of
  changing the wrap at the object without copying it, but not as the base: one provider, no object lock, a
  window after the upload without the wrap, no history. And in the favoured design the copy does not arise: a
  rotation of the root re-seals one row per connection, a replaced storage key stays readable for what it
  wrapped, and new data keys mean new bytes whatever carries the wrap. Because the database holds the
  authoritative wrap, the copy at the object can become a capability of a connection later. **The paper was
  published again at 09:53** with decision 12's new favourite, a fourth option of decision 2 (the copy at the
  object in an annotation) and the facts about annotations.
  **His objection to decision 2, at 10:03, and the correction it caused:** "Ich verstehe nicht wieso du dafür bist,
  einen Schlüssel zweimal abzulegen. Zweimal ablegen heißt doch es könnte auseinander laufen ..." **He is right, and
  the first favourite was wrongly argued.** Checked again: of the three gains the paper gave the copy in the
  database, none holds. That a rotation of the root touches no object comes from the storage key between the
  root and the data key, not from a second place; no request is saved, because the object store sends an
  object's metadata with every answer, also of a range; and whoever can only write to the bucket can slip in no
  wrap anyway, having no storage key to make one and a foreign one failing at its binding to the digest. And the
  two places do drift, with two functions Quader has (read in the code): after the database is restored to an
  earlier point in time, its wrap does not fit a blob that was deleted and uploaded again since (the restore
  accepts an object by its size), and `restore-versions` can cause the same; the blob would be whole and
  unreadable. The option that answers him was missing from the paper, because "at the object" had been taken as
  "directly under the root". **The favourite of decision 2 is now a new option (e):** the wrapped data key lies
  once, in the object's metadata, written in the same request as the bytes, bound to the digest, under the
  connection's storage key; the database holds no key of a blob, only state, size and the storage key's
  identifier. Its prices: the store's interface gains metadata; a handover to a Bastion costs the main
  installation one small request for the wrap; that his provider accepts new metadata on a copy is not verified.
  **A fifteenth decision follows from it,** where the few storage keys lie: (a) once, in the database, a sealed row
  per connection and generation, re-sealed by the existing rotation task, with a command that prints them sealed
  for safekeeping beside the key ring (the favourite: what changes its sealing at every rotation lies once and
  where the rotation is built; the price is that the bucket and the key ring alone no longer read a blob, the
  sealed storage keys of any backup of the database are needed too); (b) also as a small object in the
  connection's bucket; (c) only there. **The rule behind both:** what comes into being with every upload lies with
  the bytes and only there; the few keys whose sealing changes lie where the rotation lives. **Published again
  at 10:07.**
  **His question at 10:36, which keys there shall be:** answered with the three of the favoured design in his terms
  (the root is his instance KEK and the existing key ring; a storage key is a KEK per encrypted connection, drawn
  by Quader, sealed under the root in one row, never handled by an operator; a data key per stored object, wrapped
  under the storage key at the object) and the name key of decision 5, and with why the level between cuts by
  connection and not by project: an object belongs to exactly one connection and, by the deduplication, to any
  number of projects. The paper has the table of the keys since 10:37; the wording of decision 4 no longer
  speaks of a derived key family, which a sealed row does not need.
  **Answered by him on 2026-11-22 between 09:21 and 10:48, read at 10:48 on his word** "Ich habe die Fragen
  beantwortet, lese sie ein". **All fifteen decisions as the development agent's favourite at the time he
  answered:** (1) a format of Quader's own; (2) the wrapped data key once, at the object, the option his own
  objection brought; (3) no level of keys per project or repository; (4) the existing key ring is the root;
  (5) keyed names on an encrypted connection, with the note "Ggf. optional?"; (6) a new connection is not
  encrypted unless told; (7) a connection's properties are fixed, changing them is a new connection; (8) an
  upload is done on one connection; (9) connections in the chart; (10) the scrub that reads and repairs; (11)
  signed links as a switch of the installation with its conditions; (12) (b), the handover per blob to a
  Bastion, which lifts his Q-BAS-10 for a link and a data key per released blob and for nothing else; (13) the
  Bastion's cache encrypted; (14) three stages; (15) the storage keys once, sealed in the database. **His note at
  decision 5, read and decided by the development agent:** the keyed names become a detail of an encrypted
  connection, on by default and fixed once the connection holds data, as every property of a connection is
  (decision 7); it costs next to nothing, because the plain name exists for the connections that are not
  encrypted anyway, and on a connection that is not encrypted it is not offered, since the content there is
  readable in any case. **Not answered:** the eight readings; every reading is covered by a decision he took. **The question which stores
  his own installation shall get, answered in the chat at 10:57: (c),** one connection for now and the second
  later. What follows: his own provider is the first to be measured against, and it was the one not researched,
  so a research of its object storage reads its documentation since 10:58 (what it supports of copies with new
  metadata, checksums, conditional writes, versioning and object lock, signed links; nothing is called at the
  provider). The first stage stays first also for one connection: switching encryption on later is a second
  entry of the same bucket under another prefix that the reconciliation fills (decision 7). **No measurement against his
  provider today** (his word at 11:00: "Stackit kann ich dir im Moment nicht bieten für einen test. Das wird heute
  nichts. Machen wir ggf. später."); the research reads the provider's documentation, and what needs a real
  bucket of that provider stays "not verified" in every package until he offers one. **The research of his provider's
  object storage ended at 11:10** (its documentation and, marked apart, the documentation of the software behind
  it, which the provider's own API description names; the version it runs is not stated; nothing was called).
  *Documented by the provider:* two regions; copies and copies of parts; tagging; signed links for `GET`; static
  keys without an expiry unless one is set, valid for every bucket of a project unless a bucket's policy narrows
  them; versioning; object lock, to be chosen when a bucket is made; encryption at rest; read-after-write for new
  objects, while overwrites and deletes are eventually consistent; limits per project of 2,500 requests a second,
  500 at once and 500 MB/s. *Silent in its documentation:* every checksum header, conditional writes, a copy with
  new metadata, ranges and the response headers of a signed link, a link's longest life, the size of metadata.
  *By the software's documentation, not confirmed for the provider:* a copy with new metadata and metadata up to
  24 KiB are supported, the response headers of a signed link too; **conditional writes are not** (refused or
  accepted and ignored, the latest write wins), and the checksum trailer of the newer client library defaults is
  rejected. *Incidents:* an outage of its object storage of about 17.5 hours in July 2026, the longest of the
  whole research, and five more incidents since 2023. **What follows for the design:** no guarantee of the
  commit, the reconciliation or the garbage collection may rest on a conditional write; the start check of a
  connection establishes what it supports; and the wrap at the object (decision 2) is right for a second reason,
  since under an eventually consistent overwrite the object a read gets always carries its own wrap. Both
  planners at work were told at 11:10, with the findings' file. **A wrong assumption of the development agent,
  found at 12:01 in his deployment repository and on his cluster:** his own installation does not keep its blobs at
  that provider. It claims a bucket from the object store inside his cluster (a Ceph object gateway, Ceph 19.2.3
  as read from the cluster, with the credentials in a Secret the bucket's provisioner writes and the cluster's
  own CA). The provider he named is the one he cannot offer for a test today, so it is what he has in mind
  beside it, not what runs. What follows: the in-cluster gateway is the store the first connection of his
  installation meets, and it was not researched either; the start check per connection establishes on his
  cluster what it supports as soon as the release with package A runs there, without anybody calling it by
  hand; its address is inside the cluster, so signed links need a public address there before a client outside
  can follow one; his chart follows every new release on its own, so the values of his installation are
  rewritten to the list of connections in the step the release with package A and the chart is published (the
  chart refuses the old values at rendering, which leaves the running pods in place until then). **The reading of
  that gateway's documentation and source ended at 13:22** (the 19.2 line at its newest tag, its release notes and
  tracker, the operator's documentation; nothing run against a gateway; his cluster runs 19.2.3). *Supported, by
  the documentation or the source:* ranged reads, copies and copies of parts, a copy with replaced metadata (in
  the source, not in the documentation; refused for a source above 5 GiB and for one under the store's own
  encryption), tagging, versioning, object lock, signed links with the response headers a link asks for, and
  read-after-write with listing for a single zone. *Not there:* **no additional checksum of an upload in the 19.2
  line** (it comes with the next major line), so only `Content-MD5` and a signed SHA-256 of the payload are
  verified; conditional writes exist but are ignored at the completion of a multipart upload and, in buckets with
  versioning, were ignored before 19.2.7 and are not atomic. *Two defects of the line that concern him:* a copy of
  an object onto itself lost data in 19.2.1 (repaired in 19.2.2, so his 19.2.3 has the repair), and **signed links
  for an upload had a security defect until 19.2.6** (whoever held one could add headers and read every object
  its signer may read; repaired in 19.2.6, whose repair broke such links with a content type until 19.2.7).
  Quader signs links for downloads alone, and whether a download link was open to the same is not stated in
  what was read; **the development agent's advice for his list: the gateway of his cluster is at 19.2.7 or newer
  before signed links are switched on there** (number 25 of the list above). *The provisioner:* the Secret of a claimed
  bucket holds the two keys, its user is limited to that bucket, and a read-only user needs a user of its own
  with a policy. *Nothing about annotations.* The builder of the first stage's package A was told what bears on
  the start check (no additional checksum to rely on, no conditional write, the copy's limits). **In work since 10:51: two planners in parallel,** stage 1 (several connections, the reconciliation,
  the scrub, with the contracts the other stages build on) and stage 3 (signed links), each with a block of
  identifiers of its own. **Queued:** the planner of stage 2 (encryption, the keys, their rotation, the offline
  tool, the Bastion's cache), once stage 1's specification is on `main`; the handover to a Bastion after stage 2
  and after the Bastion serves every format (**its planner is at work since 14:22**, run record).
  **Stage 2, encryption, is specified on `main` since 13:02** (its planner worked from 12:06 to 12:49; one commit,
  rebased twice by the development agent, with the Bastion's decision blocks merged by hand; on the rebased
  commit the whole of the chart tests, the full unit run, the evidence regenerated without a difference, links,
  check and the count test exit 0, the wording scan clean; its worktree and branch are removed): R-STOR-048
  rewritten in place and raised to the first priority, R-STOR-075 to R-STOR-091, R-BAK-031, R-BAS-061,
  Q-STOR-300 to Q-STOR-320, Q-BAK-65 and 66, Q-BAS-210 and 211, Q-HA-415, Q-API-405, ADR-244 and ADR-245, the
  gaps H-1300 to H-1318 on three new security pages (the format and what it withstands; the keys and who holds
  them; what stays visible to the provider). **What it says.** *The switch:* `encrypted: true` on a connection,
  off by default, with the detail of keyed names, on by default; both fixed once the connection holds data; an
  operator handles no key. *The format:* AES-256-GCM over segments of 64 KiB, each with its stored nonce and
  authenticated with the format, its index and a mark of the last one; a final segment carries the plain length
  and the SHA-256; nothing is bound to the object's name, so the finalize's copy and the backup's pool never
  decrypt. The stored size is the plain size plus 28 bytes per segment plus 68, with an exact inverse; the
  plain limit is 4 TiB. *The record at the object:* four metadata keys written in the same request as the bytes:
  the format, the storage key's identifier, the wrap, and a commitment to the data key; the wrap seals the data
  key with the digest and the size, which is what lets the offline tool work under keyed names without a
  database. *Reads:* every failure of the record or the size comes before the first byte; a ranged read is one
  request and hands out no unauthenticated byte. *Writes:* nonces come from a seed per write, so a retried part
  re-sends the same bytes without ciphertext being kept; an upload session seals its data key in its row; the
  finalize copies with replaced metadata, and streams through the replica where the start check found that a
  copy does not replace it. *Keys:* storage keys and name keys are sealed rows registered with the key ring, so
  its canary, its compatibility check and its re-encrypt task cover them with nothing new built; a rotation of
  the root re-seals rows and touches no object; commands export and import the sealed rows; **a record that
  names an unknown storage key is not damage,** so the scrub never overwrites what an import could still open,
  and raises a critical alert instead. *Between connections:* every copy into an encrypted connection gets a new
  data key. *Backup:* the pool keeps the ciphertext with its record, so the backup's pod needs no key; a deep
  verification checks the structure without the key ring and every segment with it. *The Bastion's cache*
  encrypts everything in its store under one key file of its chart, off by default. *The offline tool* is a
  command of the binary that lists, fetches and verifies blobs without a database. *The journal, the probes and
  the marker* stay plain. **The known answers in the specification** were computed by an implementation the
  planner wrote from the specification's text alone; the builder checks the vector files with a second one.
  **Three questions of the planner are the owner's,** numbers 22 to 24 of the list above; the first of them
  takes back a side effect the development agent had told him (a provider's storage key deleted on leaving).
  **Build plan, six packages** (a new developer page of the stored content's encryption): 1 the format, its
  vectors, fuzz targets and a nightly fuzz job, which can start at once (**landed at 13:59**); 2 the keys, after
  the first stage's package A; 5 the Bastion's cache, after 1 (**in work since 14:00**); 3 the store's writes and reads, which makes `encrypted: true`
  accepted, after 1, 2 and the first stage's A and B; 4 the reconciliation, scrub, sweep, backup and restore
  under encryption, after 3 and the first stage's B to E; 6 the offline tool, the chart, the API's fields and
  the manual. **Its tests cost** (estimates): about 2.9 s of unit tests and 0.5 s of the chart's, about 10 s in
  the integration area `blob` and 8 s in `backup`, 3 s in the conformance job, no end-to-end case, and about 27
  minutes a night for five fuzz targets in a new nightly job (no nightly fuzz job exists today: the two
  formats' fuzz legs run on a closing dispatch alone). **Not verified:** anything against an object store,
  among them whether the object gateway of his cluster keeps metadata and replaces it on a copy; the speed of
  the encryption on Quader's hardware; the first stage's seams, which exist in the specification alone until
  its package A lands.
  **Stage 1, several connections, is specified on `main` since 11:39** (its planner worked from 10:51 to 11:38; two
  commits, rebased by the development agent over the third stage's specification and the Bastion's package A:
  the decisions' page, the security index and the count test merged by hand with both sides kept, the evidence
  regenerated; links, check, count test and build exit 0, the wording scan clean; its worktree and branch are
  removed): R-STOR-050 to R-STOR-063, the orphan sweep and the scrub (R-STOR-026, R-STOR-027) rewritten in place,
  R-HA-072, R-BAK-030, R-UI-196, R-UI-197, Q-STOR-220 to Q-STOR-249, Q-HA-390 to 392, Q-BAK-62 to 64, Q-UI-930 to
  932, Q-API-385 and 386, ADR-239, the gaps H-1251 to H-1257 on a new security page. **What it says.**
  *Configuration:* `storage.connections` is a list of one to eight connections and the single block `storage.s3`
  goes; an element is flat, its identifier defaults to `default` and never changes; the list's order is the
  order of writes and of reads; a provisioner's Secret reaches a connection through files; `encrypted: true` is
  refused with a sentence that encryption is not built. A connection's bucket, prefix and `encrypted` are fixed
  once it holds a copy, and a marker object says which installation and connection a location belongs to.
  *Copies:* a row per copy of a blob on a connection (present, missing, corrupt, deleting, and when it was last
  verified), with the object's name, the stored size and the storage key's identifier for the second stage; a
  blob's content state follows from its copies, so one store's 404 marks that copy and not the blob. *Writes:*
  an upload is done when the minimum of copies holds it (one by default) and falls over to the next connection
  within the request; an upload session stays on its connection and answers 503 while that is unreachable.
  *Reads:* the present copies in order, falling over before the first byte only. *The reconciliation:* one
  exclusive job of the worker, from a queue and from a pass over all blobs; a server-side copy within an
  endpoint, a stream that is hashed across providers; it fills a new connection and drains an old one, and a
  drain deletes a copy only beside a copy verified within its period. *Collection and sweep:* per copy; a delete
  that could not be sent is handed to that connection's orphan sweep, so one provider's outage holds neither the
  run nor an uploader; the sweep is built with this stage and adopts objects of live blobs that lack a row. *The
  scrub:* every copy read whole once in the connection's period (720 hours by default), throttled per
  connection, unverified copies first; the reconciliation repairs what it finds; the tests that claimed the
  scrub's requirement for other parts are renamed, so the evidence becomes true. *The start check* runs per
  connection and establishes what the store supports: a missing required ability refuses the connection, an
  optional one degrades by a stated rule; **no guarantee rests on a conditional write,** and read-after-write is
  required for new objects only (his provider's documentation, read that morning). *Readiness:* a replica is
  ready once one connection that is not draining has passed and none has a configuration error; the metrics
  keep their names and gain a label; "every connection down" is critical, "one down" a warning. *Backup and
  restore:* a backup reads one copy per blob, a restore fills the first connection and the reconciliation the
  rest. *The journal* goes to every connection through the guarded client, which repairs the finding about the
  plain client. *The admin area:* a fourth tab "Storage" of the system status, read-only, and a line per
  connection on the dashboard. **The contracts the other stages build on** (R-STOR-062): object metadata on the
  store's interface, the copy row's name, key identifier and stored size, every size check on the stored size,
  and a function that names the copies that can serve now. **Three questions of the planner are the owner's,**
  numbers 19 to 21 of the list above, each specified with its recommendation. **Build plan, six packages** (the
  developer page of the blob store): A the connections and copies, alone first; then B sessions, collection and
  sweep, C the reconciliation, D the scrub and the operator commands, E the chart, backup, restore and the
  journal; F the API, the screens and the manual after C and D. **In work since 11:41: package A.** **Decided
  by the development agent at the start:** A removes the configuration key the chart renders, so the chart's part
  of E is built in parallel from the specified shape and lands in one push with A; A keeps every other package's
  code compiling and behaving as today for one connection. **Its tests cost** (estimates): about 4 s of Go unit
  tests and 1.5 s of UI tests, 1 s of the chart's, about 20 s in the integration area `blob` (5 s of it the one
  case against two real buckets), 8 s in `backup`, 2 s in the conformance job, no end-to-end case, nothing that
  grows with the formats. **Not verified:** anything against an object store, his provider among them (whether
  its queued delete can reach a later write of the same name is gap H-1255).
  **Stage 3, signed links, is specified on `main` since 11:28** (its planner worked from 10:51 to 11:26; two
  commits, rebased by the development agent over the Bastion's package A, with the developer page of the formats
  and the count test merged by hand and the evidence regenerated; links, check, count test and build exit 0, the
  wording scan clean; its worktree and branch are removed): R-STOR-014 rewritten in place and raised to the first
  priority, R-STOR-070 to R-STOR-074, R-LIM-040, R-UI-200, R-UI-201, Q-STOR-280 to Q-STOR-294, Q-LIM-97 and 98,
  Q-UI-950 to 955, Q-API-400, Q-ARCH-570, ADR-242 and ADR-243, the gaps H-1275 to H-1283 on a new security page.
  **What it says:** one switch of the installation, off by default, kept in a row as the malware scan's is, at
  every replica within two seconds; its details are the minimum size (1 MiB), a link's life (60 s, from 10 s to
  an hour, never beyond the signing credential's) and a public address per connection; a repository opts out,
  and through a virtual repository both the addressed one and the member that holds the file must allow links;
  the stored and ignored `presignedDownloads` of a repository is refused from now on. **The serve point decides
  in a fixed order, and every condition that fails streams as today:** the route declares that its clients check
  a digest and the method is `GET`; not through a Bastion, not a scanner's read; the switch is on and nobody
  opted out; malware, vulnerability and signature checks have passed on a settled verdict; a conditional request
  and every range are answered by Quader; the file reaches the minimum size; the caller has no speed limit;
  the download limits pass; a connection that is not encrypted holds a sound copy, is not failing and passed its
  link check; the signing credential lives five seconds more. `HEAD` and the generic download path are never a
  link. **The answer** is a 307 that is never cached, to a link signed for `GET`, `https` only, whose host is
  none of Quader's hosts and below none of them, checked when saved and at every start. **The contradiction
  between the two requirements about content headers is settled:** the object store can be asked for the content
  type, the attachment's name and no caching, and the three headers it cannot carry guard Quader's origin, which
  a link's host never is (gap H-1279). **A link check per connection** at every start and when the switch is
  turned on establishes whether the store honours those overrides, which answers his provider's silence about
  them: a connection that fails it streams. **Everything is counted when the link is issued** (a request, the
  file's size, a download, the audit event with its way of delivery, a field of the request log); the lease of
  parallel downloads ends with the 307. **The format declares per route whether its clients check a digest,** a
  field of the route held by a twentieth check of the kit after the pattern of the nineteenth; a format to come
  streams until it declares with evidence. Declared with evidence read in the clients' source: the registry's
  blob, npm's tarball, Ansible Galaxy's collection artifact; not declared: NuGet and Debian (neither client was
  read), a manifest by digest, the generic path, the ClamAV definitions. **The admin page** is a section
  "Download links" of the storage settings, with each connection's reason where no link can be issued, the
  profiles a speed limit keeps away, and a test link the administrator opens from a client's machine (Quader
  sends no request itself, because the question is whether the clients' network reaches the store). **One
  question of the planner is the owner's,** number 18 of the list above. **A second it put to him is decided by
  the development agent:** Debian declares once the builder of the second package has shown, with one case of
  altered bytes in the apt clients' end-to-end set (about 15 s, in its own leg), that apt refuses them; it is a
  proof with the real tool, not a behaviour he would choose. **Build plan, three packages** (the developer page
  of the download links): 1 the contract's field and the kit's check; 2 the serve point, the store and the
  limits, the whole feature behind the API; 3 the API, the settings page and the repository's section. **Package 1
  is landed since 12:00 (run record).** Package 2 owns directories the Bastion's package B and the first stage's builders
  work in, so it starts when B has landed and the first stage's build plan is known. **Its tests cost**
  (estimates): about 3 s of unit tests, 10 to 20 ms per format in the kit, about 2 s of UI tests, about 3 s in
  the integration area `blob`, one end-to-end case per format that declares (30 s `oci`, 20 s `npm`, 15 s
  `ansible`), nothing elsewhere; with 25 formats a case of 15 to 30 s in the own leg of each format that
  declares. **Not verified:** anything against his provider's bucket; whether the end-to-end client pods reach
  the development store; the digest checks of Podman, CRI-O, pnpm, Yarn, apt and NuGet.

**A topic he opened on 2026-11-21, answered in its working paper the same morning; its three stages are built, and its
open questions are in the list above**

- **Signatures on artifacts of every format** (his words): "Quader muss in Zukunft auch das feststellen, auslesen und
  verifizieren von Signaturen auf (wenn möglich) allen Artefaktarten unterstützen. Dabei muss es zwischen öffentlich
  gültigen oder intern gültigen Signaturen unterscheiden können. Ein Admin muss also gültige unternehmens CAs
  einstellen können, wie zum Beispiel eine CA der internen Gitlab-CI. Er muss einstellen können, ob Projekt-Owner
  weitere CAs für ihr Projekt konfigurieren können (default nein). Ich bin mir aber nicht sicher ob das alles Sinn
  macht, ob man wirklich alles signieren kann. Ob es ein Standard für alle Registries gibt. Das muss bei zukünftigen
  Registries automatisch mit berücksichtigt werden. Ich kenne zum Beispiel von Dockerhub, dass dort auch signaturen
  von Images hochgeladen werden und als seperate tags auftauchen, so wollen wir das aber nicht darstellen. Natürlich
  erfüllen wir den technsichen Standard wie die Applikationen es erwarten, von der Darstellung her, soll eine
  Signatur aber nicht alleine stehen sondern klar einem Image zugeordnet werden. Das ist hier natürlich nur das
  Beispiel für OCI, andere Registries haben wahrscheinlich andere Anforderungen. Ziel, signierte Artefakte aller Art
  werden von uns erkannt, im Frontend mit einem Icon besonders gekennzeichnet und auch die Metadaten wir Zeitpunkt
  und Ausstellen (alle anderen Infos ebenso) werden durch uns dargestellt. Kommt eine Software über die API vorbei,
  erhält sie die signierungen wie ihr Interface es erwartet. Quader ist ebenso in der Lage Artefakte zu signieren,
  dafür muss aber erst ein Zertifikat in Quader über Helm oder das Frontend gepflegt werden. Hier müssen wir uns aber
  noch über den Umfang und Ziele unterhalten, ich erwarte, dass du mich hier proaktiv berätst." **Nothing is
  decided or specified.** Both researches ended on 2026-11-21 (what Quader does today with signatures per format,
  read in the code; the signing schemes of the six formats and of the formats likely to follow, with sources), and
  **the working paper is with him since 09:57** as an interactive page he answers in (standing rules): his words as
  read in eight readings and one question to him (which way to a signature his CI has or shall have: a signing
  service of its own that issues a certificate per job, a code-signing certificate of the company's CA, an OpenPGP
  key, or nothing yet), the facts per format, and fourteen decisions with options and the development agent's
  favourite each. **What the researches found:** there is no one standard but four families of trust (a
  certificate chain, an OpenPGP key, a single public key, an identity of a signing service with a log) and a group
  of formats with no scheme at all, so "company CAs" covers two of the four and the setting has to be "trust
  anchors" of more than one kind; the artifact itself is not signed everywhere (Debian signs the index only, npm's
  registry signs name, version and integrity); a publisher's signature, a repository's signature and an attestation
  are three different things, and what Quader can sign itself is the second and the third, never the first; GitLab
  runs no CA, and a self-managed instance cannot use the public signing service. **In Quader today (read):**
  Debian is the only format that verifies and signs; OCI stores `subject` and serves the referrers for every
  class, verifies nothing, shows a signature's tag as a version and a referrer nowhere, and a mirror run copies no
  referrer; npm's local publish strips the registry signature and the attestations and the two endpoints a client
  asks answer 404, a remote passes both fields through; NuGet keeps the signature inside the package unread;
  Ansible passes the upstream's signatures through; a generic repository knows a signature file as a file. There
  is no table for "signed by" at an artifact, no hook in the format contract, and no trust anchor but Debian's
  upstream keys. **The development agent's advice in the paper:** three stages (detect, read, verify and show for
  all six formats with four kinds of anchors and his switch for project Owners; then Quader signs, as a registry
  where a client checks one and as a witness by attestations of what it received and checked, not in a
  publisher's name; then a gate per repository). **Two corrections of the first reading told to him in the chat,**
  both in the paper: rewriting npm's package documents breaks no upstream signature (the signed string holds no
  URL; what is missing is the keys endpoint), and a signature bundle of the signing service is verifiable without
  the internet (the network is needed for the trust root's upkeep and for revocation lists only). **Not
  verified, named in the paper:** a library for NuGet's signatures; the size the new libraries add to the binary;
  whether the claims of an uploading pipeline stay at a version; details of the older tag scheme; what the extra
  requests for signatures cost against a registry's rate limit.
  **Answered by him on 2026-11-21 between 10:01 and 10:08, read at 10:09 on his word "bitte auslesen".** All
  eight readings stand as written. **Thirteen of the fourteen decisions as the development agent's favourite:**
  the icon stands for what proves the publisher's origin, a registry's signature is in the details (1); four kinds
  of trust anchor under one setting: a CA certificate, an OpenPGP key, a public key, an identity of the signing
  service against the public or an own root (2); anchors are kept in the UI and the API, and those declared in the
  chart's values show read-only beside them (3); shipped as public are the signing service's public root and the
  keys of the well-known upstreams with their preset, no general list of public code-signing CAs (4);
  verification is a background job of the worker with libraries in process, and an anchor change recomputes the
  trust step alone (5); for content of a remote repository Quader fetches the signatures itself (6); a signature
  is valid if it was valid when made, the time proven by a timestamp or a log entry and otherwise by Quader's own
  receipt, no revocation lists, an administrator can block a signer (7); in a generic repository a sibling file is
  a signature by its name (8); Quader signs as a registry where a client checks it with one command, which is
  Debian (built) and npm (10); Quader issues signed attestations of what it received and checked, for an image as
  a referrer (11); Quader does not sign in a publisher's name and shows who uploaded, for a pipeline its verified
  claims (12); one signing identity per installation from the company's CA, OpenPGP per repository where a format
  needs it (13); three stages (14). **One against the favourite, decision 9: the gate.** He chose, besides display
  and alarm, a detail setting per repository "only artifacts with a verified signature", optionally of chosen
  anchors (the favourite had been no gate for now). Read together with his choice of the three stages, whose
  third is that gate: the gate per repository is decided and is the third stage; this reading was told to him,
  with the question whether he wants it earlier. **The question to him,** which way to a signature his CI has:
  the signing service of its own, with the note "Alle 3 Wege sollen von dir gebaut und betrieben werden." (the
  three ways of the paper: a signing service of its own that issues a certificate per job, a code-signing
  certificate of the company's CA, an OpenPGP key). All three are therefore first-class in the first stage and
  each is exercised by a real signature in the tests. What "betrieben" covers beyond Quader (the signing services
  on his cluster and his CI, or the tests alone) is asked back to him; the specification does not wait for it.
  **The first stage is specified** (planner's report of 10:52; on `main` after the main session renumbered the
  decisions and gaps that collided with the ClamAV specification): [signatures.md](../spec/signatures.md), R-SIG-001
  to R-SIG-043, Q-SIG-1 to Q-SIG-16 (the owner's answers) and Q-SIG-20 to Q-SIG-46 (the planner's), ADR-226 to
  ADR-228, the gaps H-1166 to H-1176 on [artifact-signatures.md](../security/artifact-signatures.md); the
  contrary statements of the format pages are superseded in place. **What it settles beyond his answers:** a
  signature is checked once per signature object and content for the whole installation and shown only where it
  is attached, and trust is computed per scope (the installation's, a project's), so an anchor change reads no
  artifact; "no anchor" and "blocked" both show as signed by a signer not trusted; the best signature wins the
  badge; a CA anchor must be one for code signing; Debian's upstream keys stay what they are and are no anchors;
  a pipeline's claims do not stay at a version today (read), so an origin is stored at the upload; the keys of
  npm and the public root of the signing service refresh through that service's update repository, and without
  internet the release's snapshot holds. **Measured by the planner:** the libraries add 4.76 MiB (6.6 %) and 52
  modules to the binary; NuGet's signature needs no further module; the older tag scheme was read in its tool's
  source; the tests can sign in process for all three of his ways, a signing service of its own included. **Not
  verified:** whether Docker Hub serves the referrers and how it counts them; which timestamp roots nuget.org's
  countersignatures use; the cost of a trust recomputation. **Build plan, four packages:** (1) the contract's
  declaration, check 18 of the kit, the store, the origin, each format's detector; (2) verification and trust,
  the anchors, the API, events and metrics; (3) the formats' wire behaviour (OCI's attachment tags out of the
  lists and following their image, mirror, the background fetch, npm's keys and attestations endpoints, Ansible's
  upload of signatures); (4) the UI. **Package 1 is landed (2026-11-21, 12:55, run record), package 3 is landed
  (the same day, run record), package 2 is landed (the same day, run record), and package 4, the screens, is
  landed (the same day, run record): the first stage is whole.** **One question of the planner for the owner (Q-SIG-34), told to him at 11:20 with its default, unanswered when he left:**
  what a virtual npm repository's keys endpoint serves before Quader signs local packages: (a) the union of its
  members' keys, with `npm audit signatures` reporting every local package as unsigned until the second stage
  (the planner's recommendation, H-1176); (b) no keys; (c) signing of local npm packages pulled into the first
  stage. Built as (a) meanwhile; it concerns package 3.
  **The second stage, Quader signs, is specified on `main` since 2026-11-21, 13:10** (R-SIG-044 to R-SIG-060,
  Q-SIG-53 to Q-SIG-74, R-HA-071, Q-HA-385, ADR-230, H-1183 to H-1193; **package 1, the signing identity, is
  landed since 20:58 the same day and nothing signs with it yet; its section of the settings page is landed
  since 21:47; npm's registry signatures are landed since 23:44, proven with the real `npm audit signatures`;
  the receipt attestations are landed since 2026-11-22, 01:42, proven with the real `cosign
  verify-attestation`; the second half of the screens is landed since 02:29 (run record): the second stage
  is whole. To use it his installation needs a signing identity, by one value of the chart or by the
  admin area's section; whether his issuer signs such a certificate is among the questions below
  (Q-SIG-53)**). **One signing
  identity per installation:** an ECDSA P-256 key with a code-signing certificate of the company's CA. By the
  chart it is an existing Secret or a certificate the chart asks cert-manager for, mounted into the worker pods
  alone and read again without a restart, never in the database; by the admin area Quader makes the key, seals
  it with the key ring and hands out a certificate request, and the administrator uploads what the CA issued
  (an import of an existing key and certificate behind more options). While the chart names an identity it
  wins, and uploads are refused. A new key is announced for ten minutes while the old one signs, then it takes
  over; old npm signatures stay valid through the old key's expiry, attestations are made again. Only the
  worker signs, by a job of its own right after a commit, and a pass signs what was stored before an identity
  existed. **npm:** every version of a local repository gets a registry signature that `npm audit signatures`
  checks against the repository's keys endpoint; remote and virtual versions keep their upstream's (H-1176
  closes for an installation with an identity). **Attestations of receipt for every format** (an in-toto
  statement with a predicate of Quader's own: the time of receipt, the origin with a pipeline's claims, the
  integrity states, the malware and the vulnerability state with their dates; in a Sigstore bundle with the
  leaf certificate, without a log or a timestamp), made after the checks' first verdicts or after an hour, made
  again when the malware state or the identity changes, removed with the item, served by the API with a trusted
  root and the chain that anyone may fetch; for an image also as a referrer. Debian stays as it is; Ansible
  Galaxy, NuGet and generic get the attestation and no registry signature; system repositories get nothing.
  The format contract gains what a format's attestation is about (version 9). The screens: a section **Signing
  identity** on the signatures' settings page, Quader's own signature and attestation in the details of a
  version or a file and never as the badge, a tab of the client setup that shows the one command to verify.
  **Found by the planner in the client's source and decisive for the design:** as soon as a bundle stands
  among an image's referrers, the current cosign stops reading the older signature tags, so an attestation of
  Quader on every image would make a publisher's older signature unverifiable.
  **Four questions of the planner to the owner, each built with its recommendation meanwhile:** (1) where an
  image's attestation stands (Q-SIG-66): (a) always as a referrer, which hides older publisher signatures from
  cosign; (b) as a referrer only where it hides nothing (the repository's referrers come from a real referrers
  API and no older signature tag exists for the digest; removed if one arrives), otherwise through the API;
  (c) through the API only; (d) a referrer of a media type of Quader's own, which cosign ignores. Built: (b).
  (2) What proves the time of an attestation (Q-SIG-63): (a) nothing for now; (b) a timestamp authority of the
  company; (c) entries in the log of the company's own signing service. Built: (a); (b) or (c) once his
  signing service runs, which would also let an admission controller that insists on a time proof verify them.
  (3) Whether his company's CA issues ECDSA P-256 code-signing certificates (Q-SIG-53): npm's client accepts
  registry keys of that type only; if it cannot, the ways are RSA with npm left unsigned, or a second key for
  npm. Built: P-256, and he is asked. (4) Ansible Galaxy (Q-SIG-68): (a) no registry signature; (b) Quader
  signs with an OpenPGP key per repository as a signing service. Built: (a), until one of his clients checks
  with a keyring. **Build plan, four packages, after the first stage:** (1) the identity with its states, the
  chart, its API, events, metrics and alerts; (2) npm's signatures and the local keys endpoint; (3) the
  attestations with the contract's method for every format, the referrer under the conditions above, the route
  and the client setup's tab; (4) the screens. **Its tests cost** under 3 s of unit tests, milliseconds per
  format in the kit, about 30 s in a new integration area, about 1 s of the chart's job, one case of about 6 s
  in the npm leg and one of about 4 s in the OCI leg (which needs the cosign binary in the OCI clients' image,
  its size not measured); no new leg. **Verified by the planner in the tools' source or documentation:** how
  npm's client checks registry signatures and which errors end its run; cosign's offline verification with a
  trusted root and how it picks referrers; what the signing library accepts; cert-manager's usage `code
  signing`. **Not verified:** whether `npm audit signatures` works without reaching the internet; whether a
  flag gives cosign its older behaviour back; whether the signing library takes a trusted root without logs as
  Quader builds it (read, not run); an admission controller's check with a certificate chain; every figure of
  CPU, storage and duration, which are estimates.
  **The third stage, the gate per repository, is specified on `main` since 2026-11-21, 14:02** (R-SIG-061 to
  R-SIG-078, Q-SIG-75 to Q-SIG-95, ADR-231, H-1194 to H-1200; **package 1, the gate at the download, is landed
  since 21:47 the same day, package 2, the releases, the previews and the API, since 23:44, and package 3, the
  screens, since 2026-11-22, 00:57 (run record): the third stage is whole. Measured with real clients and
  repaired where they found a fault; not verified from the screens against a real gated repository**).
  **The setting:** a
  repository's detail section **Signatures** gets "only artifacts with a verified signature" (off by default)
  and optionally the anchors that count; never in a creation flow; narrowing it is any settings writer's,
  weakening it needs the right that weakening the malware gate needs. **What passes:** an artifact with one
  signature of its publisher or its provenance that shows as verified (with chosen anchors: by one of them);
  a registry's signature alone never passes, nor does Quader's own of the second stage; untrusted, blocked,
  invalid, unreadable and missing are refused, each with its reason; not checked yet is pending. **Where it
  acts:** at the download, for every format, where the malware gate sits; not at the upload, because half the
  formats sign after the push. An image is judged at its manifest, an index's signature covers its platform
  manifests, and signatures, attestations and SBOMs of an image stay fetchable (recognised by content, not by
  the tag's name); npm passes on provenance; NuGet on an author's signature; Ansible on a publisher's
  signature; a generic file on its sibling; Debian is not offered the setting, because no package carries a
  publisher's signature. A remote's first request fetches and checks the signatures first, with a bounded wait
  of 25 seconds; in a virtual repository its own gate and the holding member's both apply. **What a client
  gets:** 403 with a reason or 503 with a retry hint, in the shapes each format already has for the malware
  gate, never cached; an audit event, two metrics, and one alert for the case that clients wait because the
  checks do not keep up. **A removed anchor or a blocked signer** is subtracted by the gate itself within the
  propagation bound; a widening waits for the recomputation. **Before switching on** a bounded preview says
  how many artifacts of the repository would be refused, and before an anchor is removed or a signer blocked
  how many artifacts of gated repositories lose their state. A holder of the triage right can release one
  artifact in one repository. The three checks (malware, vulnerability, signature) decide before one shared
  wait, in a fixed order of refusals.
  **Three questions of the planner to the owner, each built with its recommendation meanwhile:** (1) how strong
  the gate is without chosen anchors (Q-SIG-76): (a) whatever shows as verified passes, the shipped public
  anchors included, with a sentence of warning at the setting; (b) only what is verified by an internal anchor,
  unless anchors are chosen; (c) anchors must be chosen. Built: (a), so that the gate is what the screen shows;
  (b) would refuse artifacts that show green, (c) is against his "optionally". (2) Debian (Q-SIG-80): (a) not
  offered; (b) a package passes when a covering index of the remote is verified by an administrator's anchor,
  which is an exception to his decision 1 and never passes in a local repository. Built: (a). (3) A gate at
  the upload as well for npm and NuGet, the two formats whose signature arrives with the upload (Q-SIG-79):
  (a) no; (b) yes for those two. Built: (a), one behaviour for every format, and the checks stay in the worker
  as he decided. **Build plan, three packages, after the first stage's packages 2 and 3:** (1) the gate with
  the contract's method that classifies an item, the serve points, the wait and the held fill, the combined
  decision with the malware gate, event, metrics, alert; (2) releases, the preview, the impact count, the API,
  the Bastion's feed, backups; (3) the screens. The second stage is no prerequisite. **Its tests cost** about
  2 s of unit tests, milliseconds per format in the kit and about 22 s in the integration area `signatures`;
  no end-to-end case (one measurement by hand), no new leg. **Verified by the planner:** that cosign signs only
  the index of a multi-platform tag unless told otherwise (its source); what a symbol package of NuGet is
  signed with (documentation). **Not verified:** whether authors or nuget.org sign symbol packages in
  practice; the clients' behaviour on 403 and 503 was taken from the malware gate's record, not read again.
  **Measured with real clients on 2026-11-21, evening** (R-SIG-042, R-SIG-078, [the table](../developer/signatures.md#measured-with-real-clients)):
  cosign 3.1.4 (key pair, attestation, the older `.sig` tag through deprecated flags), Notation 1.3.2, GnuPG and `cosign sign-blob`
  siblings, npm's public registry signatures and provenance are read, verified and trusted when their anchor is added, and every client
  verifies through Quader; Docker pulls a multi-platform image on its index's signature; the kubelet shows `denied: <text>`, Docker 29
  `error from registry: <text>`, npm the text, `dotnet restore` and `ansible-galaxy` the status only. Found (section 8, Product): no real
  NuGet signature is readable; no client can sign an image in a gated repository; a posted Ansible signature leaves the store at the next
  discovery; a remote image's version lists no signature; `missing` says "no signature" beside a registry signature. Not measured: keyless
  signing (it needs a sign-in or a signing service of its own).
  **Of the measurement's seven findings six are repaired and landed** (2026-11-21, 23:44 and 2026-11-22, 00:44, run
  record); the seventh, that NuGet's client and `ansible-galaxy` show a refusal's status and never its text, is the
  clients' behaviour and stands.
  **A fourth question, from that measurement, built with the development agent's choice meanwhile (Q-SIG-211):** an
  image pushed into a gated repository has no signature yet, and `cosign sign` and `notation sign` read its manifest
  from the repository before they sign, so the gate refused the very read that would make the image pass. What may
  read through the gate: (a) who may write to that repository (chosen: who may put content there may read it back to
  sign it, and a consumer with the read right alone stays gated; the price is that a token with the write right
  pulls unverified images); (b) every read by digest (rejected: a kubelet pulls by digest, so the commonest pull
  would pass); (c) nothing, and an image is signed in an ungated repository and copied over with its signatures
  (strictest; Quader has no promotion yet, and the flow would have to be built and documented first); (d) the
  uploader for some minutes after the push (a hole that depends on time). It holds for OCI alone, because the other
  formats' signatures are made on local files or arrive with the upload; the malware gate and the vulnerability
  block are not opened by it.

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

**Decided by the owner on 2026-11-22, built the same day: all four packages are on `main` since 14:50, the first
three released and on his installation, the last one tests and manual (run record)**

- **A Bastion serves every format as the main installation does** (2026-11-22, 09:46, on reading in the storage
  topic's working paper that a Bastion serves neither images nor Debian): "Wieso liefert der Bastion kein Debian
  oder Images, das ist doch total wichtig. Er soll alles liefern wie die Hauptinstanz." **Why it was not so** (read in
  the specification the same hour): two deferrals that the development agent had decided for his walkthrough and
  that he never saw as a question, Q-OCI-23 ("no exposure rules and no Bastion for the format in this phase", the
  Bastion answers `/v2/` with 404 itself) and Q-DEB-26 (the same for Debian). Both give exposure rules as their
  reason (which tags, and an endpoint's narrowed view of a signed index), which a Bastion on the default endpoint
  does not need. **Read as:** the read paths of every format there is and every format to come; not the web
  interface (his Q-BAS-14), no writes through a Bastion (R-BAS-014), and no access to the main installation's
  object store (his Q-BAS-10 stands until he answers the working paper). **In work since 09:52: its planner,** in a
  worktree, with the points to settle: the registry's path family at the Bastion's host root and its token flow
  (the Bastion holds no key ring), what of an image is cached and how it is revalidated, layers of gigabytes;
  Debian's signed indexes and what keeps an `apt update` consistent against a cache; the ClamAV definitions'
  endpoint; a rule of the format contract so that a format to come is served without a decision of its own; the
  feed, the download events and the security page; every sentence that says a Bastion does not serve a format;
  the tests by the rule that they stay runnable; a build plan. **Not verified:** whether a Bastion forwards
  Debian's paths today (the specification excludes it, a research found no code that refuses them).
  **Specified on `main` since 10:26** (its planner worked from 09:52 to 10:25; two commits of documents, rebased
  and landed by the development agent after the links, the check, the evidence regenerated without a
  difference and the wording scan; its worktree and branch are removed): R-BAS-050 to R-BAS-060, Q-BAS-165 to
  Q-BAS-180, the gaps H-1242 to H-1248, ADR-238; the interface between Bastion and main installation rises to
  its fourth version; the Bastion's parts of Q-OCI-23, Q-DEB-26 and Q-ANS-25 are superseded in place, their
  statements about exposure rules and Debian's vulnerability matching stay. **What it says:** a Bastion serves
  every format's read paths, `/api/<format>/…`, `/files/…` and `/v2/…` at its own host root, read-only, without
  knowledge of a format in its code. A token request goes to the main installation's token service, the
  Bastion holds no key; a registry token is checked by the main installation and its read shape is capped at
  partial (without the cap a token for one image would read every cached image of the repository); an
  anonymous token counts as no credential. An image's routes are cached by their kinds (a manifest by tag
  and the tag list as documents, a manifest by digest and a blob as artifacts, referrers never), and the
  registry's headers survive a hit. A resumed download of a large layer streams from the spool, and a second
  caller of a running fill follows it. Debian's signed indexes pass unchanged and the by-hash paths keep an
  `apt update` consistent. The ClamAV definitions are served anonymously or with a token, never with a client
  certificate. **A format to come needs no decision of its own:** the handshake carries a table of facts per
  format, the format contract gains the declaration of them, and a nineteenth check of the kit holds it.
  Withdrawals of images, Debian and Ansible content reach every Bastion through the feed within seconds. **Read
  in the code by the planner:** Debian, Ansible Galaxy and the ClamAV definitions are forwarded by a Bastion
  today, against what the specification said (section 8); `/v2/` is refused on both sides; an image's digest
  would never be cached (the entity tag's form), the registry's headers are dropped on a hit, every 401 marks
  a credential invalid, and the check would refuse every registry token. **Left for its last package, by the
  planner's choice and right:** the four manual pages that say a Bastion serves no Debian and no images, because
  the manual describes what a running installation does. **Three questions of the planner for the owner,** each
  specified with its recommendation, are numbers 14 to 16 of the list above. **Its tests cost** (estimates):
  under 4 s of unit tests, about 20 ms per format in the kit, about 10 s in the integration area `bastion`,
  about 100 s in the end-to-end leg `oci` (60 of them a Bastion release in the leg), nothing in the Bastion's
  own legs; with 25 formats about half a second of the kit and no further leg. **Build plan, four packages**
  (the developer page of the Bastion): A the two contracts and the kit's check; B the main installation's side
  and C the Bastion's side, in parallel after A; D the client case on kind and the manual. **Packages A, B and C
  are landed (11:16, 12:31, 13:35; run record): images and Debian are served through a Bastion, shown with the
  real clients against a Bastion run locally. D is in work since 13:37.** **Not verified:** any run with a real client; whether the token service sees a Bastion's
  host; how the image clients take a challenge without a scope; whether the largest definition file is above
  the bound for documents.

**Decided by the owner on 2026-11-21, waiting to be built**

- **The signature updater leaves the worker and becomes a Deployment of its own, and ClamAV's definitions become a
  repository kind of their own with an endpoint for every ClamAV of the company** (2026-11-21, 09:56, on seeing
  the two sets in `upstream/scanner-clamav`): "Ich sehe gerade du hast freshcalm an die worker gehängt, das ist
  natürlich falsch. Gliedere Freshclam aus, es wird als seperates deployment ins helm integriert. Default reicht uns
  1 replica. Diese Instanz fetched die online-ressource, und wir kommen regelmäßig bei unserer freshclam vorbei und
  besorgen und ggf. das aktuelle update. Außerdem möchte ich, dass wir das freshclam artefakt als spezifisches
  ClamAV-Repo in Upstream einbindbar machen. ClamAV wird also ein eigenständiges Artefakt-Repo, wir ziehen alle
  verfügbaren Metadaten aus dem ClamAV-Definitionen. Ein Admin kann in den Upstream-Repos
  ClamAV-Definition-Updates einschalten, wodurch ein unternehmens-Endpunkt für alle ClamAV Instanzen bereitgestellt
  wird, dieser Endpunkt bedient sich automatisch aus dem internen Freshclam und erhält aktualisierungen. Hier weiß
  ich nicht, ob eine Konfigurierbarkeit der Virenquelle einen Sinn macht? Gibt es überhaupt alternativen? Kann ein
  ClamAV sich gegenüber einem Endpunkt authentifizieren? Falls nein, macht eine Visibility außerhalb von public
  dann überhaupt einen Sinn? Automatisches Cleanup: ClamAV Artefakte sind stark zeitbezogen, eine neuere Version
  ist wohl immer besser als eine ältere, wir müssen hier hinter uns sauber machen um nicht über Monate alte
  Definitionen aufzubewahren. Hier bitte einen für ClamAV passenden Weg finden." **This replaces** the answer to
  decision 8 of the malware scan's working paper (the updater as a container beside the worker, the development
  agent's favourite, which he had taken on 2026-11-20). **Fixed by his words:** a Deployment of its own in the
  chart, one replica by default; it alone fetches from the internet; Quader takes the update from it by asking
  regularly; the definitions are a repository kind of their own in `upstream` that shows every piece of metadata
  the definition files hold; an administrator switches the definition updates on among the upstream repositories,
  which offers one endpoint for the company's ClamAV installations, fed from the internal updater; old sets are
  cleaned up automatically. **His three questions, open until answered with facts:** whether the source of the
  definitions should be configurable and whether there are alternatives at all; whether a ClamAV can
  authenticate against an endpoint, and whether a visibility other than public makes sense if it cannot; which
  cleanup fits. **Both researches ended by 10:12** (the facts read in the updater's source of the pinned line and in the
  project's documentation, nothing tested against a running updater; and what is built today, read in the code).
  **Answered to him at 10:15.** *The source:* there is one origin of the official definitions; the sets of third
  parties only add to it and carry terms of their own (accounts, payment, no commercial use for one of them); so
  no choice of source in the plain path, and as a detail a chart value that points the updater at another mirror.
  *Authentication:* a stock updater can present a client certificate since its version 1.2 (environment variables
  only; Quader maps no client certificate to a principal on the package routes) and very likely sends Basic
  credentials written into the URL (undocumented there, and the client logs the whole URL; Quader accepts a
  token as the Basic password already); no token header. **A stock client goes silent for 24 hours after a 403
  and for four hours after a 429, whatever the server, so the endpoint answers 401 or 404 only.** *Cleanup:* the
  updater never downgrades and the project corrects a bad release with a newer one, so the current set and the
  one before it stay (the previous one for scanner pods that still run it) and everything older goes as soon as
  a newer one is in use; the history stays as metadata without files. Read on the way: a cleanup exists today
  (sets older than 48 hours go, the one in use and its predecessor stay), and the header's fields are parsed and
  thrown away today. **One decision is with him, the endpoint's visibility:** (a) public or off; (b) public by
  default and the levels below it as a detail, with the token in the client's URL and a note that the client
  logs it, proven once by a test with a real updater (the development agent's recommendation: the levels exist,
  the server side costs next to nothing, and an installation that allows no anonymous access can still offer the
  endpoint); (c) client certificates as well. **Specified** (planner's report of 10:51, on `main`): R-CLAMAV-001 to R-CLAMAV-009 on
  [formats/clamav.md](../spec/formats/clamav.md), R-MAL-032 to R-MAL-034 and the rewritten R-MAL-010 to R-MAL-012,
  R-HA-069, R-REPO-075, R-UI-194, ADR-225, the gaps H-1162 to H-1165 on
  [clamav-definitions.md](../security/clamav-definitions.md). **What it says:** the Deployment
  `malware-updater-freshclam`, one replica, exempt from the rule of two; the updater's tool with whole signed
  containers beside a Quader container without a secret (a kind of the role `scanner`) that seeds the directory
  from the set in use and lists it on a link with mutual TLS; the worker asks it every 30 minutes, fetches only
  what it does not hold and verifies each container's signature against the project's public key before storing
  it; the task is idle where no updater is configured and waits until a first set exists (the finding of the task
  that failed every 30 minutes); the format `clamav`, system only, with the one repository `upstream/clamav` in
  place of `scanner-clamav`, one package per database, every header field, how a file verified, its signature file
  and its member files shown; the switch **ClamAV definition updates** in the system repositories' table and on
  the repository's page, off by default; the endpoint serves the set in use to a stock client with one line of
  configuration and answers a refusal with 401, 404 or 503, never 403 or 429; the set in use and the one before
  it stay, the rest goes at once, the history stays as metadata; a definition file cannot be deleted by hand; a
  chart value points the updater at another mirror, and the repository as a source stays for an installation
  without internet. **Decided by the planner against the main session's leaning:** no real client in an
  end-to-end test, because the client accepts only containers the project signed and no test may download them;
  the integration tier replays the client's request sequence, and one measurement with a real client against the
  owner's installation is recorded once. **Not verified:** a real client against the endpoint; credentials in the
  URL end to end; the legacy signature's algorithm in the tool's source (the builder reads it); whether the tool
  writes its files atomically. **Build plan, four packages:** (1) the format, the repository, the API; (2) the
  updater process and the import; (3) the chart; (4) the UI. **In work since 10:57:** packages 1 and 3 (run
  record); 1, 2 and 3 land as one. **The owner's installation at that landing:** the way to the internet as a
  label of the new pod (`malwareUpdaterFreshclam.podLabels`), which the development agent commits there; the
  first set is fetched whole once. **The endpoint's level is decided by him (2026-11-21, 11:17): "Visibility des Endpunkts: b +
  zusätzlich Client-Zertifikate."** Public by default, the levels below it with the token in the client's URL, and
  client certificates as well. The first two are what packages 1 to 4 build; **the client certificates are a
  package of their own after them,** specified on `main` since 11:40 (R-CLAMAV-010, Q-CLAMAV-8 to Q-CLAMAV-14,
  Q-EP-96, Q-UI-905, H-1177 to H-1180; Q-REPO-415 carries his words): a further endpoint whose TLS ends at
  Quader, reached through a TLS passthrough of the ingress or a Service of its own, with the company's CAs from
  a Secret that is read again without a restart; Quader never takes a certificate from a header. A request
  without a token there is a certificate subject with no principal: one fixed rule lets it read only
  repositories that admit certificates (`upstream/clamav` while its switch is on) at the levels internal and
  public, never at private, never a write, and it is bound to no service account. A missing, expired or
  foreign certificate fails the handshake, which `freshclam` counts as a failed attempt and never as a reason to
  stay away; the certificate's subject is recorded where a user stands, its serial where a token stands. The
  chart renders the passthrough per ingress controller (an annotation for HAProxy Ingress and ingress-nginx,
  refused for traefik). The client setup offers four ways: public, the token, the certificate (the two
  environment variables and the CA bundle), and the plain download of the three files for an installation
  without an updater. On the admin page the certificates are read-only lines in the fold **Visibility**. **The
  planner's decisions are the development agent's defaults told to him before he left, narrowed in one point:**
  a certificate holder counts as a reader of the definitions only, not as an internal subject anywhere else.
  **Built as one package** (the binary and the chart share the key names) **after packages 1 to 4 are on
  `main`;** its tests cost under a second of unit tests, about two seconds of the chart's job and about one
  second of the integration area `malware`, and nothing in an end-to-end leg. **Open by its text (H-1177 to
  H-1180):** every certificate of the admitted CAs is admitted; no revocation check; behind a passthrough the
  client's address is the controller's; a refused handshake leaves a line of the process log only. **Not
  verified:** that the integration harness takes a client CA on a further endpoint; the reload script and the
  unit name of a ClamAV installation in the client setup; a real `freshclam` with a certificate, which stays
  the one recorded measurement of R-CLAMAV-009. **His
  clarification the same minute:** "Weil ich Freshclam lese und nicht genau weiß wie du das meinst, Ziel ist ja,
  dass wir für ein Unternehmen der Zentral Endpunkt für ClamAV werden, ein zusätzlicher freshclam vor dem jeweiligen
  clamav sollte dabei nicht notwendig sein. Ich hoffe das ist klar." Answered: the updater is ClamAV's own, part of
  an ordinary installation, and gets one line of configuration that points at Quader; an installation without it
  fetches the files by plain HTTP under fixed addresses; nothing stands between Quader and a ClamAV. (That the
  updater comes with every installation is the development agent's knowledge, not read in the research.) **For the
  builders:** the client setup names both ways, the updater's one line and the plain download. **The unit's name follows the
  naming rule** (the tool in the name); it is fixed with the specification. **His installation:** the new kind of
  pod needs the way to the internet in his values, which the worker carries today for the updater; the
  development agent brings that with the landing (standing rules).
  **Built and landed on 2026-11-21 at 14:46** (run record), and the client certificates the same day at
  17:05 (run record). **For his installation to use a certificate, he decides and provides** (nothing of it
  is set there; the endpoint for everyone works without it): a host of its own for the endpoint, a Secret with
  his company's CA, a certificate for that host, and the endpoint's block in the values with the passthrough
  of his ingress; the development agent commits it under `apps/quader` when he names the host and the CA. **Decisions of the builders and the development agent in it, for his walkthrough:** a
  version's row stays as history after its files are removed (Q-CLAMAV-15); the client's line is written
  without a trailing slash (Q-CLAMAV-16); a commit without its metadata is refused (Q-CLAMAV-17); switching the
  clients off is a journal part of its own (Q-REPO-420); the mirror's and the proxy's values are checked so
  that none adds a line to `freshclam.conf` (Q-HA-384); a put-back keeps the refused version until the next
  set (Q-MAL-102); every new file of a set is checked before any is stored (Q-MAL-103); while no set is in use
  the import asks again every 30 seconds (Q-MAL-104); the updater's report carries its `daily`'s build time and
  no slots (Q-OPS-226); the format declares no signatures of its own, the import's check is its verification
  (Q-SIG-96); while the switch is on the repository is offered like any readable one (Q-UI-908); the admin
  page builds the client's line from the address the API gives (Q-UI-909); a database's versions take their
  state from the version list (Q-UI-910, Q-API-384); the install panel leads to the client setup (Q-UI-911);
  **Outdated** holds for `daily` alone, as ClamAV's own warning does (Q-UI-912); the updater's rows have a
  section and a fold of their own (Q-UI-913). **His question whether another source of signatures makes
  sense** is answered in the build as told to him: one official origin, a private mirror or a proxy by a
  value of the chart. **The measurement with a real client** is recorded (run record, package 2).
- **The detection moves into the scanner unit, and the analyser leaves every other image** (2026-11-21, after he saw
  no Trivy container in the scanner pod and asked why the worker runs the analyser too and why the scanner's answer
  counts as untrusted): "Option B, nach der umbenennung. Trivy dann aus den anderen Images entfernen." Option B as put
  to him: the unit `vuln-scanner-trivy` does both steps, the analysis of an image and the detection of a stored
  package list against the database, and the worker only calls it. The worker then runs no tool (H-1038 goes: no
  analyser parses an image's values in the pod with secrets), a second tool with a database format of its own would
  be a unit beside the first and nothing in the worker, and the analyser lives only in the unit that carries its
  name. What it needs, named to him: the scanner unit reads the database (a route of the scan listener, as the malware
  scanner reads its signatures), every detection after a new database is a call that carries the stored package list
  (no layer is read again: the analysis stays once per manifest digest and analyser version, R-VULN-033), and the
  report comes back as untrusted input that the worker reads in Go under limits. **Order:** after the two scanner
  units have their tool's names (run record); first the detection in the unit, then the analyser out of the images of
  `api`, `packages`, `worker` and the malware scanner. **Not measured, to be measured by its planner:** how long the
  detection of every stored image takes through the scanner's slots after a new database, and what the database
  costs each scanner pod in disk. **Where the scanner unit's analyser comes from once it is out of the common image, decided by him the same
  morning: "Option b"**, the analyser's own image as an init container that copies the binary into a volume of the
  scanner pod, and not a second image built with every release. The pattern is the ClamAV image's: one pin by digest
  in `tools/versions.env`, the same value in the chart, a chart test that holds them equal, one Renovate group; the
  four digests of the release archive that are kept by hand today go. The pod shows tool and version as an image of
  its own, and a release stays one image. **Read and measured for the question:** the release image is 239 MB, of
  which the analyser is 163 MB; the analyser's image of the pinned version exists for amd64 and arm64, is
  Alpine-based with `cp`, 186 MB, runs as root unless told otherwise, and its binary is byte for byte the one in the
  release image (SHA-256 compared on arm64); the binary's path is a configuration key already
  (`vulnerabilities.images.binary`). **Named to him as the price:** a node pulls a third party's image when a scanner
  pod starts, as it does for ClamAV. **For the package's builder:** the init container runs as non-root on a
  read-only root file system with nothing but the volume; the development profile starts the scanner units only in
  the legs whose tests need them, so that every other leg loads the small image alone (read in the profile: today
  every end-to-end leg starts the scanner pods). **Not verified:** the copy as non-root in a pod; the time of pull
  and copy at a pod's start; a rate limit of the registry on the owner's nodes.
  **Specified on `main` since 2026-11-21, 12:07** (R-VULN-048, R-VULN-049, R-HA-070, Q-VULN-196 to Q-VULN-206,
  Q-HA-378 to Q-HA-383, Q-ARCH-563, Q-ARCH-564, Q-OPS-225, ADR-229, H-1181, H-1182). The unit does both steps:
  for the detection the worker sends the stored analysis and names the database and the analyser's version, with
  no token, because the call reads nothing from Quader and the link admits the worker alone; the worker runs no
  tool and decodes the report in Go under limits. The analysis is kept as the scanner answered it after a bounded
  check, so the rewrite, its fidelity test and H-1038 go when this is built. Each scanner pod holds the database
  read-only: a second container of the kind `trivy-db` reads it through a route of the scan listener with the
  pod's certificate into a volume that the scanner's container mounts read-only, as it does the analyser's
  binary; the scanner's process is not dumpable. After a new database the images that were never detected go
  first, and while the scanners load it the run waits 30 seconds. One analysis per digest; a scanner of another
  analyser version refuses a stored one and the worker analyses again. One pin by digest replaces the version
  and the four archive digests, the chart's image is held equal by a chart test, an update of the digest merges
  and a new version is opened for review. The role `all` builds no image scanner; the test profiles run the
  image scanning in `vuln` and the platform matrix alone. **Measured once on the development machine** (arm64,
  the pinned analyser image): a detection of 17 to 892 components takes 0.06 to 0.30 s at 110 to 230 MB, the
  worker's decoding 0.1 to 32 ms; the database is 142,567,123 bytes and unpacks to 1,807,314,944 in 7 s; the
  analyser's image is 58.5 MB compressed (63.4 MB on amd64), its binary 163 MB; the copy as user 65532 on a
  read-only root file system without capabilities works and runs from a read-only mount. **Estimated, not
  measured:** with two scanner pods a new database is through 1,000 small images in about 1.3 minutes and
  through 10,000 with about 1,000 advisories each in about 80 minutes; a pod loads a database in 10 to 30 s;
  most kind legs gain 5 to 10 s. **Three packages, all after the ClamAV change is on `main`:** the detection in
  the scanner with the loader, the migration and the chart's second container; the database per scanner pod on
  the System page and the advisory sources, beside the first on its API; the analyser out of the images after
  the first is released (the image build, the pins, the workflows, the chart's init container, the development
  profiles). **His installation needs no change of values;** it will pull the analyser's image of its own on
  every node that starts a scanner pod, and each scanner pod reads 142.6 MB per new database and holds about
  1.7 GiB of it, 3.4 GiB while the next unpacks (his nodes' disks were not looked at). **A point the planner
  put to him, decided meanwhile by the development agent (H-1181):** a scanner pod whose own process is taken
  over can answer false detections for every image it serves until the pod restarts, so findings would resolve
  and blocks of the triage would lift. Options: (a) accept it and take it into the security pass before BETA;
  (b) repeat a sample of the detections on a second pod; (c) restart the scanner pods regularly. Built with (a):
  the read-only database and binary and the process that is not dumpable close the cheap ways in, (b) costs
  scanner capacity on every database, and (c) bounds the time without closing the way. **Not verified:** the
  copy in a real pod; any load through the scan listener; anything on amd64; the hardening of the process (a
  unit test on Linux is specified). **The move is whole on `main` since 2026-11-21, 21:47:** its three
  packages are landed (run record). The copy in a real pod was measured by the third package's builder on
  arm64 and checked by the development agent on the amd64 image under emulation; the first amd64 nodes that
  run it are his cluster's, with the release: seen there at 22:06, the copy takes a second and the scanner
  pods are ready within seconds (run record).

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
  and check, the full unit run and the chart tests by the planner, the white-label scan. **Built in four work packages, all landed
  on 2026-11-21 at 06:15 (run record):**
  (1) the gate and the verdicts (migration 00088, the store, the join into the download statement, the gate in the two
  serve points, remote fills, virtual repositories, the OCI walk, the repository's setting, the API, the audit events,
  the Bastion feed, backups), which needs nothing but `main` (its first stage landed alone on 2026-11-20); (2) the scanning
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
  recommendation of (a). **Built and landed on 2026-11-21 at 12:55** (run record; R-REPO-076, R-API-084, R-UI-195):
  the section **Registries of this installation** on the upstream registries' page and the second row of buttons
  in the form of a new remote repository of every format. The builder's decisions are named in the run record
  for his walkthrough.
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
- **A `trivy` of Homebrew (0.69.3) is on the development machine's path, the pin is 0.76.2.** With package 3 of the
  analyser's move `make versions-check` refuses a `trivy` of another version on the path (one version of every tool), so
  it fails locally on that line beside the buildx line of F-17 until the machine's copy is the pinned version or gone:
  `brew upgrade trivy`, or `brew uninstall trivy`. Not changed by the development agent, because it is the owner's tool on
  the owner's machine; told to him in the chat on 2026-11-21. The pipeline's runners carry no `trivy` of their own.
- **The development machine's default kubeconfig has no current context since 2026-11-21, 17:41.** A builder's removal of its
  kind cluster took the current context with it; the file (`~/.kube/config`) still holds its one context
  `kubernetes-admin@wds18-k8s-runner` and was not changed by the development agent, because it is the owner's. To set it
  again: `kubectl config use-context kubernetes-admin@wds18-k8s-runner`. Told to him in the chat the same day. Since then
  every builder's brief demands a kubeconfig file of its own for a kind cluster.
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

- **The integration stack's one lock is the builders' bottleneck** (seen on 2026-11-21 with four to six agents): a
  builder that changed several formats' serve paths held it for over 45 minutes with a proper list of areas, three others
  waited 31 to 43 minutes, among them the repair of a red job that needed 100 seconds of it. To look at: a second stack
  for builders, or a lock per area where two areas share no state; until then a brief says to run the longest areas
  last and one at a time, so that a short run can pass between them.

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

- **The integration shards are close to their limit of 45 minutes** (read from three runs of 2026-11-21 between 16:20 and
  18:31): shard a 37.0 to 39.3, shard b 39.5 to 41.0, shard c 37.8 to 42.2 minutes, and the packages in work add about a
  minute more. This is the lever the owner has before him in "With the owner" (a fourth shard, or the formats' repeated
  integration tests into the kit). Until he decides: when a shard is stopped by its limit or passes 43.5 minutes, the
  development agent cuts a fourth shard by the Makefile's rule, because a higher limit would hide the growth and a
  stopped shard is a red job without a finding; it is told to him with the minutes it costs. The run of 20:58 to 21:42
  with the signing identity: 37.8, 41.0 and 42.2 minutes (the new area `signing` adds 11.5 s to shard b). The run of
  21:47 to 22:34 with the gate: 38.2, 43.4 and 42.1 minutes. Shard b is the bucket of every package the Makefile's rule
  names in neither of the others, so every new area of the day fell into it; with npm's signatures (about 5 s), the
  attestations and the gate's releases it passes the mark. **The fourth shard is cut since 2026-11-21, 23:44** (run
  record, Q-ARCH-565): about 31 to 33 minutes each by the model, about 3.5 runner minutes more per push. **Its first
  run** (00:02 to 00:44 of 2026-11-22): 33.1, 32.7, 30.0 and 32.8 minutes (a, b, c, d), so each shard has about twelve
  minutes of room again. The run of 00:57 to 01:42 with the gate's screens: 33.0, 31.8, 31.2 and 32.9 minutes; the
  layout job, with the 186 pictures of those screens, 43.3 of its 75 minutes, the push's longest job. The run of 02:54
  to 03:40 with the whole of the signatures: 34.1, 32.4, 33.5 and 34.2 minutes, the layout job 44.0.
- **Checks of a list or a convention that sit in an integration area** (found on 2026-11-21, four red jobs in one
  day): the purge test's two lists of tables (area `upstream`), the formats' clients (area `packages`), the registry of
  revision columns (area `postgres`), the count of sealed columns the re-encryption task walks (area `repository`).
  Each failed in the pipeline after a landing whose builder had run other areas.
  To look at: which of them can be decided from the migrations' text or the catalog without a database, in the unit
  job that every builder and the landing run anyway; until then the builders' briefs name the areas and the search
  under `test/integration` for the name of whatever the change makes longer.
### B · The download limits (W-26) and a probe starts the download (W-25)

- **A CI token on the request path** is not tested anywhere above the unit test of its subject: the integration stack has no
  issuer.
- **A crashed replica's leases** of parallel downloads (R-LIM-039 names the case): the integration stack can only stop a
  replica gracefully, which releases them.
- **End to end, not verified:** the `RateLimit` policies of the parallel, upstream and refused figures; the charging of a host
  bound to a repository (no test); the daily fold's sums with rows older than 30 days (only that the query runs).
- **`TestR_LIM_001` and `TestR_LIM_027_AWellKnownMember…`, red in the pipeline on 2026-11-22: found and repaired, a product
  defect.** Run 44958303829 (`integration (shard b)`): `TestR_LIM_001` found `127.0.0.1 instance repo=- requests=1
  refused=0`; run 44967924325: the via test's row (`requests >= 1 AND uncached_bytes > 0` at `upstream/npmjs` with
  `npm-all`) never came, though its fetch was answered 200 with 6169 cached and 10555 uncached bytes. **One cause:** the
  charge buffer recorded a request's count in the usage store at the first flush after stage 7, under the subject and the
  serving repository known at that moment. A flush (every 200 ms in the stack) that fell before the token handler's
  `Retarget` put the token request under the address; one that fell after the dispatcher's `Served` and before the member's
  `ServedThrough` put the request under `npm-all` and left the member's row with the bytes and `requests=0`. Besides,
  `Retarget` made a second, tracked charge: every token request was counted twice and that charge stayed in the buffer for
  the replica's life. **Verified:** both failures reproduced on the unfixed code with the CI messages (the via test printing
  `repo=2 via=4 requests=0 cached=6169 uncached=10555; repo=4 via=- requests=1`) by a charge-buffer flush of 1 ms, a
  change made only for the reproduction; the double count in the stack at 200 ms (the token charged 3 for 2 requests); new
  unit tests that put a flush between the steps fail on the unfixed code. The pipeline's databases are gone: that this
  mechanism is what hit there is inferred from the same row and the same byte counts, and from the replica's log, in which
  the token request is the only request that was anonymous for a while. Not the fourth shard's load: the area took
  122.8 and 126.2 s in green runs with three shards, 126.9, 129.6 and about 129 s (139.3 less the failed wait) with four, and its
  measured waits (the switch to Audit 20 to 25 ms, the floor's 4.03 s) are the same. The windows, measured on the
  development machine in three runs each: stage 7 to `Retarget` 5.6 to 8.7 ms, stage 7 to `ServedThrough` 5.2 to 14.9 ms,
  so a flush every 200 ms hits one in about 3 to 7 runs of 100 per test (`TestR_LIM_001`'s builder saw 1 in 13).
  **Repaired:** a request's count and the limits it met reach the usage store at the flush after it ended, its bytes at
  every flush, and `Retarget` moves the request's own charge. The integration test now asserts exactly two requests of the
  token, and the via test prints the reader's rows when it fails. Not known: the windows' width on the runners, and so
  whether two hits in four runs of one night is more than chance; nothing in the area's timings says the runners got
  slower.
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

- **F-165** one instance of a pooler without a server after a switchover (33.7 s on 2026-11-20, 69.4 s on 2026-11-21): cause known and fixed on 2026-11-21, `ha-pooler` green once with the fix; why the one connect hung is not known (section 8).
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
