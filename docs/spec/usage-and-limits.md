# Usage and limits

Status: specification of walk item W-26 (the owner's decisions of 2026-11-19); built. A Bastion as a place of the rules (W-34, the owner's
decisions 24 to 28 of 2026-11-26, R-LIM-043 to R-LIM-046, Q-LIM-300 to Q-LIM-309) is specified in [A Bastion as a place](#a-bastion-as-a-place) and **not built**; it
revises in place R-LIM-001, R-LIM-011, R-LIM-013, R-LIM-018, R-LIM-026, R-LIM-027, R-LIM-029, R-LIM-030 and the list Later, each with a
marked sentence of what the code does until its package lands. The code is described in
[docs/developer/usage-and-limits.md](../developer/usage-and-limits.md); what it does not do yet is named on
[security/usage-and-limits.md](../security/usage-and-limits.md) and below.

This page specifies how much a caller may download from Quader and how fast, and how the people who run an installation see what
every caller downloads. It covers:

- the subject a request is charged to (a user, a technical token, a CI project, an anonymous address), for every kind of caller and
  credential;
- the figures that are counted (requests, bytes from the cache, bytes from the upstream, upstream requests, parallel transfers, a
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
| 17 | A switch Audit / Enforce for the instance, set by the instance administrator; in Audit everything is counted and the overview shows who would have been limited, nothing is refused or slowed, the package classes of R-API-016 included (owner, 2026-11-19, [Q-LIM-4](#q-lim-4--in-audit-nothing-on-a-protocol-path-is-refused-or-slowed-the-package-classes-of-r-api-016-included)); the UI and the API use his two words. The default is Enforce: the development agent's choice on the owner's leave of 2026-11-19 ("Du kannst … auch gerne den Enforce zum Standard machen. Ich erwarte dass ein Admin sich im klaren ist, was er braucht. Die Default Installation ist einfach für mich immer zum testen und ausprobieren."), since Audit has no request limit at all and an instance should start in the state that protects it | R-LIM-026 |

And, binding for every format (owner, 2026-11-19): "Alles was wir hier für OCI besprechen gilt auch für alle anderen Registries
die bereits gebaut sind und für zukünftige Registries." Every rule below is stated for the format contract and holds for Ansible
Galaxy, Debian, generic, npm, NuGet and OCI; where a protocol cannot carry a rule, it is named in
[Gaps of the protocols](#gaps-of-the-protocols) with options for the owner.

The owner decided five more questions on 2026-11-20, each for every format:

| # | Decision of the owner, 2026-11-20 | Where |
|---|---|---|
| 18 | Larger default bursts, set from a measurement: the built-in profiles' request figures are set so that normal use never reaches them, because `docker pull` and `docker push` do not retry a 429; a caller that stays above a limit is still refused at once | R-LIM-010, [Q-LIM-59](#q-lim-59--the-built-in-profiles-request-figures-are-set-from-a-measurement-of-real-clients) |
| 19 | A refused request is not counted in the request windows; the brake against a client that hammers is `refusedPerHour` with its block, which stays as it is | R-LIM-017, R-LIM-025, [Q-LIM-57](#q-lim-57--the-request-windows-are-decided-and-counted-in-one-step-by-a-script-in-the-round-trip-of-stage-7) |
| 20 | podman's long wait at a byte figure stays: `Retry-After` there is honest, and the manual says so and names the CI job's timeout | R-LIM-020, the manual's [table of the clients](../manual/user/usage-and-limits.md#what-your-client-shows) |
| 21 | The downloads that wait for a parallel slot hold at most a fixed share of a replica's request slots; a further one is refused at once (F-163) | R-LIM-024, [Q-LIM-60](#q-lim-60--waiting-downloads-hold-at-most-a-quarter-of-an-endpoints-request-slots) |
| 22 | "Option B: Ein 429 zählt nicht zur Sperre": a `429` of a request figure is not a refused request and does not count toward `refusedPerHour`; a caller that keeps sending past its figure is held to the figure's rate, and the block of decision 19 stays what it was, a brake on the refusals R-LIM-008 names | R-LIM-008, R-LIM-025, [Q-LIM-58](#q-lim-58--a-429-of-a-request-figure-is-not-a-refused-request) |

The owner decided one more question on 2026-11-26, for every format, after his rule of the same morning that a download is the
unit a person asked for (an image with all its parts, an install), never a single file (W-31 of the project plan):

| # | Decision of the owner, 2026-11-26, 10:27 | Where |
|---|---|---|
| 23 | "Option A, default darf aber großzügiger sein als 2, wir bauen hier enterprise software für große firmen": the limit of parallel downloads counts every transfer in progress, as before, and is named what it is, `parallelTransfers`, "parallel transfers", wherever a person or a client reads it, with what one pull or install costs; the built-in values are generous, sized for large companies | R-LIM-004, R-LIM-007, R-LIM-010, R-LIM-024, R-LIM-041, [Q-LIM-293](#q-lim-293--the-limit-counts-transfers-and-is-named-parallel-transfers), [Q-LIM-294](#q-lim-294--the-built-in-parallel-transfers-are-set-from-a-measurement-of-the-clients) |

The owner decided five more on 2026-11-26 in his working paper of W-34 and W-35 (stored at 11:51), for a Bastion; the paper's ids are in
brackets. They are specified and not built ([A Bastion as a place](#a-bastion-as-a-place)):

| # | Decision of the owner, 2026-11-26 | Where |
|---|---|---|
| 24 | (k4 c) A Bastion keeps its flood protection per address and adds the download budgets per caller on everything it serves, hits included, in the eleven figures; an anonymous caller is its address | R-LIM-043, [Q-LIM-305](#q-lim-305--a-bastion-protects-per-address-and-budgets-per-subject-on-everything-it-serves) |
| 25 | (k5 a) A preset per Bastion (Internet, Partner network, Internal), its rule table on demand, two profiles of the Bastion | R-LIM-046, [Q-LIM-300](#q-lim-300--a-preset-per-bastion-rules-on-demand-two-profiles-of-the-bastion) |
| 26 | (k6 a) Two budgets: what is fetched through a Bastion counts at the Bastion, a forward at the main installation as before; the overview shows both and the sum | R-LIM-045, [Q-LIM-306](#q-lim-306--two-budgets-one-at-the-bastion-one-at-the-main-installation) |
| 27 | (k7 a) Audit or Enforce per Bastion, beside the instance's switch (adds to decision 17) | R-LIM-044, [Q-LIM-301](#q-lim-301--audit-or-enforce-per-bastion) |
| 28 | (k11 a) A Bastion's defaults come from measurements of real clients; a new Bastion starts with the preset Internet in Enforce | R-LIM-046, [Q-LIM-303](#q-lim-303--the-defaults-of-a-bastion-come-from-measurements) |

The request figures of the built-in profiles are set from a measurement (Q-LIM-59), their parallel transfers from a measurement
(Q-LIM-294), and their byte, upstream and refused figures from what the uses each profile promises cost, measured or documented per
format (R-LIM-042, [Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises)): the development agent applied the owner's words of decision 23, said of the parallel figure, to
every built-in figure that bounds normal use, and tells him so. No built-in figure is an estimate any more.

## Terms

| Term | Meaning |
|---|---|
| Caller | Whoever sends a request: a person through a session or a personal token, a system through a technical token, a CI job through its CI token, or an anonymous client. |
| Subject | The unit a budget hangs on (R-LIM-001): a user, a technical token, a CI project, an anonymous address, or the system. Several credentials can share one subject (a user's sessions and personal tokens); one credential always has exactly one subject. |
| Figure | One counted quantity with its window: requests per second with a burst, requests per hour, bytes from the cache per second and per hour, bytes from the upstream per second and per hour, upstream requests per hour, parallel transfers, bytes per day, refused requests per hour (R-LIM-004). |
| Window | The time a figure counts over: one second (with a burst, so the window of the request figure is `burst ÷ rate` seconds), one hour, one day. Windows are rolling (R-LIM-018). |
| Profile | A named set of every figure (R-LIM-010); an empty figure is unlimited. Built-in profiles: Unlimited, Standard, Technical, Restricted, Anonymous; an instance administrator adds own ones. |
| Assignment (rule) | A place, a "who" (a single user, a single technical token, a CI project, an address or address range, a group, or every caller of a kind), a profile and optional overrides of single figures (R-LIM-011). |
| Place | Where a rule is set and where its budget counts: the instance, a project, or a repository; and a Bastion (R-LIM-043, specified on 2026-11-26, not built). |
| Budget key | A subject together with the place whose rule applied to a request (R-LIM-014). Every counter of a subject is a counter of one budget key. |
| Ceiling | A remote repository's limit for a class of callers taken together: all anonymous callers, or all signed-in callers (R-LIM-015). |
| Mode | `audit` or `enforce`, one setting of the instance (R-LIM-026). In Audit every decision is computed and recorded, none applied. |
| Cached bytes | Bytes of a response body that Quader sent to the caller from what it already held (R-LIM-006). |
| Uncached bytes | Bytes Quader received from an upstream in an exchange the caller led (R-LIM-006). |
| Upstream request | One exchange the remote engine starts with an upstream for the caller, a revalidation included (R-LIM-006). |
| Transfer | A response whose body is content: the answer of a route of the kind `Download` to a `GET` with a body (R-LIM-007). Only transfers are checked before they start, slowed and counted as parallel. This page's mechanics also call one a *download*; what a person asks for and calls a download (a pull of an image with its layers, an install with its packages) is many transfers (decision 23). |
| Document | A response body of a route of the kind `Metadata`, `Computed` or `Volatile` (an npm package document, a NuGet registration, an APT index, an OCI manifest of a tag). |
| Refused request | A request the decision point refused, whose credential failed, or whose repository does not exist (R-LIM-008). |
| Block | The state of a budget key whose refused requests passed their limit: every request charged to it is answered 429 for the block's time (R-LIM-025). |
| Probe | A request that names exactly one version or file without fetching its content; it starts the background download of that version ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-065). This page fixes how that download is charged (R-LIM-037). |
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
| A client certificate on an endpoint that takes it as a credential (the certificate subject, [formats/clamav.md](formats/clamav.md) R-CLAMAV-010) | `certificate` | the certificate's subject, an RFC 4514 string of at most 255 bytes | The usage store records the certificate's serial as the credential, so a renewed certificate of the same subject keeps the subject's budget; certificates of the same subject from two CAs share it ([Q-CLAMAV-12](formats/clamav.md#q-clamav-12--the-certificates-subject-stands-where-a-user-stands-and-its-serial-where-a-token-stands)). No rule names a single certificate subject; the kind default of the technical tokens applies ([Q-LIM-96](#q-lim-96--a-certificate-subject-takes-the-kind-default-of-the-technical-tokens-and-the-overview-lists-its-kind)). |
| No credential | `anonymous` | the client address prefix of R-LIM-002 | |
| A credential that failed (unknown, revoked, expired, wrong checksum, refused by an endpoint's credential rules) | `anonymous` | the client address prefix | R-LIM-003. |
| Break-glass administrator | `user` | its principal ID | An administrator (R-LIM-012). |
| A request a Bastion forwards to the main installation with the caller's credential | the subject of that credential | as above | The address is the forwarded client's ([bastion.md](bastion.md) R-BAS-017). What the Bastion serves from its own cache never reaches the main installation and is not counted here (Later). |
| The Bastion's own interface requests (principal kind `bastion`) | none | — | They are not protocol paths; R-API-016 and R-BAS-016 bound them. |
| System work: mirror passes, the Debian index parse, the Ansible documentation job, every background request of the engine (`proxy.PriorityBackground`) | `system` | `system` | Recorded per repository, limited by nothing of this page ([Q-LIM-19](#q-lim-19--mirror-passes-and-other-system-work-are-recorded-and-not-limited)). |
| A scan token of the image scanning's pull on the scan listener (principal kind `system`, name `vuln-scanner-trivy`, the unit that reads, [ha-and-helm.md](ha-and-helm.md#q-ha-373--every-name-an-operator-or-an-auditor-reads-of-a-scanner-unit-carries-its-tool) Q-HA-373; [formats/oci.md](formats/oci.md) R-OCI-037) | `system` | `system` | Recorded under the repository with the credential `vuln-scanner-trivy`; checked against no figure, no ceiling and no block, and it takes no parallel lease ([Q-LIM-66](#q-lim-66--a-scan-tokens-request-is-system-work-recorded-under-its-repository-and-limited-by-nothing)). |
| A blob token of the malware scanning's read on the scan listener (principal kind `system`, name `malware-scanner-clamav`, the unit that reads; [malware-scanning.md](malware-scanning.md) R-MAL-006) | `system` | `system` | Recorded under the repository the token names, with the credential `malware-scanner-clamav`; checked against nothing ([Q-LIM-80](#q-lim-80--the-malware-scanners-reads-are-recorded-under-the-repository-its-token-names)) |

- *Revised on 2026-11-26 by the owner's decisions 24 and 26 (R-LIM-043, [Q-LIM-305](#q-lim-305--a-bastion-protects-per-address-and-budgets-per-subject-on-everything-it-serves)):*
  what a Bastion serves from its own cache is charged at the Bastion's place to the same subject the table names (the main installation
  names its key to the Bastion, `limits_subject`), and reported to the usage store (R-LIM-045); the Bastion row's "not counted here
  (Later)" goes. *Built by E8:* the main installation names the key and stores what a Bastion reports. *Built by E9b:* the Bastion
  charges what it serves, its hits included, to that key ([bastion.md](bastion.md) R-BAS-096).
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
| `parallelTransfers` | transfers | — (a count of transfers in progress) | the subject's transfers in progress across all replicas (R-LIM-024): each layer of an image and each package of an install is one, so one pull or install holds several (R-LIM-041) | a further transfer waits up to 10 s, then 429 `too_many_parallel_transfers` |
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
  `/api/` that is not `/api/v1/`), `/files/…`, everything below `/v2/` (the version check `/v2/`, `/v2/_catalog` and `/v2/_token`
  included), and the malware scanner's blob route `/scan/v1/blobs/…` of the scan listener, whose reads are system work (Q-LIM-80). F-155
  of the project plan is closed by this: the registry's paths are counted like every other protocol path.
- **Not counted as a request**: a request without a credential that the decision point is certain to answer with the challenge
  (the rule of Q-API-290 in [api-and-ui.md](api-and-ui.md): a NuGet client's first attempt without its credentials; and by
  Q-API-357 the same on the registry's paths: the version check `/v2/` and a registry client's first attempt at a repository
  whose artifacts are not `public`; and a write without a credential on the registry's paths at any repository the index holds,
  public or not, [formats/oci.md](formats/oci.md#q-oci-225--a-push-without-a-credential-into-public-artifacts-is-answered-with-the-challenge-and-is-no-request)
  Q-OCI-225);
  a `OPTIONS` preflight; a request refused before stage 7 (`421`, `503 server_busy`, the endpoint's class of R-EP-015), except a
  failed credential, which R-LIM-003 records.
- **The management API** (`/api/v1/…`), `/auth/…`, `/ui/…` and the host-root exchange routes are not protocol paths. They keep the
  classes `read`, `write` and `anonymous` of [api-and-ui.md](api-and-ui.md) R-API-016 and are counted by nothing on this page; a
  download through the UI is a request on `/files/` and counted here. The CI exchange keeps R-CI-021, login and token validation
  R-AUTH-038.
- **The package classes of R-API-016** change ([Q-LIM-3](#q-lim-3--protocol-paths-are-counted-by-the-figures-the-management-api-keeps-its-classes),
  [Q-LIM-4](#q-lim-4--in-audit-nothing-on-a-protocol-path-is-refused-or-slowed-the-package-classes-of-r-api-016-included)): the request figures of the profiles count every protocol path (`/v2/` and `/files/`
  included), and the classes `package_read` and `package_anonymous` are gone. `package_write` applies to unsafe methods on every
  protocol path, keyed by the subject of R-LIM-001, in `enforce` only, until the upload figures (Later) replace it
  ([Q-LIM-27](#q-lim-27--uploads-stay-under-package_write-in-enforce-until-the-upload-figures-exist)). In `audit` nothing on a protocol path is refused or slowed by a request limit.
- **The endpoint's class** (R-EP-015) runs first at stage 7 and still holds: a request on a further endpoint must fit the endpoint's
  class and the figures of this page.
- **Which repository a request names** is read through the one parser of repository addresses (`qname.ParseAddress`, R-ARCH-005),
  which learns the family `registry` for `/v2/<projectKey>/<repoKey>/…`; on a host bound to a repository (R-EP-006, R-EP-007) the
  binding names it. A request that names no repository (`/v2/`, `/v2/_catalog`, `/v2/_token`, a key that does not parse, a
  repository that does not exist) is decided at the instance place (R-LIM-013).

Acceptance criteria:

- In `enforce` with the profile Anonymous, 2,200 `GET /v2/_catalog` from one address within 2 s are admitted at most 1,100 times
  (burst 1,000 plus 50 per second); in `audit` all 2,200 are admitted, as on `/api/npm/…`, and the address is recorded as
  would-have-been-limited for `requestsPerSecond`.
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

### R-LIM-007 · A transfer
Priority: **MVP** · Covers: BEYOND-030

- A **transfer** is the answer to a `GET` (not `HEAD`) on a route whose kind is `Download` (`formats.RouteKind`, R-ARCH-008) or of a
  file on `/files/` (not a folder listing), that writes a body: `200`, or `206` for any range. A `304`, a refusal and a `HEAD` are
  none. The mechanics of this page also call a transfer *a download* (the check before a download starts, the download's lease, its
  pacing).
- **What a person calls a download is many transfers** (decision 23, the owner's rule of 2026-11-26): a pull of an image is a
  manifest, a config and a transfer per layer; an install is a transfer per package. The figure that counts transfers at once is
  therefore named `parallelTransfers`, and every text a person or a client reads about it (the screens, the refusal's `detail`, the
  manual) says *transfers* and what one pull or install holds (R-LIM-041). The byte figures keep the word *download* where they mean
  the bytes a person downloads ("The download limit of 300 GB per hour is used up").
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
- a `429` of the limits, of any figure, of a block or of the class `package_write`; in `audit`, a request a figure would have
  refused. A `429` `rate_limited` of a request figure is none either (the owner's decision,
  [Q-LIM-58](#q-lim-58--a-429-of-a-request-figure-is-not-a-refused-request)): it counts in no request window (R-LIM-017), so a
  caller that keeps sending is held to its figure's rate, and it does not count toward `refusedPerHour`, so the block of R-LIM-025
  does not hold it either;
- every other status (`400`, `409`, `413`, `422`, `5xx`).

The decision point, the authentication stage and the dispatcher's repository lookup mark the request as refused in its
`limits.Charge`; the counting reads the mark, never the status code.

Acceptance criteria:

- `dotnet restore` of a solution with 300 packages over two feeds, one of which holds none of them, counts no refused request.
- 61 requests with a valid token for a repository the token may not read count 61 refused requests of the token's subject.
- 20 requests at once of a subject at `requestsPerSecond` 1 with a burst of 1 and `refusedPerHour` 5 are refused 19 times
  `rate_limited`, count no refused request, and the subject is not blocked: its next request after the `Retry-After` is admitted.

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
| `GET …/<version>/icon`, `…/license`, `…/readme` | Document (read out of the stored `.nupkg`) | — (a remote answers from its cache, Q-NUGET-137) |
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

The generic format has no remote class yet, so its requests count requests, cached bytes, parallel transfers and the day's volume.

</details>

What these protocols cannot carry is in [Gaps of the protocols](#gaps-of-the-protocols).

## Profiles, assignments and resolution

### R-LIM-010 · Profiles
Priority: **MVP** · Covers: BEYOND-030

A profile is a named set of every figure of R-LIM-004. An empty figure is unlimited.

- Five built-in profiles exist with these values. The three request figures (Req/s, Burst, Req/h) are set from a measurement of
  real clients so that normal use never reaches them (the owner's decision 18;
  [Q-LIM-59](#q-lim-59--the-built-in-profiles-request-figures-are-set-from-a-measurement-of-real-clients), migration 00085); the
  parallel transfers from a measurement of how many transfers each client holds at once, generous for large companies (decision
  23; [Q-LIM-294](#q-lim-294--the-built-in-parallel-transfers-are-set-from-a-measurement-of-the-clients), migration 00141); the
  byte, upstream and refused figures from what the uses below cost, per format (R-LIM-042), generous for large companies in the same
  way ([Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises), migration 00143). Built-ins cannot be deleted or renamed; their
  figures and description can be changed by an instance administrator, except Unlimited, whose figures stay empty (409
  `profile_immutable`).

| Profile (key) | Req/s | Burst | Req/h | Cached MB/s | Cached GB/h | Uncached MB/s | Uncached GB/h | Upstream req/h | Parallel | GB/day | Refused/h |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Unlimited (`unlimited`) | – | – | – | – | – | – | – | – | – | – | – |
| Standard (`standard`) | 200 | 4000 | 100000 | 250 | 300 | 125 | 100 | 20000 | 250 | 1000 | 1000 |
| Technical (`technical`) | 1000 | 20000 | 1000000 | 1250 | 3000 | 250 | 500 | 100000 | 1000 | 30000 | 5000 |
| Restricted (`restricted`) | 100 | 1000 | 25000 | 100 | 50 | 50 | 20 | 3000 | 64 | 200 | 300 |
| Anonymous (`anonymous`) | 50 | 1000 | 20000 | 100 | 30 | 50 | 20 | 3000 | 64 | 200 | 300 |

- The window of the burst is `burst ÷ rate`: 20 s for Anonymous, Standard and Technical, 10 s for Restricted. What the figures hold,
  measured (Q-LIM-59): one `docker pull` of an image of 40 layers is 48 requests within 0.3 s, an `npm ci` of 475 packages 475
  requests within 2.5 s; Anonymous holds ten such pulls or two such installs at once from one address, Standard an install of 4,000
  packages, Technical twenty parallel CI jobs of one token installing 475 packages each.
- The parallel transfers, measured (Q-LIM-294): a `docker pull` runs 3 transfers at once on its daemon, a `podman pull` 6, an npm
  install 15, a pnpm install up to 64, a `dotnet restore` one per package. Anonymous (64) holds four npm installs or ten podman pulls
  at once from one address; Standard (250) a developer's `dotnet restore` of 200 packages beside a `docker pull` and five `podman pull`s,
  or a pnpm install, five podman pulls and an npm install together; Technical (1,000) twenty CI jobs of one token running pnpm 10 or 11 (50 each), 66
  such jobs running npm (15 each), or five restores of 200 packages; Restricted (64) one install of any of the JavaScript clients
  whole. A transfer beyond the figure waits up to 10 s (R-LIM-024).
- The byte, upstream and refused figures, priced by R-LIM-042 (an image of 1 GB pulled first through a remote is 1 GB and 46 upstream
  requests, a CUDA image 5.9 GB, a first npm install of 475 packages 0.43 GB and 950 upstream requests, a NuGet version list kept fresh
  twelve revalidations an hour, an install refused on a token without read 15 refused requests), each within an hour and with room:
  - **Anonymous** (one address, often a company's people or CI jobs behind one NAT before they sign in): ten first pulls of 1 GB images
    at once and twenty more from the cache (10 of 20 GB from the upstream, 20 of 30 GB from the cache); two first installs of 475
    packages at once beside the first pull of a CUDA image (1,940 of 3,000 upstream requests); a dozen people whose expired token fails
    an install (180 of 300 refused); twenty pulls of 1 GB an hour for eight hours (160 of 200 GB a day). A 1 GB image takes 10 s from
    the cache at 100 MB/s, 20 s on its first pull at 50 MB/s.
  - **Restricted** (a caller an administrator narrows): one first install of 475 packages and the first pull of a CUDA image (6.3 of
    20 GB, 990 of 3,000 upstream requests), or forty pulls of 1 GB from the cache (40 of 50 GB). Never less than Anonymous on a figure,
    so that a caller never gets more by not signing in.
  - **Standard** (a developer's machine): an install of 4,000 packages on a cold cache, twice in the hour after a branch switch (16,000
    of 20,000 upstream requests, 7.2 of 100 GB); the first pulls of a dozen 1 GB images with a CUDA and a PyTorch image (22 GB); a
    compose file of a dozen 1 GB images pulled ten times an hour from the cache (120 of 300 GB), eight hours a day (960 of 1,000 GB); a
    solution of 300 packages over two NuGet feeds kept fresh (7,200 upstream requests); forty installs refused on a token without read
    (600 of 1,000). A 1 GB image takes 4 s from the cache at 250 MB/s, 8 s on its first pull at 125 MB/s.
  - **Technical** (a system or a CI project with one token): twenty parallel CI jobs installing 475 other packages each on a cold cache
    (19,000 of 100,000 upstream requests); 2,000 NuGet packages over two feeds kept fresh (48,000); a fleet's first pulls after its base
    images changed, a hundred 1 GB images and eight CUDA images (147 of 500 GB); 2,000 jobs an hour on fresh runners, each pulling a 1 GB
    image and running `npm ci` (2,180 of 3,000 GB), ten hours a day (21,800 of 30,000 GB); a token that lost read on one repository
    while twenty jobs fail on it every five minutes (3,600 of 5,000 refused, so the token is not blocked everywhere else).
  What each figure still bounds, what a thousand anonymous addresses can do, and why no figure is unlimited by default, is [Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises);
  how the figures relate to the public registries' own limits, [Q-LIM-298](#q-lim-298--the-built-in-figures-grant-nothing-a-public-registry-refuses-and-a-remotes-ceilings-stay-the-administrators).

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
  parallel_transfers          bigint NULL CHECK (parallel_transfers > 0),
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

- **Place**: the instance, a project, or a repository of any class. *Revised on 2026-11-26 (R-LIM-043):* and a Bastion, whose rules count
  what that Bastion serves; the data model below gains `bastion` and `bastion_id`. *Built by E8 (2026-11-26).*
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

*Revised on 2026-11-26 (R-LIM-043, [Q-LIM-302](#q-lim-302--the-main-installation-resolves-a-signed-in-callers-rule-the-bastion-the-address-and-kind-rules)):*
what a Bastion serves is resolved at the Bastion's place alone, its administrators included, and ends at the kind rules its preset wrote;
the main installation resolves a signed-in caller's rule there and names it to the Bastion, which resolves address and kind rules itself.
*Built by E8 (2026-11-26), the main installation's part:* `limits.Index.ResolveBastion`. *Built by E9b (2026-11-26), the Bastion's
part:* the address rule of the longest prefix, else the kind rule (`settings.Place.Anonymous`, `budget.SubjectOf`).

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
  `release` (R-AUTH-046) and the rule `technicalToken` `release` → Technical with `parallelTransfers: 100`. A request with any
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
- System work is in neither class and is not charged to a ceiling, the image scanning's reads through `upstream/scanner-trivy`
  included ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-070).
- A well-known repository of the project `upstream` ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-066) has its
  ceilings like any remote; they bound every project's callers that reach it, directly or through a virtual repository, together
  ([Q-LIM-62](#q-lim-62--a-request-through-a-virtual-repository-to-a-well-known-member-is-decided-at-the-addressed-repository-and-the-members-ceilings-bound-it)).
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
| Rules and ceilings at the built-in project `upstream` and its repositories | `limits:write`; ceilings also `repositories:write-config`; the project has no Owner ([authorization.md](authorization.md) R-AUTHZ-041, Q-LIM-62) |
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
  values and their caps in `errors[]` (each entry carries the cap as the member `cap`, in the figure's unit, [api-and-ui.md](api-and-ui.md)
  Q-API-352), and is not stored.
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
| Requests per second and per hour of a budget key | Valkey, sliding window (R-ARCH-034 arithmetic) | the take: one script (`EVALSHA`) that runs `GET`, `INCRBY`, `EXPIRE NX` | at stage 7, in the same step as the decision, and only when the request is admitted |
| Cached bytes per hour, bytes per day of a budget key | Valkey, sliding window in bytes | `GET` at the check; `INCRBY` at the flush | at the flush after the bytes were written |
| Uncached bytes per hour of a budget key and of a ceiling | Valkey, sliding window in bytes | `GET` in the take of step 9a; `INCRBY` at the flush | at the flush after the bytes were read |
| Upstream requests per hour of a budget key and of a ceiling | Valkey, sliding window | the take of step 9a (the script of stage 7); a give-back after a parallel lease refused (Q-LIM-110) | at step 9a, in the same step as the decision, and only when the upstream request is admitted |
| Parallel transfers of a budget key | Valkey, a sorted set of leases | `ZADD`, `ZREMRANGEBYSCORE`, `ZCARD`, `ZREM` | at the start and the end of a download |
| Refused requests per hour of a budget key, and its block | Valkey, sliding window and a marker | `INCRBY` at the flush; `SET NX EX`, `GET`, `DELIFEQ` | at the flush; the block at the flush that passes the limit |
| Bytes per second (pacing) | the replica's memory, with the cluster's count of the key's downloads | `ZCARD` in the flush | continuously |

**Keys.** The family `lim` (`quader:<ks>:lim:v1:<figure>:<b>:<window index>`, `…:lim:v1:par:<b>`, `…:lim:v1:blk:<b>`), with `<b>`
the hashed budget key or ceiling key (R-ARCH-030: keys reach Valkey only as hashes); a counter expires two windows after it opened,
so the family's longest time to live is 48 hours.

**What a request costs** (each a pipelined round trip bounded by `valkey.command_timeout`):

| Request | Round trips on its path |
|---|---|
| Every request on a protocol path | 1 at stage 7: the take of the request windows of its budget key with its block marker and, in `enforce` for an unsafe method, the window of `package_write`, in one `MULTI` (after a lost script cache the first take of a replica costs one more, below) |
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

**The take** (the owner's decision 19,
[Q-LIM-57](#q-lim-57--the-request-windows-are-decided-and-counted-in-one-step-by-a-script-in-the-round-trip-of-stage-7)): the
request windows are decided and counted in one step. One script reads the current and the previous counter of every window the
request must fit (the burst window, the hour, the class of R-API-016 that is in force) and the block marker; when every window has
room for the request with it and no block holds, it adds the request to each window, otherwise it adds nothing. A refused request
therefore fills no window, and the `Retry-After` of a request figure's refusal is the time after which the caller's next request
fits, if nothing else uses the room (R-LIM-020). The script is sent as `EVALSHA` inside the `MULTI` of stage 7, so the request still
costs one round trip; a server that does not hold it (a restarted primary, a replica that a failover promoted: Valkey keeps scripts
neither on disk nor on its replicas) answers `NOSCRIPT` and does nothing, and the take is sent once more with the script's body
(`EVAL`), which loads it: one more round trip, once per replica after such an event. The script runs only `GET`, `INCRBY` and
`EXPIRE` on the keys it is given, under the ACL of the Valkey user. Step 9a takes the same way
([Q-LIM-110](#q-lim-110--an-upstream-request-step-9a-refuses-is-counted-nowhere-and-a-request-stage-7-refuses-is-given-back-to-the-endpoints-class)):
its upstream request windows and the uncached byte windows it only checks are one take, and an upstream request the parallel lease
refuses after the take admitted it is given back by the same script.

The least-privilege command list of [architecture.md](architecture.md) R-ARCH-031 gains `INCRBY`, `ZADD`, `ZREM`,
`ZREMRANGEBYSCORE`, `ZCARD`, `EVALSHA` and `EVAL`; the example ACL rule, the development profile's Valkey (`deploy/dev/valkey.yaml`)
and the integration stack's Valkey user grant them. No `DEL` is used.

Acceptance criteria:

- A cached download costs two round trips to Valkey and no PostgreSQL statement on its path, measured by
  `quader_valkey_command_duration_seconds_count` and the statement counter of the download budget (R-ARCH-027) in the integration set
  `limits`.
- A replica with 1,000 downloads in progress sends one flush round trip per second; a flush that drops the leases of 1,000 downloads
  that ended in its interval is that one round trip (a batch has no bound on its operations).
- At `requestsPerSecond` 1 with a burst of 1, a caller that sends a request every second, as the `Retry-After` of 1 tells it, is
  admitted at least every second time; a caller that waits the `Retry-After` of a refusal is admitted, however many requests it sent
  in between.

### R-LIM-018 · Windows and their precision across replicas
Priority: **MVP** · Covers: BEYOND-030

- **Windows are rolling** ([Q-LIM-11](#q-lim-11--windows-roll-by-the-two-window-estimate-for-the-hour-and-the-day-too)), estimated
  as R-ARCH-034 does for every window: `previous × (1 − elapsed ÷ W) + current ≤ limit`, with `W` one second times the burst ratio
  for requests, 3,600 s for an hour and 86,400 s for a day, window boundaries at multiples of `W` since the Unix epoch.
- **What is promised**, across any number of replicas while Valkey is reachable:
  - Requests and upstream requests: in each fixed window at most the limit; in any rolling interval of length `W` at most twice the
    limit (the bound of the two-window estimate). A request the request figures, a block or the class of R-API-016 refused is in no
    request window (R-LIM-017).
  - Bytes per hour and per day: in each fixed window at most the limit, plus the bytes that the subject's downloads admitted before
    the limit was reached still transfer (a download is never cut, owner decision 4; at most `parallelTransfers` transfers), plus one
    `limits.flush_interval` (1 s) of the subject's transfer rate that was not yet charged when a check read the counter.
  - Ceilings: as bytes per hour and upstream requests, over all subjects of the class.
  - Parallel transfers: at most the limit across the cluster, except for the downloads of a replica that crashed, whose leases
    keep their place for at most `limits.parallel.lease` (60 s), and a take that Valkey ran after the replica had given up on it
    and after the drop of the same lease, which keeps its place as long ([Q-LIM-95](#q-lim-95--a-lease-is-given-up-in-both-places-it-may-be-held-and-a-drop-valkey-did-not-take-is-sent-again-until-the-leases-end)).
  - Bytes per second: R-LIM-023.
- **What is not promised**: exact accounting of a window in which Valkey failed over or was unreachable (R-LIM-019), and
  any figure of a request that Quader never saw (a Bastion's cache hit). *Revised on 2026-11-26 (R-LIM-043):* a Bastion's hit is counted at
  the Bastion's place with the same windows, in the Bastion's memory or, with its Valkey, across its replicas; the main installation's
  counters never see it. *Built by E9b (2026-11-26):* the Bastion counts it ([bastion.md](bastion.md) R-BAS-096).

### R-LIM-019 · Valkey or PostgreSQL unreachable
Priority: **MVP** · Covers: BEYOND-030

Every figure fails open to the replica's share and never refuses a request because Valkey failed
([Q-LIM-12](#q-lim-12--every-figure-fails-open-to-the-replicas-share-while-valkey-is-unreachable)):

| Figure | While Valkey is unreachable on a replica |
|---|---|
| Requests, upstream requests, bytes per hour and per day, refused requests | a local window per budget key and ceiling with the limit ÷ n, n the live replicas of the role in the replica's pool (R-ARCH-034); local windows start empty. The take runs on them under the replica's lock: a refused request fills no local window either. The class of R-API-016 decides on its own token bucket of the replica's share (R-ARCH-034), which takes nothing when it refuses; a request it refuses after the local windows took it is taken back from them |
| Parallel transfers | a local count per budget key with ⌈limit ÷ n⌉ of the replica's own downloads in progress: a lease is given up on the share and in Valkey, wherever its take ran ([Q-LIM-95](#q-lim-95--a-lease-is-given-up-in-both-places-it-may-be-held-and-a-drop-valkey-did-not-take-is-sent-again-until-the-leases-end)) |
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
| `rate_limited` | `requestsPerSecond`/`requestsBurst`, `requestsPerHour` | the time after which the caller's next request fits, if nothing else uses the room: the refused request is in no window (R-LIM-017), so the wait is true; at least 1 s, rounded up as **One wait** says |
| `download_limit_reached` | `cachedBytesPerHour`, `bytesPerDay` | the time until the window's estimate leaves room for one more byte, at least 1 s, at most the window: up to an hour or a day, and honest (the owner's decision 20) |
| `upstream_limit_reached` | `uncachedBytesPerHour`, `upstreamRequestsPerHour`, a ceiling | as above |
| `too_many_parallel_transfers` | `parallelTransfers` | 1 s |
| `caller_blocked` | a block (R-LIM-025) | the block's remaining time |

- **One wait** ([Q-LIM-291](#q-lim-291--a-refusal-names-one-wait-in-the-unit-its-text-names-it-in)). The wait of the table is rounded up
  once, to whole seconds below two minutes and to whole minutes from there, and that one value is `Retry-After`, `retryAfterSeconds`,
  the `t` of the refusal's RateLimit policy and the wait the `detail` names, exactly: "45 seconds", "14 minutes", "113 minutes", and
  from two hours "3 hours" or "2 hours 1 minute". A caller that waits it is never early and waits at most 59 s more than the counters
  need; a byte or upstream figure's wait stays at most its window. A format that caps `Retry-After` (NuGet and Ansible Galaxy at 30 s,
  for their clients' timeouts) keeps the detail's wait, which is the limit's.
- **A client that retries a refusal** is admitted once it waits what `Retry-After` says: at a request figure that is a second or two;
  at a byte figure it is the rest of the hour or the day, and a client that waits up to that long holds its command for that time
  (podman: about 16 to 20 minutes before it gives up; the manual names the CI job's timeout as the setting for it). A client that
  does not retry (`docker pull`, `docker push`) exits at the first refusal; the built-in request figures are set so that normal use
  never meets one (R-LIM-010).

- **Body.** On the management API's shape (`/files/`, generic): `application/problem+json` (R-API-009) with `code`, a `detail` in
  plain words that names the figure, the limit and the wait ("The download limit of 300 GB per hour is used up. Try again in 14
  minutes."), and the extension members `figure` (the API name), `limit` (the value), `windowSeconds`, `place` (`instance`,
  `project`, `repository`) and `retryAfterSeconds`. For a signed-in caller `detail` ends with "Your limits: <base URL>/ui/profile/usage".
  A ceiling's refusal names the class ("all anonymous callers of this repository"). A refusal that answers a recognized forward of a
  Bastion has no `place`, no "Your limits" sentence and no `requestId`: its reader is a caller of the Bastion, which serves no web
  interface, and the place is the main installation's structure ([bastion.md](bastion.md) R-BAS-099 row 7; built).
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
  `parallelTransfers` (`"cached-hour"`, `"uncached-hour"`, `"upstream-hour"`, `"day"`, `"parallel"`, `"refused-hour"`). The endpoint's
  class and, in `audit`, the R-API-016 class add their own members as today.
- **Request log**: the fields of R-LIM-030; the `429` names the figure in `limiter` (`limits.<figure>`) and the counter's mode in
  `limit_mode` (`cluster`, or `fallback` while the replica counts its share alone, as R-OPS-010 defines the field;
  [Q-LIM-51](#q-lim-51--the-request-logs-limit_mode-of-a-refusal-is-the-counters-mode-as-for-every-limiter)).

Acceptance criteria:

- With `cachedBytesPerHour` used up, `npm install` reports `npm error code E429` and `429 Too Many Requests` with the tarball's
  address; `docker pull` prints `error from registry: ` and the `detail` and exits 1; a raw file answers the problem with `figure:
  "cachedBytesPerHour"` (the cases of R-LIM-039; what each client prints is the manual's table).
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
> counted here, for the budget key and the ceiling, in the same step as the decision and only when it is admitted: an upstream
> request a figure or the parallel lease refuses is counted nowhere ([Q-LIM-110](#q-lim-110--an-upstream-request-step-9a-refuses-is-counted-nowhere-and-a-request-stage-7-refuses-is-given-back-to-the-endpoints-class)).
> Without room: an existing expired document is served
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
- A caller at `upstreamRequestsPerHour` 2 that retries a refused miss six times is admitted once the window has room for one more
  request; a request the ceiling refuses leaves the caller's own window untouched, one the caller's figure refuses the ceiling's, and
  a download the parallel lease refuses gives its upstream request back, in Valkey and on the replica's share
  (`TestR_LIM_022_ARefusedUpstreamRequestIsCountedNowhere`).

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

- `cachedBytesPerSecond` 10 MB, one 100 MB download: it takes between 9 and 11 s. Two parallel transfers of the subject on two
  replicas take between 18 and 22 s together.
- `cachedBytesPerSecond` 1,000: a 1 MB download completes at the floor in about 16 s, and no gap between two writes exceeds one
  second.

### R-LIM-024 · Parallel transfers
Priority: **MVP** · Covers: BEYOND-030

- A download takes a lease in the budget key's sorted set: member `<replica ID>:<download ID>`, score the lease's end
  (now + `limits.parallel.lease`, 60 s), in one `MULTI` with `ZREMRANGEBYSCORE <key> -inf <now>` and `ZCARD`. The lease is admitted
  when the count including it is at most `parallelTransfers`; otherwise it is removed again (`ZREM`) and the download waits. The
  figure counts transfers (R-LIM-007): a pull or an install of one client holds several leases at once, one per transfer in
  progress (R-LIM-041).
- **Waiting** ([Q-LIM-14](#q-lim-14--a-further-download-waits-up-to-10-seconds-then-429)): the download asks again once per second,
  up to `limits.parallel.wait` (10 s), holding its request slot of step 4; then 429 `too_many_parallel_transfers`. A client that
  leaves stops waiting.
- **The share of waiting downloads** (the owner's decision 21, F-163;
  [Q-LIM-60](#q-lim-60--waiting-downloads-hold-at-most-a-quarter-of-an-endpoints-request-slots)): on each replica, the downloads that
  wait hold at most a quarter of the request slots of the endpoint they came through (`server.max_in_flight`, or the endpoint's own
  `max_in_flight`, R-ARCH-005 step 4): 1,024 of the default 4,096, 256 of a further endpoint's 1,024. A download that would wait while
  that share is held is refused at once with 429 `too_many_parallel_transfers` and a `Retry-After` of 1 s; its lease is given up. The
  other three quarters stay for requests that are served, so a burst of callers at their figure cannot make the replica shed another
  caller's requests. The share is a constant (`limits.WaitingShare`), not a setting: it follows the endpoint's `max_in_flight`.
- The replica renews the leases of its downloads older than 20 s in the flush and removes ended ones there.
- **A lease is given up in both places it may be held** ([Q-LIM-95](#q-lim-95--a-lease-is-given-up-in-both-places-it-may-be-held-and-a-drop-valkey-did-not-take-is-sent-again-until-the-leases-end)):
  on the replica's share at once, and in Valkey by the drop of the flush or of a refusal. A drop that did not reach Valkey (the batch
  ran on the share) is sent again with every flush until one reaches Valkey or the lease has ended there, its last renewal plus
  `limits.parallel.lease`. A renewal goes to both places as well. No request waits for it.
- **A crashed replica** leaves its leases, which expire at their score: the subject has up to 60 s fewer slots.
- In `audit` a lease above the limit is kept, recorded as `would_limit`, and the download goes on at once.

Acceptance criteria:

- An endpoint with 8 request slots, a subject at `parallelTransfers` 1 whose one transfer is paced: two further transfers of the
  subject wait; the next is refused at once (well within a second) with `too_many_parallel_transfers`, and another caller's request is
  served beside them.
- The capacity test of R-ARCH-027 (`TestNPMPerf`, twenty technical tokens of the profile Technical in `enforce`, without a rule of
  their own) answers every request with 200 or the replica's own 503 `server_busy`.

### R-LIM-040 · A download handed to a signed link of the object store
Priority: **P1** · Covers: BEYOND-030

The owner's decision of 2026-11-22 ([storage.md](storage.md) R-STOR-014, decision 11 (a) of his working paper on the stored content):
a download is handed to a signed link only where "für den Aufrufer gilt kein Tempolimit", and "gezählt wird bei der Ausgabe, nicht beim
Transfer". *Changed on 2026-11-22 by [Q-LIM-99](#q-lim-99--a-speed-limit-does-not-stop-a-link-the-budgets-are-charged-at-the-issue), decided by the development agent after the owner left the open
questions to it: a speed limit no longer keeps a caller from a link.* A link moves the transfer away from Quader: no counting writer sees
its bytes, and no lease can last as long as it does. What each figure does with a link ([Q-LIM-99](#q-lim-99--a-speed-limit-does-not-stop-a-link-the-budgets-are-charged-at-the-issue),
[Q-LIM-98](#q-lim-98--a-link-is-charged-its-size-when-it-is-issued-and-its-parallel-lease-ends-with-the-redirect)):

| Figure | With a link |
|---|---|
| `requestsPerSecond`, `requestsBurst`, `requestsPerHour`, the block | unchanged: the `307` is a request at stage 7 like every other |
| `cachedBytesPerSecond` | **no condition**: a budget key that has it set gets links as every other, in `enforce` and in `audit` alike; the transfer by a link is not paced, since Quader writes none of its bytes, and a streamed download of the same caller is paced as before (R-LIM-023) ([security/download-links.md](../security/download-links.md) H-1352) |
| `cachedBytesPerHour`, `bytesPerDay` | checked before the link as before a stream (R-LIM-021, `Admit`); the item's size is charged in full as cached bytes when the link is issued, and reaches the windows at the next flush |
| `parallelTransfers` | checked before the link (`Admit` takes the lease and may wait); the lease ends with the `307`, so the figure bounds issuances, not transfers ([security/download-links.md](../security/download-links.md) H-1276) |
| `uncachedBytesPerSecond`, `uncachedBytesPerHour`, `upstreamRequestsPerHour`, the ceilings | unchanged: a link is only ever a stored item's (a remote's miss is streamed by the leader, R-REPO-041) |

- **No figure is a condition of a link**; the built-in profiles' `cachedBytesPerSecond` (R-LIM-010) keeps no caller from links, so on a new
  installation every caller the other conditions of R-STOR-070 allow gets them.
- **The charge at issuance** adds the item's size to the charge's cached bytes (`AddCached`, [charge.go](../../internal/limits/charge.go)
  lines 179 to 187): the windows, the usage store (cached bytes of the repository that served) and `bytes_cached` of the request log carry
  it as if the body had been written. A link fetched twice counts once, a link never fetched counts.
- **In `audit`** a link is issued wherever the conditions hold; nothing about it is refused, `Admit`'s would-limits are recorded as for a
  stream, and no would-limit of a pace is recorded, since `enforce` paces no link either.
- **A system read** (R-LIM-001) is never a link ([storage.md](storage.md) R-STOR-070).
- **As built** (2026-11-22, the links' package 2): the charge is `limits.Charge.ChargeLink` ([links.go](../../internal/limits/links.go)),
  called when the redirect is written ([download_links.go](../../internal/formathost/download_links.go)); the serve point reads no figure
  for its decision.

Acceptance criteria:

- Unit (`internal/limits`): under the profile Standard, which sets `cachedBytesPerSecond`, a link of a 5 MB tarball is charged at issuance
  in `enforce` and in `audit`, the next flush adds it to the hour window, and with `cachedBytesPerHour` used up the next download is
  `429 download_limit_reached` in `enforce` and recorded as `would_limit:cachedBytesPerHour` in `audit` (`TestR_LIM_040_ALinkAndTheFigures`).
- Unit (`internal/formathost`): a caller with `cachedBytesPerSecond` gets `307` in `enforce` and in `audit`, and the charge holds the
  item's size; a used-up `cachedBytesPerHour` answers `429` and no link (`TestR_STOR_070_TheConditionsOfALink`).

### R-LIM-041 · What one act of a client costs in transfers, and how a person reads the figure
Priority: **MVP** · Covers: BEYOND-030

The figure `parallelTransfers` counts transfers (R-LIM-007), and one act of a client holds several at once (decision 23). How many,
per format and client, measured on 2026-11-26 or taken from the client where no measurement ran
([Q-LIM-294](#q-lim-294--the-built-in-parallel-transfers-are-set-from-a-measurement-of-the-clients)):

| Format | Client and act | Transfers at once | Source |
|---|---|---|---|
| OCI | `docker pull` (Docker 29.9.3), one image or several at once, `docker compose pull` | 3 per daemon, whatever the number of pulls | measured |
| OCI | `docker build` (BuildKit) of an image `FROM` an image of 40 layers | 4 | measured |
| OCI | `podman pull` (Podman 5.8.9) | 6 per pull; five pulls at once 30 | measured |
| OCI | `ctr images pull` (containerd 2.4.2) | no bound: 32 for an image of 40 layers, 11 for one of 10 | measured |
| OCI | a Kubernetes node (the kubelet through containerd's CRI) | 3 per pull, pulls one after the other unless the node allows parallel pulls | the clients' defaults, not measured |
| Helm charts in OCI | `helm pull`, `helm install` of a chart | at most 2: the chart's config and its archive | the protocol, not measured |
| npm | `npm install`, `npm ci` (npm 10, 11, 12) | 15 | measured |
| npm | `pnpm install` (pnpm 10 and 11; pnpm 12 on a machine of 20 cores) | 50; 64 | measured |
| npm | `yarn install` (Yarn 1.22; Yarn 4.18) | 8; 32 | measured |
| NuGet | `dotnet restore` (.NET SDK 10.0.403) | no bound: one per package of the restore, 200 of 200 | measured |
| Debian | `apt-get install`, `apt-get download` (APT 3.0.4) | 1: one connection per host, its pipelined requests answered in order | measured |
| Ansible Galaxy | `ansible-galaxy collection install` | 1: one archive after the other | the client, not measured |
| Generic | `curl`, `wget`, a download from the screen | 1 per file | the clients |

- **The words a person reads** name the figure as what it counts, never as downloads: the API's member `parallelTransfers` and the
  code `too_many_parallel_transfers` (R-LIM-020); the screens' **Parallel transfers**, the editor's group **Transfers** and a
  profile's compact line "250 parallel transfers" (R-UI-178); the meter of a caller's limits (R-UI-184); the notice of a refused
  download (R-UI-186); the manual's tables of the figures.
- **Where the figure is set and where its refusal is explained, one sentence says what one unit costs:** the editor's help of the
  group **Transfers** ("Each layer of an image and each package of an install is one transfer: a pull runs 3 to 6 at once, an npm,
  pnpm or Yarn install 8 to 64, a NuGet restore one per package."), the refusal's `detail` ("The limit of 250 parallel transfers is
  reached: each layer of an image and each package of an install is one transfer. Try again in 1 second."), the notice on the screen
  and the manual's rows of the figure.

Acceptance criteria:

- Unit (`internal/limits`): the refusal's `detail` names transfers and what one is, and its wait of one second
  (`TestR_LIM_020_TheDetailAndRetryAfterNameOneWait`).
- Unit (web): the figure's name, its limit's text and the help of its group (`figures.spec.ts`), and the notice of a refused download
  (`usage.spec.ts`, R-UI-186).

### R-LIM-042 · What one act of a client costs in the byte, upstream and refused figures
Priority: **MVP** · Covers: BEYOND-030

The byte, upstream and refused figures of the built-in profiles (R-LIM-010) are set from what one act of each format's client costs in
them ([Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises)). Per act, on 2026-11-26: the bytes from the cache, and the bytes and upstream requests of its first time through a
remote, on a cold cache; *measured* is a number read on that day from a real client, cache or registry, *documented* one a registry
publishes, *derived* one that follows from a measured count and the protocol or Quader's code.

| Format | Act | From the cache | First time through a remote | Source |
|---|---|---|---|---|
| OCI | `docker pull` of an application image of 1 GB in 40 layers | 1 GB | 1 GB; 46 upstream requests: the tag, an index, the platform's and the attestation's manifest, the config, 40 layers, the attestation's blob | size documented: an application adds to a base that alone is 0.3 to 0.44 GB (Docker Hub's tag API, amd64, compressed: `node:22` 408 MB, `python:3.13` 414 MB, `gradle:jdk21` 438 MB, `golang:1.25` 305 MB, `eclipse-temurin:21-jdk` 231 MB, `postgres:17` 167 MB); requests derived from Q-LIM-59's measured 48 less the token and the second `HEAD` |
| OCI | first pull of `nvidia/cuda:12.8.1-cudnn-devel-ubuntu24.04` | 5.9 GB | 5.9 GB; a few tens of upstream requests | documented, Docker Hub's tag API; `pytorch/pytorch:latest` 3.7 GB |
| OCI | `docker pull` of a tag the machine holds | none | a `HEAD` of the tag once its metadata period ran out (R-REPO-027) | measured, Q-LIM-59 |
| npm | `npm ci` of 475 packages | 88 MB, 186 kB a tarball | 0.43 GB; 950 upstream requests: a tarball's miss fetches or revalidates its package document first (R-NPM-025) | measured: of the 559 packages of the web client's lockfile, the 437 tarballs a local npm cache held (81 MB) and the 524 package documents (373 MB in the abbreviated form npm keeps, 711 kB each); a remote fetches the full document, which is larger |
| npm | `npm install` of 475 packages without a lockfile | 0.43 GB: a document and a tarball each | as `npm ci` | as above |
| npm | an install of 4,000 packages | 0.75 GB from a lockfile | 3.6 GB; 8,000 upstream requests | derived from the 475 |
| NuGet | `dotnet restore` of 200 packages from one feed | 0.3 GB | 0.3 GB; about 400 upstream requests, a version list and a package each; then a revalidation of each version list per metadata period (300 s, R-REPO-027): twelve an hour per package and feed while restores go on | measured: the newest versions of nuget.org's 200 most-downloaded packages, 292 MB for the 194 that answered (median 338 kB, largest 39 MB); requests derived from Q-LIM-59 |
| Debian | `apt-get update` of one suite | 10.2 MB | 10.2 MB; about 12 upstream requests | measured with `debian:trixie`; requests from Q-LIM-59 |
| Debian | `apt-get install build-essential curl python3 ca-certificates` | 95 MB in 106 packages | 95 MB; 106 upstream requests | measured (`--print-uris`, `debian:trixie`); `openjdk-21-jdk-headless` 139 MB in 56 |
| Ansible Galaxy | `ansible-galaxy collection install` of twelve widely used collections | 11 MB | 11 MB; about 48 upstream requests, a version list, a version and an archive each | measured: Galaxy's v3 API, the newest versions of `community.general` (2.7 MB), `amazon.aws`, `kubernetes.core`, `ansible.posix`, `community.docker`, `community.crypto`, `azure.azcollection`, `cisco.ios`, `community.vmware`, `ansible.windows`, `community.postgresql`, `google.cloud`; requests from Q-LIM-59 |
| Generic | a download of a JDK, of a Go toolchain | 208 MB, 71 MB | none: no remote class | documented, the download lists of Adoptium and go.dev |
| every format | an act refused because the token may not read, or has expired | none | none | derived: at most the requests the client holds open, 15 for npm (R-LIM-041); docker and a NuGet restore stop at their first refused request (the tag, the feed's service index) |

What one act met in the built-in profiles before Q-LIM-296, and how many of it each holds within an hour now:

| Format and act | Met before | Now |
|---|---|---|
| OCI, the first pull of a 1 GB image through a remote | Anonymous at once (20 upstream requests: a pull of more than about 17 layers answers 429, which docker does not retry); Anonymous' 2 GB from the upstream at the third such image; Restricted's 200 upstream requests at the fifth, its 5 GB at the sixth; Standard's 2,000 at the 44th | Anonymous and Restricted hold about 65 an hour by upstream requests, 20 by bytes; Standard 100 |
| OCI, the first pull of a CUDA image | Anonymous (2 GB) and Restricted (5 GB): a layer is refused once the hour's estimate passed the figure, so the pull fails partway (R-LIM-021); Standard's 30 GB at the sixth | Anonymous and Restricted three an hour, Standard sixteen |
| OCI, pulls of a 1 GB image from the cache | Anonymous' 10 GB at the eleventh an hour, its 30 GB a day at the 31st | Anonymous thirty an hour, 200 a day |
| npm, a first install of 475 packages | Anonymous after 10 packages; Restricted after 100; Standard at the third install an hour, Technical at the eleventh | Anonymous and Restricted three an hour, Standard 21, Technical 105 |
| npm, a first install of 4,000 packages | Standard after 1,000 packages | Standard two an hour |
| NuGet, a first restore of 200 packages | Anonymous after 10 packages, Restricted after 100 | Anonymous and Restricted seven an hour |
| NuGet, version lists kept fresh | Standard by 167 packages over one feed, Technical by 833 | Standard 1,666 package-feeds, Technical 8,333 |
| Debian, an update and an install of 106 packages | Anonymous after the update and eight packages | Anonymous 25 an hour |
| Ansible Galaxy, twelve collections | Anonymous after five | Anonymous 62 an hour |
| Generic, a JDK from the cache | Anonymous' 10 GB at the 49th an hour | Anonymous 144 an hour |
| every format, refused acts | Anonymous' 60 at the fifth npm install refused from one address, which blocks every anonymous caller behind it for the block's time (H-1010) | Anonymous twenty |

- Each number is a cost within an hour; the day's figure is the hour's cost over a working day (R-LIM-010).
- A measurement is run once and written down here; it is not a test (CLAUDE.md). What is tested is the arithmetic: each profile holds
  the uses R-LIM-010 promises it at these prices.

Acceptance criteria:

- Unit (`internal/limits`): each built-in profile holds, below every byte, upstream and refused figure, the uses R-LIM-010 names for it
  at the prices of this table (`TestR_LIM_042_TheBuiltInProfilesHoldTheUsesThePagePromises`), and the profiles keep their order per
  figure, Anonymous at most Restricted at most Standard at most Technical (`TestR_LIM_042_TheBuiltInProfilesKeepTheirOrder`).
- Integration (area `postgres`): the step of migration 00143 sets the new values where the first migration's stand and keeps an
  administrator's (`TestR_HA_020_TheBuiltInFiguresStepKeepsAnAdministratorsValue`); the built-in profiles of a new installation hold the
  figures of `limits.BuiltinProfiles` (`TestR_LIM_010_TheBuiltInProfilesHoldTheFiguresOfTheMigrations`, area `limits`).

### R-LIM-025 · The block after refused requests
Priority: **MVP** · Covers: BEYOND-030

- When the flush finds a budget key's refused requests per hour above `refusedPerHour`, it sets the block marker (`SET <blk key>
  <block ID> NX EX <block seconds>`); the block time is the instance setting `blockSeconds` (900 s by default, an estimate;
  60 to 86,400). While the marker exists, stage 7 answers every request charged to that budget key with 429 `caller_blocked` and a
  `Retry-After` of the remaining time, before the handler.
- **The brake against a caller that keeps sending** is its request figure, not the block (the owner's decision 22,
  [Q-LIM-58](#q-lim-58--a-429-of-a-request-figure-is-not-a-refused-request)): a request refused by a request figure counts in no
  request window (R-LIM-017) and is not a refused request (R-LIM-008), so a caller that sends faster than its figure gets its
  figure's rate, never more and never less, for as long as it keeps sending, and a client that waits the `Retry-After` is
  admitted again. A client with many connections that overflows its burst once collects many `429`s at once, and none of them
  moves `refusedPerHour`. The block holds what R-LIM-008 names: refusals of the decision point, failed credentials and unknown
  repositories.
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

- One instance setting, `mode`: `enforce` (the default) or `audit`, changed by a holder of `limits:write` through
  `PATCH /api/v1/limits/settings` with `If-Match` and recent authentication (R-API-007), and in the UI (R-UI-182). Every change is
  audited (`limits.mode.changed`). It reaches every replica within the bound of the change area `limits`.
- **In both modes** every figure is counted, every decision is computed, and the usage store records consumption.
- **In `audit`** no decision of this page refuses or slows a request. Every decision that would have refused or slowed is recorded
  as `would_limit` with its figure: in the usage store (R-LIM-027), in `quader_limits_decisions_total{outcome="would_limit"}`, and in
  the request log (`limits_outcome: would_limit:<figure>`). A block is not set. The new `RateLimit` members are not sent.
- **In `enforce`** the decisions apply as R-LIM-020 to R-LIM-025 say.
- **Nothing protects the protocol paths in `audit`** ([Q-LIM-4](#q-lim-4--in-audit-nothing-on-a-protocol-path-is-refused-or-slowed-the-package-classes-of-r-api-016-included), the owner's decision of
  2026-11-19): no request limit refuses or slows there, the class `package_write` of R-API-016 included; the limits of the
  management API (R-API-016's `read`, `write`, `anonymous`), of the sign-in and of the token endpoints are not modes of this page
  and hold. Switching to `audit` is therefore the step that removes protection, and its dialog says so (R-UI-182).
- A new instance starts in `enforce` (decision 17).
- *Revised on 2026-11-26 by the owner's decision 27 ([Q-LIM-301](#q-lim-301--audit-or-enforce-per-bastion)):* each Bastion has a mode of its
  own (R-LIM-044), which decides its place; this setting decides the main installation's places, a forward of a Bastion included. *Built
  by E8:* the registration's mode. *Built by E9b:* the Bastion applies it to the budgets of its place (R-LIM-044).
- Switching from `audit` to `enforce` does not reset a counter: a subject over a limit is refused at its next request.

Data model: `limit_settings` (one row): `mode text NOT NULL DEFAULT 'enforce' CHECK (mode IN ('audit','enforce'))`,
`block_seconds integer NOT NULL DEFAULT 900 CHECK (block_seconds BETWEEN 60 AND 86400)`, `version`, `updated_at`, `updated_by`.

Acceptance criteria:

- In `audit`, an anonymous address that exceeds `cachedBytesPerHour` of the profile Anonymous downloads on unslowed and unrefused,
  and so does a caller that exceeds `requestsPerSecond` and the burst of `package_write`;
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
  kind of caller (`user`, `technicalToken`, `ci`, `certificate`, `anonymous`, `system`): the number of distinct subjects, requests, refused
  requests, cached bytes, uncached bytes, upstream requests, background downloads, limited and would-limit events. Kept
  `limits.usage.summary_retention` (400 days by default, an estimate), then deleted.
- **Credentials**: for a user, the credential column holds `session` or the personal token's ID; for a certificate subject the
  certificate's serial in lowercase hexadecimal; empty for every other subject.
- **Scale** (an estimate for the reader, not a bound): 2,000 subjects active in an hour, each in three repositories, write 6,000
  hourly rows an hour (4.3 million in 30 days) and up to 72,000 five-minute rows an hour (1.7 million kept).

Data model (migration of work package 2):

```sql
CREATE TABLE usage_bucket (
  granularity          text NOT NULL CHECK (granularity IN ('5m', '1h')),
  bucket_start         timestamptz NOT NULL,
  subject_kind         text NOT NULL CHECK (subject_kind IN ('user', 'technical_token', 'ci', 'certificate', 'anonymous', 'system')),  -- 'certificate': R-CLAMAV-010, a migration of its package
  subject_ref          text NOT NULL,
  credential_ref       text NOT NULL DEFAULT '',
  place                text NOT NULL,
  repository_id        bigint NULL REFERENCES repositories (id) ON DELETE CASCADE,
  via_repository_id    bigint NULL REFERENCES repositories (id) ON DELETE CASCADE,  -- Q-LIM-63; migration 00087
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
  (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id, via_repository_id) NULLS NOT DISTINCT;

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

*Revised on 2026-11-26 (R-LIM-045):* the rows a Bastion reports are written with the place `bastion:<bastion ID>`, the five-minute row and
its hour, and fold into the daily sums like every row. *Built by E8 (2026-11-26).*

`via_repository_id` is the virtual repository the client addressed when the repository that served is a well-known repository of the
project `upstream` and the virtual repository belongs to another project; null otherwise
([Q-LIM-63](#q-lim-63--the-usage-of-a-well-known-member-through-a-teams-virtual-repository-is-recorded-with-that-virtual-repository)).
It names a request the member served, a stored item or a fetch of its remote tier; a merged document of several members is the
addressed repository's row as before, and a virtual repository deleted since leaves the column null. The daily sums and the limit
events (`usage_limit_event`) keep the serving repository only
([Q-LIM-75](#q-lim-75--via_repository_id-names-the-request-a-well-known-member-served-and-nothing-else)). A scope of the overview
holds a row whose serving repository or whose `via_repository_id` is in it (R-LIM-029).

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
  rows older than 30 days; deletes the usage rows and blocks of users that no longer exist
  ([Q-LIM-25](#q-lim-25--the-usage-of-a-deleted-user-is-erased-by-the-daily-task-within-a-day)), the blocks of technical tokens that no
  longer exist, and the rules whose user, group or technical token no longer exists. A technical token is gone when it was deleted,
  with its service account, or with the repository of its account; its usage rows stay until their retention, shown as a deleted
  token (R-LIM-038, [Q-LIM-49](#q-lim-49--a-deleted-technical-tokens-rules-and-blocks-go-with-the-daily-task-its-usage-stays)).
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
| An Owner of a project | the usage rows whose repository belongs to the project now, or whose `via_repository_id` does (what its virtual repositories took from a well-known repository, Q-LIM-63), of every subject kind, with the names of users and technical tokens and the addresses of anonymous callers ([Q-LIM-21](#q-lim-21--a-project-owner-sees-the-addresses-of-the-anonymous-callers-of-its-repositories)); a subject's detail restricted to those rows; the explanation for its repositories; the CSV export of its scope |
| Every signed-in user | its own subject: consumption, limits, the explanation for any repository it can read, blocks of its subject (`/api/v1/users/me/usage`, `/api/v1/users/me/limits`) |
| A technical token, a CI token | nothing of this page through its own credential, unless its scope and its account's role give `usage:read` |

A member who is not Owner sees nothing of the project's usage but its own.

*Revised on 2026-11-26 (R-LIM-045):* a Bastion's rows are rows of the repository they name, so each reader above sees them as rows of that
repository, with their place; a Bastion's status is not usage and is shown to the holders of `endpoints:read`. *Built by E8 (2026-11-26).*

### R-LIM-030 · Metrics, logs and audit events
Priority: **MVP** · Covers: BEYOND-030

- **Metrics** carry no subject (R-OPS-007: no principal, token ID or address as a label value; Q-OPS-175's dashboard stays without
  history, this page's overview has its own store). Added families:
  - `quader_limits_decisions_total{figure,mode,outcome}`: `outcome` `admitted`, `limited`, `would_limit`, `paced`;
  - `quader_limits_bytes_total{class,kind}`: `class` `cached`, `uncached`; `kind` the subject kinds;
  - `quader_limits_upstream_requests_total{kind}`;
  - `quader_limits_blocks_started_total{kind}`;
  - `quader_limits_mode` (gauge, 1 in `enforce`, 0 in `audit`);
  - `quader_limits_index_epoch` (gauge): the epoch of the change area `limits` up to which the replica's index holds every change,
    0 before its first load ([Q-LIM-111](#q-lim-111--a-replica-reports-the-epoch-of-the-limits-it-decides-with));
  - `quader_limits_flushes_total`: the rounds of the replica's charge buffer (R-LIM-017) that ended, also a round with nothing to send
    ([Q-LIM-112](#q-lim-112--a-replica-counts-the-rounds-of-its-charge-buffer));
  - `quader_limits_usage_buffer_keys` (gauge), `quader_limits_usage_flush_failures_total`, `quader_limits_usage_overflow_total`;
  - the limiter's fallback as `quader_ratelimit_fallback{limiter="limits"}`.

  The label `figure` (the API figure names and `ceiling`) joins the enumerations of R-OPS-007.
- **The request log** (R-OPS-010) of a request on a protocol path fills `project`, `repo`, `format`, `cache`, `limiter` and
  `limit_mode` (until now filled by the Bastion only) and gains `limits_subject` (`user:<id>`, `technical_token:<id>`,
  `ci:<issuer>:<project ID>`, `certificate:<subject>`, `anonymous:<prefix>`), `limits_place` (`instance`, `project:<key>`, `repository:<projectKey>/<repoKey>`),
  `limits_rule` (the assignment's ID or `builtin:<kind>`), `limits_outcome` (`admitted`, `limited:<figure>`, `would_limit:<figure>`,
  `paced`, `blocked`), `bytes_cached` and `bytes_uncached`. *Revised on 2026-11-26 (R-LIM-043):* `limits_subject` is also named to a Bastion,
  in the check answer and the marking (`Quader-Bastion-Limits`), so that it charges the same subject; `limits.assignment.*` of a Bastion's
  rule names `place: bastion:<name>`. *Built by E7 and E8 (2026-11-26).*
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

## A Bastion as a place

Specified on 2026-11-26 from the owner's decisions 24 to 28 (W-34; [bastion.md](bastion.md#limits-configuration-and-throughput-from-the-main-installation)
holds the Bastion's side, R-BAS-093 to R-BAS-099). Built by E8 and E9b, the screens by E12: each requirement names its package of the build plan
[developer/bastion-limits.md](../developer/bastion-limits.md). His task of 10:40: "Intern könnten die Limits deaktiviert sein und auf dem
Bastion könnten sie deutlich eingeschränkt sein um Missbrauch der Registry über das offene Internet zu vermeiden oder zumindest
abzuschwächen."

### R-LIM-043 · The Bastion place
Priority: **MVP** · Covers: BEYOND-030

A Bastion is a place of the rules of its own ([Q-LIM-304](#q-lim-304--a-bastion-is-a-place-of-the-rules-beside-the-instance)), one per
registration: its rules decide and its counters count what that Bastion serves, hits and misses, and nothing else. The main installation's
places keep deciding what the main installation serves, a forward of a Bastion included (two budgets,
[Q-LIM-306](#q-lim-306--two-budgets-one-at-the-bastion-one-at-the-main-installation)).

- **Who**: the six kinds of R-LIM-011 (a single user, technical token or CI project, an address or a range, a group, every caller of a
  kind). `every` `administrator` exists at a Bastion place as at the instance, since a preset decides administrators there (R-LIM-046).
- **Resolution** at a Bastion place: step 3 of R-LIM-013 at the Bastion place alone; an administrator is decided there too, not at the
  instance (step 1 applies to the main installation's places). The place always holds the five kind rules a preset wrote (R-LIM-046), so
  resolution ends there and never falls through to the instance. The main installation resolves a signed-in caller's rule, the Bastion the
  address and kind rules ([Q-LIM-302](#q-lim-302--the-main-installation-resolves-a-signed-in-callers-rule-the-bastion-the-address-and-kind-rules)).
- **The budget key** is `<subject>@bastion:<bastion ID>`; the request log's `limits_place` of a forward stays the main installation's
  place, and a Bastion's report names `bastion:<bastion ID>` (R-LIM-045).
- **API**: `GET` and `POST /api/v1/bastions/{name}/limits/assignments`, `GET`, `PUT` and `DELETE …/assignments/{id}`, with the bodies of
  R-LIM-032 and `limits:read` or `limits:write`; a project Owner has no right here, since a Bastion belongs to no project, and R-LIM-016's
  caps do not apply. A kind rule a preset wrote is `builtin` at its place: changed, never deleted (`409 assignment_builtin`).
- **A change** of a rule of the place, or of a profile one of its rules names, writes `limits.assignment.*` with `place: bastion:<name>` and
  raises the registration's revision, which reaches the Bastion within about 3 s ([bastion.md](bastion.md) R-BAS-093).

Data model (migration 00147 of package E8):

```sql
ALTER TABLE limit_assignment
  ADD COLUMN bastion_id uuid NULL REFERENCES bastion (principal_id) ON DELETE CASCADE,
  DROP CONSTRAINT limit_assignment_place_kind_check,
  ADD CHECK (place_kind IN ('instance', 'project', 'repository', 'bastion'));
-- the place check gains: OR (place_kind = 'bastion' AND bastion_id IS NOT NULL AND project_id IS NULL AND repository_id IS NULL),
-- and the other three rows require bastion_id IS NULL; the unique index limit_assignment_who gains bastion_id.
```

*Built on 2026-11-26 by E8:* migration 00147 as above, the administrator's kind rule admitted at a Bastion's place
(`limit_assignment_administrator_check`), the triggers that raise the registration's revision with a rule of its place and with a profile
such a rule names; `limits.PlaceBastion` with `BastionPlace`, its key `bastion:<id>` and log name `bastion:<name>`, its rules in the index
and `Index.ResolveBastion` ([index.go](../../internal/limits/index.go), [model.go](../../internal/limits/model.go)); the five operations
under `/api/v1/bastions/{name}/limits/assignments` (the instance's handlers over the place, [limits_api.go](../../internal/api/v1/limits_api.go));
`limits.assignment.*` naming `place: bastion:<name>` and `bastion.configuration.changed` (Q-BAS-441). *Built on 2026-11-26 by E9b:* the
resolution of address and kind rules at the Bastion and its counters ([bastion.md](bastion.md) R-BAS-096; the budget key is the subject key
in the Bastion's own counters, which belong to one place only). The tests: `TestR_LIM_043_ABastionsPlaceResolvesAlone`
([bastion_place_test.go](../../internal/limits/bastion_place_test.go), milliseconds) and `TestR_LIM_043_TheRulesOfABastionsPlace` (area
`limits`, about 4 s with the stack, rows only).

Acceptance criteria:

- Unit (`internal/limits`, milliseconds): a rule for a technical token at the Bastion place `partners` applies to that token through
  `partners` and not at the main installation; an administrator through `partners` under the preset Internet gets "Bastion: signed-in";
  an anonymous `/24` rule at the place wins over the kind rule for an address inside it.
- Integration (area `limits`, about 4 s): the assignments API at a Bastion place with `limits:write`, `403` without it, `404` for a name
  that is no registration; deleting the registration deletes its rules.

### R-LIM-044 · The mode per Bastion
Priority: **MVP** · Covers: BEYOND-030

The owner's choice (a) of [Q-LIM-301](#q-lim-301--audit-or-enforce-per-bastion): each Bastion has its own Audit or Enforce, independent of
the instance's (R-LIM-026). "Off internally, strict at the Bastion" is the instance in Audit and the Bastion in Enforce with the preset
Internet.

- `limits.mode` of the registration ([bastion.md](bastion.md) R-BAS-093), `enforce` for a new one, changed with `limits:write`, `If-Match`
  and recent authentication (R-API-007), as the instance's mode; recorded in `bastion.configuration.changed` with the part `mode` and its
  before and after.
- **In Audit at a Bastion** every figure of the Bastion place is counted and every decision computed and reported as `would_limit`
  (R-LIM-045); none refuses or slows. The flood protection per address, the anonymous ceiling, the slots and the throughput are not modes
  of this page and hold ([Q-LIM-309](#q-lim-309--in-audit-a-bastion-still-holds-its-flood-protection-ceiling-slots-and-throughput)).
- The instance's mode decides the main installation's places, also for a forward of a Bastion; the Bastion's mode decides its own place.
- Further endpoints get no place and no mode of their own: their bound technical tokens are named by rules already, and an installation
  that runs a partner endpoint expresses "off internally" with Unlimited kind rules rather than with Audit.
- The Bastion exports `quader_bastion_limits_mode` (1 Enforce, 0 Audit).

*Built on 2026-11-26 by E8, the main installation's part:* `limits.mode` of the registration, changed by `PATCH /api/v1/bastions/{name}`
with `limits:write` and, for a change of the mode, recent authentication; `bastion.configuration.changed` with the part `mode`. The
registration's patch takes `If-Match` as every update of a registration does (required by the operation). *Built on 2026-11-26 by E9b,
the Bastion's part:* `settings.Applied.Enforce` decides the budgets ([internal/bastion/budget](../../internal/bastion/budget/budget.go)
`Charge.decide`): in Audit every figure met is counted as `wouldLimit` in the usage and in `quader_bastion_budget_events_total{mode="would_limit"}`,
nothing is refused, paced or blocked; `quader_bastion_limits_mode` exports the mode. The tests:
`TestR_LIM_044_AuditRefusesNothingOfTheBudgetsAndCountsWhatItWouldHave` (`internal/bastion/budget`, milliseconds) and
`TestR_LIM_044_ABastionInAuditServesPastTheBudgetAndStillHoldsItsFloodProtection` (`internal/bastion/engine`, an artifact of 64 bytes).

Acceptance criteria:

- Unit (`internal/bastion/engine`, part of R-BAS-096's 2 s): a Bastion in Audit serves a subject past its `cachedBytesPerHour` and reports
  one `would_limit`, while an address past the flood figure is refused; the same in Enforce refuses.
- Unit (`internal/api/v1`): the mode changes with `limits:write` and recent authentication, `403` with `endpoints:write` alone.

### R-LIM-045 · The usage a Bastion reports
Priority: **MVP** · Covers: BEYOND-030

The owner's choice (b) of [bastion.md](bastion.md) [Q-BAS-324](bastion.md#q-bas-324--a-bastion-reports-usage-and-status-on-the-channel-of-its-download-events):
a Bastion reports its usage and its state on the channel of its download events.

- **What a Bastion sends**: the batch of [bastion.md](bastion.md) R-BAS-016 gains `usage` (rows per subject key, credential, repository and
  rule in buckets of five minutes: requests, refused requests, cached bytes, uncached bytes, upstream requests, and the limit events per
  figure as `limited` or `wouldLimit`) and `status` (per replica: the cache's fill and capacity, hits, misses, evictions, the protected
  share, the throughput's state and rate per class, the counts of limited and blocked callers, and at most 100 active blocks). The bounds
  are [Q-LIM-308](#q-lim-308--a-bastions-usage-report-is-bounded-per-batch-and-folds-the-rest-into-an-overflow-row).
- **What the main installation writes**: the usage rows into `usage_bucket` and `usage_limit_event` (R-LIM-027) at the place
  `bastion:<bastion ID>`, the five-minute row as reported and the hourly row it falls in, through the replica's usage buffer (R-LIM-028);
  the subject from the key, the repository by its qualified name (a name that no longer exists is recorded without a repository), the
  credential as R-LIM-027 says. The status replaces the replica's row of the transient table `bastion_status` (`bastion_id`, `replica`,
  `reported_at`, `status jsonb`; export class transient, not in a backup), deleted with the replica's contact (R-BAS-010).
- **What a reader sees**: the overview's rows and a subject's detail list the consumption per place, a Bastion's place among them, and the
  sum (decision 15's pattern, Q-LIM-306); the place filter of the overview ([api-and-ui.md](api-and-ui.md) R-UI-242); the card's live state
  ([api-and-ui.md](api-and-ui.md) R-UI-240), read from `GET /api/v1/bastions/{name}` as `status` per replica.
- **Who sees it** is R-LIM-029's rule: a Bastion's rows are rows of the repository they name, so a project Owner reads its repositories'
  consumption through a Bastion; the status is shown to the holders of `endpoints:read`.
- No new route and no new background task at the Bastion.

*Built on 2026-11-26 by E8:* the intake hands each usage row to the replica's usage buffer at the place `bastion:<id>` (the subject from its
key; the repository by its name, a name that no longer exists recorded without one; the rule's effective figures from the snapshot of
the registrations for the utilisation; the limit events per figure in their mode), and the overflow rows as the buffer's own overflow
rows of their subject kind ([report.go](../../internal/bastionmain/report.go)); a row that does not read is dropped and counted
([Q-LIM-314](#q-lim-314--a-bastions-usage-row-that-does-not-read-is-dropped-and-counted-never-refusing-the-batch)); the state replaces
the replica's row of `bastion_status` (migration 00147, transient, deleted with the replica's contact and an hour after its last report,
Q-BAS-443); `GET /api/v1/bastions/{name}` shows it as `status`; the usage overview's place filter
([Q-LIM-313](#q-lim-313--the-usage-overviews-place-filter-is-installation-or-bastionname)); a subject's detail names a Bastion's place by
the registration's name. *Built by E12 (2026-11-26):* the screens ([api-and-ui.md](api-and-ui.md) R-UI-240 to R-UI-242). *Built on
2026-11-26 by E9b, the Bastion's part:* the replica's usage buffer
([internal/bastion/usage](../../internal/bastion/usage/usage.go)): rows per subject key, credential, repository and rule in buckets of
five minutes, at most 100,000 between two flushes and the rest folded into an overflow row per bucket, repository and subject kind that
counts the subjects it folded (Q-LIM-308; past 100,000 remembered keys a fold of a new key counts as a subject); a request whose repository
the Bastion does not know (the registry root's `/v2/` and token routes) makes no row. Every flush sends the usage since the last one and the
replica's state, also without an event; a usage the main installation did not take is kept for the next flush
([bastion.md](bastion.md) Q-BAS-474, which also says of each figure of the state whose it is). The tests:
`TestR_LIM_045_TheUsageReportIsBoundedAndFoldsTheRestIntoAnOverflowRow`, `TestR_LIM_045_AReportThatCouldNotBeSentIsRestored`
(`internal/bastion/usage`), `TestR_LIM_045_EveryFlushSendsTheUsageAndTheStateAlsoWithoutDownloads`,
`TestR_LIM_045_OnlyTheFirstBatchOfAFlushCarriesTheReport` (`internal/bastion/downloads`, 2 s with the race detector),
`TestR_LIM_045_TheStatusCarriesTheCacheTheThroughputAndTheBlocksByTheirCodes` (`internal/bastion/server`), and the area `bastion`'s
`TestR_BAS_096_…ReachTheMainInstallation`.
The tests: `TestR_LIM_045_ABatchsUsageGoesToTheBuffer` ([configuration_test.go](../../internal/bastionmain/configuration_test.go),
milliseconds) and `TestR_LIM_045_ABatchsUsageAndStateAreStored` (area `bastionreg`, rows only, under a second).

Acceptance criteria:

- Unit (`internal/bastionmain`, part of E8's 1.5 s): a batch with three usage rows and one overflow row is written at the place
  `bastion:<id>` and the status replaces the replica's row; a key above the bound lands in the overflow row.
- Integration (area `bastionreg`, part of its 3 s): a batch through the operation reaches `usage_bucket` and `bastion_status`.

### R-LIM-046 · The Bastion's profiles and presets
Priority: **MVP** · Covers: BEYOND-030

The owner's choices (a) of [Q-LIM-300](#q-lim-300--a-preset-per-bastion-rules-on-demand-two-profiles-of-the-bastion) and (a) of
[Q-LIM-303](#q-lim-303--the-defaults-of-a-bastion-come-from-measurements).

- **Two built-in profiles**: "Bastion: anonymous" (`bastion-anonymous`) and "Bastion: signed-in" (`bastion-signed-in`), built-ins of
  R-LIM-010 (not deleted, not renamed, their figures changeable). Their figures are set from the measurement E4: the bytes, transfers and
  misses of one normal command of every client through a cold and through a warm Bastion (`npm ci` of 475 packages, `dotnet restore`,
  `docker` and `podman` pulls of a small image, of 1 GB and of 40 layers, `apt-get`, `ansible-galaxy`, `freshclam`, `curl`), with a third
  or more of room as Q-LIM-296 sizes the main installation's profiles. The request figures of "Bastion: anonymous" also hold a polite crawl
  of W-35's web interface; that package measures how many requests a page view and a catalogue page take. No figure is unlimited
  ([Q-LIM-310](#q-lim-310--the-bastion-profiles-take-the-measurements-proposal-with-room-for-one-anonymous-restore-of-200-packages-at-once)).
  The figures, per hour from one address (anonymous) or for one subject (signed in), each with what it holds with a third of room;
  *measured* is a count of the measurement of 2026-11-26 ([developer/bastion.md](../developer/bastion.md#what-a-clients-command-costs-through-a-bastion-measured-2026-11-26)),
  *derived* a use priced from those counts and R-LIM-042's sizes:

| Figure | Bastion: anonymous | Holds | Bastion: signed-in | Holds |
|---|---|---|---|---|
| Requests a second, burst | 50, 1,250 | one cold `npm ci` of 475 and one cold anonymous `dotnet restore` of 200 at once, 475 + 441 = 916 (measured) | 200, 5,400 | an install of 4,000 packages at once (derived); a restore of 200 with a credential sends 802 (measured) |
| Requests an hour | 20,000 | thirty `npm ci` of 475, 14,250 (derived) | 100,000 | twenty installs of 3,000 packages, or fifty restores of 300 with their challenges, 60,100 (derived) |
| Upstream requests an hour | 5,200 | four cold `npm ci`, two cold restores of 200, five first pulls of 1 GB, five `apt-get` of 106 packages, ten Galaxy installs of twelve collections, 3,857 (derived) | 12,000 | two cold installs of 4,000, a cold restore of 300, the first pulls of twelve 1 GB images, 8,781 (derived) |
| Uncached bytes a second, an hour | 50 MB, 9 GB | a first pull of 1 GB in 20 s; the same uses' 6.6 GB (derived) | 125 MB, 20 GB | a first pull of 1 GB in 8 s; 13.9 GB of npm, NuGet and images (derived) |
| Cached bytes a second, an hour | 100 MB, 30 GB | twenty pulls of 1 GB from the Bastion's cache, one in 10 s (derived) | 250 MB, 160 GB | a compose file of twelve 1 GB images pulled ten times from the cache, one in 4 s (derived) |
| Parallel transfers | 270 | one anonymous `dotnet restore` of 200 packages, 200 at once (measured) | 340 | a restore of 200, an `npm ci` (15), five podman pulls (30) and a docker pull (3) at once, 248 (measured counts) |
| Bytes a day | 220 GB | twenty cached pulls of 1 GB an hour for eight hours, 160 GB (derived) | 1,300 GB | the compose file's 120 GB an hour for eight hours, 960 GB (derived) |
| Refused requests an hour | 300 | fifteen `npm ci` on an expired token, 15 refusals each (R-LIM-042) | 1,000 | forty installs that fail on a token without read (derived), as the main installation's Standard |

- **Three presets**, each writing the five kind rules of the place, the flood figures and the mode:

| Preset | `every` `anonymous` | `every` `user` | `every` `technicalToken`, `every` `ci` | `every` `administrator` | Flood figures per address | Mode |
|---|---|---|---|---|---|---|
| Internet (`internet`) | Bastion: anonymous | Bastion: signed-in | Bastion: signed-in | Bastion: signed-in | anonymous 50/s, burst 1,000, 1,000 misses a minute; signed-in 200/s, burst 4,000, 4,000 misses a minute; `gui` 20/s, burst 100; R-BAS-036's and R-BAS-037's figures | Enforce |
| Partner network (`partnerNetwork`) | Anonymous | Standard | Technical | Unlimited | as Internet | Enforce |
| Internal (`internal`) | Unlimited | Unlimited | Unlimited | Unlimited | as Internet | Enforce |

  Unlimited still counts (R-LIM-010); Standard and Technical are W36's figures (Q-LIM-296).
- **Applying a preset** replaces the five kind rules' profiles and overrides, the flood figures and the mode; rules of single callers,
  groups and addresses stay. The registration's `preset` reads `custom` once a kind rule or a flood figure differs from its preset's
  ([Q-LIM-312](#q-lim-312--the-preset-a-registration-reads-is-derived-never-stored-as-custom)). An administrator applies one in one
  request: `PATCH /api/v1/bastions/{name}` with `limits.preset`, after the screen's confirmation
  ([Q-LIM-311](#q-lim-311--a-preset-is-applied-by-the-registrations-patch-in-one-request-before-the-patchs-other-members)).
- **A new registration** starts with Internet in Enforce, the slots of R-BAS-025, the anonymous ceiling E4 sets, and the throughput of
  [bastion.md](bastion.md) Q-BAS-455 (200 MB/s, a quarter for anonymous callers): the Bastion's built-in configuration (Q-BAS-448).

*Built on 2026-11-26 by E8:* the two profiles and, for every registration, the Internet kind rules in migration 00147, a new registration's
kind rules from the service (`bastionmain.Presets`, [configuration.go](../../internal/bastionmain/configuration.go)), the presets in the
registration's patch. The tests: `TestR_LIM_046_TheBastionProfilesHoldOneCommandOfEveryClient`
([bastion_place_test.go](../../internal/limits/bastion_place_test.go), milliseconds, no data), `TestR_LIM_046_EachPresetWritesTheTablesKindRulesAndFloodFigures`
([configuration_test.go](../../internal/bastionmain/configuration_test.go)), the comparison of every built-in profile with the store (area
`limits`, `TestR_LIM_042…` of [limits_test.go](../../test/integration/limits/limits_test.go)) and the five kind rules of a new registration
(area `bastionreg`).

Acceptance criteria:

- Unit (`internal/limits`, milliseconds, no data): at the figures of "Bastion: anonymous" and "Bastion: signed-in", every command of E4's
  table passes a cold Bastion from one address, with the room named; each preset writes the table's kind rules and flood figures.
- Integration (area `limits`, part of E8's 4 s): migration 00147 writes the two profiles and, for each registration, the Internet kind rules.

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
    "parallelTransfers": 6,
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
    "upstreamRequestsPerHour": 200, "parallelTransfers": 6, "bytesPerDay": 100000000000, "refusedPerHour": 100
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
| `GET /api/v1/projects/{projectKey}/limits/maximum` | The project's maximum, or `maximum` `null` and no `ETag` while it has none ([api-and-ui.md](api-and-ui.md) Q-API-356) | 200; 404 (no project) | `limits:read` or an Owner of the project |
| `PUT /api/v1/projects/{projectKey}/limits/maximum` | Set or replace it: a first maximum without `If-Match` (201), a replace with it (200; 428 without) ([api-and-ui.md](api-and-ui.md) Q-API-354) | 200, 201; 412; 422; 428 | `limits:write` |
| `DELETE /api/v1/projects/{projectKey}/limits/maximum` | Remove it | 204; 404; 412 | `limits:write` |
| `GET /api/v1/limits/blocks?active=&place=` | Blocks (active by default), newest first; `place` is `project:<projectKey>` (the project and its repositories) or `repository:<projectKey>/<repoKey>` | 200 | `limits:read`; an Owner with `place` of its project |
| `GET /api/v1/limits/projects` | Per project its maximum (`null` without one) and the number of rules at the project and at its repositories ([api-and-ui.md](api-and-ui.md) Q-API-351) | 200 | `limits:read` |
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
{"project": "web", "profile": "technical", "overrides": {"bytesPerDay": 2000000000000}, "effective": {"requestsPerSecond": 400, "requestsBurst": 2000, "requestsPerHour": 200000, "cachedBytesPerSecond": 500000000, "cachedBytesPerHour": 1000000000000, "uncachedBytesPerSecond": 100000000, "uncachedBytesPerHour": 100000000000, "upstreamRequestsPerHour": 10000, "parallelTransfers": 50, "bytesPerDay": 2000000000000, "refusedPerHour": 2000}, "version": 1, "updatedAt": "2026-11-20T08:30:00Z", "updatedBy": "ada"}
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
| `GET /api/v1/limits/subjects?q=&kinds=&project=` | Suggestions of technical tokens and CI projects for the rule dialog, by name, 20 at most ([Q-LIM-50](#q-lim-50--the-rule-dialog-suggests-every-technical-token-by-name-and-the-ci-projects-tokens-were-issued-for)) | `limits:read` or an Owner of `project` |

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

The list answer: `{"period": {"from", "to"}, "mode": "audit", "items": [...], "nextCursor": "<cursor>"}` (the cursor member of
R-API-010, [Q-LIM-46](#q-lim-46--the-usage-list-names-its-cursor-nextcursor-as-every-list)). `project=<key>` narrows `GET /api/v1/usage` and its export to
the repositories of one project ([api-and-ui.md](api-and-ui.md) Q-API-353). The subject's forms:
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
      "rule": {"id": "builtin:user", "who": {"kind": "every", "callerKind": "user"}, "profile": "standard", "profileName": "Standard", "builtin": true, "capped": false},
      "figures": {
        "requestsPerHour": {"limit": 50000, "used": 31000, "windowSeconds": 3600},
        "cachedBytesPerHour": {"limit": 300000000000, "used": 246000000000, "windowSeconds": 3600},
        "uncachedBytesPerHour": {"limit": 30000000000, "used": 50000000, "windowSeconds": 3600},
        "upstreamRequestsPerHour": {"limit": 2000, "used": 120, "windowSeconds": 3600},
        "bytesPerDay": {"limit": 1000000000000, "used": 1253000000, "windowSeconds": 86400},
        "parallelTransfers": {"limit": 250, "used": 14},
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

*(example)* `budgets` are the subject's budget keys of the last 24 hours, the places its usage rows recorded; `rule` and every
`limit` are what applies there now, from the compiled index
([Q-LIM-52](#q-lim-52--a-budget-key-of-the-detail-names-the-rule-that-applies-there-now)); `used` of a windowed figure is the live
estimate of the Valkey window (one pipeline of reads for the subject's budget keys); a rule names its profile's name
(`profileName`, [Q-LIM-53](#q-lim-53--a-rule-of-the-detail-and-the-explanation-names-its-profile)); `history` holds the five-minute
points of the last 24 hours whatever `period` is; `repositories` and `credentials` follow
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
  "effective": {"requestsPerSecond": 50, "requestsBurst": 200, "requestsPerHour": 5000, "cachedBytesPerSecond": 50000000, "cachedBytesPerHour": 50000000000, "uncachedBytesPerSecond": 10000000, "uncachedBytesPerHour": 20000000000, "upstreamRequestsPerHour": 200, "parallelTransfers": 6, "bytesPerDay": 100000000000, "refusedPerHour": 100},
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
- `formats.ContractVersion` rose to 6 with this page (a method changed meaning: `Host.Serve`), and every format moved in the same change; it
  is at 7 since the probes of W-25 ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-065, ADR-207).
- **The contract test**: the format kit ([internal/formats/formattest](../../internal/formats/formattest)) gains a check that runs
  each example of each `Download` route of a format with a `GET` against a host whose admission refuses, and fails unless the
  answer is 429 in the format's shape with nothing of the body written (or 503, for a format that declares it for a rate limit: the ClamAV
  definitions and Ansible Galaxy, [bastion.md](bastion.md) Q-BAS-453); and `formats/all` pins, per format, the kind of each route
  name in a table (`TestR_LIM_036_EveryRouteOfEveryFormatCountsByItsKind`), so a new route or a new format fails until its kinds are
  listed there, the way `TestR_API_079_…` holds `VersionFetch`.

### R-LIM-037 · The hook for background downloads
Priority: **MVP** · Covers: BEYOND-030

W-25 (a probe starts the download in the background) is built after this page; its trigger, its platforms and its bounds are
[repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-065. This page fixes how such a download is charged, and W-25 changed
nothing of it:

- The decision is taken **when the probe is answered**, on the probe's request: its subject, its place and budget key, and the
  remote's ceiling for its class. A probe through a virtual repository is decided as Q-LIM-2 decides every request through one: the
  place and budget key of the virtual repository the client addressed, the ceiling of the remote member that serves the version
  ([repositories-and-proxy.md](repositories-and-proxy.md) Q-REPO-367), the usage recorded at that member. A background download starts only if step 9a (R-LIM-022) would admit an upstream request for the
  probe's caller at that moment; then the upstream request is counted, for the budget key and the ceiling, at that moment. If it
  would not, no background download starts and the probe is answered as it would have been.
- The background download's uncached bytes are charged to the probe's budget key and to the ceiling at the flush after they were
  read, as a leader's are (R-LIM-006), also when the probe's client left long before.
- The same charge holds for a download of one version that a person starts through the management API and for the fetch for
  examination that a person's request for an allow starts ([remote-curation.md](remote-curation.md) R-CUR-010, R-CUR-015): step 9a
  decides when the operation is called, for the person's budget key and the remote's ceiling of the person's class, and the ticket
  travels with the queue row. A refusal by 9a queues nothing (`429`).
- It is not paced by the caller's `uncachedBytesPerSecond` and takes no parallel lease: it runs as a background request with the
  bounds W-25 gives it.
- The usage store counts it in `background_fetches` of the probe's row (R-LIM-027); in `audit`, a background download that 9a would
  have refused starts, and is recorded as `would_limit`.
- The seam is `limits.Charge.Background(repo)`, which returns the charge a background request carries (`proxy.Request.Charge`).
  The host's decision on the probe calls it through `Charge.StartBackground`, which runs step 9a on it; the download is a job of the
  worker, so the charge travels with the job as a `limits.Ticket` and the worker resumes it with `Engine.Resume`; the upstream request
  9a counted is the download's first exchange, every further one is counted at the flush
  ([Q-LIM-54](#q-lim-54--a-background-download-carries-the-probes-charge-to-the-worker-as-a-ticket)).

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
- **Integration set `limits`** (against the stack through `testenv`, without a format's client): what is the same for every format,
  each once ([Q-LIM-55](#q-lim-55--the-limits-are-tested-once-in-the-integration-set-a-format-adds-one-case-of-its-client)): every
  figure's refusal with its code and `Retry-After`, the download that crosses a byte figure and the next refused before its first
  byte, the slowed download and the refused parallel one, the waiting downloads' share of the request slots, a caller that retries
  every second at a request figure of one a second and is admitted again, a hammering caller held to its figure's rate and not blocked, the block of
  refused requests and its lift, a reader's misses that block
  nothing, the subjects of R-LIM-001 (a user's personal tokens, two technical tokens of one account at each level, an address and an
  IPv6 `/64`, an administrator), what is cached served with the upstream budget used up, the anonymous ceiling against a second
  address, Audit beside Enforce, the switch reaching a second replica; with Valkey stopped, the fallback of R-LIM-019; the cost of
  R-LIM-017; a CI token on the request path charged to its CI project, through the issuer double of the set `ci`; a request through
  a host bound to a repository charged to that repository; the daily fold of hourly rows older than 30 days. A crashed replica's
  leases, which end at their score, are the unit tier's (`TestR_LIM_024_ACrashedReplicasLeasesEndAtTheirScore`): the crash is an
  engine left alone, and the stack can stop a replica only gracefully. The `RateLimit` policy of each figure that refuses beside the
  request figures is a table at the unit tier (`TestR_LIM_020_TheRateLimitPolicyOfEveryFigureThatRefusesBesideTheRequestFigures`).
- **Format contract**: R-LIM-036's kit check and table for the six formats.
- **e2e**: no set of the limits' own (Q-LIM-55). Each format's own set holds one case of its client against a repository of the case
  whose rule refuses the client's next request (`requestsPerSecond` 1 with a burst of 1: a `429` with `Retry-After` 1) and, where it
  downloads, its next download (`cachedBytesPerHour` 1 after a document), in Enforce, with payloads of bytes; the case asserts what the
  client prints, whether it retries and how it exits, and its rows are the manual's table of the clients. The registry's handshake from
  one address with a small anonymous budget is the OCI set's (F-161).

## Gaps of the protocols

What a protocol cannot carry, for the owner to decide (owner, 2026-11-19: "Das muss dann im Zweifel noch mit mir besprochen werden
wie wir vorgehen"). Until he decides, nothing beyond the rest of this page is built for them.

### Gap 1 · NuGet clients and APT do not show why a request was refused

The NuGet clients print the status and the reason phrase and no body (R-NUGET-002); APT prints the status line and reads no body
of an error (R-DEB-005). Over HTTP/2 there is no reason phrase at all, and Go's server writes the standard one. A user of either sees
"429 Too Many Requests" and not which limit or for how long.

As the clients show it (the cases of R-LIM-039, 2026-11-20): `dotnet restore` prints `Response status code does not indicate success:
429 (Too Many Requests).`; APT 3.0 prints `E: Failed to fetch <file>  TooManyRequests 429  Too Many Requests`. Two more clients show
no reason, not named here before: `ansible-galaxy` 2.21 at a refused archive printed `HTTP Error 429: Too Many Requests`, and at a
refused version list a message of its own response cache (`Missing expected 'results' in ansible-galaxy cache`; 2.17 exits 250 there),
which is why a Galaxy endpoint answers a rate limit `503` since 2026-11-26 ([bastion.md](bastion.md) Q-BAS-451): the version list's refusal
then prints its detail and code, the archive's `HTTP Error 503: Service Unavailable` (derived from the client's source, not run); docker at a refused
`HEAD`, which has no body, prints `unexpected status from HEAD request to …: 429 Too Many Requests`. npm, docker at a `GET` and
podman print the `detail`. The manual's table of the clients ([usage-and-limits.md](../manual/user/usage-and-limits.md#what-your-client-shows))
holds every client's line.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) As specified | The body carries the reason; the user reads it on the profile's Usage tab (R-UI-185) | Nothing to build | The client's own output says nothing |
| (b) NuGet: a warning header | Each refusal on a NuGet route also carries the reason in the header the NuGet clients print as a warning (`X-NuGet-Warning`; measured on 2026-11-23 with `dotnet restore` of the .NET SDK 10.0.403, which prints it as `warning : <reason>` before each attempt's status line, and sent since then for the answers of the download checks, [signatures.md](signatures.md) Q-SIG-241, not for the limits' 429) | The message reaches `dotnet restore`'s output | NuGet only; nuget.exe and the older SDKs not measured; printed once per attempt |
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
  blocking stages ([bastion.md](bastion.md)). *Taken up by the owner on 2026-11-26 (decisions 24 to 28) and specified in [A Bastion as a place](#a-bastion-as-a-place),
  not built.*
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
owner reviews the other choices at the walkthrough. The eight he reviewed first are decided by him on 2026-11-19: Q-LIM-2, Q-LIM-5,
Q-LIM-7, Q-LIM-10, Q-LIM-16, Q-LIM-21 and [authorization.md](authorization.md) Q-AUTHZ-296 as built (a), and Q-LIM-4 (b), with
`enforce` the default mode (decision 17).

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

Status: **Decided by the owner (2026-11-19): (a)**, as the development agent had built it.

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

**Applied in the spec:** R-LIM-006, R-LIM-013, R-LIM-037 (a probe through a virtual repository: the budget key at the virtual
repository, the member's ceiling, the usage at the member; [repositories-and-proxy.md](repositories-and-proxy.md) Q-REPO-367).

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

### Q-LIM-4 · In Audit nothing on a protocol path is refused or slowed, the package classes of R-API-016 included

Status: **Decided by the owner (2026-11-19): (b)**, against the development agent's (a).

Context: the owner's decision 17 says "in Audit nothing is refused or slowed". Before the download limits the package classes of
R-API-016 refused on `/api/<format>/`; the question was whether they stay in force while the mode is `audit`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `package_read` and `package_anonymous` keep refusing while the mode is `audit`, on every protocol path including `/v2/`, keyed by the subject; `enforce` replaces them (the development agent's proposal) | The old classes are the net of Audit mode | No installation loses its only request limit by updating | Two systems side by side in Audit mode; "nothing is refused" holds only for the figures of this page |
| (b) Audit refuses nothing at all on protocol paths (chosen by the owner) | One system | The owner's sentence literally | While the mode is `audit` an instance has no request limit on any protocol path; a public remote is open to unbounded upstream requests |
| (c) Enforce the request figures in Audit too, audit the rest | The profiles' request figures always hold | One system for requests | Contradicts decision 17 for a figure of his own |

**Choice:** (b). **Rationale:** the owner's sentence "in Audit nothing is refused or slowed" holds literally, for the classes that
existed before as much as for the new figures: Audit is observation, Enforce is protection. `package_read` and `package_anonymous`
had no effect left in either mode and are removed with their configuration keys (pre-alpha, no compatibility); `package_write`
stays for `enforce` alone ([Q-LIM-27](#q-lim-27--uploads-stay-under-package_write-in-enforce-until-the-upload-figures-exist)). Because Audit now has no request limit, a new instance starts
in `enforce` (decision 17), and the switch to Audit says that it removes the protection (R-UI-182). The gap is H-1015 of
[docs/security/usage-and-limits.md](../security/usage-and-limits.md).

**Applied in the spec:** R-LIM-005, R-LIM-026; [api-and-ui.md](api-and-ui.md) R-API-016, Q-API-344, R-UI-182.

### Q-LIM-5 · A collapsed fetch is charged to its leader alone, also after its client left

Status: **Decided by the owner (2026-11-19): (a)**, as the development agent had built it.

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

Status: **Decided by the owner (2026-11-19): (a)**, as the development agent had built it.

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

Status: **Decided by the owner (2026-11-19): (a)**, as the development agent had built it.

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
| (a) Share by the key's downloads on the replica over its leases in the cluster, refreshed each second; a floor of 64 KiB/s per download (chosen) | No round trip per chunk | The sum stays at the figure while the counts are current; no download stalls | The floor lets many parallel transfers exceed the figure; a second of lag at changes |
| (b) A Valkey token bucket per chunk | Exact | Exact | A round trip per 16 KiB |
| (c) Each replica paces at the full figure | — | Simple | The figure multiplies with the replicas the subject's downloads reach |

**Choice:** (a). **Rationale:** pacing must cost nothing per chunk; the leases already exist for parallel transfers. Revisit if
the floor matters (a figure below `parallelTransfers × 64 KiB/s`). **Applied in the spec:** R-LIM-023.

### Q-LIM-14 · A further download waits up to 10 seconds, then 429

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the owner wrote "further ones wait or 429". npm opens up to 15 connections, the Anonymous profile allowed 4 (64 since
[Q-LIM-294](#q-lim-294--the-built-in-parallel-transfers-are-set-from-a-measurement-of-the-clients); a `dotnet restore` still opens
one per package).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Wait up to 10 s, asking once a second, then 429 (chosen) | A short queue | An install that opens more connections than the figure completes without retries | A waiting request holds a request slot and one round trip per second |
| (b) 429 at once | — | No held slot | npm's retry back-off (10 s and more) slows installs far more than waiting |
| (c) Wait without bound | — | — | Clients time out in the queue |

**Choice:** (a). **Rationale:** most downloads end within seconds; a short wait turns a burst of connections into a queue. The
revisit condition this block named, waiting requests that fill `max_in_flight`, was met by the capacity test (F-163: under overload
the waiting downloads kept their slots while the replica shed every new request); the owner decided on 2026-11-20 to cap them, and
[Q-LIM-60](#q-lim-60--waiting-downloads-hold-at-most-a-quarter-of-an-endpoints-request-slots) sets the share. The wait itself stays.
Revisit if the 10 s wait turns out to hold leases longer than the clients wait (a client's own timeout below 10 s).
**Applied in the spec:** R-LIM-024.

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

Status: **Decided by the owner (2026-11-19): (a)**, as the development agent had built it.

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

**Choice:** (a). The bulk downloads ahead of a client of [remote-curation.md](remote-curation.md) (every version of a package, a
sub-section, and the version an allow starts) are system work of this kind, recorded with the person who started them and bounded by
their preview, their bounds and the queue's (R-CUR-010, Q-CUR-17); a person's download of one version and a request's examination are
charged to that person as R-LIM-037 charges a probe's. **Rationale:** the mirror's operator chose to mirror and bounded it. Revisit if mirrors exhaust an upstream's
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

Status: **Decided by the owner (2026-11-19): (a)**, as the development agent had built it.

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

### Q-LIM-27 · Uploads stay under package_write in Enforce until the upload figures exist

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Narrowed
to `enforce` by the owner's Q-LIM-4 (2026-11-19): in `audit` nothing on a protocol path is refused, a publish included.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `package_write` (10 per second, burst 50; the burst is 400 since Q-LIM-56, 1,600 since Q-LIM-292) holds in `enforce` on every protocol path (chosen) | — | A publish's cost (spool, validation) stays bounded; uploads are Later by the owner | Writes count against the request figures and `package_write` both |
| (b) Writes under the request figures only | — | One system | A publish loop gets 200 per second under Standard |

**Choice:** (a). **Applied in the spec:** R-LIM-005; [api-and-ui.md](api-and-ui.md) R-API-016.

### Q-LIM-40 · The counting writer sits at stage 7; the dispatcher, the raw-file handler and the registry mark only refusals and the serving repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-006 and R-LIM-017 name the format dispatcher, the raw-file handler and the registry root as the places that put the
counting writer around a response. Stage 7 already holds the charge of every protocol request.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Stage 7 wraps the response of every protocol request once; the three entry points mark a refusal and the repository that served (chosen) | One place | No protocol path can be missed (`/v2/` core routes, a 404 of the dispatcher's lookup); the writer and the charge are made together | The writer also sees the bytes of a refusal's problem body |
| (b) Each of the three entry points wraps | As written | The writer knows the route | Three places to keep in step; a path that reaches none is uncounted |

**Choice:** (a). A problem body counts its bytes like every body (R-LIM-006: "the bytes of response bodies"). What decides a download
is the admission (`Host.Serve` for a `Download` route, step 9a for an artifact `GET`), not the writer.

**Rationale:** one wrapping place is the property the owner's "for every format" needs. The development agent would revisit this if a
protocol path appears whose bytes must not count.

**Applied in the spec:** R-LIM-006, R-LIM-017; [developer/usage-and-limits.md](../developer/usage-and-limits.md).

### Q-LIM-41 · The registry's addresses are read by a parser of their own

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `qname.ParseRegistryAddress` beside `ParseAddress`, and `ParseProtocolAddress` over both (chosen) | Two parsers, one rule for names | The other readers of `ParseAddress` (the Bastion's classification, the authentication stage's open routes) keep their answers: none of them serves `/v2/` | Two entry points |
| (b) `ParseAddress` learns the family `registry` | One parser | As R-LIM-005 words it | The Bastion would start to classify `/v2/` paths it does not serve; every reader has to be checked |

**Choice:** (a). **Rationale:** the family is new to the limits only. Revisit when a second reader needs the registry's repository.
**Applied in the spec:** R-LIM-005.

*Superseded in part on 2026-11-22:* the Bastion serves `/v2/` since the owner's decision that it serves every format
([bastion.md](bastion.md#r-bas-051--the-registry-root-at-a-bastion) R-BAS-051, built). It is the second reader the revisit names,
and it calls `ParseRegistryAddress` beside `ParseAddress` ([family.go](../../internal/bastion/engine/family.go)), so the choice (a)
stands and its "none of them serves `/v2/`" no longer holds for the Bastion.

### Q-LIM-42 · A request's charge travels in its context, not in a field of the remote engine's request

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `limits.FromContext(ctx)` in the remote engine (chosen) | The context of the leader's request carries it into the detached flight (`context.WithoutCancel` keeps values) | No format and no host code passes it; a follower that fetches for itself carries its own | Implicit |
| (b) `proxy.Request.Charge` | A field the host fills | Explicit | Every place that builds a `proxy.Request` (the host, the discovery, the virtual engine) must fill it, and a forgotten one is uncounted |

**Choice:** (a). Background requests carry no charge; the engine records them under `system` (Q-LIM-19). **Rationale:** the context is
already how the request's principal reaches the engine. Revisit if a fetch runs under a context that is not its request's.
**Applied in the spec:** R-LIM-022.

### Q-LIM-43 · Step 9a is the fetch's first act, and its refusal is the leader's own outcome

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-022 places 9a after the populate check, and says that a follower of a flight does not pass it while a request that
leads its own fetch after waiting does.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) 9a runs where the fetch starts (`fetcher.run`); a refusal ends the flight with an outcome only its leader reads (chosen) | The request that sends the upstream request is the one that pays | A follower never pays an upstream request; a waiter that becomes a leader passes 9a then; another request that waited fetches under its own budget, never answered by another subject's 429 | 9a runs after the cross-replica flight is acquired |
| (b) 9a before the collapse | As the hook's text | Earlier | Every request that misses at the same moment pays an upstream request, also those that are then served by the leader's fetch |

**Choice:** (a). An expired document is served stale within `maxStaleSeconds` with `Cache-Status: quader; hit; detail=stale-budget`
when the leader's own fetch was refused (Q-LIM-9). **Rationale:** the owner's "the second caller pays cached only". Revisit if the flight
lock held during a refused 9a delays waiters measurably. **Applied in the spec:** R-LIM-022.

### Q-LIM-44 · A block marker holds the block's ID and its end

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The value is `<block ID>\|<end in Unix milliseconds>` (chosen) | One `GET` at stage 7 says whether and how long | The `Retry-After` of `caller_blocked` without a further command; `TTL` is not in the command list of R-ARCH-031 | A longer value |
| (b) The block ID alone and `TTL` | As R-LIM-025 words it | Shorter | A command more in the ACL and in the round trip |

**Choice:** (a). The lift removes the marker with `DELIFEQ` of the same value, which the block's row reconstructs. **Applied in the
spec:** R-LIM-025.

### Q-LIM-45 · The overview's utilisation, state and order are computed by the API from one aggregation per scope

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) SQL aggregates per subject and place (hour and day sums with the limits the rows recorded); the ratio, the state, the filter and the order in the API; an offset cursor (chosen) | Two to three statements per list | One reading of the formula, in Go, the same for the list, the export and the detail | The whole scope is aggregated for every page; with 2,000 subjects in a day that is 2,000 rows |
| (b) Everything in one SQL statement | As R-LIM-034 words it | Paging in the database | The state needs the mode and the active blocks, which the statement would join as well |

**Choice:** (a). **Rationale:** the overview is an administrator's page, read rarely; its cost is bounded by the subjects of one period.
Revisit when an installation has more than 50,000 subjects in a day. **Applied in the spec:** R-LIM-034.

**The cursor stays an offset** (the development agent, 2026-11-23, when a builder named it for a refactor): every page aggregates the
scope again and sorts the rows in Go (`listUsage` of [usage_api.go](../../internal/api/v1/usage_api.go)), so there is no stored order a
keyset cursor could point into, and one would save nothing. What an offset costs is said here: a subject whose counts change between
two pages can move in the order, so a page may repeat or miss a row; the export reads the whole scope in one answer (up to 100,000
rows) and has no such seam. Revisit with the rest of this choice.

### Q-LIM-46 · The usage list names its cursor nextCursor, as every list

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

R-LIM-034 named the cursor `next`; R-API-010 names the cursor of every list `nextCursor`. **Choice:** `nextCursor` (the general rule);
R-LIM-034 is corrected. The screens of work package 3 read either.

### Q-LIM-47 · A delete of the limits honours If-Match when sent

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `DELETE` of a profile, a rule or a maximum takes `If-Match` when sent (`412` for a stale one), as every delete of the API does (`IfMatchOptional`) (chosen) | One convention | The test of R-API-012 holds it | A delete without the header deletes what is there |
| (b) `428` without `If-Match` | As the API section of this page says for a delete | Stricter | The only deletes of the API that require it |

**Choice:** (a). `PUT` and `PATCH` require it (`428`). **Applied in the spec:** R-LIM-031 to R-LIM-033.

### Q-LIM-48 · The token request's request figures are counted once, after its handler

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

**Choice:** Stage 7 checks the block and the class of `GET /v2/_token` and defers its request windows (`limits.Charge.DeferRequestFigures`); the
handler's `Retarget` counts them once: under the subject of a valid Quader token, else under the address. A presented credential
that failed is a refused request of the address. **Applied in the spec:** R-LIM-003.


### Q-LIM-49 · A deleted technical token's rules and blocks go with the daily task; its usage stays

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-028 listed the usage rows, blocks and rules "whose user, group or technical token no longer exists" for deletion,
R-LIM-038 and R-AUTH-045 keep a deleted technical token's usage until its retention.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `limits-cleanup` deletes the rules and blocks of a technical token that no longer exists; its usage rows stay until their retention, named as a deleted token (chosen) | Deleting the token, its account or the account's repository is enough | A token names a system, not a person: there is nothing to erase, and its consumption stays visible for the month it covers | A rule names a deleted token until the next run of the task |
| (b) Its usage rows go too, as a deleted user's | — | One rule for both kinds | The history of a system's consumption is gone the day it is replaced |
| (c) The delete of the token deletes its rules in its transaction | — | No rule names a deleted token | The identity area writes the limits' tables |

**Choice:** (a). Revisit if a technical token's name is found to carry personal data. **Applied in the spec:** R-LIM-028. Code:
`DeleteRulesOfDeletedSubjects`, `EraseBlocksOfDeletedSubjects` in
[limits_usage.sql](../../internal/meta/postgres/queries/limits_usage.sql).

### Q-LIM-50 · The rule dialog suggests every technical token by name, and the CI projects tokens were issued for

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: `GET /api/v1/limits/subjects` answered the subjects that had usage rows; the rule dialog of R-UI-179 searches technical
tokens and CI projects by name, also ones that downloaded nothing yet (a rule is often set before the first request).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Technical tokens from the identity tables by the token's or the account's name; with `project=` the accounts of that project and its repositories. CI projects from the CI issuances by path or ID; for an Owner, who names its project, the CI projects that downloaded from the project's repositories in 30 days (chosen) | — | A token is found the day it is made; an Owner sees no CI project of another project | A CI project that never had a token issued is not suggested (its rule names issuer and ID by hand) |
| (b) The subjects with usage rows | As built first | One source | A new token cannot be chosen |

**Choice:** (a). **Applied in the spec:** R-LIM-034. Code: `SuggestLimitSubjects` in
[usage_api.go](../../internal/api/v1/usage_api.go), `LimitSearchTechnicalTokens`, `LimitCIProjects` in
[limits.sql](../../internal/meta/postgres/queries/limits.sql).

### Q-LIM-51 · The request log's limit_mode of a refusal is the counter's mode, as for every limiter

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-020 put the mode (`audit`, `enforce`) into `limit_mode`; R-OPS-010 defines the field as the limiter's counter mode
(`cluster`, `fallback`, `replica`). The limits refuse only in `enforce`, so the mode said nothing a refusal did not.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `limit_mode` is `cluster` or `fallback`; the mode is read from `limits_outcome` (`limited:` or `would_limit:`) (chosen) | — | One meaning of the field for every limiter | — |
| (b) `audit` or `enforce` | As R-LIM-020 read | — | Two enumerations in one field; a search for `fallback` misses the limits |

**Choice:** (a). **Applied in the spec:** R-LIM-020. Code: `Charge.CounterMode` in [charge.go](../../internal/limits/charge.go).

### Q-LIM-52 · A budget key of the detail names the rule that applies there now

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the usage rows record a budget key's place and its hour and day limits, not its rule (R-LIM-027); the detail of R-LIM-034
names each budget key's rule and every figure, the rates included.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The detail resolves the subject at each place its rows recorded on the index compiled from the store (`limits.Index.ResolveAt`): the rule and the figures that apply there now (chosen) | — | No column more in the hottest table; the rates and the parallel transfers are named too | A rule changed within the day is shown as it is now, not as it was |
| (b) Store the rule's ID in every usage row | — | The rule that applied to each row | A column more in every row, written every ten seconds; still no rates |

**Choice:** (a). Revisit if the owner wants the history of a budget's rules. **Applied in the spec:** R-LIM-034. Code: `budgetKeys` in
[usage_api.go](../../internal/api/v1/usage_api.go).


### Q-LIM-53 · A rule of the detail and the explanation names its profile

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the detail and the explanation name a rule's profile by its key; the profiles are read with `limits:read` or as an Owner
(R-API-082), so a user's own page (R-UI-185) could not name the profile its limits come from.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A rule of the detail, the explanation and its steps carries `profileName` beside `profile` (chosen) | — | Every reader of its own usage reads the name; no profile list is read for one word | A name more in each rule |
| (b) Every signed-in user reads the profiles | — | One read | The figures of every profile are no business of a user |
| (c) The built-in profiles' names in the UI | As built first | No API change | An own profile shows its key |

**Choice:** (a). **Applied in the spec:** R-LIM-034, R-LIM-035. Code: `ruleRef` in [limits_api.go](../../internal/api/v1/limits_api.go).


### Q-LIM-54 · A background download carries the probe's charge to the worker as a ticket

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. How a
background download is charged is R-LIM-037, decided before W-25 was built.

Context: R-LIM-037 takes the decision on the probe's request and charges the download's uncached bytes to the probe's budget key at the
flushes after they were read. The download is a job of the worker ([repositories-and-proxy.md](repositories-and-proxy.md) Q-REPO-361),
which may run on another replica, minutes later; a `*limits.Charge` lives in one replica's buffer. R-LIM-006 counts every exchange with
the upstream as an upstream request; R-LIM-037 says that 9a counts the first when the probe is answered.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The decision's charge becomes a ticket (subject, place, budget key, mode, ceiling key, the key's figures) in the job's arguments; the worker resumes a background charge from it; the exchange 9a counted is the first, every further one counts at the flush, and the resumed charge counts no second background download (chosen) | `Charge.StartBackground`, `Ticket`, `Engine.Resume`, `Charge.Exchange` | The usage rows of the probe's caller hold the download, wherever it runs; the ceiling learns its bytes; nothing of the model changes | A ticket in the job's arguments names the subject's reference (a principal ID, a token's ID, an address prefix), as the usage store does |
| (b) Count the whole download as the one upstream request of the decision | Simpler | One counter | An image of twelve files is twelve exchanges with the upstream; the figure would undercount what an upstream sees |
| (c) Run the download on the probe's replica | The charge is at hand | No ticket | Q-REPO-361 (a) |

**Choice:** (a). The flush also records the upstream request 9a counted for a background charge in the usage store, as it does for a
request's. **Rationale:** the budget key is what the owner's model charges; a ticket is the charge in a form a job can carry. The job's
arguments are kept with finished jobs for `jobs.retention` (30 days by default), the same span as the usage store's hourly rows.
Revisit if a subject's reference must not be stored in job arguments.

**Applied in the spec:** R-LIM-037. Code: [background.go](../../internal/limits/background.go), `fetcher.run` in
[fetch.go](../../internal/proxy/fetch.go). Tests: `TestR_LIM_037_ABackgroundDownloadIsChargedToTheProbe` in
[background_test.go](../../internal/limits/background_test.go).

### Q-LIM-55 · The limits are tested once in the integration set; a format adds one case of its client

Status: **Decided by the development agent** on the owner's rule of 2026-11-20 — selected from the options below; the owner reviews it
at the walkthrough.

Context: R-LIM-039 asked for an end-to-end set `limits` with six clients against a profile small enough to hit each figure once, in
both modes. Built that way it is a leg that loads the client images of every format (the NuGet image alone is 730 MB in the cache)
for behaviour that is the same for every format, and it grows with every format. The owner, 2026-11-20: "Unsere Tests dauern im
Moment schon sehr sehr lange. Und es kommen noch bestimmt 20 weitere Registry Arten dazu. Du musst die Tests so strukturieren, dass es
auch ausführbar bleibt. Es kann nicht einfach alles immer fetter werden und mehr und mehr Daten schieben. Wir schaffen das einfach von
unserer Infrastruktur irgendwann nicht mehr." What only a real client shows is how it reacts to a refusal: whether it reads
`Retry-After`, retries, prints the reason, and with which exit status.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One cross-format e2e set `limits` | Every client, every figure, both modes, every subject, on the kind cluster, in a leg of its own | Every figure seen through a real client | Loads every client image for what the core decides alike for all formats; minutes per format; a new format makes every run of it longer |
| (b) The model in the integration set, one client case per format's own set (chosen) | Each figure, the subjects, the block, the ceiling, Audit and the switch once in `test/integration/limits`, in seconds; each format's set gets one case of its client in Enforce: one refused request with `Retry-After` 1 and, where it downloads, one refused download, with payloads of bytes | A format adds a constant, small amount of time to its own leg and nothing to another's; the clients' rows of the manual come from what the cases assert | A figure is not seen through every client; a client's reaction to a figure other than the two is inferred from the same 429 |

**Choice:** (b). **Rationale:** the refusal is rendered by the core and each format's `WriteError`; what differs per client is its
reaction to a `429`, which two refusals show (one it may retry within a second, one that stays for the hour). Revisit if a client is
found to treat one figure's refusal differently from another's.

**Applied in the spec:** R-LIM-039. Tests: [figures_test.go](../../test/integration/limits/figures_test.go); the cases
`Test<Format>Limits_R_LIM_020_…` of the sets `npm`, `nuget`, `oci`, `deb` and `ansible`
([e2e.md](../developer/e2e.md#what-a-new-format-adds-to-the-tests)).

### Q-LIM-56 · The burst of package_write holds the push of the largest image

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The class
holds on the registry's paths in Enforce by Q-LIM-27 and the owner's Q-LIM-4; its numbers are the development agent's estimates.

Context: measured on the kind cluster on 2026-11-20 (`linux/arm64`, the profile `scatter`): `docker push` of an image of 40 layers of a
few bytes each sends three unsafe requests per layer (the upload session's `POST`, `PATCH` and `PUT`) and the manifest's `PUT`, five
layers at a time: 60 unsafe requests in 0.4 s, 44 in its busiest second. With the class at 10 a second and a burst of 50 per caller,
the 51st was answered 429 by `package_write`, docker printed `error from registry: Too many requests; retry after the period in the
Retry-After header.` and exited 1 without a retry. So in Enforce every push of an image of more than about 16 layers failed. An image
built by docker has at most about 127 layers (its storage driver refuses a deeper one with `max depth exceeded`; a limit of the
client, not checked in this repository): 3 × 127 + the configuration's 3 + a manifest is 385.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A higher default: the burst 400, the rate 10 a second (chosen) | One number; the burst holds the push of any image, and the rate refills it in 40 s | No code, no new concept; a publish loop is still bounded to 10 a second after the burst | A caller may send 400 writes at once on every format, not only on the registry |
| (b) A class of its own for the registry's upload steps | The `PATCH` and `PUT` of an upload session do not count, only its `POST` and the manifest | Counts a push as layers, not requests | A new rule in the request path for one format; an image of more than 48 layers still meets a burst of 50 |
| (c) The upload figures of Q-LIM-27 now | Bytes and requests of writes as figures of the profiles | The model the owner named as Later | A feature of its own, not a default; the owner put it after the first step |

**Choice:** (a). **Rationale:** the measurement shows the refusal is the burst, not the rate: a push is short and dense. A burst that
holds the largest image keeps the class's purpose (a publish loop runs at 10 a second) and removes the failure without a new concept.
After the change the same push of 40 layers passes in Enforce (`TestOCILimits_R_API_016_APushOfManyTinyLayersMeetsTheWriteClass`).
Revisit when the upload figures of Q-LIM-27 come, or if a client pushes several large images within seconds.

Checked on 2026-11-20 against the measurement of
[Q-LIM-59](#q-lim-59--the-built-in-profiles-request-figures-are-set-from-a-measurement-of-real-clients): `docker buildx build --push`
of an image for two platforms with 40 layers sends 95 unsafe requests (two per blob, `POST` and `PUT`, no `PATCH`; the two platforms
shared their 40 layers) and the manifests' `PUT`s, 92 in its busiest second; a pull sends one unsafe request, the `POST` of
`/v2/_token`. The burst of 400 holds every single-platform push and a multi-platform push of up to about 190 distinct blobs with
buildx (about 130 with `docker push`, three per blob); a push of two platforms with more than about 65 distinct layers each through
`docker push`, or four platforms with more than 30, meets it, and docker exits 1. The class is not changed here: the owner's decision
18 is about the profiles' request figures, and an image of that size per platform was not measured.

Measured on 2026-11-25 and changed by [Q-LIM-292](#q-lim-292--the-burst-of-package_write-holds-the-push-of-the-largest-image-on-four-platforms):
a push of two platforms of 70 distinct layers through `docker push`'s requests met the burst of 400 at its 400th unsafe request, 2.5 s in;
the burst is 1,600 since.

**Applied in the spec:** [api-and-ui.md](api-and-ui.md) R-API-016's configuration. Code: `APIRateLimitPackageWrite` in
[m1.go](../../internal/config/m1.go).

### Q-LIM-57 · The request windows are decided and counted in one step, by a script in the round trip of stage 7

Status: **Decided by the owner on 2026-11-20: a refused request is not counted in the request windows** (decision 19). How it is
built is **decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-017 counted a request in its windows (`INCR`) before the decision, so a refused request filled the window too, and a
client that waited the second its `Retry-After` named and came back kept the window full itself: at a tight figure it was never
admitted again. The owner kept the constraint he had been told about: a request on a protocol path costs one pipelined round trip to
Valkey at stage 7. Valkey has no command that adds to a counter only below a bound; `DELIFEQ` is the one conditional command the
installation already uses.

| Option | Round trips per request | For | Against |
|---|---|---|---|
| (a) One script that reads every window the request must fit and the block marker, and adds the request to all of them only when it fits all and no block holds; sent as `EVALSHA` inside the `MULTI` of stage 7 (chosen) | 1; after a lost script cache (a restarted primary, a failover) 1 more, once per replica | Exact: a refused request is in no window, its `Retry-After` is true, and the windows, the block and the class of R-API-016 are all or nothing; the same step serves every limiter of R-ARCH-034 (Q-LIM-61) | The ACL of R-ARCH-031 gains `EVALSHA` and `EVAL`; Valkey neither persists nor replicates its script cache, so `NOSCRIPT` must be handled |
| (b) Counters of refusals beside the windows, the refusal counted in a second round trip, as the Bastion's guard does (Q-BAS-127) | 1 admitted, 2 refused | No new command | A caller that hammers doubles its own cost on Valkey; a request decided while another's refusal is not counted yet sees one too many |
| (c) Counters of refusals carried by the flush (`limits.flush_interval`, 1 s) | 1 | No new command, no second round trip | The refusals are subtracted up to a second late: a client that comes back after a `Retry-After` of 1 s can meet its own refusals, so the `Retry-After` is not true, which is what the owner asked for |
| (d) Read the windows, decide, then count in a second round trip | 2 for every admitted request | No script | Doubles the cost of every request; two replicas can both admit the last place of a window |

**Choice:** (a). The script ([valkey_take.go](../../internal/cluster/valkey_take.go)) runs `GET` on the current and the previous
counter of each window and on the marker, and `INCRBY` and `EXPIRE NX` on the current counters when the request fits: at most three
windows and nine commands inside Valkey, against seven commands of the transaction before (three per window and the marker's `GET`),
with the same one round trip. Valkey with Sentinel runs scripts on the primary and replicates their writes as effects; its script
cache is per server and not replicated, so the first take on a promoted primary is answered `NOSCRIPT`, does nothing, and is sent once
more as `EVAL` with the body, which loads it. The script reaches only the keys it is given and runs under the ACL of the Valkey user,
so `EVAL` grants no command the user does not hold.

**Rationale:** only a step that decides and counts at once makes the owner's rule exact, and inside the existing transaction it costs
no round trip. The development agent would revisit this if an installation's Valkey refuses scripts (a managed service that turns
`EVAL` off): then (c) is the variant without a script, at the price of a `Retry-After` that can be a second short.

**Applied in the spec:** R-LIM-017, R-LIM-018, R-LIM-019, R-LIM-020; [architecture.md](architecture.md) R-ARCH-031 (the command list),
R-ARCH-034. Code: `OpTake` in [valkey_limits.go](../../internal/cluster/valkey_limits.go), [valkey_take.go](../../internal/cluster/valkey_take.go),
`Engine.Check` in [engine.go](../../internal/limits/engine.go). Tests: `TestR_LIM_017_ARefusedRequestFillsNoRequestWindow` in
[engine_test.go](../../internal/limits/engine_test.go), `TestR_LIM_017_ARefusedRequestFillsNoRequestWindowAndAHammeringCallerIsNotBlocked`
in [figures_test.go](../../test/integration/limits/figures_test.go) (against the stack's Valkey with Sentinel).

### Q-LIM-58 · A 429 of a request figure is not a refused request

Status: **Decided by the owner on 2026-11-20: (b)** (decision 22), "Option B: Ein 429 zählt nicht zur Sperre", over the
development agent's (a), which had been built.

Context: with Q-LIM-57 a refused request fills no request window. Built with it, a `429` `rate_limited` of a request figure was made
a refused request of R-LIM-008, counting toward `refusedPerHour` and its block (R-LIM-025), on the development agent's wrong statement
that it already counted. Before, R-LIM-008 named only the refusals of the decision point, failed credentials and unknown repositories.
The owner was asked whether to keep it, to take it back, or to count at most one refusal a second.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep it: every `429` `rate_limited` of a request figure is a refused request (in `audit`, a request the figures would have refused) | The refusals beyond the figure count toward `refusedPerHour`, and the block stops a caller that keeps sending | A caller that keeps sending past its figure for long is shut out for the block's time | A client with many connections (three `npm install` at once behind one NAT address) overflows its burst once, collects hundreds of `429`s in a second and sits in the block of 15 minutes at once (Anonymous: 60 refusals an hour), against the owner's decision that a client that waits is admitted again (Q-LIM-57); the block saves the server nothing, since a blocked request costs the same round trip as a `429` |
| (b) Take it back: a `429` of a request figure does not count toward the block (chosen) | A caller that keeps sending past its figure is held to the figure's rate and nothing more; the block holds what R-LIM-008 named before | A client that waits is admitted again however many of its connections were refused; no NAT address is blocked by its own clients' burst; the block stays for what it was built for | A caller that keeps sending is never shut out; it costs one round trip per request for as long as it sends |
| (c) Count at most one refusal a second of a budget key | A burst of refusals counts once; a caller that keeps sending for more than `refusedPerHour` seconds of an hour is blocked | A burst no longer blocks at once | A caller that keeps sending is still blocked after a minute (Anonymous); one more counter per budget key, and the block still saves no work, since a blocked request costs the round trip a `429` does |

**Choice:** (b). A request a block refuses is no refused request either, so a block does not lengthen itself.

**Rationale:** a refusal of a request figure costs the server one round trip at stage 7, the same as a refusal of the block, so the
block buys nothing against a caller that keeps sending; it only punishes clients whose many connections overflow a burst at once. The
request figure alone holds such a caller to its rate. **Revisit** if a caller is found to cost the installation by hammering at its
figure's rate (the round trips of its refusals, the log lines, the work before stage 7).

**Applied in the spec:** R-LIM-008, R-LIM-025; [security/usage-and-limits.md](../security/usage-and-limits.md) H-1010. Code:
`checkWindow` in [engine.go](../../internal/limits/engine.go), which marks no refusal. Tests:
`TestR_LIM_008_ARequestFiguresRefusalIsNoRefusedRequest` in [engine_test.go](../../internal/limits/engine_test.go) (enforce, audit,
the replica's share, and the refusals R-LIM-008 names still counting),
`TestR_LIM_017_ARefusedRequestFillsNoRequestWindowAndAHammeringCallerIsNotBlocked` in
[figures_test.go](../../test/integration/limits/figures_test.go).

### Q-LIM-59 · The built-in profiles' request figures are set from a measurement of real clients

Status: **Decided by the owner on 2026-11-20: larger default bursts, set from a measurement** (decision 18). The numbers are the
development agent's, from the measurement — selected from the options below; the owner reviews them at the walkthrough.

Context: `docker pull` and `docker push` do not retry a 429 of the registry and exit 1; podman and npm wait what `Retry-After` says.
The owner chose built-in request figures that normal use never reaches over holding a request on the server; a caller that stays
above a figure is still refused at once. The figures of R-LIM-010 were estimates nobody had measured (Anonymous 20 requests a second
with a burst of 100 and 2,000 an hour; Standard 200, 1,000, 50,000; Technical 400, 2,000, 200,000; Restricted 50, 200, 5,000).

**The measurement**, run once on 2026-11-20 on the kind cluster (`linux/arm64`, the profile `scatter`, the release image of the day),
with the real clients of the e2e harness and a temporary test that read the request log of the packages replicas; not a test of the
suite. "Charged" are the requests the limits count; the challenge without a credential is not one (R-LIM-005).

| Client, command | Charged requests | In | What they are |
|---|---|---|---|
| `docker login` | 2 (+1 challenge) | 10 ms | the version check with the credential, the token |
| `docker pull`, an image of one layer (docker builds it as an index with an attestation) | 9 (+1) | 88 ms | 2 `HEAD` of the tag, the token's `POST`, 3 manifests by digest, 3 blobs |
| `docker pull`, an image of 40 layers | 48 (+1) | 0.28 s | 2 `HEAD`, the token, 3 manifests, 42 blobs (39 in its busiest second) |
| `docker pull`, an image of two platforms | 9 (+1) | 78 ms | as one layer: the index, one platform's manifest and blobs |
| `docker pull` of an image already present | 3 (+1) | 20 ms | 2 `HEAD`, the token |
| `docker pull` without a credential, a public image | 8 | 1.1 s | as above, charged to the address |
| `podman pull`, one layer / 40 layers / two platforms / present | 5 / 44 / 5 / 4 (+1 each) | 40 ms / 0.48 s / 38 ms / 33 ms | the version check, the tag's manifest, manifests by digest, blobs |
| `docker buildx build --push`, two platforms of 40 layers | 146 (95 unsafe) | 0.62 s | `HEAD` and `POST`, `PUT` of each blob, 5 manifests (Q-LIM-56) |
| `npm ci` of a lockfile of 475 packages, warm cache | 475 | 2.5 s | one tarball each (npm ci reads no package document); recorded by the npm load of R-HA-059 on the same cluster ([e2e.md](../developer/e2e.md#drain-and-failover-under-npm-load)), not run again |

From the protocol, not run (a run of each would cost minutes for a number the protocol gives):

| Client, command | Requests | Over |
|---|---|---|
| `npm install` without a lockfile, N packages | 2N: a package document and a tarball each | seconds; npm opens up to 15 connections |
| `dotnet restore`, N packages over F feeds | about F + N × (F + 1): the service index of each feed, the version list of each package on each feed, the package | seconds |
| `apt-get update` and `apt-get install` of N packages, Debian's three suites | about 10 to 15 for the update (an `InRelease` and the indexes by hash per suite, component and architecture), then N | seconds; APT pipelines up to 10 requests on a connection |
| `ansible-galaxy collection install`, C collections with their dependencies | about 4 C: the version list (paged), the version, the archive | one after the other, tens of seconds |

**Normal use, as the figures take it:**

- **Anonymous** (one address, often a NAT of several people or CI jobs): ten pulls of 40-layer images in a row (480) or two
  installs of 475 packages at once (950) within the burst; about 40 such installs or 400 such pulls an hour.
- **Standard** (a person): one install of up to 4,000 packages, or a compose file pulling a dozen images (about 600), at once; an
  install of 475 packages every 2.5 s sustained (190 a second) stays below the rate.
- **Technical** (a system with one token, a CI runner): twenty parallel jobs of one token, each installing 475 packages within
  seconds (9,500), within the burst; a CI job that pulls a dozen images is far below.
- **Restricted** (a caller an administrator narrows): one install of 475 packages and a few pulls at once.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Each built-in burst holds the normal command of its kind of caller with room, the rate its sustained use, the hour 20 to 50 bursts (chosen) | Anonymous 50/s, 1,000, 20,000/h; Restricted 100/s, 1,000, 25,000/h; Standard 200/s, 4,000, 100,000/h; Technical 1,000/s, 20,000, 1,000,000/h | No client meets a figure in normal use; the order of the profiles stays; a caller far above is refused within its window | A caller may send a burst of 1,000 anonymous requests at once, 20,000 with a technical token |
| (b) One burst for every profile that holds the largest single command (500) | | Simple | A NAT of several people, or twenty CI jobs of one token, meets it; docker then fails |
| (c) No request figure for signed-in callers | | Nothing to tune | No bound on a token that loops; the hour figure protects the installation's capacity per caller |

**Choice:** (a). The windows of the bursts are 20 s (10 s for Restricted). The hour is consistent with the burst: Anonymous 20 and
Restricted 25 bursts, Standard 25, Technical 50 (a system runs all day); each is far below what the rate would allow in an hour, so a
caller that stays at its rate meets the hour figure, not the burst. Only the request figures change: the byte, upstream, parallel and
refused figures keep the estimates of the first migration (the parallel transfers were set from a measurement of their own on
2026-11-26, [Q-LIM-294](#q-lim-294--the-built-in-parallel-transfers-are-set-from-a-measurement-of-the-clients), and the byte, upstream
and refused figures from the uses they hold on the same day, [Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises)).

**`package_write`** (Q-LIM-56, a burst of 400 at 10 a second, 1,600 since Q-LIM-292): a pull spends one unit (the `POST` of the
token); it holds every single-platform push and the measured multi-platform push (95 unsafe requests). The burst of 400 did not hold a
push of two platforms with more than 65 distinct layers each through `docker push` (three unsafe requests per blob), measured on
2026-11-25; the burst of 1,600 holds four platforms of 127 layers ([Q-LIM-292](#q-lim-292--the-burst-of-package_write-holds-the-push-of-the-largest-image-on-four-platforms)).

**Rationale:** the owner's rule asks that normal use never reaches a figure; the measurement shows normal use is dense (a pull of
48 requests in a third of a second, an install of 475 in 2.5 s), so a burst, not the rate, is what a client meets first. The
development agent would revisit these numbers when a client or a format is found whose normal command needs more than its profile's
burst, or when the installation's capacity per replica (R-ARCH-027) is below what twenty technical bursts ask at once.

**Applied in the spec:** R-LIM-010, R-LIM-005's acceptance criteria; [api-and-ui.md](api-and-ui.md) R-API-016's acceptance criteria.
Code: migration [00085_limits_request_bursts.sql](../../internal/meta/postgres/migrations/00085_limits_request_bursts.sql),
`BuiltinProfiles` in [model.go](../../internal/limits/model.go).

### Q-LIM-60 · Waiting downloads hold at most a quarter of an endpoint's request slots

Status: **Decided by the owner on 2026-11-20: waiting downloads are capped** (decision 21; over leaving it and over refusing at once
only while the replica already sheds). **The share is the development agent's** — selected from the options below; the owner reviews
it at the walkthrough.

Context: a download beyond the subject's `parallelTransfers` waits up to 10 s for a place (Q-LIM-14), holding its request slot of
step 4 of the chain (R-ARCH-005). In the capacity test of R-ARCH-027 (`TestNPMPerf`, P-8: 1,000 requests a second of a 50 KiB tarball
over 20 technical tokens, each with 50 parallel transfers) the waiting downloads held their slots while the replica shed every new
request; the test passed only because its tokens were given the profile Unlimited (F-163; the nightly of 2026-11-19 answered 1,922
requests 429 `too_many_parallel_transfers`). The slots are the endpoint's: `server.max_in_flight` (4,096) for the default endpoint, a
further endpoint's own `max_in_flight` (1,024 by default).

Found while the capacity test ran without the exemption on 2026-11-20: the 429s had a second cause. A batch of the limits was bounded
to 255 operations, and the flush of a replica that ended more downloads than that in one second failed as a whole: the ended
downloads' leases stayed in Valkey for their full minute (`limits.parallel.lease`), so after 50 downloads a token got no place for a
minute, and its waiting downloads could never get one. P-8 answered 1,000 requests 200 and then 27,885 429 (3,072 of them after the
10 s wait, the rest at once because the waiting share was full). The bound is gone (R-LIM-017: the flush of a thousand ended downloads
is one round trip, `TestR_LIM_017_AFlushOfAThousandEndedDownloadsIsOneRoundTrip`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A quarter of each endpoint's slots on each replica, a constant (chosen) | 1,024 waiting downloads of 4,096 slots, 256 of 1,024 | Three quarters stay for requests that are served: P-8's 1,000 a second is served with up to 3 s per answer; 1,024 waiting places are about 90 installs at once that each open 15 connections at a figure of 4 (11 wait each); it follows a changed `max_in_flight` without a second setting | A burst of more than 90 such installs on one replica within the same seconds gets 429 at once instead of after a wait |
| (b) Half | 2,048 of 4,096 | More installs queue | Half the replica is waiting; P-8's answers would have to stay under 2 s |
| (c) An eighth | 512 of 4,096 | Most slots serve | About 45 installs at once; npm's retry after 10 s and more slows each further one far more than the wait would |
| (d) A setting `limits.parallel.waiting_share` | Tunable | An administrator can tune it | A number nobody can choose without the measurement the default already rests on; the owner's "simple by default" |
| (e) One bound per replica over all endpoints | | One counter | The slots are per endpoint: a further endpoint's 1,024 slots could all be held by waiters of a bound sized for 4,096 |

**Choice:** (a), per endpoint and replica, the constant `limits.WaitingShare` (4). A download that would wait while the share is held
gives up its lease and gets 429 `too_many_parallel_transfers` with a `Retry-After` of 1 s at once.

**Result** (the capacity test once on the kind cluster, `linux/arm64`, 2026-11-20, its twenty tokens under the profile Technical in
Enforce, without the exemption): P-8 offered 1,000 requests a second of the cached 50 KiB tarball and got 25,376 answers 200 and 3,399
503 `server_busy` (the replica's own shedding, which the test allows), no 429; P-7 56,141 answers 200 and 1,368 shed. The test passed.
The run shows the two fixes together (the share and the flush's batch without a bound); it does not tell how much each contributed.

**Rationale:** what an install needs is a queue of a few seconds for its own surplus connections; what the replica needs is most of
its slots for requests it serves. A quarter gives both on the default endpoint with an order of magnitude to spare. The development
agent would revisit the share if installs on one replica are refused at once in normal use (`quader_limits_decisions_total{figure=
"parallelTransfers",outcome="limited"}` without overload), or if an endpoint with few slots (below 40) makes the share smaller than
one install's surplus.

**Applied in the spec:** R-LIM-024, Q-LIM-14 (its revisit note). Code: [waitgate.go](../../internal/limits/waitgate.go),
`Engine.parallel` in [engine.go](../../internal/limits/engine.go), `waitGates` in [ratelimit.go](../../internal/api/v1/ratelimit.go).
Tests: `TestR_LIM_024_ParallelTransfers` in [engine_test.go](../../internal/limits/engine_test.go),
`TestR_LIM_024_WaitingDownloadsHoldAtMostTheirShareOfTheRequestSlots` in [figures_test.go](../../test/integration/limits/figures_test.go);
`TestNPMPerf` without its tokens' exemption ([npm_perf_test.go](../../test/e2e/npm_perf_test.go)).

### Q-LIM-61 · Every request window of the installation follows one rule: a refused request counts nothing

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the request windows of the download limits and the limiters of R-ARCH-034 (the management API's classes of R-API-016,
`package_write`, the endpoint's class of R-EP-015, the CI exchange's of R-CI-021, the starts of the login flow and the Bastion
download events of the main installation) share one arithmetic and, since R-LIM-017, one batch: `package_write` is decided in the take
of stage 7. `Limiter.Take` counted a request before it decided; its own fallback, a token bucket, took nothing when it refused, and the
Bastion's guard took nothing either (Q-BAS-127).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every take of a limiter of R-ARCH-034 counts the request only when it is admitted, through the same script (chosen) | One rule for every request window | A limiter means the same with Valkey and on its fallback; no caller slightly over a limit starves (Q-BAS-127's finding); every `Retry-After` is true; one code path | A caller that hammers the management API is held to its class's rate instead of being shut out while it keeps sending |
| (b) Only the windows of the take of stage 7 (the figures and `package_write`); the other limiters keep counting refusals | | The login and CI limiters keep their penalty | Two rules; a limiter that admits differently with and without Valkey |
| (c) Only the download limits' figures; `package_write` counted as before | | Smallest change | The take is no longer all or nothing: a write the class refuses would fill the figures' windows |

**Choice:** (a). The count-only form of R-ARCH-034 (`Exceeded`, `Count`: failed credentials, wrong device codes, R-AUTH-038) is not a
request window and is unchanged: it counts failures, which is its purpose, and it is the brake against guessing. The Bastion's guard
keeps its counters of refusals (Q-BAS-127), the same rule without a script, because its Valkey user runs none.

**Consequence for the fallback while Valkey is unreachable** (R-LIM-019, Q-LIM-12): a limiter's token bucket already took nothing on a
refusal; the figures' local windows run the same take under the replica's lock, at the replica's share; the class in the take of stage
7 decides on its own bucket, and a request it refuses after the local windows took it is given back to them; a request the local
windows or the block refuse after the class took it is given back to the class. Between two limiters that run in two steps (the
endpoint's class before stage 7, then the take) a request the second refuses is given back to the first
([Q-LIM-110](#q-lim-110--an-upstream-request-step-9a-refuses-is-counted-nowhere-and-a-request-stage-7-refuses-is-given-back-to-the-endpoints-class)).

**Rationale:** one rule is the simpler product, and the old rule was the only place where a limiter's cluster mode and its fallback
disagreed. The development agent would revisit this if a class of the management API needs a penalty for callers that ignore
`Retry-After`; the block of R-LIM-025 is the model for one.

**Applied in the spec:** [architecture.md](architecture.md) R-ARCH-034; [api-and-ui.md](api-and-ui.md) R-API-016; R-LIM-019. Code:
`Limiter.TakeNAt`, `Limiter.BatchTake` in [limiter.go](../../internal/cluster/limiter.go), `windowTake` in
[valkey_ratelimit.go](../../internal/cluster/valkey_ratelimit.go). Tests: `TestLimiterCluster` in
[limiter_test.go](../../internal/cluster/limiter_test.go).

### Q-LIM-62 · A request through a virtual repository to a well-known member is decided at the addressed repository, and the member's ceilings bound it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a virtual repository of any project may list a well-known repository of the project `upstream`
([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-071). [Q-LIM-2](#q-lim-2--a-request-through-a-virtual-repository-is-decided-at-the-repository-the-client-addressed)
decides every figure at the addressed repository and applies the remote member's ceilings; the project `upstream` has no Owner.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Q-LIM-2 unchanged: the team's rules decide at its virtual repository, the well-known member's ceilings bound all callers of a class together; rules at `upstream` and its repositories apply to requests addressed there; `limits:write` writes them, ceilings also `repositories:write-config` (chosen) | One resolution per request, as everywhere | "Which limit applies to me here" keeps one answer, the repository the client configured; the installation's upstream allowance is still protected at one place, the ceiling | An administrator's rule on `upstream/docker-hub` does not apply to a team that reaches it through its virtual repository |
| (b) Decide at the well-known member | The central repository's rules for every team | An administrator's rule reaches every team | The request figures are decided before the member is known (Q-LIM-2 (c)); two answers to "which limit applies" |
| (c) An Owner of the listing project writes the member's ceiling | Teams protect the upstream | — | One team's ceiling would bound every other team |

**Choice:** (a). System work through `upstream/scanner-trivy` is charged to no budget and no ceiling (R-LIM-015).

**Rationale:** the ceiling is the instrument for the upstream's quota, which is the installation's; the rules are the instrument for a
caller's share, which the addressed repository decides. The development agent would revisit this if administrators need a per-team
bound on a central repository; then a ceiling per listing project.

**Applied in the spec:** R-LIM-015, R-LIM-016; [authorization.md](authorization.md) R-AUTHZ-041; [repositories-and-proxy.md](repositories-and-proxy.md)
R-REPO-069.

### Q-LIM-63 · The usage of a well-known member through a team's virtual repository is recorded with that virtual repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: usage is recorded at the member that served (Q-LIM-2), and an Owner sees the rows whose repository belongs to its project
(R-LIM-029). A team whose public packages come from `upstream/npmjs` instead of a remote of its own would no longer see that traffic.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The row gains `via_repository_id`, the addressed virtual repository of another project, and the Owner of that project sees those rows (chosen) | One column in the key of `usage_bucket`, migration 00087 | A team keeps seeing what its builds take from the public registries; an administrator still sees each central repository's total | One more key column; a request through a virtual repository of the same project writes nothing new |
| (b) Record at the addressed virtual repository instead | — | No new column | The central repository's usage disappears from its own rows, which is what an administrator watches |
| (c) Nothing | — | No change | Teams lose sight of their public-registry traffic when they move to the central repository |

**Choice:** (a). The daily sums without a person stay per serving repository.

**Rationale:** both views are needed: the team's (what did my builds pull) and the installation's (what does the central repository
cost); one column gives both without a second write. The development agent would revisit this if the extra key column measurably
grows the hourly rows (one virtual repository per team and format adds none in the common case).

**Applied in the spec:** R-LIM-027, R-LIM-029.

### Q-LIM-66 · A scan token's request is system work: recorded under its repository and limited by nothing

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The owner named
it as a point to settle first (2026-11-20): the scanner's pull is not charged to the download limits. The credential is
`vuln-scanner-trivy` since [ha-and-helm.md](ha-and-helm.md#q-ha-373--every-name-an-operator-or-an-auditor-reads-of-a-scanner-unit-carries-its-tool) Q-HA-373 (2026-11-21); the text below keeps the name of its day.

Context: the scanner pulls each image it analyses through the scan listener of `packages` with a scan token
([formats/oci.md](formats/oci.md) R-OCI-037), whose principal is of the kind `system`, a kind the table of R-LIM-001 did not name.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The subject `system`, recorded under the repository with the credential `image-scanner`, checked against nothing (chosen) | As Q-LIM-19 treats the mirror and the background requests | Scanning never refuses itself or a client; the overview shows what the scanner read per repository | A scan backlog after a database update reads a lot from the cache and is bounded by the scanner's capacity, not by a figure |
| (b) Charge the scanner's reads to whoever pushed or pulled the image | A subject per image | Costs follow the cause | The pusher of an image would pay for every rescan the installation decides; a remote's image has no single cause |
| (c) Not record them at all | Nothing in the usage store | Smaller store | The overview could not show where the cache's bytes went |

**Choice:** (a). The scan listener's requests are counted as requests and cached bytes of `system` at the repository, take no parallel
lease, and meet no request figure, byte figure, ceiling or block; they are never refused requests of anybody. **Rationale:** the scanning
is the installation's own work, bounded by its own capacity (replicas × slots), as the mirror is by its own bounds. The development agent
would revisit this if a rescan of a large registry shows up in the overview as a burden the operator wants to bound.

**Applied in the spec:** R-LIM-001; [formats/oci.md](formats/oci.md) R-OCI-037; [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-042.

### Q-LIM-75 · `via_repository_id` names the request a well-known member served, and nothing else

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: Q-LIM-63 adds `via_repository_id` to the rows of `usage_bucket` and says the Owner of the virtual repository's project sees them.
Building it raised what else of a request through a virtual repository carries it: the usage store records a request under the repository
that served it, a merged document of several members is served by none, and the limit events (`usage_limit_event`) are a table of their
own.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The column names only a request a member of `upstream` served (a stored item, a fetch of its remote tier); a merge stays the addressed repository's row; the limit events and the daily sums keep the serving repository (chosen) | One key column, as Q-LIM-63 specified; the bridge's tiers record the member and the virtual repository together (`Charge.ServedThrough`) | The team's rows are what its builds took from the central repository; nothing is counted twice; no second key change | A limit the team's caller met while a central member served it (a pacing in Audit) is counted at the central repository and not in the team's events |
| (b) Every row of a request through a virtual repository of another project carries it, a merge included | The addressed repository in every row | One rule | A merge's row would name a serving repository that did not serve |
| (c) The limit events carry it too | A column and a key of `usage_limit_event` as well | The team sees every event of its builds | A second key change for a count that is rare at a member, since the refusals of a team's caller are decided at its virtual repository, whose rows the team sees |

**Choice:** (a).

**Rationale:** the refusals that decide a team's share happen at the addressed repository (Q-LIM-2), and their events are already the team's;
what a central member adds is the bytes and requests it served, which is what the column carries. The development agent would revisit this
if Owners ask for the limits their callers met at a central repository.

**Applied in the spec:** R-LIM-027.

### Q-LIM-80 · The malware scanner's reads are recorded under the repository its token names

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The credential is
`malware-scanner-clamav` since [ha-and-helm.md](ha-and-helm.md#q-ha-373--every-name-an-operator-or-an-auditor-reads-of-a-scanner-unit-carries-its-tool) Q-HA-373 (2026-11-21); the text below keeps the name of its day.

Context: a blob token reads a blob by its digest through one repository that holds it ([malware-scanning.md](malware-scanning.md) R-MAL-006);
the same bytes may be in many repositories, and the usage store records per repository.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The subject `system`, under the repository the token names, with the credential `malware-scanner`, checked against nothing (chosen) | As Q-LIM-66 for the image scanner | The overview shows what the scanning read and where; one repository per read, the one the job chose | A blob shared by many repositories is recorded under one of them |
| (b) Not recorded | — | Smaller store | The rejected option (c) of Q-LIM-66: the overview could not show where the cache's bytes went |
| (c) Under every repository that holds the blob | — | Fair | One read counted many times |

**Choice:** (a). **Applied in the spec:** R-LIM-001; [malware-scanning.md](malware-scanning.md) R-MAL-006.

### Q-LIM-95 · A lease is given up in both places it may be held, and a drop Valkey did not take is sent again until the lease's end

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. What it
serves is decided: no figure refuses a request because Valkey failed (R-LIM-019, Q-LIM-12; [ha-and-helm.md](ha-and-helm.md) R-HA-057
rule 1).

Context: while Valkey answers late or not at all, a replica's batches of the limits go to Valkey or to its share from one check to the
next. A batch that runs into `valkey.command_timeout` runs on the share (R-LIM-019); the circuit opens after
`valkey.circuit_breaker.failure_threshold` failed calls and closes after a successful probe (R-HA-057 rule 3), and a late answer counts
as a failed call. A download's lease was dropped only where its flush ran, so a lease taken on the share and dropped by a flush that
reached Valkey stayed on the share, and one taken in Valkey and dropped by a flush that ran on the share stayed in Valkey, each for
its `limits.parallel.lease`. A take that ran into the timeout is decided on the share, but a Valkey that held the command ran it
afterwards and holds that lease too. In the nightly of 2026-11-20 (`TestHAFaults_ValkeySlow`: every Valkey command held up to 2 s for
three minutes) one `packages` replica answered 176 tarball requests 429 `too_many_parallel_transfers` after the 10 s wait, within one
minute and on its share (`limit_mode` `fallback` in the 35 of its log that survived, ⌈50 ÷ 3⌉ = 17 places per token), while k6 sent
each token's tarballs to that replica at five a second and the other replicas served them in under a second: a few downloads in
progress per token against 17 places, the rest held by downloads that had ended. The k6 p99 of the slow minutes was 4.07 s; without that replica's
minute it was 554 ms, within R-HA-057's bound of the baseline plus twice the command timeout.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Give every lease up in both places: on the share at once, in Valkey by the drop of the flush or of the refusal; a drop that did not reach Valkey is sent again with every flush until one does or the lease has ended there (its last renewal plus `limits.parallel.lease`); renewals go to both (chosen) | The share counts the replica's own downloads in progress; Valkey loses an ended lease at the next flush that reaches it | Covers the take Valkey ran after the timeout, which nothing on the replica can know of; no round trip more (the drops ride the flush); bounded by the lease's end and `cluster.MaxFallbackBuckets` | A `ZREM` in Valkey for leases that were only on the share; a drop that reaches Valkey before the late take of the same lease leaves that take for its minute |
| (b) Remember where each take ran and give the lease up only there | | No `ZREM` wasted | The take that timed out and ran later stays in Valkey for its minute: the case of a slow Valkey |
| (c) Merge the share's leases into Valkey when it answers again | | One count | R-LIM-019 merges nothing (H-1005); more round trips; misses the late take as well |
| (d) Let the share count only leases younger than a few seconds | | No bookkeeping | A long download leaves the count; the number is arbitrary |

**Choice:** (a).

**Rationale:** a lease is a place of a download in progress; once the download ended it must leave every count it may be in, and the
replica cannot tell whether a timed-out take reached Valkey. Dropping in both places costs nothing on the request path. The development
agent would revisit this if the late take that outruns its own drop shows in a run (a token refused in `cluster` mode within a minute
of a slow Valkey, `quader_limits_decisions_total{figure="parallelTransfers",outcome="limited"}`).

**Applied in the spec:** R-LIM-018, R-LIM-019, R-LIM-024. Code: `dropOp`, `dropLease`, `keepUnsent` in
[engine.go](../../internal/limits/engine.go), `flushCounters` in [flush.go](../../internal/limits/flush.go). Tests: five cases of
`TestR_LIM_024_ParallelTransfers` in [engine_test.go](../../internal/limits/engine_test.go) on a store that is down or answers after
running the batch: a lease taken on the share and ended by a flush that reached Valkey, one taken in Valkey and ended by a flush on the
share, a take Valkey ran after the timeout, a lease given up at once as a waiting download does, and an unsent drop forgotten at the
lease's end.

### Q-LIM-96 · A certificate subject takes the kind default of the technical tokens, and the overview lists its kind

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the certificate subject of [formats/clamav.md](formats/clamav.md) R-CLAMAV-010 is a kind of its own (R-LIM-001), but a rule's
"who" names a user, a technical token, a CI project, an address, a group or every caller of a kind among `user`, `technicalToken`, `ci`,
`anonymous` and `administrator` (R-LIM-011); none of them is a certificate.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The kind default of the technical tokens (profile Technical), and `certificate` as a kind of the usage overview's lists, filters and subjects (chosen) | Step 3.3 of R-LIM-013 reads `every technicalToken` for it | A machine client, like a technical token or a CI job, under the same budget; nothing new to administer; the overview shows each installation by its certificate's subject | An administrator cannot give one certificate subject a rule of its own, nor every certificate a kind default of its own |
| (b) A "who" kind `certificate` (single and every) | Rules per certificate | Each installation limited on its own | A kind and a built-in rule for a client of one repository; the rule screens, the API and the migration of the rule table grow for it |
| (c) The users' kind default (Standard) | As a person | — | A ClamAV installation is no person; its budget would follow the people's |

**Choice:** (a). **Rationale:** a ClamAV installation fetches what a machine fetches; the technical tokens' profile is the one written for
that. The development agent would revisit (b) when an installation needs to limit one host apart from the others. **Applied in the
spec:** R-LIM-001. Code: `everyOf` in [model.go](../../internal/limits/model.go); the usage store keeps the serial
([buffer.go](../../internal/limits/usage/buffer.go)); `UsageSubjectCertificate` in [openapi.yaml](../../api/openapi.yaml);
[callers.ts](../../web/src/app/features/limits/callers.ts). The detail page of a caller lists the credentials of a user and, since
2026-11-23, a certificate subject's certificates by serial (`UsageCredentialCertificate`, R-UI-184).

### Q-LIM-97 · A pace figure keeps a download in Quader, in both modes

Status: **Superseded on 2026-11-22 by [Q-LIM-99](#q-lim-99--a-speed-limit-does-not-stop-a-link-the-budgets-are-charged-at-the-issue)** (a speed limit does not stop a link). It was decided by the planner
in the owner's absence on the owner's condition of 2026-11-22 ("für den Aufrufer gilt kein Tempolimit"), with what it meant for the
built-in profiles put to the owner; the text below is kept as it was decided.

Context: pacing (R-LIM-023) acts on the bytes Quader writes; a link writes none. Every built-in profile but Unlimited sets
`cachedBytesPerSecond` (R-LIM-010). In `audit` nothing is paced, and a would-limit is recorded once per download that would have waited.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A set `cachedBytesPerSecond` makes every download of the budget key a stream, in `enforce` and in `audit` (chosen) | The owner's condition, read per budget key | The limit the administrator set holds for every byte; `audit` shows what `enforce` would do | On a new installation only administrators and callers without the figure get links |
| (b) Only in `enforce` | | More links in `audit` | `audit` would record no pacing for downloads that `enforce` would pace |
| (c) The figure does not stop a link; a link's transfer is simply not paced | | Links for everyone | Against the owner's condition; a slowed caller becomes fast by link |

**Choice:** (a). **Rationale:** the owner's words, and the mode's promise that `audit` observes the decisions of `enforce`. That the
built-in profiles exclude almost every caller from links is the consequence the owner decides on; until then the settings page names it.
**Applied in the spec:** R-LIM-040, [storage.md](storage.md) R-STOR-070 step 10, [api-and-ui.md](api-and-ui.md) R-UI-200.

### Q-LIM-98 · A link is charged its size when it is issued, and its parallel lease ends with the redirect

Status: **Decided by the planner in the owner's absence** — selected from the options below; the owner reviews it.

Context: the owner accepted that "das Limit paralleler Downloads sieht den Transfer nicht" and that a link is counted when it is issued
(decision 11 (a), its stated costs). The byte windows are filled by the counting writer only ([httplimit.go](../../internal/limits/httplimit/httplimit.go)
lines 125 to 159), and a lease is given up when its request ends ([engine.go](../../internal/limits/engine.go) lines 894 to 902).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `Admit` before the link as before a stream; the item's size charged at issuance; the lease ends with the `307` (chosen) | A link is a download of its size, decided like one | The hour and day budgets hold for links; nothing waits for a transfer Quader cannot see | `parallelTransfers` bounds issuances, not transfers |
| (b) The lease lasts the link's life | A link holds a parallel slot until it expires | Some bound on parallel transfers | A one-second layer holds a slot for a minute: a pull of several layers at a low figure waits and fails where a stream would pass |
| (c) Nothing charged at issuance | | | The byte windows would never fill: a link would be a way around the hour and day budgets |

**Choice:** (a). **Applied in the spec:** R-LIM-040, [storage.md](storage.md) R-STOR-073.

### Q-LIM-99 · A speed limit does not stop a link; the budgets are charged at the issue

Status: **Decided by the development agent on 2026-11-22, after the owner left the open questions to it** ("ich gehe davon aus, dass du
selbstständig die richtige entscheidung triffst … Ändern tun wir es erst wenn mir etwas negativ auffällt"); it changes the condition of
his decision 11 ("für den Aufrufer gilt kein Tempolimit"), which is named to him. Supersedes Q-LIM-97.

Context: the condition of decision 11 kept every caller whose budget key sets `cachedBytesPerSecond` away from links (Q-LIM-97 (a)), and
every built-in profile but Unlimited sets it (R-LIM-010). Pacing (R-LIM-023) protects the replicas: it slows the bytes Quader writes, and a
link's transfer runs between the client and the object store. The hour and day budgets are charged at issuance either way (Q-LIM-98).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A set `cachedBytesPerSecond` makes every download of the budget key a stream, in `enforce` and in `audit` (Q-LIM-97's choice) | The owner's condition, read per budget key | The limit the administrator set holds for every byte | On a new installation only administrators get links: the installation's switch does next to nothing |
| (b) Only in `enforce` | | More links in `audit` | `audit` would record decisions `enforce` does not make |
| (c) The figure does not stop a link; the transfer by a link is not paced; the hour and day budgets are charged in full at issuance; in `enforce` and in `audit` alike (chosen) | No figure is a condition of a link | The switch reaches every caller the other conditions allow; pacing protects Quader's pods, which a link's transfer does not pass; the budgets still bound the volume, and `audit` shows what `enforce` does | A caller that is slowed through Quader is not slowed by a link ([security/download-links.md](../security/download-links.md) H-1352) |

**Choice:** (c). **Rationale:** with the condition, the owner's switch would reach almost no caller, and what the condition protects, the
replicas' bandwidth, a link does not use; the volume a caller may fetch stays bounded by its hour and day budgets. The development agent
would revisit it when a link's transfer turns out to hurt the object store's own limits (H-1283). **Applied in the spec:** R-LIM-040,
[storage.md](storage.md) R-STOR-014 and R-STOR-070 step 10, [api-and-ui.md](api-and-ui.md) R-UI-200. The handover to a Bastion takes the
same rule (storage.md Q-STOR-343). Code: no figure is read by the decision ([download_links.go](../../internal/formathost/download_links.go));
the charge is `limits.Charge.ChargeLink` ([links.go](../../internal/limits/links.go)).

### Q-LIM-110 · An upstream request step 9a refuses is counted nowhere, and a request stage 7 refuses is given back to the endpoint's class

Status: **That a refused request does not count is the owner's decision 19 (Q-LIM-57), carried on 2026-11-23 to step 9a and to the
endpoint's class of R-EP-015**, which it had not named; how it is built is **decided by the development agent** — selected from the
options below; the owner reviews it at the walkthrough.

Context: two places still counted a request before the step that refused it. Step 9a added the upstream request to the windows of the
budget key and of the ceiling (`INCR`) and decided after, so a caller that retried a refused miss kept its upstream window full, a
refusal by the ceiling filled the caller's own window and the other way round, and the usage store recorded upstream requests that
never reached the upstream. And the endpoint's class runs before stage 7 on its own: a request it admits and stage 7 refuses (a
request figure, the block, a class of R-API-016) stayed in the class's window, so a client that retried after a 429 of stage 7 filled
its endpoint budget too. Decision 19's reason holds for both: a waiting client must be admitted again.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Step 9a decides and counts in one take, the script of stage 7: the upstream request windows take the request, the uncached byte windows are only checked (they must have room and are not added to); an upstream request the parallel lease refuses after the take admitted it is given back by the same script. The endpoint's class keeps its own step and is given back the request stage 7 refuses (chosen) | Exact for 9a in its one round trip; one more round trip only for a request the endpoint's class admitted and stage 7 refused, or a download the parallel lease refused at 9a | The step that decides counts, as at stage 7; the endpoint's class still sees every request of the endpoint (a preflight, a path of no family, a role without the management API), which stage 7 never does | A refused request on an endpoint with a class costs a second round trip; between the class's take and its give-back another request of the same token may meet a fuller window, for the time of stage 7's round trip |
| (b) The endpoint's class joins the take of stage 7 as `package_write` does | One step for the class too | No give-back, no second round trip | The class must then also decide, on a path of its own, every request stage 7 does not reach (CORS answers a preflight before stage 7, an endpoint whose role serves no management API has no stage 7): two code paths that each must decide every request once; in `audit` the take adds nothing when a request window is full, so the class would let a caller over its figure through unbounded |
| (c) Refusals counted apart and subtracted at the flush | No new operation | No change to the order of the stages | A `Retry-After` up to a flush interval short, the variant the owner turned down for the request windows |

**Choice:** (a). The give-back (`OpGiveBack`) runs in the script of the take: it subtracts the units from each current counter that
still holds them, never below 0 and never in a counter that has expired, and skips a window that was only checked. On the replica's
share the take and the give-back run on its memory (`MemoryLimits.Return`); a class's token bucket is refunded. A request a later stage
refuses (the decision point, a 404, the remote engine's queue or circuit after 9a admitted the fetch) stays counted, as at stage 7: what
counts is decided by the step that counts it (H-1600 names the cost of that for a circuit that is open).

**Rationale:** decision 19 is a rule about the caller, not about one window, so it holds wherever a refusal counted. The take of 9a costs
nothing more than the transaction it replaces; the endpoint's class stays the one check every request of the endpoint meets, which is
its purpose (S-4), and pays its second round trip only on refusals. The development agent would revisit (b) if refusals on endpoints with
a class turn out to be the bulk of their requests.

**Applied in the spec:** R-LIM-017, R-LIM-022, Q-LIM-61; [endpoints.md](endpoints.md) R-EP-015; [architecture.md](architecture.md)
R-ARCH-034. Code: `Engine.MayFetch`, `Engine.giveBack` in [engine.go](../../internal/limits/engine.go); `OpGiveBack`, `WindowTake.Peek` in
[valkey_limits.go](../../internal/cluster/valkey_limits.go) and [valkey_take.go](../../internal/cluster/valkey_take.go);
`Limiter.GiveBack` in [limiter.go](../../internal/cluster/limiter.go); `giveBackEndpointClass` in
[endpoint_ratelimit.go](../../internal/api/v1/endpoint_ratelimit.go), called from [ratelimit.go](../../internal/api/v1/ratelimit.go).
Tests: `TestR_LIM_022_ARefusedUpstreamRequestIsCountedNowhere` in [engine_test.go](../../internal/limits/engine_test.go),
`TestR_EP_015_ARequestStage7RefusesIsGivenBackToTheEndpointsClass` in
[endpoint_ratelimit_test.go](../../internal/api/v1/endpoint_ratelimit_test.go), `TestLimiterGiveBack` in
[limiter_test.go](../../internal/cluster/limiter_test.go), and the script against the stack's Valkey in
`TestR_LIM_017_AGiveBackTakesBackWhatATakeCountedAndAPeekAddsNothing` ([units_test.go](../../test/integration/valkey/units_test.go)).

### Q-LIM-111 · A replica reports the epoch of the limits it decides with

Status: **Decided by the development agent on 2026-11-23** — selected from the options below; the owner reviews it at the walkthrough.

Context: a change of the limits reaches every replica through the change area `limits` and a reload of the index (R-ARCH-018,
R-LIM-013), and nothing told from outside when a replica decides with it. The clients' cases of the five formats and the OCI
handshake's case waited a fixed three seconds after a rule, the integration area `limits` 1.5 s after every change; a replica that
took longer counted the first document against the instance's figures where the case expected the rule's 429 (read on 2026-11-21,
not seen), and on a fast host the seconds were waited for nothing.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The index's load reads the area's epoch first, in the transaction of its snapshot, and the replica reports it as the gauge `quader_limits_index_epoch`; a test waits until every replica's gauge reaches the epoch the database holds after its change (chosen) | One statement more per load (the primary key of `cluster_epoch`), one gauge | Exact: a change and its epoch commit together, so a snapshot read after the epoch holds the change; a test waits as long as the replica takes and no longer | A gauge an operator does not need, though it shows a replica whose index stands still |
| (b) A probe request per replica with a token of the same kind, read from its `RateLimit-Policy` | No product change | — | Only request figures show in an admitted answer, the byte figures do not; an address rule cannot be probed without spending the address's budget |
| (c) The explanation of `GET /api/v1/limits/explain` | Exists | — | It compiles its own index from the database, not the replica's |

**Choice:** (a). **Rationale:** the change area already counts what a replica holds; only the index's load did not say which change
it compiled. The development agent would revisit it if the index ever loads from something else than one snapshot of the database.

**Applied in the spec:** R-LIM-030 (the metric). Code: `Store.Snapshot` in [store.go](../../internal/limits/store/store.go)
(`Snapshot.Epoch`), `limitsIndex.load` and `newLimitsIndexEpoch` in [wire_limits.go](../../internal/app/wire_limits.go); the waits
`awaitLimitsIndex` in [limits_clients_env_test.go](../../test/e2e/limits_clients_env_test.go) and `awaitIndex` in
[limits_test.go](../../test/integration/limits/limits_test.go).

### Q-LIM-112 · A replica counts the rounds of its charge buffer

Status: **Decided by the development agent on 2026-11-24** — selected from the options below; the owner reviews it at the walkthrough.

Context: the bytes of a response reach the hour's window at the next round of the replica's charge buffer (R-LIM-017,
`limits.flush_interval`, 1 s), and nothing told from outside when that round had ended. The clients' cases of the five formats
(`cachedBytesPerHour` 1 after a document) waited a fixed two seconds after the document: a runner on which a round took longer let the
archive through where the case expected the 429, and a fast one waited for nothing.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A counter `quader_limits_flushes_total` of the rounds that ended; a test reads it on every serving replica after the request and waits for two more rounds (chosen) | One counter, raised at the end of every round | The wait is the round's, whatever the runner's speed; the second round began after the first ended, so it began after the request's bytes were counted | A counter an operator rarely reads, though a replica whose counter stands still has a stuck buffer |
| (b) A gauge of the time the last round began | One gauge | One round less to wait | Compares the replica's clock with the test's |
| (c) Read the window in Valkey | No product change | Exact | The key is a hash of the budget key and the figure; a test would rebuild Quader's key scheme |

**Choice:** (a). **Rationale:** a count of what happened needs no clock and no knowledge of the keys; the cost is one counter. The
development agent would revisit it if a round were ever split across goroutines, which would make "ended" mean less than it says.

**Applied in the spec:** R-LIM-030 (the metric). Code: `Engine.Flush` in [flush.go](../../internal/limits/flush.go),
[metrics.go](../../internal/limits/metrics.go); the wait `awaitFlush` in
[limits_clients_env_test.go](../../test/e2e/limits_clients_env_test.go). Test: `TestR_LIM_030_EveryRoundOfTheChargeBufferThatEndedIsCounted`
([metrics_test.go](../../internal/limits/metrics_test.go)).

### Q-LIM-291 · A refusal names one wait, in the unit its text names it in

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. Found by the
builder of the limits' screens on 2026-11-23 and read in the code the same day: `refusal.err` rounded the wait up to whole seconds for
`Retry-After` and `retryAfterSeconds`, and `humanDuration` rounded the same wait up again for the detail, to whole minutes from two minutes
and to whole hours from two hours. A wait of 2 h 0 min 1 s was "Try again in 3 hours." beside `Retry-After: 7201`, one of 2 min 1 s
"3 minutes" beside `121`: two times for one refusal, the text's up to an hour later. A `rate_limited` refusal of `requestsPerHour` that
says "113 minutes" is not one of them: the sliding window's wait can reach two windows when the current one holds more than the limit
(`cluster.RoomAfter`), and its header said 6,780 s as well.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One value: the wait rounded up to whole seconds below two minutes and to whole minutes from there; the detail names it exactly ("2 hours 1 minute"), and `Retry-After`, `retryAfterSeconds` and the refusal's RateLimit policy carry it (chosen) | The text and the header are one time | A caller that waits what it reads is never early; a person reads minutes, not 7,201 seconds | A wait from two minutes is up to 59 s longer than the counters need |
| (b) The header keeps whole seconds; the text rounds down | The header exact | — | The text would tell a person to come back before the limit has room |
| (c) The text names seconds always | One unit | Exact | "Try again in 5,400 seconds" |

**Choice:** (a). A byte or upstream figure's wait stays at most its window: the windows are whole minutes (an hour, a day), so rounding up
to a minute does not pass them. **Applied in the spec:** R-LIM-020 (the table of codes, **One wait**). Code: `waitOf` and `humanDuration`
in [errors.go](../../internal/limits/errors.go). Test: `TestR_LIM_020_TheDetailAndRetryAfterNameOneWait`
([errors_test.go](../../internal/limits/errors_test.go)): every code that carries a wait, 21 waits from 0 to 47 hours, the detail read back
against `Retry-After`, `retryAfterSeconds` and the RateLimit policy (unit, microseconds).

### Q-LIM-292 · The burst of package_write holds the push of the largest image on four platforms

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. It changes the
default of Q-LIM-56, a number of the development agent's; the upload figures stay Later by the owner's decision (Q-LIM-27), and this is
not one of them.

Context: Q-LIM-56 set the burst to 400 so that it holds the push of the largest image docker builds (127 layers, 385 unsafe requests)
and computed, without measuring, that a push of two platforms with more than about 65 distinct layers each would meet it. Measured on
2026-11-25 on the integration stack with the registry's real routes, as one token of a person under the built-in profiles, in Enforce
(`TestMeasurePackageWriteUnderAMultiPlatformPush`, [usage-and-limits.md](../developer/usage-and-limits.md#a-multi-platform-push-against-package_write-measured)):
docker push's requests (a `HEAD`, then `POST`, `PATCH`, `PUT` per blob, five blobs at a time, a manifest per platform, the index) of two
platforms of 65 layers are 400 unsafe requests and pass; of 70 layers 430, and the 400th is answered 429 after 2.5 s with `Retry-After`
of 26 to 32 s; of 100 layers 610. A client that ends at the first 429, as docker did on the kind cluster on 2026-11-20 (Q-LIM-56), fails
the push; one that waits each `Retry-After` and sends again finishes in 35 s (70 layers) and 51 s (100 layers). buildx's requests (no
`PATCH`) of two platforms of 100 layers are 408 and meet it too. A platform of up to 127 layers is an image docker builds; several
platforms of one image are what `docker buildx build --platform` and a containerd image store push in one command.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Keep 400 | A second run of the push, or a client that honours `Retry-After`, finishes | No change | `docker push` of an image of two platforms of 66 to 127 layers, or four of 33 to 127, exits 1 on its first run; a pipeline that pushes such an image fails |
| (b) 800 at 10 a second | Two platforms of the largest image (772) | The case measured | Four platforms of more than 65 layers still fail |
| (c) 1,600 at 10 a second (chosen) | Four platforms of the largest image through `docker push` (1,542), eight platforms of up to 98 layers through buildx | One number, as Q-LIM-56; the sustained rate a publish loop gets stays 10 a second; still below the request burst of the profile Standard (4,000), so the figures bound a caller's requests first | A caller may send 1,600 writes at once on every format; the window is 160 s, so a caller that used the burst up waits up to that long |
| (d) The upload session's `PATCH` and `PUT` not counted, Q-LIM-56 (b) | A blob costs one unit | Counts a push as blobs | A rule of its own for one format's routes in the request path; not a default |
| (e) Rate and burst together, 40 a second and 1,600 | The window stays 40 s | A shorter wait after the burst | A publish loop gets 40 a second |

**Choice:** (c). **Rationale:** the measurement shows what Q-LIM-56 found for one platform: the refusal is the burst, a push is short and
dense, and docker does not retry it. A burst that holds the largest image on four platforms keeps the class's purpose (a publish loop runs
at 10 a second once its burst is spent) and removes the failure without a new concept. After the change the same measurement passed every push of two platforms of 100 and 127
layers and of four platforms of 127 layers (1,542 unsafe requests through docker push's, 10.7 s, no 429); five platforms of 127 layers met
the burst at its 1,600th unsafe request with `Retry-After` 140 s, and a client that honours it finished in 2 min 52 s. The development agent would revisit it
when the upload figures of Q-LIM-27 come, when a client is found that pushes more than four platforms of very many layers through
`docker push`, or when a caller is seen to spend the burst of 1,600 on writes that cost the installation more than a registry's blob
steps (a loop of publishes of large packages).

**Applied in the spec:** [api-and-ui.md](api-and-ui.md) R-API-016's configuration; Q-LIM-27, Q-LIM-56, Q-LIM-59. Code:
`APIRateLimitPackageWrite` in [m1.go](../../internal/config/m1.go). Test: `TestAPISection` "R-API-016 the default burst of package_write
holds a push of the largest image on four platforms" ([api_test.go](../../internal/config/api_test.go), unit).

### Q-LIM-293 · The limit counts transfers and is named parallel transfers

Status: **Decided by the owner on 2026-11-26, 10:27** (decision 23): "Option A, default darf aber großzügiger sein als 2, wir bauen hier
enterprise software für große firmen". The names are the development agent's.

Context: the owner's rule of the same morning (W-31): a download is the logical unit a person asked for, an image with all its parts,
never a single file. The figure `parallelDownloads` counted every transfer to a client in progress (R-LIM-024): a `docker pull` holds
three leases at once, an npm install fifteen, a `dotnet restore` one per package. A profile's line "20 parallel" therefore read as twenty
images at once, which it never was.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Count transfers, and name the figure so (the owner's choice) | The mechanism stays; the API, the code, the screens and the manual say *transfers* and what one pull or install holds | What the server can count; it bounds connections, which is what the limit protects | A person has to translate a pull into transfers; the sentence where the figure is set does that for them |
| (b) Count units: a pull, an install | A lease per image or per install | Reads as the owner's unit | The server sees no unit: a pull's blobs arrive over several connections without a marker of their image, an install has no start and no end on the wire; a guess per format |
| (c) Both, two figures | | Each reader finds their unit | Two numbers to set where one bounds the connections |

**Choice:** (a), with these names, used everywhere without an alias (pre-alpha, rule 9): the API member, the overrides' member and the
figure of a refusal `parallelTransfers`; the problem code `too_many_parallel_transfers`; the column `parallel_transfers`
(migration 00141, which renames the overrides' member and the recorded limit events too); the metric label
`quader_limits_decisions_total{figure="parallelTransfers"}` and the request log's `limiter: limits.parallelTransfers` (both carry the
figure's name); on the screens **Parallel transfers** (German "Gleichzeitige Übertragungen"), the editor's group **Transfers**
("Übertragungen"), the field **In parallel**, a limit "250 transfers in parallel", a compact line "250 parallel transfers", an override
"+ parallel transfers 100".

Left as they are, each with its reason: the settings `limits.parallel.lease` and `limits.parallel.wait` (they name the mechanism's
lease and wait, not the figure; their descriptions now say *transfer*); the RateLimit policy `"parallel"` with
`qu="concurrent-requests"` (a policy name of the draft's fields, which counts concurrent requests and is right); the word *download*
in this page's mechanics (the check before a download starts, a download's lease and pacing: one transfer, R-LIM-007); the byte
figures' "download limit" and the code `download_limit_reached`, which mean the bytes a person downloads, whatever the unit.

**Rationale:** the owner chose to keep counting what the server can count. A name that says what is counted, and a sentence that says
what one pull or install holds, keep a person from reading it as images at once; an alias of the old name would keep the misreading
alive.

**Applied in the spec:** R-LIM-004, R-LIM-007, R-LIM-020, R-LIM-024, R-LIM-041; [api-and-ui.md](api-and-ui.md) R-UI-178, R-UI-184,
R-UI-186. Code: `ParallelTransfers` in [figures.go](../../internal/limits/figures.go), `CodeTooManyParallelTransfers` in
[errors.go](../../internal/limits/errors.go), [figures.ts](../../web/src/app/features/limits/figures.ts),
[problem-messages-limits.ts](../../web/src/app/core/http/problem-messages-limits.ts).

### Q-LIM-294 · The built-in parallel transfers are set from a measurement of the clients

Status: **Decided by the owner on 2026-11-26 that the built-in values are generous, sized for large companies** (decision 23). The
numbers are the development agent's, from the measurement below — selected from the options below; the owner reviews them at the
walkthrough.

Context: Q-LIM-59 set the request figures from a measurement and left the parallel figure at the first migration's estimates
(Standard 20, Technical 50, Restricted 6, Anonymous 4). They contradicted R-LIM-010's own promises: twenty CI jobs of one token running
npm (15 transfers each) need 300, and a single `dotnet restore` of 200 packages opens 200 at once.

**The measurement**, run once on 2026-11-26 on a development machine (`linux/arm64` containers, 20 cores) with the client images of
the e2e harness (Docker 29.9.3 with its own daemon, Podman 5.8.9, the npm clients of R-NPM-045, the .NET SDK 10.0.403, APT 3.0.4)
against a stub server that held each transfer 0.5 s before its body, as a slow network does, and counted the transfers in progress at
once; not a test of the suite. The table of R-LIM-041 lists every client; the stub served an image of 40 layers, five of 10 layers, 200
npm packages, 200 NuGet packages and 50 Debian packages. A client that the harness has no image of (ansible-galaxy's install, helm, a
Kubernetes node) is taken from the client.

| Option | Anonymous, Standard, Technical, Restricted | For | Against |
|---|---|---|---|
| (a) Each profile holds the use R-LIM-010 promises it, with room (chosen) | 64, 250, 1,000, 64 | Anonymous: four npm installs or ten podman pulls at once from one address (a team behind one NAT before it signs in); Standard: a developer's restore of 200 packages beside a pull, or a pnpm install, five podman pulls and an npm install together; Technical: twenty CI jobs of one token running pnpm 10 or 11 (1,000) or npm (300, more than three times over), five restores of 200 packages; Restricted: one install of any JavaScript client whole | One token may hold 1,000 transfers at once: a quarter of a replica's default request slots (below) |
| (b) Smaller: 16, 100, 500, 16 | | A token holds an eighth of a replica's slots | A pnpm install and a NuGet restore wait on every machine; twenty pnpm jobs of one token wait |
| (c) No figure for signed-in callers | | Nothing waits | No bound on the connections of a token that loops |
| (d) Larger: Technical 4,096 | | No CI fleet ever waits | One token could hold every request slot of a replica |

**Choice:** (a): Anonymous 64, Standard 250, Technical 1,000, Restricted 64. Technical holds the twenty CI jobs of R-LIM-010 for every
measured client with a bound (npm 300, pnpm 10 and 11 1,000, Yarn 4 640, podman 120; pnpm 12 on runners of 20 cores 1,280, of which 280
wait a moment), 66 jobs running npm, or 333 hosts pulling with docker. A client without a bound (`dotnet restore`, `ctr`) opens one
transfer per package or layer; what is beyond the figure waits up to 10 s for a place, which a cached package frees in milliseconds
(H-2261).

**What a replica keeps** (the limit protects a replica's connections and bandwidth): a transfer holds one of the endpoint's request
slots (`server.max_in_flight`, 4,096 by default) for as long as it runs, and the transfers that wait hold at most a quarter of them
([Q-LIM-60](#q-lim-60--waiting-downloads-hold-at-most-a-quarter-of-an-endpoints-request-slots)). The largest built-in figure, 1,000, is
less than a quarter of the default endpoint's slots: one token whose transfers all land on one replica holds at most 1,000 running and,
with every other caller's waiting transfers, 1,024 waiting, so at least half of the replica's slots serve others; across n replicas
behind the ingress its transfers spread to about 1,000 ÷ n each. A further endpoint's 1,024 slots are not held that way (H-2260). The
bandwidth stays the byte figures': `cachedBytesPerSecond` is the subject's, whatever its number of transfers, and the floor of 64 KiB/s
per transfer (R-LIM-023) passes no built-in profile's rate (Technical: 1,250 MB/s since [Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises) over 1,000 transfers is
1.25 MB/s each). Valkey holds a
lease per transfer in the key's sorted set; 1,000 members are one `ZCARD` as 50 were.

**Not changed here: the other estimated figures**, which a pull of an image or a large install meets in normal enterprise use; the
development agent set them the same day from what the uses cost ([Q-LIM-296](#q-lim-296--the-built-in-byte-upstream-and-refused-figures-hold-the-uses-each-profile-promises)):

| Profile | Figure | What reaches it |
|---|---|---|
| Anonymous | `upstreamRequestsPerHour` 20 | the first pull of one image of more than about 17 layers through a remote (a pull is a request per layer, the manifests and the config); an npm install of more than 10 packages that are not cached (a document and a tarball each) |
| Anonymous | `uncachedBytesPerHour` 2 GB | the first pull of one large image through a remote (a CUDA or a database image is several GB) |
| Anonymous | `cachedBytesPerHour` 10 GB, `bytesPerDay` 30 GB | a team behind one NAT pulling images of 1 GB: ten pulls an hour, thirty a day |
| Anonymous | `cachedBytesPerSecond` 20 MB/s, `uncachedBytesPerSecond` 5 MB/s | every pull of a large image: 1 GB takes 50 s from the cache, 200 s from the upstream |
| Standard | `upstreamRequestsPerHour` 2,000 | two first installs of 475 packages an hour through a remote (950 upstream requests each: a document and a tarball per package) |
| Standard | `uncachedBytesPerHour` 30 GB | a developer's first pulls of a few large images through a remote in an hour |
| Standard | `uncachedBytesPerSecond` 50 MB/s | every first pull through a remote of a large image: 1 GB takes 20 s |
| Technical | `upstreamRequestsPerHour` 10,000 | twenty parallel CI jobs of one token installing 475 packages each on a cold cache (9,500) |
| Technical | `uncachedBytesPerHour` 100 GB | a CI fleet's first pulls of new images through a remote after a release of its base images |
| Restricted | `upstreamRequestsPerHour` 200 | one first install of more than 100 packages through a remote |
| Restricted | `uncachedBytesPerHour` 5 GB | one first pull of a large image through a remote |

**Rationale:** the owner asked for generous values; the clients' own parallelism is what a figure meets, and with it measured the
figures can hold the use the page promises instead of a guess. The development agent would revisit them when a client is found whose
normal command opens more transfers than its profile holds and waits for it (`quader_limits_decisions_total{figure="parallelTransfers",
outcome="limited"}` without overload), when the request slots of an endpoint are lowered below four times the Technical figure, or when
a CI fleet of one token is seen to hold more of a replica than the replica's other callers can spare.

**Applied in the spec:** R-LIM-010, R-LIM-041, Q-LIM-14's context. Code: migration
[00141_limits_parallel_transfers.sql](../../internal/meta/postgres/migrations/00141_limits_parallel_transfers.sql), `BuiltinProfiles` in
[model.go](../../internal/limits/model.go).

### Q-LIM-295 · The migration replaces a built-in parallel figure only where it still holds the first migration's value

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the new values reach an existing installation through a migration. Quader is pre-alpha (rule 9), but the owner's own
installation follows every release, and an administrator may have changed a built-in profile's figure (R-LIM-010).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Set the new value where the figure still holds the first migration's (20, 50, 6, 4) (chosen) | An administrator's value, a number or unlimited, stays | An administrator's choice stands; an untouched installation gets the measured values | An administrator who had set exactly the old value is taken for untouched |
| (b) Set every built-in profile's figure | | One state everywhere | Overwrites a choice an administrator made on purpose |
| (c) Leave existing installations as they are | | Nothing changes under an administrator | Every installation keeps the figures the owner found too small |

**Choice:** (a). The step also renames the column, the member of every rule's and project maximum's overrides (an unlimited override
stays `null`) and the figure of the recorded limit events, so that the usage overview and the rule tables read the new name for the past
too; audit events keep the words they were written with. **Rationale:** a value that differs from the old built-in one is an
administrator's decision, and the owner's rule is that a decision of his stands until he reverses it; the old value itself is the
estimate the owner asked to replace.

**Applied in the spec:** R-LIM-010. Code: migration
[00141_limits_parallel_transfers.sql](../../internal/meta/postgres/migrations/00141_limits_parallel_transfers.sql). Test:
`TestR_HA_020_TheParallelTransfersStepKeepsAnAdministratorsValue` in
[parallel_transfers_test.go](../../test/integration/postgres/parallel_transfers_test.go) (integration area `postgres`, about 3 s).

### Q-LIM-296 · The built-in byte, upstream and refused figures hold the uses each profile promises

Status: **Decided by the development agent** on the owner's words of 2026-11-26, 10:27 ("… default darf aber großzügiger sein …, wir
bauen hier enterprise software für große firmen", decision 23), which he said of the parallel figure and the development agent applied
to every built-in figure that bounds normal use — selected from the options below; the owner reviews it at the walkthrough.

Context: the request figures (Q-LIM-59) and the parallel transfers (Q-LIM-294) were set from measurements; the byte figures,
`upstreamRequestsPerHour` and `refusedPerHour` were the first migration's estimates, and normal use in a company met them (the table
*Not changed here* of Q-LIM-294): an anonymous first pull of an image of more than about 17 layers through a remote answered 429, which
docker does not retry, and two first installs of 475 packages an hour exhausted Standard. R-LIM-042 prices each format's normal act.

| Option | Anonymous, Restricted, Standard, Technical | For | Against |
|---|---|---|---|
| (a) Each profile holds the uses R-LIM-010 promises it, priced by R-LIM-042, with a third or more of room; the profiles keep their order per figure (chosen) | cached 100, 100, 250, 1,250 MB/s and 30, 50, 300, 3,000 GB/h; uncached 50, 50, 125, 250 MB/s and 20, 20, 100, 500 GB/h; upstream requests 3,000, 3,000, 20,000, 100,000 an hour; 200, 200, 1,000, 30,000 GB a day; refused 300, 300, 1,000, 5,000 an hour | Every promised use passes; each figure is a number a reader can check against a use; a caller never gets more by not signing in | A single anonymous address may make Quader send 3,000 requests an hour to a public registry (Q-LIM-298, H-2263) |
| (b) The byte and upstream figures of signed-in callers unlimited; only Anonymous bounded | | Nothing a person or a system does meets them | No bound on a token that loops or a person's token in a runaway script; the installation's bandwidth and a registry's quota unguarded per caller |
| (c) Ten times every old value | | Simple | Tied to no use: Anonymous' 200 upstream requests still fail a first install of 475 packages, Technical's 50 TB a day guards nothing |
| (d) Anonymous kept small, the others raised | | An installation open to the internet stays a small proxy for strangers | A company's people behind one address meet it at once (decision 23); an installation that serves the internet narrows Anonymous with one rule (the manual says how) |

**Choice:** (a). Standard keeps 300 GB of cached bytes an hour and 1,000 GB a day and Restricted 50 GB an hour: those held their uses
already. No figure is unlimited by default: each one bounds a caller that loops, a misconfigured job or a script, which is what a
built-in figure is for; Unlimited stays the administrators' profile (decision 9).

**What each figure protects, and what it still bounds:**

| Figure | Protects | One caller at the figure, an hour (Anonymous, Restricted, Standard, Technical) | A thousand anonymous addresses, an hour | A ceiling for all anonymous callers together |
|---|---|---|---|---|
| `cachedBytesPerSecond` | a replica's bandwidth for its other callers (R-LIM-023); a link is not paced (R-LIM-040) | 360, 360, 900, 4,500 GB at most | 100 GB/s, beyond any replica: the replicas' links and `max_in_flight` bound them | none; it would be a bandwidth limit of the installation, not of a caller |
| `cachedBytesPerHour`, `bytesPerDay` | the installation's egress and the object store's reads per caller | 30, 50, 300, 3,000 GB; a day 200, 200, 1,000, 30,000 GB | 30 TB an hour, 200 TB a day | none exists, and none is built here (H-1007) |
| `uncachedBytesPerSecond` | the installation's link to the internet; every follower of a fill waits at the leader's pace (R-LIM-023) | 180, 180, 450, 900 GB at most | 50 GB/s, beyond any link | none |
| `uncachedBytesPerHour` | the link to the upstream and the object store's growth: what is fetched is kept | 20, 20, 100, 500 GB | 20 TB, unless the remote's anonymous ceiling holds them | it belongs here: the remote's ceiling for all anonymous callers (R-LIM-015, decision 7), set per remote by an administrator, none by default (the owner's Q-LIM-16) |
| `upstreamRequestsPerHour` | the upstream's rate limit and the company's standing with a public registry | 3,000, 3,000, 20,000, 100,000 | 3 million, unless the ceiling holds them | as `uncachedBytesPerHour` |
| `refusedPerHour` | the brake on a caller that probes names or tokens (R-LIM-025), and how many refusals the audit records | 300, 300, 1,000, 5,000 refusals, then a block of the block time | 300,000; failed credentials are throttled first per address (R-AUTH-038) | none: a block holds one budget key |

**Rationale:** the owner's rule is that normal use of a large company never meets a built-in figure; the uses are the ones R-LIM-010
promised already, and with their cost measured or documented per format each figure can hold them with room instead of a guess, while
the order of the profiles and a finite value keep every figure a bound. The development agent would revisit a figure when
`quader_limits_decisions_total{outcome="limited"}` shows normal use meeting it, when a format's act costs more than R-LIM-042 says
(H-2264), or when an installation's egress costs make a day's figure too generous for one caller.

**Applied in the spec:** R-LIM-010, R-LIM-042, the introduction to the decisions; Q-LIM-59 and Q-LIM-294 point here; H-2262 is closed by
this decision. Code: `BuiltinProfiles` in [model.go](../../internal/limits/model.go), migration
[00143_limits_builtin_byte_figures.sql](../../internal/meta/postgres/migrations/00143_limits_builtin_byte_figures.sql). Tests:
`TestR_LIM_042_TheBuiltInProfilesHoldTheUsesThePagePromises` and `TestR_LIM_042_TheBuiltInProfilesKeepTheirOrder` in
[builtin_uses_test.go](../../internal/limits/builtin_uses_test.go) (unit, milliseconds, no data).

### Q-LIM-297 · The migration replaces a built-in byte, upstream or refused figure only where it still holds the first migration's value

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: as for the parallel figure (Q-LIM-295): the owner's installation follows every release, and an administrator may have changed
a built-in profile's figure (R-LIM-010).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Per figure: the new value where the figure still holds the first migration's (chosen) | An administrator's value, a number or unlimited, stays; the profile's other figures move; the profile's version moves once | An administrator's choice stands figure by figure | An administrator who had set exactly the old value is taken for untouched |
| (b) Per profile: only a profile none of whose seven figures was changed | | A profile an administrator tuned stays as a whole | One changed figure keeps six estimates the owner asked to replace |
| (c) Every built-in figure | | One state everywhere | Overwrites an administrator's choice |

**Choice:** (a), as Q-LIM-295. **Rationale:** a value that differs from the first migration's is an administrator's decision, and the
old value itself is the estimate being replaced.

**Applied in the spec:** R-LIM-010, R-LIM-042. Code: migration
[00143_limits_builtin_byte_figures.sql](../../internal/meta/postgres/migrations/00143_limits_builtin_byte_figures.sql). Test:
`TestR_HA_020_TheBuiltInFiguresStepKeepsAnAdministratorsValue` in
[builtin_figures_test.go](../../test/integration/postgres/builtin_figures_test.go) (integration area `postgres`, about 3 s, five rows).

### Q-LIM-298 · The built-in figures grant nothing a public registry refuses, and a remote's ceilings stay the administrator's

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a profile's upstream figures bound what one caller makes Quader ask of an upstream; the upstream counts on its own. Docker Hub
(its page *Docker Hub usage and limits*, read 2026-11-26): 100 pulls per six hours for an unauthenticated address (an IPv4 address or an
IPv6 `/64`), 200 for a Personal account, unlimited under fair use for Pro, Team and Business; a pull is a `GET` of a manifest, one per
architecture of a multi-platform image, and a `HEAD` is none; at the limit it answers 429 "You have reached your pull rate limit". A first
pull through a remote is therefore one Docker Hub pull per platform; a revalidation of a tag is a `HEAD` and costs none. An anonymous
caller at the new 3,000 upstream requests an hour can cause about 60 first pulls of distinct images of 40 layers, more than half of what an
unauthenticated remote's address may pull in six hours; at the old 20 it could cause none whole. When the upstream refuses, R-REPO-029
holds for every caller of the remote: the circuit opens for the upstream's `Retry-After` (at most the remote's `circuitOpenSeconds`), what
is cached is served, a stale document within `maxStaleSeconds`, and anything else answers 503 `upstream_rate_limited` with `Retry-After`,
not a limit of this page; docker does not retry it. The other public registries publish no read limit the development agent found for the
protocols Quader speaks to them (npm's registry, nuget.org's v3 API, Galaxy, Debian's mirrors); nuget.org documents limits for its v1 and
v2 search and its writes only.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The profiles stay generous; a remote's ceilings stay unset by default (Q-LIM-16), and the manual says how a company keeps a public registry's limit: credentials of a paid account on the remote, or the remote's ceilings set at the account's limit (chosen) | | The owner's Q-LIM-16 stands; the figures keep one meaning, the caller's share of the installation | A remote of Docker Hub without credentials can be spent by a few anonymous addresses (H-2263) |
| (b) A default ceiling for all anonymous callers on every remote | e.g. 100 upstream requests an hour | The upstream's quota is guarded on a new installation | Reopens the owner's Q-LIM-16; a company's people behind one address meet it on their first pulls |
| (c) A ceiling per well-known upstream, at its published limit | Docker Hub's 100 pulls per six hours | Matches the upstream | The figures count exchanges, the upstream pulls (Gap 2); a ceiling in exchanges cannot say 100 pulls |

**Choice:** (a). **Rationale:** no figure of a profile can grant what the upstream refuses, and a figure sized to an anonymous upstream's
quota (about 16 pulls an hour for a whole installation) would fail every company; the installation's standing with a registry is the
remote's matter, where the credentials and the ceilings are. The development agent would revisit this with the owner's Gap 2, if a
figure that counts the upstream's pulls is decided.

**Applied in the spec:** R-LIM-010; the manual's [built-in profiles](../manual/admin/download-limits.md#the-built-in-profiles).

### Q-LIM-300 · A preset per Bastion, rules on demand, two profiles of the Bastion

Status: **Decided by the owner on 2026-11-26** — option (a) of his working paper (k5), stored at 11:51 (decision 25).

Context: simple first, details on demand (CLAUDE.md); a Bastion faces the internet, the main installation's callers sit in the company.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A preset per Bastion, rules on demand (chosen) | Internet, Partner network or Internal fills everything; "Details" opens this Bastion's rule table as at every other place: a single user, a group, a technical token, an address; two new profiles "Bastion: anonymous" and "Bastion: signed-in" | One click in the normal case; a partner's token can get a rule of its own | The Bastion's rule table travels with the handshake |
| (b) Two profiles per Bastion | Anonymous and signed-in, with overrides | Less to build | No rule for a single partner |
| (c) A factor on the main installation's figures | "×0.1" | One number | Unlimited times a factor stays unlimited |

**Choice:** (a), the owner's decision. The paper's reasoning, which he took with it: under the preset Internet an administrator's token gets
"Bastion: signed-in", not Unlimited, since a leaked administrator token on the internet is the worst case; Partner network and Internal give
administrators Unlimited. **Applied in the spec:** R-LIM-011, R-LIM-013, R-LIM-043, R-LIM-046, [bastion.md](bastion.md) R-BAS-093, R-BAS-096,
[api-and-ui.md](api-and-ui.md) R-UI-240, R-UI-241.

### Q-LIM-301 · Audit or Enforce per Bastion

Status: **Decided by the owner on 2026-11-26** — option (a) of his working paper (k7), stored at 11:51 (decision 27).

Context: his sentence of 10:40, "Intern könnten die Limits deaktiviert sein und auf dem Bastion könnten sie deutlich eingeschränkt sein";
the instance has one switch Audit/Enforce (decision 17).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A switch Audit/Enforce per Bastion (chosen) | The instance in Audit (everything counted, nothing limited), Bastion B in Enforce with the preset Internet | His sentence in two clicks; internally it stays visible who would have been limited | Adds a switch per Bastion to his decision "one switch for the instance" |
| (b) Rules only | The instance's rules on Unlimited | Nothing new | "Off" is then a rule, not a switch |
| (c) Presets also for the main installation's own endpoints | | Symmetric | Larger than the task |

**Choice:** (a), the owner's decision; it adds to decision 17. **Applied in the spec:** R-LIM-026, R-LIM-044, [bastion.md](bastion.md)
R-BAS-093.

### Q-LIM-302 · The main installation resolves a signed-in caller's rule, the Bastion the address and kind rules

Status: **Decided by the development agent on 2026-11-26** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-013 resolves by the single caller, then the caller's groups, then the kind. The Bastion holds no group and no principal
(R-BAS-001); the main installation answers every signed-in caller's check and marks every forward.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The main installation names the applying rule's ID in the check answer and the marking; the Bastion resolves the address and kind rules from the place's table (chosen) | One member and one header; the table in the handshake | The groups never leave the main installation; an anonymous caller costs no request | A changed membership reaches the Bastion's charge with the caller's next answer, within the answer period (H-2284) |
| (b) The handshake carries every rule with its "who", the groups' members included | The Bastion resolves everything | No member in the check | Membership lists of the main installation in a DMZ |
| (c) The Bastion asks per request | | Always current | A request to the main installation per hit, against Q-BAS-326 |

**Choice:** (a). **Applied in the spec:** R-LIM-043, [bastion.md](bastion.md) R-BAS-096, the wire of interface version 8.

### Q-LIM-303 · The defaults of a Bastion come from measurements

Status: **Decided by the owner on 2026-11-26** — option (a) of his working paper (k11), stored at 11:51 (decision 28).

Context: his rule of the same morning (decision 23): enterprise software, generous defaults, and at the internet clearly restricted.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) From measurements of real clients (chosen) | Flood protection per address: anonymous 50/s with a burst of 1,000, signed-in 200/s with a burst of 4,000 (a normal command passes); the Bastion profiles from the measured bytes and misses of one normal command per format; Partner network: the main installation's profiles; Internal: unlimited, counted; throughput unlimited until he sets a figure; a new Bastion starts with Internet and Enforce | As the main installation's request figures; nothing guessed | A measurement first |
| (b) Reuse the existing profiles | Anonymous and Restricted, unchanged | Nothing to measure | Twenty misses an hour (then) refused a cold `docker pull` already |
| (c) Partner network as the default | | Generous | Not "clearly restricted" at the internet |

**Choice:** (a), the owner's decision. The anonymous ceiling holds N cold anonymous commands at once, N from the same measurement.
**Applied in the spec:** R-LIM-046, [bastion.md](bastion.md) R-BAS-092 (its flood figures, unchanged), R-BAS-093, R-BAS-094.

### Q-LIM-304 · A Bastion is a place of the rules, beside the instance

Status: **Decided by the development agent on 2026-11-26** — selected from the options below; the owner reviews it at the walkthrough.

Context: the owner's Q-LIM-300 wants "this Bastion's rule table as at every other place", and Q-LIM-306 two budgets.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A fourth place kind `bastion`, one per registration, resolved alone (chosen) | `limit_assignment.place_kind = 'bastion'` with `bastion_id` | The rule table, the API and the screens of the other places serve it; the budget key names it; nothing of the instance leaks into it by fall-through | Resolution at a Bastion has no project or repository step |
| (b) A condition "through Bastion B" on rules of the main installation's places | | One rule table | Every rule gains a dimension; the Bastion would need the whole table |
| (c) The Bastion as an endpoint | | Endpoints exist | An endpoint is a listener of the main installation, and owner's D7 gives further endpoints no place |

**Choice:** (a). A rule for a repository through a Bastion is not offered: a Bastion's budget is the caller's at that door. **Applied in the
spec:** R-LIM-011, R-LIM-013, R-LIM-043.

### Q-LIM-305 · A Bastion protects per address and budgets per subject, on everything it serves

Status: **Decided by the owner on 2026-11-26** — option (c) of his working paper (k4), stored at 11:51 (decision 24).

Context: a Bastion counted requests and misses per address only; no bytes, no parallel transfers, and a hit was counted nowhere.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Per address only, plus bytes | GB per hour, MB/s and parallel per address | Small | Partners behind one NAT share a budget; a token from many addresses is unlimited |
| (b) Per caller only | Everything hangs on the token | One key | Loses the protection that works before a credential is checked |
| (c) Both (chosen) | Flood protection per address as today; the download budgets per caller on everything the Bastion delivers, hits included, in the same eleven figures as at the main installation; an anonymous caller is its address | One model in the whole product; stops mass download from the cache and bounds the cache attack per caller | The Bastion must learn from the main installation who the caller is |

**Choice:** (c), the owner's decision. **Applied in the spec:** R-LIM-001, R-LIM-018, R-LIM-043, [bastion.md](bastion.md) R-BAS-035, R-BAS-059,
R-BAS-096.

### Q-LIM-306 · Two budgets: one at the Bastion, one at the main installation

Status: **Decided by the owner on 2026-11-26** — option (a) of his working paper (k6), stored at 11:51 (decision 26).

Context: does what a caller fetches through a Bastion count against the same budget as internally?

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Two (chosen) | What is fetched through Bastion B counts at B; the main installation charges a forward to the caller as today; the overview shows both and the sum | His pattern of the repository rules (consumption counts where the rule holds, decision 15); no question back per hit | A forward counts in both budgets |
| (b) One | The Bastion reads the main installation's counters per hit | Exact | A question back per hit makes the Bastion pointless |
| (c) Two, but the main installation exempts forwards | Only the Bastion's budget holds | No double refusal | A faulty or taken-over Bastion evades the main installation's figures |

**Choice:** (a), the owner's decision. **Applied in the spec:** R-LIM-014's pattern, R-LIM-027, R-LIM-043, R-LIM-045, [bastion.md](bastion.md)
R-BAS-059, R-BAS-096.

### Q-LIM-307 · Pages of the web interface are a request class of their own per address

Status: **Decided by the development agent on 2026-11-26** — selected from the options below; the owner reviews it at the walkthrough. It
was listed for his objection on the working paper ("Seitenaufrufe der GUI bekommen eine eigene Anfrage-Klasse je Adresse … und zählen nie
als Miss").

Context: W-35 adds pages at the Bastion; a first page view takes 8 to 12 requests (that plan's estimate), behind a corporate NAT many people
share an address, and a page never reaches the main installation.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A class `gui` of the flood protection per address, beside `anonymous` and `authenticated`, never a miss; its bytes counted as documents of the anonymous subject (chosen) | 20 requests a second with a burst of 100 per address until W-35 measures a page | A crawler and the package API do not share a budget; a page view never takes from the ceiling, the slots or the throughput | One more figure pair |
| (b) The anonymous class | | Nothing new | A burst of package clients and a reader of pages behind one NAT take from one budget |

**Choice:** (a). **Applied in the spec:** R-LIM-046, [bastion.md](bastion.md) R-BAS-035, R-BAS-093, R-BAS-096. *Built at the Bastion on
2026-11-26 (W34-E9a):* `guard.Page` with the limiter `bastion.requests_gui` and the configuration's `limits.flood.gui`; no route asks it until
W-35's pages exist.

### Q-LIM-308 · A Bastion's usage report is bounded per batch and folds the rest into an overflow row

Status: **Decided by the development agent on 2026-11-26** — selected from the options below; the owner reviews it at the walkthrough.

Context: an anonymous flood from many addresses makes as many subjects; the main installation's own buffer holds at most 100,000 keys and
folds the rest (R-LIM-028).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The same bound in the Bastion's buffer: 100,000 keys per batch window, the rest in an overflow row per repository and subject kind, with the number of subjects it folded (chosen) | One rule at both doors | Memory and the batch stay bounded whatever the flood | The overview cannot name the callers of the overflow |
| (b) No bound | | Every subject named | A flood of addresses grows the Bastion's memory and the main installation's store without bound |

**Choice:** (a). **Applied in the spec:** R-LIM-045, [bastion.md](bastion.md) the wire of interface version 8. *Built on 2026-11-26 by
E9b:* [internal/bastion/usage](../../internal/bastion/usage/usage.go).

### Q-LIM-309 · In Audit a Bastion still holds its flood protection, ceiling, slots and throughput

Status: **Decided by the development agent on 2026-11-26** — selected from the options below; the owner reviews it at the walkthrough.

Context: in Audit at the main installation nothing on a protocol path is refused (Q-LIM-4), which the owner accepted for an installation
inside the company. A Bastion faces the internet and bears its attacks alone (his words of 10:55).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Audit at a Bastion concerns the budgets of its place; the flood protection per address, the anonymous ceiling, the slots and the throughput hold in both modes (chosen) | | A Bastion in Audit is still protected against a flood and still protects the main installation | Audit is not "nothing refused" at a Bastion, as it is at the instance |
| (b) Audit at a Bastion refuses nothing at all | Q-LIM-4's rule | One meaning of Audit | A Bastion in Audit is open to any flood and passes it on to the main installation |

**Choice:** (a). **Applied in the spec:** R-LIM-044, [bastion.md](bastion.md) R-BAS-037, R-BAS-096. *Built on 2026-11-26 by E9b:*
the budgets decide by `settings.Applied.Enforce`; the guard, the ceiling, the slots and the throughput do not read it.

### Q-LIM-310 · The Bastion profiles take the measurement's proposal, with room for one anonymous restore of 200 packages at once

Status: **Decided by the development agent on 2026-11-26** (package E8) — selected from the options below; the owner reviews it at the
walkthrough.

Context: the measurement E4 proposed every figure of the two profiles (developer/bastion.md). Its own tables show one anonymous `dotnet
restore` of 200 packages holding 200 transfers at once through a Bastion, which the proposed 80 parallel transfers of "Bastion: anonymous"
make wait (H-2261), and NuGet retries without reading `Retry-After`. The owner's rule: generous, sized for a large company.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The proposal, with the anonymous parallel transfers 270: one anonymous restore of 200 with a third of room (chosen) | Every other figure as proposed | A normal anonymous command never waits for a lease; a flood is held by the request and byte figures, the slots and the ceiling | An anonymous address may hold 270 transfers of the Bastion's cache at once |
| (b) The proposal unchanged, 80 | | Tighter | One normal restore waits |
| (c) Unlimited parallel transfers | | | No figure is unlimited by default |

**Choice:** (a). **Applied in the spec:** R-LIM-046; code: [model.go](../../internal/limits/model.go) (`BuiltinProfiles`), migration 00147.

### Q-LIM-311 · A preset is applied by the registration's patch in one request, before the patch's other members

Status: **Decided by the development agent on 2026-11-26** (package E8) — selected from the options below; the owner reviews it at the
walkthrough.

Context: R-LIM-046 and the screen's Q-UI-1481 apply a preset after a confirmation, in one step.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `limits.preset` of `PATCH /api/v1/bastions/{name}` applies the preset (kind rules, flood figures, mode) in the update's transaction, then the patch's other members of `limits` on top; `custom` applies nothing (chosen) | One request, one revision, one event | A body read by `GET` and sent back unchanged applies nothing new; a preset and a stricter figure in one request | `preset` is a member that acts, not only one that is stored |
| (b) An operation of its own, `POST …/limits/preset` | | Explicit | A second operation and a second `ETag` round trip for one step |

**Choice:** (a). **Applied in the spec:** R-LIM-046, [bastion.md](bastion.md) R-BAS-093.

### Q-LIM-312 · The preset a registration reads is derived, never stored as custom

Status: **Decided by the development agent on 2026-11-26** (package E8) — selected from the options below; the owner reviews it at the
walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The registration stores the preset last applied; it reads `custom` while a kind rule names another profile or carries an override, or a flood figure differs from the preset's (chosen) | `bastionmain.EffectivePreset` | A change of a kind rule through the rules API needs no write to the registration; a rule changed back reads the preset again | The stored value and the shown one can differ |
| (b) Every change of a kind rule or flood figure writes `custom` | | One value | The store of the limits writes the registration |

**Choice:** (a). The mode is no part of the comparison: R-LIM-046 names the kind rules and the flood figures. **Applied in the spec:**
R-LIM-046.

### Q-LIM-313 · The usage overview's place filter is installation or bastion:name

Status: **Decided by the development agent on 2026-11-26** (package E8) — selected from the options below; the owner reviews it at the
walkthrough.

Context: R-UI-242's filter **Place**: All, This installation, one entry per Bastion.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `place=installation` reads every place but a Bastion's, `place=bastion:<name>` that registration's place (its key `bastion:<id>`), absent every place; a name that is no registration answers `404` (chosen) | One parameter of `GET /api/v1/usage` and its export | The screen's three choices, by name in the page's address | A deleted registration's rows are not reachable by name (H-2355) |
| (b) The place key itself | | No lookup | An ID in the address |

**Choice:** (a). **Applied in the spec:** R-LIM-045, [api-and-ui.md](api-and-ui.md) R-UI-242.

### Q-LIM-314 · A Bastion's usage row that does not read is dropped and counted, never refusing the batch

Status: **Decided by the development agent on 2026-11-26** (package E8) — selected from the options below; the owner reviews it at the
walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A row whose subject key is no subject of R-LIM-001 a Bastion can name (the system's is not), whose credential is longer than 64 bytes, whose rule is no rule ID, whose repository is no qualified name, or that is older than `bastions.downloads.max_age` is dropped; the number is logged with the registration; a repository that no longer exists is recorded without one (chosen) | The batch's events and its other rows are kept | One bad row loses nothing else | A Bastion that sends bad rows learns it from the main installation's log only |
| (b) Refuse the batch | | The Bastion learns at once | One row holds back a batch's events, which the Bastion sends again forever |

**Choice:** (a). **Applied in the spec:** R-LIM-045, [bastion.md](bastion.md) Q-BAS-442.

## What this does not cover

- The protocol gaps for the owner: [Gaps of the protocols](#gaps-of-the-protocols).
- The perspective and its own gaps (H-1010 to H-1012, H-1014, H-1015; for a Bastion's budgets H-2284, H-2286 to H-2288, H-2368 and
  H-2369): [security/usage-and-limits.md](../security/usage-and-limits.md).

### H-1005 · Counters lost in a Valkey outage or failover give a subject up to one more window of every budget

Component: the limits counters in Valkey (R-LIM-017, R-LIM-019). Subject: every budget key and ceiling. Dormant until the mode is
`enforce`; live then whenever Valkey is unreachable or fails over with asynchronous replication. What makes it real: a Valkey
incident, or a caller who can cause one; the local windows of each replica start empty and are not merged back, so each window open
during the incident admits up to its limit again (per replica its share, together the limit) on top of what was counted before.

### H-1006 · A download admitted before its budget was used up runs to its end

Component: the check before a download (R-LIM-021). Subject: every budget key with an hour or day byte figure. Live in `enforce`.
What makes it real: a subject with `parallelTransfers` downloads of large files started just below the limit transfers all of them
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

### H-1600 · An upstream request step 9a admitted counts also when the remote engine then sends nothing

Component: step 9a and the remote engine after it (R-LIM-022, Q-LIM-110; [proxy/fetch.go](../../internal/proxy/fetch.go) `fetcher.run`,
`fetcher.inSlot`, [query.go](../../internal/proxy/query.go), [artifact.go](../../internal/proxy/artifact.go) `head`). Subject: every
budget key and ceiling with `upstreamRequestsPerHour`. Live in `enforce` and `audit`. What makes it real: 9a counts the upstream request
when it admits it, before the fetch takes its slot and asks the circuit; a fetch the queue times out
(`proxy.queue_timeout_seconds`) or the open circuit answers as assumed offline sends nothing upstream, and its upstream request stays
counted. A client that retries against a remote whose upstream is down spends its upstream budget without reaching the upstream, and
finds it used up when the upstream is back.

### H-1861 · The built-in limit profiles read in English in a German installation

Component: the five built-in profiles of the download limits (`unlimited`, `standard`, `technical`, `restricted`, `anonymous`), which
[migration 00081](../../internal/meta/postgres/migrations/00081_limits.sql) writes with an English name and description; the tab
**Profiles** of **Download limits** ([limits-profiles-tab.ts](../../web/src/app/features/limits/limits-profiles-tab.ts)). Subject: every
reader of the tab in German (administrators, auditors). Live today: the German page shows "Standard", "Technical" and "Technical tokens
and CI projects without a rule of their own." as the installation stored them. Both are stored and an administrator may rewrite them, so
the screen shows them as written (`translate="no"`, R-UI-214) and the German render pass does not count them as a server's sentence.
What would end it: the screen says a built-in profile's first name and description from its key in its language, and shows the stored
text only once an administrator changed it; that needs the API to tell a changed text from the first one, which it does not.

### H-2260 · A technical token's transfers can hold most of a further endpoint's request slots on one replica

Component: the parallel transfers (R-LIM-024) with the built-in Technical figure of 1,000 (Q-LIM-294), and the request slots of a
further endpoint (its own `max_in_flight`, 1,024 by default, [endpoints.md](endpoints.md) R-EP-015). Subject: the other callers of a
further endpoint whose requests land on the same replica. Dormant until a further endpoint exists; live in `enforce` and `audit` when a
technical token runs many CI jobs through it. What makes it real: up to 1,000 transfers of one token through one replica's further
endpoint hold 1,000 of its 1,024 slots, and the replica sheds the endpoint's other requests with 503 `server_busy` until they end. On the
default endpoint (4,096 slots) the same token holds at most a quarter. A rule with a smaller figure for that token, or a larger
`max_in_flight` of the endpoint, closes it for an installation; the figure is not bounded by the endpoint's slots.

### H-2261 · A client without a bound of its own opens one transfer per package or layer

Component: the parallel transfers (R-LIM-024) and the clients of R-LIM-041 without a bound: `dotnet restore` (one transfer per package
of the restore) and `ctr images pull` (one per layer). Subject: a caller whose restore or pull has more packages or layers than its
figure. Live in `enforce`. What makes it real: the transfers beyond the figure wait up to `limits.parallel.wait` (10 s); with cached
packages the places free within milliseconds and the restore completes, but over a slow link, with large packages or with a low figure a
transfer waits 10 s and gets 429, and NuGet retries about once a second without reading `Retry-After`. The numbers of R-LIM-041 were
measured once against a stub, not against Quader; ansible-galaxy, helm and a Kubernetes node were not measured.

### H-2263 · An anonymous caller can spend the pulls of a remote of Docker Hub for every caller of the remote

Component: the built-in Anonymous profile's `upstreamRequestsPerHour` (3,000 since Q-LIM-296) and a remote repository of Docker Hub
without credentials or with a Personal account, whose ceilings are unset by default (R-LIM-015, Q-LIM-16). Subject: every caller of that
remote, signed in or not, for what is not cached. Live in `enforce` and `audit` where anonymous callers may read the remote's artifacts.
What makes it real: one or a few anonymous addresses pull distinct images for the first time; each first pull is one Docker Hub pull per
platform, so about 60 an hour from one address spend an unauthenticated address's 100 pulls per six hours (Q-LIM-298); Docker Hub then
answers 429, the remote's circuit opens, and every caller gets 503 `upstream_rate_limited` for what is not cached until the circuit
closes. What closes it for an installation: credentials of a paid Docker Hub account on the remote, or the remote's anonymous ceiling
set to what the account allows; the default does neither.

### H-2264 · The upstream requests of an act are derived, not counted at an upstream behind a remote

Component: the column *First time through a remote* of R-LIM-042, from which the upstream figures of Q-LIM-296 are set. Subject: every
built-in profile's `upstreamRequestsPerHour`. Live. What makes it real: the counts follow from Q-LIM-59's measurement of the clients'
requests and from the remotes' code (an npm tarball's miss fetches its document first, R-NPM-025); no client ran through a remote whose
stub upstream counted the exchanges. An OCI index with more attestations, Galaxy's paging of a long version list or a NuGet restore over
several feeds costs more than the table says. The figures hold a third or more of room above every promised use, which covers the known
differences; a format whose act costs more than that is met first. What would close it: one run per format of its client through a remote
against a counting stub, written into R-LIM-042.
