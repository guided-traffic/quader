# Getting started

Quader stores the packages and files your teams publish, caches public npm packages, and serves both under one address per repository. This page shows how to sign in, how the screen is laid out, how to change the language and the theme, and where to go next.

## What Quader does for you

Quader keeps your packages and files in repositories. You use it in two ways:

- **In the browser**, to find packages and files, read their details, copy install commands and set up your package manager.
- **With your tools**, such as npm, pnpm, Yarn or curl, which install, publish, download and upload through the repository addresses Quader shows you.

The browser signs you in through your organization's identity provider. Tools never use your password: they use a token you create in Quader. Quader serves npm and NuGet packages, container images, Debian packages, Ansible collections and roles, and generic files; [Known limitations](../admin/limitations.md) lists what is not built yet.

## Sign in

Open the address of your installation, for example `https://quader.example.com/`. Quader sends you to the sign-in screen when you are not signed in and the installation has no public content.

### The sign-in screen

The sign-in screen shows only what your administrators configured.

| What you see | What it does |
|---|---|
| **Sign in with** and the name of a provider | Takes you to your organization's identity provider. After you sign in there, Quader returns you to the page you wanted. There is one button per configured provider |
| **Currently unavailable** next to a provider | Quader cannot reach that provider right now. Try again later or use another provider |
| **Redirecting to the identity provider.** | Quader is sending you to the provider, either after your choice or because the installation sends everyone straight there |
| **Your session has ended. Sign in again to continue where you left off.** | Your browser was signed in, but that session has ended since. Sign in again, or continue without signing in where some content is public |
| **Continue without signing in** | Opens Quader anonymously. Shown only where some content is public |
| **Local administrator** with **Username** and **Password** | The form of the emergency administrator account. It is not for regular users, who never have a password in Quader |
| **No sign-in method is configured.** | Ask your administrator |

### Continue without signing in

Where your administrators made some content **Public**, you can use Quader without an account: either through **Continue without signing in** on the sign-in screen, or directly, because Quader then opens without asking you to sign in. The start page lists the public projects. You see only the repositories, packages and files that are public, and you cannot upload, publish or delete anything. A package page shows the README and the documentation only when the repository's wiki is public too; otherwise those tabs are not there. The top bar shows **Sign in** while you are anonymous.

### When your session ends

A browser session ends after a period without activity and after a maximum lifetime that your administrators set (by default one hour without activity and twelve hours after sign-in). Quader then shows **Your session has ended**:

1. Select **Sign in again** to sign in and return to the page you were on.
2. If you were editing something, **Copy draft as JSON** copies your unsaved entries first.
3. **Not now** closes the dialog without signing you in.

If the session ended while Quader was not open, for example overnight, the next time you open Quader it shows the sign-in screen
with **Your session has ended**, also where some content is public, so you know why you no longer see your own projects. Sign in
again to return to the page you opened, or select **Continue without signing in** to use the public content.

Some actions need a recent sign-in. Quader then shows **Confirm it is you**. Select **Sign in again**, sign in, and repeat the action; the sign-in screen tells you why it appeared, and editors keep your entries.

### If the sign-in fails

When your identity provider signs you in but Quader cannot complete the sign-in, a page **Sign-in failed** names a reason. **Start the sign-in again** returns to the sign-in screen.

| Reason | What it means | What to do |
|---|---|---|
| `flow_expired` | The sign-in took too long, or a page of it was opened twice | Start the sign-in again |
| `browser_mismatch` | The sign-in finished in another browser or profile than it started in | Start and finish it in the same browser |
| `user_disabled` | Your Quader account is disabled | Ask an administrator |
| `not_provisioned` | The installation does not create accounts at the first sign-in, and you have none yet | Ask an administrator for an account |
| `not_in_allowed_group` | Your identity provider does not list you in a group that may sign in | Ask an administrator |
| `email_unverified` | Your identity provider has not verified your email address | Verify it at your identity provider |
| `provider_unavailable` | Quader cannot reach the identity provider | Try again later |
| any other code | A configuration or provider problem | Tell an administrator the code and the time |

### Sign out

Open the user menu and select **Sign out**. Quader ends your session, also in your other tabs, and shows **Signed out** with a **Sign in again** button. Depending on the installation, you may also be signed out at your identity provider. Your other browser sessions stay open; **Profile and tokens**, then **Sessions**, lists them and **End all other sessions** ends them.

## The screen layout

Every screen has a top bar, a side bar on the left, and the content area. At the top of the content, a breadcrumb trail shows where you are and links back to the screens above it, for example **Projects › web › Repositories › npm-local**. On a narrow screen the side bar is hidden and opens from the menu button.

### The top bar

From left to right:

| Element | What it does |
|---|---|
| Menu button | Opens the side bar when it is collapsed, or on a narrow screen |
| **Quader** | Returns to the start page: the list of your projects, or your only project |
| **Find a repository** | Searches repositories by key and description: Enter opens **Search results**, the matching repositories with their project, format and class and a button that copies the client address (point at it to read the address), each opening the repository in its project, and a link to find packages with the same text. It finds the repositories you may read, in every project. A search never takes you into the admin area |
| **Admin** | Opens the admin area. Shown only if you hold an administrative permission |
| **Documentation** | Opens this manual. On a screen that has a matching manual page, it opens that page |
| **Theme** | **Follow the system**, **Light** or **Dark** |
| User menu | Your avatar in a circle: the picture you uploaded, or a robot that Quader generates for you alone. Its menu shows your name and holds **Profile and tokens**, **Guides**, **Language** and **Sign out** |

### The side bar

Your work happens in projects. The start page, **Projects**, lists the projects you may see with your role in each, so you can find one and explore its repositories and packages; if you have exactly one, Quader opens it for you. To find a repository, a package or a project anywhere, use the search field in the top bar ([Search](search.md)). The side bar shows where you are:

| Where | Entries | Who sees it |
|---|---|---|
| A project (`/projects/<key>/…`) | The **project switcher** on top, then **Repositories**, **Packages**, **Vulnerabilities**; for who is more than a reader of the project or holds an administrative role also **Access** (**Members**, and for owners and holders of the permission **Permission policies**, **Service accounts**, **CI identity**) and **Settings** | Everyone who sees the project, anonymous readers included. A reader finds whom to ask under the heading of the project's **Repositories**, and a screen it may read but not change says the same: "Read only. Ask priya, an Owner of web, to change this." |
| Everywhere else, under **Your work** | **Projects**, **Repositories** (the repositories you can see, of every project), **Guides**, and **Recently opened**: the last five repositories you opened in this browser, newest first | Everyone; **Recently opened** only to a signed-in user who has opened a repository they may still read |
| Everywhere else, under **Security audit** | **Security dashboard** ([Security dashboard](security-dashboard.md)) | Auditors and administrators of the installation |
| **Admin area** | Dashboard, upstream registries, endpoints, Bastions, identity, access, system and settings screens; the projects and the repositories are under **Your work**, for administrators too | Only people who hold an administrative permission, and only the entries they may use. **Admin** in the top bar leads there, **Quader** leads back |

**Recently opened** is folded the first time you see it; select it to show the repositories. Each entry is the repository's key after the mark of its project; point at it to read the project and the full name. It lists only repositories you may still read: one that was deleted or closed to you is left out. A repository you delete or move to another project leaves the list at once; one that somebody else deleted leaves it when you open its entry and Quader does not find it. Quader keeps the list in this browser only, and the search field offers the same repositories when you open it. Inside a project, the project switcher lists the projects you opened last.

The project switcher is the button with the project's name at the top of a project's side bar. It opens a panel with a search field, the projects you visited last and the ones that match; select one to open it, press Escape to close the panel without choosing. The address always carries the project, so a bookmark or a pasted link opens the same project.

The side bar is pinned on wide screens. **Collapse sidebar** at its foot hides it, and the menu button in the top bar brings it back; Quader remembers the choice in this browser. On a narrow screen it opens over the content and closes with Escape, a click beside it, or when you pick an entry. Groups such as **Identity** and **Recently opened** fold and unfold with a click on their heading, and Quader remembers that too.

### Screens keep themselves current

You never need to reload a page to see what changed. A screen that shows what Quader changes by itself (a package a client pushed, a scan that finished, a download from an upstream, a new entry of the audit log) reads it again while you look at it: every few seconds while something on it runs, such as a scan that has not answered yet, and every half minute or minute otherwise. The rows stay where they are, a row you opened stays open, and what you type is never replaced. A tab in the background reads nothing and catches up as soon as you return to it. Forms are the exception: if someone else saved a change while you edit, Quader shows both versions when you save.

## Language and theme

Quader is available in English and German.

1. Open the user menu and select **Language**.
2. Select **English** or **Deutsch**. Quader saves the choice with your preferences and reloads the same page in that language.

The language is part of the page address (`/ui/en/` or `/ui/de/`), so a bookmark keeps it. When you open Quader without a language in the address, it uses the language you saved in your preferences if you are signed in, otherwise the language your browser prefers, and English if that is neither. The sign-in page itself comes before any preference is known, so it follows the browser.

To change the colours, select **Theme** in the top bar and choose **Follow the system**, **Light** or **Dark**. When you are signed in, Quader saves the choice with your preferences and applies it at your next sign-in, also in another browser. You can set both on your profile too: **Profile and tokens**, then **Profile**, section **Preferences**, then **Save preferences**.

### Time format

Quader writes a date and a time as `26.11.2026 21:39:22 (Thu.)` unless you, or your administrator for everyone, chose another form: day, month and year with dots, the time of day with seconds, and the weekday in your language (`(Do.)` in German). To choose your own, open **Profile and tokens**, then **Profile**, section **Preferences**, choose a **Time format** and select **Save preferences**. Every form shows the current time as its example: **With the weekday** (`26.11.2026 21:39:22 (Thu.)`), **Without the weekday** (`26.11.2026 21:39:22`), **ISO 8601** (`2026-11-26 21:39:22`), **British** (`26/11/2026 21:39:22`), **American** (`11/26/2026 9:39:22 PM`) and **Your language's own** (`Nov 26, 2026, 9:39:22 PM` in English). **Installation default** follows the form your administrator set. Times stand in your browser's time zone; the audit log can show them in UTC. A report you copy from an error box adds the time in ISO 8601 with its zone, so that whoever reads your ticket reads the same moment.

## Your avatar

Until you upload a picture, Quader shows a generated robot for you, the same one on every device. To use your own picture:

1. Open the user menu and select **Profile and tokens**; the page **Profile** opens with **Public avatar** at its top.
2. Select **Choose file…** and pick a PNG, JPEG or WebP image of at most 1 MiB, at least 32 by 32 pixels, that is not animated.

Quader checks the file in your browser first and says what is wrong when it refuses it. It then stores the picture cut to a centred square and scaled to 256 by 256 pixels, so choose an image whose centre shows what you want; nothing else of the file is kept, no position data included. The new avatar shows in the top bar at once. **Remove avatar** brings the generated robot back. An administrator can also remove an avatar that should not be shown; you can then upload another.

## Where to go next

| To do this | Read |
|---|---|
| Understand local, remote and virtual repositories and their addresses | [Repositories](repositories.md) |
| Find a package, pick a version, copy its install command | [Packages](packages.md) |
| Browse, download, upload and delete files, in the browser or with curl | [Browsing and transferring files](files.md) |
| Configure npm, pnpm, Yarn or curl for a repository | [Connecting a package client](client-setup.md) |
| Create a token for a package manager or a CI job | [Personal access tokens](access-tokens.md) |
| Publish from a GitLab CI pipeline without a stored token | [GitLab CI pipelines](gitlab-ci-pipelines.md) |
| Understand an error message | [Errors and troubleshooting](troubleshooting.md) |
| Install, configure and operate Quader | [How Quader runs](../admin/overview.md) |
