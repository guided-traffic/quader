# The format contract

How a package format plugs into Quader: the contract in [internal/formats](../../internal/formats/), the registry, the `Host` a format
reaches the core through, the storage hooks, the dispatcher that serves `/api/<format>/<projectKey>/<repoKey>/…`, and how to add a format.
Requirements: [R-ARCH-005, R-ARCH-007 to R-ARCH-010, R-ARCH-014](../spec/architecture.md), [R-REPO-003](../spec/repositories-and-proxy.md),
[R-EP-018](../spec/endpoints.md), the hooks table of [storage.md](../spec/storage.md). Decisions:
[Q-ARCH-188, Q-ARCH-189, Q-ARCH-193, Q-ARCH-211 to Q-ARCH-217](../spec/architecture.md#decisions), [Q-GENERIC-7](../spec/formats/generic.md).

The conformance kit (`internal/formats/formattest`) and the resource limits and readers for untrusted content (`internal/formats/untrusted`) have
their own sections below: [Conformance kit](#conformance-kit) and [Untrusted content](#untrusted-content).

## What has to be in your head first

- **A format is a Go package that implements `formats.Format` and imports almost nothing.** The archtest allowlist
  ([imports.go](../../internal/archtest/imports.go)) lets a format import the contract, `formats/catalog`, `formats/untrusted`, `errs`,
  `config` and `observability`. Core types reach it as aliases of the contract (`formats.Action`, `formats.Path`, `formats.Tx`) and
  as fields of the request (`rq.Subject`, `rq.Index`, `rq.Repo`), so it never names `authz`, `repository` or `meta`.

**Requests for members and targets.** `rq.Repo` is the repository the call acts on; `rq.Addressed()` is the key the client addressed (a virtual repository on a request
for one of its members, else `rq.Repo.Key`) and the `Coordinate.Addressed` of every decision the Host makes about the request. A format never sets it: it derives a request
with `rq.Member(m)` (a member of `rq.Repo`; refused for a repository that is none; the audit forwarder of npm does so) or `rq.WriteTarget(t)` (a read that stands for a
write, `GET {name}?write=true`: no addressed key, so a token scope has to name the deployment target). The unexported field behind it is the only state, and the
only writer is `Member` (Q-ARCH-252, [Q-AUTHZ-165](../spec/authorization.md)).
- **A format runs no SQL.** Its generic package rows go through the `PackageWriter` the event or `Host.Write` hands it; its own side
  tables go through a data port it declares (`Descriptor.DataPort`), implemented by a domain package
  (`internal/formatdata/<id>`) and bound by the composition root ([Q-ARCH-193](../spec/architecture.md#decisions)). The `Tx` it
  receives is opaque: it cannot unwrap it.
- **`packages.rev_seq` is the writer's job, not the format's.** Every `PackageWriter` call that writes generic rows raises the revision of
  that package, once per transaction ([Q-ARCH-212](../spec/architecture.md#decisions)). A change that writes only side rows calls
  `BumpRevision`.
- **The core owns the headers, the checks and the transaction.** `formats.ServeRoute` wraps every `Download`, `Metadata`, `Computed` and
  `Volatile` route in the content headers of R-ARCH-007 (`Volatile` without `Content-Disposition`, Q-ARCH-258); the dispatcher runs the key shape, the clean path, the match, the index and the coarse
  check before a handler sees the request; `Host.Ingest().Commit` runs the hook in the item's transaction.
- **A format with no routes has no prefix.** generic's protocol is `/files/` ([Q-GENERIC-7](../spec/formats/generic.md)); `/files/` is the
  raw view of every repository and is served by the generic layer, not by the dispatcher.
- **The contract is frozen** since the end of M3 (version 1; version 2 since the project model, ADR-156; version 8 since the signatures, ADR-228; version 9 since the gate per repository, ADR-231; version 10 since the receipt attestations, ADR-230 and Q-SIG-180; version 11 since the declaration of the upstream catalogue and the version canon, ADR-277 and [remote-curation.md](remote-curation.md#c--contracts)) ([ADR-141](../decisions.md#adr-141--the-format-contract-is-frozen-at-version-1)):
  every change of its exported surface, an addition included, is an ADR, and
  [testdata/contract.txt](../../internal/formats/testdata/contract.txt) is regenerated in the same change (see [Changing the contract](#changing-the-contract)).
- **Unbound seams fail closed.** `Claim` and `AutoClaim` refuse until the composition root binds the ownership rule and the namespace auto
  claim ([Q-ARCH-215](../spec/architecture.md#decisions)); it binds both ([wire_formats.go](../../internal/app/wire_formats.go)), so a format with package
  names publishes under the rule. `AutoClaim` returns the `AutoClaimResult` that the publish's audit event records.
- **Per-replica state of a format hangs off the Host.** A format is registered once per process; a process may hold several replicas (the integration tests do).
  A format that reads `quader.yaml` or exports metrics implements `Bind(host, cfg, metrics)`, which the composition root calls with the Host it built
  (`hostBinder` in [wire_formats.go](../../internal/app/wire_formats.go)); the format keeps the result per Host, never in a package variable (npm: [bind.go](../../internal/formats/npm/bind.go)).

## Files

### `internal/formats` (the contract, edge layer)

| File | Responsibility |
|---|---|
| [formats.go](../../internal/formats/formats.go) | `ContractVersion`, the aliases and constants, `Format`, `Descriptor`, `Register` / `Validate` / `CatalogEntry`, `Lookup`, `List`, `Descriptors`, the optional capabilities (`ItemsRestorer`, `VersionPather` (the file of a version, Q-AUTHZ-255), `Capabilities`, `CapabilitiesOf`, `CapabilitiesFor`; the remote adapter's `Discoverer` and `FillInspector` are declared in remote.go) |
| [scope.go](../../internal/formats/scope.go) | The repositories a request may name: `ServeRoute` puts them in the handler's context, `RepositoryAllowed` is the check of the stores and the package writer (Q-ARCH-260) |
| [route.go](../../internal/formats/route.go) | `Route` (with `ClientDigest`, whether every client checks the route's body against a digest, R-STOR-071), `RouteKind`, `Params`, `Template`, `MatchRoutes`, `Request`, `Refusal` (`WithRefusal`, `RefusalFrom`), `EndpointFacts`, `Target`, `PathKind`, `PathInfo` |
| [host.go](../../internal/formats/host.go) | `Host` (with `PathsAllowed`, the listing form of `Authorize` for many paths, ADR-174) and its parts: `Ingestor`, `ItemStore`, `PackageStore`, `LinkBuilder`, `RenditionCache` |
| [hooks.go](../../internal/formats/hooks.go) | The storage-hook types: `RepoRef`, `ItemRef`, `CommitEvent`, `ItemsEvent`, `Inspection`, `PackageWriter`, `OwnershipWrite` |
| [remote.go](../../internal/formats/remote.go) | `RemoteAdapter`, `RemoteFetcher` and their types, among them `Exchange` and `ErrOffline` for a request outside the cache (Q-NPM-62), `Query`/`QueryResult` for the pass-through of a `Volatile` request with the engine's cached answer (`RemoteAdapter.UpstreamQuery` builds the request; Q-ARCH-270) and `Gate`, the artifact gate as a method of the fetcher (Q-ARCH-272) (the engine is internal/proxy, bridged by [internal/formathost/remote.go](../../internal/formathost/remote.go)) |
| [virtual.go](../../internal/formats/virtual.go) | `VirtualAdapter` (with `ArtifactPrefix`, Q-ARCH-272), `MemberDocument`, `Resolver` (with `Plan`, `KeepsAway` and `ReadScope` for a format that fans out over the members, Q-ARCH-271) and their types |
| [exposure.go](../../internal/formats/exposure.go) | `ExposureAdapter` (the version syntax and the dimensions), `ExposureView` (`Package`, `Version`) |
| [versions.go](../../internal/formats/versions.go) | The versions a caller's own grants let it read (Q-AUTHZ-255): `CallerVersions` (nil without path patterns), `VersionView` (`Version`, `Hidden`), `HiddenFingerprint` for the key and the validator of a rendition, `LiveVersions` (the version list of a revision, kept in the rendition cache under it) |
| [setup.go](../../internal/formats/setup.go) | `Client`, `SetupTemplate` (with `VersionSpec`), `SetupData`, the three spellings of the token reference (`SetupTokenRef`, `SetupTokenRefWin`, `SetupTokenRefPS`) and `SetupPackageRef`, `SetupVersionRef` |
| [guide.go](../../internal/formats/guide.go) | `GuideSection`, `GuideData`, the topics (`GuideTopics`, `RequiredGuideTopics`), `GuideRepositoryPlaceholder`, `ExecuteGuide` (the one parser the API and the kit use), `AppliesTo` |
| [serve.go](../../internal/formats/serve.go) | `ServeMode`, `ServeRoute`, `RouteKind.WrapsContent`, `Base` |
| [signatures.go](../../internal/formats/signatures.go) | `SignatureAdapter` (the declaration with what Quader attests, the detector, the classification of the gate, `Gate`, and what Quader's receipt attestation is about, `AttestationSubjects`; every format returns it from `Signatures()`), `GateKind`, `GateItem`, `GateSubject`, `GateNoneFor`, `AttestationItem`, `AttestationSubject`, `AttestNoneFor`, the optional `AttestationReferrer` with `ReferrerFile` and the reasons `ReferrerHidesSignature`, `ReferrerNoAPI` (OCI), `SignatureDeclaration` (with `ReadToSign` and `ArtifactName` for the gate, Q-SIG-211, Q-SIG-213), `SignatureKind`, `SignatureCandidate`, `SignatureLocator`, `SignatureEvent`, `SignatureContent`, the roles, schemes, sources and locators of signatures.md, `NoSignatures`, `MaxSignatureRaw` ([signatures.md](signatures.md)) |
| [contract_spec_test.go](../../internal/formats/contract_spec_test.go), [testdata/contract.txt](../../internal/formats/testdata/contract.txt) | The written contract equals the code (the Go blocks of R-ARCH-008/009 against the package, Q-ARCH-254), every exported name is on a spec page, and the frozen surface of `internal/formats` and `internal/formats/catalog` equals the generated file (ADR-141, Q-ARCH-280) |

### The packages around it

| Package | Responsibility |
|---|---|
| [internal/formats/catalog](../../internal/formats/catalog/) | The facts of every format, the name rule, the exposure facts and the compiled settings schema with `Entry.ValidateSettings` (a leaf layer); `Register` fills it. [bastion.go](../../internal/formats/catalog/bastion.go): `BastionFacts`, the core's `RepresentationHeaders` and `PerRequestHeaders`, `Facts.BoundPrefixes` and the rules `Register` applies to them ([What a Bastion needs of a format](#what-a-bastion-needs-of-a-format)) |
| [internal/formats/untrusted](../../internal/formats/untrusted/) | `Limits`, `DefaultLimits`, `Tighten`, and the bounded readers of R-ARCH-028 (see [Untrusted content](#untrusted-content)) |
| [internal/formats/formattest](../../internal/formats/formattest/) | The conformance kit: the in-memory `Host`, the checks (1 to 13, 15 and 17 to 20), the fuzz helpers (see [Conformance kit](#conformance-kit)); `crafted/` generates the hostile inputs, `testformat/` is the kit's own format with one named defect per check |
| [internal/formats/generic](../../internal/formats/generic/) | The generic format: no routes, `Classify`, a paths-only exposure adapter, the curl setup templates ([setup.go](../../internal/formats/generic/setup.go)) and the guide of curl ([guide.go](../../internal/formats/generic/guide.go), [guide/](../../internal/formats/generic/guide/)) |
| [internal/formats/all](../../internal/formats/all/all.go) | Registers every format at compile time (R-ARCH-010) |
| [internal/httpserver/content](../../internal/httpserver/content/content.go) | The content-header wrapper (`File`, `Index`, `Document`); `httpserver.ContentWriterFor` is the request-aware call |
| [internal/packages](../../internal/packages/) | The generic package rows: `Store` (reads), `Writer` (writes, raises `rev_seq` once per package per transaction), `RestoredAt`; statements in [packages.sql](../../internal/meta/postgres/queries/packages.sql) |
| [internal/formathost](../../internal/formathost/) | The `Host` ([host.go](../../internal/formathost/host.go)), the item-store adapters ([ingest.go](../../internal/formathost/ingest.go)), the `PackageWriter` adapter and the ownership seams ([writer.go](../../internal/formathost/writer.go)), the storage hooks and the files seam ([hooks.go](../../internal/formathost/hooks.go)), the data-port bindings ([bind.go](../../internal/formathost/bind.go)), links, renditions, per-request state, the stubs of a Host built without the engines ([stubs.go](../../internal/formathost/stubs.go); every call refuses, the shadowing check included), and the bridge from the virtual engine to `formats.Resolver` ([virtual.go](../../internal/formathost/virtual.go), [virtual.md](virtual.md)) |
| [internal/api/dispatch](../../internal/api/dispatch/) | The dispatcher: [dispatcher.go](../../internal/api/dispatch/dispatcher.go) (routing and the request path), [errors.go](../../internal/api/dispatch/errors.go) (the error shape of a prefix) |
| [internal/app/wire_formats.go](../../internal/app/wire_formats.go) | The wiring: hooks and files seam at step 8, Host and dispatcher at step 9, the data-port bindings, the catalog freeze |
| [internal/files/formats.go](../../internal/files/formats.go) | `files.Formats`, the seam the generic layer asks a format through: `Classify` for the coordinate of every decision and the patterns (R-EP-014), `ServeCheck` in `Stat` and `ServeFactsKey`, the rows the check decides on, which the lookup of `Stat` reads with the item (R-ARCH-027), `RawDeploy` in `Deploy`, `CheckRemoval` in the transaction of a delete |

## The package view

A format that has package screens (R-API-029) returns a `formats.ViewAdapter` from `View()`; `Base.View()` is nil, and a format without one is not
listed on the screens. The adapter orders the versions (`Compare`; "is a pre-release" is the exposure adapter's `Prerelease`, which the host's view bridge asks, Q-ARCH-257), reads what the screens show of a version beyond the generic rows
(`Describe`: description, keywords, licenses, the README flag, the metadata and, when asked, the README), answers the two searches that need the format's
data (`Find`) and names the stored versions it does not serve (`Withheld`). It runs no SQL: it reaches its tables through the data port the host hands it
(`formats.DataPorts`, the part of `Host` that is `FormatData`). `Validate` refuses a view without package names. The read model that calls the adapter, and
the bridge to it (`formathost.NewViewAdapters`), are in [packages.md](packages.md). To give a format a view: implement the six methods in `view.go` of the
format, add the statements to the format's data package and the matching methods to its port, and add the port interface the adapter asserts (`viewPort` in
[npm view.go](../../internal/formats/npm/view.go)). `Stored` is the sixth method: what one local or remote repository serves of one package, newest first (arrival for a hosted repository, publish time for a
remote one, otherwise `Compare` descending), and its tags only when the query asks (the catalog says the format has tags). "Serves" means answerable by a request for
the exact version, so unlisted and deprecated versions count and withheld ones do not (Q-EP-67). The dry run of an endpoint's exposure rules calls it when a check
names no versions (R-EP-016, Q-EP-61); the bridge is `formathost.ViewAdapters.Stored`, which is the `authz.StoredPackages` of the rule service, and it asks neither a
virtual repository nor a format without an adapter. Kit check 11 holds the adapter (`Compare` a total order over the fixture's sample versions, `Withheld` and
`Stored` over an empty repository and over a seeded package).

## The registry

`formats.Register(f)` (called from `internal/formats/all`) validates the format, puts its facts into `catalog.Default()` and keeps the format
for `Lookup` and `List`. It panics on:

- an invalid or reserved ID, a duplicate ID;
- a `ContractVersion` that is not the contract's;
- a package view (`View()` not nil) without `Descriptor.VersionFetch`: which downloads are fetches of a version (`FetchLeadFile`, `FetchAnyFile`,
  `FetchDocument`; R-API-079, ADR-205), the rule the package view counts a version's downloads and its last download by;
- a class without its adapter (`Remote` listed and `Remote()` nil) or an adapter without its class;
- the class `remote` without `Descriptor.Probes` (the requests that name exactly one version or file without fetching it, whose handlers call
  `Host.Remote().Probed`, R-REPO-065, ADR-207) or a `NoProbe` reason, and a probe on a route the format does not have, that serves neither
  the class `remote` nor the class `virtual`, whose method is not GET or HEAD or that says nothing of what it names;
- the class `remote` without `Descriptor.Catalogue` (the lookup by exact name, and for search and levels each the call or a one-sentence
  reason, never both) or without `Descriptor.VersionCanon` (the canonical form of a version, `catalog.VersionCanon`), and either of them on
  a format without the class ([remote-curation.md](../spec/remote-curation.md) R-CUR-006, R-CUR-007, [checkCatalogue](../../internal/formats/catalogue.go));
- a `DataPort` that is no interface type;
- a format whose `Signatures()` is nil, or whose declaration says neither its kinds nor why it has none, says both, or leaves a field of
  a kind empty or outside the roles and schemes of [signatures.md](../spec/signatures.md) R-SIG-001 (R-SIG-022, Q-SIG-43); a format
  without signatures returns `formats.NoSignatures(reason)`;
- a route set that is not servable: a route without name, method, template, handler, kind, action or example; a name used twice; a route
  that does not match its own example; an example that another route of the same method (and an overlapping class set) also matches; routes under a protocol base
  path other than `/api/<id>/{project}/{key}/` (the format that declares `catalog.Facts.RegistryRoot` has `/v2/{project}/{key}/` instead, and the catalog refuses a second one); a route of a class the format does not support;
- a format that is system only (`catalog.Facts.SystemOnly`, ADR-225: its one repository is the product's, [clamav.md](clamav.md)) and has
  a route of the kind `Upload`; the catalog refuses one with the class `remote` or `virtual`;
- every rule of the catalog (class set, layout, pattern subject, name rule, exposure).

`formats.Validate(f)` runs the contract checks without registering. The exposure facts of the catalog come from `Format.Exposure()`: no adapter
means not exposable, and the adapter's `Dimensions` and `catalog.Versioning` are what the decision point reads. The composition root
freezes the catalog ([wire_formats.go](../../internal/app/wire_formats.go) `wireFormats`) before the replica serves.

`Register` also records the format's [`Capabilities`](../../internal/formats/formats.go) once (`CapabilitiesOf`, read back with `CapabilitiesFor`):
the optional `ItemsRestorer` and, in `Capabilities.Remote` (`RemoteCaps`, read back with `RemoteCapabilities(id)`), the optional interfaces of the
remote adapter, `Discoverer` (a discovery document such as NuGet's service index, Q-ARCH-268) and `FillInspector` (inspect a fetched artifact
before it is stored, Q-ARCH-267) and `FactsGate` (the artifact gate on the rows the item lookup read, `RemoteCaps.Gate`, Q-ARCH-291), `Authenticator` (the token handshake with the upstream: the format reads the 401 challenge and the realm's answer, the engine does the I/O and keeps the token per replica, `RemoteCaps.Auth`) and `Revalidator` (the format's own revalidation request, a `HEAD` for a registry, `RemoteCaps.Revalidate`; [ADR-161](../decisions.md#adr-161--the-format-contract-gains-the-registry-root-the-upstream-handshake-head-revalidation-and-upload-sessions)); and, on the format itself, the optional `ServeFactsKeyer` (`Capabilities.ServeFacts`: the rows the serve check decides on, which the item lookup of a download reads with the item, Q-ARCH-291, [download-statements.md](download-statements.md)) and `VulnerabilityAdapter` (`Capabilities.Vulnerability`, R-VULN-009, [ADR-150](../decisions.md#adr-150--the-format-contract-gains-the-optional-vulnerability-adapter)), from which `Register` also derives `catalog.Entry.Ecosystem`; a format's audit routes read the scanning through `Host.Vulnerabilities()` ([vulnerabilities.md](vulnerabilities.md)). The remote adapter must be a stable value, because the instance `Remote()` returned at `Register` is the one
recorded. What the engine does with them is in [proxy.md](proxy.md#what-a-format-adds-to-the-engine).

## The request path of a format route

```text
chain 1-7 (recovery, trace, log, in-flight, address and host, authentication, rate limit)
     authentication asks Dispatcher.APIKeyRoute (steps 1-4 below, then Route.APIKey) where the API-key header decides (Q-AUTH-186)
  -> dispatch.Dispatcher.ServeHTTP
     1  qname.ParseAddress: /api/<id>/<project>/<key>[~cache]/<rest>; one segment, a "%" in a named segment or a project or key of the wrong shape is no address
        (404 repository_not_found in the format's shape, nothing asked); rest stays escaped
     2  a family of its own below the prefix (npm's exchange) only where the endpoint's surface holds it
     3  repository.CleanPath(rest) -> 400 invalid_path            (before authorization)
     4  Format.Match(method, rest) -> route and params | 404 | 405 with Allow;  the route name labels the request
     5  edge.Resolve -> the request's holder: subject (with the endpoint), index, the qualified name made known
     6  coarse DecideOn: the route's action on the repository, kind from the route; the exposure ceiling first
     7  Repos.ByName(project, key); deleting or another format -> 404 repository_not_found; disabled -> 404 repository_disabled
     8  a class the route does not serve -> a virtual write goes to Resolver().DeployTarget, else 405 with Allow
     9  formats.Request built (rq.Endpoint: name, Rules, CacheFill, exposure fingerprint and view, from the listener and the index),
        Host.Begin(rq) (state of the request; its finish function closes the spools); formats.ServeRoute (content headers, recovery, WriteError)
```

`rq.Endpoint` is what a format knows of the endpoint ([endpoints.md](endpoints.md#package-formats-on-an-endpoint-r-ep-018-r-ep-019)):
`Exposure` is the view a format's own filter takes (`Package`, `Version`; npm reads `Tag` through an interface of its own, Q-ARCH-257), read with `ExposureFingerprint` from the request's index, nil where the endpoint does not
restrict; `Rules` forbids member names in answers; `CacheFill` is folded into every populate decision by the core.

**A repository is addressed by project and key (Q-REPO-212).** The key is unique within its project, so every comparison and every cache key in this
path uses the `qname.Repo` (`rq.Repo.Name`, `Request.Addressed()`, `Refusal.Name`), never the bare key. Step 5 and 6 run on the qualified name before the
lookup of step 7: a project the caller cannot see answers exactly like a repository that does not exist (R-AUTHZ-016). Every URL a format returns to a
client (tarball URLs, NuGet `@id`s, registration and flat-container bases, the service index) is built by `Links` ([links.go](../../internal/formathost/links.go))
from the format's `ProtocolBasePath` template (`/api/npm/{project}/{key}/`) through `qname.ProtocolBase`, which escapes each value as one path segment; the
cache view is the same address with `~cache` on the key only. The metrics label `repo` and the audit `repository` hold `Name.String()`, `web/npm-all`.

Every error of steps 1 to 8 is rendered by the format's `WriteError`. The chain's own refusals (401, 421, 429, 503) on the prefix are rendered by
it too: [errors.go](../../internal/api/dispatch/errors.go) registers it with `Router.HandleErrorShape`, and `httpserver.Problem` consults the table by path
prefix ([router.go](../../internal/httpserver/router.go), [context.go](../../internal/httpserver/context.go)). A format's `WriteError` therefore must not call
`httpserver.Problem`. The dispatcher keeps each format behind `challenged`, which hands `WriteError` the error after `httpserver.PackageChallenge`: the
decision point's 401 to a caller without a credential carries the generic `Bearer`, and a format that passes the error's challenge on sends
`Basic realm="quader"` to a package client and `Bearer` to a browser all the same (Q-AUTH-235).

The handler decides on the concrete package or path with `Host.Authorize(ctx, rq, Target{...})`: the host completes the coordinate (repository, class, format, the
request kind of the route), decides on `rq.Index`, then applies the repository's include and exclude patterns to the package name or the path (404 for a read,
403 `not_admitted` for a write). A target that names a package names the canonical path of the document or file the route serves or writes as well
(npm's `documentTarget` and `tarballTarget`, NuGet's `docSpec.path` and the file paths): the decision point refuses a package without a path to every
grant with path patterns, and kit check 13 fails the handler (Q-AUTHZ-250). The same holds for the targets of `CheckOwnership` and `Claim`. A write that
writes or removes several files decides each of their paths, whether or not a file is there: a few with `Authorize` (npm's provenance bundle, NuGet's
`.nuspec` and `.snupkg`), many with `Host.PathsAllowed`, which decides a list without an audit event, followed by the `Authorize` of the first path not
held for the refusal (npm's unpublish, `mayDelete`; Q-AUTHZ-256). What a removal through `PackageWriter.DeleteItems` takes besides
the paths it names (the other files of a folder) the host decides as it removes them: the writer of `Host.Write` passes
`removalAllowed` as `items.RemoveSpec.Check`, which decides every file of the removal with `PathsAllowed` and refuses the whole
removal with the audited refusal of the first file not held (Q-AUTHZ-257).

A format whose versions each have a file implements `formats.VersionPather` (npm's tarball, NuGet's `.nupkg`): the decision point decides a version on
that path as well as on the document's, so a document must list only the versions the caller may read. `formats.CallerVersions(ctx, rq, name)` returns
the caller's `VersionView`, nil when the caller's own grants carry no path pattern on the repository (`ReadScope.VersionsByPath`), which is every
caller of an installation without path patterns and costs it nothing; otherwise the document filters with `View.Version`, joins
`formats.HiddenFingerprint(view.Hidden(versions))` to the variant of its rendition key and validator, takes the versions of the revision from
`formats.LiveVersions` (kept in the rendition cache per revision, so a hit asks no statement), and keeps no rendition of rows read at another
revision than the fingerprinted one ([versions.go](../../internal/formats/versions.go); npm's `callerVersions` and `filterOf`, NuGet's
`serveDocument` and `callerKeep`; Q-AUTHZ-255). In the kit, `formattest.Resolver` answers `ReadScope` from the host's `Allow`; a test's own resolver
embeds it. An allowed write decision is remembered with the request and recorded in the transaction of the write
(`Ingest().Commit` and `Host.Write`), where the generic layer records its own (R-AUTHZ-029). Before that transaction opens, the host decides
every remembered write again on a new index (`Host.recheck`, the commit-time re-check of R-AUTHZ-015, Q-ARCH-225): a grant removed while a
body was arriving refuses the write, the new decisions are the ones recorded, and the request decides on the new index from then on, so
`Claim` and `AutoClaim` in the transaction see it too.

## The storage hooks

The item store ([items.md](items.md)) runs `items.Hooks` inside the transaction of a commit and of a removal. `formathost.Hooks` is the implementation:

1. It finds the format of the event's repository (`RepoRef.Format`); a repository of an unregistered format has no hook.
2. It binds a `PackageWriter` to the transaction (property writes only below the format's `PropertyPrefix`, package rows and item removals only in the event's repository,
   Q-ARCH-260) and fetches the format's data port. The writer attaches the signal `metadata_changed` itself, once per package the transaction raised
   (`packages.Writer` with `packages.WithRaised`, Q-ARCH-259); a format does not call `Notify` for a publish, a tag change, an unpublish or a restore. `Notify` is for the areas
   a descriptor lists in `NotifyAreas` (`catalog.FormatAreas`: today `metadata_changed` alone), for a change that raised no package row: npm's `OnFill` after it replaced a stored remote document.
3. It calls `OnCommit` (with `Origin` upload or fill, the inspection and payload of an upload, `Replaced`) or `OnItemsRemoved` (each removed item with the
   version it belonged to and the SHA-256 of its content, which the item row no longer carries when the hook runs: NuGet's tombstone reads the SHA-512 of the blob by it, Q-NUGET-39).

**One request, one transaction.** Everything one client request writes commits in one transaction (R-ARCH-009). A request that stores more than one file passes the lead to
`Ingest().Commit` and the others as `Commit.Companions` (`formats.Companion`: its spool, path, media type, an own payload, and `Generated` for a file the format derived from the
lead, such as a package's generated manifest; npm's provenance bundle is one, NuGet's `.nuspec` will be). The item store commits the blobs of all files with one link transaction
(`blob.Store.CommitAll`) and links the lead first, then each companion in order, each with its own `OnCommit` (`CommitEvent.Lead` is the lead item on a companion's event, so the
hook of a companion finds its version through the rows the lead's hook wrote a moment before) and its own audit row; a failure of any of them leaves none of the files, the blob rows
of the written objects included (they are queued for the collector). A companion goes with a lead that changed: when the lead is identical to the content at its path, `Committed.Identical`
is true, nothing is written, the companions included, and `Committed.Companions` is empty. The companions take the lead's `CreateOnly` and `MayOverwrite`: the format decided the write as a whole.

A route that removes the files of a version calls `PackageWriter.DeleteItems(repo, paths...)` from inside `Host.Write`: the host's writer hands the paths to
`items.Store.RemoveIn` in the same transaction, which tells `OnItemsRemoved` there, so the route's checks and the format's side rows commit with the removal (npm: [npm.md](npm.md), Q-NPM-27).
Only the writer of a request removes (`formathost.ItemRemover` on the host's item store); the writer of a hook answers `errs.Internal`.

The writer is bound to the request when the commit came through `Host.Ingest().Commit`; a fill or the `items-delete` job has no request, so `Claim` and `AutoClaim`
refuse there. The sync delete and every batch of the delete job run the removal hook, which is how a delete of an npm folder through `/files/` raises `rev_seq`.

`files.Formats` ([formats.go](../../internal/files/formats.go)) is the generic layer's seam: `FilesSeam` in [hooks.go](../../internal/formathost/hooks.go) implements it over the
registry. `Classify` gives `/files/` and the items API the coordinate of a path (package and version, and whether the repository's patterns match names,
Q-AUTHZ-160); `ServeCheck` makes the `/files/` GET of a tombstoned tarball a 404; `RawDeploy` lets `/files/` deploy only into a format whose
`ProtocolBasePath` is `/files/{project}/{key}/` (409 for every other format, before the body, Q-ARCH-223); `CheckRemoval` asks the ownership of every
package whose files a delete removes, in the removing transaction (Q-AUTHZ-161). For generic, classification, the check and the raw deploy
change nothing.

## Format settings

A format with settings of its own (npm: `latestGuard`, `serveRemovedVersions`, `latestStrategy`, `distTagStrategy`) keeps them in the member of the repository document that is named
after the format (R-REPO-004). Four pieces make it one mechanism, and none of them is the format's own SQL:

| Piece | Where | What |
|---|---|---|
| The schema | `catalog.Facts.SettingsSchema`, a JSON Schema 2020-12 the format embeds ([npm settings.schema.json](../../internal/formats/npm/settings.schema.json)) | The root is an object with `additionalProperties: false` (a misspelt setting is refused, R-REPO-008); a property may name the classes it applies to in `x-quader-classes`; every property has a `title`, the label of its field in the web UI (sentence case, its unit included), and a property with an `enum` has a label for each value in `x-quader-enum-titles`, which the select shows. The title, the description and the value labels are the texts the form shows to users, so they are plain words: no requirement or decision ID, no HTTP status, no error code, no configuration key or member name, no `null`, a limit in words, and a description of one or two sentences that says what the setting does and when to change it; the wording of the specification stays with the format's spec page and the OpenAPI member (R-UI-088, Q-UI-79; `TestR_UI_088_EverySettingOfAFormatHasATitleAndPlainTexts` checks every schema under `internal/formats`). `Register` compiles it and panics on a schema that does not compile or breaks the first two rules |
| The validator | `catalog.Entry.ValidateSettings(class, raw)` ([settings.go](../../internal/formats/catalog/settings.go)), on `github.com/santhosh-tekuri/jsonschema/v6`, the library `kin-openapi` uses (one version, in `go.mod`) | Reports every violation with a JSON Pointer inside the member (`invalid_value`), and a setting of another class as `not_applicable` |
| The member | `repository.Settings.NPM` and `formatMembers` in [document.go](../../internal/repository/document.go) | Decoded strictly with the rest of the document, stored in `repositories.config` in canonical form (key order, no spacing, so a read-back is byte-equal), validated in `validateFormatSettings` ([validate.go](../../internal/repository/validate.go)) at `/<format>/<pointer>`: 422 `validation_failed`. The member of one format on a repository of another is `not_applicable` |
| The reader | the format reads its member from `rq.Repo.Config` (npm: `settingsOf`, [settings.go](../../internal/formats/npm/settings.go)) | Defaults for what the document leaves out. The format never validates what the service validated |

`formats.Validate` refuses a format that declares a schema and has no `formatMembers` entry, and a format named like another member of the document (`repository.IsReservedMember`). The schema is
served as `settingsSchema` of `GET /api/v1/formats`, and the creation form and the settings page render the member from it, labelled with each member's `title` (R-ARCH-004, [web-ui.md](web-ui.md#the-repository-settings-form-rendered-from-the-formats-schema)):
a setting needs no UI code as long as it uses the rendered subset (objects, booleans, enums, strings, integers, numbers; `title`, `description`,
`default`, `x-quader-classes`, `x-quader-enum-titles`), and its `description` is the text the form shows. The OpenAPI schema of the
member (`NpmSettings`, `NugetSettings`, `OciSettings`, `DebSettings`, `AnsibleSettings`) is written by hand, describes each setting for the API reader, and `TestR_ARCH_008_NpmSettingsSchemaEqualsTheOpenAPIMember`, `…Nuget…`, `…Oci…`, `…Deb…` and `…Ansible…` ([format_settings_test.go](../../internal/api/v1/format_settings_test.go)) fail on any difference to the registered schema. A format with settings adds its member to `repository.Settings` and to `formatMembers` (npm: `NPM`; NuGet: `NuGet`; OCI: `OCI`; Debian: `Deb`).

## The guide of a format

R-UI-049, R-API-053, Q-ARCH-235. `Guide()` returns the sections of the format's guide: what a repository of each class is and how each client
configures, signs in, installs, publishes, maintains, audits and keeps lockfiles through it, what an endpoint with exposure rules changes, and a
troubleshooting table. The API ([guide_api.go](../../internal/api/v1/guide_api.go), [http-api.md](http-api.md#the-guides-of-the-formats-r-api-053))
filters the sections by the class and the client, orders them by topic, runs each through `formats.ExecuteGuide`, rewrites `${QUADER_TOKEN}` by the
reader's credential mode and renders the Markdown with the README renderer; the setup snippets of the reader's client travel beside the sections
(`Guide.snippets`, R-API-070), and the page **Set up client** places them above the sections. A section that renders to white space only is left out,
which is how a section speaks to some readers only (`{{if .ExposureRules}}`, `{{if not .Anonymous}}`); the guide of a repository also leaves out the
topics `publish` and `maintain` for a reader who may not publish there (`keepsSection`).

| Field of `GuideSection` | Rule |
|---|---|
| `ID` | Stable and unique, `<format>.<name>` |
| `Topic` | One of `formats.GuideTopics()`; the required ones (`what`, `configure`, `authenticate`, `install`, `troubleshooting`) must cover every class and client |
| `Classes`, `Clients` | Whom the section is written for; empty is everyone |
| `Markdown` | `text/template` over `GuideData`; the text may speak of "the snippet", which the page shows above it (`SetupData` plus `Class`, `Client`, `PublishTarget`, `Anonymous`, `ExposureRules`, `TokenEnvVar`); `.Repository` is empty in the guide of a format, whose addresses carry `<repository>` |

Rules for the text: describe only what the format does as built and name what a user would try and is not built as not available; put a credential
only as `${QUADER_TOKEN}` in a fenced code block; keep sub-headings (`###`) unique across the sections, because each section's HTML gets its heading
anchors from the README component; pin each fact a test can hold (codes, quoted texts, defaults) in the format's tests (npm:
[guide_test.go](../../internal/formats/npm/guide_test.go)). npm and generic embed one Markdown file per section ([npm guide/](../../internal/formats/npm/guide/),
[generic guide/](../../internal/formats/generic/guide/)); generic's guide is for its one client, curl, and its local class (R-GENERIC-016,
[guide_test.go](../../internal/formats/generic/guide_test.go)).

## Content headers and error shapes

`formats.ServeRoute(f, w, rq, sameOrigin)` is the one function that runs a handler. It wraps the writer for a route of kind `Download` (file headers with
`Content-Disposition`), `Metadata`, `Computed` and `Volatile` (the same set without it, `RouteKind.WrapsContent`); a search may return package-derived bytes (titles,
descriptions) under those headers, but serves no stored item (Q-ARCH-258). A route of kind `Upload`, `Delete` or `Admin` is not wrapped and must return no package-derived bytes
(kit check 7). It also puts the repositories the request may name in the handler's context (`RepositoryAllowed`, Q-ARCH-260): the request's repository and the members below it. It recovers a panic (log with format and route, 500 through `WriteError`, `http.ErrAbortHandler` passes), renders an error returned before the status
line through `WriteError` without `Content-Disposition`, and aborts the connection for an error after it. `sameOrigin` is true when a session cookie authenticated the request.
On an endpoint with exposure rules (`rq.Endpoint.Rules`) its writer removes `Cache-Status` when the status line goes out, from a handler, an engine or an
error's headers alike (`trackWriter.scrub`, Q-EP-60; `TestR_EP_018_NoCacheStatusOnAnEndpointWithRules`).

## What a Bastion needs of a format

A Quader Bastion ([bastion.md](bastion.md)) is a read-only cache in front of a main installation. The main installation marks the
response of a forwarded request without asking the format (R-BAS-013): the dispatcher tells the marking stage the route's kind and
coarse action, the stage derives the class from the kind, the status and the caller's read shape. A format author owes four things
and writes no marking code:

| The Bastion relies on | So |
|---|---|
| `Download` is immutable bytes for a path | a route that serves bytes that change under one path is not `Download` |
| A `Metadata` or `Computed` document is the same for every caller that may read everything (`shared`) | a document that names the caller, lists the caller's groups, or answers `401` to the anonymous caller is `Volatile` (npm's `-/whoami` and `-/ping`). Kit check 12 sends each such route's content case as three callers and fails on a difference; it is the only place that finds it |
| A write has a writing kind or an action other than `read` | `Upload`, `Delete` and `Admin` routes are refused for a Bastion token before the handler runs, in your `WriteError` shape (code `bastion_read_only`); nothing to implement, but the shape must render any `*errs.Error`. A read route that takes a body (npm's audit) is `Volatile` with action `read`: the Bastion forwards it after a `204` probe answer |
| `absent` means missing for every subject | look the package up with `rq.Host.Packages().Package`, the item with `rq.Host.Items().Stat` and remote content with `rq.Host.Remote()`: those lookups tell the marking what was missing. A `404` after a lookup through your own data port is `pass` (correct, slower); never add a `Quader-Bastion-` header yourself, the core strips and sets them |

The mechanism is in [bastion.md](bastion.md#marking-the-write-probe-and-the-read-shape-package-3).

A Bastion serves every format through these rules alone, and a format adds no decision of its own about it
([R-BAS-058](../spec/bastion.md#r-bas-058--what-a-format-owes-a-bastion-and-kit-check-19), the owner's decision of 2026-11-22). Three
duties join the four above, and kit check 19 holds them with the first:

| The Bastion relies on | So |
|---|---|
| A `Download` answer carries `Content-Length` and a strong `ETag` that is the SHA-256 of its body, `"<hex>"` or `"sha256:<hex>"` | serve stored items through `rq.Host.Items().Serve`, which sets both; without them the Bastion passes the bytes through on every request |
| A document a `Metadata` or `Computed` route serves carries an `ETag` and answers `If-None-Match` with it `304` | the Bastion revalidates documents with conditional requests; `Items().Serve` answers them for a stored item |
| A response header of a content route's `200`, `206` or `304` is a representation header of the core (`catalog.RepresentationHeaders()`), a per-request header of the core (`catalog.PerRequestHeaders()`) or declared in `catalog.Facts.Bastion.ReplayHeaders` | a cached answer replays the representation headers and the declared ones and drops every other; OCI declares `Docker-Content-Digest`, `Docker-Distribution-Api-Version`, `Oci-Filters-Applied` and `Link`. Names are in net/http's canonical form, and `Register` refuses a declaration of a header of the core, a hop-by-hop header, `Set-Cookie` or a name with the prefix `Quader-` |
| WriteError answers a rate limit (`errs.RateLimited`) with `catalog.Facts.Bastion.RateLimited()`: `RateLimitedStatus`, 429 when it is 0, otherwise 429 or 503 | the Bastion's own rate-limit refusals of the format take it; the ClamAV definitions declare `503` |

The facts reach every Bastion in the handshake's format table ([R-BAS-057](../spec/bastion.md#r-bas-057--format-facts-at-the-bastion-the-handshakes-format-table-and-bound-hosts)),
with `catalog.Facts.BoundPrefixes()`, which is `/v2/` for the format that declares the registry root
([Q-BAS-183](../spec/bastion.md#q-bas-183--the-bound-host-prefixes-are-derived-from-the-registry-root-not-declared)). A route whose answers
cannot meet the content-address or the conditional duty is listed in the fixture's `NoBastion` with the reason; npm's root, dist-tags, keys
and attestations and OCI's tags list are, because they carry no `ETag` yet
([H-1249](../spec/bastion.md#h-1249--five-documents-of-npm-and-oci-carry-no-validator-so-a-bastion-fetches-them-whole-once-per-metadata-period)).
A format that needs a path family INV-6 does not have, or a host root beyond the bound-host prefixes of R-EP-006, needs a decision.

**Read shapes per package: kit check 21.** A Bastion will serve a reader of one package that package's documents and artifacts from its
cache ([R-BAS-080 to R-BAS-088](../spec/bastion.md#read-shapes-per-package); the check is built, the Bastion's and the marking's sides are
not yet). The main installation learns the package of a response from the rights decisions your handlers make through the host, with no code
of yours: `Host.Authorize` with `Target.Package`, `Host.WikiReadable`, `Host.PathsAllowed` and the `MayRead` rows of the read scope
([R-BAS-081](../spec/bastion.md#r-bas-081--the-package-of-a-response)). Kit check 21
([R-BAS-086](../spec/bastion.md#r-bas-086--what-a-format-owes-kit-check-21), [checks_package.go](../../internal/formats/formattest/checks_package.go))
holds three duties:

| The Bastion relies on | So |
|---|---|
| Every rights decision of a content route is made before its status line | decide through the host first, then write; a decision after the status line fails the check |
| A response that draws on packages other than the one it was decided for also decides a package-less read | call `Host.Authorize` with a target that names no package for such a response (npm's audit and signature handlers do); the response then carries no package and is shared only with readers of the whole repository |
| A reader of only the response's package receives the same status, body, `ETag`, `Content-Type`, `Content-Encoding`, `Vary` and `Cache-Control` as a reader of the whole repository | a document that lists or depends on another package must take the package-less decision above |

The check stores `Fixture.OtherPackage` (a `Name` and a `Seed`) after each case's own seed, serves the case as the fixture's caller, derives the
package from the decisions the kit's host recorded (`h.Decisions()`: one package named by every decision, none package-less), and serves it once
more on the same host as a reader of that package alone: `Allow` refuses every other package and every path in no package, and the read scope
reports `VersionsByPath` false (`HostOptions.NoPathPatterns`), as the decision point does for a reader whose package shape is full (condition 2
of R-BAS-080). A format with package names must set `OtherPackage` and may not skip the check; the seed must store a package row of its name, no
case's answer may belong to it, and at least one case must be compared. Generic skips it with its reason. The testformat defects
`ListsAnotherPackage` and `DecidesAfterItsStatusLine` each fail it alone (`TestR_BAS_086_KitCheck21`). npm, NuGet, OCI, Debian, Ansible Galaxy
and the ClamAV definitions passed it as they were, with no handler changed; their second packages are `underscore`, `newtonsoft.json`, the image
`other` (the same blobs under another name), `libxml2-utils`, `community.general` and the database `bytecode`. The build plan is
[bastion-package-shapes.md](bastion-package-shapes.md).

## What a signed link needs of a format

With the installation's switch on, a download may be answered `307` to a signed link of the object store
([R-STOR-070](../spec/storage.md#r-stor-070--the-conditions-of-one-download-in-the-order-of-the-serve-point), the owner's decision of
2026-11-22), but only on a route that declares `ClientDigest{Checks: true, Against, Evidence}`
([R-STOR-071](../spec/storage.md#r-stor-071--the-format-declares-whether-its-clients-check-a-digest)): every client family the format
supports compares the whole body with a digest it knew before the request. The declaration, its checks, the three declarations and the
serve point that reads them are built ([download-links.md](download-links.md)).

| Rule | Where |
|---|---|
| The zero value is "does not check" and the route streams, so a format that says nothing is safe | `formats.ClientDigest` ([route.go](../../internal/formats/route.go)) |
| Only a `GET` of the kind `Download` declares, and only with `Against` (what the clients compare with) and `Evidence` (the client's source read, or the end-to-end case in the format's own leg in which the real client refuses a link whose object was altered) | `Register` (`checkClientDigest`, [formats.go](../../internal/formats/formats.go)), so kit check 1 too |
| Every `GET` answer `200` of a declaring route is served through `rq.Host.Items().Serve` and is the stored blob under its content address; a route only the remote or the virtual class serves cannot show it in the kit and does not declare | kit check 20 ([checks_links.go](../../internal/formats/formattest/checks_links.go)); `NoContent` and `NoBastion` do not exempt from it ([Q-STOR-295](../spec/storage.md#q-stor-295--kit-check-20-judges-every-200-a-declaring-route-answers-to-a-get-against-the-stored-blob-and-a-skip-while-a-route-declares-is-refused)) |
| Every format has its row of declaring routes | `TestR_STOR_071_EveryFormatDeclaresWhetherItsClientsCheck` ([all_test.go](../../internal/formats/all/all_test.go)) |

Declaring today: `oci.blob.get` (the digest in the path) and `ansible.artifact.get` (`artifact.sha256` of the collection version); each
declaration sits beside its route with the evidence, and the end-to-end case in the format's own leg shows that every client family refuses
a link whose object was altered in the bucket. `npm.tarball.get` declared until that case showed Yarn 4 installing such a tarball: its npm
fetcher compares nothing with `dist.integrity` ([Q-STOR-354](../spec/storage.md#q-stor-354--yarn-4-installs-a-tarball-whose-object-was-altered-so-npms-tarball-route-declares-nothing)).
npm, NuGet, Debian, generic and the ClamAV definitions declare nothing. It follows the pattern of
the Bastion's facts above (a declared fact, held by the kit), with the difference that it is a field of the route, read only at the main
installation's serve point.

## Wiring

[wire_formats.go](../../internal/app/wire_formats.go) is the only wiring:

| Function | Moment | Does |
|---|---|---|
| `wireFormats` | step `formats` | freezes the catalog, binds and checks the data ports (`formatPorts` is where a format with side tables adds its constructor), builds the hooks and the files seam; also on a worker, whose delete job runs the hooks |
| `wireFormatHost` | step `formathost` (a role with a listener) | builds the package store, the rendition cache (`cache.rendition_bytes`), the `Host` with the proxy engine as its remote and the virtual engine as its resolver, and the dispatcher; refuses to start without the item store, the decision point, the repository reader, the exposure ceiling or the virtual engine |
| `wirePackagesAPI` | step `packages-api` | the read model and the README renderer behind the package screens ([packages.md](packages.md)); a part that cannot be built stops the start-up |
| `registerFormats` | step 9, from `startClient` | mounts the prefix of every format with routes on each endpoint's router, with its error shape |

The composition root binds `formathost.Ownership` (over `authz.Ownership.EnforceTx`, and `PrecheckWrite` for `Host.CheckOwnership`, the read-only question a writer asks before it reads a body, Q-ARCH-226; the in-memory host takes it from `HostOptions.Precheck`) and `formathost.AutoClaimer` (over `repository.Routing.AutoClaim`, which gets the caller of the
request and returns its `AutoClaimResult`) with two small adapters. It binds the remote engine (`formathost.Options.Engine`) and the resolver (`formathost.NewResolver` over the virtual engine) too, and `wireFormatHost`
refuses a replica that has no virtual engine. A `Host` built without them (a unit test, `formattest`) answers every call of `Remote()` and `Resolver()` with an internal error that says so, and the stub's
`ShadowCheck` is one of them: it **refuses** the first publish, because a check that cannot run must not let a publish through (R-REPO-048). A virtual `DeployTarget` of such a host answers `405 no_deploy_target`.
Each is a field of `formathost.Options` or `Deps`.

The composition root also reads the host at request time in one place, the files seam (`a.host`, a `late` value): the host is built from the item store, which is built with the seam, so the seam
cannot hold the host when it is created (Q-ARCH-210).

## Tests

| Where | What |
|---|---|
| [formats_test.go](../../internal/formats/formats_test.go), [serve_test.go](../../internal/formats/serve_test.go) | `Register`'s start-up checks (R-ARCH-010), `MatchRoutes`, `Template`, `ServeRoute` (R-ARCH-007, R-ARCH-023, R-ARCH-028) |
| [internal/httpserver/content](../../internal/httpserver/content/content_test.go) | The header sets of the three kinds and that the handler cannot weaken them |
| [dispatcher_test.go](../../internal/api/dispatch/dispatcher_test.go), [apikey_test.go](../../internal/api/dispatch/apikey_test.go) | Routing, the order of the checks, the class rules, the chain's refusals in the format's shape; `APIKeyRoute` names exactly the routes the dispatcher then serves that declare `APIKey` (fakes, no database) |
| [formathost_test.go](../../internal/formathost/formathost_test.go) | Data-port bindings (Q-ARCH-193), `Authorize`, the fail-closed `Claim`, links, renditions |
| [format_enums_test.go](../../internal/api/v1/format_enums_test.go) | `TestR_ARCH_010_PerClassFormatEnums`: the OpenAPI enums equal the registry |
| [formattest](../../internal/formats/formattest/) (`host_test.go`, `kit_test.go`), [testformat](../../internal/formats/formattest/testformat/format_test.go), [crafted](../../internal/formats/formattest/crafted/crafted_test.go) | The in-memory `Host` against the rules of the production writer, the kit's skip and fixture rules, every check failing on its named defect, the generators producing what they claim. The in-memory `Host` runs its transactions one at a time (`Host.Write`): its undo log writes into shared maps, so overlapping transactions would see each other's uncommitted rows and a rollback would take away rows another had built on, which PostgreSQL's row locks and isolation rule out. A test that needs another client's request to finish between a handler's checks outside the transaction and the transaction registers it with `Host.OnNextWrite` (it cannot run inside a transaction, where a second one on the same rows would wait). Concurrency in a unit test therefore exercises the handler's decisions outside the transaction (the retry of `commitTag`), not the database's locking; the locking is proven by the integration tier (the subtest `R-OCI-013_push_refusals_artifacts_and_concurrent_tags` of `TestProtocol` in [protocol_test.go](../../test/integration/oci/protocol_test.go): six concurrent pushes of one tag from three replicas) |
| [internal/formats/untrusted](../../internal/formats/untrusted/) (`readers_test.go`, `internal_test.go`) | The bounded readers against the crafted inputs: 2 GiB manifest, 10 GiB zip, JSON 100,000 deep, a document type declaration |
| [generic/conformance_test.go](../../internal/formats/generic/conformance_test.go) | Generic runs the kit with reasoned skips; checks 2, 3 and 8 run |
| [test/integration/files/formats_test.go](../../test/integration/files/formats_test.go) | Against PostgreSQL and the object store: `rev_seq` once per transaction, the hooks in the item's transaction, the dispatcher end to end, `ServeCheck` on the route and on `/files/`, the removal hook |

[probeformat_test.go](../../test/integration/files/probeformat_test.go) is a complete small format (routes, hooks, data port, exposure adapter, error shape) and the
best example to copy from.

## Conformance kit

[internal/formats/formattest](../../internal/formats/formattest/) is the table-driven test every format runs in its own package (R-ARCH-010). It needs no
PostgreSQL, no object store and no Valkey: `go test ./internal/formats/...` runs it. Decision: [Q-ARCH-216](../spec/architecture.md#decisions) (the kit
imports what the contract imports, so check 7 runs the real `formats.ServeRoute`).

### How a format runs it

```go
// internal/formats/<id>/conformance_test.go
func TestR_ARCH_010_Conformance(t *testing.T) { formattest.Run(t, Format{}, fixture()) }
func FuzzR_ARCH_028_Match(f *testing.F)       { formattest.FuzzMatch(f, Format{}) }
func FuzzR_ARCH_028_Inspect(f *testing.F)     { formattest.FuzzInspect(f, Format{}, "name/-/name-1.0.0.pkg", validUpload) }
```

`Run` makes one subtest per check, named `R-ARCH-010 check 7 content headers`. `Evaluate(f, fx)` runs the same checks and returns a `Report` without a
`testing.T`; the kit's own tests use it to assert that a defective format fails the check the spec names.
The fixture ([kit.go](../../internal/formats/formattest/kit.go)) holds what only the format can state. Everything else is derived from the format: its routes, their
`Examples`, its descriptor, its adapters, its setup templates.

| Fixture field | Used by | What goes in it |
|---|---|---|
| `Host` | all | `func() HostOptions`, called once per case: the class, the base URL, `Data` (the in-memory twin of the format's data port, bound and checked like the composition root does), `Allow`, `Claim`, `Remote` |
| `Rejects` | 2 | Further escaped paths `Match` must refuse |
| `Paths` | 3 | Escaped path and the `PathInfo` `Classify` must say; root, folder and a scoped or odd name belong here |
| `Canonical` | 3 | Maps an escaped route example to the canonical repository path `Classify` is asked about; nil when the protocol paths are the canonical paths (npm, generic). A route that addresses no stored path returns `ok == false`. NuGet maps through its own `Match` and `nuget.CanonicalPath` (Q-NUGET-29) |
| `Protocol` | 10 | Maps the escaped canonical package-document path to the path a client requests it at; nil when they are one string |
| `Remote` | 4 | The upstream origin, upstream documents (`RemoteDoc`), and `DownloadLinks(doc)`; at least one document must link to the upstream |
| `Merge` | 5 | Member documents in the core's order, one source marker per part, `Versions(doc)` and `VersionSource(merged, v)` to read the marker back |
| `Inspect` | 6 | Crafted uploads: `InspectTarGz(path, manifestEntry)`, `InspectZip`, `InspectJSON`, `InspectXML` give the standard cases; add your own `InspectCase` with a `crafted.Input` |
| `Content` | 7 | A request per `Download`, `Metadata`, `Computed` and `Volatile` route, with a `Seed` that stores what it reads; each such route needs one case it serves (a status below 400), since the headers are checked on what it serves |
| `NoContent` | 7 | A route of those kinds whose cases only refuse (an operation not built yet, a refusal by design), with the reason; an entry for a route a case serves fails (Q-ARCH-281) |
| `NoBastion` | 19 | A `Download`, `Metadata` or `Computed` route whose answers cannot meet the content-address or the conditional duty, with the reason; never the header duty. An entry without a reason, for another kind, for a route no case served or for a route whose answers meet both duties fails (Q-BAS-184) |
| `Changes` | 9 | A `ChangeCase` per storage hook and per `Upload`, `Delete` or `Admin` route; `Do` drives the host (`h.Serve`, `h.Ingest().Commit`, `h.Remove`, `h.Restore`). The kit records the hooks each case ran (`h.HooksRun()`) |
| `NoRevision` | 9 | A route of those kinds that changes no package row, or a hook (`OnCommit`, `OnItemsRemoved`, `OnItemsRestored`) no change can drive, with the reason |
| `Names` | 10 | Spellings of one package name: they must normalize to one name and lead to one document path |
| `NoDocument` | 10 | The reason a format has no package document (an APT archive: its upstream has none per package, Q-REPO-270): `PackageDocumentPath` must then report no path for every sample name, and the names are held to their normalization alone |
| `OptionNames` | 8 | Literal client option names a template may spell although they contain `token`, `password` or `_auth` |
| `Signatures` | 18 | A `SignatureCase` per kind the format declares at least: the kind's name, the stored path, a `Commit` that stores the signed sample through the host (or the `Body` of a remote's stored document) and the candidates `Detect` must name ([signatures.md](signatures.md#check-18)); for a signature that follows its subject (OCI), `RemoveSubject` removes the subject the way a client does and the kit holds that the sample went with it (R-SIG-025) |
| `OtherPackage` | 21 | A package beside the one each content case reads: its normalized `Name` and a `Seed` that publishes it the way a client does, so that the host holds its package row; required with package names, and no case's answer may belong to it (R-BAS-086) |
| `GateCases` | 18 | A `GateCase` per row of the format's classification for the signature gate (R-SIG-063): a `Seed` that stores the item (and what `Gate` reads of the data port), its `Path`, the `GateKind` it wants and, for an artifact covered by another, the stored paths of what covers it (`Covering`), and whether Quader's receipt attestation is about it (`Attested`, R-SIG-044). Required of a format that declares signature kinds, with at least one `Attested` case when the declaration attests something; a format without signatures is held to `GateNone` and to no attestation subject over its content cases |
| `Skip` | all | The checks that do not apply, each with the reason |

### The checks (1 to 13, 15 and 17 to 21; check 14, the vulnerability corpus, runs in the vulnerability tests; check 16 is in [usage-and-limits.md](usage-and-limits.md))

| # | The kit does | Fails when |
|---|---|---|
| 1 | `formats.Validate` and the catalog's rules (what `Register` runs), then `Match(method, example)` for every example of every route | An example is unmatched, matched by two routes (`/{package}` and `/{scope}/{name}` without a disambiguating rule), or resolves to another route |
| 2 | The traversal inputs (`..`, `%2e%2e`, `%2E%2e%2f`, NUL, `%00`, `\x01`, backslash, malformed escapes, 4,097 bytes, a 256-byte segment), `Rejects`, and every route example mutated with them, through `Match`; runs for a format without routes too | Any of them resolves to a route, or `Match` panics |
| 3 | `Classify` over every example, mapped by `Canonical` when the fixture has one (a `Download` route is `Content`, a `Metadata` or `Computed` route is never `Content`, a `Volatile` route is `PathVolatile`), and over `Paths`; an example `Canonical` maps to no path is left to checks 7 and 9 | A missing kind, a wrong kind, a path case that differs, a `Download` example that addresses no canonical path |
| 4 | `Rewrite` twice on a copy of each document, then on its own output | A different output, a changed input, an output that rewriting changes again, an upstream host left in a download link; a fixture without an upstream link |
| 5 | `Merge` twice; for each version, the source must be the lowest (tier, position) part that holds it | Another member wins a version, or the output differs |
| 6 | Every crafted input through `Inspect` with the effective limits (`untrusted.DefaultLimits` tightened by the descriptor): `413` or `422` within 5 s (stretched fivefold in a build under the race detector; `make test` also runs the kit without it, which holds the 5 s), heap sampled with `runtime/metrics` within 512 MiB of the baseline. A format whose remote adapter is a `FillInspector` runs every input its `InspectsFill` takes through `InspectFill` too, under the same bounds, whatever it answers (Q-ARCH-281). A format whose remote adapter is a `Discoverer` runs the kit's crafted discovery documents and the fixture's `Discovery` cases (at least one `Valid`) through `ParseDiscovery` under the same bounds ([checks_discovery.go](../../internal/formats/formattest/checks_discovery.go)); the check cannot be skipped for either | Success, another kind, a timeout (the kit cancels the context), the heap bound, a panic; for the fill hook a timeout, the heap bound or a panic; for the discovery parser a crafted document accepted, a valid one refused, a timeout, the heap bound, a panic, or a discovery document that is no metadata path |
| 7 | Every content case through `formats.ServeRoute`, once with and once without a session cookie, with the handler wrapped so that it deletes the content headers before and while it writes; the headers are read at the status line and compared with `content.Headers`; then `WriteError` renders a refusal without a credential that carries a Basic and a Bearer challenge (`challenge` in [checks.go](../../internal/formats/formattest/checks.go)) | A header missing or weakened; a refusal without a credential answered with another status than 401 or without `WWW-Authenticate` (`testformat.DropsChallenge`); `Content-Disposition` on a document; a route of kind `Upload`, `Delete` or `Admin` whose body holds package bytes it read, a route that is no `Download`, `Metadata` or `Computed` route and served an item (a `Volatile` one included); a route without a case; a route whose cases only refuse and that `NoContent` does not list; a `NoContent` entry without a reason, for a route a case serves or for no content route |
| 8 | Every client has a `configure` and an `install` template; every template parses and executes over `SetupData`; no template text matches `(?i)token\|password\|_auth` outside the whitelisted option names. Then the guide ([guide.go](../../internal/formats/formattest/guide.go), Q-ARCH-236): every section has an ID, a known topic, a title, classes and clients of the descriptor and a valid `Snippets`; it executes over `GuideData` for every class and client it applies to and every reader variant (a named repository and none, with and without a publish target, signed in and anonymous, with and without exposure rules); its fenced code blocks match the credential pattern only in the option names, `${QUADER_TOKEN}` and the variable name; no output holds `qdr_(pat\|sat\|cit)_`; every class and client has the required topics, which say something in every variant, and the `configure` and `install` topics attach those snippets | A missing section or template, an unknown client, a duplicate ID, a bad language or section, a credential; a class or client without a required topic, a required topic that says nothing, a section that attaches a section its client has no template of, a guide without clients |
| 9 | Each `ChangeCase` against the in-memory host: `rev_seq` of its package before and after, and the routes and hooks it ran | The revision did not rise; a route of kind `Upload`, `Delete` or `Admin`, or a hook the format has (`OnCommit` and `OnItemsRemoved` with package names, `OnItemsRestored` with `ItemsRestorer`), without a case or a `NoRevision` entry |
| 10 | For each spelling: normalize, `PackageDocumentPath`, escape, `Protocol` when the fixture has one, `Match("GET", …)`; all spellings of one name | A name the format rejects, spellings that normalize differently or lead to two paths, a path that matches no `GET` route |
| 12 | Every content case that a `Metadata` or `Computed` route serves, sent once to one seeded Host as a user, a service account and the anonymous caller ([checks_shared.go](../../internal/formats/formattest/checks_shared.go); the callers are derived from the Host's own subject, the seed is made once so only the rendering is compared) | The status, the bytes or `ETag`, `Content-Type`, `Content-Encoding`, `Vary` or `Cache-Control` differ between the callers; a `Metadata` or `Computed` route without a case that serves a document and without a `NoContent` entry. The message says to render the route `Volatile` or to take the caller out of the document |
| 13 | Every content case and every change case again, each on a fresh Host of the local class with the targets of its seed forgotten, every document of the `Remote` fixture on a remote Host and every case of the `Merge` fixture on a virtual one, each at its protocol path (`RemoteDoc.Request`, `MergeCase.Request`; NuGet names them, npm's are its canonical paths); the package rules apply to every answer that was served or asked the decision point anything; reads the targets of `Authorize`, `CheckOwnership` and `Claim` (`h.Authorizations()`, `h.OwnershipChecks()`, `h.Claims()`) and the paths of the items a route served (`h.ServedPaths()`) ([checks_authz.go](../../internal/formats/formattest/checks_authz.go), Q-AUTHZ-250) | A target names a package and no path; a path that is not canonical, or that `Classify` places in another package than the target names; a file a route served and did not authorize for `read` at its own path; a request whose canonical path `Classify` places in a package and that authorized no target naming that package at a path (a repository-level `Authorize` for a package's document); a `Remote` document or `Merge` case that matches no route; content cases served or change cases ran and no target with a path was handed over (a fixture whose every case refuses by design, as the OCI skeleton's, has nothing to judge yet), or the fixture has no content and no change case; for a format with `VersionPather`, a package document (a content case `Classify` places in a package and no version) served again on its seeded Host that differs when only an unrelated path is refused (the control run), or that still names a stored version, as a value of its own, when that version's file is refused by `Allow` (it lists a version whose file the caller may not read), or a `VersionPath` that is not canonical or does not classify to its package and version (Q-AUTHZ-255); a format with package names whose fixture places a path in a version and that has no `VersionPather`, or a fixture that serves package documents of which none names a stored version (Q-AUTHZ-258) |
| 15 | `Route.Wiki` is a `Metadata` or `Computed` route with the action `read`, and only a format whose facts say it has wiki content (`catalog.Facts.Wiki`) declares one; a format with wiki content has `Fixture.WikiSamples`; every content case is served again on a host with `HostOptions.WikiDenied` (`WikiReadable` false, `Authorize` of a wiki target refused) and no answer holds a byte of a sample ([checks_wiki.go](../../internal/formats/formattest/checks_wiki.go)); a route declared `Wiki` is refused by the dispatcher's coarse check before its handler runs and is not served | A `Wiki` route that is not a read, a `Wiki` route in a format without wiki content, no sample, a document or file that holds the README for a caller who may not read the wiki (`testformat.LeaksWiki`) |
| 18 | Every kind of the format's signature declaration has a case of `Fixture.Signatures`; each case is stored through a fresh host (or handed over as a remote's document), the kit builds the `SignatureEvent` from what was stored and runs `Detect`; every wanted candidate is named with its source, location, part, role, subject and object, and every named one has a valid role, source and scheme, a location, a subject and at most `MaxSignatureRaw` bytes; no live version's lead item is a carrier the case names (a signature embedded in its subject's own file excepted); a format that declares none detects nothing over the items its content cases seed; each `GateCase` is seeded on a fresh host and `Gate` must name its kind and the covering items' digests and `AttestationSubjects` attest it exactly when the case says (under a name), and a format without signatures answers `GateNone` and no attestation subject over the items its content cases seed ([checks_signatures.go](../../internal/formats/formattest/checks_signatures.go)) | A kind without a case, a candidate not named, a signature that became a version (`testformat.SignatureVersion`), no declaration (`testformat.NoSignatureDeclaration`), no gate case, a signature file named an artifact (`testformat.GatesAttachment`), an artifact named `GateNone` (`testformat.PassesArtifact`), a signature file attested (`testformat.AttestsSignature`) |
| 19 ([R-BAS-058](../spec/bastion.md#r-bas-058--what-a-format-owes-a-bastion-and-kit-check-19)) | Every content case of a `Download`, `Metadata` or `Computed` route again, on a fresh seeded host, for a Bastion ([checks_bastion.go](../../internal/formats/formattest/checks_bastion.go)): every header of a `200`, `206` or `304` against `catalog.RepresentationHeaders()`, `catalog.PerRequestHeaders()` and `catalog.Facts.Bastion.ReplayHeaders`; a `Download` answer's `Content-Length` and content-address `ETag` against its body; a document served again with its own `ETag` in `If-None-Match`; the format's `WriteError` of a rate limit against `catalog.Facts.Bastion.RateLimited()`; the `NoBastion` entries (Q-BAS-184) | An undeclared header (`testformat.UndeclaredReplayHeader`), a `Download` answer without a content address (`DownloadWithoutContentAddress`, and a format that writes its downloads itself, `WritesOwnDownload`), a document without an `ETag` or that answers its own `200` (`DocumentIgnoresConditional`), another status for a rate limit (`RateLimitedStatusDiffers`), a route of those kinds without a case that served, an exemption it does not need |
| 20 ([R-STOR-071](../spec/storage.md#r-stor-071--the-format-declares-whether-its-clients-check-a-digest)) | Every `GET` content case of a route that declares `Route.ClientDigest.Checks` again, on a fresh seeded host ([checks_links.go](../../internal/formats/formattest/checks_links.go), Q-STOR-295): each `200` against the items `Items().Serve` served (`h.ServedPaths()`), its body, its `ETag` and the SHA-256 of the stored item; a format that declares nothing passes | A declaring route without a `GET` case answered `200` (`testformat.DeclaresUnservedBody`, whose route `NoContent` lists), a `200` not served through `Items().Serve`, without a body, whose `ETag` is no content address of its body (`DeclaresUnaddressedBody`, whose route `NoBastion` lists) or whose body is not the stored blob |
| 21 ([R-BAS-086](../spec/bastion.md#r-bas-086--what-a-format-owes-kit-check-21)) | Every `GET` and `HEAD` content case below `400` of a `Download`, `Metadata` or `Computed` route again, on a fresh host seeded with the case and `OtherPackage` ([checks_package.go](../../internal/formats/formattest/checks_package.go)): the decisions the request made (`h.Decisions()`: `Authorize`, `WikiReadable`, each path of `PathsAllowed`, each `MayRead` row) and where the status line fell among them; when one package P is named by every decision and none is package-less (R-BAS-081), the case again on the same host, renditions forgotten, as a reader of P alone (`Allow` refuses every other package and every path in no package, `NoPathPatterns`) | A decision after the status line (`testformat.DecidesAfterItsStatusLine`); the status, the bytes or `ETag`, `Content-Type`, `Content-Encoding`, `Vary` or `Cache-Control` differ between the two readers (`testformat.ListsAnotherPackage`); with package names, no `OtherPackage`, a seed that stores no package row of its name, a case whose status changes with it stored, a case whose answer belongs to it, or no case compared |
| 17 | Every content case of a `Download` route the fixture serves with `GET`, again on a host whose `Serve` answers each of the malware gate's three answers (`HostOptions.Admit`; [checks_malware.go](../../internal/formats/formattest/checks_malware.go), [malware.md](malware.md#the-gate)) | Another status than `403`, `403`, `503`; no `Cache-Control: no-store`; no `Retry-After` on the `503`; the item served after the refusal (`testformat.WritesOwnDownload`, `testformat.RendersBareErrors`) |

A skip without a reason fails the check. So does a skip the format contradicts: checks 1, 3, 7, 12, 13, 19 and 21 with routes, 4 with a remote adapter, 5 with a virtual
adapter, 6 with an `Upload` route, 8 with clients, setup templates or a guide, 9 with package names or a route that writes, 10 and 21 with package names, 11 with a
view adapter, 15 with wiki content, 16 and 17 with a `Download` route, 20 with a route that declares `ClientDigest.Checks`, and checks 2 and 18 always. npm and NuGet skip nothing (`TestR_ARCH_010_NoCheckIsSkipped` in each package); Debian's fixture declares no skip either ([conformance_test.go](../../internal/formats/deb/conformance_test.go), the merge check 5 runs over its virtual adapter), though no test of its own pins that. A check with nothing to run over and no skip fails: that is how a later generic remote
class cannot pass vacuously ([generic's test](../../internal/formats/generic/conformance_test.go) shows the skips of a format with no routes).

### The in-memory host

[`formattest.NewHost(f, HostOptions)`](../../internal/formats/formattest/host.go) implements `formats.Host`: items ([store.go](../../internal/formats/formattest/store.go)), package rows
with `rev_seq`, links on a fixed base URL (`DefaultBaseURL`), renditions and notifications as logs, an authorization table (`Allow`, with every call recorded),
the format's data port, and `Remote` and `Resolver` from the options (`MemRemote` is a ready remote over fixed documents; unset, they answer "not supported" like the
production host before an engine is bound). `h.Decisions()` returns the rights decisions of the last request `ServeRequest` ran, in order, each with whether the status
line was out (`ServeRequest` hands the route a writer that tells the host), for check 21. It mirrors production where a format can tell the difference:

- the package writer raises `rev_seq` once per package per transaction, however many calls touch it; `EnsureLocked` of a new package starts at 1 and does not bump an
  existing one; `BumpRevision` is idempotent within the transaction (Q-ARCH-212); it attaches `metadata_changed` once per raised package, `Notify` accepts the areas of
  `Descriptor.NotifyAreas` only and attaches one (area, payload) once (Q-ARCH-259);
- the read stores and the writer refuse a repository outside the request's scope with an internal error (Q-ARCH-260);
- a transaction (`Write`, `Ingest().Commit`, `Remove`, `Restore`, `Seed`) is undone as a whole when its function or a `Before` hook fails: rows and notifications vanish;
  a commit sends the notifications and runs the `After` hooks;
- `Claim` on a writer bound to no request (`Seed`) answers an internal error; `SetProperties` refuses a key outside the descriptor's `PropertyPrefix`;
- `Ingest().Commit` of the same content at the same path is an identical commit that runs no hook; other content without `MayOverwrite` is refused;
- `Serve` runs the format's `ServeCheck`, answers `If-None-Match` that names the item's content address `304` without the body before the
  admission (as `edge.ServeItem` does), and `Host.Serve(ctx, method, path, body, sameOrigin)` runs a request the way the dispatcher does
  (`Match`, the request, `ServeRoute`).

A seed that stores state without a hook: `h.PutItem(path, mediaType, data)`. A seed that goes through the hooks: `h.Ingest().Commit` or the format's own upload route.
Reads see the effects of a transaction as it runs (there is no isolation between a running transaction and a reader); production reads the committed state.

### Crafted inputs

[crafted](../../internal/formats/formattest/crafted/crafted.go) generates each input from a few bytes on demand, so a test holds none of the gigabytes it pretends to carry:
`TarGzEntry(name, size)` (a regular file that declares and holds `size` zeros), `TarGzEntries(n)`, `TarGzDuplicates(name)`, `TarGzSymlink(name, target)`, `ZipBomb(name, size)`
(a zip64 archive with a correct CRC, addressed by offset and never held in memory; it is 10.6 MB long for 10 GiB), `ZipEntries(n)`, `DeepJSON(depth)`, `WideJSON(n)`, `XMLDoctype()` (entities that expand
into each other), `DeepXML(depth)`. An `Input` is a stream (`Open`) and, for an archive, a `ReaderAt`; `formattest.SpoolOf(in)` serves it as the spool of an upload.

To add a crafted input: write the generator in `crafted.go` (a `pull` reader or a `segments` reader keeps it streaming), test in `crafted_test.go` that it is what it claims (the
standard library reads it, the size and CRC are right), then add an `InspectCase` to the standard set in [kit.go](../../internal/formats/formattest/kit.go) if every format of that archive type
should meet it, or to one format's fixture if it is specific. A format-specific ambiguity (`TarGzDuplicates`, `TarGzSymlink`) is not in the standard set: the spec asks 413 or 422 of the
kit's cases, and a format may answer such an input with its own 400.

### The test format and its defects

[testformat](../../internal/formats/formattest/testformat/format.go) is a complete small format that passes the checks (`testformat.New()` with `testformat.Fixture()`; it has no package screens, so check 11 is skipped there and the kit's own tests give it a view adapter, `kit_view_test.go`), and a named
`Defect` per check that breaks the rule the check exists for (`AmbiguousRoutes`, `CleaningMatch`, `ContentMetadata`, `NondeterministicRewrite`, `UpstreamLeak`, `MergeLastWins`,
`InspectAccepts`, `InspectWrongKind`, `InspectHeap`, `TarballVolatile`, `TemplateCredential`, `NoInstallTemplate`, `NoGuide`, `GuideCredential`, `TagWithoutRevision`,
`NamesCaseSensitive`, `WrongDocumentPath`, `AdminReadsPackages`, `NotifiesUndeclared`, `ReadsOtherRepository`, `SignatureVersion`, `NoSignatureDeclaration`,
`DownloadWithoutContentAddress`, `UndeclaredReplayHeader`, `DocumentIgnoresConditional`, `RateLimitedStatusDiffers`, `DeclaresUnservedBody`, `DeclaresUnaddressedBody`,
`ListsAnotherPackage`, `DecidesAfterItsStatusLine`;
the options `SearchReadsPackages` and `DeclaresClientDigest`, a declaration on the tarball route, are conforming variants).
Its documents carry the SHA-256 of their bytes as `ETag` and answer it `304`, as check 19 asks. Every way a declaring route can miss check 20 is one checks 16 and 19
catch as well, unless the route carries their exemption, so each defect of check 20 is tested with the fixture entry such a format would carry, `NoContent` or
`NoBastion`, and fails check 20 alone ([Q-STOR-296](../spec/storage.md#q-stor-296--each-defect-of-check-20-takes-the-exemption-another-check-grants-its-route-so-that-it-fails-check-20-alone)). [format_test.go](../../internal/formats/formattest/testformat/format_test.go) asserts that each defect fails the check the spec names, plus the checks that cannot
stand without it (routes that are not told apart cannot be served), and no other: these are the acceptance criteria of R-ARCH-010 and R-ARCH-007. To add a rule to the kit, add the defect first
and watch the test fail.

### Fuzzing

`FuzzMatch(f, format)` seeds from the route examples, their traversal mutations and the kit's inputs; for any method and path `Match` must not panic, answer only not found, method not
allowed (with `Allow`) or invalid path, never resolve a path that `repository.CleanPath` refuses, and answer the same twice. `FuzzInspect(f, format, path, seeds...)` runs `Inspect` with the
kit's tightened limits: it must not panic, finish within its deadline, fail only with a client error (never `Internal`, never unclassified) and return properties below the descriptor's
`PropertyPrefix` only. Run one with `go test -run '^$' -fuzz FuzzR_ARCH_028_Match -fuzztime 2m ./internal/formats/formattest/testformat/`.

### What the kit does not see

- Which hooks a format implements: `formats.Base` supplies no-op hooks, so the kit cannot tell an overridden `OnCommit` from the default. The fixture must hold a change case for each hook the
  format overrides; a hook with no case is a gap nobody reports.
- That a route handler decides authorization on the right action and package: check 13 holds every target to a canonical path of its package and every
  served file to a read at its path, but not that the action is the one R-AUTHZ-007 names; the host records every `Authorize` call (`h.Authorizations()`),
  and a format's own test asserts on them.
- What a remote or a virtual repository's handlers decide for a reader of one package: check 21 serves the content cases on a local host, so the
  handlers of the other classes (npm's, NuGet's, Debian's and Ansible Galaxy's remote and virtual handlers, OCI's virtual read) and the routes
  `NoContent` lists are not run by it; they were read for a decision after a write, and none was found
  ([H-1390](../spec/bastion.md#h-1390--kit-check-21-runs-over-the-local-class-only)).
- Whether a client really checks what a route declares (`Route.ClientDigest`): check 20 holds the route's answers to the stored blob, never
  the client; the declaration rests on its `Evidence`, and the table test makes every change of it a reviewed one.
- The dispatcher's order of checks and the error shapes of the chain: [dispatcher_test.go](../../internal/api/dispatch/dispatcher_test.go).
- Retried transactions: `Write` runs the function once; a format with a non-transactional twin of its data port must not depend on a rollback.
- A leak of package bytes through an `Upload`, `Delete` or `Admin` route that the kit detects only when the bytes appear unchanged in the body or the route served an item; a `Volatile` route is allowed to return them (Q-ARCH-258), so what a search puts in its answer is the format's to review.
- A format that reads a member it took from `rq.Repo.Members()` instead of `rq.Member`: the stores accept every member of the request's repository, so the per-member read check of R-AUTHZ-009 rule 2 is skipped without anyone noticing (Q-ARCH-260, [H-329](../security/repository-model.md#h-329--a-format-that-reads-a-member-without-rqmember-skips-the-per-member-read-check)).

## Untrusted content

[internal/formats/untrusted](../../internal/formats/untrusted/) is the one place that enforces R-ARCH-028 for every format. It imports the standard library and `errs`; a format may import it.
Every reader takes a `Limits` and treats an unset field as the default (`DefaultLimits().Tighten(l)`), so a zero `Limits` is never unbounded; a format can only tighten
(`Descriptor.Limits`, `Tighten`). A `Limits` field that is crossed is `untrusted.TooLarge` (413); a structure the readers refuse is `untrusted.Refused` (422, code `content_refused`).
A format maps both to the codes of its protocol; `IsTooLarge` and `IsRefused` classify. Decision: [Q-ARCH-217](../spec/architecture.md#decisions).

| File | Responsibility |
|---|---|
| [limits.go](../../internal/formats/untrusted/limits.go) | `Limits`, `DefaultLimits`, `Tighten` (never loosens), `Bound` (the document cap: `DocumentBytes`, else `ManifestEntryBytes`) |
| [bounded.go](../../internal/formats/untrusted/bounded.go) | `Limit` and `ReadAll` (fail with `TooLarge` instead of cutting), `WithContext`, `Limits.Context` (the deadline) |
| [json.go](../../internal/formats/untrusted/json.go) | `CheckJSON` (one pass over the bytes for nesting depth and number of values, strings skipped), `DecodeJSON`, `UnmarshalJSON` |
| [xml.go](../../internal/formats/untrusted/xml.go) | `CheckXML` (any declaration, so any document type declaration, is refused; nesting depth; well-formedness; UTF-8 only), `DecodeXML`, `UnmarshalXML` |
| [tar.go](../../internal/formats/untrusted/tar.go) | `NewTarGz`, `TarReader` (`Next`, `Read`, `ReadEntry`), `CountTarGz` |
| [zip.go](../../internal/formats/untrusted/zip.go) | `NewZip`, `NewZipDeclared`, `ZipReader` (`Entries`, `CheckEntry`, `ReadEntry`); `CheckEntry` judges the bounds of one entry (declared size, ratio) from its declaration and reads nothing, so a caller that can go on without the entry tells them from the bounds of the whole archive, which only `ReadEntry` meets; `Limits.DeclaredEntryCap` lets such a caller count a rejected entry at its bound toward the declared total (R-VULN-004, Q-VULN-116) |

What the archive readers enforce, and what stays with the format:

| Rule | tar.gz | zip |
|---|---|---|
| Entry count | `Entries`, at the header | `Entries`, from the end record, before the standard library parses the directory |
| Declared bytes of all files | `StreamBytes`, at the header | `StreamBytes`, at open |
| Bytes read into memory | `UncompressedBytes` in total; `ReadEntry(max)` refuses a declared size above `max` (default `ManifestEntryBytes`) before inflating | the same, per `ReadEntry`; the size of one entry is also `CheckEntry`'s |
| Inflated bytes | capped at `StreamBytes` plus a per-entry overhead, skipped content included | cut at the declared size; a stream that disagrees is refused |
| Ratio | `Ratio` per entry read, measured on the compressed bytes the inflater consumed, with 128 KiB of read-ahead allowance | `Ratio` on the declared sizes, before inflating (`CheckEntry`, and `ReadEntry` after the size and the memory bound) |
| Names | A name used twice (a directory's trailing slash is no second name) is refused | The same, and the central directory's name must equal the local header's |
| Entry types | Links, devices, sparse files refused | Links, encrypted entries and methods other than store and deflate refused |
| After the archive | Only zero padding, a clean gzip trailer and no second member | No bytes in the central directory its entries do not account for |

What a format still does itself: the rules of its own protocol (npm's single first path component, case-folded `package.json`, link targets: [tarball](../../internal/formats/npm/tarball/tarball.go) implements them with
its own finite limits, as R-ARCH-028 allows a streaming pass), which entry is the manifest, and the mapping of the two error kinds to its codes. `CountTarGz` is the generic form of the counting pass
(`dist.unpackedSize`); npm's own pass stays in `tarball`. Names are never file-system paths: nothing in the package writes to disk.

What is not covered: an archive in several parts is refused, not read; the zip reader takes sizes from the central directory and does not look at a local header's own sizes or data descriptor;
an entry whose compressed stream is longer than the central directory says is read only up to the declared compressed size by the standard library and refused as damaged (this path is not covered by a
test of its own).

## Signatures on the wire

The optional capabilities of the formats' wire behaviour of signatures ([signatures.md](signatures.md#package-3-built)), read by
`CapabilitiesOf` like every other: `RemoteCaps.Signatures` (`formats.SignatureFetcher`, a remote adapter that fetches its signatures in the
background: OCI, npm) and `Capabilities.SignatureCoverage` (`formats.SignatureCoverage`, a format whose signatures cover an index:
Debian). `Host.Signatures()` answers what a format asks of the verdicts before it serves a signature (`formats.SignatureVerdicts`;
`formats.NoSignatureVerdicts` until the trust step is wired), and `Host.Signatures().Refuse` applies the gate per repository to a stored
document the format serves without the item store (a remote OCI repository's tag, R-SIG-064); every item served through `Items().Serve`
passes the gate without a line of the format. A format with a background fetch queues its targets through its data port from the storing
transaction. Quader's own registry signatures (stage 2: npm's `dist.signatures` of a local version and the keys of a local repository)
reach the format through its data port as well, not through the contract ([signatures.md](signatures.md#package-2-of-stage-2-built),
Q-SIG-156): a future format whose clients check a registry's signature reads them the same way.

## Changing the contract

The contract is frozen since the end of M3 (E3.3; version 1, and version 2 since the project model, ADR-156, [ADR-141](../decisions.md#adr-141--the-format-contract-is-frozen-at-version-1)): it only grows,
and every change of its exported surface is an ADR (R-ARCH-025, Q-ARCH-256, Q-ARCH-280). Ordered steps for a new behaviour:

The additions of [ADR-161](../decisions.md#adr-161--the-format-contract-gains-the-registry-root-the-upstream-handshake-head-revalidation-and-upload-sessions)
show the three kinds: `Host.Uploads()` and `Ingestor.Link` are services (the production host implements both over `internal/uploads` and
the item store, [oci.md](oci.md#upload-sessions-r-stor-011); the in-memory host implements `Link` and takes an `UploadSessions` in `HostOptions.Uploads`),
`Authenticator` and `Revalidator` are optional interfaces, and `catalog.Facts.RegistryRoot`, `DefaultCredentialHosts` (since version 4
`UpstreamPresets`, [ADR-198](../decisions.md#adr-198--the-format-catalog-names-a-formats-upstream-presets-and-the-contract-is-at-version-4)),
`Refusal.Base` and `SetupData.ImagePrefix` are facts that every layer reads.

1. Ask first whether it is a service a format calls (a method of `Host`, `PackageWriter` or a store): adding one breaks no format. Do that, with the in-memory twin in
   [store.go](../../internal/formats/formattest/store.go) and the production implementation in `internal/formathost`, and stop.
2. If a format must implement it, and every format answers it, add the method to `Format` (or to an adapter) together with its default in `Base` (or `RemoteBase`).
3. If only some formats have it, or the core cannot call it yet, declare an optional interface in `internal/formats`, add a field to `Capabilities` and the assertion to
   `CapabilitiesOf` ([formats.go](../../internal/formats/formats.go)): that function is the one place the architecture test allows to assert to an interface of the package.
   The core reads `formats.CapabilitiesFor(id)`.
4. Write the block into R-ARCH-008 or R-ARCH-009 of [architecture.md](../spec/architecture.md) in the same change (`TestR_ARCH_008_TheWrittenContractEqualsTheCode` fails otherwise).
5. Add the kit check, or extend one, with a named defect in `testformat` that fails it.
6. Raise `ContractVersion` only if the meaning of an existing method changed, a signature changed or a member left, and apply that to every format in the same change.
7. Write the ADR, and regenerate the surface file in the same change: `go test ./internal/formats -run TestR_ARCH_025 -update-contract`. Until then
   `TestR_ARCH_025_TheFrozenContractChangesOnlyWithItsSurfaceFile` fails with one `-`/`+` line per changed member of
   [testdata/contract.txt](../../internal/formats/testdata/contract.txt); an exported function or value also needs its name on a spec page
   (`TestR_ARCH_008_EveryExportedFunctionAndValueOfTheContractIsNamedInTheSpec`).

## How to add a format

1. Write the spec page `docs/spec/formats/<id>.md` (routes, grammar, error shape, hooks, tables) and its decisions, in the numbers the owning plan assigns.
2. Create `internal/formats/<id>/` with a type that embeds `formats.Base`, implements `Signatures()` (the format's row of
   [signatures.md](../spec/signatures.md) R-SIG-002, a detector and `Gate`, the classification of what the signature gate of a repository
   judges (R-SIG-063: an artifact with what covers it, a carrier of signatures, a signature file, or nothing), and `AttestationSubjects`, the
   stored items Quader's receipt attestation is about with the declaration's `Attests` (R-SIG-044: the artifacts a client fetches, never a
   carrier of a signature), or `formats.NoSignatures(reason, attestsNone)`, plus its cases of kit check 18 (`Signatures` and `GateCases` with
   `Attested`), its row in the table, which
   `TestR_SIG_002_EveryFormatDeclaresItsRowOfTheSpecification` holds, and its row in R-SIG-063's table; [signatures.md](signatures.md)) and `Descriptor` (facts, `ContractVersion: formats.ContractVersion`,
   `Names` if it has package names, `PropertyPrefix`, `DataPort` if it has side tables, `VersionFetch` if it has package screens: what a client
   fetches to install a version, and add the format with its rule to the test of [all_test.go](../../internal/formats/all/all_test.go)), `Routes`, `Match` (one call of `formats.MatchRoutes`), `Classify` and
   `PackageDocumentPath`, then what it needs of the hooks, the adapters (`Remote()` exactly when `Classes` lists `Remote`, `Virtual()` likewise), `Exposure()` and `WriteError`.
   A format with the class `remote` declares its probes in `Descriptor.Probes`, calls `rq.Host.Remote().Probed` in each probe's handler once
   it answered (with the files the answer names), implements `RemoteAdapter.ProbeContent` (`formats.ProbeFiles(p)` when a version is one
   file), adds its row to `TestR_REPO_065_EveryFormatDeclaresItsProbes` in [all_test.go](../../internal/formats/all/all_test.go) and a unit
   test of its probes with `formattest.MemRemote`, whose `ProbesSeen` lists what the handlers told it ([proxy.md](proxy.md#a-probe-starts-the-download)).
   With the class `virtual` too, its virtual handlers call `Probed` on the same requests with the virtual request (or a member's, when they
   answered from a member's own handler); `ProbedOnSeen` says which, and `TestQ_REPO_367_AProbeIsAProbeThroughAVirtualRepositoryToo` holds
   that every probe route serves the class `virtual` (Q-REPO-367).
   A format with the class `remote` also declares its upstream catalogue (`Descriptor.Catalogue`) and its version canon
   (`Descriptor.VersionCanon`) and adds its rows to `TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue` and
   `TestR_CUR_006_EveryRemoteFormatHasItsVersionCanon` in [catalogue_test.go](../../internal/formats/all/catalogue_test.go)
   ([remote-curation.md](../spec/remote-curation.md) R-CUR-023).
   Give every route at least one example; a name that two routes could both match needs a `Route.Match` that tells them apart.
   A `Download` route whose clients all check a digest declares it in `Route.ClientDigest`, with its evidence, and gets its row in
   `TestR_STOR_071_EveryFormatDeclaresWhetherItsClientsCheck` ([What a signed link needs of a format](#what-a-signed-link-needs-of-a-format));
   without evidence it declares nothing, and every format has its row either way.
   If it has settings: embed the schema, set `Facts.SettingsSchema`, add the member to `repository.Settings` and to `formatMembers`, the schema to `api/openapi.yaml`
   (`<Format>Settings`, referenced from `Repository`) and read the member with defaults ([Format settings](#format-settings)).
   `Names.Normalize` is the widest rule the format accepts anywhere (a remote may serve a name a local repository would refuse): `!ok` means the string is no name of the
   format, and routing then refuses it instead of looking up the spelling as written ([virtual.md](virtual.md)); a rule only local repositories apply belongs in `Inspect`.
   A route of kind `Upload` must not accept a write to a path that `Classify` marks computed (`Register` refuses it, R-ARCH-012); a change of rows that no client uploaded, such as a dist-tag, is an `Admin` route.
3. If it has side tables: add the migration ([database.md](database.md)), the port interface in the format package, the implementation in
   `internal/formatdata/<id>` (domain layer, sqlc statements in `queries/`), and its constructor in `formatPorts` in
   [wire_formats.go](../../internal/app/wire_formats.go).
4. Register it in [internal/formats/all/all.go](../../internal/formats/all/all.go).
5. Add the format's enum entries to `api/openapi.yaml` (`FormatName` and the schemas of its classes, the block `# >>> WP2.1 formats`);
   `TestR_ARCH_010_PerClassFormatEnums` fails until they match, then `make generate`.
6. Write the conformance test with the kit (`formattest.Run`), the fixture of the format and its fuzz targets ([Conformance kit](#conformance-kit)), and the integration tests against the real stack.
   A format with clients writes its setup templates and its guide ([The guide of a format](#the-guide-of-a-format)) in the same change: check 8 fails without them.
   Every format, a system-only one too, adds its client IDs to the table of the subtest "the formats list names the clients" of
   `TestR_API_053_TheGuideThroughTheStack` ([guide_test.go](../../test/integration/packages/guide_test.go)), which fails for a format it does not
   know, and runs the integration area `packages` once.
7. Update this page, [repositories.md](repositories.md) (the format registry section) and the README's pages table.

## What this page does not cover

- The kit does not run the dispatcher, the authorization decision or the real item store: it runs the format against an in-memory `Host`. That the production host
  behaves like the in-memory one is tested by the integration tests of `test/integration/files`, not by the kit.
- The remote engine, package ownership, the namespace auto claim and the virtual engine are built and bound by the composition root ([wire_formats.go](../../internal/app/wire_formats.go)). The virtual engine exists ([virtual.md](virtual.md)) and the Host takes it as `Options.Resolver`
  through `formathost.NewResolver`; `MemberDocument.Restricted` and `ShadowOutcome.Refusal`/`Shadowed` are the two fields it added to the contract.
- What the contract lacked for NuGet (a hook on a remote fill, discovery documents, a pass-through for volatile requests, validation of remote settings, an `Inspect` on the raw routes) was the work package of
  roadmap M3, "Contract additions before the freeze" ([roadmap.md](../coverage/roadmap.md)), whose every row is built or decided. A format never approximates such a seam over `rq.Repo.Members()` and the stores: that skips the per-member read check (R-AUTHZ-009 rule 2) and the pin and claim rules.
- The dormant methods that could not be implemented as frozen (`IndexBuilder.Rebuild`, `RemoveVersion`, `VersionGroup`, `Enqueue`, the `RepositoryIndex` descriptor field) are not in the frozen contract, and `OnItemsRestored` is an optional interface (Q-ARCH-256); they join it by the rule below.
