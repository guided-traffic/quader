# Usage and limits

Status: specification of walk item W-26 (the owner's decisions of 2026-11-19); not implemented. The work packages that build it
are cut in [docs/developer/usage-and-limits.md](../developer/usage-and-limits.md).

This page specifies how much a caller may download from Quader and how fast, and how the people who run an installation see what
every caller downloads. It covers:

- the subject a request is charged to (a user, a technical token, a CI project, an anonymous address), for every kind of caller and
  credential;
- the figures that are counted (requests, bytes from the cache, bytes from the upstream, upstream requests, parallel downloads, a
  day's volume, refused requests), what each counts in every protocol, and their windows;
- profiles (named sets of every figure), assignments of a profile to a caller at the instance, a project or a repository, the
  order in which an assignment is found, and the ceilings of a remote repository for all anonymous and all signed-in callers
  together;
- the counters across replicas, what a request costs, the answer at each limit, slowing down, the block after refused requests,
  and the switch between Audit and Enforce;
- the usage store behind the overview, its retention, who reads it, its API;
- the rule of the format contract that makes every format, built and future, count by these figures;
- the hook that the background downloads of W-25 use.

The technical tokens on three levels, which decision 12 asks for, are identity: they are specified in
[identity-and-authentication.md](identity-and-authentication.md) R-AUTH-045 and R-AUTH-046 and
[authorization.md](authorization.md) R-AUTHZ-039; this page says how they are charged (R-LIM-038). The screens are in
[api-and-ui.md](api-and-ui.md) R-UI-177 to R-UI-186.

| Topic | Owner |
|---|---|
| The middleware chain and its stage 7, the client address and trusted proxies, the limiter of the cluster | [architecture.md](architecture.md) R-ARCH-005, R-ARCH-006, R-ARCH-034 |
| The general rate-limit classes of the management API, which stay | [api-and-ui.md](api-and-ui.md) R-API-016 |
| The endpoint's own rate-limit class, which stays | [endpoints.md](endpoints.md) R-EP-015 |
| Login and token-validation throttles, which stay | [identity-and-authentication.md](identity-and-authentication.md) R-AUTH-038 |
| The CI exchange's own limiters, which stay | [gitlab-ci-oidc.md](gitlab-ci-oidc.md) R-CI-021 |
| The remote request path, the collapsing protocol, stale serving | [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-025, R-REPO-026, R-REPO-029 |
| The mirror's own bounds and its throttling of the upstream | [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-052 to R-REPO-055 |
| Download statistics per item (not per caller) | [storage.md](storage.md) R-STOR-015 |
| Storage quotas (later, not this page) | [storage.md](storage.md) R-STOR-029 |
| Service accounts, technical tokens, their grants | [identity-and-authentication.md](identity-and-authentication.md) R-AUTH-020, R-AUTH-045, R-AUTH-046; [authorization.md](authorization.md) R-AUTHZ-026, R-AUTHZ-039 |
| Capabilities `limits:read`, `limits:write`, `usage:read` | [authorization.md](authorization.md) R-AUTHZ-040 |
| Metric labels, the request log, the audit catalog | [operations-and-observability.md](operations-and-observability.md) R-OPS-007, R-OPS-010, R-OPS-016 |
| The screens | [api-and-ui.md](api-and-ui.md) R-UI-177 to R-UI-186 |

Covers: [beyond-baseline.md](../features/beyond-baseline.md) BEYOND-030 (per-tenant quotas and rate limits), taken up by the
owner on 2026-11-19 for downloads; its storage quota part stays with [storage.md](storage.md) R-STOR-029.

## The owner's decisions

The owner decided the model on 2026-11-19 in his working paper (walk item W-26 of the project plan). They are not reopened on this
page; every decision block below that cites one of them builds on it.

| # | Decision of the owner, 2026-11-19 | Where |
|---|---|---|
| 1 | A group's limit is a profile for each member, each with a budget of its own; not a shared pot | R-LIM-013 |
| 2 | A caller in several groups gets the most generous value, per figure the larger one (unlimited wins) | R-LIM-013 |
| 3 | Instance, project, repository: the most specific wins; a repository may set a caller's limit higher than the instance's | R-LIM-013, R-LIM-016 |
| 4 | At the limit, what fits the figure: requests 429 with the time to wait; bytes per second slowed, no error; bytes per hour or day 429 before a download starts, never in the middle; upstream budget used up: what is cached is still served | R-LIM-020 to R-LIM-024 |
| 5 | Windows: second, hour and day | R-LIM-004, R-LIM-018 |
| 6 | A technical token's budget hangs on the single token: "Ich kann hier für jedes System einen eigenen Token ausstellen und die Budgets damit pro System genau einstellen." | R-LIM-001, R-LIM-038 |
| 7 | An anonymous address: IPv4 by itself, IPv6 per /64, and a ceiling for all anonymous callers together | R-LIM-002, R-LIM-015 |
| 8 | Instance administrator and auditor see everything, a project Owner what happens in the project, every user their own; single values 30 days, after that sums without a person | R-LIM-027, R-LIM-029 |
| 9 | Without any setting: known callers the profile Standard, anonymous callers the profile Anonymous, administrators unlimited | R-LIM-012 |
| 10 | Everything in one step | this page |
| 11 | "Anonymous" is a fixed class of the limit settings only; no group and no policy names the anonymous caller ([identity-and-authentication.md](identity-and-authentication.md) R-AUTH-037 stays) | R-LIM-011 |
| 12 | Technical tokens are a kind of their own on three levels, instance, project and repository, each with a budget of its own | R-LIM-038, R-AUTH-045, R-AUTH-046 |
| 13 | An account for the rights, a token for the budget: rights are given once, at the account; each token has a name for its system and a budget of its own; a rotated token keeps its budget and its consumption | R-AUTH-045 |
| 14 | Only the instance administrator raises above the instance; a project Owner narrows or redistributes in project and repository, higher than the instance only up to a maximum the instance administrator gives the project | R-LIM-016 |
| 15 | Consumption under a repository's own rule counts against that rule only; the caller's instance budget stays for every other repository; the overview shows consumption per place and the sum | R-LIM-014 |
| 16 | The order: the place first, then the caller. Repository before project before instance; at each place the rule for the single caller or address, else the caller's groups with the most generous value per figure, else the default of its kind | R-LIM-013 |
| 17 | A switch Audit / Enforce for the instance, set by the instance administrator, default Audit; in Audit everything is counted and the overview shows who would have been limited, nothing is refused or slowed; the UI and the API use his two words | R-LIM-026 |

And, binding for every format (owner, 2026-11-19): "Alles was wir hier für OCI besprechen gilt auch für alle anderen Registries
die bereits gebaut sind und für zukünftige Registries." Every rule below is stated for the format contract and holds for Ansible
Galaxy, Debian, generic, npm, NuGet and OCI; where a protocol cannot carry a rule, it is named in
[Gaps of the protocols](#gaps-of-the-protocols) with options for the owner.

Every number of the built-in profiles is the development agent's estimate, which the owner did not change; none is measured.

## Terms

| Term | Meaning |
|---|---|
| Caller | Whoever sends a request: a person through a session or a personal token, a system through a technical token, a CI job through its CI token, or an anonymous client. |
| Subject | The unit a budget hangs on (R-LIM-001): a user, a technical token, a CI project, an anonymous address, or the system. Several credentials can share one subject (a user's sessions and personal tokens); one credential always has exactly one subject. |
| Figure | One counted quantity with its window: requests per second with a burst, requests per hour, bytes from the cache per second and per hour, bytes from the upstream per second and per hour, upstream requests per hour, parallel downloads, bytes per day, refused requests per hour (R-LIM-004). |
| Window | The time a figure counts over: one second (with a burst, so the window of the request figure is `burst ÷ rate` seconds), one hour, one day. Windows are rolling (R-LIM-018). |
| Profile | A named set of every figure (R-LIM-010); an empty figure is unlimited. Built-in profiles: Unlimited, Standard, Technical, Restricted, Anonymous; an instance administrator adds own ones. |
| Assignment (rule) | A place, a "who" (a single user, a single technical token, a CI project, an address or address range, a group, or every caller of a kind), a profile and optional overrides of single figures (R-LIM-011). |
| Place | Where a rule is set and where its budget counts: the instance, a project, or a repository. |
| Budget key | A subject together with the place whose rule applied to a request (R-LIM-014). Every counter of a subject is a counter of one budget key. |
| Ceiling | A remote repository's limit for a class of callers taken together: all anonymous callers, or all signed-in callers (R-LIM-015). |
| Mode | `audit` or `enforce`, one setting of the instance (R-LIM-026). In Audit every decision is computed and recorded, none applied. |
| Cached bytes | Bytes of a response body that Quader sent to the caller from what it already held (R-LIM-006). |
| Uncached bytes | Bytes Quader received from an upstream in an exchange the caller led (R-LIM-006). |
| Upstream request | One exchange the remote engine starts with an upstream for the caller, a revalidation included (R-LIM-006). |
| Download | A response whose body is content: the answer of a route of the kind `Download` to a `GET` with a body (R-LIM-007). Only downloads are checked before they start, slowed and counted as parallel. |
| Document | A response body of a route of the kind `Metadata`, `Computed` or `Volatile` (an npm package document, a NuGet registration, an APT index, an OCI manifest of a tag). |
| Refused request | A request the decision point refused, whose credential failed, or whose repository does not exist (R-LIM-008). |
| Block | The state of a budget key whose refused requests passed their limit: every request charged to it is answered 429 for the block's time (R-LIM-025). |
| Probe | A request that names exactly one version without fetching its content; it starts a background download under W-25, built later. This page fixes only how that download is charged (R-LIM-037). |
| Protocol path | A path of a repository protocol: `/api/<format>/<projectKey>/<repoKey>/…`, `/files/<projectKey>/<repoKey>/…`, and everything below `/v2/` (R-LIM-005). |

## The model

Every request on a protocol path must fit two budgets measured in the same figures: the budget of its subject at the place whose
rule applies, and, where it needs the upstream of a remote repository, that repository's ceiling for its class of callers. Both
hold; neither replaces the other.

```text
            request on a protocol path
                       │
   stage 7 ── subject (R-LIM-001) ── place and rule (R-LIM-013) ── budget key (R-LIM-014)
                       │                   requests per second / hour, block (R-LIM-025)
                       ▼
   handler ── document or download? ── cached or a miss?
                       │                         │
          download from the cache          miss: step 9a of R-REPO-025
          (edge.ServeItem):                upstream requests, uncached bytes,
          cached bytes per hour,           the remote's ceiling (R-LIM-015),
          bytes per day, parallel          parallel (R-LIM-021, R-LIM-022)
          (R-LIM-021, R-LIM-024)
                       │
          body: counted, paced (R-LIM-023) ──► per-replica buffer ──► Valkey (1 s), usage store (10 s)
```

## Subjects and attribution

### R-LIM-001 · The subject a request is charged to
Priority: **MVP** · Covers: BEYOND-030

The authentication stage (R-ARCH-005 step 6) has decided the principal and the credential; the subject follows from them alone, by
this table. The subject is decided once per request and carried in the request context (`limits.Charge`, R-LIM-017) to every
place that counts.

| Caller and credential | Subject kind | Subject identifier | Notes |
|---|---|---|---|
| User through a UI session | `user` | the user's principal ID | The session and every personal token of the user share the subject (owner: "Ein PAT gehört zu einem konkreten User und geht direkt gegen das Budget des Users"). |
| User through a personal access token (`qdr_pat_`), as Bearer, Basic password, NuGet API key or Galaxy `Token` | `user` | the user's principal ID | The usage store records the token's ID as the credential (R-LIM-027), so the overview splits a user's consumption into session and tokens. Today's bucket key "token ID first" of R-API-016 goes. |
| Technical token (`qdr_sat_`) of a service account of the instance, a project or a repository | `technicalToken` | the technical token's ID (`technical_token.id`, R-AUTH-045) | Not the account and not the secret: rotation and a new secret keep the subject, its budget and its consumption (owner, decisions 6 and 13). |
| CI token (`qdr_cit_`) | `ci` | `<issuer ID>:<project ID>`: the CI issuer's immutable ID and the `ProjectID` of the token's frozen bundle (the verified pin claim, `project_id` for GitLab) | Every job of a pipeline's project shares one budget, whatever job, pipeline or mapping issued it ([Q-LIM-1](#q-lim-1--a-ci-token-is-charged-to-its-issuer-and-the-project-of-its-bundle)). |
| Registry token of the OCI token service (`qdr_rgt_`) | the subject of the Quader token it was issued for | as that token's row above | `Credential.TokenID` is the underlying token ([formats/oci.md](formats/oci.md) R-OCI-005); a registry token issued without a Quader token is anonymous and charged to the address. |
| No credential | `anonymous` | the client address prefix of R-LIM-002 | |
| A credential that failed (unknown, revoked, expired, wrong checksum, refused by an endpoint's credential rules) | `anonymous` | the client address prefix | R-LIM-003. |
| Break-glass administrator | `user` | its principal ID | An administrator (R-LIM-012). |
| A request a Bastion forwards to the main installation with the caller's credential | the subject of that credential | as above | The address is the forwarded client's ([bastion.md](bastion.md) R-BAS-017). What the Bastion serves from its own cache never reaches the main installation and is not counted here (Later). |
| The Bastion's own interface requests (principal kind `bastion`) | none | — | They are not protocol paths; R-API-016 and R-BAS-016 bound them. |
| System work: mirror passes, the Debian index parse, the Ansible documentation job, every background request of the engine (`proxy.PriorityBackground`) | `system` | `system` | Recorded per repository, limited by nothing of this page ([Q-LIM-19](#q-lim-19--mirror-passes-and-other-system-work-are-recorded-and-not-limited)). |

- A subject identifier never contains a token secret or a hash of one.
- A request on a protocol path whose principal kind is none of the above (a future kind) is charged to the client address until
  this table names it; a test lists the principal kinds of [internal/httpserver/principal.go](../../internal/httpserver/principal.go)
  and fails for a kind the table does not name.

Acceptance criteria:

- Alice downloads 1 MB through the UI session and 2 MB with each of two personal tokens; `GET /api/v1/usage/subjects/user/alice`
  shows 5 MB of cached bytes for the subject and the split session 1 MB, token A 2 MB, token B 2 MB.
- A technical token `deploy-prod` is rotated with an overlap of one hour; downloads with the old and the new secret are charged to
  one subject, and the rotated token's consumption before the rotation stays in its history.
- Two jobs of GitLab project 4711, issued through two different mappings, are charged to `ci` `<issuer ID>:4711`; a job of project
  4712 to another subject.
- `docker pull` with a registry token issued for Bob's personal token is charged to `user` Bob.

### R-LIM-002 · The address of an anonymous subject
Priority: **MVP** · Covers: BEYOND-030

- The address is the client address of [architecture.md](architecture.md) R-ARCH-006 (the trusted-proxy rule and the one header
  family of `server.forwarded_header`), never a header the stage did not trust.
- IPv4: the address itself (`/32`). IPv6: the `/64` prefix the address lies in, written as `<prefix>::/64` in canonical form
  ([RFC 5952](https://www.rfc-editor.org/rfc/rfc5952)). An IPv4-mapped IPv6 address (`::ffff:198.51.100.7`) is the IPv4 address.
- An IPv6 zone is dropped before the prefix is taken.
- A request without a client address (a command, a test) is charged to the subject `anonymous` `unknown`, one subject for all of
  them.

Acceptance criteria:

- Two requests from `2001:db8:1:2::10` and `2001:db8:1:2:ffff::1` are charged to `2001:db8:1:2::/64`; one from `2001:db8:1:3::1`
  to another subject.
- Behind a trusted proxy that sends `X-Forwarded-For: 198.51.100.7`, the subject is `198.51.100.7`; the same header from an untrusted
  peer is ignored and the peer's address is the subject.

### R-LIM-003 · A failed credential, the registry's token service and the Bastion
Priority: **MVP** · Covers: BEYOND-030

- **A failed credential.** A request whose credential the authentication stage refused is answered 401 there (R-AUTH-002), before
  stage 7 (R-ARCH-005). The stage records it for the address of its client through the replica's charge buffer: one request and one
  refused request (R-LIM-008), which reach the refused window at the next flush and the usage store. It is not checked against the
  request windows or a block (it never reaches stage 7); the failed-credential throttle of R-AUTH-038 bounds such requests per
  address. It is never charged to the subject the credential names, so a wrong or revoked token does not spend the budget of the
  token's owner.
- **The registry's token service.** `GET /v2/_token` authenticates its own Basic credential in its handler
  ([formats/oci.md](formats/oci.md) R-OCI-005, Q-OCI-51), so stage 7 sees it as anonymous. Its request figures are charged after
  the handler authenticated: to the subject of the presented Quader token when it was valid, to the address when there was none or
  it failed. The handler calls `limits.Charge.Retarget` before it answers; a token request that is refused by the handler's own
  checks is a refused request of the address.
- **A Bastion's forwarded request.** As R-LIM-001: the caller's credential decides the subject; an anonymous one is the forwarded
  client's address.

Acceptance criteria:

- 100 requests with a revoked personal token of Alice from `198.51.100.7` leave Alice's counters untouched and count 100 requests
  and 100 refused requests on `198.51.100.7`.
- `docker login` and a pull through a public OCI remote: the token request and the pull with the registry token are charged to the
  user, not to the address.

## Figures

### R-LIM-004 · The figures
Priority: **MVP** · Covers: BEYOND-030

| Figure (API name) | Unit | Window | What counts | At the limit (owner, decision 4) |
|---|---|---|---|---|
| `requestsPerSecond` with `requestsBurst` | requests | `requestsBurst ÷ requestsPerSecond` seconds | every request on a protocol path (R-LIM-005) | 429 `rate_limited` with `Retry-After` (R-LIM-020) |
| `requestsPerHour` | requests | 1 hour | the same | 429 `rate_limited` |
| `cachedBytesPerSecond` | bytes per second | 1 second | cached bytes of downloads (R-LIM-006, R-LIM-007) | the download is slowed, no error (R-LIM-023) |
| `cachedBytesPerHour` | bytes | 1 hour | cached bytes of every body | 429 `download_limit_reached` before a download starts (R-LIM-021) |
| `uncachedBytesPerSecond` | bytes per second | 1 second | uncached bytes of fills the caller leads | the fill is slowed, no error (R-LIM-023) |
| `uncachedBytesPerHour` | bytes | 1 hour | uncached bytes | what is cached is still served; what needs the upstream answers 429 `upstream_limit_reached` (R-LIM-022) |
| `upstreamRequestsPerHour` | upstream requests | 1 hour | upstream requests for the caller (R-LIM-006) | as `uncachedBytesPerHour` |
| `parallelDownloads` | downloads | — (a count of downloads in progress) | the subject's downloads in progress across all replicas (R-LIM-024) | a further download waits up to 10 s, then 429 `too_many_parallel_downloads` |
| `bytesPerDay` | bytes | 1 day | cached plus uncached bytes | 429 `download_limit_reached` before a download starts |
| `refusedPerHour` | refused requests | 1 hour | refused requests (R-LIM-008) | the budget key is blocked for the block's time (R-LIM-025) |

- `requestsBurst` is at least `requestsPerSecond`; a profile that sets `requestsPerSecond` and leaves `requestsBurst` empty takes
  `requestsBurst = requestsPerSecond` (a window of one second). `requestsBurst` without `requestsPerSecond` is refused (422
  `invalid_figure`).
- Every figure is an integer of at least 1, or empty (`null`) for unlimited. Bytes are bytes in the API; the UI shows decimal MB
  (10^6) and GB (10^9) as the owner's table does (R-UI-178).
- Background downloads (W-25) are no figure of their own: they count against the uncached figures of the caller that triggered
  them and against the remote's ceiling (R-LIM-037). The usage store counts them apart.

### R-LIM-005 · What a request is, and the border to the management API
Priority: **MVP** · Covers: BEYOND-030

- **A request** is every HTTP request on a protocol path that reaches stage 7 of R-ARCH-005: every method, `HEAD` and a request
  answered `304` included, a request the handler refuses included. The protocol paths are `/api/<format>/…` (every path below
  `/api/` that is not `/api/v1/`), `/files/…` and everything below `/v2/` (the version check `/v2/`, `/v2/_catalog` and `/v2/_token`
  included). F-155 of the project plan is closed by this: the registry's paths are counted like every other protocol path.
- **Not counted as a request**: a request without a credential that the decision point is certain to answer with the challenge
  (the rule of Q-API-290 in [api-and-ui.md](api-and-ui.md), unchanged: a NuGet client's first attempt without its credentials);
  a `OPTIONS` preflight; a request refused before stage 7 (`421`, `503 server_busy`, the endpoint's class of R-EP-015), except a
  failed credential, which R-LIM-003 records.
- **The management API** (`/api/v1/…`), `/auth/…`, `/ui/…` and the host-root exchange routes are not protocol paths. They keep the
  classes `read`, `write` and `anonymous` of [api-and-ui.md](api-and-ui.md) R-API-016 and are counted by nothing on this page; a
  download through the UI is a request on `/files/` and counted here. The CI exchange keeps R-CI-021, login and token validation
  R-AUTH-038.
- **The package classes of R-API-016** change ([Q-LIM-3](#q-lim-3--protocol-paths-are-counted-by-the-figures-the-management-api-keeps-its-classes),
  [Q-LIM-4](#q-lim-4--in-audit-mode-the-package-classes-of-r-api-016-stay-in-force-as-the-safety-net)): `package_read` and
  `package_anonymous` apply to every protocol path (`/v2/` and `/files/` included), keyed by the subject of R-LIM-001, and only
  while the mode is `audit`; in `enforce` the request figures replace them. `package_write` applies to unsafe methods on every
  protocol path in both modes until the upload figures (Later) replace it
  ([Q-LIM-27](#q-lim-27--uploads-stay-under-package_write-in-both-modes-until-the-upload-figures-exist)).
- **The endpoint's class** (R-EP-015) runs first at stage 7 and still holds: a request on a further endpoint must fit the endpoint's
  class and the figures of this page.
- **Which repository a request names** is read through the one parser of repository addresses (`qname.ParseAddress`, R-ARCH-005),
  which learns the family `registry` for `/v2/<projectKey>/<repoKey>/…`; on a host bound to a repository (R-EP-006, R-EP-007) the
  binding names it. A request that names no repository (`/v2/`, `/v2/_catalog`, `/v2/_token`, a key that does not parse, a
  repository that does not exist) is decided at the instance place (R-LIM-013).

Acceptance criteria:

- In `enforce` with the profile Anonymous, 200 `GET /v2/` from one address within 2 s are admitted at most 140 times (burst 100
  plus 20 per second); in `audit` the class `package_anonymous` (20 per second, burst 100) refuses the same requests, as it does on
  `/api/npm/…`.
- 300 `GET <feed>/v3/flatcontainer/<id>/index.json` without a credential on a NuGet repository whose artifacts are `internal` are
  all answered 401 and none is counted as a request or a refused request of the address.

### R-LIM-006 · Cached bytes, uncached bytes and upstream requests
Priority: **MVP** · Covers: BEYOND-030

**Cached bytes** are the bytes of response bodies that Quader writes to the client of a request on a protocol path, counted where
they are written (the counting writer the format dispatcher, the raw-file handler and the registry root put around the response,
R-LIM-017), except the bytes of an artifact that reached the client through the leader's stream of a remote fill (below), which
are uncached bytes. In particular:

- `HEAD` and `304` write no body and count no bytes; a `206` counts the bytes of its range; an aborted transfer counts what was
  written before it ended (the owner: "an aborted or resumed download counts what was transferred").
- A metadata document counts its bytes like content ([Q-LIM-6](#q-lim-6--documents-count-their-bytes-only-content-downloads-are-checked-before-they-start-slowed-and-counted-as-parallel)):
  an npm package document of several megabytes is a large part of an install.
- Local repositories know cached bytes only.

**Uncached bytes** are the bytes of upstream response bodies that the remote engine reads in an exchange the caller leads, counted
where the body is read (`(*fetcher).artifact` and `(*fetcher).metadata` in [internal/proxy](../../internal/proxy)). While the leader's
stream writes to its client, the engine marks the request's charge, and the counting writer neither counts nor paces those writes:
they are counted once, as uncached bytes, where they were read.

- **The leader of a collapsed fetch** (R-REPO-026) is charged the upstream request and every uncached byte of the fetch; every
  request that waited for the flight, on this replica or another, is served from the stored entry and charged cached bytes only
  (owner: "the second caller of the same file pays cached only";
  [Q-LIM-5](#q-lim-5--a-collapsed-fetch-is-charged-to-its-leader-alone-also-after-its-client-left)).
- **A fetch that goes on after its client left** (the detached fetch of R-REPO-026) is charged in full to the leader, at the
  bytes the engine read, also those after the client left.
- **An upstream answer larger than what the client receives** (a ranged request whose miss fetches the whole artifact first, a
  client that leaves, an upstream document Quader rewrites or abbreviates): the uncached bytes are what Quader read from the
  upstream; the bytes the client then receives from the stored item are cached bytes. For an artifact the leader's client receives
  through the stream, the streamed bytes are counted once, as uncached bytes. A document fetched fresh for the request therefore
  counts its upstream bytes as uncached and the rendering sent to the client as cached.
- **A follower that fetches for itself** after waiting (a flight that left nothing to read, R-REPO-026) is the leader of its own
  fetch and is charged as one.

**An upstream request** is one exchange the remote engine starts with the upstream for the caller's request: the fetch of an
artifact, the fetch or the revalidation of a document (a conditional `GET` answered `304` included), a `HEAD` the engine sends for
a client's `HEAD`, a forwarded search. The token handshake of the exchange (R-REPO-064, OCI), its redirects (R-REPO-039) and its
retries inside one attempt belong to the exchange and are not counted again; a new attempt after a failure is a new exchange. An
exchange that a refusal before it prevented (offline, an open circuit, a negative entry, the populate check, the budget of
R-LIM-022) is none.

**Through a virtual repository** a request is decided once, at the repository the client addressed (R-LIM-013,
[Q-LIM-2](#q-lim-2--a-request-through-a-virtual-repository-is-decided-at-the-repository-the-client-addressed)), and counted once:
its bytes and upstream requests are recorded in the usage store under the member that served them; the ceiling that applies to an
upstream request is the remote member's.

**System work** (R-LIM-001) records its upstream requests and uncached bytes under the subject `system` of the repository and is
checked against nothing.

Acceptance criteria:

- Three clients request the same uncached 10 MB tarball at once through one remote: the leader is charged 1 upstream request and
  10 MB of uncached bytes; each of the two others 10 MB of cached bytes and no upstream request.
- A client that reads the first 1 MB of an uncached 100 MB blob and disconnects is charged 100 MB of uncached bytes once the
  detached fetch ended, and no cached bytes.
- `curl -r 0-99` of an uncached 1 MB file is charged 1 MB of uncached bytes and 100 cached bytes.
- `npm install` of a lockfile with 200 cached tarballs through a virtual repository records 200 downloads under the remote member,
  none under the virtual repository.

### R-LIM-007 · A download
Priority: **MVP** · Covers: BEYOND-030

- A **download** is the answer to a `GET` (not `HEAD`) on a route whose kind is `Download` (`formats.RouteKind`, R-ARCH-008) or of a
  file on `/files/` (not a folder listing), that writes a body: `200`, or `206` for any range. A `304`, a refusal and a `HEAD` are
  none.
- A request holds at most one parallel lease: a miss that took it at step 9a and is then served from the stored item (a range, a
  follower that fetched for itself) passes `Admit` without a second check.
- Only downloads are checked before they start against `cachedBytesPerHour` and `bytesPerDay` (R-LIM-021), slowed (R-LIM-023) and
  counted as parallel (R-LIM-024). A document is never refused by those two budgets and never slowed; its bytes count (R-LIM-006).
  A document that needs the upstream meets step 9a like every request that does (R-LIM-022).
- What a route's kind is, each format declares already; R-LIM-036 makes it the declaration of this page and holds it in a test.

### R-LIM-008 · A refused request
Priority: **MVP** · Covers: BEYOND-030

A request is a **refused request** of its subject when its answer is one of these, whatever its status code:

- a refusal of the decision point: a `403`, or a `404` that hides existence ([authorization.md](authorization.md) R-AUTHZ-016),
  for a caller with or without a credential; not the challenge (below);
- `401` because a presented credential failed (R-LIM-003);
- `404` `repository_not_found` or a project that does not exist, for a caller with a credential (the `401` R-AUTH-037 gives an
  anonymous caller for an unknown key is the challenge and not refused, below).

Not a refused request ([Q-LIM-7](#q-lim-7--a-refused-request-is-a-refusal-not-a-missing-package)):

- the challenge of a request without a credential (Q-API-290: a NuGet client's first attempt), also for a repository or project
  key that does not exist;
- a `404` of a package, a version or a file that does not exist in a repository the caller may read (a normal miss: NuGet asks
  every feed of a restore for every package, a client probes a tag);
- every other status (`400`, `409`, `413`, `422`, `429`, `5xx`).

The decision point, the authentication stage and the dispatcher's repository lookup mark the request as refused in its
`limits.Charge`; the counting reads the mark, never the status code.

Acceptance criteria:

- `dotnet restore` of a solution with 300 packages over two feeds, one of which holds none of them, counts no refused request.
- 61 requests with a valid token for a repository the token may not read count 61 refused requests of the token's subject.

### R-LIM-009 · The figures in every format
Priority: **MVP** · Covers: BEYOND-030

Every format counts by R-LIM-005 to R-LIM-008 through the core; no format counts itself. The tables say what the protocol's requests
are and how each counts, read from the format pages (2026-11-19). "Document" and "Download" are the route kinds of R-LIM-007; "Write"
is a route of the kind `Upload`, `Delete` or `Admin`, which counts as a request and under `package_write` only.

<details>
<summary>npm (<a href="formats/npm.md">formats/npm.md</a>)</summary>

| Protocol request | Counts as | Upstream request on a miss |
|---|---|---|
| `GET /<name>` package document (full or abbreviated by `Accept`) | Document | the fetch or revalidation of the document |
| `GET /<name>/<version>` version document | Document | as the package document it is cut from |
| `GET /<name>/-/<file>.tgz` tarball | Download | the tarball's fetch |
| `PUT /<name>` publish, `DELETE` unpublish, dist-tag writes | Write | — |
| `GET /-/v1/search` | Document (`Volatile`) | the forwarded search on a remote |
| `POST /-/npm/v1/security/advisories/bulk` (audit) | Document | the forward to the advisory service, when the mode forwards |
| `GET /-/whoami`, `GET /-/ping` | Document | — |

</details>

<details>
<summary>NuGet (<a href="formats/nuget.md">formats/nuget.md</a>)</summary>

| Protocol request | Counts as | Upstream request on a miss |
|---|---|---|
| `GET …/v3/index.json` service index | Document | — (rendered by Quader) |
| Registration index, page and leaf | Document | the fetch or revalidation |
| `GET …/flatcontainer/<id>/index.json` | Document | the fetch or revalidation |
| `GET …/flatcontainer/<id>/<version>/<id>.<version>.nupkg` | Download | the package's fetch |
| `GET …/<id>.nuspec` | Download | the fetch |
| Search and autocomplete | Document (`Volatile`) | the forwarded query on a remote |
| `PUT` push, symbol push, `DELETE`, relist | Write | — |

A NuGet client's first attempt without its credentials is not counted (R-LIM-005). A `Retry-After` on a NuGet route is capped at
30 s by R-NUGET-002; that cap applies to every refusal of this page.

</details>

<details>
<summary>OCI (<a href="formats/oci.md">formats/oci.md</a>)</summary>

| Protocol request | Counts as | Upstream request on a miss |
|---|---|---|
| `GET /v2/` version check, `GET /v2/_catalog` | Document, instance place | — |
| `GET /v2/_token` | Document, charged after the handler authenticated (R-LIM-003) | — |
| `GET` manifest by tag | Document (route kind `Metadata`) | the fetch or the `HEAD` revalidation of the tag |
| `GET` manifest by digest | Download (route kind `Download`) | the manifest's fetch |
| `HEAD` manifest | request only | a `HEAD` to the upstream for a tag that must be revalidated or is not held |
| `GET` blob | Download | the blob's fetch |
| `HEAD` blob | request only | a `HEAD` to the upstream on a miss |
| Tags list, referrers | Document | the fetch on a remote |
| Upload session `POST`/`PATCH`/`PUT`, manifest `PUT`, `DELETE`, mount | Write | — |

</details>

<details>
<summary>Debian (<a href="formats/debian.md">formats/debian.md</a>)</summary>

| Protocol request | Counts as | Upstream request on a miss |
|---|---|---|
| `dists/<suite>/InRelease`, `Release`, `Release.gpg` | Document | the fetch or revalidation of the release |
| `Packages`, `Sources`, `Contents` in every compression, `by-hash/…` | Document | the fetch of the index |
| `pool/…/*.deb`, `*.udeb`, source files | Download | the file's fetch |
| Upload `PUT`, delete | Write | — |

</details>

<details>
<summary>Ansible Galaxy (<a href="formats/ansible.md">formats/ansible.md</a>)</summary>

| Protocol request | Counts as | Upstream request on a miss |
|---|---|---|
| Discovery document, `v3/collections/…` lists, version documents, `docs-blob` | Document | the fetch or revalidation |
| The collection archive (`…/download/<file>.tar.gz`) | Download | the archive's fetch |
| `v1` roles: role documents | Document | the fetch |
| A role's archive (translated from the hosts the operator named) | Download | the archive's fetch from that host |
| Publish `POST`, import tasks | Write and Document | — |

</details>

<details>
<summary>Generic (<a href="formats/generic.md">formats/generic.md</a>)</summary>

| Protocol request | Counts as | Upstream request on a miss |
|---|---|---|
| `GET /files/<projectKey>/<repoKey>/<path>` | Download | — (no remote class) |
| `HEAD`, a folder listing | request only, Document | — |
| `PUT`, `DELETE` | Write | — |

The generic format has no remote class yet, so its requests count requests, cached bytes, parallel downloads and the day's volume.

</details>

What these protocols cannot carry is in [Gaps of the protocols](#gaps-of-the-protocols).

## Profiles, assignments and resolution

### R-LIM-010 · Profiles
Priority: **MVP** · Covers: BEYOND-030

A profile is a named set of every figure of R-LIM-004. An empty figure is unlimited.

- Five built-in profiles exist from the first migration with these values (the development agent's estimates; the owner changed
  none). Built-ins cannot be deleted or renamed; their figures and description can be changed by an instance administrator,
  except Unlimited, whose figures stay empty (409 `profile_immutable`).

| Profile (key) | Req/s | Burst | Req/h | Cached MB/s | Cached GB/h | Uncached MB/s | Uncached GB/h | Upstream req/h | Parallel | GB/day | Refused/h |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Unlimited (`unlimited`) | – | – | – | – | – | – | – | – | – | – | – |
| Standard (`standard`) | 200 | 1000 | 50000 | 200 | 300 | 50 | 30 | 2000 | 20 | 1000 | 500 |
| Technical (`technical`) | 400 | 2000 | 200000 | 500 | 1000 | 100 | 100 | 10000 | 50 | 5000 | 2000 |
| Restricted (`restricted`) | 50 | 200 | 5000 | 50 | 50 | 10 | 5 | 200 | 6 | 100 | 100 |
| Anonymous (`anonymous`) | 20 | 100 | 2000 | 20 | 10 | 5 | 2 | 20 | 4 | 30 | 60 |

- An instance administrator creates own profiles (key `^[a-z][a-z0-9-]{1,31}$`, unique and immutable; name 1 to 64 characters,
  unique; description at most 1024) and deletes one that no assignment and no project maximum names (409 `profile_in_use`
  otherwise).
- A change of a profile takes effect on every replica within the propagation bound of the change area `limits`
  ([architecture.md](architecture.md) R-ARCH-018); counters keep running.

Data model (migration of work package 2):

```sql
CREATE TABLE limit_profile (
  id                          uuid PRIMARY KEY,
  key                         text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9-]{1,31}$'),
  name                        text NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 64),
  description                 text NOT NULL DEFAULT '' CHECK (length(description) <= 1024),
  builtin                     boolean NOT NULL DEFAULT false,
  requests_per_second         bigint NULL CHECK (requests_per_second > 0),
  requests_burst              bigint NULL CHECK (requests_burst > 0),
  requests_per_hour           bigint NULL CHECK (requests_per_hour > 0),
  cached_bytes_per_second     bigint NULL CHECK (cached_bytes_per_second > 0),
  cached_bytes_per_hour       bigint NULL CHECK (cached_bytes_per_hour > 0),
  uncached_bytes_per_second   bigint NULL CHECK (uncached_bytes_per_second > 0),
  uncached_bytes_per_hour     bigint NULL CHECK (uncached_bytes_per_hour > 0),
  upstream_requests_per_hour  bigint NULL CHECK (upstream_requests_per_hour > 0),
  parallel_downloads          bigint NULL CHECK (parallel_downloads > 0),
  bytes_per_day               bigint NULL CHECK (bytes_per_day > 0),
  refused_per_hour            bigint NULL CHECK (refused_per_hour > 0),
  version                     bigint NOT NULL DEFAULT 1,
  created_at                  timestamptz NOT NULL DEFAULT now(),
  created_by                  uuid NULL REFERENCES principal (id) ON DELETE SET NULL,
  updated_at                  timestamptz NOT NULL DEFAULT now(),
  updated_by                  uuid NULL REFERENCES principal (id) ON DELETE SET NULL,
  CHECK (requests_burst IS NULL OR (requests_per_second IS NOT NULL AND requests_burst >= requests_per_second))
);
```

### R-LIM-011 · Assignments
Priority: **MVP** · Covers: BEYOND-030

An assignment (a rule) gives a profile to a "who" at a place, with optional overrides of single figures.

- **Place**: the instance, a project, or a repository of any class.
- **Who**, exactly one of:

| `who.kind` | Names | Matches |
|---|---|---|
| `user` | one user | requests of that user's subject |
| `technicalToken` | one technical token (R-AUTH-045) | requests with any secret of that token |
| `ciProject` | one CI issuer and project ID | requests with CI tokens of that project |
| `address` | an IPv4 prefix `/8` to `/32` or an IPv6 prefix `/32` to `/64` | anonymous requests whose subject lies in it |
| `group` | one group | users and technical tokens of instance service accounts that are members (R-AUTH-018); CI tokens have no groups here |
| `every` | a kind: `user`, `technicalToken`, `ci`, `anonymous` or `administrator` | every caller of that kind |

- "Anonymous" exists here as the `address` rules and the kind `anonymous` only (owner, decision 11): no group, no policy and no
  role binding names the anonymous principal ([identity-and-authentication.md](identity-and-authentication.md) R-AUTH-037 stays).
  A `group` rule never matches an anonymous request.
- **Overrides** set single figures of the profile: a number, or `null` for unlimited; a figure that is not named keeps the
  profile's value.
- At one place there is at most one rule per "who" (409 `assignment_exists`); two `address` rules with the same prefix are the same
  "who".
- `every` `administrator` exists at the instance only: administrators are decided there (R-LIM-012), so a project or repository
  rule for it answers 422 `invalid_who`.
- A rule names a profile by its key. A note of at most 512 characters says why the rule exists.
- An assignment's effective figures are the profile's with the overrides, then clamped by R-LIM-016 when the rule is capped.

Data model:

```sql
CREATE TABLE limit_assignment (
  id             uuid PRIMARY KEY,
  place_kind     text NOT NULL CHECK (place_kind IN ('instance', 'project', 'repository')),
  project_id     uuid NULL REFERENCES project (id) ON DELETE CASCADE,
  repository_id  bigint NULL REFERENCES repositories (id) ON DELETE CASCADE,
  who_kind       text NOT NULL CHECK (who_kind IN ('user', 'technical_token', 'ci_project', 'address', 'group', 'every')),
  who_ref        text NOT NULL,   -- principal ID of a user or group, technical token ID, '<issuer ID>:<project ID>',
                                  -- canonical prefix, or one of 'user', 'technical_token', 'ci', 'anonymous', 'administrator'
  profile_id     uuid NOT NULL REFERENCES limit_profile (id) ON DELETE RESTRICT,
  overrides      jsonb NOT NULL DEFAULT '{}',   -- figure name -> bigint or null; absent = the profile's value
  capped         boolean NOT NULL,              -- written by a caller without limits:write (R-LIM-016)
  builtin        boolean NOT NULL DEFAULT false,
  note           text NOT NULL DEFAULT '' CHECK (length(note) <= 512),
  version        bigint NOT NULL DEFAULT 1,
  created_at     timestamptz NOT NULL DEFAULT now(),
  created_by     uuid NULL REFERENCES principal (id) ON DELETE SET NULL,
  updated_at     timestamptz NOT NULL DEFAULT now(),
  updated_by     uuid NULL REFERENCES principal (id) ON DELETE SET NULL,
  CHECK ((place_kind = 'instance' AND project_id IS NULL AND repository_id IS NULL)
      OR (place_kind = 'project' AND project_id IS NOT NULL AND repository_id IS NULL)
      OR (place_kind = 'repository' AND project_id IS NULL AND repository_id IS NOT NULL))
);
CREATE UNIQUE INDEX limit_assignment_who ON limit_assignment
  (place_kind, project_id, repository_id, who_kind, who_ref) NULLS NOT DISTINCT;
```

`who_ref` carries no foreign key: a user, a group or a technical token is deleted by work that knows nothing of limits, and a rule
whose "who" is gone matches nothing. The daily task `limits-cleanup` (R-LIM-028) deletes such rules; until then the API shows them
with `who.deleted: true`.

### R-LIM-012 · The built-in rule set
Priority: **MVP** · Covers: BEYOND-030

Without any other setting (owner, decision 9) the instance place holds these rules, created by the migration with `builtin: true`.
Their profile and overrides can be changed by an instance administrator; they cannot be deleted (409 `assignment_builtin`).

| Who | Profile |
|---|---|
| `every` `administrator` | Unlimited |
| `every` `user` | Standard |
| `every` `technicalToken` | Technical |
| `every` `ci` | Technical |
| `every` `anonymous` | Anonymous |

- **An administrator** is a subject whose principal holds the system role `admin` now (directly, through a group, or through
  `authz.role_bindings`), the break-glass administrator included, and a technical token of an instance service account that holds
  the role ([Q-LIM-18](#q-lim-18--an-administrator-is-a-principal-that-holds-the-role-admin-and-is-decided-at-the-instance-only)).
  It is decided by the principal, not by the presented token's scope: a personal token of an administrator without `admin: true`
  is still charged to the administrator's subject, whose kind default is Unlimited. A `security-admin`, an `auditor` or a project
  Owner is no administrator here.
- An administrator's requests are decided at the instance place only (R-LIM-013); a rule of a project or a repository never applies
  to one.
- `api.rate_limits.exempt_admins` of R-API-016 stays for the management API's classes and plays no part here.

### R-LIM-013 · Resolution: which rule applies
Priority: **MVP** · Covers: BEYOND-030

For a request with subject S on repository R of project P (owner, decision 16):

1. If S is an administrator (R-LIM-012), go to step 3 with the instance place only.
2. The places, in order: R, then P, then the instance. A request that names no repository (R-LIM-005) has the instance place only.
3. At each place in that order:
   1. **The single caller**: the rule whose `who` is S itself (`user`, `technicalToken`, `ciProject`); for an anonymous S, the
      `address` rule with the longest prefix that contains S. If one exists, it applies; stop.
   2. **The groups**: the `group` rules of the place whose group S belongs to (a user's groups as the authentication stage resolved
      them, R-AUTHZ-002; a technical token's account's groups). If one or more exist, the applying figures are, per figure, the
      most generous value among their effective figures, where empty (unlimited) is the most generous (owner, decision 2); stop.
      The rule the explanation names is each group rule that contributed a figure.
   3. **The kind**: the `every` rule of the place for `administrator` if S is one, else for S's kind. If one exists, it applies;
      stop.
4. The instance place always holds the built-in `every` rules (R-LIM-012), so resolution ends at the latest there.

The place at which resolution stopped is the request's **place**; the budget key is S at that place (R-LIM-014). Rules of a further
place are not looked at: a repository rule replaces the instance's for that repository, also with higher figures (owner,
decision 3), within R-LIM-016.

Resolution runs in memory on a compiled index of the rules, the profiles, the project maxima and the settings (`limits.Index`),
rebuilt on a change of the area `limits` (R-ARCH-018); it reads no database. The authentication stage has resolved the groups.

Worked examples (instance rules: `group` `web-devs` → Standard, `group` `contractors` → Restricted, the built-ins; project `web`
has no rules):

- **A user in two groups and a group rule at a repository.** Alice is in `web-devs` and `contractors`. Repository `web/npm-remote`
  has `group` `contractors` → Restricted with the override `uncachedBytesPerHour: 20000000000`.
  - A request to `web/npm-remote`: at the repository, no rule names Alice; one group rule matches (`contractors`): it applies with
    Restricted and 20 GB of uncached bytes per hour. Budget key: Alice at `web/npm-remote`.
  - A request to `web/npm-local` (no rules): repository and project hold none; at the instance, no rule names Alice; two group
    rules match: per figure the larger of Standard and Restricted, which is Standard throughout. Budget key: Alice at the instance.
- **A technical token of a repository.** Repository `web/npm-local` has an account `sa:web/npm-local/ci` with the technical token
  `release` (R-AUTH-046) and the rule `technicalToken` `release` → Technical with `parallelDownloads: 100`. A request with any
  secret of `release` to `web/npm-local` stops at the repository's single-caller rule. A request of the same token to
  `web/npm-remote`, which it may not read, is decided at the instance (`every` `technicalToken` → Technical) and counted as a
  refused request there.
- **An anonymous address inside a range.** The instance has `address` `198.51.100.0/24` → Restricted; repository
  `platform/dockerhub` has `address` `198.51.100.0/22` → Anonymous with `upstreamRequestsPerHour: 5`. A request from
  `198.51.100.23` to `platform/dockerhub` stops at the repository's `/22` rule; the same address on `platform/npm` (no rules)
  stops at the instance's `/24` rule; `203.0.113.9` on `platform/npm` gets `every` `anonymous` → Anonymous.
- **A CI identity.** The instance has `ciProject` `gitlab-internal:4711` → Technical with `bytesPerDay: 50000000000`. Every job of
  GitLab project 4711 stops at that rule wherever no repository or project rule names the project or `every` `ci`; a job of project
  4712 gets `every` `ci` → Technical.
- **An administrator.** Ada holds `admin`; repository `web/npm-remote` has `every` `user` → Restricted. Ada's requests there are
  decided at the instance (step 1): `every` `administrator` → Unlimited.

Acceptance criteria:

- The examples above, as unit tests of `limits.Index.Resolve`, give the place, the rule and the effective figures stated.
- A user removed from `contractors` is decided by the new group set at the next request after the principal cache learned it
  (R-AUTH-023), without a change of a rule.

### R-LIM-014 · The budget key, and the sum of a subject
Priority: **MVP** · Covers: BEYOND-030

- The **budget key** of a request is its subject together with its place: `user:<id>@instance`, `user:<id>@project:<project ID>`,
  `user:<id>@repository:<repository ID>`. Every counter of R-LIM-017 counts one budget key (owner, decision 15): consumption under
  a repository's own rule counts against that rule only, and the subject's instance budget stays for every other repository.
- A project rule's budget key is one for the whole project: a user under a project rule shares one budget across the project's
  repositories that have no rule of their own.
- **The sum** of a subject is the sum of its consumption over all its budget keys in the period and scope asked (the instance, a
  project, a repository). The overview shows the sum per subject and, in the detail, the consumption per budget key (R-LIM-034).
- A ceiling (R-LIM-015) has keys of its own and is not part of any subject's sum.

### R-LIM-015 · The ceilings of a remote repository
Priority: **MVP** · Covers: BEYOND-030

A remote repository has a ceiling per class of callers (owner, decision 7 and the model): **all anonymous callers together** and
**all signed-in callers together** (users, technical tokens, CI identities; administrators included, since the upstream's quota
belongs to the installation).

- A ceiling has two figures, `uncachedBytesPerHour` and `upstreamRequestsPerHour`
  ([Q-LIM-16](#q-lim-16--a-ceiling-is-a-remote-repositorys-for-the-two-upstream-figures-and-none-is-set-by-default)); each is
  empty (unlimited) by default. Local and virtual repositories have none (422 `not_applicable`).
- A request that needs the upstream must fit its subject's budget **and** the ceiling of its class at the remote that fetches
  (through a virtual repository: the remote member). The ceiling is checked at step 9a of R-REPO-025 with the subject's upstream
  figures, charged by the same exchange, and answered as they are (R-LIM-022). A ceiling slows nothing.
- System work is in neither class and is not charged to a ceiling.
- The owner's example of a public remote, all signed-in callers 50 GB and 2,000 upstream requests per hour, all anonymous callers
  10 GB and 100, is the example of the API (R-LIM-032), not a default.

Data model:

```sql
CREATE TABLE limit_ceiling (
  repository_id               bigint NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
  class                       text NOT NULL CHECK (class IN ('anonymous', 'signed_in')),
  uncached_bytes_per_hour     bigint NULL CHECK (uncached_bytes_per_hour > 0),
  upstream_requests_per_hour  bigint NULL CHECK (upstream_requests_per_hour > 0),
  version                     bigint NOT NULL DEFAULT 1,
  updated_at                  timestamptz NOT NULL DEFAULT now(),
  updated_by                  uuid NULL REFERENCES principal (id) ON DELETE SET NULL,
  PRIMARY KEY (repository_id, class)
);
```

### R-LIM-016 · Who may set what, and the project's maximum
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 14: only the instance administrator raises above the instance.)

| Action | Who |
|---|---|
| Create, change, delete profiles; change the built-in rules | `limits:write` (the role `admin`, R-AUTHZ-040) |
| Rules at the instance place; the mode and the block time (R-LIM-026, R-LIM-025) | `limits:write` |
| A project's maximum | `limits:write` |
| Rules at a project and at its repositories | `limits:write`, uncapped; an Owner of the project (R-AUTHZ-026), capped |
| Ceilings of the project's remote repositories | `limits:write`; `repositories:write-config`; an Owner of the project. A ceiling only narrows and is never capped |
| Lift a block | `limits:write` every block; an Owner of the project the blocks whose place is the project or one of its repositories |

- **The project's maximum** is a profile with optional overrides that an instance administrator gives a project; a project has none
  until one is given.
- **A capped rule** is a project or repository rule last written by a caller without `limits:write` (`capped: true`); a write by a
  holder of `limits:write` makes it uncapped. For a capped rule with "who" W, per figure f:

  `cap(f) = max(instance(W, f), maximum(f))`, and the rule's effective `f` is `min(rule(f), cap(f))`,

  where empty is unlimited in `max` and `min`, `maximum(f)` is the project's maximum (absent: no maximum, so `cap(f) =
  instance(W, f)`), and `instance(W, f)` is what the instance place gives W:
  - a single user, technical token or CI project: the instance resolution of that subject (R-LIM-013 step 3 at the instance only),
    with the user's current groups;
  - a group: the instance rule of that group if one exists, else the instance's `every` `user` rule;
  - an `address` prefix: the instance's `address` rule with the longest prefix containing the whole prefix, else `every`
    `anonymous`;
  - `every` kind: the instance's `every` rule of that kind.
- **At write time** a capped rule whose figures exceed the cap answers 422 `limit_above_maximum` with the figures, their requested
  values and their caps in `errors[]`, and is not stored.
- **At evaluation time** the clamp is applied again with the current instance rules and maximum
  ([Q-LIM-17](#q-lim-17--an-owners-rule-is-clamped-per-figure-when-it-is-evaluated-not-rewritten)). When an instance administrator
  lowers a project's maximum or an instance rule, every capped rule that exceeds the new cap is clamped from the next request on;
  no stored rule changes. The API returns the clamped figures in `clamped` (R-LIM-032) and the UI marks them.
- A project Owner narrows freely: a rule below the cap is always accepted.

Data model:

```sql
CREATE TABLE limit_project_maximum (
  project_id  uuid PRIMARY KEY REFERENCES project (id) ON DELETE CASCADE,
  profile_id  uuid NOT NULL REFERENCES limit_profile (id) ON DELETE RESTRICT,
  overrides   jsonb NOT NULL DEFAULT '{}',
  version     bigint NOT NULL DEFAULT 1,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid NULL REFERENCES principal (id) ON DELETE SET NULL
);
```

Acceptance criteria:

- Project `web` has no maximum; its Owner sets `group` `web-devs` → Unlimited at the project: 422 `limit_above_maximum` naming every
  figure of Standard. With the maximum Technical, the same rule with the profile Technical is stored; Unlimited is refused for every
  figure.
- The maximum of `web` is lowered from Technical to Standard: a stored capped rule Technical of `web` applies Standard's figures at
  the next request, and `GET` of the rule shows them in `clamped`.

## Counting and enforcement

### R-LIM-017 · Where the counters live, and what a request costs
Priority: **MVP** · Covers: BEYOND-030

The figures are enforced from counters in Valkey; the usage store in PostgreSQL records consumption for the overview and is never
read on a request ([Q-LIM-10](#q-lim-10--the-enforcing-counters-live-in-valkey-the-usage-store-in-postgresql)).

| Figure | Where | Valkey commands | Charged when |
|---|---|---|---|
| Requests per second and per hour of a budget key | Valkey, sliding window (R-ARCH-034 arithmetic) | `INCR`, `EXPIRE NX`, `GET` | before the decision, at stage 7 |
| Cached bytes per hour, bytes per day of a budget key | Valkey, sliding window in bytes | `GET` at the check; `INCRBY` at the flush | at the flush after the bytes were written |
| Uncached bytes per hour of a budget key and of a ceiling | Valkey, sliding window in bytes | `GET` at step 9a; `INCRBY` at the flush | at the flush after the bytes were read |
| Upstream requests per hour of a budget key and of a ceiling | Valkey, sliding window | `INCR`, `EXPIRE NX`, `GET` | before the decision, at step 9a |
| Parallel downloads of a budget key | Valkey, a sorted set of leases | `ZADD`, `ZREMRANGEBYSCORE`, `ZCARD`, `ZREM` | at the start and the end of a download |
| Refused requests per hour of a budget key, and its block | Valkey, sliding window and a marker | `INCRBY` at the flush; `SET NX EX`, `GET`, `DELIFEQ` | at the flush; the block at the flush that passes the limit |
| Bytes per second (pacing) | the replica's memory, with the cluster's count of the key's downloads | `ZCARD` in the flush | continuously |

**Keys.** The family `lim` (`quader:<ks>:lim:v1:<figure>:<b>:<window index>`, `…:lim:v1:par:<b>`, `…:lim:v1:blk:<b>`), with `<b>`
the hashed budget key or ceiling key (R-ARCH-030: keys reach Valkey only as hashes); a counter expires two windows after it opened,
so the family's longest time to live is 48 hours.

**What a request costs** (each a pipelined round trip bounded by `valkey.command_timeout`):

| Request | Round trips on its path |
|---|---|
| Every request on a protocol path | 1 at stage 7: the request windows of its budget key, the block marker, and in Audit mode the counter of its R-API-016 class, in one `MULTI` |
| A download from the cache | +1 in `edge.ServeItem` before the status line: the hour and day byte windows, the parallel lease |
| A miss (document or artifact) that would contact the upstream | +1 at step 9a: upstream requests and uncached bytes of the budget key and of the ceiling, and for an artifact `GET` the parallel lease |
| The end of a download, the bytes, refused requests | none on the request: the replica's buffer carries them to the next flush |

Per replica, independent of the request rate: one round trip per `limits.flush_interval` (1 s) that carries every byte and refused
charge of the interval as `INCRBY`, the `ZREM` of the downloads that ended, the lease renewals of downloads older than 20 s, the
`ZCARD` of the keys being paced, and the block markers that a flush found due. PostgreSQL: no statement per request; one batch per
replica every `limits.usage.flush_interval` (10 s, R-LIM-028).

**The changes to the cluster package** (`internal/cluster`):

- a limiter whose rate and burst are given per call (the subject's figures differ per budget key), with the arithmetic of
  R-ARCH-034;
- windows up to 24 hours; `maxLimiterWindow` rises from 12 to 24 hours for the family `lim`, whose keys live 48 hours;
- byte charges with `INCRBY`, no unit bound (the 100 units of `TakeNAt` stay for the limiters that use it);
- leases in a sorted set.

The least-privilege command list of [architecture.md](architecture.md) R-ARCH-031 gains `INCRBY`, `ZADD`, `ZREM`,
`ZREMRANGEBYSCORE` and `ZCARD`; the example ACL rule, the development profile's Valkey (`deploy/dev/valkey.yaml`) and the integration
stack's Valkey user grant them. No script and no `DEL` is used.

Acceptance criteria:

- A cached download costs two round trips to Valkey and no PostgreSQL statement on its path, measured by
  `quader_valkey_command_duration_seconds_count` and the statement counter of the download budget (R-ARCH-027) in the integration set
  `limits`.
- A replica with 1,000 downloads in progress sends one flush round trip per second.

### R-LIM-018 · Windows and their precision across replicas
Priority: **MVP** · Covers: BEYOND-030

- **Windows are rolling** ([Q-LIM-11](#q-lim-11--windows-roll-by-the-two-window-estimate-for-the-hour-and-the-day-too)), estimated
  as R-ARCH-034 does for every window: `previous × (1 − elapsed ÷ W) + current ≤ limit`, with `W` one second times the burst ratio
  for requests, 3,600 s for an hour and 86,400 s for a day, window boundaries at multiples of `W` since the Unix epoch.
- **What is promised**, across any number of replicas while Valkey is reachable:
  - Requests and upstream requests: in each fixed window at most the limit; in any rolling interval of length `W` at most twice the
    limit (the bound of the two-window estimate).
  - Bytes per hour and per day: in each fixed window at most the limit, plus the bytes that the subject's downloads admitted before
    the limit was reached still transfer (a download is never cut, owner decision 4; at most `parallelDownloads` downloads), plus one
    `limits.flush_interval` (1 s) of the subject's transfer rate that was not yet charged when a check read the counter.
  - Ceilings: as bytes per hour and upstream requests, over all subjects of the class.
  - Parallel downloads: at most the limit across the cluster, except for the downloads of a replica that crashed, whose leases
    keep their place for at most `limits.parallel.lease` (60 s).
  - Bytes per second: R-LIM-023.
- **What is not promised**: exact accounting of a window in which Valkey failed over or was unreachable (R-LIM-019), and
  any figure of a request that Quader never saw (a Bastion's cache hit).

### R-LIM-019 · Valkey or PostgreSQL unreachable
Priority: **MVP** · Covers: BEYOND-030

Every figure fails open to the replica's share and never refuses a request because Valkey failed
([Q-LIM-12](#q-lim-12--every-figure-fails-open-to-the-replicas-share-while-valkey-is-unreachable)):

| Figure | While Valkey is unreachable on a replica |
|---|---|
| Requests, upstream requests, bytes per hour and per day, refused requests | a local window per budget key and ceiling with the limit ÷ n, n the live replicas of the role in the replica's pool (R-ARCH-034); local windows start empty |
| Parallel downloads | a local count per budget key with ⌈limit ÷ n⌉ |
| Block | only blocks the replica set itself in its local state are known; a block set before the outage is not seen |
| Pacing | the replica paces at the figure ÷ n |
| The check's wait | at most `valkey.command_timeout`, then the local state, as every limiter |

`quader_ratelimit_fallback{limiter="limits"}` is 1 while the replica uses local state. When Valkey answers again, the replica
returns to the cluster's counters at the next check; local counts are not merged (a subject may get up to one more window of its
budget per outage, H-1005).

While PostgreSQL is unreachable, enforcement goes on with the compiled index and the counters; writes of rules, profiles and
settings fail as every write does; the usage buffer keeps its counts for the next flush and, when it is full, merges new keys into
overflow rows (R-LIM-028).

### R-LIM-020 · The answer at a limit
Priority: **MVP** · Covers: BEYOND-030

Every refusal of this page is an error of the kind `errs.RateLimited` ([internal/errs](../../internal/errs)) with status 429, `Retry-After`, and one of these
codes:

| Code | Figure | `Retry-After` |
|---|---|---|
| `rate_limited` | `requestsPerSecond`/`requestsBurst`, `requestsPerHour` | the limiter's wait (R-ARCH-034), at least 1 s |
| `download_limit_reached` | `cachedBytesPerHour`, `bytesPerDay` | the time until the window's estimate leaves room for one more byte, at least 1 s, at most the window |
| `upstream_limit_reached` | `uncachedBytesPerHour`, `upstreamRequestsPerHour`, a ceiling | as above |
| `too_many_parallel_downloads` | `parallelDownloads` | 1 s |
| `caller_blocked` | a block (R-LIM-025) | the block's remaining time |

- **Body.** On the management API's shape (`/files/`, generic): `application/problem+json` (R-API-009) with `code`, a `detail` in
  plain words that names the figure, the limit and the wait ("The download limit of 300 GB per hour is used up. Try again in 14
  minutes."), and the extension members `figure` (the API name), `limit` (the value), `windowSeconds`, `place` (`instance`,
  `project`, `repository`) and `retryAfterSeconds`. For a signed-in caller `detail` ends with "Your limits: <base URL>/ui/profile/usage".
  A ceiling's refusal names the class ("all anonymous callers of this repository").
- **Every protocol** renders the same error through its format's `WriteError` (R-ARCH-023, Q-ARCH-214): npm the JSON `error` and
  the status; NuGet one text line `<code>: <detail>` and its `Retry-After` capped at 30 s (R-NUGET-002); OCI `TOOMANYREQUESTS` with
  the `detail` as `message` ([formats/oci.md](formats/oci.md) R-OCI-008); Debian and Ansible Galaxy their problem shapes. A refusal
  at stage 7 reaches the format's shape through `httpserver.Problem`, a refusal in a handler through the format's own rendering.
- **RateLimit fields** ([draft-ietf-httpapi-ratelimit-headers](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/)
  draft-11, as R-API-016 uses it), in `enforce` only
  ([Q-LIM-23](#q-lim-23--the-ratelimit-fields-name-one-policy-per-figure-the-request-figures-always-the-others-on-their-refusal)):
  every answer on a protocol path that a request figure charged carries `RateLimit-Policy: "requests";q=<burst>;w=<W>,
  "requests-hour";q=<n>;w=3600` (for the figures that are set) and `RateLimit` with `r` and `t` for each; a refusal by another
  figure adds that figure's policy and remaining, with `qu="content-bytes"` for byte figures and `qu="concurrent-requests"` for
  `parallelDownloads` (`"cached-hour"`, `"uncached-hour"`, `"upstream-hour"`, `"day"`, `"parallel"`, `"refused-hour"`). The endpoint's
  class and, in `audit`, the R-API-016 class add their own members as today.
- **Request log**: the fields of R-LIM-030; the `429` names the figure in `limiter` (`limits.<figure>`) and the mode in
  `limit_mode`.

Acceptance criteria:

- With `cachedBytesPerHour` used up, `npm install` reports `429 Too Many Requests` and the problem's `detail` is in the npm debug
  log; `docker pull` reports `toomanyrequests` with the detail; `curl` of a raw file shows the problem with `figure:
  "cachedBytesPerHour"`.
- In `audit` no answer carries a `RateLimit` member named after a figure of this page.

### R-LIM-021 · The check before a download starts
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 4: "429 before the download starts, never in the middle of one.")

- **From the cache.** `edge.ServeItem` asks `ServeOptions.Admit` after the conditionals and the range were evaluated and before
  `open` and the status line, for a download (R-LIM-007): the budget key's `cachedBytesPerHour` and `bytesPerDay` windows, and a
  parallel lease (R-LIM-024). A `304`, a `HEAD` and a refused precondition never reach it. A refusal is returned like the error of
  `open` (nothing was written) and rendered by the caller.
- **From the upstream.** Step 9a of R-REPO-025 checks the uncached figures (R-LIM-022) and, for an artifact `GET`, takes the
  parallel lease.
- **The rule**, whether the size is known or not
  ([Q-LIM-8](#q-lim-8--a-download-is-admitted-while-its-budget-has-room-whatever-its-size)): a download is admitted while the
  window's estimate is below the limit; its size is not reserved. The download that crosses the limit completes and is charged; the
  next one is refused. A known size (a cached item, an announced OCI descriptor size) is not used.
- A download, once its status line is out, is never refused or cut by a figure; pacing still applies.

Acceptance criteria:

- `cachedBytesPerHour` 100 MB, an estimate of 99 MB: a 50 MB download is admitted and completes; the next answers 429
  `download_limit_reached` before any byte.

### R-LIM-022 · What is cached is still served
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 4: "Upstream budget used up: what is cached is still served; only what would need the upstream is refused.")

R-REPO-025 gains a step between 9 and 10:

> 9a. The request would contact the upstream (an artifact miss, a document that is absent, expired or invalidated, a `HEAD` the
> engine forwards, a forwarded search): the caller's budget key must have room in `upstreamRequestsPerHour` and
> `uncachedBytesPerHour`, and the remote's ceiling of the caller's class room in both of its figures. The upstream request is
> counted here, for the budget key and the ceiling, before the decision. Without room: an existing expired document is served
> stale within `cache.maxStaleSeconds` (R-REPO-029) without contacting the upstream; otherwise 429 `upstream_limit_reached`. No
> negative entry is written, the circuit is not touched.

- Cached artifacts and fresh documents are served from step 4 before 9a is reached, so they never meet the upstream figures.
- Stale serving follows `cache.maxStaleSeconds` as an upstream failure does
  ([Q-LIM-9](#q-lim-9--an-expired-document-is-served-stale-within-maxstaleseconds-when-the-upstream-budget-is-used-up)): within it
  (or with `0`, unbounded) the stale document is served with `Cache-Status: quader; hit; detail=stale-budget`; beyond it the answer
  is 429.
- A follower of a flight does not pass 9a: it pays cached bytes only (R-LIM-006). A request that becomes the leader of its own fetch
  after waiting passes 9a then.
- Background requests (`proxy.PriorityBackground`) skip 9a; W-25's background download passes it for its triggering caller
  (R-LIM-037).
- The hook is `(*Engine).mayFetch` beside `mayFill` in [internal/proxy/engine.go](../../internal/proxy/engine.go), called from
  `Artifact` after `mayFill` and from `beforeFetch` in [metadata.go](../../internal/proxy/metadata.go).

Acceptance criteria:

- An anonymous caller with `upstreamRequestsPerHour` used up gets every cached tag of a public OCI remote, a stale tag document
  within `maxStaleSeconds`, and 429 `upstream_limit_reached` for an uncached blob; the upstream stub records no request.
- With the anonymous ceiling used up, a second anonymous address with an untouched budget gets the same 429; a signed-in user is
  served from the upstream.

### R-LIM-023 · Slowing a download down
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 4: "Bytes per second: slowed down, no error.")

- **Cached bytes per second** pace the body of a download from the cache: the counting writer around the response (R-LIM-017)
  waits before each write for the budget key's tokens.
- **Uncached bytes per second** pace the read of the upstream body in a fill the caller leads (`(*fetcher).artifact`): the fill,
  and with it the leader's stream and every follower that waits, proceeds at the leader's rate. A detached fill keeps the leader's
  rate.
- **The share of a replica** ([Q-LIM-13](#q-lim-13--a-replica-paces-a-budget-key-at-its-share-of-the-keys-downloads-with-a-floor-per-download)):
  each replica keeps a token bucket per budget key and figure with the rate `figure × local ÷ max(local, cluster)`, where `local`
  is the key's downloads in progress on the replica and `cluster` the key's parallel leases (`ZCARD`, refreshed in each flush),
  and a burst of one second of that rate. The downloads of the key on the replica take turns on the bucket in chunks of 16 KiB.
- **The floor**: every download proceeds at least at `limits.pacing.min_bytes_per_second` (65,536 bytes per second by default, an
  estimate), whatever its share, and no write waits longer than one second, so no idle timeout of a client fires. A subject may
  therefore exceed its figure by at most its downloads in progress times the floor.
- A client with a timeout on the whole request (npm's `fetch-timeout`) can still fail a large file at a low figure; that is a
  consequence of the figure's value and is shown in the profile editor's help (R-UI-178).
- In `audit` nothing is slowed; a download that would have waited is recorded once as `would_limit` for its figure (R-LIM-026).
- Documents and writes are never paced.

Acceptance criteria:

- `cachedBytesPerSecond` 10 MB, one 100 MB download: it takes between 9 and 11 s. Two parallel downloads of the subject on two
  replicas take between 18 and 22 s together.
- `cachedBytesPerSecond` 1,000: a 1 MB download completes at the floor in about 16 s, and no gap between two writes exceeds one
  second.

### R-LIM-024 · Parallel downloads
Priority: **MVP** · Covers: BEYOND-030

- A download takes a lease in the budget key's sorted set: member `<replica ID>:<download ID>`, score the lease's end
  (now + `limits.parallel.lease`, 60 s), in one `MULTI` with `ZREMRANGEBYSCORE <key> -inf <now>` and `ZCARD`. The lease is admitted
  when the count including it is at most `parallelDownloads`; otherwise it is removed again (`ZREM`) and the download waits.
- **Waiting** ([Q-LIM-14](#q-lim-14--a-further-download-waits-up-to-10-seconds-then-429)): the download asks again once per second,
  up to `limits.parallel.wait` (10 s), holding its request slot of step 4; then 429 `too_many_parallel_downloads`. A client that
  leaves stops waiting.
- The replica renews the leases of its downloads older than 20 s in the flush and removes ended ones there.
- **A crashed replica** leaves its leases, which expire at their score: the subject has up to 60 s fewer slots.
- In `audit` a lease above the limit is kept, recorded as `would_limit`, and the download goes on at once.

### R-LIM-025 · The block after refused requests
Priority: **MVP** · Covers: BEYOND-030

- When the flush finds a budget key's refused requests per hour above `refusedPerHour`, it sets the block marker (`SET <blk key>
  <block ID> NX EX <block seconds>`); the block time is the instance setting `blockSeconds` (900 s by default, an estimate;
  60 to 86,400). While the marker exists, stage 7 answers every request charged to that budget key with 429 `caller_blocked` and a
  `Retry-After` of the remaining time, before the handler.
- **Who is blocked** ([Q-LIM-15](#q-lim-15--a-block-holds-one-budget-key-for-15-minutes-and-only-requests-charged-to-it)): the
  budget key only. A block of an address blocks the anonymous requests from that address (a failed credential is answered 401 before
  stage 7 anyway, R-LIM-003), never a request whose credential is valid, which is charged to its own subject. A block of a user at a repository does not reach
  the user's other places.
- **How it ends**: when the marker expires; or when a holder of `limits:write`, or an Owner of the project the block's place
  belongs to, lifts it (`DELETE /api/v1/limits/blocks/{id}`, R-LIM-033), which removes the marker (`DELIFEQ` with the block's ID) and
  sets the budget key's refused counters of the current and the previous window to 0 (`SET … KEEPTTL`), so the next refused request
  does not block again at once.
- **The record**: every block is written to `limit_block` (asynchronously, never failing a request) and audited
  (`limits.block.started`, `limits.block.lifted`, R-LIM-030).
- **In `audit`** no marker is set; the flush records `would_limit` for `refusedPerHour` once per budget key and window.
- **What it cannot do**: an attacker who sends wrong tokens from a victim's NAT address blocks that address's anonymous requests
  (anonymous colleagues of a public repository) for the block time, repeatably; it cannot block a colleague who presents a valid
  credential, whose requests are charged to the colleague's subject. The remaining gap is H-1010 on
  [security/usage-and-limits.md](../security/usage-and-limits.md). The failed-credential throttle of R-AUTH-038 is a separate
  mechanism and stays as it is.

Data model:

```sql
CREATE TABLE limit_block (
  id               uuid PRIMARY KEY,
  subject_kind     text NOT NULL,
  subject_ref      text NOT NULL,
  place            text NOT NULL,              -- 'instance' | 'project:<uuid>' | 'repository:<id>'
  started_at       timestamptz NOT NULL,
  until            timestamptz NOT NULL,
  refused_count    bigint NOT NULL,
  refused_limit    bigint NOT NULL,
  lifted_at        timestamptz NULL,
  lifted_by        uuid NULL REFERENCES principal (id) ON DELETE SET NULL
);
CREATE INDEX limit_block_until ON limit_block (until);
```

Acceptance criteria:

- An anonymous address sends 61 requests with revoked tokens within an hour on a profile with `refusedPerHour` 60: within two
  seconds of the 61st it is blocked; a request with a valid token from the same address is admitted; an anonymous request answers
  429 `caller_blocked` with a `Retry-After` of at most 900.
- An Owner of the project lifts a block at one of its repositories (204); a block at the instance answers that Owner 403.

### R-LIM-026 · Audit and Enforce
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 17.)

- One instance setting, `mode`: `audit` (the default) or `enforce`, changed by a holder of `limits:write` through
  `PATCH /api/v1/limits/settings` with `If-Match` and recent authentication (R-API-007), and in the UI (R-UI-182). Every change is
  audited (`limits.mode.changed`). It reaches every replica within the bound of the change area `limits`.
- **In both modes** every figure is counted, every decision is computed, and the usage store records consumption.
- **In `audit`** no decision of this page refuses or slows a request. Every decision that would have refused or slowed is recorded
  as `would_limit` with its figure: in the usage store (R-LIM-027), in `quader_limits_decisions_total{outcome="would_limit"}`, and in
  the request log (`limits_outcome: would_limit:<figure>`). A block is not set. The new `RateLimit` members are not sent.
- **In `enforce`** the decisions apply as R-LIM-020 to R-LIM-025 say.
- **What protects an installation in `audit`**
  ([Q-LIM-4](#q-lim-4--in-audit-mode-the-package-classes-of-r-api-016-stay-in-force-as-the-safety-net), for the owner's review): the
  classes `package_read` and `package_anonymous` of R-API-016, now on every protocol path including `/v2/` and keyed by the subject
  of R-LIM-001, refuse as today while the mode is `audit`; `enforce` switches them off and the profiles' request figures take
  their place. `package_write` holds in both modes. An installation that is updated therefore keeps its request limit, and gains it
  on `/v2/`, before anyone switches to `enforce`.
- Switching from `audit` to `enforce` does not reset a counter: a subject over a limit is refused at its next request.

Data model: `limit_settings` (one row): `mode text NOT NULL DEFAULT 'audit' CHECK (mode IN ('audit','enforce'))`,
`block_seconds integer NOT NULL DEFAULT 900 CHECK (block_seconds BETWEEN 60 AND 86400)`, `version`, `updated_at`, `updated_by`.

Acceptance criteria:

- In `audit`, an anonymous address that exceeds `cachedBytesPerHour` of the profile Anonymous downloads on unslowed and unrefused;
  `GET /api/v1/usage?state=wouldBeLimited` lists it with `wouldLimitEvents` of `cachedBytesPerHour`, and the request log of the next
  download has `limits_outcome: "would_limit:cachedBytesPerHour"`.
- After `PATCH /api/v1/limits/settings {"mode": "enforce"}`, the same address's next download answers 429 on every replica within
  the propagation bound.

## Usage store and overview

### R-LIM-027 · The usage store: what is recorded and for how long
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 8: single values 30 days, after that sums without a person.)

- **What**: per subject, credential, place, repository that served, and time bucket: requests, refused requests, cached bytes,
  uncached bytes, upstream requests, background downloads (W-25), and the limits it met per figure, as `limited` (Enforce) and
  `would_limit` (Audit) counts. Each row also records the budget key's effective hour and day limits at the flush, so that the
  overview computes utilisation in SQL (R-LIM-034).
- **Buckets** ([Q-LIM-20](#q-lim-20--buckets-of-five-minutes-for-a-day-of-an-hour-for-30-days-then-daily-sums-per-repository-and-caller-kind)):
  five minutes, kept 25 hours (the 24-hour history and the periods `hour` and `day`); one hour, kept 30 days (the period `30d`).
- **After 30 days** the hourly rows of a day are folded into **daily sums without a person**: per day, repository (or none) and
  kind of caller (`user`, `technicalToken`, `ci`, `anonymous`, `system`): the number of distinct subjects, requests, refused
  requests, cached bytes, uncached bytes, upstream requests, background downloads, limited and would-limit events. Kept
  `limits.usage.summary_retention` (400 days by default, an estimate), then deleted.
- **Credentials**: for a user, the credential column holds `session` or the personal token's ID; empty for every other subject.
- **Scale** (an estimate for the reader, not a bound): 2,000 subjects active in an hour, each in three repositories, write 6,000
  hourly rows an hour (4.3 million in 30 days) and up to 72,000 five-minute rows an hour (1.7 million kept).

Data model (migration of work package 2):

```sql
CREATE TABLE usage_bucket (
  granularity          text NOT NULL CHECK (granularity IN ('5m', '1h')),
  bucket_start         timestamptz NOT NULL,
  subject_kind         text NOT NULL CHECK (subject_kind IN ('user', 'technical_token', 'ci', 'anonymous', 'system')),
  subject_ref          text NOT NULL,
  credential_ref       text NOT NULL DEFAULT '',
  place                text NOT NULL,
  repository_id        bigint NULL REFERENCES repositories (id) ON DELETE CASCADE,
  requests             bigint NOT NULL DEFAULT 0,
  refused              bigint NOT NULL DEFAULT 0,
  cached_bytes         bigint NOT NULL DEFAULT 0,
  uncached_bytes       bigint NOT NULL DEFAULT 0,
  upstream_requests    bigint NOT NULL DEFAULT 0,
  background_fetches   bigint NOT NULL DEFAULT 0,
  limit_requests_hour  bigint NULL,   -- the budget key's effective limits when the row was last written; null = unlimited
  limit_cached_hour    bigint NULL,
  limit_uncached_hour  bigint NULL,
  limit_upstream_hour  bigint NULL,
  limit_bytes_day      bigint NULL
);
CREATE UNIQUE INDEX usage_bucket_key ON usage_bucket
  (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id) NULLS NOT DISTINCT;

CREATE TABLE usage_limit_event (
  granularity   text NOT NULL,
  bucket_start  timestamptz NOT NULL,
  subject_kind  text NOT NULL,
  subject_ref   text NOT NULL,
  place         text NOT NULL,
  repository_id bigint NULL REFERENCES repositories (id) ON DELETE CASCADE,
  figure        text NOT NULL,   -- an API figure name, or 'ceiling.anonymous' / 'ceiling.signedIn'
  mode          text NOT NULL CHECK (mode IN ('audit', 'enforce')),
  count         bigint NOT NULL
);
CREATE UNIQUE INDEX usage_limit_event_key ON usage_limit_event
  (granularity, bucket_start, subject_kind, subject_ref, place, repository_id, figure, mode) NULLS NOT DISTINCT;

CREATE TABLE usage_daily_summary (
  day                 date NOT NULL,
  repository_id       bigint NULL REFERENCES repositories (id) ON DELETE CASCADE,
  subject_kind        text NOT NULL,
  subjects            bigint NOT NULL,
  requests            bigint NOT NULL,
  refused             bigint NOT NULL,
  cached_bytes        bigint NOT NULL,
  uncached_bytes      bigint NOT NULL,
  upstream_requests   bigint NOT NULL,
  background_fetches  bigint NOT NULL,
  limited_events      bigint NOT NULL,
  would_limit_events  bigint NOT NULL
);
CREATE UNIQUE INDEX usage_daily_summary_key ON usage_daily_summary (day, repository_id, subject_kind) NULLS NOT DISTINCT;
```

A deleted repository takes its rows with it; a moved repository keeps them, and the overview of a project reads them through the
repository's current project.

### R-LIM-028 · Writing the usage store, erasure and addresses
Priority: **MVP** · Covers: BEYOND-030

- **The buffer.** Each replica sums its charges in memory per row key (the usage store's unique key without the granularity) and
  writes them every `limits.usage.flush_interval` (10 s) in one transaction: one upsert statement for the five-minute rows and one
  for the hourly rows of the batch, in key order, `count = count + n`, and the limit events likewise. It is a write-only buffer
  (R-ARCH-032), as `internal/items/stats.go` is: no request reads it.
- **Loss bound.** The buffer is flushed at shutdown (R-ARCH-024), so a drain loses nothing; a crash loses at most one flush
  interval of usage records. The enforcing counters in Valkey are not affected by either.
- **A failed flush** keeps its batch merged with what arrived meanwhile and retries at the next interval; the buffer holds at most
  `limits.usage.max_buffered_keys` (100,000) keys. Beyond that, a new key's counts are merged into an overflow row per repository and
  subject kind with the subject `overflow`, counted in `quader_limits_usage_overflow_total`. A flood of distinct addresses cannot
  grow a replica's memory without bound.
- **The daily task `limits-cleanup`** (on `worker`, `25 3 * * *` by default, R-OPS-019): folds the hourly rows older than 30 days into the daily
  sums and deletes them; deletes five-minute rows older than 25 hours, daily sums older than the summary retention, `limit_block`
  rows older than 30 days; deletes the usage rows, blocks and rules whose user, group or technical token no longer exists
  ([Q-LIM-25](#q-lim-25--the-usage-of-a-deleted-user-is-erased-by-the-daily-task-within-a-day)).
- **Erasure.** A deleted user's rows are gone within one day of the deletion, with the daily task; the daily sums never held the
  user. The audit trail keeps what R-OPS-017 keeps.
- **Addresses** ([Q-LIM-22](#q-lim-22--an-address-is-stored-as-its-subject-prefix-and-deleted-with-the-hourly-rows)) are stored as
  the subject prefix of R-LIM-002 (an IPv4 address, an IPv6 `/64`) in the five-minute and hourly rows and in `limit_block`, and
  nowhere else of this page; they are deleted with those rows (25 hours, 30 days). The daily sums hold no address.

Configuration:

```yaml
limits:
  flush_interval: 1s                  # default (proposed): byte and refused charges to Valkey
  parallel:
    lease: 60s                        # default (proposed)
    wait: 10s                         # default (proposed)
  pacing:
    min_bytes_per_second: 65536       # default (proposed)
  usage:
    flush_interval: 10s               # default (proposed)
    max_buffered_keys: 100000         # default (proposed)
    summary_retention: 9600h          # default (proposed): 400 days
```

### R-LIM-029 · Who sees which usage
Priority: **MVP** · Covers: BEYOND-030

(Owner, decision 8.)

| Caller | Sees |
|---|---|
| Holder of `usage:read` (`admin`, `security-admin`, `auditor`, R-AUTHZ-040) | every subject in every scope, with addresses, the detail, the explanation of any subject, the CSV export |
| An Owner of a project | the usage rows whose repository belongs to the project now, of every subject kind, with the names of users and technical tokens and the addresses of anonymous callers ([Q-LIM-21](#q-lim-21--a-project-owner-sees-the-addresses-of-the-anonymous-callers-of-its-repositories)); a subject's detail restricted to those rows; the explanation for its repositories; the CSV export of its scope |
| Every signed-in user | its own subject: consumption, limits, the explanation for any repository it can read, blocks of its subject (`/api/v1/users/me/usage`, `/api/v1/users/me/limits`) |
| A technical token, a CI token | nothing of this page through its own credential, unless its scope and its account's role give `usage:read` |

A member who is not Owner sees nothing of the project's usage but its own.

### R-LIM-030 · Metrics, logs and audit events
Priority: **MVP** · Covers: BEYOND-030

- **Metrics** carry no subject (R-OPS-007: no principal, token ID or address as a label value; Q-OPS-175's dashboard stays without
  history, this page's overview has its own store). Added families:
  - `quader_limits_decisions_total{figure,mode,outcome}`: `outcome` `admitted`, `limited`, `would_limit`, `paced`;
  - `quader_limits_bytes_total{class,kind}`: `class` `cached`, `uncached`; `kind` the subject kinds;
  - `quader_limits_upstream_requests_total{kind}`;
  - `quader_limits_blocks_started_total{kind}`;
  - `quader_limits_mode` (gauge, 1 in `enforce`, 0 in `audit`);
  - `quader_limits_usage_buffer_keys` (gauge), `quader_limits_usage_flush_failures_total`, `quader_limits_usage_overflow_total`;
  - the limiter's fallback as `quader_ratelimit_fallback{limiter="limits"}`.

  The label `figure` (the API figure names and `ceiling`) joins the enumerations of R-OPS-007.
- **The request log** (R-OPS-010) of a request on a protocol path fills `project`, `repo`, `format`, `cache`, `limiter` and
  `limit_mode` (until now filled by the Bastion only) and gains `limits_subject` (`user:<id>`, `technical_token:<id>`,
  `ci:<issuer>:<project ID>`, `anonymous:<prefix>`), `limits_place` (`instance`, `project:<key>`, `repository:<projectKey>/<repoKey>`),
  `limits_rule` (the assignment's ID or `builtin:<kind>`), `limits_outcome` (`admitted`, `limited:<figure>`, `would_limit:<figure>`,
  `paced`, `blocked`), `bytes_cached` and `bytes_uncached`.
- **Audit events** (R-OPS-016, all transactional unless marked):

| Event | Recorded when | Key fields in `details` |
|---|---|---|
| `limits.profile.created`, `.updated`, `.deleted` | A profile changes | `key`, `changes` |
| `limits.assignment.created`, `.updated`, `.deleted` | A rule changes, by the API or by `limits-cleanup` | `place`, `who`, `profile`, `overrides`, `capped` |
| `limits.ceiling.changed` | A remote's ceilings change | `repository`, `before`, `after` |
| `limits.project_maximum.changed` | A project's maximum is set, changed or removed | `project`, `before`, `after` |
| `limits.mode.changed` | The mode changes | `before`, `after` |
| `limits.settings.changed` | The block time changes | `before`, `after` |
| `limits.block.started` (asynchronous) | A block is set | `subject`, `place`, `until`, `refused`, `limit` |
| `limits.block.lifted` | A block is lifted | `subject`, `place`, `block` |

Events of technical tokens and repository service accounts are on [identity-and-authentication.md](identity-and-authentication.md)
R-AUTH-045 and R-AUTH-046.

## API

The operations below are listed in [api-and-ui.md](api-and-ui.md) R-API-082 with their permissions; the bodies here are the contract
the OpenAPI document is written from. Every write answers with the resource and `ETag`, takes `If-Match` on change and delete (428
without it, 412 on a stale one, R-API-012), and is audited. Lists page with `limit` (default 50, at most 200) and `cursor`
(R-API-010).

### R-LIM-031 · Profiles API
Priority: **MVP** · Covers: BEYOND-030

| Method and path | Purpose | Status |
|---|---|---|
| `GET /api/v1/limits/profiles` | Every profile | 200 |
| `POST /api/v1/limits/profiles` | Create an own profile | 201; 409 `already_exists`; 422 `invalid_figure` |
| `GET /api/v1/limits/profiles/{key}` | One profile | 200; 404 |
| `PUT /api/v1/limits/profiles/{key}` | Replace name, description and figures | 200; 404; 409 `profile_immutable`; 412; 422; 428 |
| `DELETE /api/v1/limits/profiles/{key}` | Delete an own profile | 204; 404; 409 `profile_builtin`, `profile_in_use`; 412 |

A profile:

```json
{
  "key": "partners",
  "name": "Partners",
  "description": "External partners with a contract for nightly mirrors",
  "builtin": false,
  "figures": {
    "requestsPerSecond": 50,
    "requestsBurst": 200,
    "requestsPerHour": 5000,
    "cachedBytesPerSecond": 50000000,
    "cachedBytesPerHour": 50000000000,
    "uncachedBytesPerSecond": 10000000,
    "uncachedBytesPerHour": 5000000000,
    "upstreamRequestsPerHour": 200,
    "parallelDownloads": 6,
    "bytesPerDay": 100000000000,
    "refusedPerHour": 100
  },
  "usedBy": {"assignments": 3, "projectMaximums": 0},
  "version": 2,
  "createdAt": "2026-11-20T09:00:00Z",
  "createdBy": "ada",
  "updatedAt": "2026-11-21T10:00:00Z",
  "updatedBy": "ada"
}
```

*(example)* Every member of `figures` is present in an answer; `null` is unlimited. A write sends `key` (create only), `name`,
`description` and `figures`; a figure missing from a write is `null`. `usedBy`, `builtin`, `version` and the times are read-only.

### R-LIM-032 · Assignments and ceilings API
Priority: **MVP** · Covers: BEYOND-030

The rules of a place live under the place:

| Place | Base path |
|---|---|
| Instance | `/api/v1/limits/assignments` |
| Project | `/api/v1/projects/{projectKey}/limits/assignments` |
| Repository | `/api/v1/projects/{projectKey}/repositories/{repoKey}/limits/assignments` |

| Method and path (below the base) | Purpose | Status |
|---|---|---|
| `GET` | The place's rules, filters `whoKind`, `profile` | 200 |
| `POST` | Create | 201; 404 (a place, a profile or a "who" that does not exist); 409 `assignment_exists`; 422 `invalid_who`, `invalid_figure`, `limit_above_maximum` |
| `GET /{id}` | One rule | 200; 404 |
| `PUT /{id}` | Replace profile, overrides and note; `who` is immutable | 200; 404; 409 `assignment_builtin` for a change of `who`; 412; 422; 428 |
| `DELETE /{id}` | Delete | 204; 404; 409 `assignment_builtin`; 412 |

A rule:

```json
{
  "id": "0193f2a0-7c1e-7b55-9a10-3f2c8d4e5a61",
  "place": {"kind": "repository", "repository": "web/npm-remote"},
  "who": {"kind": "group", "group": "contractors", "displayName": "Contractors", "deleted": false},
  "profile": "restricted",
  "overrides": {"uncachedBytesPerHour": 20000000000},
  "effective": {
    "requestsPerSecond": 50, "requestsBurst": 200, "requestsPerHour": 5000,
    "cachedBytesPerSecond": 50000000, "cachedBytesPerHour": 50000000000,
    "uncachedBytesPerSecond": 10000000, "uncachedBytesPerHour": 20000000000,
    "upstreamRequestsPerHour": 200, "parallelDownloads": 6, "bytesPerDay": 100000000000, "refusedPerHour": 100
  },
  "clamped": {},
  "capped": true,
  "builtin": false,
  "note": "external contractors, narrowed on the public mirror",
  "version": 1,
  "createdAt": "2026-11-20T09:05:00Z",
  "createdBy": "priya",
  "updatedAt": "2026-11-20T09:05:00Z",
  "updatedBy": "priya"
}
```

*(example)* The forms of `who`: `{"kind": "user", "user": "alice"}`, `{"kind": "technicalToken", "technicalToken": "<ID>",
"name": "deploy-prod", "account": "sa:web/partner"}`, `{"kind": "ciProject", "issuer": "gitlab-internal", "projectId": "4711",
"projectPath": "acme/web"}`, `{"kind": "address", "prefix": "198.51.100.0/24"}`, `{"kind": "group", "group": "web-devs"}`,
`{"kind": "every", "callerKind": "anonymous"}`. A write names a user and a group by name, a technical token by ID, a CI issuer by
name; `displayName`, `name`, `account`, `projectPath` and `deleted` are read-only. `clamped` holds the figures R-LIM-016 lowered
now, with the value that applies. An address prefix is stored canonical (`198.51.100.77/24` becomes `198.51.100.0/24`); an IPv6
prefix longer than `/64` answers 422 `invalid_who`.

**Ceilings** of a remote repository:

| Method and path | Purpose | Status |
|---|---|---|
| `GET /api/v1/projects/{projectKey}/repositories/{repoKey}/limits/ceilings` | Both classes | 200; 404; 422 `not_applicable` (not remote) |
| `PUT /api/v1/projects/{projectKey}/repositories/{repoKey}/limits/ceilings` | Replace both classes | 200; 404; 412; 422; 428 |

```json
{
  "repository": "platform/dockerhub",
  "anonymous": {"uncachedBytesPerHour": 10000000000, "upstreamRequestsPerHour": 100},
  "signedIn": {"uncachedBytesPerHour": 50000000000, "upstreamRequestsPerHour": 2000},
  "version": 1,
  "updatedAt": "2026-11-20T09:10:00Z",
  "updatedBy": "priya"
}
```

*(example: the owner's public remote)*

### R-LIM-033 · Settings, the project's maximum and blocks API
Priority: **MVP** · Covers: BEYOND-030

| Method and path | Purpose | Status | Permission |
|---|---|---|---|
| `GET /api/v1/limits/settings` | The mode and the block time | 200 | `limits:read` |
| `PATCH /api/v1/limits/settings` | Change them; a change of `mode` needs recent authentication | 200; 412; 422; 428 | `limits:write` |
| `GET /api/v1/limits/mode-preview` | What `enforce` would have done in the last 24 hours | 200 | `limits:read` |
| `GET /api/v1/projects/{projectKey}/limits/maximum` | The project's maximum | 200; 404 (none, or no project) | `limits:read` or an Owner of the project |
| `PUT /api/v1/projects/{projectKey}/limits/maximum` | Set or replace it | 200, 201; 412; 422; 428 | `limits:write` |
| `DELETE /api/v1/projects/{projectKey}/limits/maximum` | Remove it | 204; 404; 412 | `limits:write` |
| `GET /api/v1/limits/blocks?active=&place=` | Blocks (active by default), newest first | 200 | `limits:read`; an Owner with `place` of its project |
| `DELETE /api/v1/limits/blocks/{id}` | Lift an active block | 204; 404; 409 `block_ended` | `limits:write`, or an Owner of the block's project |

```json
{"mode": "audit", "blockSeconds": 900, "version": 4, "updatedAt": "2026-11-20T08:00:00Z", "updatedBy": "ada"}
```

```json
{
  "period": {"from": "2026-11-19T10:00:00Z", "to": "2026-11-20T10:00:00Z"},
  "subjects": 12,
  "refusedRequests": 3400,
  "slowedDownloads": 25,
  "blockedSubjects": 1,
  "byFigure": [{"figure": "cachedBytesPerHour", "subjects": 3, "events": 140}, {"figure": "requestsPerSecond", "subjects": 9, "events": 3260}],
  "byKind": [{"kind": "anonymous", "subjects": 10}, {"kind": "user", "subjects": 2}]
}
```

*(examples: settings; mode preview, read from the `audit` limit events of the five-minute buckets)*

```json
{"project": "web", "profile": "technical", "overrides": {"bytesPerDay": 2000000000000}, "effective": {"requestsPerSecond": 400, "requestsBurst": 2000, "requestsPerHour": 200000, "cachedBytesPerSecond": 500000000, "cachedBytesPerHour": 1000000000000, "uncachedBytesPerSecond": 100000000, "uncachedBytesPerHour": 100000000000, "upstreamRequestsPerHour": 10000, "parallelDownloads": 50, "bytesPerDay": 2000000000000, "refusedPerHour": 2000}, "version": 1, "updatedAt": "2026-11-20T08:30:00Z", "updatedBy": "ada"}
```

```json
{
  "id": "0193f2b1-0d2c-7c00-8a77-1a2b3c4d5e6f",
  "subject": {"kind": "anonymous", "id": "198.51.100.23", "address": "198.51.100.23"},
  "place": {"kind": "repository", "repository": "platform/dockerhub"},
  "startedAt": "2026-11-20T09:40:00Z",
  "until": "2026-11-20T09:55:00Z",
  "refused": 61,
  "limit": 60,
  "liftedAt": null,
  "liftedBy": null
}
```

*(examples: a project's maximum; a block)*

### R-LIM-034 · Usage API
Priority: **MVP** · Covers: BEYOND-030

| Method and path | Purpose | Permission |
|---|---|---|
| `GET /api/v1/usage` | Every subject's consumption in the instance | `usage:read` |
| `GET /api/v1/projects/{projectKey}/usage` | Consumption in the project; `repository=<repoKey>` narrows to one repository | `usage:read` or an Owner |
| `GET /api/v1/usage/export`, `GET /api/v1/projects/{projectKey}/usage/export` | The same list as CSV (`text/csv`), every row of the filter up to 100,000 | as the list |
| `GET /api/v1/usage/subjects/{kind}/{id}` | One subject's detail | `usage:read` |
| `GET /api/v1/projects/{projectKey}/usage/subjects/{kind}/{id}` | One subject's detail within the project | `usage:read` or an Owner |
| `GET /api/v1/users/me/usage` | The caller's own detail | every signed-in user |
| `GET /api/v1/limits/subjects?q=&kinds=&project=` | Suggestions of technical tokens and CI projects for the rule dialog, by name, 20 at most | `limits:read` or an Owner of `project` |

**Parameters of the lists**: `period` = `hour` (the last 60 minutes of five-minute buckets), `day` (the last 24 hours of
five-minute buckets) or `30d` (the last 30 days of hourly buckets), default `day`; `kind` = one or more of `user`, `technicalToken`,
`ci`, `anonymous`, `system`; `state` = `close`, `limitReached`, `wouldBeLimited`, `blocked`; `q` = a search on the subject's name or
address; `sort` = `requests`, `cachedBytes`, `uncachedBytes`, `upstreamRequests`, `refused`, `utilisation` or `name`, `order` =
`desc` (default) or `asc`; `limit`, `cursor`.

**`{kind}/{id}`**: `user/<username>`, `technicalToken/<ID>`, `ci/<issuer name>:<project ID>`, `anonymous/<address or prefix,
percent-encoded>`, `system/system`.

**Utilisation and state** of a row are computed in SQL from the usage store, independent of `period`
([Q-UI-732](api-and-ui.md#q-ui-732--the-meter-shows-the-tightest-hour-or-day-figure-now-not-the-periods)): the highest ratio,
over the subject's budget keys in the scope, of the last 60 minutes' requests, cached bytes, uncached bytes and upstream requests
to the hour limits the rows recorded, and of the last 24 hours' bytes to the day limit. `state` is `blocked` while a block is
active, `limitReached` with a `limited` event in the last 60 minutes or a ratio of at least 1 in `enforce`, `wouldBeLimited` with a
`would_limit` event or a ratio of at least 1 in `audit`, `close` from 0.7, `unlimited` when every hour and day figure is unlimited,
else `normal`.

A list row:

```json
{
  "subject": {"kind": "user", "id": "alice", "name": "alice", "displayName": "Alice Example"},
  "requests": 48210,
  "refused": 3,
  "cachedBytes": 1203000000,
  "uncachedBytes": 50000000,
  "upstreamRequests": 120,
  "limitedEvents": 0,
  "wouldLimitEvents": 2,
  "utilisation": {"ratio": 0.82, "figure": "cachedBytesPerHour", "place": {"kind": "instance"}},
  "state": "close",
  "blockedUntil": null
}
```

The list answer: `{"period": {"from", "to"}, "mode": "audit", "items": [...], "next": "<cursor>"}`. The subject's forms:
`{"kind": "technicalToken", "id": "<ID>", "name": "deploy-prod", "account": "sa:web/partner", "level": "project"}`,
`{"kind": "ci", "id": "gitlab-internal:4711", "issuer": "gitlab-internal", "projectId": "4711", "projectPath": "acme/web"}`,
`{"kind": "anonymous", "id": "2001:db8:1:2::/64", "address": "2001:db8:1:2::/64"}`, `{"kind": "system", "id": "system"}`.

A subject's detail:

```json
{
  "subject": {"kind": "user", "id": "alice", "name": "alice", "displayName": "Alice Example"},
  "period": {"from": "2026-11-19T10:00:00Z", "to": "2026-11-20T10:00:00Z"},
  "mode": "audit",
  "totals": {"requests": 48210, "refused": 3, "cachedBytes": 1203000000, "uncachedBytes": 50000000, "upstreamRequests": 120, "limitedEvents": 0, "wouldLimitEvents": 2},
  "budgets": [
    {
      "place": {"kind": "instance"},
      "rule": {"id": "builtin:user", "who": {"kind": "every", "callerKind": "user"}, "profile": "standard", "builtin": true},
      "figures": {
        "requestsPerHour": {"limit": 50000, "used": 31000, "windowSeconds": 3600},
        "cachedBytesPerHour": {"limit": 300000000000, "used": 246000000000, "windowSeconds": 3600},
        "uncachedBytesPerHour": {"limit": 30000000000, "used": 50000000, "windowSeconds": 3600},
        "upstreamRequestsPerHour": {"limit": 2000, "used": 120, "windowSeconds": 3600},
        "bytesPerDay": {"limit": 1000000000000, "used": 1253000000, "windowSeconds": 86400},
        "parallelDownloads": {"limit": 20, "used": 2},
        "refusedPerHour": {"limit": 500, "used": 3, "windowSeconds": 3600},
        "requestsPerSecond": {"limit": 200, "burst": 1000},
        "cachedBytesPerSecond": {"limit": 200000000},
        "uncachedBytesPerSecond": {"limit": 50000000}
      },
      "blockedUntil": null
    }
  ],
  "history": [{"start": "2026-11-20T09:55:00Z", "requests": 410, "cachedBytes": 9000000, "uncachedBytes": 0, "upstreamRequests": 0, "refused": 0, "limitedEvents": 0, "wouldLimitEvents": 0}],
  "repositories": [{"repository": "web/npm-remote", "requests": 30000, "cachedBytes": 1000000000, "uncachedBytes": 50000000, "upstreamRequests": 120, "refused": 0}],
  "credentials": [{"credential": {"kind": "session"}, "requests": 210, "cachedBytes": 3000000, "uncachedBytes": 0}, {"credential": {"kind": "token", "id": "0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e11", "hint": "qdr_pat_3kQ9", "description": "laptop npm"}, "requests": 48000, "cachedBytes": 1200000000, "uncachedBytes": 50000000}]
}
```

*(example)* `used` of a windowed figure is the live estimate of the Valkey window (one pipeline of reads for the subject's budget
keys); `history` holds the five-minute points of the last 24 hours whatever `period` is; `repositories` and `credentials` follow
`period`; `credentials` only for a user. A project Owner's detail holds only the budgets, history, repositories and credentials of
the project's repositories.

**CSV columns**: `subject_kind, subject, display_name, account, from, to, requests, refused, cached_bytes, uncached_bytes,
upstream_requests, limited_events, would_limit_events, utilisation, utilisation_figure, state`.

### R-LIM-035 · Which limit applies
Priority: **MVP** · Covers: BEYOND-030

`GET /api/v1/limits/explain?subject=<kind>/<id>&repository=<projectKey>/<repoKey>` (permission `limits:read`; an Owner of the
repository's project for that repository; `GET /api/v1/users/me/limits?repository=<projectKey>/<repoKey>` for the caller itself,
for a repository it can read) answers the resolution of R-LIM-013 step by step, the effective figures, the clamp and the ceilings:

```json
{
  "subject": {"kind": "user", "id": "alice", "name": "alice", "displayName": "Alice Example"},
  "repository": "web/npm-remote",
  "mode": "enforce",
  "administrator": false,
  "steps": [
    {"place": {"kind": "repository", "repository": "web/npm-remote"}, "step": "single", "outcome": "none"},
    {"place": {"kind": "repository", "repository": "web/npm-remote"}, "step": "groups", "outcome": "matched",
     "rules": [{"id": "0193f2a0-7c1e-7b55-9a10-3f2c8d4e5a61", "who": {"kind": "group", "group": "contractors"}, "profile": "restricted"}]}
  ],
  "place": {"kind": "repository", "repository": "web/npm-remote"},
  "rule": {"id": "0193f2a0-7c1e-7b55-9a10-3f2c8d4e5a61", "who": {"kind": "group", "group": "contractors"}, "profile": "restricted", "capped": true},
  "effective": {"requestsPerSecond": 50, "requestsBurst": 200, "requestsPerHour": 5000, "cachedBytesPerSecond": 50000000, "cachedBytesPerHour": 50000000000, "uncachedBytesPerSecond": 10000000, "uncachedBytesPerHour": 20000000000, "upstreamRequestsPerHour": 200, "parallelDownloads": 6, "bytesPerDay": 100000000000, "refusedPerHour": 100},
  "clamped": {},
  "figureSources": {"uncachedBytesPerHour": "override"},
  "ceiling": {"class": "signedIn", "uncachedBytesPerHour": 50000000000, "upstreamRequestsPerHour": 2000}
}
```

*(example)* `steps` lists every step tried until the one that matched; `figureSources` names, for a figure that did not come from
the rule's profile, `override`, `group:<name>` (several group rules) or `clamp`; `ceiling` is `null` for a repository that is not
remote. An Owner's explanation of a subject at an instance rule shows the instance rule, since that is what applies in its
repository.

## Formats and later work

### R-LIM-036 · The format contract, version 6
Priority: **MVP** · Covers: BEYOND-030

The rule that holds every format, built and future, to these figures is part of the format contract
([Q-LIM-24](#q-lim-24--the-format-contract-moves-to-version-6-hostserve-admits-and-the-route-kind-is-the-declaration)):

- **The route's kind is the declaration** (R-LIM-007): `Download` is a download; `Metadata`, `Computed` and `Volatile` are
  documents; `Upload`, `Delete` and `Admin` are writes. A format already declares a kind for every route (R-ARCH-008); it now also
  decides what is checked, paced and counted.
- **`Host.Serve` may refuse** before the status line: it runs the admission of R-LIM-021 for a download and returns the 429 error
  without writing, which the format returns to the dispatcher like any error of `Serve`. A format answers the body of a `Download`
  route through `Host.Serve` or the remote engine (`Host.Remote()`), the two paths that admit and pace; a body it writes itself is
  counted by the dispatcher's writer but neither admitted nor paced.
- **Upstream exchanges** of a format for a client's request go through the remote engine or the upstream client `Host.Remote()`
  gives it, which counts them (R-LIM-006); a format opens no other outbound connection.
- `formats.ContractVersion` is 6 (a method changed meaning: `Host.Serve`), and every format moves in the same change.
- **The contract test**: the format kit ([internal/formats/formattest](../../internal/formats/formattest)) gains a check that runs
  each example of each `Download` route of a format with a `GET` against a host whose admission refuses, and fails unless the
  answer is 429 in the format's shape with nothing of the body written; and `formats/all` pins, per format, the kind of each route
  name in a table (`TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind`), so a new route or a new format fails until its kinds are
  listed there, the way `TestR_API_079_…` holds `VersionFetch`.

### R-LIM-037 · The hook for background downloads
Priority: **MVP** · Covers: BEYOND-030

W-25 (a probe starts the download in the background) is built after this page; its trigger and platforms are its own
([promt.md](../projektplan/promt.md), W-25). This page fixes how such a download is charged, so W-25 changes nothing here:

- The decision is taken **when the probe is answered**, on the probe's request: its subject, its place and budget key, and the
  remote's ceiling for its class. A background download starts only if step 9a (R-LIM-022) would admit an upstream request for the
  probe's caller at that moment; then the upstream request is counted, for the budget key and the ceiling, at that moment. If it
  would not, no background download starts and the probe is answered as it would have been.
- The background download's uncached bytes are charged to the probe's budget key and to the ceiling at the flush after they were
  read, as a leader's are (R-LIM-006), also when the probe's client left long before.
- It is not paced by the caller's `uncachedBytesPerSecond` and takes no parallel lease: it runs as a background request with the
  bounds W-25 gives it.
- The usage store counts it in `background_fetches` of the probe's row (R-LIM-027); in `audit`, a background download that 9a would
  have refused starts, and is recorded as `would_limit`.
- The seam is `limits.Charge.Background(repo)`, which returns the charge a background request carries (`proxy.Request.Charge`) and
  is called by W-25's trigger in the handler of the probe.

### R-LIM-038 · Technical tokens in this model
Priority: **MVP** · Covers: BEYOND-030

- A technical token (R-AUTH-045) of a service account of the instance, a project or a repository (R-AUTH-046) is a subject of its
  own (R-LIM-001); its account is none.
- Its own rule is set where its account lives: an instance account's token at the instance place, a project account's token at
  its project, a repository account's token at its repository; any other place may name it too, within R-LIM-016.
- Without a rule it gets `every` `technicalToken` (Technical), or `every` `administrator` when its account holds `admin`.
- Its rule and its history stay with it across rotation and a new secret; when the technical token is deleted, its rules go with
  `limits-cleanup` and its usage rows stay until their retention, shown as a deleted token.

### R-LIM-039 · Test suites
Priority: **MVP** · Covers: BEYOND-030

- **Unit** (`internal/limits`): resolution and clamp with the worked examples of R-LIM-013 and R-LIM-016; subject derivation for
  every principal kind of R-LIM-001; the sliding-window arithmetic of hour and day windows; pacing shares and the floor; the buffer's
  merge, overflow and flush order.
- **Integration set `limits`** (against the stack, three `web` replicas through `testenv`): the acceptance criteria of R-LIM-005,
  R-LIM-006, R-LIM-017, R-LIM-021 to R-LIM-026; with Valkey stopped, the fallback of R-LIM-019; a crashed replica's leases.
- **Format contract**: R-LIM-036's kit check and table for the six formats.
- **e2e**: a set `limits` with `npm install`, `dotnet restore`, `docker pull`, `apt-get update && apt-get download`,
  `ansible-galaxy collection install` and `curl` against a profile small enough to hit each figure once, in both modes, reading the
  client's message.

## Gaps of the protocols

What a protocol cannot carry, for the owner to decide (owner, 2026-11-19: "Das muss dann im Zweifel noch mit mir besprochen werden
wie wir vorgehen"). Until he decides, nothing beyond the rest of this page is built for them.

### Gap 1 · NuGet clients and APT do not show why a request was refused

The NuGet clients print the status and the reason phrase and no body (R-NUGET-002); APT prints the status line and reads no body
of an error (R-DEB-005). Over HTTP/2 there is no reason phrase at all, and Go's server writes the standard one. A user of either sees
"429 Too Many Requests" and not which limit or for how long.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) As specified | The body carries the reason; the user reads it on the profile's Usage tab (R-UI-185) | Nothing to build | The client's own output says nothing |
| (b) NuGet: a warning header | Each refusal on a NuGet route also carries the reason in the header the NuGet clients print as a warning (`X-NuGet-Warning`; that the clients print it was not verified in this repository) | The message reaches `dotnet restore`'s output | NuGet only; must be verified per client version |
| (c) A reason phrase over HTTP/1.1 | A server that writes its own reason phrase | Reaches APT on HTTP/1.1 | Not possible over HTTP/2; Go's server does not offer it |

### Gap 2 · The upstream counts differently from Quader

Docker Hub counts a pull as a `GET` of a manifest, not a `HEAD` and not a blob; npm's and NuGet's public registries count nothing
published. `upstreamRequestsPerHour` counts every exchange (R-LIM-006), so 100 upstream requests are not 100 pulls of Docker Hub.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) As specified | Every exchange counts | One rule for every format, simple to explain | The figure does not match the upstream's limit |
| (b) A second figure, "upstream pulls" | Each format declares which exchanges its upstream counts (OCI: a manifest `GET`) | Matches Docker Hub's pull limit | A further figure in every profile, a declaration per format |
| (c) Count only what the upstream counts | `upstreamRequestsPerHour` becomes the format's declaration | One figure | Formats whose upstream counts nothing would count nothing |

### Gap 3 · A signed index that is too old cannot be served stale

When the upstream budget is used up, an expired document is served stale within `maxStaleSeconds` (R-LIM-022). An APT `InRelease`
whose `Valid-Until` has passed is refused by APT itself; the client cannot use what Quader still holds.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) As specified | The stale release is served; APT refuses it after `Valid-Until` | No special case | `apt-get update` fails for that suite until the budget has room |
| (b) Exempt release files from the upstream figures | A `Release`, `InRelease` or `Release.gpg` revalidation is counted and never refused | `apt-get update` keeps working | A caller can cause upstream requests beyond its budget, bounded by the suites it names |

## Later

Not part of this step, named by the owner on 2026-11-19 or by this page:

- **Uploads** as figures (bytes and requests of writes); until then `package_write` of R-API-016 bounds writes on protocol paths.
- **The storage quota** of a repository and of a project ([storage.md](storage.md) R-STOR-029, R-STOR-049).
- **The Bastion's own limits**: what a Bastion serves from its cache is not counted on the main installation; the Bastion keeps its
  blocking stages ([bastion.md](bastion.md)).
- **A monthly window.**

## Security notes

- The budgets are fair-share protection and protection of the upstream's quota, not an authorization mechanism: a subject that is
  refused here may still read; a subject admitted here is still decided by the decision point.
- Nothing of this page grants anything; a rule can only change how much of what the decision point admits a caller gets.
- The budget of an anonymous caller is as good as the client address (R-ARCH-006); a misconfigured trusted-proxy list makes every
  caller one address or lets a caller choose one (H-1012 on [security/usage-and-limits.md](../security/usage-and-limits.md)).
- A Valkey writer under Quader's prefix can refuse requests with 429 or lift budgets, never admit a request the decision point
  refuses ([architecture.md](architecture.md) H-49 covers the prefix).
- The perspective is [security/usage-and-limits.md](../security/usage-and-limits.md).

## Decisions

Each question below was decided by the development agent from the listed options, except where a block says the owner decided; the
owner reviews the choices at the walkthrough.

### Q-LIM-1 · A CI token is charged to its issuer and the project of its bundle

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: each GitLab job exchanges a fresh token (R-CI-007), so the token is no subject; the owner's model hangs a CI budget on the
pipeline's project.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The issuer and the `ProjectID` of the frozen bundle | The verified pin claim, the one `ci_exchange_subject` keys on | Stable across renames (an ID, not a path); one budget per project whatever mapping matched; the same unit as R-CI-021 | A project of another GitLab with the same number is another subject only through the issuer, which (a) includes |
| (b) The mapping | One budget per identity mapping | Rules could name an existing object | A namespace-wide mapping pools many projects; a project that matches two tiers has two mappings, so its budget would depend on which matched |
| (c) The job's token | Per job | No new identifier | Every job starts with a full budget: no limit at all |

**Choice:** (a).

**Rationale:** it is the unit the owner named, it is already verified at the exchange and frozen in the token, and it needs no
lookup on the request. The development agent would revisit this if a CI issuer without a project claim is added (P1 issuers fall
back to `sub`, as R-CI-021 does).

**Applied in the spec:** R-LIM-001, R-LIM-011 (`ciProject`); [gitlab-ci-oidc.md](gitlab-ci-oidc.md) R-CI-017.

### Q-LIM-2 · A request through a virtual repository is decided at the repository the client addressed

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the owner's rule "through a virtual repository a request counts once, at the member that served it". Which repository's
rules decide is a second question: the request figures are decided at stage 7, before a member is known.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Decide every figure at the addressed repository; record usage at the serving member; ceilings at the remote member (chosen) | One resolution per request | One answer to "which limit applies to me here": the repository the client configured; the request and its bytes count against one budget key | A rule on a member does not apply to a request that came through the virtual repository |
| (b) Requests at the addressed repository, bytes at the member | Two resolutions | The member's rule governs its bytes | One request against two budget keys; "which limit applies" has two answers |
| (c) Everything at the member | — | — | The request figures are decided before the member is known |

**Choice:** (a). The remote member's ceiling always applies, since it protects that member's upstream.

**Rationale:** clients configure one repository; a rule an Owner puts on the virtual repository is the one its users meet, and the
explanation stays one answer. The development agent would revisit this if Owners expect a member's rule to hold through the virtual
repository (the explanation would then name both).

**Applied in the spec:** R-LIM-006, R-LIM-013.

### Q-LIM-3 · Protocol paths are counted by the figures; the management API keeps its classes

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The figures count `/api/<format>/`, `/files/`, `/v2/`; `/api/v1/` keeps `read`, `write`, `anonymous` (chosen) | The border is the protocol | The owner's figures are about repositories; the UI's management traffic does not spend a download budget; F-155 closed | Two systems of limits, each on its own paths |
| (b) The figures count every request | One system | — | A UI session's page loads spend the download budget; the management classes protect the API's cost per request, which a download profile does not express |
| (c) The figures count downloads only | Only content | — | Metadata floods and upstream requests through documents stay unbounded |

**Choice:** (a). `/files/` moves from the general classes to the protocol paths: it is the generic format's protocol.

**Rationale:** the owner's requirement names repository requests; management API calls are a different cost. The development
agent would revisit this if a management operation turns out to move content (a bulk export), which would then be a protocol path.

**Applied in the spec:** R-LIM-005; [api-and-ui.md](api-and-ui.md) R-API-016.

### Q-LIM-4 · In Audit mode the package classes of R-API-016 stay in force as the safety net

Status: **Decided by the development agent** — selected from the options below; **the owner reviews it first** at the walkthrough.

Context: the owner chose Audit as the default state (decision 17) and "in Audit nothing is refused or slowed". Today the package
classes of R-API-016 refuse on `/api/<format>/`; an installation that updates would lose them if Audit meant "nothing".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `package_read` and `package_anonymous` keep refusing while the mode is `audit`, on every protocol path including `/v2/`, keyed by the subject; `enforce` replaces them (chosen) | The old classes are the safety net of Audit mode | No installation loses its only request limit by updating; F-155 is closed at once; the values equal the request figures of Standard and Anonymous, so switching changes little for requests | Two systems side by side in Audit mode; "nothing is refused" holds only for the figures of this page |
| (b) Audit refuses nothing at all on protocol paths | One system | The owner's sentence literally | An installation that updates has no request limit on any protocol path until someone switches; a public remote is open to unbounded upstream requests |
| (c) Enforce the request figures in Audit too, audit the rest | The profiles' request figures always hold | One system for requests | Contradicts the owner's decision 17 for a figure of his own |

**Choice:** (a).

**Rationale:** the owner's Audit is about the new figures, whose numbers are estimates; the classes that exist today are not new
and their numbers are the ones the profiles start from. Losing them on update would make the default state weaker than the release
before. The development agent would revisit this if the owner reads "nothing is refused" as including the old classes; option
(b) then is a one-line change of R-LIM-026.

**Applied in the spec:** R-LIM-005, R-LIM-026; [api-and-ui.md](api-and-ui.md) R-API-016, Q-API-344.

### Q-LIM-5 · A collapsed fetch is charged to its leader alone, also after its client left

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The leader pays the upstream request and all uncached bytes, also of a detached fetch; followers pay cached bytes (chosen) | One payer | The owner's "the second caller pays cached only"; a client cannot start fetches for free by leaving at once | A leader pays for bytes its client did not receive |
| (b) Every waiter pays uncached | — | — | One upstream fetch counted several times; contradicts the owner |
| (c) Split among the waiters | — | Fair | Unknown at the start who will wait; waiters on other replicas are invisible to the leader |

**Choice:** (a).

**Rationale:** the leader caused the fetch; charging only the bytes its client received would make "request and disconnect" a way
to fill a public remote's cache without spending a budget. The development agent would revisit this if leaders on slow links are
charged for fetches that others then use heavily.

**Applied in the spec:** R-LIM-006.

### Q-LIM-6 · Documents count their bytes; only content downloads are checked before they start, slowed and counted as parallel

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Bytes of every body count; check, pacing and parallel for downloads only (chosen) | Documents count but are never refused by the cached-bytes or day budget; a document that needs the upstream meets the upstream figures | npm package documents are large and frequent; resolution from the cache is never broken by a byte figure | A caller over its byte budget can still fetch cached documents (bounded by the request figures) |
| (b) Bytes of content only | Documents are free | — | A client that reads package documents in a loop downloads gigabytes uncounted |
| (c) Everything for every body | — | One rule | Refusing a package document breaks resolution in the middle of an install; pacing it slows every resolution |

**Choice:** (a).

**Rationale:** the owner's byte figures are about bandwidth, which documents use; his "429 before the download starts" is about
downloads. The development agent would revisit this if a format's documents are as large as its content (an APT `Contents`
index), which the route kind could then declare as a download.

**Applied in the spec:** R-LIM-006, R-LIM-007.

### Q-LIM-7 · A refused request is a refusal, not a missing package

Status: **Decided by the development agent** — selected from the options below; **the owner reviews it first** at the walkthrough.

Context: the owner's table counts "answers 401, 403 and 404 to a caller (probing for names)". NuGet asks every feed of a restore for
every package and gets a 404 from each feed that does not hold it; a restore of 300 packages over two feeds draws 300 such 404s, the
Standard profile allows 500 refused requests per hour.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Refusals of the decision point, failed credentials and unknown repositories (chosen) | What hides or refuses something | Counts probing for names the caller may not see; a normal miss in a readable repository is no probing | A caller reading a repository it may read can probe package names there, which it could list anyway |
| (b) Every 401, 403 and 404 | The owner's words | Simple | Multi-feed NuGet restores, Docker's tag probes and npm's lookups of unpublished names block legitimate users |
| (c) (b) with a format list of "normal" 404 routes | A declaration per route | Closer to (b) | A second declaration per format, and still wrong for a 404 a client expects |

**Choice:** (a).

**Rationale:** the purpose the owner gave is "probing for names"; a name can only be probed where its existence is hidden. The
development agent would revisit this if probing inside readable repositories causes load, which the request figures bound anyway.

**Applied in the spec:** R-LIM-008.

### Q-LIM-8 · A download is admitted while its budget has room, whatever its size

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Admit while the estimate is below the limit; charge after (chosen) | The size is not used | Same rule with and without a known size; a file larger than the remainder is not refused forever | One download can cross the limit |
| (b) Admit only when the size fits the remainder, where the size is known | — | Exact where possible | A file larger than any remainder of a window is refused until a window is empty; known and unknown sizes behave differently |
| (c) Reserve the size at the start, refund at the end | — | Exact | A round trip more per download; reservations of crashed replicas |

**Choice:** (a).

**Rationale:** the owner's "never in the middle" already accepts that a download admitted before the limit ends after it. The
development agent would revisit this if one download per window is a material overshoot for an installation (very large files and
small budgets).

**Applied in the spec:** R-LIM-021.

### Q-LIM-9 · An expired document is served stale within maxStaleSeconds when the upstream budget is used up

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Stale within `cache.maxStaleSeconds`, else 429 (chosen) | As an upstream failure (R-REPO-029) | The operator's bound on staleness holds whatever keeps a fresh copy away | Beyond the bound the caller gets 429 although Quader holds a copy |
| (b) Stale without bound, as the populate check of step 9 | — | Nothing is refused that is held | A caller with a used-up budget reads documents of any age, which `maxStaleSeconds` is there to prevent |
| (c) 429 for every expired document | — | — | Contradicts "what is cached is still served" |

**Choice:** (a). With `maxStaleSeconds: 0` (the default, unbounded) (a) and (b) are the same.

**Rationale:** a remote that bounds staleness does so for its users' safety (security fixes in new versions). The development agent
would revisit this if users of bounded remotes meet 429s at expiry often.

**Applied in the spec:** R-LIM-022; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-025 step 9a.

### Q-LIM-10 · The enforcing counters live in Valkey, the usage store in PostgreSQL

Status: **Decided by the development agent** — selected from the options below; **the owner reviews it first** at the walkthrough.

Context: Q-STOR-12 keeps quota counters in PostgreSQL because a storage quota must survive a Valkey incident. The figures here are
checked on every protocol request, and the brief of W-26 asks for the cost per request.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Windows in Valkey (with `INCRBY` and sorted sets in the ACL); usage buffered per replica and flushed to PostgreSQL every 10 s (chosen) | Rate-limit style | One round trip at stage 7, one more for a download or a miss; no PostgreSQL write per request; the same arithmetic as R-ARCH-034 | A Valkey failover or outage loses counts: a subject may get one more window (H-1005); the ACL grows by five commands (Q-BAS-117 had refused `INCRBY`) |
| (b) Folded deltas in PostgreSQL, as Q-STOR-12 | Exact and durable | Survives Valkey | A database write or read per request on the hottest path; the lag of the fold is seconds of 200 MB/s |
| (c) Valkey for seconds, PostgreSQL for hours and days, read through a per-replica cache | Mixed | Hour and day survive Valkey | Two stores per request; the cache adds its own lag |

**Choice:** (a).

**Rationale:** a download budget is a rate limit over a longer window: a lost window costs one more window of downloads, not data,
which is the INV-3 contract for rate-limit counters; a storage quota is different because what it admits stays. `INCRBY` is needed
because bytes are not units of 100. The development agent would revisit this if Valkey incidents are frequent enough that budgets are
visibly exceeded (`quader_ratelimit_fallback{limiter="limits"}`), and then seed local windows from the usage store.

**Applied in the spec:** R-LIM-017, R-LIM-019, R-LIM-028; [architecture.md](architecture.md) R-ARCH-031 (the command list),
R-ARCH-034 (windows to 24 hours).

### Q-LIM-11 · Windows roll by the two-window estimate, for the hour and the day too

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Rolling, two-window estimate (chosen) | As R-ARCH-034 | No reset at the full hour that lets a subject use two budgets back to back; the same code for every window | In any rolling interval up to twice the limit (bounded) |
| (b) Fixed windows | Reset at the hour and at midnight UTC | Easy to explain | A subject can spend a budget at 10:59 and again at 11:00 |
| (c) Exact rolling (a log of every charge) | — | Exact | Memory per subject grows with its requests |

**Choice:** (a). **Rationale:** consistency with every limiter of the installation and the bound it states. Revisit if users find
the estimate's wait times surprising. **Applied in the spec:** R-LIM-018.

### Q-LIM-12 · Every figure fails open to the replica's share while Valkey is unreachable

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The replica's share, local windows (chosen) | As R-ARCH-034 | Valkey's outage does not stop downloads (INV-12); budgets hold approximately | Windows restart empty; blocks set before are not seen |
| (b) Fail closed for anonymous callers | Refuse anonymous protocol requests | An outage cannot be used to pull unbounded | A Valkey outage stops every public repository |
| (c) Read the usage store during the outage | PostgreSQL as the backup counter | Hour and day keep their history | A database read per download during the outage; ten seconds of lag |

**Choice:** (a). **Rationale:** availability is the owner's first constraint for the protocols; a Valkey outage is alerted on.
Revisit with Q-LIM-10. **Applied in the spec:** R-LIM-019.

### Q-LIM-13 · A replica paces a budget key at its share of the key's downloads, with a floor per download

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Share by the key's downloads on the replica over its leases in the cluster, refreshed each second; a floor of 64 KiB/s per download (chosen) | No round trip per chunk | The sum stays at the figure while the counts are current; no download stalls | The floor lets many parallel downloads exceed the figure; a second of lag at changes |
| (b) A Valkey token bucket per chunk | Exact | Exact | A round trip per 16 KiB |
| (c) Each replica paces at the full figure | — | Simple | The figure multiplies with the replicas the subject's downloads reach |

**Choice:** (a). **Rationale:** pacing must cost nothing per chunk; the leases already exist for parallel downloads. Revisit if
the floor matters (a figure below `parallelDownloads × 64 KiB/s`). **Applied in the spec:** R-LIM-023.

### Q-LIM-14 · A further download waits up to 10 seconds, then 429

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the owner wrote "further ones wait or 429". npm opens up to 15 connections, the Anonymous profile allows 4.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Wait up to 10 s, asking once a second, then 429 (chosen) | A short queue | An install that opens more connections than the figure completes without retries | A waiting request holds a request slot and one round trip per second |
| (b) 429 at once | — | No held slot | npm's retry back-off (10 s and more) slows installs far more than waiting |
| (c) Wait without bound | — | — | Clients time out in the queue |

**Choice:** (a). **Rationale:** most downloads end within seconds; a short wait turns a burst of connections into a queue. Revisit if
waiting requests fill `max_in_flight`. **Applied in the spec:** R-LIM-024.

### Q-LIM-15 · A block holds one budget key for 15 minutes, and only requests charged to it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The budget key; requests charged to it; 900 s; lifted by an administrator or the place's Owner (chosen) | — | A wrong-token flood from a NAT address cannot block a colleague with a valid token | Anonymous colleagues behind that address are blocked (H-1010) |
| (b) The address for every request from it | — | Stops an attacker who alternates credentials | Every person behind a NAT is locked out by one attacker |
| (c) No block, only the request figures | — | No lock-out at all | The owner's figure has no consequence |

**Choice:** (a). **Rationale:** a valid credential is not the attacker's to spend. Revisit if probing with valid tokens from many
subjects appears. **Applied in the spec:** R-LIM-025.

### Q-LIM-16 · A ceiling is a remote repository's, for the two upstream figures, and none is set by default

Status: **Decided by the development agent** — selected from the options below; **the owner reviews it first** at the walkthrough.

Context: the owner's example names uncached bytes and upstream requests per hour for each class; whether the other figures are
offered was open.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Remote repositories only; `uncachedBytesPerHour` and `upstreamRequestsPerHour`; unset by default (chosen) | What protects the upstream | Matches the example; simple settings; no surprise on update | All anonymous callers together can still download any amount from the cache (H-1007) |
| (b) Every figure for every class and repository | — | Complete | Eleven figures twice per repository; the cached figures of a class are a bandwidth question for the installation, not the repository |
| (c) The example as the default of every public remote | — | Protection without setting | A number nobody measured applied everywhere |

**Choice:** (a). **Rationale:** the upstream's quota belongs to the installation and is what a crowd of callers can exhaust; cache
bandwidth is the replicas' and is bounded by `max_in_flight`. Revisit if cached-byte floods from many addresses appear.
**Applied in the spec:** R-LIM-015.

### Q-LIM-17 · An Owner's rule is clamped per figure when it is evaluated, not rewritten

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: decision 14 lets a project Owner go "higher than the instance only up to a maximum the instance administrator gives the
project"; what happens to stored rules when the maximum is lowered was open.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Clamp at evaluation; refuse at write; the cap per figure is the larger of the instance's value for the same "who" and the maximum (chosen) | No stored rule changes | Lowering a maximum takes effect at once and is reversible; the Owner sees what applies | A stored value can differ from what applies (shown in `clamped`) |
| (b) Refuse to lower a maximum below a stored rule | — | Stored equals applied | The administrator must first edit every project's rules |
| (c) Rewrite the stored rules | — | Stored equals applied | Raising the maximum again does not restore them |

**Choice:** (a). The maximum is a profile with overrides, so the administrator sets it as he sets any limit.

**Rationale:** "narrows or redistributes" means an Owner may give one caller more and another less within the instance's values;
the maximum widens that room. Revisit if Owners expect the maximum to be a pool shared by the project. **Applied in the spec:**
R-LIM-016.

### Q-LIM-18 · An administrator is a principal that holds the role admin, and is decided at the instance only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The principal holds `admin` (content:bypass), whatever the token's scope; only instance rules apply to it (chosen) | — | A person's budget does not depend on which of their tokens is used (a PAT counts against the user); an Owner cannot throttle the installation's administrators | An administrator's narrowed token is unlimited too |
| (b) The request exercises `admin` (the scope holds it) | Like `exempt_admins` | Matches authorization | The same person is unlimited in the UI and Standard with `npm` |
| (c) Every holder of a system role | — | — | An auditor reads, it does not need unlimited downloads |

**Choice:** (a). **Rationale:** the owner's "administrators unlimited" is about the person. Revisit if administrators' automation
should be limited: an instance rule naming the user does that. **Applied in the spec:** R-LIM-012, R-LIM-013.

### Q-LIM-19 · Mirror passes and other system work are recorded and not limited

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Subject `system`, recorded per repository, checked against nothing (chosen) | — | Mirrors have bounds of their own (R-REPO-052 to R-REPO-055); the overview shows what the system fetched | A mirror can use the upstream quota the ceilings protect |
| (b) Charge the signed-in ceiling | — | One upstream budget | A mirror pass would starve signed-in users |
| (c) A third ceiling class "system" | — | Separate budget | A further setting per remote beside the mirror's own bounds |

**Choice:** (a). **Rationale:** the mirror's operator chose to mirror and bounded it. Revisit if mirrors exhaust an upstream's
quota in practice. **Applied in the spec:** R-LIM-001, R-LIM-006, R-LIM-015.

### Q-LIM-20 · Buckets of five minutes for a day, of an hour for 30 days, then daily sums per repository and caller kind

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Five-minute rows kept 25 hours, hourly rows 30 days, daily sums 400 days (chosen) | — | The 24-hour history and "the last hour" to five minutes; 30 days at an hour | Two writes per key per flush |
| (b) Hourly only | — | Fewer rows | "The last hour" is one or two clock hours |
| (c) Minute buckets for 30 days | — | Precise | 60 times the rows |

**Choice:** (a). **Rationale:** the owner's periods are an hour, a day and 30 days. Revisit if the row count matters
(`usage_bucket` above ten million rows). **Applied in the spec:** R-LIM-027.

### Q-LIM-21 · A project Owner sees the addresses of the anonymous callers of its repositories

Status: **Decided by the development agent** — selected from the options below; **the owner reviews it first** at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Full subject prefixes (chosen) | — | The owner's "a project Owner what happens in the project"; an Owner can set an address rule on its public remote | Personal data reaches every project Owner of a public repository (H-1011) |
| (b) Anonymous callers as one row, "N addresses" | — | No address leaves the administrators | The Owner cannot act on an abusive address; needs an administrator |
| (c) A pseudonym per address | — | Rules by pseudonym possible | Pseudonyms of addresses are reversible by trying |

**Choice:** (a). **Rationale:** the Owner administers its public remotes and their limits; the address is kept 30 days only.
Revisit if the installation's data protection rules forbid it; (b) is then a filter in the API. **Applied in the spec:** R-LIM-029.

### Q-LIM-22 · An address is stored as its subject prefix and deleted with the hourly rows

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The prefix (IPv4 address, IPv6 `/64`), deleted with its rows after 30 days (chosen) | — | What the budget is keyed on; nothing more precise than needed | An IPv4 address is personal data for 30 days |
| (b) Truncate after a day (IPv4 `/24`) | — | Less personal data | The 30-day view cannot name the address that was limited |
| (c) A hash | — | — | Reversible for IPv4 |

**Choice:** (a). **Rationale:** an IPv6 address is already cut to `/64`; the owner fixed 30 days for single values. **Applied in
the spec:** R-LIM-028.

### Q-LIM-23 · The RateLimit fields name one policy per figure: the request figures always, the others on their refusal

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Request figures on every answer; a byte, parallel or block figure only on its refusal (chosen) | — | Small headers on every answer; a client sees why it was refused | A client cannot plan its bytes from the headers |
| (b) Every figure on every answer | — | Complete | Up to ten members on every answer; byte remainders read from Valkey on each document |
| (c) Only the refusing figure | — | Smallest | Today's answers carry the policy on success too (R-API-016) |

**Choice:** (a). **Rationale:** the draft allows several policies and units; request figures are read anyway at stage 7. **Applied
in the spec:** R-LIM-020.

### Q-LIM-24 · The format contract moves to version 6: Host.Serve admits, and the route kind is the declaration

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The existing `RouteKind` declares download, document and write; `Host.Serve` admits; a kit check and a pinned table (chosen) | No new field | Every format already declares the kind; a new format fails the test until its routes are listed | A format that writes a download's body itself escapes admission (the kit check catches it) |
| (b) A new required field `Route.Usage` | — | Explicit | Duplicates `Kind` for every route of six formats |
| (c) A rule in the core by path | — | — | The core does not know which path is content |

**Choice:** (a). **Rationale:** the kind already says what the response is; what was missing is the meaning and the test, as with
`VersionFetch` (ADR-205). **Applied in the spec:** R-LIM-036; [architecture.md](architecture.md) R-ARCH-025 (the version).

### Q-LIM-25 · The usage of a deleted user is erased by the daily task within a day

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The daily `limits-cleanup` deletes rows of subjects that no longer exist (chosen) | — | No code of the identity area changes; also covers groups and technical tokens | Up to a day of delay |
| (b) The user's deletion deletes them in its transaction | — | At once | The deletion of a user touches the usage tables, possibly millions of rows, in one transaction |

**Choice:** (a). **Rationale:** a day is within any reasonable erasure period, and the delete runs in batches. **Applied in the
spec:** R-LIM-028.

### Q-LIM-26 · The new figures send RateLimit fields only in Enforce mode

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Only in `enforce` (chosen) | — | A client never slows down for a limit that is not applied; Audit changes nothing a client sees | A client cannot be watched approaching a limit in Audit |
| (b) In both modes | — | Shows the coming limit | Some clients pace themselves on `RateLimit`, which would be a limit applied in Audit |

**Choice:** (a). **Applied in the spec:** R-LIM-020, R-LIM-026.

### Q-LIM-27 · Uploads stay under package_write in both modes until the upload figures exist

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `package_write` (10 per second, burst 50) holds in both modes on every protocol path (chosen) | — | A publish's cost (spool, validation) stays bounded; uploads are Later by the owner | Writes count against the request figures and `package_write` both |
| (b) Writes under the request figures only in `enforce` | — | One system | A publish loop gets 200 per second under Standard |

**Choice:** (a). **Applied in the spec:** R-LIM-005; [api-and-ui.md](api-and-ui.md) R-API-016.

## What this does not cover

- The protocol gaps for the owner: [Gaps of the protocols](#gaps-of-the-protocols).
- The perspective and its own gaps (H-1009 to H-1012): [security/usage-and-limits.md](../security/usage-and-limits.md).

### H-1005 · Counters lost in a Valkey outage or failover give a subject up to one more window of every budget

Component: the limits counters in Valkey (R-LIM-017, R-LIM-019). Subject: every budget key and ceiling. Dormant until the mode is
`enforce`; live then whenever Valkey is unreachable or fails over with asynchronous replication. What makes it real: a Valkey
incident, or a caller who can cause one; the local windows of each replica start empty and are not merged back, so each window open
during the incident admits up to its limit again (per replica its share, together the limit) on top of what was counted before.

### H-1006 · A download admitted before its budget was used up runs to its end

Component: the check before a download (R-LIM-021). Subject: every budget key with an hour or day byte figure. Live in `enforce`.
What makes it real: a subject with `parallelDownloads` downloads of large files started just below the limit transfers all of them
in full (owner, decision 4: never in the middle); the overshoot is bounded by R-LIM-018.

### H-1007 · Many addresses are many anonymous budgets

Component: the anonymous subject (R-LIM-002). Subject: every repository with `public` artifacts. Live in `enforce`. What makes it real:
a caller with many IPv4 addresses or many IPv6 `/64` prefixes (a `/48` holds 65,536) gets a budget per prefix; the ceilings of a
remote bound what they make Quader fetch from the upstream together, nothing bounds their cached bytes together but the replicas'
`max_in_flight`.

### H-1008 · The limits' keys in Valkey grow with the addresses that call, and can evict other keys

Component: the key family `lim` (R-LIM-017). Subject: every mechanism in Valkey (flight locks, the negative cache, change messages).
Live in both modes, since Audit counts too. What makes it real: requests from very many anonymous prefixes create keys of up to 48
hours' life; with `maxmemory-policy allkeys-lru` Valkey evicts the least recently used keys of every family, which costs duplicate
upstream fetches (INV-11), not correctness.
