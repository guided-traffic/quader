# The downloads from upstreams

Status: specified on 2026-11-24 ([operations-and-observability.md](../spec/operations-and-observability.md) R-OPS-049 to R-OPS-052,
[api-and-ui.md](../spec/api-and-ui.md) R-UI-210 to R-UI-212); the tracker, the publication, the two tables and the API are built (T-26
package A), and the top bar's button and its panel (T-26 package B). The perspective of this page is what the overview shows and keeps, and to whom. The code is described in
[developer/upstream-downloads.md](../developer/upstream-downloads.md).

## What it shows and to whom

- The names, versions and paths of what every remote repository fills, and who caused each fill (a client's principal, the person who
  started a queue row, its bulk or its request), to the holders of `content:metadata-read` only: the administrator and the auditor
  (`GET /api/v1/system/upstream-downloads` and `…/summary`, declared with `x-quader-permission: {permission: content:metadata-read}` in
  [openapi.yaml](../../api/openapi.yaml) and enforced by the shared authorization step). Both read every repository's metadata already,
  and both hold `identities:read` and `usage:read`, so the overview gives them no fact they could not read before. It gathers those facts
  in one place, live.
- A project's Owner, a `security-admin`, a `repository-admin` and a plain user get `403`; a caller without a session or token `401`. A
  Maintainer does not see the fills of their own repository here; the repository's tab Downloads shows its queue.
- No upstream address is shown or kept: a signed address of a content network carries a credential in its query. A client's network
  address and the headers of either side are not kept. What is kept of a download is listed in R-OPS-049's table of facts. A download
  queue an upstream's rate limit holds is listed with the upstream's name: its preset's ("Docker Hub"), else the host of the remote's
  upstream address, never a path or a query, read from the repository's configuration that both roles read already
  ([remote-curation.md](../spec/remote-curation.md) Q-CUR-176).
- An anonymous caller's fill reads as nobody's: the principals `anonymous` and `authenticated` are not named.
- Reading is not audited, as reading a package list is not.

## Where it lives

- In PostgreSQL only: `upstream_download_live` (one row per replica, rewritten every `proxy.upstream_downloads.publish_interval`) and
  `upstream_download_finished` (at most 100 rows plus one batch per replica), both of export class `transient`: not in a backup, empty
  after a restore. Nothing of it is in Valkey, so a write to Valkey cannot put a name on an administrator's screen.
- The repository of a finished download is a reference that cascades: removing a repository removes its rows. The principal is a
  reference that nulls: removing a principal leaves its downloads as nobody's.
- What a replica writes are facts of its own fills. A replica that can write the database can write any row here, as it can write every
  other table; the reader resolves the repository and the principal by id, so a forged row cannot name a repository or a person that
  does not exist.
- The names in a live row are JSON a replica wrote; the API hands them back as strings in a JSON answer and never interprets them.

## What this does not cover

### H-1843 · Who fetched what stays until 100 newer downloads push it out

Component: the finished list (R-OPS-051). Subject: the principals who caused fills. Live. The list has no retention in time: in a quiet
installation, that a person fetched a package is kept until 100 newer downloads end, which can be weeks. A deleted principal reads as
nobody. What makes it real: a quiet installation and a policy that bounds how long personal usage data is kept.

### H-1844 · An auditor sees, live, who fetches what

Component: the overview (R-OPS-052). Subject: every caller of a remote repository. By design. The auditor read the same facts through the
usage overview and the audit before, but not live. What makes it real: an organisation whose auditors are not meant to watch people.

### H-1845 · A replica that stalled can show its download as interrupted for a while

Component: the sweep (R-OPS-051). Subject: the accuracy of the list. Live. A replica that cannot write for longer than
10 × `publish_interval` (at least 30 s) has its running downloads recorded as `interrupted`; its later end replaces them. The list may say
"interrupted" for a download that completes. No content is affected.

### H-1846 · The list shows what one fetch read, not what a client received

Component: the tracker (R-OPS-049). Subject: the meaning of the bytes. By design. A body that was pre-empted, cut or refused counts the
bytes the upstream sent; the bytes a client received through the stream and the cache's later answers are not here.
