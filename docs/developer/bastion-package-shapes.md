# Read shapes per package at a Bastion: the build plan

Status: specified on 2026-11-22; **all four packages built**: P1 (the wire), P2 (the decision point and the contract) and P3 (the main
installation's side) on 2026-11-22, P4 (the Bastion's side) on 2026-11-23 ([bastion.md](../spec/bastion.md#read-shapes-per-package) R-BAS-080 to R-BAS-088,
Q-BAS-240 to Q-BAS-255, Q-BAS-260, Q-BAS-261, Q-BAS-265 and Q-BAS-270, the gaps H-1380 to H-1384, H-1390, H-1395 and H-1242; H-1295 closed;
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
| **P2 · The decision point and the contract** — **built** | nothing (beside P1) | `PackageReachOn` and its tests (R-BAS-080); kit check 21, the kit host's recording of `MayRead`, `PathsAllowed`, `WikiReadable` and the status line, `Fixture.OtherPackage` for npm, NuGet, OCI, Debian, Ansible Galaxy and the ClamAV definitions, two `testformat` defects (R-BAS-086); fixes of any format that fails check 21 | `internal/authz` (new `reach_package.go`, `reach_test.go`), `internal/formats/formattest` (new `checks_package.go`, `host.go`, `kit.go`), `testformat`, each format's fixture file, [formats.md](formats.md), the read-shape section of [authorization.md](authorization.md) |
| **P3 · The main installation's side** — **built** on 2026-11-22 | P1 and P2 | `NotePackage` in the `Absence` record; the notes in `Host.Authorize`, `WikiReadable`, `PathsAllowed` and the read scope (R-BAS-081); `ShapeResolver.PackageShapes`; `CapRegistryPackage`; the marking keeps subject and index from `Decided`, computes at the status line, applies the `shared` rule (R-BAS-082); the check's `packages` (R-BAS-083) | `internal/bastionmain` (`absence.go`, `shape.go`, `shape_resolver.go`), `internal/formathost` (`host.go`, `virtual.go`), `internal/api/bastionmark`, `internal/api/v1/bastion_check_api.go`, the marking section of [bastion.md](bastion.md#marking-the-write-probe-and-the-read-shape-package-3) |
| **P4 · The Bastion's side** — **built** on 2026-11-23 | P1 (unit tests); P3 (integration) | `answers.Package` and its feed matching; `shared.AnswerPackage`; `cache.Entry.Package`; recording from markings and checks with the grant comparison (R-BAS-084); the read flow's steps 2 to 5, followers, `storedCheck` (R-BAS-085); the value `package` of the metric label `kind` (R-BAS-040); the integration and OCI end-to-end assertions | `internal/bastion/answers`, `internal/bastion/engine`, `internal/bastion/shared`, `internal/bastion/cache/cache.go` (one field), `internal/bastion/metrics`, `test/integration/bastion`, `test/e2e/oci_bastion_test.go`, the engine section of [bastion.md](bastion.md#the-engine-package-4b), [security/bastion.md](../security/bastion.md), one sizing sentence in `docs/manual/admin/bastion.md` |

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

### What P3 left for P4

| Piece | Where | What P4 relies on |
|---|---|---|
| The two headers on every `200` or `304` of a `Download`, `Metadata` or `Computed` route whose decisions named one package, `Package-Read` limited to a registry token's grant at the main installation | [mark.go](../../internal/api/bastionmark/mark.go) (`marking`, `packageRead`, `classOf`) | a marking whose `Quader-Bastion-Repository` is the request's names the entry's package; `Package-Read: full` never comes for a registry token on an image outside its grant, but the Bastion still compares with the grant it reads (Q-BAS-246) |
| A document marked `shared` for a caller `partial` on the repository and `full` on the package | `classOf` | the Bastion of today stores it as the shared entry; P4's package answer is what lets that caller hit it without a conditional request |
| The check's `packages`: one element per pair, in request order, `partial` for a repository read none of, `[]` for an unusable subject | [bastion_check_api.go](../../internal/api/v1/bastion_check_api.go) (`checkPackages`) | `storedCheck` and the read flow's step 4 can index the answer by position |
| No package on a response that drew on two packages or on a package-less decision: an official image of a Docker Hub remote without a namespace prefix carries none ([H-1395](../spec/bastion.md#h-1395--an-official-image-of-a-docker-hub-remote-without-a-namespace-prefix-names-two-packages-and-carries-none)) | [absence.go](../../internal/bastionmain/absence.go) | P4's assertions of a warm pull use an image with a namespace, or a remote with a namespace prefix |

### P4 as built

| Piece | Where | What it does |
|---|---|---|
| `answers.Package`, rule 3 | [answers.go](../../internal/bastion/answers/answers.go) `FeedLog.matches`, [answerstore.go](../../internal/bastion/engine/answerstore.go) `entryMatcher` | a package answer is matched by `token`, `principal` and `grants` entries, not by `content` entries |
| The store | `answerStore.pkg`, `packageKey`, `shared.AnswerPackage` | memory key `package\|<credential key>\|<repository>\|<package>`, Valkey `ans:v1:package:<sha256>`; the record carries the package only in its hashed key |
| Recording | [packages.go](../../internal/bastion/engine/packages.go) `putPackage`, called by `recordAnswers` ([forward.go](../../internal/bastion/engine/forward.go)) and `recordCheck` ([background.go](../../internal/bastion/engine/background.go)) | only for a marking of the request's repository, only while the capped shape is `partial`; `capPackage` ([registry.go](../../internal/bastion/engine/registry.go)) compares a registry token's `full` with its grant; the expiry is the earlier of the reported one and the claims' `exp` |
| The entry's package | `entryPackage` in [forward.go](../../internal/bastion/engine/forward.go); `storeDocument`, `spoolEntry`, both `304` paths | set by the filling `200`, replaced by every `200` or `304`, cleared by a marking without one |
| The read flow | `fromEntry`, `checkShape`, `storedCheck`, `applyCheck` in [readflow.go](../../internal/bastion/engine/readflow.go) | steps 2 to 6 of R-BAS-085; the check collapses per credential, repository and package; `storedCheck` rebuilds the package answer with its own time (`checkResult.packageAt`) |
| Followers | `followFill`, `fillRun.repo`, `fillRun.pkg` in [fill.go](../../internal/bastion/engine/fill.go) | a `full` package answer on the leader's package lets a caller follow the running fill |
| The expiry of a registry token | `registryTokenExpired` in [registry.go](../../internal/bastion/engine/registry.go) | `iface.RegistryTokenExpiry` of the token itself; `engine.expiries` and `noteRegistryToken` are gone (Q-BAS-252, H-1295 closed) |
| The metric | `metrics.AnswerPackage` | the value `package` of `kind` of `quader_bastion_answers` and `quader_bastion_answers_total` |

What the build changed in the design: a package answer ends together with the shape answer it was obtained with, so its renewal is the check
of step 4 more often than the conditional request R-BAS-084 named, and the check consults no limit
([Q-BAS-270](../spec/bastion.md#q-bas-270--a-package-answer-that-ended-with-its-shape-answer-is-renewed-by-the-check), H-1381 widened).

## Tests and what they cost

By the rule "Tests stay runnable" of the repository's working rules: every behaviour once, at the cheapest tier; bytes to kilobytes. Times
marked [J] are the planner's estimates; each package writes its measured times here.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit, `internal/bastion/iface`, `internal/bastion/mainclient`, `internal/api/v1` | markings and their refusals, the check's bodies and rules, the grant and expiry readers with the real encoder, the interface file; the client against `maintest`; the check's `422` at `/packages/<i>` | P1 | measured: about 0.07 s (iface 0.01 s, mainclient 0.03 s, api/v1 0.01 s after the document the package loads anyway), race detector and atomic coverage | hundreds of bytes |
| Unit, `internal/authz` | `TestR_BAS_080_PackageShapeAgreesWithTheDecisionPoint` over the reach fixture (26 subjects, 3 endpoints, 16 keys, 4 packages), `TestR_BAS_080_PackageShapes`; `TestR_BAS_012_ShapeAgreesWithTheDecisionPoint` now asks the wiki too, skips a partial reach, stops at the first refutation and asks a version and a path in pairs (`packageReads`) instead of their cross product | P2 | measured in the unit job's mode at a load of 22 to 35: 0.26 s and 0.01 s; the R-BAS-012 test 2.65 s, 6.69 s before on the old fixture, so the package gains about 3.8 s | none; decisions in memory |
| Kit, every format | check 21; `TestR_BAS_086_KitCheck21` with the two defects and five fixture faults | P2 | measured in the unit job's mode (race detector, atomic coverage) on a host at a load of 45 to 52: npm 0.25 to 0.63 s, NuGet 0.02 s, OCI 0.01 to 0.05 s, Debian 0.13 to 0.16 s, Ansible Galaxy 0.07 to 0.23 s, the ClamAV definitions 0.01 to 0.03 s, testformat 0.05 s, generic skips; `TestR_BAS_086_KitCheck21` 0.66 s beside the fixture check 19's test builds | the fixture's content cases, each seeded once more with one second package |
| Unit, `internal/api/bastionmark`, `internal/bastionmain`, `internal/formathost`, `internal/api/v1` | the notes, the marking, the grant limit, the image equal to the enforcement point's, the check per package | P3 | measured in the unit job's mode (race detector, atomic coverage) at a load of 6 to 14: `bastionmain` 0.00 s (three tests), `formathost` 0.00 s, `bastionmark` 0.35 s (the registry root through `registry.Register` with a real signer 0.34 s, the marking 0.01 s), `api/v1` 0.71 s for the first check test, which builds the package's OpenAPI document every test of the package builds anyway, and 0.02 s for the other two; about 0.4 s in all | hundreds of bytes; a blob body of 4 bytes |
| Integration, areas `bastion` and `bastioniface` | the package headers of the real main installation in `TestR_BAS_013_TheMarkingContractAtTheWire` (the real decision point: `$PART` is `full` on `@acme/sdk` through `npm-local`, `partial` through `npm-all`); a Bastion without answers per package serving `$FULL` the shared entry `$PART` filled (`TestR_BAS_022_R_BAS_024_NegativeCacheOnlyForFullReaders`); the check's pairs of `$PART` (`TestR_BAS_011_ACheckAnswersTheSubjectAndTheShapes`) | P3 | two to four requests more inside existing tests and worlds, milliseconds | existing |
| Unit, `internal/bastion/engine`, `internal/bastion/answers` | `TestR_BAS_084_PackageAnswers` (both packages), `TestR_BAS_084_TheEntryCarriesThePackageOfItsLastMarking`, `TestR_BAS_085_AnEntryIsServedOnAFullAnswerForItsPackageOnly`, `TestR_BAS_085_AWarmPullWithANewRegistryTokenCostsOneCheck`, `TestR_BAS_085_NoRoundTripTellsACachedPackageFromAnUncachedOne`, the `content` entry, the follower and `storedCheck`, over `fakeMain` (a registry double of one repository, `packages_test.go`); the case of `TestR_BAS_052_RegistryTokensAtTheBastion` for a replica that never saw the token | P4 | measured in the unit job's mode (race detector, atomic coverage): the eight engine tests 0.16 s together (the largest, the warm pull of a tag and 11 blobs, 0.03 s), the answers test 0.00 s, the case of R-BAS-052 0.02 s with the test around it | bytes: blobs of 32 to 64 bytes, one of 128 KiB for the follower |
| Integration, area `bastion` | the changed assertions of the OCI replay (`TestR_BAS_051_R_BAS_053_ADockerAndAContainerdPullThroughABastion`: one check and no conditional request on the second pull, `besidesTheToken`) and of the npm replay (`TestR_BAS_022_R_BAS_024_NegativeCacheOnlyForFullReaders`: `$PART`'s second `npm ci` through `npm-local` sends nothing; through `npm-all` a tarball costs one conditional request answered `304`) | P4 | the area 159.9 s in all, as before within the noise; the OCI replay sends two requests fewer per warm pull, the npm assertions add two `npm ci` replays of one package and two tarball reads, milliseconds | existing: two blobs of 1 KiB, the fixture's npm packages |
| End-to-end, set `oci` | the warm assertion of `TestOCI_R_BAS_051_ThroughABastion` tightened to no request about an artifact (`noArtifactRequest`, Q-BAS-271: the planned "at most one request below `/v2/` besides the token step" does not hold for the real clients, whose anonymous challenge, tag `HEAD`, referrers and version check the package answers are not about), on the warm Docker pull, the Podman pull and the kubelet's pull, the cold pull after `ociFeedQuiet` (16 s after the push or the replica's readiness, whichever is later) | P4 | the wait, 16 s | existing |

What each pipeline job gains [J]: the unit job about 1.3 s (P2 measured: check 21 over seven formats 0.7 to 1.0 s and `TestR_BAS_086_KitCheck21`
0.66 s, against 3.8 s fewer in `internal/authz`, so the unit job loses about 2 s; P3 measured about 0.4 s more, most of it the registry root's
test, since `api/v1` builds its document anyway); the integration shard that holds the area `bastion` about −0.1 s (P3: a few requests more,
milliseconds); the shard of `bastioniface` one check more (P3); e2e `oci` 0 s;
`npm`, `bastion`, `bastion-long` and every other leg 0 s; chart and web 0 s. With 25 formats: check 21 about 1 s in all [J]; measured, a
format costs check 21 what it costs check 19 plus one seed of its second package per content case, 0.01 to 0.6 s, so 2.5 to 3.6 s in all for 25
formats at the average of the seven (0.10 to 0.15 s); nothing else grows with the number of formats. No new end-to-end case: no client shows anything the replay
does not. P4 measured: the unit job about 0.2 s more (the engine's new tests 0.16 s, the case of R-BAS-052 0.02 s); the integration shard of
the area `bastion` nothing measurable (the area 159.9 s); e2e `oci` about 16 s more, the wait for the feed of Q-BAS-271; every other leg,
chart and web 0 s. With 25 formats P4 adds nothing: its tests are the engine's, which knows no format.

## The measurement, once

After P3 and before P4 lands, not a test: Docker, Podman and `crictl` warm pulls through a local Bastion, before and after P4, counting
`quader_bastion_main_requests_total` by `kind`. The numbers are written in
[The real clients through a Bastion](bastion.md#the-real-clients-through-a-bastion). It answers whether Docker 29 always uses `POST` with a
probe, and whether the third forward of the measured warm pull of 2026-11-22 was a version check sent with the token.

*Ran on 2026-11-23* on a kind cluster of its own, an image of ten layers: Docker 29.9.3 16 → 4, Podman 5.8.9 13 → 2, crictl with containerd
2.3.4 19 → 6. Docker takes its token by `POST` with the probe on every pull; the third forward was Docker's `HEAD` of the tag without a
credential, which the Bastion passes on while anonymous reads are on
([H-1420](../spec/bastion.md#h-1420--while-the-main-installation-admits-anonymous-reads-a-clients-first-head-of-a-tag-without-a-credential-is-forwarded-on-every-pull)).

## Not verified

- **Request counts.** Measured once on 2026-11-23 (above), for the three clients and one image; the derivation's 3 for Docker and the
  kubelet assumed the Bastion answers the anonymous challenge itself and serves the tag's `HEAD` from the cache, which a cluster with a public
  repository and a client that only `HEAD`s the tag do not give. The kubelet was not measured apart from crictl on the same containerd.
- **A download's one statement with a package shape.** R-ARCH-027: computing the package shape adds no database statement. Shown by
  construction, not by a test: `PackageReachOn` runs on the request's in-memory index and takes no transaction runner; the area `querybudget`
  has no case through a recognized Bastion request, and none was added.
- **What a Bastion of today does with the new marking.** That it keeps a `shared` document a reader of one package filled and serves it to a
  full reader without forwarding it is run in `TestR_BAS_022_R_BAS_024_NegativeCacheOnlyForFullReaders`; that the reader of one package
  keeps sending conditional requests answered `304` is read in [readflow.go](../../internal/bastion/engine/readflow.go) and
  [forward.go](../../internal/bastion/engine/forward.go) (`onShared`), not asserted.
- **Decisions before the status line.** Kit check 21 shows it for every content case of the six formats with package names on a local
  repository; the remote and virtual handlers and the routes in `NoContent` were read by a scan for a decision after a write, not run
  ([H-1390](../spec/bastion.md#h-1390--kit-check-21-runs-over-the-local-class-only)).
- **OCI's package equals the image.** Run for the blob route by P3 (`TestR_BAS_081_TheImageTheHandlersAuthorizeIsTheEnforcementPointsImage`:
  an unbound host, a nested name, a bound host, the cache view of a remote repository, each through `registry.Register` with tokens of a real
  signer); the manifest, tags-list and referrers routes and the local, remote and virtual handlers other than the local and remote blob were
  not run. That the token service names the bound repository in grant entries is read in [token.go](../../internal/api/registry/token.go),
  lines 159 to 167, not run; the test mints the grant with the bound repository itself.
- **Conditionals and `Authorize`.** That `Host.Authorize` runs on the `304` path of formats that render their own documents; read only for the
  item store ([ingest.go](../../internal/formathost/ingest.go), line 191). For OCI's local repository it was seen on the wire on 2026-11-23
  (a forward made by hand as a Bastion's on the kind cluster): the `HEAD` and `GET` of a tag, the manifest by digest and a blob, `200` and
  `304` alike, carry `Quader-Bastion-Package` with the image and `Package-Read: full` for a registry token of the image.
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
  line 145): read, not run. A replica of the previous release that renews an entry writes it without the member, which clears the package:
  fails closed (the next read of a package reader is a conditional request).
- **Valkey with package answers.** The shared store's path of package answers is run with the local shared store
  (`TestR_BAS_084_PackageAnswers`, "replica A serves on B"); the Valkey form of `shared.AnswerPackage` (the key family `ans:v1:package:`) is
  the same code as the other kinds and was not run with a Valkey of its own.
- **Cost.** The cost of computing a package shape for each response marked to a `partial` caller (one `PackageReachOn` of one package at the
  status line; P2 measured microseconds per call); the times marked [J].
- **The conservative rule.** Whether a path-pattern grant on a repository, which makes every package of it `partial`, takes the gain away from
  real installations.
- **The interface version.** That nothing on `main` uses version 5 when P1 lands: held at P1's base `e9dcbfaf0` (version 4, the handover
  specified and not built); a rebase past a commit that raises the version needs the next number.
