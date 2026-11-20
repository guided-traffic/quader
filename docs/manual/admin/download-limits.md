# Download limits and usage

How administrators and project Owners see what every caller downloads, set how much each caller may download from the instance, a project and a repository, switch between Audit and Enforce, and lift a block.

## The model in short

Every request on a repository's address is charged to a caller: a user (all sessions and personal tokens together), a technical token, a CI project, or an anonymous address (an IPv4 address, or the `/64` of an IPv6 address). Each caller has a budget at the place whose rule applies to it, counted in these figures:

| Figure | Window | At the limit |
|---|---|---|
| Requests per second (with a burst) and per hour | second, hour | 429 with the time to wait; a refused request does not count, so a caller that waits that long is admitted |
| From the cache, MB per second | second | the download is slowed |
| From the cache, GB per hour | hour | 429 before the next download starts |
| From the upstream, MB per second | second | the fill is slowed |
| From the upstream, GB per hour; upstream requests per hour | hour | what is cached is still served, what needs the upstream is refused |
| Downloads in parallel | — | a further download waits up to 10 seconds, then 429; while waiting downloads hold a quarter of a replica's request slots, 429 at once |
| GB per day | day | 429 before the next download starts |
| Refused requests per hour | hour | the caller is blocked for the block time; a request refused by a request figure counts as one |

A **profile** is a named set of these figures; an empty figure is unlimited. A **rule** gives a profile to a caller at a place (the instance, a project, a repository), optionally with single figures changed. Which rule applies: the repository's rules first, then the project's, then the instance's; at each place a rule for the single caller or address, else the caller's groups (per figure the most generous), else the rule of its kind. Without other rules a signed-in user gets **Standard**, a technical token and a CI job **Technical**, an anonymous caller **Anonymous**, and an administrator **Unlimited**. A remote repository also has **ceilings**: what all anonymous callers, and all signed-in callers, may fetch from its upstream together.

## Audit and Enforce

The instance is in one of two modes, shown under the heading of **Download limits**:

- **Enforce** (the default of a new instance): the limits refuse and slow requests.
- **Audit**: every limit is counted and shown; nothing is refused or slowed on the package and registry addresses, not even by a request limit. The overview shows who **Would have been limited**. Use it to watch the numbers before you rely on them, knowing that while it lasts a caller, also one without a credential on a public remote repository, can send as many requests as it likes. The rate limits of the management API, of the sign-in and of the token endpoints hold in both modes.

**Switch to Audit…** (administrators only) says in one sentence what it turns off; **Switch to Audit** asks for a recent sign-in. **Switch to Enforce…** first shows what the last 24 hours would have been in Enforce: how many callers would have been limited, requests refused, downloads slowed and callers blocked, the figures that would have refused most and the kinds of caller, each a link to the overview of those callers. **Switch to Enforce** asks for a recent sign-in and takes effect on every replica within seconds.

## The usage overview

**Admin area › Identity › Usage** (the roles admin, security-admin and auditor) lists every caller that used the instance in the period: **Caller**, **Requests**, **From the cache**, **From the upstream**, **Upstream requests**, **Refused**, and **Now**, a bar of the tightest hourly or daily limit right now with its percentage. **Close to the limit** starts at 70 %; at the limit the row says **Limit reached**, in Audit **Would have been limited**; a blocked caller has the badge **Blocked**.

- The period buttons choose **Last hour**, **Last 24 hours** or **Last 30 days**; **Now** stays the current hour and day whatever the period.
- Every figure column sorts. The links over the table show all callers, those **Close to the limit**, and those at the limit.
- **Filters** narrows by **Kind** (Users, Technical tokens, CI, Anonymous addresses, System), **State** and **Project**; the search finds a name or an address.
- **Export CSV** downloads the list with the same filters.
- A caller's name opens its page, titled with the caller's name (a user's display name, a technical token's name, a CI project's path, an address) and the reference under it (`@priya`, the token's service account): its limits now per place with the rule that applies and why, **Explain for a repository**, the last 24 hours, and its usage by repository and, for a user, by credential. A blocked caller's page shows **Lift block**.

A project's Owner opens **Usage** in the project's side bar: the same list for the project's repositories, with the names of the users and technical tokens and the addresses of the anonymous callers that used them. From a repository's settings, **Usage of this repository** narrows it to that repository. The usage of single callers is kept 30 days; after that only daily sums per repository and kind of caller remain.

## Setting limits

**Admin area › Identity › Download limits** (read by admin, security-admin and auditor; changed by admin) has four tabs:

- **Rules**: the rules of the instance, in the order Quader tries them. **Add rule** opens the rule dialog: **Applies to** (a user, a group, a technical token or a CI project chosen by name; an address or range such as `198.51.100.0/24` or `2001:db8:1::/48`; or every caller of a kind), the **Profile**, and under **Change single figures** the profile's figures, each of which can be changed for this rule (**Use the profile's value** undoes it). The five built-in rules can be changed, not deleted. Under the table, **Block time after too many refused requests** (1 to 1,440 minutes).
- **Profiles**: the profiles with their figures and what uses them. **New profile** and a profile's name open the editor; the figures are in MB and GB (1 GB is 1,000,000,000 bytes), an empty field is unlimited. A profile that a rule or a project's maximum uses cannot be deleted. **Unlimited** is always unlimited.
- **Projects**: the projects with a maximum or rules. A **maximum** lets the project's Owners set limits above the instance's values, up to it; without one they can set limits up to the instance's values.
- **Blocks**: the callers blocked now, with **Lift**; **Show ended blocks** adds those of the last 30 days.

A project's Owner sets limits in the project's **Settings**, section **Download limits** (the rules for the project's repositories without rules of their own), and in a repository's **Settings**, section **Download limits** (the rules of that repository and, for a remote repository, its ceilings). An Owner's rule above the instance's values and the project's maximum is refused with the most it may be ("At most 30 GB per hour here"); when an administrator lowers the maximum, an Owner's rule above it is lowered from the next request on and marked **Above the maximum**.

A technical token's limit is set on its service account's page: its **Limit** opens **Set limit** (or **Change limit**, with **Remove limit**); see [Managing tokens](tokens.md). When a technical token is deleted, its rules go with the daily cleanup and its usage stays in the overview for its retention, named as a deleted token.

## What the limits do not cover

The limits bound how much one caller takes; they decide nothing about what a caller may read, which the permissions do ([Permissions](permissions.md)). In Audit nothing of the download limits refuses: an anonymous caller of a public remote is bounded only by the request rate of the management API's classes until the instance is switched to Enforce. A user's own view is [Your usage and download limits](../user/usage-and-limits.md).
