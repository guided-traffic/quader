# Signatures on artifacts

Status: stage 1 in work: the detectors, the store and the hooks that record candidates and origins are built, the read, the checks
and the trust step are not ([developer/signatures.md](../developer/signatures.md)); stage 1 of [signatures.md](../spec/signatures.md) (R-SIG-001 to R-SIG-043,
[ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact) to
[ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version)) and stage 2,
Quader signs (R-SIG-044 to R-SIG-060,
[ADR-230](../decisions.md#adr-230--quader-signs-with-one-identity-per-installation-npms-registry-signatures-and-a-receipt-attestation-of-every-artifact-signed-in-the-worker)).
The perspective of this page is what a verdict about a signature can be made to say, by whom, and what reading a hostile signature container
can do to the process that reads it; and, for stage 2, what Quader's own signature makes others believe, who holds the key that makes it,
and what a holder of that key can do. Neither stage refuses a download; every gap below is about what a reader or a client is shown, what
the worker is made to do, and what an operator relies on.

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
of at most 256 KiB, chains of eight certificates, JSON scanned for depth and count before any decoder, a check bounded to 5 s, a panic
recovered per check); the libraries are memory-safe Go (R-SIG-014), reached only from `internal/signatures`; and the content a keyless
scheme or NuGet reads is streamed from the blob store and hashed, never written to disk or parsed beyond what the scheme needs.

## Who decides what reads as trusted

| Party | What it can make Quader show | What stops it |
|---|---|---|
| A holder of `deploy` on a repository | an attachment of any signature to content of that repository: a valid one by any signer (shown as its trust allows), an invalid one, an unreadable one | an invalid one never lowers a better badge ([Q-SIG-25](../spec/signatures.md#q-sig-25--an-artifacts-state-is-the-best-of-its-signatures-and-an-invalid-one-alone-shows-invalid)); at most 32 read per subject; it alarms ([H-1172](#h-1172--a-writer-can-attach-signatures-that-raise-alarms-and-fill-the-dashboard)) |
| An upstream of a remote | the signatures its documents and referrers carry, for the remote's readers | the integrity check against the content Quader stored; trust only through anchors the installation chose |
| A project's Owner, while the administrator's switch is on | **Verified · internal** for signatures its anchors know, for every reader of the project's repositories and of what its virtual repositories serve | anchors only add; the administrator lists and removes them; off again, they stop applying ([H-1167](#h-1167--a-project-owners-anchor-is-shown-to-every-reader-of-the-projects-repositories)) |
| An administrator (`security-settings:write`) | trust for the whole installation: anchors, switching shipped ones off, blocks, the switch | audit events; removals and blocks carry the journal flag, so a restore does not bring back withdrawn trust |
| The release | the shipped anchors: the public Sigstore root, npm's keys, Debian's and Ubuntu's keyrings, nuget.org's certificates and timestamping roots | each can be switched off; the public root and npm's keys are refreshed through TUF from the embedded root ([H-1170](#h-1170--without-internet-the-public-sigstore-root-is-the-releases-snapshot)) |
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
public parts only, held by the archtest of R-SIG-049); the Helm way keeps the key out of the database and the backups; the plain upload is a
certificate request, so the key never passes a browser (R-SIG-047); a new key is announced before it signs and a signature after its key's
switch is refused by a lock (R-SIG-048), so no client run aborts on Quader's keys; Quader's referrer never stands where it would hide a
publisher's older cosign signature (R-SIG-054); every change of the identity is an audit event, and a removal and a retirement carry the
journal flag (R-SIG-057).

## What this does not cover

- [H-1176](../spec/signatures.md#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature):
  through a virtual npm repository, `npm audit signatures` reports local packages as missing a registry signature (closed by stage 2 for an
  installation with a signing identity).
- [H-1183](../spec/signatures.md#h-1183--npm-audit-signatures-reports-a-local-version-as-missing-until-quader-has-signed-it):
  `npm audit signatures` reports a local version as missing until Quader has signed it.
- [H-1184](../spec/signatures.md#h-1184--npm-audit-signatures-needs-the-public-sigstore-tuf-repository-before-it-asks-quader):
  `npm audit signatures` needs the public Sigstore TUF repository before it asks Quader.
- [H-1185](../spec/signatures.md#h-1185--an-image-with-an-older-cosign-signature-gets-quaders-attestation-only-through-the-api): an image
  with an older cosign signature gets Quader's attestation only through the API.
- [H-1186](../spec/signatures.md#h-1186--a-policy-engine-that-demands-a-log-or-a-timestamp-cannot-verify-quaders-attestations): a policy
  engine that demands a log or a timestamp cannot verify Quader's attestations.

### H-1166 · The worker reads hostile signature containers in a process that holds secrets

Component: the read and the integrity check of the job `signature-verify` (R-SIG-005, R-SIG-006, R-SIG-013). Subject: the installation's
credentials the worker holds. Live from stage 1. A signature object is parsed by sigstore-go, notation-core-go with go-cose, tspclient-go,
digitorus/pkcs7 and digitorus/timestamp, go-crypto and code of this repository (ASN.1, CBOR, protobuf JSON, OpenPGP packets, zip offsets for NuGet). A flaw that
turns a crafted object into more than a refusal (a memory blow-up past the bounds, a hang the deadline does not reach, a logic flaw in a
verifier) runs in the worker. What makes it real: a library flaw of that kind and a push or an upstream that delivers the object; the owner
chose the worker over a unit without secrets (Q-SIG-5).

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

Component: the remote's fetch (R-SIG-026). Subject: the installation's standing with an upstream's rate limits, and the upstream that learns
which images the installation stores. Live for every remote with `signatures.fetch: true` (the default). The requests are system work,
charged to no caller and no ceiling ([usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-015), bounded by four signature manifests per
image and `signatures.remote.requests_per_hour` per remote and replica; against Docker Hub a `GET` of a signature manifest counts as a pull.
What makes it real: a Docker Hub remote without an account on an address shared with other clients.

### H-1174 · "Signed · signer not trusted" can stand for a signature whose match was not checked

Component: the state `no_key` ([Q-SIG-24](../spec/signatures.md#q-sig-24--a-signature-without-a-key-or-with-a-blocked-signer-reads-signed--signer-not-trusted)).
Subject: readers of OpenPGP, npm-registry and raw-key signatures whose key neither an anchor nor the upstream announces. Live from stage 1. The
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
and open it, although only the worker ever does in normal operation. What makes it real: code execution in a role that faces clients; the
Helm way, whose Secret only the worker pods mount, avoids it.

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
