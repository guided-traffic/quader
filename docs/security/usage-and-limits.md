# Download limits and usage: what bounds a caller, and what the overview knows about it

Perspective: how much one caller can take from Quader and from the upstreams behind its remote repositories, who can change that,
what the usage overview records about people and addresses, and where the bounds stop. The mechanisms are specified on
[spec/usage-and-limits.md](../spec/usage-and-limits.md) (walk item W-26, the owner's decisions of 2026-11-19) and
[identity-and-authentication.md](../spec/identity-and-authentication.md) R-AUTH-045 and R-AUTH-046 (technical tokens, a
repository's service accounts). All of it is built: the technical tokens and a repository's service accounts (the code is described
on [tokens-and-credentials.md](tokens-and-credentials.md) and [permissions.md](permissions.md#account-grants)), the accounting, the
enforcement, the usage store and the screens ([developer/usage-and-limits.md](../developer/usage-and-limits.md)). Where the code
differs from what the specification promises, the gap is named below. What was built before on the same paths is on
[api-edge.md](api-edge.md) (the request classes of R-API-016) and [endpoints-and-exposure.md](endpoints-and-exposure.md) (the
endpoint's class).

## What the limits protect, and what they do not

| They protect | How | They do not |
|---|---|---|
| The installation's capacity against one caller | request figures per second and per hour; bytes per second paced, bytes per hour and day refused before a download; parallel downloads; per subject and place ([R-LIM-004](../spec/usage-and-limits.md#r-lim-004--the-figures)) | protect a replica's CPU and memory: that is `max_in_flight` and the memory guard of stage 4 ([R-ARCH-005](../spec/architecture.md#r-arch-005--http-listeners-routing-and-middleware-chain)) |
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

What remains is H-1010: anonymous colleagues behind the same address (readers of a `public` repository who use no credential) are
blocked for the block's time, repeatably. The failed-credential throttle of R-AUTH-038 is a separate mechanism with its own
exemption of recently validated tokens and is unchanged.

## An anonymous caller on a public remote

A remote repository whose artifacts are `public` with `cacheFillAction: read` lets a request without a credential fill its cache.
Per case:

| Mode | Stage | Subject | What bounds it | Live or dormant |
|---|---|---|---|---|
| `audit` (switched to by an administrator) | 7 | the address prefix | nothing: no request limit, nothing on bytes, upstream requests or parallel downloads; everything is recorded as would-have-been-limited ([R-LIM-026](../spec/usage-and-limits.md#r-lim-026--audit-and-enforce), [Q-LIM-4](../spec/usage-and-limits.md#q-lim-4--in-audit-nothing-on-a-protocol-path-is-refused-or-slowed-the-package-classes-of-r-api-016-included)) | dormant in the default state; live from an administrator's switch to `audit` (H-1015) |
| `enforce` (the default) | 7 | the address prefix | the profile Anonymous: 20 per second, 2,000 per hour | live |
| `enforce` | step 9a of R-REPO-025 | the address prefix and the remote's anonymous ceiling | 20 upstream requests and 2 GB from the upstream per hour per address; the ceiling for all anonymous callers, which is unset by default ([Q-LIM-16](../spec/usage-and-limits.md#q-lim-16--a-ceiling-is-a-remote-repositorys-for-the-two-upstream-figures-and-none-is-set-by-default)) | live; the installation's upstream quota is bounded only once a ceiling is set (H-1007) |
| `enforce` | `edge.ServeItem` | the address prefix | 10 GB from the cache per hour, 30 GB a day, 4 parallel downloads, paced at 20 MB/s per address | live; many addresses together are bounded by the replicas' capacity only (H-1007) |
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
| A user's consumption, with the personal token's ID per row | five-minute and hourly rows | 25 hours and 30 days | `usage:read` (administrator, security administrator, auditor), the Owner of the project for its repositories' rows, the user itself |
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

## What this does not cover

The gaps of the spec page whose mechanisms this page describes:

- [H-1005](../spec/usage-and-limits.md#h-1005--counters-lost-in-a-valkey-outage-or-failover-give-a-subject-up-to-one-more-window-of-every-budget): counters lost in a Valkey outage or failover give a subject up to one more window of every budget.
- [H-1006](../spec/usage-and-limits.md#h-1006--a-download-admitted-before-its-budget-was-used-up-runs-to-its-end): a download admitted before its budget was used up runs to its end.
- [H-1007](../spec/usage-and-limits.md#h-1007--many-addresses-are-many-anonymous-budgets): many addresses are many anonymous budgets.
- [H-1008](../spec/usage-and-limits.md#h-1008--the-limits-keys-in-valkey-grow-with-the-addresses-that-call-and-can-evict-other-keys): the limits' keys in Valkey grow with the addresses that call and can evict other keys.

### H-1010 · Anonymous callers behind one address are blocked by refused requests another caller sends from it

Component: the block of [R-LIM-025](../spec/usage-and-limits.md#r-lim-025--the-block-after-refused-requests). Subject: readers of
`public` repositories without a credential behind a shared address (a NAT, a CI fleet, a proxy). Dormant in `audit`; live in
`enforce`. What makes it real: anyone behind the same address sends `refusedPerHour` wrong tokens (60 an hour with the profile
Anonymous); every anonymous request from the address is then refused for the block time, again after each block. Callers with a
valid credential are not affected.

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

