# Projects: members, roles and delegated administration

Status: the project model of [R-AUTHZ-026](../spec/authorization.md#r-authz-026--projects-for-delegated-administration)
(Q-AUTHZ-206 to Q-AUTHZ-209, Q-AUTHZ-214 to Q-AUTHZ-217) as the authorization engine and the project service build it: members with
one of four roles, the role in the decision path, project policies named within their project, the last-Owner rule and the move of a
repository between projects ([R-REPO-060](../spec/repositories-and-proxy.md#r-repo-060--move-a-repository-to-another-project)). The
code is [internal/authz](../../internal/authz) (`project.go`, `project_role.go`, `decide.go`, `index.go`) and
[internal/project](../../internal/project) (`members.go`, `move.go`); [developer/projects.md](../developer/projects.md) is the contract.
The management API's declarations that admit members are the API's ([permissions.md](permissions.md)).

This page says what a member can reach through its role, what bounds it, what a stolen credential of one gains, and what keeps a
project's objects inside it. It does not repeat the model of permissions ([permissions.md](permissions.md)) or of tokens
([tokens-and-credentials.md](tokens-and-credentials.md)).

## Roles and trust boundaries

| Party | Trusted with | Not trusted with |
|---|---|---|
| Administrator of Quader (`admin`; `projects:write`, `repositories:write-config`) | Creating and deleting projects, the members of every project (the first Owner among them), removing the last Owner, moving any repository | — |
| Owner (user, or member of a group with the role) | Every action on every repository of the project; the project's members, policies, service accounts, CI mappings, settings; moving a repository to another project it owns | Anything outside the project; system roles; global policies, users and groups; `anonymous` and `authenticated` grants; removing or demoting the last Owner |
| Maintainer | Every action on every repository of the project (`manage` included) | The administration of the project |
| Developer | `read`, `annotate`, `deploy` on every repository of the project | `overwrite`, `delete`, `manage`; the administration |
| Reader | `read` on every repository of the project, reading the project and its members | Every write |
| Project service account, CI mapping | What the project's policies grant it | Every role: it is never a member |
| Repository service account | The actions stored with it, on its own repository ([permissions.md](permissions.md#account-grants)) | Every role, every policy, every other repository |
| `auditor`, `security-admin`, `repository-admin` (`projects:read`) | Reading every project and its members | Changing one |

```
 administrator of Quader ── creates, names the first Owner ──▶ project ◀── members: users and groups with a role
                                                               │          (reader < developer < maintainer < owner)
                                                               ├── repositories (key unique in the project)
                                                               │     └── repository service accounts
                                                               │           (<project>/<repository>/<name>, actions here only)
   global policies ─────────────── union ─────────────────────┤
                                                               ├── project policies (<project>/<name>) ──▶ users, groups,
                                                               │        project service accounts, CI mappings
                                                               └── project service accounts (<project>/<name>)
```

## What a role grants, and through which credential

- **The role is decided, not stored as a grant.** The index keeps every membership row by principal
  ([index.go](../../internal/authz/index.go), `addProjects`) and by project. A decision reads the subject's role in the one project of
  the repository it decides, the highest over its user ID and its group IDs ([project.go](../../internal/authz/project.go), `roleOf`;
  [decide.go](../../internal/authz/decide.go), `evalState.roleIn` in `grantedBy`). The role's actions join the policy grants as one more
  source, `role:<projectKey>:<role>`, so the refusal and audit of a decision are the same as for a grant. On a virtual repository a role
  has the effect of `read` and `manage` only, as a grant does (R-AUTHZ-009).
- **Credentials.** A role reaches a user's UI session and personal access tokens; a token's scope is the ceiling over it as over a
  grant ([Q-AUTHZ-215](../spec/authorization.md#q-authz-215--a-members-role-acts-through-its-session-and-personal-tokens-within-their-scope-and-service-accounts-are-not-members)).
  The administration of an Owner needs a session or a token with `admin: true` (`mayAdminister`); through any other credential an Owner
  holds what a Maintainer holds, which is what `RoleOn` and `MembershipsOn` answer. A leaked everyday token of an Owner reads and writes
  content within its scope and changes no member, policy or setting.
- **What a token sees of its projects.** The scope also bounds the role outside the content: `MembershipsOn` and `RoleOn` answer a
  token only for the projects its scope reaches (`scopeReachesProject` in [project.go](../../internal/authz/project.go)): every
  membership for no scope (a session), `inherit`, `admin: true` or the system item `projects:read`; otherwise the projects one of its
  repository elements names (a key of the project, a pattern whose project glob matches it, or a class, which names every project),
  whatever the element's actions. The project list, `GET /projects/{projectKey}`, its members, `/authz/me` and the 404 of a hidden
  project all read these memberships, so a token scoped to one repository's `deploy` does not read its owner's other projects and their
  members; to them it is no member, so their routes answer 404 (`TestQ_AUTHZ_215_AMembershipReachesATokenWithinItsScope`).
- **Who is never a member.** A service account, a CI mapping, a Bastion registration and `anonymous` carry no role
  (`carriesRoles`). The member write refuses every principal but a user or a group, and an auto-join group, with 422
  `principal_not_allowed` ([members.go](../../internal/project/members.go)); the table refuses a principal of another kind
  (trigger `project_member_kind`, SQLSTATE `AI001`); the index skips a row of another kind and a group that became auto-join after it
  was made a member, so neither grants (fail closed).
- **Ownership.** A `deploy` through a role makes the member through which the role came the owner of a new name: the group whose
  membership carries the role, or the user for its own membership ([ownership.go](../../internal/authz/ownership.go), `roleClaimers`,
  R-AUTHZ-018 rule 2). A role is never an owner identity of its own, so a member who leaves the project keeps owning what it published
  through its own membership.
- **Cost.** A decision reads a map entry of the repository's project per user and group ID and allocates nothing for the role; nothing
  depends on the number of projects of the installation or of the subject's memberships, and nothing is queried; a listing decides every
  key on one state derived from the subject (`ReadableOn`) (`TestR_AUTHZ_015_ARoleDecisionDoesNotGrowWithTheProjects`,
  `TestR_AUTHZ_015_ARoleDecisionDoesNotGrowWithTheMemberships`, `TestR_AUTHZ_015_AListingDerivesTheSubjectOnce`). Only `MembershipsOn`,
  which lists them, walks the subject's memberships (`projectRoles`).

## The members and the last Owner

- The writes run under the project's row lock ([members.go](../../internal/project/members.go), `lockProject`). A write that would take
  the role `owner` from the last membership with it (a group counts as one, whatever its members) answers 409 `last_owner` unless the
  caller holds `projects:write` ([Q-AUTHZ-217](../spec/authorization.md#q-authz-217--the-last-owner-of-a-project-is-removed-or-demoted-only-by-an-administrator-of-quader)).
  Two Owners who remove each other at once run one after the other: one removal commits and one Owner remains. The second request
  is refused as what its caller then is: 409 `last_owner` while the index it is decided on still holds the caller's role, the 404
  of a non-member once its own removal is in force there. The member write does not read the caller's role again under the lock,
  like every write decided on its request's index (R-AUTHZ-028).
- Every member write notifies the change area `authz` in its transaction, so the role takes effect or ends on every replica within the
  bound of R-AUTHZ-028. An addition records `authz.project.member_added`; a change and a removal record `member_changed` and
  `member_removed` with the journal entry of R-AUTHZ-031 (the project and the principal by ID, the roles of a change), so a restore
  replays no role above the one in force. The same role written again changes nothing and records nothing (Q-AUTHZ-193).
- A group that is an auto-join group is never a member, in either order, because every user is in it and the membership would make
  every user a member. The member write reads the flag under a shared lock of the group's row and answers 422 `principal_not_allowed`
  ([members.go](../../internal/project/members.go), `memberPrincipal`, query `ProjectGroupAutoJoinShare`). Turning the flag on takes the
  same row `FOR UPDATE` and then looks for the group's project memberships; a group that is a member of a project answers 409
  `auto_join_member_conflict` and names the projects ([groups.go](../../internal/auth/principal/groups.go), `UpdateGroup`, query
  `IdentityGroupProjects`). The two checks run one after the other, so neither can pass on a state the other is changing.

## What an Owner reads of the installation's people

An Owner names its members and the principals of its policies in the shared principal field of the UI (R-UI-150), which searches
the project's lookup `GET /api/v1/projects/{projectKey}/principals` ([Q-AUTHZ-182](../spec/authorization.md#q-authz-182--how-a-project-administrator-finds-the-users-and-groups-it-names),
R-API-077, [lookup.go](../../internal/project/lookup.go), the `ProjectLookup*` statements of
[project.sql](../../internal/meta/postgres/queries/project.sql)). This lets every Owner of every project read, by prefix, the
username and the display name of every active user of the installation and the name of every active group; that is the gap
[H-392](../spec/authorization.md#h-392--a-project-administrator-can-enumerate-user-and-group-names), widened on 2026-11-18 by the
display name and the avatar so that the field shows a person's row. The bounds:

- **Who.** An Owner of the addressed project through a credential that administers it (a session, a token with `admin: true`), or a
  holder of `identities:read`. A Reader, a Developer and a Maintainer get 403, a caller who is no member 404, an anonymous caller 401.
  An internal user who owns no project has no person search at all; the UI never falls back to another read (Q-UI-581).
- **What leaves.** The kind, the name, the reference, and for a user its display name, its principal ID (the seed of its generated
  avatar) and the path of its uploaded avatar; for a service account its project. Never the e-mail address, the status, the origin,
  a sign-in time or a membership: those stay with `identities:read` and `GET /api/v1/users`. The avatar's path names a picture that
  every authenticated caller may read anyway ([user-avatars.md](user-avatars.md)).
- **What is never offered.** A disabled principal, the break-glass account and an auto-join group. Service accounts are the project's
  own, which the Owner lists anyway; the instance-wide ones only to a holder of `identities:read`, which lists them anyway.
- **How much.** A prefix of 2 characters or more, compared as the start of the name or of one of its words and never as a pattern, at
  most 20 results per call, at most four statements per call, and the general rate limit of the `read` class (R-API-016).

Outside a project, the fields of the admin area search through the suggestions of the search field (`GET /api/v1/search/suggestions`
with the kinds `user`, `group` and `service_account`), which answer only to a holder of `identities:read` ([search.md](search.md)); a
caller there reads nothing it could not list before.

## What keeps a project's objects inside it

- **Selectors.** A project policy names repositories by bare key, key pattern or class, read in the project's context
  ([selector.go](../../internal/authz/selector.go), `qname.ParseSelector`); a key is looked up in the project only, so a repository of
  another project cannot be named at all (422 `unknown_repository`), and a selector with a `/` is 422 `invalid_pattern`. The build
  covers the project's repositories only, whatever a stored selector says, and the memoized match of a selector is keyed by the
  project (`builder.covered`). A global policy, a token scope and an exposure rule name the project in every selector; a bare key there
  is refused, never read as a selector that matches nothing ([Q-AUTHZ-216](../spec/authorization.md#q-authz-216--repository-selectors-name-the-project-in-instance-contexts-and-a-bare-key-inside-a-project)).
- **Names.** A project's policy is stored as `<projectKey>/<name>` (`StoredPolicyName`), a global one without a `/`; the database
  compares the prefix with the policy's project and keeps the project fixed (SQLSTATE `AI003`). The same name may exist in two projects
  and once instance-wide; the delete of a project removes its own policies by their stored names and never another's
  ([Q-AUTHZ-209](../spec/authorization.md#q-authz-209--a-projects-policies-and-service-accounts-are-named-within-the-project-administrators-keep-instance-wide-ones)).
  No prefix rule and no overlap check exist between keys and names.
- **Principals of a project policy.** Users, groups, service accounts of no project or of the same project, CI mappings; never
  an auto-join group or another project's service account (422 `principal_outside_project`; to an Owner
  without a policies capability another project's account answers as an unknown one, `AsProjectOwner`). The database refuses a project
  service account in a role binding, a global entry or another project's entry (SQLSTATE `AI005`), and the grants of a project policy
  in the table of global grants (`AI004`).
- **A repository's service accounts.** An Owner creates, changes, disables and deletes them under the repository's settings
  (`/projects/{projectKey}/repositories/{key}/service-accounts`, R-AUTH-046) and chooses their actions on that repository
  (`read`, `annotate`, `deploy`, `overwrite`, `delete`, `manage`). No policy, no role binding and no other repository's owner record
  takes one (422 `principal_outside_repository`, SQLSTATE `AI005`); the name is stored `<projectKey>/<repositoryKey>/<name>` and the
  database holds the prefix to the account's repository and its project (`service_account_project_guard`, `AI005`)
  ([permissions.md](permissions.md#account-grants)).

## Moving a repository

A move ([move.go](../../internal/project/move.go)) takes both projects' row locks in key order and the repository's row lock, and is
refused with 409 `repository_in_use`, the member `references` listing each object by kind and name, while anything of the source
project names the repository: a virtual repository's member list, pin rule or deployment target, a key selector of a project policy, an
inline grant of a project CI mapping that names its key; a virtual repository moves only without members and deployment target. A
shared ownership domain, which R-REPO-060 counts as well, cannot be set yet (`ownershipDomain` is 422 `not_applicable`). A
target that holds the key, also by a repository being deleted, is 409 `already_exists`. The references are read under the row locks
the writers of those objects take as well: a policy write binds its key selectors in its own transaction, the project's row and then
the repository's FOR SHARE (`bindKeySelectors` in [store.go](../../internal/authz/store.go)), and a virtual repository's write its
members FOR SHARE, so a write that names the repository runs before the move, which then sees it, or after it, when the key no longer
names it. The database holds the same rule for every writer (SQLSTATE `AI007`, migration 00024): a repository its project holds by ID
does not change its project, and a project policy's key selector names a repository of its own project. A member, pin member or deployment target of a virtual repository and a key selector of a project policy additionally lock both repositories FOR SHARE in the database (migration 00045), so the move and a write that skipped the locks above meet in either order and one of them is refused. Nothing is rewritten on the caller's behalf,
so no grant or resolution changes behind the back of the people who wrote those objects. The move notifies `authz` and `repo_config`
with the repository's ID and raises its `config_version`, so the old address answers 404 and every cache keyed by the configuration
version drops what it built, on every replica within the bound of R-AUTHZ-028; it records `repository.moved` in the source project with
the journal entry of R-AUTHZ-031, and `repository.moved_in` in the target project, so the audit of each project shows the move
([Q-REPO-233](../spec/repositories-and-proxy.md#q-repo-233--a-move-is-recorded-in-the-audit-of-both-projects)). What holds the repository by its ID outside the project (a key selector of a global policy, an exposure rule) follows it.
The repository's own service accounts move with it in the same transaction (`principal.MoveRepositoryAccountsTx`, R-REPO-060): each
is renamed to `<targetProject>/<repositoryKey>/<name>` and its project set to the target, its actions and technical tokens stay,
the principals' change area is notified (the account grant is keyed by the repository's ID, which the move's own `authz`
notification rebuilds), and `serviceaccount.updated` with `moved` is recorded; a deferred
constraint trigger (`repository_accounts_follow`, `AI005`) refuses a commit in which a repository's project changed and an account
of it did not follow. An Owner of the source project therefore loses the administration of those accounts with the repository, and
an Owner of the target gains it; the account never holds a right in the source project.
Who may move is the API's decision: `repositories:write-config`, or an Owner of both projects (403 for a member with a lower role in
the target, 404 for a caller that is no member there). The Owner's move is restricted (`checkMoveTarget` in
[repositories_api.go](../../internal/api/v1/repositories_api.go) passes `restricted`): while a key selector of a global policy names
the repository it is refused as well, with the reference `{"kind": "global_policy", "name": "<policy>"}` (`globalReferences`,
[Q-REPO-232](../spec/repositories-and-proxy.md#q-repo-232--an-owners-move-is-refused-while-a-global-policy-names-the-repository-by-key)),
so an administrator's grant never follows the repository into a project whose other Owners cannot see or remove it. The answer names
the global policy to an Owner who may not read it, and nothing of its entries. An exposure rule follows without a refusal: it is a
ceiling and grants nothing.

## Remote repositories of a project

An Owner creates the remote repositories of its project ([Q-AUTHZ-173](../spec/authorization.md#q-authz-173--a-project-administrator-creates-remote-repositories)).
The spec bounds what an Owner without `repositories:write-config` may give a remote (a secret as a value, never a reference of the
installation; the installation's outbound path; a mirror's mode and scope as stored): Q-REPO-127, Q-REPO-128, Q-REPO-142 and
Q-REPO-193. The checks belong to the repository service's write path ([delegated.go](../../internal/repository/delegated.go),
[validate_mirror.go](../../internal/repository/validate_mirror.go)) and run against the stored section in the transaction of the
write: the create, the replace and the patch of a repository and the unsaved upstream test. The management API decides who is held
to them ([repositories_api.go](../../internal/api/v1/repositories_api.go), `writeCaller`): a caller that does not hold
`repositories:write-config` and is admitted by the role Owner alone is `Restricted`, and `MayWeakenVulnerabilities` is set for a holder of
`vulnerabilities:triage` ([Q-VULN-69](../spec/vulnerability-scanning.md#q-vuln-69--weakening-the-vulnerabilities-member-needs-the-triage-capability-or-repositoriesWrite-config)). An administrator of Quader,
who holds the capability, is bound by none of the rules; the Owner's answers are 422 `secret_value_required`,
`outbound_path_restricted` and `mirror_restricted`, and 403 `vulnerability_weakening_refused`. The integration test
`TestR_AUTHZ_026_AnOwnerWritesRemoteRepositoriesWithinTheRules` ([owner_repositories_test.go](../../test/integration/projects/owner_repositories_test.go))
drives every rule over HTTP as an Owner and as an administrator.

## The built-in project `upstream`: where a project's boundary is crossed

Specified, not built (the owner's decisions of 2026-11-20, [repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-066 to
R-REPO-074, [authorization.md](../spec/authorization.md) R-AUTHZ-041). Every repository still belongs to one project,
but one project's repositories are listed by every other project's virtual repositories: the remote repositories of the well-known
public registries that the product declares in the project `upstream`.

| Who | Decides | Cannot |
|---|---|---|
| The product (the catalog of the running binary, applied by `quader migrate`) | which well-known repositories exist, their keys, their default address and archive keys, every setting an administrator does not set | switch one on, open one, store a credential |
| An administrator (`repositories:write-config`; `security-settings:write` for a restricted level) | which are on, their levels, an overriding address, an account, the narrowing settings | delete one, create one, give a team a role in the project |
| A team's Owner | whether its virtual repositories list a well-known repository, and who reads those virtual repositories | change anything of the well-known repository; keep it on by listing it |
| A team's members and machines | nothing of `upstream` | read a well-known repository that its level does not admit them to, also through their team's virtual repository: a member flagged external skips an `internal` one ([Q-AUTHZ-302](../spec/authorization.md#q-authz-302--internal-admits-every-signed-in-subject-that-is-not-external-so-a-teams-ci-job-reads-a-well-known-member-by-its-level)) |

- **The line is crossed in one direction and one place.** A virtual repository lists its own project's repositories and `upstream/<key>`,
  never another team's; the database's same-project guard (AI006) admits only the built-in project's ID for member rows, and never
  for a deployment target, so no write reaches into `upstream` through a virtual repository.
- **The project has no Owner.** No member, policy, service account or CI mapping of `upstream` can be written (AI010), so no team can
  grant itself more on a central repository than its level gives; global policies, written by administrators, are the only grants.
- **A reference protects nothing.** A team's member list never keeps a well-known repository on: switching it off records one
  `repository.disabled` naming the virtual repositories that list it, and they skip it from the next request.

## What this does not cover

The gaps of these mechanisms recorded on the spec pages are linked here; the ones only this page describes follow.

- [H-392](../spec/authorization.md#h-392--a-project-administrator-can-enumerate-user-and-group-names): the lookup of users and
  groups lists the username and the display name of every active user and the name of every active group by prefix to every Owner;
  since 2026-11-18 with the display name and the avatar's path (see [What an Owner reads of the installation's people](#what-an-owner-reads-of-the-installations-people)).
- [H-393](../spec/authorization.md#h-393--a-project-administrator-with-managerepositories-probes-the-addresses-the-outbound-rules-allow):
  the upstream test of a project's remote repository (it extends [H-282](egress.md#h-282--the-upstream-test-tells-a-holder-of-repositorieswrite-config-what-it-sees-of-an-address)).
- [H-394](../spec/authorization.md#h-394--whoever-controls-a-project-administrator-group-administers-the-project): whoever controls
  the identity provider's group holds the group's role, Owner included.
- [H-398](../spec/authorization.md#h-398--a-failed-project-delete-can-leave-the-project-without-some-of-its-policies): a project
  delete that fails after the policy deletions leaves the project with fewer policies.
- [H-406](../spec/authorization.md#h-406--the-audit-event-of-a-refused-project-administrator-does-not-name-the-project): the audit
  event of a refused declaration does not name the project.
- [H-399](../spec/authorization.md#h-399--a-global-service-account-and-a-project-key-created-at-the-same-instant-can-overlap): a global
  service account and a project key created at the same instant can overlap.
- [H-405](../spec/authorization.md#h-405--a-global-policy-that-predates-a-project-keeps-the-projects-prefix): a global policy that
  predates a project keeps the project's prefix.

### H-403 · What a project administrator still reaches of an upstream an administrator configured

Stage: a remote repository an administrator of Quader created in a project or moved into it. Subject: the upstream credentials,
references and outbound path an administrator configured for such a repository, and the partner networks they open.

- **Live, by design of the move and of the roles.** While `url` and the credential hosts stay, an Owner or a Maintainer causes fetches
  with the stored credential, the stored reference and the stored outbound path, and reads the status of the answer; an Owner reads the
  full document, which names a reference by name (`{"secretRef": {"file": "partner-token"}}`), so it learns the file names under
  `secrets_dir` and the `QUADER_CAC_` variable names its repositories use, not their content. What makes it real: an administrator who
  puts a repository into a project whose members are not meant to use its upstream credential; keep it in another project, or move the
  credential to a repository outside the project.

### H-531 · A run can be requested as often as a pass ends

Stage: the run of a mirror ([mirror_api.go](../../internal/api/v1/mirror_api.go), `enqueueMirrorPass`). Subject: the upstream a mirror
pulls from, and the installation's egress and storage, which a pass loads.

- **Live, by design of the permission.** One pass per repository runs at a time, but nothing limits how often a pass is requested: a
  holder of `manage` on the repository, which every Maintainer and Owner of its project is, and a holder of `repositories:write-config`
  can request a run the moment the last pass ends, and `{"full": true}` makes it a full discovery. Each pass respects the mirror's
  bounds, window and rate-limit waits (R-REPO-052, R-REPO-054), so the cost is upstream requests and discovery work, not unbounded
  storage. The audit event `mirror.pass_requested` names every request and its caller.
- What makes it real: a member that is not trusted with the upstream's goodwill, on an upstream that throttles or bills by request.
- What would close it: a minimum time between requests per repository.

### H-1021 · A well-known repository shows every project's fetched packages to everyone who reads it

Stage: the cache of a well-known repository of the project `upstream` (specified, not built: R-REPO-066, the list of what a remote
fetched, [api-and-ui.md](../spec/api-and-ui.md) R-API-081, and the packages view). Subject: which public packages, and which versions,
the teams of the installation pull.

- **Dormant until R-REPO-066 is built; live by design from then on.** A team's own remote shows its cache to the readers of that team's
  repository; the central repository caches what every team fetched through it, and a reader of it, which at `internal` is every
  signed-in subject that is not external, technical ones included ([Q-AUTHZ-302](../spec/authorization.md#q-authz-302--internal-admits-every-signed-in-subject-that-is-not-external-so-a-teams-ci-job-reads-a-well-known-member-by-its-level)), lists those
  packages and versions and learns what other teams build with (a vulnerable version a team still pulls,
  a package that names an unreleased product). The project's own report and its package lists show none of it, only the central
  repository's views do.
- What makes it real: a level `internal` or `public` on a central repository in an installation whose teams do not share what they
  use. A team that must not disclose its dependencies keeps a remote of its own; an administrator keeps the central repository
  `private` and grants it through global policies.

### H-1022 · A team cannot keep a central repository's state, level or address from changing under it

Stage: the operations of [repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-073 and the reconcile of R-REPO-067
(specified, not built). Subject: what a team's virtual repository resolves through a well-known member.

- **Dormant until built; live by design, the owner's decision ("Ein virtuelles Repo, das auf unser zentrales zeigt, kann keine
  Deaktivierung durch einen Admin verhindern").** An administrator switches a central repository off or makes it `private`, and every
  team's builds stop getting its packages at once; an administrator's address override or a release's new address sends every team's
  misses to another host; nothing of this changes a team's document or writes to a team's audit. The placeholder's 404 and the settings
  page say why (R-REPO-072), and the administrator's switch-off records the virtual repositories it affects.
- What makes it real: a team that depends on a central repository for a release build. A team that cannot accept it keeps a remote of
  its own beside or instead of the central one.
