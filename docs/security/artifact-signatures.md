# Signatures on artifacts

Status: specified, not built: stage 1 of [signatures.md](../spec/signatures.md) (R-SIG-001 to R-SIG-043,
[ADR-226](../decisions.md#adr-226--quader-detects-reads-and-verifies-the-signatures-of-every-format-and-shows-them-at-the-artifact) to
[ADR-228](../decisions.md#adr-228--the-format-contract-declares-every-formats-signatures-and-a-signature-is-never-a-version)). The
perspective of this page is what a verdict about a signature can be made to say, by whom, and what reading a hostile signature container
can do to the process that reads it. Stage 1 refuses no download; every gap below is about what a reader is shown, what the worker is made
to do, and what an operator relies on.

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

## What this does not cover

- [H-1176](../spec/signatures.md#h-1176--through-a-virtual-npm-repository-npm-audit-signatures-reports-local-packages-as-missing-a-registry-signature):
  through a virtual npm repository, `npm audit signatures` reports local packages as missing a registry signature.

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
