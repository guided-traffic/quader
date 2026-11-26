# Packages

The Packages screen lists the packages of all repositories you may read, with filters, sorting and saved views. An npm or NuGet package and an Ansible collection open on their newest version's documentation, from where you switch to older versions; a container image and a Debian package open on the table of their versions. Each version has a page of its own with its documentation, its vulnerabilities, its files, its metadata and, behind Install or Pull, a ready-made command. This page explains each part.

## What the list shows

Select **Packages** in the side bar of a project. The list holds the npm packages of every repository of the project that you may read; generic repositories have no package names, so their files are on the repository's own page instead (see [Browsing and transferring files](files.md)).

A package appears once, however many repositories hold it. Everything in its row is counted from the versions you may see: a version you may not read changes neither the latest version nor the counts. A remote repository contributes only the versions it has already cached.

| Column | Meaning |
|---|---|
| **Package** | The package name, which opens the package, and under it the description of the latest version |
| **Format** | The package format |
| **Latest** | The highest stable version you may see. A pre-release is shown only while no stable version exists |
| **Updated** | When the latest version you may see was published, as a relative time ("4 minutes ago"); point at it for the exact time |

When a version was published, how large it is, its vulnerabilities and when a client last downloaded it are in the table of the
package's versions.

The **?** next to some column headings explains the column. The list shows one page at a time; **Load more** adds the next page. When nothing is published yet, the list says **No package has been published yet.** and offers **Set up a client to publish the first package**. **Set up a client** at the top of the screen opens the client setup at any time.

## Filter the list

Type into **Filter packages** above the list to keep the packages whose name or description contains the text, ignoring case; the
list follows a moment after you stop typing, or at once with Enter.

The other filters are behind **Filters**, which opens them in a panel that slides in from the right over the page. The button shows
how many filters are set.

1. Select **Filters**.
2. Set the filters you need; they stand one under the other:

   | Filter | What it keeps |
   |---|---|
   | **Package type** | Packages of the ticked formats |
   | **Repository** | Packages with a version in one of the ticked repositories; a virtual repository stands for its members |
   | **Keyword** | Packages with a version that lists exactly this keyword |
   | **State** | **Deprecated only**: packages whose latest version is deprecated (npm, NuGet, Ansible Galaxy, Helm charts) |
   | **Created** | Packages whose first version falls into **Last 7 days**, **Last 30 days**, **Last 90 days** or a **Custom period** from **From** to **To (included)** |

3. Select **Apply filters**. The panel closes.

Each filter that is set appears as a chip beside **Filters**, with its value; the cross on a chip removes that filter. Where the chips
do not fit, the last place says **+2 more**, which opens the panel with every filter. **Clear all** in the panel removes every filter
of the panel and keeps the text of **Filter packages**; **Clear all filters**, when no package matches, removes the text too. The
close button or Escape closes the panel without applying what you changed.

## Sort the list

Select the heading **Package** or **Updated** to sort by it; select it again to reverse the order. The list is sorted by name unless you choose otherwise. The other columns cannot be sorted.

Filters, sort and the open tab are part of the page address, so you can bookmark a filtered list or send it to a colleague. The colleague sees the packages that they may read.

To find a package in any of your projects, type a part of its name into the search field in the top bar: the
[search](search.md) suggests it with the project and repository that hold it, and Enter lists every match.

## Recently opened and saved views

When you are signed in, two more tabs appear next to **All packages**:

- **Recently opened** lists the last 10 packages you opened.
- **Saved views** keeps up to 8 combinations of filters and sort. To save one, set the filters and the sort, open **Saved views**, enter a **Name of the view** and select **Save the current filters and sort**. Each saved view opens with its name; the menu at the end of its row offers **Rename**, **Delete** and **Make default**: the list then opens with that view whenever you open **Packages** without filters. **Remove default** undoes that.

Both are kept in this browser only, separately for each user. They are not available where the browser blocks site storage, and they do not follow you to another browser.

## A package's page

Select a package name to open its page. The address of a package is `/projects/<project>/packages/<format>/<name>`, for example
`https://quader.example.com/ui/en/projects/web/packages/npm/@team-a/ui`.

**npm, NuGet and Ansible Galaxy: the newest version's documentation.** Their packages carry documentation, so the name opens the page
of the newest version (the highest that is no pre-release; a package of pre-releases only opens its highest) on its documentation: the
**README** of an npm or a NuGet package, the tab **Documentation** of a collection, as Ansible Galaxy does. In the head, the button
with the version (for example **5.4.1**) lists the ten newest versions, the one you read marked, the newest marked **Latest**, and
under them **All 30 versions**, the table of every version below. A version you choose opens on the tab you were reading, and for a collection on the same page of its documentation. A version
without a README opens on its next tab, and its tab **README** says that there is none to show; a collection without documentation, such
as a role, opens on the tab it has. The table of every version has the address `…/<name>/-/versions`.

**Container images and Debian packages: the table of their versions.** Their name opens the table directly.

## The table of a package's versions

The table shows the name, the format, the description of the latest version, how many versions you may see and the latest one, then
every version you may see, the highest version first:

| Column | Meaning |
|---|---|
| **Version** | The version, which opens its page. An npm tag that points at it, such as `latest` or `next`, stands beside it as a badge, at most three and then "+N more" (see [npm tags](remote-curation.md#npm-tags)), and so does **Pre-release** for a pre-release. For a container image the column is **Tag**, and a row is an image (see below) |
| **Size** | The size of the version's files; for an image, the size of the image. **≥** before the size of an image of several platforms counts only the platforms clients have fetched so far through a remote repository; a dash means the size is not known yet, because no platform was fetched. Point at the size for what it counts |
| **Vulnerabilities** | The grade of the version, the number of its open findings and how many of them have a fix, for example the shield **E** and **1,145 findings · 112 fixable** (see [Vulnerabilities](vulnerabilities.md#grades)). Only where the versions are checked for vulnerabilities and you are signed in |
| **Published** | When the version was published, as a relative time; hover for the exact time. For a container image the column is **Pushed**, or **Fetched** where only remote repositories hold it |
| **Last download** | When a client last downloaded the version through Quader, or **Never**, and under it how often clients downloaded it. What counts as a download of a version is the file a client fetches to install it: the npm tarball, the NuGet package, the Debian package file of any architecture, the Ansible collection archive, the image manifest of a tag (pulled by the tag or by its digest). Reading metadata, such as a package document or an index, and checking a tag with a HEAD request are not downloads. A version a mirror pass fetched and no client downloaded yet says **Never** |
| **Repositories** | Which repositories hold the version. Only when the versions are not all in the same repositories |

- Type into **Filter versions** to keep the versions that contain the text, or whose tag does: `4.0` finds every 4.0.x, `rc` the
  release candidates, `latest` the version the tag points at. The table follows as you type.
- Select a column heading to sort by it; select it again to reverse the order. **Vulnerabilities** sorted the second time lists
  the worst versions first.
- The table shows 50 versions; **Load more** shows 50 more.
- The search and the sort are part of the page address, so you can send the narrowed table to a colleague.

**A container image** lists its images, not its tags: the tags that name one image (a version, its shorter forms, `latest`, the
commit tag a pipeline pushes beside it) are one row, led by its highest version, with every other tag after it in grey and the image's
signature badge. The header counts the tags ("204 tags · Latest 5.4.7"). **Filter tags** keeps the images one of whose tags holds the
text, so `sha-5ccf` finds the image of that commit, and "12 of 97 images" says how many match. Size, grade and **Last download** are the
image's; a row's link and its page are the leading tag's. Under the tags stand the first twelve characters of the image's digest (point at
them for the whole); the whole digest, with **Copy digest**, is on the tag's page under **Details**.

Below the table follows the README of the latest version, when it has one you may read. A collection of Ansible content links to
the documentation of its latest version instead. **Details**, at the end of the page and closed until you open it, lists the
repositories that hold the package, its keywords (each opens the packages that share it), its downloads and when it was first published.

## A version's page

Select a version to open its page. Its address is the package's with `@` and the version, for example
`https://quader.example.com/ui/en/projects/web/packages/npm/@team-a/ui@1.4.0`; send it to point a colleague at exactly this version.

At the right of the head stand the version's acts: for npm, NuGet and Ansible Galaxy the switch between the versions, then **Install**
or **Pull** with the commands of this version ([below](#the-command-of-a-version)). Where no client can install the version (only a
disabled repository or a system repository holds it) there is no button, and a sentence under the facts says why; a version of the
ClamAV definitions has **Set up client** there instead, since a ClamAV installation always takes the version in use.

Under the name and the version, one line holds the facts of the version: its grade with the number of findings and how many have a
fix (select it to open the findings), when it was published (for a container image: pushed, or fetched by a remote repository), its
size, when a client last downloaded it, its downloads and its licences; where the version names the address of its licence, the licence
opens it. Under the facts stand the version's links in one row (**Homepage**, **Documentation** or **Docs site**, **Source repository**,
**Issue tracker**, as far as it declares them), each opening the other site in a new tab, and only for `http` and `https` addresses; any
other address is shown as text. An npm package that runs a script of its own when it is installed (a `preinstall`, `install` or
`postinstall` script, or native code built with `binding.gyp`) says **Runs a script of the package when it is installed** there: Quader
reads this from the package itself, before you copy its command. A
version that its publisher marked as deprecated shows **Deprecated.** with the publisher's message, for NuGet with its reasons and the
package to use instead, for an Ansible collection with "Every version of this collection is deprecated."; a version whose download a
repository refuses shows **Blocked in** and the repository. A file of the version that the malware scan refuses shows **Malware found in**
the file and the signature, or that the file **could not be checked for malware** where only checked artifacts can be downloaded; each
ends with **Downloads are refused.** (see [the malware scan's states](files.md#the-malware-scans-states)).

### Tabs

| Tab | What it shows |
|---|---|
| **README** | The README of this version. The first tab of an npm or a NuGet package, also where the version has none: it then says so. For a container image and a Debian package shown only when the version has one |
| **Vulnerabilities** | For each repository that holds the version, its grade card and its open findings, the worst first (see [Vulnerabilities](vulnerabilities.md#on-the-page-of-a-version)). The first tab of a checked container image; after the README of an npm or a NuGet package; after **Details** for a version that is not checked |
| **Files** | The files of this version, each path with its size and the first twelve characters of its SHA-256 under it (point at the hash for the whole), and the columns **Malware scan** (while the installation scans for malware: **No malware found**, **Queued**, **No scanner**, **No signatures**, **Malware found**, **Could not be checked** or **Released**, see [the malware scan's states](files.md#the-malware-scans-states)) and a **Download** link, which a file the malware scan refuses does not have. **Copy SHA-256** copies the checksum; compare it with the output of `sha256sum` on the downloaded file |
| **Details** | The same blocks, in the same order, for every package type, as far as the version declares them: a Debian package's long **Description**; **Keywords** (npm's keywords, the tags of a NuGet package or an Ansible collection), each opening the packages of that type with the keyword; **Dependencies**, grouped as the package type groups them (npm's `dependencies`, `peerDependencies`, `optionalDependencies`, `bundleDependencies` and `devDependencies`; NuGet's target frameworks; Debian's Depends, Recommends and the other relations; a list for an Ansible collection or a Helm chart), a name the installation holds opening its page; for an image its digest, media type, size and platforms, each platform with its own size or, in a remote repository, **Not fetched** until a client pulls it; **Package details**, the facts only the package type has (npm: the required runtimes, the types, the unpacked size, the number of files, the install script and the publisher; NuGet: title, minimum client version, package types, copyright, release notes, whether its licence must be accepted; Debian: architectures, suites, maintainer, section, priority, installed size, the source package; Ansible: the Ansible versions it needs; an image: the facts of its labels); **Authors**; then **Repositories**: each repository that holds the version, its **Class** and when it was **Published** (an image: **Pushed** or **Fetched**) there |

## Deprecated and unlisted versions

npm and NuGet deprecate single versions, Ansible Galaxy a whole collection; a Helm chart is deprecated when its `Chart.yaml` says
`deprecated: true`. A deprecated version is still downloaded and installed; the publisher says it should no longer be used, and why. Quader marks it **Deprecated** wherever it is listed: in the table of versions,
in the switch of a version's head, in a repository's package list and in **Packages** (for a package, by its latest version), in the
search's results and suggestions, and on the tab **Upstream** of a remote repository (a Helm chart only once the remote has cached it:
the upstream's catalogue does not say it). Point at the mark to read the message.

A NuGet version can also be **Unlisted**: it is hidden from search and still downloaded when a project asks for exactly this version. It
is not deprecated, and the two marks can stand together.

To deprecate a version of a local repository, open its page and select the menu at the right of the head: **Deprecate version** (npm,
NuGet) or **Deprecate collection** (Ansible Galaxy). The dialog asks what the format carries:

| Format | What you enter | What the clients do |
|---|---|---|
| npm | A message, required | `npm install` prints it as a warning; a version range installs another version where one is not deprecated |
| NuGet | One or more reasons (**Legacy, no longer maintained**, **Critical bugs**, **Other**), a message, and the package to use instead with its versions | Visual Studio and `dotnet list package --deprecated` show the reasons and the package to use instead; no client shows the message; a restore goes on as before |
| Ansible Galaxy | A message, shown on Quader's pages only | `ansible-galaxy` installs the collection as before and shows nothing |

On a deprecated version the menu has **Change deprecation** and **Undo deprecation**. You need the deploy permission on the package and,
where the package has owners, ownership of it. A remote repository shows the deprecation of its upstream and a virtual repository that of
its members; neither can be changed there. `npm deprecate` itself is not available yet: deprecate an npm version on its page. A Helm
chart has no such menu: set `deprecated: true` in its `Chart.yaml` and push the chart as a new version; `helm install` and `helm upgrade`
then print a warning.

## Distribution tags

npm packages carry distribution tags, names such as `latest` or `next` that point at a version. The versions table shows each tag
beside the version it points at, and only tags that point at a version you may see.

You change tags with your package manager, not in the UI, for example `npm dist-tag add @team-a/ui@1.4.0 next`. That needs the deploy permission and, where the package has owners, ownership of it. The guide on the repository's page **Set up client** has the commands for each client; see [Connecting a package client](client-setup.md). The content page of a repository also lists its packages with their install commands; see [Repositories](repositories.md#the-content-of-a-repository).

## The command of a version

**Install** (**Pull** for a container image) in the head of a version's page, and at the end of a version's row in a repository's list,
opens the commands of this version:

1. Where several repositories you can install from hold the version, choose one under **Repository** at the top; Quader preselects a virtual repository, otherwise a local one. A system repository of the project Upstream is never offered: no client can install from it. Nor is a disabled repository, or a virtual one that holds the version only through disabled members. Nor is a remote repository whose delivery rules do not deliver the version: a note at the top of the page names the repository and the reason (a rule blocks it, no rule allows it, or it lies outside the allowed range of versions) with a link to its tab **Upstream**, and when no repository delivers the version the head has no button and a sentence under the facts says so.
2. Choose your client's tab: **npm**, **pnpm**, **Yarn Berry**, **Yarn Classic** for npm; **.NET CLI**, **NuGet CLI**, **Visual Studio** for NuGet; **Docker** and **Podman** for an image. A package type with one client (APT, ansible-galaxy, curl) names it instead. The arrow keys move between the tabs, and the panel opens again on the tab you chose last.
3. Select the copy button at the end of the command. A command of several lines is shown as a block; one longer than three lines is collapsed, **Show the whole command** expands it and **Show less** collapses it again.

The command assumes that your package manager is already configured for the repository; it never contains a token. **Set up client**, at the end of the row of tabs, opens the page of the chosen repository with the chosen client, where you configure that once; see [Connecting a package client](client-setup.md). Escape or a click outside closes the panel.

## The README

Quader renders the README on the server from the package's own Markdown, with tables, task lists and code blocks, and removes anything that could run in your browser. Links to other sites carry no referrer, and the links of a README's table of contents jump within the README.

- A README larger than the display limit is shortened, and the page says **This README is shortened because it is larger than the display limit.**
- Images from other sites are shown only from sites your administrators allowed; every other external image is replaced by a link, and the page says how many.
