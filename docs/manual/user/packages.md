# Packages

The Packages screen lists the packages of all repositories you may read, with filters, sorting and saved views. A package opens on the table of its versions; each version has a page of its own with its vulnerabilities, a ready-made install command, its files and its metadata. This page explains each part.

## What the list shows

Select **Packages** in the side bar of a project. The list holds the npm packages of every repository of the project that you may read; generic repositories have no package names, so their files are on the repository's own page instead (see [Browsing and transferring files](files.md)).

A package appears once, however many repositories hold it. Everything in its row is counted from the versions you may see: a version you may not read changes neither the latest version nor the counts. A remote repository contributes only the versions it has already cached.

| Column | Meaning |
|---|---|
| **Package** | The package name, which opens the package, and under it the description of the latest version |
| **Format** | The package format |
| **Latest** | The highest stable version you may see. A pre-release is shown only while no stable version exists |
| **Updated** | When the latest version you may see was published, as a relative time ("4 minutes ago"); point at it for the exact time |

When a version was published, how large it is, its vulnerabilities and when a client last downloaded it are in the versions table of the
package's page.

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

## A package's page: its versions

Select a package name to open its page. The page shows the name, the format, the description of the latest version, how many
versions you may see and the latest one. The address of a package is `/projects/<project>/packages/<format>/<name>`, for example
`https://quader.example.com/ui/en/projects/web/packages/npm/@team-a/ui`.

The page is a table of every version you may see, the highest version first:

| Column | Meaning |
|---|---|
| **Version** | The version, which opens its page. A distribution tag that points at it, such as `latest` or `next`, stands beside it, and so does **pre-release** for a pre-release. For a container image the version is the tag |
| **Size** | The size of the version's files; for an image, the size of the image. **≥** before the size of an image of several platforms counts only the platforms clients have fetched so far through a remote repository; a dash means the size is not known yet, because no platform was fetched. Point at the size for what it counts |
| **Vulnerabilities** | The grade of the version, the number of its open findings and how many of them have a fix, for example the shield **E** and **1,145 findings · 112 fixable** (see [Vulnerabilities](vulnerabilities.md#grades)). Only where the versions are checked for vulnerabilities and you are signed in |
| **Published** | When the version was published, as a relative time; hover for the exact time |
| **Last download** | When a client last downloaded the version through Quader, or **Never**. What counts as a download of a version is the file a client fetches to install it: the npm tarball, the NuGet package, the Debian package file of any architecture, the Ansible collection archive, the image manifest of a tag (pulled by the tag or by its digest). Reading metadata, such as a package document or an index, and checking a tag with a HEAD request are not downloads. A version a mirror pass fetched and no client downloaded yet says **Never** |
| **Repositories** | Which repositories hold the version. Only when the versions are not all in the same repositories |

- Type into **Filter versions** to keep the versions that contain the text, or whose tag does: `4.0` finds every 4.0.x, `rc` the
  release candidates, `latest` the version the tag points at. The table follows as you type.
- Select a column heading to sort by it; select it again to reverse the order. **Vulnerabilities** sorted the second time lists
  the worst versions first.
- The table shows 50 versions; **Load more** shows 50 more.
- The search and the sort are part of the page address, so you can send the narrowed table to a colleague.

Below the table follows the README of the latest version, when it has one you may read. A collection of Ansible content links to
the documentation of its latest version instead. **Details**, at the end of the page and closed until you open it, lists the
repositories that hold the package, its keywords, its downloads and when it was first published.

## A version's page

Select a version to open its page. Its address is the package's with `@` and the version, for example
`https://quader.example.com/ui/en/projects/web/packages/npm/@team-a/ui@1.4.0`; send it to point a colleague at exactly this version.

Under the name and the version, one line holds the facts of the version: its grade with the number of findings and how many have a
fix (select it to open the findings), when it was published, its size, when a client last downloaded it, its downloads and its licences. A
version that its publisher marked as deprecated shows **Deprecated.** with the publisher's message; a version whose download a
repository refuses shows **Blocked in** and the repository. A file of the version that the malware scan refuses shows **Malware found in**
the file and the signature, or that the file **could not be checked for malware** where only checked artifacts can be downloaded; each
ends with **Downloads are refused.** (see [the malware scan's states](files.md#the-malware-scans-states)).

### Tabs

| Tab | What it shows |
|---|---|
| **Vulnerabilities** | For each repository that holds the version, its grade card and its open findings, the worst first (see [Vulnerabilities](vulnerabilities.md#on-the-page-of-a-version)). The first tab of a version that is checked |
| **README** | The README of this version. Shown only when the version has one |
| **Files** | The files of this version with **Size**, **SHA-256**, **Malware scan** (while the installation scans for malware: **No malware found**, **Checking**, **Malware found**, **Could not be checked** or **Released**, see [the malware scan's states](files.md#the-malware-scans-states)) and a **Download** link, which a file the malware scan refuses does not have. **Copy SHA-256** copies the checksum; compare it with the output of `sha256sum` on the downloaded file |
| **Details** | **Links** (**Homepage**, **Source repository**, **Issue tracker**), **Authors**, **Dependencies**, **Development dependencies**, **Peer dependencies**, **Optional dependencies** and **Required runtimes**, as far as the version declares them; for an image its digest, media type, size and platforms, each platform with its own size or, in a remote repository, **Not fetched** until a client pulls it; then **Repositories**: each repository that holds the version, its **Class** and when it was **Published** there |

Links in **Details** open in a new tab and only for `http` and `https` addresses; any other link is shown as text.

## Distribution tags

npm packages carry distribution tags, names such as `latest` or `next` that point at a version. The versions table shows each tag
beside the version it points at, and only tags that point at a version you may see.

You change tags with your package manager, not in the UI, for example `npm dist-tag add @team-a/ui@1.4.0 next`. That needs the deploy permission and, where the package has owners, ownership of it. The guide on the repository's page **Set up client** has the commands for each client; see [Connecting a package client](client-setup.md). The content page of a repository also lists its packages with their install commands; see [Repositories](repositories.md#the-content-of-a-repository).

## The install panel

The panel **Install** stands on a version's page, under its facts and above its tabs. It shows the command that installs this version, in full.

1. Under **Client**, choose npm, pnpm, Yarn Berry (2 and later) or Yarn Classic (1.x).
2. Under **Repository**, choose the repository to install from. The picker appears only when several repositories hold the version; Quader preselects a virtual repository, otherwise a local one. A system repository of the project Upstream is never offered: no client can install from it, and a version only such a repository holds shows one sentence instead of a command.
3. Select **Copy install command** beside the **Install command**. A command of several lines is shown as a block; one longer than three lines is collapsed, **Show the whole command** expands it and **Show less** collapses it again.

The command assumes that your package manager is already configured for the repository; it never contains a token. **Set up client** opens the page of the chosen repository with the chosen client, where you configure that once; see [Connecting a package client](client-setup.md).

## The README

Quader renders the README on the server from the package's own Markdown, with tables, task lists and code blocks, and removes anything that could run in your browser. Links to other sites carry no referrer, and the links of a README's table of contents jump within the README.

- A README larger than the display limit is shortened, and the page says **This README is shortened because it is larger than the display limit.**
- Images from other sites are shown only from sites your administrators allowed; every other external image is replaced by a link, and the page says how many.
