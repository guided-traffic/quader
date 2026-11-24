# Curating a remote repository

A remote repository caches an upstream registry, and what it delivers of that upstream can be restricted: everything except what is blocked, or only what is allowed. This page explains the delivery rules, the tab Upstream on which you browse the upstream, ask for a package and allow it, the downloads into the cache ahead of a client and the tab Downloads that shows them, and what each package client prints when the rules refuse it.

## What a remote repository delivers

Every remote repository of every format (npm, NuGet, OCI, Debian, Ansible Galaxy) has a delivery mode and delivery rules.

- **Everything except what is blocked** is the default: a client gets every package and version of the upstream unless a rule blocks it.
- **Only what is allowed** delivers nothing without a rule that allows it. Readers look up what they need on the tab **Upstream** and
  ask for it there; those who may allow it see the request.
- **A rule** allows or blocks one of three things: a sub-section, a name pattern such as `@acme/*` (an npm scope), `Contoso.*` (a NuGet
  ID prefix), `library/*` (an image namespace), `community.*` (a Galaxy namespace) or `lib*` (Debian names); a package with every version,
  future ones included; or one exact version (no range). The narrowest rule decides: a version's rule before the package's, the package's
  before a sub-section's, and at equal width a block wins. Without a rule the mode decides.
- **Who.** A Maintainer or Owner of the project and an administrator set rules and answer requests. The mode is the repository's
  configuration, set in the section **Delivery** of its settings by whoever may change the configuration
  ([Managing repositories](../admin/repositories.md#delivery-of-a-remote-repository)). Anyone who may read the repository may ask.
- **What a rule refuses.** The rules decide before Quader fetches anything from the upstream, when the cache serves a stored file, and
  in the documents a client resolves versions from: npm's package documents and dist-tags, NuGet's version lists, registrations and
  search, an OCI repository's tags list and Ansible Galaxy's collection documents list only what is delivered, so a client's version
  range resolves to the newest version that is delivered. A Debian remote passes its upstream's signed indexes through unchanged, so APT
  lists every package and is refused the download of one the rules do not deliver; a virtual repository over the remote shows a filtered
  index ([Debian packages with APT](apt-packages.md)).
- **A rule binds everyone**, administrators included. Through a virtual repository a refused package counts as not found in that member,
  and the virtual repository goes on to its next member.
- **Allowing is not releasing.** A version that is allowed and that the malware scan, the vulnerability block or the signature check
  refuses stays refused until an auditor releases it; the tab says which of them holds a version back.

## The tab Upstream

When you are signed in, a remote repository's page has these tabs under its title: its content (**Packages**, **Images** or **Files**),
**Upstream**, **Downloads** and, last, **Details** with the upstream it caches and its other addresses. **Upstream** shows what the registry the repository caches offers, whether it is stored here or not, and
what this repository delivers of it.

- **The line at the top** says what the repository delivers: "Delivers everything except what is blocked." or "Delivers only what is
  allowed.". If you may change the repository's configuration, **Change** opens the section **Delivery** of its settings. **Rules** with
  their number opens the list of the repository's delivery rules, if you may read them.
- **Search the upstream** finds packages by words, where the registry answers a search (npm, NuGet, Ansible Galaxy, Docker Hub, Quay);
  a Debian remote searches the package indexes Quader has read, as `apt search` does, and **Names only** matches names alone. **Look up by
  exact name** opens one package by its full name. A registry without a search (ghcr.io, GitLab) offers only **Look up**, and the
  sentence under the field says why. The address keeps what you searched for, so you can share the link; the message a client gets
  for a package the repository does not deliver links the tab with the package's name.
- **In a repository that delivers only what is allowed**, a search shows you only what is delivered, with the number of hits it leaves
  out; look a package up by its exact name to see it and ask for it.
- **Levels**: where the registry has them, the tab lists them: a Debian remote's suites, then their components; an npm scope or a
  Docker Hub namespace is opened by typing its name.
- Each package shows its newest version, its state here (**Delivered**, **Allowed**, **Blocked**, **Not allowed** or **Requested**)
  and whether the cache holds a version of it (**Stored**). Open a package with its arrow to see its versions, newest first, 100 at a
  time with **Load more** and **Filter versions**: when each was published, its size where the registry tells it, its state, the rule
  that decided it, whether the cache holds it (**Stored**, **Stored for examination**, **Waiting**, **Running**), and its verdicts: for a
  version that is not stored the vulnerability verdict Quader knows without downloading it (npm and NuGet), for a stored version a link
  to its page with the malware, vulnerability and signature verdicts.
- When the registry cannot be asked, the tab says why: "The upstream is assumed offline until 10:42; Quader retries then.", "The
  upstream catalogue's budget is used up; try again in 40 s.", or that a package's document is too large to read for browsing.

### Asking for a package

- **Request** asks for a version, or with **Request every version** for a whole package, in a repository that does not deliver it.
  The dialog takes a note on why you need it, which those who decide read. **Your requests** at the top of the tab lists what you asked
  for, with its state (**Open**, **Allowed** or **Declined**) and, for a declined request, the reason; **Withdraw** takes back a request
  that is still open.
- Quader usually downloads what you asked for at once, charged to your download budget, so that it is scanned before anyone decides; it
  is not delivered until it is allowed. A repository can switch that off.
- Once your request is allowed, the version is delivered and Quader downloads it into the cache; your client installs it as any other.

### Allowing, blocking and downloading

For Maintainers, Owners and administrators the tab adds:

- **Requests** at the top: every open request with who asked and their notes, how often clients were refused the package, and what its
  examination found (**Verdicts** opens the examined version). **Allow…** writes a rule for the request, optionally for every version of
  the package and with a download of every version; **Decline…** takes a reason, which the askers read, and discards what was fetched for
  examination.
- Each version's menu: **Download** or **Download for examination** (for a version the repository does not deliver: it is stored and
  scanned, not delivered), **Allow**, **Block**, **Remove rule** and **Discard** (removes what was fetched for examination).
- On a package: **Allow every version** and **Block every version** (future versions included) and **Download every version…**; on a
  level: **Download this sub-section…**. Both downloads open a preview of how many versions and bytes they take, how many are stored and
  how many are not delivered here, with bounds you may lower, and **Start download**.
- **Rules** lists the repository's rules with their notes, adds a rule for a sub-section, and removes rules.

A reader who may fetch a version through the repository sees **Download** on a delivered version too; it is charged to their download
budget.

## Downloads ahead of a client

A download ahead stores a version in the cache before any client asks for it, so the first client is served from the cache. It is a row
of the repository's download queue and is started by **Download** on the tab **Upstream**, by a bulk download of a package or a
sub-section, by an allow, by a request's examination, or by Quader itself when a client only asked about a version
([Managing repositories](../admin/repositories.md#downloads-a-client-only-asked-about)). Through the management API it is
`POST /api/v1/projects/<projectKey>/repositories/<key>/downloads` with `{"level": "version", "name": "…", "version": "…"}`.

- A download ahead does not depend on the rules: what it stores and the rules do not deliver is kept for examination, scanned as every
  stored file is, and not served. Allowing it later serves it from the cache without asking the upstream again.
- One version the rules deliver is charged to the person who starts it, as if they had downloaded it. A version the rules do not
  deliver, every version of a package and a sub-section need a Maintainer, an Owner or an administrator and are system work, bounded by
  the preview's bounds.
- What was fetched for examination and never allowed is removed after the days the repository sets (30 unless changed), or at once on a
  decline or **Discard**.

## The tab Downloads

**Downloads** lists the repository's download queue: what Quader downloads into the cache ahead of a client, and the
fetches of clients that failed. When downloads have failed, the tab says how many, for example **Downloads · 2 failed**. Use it to follow
a cache while it is being built and to find out why a client's fetch failed.

- **All**, **Waiting**, **Running**, **Failed** and **Done**, each with its count, show the downloads in that state; **Failure** shows
  only the failures of one kind, such as **TLS** or **Rate limited by the upstream**. The address keeps what you chose, so you can
  share the link.
- Each row names the package and version, where the download came from (**You**, a person's name if you may see it, **Request**,
  **Allow**, **Bulk**, **Probe** for a download Quader started after a client asked about a version, **Client** for a client's fetch
  that failed, with how often it failed), its state with the attempt ("Attempt 2 of 5 · next in 4 minutes"), the files and bytes stored,
  and a failure in one line: its kind, the upstream's HTTP status (or Quader's error code where no answer came) and the upstream's host.
- Open a row with its arrow to read the whole failure: Quader's sentence of what failed and what helps, the step it failed in (resolving
  the host name, connecting, the TLS check, the answer's status, reading the body, checking the digest, storing), the error code, the
  file, the address it asked (without its query, which can carry a credential), the upstream's status, how long the upstream asked to
  wait, and the start of the upstream's answer. Everyone who may read the repository sees these details, the upstream's answer included.
- **Retry** lets a failed or cancelled download wait again; **Cancel** stops one that waits or runs. You see them on the downloads you
  started; on the downloads of clients and probes if you may fetch that version through the repository; and on every row if you are a
  Maintainer or Owner of the project or an administrator, who also see **Retry all failed**, which retries every failed download of the
  kind **Failure** shows.
- **Bulk downloads**, above the filters once the repository has one, lists its bulk downloads, newest first: what each downloads, who
  started it and when, its state (**Finding versions** while Quader reads the upstream's list of versions, **Queued** while its rows
  wait or run, **Done**, **Stopped at a bound**, **Cancelled**, **Failed**), how many versions it queued of those it found, and its
  rows by state. A bulk reads the versions first and queues its rows after that, so a bulk that fails before its first row is seen only
  here: its line says what could not be read ("Docker Hub's version list could not be read") and why, in Quader's words. **Start
  again** starts a failed bulk anew with the same bounds, for Maintainers, Owners and administrators; **Cancel** stops one that
  still runs.
- **Bulk** (a row's origin) and the name of a bulk in **Bulk downloads** open the rows of that bulk with its line above them: how many
  versions it found and queued, its rows by state, its bounds, what failed, and **Cancel bulk** for the person who started it and for
  Maintainers, Owners and administrators, or **Start again** for a failed one. **Show every download** returns to the whole queue.
- While something waits or runs, or a bulk is still finding versions, the tab reads the queue again every five seconds; **Pause
  automatic refresh** stops that until you resume it.

By default, finished and cancelled downloads leave the list a week after they ended, failed ones after 30 days.

## What your client shows when the rules refuse

A refused download answers `403` with the code `not_allowed_in_repository` and a text that names the package, the version (or "any
version"), the repository and the reason, followed by where to ask: "left-pad any version is not allowed in web/npmjs: this repository
delivers only what is allowed. Ask for it at https://quader.example.com/ui/projects/web/repositories/npmjs/upstream?name=left-pad". The
answer is never cached, so an allow reaches every client within seconds, through a Bastion too: no cached refusal outlives it.

What the clients print, measured on 2026-11-24 against a remote that delivers only what is allowed:

| Client | What it prints | Exit status |
|---|---|---|
| npm 11, `npm install` | `npm error code E403`, then `npm error 403 403 Forbidden - GET <address> - <text>` | 1 |
| .NET SDK 10, `dotnet restore` | the text as `warning : <text>`, a retry of the request, then `error NU1301: Failed to retrieve information about '<ID>' from remote source '<address>'` and `Response status code does not indicate success: 403 (Forbidden).`; about 6 seconds with the retry | 1 |
| Docker 29, `docker pull` | `Error response from daemon: error from registry: <text>` | 1 |
| APT 3.0, `apt-get install` | `Err:1 <address> … 403  Forbidden` and `E: Failed to fetch <address>  403  Forbidden`: APT shows no reason, and lists the package, since a Debian remote passes its archive's indexes unchanged | 100 |
| ansible-core 2.21, `ansible-galaxy collection install` | `[ERROR]: Error when getting the collection info for <name> from quader (<address>) (HTTP Code: 403, Message: <text> Code: not_allowed_in_repository)` | 1 |

Through a virtual repository the refusal of a member counts as not found there: the virtual repository goes on to its next member, and
only when no member serves the package does the client get the first member's refusal.

When the version you need is not delivered, open the address the message names, look the package up and select **Request**.
