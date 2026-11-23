# Curating a remote repository

Status: specified on 2026-11-23; its contracts (the tables, the types, the codes, the events, the keys) are built, its behaviour is not. The owner asked for it on 2026-11-23 (task T-24 of the
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
  patterns before this change delivers everything until its rules are set.
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
- The kit of the format contract holds every format with a remote class to it (check 22, R-CUR-022): with a rule that refuses one version
  of a fixture package, no document of the format lists that version, the version's file answers the refusal with `Cache-Control:
  no-store`, and a package whose every version is refused answers the refusal at its document.

Acceptance criteria:

- Unit (kit check 22, `internal/formats/formattest`, milliseconds per format): the three statements above for every format with the class
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
  | OCI | `403`, `{"errors": [{"code": "DENIED", "message": <text>, "detail": null}]}` | `denied: <text>` |
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
  that a narrowing of the rules refuses is dropped by the `content` entry of `repository.narrowed` (R-CUR-021, R-BAS-015).
- **Audit:** `remote.delivery.refused` (R-CUR-019), asynchronous, at most once a minute per repository, package, version and client
  address on each replica, as `vulnerability.download_blocked` is throttled; details: the path, the package, the version, the reason
  (`blocked` or `not_allowed`), the kind (`document` or `artifact`).
- **Metric:** `quader_remote_delivery_refusals_total{repo,reason,kind}`.
- **Record:** every refusal on a client's request also counts in the request list's entry of its subject (R-CUR-015): one row per
  repository, package and version, with the first and last time and the count, written with the event's throttle, so the list reads no
  audit log.
- A download ahead is never refused by the rules (R-CUR-014); a pass of the mirror fetches only what the rules deliver (R-CUR-017).

Acceptance criteria:

- Unit (kit check 22): every format renders `403` with `Cache-Control: no-store` for the code, and the body's code in its shape.
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
[00114_remote_curation.sql](../../internal/meta/postgres/migrations/00114_remote_curation.sql) (built); beside the statements of this page
it bounds every counter at zero from below and every bulk's bounds at one.

- **One rule per subject.** A write of a subject that has a rule replaces its effect and note; the subject is the identity.
- **Canonical forms.** A name is stored by the format's name rule (`catalog.Names.Normalize`); a pattern folded by it; a version by the
  format's canonical form, which the format declares as `catalog.VersionCanon` (new, beside its `catalog.Names`; R-CUR-007): npm and
  Ansible Galaxy the SemVer text as given, NuGet the identity of its versions, the URL form of R-NUGET-003 (normalized, lowercased,
  without build metadata, so `1.0.0-Beta` and `1.0.0-beta` are one, Q-CUR-3), Debian the version as the index row has it, OCI the tag as given. A
  name the rule refuses answers `422 invalid_name`, a version the format cannot read `422 invalid_version`; the decision folds what it is
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
  | `search: upstream` | the upstream answers a text query (npm `-/v1/search`, NuGet's search resource, the Galaxy's keyword search, a preset's registry API); `searchWindow` is the deepest hit it reaches, `searchNote` what it matches |
  | `search: index` | Quader answers a text query over an index it holds of the upstream (Debian's `Packages` and `Translation-en` of the suites read) |
  | `search: none` | no text search; `searchReason` says why in one sentence |
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
      SearchNote   string     // what a query matches, one sentence
      SearchReason string     // why there is none, one sentence
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
- **The version canon** (`catalog.VersionCanon`, `Canonical(version string) (string, bool)`) is declared beside the catalogue by every
  format with the class remote (R-CUR-006); `Register` refuses a remote format without it.
- **Formats that come later** declare their catalogue in the same change that brings their remote class; R-CUR-023 shows what four of
  them would declare.

Acceptance criteria:

- Unit (`internal/formats`, milliseconds): `Register` refuses a remote format without the declaration and one whose declared call its
  adapter does not answer; every registered format's declaration is pinned by `TestR_CUR_007_EveryRemoteFormatDeclaresItsCatalogue`
  (`internal/formats/all`).
- Unit (kit check 21, `internal/formats/formattest`, milliseconds per format, a fake upstream in process answering canned documents of a
  few kilobytes from the format's fixture): each declared call answers a page whose names the format's rule accepts, a cursor that
  continues, `Done` at the end, and `ErrNotOffered` for an undeclared call; an answer above the bound of R-CUR-008 fails with
  `catalogue_answer_too_large`; no call reaches a host outside the upstream, its `extraHosts` and the declared `Hosts`.

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
  and never cached.
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
  `foreign_host_denied`; a shape failure of the declared undocumented search turns the capability off for the period.
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
| `upstream/levels?level=&cursor=&limit=` | a page of a level (`level` is the path joined by `/`, each part URL-encoded) |
| `upstream/package?name=&cursor=&limit=&filter=` | the package and a page of its versions; `404 catalogue_not_found` when the upstream does not have it |
| `upstream/vulnerabilities` (`POST`, `{"name", "versions": [≤ 50]}`) | the vulnerability verdict of each version that is not stored, computed as the miss gate computes it (`Evaluator.EvaluateMiss`, nothing written, [vulnerability-scanning.md](vulnerability-scanning.md) R-VULN-015); a format the vulnerability scanning does not cover answers `"notCovered": true` (R-CUR-014) |

- **Quader's state on every row.** A hit, a package and a version carry, beside the upstream's facts, what this repository makes of them:
  `delivery` (`delivered`, `blocked`, `notAllowed`, with `decidedBy` as in `delivery-rules/check`), `stored` (`none`, `partial`, `whole`;
  for OCI the platforms held), `examination` (a version stored that the rules do not deliver, R-CUR-014), the stored version's gate verdicts
  (`malware`, `vulnerabilities`, `signatures`, each as the version views of the package API carry them, R-VULN-020, R-MAL-022, R-SIG-034),
  `queued` (the queue row of a waiting or running download, R-CUR-011) and `request` (the open request entry of its subject, R-CUR-015).
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
  (Debian's index rows, a Galaxy namespace's count, a Docker Hub namespace's count), else `null`.
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
    own throttling (R-REPO-054). A mirror (Q-REPO-142) keeps a scope current without end and stays an installation's decision; a bulk is
    one act, previewed and bounded, and so a project's (Q-CUR-17).
- **Not a download.** A download ahead writes no download statistics (R-STOR-015) and no **Last download**, and is not a download of the
  limits' figures (R-LIM-007): it counts in `background_fetches` of the person charged, or of `system`.
- **Audit:** `remote.download.started` per `POST` (details: the level, the subject, the bounds, the bulk's ID, the count queued), with
  the person as actor; each stored file records the populate event of a fill with the actor `system:background-download` and the reason
  `download-ahead` (R-AUTHZ-029, as R-REPO-065's fills record theirs).

Acceptance criteria:

- Integration (area `npm`, `TestR_CUR_010_…`, about 3 s, the stub's corpus): the Maintainer previews `@acme/a` (3 versions, size
  `null`, `sizeUnknown: 3`), starts it, and three rows end `done`; the stub served three tarballs; the Developer's `POST` of the same
  answers `403`; the Developer's one version is charged to the Developer (usage row `background_fetches` 1); `maxVersions: 2` queues two.
- Integration, one download ahead per format (areas `nuget`, `ociremote`, `deb`, `ansible`, about 2 s each): one version is stored and
  listed; for OCI the platforms of `oci.platforms` with their layers.

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
    finished_at         timestamptz NULL
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
  download: at most 1,000 files, at most the repository's `limits.maxArtifactBytes`). The job of R-REPO-065,
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
  or verification error `tls`, a timeout while the body is read `body` (`internal/proxy/classify.go` gains the step beside the code).
- **A client's failed fetch** is recorded in the same table (Q-CUR-19), so the tab answers "why did my pull fail": a fetch of a client
  that ends with a failure of the classes above writes, at most once per repository and file per replica and minute, one row of origin
  `client` and state `failed` per repository and file, whose `occurrences` and failure the next failure updates; at most
  `proxy.downloads.client_failures_per_repository` (1,000) such rows per repository, the oldest removed. A `404` of the upstream is not
  recorded (the negative cache holds it, R-REPO-028).
- **Retention:** rows `done` and `cancelled` are removed `proxy.downloads.done_retention` (168 h) after they finished, rows `failed` after
  `proxy.downloads.failed_retention` (720 h), a bulk with its rows when its last row is removed; the task `remote-downloads-dispatch`
  removes them in batches of 1,000.
- A row is a download ahead of its repository: a virtual repository has no queue; a download through a virtual repository's address is
  started at the member that serves it (Q-REPO-367).

Acceptance criteria:

- Unit (`internal/proxy`, milliseconds, a table of about 30 transport and answer failures produced by `httptest` and a fake resolver):
  each maps to its class, step and code; the query is dropped from the address; the excerpt is cut and cleaned.
- Unit (`internal/remotecuration/queue`, milliseconds, a fake store and clock): the order of the waiting rows, the per-repository bound of
  running rows, the backoff and `Retry-After`, the end at `max_attempts`, the bound of waiting rows.
- Integration (area `npm`, `TestR_CUR_011_…`, about 3 s): the stub answers `503` twice and then `200`: the row shows `upstream_error`,
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
  a job; a version that has a waiting or running row starts nothing (`active`); "fewer than `limits.maxConcurrentFetches`" becomes the
  bound of waiting rows of R-CUR-011, and the outcome `queue_full` is counted only there.
- The run is R-CUR-011's: the switch `downloadOnProbe` still decides at the probe; a row that waits while it is turned off is `skipped`.
- **What a user sees** is the row on the tab **Downloads** (R-CUR-018), with its counters and its failure, instead of a job on the jobs
  page that only `settings:read` holders saw.
- The metric `quader_remote_background_downloads_total{format,outcome}` gives way to `quader_remote_downloads_total{repo,origin,outcome}`
  (R-CUR-019): the queue is a repository's, and the label `repo` is capped by R-OPS-007.

Acceptance criteria:

- The integration cases of R-REPO-065 (`TestR_REPO_065_*`, `TestQ_REPO_367_*`) run unchanged against the row and its run; one more
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
  decline_reason  text        NULL CHECK (char_length(decline_reason) BETWEEN 1 AND 1000)
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
  says so. A second asker starts nothing more.
- **Who sees them:** those who may set rules see every entry of the repository; the tab **Upstream** shows the count of open entries in
  its label and lists them first, set apart (R-CUR-018); an asker sees the entries they asked in, with the state and the reason of a
  decline.
- **Allow** (`POST delivery-requests/{id}/allow`, `{"as": "asked" | "package", "downloadEveryVersion": false, "note"?}`): writes the rule of
  the subject (`allow`, `request_id` set; `as: package` widens a version's entry to its package), sets the entry `allowed`, and starts the
  download at once: nothing more where the examination stored the version (it is delivered from that moment), else a row of origin `allow`
  of the version asked for (of a package, its newest version), and with `downloadEveryVersion` a bulk of the package (R-CUR-010). A row
  of the examination that waits or failed is started again. "An allow starts the download" holds in every case.
- **Decline** (`POST delivery-requests/{id}/decline`, `{"reason", "discard": true}`): a reason of 1 to 1,000 characters; the entry is
  `declined`; with `discard` (the default in the dialog) what was stored for its examination is discarded (R-CUR-016). Later refusals of
  clients count in the declined entry; a person's new ask opens a new entry.
- **Withdraw** (`POST delivery-requests/{id}/withdraw`): an asker leaves the entry; an entry with no asker and no refusal in
  `proxy.delivery.request_retention` (720 h) is removed by the task of R-CUR-016. At most `proxy.delivery.max_client_entries` (10,000)
  entries without an asker per repository: a client that is refused many names in mode `onlyAllowed` cannot grow the list without end;
  the entry with the oldest last refusal goes first.
- **The list** (`GET delivery-requests?state=&origin=person|client&mine=&cursor=`): `{"id", "name", "version", "state", "askers": [{"id",
  "name", "note", "askedAt"}] (names only for those who may set rules and for the asker themselves), "askerCount", "refusals", "firstAt",
  "lastAt", "examination": <queue row>, "verdicts", "decidedBy", "decidedAt", "declineReason"}`.
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
  PRIMARY KEY (repo_id, name, version)
);
CREATE INDEX remote_examinations_age ON remote_examinations (fetched_at);
```

- **What is examination.** A run of the queue that stores a file of a package's version that the rules do not deliver at that moment
  writes the version's row (once; a later run keeps `fetched_at`); a document it stores (a Debian index that **Read the index** reads,
  R-DEB-036) is none. The row goes when the rules come to deliver the version (the write of the allowing rule
  deletes the rows it covers), at a discard and at the removal below.
- **Discard** (`POST examinations/discard`, `{"name", "version"?}`, those who may set rules): removes the stored files of the versions
  of the subject that the rules do not deliver, as an eviction (R-AUTHZ-006 `delete` on a remote), with their rows, and writes
  `remote.examination.discarded`, flagged `journal: revocation`, so a restore removes them again (replay: evict what is stored of the
  subject and still not delivered; nothing else). A delivered version is never discarded (`409 delivered`).
- **Removal after a time without a decision:** the task `remote-examination-cleanup` (daily, `0 4 * * *`, R-OPS-019) removes the stored
  files of every version whose examination row is older than the repository's `remote.delivery.examinedRetentionDays` (30 by default, 1
  to 365) and that the rules still do not deliver. It writes the same event with the actor `system:examination-cleanup`, in batches of
  1,000, checkpointed.
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
  (`discover.LiteralPrefix`) or by `remote.mirror.maxBytes`; the rules alone are no bound, because a Maintainer can allow `**`.
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
  the document title and the breadcrumb end with the tab. A local or virtual repository shows no tab strip. A tab's filters stay in its
  address. The label **Upstream** carries the count of open requests for those who may set rules (`Upstream 3`); **Downloads** the count
  of failed rows when there are any (`Downloads · 2 failed`).
- **The tab Upstream** (`…/upstream`):
  - At the top, one line about the mode: "Delivers everything except what is blocked." or "Delivers only what is allowed.", with
    **Change** to the setting for those who may set the configuration, and for those who may set rules **Rules (n)**, which opens the
    rule list.
  - For those who may set rules and while open entries exist, the section **Requests** first, set apart (a bordered panel with its count):
    each entry's subject, who asked or how often clients were refused, the examination's state and verdicts, and **Allow** and
    **Decline**; an asker sees **Your requests** in the same place.
  - The search: a field as wide as a name, the toggle **Names only** where the format has it (Debian), the sentence of what it matches
    (`searchNote`); where the capability is `none`, the field becomes **Look up by name** and one sentence says why ("ghcr.io offers no
    search and no listing of images; look an image up by its full name.").
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
- **The settings page** (R-UI-087): a closed section **Delivery** (details on demand, R-UI-067) with the mode as two radio choices and
  their sentences, **Fetch for examination when someone asks** (on), **Remove what was examined and not allowed after [30] days**; for a
  mirror, `remote.mirror.include` joins the section Mirror as **Mirror only these packages**.
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
│ ghcr.io offers no search and no listing of images; look an image up by its full name.                 │
│ ⚠ The upstream is assumed offline until 10:42 (3 failures: upstream_unreachable); Quader retries then. │
```

Debian, a suite read and one not read:

```text
│ Suites: trixie ▸ main ▸ amd64        trixie-security (index not read) [Read the index]               │
│ Search the index [ libssl           ] [x] Names only     (as apt search: names, provides, descriptions)│
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
  a remote only; each state's sentence; the actions by permission; the German texts of the glossary rows.
- UI layout checks (`web/e2e`, the existing walk, about 20 more screens at three widths as the three users): the two tabs in both
  schemes with the states listed above.

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
| `quader_remote_delivery_requests_open` | `repo` | open request entries (gauge) |

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
[maintenance.go](../../internal/config/maintenance.go), `docs/developer/configuration.md`; built, nothing reads them yet). A duration is a
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

### R-CUR-022 · Tests and what they cost
Priority: **P1** · Covers: quader-only

By the owner's rule of 2026-11-20 (tests stay runnable): each behaviour once, at the cheapest tier that shows it; a format adds a small,
constant amount; payloads of kilobytes.

| Tier | What | Run time | Data |
|---|---|---|---|
| Unit | the rule decision (R-CUR-001), the failure classification (R-CUR-011), the queue's order and retries, the catalogue engine's budget, cache and bounds, the combined serve decision with the fourth check | about 2 s in all | none |
| Unit, the format contract's kit | check 21 (the catalogue against a fake upstream in process) and check 22 (documents and refusals), per format | milliseconds per format, about 0.5 s for five | the fixture's canned documents, a few KB each |
| Integration, per format | one case: a rule refuses a document and an artifact (R-CUR-003), and one download ahead (R-CUR-010) | about 4 s per format, 20 s for five, spread over the shards | the stubs' existing corpora |
| Integration, once (npm) | the virtual walk (R-CUR-004), the queue's failure and retry (R-CUR-011), the examination (R-CUR-014), the requests (R-CUR-015), the retention (R-CUR-016), the catalogue's API (R-CUR-008, R-CUR-009), the permissions (R-CUR-005), the mirror (R-CUR-017) | about 20 s | the npm stub with a search route of 50 lines added |
| End-to-end, per format, in the format's own leg | what only the client shows: npm and NuGet resolve a range to the newest allowed version from a filtered document; `docker pull` of a refused tag prints `denied: …`; apt through the virtual repository sees the filtered index; `ansible-galaxy` installs the newest allowed version | about 10 s per leg | one package of the leg's corpus |
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
| (a) A gate in the signature gate's shape: asked before a fill (no upstream request), as the fourth check of the one serve decision, and as a filter of documents (chosen) | R-CUR-003 | What is stored stays scanned and visible; the refusal has a reason; examination fits; one serve decision for every refusal of content | Three places to keep in step, held by kit check 22 |
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

## What this does not cover

- [H-1720](../security/remote-curation.md#h-1720--a-search-tells-the-upstreams-operator-what-people-look-for) to
  [H-1729](../security/remote-curation.md#h-1729--a-reader-learns-what-is-being-examined): the gaps of this page's mechanisms, on its
  security page.
- The conditions of a curation policy and their wizard (SEC-009 to SEC-011), curation across sites (SEC-040, REPL-061).
- Rules of a local or a virtual repository.
- PyPI and every other format that does not exist (R-CUR-023).
