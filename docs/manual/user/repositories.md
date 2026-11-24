# Repositories

Everything Quader stores lives in a repository, and every repository belongs to a project and has a key, a format and a class. This page explains the local, remote and virtual classes from a user's point of view, how projects and keys become addresses, which formats this version serves, what you see depending on your permissions, and how the content page of a repository and its setup help you connect a client.

## Repository classes

A repository is local, remote or virtual. An administrator chooses the class when creating the repository; it never changes.

| Class | What it holds | What you do with it |
|---|---|---|
| **Local** | The packages and files your teams publish or upload | Read, and publish or upload where you hold the permission |
| **Remote** | A cache of an upstream registry, filled on demand | Read only; Quader fetches from the upstream for you |
| **Virtual** | Nothing of its own: it combines several repositories, its members, under one address | Read through one address; publishing goes to one local member |

### Local repositories

A local repository keeps what is published to it, and a publish is visible as soon as it has returned. For npm, a published version never changes: publishing the same version again is refused, and an unpublished version number stays blocked for good. NuGet is the same: a version never changes, a deleted version number stays blocked for good, and pushing the identical package again is accepted and changes nothing, so a push that a client repeats after a lost answer succeeds. A Debian package file is stored once per package, version and architecture: other bytes under the same identity are refused, and the same bytes again only add suites and components; the signed index that APT reads follows a few seconds after the upload. For generic files, uploading other bytes to an existing path replaces the file, but only for people who hold the overwrite permission.

### Remote repositories

A remote repository is a pull-through cache of an upstream registry; for npm the default upstream is `https://registry.npmjs.org/`. The first request for a package fetches it from the upstream and stores it; later requests are answered from Quader. While the upstream is down, Quader serves what it has cached. A version that the upstream withdraws is no longer served, even from the cache, unless an administrator chose otherwise. You cannot publish to a remote repository.

For NuGet the default upstream is `https://api.nuget.org/v3/index.json`: Quader reads the service index of the feed and takes the package files and documents from the addresses it names. A NuGet remote checks every package it caches: the ID and version the package declares must be the ones the request named, and an archive whose ID and version cannot be read is refused the same way: the answer is `502` `upstream_package_mismatch` and nothing is stored. A version that disappears from the upstream's lists stays downloadable from the cache by its exact address; an ID the upstream no longer has is not served at all, though its cached packages stay stored and the cache view (`<key>~cache`) lists them.

For Debian (APT) the default upstream is `https://deb.debian.org/debian/`. A Debian remote passes the archive's signed release files through unchanged, so your APT client verifies the archive's own signature; [Debian packages with APT](apt-packages.md#through-a-remote-repository) says which key file to use.

### Virtual repositories

A virtual repository is one address for several npm repositories, several NuGet repositories, several OCI repositories or several Debian repositories, of its own project, in an order an administrator sets, for example an internal local repository followed by a remote one. Point your package manager at a virtual repository and you get internal and public packages through one address and one token. Keep in mind:

- You need the read permission on the members too. A member you may not read is left out without notice.
- Publishing through a virtual repository goes to its deployment target, a local member. A virtual repository without one refuses every publish.
- For NuGet, the version list and the package documents of an ID are the union of the versions of its members. A version that two members hold comes from the first one in the order, and so does the package you download; a search lists a package once, with the versions of all members.
- A name that a local member has claimed is served only from that member, so a public package with the same name and a higher version never replaces an internal one.
- A member may be a public registry Quader offers to every project, `upstream/npmjs` or `upstream/docker-hub`, of the built-in project **Upstream**, which stands first in the list of projects with an arrow as its mark. Until an administrator switches it on, the virtual repository skips it, and a package only it would have served answers not found with a message that says so.

## The project Upstream

The built-in project **Upstream** holds the public registries Quader offers to every project, such as `upstream/docker-hub` and
`upstream/npmjs`. You see the registries an administrator switched on whose level admits you, and you use them like any remote
repository: **Set up client** in a registry's row shows how. The line under the title of its page says what the project is, in the
language of the page.

Below the registries, set apart and closed, **System repositories** with their number holds the two repositories Quader keeps for its
own work: `scanner-trivy`, the database of the image scanning, and `clamav`, the signature database of the malware scan. Only
Quader's scanners read them, and their addresses answer `404` to every client, so neither their rows nor their pages offer **Set up
client**, and nothing can be uploaded to them or downloaded from them. They are always Private: the section is there only if you may read them, normally as
an administrator. Select its heading to open it. The filter above the list covers it too: a text or a filter that matches one of them
opens it and its number counts the matches; one that matches none of them leaves it closed, at 0.

## Projects and repository keys

Every repository belongs to exactly one project, for example the project `web` of a team. The project's key and the repository's key together name the repository: `web/npm-internal`. The key is unique within its project only, so the projects `web` and `mobile` can each have a repository `npm-remote`, and the two are different repositories.

Keys are set when the repository is created and cannot be renamed:

- 2 to 64 characters: lower-case letters and digits, separated by one `.`, one `_`, two `_` or any number of `-`
- it starts and ends with a letter or a digit, so `docker-local` and `docker.hub` are accepted and `docker-` and `a.-b` are not

Your client reaches a repository at an address built from the installation's address, the project key and the repository key:

| Format | Address for your package manager | Address for raw files |
|---|---|---|
| npm | `https://quader.example.com/api/npm/<project>/<key>/` | `https://quader.example.com/files/<project>/<key>/` |
| NuGet | `https://quader.example.com/api/nuget/<project>/<key>/v3/index.json` | `https://quader.example.com/files/<project>/<key>/` |
| Generic | `https://quader.example.com/files/<project>/<key>/` | the same |
| OCI (Docker) | `quader.example.com/<project>/<key>/<image>:<tag>`, an image reference without a scheme | `https://quader.example.com/files/<project>/<key>/` (download and delete; uploads go through the registry) |
| Debian (APT) | `https://quader.example.com/api/deb/<project>/<key>/`, with a suite in the source line | `https://quader.example.com/files/<project>/<key>/` (download and delete; uploads go to `upload/<file>` of the APT address) |
| Ansible Galaxy | `https://quader.example.com/api/ansible/<project>/<key>/`, the `url` of a server in `ansible.cfg` | none: collections are published with `ansible-galaxy collection publish` |

**Set up client** on the page of a repository shows its address for your package manager, with a copy button, and the configuration of your client that uses it, so you do not have to build it yourself; the address for downloads by path is on the tab **Details** of the repository's page. A repository that an administrator moves to another project changes its address: the old address answers `404`, and your client configuration needs the new one.

## Formats in this version

| Format | Classes | Clients the guides and setup cover | Where its content appears in the UI |
|---|---|---|---|
| npm | Local, remote, virtual | npm, pnpm, Yarn Berry (2 and later), Yarn Classic (1.x) | **Packages**, and the repository's page with its **Files** |
| NuGet | Local, remote, virtual | .NET CLI (dotnet), NuGet CLI (nuget.exe), Visual Studio | **Packages**, and the repository's page with its **Files** |
| OCI (Docker) | Local, remote, virtual | Docker CLI (with Buildx), Podman; Kubernetes through an image pull secret | **Packages** (an image name is a package, a tag a version), and the repository's page with its **Files** |
| Debian (APT) | Local, remote, virtual | APT (apt, apt-get) on Debian and Ubuntu | **Packages** (a binary package name is a package), and the repository's page with its **Files** |
| Ansible Galaxy | Local, remote, virtual | ansible-galaxy (ansible-core 2.17 and 2.21) | **Packages** (a collection is a package), and the repository's page with its **Files** |
| Generic | Local | curl | The repository's page only: generic files have no package names, so they are not listed under **Packages** |

Generic repositories store files of any kind at paths you choose: installers, archives, documents, build outputs. OCI repositories hold container images and other OCI artifacts; [Container images with Docker and Podman](container-images.md) explains them. Debian repositories are APT repositories with signed indexes; [Debian packages with APT](apt-packages.md) explains them. Other formats are not built yet; see [Known limitations](../admin/limitations.md).

## What you see depends on your permissions

Quader shows you only what you may read. A repository you may not read is not listed anywhere, and its addresses answer `404`, exactly as if it did not exist (or `403`, where an administrator chose not to hide repositories).

| What you hold | What you see and can do |
|---|---|
| A role in the repository's project | **Reader** reads every repository of the project, **Developer** also publishes and uploads, **Maintainer** and **Owner** also replace, delete and manage; [Projects](../admin/projects.md#members-and-roles) explains the roles |
| Read on a repository | It appears in the project's **Repositories** and, for npm, NuGet, OCI and Debian, under **Packages**; you can open its page and its files, download, and install from it |
| Deploy, overwrite or delete | Upload or publish, replace, and delete, on the paths and packages your permission names |
| The permission to read repository configuration | Every repository, in its project and under **Repositories** of **Your work**, and the settings of every repository |
| Nothing, as an anonymous reader | Only the public projects and repositories, and nothing you can change |
| Only the level **Internal** or **Public** | A project and its repositories whose level admits you: **Internal** admits everyone signed in, service accounts and CI jobs included, except an **External user**, **Public** everyone. The projects start page lists your own projects first, then these, without a role; the level's icon (a lock, a shield, a globe) stands before the name |

An administrator flags a user as external, for example a contractor: an external user sees only the projects it is a member of, what a permission grants it and what is public, and never what is only internal. An administrator can also keep a level to administrators: as the Owner of a project you then see that option disabled, with "Only an administrator of Quader can choose this level." under it. A repository that has such a level keeps it; you may lower it, but only an administrator can choose it again. A repository's wiki (the README and the documentation) has the repository's level unless an Owner gave it a lower one under **Wiki visibility** in the repository's settings, so you can install a package and find no README tab; that is the wiki's level, not an error.

Permissions can be narrower than a whole repository: a permission may cover only some paths or package names, and you then see only those. Ask an Owner of the repository's project if something you need is missing.

## The content of a repository

Open a project and you land on its **Repositories**. Select a repository to see what it holds, on the level you think in: images and tags, packages and versions, or files. You get there from the project's **Repositories**, from **Admin area › Repositories** (every project, each row naming its project), and with **Find a repository** in the top bar: Enter opens **Search results**, a page of the main area that lists the matching repositories of every project you may read.

The page is titled with the repository's key and says what the repository is. At the top right:

- **Set up client** opens the page of this repository that shows the address your package manager needs, with its copy button, and how to configure your client, with the commands and their explanation; you only pick your client where its package type has several. A disabled repository has no **Set up client**, because clients cannot use it, and neither has a system repository of the project **Upstream** ([The project Upstream](#the-project-upstream)). [Connecting a package client](client-setup.md) describes the page.
- **Settings** opens the repository's settings, if you may read them.
- **More actions** holds **Files**, the raw files of a repository of a package format ([Browsing and transferring files](files.md)), and **Vulnerabilities**.

Below them the page lists the **Images**, **Packages** or **Files** of the repository, depending on the format, with a filter. Above the list stand the page's tabs: the list itself; for a remote repository **Upstream** and **Downloads** ([Remote repositories](remote-curation.md)); and, always last, **Details**, with its own address (`…/repositories/<key>/details`). **Details** holds the repository's other addresses, each named, with a copy button (**Address for downloads by path (curl, wget)**), under **Caches** the upstream a remote repository caches, and under **Resolves from** the members a virtual repository resolves from. A repository that has none of these for you has no tab **Details**, and a page with only its list shows no tabs.

**Pull**, **Install** or **Download** at the end of a version's row opens the commands of that version, one section per client of the package type (for container images **Docker CLI (with Buildx)** and **Podman**, for npm **npm**, **pnpm**, **Yarn Berry (2 and later)** and **Yarn Classic (1.x)**), each a field with this installation's address and a copy button. The same button in an item's row holds the commands of its latest version. Escape or a click outside closes it. The commands carry no credentials: **Set up client** configures your client once.

Depending on the format, the content shows:

| Format | What is listed | Open a row to see |
|---|---|---|
| **OCI (Docker)** | Images, each with its latest tag, the number of tags, the [grade](vulnerabilities.md#grades) of its latest tag, and when it was last pushed. A remote repository lists the tags clients have pulled through it and, in mirror mode, the tags a pass has fetched | The tags with digest, size (marked **≥** when it counts only the platforms fetched so far, a dash when it is not known yet), pushed time (fetched time in a remote repository, the time its cached copy of the tag was stored) and grade, and **Pull**, which opens the `docker pull` and `podman pull` commands of the tag, with this installation's address |
| **npm, NuGet, Debian, Ansible** | Packages, each with its latest version, the number of versions, the [grade](vulnerabilities.md#grades) of its latest version where the format is scanned (npm and NuGet today) and downloads. A remote repository lists the versions whose file a client fetched through it (the tarball, the package, the package file, the collection archive) and, in mirror mode, those a pass fetched | The versions with size, published time and grade, and **Install**, which opens the install commands of the version for each client |
| **Generic** | The files of the repository, folder by folder | Selecting a folder opens it, selecting a file opens its page with **Download** and its details; the copy button copies its download address. A file of a disabled repository has no **Download** and no address: one sentence says that nobody can download it until the repository is enabled again. Uploading, new folders and deleting are on the same pages; see [Browsing and transferring files](files.md) |

An empty repository says under its list what fills it: if you may publish to it, publish with your client, and **Set up client**
at the top shows how; otherwise, who fills it. A remote repository fills as clients fetch through it.

Select a package or image name to open the [package page](packages.md), the table of its versions and the README; a version opens its own page. A virtual repository lists what it resolves through its members, which are named above the list. A remote repository lists what has been fetched through it, marked in a note.

The **Vulnerabilities** column shows a shield with the grade's letter and beside it the number of open findings and how many of them
have a fix, for example the shield **E** and **1,145 findings · 112 fixable**, as the versions table of a package does; point at it
for the rule of the letter and every count. Select its heading to sort by grade: once ascending, from N/A and S to E, and again for the worst first. Sorting by
grade loads up to 1,000 entries first; in a larger repository the list says that it sorted only those. The column is not shown when
you are signed out, or when the installation does not scan.

Debian packages are not scanned for vulnerabilities yet; the page says so and shows no vulnerability column for them.

### The upstream and the downloads of a remote repository

When you are signed in, a remote repository's page has three tabs under its title: its content (**Packages**, **Images** or **Files**),
**Upstream** and **Downloads**. **Upstream** shows what the registry the repository caches offers and what this repository delivers of
it; there you search the upstream, ask for a package the repository does not deliver, and, as a Maintainer, Owner or administrator,
allow or block it and download it into the cache before a client asks. **Downloads** shows the repository's download queue with the
failures in their own words. [Curating a remote repository](remote-curation.md) explains both tabs, the delivery rules and what each
client prints when the rules refuse it.

## Repository settings

**Settings** on the content page opens the settings of the repository at `…/settings`. Editing it needs a further permission and is described in [Managing repositories](../admin/repositories.md). If you may use the repository but not read its configuration, the page says **You may use this repository but not read or change its configuration.** and shows the facts instead of the form.

The settings page is one column: the key and the description at the top, then **Visibility** and, for a remote or virtual repository, its **Upstream** or **Members**, all open; below them closed sections such as **Access details**, **Malware scan** (whether only checked artifacts can be downloaded from the
repository, while the installation scans for malware; see [Monitoring and operations](../admin/operations.md#malware-scanning)),
**Download links** (whether large downloads may go directly from the object store by a short-lived link; your clients must then reach the
object store's address too, also through a proxy or a firewall; see [Download links](../admin/download-links.md)), the
format's settings (for example **npm settings**), **Details** and **API**. How a client is configured is not a setting: it is **Set up client** at the top of the repository's page.

## Set up client and the guides

**Set up client** at the top of a repository's page opens the configuration of your client for this repository, with this installation's addresses, and below it the guide that explains what the repository's class allows, how the client signs in, installs and publishes, and what its errors mean. A guide never contains a token: snippets read it from an environment variable such as `QUADER_TOKEN`, and the page for an anonymous reader uses no credential at all.

**Guides** in the start side bar and in the user menu shows the package types as tiles; a tile lists the repositories of that type you may use, each with **Set up client** (a system repository of **Upstream**, which no client can use, is not among them), and the guide of each kind of repository for when you have none yet. See [Connecting a package client](client-setup.md).
