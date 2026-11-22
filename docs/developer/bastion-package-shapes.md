# Read shapes per package at a Bastion: the build plan

Status: specified on 2026-11-22; **P1 (the wire) built on 2026-11-22**, P2 to P4 not built ([bastion.md](../spec/bastion.md#read-shapes-per-package) R-BAS-080 to R-BAS-088,
Q-BAS-240 to Q-BAS-255, the gaps H-1380 to H-1384 and H-1242;
[ADR-254](../decisions.md#adr-254--read-shapes-per-package-at-a-bastion-a-registry-token-is-full-on-the-images-of-its-grant); security
perspective [security/bastion.md](../security/bastion.md#read-shapes-per-package)). This page is the build plan: the four packages, what each
owns, their order, the tests and what they cost, the measurement to run once, and what was not verified.

## What has to be in your head first

- **Today one shape per request.** The marking computes the caller's shape on the addressed repository once, in `Decided`, before the
  handler runs, and caps a credential with a registry grant at `partial` (`CapRegistryShape`); a `Metadata` or `Computed` answer is `shared`
  only on a `full` shape ([mark.go](../../internal/api/bastionmark/mark.go), [shape.go](../../internal/bastionmain/shape.go)).
- **The decision point knows reach per repository only.** `ReachOn` asks for `ReadAll`, the wiki, the populate right and an endpoint that
  exposes everything; a virtual repository its gate and every member ([reach.go](../../internal/authz/reach.go),
  [visible.go](../../internal/authz/visible.go)). R-BAS-080 adds `PackageReachOn` beside it, with the same five conditions per package.
- **The package is already decided inside the handler.** Every format calls `Host.Authorize(ctx, rq, Target{Package…})`, which calls
  `bastionmain.NoteUnknown` on a refusal today ([host.go](../../internal/formathost/host.go)); OCI authorizes the image name
  ([handlers_blob.go](../../internal/formats/oci/handlers_blob.go)). R-BAS-081 notes the package of each such decision into the request's
  `Absence` record ([absence.go](../../internal/bastionmain/absence.go)), read at the status line like `absent` and `counted`.
- **The grant is not in the subject.** `authz.Subject` has no field for a registry grant; the enforcement point in front of `/v2/` checks it
  per request ([enforce.go](../../internal/api/registry/enforce.go), [grant.go](../../internal/auth/registrytoken/grant.go)). So the package
  shape is limited to the grant by the marking and the check (`CapRegistryPackage`), not by the decision point.
- **The Bastion holds three kinds of answers** under the credential key, four validity rules, and a memory LRU of 200,000 answers signed into
  Valkey ([answers.go](../../internal/bastion/answers/answers.go), [answerstore.go](../../internal/bastion/engine/answerstore.go)). R-BAS-084
  adds the fourth, `answers.Package`, with the same rules; `grants`, `token` and `principal` entries match it, `content` entries do not.
- **The read flow asks the check first for a stranger** and then sends the conditional request
  ([readflow.go](../../internal/bastion/engine/readflow.go), lines 70 to 106). R-BAS-085's step 5 sends the conditional request without a
  check when the caller's shape on the repository is known and `partial`, so no timing tells a cached package from an uncached one.
- **The Bastion caps a registry token itself** (`capRead` in [registry.go](../../internal/bastion/engine/registry.go), Q-BAS-198). R-BAS-084
  records a package answer `full` for a registry token only where the token's grant, read without verifying it from the claim `g` (entries
  `{r, n, a}`, pull is bit 1, payload version 1; `iface.RegistryTokenGrant` beside `RegistryTokenAnonymous` in
  [regtoken.go](../../internal/bastion/iface/regtoken.go); claim names in [token.go](../../internal/auth/registrytoken/token.go)) names the
  pair.
- **Entries stay per cache key.** An entry is written only from a marked `200` or `304` for its own key and gains `Package`
  ([entries.go](../../internal/bastion/engine/entries.go), [cache.go](../../internal/bastion/cache/cache.go)); a blob is reachable only through
  a path that names an image (R-OCI-010), so a token granted one image never reads another's entry by digest.
- **Kit check 12 compares only callers who read everything** ([checks_shared.go](../../internal/formats/formattest/checks_shared.go)); kit
  check 21 compares a reader of the repository with a reader of only the response's package, with `Fixture.OtherPackage` seeded.
- **The interface is at version 5** since P1 ([version.go](../../internal/bastion/iface/version.go)); it was 4 before. The handover
  ([bastion-handover.md](bastion-handover.md)) takes 6. Equal versions are required on both sides (R-BAS-009).
- **No configuration value is added** on either side; the check's bound is `bastions.check.max_repositories` (16).
- **What disappears:** a warm Docker pull of an image of ten layers falls from 14 requests to the main installation to 3 (token probe, token,
  one check), Podman's from 13 to 2; npm with a reader narrowed to `@acme/*` sends nothing on a second run within the period. Derived from the
  read flow, not measured (the measurement below).

## Work packages

At most four packages run at once, counting the packages of the other plans that run then. The specification is on `main` before P1 and P2
start; each package adds "As built" notes to its own blocks only.

| Package | After | What it builds | Owns |
|---|---|---|---|
| **P1 · The wire** — **built** on 2026-11-22 | nothing | `iface.InterfaceVersion` 5; `HeaderPackage`, `HeaderPackageRead`, `Marking.Package` and `Marking.PackageRead` with `ParseMarking`'s rules (R-BAS-082); `CheckRequest.Packages`, `CheckAnswer.Packages` (R-BAS-083); `RegistryTokenGrant` and, if kept, the `exp` reader of Q-BAS-252; `testdata/interface.txt` regenerated; `maintest` speaks version 5 and can mark packages; the OpenAPI schemas `BastionCheckRequest` and `BastionCheckAnswer` and `x-quader-bastion-interface: 5` on the four operations, with what `make generate` writes; the one line that makes the check answer `packages: []`; the renumbering of R-BAS-073 is in the specification already | `internal/bastion/iface`, `internal/bastion/maintest`, `api/openapi.yaml` and `internal/api/v1/gen`, one line of `internal/api/v1/bastion_check_api.go` |
| **P2 · The decision point and the contract** | nothing (beside P1) | `PackageReachOn` and its tests (R-BAS-080); kit check 21, the kit host's recording of `MayRead`, `PathsAllowed`, `WikiReadable` and the status line, `Fixture.OtherPackage` for npm, NuGet, OCI, Debian, Ansible Galaxy and the ClamAV definitions, two `testformat` defects (R-BAS-086); fixes of any format that fails check 21 | `internal/authz` (new `reach_package.go`, `reach_test.go`), `internal/formats/formattest` (new `checks_package.go`, `host.go`, `kit.go`), `testformat`, each format's fixture file, [formats.md](formats.md), the read-shape section of [authorization.md](authorization.md) |
| **P3 · The main installation's side** | P1 and P2 | `NotePackage` in the `Absence` record; the notes in `Host.Authorize`, `WikiReadable`, `PathsAllowed` and the read scope (R-BAS-081); `ShapeResolver.PackageShapes`; `CapRegistryPackage`; the marking keeps subject and index from `Decided`, computes at the status line, applies the `shared` rule (R-BAS-082); the check's `packages` (R-BAS-083) | `internal/bastionmain` (`absence.go`, `shape.go`, `shape_resolver.go`), `internal/formathost` (`host.go`, `virtual.go`), `internal/api/bastionmark`, `internal/api/v1/bastion_check_api.go`, the marking section of [bastion.md](bastion.md#marking-the-write-probe-and-the-read-shape-package-3) |
| **P4 · The Bastion's side** | P1 (unit tests); P3 (integration) | `answers.Package` and its feed matching; `shared.AnswerPackage`; `cache.Entry.Package`; recording from markings and checks with the grant comparison (R-BAS-084); the read flow's steps 2 to 5, followers, `storedCheck` (R-BAS-085); the value `package` of the metric label `kind` (R-BAS-040); the integration and OCI end-to-end assertions | `internal/bastion/answers`, `internal/bastion/engine`, `internal/bastion/shared`, `internal/bastion/cache/cache.go` (one field), `internal/bastion/metrics`, `test/integration/bastion`, `test/e2e/oci_bastion_test.go`, the engine section of [bastion.md](bastion.md#the-engine-package-4b), [security/bastion.md](../security/bastion.md), one sizing sentence in `docs/manual/admin/bastion.md` |

The order:

- P1 and P2 run side by side, and each is safe to land alone.
- P3 does not land before P2: the marking may say `full` per package only once check 21 holds for every format.
- P3 may land before P4. The Bastion of today then shares documents filled by readers of one package only with full readers of the
  repository, which check 21 makes identical.
- P4's unit tests run beside P3; its integration and end-to-end assertions land after P3.
- P3 and P4 both edit [bastion.md](bastion.md), in different sections.
- The handover's package 1 ([bastion-handover.md](bastion-handover.md)) lands after P1 and takes version 6.

### What P1 left for P3 and P4

| Piece | Where | Who uses it |
|---|---|---|
| `Marking.Package` and `Marking.PackageRead`, written by `Marking.Set` (both headers only for a name `iface.ValidPackage` accepts, `partial` unless `PackageRead` is `full`) and read by `ParseMarking` (the five refusals of R-BAS-082) | [iface/marking.go](../../internal/bastion/iface/marking.go), [iface/package.go](../../internal/bastion/iface/package.go) | P3 sets them at the status line; P4 reads them from `mainclient.Response.Marking` (the client strips the headers already) |
| `CheckRequest.Packages`, `CheckAnswer.Packages`, both written `[]` when empty; `CheckRequest.PackagesProblem(max)` (codes `count`, `invalid_package`, `unknown_repository`, `duplicate`) | [iface/wire.go](../../internal/bastion/iface/wire.go) | the main installation's check validates with it and answers `packages: []` until P3 fills one element per pair in request order; P4 builds the pairs ([readflow.go](../../internal/bastion/engine/readflow.go), [background.go](../../internal/bastion/engine/background.go)) and may skip a pair `ValidPackage` refuses |
| `RegistryTokenGrant` and `RegistryGrant.Pulls(repository, package)`; `RegistryTokenExpiry` | [iface/regtoken.go](../../internal/bastion/iface/regtoken.go) | P4 compares a `full` package answer with the grant when it records it (Q-BAS-246) and may refuse by the expiry, which closes H-1295 (Q-BAS-252) |
| A refused pair answers `422` with the field error at `/packages/<i>` ([Q-BAS-255](../spec/bastion.md#q-bas-255--a-checks-package-that-breaks-a-rule-answers-422-as-the-checks-other-field-errors)) | [bastion_check_api.go](../../internal/api/v1/bastion_check_api.go) | P4: the client treats it as a failed check; `maintest` refuses the same pairs (`maintest.CheckMaxRepositories`, 16) |

## Tests and what they cost

By the rule "Tests stay runnable" of the repository's working rules: every behaviour once, at the cheapest tier; bytes to kilobytes. Times
marked [J] are the planner's estimates; each package writes its measured times here.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit, `internal/bastion/iface`, `internal/bastion/mainclient`, `internal/api/v1` | markings and their refusals, the check's bodies and rules, the grant and expiry readers with the real encoder, the interface file; the client against `maintest`; the check's `422` at `/packages/<i>` | P1 | measured: about 0.07 s (iface 0.01 s, mainclient 0.03 s, api/v1 0.01 s after the document the package loads anyway), race detector and atomic coverage | hundreds of bytes |
| Unit, `internal/authz` | `TestR_BAS_080_PackageShapeAgreesWithTheDecisionPoint` over the reach fixture, `TestR_BAS_080_PackageShapes` | P2 | about 0.2 s [J] | none; about 50,000 decisions in memory |
| Kit, every format | check 21; `TestR_BAS_086_KitCheck21` with the two defects | P2 | about 40 ms per format [J]; the defects about 0.1 s, joining the table that builds the fixture once | the fixture's content cases and one second package |
| Unit, `internal/api/bastionmark`, `internal/bastionmain`, `internal/formathost`, `internal/api/v1` | the notes, the marking, the grant limit, the image equal to the enforcement point's, the check per package | P3 | about 0.4 s [J] | hundreds of bytes |
| Unit, `internal/bastion/engine`, `internal/bastion/answers` | `TestR_BAS_084_PackageAnswers`, `TestR_BAS_085_AWarmPullWithANewRegistryTokenCostsOneCheck`, `TestR_BAS_085_NoRoundTripTellsACachedPackageFromAnUncachedOne`, the follower and the `content` entry, over `fakeMain` | P4 | about 0.6 s [J] | bytes |
| Integration, area `bastion` | the changed assertions of the OCI replay (`TestR_BAS_051_R_BAS_053_ADockerAndAContainerdPullThroughABastion`: one check and no conditional request on the second pull) and of the npm replay | P4 | about −0.1 s [J] (fewer requests) | existing: two blobs of 1 KiB |
| End-to-end, set `oci` | the warm assertion of `TestOCI_R_BAS_051_ThroughABastion` tightened to at most one request below `/v2/` besides the token step | P4 | 0 s | existing |

What each pipeline job gains [J]: the unit job about 1.3 s; the integration shard that holds the area `bastion` about −0.1 s; e2e `oci` 0 s;
`npm`, `bastion`, `bastion-long` and every other leg 0 s; chart and web 0 s. With 25 formats: check 21 about 1 s in all, and one
second-package seed per format; nothing else grows with the number of formats. No new end-to-end case: no client shows anything the replay
does not.

## The measurement, once

After P3 and before P4 lands, not a test: Docker, Podman and `crictl` warm pulls through a local Bastion, before and after P4, counting
`quader_bastion_main_requests_total` by `kind`. The numbers are written in
[The real clients through a Bastion](bastion.md#the-real-clients-through-a-bastion). It answers whether Docker 29 always uses `POST` with a
probe, and whether the third forward of the measured warm pull of 2026-11-22 was a version check sent with the token.

## Not verified

- **Request counts.** Derived from reading the read flow and from the measurement of 2026-11-22 on smaller images; not measured for ten
  layers.
- **Decisions before the status line.** That every format's content handlers decide all reads through the host before their status line:
  found by search in OCI, npm, NuGet and Ansible Galaxy, not traced handler by handler. Kit check 21 shows it.
- **OCI's package equals the image.** That `Target.Package` equals the enforcement point's `route.Target.Image` for every `/v2/` route, bound
  hosts and the cache view included; and that the token service names the bound repository in grant entries (read in
  [token.go](../../internal/api/registry/token.go), lines 159 to 167). Neither was run.
- **Conditionals and `Authorize`.** That `Host.Authorize` runs on the `304` path of formats that render their own documents; read only for the
  item store ([ingest.go](../../internal/formathost/ingest.go), line 191).
- **Other formats' packages.** Whether Debian's pool files and the documents of Ansible Galaxy and the ClamAV definitions name a package in
  their targets.
- **Two findings about today's code, read and not run:** the timing asymmetry of a first read by a credential without a shape answer
  ([H-1384](../spec/bastion.md#h-1384--the-first-read-of-a-cached-entry-by-a-credential-without-a-shape-answer-costs-two-requests-an-uncached-one-costs-one),
  [readflow.go](../../internal/bastion/engine/readflow.go) lines 92 to 105), and that a blocked full reader's shape answer keeps being renewed
  by the background refresh, since the check consults no limit
  ([H-1381](../spec/bastion.md#h-1381--the-main-installations-limits-and-blocks-do-not-reach-hits-from-per-package-answers),
  [background.go](../../internal/bastion/engine/background.go), [bastion_check_api.go](../../internal/api/v1/bastion_check_api.go)).
- **Lenient decoding.** That a replica of the previous release ignores the member `package` of entries
  ([sign.go](../../internal/bastion/cache/sign.go), line 93) and of Valkey records ([answerstore.go](../../internal/bastion/engine/answerstore.go),
  line 145): read, not run.
- **Cost.** The cost of computing a package shape for each response marked to a `partial` caller; the times marked [J].
- **The conservative rule.** Whether a path-pattern grant on a repository, which makes every package of it `partial`, takes the gain away from
  real installations.
- **The interface version.** That nothing on `main` uses version 5 when P1 lands: held at P1's base `e9dcbfaf0` (version 4, the handover
  specified and not built); a rebase past a commit that raises the version needs the next number.
