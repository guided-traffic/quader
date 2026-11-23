# Signatures on artifacts

Status: all three stages built. Stage 1: the detectors, the store and the hooks that record candidates and origins, the read and the
integrity check of every scheme, the trust step, the anchors with the switch and the blocks, the jobs and the API are built, and so is the formats' wire
behaviour (an OCI attachment follows its image, the background fetch of a remote's signatures, npm's keys and attestations endpoints,
the signatures a client posts for a local Ansible Galaxy version), and so are the screens ([developer/signatures.md](../developer/signatures.md)); stage 1 of [signatures.md](../spec/signatures.md) (R-SIG-001 to R-SIG-043,
[ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact) to
[ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version)) and stage 2,
Quader signs (R-SIG-044 to R-SIG-060,
[ADR-230](../decisions.md#adr-230--quader-signs-with-one-identity-per-installation-npms-registry-signatures-and-a-receipt-attestation-of-every-artifact-signed-in-the-worker)),
and stage 3, the gate per repository (R-SIG-061 to R-SIG-078,
[ADR-231](../decisions.md#adr-231--a-repository-can-serve-only-artifacts-with-a-verified-signature-a-gate-at-download-for-every-format-judged-from-stored-verdicts-and-failing-closed)).
Of stage 2 the identity (package 1), npm's registry signatures (package 2), the receipt attestations (package 3) and the screens
(package 4: the identity's section of the admin area and Quader's own signatures at a version, a file and an image) are built; of stage 3
the gate itself (package 1), releases, the preview and the impact count (package 2) and the screens (package 3) are built.
The perspective of this page is what a verdict about a signature can be made to say, by whom, and what reading a hostile signature container
can do to the process that reads it; for stage 2, what Quader's own signature makes others believe, who holds the key that makes it, and
what a holder of that key can do; and for stage 3, what a repository's gate keeps out, who can open it, and what it does when it cannot
decide. Stages 1 and 2 refuse no download; stage 3 refuses downloads from the repositories whose gate is on.

## The boundary

A signature object is input of whoever may push to a repository, of whoever runs an upstream a remote fronts, and of whoever can change a
document a remote stores. Quader reads it in the worker, which holds the installation's database, object-store and Valkey credentials and
the key ring (the owner's decision, [Q-SIG-5](../spec/signatures.md#q-sig-5--where-and-when-verification-runs)), unlike the scanners, which
open whole artifacts with third-party tools in pods without secrets ([image-scanning.md](image-scanning.md),
[malware-scanning.md](malware-scanning.md)).

```
 client push, remote fill, mirror pass, background fetch
        │ commit: the format names what it carries (no crypto, no SQL)          screens, API, security dashboard
        ▼                                                                               ▲
 item store ──commit hook──▶ signature rows (pending) ──job, worker──▶ read · integrity check · trust step
                                                                   bounded parsers;      stored chain, key IDs,
                                                                   content read once     committed digest
                                                                                ▲
                                     anchors (shipped · Helm · administrator · project) and blocks ──▶ trust job (no artifact read)
```

What runs before the worker, in the process that commits (the `packages` or `api` role, or the worker for a mirror pass): the format's
detector, which reads only what it needs of the committed bytes, bounded (a manifest or a document up to the format's document limit, a
NuGet package's central directory and its signature entry of at most 256 KiB, an OpenPGP signature's packet headers), parses JSON
through `internal/formats/untrusted` and ASN.1 with the standard library's memory-safe decoder, checks nothing cryptographic, and never
fails a commit: a detector's error is logged and the content is stored without candidates (Q-SIG-47). The paths a candidate names are
resolved in the event's repository only, so a signature in one repository can never name content of another.

The worker's exposure is kept small by three things the specification fixes: the bounds of R-SIG-013 sit outside the libraries (an object
of at most 256 KiB, chains of eight certificates, JSON scanned for depth and count before any decoder, ASN.1 and CBOR nesting of at most 32
levels, a check bounded to 5 s, a panic recovered per check, [all.go](../../internal/signatures/scheme/all/all.go)); the libraries are
memory-safe Go (R-SIG-014), reached only from `internal/signatures` (`TestR_SIG_014_OnlyTheSignaturesTreeImportsTheVerificationLibraries` in
[internal/archtest](../../internal/archtest/signatures_test.go)); and the content a keyless scheme or NuGet reads is streamed from the blob
store and hashed, never written to disk or parsed beyond what the scheme needs.

The trust step reads no content at all: it judges the evidence the check stored in `signatures.evidence` (the chain as carried, the
timestamps that matched, a Sigstore bundle with the digest it was checked against, a keyless scheme's signature; [Q-SIG-109](../spec/signatures.md#q-sig-109--the-trust-step-judges-the-evidence-the-check-stored)),
so an anchor change, a block or a restore never reads an artifact. That evidence is as trustworthy as the worker that wrote it and the row
that holds it, exactly as the committed digest of a keyless scheme is.

## Who decides what reads as trusted

| Party | What it can make Quader show | What stops it |
|---|---|---|
| A holder of `deploy` on a repository | an attachment of any signature to content of that repository: a valid one by any signer (shown as its trust allows), an invalid one, an unreadable one; for a local Ansible Galaxy version through the management API, a signature whose hash prefix matches `MANIFEST.json` (one of other content is refused at once) | an invalid one never lowers a better badge ([Q-SIG-25](../spec/signatures.md#q-sig-25--an-artifacts-state-is-the-best-of-its-signatures-and-an-invalid-one-alone-shows-invalid)); at most 32 read per subject; it alarms ([H-1172](#h-1172--a-writer-can-attach-signatures-that-raise-alarms-and-fill-the-dashboard)) |
| An upstream of a remote | the signatures its documents and referrers carry, for the remote's readers | the integrity check against the content Quader stored; trust only through anchors the installation chose |
| A project's Owner, while the administrator's switch is on | **Verified · internal** for signatures its anchors know, for every reader of the project's repositories and of what its virtual repositories serve | anchors only add; the administrator lists and removes them; off again, they stop applying ([H-1167](#h-1167--a-project-owners-anchor-is-shown-to-every-reader-of-the-projects-repositories)) |
| An administrator (`security-settings:write`) | trust for the whole installation: anchors, switching shipped ones off, blocks, the switch | audit events; removals and blocks carry the journal flag, so a restore does not bring back withdrawn trust |
| The release | the shipped anchors: the public Sigstore root, npm's keys, Debian's and Ubuntu's keyrings, nuget.org's certificates and timestamping roots ([shipped/data](../../internal/signatures/shipped/data)) | each can be switched off; the public root and npm's keys are refreshed through TUF from the root sigstore-go embeds, a target the chain does not sign refused ([H-1170](#h-1170--without-internet-the-public-sigstore-root-is-the-releases-snapshot)) |
| The operator of the Helm values | anchors of the scope `configuration`, applied by `quader migrate` | read-only in the API (`409 anchor_managed`); the material is public, mounted from ConfigMaps into the migrate Job only, never from a Secret (R-SIG-018) |
| A writer of `deb.upstreamKeys` | which releases a Debian remote stores (as built) | not what reads as trusted: those keys are no anchors ([Q-SIG-31](../spec/signatures.md#q-sig-31--debians-upstream-keys-stay-the-remotes-condition-and-are-no-anchor)) |

## What a verdict rests on

- **The integrity check is a fact of bytes**, made once per signature object and subject and shown in every repository that carries an
  attachment (R-SIG-003, R-SIG-004). A signature attached in one repository is never shown at the same bytes in another repository, whose
  readers may not read the first.
- **Time.** A certificate is judged at the time a verified log entry or a timestamp of a trusted authority proves, else at Quader's receipt
  (R-SIG-007, [Q-SIG-41](../spec/signatures.md#q-sig-41--a-time-proof-counts-only-from-a-log-or-an-authority-the-scope-trusts)): a
  timestamp of an authority the scope does not trust moves no expiry.
- **A keyless scheme** (OpenPGP, npm's registry signature, a raw key) keeps the digest its signature commits to, computed by Quader over the
  content it stored (R-SIG-006, [Q-SIG-23](../spec/signatures.md#q-sig-23--a-keyless-scheme-keeps-the-digest-its-signature-commits-to)); a key
  added later checks against it. The digest is as trustworthy as the worker that computed it and the database row that holds it.
- **The original is kept** in `signature_objects` (bounded) or as the item that carries it, so a re-judgement needs no upstream.

## Quader's own signature (stage 2)

Quader signs with one identity per installation: an ECDSA P-256 key and a code-signing certificate of the company's CA (R-SIG-045). It
makes npm's registry signature of every version of a local npm repository (R-SIG-050) and a receipt attestation of every artifact a user's
repository receives (R-SIG-051 to R-SIG-054). Debian's index signatures keep their own per-repository keys
([deb-signing.md](deb-signing.md)).

**What is built** (all four packages, [developer/signatures.md](../developer/signatures.md#stage-2-quader-signs)): the identity with
its states, its two ways in, its checks, the announce and the switch under the lock, removal and retirement, the trusted root and the chain,
the events, metrics and alerts, the frame of the job `signing`, npm's registry signature of every version of a local repository with the
local keys endpoint, the receipt attestations, and the screens (the section Signing identity, Quader's own signatures at an artifact).
The job signs in the worker with the session's key inside a transaction that holds the current identity's row; the signing code
(`internal/signing/npmsign`, `internal/signing/attest`) receives the key as a `crypto.Signer` from the job and imports neither the key
package nor the job (`CheckSigningKey`). What an npm signature says is that this installation served
this name, version and integrity from a local repository: the message is `<name>@<version>:<integrity>`, the integrity the one the publish
computed of the stored tarball. A receipt attestation is a statement of every artifact a user's repository receives, signed as a Sigstore
bundle with the identity's leaf (R-SIG-051, R-SIG-052); both are kept in stage 1's store with `identity_id` set and the attachment source
`issued` (one mechanism, `RecordIssued`), which the gate never counts (R-SIG-062) and the trust job never judges. An attestation is served
through the API to whoever may read the item, and an image's is written as a referrer by the system (`system:signing`) where it hides no
older signature (R-SIG-054). The signatures answers decode its statement for their readers of R-SIG-034 (R-SIG-055, Q-SIG-234), which
include a holder of `content:metadata-read` without `read`: what it adds is the receipt's own snapshot of facts those answers already show
(the origin with the CI claims through R-SIG-010's allowlist, the integrity states) and of the malware and vulnerability states, within the
statement's bounds and without an e-mail address; the bundle itself stays with the item's readers. Retiring an uploaded identity drops its sealed key from the database
([Q-SIG-149](../spec/signatures.md#q-sig-149--retiring-an-uploaded-identity-drops-its-sealed-key)), and a worker drops an opened key once its
identity neither signs nor waits to.

```
 Helm values: Secret (cert-manager may issue it)        admin area: key request, certificate upload, import
        │ mounted into worker pods only                        │ api seals the key under the key ring
        ▼                                                      ▼
 worker: reloads the files ──────────▶ signing_identities ◀── sealed_key (PostgreSQL, travels with backups)
        │ holds the key in memory        public key, chain, state
        ▼                                      │
 job `signing` ── npm signature, attestation ──▶ store (role registry / attestation, source issued)
                                               │
 api, packages: keys endpoint, documents, referrers, trusted root, chain ── public parts only, never the key
```

**What a signature of Quader proves, and what it does not.** npm's signature proves that this installation's local repository serves the
version with that integrity. The receipt attestation proves that this installation received the bytes of that digest, when, from which
uploader or upstream, and what its checks said then, with the dates of those checks. Neither proves who made the content, that the uploader's
account was not taken over, or that the content is safe today: the uploader's pipeline claims are the claims Quader verified at the upload
(R-SIG-010), and the malware and vulnerability states are snapshots (R-SIG-051).

**Who holds the key, and what that holder can do.**

| Holder | How | What it can make clients believe |
|---|---|---|
| A worker process | it loads the key from the files or opens the sealed key (R-SIG-049) | everything below, and it also writes the facts an attestation states ([H-1187](#h-1187--a-holder-of-the-signing-key-can-make-clients-believe-anything-was-received-here)) |
| A reader of the Secret of the Helm values | the Kubernetes API or the node | as a worker, outside Quader, without an audit event of Quader's |
| A role that holds the key ring and the database (`api`, `packages`, `worker`, the restore Job) | it opens an uploaded key ([H-1188](#h-1188--an-uploaded-identity-can-be-opened-by-every-role-that-holds-the-key-ring-and-the-database)) | as a worker; the Secret way keeps the key out of `api` and `packages` |
| An administrator (`security-settings:write`) | makes, uploads, imports, removes and retires the identity (R-SIG-055) | that a key the company's CA certified is Quader's; never reads the private key back |
| The company's CA | issues the certificate | that any key it certifies with that name is Quader's: the CA is the root of every attestation's trust |
| Whoever restores a backup with the key ring | the sealed key travels with the database | that its copy is the installation ([H-1191](#h-1191--a-restore-carries-the-uploaded-identity-into-whichever-installation-restores-it)) |

**What a taken-over worker can make clients believe:** that any npm package of any name and integrity was served by a local repository of the
installation (a valid registry signature under the listed key, which `npm audit signatures` accepts for any registry that lists that key),
and that any digest was received from any uploader with any pipeline's claims and found clean (an attestation that `cosign
verify-attestation` with Quader's identity accepts). It can do so while it runs and, with a copy of the key, after it is gone, until an
administrator retires the identity and, for attestations, until the certificate expires, because no verifier of Quader's attestations
asks for revocation ([H-1190](#h-1190--a-copied-attestation-outlives-what-it-says)).

**What the design keeps small.** Only the worker loads the key (`api` and `packages`, which face clients and parse their uploads, read
public parts only, held by the archtest of R-SIG-049, `CheckSigningKey`, which allows `internal/signing/key` to the signing store, the signing
job and the composition root only, and by the composition root, which builds the key holder on the roles `worker` and `all` alone); the Helm way keeps the key out of the database and the backups; the plain upload is a
certificate request, so the key never passes a browser (R-SIG-047); a new key is announced before it signs and a signature after its key's
switch is refused by a lock (R-SIG-048), so no client run aborts on Quader's keys; Quader's referrer never stands where it would hide a
publisher's older cosign signature (R-SIG-054); every change of the identity is an audit event, and a removal and a retirement carry the
journal flag (R-SIG-057).

## The gate per repository (stage 3)

A repository whose gate is on (`signatures.onlyVerified`, R-SIG-061) serves an artifact only when a signature of the roles `publisher` or
`provenance` is attached where the repository sees it, matches the content and is known to an anchor the repository accepts (R-SIG-062). It
acts at every download of every format but Debian, at the two serve points the malware gate uses and at OCI's manifest handlers (R-SIG-064).

```
 client GET/HEAD ─▶ authorization ─▶ format's serve check ─▶ malware · vulnerability · signature: all three decide (R-SIG-069)
                                                                     │ the item's signatures, attachments, trust rows: read only where a gate applies
                                                                     │ anchors and blocks: the replica's snapshot, narrowings subtracted
                      403, no-store ◀── one refuses ─────────────────┤
                      served ◀───────── every gate passes ───────────┤
                      503, Retry-After ◀── still pending after the wait (25 s from arrival) ◀── priority check, asked fetch
```

**What it keeps out:** an artifact no accepted signer signed: an upload of an account that was taken over but holds no signing key, content
an upstream changed after it was signed (the integrity check fails) or never signed, a name in a public registry that a remote would fetch
in place of a private one; unless the attacker also holds a key an accepted anchor knows.

**What it does not keep out:** what a trusted signer signed ([H-1171](#h-1171--a-verified-signature-says-who-signed-not-that-the-content-is-safe));
a signature of a stolen key until an administrator blocks the signer, after which the gate refuses at once
([H-1168](#h-1168--a-revoked-certificate-stays-verified-until-an-administrator-blocks-its-signer), R-SIG-066); under "any anchor", a
signature of any identity of the public Sigstore
([H-1194](#h-1194--a-gate-that-accepts-any-anchor-passes-what-any-identity-of-the-public-sigstore-signed)) and of a project Owner's own anchor
([H-1195](#h-1195--under-any-anchor-a-project-owners-own-anchor-passes-the-gate-of-its-projects-repositories)); an image's blob fetched by
its digest ([H-1196](#h-1196--an-images-gate-guards-its-manifests-a-blob-fetched-by-its-digest-passes)); a file that reads as a signature
([H-1197](#h-1197--a-file-read-as-a-signature-passes-the-gate-as-an-attachment)); a pull of an image by a holder of the repository's write
right, which reads unsigned images so that it can sign them, audited and counted for each read but not stopped
([H-1229](#h-1229--a-holder-of-the-write-right-pulls-unverified-images-of-a-gated-oci-repository)).

**Who can open or close it:**

| Party | What it can do to a gate | What stops it |
|---|---|---|
| A writer of the repository's settings, an Owner of the project included | switch it on, accept fewer anchors | a weakening (off, any anchor, an anchor added) needs `repositories:write-config` or `vulnerabilities:triage`; an Owner gets 403 (R-SIG-061) |
| A holder of `repositories:write-config` or `vulnerabilities:triage` | switch it off, accept more anchors | `repository.updated` with `block: lifted`; a narrowing is journaled, so a restore keeps the gate closed |
| A holder of `vulnerabilities:triage` with `read` on the repository or `content:metadata-read` | release one artifact in one repository, also before the gate is switched on and while it is pending (R-SIG-070, Q-SIG-196, Q-SIG-197); the release passes it in that repository's gate alone, whatever its signatures say later ([H-1223](#h-1223--a-release-keeps-passing-an-artifact-after-its-signer-is-blocked)) | `signature.release.granted` with the reason and the gate's reason at the grant; an Owner gets 403; without `read` the repository answers 404; the withdrawal is journaled, so a restore keeps it withdrawn, and reaches the Bastions as `content` of the repository |
| A project's Owner, while project anchors are allowed | add an anchor that passes the project's gates that accept any anchor | a gate with chosen anchors does not take it, and adding it to the list is a weakening (H-1195) |
| An administrator (`security-settings:write`) | remove, switch off or change the material of an anchor, block a signer: the gate refuses at once; add an anchor: it passes after the recomputation | `POST /api/v1/signatures/impact` counts before the change what it makes the gates refuse, by the gate's own decision under the change (R-SIG-071); bounded, so a count beyond `preview_limit` says it is a lower bound ([H-1225](#h-1225--the-impact-of-a-block-of-an-identity-is-a-lower-bound-beyond-the-preview-limit)); the narrowing reaches the Bastions as `content` of every gated repository |
| The operator | `signatures.enabled: false` switches every gate off ([H-1198](#h-1198--switching-the-signature-checks-off-lifts-every-repositorys-gate)) | the repository's section says so |

**What it does when it cannot decide: it refuses.** A signature not checked yet, a trust row not written yet, a remote whose signatures were
not fetched yet make the request wait up to 25 s from its arrival while that work goes first, then answer `503` with `Retry-After`; nothing is
served without a decision (R-SIG-067). A trust row older than a narrowing is narrowed at the gate by subtraction from the replica's snapshot,
so the minutes of the recomputation never serve what a removed anchor or a block refuses (R-SIG-066,
[H-1175](#h-1175--the-shown-trust-lags-a-change-of-anchors-by-the-recomputation)); a widening waits for the recomputation and refuses until
then. A remote's fill is held until its stored item is judged, and a body the remote engine would pass on unstored during an outage of the
object store is answered pending (Q-SIG-140). PostgreSQL down fails the download; the object store takes no part.

## What this does not cover

- [H-1176](../spec/signatures.md#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature):
  through a virtual npm repository, `npm audit signatures` reports local packages as missing a registry signature (closed by stage 2's
  package 2 for an installation with a current signing identity).
- [H-1183](../spec/signatures.md#h-1183--npm-audit-signatures-reports-a-local-version-as-missing-until-quader-has-signed-it):
  `npm audit signatures` reports a local version as missing until Quader has signed it.
- [H-1184](../spec/signatures.md#h-1184--npm-audit-signatures-needs-the-public-sigstore-tuf-repository-before-it-asks-quader):
  `npm audit signatures` needs the public Sigstore TUF repository before it asks Quader.
- [H-1204](../spec/signatures.md#h-1204--after-a-retirement-a-cached-document-carries-a-signature-whose-key-the-keys-endpoint-no-longer-lists):
  after a retirement, a document a cache outside Quader still holds carries a signature whose key the keys endpoint no longer lists.
- [H-1205](../spec/signatures.md#h-1205--between-a-change-of-the-identities-and-the-jobs-next-run-two-renditions-share-a-validator):
  between a change of the identities and the job's next run, two renditions of an npm document share a validator.
- [H-1185](../spec/signatures.md#h-1185--an-image-with-an-older-cosign-signature-gets-quaders-attestation-only-through-the-api): an image
  with an older cosign signature gets Quader's attestation only through the API.
- [H-1186](../spec/signatures.md#h-1186--a-policy-engine-that-demands-a-log-or-a-timestamp-cannot-verify-quaders-attestations): a policy
  engine that demands a log or a timestamp cannot verify Quader's attestations.
- [H-1215](#h-1215--a-remotes-and-a-virtuals-images-carry-quaders-attestation-only-through-the-api): a remote's and a virtual's images carry
  Quader's attestation only through the API.
- [H-1216](#h-1216--a-retired-identitys-referrer-stays-among-an-images-referrers-until-the-job-made-it-again): a retired identity's
  referrer stays among an image's referrers until the job made it again.
- [H-1217](#h-1217--through-a-virtual-repository-quaders-referrer-of-one-member-can-hide-an-older-signature-tag-of-another): through a
  virtual repository, Quader's referrer of one member can hide an older signature tag of another.
- [H-1199](../spec/signatures.md#h-1199--a-client-that-resolves-a-range-picks-a-version-the-gate-refuses): a client that resolves a range
  picks a version the gate refuses.
- [H-1200](../spec/signatures.md#h-1200--docker-and-ansible-galaxy-fail-a-first-pull-through-a-gated-remote-when-the-checks-outlast-the-wait):
  Docker and `ansible-galaxy` fail a first pull through a gated remote when the checks outlast the wait.
- [H-1224](../spec/signatures.md#h-1224--a-virtual-repositorys-member-refused-and-the-version-list-show-one-gate-each): a virtual repository's
  member `refused` and the version list show one gate each.

### H-1166 · The worker reads hostile signature containers in a process that holds secrets

Component: the read and the integrity check of the job `signature-verify` (R-SIG-005, R-SIG-006, R-SIG-013). Subject: the installation's
credentials the worker holds. Live from stage 1. A signature object is parsed by sigstore-go, notation-core-go with go-cose, tspclient-go,
digitorus/pkcs7 and digitorus/timestamp (a Sigstore bundle's timestamps), go-crypto and code of this repository (ASN.1 of NuGet's CMS and
its timestamps since [Q-SIG-216](../spec/signatures.md#q-sig-216--quader-reads-nugets-cms-itself-and-a-signer-named-by-its-key), CBOR,
protobuf JSON, OpenPGP packets, zip offsets for NuGet). A flaw that
turns a crafted object into more than a refusal (a memory blow-up past the bounds, a hang the deadline does not reach, a logic flaw in a
verifier) runs in the worker. What bounds it: the refusals of R-SIG-013, tested with crafted objects at the unit tier; the heap bound
of 64 MiB that R-SIG-013's criterion names is not asserted by a test. What makes it real: a library flaw of that kind and a push or an
upstream that delivers the object; the owner chose the worker over a unit without secrets (Q-SIG-5).

### H-1167 · A project Owner's anchor is shown to every reader of the project's repositories

Component: project anchors (R-SIG-019) and the scope rule
([Q-SIG-26](../spec/signatures.md#q-sig-26--the-scope-is-the-project-of-the-addressed-repository)). Subject: the readers of a project's
repositories, also of content of `upstream` read through its virtual repositories. Dormant until the administrator switches project anchors
on. An Owner, or whoever takes over an Owner's account, adds a CA, a key or a Sigstore identity of its choice, and every signature that anchor
knows reads **Verified · internal** in that project, public images through its virtual repository included. The details name the anchor, its
scope "project" and who added it; no other project and no direct read of `upstream` is affected. What makes it real: a compromised Owner
account or a careless Owner, and readers who take the badge for the company's statement.

### H-1168 · A revoked certificate stays verified until an administrator blocks its signer

Component: the trust step (R-SIG-007, R-SIG-008, the owner's decision Q-SIG-7). Subject: every reader of signatures of a certificate a CA
revoked. Live from stage 1. Quader asks no revocation list and no OCSP responder, so a signature of a revoked certificate stays **Verified**
until an administrator blocks the certificate's fingerprint, serial or identity (R-SIG-020). What makes it real: a stolen signing key that a
CA revoked, while nobody blocks it in Quader.

### H-1169 · A signature first received long after it was made is judged at its receipt

Component: time (R-SIG-007). Subject: a remote's or an uploaded signature without a time proof (OpenPGP, npm's registry signature, a raw key,
a timestamp of an untrusted authority). Live from stage 1. Without a proof the time is Quader's receipt: a valid old signature whose key
expired before Quader saw it reads as not valid at its time, and a signature made with a key after its expiry but before Quader's receipt of
the key's anchor is judged by the key's validity at the receipt. Quader errs to the strict side here; what it does not do is prove a time it
did not witness. What makes it real: a remote created long after the upstream signed its content.

### H-1170 · Without internet the public Sigstore root is the release's snapshot

Component: the shipped anchor `sigstore-public` and npm's keys (R-SIG-017). Subject: installations with `signatures.trust_root.refresh:
false` or whose refresh fails. Live there. The release's snapshot ages: a key the Sigstore project rotated in or out after the build is not
known or still trusted until the next release, and a compromised key the project withdrew stays trusted in the installation. What shows it:
`quader_signatures_trust_root_age_seconds` and `QuaderSignatureTrustRootStale` (14 days), the admin page's section **Public Sigstore root**.
What makes it real: a withdrawal by the Sigstore project in an installation that follows no release for weeks.

### H-1171 · A verified signature says who signed, not that the content is safe

Component: the badge (R-SIG-009, R-SIG-035). Subject: every reader. Live from stage 1. A trusted signer can sign malicious or vulnerable
content; the badge says nothing about the content. The screens say it in one sentence and name the malware scan and the vulnerability
scanning as the checks of the content. What makes it real: a compromised build that signs what it builds.

### H-1172 · A writer can attach signatures that raise alarms and fill the dashboard

Component: detection and the security dashboard (R-SIG-011, R-SIG-038). Subject: the auditors who read the dashboard, the worker's checks.
Live from stage 1. A holder of `deploy` attaches invalid or unreadable signatures to any content of its repository (OCI referrers and tags,
generic siblings); each records `signature.invalid` and an entry, and each costs a bounded check. The badge of content with a valid signature
does not change. What bounds it: 32 signatures read per subject and repository, the object bound, the check's deadline; what makes it real: a
writer who wants to bury a real finding among false ones.

### H-1173 · The background fetch asks an upstream for what no client asked

Component: the remote's fetch (R-SIG-026, built: the job `signature-fetch` and the mirror's plan). Subject: the installation's standing with
an upstream's rate limits, and the upstream that learns which images the installation stores. Live for every remote whose member
`signatures.fetch` (R-SIG-040, the closed section **Signatures** of its settings; an administrator's on a well-known registry, Q-SIG-128)
is on, the default. The requests
are system work, charged to no caller and no ceiling
([usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-015), bounded by four signature manifests per image and eight layers per manifest,
and the job's by `signatures.remote.requests_per_hour` per remote on the worker that runs it (600 by default); a mirror's pass asks for the
signatures of every kept tag in every pass, bounded by the pass's window and slots and not by that hourly budget (Q-SIG-99). An image whose
tags carry no signature costs one referrers request and two `HEAD`s; against Docker Hub a `GET` of a signature manifest counts as a pull, a
`HEAD` does not, and whether the referrers request counts is not stated. What makes it real: a Docker Hub remote without an account on an
address shared with other clients, and a mirror of many tags of Docker Hub.

### H-1174 · "Signed · signer not trusted" can stand for a signature whose match was not checked

Component: the state `no_key` ([Q-SIG-24](../spec/signatures.md#q-sig-24--a-signature-without-a-key-or-with-a-blocked-signer-reads-signed--signer-not-trusted)).
Subject: readers of OpenPGP, npm-registry and raw-key signatures, and of Sigstore bundles and older cosign signatures made with a key and no
certificate, whose key neither an anchor nor the upstream announces. Live from stage 1. The
badge reads like that of a matching signature by an unknown signer, although the bytes were not matched; anyone can write a well-formed
OpenPGP packet. The details say "could not be matched with the content: no key of this signer is known". What makes it real: a reader who
takes "Signed" for "signed by someone, unchanged since".

### H-1175 · The shown trust lags a change of anchors by the recomputation

Component: the trust job (R-SIG-012). Subject: readers after an anchor was removed, switched off or a signer blocked. Live from stage 1. Until
the job reaches a row, the screens show the trust computed under the previous anchors, marked `stale` in the API; with a million signatures
that can be minutes. Stage 1 refuses nothing, so the lag misleads a reader and lets nothing through; a gate of stage 3 has to decide on stale
rows itself (R-SIG-043). What makes it real: a block of a stolen key, read by someone in the minutes after.

### H-1187 · A holder of the signing key can make clients believe anything was received here

Component: the signing identity and the job `signing` (R-SIG-045, R-SIG-049). Subject: every consumer that trusts Quader's npm keys or its
attestations: pipelines that run `npm audit signatures`, clusters and pipelines that run `cosign verify-attestation` with Quader's identity.
Live from stage 2 in an installation with an identity. Whoever holds the private key (a worker process, a reader of the Secret of the Helm
values, a role that holds the key ring and the database for an uploaded key) signs any package name and integrity as served by a local
repository and any digest as received, from any uploader, with any pipeline claims and a clean verdict. A taken-over worker also writes the
facts an honest attestation states, so even a correct key states false facts. What bounds it: the key is loaded by the worker alone; the
Helm way keeps it out of the database; an administrator retires a key believed taken, which takes it out of npm's keys at once. What makes it
real: a flaw that runs code in the worker (H-1166 is one way in) or read access to the Secret.

### H-1188 · An uploaded identity can be opened by every role that holds the key ring and the database

Component: the sealed key of an upload (R-SIG-047, Q-SIG-54). Subject: installations whose identity was uploaded in the admin area. Live from
stage 2 there; dormant with the Helm way. The key ring is mounted into every replica (`api`, `packages`, `worker`) and the restore Job
([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-030), so a taken-over `api` or `packages` process can read `signing_identities.sealed_key`
and open it, although only the worker ever does in normal operation: the archtest of R-SIG-049 holds which packages import the code that
opens it, not what a taken-over process does with the key ring. A retired identity's sealed key is gone (Q-SIG-149), so the exposure covers
the pending, announced, current and previous uploads. What makes it real: code execution in a role that faces clients; the Helm way, whose
Secret only the worker pods mount, avoids it.

### H-1189 · The time and the facts of an attestation are Quader's own claim

Component: the receipt attestation without a log and without a timestamp (R-SIG-051, R-SIG-052, Q-SIG-63). Subject: auditors and consumers who
read `receivedAt` and `issuedAt`. Live from stage 2. Nothing outside Quader witnesses when an attestation was made: a holder of the key can make
one today that claims a receipt of last year, and no log shows which attestations exist. What makes it real: an audit that relies on the
attestation's dates alone; a company timestamp authority or a log of the company's own Sigstore would give the time a witness.

### H-1190 · A copied attestation outlives what it says

Component: the receipt attestation's snapshots and its lifetime (R-SIG-051, R-SIG-053). Subject: consumers who keep or cache an attestation.
Live from stage 2. Quader replaces an attestation when the malware state changes and when the identity changes, and removes it with the item,
but a copy that someone saved keeps verifying until its certificate expires: "clean" for a blob found infected since, a vulnerability grade
from before new advisories, a receipt of an item deleted since. No verifier of Quader's attestations asks for revocation, and Quader publishes
none. What makes it real: a policy that verifies a stored attestation instead of fetching the current one from Quader.

### H-1191 · A restore carries the uploaded identity into whichever installation restores it

Component: backups of `signing_identities` (R-SIG-059, Q-SIG-74). Subject: installations with an uploaded identity whose backups are restored
elsewhere with the same key ring. Live from stage 2 there. The restored copy keeps the installation's ID and opens the sealed key, so a copy
restored for a test signs npm packages and attestations as production until an administrator removes the identity. What makes it real: a
restore into a test cluster that reuses production's key ring.

### H-1192 · The signing key is in memory and in a Secret or the database, not in hardware

Component: the signing identity (R-SIG-045, Q-SIG-73). Subject: the key. Live from stage 2. The key lives in the worker's memory and either in
a Kubernetes Secret or sealed in PostgreSQL; a copy can be taken by whoever reads one of them, and nothing shows that it was. No PKCS#11 device
and no cloud key store is supported. What makes it real: a company policy that requires signing keys in hardware, as the CA/Browser Forum does
for publicly trusted code-signing certificates.

### H-1193 · `cosign verify` with Quader's identity accepts Quader's receipt as a signature of the image

Component: the OCI attestation referrer (R-SIG-054). Subject: whoever runs `cosign verify` (signatures, not attestations) with
`--certificate-identity` set to Quader's. Live from stage 2. cosign 3.1.4 takes every bundle among the referrers as a candidate and accepts a
DSSE bundle whose statement names the image (read in its source on 2026-11-21), so Quader's receipt passes as "signed by Quader". It says
received, not made; a policy that reads it as the publisher's signature trusts the wrong fact. What makes it real: a policy written with
Quader's identity in `cosign verify` instead of `cosign verify-attestation` with the receipt's type.

### H-1194 · A gate that accepts any anchor passes what any identity of the public Sigstore signed

Component: the gate with `anchors: []` (R-SIG-061, R-SIG-062, Q-SIG-76) and the shipped anchor `sigstore-public`, whose identity pattern takes
every issuer and identity (R-SIG-017). Subject: repositories whose gate accepts any anchor. Live where a gate is on; dormant where
the administrator switched `sigstore-public` off or the repository chose anchors. Anyone can sign keyless with the public Sigstore with an
e-mail account or a CI job of a public forge, and an npm package published from such a job carries a provenance that verifies against it, so
a writer of a gated local repository, or an upstream of a gated remote, passes the gate with a signature of an identity of its own. The
setting says so beside the choice, and the details name the identity. What makes it real: a gate switched on with the default choice in the
belief that it accepts only the company's signers.

### H-1195 · Under any anchor, a project Owner's own anchor passes the gate of its project's repositories

Component: project anchors in the gate's scope (R-SIG-019, R-SIG-061, Q-SIG-93). Subject: gated repositories of a project whose gate accepts
any anchor. Dormant until the administrator allows project anchors. An Owner, or whoever takes over an Owner's account, adds a key of its
choice as a project anchor and signs what it publishes, and the project's gates that accept any anchor pass it: the Owner cannot switch a gate
off, but this has the same effect for the Owner's own uploads. A gate whose anchors were chosen does not take the new anchor, and adding it
to the list is a weakening the Owner may not make. What makes it real: an administrator's gate on a project's repository with the default
choice while project anchors are allowed.

### H-1196 · An image's gate guards its manifests: a blob fetched by its digest passes

Component: OCI's classification (R-SIG-063, Q-SIG-82). Subject: gated OCI repositories. Live where a gate is on. The gate judges
manifests by tag and by digest; layers, configs and the content of an attachment are blobs and pass, as at the vulnerability block
([image-scanning.md](image-scanning.md#h-811--the-block-of-an-image-is-the-block-of-its-manifest)). A client that knows the digest of a layer
of an unsigned image, or pulls the layer of an SBOM or signature artifact with a tool such as `oras`, gets the bytes; no image runtime runs an
image without its manifest. What makes it real: a consumer that fetches blobs by digest from a list it got elsewhere.

### H-1197 · A file read as a signature passes the gate as an attachment

Component: `GateSignatureFile` (R-SIG-063). Subject: gated generic and npm repositories. Live where a gate is on. A file named like a
signature of the file beside it (`.asc`, `.sig`, `.sigstore.json`, `.jws.sig`, `.cose.sig`) and npm's provenance bundle pass while they were
read as a signature of a known scheme, so that a client can verify what it downloads; within the bound of 256 KiB such a file can carry
other bytes, in an OpenPGP notation or a bundle's fields. What makes it real: a writer who hides a payload in a signature file and a consumer
that fetches and runs it.

### H-1198 · Switching the signature checks off lifts every repository's gate

Component: `signatures.enabled` (R-SIG-040, R-SIG-064). Subject: every gated repository. Live. The operator's
configuration switches detection and checks off, and with them every gate, as `malware.enabled: false` lifts the malware gate; the
repositories' settings stay stored and apply again when the checks are switched on. No repository writer is asked; the repository's section
says that the setting does not apply. What makes it real: an operator who switches the checks off to spare the worker while teams rely on
their gates.

### H-1223 · A release keeps passing an artifact after its signer is blocked

Component: `signature_releases` (R-SIG-070, Q-SIG-88). Subject: gated repositories with releases. Live since package 2 of stage 3. A release
passes its artifact in its repository whatever the signatures say, so an artifact released while its signer was unknown keeps being served
after an administrator blocks that signer as stolen; the block's impact count leaves it out, because it still passes, and the dashboard's
list of refused downloads never shows it. The release names its reason and who granted it, and the releases' list of the repository shows
it. What makes it real: a release granted for an urgent artifact of a signer whose key is later found compromised, and nobody reviews the
repository's releases at the block.

### H-1225 · The impact of a block of an identity is a lower bound beyond the preview limit

Component: the impact count (R-SIG-071, Q-SIG-202). Subject: an administrator who blocks a Sigstore identity pattern in an installation with
many keyless signatures. Live since package 2 of stage 3. A block of a key reads its candidates through the index of key IDs; a block of an
identity has no index to read through and reads at most `signatures.gate.preview_limit` signatures that carry an identity, in no order, and
matches the pattern itself. Beyond the bound the answer says `complete: false` and its counts are lower bounds; the block itself refuses
every match at once. What makes it real: more than 100,000 keyless signatures of publishers and builds attached in gated repositories.

### H-1229 · A holder of the write right pulls unverified images of a gated OCI repository

Component: the writer's read of R-SIG-064 ([Q-SIG-211](../spec/signatures.md#q-sig-211--a-writer-of-the-repository-reads-through-the-gate-where-the-formats-signing-tools-read-the-artifact)).
Subject: gated OCI repositories and every credential with `deploy` on them, a CI token included. Live since the repair of 2026-11-21. So
that `cosign sign` and `notation sign` can read an image pushed while the gate is on, a principal that may write to the repository reads its
manifests through the signature gate, by tag and by digest; the same credential therefore pulls and runs an unsigned image, where a
consumer with the read right alone is refused. The malware gate and the vulnerability block still decide; other formats are not affected,
because their signatures are made before the upload. The owner kept the read on 2026-11-22 with a trace: each such read is audited as
`signature.download_by_writer` (outcome `warning`, the caller, the repository, the artifact in its client's words and the reason a reader is
refused for, once a minute per repository, path, caller and client address on each replica,
[Q-SIG-240](../spec/signatures.md#q-sig-240--a-writers-head-and-get-of-one-manifest-within-a-minute-are-one-event)) and counted in
`quader_signatures_downloads_by_writer_total`. A writer's `HEAD` passes the same way and is neither audited nor counted
([Q-SIG-422](../spec/signatures.md#q-sig-422--a-writers-head-through-the-gate-is-not-a-read-it-is-served-and-neither-counted-nor-written)):
it answers the manifest's digest, size and media type, none of its content, and a push asks it for every manifest it puts. What remains open is the read itself: the trace tells who pulled an unverified image, it does
not stop the pull, and an event the full buffer drops is in the log stream only (R-OPS-015). What makes it real: a deploy token used also to
pull into a cluster, or a CI job that pushes and runs images with one credential.

### H-1201 · An npm version's fetch state outlives its tarball

Component: the row of `signature_fetches` of an npm version (R-SIG-026, R-SIG-067,
[Q-SIG-106](../spec/signatures.md#q-sig-106--one-table-of-fetch-state-for-both-stages-keyed-by-target-with-the-subject-beside-it)). Subject: a
remote npm repository whose cache lost a tarball (an eviction, a delete of the cached item) and fills it again. Live from stage 1 for the
badge, and for the gate, which reads the row. The OCI row goes with its manifest's row; the npm row stays, because the npm hook of the removal
writes through the package writer and has no transaction of the data port. A tarball stored again is not fetched again (its row reads
fetched, and npm queues no refresh of a version), so attestations the cache lost with it come back only when a client asks the attestations
endpoint, and the gate reads "fetched" from the earlier run. What makes it real: an eviction of a version's attestations document
together with its tarball, and a later fill of the tarball alone.

### H-1202 · A project that starts to reach a signature is served the installation's trust until the next recomputation

Component: the trust job (R-SIG-008, R-SIG-012). Subject: readers of a project with anchors of its own (switch on) when a signature it did not
reach before becomes attached in one of its repositories, or in a repository of `upstream` one of its virtual repositories lists. Live where
project anchors are used. The project's row is computed when the signature is checked and when the anchor epoch changes; a new attachment of
a signature that is checked already changes neither, so the project's readers see the installation's row (often **Signed · signer not
trusted** where the project's anchor would say **Verified · internal**) until the next change of anchors, blocks or the switch. It errs
towards less trust, never more. What makes it real: the same signed bytes pushed to a second project after the first project's push was
checked.

### H-1203 · An object store that breaks off inside a package or an archive makes a signature unreadable instead of retried

Component: the read of NuGet's signature entry and unsigned bytes and of an Ansible collection's files (R-SIG-006). Subject: a signature
whose content read broke off in the middle. Live from stage 1. A whole-blob read or a hash stream that fails is a check that could not run
and is tried again with its back-off; an error inside the bounded zip or tar reader is reported by the reader as a damaged archive, so the
signature is recorded `malformed` ("Signature could not be read") and is checked again only when its subject is committed again. What
makes it real: a transient object-store failure during the check of a NuGet package or an Ansible collection.

### H-1210 · An imported signing key passes through the administrator's browser

Component: **Import key and certificate** of the section **Signing identity** (R-SIG-047, R-SIG-056, Q-SIG-174). Subject: installations
whose identity is imported in the admin area instead of made by Quader's key request. Live from the section's landing whenever the import is
used; dormant with the key request and with the Helm values, where the key never reaches a browser. The dialog sends the PEM key and its
passphrase once and clears both fields after any answer, and no draft keeps them, but the pasted key lives in the page's memory until it is
collected, in the clipboard until it is replaced, and in reach of every browser extension that may read the page; a password manager may
offer the passphrase to be saved (the field is `autocomplete="off"`, which not every manager honours). The Debian import has the same
([H-765](deb-signing.md#h-765--an-imported-secret-key-passes-through-the-operators-browser)). What makes it real: an administrator's browser
or machine that is not trusted with the installation's signing key; `POST /api/v1/signing/identity/import` from `curl` with the key read
from a file keeps it out of a browser.

### H-1215 · A remote's and a virtual's images carry Quader's attestation only through the API

Component: the placement of an image's attestation (R-SIG-054, Q-SIG-183). Subject: clusters and pipelines that look for Quader's receipt
among the referrers of an image pulled through a remote repository, or addressed through a virtual repository whose member that holds it is
a remote. Live since package 3 of stage 2. Quader writes its referrer only into a local repository; an image a remote cached gets its
attestation through `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/attestation` alone, so `cosign verify-attestation` against the
remote's address and an admission controller that reads the referrers find none and refuse the image if their policy demands Quader's
receipt. Nothing is hidden by it: no publisher's signature is shadowed, and the attestation exists. What makes it real: a policy that demands
Quader's receipt for every image, and images pulled through remotes.

### H-1216 · A retired identity's referrer stays among an image's referrers until the job made it again

Component: the retirement of an identity (R-SIG-048, Q-SIG-191). Subject: verifiers of a local image's attestation after an administrator
retired the identity that made it, for a key believed taken. Live since package 3 of stage 2. The route stops serving the retired identity's
attestations at once and the retirement writes each of them as work at priority 2, but the referrer of an image stays in the registry until
the job replaces it, minutes for a backlog of thousands; a verifier that still trusts the retired identity's chain (an old trusted root, a CA
anchor of the company) verifies it meanwhile. The certificate's chain is the trust a verifier holds, not Quader's list. What makes it real: a
retirement of a key that was taken, a large backlog, and verifiers that do not refresh the trusted root.

### H-1217 · Through a virtual repository, Quader's referrer of one member can hide an older signature tag of another

Component: the virtual repository's referrers union (R-SIG-027, R-SIG-054). Subject: verifiers that pull an image through a virtual repository
whose local member holds the image with Quader's referrer while another member (a remote, or a second local repository) holds a cosign
signature of the same digest in the older layout (`sha256-<hex>.sig`, `.att`). Live since package 3 of stage 2. Quader's referrer stands only
where its own repository holds no such tag (Q-SIG-66), but the union of a virtual repository lists the referrers of every member and the
filter R-SIG-054 names, which would leave Quader's out there, is not built; cosign 3 then finds a bundle among the referrers and stops
reading the older tag, so `cosign verify` of the publisher's older signature through the virtual address fails, while the same image's address
of the member that holds the tag verifies. What makes it real: the same digest pushed to a local repository and signed in the older layout
in another member of the same virtual repository.

### H-1233 · A timestamp of a Sigstore bundle or a Notation signature whose authority names its signer by its key is not read

Component: the RFC 3161 timestamps of the schemes `sigstore-bundle` and `cosign-legacy` (sigstore-go with digitorus/timestamp, and
Quader's reading of a bundle's timestamp in [proofs.go](../../internal/signatures/scheme/sigstore/proofs.go) on digitorus/pkcs7) and of
`notation-jws` and `notation-cose` (tspclient-go). Subject: a signature whose timestamp authority writes its `SignerInfo` as version 3, the
signer named by `subjectKeyIdentifier` (RFC 5652, section 5.3). Dormant: all three libraries read a signer named by issuer and serial
number only, the form of NuGet's own blind spot until
[Q-SIG-216](../spec/signatures.md#q-sig-216--quader-reads-nugets-cms-itself-and-a-signer-named-by-its-key) repaired it for NuGet. Such a
bundle reads `malformed` ("an RFC 3161 timestamp of the bundle does not parse") and such a Notation signature does not verify. It stays
dormant as long as the writing tools share the readers: cosign parses an authority's answer with digitorus/timestamp and Notation with
tspclient-go before either writes the signature (read from what each tool depends on, not run here); the authorities seen on 2026-11-21
(DigiCert's tokens in nuget.org's packages) name their signer by issuer and serial number. What makes it real: a timestamp
authority that answers in the other form and a signing tool that does not check the answer with one of these libraries. Repairing only
Quader's own reading in `proofs.go` would turn such a signature from unreadable into `timestamp_invalid` wherever a root knows the authority,
because sigstore-go's verification still refuses it.
