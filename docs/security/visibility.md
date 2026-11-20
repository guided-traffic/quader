# Visibility levels: who reads what

Status: specified in [authorization.md](../spec/authorization.md) R-AUTHZ-003, R-AUTHZ-030 and R-AUTHZ-033 to R-AUTHZ-038 and
[identity-and-authentication.md](../spec/identity-and-authentication.md) R-AUTH-037 and R-AUTH-044 (task T-18), and built in
steps. Built: the schema with the rules of the container in the database ([00073_visibility.sql](../../internal/meta/postgres/migrations/00073_visibility.sql),
[00074_external_users.sql](../../internal/meta/postgres/migrations/00074_external_users.sql),
[00075_principal_builtins.sql](../../internal/meta/postgres/migrations/00075_principal_builtins.sql)), the contracts
([visibility.go](../../internal/authz/visibility.go): `Level`, `Part`, `Subject.Internal`), the levels in the grant index, and the removal of
the anonymous switch, the principal `authenticated` and `requireAuthentication`. The decision point reads the levels (`leveledBy` in [decide.go](../../internal/authz/decide.go), the shapes in
[visible.go](../../internal/authz/visible.go)). The restricted levels of R-AUTHZ-037 (owner requirement W-11) are built: the column of
[00077_visibility_restricted.sql](../../internal/meta/postgres/migrations/00077_visibility_restricted.sql), the rules in
[internal/visibility](../../internal/visibility/visibility.go) and the writes of [projects](../../internal/project/visibility.go) and
[repositories](../../internal/repository/visibility.go). The change that builds each remaining part names its code here.

This page answers one question: besides the members of a project and the principals a grant names, who reads a repository's
artifacts and its wiki, who decides that, and what keeps the answer from being wider than intended. The decision point, the grant
index and the denial semantics are in [permissions.md](permissions.md); projects and their roles in
[delegated-administration.md](delegated-administration.md); the internet-facing door in [bastion.md](bastion.md); further endpoints
and their exposure rules in [endpoints-and-exposure.md](endpoints-and-exposure.md).

## Who reads what

A level is `private`, `internal` or `public`. A project has one; a repository has one for its artifacts and one for its wiki
(R-AUTHZ-033). A level only adds `read`; grants, project roles and capabilities add what they add, and nothing takes away what
another source gives (R-AUTHZ-035).

The owner's rule (2026-11-20, [Q-AUTHZ-268](../spec/authorization.md#q-authz-268--every-authenticated-subject-is-internal-except-a-user-flagged-external-and-that-users-tokens)):
`internal` admits every authenticated caller, technical ones included, except a user flagged external and that user's tokens.

| Caller | `private` | `internal` | `public` |
|---|---|---|---|
| No credential (the anonymous subject), on an endpoint whose `auth.anonymous` is `true` | — | — | reads |
| No credential, on an endpoint whose `auth.anonymous` is `false` | — | — | — (401) |
| User through a UI session or a personal token, not flagged external | — | reads, within the token's scope | reads |
| User flagged external (R-AUTH-044), through its session and every one of its tokens | — | — | reads |
| Technical token of a service account of the instance, of a project or of a repository | — | reads, within the token's scope | reads |
| CI token | — | reads, within the token's scope | reads |
| Any of these through a Bastion ([Q-BAS-148](../spec/bastion.md#q-bas-148--a-request-through-a-bastion-is-decided-as-any-other)) | as at the main door | as at the main door | as at the main door |
| Bastion registration | — | — | reads (no route admits it; [Q-AUTHZ-320](../spec/authorization.md#q-authz-320--a-bastion-registration-is-not-internal-a-bastion-reads-for-the-caller-it-forwards-for)) |

"—" means: only what a grant, a project role or a capability gives. Every row is bounded by the exposure of the endpoint the
request arrived on, which binds a read by a level as it binds a read by a grant (R-EP-013). A `public` read is not narrowed by the
token scope or by the caps of a CI token, because it needs no credential at all; an invalid credential is still refused with 401 and
never read as anonymous (R-AUTH-001).

## What a leaked credential reads

Per credential, what its holder reads besides what its grants and project roles give, and through which doors. A level never gives a
write: a write is always a grant's, a role's or a capability's (R-AUTHZ-033 rule 6).

| Credential | Reads by a level | Doors | Ends |
|---|---|---|---|
| Personal access token of a user not flagged external | every `internal` and `public` artifact, and every wiki whose level is `internal` or `public`, within the token's scope | the default endpoint; every further endpoint whose `auth.token_types` admit personal tokens and whose binding the token meets; every Bastion | revocation, within `auth.revocation_bound` at the main installation and the feed's poll interval at a Bastion (R-BAS-026); setting the external flag, within the same bounds |
| Technical token of a service account (instance, project or repository) | the same as a personal token | the same, for the token type `sat` | revocation, the token's expiry, disabling the account |
| CI token | the same, within the token's stored scope | the same, for the token type `cit` | its lifetime (R-CI-016), revocation |
| Token of a user flagged external | `public` only | every door that accepts it | — |
| Bastion token | nothing: it is refused by type on every route but the Bastion interface | the interface | revocation, expiry |
| Session cookie of a user not flagged external | as a personal token, without a scope | every endpoint that accepts the session cookie; never through a Bastion, which forwards no cookie (R-BAS-003) | sign-out, session end |

Every row is bounded by the exposure rules of the endpoint the request arrives on (R-EP-013). H-884 and H-1052 below say what makes
a row real.

## Who decides

| Decision | Who | Bound |
|---|---|---|
| Whether requests without a credential are admitted at all | The operator: an endpoint's `auth.anonymous` in the configuration file | The default endpoint's value is the ceiling of `public`: while it is `false` no level can be set to `public` (409 `public_not_allowed`, Q-AUTH-232) |
| The level of a project | An administrator (`projects:write`), with recent authentication | At most `public`, subject to the line above; a restricted level only with `security-settings:write` |
| The levels of a repository | Its project's Owner or `repositories:write-config`, with recent authentication to raise | Artifacts at most the project's level, wiki at most the artifacts' (422; the database refuses the state with `AI008` and a check constraint); a restricted level only with `security-settings:write` (403) |
| The installation's default and its restricted levels | `security-settings:write`, with recent authentication | Decide only later writes; the default is never restricted (a check constraint) |
| Whether a user is external | `identities:write`, with recent authentication; `newUsersExternal` for users created later | No identity-provider claim, group sync or SCIM attribute writes it |

No policy entry, role binding, project membership or owner record can name `anonymous` or `authenticated` (422
`principal_not_allowed`, refused by the database with `AI009`, Q-AUTHZ-279), so no grant can open what the levels keep closed.

**The built-in project `upstream`** ([authorization.md](../spec/authorization.md) R-AUTHZ-041, built) is stored `public` and fixed (migration 00086, AI010), so that an administrator may give a
well-known repository any level; the stored level admits nobody to the project, which is listed only for a holder of `projects:read`
and for whoever reads one of its repositories that is switched on (`projectSight` in
[projects_api.go](../../internal/api/v1/projects_api.go), Q-AUTHZ-305), first in every list of projects (Q-UI-837), and is not one of the `publicObjects`. The levels of its repositories are an administrator's
(`repositories:write-config`, `security-settings:write` for a restricted one), never an Owner's: it has none.

**Switching a registry on opens it to everyone signed in who is not external.** A preset repository is created switched off and
`internal` (the owner's Q-REPO-388; `wellknown.PresetLevel`), so the one switch of `setUpstreamRepositoryState`, held by
`repositories:write-config`, makes it readable by every signed-in subject that is not a user flagged external, people's pipelines and
service accounts included, directly and through every virtual repository that lists it: the technical tokens and CI tokens of every
project read it ([H-1052](#h-1052--a-technical-token-or-a-ci-token-reads-every-internal-repository-not-only-what-it-was-made-for)).
An administrator who wants fewer readers lowers the level before switching on. An installation that restricts `internal` to its
security administrators (R-AUTHZ-037) gets its registries `private` instead (Q-REPO-393, `levelChoice.productLevel` in
[visibility.go](../../internal/repository/visibility.go)), so the switch of a repository-admin never opens a level they may not
choose. While a registry is off it serves nothing, and a caller without `repositories:read-config` neither lists nor is suggested it,
nor sees the project because of it (Q-AUTHZ-305, Q-AUTHZ-306, `upstreamServes` in
[upstream_repositories_api.go](../../internal/api/v1/upstream_repositories_api.go)).

**Switch all on** sets `internal`, never `public`; choosing `public` asks first with the sentence the owner asked for, because a public well-known repository
lets anyone on the internet pull through the installation ([H-881](#h-881--anyone-can-make-a-public-remote-repository-fetch-from-its-upstream)
applies to it as to every public remote). Through a team's virtual repository a well-known repository is read by its own level, never
by the team's grants ([permissions.md](permissions.md#a-well-known-member-read-through-a-teams-virtual-repository)): at `internal` by every
caller of the table above that reads `internal`, the technical tokens and CI tokens of every project included
([H-1052](#h-1052--a-technical-token-or-a-ci-token-reads-every-internal-repository-not-only-what-it-was-made-for) applies to it), and not
by a user flagged external ([authorization.md](../spec/authorization.md) R-AUTHZ-030, Q-AUTHZ-302).

**What a signed-in caller learns about the well-known repositories.** Which preset repositories exist is public: `GET /api/v1/formats`
lists the presets of every format, and the keys are the same in every installation. Their state and their level are told to two kinds
of caller. An administrator with `repositories:read-config` reads the whole item, address and account without the secret included
(R-REPO-073). Whoever writes a virtual repository's member list, the holder of `repositories:write-config` and the Owner of any
project, reads the state and the level of a format's presets through `GET /api/v1/formats/{format}/upstream-repositories`
(`listFormatUpstreamRepositories` in [upstream_repositories_api.go](../../internal/api/v1/upstream_repositories_api.go), Q-API-364)
and nothing of the address, the account or which virtual repositories list it. That is no more than such a caller learns already:
listing `upstream/<key>` in a member list answers the warning `member_inactive` on the save and the member's `upstreamState` in the
resolution explanation, and the explanation names the member as `no-read` to a caller who cannot read it, which tells whether its
level admits them (R-REPO-072). A reader, a user flagged external who owns no project, a service account or a CI token without an Owner's role gets
403, and an anonymous caller 401: they learn the state of a well-known repository only by reading it once it serves or through a
virtual repository that lists it. That the presets are `internal` from their create tells them nothing more: the lists and the
suggestions leave out what does not serve (Q-AUTHZ-306), and a direct request answers `404 repository_disabled`.

## Who may open

An administrator restricts levels to administrators (R-AUTHZ-037, ADR-204): a level in `restrictedLevels` of the installation's
visibility settings is chosen only by a caller that holds `security-settings:write` for the request, within its token's scope
(Q-AUTHZ-283). Restricting `public` keeps the decision to open content to the internet with the administrators, restricting
`internal` the decision to open it to the whole organization; restricting `private` keeps content from being closed by its Owner.

| Write | A caller without `security-settings:write` |
|---|---|
| Creates a project or a repository and names a restricted level | 403 `visibility_level_restricted` |
| Creates a repository and names no level, where the level it would take is restricted | Takes the highest lower level it may choose; 403 when there is none (Q-AUTHZ-286) |
| Changes a part to a restricted level, higher or lower than the stored one | 403 |
| Names the level a project or part already has, or lowers it to a level it may choose | Allowed: an object keeps a restricted level (Q-AUTHZ-285) |
| The cascade of a lowered project, the lowering of a move, a journal replay, a restore | Not a choice; never refused (Q-AUTHZ-288) |

The check runs in the transaction of the write, on the settings read in it, after the rules that bind everyone (the container, the
anonymous door) and before the recent authentication (R-AUTHZ-038), so the answer does not depend on a cache and an Owner is not
told that an administrator could set a level nobody can. The forms read the same rules from `GET /api/v1/visibility-choices`
(R-API-076); what they show is a courtesy, the write decides. A refusal writes no audit event of its own; the change of the setting
writes `settings.visibility.changed` with both sets before and after.

## The wiki

The wiki of a repository is the documentation Quader serves or shows apart from the files: npm's `readme` members, the NuGet README
route, the README operation of the package API and the collection documentation of the Ansible format (R-AUTHZ-034). It is never
more open than the artifacts, so a caller who reads the wiki reads the package. A caller who reads the artifacts and not the wiki
gets the refusal of a read on a route that serves only wiki content and documents without the wiki members; the README tab is not
shown. One coordinate part decides it, asked through four named forms that the format kit and a document test hold every format and
operation to (R-AUTHZ-034, R-API-058, R-ARCH-010 check 15).

The README is wiki content of every format that has one. npm's full document leaves `readme` and `readmeFilename` out for a caller who
may not read the wiki of the repository that supplies the version `latest` points to; the validator and the rendition key carry the
answer, and on a virtual repository the merge names the supplying member, so a cached merged document never carries a README to
a caller without it ([npm.md](npm.md)). The NuGet README has a route of its own, declared as a wiki route, so the dispatcher refuses
a caller without the wiki before the handler runs. The package API drops `hasReadme`, the wiki members of the metadata and the README
operation's answer for such a caller, per repository that holds the version, and the Ansible documentation operations skip a repository
whose wiki the caller may not read (R-ANS-037). The shape of a repository for a Bastion is `full` only when the wiki is read as well.

## How a change reaches every replica

| Change | Path | Bound |
|---|---|---|
| A project's or a repository's level | The compiled grant index, through the change areas `authz` and `repo_config` (R-AUTHZ-028) | Typically 100 ms plus one build; at most `cluster.epoch_poll_interval` plus 1 s with or without Valkey; never above `authz.cache.max_staleness` plus one build |
| A user's external flag | The principal cache of the authentication stage, through the change area `auth` (R-AUTH-023) | `auth.revocation_bound` |
| A Bastion's kept answers | Its revocation feed: a lowered project level and a changed repository level drop every answer (`grants`), a changed flag the user's (`principal`) (R-BAS-015) | The feed's poll interval |

Lowering a project, a repository's level and setting the external flag are journaled narrowings: a restore to a point before them
applies them again, and never applies a raise (R-AUTHZ-036, R-AUTH-041).

## What this does not cover

### H-880 · The wiki level does not hide documentation shipped inside an artifact

Component: the wiki part of R-AUTHZ-034. Subject: a repository whose wiki is less open than its artifacts. Live once such a
repository holds packages that carry their README inside the artifact: an npm tarball's `README.md`, the file a `.nupkg` names, the
README and the plugin sources of an Ansible collection archive. Whoever may download the artifact reads them, whatever the wiki
level says; Quader cannot change an artifact's bytes without breaking every digest a client verifies. What makes it real: a README or
documentation that is meant to stay inside while the package is public.

### H-881 · Anyone can make a public remote repository fetch from its upstream

Component: the populate check of R-AUTHZ-008 for a read by a level (R-AUTHZ-035 rule 6). Subject: remote repositories whose artifacts
are `public` and whose `cacheFillAction` is `read`. Dormant until such a repository exists; the write that creates the state answers
with the warning `public_populates_remote`. Then every caller on the network can make Quader download arbitrary upstream packages:
storage growth, upstream rate limits, possibly upstream cost. The installation-wide fetch budget of R-REPO-040 bounds concurrency,
not volume. What closes it: `cacheFillAction: deploy` on public remote repositories.

### H-882 · A user is internal until an administrator flags it external

Component: the external flag of R-AUTH-044. Subject: users of an identity provider that also signs in external people. Live while
`newUsersExternal` is `false` and such a person signs in for the first time: until an administrator sets the flag, the user reads
every `internal` repository. No claim of the identity provider sets the flag. What closes it: `newUsersExternal: true`, with
employees flagged internal by hand instead.

### H-883 · Lowering a level withdraws nothing a caller already holds

Component: the levels of R-AUTHZ-033. Subject: content that was readable by a level before it was lowered. Live on every lowering:
what clients downloaded stays in their caches, lockfiles and mirrors outside Quader; a Bastion keeps serving what it holds until its
feed applies the `grants` entry (R-BAS-026). What makes it real: content that was opened by mistake and read before it was closed.

### H-884 · `internal` is a property of the caller, not of the network

Component: the internal subject of R-AUTHZ-030. Subject: personal access tokens and sessions of users who are not external. Live: a
leaked personal token of an internal user reads every `internal` repository from wherever it reaches the default endpoint, a further
endpoint that accepts personal tokens, or a Bastion, which since the owner's decision of 2026-11-20 decides it as the main
installation does (Q-BAS-148): a Bastion on the internet is a door to every `internal` repository for such a token. The token's scope
narrows it (R-AUTHZ-013); revoking the token ends it within the revocation bound and, at a Bastion, the feed's poll interval. What
makes it real: a leaked personal token and a door reachable from where it leaked.

### H-885 · The installation's default level is not journaled

Component: `/api/v1/settings/visibility` (R-API-056). Subject: projects and repositories created after a restore. Dormant until a
restore goes back to a point before the default was lowered: the restored default is the older, more open one, and objects created
afterwards take it until an administrator lowers it again. Nothing that existed opens: the default decides only creations.

### H-1001 · Restricting a level closes nothing that has it

Component: the restricted levels of R-AUTHZ-037 (rule 2, Q-AUTHZ-285). Subject: projects and repositories that had a level when an
administrator restricted it. Live on every restriction: a repository an Owner made `public` before `public` was restricted stays
`public`, and its Owner may keep it so; only a new choice is refused. `GET /api/v1/settings/visibility` lists what is `public`
(`publicObjects`, the first 100 of each kind), so an administrator finds what is open to the internet; for `internal` there is no such
list. What closes it: the administrator lowers the projects or the repositories (a project's lowering cascades, R-AUTHZ-033 rule 3).
What makes it real: content opened by an Owner before the restriction that the administrator meant to close with it.

### H-1002 · The restricted levels are not journaled

Component: `restrictedLevels` of `/api/v1/settings/visibility` (R-API-075). Subject: the choices of levels after a restore. Dormant
until a restore goes back to a point before a level was restricted: the restored settings restrict less, and until an administrator
restricts the level again, Owners may choose it. Nothing opens by the restore itself; what an Owner opens in between stays open
(H-1001).

### H-1052 · A technical token or a CI token reads every `internal` repository, not only what it was made for

Component: the internal subject of R-AUTHZ-030 ([Q-AUTHZ-268](../spec/authorization.md#q-authz-268--every-authenticated-subject-is-internal-except-a-user-flagged-external-and-that-users-tokens)).
Subject: the technical tokens of service accounts of every level and CI tokens. Live on every installation with an `internal`
repository since the owner's decision of 2026-11-20. A service account is created for one purpose - a monitoring check, a project's
partner, a repository's deployer - and its token is the credential most often stored outside Quader (a CI variable, a partner's
system); it now reads every `internal` repository of every project, and through every door that accepts its type, a Bastion included.
A CI mapping makes every job it matches internal:

- On a CI issuer with `public_issuer: true` the pinning rule of R-CI-005 bounds which projects match; a mapping pinned to a namespace
  makes every project of it read everything `internal`.
- A mapping with `cross_project_source: read_only` (R-CI-012) gives fork code running in the parent pipeline a token that reads every
  `internal` repository, where it read only the mapping's capped grants before.
- On a GitLab older than 18.4, a fork's pipeline that matches a mapping without a pin on the parent gets such a token as well.

What bounds it: the token's scope (a technical token issued with a scope naming its repositories reads by `internal` only there); the
endpoint binding of a token and the exposure rules of an endpoint; a CI token's lifetime; revocation. A CI job cannot narrow its token
to an `internal` repository that no grant of its mappings names: the exchange refuses such a scope item (`scope_not_granted`,
R-CI-007), so a job that wants a narrow token reads by an unnarrowed one or by a grant. What makes it real: a leaked technical or CI
token, or a fork pipeline under a mapping with the `read_only` opt-in, on an installation that keeps content `internal` that such a
caller should not read; that content belongs `private`, granted to the groups of the people who read it.
