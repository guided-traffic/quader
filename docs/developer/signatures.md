# Signatures on artifacts

Stage 1 of [signatures.md](../spec/signatures.md) (R-SIG-001 to R-SIG-043, [ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact)
to [ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version)) is
built in four packages. This page is the contract between them: what the first package built, the types and the store API the next
ones build on, and what each format's detector hands over. The security perspective is
[security/artifact-signatures.md](../security/artifact-signatures.md).

## What has to be in your head first

- **Detect, read, verify are three steps in two places.** Detection runs in the transaction that stores the content (the commit of an
  item, the fill of a remote's document) and only names candidates; the read, the integrity check and the trust step run later in the
  worker's job (package 2). Nothing in package 1 checks a signature.
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

- **Package 2** (verification and trust): `internal/signatures/{scheme,trust,anchors,shipped,job}`, the libraries of R-SIG-014, the
  tables of the anchors, blocks, settings, trust roots and trust rows (a migration of its own), the configuration of R-SIG-040, the job
  and its wake through `store.Options.Wake`, discovery of items without attachments, the API, events and metrics.
- **Package 3** (the formats' wire behaviour): OCI's attachment tags following their image (R-SIG-025), the mirror and the background
  fetch (R-SIG-026), npm's keys and attestations endpoints (storing the attestations document at the path of Q-SIG-51), Ansible's
  management operation (recording through `store.Record` with `source` `upload`), Debian's coverage lookup.
- **Package 4**: the screens.

## What this page does not cover

- The read of the parts a detector could not see: a Debian `Release` named before its `Release.gpg` names part 0 only, and a referrers
  tag names part 0 of each listed manifest; the read of package 2 adds the others.
- A format added later (ClamAV's definitions, [formats/clamav.md](../spec/formats/clamav.md)) declares its kinds and adds its row to the
  table of R-SIG-002 when it registers.
