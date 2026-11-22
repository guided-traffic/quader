# Delivery by signed links of the object store

Status: specified on 2026-11-22; built behind the API on 2026-11-22 ([storage.md](../spec/storage.md) R-STOR-014, R-STOR-070 to R-STOR-074;
[usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-040;
[ADR-242](../decisions.md#adr-242--delivery-by-signed-links-of-the-object-store-one-switch-of-the-installation-where-a-link-costs-nothing),
[ADR-243](../decisions.md#adr-243--a-route-declares-whether-its-clients-check-a-digest-and-kit-check-20-holds-it)). Built: the format's
declaration that its clients check a digest (`formats.Route.ClientDigest`, R-STOR-071) with kit check 20, declared by `oci.blob.get` and
`ansible.artifact.get`; the serve point that reads it ([download_links.go](../../internal/formathost/download_links.go),
`edge.ServeItem`'s `307`), the row of the switch with its store, audit event and snapshot, the host rule, each connection's link check and
state, and the signing ([internal/downloadlinks](../../internal/downloadlinks/)). Not built: the operations of the management API that
switch it on, the settings page and the repository's section (package 3). A new installation keeps the switch off, and nothing of the
product switches it on before package 3 lands, so every gap below is dormant until an administrator switches it on; the sections describe
what then holds.

The perspective of this page is what changes for the security of a download when Quader answers it with a link to the object store
instead of the bytes: who can use the link, what Quader no longer sees or checks, and what the object store learns and records.

## The boundary

```
 client ──GET, its credential──▶ Quader (packages): authentication, decision, gates, limits' Admit, the conditions of R-STOR-070
   │                                  │ signs locally, sends nothing to the store
   │◀──307, Location: signed link─────┘ counts the download, charges the size, records the audit event
   │
   └──GET, no credential──▶ object store (the connection's public address): checks the signature and the expiry, serves the bytes
                             (its own log names the signer, never the client)
```

Quader's part ends with the `307`. Everything that happens to the link afterwards (who fetches it, how often, with which result) happens
between the client and the object store.

## What a link allows, and to whom

- A link allows exactly one thing: a `GET` of one object key on one connection, until its expiry. It carries the store's address, the
  bucket, the object key (the blob's digest), the signing access key's ID and, with temporary credentials, the session token; none of
  them lets its holder sign another request (H-1281).
- It is issued only after everything a streamed download passes: authentication, the decision point with the endpoint's exposure, the
  format's serve-time check, the malware gate, the vulnerability block and the signature gate on decided passing states, and the check of
  the download limits (R-STOR-070). It is never issued for a request a Bastion forwarded, for a system read, for `HEAD`, a range, a
  conditional answer, a file of `/files/`, or a route whose format's clients do not check a digest themselves. A Bastion's request may get
  a handover instead, which the Bastion reads and verifies itself and never hands on (R-STOR-095, [bastion-handover.md](bastion-handover.md)).
- Its life is `linkLifeSeconds` (60 s unless the administrator sets up to an hour), bounded by the signing credential's own expiry
  (R-STOR-072). Within its life anyone who holds it can use it ([H-12](../spec/storage.md#h-12--presigned-download-urls-are-bearer-capabilities)).
- Its host is never a host of Quader nor below one, so a client that keeps its credential for subdomains does not send it to the store
  (R-STOR-072), and the store's answer never runs in Quader's origin. The rule knows the hosts of Quader's configuration only (H-1282).

## Integrity: the client's

A link replaces the check of R-STOR-013 (Quader holds back the last bytes until the SHA-256 matches) with the client's own. The format
contract says per route whether every client family of the format compares the whole body with a digest it knew before the request
(R-STOR-071); only such a route is ever answered with a link, and a declaration needs the client's source or a test with the real client.
What Quader no longer does for such a download: it does not see a changed object, does not mark it `corrupt`, and does not abort the
transfer (H-1277).

## What is counted, and where

Quader counts at issuance: the request, the item's size as cached bytes, one download in the statistics, the audit event
`artifact.downloaded` with `delivery: "link"` under `audit.record_downloads`, the request log's `delivery`, and the metrics of R-STOR-073.
It does not learn whether the link was fetched. No figure of the download limits keeps a caller from a link: a speed limit paces only what
Quader writes, and the hour and day budgets are charged in full at the issue (R-LIM-040, Q-LIM-99, H-1352); the parallel lease ends with
the `307` (H-1276). The object store's own log, where its operator turned one on, records the fetch with the signer, not the caller
(H-1278).

## What this does not cover

- [H-12](../spec/storage.md#h-12--presigned-download-urls-are-bearer-capabilities): a signed link is a bearer for its life; it can be
  fetched several times and passed on, and revoking the caller's token does not end it.
- [H-11](../spec/storage.md#h-11--object-store-principals-bypass-quaders-authorization-and-integrity-checks): whoever can write the bucket
  can replace content; a link delivers whatever the object holds.

### H-1275 · A link outlives a change of what the caller may receive

Component: the issuance of R-STOR-070 and the link's life (R-STOR-072). Subject: every holder of a link. Dormant: live once the switch is
on. A decision taken after the link was issued does not reach it: a malware verdict `infected` or a withdrawn release, a vulnerability
block, a narrowed trust of the signature gate, the removal of the item, a lowered visibility, a revoked grant. Until the link expires the
store serves the object to whoever holds it. Q-STOR-284 keeps links away from blobs whose verdicts are still open, so the window is the
link's life after a change of a decision that was already taken. What makes it real: a finding or a takedown within the minute after a
client received a link, and a long link life.

### H-1276 · The limit of parallel downloads does not see a link's transfer, and the byte windows count its size at issuance

Component: the download limits ([usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-040). Subject: every budget key, a speed limit
or none (Q-LIM-99). Dormant: live once the switch is on. The parallel lease ends with the `307`, so `parallelDownloads` bounds how many links a caller
receives at once, not how many transfers run; a caller can start many transfers from the store within the links' lives. The hour and day
windows count the item's size once at issuance: a link fetched several times within its life is charged once, a link never fetched is
charged in full. What makes it real: a caller that requests links faster than it fetches them, or fetches one link many times.

### H-1277 · A link's bytes are checked by the client alone

Component: the declaration of R-STOR-071 and the clients. Subject: every client of a route that declares `Checks`. Dormant: live once the
switch is on. Quader does not hold back the last bytes, does not compare the digest, does not mark a changed object `corrupt`
(R-STOR-013), so a damaged or replaced object is delivered whole and is refused, if at all, by the client. The declarations rest on the
source of the reference clients read on 2026-11-22 (containerd, `ansible-galaxy`) and on the end-to-end cases of R-STOR-074
(`Test<Set>_R_STOR_071_ClientsCheckALink`), which overwrite a link's object in the bucket with bytes that unpack to the same files and
require every client of the leg to refuse it. Run once on 2026-11-22 on a kind cluster: the Docker Engine and Podman refused the altered
layer, `ansible-galaxy` of ansible-core 2.17 and 2.21 the altered collection. The npm leg's case showed Yarn 4.18.1 installing an altered
tarball while npm 12, pnpm 12 and Yarn 1 refused it, so npm's tarball route declares nothing and every npm download streams through
Quader's check
([Q-STOR-354](../spec/storage.md#q-stor-354--yarn-4-installs-a-tarball-whose-object-was-altered-so-npms-tarball-route-declares-nothing)).
CRI-O and the Docker Engine's older pull path were not run. A client that a
user configured to skip its check (an option of the client, an old version) gets unchecked bytes. What makes it real: a principal who can
write the bucket (H-11), or a store that damages an object, together with a client that does not check.

### H-1278 · The object store's log names the signer, not who fetched the link

Component: the object store's request log, the operator's. Subject: an investigation of who received an object. Dormant: live once the
switch is on. Signing is local work: the store records no issuance, and a fetch is recorded with the connection's credential as its
principal and the address the store saw, never Quader's caller. Who received a link is in Quader's request log for every link and in the
audit trail where downloads are audited (R-STOR-073); joining the two takes the object key (the digest) and the link's life, and a link
passed on is fetched by someone neither log names. What makes it real: an inquiry into a leaked artifact on an installation that does not
audit downloads.

### H-1279 · A link's answer lacks three of the content headers

Component: the store's answer to a link (R-STOR-072). Subject: a browser that opens a link. Dormant: live once the switch is on. A link can
ask the store for `Content-Type`, `Content-Disposition` and `Cache-Control` only; `X-Content-Type-Options: nosniff`, the sandbox
`Content-Security-Policy` and `Cross-Origin-Resource-Policy` of R-STOR-012 are missing. The answer comes from a host that is no host of
Quader nor below one, carries `attachment`, and the object is stored as `application/octet-stream`; a connection whose store ignores the
overrides serves no link (R-STOR-074). What is left: the store's host can be the same site as Quader's (`s3.example.com` beside
`quader.example.com`), and another site can embed the bytes while the link is valid. What makes it real: an attacker who can store bytes in
a repository whose format declares a client's check (any blob of an OCI repository) and gets a victim's browser to a link within its life.

### H-1280 · A client that cannot reach the store, or that sends its own credential there, fails the download, and Quader counted it

Component: the network between the clients and the store, and the clients' configuration. Subject: every client of a repository that
serves links. Dormant: live once the switch is on. A client whose network does not reach the public address gets a connection error from
the store's host, not an answer of Quader; a client configured with a credential for the store's host (a credential helper, an `auth.conf`
entry for that host) sends it, and the store refuses a signed link that arrives with a credential. Quader has counted the download and
charged its size already, and has no fallback: the client's next request gets a link again. What makes it real: client networks that the
administrator's test link did not cover (CI runners behind another firewall), and a store's host that is also a registry the clients log
in to. The way out is the repository's opt-out or the switch.

### H-1281 · A link names the store's address, its bucket, the object key and the signing key's ID

Component: the link (R-STOR-072). Subject: every caller that receives one, an anonymous caller of a public repository included. Dormant:
live once the switch is on. The query of a link holds the access key's ID of the connection's credential (and with temporary credentials
its session token), its path the bucket, the prefix and the digest. None of it signs another request, but a caller learns the store's
address and layout and can try other requests against it. What makes it real: a store whose bucket or address accepts more than signed
requests (a listing open to anonymous callers, a credential with a weak secret).

### H-1282 · The host rule knows only the host names of Quader's configuration

Component: the check of the links' host (R-STOR-072, R-STOR-074). Subject: clients that reach Quader under a name its configuration does
not list. Dormant: live once the switch is on. A default endpoint with no `hosts` answers on any host name; a client that addresses Quader
by such a name (an alias of the ingress) and receives a link whose host lies below that name keeps its credential if it is written in Go,
and the store refuses the link. What makes it real: an alias of Quader that is a parent of the store's public host (`registry.corp` beside
`objects.registry.corp`), used by image clients.

### H-1283 · The transfers of links spend the object store's own limits, which Quader shares and does not see

Component: the object store's limits per account or project, the provider's. Subject: the whole installation. Dormant: live once the switch
is on, or once a Bastion registration's handover is, whose direct reads are transfers by link as well
([bastion-handover.md](bastion-handover.md)). A provider limits requests, concurrent requests and bandwidth of an account or project (the owner's provider documents 2,500
requests a second, 500 concurrent requests and 500 MB/s per project, read on 2026-11-22). A transfer by link counts against them as
Quader's own reads and writes do, and Quader neither sees nor bounds it: many clients fetching large links at once can make the provider
refuse or slow Quader's own uploads and reads. What makes it real: a burst of pulls by link, such as a fleet of nodes pulling one image,
against a store whose limits are shared with the installation's own traffic. The ways out are the repository's opt-out, a higher minimum
size, or the switch; nothing in Quader enforces the provider's limit.

### H-1350 · A store that stops honouring an override after the link check serves links without it until the next check

Component: the link check of R-STOR-074 ([linkcheck.go](../../internal/blob/linkcheck.go)) and each replica's state of a connection. Subject:
a browser that opens a link of a connection whose store changed. Dormant: live once the switch is on. The check runs at a replica's start,
when the switch is turned on, and at the snapshot's safety period only while a connection's check has not answered
([Q-STOR-351](../spec/storage.md#q-stor-351--the-link-check-runs-on-every-replica-with-a-client-listener-in-the-background-and-again-until-it-answers));
a store that passed it and then stops honouring `response-content-disposition` or `response-content-type` (a new version of its software,
a proxy put in front of the public address that strips the query) keeps getting links on that replica, and their answers lack
`attachment` or carry the stored type. What is left is H-1279's case without its two mitigations: the object is still stored as
`application/octet-stream`, which no browser renders. What makes it real: a change of the store or of the path to its public address while
links are on and no replica restarts; the way out is to switch the delivery off and on, which runs the check again on every replica.

### H-1351 · A link may name a copy that a mark made damaged after the item's lookup

Component: the connection choice of R-STOR-074 over the copies the item lookup read ([links.go](../../internal/downloadlinks/links.go)).
Subject: a client of a blob whose copy is marked damaged between its lookup and its link. Dormant: live once the switch is on. The choice
reads the copies of the request's own lookup and costs no statement (R-ARCH-027); a concurrent read or scrub that marks one of them `missing`
or `corrupt` after that lookup is not seen, so the link may name the object that was just found damaged, and the client refuses its bytes
(H-1277) or gets the store's `404`. A streamed download in the same window meets the copy and falls over to the next before its first byte
(R-STOR-055). What makes it real: a damaged copy on the first connection of the read order beside a sound one on another, and a link issued
in the milliseconds after the mark; the client's retry asks Quader again and gets the next copy.

### H-1352 · A caller that is slowed through Quader is not slowed by a link

Component: the download limits' speed limit (`cachedBytesPerSecond`, [usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-023) and the
links of R-STOR-070. Subject: every budget key whose profile or rule sets a speed limit, which on a new installation is every caller but an
administrator (R-LIM-010). Dormant: live once the switch is on. The development agent's decision
[Q-LIM-99](../spec/usage-and-limits.md#q-lim-99--a-speed-limit-does-not-stop-a-link-the-budgets-are-charged-at-the-issue) makes a speed
limit no condition of a link: the transfer by a link runs between the client and the object store at the store's speed, and only the hour
and day budgets, charged in full at the issue, and the parallel lease of the issue bound it (H-1276). An administrator who set a speed limit
to make a caller fetch slowly gets that only for the downloads Quader streams (below the minimum size, a range, `/files/`, a format whose
clients do not check). In `audit` nothing about the pace of a link is recorded, as `enforce` paces none. What makes it real: a caller whose
speed limit was meant to spread its load over time, a CI fleet pulling large layers, against a store whose bandwidth is shared with the
installation's own traffic (H-1283). The ways out are a smaller hour or day budget for that caller, a higher minimum size, a repository's
opt-out, or the switch.

