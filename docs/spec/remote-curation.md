# Curating a remote repository

Status: specified on 2026-11-23; its contracts (the tables, the types, the codes, the events, the keys) are built, the delivery rules (R-CUR-001
to R-CUR-006, R-CUR-017, package R2) are built with what [Q-CUR-42](#q-cur-42--the-places-the-rules-decide-as-built-kit-check-23-oci-and-debian-in-the-kit)
lists as not built, the downloads ahead with their queue (R-CUR-010 to R-CUR-013, package R4), the examination, the requests and the retention
(R-CUR-014 to R-CUR-016, package R2b, with what [Q-CUR-140](#q-cur-140--an-examination-row-names-the-files-its-runs-stored-a-discard-removes-what-belongs-to-the-version-alone)
to [Q-CUR-148](#q-cur-148--every-persons-ask-is-recorded-the-last-asker-who-withdraws-ends-an-entry-no-client-was-refused) decide), the upstream catalogue
(R-CUR-007 to R-CUR-009, package R3, with what [Q-CUR-62](#q-cur-62--debian-searches-the-name-provides-and-the-short-description-the-long-descriptions-and-read-the-index-wait)
lists as not built), and the screens (R-CUR-018): the tab strip and the tab **Downloads** (package R5a), the tab **Upstream** and the
settings' section **Delivery** (package R5b, [Q-CUR-150](#q-cur-150--the-strip-holds-the-content-upstream-and-downloads-for-every-signed-in-reader-the-strip-counts-the-open-requests-itself) to
[Q-CUR-155](#q-cur-155--the-catalogues-own-states-are-sentences-of-the-screen-the-upstreams-own-sentences-stand-as-the-upstream-wrote-them)),
and the end-to-end case of each format, the journey of the tab Upstream and the manual's page (package R6,
[Q-CUR-160](#q-cur-160--an-oci-referrers-tag-is-no-version-it-follows-its-subject) to
[Q-CUR-163](#q-cur-163--the-background-engine-of-the-role-api-shares-the-caches-of-its-read-only-view));
the gauge `quader_remote_delivery_requests_open` and **Read the index** (Q-CUR-62) are not. The owner asked for it on 2026-11-23 (task T-24 of the
[project plan](../projektplan/promt.md)), at 12:38:

> In Repositories, die Remote sind, fehlt mir dafür ein Reiter um das Remote zu durchsuchen. Manche Varianten von
> remote-Repositories lassen sich ggf. nicht duchsuchen, dass musst du dann vom Repository-Kind abhängig machen. Hier will ich also
> einen Reiter. Es soll damit möglich sein, das remote zu durchsuchen und Downloads in den Pull-Trough direkt über den Browser zu
> triggern. Gerne auch Bulk, in verschiedenen Stufen. Eine ganze Sub-Sektion, alle Versionen eines Artefakts oder nur eine Version
> eines Artefakts, das muss alles wenn technisch möglich über das GUI abrufbar und bedienbar sein.
>
> In manchen Firmen will man bei einem Pull Through auch gezielt nur Teile des Remotes intern Freigeben. Wir brauchen in bei
> Remote-Repos als ein Whitelisting und Blacklisting Konzept. Default ist Blacklisting, alles kann gepullt werden, außer Artefakte
> die gezielt disabled wurden. Es gibt aber auch ein Whitelisting Konzept, bei dem zum Beispiel in einem Unternehmen nur bestimmte
> python-libaries und auch nur in bestimmten version an die mitarbeiter ausgeliefert werden. Ein Admin, Owner oder Maintainer eines
> sollchen Repos kann dafür in den Remote-Strukturen stöbern, alle Versionen (+ Zukünftige) einer Libary freigeben, oder nur
> bestimmte Versionen. Außerdem kann der Download von bestimmten Artefakten oder ganze Bereiche eines Remotes getriggert werden,
> damit direkt mit dem aufbau des Caches begonnen wird bevor der erste Client dies angefragt hat.
>
> Remote-Repos benötigen auch einen Reiter mit dem der User sich die aktuelle Download-Queue des Remote-repos anschauen kann um
> während des Aufbaus eines Caches begleiten zu können. Hier müssen auch Fehler beim Download technisch präzise beschrieben sein und
> ein Re-Trigger muss möglich sein.
>
> Suchfunktion, ganz klar, ich will im Remote Repo suchen können wenn technisch möglich. Also wenn ich mit apt search suchen kann,
> können wir das bitte auch im Frontend. ich will im PIP remote nach python-gitlab suchen können, das Paket finden und auch die
> Versionen angezeigt bekommen.

At 12:57:

> Sollte ein Repo auf Whitelistig gestellt sein, also Libaries müssen "freigegeben" werden damit der Cache diese anbietet, kann ein
> Nutzer der selber nicht whitelisten kann, eine lib oder lib+version "beantragen", berechtigen Usern wie Administratoren oder
> Maintainern wird diese dann in diesem Repo angezeigt/hervorgehoben, nach der Freigabe durch eine berechtigte Person startet quader
> den download direkt.

At 13:01:

> oder wird der Download ggf. direkt gestartet um die lib durch quader untersuchen zu können? Klingt auch gut. Es muss auch möglich
> sein, für ein Maintainer, eine Version und alle Versionen einer Lib herunterladen zu können, ob diese auf vulns und malware
> untersuchen zu können, ohne direkt den Download dieser lib zu ermöglichen. Eine Art pre-cache für untersuchungen.

His word of 08:09 the same day stands: no new format is built. PyPI, his example, is not specified here; this page shows that its
design carries PyPI and three other formats by one table (R-CUR-023), and the five formats with a remote class today (npm, NuGet, OCI,
Debian, Ansible Galaxy) are specified in full, each on its own page.

This page specifies, for every remote repository of every format:

- **the delivery rules**: a remote delivers everything except what is blocked (the default) or only what is allowed; a rule allows
  or blocks a sub-section of the upstream, a package with every version (future ones included) or one version; the narrowest rule
  decides. The include and exclude patterns of a remote become rules (R-CUR-001 to R-CUR-006);
- **the refusal** that a rule gives a client, in each format's shape, its place before a fill, at the serve point and in documents,
  through a virtual repository and a Bastion (R-CUR-003, R-CUR-004);
- **the upstream catalogue**: a port of the format contract with declared capabilities (search by the upstream, search by Quader over
  an index it holds, levels, lookup by exact name, versions with what the upstream tells of each), its budget, its cache and its bounds
  (R-CUR-007 to R-CUR-009);
- **downloads ahead of a client** at three levels (a version, every version of a package, a sub-section), and the repository's
  **download queue** with its states, its failures in the failure's own words and its restart (R-CUR-010 to R-CUR-013);
- **fetching for examination**: content stored and scanned while the rules do not deliver it, with each scan's verdict beside the
  decision, and how long it lives (R-CUR-014, R-CUR-016);
- **requests**: a user who may not allow asks for a package or a version; those who may allow see the requests, with what clients
  asked for and the rules refused, and an allow starts the download (R-CUR-015);
- **future versions** and the mirror (R-CUR-017), the screens (R-CUR-018), audit events and metrics (R-CUR-019), configuration
  (R-CUR-020), replicas and backups (R-CUR-021), the tests and what they cost (R-CUR-022).

| Topic | Owner |
|---|---|
| The mirror mode, the remote request path, virtual resolution, the probe's background download, the journal of narrowings | [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-019, R-REPO-024, R-REPO-025, R-REPO-050 to R-REPO-061, R-REPO-065 |
| Who may populate a cache; the repository actions | [authorization.md](authorization.md) R-AUTHZ-006, R-AUTHZ-008 |
| The combined serve decision of the malware gate, the vulnerability block and the signature gate | [signatures.md](signatures.md) R-SIG-069 |
| The vulnerability miss gate | [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015 |
| The download limits and the charge of a background download | [usage-and-limits.md](usage-and-limits.md) R-LIM-037, Q-LIM-19 |
| The repository page and its tabs; the listing of a remote | [api-and-ui.md](api-and-ui.md) R-UI-052, R-API-081 |
| What each format's catalogue reads and how it filters its documents | [formats/npm.md](formats/npm.md) R-NPM-049, [formats/nuget.md](formats/nuget.md) R-NUGET-035, [formats/oci.md](formats/oci.md) R-OCI-038, [formats/debian.md](formats/debian.md) R-DEB-036, [formats/ansible.md](formats/ansible.md) R-ANS-047 |
| The revocation feed of a Bastion | [bastion.md](bastion.md) R-BAS-015 |
| The security view of this page | [../security/remote-curation.md](../security/remote-curation.md) |
| The work packages, their files and their tests | [../developer/remote-curation.md](../developer/remote-curation.md) |

## Terms

| Term | Meaning |
|---|---|
| Upstream catalogue | What a person sees of a remote's upstream through Quader's management API: a search, the levels above a package, a lookup by exact name, the versions of a package with what the upstream tells of each. It is never a package protocol route and never stores into the cache (R-CUR-008). |
| Delivery mode | `allExceptBlocked` (the default: every package and version is delivered unless a rule blocks it) or `onlyAllowed` (nothing is delivered unless a rule allows it). A member of the repository's configuration (R-CUR-001). |
| Delivery rule | A subject (a sub-section, a package, or a version of a package) and an effect (`allow` or `block`), a row of the repository (R-CUR-001, R-CUR-006). |
| Sub-section | A name pattern of R-AUTHZ-005 in the format's name rule: an npm scope (`@acme/*`), a NuGet ID prefix (`Contoso.*`), an image namespace (`library/*`), a Galaxy namespace (`community.*`), a Debian name prefix (`lib*`). As a rule's subject it always matches names; as the level of a download it is a level the catalogue lists (R-CUR-010). |
| Delivered | The rules let a version reach clients in this repository. A gate (the malware gate, the vulnerability block, the signature gate) may still refuse a delivered version; those are separate checks with separate deciders (R-CUR-014). |
| Download ahead | A download into the cache that no client's request caused: started by a person through the management API, by an allow, by a request, by a bulk, or by a probe (R-REPO-065). It does not depend on the rules: what it stores and the rules do not deliver is stored for examination (R-CUR-014). |
| Queue row | One version of one package that a download ahead fetches, a row of `remote_download` (R-CUR-011). A bulk is expanded into queue rows. |
| Request | A person's ask that a package or a version be allowed; one entry per subject, with everyone who asked and what clients asked for and the rules refused (R-CUR-015). |

## Requirements

### R-CUR-001 · The delivery mode and the delivery rules of a remote repository
Priority: **P1** · Covers: SEC-007

- **The mode** is the member `remote.delivery.mode` of a remote repository's document (R-REPO-024): `allExceptBlocked` (default) or
  `onlyAllowed`. The owner's two concepts, "Blacklisting" and "Whitelisting"; the words of the API and the screens are those of
  Q-CUR-2.
- **A rule** has a subject and an effect:

  | Subject (`kind`) | Holds | Matches a package name and version when |
  |---|---|---|
  | `section` | `pattern`: a name pattern of R-AUTHZ-005, folded by the format's name rule ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-005), matched by the one matcher of Q-STOR-9 | the pattern matches the name |
  | `package` | `name`: one normalized package name | the name equals it; every version, future ones included |
  | `version` | `name` and `version`: the version in the format's canonical form (R-CUR-006) | both equal |

  The effect is `allow` or `block`. A rule has an optional note (at most 1,000 characters) that only those who may read the rules
  see (R-CUR-005).
- **The decision** for a package name `n` and a version `v`, the same everywhere it is asked (R-CUR-003):
  1. the rules of the narrowest kind that matches decide: a `version` rule of `n@v`, else a `package` rule of `n`, else the `section`
     rules whose pattern matches `n`;
  2. among the rules of that kind that match, `block` wins (two sections can match one name);
  3. no rule matches: the mode decides; `allExceptBlocked` delivers, `onlyAllowed` does not.
- **The package's decision** (for a request that names a package and no version: a package document, a version list, a tag list) is
  "delivers some version": the package is delivered when step 1 to 3 at the package level (a `package` rule of `n`, else the sections,
  else the mode) give `allow`, or when a `version` rule of `n` allows. A package that is not delivered is refused whole; a package that
  is delivered has its documents filtered to the delivered versions (R-CUR-003).
- **Examples** (*example*), mode `onlyAllowed`:

  | Rules | `lodash@4.17.21` | `lodash@4.17.20` | `@acme/ui@2.0.0` | `left-pad@1.3.0` |
  |---|---|---|---|---|
  | `package lodash allow` | delivered | delivered | refused | refused |
  | `version lodash@4.17.21 allow` | delivered | refused | refused | refused |
  | `package lodash allow`, `version lodash@4.17.20 block` | delivered | refused | refused | refused |
  | `section @acme/* allow`, `section @acme/ui* block` | refused | refused | refused (the block wins at equal width) | refused |
  | `section @acme/* block`, `package @acme/ui allow` (mode `allExceptBlocked`) | delivered | delivered | delivered (the package is narrower) | delivered |

- A rule is **exact**: a version rule names one version, never a range (Q-CUR-3). To allow a subset of a blocked section, allow its
  packages.
- The mode and the rules bind **every caller**, administrators included, on the package protocols and on `/files/`, as the patterns
  did (R-REPO-005): they say what the repository delivers, not who may read it.

Acceptance criteria:

- Unit (`internal/remotecuration/rules`, milliseconds, a table of about 40 cases): the table above, a section and a package of the same
  name, two sections with different effects, a version rule beside a package rule of the other effect, an empty rule set in both
  modes, the package's decision with only a version rule, a name the format's rule folds (NuGet `Newtonsoft.Json` and
  `newtonsoft.json`), and a version given in a non-canonical form (NuGet `1.0` against a rule on `1.0.0`).

### R-CUR-002 · The include and exclude patterns of a remote repository are rules
Priority: **P1** · Covers: quader-only

- A remote repository has **no** `includePatterns` and `excludePatterns` (R-REPO-005): a write that sets either on a remote answers
  `422 not_applicable` at its pointer, and a remote's document reads back without them. What they said is said by rules: an exclude
  pattern is a `section` rule with `block`; an include list is the mode `onlyAllowed` with a `section` rule with `allow` per pattern.
  The two lists are no second mechanism beside the rules (Q-CUR-4). Local and virtual repositories keep their patterns as R-REPO-005
  says.
- **No migration** carries a stored remote's patterns into rules (the stage's rule, [CLAUDE.md](../../CLAUDE.md)): a remote that held
  patterns before this change delivers everything until its rules are set. Its stored lists are read by nothing: neither the snapshot
  nor its document holds them, so an update of it is not refused for them and the next write stores the section without them
  ([Q-CUR-45](#q-cur-45--a-remotes-stored-patterns-are-not-read-into-its-document-either), `TestR_CUR_002_AStoredRemotesPatternsAreNotRead`).
  A mirror whose bound were those patterns does not run until it has a bound of its own
  ([Q-CUR-43](#q-cur-43--a-mirror-without-its-bound-does-not-run-and-says-so)).
- **What a blocked name loses:** a pattern hid a package from every view, its findings included, because it was not served
  (`internal/packages/view/service.go:194`, `internal/vuln/report/report.go:220`). A rule does not hide: what a remote stores and its
  rules do not deliver stays visible, with its verdicts, to whoever may read the repository's content in the screens and the management
  API, marked as not delivered with the reason (R-CUR-014, R-API-081); only the data path refuses it, and its documents leave it out
  (R-CUR-003). Nothing of a remote has to stay hidden from every view: what it stores came from its upstream and was fetched by a
  decision of this installation (Q-CUR-4).
- **What stays a routing rule:** a remote never asks its upstream for a name that its rules do not deliver, on a client's request
  (R-CUR-003), as it never asked for a name outside its patterns. Only a download ahead asks for it (R-CUR-010, R-CUR-014), never a
  client. The refusal of a name that a global claim covers stays as R-REPO-023 has it, a missing resource, before the rules.
- **The search guard** (`KeepsAway`, R-REPO-023, R-NUGET-016) withholds a forwarded search term that a `section` or `package` rule
  with `block` matches, as it withheld one an exclude pattern matched, and, in mode `onlyAllowed`, every term that no `allow` rule's
  pattern or name covers.
- **The mirror's scope** is `remote.mirror.include` (R-CUR-017), no longer the include patterns.

Acceptance criteria:

- Integration (area `repository`, about 1 s): a `PATCH` of `{"excludePatterns": ["@acme/*"]}` on a remote answers `422 not_applicable`
  at `/excludePatterns`; on a local repository it is stored.
- Integration (area `npm`, inside the case of R-CUR-003): a remote with `section @acme/* block` refuses `npm view @acme/util` with the
  refusal of R-CUR-004 and the counting stub records no request; after an examination download of `@acme/util@1.0.0`
  (R-CUR-014) `GET /api/v1/packages?repository=…` lists it with `available: false` and the reason `blocked`.

### R-CUR-003 · Where the rules decide: before a fill, at the serve point, in documents
Priority: **P1** · Covers: SEC-007

The rule's refusal is a **delivery gate**, in the shape of the signature gate's (R-SIG-061 to R-SIG-074): the item stays stored and
scanned and visible, the data path refuses it with a reason, and a named person changes the answer with a recorded act (Q-CUR-6). It is
asked at three places, always with the decision of R-CUR-001 on the repository's rules as the replica's snapshot holds them:

1. **Before a fill (the miss).** On a client's request for a path that is not stored, the remote request path
   ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-025) asks the rules after the cache lookup and before the offline,
   circuit, negative-cache and populate steps (step 5a): a package document, a version list or tag list of a package that is not
   delivered, and an artifact of a version that is not delivered, answer the refusal of R-CUR-004 without any upstream request,
   whatever the upstream's state. A pass-through query (a search) is not refused; its results are filtered (point 3).
2. **At the serve point.** A stored item is decided in the one decision of the serve points (R-SIG-069) as its fourth check, from the
   name and version the item lookup brings (the item's version row; a file of no version as its format's page says, an OCI blob by R-OCI-038), in this order:
   `malware_found`, `vulnerability_blocked`, **`not_allowed_in_repository`**, `signature_not_verified`, `malware_not_checked`,
   `vulnerability_data_missing` (Q-CUR-7). The refusal never waits: a pending scan of a version the rules do not deliver is not waited
   for. `/files/`, the cache view, a remote's refill of a lost item and every tier of a virtual repository pass this point.
3. **In documents.** A document that lists versions lists only the delivered ones, after the format's rewrite of a remote document and
   the merge of a virtual one, with the endpoint's exposure view and the caller's version view (R-EP-018, Q-AUTHZ-255): npm's package
   and single-version documents and dist-tags (with `latest` moved as R-NPM-048 moves it), NuGet's version list, registrations and search,
   an OCI tags list and the catalog's names, Ansible Galaxy's collection document, version list and search; each format's page says how
   (R-NPM-049, R-NUGET-035, R-OCI-038, R-ANS-047). A Debian remote passes its upstream's signed indexes through unchanged and refuses the
   files; the filtered view of a Debian remote is a virtual repository over it, whose merge drops the rows its member's rules refuse
   ([formats/debian.md](formats/debian.md) R-DEB-036, Q-CUR-9).

- A remote whose mode is `allExceptBlocked` and that has no rule pays nothing: the snapshot holds no rule set, and no document is
  filtered or keyed differently (R-ARCH-027).
- A filtered document is a rendition of its own: its key and validator carry the revision of the repository's rule set
  (`repositories.delivery_rules_revision`, R-CUR-006), so a change of the rules is never answered with a document rendered under the old ones.
- The kit of the format contract holds every format with a remote class to it (check 23, R-CUR-022): with a rule that refuses one version
  of a fixture package, no document of the format lists that version, the version's file answers the refusal with `Cache-Control:
  no-store`, and a package whose every version is refused answers the refusal at its document.

Acceptance criteria:

- Unit (kit check 23, `internal/formats/formattest`, milliseconds per format): the three statements above for every format with the class
  remote, against a host whose rule set refuses `2.0.0` of the fixture's package.
- Integration, one case per format (areas `npm`, `nuget`, `ociremote`, `deb`, `ansible`, `TestR_CUR_003_…`, about 2 s each, the counting
  stub upstreams and their corpora): mode `onlyAllowed` with one version allowed; the package document lists that version only; the
  allowed version's file is fetched and served; the other version's file answers `403 not_allowed_in_repository` and the stub records no
  request for it; a package without a rule answers the refusal at its document and the stub records no request. For Debian the remote's
  `InRelease` is the stub's byte for byte, the refused `.deb` answers 403, and the virtual repository's `Packages` lacks its row.
- Unit (`internal/signatures/gate`, milliseconds, fakes of the four checks): an infected item that the rules refuse answers
  `malware_found`; an item that the rules refuse with a pending malware scan answers `not_allowed_in_repository` at once.

### R-CUR-004 · The refusal a client gets
Priority: **P1** · Covers: SEC-007, SEC-032

- **Its kind.** An error of the kind `Forbidden` with the code `not_allowed_in_repository` (registered in `internal/errs/codes.go`), and
  `Cache-Control: no-store`. Each format's `WriteError` renders it in its client's shape, as it renders the malware gate's codes
  ([malware-scanning.md](malware-scanning.md) R-MAL-016):

  | Format | Answer | What the client shows |
  |---|---|---|
  | npm | `403`, `{"error": <text>, "message": <text>, "code": "not_allowed_in_repository"}` | `npm error 403 … - <text>` |
  | NuGet | `403`, one line `not_allowed_in_repository: <text>`, the text in `X-NuGet-Warning` | `dotnet restore` prints the warning and fails the package |
  | OCI | `403`, `{"errors": [{"code": "DENIED", "message": <text>, "detail": null}]}` | Docker 29: `Error response from daemon: error from registry: <text>` (the message, not the code; [Q-CUR-161](#q-cur-161--each-formats-end-to-end-case-is-the-remotes-own-refusal-an-allow-and-a-download-ahead-what-the-clients-print-is-measured)) |
  | Debian | `403` problem | apt prints the status line only: the protocol carries no reason (Q-CUR-9) |
  | Ansible Galaxy | `403`, `{"errors": [{"status": "403", "code": "not_allowed_in_repository", "title": …, "detail": <text>}]}` | `ansible-galaxy` prints the detail |
  | `/files/` | `403` problem with `code` | — |

- **The text** names the package, the version (or "any version"), the repository, the reason and where to ask: "`<name>` `<version>`
  is not allowed in `<project>/<repo>`: blocked by a rule of the repository." or "… this repository delivers only what is allowed."
  followed by "Ask for it at `<base URL>/ui/projects/<project>/repositories/<repo>/upstream?name=<name>`." It names no rule's note and
  no other repository.
- **Existence.** The refusal is asked after the decision point (R-AUTHZ-015) and before any upstream request, so a caller who may not
  read the repository keeps its `404` or `401`, and a reader learns only what the rules say, never whether the upstream has the name. A
  hidden existence (`hideExistence`, R-AUTHZ-017) does not change it: the reader may read the repository.
- **A virtual repository** treats the refusal as "not found" of that member: [repositories-and-proxy.md](repositories-and-proxy.md)
  R-REPO-019 step 7 lists `403 not_allowed_in_repository` beside `404`, `403 forbidden` and `403 populate_not_permitted`, and the walk goes
  on to the next member. When nothing serves the request, the answer is the first transient failure as before, else the first member's
  rule refusal (its text names that member), else `404`: "not found" would hide the act that changes the answer (Q-CUR-8). A rule of the
  virtual repository's own does not exist; its patterns stay R-REPO-005's.
- **A Bastion** forwards a miss to the main installation's route (R-BAS-013), which decides as above; what a Bastion holds of a version
  that a narrowing of the rules refuses is dropped by the `content` entry of `repository.narrowed` (R-CUR-021, R-BAS-015). It relays the
  refusal unchanged and keeps no copy of it, because the refusal says `Cache-Control: no-store` (R-BAS-020,
  [Q-CUR-44](#q-cur-44--a-bastion-keeps-no-refusal-of-the-rules-an-allow-reaches-its-clients-at-once)): an allow reaches a Bastion's
  clients at once.
- **Audit:** `remote.delivery.refused` (R-CUR-019), asynchronous, at most once a minute per repository, package, version and client
  address on each replica, as `vulnerability.download_blocked` is throttled; details: the path, the package, the version, the reason
  (`blocked` or `not_allowed`), the kind (`document` or `artifact`).
- **Metric:** `quader_remote_delivery_refusals_total{repo,reason,kind}`.
- **Record:** every refusal on a client's request also counts in the request list's entry of its subject (R-CUR-015): one row per
  repository, package and version, with the first and last time and the count, summed on each replica and written at most every 5 s per
  subject ([Q-CUR-40](#q-cur-40--a-refusals-count-is-summed-on-the-replica-and-written-every-five-seconds)), so the list reads no audit log;
  beyond `proxy.delivery.max_client_entries` entries without an asker the oldest last refusal goes.
- A download ahead is never refused by the rules (R-CUR-014); a pass of the mirror fetches only what the rules deliver (R-CUR-017).

Acceptance criteria:

- Unit (kit check 23): every format renders `403` with `Cache-Control: no-store` for the code, and the body's code in its shape.
- Integration (area `npm`, `TestR_CUR_004_…`, about 2 s): a virtual repository over a remote that refuses `left-pad` and a second remote
  that delivers it serves the second's tarball; with only the first, the answer is the first's `403` and its text names the first;
  `npm install` through it prints the text (unit of the npm error writer).
- Integration (same case): two refusals within a minute write one `remote.delivery.refused` and count 2 in the request list's entry.

### R-CUR-005 · Who may do what
Priority: **P1** · Covers: SEC-035

| Act | Least | On a repository of the project `upstream` |
|---|---|---|
| Read the catalogue (search, levels, a package and its versions) | a signed-in caller with `read` on the repository; in mode `onlyAllowed` a reader without `manage` sees what is delivered and looks up by exact name (R-CUR-009) | a signed-in caller who may read it |
| Read the rules with their notes | `manage` on the repository, `repositories:read-config`, or the system role `admin` | `repositories:read-config` |
| Ask which rule decides a name and version (`delivery-rules/check`) | `read` | `read` |
| Add, change or remove a rule | `manage` on the repository (the roles Maintainer and Owner, R-AUTHZ-026), or the system role `admin` | `admin` only: such a repository has no members (R-REPO-066) |
| Set the mode and the repository's switches of R-CUR-020 | configuration: `repositories:write-config` or the project's Owner (R-AUTHZ-006) | `repositories:write-config` |
| Download one version the rules deliver | the person's populate right: `read` plus the action of `cacheFillAction` (R-AUTHZ-008), asked without the session-cookie refusal (R-CUR-010) | the same |
| Download one version the rules do not deliver (examination), every version of a package, a sub-section | `manage`, or `admin` | `admin` |
| See the queue | `read` | `read` |
| Retry or cancel a row | the person who started it, `manage`, or `admin`; a row a probe or a client caused: who may download that version | the same |
| Ask for an allow (a request) | a signed-in caller with `read` who may not set rules | the same |
| See the requests | `manage` or `admin` see all; a person sees the entries they asked in | the same |
| Allow or decline a request; discard what was examined | as adding a rule | `admin` |

- Every operation that the table gives to "`manage`, or `admin`" is declared `authenticated: true` in the API document and decided in the
  handler, as the mirror's run is (`internal/api/v1/mirror_api.go`, Q-API-199): a declaration of one form cannot say "an action or a role".
- **Allowing is not releasing.** A gate's release (the malware gate's, R-MAL-021; the signature gate's, R-SIG-070) and a triage block
  (R-VULN-013) stay the acts of the holders of `vulnerabilities:triage`, which no project role grants. A Maintainer allows a package;
  whether a gate still refuses it is not the Maintainer's to lift, and the screens say which of the two holds a version back (R-CUR-014,
  Q-CUR-11).

Acceptance criteria:

- Integration (area `repository`, `TestR_CUR_005_…`, about 2 s): every row of the table for the roles Reader, Developer, Maintainer and
  Owner of `web`, an administrator, a `repository-admin` (no rule write, mode write) and an anonymous caller (401 at every operation),
  on `web/npm-remote` and on `upstream/npmjs`.

### R-CUR-006 · The rules' storage, limits, propagation and API
Priority: **P1** · Covers: SEC-037

```sql
CREATE TABLE remote_delivery_rules (
  id          uuid        PRIMARY KEY,                                  -- UUIDv7
  repo_id     bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
  kind        text        NOT NULL CHECK (kind IN ('section', 'package', 'version')),
  pattern     text        NULL CHECK (char_length(pattern) BETWEEN 1 AND 1024),
  name        text        NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
  version     text        NULL CHECK (char_length(version) BETWEEN 1 AND 256),
  effect      text        NOT NULL CHECK (effect IN ('allow', 'block')),
  note        text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
  request_id  uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
  created_by  uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'section' AND pattern IS NOT NULL AND name IS NULL AND version IS NULL)
      OR (kind = 'package' AND pattern IS NULL AND name IS NOT NULL AND version IS NULL)
      OR (kind = 'version' AND pattern IS NULL AND name IS NOT NULL AND version IS NOT NULL))
);
CREATE UNIQUE INDEX remote_delivery_rules_subject
  ON remote_delivery_rules (repo_id, kind, coalesce(pattern, ''), coalesce(name, ''), coalesce(version, ''));
CREATE INDEX remote_delivery_rules_name ON remote_delivery_rules (repo_id, name) WHERE name IS NOT NULL;
-- The revision of a repository's rule set: raised by every write in the writing transaction, and by a restore (a client-held
-- revision, R-BAK-022, R-CUR-021).
ALTER TABLE repositories ADD COLUMN delivery_rules_revision bigint NOT NULL DEFAULT 0;
```

The migration of every table of this page is
[00114_remote_curation.sql](../../internal/meta/postgres/migrations/00114_remote_curation.sql) (built), with the columns
[00115_remote_examination_files.sql](../../internal/meta/postgres/migrations/00115_remote_examination_files.sql) adds (Q-CUR-140, Q-CUR-141,
R-CUR-015); beside the statements of this page
it bounds every counter at zero from below and every bulk's bounds at one.

- **One rule per subject.** A write of a subject that has a rule replaces its effect and note; the subject is the identity.
- **Canonical forms.** A name is stored by the format's name rule (`catalog.Names.Normalize`); a pattern folded by it; a version by the
  format's canonical form, which the format declares as `catalog.VersionCanon` (new, beside its `catalog.Names`; R-CUR-007): npm and
  Ansible Galaxy the SemVer text as given, NuGet the identity of its versions, the URL form of R-NUGET-003 (normalized, lowercased,
  without build metadata, so `1.0.0-Beta` and `1.0.0-beta` are one, Q-CUR-3), Debian the version as the index row has it, OCI the tag as given. A
  name the rule refuses answers `422 invalid_name` (registered with the rules' package, `internal/errs/codes.go`), a version the format
  cannot read `422 invalid_version`, a pattern the matcher refuses `422 invalid_pattern`; the decision folds what it is
  asked with the same functions, so `NuGet 1.0` and a rule on `1.0.0` meet (the gap of H-380 does not arise).
- **Limits:** at most `proxy.delivery.max_rules` (10,000 by default) rules per repository, of which at most 100 sections (the bound of a
  pattern list, R-REPO-005); beyond it `422 too_many_rules`.
- **Propagation.** Every write records the change area `repository` in its transaction (R-ARCH-018), as a write of a virtual
  repository's member rows does; every replica's snapshot of the repository then holds its compiled rule set: a map of names to the
  package rule and the version rules, and the section patterns compiled by the matcher. A replica decides on its snapshot until it is
  rebuilt (H-132, [../security/repository-model.md](../security/repository-model.md)). Memory: about 150 bytes per rule, 1.5 MB per
  repository at the limit.
- **Journal.** A write that narrows what the repository delivers (a `block` added, an `allow` removed or turned into a `block`, the
  mode set to `onlyAllowed`) records `repository.narrowed` beside its own event, with the part `rules`
  (`{"blocksAdded": [<subject>], "allowsRemoved": [<subject>]}`) or `deliveryMode` (`"onlyAllowed"`), so a restore keeps it narrowed
  ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-061; replay: the blocks are added, the allows removed, the mode set; a
  rule set that would exceed the limit disables the repository).
- **The API** (tag `remote-curation`, below `/api/v1/projects/{projectKey}/repositories/{repoKey}`; R-CUR-005 for who):

  | Method and path | What | Answers |
  |---|---|---|
  | `GET delivery-rules?kind=&effect=&q=&cursor=` | the rules, newest first, `q` a substring of the subject | `200`, cursor paging (R-API-010) |
  | `POST delivery-rules` | `{"kind", "pattern" \| "name" [, "version"], "effect", "note"}`; creates or replaces the subject's rule | `201` (created), `200` (replaced) with the rule and `"revision"`; `422` as above; `400 not_applicable` for a local or virtual repository |
  | `DELETE delivery-rules/{ruleId}` | removes the rule | `204` |
  | `POST delivery-rules/check` | `{"name" [, "version"]}`: the decision of R-CUR-001 and the rule that made it (`"decidedBy": {"kind", "subject", "effect"} \| {"mode"}`) | `200` |

  The rule's representation: `{"id", "kind", "subject", "pattern"?, "name"?, "version"?, "effect", "note", "requestId", "createdBy": {"id",
  "name"}, "createdAt"}`. Writes take `Idempotency-Key` (R-API-014).

Acceptance criteria:

- Integration (area `repository`, `TestR_CUR_006_…`, about 2 s): create, replace and remove a rule; `422` for a name npm refuses, a range,
  a 101st section; the rule takes effect on a second replica of the test stack within the snapshot's bound; a write that adds a block
  records `repository.narrowed` with the part `rules`; a restore's replay of that entry (the replay unit of R-REPO-061) adds the block to a
  rule set restored without it.

### R-CUR-007 · The upstream catalogue: a port of the format contract
Priority: **P1** · Covers: ART-044

- **The declaration.** Every format with the class remote declares its catalogue in its descriptor, as it declares its probes
  (R-REPO-065): `formats.Descriptor.Catalogue`, the calls its remote adapter answers and, for each it does not, one sentence why
  (`NoSearch`, `NoLevels`). `formats.Register` refuses a format with the class remote that does not declare the lookup by exact name (the
  floor), that declares a call and a reason for it or neither, or that has no version canon; and, from the catalogue's package on, one
  whose remote adapter does not implement `formats.Catalogue` (Q-CUR-109). The contract version rises to 11 and every format moves in the
  same change (R-ARCH-010, Q-CUR-12).
- **The capabilities** (`discover.CatalogueCaps`, read per repository because one format's upstreams differ: npm's public registry has a
  replication host, NuGet a search resource or none, OCI a registry with or without its operator's API):

  | Capability | Meaning |
  |---|---|
  | `search: upstream` | the upstream answers a text query (npm `-/v1/search`, NuGet's search resource, the Galaxy's keyword search, a preset's registry API); `searchWindow` is the deepest hit it reaches, `searchHint` the code of what a person can type and find |
  | `search: index` | Quader answers a text query over an index it holds of the upstream (Debian's `Packages` and `Translation-en` of the suites read) |
  | `search: none` | no text search; `searchHint` the code of why and of what to enter instead |
  | `levels: [<kind>…]` | the levels above a package, outermost first (`scope`; `namespace`; `suite`, `component`, `architecture`), each listable; empty with `levelsReason` |
  | `lookup` | a package by its exact name; every format has it (the floor) |
  | `versions` | the versions of a package with what the upstream tells of each without a download: `published`, `size` (`sizes: known \| perVersion \| unknown`), flags (`deprecated`, `yanked`, `unlisted`, `prerelease`), platforms, a digest |

- **The interface** (`formats.Catalogue` in `internal/formats/catalogue.go`; the remote adapter implements it, `formats.CapabilitiesOf`
  finds it as `RemoteCaps.Catalogue`). Its browsing calls and every type below are the leaf's, `internal/mirror/discover`
  ([catalogue.go](../../internal/mirror/discover/catalogue.go)), which the mirror's lister speaks, for the reason the lister is there
  (ADR-152, Q-CUR-109): the catalogue's engine and a bulk's expansion are domain packages and call a format's catalogue without importing
  the contract. A request of the catalogue goes through the same lent `discover.Upstream` with the egress rules, the credential policy and
  the bounded reader:

  ```go
  // package discover. Catalogue is the part of a format's upstream catalogue that browses (R-CUR-007). Every call reads through u,
  // which stores nothing in the repository's cache (R-CUR-008); a call may use a stored document of the repository that u hands it.
  type Catalogue interface {
      CatalogueCaps(ctx context.Context, u Upstream) (CatalogueCaps, error)
      // Search answers one page of a text query; a repository whose caps say search: none answers ErrNotOffered.
      Search(ctx context.Context, u Upstream, q SearchQuery) (SearchPage, error)
      // Children lists one page of a level: the levels below it and the packages directly in it.
      Children(ctx context.Context, u Upstream, rq LevelRequest) (LevelPage, error)
      // Package looks a package up by its exact name and answers one page of its versions, newest first by the format's order.
      Package(ctx context.Context, u Upstream, name string, rq VersionsRequest) (PackageInfo, error)
  }

  // package formats. The contract adds the call that names a format's probe, a type of the contract.
  type Catalogue interface {
      discover.Catalogue
      // VersionProbe names what a download of one version starts from: the probe the queue's run hands to ProbeContent (R-CUR-011).
      // It may read and store the version's documents through u, which in a run is the worker's (R-CUR-011), never the catalogue's.
      VersionProbe(ctx context.Context, u discover.Upstream, name, version string) (Probe, bool, error)
  }

  // package formats. What a format with the class remote declares (Descriptor.Catalogue): the lookup is the floor; search and levels
  // each the call or a one-sentence reason, never both.
  type CatalogueDeclaration struct {
      Lookup   bool
      Search   bool
      NoSearch string
      Levels   []string // the kinds of the levels above a package, outermost first
      NoLevels string
  }

  // package discover, the types the calls speak.
  type SearchKind string // SearchUpstream "upstream", SearchIndex "index", SearchNone "none"
  var ErrNotOffered error // a call the repository's upstream does not answer; the API answers 400 catalogue_not_offered

  type CatalogueCaps struct {
      Search       SearchKind // "upstream", "index", "none"
      SearchWindow int        // deepest hit the upstream's search reaches; 0 when it says nothing
      SearchHint   SearchHint // the code of the sentence under the search field (Q-CUR-166); the screen writes it
      Levels       []string   // outermost first
      LevelsReason string
      Sizes        string     // "known", "perVersion", "unknown"
      Hosts        []string   // the hosts besides the upstream's that the calls reach, from the preset (R-CUR-008)
  }

  type SearchQuery struct {
      Text      string     // 1 to 128 characters after trimming
      NamesOnly bool       // Debian: apt-cache --names-only
      Level     LevelRef   // restrict to a level where the upstream can
      Cursor    Cursor
      Limit     int        // at most 50
  }
  type Hit struct {
      Name, Description, Latest string
      Updated                   time.Time
      Flags                     []string // deprecated, unlisted, official, verified, unsigned-index (Debian)
  }
  type SearchPage struct {
      Hits      []Hit
      Total     int  // -1 when the upstream does not say
      Cursor    Cursor
      Done      bool
      Truncated bool   // the upstream's window ends before its total
      Note      string // why, one sentence
  }
  type LevelRef struct{ Path []string } // e.g. ["trixie", "main", "amd64"]; empty: the root
  type LevelRequest struct {
      Level  LevelRef
      Cursor Cursor
      Limit  int // at most 200
  }
  type LevelPage struct {
      Levels   []string // names of the levels below
      Packages []Hit
      Total    int
      Cursor   Cursor
      Done     bool
      Unknown  *UnknownLevel // the upstream does not have the level (Q-CUR-165); nil where it lists something or cannot tell
  }
  type UnknownLevel struct {
      Rule       NameRule // the naming rule the name breaks ("lowercaseDigits", "lowercaseDigitsUnderscore"); "" when it keeps it
      Forbidden  string   // the name's characters the rule does not allow
      Suggestion string   // the name made to keep the rule, asked once and found; "" for none
  }
  type VersionsRequest struct {
      Cursor Cursor
      Limit  int    // at most 100
      Filter string // a substring of the version, applied by Quader
  }
  type VersionFacts struct {
      Version   string // canonical (R-CUR-006)
      Published time.Time
      Size      int64 // 0 unknown
      Flags     []string
      Platforms []string
      Digest    string
  }
  type PackageInfo struct {
      Found                   bool
      Name, Description       string
      Tags                    map[string]string // npm dist-tags, OCI none
      Versions                []VersionFacts
      Total                   int
      Cursor                  Cursor
      Done                    bool
  }
  ```

- **One notion of discovery.** The mirror's `discover.Lister` stays what discovers every name for a pass; the catalogue lists a level,
  answers a query and reads one package. A format implements both in the same remote adapter and reuses its calls: npm's scope listing
  reads the key range its feed reads on the replication host it declares in `MirrorHosts`; NuGet's search reads the search resource its
  listing reads; OCI's root level is the `_catalog` its listing reads; Debian's levels and search read the indexes its listing parses;
  Ansible Galaxy's namespace level is the collection index its listing reads with a `namespace` filter. `Package` reads what `MirrorPlan`
  reads for the package, without storing it. A bulk download (R-CUR-010) expands through `Package` and `Children`, never through a second
  lister.
- **A level the upstream does not have** ([Q-CUR-165](#q-cur-165--a-level-the-upstream-does-not-have-says-so-with-the-formats-naming-rule-and-one-suggestion-built-from-it))
  is told from one that lists nothing where the upstream tells them apart, at a constant cost: a first page that lists nothing asks at
  most two accounts of the name (`discover.Exists`, a `HEAD`) and sets `Unknown`. Docker Hub (`/v2/users/{name}`), Quay (its organization
  and its user), npm's public registry (`/-/org/{scope}/package`), the Galaxy (`v3/namespaces/{name}/`), GitLab (the message
  `404 Project Not Found`) and Debian (the suite's `InRelease` or `Release`, its `Components` and `Architectures`) do; NuGet has no levels
  and a registry without a preset lists only its root. A name that breaks the part of the format's naming rule that is certain is not
  asked for (a Docker Hub namespace's characters outside `[a-z0-9_]`, a Galaxy namespace's outside `[a-z0-9_]`): the page names the rule
  and the forbidden characters, and the one name the rule makes of it when the upstream has it (one request).
- **The version canon** (`catalog.VersionCanon`, `Canonical(version string) (string, bool)`) is declared beside the catalogue by every
  format with the class remote (R-CUR-006); `Register` refuses a remote format without it.
- **Formats that come later** declare their catalogue in the same change that brings their remote class; R-CUR-023 shows what four of
  them would declare.

Acceptance criteria:

- Unit (`internal/formats`, milliseconds): `Register` refuses a remote format without the declaration and one whose declared call its
  adapter does not answer; every registered format's declaration is pinned by `TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue`
  (`internal/formats/all`).
- Unit (kit check 22, `internal/formats/formattest`, milliseconds per format, a fake upstream in process answering canned documents of a
  few kilobytes from the format's fixture): each declared call answers a page whose names the format's rule accepts, a cursor that
  continues, `Done` at the end, and `ErrNotOffered` for an undeclared call; an answer above the bound of R-CUR-008 fails with
  `catalogue_answer_too_large`; no call reaches a host outside the upstream, its `extraHosts` and the declared `Hosts`; a level the
  case names as unknown (`UnknownLevel`, recorded answers of the real upstream) answers `Unknown` and nothing listed within three reads
  of the upstream, with the expected suggestion (Q-CUR-165).
- Unit (each format with levels, milliseconds, the recorded answers): an unknown level, a level that is there and lists nothing, a name
  that breaks the rule with and without a suggestion (`TestR_CUR_007_ALevelTheRegistryDoesNotHaveIsUnknown` in `internal/formats/oci`,
  `TestR_CUR_007_AGalaxyNamespaceItDoesNotHaveIsUnknown`, `TestR_CUR_007_ADebianLevelTheUpstreamDoesNotHaveIsUnknown`); the engine keeps
  such a page and drops `Unknown` from a page that lists something (`TestR_CUR_007_AnUnknownLevelIsKeptAndAPageWithEntriesIsKnown`).

### R-CUR-008 · The catalogue's calls: no cache fill, a budget of their own, short caching, bounds
Priority: **P1** · Covers: ART-044

- **Nothing is stored.** A catalogue call never writes the repository's cache, its format rows or a negative entry: the `discover.Upstream`
  the catalogue lends a format answers `Document` from the stored copy when the repository holds one (fresh or stale, with its age),
  else fetches it without storing, and refuses `Artifact`. Browsing therefore never populates, and the session cookie's rule of R-AUTHZ-008
  needs no exception for it (Q-CUR-13).
- **The upstream engine.** Every request goes through the repository's upstream client: its credentials, TLS, the egress address policy
  ([../security/egress.md](../security/egress.md)), the redirect rules, the circuit (read, never moved, as a background request reads it,
  R-REPO-054) and a background fetch slot that yields to clients (R-REPO-055). An open circuit or offline mode answers
  `503 upstream_assumed_offline` or `409 offline` to the catalogue call.
- **Second hosts.** A registry whose search or listing lives on another host than its protocol endpoint (Docker Hub's `hub.docker.com`,
  GitLab's `gitlab.com` API) is declared by the format's preset as `catalogueHosts` (`catalog.UpstreamPreset`, new), each with whether it
  receives the repository's credential; a person never types such a host. A remote whose URL is no preset's has no second host. Every
  declared host passes the egress address policy at connect time like the upstream's.
- **An undocumented endpoint** is used only where its operator's documented interface has no equal (Docker Hub's repository search,
  `GET /v2/search/repositories/`, [formats/oci.md](formats/oci.md) R-OCI-038). The call keeps working without it: an answer that is not a
  page of the expected shape (any status but `200`, a body that does not parse) turns the capability to `search: none` with the sentence
  "Docker Hub's search did not answer as expected; look an image up by its name or list a namespace." for the cache period, and counts
  `quader_remote_catalogue_requests_total{outcome="shape"}`.
- **A budget of its own** per repository and per person, so that browsing cannot starve clients and one person cannot spend another's:
  `proxy.catalogue.requests_per_minute` (120 by default) upstream requests per repository across replicas, and
  `proxy.catalogue.requests_per_minute_per_person` (30) per person across repositories, both on the cluster limiter (R-ARCH-030; without
  Valkey per replica at the value divided by the live replicas of the role). A call over either answers `429 catalogue_budget_exhausted`
  with `Retry-After`; a call answered from the catalogue's cache costs nothing. The upstream's own rate-limit answer turns into the same
  `429` with its `Retry-After` and is not retried within the call.
- **Short caching.** Answers are kept per replica in an in-process cache of `proxy.catalogue.cache_bytes` (32 MiB) for
  `proxy.catalogue.cache_ttl` (300 s; npm's search answers `max-age=300`), keyed by repository, upstream URL, call and arguments. What
  is kept is the parsed page (names, facts), never a document's bytes. Quader's own state of a row (R-CUR-009) is added at answer time
  and never cached. A page that found nothing (a package not found, a search without a hit, a level without an entry) is kept only when a
  request of the call reached the upstream: a format that answers from what Quader holds of the upstream (Debian's parsed indexes,
  R-DEB-036) finds a package as soon as its index is parsed, and asking its rows again costs no upstream request (Q-CUR-164).
- **Bounds on untrusted answers.** A document the catalogue reads is at most `proxy.catalogue.max_document_bytes` (64 MiB) and is
  parsed as a stream; a call takes at most `proxy.catalogue.timeout` (20 s); a page holds at most 50 hits, 200 entries of a level or
  100 versions; a text from the upstream (a description) is cut at 1,000 characters and shown as text, never as markup. Above a bound the
  call answers `502 catalogue_answer_too_large` with the bound in the detail, and the screen says it (R-CUR-018).
- **What a search tells the upstream:** the query, from Quader's address, with the repository's credential where the upstream receives
  it ([../security/remote-curation.md](../security/remote-curation.md)). The search guard of R-CUR-002 withholds a term a block covers.

Acceptance criteria:

- Unit (`internal/remotecuration/catalogue`, milliseconds, a fake format and a fake upstream): a call stores nothing (the fake engine's
  store records no write); a second identical call within the period sends nothing; the 121st upstream request of a repository within a
  minute answers `429`; a 65 MiB document answers `502 catalogue_answer_too_large`; a host the preset does not declare is refused
  `foreign_host_denied`; a shape failure of the declared undocumented search turns the capability off for the period; a not-found the
  upstream answered is kept for the period, while a format that answers from its own rows finds within the period what its index's
  parse added (lookup, search and level), and its pages that found something are kept.
- Integration (area `npm`, `TestR_CUR_008_…`, about 2 s, the counting npm stub with a search route added): a search through the API
  reaches the stub once for two identical calls; the packument of a package no client fetched is read and not stored
  (`remote_cache_entries` unchanged).

### R-CUR-009 · The catalogue's API and what a row carries
Priority: **P1** · Covers: ART-044

Below `/api/v1/projects/{projectKey}/repositories/{repoKey}` (tag `remote-curation`), every operation `GET`, a signed-in caller with
`read` (R-CUR-005); a local or virtual repository answers `400 not_applicable`:

| Path | Answer |
|---|---|
| `upstream` | the capabilities (R-CUR-007) for this repository, the mode, the counts of rules, open requests (for those who may see them) and of the queue by state, and `"hosts"` |
| `upstream/search?q=&namesOnly=&level=&cursor=&limit=` | a page of hits; `400 catalogue_not_offered` where the capability is `none` |
| `upstream/levels?level=&cursor=&limit=` | a page of a level (`level` is the path joined by `/`, each part URL-encoded); `unknown` when the upstream does not have it: `upstream` (the preset's display name, else the URL's host), `rule`, `forbidden`, `suggestion` (Q-CUR-165) |
| `upstream/package?name=&cursor=&limit=&filter=` | the package and a page of its versions; `404 catalogue_not_found` when the upstream does not have it |
| `upstream/vulnerabilities` (`POST`, `{"name", "versions": [≤ 50]}`) | the vulnerability verdict of each version that is not stored, computed as the miss gate computes it (`Evaluator.EvaluateMiss`, nothing written, [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015); a format the vulnerability scanning does not cover answers `"notCovered": true` (R-CUR-014) |

- **Quader's state on every row.** A hit, a package and a version carry, beside the upstream's facts, what this repository makes of them:
  `delivery` (`delivered`, `blocked`, `notAllowed`, with `decidedBy` as in `delivery-rules/check`), `stored` (`none`, `partial`, `whole`;
  for OCI the platforms held), `examination` (a version stored that the rules do not deliver, R-CUR-014), the stored version's gate verdicts
  (`malware`, `vulnerabilities`, `signatures`, each as the version views of the package API carry them, R-VULN-020, R-MAL-022, R-SIG-034),
  `queued` (the queue row of a waiting or running download, R-CUR-011) and `request` (the open request entry of its subject, R-CUR-015).
  As built, `stored` is `none` or `partial` for a hit and a package and `none` or `whole` for a version, and a stored version's gate
  verdicts are read from the package API's version view, not repeated in the row; `examination` waits for R2b
  ([Q-CUR-61](#q-cur-61--a-rows-stored-state-is-what-the-repository-holds-the-stored-versions-verdicts-stay-the-version-views)).
  The versions a repository holds are the ones its package screens list: its version rows, and for an OCI remote the tags it lists,
  which are cache entries and no version rows (R-OCI-036,
  [Q-CUR-156](#q-cur-156--an-oci-remotes-stored-tags-are-the-tags-its-package-screens-list)).
- **What a reader sees in mode `onlyAllowed`.** A caller who may not set rules sees in search and level answers only the hits whose
  package is delivered, with `"withheld": <count>` and the sentence "N hits are not allowed in this repository; look a package up by its
  exact name to ask for it." A lookup by exact name answers every package with its versions and its state, so the person can ask for it
  (R-CUR-015). Those who may set rules see every hit. In mode `allExceptBlocked` every reader sees every hit, a blocked one marked
  (Q-CUR-14).
- A name in the answers is the format's normalized name; a version the canonical one (R-CUR-006); every text from the upstream is plain
  text.

Acceptance criteria:

- Integration (area `repository` with the npm stub, `TestR_CUR_009_…`, about 3 s): a search answers the stub's hits with their states (one
  stored, one blocked, one with an open request); in mode `onlyAllowed` the Reader of `web` sees one hit and `withheld: 2`, the Maintainer
  three; a lookup of a not-allowed package answers its versions to the Reader; `upstream/vulnerabilities` answers the verdict of a test
  `MAL-` record for an unstored version and writes no row.

### R-CUR-010 · Downloads ahead of a client: levels, preview, bounds, who and whose budget
Priority: **P1** · Covers: RMT-027

- **Three levels** (the owner's): one version of a package; every version of a package; a sub-section (a level of the catalogue, or, where
  the format has no listable level, nothing: the screen says so). Below `/api/v1/projects/{projectKey}/repositories/{repoKey}`:

  | Method and path | Body | Answer |
  |---|---|---|
  | `POST downloads/preview` | `{"level": "version" \| "package" \| "section", "name"?, "version"?, "section"?, "maxVersions"?, "maxBytes"?, "includePrereleases"?}` | `200` `{"versions": <n> \| null, "packages": <n> \| null, "bytes": <n> \| null, "sizeUnknown": <n>, "stored": <n>, "notDelivered": <n>, "bounds": {…}, "note"}`; for a section whose size the upstream does not tell, `versions` and `bytes` are `null` and `note` says that the bound cuts while it runs (D10) |
  | `POST downloads` | the same | `202` with the queue row of a version, or the bulk `{"id", "state": "expanding", …}` and `Location` of `download-bulks/{id}`; `409 already_queued` names the row of a version that is waiting or running |

  The preview of a version or a package is one catalogue call (`Package`); of a section the level's count where the upstream tells it
  (Debian's index rows, a Galaxy namespace's count, a Docker Hub namespace's count), else `null`; a section of an upstream that lists no
  levels answers `400 catalogue_not_offered`. A package's bulk is expanded through the catalogue's `Package`, a section's through its
  `Children` and `Package`, with the bulk's bounds ([Q-CUR-65](#q-cur-65--a-bulk-and-a-preview-read-the-catalogue-a-preview-is-one-page-of-the-bulks-bound),
  which replaces the mirror plan's expansion of [Q-CUR-80](#q-cur-80--a-download-of-one-version-runs-before-the-catalogue-the-call-that-names-its-files-is-a-contract-interface-of-its-own)).
- **Bounds of a bulk**, each a value in the form that the person may lower: `maxVersions` (`proxy.downloads.max_versions_per_bulk`,
  10,000 by default), `maxBytes` (`proxy.downloads.max_bytes_per_bulk`, 100 GiB), `includePrereleases` (true), and for a package
  `newest` (all). A size the upstream does not announce counts when the file is stored: the bulk stops adding rows when the stored bytes
  of its rows reach `maxBytes`, and ends `bounded`.
- **Who** (R-CUR-005): one version the rules deliver, whoever may populate the repository with a client; one version the rules do not
  deliver, every version and a section, the holders of `manage` and `admin`.
- **The session.** A download ahead is an explicit act of the management API (a `POST` that needs the CSRF token of the session, R-API-006,
  or a token), run by a worker, never a read of the data path: the session cookie's refusal of R-AUTHZ-008, which guards against a
  link or an image that makes a browser fill a cache, does not apply to it, and the person's own actions and the endpoint's
  `cache_fill` setting (R-EP-019) are asked as the populate check asks them ([authorization.md](authorization.md) R-AUTHZ-008, new
  bullet; Q-CUR-27).
- **Whose budget** ([usage-and-limits.md](usage-and-limits.md) R-LIM-037, Q-LIM-19):
  - one version started by a person, and the examination a request starts (R-CUR-015), are charged to that person as a probe's background
    download is: step 9a decides at the `POST` for the person's budget key and the remote's ceiling of the person's class, and the charge
    travels to the worker as a `limits.Ticket`; a refusal by 9a answers `429` with the limits' problem and queues nothing;
  - every version of a package, a section, and the version an allow starts are system work in the sense of Q-LIM-19: recorded under the
    subject `system` with the person named in the row and the usage store's `startedBy`, charged to no ceiling, and bounded by the
    preview, the bounds above, the waiting rows of R-CUR-011, the repository's share of background slots (R-REPO-055) and the upstream's
    own throttling (R-REPO-054). The usage store has no `startedBy`: the person is the row's, the bulk's and the event's
    ([Q-CUR-85](#q-cur-85--the-usage-store-records-a-bulk-under-system-the-person-is-the-rows-and-the-events)). A mirror (Q-REPO-142) keeps a scope current without end and stays an installation's decision; a bulk is
    one act, previewed and bounded, and so a project's (Q-CUR-17).
- **Not a download.** A download ahead writes no download statistics (R-STOR-015) and no **Last download**, and is not a download of the
  limits' figures (R-LIM-007): it counts in `background_fetches` of the person charged, or of `system`.
- **Audit:** `remote.download.started` per `POST` (details: the level, the subject, the bounds, the bulk's ID, the count queued), with
  the person as actor; each stored file records the populate event of a fill with the actor `system:background-download` and the reason
  `download-ahead` (R-AUTHZ-029, as R-REPO-065's fills record theirs).

Acceptance criteria:

- Integration (area `npm`, `TestR_CUR_010_ADownloadAheadOfOneVersionAndOfAPackage`, about 3 s, the stub's corpus): a reader who may
  populate downloads one version, which ends `done` and is charged to the reader (usage row `background_fetches` 1); the reader's `POST`
  of a package answers `403`; the Maintainer's preview of the package answers; the Maintainer's bulk with `maxVersions: 2` queues two
  rows that end `done`, the newest version among them. The preview's counts of a package (3 versions, size `null`, `sizeUnknown: 3`) wait
  for the catalogue (R3, Q-CUR-80).
- Unit (`internal/limits`, `TestR_CUR_010_APersonsDownloadOfOneVersionIsChargedToThePerson`, milliseconds): one background download and
  one upstream request of the person's budget key, no request.
- Unit (`internal/remotecuration/queue`, `TestR_CUR_010_ABulkIsExpandedWithinItsBounds`, milliseconds): `maxVersions`, `maxBytes` of
  the announced sizes, the pause at the bound of waiting rows, a section's packages, a failure without a planner.
- Integration, one download ahead per format (areas `nuget`, `ociremote`, `debremote`, `ansibleremote`, `TestR_CUR_010_…`, about 3 s
  each): one version is stored; for OCI the platforms of `oci.platforms` with their layers; for Debian the file a parsed index lists,
  verified against its digest.

### R-CUR-011 · The download queue of a remote repository
Priority: **P1** · Covers: RMT-027

- **A table of its own, executed by jobs** (Q-CUR-18). The job system cannot list by repository, has no retry operation and keeps a
  failure as 2,000 bytes of text (`internal/jobs/worker.go`); the queue is rows of `remote_download`, and a row's run is one attempt of a
  job.

  ```sql
  CREATE TABLE remote_download_bulks (
    id                  uuid        PRIMARY KEY,                      -- UUIDv7
    repo_id             bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    level               text        NOT NULL CHECK (level IN ('package', 'section')),
    subject             text        NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 1024), -- the name or the level's path
    max_versions        integer     NOT NULL,
    max_bytes           bigint      NOT NULL,
    include_prereleases boolean     NOT NULL,
    state               text        NOT NULL CHECK (state IN ('expanding', 'queued', 'done', 'bounded', 'cancelled', 'failed')),
    versions_found      integer     NOT NULL DEFAULT 0,
    versions_queued     integer     NOT NULL DEFAULT 0,
    cursor              jsonb       NULL,                             -- the expansion's position, at most 4 KiB
    failure             jsonb       NULL,                             -- as a row's failure, below
    started_by          uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    finished_at         timestamptz NULL,
    -- a bulk a request or an allow starts for the newest versions of a package (Q-CUR-141): its rows' origin, request and charge
    origin              text        NOT NULL DEFAULT 'bulk' CHECK (origin IN ('bulk', 'request', 'allow')),
    request_id          uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
    ticket              jsonb       NULL
  );
  CREATE INDEX remote_download_bulks_repo ON remote_download_bulks (repo_id, created_at DESC, id);

  CREATE TABLE remote_download (
    id              uuid        PRIMARY KEY,                          -- UUIDv7
    repo_id         bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
    version         text        NOT NULL CHECK (char_length(version) <= 256), -- '' for a file of no version (an OCI blob)
    probe           jsonb       NULL,      -- {files[], document, digest}: what the run starts from, when a probe or a plan named it
    origin          text        NOT NULL CHECK (origin IN ('person', 'request', 'allow', 'bulk', 'probe', 'client')),
    bulk_id         uuid        NULL REFERENCES remote_download_bulks (id) ON DELETE CASCADE,
    request_id      uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
    started_by      uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
    ticket          jsonb       NULL,      -- the limits.Ticket of a charged download (R-CUR-010)
    state           text        NOT NULL CHECK (state IN ('waiting', 'running', 'done', 'failed', 'cancelled')),
    attempts        integer     NOT NULL DEFAULT 0,
    max_attempts    integer     NOT NULL,
    not_before      timestamptz NULL,      -- the next attempt
    job_id          uuid        NULL,      -- the running attempt's job
    occurrences     integer     NOT NULL DEFAULT 1, -- a client's failures of the same file, aggregated
    files           integer     NOT NULL DEFAULT 0,
    stored          integer     NOT NULL DEFAULT 0,
    present         integer     NOT NULL DEFAULT 0,
    refused         integer     NOT NULL DEFAULT 0,
    bytes           bigint      NOT NULL DEFAULT 0,
    outcome         text        NULL CHECK (outcome IN ('completed', 'bounded', 'refused', 'skipped')),
    failure         jsonb       NULL,      -- R-CUR-011 "The failure"
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz NULL,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz NULL
  );
  CREATE INDEX remote_download_list ON remote_download (repo_id, state, created_at DESC, id);
  CREATE INDEX remote_download_due ON remote_download (not_before, id) WHERE state = 'waiting';
  CREATE UNIQUE INDEX remote_download_one_active ON remote_download (repo_id, name, version) WHERE state IN ('waiting', 'running');
  CREATE INDEX remote_download_bulk ON remote_download (bulk_id) WHERE bulk_id IS NOT NULL;
  CREATE INDEX remote_download_finished ON remote_download (finished_at) WHERE finished_at IS NOT NULL;
  ```

  Export class `data` (R-BAK-004), so a backup holds the queue; a restore marks every `running` row `waiting` (R-CUR-021).
- **States:** `waiting` (with `not_before` when an attempt failed and a next one comes), `running`, `done` (with `outcome`: `completed`,
  `bounded` at the bounds of R-REPO-065, `refused` when every file was refused by a gate before the fill, `skipped` when the repository
  is gone, no longer remote, disabled or offline), `failed` (attempts spent, or a failure that no attempt can overcome), `cancelled`.
- **Execution.** At most `proxy.downloads.per_repository` (4) rows of a repository are `running` at once; the rest wait, in the order of
  their creation, rows of a person and of a request before rows of a bulk and of a probe. A row becomes `running` when its repository has a
  free place: at its insert, at the end of another row's run (in that run's settling transaction), and by the task `remote-downloads-dispatch`
  (every minute, on the queue `maintenance` as the mirror's dispatch, R-OPS-019) for rows whose `not_before` came. Each run is one job `remote-download` on the queue `downloads`
  (`jobs.workers.downloads`, 4 per replica by default), `MaxAttempts 1`, exclusive per row, 30 minutes (R-REPO-065's bounds of one
  download: at most 1,000 files, at most the repository's `limits.maxArtifactBytes`). Every start takes the repository's row with
  `FOR NO KEY UPDATE` first, so the bound holds across replicas ([Q-CUR-82](#q-cur-82--the-bound-of-running-rows-holds-across-replicas-by-the-repositorys-row-lock)). The job of R-REPO-065,
  `remote-background-download`, is this job: a probe inserts a row (R-CUR-013).
- **The run** (in `internal/formathost/background_download.go`, R-REPO-065's run, which this generalizes): the probe of the row, else the
  format's `Catalogue.VersionProbe` (its documents read and stored through the worker's upstream; the step `plan`); then
  `RemoteAdapter.ProbeContent` and each file through the format's gate, the vulnerability miss gate (R-CUR-014), the integrity checks and
  the store, as a background request (a slot of the background share, the circuit read and never moved, a rate limit waited out,
  R-REPO-054, R-REPO-055). The rules are not asked (R-CUR-014). A file another fetch stored meanwhile costs nothing.
- **Waiting rows are bounded:** at most `proxy.downloads.max_waiting_per_repository` (10,000) rows of a repository are `waiting`; a
  person's `POST` beyond it answers `429 download_queue_full` with the count; a bulk's expansion pauses until rows drain, and a probe that
  finds the queue full starts nothing (the outcome `queue_full` of R-REPO-065 stays only for this bound). Rows are never refused because
  others run: they wait.
- **Retries.** A failed attempt of a class that a later attempt can overcome (`upstream_rate_limited`, `upstream_error`, `network`,
  `circuit_open`, `storage`, `internal`) waits `min(30 s × 2^(attempt−1), 1 h)`, ±20 %, or the upstream's `Retry-After` when it names
  one, up to `proxy.downloads.max_attempts` (5); a failure of another class ends the row `failed` at once.
- **The failure** (`failure`, Q-CUR-20), written by the run for the attempt that failed, every member a script can filter on:

  | Member | Holds |
  |---|---|
  | `class` | closed list: `upstream_not_found`, `upstream_auth`, `upstream_rate_limited`, `upstream_error`, `upstream_unexpected`, `network`, `tls`, `egress_denied`, `integrity`, `content_refused`, `gate_refused`, `circuit_open`, `bound`, `storage`, `internal` |
  | `step` | closed list: `plan` (reading the documents that name the files), `resolve` (the host's name), `connect`, `tls` (the handshake or the certificate's verification), `request`, `status` (an answer whose status is no success), `body` (reading the body), `verify` (digest, size, media type), `gate` (the vulnerability miss gate), `store` (the object store or the database) |
  | `code` | the error code of the engine (`upstream_unreachable`, `tls_untrusted`, `upstream_integrity_mismatch`, `vulnerability_blocked`, …; `internal/errs/codes.go`) |
  | `file` | the repository path of the file that failed |
  | `address` | the address asked: scheme, host, port and path; the query is dropped (a signed address of a CDN carries a credential in it) and the sentence says so |
  | `status` | the upstream's status, when an answer came |
  | `excerpt` | the first 512 bytes of a non-success answer's body as text, characters outside printable ASCII and line ends replaced by `?`; none for a body the upstream declared as binary |
  | `retryAfter` | the upstream's `Retry-After` in seconds, when it named one |
  | `detail` | Quader's sentence for a person: what failed, where, and what helps ("The TLS certificate of `registry.example.com:443` is not trusted by this installation; add its CA to `outbound.tls.ca_file`.") |
  | `attempt`, `at` | the attempt's number and time |

  The engine tells the steps apart from what Go's transport reports: a `*net.DNSError` is `resolve`, a dial error `connect`, a TLS record
  or verification error `tls`, a timeout while the body is read `body` (`internal/proxy/classify.go` gains the step beside the code). A
  failed exchange names its own step wherever the run was; `plan`, `verify`, `gate` and `store` name failures that are no exchange's
  ([Q-CUR-81](#q-cur-81--the-step-of-a-failure-is-the-exchanges-where-an-exchange-failed)). A run that reaches the job's 30 minutes ends
  its attempt `bound`, step `body`, code `download_timeout` ([Q-CUR-83](#q-cur-83--a-probe-without-a-version-is-a-row-named-by-its-file-a-run-that-reaches-its-timeout-is-a-bound)).
- **A client's failed fetch** is recorded in the same table (Q-CUR-19), so the tab answers "why did my pull fail": a fetch of a client
  that ends with a failure of the classes above writes, at most once per repository and file per replica and minute, one row of origin
  `client` and state `failed` per repository and file, whose `occurrences` and failure the next failure updates; at most
  `proxy.downloads.client_failures_per_repository` (1,000) such rows per repository, the oldest removed. A `404` of the upstream is not
  recorded (the negative cache holds it, R-REPO-028). The leader of a client's flight records it where the engine classifies the failure
  and where the integrity check refuses ([Q-CUR-84](#q-cur-84--a-clients-failed-fetch-is-recorded-on-the-fetchs-own-failure-once-per-flight)).
- **Retention:** rows `done` and `cancelled` are removed `proxy.downloads.done_retention` (168 h) after they finished, rows `failed` after
  `proxy.downloads.failed_retention` (720 h), a bulk with its rows when its last row is removed; the task `remote-downloads-dispatch`
  removes them in batches of 1,000.
- A row is a download ahead of its repository: a virtual repository has no queue; a download through a virtual repository's address is
  started at the member that serves it (Q-REPO-367).

Acceptance criteria:

- Unit (`internal/proxy`, `TestR_CUR_011_TheFailureOfAnExchangeInPreciseWords`, milliseconds, about 30 transport and answer failures
  produced by `httptest`, a closed port, a resolver that answers no name, an untrusted certificate and a deadline): each maps to its
  class, step and code; the query is dropped from the address; the excerpt is cut and cleaned.
- Unit (`internal/remotecuration/queue`, milliseconds, a store in memory and a fixed clock): the order of the waiting rows, the
  per-repository bound of running rows, the backoff and `Retry-After`, the end at `max_attempts`, the bound of waiting rows, the throttle
  of a client's failures.
- Integration (area `npm`, `TestR_CUR_011_AFailureInItsOwnWordsAndTheRetries`, about 4 s; the test makes the waiting row due and runs the
  task through the system tasks' API instead of waiting for the backoff): the stub answers `503` twice and then `200`: the row shows `upstream_error`,
  step `status`, the stub's address without its query, status 503 and the excerpt, then `done` after the third attempt; a stub that
  answers `401` ends the row `failed` at once with `upstream_auth`.

### R-CUR-012 · The queue's API
Priority: **P1** · Covers: RMT-027

Below `/api/v1/projects/{projectKey}/repositories/{repoKey}` (tag `remote-curation`; R-CUR-005 for who):

| Method and path | What | Answers |
|---|---|---|
| `GET downloads?state=&class=&origin=&bulk=&name=&cursor=&limit=` | the rows, newest first within `waiting` and `running` first; `class` filters failures | `200`, cursor paging; `"counts": {"waiting", "running", "failed", "done"}` |
| `GET downloads/{id}` | one row with its failure | `200` |
| `POST downloads/{id}/retry` | a `failed` or `cancelled` row waits again with its attempts reset; a row of origin `client` becomes a download ahead of that file's version by the caller (R-CUR-010) | `200` with the row; `409 not_retryable` for another state |
| `POST downloads/retry` | `{"class"?, "bulk"?}`: every failed row of the repository that matches, at most 10,000 | `200` `{"retried": <n>}` |
| `POST downloads/{id}/cancel` | a waiting row is `cancelled`; a running row stops after its current file | `200` |
| `GET download-bulks/{id}` | the bulk with its counters and the counts of its rows by state | `200` |
| `POST download-bulks/{id}/cancel` | stops the expansion and cancels its waiting rows | `200` |

- A row's representation: `{"id", "name", "version", "origin", "state", "outcome", "attempts", "maxAttempts", "nextAttemptAt",
  "startedBy": {"id", "name"} | null, "bulk", "request", "files", "stored", "present", "refused", "bytes", "failure", "createdAt",
  "startedAt", "finishedAt", "delivery"}` (`delivery` as in R-CUR-009, so an examined row says it is not delivered). `startedBy` names a
  person to those who may set rules and to that person; to other readers it reads `{"id": null, "name": null}` with `origin`.
- Retry and cancel write `remote.download.retried` and `remote.download.cancelled` (R-CUR-019).

Acceptance criteria:

- Integration (area `npm`, inside `TestR_CUR_011_…`): the list filters by `class=upstream_auth`; a retry of the failed row runs again and
  writes its event; a cancel of a waiting row leaves the stub untouched; the Reader of `web` sees the rows without the starter's name.

### R-CUR-013 · A probe's background download is a row of the queue
Priority: **P1** · Covers: quader-only

The owner's decisions of 2026-11-19 (W-25) about the trigger, the platforms, the bound and every format stand
([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-065). What changes:

- The decision's step 5 inserts a row of origin `probe` (with the probe's files, document and digest, and the caller's ticket) instead of
  a job; a probe without a version (an OCI blob) is a row named by its file (Q-CUR-83); a version that has a waiting or running row starts
  nothing (`active`); "fewer than `limits.maxConcurrentFetches`" becomes the
  bound of waiting rows of R-CUR-011, and the outcome `queue_full` is counted only there.
- The run is R-CUR-011's: the switch `downloadOnProbe` still decides at the probe; a row that waits while it is turned off is `skipped`.
- **What a user sees** is the row on the tab **Downloads** (R-CUR-018), with its counters and its failure, instead of a job on the jobs
  page that only `settings:read` holders saw.
- The metric `quader_remote_background_downloads_total{format,outcome}` gives way to `quader_remote_downloads_total{repo,origin,outcome}`
  (R-CUR-019): the queue is a repository's, and the label `repo` is capped by R-OPS-007.

Acceptance criteria:

- The integration cases of R-REPO-065 (`TestR_REPO_065_*`) run unchanged against the row and its run; one more
  assertion reads the row of origin `probe` with its counters.

### R-CUR-014 · Fetched for examination, not delivered
Priority: **P1** · Covers: SEC-007

The owner's words of 13:01: a Maintainer fetches one version or every version of a library so that Quader examines it, without making
it deliverable.

- **A download ahead does not depend on the rules.** It stores a version whatever the rules say; the scans run on what it stores as on any
  stored item, because every scan starts at the item store's commit, whatever stored the item (`internal/app/wire_formats.go`, the
  malware hook, the vulnerability hooks, the image scan's wake-up and the signature recording); the content is not served until a rule
  delivers it (R-CUR-003). "Stored, examined, not delivered" is the state of such a version.
- **What each gate does to an examination fetch** (Q-CUR-21):
  - **The vulnerability miss gate** (R-VULN-015 "Remote miss", `internal/proxy/fill.go`) keeps refusing a fill of a version it knows as
    malicious, blocked in triage, or in scope with incomplete data under `onMissingData: block`: the verdict is the examination's answer,
    and known-malicious content is not brought in to look at. The row ends `done` with `outcome: refused` when every file was refused, or
    the file counts in `refused`, and the failure names the step `gate`, the code `vulnerability_blocked` and the gate's own text.
  - **A fill without a client is no download.** The miss gate's refusal of such a fill (a mirror's pass, a download ahead) is not counted
    in `quader_vuln_downloads_blocked_total` and writes no `vulnerability.download_blocked`: today both are written for it
    (`internal/vuln/enforce/enforce.go`, `Refuse`, called from `Fill`), which counts system work as refused downloads. The refusal is the
    fill's outcome, in the queue row or the mirror's pass counters ([vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015, a
    sentence added; Q-CUR-22).
  - **The malware gate and the signature gate** never refuse a fill and judge downloads only (R-MAL-013, R-SIG-077): unchanged. A
    background fill has no client, so nothing is held (R-MAL-015). A blob stored for examination is scanned at the priority `new`.
- **What a verdict says, per format** (Q-CUR-21):

  | Format | Before any fetch | After the fetch | Never |
  |---|---|---|---|
  | npm, NuGet | the vulnerability verdict by name and version, through `upstream/vulnerabilities` (R-CUR-009), while the remote's upstream is in scope (R-VULN-010) | malware; signatures (npm's registry signature and attestations, NuGet's embedded signature) | — |
  | OCI | — | the image's vulnerabilities once the platforms of `oci.platforms` are stored (R-VULN-042); malware per blob; signatures (fetched by `signature-fetch`, R-SIG-026) | the platforms outside the list |
  | Debian | — | malware | vulnerabilities (no analysis, `not_covered`, R-VULN-001); signatures (only the release is signed, R-SIG-061) |
  | Ansible Galaxy | — | malware; the signatures in the version document | vulnerabilities (`not_covered`) |

  The screens show a verdict that cannot exist as that sentence ("Debian packages are not analysed for vulnerabilities"), never as an
  empty cell.
- **What a malware find does to such an item.** It is kept, as every infected item is ("Nothing is deleted", R-MAL-002), with its verdict
  visible to whoever reads the repository; the repository's retention of what is not delivered removes it (R-CUR-016). An allow does not
  make it downloadable: the malware gate refuses it until an instance auditor releases it (R-MAL-021).
- **Two acts, shown apart** (Q-CUR-11). For a version, the screens say which holds it back: "Not allowed in this repository" (the
  rules; the action **Allow** for those who may set rules) and, separately, each gate that refuses it ("Malware found", "Blocked:
  vulnerability", "Signature not verified"; lifted only by a release or a triage of the holders of `vulnerabilities:triage`, who are
  named). An allowed version a gate refuses reads "Allowed; refused by the malware gate", so it does not look like a defect of the rules.
- **Who sees the findings:** whoever reads the repository sees a stored version's findings, malware verdict and signatures
  (R-VULN-019, R-MAL-022, R-SIG-034); a Maintainer who holds no triage capability sees everything needed to decide and may rescan
  (`manage`, R-VULN-011); the security dashboard stays the auditors'.
- **The listing.** A version a remote stores and its rules do not deliver is listed by R-API-081 like every stored version, with
  `available: false` and `unavailableReason` (`blocked`, `notAllowed`) in its repository entry (`PackageVersionRepository`,
  [api-and-ui.md](api-and-ui.md) R-API-081, the member that a disabled repository already sets); no install command and no **Download**
  are offered through that repository (Q-CUR-26).
- **Not a download:** a fetch for examination writes no download statistics and no usage download; whoever downloads the stored file
  through a gate that lets it through counts one, as always.

Acceptance criteria:

- Integration (area `npm`, `TestR_CUR_014_…`, about 3 s, the stub's corpus and the test `MAL-` record): in mode `onlyAllowed` the
  Maintainer downloads `@acme/a@1.0.0` ahead: it is stored, its malware row exists at priority `new`, the package API lists it with
  `available: false` and `notAllowed`, a client's request answers `403 not_allowed_in_repository`; an allow serves it without an upstream
  request; a download ahead of `quader-test-mal@1.0.0` ends `refused` with step `gate`, nothing is stored, and
  `quader_vuln_downloads_blocked_total` is unchanged.

### R-CUR-015 · Requests for an allow
Priority: **P1** · Covers: SEC-029

The owner's words of 12:57: in a repository that delivers only what is allowed, a user who may not allow asks for a package or a package
and version; those who may allow see the requests in that repository, set apart; an allow by one of them starts the download.

```sql
CREATE TABLE remote_delivery_requests (
  id              uuid        PRIMARY KEY,                          -- UUIDv7
  repo_id         bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
  name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 1024),
  version         text        NULL CHECK (char_length(version) BETWEEN 1 AND 256), -- NULL: the package, future versions included
  state           text        NOT NULL CHECK (state IN ('open', 'allowed', 'declined', 'withdrawn')),
  askers          integer     NOT NULL DEFAULT 0,                   -- people who asked and did not withdraw
  refusals        bigint      NOT NULL DEFAULT 0,                   -- refusals of clients counted in it (R-CUR-004)
  first_at        timestamptz NOT NULL DEFAULT now(),
  last_at         timestamptz NOT NULL DEFAULT now(),
  decided_by      uuid        NULL REFERENCES principal (id) ON DELETE SET NULL,
  decided_at      timestamptz NULL,
  decline_reason  text        NULL CHECK (char_length(decline_reason) BETWEEN 1 AND 1000),
  examination_refused text    NULL CHECK (char_length(examination_refused) BETWEEN 1 AND 1000) -- why the examination did not start
);
CREATE UNIQUE INDEX remote_delivery_requests_open
  ON remote_delivery_requests (repo_id, name, coalesce(version, '')) WHERE state = 'open';
CREATE INDEX remote_delivery_requests_list ON remote_delivery_requests (repo_id, state, last_at DESC, id);

CREATE TABLE remote_delivery_request_askers (
  request_id  uuid        NOT NULL REFERENCES remote_delivery_requests (id) ON DELETE CASCADE,
  principal   uuid        NOT NULL REFERENCES principal (id) ON DELETE CASCADE,
  note        text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
  asked_at    timestamptz NOT NULL DEFAULT now(),
  withdrawn   boolean     NOT NULL DEFAULT false,
  PRIMARY KEY (request_id, principal)
);
-- A person's own entries (mine=true) and the bound of open entries per person: the key leads with the request.
CREATE INDEX remote_delivery_request_askers_principal ON remote_delivery_request_askers (principal, request_id);
```

- **One entry per subject.** The same package (or package and version) asked by several people is one open entry with every asker
  and its count; what clients asked for and the rules refused counts in the entry of the same subject (`refusals`, R-CUR-004), so a
  subject that only clients asked for is an entry with no asker. The list tells them apart: "asked by 3 people", "refused to clients 214
  times", or both (the owner's addition of the development agent, T-24 item 5).
- **Asking** (`POST delivery-requests`, `{"name", "version"?, "note"?}`): the caller may read the repository and may not set rules; the
  subject is checked by the catalogue's lookup (`Package`), so a typo cannot become a request (`404 catalogue_not_found`); a subject the
  rules deliver answers `409 already_delivered`. At most `proxy.delivery.open_requests_per_person` (20) open entries per person and
  repository (`429 too_many_requests`). In mode `allExceptBlocked` the same act asks for a blocked package or version: one mechanism.
- **Examination at once** (Q-CUR-23): with the repository's `remote.delivery.examineOnRequest` on (the default), a new entry starts a
  download ahead of origin `request` (R-CUR-010): the version asked for, or of a package the newest version that is no prerelease
  (`proxy.delivery.examine_versions_per_request`, 1), charged to the asker. A refusal by the asker's budget starts nothing and the entry
  says so (`examinationRefused`). A second asker starts nothing more. The newest versions of a package are named by the worker that
  expands a bulk of them (Q-CUR-141), whose rows carry the origin `request`, the entry and the asker's charge.
- **Who sees them:** those who may set rules see every entry of the repository; the tab **Upstream** shows the count of open entries in
  its label and lists them first, set apart (R-CUR-018); an asker sees the entries they asked in, with the state and the reason of a
  decline.
- **Allow** (`POST delivery-requests/{id}/allow`, `{"as": "asked" | "package", "downloadEveryVersion": false, "note"?}`): writes the rule of
  the subject (`allow`, `request_id` set; `as: package` widens a version's entry to its package), sets the entry `allowed`, and starts the
  download at once: nothing more where the examination stored the version (it is delivered from that moment), else a row of origin `allow`
  of the version asked for (of a package, a bulk of its newest version that is no prerelease, Q-CUR-141), and with `downloadEveryVersion` a
  bulk of the package (R-CUR-010). A row of the examination that failed or was cancelled is started again; one that waits runs as it is.
  "An allow starts the download" holds in every case. An entry that is not open answers `409 conflict`.
- **Decline** (`POST delivery-requests/{id}/decline`, `{"reason", "discard": true}`): a reason of 1 to 1,000 characters; the entry is
  `declined`; its examination's waiting rows and expanding bulk are cancelled; with `discard` (the default) what was stored for its
  examination is discarded (R-CUR-016, Q-CUR-143). Later refusals of clients count in the declined entry; a person's new ask opens a new
  entry.
- **Withdraw** (`POST delivery-requests/{id}/withdraw`): an asker leaves the entry; an entry with no asker and no refusal in
  `proxy.delivery.request_retention` (720 h) is removed by the task of R-CUR-016. At most `proxy.delivery.max_client_entries` (10,000)
  entries without an asker per repository: a client that is refused many names in mode `onlyAllowed` cannot grow the list without end;
  the entry with the oldest last refusal goes first.
- **The list** (`GET delivery-requests?state=&origin=person|client&mine=&cursor=`): `{"id", "name", "version", "state", "askers": [{"id",
  "name", "note", "askedAt"}] (names only for those who may set rules and for the asker themselves), "askerCount", "refusals", "firstAt",
  "lastAt", "examination": <queue row>, "examined": [{"version", "fetchedAt", "files"}], "examinationRefused", "decidedBy", "decidedAt",
  "declineReason"}`. A caller without `manage` sees only the entries it asks in. The verdicts of an examined version are the stored
  version's, read where every stored version's are (R-API-081; Q-CUR-146).
- **Audit:** `remote.request.created`, `remote.request.allowed`, `remote.request.declined`, `remote.request.withdrawn`, transactional and
  webhook events (R-OPS-037), so an installation can tell people outside the page; Quader sends no mail of its own.

Acceptance criteria:

- Integration (area `npm`, `TestR_CUR_015_…`, about 4 s): the Developer of `web` asks for `@acme/b`; its newest version is downloaded for
  examination, charged to the Developer; the Maintainer's list shows the entry with one asker and, after a client's refused `npm view`,
  `refusals: 1`; the Maintainer allows it: the rule exists, the entry is `allowed`, a client fetches the tarball without an upstream
  request; a second entry is declined with a reason and its examined tarball is gone; the Developer reads both states.

### R-CUR-016 · What was fetched for examination is not kept for ever
Priority: **P1** · Covers: quader-only

Nothing removes an unused cached item today (R-STOR-045 is specified and not built; `remote.cache.unusedRetentionHours` above 0 is
refused, `internal/repository/validate_remote.go`), so content fetched for examination would stay for ever. Content that was delivered
when it was stored and that a rule refuses later is not examination: it stays stored and is not served, as a cached item a rule refuses
always did (R-CUR-003); a discard or R-STOR-045 removes it.

```sql
CREATE TABLE remote_examinations (
  repo_id     bigint      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
  name        text        NOT NULL,
  version     text        NOT NULL,
  fetched_at  timestamptz NOT NULL DEFAULT now(),
  request_id  uuid        NULL REFERENCES remote_delivery_requests (id) ON DELETE SET NULL,
  files       text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(files) <= 2000), -- the paths its runs stored or found (Q-CUR-140)
  PRIMARY KEY (repo_id, name, version)
);
CREATE INDEX remote_examinations_age ON remote_examinations (fetched_at);
```

- **What is examination.** A run of the queue that stores a file of a package's version that the rules do not deliver at that moment
  writes the version's row (once; a later run keeps `fetched_at` and adds the files it found) with the paths of the files the run stored or
  found (Q-CUR-140); a document it stores (a Debian index that **Read the index** reads, R-DEB-036) is none. The row goes when the rules
  come to deliver the version (the write of an allowing rule, or the removal of a block, deletes the rows of its subject that the rules
  then deliver; a widening the write does not see, such as the mode, is caught by the task below, Q-CUR-142), at a discard and at the
  removal below.
- **Discard** (`POST examinations/discard`, `{"name", "version"?}`, those who may set rules): removes the stored files of the versions
  of the subject that the rules do not deliver, as an eviction (R-AUTHZ-006 `delete` on a remote), with their rows, and writes
  `remote.examination.discarded`, flagged `journal: revocation`, so a restore removes them again (replay: evict what is stored of the
  subject and still not delivered; nothing else; the replay makes the subject's rows due and queues a run of the task below, Q-CUR-144).
  A delivered version is never discarded (`409 delivered`). The files of a version are those its examination row names and the items
  the package rows link to it, each kept when the format classifies it as a file of that package and version; a file the version shares
  by its digest (an image's manifest and layers) stays (Q-CUR-140). The answer is `{"versions", "files"}`.
- **Removal after a time without a decision:** the task `remote-examination-cleanup` (daily, `0 4 * * *`, R-OPS-019) removes the stored
  files of every version whose examination row is older than the repository's `remote.delivery.examinedRetentionDays` (30 by default, 1
  to 365) and that the rules still do not deliver. It writes the same event with the actor `system:examination-cleanup`, in batches of
  1,000, each version in a transaction of its own so that a run that stops keeps what it did. It also drops the rows of versions the rules
  deliver now without removing anything (Q-CUR-142), and removes the request entries without an asker after
  `proxy.delivery.request_retention` (R-CUR-015). It removes content, so it is mutating: it does not run in read-only maintenance mode
  (R-OPS-024).
- **With R-STOR-045, once it is built:** its task leaves a file of a version with an examination row to this rule, and counts the last
  use of a file the rules deliver from the later of its last download, its storing and the creation of the rule that delivers it, so an
  allow is a use and an examined file that waited for its decision is not removed just after it.
- What removal does to the scans' state follows their own rules: findings resolve `version_removed`, the malware row goes with the next
  orphan sweep (R-VULN-011, R-MAL-004).

Acceptance criteria:

- Integration (area `npm`, `TestR_CUR_016_…`, about 2 s, a test clock): an examined version older than the days is removed by the task and
  its event recorded; a delivered one stays; a discard records the journal entry, and its replay unit evicts a restored copy.

### R-CUR-017 · Future versions, and the mirror
Priority: **P1** · Covers: quader-only

- **Allowing future versions** is a `package` rule (or a `section` rule): a version the upstream publishes later is delivered when a
  client asks, without anybody's act.
- **Keeping them fetched** is what the mirror mode does ([repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-050 to
  R-REPO-058): its passes discover new versions and store them before a client asks. There is no second mechanism: no per-package
  "keep fetched" flag (Q-CUR-25).
- **The mirror's scope** is `remote.mirror.include`, a list of name patterns of R-AUTHZ-005 (at most 100), a member of `remote.mirror`,
  which only `repositories:write-config` sets and which a project's Owner keeps as stored (Q-REPO-142); empty means "everything the rules
  deliver". A pass stores what both admit: a name the scope covers and the rules deliver, a version the rules deliver. The `names` method
  takes the scope's patterns without a wildcard as exact names (as it took the include patterns), and in mode `onlyAllowed` also the
  names of the `package` rules with `allow` and of the `version` rules, so an allow-list is mirrored by naming it. A change of the rules
  that widens what is delivered makes the next pass full (the state's fingerprint covers the scope and the rules' revision).
- **The bound of a well-known upstream** (R-REPO-052) is met by a scope of `remote.mirror.include` that passes today's bound rule
  (`discover.LiteralPrefix`) or by `remote.mirror.maxBytes`; the rules alone are no bound, because a Maintainer can allow `**`. The
  bound is checked when a pass starts as well as when the mirror is saved: a stored mirror without it does not run and its status reads
  `unbounded` (Q-CUR-43).
- Q-REPO-193 freezes for an Owner the scope, the URL and the extra hosts of a mirror; the include and exclude patterns leave that rule with
  the patterns of a remote. A Maintainer's rules widen a mirror only within the scope an administrator set.

Acceptance criteria:

- Integration (area `mirror`, the npm stub, `TestR_CUR_017_…`, about 3 s): a mirror with `remote.mirror.include: ["@acme/*"]` and mode
  `onlyAllowed` with `package @acme/a allow` stores `@acme/a` only; adding `package @acme/b allow` makes the next pass full and stores
  `@acme/b`; a version the stub publishes after a complete pass is stored by the next incremental pass.

### R-CUR-018 · The screens
Priority: **P1** · Covers: ART-044, SEC-037

The owner's word of 12:38: "Hier will ich also einen Reiter", and a second one for the queue (Q-CUR-1). The style is R-UI-067's and the
style guide's; every screen is rendered in both schemes, wide and narrow, with realistic data before it lands (rule 11).

- **Tabs on a remote repository's page** (R-UI-052 changed, Q-CUR-1): under the header a tab strip (`tab-nav`, links with
  `aria-current="page"`, as the page **System status** has, R-UI-164) with **Packages** (**Images** for OCI; the content as today), **Upstream**
  and **Downloads**, each with its address: `…/repositories/{key}`, `…/repositories/{key}/upstream`, `…/repositories/{key}/downloads`;
  the document title and the breadcrumb end with the tab. **Details** follows them, the last tab of every repository's page that has
  details ([Q-UI-1067](api-and-ui.md#q-ui-1067--details-is-the-last-tab-of-a-repositorys-page-not-a-section-under-its-list)). A local or
  virtual repository has no **Upstream** and no **Downloads**, and the address of its tab Downloads (and of its tab Upstream) leads to its
  content. A tab's filters stay in its address. A visitor without a session, whom
  the catalogue and the queue do not answer, sees neither **Upstream** nor **Downloads**, and no strip where **Details** is not there
  either, because one tab is no strip
  ([Q-CUR-150](#q-cur-150--the-strip-holds-the-content-upstream-and-downloads-for-every-signed-in-reader-the-strip-counts-the-open-requests-itself), which replaces
  Q-CUR-130). The label **Upstream** carries the count of open requests for those who may set rules (`Upstream 3`); **Downloads** the count
  of failed rows when there are any (`Downloads · 2 failed`).
- **The tab Upstream** (`…/upstream`):
  - At the top, one line about the mode: "Delivers everything except what is blocked." or "Delivers only what is allowed.", with
    **Change** to the setting for those who may set the configuration, and for those who may set rules **Rules (n)**, which opens the
    rule list.
  - For those who may set rules and while open entries exist, the section **Requests** first, set apart (a bordered panel with its count):
    each entry's subject, who asked or how often clients were refused, the examination's state and verdicts, and **Allow** and
    **Decline**; an asker sees **Your requests** in the same place.
  - The search: a field as wide as a name, the toggle **Names only** where the format has it (Debian), and under it one sentence in the
    screen's language of what a person can type and find, by the code `searchHint` ("Search for an image name, for example nginx;
    official images are under library/<name>."); where the capability is `none`, the field becomes **Look up by name** and the sentence
    says why and what to enter instead ("ghcr.io offers no search; enter an image's full name, for example stefanprodan/podinfo.").
    The sentences per code are [Q-CUR-166](#q-cur-166--the-hint-under-the-upstream-search-is-a-code-the-screen-says-in-its-language)'s.
  - Where the format has levels, a level bar beside the results (the breadcrumb of the level, its children as a list); for Debian the
    suites the remote knows and **Read the index** for a suite or component whose index is not read yet (a download ahead of the index
    documents, R-DEB-036).
  - The results: one row per package: name (wrapping inside itself), the upstream's description (one line, cut), the latest version, the
    state as a badge (**Allowed**, **Blocked**, **Not allowed**, **Requested**), **Stored** with the count of stored versions.
  - A package opened (in place, as the content list's rows open, `collapse-region`): its versions, newest first, paged by 100 with
    **Load more** and a filter field for the version; per version its published time, size (or "size not told by the upstream"), flags,
    the delivery state, stored or not, the verdicts (with the sentence of a verdict that cannot exist), a queued download's state; the
    actions per version and per package: **Download** (into the cache; for an examination **Download for examination**), **Allow**,
    **Block**, **Remove rule**, **Request** (for a reader), and on the package **Download every version…** (opens the preview) and
    **Allow every version** (future ones included).
  - States: no result ("No package of the upstream matches “…”."), the upstream down ("The upstream is assumed offline until 10:42; Quader
    retries then." with the circuit's time), the budget used up ("The upstream catalogue's budget of this repository is used up; try again
    in 40 s."), a document too large ("This package's document is larger than 64 MiB; Quader does not read it for browsing."), a format
    without search, a long name, a package with thousands of versions (the filter and the paging; "Showing 100 of 3,893"), a reader in mode
    `onlyAllowed` ("14 hits are not allowed in this repository; look a package up by its exact name to ask for it.").
  - **As built (R5b).** The line about the mode with **Change** (to `…/settings#delivery`, which opens the section **Delivery**) for
    those who may set the configuration, and **Rules** with the count as a badge for those who may read the rules. The requests panel
    under it ([Q-CUR-153](#q-cur-153--the-requests-panel-answers-by-a-rows-menu-and-dialogs-the-rule-list-is-a-dialog-that-adds-sub-sections)).
    One field for the search and the lookup: **Search** and **Look up by exact name** where the upstream answers a search (by itself or
    over Quader's index, with **Names only** for Debian), **Look up** alone where it does not, with the upstream's sentence under the field;
    the address carries `q`, `name`, `level` and `namesOnly`, so the refusal's link of R-CUR-004 (`?name=`) opens the package
    ([Q-CUR-151](#q-cur-151--one-field-searches-and-looks-up-the-address-holds-the-query-the-levels-are-a-breadcrumb-and-links)). The
    levels: a breadcrumb under **Upstream** and the children as links; a root that lists nothing (an npm scope, a Docker Hub namespace,
    Q-CUR-63) offers a field named by the level's kind to enter one by its name; a level the upstream does not have says so by its kind
    ("Docker Hub has no namespace “guided-traffic”."), with the rule its name breaks ("Docker Hub allows only lowercase letters and
    digits in this name, no “-”.") and **Did you mean “guidedtraffic”?** as a link where the upstream has the name the rule makes
    ([Q-CUR-165](#q-cur-165--a-level-the-upstream-does-not-have-says-so-with-the-formats-naming-rule-and-one-suggestion-built-from-it));
    **Download this sub-section…** at a level that lists
    packages, for those who decide. The results: **Package** (name, the description cut to one line with the whole as its title),
    **Latest**, **State** (**Allowed**, **Delivered**, **Blocked**, **Not allowed**, **Requested**), **Cache** (**Stored** where the
    repository holds a version: the API answers no count, Q-CUR-61). A package opens in place: its description where nothing showed it,
    its tags, its state with the rule that decided, **Download every version…**, **Allow every version** and **Block every version** (the
    one the package's rule has is **Remove the allow** or **Remove the block**) for those who decide, **Request every version** for a
    reader; **Filter versions** and "Showing 100 of 3,893" with **Load more**; per version **Version** (with the published time and the
    size under it, "Size not told"), **Delivery** (with the rule that decided under it), **Cache** (**Stored**, **Stored for
    examination**, **Waiting**, **Running**, **Not stored**, with the verdicts under it) and the acts
    ([Q-CUR-152](#q-cur-152--a-versions-acts-are-a-word-for-a-reader-and-a-menu-for-those-who-decide-the-rule-is-a-column-the-verdicts-a-word-or-a-link)),
    a table from 1,024 px on
    ([Q-CUR-157](#q-cur-157--a-packages-versions-are-three-columns-the-facts-under-the-version-the-rule-under-the-delivery-the-verdicts-under-the-cache)).
    The catalogue's states are sentences in a warning line
    ([Q-CUR-155](#q-cur-155--the-catalogues-own-states-are-sentences-of-the-screen-the-upstreams-own-sentences-stand-as-the-upstream-wrote-them)).
- **The preview of a bulk** (a dialog): what will be fetched (versions, packages, bytes, "size of 12 versions not told by the upstream;
  the bound of 100 GiB stops the download when it is reached"), what is stored already, how much is not delivered (fetched for
  examination), the bounds as fields with their values, **Start download**.
- **The rule list** (a dialog or a page section): kind, subject, effect, note, who and when, **Remove**; a field to add a section rule
  (pattern) with its effect; the sentence "The narrowest rule decides; at equal width a block wins.".
- **The tab Downloads** (`…/downloads`): the counts by state as filters (**Waiting 12**, **Running 4**, **Failed 2**, **Done**), the filter
  **Failure** by class, and a table: subject (name and version), origin (**You**, a person's name for those who may see it, **Request**,
  **Allow**, **Bulk** with a link to its bulk, **Probe**, **Client**), state, progress (files stored of files, bytes), attempts ("Attempt
  2 of 5, next at 10:41"), and for a failed row the failure in one line ("TLS · `registry.example.com:443` · certificate not trusted");
  the row opens to the whole failure: step, code, address (with "query removed"), status, the excerpt in a code block, the detail, and
  **Retry**; **Retry all failed** (with the class filter) and **Cancel** for those who may. A bulk's line shows its counters and
  **Cancel bulk**. The tab refreshes every 5 s while something waits or runs, with **Pause automatic refresh** as the page System status
  has it.
  - **As built (R5a).** One muted sentence says what the tab lists. The head: **All** and the four states as pressed buttons with their
    counts, **Failure** as a select of the fifteen classes, and at the row's end **Retry all failed** and **Pause automatic refresh**;
    the address carries `state`, `failure` (the class) and `bulk`
    ([Q-CUR-131](#q-cur-131--the-tabs-filters-are-the-states-as-pressed-buttons-with-their-counts-and-failure-as-a-select-in-the-address)).
    The table: **Package** (name and version, a row that fetches what the rules do not deliver marked "Not delivered here: stored for
    examination"), **Origin**, **State** (the badge, a done row's outcome, "Attempt 2 of 5 · next in 4 minutes"), **Progress** ("2 of 3
    files · 46.0 MiB", the files a gate refused), **Failure** (the class, the upstream's status or else the engine's code, the host:
    "Upstream error · HTTP 503 · registry.npmjs.org"). A row opens in place to its whole failure (the sentence, the class and the step
    each in words with its value in a muted line of code below
    ([Q-CUR-134](#q-cur-134--a-failures-class-and-step-stand-in-words-with-their-value-in-a-muted-line-of-code-below)), the code, the file, the address "shown without its query, which can carry a credential", the
    upstream's status, Retry-After, the start of the upstream's answer in a code block) and to its facts (who started it, where the API
    names a person; queued, started, finished; the files named, stored, already stored and refused; the bytes). A row's one action is
    **Retry** or **Cancel** by its state, in its action cell
    ([Q-CUR-132](#q-cur-132--a-row-offers-retry-or-cancel-by-its-state-to-whom-the-api-lets-change-it-and-a-bulks-line-stands-over-its-rows)).
    The origin **Bulk** links to `?bulk=<id>`, which shows the bulk's line over its rows: what it downloads, its state, "38 of 38
    versions found are queued", its rows by state, its bounds, who started it, **Cancel bulk** and **Show every download**. The
    refresh reads as many rows as are shown, so **Load more** and the opened rows stay
    ([Q-CUR-133](#q-cur-133--the-tab-reads-the-queue-again-while-the-repository-has-a-row-that-waits-or-runs-keeping-what-is-shown)).
    The excerpt is shown to every reader of the repository, as the API answers it
    ([H-1728](../security/remote-curation.md#h-1728--the-excerpt-of-an-upstreams-error-answer-is-shown-to-every-reader-of-the-repository)).
- **The settings page** (R-UI-087): a closed section **Delivery** (details on demand, R-UI-067) with the mode as two radio choices and
  their sentences, **Fetch for examination when someone asks** (on), **Remove what was examined and not allowed after [30] days**; for a
  mirror, `remote.mirror.include` joins the section Mirror as **Mirror only these packages**. As built (R5b): the section stands after **Mirror** on
  every remote but a system repository, saves `remote.delivery` alone with the page's `ETag`, and links the tab **Upstream** for the
  rules ([Q-CUR-154](#q-cur-154--the-section-delivery-saves-remotedelivery-alone-and-leads-to-the-rules-on-the-tab-upstream)).
- **German texts** follow the glossary (`web/src/locale/GLOSSARY.md`), without "du": allow `zulassen` is taken (a gate's release) and
  `freigeben` renders exposure, so allow is **erlauben** (Erlaubt), block **sperren** (Gesperrt, named with "durch eine Regel" where a
  vulnerability block could be meant), request **beantragen** (Antrag, the owner's word), decline **ablehnen**, discard **verwerfen**,
  download ahead **In den Cache laden**, the modes **Alles außer Gesperrtem** and **Nur Erlaubtes**; the glossary gains these rows.

**Layouts** (*example*). Wide, a reader with `manage`, mode `onlyAllowed`, npm:

```text
┌─────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ npm-remote                                                              [Set up client] [Settings] [⋮]  │
│ Proxy of registry.npmjs.org                                     npm  remote                             │
│ ───────────────────────────────────────────────────────────────────────────────────────────────────────  │
│  Packages    Upstream 3    Downloads · 2 failed                                                         │
│ ═══════════════════════════════════════════════════════════════════════════════════════════════════════ │
│ Delivers only what is allowed.  Change · Rules (42)                                                     │
│ ┌ Requests (3) ─────────────────────────────────────────────────────────────────────────────────────┐   │
│ │ @gitbeaker/rest       every version   asked by 2 people · refused to clients 14×                  │   │
│ │   examined 39.2.0: malware none found · vulnerabilities none · signature verified  [Allow][Decline]│   │
│ │ axios 0.21.1          one version     refused to clients 3×   not examined          [Allow][Decline]│   │
│ └───────────────────────────────────────────────────────────────────────────────────────────────────┘   │
│ Search the upstream [ gitlab                      ] (names, descriptions and keywords, ranked by npm)   │
│ 11,235 hits; npm shows at most the first 5,250                                                          │
│ ▸ @gitlab/ui            GitLab UI components                      124.0.3  Allowed       Stored 6        │
│ ▾ gitlab-ai-provider    An AI SDK provider for GitLab              1.4.0    Not allowed  Stored 1        │
│     Filter versions [      ]                       [Download every version…] [Allow every version]      │
│     1.4.0   2026-11-02  size not told  examined: no malware · 0 findings   Not allowed  [Allow][Discard] │
│     1.3.9   2026-10-20  size not told                                      Not allowed  [Download for examination][Allow] │
│     … Showing 100 of 212   [Load more]                                                                  │
└─────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

Narrow (375 px), the same, a Reader without `manage`:

```text
┌───────────────────────────────────┐
│ npm-remote                    [⋮] │
│ Proxy of registry.npmjs.org       │
│ npm  remote                       │
│ [Set up client]                   │
│ Packages  Upstream  Downloads     │
│ ═════════════════════════════════ │
│ Delivers only what is allowed.    │
│ Your requests                     │
│  axios 0.21.1 · open              │
│ Search [ gitlab        ]          │
│ 2 hits. 14 hits are not allowed   │
│ in this repository; look a        │
│ package up by its exact name to   │
│ ask for it.                       │
│ ▸ @gitlab/ui                      │
│   124.0.3 · Allowed · Stored 6    │
└───────────────────────────────────┘
```

A format without search (OCI on ghcr.io), the upstream down:

```text
│ Look up an image [ fluxcd/flux-cli          ]                                                         │
│ ghcr.io offers no search; enter an image's full name, for example stefanprodan/podinfo.               │
│ ⚠ The upstream is assumed offline until 10:42 (3 failures: upstream_unreachable); Quader retries then. │
```

Debian, a suite read and one not read:

```text
│ Suites: trixie ▸ main ▸ amd64        trixie-security (index not read) [Read the index]               │
│ Search the index [ libssl           ] [x] Names only                                                 │
│ Search for words of a package's name or short description, for example nginx; Quader                 │
│ searches only the suites whose index it has read.                                                    │
│ ▾ libssl3t64   Secure Sockets Layer toolkit - shared libraries                                         │
│     3.5.1-1+deb13u1  amd64  2,412,448 bytes  Delivered  Not stored  [Download]                        │
```

The tab Downloads, wide:

```text
│  Packages    Upstream    Downloads · 2 failed                                                                 │
│ Waiting 12   Running 4   Failed 2   Done 230        Failure [ any class ▾ ]       [Retry all failed] [⏸]      │
│ ┌────────────────────────┬─────────┬─────────┬───────────────────────┬──────────────────────────────────────┐ │
│ │ @acme/a 1.2.0          │ Bulk    │ Running │ 1 of 1 files · 2.1 MB │                                      │ │
│ │ lodash 4.17.21         │ Request │ Waiting │                       │ examination for a request            │ │
│ │ left-pad 1.3.0         │ Client  │ Failed  │ attempt 5 of 5        │ status · 503 · registry.npmjs.org/…  │ │
│ │   step status · code upstream_error · https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz (query removed)│
│ │   HTTP 503 · excerpt: "<html><body>Service Unavailable</body></html>"                       [Retry]    │ │
│ └────────────────────────┴─────────┴─────────┴───────────────────────┴──────────────────────────────────────┘ │
```

Acceptance criteria:

- Web unit (Vitest, milliseconds per component, `web/src/app/features/repositories/upstream/`, `…/downloads/`): the tab strip appears for
  a remote only; each state's sentence; the actions by permission; the German texts of the glossary rows. Built for the strip and the tab
  Downloads in [remote-downloads.spec.ts](../../web/src/app/features/repositories/downloads/remote-downloads.spec.ts) (twelve cases, about
  0.7 s, rows of a few hundred bytes each); the renders gain two screens, about 45 s. Built for the tab Upstream and the section Delivery in
  [remote-upstream.spec.ts](../../web/src/app/features/repositories/upstream/remote-upstream.spec.ts) (twelve cases, about 1.3 s) and
  [repository-delivery.spec.ts](../../web/src/app/features/repositories/repository-delivery.spec.ts) (two cases, about 0.2 s), answers of
  a few kilobytes in memory; the renders gain six screens (about 4 minutes on the development machine, most of it the npm walk's
  eleven states as three users).
- UI layout checks (`web/e2e`, the existing walk, about 20 more screens at three widths as the three users): the two tabs in both
  schemes with the states listed above.
- Browser journey (`web/e2e/tests/24-upstream.spec.ts`, the set `access-long`, the npm test upstream, one package of a few hundred bytes):
  a Reader looks a package up by its exact name and asks for a version, a Maintainer allows it from the requests, the version is then
  delivered by the package protocol and stored, the Reader reads the request as allowed, and **Documentation** on the tab names the
  manual's page of curating a remote.

### R-CUR-019 · Audit events, metrics, webhooks
Priority: **P1** · Covers: SEC-032

| Event | Mode | Journal | Webhook | Details |
|---|---|---|---|---|
| `remote.rule.created`, `remote.rule.updated`, `remote.rule.deleted` | transactional | — (a narrowing writes `repository.narrowed`, R-CUR-006) | yes | `ruleId`, `kind`, `subject`, `effect`, `previousEffect`, `note`, `requestId` |
| `remote.delivery.refused` | async, throttled (R-CUR-004) | — | no | `path`, `package`, `version`, `reason`, `kind` |
| `remote.request.created`, `.allowed`, `.declined`, `.withdrawn` | transactional | — | yes | `requestId`, `name`, `version`, `reason` (declined) |
| `remote.download.started`, `.retried`, `.cancelled` | transactional | — | no | the level, the subject, the bulk, the count, the row |
| `remote.examination.discarded` | transactional | `revocation` | no | `name`, `version`, `files`, `cause` (`person`, `retention`, `declined`) |

- Every event names the repository by its ID and qualified name; the actor is the person, or a system subject for the task.
- The Bastion's feed classifies every new journaled event (R-BAS-015): `remote.examination.discarded` yields none (what it removes was
  never delivered, so a Bastion holds none of it); the new parts of `repository.narrowed` are `content` as every part is.

| Metric | Labels | Meaning |
|---|---|---|
| `quader_remote_delivery_refusals_total` | `repo`, `reason`, `kind` | refusals of clients by the rules |
| `quader_remote_downloads_total` | `repo`, `origin`, `outcome` | ends of queue runs (`completed`, `bounded`, `refused`, `skipped`, `failed`, `cancelled`) and the decisions on probes (`off`, `stored`, `active`, `queue_full`, `limited`, `busy`, `not_admitted`) |
| `quader_remote_downloads` | `repo`, `state` | rows by state (gauge, read by the dispatch task every minute) |
| `quader_remote_download_failures_total` | `repo`, `class` | failed attempts |
| `quader_remote_catalogue_requests_total` | `repo`, `call`, `outcome` | catalogue calls (`ok`, `cached`, `budget`, `upstream_error`, `not_offered`, `too_large`, `shape`) |
| `quader_remote_catalogue_seconds` | `call` | duration of a catalogue call (histogram) |
| `quader_remote_delivery_requests_open` | `repo` | open request entries (gauge; specified, not built) |

The label `repo` is capped as R-OPS-007 caps it.

Acceptance criteria:

- Unit (`internal/audit`, milliseconds): the events are registered with their schemas, modes and journal flags; the Bastion's
  `TestR_BAS_015_EveryJournaledEventIsClassified` passes with the new event.

### R-CUR-020 · Configuration
Priority: **P1** · Covers: quader-only

The repository's members (R-REPO-024), configuration of R-AUTHZ-006:

```yaml
remote:
  delivery:
    mode: allExceptBlocked          # default (proposed); onlyAllowed
    examineOnRequest: true          # default (proposed)
    examinedRetentionDays: 30       # default (proposed); 1 to 365
  mirror:
    include: ["@acme/*"]            # example; default (proposed) []: everything the rules deliver
```

The installation's keys ([remote_curation.go](../../internal/config/remote_curation.go), [jobs.go](../../internal/config/jobs.go),
[maintenance.go](../../internal/config/maintenance.go), `docs/developer/configuration.md`; `proxy.downloads.*`, `jobs.workers.downloads` and the schedule of `remote-downloads-dispatch` are read since R4, the rest by their packages). A duration is a
duration and not an integer with a unit in its name ([../developer/configuration.md](../developer/configuration.md#adding-a-key)), so the
catalogue's cache period and call time are `cache_ttl` and `timeout`:

```yaml
proxy:
  delivery:
    max_rules: 10000                          # default; 100 to 1000000, rules per repository
    open_requests_per_person: 20              # default; 1 to 1000, per repository
    examine_versions_per_request: 1           # default; 1 to 100
    request_retention: 720h                   # default; at least 1h
    max_client_entries: 10000                 # default; 1 to 1000000, entries without an asker per repository
  catalogue:
    requests_per_minute: 120                  # default; 1 to 100000, per repository, across replicas
    requests_per_minute_per_person: 30        # default; 1 to 100000
    cache_bytes: 32MiB                        # default; per replica, 0 turns the cache off
    cache_ttl: 300s                           # default; 0s to 1h, 0s keeps nothing
    max_document_bytes: 64MiB                 # default; at least 1KiB
    timeout: 20s                              # default; 1s to 5m
  downloads:
    per_repository: 4                         # default; 1 to 64, running rows per repository
    max_waiting_per_repository: 10000         # default; 1 to 1000000
    max_versions_per_bulk: 10000              # default; 1 to 1000000
    max_bytes_per_bulk: 100GiB                # default; at least 1MiB
    max_attempts: 5                           # default; 1 to 20
    done_retention: 168h                      # default; at least 1h
    failed_retention: 720h                    # default; at least 1h
    client_failures_per_repository: 1000      # default; 1 to 100000
jobs:
  workers:
    downloads: 4                              # default; the queue downloads
maintenance:
  tasks:
    remote-downloads-dispatch: {enabled: true, schedule: "* * * * *"}        # default
    remote-examination-cleanup: {enabled: true, schedule: "0 4 * * *"}       # default
```

- What the defaults cost: the catalogue's cache 32 MiB per `api` replica; the rule sets about 150 bytes per rule per replica; four
  running downloads per repository take at most four of the repository's background slots (R-REPO-055); the dispatch task reads the
  due index once a minute.

### R-CUR-021 · Replicas, Valkey, drains, backups and restores
Priority: **P1** · Covers: quader-only

- **Replicas.** The rules are in PostgreSQL and in each replica's snapshot (R-CUR-006); the catalogue's cache is per replica and holds
  nothing a client is served (INV-3); the budgets use the cluster limiter and fall back to a share per replica without Valkey; the queue
  is rows, its runs jobs, so any `worker` replica runs a row and a drained worker releases its job, whose row the dispatch task finds
  `running` with a job that ended and sets `waiting` without counting the attempt (as the job framework does not count a release).
- **Backups and restores.** The rules, requests, askers, the queue and its bulks are export class `data`. A restore to a point *t*
  replays the journal's narrowings after *t* (R-CUR-006, R-REPO-061) and the discards (R-CUR-016); it sets every `running` row `waiting`.
  `repositories.delivery_rules_revision` is a client-held revision and is raised as R-BAK-022 raises every one, so no filtered document
  rendered under rules after *t* is answered `304` under the restored ones. A Debian remote's `deb_remote_descriptions` are its cache rows,
  purged with its parsed indexes (R-BAK-008).
- **A Bastion** holds no rule; it learns of a narrowing by the `content` entry of `repository.narrowed` (R-BAS-015).
- **A repository that goes away.** A deleted repository takes every row of this page with it (each table's key to the repository
  cascades). A retired well-known repository keeps its row as a tombstone and its content is purged (R-REPO-067 step 5,
  [Q-REPO-376](repositories-and-proxy.md#q-repo-376--a-preset-the-product-no-longer-offers-leaves-a-tombstone-without-content)): the purge
  removes its queue rows, its bulks and its examination rows, which are derived work, and the Debian descriptions with their index; it
  keeps its rules and its requests with their askers, which are people's decisions, as it keeps the repository's configuration
  ([Q-CUR-111](#q-cur-111--the-purge-of-a-retired-repository-removes-its-queue-and-examinations-and-keeps-its-rules-and-requests)).

### R-CUR-022 · Tests and what they cost
Priority: **P1** · Covers: quader-only

By the owner's rule of 2026-11-20 (tests stay runnable): each behaviour once, at the cheapest tier that shows it; a format adds a small,
constant amount; payloads of kilobytes.

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | the rule decision (R-CUR-001), the failure classification (R-CUR-011), the queue's order and retries, the catalogue engine's budget, cache and bounds, the combined serve decision with the fourth check | about 2 s in all | none |
| Unit, the format contract's kit | check 22 (the catalogue against a fake upstream in process) and check 23 (documents and refusals), per format | milliseconds per format, about 0.5 s for five | the fixture's canned documents, a few KB each |
| Integration, per format | one case: a rule refuses a document and an artifact (R-CUR-003), and one download ahead (R-CUR-010) | about 4 s per format, 20 s for five, spread over the shards | the stubs' existing corpora |
| Integration, once (npm) | the virtual walk (R-CUR-004), the queue's failure and retry (R-CUR-011), the examination (R-CUR-014), the requests (R-CUR-015), the retention (R-CUR-016), the catalogue's API (R-CUR-008, R-CUR-009), the permissions (R-CUR-005), the mirror (R-CUR-017) | about 20 s | the npm stub with a search route of 50 lines added |
| End-to-end, per format, in the format's own leg (Q-CUR-161) | what only the client shows: what it prints at the refusal of a remote in mode `onlyAllowed` (npm's `E403` with the text, `dotnet restore`'s warning with the text and `NU1301`, Docker's `error from registry:` with the text, APT's bare `403` from the remote's unchanged index, H-1726, `ansible-galaxy`'s message with the text); after an allow of one version npm (`^1.0.0`), NuGet (`2.*`) and `ansible-galaxy` take the allowed version, not the newer one, and `docker pull` and `apt-get install` succeed; a version downloaded ahead through the queue and then allowed is fetched by the client from the cache (the stub's count) | 2.7 to 11.4 s per leg (measured 2026-11-24, [e2e.md](../developer/e2e.md#what-a-new-format-adds-to-the-tests)) | one or two packages of the leg's test upstream, bytes to kilobytes |
| Web unit and layout checks | R-CUR-018 | seconds; about 20 screens more in the layout walk | the dev profile's seed |

- What a leg gains: the unit job about 3 s; the integration shards about 40 s together; each format's e2e leg about 10 s; the UI layout
  checks about 60 s. With 25 formats the per-format part is about 4 s of integration and 10 s of its own leg each, and nothing in another
  format's leg.
- No case loads every format's client, and no case's cost grows with the number of formats beyond its own.

### R-CUR-023 · Formats that come later
Priority: **P1** · Covers: quader-only

A format that brings a remote class declares its catalogue (R-CUR-007) and its version canon (R-CUR-006) in the same change; nothing else
of this page changes for it. What four formats would declare, from the protocol research of 2026-11-23 (not specified, not built; the
owner's word of 08:09):

| Format | Search | Levels | Lookup and versions | Sizes | What a person sees |
|---|---|---|---|---|---|
| PyPI | `index`: a name search over the project list `GET /simple/` (JSON, about 10 MB with gzip, refreshed by `X-PyPI-Last-Serial`); PyPI has no search API (its XML-RPC search is gone, its web search answers a script challenge) | none: names are flat | `GET /simple/<name>/` (JSON): versions, files, `size`, `upload-time`, `yanked` | `known` | `python-gitlab` is found by its name and shows its 124 versions with sizes; a description search is not possible |
| Maven | `upstream`: Central's search service on its own host, declared by the preset | group IDs as path levels (directory listings of the repository) | `maven-metadata.xml` per artifact | `unknown` (a `HEAD` per file) | search by group and artifact; the group tree |
| Helm (classic) | `index`: `index.yaml` of the repository | none | the index's entries | `unknown` | search over chart names and descriptions |
| RubyGems | `upstream`: `api/v1/search.json` | none | `/info/<gem>` | `unknown` | search, versions |

## Changes to other pages

Made in the same change as this page, each named in a decision block below or on its page:

- [README.md](README.md): the page table; INV-11 (a remote fetches ahead also what a person starts through the management API and what
  a request starts for examination).
- [repositories-and-proxy.md](repositories-and-proxy.md): R-REPO-005 (a remote has no patterns), R-REPO-019 step 7 and the
  explanation's reasons (`not-allowed`), R-REPO-024 (`remote.delivery`, "nothing else is fetched ahead"), R-REPO-025 (step 5a), R-REPO-050
  and R-REPO-051 (`remote.mirror.include`), R-REPO-052 (the bound), R-REPO-061 (the parts `rules` and `deliveryMode`), R-REPO-065 (the
  row and the tab; `queue_full`; the metric), R-REPO-069 and R-REPO-070 (a well-known repository's rules are the administrator's; the
  system repository `scanner-trivy` is declared with a product rule instead of an include pattern), and notes in Q-REPO-193, Q-REPO-361,
  Q-REPO-363 and Q-REPO-365.
- [architecture.md](architecture.md): R-ARCH-010 (the contract version 11, the refusal of a remote format without a catalogue, kit checks
  21 and 22).
- [authorization.md](authorization.md): R-AUTHZ-006 (`manage` on a remote also sets delivery rules, decides requests and starts
  downloads ahead), R-AUTHZ-008 (a download ahead is an explicit act and populates as the system for the person).
- [api-and-ui.md](api-and-ui.md): R-UI-052 (tabs on a remote), R-API-081 (a stored version the rules do not deliver is listed and
  marked), R-API-015 (a download ahead is a queue row, not a job of its starter).
- [operations-and-observability.md](operations-and-observability.md): R-OPS-019 (the two tasks).
- [usage-and-limits.md](usage-and-limits.md): R-LIM-037 (a person's download ahead and a request's examination), Q-LIM-19 (a bulk is
  system work).
- [vulnerability-scanning.md](vulnerability-scanning.md): R-VULN-015 (a fill without a client is no refused download), Q-VULN-22 (its
  revisit).
- [signatures.md](signatures.md): R-SIG-069 (the fourth check and its place).
- [bastion.md](bastion.md): R-BAS-015 (the new journaled event).
- The format pages: R-NPM-037, R-NPM-049, R-NUGET-016, R-NUGET-035, R-OCI-017, R-OCI-038, R-DEB-036, R-ANS-017, R-ANS-047, and the
  sections "Mirror discovery" and the mirror settings (R-OCI-026, R-DEB-024, R-ANS-021), whose include patterns become
  `remote.mirror.include`.
- The security pages: [remote-curation.md](../security/remote-curation.md) (new), and sections of
  [remote-repositories.md](../security/remote-repositories.md), [egress.md](../security/egress.md),
  [repository-model.md](../security/repository-model.md), [delegated-administration.md](../security/delegated-administration.md),
  [data-and-secret-flow.md](../security/data-and-secret-flow.md) and [usage-and-limits.md](../security/usage-and-limits.md).

## Deferred

- A version range as a rule (Q-CUR-3).
- Conditions of a curation policy (a minimum age of a release, a licence, a severity), the rest of SEC-007 to SEC-011: a rule is a
  subject and an effect. The parked "minimum release age" idea of [FULL.md](../projektplan/FULL.md) stays parked.
- Pre-warming from a lock file or an SBOM (parked in FULL.md): a bulk names a package or a level, not a file.
- Rules on a virtual repository of its own.

## Security notes

The security view is [../security/remote-curation.md](../security/remote-curation.md): what a search tells the upstream's operator,
what the catalogue shows with an upstream credential, the second hosts of a preset, the bounds on untrusted answers, who can cause
volume, what a rule refuses and what it does not. Its gaps are recorded there.

## Decisions

Each question below was decided by the development agent from the listed options, except where the status line names the owner; the
owner reviews the development agent's choices at the walkthrough.

### Q-CUR-1 · A remote repository's page has tabs: its content, Upstream and Downloads

Status: **Decided by the owner on 2026-11-23** — his words: "In Repositories, die Remote sind, fehlt mir dafür ein Reiter um das Remote
zu durchsuchen … Hier will ich also einen Reiter." and "Remote-Repos benötigen auch einen Reiter mit dem der User sich die aktuelle
Download-Queue des Remote-repos anschauen kann". The details below are the development agent's.

Context: R-UI-052 says "No tabs", from Q-UI-321 (the development agent's, fix wave 2), which removed a second way to the client setup; the
settings page's tabs became sections by Q-UI-351 and the mirror's tab a section by Q-UI-344 (both the development agent's). The owner's
own Q-UI-650 made the views of one thing tabs with their own addresses.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A tab strip on a remote's page only: the content, **Upstream**, **Downloads**, each with its address (chosen) | R-UI-164's form | His word; views of one repository are tabs (Q-UI-650); a link opens a tab; local and virtual repositories keep their page | Two kinds of repository page |
| (b) The same strip on every repository, with one tab for local and virtual | Uniform | Same page shape | A tab bar of one tab |
| (c) Sections on the content page | No tabs | Smallest change | Not his word; the queue's refresh and the upstream's search under a long list |

**Choice:** (a). The content tab keeps its caption (Packages, Images); Set up client stays the header's (Q-UI-321 stands for that).

**Rationale:** the owner asked for tabs, and Q-UI-321's reason (one way to one job) is kept: every job of the new tabs has one way. The
development agent would revisit (b) if local repositories gain a second view.

**Applied in the spec:** R-CUR-018; [api-and-ui.md](api-and-ui.md) R-UI-052 (the sentence "No tabs" holds for local and virtual
repositories); Q-UI-321 (a note); ADR-277.

### Q-CUR-2 · The words: allow and block, two modes a plain user reads, no "release"

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: "release" is a gate's exception that only triage holders grant (R-SIG-070, R-MAL-021); the glossary renders exposure as
`freigeben` and a gate's release as `zulassen`; the owner wrote "Whitelisting", "Blacklisting", "freigeben" and "disabled".

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `allow` and `block`; modes `allExceptBlocked` and `onlyAllowed` ("Delivers everything except what is blocked", "Delivers only what is allowed"); German erlauben and sperren (chosen) | Plain verbs | A plain user reads the mode's sentence and knows; no word of another mechanism | "block" also names the vulnerability block, so the screens say whose block |
| (b) "Allow list" and "block list" | The industry's words | Familiar to administrators | Lists suggest one list each, while one rule set decides; less plain |
| (c) `release` and `disable` | His words translated | Literal | "Release" is taken; "disable" is a whole repository's switch (R-REPO-004) |

**Choice:** (a). The owner's "disabled" for the default mode's exceptions is `block`: one notion. A repository's `disabled` stays the
switch of a whole repository and is never used for a package.

**Rationale:** each word names one act. The development agent would revisit the German "sperren" if users confuse a rule's block with a
vulnerability block in practice.

**Applied in the spec:** R-CUR-001, R-CUR-018; the glossary rows (`web/src/locale/GLOSSARY.md`) with the screens' package.

### Q-CUR-3 · One rule model: the narrowest decides, a block wins at equal width, exact versions only

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Three kinds (section, package, version), the narrowest kind decides, block wins at equal width, exact versions (chosen) | R-CUR-001 | Every decision is explained by one rule; an exception to a blocked scope is a package's allow; NuGet's and Debian's version canons apply | A subset of a blocked section needs its packages named |
| (b) The most specific pattern decides among sections | Pattern ranking as claims rank | Finer sections | Two patterns' specificity is hard to read; claims rank by a rule nobody reads in a list |
| (c) Version ranges in each format's syntax | `>=4 <5` | Fewer rules | Three of five formats have no range evaluator (OCI tags, Debian, Ansible Galaxy have none in the contract; only npm's and NuGet's exposure adapters parse ranges); a range in a document and at an artifact must agree and a client's range resolution differs from Quader's |
| (d) The exposure rules' shape (R-EP-012): rules of several dimensions (package and version globs, a range, prereleases, tags), deny by default, adding up like grants | One matcher for both | Exists for npm and NuGet | A rule is a bundle a plain user cannot read; "exclude wins inside a rule and nowhere else", so "allow the package, block one version" needs both in one rule; no block across rules; no adapter for OCI, Debian and Ansible Galaxy |

**Choice:** (a). The section's pattern is matched by the one matcher of names that the exposure rules' package dimension, the grants and
the patterns use (Q-STOR-9); what is not taken from the exposure rules is their bundle of dimensions and their version globs and range.
**Rationale:** the owner asked for "all versions (and future ones)" or "certain versions": both are exact. The
development agent would offer ranges for npm and NuGet if allow lists grow to hundreds of versions per package in practice.

**Applied in the spec:** R-CUR-001, R-CUR-006.

**Corrected with the contracts package (2026-11-23).** R-CUR-006 named NuGet's canon "its normalized version". R-NUGET-003's normalized
form keeps the pre-release label's case, while two NuGet versions are one when their URL forms are equal (`1.0.0-Beta` and `1.0.0-beta`);
a rule compared on the normalized form would miss a request in the other case. The canon is the URL form, the identity of a NuGet version
([formats/nuget.md](formats/nuget.md) R-NUGET-035 changed with it).

### Q-CUR-4 · A remote's patterns become rules, and lose their hiding

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a remote's include and exclude patterns answer the question of the rules (what does the repository deliver), name-level only;
they hide a package from every view (`internal/packages/view/service.go`, `internal/vuln/report/report.go`) because it is not served.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A remote has no patterns; an exclude is a section block, an include list the mode `onlyAllowed` with section allows; what is stored and not delivered is shown as such (chosen) | One model on a remote | One answer to one question; a Maintainer can curate (the owner's deciders); examination needs visibility (D17) | Local and virtual repositories keep patterns, so the product has two notions, one per class |
| (b) Rules on every class | One model everywhere | One notion | Not asked; local patterns refuse deploys, a different question |
| (c) Patterns beside rules on a remote | No change | — | Two mechanisms for one question |

**Choice:** (a). Nothing of a remote must stay hidden from every view: what it stores came from its upstream by a decision of this
installation, and a reader of the repository may know what it holds and why it is refused.

**Rationale:** the development agent would revisit it if virtual repositories need curation of their own.

**Applied in the spec:** R-CUR-002; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-005 (a sentence for remotes).

### Q-CUR-5 · The mirror's scope is its own member, and the rules filter it

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: the include patterns were a mirror's scope and its bound (R-REPO-051, R-REPO-052), frozen for an Owner (Q-REPO-193); with the
patterns gone on a remote, the scope needs a home that a Maintainer's rules cannot widen without an administrator.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `remote.mirror.include`, a member of `remote.mirror` (write-config only); a pass stores what the scope and the rules admit (chosen) | R-CUR-017 | The mirror stays an installation's decision (Q-REPO-142); the rules stay a project's | One more list |
| (b) The rules are the scope; under a mirror only write-config changes rules | Q-REPO-193 extended to rules | No new member | A Maintainer of a mirrored remote cannot allow a package, which is the owner's case |
| (c) The rules are the scope without a freeze | Simplest | — | A Maintainer's `**` allow makes a mirror of a public registry unbounded |

**Choice:** (a). **Rationale:** what a mirror fetches and what a repository delivers are two decisions with two deciders. The development
agent would revisit (c) with Q-REPO-142's condition, per-project allowances.

**Applied in the spec:** R-CUR-017; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-050, R-REPO-051, R-REPO-052, Q-REPO-193 (a
note).

### Q-CUR-6 · The rule's refusal is a delivery gate: before the fill, at the serve point, in documents

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The shape is the
coordinator's D17 of 2026-11-23, from the research on examination.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A gate in the signature gate's shape: asked before a fill (no upstream request), as the fourth check of the one serve decision, and as a filter of documents (chosen) | R-CUR-003 | What is stored stays scanned and visible; the refusal has a reason; examination fits; one serve decision for every refusal of content | Three places to keep in step, held by kit check 23 |
| (b) In `Host.Authorize`, as the patterns are | Before the cache | One place | Hides from views; cannot tell "examined" from "missing"; a cached item's malware find is never said |
| (c) A per-version list joined into the item lookup, as the vulnerability block | One query | Cheap at the serve point | Covers stored versions only, and npm and NuGet only (the lookup's join is version-based) |

**Choice:** (a). **Rationale:** the only existing state "stored, scanned, visible, refused with a reason, lifted by a named person" is the
signature gate's. The development agent would revisit (c) if the fourth check costs a statement at the serve point (it reads the
snapshot only).

**Applied in the spec:** R-CUR-003; [signatures.md](signatures.md) R-SIG-069; [repositories-and-proxy.md](repositories-and-proxy.md)
R-REPO-025.

### Q-CUR-7 · The rule's place in the serve decision: after what was found in the content, before what is missing

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `malware_found`, `vulnerability_blocked`, `not_allowed_in_repository`, then the unverified and missing (chosen) | R-CUR-003 | A finding in the content holds whatever the rules say, so the client hears it first and nobody asks for malware; the rule comes before every pending check, so no client waits for a version it would not get | A not-allowed malicious version tells a client that it is malicious |
| (b) The rule first | The repository's choice above all | One reason for everything not allowed | A request for a version with malware found would be asked and declined |
| (c) The rule last | After the gates | — | A client waits for a scan of a version it would never get |

**Choice:** (a). **Rationale:** R-SIG-069's principle, what was found before what is missing, with the rule as the first thing that is
neither. The development agent would revisit it if the malware text for a not-allowed version is judged to tell too much.

**Applied in the spec:** R-CUR-003; [signatures.md](signatures.md) R-SIG-069.

### Q-CUR-8 · `403 not_allowed_in_repository`, a miss of a virtual member, and the answer when nothing serves

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `403` with its own code and a text with where to ask; a member's refusal continues the walk; when nothing serves, the rule refusal is the answer before `404` (chosen) | R-CUR-004 | A client sees why and where to ask; another member may deliver it under its own rules | The answer of a virtual repository names a member |
| (b) `404` as patterns answer | Indistinguishable | No new code | The person never learns that a request would help |
| (c) `403` that ends the walk, as the gates' refusals do | One rule for refusals | Simpler | A rule is no statement about the content; another member's delivery is legitimate |

**Choice:** (a). The member it names is one the caller may read, as `Quader-Resolved-From` names one (R-REPO-019).

**Rationale:** the development agent would revisit the end answer if a virtual repository's members should stay unnamed on an endpoint
with exposure rules (there the text names the virtual repository only, R-EP-018).

**Applied in the spec:** R-CUR-004; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-019.

### Q-CUR-9 · Debian: the upstream's signed indexes pass unchanged; the filtered view is a virtual repository

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a Debian remote passes the upstream's `InRelease` byte for byte and never signs (Q-DEB-13); the hashes of the indexes are in the
signed release, so a filtered `Packages` would fail every client's check. A virtual Debian repository merges its members' rows and signs
with its own key (R-DEB-025).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Addressed directly, the remote serves the upstream's indexes and refuses the files the rules refuse; through a virtual repository, its merge drops the rows the member's rules refuse (chosen) | R-DEB-036 | No new key; the curated view exists with the existing signer; apt's clients trust it as they trust any virtual repository | `apt install` of a refused package directly through the remote fails at the download with a status line, after the index listed it |
| (b) The remote builds and signs filtered indexes with a key of its own | A remote signs | Direct use is curated | A remote gains a signing key, its rotation and its setup: a second virtual repository |
| (c) Refuse mode `onlyAllowed` on a Debian remote | Not offered | Honest | The owner's concept missing for one format |

**Choice:** (a). The client setup of a Debian remote in mode `onlyAllowed` says that the curated view is the virtual repository over it.

**Rationale:** the development agent would revisit (b) if installations use Debian remotes directly in mode `onlyAllowed`.

**Applied in the spec:** R-CUR-003; [formats/debian.md](formats/debian.md) R-DEB-036.

### Q-CUR-10 · OCI: a tag is the version; content by digest follows the image and the stored tags

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A rule's version is a tag; a manifest by digest is delivered when the image is delivered without version rules, or a delivered tag's stored document names it directly or through a stored index; a blob by digest is delivered when some version of the image is (chosen) | R-OCI-038 | A pull by tag and by the digests a delivered tag names work; no new bookkeeping beyond `oci_references` | A blob of a refused tag that a delivered tag of the same image does not share can be fetched by its digest (H-1724) |
| (b) Rules name digests | Exact content | Precise | Nobody knows a digest when allowing; tags move |
| (c) Tags only, every digest refused under version rules | Strict | Closes the blob case | `docker pull` by tag reads manifests and blobs by digest: nothing would pull |

**Choice:** (a). A tag moves upstream; a rule on a tag delivers what the tag names when it is read.

**Rationale:** the development agent would revisit it if the gap of H-1724 matters; the signature gate made the same cut (H-1196).

**Applied in the spec:** [formats/oci.md](formats/oci.md) R-OCI-038.

### Q-CUR-11 · Who: the holders of `manage` and the administrator allow; the mode is configuration; triage stays separate

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The set of
deciders is the owner's words ("Ein Admin, Owner oder Maintainer").

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Rules and requests: `manage` or `admin`; the mode: configuration (write-config, Owner); gates' releases stay triage (chosen) | R-CUR-005 | His three deciders exactly; the mode, which changes everything at once, is the repository's configuration; a security exception stays a security role's | A Maintainer cannot switch the mode |
| (b) Everything configuration | Like the patterns | One right | Excludes Maintainers, against his words |
| (c) A Maintainer also releases gates for the repository | One act | Simple for a team | A gate's exception weakens a security check, which no project role may (R-MAL-021) |

**Choice:** (a). On a well-known repository of `upstream`, which has no members, the administrator. `repository-admin` holds no content
right (R-AUTHZ-012) and sets no rule.

**Rationale:** the development agent would revisit the mode's right if teams ask to switch their remotes themselves.

**Applied in the spec:** R-CUR-005; [authorization.md](authorization.md) R-AUTHZ-006.

### Q-CUR-12 · The catalogue is a declared part of the format contract; the contract version rises to 11

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every remote format declares its catalogue or a reason per call, as it declares its probes; version 11 (chosen) | R-CUR-007 | A future format cannot land without saying what its upstream can be browsed by; the screens read the declaration | Every format moves in one change |
| (b) An optional interface found by type assertion | No version | Smaller | A format without it shows nothing and says nothing (Q-ARCH-254's reason to make adapters' methods contract methods) |

**Choice:** (a). **Rationale:** the floor, a lookup by exact name, exists for every format, so the declaration is never empty. The
development agent would revisit it never before a format without names arrives.

**Applied in the spec:** R-CUR-007; [architecture.md](architecture.md) R-ARCH-010 (the contract version).

**Corrected with the contracts package (2026-11-23).** The version named here was 8, which the signatures took (ADR-228); the contract
stood at 10 (the receipt attestations) when the catalogue's declaration was built, so the catalogue's version is 11. Where the interface
lives and when `Register` checks the adapter is [Q-CUR-109](#q-cur-109--the-catalogues-browsing-calls-live-in-the-leaf-discover-and-registers-adapter-check-lands-with-the-adapters).

### Q-CUR-13 · The catalogue stores nothing in the cache

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The catalogue reads stored documents and fetches the rest without storing; its own short cache keeps parsed pages (chosen) | R-CUR-008 | A browser session never fills a cache by reading (R-AUTHZ-008 unchanged); the listing of R-API-081 stays "what was fetched" | A package document read twice within ten minutes by two replicas costs two requests |
| (b) Fill the remote's metadata as a client's request does | Reuse | Warm cache | A `GET` through the session cookie would populate, the case R-AUTHZ-008 forbids |

**Choice:** (a). **Rationale:** the development agent would revisit it if the catalogue's upstream requests cost an upstream's limits in
practice.

**Applied in the spec:** R-CUR-008.

### Q-CUR-14 · Who browses what: signed-in readers; in mode `onlyAllowed` a reader sees what is delivered and looks up by name

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Signed-in readers; in `onlyAllowed` search and levels show readers the delivered hits with the count of the others, a lookup by exact name shows any package (chosen) | R-CUR-009 | A reader finds what is allowed and can ask for anything named; with a credential on a private upstream, casual browsing of it stays with the deciders | A reader can still look up names one by one (H-1721) |
| (b) Every reader sees the whole upstream in both modes | Simple | — | Against the coordinator's D6; a private upstream's whole catalogue for every reader |
| (c) Anonymous callers too | Public remotes | Complete | An internet caller drives upstream searches through Quader |

**Choice:** (a). **Rationale:** the development agent would revisit (c) if public installations want a public catalogue.

**Applied in the spec:** R-CUR-005, R-CUR-009.

### Q-CUR-15 · Second hosts come from the preset, and Docker Hub's undocumented search is used with a fallback

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `catalogueHosts` of a preset; an undocumented endpoint only where no documented one answers the need, and its failure turns the capability off for the period (chosen) | R-CUR-008 | No user-typed host (no forgery of a second host); Docker Hub, the most used registry, is searchable today | An endpoint that disappears silently becomes "no search" |
| (b) Documented endpoints only | Docker Hub: namespace listing and lookup | Stable | No search on Docker Hub, the owner's most likely search |
| (c) A host typed per repository | Flexible | Any registry | A person names a host that receives requests and possibly a credential |

**Choice:** (a). **Rationale:** the development agent would revisit it when Docker Hub documents or removes its search.

**Applied in the spec:** R-CUR-008; [formats/oci.md](formats/oci.md) R-OCI-038.

### Q-CUR-16 · Debian: Quader searches the indexes it read, by words, as `apt search` does

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Words AND-ed, each a case-insensitive substring of the name, `Provides`, the short description and the long description of `Translation-en` where read; `Names only` (chosen) | R-DEB-036 | What `apt search` finds for words; no regular expression of a user runs on the server | `apt search`'s POSIX regular expressions are not offered |
| (b) POSIX regular expressions | Exactly apt | Complete | A user's regular expression in PostgreSQL over 60,000 rows per suite |
| (c) Names only | Cheap | — | Not what `apt search` does |

**Choice:** (a). **Rationale:** the development agent would revisit (b) with a bounded regular-expression engine if users ask for it.

**Applied in the spec:** [formats/debian.md](formats/debian.md) R-DEB-036.

**Corrected with the contracts package (2026-11-23).** R-DEB-036 named the table `deb_remote_descriptions (repo_id, dir, md5, text)` and
the columns `provides` and `description` of `deb_remote_entries`, which could not find a row's long description: a `Packages` record names
it by its `Description-md5`, and the rows "going with their index" need the index they came from. The entries keep `description_md5`, and
a description row names its `index_path`, whose foreign key to `deb_remote_indexes` removes it with its index; the search joins on
`(repo_id, dir, md5)`, which an index of its own serves.

### Q-CUR-17 · One version is charged to the person; a bulk is bounded system work by a project's decider

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-LIM-037 charges a probe's download to its caller; Q-LIM-19 records system work and limits nothing; Q-REPO-142 kept the mirror,
which decides on everything, an installation's decision.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One version and a request's examination charged to the person (R-LIM-037's shape); a bulk and an allow's download system work, previewed and bounded, by `manage` (chosen) | R-CUR-010 | What a person's client could cause costs the person; a bulk is one deliberate act with a preview and bounds, unlike a mirror's standing scope | A Maintainer can fetch up to 100 GiB per bulk without a ceiling |
| (b) Every download ahead charged to the person | One rule | Bounded by the person's limits | A Maintainer's cache-building would stop at a developer's hourly limits |
| (c) Bulks only by write-config | Q-REPO-142's reason | Volume stays an installation's | Not the owner's deciders |

**Choice:** (a). **Rationale:** the development agent would revisit it if bulks exhaust an upstream's quota, with a per-project allowance.

**Applied in the spec:** R-CUR-010; [usage-and-limits.md](usage-and-limits.md) R-LIM-037, Q-LIM-19.

### Q-CUR-18 · The queue is a table whose rows run as one-attempt jobs, and nothing is refused because others run

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `remote_download` rows own their state and retries; each attempt is a job `remote-download` with one attempt; a bounded number runs per repository, the rest waits (chosen) | R-CUR-011 | Listed per repository with an index; a failure in a script's words; a retry is a row update; `queue_full` only at the bound of waiting rows | A second table beside the jobs |
| (b) Jobs with a repository index and a structured error | The job table extended | One table | Every job kind pays for the members of one; the job API's visibility rules do not fit a repository's readers |
| (c) The mirror's `mirror_pending` | Exists | Has attempts | Transient by design, deleted when a pass ends |

**Choice:** (a). The metric of the background downloads gains the repository (`quader_remote_downloads_total{repo,origin,outcome}`,
R-CUR-013): Q-REPO-366 counted them without one because they had no list of their own; the queue is a repository's.

**Rationale:** the development agent would revisit (b) if a second kind of per-repository work needs a list.

**Applied in the spec:** R-CUR-011, R-CUR-013; [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-065, Q-REPO-361 and
Q-REPO-363 (notes).

### Q-CUR-19 · A client's failed fetch is a row of the queue, aggregated and bounded

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A row of origin `client` per repository and file, updated at most once a minute per replica, 1,000 per repository (chosen) | R-CUR-011 | "Why did my pull fail" is answered on the same tab, and its retry is a download ahead | A write on failures |
| (b) Not recorded | The circuit's last failure and the log | No write | The tab shows what persons started only |

**Choice:** (a). **Rationale:** the development agent would revisit the throttle if a failing upstream's writes show in the database's
load.

**Applied in the spec:** R-CUR-011.

### Q-CUR-20 · A failure is a class, a step, a code, the address without its query, the status and an excerpt

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The members of R-CUR-011, closed lists for `class` and `step`, the step from Go's transport errors (chosen) | Precise words | The owner's "technisch präzise"; a script filters by class | The engine's classification grows a step |
| (b) The job's text | As today | Nothing new | No address, no status, no filter |

**Choice:** (a). The query of an address is dropped because a CDN's signed address carries a credential (the mirror's rule, H-529).

**Rationale:** the development agent would revisit the excerpt's length if 512 bytes do not show a registry's error.

**Applied in the spec:** R-CUR-011.

### Q-CUR-21 · An examination stores whatever the rules say; the vulnerability miss gate still refuses what it knows

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The owner's words
of 13:01; the coordinator's D16, D18 and D19.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A download ahead ignores the rules; the miss gate refuses known-malicious and triage-blocked versions as for any fill; npm and NuGet show the vulnerability verdict before a fetch (chosen) | R-CUR-014 | The verdict of the miss gate is the examination's answer; known-malicious bytes do not enter the cache to be looked at | The malware scan and the signature check of such bytes are not made |
| (b) Exempt examination fills from the miss gate | Store everything | Every scan runs | A known-malicious artifact in the cache, one serve-time decision from delivery |

**Choice:** (a). **Rationale:** the development agent would revisit it if a decider needs a malware verdict of a version the advisories
already name malicious.

**Applied in the spec:** R-CUR-014.

### Q-CUR-22 · A fill without a client refused by the miss gate is no refused download

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: today `Fill` calls `GateMiss`, whose `Refuse` counts `quader_vuln_downloads_blocked_total` and writes
`vulnerability.download_blocked` for a mirror's or a background download's fill, with the system as actor (`internal/proxy/fill.go`,
`internal/vuln/enforce/enforce.go`).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Neither the count nor the event for a fill without a client; the fill's own record holds the refusal (chosen) | R-CUR-014 | "Downloads blocked" counts downloads | A mirror's refused fills leave the dashboard's count |
| (b) Keep both | As built | No change | A bulk of 1,000 known-malicious versions would read as 1,000 refused downloads |

**Choice:** (a). **Rationale:** the development agent would revisit it if the security dashboard needs the system's refused fills as a
figure of its own.

**Applied in the spec:** R-CUR-014; [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015.

### Q-CUR-23 · A request is one entry per subject, starts the examination at once, and an allow downloads what was asked

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. The owner's
words of 12:57 and 13:01 ("klingt auch gut").

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One entry per subject with askers and client refusals; examination of the version asked (of a package, its newest) charged to the asker, a switch per repository; an allow writes the rule and starts the download of what was asked, every version on a choice (chosen) | R-CUR-015 | The decider sees the verdicts beside the request; a request costs the asker, bounded per person | A package's request examines one version, not all |
| (b) No examination before an allow | His first words | No upstream work before a decision | The decider decides without verdicts |
| (c) Examine every version of a requested package | Complete | All verdicts | A request of a package with 3,893 versions |

**Choice:** (a). **Rationale:** the development agent would revisit the one version if deciders ask for more before deciding.

**Applied in the spec:** R-CUR-015.

### Q-CUR-24 · What was fetched for examination is discarded on a decision or after 30 days

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) **Discard** beside **Allow**, a decline discards, and a daily task removes what a download ahead stored without delivery once its examination row is older than `examinedRetentionDays` (30); journaled (chosen) | R-CUR-016 | Bounded storage; a restore does not bring back what was discarded; content a rule refuses after it was delivered stays, as the rules' refusal promised (R-CUR-003) | A row of its own per examined version |
| (b) Wait for R-STOR-045 | No new rule | — | Examined content stays for ever until then, and after it is removed by age, possibly before a decision |
| (c) Remove everything the rules do not deliver after the days | No bookkeeping | One rule | A block set for a while removes a cache that clients filled, against "what the cache holds and a rule now refuses stays stored" |

**Choice:** (a). **Rationale:** the development agent would revisit the default of 30 days with measured storage of examinations.

**Applied in the spec:** R-CUR-016; [storage.md](storage.md) R-STOR-045 (a sentence when it is built).

### Q-CUR-25 · Future versions: a rule allows them, the mirror keeps them fetched

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A package rule allows future versions; storing them ahead is the mirror's job, within its scope (chosen) | R-CUR-017 | One mirror | A team that wants one package kept fetched asks an administrator for a mirror scope |
| (b) A per-package "keep fetched" flag | A small mirror | Self-service | A second mirror with its own bounds, passes and failures |

**Choice:** (a). **Rationale:** the development agent would revisit (b) if per-project allowances arrive (Q-REPO-142's condition).

**Applied in the spec:** R-CUR-017.

### Q-CUR-26 · A stored version the rules do not deliver is listed, marked, with no install command

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough. R-API-081 is the
owner's decision of 2026-11-19 (W-24, "Option A": what was fetched is listed, every cell true); it stands.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Listed with `available: false` and the reason, no install command (chosen) | R-CUR-014 | Every cell is true: it was fetched, it is not delivered; the examination is visible | A reader sees what is being examined |
| (b) Hidden from readers | As patterns did | Quieter | Against D17; a reader cannot tell why a client is refused |

**Choice:** (a). **Rationale:** the development agent would revisit it if examinations must stay confidential to deciders.

**Applied in the spec:** R-CUR-014; [api-and-ui.md](api-and-ui.md) R-API-081.

### Q-CUR-27 · A download ahead is an explicit act of the management API, outside the session cookie's refusal

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-AUTHZ-008 refuses the populate check to the session cookie, because the cookie accompanies a link's navigation and an image's
load, which could make an administrator's browser bring a package into a frozen mirror; R-REPO-024 said "nothing else is fetched ahead"
beside a probe's download, and INV-11 named the probe and the mirror as the only reasons to fetch ahead. Both are the development
agent's sentences; the owner's words of 2026-11-23 ask for downloads started from the browser.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A `POST` of the management API, which needs the session's CSRF token or a token, run by a worker as the system for the person, who is asked for their own actions; the data path keeps the cookie's refusal (chosen) | R-CUR-010 | A navigation or an image cannot send it; the person's rights decide; the reason of the cookie rule stays whole | One more road into a cache |
| (b) Lift the cookie's refusal for the browser's data path | The UI fetches like a client | No new operation | Every link and image could fill a cache, the case the rule exists for |
| (c) Downloads only with a token | The UI asks for a token | The rule unchanged | The owner's "über den Browser" needs a token in the browser |

**Choice:** (a). INV-11 and R-REPO-024 name the new reasons to fetch ahead: a person's download through the management API and a
request's examination, each a row of the queue.

**Rationale:** the rule guards against acts a person did not mean; a `POST` with the CSRF token is an act a person meant. The
development agent would revisit it if a cross-site way to send the CSRF token were found.

**Applied in the spec:** R-CUR-010; [authorization.md](authorization.md) R-AUTHZ-008 (a bullet);
[repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-024; INV-11 in [README.md](README.md).

### Q-CUR-28 · The upstream is another surface: the protocol routes keep what they serve

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: R-NPM-037 (a remote's search never sends the query upstream), R-OCI-017 (the catalog lists cached names), R-ANS-017 (no browsing of
uncached content; "the mirror is the only thing that reads the index"), R-NUGET-016 (the forwarded search) and R-API-081 (the owner's: a
remote lists what was fetched) describe what a package client and the package list see.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The catalogue is a management API surface for a person; every protocol route and the package list keep their answers, filtered by the rules (chosen) | R-CUR-007, R-CUR-009 | A client's resolution never depends on an upstream's search; the owner's W-24 stands; the sentences change by one clause each | Two places show packages of a remote, the cached and the upstream's |
| (b) The protocol routes also answer from the upstream (npm's search forwarded, the catalog of the upstream) | One surface | A client could search | Docker Hub cannot list a namespace by the protocol (Q-API-343's reason); a client's search would spend the catalogue's budget |

**Choice:** (a). R-NPM-037, R-OCI-017 and R-ANS-017 gain the clause that the upstream is the catalogue's; R-NUGET-016 filters by the rules
as it filtered by the patterns; R-API-081 lists a stored version the rules refuse as not delivered.

**Rationale:** the development agent would revisit (b) for npm's search if clients ask for upstream hits through a remote.

**Applied in the spec:** [formats/npm.md](formats/npm.md) R-NPM-037, [formats/oci.md](formats/oci.md) R-OCI-017,
[formats/ansible.md](formats/ansible.md) R-ANS-017, [formats/nuget.md](formats/nuget.md) R-NUGET-016, [api-and-ui.md](api-and-ui.md) R-API-081.

### Q-CUR-109 · The catalogue's browsing calls live in the leaf discover, and Register's adapter check lands with the adapters

Status: **Decided by the development agent** — selected from the options below while building the contracts package; the owner reviews it
at the walkthrough.

Context: R-CUR-007 put the interface `Catalogue` and its types in `internal/formats/catalogue.go`. The catalogue's engine
(`internal/remotecuration/catalogue`) and the expansion of a bulk (`internal/remotecuration/queue`) call it, and both are domain packages,
which may not import the format contract, an edge package (R-ARCH-002). The mirror met the same wall and solved it with the leaf
`internal/mirror/discover` (ADR-152). Second, `Register` was to refuse a format whose adapter does not answer a declared call, while every
format declares the lookup and the five formats' catalogues are the catalogue's package (R3), not the contracts package.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The browsing calls (`CatalogueCaps`, `Search`, `Children`, `Package`) and their types in `discover`; `formats.Catalogue` embeds them and adds `VersionProbe`, whose `Probe` is a type of the contract; `Register` refuses a missing adapter from the change that brings the five catalogues (chosen) | ADR-152's shape | The domain packages call the catalogue as the mirror engine calls the lister; `VersionProbe` is called only by the queue's run in `internal/formathost`, an edge package; no format claims a call it does not answer | Two packages to read for one interface; the contracts package leaves one check of R-ARCH-010 to the catalogue's package |
| (b) Everything in `formats`, the engine in `internal/formathost` | No leaf | One place | The engine and the expansion leave the domain package the work packages give them, and the queue's expansion moves with them |
| (c) A port per call in the domain package, bound by the composition root | No leaf | Narrow ports | Every type twice, one copy per side (Q-ARCH-284's reason against it) |
| (d) Stubs of the five catalogues in the contracts package that answer `ErrNotOffered`, so the check holds at once | Full check now | R-ARCH-010 whole from the start | Five adapters that say they answer the lookup and answer nothing: the declaration would lie until R3 |

**Choice:** (a). **Rationale:** the leaf is the project's answer to a contract that a domain package calls; `VersionProbe` stays in the
contract because its result is the contract's `Probe`, and its only caller is an edge package. The development agent would revisit the
split if a domain package needs `VersionProbe`.

**Applied in the spec:** R-CUR-007 (the interface block); [architecture.md](architecture.md) R-ARCH-008 (the contract block) and R-ARCH-010
(the check of the adapter from the catalogue's package on); [../developer/remote-curation.md](../developer/remote-curation.md) (the files of
C and R3).

### Q-CUR-110 · The ports of the domain package speak in a repository's ID, and the decider reads no database

Status: **Decided by the development agent** — selected from the options below while building the contracts package; the owner reviews it
at the walkthrough.

Context: the developer page named the ports `rules.Decider` (`Decide(repo, name, version)`, `Package(repo, name)`), `queue.Lookup` and
`requests.Lookup` without saying what `repo` is. The repository model will hold the compiled rule set in its snapshot (R-CUR-006) and so
imports the domain package; a port that took the repository's type would make that import a cycle.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Ports at the root of `internal/remotecuration` in plain values: `Decider.Decide(ctx, repoID, name, version) Decision` and `Package(ctx, repoID, name)`, no error; `Queue.Active` and `Requests.Open` by repository ID, package and versions; stubs `DeliverAll`, `NoQueue`, `NoRequests` (chosen) | The rule of `internal/vuln` | No cycle whatever the repository model imports; the decision is the fourth check of the serve point and in every document, so it answers from the snapshot without a statement or an error (R-ARCH-027) | The decider looks the repository's rule set up by ID once more where the caller holds the snapshot |
| (b) Ports that take `*repository.Repository` | The snapshot in hand | No second lookup | The repository model cannot import the package that holds the compiled rule set |
| (c) Ports in the sub-packages (`rules.Decider`) | As the page named them | Each next to its builder | A consumer that needs the three imports three packages; the stubs need a home before their packages exist |

**Choice:** (a). **Rationale:** the domain package is the vocabulary every package of the topic shares (modes, kinds, effects, reasons,
the queue's states, origins, outcomes, failure classes and steps, the job, queue and task names), so its ports belong with it. The
development agent would revisit the second lookup if it shows at the serve point.

**Applied in the spec:** [../developer/remote-curation.md](../developer/remote-curation.md) (the contracts the packages build on).

### Q-CUR-111 · The purge of a retired repository removes its queue and examinations and keeps its rules and requests

Status: **Decided by the development agent** — selected from the options below while building the contracts package; the owner reviews it
at the walkthrough.

Context: a well-known repository the product no longer declares is retired: disabled, its row kept as a tombstone, its content purged by
`upstream_repository_purge`, which content goes and which configuration stays decided per table and held to the schema's cascading keys by
`TestR_REPO_067_ThePurgeOfARetiredRepository` (R-REPO-067, Q-REPO-376, Q-REPO-391). This page's tables with such a key are
`remote_delivery_rules`, `remote_delivery_requests`, `remote_download`, `remote_download_bulks` and `remote_examinations`;
`remote_delivery_request_askers` reaches a repository through a request, `deb_remote_descriptions` through an index.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The queue, its bulks and the examination rows are purged; the rules and the requests (with their askers) are kept (chosen) | Derived work goes with the content, people's decisions stay with the row | The purge's rule as it stands (content goes, configuration stays); a waiting row of a repository that serves nothing would only end `skipped`; an examination row names content the purge removes; a rule or a request is an act of a named person, kept as the claims and the exposure rules are | Kept rules and requests of a repository nobody can use |
| (b) Everything of this page is purged | Nothing remains | No rows of a dead repository | A person's decisions vanish without an event of their own; a repository declared again (R-REPO-067) would start without its curation |
| (c) Everything is kept | Nothing changes in the purge | Smallest change | Queue rows that point at removed content, examination rows of versions no longer stored |

**Choice:** (a). The Debian descriptions go with their index, which the purge removes, through their foreign key; the askers stay with their
request.

**Rationale:** the purge removes what the content implies and keeps what a person decided, the line R-REPO-067 already draws. The
development agent would revisit the requests if a retired repository's open requests are shown anywhere as waiting for a decision.

**Applied in the spec:** R-CUR-021 ("A repository that goes away"); [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-067
step 5 (a sentence); the function in [00114_remote_curation.sql](../../internal/meta/postgres/migrations/00114_remote_curation.sql).

### Q-CUR-40 · A refusal's count is summed on the replica and written every five seconds

Status: **Decided by the development agent** — selected from the options below while building the delivery rules (package R2); the
owner reviews it at the walkthrough.

Context: R-CUR-004 said the count of a client's refusals in the request list is "written with the event's throttle", once a minute per
repository, package, version and client address. Its acceptance criterion counts two refusals within a minute as 2, which a write once a
minute shows only after that minute.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The event once a minute as specified; the counts summed per subject in the replica's memory and written by one statement per subject every 5 s (chosen) | `internal/remotecuration/rules/store/record.go` | A refused client in a loop costs one write per subject per period, not per request; the list is at most 5 s behind; a test waits seconds, not a minute | A crash loses up to 5 s of counts ([H-1800](../security/remote-curation.md#h-1800--a-replica-loses-up-to-five-seconds-of-refusal-counts-at-a-crash)) |
| (b) A write per refusal | Exact | Simple | A refused CI loop writes per request |
| (c) The count written with the event, once a minute | As written | Fewest writes | The count lags a minute; the test waits a minute |

**Choice:** (a). **Rationale:** the count is a figure for a decider, not a record that must survive a crash; the bound of writes is the
point of the throttle. The development agent would revisit the period if the request list's writes show in the database's load.

**Applied in the spec:** R-CUR-004 ("Record").

### Q-CUR-41 · The vocabulary and the compiled rules are leaves; the contract hands a format the rules; the Host records a refusal

Status: **Decided by the development agent** — selected from the options below while building the delivery rules (package R2); the
owner reviews it at the walkthrough. It changes the layering the contracts package chose (R-ARCH-002, Q-CUR-110).

Context: a format filters its documents by the rules of the request's repository (R-CUR-003 point 3) and refuses a package whole at its
document, and a format may import only leaves besides the contract (R-ARCH-008). The contracts package registered `internal/remotecuration`
whole as a domain package; its root imports nothing of the module.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The root (`Mode`, `Decision`, the ports) and `internal/remotecuration/rules` (the compiled `Set`, the decision, the `Refusal`, the journal's `Narrowing`) are leaves; the store and the record of a refusal are `rules/store`, domain; the contract adds `Delivery`, `Undelivered`, `RefusalOf`, `DeliveryView`, `DocumentView`, `JoinViews`, `DocumentFingerprint`, `SystemRead`, `MemberDelivery`, `MemberRules`, `ModeReason` and the Host's service `RecordRefusal` (chosen) | A format reads `rq.Repo.Delivery()` through the contract | One decision for the core and the formats; a refusal is recorded once, in `ServeRoute`, wherever it was decided; additions keep contract version 11 (R-ARCH-025) | The layer table names five sub-packages of the topic one by one |
| (b) A port of the Host that answers per package and version | No new leaf | The formats call the Host | A document of thousands of versions calls the Host per version; the virtual merge needs a member's rules, which the Host does not name |
| (c) An optional interface of the Host found by a type assertion | No contract method | Small | R-ARCH-008 forbids a type assertion to an interface of the contract outside `CapabilitiesOf` |

**Choice:** (a). Also registered with it: the code `invalid_name` (422), which R-CUR-006 named and the contracts package had not
registered. **Rationale:** what every format needs to decide a document is a pure value; the machinery that writes stays domain. The
development agent would revisit (b) if a format needed rules that the snapshot cannot hold.

**Applied in the spec:** [architecture.md](architecture.md) R-ARCH-008 (the Host block, `MemberRules`, the paragraph of supporting
names); `internal/archtest/layers.go`, `internal/archtest/imports.go`.

### Q-CUR-42 · The places the rules decide, as built; kit check 23; OCI and Debian in the kit

Status: **Decided by the development agent** — selected from the options below while building the delivery rules (package R2); the
owner reviews it at the walkthrough.

Context: R-CUR-003 names three places; building them showed four details the page left open: where step 5a stands for a document, how a
system read and the documents an artifact's gate reads are treated, how OCI's tags list is filtered, and what kit check 22 (now 23) holds
for a Debian remote whose documents pass unchanged.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A document of a package (or a version) the rules do not deliver is refused before the cache lookup, stored or not; an artifact at step 5a for a miss and at the serve point when stored; the documents a format's gate reads for an artifact are not refused (the artifact decides); a system read is not refused at a document or the serve point, while a mirror's pass and the image database's fill admit only what the rules deliver; OCI filters its tags list itself on the cached name, `Host.PathsAllowed` joins no rule; the kit takes a Debian remote's `InRelease` as a `Passthrough` document (chosen) | `internal/formathost/remote.go`, `signature_gate.go`, `mirror_host.go`, `internal/formats/oci/delivery.go`, `internal/formats/formattest/checks_delivery.go` | One answer for a refused document, a stored infected file of a refused version still answers `malware_found` (Q-CUR-7), scanner-trivy still delivers only its database to the scanning's fill, a Docker Hub name's second spelling is not refused by a rule that names the first | A document refused while stored is never served stale |
| (b) Every refusal at step 5a after the cache lookup | As R-REPO-025 orders it | Literal | A stored document of a refused package would be served, and each format would refuse it again |
| (c) The rules joined into `Host.PathsAllowed` | One filter for every listing | Generic | OCI asks it for both spellings of a name: `library/nginx` allowed refuses `nginx`, so the pull answers 404 |

**Choice:** (a). The check of the delivery rules is kit check 23: 21 was taken by the package shapes (R-BAS-086) and 22 is the
catalogue's (R-ARCH-010).

**Built and held** (package R2, 2026-11-23):
- R-CUR-001: the decision (`internal/remotecuration/rules`, `TestR_CUR_001_TheDecisionTable`, `TestR_CUR_001_ThePackagesDecision`).
- R-CUR-002: no patterns on a remote (`TestR_CUR_002_ARemoteHasNoIncludeOrExcludePatterns`), a blocked package listed and marked
  (`TestR_CUR_002_ABlockedPackageStaysListedAndIsMarkedNotDelivered`), the search guard (`TestR_CUR_002_TheSearchGuard`, NuGet's forwarded
  search), both spellings of a Docker Hub name (`TestR_CUR_002_ABlockRuleHoldsForBothSpellingsOfADockerHubName`).
- R-CUR-003: before a fill, at the serve point and in the documents of the five formats (kit check 23; `TestR_CUR_003_…` in `npm`,
  `nuget`, `ociremote`, `debvirtual` (the Debian remote and its virtual repository), `ansibleremote`).
- R-CUR-004: the refusal's shapes (kit check 23), the virtual walk and the record (`TestR_CUR_004_AVirtualRepositoryWalksPastARefusingMember`,
  `TestR_CUR_004_TheRefusalsText`).
- R-CUR-005: the rules' operations (`TestR_CUR_005_WhoReadsWritesAndChecksTheRules`).
- R-CUR-006: the store, the limits, the revision, the propagation to a second replica, the journal's part `rules`
  (`TestR_CUR_006_TheRulesAreWrittenCheckedAndPropagated`), the part `deliveryMode` (`repository/journal_narrowed_test.go`).
- R-CUR-017: the mirror's scope and the rules (`TestR_CUR_017_TheMirrorAdmitsWhatItsScopeAndTheRulesAdmit`,
  `TestR_CUR_017_AMirrorStoresWhatItsScopeAndTheRulesBothAdmit`).

**Built with the landing** (the three items the first report left open):
- R-CUR-005: the rows of the project `upstream` on `upstream/npmjs` and the `409 managed_by_product` of a rule's write and removal on
  `upstream/scanner-trivy` (`TestR_CUR_005_TheRowsOfTheProjectUpstream`, area `repository`, on the replicas of the testenv harness, which
  wire the rules' API).
- R-CUR-006: the replay of the parts `rules` and `deliveryMode` after a restore, and the fail-closed disable of a rule set at its bound
  (`TestR_CUR_006_TheRulesAndTheModeNarrowedAfterARestorePointAreReplayed`, area `backup`).
- R-CUR-004: the refusal through a Bastion (`TestR_CUR_004_ARuleRefusalReachesTheClientThroughABastionAndIsNotKept`, unit,
  `internal/bastion/engine`); it found that a Bastion kept the refusal for its answer period, which Q-CUR-44 ends.

**Not built** (the plan takes them over): the screens of the rules (R5a, R5b) and the manual's sentences about them (R6). The forms of a
remote no longer offer the include and exclude patterns, and the mirror's form has **Mirror only these packages** (R-CUR-018, cut forward
from R5b).

**Rationale:** each detail keeps one answer per question. The development agent would revisit the treatment of a stored document of a
refused package if serving it stale were wanted.

**Applied in the spec:** R-CUR-003 (this block), [architecture.md](architecture.md) R-ARCH-010 (checks 21 to 23),
[formats/oci.md](formats/oci.md) R-OCI-038 (the tags list).

### Q-CUR-43 · A mirror without its bound does not run, and says so

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: with R-CUR-017 a mirror's scope moved from the repository's `includePatterns` to `remote.mirror.include`, and a remote's stored
patterns are no longer read (no migration, by the stage's rule). The bound of R-REPO-052 was checked only when a mirror was saved, and a
pass took `remote.mirror.include` with "empty means everything". A mirror of a public registry stored with patterns as its only bound would
therefore have stored everything the upstream offers at its next pass.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The dispatch and the pass apply the save's predicate (`repository.UnboundedMirror`): such a mirror is not dispatched, a pass of it ends `skipped: unbounded`, its status reads `unbounded`, its run answers 422 `mirror_unbounded` (chosen) | `internal/repository/validate_mirror.go`, `internal/mirror/engine.go`, `internal/mirror/pass.go`, `internal/api/v1/mirror_api.go` | One predicate for the save and the run, so what a save refuses never runs, whatever stored it; the owner sees why it stands and what fixes it; nothing is lost (a pass in progress keeps its pending names) | A stored mirror stops until an administrator gives it a scope or `maxBytes` |
| (b) A pass of an unbounded mirror stores nothing beyond a default byte bound | A hidden `maxBytes` | It keeps running | A bound nobody chose; the mirror fills up to it with what nobody asked for |
| (c) Copy the stored include patterns into `remote.mirror.include` on read | A migration path | Nothing stops | The stage forbids migration paths (pre-alpha, no installations to carry) |

**Choice:** (a). The state `unbounded` stands after `paused` and before `failing`: a paused mirror does not run either, and the pause is
the choice someone made. The alert `QuaderMirrorStale` fires for it after an hour, as for any mirror whose next pass is overdue; its text
names the cause. **Rationale:** the guard costs one lookup in the snapshot per dispatch and pass, and it is the only thing that protects
a stored mirror whose owner never reads the change. The development agent would revisit (b) if a mirror that stops were worse for an
installation than one that fills.

**Applied in the spec:** [repositories-and-proxy.md](repositories-and-proxy.md) R-REPO-052, R-REPO-056; R-CUR-017.

### Q-CUR-44 · A Bastion keeps no refusal of the rules: an allow reaches its clients at once

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a Bastion keeps the main installation's `403` as the caller's URL answer for the answer period (`bastion.answers.period`, 5 min by
default, R-BAS-020), and a feed entry ends it early: a token, principal or grants entry, or the `content` entry of a narrowing. The refusal
of the rules (`403 not_allowed_in_repository`, R-CUR-004) is lifted by an allow, which is a widening and journals nothing, so the test of a
refusal through a Bastion showed the kept refusal replayed after the allow for the rest of the period. The refusal already says
`Cache-Control: no-store`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A Bastion keeps no `403` that says `Cache-Control: no-store`; it relays it unchanged (chosen) | `internal/bastion/engine/forward.go` (`onPass`, `noStore`) | An allow reaches a Bastion's clients as it reaches the main installation's; the Bastion honours the directive the refusal carries; no new feed kind | A client that retries a refused version in a loop through a Bastion costs the main installation one request each time, bounded by the Bastion's guard and budget; the main installation decides it from its snapshot before any fill |
| (b) Keep it, as a refusal of a right is kept (the owner's rule that a widening of a right may take the period) | As before | Fewest requests | A person whose package a Maintainer just allowed is refused for up to 5 min (an hour at the most) behind a Bastion, and nowhere else |
| (c) Journal an allow as a feed entry that ends the URL answers of the repository | A widening in the revocation feed | Keeps the refusals | The feed carries revocations; a widening in it changes R-BAS-015's classification for every Bastion |

**Choice:** (a). **Rationale:** the owner's rule about a right's widening is about who may read; the rules decide what a repository
delivers to everyone who may, and the refusal marks itself as not to be stored. The development agent would revisit (b) if refused loops
through Bastions showed in the main installation's load.

**Applied in the spec:** R-CUR-004 ("A Bastion"); [bastion.md](bastion.md) R-BAS-020, R-BAS-022 (the check of a cached artifact).

### Q-CUR-45 · A remote's stored patterns are not read into its document either

Status: **Decided by the development agent** — selected from the options below; the owner reviews it at the walkthrough.

Context: a remote stored with include or exclude patterns (every remote saved before the rules, and `upstream/scanner-trivy`, whose
product pattern was `aquasec/trivy-db`) kept them in its document while the snapshot ignored them. An update starts from the stored
document, so every update of such a remote answered 422 `not_applicable` at `/includePatterns`, the screens offer no field to clear
them, and the reconcile of `quader migrate` that writes scanner-trivy's product rule failed with it: a replica of the profile of the
renders did not start (found while rendering this change).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The document of a remote is read without patterns, as the snapshot reads it (chosen) | `toDocument` in `internal/repository/store.go` | One reading of a remote everywhere; the next write stores the section without them; nothing is migrated | A stored list stays in the row until that write (it is read by nothing) |
| (b) The reconcile clears the patterns of a system repository | `setFixed` | Smallest | Every other stored remote still cannot be updated, through the API or the screens |
| (c) The validation lets unchanged stored patterns pass | A compare in the update | Keeps the stored lists visible | The document shows a member that does nothing and that no screen offers |

**Choice:** (a). **Rationale:** R-CUR-002 says a remote has no patterns; the document is where a reader and a writer meet that rule.
The development agent would revisit it if a remote's old lists were wanted as a record (the audit log has them).

**Applied in the spec:** R-CUR-002 (the paragraph on stored sections).

### Q-CUR-130 · Until the catalogue's screens land, the strip holds the content and Downloads, and a visitor sees none

Status: **Superseded by [Q-CUR-150](#q-cur-150--the-strip-holds-the-content-upstream-and-downloads-for-every-signed-in-reader-the-strip-counts-the-open-requests-itself)** with the tab Upstream (R5b); it was
decided by the development agent while building the tab strip and the tab Downloads (R5a).

Context: R-CUR-018 gives a remote's page three tabs, and the tab **Upstream** (search, levels, rules, requests) is R5b's, which needs the
catalogue (R3) and the rules' screens. The specification says how **Upstream** looks once built, not before. The queue's API answers a
signed-in reader only (`authenticated: true`, R-CUR-005); a public remote is read by visitors without a session.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The strip holds the content and **Downloads** until R5b adds **Upstream**; a visitor without a session sees no strip (chosen) | Two tabs now, three later | Every tab leads to a screen that does its job; no label promises what is not there; one tab is no strip, as on the page System status (Q-UI-652) | The strip changes when R5b lands |
| (b) **Upstream** as a tab now, with a sentence that the upstream's search comes later | Three tabs now | The final shape at once | A tab that does nothing: a label that promises more than the thing does (the style guide's rule of text) |
| (c) The strip for a visitor too, with **Downloads** answering 401 | One shape for everyone | Uniform | A tab that leads a visitor to a refusal |

**Choice:** (a). **Rationale:** the specification has no placeholder state, and a screen that only says "later" is the kind of element
that takes room without being the screen's job. The development agent would revisit (c) if the queue becomes readable without a session.

**Applied in the spec:** R-CUR-018 (the tab strip's paragraph).

### Q-CUR-131 · The tab's filters are the states as pressed buttons with their counts, and Failure as a select, in the address

Status: **Decided by the development agent** — selected from the options below while building the tab Downloads (R5a); the owner reviews
it at the walkthrough.

Context: R-CUR-018 asks for "the counts by state as filters" and "the filter **Failure** by class". The head of other lists is
`<quader-list-head>` with a drawer of filters and chips (R-UI-100); the usage screens choose their period with pressed buttons in the
selection's fill (R-UI-183). The API's filter of a class is the query parameter `class`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) **All**, **Waiting**, **Running**, **Failed**, **Done** as pressed buttons with their counts as badges; **Failure** a select beside them; the address `state`, `failure`, `bulk` (chosen) | The specification's drawing | The counts are what a person following a cache's build looks at, and each is one click; the control is one the app has (the period of the usage screens); a link opens the same view | A head that differs from the lists with a drawer |
| (b) The list head with a drawer holding the state and the class, the counts in a line above | The head of every list | One head for every list | The counts and the filter apart; a filter of five values behind a drawer |
| (c) The states as a second `tab-nav` under the repository's strip | The reference's state tabs of an issue list | Familiar from the reference | Two tab strips one over the other read as one navigation of two levels |

**Choice:** (a). Cancelled rows, which the counts do not hold, are under **All**. The address names the filter by its label, `failure`,
and the screen sends it as the API's `class`. **Rationale:** the development agent would revisit (b) if the tab gains more filters
(origin, a package's name) than one row holds.

**Applied in the spec:** R-CUR-018 (the tab Downloads, "As built").

### Q-CUR-132 · A row offers Retry or Cancel by its state to whom the API lets change it, and a bulk's line stands over its rows

Status: **Decided by the development agent** — selected from the options below while building the tab Downloads (R5a); the owner reviews
it at the walkthrough.

Context: R-CUR-005 lets the person who started a row, the holders of `manage` and the administrator retry or cancel it, and a row a probe
or a client caused whoever may fill that version; the server decides each call. A row has no member that says what the caller may do,
and the API lists no bulks, only one bulk by its ID. R-UI-163 gives a row with one action its word, a row with several a menu.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One word per row by its state (**Retry** for failed and cancelled, **Cancel** for waiting and running), offered to the row's starter, to a holder of `manage` or the role `admin`, and for a probe's or a client's row that the rules deliver to a caller whose check of `read` and of the repository's `cacheFillAction` passes (a reader's view, which lacks it, asks `read`, the default); the opened row shows the failure without a second **Retry**; a bulk's line only where the address names the bulk (chosen) | The row's one action | One way to each job (the action cell); no action shown that the server refuses in the normal case; a bulk is reached from its rows | The check of the fill is the repository's, not the version's: a grant on some packages only offers **Retry** that the server may refuse, with its problem in the notice |
| (b) **Retry** and **Cancel** to every reader, the server's refusal in a notice | Simple | No check | An action offered that the server refuses for most readers |
| (c) A list of the repository's bulks above the rows | Every bulk at a glance | A bulk without its rows | An API of its own (a list of bulks) that R-CUR-012 does not have |

**Choice:** (a). **Rationale:** R-CUR-018's "and **Retry**" in the opened row and the action cell are one job; the action cell keeps it
where every other list has it. The development agent would revisit the check of the fill if the API answers each row's actions, and (c)
if bulks become many per repository.

**Applied in the spec:** R-CUR-018 (the tab Downloads, "As built").

### Q-CUR-133 · The tab reads the queue again while the repository has a row that waits or runs, keeping what is shown

Status: **Decided by the development agent** — selected from the options below while building the tab Downloads (R5a); the owner reviews
it at the walkthrough.

Context: R-CUR-018 refreshes the tab every 5 s "while something waits or runs", with **Pause automatic refresh**. The jobs list starts its
list again at every refresh (`PagedResource.start`), which drops what **Load more** added and closes nothing because it opens nothing.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The timer runs while the answer's counts hold a waiting or running row of the repository, or the bulk shown expands or waits; a refresh reads the first page again with as many rows as are shown (at least 50, at most 500) and replaces them; a filter starts over (chosen) | The repository's state decides | A filter of done rows still follows a build that goes on; the rows opened and loaded stay; no list empties between two reads | A page of 500 rows every 5 s for whoever loaded that many |
| (b) The timer runs while a row shown waits or runs | The rows shown decide | Quiet under a filter of finished rows | The counts go stale while the cache is built |
| (c) Restart the list at each refresh, as the jobs list | The existing pattern | One pattern | The opened failure closes under the reader every 5 s |

**Choice:** (a). The pause is the tab's own, a pressed button at the head's end; it lasts while the tab is open. **Rationale:** the
development agent would revisit the cap if a refresh of 500 rows shows in the API's figures.

**Applied in the spec:** R-CUR-018 (the tab Downloads, "As built").

### Q-CUR-134 · A failure's class and step stand in words, with their value in a muted line of code below

Status: **Decided by the development agent on 2026-11-24** — selected from the options below; the owner reviews it at the walkthrough.

Context: the opened row of the tab Downloads showed the class and the step of a failure each in words with its value beside them,
"Step  The answer's status `status`" and "Class  Upstream error `upstream_error`": where the words and the value share a word, the line
reads as one phrase said twice. The value stays, since it is what a script filters on (Q-CUR-20) and what the filter **Failure** puts
into the address.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The words, and below them the value as code in the muted second line the shared table gives a technical name (`.secondary`), for every failure (chosen) | One place for the value in every row | No stutter: the eye reads the words and finds the value under them; the value is where it was in every failure; no new style | One line more per fact |
| (b) The value only where it differs from the words | A judgement per value | Shorter where they agree | "Differs" has no rule a reader can see (`upstream_error` against "Upstream error", `status` against "The answer's status"); the value a script needs is missing in some rows and present in others, and German words always differ |
| (c) The value alone | The code | Shortest | A reader who does not script reads `upstream_rate_limited` instead of a sentence |

**Choice:** (a). The step and the class keep their words first; the code (`Code`) and the file, the address and the status keep their
own rows. **Rationale:** the two readers of the details, a person and a script's author, each find their part at one place; the
development agent would revisit it if a reader asks for the values to be hidden behind a toggle.

**Applied in the spec:** R-CUR-018 (the tab Downloads, "As built"). Code: [remote-downloads.html](../../web/src/app/features/repositories/downloads/remote-downloads.html),
[remote-downloads.spec.ts](../../web/src/app/features/repositories/downloads/remote-downloads.spec.ts).

### Q-CUR-150 · The strip holds the content, Upstream and Downloads for every signed-in reader; the strip counts the open requests itself

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building the tab Upstream (R5b); the
owner reviews it at the walkthrough. It replaces Q-CUR-130.

Context: R-CUR-018 gives the label **Upstream** the count of open requests for those who may set rules. The API counts them in the
capabilities (`GET …/upstream`, `openRequests`), which asks the format's catalogue and can reach the upstream (once per cache period) and
fails while the upstream is down. The list of requests (`GET …/delivery-requests?state=open`) reads Quader's own tables and answers no
total.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The tab Upstream passes the capabilities' count; on the other tabs the strip reads the open entries once (up to 100, "100+" beyond) for a holder of `manage` or the role `admin`, and nothing for anyone else (chosen) | Two readers of one count | The content's page asks no upstream; the count is right where it is shown first | A page of up to 100 entries for a count |
| (b) The strip reads the capabilities on every tab | One reader | One number | Every view of a remote's content asks its catalogue, and its label loses the count while the upstream is down |
| (c) A count of open requests in the repository's document | Exact and cheap | — | An API change for a label |

**Choice:** (a). A visitor without a session sees no strip, as before. **Rationale:** the development agent would revisit (c) if the
entries of a repository exceed a page often.

**Applied in the spec:** R-CUR-018 (the tab strip), [api-and-ui.md](api-and-ui.md) R-UI-052. Code:
[repository-tabs.ts](../../web/src/app/features/repositories/content/repository-tabs.ts) (named remote-tabs.ts until Q-UI-1067).

### Q-CUR-151 · One field searches and looks up; the address holds the query; the levels are a breadcrumb and links

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building the tab Upstream (R5b); the
owner reviews it at the walkthrough.

Context: R-CUR-018 wants a search where the upstream answers one, the lookup by exact name where only that works (said plainly), and in
mode `onlyAllowed` the lookup for a reader whose search withholds hits. R-CUR-004's refusal links `…/upstream?name=<name>`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One field with **Search** and **Look up by exact name**; where the upstream has no search, **Look up** alone and the upstream's sentence under the field; `q`, `name`, `level` and `namesOnly` in the address; a package looked up stands above the list under its name with **Close** (chosen) | One place to type | The refusal's link and a shared link open the same view; the reader who is told to look a package up does it in the field they used | Two buttons beside a field |
| (b) A field per act | Separate | Each labelled for itself | Two fields for one name |
| (c) The lookup as a link under each empty search | Implicit | Fewer controls | A reader in `onlyAllowed` has to search first to find the lookup |

The levels: the breadcrumb of the level (from **Upstream**) and its children as links, so a suite of Debian and its components are
reached by following them; a root that lists nothing (an npm scope, a Docker Hub namespace, Q-CUR-63) offers a field named by the kind of
the level to enter one by its name, and then no empty list. A level that lists packages offers **Download this sub-section…** to those who
decide. A sub-section's rule is not offered at a level: a level's name is a name pattern in some formats only (a Debian suite is not), so
sub-section rules are written in the rule list (Q-CUR-153). **Read the index** waits for Q-CUR-62.

**Choice:** (a). **Rationale:** the development agent would revisit (b) if a format's lookup takes more than a name.

**Applied in the spec:** R-CUR-018 (the tab Upstream, "As built (R5b)"). Code:
[remote-upstream.ts](../../web/src/app/features/repositories/upstream/remote-upstream.ts).

### Q-CUR-152 · A version's acts are a word for a reader and a menu for those who decide; the rule is a column, the verdicts a word or a link

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building the tab Upstream (R5b); the
owner reviews it at the walkthrough.

Context: R-CUR-018's drawing shows a version's acts as buttons side by side ([Allow][Discard]). R-UI-163 gives a row with one action its
word and a row with several a menu, and the layout checks refuse a table whose action cells are built two ways. Those who decide have
**Download** or **Download for examination**, **Allow**, **Block**, **Remove rule** and **Discard**; a reader **Download** (a version the
rules deliver, where they may fill the repository) or **Request**. The rule that decided and the verdicts are facts of the version; a
stored version's verdicts are its version page's (Q-CUR-61), an unstored one's vulnerability verdict is `upstream/vulnerabilities`'.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A reader's row has at most one act, its word; the row of those who decide is the menu **Actions for &lt;name&gt; &lt;version&gt;** with **Allow** and **Block** always (the effect the version's own rule has is **Remove rule** in its place), so every row has two acts or more; the rule that decided is a column **Rule**; an unstored version's verdict is a word (**None known**, **Known as malicious**, **Blocked in triage**), a stored one's the link **Open the version** to its page; a format without verdicts says so once at the package (chosen) | R-UI-163 in every row | One construction per table; an explicit allow of a delivered version is meaningful (it outlasts a narrower block and a change of the mode, R-CUR-001) | A menu where the drawing has buttons |
| (b) The drawing's buttons | R-CUR-018's drawing | Every act one click away | Two to four words per row, against R-UI-163 and the layout checks |
| (c) The rule under the state's badge | One column fewer | Narrower | The cell's first line is no longer the badge's (the layout check `row-offset`, R-UI-172) |

**Choice:** (a). The package's acts stand as buttons in the opened package's head, which is no row. **Rationale:** the development agent
would revisit the verdicts' link (Q-CUR-61 (b)) if a decider needs the badges in the list itself. **Revised on 2026-11-24**
([Q-CUR-157](#q-cur-157--a-packages-versions-are-three-columns-the-facts-under-the-version-the-rule-under-the-delivery-the-verdicts-under-the-cache)):
the rule that decided stands under the delivery's badge and the verdicts under the cache's state, not in columns of their own, so
that the list is a table at 1,024 px; the badge stays its cell's first line, which was (c)'s objection.

**Applied in the spec:** R-CUR-018 (the tab Upstream, "As built (R5b)"). Code:
[upstream-package.ts](../../web/src/app/features/repositories/upstream/upstream-package.ts).

### Q-CUR-153 · The requests panel answers by a row's menu and dialogs; the rule list is a dialog that adds sub-sections

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building the tab Upstream (R5b); the
owner reviews it at the walkthrough.

Context: R-CUR-018 sets the requests apart with **Allow** and **Decline**; R-CUR-015's allow takes `as` and `downloadEveryVersion`, a decline
a reason and `discard`. The rule list is "a dialog or a page section". The examined versions' verdicts are read where every stored
version's are (Q-CUR-146).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A bordered panel **Requests** with the count; each entry its subject, who asked (with the askers' notes) or how often clients were refused, the time, the examination (its row's state, **Examined 39.2.0** with **Verdicts** to the version page, or why it did not start); **Allow…** and **Decline…** in the entry's menu, each a dialog with its choices; a reader's **Your requests** with the state, a decline's reason and **Withdraw**; **Rules** opens the rule list as a dialog that adds a sub-section's rule (pattern and effect) and removes any, read only for `repositories:read-config` (chosen) | The acts with their choices | The allow's two choices and the decline's reason are asked where they are made; the list's package and version rules are set where the package is shown, with its versions in view | Two clicks to an allow |
| (b) **Allow** and **Decline** as words on the entry, the choices in a later edit | Faster | One click | Two words in a list item (R-UI-163); a decline without its reason |
| (c) The rule list as a section of the settings page | With the mode | One place for the configuration | A Maintainer sets rules and does not read the settings page (R-CUR-005) |

**Choice:** (a). **Rationale:** the development agent would revisit (b) for the allow if deciders answer many entries in a row.

**Applied in the spec:** R-CUR-018 (the requests, the rule list). Code:
[delivery-requests-panel.ts](../../web/src/app/features/repositories/upstream/delivery-requests-panel.ts),
[request-dialogs.ts](../../web/src/app/features/repositories/upstream/request-dialogs.ts),
[delivery-rules-dialog.ts](../../web/src/app/features/repositories/upstream/delivery-rules-dialog.ts).

### Q-CUR-154 · The section Delivery saves `remote.delivery` alone and leads to the rules on the tab Upstream

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building the settings' section (R5b);
the owner reviews it at the walkthrough.

Context: R-CUR-018 puts the mode, **Fetch for examination when someone asks** and the days of the retention into a closed section
**Delivery** of the settings page; the mode is configuration (`repositories:write-config` or the project's Owner, R-CUR-005), the rules
are `manage`'s. The page's other sections save through the form's model (`settings-sections.ts`), which other builders change.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A component of its own that patches `remote.delivery` with the page's `ETag`, after **Mirror** on every remote but a system repository, opened by the address's `#delivery` (the tab's **Change**), with a link to the tab Upstream for the rules (chosen) | As **Download links** | The section's save touches one member; a remote's other sections are unchanged | A second save path beside the form's sections |
| (b) A section of the form's model | As **Mirror** | One model | A change of shared files for three values |

**Choice:** (a). **Rationale:** the development agent would revisit (b) when the form's model moves every section to one pattern.

**Applied in the spec:** R-CUR-018 (the settings page). Code:
[repository-delivery.ts](../../web/src/app/features/repositories/repository-delivery.ts).

### Q-CUR-155 · The catalogue's own states are sentences of the screen; the upstream's own sentences stand as the upstream wrote them

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building the tab Upstream (R5b); the
owner reviews it at the walkthrough.

Context: R-CUR-018 names the sentences of the upstream down ("until 10:42"), the budget used up ("try again in 40 s"), a document too
large and a format without search. The API answers `503 upstream_assumed_offline`, `429 catalogue_budget_exhausted` and
`502 catalogue_answer_too_large` with `Retry-After`; `searchNote`, `searchReason`, `levelsReason` and a page's `note` are English
sentences the server writes per upstream.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The three refusals, the offline mode and a call not offered as the screen's own translated sentences in a warning line, the time computed from `Retry-After`; the server's sentences shown as written; the catalogue's reads never repeated by the client (chosen) | The screen speaks | A state reads as a state, not as an error with **Retry**; a budget is not spent by an automatic retry | The upstream's sentences stay English in German |
| (b) Every failure in the shared error notice | One pattern | Nothing new | "upstream_assumed_offline" with **Retry** says less than the specification's sentence |

**Choice:** (a). The sentence of the budget says "the upstream catalogue's budget": the API's refusal does not say whether the
repository's or the person's ran out. **Rationale:** the development agent would revisit the server's sentences if the API answers them
as codes. Revisited for the sentence under the search field by [Q-CUR-166](#q-cur-166--the-hint-under-the-upstream-search-is-a-code-the-screen-says-in-its-language): `searchNote` and `searchReason` became the code
`searchHint`, which the screen says in its language.

**Applied in the spec:** R-CUR-018 (the states). Code:
[upstream-words.ts](../../web/src/app/features/repositories/upstream/upstream-words.ts).

### Q-CUR-156 · An OCI remote's stored tags are the tags its package screens list

Status: **Decided by the development agent on 2026-11-24** — selected from the options below when R5b's renders showed the tags an OCI
remote had pulled as not stored; the owner reviews it at the walkthrough.

Context: `stored` (R-CUR-009, Q-CUR-61) was read from the live rows of `package_versions`. An OCI remote writes none: a tag it holds is
its cache entry `<name>/_tags/<tag>` (R-OCI-036, Q-OCI-121), listed by the view `oci_remote_tags` when a client fetched it, a download
ahead stored its image or a mirror holds the repository. So every tag of an OCI remote answered `none`, the image `none`, and the preview
of a download ahead counted no tag as stored. npm, NuGet, Debian and Ansible Galaxy remotes write version rows at a fill and were not
affected.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The statement adds an OCI remote's tag entries, each kept by the view's rules written as checks per entry from the page's packages, as the version list's `PackageViewTagPageDesc` does (chosen) | One statement, two branches | The catalogue's `stored` is what the package screens list and open; measured 1.4 ms for three names among 400,000 tag entries, 28 ms for 200 names of 40 tags each | The view's rules stand in a third place |
| (b) The statement reads `package_version_listing` | The view as it is | The rules in one place | The planner reads every tag entry of every OCI remote before it joins the names: 1.1 s for three names, measured the same day |
| (c) An OCI remote writes version rows for its tags | As the other formats | One branch | A tag moves and expires as a cache entry; a second record of it to keep in step, against Q-OCI-121 |

**Choice:** (a). **Rationale:** the screen opens a stored version's page (Q-CUR-61), so what the catalogue calls stored must be what the
version list holds. The development agent would revisit (b) if the view can be read from a package's side without reading every tag
entry.

**Applied in the spec:** R-CUR-009 (as built). Code:
[upstream_catalogue.sql](../../internal/meta/postgres/queries/upstream_catalogue.sql) `CatalogueStoredVersions`. Test:
`TestR_CUR_009_TheCatalogueSaysStoredOfATagARemotePulledThrough` (area `ociremote`).

### Q-CUR-157 · A package's versions are three columns: the facts under the version, the rule under the delivery, the verdicts under the cache

Status: **Decided by the development agent on 2026-11-24** — selected from the options below when R5b's renders showed a package's
versions as blocks at 1,024 px; the owner reviews it at the walkthrough.

Context: the list of a package's versions on the tab Upstream had seven columns and the acts (**Version**, **Published**, **Size**,
**Delivery**, **Rule**, **Cache**, **Verdicts**, Q-CUR-152). Their least widths with a cell's padding of 16 px on each side need more
than the list has at 1,024 px beside the side bar (about 690 px, measured in the renders), so the shared table stacked its rows
(R-UI-174): five lines a version, 24 versions over several screens, on the list a person scrolls most on the tab. Four columns and the
acts (the verdicts still a column) were measured the same day: they still stacked for a reader, whose act is the word **Request** or
**Download** and not a menu, and they wrapped **Open the version** beside a column with room to spare (`cramped`). At 390 px a list
may stack.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) **Version** (the version, its flags, under it the published time and the size, "Size not told" where the upstream does not tell it, and an image's platforms), **Delivery** (the badge, under it the rule that decided: "Rule: this version", "Rule: the package", "Rule:" and a sub-section's pattern), **Cache** (the state, under it **Open the version** for a stored version or the vulnerability verdict of one that is not stored, "No known vulnerability" where none is known) and the acts (chosen) | Three columns | The list is a table from 1,024 px on for every kind of user; a row's first line is the version, its state and its cache; nothing moves into a menu | Published, size and verdict have no column of their own to scan down |
| (b) The verdicts and the rule behind the row's menu | Three columns | As narrow | A reader has no menu (Q-CUR-152), and a verdict is a fact, not an act |
| (c) Published and size hidden below 64 rem of the list's width (`narrow` columns) | Seven columns at 1,440 px | Nothing changes where there is room | Published and size are gone at 1,024 px, and the rule and the verdict still need their room |

**Choice:** (a). The verdict "None known" became "No known vulnerability": under the cache's state it no longer has a column's name that
says what is known. The layout check `row-offset` measured a cell whose badge has a line of text under it on that text, 16 px under the
row's line; a control on a line of its own above the cell's text is now the cell's first line (the fixture in
[layout-checks.spec.ts](../../web/e2e/renders/layout-checks.spec.ts)), which was Q-CUR-152 (c)'s objection. **Rationale:** the facts a
person reads per version stay in the row, and the render run holds the form at 1,024 px with the screens' `tables` (`stacked` finding).
The development agent would revisit (c) if a person asks to sort or scan by the published time.

**Applied in the spec:** R-CUR-018 (the tab Upstream, "As built (R5b)"); Q-CUR-152 (the rule's and the verdicts' columns). Code:
[upstream-package.ts](../../web/src/app/features/repositories/upstream/upstream-package.ts),
[upstream-package.html](../../web/src/app/features/repositories/upstream/upstream-package.html),
[upstream-words.ts](../../web/src/app/features/repositories/upstream/upstream-words.ts) `verdictText`; the check in
[layout.ts](../../web/e2e/renders/layout.ts) (`row-offset`); the screens' [upstream.ts](../../web/e2e/renders/upstream.ts)
`upstreamVersions`.

### Q-CUR-160 · An OCI referrers tag is no version: it follows its subject

Status: **Decided by the development agent on 2026-11-24** — selected from the options below when the end-to-end case of the leg `oci`
(R6) found it; the owner reviews it at the walkthrough.

Context: with the tag `3` of an image allowed in mode `onlyAllowed`, `docker pull` (Docker 29 with the containerd image store) fetched the
index, the platform manifest and its layers, then asked the referrers API for the platform manifest's digest; the upstream answered 404,
Docker fell back to the referrers tag schema of the OCI distribution specification (`GET …/manifests/sha256-<hex of the subject>`), the
rules refused that tag as a version nobody had allowed, and Docker failed the pull with the rules' text. Every OCI remote with version
rules whose upstream answers the referrers API with 404 (the test upstream does; which public registries do was not read) could not
pull an allowed tag with Docker. The same case found a second defect on the way: the decision of a manifest by digest
read the stored tags with no transaction and answered 500 (`TagTargets`, now read in a short transaction of its own).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A referrers tag is no version (`Classify` gives none, as for a manifest by digest); the tag's handler decides it by its subject's digest with the rule of R-OCI-038 (chosen) | The attachments' index follows the manifest it describes | Docker pulls an allowed tag; the referrers of a refused manifest answer the refusal, as R-OCI-038 says of the referrers API | A version pattern of an endpoint's exposure or a permission now decides such a tag by its image, as it decides a manifest by digest |
| (b) Answer every referrers tag of a remote with rules 404 | Docker goes on without attachments | Simple | Hides the attachments of an allowed image (its signatures, which the signature gate reads) |
| (c) Leave it a version; the operator allows `sha256-*` | No code | Nothing changes | A rule that names no image's version; every operator meets the failed pull first |

**Choice:** (a). **Rationale:** a referrers tag names a digest, not a release; the rules' unit is the tag a person allows. The
development agent would revisit it if a registry's clients ask for the tags of cosign's older layout (`sha256-<hex>.sig`, `.att`) under
rules: those stay versions here, since no client of the legs asks for them on a pull.

**Applied in the spec:** [formats/oci.md](formats/oci.md) R-OCI-038. Code: `Classify` in
[paths.go](../../internal/formats/oci/paths.go), `remoteManifestGet` in
[remote_handlers.go](../../internal/formats/oci/remote_handlers.go), `TagTargets` in
[store_signatures.go](../../internal/formatdata/oci/store_signatures.go); held by the area `ociremote`
(`TestR_CUR_003_AnOCIRemoteInModeOnlyAllowedDeliversTheAllowedTagOnly`) and the leg `oci` (`TestOCICuration_R_CUR_004_…`).

### Q-CUR-161 · Each format's end-to-end case is the remote's own refusal, an allow and a download ahead; what the clients print is measured

Status: **Decided by the development agent on 2026-11-24** — selected from the options below while building R6; the owner reviews it
at the walkthrough.

Context: R-CUR-022 named what each format's leg shows: a range resolved from a filtered document (npm, NuGet), `denied:` of `docker
pull`, apt through the virtual repository, the newest allowed version of `ansible-galaxy`. The virtual repository's filtered Debian index
is held already by the area `debvirtual` (R-CUR-003), the virtual walk by the area `npm`, and what a client prints at a refusal was not
known for four of the five clients.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One case per format in its own set, on a remote of its counting test upstream in mode `onlyAllowed`: the client refused (what it prints, a row of the manual's table), an allow of one version through the API (where the client resolves a range or "the newest", it takes the allowed one), a version downloaded ahead through the queue and allowed that the client takes from the cache (the stub's count) (chosen) | The three acts every remote's user meets, through the client | Each behaviour once: the virtual index and walk stay the integration's; the clients' messages become a measured table | Debian's case shows the remote's bare 403 (H-1726), not the virtual repository's index |
| (b) The table of R-CUR-022 as written | — | As specified | The virtual Debian index twice, and the queue's effect on a client never shown |

**Choice:** (a). NuGet resolves the floating version `2.*` (a plain minimum takes the lowest version, not the newest); Docker 29 prints
the registry's message after `error from registry:`, not `denied:` (R-CUR-004's table now says so). **Rationale:** a client shows what a
remote's user meets; what a format shares is tested once below the client. The development agent would revisit it if a client's
behaviour through a virtual repository differs from its behaviour through the remote.

**Applied in the spec:** R-CUR-004 (the OCI row), R-CUR-022 (the end-to-end row). Tests: `Test<Format>Curation_R_CUR_004_…` in
`test/e2e/<format>_curation_test.go`, helpers in [curation_clients_env_test.go](../../test/e2e/curation_clients_env_test.go).

### Q-CUR-162 · A Debian index APT fetched by its hash is parsed

Status: **Decided by the development agent on 2026-11-24** — selected from the options below when the end-to-end case of the leg `deb`
(R6) found it; the owner reviews it at the walkthrough.

Context: R-DEB-023 says every `Packages` index a remote stores is parsed. The parse was enqueued only by the fill of an index at its plain
path; APT 3.0 fetches every index by its hash when the release says `Acquire-By-Hash: yes` (the test upstream's, Debian's and Ubuntu's
archives do), here by its SHA-512, and a by-hash file is content that no fill hook of an index saw. A Debian remote that only APT read had
no parsed rows: the tab Upstream found none of its packages, a download ahead of a version had no file to name, and the pool files were
stored without the digest check of R-DEB-022. The integration area `debremote` fetched the plain `Packages.gz` and never met it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The commit of a by-hash file below a `binary-<arch>` directory enqueues the parse of that directory's `Packages`; the job reads the index the current release names for it by its SHA-256 (chosen) | The rows follow what APT reads | One coalesced job per index and release, a no-op when parsed already; no new column | The job may fetch the index a second time from the upstream (by its SHA-256 where APT took the SHA-512), once per release |
| (b) Match the by-hash file to its index by its digest | Exact | The stored file is the one parsed | The SHA-512 of an index is not stored: a column and a migration for a match the release already implies |
| (c) Parse every index a release lists when it is stored | Rows before any client | The catalogue is full at once | Every component and architecture of a large archive on every release, what **Read the index** (Q-CUR-62) leaves to a person |

**Choice:** (a). **Rationale:** a remote parses what its clients read, as R-DEB-023 intended. The development agent would revisit it if a
large archive's second fetch per release shows in the upstream's quota.

**Applied in the spec:** [formats/debian.md](formats/debian.md) R-DEB-023. Code: `byHashFilled` in
[hooks.go](../../internal/formats/deb/hooks.go); held by `TestR_DEB_023_AnIndexFetchedByItsHashIsParsed` (area `debremote`) and the leg
`deb` (`TestDebCuration_R_CUR_004_…`).

### Q-CUR-163 · The background engine of the role api shares the caches of its read-only view

Status: **Decided by the development agent on 2026-11-24** — selected from the options below when the journey of the tab Upstream
(R6) found it on the kind cluster; the owner reviews it at the walkthrough.

Context: Q-CUR-60 lets the role `api` build a background engine at the first catalogue call. That role has built, at its start, the
read-only view of the remote members for the explanation of a resolution (Q-ARCH-294), on the same engine code, which registers the
per-repository state and the negative L1 in the process's cache registry; the second engine registered both again, the registry refused
it with a panic, and every catalogue call of the role `api` answered 500. The integration tests ran the catalogue on the role `all`,
which has no view; the renders answered the catalogue in the browser. On an installation that separates `api` from `packages`, as the
chart does by default (its Deployments `api` and `packages`), the tab **Upstream** answered no search, no lookup and no package.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The engine takes the view (`proxy.Options.View`) and reads the state and the negative L1 through the view's two caches (chosen) | One registration per process | The same rows by the same readers; the change area still invalidates the shared entry | The view's engine reloads only the repositories it knows at once; the background engine loads its others at its next read |
| (b) The background engine registers its caches under other names | Two copies | Simple | Two caches of the same rows, both flushed, twice the memory |
| (c) The background engine on a cache registry of its own | Isolated | No coupling | That registry hears no change area: a circuit opened elsewhere stays unseen up to the safety period |

**Choice:** (a). **Rationale:** the view and the engine read one state; one cache of it is the registry's rule (R-ARCH-032). The
development agent would revisit it if the role `api` gets the remote engine of the client path, which would make the view unnecessary.

**Applied in the spec:** none (the wiring of Q-CUR-60). Code: `Options.View` in [engine.go](../../internal/proxy/engine.go),
`virtualEngineArea.view` in [wire_virtual_engine.go](../../internal/app/wire_virtual_engine.go), `buildEngine` in
[wire_mirror_host.go](../../internal/app/wire_mirror_host.go); held by `TestR_CUR_009_TheCatalogueAnswersOnAReplicaOfTheRoleAPI` (area
`npm`) and the journey `24-upstream.spec.ts`.

### Q-CUR-164 · The catalogue keeps a page that found nothing only when it asked the upstream

Status: **Decided by the development agent on 2026-11-24** — selected from the options below after R6's builder recorded that a Debian
lookup made before the remote's index was parsed stayed empty for up to five minutes; the owner reviews it at the walkthrough.

Context: the catalogue kept every page for `proxy.catalogue.cache_ttl` (300 s) per replica (R-CUR-008), a not-found as well. The period
protects the upstream's budget: a second identical call sends no request. A Debian remote's catalogue sends none at all; it answers from
the indexes the remote parsed (R-DEB-036), which a client's `apt update` or a download ahead fills at any moment. A lookup or a search made
before the parse kept "nothing" on that replica for the period, though the rows held the package seconds later; the end-to-end case of
the leg `deb` and the integration case of the area `debremote` had to read the rows in the database instead of the catalogue for it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A page that found nothing is kept only when a request of the call reached the network; the engine sees it in the budget's `Spend`, which the lent upstream calls before every request (chosen) | No format flag: whatever answers from Quader's own rows or stored copies and found nothing is asked again | Every format that searches its own rows is covered without declaring it; an upstream's not-found keeps its period and its budget; no contract change | A query that finds nothing is run again at every call, a statement on Quader's own rows; a page that found something stays up to the period old after a parse |
| (b) The epoch of the repository's last parse in the cache's key, read per call through a new optional call of the format | Every page of a parsed index is dropped at the next parse | A version the parse added shows at once on a page that found something, too | A database read at every call, also one the cache answers, which halves what the cache saves for Debian; a call of the format contract and of Debian's data port, in the kit's lists |
| (c) No page of a format whose search is `index` is kept | Debian's catalogue reads its rows every time | Simple | Keyed on the search's kind, though the lookup and the levels are what goes stale; every page read again, also those that found something |

**Choice:** (a). **Rationale:** the period exists to spare the upstream's budget (R-CUR-008), and a call that sent no request spent none,
so keeping its "nothing" only hides what the next parse adds. A page that found something is as old as any upstream's page may be. The
development agent would revisit (b) if a person misses a version the parse added on a package page within the period, or if the queries
that find nothing show in the database's load.

**Applied in the spec:** R-CUR-008 ("Short caching" and the unit criterion). Code: `call` and `foundNothing` in
[catalogue.go](../../internal/remotecuration/catalogue/catalogue.go); held by `TestR_CUR_008_TheCatalogueEngine` (unit), by
`TestR_DEB_036_TheCatalogueSearchesTheIndexesTheRemoteParsed` (area `debremote`, which now looks the package up before the parse and waits
on the catalogue) and by the leg `deb` (`TestDebCuration_R_CUR_004_…`, which waits on the catalogue instead of the database rows).

### Q-CUR-165 · A level the upstream does not have says so, with the format's naming rule and one suggestion built from it

Status: **Decided by the development agent on 2026-11-24** — selected from the options below after the owner's report of 08:05 ("Die
Suche nach meinem namespace scheitert."); the owner reviews it at the walkthrough.

Context: on the owner's installation the tab **Upstream** of the Docker Hub remote, at the namespace entered by name `guided-traffic`,
said "This level lists no package." Docker Hub's namespace is `guidedtraffic`: its namespaces hold no hyphen, and its listing
(`/v2/namespaces/{namespace}/repositories`) answers a namespace it does not have with `200` and an empty page, not a `404`. Measured on
2026-11-24 with public requests (the formats' `testdata/catalogue/README.md`): the same holds for Quay's listing, npm's key range of a
scope on the replication host and the Galaxy's collection index; each operator has an account of the name that answers `404` for one it
does not have: Docker Hub `/v2/users/{name}` (`200` a user, `308` to `/v2/orgs/{name}` an organization; `/v2/namespaces/{name}` answers
`405`), Quay `/api/v1/organization/{name}` and `/api/v1/users/{name}` (an organization answers only the first, a user only the second),
npm `/-/org/{scope}/package` (`200` for an organization's and a user's scope, `404 Scope not found`), the Galaxy `v3/namespaces/{name}/`.
GitLab's project API answers `404` with the message `404 Project Not Found`; a Debian archive has a suite when it has its `InRelease` or
`Release`, whose `Components` and `Architectures` name the rest.

Decision 1, where the difference lives:

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `discover.LevelPage.Unknown` (`*UnknownLevel`: the rule the name breaks, its forbidden characters, a suggestion), set by the format; the API's `unknown` with the upstream's display name; the screen's sentence by the level's kind (chosen) | A field of the contract's page | One screen branch for every format; the engine keeps such a page like any page an asked upstream answered; a format that cannot tell sets nothing and the screen says what it said | A field every format may set, held by kit check 22 where its fixture names an unknown level |
| (b) `404 catalogue_not_found` for an unknown level | An error | No new field | An error is not kept by the cache, so every look at the same wrong name spends the budget again; the screen shows the shared error notice with **Retry**, which a wrong name does not need; a suggestion has no place |
| (c) A sentence of the format in English | Free text | No closed set | Not translated; the German screen would carry English |

Decision 2, what it costs: a level that lists nothing on its first page asks at most two accounts of its name besides the listing, by
`HEAD` (`discover.Exists`; Quay's two, Debian's `InRelease` and `Release`, which the stored copy answers without a request), and only
then; a level that lists something costs nothing more. Kit check 22 holds the bound: at most three reads of the upstream.

Decision 3, the naming rule before any request:

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Only what is certain is refused unasked: a Docker Hub namespace's characters outside `[a-z0-9_]` (its rule is lowercase letters and digits; the underscore of its oldest namespaces and their lengths are not certain, so a name with one is asked for), a Galaxy namespace's outside `[a-z0-9_]`; npm's scope keeps the name parser's refusal (`400`) (chosen) | The rule's certain part | No request for a name no namespace can have, the owner's case among them | A Docker Hub name of three characters is asked for |
| (b) Docker Hub's whole rule, 4 to 30 lowercase letters and digits | Stricter | One request fewer for a short name | A namespace of the registry's first years that breaks today's rule could not be browsed |

Decision 4, how far the suggestion goes:

| Option | What it means | For | Against |
|---|---|---|---|
| (a) One candidate made by the format's rule from the name, asked once and named only when the upstream has it: Docker Hub drops the forbidden characters (`guided-traffic` → `guidedtraffic`), the Galaxy turns a hyphen into an underscore and drops the rest (`netbox-community` → `netbox_community`, the namespace of that GitHub organisation); none for a name that keeps the rule (chosen) | One request, only after a rule break | A suggestion that is never wrong (the upstream has it); constant cost | No help for a typo that keeps the rule |
| (b) The upstream's search for the name | Fuzzy | Finds typos | Docker Hub's search for `guided-traffic` answered `apache/traffic_*` and none of the owner's; a page of guesses for one request more |
| (c) No suggestion | Nothing | No request | The owner's case needs the one word the rule implies |

**Choice:** 1 (a), 2 as stated, 3 (a), 4 (a). **Rationale:** the person entered a name and needs to know whether it is wrong or the
level empty, in the words of the level's kind and, where the format has a rule, the rule; the screen stays one branch and the cost one
constant per format. The development agent would revisit 3 (b) if Docker Hub documents that no namespace breaks its rule, and 4 (b) if
a person reports a typo the rule does not catch.

**Applied in the spec:** R-CUR-007 (the page and its acceptance), R-CUR-009 (`unknown`), R-CUR-018 (the screen); ADR-277 (the contract's addition);
[formats/oci.md](formats/oci.md) R-OCI-038, [formats/npm.md](formats/npm.md) R-NPM-049, [formats/ansible.md](formats/ansible.md)
R-ANS-047, [formats/debian.md](formats/debian.md) R-DEB-036; [architecture.md](architecture.md) check 22. Code: `UnknownLevel`,
`Forbidden`, `Exists` and `EmptyOrUnknown` in [discover/catalogue.go](../../internal/mirror/discover/catalogue.go); each format's
`catalogue.go`; `unknownLevel` in [upstream_catalogue_api.go](../../internal/api/v1/upstream_catalogue_api.go); `unknownLevelText`,
`nameRuleText` and `suggestionText` in [upstream-words.ts](../../web/src/app/features/repositories/upstream/upstream-words.ts).

### Q-CUR-166 · The hint under the upstream search is a code the screen says in its language

Status: **Decided by the development agent on 2026-11-24** — selected from the options below after its own look at the renders of the
unknown level (09:10); the owner reviews it at the walkthrough.

Context: under "Upstream durchsuchen" the German screen of a Docker Hub remote read "Docker Hub's repository search, which its operator
does not document; an official image is library/<name>." The capabilities carried `searchNote` and `searchReason`, English sentences
each format wrote per upstream, and the screen showed them as written (Q-CUR-155). They spoke to an operator (how the upstream's
interface is documented, how it ranks) rather than to a person who wants a package, and none was translated. The texts as they stood:
npm "The registry ranks names, descriptions, keywords and maintainers; a query takes 2 to 64 characters." (and "This npm registry does
not answer searches." while off); NuGet "The upstream searches listed packages only; id:, packageid:, owner: and tags: narrow a query." and
"This NuGet upstream has no search resource; look a package up by its exact ID."; OCI on Docker Hub the sentence above (and "Docker Hub's
search did not answer as expected; look an image up by its name or list a namespace." while off), on Quay "Quay's search of public
repositories by name and description.", on ghcr.io "ghcr.io offers no search and no listing of images; look an image up by its full
name.", on GitLab "GitLab's registry offers no search; look an image up by its full name or list a project's images.", on a registry
without a preset "This registry offers no search; look an image up by its full name."; Ansible Galaxy "The Galaxy ranks the namespace,
the name, the tags and the description of the highest version." (and "This Galaxy does not answer the keyword search; list a namespace or
look a collection up by its exact name." while off); Debian "Quader searches the indexes the remote has read, as apt search does: every
word in the name, its Provides or its short description; a suite whose indexes are not read is not searched."

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A code: `discover.CatalogueCaps.SearchHint` from a closed list in the leaf (`discover.SearchHints`), one per upstream whose hint differs; `UnavailableError.Hint` for a search the engine turns off; the API's `searchHint` enum replaces `searchNote` and `searchReason`; the screen writes the sentence per code with `$localize` (chosen) | The screen speaks, as for the unknown level (Q-CUR-165) | Every sentence is in the screen's language and written for a person: what to type, an example, and where there is no search what to enter instead; a format adds its codes and the screen a sentence each, a constant per format | A new upstream's hint needs a code in the leaf, the API's enum and the screen; a Go test holds the enum to the leaf, kit check 22 a format's caps to the list |
| (b) The server writes the sentence in the language of `Accept-Language` | Server-side translation | One field, no enum | A second catalogue of German texts in Go beside the screen's, which nothing else in the API has; the glossary and the formal-German check would not reach it |
| (c) Code and parameters (an example name from the format) | Code plus values | The example comes from the format | The example is the screen's wording, not a fact of the upstream; one field more for no gain |
| (d) No hint | Nothing under the field | Nothing to translate | Docker Hub's `library/<name>`, npm's 2 to 64 characters, NuGet's `id:` and why there is no search are lost |

**Choice:** (a). The codes and their sentences:

| Code | Upstream | English | German |
|---|---|---|---|
| `dockerHub` | OCI, Docker Hub | Search for an image name, for example nginx; official images are under library/<name>. | Suchen Sie nach einem Image-Namen, z. B. nginx; offizielle Images liegen unter library/<Name>. |
| `dockerHubUnavailable` | OCI, Docker Hub's search off for the cache period (R-CUR-008) | Docker Hub's search is not available at the moment; enter an image's full name, for example library/nginx, or open a namespace. | Die Suche von Docker Hub ist zurzeit nicht verfügbar; geben Sie den vollständigen Namen eines Images ein, z. B. library/nginx, oder öffnen Sie einen Namespace. |
| `quay` | OCI, Quay | Search for an image name or a word of the description, for example prometheus. | Suchen Sie nach einem Image-Namen oder einem Wort aus der Beschreibung, z. B. prometheus. |
| `ghcr` | OCI, ghcr.io | ghcr.io offers no search; enter an image's full name, for example stefanprodan/podinfo. | ghcr.io bietet keine Suche; geben Sie den vollständigen Namen eines Images ein, z. B. stefanprodan/podinfo. |
| `gitlab` | OCI, GitLab | GitLab's registry offers no search; open a project to see its images, or enter an image's full name. | Die Registry von GitLab bietet keine Suche; öffnen Sie ein Projekt, um seine Images zu sehen, oder geben Sie den vollständigen Namen eines Images ein. |
| `ociRegistry` | OCI, a registry without a preset | This registry offers no search; enter an image's full name. | Diese Registry bietet keine Suche; geben Sie den vollständigen Namen eines Images ein. |
| `npmRegistry` | npm | Search for a package name, a keyword or a maintainer, for example express; a search takes 2 to 64 characters. | Suchen Sie nach einem Paketnamen, einem Schlüsselwort oder einem Maintainer, z. B. express; eine Suche umfasst 2 bis 64 Zeichen. |
| `npmNoSearch` | npm, a registry without the search route | This registry offers no search; enter a package's exact name, for example express. | Diese Registry bietet keine Suche; geben Sie den genauen Namen eines Pakets ein, z. B. express. |
| `nugetSearch` | NuGet | Search for a package ID or a word of the description, for example Serilog; id:, owner: and tags: narrow the search. | Suchen Sie nach einer Paket-ID oder einem Wort aus der Beschreibung, z. B. Serilog; id:, owner: und tags: grenzen die Suche ein. |
| `nugetNoSearch` | NuGet, a service index without a search resource | This NuGet upstream offers no search; enter a package's exact ID, for example Serilog. | Dieser NuGet-Upstream bietet keine Suche; geben Sie die genaue ID eines Pakets ein, z. B. Serilog. |
| `galaxy` | Ansible Galaxy | Search for a namespace, a collection name, a tag or a word of the description, for example docker. | Suchen Sie nach einem Namespace, einem Collection-Namen, einem Tag oder einem Wort aus der Beschreibung, z. B. docker. |
| `galaxyNoSearch` | Ansible Galaxy without the keyword search | This Galaxy offers no search; open a namespace or enter a collection's exact name, for example community.general. | Diese Galaxy bietet keine Suche; öffnen Sie einen Namespace oder geben Sie den genauen Namen einer Collection ein, z. B. community.general. |
| `aptIndex` | Debian | Search for words of a package's name or short description, for example nginx; Quader searches only the suites whose index it has read. | Suchen Sie nach Wörtern aus dem Namen oder der Kurzbeschreibung eines Pakets, z. B. nginx; Quader durchsucht nur die Suites, deren Index es gelesen hat. |
| `noSearch` | an upstream without a search and without a hint of its own (the engine's fallback for a search turned off without one) | This upstream offers no search; enter a package's exact name. | Dieser Upstream bietet keine Suche; geben Sie den genauen Namen eines Pakets ein. |

The upstream's own name (Docker Hub, ghcr.io, GitLab) is not translated. `searchHint` is null where a format names none or one the API
does not know; the screen then shows no sentence. `levelsReason`, a page's `note` and the detail of `400 catalogue_not_offered` stay
English sentences of the API: the screen shows none of them (the refusal has its own sentence, Q-CUR-155). The contract's version stays
11: two fields of `discover.CatalogueCaps` became one and `discover.UnavailableError` gained one, no method changed its meaning or left,
and every format moved in the same change (ADR-277). **Rationale:** a hint a person reads is in the screen's language and says what to
type, as the unknown level's sentence does; the development agent would revisit (b) if the API ever answers more than a handful of
server-written sentences a screen shows.

**Applied in the spec:** R-CUR-007 (the capabilities and their Go block), R-CUR-018 (the search and the sketches); Q-CUR-155 (the note
that it was revisited); [formats/oci.md](formats/oci.md) R-OCI-038, [formats/npm.md](formats/npm.md) R-NPM-049,
[formats/nuget.md](formats/nuget.md) R-NUGET-035, [formats/ansible.md](formats/ansible.md) R-ANS-047, [formats/debian.md](formats/debian.md)
R-DEB-036; the API's `UpstreamCatalogue.searchHint`. Code: `SearchHint` in
[discover/catalogue.go](../../internal/mirror/discover/catalogue.go); each format's `catalogue.go`; the engine's `turnOff` in
[catalogue.go](../../internal/remotecuration/catalogue/catalogue.go); kit check 22 in
[checks_catalogue.go](../../internal/formats/formattest/checks_catalogue.go); `searchHintText` in
[upstream-words.ts](../../web/src/app/features/repositories/upstream/upstream-words.ts).

### Q-CUR-80 · A download of one version runs before the catalogue: the call that names its files is a contract interface of its own

Status: **Decided by the development agent** — selected from the options below while building the download queue (R4); the owner reviews
it at the walkthrough.

Context: R-CUR-011's run starts from the row's probe, else from the format's `Catalogue.VersionProbe` (Q-CUR-109), and a bulk is
expanded through the catalogue's interface. The catalogue's package (R3) is not built: no format's adapter implements `formats.Catalogue`,
so a person's download of one version, the acceptance of R-CUR-010 and R-CUR-011, and every bulk would have nothing to start from.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `VersionProbe` is the interface `formats.VersionProber`, which `formats.Catalogue` embeds and `CapabilitiesOf` finds as `RemoteCaps.VersionProbe`; the five formats implement it now in `version_probe.go`; a package bulk is expanded through the format's mirror lister (`MirrorPlan`) with the bulk's bounds until the catalogue answers it; a sub-section answers `400 catalogue_not_offered` until the catalogue lists levels (chosen) | One method of the contract ahead of the rest | A version and a package can be downloaded ahead now in every format, with their integration cases; the declaration never claims a browsing call the adapter does not answer (Q-CUR-109's reason against stubs); R3 adds the browsing calls beside it | The contract surface grows by one interface; the preview of a package counts nothing until the catalogue does (`versions` and `bytes` are `null` with a note) |
| (b) The queue waits for R3 | Nothing new | One package fewer touches the contract | No download ahead runs, and R4's acceptance waits for another package |
| (c) Every format answers `VersionProbe` through its mirror plan in the format host | No contract change | One generic path | OCI reads every tag's manifest of an image and filters by the mirror's tag patterns; Ansible Galaxy reads every version document; a Debian remote without mirror suites finds nothing |

**Choice:** (a). npm, NuGet and Debian name a version's files from their mirror plan's rows (`formats.ProbeFromPlan`); OCI reads the tag's
document and names the manifest by its digest, as a client's GET of the tag does; Ansible Galaxy reads the version document. Debian looks in
every suite whose release a key verifies and every binary index parsed below it, whatever its mirror scope (`rows.MirrorScope.AnyIndex`):
a remote with no mirror suites can download ahead what its clients have read the indexes of.

**Rationale:** the call is the queue's, not the screens'; the development agent would revisit it if the catalogue's `Package` answers a
version's files more cheaply than the plan.

**Applied in the spec:** R-CUR-010 (the preview without the catalogue), R-CUR-011 (the run's plan); [architecture.md](architecture.md)
R-ARCH-008 (the contract block); [../developer/remote-curation.md](../developer/remote-curation.md) (R4).

### Q-CUR-81 · The step of a failure is the exchange's where an exchange failed

Status: **Decided by the development agent** — selected from the options below while building the download queue (R4); the owner reviews
it at the walkthrough.

Context: R-CUR-011 lists `plan` (reading the documents that name the files) beside the steps of one exchange (`resolve`, `connect`,
`tls`, `request`, `status`, `body`) and its acceptance wants a 503 of the stub shown as step `status`. The first attempt of a version reads
its document in the step `plan`, so the two descriptions meet.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A failed exchange names its own step wherever the run was; `plan`, `verify`, `gate` and `store` name failures that are no exchange's (a version the document does not list, a digest that differs, a gate's refusal, the object store) (chosen) | The step says what failed | A script that filters `step=tls` finds every TLS failure; the address and the status say which document | Whether a document or a file failed is read from the address, not the step |
| (b) The phase wins: a failure while the plan is read is `plan` | The phase | Two steps for one fact | `plan` would hide the transport's step that R-CUR-011 asks for |

**Choice:** (a). The engine attaches what the exchange told to its error (`proxy.Exchange`: the step, the address without its query, the
status, the excerpt, the Retry-After) and `proxy.Describe` maps any error of a fill to the class, the step and the code.

**Rationale:** the development agent would revisit it if the screens need the phase as well; it would be a member of its own.

**Applied in the spec:** R-CUR-011 ("The failure").

### Q-CUR-82 · The bound of running rows holds across replicas by the repository's row lock

Status: **Decided by the development agent** — selected from the options below while building the download queue (R4); the owner reviews
it at the walkthrough.

Context: R-CUR-011 starts rows at an insert, at the end of another row's run and in the dispatch task, on any replica, at most
`proxy.downloads.per_repository` at once. Transactions run in READ COMMITTED: two starts that count the running rows at once would both
start one.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) Every start takes the repository's row with `SELECT … FOR NO KEY UPDATE` first, then counts and starts (chosen) | A row lock | No lock category to register; it does not conflict with the key-share lock a row's foreign key takes, so an insert is never held | A start waits for a configuration write of the same repository, which is short |
| (b) A transaction-scoped advisory lock of a new category | `pg_advisory_xact_lock(<category>, repo)` | Touches no table | A category to register in `internal/cluster/locks.go` and R-ARCH's table for one bound |
| (c) No lock | Count and start | Simplest | The bound is exceeded by concurrent starts |

**Choice:** (a). **Rationale:** the development agent would revisit it if a configuration write of a remote shows waits behind its starts.

**Applied in the spec:** R-CUR-011 ("Execution").

### Q-CUR-83 · A probe without a version is a row named by its file; a run that reaches its timeout is a bound

Status: **Decided by the development agent** — selected from the options below while building the download queue (R4); the owner reviews
it at the walkthrough.

Context: `remote_download_one_active` keeps one waiting or running row per repository, name and version, and an OCI blob's HEAD names a
file of no version (`version` ''). Two blobs of one image would be one row. Separately, a run is a job of 30 minutes (R-REPO-065's bound),
and R-CUR-011 does not say what its end is.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A probe without a version is a row whose name is the probed file; a run that reaches 30 minutes ends its attempt `bound`, step `body`, code `download_timeout`, not retried (chosen) | Each blob its row; a timeout is a bound | Two blobs of one image are fetched both; what a timed-out attempt stored stays, and a retry by a person goes on from it | The tab shows a blob's path as its name |
| (b) Name the row by the image and refuse the second blob as active | One row per image | Fewer rows | The second probe is lost |
| (c) A timeout is `internal` and retried | Retried | Goes on by itself | A download that cannot finish in 30 minutes retries five times for 2.5 hours |

**Choice:** (a). **Rationale:** the development agent would revisit the timeout if large images on slow links need more than one attempt
by default.

**Applied in the spec:** R-CUR-011, R-CUR-013.

### Q-CUR-84 · A client's failed fetch is recorded on the fetch's own failure, once per flight

Status: **Decided by the development agent** — selected from the options below while building the download queue (R4); the owner reviews
it at the walkthrough.

Context: Q-CUR-19 records a client's failed fetch as a row of origin `client`, at most once per repository and file per replica and minute.
Where a client's failure is known: the engine's classification of an answer or a transport error, the integrity check, and every
collapsed waiter that receives the leader's result.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The leader of a client's flight records it where the engine classifies the failure and where the integrity check refuses, through `proxy.ClientFailures`; a 404 and a shutdown are not recorded; waiters record nothing (chosen) | One write per failed flight, then the throttle | The flight is the upstream exchange; the failure has its words there | A failure the format's handler makes of a good answer (a document it cannot read) is not recorded |
| (b) Record in the format host for every client error | Every error | Covers the format's own refusals | Many errors are no upstream failure (403 of a rule, 404 of a package); the words are not known there |

**Choice:** (a). **Rationale:** the development agent would revisit it if the tab must show a format's refusal of an upstream document.

**Applied in the spec:** R-CUR-011 ("A client's failed fetch").

### Q-CUR-85 · The usage store records a bulk under `system`; the person is the row's and the event's

Status: **Decided by the development agent** — selected from the options below while building the download queue (R4); the owner reviews
it at the walkthrough.

Context: R-CUR-010 records a bulk "under the subject `system` with the person named in the row and the usage store's `startedBy`". The
usage store (`usage_bucket`) has no such column; a background request without a charge is recorded under `system` already (Q-LIM-19).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A bulk's fetches are recorded under `system` as every uncharged background request; the person is the row's `started_by`, the bulk's and the event `remote.download.started` (chosen) | No usage column | No change of the usage store, its rollups and its API | The usage overview does not say who started system work; the rows go after their retention, the event stays |
| (b) A column `started_by` in the usage buckets | A key of the usage rows | The overview could name the person | Every bucket's key grows for one origin of work |

**Choice:** (a). A person's download of one version is charged to the person (R-LIM-037's shape, `limits.Engine.StartDownload`): the POST of
the management API counts no request, only the background download and its upstream exchanges. **Rationale:** the development agent would
revisit it if an administrator needs the usage overview to name who caused system work ([H-1841](../security/remote-curation.md#h-1841--the-usage-overview-does-not-name-who-started-a-bulk)).

**Applied in the spec:** R-CUR-010 ("Whose budget").

### Q-CUR-140 · An examination row names the files its runs stored; a discard removes what belongs to the version alone

Status: **Decided by the development agent** — selected from the options below while building the requests and the examination (R2b);
the owner reviews it at the walkthrough.

Context: a discard and the retention task remove "the stored files of a version" (R-CUR-016) for every format with a remote class. The
generic package rows link a version to its items for npm, NuGet, Debian and Ansible Galaxy; an OCI remote writes no version rows (a tag is
a cached document), and its manifests and layers are named by digest and shared by tags. The run of a queue row knows every file it stored.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The examination row records the paths a run stored or found (`files`, at most 2,000); a removal takes those and the items the package rows link to the version, each kept when the format's `Classify` names that package and version (chosen) | A column, no new method of the format contract | Constant cost per format: nothing per format; no upstream call at a discard; a file shared by digest (an image's manifest, its layers) classifies to no version and is never taken from a sibling | An image's manifests and layers fetched for examination stay after the discard, until R-STOR-045 ([H-1842](../security/remote-curation.md#h-1842--a-discard-leaves-what-a-version-shares-by-its-digest)) |
| (b) A method of the format contract that names a stored version's files | Each format answers | Exact per format | Five implementations, a contract version, the kit's check; OCI cannot tell a layer of one tag from a shared one without reading every manifest |
| (c) Plan the version again at the discard (`VersionProbe`, `ProbeContent`) | The run's own plan | No column | An upstream call at every discard and every retention run; a discard fails while the upstream is down |

**Choice:** (a). **Rationale:** the development agent would revisit it when R-STOR-045 removes unused files, which takes the shared layers
no tag needs any more.

**Applied in the spec:** R-CUR-016 (the table, "What is examination", "Discard"); the migration
[00115_remote_examination_files.sql](../../internal/meta/postgres/migrations/00115_remote_examination_files.sql).

### Q-CUR-141 · The newest versions of a package are named by a bulk of the request or of the allow

Status: **Decided by the development agent** — selected from the options below while building the requests (R2b); the owner reviews it at
the walkthrough.

Context: a request for a package examines its newest versions that are no prereleases, and an allow of a package without
`downloadEveryVersion` downloads its newest version (R-CUR-015). The catalogue (R3) is not built, and only a worker holds the upstream
client that can name a package's versions (the format's mirror plan, the planner of a bulk, Q-CUR-80); the ask and the allow are answered
by an `api` replica.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A bulk of the package with `maxVersions` the examination's count (`examine_versions_per_request`) or 1, no prereleases, whose rows take the origin `request` (with the asker's charge) or `allow`, the entry's ID and the person; the bulk table gains `origin`, `request_id` and `ticket` (chosen) | The planner that expands every bulk names them on a worker | One way to name versions; the rows are the request's (order, the entry's examination, the decline's cancel); the asker is charged as R-CUR-010 says | Three columns of the bulk table; until the bulk is expanded the entry shows no row |
| (b) Name the version at the ask through the catalogue | Ask the upstream on the `api` replica | The row exists at once | Waits for R3; an upstream call inside the ask |
| (c) A row with an empty version that the run resolves | One row | No bulk | An empty version means a file of no version (an OCI blob, Q-CUR-83); the row's version changes while it runs |

**Choice:** (a). **Rationale:** the development agent would revisit it when the catalogue (R3) answers the newest versions on every
replica, which would make the ask's row exist at once.

**With the catalogue (R3, 2026-11-24).** The bulk is kept: its expansion now names the newest versions through the catalogue's `Package`
on the worker ([Q-CUR-65](#q-cur-65--a-bulk-and-a-preview-read-the-catalogue-a-preview-is-one-page-of-the-bulks-bound)), so the order is
the catalogue's; naming the version at the ask stays the revisit above.

**Applied in the spec:** R-CUR-011 (the bulk's columns), R-CUR-015 ("Examination at once", "Allow").

### Q-CUR-142 · A write that widens the rules removes the examination rows it comes to deliver; the task catches the rest

Status: **Decided by the development agent** — selected from the options below while building the examination (R2b); the owner reviews it
at the walkthrough.

Context: an examination row goes "when the rules come to deliver the version" (R-CUR-016). A rule's subject can be narrower or wider than
the rows it touches: a package allow beside a version block keeps that version refused, and a section allow covers many names. The mode is
written by the repository service, which knows nothing of examinations.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The write of an allow, and the removal of a block, compile the repository's rules in their transaction with the caller's mode and remove the rows of the rule's subject that the rules then deliver (a section reads at most 100,000 rows); the task `remote-examination-cleanup` drops every row of a delivered version it reads (chosen) | Exact at the write; the task closes what the write cannot see | A row of a version a narrower block still refuses stays, so the retention still removes it; a mode change is caught within a day | A row of a delivered version may live until the next run of the task |
| (b) Remove every row the rule's subject covers | Literal | Simple | A row of a version still refused would leave the retention, and its content would stay for ever |
| (c) Only the task removes rows | One place | No work in the rule's write | The allow of an examined version leaves its row a day; the retention could not tell it from what still waits |

**Choice:** (a). **Rationale:** the development agent would revisit the bound of a section's scan with measured examination counts.

**Applied in the spec:** R-CUR-016 ("What is examination", "Removal after a time without a decision").

### Q-CUR-143 · A decline discards what its own examination stored and cancels what it still fetches

Status: **Decided by the development agent** — selected from the options below while building the requests (R2b); the owner reviews it at
the walkthrough.

Context: a decline with `discard` discards "what was stored for its examination" (R-CUR-015). A package's entry is one subject among others:
a version of the same package may be examined for another entry, or have been fetched by a client before a rule refused it.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The versions whose examination row names the entry, with their files (Q-CUR-140); the entry's waiting rows and expanding bulks are cancelled (chosen) | What this entry caused | Another entry's examination and a client's earlier fetch stay; nothing the decline dismissed is still fetched | A version examined before the entry existed is not discarded by it (a discard of the subject removes it) |
| (b) Every version of the subject that the rules do not deliver | The subject | One rule with the discard | Removes what another request is examining |

**Choice:** (a). **Rationale:** the development agent would revisit it if deciders expect a decline of a package to empty its cache.

**Applied in the spec:** R-CUR-015 ("Decline").

### Q-CUR-144 · The replay of a discard makes the rows due and queues the retention task; it removes nothing itself

Status: **Decided by the development agent** — selected from the options below while building the retention (R2b); the owner reviews it at
the walkthrough.

Context: a restore replays `remote.examination.discarded` so that content discarded after the restore point does not come back
(R-CUR-016). A replay handler runs in a transaction of the restored database while no replica serves, holds no item store, no format hook and
no snapshot of the rules, and never deletes content (R-HA-054).

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The handler makes the subject's examination rows due (fetched at the epoch; a stored version without a row gets one) and queues a run of `remote-examination-cleanup` in its transaction (`jobs.EnqueueTx`); the run evicts each due version the rules still do not deliver (chosen) | The retention's own removal, with the format's hooks and the rules as restored and replayed | No content deleted by the replay; one way to remove; a version a rule allowed in between stays; idempotent (a resumed replay finds the rows due: already in effect) | The content stays until a worker runs the task after the restore; it is not served meanwhile, since the rules do not deliver it |
| (b) Delete the items in the handler | At once | No wait | Without the format's hooks its rows go stale; R-HA-054 forbids a replay to delete content |

**Choice:** (a). **Rationale:** the development agent would revisit it if a restore must leave no examined byte stored before it serves.

**Applied in the spec:** R-CUR-016 ("Discard"); [ha-and-helm.md](ha-and-helm.md) the row of `remote.examination.discarded` in R-HA-054.

### Q-CUR-145 · Until the catalogue is wired, an ask is not checked against the upstream

Status: **Decided by the development agent** — selected from the options below while building the requests (R2b), before the catalogue
(R3) lands; the owner reviews it at the walkthrough.

Context: the subject of an ask is checked by the catalogue's lookup, so a typo cannot become a request (`404 catalogue_not_found`,
R-CUR-015). The catalogue is built beside R2b.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The requests' service takes a lookup port; while none is wired an ask is checked by the format's name rule only, and the examination's download says whether the upstream has the subject (`upstream_not_found`) (chosen) | The check arrives with R3's wiring | Requests work before R3; the decider sees the failure in the entry's examination | A typo becomes an entry until then |
| (b) Refuse asks until the catalogue lands | Strict | No typo entries | No requests at all until R3 |

**Choice:** (a). **Rationale:** R3 wires its lookup into `requests.Options.Lookup`; nothing else changes.

**Wired with the catalogue (R3, 2026-11-24).** An ask is checked by the catalogue's lookup on the replica that answers it, spent from the
asker's budget: a package the upstream does not have, or a version it does not list, answers `404 catalogue_not_found`; an upstream that
cannot be asked answers the catalogue's error (`catalogueLookup` in [wire_upstream_catalogue.go](../../internal/app/wire_upstream_catalogue.go)).

**Applied in the spec:** R-CUR-015 ("Asking", through this block).

### Q-CUR-146 · The list names the versions an entry's examination stored; their verdicts are the stored versions'

Status: **Decided by the development agent** — selected from the options below while building the requests' list (R2b); the owner reviews
it at the walkthrough.

Context: the list of R-CUR-015 carried a member `verdicts`. The verdicts of a stored version (vulnerability findings, the malware verdict,
the signatures) are answered by their own services for every stored version (R-API-081, R-VULN-019, R-MAL-022, R-SIG-034), each with its
visibility.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The member `examined` lists each version the examination stored with its time and its count of files; a screen reads its verdicts where every stored version's are read (chosen) | No second path to the verdicts | The verdicts' visibility stays the scanners'; the list reads only its own tables | A screen makes one more call per examined version |
| (b) Join the three scanners' verdicts into each entry | One answer | Fewer calls | Three domains joined into a list; their visibility rules repeated |

**Choice:** (a). **Rationale:** the development agent would revisit it if the tab **Upstream** (R5b) needs the verdicts in the list.

**Applied in the spec:** R-CUR-015 ("The list").

### Q-CUR-147 · The mirror's admission asks the rules for a pass, never for a row of the queue

Status: **Decided by the development agent** while building the examination (R2b): a defect of how R2 and R4 met; the owner reviews it at the
walkthrough.

Context: the admission of a background fill (`MirrorHost.admitted`) asked the delivery rules for every fill, so a row of the download queue
of a version the rules do not deliver ended `refused` and stored nothing, against R-CUR-014 ("A download ahead does not depend on the
rules"). It was found by the examination's integration case.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The admission asks the rules for a pass of the mirror (R-CUR-017) and not for a row of the queue (a probe's, a person's, a request's, an allow's, a bulk's), which keeps the patterns (chosen) | The fill's own filler tells them apart | R-CUR-014 as specified; a mirror still keeps only what the rules deliver | — |

**Choice:** (a).

**Applied in the spec:** R-CUR-014 (as written); the code in [mirror_host.go](../../internal/formathost/mirror_host.go).

### Q-CUR-148 · Every person's ask is recorded; the last asker who withdraws ends an entry no client was refused

Status: **Decided by the development agent** — selected from the options below while building the requests (R2b); the owner reviews it at
the walkthrough.

Context: R-CUR-019 names `remote.request.created` and `remote.request.withdrawn`; R-CUR-015 names the state `withdrawn` and says that an
entry with no asker and no refusal is removed after `request_retention`.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `remote.request.created` for each person's ask (the first opens the entry, a later one joins it; a repeated ask of the same person is answered `200` and writes nothing); a withdrawal writes `remote.request.withdrawn`, and an open entry that keeps no asker and no refusal ends `withdrawn`; one that clients were refused stays open as theirs (chosen) | One event per act | A webhook tells about every ask; the list's states say what happened | An entry asked by many writes many events |
| (b) The event only when the entry opens | Fewer events | — | A second person's ask leaves no trace in the audit log |

**Choice:** (a).

**Applied in the spec:** R-CUR-015 ("Asking", "Withdraw"), R-CUR-019.

### Q-CUR-60 · The catalogue's upstream is lent by the mirror host, which the roles of the management API now build

Status: **Decided by the development agent** — selected from the options below while building the catalogue (R3); the owner reviews it at
the walkthrough.

Context: R-CUR-008 wants every catalogue request to go through the repository's upstream client with its egress rules, credential policy,
circuit and a background slot, and a document answered from the stored copy or read without storing. The developer page put the lent
upstream in `internal/proxy/catalogue_upstream.go`. The proxy does not know a format's handshake, exchange or URL map of a path, which the
format host holds; and the role `api`, which answers the management API, has no remote engine and no format host.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The proxy gains `Engine.Peruse` (a format's request built as a fetch builds it, sent as a discovery request, nothing written); the lent upstream is `formathost`'s `catalogueUpstream` over a pass's upstream, answering the stored copy by the cache view and the rest by `Peruse`; the area `mirror-engine` is built on `api` and `web` too, so `api` builds a background engine of its own at the first call (chosen) | One request path for passes, background runs and the catalogue | Egress, credentials, handshake, exchange, URL map, circuit and slots are the engine's, unchanged; nothing new decides a host | An `api` replica runs a background engine (small caches, Q-ARCH-317) once the catalogue is used |
| (b) A catalogue client of its own over `proxy/upstream.Clients` | No engine on `api` | Smaller footprint | A second request path that has to repeat the handshake, the exchange and the circuit, and reads no stored copy |
| (c) The catalogue only on roles with a remote engine (`web`, `all`) | No new area | Nothing to build | An installation that separates `api` and `packages` has no catalogue |

**Choice:** (a). A preset's `catalogueHosts` are handed to the request as the mirror's hosts are, so they never receive the repository's
credential; a preset that declared one with `sendCredentials` would still get none, and no preset does.

**Rationale:** the egress and credential rules are the remote engine's, and a second path would have to be held to them twice. The
development agent would revisit (b) if the background engine's memory shows on `api` replicas.

**Applied in the spec:** R-CUR-008; [architecture.md](architecture.md) R-ARCH-001 (the area table) and Q-ARCH-316; [../developer/remote-curation.md](../developer/remote-curation.md) (R3).

### Q-CUR-61 · A row's stored state is what the repository holds; the stored version's verdicts stay the version view's

Status: **Decided by the development agent** — selected from the options below while building the catalogue (R3); the owner reviews it at
the walkthrough.

Context: R-CUR-009 lists `stored` (`none`, `partial`, `whole`, for OCI the platforms held) and the stored version's gate verdicts (malware,
vulnerabilities, signatures) on every row. Whether a version is held whole needs the upstream's list of its files, and the three verdicts
are computed by the package API's version view over the scopes of the caller, three readers that each read their own rows.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `stored` from the live package versions of the repository: a package `none` or `partial` (some version held), a version `none` or `whole`; a stored version's verdicts are read from the package API's version view, which R5b's screen opens (chosen) | One statement per page | No second computation of three gates' badges that could disagree with the version view | A row says "stored" without its verdicts; OCI's platforms held are not shown |
| (b) Compute the three verdicts per row | As R-CUR-009 says | One answer | Three readers per page, and a second place that decides a badge |

**Choice:** (a). **Rationale:** the version view is the one place a badge is decided; the catalogue names the version so that the screen
can open it. The development agent would revisit (b) when R5b's screen needs the badges in the list itself.

**Applied in the spec:** R-CUR-009 (as built).

### Q-CUR-62 · Debian searches the name, Provides and the short description; the long descriptions and "Read the index" wait

Status: **Decided by the development agent** — selected from the options below while building the catalogue (R3); the owner reviews it at
the walkthrough.

Context: R-DEB-036 searches the long descriptions of `i18n/Translation-en` too, parsed as an index of their own, and offers **Read the
index**, a queue row that downloads a suite's release and indexes. Both change what the remote's parse and its queue run walk: every place
that enqueues a parse reads the release's index list (the release's record, the scope's fetch, the housekeeping and the virtual
repository's merge), and the merge reads the same list for paragraphs.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The parse keeps `provides`, the short description and `description_md5`; the search matches the name, Provides and the short description; the long descriptions and **Read the index** are not built in R3 (chosen) | What `apt search` finds in a `Packages` file | No change of what the merge and the housekeeping walk | A word only a long description holds is not found; a suite no client read is not searchable |
| (b) Parse `Translation-en` and add **Read the index** now | All of R-DEB-036 | Complete | A change of four enqueue paths and the queue run in the same package as five catalogues |

**Choice:** (a). `deb_remote_entries.description_md5` is stored, so the long descriptions join by it when their parse lands.

**Rationale:** the catalogue answers for every format now; the two parts change the remote's parse and queue, which are tested by their
own areas. The development agent would revisit it with R5b, whose Debian screen offers **Read the index**.

**Applied in the spec:** [formats/debian.md](formats/debian.md) R-DEB-036 (as built); the status of this page.

### Q-CUR-63 · The levels a repository has: a registry's catalog is a root of packages; a namespace or a project is entered by name

Status: **Decided by the development agent** — selected from the options below while building the catalogue (R3); the owner reviews it at
the walkthrough.

Context: `CatalogueCaps.Levels` names the kinds of levels above a package. An OCI registry without a preset lists its images at its root
(`_catalog`), with no level above an image; Docker Hub's and Quay's namespaces, npm's scopes and GitLab's projects are not listable
themselves.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) `levels` empty with no `levelsReason` means the root lists packages; a reason means no listing, and `Children` answers `catalogue_not_offered`; a level whose names are not listable answers an empty root and is entered by its name (chosen) | No new member | The engine and the screen read one rule | An empty root says nothing by itself; the capabilities' note says it |
| (b) A member `rootLists` | Explicit | Clear | A type change of the contract's leaf for one registry kind |

**Choice:** (a). OCI declares the level `namespace` for the format; a repository's capabilities say `project` for the GitLab preset.

**Rationale:** the declaration is the format's, the capabilities the repository's (R-CUR-007). The development agent would revisit (b)
if a second registry kind needs it.

**Applied in the spec:** R-CUR-007 (the capabilities), [formats/oci.md](formats/oci.md) R-OCI-038.

### Q-CUR-64 · A document is read whole up to the bound; a cursor travels base64url; the preview and the bulk read larger pages

Status: **Decided by the development agent** — selected from the options below while building the catalogue (R3); the owner reviews it at
the walkthrough.

Context: R-CUR-008 says a document is at most `proxy.catalogue.max_document_bytes` and parsed as a stream. The engine's request reads a
body into memory up to its bound, and the formats' parsers take bytes.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) A document is read whole up to the bound (64 MiB) and parsed; a call reads a document once and keeps it for the call; a cursor is the format's JSON, base64url-encoded in the API; the preview and a bulk's expansion read pages of the bulk's bound, the API at most 50, 200 and 100 (chosen) | The engine's discovery request | One read path; the largest document seen (npm's `typescript`, 2 MB) is far below the bound | A call may hold up to 64 MiB ([H-1730](../security/remote-curation.md#h-1730--a-catalogue-call-holds-a-whole-document-in-memory)) |
| (b) A streaming parser per format | As specified | Bounded memory | Five parsers of their own for the catalogue alone |

**Choice:** (a). **Rationale:** the budget (120 requests a minute per repository) and the bound keep the memory of a replica bounded; the
development agent would revisit (b) when a replica's memory shows it.

**Applied in the spec:** R-CUR-008 (as built).

### Q-CUR-65 · A bulk and a preview read the catalogue; a preview is one page of the bulk's bound

Status: **Decided by the development agent** — selected from the options below while building the catalogue (R3); the owner reviews it at
the walkthrough.

Context: R4 expanded a package's bulk through the format's mirror plan (Q-CUR-80) and answered a section `400 catalogue_not_offered`;
R-CUR-007 says a bulk expands through `Package` and `Children`, never a second lister.

| Option | What it means | For | Against |
|---|---|---|---|
| (a) The planner of the worker calls the format's catalogue through the worker's upstream (which stores, since a bulk downloads); a section's packages are the level's; the preview counts the newest `maxVersions` of one `Package` page and a section's packages by the level's count (chosen) | R-CUR-007 | OCI's tags are listed without a manifest per tag; one notion of a package's versions | npm's `latest` is no longer pinned in a bulk: the bulk takes the newest versions |
| (b) Keep the mirror plan for a package | Q-CUR-80 | Unchanged | Two listers of a package's versions |

**Choice:** (a). A level that lists only levels below it (a Debian suite) queues nothing; the preview's count says so.

**Rationale:** one lister per question. The development agent would revisit it if a format's catalogue answers a package's versions
more expensively than its plan.

**Applied in the spec:** R-CUR-010 (the preview and the expansion).

## What this does not cover

- [H-1720](../security/remote-curation.md#h-1720--a-search-tells-the-upstreams-operator-what-people-look-for) to
  [H-1730](../security/remote-curation.md#h-1730--a-catalogue-call-holds-a-whole-document-in-memory),
  [H-1840](../security/remote-curation.md#h-1840--clients-failures-of-many-files-write-as-many-rows-a-minute) and
  [H-1841](../security/remote-curation.md#h-1841--the-usage-overview-does-not-name-who-started-a-bulk) and
  [H-1842](../security/remote-curation.md#h-1842--a-discard-leaves-what-a-version-shares-by-its-digest): the gaps of this page's
  mechanisms, on its security page.
- The conditions of a curation policy and their wizard (SEC-009 to SEC-011), curation across sites (SEC-040, REPL-061).
- Rules of a local or a virtual repository.
- PyPI and every other format that does not exist (R-CUR-023).
