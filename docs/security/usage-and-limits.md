# Download limits and usage: what bounds a caller, and what the overview knows about it

Perspective: how much one caller can take from Quader and from the upstreams behind its remote repositories, who can change that,
what the usage overview records about people and addresses, and where the bounds stop. The mechanisms are specified on
[spec/usage-and-limits.md](../spec/usage-and-limits.md) (walk item W-26, the owner's decisions of 2026-11-19) and
[identity-and-authentication.md](../spec/identity-and-authentication.md) R-AUTH-045 and R-AUTH-046 (technical tokens, a
repository's service accounts). All of it is built but [a Bastion as a place](#a-bastion-as-a-place), specified on 2026-11-26: the technical tokens and a repository's service accounts (the code is described
on [tokens-and-credentials.md](tokens-and-credentials.md) and [permissions.md](permissions.md#account-grants)), the accounting, the
enforcement, the usage store and the screens ([developer/usage-and-limits.md](../developer/usage-and-limits.md)). Where the code
differs from what the specification promises, the gap is named below. What was built before on the same paths is on
[api-edge.md](api-edge.md) (the request classes of R-API-016) and [endpoints-and-exposure.md](endpoints-and-exposure.md) (the
endpoint's class).

## What the limits protect, and what they do not

| They protect | How | They do not |
|---|---|---|
| The installation's capacity against one caller | request figures per second and per hour; bytes per second paced, bytes per hour and day refused before a download; parallel transfers (each layer of an image and each package of an install is one), whose waiting holds at most a quarter of an endpoint's request slots so that callers at their figure cannot make a replica shed another caller's requests ([R-LIM-024](../spec/usage-and-limits.md#r-lim-024--parallel-transfers)); per subject and place ([R-LIM-004](../spec/usage-and-limits.md#r-lim-004--the-figures)) | protect a replica's CPU and memory: that is `max_in_flight` and the memory guard of stage 4 ([R-ARCH-005](../spec/architecture.md#r-arch-005--http-listeners-routing-and-middleware-chain)) |
| The upstream's quota (Docker Hub's pull limit, a partner registry's rate) against all callers of one kind together | the ceilings of a remote for all anonymous and all signed-in callers ([R-LIM-015](../spec/usage-and-limits.md#r-lim-015--the-ceilings-of-a-remote-repository)) and the upstream figures of each caller, checked before an exchange at step 9a of R-REPO-025 | bound mirror passes and other system work, which have their own bounds (R-REPO-052 to R-REPO-055) |
| Names that are hidden from a caller | refused requests per hour and the block ([R-LIM-008](../spec/usage-and-limits.md#r-lim-008--a-refused-request), [R-LIM-025](../spec/usage-and-limits.md#r-lim-025--the-block-after-refused-requests)) | decide what a caller may read: the decision point does, before and independently of every figure |

A limit never grants: a rule can only change how much of what the decision point admits a caller gets. A caller over a limit is
refused with 429 or slowed; it is never given a 403 or a 404 by a limit, so a limit leaks nothing about what exists.

## Whose budget a request spends

| Stage | What decides the subject | What it trusts |
|---|---|---|
| 6, authentication | the principal and the credential; a failed credential is anonymous | the token store and caches of R-AUTH-023 |
| 7, the limits stage | the subject of [R-LIM-001](../spec/usage-and-limits.md#r-lim-001--the-subject-a-request-is-charged-to): a user with all its sessions and personal tokens, a technical token, a CI project, an anonymous address prefix | for an anonymous subject, the client address of R-ARCH-006 |
| `/v2/_token` | the token service's own authentication of its Basic credential, after the handler ran ([R-LIM-003](../spec/usage-and-limits.md#r-lim-003--a-failed-credential-the-registrys-token-service-and-the-bastion)) | the same token store |

- A wrong or revoked token spends its client address's budget, never the budget of the token's owner.
- A registry token spends the budget of the Quader token it was issued for.
- A request forwarded by a Bastion with a caller's credential spends that caller's budget; an anonymous one spends the forwarded
  client address, which the main installation reads only from the Bastions it trusts (R-BAS-017).
- A download ahead of a client (specified on 2026-11-23, not built, [remote-curation.md](remote-curation.md)): one version that a person starts, and the
  examination a person's request for an allow starts, spend that person's budget as a probe's background download spends its caller's;
  a bulk of a holder of `manage` is system work and spends no budget
  ([H-1725](remote-curation.md#h-1725--a-holder-of-manage-can-make-quader-fetch-up-to-a-bulks-bound-without-a-ceiling)).

## Trust in the client address

The budget of an anonymous caller is as good as the client address (R-ARCH-006). With `server.trusted_proxies` too wide, a client
names its own address in the forwarded header and picks a fresh budget per request; with it empty behind an edge proxy, every
anonymous caller is the proxy's address and shares one budget, and one abusive caller blocks them all (H-1012). An IPv6 caller is one
subject per `/64`, so a host cannot dodge its budget by rotating within its own prefix; a provider that hands out `/48`s gives a
caller 65,536 budgets (H-1007).

## Lock-out by refused requests

The block of [R-LIM-025](../spec/usage-and-limits.md#r-lim-025--the-block-after-refused-requests) holds one budget key: a subject at
one place. Its cases:

| Who sends the refused requests | What is blocked | What is not |
|---|---|---|
| A caller with a valid token that probes for hidden names | that token's subject (its user, its technical token, its CI project) at the place | every other subject |
| An attacker who sends wrong or revoked tokens from a victim's NAT address | the NAT address's anonymous subject: requests without a credential and with failed credentials from it | every colleague behind the NAT who presents a valid credential: their requests are charged to their own subjects |
| An attacker who probes without a credential | the address's anonymous subject | — |
| A caller that keeps sending past its request figure | nothing: a `429` `rate_limited` is no refused request ([Q-LIM-58](../spec/usage-and-limits.md#q-lim-58--a-429-of-a-request-figure-is-not-a-refused-request), the owner's decision) | every subject, the caller's own included; a request the block refuses counts nothing either, so the block does not lengthen itself |

A refused request fills no request window ([Q-LIM-57](../spec/usage-and-limits.md#q-lim-57--the-request-windows-are-decided-and-counted-in-one-step-by-a-script-in-the-round-trip-of-stage-7)):
a caller that keeps sending gets its figure's rate and nothing more, for as long as it sends, and is never blocked for it. What that
costs the installation is one round trip to Valkey at stage 7 per refused request, the same as a request the block would refuse; a
client whose many connections overflow its burst at once (several installs behind one NAT address) is not shut out for the block's
time.

What remains is H-1010: anonymous colleagues behind the same address (readers of a `public` repository who use no credential) are
blocked for the block's time, repeatably. The failed-credential throttle of R-AUTH-038 is a separate mechanism with its own
exemption of recently validated tokens and is unchanged.

## An anonymous caller on a public remote

A remote repository whose artifacts are `public` with `cacheFillAction: read` lets a request without a credential fill its cache.
Per case:

| Mode | Stage | Subject | What bounds it | Live or dormant |
|---|---|---|---|---|
| `audit` (switched to by an administrator) | 7 | the address prefix | nothing: no request limit, nothing on bytes, upstream requests or parallel transfers; everything is recorded as would-have-been-limited ([R-LIM-026](../spec/usage-and-limits.md#r-lim-026--audit-and-enforce), [Q-LIM-4](../spec/usage-and-limits.md#q-lim-4--in-audit-nothing-on-a-protocol-path-is-refused-or-slowed-the-package-classes-of-r-api-016-included)) | dormant in the default state; live from an administrator's switch to `audit` (H-1015) |
| `enforce` (the default) | 7 | the address prefix | the profile Anonymous: 50 per second with a burst of 1,000, 20,000 per hour; a request refused there counts in no window and is no refused request of the address | live |
| `enforce` | step 9a of R-REPO-025 | the address prefix and the remote's anonymous ceiling | 20 upstream requests and 2 GB from the upstream per hour per address; the ceiling for all anonymous callers, which is unset by default ([Q-LIM-16](../spec/usage-and-limits.md#q-lim-16--a-ceiling-is-a-remote-repositorys-for-the-two-upstream-figures-and-none-is-set-by-default)) | live; the installation's upstream quota is bounded only once a ceiling is set (H-1007) |
| `enforce` | `edge.ServeItem` | the address prefix | 10 GB from the cache per hour, 30 GB a day, 64 parallel transfers, paced at 20 MB/s per address | live; many addresses together are bounded by the replicas' capacity only (H-1007) |
| both | the usage store | the address prefix | the per-replica buffer is bounded (100,000 keys, then an overflow row), the Valkey keys expire within 48 hours | Valkey eviction is H-1008 |

An instance starts in `enforce`. What makes the `audit` row real: an administrator switches to `audit` on an installation that
publishes a public remote to the internet; the switch's dialog says in one sentence that nothing is refused or slowed there
afterwards ([R-UI-182](../spec/api-and-ui.md#r-ui-182--the-mode-switch)).

## Who can raise a limit

| Change | Who | Bound |
|---|---|---|
| Profiles, the instance's rules, the mode, the block time, a project's maximum | `limits:write`: the role `admin` only ([R-AUTHZ-040](../spec/authorization.md#r-authz-040--capabilities-for-download-limits-and-usage)) | none: the instance administrator decides (owner, decision 14) |
| A rule at a project or one of its repositories | an Owner of the project | per figure the larger of the instance's value for the same caller and the project's maximum; clamped again at every evaluation, so a lowered maximum binds stored rules at once ([R-LIM-016](../spec/usage-and-limits.md#r-lim-016--who-may-set-what-and-the-projects-maximum)) |
| A remote's ceilings | an Owner, `repositories:write-config`, `limits:write` | a ceiling only narrows |
| Lift a block | `limits:write`; an Owner for the places of its project | — |

An Owner cannot raise a limit for its project above both the instance's value and its maximum, cannot limit the installation's
administrators (they are decided at the instance only), and cannot see or change rules of other projects. A `security-admin` and an
`auditor` read limits and usage and change nothing.

## Personal data in the usage store

| Data | Where | How long | Who reads it |
|---|---|---|---|
| A user's consumption, with the personal token's ID per row | five-minute and hourly rows | 25 hours and 30 days | `usage:read` (administrator, security administrator, auditor), the Owner of the project for its repositories' rows and for the rows its virtual repositories took from a well-known repository of `upstream` (`via_repository_id`, Q-LIM-63: only requests through the project's own virtual repositories, never another project's callers of the central repository), the user itself |
| An anonymous caller's address (an IPv4 address, an IPv6 `/64`) | the same rows, `limit_block` | 25 hours, 30 days | the same; a project Owner sees the addresses of its repositories' anonymous callers (H-1011) |
| Sums per day, repository and kind of caller, no person | `usage_daily_summary` | 400 days by default | the same readers |

A deleted user's rows are erased by the daily task within one day ([R-LIM-028](../spec/usage-and-limits.md#r-lim-028--writing-the-usage-store-erasure-and-addresses));
the metrics carry no person (R-OPS-007); the request log carries the subject as it carries the principal and the address today.

## Technical tokens

A technical token's rights are its service account's; its budget is its own. A repository's service account holds actions on its
repository only, is administered by the project's Owners, moves with its repository and is deleted with it; it cannot be named in a
policy, a group, a role binding or a project membership ([R-AUTHZ-039](../spec/authorization.md#r-authz-039--the-actions-of-a-repositorys-service-account)).
Its tokens follow the Owner's token bounds (never non-expiring, no administrator scope, no system item). Rotation and a new secret
keep the token's budget subject, so a leaked secret that is revoked and replaced keeps its system's limit; the leaked secret itself
is dead. A deleted token's rules go with the daily task `limits-cleanup`; a new token never inherits them, since a rule names the
token's ID ([Q-LIM-49](../spec/usage-and-limits.md#q-lim-49--a-deleted-technical-tokens-rules-and-blocks-go-with-the-daily-task-its-usage-stays)).
A registry token issued for a technical token's secret spends the technical token's budget, on the token request and on every
registry request after it (tested on the request path: `TestR_LIM_001_ARegistryTokenOfATechnicalTokenIsChargedToTheTechnicalToken`). Built: the subject is the technical token's ID, which the authentication stage hands on as `Credential.TechnicalTokenID`
([principal.go](../../internal/httpserver/principal.go)); a rotation's successor and a new secret carry the same ID
([rotate.go](../../internal/auth/token/rotate.go), [technical.go](../../internal/auth/token/technical.go)). A repository's account
being deleted keeps authenticated tokens for as long as the delete job runs, with no right
([H-1013](permissions.md#h-1013--the-tokens-of-a-repositorys-accounts-authenticate-until-the-repository-row-is-removed)).

## The precision that remains

While Valkey is reachable, a fixed window admits at most its limit, a rolling interval at most twice, plus the remainder of the
downloads admitted before a byte budget was reached and one second of unflushed bytes
([R-LIM-018](../spec/usage-and-limits.md#r-lim-018--windows-and-their-precision-across-replicas)). While it is not, each replica
counts its share from zero (H-1005).

## A Bastion as a place

The owner's decisions of 2026-11-26 (W-34; [spec/usage-and-limits.md](../spec/usage-and-limits.md#a-bastion-as-a-place) R-LIM-043 to
R-LIM-046, [spec/bastion.md](../spec/bastion.md) R-BAS-096), built by E8 and E9b
([developer/bastion-limits.md](../developer/bastion-limits.md)): each Bastion is a place of the rules, and what it serves, its hits
included, is counted against the caller's budget at that place, in the Bastion's memory or its Valkey
([internal/bastion/budget](../../internal/bastion/budget/budget.go)). What changes for this page's perspective:

- **Two budgets.** A forward of a Bastion is charged at the main installation as before and at the Bastion as well; neither replaces the
  other, so a taken-over Bastion that ignores its own budgets still meets the main installation's for everything it forwards.
- **The Bastion decides a budget with the main installation's words.** The subject key and the applying rule of a signed-in caller come
  with the check answer and the marking (Q-LIM-302); a change of a group reaches the Bastion's charge with the caller's next answer
  ([H-2284](#h-2284--a-changed-membership-reaches-a-bastions-charge-only-with-the-callers-next-answer)).
- **Audit at a Bastion is not "nothing refused".** The budgets of its place stop refusing; its flood protection, the anonymous ceiling, the
  slots and the throughput hold (Q-LIM-309), unlike the instance's Audit (H-1015).
- **Personal data.** A Bastion's usage rows hold addresses and subjects like the main installation's (R-LIM-028) and are deleted with them;
  its blocks' addresses are kept with the replica's contact ([security/bastion.md](bastion.md#h-2283--the-main-installation-keeps-the-addresses-of-a-bastions-blocks)).

## What this does not cover

The gaps of a Bastion's budgets (W-34) are recorded on this page, each with the package whose mechanism has it: H-2284, H-2286 to H-2288,
H-2368 and H-2369; those of a Bastion's other mechanisms are on [bastion.md](bastion.md#what-this-does-not-cover).

The gaps of the spec page whose mechanisms this page describes:

- [H-1005](../spec/usage-and-limits.md#h-1005--counters-lost-in-a-valkey-outage-or-failover-give-a-subject-up-to-one-more-window-of-every-budget): counters lost in a Valkey outage or failover give a subject up to one more window of every budget.
- [H-1006](../spec/usage-and-limits.md#h-1006--a-download-admitted-before-its-budget-was-used-up-runs-to-its-end): a download admitted before its budget was used up runs to its end.
- [H-1007](../spec/usage-and-limits.md#h-1007--many-addresses-are-many-anonymous-budgets): many addresses are many anonymous budgets.
- [H-1008](../spec/usage-and-limits.md#h-1008--the-limits-keys-in-valkey-grow-with-the-addresses-that-call-and-can-evict-other-keys): the limits' keys in Valkey grow with the addresses that call and can evict other keys.

### H-1010 · Anonymous callers behind one address are blocked by refused requests another caller sends from it

Component: the block of [R-LIM-025](../spec/usage-and-limits.md#r-lim-025--the-block-after-refused-requests). Subject: readers of
`public` repositories without a credential behind a shared address (a NAT, a CI fleet, a proxy). Dormant in `audit`; live in
`enforce`. What makes it real: anyone behind the same address sends `refusedPerHour` wrong tokens (300 an hour with the profile
Anonymous), or as many anonymous requests for names the address may not read; every anonymous request from the address is then
refused for the block time, again after each block. Requests past the address's request figure do not block it: a `429` is no
refused request ([Q-LIM-58](../spec/usage-and-limits.md#q-lim-58--a-429-of-a-request-figure-is-not-a-refused-request)), so they
only hold the address's anonymous callers to the figure's rate together. Callers with a valid credential are not affected.

### H-1011 · A project Owner reads the addresses of the anonymous callers of its repositories

Component: the usage overview's project scope ([R-LIM-029](../spec/usage-and-limits.md#r-lim-029--who-sees-which-usage),
[Q-LIM-21](../spec/usage-and-limits.md#q-lim-21--a-project-owner-sees-the-addresses-of-the-anonymous-callers-of-its-repositories),
decided by the owner on 2026-11-19). Subject: every person who reads a project's `public` repositories without a credential. Live. What makes it real: an
Owner who is not entitled by the installation's data protection rules to see visitors' addresses reads them for 30 days, and can
export them as CSV.

### H-1012 · The budget of an anonymous caller is as good as the client address

Component: the anonymous subject of [R-LIM-002](../spec/usage-and-limits.md#r-lim-002--the-address-of-an-anonymous-subject) on
the client address of R-ARCH-006. Subject: every anonymous budget and block. Live. What makes it real: a
`server.trusted_proxies` that trusts a network the client controls lets it choose an address per request and never meet a budget or
a block; an empty one behind an edge proxy makes every anonymous caller the proxy's address, so one caller's consumption or block
is everyone's.

### H-1015 · While the mode is Audit an instance has no request limit on its protocol paths

Component: stage 7 on the protocol paths (`/api/<format>/…`, `/files/…`, `/v2/…`) in the mode of
[R-LIM-026](../spec/usage-and-limits.md#r-lim-026--audit-and-enforce), by the owner's [Q-LIM-4](../spec/usage-and-limits.md#q-lim-4--in-audit-nothing-on-a-protocol-path-is-refused-or-slowed-the-package-classes-of-r-api-016-included) (b)
of 2026-11-19: in `audit` no profile figure, no ceiling, no block and no class of R-API-016 refuses or slows there (`package_write`
holds in `enforce` only). Subject: every caller of a protocol path, above all every remote repository whose artifacts are `public`
on an endpoint the internet reaches. Dormant in the default state, `enforce`; live from the moment an administrator switches to
`audit`. What makes it real: a caller without a credential makes such a remote fetch from its upstream as many artifacts as it asks
for, unbounded, spending the installation's upstream quota (Docker Hub's pull limit) and storage, and a client with a token sends
reads and publishes at any rate, until an administrator switches back to `enforce`. The plan's finding F-155 is open in `audit` by
the owner's decision. The limits of the management API (R-API-016's `read`, `write`, `anonymous`), of the sign-in (R-AUTH-038) and
of the token endpoints are not modes of the download limits and hold in both.

### H-1014 · In Audit mode a block set before the switch stays in Valkey until it expires, unenforced

Component: the block of [R-LIM-025](../spec/usage-and-limits.md#r-lim-025--the-block-after-refused-requests). Subject: every budget
key blocked in `enforce` shortly before an administrator switched to `audit`. Dormant: stage 7 in `audit` records the marker as
`would_limit` and refuses nothing (R-LIM-026), and the overview shows the block as active until its end. What makes it real: a switch
back to `enforce` within the block time refuses the key again for the rest of it, without a new refused request.

### H-2284 · A changed membership reaches a Bastion's charge only with the caller's next answer

Stage: R-LIM-043, Q-LIM-302 (packages E8 and E9b). Subject: a signed-in caller whose group carries a rule at a Bastion's place. Live
since E9b (2026-11-26). The main installation resolves the caller's rule and names its ID with the answer; the Bastion charges by it until the caller's
answer ends (`bastion.answers.period`, 5 minutes by default) or a feed entry drops it. A `principal` entry drops it at once for a removed
member; a group whose rule changes, or a membership that only widens, takes effect at the next answer. What makes it real: an administrator
who tightens a group's rule at a Bastion and expects it in seconds; a new revision of the configuration carries the rule's new figures at
once, only the choice of the rule waits.

### H-2286 · Without Valkey a Bastion's budgets hold per replica, and a restart forgets them

Stage: R-BAS-096 (package E9b). Subject: every budget key at a Bastion's place. Live since E9b (2026-11-26), on a Bastion without
Valkey. Each
replica counts its share of every figure (the rule of R-ARCH-034), so a caller spread over the replicas by the Service gets its whole budget,
and a caller pinned to one replica its share only; a replica's restart starts its counters empty (the counterpart of H-1005). A replica's
memory holds at most 100,000 counters (`maxLocalCounters` in [bytes.go](../../internal/bastion/shared/bytes.go)), about ten per subject that
moves bytes; a flood from more addresses than that empties them, and every subject starts its windows again. With Valkey the counters are
shared and survive a replica, not Valkey's loss.

### H-2287 · Many addresses are many anonymous budgets at a Bastion too

Stage: R-BAS-096 (package E9b), the anonymous subject by address. Subject: everything an anonymous caller can read through a Bastion. Live
since E9b (2026-11-26), as [H-1007](../spec/usage-and-limits.md#h-1007--many-addresses-are-many-anonymous-budgets) is at the main installation: a
caller with many IPv4 addresses or IPv6 `/64` prefixes gets a budget per prefix. What bounds them together: the anonymous ceiling for what
reaches the main installation (R-BAS-094), the throughput's anonymous share for what crosses the link (R-BAS-097), and for hits nothing but
the replicas' `max_in_flight` and the network (the Bastion's own H-382).

### H-2288 · A paced hit holds a replica's request slot for longer

Stage: R-BAS-096, Q-BAS-330 (package E9b). Subject: the other callers of a Bastion replica. Live since E9b (2026-11-26), in Enforce. A hit paced to a
subject's `cachedBytesPerSecond`, never below 64 KiB a second, holds its request slot until its last byte: a layer of 1 GB at the floor
holds one for about 4.2 hours [derived], and a subject's parallel transfers multiply it up to its `parallelTransfers`. The waiting leases hold
at most a quarter of `max_in_flight` (Q-BAS-329), the transfers in progress are bounded by the subject's figure only; many slow subjects can
hold most of a replica's 1,024 slots, and the replica then sheds with `503`. What bounds it: the figures of "Bastion: anonymous" set by E4,
the flood protection per address, and the replicas.

### H-2368 · At the turn of an hour a subject can hold up to twice its parallel transfers at a Bastion

Stage: R-BAS-096, Q-BAS-329 (package E9b; `Charge.claimLease` in [budget.go](../../internal/bastion/budget/budget.go)). Subject: every budget
key with `parallelTransfers` at a Bastion's place. Live since E9b, in Enforce. A transfer's lease is counted in the counter of the hour it
began in, which lives two hours, and given back there; a transfer that begins in the next hour counts in a new counter, which does not see
the transfers still running from the previous one. What makes it real: a subject that holds its figure's transfers (270 anonymous, 340
signed in by default) across the hour's turn may start as many again, and with long transfers paced at the floor it holds twice its figure's
request slots of the replicas (H-2288). The waiting leases stay bounded by a quarter of `max_in_flight` (Q-BAS-329); each transfer still
pays its bytes against the hour and day figures.

### H-2369 · A Bastion charges a caller it does not know yet only after the main installation's answer

Stage: R-BAS-096, Q-BAS-470 and Q-BAS-471 (package E9b; `learnSubject` in [budget.go](../../internal/bastion/engine/budget.go),
`budget.SubjectOf`). Subject: a caller with a token the Bastion holds no answer for, and a caller the main installation names no limits for.
Live since E9b, in Enforce. A token's first request within the answer period (`bastion.answers.period`, 5 minutes by default) is counted when
the main installation's answer names its subject and is refused by none of the request figures; a download it starts is checked before its
first byte. A caller whose answer names no key and rule (a registration the main installation's index does not hold yet, a replica without
the limits' index) is charged by the users' kind rule under `user:<principal ID>`, so a technical token or a CI project is charged and
reported as a user meanwhile. What makes it real: a client that presents a new token for each command (a signed-in registry client takes a
registry token per pull, R-BAS-052) passes the first request of each command past the request figures; the flood protection per address
and the main installation's own budget for every forward hold it (two budgets, Q-LIM-306).
