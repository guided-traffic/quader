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
| `nuget-cms`: the CMS, the countersignatures, the timestamps, the unsigned-package hash | [scheme/nugetcms](../../internal/signatures/scheme/nugetcms/nugetcms.go) |
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
package 2's; the screens package 3's.

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
| `Record(ctx, tx, Batch)` | the hook, and package 3's management API (Ansible's posted signatures) | Objects and signatures once, attachments replaced; at most `MaxPerSubject` per subject; an object above `MaxObjectSize` is born `malformed`, `limits_exceeded`, not due (Q-SIG-49); calls `Options.Wake` |
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
| npm ([signatures.go](../../internal/formats/npm/signatures.go)) | a remote's package document; a local publish's `.sigstore` companion; a remote's attestations document at `-/npm/v1/attestations/<name>@<version>` (Q-SIG-51) | Each `dist.signatures` entry: `Raw` the entry, subject the SHA-256 of `<name>@<version>:<integrity>`, `SubjectName`, `SignedContent.Raw` the message (Q-SIG-50); the bundle: `provenance` of `SubjectPath` the tarball, `upload`; each attestation: `Raw` the bundle, role by predicate type, subject path the tarball |
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
- **Stage 2:** package 1 built, below; packages 2 to 4 are not.
- **Stage 3, package 2**: releases (`signature_releases`, passed to `gate.Judge` as `released`), the preview (`formats.SignatureAdapter.Gate` in
  batches and `gate.Judge` without the wait), the impact count (the trust rows the change subtracts from, `Snapshot.Subtract`), their API
  and the members `refused` and `gate`, the read of `signature_refusals`, the Bastion's feed. Stage 3, package 3: the screens.

## Stage 2: Quader signs

Stage 2 of [signatures.md](../spec/signatures.md#quader-signs-stage-2) (R-SIG-044 to R-SIG-060, [ADR-230](../decisions.md#adr-230--quader-signs-with-one-identity-per-installation-npms-registry-signatures-and-a-receipt-attestation-of-every-artifact-signed-in-the-worker))
is built in four packages: (1) the signing identity with its chart, its API, events, metrics and alerts, and the frame of the job
`signing` (built, below); (2) npm's registry signatures of local versions and the local keys endpoint; (3) the receipt attestations with
the contract's `AttestationSubjects`; (4) the screens, of which the section **Signing identity** of the admin area is built (below). The
decisions of package 1 are Q-SIG-145 to Q-SIG-155, those of the section Q-SIG-170 to Q-SIG-176.

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
- **The public parts are a snapshot.** Every replica answers the trusted root and the chain (and, from package 2, npm's keys) from
  `store.Snapshot`, which the change area `signing_identities` refreshes.

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
| npm's keys document of a local repository | `signing.NPMKeys(identities)` over `(*store.Snapshot).Identities` |
| The identity a stored signature was made with | `signatures.identity_id` (migration 00101), counted by a retirement |
| The trusted root and the chain | `signing.TrustedRoot`, `signing.ChainPEM` (served at `/api/v1/signing/trusted-root.json` and `/chain.pem`) |

### Tests and what they cost

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | `internal/signing`: each row of R-SIG-045 with a passing and a failing sample, the state table, npm's keyid against npmjs's published keys, the keys document per state, the trusted root (one authority per chain, read back by sigstore-go), the key request, the import of three key forms; `internal/signing/key`: the seal under its own row alone, the key file; `internal/signing/job`: the order, a transaction refused after a switch or an expiry, the attempts and `signing.failed`, the frame without a kind, the watcher's load, rotation and refusal, the key that renders nothing; `internal/config`, `internal/audit`, `internal/archtest` (`CheckSigningKey`, R-SIG-014 for the signing tree), `internal/app` (the roles that build a signer); the API's permissions, shapes and public material | about 0.5 s for the three signing packages, about 1.5 s for the API's two tests | certificates and keys made by the tests (two RSA keys of 2048 bits for the refusals) |
| Chart | the Secret in the worker pods alone, the Certificate, both sources refused, nothing without one; the four alerts and their promtool tests | about 0.5 s and 1 s | rendered YAML |
| Integration | area `signing` ([signing_test.go](../../test/integration/signing/signing_test.go)): the files loaded, rotated and refused without a restart; the key request, the CA's certificate, a mismatch, an import, the switch, retire and remove, the permissions, the public material, no key material in an answer, an event or a log | 11.6 s for the package (5.8 s and 4.4 s, the rest the stack), shard `b` | certificates of a kilobyte |
| Integration | area `backup`: the replay of a removal and a retirement into a restored database | 0.7 s, shard `c` | rows |

No end-to-end leg. The integration area `signing` joins shard `b` by the rule of Q-ARCH-544.

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

## What this page does not cover

- A project that starts to reach a checked signature gets its own trust row only at the next recomputation
  ([H-1202](../security/artifact-signatures.md#h-1202--a-project-that-starts-to-reach-a-signature-is-served-the-installations-trust-until-the-next-recomputation)).
- A format added later (ClamAV's definitions, [formats/clamav.md](../spec/formats/clamav.md)) declares its kinds and adds its row to the
  table of R-SIG-002 when it registers.
