# Signatures on artifacts

Stage 1 of [signatures.md](../spec/signatures.md) (R-SIG-001 to R-SIG-043, [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact)
to [ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version)) is
built in four packages. This page is the contract between them: what the first package built, the types and the store API the next
ones build on, and what each format's detector hands over. The security perspective is
[security/artifact-signatures.md](../security/artifact-signatures.md).

## What has to be in your head first

- **Detect, read, verify are three steps in two places.** Detection runs in the transaction that stores the content (the commit of an
  item, the fill of a remote's document) and only names candidates; the read, the integrity check and the trust step run later in the
  worker's job (package 2).
- **A detector is pure.** `formats.SignatureAdapter.Detect` reads the bytes it is handed, bounded, and runs no SQL. What it cannot know
  (the blob of a file beside the signature, the tarball of a bundle) it names by a path of the event's repository; the core resolves it
  there and nowhere else, and drops a candidate whose path holds nothing (Q-SIG-47). The other side's commit names it again.
- **One row per fact.** An object once per its own SHA-256, a signature once per (object, part, subject, subject name), an attachment
  per (repository, signature, location). The verdict follows the bytes; what a reader sees is the attachments of the repositories it
  reads (R-SIG-003, R-SIG-004).
- **A batch replaces.** Recording a commit or a stored document removes the attachments its carrier item (or stored document) and the
  locations it names had and does not make again: a replaced file, a moved tag, a refilled document leave nothing stale.
- **A detector never fails a commit.** Its error is logged (`signatures: the detector could not read the stored content`) and the
  content is stored without candidates. A store error rolls the commit back, as every hook's does.

## Package 1 (built)

| What | Where |
|---|---|
| The contract: `Format.Signatures()`, `SignatureAdapter`, `SignatureDeclaration`, `SignatureKind`, `SignatureCandidate`, `SignatureLocator`, `SignatureEvent`, `SignatureContent`, the roles, schemes, sources and locators, `NoSignatures`; `ContractVersion` 8; `Register` refuses a format without a declaration | [internal/formats/signatures.go](../../internal/formats/signatures.go), [formats.go](../../internal/formats/formats.go) `checkSignatures` |
| Kit check 18, `Fixture.Signatures`, `formattest.ContentOf`, `Host.StoredVersions`, `Host.SignatureEventOf`; the test format's kind and its defects `SignatureVersion`, `NoSignatureDeclaration` | [checks_signatures.go](../../internal/formats/formattest/checks_signatures.go), [testformat/signatures.go](../../internal/formats/formattest/testformat/signatures.go) |
| The declaration and the detector of each format | `internal/formats/<id>/signatures.go` (below) |
| The bounded walk over OpenPGP packet headers (dearmor, count signature packets) a detector uses | [untrusted/openpgp.go](../../internal/formats/untrusted/openpgp.go) |
| The vocabulary: states, reasons, priorities, `Batch`, `Candidate`, `Object`, `Origin`, `Signature`, `Attachment`, `Result` | [internal/signatures/signatures.go](../../internal/signatures/signatures.go) |
| The store | [internal/signatures/store/store.go](../../internal/signatures/store/store.go), [queries/signatures.sql](../../internal/meta/postgres/queries/signatures.sql), migration 00092 ([database.md](database.md#signatures-on-artifacts-00092)) |
| The hooks: the item store's commit (`items.CommitEvent.Content` and `.By`), the remote's stored document (`proxy.DocumentHook`), and the format host that turns candidates into a batch | [formathost/signatures.go](../../internal/formathost/signatures.go), [formathost/hooks.go](../../internal/formathost/hooks.go) `signaturesOf`, [items/content.go](../../internal/items/content.go), [proxy/metadata.go](../../internal/proxy/metadata.go) `fillLink` |
| OCI's attachment tags get no version row (R-SIG-024, Q-SIG-32) | [oci/hooks.go](../../internal/formats/oci/hooks.go) `commitTagItem`, `oci.IsAttachmentTag` |
| The wiring: the store is the format host's `Deps.Signatures` on every role that has the database | [app/wire_formats.go](../../internal/app/wire_formats.go) |

### The commit hook

`formathost.Hooks.OnCommit` runs the format's `OnCommit` and then `signaturesOf`: for an item a client uploaded it records the origin
(`artifact_origins`: the actor's kind and name and the credential's ID as the audit row has them; the store copies the claims of the
credential's CI issuance record in the same statement, never `user_email`; Q-SIG-40, Q-SIG-52); for every item that is not `Generated`
it asks the detector and records the batch; for every item it makes due the signatures that waited for this content as their subject
(`SubjectArrived`: `pending` with reason `subject_missing` becomes due with priority 2). A remote's metadata document is no item: the
proxy's fill calls `proxy.DocumentHook.DocumentStored` in the fill's transaction after the entry took the document (not when the stored
one was kept), and the format host's adapter does the same with `Item.ID` 0 and the document's path as the carrier.

The content a detector reads (`formats.SignatureContent`, implemented by `items.Content`) is the spool of the commit when it has one, and
the object store (whole or ranged reads) for a link of content that is stored already. `Bytes(max)` refuses a content above `max`
without reading.

### Check 18

A format adds a `SignatureCase` per kind it declares: the kind's name, the path the sample is stored at, and either `Commit` (the sample
stored through the in-memory host as a client or a fill stores it; the kit builds the event from the stored item) or `Body` (a remote's
stored document). `Want` lists the candidates `Detect` must name; the kit compares source, location, part, role, subject (or subject
path, subject name), the object's path, blob and locator (its own SHA-256 and its bytes where the case sets them) and the signed content.
The roles of the declaration are held to the format's row of R-SIG-002 by `TestR_SIG_002_EveryFormatDeclaresItsRowOfTheSpecification`
([internal/formats/all](../../internal/formats/all/signatures_test.go)), which reads the specification's table: a new format adds its
row there and its cases here, a constant cost of milliseconds.

## Package 3 (built)

The formats' wire behaviour, R-SIG-023 to R-SIG-033; the decisions are Q-SIG-97 to Q-SIG-108 of the specification.

| What | Where |
|---|---|
| OCI: an attachment follows its image (delete, eviction), the background fetch and the mirror's, the version's `attestations`, the image scanning's selection | [oci.md](oci.md#signatures-on-the-wire-signaturesmd-r-sig-023-to-r-sig-027) |
| npm: the keys and the attestations endpoints of every class, `dist.attestations.url`, the background fetch of attestations and keys | [npm.md](npm.md#signatures-signaturesmd-r-sig-028) |
| Ansible Galaxy: the signatures a client posts for a local version (`addAnsibleVersionSignature`, `removeAnsibleVersionSignature`) | [ansible.md](ansible.md#signatures-a-client-posts-signaturesmd-r-sig-031) |
| Debian: the releases that cover a package (`formats.SignatureCoverage`) | [deb.md](deb.md#coverage-of-a-package-by-a-signed-release-signaturesmd-r-sig-030) |
| NuGet: no `RepositorySignatures` resource | [nuget.md](nuget.md#signatures-signaturesmd-r-sig-029) |
| The background fetch: the queue, the job `signature-fetch` (queue `index`, coalescing per target), the state `signature_fetches`, the budget per remote on the worker | [internal/signatures/fetch](../../internal/signatures/fetch/fetch.go), [wire_signature_fetch.go](../../internal/app/wire_signature_fetch.go), migration 00096 ([database.md](database.md#the-formats-wire-behaviour-of-the-signatures-00096)) |
| The `HEAD` of a pass's and a run's upstream | [mirror.md](mirror.md#signatures-in-a-pass-signaturesmd-r-sig-025) |
| The contract: `formats.SignatureFetcher` (`RemoteCaps.Signatures`), `formats.SignatureCoverage`, `Host.Signatures()` with `formats.SignatureVerdicts`, the kit's `SignatureCase.RemoveSubject` | [formats.md](formats.md#signatures-on-the-wire), [signatures.go](../../internal/formats/signatures.go) |
| The keyless facts of an OpenPGP signature packet (issuer fingerprint, hash prefix) | [untrusted/openpgp.go](../../internal/formats/untrusted/openpgp.go) `OpenPGPSignatureFacts`, `MatchesHashPrefix` |
| The store's `Detach`: an attachment a client posted and removed | [store.go](../../internal/signatures/store/store.go) |

### Where packages 2 and 3 meet

- **`Host.Signatures()`** answers `formats.NoSignatureVerdicts` until package 2 sets `formathost.Options.Verdicts` to its trust step (wired,
  below): a
  local npm provenance bundle is served to npm only when `PublicProvenance` vouches for it (valid, trusted by a shipped public anchor in the
  installation's scope).
- **The fetch's knobs** take the configuration of R-SIG-040 (wired, below): `fetch.Options.RefreshAfter`,
  `fetch.HandlerOptions.RequestsPerHour`, and the format's bounds through `fetch.HandlerOptions.RunContext` (ADR-233). The job reads the
  repository member `signatures.fetch` from the repository document (default true), which a remote repository now accepts (Q-SIG-108).
- **The fetch state for stage 3's gate** (Q-SIG-106): `signature_fetches` keeps per target `queued_at` (due or running), `fetched_at` (asked
  to the end, found or not), `attempts` and `failed_at` (`SignatureFetchFailed`, written by the handler; the last attempt marks it failed),
  and the item's digest in `subject_sha256`. The gate's builder adds the raise of a waiting job's priority and the signal after a run.
- **The audit events of a posted Ansible signature** (`ansible.signature.added`, `ansible.signature.removed`) are the format's
  (`internal/audit/events_ansible.go`, Q-SIG-107); R-SIG-039's events stay package 2's.
- **The counter** `quader_signatures_remote_requests_total` of R-SIG-039: `fetch.HandlerOptions.Observe` is told every request of a run
  with the format and `ok` or `error`; package 2 registers the metric and wires the observer (wired, below).
- **Who wakes the job `signature-verify`:** what the fetch stores passes through the commit hook and `proxy.DocumentHook`, so
  `store.Options.Wake` wakes it as for a client's fill; the posted Ansible signature records through `store.Record` with priority 0.
- **Debian's coverage** is asked by the API of R-SIG-034 through `formats.CapabilitiesFor(id).SignatureCoverage` (wired, below).
## Package 2 (built)

Verification, trust, anchors, the jobs, the API, events and metrics (R-SIG-005 to R-SIG-021, R-SIG-034, R-SIG-039 to R-SIG-041).

| What | Where |
|---|---|
| The contract between the job and the verifiers: `Input` (the row, the object, the subject with its SHA-512 and size, the signed content, the keys, the trust roots, the bounds, a blob reader), `Output` (the `Result`, the evidence, the number of parts), `Evidence` (what the trust step judges: chain, timestamps, a Sigstore bundle with its artifact digest, a keyless scheme's signature), `ErrTransient`, `ErrBlobMissing` | [scheme/scheme.go](../../internal/signatures/scheme/scheme.go) |
| The dispatch by scheme with the object bound, the check's deadline and the recovery of a panic | [scheme/all](../../internal/signatures/scheme/all/all.go) |
| `sigstore-bundle` and `cosign-legacy` (sigstore-go; the older layout converted to a bundle of version 0.1), `LoadRoot` and `Judge` for an identity anchor | [scheme/sigstore](../../internal/signatures/scheme/sigstore/sigstore.go) |
| `notation-jws`, `notation-cose` (notation-core-go, tspclient-go) | [scheme/notation](../../internal/signatures/scheme/notation/notation.go) |
| `nuget-cms`: the CMS (its own reader on `encoding/asn1`, a signer named by issuer and serial number or by its key, Q-SIG-216), the countersignatures, the timestamps, the unsigned-package hash; the real tools' signatures of the measurement in `testdata/` | [scheme/nugetcms](../../internal/signatures/scheme/nugetcms/nugetcms.go) |
| `openpgp`: the committed digest, `VerifyDigest` for a key added later, Debian's cleartext, Ansible's chain to every file | [scheme/openpgp](../../internal/signatures/scheme/openpgp/openpgp.go) |
| `npm-registry`, `public-key`, and `Verify` of a stored digest | [scheme/rawkey](../../internal/signatures/scheme/rawkey/rawkey.go) |
| What the verifiers share (refusals, the signer's certificate fields, chains, keys, bounded ASN.1 and CBOR) and the tests' certificate authority and keys | [scheme/rawkey/pki](../../internal/signatures/scheme/rawkey/pki/outcome.go), [pki/pkitest](../../internal/signatures/scheme/rawkey/pki/pkitest/pkitest.go) |
| The trust step: `Judge` of one signature in one scope under the blocks, the time of a scope, the precedence | [trust](../../internal/signatures/trust/trust.go) |
| The anchors: the material of the four kinds (the API's and the Helm values' JSON), `Validate` (the checks on save), `Parse`, the fingerprints | [anchors](../../internal/signatures/anchors/anchors.go) |
| The shipped anchors, the release's snapshot of the TUF targets, `Refresh` (go-tuf's updater from sigstore-go's embedded root), `NPMKeys`; a TUF repository for tests | [shipped](../../internal/signatures/shipped/shipped.go), [shipped/data](../../internal/signatures/shipped/data), [shipped/tuftest](../../internal/signatures/shipped/tuftest/tuftest.go); `make shipped-anchors` ([internal/tools/shippedanchors](../../internal/tools/shippedanchors/main.go)) |
| The badge rule | [badge.go](../../internal/signatures/badge.go) |
| The records of the management side, the `Signer` JSON | [records.go](../../internal/signatures/records.go), [signer.go](../../internal/signatures/signer.go) |
| The store's new half: settings, anchors, blocks, findings, views and counts, originals, the job's reads and writes, the trust rows, the trust roots, the declared anchors, the journal's replay | [store](../../internal/signatures/store/store.go): `settings.go`, `anchors.go`, `blocks.go`, `findings.go`, `views.go`, `checks.go`, `trust.go`, `roots.go`, `declared.go`, `discovery.go`, `journal_replay.go`; [queries/signature_trust.sql](../../internal/meta/postgres/queries/signature_trust.sql), migration 00098 ([database.md](database.md#signatures-trust-anchors-and-the-trust-step-00098)) |
| The jobs `signature-verify`, `signature-trust`, the task `signature-trust-root`, their metrics, the wake-ups, the verifiers' blob reader | [job](../../internal/signatures/job/job.go): `kinds.go`, `trust.go`, `metrics.go`, `blobs.go` |
| Discovery: the format host asks each stored file's format as its commit did | [formathost/discover.go](../../internal/formathost/discover.go) |
| The audit events | [audit/events_signatures.go](../../internal/audit/events_signatures.go) |
| The API | [http-api.md](http-api.md#the-operations-of-signatures-r-sig-034), [signatures_api.go](../../internal/api/v1/signatures_api.go), `signatures_views.go`, `signatures_findings.go` |
| The configuration and the chart | [config/signatures.go](../../internal/config/signatures.go), the chart's `signatures.anchors` ([_signatures.tpl](../../deploy/helm/quader/templates/_signatures.tpl)) and the three alerts; the configuration reference of [README.md](../../README.md) |
| The wiring: the area `signatures` on every role (the store, and the jobs where jobs run), `quader migrate`'s anchors | [app/wire_signatures.go](../../internal/app/wire_signatures.go), [app/migrate_signatures.go](../../internal/app/migrate_signatures.go) |

### The flow

1. A commit or a fill records candidates (package 1) and wakes `signature-verify` in its transaction.
2. The job (queue `signatures`, one at a time in the installation) runs discovery from its cursor, removes orphans, makes `failed` rows due
   once a day, then checks the due rows by priority and time with `signatures.concurrency` in flight: a row whose subject is not stored waits
   `subject_missing`; otherwise the store hands the object, the subject's SHA-512 and size, the content blob, the keys of every enabled
   anchor and the upstream keys of the repositories that attach it (a Debian remote's `deb.upstreamKeys`, an npm remote's stored
   `-/npm/v1/keys`), and the trust roots; `all.Check` runs the scheme's verifier under the deadline. A transient error spends an attempt with a
   doubling back-off from a minute; a result is written with its evidence, the parts a detector could not name are added (`AddParts`), and the
   signature's trust rows are judged at once under the current epoch; a state that turned `invalid` records `signature.invalid` once.
3. A write of an anchor, a block, the switch or a stored public root raises `signature_settings.anchor_epoch`, announces the change area
   `signature_anchors` and wakes `signature-trust`, which judges every `valid` or `no_key` signature whose rows are older than the epoch
   (`signature_trust.anchor_epoch`, the epoch a row was computed under), in batches of 10,000, from the stored fields alone: the
   installation's scope and every project's with anchors of its own (switch on) that reaches the signature. An anchor key that decides a
   `no_key` signature rewrites its integrity state.
4. The task `signature-trust-root` (daily) refreshes the public Sigstore root and npm's keys through TUF; a snapshot whose version is above
   the stored `signature_trust_roots.tuf_version_number` and the release's is stored and raises the epoch.
5. `quader migrate` applies the release's shipped anchors (an administrator's `enabled` kept) and the anchors of `signatures.anchors`.

Neither the anchor epoch, nor its copy in a trust row, nor the TUF version number is a revision column (R-ARCH-009): a restore raises none
of them, and their names stay out of the ones the schema test of the area `postgres` claims (`rev_seq`, `revision`, `epoch`, `*_version`).
Raised by 2^32, a copy of the epoch would make a trust row look current for good, and the TUF number would make the stored root look newer
than every snapshot to come ([00100_signature_trust_names](../../internal/meta/postgres/migrations/00100_signature_trust_names.sql),
[spec](../spec/signatures.md) R-SIG-041). The registered ones are the ETags `signature_settings.version` and `signature_anchors.version`.

### What package 2 wires of package 3

| Meeting point | Wired | Test |
|---|---|---|
| `formathost.Options.Verdicts` | the store's `PublicProvenance` (valid, not blocked, known by the shipped `sigstore-public` in the installation's scope) through `storeVerdicts` ([wire_signatures.go](../../internal/app/wire_signatures.go)), set on the format host while the signatures are on | the npm format's unit tests of `localAttestations` with a fake; the query in the area `signatures` is not exercised by a public bundle (none can be made in a test) |
| `fetch.HandlerOptions.Observe` | `(*job.Metrics).RemoteRequest`, the counter of R-SIG-039 registered by the jobs of the replica | — |
| The configuration of the fetch | `refresh_after` to the queue, `requests_per_hour` to the handler, `max_manifests_per_subject` and `max_object_size` to the format in the context of a run (`formats.WithSignatureFetchLimits`, ADR-233) | `TestR_SIG_026_TheFetchStoresAtMostFourManifests` |
| The repository member `signatures.fetch` | `repository.SignatureSettings`, validated in `validateSignatures` (remote only), in the document schema `RepositorySignatureSettings` | `TestR_SIG_040_TheFetchSwitchIsAMemberOfARemoteRepository` |
| Debian's coverage | `v1.SignatureServices.Coverage`, bound to `formats.CapabilitiesFor(format).SignatureCoverage` with the format's data port; the API's `covering` of a file and of a version | `TestR_SIG_030_TheCoveringReleasesOfAPackageAreListedWithTheirSignatures` |
| An npm remote's keys | the store reads the remote's stored document at `-/npm/v1/keys` (the path package 3 stores it at) as upstream keys of the integrity check | `TestR_SIG_028_ARemotesRegistrySignatureIsMatchedByTheKeysItsUpstreamAnnounces` (area `npm`) |
| Ansible's posted signature | the data port's store takes the configuration's bounds and `job.WakeVerify` (`signaturePorts.storeOptions`), so a post wakes the job | `TestR_SIG_031_ASignaturePostedForALocalVersionIsServed` (area `ansiblelocal`) |
| A format without signatures | discovery asks no format that declares none (the ClamAV definitions) and no system repository of `upstream` | `TestR_SIG_011_DiscoveryAsksNoFormatWithoutSignatures` |

### Tests and what they cost

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | every scheme's read and check with signatures made in the test (the company's Sigstore as a Fulcio, log and timestamp authority of the test, the public one as a second; the company CA made with `crypto/x509`; the release key made by go-crypto), the bounds, the trust step and its precedence, the anchors' checks, the shipped documents and the TUF refresh against a test repository, the badge rule, the job's order and decisions, the store's checks, the configuration, the events, the API's operations and permissions | about 1 s a package, about 10 s for the signatures' packages together; the API's new tests about 5 s | bytes to kilobytes; the bound tests up to 1 MiB in memory |
| Chart | the three alerts with promtool, a declared anchor's mount | about 1 s | rendered YAML |
| Integration | area `signatures`, [trust_test.go](../../test/integration/signatures/trust_test.go): the job, the trust following anchors, blocks and discovery, an invalid signature in the findings; project anchors and the switch with their permissions; the anchors of the configuration through `quader migrate` | about 13 s for the three tests (the area about 15 s with package 1's) | files of a kilobyte |
| Integration | area `backup`: the journal's replay of what narrowed trust into a restored database; area `npm`: a remote's registry signature matched by its upstream's keys; area `ansiblelocal`: a posted signature wakes the job | about 2 s, 4 s and nothing measurable (an assertion more) | rows, a recorded document |

No end-to-end leg and no client image. The integration shards gain about 13 s (`signatures`), 2 s (`backup`) and 4 s (`npm`); the unit leg
about 15 s; the chart leg about 1 s. With 25 formats the cost is the same: the core is format-neutral.

## Stage 3, package 1: the gate per repository (built)

A repository whose member `signatures.onlyVerified` is on serves an artifact only when a signature of its publisher or its build, attached
where the repository sees it, is verified by an anchor it accepts (R-SIG-061 to R-SIG-069, R-SIG-075, R-SIG-076; the decisions Q-SIG-75 to
Q-SIG-95 and Q-SIG-131 to Q-SIG-144 of the specification). Releases, the preview, the impact count, their API and the Bastion's feed are
package 2's ([below](#stage-3-package-2-releases-previews-and-the-api-built)); the screens package 3's.

| What | Where |
|---|---|
| The member: `repository.SignatureSettings.OnlyVerified` and `.Anchors`, the snapshot's `Repository.SignatureGate()`, `validateSignatures` (Debian `422 not_applicable`, the references' form, at most 32), `checkSignatureAnchors` (a reference a write adds must name an anchor of the scope, `SignatureGateAnchorRefs`), `weakenedSignatures` (`403 signature_weakening_refused` for an Owner, `block: lifted`), the part `signatures` of `repository.narrowed` and its replay | [document.go](../../internal/repository/document.go), [validate.go](../../internal/repository/validate.go), [delegated.go](../../internal/repository/delegated.go), [narrowed.go](../../internal/repository/narrowed.go), [journal_replay.go](../../internal/repository/journal_replay.go) |
| The order of a change and the replay's narrower, with no import of the module | [gateset/setting.go](../../internal/signatures/gateset/setting.go) |
| The decision of one gate (`Judge`), the replica's snapshot and the subtraction of a narrowing (`Snapshot.Subtract`, `Chosen`) | [gate/decide.go](../../internal/signatures/gate/decide.go), [gate/snapshot.go](../../internal/signatures/gate/snapshot.go) |
| The gate of the serve points: `Applies`, `Decide`, `Refuse`, `Check`, the answers, the event and the list of refused downloads, `Signal`, `Stop`; the wait (`Begin`, `Wait`); the metrics | [gate/gate.go](../../internal/signatures/gate/gate.go), [gate/wait.go](../../internal/signatures/gate/wait.go), [gate/metrics.go](../../internal/signatures/gate/metrics.go) |
| One decision, one wait of the three checks (`Serve`) | [gate/serve.go](../../internal/signatures/gate/serve.go) |
| The store's reads and writes (`GateRows`, `GateFetches`, `GateObjectsRead`, `GateSnapshot`, `GatePrioritize`, `RecordRefusal`, `ExpireRefusals`) | [store/gate.go](../../internal/signatures/store/gate.go), [queries/signature_gate.sql](../../internal/meta/postgres/queries/signature_gate.sql), migration 00102 ([database.md](database.md#the-gate-per-repository-of-the-signatures-00102)) |
| The epochs of a narrowing: `UpdateAnchor` and `quader migrate` set `narrowed_epoch`, `CreateBlock` the block's `anchor_epoch` | [store/anchors.go](../../internal/signatures/store/anchors.go), [store/declared.go](../../internal/signatures/store/declared.go), [store/blocks.go](../../internal/signatures/store/blocks.go) |
| The contract: `SignatureAdapter.Gate`, `GateKind`, `GateItem`, `GateSubject`, `GateNoneFor`, `SignatureVerdicts.Refuse`; `ContractVersion` 9; kit check 18's `GateCases`, check 17's two answers | [formats/signatures.go](../../internal/formats/signatures.go), [checks_signatures.go](../../internal/formats/formattest/checks_signatures.go), [checks_malware.go](../../internal/formats/formattest/checks_malware.go) |
| Each format's classification | `internal/formats/<id>/signatures.go` `Gate`; OCI [signatures_gate.go](../../internal/formats/oci/signatures_gate.go) with `IsAttachment` and the data port's `GateFacts` |
| The serve points: the format host's `serveChecks`, `gateDownload`, `readableMembers`, `GateClassify`, `holdFill`, `unstored`; `/files/`; the remote OCI tag | [formathost/signature_gate.go](../../internal/formathost/signature_gate.go), [formathost/malware.go](../../internal/formathost/malware.go), [files/download.go](../../internal/files/download.go), [oci/remote_handlers.go](../../internal/formats/oci/remote_handlers.go) |
| The wiring: the gate on every role (while the signatures are on), its component (the snapshot by `signature_anchors`, the subscription to `signature_verdict`, the drain), the classifier bound to the format host, the signals of the jobs and the fetch | [app/wire_signature_gate.go](../../internal/app/wire_signature_gate.go), [wire_signatures.go](../../internal/app/wire_signatures.go), [wire_formats.go](../../internal/app/wire_formats.go), [wire_files.go](../../internal/app/wire_files.go), [wire_signature_fetch.go](../../internal/app/wire_signature_fetch.go) |
| The configuration `signatures.gate.*`, the event `signature.download_refused`, the alert `QuaderSignatureGatePending` | [config/signatures.go](../../internal/config/signatures.go), [audit/events_signatures.go](../../internal/audit/events_signatures.go), [_rules.tpl](../../deploy/helm/quader/templates/_rules.tpl) |

### Where the gate sits

1. **Is it asked at all?** `Gate.Applies` reads the replica's snapshot of the repositories: the signatures are on, neither repository is a
   system repository of `upstream`, and the addressed repository's member or the holder's is on. Nothing else reads a row, so an ungated
   download is unchanged. A generated item and the image scanning's pull are never asked.
2. **What is the item?** The gate asks the item's format through the format host (`GateClassify` → `SignatureAdapter.Gate`): a carrier of
   signatures and an item that is no artifact pass; a signature file passes while its object's scheme was read (`GateObjectsRead`); an
   artifact is judged by its blob and the digests that cover it (an OCI index for its platform manifests, a `.nupkg` for its `.snupkg`).
3. **Which gates?** The addressed repository's in its view (itself, or for a virtual repository its members the caller may read) and its
   project's scope, and the holder's in its own; the first refusal names the reason, else any pending one waits.
4. **The rows.** `SignatureGateRows` reads the counting signatures (publisher, provenance) of the digests attached in the view with their
   trust row of the scope (the project's, else the installation's) and whether Quader issued the signature itself (`signatures.identity_id`
   set, `Row.Issued`); `SignatureGateFetches` the state of a remote's background fetch.
   `Snapshot.Subtract` takes from a row the anchors no longer in force or narrowed after it and applies a block created after it; a widening
   waits for the trust job.
5. **The decision.** `Judge`: a signature Quader issued itself counts for nothing (R-SIG-062); one passing signature of one digest passes;
   a pending check or a due fetch waits; else the first reason of `blocked`, `invalid`, `not_chosen`, `untrusted`, `not_checked`, else
   `missing`.

### The combined decision (R-SIG-069)

Both serve points build a `gate.Serve` from what the item lookup read: the malware gate's `Decide` (pure), the vulnerability block the
enforcer decided, and the signature gate's `Decide` (read only when neither check above it refuses). The first refusal in the order
`malware_found`, `vulnerability_blocked`, `signature_not_verified`, `malware_not_checked`, `vulnerability_data_missing` is answered at once;
a block in the mode report is recorded and serves. When something is pending, each pending check's `Begin` asks its priority work and takes
a slot of its own, and one loop waits on both wake channels, a poll and the earliest deadline; a refusal decided during the wait ends it,
and at the bound the malware gate's 503 comes first. Each check keeps its answers, events and metrics.

### The wait and its signals (R-SIG-067)

`Begin` makes the subjects' pending signatures due at priority 0 and wakes the job `signature-verify` (`SignatureGatePrioritize`), raises
the waiting job `signature-fetch` of a due target to priority 0 (`SignatureGateRaiseFetch`; an enqueue of a waiting key returns it
unchanged), and kicks the queues `signatures` and `index` through the signal area `jobs`. The job `signature-verify` publishes the subject
on `signature_verdict` after the commit of a verdict (and of a check that failed for the last time), the job `signature-fetch` after its
run's row, and the trust step `*` after each batch. Without Valkey the waiter reads again once a second. `signatures.gate.max_waiting`
bounds the slots, a drain answers every wait at once (the component's `Drain`).

### Tests and what they cost

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | the order and the replay (`gateset`); the decision table (with Quader's own signatures, which never pass), the fetch states, the subtraction, the chosen anchors, the answers, the view and scope, the wait (signal, poll, bound from the arrival, slots, drain), one decision one wait (`gate`); the member's validation, narrowing, weakening and replay (`repository`); the configuration; the event's registration | about 0.8 s (`gate`), 0.3 s each for `gateset` and the repository's cases | rows and snapshots built by the tests |
| Kit | check 18's gate cases for every format and the two defects; check 17's two answers for every format; OCI's recognition of an attachment | milliseconds per format | bytes and manifests of the tests |
| Chart | `QuaderSignatureGatePending` rendered and its `promtool` test | well under a second | rendered YAML |
| Integration | area `signatures`, [gate_test.go](../../test/integration/signatures/gate_test.go): the member and who may change it; `/files/` with the gate on (GET, HEAD, conditional), the list of refusals and the event, a block on another replica before a trust row changed, the wait and the 503 while the worker is stopped | about 11 s for the two tests | files of a kilobyte |

No end-to-end case (Q-SIG-95). The virtual view and a remote's fetch state are tested at the unit tier (Q-SIG-141). With 25 formats the
cost is 25 times the kit's milliseconds; no leg loads a client image.

## Stage 3, package 2: releases, previews and the API (built)

The exceptions of a repository's gate, what an administrator sees before a change, and the API the screens of package 3 build on
(R-SIG-070 to R-SIG-073, R-SIG-075, R-SIG-076; the decisions Q-SIG-196 to Q-SIG-205 of the specification). Everything judges with the gate's
own `Judge`: a preview, a release's check and the members are `Gate.Assess` over stored items, the impact is `Judge` under two snapshots.

| What | Where |
|---|---|
| The release at the serve point: `judge` reads `GateReleased` of each view's repository only when the signatures do not pass; `Verdict.Released` and `Verdict.Signature` say what passed | [gate/gate.go](../../internal/signatures/gate/gate.go) |
| The judgement without a wait: `Assess` (one classification per holding repository, rows per 1,000 digests, releases for what does not pass), `Preview` (batches of 500 items in the order of the live paths, the 20 newest refused as examples, `preview_limit` items or 10 s), `Impact` (`Snapshot.with` makes the snapshot of the change; candidates from `GateImpact`; per gated repository the artifacts that pass before and not after and that no release passes) | [gate/assess.go](../../internal/signatures/gate/assess.go), [gate/preview.go](../../internal/signatures/gate/preview.go), [gate/impact.go](../../internal/signatures/gate/impact.go) |
| The batch classification: `sgate.Classifier` takes many items; the format host's `GateClassify` hands them to `SignatureAdapter.Gate` at once | [formathost/signature_gate.go](../../internal/formathost/signature_gate.go) |
| The store: `GateReleased`, `GateItems`, `GateImpact`, `Releases`, `Release` (`signature.release.granted`), `Withdraw` (`signature.release.withdrawn` with its journal entry), `RefusalHolders`, `Refusals`, `BlobItem`; the replay `replayReleaseWithdrawn` | [store/releases.go](../../internal/signatures/store/releases.go), [store/journal_replay.go](../../internal/signatures/store/journal_replay.go), migration 00104 ([database.md](database.md#the-releases-of-the-signature-gate-00104)) |
| The operations: the preview, the releases, the refused downloads, the impact; `SignatureServices.Gate`, `.GateStore`, `.PreviewLimit` | [api/v1/signatures_gate.go](../../internal/api/v1/signatures_gate.go), [signatures_gate_lists.go](../../internal/api/v1/signatures_gate_lists.go), [signatures_services.go](../../internal/api/v1/signatures_services.go) |
| The members `refused`, `refusedReason` (a file, a version and its files, the version list) and `gate` (the signatures answers), each the repository's own gate (Q-SIG-203) | [signatures_gate_members.go](../../internal/api/v1/signatures_gate_members.go), [signatures_views.go](../../internal/api/v1/signatures_views.go), [signatures_versionlist.go](../../internal/api/v1/signatures_versionlist.go), `edge.SignatureView` |
| The Bastion's feed: a withdrawn release is `content` of its repository; a removed anchor, a block, an anchor switched off or losing a fingerprint, project anchors switched off are `content` of every gated repository (`BastionGatedRepositories`) and the virtual repositories above them | [bastionmain/feed.go](../../internal/bastionmain/feed.go) |
| The events' registration, the UI's labels | [audit/events_signatures.go](../../internal/audit/events_signatures.go), [audit-labels.ts](../../web/src/app/features/audit/audit-labels.ts) |

### What the screens of package 3 call

| Operation | For |
|---|---|
| `GET /api/v1/projects/{p}/repositories/{r}/signatures/preview?anchors=a,b` | the section's state line and the confirmation before a narrowing: `examined`, `artifacts`, `pass`, `pending`, `refused` per reason (`missing`, `untrusted`, `notChosen`, `invalid`, `blocked`, `notChecked`), `complete`, up to 20 `examples` (holding repository, path, package, version, reason); `anchors` absent is the stored setting, empty any anchor |
| `POST /api/v1/signatures/impact` with `{"anchor", "change"}`, `{"block": {"kind", "value", "issuer"}}` or `{"projectAnchors": false}` | the confirmation before removing, switching off or editing an anchor, blocking a signer, switching project anchors off: `artifacts`, `repositories`, up to 20 `byRepository`, `complete`, `bound` (`upper` for a change of material); ask nothing when `artifacts` is 0 |
| `GET /api/v1/signatures/refusals?repository=&reason=&cursor=` | the dashboard's **Refused downloads**: repository, format, path, package, version, sha256, reason, first and last refusal, count, the release since |
| `GET`, `POST /api/v1/projects/{p}/repositories/{r}/signatures/releases`; `DELETE …/releases/{sha256}` | **Release for this repository** (`{"sha256", "reason"}`, `201`; `409 nothing_to_release`, `409 already_released`; `404` without read or for a digest the repository does not store) and **Withdraw release** |
| the member `signatures.refused` / `refusedReason` of the items answer, a version, its files and the version list | the fact **Signature** "Refused here", no **Download**, the banner of a refused version (`pending` shows nothing more) |
| the member `gate` of `GET …/versions/{v}/signatures` per repository and of `GET …/repositories/{r}/signatures?path=` | the accepted anchors (`ref`, `title`, `kind`, `scope`, `removed`), `decision`, `reason`, `passedBy` (`signature` with its id, or `release`); absent while the repository's gate is off |

### Tests and what they cost

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | a release at the serve point and in `Assess`, the preview in batches with chosen anchors and the limit, the impact of a removal, a block, a narrowing, project anchors off and the limit (`gate`); the impact request and the preview's parameter, a version's summary (`api/v1`, no document); the feed's mapping (`bastionmain`); the events' registration (`audit`) | milliseconds each; `gate` stays under a second | rows, snapshots and items built by the tests |
| Integration | area `signatures`, [gate_releases_test.go](../../test/integration/signatures/gate_releases_test.go): R-SIG-070 to R-SIG-073 in one test with one web replica and a worker | 5.6 s | files of a kilobyte, two OpenPGP keys of the test |
| Integration | area `backup`: a withdrawn release replayed into a restored database, within `TestR_SIG_041_…` | nothing measurable | a row and an entry |

The integration shard that runs `signatures` gains about 6 s; no other leg gains anything measurable. With 25 formats the cost is the same:
nothing here is per format.

## Stage 3, package 3: the screens (built)

The screens of R-SIG-074 (decisions Q-SIG-223 to Q-SIG-232 of the specification) on the operations above; no operation and no member was
added. What each state is decided from:

| Screen | Reads | Decides |
|---|---|---|
| A repository's section **Signatures** | the member `signatures` of the document; the preview (stored anchors) when the section opens; the project's anchors, and the installation's own for `settings:read` | off, on with any anchor (the warning of Q-SIG-76), on with chosen anchors (a stored reference neither list names is **Removed trust anchor**), read-only parts for a writer without `repositories:write-config` or `vulnerabilities:triage` (the order of R-SIG-061, `gateOrder`), Debian (no controls), the checks off (the preview's `409 signatures_not_enabled`); a narrowing previews the draft (`?anchors=` the chosen references, empty for any) and asks when it refuses anything |
| A version's banner, its Files tab, a file's fact | the member `refused` and `refusedReason` of each file | a banner per refused file in the repository its content route names, "Refused here" with the reason, no **Download**; `pending` shows nothing more |
| The gate's block in a version's tab and a file's section | the member `gate` per repository; the repository's releases while `passedBy.kind` is `release` | the accepted anchors, the decision, the release (by whom, when, why) with **Withdraw release**, an image's platforms signed through its index (Q-SIG-226) |
| The dashboard's **Refused downloads** and **Releases** | the refusals with `repository` and `reason`; one repository's releases | rows with **Release for this repository** or, released, **Withdraw release**, for the holder of `vulnerabilities:triage` |
| The questions of the admin page and a project's anchors | the impact of the change | the count and the repositories when it is above 0, else the screen's own question or none (`GateActions.askImpact`); a block asks inside its dialog |
| The client setup | the repository document; the project's anchors for the titles | the line above the snippets for a writer; none for the reduced view (Q-SIG-228) |

| What | Where |
|---|---|
| The API's operations and the fake's | [signatures-api.ts](../../web/src/app/api/signatures-api.ts), [fakes-signatures.ts](../../web/src/app/api/testing/fakes-signatures.ts) |
| The words of each reason, the state line, the questions of the preview and of the impact | [gate-words.ts](../../web/src/app/shared/signatures/gate-words.ts) |
| The release's dialog, the withdrawal's question, the impact's question | [gate-release-dialog.ts](../../web/src/app/shared/signatures/gate-release-dialog.ts), [gate-releases.ts](../../web/src/app/shared/signatures/gate-releases.ts) |
| The gate's block | [signature-gate.ts](../../web/src/app/shared/signatures/signature-gate.ts), placed by [version-signatures.ts](../../web/src/app/features/packages/version-signatures.ts) and [repository-file.ts](../../web/src/app/features/repositories/content/repository-file.ts) |
| The section and the order of R-SIG-061 | [repository-signatures.ts](../../web/src/app/features/repositories/repository-signatures.ts), [signature-gate-order.ts](../../web/src/app/features/repositories/signature-gate-order.ts); saved by `saveSignatures` of [repository-detail.ts](../../web/src/app/features/repositories/repository-detail.ts) |
| The banner and the Files tab | [package-version.html](../../web/src/app/features/packages/package-version.html), [package-version.ts](../../web/src/app/features/packages/package-version.ts); the repository of a file from its content route, `repositoryOfContent` of [files-api.ts](../../web/src/app/api/files-api.ts) |
| The dashboard | [signature-refusals.ts](../../web/src/app/features/vulnerabilities/signature-refusals.ts), placed by [signature-findings.ts](../../web/src/app/features/vulnerabilities/signature-findings.ts) |
| The refusals' texts of the API | [problem-messages-signatures.ts](../../web/src/app/core/http/problem-messages-signatures.ts) (`signature_weakening_refused`, `signature_not_verified`, `signature_check_pending`, `signatures_not_enabled`); the release's dialog says `nothing_to_release` and `already_released` itself, since the malware scan's release has the same codes |
| The renders | [renders/screens.ts](../../web/e2e/renders/screens.ts): `repository-settings-signature-gate`, `setup-signature-gate`, `package-version-signature-refused`, `package-version-signature-gate-oci`, `files-signature-refused`, `security-signature-refusals`, and the states `impact` and `block-impact` of `admin-settings-signatures`; the gate off is the profile's real answer, the rest its answers changed in the browser (the profile gates nothing), and no row is written by SQL |

| Tier | What | Run time | Data |
|---|---|---|---|
| Web unit | [repository-signature-gate.spec.ts](../../web/src/app/features/repositories/repository-signature-gate.spec.ts): the section off (with axe), any anchor, chosen with a removed one, read-only for an Owner, Debian and the checks off, the question with the preview's numbers, the order; the banner of a refused file and none for a pending one, the release's dialog for its holder only; the dashboard's rows and their actions by right | eleven cases, about 0.4 s (1.6 s with the file's start) | the fake's answers |
| Web unit | the impact of a removal and of a switch-off in [signature-settings.spec.ts](../../web/src/app/features/settings/signature-settings.spec.ts) | two cases, milliseconds | the fake's answers |
| Renders | six screens and two states of a seventh: 31 states at three widths and in both schemes (186 pictures) | about 270 s of a browser (2026-11-22, shared machine), about 90 s of the job `ui-layout` at its three workers | answers changed in the browser |

## The interface for packages 2 and 3

### The candidate (`formats.SignatureCandidate`, [signatures.go](../../internal/formats/signatures.go))

| Field | Meaning |
|---|---|
| `Object` | The original: `Raw` bytes (at most `MaxSignatureRaw`), a `Blob` the detector knows by digest, or a `Path` of the event's repository; `Locator` inside the blob; `SHA256` the object's own digest when it is a part of the blob (an archive entry) |
| `Part` | The signature within the object: an OpenPGP packet, a layer of an OCI manifest, a countersignature |
| `Subject` / `SubjectPath` | The covered blob by digest, or a stored file of the event's repository whose blob it is |
| `SubjectName` | npm: `<name>@<version>` |
| `Role`, `SchemeHint` | As far as the detector can tell from what it read; the read may correct both (`Result.Role`) |
| `Source`, `Location` | The attachment: how the signature reaches its subject and where (an item path, or `path#pointer` in a document) |
| `SignedContent` | What a keyless scheme or NuGet hashes: the zero locator is the subject's bytes; `Blob`+`Locator` (`unsigned`, `cleartext`, `entry:<name>`) a view of a blob; `Raw` a message, whose SHA-256 the store keeps as `signed_digest` |

### The store (`internal/signatures/store`, [store.go](../../internal/signatures/store/store.go))

| Method | For | What |
|---|---|---|
| `Resolve(ctx, tx, repoID, paths)` | the hook | What the paths of one repository hold now: a live file's blob, size and item, or a remote's stored document |
| `Record(ctx, tx, Batch)` | the hook, and package 3's management API (Ansible's posted signatures) | Objects and signatures once, attachments replaced of the batch's own `Sources` only (the hook: the eight a detector names; the API: `upload`; none: nothing replaced, Q-SIG-217); at most `MaxPerSubject` per subject; an object above `MaxObjectSize` is born `malformed`, `limits_exceeded`, not due (Q-SIG-49); calls `Options.Wake` |
| `SubjectArrived(ctx, tx, subject)` | the hook | Makes the signatures waiting for the subject due |
| `RecordOrigin(ctx, tx, Origin)` | the hook | The origin of an uploaded item, with a CI job's claims |
| `Due(ctx, tx, limit)` | package 2's job | Locks due rows whose `not_before` passed, by priority and time, `SKIP LOCKED` |
| `SetResult(ctx, tx, id, Result)` | package 2's job | State, reason, role, predicate, signer, key IDs, signed digest, times; no longer due |
| `Defer(ctx, tx, id, spend, notBefore, due, state, reason)` | package 2's job | An attempt spent, a wait for the subject (`pending`, `subject_missing`, not due), or `failed` |
| `RemoveOrphans(ctx, tx, limit)` | package 2's job | Signatures without an attachment, then objects without a signature |
| `Object`, `Signature`, `BySubject`, `Attachments`, `AttachmentsUnder`, `Origin` | package 2's API and job | The rows of an object, a signature, a subject, a signature's attachments, a repository's attachments under a package or image name, an item's origin |

`Options` takes `MaxObjectSize` and `MaxPerSubject` (0: the defaults of R-SIG-013) and `Wake`; package 2 sets them from
`signatures.max_object_size`, `signatures.max_per_subject` and the job's wake when it adds the configuration of R-SIG-040.

The purge of a retired well-known repository (R-REPO-067 step 5) deletes the repository's `signature_attachments` as content
(migration 00093, [database.md](database.md#the-purge-of-a-retired-repositorys-signature-attachments-00093)) and leaves the signatures
and objects to `RemoveOrphans`; `artifact_origins` goes with the items it removes first.

### What each format's detector hands over

| Format | Event | Candidates |
|---|---|---|
| OCI ([signatures.go](../../internal/formats/oci/signatures.go)) | a `_manifests/` item; a `_tags/` item or a remote's tag document whose name is an attachment tag | A referrer (a manifest with `subject`): object the manifest (blob, part = the layer index), scheme and role by artifact type, layers and the predicate annotation, source `referrer`; BuildKit's attestation entries of an index: object the entry's manifest, subject `vnd.docker.reference.digest`, `index_entry`, location `path#/manifests/<i>`; a referrers tag: each listed manifest, `referrers_tag`; `.sig`: one per layer, `.att`: one per layer by predicate, `.sbom`: one attachment, all `cosign_tag`; at most 32 per event (Q-SIG-48) |
| npm ([signatures.go](../../internal/formats/npm/signatures.go)) | a remote's package document; a local publish's `.sigstore` companion; a remote's attestations document at `-/npm/v1/attestations/<name>@<version>` (Q-SIG-51) | Each `dist.signatures` entry: `Raw` the entry, subject the SHA-256 of `<name>@<version>:<integrity>`, `SubjectName`, `SignedContent.Raw` the message (Q-SIG-50); the bundle: `provenance` of `SubjectPath` the tarball, `sibling` (Q-SIG-217); each attestation: `Raw` the bundle, role by predicate type, subject path the tarball |
| NuGet ([signatures.go](../../internal/formats/nuget/signatures.go)) | a `.nupkg` item (push or fill) | The entry `.signature.p7s`: object the package blob with locator `entry:.signature.p7s` and the entry's SHA-256, part 0 with the role its commitment type gives, parts 1… the repository countersignatures; subject the package; signed content `unsigned`; `embedded` |
| Debian ([signatures.go](../../internal/formats/deb/signatures.go)) | a remote's `dists/<suite>/InRelease`, `Release.gpg`, `Release` | One per OpenPGP signature packet, `registry`, `release`: InRelease with signed content `cleartext`; Release.gpg with subject path `Release`; Release names `Release.gpg` beside it by path (its first signature; the read finds the others) |
| Ansible ([signatures.go](../../internal/formats/ansible/signatures.go)) | a remote's version document | Each `signatures[]` entry: `Raw` the armored signature, one candidate per packet, subject `artifact.sha256`, signed content `entry:MANIFEST.json`, `registry` with a `signing_service`, else `publisher`; `document` at `path#/signatures/<i>` |
| generic ([signatures.go](../../internal/formats/generic/signatures.go)) | every item | A signature file (`.asc`, `.sig`, `.sigstore.json`, `.jws.sig`, `.cose.sig`, longest suffix): object itself, subject path the file beside it, scheme sniffed from the content, one per OpenPGP packet; every file: the five signature files that may stand beside it, by path; `sibling` |
| ClamAV definitions ([signatures.go](../../internal/formats/clamav/signatures.go)) | none | `NoSignatures`: a container's signature is ClamAV's file format, checked by the malware scanning's import before the container is stored and shown as the version's verification (Q-SIG-96) |

## What the later packages build

- **Package 2** (verification and trust): built, above.
- **Package 3** (the formats' wire behaviour): built, above.
- **Package 4** (the screens): built, [web-ui.md](web-ui.md#the-signatures-screens). It added to the API the format descriptor's
  `signatureKinds` (the kinds of each format's declaration, Q-SIG-127) and made `signatures` a member an administrator sets on a well-known
  repository of `upstream` (Q-SIG-128, [upstream_managed.go](../../internal/repository/upstream_managed.go)).
- **Stage 2:** packages 1 and 2 built, below, and of package 4 the section Signing identity; package 3 and the rest of 4 are not.
- **Stage 3:** packages 1, 2 and 3 built, above; the screens of R-SIG-074 call the operations listed in
  [What the screens of package 3 call](#what-the-screens-of-package-3-call).

## Stage 2: Quader signs

Stage 2 of [signatures.md](../spec/signatures.md#quader-signs-stage-2) (R-SIG-044 to R-SIG-060, [ADR-230](../decisions.md#adr-230--quader-signs-with-one-identity-per-installation-npms-registry-signatures-and-a-receipt-attestation-of-every-artifact-signed-in-the-worker))
is built in four packages: (1) the signing identity with its chart, its API, events, metrics and alerts, and the frame of the job
`signing` (built, below); (2) npm's registry signatures of local versions and the local keys endpoint (built, below); (3) the receipt
attestations with the contract's `AttestationSubjects`; (4) the screens, of which the section **Signing identity** of the admin area is
built (below). The decisions of package 1 are Q-SIG-145 to Q-SIG-155, those of package 2 Q-SIG-156 to Q-SIG-163, those of the section
Q-SIG-170 to Q-SIG-176.

### What has to be in your head first

- **One identity per installation, over time a row per key and certificate** (`signing_identities`): `pending` (an uploaded key with its
  request), `announced` (listed before it signs), `current` (the one that signs), `previous` (signed until `expires_at`), `retired`
  (withdrawn). The moves are a table ([states.go](../../internal/signing/states.go), Q-SIG-145); a retired identity never comes back.
- **Two ways in, Helm wins.** The files of the Helm values (a Secret mounted into the worker pods alone) are read by the worker's watcher
  and registered with the source `configuration`; their private key is never stored. The admin area's key request makes a key in the
  `api` replica, seals it with the key ring under the row's id (`signing_identities.sealed_key`) and forgets it. While the configuration
  names files (`signing.identity.cert_file`), uploads, key requests and removals answer `409 signing_identity_managed`.
- **Only the worker opens a key.** `internal/signing/key` seals (for the store) and opens and reads the key file (for the job's holder);
  `internal/archtest` (`CheckSigningKey`) holds its importers to the store, the job and the composition root, and the composition root
  builds the holder and the job on the roles worker and all only.
- **Announce, then switch.** A new identity beside the current one is `announced` for `signing.identity.announce_period`; the job switches
  it in one transaction that locks every identity row exclusively. A signing transaction holds the current identity's row shared
  (`store.HoldCurrent`), so no signature is written after its identity's switch; the first identity of an installation is current at once.
- **The public parts are a snapshot.** Every replica answers the trusted root, the chain and npm's keys of a local repository from
  `store.Snapshot`, which the change area `signing_identities` refreshes.
- **A signature of Quader's is a row of stage 1's store.** npm's registry signature is a `signatures` row with `identity_id`, attached
  `issued` to the version's tarball; Quader never checks it (no trust row, never due), stage 1's batches never replace it (no batch speaks
  for `issued`, Q-SIG-217), and the documents
  render it while its identity is listed.

### Package 1 (built)

| What | Where |
|---|---|
| The vocabulary (states, sources, kinds, reasons, priorities), `Identity`, the checks of R-SIG-045 (`ParseChain`, `Chain.Check`, `Refusal`), `ParseKey`, the key request (`KeyRequest.Normalized`, `NewKey`, `CertificateRequest`), npm's keyid and keys document (`NPMKeyID`, `NPMKeys`), the trusted root and the chain (`TrustedRoot`, `ChainPEM`), the moves (`CanMove`) | [internal/signing](../../internal/signing/signing.go) |
| The private key: the sealed column, `Seal`, `Open`, `Load`, `ReadFile`, `Reseal` (the walk of `secrets-reencrypt`), `Private` that renders nothing | [internal/signing/key](../../internal/signing/key/key.go) |
| The store: the reads (`Identities`, `Overview`), the API's writes (`CreateKey`, `UploadCertificate`, `Import`, `DiscardPending`, `Remove`, `Retire`), the worker's (`RegisterFiles`, `RecordRefusal`, `ConfigurationGone`, `SwitchDue`, `HoldCurrent`), the work (`Enqueue`, `DueWork`, `LockWork`, `DoneWork`, `DeferWork`, `Backlog`, `RetryFailedDaily`, `ClaimFailedReport`, `Discovery`, `SetDiscovery`), the events and journal entries, the replay of a removal and a retirement, the snapshot | [internal/signing/store](../../internal/signing/store/store.go): `files.go`, `work.go`, `events.go`, `journal.go`, `snapshot.go`; [queries/signing.sql](../../internal/meta/postgres/queries/signing.sql), migration 00101 ([database.md](database.md#the-signing-identity-and-the-job-signing-00101)) |
| The job `signing` (queue `signing`, coalesce, task `*/10 * * * *`), `job.Kind`, the key holder, the watcher of the files, the metrics | [internal/signing/job](../../internal/signing/job/job.go): `kinds.go`, `holder.go`, `watcher.go`, `metrics.go` |
| The test CA: a root, an intermediate and code-signing leaves for P-256 keys, a leaf for a PKCS#10 request | [internal/signing/signingtest](../../internal/signing/signingtest/signingtest.go) |
| The events | [audit/events_signing.go](../../internal/audit/events_signing.go) |
| The API | [http-api.md](http-api.md#the-signing-identity-r-sig-055), [signing_api.go](../../internal/api/v1/signing_api.go) |
| The configuration and the chart | [config/signing.go](../../internal/config/signing.go); the chart's `signing.identity` ([_signing.tpl](../../deploy/helm/quader/templates/_signing.tpl), [signing-certificate.yaml](../../deploy/helm/quader/templates/signing-certificate.yaml)) and the four alerts; [helm-chart.md](helm-chart.md) |
| The wiring: the area `signing` (the store and the snapshot on every role; the holder, the watcher and the job where jobs run), the kind and the task, the re-encryption walk | [app/wire_signing.go](../../internal/app/wire_signing.go), [app/wire_api.go](../../internal/app/wire_api.go) `registerSecretsReencrypt` |

### The flow of an identity

1. **The files:** the watcher (`job.Watcher`) reads `cert_file`, `key_file` and `ca_file` at the worker's start and every
   `reload_interval`, compares their content, checks a changed pair (`ParseChain`, `key.ReadFile`, `Chain.Check`), puts its key into the
   holder (the newest and the one before it, Q-SIG-153) and calls `store.RegisterFiles`; a refused pair goes to `store.RecordRefusal`
   (once per content). A worker that starts with no file named calls `store.ConfigurationGone`.
2. **The upload:** `CreateKey` writes the pending row with the sealed key and the request; `UploadCertificate` checks the CA's
   certificate against the stored public key and installs it; `Import` checks, seals and installs in one call.
3. **Installation** (`store.installed`): `announced` beside a current identity, `current` when none is or the announce period is 0; the
   event `signing.identity.installed`, the change area, the wake-up of the job.
4. **The job** switches an announced identity once its period passed (`SwitchDue`, `signing.identity.switched`) and enqueues itself for
   the end of a period it saw; then, with signing on, a registered kind and the current key in this worker's holder, it runs discovery
   and the due work.
5. **Remove** makes the current uploaded identity previous; **retire** withdraws a previous one and drops its sealed key; both carry the
   journal flag, and the replay re-applies them after a restore.

### What the next packages call

| Need | Call |
|---|---|
| Write work in the commit's transaction (an npm publish: `WorkNPM`, priority 0, reason `upload`; an attestation subject: `WorkAttestation`, priority 1 or 2) | `(*store.Store).Enqueue(ctx, tx, kind, reason, priority, itemIDs, delay)`; it wakes the job |
| Sign one row | register a `job.Kind{Name, Sign, Discover}` in `job.Options.Kinds` ([wire_signing.go](../../internal/app/wire_signing.go)); `Sign(ctx, tx, session, work)` runs in a transaction that holds the current identity's row, with `session.Identity` and `session.Key.Signer()`; return an `internal/errs` error to spend an attempt with its code |
| Find what lacks a signature | `Kind.Discover(ctx, current, after, limit)`: write work for the items after the cursor, answer the last item id |
| Follow a change of the identities in what is served (package 2) | `Kind.Served(ctx, ids)`: called once per run before any work, whatever the identity's state and `signing.enabled` |
| Write a signature of Quader's (package 2) | `(*sigstore.Store).RecordIssued(ctx, tx, sigstore.Issued{…})` in the operation's transaction; `DetachRetiredIssued` before it for a version signed again |
| npm's keys document of a local repository | `signing.NPMKeys(identities)` over `(*store.Snapshot).Identities` |
| The identity a stored signature was made with | `signatures.identity_id` (migration 00101), counted by a retirement |
| The trusted root and the chain | `signing.TrustedRoot`, `signing.ChainPEM` (served at `/api/v1/signing/trusted-root.json` and `/chain.pem`) |

### Tests and what they cost

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | `internal/signing`: each row of R-SIG-045 with a passing and a failing sample, the state table, npm's keyid against npmjs's published keys, the keys document per state, the trusted root (one authority per chain, read back by sigstore-go), the key request, the import of three key forms; `internal/signing/key`: the seal under its own row alone, the key file; `internal/signing/job`: the order, a transaction refused after a switch or an expiry, the attempts and `signing.failed`, the frame without a kind, the watcher's load, rotation and refusal, the key that renders nothing; `internal/config`, `internal/audit`, `internal/archtest` (`CheckSigningKey`, R-SIG-014 for the signing tree), `internal/app` (the roles that build a signer); the API's permissions, shapes and public material | about 0.5 s for the three signing packages, about 1.5 s for the API's two tests | certificates and keys made by the tests (two RSA keys of 2048 bits for the refusals) |
| Chart | the Secret in the worker pods alone, the Certificate, both sources refused, nothing without one; the four alerts and their promtool tests | about 0.5 s and 1 s | rendered YAML |
| Integration | area `signing` ([signing_test.go](../../test/integration/signing/signing_test.go)): the files loaded, rotated and refused without a restart; the key request, the CA's certificate, a mismatch, an import, the switch, retire and remove, the permissions, the public material, no key material in an answer, an event or a log | 11.6 s for the package (5.8 s and 4.4 s, the rest the stack), shard `d` | certificates of a kilobyte |
| Integration | area `backup`: the replay of a removal and a retirement into a restored database | 0.7 s, shard `c` | rows |

No end-to-end leg. The integration area `signing` runs in shard `d`, with the other security checks, by the rule of Q-ARCH-565.

### Package 2 of stage 2 (built)

npm's registry signatures of local versions and the local keys endpoint (R-SIG-048 to R-SIG-050 for npm, R-SIG-054's store, R-SIG-055's
signatures answers); the decisions are Q-SIG-156 to Q-SIG-163 of the specification.

| What | Where |
|---|---|
| The message, the signature and pacote's check in Go (`NPMMessage`, `SignNPM`, `NPMSignature.Entry`, `VerifyNPM`), the identities whose signatures the documents serve (`NPMServed`) | [internal/signing/npm.go](../../internal/signing/npm.go) |
| The kind `npm`: `Sign` (the version's package locked, its other due rows taken, every unsigned version signed, the revision raised once), `Discover`, `Served` (the revision raise when the served set changes, the retired identity's versions queued) | [internal/signing/npmsign](../../internal/signing/npmsign/npmsign.go), [queries/signing_npm.sql](../../internal/meta/postgres/queries/signing_npm.sql) |
| The frame's `Kind.Served`, called once per run | [job/kinds.go](../../internal/signing/job/kinds.go), [job/job.go](../../internal/signing/job/job.go) `served` |
| The work of a publish while an identity is current (`EnqueueWhileCurrent`), the stored served set (`Served`, `SetServed`) | [store/work.go](../../internal/signing/store/work.go), [queries/signing.sql](../../internal/meta/postgres/queries/signing.sql) |
| A signature of Quader's in stage 1's store (`RecordIssued`, `DetachRetiredIssued`, `SourceIssued`); the batches and the trust job leave it alone; the views name the identity (`View.IssuedBy`) | [store/issued.go](../../internal/signatures/store/issued.go), [queries/signatures.sql](../../internal/meta/postgres/queries/signatures.sql) `SignatureReplaceAttachments`, [queries/signature_trust.sql](../../internal/meta/postgres/queries/signature_trust.sql) `SignatureTrustBatch`, `SignatureViews` |
| The documents' `dist.signatures` and the keys of a local and a virtual repository | [npm.md](npm.md#signatures-signaturesmd-r-sig-028) |
| The API's `source: "issued"` and `issuedBy` | [http-api.md](http-api.md#the-operations-of-signatures-r-sig-034), [signatures_views.go](../../internal/api/v1/signatures_views.go) |
| The tab **Signatures** of a version until the second half of the screens: an `issuedBy` entry reads "Origin confirmed by registry" with its key and carries no badge of its own (Q-SIG-1) | [signature-words.ts](../../web/src/app/shared/signatures/signature-words.ts) `entryBadgeState`; the fake `issuedNpmEntry` ([fakes-signatures.ts](../../web/src/app/api/testing/fakes-signatures.ts)) |
| The wiring: the kind on the roles that run jobs (`npmSigningKind`), the npm data port's `RegistrySigning` (`npmSigningPort`: the keys from the snapshot, the work of a publish) | [app/wire_signing.go](../../internal/app/wire_signing.go), [app/wire_formats.go](../../internal/app/wire_formats.go) |
| Migration 00103: the source `issued`, `signing_settings.served` | [database.md](database.md#npms-registry-signatures-00103) |

#### The flow of a version

1. **Publish.** The npm data port's `Publish`, in the commit's transaction, calls `RegistrySigning.Published` with the tarball's item:
   `EnqueueWhileCurrent` writes the row (`npm`, `upload`, priority 0) while `signing.enabled` is true and an identity is current, and wakes
   the job (Q-SIG-158).
2. **Sign.** The job's operation holds the row and the current identity's row shared; `npmsign.Sign` locks the package
   (`SigningNPMLockPackage`), takes the package's other due npm rows that no other operation holds (`FOR UPDATE SKIP LOCKED`, at most 999),
   signs every one of those versions that a document holds and that carries no served signature (`SigningNPMUnsigned`), removes a retired
   identity's attachment of each, records the signature (`RecordIssued`), deletes the other rows it signed, and raises the package's
   revision and `time.modified` once with the signal `metadata_changed` (Q-SIG-157).
3. **Serve.** `NPMLocalDocument` and `NPMLocalVersion` read, per version, the JSON array of the tarball's `issued` signatures of the scheme
   `npm-registry` whose identity is current or previous while one is current (Q-SIG-159); `document.prepare` sets `dist.signatures` from it.
   The keys endpoint of a local repository answers `signing.NPMKeys` of the snapshot (`npmSigningPort.Keys`); a virtual repository adds it
   once, for its first enabled local member.
4. **Follow the identities.** Every run of the job calls `npmsign.Served` before any work: the served set of `signing.NPMServed` against
   `signing_settings.served`; a difference raises every package whose versions carry a signature of an identity that joined or left, 1,000 per
   transaction, and queues a retired identity's versions (`retired`, priority 2); then the set is stored (Q-SIG-160).
5. **Discovery** (`npmsign.Discover`) walks the tarballs of the live versions of local repositories in item order and queues those without a
   served signature (`discovery`, priority 3): what was published before the identity, what a restore loaded, what a retirement left.

#### Tests and what they cost

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | `internal/signing`: a signature of `SignNPM` verified by pacote's check in Go and refused for another message, key or a cut signature; `NPMServed` per combination of states. `internal/signing/job`: `Kind.Served` once per run with signing on, off and without a current identity, its error failing the run. `internal/formats/npm`: the keys of a local repository with and without keys, a virtual repository's union in member order with a disabled local member and with remotes alone, `dist.signatures` in the full, the abbreviated and the version document, the publish handing the tarball's item. `test/npmstub`: the control call `POST /-/stub/keys` | see R-SIG-060 of the specification | keys made by the tests, documents of bytes |
| Integration | area `signing` ([npm_test.go](../../test/integration/signing/npm_test.go)): no signature and no keys before an identity; discovery signs an earlier version; a publish signed 0.33 s after its answer; the documents of `npm-local` and `npm-all` verified with pacote's check against each one's keys; the keys of `npm-all` and `npm-remote`; the signatures answer's `issued` and `issuedBy`; the gate of `npm-local` refusing the tarball Quader signed as `missing` while the document keeps the signature; a rotation, a retirement and a removal; and, in `TestR_SIG_046_…`, a publish signed with the key of the files of the Helm values | 5.3 s for the new test and under a second added to `TestR_SIG_046_…` (the area 11.6 s before, about 17 s now), shard `b` | packages of a few hundred bytes, the npm stub |
| End to end | `e2e (npm)`, [npm_signing_test.go](../../test/e2e/npm_signing_test.go): npm publishes to `npm-local` through a virtual repository of its own, a project installs it and left-pad of the test upstream, and `npm audit signatures` verifies both | see R-SIG-060 | a package of a few hundred bytes, left-pad's recorded tarball |

### Package 4: the section Signing identity (built)

The first section of **Admin area › Settings › Signatures** (R-SIG-056, first bullet; Q-SIG-70). It reads `GET /api/v1/signing/identity`
once per page load and after each write, and decides every state from that answer alone:

| State of the answer | What the section shows |
|---|---|
| `current` null, `managed` false, no `pending` | the sentence that Quader signs nothing; for `security-settings:write` **Create signing key**, and **Import key and certificate** behind **More options** |
| `pending` set | its subject, when and by whom the key was made, the request in a file window with **Copy** and **Download** (`quader-signing.csr`), the sentence about the company's CA, **Upload certificate** and **Discard** |
| `current` of the source `upload` | "Uploaded in the admin area" with who and when, the facts, **Download chain**, **Download trusted root**, **Replace** (not while a key is pending or announced) and **Remove**; **Import** behind **More options** (Q-SIG-172) |
| `current` of the source `configuration`, or `managed` | "From the Helm values" and the facts, no control; a `refused` pair as a red line with its check in plain words (Q-SIG-173) |
| `announced` set | its facts under **Announced**, with the time of its switch |
| `previous` not empty | **Previous identities**, closed, with **Retire** and the question of R-SIG-056; a stored upload under the Helm values reads "Not in use" |
| `enabled` false | a note that `signing.enabled` is false |

| What | Where |
|---|---|
| The API: the overview and the six writes, the public material's addresses | [signing-api.ts](../../web/src/app/api/signing-api.ts); the fake [fakes-signing.ts](../../web/src/app/api/testing/fakes-signing.ts) |
| The section and its facts | [signing-identity.ts](../../web/src/app/features/settings/signing-identity.ts), `.html`; placed by [signature-settings.html](../../web/src/app/features/settings/signature-settings.html) |
| The dialogs **Create signing key** (and **Replace**), **Upload certificate**, **Import key and certificate**, and the drafts they keep for a recent sign-in (Q-SIG-174) | [signing-dialogs.ts](../../web/src/app/features/settings/signing-dialogs.ts) |
| The words: the validity (amber from 14 days, `signingValidity`), the checks of R-SIG-045 in plain words (`signingCheckText`, `signingRefusalText`, `signingFieldError`), the states and sources | [signing-words.ts](../../web/src/app/shared/signatures/signing-words.ts) |
| The refusals of R-SIG-055 (`signing_identity_managed`, `signing_identity_pending`, `signing_key_mismatch`, `signing_no_pending_key`, `signing_identity_announced`, `signing_identity_exists`, `signing_identity_not_previous`, `signing_key_in_use`) | [problem-messages-signatures.ts](../../web/src/app/core/http/problem-messages-signatures.ts) |
| The renders: the screen `admin-settings-signing` (as the administrator and the auditor) with the walk `signingIdentity`, whose states are the profile's answer changed in the browser (Q-SIG-175) | [renders/signing.ts](../../web/e2e/renders/signing.ts), [renders/screens.ts](../../web/e2e/renders/screens.ts) |

| Tier | What | Run time | Data |
|---|---|---|---|
| Web unit | [signing-identity.spec.ts](../../web/src/app/features/settings/signing-identity.spec.ts): without an identity as the writer (with axe) and read-only; a pending key, its request and **Discard**; an upload with the amber validity, **Replace**, **Remove** and **Retire** with the spec's question; the red "expired" read-only; the Helm values with a refused pair; the key request's defaults and a refused chain under its field | seven cases, about 0.3 s (1.3 s with the file's start) | the fake's answers |
| Renders | `admin-settings-signing`: five states as the administrator, four as the auditor, at three widths and in both schemes (54 pictures) | 48 s and 39 s of a browser (measured on 2026-11-21 on a shared machine), about 30 s of the job `ui-layout` at its three workers | answers changed in the browser |

What follows with packages 2 and 3: Quader's npm signature and its attestation in the details of a version and a file, and the client
setup's tab **Verify** (R-SIG-056, the second and third bullets).

## Measured with real clients

A measurement, run once on 2026-11-21 against `main` at 452cd31f5 (stage 1, the signing identity of stage 2, package 1 of stage 3), as
R-SIG-042 and R-SIG-078 of the [specification](../spec/signatures.md) owe it ([Q-SIG-95](../spec/signatures.md#q-sig-95--no-end-to-end-case-one-measurement-by-hand)).
Every signature below was made by the real tool, every download was made by the real client, and what Quader showed was read from
`GET /api/v1/packages/{id}/versions/{version}/signatures` and `GET /api/v1/projects/{project}/repositories/{repo}/signatures?path=`. It
adds no test: nothing here runs in seconds. The defects it found are listed at the end of this section, most serious first.

### The setup, and how to repeat it

- **Quader:** one replica of the role `all`, built from the worktree (`go build ./cmd/quader`), on the host at `127.0.0.1:27443`, with
  `server.base_url: https://host.docker.internal:27443`; PostgreSQL, Valkey and SeaweedFS in a compose project of their own (the
  services of [deploy/ui-dev/compose.yaml](../../deploy/ui-dev/compose.yaml) with other names and ports, the certificates of
  [test/integration/certs.sh](../../test/integration/certs.sh)). `malware.enabled` and `vulnerabilities.enabled` were false, so that the
  signature gate alone decided. Quader's own listener had a certificate of a second CA with `keyUsage`: Python 3.13 refuses the
  integration stack's CA, which has none (`CA cert does not include key usage extension`), and `ansible-galaxy` runs on Python.
- **The token:** a user with the binding `admin` and a personal access token inserted the way `testenv.Stack.Token` writes one, with
  `admin: true` in its scope (without it the token has the user's rights without the administrator's).
- **The clients** ran in containers and reached Quader as `host.docker.internal`: Debian 13 with cosign, notation, crane, gpg and curl;
  the pinned images of the end-to-end sets for npm (`E2E_CLIENTS_NODE_IMAGE`), the .NET SDK (`E2E_NUGETCLIENTS_SDK_IMAGE`), Docker
  (`E2E_OCICLIENTS_DOCKER_IMAGE`, Docker in Docker with Quader's CA under `/etc/docker/certs.d/host.docker.internal:27443/ca.crt`), Ansible
  (`E2E_ANSIBLECLIENTS_CORE221_IMAGE` with `pip install ansible-core==2.21.6`) and apt (`E2E_APTCLIENTS_DEBIAN13_IMAGE`).
- **Kubernetes:** a kind cluster of one node (`DEV_KIND_NODE_IMAGE`) with a kubeconfig file of its own, whose containerd reads
  `/etc/containerd/certs.d/host.docker.internal:27443/hosts.toml` (an `extraMounts` of the CA and a `containerdConfigPatches` setting
  `config_path`), and an `imagePullSecret` with the token.
- **The test material**, made for the run and thrown away: a company CA (an ECDSA root and intermediate, an ECDSA code-signing leaf for
  cosign and Notation), a second hierarchy of RSA for NuGet (`dotnet nuget sign` refuses a leaf an ECDSA CA issued: `NU3013: The signing
  certificate has an unsupported signature algorithm`), a cosign key pair, a second cosign key pair no anchor knows, and an Ed25519
  OpenPGP release key. The anchors: the cosign public key (kind `public-key`), the two roots (kind `ca`) and the release key (kind
  `openpgp`).
- **What the client side needed** that Quader does not decide: cosign 3 signs without a transparency log only through a signing
  configuration without one (`cosign signing-config create --out nolog.json`, then `--signing-config nolog.json`; `--tlog-upload=false`
  alone is refused); `dotnet nuget sign` on Linux builds the chain against the SDK's own bundle, so the company root was appended to
  `/usr/share/dotnet/sdk/*/trustedroots/codesignctl.pem` and the intermediate added to the system store (without both:
  `NU3018: PartialChain: unable to get local issuer certificate`).

| Tool | Version |
|---|---|
| cosign | 3.1.4 (`cosign-linux-arm64`, checksum checked) |
| notation | 1.3.2 |
| crane (pushes of the test images) | 0.22.1 |
| npm | 11.20.0 on Node 26.13.0 |
| .NET SDK (NuGet client) | 10.0.403 (NuGet 7.9.0) |
| ansible-core | 2.21.6 on Python 3.13.18 |
| GnuPG | 2.4.7 |
| Docker | 29.9.3 (containerd client 2.4.2) |
| Kubernetes | kind 0.33.0, node image v1.37.0 |
| curl | 8.14.1 |

### Stage 1: what Quader showed for signatures the real tools made

"Before" is the state without the matching anchor (or with it switched off), "after" with it.

| Case | Tool | Quader before → after | Client through Quader | Judgement |
|---|---|---|---|---|
| OCI, key pair, default format | `cosign sign --key` | publisher · `sigstore-bundle` (cosign 3 writes a DSSE envelope with the predicate type `https://sigstore.dev/cosign/sign/v1` as a referrer) · `no_key`, untrusted → `valid`, **Verified · internal** | `cosign verify --key` exit 0 | works |
| OCI, key pair, the older tag scheme | `cosign sign --new-bundle-format=false --use-signing-config=false --tlog-upload=false` (all three deprecated) writes `sha256-<hex>.sig` | publisher · `cosign-legacy` · `cosign_tag` → `valid`, internal | `cosign verify --new-bundle-format=false` exit 0 | works |
| OCI, company CA chain, default format | `cosign sign --key --certificate --certificate-chain` | `malformed`, `content_unreadable`: "the Sigstore bundle is not valid: verification material cannot be X.509 certificate chain (for bundle v0.3)"; **Scheme not supported** with and without the CA anchor | `cosign verify --ca-roots --ca-intermediates` and `--trusted-root` both: `Error: no signatures found`, exit 10 | cosign 3.1.4 writes a bundle v0.3 with `x509CertificateChain`, which the bundle format forbids and neither cosign nor Quader reads; not a defect of Quader |
| OCI, company CA chain, the older format | the same with `--new-bundle-format=false` | `cosign-legacy` → `valid`, internal by the CA anchor | `cosign verify --ca-roots … --new-bundle-format=false` exit 0 | works |
| OCI, attestation | `cosign attest --key --type slsaprovenance1` | provenance · `sigstore-bundle` · predicate `https://slsa.dev/provenance/v1` → `valid`, internal | `cosign verify-attestation` exit 0 | works |
| OCI, Notation | `notation sign` (company CA) | publisher · `notation-jws` · `valid`, untrusted, signer `ci@example.com`, issuer `CN=Example Code Signing CA` → internal by the CA anchor | `notation verify` exit 0 | works |
| OCI, multi-platform index | `cosign sign` of the index digest, no `--recursive` | `valid`, internal | `cosign verify` exit 0 | works |
| OCI, remote of a local repository | crane through `docker-remote` (upstream `https://127.0.0.1:27443`) | the background fetch stored the referrers and the `.sig` (`signature_fetches.fetched_at` set, attachments in the remote); the version's view answers **none**, the manifest's file view **Verified · internal** | cosign and notation verify through the remote: the same exits as local | **defect 4** (the version's view) |
| npm, local | `npm audit signatures` | none (Quader signs nothing yet; `-/npm/v1/keys` answers `{"keys":[]}`) | `found no dependencies to audit that were installed from a supported registry`, exit 1 | as expected until stage 2 |
| npm, remote of registry.npmjs.org | `npm audit signatures` after `npm install sigstore@3.1.0` | provenance `valid`, **Verified · public** (identity of sigstore-js's release workflow); two registry signatures `valid`, public by `npm registry signing keys` | `97 packages have verified registry signatures`, `24 packages have verified attestations`, exit 0 | works |
| NuGet, author signature | `dotnet nuget sign` (company RSA CA), push | `nuget-cms` · `malformed`, `content_unreadable`: "the .signature.p7s is no CMS SignedData", with and without the CA anchor | `dotnet nuget verify --all` on the package downloaded from Quader (bytes identical): `Signature type: Author`, exit 0 | **defect 1** |
| NuGet, nuget.org through a remote | `Microsoft.Extensions.Primitives` 8.0.0 | author and repository signature both `malformed`, the same detail | — | **defect 1** |
| Ansible Galaxy | detached `gpg --armor` over `MANIFEST.json`, posted as the user manual's "Sign a collection" says (`201`) | posted right after the publish: no row in the signature store, **No signature**; posted again minutes later: publisher · `openpgp` · `upload` → `valid`, untrusted → internal by the release key | `ansible-galaxy collection install --keyring … --required-valid-signature-count +1` exit 0, `collection verify` exit 0 | **defect 3** |
| Generic, OpenPGP | `gpg --detach-sign --armor`, `tool-gpg.bin.asc` | `openpgp` · `sibling` → `valid`, untrusted → internal | the sibling with curl, then `gpg --verify`: `Good signature` | works |
| Generic, cosign | `cosign sign-blob --key --bundle tool-cosign.bin.sigstore.json` (cosign 3 has no `--output-signature` any more) | `sigstore-bundle` · `sibling` → `valid`, untrusted → internal | `cosign verify-blob --bundle` on the fetched files: `Verified OK` | works |
| Debian | `apt-get update` against `deb-local` with `signing-key.asc` as `Signed-By` | — | `Get:1 … stable InRelease [1560 B]`, exit 0 | works |

### Stage 3: real clients against the gate

Every repository but `deb-local` had `{"signatures":{"onlyVerified":true}}` with any anchor. Pending was provoked by running Quader as the
role `web` (no worker), pushing signed content, and downloading it; the same downloads were repeated after the role `all` came back.

| Case | Client | Quader answered | Client output, in short | Exit (time) | Judgement |
|---|---|---|---|---|---|
| OCI refused, unsigned | `docker pull` | 403 `DENIED`, reason `missing` | `Error response from daemon: error from registry: Quader refused unsigned/_tags/1.0 of web/docker-local: it has no signature, …` | 1 | readable; no `denied:` prefix (below) |
| OCI refused, untrusted signer | `docker pull` | 403, `untrusted` | `… no trust anchor this repository accepts knows its signer. …` | 1 | readable |
| OCI refused, unreadable signature | `docker pull` of `chain` | 403, `not_checked` | `… its signature could not be checked. …` | 1 | readable; a reader cannot tell that cosign wrote an invalid bundle |
| OCI passing | `docker pull` of `key`, `notation`, `tagscheme`, `att`, `chainlegacy` | 200 | `Status: Downloaded newer image` | 0 | works |
| OCI multi-platform, signed at its index | `docker pull multi:1.0` | the index and then `GET …/manifests/sha256:be6f…` (the arm64 child) 200 | pulled, `arm64` | 0 | works: the child passes on the index's signature (R-SIG-063) |
| OCI remote | `docker pull` through `docker-remote` | signed image served, unsigned refused with the remote's name in the text; a signed image's first pull through the remote waited for the fetch and was served | as local | 0 / 1 (1 s) | works |
| OCI pending | `docker pull pend:1.0` | `HEAD` 503 after 25 s, retried once | `Error response from daemon: unknown: failed to resolve reference …: unexpected status from HEAD request to …: 503 Service Unavailable` | 1 (50 s) | **unclear to a reader** (defect 5) |
| OCI, verify a refused image | `cosign verify --key` of `blobs` (signed by a key no anchor knows) | by tag: 403 on the tag; by digest: the referrers served | by tag: the `DENIED` text, exit 1; by digest: `The signatures were verified against the specified public key`, exit 0 | 1 / 0 | works: the signatures stay fetchable; the digest is needed |
| OCI, sign in a gated repository | `cosign sign`, `notation sign` of a pushed, unsigned image by digest | 403 on the client's read of the manifest before it signs | cosign: `accessing image: GET …: DENIED: Quader refused …`; notation: `failed to get manifest descriptor: HEAD …: response status code 403: Forbidden` | 1 | **defect 2** |
| Kubernetes refused | kubelet (containerd 2) | 403 | event `Failed`: `failed to resolve reference "…/unsigned:1.0": unexpected status from HEAD request to …: 403 Forbidden` then `denied: Quader refused unsigned/_tags/1.0 of web/docker-local: it has no signature, …`; pod `ImagePullBackOff` | — | readable |
| Kubernetes passing | kubelet | 200 | `Successfully pulled image "…/key:1.0"` | — | works |
| npm refused | `npm install sigm-unsigned@1.0.0` (npm-local) | 403 | `npm error code E403` … `- Quader refused sigm-unsigned/-/sigm-unsigned-1.0.0.tgz of web/npm-local: it has no signature, …` | 1 | readable |
| npm refused, a dependency of the public registry | `npm install sigstore@3.1.0` through `npm-remote` | 403 for `color-name-1.1.4.tgz` | `… it has no signature, …` although Quader itself shows npm's registry signature of that version | 1 | **misleading** (defect 6) |
| npm pending | `npm install @sigstore/protobuf-specs@0.3.2` through `npm-remote` (fetch not run) | 503 three times, 25 s each | `npm error code E503` … `Quader is checking the signatures of … Try again in a minute.` | 1 (146 s) | readable; npm retries twice |
| npm after the decision | the same | 200 | `added 1 package in 5s` | 0 | works |
| NuGet refused | `dotnet restore` of `Sigm.Unsigned` | 403 | `Response status code does not indicate success: 403 (Forbidden).` five times, then `error : Failed to download package 'Sigm.Unsigned.1.0.2'` | 1 (7 s) | **unclear to a reader** (defect 5) |
| NuGet with an author's signature | `dotnet restore` of `Sigm.Signed` (signed, CA anchor present) | 403, `not_checked` | as refused | 1 | **defect 1**: cannot pass |
| NuGet pending | `dotnet restore` of `Sigm.Pend` | 503 six times, 25 s each | `503 (Service Unavailable)` five times, then `The feed … lists package 'Sigm.Pend.1.0.0' but multiple attempts to download the nupkg have failed. The feed is either invalid or required packages were removed …` | 1 (160 s) | **misleading** (defect 5); NuGet retried at once each time, not after `Retry-After: 30` |
| Ansible refused | `ansible-galaxy collection install sigm.unsigned` | 403 | `[ERROR]: Failed to download collection tar from 'quader': HTTP Error 403: Forbidden` | 1 | **unclear to a reader** (defect 5) |
| Ansible passing | `sigm.signed:1.0.0` (the signature posted the second time) | 200 | `sigm.signed:1.0.0 was installed successfully` | 0 | works |
| Ansible, signature posted right after the publish | `sigm.signed:1.0.1`, `sigm.pend:1.0.0` | 403, `missing` | `HTTP Error 403: Forbidden`, while the version document still lists the signature | 1 | **defect 3** |
| Ansible pending | `sigm.pend:1.0.0` while no worker ran | 503 once | `HTTP Error 503: Service Unavailable`, no retry | 1 (25 s) | **unclear to a reader** (defect 5) |
| Generic | `curl` | 200 for `tool-gpg.bin`, its `.asc` and `tool-cosign.bin`; 403 and 503 below | — | 0 / 22 | readable |
| Debian | `PATCH` of `deb-local` | 422 `validation_failed`, pointer `/signatures/onlyVerified`, `not_applicable`: "apt checks the signature of a Debian repository's index, not of a package: the gate is not offered" | — | — | readable |

The answers of `/files/`, as curl prints them:

```text
HTTP/2 403
cache-control: no-store
content-type: application/problem+json
{"code":"signature_not_verified","detail":"Quader refused tool-unsigned.bin of web/files-internal: it has no signature, and this
repository serves only artifacts with a verified signature. Details: https://host.docker.internal:27443/ui/projects/web/repositories/
files-internal/files/tool-unsigned.bin","instance":"/files/web/files-internal/tool-unsigned.bin","reason":"missing","requestId":"…",
"sha256":"6020ef54…","status":403,"title":"Signature not verified","type":"/api/v1/problems/signature_not_verified"}

HTTP/2 503
cache-control: no-store
content-type: application/problem+json
retry-after: 30
{"code":"signature_check_pending","detail":"Quader is checking the signatures of tool-pend.bin. Try again in a minute.",
"instance":"/files/web/files-internal/tool-pend.bin","requestId":"…","sha256":"8662a944…","status":503,"title":"Signature check pending",
"type":"/api/v1/problems/signature_check_pending"}
```

Docker and the kubelet on the same refusal:

```text
$ docker pull host.docker.internal:27443/web/docker-local/unsigned:1.0
Error response from daemon: error from registry: Quader refused unsigned/_tags/1.0 of web/docker-local: it has no signature, and this
repository serves only artifacts with a verified signature. Details: https://host.docker.internal:27443/ui/projects/web/repositories/
docker-local/files/unsigned/_tags/1.0

Failed  Failed to pull image "host.docker.internal:27443/web/docker-local/unsigned:1.0": failed to pull and unpack image "…": failed to
        resolve reference "…": unexpected status from HEAD request to https://host.docker.internal:27443/v2/web/docker-local/unsigned/
        manifests/1.0: 403 Forbidden
        denied: Quader refused unsigned/_tags/1.0 of web/docker-local: it has no signature, …
```

The details link answers `302` to `/ui/en/projects/…`; whether the page there shows the file was not looked at in a browser.

### Defects found

1. **No real NuGet signature is readable.** Reproduce: `dotnet nuget sign <pkg> --certificate-path <pfx>` with SDK 10.0.403, push to a
   local NuGet repository; or fetch `Microsoft.Extensions.Primitives` 8.0.0 through a remote of nuget.org. Expected: the author's signature
   `valid`, trusted by the CA anchor (nuget.org's by the shipped anchors), and a gated repository serves the package. Actual: every
   signature, the countersignature of nuget.org included, is `malformed` with "the .signature.p7s is no CMS SignedData"; the badge reads
   **Scheme not supported**, and a gated repository refuses every signed package with `not_checked`. Cause, confirmed with a probe of
   `pkcs7.Parse` on both files: their `SignerInfo` is of version 3 and names its signer by `subjectKeyIdentifier` (RFC 5652, section
   5.3), and `github.com/digitorus/pkcs7` parses only `issuerAndSerialNumber` (`asn1: structure error: tags don't match … issuerAndSerial
   @5`). The place: `parse` in [nugetcms/cms.go](../../internal/signatures/scheme/nugetcms/cms.go) and the `signerInfo` it fills; the
   unit tests sign with `issuerAndSerialNumber` only, which is why they pass.
   **Repaired:** `nugetcms` reads `ContentInfo`, `SignedData`, every `SignerInfo` (the signer by issuer and serial number or by subject key
   identifier, a certificate without the extension by the SHA-1 of its key) and the timestamp's `TSTInfo` itself on `encoding/asn1`, BER
   brought to definite lengths by `pki.Definite` first, the signature checked over the signed attributes as encoded
   ([Q-SIG-216](../spec/signatures.md#q-sig-216--quader-reads-nugets-cms-itself-and-a-signer-named-by-its-key)); `nugetcms` imports
   digitorus no more. With that, nuget.org's countersignature of 2023 read `weak_algorithm`: its chain ends at a root self-signed with SHA-1
   in 2006, and `pki.CheckCertificate` no longer judges a self-signed certificate's own signature
   ([Q-SIG-218](../spec/signatures.md#q-sig-218--a-self-signed-roots-own-signature-is-not-judged)). The two signatures of this measurement
   are the fixtures of [realtool_test.go](../../internal/signatures/scheme/nugetcms/realtool_test.go) (31 KiB): `dotnet nuget sign`'s package
   reads `valid`; nuget.org's author signature and countersignature each find their certificate, chain, verify and carry DigiCert's verified
   timestamp, and the whole check reaches the package's hash (the 205 KiB package is not kept). The old parser refuses both fixtures with the
   error above (probed once). Why the vectors did not catch it: every NuGet signature of the tests was made in process by digitorus/pkcs7,
   the library that read them, which writes the one form it reads; no signature came from dotnet or nuget.org. The tests' own signatures now
   also name every signer by its key. Not measured again with the real client: a gated repository serving `Sigm.Signed`, and the trust
   row of nuget.org's countersignature under the shipped anchors. A row the defect already wrote stays `malformed` in an installation:
   nothing reads it again (the trust job recomputes trust rows only, discovery's insert keeps an existing row's verdict, the daily retry
   takes `failed` rows only), and storing the same package again reaches the same row; only after every attachment of it is gone and the
   job's removal of orphans has deleted the row does the next store read the signature anew.
2. **In a gated OCI repository no client can sign an image after the push.** Reproduce: gate on `docker-local`, push an unsigned image,
   `cosign sign --key k <repo>/<image>@<digest>` or `notation sign` of the same. Expected (R-SIG-064: "OCI's signatures arrive after the
   push"): the signature is pushed and the image passes. Actual: cosign `GET`s and notation `HEAD`s the manifest before signing, the gate
   refuses that read (`missing`), and both exit 1; the image can only be signed while the gate is off. The place: the gate judges every
   manifest read alike, whoever asks ([formathost/signature_gate.go](../../internal/formathost/signature_gate.go) `serveChecks`, OCI's
   [handlers_manifest.go](../../internal/formats/oci/handlers_manifest.go)); what should pass (a writer's read of a manifest by digest, any
   `HEAD`, or a writer's read at all) is a decision of the specification.
   **Repaired** (Q-SIG-211): a principal that may write (`deploy`) to the repository reads through its signature gate where the format's
   signing tools read the artifact, which OCI declares (`SignatureDeclaration.ReadToSign`); the format host passes the right as
   `Download.MayWrite` and the gate asks it only after a decision that is no pass (`Gate.writerRead`); the malware gate and the vulnerability
   block are not opened. Shown by a unit test and by an integration assertion (`HEAD` and `GET` by digest with the pusher's token in
   `TestR_SIG_070_…`), not by the real tools again ([H-1229](../security/artifact-signatures.md#h-1229--a-holder-of-the-write-right-pulls-unverified-images-of-a-gated-oci-repository)).
3. **A signature posted for a new Ansible version disappears from the signature store.** Reproduce: publish a collection, post its
   `MANIFEST.json.asc` at once (the manual's CI flow), wait for the worker. Expected: the posted signature is checked and stays attached.
   Actual: the post answers `201` and the version document keeps serving it to `ansible-galaxy`, but its attachment, signature row and
   object are gone within seconds: the version shows **No signature** and a gated repository refuses it with `missing`. A signature posted
   after the worker's discovery passed the archive stays (seen for `sigm.signed` 1.0.0). Cause, read in the code and matching what was
   seen: discovery ([formathost/discover.go](../../internal/formathost/discover.go), `recordAt` with `fresh` false) records an empty
   batch for the archive's item, and `SignatureReplaceAttachments` ([queries/signatures.sql](../../internal/meta/postgres/queries/signatures.sql))
   deletes every attachment whose `item_id` is that item, the posted one included, which
   [ansible/signatures_store.go](../../internal/formatdata/ansible/signatures_store.go) `record` attaches with the archive's `ItemID`;
   `RemoveOrphans` then removes the row and the object. Since discovery starts again from the first item once a day
   ([Q-SIG-111](../spec/signatures.md#q-sig-111--discovery-walks-the-items-by-a-cursor-and-starts-a-new-pass-once-a-day)), every
   posted signature is lost at the latest at the next pass (inferred, not waited for).
   **Repaired:** a batch names the sources it speaks for (`signatures.Batch.Sources`) and `SignatureReplaceAttachments` removes attachments
   of those sources only; a detector's batch (`formathost.recordAt`, so a commit, a fill and discovery) speaks for the eight sources a
   detector names, the posted signature's for `upload`, and `formats.SignatureSourceValid` refuses `upload` from a detector. npm's provenance
   bundle, the one detector candidate that was `upload`, is attached `sibling`
   ([Q-SIG-217](../spec/signatures.md#q-sig-217--a-batch-replaces-the-attachments-of-its-own-sources-only)). The formats that attach from
   more than one writer are Ansible (the detector's `document` in a remote, the management API's `upload` in a local repository) and npm
   (the detector's `sibling` and `document`, and stage 2's `issued`); every other format attaches from its detector alone. Tested in
   `ansiblelocal` by running the worker's discovery and the removal of orphans in process after the post (the attachment, its row and its
   object stay; with `upload` among a detector's sources the test fails, run once), and at the unit tier in `internal/formathost`. A
   version whose posted signature an installation already lost stays without it until the signature is posted again.
4. **A remote OCI image's version lists no signature.** Reproduce: pull a signed image through an OCI remote and read
   `GET /api/v1/packages/{id}/versions/1.0/signatures?repository=web/docker-remote`. Expected: the fetched signatures and **Verified ·
   internal**. Actual: `"state":"none"` with no signature, while the manifest's file view of the same repository shows them verified. The
   place: `versionSubjects` in [signatures_views.go](../../internal/api/v1/signatures_views.go) adds the tag's digest only when the
   repository holds the tag as a file, and a remote holds its tags as documents (R-OCI-022), so the version has no files and no subject.
   **Repaired:** `SignatureVersionBlobs` also answers the blob of the document a repository lists a version by
   (`package_version_listing.document_sha256`: a remote's OCI tag, whose document is the manifest), marked `Document`; the version's answer
   reads it for all holders in one call (`versionDocuments`) and a holder whose document names the metadata's manifest asks for it and its
   platforms, one whose tag names another manifest asks for that one; the version list counts it the same way
   ([Q-SIG-219](../spec/signatures.md#q-sig-219--a-remotes-version-reads-the-document-it-holds-the-version-as)). The other formats' remotes
   (npm, NuGet, Ansible Galaxy, Debian) hold a version's file as an item and showed their signatures already; the generic format has no
   remote class; a virtual repository shows its members' union, a remote member now included. Tested at the unit tier
   (`TestR_SIG_034_VersionSignatures`) and in `ociremote` (`TestR_SIG_026_…`, the version's answer and its list after the fetch).
5. **NuGet and `ansible-galaxy` never show the gate's text, and Docker does not show it for a pending image.** `dotnet restore` prints
   `Response status code does not indicate success: 403 (Forbidden).` (also with `-v detailed`, where the body never appears), retries a
   403 and a 503 five times, and after the 503s ends with "The feed is either invalid or required packages were removed"; `ansible-galaxy`
   prints `HTTP Error 403: Forbidden` or `HTTP Error 503: Service Unavailable`; Docker asks a tag with `HEAD`, whose 503 has no body. A
   reader learns that the download failed, not why, and the NuGet text points the wrong way. Expected (R-SIG-068): the client shows the
   text. A way for NuGet that was not tried: the header `X-NuGet-Warning`, which NuGet's client prints for nuget.org's notices (whether it
   prints it on an error answer is not verified). Docker 29.9.3 also retries the pending `HEAD` once, so its pull waits twice the bound
   (50 s) before it fails without the text.
   `X-NuGet-Warning` was not tried with the pinned SDK: not cheap enough for the repair of 2026-11-21, so nothing is sent (Q-SIG-214).
6. **The text of `missing` is wrong where a registry signature exists.** `color-name` 1.1.4 through `npm-remote` is refused with "it has
   no signature" while Quader's own view of it shows npm's registry signature verified (and `npm audit signatures` counts it verified). The
   same holds for a nuget.org package without an author's signature. Expected: the text says what is missing, "no signature of its
   publisher or its build". The place: the texts of `gate.Gate.refusal` ([gate/gate.go](../../internal/signatures/gate/gate.go)).
   **Repaired** (Q-SIG-212): every reason's text speaks of the signature of the publisher or the build (`missing`: "it has no signature of its
   publisher or its build"; the others likewise), in `reasonText`.
7. **The texts name Quader's storage paths**, not what the client asked for: `unsigned/_tags/1.0` for `unsigned:1.0`,
   `chain/_manifests/sha256:…` for a digest. Readable, but `_tags` and `_manifests` are words of Quader's file view, not of the client.
   **Repaired** (Q-SIG-213): the format names the artifact (`SignatureDeclaration.ArtifactName`), the format host hands it to the gate as
   `Download.Name`: OCI `unsigned:1.0` or `chain@sha256:…`, npm `color-name@1.1.4`, NuGet `sigm.unsigned 1.0.2` (the id as stored, in lower
   case), Ansible Galaxy `sigm.unsigned:1.0.0`; a generic file and `/files/` keep the path, which is the client's own. The details link keeps
   the stored path.

What the repairs of 1, 3 and 4 cost in tests (measured on 2026-11-21 in the unit job's mode, `-race -covermode=atomic -coverpkg=./...`,
load 14 to 16 on 20 cores):

| Tier | Test | Run time | Data |
|---|---|---|---|
| Unit | `nugetcms`: the real tools' two signatures (`TestR_SIG_029_TheSignatureDotnetNugetSignWritesIsValid`, `…NugetOrgsAuthorSignatureAndRepositoryCountersignatureRead`), the signers named by key in DER and BER (`TestR_SIG_006_ASignerNamedByItsKeyIdentifierIsFound`) | 0.02 s, 0.02 s and 0.03 s | the fixtures, 31 KiB; packages of the test |
| Unit | `trust`: the `dotnet nuget sign` package internal under its CA (`TestR_SIG_029_TheSignatureDotnetNugetSignWritesIsInternalUnderItsCA`) | under 0.01 s | the fixture, 7.5 KiB |
| Unit | `formathost`: a detector's batch speaks for the detectors' sources (`TestR_SIG_004_…`); `api/v1`: two cases of a remote's tag in `TestR_SIG_034_VersionSignatures` | under 0.01 s each | none |
| Unit (fuzz seeds) | `FuzzSignatureP7s` (`nugetcms`: the parse and every step of a check on each part) and `FuzzDefinite` (`pki`: the conversion to definite lengths is DER in its tags and lengths, nests at most 32 deep, grows by at most 64 bytes, is a fixed point); R-SIG-013 lists the bounds they assert | 0.04 s and 0.02 s | the fixtures and the tests' own signatures; six short BER structures and one kept input |
| Fuzz (closing run only) | the same two targets in the `fuzz nuget` matrix, 30 minutes a leg; run here two minutes each on 2026-11-22 (8 workers, `-fuzzminimizetime 5s`, load 15 to 25): 565,507 executions of the CMS target and 7,407,794 of `Definite`, no finding of the readers; `Definite`'s first run found a flaw of the target's own walk (an empty constructed value at the deepest level counted as one level deeper), whose input stays as `testdata/fuzz/FuzzDefinite/ca72bc74dfb10117` | two legs of 30 minutes | none kept besides the input |
| Integration | `ansiblelocal`: discovery and the removal of orphans in process after a post; `ociremote`: the remote's version and its list after the fetch | about 0.1 s (0.09 s measured); two requests and a wait of at most 10 s for the tag's download to be counted, in existing tests (`TestR_SIG_026_…` took 42.6 s whole at load 15 to 30, its share not isolated) | rows; the stub's image |

No leg gains a second the unit run would notice; the shard with `ociremote` at most the 10 s wait, the one with `ansiblelocal` 0.1 s; no
end-to-end leg anything; with 25
formats the same: the rules are the core's.

### Not measured, and why

- **Keyless signing** (cosign with Fulcio and an OIDC token, npm provenance): it needs an interactive sign-in or a signing service of its
  own; no way around either was found. npm's passing case under the gate rests on the public provenance of `sigstore` and
  `@sigstore/protobuf-specs`, which the public registry serves.
- **Symbol packages of NuGet, a virtual repository's gate, chosen anchors, a blocked signer and a narrowing at the gate** with a real
  client: the gate's decision is the same for every client and is tested at the unit and integration tiers; only the answers' shapes were
  in question here.
- **Notation's COSE envelope** (`--signature-format cose`) and Notation's plugins; **Podman**; **APT under the gate** (not offered).
- The malware gate's refusals: not provoked (the scanning was off). The user manual's line on Docker for them was changed with this
  measurement because the OCI error body is the same writer's (R-SIG-068); that line is inferred, not seen.
- Verified, not only read: every row of both tables. Read, not run: the cause of defect 3 (the code path), the daily repetition of
  discovery, and whether `X-NuGet-Warning` would reach a NuGet user. The cause of defect 3 was run since, with the repair: discovery in
  process removes the posted signature when a detector's batch speaks for `upload`, and keeps it when it does not.

## What this page does not cover

- A project that starts to reach a checked signature gets its own trust row only at the next recomputation
  ([H-1202](../security/artifact-signatures.md#h-1202--a-project-that-starts-to-reach-a-signature-is-served-the-installations-trust-until-the-next-recomputation)).
- A format added later (ClamAV's definitions, [formats/clamav.md](../spec/formats/clamav.md)) declares its kinds and adds its row to the
  table of R-SIG-002 when it registers.
- After a retirement, a cache outside the replica keeps a document whose signature names a key the keys endpoint dropped
  ([H-1204](../spec/signatures.md#h-1204--after-a-retirement-a-cached-document-carries-a-signature-whose-key-the-keys-endpoint-no-longer-lists)),
  and until the job's next run two renditions of a document can share a validator
  ([H-1205](../spec/signatures.md#h-1205--between-a-change-of-the-identities-and-the-jobs-next-run-two-renditions-share-a-validator)).
