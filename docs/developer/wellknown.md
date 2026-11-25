# The built-in project `upstream`

Every installation has the project `upstream`, which holds the product's well-known repositories: one remote repository per upstream
preset of the format catalog and the system repositories of Quader's own work, today `scanner-trivy` (a remote) and `clamav` (a
local repository of the format `clamav` that may serve clients, [R-REPO-075](../spec/repositories-and-proxy.md#r-repo-075--clamav-clamavs-definitions-for-the-malware-scanning-and-switched-on-for-the-companys-clamav-installations))
([repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-066 to R-REPO-074, [authorization.md](../spec/authorization.md)
R-AUTHZ-041, [ADR-209](../decisions.md#adr-209--the-built-in-project-upstream-holds-the-products-well-known-repositories-and-a-virtual-repository-may-list-them),
[ADR-211](../decisions.md#adr-211--scanner-trivy-is-a-system-repository-of-upstream-that-only-the-image-scanning-reads)). The security
side is in [security/delegated-administration.md](../security/delegated-administration.md), [security/egress.md](../security/egress.md),
[security/visibility.md](../security/visibility.md) and [security/data-and-secret-flow.md](../security/data-and-secret-flow.md); the
administrator's view in the manual's [Upstream registries](../manual/admin/upstream-registries.md).

## What is built and what is not

| Part | State |
|---|---|
| Names, kinds, states, `Declared`, the reconcile's plan ([internal/wellknown](../../internal/wellknown/)) | built |
| Catalog rules for preset ids and `UpstreamPreset.Settings`; the Debian and Ubuntu archive keys ([deb.md](deb.md#the-archive-keys-of-the-presets)) | built |
| Migration [00086_upstream.sql](../../internal/meta/postgres/migrations/00086_upstream.sql): the project row, `upstream_repository`, the guards, the purge function | built |
| The reconcile in `quader migrate` ([migrate_declared.go](../../internal/app/migrate_declared.go)) and the purge job `upstream-purge` | built |
| `Repository.Upstream` in every read ([repository/wellknown.go](../../internal/repository/wellknown.go)) | built |
| The ten operations of R-API-083 ([upstream_repositories_api.go](../../internal/api/v1/upstream_repositories_api.go), [upstream_ops.go](../../internal/repository/upstream_ops.go)) | built |
| The member picker's read `listFormatUpstreamRepositories` (Q-API-364) | built |
| The refusals of R-REPO-069 and R-AUTHZ-041 ([upstream_managed.go](../../internal/repository/upstream_managed.go), [project/upstream.go](../../internal/project/upstream.go) and one check in each writer of policies, service accounts and CI mappings) | built; batch writes and quick setup are not built, so they have nothing to refuse |
| The project's visibility and its place first in the list of projects: listed for `projects:read` and for a caller that reads one of its serving repositories (Q-AUTHZ-301, Q-AUTHZ-305, Q-UI-837; `projectSight`, `projectOrder` in [projects_api.go](../../internal/api/v1/projects_api.go)); not in `publicObjects` | built |
| Its repositories in the list of repositories and the search's suggestions: to a caller without `repositories:read-config` only while they serve (Q-AUTHZ-306; `upstreamServes` in [upstream_repositories_api.go](../../internal/api/v1/upstream_repositories_api.go), used by `readsUpstream`, `visibleRepositories` and `servingUpstream` of the suggestions) | built |
| `scanner-trivy`: its declaration, the 404 of every client request, its fixed members | built |
| `clamav`: its declaration with the class `local` and `Clients`, the reconcile of a local repository (its level is no fixed member, Q-REPO-414), its item (`fixed` with `address` and `account`, `visibility` while its clients are off; `clients`), the refusals of its address and account, its "in use" (`Options.Upstream.ClamavInUse`, from `malware.enabled` and `malware.database.repository`), the switch `SetUpstreamClients` with the journal part `clientsOff`, `UpstreamFacts.Closed`, the refused hand delete ([clamav.md](clamav.md)) | built (Q-REPO-411 to Q-REPO-416, Q-REPO-420) |
| `scanner-trivy` "in use" (`Options.Upstream.ScannerTrivyInUse`) | built: in use while the image scanning is on and `vulnerabilities.images.database.repository` names it (`config.Vulnerabilities.ReadsThroughScannerTrivy`, wired in [app.go](../../internal/app/app.go); [vulnerability-scanning.md](../spec/vulnerability-scanning.md) R-VULN-044) |
| The cleanup of R-REPO-046 that applies `scanner-trivy`'s `unusedRetentionHours: 48` | not built: the value is stored and has no effect (Q-REPO-392) |
| The warning `member_inactive` (`repository.WarningMemberInactive`) | raised by a write of a virtual repository that lists a member that is not active ([virtual.md](virtual.md#members-of-the-project-upstream-and-placeholders)) |
| Members of `upstream` in virtual repositories and placeholders (R-REPO-071, R-REPO-072): `upstream/<key>` in the member list and the pin rules, the validation, the placeholder in routing, the 404's sentence, the explanation, the move, the journal ([virtual.md](virtual.md#members-of-the-project-upstream-and-placeholders)) | built |
| The usage of a member of `upstream` through a team's virtual repository (Q-LIM-63, migration 00087) | built ([usage-and-limits.md](usage-and-limits.md#the-usage-of-a-well-known-member-through-a-teams-virtual-repository)) |
| The admin page and the project's screens (R-UI-187 to R-UI-191) | another package |

## The package `internal/wellknown`

A leaf ([layers.go](../../internal/archtest/layers.go)): the catalog, `qname` and the standard library. The repository model, the project
service, the authorization engine and the edge read one set of names from it.

| Name | What it is |
|---|---|
| `ProjectKey`, `ProjectID`, `ProjectDisplayName`, `ProjectDescription`, `IsProject` | the project row of migration 00086, fixed |
| `ScannerTrivy`, `ScannerTrivyKey`, `ScannerTrivyImage`, `ScannerTrivyURL`, `ScannerTrivyRetentionHours` | `upstream/scanner-trivy`, the image `aquasec/trivy-db` it admits, its upstream `https://mirror.gcr.io/`, its cache period of 48 hours |
| `PathSwitchOnAll`, `PathSetVisibility`, `ReservedKeys()` | the keys of the project no preset takes |
| `Kind` (`preset`, `system`), `State` (`active`, `switched-off`, `not-switched-on`, `retired`), `StateOf` | the column `upstream_repository.kind` and the state derived from it, `disabled`, `first_switched_on_at` and `retired_at` (Q-REPO-375); a system repository has no state |
| `Clamav`, `ClamavKey`, `ClamavDisplayName` | `upstream/clamav`, ClamAV's definitions, "ClamAV definitions" |
| `Declaration`, `Declared(*catalog.Registry)`, `Lookup` | the repositories the product declares: one per preset, in catalog order (formats by ID, presets in the format's order), then `scanner-trivy` while `oci` is registered and `clamav` while `clamav` is; `Class` is `remote` for a preset and the system repository's own otherwise (`Local()`); `Clients` marks the one declaration whose clients an administrator may switch on; `Address()` and `Fixed()` are what the product holds of each; the plan refuses a stored repository of another class (step 6) |
| `Store`, `Stored`, `Plan`, `Reconcile`, `Result` ([reconcile.go](../../internal/wellknown/reconcile.go)) | the reconcile of R-REPO-067: `Plan` decides the steps without a database, `Reconcile` applies them through a `Store` |

`catalog.ReservedPresetIDs()` repeats the reserved repository keys of R-REPO-002 and `wellknown.ReservedKeys()`, because the catalog is a
leaf below both; `TestReservedKeysAreTheCatalogs` holds the three lists together. A preset id is unique across the formats of a
registry (`Registry.Register` refuses a second), a repository key, and not reserved (Q-REPO-387). A preset taken out of a format goes
into `retiredPresetIDs` of [wellknown_test.go](../../internal/wellknown/wellknown_test.go) in the same change, so no later preset takes
its key.

## The reconcile

`app.DeclaredState` ends with `ReconcileUpstream` ([migrate_declared.go](../../internal/app/migrate_declared.go)): after the role bindings
and the CI issuers, in one transaction of the migration session, a repository service over the session (the actor `system:catalog`),
the change log, the cache invalidation of a remote whose upstream moved, and a job queue that only enqueues. `wellknown.Reconcile` reads
the project's rows through `Service.UpstreamStore(tx)` ([upstream_store.go](../../internal/repository/upstream_store.go)), plans and writes:

| Step | `Plan` | `Store` |
|---|---|---|
| 1 missing | `OpCreate` | `Create`: the document a create of key, class and format stores, with the address, the display name as description, off for a preset, the fixed members of a system repository, both levels at `Declaration.Level` (`PresetLevel`, `internal`, for a preset, Q-REPO-388; `private` for a system repository); the row of `upstream_repository` |
| 2 the product's address differs and no administrator overrode it; a system repository's fixed members differ | `OpWrite` with `Write{Address, Fixed, RemoveAccount}` | `Write`: one update with `reason: catalog`; the account goes when the hosts that receive it moved (`CredentialHosts`), with `accountRemoved` and `account_removed_at` |
| 3 overridden, and the product's address caught up | `OpClearOverride` | `SetOverridden(false)`, no event |
| 4 retired and declared again | `OpUnretire` | `Unretire`: `retired_at` cleared, the repository stays off |
| 5 stored and no longer declared | `OpRetire` | `Retire`: `retired_at`, `repository.disabled` with `reason: retired` (journaled), the job `upstream-purge` |
| 6 a declared key of another format, class or kind | an error naming the key | nothing; `quader migrate` exits 1 |

The level of the create goes through the levels' rules of every create (`resolveLevels` in [visibility.go](../../internal/repository/visibility.go)):
`Caller.product.level` makes `levelChoice.productLevel` the level an absent member takes instead of the installation's default, lowered to the
highest level a non-administrator may choose, and `private` where there is none, so a restricted `internal` gives `private` and the reconcile
never fails on a restriction; a level the declaration names is no choice (Q-REPO-393). The reconcile writes no level after the create.

What the reconcile compares is the address and a system repository's fixed members (Q-REPO-390); every other member the product holds is kept by
the refusals of R-REPO-069. A run against the catalog it wrote writes nothing and records nothing. The tests run the same against a catalog of their
own: `app.UpstreamStep(cfg, cat, …)` is the step with the writers `DeclaredState` builds.

**The purge** (`RunUpstreamPurgeJob`, Q-REPO-376, Q-REPO-391): the items in batches as the deletion job removes them, then
`upstream_repository_purge(id)` of migration 00086 (replaced by 00093 and 00096) removes, in one transaction, the rows of every table whose foreign key to
`repositories` cascades and that holds content (the format's rows, the cache's entries and mappings, the usage, the findings, the signature
attachments); configuration stays (secrets, grants, rules,
limits, claims, the member rows of the virtual repositories that keep it as a placeholder), and so do the repository row and its
`upstream_repository` row. `TestR_REPO_067_ThePurgeOfARetiredRepository` holds the function's list to the schema's foreign keys: a migration that
adds such a table fails it until the table is purged or kept by a decision.

## The rows and the guards

`upstream_repository` has one row per repository of the project; the repository row keeps the configuration as for every repository.
`repository.Repository.Upstream` is read from it by the snapshot (`readSnapshot`), the lookup of a key the snapshot lacks and the
management reads (`Service.stored`, `Service.List`), and is nil for every other project. A write of the row that changes a state
(`retired_at`) notes `repo_config` in its transaction, so the snapshots follow; `first_switched_on_at` is written in the transaction of the
switch-on, which notes it already.

The guards of migration 00086 answer SQLSTATE `AI010` whatever the API checked:

| Guard | Holds |
|---|---|
| `project_upstream_fixed` | the project row is never deleted; its display name, description and level never change |
| `project_member_upstream`, `authz_policy_upstream`, `service_account_upstream`, `ci_mappings_upstream` | no member, policy, service account (of the project or of a repository in it) or CI mapping of the project |
| `repositories_upstream_declared` (deferred) | a repository of the project commits only with its row in `upstream_repository` |
| `upstream_repository_project` | a row of `upstream_repository` names a repository of the project |
| `repositories_upstream_fixed` | a repository of the project keeps its project, key, class and format; none moves into it |
| `upstream_repository_purge` | purges only a retired repository |

The foreign key `ON DELETE RESTRICT` keeps a well-known repository's row. The same-project guard of the member tables
(`virtual_same_project_guard`, `AI006`) admits a repository of the project in `virtual_members` and `virtual_pin_members` and keeps
`virtual_deploy_targets` to the own project; the move guard (`repository_move_guard`, `AI007`) lets a virtual repository move with its
members of `upstream` (Q-REPO-389). `upstream_repository` is exported with the class `Data`; the restore truncates the seeded project
row with the rest of `project` and loads the backup's.

**Fixture rule.** A test that writes a repository of the project straight into the database inserts its `upstream_repository` row in the same
transaction (a CTE), or the deferred guard refuses the commit.

## The operations and the refusals

The operations of R-API-083 are methods of the repository service in [upstream_ops.go](../../internal/repository/upstream_ops.go): `UpstreamItems`,
`UpstreamItem`, `SetUpstreamState`, `SwitchOnAllUpstreamTx`, `SetUpstreamVisibility`, `SetAllUpstreamVisibilityTx`, `SetUpstreamAddress`,
`ResetUpstreamAddress`, `SetUpstreamAccount`, `RemoveUpstreamAccount`. Each writes through `updateTx`, the update of every repository, as a write
of the product (`Caller.product`): the managed-member check does not apply, the levels' rules, the secrets, the narrowing and the events do. The
reason (`switched_on`, `switched_off`) and a bulk ID go into `repository.updated` and `repository.disabled`; switching off adds `listedBy` and its
count. The bulk operations run in the transaction of their `Idempotency-Key` (Q-API-361). The address and the account keep the account to its hosts
for every caller (`secret_value_required` at `/account/password`), set `address_overridden` by comparing the stored address with the
declaration's, and clear `account_removed_at` when an account is set; `UpstreamPointers` maps the refusals to the members of the body. The
account takes a value only (`valueOnly`, Q-REPO-487).

**An account from the configuration** (R-REPO-077, [upstream_accounts.go](../../internal/repository/upstream_accounts.go)). `quader migrate`
runs `applyUpstreamAccounts` ([migrate_declared.go](../../internal/app/migrate_declared.go)) after the reconcile: `ApplyUpstreamAccounts` checks
every declared key first (a remote repository of the project, or the run fails naming it), then writes each declared repository whose row is
not marked or that still stores an account, and each marked one whose key is gone, through `updateTx` with `Caller.product.account` set: the
write raises `config_version`, records `account: {source, authMode}` and calls `OnUpstreamChanged` even where the document stays. The mark is
`upstream_repository.account_configured_at` (migration 00125). At use the replica's own `ConfiguredAccounts` decide: the item
(`source: configuration` and the files' state, read with `Options.ReadFile`), the refusals (`declaredAccount`: the replica's entry or the
mark), and the client of the upstream (`upstream.Options.Accounts`).

The list's order (`upstreamRank`): the preset repositories in catalog order, a retired one after them by key, the system repositories last.
The list reads three queries whatever the number of repositories: the rows, `upstreamFactsByID` and `listedByCounts`, whose one statement
(`CountUpstreamListedBy`) counts the listing virtual repositories and their projects (`listedBy`, `listedByProjects`). Every other answer is
built by `upstreamItemTx` and adds `listedByNames` from `UpstreamListedByNames`, the query and the bound of 100 that `repository.disabled`
uses (Q-API-363).

`UpstreamSummaries` is the member picker's read (`GET /api/v1/formats/{format}/upstream-repositories`, Q-API-364): the preset repositories
of one format that are not retired, in the list's order, with the key, the display name, the state and the level and nothing else. Its
declaration, `{permission: repositories:write-config, projectRole: owner}` without `project`, admits the holder of the capability and the
Owner of any project (Q-API-355); the handler answers each of them the same.

The refusals: `createTx` refuses every create in the project but the reconcile's; `Replace` (PUT) and `DeleteTx` refuse every well-known
repository; `managedMembers` reports `managed_by_product` at each member a merge patch changes and may not (the narrowing settings pass: `adminNotes`,
the patterns, `vulnerabilities`, `remote.cacheFillAction`, `remote.availability.mode`; a system repository's patterns and scanning are fixed). The
project service refuses replace, delete, level and members of the project and a move from or into it; the policy, service-account and CI-mapping
writers refuse the project. `scanner-trivy`'s `unusedRetentionHours`, which the validation refuses on every other repository until the cleanup is
built, passes where it is the product's (`dropProductHeld`).

**`scanner-trivy` and the clients.** `Dispatcher.resolve` ([dispatcher.go](../../internal/api/dispatch/dispatcher.go)) and the strict lookup of
`/files/` ([files.go](../../internal/files/files.go)) answer a system repository `404 repository_not_found` as one that does not exist, after the
decision, so the answer is the same for every caller; `/v2/_catalog` lists nothing of it. The management API reads its document and its items.

## Tests

| Test | Tier | Time | Data |
|---|---|---|---|
| `TestR_REPO_066_…`, `TestReservedKeysAreTheCatalogs`, `TestR_REPO_068_TheStateIsDerivedFromWhatIsStored`, `TestR_REPO_067_ThePlanOfTheReconcile` ([internal/wellknown](../../internal/wellknown/)) | unit | under 1 s | none |
| `TestR_REPO_062_APresetsIDIsARepositoryKeyOfTheProjectUpstream` ([catalog_test.go](../../internal/formats/catalog/catalog_test.go)) | unit | under 1 s | none |
| `TestR_REPO_062_TheArchiveKeys` ([archivekeys_test.go](../../internal/formats/deb/archivekeys_test.go)) | unit | under 1 s | the keys in the binary |
| `TestR_REPO_068_ARepositoryOfUpstreamCarriesItsKindAndState`, `TestR_REPO_069_AMergePatchOfAWellKnownRepositoryChangesOnlyTheNarrowingSettings`, `TestR_REPO_062_ARemoteOfAPresetsURLTakesTheSettingsThePresetCarries`, `TestR_REPO_070_TheCachePeriodOfScannerTrivyIsTheProducts`, `TestR_REPO_073_TheRefusalsOfTheAddressNameTheMembersOfTheBody` ([internal/repository](../../internal/repository/)) | unit | under 1 s | none |
| `TestR_REPO_070_ASystemRepositoryOfUpstreamAnswersNoClient` (dispatch), `TestR_REPO_070_TheFilesOfASystemRepositoryServeNoClient` (files), `TestR_REPO_070_ScannerTrivyIsInUseWhileTheImageScanningReadsThroughIt`, `TestR_AUTHZ_041_TheProjectUpstreamIsSeenThroughItsRepositories` (api/v1), `TestR_AUTHZ_041_TheProjectUpstreamHasNoServiceAccounts` (principal), `TestR_AUTHZ_041_TheProjectUpstreamHasNoCIMappings` (ci) | unit | under 1 s each | none |
| [placeholder_test.go](../../internal/repository/placeholder_test.go): the document rules, `checkMember`, the placeholders of a route, the sentences (R-REPO-071, R-REPO-072) | unit | under 1 s | none |
| `TestR_AUTHZ_041_AMemberOfUpstreamIsDecidedOnItsLevelThroughATeamsVirtualRepository` and `TestR_AUTHZ_009_TheGrantIndexFollowsAMemberOfUpstream` ([upstream_rows_test.go](../../internal/authz/upstream_rows_test.go): the rows, a token scoped to the virtual repository, the anonymous rule 6, a listing and a Bastion's reach through it), `TestR_AUTHZ_041_ATeamsVirtualRepositoryReadsAMemberOfUpstreamByItsLevel` and the R-REPO-072 tests ([internal/virtual/upstream_test.go](../../internal/virtual/upstream_test.go)) | unit | under 1 s | none |
| `TestR_REPO_072_…` in the error writers of npm, NuGet, OCI, Ansible Galaxy, Debian and generic (`errors_test.go`) | unit | milliseconds each | none |
| `TestR_REPO_071_R_REPO_072_ATeamResolvesThroughAPlaceholderAndThroughTheMemberOnceItIsOn` ([virtual_members_test.go](../../test/integration/upstream/virtual_members_test.go)) | integration, area `upstream` (shard b) | about 3 s | documents and one tarball of the test corpus, about 1 KiB each |
| [test/integration/upstream](../../test/integration/upstream/): `TestR_REPO_067_Reconcile`, `TestR_REPO_067_ThePurgeOfARetiredRepository`, `TestR_REPO_073_TheOperationsOfTheWellKnownRepositories`, `TestR_REPO_069_TheProductKeepsTheProjectUpstream` | integration, shard b | about 6 s (four databases, two replicas in two tests) | rows only; secrets are references, no upstream is contacted |
| `TestR_LIM_027_AWellKnownMemberServedThroughATeamsVirtualRepositoryRecordsTheVirtualRepository` ([via_test.go](../../test/integration/limits/via_test.go)) | integration, area `limits` (shard b) | about 3 s | one tarball of about 1 KiB |

The area's `main_test.go` and the helpers of [fixture_test.go](../../test/integration/upstream/fixture_test.go) serve the reconcile, the
operations and the refusals; the cases of R-REPO-071 and R-REPO-072 have their own installation and helpers, named `virt…`, in
[virtual_env_test.go](../../test/integration/upstream/virtual_env_test.go).
