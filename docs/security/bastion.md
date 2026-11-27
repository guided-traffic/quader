# Quader Bastion

Status: specified for phase 5b of the [project plan](../projektplan/BETA.md#phase-5b--quader-bastion-m13) and built. The Bastion's own side is
wired into one role ([developer page](../developer/bastion.md#how-the-role-is-wired-package-4c)): the request flow
([internal/bastion/engine](../../internal/bastion/engine)), the guard, the cache backends, the Valkey state and the link to the main
installation. On the main installation's side the registrations and the Bastion token
([below](#the-registration-and-its-token-on-the-main-installation)) and the interface (recognition, check, feed, download intake,
contact; [its section](#the-main-installations-side-of-the-interface)) are built, and so is the read-only web interface for anonymous
visitors (W-35: [the catalogue's wire](#the-public-catalogue-and-the-configuration-on-the-wire), [its builder](#the-builder-of-the-public-catalogue),
[the copy at the Bastion](#the-public-catalogue-at-the-bastion), [the pages](#the-web-interface), [search engines](#search-engines)).
This page describes the code, with links to it, and the threat model of [spec/bastion.md](../spec/bastion.md).

This page says what the internet-facing Bastion can do, what an attacker who reaches it can do, what the Bastion's own token
can do, and where abuse is stopped. The roles and boundaries of the main installation are in
[roles-and-trust-boundaries.md](roles-and-trust-boundaries.md), its tokens in [tokens-and-credentials.md](tokens-and-credentials.md)
and its permissions in [permissions.md](permissions.md); the rules are in the spec page.

## What the Bastion is trusted with

```text
 internet ──► HAProxy Ingress ──► Bastion replicas ──(Bastion token + caller's credential)──► main endpoint
 (anyone)     stage 1: coarse     stage 2: blocks, limits,                                    decides every
              per-address limit   failed-auth timeout, budget                                 answer, renders
                                  │            │                                              every byte
                                  ▼            ▼
                      own bucket or volume   Valkey (optional)
                      (bytes, signed entries) (blocks, counters, signed answers)
```

| The Bastion holds | It does not hold |
|---|---|
| The Bastion token (`qdr_bst_`), read from a mounted Secret and re-read on change (R-BAS-006) | Any user, group, policy, token hash or session of the main installation (R-BAS-020) |
| The main installation's answers about callers, keyed by the SHA-256 of the presented token, for at most `bastion.answers.period` (plus the grace period while the main installation is unreachable) | A caller's token beyond the request: never on disk, never in Valkey (R-BAS-019) |
| Cached artifacts under their SHA-256, shared documents, negative entries, and per-caller documents in memory only (R-BAS-022 to R-BAS-024) | Any credential for the main installation's object store, PostgreSQL or upstream registries (R-BAS-030); the chart does not limit where its pods connect ([H-522](#h-522--the-chart-restricts-no-egress-of-the-bastion-pods)) |
| The signing key for its records in Valkey and the bucket (R-BAS-032) | A right to write Kubernetes Secrets or to issue tokens (R-BAS-006, R-BAS-045) |
| The cache key, when its chart gives one (`secrets.cacheKey`): every object of its cache store is then an object of the stored format under keys derived from it ([below](#with-a-cache-key), R-BAS-061; [stored-content-visibility.md](stored-content-visibility.md#h-1318--the-bastions-cache-shows-sizes-counts-and-times-and-stays-plain-without-a-key)) | Any key of the main installation's stored content ([stored-content-keys.md](stored-content-keys.md)) |
| With the handover switched on for its registration, for the length of one read: the link of one object of the main installation's store and, on an encrypted connection, that object's data key (R-BAS-070 to R-BAS-079, built; [bastion-handover.md](bastion-handover.md)) | An account of the main installation's store, a storage key, a name key or anything of the key ring; a data key it was not handed for a caller's fill |

Every decision about a caller is the main installation's. A miss is forwarded with the caller's own credential and decided by
the main installation's own route; a cached artifact is served only after a `full` read answer or a conditional request with the
caller's credential answered `304` through the real route (R-BAS-022, Q-BAS-27); a shared document only to a caller whose `full`
read the main installation confirmed (R-BAS-023). The Bastion carries no format knowledge, so there is no second classifier
whose disagreement with the main installation could serve bytes.

## What an attacker who reaches the Bastion can do

- **Without a credential.** Read what `anonymous` may read at the main installation, exactly when the main installation admits
  anonymous requests (R-BAS-019); otherwise every request answers `401` from the Bastion without reaching the main installation.
  Misses cost the attacker nothing, so they are absorbed: names the main installation does not have are answered from the
  negative cache to callers who read the whole repository (R-BAS-024), anonymous requests have stricter per-address limits and a
  per-address miss budget (R-BAS-035), and anonymous misses can hold only a share of the concurrency budget towards the main
  installation (R-BAS-025). Spread over many addresses, they still take that share (H-388). An anonymous token request is answered by
  the Bastion itself, and a document filtered for the anonymous caller is kept for the metadata period, so neither reaches the main
  installation per request (R-BAS-094).
- **With guessed or stolen tokens.** A malformed token is refused without a request (R-AUTH-022's checksum). More than 20 failed
  authentications from one address put it into a timeout in which unverified credentials are refused without a request, while
  known credentials from that address keep working (R-BAS-036). Behind the Bastion, the main installation still applies its own
  failed-credential throttle to the real client (R-BAS-017).
- **With a valid token.** Read what the token may read at the main installation, no more and no less: a request through a Bastion
  is decided as the same request at the main installation's door ([Q-BAS-148](../spec/bastion.md#q-bas-148--a-request-through-a-bastion-is-decided-as-any-other)).
  For a token of an internal caller - a personal token of a user not flagged external, a service account's technical token, a CI
  token - that is every `internal` repository besides its grants; a token of a user flagged external reads its grants and what is
  `public` ([visibility.md](visibility.md#what-a-leaked-credential-reads), H-884, H-1052). The shape of the answer is the decision
  point's (R-BAS-012), a narrowed reader never receives a shared document (R-BAS-023) except, once the read shapes per package are
  built, the document of a package it reads in full ([below](#read-shapes-per-package)), and a revocation at the main installation
  reaches every Bastion replica within the feed's poll interval and one round trip (R-BAS-026). During an outage of the main
  installation a revoked token reads on for the grace period (H-383).
- **Writes.** None reach the main installation: the Bastion refuses every write itself, `403 read_only` in the error shape the format
  declares, before it reads the body or the credential and without a request to the main installation (R-BAS-095, Q-BAS-452); a forward of
  a write that reaches the main installation anyway, a defect of the Bastion, is refused by the backstop of its marking stage.
- **Learn about others.** The Bastion sends no cache state (Q-BAS-40), and negative entries go only to callers who read the whole
  repository, and a `404` that is not `absent` is never kept for anybody, so a narrowed reader cannot time hidden names (Q-BAS-42, Q-BAS-124). The time of a hit still tells whether a readable package
  was fetched recently (H-390).
- **Flood it.** See [Where abuse is stopped](#where-abuse-is-stopped).

## What the Bastion token can do

The token has the type `bst` and belongs to a Bastion registration, a principal that holds no grants (R-BAS-004). Every route of
the main installation except the Bastion interface refuses it by type, before any lookup, as `Authorization`, Basic password or
API key (R-BAS-005).

| On the interface it can | It cannot |
|---|---|
| Run the handshake: learn the interface version, its own expiry, whether anonymous requests read anything (`anonymousReads`) and whether download recording is on | Read a package, a document or a file: on a forwarded request only the caller's credential decides |
| Check any credential and read its shape on named repositories, for a client address it names (R-BAS-011) | Write content, change a setting, read the audit trail, issue or rotate a token |
| Read the revocation feed: token IDs, principal IDs, qualified repository names `<projectKey>/<repoKey>` (R-BAS-015) | Reach any route on a further endpoint |
| Submit download events for its cached downloads, marked with its registration (R-BAS-016) | Submit any other audit event type |
| Make the main installation mark its forwarded responses and refuse a forwarded write (R-BAS-013, R-BAS-095's backstop) | Turn an anonymous forwarded request into anything but anonymous |
| Read its registration's configuration in the handshake (R-BAS-093) and, while the registration's web interface is on, its public catalogue (R-BAS-101) | Read another registration's configuration or catalogue, or a catalogue while `gui` is off |

While its registration is disabled (R-BAS-133) the token keeps the handshake, the feed and the download intake, and gets `503
bastion_disabled` for a forward, the check and the catalogue: the Bastion learns its disable over the line it keeps, and gets nothing new
([H-2443](#h-2443--a-disabled-bastions-token-still-answers-the-handshake-the-feed-and-the-download-intake)).

What its holder can still do with those rights is H-386. It is revoked like every token, journaled, and renewed by hand with an
overlap (R-BAS-006); the Bastion warns 30 days before it expires and alerts when the main installation refuses it (R-BAS-041).

## The registration and its token on the main installation

Built ([R-BAS-004](../spec/bastion.md#r-bas-004--bastion-registrations-and-the-bastion-token-on-the-main-installation),
[R-BAS-005](../spec/bastion.md#r-bas-005--what-the-bastion-token-can-do)). The token's whole security is in the type and in the registration
that owns it:

- **A registration holds no grant.** Every write that would give it a policy entry, a role, an ownership or a membership answers `422
  principal_not_allowed`, and the database refuses the rows when they are written around the API
  ([permissions.md](permissions.md#a-bastion-registration-holds-nothing)).
- **The token is refused everywhere but on the interface.** As `Authorization: Bearer`, as the Basic password and as `X-NuGet-ApiKey` it
  is `401 invalid_token` before any lookup; listing it in an endpoint's `auth.token_types` stops start-up
  ([tokens-and-credentials.md](tokens-and-credentials.md#bastion-tokens)).
- **An issued token always expires and is limited in number** (the declared token of [the next section](#a-registration-the-configuration-declares) does not, and counts against no limit). At most three unrevoked tokens per registration by default, so that an overlap fits and
  a forgotten token does not accumulate; the value is shown once and is in no event, no log line and no list.
- **Who manages it.** `endpoints:write` registers, issues, disables and deletes; `tokens:revoke` alone may revoke a token, so a
  security administrator can end a leaked token without managing endpoints. Every operation is an audit event with `resource_type`
  `bastion`, a revocation is journaled, and deleting a registration revokes its tokens first.
- **Disabling is not revoking.** A disable is a member of the registration's configuration ([Disabling a Bastion](#disabling-a-bastion)):
  its tokens stay valid, so the Bastion keeps its line and learns the disable within seconds. Revoking a token is the act that ends it,
  the last resort, and works on every token, the declared one included; nothing is deleted by a disable.

### A registration the configuration declares

Built ([R-BAS-127](../spec/bastion.md#r-bas-127--a-bastion-registration-declared-in-the-main-installations-configuration)). The main
installation's configuration may declare a registration and the value of its token (`bastions.declared`), so that it exists again after a
fresh database:

- **What the main installation's pods now read.** The migration Job alone mounts the Bastion's token Secret, read-only and optional, at
  `/var/run/secrets/quader/bastion-tokens/<name>/token` ([_bastions.tpl](../../deploy/helm/quader/templates/_bastions.tpl)); no api,
  packages, worker, scanner, backup or restore pod mounts it. `quader migrate` reads the file, checks the value (the issued format, at most
  1,024 bytes) and forgets it: its errors and the audit events name the token's hint at most, never the value.
- **Stored as an issued token is.** The database keeps the SHA-256 of the value and its hint, in `api_token` like every token, with
  `created_via: configuration`, and the registration points at it (`bastion.declared_token`). The configuration file and its ConfigMaps
  hold the path and the Secret's name, never the value.
- **Who can change it.** Whoever writes the Secret in the main installation's namespace (and whoever writes the values in Git) decides the
  Bastion's credential at the next rollout: they can rotate it, or put in the value of another Bastion they hold, which makes that
  Bastion speak as this registration. That is the right to write a Secret of the namespace, which already decides the database's and the
  key ring's credentials. On the API the declared members (description, networks), a token issued beside the declared one and the
  deletion answer `409 managed_by_config`: the Secret is the one source of the Bastion's credential. An administrator keeps the switches,
  the limits and the disable, and may revoke the declared token as the last resort against a leak (R-BAS-133): the next rollout then stops
  at its migration until the Secret holds a new value, so the revoked value does not come back.
- **What it does not loosen.** A revoked, expired or another registration's value stops the run: a token revoked because it leaked does not
  come back by configuration. The value must be a Bastion token in the issued format, so the routing by prefix (R-BAS-005), the Bastion's
  own check and the log redaction hold for it as for every Bastion token.
- **It does not expire** ([H-2366](#h-2366--a-declared-bastion-token-does-not-expire)), and a restore can leave the Bastion refused until the
  next migration ([H-2367](#h-2367--after-a-restore-the-declared-token-is-the-restore-points-until-the-next-migration)).

## The main installation's side of the interface

What the main installation does with a request that claims to come from a Bastion, in the order the request meets it
([R-BAS-008](../spec/bastion.md#r-bas-008--interface-routes-recognition-and-headers) to
[R-BAS-017](../spec/bastion.md#r-bas-017--the-real-client-and-the-bastions-host-at-the-main-installation)):

| Mechanism | What it guarantees | Code |
|---|---|---|
| Recognition | A request is a Bastion's only with a valid, enabled `bst` token in `Quader-Bastion-Authorization` (a forward) or `Authorization` (an interface call, which carries no `Quader-Bastion-Authorization`); every other `Quader-Bastion-*` header is ignored. The stages behind it get a copy of the request without the Bastion token, so neither the dispatcher nor a remote repository's outbound client can pass it on. | [recognize.go](../../internal/bastionmain/recognize.go) |
| Route-family backstop | A forward reaches only `/api/<format>/`, `/files/` and the registry root `/v2/` (R-BAS-051, whose token service, catalog and repository routes tell the marking their kind before they decide or read a body), judged on the path as the router decodes it segment by segment (`/api/%76%31/tokens` is `/api/v1/tokens`); anything else answers `404 not_found` before authentication, so a forward runs no route of the management API, the login tree or the UI with the caller's credential, and is never answered `bastion_token_invalid` for the caller's credential ([Q-BAS-115](../spec/bastion.md#q-bas-115--a-request-with-quader-bastion-authorization-is-a-forward-and-a-forward-reaches-only-the-package-families)) | [recognize.go](../../internal/bastionmain/recognize.go) |
| Forwarded headers | Each registration lists the networks its Bastion connects from (`egress`, at most 32, no `/0`); they are not in `server.trusted_proxies`. A Bastion's forwarded client address and host count only for a request whose Bastion token was recognized and that came from its own registration's networks; a request with that token from anywhere else - another registration's network included - and every request of a registration with an empty list is the Bastion's own request, from its address, and its request line says `bastion_egress: "unlisted"`; any request without the token from a Bastion's address - a workload sharing the egress - cannot choose its client address (rate limits, throttle, audit) or its response base URL ([Q-BAS-430](../spec/bastion.md#q-bas-430--a-bastion-is-recognized-by-its-token-its-reports-about-its-callers-count-only-from-its-own-registrations-networks), [Q-BAS-116](../spec/bastion.md#q-bas-116--the-bastions-forwarded-headers-count-only-for-a-recognized-request-from-an-address-of-bastionstrusted_proxies), [H-467](../spec/bastion.md#h-467--behind-the-main-installations-own-ingress-the-forwarded-host-is-what-the-ingress-reports)). The networks are read from the replica's snapshot of the registrations, never per request. | [recognize.go](../../internal/bastionmain/recognize.go), [egress.go](../../internal/bastionmain/egress.go), [snapshot.go](../../internal/bastionmain/snapshot.go), [wire_bastion.go](../../internal/app/wire_bastion.go) |
| Throttle of rejected Bastion tokens | The stage runs before the authentication stage, so it counts rejected `qdr_bst_` tokens against the address that sent them (a budget of its own, the size of R-AUTH-038's); the token service asks the throttle after its cache, so a throttled address is refused a token the replica does not hold without a lookup, and a known one passes ([Q-BAS-65](../spec/bastion.md#q-bas-65--the-recognition-admits-an-interface-route-on-its-own-proof-throttles-tokens-it-cannot-place-and-counts-the-bastion-in-the-read-class), H-440). `token.rejected` names the request's source; the token's last use records the Bastion's own address | [recognize.go](../../internal/bastionmain/recognize.go), [authenticator.go](../../internal/bastionmain/authenticator.go), [bastion.go](../../internal/auth/token/bastion.go) |
| Interface routes | Admitted on the recognition alone, as the registration, which holds no grant and is not internal in the decision point ([Q-AUTHZ-320](../spec/authorization.md#q-authz-320--a-bastion-registration-is-not-internal-a-bastion-reads-for-the-caller-it-forwards-for)): the Bastion reads nothing on its own credential; the registration's requests are charged to the read class under its token ([H-438](../spec/bastion.md#h-438--the-interface-calls-of-a-bastion-share-the-read-budget-of-its-token)) | [bastion_services.go](../../internal/api/v1/bastion_services.go), [index.go](../../internal/authz/index.go) |
| Write probe | Admitted as the anonymous caller for routing only, whatever the levels say; answered at the route match (`403 read_only` or `204`) before any decision, handler or body read ([Q-BAS-118](../spec/bastion.md#q-bas-118--a-write-probe-is-admitted-as-the-anonymous-caller-for-routing-only), [H-469](../spec/bastion.md#h-469--a-probe-admitted-for-routing-runs-any-handler-below-the-package-families-that-does-not-tell-the-marking-its-route)) | [bastion_services.go](../../internal/api/v1/bastion_services.go) |
| Check | The caller's credential is validated by the authentication stage of the default endpoint as a direct request from the caller's address: the endpoint's token types, the failed-credential throttle and `token.rejected` apply to that address, never to the Bastion's. A subject that is not usable gets no shapes; the shapes of a usable one are decided on the caller's own subject, as at the main installation's door, with no mark of the Bastion (Q-BAS-148). No `authz.denied` is written, and the request log line stays the registration's ([Q-BAS-119](../spec/bastion.md#q-bas-119--the-check-runs-the-credential-through-a-stage-that-records-nothing-in-the-request-log)). | [bastion_check_api.go](../../internal/api/v1/bastion_check_api.go) |
| Feed | Entries carry IDs and qualified repository names only. A cursor that is gone, from the future beyond the lookback or older than the audit retention, or too far behind, answers `reset`, never a truncated list. An event whose target cannot be read drops every answer. Every journaled event is either mapped or listed with the reason it narrows nothing a Bastion keeps, held to the audit registry by a test; moving a repository to another project and removing or re-roling a project member drop every permission answer ([Q-BAS-120](../spec/bastion.md#q-bas-120--unassigning-a-repository-from-a-project-drops-every-permission-answer-and-every-journaled-event-is-classified)). A restore makes the feed answer reset to a Bastion whose cursor lies before it, because the journal replay re-applies narrowings without writing audit events (R-REPO-061). A narrowed repository and a new claim end the cached answers of the repository and of the virtual repositories over it (`content`), a `global` claim every answer (`grants`), so a name that a member removal, a pattern or a claim took away is not served from a Bastion's cache for the entry's period ([Q-BAS-146](../spec/bastion.md#q-bas-146--a-content-narrowing-and-a-claim-of-scope-members-yield-a-content-entry-a-global-claim-yields-grants)). | [feed.go](../../internal/bastionmain/feed.go) |
| Download intake | Events are recorded as `artifact.downloaded` marked `origin: bastion` with the registration's name, a time in the future at the time of receipt; a lost, expired or malformed event is counted in `bastion.downloads.dropped`. A batch is charged to its registration by its events (`bastions.downloads.events_per_second`, in units of 100); beyond it the answer is `429` and nothing is recorded ([Q-BAS-117](../spec/bastion.md#q-bas-117--download-events-are-charged-to-their-registration-by-their-count), [H-468](../spec/bastion.md#h-468--the-download-budget-is-per-registration-so-many-registrations-at-their-budget-still-fill-the-audit-buffer)). The time the Bastion reports is trusted ([H-439](../spec/bastion.md#h-439--download-events-enter-the-audit-trail-with-the-time-the-bastion-reports)). | [downloads.go](../../internal/bastionmain/downloads.go) |
| Last contact | Written at most once a minute per replica, at most 256 rows per registration; a change of a reported value is `bastion.contact`. The values are what the Bastion says about itself, except the address, which is the one the main installation saw. | [contact.go](../../internal/bastionmain/contact.go) |

`Quader-Bastion-Authorization` is redacted like `Authorization` in every log, span and support bundle: the key rule of R-OPS-013 matches
the word `authorization` in a header name (tested in [redact_bastion_test.go](../../internal/observability/redact_bastion_test.go)). No
`Quader-Bastion-*` request header is echoed; the one header on an answer to a Bastion request is the interface version.

### Who may speak as a Bastion

Two proofs, held apart (built with package E13 on 2026-11-26,
[Q-BAS-430](../spec/bastion.md#q-bas-430--a-bastion-is-recognized-by-its-token-its-reports-about-its-callers-count-only-from-its-own-registrations-networks)):

```text
                       valid bst token of X?
                        │no                 │yes
                        ▼                   ▼
             an ordinary request     X's request (marking, interface routes)
             forwarded headers by           │
             server.trusted_proxies         ▼   came from a network of X's own egress?
             only                     │no                          │yes
                                      ▼                            ▼
                        X's own request, from its      the caller's address and host X
                        address; "unlisted" in the     forwards count, and its check's
                        request line                   clientAddress
```

| Who | May |
|---|---|
| A holder of X's token, from anywhere | be X: its interface routes, its marked forwards, its handshake (the contact records the address it came from) |
| A holder of X's token, from a network X's registration lists | also speak for X's callers: their address for the limits, the throttle, the blocks and the audit, and the host documents are rendered for |
| Anyone else at a Bastion's address | nothing of the above; a request without the token is an ordinary request from that address |
| A holder of `endpoints:write` | change X's networks on its card or by `PATCH` (`bastion.updated` records the lists before and after) |

A token of X used from Y's network is X's request and speaks for no caller: one registration's network never lends its trust to another
registration's token. What stays open is a network listed too widely ([H-2291](#h-2291--an-egress-list-that-no-longer-names-a-bastions-address-pools-its-callers-silently)),
a network brought back by a restore ([H-2348](#h-2348--a-restore-brings-back-a-registrations-networks-as-they-were-at-its-point-in-time)) and a
network also in `server.trusted_proxies` ([H-2349](#h-2349--a-bastions-network-that-servertrusted_proxies-covers-is-trusted-for-every-request-and-only-logged)).

## Where abuse is stopped

| Stage | What it knows | What it does | Requirement |
|---|---|---|---|
| Network provider | bandwidth | absorbs volumetric attacks, or does not (H-382) | outside Quader |
| HAProxy Ingress (stage 1, switchable) | the client address | a per-address request and connection limit, set wider than the Bastion's | R-BAS-038 |
| Bastion block list (stage 2) | refusals per address, shared through Valkey | blocks an address that collects too many refusals, with durations doubling to 24 h; IPv6 per `/64` | R-BAS-037 |
| Bastion limits | credential or not, misses, failures | per-address request rates (anonymous stricter), per-address miss budgets, the failed-authentication timeout | R-BAS-035, R-BAS-036 |
| Concurrency budget | requests to the main installation | at most `max_concurrent` across replicas, anonymous at most a share; collapsed misses | R-BAS-025 |
| Cache room | the stored bytes of each area, from its listings and its own commits | a fill that would take its area past `max_bytes` is not kept, and a sweep starts at once above 95 %, so a flood of distinct artifacts cannot fill the cache volume or get the pod evicted; what was served twice is evicted after what was served once, within 80 % of an area, so single requests for distinct artifacts do not evict what others read twice (H-2272, H-2290) | R-BAS-089, R-BAS-098 |
| Main installation | the real client, through the registration's own networks (`egress`) for a recognized Bastion request | its own rate limits, throttle and audit, applied to the client, not to the Bastion; with an empty list, or from an unlisted address, to the Bastion's address for all its callers (H-2291) | R-BAS-017 |

Without Valkey every counter and block holds per replica at the configured value divided by the chart's replica count (H-389).

**What the guard does with an address** ([guard](../../internal/bastion/guard/instance.go), R-BAS-035 to R-BAS-037):

- The address is the one R-ARCH-006 yields with the Bastion's own `server.trusted_proxies`; an IPv6 address counts as its `/64`, so one
  host that owns a whole prefix is one address. `exempt_cidrs` are never limited, timed out or blocked: an entry is a promise that nothing
  behind that network abuses the Bastion.
- Each refusal of a request limit, a miss limit or a timeout counts toward one threshold. Above it the address is blocked, for 15 minutes
  and twice as long for each further block within 24 h, up to `max_duration`. A blocked address costs the Bastion one comparison in memory
  and is answered `429 address_blocked`; its requests are not logged one by one, the block is, once.
- The failed-authentication timeout refuses only a credential for which the Bastion holds no usable answer; a partner's known token
  behind the same address keeps working, and the guesser keeps anonymous access (Q-BAS-36). It cannot be used to cut a colleague off: the
  count is of the unverified credentials the address sent.
- A limit admits up to its rate and a refused request or miss consumes nothing, so a client slightly over its limit is not starved and does not
  collect the refusals that block (Q-BAS-127; before, at 330 misses a minute against 300 no miss was admitted from the second minute and the
  address was blocked for 15 minutes, doubling to 24 hours, with its hits, which cut off every partner behind a shared address). A flood of several
  times the limit still reaches the threshold.
- **The defaults and what a flood can still do** (R-BAS-092, set from the measurement of real clients so that one normal command, and two
  at once from one address, never meet them). Anonymous: 50 requests a second with a burst of 1,000 and 1,000 misses a minute; with a
  credential: 200 a second with a burst of 4,000 and 4,000 misses a minute; a block after 3,000 refusals a minute; at the ingress
  1,000 HTTP requests a second and 256 in flight. One address is admitted at most 4,000 anonymous requests in its first minute and 3,000 in
  each further, sends at most 1,000 anonymous misses a minute toward the main installation, and is blocked within about four seconds when it
  floods at the ingress's 1,000 a second. A thousand addresses are not bounded by these figures, which are per address: by count they could
  send 3,000,000 anonymous requests and 1,000,000 misses a minute (600,000 and 30,000 before). What bounds them is the Bastion's capacity
  (`server.max_in_flight` per replica, hits served from the cache), the network provider (H-382), and toward the main installation the
  concurrency budget, which lets at most 16 anonymous requests of a Bastion of two replicas reach it at once and answers the rest
  `503 busy` after 2 s; each forward is charged to its caller at the main installation (R-BAS-017). Raising the per-address figures
  therefore widened what one address can do, not what the main installation receives from many.
- The address the guard counts is only the client's when the chart's `config.server.trusted_proxies` names the ingress controller's pods; the
  chart refuses a render without it while its Ingress is on (Q-BAS-130). Before, every caller shared the controller's address by default: one
  client's bad tokens put every unverified credential into the timeout and a flood blocked the Bastion for everyone.
- Without Valkey the maps of the counters, buckets and blocks are bounded at 100,000 keys each, sharded, and evict the least recently used entry in
  constant time; a flood of addresses from an IPv6 /47 neither scans a map under a lock every request takes nor holds a lock across Valkey round
  trips (Q-BAS-128).
- The guard never refuses on its own fault. If the shared state fails, the request is admitted; with Valkey unavailable the
  counters and blocks work from memory at the configured value divided by `bastion.replicas`, and `quader_ratelimit_fallback` and
  `quader_valkey_available` say so (Q-BAS-94).
- With Valkey the block list, the timeouts and the counters are shared: a block set by one replica holds on every replica at its next
  request. What Valkey holds is chosen by whoever can write to it; the answers are signed (below), the blocks and counters are not.
  Write access to the Bastion's Valkey therefore lets its holder block an address or reset a counter, which is a denial or a missing limit
  and no grant of access; the ACL rule of the Bastion's user confines it to `quader-bastion:*`.
## What the main installation tells the Bastion about a response

The main installation marks the response of a recognized Bastion request (R-BAS-013) and refuses a forwarded write as R-BAS-095's
backstop; a Bastion sends none. The code is
[internal/api/bastionmark](../../internal/api/bastionmark/mark.go); the lookups that feed `absent` are in `internal/formathost` and
`internal/files`.

- **Only a recognized Bastion sees the marking.** The headers are added to the response of a request that carries a valid Bastion token
  and stripped from every other response, a header a handler set included. A caller learns its own shape from nobody. This is held for the
  default endpoint's chain, the only one that carries the stage; the other endpoints add no such header (nothing sets one) and
  the remote engine copies a fixed list of upstream headers.
- **`absent` is never a hidden item.** A negative entry is replayed to full readers only (R-BAS-024). The class `absent` therefore
  needs a `full` shape, a lookup that found the item missing for every subject, and no refusal, pattern refusal, serve-time check or
  remote member that could not tell on the way; every other `404` is `pass` ([Q-BAS-70](../spec/bastion.md#q-bas-70--absent-is-derived-from-the-notes-of-the-cores-lookups-and-only-for-a-full-reader)).
  The residual risk is [H-441](../spec/bastion.md#h-441--absent-rests-on-the-notes-of-the-cores-lookups-not-on-the-routes).
- **`full` is checked against the decision point.** A full shape never meets a refused read: `TestR_BAS_012_ShapeAgreesWithTheDecisionPoint`
  asks the decision point for every read of its fixture, the wiki of every package included, and `content:metadata-read` alone, a scope
  that narrows, exposure rules and a missing populate right are all `partial`. So are a wiki read and a fill of a frozen mirror that only a
  grant or a scope narrowed by package allows: until the build of R-BAS-080 (2026-11-22) both were asked on a coordinate without a package,
  which such a grant matches, and the repository shape was `full` while the wiki or the fill of another package was refused. A package
  shape (R-BAS-080, `PackageReachOn`) is held the same way by `TestR_BAS_080_PackageShapeAgreesWithTheDecisionPoint`.
- **A shared document is the same for every full reader.** Kit check 12 sends each `Metadata` and `Computed` document as a user, a
  service account and the anonymous caller, each with its own request context and its own credential, client, session and forwarded
  headers except those the answer names in `Vary`; npm's `-/whoami` and `-/ping`, which differed, are `Volatile`.
- **A refused read says nothing about the caller's shape.** The marking learns the decision only once a read was allowed, on `/files/` as
  on the format routes, so a refusal carries no `Quader-Bastion-Read`.
- **The Bastion token cannot write.** A write route (kind `Upload`, `Delete`, `Admin`, or an action other than `read`) is refused before any
  handler runs and before a body is read, as a probe and as a forwarded request; the refusal names the main installation's URL of the
  repository and no secret. `GET` and `HEAD` on those routes are decided as the caller's own request. The probe gets there with anonymous
  access off as well: it is admitted for routing only.
- **The subject header carries IDs and an expiry.** `token=<token ID>; principal=<principal ID>; expires=<unix seconds>`, `anonymous` or
  `invalid` (every `401`, a session cookie): no name, no group. [H-442](../spec/bastion.md#h-442--a-401-states-the-subject-as-invalid-whether-the-credential-was-rejected-or-none-was-sent)
  records what `invalid` does not say.

## What the request flow enforces

These are properties of [internal/bastion/engine](../../internal/bastion/engine); each has a test named for its requirement.

| Property | Where | Test |
|---|---|---|
| A path outside `/api/<format>/<projectKey>/<repoKey>/…` and `/files/<projectKey>/<repoKey>/…`, a project or key that fails INV-5's rule, and a path with one segment after the format, is refused before any request | `stepPath`, `classifyPath` | `TestR_BAS_003_OnlyThePackagePathFamiliesReachTheReadFlow` |
| The same repository key in two projects is two repositories: the read-shape answer is keyed by credential and the qualified name `<projectKey>/<repoKey>`, so a caller's answer for one is never used for the other | `shapeKey`, `classifyPath` | `TestR_BAS_018_TheAddressedRepositoryIsTheQualifiedName`, `TestR_BAS_018_ARepositoryOfAnotherProjectIsAnotherRepository` |
| A malformed token, a `bst` token and an unknown prefix are refused without a request and counted as failed authentications; `X-NuGet-Apikey` and cookies are never read as credentials | `parseCredential` | `TestR_BAS_019_AMalformedTokenAnswers401WithoutARequest`, `TestR_BAS_019_TheNuGetAPIKeyIsNeverAReadCredential` |
| No answer is keyed by a token: the key is the SHA-256 of the token string, and the string lives in memory for the request | `credentialKey`, H-447 | `TestR_BAS_019_TheCredentialKeyIsTheSHA256AndNoTokenIsKept` |
| A cached artifact or document is served only to a caller with a usable `full` answer or a usable URL answer `allowed`; every answer is judged at each use by the four rules of R-BAS-020, so one obtained before a revocation is never used after it, in memory or from Valkey | `fromEntry`, `answerStore.get` | `TestR_BAS_020_AnAnswerObtainedBeforeARevocationIsNeverUsedAfterIt` |
| A forged or moved answer or negative record in Valkey is ignored and deleted by the store that signs and verifies them (the engine hands over plain records and never sees one that failed); an entry rewritten in the bucket is ignored and deleted by the cache | the Valkey `AnswerStore` (Q-BAS-93), `Cache.LookupEntry` through `loadEntry` | `TestR_BAS_034_*` in `test/integration/bastionshared`, `TestR_BAS_032_AnEntryRewrittenToPointAtOtherBytesIsIgnored`, `TestR_BAS_020_AnswersAreSharedThroughTheSharedStoreAndAnUnreadableRecordIsIgnored` |
| A document filtered for a caller is kept in memory only, under that caller's key, and validated with a conditional request on every use; a narrowed reader never receives the shared document and never gets a negative entry; the one exception is a reader the main installation itself validated: its `304` to the shared entry's validator proves that the caller's document is those bytes, and they are served without renewing the entry (Q-BAS-107) | `onPrivate`, `fromNegative` | `TestR_BAS_023_APartialReaderNeverReceivesTheSharedDocumentAndTheSharedEntryStaysUntouched`, `TestR_BAS_024_APartialReaderAsksTheMainInstallationEveryTime` |
| The headers of the interface, the replica header, `Cache-Status`, cookies and the hop-by-hop headers never reach the caller, and the caller's cookie, conditionals, range and forwarded-for headers never reach the main installation | `replayHeader`, `stripRequest` | `TestR_BAS_021_AMissIsForwardedWithTheCallersCredentialAndTheListedHeaders`, `TestR_BAS_021_TheCallerReceivesTheMainInstallationsAnswerWithoutTheInternalHeaders` |
| A write sends the main installation a probe without body and credential; the upload is not read | `write` | `TestR_BAS_014_AWriteIsProbedWithoutBodyAndCredentialAndTheRefusalIsReplayed` |
| Below `/v2/` only the core routes and `/v2/<projectKey>/<repoKey>/…` (on a bound host every path, as the main installation reads it) are classified; any other path answers the Bastion's own `404 NAME_UNKNOWN` without a request; the version check without a credential is the Bastion's own `401` | `classifyRegistry`, `versionChallenge` | `TestR_BAS_051_TheRegistryRootIsAPathFamilyOfTheBastion`, `TestR_BAS_057_ABoundHostIsClassifiedAsItsRepository` |
| The token service's answer is passed on and never kept, collapsed or put into an answer; the form of `POST /v2/_token` is read only for its password's syntax and slot class, at most 16 KiB | `passCore`, `onPassOnly`, `stepTokenForm` | `TestR_BAS_051_TheRegistryRootIsAPathFamilyOfTheBastion` |
| The Galaxy clients' `Authorization: Token` is read on `/api/<format>/…` only, checked like any token and forwarded unchanged; its answers are keyed apart from the same token's under Bearer or Basic, so the main installation's refusal of the scheme on one route is never the token's answer on another | `parseCredential` | `TestR_BAS_019_TheGalaxyTokenSchemeIsACredentialOfTheProtocolFamily` |
| A registry token is a credential below `/v2/` only, checked by its form; one that names the anonymous principal is forwarded without `Authorization`; its read shape is recorded at most `partial` whatever the main installation marks; `insufficient_scope` keeps no `invalid` answer and counts nothing | `parseCredential`, `capRead`, `insufficientScope` | `TestR_BAS_052_RegistryTokensAtTheBastion` |
| A cached answer replays the core's representation headers, the content headers of R-ARCH-007 among them, and the headers its format declares; never a per-request header of the core, a cookie or a header of the interface, whatever a format declares | `keepHeader` | `TestR_BAS_053_TheEnginesHeaderListsAreTheCatalogs`, `TestR_BAS_022_AHitCarriesTheContentHeadersOfRARCH007`, `TestR_BAS_053_ContainerImagesFollowTheRouteKinds` |
| A caller of a running fill is served from its spool only after its own permission (a usable `full` answer, a check, or a conditional request answered `304`); the last 64 KiB of what it asked for wait for the commit, and a fill that fails cuts every follower | `followFill`, `streamFill` | `TestR_BAS_054_LargeArtifacts` |
| The main installation unreachable: cached content for the answers the grace period allows, `503` for everything else, nothing after the grace period; a token past its expiry is refused throughout | `answers.State`, `unreachable` | `TestR_BAS_027_AnswersHeldServeCachedContentForThePeriodAndTheGraceThenNothing`, `TestR_BAS_020_ATokenIsRefusedFromItsExpiryOnAlsoWhileTheMainInstallationIsUnreachable` |
| Requests to the main installation are bounded: the budget per replica with a share for anonymous callers, held until the response body ends or is closed, the per-address miss limit, one fetch per key | `mainclient.SlotBudget`, `forward`, `miss`, every `resp.Body.Close` | `TestR_BAS_025_MissesNeverPutMoreThanTheBudgetOntoTheMainInstallation`, `TestR_BAS_025_EveryPathClosesTheResponseBodyAndSoReleasesItsSlot`, `TestR_BAS_035_AMissLimitedAddressKeepsGettingItsHits` |
| A `HEAD` makes the Bastion send a `GET` only after the main installation answered that `HEAD` `200` for this caller and marked it a document (`shared` or `private`) whose declared length is within the memory bound of its class; the `GET` carries the caller's own credential, so it reads nothing the `HEAD` did not, is kept by the same shared-or-private rule as the caller's own `GET`, and is charged as a forward (a miss of the address, an upstream request, a slot, the anonymous ceiling) after the `HEAD`'s slot is freed; a `HEAD` of an artifact is never sent as a `GET` (Q-BAS-475 to Q-BAS-478, [H-2374](#h-2374--a-head-makes-the-bastion-read-a-document-its-caller-never-receives)); since interface version 9 the `GET` says that it answers a `HEAD` (`Quader-Bastion-For-Head`), and the main installation records no download for it on a document's route ([Q-BAS-479](../spec/bastion.md#q-bas-479--a-get-a-bastion-sends-for-a-head-says-so-in-a-header-of-the-request), [H-2375](#h-2375--the-get-of-a-head-is-a-download-at-the-main-installation), closed) | `fillForHead`, `admitFill`, `headAsGet`, `rq.asGet` | `TestR_BAS_059_OnlyTheGetOfAHeadSaysItIsForAHead`, `TestR_BAS_021_AHeadMissOfADocumentIsFilledAsAGetAndAnsweredFromIt`, `TestR_BAS_023_AHeadOfADocumentKeepsTheCopyOfACallerWithoutFull`, `TestR_BAS_021_AHeadOfADocumentAboveTheMemoryBoundStaysAHead`, `TestR_BAS_021_AHeadMissOfAnArtifactIsForwardedAsHeadAndKeepsAnswersOnly`, `TestR_BAS_035_TheGetOfAHeadIsAMissAndARefusedOnePassesTheHeadOn` |
| Before the first handshake the replica does not know whether anonymous requests are admitted, and answers them `503` rather than admit them or call the access off (Q-BAS-99) | `stepCredentials` | `TestR_BAS_019_AnAnonymousRequestBeforeTheFirstHandshakeIs503` |
| A request from a blocked address is counted and refused, and kept out of the request log, so a flood does not fill the log; the guard logs one line per block | `Verdict.Quiet`, `engine.WithQuietSlot`, `server.requestLog` | `TestR_BAS_037_ARequestFromABlockedAddressIsCountedButNotLoggedOneByOne` |

## Integrity of what it serves

Artifacts are stored under their SHA-256, verified when they are filled (the withheld tail of R-REPO-041) and again while they are
served (R-STOR-013's verifying reader): a replaced object aborts the transfer and is removed (R-BAS-032). Entries, negative entries
and answers in the bucket and in Valkey are signed with HMAC-SHA-256; write access to either without the key only deletes. With the
key, it forges (H-387).

What each layer of the cache holds, and what protects it, without a cache key (the default; with one, [below](#with-a-cache-key)):

| Held | Where | Protected by | What write access without the key does |
|---|---|---|---|
| Entries (content address, size, headers, repository, `validatedAt`) | `entries/` | HMAC-SHA-256 over every field and the cache key (`cache.Signer`); a record that fails is ignored, deleted and counted as `stale`. **Without a key** (the volume backend with no Valkey, Q-BAS-98) the entry is plain JSON and nothing protects it (H-460) | Deletes; cannot point an entry at other bytes, move it to another key, or extend its validity. Without a key it can do all three |
| Artifact and document bytes | `sha256/`, `documents/` | the content address, checked while served by a verifying reader that withholds the last 64 KiB until the SHA-256 and the length match | Replaces: the next whole read aborts, deletes the object and the entry, and counts `quader_bastion_cache_corrupt_total`; the next request refetches. A range is answered through the verifying read of the whole object (Q-BAS-82, H-449), so it is verified too; the replacement stays until read (H-453) |
| Access markers, modification times | `access/` | nothing | Steers the order of eviction (H-453) |
| The audit spool | `audit-spool/` | nothing; the files are events the main installation receives as reports of the Bastion's own token | Deletes events or adds events marked with the Bastion's registration (H-386's capability, from the bucket) |

A fill is spooled to a temporary directory of the volume or the pod's `tmp` and committed under its address only after its digest
was checked (`cache.Fill.Commit`), and the engine releases the last 64 KiB of the transfer to the caller only after that, so a reader never sees a partial
object and a caller never receives a complete corrupt one; a spool that fails during the transfer cuts it, so bytes are never passed on unverified (Q-BAS-123); the backend checks the digest and size of every `PutObject` once more. Every
name that becomes a path or a key (content address, key hash, replica, spool file) is validated as hex or a fixed form before use.
The bucket is reached over `https` with the trust bundle of the chart, and its credential is a file the replica re-reads.
An answer record that Valkey returns is verified by the store (not by the engine) against its kind and key before anything else sees it: a record that is altered,
unsigned, signed with another key or moved to another key is not returned, is deleted and is counted as `bad_signature` (Q-BAS-93). The
engine still judges the content with the validity rules of R-BAS-020, so a valid old record cannot outlive the revocations received after it.

### With a cache key

Built ([R-BAS-061](../spec/bastion.md#r-bas-061--the-cache-encrypted-in-the-stored-format), decisions Q-BAS-212 to Q-BAS-215; code
[crypt.go](../../internal/bastion/cache/crypt.go), [sealed.go](../../internal/bastion/cache/sealed.go),
[foreign.go](../../internal/bastion/cache/foreign.go)). With `secrets.cacheKey` the replica reads the key file at start, derives a storage key,
an identifier `bc-<16 hex>` and a name key from it, and keeps every object below `<prefix>/bc-<id>/`. Every object but the empty access
markers is an object of the stored format of [stored-content-format.md](stored-content-format.md) with a data key of its own, sealed and read
by `internal/blob/blobcrypt` alone; its record lies in front of the body, because neither backend keeps object metadata (Q-BAS-212). The
layout family by family is on the [developer page](../developer/bastion.md#the-cache-backends-package-5).

| Held | With a cache key it is | What write access to the store without the cache key does |
|---|---|---|
| Artifact and document bytes | encrypted, named by an HMAC of their digest under the name key; read as the copy of their content address, so the record must bind that digest before a byte leaves; then the verifying reader as without a key | Replaces or moves: refused at the record or at the first changed segment, before a byte of it leaves; the object and its entry are deleted and counted in `quader_bastion_cache_corrupt_total`. A range authenticates the segments of its window ([H-451](../spec/bastion.md#h-451--a-ranged-read-of-a-cached-artifact-is-not-verified) narrowed) |
| Entries | encrypted, their plaintext the object's name and the signed entry, named by an HMAC of the cache key's hash | Moves or rewrites: refused, deleted, counted as `stale`. The binding to the name authenticates an entry also where no signing key is configured, so [H-460](../spec/bastion.md#h-460--without-a-signing-key-the-entries-on-the-volume-are-not-authenticated) is closed while a cache key is set, and pointing an entry at other bytes needs the cache key besides the signing key ([H-387](../spec/bastion.md#h-387--the-signing-key-and-write-access-to-the-bastions-valkey-or-bucket-together-forge-answers-and-entries)) |
| Access markers | empty objects named by their object's keyed name | Steers the order of eviction as without a key ([H-453](../spec/bastion.md#h-453--objects-and-markers-are-not-signed-a-replaced-object-is-found-only-when-it-is-read-and-markers-steer-eviction)) |
| The audit spool | encrypted and bound to its name `s/<replica>/<unix ms>` | Deletes events; a forged or moved file is refused, deleted and counted corrupt, so events can no longer be made up through the store (H-386's capability through the bucket); a copy of a genuine file put back is delivered again, as [H-444](../spec/bastion.md#h-444--download-events-spooled-by-a-draining-replica-can-be-delivered-twice)'s duplicates are |
| An object of another key or another tree put into the tree | refused for its key identifier (`unknown_key` counts as a refusal here: the tree holds only what its key wrote) | Deleted when read |

A holder of the store learns sizes, counts, times, the key's identifier and the replica names of the spool's files
([H-1318](stored-content-visibility.md#h-1318--the-bastions-cache-shows-sizes-counts-and-times-and-stays-plain-without-a-key)). The key file is
read once; no error, log line or metric names a byte of it, and a `sealer` renders its identifier only. With the volume backend the fills in
progress are spooled on the pod's temporary volume, so the cache volume holds ciphertext only (Q-BAS-214). A new key is a new tree: the old
one is a miss, and the sweeps of the new replicas' first hour delete it and the plain layout, and a cache without a key deletes every tree
(Q-BAS-215). What the key does not protect: a taken-over Bastion pod, which holds the key
([H-1327](../spec/bastion.md#h-1327--a-taken-over-bastion-pod-holds-the-cache-key)); the old tree after a key change until it is swept
([H-1326](../spec/bastion.md#h-1326--after-a-key-change-the-old-tree-stays-readable-to-a-holder-of-the-old-key-until-the-sweep-has-deleted-it));
the answers in Valkey, which stay signed only; and the download events a draining replica spooled under the old layout during a key change
([H-1325](../spec/bastion.md#h-1325--a-change-of-the-cache-key-loses-the-download-events-spooled-under-the-old-layout)).

## The link to the main installation

The client of the main installation, the standing link and the download buffer are built ([developer/bastion.md](../developer/bastion.md#the-link-to-the-main-installation-package-4a));
this is what they do about the security of the connection and of what flows over it.

| Concern | Mechanism | Code |
|---|---|---|
| Where the token and the callers' credentials go | `https` only; the trust is the system roots plus `bastion.main.ca_file`, read again without a restart; a redirect is returned, never followed, because the Bastion token travels in a header that no redirect policy strips; the address policy of upstream clients is not applied to the main installation (H-446) | [client.go](../../internal/bastion/mainclient/client.go) |
| What a forward sends | the caller's `Authorization` as it came; the Bastion token in `Quader-Bastion-Authorization`; `Cookie`, `Range`, `If-*`, `Forwarded`, `X-Forwarded-*`, `Quader-*` and trace headers of the caller removed; exactly one client address in both families, replacing the caller's; a write probe carries no credential of the caller and no body | [client.go](../../internal/bastion/mainclient/client.go) |
| What is logged | no header is logged; errors of the client name no URL query and no header; the token file's content is in no error; a token appears as its ID (`token_id`) and as a hint that the log scrubber already reduces to `qdr_bst_[REDACTED]` (R-OPS-013) | [reload.go](../../internal/bastion/mainclient/reload.go), [link.go](../../internal/bastion/link/link.go) |
| Rotation of the Bastion token | the file is re-read every `token_reload_interval` and when its identity changes; a malformed or vanished file keeps the token in use, is logged as an error and counted as refused; a revoked token in use makes the replica unreachable and sets `quader_bastion_main_token_refused`, when the answer to a call of the interface's own says so; the same answer to a forward moves nothing (Q-BAS-121) | [reload.go](../../internal/bastion/mainclient/reload.go) |
| Revocations | every replica polls the feed and records each entry with its receipt time; an answer is refused if a matching entry was received after it was asked for, whatever the order in which the answer and the entry reached the replica (R-BAS-020 rule 3); a reset or a start without history refuses every older answer | [link.go](../../internal/bastion/link/link.go), [answers](../../internal/bastion/answers/answers.go) |
| The main installation cannot be reached | the replica is unreachable after a failed handshake, a refused token, a version mismatch, a base URL the main installation does not honour, or `unreachable_after` without a successful poll; only then do held answers live on for the grace period (H-383), a token the replica has not seen gets a `503`, and nothing is served after `period + grace_period` | [link.go](../../internal/bastion/link/link.go) |
| A main installation that does not know the Bastion's host | the handshake's response base URL must equal `server.base_url`, so no client receives a document with the main installation's URLs | [link.go](../../internal/bastion/link/link.go) |
| The concurrency budget | `ceil(max_concurrent / replicas)` slots per replica, anonymous callers at most their share; a slot is held until the response body ends, and no client's speed decides when that is: a fill reads at the main installation's speed, a pass-through is cut below 64 KiB per second after 30 s or when a chunk takes 10 s (Q-BAS-123, H-471) | [budget.go](../../internal/bastion/mainclient/budget.go), [fill.go](../../internal/bastion/engine/fill.go) |
| Download events | a bounded memory buffer; the excess is counted and reported in the audit; at a drain the buffer is sent or written to the cache backend's audit spool, which any replica delivers (H-444) | [reporter.go](../../internal/bastion/downloads/reporter.go) |

## Findings of the review of the engine, the client and the link

A security review with proofs of concept found the defects below on the Bastion's own side; each is closed by a decision of the spec page and a test that goes through the code path of the proof. The
gaps that remain are the H-items in [What this does not cover](#what-this-does-not-cover).

| Finding | What it allowed | Closed by | Test |
|---|---|---|---|
| P1: an encoded `v1` in the path | `GET /api/%761/users` reached the management API through the Bastion; `GET /api/%761/bastion/handshake` made the main installation answer `401 bastion_token_invalid`, which the Bastion read as a refusal of its own token: one request from anyone made the replica unreachable | the path is classified by what the main installation's router will read, an encoded family, format or key segment and a dot segment are refused ([Q-BAS-121](../spec/bastion.md#q-bas-121--a-path-is-classified-as-the-main-installations-router-will-read-it-and-a-token-refusal-on-a-forward-is-no-statement-about-the-bastions-token)); a token refusal counts only on a call of the interface's own | `TestR_BAS_003_R_BAS_018_APathIsClassifiedAsTheMainInstallationsRouterWillRouteIt`, `TestR_BAS_003_R_BAS_006_AnEncodedSpellingOfTheInterfaceRoutesCannotTurnTheReplicaUnreachable` |
| P2: a withdrawal while unreachable | an artifact withdrawn through the feed was served on while the replica was unreachable and its link still read the feed | a `content` entry ends the service of an entry also then ([Q-BAS-122](../spec/bastion.md#q-bas-122--a-content-entry-ends-the-service-of-a-cached-entry-also-while-the-main-installation-is-unreachable)) | `TestR_BAS_022_R_BAS_027_AWithdrawnArtifactIsNotServedWhileTheMainInstallationIsUnreachable` |
| P3, B4: slots and waiters held by slow clients | seventeen connections that took their tarball one buffer at a time held every anonymous slot; a second caller of the key waited without bound | the fill does not wait for the client, the flight ends at the commit, a waiter is bounded, a pass-through has a floor ([Q-BAS-123](../spec/bastion.md#q-bas-123--the-fill-reads-the-main-installation-at-its-own-speed-and-no-client-holds-a-slot-or-a-waiter)) | `TestR_BAS_025_ClientsThatReadSlowlyDoNotHoldTheBudgetSlots`, `TestR_BAS_025_AWaiterOfACollapsedFetchIsNotHeldByTheLeadersClient` |
| P4: the `404` oracle | a narrowed reader's second request for a name was answered from memory when the name is cached and hidden, and from the main installation when it does not exist | a `404` that is not `absent` is never kept ([Q-BAS-124](../spec/bastion.md#q-bas-124--a-404-that-is-not-marked-absent-is-never-kept-as-a-url-answer)) | `TestR_BAS_024_ANarrowedReaderCannotTellACachedHiddenNameFromAMissingOne` |
| B2: the probe's slot | with `anonymous_share: 0` every write refusal answered `503` | the probe takes the class of the caller | `TestR_BAS_014_R_BAS_025_AProbeTakesTheSlotClassOfTheCaller` |
| B10: a mark of one caller | a caller without rights could keep collapsing off for a popular key | a mark that depends on the caller's rights is the caller's ([Q-BAS-125](../spec/bastion.md#q-bas-125--a-result-that-depends-on-the-callers-rights-switches-collapsing-off-for-that-caller-only)) | `TestR_BAS_025_ARefusalOfOneCallerDoesNotSwitchCollapsingOffForOthers` |
| B3, B7, B8, B11: the link | a half-read start counted as the start of the history; a burst page of 1,000 entries cost 1,000 passes over the answers; a black-holed call held the tick for `response_header_timeout`; IDs forgotten before the feed stopped re-sending ([Q-BAS-126](../spec/bastion.md#q-bas-126--the-link-applies-a-page-as-a-unit-ends-the-start-read-at-the-end-of-the-feed-and-bounds-each-call), [Q-BAS-152](../spec/bastion.md#q-bas-152--the-id-of-an-applied-entry-is-kept-until-a-read-behind-which-it-lies-no-longer-returns-it)) | see the decision | `TestR_BAS_026_TheHistoryStartsOnlyWhenTheStartReadReachedItsEnd`, `TestR_BAS_026_AFeedPageOfAThousandEntriesIsAppliedAgainstTwoHundredThousandAnswersWithinTheBound`, `TestR_BAS_027_ACallThatTheMainInstallationNeverAnswersIsCutAtUnreachableAfter`, `TestR_BAS_026_AnAppliedIDIsKeptWhileTheFeedSendsItsEntry`, `TestR_BAS_026_AQuietFeedThatReSendsAnEntryPastItsLookbackDoesNotApplyItAgain` |
| B1 (mirror): a `304` marked `shared` against the caller's private copy | the caller, who sent no condition, received an empty `304` | the copy's bytes answer as a `200` | `TestR_BAS_023_A304SharedAgainstThePrivateCopyAnswersTheCallerWithTheCopysBytes` |

## Every format, the registry root and large content

The owner's decision of 2026-11-22 that a Bastion serves every format as the main installation does
([spec/bastion.md](../spec/bastion.md#every-format), R-BAS-050 to R-BAS-060). Both sides are built. The main installation's side: its
backstop admits `/v2/`, a registry token's shape is capped at `partial` at the marking and the check, and the marking tells a Bastion when
a download was counted. The Bastion's side: the rows held by the engine name their tests in the table above, and the Bastion caps a
registry token's shape itself as well (Q-BAS-198). The cap stays on the repository when the read shapes per package let a registry token be
`full` on the images of its grant ([below](#read-shapes-per-package), built). What it changes in the trust boundary:

| Change | What it means for trust | Held by |
|---|---|---|
| `/v2/…` is served at the Bastion's host root | One more path family reaches the read flow. It is classified as the main installation's router reads it (an encoded or dot segment of the family, the project or the key is refused), and the main installation's backstop admits it as a forwarded family; nothing else below the host root is served | R-BAS-051, Q-BAS-121, Q-BAS-165 |
| The registry's token flow passes through the Bastion | A client's Quader token reaches the Bastion in the Basic header or the form of a token request, and the registry token the main installation issues passes back through it. That is the trust the Bastion already holds for every caller's credential in transit; the token's answer is never kept, collapsed or logged, and the Bastion holds no key to issue or verify a registry token. An anonymous token request does not pass: the Bastion answers it with a token of its own that no key signs and every Bastion drops (R-BAS-094, [H-2350](#h-2350--anyone-can-mint-an-anonymous-registry-token-of-a-bastion)) | R-BAS-051, Q-BAS-166, Q-BAS-435 |
| A registry token is a credential the Bastion cannot verify | Its form is checked and every decision about it is the main installation's (a forward, the check presented below `/v2/`). Its read shape is capped at `partial`: the decision point does not see the grant (`authz.Subject` has no member for it, verified by `TestR_BAS_080_TheDecisionPointDoesNotSeeTheGrant`), so an uncapped `full` would let a token granted one image read every cached image of its repository from the Bastion. The cap is built at the marking and at the check, an anonymous registry token's included (`bastionmain.CapRegistryShape`) | R-BAS-052, Q-BAS-167 |
| A token answer's marking names the token service's caller | The marking states the Quader token the token service accepted, not the anonymous caller the chain saw on `/v2/_token`, so a Bastion never takes a narrowed token for the anonymous caller from a token answer | R-BAS-051, Q-BAS-187 |
| The check verifies a registry token on every replica | The check's stage counts its paths as served, so the answer for a registry token does not depend on the families the replica that answers the interface serves | R-BAS-052, Q-BAS-190 |
| The Bastion reads an anonymous registry token's payload without verifying it | The one bit it uses, "anonymous", can only lower what is read: the request is forwarded without a credential, which any client may send | R-BAS-052, Q-BAS-168 |
| `401 insufficient_scope` is no longer `invalid` | A client that reuses a token for another image is not counted toward the failed-authentication timeout of its address; every other `401` still is | R-BAS-052 |
| Signed Debian indexes pass through | The Bastion cannot forge an `InRelease`; clients verify it with the key of their source line. It can serve an older genuine one ([H-1246](../spec/bastion.md#h-1246--a-compromised-bastion-can-serve-an-older-genuine-document-of-a-repository)) | R-BAS-055 |
| The ClamAV definitions without certificates | No certificate is verified at a Bastion and none is vouched for to the main installation; the certificate subject stays where TLS ends at Quader | R-BAS-056, Q-BAS-176 |
| Larger content | Gigabyte layers make the existing bounds matter more: a fill holds a budget slot and spool for its transfer, a caller of a running fill follows the spool after its own permission check, never on another's | R-BAS-054, Q-BAS-172 |
| A format's facts and a bound host come from the handshake | What headers are replayed, which status a refusal takes and which repository a bound host names are the main installation's statements over the Bastion token's channel, not the Bastion's configuration | R-BAS-057, Q-BAS-170, Q-BAS-178 |

## The handover per blob

The owner's decision 12 (b) of 2026-11-22 ([spec/bastion.md](../spec/bastion.md#the-handover-per-blob), R-BAS-070 to R-BAS-079), built:
with the handover switched on for its registration, a Bastion that misses a large file may be answered with a link of the main
installation's object store and, for a copy on an encrypted connection, that copy's data key, and reads the file from the store itself. It
lifts Q-BAS-10 for that and for nothing else. Its perspective, its boundary and its gaps (H-1340 to H-1345) are on
[bastion-handover.md](bastion-handover.md).

## Read shapes per package

The development agent's decision of 2026-11-22 on Q-BAS-167, option (b) ([spec/bastion.md](../spec/bastion.md#read-shapes-per-package),
R-BAS-080 to R-BAS-088), built on 2026-11-22 (the main installation's side) and 2026-11-23 (the Bastion's side): a registry token, and every
reader narrowed by package, can be `full` on one package of a repository, and the Bastion serves that package's cached entries to it without
asking the main installation. What it changes in the trust boundary:

| Change | What it means for trust | Held by |
|---|---|---|
| A fourth kind of answer, the caller's shape on one package | A package answer is the main installation's statement about the caller, computed by the decision point on the same index as every other (`PackageReachOn`) and dropped by the same feed entries (`token`, `principal`, `grants`; a `content` entry makes the entry stale instead); no copy of a grant or a policy reaches the Bastion (Q-BAS-1 holds). It obeys the four validity rules of a `full` answer on the repository and is renewed only by use, by a request of its holder: the conditional request, or the check when its shape answer ended with it (Q-BAS-270), so its holder is served no longer than a full reader is | R-BAS-080, R-BAS-084, Q-BAS-240, Q-BAS-248, Q-BAS-270 |
| A registry token can be `full` on the images of its grant | The main installation limits the package shape to the grant, and the Bastion records `full` only where the token's grant, read without verifying it, names the repository and the image with pull (`capPackage`); a defect on either side alone cannot open another image, and trusting the unverified grant can only lower what is read. The cap on the repository stays (Q-BAS-198). The answer ends at the earlier of the expiry the main installation reported and the `exp` of the token's claims, so a report that names a later expiry cannot extend it, also in the grace period | R-BAS-052, R-BAS-084, Q-BAS-246, Q-BAS-252 |
| Blobs shared between images | Entries stay per cache key and carry the package of the marking that validated them (`cache.Entry.Package`, signed with the entry, replaced at every renewal and cleared by a marking without one); a package answer serves only the entries of its own package; a blob is reached only through a path that names an image, and a token's answer for any other image is `partial`, so it meets the main installation's `401 insufficient_scope`; no path serves bytes by digest alone | R-BAS-084, R-BAS-085, R-OCI-010 |
| A document is shared with a reader of its package | The main installation marks it `shared` for a reader `full` on its package; kit check 21 holds that such a reader receives what a reader of the whole repository receives. The rule rests on the core's notes of the host's decisions ([H-1383](../spec/bastion.md#h-1383--per-package-sharing-rests-on-the-cores-notes-and-kit-check-21)) | R-BAS-081, R-BAS-082, R-BAS-086, Q-BAS-244 |
| No new timing signal | A caller whose shape on the repository is known and `partial` gets a conditional request, not a check, for an entry whose package it has no answer for, so a cached and an uncached path cost one request each (`TestR_BAS_085_NoRoundTripTellsACachedPackageFromAnUncachedOne`). The signal of a first read by a fresh credential exists as before ([H-1384](../spec/bastion.md#h-1384--the-first-read-of-a-cached-entry-by-a-credential-without-a-shape-answer-costs-two-requests-an-uncached-one-costs-one)) | R-BAS-085, Q-BAS-243, Q-BAS-245 |
| The gates of the main installation see fewer pulls | A reader `full` on a package meets the gates at the fills and revalidations of the entries, not at every pull; what the feed does not carry reaches it within the revalidation periods ([H-1380](../spec/bastion.md#h-1380--changes-the-feed-does-not-carry-reach-registry-token-and-package-narrowed-readers-within-the-revalidation-periods-no-longer-at-their-next-read)), and the limits and blocks do not reach it while a check renews its answers ([H-1381](../spec/bastion.md#h-1381--the-main-installations-limits-and-blocks-do-not-reach-hits-from-per-package-answers)) | R-BAS-085 |
| The check answers per package | The holder of the Bastion token learns a held credential's shape per package, no more than requesting content with it would tell ([H-1382](../spec/bastion.md#h-1382--whoever-holds-the-bastion-token-learns-a-held-credentials-shape-per-package)) | R-BAS-083 |

An attacker with a token for one image gains reads of that image's cached entries without the main installation seeing each read, for at
most the token's life; its downloads are still reported. It gains no read of another image's entries, no read by digest, no hidden name
(package answers come from grants, never from existence, and there is no `none` per package) and no new timing signal. Every pull costs the
main installation its token request and one request for the new token (a check, or the forward whose marking gives the answer) instead of a
request per manifest and blob; measured on 2026-11-23, a warm pull of ten layers fell from 13 to 19 requests to 2 to 6 by client.

## Limits, configuration and throughput from the main installation

The owner's decisions of 2026-11-26 (W-34; [spec/bastion.md](../spec/bastion.md#limits-configuration-and-throughput-from-the-main-installation)
R-BAS-093 to R-BAS-099, [ADR-300](../decisions.md#adr-300--a-bastion-is-a-place-of-the-download-limits-configured-from-the-main-installation-and-names-nothing-of-it)),
**specified and not built**, except the rows of R-BAS-099 that package E16 built on 2026-11-26 (the texts, the problem type, the
policy names, the header allowlist, the check of `Location`, the forward's problems at the main installation and the replays' scan) and
the anonymous token and anonymous documents of R-BAS-094 that package E14a built the same day, and the refusal of writes at the Bastion
that package E15 built the same day (R-BAS-095); the build plan is [developer/bastion-limits.md](../developer/bastion-limits.md). What they change in the trust boundary once their packages
have landed:

| Change | What it means for trust | Held by |
|---|---|---|
| The configuration comes from the main installation | The Bastion applies what its link delivers: its figures, its switches and its place's rules are the main installation's word, as every answer already is. Whoever can answer as the main installation sets them ([H-2280](#h-2280--whoever-answers-as-the-main-installation-sets-a-bastions-limits-and-switches)); a replica that cannot reach it keeps its last configuration, stored and signed in its cache backend, or fails closed ([H-2285](#h-2285--a-replica-restarted-during-an-outage-without-its-stored-configuration-applies-the-built-in-one)) | R-BAS-093, Q-BAS-327 |
| Less of the internet reaches the main installation | Writes are refused at the Bastion, anonymous token requests are answered there, anonymous documents are kept for the metadata period, and a ceiling bounds what all anonymous callers of a Bastion cause together; a write never reaches the main installation's routing, so Q-BAS-118's admission of probes goes. *Built by E14a:* the anonymous token and the anonymous documents. *Built by E15:* the refusal of writes at the Bastion and the end of the
probes' admission; a write costs the Bastion its request limit and nothing of the main installation. The Bastion issues a token without a key: it is worth a request without a credential, anyone can mint one, and the main installation refuses it ([H-2350](#h-2350--anyone-can-mint-an-anonymous-registry-token-of-a-bastion)); a withdrawal reaches a kept anonymous document through the feed ([H-2351](#h-2351--a-withdrawal-reaches-an-anonymous-callers-kept-document-within-the-feeds-bound-no-longer-at-its-next-read)) | R-BAS-094, R-BAS-095, Q-BAS-435 |
| Budgets per subject at the Bastion | A hit is counted against the caller's budget at the Bastion's place, which narrows [H-1381](../spec/bastion.md#h-1381--the-main-installations-limits-and-blocks-do-not-reach-hits-from-per-package-answers); the subject key and the applying rule come from the main installation, which names more of each caller to the Bastion than the marking did ([H-2281](#h-2281--the-check-answer-names-each-callers-subject-key-and-rule-to-the-bastion)) | R-BAS-096, Q-BAS-328 |
| A cap on the bytes between the two (built, E10) | The Bastion paces every body it reads, from the main installation and from the object store, to its registration's throughput, 200 MB/s by default with a quarter for anonymous callers, so a flood of distinct artifacts fills neither the transfer network nor the store's egress beyond it; a Bastion that was taken over ignores its own cap ([H-2289](#h-2289--a-bastion-that-was-taken-over-ignores-its-throughput-cap)); without Valkey each replica holds its share ([H-2360](#h-2360--without-valkey-the-throughputs-replicas-hold-their-shares-as-bastionreplicas-says)); the hourly volume refuses the next miss, not a running transfer ([H-2361](#h-2361--the-throughputs-hourly-volume-is-checked-when-a-miss-is-admitted-not-while-it-reads)) | R-BAS-097, Q-BAS-455 |
| The Bastion reports usage and state | The main installation stores what a Bastion says it served; it is the Bastion's word ([H-2282](#h-2282--the-usage-and-the-state-a-bastion-reports-are-its-word)) | R-BAS-016, R-LIM-045 |
| Egress addresses per registration (built, E13) | A Bastion token makes its forwarded client address count only from its own registration's addresses, no longer from any address of one list for all registrations ([who may speak as a Bastion](#who-may-speak-as-a-bastion)) | R-BAS-017, Q-BAS-334 |
| Nothing names the main installation | No host, base URL, request ID, rule, place, internal repository name or version in a Bastion's answers; what remains tells that an installation stands behind it, not which ([H-2294](#h-2294--a-bastions-answers-still-tell-that-an-installation-stands-behind-it)). *Since W-35's P8 with E17:* the problems the main installation renders for a forward carry their forward texts, every 5xx of a forward the neutral text of its status, and a registry token names only its anonymity, expiry and grant ([H-2297](#h-2297--a-registry-tokens-readable-grant-tells-its-holder-which-of-the-names-it-asked-for-it-may-pull), [H-2322](#h-2322--an-ansible-galaxy-role-served-through-a-virtual-repository-names-the-member-in-its-id), [H-2323](#h-2323--a-4xx-text-written-later-reaches-a-forward-as-written-until-it-is-given-its-forward-text)) | R-BAS-099, R-BAS-114, R-BAS-115 |

## Disabling a Bastion

Built on the main installation's side ([R-BAS-133](../spec/bastion.md#r-bas-133--disabling-a-bastion-soft-or-hard-told-over-its-connection),
W-41's A1) and on the Bastion's (its `503`, the pause of its sync and the cut of what it runs, W-41's A2).
The owner's words of 2026-11-27: the Bastion is told at once to stop work, soft (what started finishes) or hard (what runs is cut), not by
a refused token and a grace period.

- **The disable is a member of the configuration**, signed and stored with it at the Bastion, so a replica restarted during an outage keeps
  it ([H-2442](#h-2442--a-replica-restarted-during-an-outage-without-its-stored-configuration-does-not-know-it-was-disabled) for one that
  lost its stored configuration). Every replica learns it within one poll of the feed (about 3 s) and confirms it with its status; the
  registration's state says whether it did.
- **The main installation refuses the disabled Bastion's work.** From every main replica's snapshot (2 s): a forward, the check and the
  catalogue answer `503 bastion_disabled`, a problem the Bastion never passes on; a hard disable cancels the Bastion's forwards in flight on
  every main replica. A Bastion that ignores its disable gets nothing new, but keeps what it holds
  ([H-2440](#h-2440--a-disabled-bastion-that-ignores-the-disable-serves-what-it-holds)).
- **What an outsider sees**: a temporary outage, `503` with `Retry-After`, in the format's shape and the Bastion's page, naming no
  administrator's act (R-BAS-099).
- **The line stays open.** The handshake, the feed and the download intake answer while disabled, so the Bastion confirms and learns the
  enable, and a soft disable's last downloads are reported. That is also what a leaked token of a disabled registration can still do
  ([H-2443](#h-2443--a-disabled-bastions-token-still-answers-the-handshake-the-feed-and-the-download-intake)); revoking the token ends it.
- **Restore.** `bastion.disabled` is journaled with its way and replayed never more lightly; a deletion is replayed as a hard disable, its
  tokens by their own entries. A restore never hands a Bastion back work an administrator stopped.
- **Upgrade.** A registration disabled before by its principal's status became disabled hard with migration 00158, so an upgrade restarted
  no Bastion an administrator had stopped.
- **Without a connection** nothing reaches the Bastion: it serves its cache for its grace period as an unreachable Bastion does, and applies
  the disable at its first handshake ([H-2441](#h-2441--a-disable-reaches-a-bastion-only-over-its-connection)). A soft disable has no
  time bound ([H-2444](#h-2444--a-soft-disable-has-no-time-bound)).
## What a Bastion may distribute

The owner's words of 2026-11-27 ([spec/bastion.md](../spec/bastion.md#what-a-bastion-may-distribute) R-BAS-134, superseding Q-BAS-6):
`public` is the main installation's level, on its network; what leaves it through a Bastion is granted twice. **Stored and built** (W-41's
B1): the lists and the choices, who changes them, and what a restore keeps. **Not built yet** (W-41's B2): the ceiling that holds every
request through a Bastion to them; until it lands a Bastion serves what it served before, decided per request on the caller's credential
alone.

| Who | Changes what | With |
|---|---|---|
| An administrator (`endpoints:write`) | the projects a Bastion allows; a new Bastion allows none | `PUT`/`DELETE /api/v1/bastions/{name}/projects/{projectKey}` |
| The project's Owner, or `repositories:write-config` | the Bastions among those that allow the project that distribute a repository; never a Maintainer | the repository document's member `bastions` |
| Nobody around the API | a choice whose project the Bastion does not allow: the database refuses it (`AI011`), deletes the choices with a project taken off a list and with a repository's move | migration `00159` |

- **What an administrator can no longer do by accident** (once the ceiling holds): make a repository Public on the intranet and have it on
  the internet; allow a project and send every repository of it out (the Owner chooses per repository); allow a project again and bring
  back its old choices (the removal deleted them).
- **A restore never widens it.** Taking a project off a list and taking a Bastion off a repository are journaled, and the replay deletes
  the restored rows again (Q-BAS-521). An allow and a choice are not journaled: a restore to a point before them takes them back, which
  narrows.
- **Every reader learns it.** A repository's readers see the names of the Bastions that distribute it and their public base URLs
  ([H-2445](#h-2445--a-repositorys-readers-learn-the-names-and-addresses-of-the-bastions-that-distribute-it)).

## The public catalogue and the configuration on the wire

Interface version 8 (built on 2026-11-26 by E7 with W-35's P5; [spec/bastion.md](../spec/bastion.md#the-interface-version-8-the-shared-wire),
R-BAS-101 to R-BAS-103, [ADR-303](../decisions.md#adr-303--the-public-catalogue-travels-on-the-bastion-interface-as-compressed-documents-under-a-change-list)).
What it changes in the trust boundary:

| Change | What it means for trust | Held by |
|---|---|---|
| The Bastion token reads the public catalogue | The catalogue routes admit the Bastion token alone and take the registration from its recognition: a token reads its own registration's catalogue, nothing while the registration's `gui` is off (`404 not_found`), no documentation while `docs` is off. What it reads is what an anonymous visitor reads, stripped of internal identities (W-35's B.10), in bulk ([H-2305](#h-2305--the-bastion-token-reads-its-registrations-public-catalogue-in-bulk)) | R-BAS-101, R-BAS-102, Q-BAS-350 |
| A document's address is checked by the Bastion | A document's SHA-256 is of its raw bytes; a Bastion decompresses what it receives, at most to the size it was told, and verifies the SHA-256 before it keeps it. A path has one spelling, no dot segment and no encoded slash, so a path cannot name a file outside the catalogue's tree of the store (W-35's P7 keeps the documents by their address) | R-BAS-101, R-BAS-103, Q-BAS-346, Q-BAS-348 |
| The feed's catalogue entries and configuration changes name no registration | Every Bastion learns that some registration's catalogue or configuration changed, and when ([H-2306](#h-2306--every-bastion-learns-when-some-catalogue-or-some-configuration-changed)) | R-BAS-015, Q-BAS-349 |
| A configuration in the handshake | A Bastion learns the figures, the rule IDs, the address rules and the exempt networks of its place; whoever holds its token or takes over a pod learns them too ([H-2307](#h-2307--a-bastions-configuration-tells-its-holder-the-rules-and-networks-of-its-place)). Since E8 the main installation sends each registration's own configuration | R-BAS-093, Q-BAS-440 |
| No probe | Until E15 a probe was a forward of a writing method without a body and without a caller's credential, which the main installation admitted as the anonymous caller for routing only. E15 removed it: the main installation admits no forward for routing alone, a forwarded write is the authentication stage's and then the backstop's, and a read route with a body that a format does not declare is refused at the Bastion (H-2293) | R-BAS-095, Q-BAS-351 |

Interface version 9 (built on 2026-11-26 by W34-E19; [spec/bastion.md](../spec/bastion.md#the-interface-version-9-a-head-is-no-download-and-a-replica-says-whose-cache-it-reports)) adds two members:

| Change | What it means for trust | Held by |
|---|---|---|
| A forwarded `GET` may say that it answers a `HEAD` | `Quader-Bastion-For-Head: true` makes the main installation record no download for the answer and charge no download figure: no statistics, no activity, no `artifact.downloaded`, no cached bytes, admission or parallel lease. Only a recognized forward's statement counts (an ordinary request's header is removed and means nothing), and only on a metadata or computed route, the route of a document ([Q-BAS-482](../spec/bastion.md#q-bas-482--the-main-installation-honours-the-statement-of-a-head-on-a-documents-route-only)): on a download route the `GET` counts as it always did, so whoever holds a Bastion token cannot take an artifact out of the count, the audit log's downloads or the caller's download figures. What a token holder can still do: fetch a counted document without a download for it ([H-2370](#h-2370--a-bastion-token-takes-a-counted-documents-download-out-of-the-count)) | R-BAS-021, R-BAS-059, Q-BAS-479, Q-BAS-481, Q-BAS-482 |
| The status says whether the cache is shared | `cache.shared` is the Bastion's word like every figure of its state ([H-2282](#h-2282--the-usage-and-the-state-a-bastion-reports-are-its-word)): a Bastion that lies about it makes the card add or not add its cache figures, nothing else | R-LIM-045, Q-BAS-480 |

Interface version 10 (built on 2026-11-27 by W35-P9e; [spec/bastion.md](../spec/bastion.md#r-bas-128--the-interface-version-10-a-packages-grade-per-version-the-projects-names-and-the-installations-form-of-a-time)) adds three members:

| Change | What it means for trust | Held by |
|---|---|---|
| A package's open findings per version are a document | The anonymous answer of the main installation, open findings only (no dismissed or resolved one, W-35's B.4b), through the strip: what an anonymous visitor of the main installation reads of the package's grades, nothing more; the time of a check is taken out and kept nowhere | R-BAS-128, Q-BAS-403 |
| The public set names a project | The key, the display name and the description an anonymous caller of the main installation reads, their links rewritten to the Bastion's (R-BAS-107); no owner, no member count, no size | R-BAS-128, Q-BAS-423 |
| The configuration carries the installation's form of a time | A word of six values the Bastion's rules accept or refuse (a configuration with another fails the handshake); it says nothing of the installation but how it writes a date, which its public pages show anyway; its change is a `settings` entry of the feed, which names no registration ([H-2306](#h-2306--every-bastion-learns-when-some-catalogue-or-some-configuration-changed)) | R-BAS-093, R-BAS-128, Q-BAS-402 |

## The builder of the public catalogue

W-35's package P6, built on 2026-11-26 ([spec/bastion.md](../spec/bastion.md#the-builder-of-the-public-catalogue), R-BAS-104 to R-BAS-108).
The main installation renders what its anonymous caller reads into the documents a Bastion mirrors. What it means for trust:

| Mechanism | What it means for trust | Held by |
|---|---|---|
| The anonymous caller decides | The builder reads the management API as the anonymous caller of the default endpoint, through the decision point and the exposure ceiling, so the catalogue holds what an anonymous visitor reads today and nothing a level or a grant closes; it never reads as a user or with the Bastion token | R-BAS-104, Q-BAS-355 |
| The strip | Who uploaded, released or triaged, a CI job's claims, an internal anchor's title, a state code, an upstream that is no public registry, a principal's reference and every download count leave every document before it is kept ([H-2309](#h-2309--the-strip-knows-the-members-the-main-installation-writes-today)) | R-BAS-106, Q-BAS-364 |
| Nothing names the main installation | Every link is the Bastion's; an origin of the main installation in any text becomes the Bastion's base URL; a link of the management API becomes the Bastion's data path or nothing ([H-2308](#h-2308--a-bare-host-name-of-the-main-installation-in-author-text-is-not-rewritten)) | R-BAS-107, Q-BAS-363, Q-BAS-365 |
| The base URL is the installation's word | A registration's documents are rendered only on a base URL `server.additional_base_urls` lists; what a Bastion reports alone never decides a host the documents name | R-BAS-104 |
| A narrowing is a change | A level lowered, a grant removed or a block set changes the fingerprint or the public set, and the next run deletes what fell out; the run starts 5 s after the change ([H-2311](#h-2311--a-narrowing-stays-in-the-catalogue-until-the-next-run), [H-2314](#h-2314--a-narrowing-reaches-a-bastions-pages-only-at-its-next-poll-after-the-run)) | R-BAS-105 |

## The public catalogue at the Bastion

W-35's package P7, built on 2026-11-26 ([spec/bastion.md](../spec/bastion.md#the-public-catalogue-at-the-bastion), R-BAS-109 to
R-BAS-113). A replica mirrors its registration's catalogue into its cache backend and serves the web interface from it alone. What it
means for trust:

| Mechanism | What it means for trust | Held by |
|---|---|---|
| No page view reaches the main installation | The reader answers from the replica's memory and its cache backend; the only requests to the main installation are the sync's, which the Bastion token makes, which take no slot of the callers' budget and are paced in the throughput's class `catalogue`, below the callers' misses. A visitor, a crawler or a scraper costs the Bastion and never the main installation | R-BAS-109, R-BAS-113, Q-BAS-381 |
| Every document is verified, on the way in and on every read | A document is decompressed to at most the size its change named and held to the SHA-256 of its raw bytes before it is kept, and again whenever it is read; one that fails is not served, is deleted and is fetched again by the next round (W-35's D6) | R-BAS-109, R-BAS-113, Q-BAS-372 |
| The state record is signed | The record that names the cursor, the public set, the switches and the shards of the manifest is signed with the signing key in the domain `catalogue` and, with a cache key, sealed and bound to its name, so a write to the bucket or the volume without the keys cannot make a replica serve another catalogue; without a signing key (the volume backend without Valkey) it is not authenticated ([H-2316](#h-2316--without-a-signing-key-the-catalogues-state-record-is-not-authenticated)) | R-BAS-110, Q-BAS-374 |
| The public set hides at once | A feed entry of a narrowing makes the replica read the public set before anything else, and a package none of whose repositories is in it is no longer answered; its documents go at the builder's run ([H-2321](#h-2321--a-package-still-public-in-another-repository-shows-what-a-narrowed-one-held-until-the-run)) | R-BAS-112, Q-BAS-376, Q-BAS-377 |
| The switch gui off | Every read answers off at once, the sync stops and the catalogue's tree is deleted; a replica that does not know its switches (the built-in configuration without a stored state) answers not ready, never off | R-BAS-102, R-BAS-112, Q-BAS-379 |
| Serving on during an outage | The owner's choice (W-35's B.2b): the pages serve their last state whatever its age and say how old it is, so a narrowing made during an outage stays visible until the link returns ([H-2319](#h-2319--a-narrowing-made-during-an-outage-stays-on-the-pages-until-the-link-returns)) | R-BAS-111 |
| The catalogue's share | The documents count against `bastion.gui.max_bytes`, which comes off the artifacts' room; a catalogue above it is kept and alerted, never evicted ([H-2317](#h-2317--a-catalogue-above-its-share-is-kept-and-the-cache-may-pass-its-size)) | R-BAS-110, Q-BAS-380 |

## The web interface

W-35's packages P9 to P11 with P9b, P9d and P9e, built on 2026-11-26 and 2026-11-27 ([spec/bastion.md](../spec/bastion.md#the-bastions-web-interface),
R-BAS-116 to R-BAS-126, R-BAS-128, R-BAS-129; the administrator's side is the manual's
[The web interface for visitors](../manual/admin/bastion.md#the-web-interface-for-visitors)). Anonymous visitors from the internet read
the public catalogue in a browser at the Bastion's own host, while the registration's switch `gui` is on (off for a new registration).

**What a visitor can read.** Exactly what the main installation answers its anonymous caller of the default endpoint, through the
decision point and the exposure ceiling (R-BAS-104): the projects that hold a public package or a public repository of files, with their
key, display name and description; every repository whose artifacts are `public`, of every class, a remote with what its cache holds;
per package and version its facts, files (size, SHA-256, a download link on the Bastion's package routes), malware verdict, signatures
(kind, signer, issuer, time proof, log, trust state), the signature objects and the anchors to verify them offline, and its grade with
the open findings only (`needs_triage`, `confirmed`, `blocked`); with `docs` on and a public wiki the README, the documentation and
Debian's changelog and copyright; a public generic repository's folders and files; every public repository's client set-up with
`credentials=none`; the Bastion's own imprint and privacy notice; and the time of the replica's last round.

**How that is bounded:**

- **Public only.** The builder reads as the anonymous caller and never with a user's or the Bastion's credential, and the replica
  hides at once what falls out of the public set (R-BAS-104, R-BAS-112). The anonymous caller's reads are the rule; the catalogue has
  none of its own. While the default endpoint admits no request without a credential (`auth.anonymous: false`), the builder builds every
  catalogue empty and records why (R-BAS-104, Q-BAS-495;
  [H-2330](#h-2330--the-catalogue-holds-public-content-while-the-default-endpoint-admits-no-request-without-a-credential), closed).
- **The strip.** Who published, released, uploaded or triaged, a CI job's claims, every principal's reference, download counts, an
  internal anchor's title, a state code, the scanner's engine and replica, an upstream that is no public registry, and every origin of
  the main installation leave every document before it is kept (R-BAS-106, R-BAS-107; [H-2309](#h-2309--the-strip-knows-the-members-the-main-installation-writes-today)).
- **No request to the main installation.** Every answer comes from the replica's catalogue and its legal pages; an architecture rule
  keeps the client, the handover and the engine out of the web interface's closure (R-BAS-116).
- **The flood class `gui`.** Every request of a page, a file of the bundle or the data asks the block list and the class `gui` per
  address (20 requests a second, a burst of 100, by default), never a miss and never the anonymous class's budget (R-BAS-117).
- **The CSP and the headers.** A page runs only the Bastion's own scripts under Trusted Types, frames and forms are refused, no
  referrer leaves, no cookie is set and no `Server` or `Quader-` header is sent (R-BAS-117).
- **The bundle.** The application is built apart from the main installation's, and its build fails when the main installation's API
  client (its write methods), its session, a screen of its own or a dialog put a byte into it (R-BAS-119, R-UI-228): a page has no way to
  write, to sign in or to hold a session.

What it means for trust, mechanism by mechanism:

| Mechanism | What it means for trust | Held by |
|---|---|---|
| Read-only by construction | Every address of the web interface answers `GET` and `HEAD` alone; any other method is `405` before anything is read. There is no act on any page, no form, no session, no cookie and no state per visitor | R-BAS-116, R-BAS-117 |
| Nothing behind the Bastion | The web interface reads the replica's catalogue and its legal pages only; an architecture rule keeps the client of the main installation, the handover and the engine out of its dependency closure, and the route test over the whole replica counts no forward and no check for any of its addresses. A visitor, a crawler or a scraper costs the Bastion alone ([H-2326](#h-2326--many-addresses-cost-a-bastions-cpu-and-bandwidth-through-its-pages)) | R-BAS-116 |
| The Bastion's own bundle | The application is built and embedded apart from the main installation's; the main installation's bundle is a package the Bastion may not import, so none of its screens (sign-in, administration) is in the role's closure. Its build fails when the main installation's API client, its session, a screen of its own or a dialog put a byte into it, read from the build's metafile | R-BAS-116, R-BAS-119, R-UI-228 |
| Authors' HTML on the page | READMEs, documentation and changelogs are the main installation's sanitized HTML, bound through Angular's sanitizer under Trusted Types ([H-2327](#h-2327--authors-html-is-shown-on-the-bastions-origin)) | R-UI-229, R-BAS-117 |
| A page runs only its own code | The page's policy admits scripts, styles, images, fonts and requests of the Bastion's own origin alone, no form target, no frame ancestor and Trusted Types; every other answer runs nothing (`default-src 'none'`); frames are refused twice (`frame-ancestors 'none'`, `X-Frame-Options: DENY`), and no referrer leaves the page (W-35's D7) | R-BAS-117 |
| The request class `gui` | Every request asks the block list and the class `gui` per address, never the anonymous class's budget and never a miss | R-BAS-117, Q-LIM-307 |
| One origin with the package API | A browser that answered a `401` of the package API holds Basic credentials for the origin ([H-2325](#h-2325--the-web-interface-shares-its-origin-with-the-package-api)) | — |
| A rolling upgrade | A replica answers a file of another release's bundle `404`; the page loads itself again ([H-2324](#h-2324--a-page-of-one-release-asks-a-replica-of-another-for-a-file-its-bundle-lacks)) | R-BAS-116 |
| A generic repository's files | A public generic repository's folders and files are the main installation's views without an act (W-35's P9d): a file downloads through the Bastion's own package route, its commands are the catalogue's set-up of its path, and no upload, delete or statistic is in the bundle. The file's signatures are a badge without their report ([H-2328](#h-2328--a-generic-files-signatures-are-a-badge-alone-at-the-bastion)), and its malware verdict shows no database date ([H-2329](#h-2329--a-generic-files-malware-verdict-at-the-bastion-shows-no-date-of-its-database)) | R-UI-229, R-BAS-118 |
| The documentation (W-35's P11) | A version's README and documentation travel only while the registration's `docs` is on and a public repository that holds the version has a public wiki; without them the version announces none (`hasReadme` false). With `docs` off the data path answers no documentation, no hit of the documentation's search and no Debian changelog or copyright at once, before the builder's next run. The search inside a version runs in the browser over the tree; the search across packages scans the replica's index ([H-2338](#h-2338--a-search-of-the-documentation-across-packages-scans-every-entry-of-the-newest-versions), [H-2339](#h-2339--a-replica-holds-every-public-packages-newest-documentation-entries-in-memory)) | R-BAS-124 to R-BAS-126 |
| A package's grades per version (W-35's P9e) | The document `packages/<id>/vulnerabilities` is the main installation's anonymous answer, open findings only, through the strip, which takes each grade's `checkedAt` out ([H-2407](../spec/bastion.md#h-2407--a-bastions-grade-in-a-list-says-nothing-of-when-and-against-which-database-it-was-checked)) | R-BAS-128, Q-BAS-403 |
| Set up client (W-35's P9e) | The snippets are the catalogue's set-up documents, rendered with `credentials=none` and `bastion=<name>`: the Bastion's address, no login line, no publish step, no token field and no guide of the main installation; a repository the public set does not hold gets none | R-BAS-129, R-BAS-104 |
| The age | A replica serves its copy whatever its age and every page says how old it is; a narrowing made during an outage stays visible until the link returns ([H-2319](#h-2319--a-narrowing-made-during-an-outage-stays-on-the-pages-until-the-link-returns)) | R-BAS-111, R-BAS-120 |
| Hide repositories (W-40's P2) | The replica applies the switch at once and keeps it in its signed state record: no repository's index, no list of repositories, a single repository's name and format alone, and no repository in the crawler's text or the sitemap. It hides the presentation, not the facts: the data keep each version's holders with their keys and classes, and every command names a key ([H-2439](#h-2439--with-hide-repositories-the-pages-name-no-repository-but-the-data-a-browser-reads-keep-each-versions-holders-and-every-command-names-a-key)) | R-BAS-131, R-BAS-112, R-BAS-118, R-BAS-132 |

## Search engines

W-35's package P10, built on 2026-11-26 ([spec/bastion.md](../spec/bastion.md#search-engines-find-the-bastions-pages), R-BAS-121 to
R-BAS-123, R-UI-231). A crawler reads a page's head and text, `robots.txt` and the sitemap at the Bastion's host. What it means for trust:

| Mechanism | What it means for trust | Held by |
|---|---|---|
| From the catalogue alone | The head, the text, `robots.txt` and the sitemap are written from the replica's catalogue and its legal pages, on the Bastion's own `server.base_url`; the integration case counts no request at the main installation for a crawler's reads and scans what it reads for the main installation's names | R-BAS-121, R-BAS-122, R-BAS-120 |
| No format's metadata | Only the members of the main installation's shapes that are the same for every format are decoded; a sentinel in a version's `metadata` reaches no page in the unit test | R-BAS-121 |
| JSON-LD cannot close its element | The encoder escapes `<`, `>` and `&`; a description with `</script>` stays inside the data block, and the page's policy runs no inline script anyway | R-BAS-121 |
| The findings stay out of search results | The findings' documents are disallowed, an address that opens them is `noindex`, and the grade and the findings are `data-nosnippet` on the pages; the text carries no finding at all | R-BAS-121, R-BAS-122, R-UI-231 |
| A crawler costs the main installation nothing | `robots.txt` disallows the package API's families and the download links are `nofollow`; a crawler that ignores both is an anonymous client under the class `gui` and the miss limits ([H-2337](#h-2337--a-page-a-crawler-reads-costs-the-bastion-its-reads-and-a-crawler-that-ignores-robotstxt-reaches-the-package-api)) | R-BAS-122, R-UI-231 |
| What became private leaves the index | A page the catalogue no longer shows answers `404`, which a search engine reads at its next visit ([H-2334](#h-2334--a-search-engine-keeps-a-page-after-its-content-became-private-until-it-visits-again)) | R-BAS-121 |
| A refusal is a page that loads nothing | A page's address over the class `gui` is a page with its styles under its own nonce and no script; it cannot cascade into refused files | R-BAS-123 |

## What this does not cover

The gaps of the Bastion's mechanisms are recorded on the spec page, where the mechanisms are, and only linked here. The one gap
that no mechanism of the Bastion holds, because the chart leaves it to the cluster operator, is recorded on this page:
[H-522](#h-522--the-chart-restricts-no-egress-of-the-bastion-pods). The gaps of the owner's decisions of 2026-11-26 (W-34), specified and
not built, are recorded on this page as well, each with the package whose mechanism has it: H-2280 to H-2283, H-2285, H-2289 to H-2297,
H-2348 to H-2351, H-2354, H-2355 (H-2291, H-2348 and H-2349 are E13's, built; H-2281 to H-2283, H-2354 and H-2355 E8's, live); so are the gaps of the public catalogue's wire (W-35, interface version 8),
H-2305 to H-2307, of its builder (W-35's P6), H-2308 to H-2314, of the catalogue at the Bastion (W-35's P7), H-2315 to H-2321, of the web interface (W-35's P9), H-2324 to H-2327, with its generic files (W-35's P9d), H-2328 and H-2329, of what the web interface shows (W-35's P12), H-2330 (closed by W35-P9f), and of its search engines (W-35's P10), H-2334 to H-2337; so are the gaps of the packages built since: H-2356 and H-2357
(E9a), H-2358 and H-2359 (E15), H-2360 and H-2361 (E10), H-2362 (E11), H-2322 and H-2323 (W-35's P8 with E17), H-2374 and H-2375 (E18;
H-2375 closed by E19), H-2370 (E19);
and of a registration the configuration declares (R-BAS-127), H-2366 and H-2367; and of the documentation of the web interface (W-35's
P11), H-2338 and H-2339; and of what a Bastion may distribute (R-BAS-134, W-41's B1), H-2445;
those of a Bastion's budgets are on [usage-and-limits.md](usage-and-limits.md#what-this-does-not-cover) (H-2284, H-2286 to H-2288, H-2368, H-2369). Numbers
marked [derived] are calculated from the defaults, not measured.

- [H-382](../spec/bastion.md#h-382--a-volumetric-attack-on-bandwidth-is-not-absorbed-by-the-bastion): volumetric attacks on
  bandwidth;
- [H-383](../spec/bastion.md#h-383--during-the-grace-period-a-token-revoked-at-the-main-installation-reads-on-at-the-bastion):
  revocations during an outage of the main installation;
- [H-384](../spec/bastion.md#h-384--a-version-newly-published-appears-at-the-bastion-only-after-the-metadata-period): new versions
  and security fixes after the metadata period;
- [H-385](../spec/bastion.md#h-385--a-withdrawal-the-feed-does-not-carry-reaches-the-bastion-within-the-artifact-revalidation-period):
  withdrawals and automatic blocks the feed does not carry;
- [H-1102](../spec/bastion.md#h-1102--the-bastion-serves-what-it-cached-for-up-to-ten-minutes-after-the-switch-is-turned-on):
  the malware scanning's switch turned on, until the cached artifact's next revalidation;
- [H-2407](../spec/bastion.md#h-2407--a-bastions-grade-in-a-list-says-nothing-of-when-and-against-which-database-it-was-checked): a grade in a
  Bastion's list names no time of its check and no database; a version's page does since W-36's P8;
- [H-386](../spec/bastion.md#h-386--whoever-holds-the-bastion-token-can-test-credentials-name-client-addresses-and-add-download-events):
  what the holder of the Bastion token can do;
- [H-387](../spec/bastion.md#h-387--the-signing-key-and-write-access-to-the-bastions-valkey-or-bucket-together-forge-answers-and-entries):
  the signing key together with write access to Valkey or the bucket;
- [H-388](../spec/bastion.md#h-388--anonymous-misses-from-many-addresses-take-the-anonymous-share-of-the-budget): distributed
  anonymous miss floods;
- [H-389](../spec/bastion.md#h-389--without-valkey-a-block-and-a-counter-hold-on-one-replica-only): blocks and counters without
  Valkey;
- [H-390](../spec/bastion.md#h-390--the-time-of-an-answer-tells-a-caller-whether-others-fetched-a-package-recently): what the
  timing of a hit tells;
- [H-391](../spec/bastion.md#h-391--download-events-lost-to-a-full-buffer-a-crash-or-an-ephemeral-spool-are-missing-from-the-audit):
  download events lost before they reach the audit;
- [H-428](../spec/bastion.md#h-428--the-ops-listener-of-a-bastion-serves-its-metrics-without-authentication): the ops listener's
  metrics carry no authentication;
- [H-432](../spec/bastion.md#h-432--the-edge-limit-counts-per-tcp-peer-address-and-has-no-burst): the edge limit keys on the TCP
  peer address and has no burst;
- [H-434](../spec/bastion.md#h-434--a-controller-that-ignores-the-annotations-turns-the-edge-limit-and-routing-off-silently): a
  controller that ignores the Ingress annotations turns the edge limit and routing off silently;
- [H-436](../spec/bastion.md#h-436--deleting-a-registration-deletes-its-token-rows-so-a-later-use-of-a-deleted-token-is-unknown-and-not-logged):
  a deleted registration's token leaves no rejection log;
- [H-437](../spec/bastion.md#h-437--a-bastion-tokens-last-use-is-one-value-per-token-at-the-granularity-of-the-last-used-buffer): the last
  use of a Bastion token is one value per token;
- [H-438](../spec/bastion.md#h-438--the-interface-calls-of-a-bastion-share-the-read-budget-of-its-token): the interface calls of a
  Bastion share the read budget of its token;
- [H-439](../spec/bastion.md#h-439--download-events-enter-the-audit-trail-with-the-time-the-bastion-reports): download events enter
  the audit trail with the time the Bastion reports, behind the position of a tailing consumer;
- [H-440](../spec/bastion.md#h-440--an-address-that-sends-rejected-bastion-tokens-is-refused-its-valid-bastion-token-for-a-minute): an
  address that sends rejected Bastion tokens is refused its valid one for a minute;
- [H-441](../spec/bastion.md#h-441--absent-rests-on-the-notes-of-the-cores-lookups-not-on-the-routes): the class `absent` rests on the
  notes of the core's lookups;
- [H-442](../spec/bastion.md#h-442--a-401-states-the-subject-as-invalid-whether-the-credential-was-rejected-or-none-was-sent): a `401`
  states the subject as `invalid` whatever the reason;
- [H-444](../spec/bastion.md#h-444--download-events-spooled-by-a-draining-replica-can-be-delivered-twice): spooled download
  events can be delivered twice;
- [H-445](../spec/bastion.md#h-445--a-bastion-clock-ahead-of-the-main-installations-shortens-the-feed-lookback): a Bastion
  clock ahead of the main installation's shortens the feed lookback;
- [H-446](../spec/bastion.md#h-446--the-connection-to-the-main-installation-is-not-under-the-egress-address-policy): the
  connection to the main installation is not under the egress address policy;
- [H-447](../spec/bastion.md#h-447--a-background-refresh-keeps-the-token-string-beyond-its-request): a background refresh keeps the
  token string beyond its request;
- [H-448](../spec/bastion.md#h-448--a-chunked-body-of-a-read-route-that-takes-a-body-is-held-in-memory): a chunked body of a read
  route that takes a body is held in memory;
- [H-449](../spec/bastion.md#h-449--a-range-request-costs-a-read-of-the-whole-object): a range request costs a read of the whole
  object;
- [H-450](../spec/bastion.md#h-450--a-fill-that-does-not-fit-the-spool-is-passed-through-unkept-and-a-request-answered-from-the-spool-fails):
  a fill that does not fit the spool is passed through unkept, and a request answered from the spool fails;
- [H-451](../spec/bastion.md#h-451--a-ranged-read-of-a-cached-artifact-is-not-verified): a ranged read of a cached artifact is not
  verified;
- [H-452](../spec/bastion.md#h-452--the-entries-of-evicted-bytes-stay-until-they-are-read-and-are-not-counted-in-the-cache-size):
  entries of evicted bytes stay until read and are not counted in the size;
- [H-453](../spec/bastion.md#h-453--objects-and-markers-are-not-signed-a-replaced-object-is-found-only-when-it-is-read-and-markers-steer-eviction):
  objects and markers are not signed;
- [H-454](../spec/bastion.md#h-454--the-gauge-of-blocked-addresses-counts-the-blocks-a-replica-knows): the gauge of blocked addresses
  is per replica;
- [H-455](../spec/bastion.md#h-455--every-request-costs-the-bastions-valkey-round-trips-and-an-unblocked-flood-from-many-addresses-costs-it-per-request):
  the load a flood from many addresses puts on the Bastion's Valkey;
- [H-456](../spec/bastion.md#h-456--a-block-or-a-timeout-set-without-valkey-or-while-it-is-unavailable-is-forgotten-when-the-replica-restarts):
  blocks set without Valkey are forgotten at a restart;
- [H-457](../spec/bastion.md#h-457--a-reader-without-endpointsread-is-not-offered-the-bastion-as-a-target): a reader without
  `endpoints:read` is not offered the Bastion as a target;
- [H-458](../spec/bastion.md#h-458--the-address-a-registration-reports-chooses-among-the-listed-addresses-and-the-name-is-testable):
  the reported address chooses among the listed addresses, and the name is testable;
- [H-460](../spec/bastion.md#h-460--without-a-signing-key-the-entries-on-the-volume-are-not-authenticated): without a signing key the entries on the
  volume are not authenticated;
- [H-461](../spec/bastion.md#h-461--the-suites-front-reaches-the-main-installations-replica-in-plain-http): the suite's front reaches the main installation's replica in plain HTTP;
- [H-462](../spec/bastion.md#h-462--a-main-installation-that-reports-a-higher-interface-version-in-its-own-problem-body-is-not-exercised): a main installation that reports a higher interface version in its own problem body is not exercised;
- [H-463](../spec/bastion.md#h-463--the-bound-on-the-upload-body-a-bastion-reads-is-shown-by-the-answer-not-by-a-counter): the bound on the upload body a Bastion reads is shown by the answer, not by a counter;
- [H-467](../spec/bastion.md#h-467--behind-the-main-installations-own-ingress-the-forwarded-host-is-what-the-ingress-reports): behind the main installation's own ingress the forwarded host is what the ingress reports;
- [H-468](../spec/bastion.md#h-468--the-download-budget-is-per-registration-so-many-registrations-at-their-budget-still-fill-the-audit-buffer): the download budget is per registration;
- [H-469](../spec/bastion.md#h-469--a-probe-admitted-for-routing-runs-any-handler-below-the-package-families-that-does-not-tell-the-marking-its-route): a probe admitted for routing runs any handler below the package families that does not tell the marking its
  route;
- [H-471](../spec/bastion.md#h-471--a-pass-through-body-at-the-floor-rate-holds-its-budget-slot-for-the-length-of-the-transfer): a pass-through body at
  the floor rate holds its budget slot for the length of the transfer;
- [H-472](../spec/bastion.md#h-472--a-fill-goes-on-after-its-client-has-left): a fill goes on after its client has left;
- [H-473](../spec/bastion.md#h-473--the-roles-binary-contains-packages-of-the-main-installation-it-never-starts): the role's binary contains
  packages of the main installation it never starts;
- [H-474](../spec/bastion.md#h-474--the-sliding-estimate-of-a-limit-admits-more-than-the-limit-in-a-minute-that-spans-a-boundary): the sliding
  estimate of a limit admits more than the limit in a minute that spans a boundary;
- [H-464](../spec/bastion.md#h-464--a-client-that-sends-accept-encoding-gzip-was-asked-about-at-the-main-installation-on-every-artifact-read-cause-not-found): a client that sends
  `Accept-Encoding: gzip` was asked about at the main installation on every artifact read, cause not found;
- [H-1242](../spec/bastion.md#h-1242--every-pull-with-a-registry-token-costs-the-main-installation-its-token-request-and-one-check):
  every pull with a registry token costs the main installation its token request and one check (live for every image pulled with a registry
  token; a load and not a confidentiality matter);
- [H-1243](../spec/bastion.md#h-1243--a-plain-index-path-of-a-debian-remote-without-by-hash-can-disagree-with-the-inrelease-a-bastion-serves):
  a plain index path of a Debian remote without by-hash can disagree with the `InRelease` a Bastion serves (live for such remotes);
- [H-1244](../spec/bastion.md#h-1244--cold-fills-of-large-blobs-hold-budget-slots-and-spool-for-the-length-of-their-transfer):
  cold fills of large blobs hold budget slots and spool for their transfer (live where large layers are readable through a Bastion);
- [H-1245](../spec/bastion.md#h-1245--a-caller-on-another-replica-waits-for-a-running-fill-without-a-byte-up-to-fill_wait_max):
  a caller on another replica waits for a running fill without a byte (live on the bucket backend with Valkey and several replicas);
- [H-1246](../spec/bastion.md#h-1246--a-compromised-bastion-can-serve-an-older-genuine-document-of-a-repository): a compromised Bastion
  can serve an older genuine document (live for a taken-over pod);
- [H-1247](../spec/bastion.md#h-1247--the-certificate-admission-of-the-clamav-definitions-does-not-reach-a-bastion): the certificate
  admission of the ClamAV definitions does not reach a Bastion (a limit of the design);
- [H-1248](../spec/bastion.md#h-1248--a-binding-changed-at-the-main-installation-reaches-a-bastion-at-its-next-handshake): a binding
  changed at the main installation reaches a Bastion at its next handshake (dormant until a main installation binds a Bastion's host);
- [H-1290](../spec/bastion.md#h-1290--a-miss-passed-through-during-an-outage-of-the-object-store-is-marked-as-counted): a miss passed
  through during an outage of the object store is marked as counted (live while the object store is unavailable: the Bastion reports
  counted entries);
- [H-1296](../spec/bastion.md#h-1296--a-freshclam-run-through-a-bastion-asks-the-main-installation-once-per-database-for-the-cld-it-never-serves):
  a freshclam run through a Bastion asks the main installation once per database for the `.cld` it never serves (live, a load and not a
  confidentiality matter);
- [H-1325](../spec/bastion.md#h-1325--a-change-of-the-cache-key-loses-the-download-events-spooled-under-the-old-layout): a change of the
  cache key loses the download events spooled under the old layout (live only for a key change during an outage of the main installation);
- [H-1326](../spec/bastion.md#h-1326--after-a-key-change-the-old-tree-stays-readable-to-a-holder-of-the-old-key-until-the-sweep-has-deleted-it):
  after a key change the old tree stays readable to a holder of the old key until the sweep has deleted it (live after every key change;
  it matters for a leaked key);
- [H-1327](../spec/bastion.md#h-1327--a-taken-over-bastion-pod-holds-the-cache-key): a taken-over Bastion pod holds the cache key (live
  wherever a cache key is set);
- [H-1380](../spec/bastion.md#h-1380--changes-the-feed-does-not-carry-reach-registry-token-and-package-narrowed-readers-within-the-revalidation-periods-no-longer-at-their-next-read):
  changes the feed does not carry reach registry-token and package-narrowed readers within the revalidation periods (live for every image
  pulled with a registry token since 2026-11-23);
- [H-1381](../spec/bastion.md#h-1381--the-main-installations-limits-and-blocks-do-not-reach-hits-from-per-package-answers): the main
  installation's limits and blocks do not reach hits from package answers, which the check renews without consulting them (live since
  2026-11-23, Q-BAS-270; the same for full readers through the background refresh, read in the code and not run);
- [H-1382](../spec/bastion.md#h-1382--whoever-holds-the-bastion-token-learns-a-held-credentials-shape-per-package): whoever holds the Bastion
  token learns a held credential's shape per package (dormant until a token leaks);
- [H-1383](../spec/bastion.md#h-1383--per-package-sharing-rests-on-the-cores-notes-and-kit-check-21): sharing per package rests on the
  core's notes and kit check 21 (dormant while every format passes check 21);
- [H-1384](../spec/bastion.md#h-1384--the-first-read-of-a-cached-entry-by-a-credential-without-a-shape-answer-costs-two-requests-an-uncached-one-costs-one):
  the first read of a cached entry by a credential without a shape answer costs two requests, an uncached one costs one (live, a finding read
  in the code and not run; the answers per package add no such signal);
- [H-1390](../spec/bastion.md#h-1390--kit-check-21-runs-over-the-local-class-only): kit check 21 runs over the local class only (live for
  every remote and virtual repository a Bastion serves);
- [H-1395](../spec/bastion.md#h-1395--an-official-image-of-a-docker-hub-remote-without-a-namespace-prefix-names-two-packages-and-carries-none):
  an official image of a Docker Hub remote without a namespace prefix names two packages and carries none, so its pull keeps a conditional
  request per artifact (live, a load and not a confidentiality matter);
- [H-2270](../spec/bastion.md#h-2270--a-replica-learns-another-replicas-writes-to-a-shared-bucket-only-at-its-own-listings): a replica
  learns another replica's writes to a shared bucket only at its own listings, so the bucket's bound is an estimate (live with the bucket
  backend and several replicas; storage, not a pod);
- [H-2271](../spec/bastion.md#h-2271--a-sweep-the-size-asks-for-costs-a-listing-of-the-area-at-most-one-a-second-per-replica): a sweep the
  size asks for costs a listing of the area, at most one a second per replica (live under a sustained churn; a load on the bucket);
- [H-2272](../spec/bastion.md#h-2272--a-churn-of-distinct-artifacts-still-evicts-the-working-set-and-crosses-the-transfer-network): a churn
  of distinct artifacts crosses the transfer network (live wherever a caller can read more distinct content than the cache holds; the
  owner's case of W-34); since E11 it no longer evicts what was served twice, unless the attacker requests each artifact twice (H-2290);
- [H-2276](../spec/bastion.md#h-2276--the-busiest-second-of-an-npm-ci-and-the-requests-in-flight-of-a-large-dotnet-restore-are-not-measured):
  the edge's defaults are set from averages; a large `dotnet restore` on Linux may hold more requests in flight than `edgeLimit.connections`
  (live, a refusal of a legitimate client, not a weakness against a flood);
- [H-2277](../spec/bastion.md#h-2277--many-clients-with-credentials-behind-one-address-are-held-to-one-persons-figures):
  many clients with credentials behind one address are held to one person's figures, and the remedy, `exempt_cidrs`, takes every limit off
  that address.

This page does not describe the main installation's own mechanisms that the Bastion relies on (the decision point, token
validation, the revocation journal); their pages and gaps are linked above.

### H-522 · The chart restricts no egress of the Bastion pods

Stage: the pod network of the Bastion pods. The chart's NetworkPolicy holds ingress rules only
([networkpolicy.yaml](../../deploy/helm/quader-bastion/templates/networkpolicy.yaml),
[R-BAS-047](../spec/bastion.md#r-bas-047--network-policy)), by the owner's decision of 2026-11-05 that
[ADR-154](../decisions.md#adr-154--network-policies-are-ingress-rules-between-the-charts-pods-each-with-its-port) governs this
chart as well. Until then the chart rendered an egress policy, on by default, that let the pods reach DNS, the main installation,
Valkey, the cache bucket and the span collector and nothing else.

Live wherever a Bastion shares a cluster or a routed network with the main installation and the cluster operator sets no egress
policy of their own; dormant where the Bastion's network reaches nothing but the main installation's public address. Subject:
every address the pod network reaches from a Bastion pod, reached by a process that took over a Bastion pod, the component of
Quader built to face the internet. Per case, in the kind profile, where the Bastions run in the namespace `quader-bastion` next to the
main installation in `quader`:

- the main installation's PostgreSQL Service `quader-db-rw:5432` and `registry.npmjs.org:443`. Until 2026-11-05
  `TestBastion_R_BAS_049_NetworkPolicy` ([bastion_scenarios_test.go](../../test/e2e/bastion_scenarios_test.go)) asserted that the
  chart's former egress policy refused a Bastion pod's connections to both, and that a client pod of `quader`, which no egress
  policy restricts, reached both. No NetworkPolicy of the profile selects the PostgreSQL pods, so without the chart's policy a
  Bastion pod reaches them as well;
- the profile's Keycloak (`quader-keycloak:8443`), to which the same test found a Bastion pod's connection refused while that
  policy was in force; no NetworkPolicy of the profile selects Keycloak either;
- not the main installation's Valkey: its own ingress policy `quader-valkey-clients` admits only Quader's serving pods of `quader`
  ([deploy/dev/valkey.yaml](../../deploy/dev/valkey.yaml)). A target that fences itself with an ingress policy stays fenced.

What does not change: the Bastion holds no credential for PostgreSQL, the main installation's object store or upstream registries
(R-BAS-030), so a connection to them is a network path, not access; the Bastion token and the callers' credentials in transit are
what a taken-over pod holds (H-386). Beyond the targets that fence themselves, the in-cluster services, on a cloud node its
metadata endpoint (`169.254.169.254`), and the internet are reachable, so the taken-over pod is a foothold for scanning and for
attacking services that trust the pod network.

What makes it real: a flaw that lets an attacker run code in a Bastion process, in a cluster or network without an egress policy
for the Bastion pods. What closes it is the cluster operator's own egress policy for the Bastion pods, admitting what R-BAS-047
lists: DNS; the main installation's addresses (`bastion.main.url`, and `bastion.main.packages_url` where set) on their ports;
Valkey and its Sentinels while Valkey is configured; the cache bucket with the bucket backend, and its credential endpoint with
`credentials.source` `webIdentity` or `default`; the span collector while tracing is enabled; and, once the handover is built and switched on
for the registration, the public address of every connection of the main installation a handover may name
([bastion-handover.md](bastion-handover.md)). The chart's counterpart for the main installation's pods is
[H-516](egress.md#h-516--the-chart-restricts-no-egress-of-its-pods).

### H-2280 · Whoever answers as the main installation sets a Bastion's limits and switches

Stage: the handshake's member `configuration` (R-BAS-093; packages E7 and E9a). Subject: every figure, switch and rule a Bastion applies.
The member travels since E7 (interface version 8), and since E9a (2026-11-26) a replica applies all of it: the flood figures, the exempt
networks, the slots and the anonymous ceiling. Live wherever an attacker can answer a Bastion's handshake: a certificate for the main installation's host
from a CA the Bastion trusts (the system roots or `bastion.main.ca_file`) together with a position on the path, or a main installation
taken over. What it gains beyond today's forged answers: the web interface switched on, Unlimited figures, no ceiling and no throughput cap
at that Bastion. What still holds: every forward is decided and charged at the real main installation, which a forged handshake does not
reach (two budgets, Q-LIM-306). Without a signing key the stored configuration on a volume is not authenticated either (as
[H-460](../spec/bastion.md#h-460--without-a-signing-key-the-entries-on-the-volume-are-not-authenticated)), so a writer of the volume sets the
figures a restarted replica applies during an outage. No package closes it; TLS verification of the link and a signing key are the
mechanisms.

### H-2281 · The check answer names each caller's subject key and rule to the Bastion

Stage: the check answer's `subject.limits` and the marking's `Quader-Bastion-Limits` (R-BAS-096, Q-BAS-328; packages E7 and E8). Subject:
the users, technical tokens and CI projects whose credentials pass through a Bastion, and the rules that name them. The member and the
header are on the wire since E7 and carry the key and the rule since E8 (2026-11-26). Live.
Today the marking names a presented token's ID and its principal's ID ([H-386](../spec/bastion.md#h-386--whoever-holds-the-bastion-token-can-test-credentials-name-client-addresses-and-add-download-events));
the key adds a technical token's ID and a CI project's issuer and project ID, and the rule's ID tells which callers have a rule of their own
at the Bastion place. What makes it real: the holder of the Bastion token, or a process that took over a Bastion pod, collecting the keys of
the credentials it checks. Accepted with Q-LIM-302: the alternative sends the groups' members instead.

### H-2282 · The usage and the state a Bastion reports are its word

Stage: the reporting batch's `usage` and `status` (R-BAS-016, R-LIM-045; package E8). Subject: the usage overview's rows at a Bastion's
place and the card's live state. Live since E8 (2026-11-26). The holder of the Bastion token, or a taken-over pod, writes rows for any subject key at
its own place (a user who downloaded nothing appears to), leaves its real consumption out, and reports any cache and throughput state. What
it does not reach: the main installation's counters, which never read these rows, so no caller is refused because of them; and another
Bastion's place, since the place is the registration of the token. Bounded by the batch's budget (R-BAS-016) and 100,000 keys a batch
(Q-LIM-308). No package closes it: the place names the Bastion, so a reader can tell whose word a row is.

### H-2283 · The main installation keeps the addresses of a Bastion's blocks

Stage: the transient table `bastion_status` (R-LIM-045; package E8). Subject: the client addresses a Bastion blocked, which are personal
data. Live since E8 (2026-11-26), narrowed by [Q-BAS-443](../spec/bastion.md#q-bas-443--a-replicas-state-is-kept-an-hour-after-its-last-report-without-the-blocks-that-ended):
each replica's report replaces its row and keeps only its blocks that have not ended, at most 100 with their addresses; a row not reported
again for an hour is deleted ([report.go](../../internal/bastionmain/report.go) `statusRetention`), and a read leaves out every block that
ended since. An address therefore stays as long as its block (at most `maxDurationSeconds`, a day by default) and at most an hour after its
replica's last report. Readers are the holders of `endpoints:read`; the table is transient and never in a backup. What remains: a block's
address is kept for the block's whole length, which the card needs to show it.

### H-2354 · A Bastion's batch of up to about 71 MiB is decoded in memory at the main installation

Stage: the intake of `POST /api/v1/bastion/downloads` (R-BAS-016, R-LIM-045; package E8,
[Q-BAS-442](../spec/bastion.md#q-bas-442--a-batchs-body-is-sized-for-100000-usage-rows-and-its-usage-is-stored-after-every-refusal-was-decided)).
Subject: the memory of the replica of the main installation that takes a batch. Live since E8. The body is bounded at `max_batch` × 10 KiB
+ 100,000 × 640 bytes + 256 KiB, about 71 MiB with the defaults, so that a report of 100,000 usage rows fits; the replica reads it whole and
decodes it, which takes a multiple of it in memory. What makes it real: the holder of a Bastion token, or a taken-over Bastion pod, sending
such batches as fast as the intake answers; the events' budget charges events only, not rows. What bounds it: the Bastion token (one per
registration, `bastions.max_registrations`), the API's own request limits of the replica, and the body bound itself. What would close it: a
streaming decoder of the rows, or a smaller bound with the report split by the Bastion.

### H-2355 · The usage a deleted registration's Bastion reported stays in the usage store, named by its ID

Stage: the usage store's rows at the place `bastion:<id>` (R-LIM-045; package E8). Subject: the callers whose use through a Bastion was
reported before its registration was deleted. Live since E8. A deleted registration's rules go with it (the cascade of `limit_assignment`)
and its replicas' states with their contacts, but its usage rows stay for their retention (R-LIM-027: five-minute rows a day, hourly rows
30 days, then daily sums by repository and caller kind) like every row; the overview names their place by the registration's ID, since the
name is gone, and the place filter cannot select them by name (Q-LIM-313). What makes it real: a reader of the overview who expects a
deleted Bastion's callers to disappear with it. What holds: the rows hold what R-LIM-027 allows any row to hold, and the daily task erases
a deleted user's rows (R-LIM-028).

### H-2285 · A replica restarted during an outage without its stored configuration applies the built-in one

Stage: R-BAS-093, Q-BAS-327 (package E9a, built: `settings.Holder.LoadStored`, `configuration/current.json`). Subject: the figures and
switches of a Bastion whose replica restarts while the main installation
cannot be reached. Live with the volume backend on an ephemeral volume (the chart's default) and wherever the stored object's signature does
not hold, during an outage of the link at the restart. The built-in configuration is the preset Internet with every switch off and Enforce:
an administrator's figures stricter than the preset are loosened until the link returns, looser ones tightened, and the web interface is
off. What closes it for an installation: the bucket backend or persistent volumes, which keep the stored object across a restart.

### H-2289 · A Bastion that was taken over ignores its throughput cap

Stage: R-BAS-097 (package E10, built: `throughput.Pacer`), the owner's option (a) of k8, which enforces the cap at the Bastion. Subject: the
network between a Bastion and the main installation, and the object store's egress through handovers. Live for a Bastion that does not run
its own code: since E10 the cap holds for every Bastion that does (it closed
[H-2273](../spec/bastion.md#h-2273--the-bytes-between-a-bastion-and-the-main-installation-are-counted-and-not-bounded)). A process that took
over a Bastion pod reads at the speed the main installation and the object store give it, bounded by the callers' budgets at the main installation (each forward is its caller's request there) and by the main
installation's own limits; a handover's direct read still needs a forward per blob. What would close it: the owner's option (c) of k8, a cap
at the main installation as well, not chosen.

### H-2290 · The protected markers are neither signed nor hard to earn

Stage: R-BAS-098 (package E11, built 2026-11-26). Subject: what a churn of distinct artifacts evicts. Live on every Bastion. The protection
is the access marker itself ([Q-BAS-460](../spec/bastion.md#q-bas-460--the-access-marker-is-the-protection-because-a-fill-writes-none)): a
writer of the bucket or the volume sets or removes markers and steers the sweep, as it steered it with access markers before
([H-453](../spec/bastion.md#h-453--objects-and-markers-are-not-signed-a-replaced-object-is-found-only-when-it-is-read-and-markers-steer-eviction)).
An attacker who requests each distinct artifact twice protects it: the churn costs twice the requests and twice the bytes from the cache
[derived], within the 80 % bound, ranked by recency, so the attacker's reads displace a working set read longer ago than they are
(`TestR_BAS_098_AnAttackerWhoReadsEveryObjectTwiceDoesNotProtectEverything` shows both sides: the attacker holds at most 80 %, and a working
set read twice after it takes the place of its oldest). What bounds it beyond: the subject's budgets and the anonymous ceiling (R-BAS-094,
R-BAS-096) and the throughput (R-BAS-097). What would close it: a protection that a single caller cannot earn alone, for example two serves to
different callers, which needs a caller's identity at the cache and was not in the owner's options.

### H-2362 · A refill by another replica of a shared bucket can stay unprotected for up to an hour

Stage: R-BAS-098, [Q-BAS-462](../spec/bastion.md#q-bas-462--served-twice-means-filled-once-and-served-from-the-same-store-once-more) and
[Q-BAS-464](../spec/bastion.md#q-bas-464--a-replicas-own-commit-lets-the-next-serve-write-the-marker-again). Subject: what a churn evicts on
a Bastion with the bucket backend and several replicas. Live there, dormant on the volume backend. A replica writes an object's marker at
most once per `access_resolution` (one hour by default) and forgets that only for its own commits. When replica B served an object, the sweep
evicted it (it had fallen out of the protected share) and replica A filled it again, B's serves in the rest of B's hour write no marker, and
the object is evicted like one fetched once if no other replica serves it. Two replicas that miss the same artifact at once both fill it,
and neither fill protects it. What makes it real: a working set that had been pushed out of the protected share by more recent reads (H-2290)
and is read again on the same replica within the hour. What would close it: the serve comparing the object's own time with its throttle
(option (b) of Q-BAS-464).

### H-2374 · A HEAD makes the Bastion read a document its caller never receives

Stage: R-BAS-021, [Q-BAS-476](../spec/bastion.md#q-bas-476--a-head-miss-learns-from-its-marking-that-it-addresses-a-document-and-then-sends-the-get)
and [Q-BAS-477](../spec/bastion.md#q-bas-477--the-get-of-a-head-is-sent-only-for-a-document-the-bastion-keeps-in-memory) (package E18,
built). Subject: the transfer from the main installation to the Bastion, an availability property; nothing is read that the caller may
not read. Live on every Bastion since 2026-11-26. A `HEAD` of a document the Bastion does not hold costs a request of a few hundred bytes
and an answer without a body, and makes the Bastion read the document's whole body from the main installation: up to 64 MiB (the memory
bound), 2.7 MiB for an npm package of 1,000 versions [measured: the render benchmark]. A `GET` of the same document makes the Bastion read
the same bytes too (a document is read whole before it is served); the difference is the caller's own side: the `GET`'s caller is sent the
bytes, the `HEAD`'s caller receives a few hundred bytes, so the transfer a caller causes per byte it receives is larger. What bounds it: the document is
kept, so the same key costs nothing more within `metadata_revalidate_after`; each such `HEAD` is two misses of its address and two upstream
requests of its subject, and the anonymous ones take from the anonymous ceiling twice; the slots of R-BAS-025. What makes it real: a caller
that `HEAD`s many distinct large documents (npm package documents of packages with many versions, Debian indexes up to 64 MiB) from many
addresses, at the anonymous ceiling's rate. What would close it: a lower bound of the `GET` for the anonymous caller, or charging the `GET`'s
bytes to the caller's uncached bytes (R-BAS-096) as if it had received them.

### H-2375 · The GET of a HEAD is a download at the main installation

*Closed on 2026-11-26 by W34-E19 (interface version 9, [Q-BAS-479](../spec/bastion.md#q-bas-479--a-get-a-bastion-sends-for-a-head-says-so-in-a-header-of-the-request) to
[Q-BAS-482](../spec/bastion.md#q-bas-482--the-main-installation-honours-the-statement-of-a-head-on-a-documents-route-only)):* the `GET` of a `HEAD` and the conditional `GET` of a `HEAD` carry `Quader-Bastion-For-Head: true`, and
on a document's route the main installation answers them as the `GET`s they are but records no download, no download event and no
download figure for them, as for a `HEAD`. Measured in the area `bastion` (2026-11-26,
`TestR_BAS_059_DockersHeadOfATagIsNoDownloadAtTheMainInstallation`): Docker's sequence through a Bastion recorded three
`artifact.downloaded` at the main installation, the manifest by digest and two blobs of one image, and four with the item store's switch
left out. What remains is what the main installation records for a `HEAD` anyway (its request log). What it was:

Stage: R-BAS-021, R-BAS-059, [Q-BAS-476](../spec/bastion.md#q-bas-476--a-head-miss-learns-from-its-marking-that-it-addresses-a-document-and-then-sends-the-get)
(package E18, built). Subject: the main installation's download count, usage and audit log (`artifact.downloaded`). Live on every
Bastion since 2026-11-26 for the documents the main installation counts when it serves them: an OCI tag's manifest (Q-OCI-222), Debian's
index files, every document the item store serves (R-BAS-059's list); not a document a format renders itself, such as npm's package
documents. Measured in the area `bastion` (2026-11-26):
after one anonymous `HEAD` of a tag through a Bastion the audit log held `artifact.downloaded` for the tag from the caller's address, with
the main installation as origin, although the caller had sent only the `HEAD`, which the main installation counts as no download. It happens
once per fill: a cold `HEAD`, and a revalidation whose `GET` is answered `200` because the document moved; a `304` counts nothing, and the
`HEAD`s the Bastion answers from what it keeps are reported as nothing. A Docker pull through a Bastion whose tag was cold therefore counts
the tag and the manifest by digest, where the same pull at the main installation counts only the manifest by digest. What makes it real: a
report or a budget at the main installation that counts tag downloads of callers who only resolve tags (Docker, containerd, the kubelet).
What would close it: a statement of the forward that it fills for a `HEAD`, which the main installation's counting writer honours, a member of
the interface (a new interface version).

### H-2370 · A Bastion token takes a counted document's download out of the count

Stage: R-BAS-059, [Q-BAS-482](../spec/bastion.md#q-bas-482--the-main-installation-honours-the-statement-of-a-head-on-a-documents-route-only)
(package W34-E19, built on 2026-11-26). Subject: the main installation's download count, statistics and `artifact.downloaded` of the
documents it counts when it serves them: an OCI tag's manifest (Q-OCI-222), Debian's index files, the documents the item store serves.
Live on every main installation with a Bastion since interface version 9. Whoever holds a Bastion token can send
`Quader-Bastion-For-Head: true` on a forwarded `GET` of such a document, and the main installation then records no download for it and
charges it no cached bytes; the request log still records the request with its caller. It cannot do so for an artifact: on a download
route the statement is ignored. What makes it real: a stolen Bastion token, used to read counted documents unseen by the download
reports; a Bastion itself already decides which of its own serves it reports ([H-2282](#h-2282--the-usage-and-the-state-a-bastion-reports-are-its-word)), so a taken-over Bastion gains nothing new. What
would close it: none planned; the request log is the record of what was asked.

### H-2291 · An egress list that no longer names a Bastion's address pools its callers silently

Stage: R-BAS-017, R-UI-243 (package E13, built on 2026-11-26). Subject: the callers of one Bastion at the main installation. Live on every
main installation with a Bastion whose registration lists no network, or none that holds the address it connects from now. When a
Bastion's egress address changes (a cloud NAT, a new node) or the list is empty - the default of a new registration - every caller
through it counts at the main installation as the Bastion's address: one anonymous budget, one failed-credential throttle, one block for
all of them, and the audit log names the Bastion's address. The card warns with the address of each replica's last contact and offers to
add it (Q-UI-1482); nothing tells an administrator who does not open the card. Narrowed by E13: the list is each registration's, so an
address trusted for one Bastion no longer speaks for another, and a token used from outside its registration's networks chooses no
address. What remains of a list that is too wide: a network shared by two Bastions' pods (one node range listed in two registrations)
lets either token forward any client address from any pod of the range, and a wide prefix in one registration's list lets its token do
so from any address in the prefix, which is H-386's power within the prefix.

### H-2348 · A restore brings back a registration's networks as they were at its point in time

Stage: R-BAS-017, R-HA-054 (package E13). Subject: a network an administrator removed from a registration's list. Live once a database is
restored to a point before the removal. A change of the list is recorded (`bastion.updated` with `egressBefore` and `egressAfter`) but not
journaled, so the replay of R-HA-054 does not remove the network again, as it switches a handover off again (R-BAS-071): after the
restore, a request with that registration's token from the removed network forwards its caller's address again. What makes it real: a
network removed because it was no longer the Bastion's (a released NAT address another tenant now holds) and a restore to before the
removal; the card shows the restored list, and removing the network again closes it.

### H-2349 · A Bastion's network that server.trusted_proxies covers is trusted for every request, and only logged

Stage: R-BAS-017, R-ARCH-006 (package E13). Subject: every request from an address that is both in a registration's networks and in
`server.trusted_proxies` of the default endpoint. Live where an operator lists the Bastion's address in `server.trusted_proxies` as well
(the older setup of [Q-BAS-21](../spec/bastion.md#q-bas-21--internet-exposure-limits-blocking-failed-sign-in-timeouts-and-a-configurable-cache)).
Its forwarded headers then count for every request from it, a Bastion token or not, as for any trusted proxy: a workload sharing the
Bastion's egress chooses its client address and its response base URL. The replica logs the overlap once when its snapshot first holds the
network ([Q-BAS-433](../spec/bastion.md#q-bas-433--the-networks-live-in-the-replicas-snapshot-of-the-registrations-with-a-change-area-of-their-own));
the API cannot refuse it, since `server.trusted_proxies` is the configuration file's and the registration knows nothing of it. Removing
the address from `server.trusted_proxies` closes it.

### H-2292 · Anonymous documents are kept for the metadata period

Stage: R-BAS-094, R-BAS-023 (package E14a, built). Subject: anonymous callers of a repository whose metadata changes. Live on every
Bastion whose main installation admits anonymous reads. A narrowing the feed does not carry reaches anonymous callers within
`metadata_revalidate_after` (1 minute by default), as it reaches full readers of a shared document
([H-384](../spec/bastion.md#h-384--a-version-newly-published-appears-at-the-bastion-only-after-the-metadata-period),
[H-385](../spec/bastion.md#h-385--a-withdrawal-the-feed-does-not-carry-reaches-the-bastion-within-the-artifact-revalidation-period)), and
so does a new version. Before E14a an anonymous private document was revalidated on every use. What the feed carries (an unpublish, a
deletion, a block set in triage, a lowered visibility) ends the copy within the feed's bound ([H-2351](#h-2351--a-withdrawal-reaches-an-anonymous-callers-kept-document-within-the-feeds-bound-no-longer-at-its-next-read)).

### H-2293 · A read route with a body that a format does not declare is refused

Stage: R-BAS-095, kit check 19 (package E15). Subject: a client feature through a Bastion, availability only. Live since E15
(2026-11-26). The refusal fails closed: a route with a writing method whose kind is a read and that the format does not list in
`readWithBody` answers `403 read_only` at the Bastion. Kit check 19 holds the routes a format declares; a client that uses a route the
format does not have reaches nothing either way.

### H-2358 · A 502 on a document ansible-galaxy caches ends the client with exit 250

*Closed on 2026-11-26 (the day's findings, batch B):* the `galaxy` shape of the Bastion (`galaxyStatus` of
[shape.go](../../internal/bastion/engine/shape.go)) and Ansible's `WriteError` at the main installation
([errors.go](../../internal/formats/ansible/errors.go)) answer a gateway failure (`502`, `504`) `503` with its code and detail, which
ansible-galaxy 2.17.14 printed and ended with exit 1, where the `502` had ended it with exit 250 (both run once, [spec/bastion.md](../spec/bastion.md)
R-BAS-095's table). What it was:

Stage: the Bastion's `502 bad_gateway` (R-BAS-021, R-BAS-099 row 6) and the main installation's `502 upstream_unavailable` of a remote
Galaxy (formats/ansible.md R-ANS-007). Subject: Ansible Galaxy's clients, availability and the readability of a failure only. Live:
ansible-galaxy 2.17 and 2.21 retry a `502` as they retry a `429`, and the retry of a version list (the one document of an install it caches) reads the
empty entry its first attempt left in the client's response cache, which ends the command with `Unexpected Exception … 'results'`
(exit 250, 2.17) instead of the answer's detail (the mechanism measured for `429` on 2026-11-26, [spec/bastion.md](../spec/bastion.md)
Q-BAS-451; for `502` read from the client's source, not run). Rate limits answer `503` since E15, which the client does not retry. What
makes it real: a main installation that does not answer a forward, a `Location` on another host, or a remote Galaxy that fails. What
closes it: the `galaxy` shape and the format's `WriteError` answering a gateway failure `503` on a Galaxy route, or a client that
writes no empty entry.

### H-2359 · Before its first handshake a Bastion answers in problem details

*Closed on 2026-11-26 (the day's findings, batch B):* the stored configuration (R-BAS-093) holds the format table of the same
handshake, and a replica that applies it at start holds that table until its first handshake (`settings.Holder.LoadStored`,
`link.Link.UseStoredFormats`). What remains is a replica that has never stored one (its first start, an empty volume, a forged or
refused object): it answers in problem details as below, with the right status and `Retry-After`. What it was:

Stage: the Bastion's own answers (R-BAS-029, R-BAS-095). Subject: what a client prints while a replica has never reached the main
installation, readability only. Live: the format table, and with it each format's `errorShape`, comes with the handshake; a replica that
started while the main installation was unreachable answers its `503 unavailable` (and a write's `403 read_only`) as problem details on
every family but `/v2/`, which npm and ansible-galaxy print without the detail, and refuses a read route with a body `403 read_only`,
since no format has declared one yet. The status and `Retry-After` of the `503` are right. What closes it: a replica that keeps the last
format table across a restart, as E9a keeps the configuration (R-BAS-093, Q-BAS-327); no package plans it.

### H-2294 · A Bastion's answers still tell that an installation stands behind it

Stage: R-BAS-099 (package E16). Subject: what an internet caller learns of the main installation's existence, not of its identity. Live,
inherent: a miss takes longer than a hit, a `503 unavailable` follows an outage of the link, the address of a repository carries its
project's key (INV-5), and the registry token's grant names the repositories a caller asked for. The owner's rule hides the main
installation's name, address and structure; that a Bastion is a cache in front of something is its purpose. Since E16 the texts say "this
service" and "this address" and the problem type is `about:blank`; until E7 renames them, the codes themselves (`unavailable`,
`bad_gateway`, `gateway_timeout`, `busy`, `not_found`, `read_only`) still tell a caller that a Bastion answers
and that a main installation stands behind it.

### H-2295 · Problem texts the main installation relays are inventoried only in part

Stage: R-BAS-099 rows 7 and 15 (package E16), R-BAS-114 (W-35's P8). Subject: the texts of the main installation's problems a forward
relays. Closed by E16 for the remote engine: a forward reads "The package could not be fetched right now." for every code of the remote
engine and its upstream clients, whatever text made it ([Q-BAS-426](../spec/bastion.md#q-bas-426--a-forward-reads-a-neutral-detail-for-every-failure-of-the-remote-engine-chosen-by-the-code-when-the-problem-is-rendered)),
and kit check 19 holds every format's error shape to the rendering that applies it. Closed by P8 for the rest as it stood on
2026-11-26: every text of the formats and the core on a forwarded route was read, the six that named a member, a page of the web
interface or a finding carry their forward text (R-BAS-029's table of what a forward relays), and every status of 500 or more that is
no rate limit reads the neutral text of its status unless its error vouches for its own, so a server's failure fails closed
([Q-BAS-386](../spec/bastion.md#q-bas-386--every-5xx-of-a-forward-reads-the-neutral-text-of-its-status-unless-its-error-vouches-for-its-own)).
What stays live is a 4xx text written after P8 ([H-2323](#h-2323--a-4xx-text-written-later-reaches-a-forward-as-written-until-it-is-given-its-forward-text)),
and a writer of errors that renders a request's problem with `errs.ProblemOf` instead of `errs.ProblemFor`, which would bypass the rule;
none outside the formats exists today, and the formats are held by the kit.

### H-2296 · An administrator's or an author's words in content are passed on

Stage: R-BAS-099 rows 10 and 11. Subject: names an administrator or a package's author wrote into content. Live: a repository's Debian
`Origin` (default `quader`), a package's homepage and repository links, an image's labels and a README are content the main
installation serves as written; an internal host written there reaches every caller of a Bastion. Content is not filtered by design (W-35
keeps the authors' links); what an installation keeps out of its public content is its own decision.

### H-2297 · A registry token's readable grant tells its holder which of the names it asked for it may pull

Stage: R-OCI-005's payload version 2, Q-BAS-339, R-BAS-115 (package E17, built on 2026-11-26 with W-35's P8). Subject: a caller with a
registry token, about its own grant. Live: the grant `g` stays readable, so a caller learns which of the repositories and images it asked
for the token service granted, which a pull with the token tells as well; the Bastion reads it to bound what a token's answer opens
(Q-BAS-246). Every other claim is sealed under a key of the ring and the key ID is opaque
([Q-BAS-388](../spec/bastion.md#q-bas-388--the-sealed-part-of-a-registry-token-is-encrypted-under-a-key-of-the-ring-not-a-reference)); a
token of version 1, every claim readable, issued before the upgrade, is accepted until it expires, at most `oci.token_lifetime` later.

### H-2322 · An Ansible Galaxy role served through a virtual repository names the member in its ID

Stage: R-ANS-022's roles of a virtual repository (`encodeRoleID` in
[virtual_roles.go](../../internal/formats/ansible/virtual_roles.go)), R-BAS-099 (W-35's P8, found and not fixed). Subject: a caller of a
Bastion that searches or reads roles through a virtual Ansible repository. Live wherever a virtual Ansible repository has a remote member
that serves the v1 role API: the ID of every role in a search, a lookup and a version list, and the role URLs in them, are
`<member key>-<upstream's id>`, so the answer names the key of a member the caller did not address. It is a body of `200`, not a problem,
so R-BAS-114's rendering does not reach it. What would close it: an ID the virtual repository can map back without naming the member, for
example a short keyed hash of the member's key; the virtual role code and its tests change, nothing else.

### H-2323 · A 4xx text written later reaches a forward as written until it is given its forward text

Stage: R-BAS-114 (W-35's P8). Subject: the problems of a status below 500 that the main installation's formats and core write on the
routes a Bastion forwards. Live by design: a 4xx keeps its own text for a caller of a Bastion, because the client needs it (the package
and version that were not found or refused), so a text added after P8 that names a member, a page or an identifier of the installation
reaches the internet until it is given `errs.WithForwardDetail`. What holds it: the replays' scan of R-BAS-099 row 10 for the texts a
replay meets, and the review of a new text against R-BAS-029's table; no test fails for a text no replay reaches.

### H-2350 · Anyone can mint an anonymous registry token of a Bastion

Stage: R-BAS-094, Q-BAS-335, Q-BAS-435 (package E14a, built). Subject: what an anonymous registry token is worth, and what the main
installation counts of anonymous pulls. Live on every Bastion whose main installation admits anonymous reads. The token's MAC is random
bytes that nobody verifies, so whoever writes `1:bastion:{"a":true,…}` in the registry token's form, or any token whose readable claim
`a` is true, has one, at any Bastion; and any Bastion drops it and serves the anonymous caller (Q-BAS-168). What it gains: exactly what a
request without a credential gains there, nothing more: the anonymous caller's answers, its limits and slots, its ceiling (since E9a).
At the main installation the key ID `bastion` names no key, so the token is refused `401` there. What it costs the operator: the main
installation's `quader_oci_registry_tokens_total{outcome="anonymous"}` and its request log no longer count the anonymous pulls through a
Bastion; the Bastion's `quader_bastion_requests_total{class="pass",result="hit"}` counts them with its other own `pass` answers. What would
close the counting: a series of the Bastion's own for its tokens, not added because every token request already shows in the Bastion's
request log with its path.

### H-2351 · A withdrawal reaches an anonymous caller's kept document within the feed's bound, no longer at its next read

Stage: R-BAS-094, R-BAS-023, Q-BAS-437 (package E14a, built). Subject: anonymous callers of a repository the main installation filters for
them (for example one whose artifacts are public and whose wiki is not, Q-BAS-149). Live on every Bastion whose main
installation admits anonymous reads. Before E14a the anonymous caller's document was revalidated on every use, so an unpublish, a deletion,
a block set in triage or a lowered visibility reached it at its next read. Now the content and grants entries of the feed end the copy
(rule 3 of R-BAS-020, `freshFor`), which reaches every replica within the feed's poll interval and one round trip, the bound of the
revocations (R-BAS-026: within 5 s); a full reader's shared document has the same bound. A withdrawal the feed
does not carry is H-2292's. What makes it real: an anonymous `npm install` through a Bastion within the seconds after an unpublish.

### H-2305 · The Bastion token reads its registration's public catalogue in bulk

Stage: the catalogue routes (R-BAS-101, R-BAS-102; W-35's P5, built; the builder P6 fills them). Subject: the public packages, versions,
open findings, signatures, documentation and set-up snippets of the repositories a registration's catalogue shows. Live wherever a registration's `gui` is
on and its Bastion token is (P6 builds the catalogue since 2026-11-26). Its holder reads the whole
catalogue at once, with the change list as an index, where an anonymous visitor reads the same pages one at a time through the main
installation's public reads or the Bastion's pages; nothing in the catalogue is closed to the anonymous caller, so the gain is speed and
completeness, and, with public remotes in the catalogue, the company's dependency footprint in one pull (W-35's D3). What makes it real: a
leaked token Secret or a taken-over Bastion pod. What holds: the token reads no other registration's catalogue and nothing while `gui` is
off; revoking the token ends it.

### H-2306 · Every Bastion learns when some catalogue or some configuration changed

Stage: the feed entries of the kinds `catalogue` and `settings` (R-BAS-015, R-BAS-093, R-BAS-101; Q-BAS-349). Subject: the times at which
the main installation published into a public repository or changed a Bastion's configuration. Live for `settings` since phase 5b (since
W-35's P9e also for a change of the installation's default form of a time, `system.time_format_changed`); dormant for `catalogue`, since
the main installation writes no entry of that kind (Q-BAS-378). An entry names no registration, so a Bastion reads the times of changes that concern other
registrations, and from the catalogue entries the times of publishes into public repositories of any registration's catalogue. What makes it
real: a holder of any Bastion token reading the feed. What holds: an entry carries no repository, package or registration, and the publish
itself is public once it is in a catalogue. Naming the registration would tell every Bastion the others' IDs instead.

### H-2307 · A Bastion's configuration tells its holder the rules and networks of its place

Stage: the handshake's `configuration.limits` (R-BAS-093, R-BAS-096; the wire since E7, the content since E8). Subject: the figures of each
rule of the Bastion's place, its rule IDs, its address rules (prefixes such as a partner's network) and its exempt networks. Live since E8
(2026-11-26) wherever the Bastion token is, or a Bastion pod is taken over. Its holder learns which networks the administrator exempts or treats differently and how
much each kind of caller may take, which helps an attacker size a flood below the figures and pick an address range that is exempt. What
holds: a configuration names no user, group or profile (Q-BAS-320 sends each rule's effective figures, not the profiles), and the figures
bound only what this Bastion admits; the main installation charges every forward to the caller there (Q-LIM-306).

### H-2308 · A bare host name of the main installation in author text is not rewritten

Stage: the link rewrite of the builder (R-BAS-107, Q-BAS-365). Subject: a README, a docs entry or a package's metadata whose author wrote the
main installation's host without a scheme (`quader.corp.example/team`), or a host of it no base URL names. The rewrite replaces origins
(`scheme://host[:port]`) of `server.base_url`, `server.additional_base_urls` and the further endpoints; a bare host name passes. Live
wherever an author names the internal host in a public package's text; what makes it real is the author, not the builder. What holds: the
links the main installation renders itself are on the Bastion's base URL; the integration test scans every document for the origins.

### H-2309 · The strip knows the members the main installation writes today

Stage: the strip of the builder (R-BAS-106). Subject: a member a later change of an answer adds that names an internal identity or the
infrastructure under another name than the strip's (`releasedBy`, `origin`, the triage members, `upstreamUrl`, a principal's reference).
Dormant: no answer the builder reads carries one today (the unit and the integration tests scan for the fixtures' principals and hosts).
What makes it real: a new member in a package, version or signatures answer. What holds: a principal's reference is removed wherever it
stands, whatever the member's name; the review of a change to these answers is the strip's other half.

### H-2310 · A version's state shows the times of its package's last render

Stage: the reconciliation (R-BAS-105, Q-BAS-359). Subject: the times in `packages/<id>/versions/<v>/state`: when a version was scanned, when
its findings were checked, the malware database a verdict was made with. The fingerprint holds no time, so a rescan or a sync that finds
the same renders nothing, and the state keeps the times of the render before. Live: a visitor reads "checked at" older than the last check,
up to the package's next change. What holds: what was found is current; only its date is older. The time of the catalogue's own runs is the
Bastion's age of P7.

### H-2311 · A narrowing stays in the catalogue until the next run

Stage: the trigger and the job (R-BAS-105). Subject: a document of a package whose repository's level was lowered, whose grant was removed
or whose version was blocked. The catalogue still holds it from the change's commit until the run that the change starts 5 s later has
written its deletion; a run that is already rendering a large registration takes the change only when it ends. Live with every narrowing;
the window is the debounce plus a run (measured 0.9 to 1.7 s for three packages in the integration suite). A signal missed (Valkey
unavailable) leaves a change to the hourly reconciliation, but a level, a grant and a block arrive through change areas (`repo_config`,
`authz`), whose poll does not lose one. What holds: the package API of a Bastion decides every download at the main installation (R-BAS-011);
the catalogue only shows.

### H-2312 · What the fingerprint does not see waits for another change of the package

Stage: the fingerprint (R-BAS-105, Q-BAS-360). Subject: a change of a document that none of the fingerprint's facts records: the content of
an Ansible documentation build of a version that had one already, a release of the signature gate, a change of a repository's settings that
shows in a set-up of a version (the set-ups per repository are rendered every run). Live but narrow; the documents follow at the package's
next change, or at a full render when the salt changes (the public set, `docs`, the base URL, the README policy). What holds: none of these
widens what a visitor reads beyond what the anonymous caller reads; a narrowing of access is a change of the public set or of the facts.

### H-2313 · A disabled registration's catalogue is kept, and a deleted one's documents go at the next sweep

Stage: the builder (R-BAS-104, R-BAS-108). Subject: the documents of a registration that was disabled or deleted. A disabled registration's
catalogue is built on, so that an enable finds it current (R-BAS-133); its routes answer `503 bastion_disabled` meanwhile, so nothing is
served from them. A deleted registration takes its rows at once (the foreign key), its documents with the next sweep. Live for a disabled
registration as long as it is disabled: the rows exist, read by nobody.

### H-2314 · A narrowing reaches a Bastion's pages only at its next poll after the run

Stage: the change list (R-BAS-101, Q-BAS-369). Subject: a deletion the builder wrote. The builder writes no feed entry of the kind
`catalogue` yet, so a Bastion learns of it at its next read of the change list (P7's poll, 10 s by default), not within the feed's seconds.
Live once P7 serves pages: a lowered level is visible on a Bastion's pages for the debounce, the run and up to one poll. What holds: the
Bastion's downloads are decided at the main installation; P7 revisits the feed entry. *Revisited by W-35's P7 on 2026-11-26
([Q-BAS-378](../spec/bastion.md#q-bas-378--no-feed-entry-of-the-kind-catalogue-the-follow-up-round-after-the-builders-debounce-carries-a-change)):*
a narrowing of access arrives at the Bastion as a feed entry of the kinds `grants`, `settings`, `principal` or `content` within the feed's
seconds, and the replica runs a round at once and a second one 8 s later, after the builder's debounce and its run, so the deletion reaches
its pages without waiting for the poll (measured in the integration suite: a project lowered to private was gone from two Bastions' pages
7 s after the change). What waits for the poll is a change the feed carries no entry for: a publish (`package.published` has none,
R-BAS-015) and a narrowing the builder renders later than 8 s after its entry (a run of a large registration).

### H-2315 · A replica holds the catalogue's map of paths and its index in memory

Stage: the sync and the reader (R-BAS-109, R-BAS-113, Q-BAS-373). Subject: the memory of a replica whose registration has a large
public catalogue. Each replica keeps which path names which document (about 150 bytes a path) and the index of the public packages in
memory, and a change list after a reset or a first start is read whole before the round swaps. Derived from the measurement of
2026-11-26 (14 documents per version): about 200 MB of map for 100,000 public versions; not measured at that size. Since W-40's P2 it
also holds the index of every public project and repository (R-BAS-130): derived from W-40's figures, about 0.93 KiB per package per
scope, so a package in one project and one repository adds about 1.9 KiB (about 56 MiB for 30,000 public packages), less where two
scopes' pages are the same bytes, which share their entries. Dormant until a
catalogue of that size; the pod's memory limit is the operator's. What holds: the map is cloned only in the shards a round changed, so a
round of one publish costs a shard, not the map.

### H-2316 · Without a signing key the catalogue's state record is not authenticated

Stage: the store (R-BAS-110, Q-BAS-374). Subject: someone who can write the cache volume of a Bastion that runs the volume backend without
Valkey and has no signing key, which only the bucket backend and Valkey require (Q-BAS-98). The state record is then stored plain, so such a writer can make a
replica serve, after its next start, a catalogue of the writer's making: the documents are held to the addresses the record's shards name,
and the writer writes those too. Dormant: it needs write access to the pod's volume, which is already the cache's (H-460's case). What
holds: with a signing key, which the bucket backend and Valkey require, the record is signed in the domain `catalogue`; with a cache key it
is sealed and bound to its name.

### H-2317 · A catalogue above its share is kept, and the cache may pass its size

Stage: the store (R-BAS-110, Q-BAS-380). Subject: the disk or the bucket of a Bastion whose public catalogue grew beyond
`bastion.gui.max_bytes`. The catalogue is kept whole (the web interface serves it whole), so the cache's three parts may hold more than
`cache.max_bytes`; `QuaderBastionCatalogueOverBound` fires. Dormant: the default of 4 GiB holds about 290,000 public versions of the
measured mix (derived, developer/bastion.md). What holds: the artifacts and the documents keep their limits; the volume's size is
checked against `max_bytes` plus the spool by the chart, so a catalogue far above its share fills the volume's headroom first.

### H-2318 · A replica without a kept catalogue shows nothing after a restart during an outage

Stage: the start (R-BAS-111, W-35's D14). Subject: the visitors of a Bastion on the chart's default ephemeral volume. A replica restarted
while the main installation is unreachable finds no state and answers every page not ready until the link returns; other replicas serve
on. Live with the ephemeral volume; the registration's card says so from the replica's `Quader-Bastion-Catalogue-Kept`, and
`QuaderBastionCatalogueMissing` fires after 15 minutes. What holds: the bucket backend and a persistent volume keep the state.

### H-2319 · A narrowing made during an outage stays on the pages until the link returns

Stage: serving on (R-BAS-111, the owner's B.2b, W-35's D10). Subject: a package whose level was lowered while the Bastion cannot reach
the main installation. The pages serve their last state whatever its age, so the package stays visible until the first round after the
link returns. Live during any outage, by the owner's choice against a grace period. What holds: every page says how old it is,
`QuaderBastionCatalogueStale` fires after `stale_after`, and a download of the package is decided at the main installation, so it fails
once the package API's grace period ends (R-BAS-027).

### H-2320 · A replica of a shared bucket whose state is an hour behind can find its documents swept

Stage: the sweep of the catalogue's tree (R-BAS-110, Q-BAS-375). Subject: a replica of a bucket without Valkey that could not sync for
more than an hour while another replica could. The other replica's sweep deletes the documents no state it knows names once they are an
hour old, which may be documents the lagging replica still names; it answers those not found and fetches them at its next round. Dormant:
it needs replicas of one Bastion that reach the main installation differently for an hour. What holds: the documents are content-addressed,
so nothing wrong is served, only nothing.

### H-2321 · A package still public in another repository shows what a narrowed one held until the run

Stage: the reader's hiding (R-BAS-112, Q-BAS-376). Subject: a package held by two public repositories of which one is narrowed. The
reader hides by package: the package stays shown because one of its repositories is public, and its documents still list the versions only
the narrowed repository held until the builder's run renders the package again, which the replica takes at its round 8 s after the feed's
entry or at its poll. Live for those seconds. What holds: the package's downloads are decided at the main installation per version
(R-BAS-084).

### H-2324 · A page of one release asks a replica of another for a file its bundle lacks

Stage: the web interface's files (R-BAS-116, W-35's P9). Subject: a visitor whose page was loaded while the Bastion's replicas roll from
one release to the next. The main installation keeps every release's hashed files in a shared store (R-HA-020); a Bastion has none: a
replica answers a hashed file its own bundle lacks `404`. Live during every rolling upgrade of a Bastion with more than one replica: a
screen the page has not loaded yet (a lazy chunk) fails to load from a replica of the other release, and the application loads the page
again from the server (the main installation's rule of a screen whose script is gone, Q-UI-1370), which then comes from either release.
What would close it: W-35's plan named a store in the cache backend (`catalogue/assets/<locale>/…`) that every replica publishes its
bundle to at start, as the main installation's keeper does; it was not built, since the reload already ends in a working page and a
second store under the catalogue's tree would need its own sweep.

### H-2325 · The web interface shares its origin with the package API

Stage: the client listener (R-BAS-116, W-35's D2). Subject: a visitor whose browser answered the Bastion's `WWW-Authenticate: Basic` for a
path of the package API that is not public. The browser keeps those credentials for the origin and sends them with every later request to
it, the web interface's included. Dormant: the web interface reads no credential and sends none on (it reads the catalogue only), and a
script of the page's own origin cannot read them; it would take author HTML that runs on the page (D1, which the sanitizer, Angular's and
Trusted Types hold) to make a top-level navigation carry the visitor somewhere with them. What would close it: a host of its own for the
web interface, which needs several base URLs per Bastion (deferred).

### H-2326 · Many addresses cost a Bastion's CPU and bandwidth through its pages

Stage: the class `gui` (R-BAS-117, Q-LIM-307, W-35's D9). Subject: the availability of a Bastion's web interface and package API. The
class bounds each address (20 requests a second, a burst of 100); a scraper or a crawler spread over many addresses is bounded only by
`server.max_in_flight` and the network in front (H-382). Live against the Bastion alone: every answer comes from the replica's memory and
its cache backend, so the main installation pays nothing (R-BAS-116), and a document is sent compressed as stored (Q-BAS-397). The search is
a scan of the index in memory per request (Q-BAS-383).

### H-2327 · Authors' HTML is shown on the Bastion's origin

Stage: the pages of a version (R-UI-229, W-35's D1). Subject: a publisher of a public package, or an upstream author behind a public
remote, whose README, documentation or changelog the Bastion's pages show. The HTML is the main installation's sanitized output
(R-UI-008's allowlist on the server), stored in the catalogue as it was rendered; the page binds it through Angular's sanitizer and under
the page's Trusted Types policy (R-BAS-117), so a script, a handler or a frame does not run. Dormant: it takes a flaw in all three layers.
A tightened server allowlist reaches the catalogue only when the builder renders the document again: the policy's hash, the README
renderer's version and the Ansible markup converter's version are in the salt of every unit, so a change of any of them renders every
document at the builder's next run (R-BAS-124, Q-BAS-419); the converter's version is raised by hand with its output. What makes it real:
the same origin serves the package API (H-2325), so a script that ran would act with what the browser holds for the origin.

### H-2338 · A search of the documentation across packages scans every entry of the newest versions

Stage: `/ui/data/v1/search/documentation` (R-BAS-125, Q-BAS-421, W-35's D9). Subject: the CPU of a Bastion's replica. Every request scans
the index of the entries of every public package's newest version in memory, matching each word of the text against each entry's search
words, and sorts the hits. Measured once on 2026-11-26 (Apple M-series, in process): 100,000 entries of 2,000 collections, a word every
entry holds takes 59 ms, one that few hold 8 to 17 ms. Live against the Bastion alone, as H-2326: the class `gui` bounds each address
(20 requests a second), so one address can keep about one core busy with the broadest word; many addresses are bounded only by
`server.max_in_flight` and the network in front. The main installation pays nothing. What would bound it: an index by word prefix, as the
main installation's statement reads through `ansible_doc_entries_search`.

### H-2339 · A replica holds every public package's newest documentation entries in memory

Stage: the documentation index of the catalogue (R-BAS-125, Q-BAS-421). Subject: the memory of a Bastion's replica. Each round keeps, beside
the index of packages (H-2315), every entry of the documentation tree of each public package's newest version with its search words (at
most 4 KiB of words per entry at the main installation, `rows.MaxDocSearchText`, fewer after the reduction of Q-BAS-418). Measured once on
2026-11-26: 100,000 entries of twenty short words take about 57 MiB. Live with docs on: no bound of its own caps it; the catalogue's share
(`bastion.gui.max_bytes`) bounds the documents kept, not the index built from them. What makes it real: a public remote of a large
Galaxy mirror, whose every collection's newest documentation enters the index.

### H-2330 · The catalogue holds public content while the default endpoint admits no request without a credential

*Closed on 2026-11-27 by W35-P9f ([Q-BAS-495](../spec/bastion.md#q-bas-495--while-the-default-endpoint-refuses-anonymous-requests-every-catalogue-is-empty-with-its-reason),
[Q-BAS-496](../spec/bastion.md#q-bas-496--a-replica-that-claims-jobs-starts-a-run-at-its-start-only-when-the-store-differs-from-what-a-run-would-leave)):*
the builder takes the default endpoint's `auth.anonymous` from the configuration, as the handshake's `anonymousReads` does, and while it is
`false` builds nothing: each registration's catalogue is emptied once (its documents, its public set, and a Bastion's cursor answers
`reset`), and the state names `anonymous_requests_refused` for the card. A replica that claims jobs and starts with the setting changed
starts a run within the debounce. Tested by `TestR_BAS_104_NothingIsBuiltWhileTheDefaultEndpointRefusesAnonymousRequests` and
`TestR_BAS_104_TheBuilderTakesTheDefaultEndpointsAnonymousSetting` (unit). What remains, and it is bounded: from the restart that closes
the endpoint until the run, the debounce and the run itself (seconds), the catalogue routes still answer the old catalogue, and each
replica of a Bastion empties its copy at its first round after the run (`bastion.gui.poll_interval`, 10 s), or, cut off from the main
installation, when the link returns ([H-2319](#h-2319--a-narrowing-made-during-an-outage-stays-on-the-pages-until-the-link-returns)).
During a rolling restart that changes the setting, a replica that claims jobs with the old configuration may run the builder after one
with the new configuration and build the catalogue again; the next run of a new replica empties it, at the latest the hourly
reconciliation. What makes that real: a job claimed by an old replica after the new ones started, while it terminates. What it was:

Stage: the builder's renderer (R-BAS-104, Q-BAS-355; `catalogueRenderer` of
[wire_bastion_catalogue.go](../../internal/app/wire_bastion_catalogue.go)). Subject: the public repositories of an installation whose
default endpoint sets `auth.anonymous: false`, explicitly or because its listener requires a client certificate (R-EP-010). The renderer's
chain has no authentication stage, so every read is the anonymous principal's and is decided by the levels alone; the endpoint's
`auth.anonymous`, which the authentication stage of a real request applies, is never asked. A registration with `gui` on therefore shows
every repository whose level is `public`, while the same installation answers an anonymous request at its default endpoint `401`, and
while the Bastion's own package API refuses the anonymous downloads its pages link (R-BAS-019). Read in the code on 2026-11-27, not run.
Dormant while the default endpoint admits anonymous requests, which it does by default; live where an operator closed it and switches
`gui` on. What makes it real: an operator who relies on `auth.anonymous: false` to keep `public` repositories closed. What holds: only
what a `public` level opens is shown, and switching `gui` on is the operator's act. What would close it: the builder asks the default
endpoint's `AnonymousAllowed` and builds nothing (or `gui` is refused) while it is false, as R-BAS-104's sentence "what an anonymous
visitor reads through the default endpoint" says.

### H-2334 · A search engine keeps a page after its content became private, until it visits again

Stage: the pages' status and the sitemap (R-BAS-121, R-BAS-122, W-35's D15). Subject: a package or a project whose level an administrator
lowered, or a version deleted, after a search engine indexed its page. The Bastion hides it within seconds of the builder's run (R-BAS-112)
and answers its address `404`, and the sitemap of the next revision no longer lists it; the search engine's index, its snippet and its
cached copy keep the name, the description and the README's start until the crawler visits the address again, days to weeks for a page of
little traffic. Live, inherent to indexing: nothing the Bastion answers can reach into an index. What makes it real: making public what was
meant to be internal, even for an hour. What would narrow it: the search engines' removal tools, used by the operator for the address.

### H-2335 · The README in a page's text is the catalogue's stored HTML, inserted as it was rendered

Stage: the page's text (R-BAS-121, W-35's D1). Subject: a publisher of a public package, or an upstream author behind a public remote. The
text inside the root element carries the README the main installation rendered and sanitized (R-UI-008's allowlist), as the catalogue
stored it: the Bastion does not sanitize it again, and Angular's sanitizer, which binds it on the application's page (H-2327), does not run
for the text. Dormant: the page's policy runs no inline script, no handler and no script of another origin, and Trusted Types hold, so markup
the server's allowlist let through would show, not run; the text is hidden in a browser that runs the application. What makes it real: a
flaw in the main installation's allowlist together with a way past the page's policy; a tightened allowlist reaches the catalogue only when
the builder renders the document again.

### H-2336 · The sitemap lists every public package for anyone in one fetch

Stage: the sitemap (R-BAS-122, W-35's D3). Subject: the company's public packages and, with a public remote, the dependency footprint of its
builds. The catalogue's search and the pages already enumerate what is public; the sitemap lists it in one file of up to 50,000 addresses,
and a search engine makes it findable by a web search. Live by the owner's answer that public remotes are part of the catalogue: a public
npmjs.com or Docker Hub remote lists everything internal builds pulled. The only mitigation is not to make such a remote public; the manual says so
([What becomes public](../manual/admin/bastion.md#what-becomes-public), W-35's P12), the card's sentence of the switch does not name
remotes.

### H-2337 · A page a crawler reads costs the Bastion its reads, and a crawler that ignores robots.txt reaches the package API

Stage: the pages' head and text and the class `gui` (R-BAS-121, R-BAS-117, W-35's D9, D12). Subject: the availability of a Bastion. A page's
head and text read two or three documents of the catalogue per request and keep nothing per address
([Q-BAS-410](../spec/bastion.md#q-bas-410--a-pages-head-and-text-are-written-per-request-without-a-cache)); the sitemap is built once per
revision. A crawler spread over many addresses is bounded by the class per address and `server.max_in_flight` alone (H-2326). A crawler that
ignores `robots.txt` and `nofollow` follows a version's download links into the package API, where it is an anonymous client under the miss
limits and the anonymous ceiling (R-BAS-094): it can make the main installation fill what the Bastion does not hold yet. Live against the
Bastion's CPU for the first; dormant for the second while the anonymous limits hold.

### H-2328 · A generic file's signatures are a badge alone at the Bastion

Stage: the page of a public generic repository's file (R-UI-229, W-35's P9d, Q-UI-1457). Subject: a visitor who checks who signed a file
before using it. The catalogue keeps a file's description, whose member `signatures` carries the state the badge shows (verified, by an
anchor of the installation or a public one, or none), but no report of the file's signatures (the signer, the anchor, the time proof, the
origin): the builder renders none for a file (R-BAS-104), and the page offers no section Signatures. Live wherever the signatures are on
and a public generic repository holds signed files. What a visitor can still do: download the signature files beside the file (`.sig`,
`.asc`, `.sigstore.json`, R-SIG-032), which the folder lists, and verify them offline against the anchors the catalogue serves
(`signing/trusted-root.json`, `signing/chain.pem`). What would close it: a file's report as a document of the catalogue
(`files/<project>/<key>/signatures/<path…>`), a new form of path in the grammar of R-BAS-103 and so a change of the interface between the
Bastion and the main installation.

### H-2329 · A generic file's malware verdict at the Bastion shows no date of its database

Stage: the page of a public generic repository's file (R-UI-229, W-35's P9d). Subject: a visitor who weighs "No malware found" by how
current the ClamAV database was (W-35's B.9: the verdict "with the ClamAV database version and date"). The builder takes the times of a
check out of every document (`scannedAt`, `database`, `databaseBuiltAt`, R-BAS-103), so that a rescan that finds the same changes no
document, and puts them into the state of a package's version, from which the Bastion's reads put them back (Q-UI-1451). A file of a
generic repository has no version and no state document: its description keeps the verdict and loses the database's date, which the
main installation's file page shows beside it. Live for every public generic file a scan has judged. What would close it: a state document
per repository's files that holds the times by path, or keeping the times in a file's description at the cost of a changed document
after every database update; either is a change of what the catalogue holds. W-36's P8, which brought the malware mark to the Bastion's
pages, left it ([Q-BAS-493](../spec/bastion.md#q-bas-493--a-generic-file-at-a-bastion-carries-no-time-of-its-check)): the mark of a
generic file says its word and the scanner, no date and no class.

### H-2439 · With Hide repositories the pages name no repository, but the data a browser reads keep each version's holders, and every command names a key

Stage: the switch Hide repositories at the Bastion (R-BAS-131, W-40's P2,
[Q-BAS-504](../spec/bastion.md#q-bas-504--hide-repositories-hides-every-repository-a-visitor-reads-and-keeps-the-key-a-client-needs)).
Subject: an operator who switches it on to keep the company's repository layout (which repositories exist, which are remotes or virtual,
what each is called) from visitors. The pages, the crawler's text and the sitemap name no repository, and the data's list of
repositories, their indexes, a repository's class and description and a remote's upstream are gone at once. What stays readable to
anyone who reads the data below `/ui/data/v1/` instead of the page: a version's document lists its holders with their keys and classes
(the shared views' rules read them to pick a command), a single repository's data answer its name and format, and every command, set-up
snippet and download address carries the project's and the repository's key, since the Bastion mirrors the main installation's address
space (Q-BAS-17). A visitor who wants the layout learns the keys and classes of every repository that holds a public version. Live
whenever the switch is on: it hides the presentation, not the facts. What would close it: documents without the holders' classes and a
command order that needs none (W-40's D14 option (b), rejected for forking the views per mode), and client addresses without a
repository's key, which need a binding per host the owner has not asked for.

### H-2440 · A disabled Bastion that ignores the disable serves what it holds

Stage: the disable (R-BAS-133). Subject: a Bastion that was taken over, or that runs a release without the disable. The main installation
refuses its forwards, checks and catalogue reads and cuts its forwards in flight, but the bytes of its cache and its stored catalogue are
its own: it can serve them on to anyone who reaches it. Live for every disabled Bastion whose replicas do not confirm (the state reads
`disableUnconfirmed`). What holds: nothing new leaves the main installation for it. What would close it: an act on its deployment (scale it
to zero, take its ingress or its DNS away), which the main installation cannot do.

### H-2441 · A disable reaches a Bastion only over its connection

Stage: the disable (R-BAS-133). Subject: a Bastion cut off from the main installation when it is disabled. It is unreachable already
(R-BAS-027): it serves its cache for `answers.period` plus `grace_period` (5 m and 15 m by default) and then nothing, and applies the disable
at its first handshake, before it serves a request through the main installation, which refuses those anyway. Live during an outage of the
link; the state reads `disableUnconfirmed` meanwhile. What would close it: nothing on the main installation's side; an act on the Bastion's
deployment.

### H-2442 · A replica restarted during an outage without its stored configuration does not know it was disabled

Stage: the stored configuration (R-BAS-093, R-BAS-133), the extension of
[H-2285](#h-2285--a-replica-restarted-during-an-outage-without-its-stored-configuration-applies-the-built-in-one). Subject: a replica
restarted while it cannot reach the main installation and without the configuration its cache backend keeps (a new volume, a lost bucket
object). It applies the built-in configuration, which is not disabled, and serves its cache for its grace period until its first
handshake. Live only with both an outage and a lost stored configuration. What would close it: a built-in configuration that is disabled,
which would keep every new Bastion from serving before its first handshake.

### H-2443 · A disabled Bastion's token still answers the handshake, the feed and the download intake

Stage: the main installation's refusal (R-BAS-133, Q-BAS-513). Subject: the holder of a disabled registration's token, the Bastion or
whoever took it. They read the feed's IDs (token and principal IDs, qualified repository names), learn their configuration and can report
download events and usage for the registration until the token is revoked. Live by design: the line stays open so that the Bastion confirms
and learns the enable. What closes it: revoking the token, which a disable deliberately does not do.

### H-2444 · A soft disable has no time bound

Stage: the soft disable (R-BAS-133). Subject: a slow download running through a softly disabled Bastion. It runs until it ends; the state
reads `disabling` with the requests still running. Live for every soft disable while a long download runs. What would close it: the hard
disable, which cuts it, or a bound after which soft turns hard, which the owner's words do not ask for.

### H-2408 · The Bastion's class ignores demand

Stage: the malware mark of the Bastion's web interface ([api-and-ui.md](../spec/api-and-ui.md) R-UI-258,
[malware-scanning.md](../spec/malware-scanning.md) R-MAL-042). Subject: a visitor who reads from the mark's colour how current a check of a
public artifact is. The main installation classes a check by its scan pressure: the signatures' age plus the demand since the check plus a
request (R-MAL-037). The catalogue keeps neither the class nor the downloads (Q-MAL-133, Q-BAS-358), so the Bastion classes by the
signatures' dates and the bounds alone ([Q-MAL-138](../spec/malware-scanning.md#q-mal-138--a-bastion-classes-a-check-by-the-dates-of-the-signatures-alone)):
a much-downloaded artifact whose check the main installation calls due reads "under older signatures" at the Bastion, and the part that
decides a unit's word is the main installation's choice, made with demand. Live by design for every public artifact that is downloaded. The
mark never says more than the dates show: "Checked" and "No malware found" are the main installation's verdict either way, and the details
name the set then and today with their dates. A list at a Bastion carries no time of a check and its mark no class at all
([Q-MAL-139](../spec/malware-scanning.md#q-mal-139--a-list-at-a-bastion-names-no-class-the-versions-page-does)); the version's page says
it. What would close it: the main installation's class in each version's state, which changes every public version's state at every new
set and carries a trace of demand to the Bastion (W-36's B.15, option b).

### H-2356 · The anonymous ceiling is a share per replica without Valkey, and requests in flight pass its hour

Stage: R-BAS-094, Q-BAS-445 (package E9a, built: `guard.Ceiling`). Subject: the requests all anonymous callers of a Bastion cause at the main
installation. Live on every Bastion without Valkey, and on every Bastion for the hour's figure. Without Valkey each replica holds the
ceiling divided by `bastion.replicas` (Q-BAS-34): two replicas allow the figure together while the load is spread over both, a replica that
takes the whole load allows half, and with autoscaling `bastion.replicas` is the maximum, so a Bastion running fewer replicas than its
maximum allows that share of the figure; routing by path (R-BAS-033) concentrates a path's requests on one replica. While Valkey is
unavailable a replica falls back to its share as well. The hour is read before a request and counted after its second's bucket admitted it,
so requests decided at the same instant may each pass a full hour: at most the requests in flight on behalf of anonymous callers, the
anonymous slots (16 by default), beyond the figure. What makes it real: an operator who sizes the ceiling from the main installation's
capacity and runs several replicas without Valkey errs toward less, never toward more, except for the in-flight overshoot. Valkey closes
the first; the second is the cost of counting without a script (the Bastion's Valkey user runs none).

### H-2360 · Without Valkey the throughput's replicas hold their shares as bastion.replicas says

Stage: R-BAS-097, Q-BAS-456 (package E10, built: `shared.Bytes`). Subject: the bytes a Bastion reads from the main installation and the
object store. Live on every Bastion without Valkey, and for a slice in which Valkey becomes unavailable. Without Valkey each replica counts
its own slices and takes `bytesPerSecond ÷ bastion.replicas`: two replicas read 2 × 100 MB/s = 200 MB/s together at the default while both
carry load, a replica that carries all of it reads at half the figure, and with autoscaling `bastion.replicas` is the maximum, so a Bastion
running fewer replicas reads below the figure, never above it. While Valkey cannot be used a replica claims its share in memory in addition
to what it already claimed of the same slice in Valkey: during the slice of the failure, at most its share more than the figure [derived].
What makes it real: an operator who sizes the figure to the link and runs several replicas without Valkey errs toward less, except for the
slice of a failure. Valkey closes it, as for the anonymous ceiling (H-2356).

### H-2361 · The throughput's hourly volume is checked when a miss is admitted, not while it reads

Stage: R-BAS-097, Q-BAS-459 (package E10, built). Subject: the hourly volume per class (`bytesPerHour`), unlimited by default, so dormant
until an administrator sets one. A miss is refused once its class's sliding hour reached the figure; a transfer that runs is never refused,
and its bytes are added to the hour as it reserves its next chunk and at its end. The transfers in flight when the hour fills pass the figure
by what they still read: at most the class's transfers in flight times their sizes, for the anonymous class its 16 slots and the direct reads
of the handovers they started, each up to `max_object_bytes` (2 GiB by default) [derived]. Misses admitted at the same instant may each pass
the check, as the anonymous ceiling's hour does (H-2356). What makes it real: an administrator who sets an hourly volume as a hard budget of
the link; the per-second figure is the bound that holds at every moment.

### H-2357 · Until the main installation sends a configuration of its own, a Bastion has no anonymous ceiling after its first handshake

*Closed by E8 (2026-11-26):* every registration's configuration holds the ceiling, a new one the default of Q-BAS-448, and the slots' 5 s
wait. What it was:

Stage: R-BAS-094, Q-BAS-352 (packages E9a and E8). Subject: what all anonymous callers of a Bastion together cause at the
main installation. Live on every Bastion until E8: the interim configuration the main installation sends until E8 holds no ceiling and the
slots of the chart's former defaults (a wait of 2 s), so after its first handshake a Bastion bounds an anonymous flood by its anonymous
slots alone, as before E9a (the measurement's flood of distinct artifacts reached 12 to 18 requests and 34 to 52 MB a second at the main
installation from 256 addresses). The built-in configuration's ceiling holds only before the first handshake. What closes it: E8, whose
registration carries the ceiling's default (Q-BAS-448) for every Bastion.

### H-2366 · A declared Bastion token does not expire

Stage: R-BAS-127, [Q-BAS-466](../spec/bastion.md#q-bas-466--the-declared-token-does-not-expire-its-lifetime-is-its-declarations). Subject:
the token a registration's configuration declares. Live wherever `bastions.declared` is used. An issued Bastion token always expires, so a
token nobody renews ends by itself; the declared one ends only when its Secret holds another value or the declaration is removed, and
nothing at either side reminds anyone to rotate it (the Bastion's metric of the remaining lifetime is `+Inf`). What makes it real: a value
copied out of the Secret (a backup of the namespace, a support bundle, a laptop) stays a working Bastion credential for as long as nobody
changes the Secret. What it can do is still only what a Bastion token can (R-BAS-005), from the networks of the registration's `egress` for
anything a caller's address decides (R-BAS-017). Rotating the Secret on a schedule of the operator's closes it for each rotation; the card's
**Last used** shows whether a copy is used beside the Bastion.

### H-2367 · After a restore the declared token is the restore point's until the next migration

Stage: R-BAS-127, R-HA-054, [backup-and-restore.md](../spec/backup-and-restore.md). Subject: a registration the configuration declares, after
a restore. Live after every restore. The restore Job applies no declaration: the registration, its mark and its declared token are those of
the restore point, and the journal's replay revokes a declared token that a rotation revoked after it. A value stored after the point is not
known until the next `quader migrate`, the upgrade that turns `restore.enabled` off: until then the Bastion holding the current value is
refused (its callers are served from its cache within the grace period, then refused), and a value the Secret held at the restore point
but replaced since is accepted again until that run revokes it. What makes it real: a rotation between the restore point and the restore,
which is the case where the old value was rotated because it may have leaked. The upgrade that turns `restore.enabled` off closes it: its
migration Job is a pre-upgrade hook and runs before the roles' new replicas start.

### H-2445 · A repository's readers learn the names and addresses of the Bastions that distribute it

Stage: R-BAS-134, the repository document's member `bastions` in every view ([repositories-and-proxy.md](../spec/repositories-and-proxy.md)
R-REPO-004, Q-REPO-491). Subject: the names of Bastion registrations and the base URLs of their deployments. Live since W-41's B1 wherever a
repository is distributed through a Bastion. Every caller who reads the repository, its consumer view and the lists included, reads each
distributing Bastion's name, whether it is disabled, and the base URLs its deployments reported within the last 24 hours that are entries
of `server.additional_base_urls`. The base URLs are the Bastion's public addresses, which its users are meant to use; what is new is that a
reader on the intranet learns which registration serves which repository, and that a registration is disabled. What makes it real: a
reader of a public repository who should not know the names an administrator gave the Bastions; no secret, network or token is in it. The
owner asked for it ("Es muss für die Viewer von Repos … direkt ersichtlich sein, ob das Repo zusätzlich auch über einen Bastion (per
Tooltip auch welcher Bastion) verteilt wird"), so it is not closed; an administrator who minds names a registration for its users.
