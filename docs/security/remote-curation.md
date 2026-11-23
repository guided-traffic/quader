# Curating a remote repository

Status: specified on 2026-11-23, not built ([remote-curation.md](../spec/remote-curation.md)). Every section below describes that design.
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
- A redirect of a catalogue request follows the redirect rules of the remote (R-REPO-039), as a discovery request of a mirror does
  ([H-525](egress.md#h-525--a-redirect-of-a-discovery-request-is-followed-to-any-host-the-egress-policy-admits)).
- Docker Hub's repository search is an endpoint its operator does not document; it is used with a fallback that turns the search off
  when its answer is not of the expected shape ([H-1723](#h-1723--docker-hubs-search-is-an-endpoint-its-operator-does-not-document)).

## The bounds on what an upstream answers

- A document the catalogue reads is at most 64 MiB and parsed as a stream; a call ends after 20 s; a page holds at most 50 hits, 200
  entries of a level or 100 versions; a text is cut at 1,000 characters (R-CUR-008). The catalogue's cache keeps parsed pages, 32 MiB per
  replica, never a document's bytes, and nothing it keeps is served to a client.
- A catalogue call stores nothing in the repository's cache, its rows or the negative cache: browsing never populates, so the rule that a
  session cookie never fills a cache (R-AUTHZ-008) holds for it unchanged.
- A failure's excerpt in the download queue is the first 512 bytes of the upstream's answer as printable text, and the address is shown
  without its query, because a CDN's signed address carries a credential in it (R-CUR-011)
  ([H-1728](#h-1728--the-excerpt-of-an-upstreams-error-answer-is-shown-to-every-reader-of-the-repository)).

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
- A narrowing (a block added, an allow removed, the mode set to `onlyAllowed`) is journaled, so a restore keeps it; a widening is not.

## Who can make Quader download how much

| Who | What | Bound | Charged to |
|---|---|---|---|
| A reader who may populate | one delivered version, through the management API | the person's upstream budget and the remote's ceiling (R-LIM-037), one row per version, the waiting rows of the repository | the person |
| A reader who asks for an allow | the examination of the version asked (of a package, its newest) | 20 open requests per person and repository; the person's budget | the person |
| A holder of `manage` | one version for examination; every version of a package; a sub-section | per bulk 10,000 versions and 100 GiB, the waiting rows (10,000) and four running rows per repository, the background share of the fetch slots, the upstream's throttling | nobody: system work, recorded with the person ([H-1725](#h-1725--a-holder-of-manage-can-make-quader-fetch-up-to-a-bulks-bound-without-a-ceiling)) |
| An administrator | a mirror's scope | `remote.mirror.include`, `maxBytes` | nobody (Q-LIM-19) |

- A probe's background download keeps its charge and bounds (R-REPO-065), as a row of the queue.
- A download ahead writes no download statistics and no usage download, so the figures of downloads stay the clients'.

## What is kept of content no client may get

- A download ahead stores what it names whatever the rules say, so the installation's object store holds content that no client may
  get, possibly malicious: the vulnerability miss gate still refuses what the advisories name as malicious or a triager blocked, and
  the malware gate refuses what ClamAV finds at every serve point, but a format the advisories do not cover (Debian, Ansible Galaxy, OCI
  before its analysis) stores it unexamined by them ([H-1727](#h-1727--content-fetched-for-examination-lies-in-the-object-store-until-it-is-discarded)).
- It is removed by a discard, by a decline, or after the repository's days without a decision (30 by default), each journaled, so a
  restore removes it again (R-CUR-016).
- Who sees it: whoever reads the repository sees the stored version as not delivered, with its verdicts
  ([H-1729](#h-1729--a-reader-learns-what-is-being-examined)).

## What this does not cover

- [H-132](repository-model.md#h-132--a-replica-enforces-the-previous-repository-settings-until-its-snapshot-is-rebuilt): a replica decides
  on its snapshot of the rules until it is rebuilt.
- [H-525](egress.md#h-525--a-redirect-of-a-discovery-request-is-followed-to-any-host-the-egress-policy-admits): a redirect of a catalogue
  request is followed to any host the egress policy admits, as a discovery request's is.

### H-1720 · A search tells the upstream's operator what people look for

Component: the upstream catalogue (R-CUR-008). Subject: the people of an installation who search a remote's upstream. Dormant until the
catalogue is built; live for every remote whose upstream answers a search. The query, from Quader's address and with the repository's
credential where the upstream receives it, reaches the upstream's operator, who learns the names and words an organisation looks for. The
search guard withholds only terms a rule protects. What makes it real: an operator that logs queries per account, and a search for an
internal project's name that no rule protects.

### H-1721 · A reader of a remote that delivers only what is allowed can look up names one by one

Component: the catalogue's lookup by exact name (R-CUR-009). Subject: a remote in mode `onlyAllowed` whose upstream is private (a
credential). Dormant until built. Search and levels show a reader only what is delivered, but a lookup by exact name answers any package
with its versions, so that a reader can ask for it; a reader who guesses names learns which exist in the private upstream and their
versions, bounded by the catalogue's budget per person (30 requests a minute). What makes it real: a private registry whose names are
confidential to its deciders.

### H-1722 · The catalogue shows what the upstream credential sees

Component: the catalogue (R-CUR-008, R-CUR-009). Subject: every reader of a remote whose upstream credential reads more than the readers
should see. Dormant until built. The catalogue asks with the repository's credential, so its answers are that account's view; in mode
`allExceptBlocked` every signed-in reader of the repository browses it. A reader could already fetch every one of those packages through
the remote in that mode, so the catalogue adds the listing, not the access. What makes it real: an account of a hosted registry that sees
packages of several organisations.

### H-1723 · Docker Hub's search is an endpoint its operator does not document

Component: the OCI format's catalogue for the preset `docker-hub` (R-OCI-038). Subject: every Docker Hub remote. Dormant until built.
`GET /v2/search/repositories/` of the Hub API is not in its reference; it may change or disappear without notice. The fallback turns the
search off for the cache period when the answer is not of the expected shape, so a change shows as "search unavailable", never as a
wrong answer, but an answer that keeps the shape and changes its meaning (a different ranking, a cut window) is shown as it comes. What
makes it real: a change of the Hub API.

### H-1724 · A blob of a refused tag can be fetched by its digest when the image is partly allowed

Component: the delivery rules for OCI (R-OCI-038, Q-CUR-10). Subject: an image of a remote with version rules (some tags allowed or
blocked). Dormant until built. A blob by digest is delivered when some version of the image is, because a blob names no tag; a client
that knows the digest of a layer of a refused tag fetches it, through the remote and from the upstream. Manifests by digest are decided by
the stored tags that name them, so a client cannot pull the refused tag's image as a whole. What makes it real: a layer that a refused tag
holds and an allowed one does not, and a client that learned its digest elsewhere; the signature gate made the same cut (H-1196).

### H-1725 · A holder of `manage` can make Quader fetch up to a bulk's bound without a ceiling

Component: the downloads ahead (R-CUR-010, Q-CUR-17). Subject: the upstream's quota of the installation, its object store, and the
upstream's operator. Dormant until built. A bulk is system work: no ceiling of the remote counts it. A Maintainer can start bulks one after
another, each up to 10,000 versions and 100 GiB, bounded by the waiting rows and the four running rows per repository, the background
share of the fetch slots and the upstream's throttling, not by a budget. On Docker Hub anonymously, 100 manifest pulls a window are used
up by one bulk of 50 two-platform tags, and every client of the installation's address waits for the window. What makes it real: a
Maintainer who downloads a large section, or several, of a registry with a pull limit.

### H-1726 · A Debian remote addressed directly lists what its rules refuse

Component: the delivery rules for Debian (R-DEB-036, Q-CUR-9). Subject: an APT client that uses a Debian remote directly instead of a
virtual repository over it. Dormant until built. The upstream's signed indexes pass unchanged, so `apt` lists every package and is
refused the file with a status line that carries no reason; the refused content is not served, but the listing shows what the rules
were meant to keep out of view. What makes it real: a client set up against the remote in mode `onlyAllowed`; the client setup names the
virtual repository instead.

### H-1727 · Content fetched for examination lies in the object store until it is discarded

Component: the examination (R-CUR-014, R-CUR-016). Subject: the installation's object store and every process that reads blobs from it
(the scanners, the replicas). Dormant until built. A download ahead stores a version that no client may get, which may be malicious: the
vulnerability miss gate refuses only what the advisories of npm and NuGet name, ClamAV's verdict follows the store, and Debian and Ansible
Galaxy content is not analysed for vulnerabilities at all. It stays until a discard, a decline or the days of the repository pass
(30 by default). It is never served while the rules refuse it, and the malware gate refuses what ClamAV finds once it is delivered. What
makes it real: a malicious package fetched for examination whose malware no signature describes, later allowed by a decider who reads
"No malware found" as "safe" (R-MAL-003).

### H-1728 · The excerpt of an upstream's error answer is shown to every reader of the repository

Component: the download queue's failures (R-CUR-011). Subject: every reader of a remote's tab **Downloads**. Dormant until built. The
first 512 bytes of an upstream's non-success answer are kept and shown as text; the address is shown without its query. An upstream whose
error page names internal hosts, accounts or tokens shows them to every reader of the repository. What makes it real: a private
registry or proxy behind the remote whose error pages carry internal details.

### H-1729 · A reader learns what is being examined

Component: the listing of a remote (R-API-081, R-CUR-014, Q-CUR-26). Subject: what a project's deciders are considering. Dormant until
built. A stored version that the rules do not deliver is listed to every reader of the repository, marked not delivered, with its
verdicts; so is an open request's examination. A reader therefore learns which packages are being looked at before a decision. What
makes it real: an examination that is confidential to its deciders, such as a package considered for a security product.
