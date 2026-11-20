# Upstream registries

Every installation has the built-in project Upstream. It holds one remote repository for each public registry Quader knows, and the
repository the image scanning reads its database from. Quader maintains these repositories; an administrator decides which are switched
on, who reads them, and may give one another address or an account. Teams list them in their virtual repositories instead of each
configuring the same public registry.

The project's key is `upstream`, so its repositories are named `upstream/<key>`.

## The repositories

| Format | Repository | Upstream |
|---|---|---|
| Ansible Galaxy | `upstream/galaxy` | `https://galaxy.ansible.com/api/` |
| Debian (APT) | `upstream/debian`, `upstream/debian-security` | `https://deb.debian.org/debian/`, `https://deb.debian.org/debian-security/` |
| Debian (APT) | `upstream/ubuntu`, `upstream/ubuntu-security`, `upstream/ubuntu-ports` | `https://archive.ubuntu.com/ubuntu/`, `https://security.ubuntu.com/ubuntu/`, `https://ports.ubuntu.com/ubuntu-ports/` |
| npm | `upstream/npmjs` | `https://registry.npmjs.org/` |
| NuGet | `upstream/nuget-org` | `https://api.nuget.org/v3/index.json` |
| OCI (Docker) | `upstream/docker-hub`, `upstream/quay`, `upstream/ghcr`, `upstream/gitlab` | Docker Hub, Quay, GitHub (ghcr.io), GitLab |
| OCI (Docker) | `upstream/scanner-trivy` | `https://mirror.gcr.io/`, the image `aquasec/trivy-db` only |

The Debian and Ubuntu repositories come with the archive keys of their distribution, so they verify what they fetch from the start.
Every repository is addressed like any other: `https://quader.example.com/api/npm/upstream/npmjs/`,
`docker pull quader.example.com/upstream/docker-hub/library/nginx:1.27`.

`quader migrate` creates these repositories and keeps them in step with the release: a registry a new release adds appears switched
off, a changed address of a registry follows the release unless you set one yourself, and a registry a release no longer offers is
switched off, its cached content removed, and kept as **No longer offered** so that the virtual repositories that list it say why.
When a release moves a registry to another host while you stored an account for it, the account is removed, because it was entered for
the old host; the repository says so, and you enter the account again.

## Switching a registry on

Every registry starts **switched off** and **Internal**: nothing reaches a public registry until you switch it on. A switched-off
repository answers `404 repository_disabled`, the virtual repositories that list it skip it until it is on again, and nobody but holders
of `repositories:read-config` finds it in a list or the search; switching on restores the virtual repositories at once, with no change to
them. Before you switch one off, the list of registries says how many virtual repositories in how many projects list it (`listedBy`,
`listedByProjects`), and `GET /api/v1/upstream-repositories/<key>` names them (`listedByNames`, up to 100); switching off records them in
the event `repository.disabled`.

**One switch opens a registry to everyone signed in who is not marked external**: people and their pipelines and service accounts, in
every project, can pull through it, directly and through every virtual repository that lists it. A technical token or a CI job's token
without a scope reads every Internal repository of the installation, these included ([Visibility levels](permissions.md#visibility-levels),
[A CI service account of a project](permissions.md#a-ci-service-account-of-a-project)). If fewer should read a registry, lower its level
before you switch it on. Where the installation lets only administrators choose **Internal**, a new registry starts **Private**
instead.

- One registry: `PUT /api/v1/upstream-repositories/<key>/state` with `{"state": "on"}` or `{"state": "off"}`.
- All at once: `POST /api/v1/upstream-repositories/switch-on-all` switches on every registry that is off (and still offered) and makes
  it **Internal** (in a fresh installation they are Internal already); a registry that is already on keeps its level. It never makes
  anything public.

`scanner-trivy` has no switch: it is in use exactly while the image scanning reads its database through it.

## Who reads a registry

A registry has one level for its packages and its documentation: `PUT /api/v1/upstream-repositories/<key>/visibility` with
`{"level": "internal"}`, or `POST /api/v1/upstream-repositories/set-visibility` for every registry. A new registry is **Internal**,
whatever the installation's default level, and keeps the level you give it; where the installation restricts **Internal**, it is
**Private**.

| Level | Reads it, directly and through every virtual repository that lists it |
|---|---|
| Private | administrators and whom a global permission policy names (`upstream/npmjs`, `upstream/*`) |
| Internal | everyone signed in who is not marked external: people, their CI jobs and service accounts |
| Public | anyone, also without signing in, on your bandwidth and on the registry's pull allowance |

A team's own permissions do not reach a registry: listing it in a virtual repository opens it to nobody its level does not already admit.
Raising a level asks for a recent sign-in. `scanner-trivy` is always **Private** and answers no client, administrators included.

## Address and account

- **Another address**, for a registry whose address changed before a release of Quader follows it:
  `PUT /api/v1/upstream-repositories/<key>/address` with `{"url": "https://mirror.example.com/"}` (and `extraHosts`, and for a Debian or
  Ubuntu archive `upstreamKeys`, the armored archive keys). The registry then reads **overridden**, and a release keeps your address.
  `DELETE …/address` brings back Quader's address.
- **An account**, to pull with your own allowance: `PUT /api/v1/upstream-repositories/<key>/account` with `authMode`, `username` and
  `password` (a value, or a `secretRef`). Everyone who reads the registry pulls with this account.
- A stored account goes only to the hosts it was entered for: an address on another host answers `422 secret_value_required` at
  `/account/password` until the request carries `account` (a new one, or `null` to remove it); a reset to Quader's address carries
  `?removeAccount=true` in that case.

Both need a recent sign-in and the registry's current `ETag` in `If-Match` (`GET /api/v1/upstream-repositories/<key>`).

## What else you may change

The registry's settings page (the repository document, `PATCH /api/v1/projects/upstream/repositories/<key>`) takes the settings that
narrow what it serves: the administrator's notes, include and exclude patterns, the vulnerability settings, offline
(`remote.availability.mode`) and who may fill the cache (`remote.cacheFillAction`). Every other setting is Quader's and answers
`422 managed_by_product` at the setting; replacing the document, deleting or moving a registry, creating a repository in the project,
and giving the project members, policies, service accounts or CI mappings answer `409 managed_by_product`. The download limits apply to
the registries as to every repository ([Download limits and usage](download-limits.md)).

## Permissions

Reading the registries needs `repositories:read-config` (the roles `admin`, `security-admin`, `auditor`, `repository-admin`); changing them
`repositories:write-config` (`admin`, `repository-admin`); a level the installation restricts needs `security-settings:write`. The project
**Upstream** has no Owner and is listed only to holders of `projects:read` and to people who read one of its registries; it stands first
in every list of projects. A project's Owner who adds a registry to a virtual repository sees each registry of the format with its state
and level (`GET /api/v1/formats/<format>/upstream-repositories`), never its address or account.
