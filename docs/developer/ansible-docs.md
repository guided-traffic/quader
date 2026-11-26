# Ansible Galaxy: the documentation of collections

The documentation of a collection version (the README, the Markdown files of `docs/`, and one entry per module, plugin and role) is derived
data that Quader generates for a local repository, keeps for a remote one and reads from the winning member for a virtual one. The
requirements are [R-ANS-033 to R-ANS-046](../spec/formats/ansible.md), the screens are R-UI-060 to R-UI-064
([api-and-ui.md](../spec/api-and-ui.md)), the decisions [ADR-196](../decisions.md) and [ADR-197](../decisions.md), and the threats
[docs/security/ansible-documentation.md](../security/ansible-documentation.md).

## What is built and what is not

The backend and the screens are built; the end-to-end rows and the browser journey of the set `ansible` are not written yet. Where a part is a stand-in it says so.

| Part | Where | State |
|---|---|---|
| The neutral form, the file title rule | [rows/docs.go](../../internal/formats/ansible/rows/docs.go), [rows/doctitle.go](../../internal/formats/ansible/rows/doctitle.go) | built |
| The generator: the literal scanner, the YAML node walker, the shaping of options and return values, the bounds | [docgen](../../internal/formats/ansible/docgen) (`scan.go`, `shape.go`, `docgen.go`) | built; executes nothing (a test lists its dependencies) |
| The converter of the Ansible markup | [docgen/markup.go](../../internal/formats/ansible/docgen/markup.go) | built; escapes first, a fuzz target holds the tag set and the addresses |
| The links of the documentation to itself (R-ANS-048): `docgen.Links` (the collection, the tree, the entry's option and return paths), `docgen.PageLink` (the one grammar of the addresses), `docsRead.links` and `keepPageLink` (the tree read once and only for an entry that names another), `Renderer.SanitizeHTMLLinked` | [docgen/markup.go](../../internal/formats/ansible/docgen/markup.go), [api/v1/ansible_docs.go](../../internal/api/v1/ansible_docs.go), [readme/render.go](../../internal/packages/readme/render.go) | built |
| The configuration of an option, `version_added_collection`, `positional` (R-ANS-049, R-ANS-050) | `configKeys` in [docgen/shape.go](../../internal/formats/ansible/docgen/shape.go), `docKeys` in [galaxydoc/docs.go](../../internal/formats/ansible/galaxydoc/docs.go), `configurationOf` and `markupHTML.added` in [api/v1/ansible_docs.go](../../internal/api/v1/ansible_docs.go) | built |
| The fragments of the collection (R-ANS-051): `scanFragment` reads the literal attributes of `class ModuleDocFragment`, `fragmentOf` resolves a name of `extends_documentation_fragment`, `mergeFragment` merges as ansible-doc | [docgen/scan.go](../../internal/formats/ansible/docgen/scan.go), [docgen/fragments.go](../../internal/formats/ansible/docgen/fragments.go) | built; proven against `ansible-doc --json` of the fixture [docgen/testdata/acme_tools](../../internal/formats/ansible/docgen/testdata/acme_tools) |
| A role's argument specs (R-ANS-052): `entryPoints` shapes them into `doc.entry_points`; `entryPointsOf` answers them with the links of `Links.EntryPoint` | [docgen/argspecs.go](../../internal/formats/ansible/docgen/argspecs.go), [api/v1/ansible_docs.go](../../internal/api/v1/ansible_docs.go) | built; the same fixture |
| The changelog (R-ANS-053): `ChangelogReader` (a tap of its own, embedded in the generator and used alone by `InspectFill` of a remote), the rendering of changelog.yaml; the table, `PutChangelog`, `DocChangelog`, `RebuildChangelogs`; `ReindexPort`, the entry of the task `remote-describe` that queues the reindex of what an older generator made ([reindex_port.go](../../internal/formatdata/ansible/reindex_port.go), Q-ANS-99) | [docgen/changelog.go](../../internal/formats/ansible/docgen/changelog.go), [changelog_store.go](../../internal/formatdata/ansible/changelog_store.go), migration `ansible_changelogs` | built; `docgen.Version` 3 |
| The anchors, the contents, the sections of filter, test and lookup plugins, the highlighted YAML | `rowAnchor`, `parameterGroups`, `entrySections`, `configLines` in [docs-model.ts](../../web/src/app/features/packages/docs-model.ts); [ansible-doc-entry.ts](../../web/src/app/features/packages/ansible-doc-entry.ts); [shared/yaml-highlight.ts](../../web/src/app/shared/yaml-highlight.ts); the `docLinks` input of [package-readme.ts](../../web/src/app/features/packages/package-readme.ts) | built |
| The tap of the archive reader | [collection/tap.go](../../internal/formats/ansible/collection/tap.go); `Generator` implements it | built |
| The port group `DocsStore` and its twin | [store.go](../../internal/formats/ansible/store.go), [ansibletest/docs.go](../../internal/formats/ansible/ansibletest/docs.go) | built |
| The PostgreSQL implementation, the statements, the reindex step | [docs_store.go](../../internal/formatdata/ansible/docs_store.go), [queries/ansible_docs.sql](../../internal/meta/postgres/queries/ansible_docs.sql), [reindex.go](../../internal/formatdata/ansible/reindex.go) | built |
| The five tables | migrations `ansible_doc_tables`, `ansible_doc_fetches` and `ansible_changelogs`; registered `transient` in [doc_tables.go](../../internal/formatdata/ansible/doc_tables.go) | built |
| The publish | `Inspect` runs the generator in the one pass; `OnCommit` puts the rows with the version; `OnItemsRemoved` removes them; the warnings are messages of the import task | built |
| The upstream parser and the document renderer | [galaxydoc/docs.go](../../internal/formats/ansible/galaxydoc/docs.go) | built |
| `Host.Markup`, `NoMarkup`, the kit's stand-in, the composition root's value | [markup.go](../../internal/formats/markup.go), [formattest/markup.go](../../internal/formats/formattest/markup.go), [app/wire_markup.go](../../internal/app/wire_markup.go) | built |
| The route `ansible.version.docs` | [handlers_docs.go](../../internal/formats/ansible/handlers_docs.go) (local, virtual), [handlers_remote_docs.go](../../internal/formats/ansible/handlers_remote_docs.go) (remote) | built |
| The remote fill | `PrepareFill`, `OnFill`, `queueDocs` and `docsAddress` in [remote.go](../../internal/formats/ansible/remote.go) | built |
| The job `ansible-docs-fill` | [jobs.go](../../internal/formatdata/ansible/jobs.go), [docs_job.go](../../internal/formatdata/ansible/docs_job.go), wired in [wire_ansible.go](../../internal/app/wire_ansible.go) | built |
| The setting `ansible.fetchDocumentation` | [settings.schema.json](../../internal/formats/ansible/settings.schema.json), `AnsibleSettings` in [api/openapi.yaml](../../api/openapi.yaml) | built |
| The three operations, `metadata.docs`, the filters of `listAnsibleImportTasks` | [api/v1/ansible_docs.go](../../internal/api/v1/ansible_docs.go), [ansible_tasks.go](../../internal/api/v1/ansible_tasks.go) | built |
| The wiki seam | [wikigate.go](../../internal/formats/ansible/wikigate.go) (`Host.WikiReadable`) and `Scope.MayReadWiki` in [ansible_docs.go](../../internal/api/v1/ansible_docs.go) | built: the wiki check of R-AUTHZ-034, since task T-18 |
| Metrics | [docs_metrics.go](../../internal/formats/ansible/docs_metrics.go) | the route and the fills of the request path; see H-903 |
| The counting stub | [test/ansiblestub](../../test/ansiblestub) (`docs-blob/`, the modes `docs`, `docs_foreign_href`) | built |
| The Documentation, Contents and Import log tabs, the Dependencies links | [web-ui.md](web-ui.md#the-ansible-documentation-screens) | built against the generated types and the fakes; not yet seen in a browser, the browser journey of the set `ansible` is not written yet |

## The paths

**Publish.** `Inspect` gives the archive reader a `docgen.Generator` as its tap: the reader streams every member through the digest and hands the generator the first bytes of the members it wants; a Python source is scanned for its three literals at once (a fragment for the attributes of its class) and dropped. `Build(readmeName, collection)` merges the collection's fragments, reads the roles' argument specs and the changelog, and makes the `rows.DocSet`, which travels in the `Inspection.Payload` to `OnCommit`, where `PutChangelog` and `PutDocs` write it in the commit transaction.

**Changelog of a remote.** `InspectFill` reads a cached archive with a `docgen.ChangelogReader` as its tap; `CommitFill` writes the changelog (or the row `none`) after the version row. The reindex of a remote (`remoteChangelogs` in [reindex.go](../../internal/formatdata/ansible/reindex.go)) reads it again from the stored archive for a row that is missing or older; the task `remote-describe` queues that reindex through its Ansible Galaxy entry (`ReindexPort`, wired in [wire_remote_describe.go](../../internal/app/wire_remote_describe.go)), and a worker that starts queues the task while a repository is left, so raising `docgen.Version` needs no operator.

**Remote.** The fill of a version document records the address of the documentation as a URL-map row and, with `fetchDocumentation` on, queues the job (`queueDocs`); the job asks the engine for the stored document, whose `PrepareFill` parses it and whose `OnFill` writes the rows. A client's request for the route takes the same path inline. The route renders from the parsed stored document through the Host's sanitizer; the stored bytes stay as received.

**Virtual.** `virtualDocs` takes the resolver's plan and tries the members by tier and position (Q-ANS-74).

**Screens.** `resolveDocs` takes the repositories of the version details, local before remote, and the first whose state is not none (Q-API-264).

**Links.** The entry operation converts the prose with a `docgen.Links` of the entry: the paths of its options (with aliases) and return
values, and, only when the entry's source names another entry (`M(`, `P(`, a `#`, a see-also list), the names of the version's tree, read
once. The converter writes `?doc=<type>/<name>`, `#parameter-<path>` or `#return-<path>` only for a name it found there; the sanitizer
keeps a relative link only when `docgen.PageLink` matches it. In the browser, `DocProse` binds the prose with `docLinks`: the README
component keeps a link of the same grammar (`DOC_LINK`), gives it the page's address (`Router.createUrlTree` with the entry and the
fragment) and opens it in place. The entry page scrolls to the fragment of the address (`target`, from `ActivatedRoute.fragment` of the
Documentation tab) once the row is drawn, unfolds a row above it and marks it. **Add a macro that links:** the target's name is checked
in `reference` of `markup.go`, the address is one of `PageLink`'s grammar (or the grammar grows in `markup.go` and in `DOC_LINK` together),
and the table of `TestR_ANS_048_TheMacrosLinkOnlyToWhatTheTreeHolds` gets a case with and without the target.

## The wiki seam

Who may read a repository's artifacts may not be who may read its documentation (R-ANS-037). The format asks one function,
`wikiReadable(ctx, rq, target) bool` in [wikigate.go](../../internal/formats/ansible/wikigate.go), which calls `rq.Host.WikiReadable`, the
question form of the wiki check of R-AUTHZ-034; a handler that gets false answers as an absent resource. The route `ansible.version.docs`
is declared `Wiki` in [routes.go](../../internal/formats/ansible/routes.go), so the dispatcher refuses a caller who may not read the wiki
before the handler runs; the handlers ask again for the repository they read from, which for a virtual repository is a member. The three
management operations carry the marker `x-quader-part: wiki` in [api/openapi.yaml](../../api/openapi.yaml) (read into `Operation.Part` by
[document.go](../../internal/api/v1/document.go)) and decide each candidate repository with `Scope.MayReadWiki` in
[ansible_docs.go](../../internal/api/v1/ansible_docs.go), the caller's `authz.ReadScope.MayReadWiki`; the announcement `metadata.docs` of
the version details goes through the same function, so it is absent for a caller who may not read the wiki. `registerWikiSurface` lists
the surfaces that serve documentation, `TestR_ANS_037_EverySurfaceAsksTheWikiGate` walks the list, and check 15 of the format kit serves
every content case again for a caller who reads the artifacts and not the wiki.

## The Markup capability

A format may import neither the README renderer nor an HTML library (R-ARCH-002), yet the document it serves holds HTML members. The Host
hands it `Markup()` (ADR-196): `Markdown(src)` renders under the policy of R-UI-008 and `SanitizeHTML(html)` applies the allowlist to HTML
somebody else wrote. The composition root builds the value over `internal/packages/readme` with the operator's `ui.readme` keys
(`App.hostMarkup`). A Host without a renderer answers `formats.NoMarkup`, whose calls return the empty string. The conformance kit's Host
answers `formattest.StandInMarkup`, which escapes: a unit test that uses it proves the wiring, not the policy (H-898).

## Tests

| Test | Proves |
|---|---|
| `TestR_ANS_034_*` in [docgen](../../internal/formats/ansible/docgen) | the generator over crafted sources, the bounds, the determinism, `FuzzR_ANS_034_Generator` |
| `TestR_ANS_039_TheMarkupTableHasACasePerMacro`, `FuzzR_ANS_039_RenderMarkup` | the converter, with and without the links of a tree |
| `TestR_ANS_048_*`, `TestR_ANS_049_*` in [docgen](../../internal/formats/ansible/docgen) and [api/v1](../../internal/api/v1/ansible_docs_links_test.go) | the links only to what the tree holds, the grammar of the addresses, the tree read once, the configuration and the collection of a version |
| `TestR_ANS_051_*`, `TestR_ANS_052_*`, `TestR_ANS_053_*` in [docgen](../../internal/formats/ansible/docgen/content_test.go), [the format](../../internal/formats/ansible/publish_changelog_test.go) and [api/v1](../../internal/api/v1/ansible_docs_links_test.go) | the fragments and the argument specs against ansible-doc's own output for the fixture, the changelog's rendering, sources and bounds, the publish and the fill, the entry points' anchors |
| `test/integration/ansiblelocal/docs_content_test.go` | the operations answer the merged option, the entry points and the changelog; the task remote-describe, queued by a starting worker, queues the reindex of a changelog an older generator recorded |
| `yaml-highlight.spec.ts`, `docs-model.spec.ts`, `ansible-doc-entry.spec.ts`, `ansible-docs.spec.ts`, `package-readme.spec.ts` (R-ANS-048 to R-ANS-050) | the tokenizer, the anchors, the target, the sections, a prose link opened in place, the README keeping no `?doc=` link |
| `test/integration/ansiblelocal/docs_test.go` | publish, the operations, the route, the reindex (rows that are missing and rows of an older generator, R-ANS-044), the warnings, the metrics of the generation and the reads (R-ANS-045) |
| `test/integration/ansibleremote/docs_list_test.go` | the changelog of a cached archive, and its row read again from the archive by the reindex the task remote-describe queues (R-ANS-053) |
| `test/integration/ansibleremote/docs_test.go` | the fetch once, the sanitized serving, the states, the setting, a foreign `href`, the stored document evicted while its rows stay (R-ANS-043), the metrics of the fill (R-ANS-045) |
| `test/integration/ansiblevirtual/docs_test.go` | the first member that has documentation |
| `test/integration/ansiblevirtual/wiki_test.go` | a caller who reads the artifacts and not the wiki of the local member: the virtual repository serves the remote member's documentation, the local member's route answers 404, its version document 200 |
| `TestR_ANS_021_APassOfTheCollectionIndexStoresTheCollectionsThePatternsSelect` in [mirror/ansible_test.go](../../test/integration/mirror/ansible_test.go) | a mirror pass queues the documentation fetch of every version it keeps (R-ANS-040) |

Held at the unit tier and not repeated at the integration tier (the rule of [CLAUDE.md](../../CLAUDE.md): once, at the cheapest tier):

- **A collection of 6,000 files.** The bound of 5,000 entries and `truncated` is a rule of the generator, which a publish runs in its one pass:
  `TestR_ANS_034_R_ANS_043_TheBoundsOfAVersionKeepTheListAndDropTheRest`. An integration case would publish an archive of 6,000 members
  to show the same function's answer.
- **A local repository's gate for a caller who may not read the wiki.** The route is declared `Wiki` and the dispatcher refuses the caller
  before the handler runs (`TestR_AUTHZ_034_ARouteThatServesOnlyWikiContentIsDecidedWithThePartWiki`), every surface asks `wikiReadable`
  (`TestR_ANS_037_EverySurfaceAsksTheWikiGate`), and check 15 of the kit serves every content case to such a caller. With the real decision
  point the refusal is part of `wiki_test.go`, at the local member's own route.
| `TestR_ANS_033_TheTwinReportsTheStatesOfTheDocumentation`, `TestR_ANS_036_TheTwinsTreeKeepsTheCountsWhileASearchFilters` | the rules the twin shares with the SQL store |
| `TestR_ANS_034_TheTapSeesTheFirstBytesOfAWantedMember` | the tap sees the capped bytes of the members it wants and changes nothing else |
| `TestR_ANS_035_TheDocsBlobDocumentIsMetadataOfItsVersion`, `TestR_ANS_003_TheRouteTable` | the route and the canonical path of the stored document |
| `TestR_ANS_037_EverySurfaceAsksTheWikiGate`, `TestR_ANS_037_TheDocumentListsTheWikiOperations` | the seam and the marker |
| `TestR_ANS_039_SanitizeHTMLAppliesTheREADMEPolicyToForeignHTML`, `TestR_ANS_039_TheHostMarkupRunsThePolicyOfTheREADMERenderer` | the one policy behind `Markup` |
