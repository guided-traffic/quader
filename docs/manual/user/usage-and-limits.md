# Your usage and download limits

How much you may download from Quader and how fast, where you see what you downloaded and which limits apply to you, and what a refused download means.

## What a download limit is

An installation can limit how much each caller downloads: how many requests per second and per hour, how many bytes per hour and per day from the cache and from the upstream of a remote repository, how many downloads at once, and how many refused requests per hour. Your sessions in the browser and every personal token of yours share one budget; a technical token of a system has a budget of its own.

Every installation starts in the mode **Enforce**: the limits apply. In the mode **Audit**, which an administrator can switch to, everything is counted and shown, and nothing is refused or slowed. Your **Usage** page says which mode is in force.

## Your usage page

Open the user menu, then **Profile**, then the tab **Usage**. The page shows:

- **Your limits**: one block for each place whose rule applies to you, the instance and each project or repository with a rule of its own. Each block says where the limits come from ("Your limits come from the default for every signed-in user at the instance, profile Standard.") and lists every figure. A figure with a window (an hour, a day, the downloads in progress, the refused requests) has a bar of what you used against the limit, with the amount beside it ("246 GB of 300 GB"); from 70 % it says **Close to the limit**, at the limit **Limit reached**, or in Audit **Would have been limited**. A rate per second is shown as its number. An unlimited figure says **Unlimited**.
- **Explain for a repository**: type or choose a repository you can read and select **Explain**. The suggestions leave out the system repositories of the project Upstream, whose limits apply to no caller. The answer lists, step by step, which rule applies to you there and why, the limits that apply, what a project's maximum lowered, and for a remote repository the ceiling of all callers like you.
- **Last 24 hours**: three small charts in five-minute steps, your requests, the bytes from the cache and the bytes from the upstream. A mark over the requests shows a five-minute step with refused requests or a limit. Point at a chart, or focus it and use the arrow keys, to read one step out; **Show as table** shows every step as a row.
- **By repository** and **By credential**: what you downloaded per repository, and per browser session and personal token. The period buttons at the top (**Last hour**, **Last 24 hours**, **Last 30 days**) change these two tables.

If you downloaded nothing in the last 24 hours, the page says so.

**Downloads in the background count as yours.** When you ask a remote repository about one version without downloading it, the manifest of
an image tag for example, the repository downloads that version in the background ([Downloads a client only asked about](../admin/repositories.md#downloads-a-client-only-asked-about)).
That download is charged to you: its requests to the upstream and the bytes read from it appear in your usage as if you had pulled the
version, and when your upstream budget is used up nothing is downloaded.

## When a download is refused

A client that meets a limit gets the answer `429 Too Many Requests` with the time to wait. npm, Docker, Podman and curl show the reason; the NuGet clients, APT and ansible-galaxy show only the status.

| Limit | What happens |
|---|---|
| Requests per second or per hour | The request is answered 429 with the time to wait. A refused request does not count against your limit: wait that long and your next request is admitted. The built-in limits are set so that normal use does not reach them: a pull of an image of 40 layers is about 50 requests, an install of 500 npm packages about 500, and even the limit for callers without a sign-in takes 1,000 at once |
| Bytes per second | The download is slowed down; nothing fails |
| Bytes per hour or per day | The next download is refused before it starts; a download that has started finishes. The time to wait is the rest of the hour or the day, and it is meant: there is no room before then |
| From the upstream | What is in the cache is still served; only what Quader would have to fetch first is refused |
| Downloads at once | A further download waits up to 10 seconds, then is refused. While a server already holds many waiting downloads, a further one is refused at once |
| Refused requests | After too many refused requests in an hour (names you may not read or write, a repository that does not exist), every request is refused for the block time, 15 minutes by default. A 429 of a limit is not one: a client that sends past its request limit is slowed to the limit, never blocked for it |

### What your client shows

What each client does when it meets a limit, as the tests of each format see it (2026-11-20, with the client's defaults unless a
step says otherwise). A request limit answers with a short wait (one or two seconds); a byte limit per hour or day answers with the
time until the hour or day has room again, up to an hour or a day.

| Client (version tested) | At a request limit | At a byte limit |
|---|---|---|
| `npm install` (npm 11) | Retries twice with its own pause (10 s, then up to 60 s; it does not read the time to wait) and succeeds, since the refused request did not count and the retry is admitted. The built-in limits take an install of hundreds of packages at once, so a normal install does not meet one | Retries twice, then prints `npm error code E429` and `npm error 429 Too Many Requests - GET <tarball> - The download limit of … is used up. Try again in 60 minutes. Your limits: …`; exit 1, after about 70 s |
| `dotnet restore` (.NET SDK 10.0.403) | Retries about once a second without reading the time to wait (`Retrying 'FindPackagesByIdAsync' …`, `Failed to download package '…'`, each with `429 (Too Many Requests)`) and succeeds, since the refused request did not count and the retry is admitted; exit 0, after about 6 s at a limit of one request a second. A client that is refused on every retry fails after six attempts with `error NU1301: …` | Retries, then `error : Failed to download package '…' … 429 (Too Many Requests).`; exit 1, after about 6 s; the reason is not shown |
| `docker pull` (Docker 29.9) | Does not retry: `error from registry: The request limit of … is used up. Try again in 2 seconds. Your limits: …`; exit 1 at once. The built-in limits leave room for many pulls at once (a pull of 40 layers is 48 requests), so a normal pull does not meet one | Does not retry: `error from registry: The download limit of … is used up. Try again in 60 minutes. …`; exit 1. A refused `HEAD` has no message: `unexpected status from HEAD request to …: 429 Too Many Requests` |
| `docker push` (Docker 29.9) | Does not retry: `error from registry: Too many requests; retry after the period in the Retry-After header.`; exit 1. A push of one platform with up to about 127 layers fits | — |
| `podman pull` (Podman 5.8) | Waits the time it is told and retries; succeeds | Waits what it is told, at most 60 s per try, five tries per request and the whole pull three more times (`failed, retrying in 1s ... (1/3)`), then `Error: … toomanyrequests: The download limit of … is used up …`; it fails after about 16 to 20 minutes, with the reason. The wait is meant (see below) |
| `apt-get update`, `apt-get download` (APT 3.0) | Retries the refused file (an `Ign:` line, then `Get:`) and succeeds | Retries three times (`Ign:1`), waiting longer each time, then `E: Failed to fetch <file>  TooManyRequests 429  Too Many Requests`; exit 100, after about 90 s; the reason is not shown |
| `ansible-galaxy collection install` (ansible-core 2.21) | Retries the refused version list, then fails on its own response cache: `[ERROR]: Missing expected 'results' in ansible-galaxy cache …`; exit 1. The message is the client's and does not name the limit | Does not retry: `[ERROR]: Failed to download collection tar from '<server>': HTTP Error 429: Too Many Requests`; exit 1; the reason is not shown |

Where a client shows only the status, the reason is on your **Usage** page and in the answer's body, for example with
`curl -sS <address>`, which prints it.

**A client that waits at a byte limit waits as long as it is told.** At a limit of bytes per hour or per day the time to wait is the
rest of the hour or the day, because there is no room before then. podman waits it, up to 60 seconds per try and about 20 tries, so
a `podman pull` in a CI job holds the job for about 16 to 20 minutes before it fails with the reason. If your job should fail sooner,
set the job's own timeout: `timeout:` of the job in GitLab CI, `timeout-minutes:` of the job or the step in GitHub Actions. The same
holds for any client that honours the wait.

A download in the UI that a limit refuses shows a notice at the bottom of the page instead ("Your download limit of 300 GB per hour is used up. Try again in 14 minutes."), with **See your usage**, which opens this page. The page you were on stays as it was. A slowed download is not announced: your browser shows its speed.

If you need more than your limits allow, ask the administrators of your installation or the Owner of the project. See also [Errors and troubleshooting](troubleshooting.md).
