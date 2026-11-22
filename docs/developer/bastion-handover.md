# The handover per blob to a Bastion: the build plan

Status: specified on 2026-11-22, not built ([bastion.md](../spec/bastion.md#the-handover-per-blob) R-BAS-070 to R-BAS-079, Q-BAS-230 to
Q-BAS-239; [storage.md](../spec/storage.md) R-STOR-095, R-STOR-096, Q-STOR-340 to Q-STOR-347;
[ADR-250](../decisions.md#adr-250--the-handover-per-blob-to-a-bastion-a-link-and-the-data-key-of-one-released-blob-in-the-answer-to-its-forward);
security perspective [security/bastion-handover.md](../security/bastion-handover.md)). This page is the build plan: the five packages, what
each owns, the order against the packages of the signed links ([download-links.md](download-links.md)), of several connections
([blob-store.md](blob-store.md#several-connections-the-build-plan-specified-not-built)) and of the encryption
([stored-content-encryption.md](stored-content-encryption.md)), the tests and what they cost.

## What has to be in your head first

- **A handover is another answer to a forward.** The Bastion offers on a miss (`Quader-Bastion-Handover: accept; max-size=<n>`), and the
  main installation decides the forward as the caller's own request, exactly as today, up to the point where `edge.ServeItem` would open the
  object. There, behind `Admit`, `ServeOptions.Handover` answers either "stream" or a handover; nothing before that point changes.
- **The answer is the file's own `200` without its bytes.** Status, the file's headers through the format's own writers, `Content-Length: 0`,
  the class `handover` and the value in a marking header. The Bastion's client strips every `Quader-Bastion-*` header before the engine sees an
  answer, so the value cannot reach a caller by a defect of the engine.
- **The Bastion's fill gets a second source.** The direct read writes into the same spool, behind the same withheld tail, with the same
  followers and the same commit as a fill from the main installation. The only new things are where the bytes come from, the verification of an
  encrypted body with a handed key, and what happens on a failure.
- **Nothing is per format.** The main installation hands over any stored artifact through one serve point; the Bastion reads artifacts without
  knowing a format. No format's code changes, and no test is per format.
- **A data key leaves only for an encrypted copy, and only after its trace is written.** A plain copy is handed before an encrypted one; the
  transactional event `bastion.data_key.handed` is written before the answer, and a failed insert streams.
- **What the code does not have yet:** `internal/downloadlinks`, `Presign` with options and the link check per connection (download links,
  package 2); `blob.Store.Servable` and the connections (several connections, package A); `blob.Store.CopyDataKey` and encrypted connections
  (encryption, packages 2 and 3). The format's reader the Bastion uses is built (`blobcrypt.KeyFromBytes`, `blobcrypt.DataKey`,
  `blobcrypt.NewReader`).

## Work packages

At most four packages run at once, counting the packages of the other plans that run then. The directories (or, where named, files) of
packages that run at the same time are disjoint.

| Package | After | What it builds | Owns |
|---|---|---|---|
| **1 · The wire, interface version 5** | nothing | `iface.InterfaceVersion` 5; `HeaderHandover`, `iface.HandoverOffer` (format and parse of `accept; max-size=<n>`), `ClassHandover` in `Class.Valid`, `iface.Handover` (encode to base64url JSON, decode, the checks of R-BAS-073 but `storedSize` against `S(size)`, which needs the format and is the engine's), `Handshake.Handover`; `testdata/interface.txt` regenerated; the OpenAPI schema `BastionHandshake.handover` and `x-quader-bastion-interface: 5` with what `make generate` writes from it; the redaction of `Quader-Bastion-Handover` like `Authorization` (R-OPS-013's header rule); `maintest` speaks version 5 and can be told to answer a forward with a handover; this page's "What package 1 left" | `internal/bastion/iface`, `internal/bastion/maintest`, `api/openapi.yaml` (the handshake schema), the redaction rule in `internal/observability` |
| **2 · The main installation's side, plain copies** | 1; download links' package 2; several connections' package A | The registration's member: the migration (`bastion.handover boolean NOT NULL DEFAULT false`), `PATCH`/`GET` of R-BAS-071 and their OpenAPI paths, the events `bastion.handover.enabled` and `bastion.handover.disabled` (journaled) with the journal's replay handler, both in `noFeedEntry` with their reason, R-HA-054's table row for the journaled one (held to the registry by `journal_list_test`), R-OPS-016's catalog rows; the snapshot of the registrations and its change area; the handshake's member; `internal/downloadlinks/handover.go`: the conditions of R-STOR-095 steps 1 to 7, 9 and 11, the copy (plain first), the link without overrides; `ServeOptions.Handover` in `edge.ServeItem` and its callers in `itemStore.Serve` and `/files/`; the marking's class and header (only for a recognized forward that offered); what is counted and charged at issuance, the request log's `delivery: handover`, the metrics of R-STOR-095; the link check's two parts and the settings' `handoverUsable` and `handoverReasons` (R-STOR-096); the test link on an encrypted connection; the integration case with a plain connection | `internal/meta/postgres/migrations` and `queries` (the registration's member), `internal/bastionmain` (`registrations.go`, `feed.go`'s `noFeedEntry`), `internal/audit/events_bastion.go`, `internal/api/v1` (`bastion_registrations_api.go`, `bastion_handshake_api.go`, the settings' answer), `internal/downloadlinks/handover.go` (new), `internal/api/edge` (`content.go`), `internal/formathost` (`ingest.go`), `internal/api/rawfiles` (`handler.go`), `internal/api/bastionmark`, `internal/blob/conformance.go` (the link check's parts), the `package 2` and `package 3` blocks of `internal/app/wire_bastion.go`, `docs/spec/ha-and-helm.md` R-HA-054's row, `docs/spec/operations-and-observability.md` R-OPS-016's rows, `test/integration/bastion/handover_test.go` (new) |
| **3 · The Bastion's side** | 1 (its integration case also 2) | `internal/bastion/handover` (new): the link client of R-BAS-074 (its own `http.Client`, TLS from `bastion.main.ca_file` re-read, the two timeouts, no redirect, no cookie, no credential, no header of the interface, the host refusal), the reader of a plain body and the reader of an encrypted body (`blobcrypt.KeyFromBytes`, `DataKey`, `NewReader` with the digest checked; `S(size)` checked against `storedSize` before the read; `Key.Destroy` at every end); in the engine: the offer (R-BAS-072), the fill whose source is the direct read, the fallback before the first byte, the continuation by `Range` and `If-Range` after it, the mark of a key whose digest failed, the cooldown (a field of the engine, 60 s), the request log's `bastion_handover`; `mainclient.Forward` gains the offer and the Bastion's own `Range` and `If-Range`; the metrics of R-BAS-075, added to R-BAS-040's table in the same change (its test holds the table to `Names()`); the alert `QuaderBastionHandoverFailing` in the chart's rules with its promtool test, its row in R-BAS-041 and its name in the chart test's list; the comment of `trustedCA.existingSecret` in the chart's `values.yaml` and in R-BAS-046's block together; the archtest allows `internal/blob/blobcrypt` for `internal/bastion/handover`; the integration case's Bastion side | `internal/bastion/handover` (new), `internal/bastion/engine`, `internal/bastion/mainclient`, `internal/bastion/metrics`, `internal/archtest/bastion.go`, `deploy/helm/quader-bastion` (`templates/_rules.tpl`, `tests/rules_test.yaml`, the one comment of `values.yaml`), `test/chart/bastion_test.go` (the alert list), `docs/spec/bastion.md` R-BAS-040, R-BAS-041 and R-BAS-046's block |
| **4 · Data keys** | 2 and 3; encryption's packages 2 (the keys, `CopyDataKey`) and 3 (encrypted connections) | R-STOR-095 steps 8 and 10 and the encrypted half of step 7: `CopyDataKey` from `internal/downloadlinks`, the member `dataKey` and `storedSize` as `S(size)`; the architecture rule that allows `CopyDataKey` from `internal/blob` and `internal/downloadlinks` only; the transactional event `bastion.data_key.handed` in a transaction of its own before the answer, a failed insert streaming, its row in R-OPS-016's catalog; the metric's kind `link_and_key`; the integration case with an encrypted connection | `internal/downloadlinks/handover.go`, `internal/archtest` (the rule of `CopyDataKey`), `internal/audit` (the event), `docs/spec/operations-and-observability.md` R-OPS-016's row, `test/integration/bastion/handover_test.go` (the encrypted case) |
| **5 · The surface** | 2 | The card's switch **Direct reads from the object store** with its sentence, the connections' reasons and the link to the storage settings (R-BAS-071), rendered under rule 11 in both schemes, wide and narrow, with a registration that has it on and one that has it off; the manual's section of R-BAS-078 in `docs/manual/admin/bastion.md` and its sizing sentence; the translation source extracted last | the Bastions screen's files under `web/src/app/features/` ([web-ui.md](web-ui.md#the-bastions-screen)), `web/src/locale/`, `docs/manual/admin/bastion.md` |

The order: 1 can start now and lands first. 2 starts when download links' package 2 and several connections' package A are on `main`; 3 runs
beside 2 after 1 (its unit tests need no main installation: the engine's double and `maintest` hand over), and lands after 2 because its
integration case needs both sides. 4 waits for 2, 3 and encryption's packages 2 and 3; until it lands, an encrypted copy is never handed over
(step 7 finds no copy that may be named, and the download streams), so 2 and 3 are a whole feature for plain connections. 5 can be built beside
2 against the answers R-BAS-071 specifies and lands after 2. Packages 2 and 4 both own `internal/downloadlinks/handover.go` and never run at
the same time.

**Where each step of R-STOR-095 is answered:**

| Steps | Where | Reads |
|---|---|---|
| 1 the offer and the switch | `downloadlinks/handover.go` | `bastionmain.RecognitionFrom`, the request's header, the registrations' snapshot |
| 2 an artifact | `formathost` (`rq.Route.Kind == formats.Download`, not `systemRead`) and `rawfiles` set `ServeOptions.Handover` only there | the route |
| 3 content and gates | `ingest.go`, after `serveChecks`, as for links | the item row and the gates' outcome |
| 4 conditionals and ranges, 6 `Admit` | `edge.ServeItem`, as today | the request, the row, the charge |
| 5 size, 7 copy | `downloadlinks/handover.go` | the row's size, the links' snapshot, `blob.Store.Servable`, each connection's handover state |
| 8 data key, 10 trace | `downloadlinks/handover.go` (package 4) | `blob.Store.CopyDataKey`, the audit store |
| 9 credential and signature | `downloadlinks`, shared with the links | the connection's credential, `Presign` with its options |
| 11 issue | `edge.ServeItem` (the `200` without a body), `formathost` and `rawfiles` (`Served`, `AddCached`), `bastionmark` (class, header) | |

## Tests and what they cost

By the rule "Tests stay runnable" of the repository's working rules: every behaviour once, at the cheapest tier; nothing per format; bytes to
kilobytes. Estimates, not measured; each package writes its measured times here.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit | the value, the offer, the class, the interface's file | 1 | under 0.1 s | none |
| Unit | the conditions of R-STOR-095 one by one in one table, the plain copy first, what is counted and charged, the `200` without a body, the marking only on an offer, the switch, its events and replay, the handshake's member, the link check's two parts, the settings' reasons | 2 | about 1.5 s | the in-memory store, objects of 1 KiB |
| Unit | the link client's rules, the plain and the encrypted read with the withheld tail and a follower, the fallback, the continuation, the mark, the cooldown, the data key destroyed and written nowhere, a withdrawal during a read | 3 | about 1.5 s | objects up to 256 KiB, made in memory with the format's writer |
| Chart | the alert's promtool case, firing and silent; the alert in the list | 3 | about 0.2 s | none |
| Unit | the data key from `CopyDataKey`, the trace before the answer, a failed insert streams, the architecture rule | 4 | about 0.4 s | the in-memory store with an encrypted connection, objects of 1 KiB |
| Vitest | the card's switch and reasons | 5 | about 0.5 s | fixtures |
| Integration, area `bastion` | `TestR_BAS_070_ALargeFileIsReadFromTheObjectStore`: the handover on and `minSize` 0, a cold `GET` of a file of 64 KiB below `/files/` through the Bastion, its line `delivery: handover` at the main installation, the Bastion's bytes from the store, a hit; the same with the public address on a closed port, which ends in the fallback | 2 and 3 | about 3 s | one file of 64 KiB |
| Integration, area `bastion` | the same case with an encrypted connection | 4 | about 2 s | one file of 64 KiB |
| Measurement, once before package 3 lands (not a test) | `docker pull` of an image with a layer of about 300 MiB through a Bastion with the handover on, against the local stack: bytes from the main installation against bytes from the store, the time of a cold pull with and without the handover; the numbers written here | 3 | minutes, once | a layer of about 300 MiB, on the development machine only |

What each pipeline job gains: the unit job about 3.7 s; the web unit job about 0.5 s; the chart test about 0.2 s; the integration shard that
holds the area `bastion` about 5 s (it ran 139 s, [test-cost.md](test-cost.md)); every end-to-end leg and the nightly nothing. A change of
the serve point is shared code and starts the npm leg, which gains nothing, since no end-to-end case is added. With 25 formats the cost is the
same.

## Not verified

- Anything against an object store. The integration case uses the stack's SeaweedFS over `https` (the suite's main installation reads
  `QUADER_TEST_S3_ENDPOINT`, `https://127.0.0.1:18333` in [up.sh](../../test/integration/up.sh)); whether the suite's Bastion process can be
  given the stack's CA through `bastion.main.ca_file` without further work was not checked.
- Whether the object gateway of the owner's cluster can be reached from a Bastion's network: it has an address inside the cluster only (read
  there on 2026-11-22 by the development agent), so a handover there needs a public address first, which is his network's decision.
- How large a response header the main installation's ingress lets through. The handover's header is about 3 KiB with a link of temporary
  credentials (an estimate); the bound of HAProxy's buffer for a header block (`tune.bufsize`) was neither read in its documentation for this page nor
  checked on the owner's ingress.
- The throughput of the Bastion's decryption on its hardware; the format's reader was not measured (encryption's page says the same of the
  main installation).
- Whether every store answers a signed `GET` without overrides with `Content-Length` equal to the stored size; the link check's first part
  shows it per connection at start.
