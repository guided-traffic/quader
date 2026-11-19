# Your usage and download limits

How much you may download from Quader and how fast, where you see what you downloaded and which limits apply to you, and what a refused download means.

## What a download limit is

An installation can limit how much each caller downloads: how many requests per second and per hour, how many bytes per hour and per day from the cache and from the upstream of a remote repository, how many downloads at once, and how many refused requests per hour. Your sessions in the browser and every personal token of yours share one budget; a technical token of a system has a budget of its own.

Every installation starts in the mode **Audit**: everything is counted and shown, and nothing is refused or slowed. In the mode **Enforce** the limits apply. Your **Usage** page says which mode is in force.

## Your usage page

Open the user menu, then **Profile**, then the tab **Usage**. The page shows:

- **Your limits**: one block for each place whose rule applies to you, the instance and each project or repository with a rule of its own. Each block says where the limits come from ("Your limits come from the default for every signed-in user at the instance, profile Standard.") and lists every figure. A figure with a window (an hour, a day, the downloads in progress, the refused requests) has a bar of what you used against the limit, with the amount beside it ("246 GB of 300 GB"); from 70 % it says **Close to the limit**, at the limit **Limit reached**, or in Audit **Would have been limited**. A rate per second is shown as its number. An unlimited figure says **Unlimited**.
- **Explain for a repository**: type or choose a repository you can read and select **Explain**. The answer lists, step by step, which rule applies to you there and why, the limits that apply, what a project's maximum lowered, and for a remote repository the ceiling of all callers like you.
- **Last 24 hours**: three small charts in five-minute steps, your requests, the bytes from the cache and the bytes from the upstream. A mark over the requests shows a five-minute step with refused requests or a limit. Point at a chart, or focus it and use the arrow keys, to read one step out; **Show as table** shows every step as a row.
- **By repository** and **By credential**: what you downloaded per repository, and per browser session and personal token. The period buttons at the top (**Last hour**, **Last 24 hours**, **Last 30 days**) change these two tables.

If you downloaded nothing in the last 24 hours, the page says so.

## When a download is refused

A client that meets a limit gets the answer `429 Too Many Requests` with the time to wait. npm, Docker and curl show the reason; the NuGet clients and APT show only the status.

| Limit | What happens |
|---|---|
| Requests per second or per hour | The request is answered 429 with the time to wait |
| Bytes per second | The download is slowed down; nothing fails |
| Bytes per hour or per day | The next download is refused before it starts; a download that has started finishes |
| From the upstream | What is in the cache is still served; only what Quader would have to fetch first is refused |
| Downloads at once | A further download waits up to 10 seconds, then is refused |
| Refused requests | After too many refused requests in an hour (for example names you may not read), every request is refused for the block time, 15 minutes by default |

A download in the UI that a limit refuses shows a notice at the bottom of the page instead ("Your download limit of 300 GB per hour is used up. Try again in 14 minutes."), with **See your usage**, which opens this page. The page you were on stays as it was. A slowed download is not announced: your browser shows its speed.

If you need more than your limits allow, ask the administrators of your installation or the Owner of the project. See also [Errors and troubleshooting](troubleshooting.md).
