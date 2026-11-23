# The handover per blob to a Bastion: the build plan

Status: specified on 2026-11-22; package 1 (the wire, interface version 6), package 2 (the main installation's side, plain copies),
package 3 (the Bastion's side), package 4 (data keys) and package 5 (the surface) **built** on 2026-11-23
([What package 2 left](#what-package-2-left), [What package 3 left](#what-package-3-left), [What package 5 left](#what-package-5-left),
[What package 4 left](#what-package-4-left)); the integration case that joins the two sides, and its twin on an encrypted connection,
**built** on 2026-11-23 ([Tests and what they cost](#tests-and-what-they-cost)); package 4 was joined with package 5 and the surface brought
to the data keys in the same landing
([bastion.md](../spec/bastion.md#the-handover-per-blob) R-BAS-070 to R-BAS-079, Q-BAS-230 to Q-BAS-239, Q-BAS-280, Q-BAS-281;
[storage.md](../spec/storage.md) R-STOR-095, R-STOR-096, Q-STOR-340 to Q-STOR-347;
[ADR-250](../decisions.md#adr-250--the-handover-per-blob-to-a-bastion-a-link-and-the-data-key-of-one-released-blob-in-the-answer-to-its-forward);
security perspective [security/bastion-handover.md](../security/bastion-handover.md)). This page is the build plan: the five packages, what
each owns, the order against the packages of the signed links ([download-links.md](download-links.md)), of several connections
([blob-store.md](blob-store.md#several-connections-the-build-plan)) and of the encryption
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
- **The wire is built (package 1).** [handover.go](../../internal/bastion/iface/handover.go) holds the offer, the value, the data key's type
  and every check of R-BAS-073; [What package 1 left](#what-package-1-left) says what packages 2 and 3 take from it.
- **The main installation hands plain copies over (package 2).** The registration's switch, the snapshot of it, the decision in
  [downloadlinks/handover.go](../../internal/downloadlinks/handover.go), the `200` without a body and the marking.
  [What package 2 left](#what-package-2-left) says what packages 3, 4 and 5 take from it.
- **And encrypted copies with their data key (package 4).** Where no plain copy can serve, the first encrypted one is named; its link is
  signed, then `CopyDataKey` opens that copy's record (one `HeadObject`, no statement), the value is encoded and the key destroyed, and
  `bastion.data_key.handed` is committed before the value is noted for the marking. [What package 4 left](#what-package-4-left) says what
  package 5 and the key destruction take from it.
- **The Bastion's side is built (package 3).** A Bastion offers on every cold `GET` while its handshake says `handover: true`, reads a handed
  plain or encrypted copy with the link client of [internal/bastion/handover](../../internal/bastion/handover), and falls back or continues
  through the main installation when the read fails; [What package 3 left](#what-package-3-left) says what packages 2, 4 and 5 take from it.
  A Bastion offers only while its handshake says `handover: true`, which a main installation says once an administrator has switched the
  registration's handover on; until then nothing is offered and every fill comes from the main installation as before. The two sides were
  built apart, each against a double of the other; the integration case that joins them, `TestR_BAS_070_ALargeFileIsReadFromTheObjectStore`,
  runs them together with the stack's object store (2026-11-23): they meet as each expected the other; the look at the hosts' check found
  that a refused value did not start the cooldown, fixed on the Bastion's side
  ([Q-BAS-293](../spec/bastion.md#q-bas-293--a-value-the-bastion-refuses-starts-the-cooldown)).
- **The surface is built (package 5).** The card's switch **Direct reads from the object store** in
  [bastion-handover.ts](../../web/src/app/features/bastions/bastion-handover.ts), the connections a Bastion cannot read under it, the
  storage settings' test link of an encrypted connection and the manual's section
  [Direct reads from the object store](../manual/admin/bastion.md#direct-reads-from-the-object-store); [What package 5 left](#what-package-5-left)
  says what package 4 changes there, and [What package 4 left](#what-package-4-left) what the join changed.
- **What it builds on, all on `main`:** encrypted connections with their jobs, backup and restore (encryption, complete on 2026-11-23); the
  signed links (`internal/downloadlinks`, `Connection.Presign` with `PresignOptions`, `LinkCheck.Works`, `ConnectionState.HandoverUsable`);
  `blob.Store.Servable` and the connections (several connections, package A); `blob.Store.CopyDataKey` with the architecture rule of its
  callers (encryption, package 2; its signature changed by package 4, Q-STOR-470); the format's reader the Bastion uses (`blobcrypt.KeyFromBytes`,
  `blobcrypt.DataKey`, `blobcrypt.NewReader`).

## Work packages

At most four packages run at once, counting the packages of the other plans that run then. The directories (or, where named, files) of
packages that run at the same time are disjoint.

| Package | After | What it builds | Owns |
|---|---|---|---|
| **1 · The wire, interface version 6** — **built** on 2026-11-23 | nothing; it lands after package P1 of [bastion-package-shapes.md](bastion-package-shapes.md), which takes version 5 | `iface.InterfaceVersion` 6; `HeaderHandover`, `iface.HandoverOffer` (format and parse of `accept; max-size=<n>`), `ClassHandover` in `Class.Valid`, `iface.Handover` (encode to base64url JSON, decode, every check of R-BAS-073, `storedSize` against `S(size)` included: *amended on 2026-11-23*, [Q-BAS-280](../spec/bastion.md#q-bas-280--the-wire-checks-storedsize-against-ssize-itself)), `Handshake.Handover`; `testdata/interface.txt` regenerated; the OpenAPI schema `BastionHandshake.handover` and `x-quader-bastion-interface: 6` with what `make generate` writes from it; the redaction of `Quader-Bastion-Handover` like `Authorization` (R-OPS-013's header rule); `maintest` speaks version 6 and can be told to answer a forward with a handover; this page's "What package 1 left" | `internal/bastion/iface`, `internal/bastion/maintest`, `api/openapi.yaml` (the handshake schema), the redaction rule in `internal/observability` |
| **2 · The main installation's side, plain copies** — **built** on 2026-11-23 | 1; download links' package 2; several connections' package A | The registration's member: the migration (`bastion.handover boolean NOT NULL DEFAULT false`), `PATCH`/`GET` of R-BAS-071 and their OpenAPI paths, the events `bastion.handover.enabled` and `bastion.handover.disabled` (journaled) with the journal's replay handler, both in `noFeedEntry` with their reason, R-HA-054's table row for the journaled one (held to the registry by `journal_list_test`), R-OPS-016's catalog rows; the snapshot of the registrations and its change area; the handshake's member; `internal/downloadlinks/handover.go`: the conditions of R-STOR-095 steps 1 to 7, 9 and 11, the copy (plain first), the link without overrides; `ServeOptions.Handover` in `edge.ServeItem` and its callers in `itemStore.Serve` and `/files/`; the marking's class and header (only for a recognized forward that offered); what is counted and charged at issuance, the request log's `delivery: handover`, the metrics of R-STOR-095; the link check's two parts and the settings' `handoverUsable` and `handoverReasons` (R-STOR-096); the test link on an encrypted connection; the integration case with a plain connection | `internal/meta/postgres/migrations` and `queries` (the registration's member), `internal/bastionmain` (`registrations.go`, `feed.go`'s `noFeedEntry`), `internal/audit/events_bastion.go`, `internal/api/v1` (`bastion_registrations_api.go`, `bastion_handshake_api.go`, the settings' answer), `internal/downloadlinks/handover.go` (new), `internal/api/edge` (`content.go`), `internal/formathost` (`ingest.go`), `internal/api/rawfiles` (`handler.go`), `internal/api/bastionmark`, `internal/blob/conformance.go` (the link check's parts), the `package 2` and `package 3` blocks of `internal/app/wire_bastion.go`, `docs/spec/ha-and-helm.md` R-HA-054's row, `docs/spec/operations-and-observability.md` R-OPS-016's rows, `test/integration/bastion/handover_test.go` (new) |
| **3 · The Bastion's side** — **built** on 2026-11-23 | 1 (its integration case also 2) | `internal/bastion/handover` (new): the link client of R-BAS-074 (its own `http.Client`, TLS from `bastion.main.ca_file` re-read, the two timeouts, no redirect, no cookie, no credential, no header of the interface, the host refusal), the reader of a plain body and the reader of an encrypted body (`blobcrypt.KeyFromBytes`, `DataKey`, `NewReader` with the digest checked; `S(size)` checked against `storedSize` before the read; `Key.Destroy` at every end); in the engine: the offer (R-BAS-072), the fill whose source is the direct read, the fallback before the first byte, the continuation by `Range` and `If-Range` after it, the mark of a key whose digest failed, the cooldown (a field of the engine, 60 s), the request log's `bastion_handover`; `mainclient.Forward` gains the offer and the Bastion's own `Range` and `If-Range`; the metrics of R-BAS-075, added to R-BAS-040's table in the same change (its test holds the table to `Names()`); the alert `QuaderBastionHandoverFailing` in the chart's rules with its promtool test, its row in R-BAS-041 and its name in the chart test's list; the comment of `trustedCA.existingSecret` in the chart's `values.yaml` and in R-BAS-046's block together; the archtest allows `internal/blob/blobcrypt` for `internal/bastion/handover`; the integration case's Bastion side | `internal/bastion/handover` (new), `internal/bastion/engine`, `internal/bastion/mainclient`, `internal/bastion/metrics`, `internal/archtest/bastion.go`, `deploy/helm/quader-bastion` (`templates/_rules.tpl`, `tests/rules_test.yaml`, the one comment of `values.yaml`), `test/chart/bastion_test.go` (the alert list), `docs/spec/bastion.md` R-BAS-040, R-BAS-041 and R-BAS-046's block |
| **4 · Data keys** — **built** on 2026-11-23 | 2 and 3; encryption's packages 2 (the keys, `CopyDataKey`) and 3 (encrypted connections) | R-STOR-095 steps 8 and 10 and the encrypted half of step 7: `CopyDataKey` from `internal/downloadlinks`, the member `dataKey` and `storedSize` as `S(size)` (the architecture rule that allows `CopyDataKey` from `internal/blob` and `internal/downloadlinks` only is built by encryption's package 2, `CheckDataKeyCallers`); the transactional event `bastion.data_key.handed` in a transaction of its own before the answer, a failed insert streaming, its row in R-OPS-016's catalog; the metric's kind `link_and_key`; the integration case with an encrypted connection; in the join with package 5, the surface's texts of an encrypted connection brought to the data keys | `internal/downloadlinks/handover.go`, `internal/audit` (the event), `docs/spec/operations-and-observability.md` R-OPS-016's row, `test/integration/bastion/handover_test.go` (the encrypted case); in the join, package 5's files named in [What package 4 left](#what-package-4-left) |
| **5 · The surface** — **built** on 2026-11-23 | 2 | The card's switch **Direct reads from the object store** with its sentence, the connections' reasons and the link to the storage settings (R-BAS-071), rendered under rule 11 in both schemes, wide and narrow, with a registration that has it on and one that has it off; the manual's section of R-BAS-078 in `docs/manual/admin/bastion.md` and its sizing sentence; the translation source extracted last | the Bastions screen's files under `web/src/app/features/` ([web-ui.md](web-ui.md#the-bastions-screen)), `web/src/locale/`, `docs/manual/admin/bastion.md` |

The order: 1 can start now and lands first of these five, after package P1 of the read shapes per package, which takes version 5 ([Q-BAS-251](../spec/bastion.md#q-bas-251--the-version-numbering-against-the-handover); *amended on 2026-11-22: 1 took version 5*). 2 starts when download links' package 2 and several connections' package A are on `main`; 3 runs
beside 2 after 1 (its unit tests need no main installation: the engine's double and `maintest` hand over), and lands after 2 because its
integration case needs both sides. 4 waits for 2, 3 and encryption's packages 2 and 3; until it landed, an encrypted copy was never handed over
(step 7 found no copy that may be named, and the download streamed), so 2 and 3 were a whole feature for plain connections. 5 can be built beside
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

## What package 1 left

Built in [handover.go](../../internal/bastion/iface/handover.go), [maintest.go](../../internal/bastion/maintest/maintest.go),
[client.go](../../internal/bastion/mainclient/client.go) and [redact.go](../../internal/observability/redact.go); the as-built note is under
R-BAS-073.

| For | What to use | What to know |
|---|---|---|
| 2 · the main installation | `iface.HandoverOfferFrom(r.Header)` for the offer; `Handover{…}.Set(w.Header())` (or `EncodeHandover`) for the value; `iface.ClassHandover` in the marking; `Handshake.Handover` in the handshake | `Set` refuses what the Bastion would refuse on the value's form (an `http` link, user information, a wrong `storedSize`, a key on a plain copy, more than 8 KiB): a refusal streams the download. The offer's rule is strict (one spelling, one value); anything else is no offer. A key enters through `iface.NewHandoverKey(raw)`, which copies it: clear `raw`. `Handshake.Handover` is a plain `bool` and the handshake writes `false` until the registration's member feeds it. The header is redacted already, as a header and as text. |
| 3 · the Bastion | `iface.HandoverFrom(resp.Header)` before `StripMarking`; then `h.Check(iface.HandoverCheck{Now, Offer, ETag, RefusedHosts})`; `iface.HandoverOffer{MaxSize: n}.Set(req.Header)` for the offer; `h.DataKey.Bytes()` into `blobcrypt.KeyFromBytes`, then clear the copy and `h.DataKey.Destroy()` | `mainclient.accept` now refuses every answer of the class `handover`, and every answer that carries the header, as `ErrBadAnswer`, because the client offers nothing yet: package 3 replaces that refusal by "only when the forward offered", and keeps it for every forward that did not. `Forward` strips the caller's `Quader-*` headers, so the offer must be set after the copy of the caller's headers. `ETagContentAddress` is the engine's `contentAddress` of [forward.go](../../internal/bastion/engine/forward.go) in the interface: the engine may switch to it. `RefusedHosts` takes host names (`url.URL.Hostname`), compared case-insensitively, without a trailing dot and whatever the port. A `Handover` and a `HandoverKey` print, log and marshal without the link and the key, so a log line may carry the value; the link client's own errors are its own to keep free of the link. |
| tests of 2 and 3 | `maintest.Main.SetHandover`, `Main.HandOver(w, r, marking, handover)`, `Forwarded.Offer()` | `HandOver` hands over only while the switch is on, the forward offered and the size is within the offer, and writes the class, the counted download, `Content-Length: 0` and the value; headers set before it (the file's `ETag`) stay. |

## What package 2 left

Built in [downloadlinks/handover.go](../../internal/downloadlinks/handover.go), [bastionmain/handover.go](../../internal/bastionmain/handover.go),
[registrations.go](../../internal/bastionmain/registrations.go), [recognize.go](../../internal/bastionmain/recognize.go),
[absence.go](../../internal/bastionmain/absence.go), [mark.go](../../internal/api/bastionmark/mark.go),
[content.go](../../internal/api/edge/content.go), [formathost/download_links.go](../../internal/formathost/download_links.go) and
[rawfiles/handler.go](../../internal/api/rawfiles/handler.go); the as-built notes are under R-BAS-070, R-BAS-071, R-BAS-073 and storage.md
R-STOR-095.

| For | What to use | What to know |
|---|---|---|
| 3 · the Bastion | the handshake's `handover` (the main installation's snapshot of the switch, at most 2 s behind a `PATCH`); `TestR_STOR_095_AForwardThatOfferedIsHandedOverFromTheStacksStore` was the main installation's half of `TestR_BAS_070_ALargeFileIsReadFromTheObjectStore` and is replaced by it (2026-11-23), which took its set-up (`download_link_settings` with `min_size` 0 and the public address `https://localhost:<the store's port>`, the registration and its switch through the API, a clean verdict) the Bastion's case can take | The offer is read only from a forwarded `GET` and only into `Recognition`: the main installation takes the header out of every request, so an offer on a `HEAD` or an interface call is no offer. A handover answer is the file's `200` with `Content-Length: 0`, the file's own `ETag` and the format's headers, `Quader-Bastion-Counted: download` and, where the request named one package, the package headers of R-BAS-082. An answer to an offer can still be the bytes (any condition, the switch off within the snapshot's 2 s, a blob above `max-size` or below `minSize`, an encrypted copy) or Quader's own `304`, `206`, `412`, `429` or refusal: only the class `handover` is a handover. The main installation has counted the download and charged the size when it answered; a fallback forward is counted again (H-1342) |
| 4 · data keys | `Links.handover`'s loop: the `case c.Encrypted(): continue` is where an encrypted copy is skipped today; Q-STOR-341 wants the plain copies first and the first encrypted one after them, so the loop becomes two passes. `HandoverOptions` notes `given` for `HandedOver`, which records `Encrypted` and the connection in `artifact.downloaded`; the metric's kind is `HandoverKindLink` and gains `link_and_key`; the declined reasons gain `key` and `trace` | The value is encoded by `iface.EncodeHandover` in `handover`'s caller; the data key must be destroyed after the encoding and on every path that streams. The trace's insert must come before `bastionmain.NoteHandover`, which is the point after which the answer leaves |
| 5 · the surface | `GET /api/v1/bastions[/{name}]` show `handover`; `PATCH` with `{"handover": true\|false}` and `If-Match`; a `null` switches nothing; the connections' `handoverUsable` and `handoverReasons` are in `GET /api/v1/download-links/settings` | The settings page offers the test link for the plain connections only (`plainConnections` in [storage-settings.ts](../../web/src/app/features/settings/storage-settings.ts)) although `POST /api/v1/download-links/test` takes an encrypted one (R-STOR-096): the Bastions screen's link to the storage settings needs it for encrypted connections too. The two audit labels exist in [audit-labels.ts](../../web/src/app/features/audit/audit-labels.ts); their translation units were added by hand without an extraction (no `node_modules` in the builder's worktree) |

## What package 3 left

Built in [handover/client.go](../../internal/bastion/handover/client.go) and [read.go](../../internal/bastion/handover/read.go) (the link
client and the two readers), [engine/handover.go](../../internal/bastion/engine/handover.go) (the offer, `onHandover`, the fallback, the
continuation, the mark, the cooldown), [engine/fill.go](../../internal/bastion/engine/fill.go) (`newFillRun`, `runFill`, the run's turn to
the main installation), `takeHandover` in [mainclient/client.go](../../internal/bastion/mainclient/client.go), `link.State.Handover`, the four
metrics, `cache.Cache.SpoolRoom`, [maintest/store.go](../../internal/bastion/maintest/store.go) and the alert; the as-built notes are under
R-BAS-072 to R-BAS-078, the decisions [Q-BAS-290](../spec/bastion.md#q-bas-290--a-handover-counts-one-outcome-by-the-path-it-took) to
[Q-BAS-293](../spec/bastion.md#q-bas-293--a-value-the-bastion-refuses-starts-the-cooldown) (Q-BAS-293 since the joined case).

| For | What to know |
|---|---|
| 2 · the main installation | The Bastion offers on its forwards of a miss and on its own conditional `GET`, never on a `HEAD`, a probe, a core route of `/v2/`, a follower's conditional request, a continuation or the forward that replaces a failed handover; `max-size` is the smaller of `max_object_bytes` and the spool's room at that moment. It checks the value with `Handover.Check` against the hosts of its `server.base_url`, `bastion.main.url` and `bastion.main.packages_url` **by name, whatever the port**: a connection's public address on the same host name as the main installation's URL is refused and every handover falls back. Its continuation is the caller's forward with `Range: bytes=<n>-` and `If-Range: <the ETag>` and no offer; it accepts only a `206` with `Content-Range: bytes <n>-<size − 1>/<size>` and the same `ETag`, and `n` is never 0. A handover answer's body is closed unread at once. The joined integration case is built: `TestR_BAS_070_ALargeFileIsReadFromTheObjectStore` in [handover_test.go](../../test/integration/bastion/handover_test.go) runs the suite's main installation and a Bastion role with the stack's CA and the front's CA in one bundle of `bastion.main.ca_file` (`bastionOptions.CAFile`), and the default connection's public address on a relay (`storeRelay`) on `localhost`, which is not the host of the Bastion's `bastion.main.url` (the front, `127.0.0.1`). It found the value's encoding, the hosts' check, the `ETag`, the empty answer and the package headers of a handover answer as the two sides expect them. A value the Bastion refuses (a link on its refused hosts, as an unset public address gives in the stack, or an expired one) now starts the cooldown (Q-BAS-293): before, every miss cost the main installation a second forward and a second counted download. |
| 4 · data keys | The Bastion takes a key only with `"encrypted": true`; it checks `storedSize` against `blobcrypt.StoredSize(size)` itself, builds the key with `blobcrypt.KeyFromBytes`, destroys the `HandoverKey` at once and the format's key at every end of the read (`Client.KeysHeld` is 0 whenever no encrypted read runs). A body under another key fails at its first segment and falls back without the cooldown; the trail `bastion.data_key.handed` therefore names keys whose reads fell back. The encrypted read is tested against `maintest.Store` with objects sealed by `blobcrypt.NewSection`, not against a main installation's encrypted connection: that is package 4's integration case, which belongs beside the joined case in [handover_test.go](../../test/integration/bastion/handover_test.go) and can take its set-up: `storeRelay` as the public address (it keeps the link's `Host`, counts the bytes per object and can refuse or cut one) and `bastionOptions.CAFile` with the bundle. |
| 5 · the surface | The Bastion's chart has the alert `QuaderBastionHandoverFailing` and no value. The manual's section should say that the connections' public addresses need a host name of their own (above), that `trustedCA` vouches for the object store too, that a fallback's reason is in the Bastion's log line "a handover is replaced by the forward without the offer" ([H-1445](../spec/bastion.md#h-1445--the-stores-answers-to-a-bastions-direct-reads-are-counted-only-as-outcomes)), and the sizing: the offer is bounded by `max_object_bytes` and by the spool's room at the moment of the miss. For the manual's "when to switch it on": in the measurement of 2026-11-23 a blob of 300 MiB cost the main installation no byte with the handover on and its full size with it off ([Tests and what they cost](#tests-and-what-they-cost)). An unset public address names the connection's endpoint, which the Bastion refuses when it has the host name of `bastion.main.url`. |

## What package 5 left

Built in [bastion-handover.ts](../../web/src/app/features/bastions/bastion-handover.ts) (the switch, `handoverLines`, `handoverReasonText`),
[bastion-list.ts](../../web/src/app/features/bastions/bastion-list.ts) (`loadConnections`), `setHandover` of
[bastions-api.ts](../../web/src/app/api/bastions-api.ts), [storage-settings.ts](../../web/src/app/features/settings/storage-settings.ts) and its
template (every connection's address and test link, the fragment `details`), the walks `bastionsHandover` and `bastionsRead` of
[renders/bastions.ts](../../web/e2e/renders/bastions.ts) and the state `encrypted-test` of `storageSettings`, and the manual's section; the as-built
notes are under R-BAS-071 and R-BAS-078, the decisions
[Q-BAS-295](../spec/bastion.md#q-bas-295--the-card-names-the-connections-a-bastion-cannot-read-and-only-those),
[Q-BAS-296](../spec/bastion.md#q-bas-296--until-the-data-keys-are-handed-the-cards-sentence-names-no-key-and-an-encrypted-connection-has-its-line) and
[Q-UI-958](../spec/api-and-ui.md#q-ui-958--an-encrypted-connection-has-its-address-and-its-test-link-for-a-bastion-and-says-what-it-serves-today).

| For | What changes when it lands |
|---|---|
| 4 · data keys | The texts that say an encrypted connection's files go through the main installation are true until then and go with it, in the same change: the card's sentence gains ", and for an encrypted store with that file's key" (`@@bastion.handover.note`); `handoverLines` loses its branch `c.encrypted`, so an encrypted connection is judged by its `handoverUsable` and `handoverReasons` like a plain one, and the clause `@@bastion.handover.reason.encrypted` goes; the storage settings' field of an encrypted connection loses its last sentence, "For now a Bastion gets the files of an encrypted connection through the main installation." (`@@links.address.hint-encrypted`); the manual's paragraph **Encrypted connections** of [admin/bastion.md](../manual/admin/bastion.md#direct-reads-from-the-object-store) says that such a file's key goes with its link, and its list **What you take on** gains the data key (H-1340, H-1341) and, under a Bastion taken over, the new connection of R-BAS-078; the row **Public address of** of [admin/download-links.md](../manual/admin/download-links.md#the-settings-page) loses "for now". The tests that hold the old texts are in `bastions.spec.ts` and `storage-settings.spec.ts` |

## What package 4 left

Built in [downloadlinks/handover.go](../../internal/downloadlinks/handover.go) (`handoverCopy`, `dataKey`, `trace`, `destroy`, `KeysHeld`, the
two reasons and the kind), [links.go](../../internal/downloadlinks/links.go) (`Options.DataKeys`, `Runner`, `Audit`),
[datakey.go](../../internal/blob/datakey.go) (`CopyDataKey(ctx, digest, size, copy)`, Q-STOR-470),
[events_bastion_datakey.go](../../internal/audit/events_bastion_datakey.go) (the event), the wiring in
[wire_downloadlinks.go](../../internal/app/wire_downloadlinks.go), and `HandoverDownload`'s caller filled by
[formathost/download_links.go](../../internal/formathost/download_links.go) and [rawfiles/handler.go](../../internal/api/rawfiles/handler.go);
the as-built notes are under storage.md R-STOR-095 and R-STOR-096 and bastion.md R-BAS-070 and R-BAS-073, the decisions
[Q-STOR-470](../spec/storage.md#q-stor-470--copydatakey-opens-the-copy-the-requests-lookup-read-and-reads-no-row) to
[Q-STOR-472](../spec/storage.md#q-stor-472--the-value-is-encoded-before-the-trace-and-the-key-destroyed-before-the-insert), the gap
[H-1490](../security/bastion-handover.md#h-1490--the-encoded-value-keeps-a-handed-data-key-in-the-main-installations-memory-until-the-collector-reclaims-it).

| For | What to know |
|---|---|
| 5 · the surface | An encrypted connection's files now reach a Bastion by a link and that file's data key, where no plain copy can serve and the registration's switch is on; every text that says they come through the main installation is false once this lands (the sentences are listed in the report of package 4 and below). The connections' `handoverUsable` and `handoverReasons` are unchanged: an encrypted connection that passes the first part of its link check can serve a handover, which it could before, only nothing named it. The test link of the storage settings is still offered for plain connections only (`plainConnections` in [storage-settings.ts](../../web/src/app/features/settings/storage-settings.ts)), so an administrator cannot yet test from the Bastion's network the address an encrypted connection's handover names (R-STOR-096 asks for it). The audit label of `bastion.data_key.handed` ("Data key of a file handed to a Bastion") and its two translation units were added by hand without an extraction: the location notes of the labels after it in [audit-labels.ts](../../web/src/app/features/audit/audit-labels.ts) are two lines off until the next extraction. Sentences on `main` that this package makes false or incomplete: [download-links.md](../manual/admin/download-links.md) "An encrypted connection never serves a link: Quader decrypts and streams its content." and its table row "The connection is encrypted / Nothing: Quader delivers its content itself", [configuration.md](../manual/admin/configuration.md) `storage.connections[].encrypted` "It costs: no signed links from this connection", and the storage settings' reason `links.reason.encrypted` "The connection … is encrypted. Quader delivers its content itself." ([storage-settings.ts](../../web/src/app/features/settings/storage-settings.ts)): each holds for the clients' links and no longer for a Bastion with the handover on. |
| the key destruction (encryption's package 7) | A data key that `CopyDataKey` cannot open (a storage key the replica does not hold today, `unknown_key`; a destroyed one once package 7 lands, its reason `destroyed_key`) streams the download with the reason `key` and writes no trace; nothing here changes when the reason is added, since every error of `CopyDataKey` is `key`. The dry run's count of "data keys handed to a Bastion" reads the events `bastion.data_key.handed` of the connection's storage keys: `details.storageKeyId` names the storage key that wrapped each, `details.connection` the connection, `resource_id` the registration. |
| the query budget | A handover of an encrypted copy costs two statements, `GetLiveItem` and `InsertAuditEvents` (the trace's transaction), measured once on 2026-11-23 with the area's harness and a replica whose one connection was encrypted; the case was not kept (a measurement), and the unit tier holds both halves: `CopyDataKey` runs no transaction (`TestR_STOR_083_CopyDataKeyOpensTheRecordOfTheCopy`), the handover one (`TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey`). R-ARCH-027's budget names the line. |

## Tests and what they cost

By the rule "Tests stay runnable" of the repository's working rules: every behaviour once, at the cheapest tier; nothing per format; bytes to
kilobytes. Estimates, not measured; each package writes its measured times here.

| Tier | What | Package | Time | Data |
|---|---|---|---|---|
| Unit | the value, the offer, the class, the interface's file, the key rendered nowhere, the copies of `S` and of the connection pattern (`internal/bastion/iface`); the redaction (`internal/observability`); the client and the fake at version 6 (`internal/bastion/mainclient`) | 1 | **measured** 0.08 s under the race detector with atomic coverage (iface 0.01 s, observability 0.01 s, mainclient 0.06 s) | values of hundreds of bytes; one link of 6 KiB for the bound of 8 KiB |
| Unit | the conditions of R-STOR-095 one by one in one table, the plain copy first and an encrypted one streamed, what is counted and charged, the `200` without a body, the marking only on an offer, the offer read by the recognition and the header removed, the switch's operations, snapshot, events and replay handler, the handshake's member (the link check's two parts and the settings' reasons were built with the links' package 2) | 2 | **measured** 0.90 s under the race detector with atomic coverage: `TestR_STOR_095_TheConditionsOfAHandover` 0.15 s (28 cases), `TestR_BAS_071_TheOperationsOfTheSwitchAndTheHandshake` 0.74 s (six servers of the API document), `TestR_BAS_073_OnlyARecognizedForwardThatOfferedGetsAHandover` 0.01 s, the seven others under 0.01 s each | the in-memory store, objects of 21 to 64 bytes |
| Integration, area `bastionreg` | `TestR_BAS_071_TheSwitchIsPerRegistration`: the `PATCH` with `endpoints:write` and an auditor's `403`, the revision, both events and the journal row, the nulls, the snapshot following the change area, the replay against PostgreSQL | 2 | **measured** 0.95 s | one registration |
| Integration, area `bastion` | `TestR_STOR_095_AForwardThatOfferedIsHandedOverFromTheStacksStore`: the main installation's half of the case below, with the forward sent by the test — **removed** on 2026-11-23: the joined case shows its handover answer, its link, its request line and its metric through a real Bastion, and took its two checks of its own (an offered forward streamed with the switch off, an ordinary request with the offer) | 2 | was 1.94 s | was one file of 64 KiB |
| Integration, area `querybudget` | a handed-over OCI blob of a recognized forward costs one statement, `GetLiveItem` (R-ARCH-027), beside the link's case | 2 | **measured** 11.48 s (a replica of its own, 30 downloads and the window's pauses), the link's case beside it 11.29 s | a blob of 48 bytes |
| Unit | the link client's rules, the plain and the encrypted read with the withheld tail and a follower, the fallback, the continuation, the mark, the cooldown, the data key destroyed and written nowhere, a withdrawal during a read | 3 | **measured** 0.70 s of tests under the race detector with atomic coverage (`internal/bastion/handover` 0.17 s, `internal/bastion/engine` 0.51 s, `internal/bastion/mainclient` 0.01 s), and one test binary more (`internal/bastion/handover`, 1.9 s from start to end) | objects of 1 to 192 KiB made in memory, the encrypted ones with the format's writer; at most four of 192 KiB per test |
| Chart | the alert's promtool case, firing and silent; the alert in the list | 3 | **measured**: two promtool groups of 60 samples and one name in a list, under 0.1 s of the chart test's 138 s | none |
| Unit | the data key from `CopyDataKey`, the trace before the answer, a failed insert streams, the key destroyed on every path, every condition once more on an encrypted connection, the kind `link_and_key` (the architecture rule was built with the keys and is unchanged) | 4 | **measured** 0.65 s under the race detector with atomic coverage: `TestR_STOR_096_ThePlainCopyFirstAndTheTraceOfAKey` 0.15 s (eleven cases), `TestR_STOR_095_TheConditionsOfAHandover` 0.46 s (was 0.15 s; 46 cases, 18 of them on an encrypted connection), `TestR_STOR_095_WhatAHandoverCounts` 0.02 s, `TestR_STOR_083_CopyDataKeyOpensTheRecordOfTheCopy` 0.02 s | the in-memory store with plain and encrypted connections, objects of 24 to 1,200 bytes |
| Vitest | the card's switch and reasons: off for a new registration, on with a reason and an encrypted connection's line, every clause, the `412` and a refusal, the reader's sentence, no read without `settings:read`, a read that fails (`bastions.spec.ts`, ten cases); an encrypted connection's field, test link and save, the test link's sentence, the fragment `details` (`storage-settings.spec.ts`, three cases) | 5 | **measured** 0.76 s of tests (0.65 s and 0.11 s), in the two spec files that ran before | the in-memory fakes |
| Vitest, the card's tables (Q-UI-959, Q-UI-960) | the contacts' three columns, the facts under a replica's name, a relative time with its date as the title, a URL kept whole where it may break; a token's hint and creation under its description; `breakParts` of a URL and an IPv6 address; the reader's sentence (`bastions.spec.ts`: one case more, four changed) | 5, after the coordinator's look | under 0.05 s more | the in-memory fakes |
| Renders, the job `ui-layout` | `admin-bastions` with the walk `bastionsHandover` (as the stack answers, the switch off and on, the reasons) and `admin-bastions-read` as the auditor, answered in the browser, one card with a pod name of 63 characters, an IPv6 address, a base URL of 51 characters and a token that expires in 20 days, one with short values; the state `encrypted-test` of `admin-settings-storage` | 5 | 24 shots more (12 of the admin's Bastions screen, 6 of the auditor's, 6 of the storage settings), about 9 s at the measured 0.378 s a shot ([test-cost.md](test-cost.md)) | answers in the browser, no seed |
| Integration, area `bastion` | `TestR_BAS_070_ALargeFileIsReadFromTheObjectStore`, the two sides joined: the handover on through the API and `minSize` 0, a cold `GET` of a file of 64 KiB below `/files/` through the Bastion (its bytes and headers, the main installation's class `handover` with no body, its line `delivery: handover`, its metric, the Bastion's `read` and request line, one download counted), the hit (no request, reported once), an npm tarball of a partial reader kept with its package, a read of 160 KiB cut after 96 KiB and continued with `Range` and `If-Range` at the real serve point (`206`, no second download), a refused link's fallback (counted again, H-1342), an ordinary request with the offer, the switch off (the main installation streams an offered forward, the handshake tells the Bastion, the read comes as bytes), no usable link in either side's log | 2 and 3 | **measured** 8.1 s, of which about 3 s are the world's start and 2.7 s waits for the Bastion's report and its handshake | files of 64 KiB (one blob at two paths, one more), one of 160 KiB, an npm tarball of a few hundred bytes |
| Integration, area `bastion` | the Bastion's half, `TestR_BAS_074_ABastionReadsAHandedFileFromTheObjectStore` against `maintest` — **removed** on 2026-11-23: the joined case shows its read, metrics, hit, fallback and log through the real main installation; the network cause of a fallback stays at the unit tier (`TestR_BAS_074_TheLinkClientsRefusals`) | 3 | was 0.37 s | was one object of 64 KiB |
| Integration, area `bastion` | `TestR_BAS_070_AnEncryptedFileIsReadFromTheObjectStoreWithItsDataKey`, the same case with an encrypted connection: the caller gets the plaintext, the main installation sent no body, counted `link_and_key` and wrote the trace with its members and without the key or the link, the store sent the stored size, the Bastion read and holds no key; a body under another key falls back without the cooldown and its key's trace stands; no log line of either side holds the key or a usable link | 4 | **measured** 3.0 s, of which about 2.5 s are the world's start | two files of 65 KiB (one segment and 1 KiB) |
| Measurement, once (not a test) — **made** on 2026-11-23 | the statements of a handover of an encrypted copy, with the area `querybudget`'s harness and a replica whose one connection was encrypted: 2, `GetLiveItem` and `InsertAuditEvents` (30 downloads in the window, 32 traces) | 4 | 4.5 s once | one blob of 66 bytes |
| Measurement, once before package 3 lands (not a test) — **made** on 2026-11-23 | a blob of 300 MiB below `/files/` read through a Bastion with a plain HTTP client, with the handover on and then, at another path of the same blob, with it off, against the stack's SeaweedFS through the relay of the joined case; the numbers below | 3 | 37 s once | one blob of 300 MiB, on the development machine only |

What each pipeline job gains: the unit job about 4.2 s (package 1: 0.08 s measured; package 2: 0.90 s measured; package 3: 0.70 s of tests
and one test binary, about 2.6 s, measured; package 4: 0.48 s measured, no test binary); the web unit job 0.76 s, measured (package 5, no new
spec file); the job `ui-layout` about 9 s (package 5: 24 shots); the chart test about 0.2 s (package 3: under 0.1 s); the integration shard
that holds the area `bastion` 8.1 s for the joined case, measured, which replaced the two halves' 2.3 s (it ran 139 s,
[test-cost.md](test-cost.md)), and 3.0 s for its encrypted twin (package 4, measured); the shard of the area `querybudget` nothing; every
end-to-end leg and the nightly nothing. With 25 formats the same: nothing of packages 4 and 5 is per format.

**The measurement** (2026-11-23, once, not a test): a blob of 300 MiB (314,572,800 bytes) read through a Bastion (volume cache) by a plain HTTP
client, the main installation the suite's web role behind its TLS front, the store the stack's SeaweedFS through the relay that stands for
its public address; an Apple M1 Ultra with 20 cores and 128 GiB, under a load average of 30 to 32 while three other builders ran.

| Read | Time | Bytes the main installation sent | Bytes the store sent through the public address |
|---|---|---|---|
| Handover on (cold) | 7.1 s | 0 | 314,572,800 |
| Handover off (cold, the same blob at another path, read second) | 18.3 s | 314,572,800 | 0 (the main installation reads its store directly) |

The main installation sent no byte of the file with the handover on. The times are one run each under that load and say only that the direct
read was not slower here; the off read came second, with the store's copy warm. A change of
the serve point is shared code and starts the npm leg, which gains nothing, since no end-to-end case is added. With 25 formats the cost is the
same.

## Not verified

- Anything against an object store of a provider. The integration case uses the stack's SeaweedFS over `https` (the suite's main
  installation reads `QUADER_TEST_S3_ENDPOINT`, `https://127.0.0.1:18333` in [up.sh](../../test/integration/up.sh)). *Verified on
  2026-11-23:* the suite's Bastion process reads a link the stack signed with the stack's CA in `bastion.main.ca_file`, bundled with the
  main installation's certificate, and nothing else; a relay that keeps the link's `Host` passes the signature (the joined case's
  public address).
- A public address that the main installation leaves unset in a deployment where the connection's endpoint and the Bastion's
  `bastion.main.url` share a host name: the link then names that host and the Bastion refuses it ([Q-BAS-281](../spec/bastion.md#q-bas-281--the-wires-checks-are-narrower-than-the-list-of-r-bas-073)); the
  stack has exactly that (`127.0.0.1` for both), which is why the joined case names the store `localhost`.
- Whether the object gateway of the owner's cluster can be reached from a Bastion's network: it has an address inside the cluster only (read
  there on 2026-11-22 by the development agent), so a handover there needs a public address first, which is his network's decision.
- How large a response header the main installation's ingress lets through. The handover's header is about 3 KiB with a link of temporary
  credentials (an estimate); the bound of HAProxy's buffer for a header block (`tune.bufsize`) was neither read in its documentation for this page nor
  checked on the owner's ingress.
- The throughput of the Bastion's decryption on its hardware; the format's reader was not measured (encryption's page says the same of the
  main installation).
- Whether every store answers a signed `GET` without overrides with `Content-Length` equal to the stored size; the link check's first part
  shows it per connection at start.
- *Package 4:* a data key whose storage key was destroyed: the destruction is not on `main` yet, so only a key the replica does not hold
  (`unknown_key`) is tested; both end in `CopyDataKey`'s error and the reason `key`. The audit label's spec (`audit-labels.spec.ts`, which
  asks every registered event for a name of its own) was not run: the builder's worktree has no `node_modules`; the label exists and the
  fixture was regenerated by its Go test. A format's route handed over with a key to a real Bastion: the integration case reads below
  `/files/`; the unit table runs `itemStore`'s serve point with an encrypted connection, and the one-time measurement handed an OCI blob of
  an encrypted connection, with its key, to a forward the test sent itself.
