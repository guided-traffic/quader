# Security dashboard

The security dashboard is the instance-wide view of the vulnerability findings, for the auditors and the administrators of the installation. It shows the counts and the trend over every project, finds a finding by its advisory identifier or any alias of it, lists the findings with the project of each, and lets the people who may triage change their status.

A project's **Vulnerabilities** screen is the same report for that project; this page covers what differs. For what a finding is and what the triage statuses mean, see [Vulnerabilities](vulnerabilities.md).

## Who sees it

In the side bar of **Your work** (the projects, the guides, your profile), the section **Security audit** holds **Security dashboard**, which opens `/ui/<locale>/security`. The section is shown to whoever holds the capability `content:metadata-read`: the roles `auditor` and `admin`. Without it there is no section, and the address answers **Page not found**.

| Capability | Held by | What it allows on the dashboard |
|---|---|---|
| `content:metadata-read` | `auditor`, `admin` | Opens the dashboard and reads the findings of every project, whether or not you are a member of it |
| `vulnerabilities:triage` | `auditor`, `security-admin`, `admin` | Changes the status of a finding: confirm, dismiss, block, lift a block |

The role `security-admin` holds `vulnerabilities:triage` but not `content:metadata-read`: it triages the findings of the repositories it may read, on the screens of their projects, and has no security dashboard. A caller who reads but may not triage sees no selection boxes, no **Change status** and no status buttons on a finding, and the dashboard says so.

The dashboard is no page of the admin area: nothing on it leads there except **Advisory sources**, which is shown to whoever may open that page (`settings:read`). The **Admin** button of the top bar is shown to whoever may use a page of the admin area; an auditor may (the audit log, the users, the admin dashboard), so it sees the button as well.

## What it shows

The page starts with its title, a line that says what the counts cover, the counts and the trend; the list of findings is below them.

**The severity bar, the counts and the banner** are those of a project's report (the bar of **Critical** to **Unknown**, then **Malicious**, **Needs triage**, **Blocked versions**; the banner for stale or never-imported advisory data), counted over every project. Each segment of the bar and each count is a link that applies its filter.

**The trend** is the chart of open findings per day by severity over 30, 90 or 365 days, with **Show as table**.

**What the counts and the trend cover.** They follow the projects, the repositories and the formats you filter by, and nothing else: the search and the other filters (severity, status, package, dates and the rest) narrow the list of findings only. The line above the counts says which of the two you are looking at: "The counts and the trend cover every project" while no project, repository or format filter is set, and that they cover the filtered projects, repositories and formats while one is. A list narrowed to one package next to counts of the whole installation is therefore expected.

**The list** has the columns of a project's report. In **Repository**, the project part of each repository's name (`web-frontend` of `web-frontend/npm-vendor`) is a link that narrows the list to that project. The identifier of a row opens the finding, and **Export CSV** downloads the rows of the current filter, with a `project` column; the findings and the advisories open inside the dashboard.

## Searching and filtering

The list has one search field, **Search by advisory, alias, package or summary**, and **Filters** beside it.

| You type | The dashboard finds | Notes |
|---|---|---|
| A whole advisory identifier: `CVE-2021-23337`, `GHSA-35jh-r3h4-6jhm`, `MAL-2026-1234`, `PYSEC-…`, `GO-…`, `RUSTSEC-…`, `OSV-…` | The findings of that advisory, and of every advisory that names it as an alias | `CVE-2021-23337` finds the finding whose advisory is `GHSA-35jh-r3h4-6jhm`, because the GitHub advisory names that CVE as an alias. The identifier is written in its usual spelling as you type: `cve-2021-23337` becomes `CVE-2021-23337`, a `GHSA-` identifier keeps its lower-case groups. The search starts at the advisory and reaches its findings through an index, so it answers as fast on a large installation as on a small one |
| Any other text, for example `lodash` or `prototype pollution` | The findings whose package name, advisory identifier, alias or summary contains the text, in any case | A part of an identifier (`CVE-2021`) is such a text. It reads every finding you may read and is slower on very large installations; a whole identifier is the fast way |

**Filters** opens the drawer: **Project** (the projects as a list to tick, by mark and name), **Severity**, **Status**, **Kind**, **Repository**, **Format**, **Package** (the exact name and a version), **Exploit, fix and block**, and **Detected**. Every filter that is set shows as a chip beside **Filters**, with its value and a button that removes it. Everything is part of the address, so a link reproduces the list.

The advisory page's link to all of its findings opens the dashboard with that identifier in the search field.

## Triage

With `vulnerabilities:triage`, tick findings (**Select all loaded findings** ticks the loaded rows) and choose **Change status** to confirm, dismiss, or block several at once; the dialog reports one result per finding. A single finding is changed on its own screen, where the history is. Blocking a finding refuses the download of that version to every client of its repository and of the virtual repositories that contain it, in whichever project it lives; the dialog says so before it acts. Every change is audited with your name.

## After a restore

A restore to an earlier point replays every block that was set after it, whether a person set it or the scanning did (a malicious version detected, an advisory that turned malicious, a version blocked again when it came back). A finding that the restored installation did not have is created by the replay; its history has a row, a detection or a status change, that names no person. A version the repository does not store any more is resolved with the reason that the version was removed: the block still counts for a fetch of that version, and for a published copy of it, until the ecosystem has synced since the restore. The first complete sync then decides again: a block whose advisory still affects the version stays, one whose advisory stopped applying resolves. You can lift a replayed block on a stored version like any other block.

## Limits

- The dashboard lists findings; it does not show a project's repositories or members. Open the project from **Projects**.
- The package link of a row opens the package page in its project.
- Counting is per request and per caller; nothing is cached across callers.
