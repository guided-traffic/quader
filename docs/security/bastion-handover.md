# The handover per blob to a Bastion

Status: specified on 2026-11-22; the wire and the main installation's side for plain copies built on 2026-11-23
([bastion.md](../spec/bastion.md#the-handover-per-blob) R-BAS-070 to R-BAS-079;
[storage.md](../spec/storage.md) R-STOR-095, R-STOR-096;
[ADR-250](../decisions.md#adr-250--the-handover-per-blob-to-a-bastion-a-link-and-the-data-key-of-one-released-blob-in-the-answer-to-its-forward)).
The wire (interface version 6, [handover.go](../../internal/bastion/iface/handover.go)) holds the value's checks, a data key type that
renders `[REDACTED]` in every form, and the redaction of `Quader-Bastion-Handover` in logs, spans and support bundles. The main installation
answers a recognized forward that offered with a link of a **plain** copy while the registration's switch is on and every condition of
R-STOR-095 holds ([downloadlinks/handover.go](../../internal/downloadlinks/handover.go)); it hands out no data key: an encrypted copy is never
named, and `blob.Store.CopyDataKey` still has no caller (package 4). No Bastion offers yet (package 3), and its client refuses a handover
answer, so no link leaves today. The gaps of a data key (H-1340, H-1341) stay dormant until package 4; the others are dormant until a
Bastion offers and an administrator switches the handover on for its registration.

The perspective of this page is what changes in the trust between the main installation, a Bastion in the DMZ and the object store when a
Bastion reads a blob from the store by a link the main installation hands it: what leaves the main installation and how long it is worth
anything, what a taken-over Bastion gains, what the store's operator learns, and what stays where it was. The Bastion's other mechanisms are
on [bastion.md](bastion.md), the keys of the stored content on [stored-content-keys.md](stored-content-keys.md), the clients' links on
[download-links.md](download-links.md).

## The boundary

```text
 caller ──(1)──► Bastion ──(2) forward with the caller's credential + offer ──► main installation: decides as the caller's
                   │     ◄─(3) 200 without a body, the file's headers,           own request, gates, Admit, counts; signs a
                   │           the link [and the data key] in a marking header   link (local work); for an encrypted copy opens
                   │                                                             its record and records the key's handover
                   └──(4) GET <link>, no credential ──► object store, at the connection's public address
 (1) the internet. (2), (3) the trust boundary between the DMZ and the main installation: TLS, the Bastion token, the caller's credential.
 (4) the DMZ to the store: TLS verified against the system roots and the Bastion chart's trust bundle; nothing of Quader's credentials.
```

The main installation's part ends with its answer (3). What the Bastion does with the link happens between the Bastion and the store; the
main installation does not learn whether, when or how often it was fetched.

## What leaves the main installation

| Item | Lives | Opens | Worth after its blob is withdrawn |
|---|---|---|---|
| The link | `linkLifeSeconds` (60 s unless the administrator sets 10 s to an hour), never beyond the signing credential (R-STOR-072) | a `GET` of one object on one connection, for whoever holds it ([H-12](../spec/storage.md#h-12--presigned-download-urls-are-bearer-capabilities)) | nothing once it expired; within its life the object as long as it is at the store |
| The data key, for a copy on an encrypted connection | in the Bastion's memory for one read (R-BAS-076); for good in a taken-over Bastion that keeps it | that object's ciphertext wherever it lies: the connection's bucket, its noncurrent versions, a backup's pool (R-BAK-031); no other object and no other copy, since each encrypted copy has a data key of its own (R-STOR-087) | as much as before: a rotation of the root, a new generation of the storage key and the deletion of the blob change no data key; only a new connection filled with new data keys ends it for the live copy ([H-1340](#h-1340--a-taken-over-bastion-collects-the-data-keys-of-the-encrypted-blobs-filled-through-it)) |
| The digest, the sizes, the connection's identifier, the file's headers | with the Bastion's cache entry | nothing; the digest is the content address the `ETag` of every forwarded answer carries already | nothing |

**What never leaves:** the store's credential (it signs at the main installation and stays there), the storage keys, the name keys, the wraps
and records, anything of the key ring, and the data key of a plain copy, which has none (R-BAS-070). A handover of a plain copy carries a link
and no key, and a plain copy is handed before an encrypted one (Q-STOR-341).

**For which blobs:** only for a blob a caller fetched cold through the Bastion while its registration's handover was on, decided as that
caller's own request at the main installation: authentication, the decision point with the endpoint's exposure, the format's serve-time
check, the three gates on decided passing states and the download limits (R-STOR-095). A Bastion cannot ask for a handover without a
caller's request that may read the blob. A taken-over Bastion sees the credentials of the callers passing through it
([H-386](../spec/bastion.md#h-386--whoever-holds-the-bastion-token-can-test-credentials-name-client-addresses-and-add-download-events) and
R-BAS-051) and can ask with them: it obtains handovers for what those callers may read, which is what it can read through them today.

**Who can ask at all:** only a request with a valid Bastion token of a registration whose switch is on. The recognition stage reads the offer
into the request's recognition after the token check and takes `Quader-Bastion-Handover` out of every request, recognized or not; the serve
point decides on the recognition alone, and notes the value for the marking stage, which writes it only for a recognized forward that
offered and strips every `Quader-Bastion-*` header a handler set. An ordinary caller that sends the offer gets the file and no marking
header, and a forward whose answer the marking stage would not mark is streamed rather than answered with an empty `200`
([recognize.go](../../internal/bastionmain/recognize.go), [absence.go](../../internal/bastionmain/absence.go),
[mark.go](../../internal/api/bastionmark/mark.go)).

## Integrity: the Bastion's check

The Bastion verifies every byte it keeps or serves in full before it releases the last of it (R-BAS-074):

- **A plain copy:** the SHA-256 of the whole object against the digest of the handover and the content address of the `ETag`, at the
  commit; no caller receives the last 64 KiB before it (R-BAS-021, R-BAS-032).
- **An encrypted copy:** every segment's tag under the handed data key before its plaintext reaches the spool; the final segment's length and
  digest; the plaintext's SHA-256 (the format's reader, R-STOR-082); then the commit's check as for every fill.

A body changed in the bucket, the body of another object, or a body under another data key is refused: a store or a network in between
cannot make the Bastion keep or fully serve other bytes. The Bastion reads no record. It cannot open a wrap, and it needs none: the main
installation opened the record (steps 1 to 6 of R-STOR-077, the commitment among them) before it handed the key, so the Bastion reads under
the one key the record names, and no body is read under two keys
([H-1302](stored-content-format.md#h-1302--the-commitment-binds-a-record-to-one-data-key-not-a-body-to-one-record) stays dormant, Q-BAS-234).
A failure ends in the bytes through the main installation, whose own reader then meets the copy and marks it where it is damaged (R-BAS-075),
except for a changed plain copy, found only at the end of a read
([H-1345](#h-1345--a-changed-plain-copy-cuts-the-callers-of-one-fill-before-the-main-installation-learns-of-it)).

## What a taken-over Bastion gains

What it holds today is on [bastion.md](bastion.md#what-the-bastion-is-trusted-with): its cache, the Bastion token, the callers' credentials in
transit, the plaintext of what it serves. With the handover, per case:

| With | It gains | For how long | What bounds it |
|---|---|---|---|
| a link it is handed | the object's bytes, which it receives anyway; the store's public address, the bucket, the object's name and the signing key's identifier ([H-1344](#h-1344--a-handover-shows-a-bastion-where-the-main-installations-objects-lie)) | the bytes: the link's life; the address and the layout: for good | the link's life; the store's own policies |
| a data key it is handed | the plaintext of that blob, which it receives anyway; with read access to the connection's bucket, its noncurrent versions or a backup's target, that object's ciphertext there, now and later (H-1340) | until the connection is replaced and its old objects are gone | the trail `bastion.data_key.handed`, which names every key it received (R-STOR-096) |
| a data key and write access to the bucket | segments of that one object that authenticate, which a ranged read at the main installation serves ([H-1341](#h-1341--a-handed-data-key-and-write-access-to-the-bucket-make-segments-of-that-object-that-a-ranged-read-accepts)) | as above | a whole read and the scrub refuse such an object |

**What stays out of the DMZ:** an account of the store, the storage keys, the name keys and with them every name mapping beyond the blobs it
was handed, the key ring, and every data key of a blob no caller fetched through that Bastion. A Bastion pod that is taken over without the
handover switched on gains nothing of this page.

## What the object store's operator learns

A direct read is a request of the Bastion to the store: its log, where its operator turned one on, holds the Bastion's egress address, the
object's name and the time, the link's signer (the connection's credential, never the caller) and the user agent `quader-bastion/<release>`
([H-1343](#h-1343--the-object-stores-log-shows-the-bastions-address-and-which-objects-it-reads)). Today the store sees only the main
installation's replicas. On a connection with keyed names the operator learns which objects are read through a Bastion, not which files
they are.

## The store's certificate and the link's host

The link client trusts the system roots and the Bastion chart's `trustedCA` (Q-BAS-237), the bundle it trusts for the main installation
already: a CA added for one vouches for the other. The integrity of the bytes does not rest on that trust (above); a party that could
present a certificate for the store's host would see a plain object and the link, which serves that object for its life. The client sends no
credential and no header of the interface, follows no redirect, and refuses a link that is not `https` or whose host is a host of the Bastion
or of the main installation (R-BAS-074); the value's check already refuses such a link, compares the host by name whatever the port, and
refuses a link with user information, which Go's client would send as a credential
([Q-BAS-281](../spec/bastion.md#q-bas-281--the-wires-checks-are-narrower-than-the-list-of-r-bas-073)); the main installation checks every public address against Quader's hosts when it is saved and at
every start (R-STOR-072, R-STOR-074). The connection to the store is not under the egress address policy of the upstream clients, as the
connection to the main installation is not
([H-446](../spec/bastion.md#h-446--the-connection-to-the-main-installation-is-not-under-the-egress-address-policy)): its host comes from the
main installation.

## A blob withdrawn while its link lives

The main installation decides when the forward arrives; a deletion, a malware find, a narrowed signature gate, a vulnerability block or a
revoked right after that does not stop a direct read that has begun, as it does not stop a forwarded fill today, and the feed's entry refuses
the cached entry to every later caller (R-BAS-077). No window opens that a forwarded fill does not have. What outlives a withdrawal is the
link for the rest of its life (H-12) and a data key a taken-over Bastion kept (H-1340).

## What is counted and recorded

The main installation counts a handover at issuance, as a link: one download, the size as cached bytes, the parallel lease ended with the
answer, `artifact.downloaded` with `delivery: "handover"` where downloads are audited, the request log's `delivery`, and the metrics of
R-STOR-095. A speed figure does not stop a handover (Q-STOR-343). Every data key that leaves is the transactional event
`bastion.data_key.handed`, written before the answer whatever `audit.record_downloads` says; a failed insert streams the download instead
(R-STOR-096). A direct read that fails is replaced by a fetch through the main installation, which counts or charges again
([H-1342](#h-1342--a-handover-whose-direct-read-fails-is-counted-or-charged-again)).

## What this does not cover

- [H-12](../spec/storage.md#h-12--presigned-download-urls-are-bearer-capabilities): a handed link is a bearer for its life, as a client's
  link is.
- [H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks): whoever can write a plain
  connection's bucket replaces content; the Bastion refuses it at the end of the read (H-1345).
- [H-1244](../spec/bastion.md#h-1244--cold-fills-of-large-blobs-hold-budget-slots-and-spool-for-the-length-of-their-transfer): cold fills
  of large blobs; a handed fill holds the spool and no budget slot.
- [H-386](../spec/bastion.md#h-386--whoever-holds-the-bastion-token-can-test-credentials-name-client-addresses-and-add-download-events) and
  [H-446](../spec/bastion.md#h-446--the-connection-to-the-main-installation-is-not-under-the-egress-address-policy): what the holder of the
  Bastion token can do; the connections that the egress address policy does not cover.
- [H-1311](stored-content-keys.md#h-1311--a-leaked-data-key-of-one-blob-has-no-answer-short-of-a-new-connection): a leaked data key has no
  answer short of a new connection; live for the blobs handed to a Bastion.
- [H-1309](stored-content-keys.md#h-1309--a-replicas-memory-holds-the-storage-keys-for-its-life-and-go-cannot-erase-a-key): Go cannot erase
  a key; at a Bastion, the data keys of its current direct reads.
- [H-1283](download-links.md#h-1283--the-transfers-of-links-spend-the-object-stores-own-limits-which-quader-shares-and-does-not-see): the
  Bastions' direct reads spend the store's own limits, as the clients' links do.
- [H-522](bastion.md#h-522--the-chart-restricts-no-egress-of-the-bastion-pods): the chart restricts no egress of the Bastion pods; an
  operator's own policy admits the store's public addresses while the handover is on.

### H-1340 · A taken-over Bastion collects the data keys of the encrypted blobs filled through it

Component: the handover of a data key ([bastion.md](../spec/bastion.md) R-BAS-073, R-BAS-076; [storage.md](../spec/storage.md) R-STOR-095).
Subject: every blob of an encrypted connection that a caller fetched cold through the Bastion while its registration's handover was on.
Dormant: live once the handover is on for a registration and an encrypted connection holds blobs read through it. A Bastion holds a data key
for the length of one read and writes it nowhere; a process that took over a Bastion pod can keep every key it is handed from then on, and
read the keys of the current reads out of the pod's memory. A kept key opens its object's ciphertext wherever it lies, for as long as it
lies there: the connection's bucket, its noncurrent versions, a backup's pool; neither a rotation of the root, nor a new generation of the
storage key, nor the deletion of the blob changes it, and no command writes one blob again under a new key (Q-STOR-347). The plaintext of
those blobs is what such a Bastion reads anyway; the key adds the ciphertext elsewhere, now and later. What makes it real: a taken-over
Bastion together with read access to the connection's bucket, its versions or a backup's target. What bounds it: the trail
`bastion.data_key.handed` names each blob and the registration it went to (R-STOR-096); switching the handover off and revoking the token end
the flow; a new connection filled by the reconciliation with new data keys ends the worth of the kept keys for the live copies, not for
noncurrent versions or backups' pools
([stored-content-keys.md](stored-content-keys.md#h-1311--a-leaked-data-key-of-one-blob-has-no-answer-short-of-a-new-connection)).

### H-1341 · A handed data key and write access to the bucket make segments of that object that a ranged read accepts

Component: the segments of the stored format under a data key that left the main installation ([storage.md](../spec/storage.md) R-STOR-076,
R-STOR-082). Subject: the one object whose data key is held. Dormant: live only when a kept data key (H-1340) meets write access to the
connection's bucket. A segment authenticates under its object's data key, its index and its mark; whoever holds the key can seal other
plaintext of the same length into segments that pass. A whole read refuses such an object at its end, because the final segment or the
plaintext's SHA-256 does not match the digest, and the last bytes are held back (R-STOR-013); a ranged read that does not reach the final
segment cannot hash what it does not read, and serves the forged window: a resumed download, the internal ranges of NuGet's reader, the
format discovery and the signature job. The scrub reads the copy whole within the connection's period and marks it. It is the same case as
[stored-content-format.md](stored-content-format.md#h-1301--a-holder-of-a-storage-key-can-write-objects-that-authenticate) H-1301, narrowed
to one object. What makes it real: a taken-over Bastion that kept keys, together with a credential that can write the bucket.

### H-1342 · A handover whose direct read fails is counted or charged again

Component: the fallback of a failed direct read ([bastion.md](../spec/bastion.md) R-BAS-075). Subject: the download statistics, the audit
trail and the byte budgets of the callers of a Bastion. Dormant: live once the handover is on for a registration whose network or store fails
reads. The main installation counts and charges a handover when it issues it. A direct read that fails before its first byte is replaced by
a forward that the main installation counts and charges again: two downloads in the statistics and the audit, twice the size in the caller's
byte windows. One that fails after it continues with a ranged forward from the spooled length, which counts no second download but charges its
bytes again. For an encrypted copy the trail of the data key holds a key that left although its read failed. What bounds it: the cooldown of a
replica after a failure of the network or the store (60 s), so a Bastion that cannot reach the store causes at most one such double count per
replica and minute, and the alert `QuaderBastionHandoverFailing` reports it. What makes it real: a Bastion whose network does not reach a
connection's public address, or a store that cuts transfers.

### H-1343 · The object store's log shows the Bastion's address and which objects it reads

Component: the object store's request log, its operator's ([bastion.md](../spec/bastion.md) R-BAS-074). Subject: the activity of the
Bastion's callers, as seen by the store's operator. Dormant: live once the handover is on for a registration. A direct read is a request of
the Bastion to the store with the link's signer, the connection's credential: the log holds the Bastion's egress address, the object's name,
the time and the user agent, never Quader's caller. Today the store sees only the main installation's replicas; with the handover its operator
learns where the Bastions stand and which objects are popular through them, on a plain connection by their digests, on a connection with keyed
names by names it cannot map to files. Who received a blob is in the main installation's request log and audit trail (R-STOR-095), which join
the store's log by the object's name and the link's life. What makes it real: a provider whose operator must not learn the DMZ's addresses or
its access pattern.

### H-1344 · A handover shows a Bastion where the main installation's objects lie

Component: the link of a handover ([storage.md](../spec/storage.md) R-STOR-072, R-STOR-095). Subject: the layout of the main installation's
store. Dormant: live once the handover is on for a registration. A link names the connection's public address, the bucket, the prefix, the
object's name and the signing access key's identifier, with temporary credentials its session token; on a connection with keyed names the
object's name and the digest of the handover together give one entry of the name mapping. None of it signs another request or names an object
the Bastion was not handed, but a taken-over Bastion learns the store's address and layout and can try other requests against it. It is
[download-links.md](download-links.md#h-1281--a-link-names-the-stores-address-its-bucket-the-object-key-and-the-signing-keys-id) H-1281
for a Bastion. What makes it real: a store whose bucket or address accepts more than signed requests, such as a listing open to anonymous
callers.

### H-1345 · A changed plain copy cuts the callers of one fill before the main installation learns of it

Component: the direct read of a plain copy ([bastion.md](../spec/bastion.md) R-BAS-074, R-BAS-075). Subject: the callers of a Bastion that
read a blob whose plain object was replaced in the bucket. Dormant: live once the handover is on for a registration whose handovers name a
plain connection. A plain body can be checked only at its end: the Bastion withholds the last 64 KiB from every caller, fails the fill when
the SHA-256 differs, keeps nothing, and marks the cache key so that its next fills go through the main installation, whose verifying read
marks the copy and falls over to another. The callers of that one fill see a cut transfer, as the callers of a forwarded fill of a damaged
object do today, and the main installation learns of the damage only from the next fill, not from the Bastion. What makes it real: a principal
who can write a plain connection's bucket (H-11), or a store that damages an object.
