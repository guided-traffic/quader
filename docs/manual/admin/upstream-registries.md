# Upstream registries

How an administrator switches on the public registries that Quader offers to every project, sets who may read each, changes a
registry's address or stores an account for it, and what the image scanning's database repository is.

## The project upstream

Every installation has the built-in project Upstream. It holds one remote repository for each public registry Quader knows, and the
repository the image scanning reads its database from. Quader creates and maintains these repositories; an administrator decides which
are switched on, who reads them, and may give one another address or an account. Teams list them in their virtual repositories instead
of each configuring the same public registry ([Members of a virtual repository](repositories.md#members-of-a-virtual-repository)).

The project's key is `upstream`, so its repositories are named `upstream/<key>`. Nobody creates, moves or deletes a repository in it,
and the project has no members, policies, service accounts or settings. In the lists of projects it stands first with the badge
**Built-in**, its mark is an arrow pointing up, and its visibility is a dash: each registry has its own level. On the project's own page
**Repositories** the two system repositories are not among the registries: they stand below them in the closed section **System
repositories**, without **Set up client** ([Repositories](../user/repositories.md#the-project-upstream)).

| Format | Registry (repository) | Upstream |
|---|---|---|
| Ansible Galaxy | Ansible Galaxy (`upstream/galaxy`) | `https://galaxy.ansible.com/api/` |
| Debian (APT) | Debian (`upstream/debian`), Debian security updates (`upstream/debian-security`) | `https://deb.debian.org/debian/`, `https://deb.debian.org/debian-security/` |
| Debian (APT) | Ubuntu (`upstream/ubuntu`), Ubuntu security updates (`upstream/ubuntu-security`), Ubuntu ports (`upstream/ubuntu-ports`) | `https://archive.ubuntu.com/ubuntu/`, `https://security.ubuntu.com/ubuntu/`, `https://ports.ubuntu.com/ubuntu-ports/` |
| npm | npm public registry (`upstream/npmjs`) | `https://registry.npmjs.org/` |
| NuGet | nuget.org (`upstream/nuget-org`) | `https://api.nuget.org/v3/index.json` |
| OCI (Docker) | Docker Hub (`upstream/docker-hub`), Quay (`upstream/quay`), GitHub (ghcr.io) (`upstream/ghcr`), GitLab (`upstream/gitlab`) | Docker Hub, Quay, GitHub (ghcr.io), GitLab |
| OCI (Docker) | Image scanning database (`upstream/scanner-trivy`) | `https://mirror.gcr.io/`, the image `aquasec/trivy-db` only |
| ClamAV definitions | ClamAV definitions (`upstream/clamav`) | none: a local repository that Quader fills with ClamAV's signature databases, which serves the company's ClamAV installations while **ClamAV definition updates** is on |

The Debian and Ubuntu repositories come with the archive keys of their distribution, so they verify what they fetch from the start.
Every repository is addressed like any other: `https://quader.example.com/api/npm/upstream/npmjs/`,
`docker pull quader.example.com/upstream/docker-hub/library/nginx:1.27`.

`quader migrate` creates these repositories and keeps them in step with the release: a registry a new release adds appears switched
off, a changed address of a registry follows the release unless you set one yourself, and a registry a release no longer offers is
switched off, its cached content removed, and kept as **No longer offered** so that the virtual repositories that list it say why. A
system repository a release no longer uses (one of Quader's own, such as the earlier `scanner-clamav`) is deleted instead: no virtual
repository lists one, so nothing of it stays.
When a release moves a registry to another host while you stored an account for it, the account is removed, because it was entered for
the old host; the repository says so, and you enter the account again.

## The page

**Admin area › Upstream registries** lists the registries grouped by format. Each row has the registry's name with `upstream/<key>`
under it, its switch, its visibility level, **Listed by** (how many virtual repositories, in all projects, list it) and **Details**.
Below them the section **Registries of this installation** holds your organization's own registries ([below](#registries-of-this-installation)),
and the table **System repositories** holds `scanner-trivy` and `clamav`, the same name the project's page gives them.
Holders of the roles admin and repository-admin change them; security-admin and auditor read the page without the controls.

## Switching on and off

Every registry starts **switched off** and **Internal**: nothing reaches a public registry until you switch it on, and nothing a new
installation or a new release adds contacts a registry unasked. A switched-off repository answers `404 repository_disabled`, the
virtual repositories that list it skip it until it is on again, and nobody but holders of `repositories:read-config` finds it in a
list or the search; switching on restores the virtual repositories at once, with no change to them.

**One switch opens a registry to everyone signed in who is not marked external**: people and their pipelines and service accounts, in
every project, can pull through it, directly and through every virtual repository that lists it. A technical token or a CI job's token
without a scope reads every Internal repository of the installation, these included ([Visibility levels](permissions.md#visibility-levels),
[A CI service account of a project](permissions.md#a-ci-service-account-of-a-project)). If fewer should read a registry, lower its level
before you switch it on. Where the installation lets only administrators choose **Internal**, a new registry starts **Private**
instead.

- The row's switch saves at once; the status line says "Docker Hub is on.". Switching off a registry that virtual repositories list asks
  first and says how many list it, in how many projects ("3 virtual repositories in 2 projects list it."). Those virtual repositories
  keep it as a member and skip it while it is off; nothing of theirs changes. **Details** of the registry's page names them, as links,
  up to 100, and says how many others there are. Switching off records them in the event `repository.disabled`.
- **Switch all on** names every registry that is off and switches them on together, each at the level **Internal** (in a fresh
  installation they are Internal already); a registry that is already on keeps its level. It never makes a registry public. While nothing is off the button is disabled and the page says "Every
  registry is on.".
- A registry Quader no longer offers says **No longer offered** and has no switch; it serves nothing, its cached content is removed, and
  the virtual repositories that list it skip it and are told to remove it. Its row in the project's list of repositories and its own page
  in the project say **No longer offered** too, in place of **Off** and **Disabled**.
- A registry that was on before and is off again says **Switched off**; one that was never on says **Not switched on**.

Through the API: one registry with `PUT /api/v1/upstream-repositories/<key>/state` and `{"state": "on"}` or `{"state": "off"}`; all at
once with `POST /api/v1/upstream-repositories/switch-on-all`. The list counts the listing virtual repositories and their projects
(`listedBy`, `listedByProjects`), and `GET /api/v1/upstream-repositories/<key>` names them (`listedByNames`, up to 100).

`scanner-trivy` has no switch: it is in use exactly while the image scanning reads its database through it. Nor has `clamav` a state:
it is in use exactly while the malware scan keeps its signature database in it; its switch **ClamAV definition updates** decides only
whether it serves the company's ClamAV installations.

## Who reads a registry

Each registry has one level for its packages and its documentation, chosen in its row or on its page. A new registry is **Internal**,
whatever the installation's default level, and keeps the level you give it; where the installation restricts **Internal**, it is
**Private**.

| Level | Reads it, directly and through every virtual repository that lists it |
|---|---|
| Private | Administrators and whom a global permission policy names (`upstream/npmjs`, `upstream/*`) |
| Internal | Everyone signed in, service accounts and CI jobs included, except users marked external |
| Public | Anyone on the internet, also without signing in, on this installation's bandwidth and on the registry's allowance (Docker Hub's pull limit, for example) |

A team's own permissions do not reach a registry: listing it in a virtual repository opens it to nobody its level does not already admit.
Choosing **Public**, in a row or with **Set visibility of all**, asks first, and raising a level asks for a recent sign-in. A level the
installation restricts can be chosen only by whoever may change the installation's security settings
([Visibility](repositories.md#visibility)). **Set visibility of all** sets the level of every registry Quader offers, on or off.
Through the API: `PUT /api/v1/upstream-repositories/<key>/visibility` with `{"level": "internal"}`, or
`POST /api/v1/upstream-repositories/set-visibility` for every registry.

## Address and account

**Details** of a row opens the registry's page, with **Open repository** to its content and its settings.

- **Address.** The upstream URL, the further hosts and, for a Debian or Ubuntu archive, the archive keys that sign its indexes. While the
  address is Quader's the page says "This is Quader's address."; an address of your own is marked **Differs from Quader's** with Quader's
  address beside it and **Reset to Quader's address**. Use it for a registry whose address changed before a release of Quader follows
  it; a release keeps your address. **Save address** changes where every project's requests go, so check the host before you save it.
- **Account.** **Authentication** (None, Username and password, Bearer token), the username and the password or token, which is never
  shown again. An account counts the pulls of every project against that account's allowance, and whoever can read the registry pulls
  with it. **Test connection** tests the saved registry with the stored account, or what you typed before you save it. Docker Hub's page
  says "Docker Hub allows more requests with an account." while it has none: without one, Docker Hub counts the pulls of the whole site
  against its public address. An account from the Helm values shows here read-only, see below.
- **The account follows its hosts.** A stored account goes only to the hosts it was entered for. When a new address sends it to other
  hosts, the save asks for the account again or removes it, in the same change; a reset to Quader's address on other hosts removes it
  after a question. When a release of Quader moves a registry to another host, the account is removed and the row says so; enter it
  again.

Through the API: `PUT /api/v1/upstream-repositories/<key>/address` with `{"url": "https://mirror.example.com/"}` (and `extraHosts`, and
for a Debian or Ubuntu archive `upstreamKeys`, the armored archive keys); the registry then reads **overridden**, and
`DELETE …/address` brings back Quader's address. `PUT /api/v1/upstream-repositories/<key>/account` takes `authMode`, `username` and
`password`, a value (`{"value": "…"}`): a `secretRef` answers `422 secret_value_required` at `/password/secretRef`, since an account from
a file is the Helm values' (below). An address on another host answers `422 secret_value_required` at `/account/password` until the
request carries `account` (a new one, or `null` to remove it); a reset to Quader's address carries `?removeAccount=true` in that case.
Both need a recent sign-in and the registry's current `ETag` in `If-Match` (`GET /api/v1/upstream-repositories/<key>`).

## An account from the Helm values

An installation run with Helm can give a registry its account from a Kubernetes Secret that exists, for example one you encrypt with SOPS
and apply with Flux; the value never enters the chart's values or Quader's database. Only the registries of this page take an account this
way; a project's own remote repository keeps its account in its settings.

The Secret, a manifest of its own:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: quader-upstream-docker-hub        # example
  namespace: quader                       # example: the release's namespace
type: Opaque
stringData:
  username: REPLACE_WITH_DOCKER_HUB_USERNAME
  token: REPLACE_WITH_DOCKER_HUB_ACCESS_TOKEN
```

The values name it by the registry's key:

```yaml
secrets:
  upstreamAccounts:
    docker-hub:                           # the key the registry's page shows
      existingSecret: quader-upstream-docker-hub
      authMode: basic                     # default; bearer sends a token alone (npm)
      usernameKey: username               # default; basic only
      passwordKey: token                  # default password
```

For Docker Hub use a personal access token with the scope **Public Repo Read-only** as the password: whoever can read the registry pulls
with it. GitHub (ghcr.io) takes a username and a classic token with `read:packages`, GitLab a username and a token with `read_registry`
(or a deploy token), Quay a robot account; npm takes `authMode: bearer` with a read-only token.

- **What happens at the upgrade.** The migration Job applies the account: an account an administrator had stored on the page is removed,
  and the change is in the audit log with the actor `system:configuration`. A key that names no registry here stops the upgrade with the
  key named.
- **The page** shows "From the Helm values: Secret quader-upstream-docker-hub. Change or remove it there.", the authentication, the state
  of the Secret's files on the server that answered, the last download and the last failed download, and **Test connection**. Saving or
  removing the account there, and an address that sends it to other hosts, are refused.
- **A Secret that does not exist yet** starts every pod. Every request to that registry fails with `502 upstream_secret_unavailable` until
  the Secret exists with its keys; the page says "Missing on this server" with a warning, and the log names the file's path. A wrong token
  is refused by the registry: **Test connection** says "The upstream answered and refused the credentials."
- **Rotation, without a restart:** create the new token at the registry, update the Secret, wait three minutes (the pods receive a
  changed Secret within two minutes and read it again after ten seconds), then revoke the old token. Adding or removing an entry rolls the
  pods once.
- **One allowance.** Every installation that uses the same Docker Hub user shares that user's allowance.

Without Helm, `repositories.upstream_accounts.<key>` of the configuration file takes `auth_mode`, `username_file`, `password_file` and
`secret_name` (empty) directly; the files are read at every use.

## What else you may change

The registry's settings page, under **Open repository** (the repository document,
`PATCH /api/v1/projects/upstream/repositories/<key>`), keeps editable what narrows what it serves:
**Offline** (`remote.availability.mode`) and the administrator's notes. The address, the account, the level and the format's settings
show there read-only with "Managed in Admin area › Upstream registries.". The vulnerability settings and who may fill the cache
(`remote.cacheFillAction`) are taken too, through the API. Every other setting is Quader's and answers `422 managed_by_product` at the
setting; replacing the document, deleting or moving a registry, creating a repository in the project, and giving the project members,
policies, service accounts or CI mappings answer `409 managed_by_product`. The download limits apply to the registries as to every
repository ([Download limits and usage](download-limits.md)).

## The image scanning's database

**System repositories** under the list holds `scanner-trivy`, **Image scanning database**: the repository through which the image scanning
reads the vulnerability database of its analyser, the image `aquasec/trivy-db`. It has no switch and its level is always Private: it is
**In use by the image scanning** while the scanning is on and reads its database through it, else **Not in use**. It answers no client,
administrators included: no screen offers **Set up client** for it, nor its address, a download or a pull command, and its page
`…/setup`, opened by its address, says so instead of showing snippets ([Where the system repositories show](#where-the-system-repositories-show)).
Its page lets you change the address and store
an account like any registry; the address must serve the image `aquasec/trivy-db`. For a registry that publishes the database under
another name, set the image scanning's database setting instead ([Advisory sources](vulnerability-sources.md#container-images)).

## ClamAV's definitions

Below it stands `clamav`, **ClamAV definitions**: the repository of the format ClamAV definitions in which the malware scan keeps ClamAV's
signature databases, one package per database (`main`, `daily`, `bytecode`) and one version per container (`daily/28190/daily.cvd`, with
its detached signature `daily-28190.cvd.sign` beside it). Only Quader writes it; nobody uploads to it or deletes a file of it by hand
(`409 managed_by_product`, also for an administrator). It has no state, no address and no account: it is **In use by the malware scan**
while the scanning is on and `malware.database.repository` names it (the default), else **Not in use**. No repository of the format is
created in a project.

**ClamAV definition updates** is its switch, off by default: in its row of **System repositories** (column **Clients**) and as the first
section of its page. While it is off, the repository answers no client, administrators included, and its level is Private. Switched on
(the switch asks first and names the address; through the API `PUT /api/v1/upstream-repositories/clamav/clients` with `{"enabled": true}`,
which asks for a recent sign-in), it serves the set of definitions Quader's own scanners use to every ClamAV of the company at
`https://<your installation>/api/clamav/upstream/clamav`, at the level Public unless you name a lower one, or Internal where the
installation allows no Public level, which the section then says in a warning. While it is on, the section shows the address of the
endpoint with its copy button, the level and, in the fold **Visibility**, what a ClamAV needs at each level (a token or a client
certificate at Internal, no certificate at Private) and, while an endpoint admits client certificates, "With a client certificate:" with
that endpoint's address; while none does, the line says that client certificates need an endpoint your operator sets up and links to
[Client certificates for ClamAV installations](endpoints.md#client-certificates-for-clamav-installations). Then the one line a ClamAV
installation puts into `freshclam.conf` (`PrivateMirror`): the plain one at Public, the one with a token below it; behind
**Authenticate** the token's form and, while an endpoint admits certificates, the certificate's (the line with that endpoint's address and
the systemd drop-in with `FRESHCLAM_CLIENT_CERT`, `FRESHCLAM_CLIENT_KEY` and `CURL_CA_BUNDLE`); behind **Without freshclam** the script
that downloads the three databases with `curl` for an installation that runs no `freshclam`. **The full guide on Set up client** leads to
the repository's setup page. The section **Updater** below it
lists the pods of `malware-updater-freshclam` with their state, freshclam's version and the definitions each holds with the age of its
newest `daily`, or says why there is none; **Versions** leads to the repository's databases. Switched
off again, the level is Private and every request answers `404`.

The repository's own page lists its databases, each with the version in use, its build time and its age; a `daily` older than 7 days
carries **Outdated**, because ClamAV warns about it. `main` and `bytecode` are rebuilt rarely and ClamAV does not warn about their age,
so they carry no badge. Above them stand the set in use and, where it differs, the set before it. A
database's page lists its versions with their state (**In use**, **Previous**, **Superseded**, **Files removed**), and a version's page
shows every field of its header (build time, signatures, functionality level, builder, how it was verified, with the two MD5 sums in a
fold), its detached signature and the files of its container. An installation without internet names a repository of its own as the source instead
([Monitoring and operations](operations.md#malware-scanning)).

## Where the system repositories show

Both system repositories answer `404` to every client, administrators included, while they answer no client (`scanner-trivy` always,
`clamav` while ClamAV definition updates is off), so no screen offers what would fail. While the switch is on, `clamav` is offered like
any repository you read: **Set up client**, its address and **Download**; it keeps the badge **System** and nobody deletes its files.
While closed:

- **Repositories** under **Your work** (the repositories of every project) lists them with their project and the badge **System**
  after the key, without **Set up client**; the row's menu keeps **Settings**, **Files** and, for `scanner-trivy`,
  **Vulnerabilities**.
- The search finds them, in the suggestions under the search field and on the tab **Repositories** of the results, with the same badge.
- On their own pages there is no **Set up client**, no address for downloads by path on the tab **Details**, no **Pull** beside an image or a tag
  and no copy button in a folder. The page of one of their files has no **Download** and no download address; one sentence says which of
  Quader's work reads it. **Details** keeps the request of the file through the API with curl, which the API answers. A package's install
  panel never offers them, and a version only they hold says why instead of showing a command.
- They are not offered where you pick a repository to work with: the guides, the repository of an exposure rule, the repository filters
  of the security dashboard and **Explain for a repository**.
- **Recently opened** in the side bar lists them like any repository you opened.

## Registries of this installation

Your organization's own registries (a company Harbor, a Verdaccio, a GitLab registry of your own instance) are kept in the section
**Registries of this installation** of the same page, between the public registries and the system repositories. The form of a new
remote repository offers each one as a button in every project, after the format's well-known registries, so a team presses it instead
of typing the address and its token service again.

- **Add registry** asks for the format (every format with remote repositories), the name of the button, the upstream URL and the
  further hosts. Name the registry's token service there and tick **Receives the credential**, so that the credential a project enters
  reaches it; a container registry whose login is on another host needs this.
- A preset holds no credential: there is no field for one. Each project enters its own account in its remote repository.
- At most 10 registries per format. A well-known registry's address is refused, because it has its button already.
- A preset is only a value the form puts into its fields. A remote repository created from it keeps its address and hosts when you edit
  or remove the preset; nothing follows a preset afterwards.
- `GET` and `PUT /api/v1/settings/registry-presets` read and replace the whole list (the `PUT` with `If-Match`); a change is audited as
  `settings.registry_presets.changed`.

## Permissions

Reading the registries needs `repositories:read-config` (the roles `admin`, `security-admin`, `auditor`, `repository-admin`); changing
them `repositories:write-config` (`admin`, `repository-admin`); a level the installation restricts needs `security-settings:write`. The
project **Upstream** has no Owner and is listed only to holders of `projects:read` and to people who read one of its registries; it
stands first in every list of projects. A project's Owner who adds a registry to a virtual repository sees each registry of the format
with its state and level (`GET /api/v1/formats/<format>/upstream-repositories`), never its address or account. The registries of this
installation are kept with `repositories:write-config` and read with `repositories:read-config`; whoever may create a remote repository
in a project (its Owner) sees the presets of one format as buttons in the form, and no one else.
