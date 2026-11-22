# The stored format of an encrypted connection: what it authenticates and what it withstands

Status: specified on 2026-11-22 ([storage.md](../spec/storage.md) R-STOR-048, R-STOR-075 to R-STOR-091;
[ADR-244](../decisions.md#adr-244--encrypted-connections-quaders-own-segment-format-bound-to-the-content-the-wrapped-key-once-at-the-object-a-storage-key-per-connection-under-the-key-ring));
the format itself is built ([internal/blob/blobcrypt](../../internal/blob/blobcrypt/doc.go), its vectors checked by an independent
implementation, [stored-content-encryption.md](../developer/stored-content-encryption.md#the-vectors-and-their-independent-check)), and
nothing calls it yet. Until the store writes and reads through it no connection is encrypted, and
[object-store-connections.md](object-store-connections.md) describes what a reader of a bucket can do: read every blob, and replace one that
only a full read then finds.

The perspective of this page is one stored object on an encrypted connection: what each of its parts authenticates, what a reader checks
before and while it hands out bytes, and which changes to the object are refused and where. Who holds the keys is
[stored-content-keys.md](stored-content-keys.md); what a provider still sees is [stored-content-visibility.md](stored-content-visibility.md).

## One object

```
 object metadata (written in the same request as the body)          body (S(L) bytes)
 ┌──────────────────────────────────────────────────────────┐       ┌───────────────────────────────────────────────────────┐
 │ quader-format      quader-blob-v1                         │       │ seg 0    nonce ‖ AES-GCM(K_seg, 64 KiB) ‖ tag   mark 0 │
 │ quader-key         sk-archive-1-3f9a0c2e  (storage key)   │       │ seg 1    …                                       mark 0 │
 │ quader-wrap        salt ‖ nonce ‖ AES-GCM(K_w,            │       │ seg n-1  … (1 to 64 KiB)                         mark 0 │
 │                      DEK ‖ digest ‖ L) ‖ tag              │       │ final    nonce ‖ AES-GCM(K_seg, L ‖ SHA-256) ‖ tag mark 1│
 │ quader-commitment  HKDF(DEK, "commitment")                │       └───────────────────────────────────────────────────────┘
 └──────────────────────────────────────────────────────────┘
        name: <prefix>/keyed/<..>/<..>/HMAC(name key, digest)   (or sha256/<digest> without keyed names)
```

| Part | Authenticates | Under |
|---|---|---|
| A data segment | its plaintext, the format's identifier, its index, that it is not the last | the segment key, derived from the object's data key |
| The final segment | the plaintext's length and SHA-256, the identifier, its index, that it is the last | the segment key |
| The wrap | the data key, the blob's digest and size, the storage key's identifier | a key derived from the storage key with the wrap's own salt |
| The commitment | that the record names exactly one data key | computed from the data key and compared, not sealed |
| `quader-format`, `quader-key` | nothing by themselves: a changed value selects nothing that opens | — |

Nothing binds the object to its name, its bucket or its connection ([storage.md](../spec/storage.md) R-STOR-076): a copy keeps its meaning,
which is what lets the finalize of an upload session and a backup's pool copy objects without opening them. What binds the object to the blob
is the digest inside the wrap and the final segment, compared with the digest the copy row names.

## What a reader checks, and when

| Before the first byte | While the body passes | At the end of a whole read |
|---|---|---|
| the format's identifier; a key of this connection; the wrap opens; its digest and size are the copy's; the stored size is `S(L)` and what the store answered; the commitment | every segment's tag with its index and mark, before its plaintext is released | the final segment, its `L` and digest; the SHA-256 of the plaintext against the digest, the last 64 KiB held back until it matches |

A failure before the first byte falls over to another copy; a failure in the body aborts the response after earlier segments were released,
each of them authenticated ([storage.md](../spec/storage.md) R-STOR-082). A ranged read runs the checks before the first byte and the tags of
its window's segments, so it never hands out a byte that was not authenticated. `storage.download.verify: false` turns off only the SHA-256 of
the plaintext; the tags are not optional.

## What it withstands

Each row is a unit test of the format ([storage.md](../spec/storage.md) R-STOR-078; [attacks_test.go](../../internal/blob/blobcrypt/attacks_test.go),
whole and ranged, with a reader that counts what left before the refusal) and a negative case of the vector file; the attacker can write the
bucket and holds no storage key. A record whose `quader-key` is missing or no identifier is `record`, not `unknown_key`
([Q-STOR-322](../spec/storage.md#q-stor-322--a-missing-or-malformed-quader-key-is-record-not-unknown_key)): no import can bring such a key
back, so the copy is repaired.

| Change to the object | Refused by | Where |
|---|---|---|
| Truncated, extended, a segment dropped | the stored size against `S(L)`; the index and the final mark | before the first byte |
| Segments reordered, a data segment passed off as final or the reverse | index and mark in each segment's additional data | at that segment |
| A segment, or a whole object, of another blob | another data key; the wrap's digest | at that segment; before the first byte |
| The record of another object on this body | the wrap's digest | before the first byte |
| Any field of the record changed | the identifier, the wrap's tag, the commitment | before the first byte |
| A record made under another connection's or installation's key | the identifier is not this connection's, or the wrap does not open | before the first byte |
| A ciphertext made to open under two data keys | the commitment | before the first byte |
| Parts of two sealings of one upload spliced | the final segment's SHA-256 and the reader's digest | at the end of a whole read |
| An older genuine object of the same content put back | not refused, and harmless: the same plaintext under a key of this connection | — |

The proxy whose segment chain was the model (a third project's tool, at its release 5.4.2, run against on 2026-11-22) refuses the same changes
except that its segments are bound to the object's name, it marks no last segment, and it has no commitment; the research found it accepts an
older version under the same name, as this format does.

## What this does not cover

- [H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks) stays whole on every connection
  that is not encrypted.
- Who can open a record at all, and what a lost or leaked key costs: [stored-content-keys.md](stored-content-keys.md#what-this-does-not-cover).
- What the object still tells a provider (sizes, times, names without keyed names):
  [stored-content-visibility.md](stored-content-visibility.md#what-this-does-not-cover).

### H-1300 · A ranged read does not see damage outside its window

Component: the ranged read of an encrypted copy ([storage.md](../spec/storage.md) R-STOR-082). Subject: every blob served by ranges (resumed
downloads, NuGet's and the discovery's internal ranges, the signature job). A ranged read authenticates the segments it reads, so it never
serves a changed byte; a change in another segment, or a cut-off end that its window does not reach, is not seen by it, and the copy stays
`present` until a whole read or the scrub reads it (within the connection's `scrub_period`, 30 days by default). Live on every encrypted
connection; it costs availability of a damaged copy later, never wrong bytes now. Reading the whole object for a range would close it at the
price of the range.

### H-1301 · A holder of a storage key can write objects that authenticate

Component: the record and the segments ([storage.md](../spec/storage.md) R-STOR-076, R-STOR-077). Subject: every blob of the connection whose
storage key is held. Whoever holds a storage key and can write the bucket can encrypt other bytes under a data key of his own and wrap it bound
to the right digest: every tag then passes. A whole read still refuses the object at its end, because the plaintext's SHA-256 is not the digest
(the last 64 KiB are held back); a ranged read serves it, since it cannot hash what it does not read. Dormant until a storage key leaks (its
holders are the pods of [stored-content-keys.md](stored-content-keys.md)); the answer to a leak is a new connection filled with new data keys
([storage.md](../spec/storage.md) R-STOR-085).

### H-1302 · The commitment binds a record to one data key, not a body to one record

Component: the commitment ([storage.md](../spec/storage.md) R-STOR-077, Q-STOR-304). Subject: a body that more than one record or reader
could stand for. The commitment makes each record name exactly one data key; it does not make the body name one, because it lies in the
metadata. In this stage every copy has its own data key and record, and only Quader makes records, so no body has two. It becomes real when a
body is read under more than one record or key: the handover of a data key to a Bastion (decision 12 (b), a later planner) and a later level of
keys per project (decision 3). Dormant; the later planner decides whether a commitment in the body is needed then.

### H-1303 · A copy of an encrypted bucket that drops the metadata cannot be read

Component: the record in the object's metadata (the owner's decision 2 (e), [storage.md](../spec/storage.md) R-STOR-077). Subject: copies of a
bucket made outside Quader. A tool that copies objects to a file system or another store without their user metadata (many do by default when
the target is a disk) loses every record and with it every data key: the copy is ciphertext nobody can open. Quader's own copies keep it (the
reconciliation writes records, a backup's pool keeps them, `quader admin storage offline pull` writes each record beside its object). Live for
every operator who backs up an encrypted bucket with a tool of his own; the manual says to use `offline pull` or a copy that keeps metadata.
