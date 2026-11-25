# Curating a remote repository

Status: specified on 2026-11-23 ([remote-curation.md](../spec/remote-curation.md)); the delivery rules (R-CUR-001 to R-CUR-006,
R-CUR-017), the downloads ahead with their queue (R-CUR-010 to R-CUR-013), and the examination, the requests and the retention
(R-CUR-014 to R-CUR-016) are built; the catalogue is not. A gap of an unbuilt mechanism says "dormant".
The perspective of this page is what the curation of a remote repository lets a person make Quader do and show: what a search of the
upstream tells its operator and shows its caller, which hosts it reaches, what a delivery rule keeps from a client and what it does not,
who can make Quader download how much, and what is kept of content that no client may get.

## The parties

```
 person (session + CSRF, or token) ──management API──▶ api: catalogue, rules, requests, downloads ahead
        │                                                │ reads through the repository's upstream client (egress policy, TLS,
        │                                                │ credential policy, background slot, circuit read only), stores nothing
        │                                                ▼
        │                                         upstream registry, and the catalogue hosts its preset declares
        │                                                ▲
        └── a POST queues rows ──▶ worker: remote-download ┘ stores through the remote engine: the format's gate,
                                                             the vulnerability miss gate, integrity checks, the item store
 client (token) ──package protocol──▶ packages: the rules decide before a fill (no upstream request), at the serve point
                                                  (the fourth check of the one decision) and in documents
```

| Party | Trusted with | Not trusted with |
|---|---|---|
| A reader of the repository (signed in) | browsing the catalogue (in mode `onlyAllowed`: what is delivered, and a lookup by exact name), asking for an allow, downloading one delivered version at its own charge | the rules' notes; examination fetches; bulks |
| A holder of `manage` (Maintainer, Owner) and the administrator | the rules, requests, examination fetches and bulks within their bounds | the mode and the repository's switches (configuration: Owner, `repositories:write-config`); a gate's release (triage) |
| The upstream and the hosts its preset declares | the answers the catalogue shows, as text within bounds | anything stored by a catalogue call; a host a person names |

## What a search tells the upstream, and what it shows

- A search sends the person's text, the page and the filters to the upstream, from Quader's address, with the repository's credential
  where the upstream receives it (the upstream origin and `extraHosts` entries with `sendCredentials`); a catalogue host of a preset
  receives no credential unless its preset says so, and none of the five formats' presets does (R-CUR-008, R-OCI-038). The upstream's
  operator therefore learns what the installation's people look for, under the installation's account ([H-1720](#h-1720--a-search-tells-the-upstreams-operator-what-people-look-for)).
- The search guard withholds a term that a blocking rule covers, and in mode `onlyAllowed` every term no allowing rule covers, so an
  internal name that a block protects is not sent upstream by a search (R-CUR-002, as R-NUGET-016 withholds a term an exclude pattern
  covered). A lookup by exact name sends the name.
- With an upstream credential the catalogue shows what that credential sees: a private registry's packages to every signed-in reader of
  the repository in mode `allExceptBlocked`, to its deciders in mode `onlyAllowed`, and by exact name to every reader in either mode
  ([H-1722](#h-1722--the-catalogue-shows-what-the-upstream-credential-sees), [H-1721](#h-1721--a-reader-of-a-remote-that-delivers-only-what-is-allowed-can-look-up-names-one-by-one)).
- Every text the upstream sends (a description, a version's flags) is shown as plain text, cut at 1,000 characters; no markup of an
  upstream is rendered. The search requires a signed-in caller: an anonymous caller cannot drive upstream searches through Quader.

## Which hosts the catalogue reaches

- The upstream's origin and its `extraHosts`, and the catalogue hosts a format's preset declares (`hub.docker.com` for Docker Hub,
  `gitlab.com` for GitLab, the replication host of the public npm registry that the mirror already reads). A person never names a host:
  a repository whose URL is no preset's has no catalogue host, so a second host cannot be used for a request forgery (R-CUR-008,
  Q-CUR-15). Every host passes the egress address policy at connect time ([egress.md](egress.md)).
- A bulk download's expansion reads the same catalogue and reaches the same hosts, a catalogue host without the repository's
  credential, also where an `extraHosts` entry with `sendCredentials` names the same host (GitLab's `gitlab.com`); the fetch of a
  queued version's files reaches only the upstream, its `extraHosts` and the format's mirror hosts, so a catalogue host never serves an
  artifact (Q-CUR-167).
- A redirect of a catalogue request follows the redirect rules of the remote (R-REPO-039), as a discovery request of a mirror does
  ([H-525](egress.md#h-525--a-redirect-of-a-discovery-request-is-followed-to-any-host-the-egress-policy-admits)).
- Docker Hub's repository search is an endpoint its operator does not document; it is used with a fallback that turns the search off
  when its answer is not of the expected shape ([H-1723](#h-1723--docker-hubs-search-is-an-endpoint-its-operator-does-not-document)).

## The bounds on what an upstream answers

- A document the catalogue reads is at most 64 MiB and parsed as a stream; a call ends after 20 s; a page holds at most 50 hits, 200
  entries of a level or 100 versions; a text is cut at 1,000 characters (R-CUR-008). The catalogue's cache keeps parsed pages, 32 MiB per
  replica, never a document's bytes, and nothing it keeps is served to a client. A page that found nothing is kept only when the call
  asked the upstream, so the period still spares an upstream's budget; a not-found answered from Quader's own rows (Debian) is asked
  again, against the database and not the upstream (Q-CUR-164).
- A catalogue call stores nothing in the repository's cache, its rows or the negative cache: browsing never populates, so the rule that a
  session cookie never fills a cache (R-AUTHZ-008) holds for it unchanged.
- A failure's excerpt in the download queue is the first 512 bytes of the upstream's answer as printable text, and the address is shown
  without its query, because a CDN's signed address carries a credential in it (R-CUR-011). The tab **Downloads** shows the excerpt, the
  address and the sentence as text (Angular's interpolation into a `pre` and a `code`), never as markup, so an upstream's error page
  cannot put an element or a script into the screen (R-CUR-018)
  ([H-1728](#h-1728--the-excerpt-of-an-upstreams-error-answer-is-shown-to-every-reader-of-the-repository)).
- A rate-limit answer keeps, besides its excerpt, only the headers of a fixed list (`Retry-After`, the `RateLimit` fields and the
  `X-RateLimit-` three, each at most 200 bytes): a registry's own header of its limit can name the address the limit counts, which is the
  installation's address on the internet, and is never kept (Q-CUR-174). The limit, its times and its last answer are shown to every
  reader of the queue, as a row's failure is ([H-1883](#h-1883--a-reader-of-the-queue-sees-how-much-of-the-upstreams-quota-is-left)).

## What an upstream's rate limit holds back

- A rate-limit answer longer than 10 s ends a run of the queue and holds the repository's queue until the upstream's time, or the queue's
  back-off of up to an hour (R-CUR-011, Q-CUR-170 to Q-CUR-172): no row starts and a bulk's expansion asks nothing, so the installation does
  not send a request per waiting row into a limit, and after the time one row asks first. The attempt is not counted, so a limit cannot
  end a row `failed`. The hold is the queue's alone: a mirror pass and a client's fetch of the same upstream do not read it
  ([H-1880](#h-1880--the-queue-and-a-mirror-pass-of-one-repository-meet-an-upstreams-limit-apart),
  [H-1882](#h-1882--a-clients-fetch-asks-an-upstream-whose-limit-holds-the-queue)), and a probe after a limit without a time sends up to
  five requests ([H-1881](#h-1881--a-probe-after-a-limit-without-a-time-sends-up-to-five-requests-into-it)).

## What a rule keeps from a client, and what it does not

- A rule binds every caller on the package protocols and `/files/`, administrators included. On a client's request it decides before
  any upstream request, so a name the rules do not deliver is never asked of the upstream by a client: the dependency-confusion
  protection of the former patterns holds (R-CUR-002, R-CUR-003).
- At the serve point it is the fourth check of the one decision, after what was found in the content; through a virtual repository the
  walk goes on to the next member; through a Bastion the main installation decides, and a narrowing reaches a Bastion's cache as a
  `content` entry of `repository.narrowed` (R-CUR-004).
- A rule is no security check: it says what a repository delivers, not that delivered content is safe. The malware gate, the
  vulnerability block and the signature gate judge a delivered version as before, and an allow lifts none of them (R-CUR-014).
- What a rule does not reach: content by digest of an image whose tags are partly allowed ([H-1724](#h-1724--a-blob-of-a-refused-tag-can-be-fetched-by-its-digest-when-the-image-is-partly-allowed));
  the indexes of a Debian remote addressed directly, which list what its rules refuse ([H-1726](#h-1726--a-debian-remote-addressed-directly-lists-what-its-rules-refuse));
  a replica that has not rebuilt its snapshot ([H-132](repository-model.md#h-132--a-replica-enforces-the-previous-repository-settings-until-its-snapshot-is-rebuilt)).
- A narrowing (a block added, an allow removed, a range of versions set on a package's allow, the mode set to `onlyAllowed`) is
  journaled, so a restore keeps it; a widening is not. The replay of a range keeps the narrower of the restored and the journaled one and
  never widens: a removed allow with a range is not replayed where the package would then be delivered whole (R-CUR-026, Q-CUR-225).
- A range of versions (R-CUR-026) is compared in the format's own order, the one every list of the format shows; a string the format
  does not rank as a version (an image's `latest`, a branch tag) lies outside every range, so a range cannot be passed by a tag name that
  points at an image outside it (Q-CUR-223). A manifest by digest of an image with a range is decided by the stored tags that name it, as
  with version rules (Q-CUR-230); a blob stays reachable by its digest
  ([H-1724](#h-1724--a-blob-of-a-refused-tag-can-be-fetched-by-its-digest-when-the-image-is-partly-allowed)). A bound the store cannot have
  written (on a block, in a format without an order, a bound the format does not rank) makes the snapshot load the repository disabled
  (fail closed).
- A switch (R-CUR-024) is decided by the server in its own transaction, under the rule set's row lock, against the rules and the mode as
  stored; it is a rule's write for who may (`manage`, the administrator) and journals its narrowings as one. The change of the mode to
  `onlyAllowed` writes its allows first (`allow-stored`, R-CUR-025), a widening that is not journaled
  ([H-2020](#h-2020--a-restore-to-a-point-before-allow-stored-refuses-the-packages-it-allowed)).

## Who can make Quader download how much

| Who | What | Bound | Charged to |
|---|---|---|---|
| A reader who may populate | one delivered version, through the management API | the person's upstream budget and the remote's ceiling (R-LIM-037), one row per version, the waiting rows of the repository | the person |
| A reader who asks for an allow | the examination of the version asked (of a package, its newest) | 20 open requests per person and repository; the person's budget | the person |
| A holder of `manage` | one version for examination; every version of a package; a sub-section | per bulk 10,000 versions and 100 GiB, the waiting rows (10,000) and four running rows per repository, the background share of the fetch slots, the upstream's throttling | nobody: system work, recorded with the person ([H-1725](#h-1725--a-holder-of-manage-can-make-quader-fetch-up-to-a-bulks-bound-without-a-ceiling)) |
| A holder of `manage` | a sync: every version of a package or a sub-section again at every interval ([remote-curation.md](../spec/remote-curation.md) R-CUR-027) | a bulk's bounds per run, one run of a sync at a time, the repository's interval (at least five minutes, an hour by default), 100 syncs per repository, `remote.syncs.maxBytes` and `remote.syncs.paused` (configuration), only what the rules deliver | nobody: system work, the run's event names `system:download-sync` ([H-2030](../spec/remote-curation.md#h-2030--a-syncs-reads-of-the-upstream-have-no-budget-over-time)) |
| An administrator | a mirror's scope | `remote.mirror.include`, `maxBytes` | nobody (Q-LIM-19) |

- A probe's background download keeps its charge and bounds (R-REPO-065), as a row of the queue.
- A download ahead writes no download statistics and no usage download, so the figures of downloads stay the clients'.

## What is kept of content no client may get

- A download ahead stores what it names whatever the rules say, so the installation's object store holds content that no client may
  get, possibly malicious: the vulnerability miss gate still refuses what the advisories name as malicious or a triager blocked, and
  the malware gate refuses what ClamAV finds at every serve point, but a format the advisories do not cover (Debian, Ansible Galaxy, OCI
  before its analysis) stores it unexamined by them ([H-1727](#h-1727--content-fetched-for-examination-lies-in-the-object-store-until-it-is-discarded)).
- It is removed by a discard, by a decline, or after the repository's days without a decision (30 by default), each journaled, so a
  restore removes it again (R-CUR-016): the replay makes the version due and queues the retention task, whose run on a worker removes it
  while the rules still do not deliver it (Q-CUR-144); until that run the restored copy is stored and not served. What a removal takes is
  what the version's runs recorded and the package rows link to it; a file it shares by its digest stays
  ([H-1842](#h-1842--a-discard-leaves-what-a-version-shares-by-its-digest)).
- Who removes it: the holders of `manage` and the administrator (a discard, a decline), and the task as `system:examination-cleanup`.
  A reader who asks for an allow starts an examination charged to the reader's own budget, at most one per entry it opens and 20 open
  entries per repository.
- Who sees it: whoever reads the repository sees the stored version as not delivered, with its verdicts
  ([H-1729](#h-1729--a-reader-learns-what-is-being-examined)).

## What this does not cover

- [H-132](repository-model.md#h-132--a-replica-enforces-the-previous-repository-settings-until-its-snapshot-is-rebuilt): a replica decides
  on its snapshot of the rules until it is rebuilt.
- [H-525](egress.md#h-525--a-redirect-of-a-discovery-request-is-followed-to-any-host-the-egress-policy-admits): a redirect of a catalogue
  request is followed to any host the egress policy admits, as a discovery request's is.

### H-1720 · A search tells the upstream's operator what people look for

Component: the upstream catalogue (R-CUR-008). Subject: the people of an installation who search a remote's upstream. Live for every
remote whose upstream answers a search. The query, from Quader's address and with the repository's
credential where the upstream receives it, reaches the upstream's operator, who learns the names and words an organisation looks for. The
search guard withholds only terms a rule protects. What makes it real: an operator that logs queries per account, and a search for an
internal project's name that no rule protects.

### H-1721 · A reader of a remote that delivers only what is allowed can look up names one by one

Component: the catalogue's lookup by exact name (R-CUR-009). Subject: a remote in mode `onlyAllowed` whose upstream is private (a
credential). Live. Search and levels show a reader only what is delivered, but a lookup by exact name answers any package
with its versions, so that a reader can ask for it; a reader who guesses names learns which exist in the private upstream and their
versions, bounded by the catalogue's budget per person (30 requests a minute). What makes it real: a private registry whose names are
confidential to its deciders.

### H-1722 · The catalogue shows what the upstream credential sees

Component: the catalogue (R-CUR-008, R-CUR-009). Subject: every reader of a remote whose upstream credential reads more than the readers
should see. Live. The catalogue asks with the repository's credential, so its answers are that account's view; in mode
`allExceptBlocked` every signed-in reader of the repository browses it. A reader could already fetch every one of those packages through
the remote in that mode, so the catalogue adds the listing, not the access. What makes it real: an account of a hosted registry that sees
packages of several organisations.

### H-1723 · Docker Hub's search is an endpoint its operator does not document

Component: the OCI format's catalogue for the preset `docker-hub` (R-OCI-038). Subject: every Docker Hub remote. Live.
`GET /v2/search/repositories/` of the Hub API is not in its reference; it may change or disappear without notice. The fallback turns the
search off for the cache period when the answer is not of the expected shape, so a change shows as "search unavailable", never as a
wrong answer, but an answer that keeps the shape and changes its meaning (a different ranking, a cut window) is shown as it comes. What
makes it real: a change of the Hub API.

### H-1724 · A blob of a refused tag can be fetched by its digest when the image is partly allowed

Component: the delivery rules for OCI (R-OCI-038, Q-CUR-10). Subject: an image of a remote with version rules (some tags allowed or
blocked) or with a range of versions of its allow (R-CUR-026, Q-CUR-230: a tag outside the range is refused like a blocked one). Live since the delivery rules are built. A blob by digest is delivered when some version of the image is, because a blob names no tag; a client
that knows the digest of a layer of a refused tag fetches it, through the remote and from the upstream. Manifests by digest are decided by
the stored tags that name them, so a client cannot pull the refused tag's image as a whole. What makes it real: a layer that a refused tag
holds and an allowed one does not, and a client that learned its digest elsewhere; the signature gate made the same cut (H-1196). A
referrers tag (`sha256-<hex of a manifest>`) is no version since [Q-CUR-160](../spec/remote-curation.md#q-cur-160--an-oci-referrers-tag-is-no-version-it-follows-its-subject):
it is decided by the manifest it names, as that manifest by digest is, and adds nothing to this gap.

### H-1725 · A holder of `manage` can make Quader fetch up to a bulk's bound without a ceiling

Component: the downloads ahead (R-CUR-010, Q-CUR-17). Subject: the upstream's quota of the installation, its object store, and the
upstream's operator. Live since the download queue (R4) is built: a package's bulk runs now, a sub-section's once the catalogue lists
levels (R3). A bulk is system work: no ceiling of the remote counts it. A Maintainer can start bulks one after
another, each up to 10,000 versions and 100 GiB, bounded by the waiting rows and the four running rows per repository, the background
share of the fetch slots and the upstream's throttling, not by a budget. On Docker Hub anonymously, 100 manifest pulls a window are used
up by one bulk of 50 two-platform tags, and every client of the installation's address waits for the window. What makes it real: a
Maintainer who downloads a large section, or several, of a registry with a pull limit.

### H-1726 · A Debian remote addressed directly lists what its rules refuse

Component: the delivery rules for Debian (R-DEB-036, Q-CUR-9). Subject: an APT client that uses a Debian remote directly instead of a
virtual repository over it. Live since the delivery rules are built. The upstream's signed indexes pass unchanged, so `apt` lists every package and is
refused the file with a status line that carries no reason; the refused content is not served, but the listing shows what the rules
were meant to keep out of view. Under a range of a package's allow (R-CUR-026) APT also picks the highest version listed, which lies
outside the range when the upstream moved past it, and the install fails. What makes it real: a client set up against the remote while
it has rules that refuse a version (the mode `onlyAllowed`, a block, a range); the remote's guide names the virtual repository instead
(Q-CUR-227).

### H-1727 · Content fetched for examination lies in the object store until it is discarded

Component: the examination (R-CUR-014, R-CUR-016). Subject: the installation's object store and every process that reads blobs from it
(the scanners, the replicas). A download ahead stores a version that no client may get, which may be malicious: the
vulnerability miss gate refuses only what the advisories of npm and NuGet name, ClamAV's verdict follows the store, and Debian and Ansible
Galaxy content is not analysed for vulnerabilities at all. It stays until a discard, a decline or the days of the repository pass
(30 by default). It is never served while the rules refuse it, and the malware gate refuses what ClamAV finds once it is delivered. Live
since the examination (R2b) is built. What makes it real: a malicious package fetched for examination whose malware no signature describes, later allowed by a decider who reads
"No malware found" as "safe" (R-MAL-003).

### H-1728 · The excerpt of an upstream's error answer is shown to every reader of the repository

Component: the download queue's failures (R-CUR-011). Subject: every reader of a remote's tab **Downloads**. Live since the download
queue (R4) is built, through `GET …/downloads`, and shown on the tab **Downloads** since R5a, where an opened row shows the excerpt in a
code block to every signed-in reader of the repository (R-CUR-018); a client's failed fetch writes its excerpt as well (Q-CUR-19). The
first 512 bytes of an upstream's non-success answer are kept and shown as text; the address is shown without its query. An upstream whose
error page names internal hosts, accounts or tokens shows them to every reader of the repository. What makes it real: a private
registry or proxy behind the remote whose error pages carry internal details.

### H-1729 · A reader learns what is being examined

Component: the listing of a remote (R-API-081, R-CUR-014, Q-CUR-26). Subject: what a project's deciders are considering. Live since the
examination (R2b) is built. A stored version that the rules do not deliver is listed to every reader of the repository, marked not delivered, with its
verdicts; so is an open request's examination. A reader therefore learns which packages are being looked at before a decision. What
makes it real: an examination that is confidential to its deciders, such as a package considered for a security product.

### H-1730 · A catalogue call holds a whole document in memory

Component: the upstream catalogue's lent upstream (R-CUR-008, [Q-CUR-64](../spec/remote-curation.md#q-cur-64--a-document-is-read-whole-up-to-the-bound-a-cursor-travels-base64url-the-preview-and-the-bulk-read-larger-pages)).
Subject: the replicas that answer the management API (`api`, `web`, `all`). Live. A document the catalogue reads is held whole, up to
`proxy.catalogue.max_document_bytes` (64 MiB), for the call; signed-in readers who look up large packages at once make a replica hold that
much per call. The budget (120 upstream requests a minute per repository, 30 per person) and the cache bound the rate, not the bytes in
flight at one moment. What makes it real: an upstream whose package documents approach the bound, or an operator who raises it.

### H-1800 · A replica loses up to five seconds of refusal counts at a crash

Component: the record of a client's refusal (R-CUR-004, Q-CUR-40). Subject: the request list's figure "refused to clients N times".
Live. A replica sums the refusals per subject in memory and writes them every 5 s, and once more when it stops; a replica that crashes
loses what it summed since its last write, and a replica that cannot write keeps at most 8,192 subjects. The event
`remote.delivery.refused` and the metric are not affected. What makes it real: a crash of a replica that answers many refusals; the
figure is a decider's hint, never a control.

### H-1840 · Clients' failures of many files write as many rows a minute

Component: the rows of clients' failed fetches (R-CUR-011, Q-CUR-19, Q-CUR-84). Subject: the database of the installation. Live since the
download queue (R4) is built. A failure is written at most once per repository and file per replica and minute, and at most 1,000 such
rows stay per repository, but the throttle is per file: a client, or several, that asks a failing upstream for many different files
causes one write per file and replica a minute, each followed by the trim of the repository's client rows. What makes it real: an
upstream that fails while a build fetches thousands of files through a remote, or a caller who requests made-up paths of a remote whose
upstream answers them with a 5xx instead of a 404.

### H-1841 · The usage overview does not name who started a bulk

Component: the charge of a bulk (R-CUR-010, Q-CUR-17, Q-CUR-85). Subject: an administrator who reads the usage overview to find out
who used an upstream's quota. Live since the download queue (R4) is built. A bulk is system work, recorded in the usage store under
`system` with no person; the person stands in the bulk's and its rows' `started_by`, which go after the queue's retention (7 days for
done rows, 30 for failed ones), and in the audit event `remote.download.started`, which stays as long as the audit log keeps it. What
makes it real: a quota of an upstream used up by bulks, looked into after the rows are gone, by someone without the audit log.

### H-1842 · A discard leaves what a version shares by its digest

Component: the discard and the retention of what was fetched for examination (R-CUR-016, Q-CUR-140). Subject: the object store of an
installation with OCI remotes. Live since the examination (R2b) is built. A removal takes the files a version's runs recorded and the items
the package rows link to it, each only when the format classifies it as a file of that version; an image's manifests and layers are named
by digest and belong to no tag, so they stay after the tag's examination is discarded or removed after its days, possibly malicious, until
R-STOR-045 removes unused files (specified, not built). They are not served while no delivered tag names them (Q-CUR-10), except a layer
by its digest ([H-1724](#h-1724--a-blob-of-a-refused-tag-can-be-fetched-by-its-digest-when-the-image-is-partly-allowed)). An OCI tag that a
client fetched before a rule refused it has no examination row and no version row, so a discard of it removes nothing. What makes it real:
an image examined and declined whose layers no allowed image shares, on an installation that counts its storage.

### H-1880 · The queue and a mirror pass of one repository meet an upstream's limit apart

Component: the queue's limit (R-CUR-011, [Q-CUR-173](../spec/remote-curation.md#q-cur-173--the-limit-is-a-row-per-repository-of-a-table-of-its-own-transient-and-apart-from-the-mirrors))
and the mirror's `rate_limited_until` (R-REPO-054). Subject: the upstream's quota of the installation's address. Live for a remote with a
mirror and a download queue. Each holds its own requests: a pass that waits for a limit does not stop the queue's rows, and a queue that
waits does not stop a pass, so each spends its own refused requests to learn of the same limit. What makes it real: a mirror and a bulk of
one registry with a pull limit, at the same time.

### H-1881 · A probe after a limit without a time sends up to five requests into it

Component: the run's wait and the queue's back-off ([Q-CUR-171](../spec/remote-curation.md#q-cur-171--a-run-waits-out-a-limit-of-at-most-10-s-a-longer-one-ends-its-attempt),
[Q-CUR-172](../spec/remote-curation.md#q-cur-172--the-limits-time-is-the-upstreams-else-the-queues-back-off-then-one-row-asks-first)).
Subject: the upstream's quota of the installation's address. Live. Where the upstream names no time (Docker Hub's `429` of 2026-11-25),
the probe after each hold waits about 1, 2, 4 and 8 s inside its run and asks five times before it hands the row back; the holds grow to an
hour, so about twelve probes, sixty requests, fall into the first six hours of a limit. Whether a refused request counts against a
registry's limit is not known for Docker Hub. A limit counts per address, so these requests are taken from every caller behind it: on
2026-11-25 a CI job of the same site address was refused `429` while the profile's queue had used Docker Hub's hour. What makes it real:
a registry that counts refused requests and names no time, and neighbours behind the installation's address.

### H-1882 · A client's fetch asks an upstream whose limit holds the queue

Component: the client's fill (R-REPO-029) beside the queue's limit (R-CUR-011). Subject: the upstream's quota and the clients of the
remote. By design: a client's miss is never held by the background's state, and it gets the upstream's answer as R-REPO-029 classifies it
(`upstream_rate_limited` with its `Retry-After`). A client that retries at once spends the limit the queue waits out. What makes it real: a
build that pulls through the remote while a bulk waits for the same limit.

### H-2020 · A restore to a point before allow-stored refuses the packages it allowed

Component: the change of the delivery mode (R-CUR-025, Q-CUR-204) and the journal's replay (R-CUR-006). Subject: the clients of a remote
whose mode went to `onlyAllowed` with its stored packages allowed. Dormant until a restore to a point between the allows and the mode's
change, or before both with the mode's entry after it. `allow-stored` widens and is not journaled; the mode's change narrows and is. A
restore to a point before `allow-stored` replays the mode and has no allows, so the packages that stayed delivered are refused (fail
closed) until a holder of `manage` allows them again. What makes it real: a restore after a change of the mode of a remote that clients
use.

### H-2021 · A large allow-stored writes as many audit events and webhook deliveries

Component: `allow-stored` (R-CUR-025, Q-CUR-208). Subject: the audit log and the webhooks of an installation. Live. Every rule it writes
records `remote.rule.created`, 50,000 at the bound, in one transaction (measured 4.8 s on the development machine); a subscription to
the event receives as many deliveries. Not measured: how the webhooks' deliveries scale. What makes it real: a change of the mode of a
remote that stores tens of thousands of packages, with a subscription to `remote.rule.*`.

### H-1883 · A reader of the queue sees how much of the upstream's quota is left

Component: the list's `limit` and a row's failure ([Q-CUR-174](../spec/remote-curation.md#q-cur-174--a-limits-failure-keeps-the-upstreams-answer-and-the-headers-of-the-limit-and-no-other),
[Q-CUR-175](../spec/remote-curation.md#q-cur-175--the-list-answers-the-limit-beside-its-counts-for-every-reader)). Subject: the installation's
use of an upstream. Live. The headers of a limit (`RateLimit-Limit: 100;w=3600`, `RateLimit-Remaining: 0`) and the time the queue waits
show every reader of the repository the quota of the installation's address and that it is used up; the address itself is not kept. What
makes it real: an upstream account whose quota is a commercial figure.
