# Download links

How an administrator lets large downloads go directly from the object store by short-lived signed links, what such a link costs, how the clients' network has to reach the store, and how a project Owner keeps a repository out.

## What the switch does

With **Deliver large downloads by signed links of the object store** on, Quader answers a large download with `307 Temporary Redirect` to a link of the object store that is valid for a short time, instead of streaming the bytes itself. The client fetches the file from the store with that link. Quader still decides who may download: the link is issued only after the sign-in, the permissions, the malware and signature checks and the download limits have passed, exactly as for a streamed download.

A download goes by link only when all of these hold:

- every client of the format compares every byte with a digest it knew before the download: today the blobs of container images (OCI) and the collection and role archives of Ansible Galaxy. npm (not every npm client checks), NuGet, Debian and generic files always stream; the settings page lists the downloads it reaches under **Details**, from the running version;
- the file is at least the **Minimum size** (1 MiB unless you change it);
- it is a plain `GET` of the whole file: no `HEAD`, no range, no conditional request, nothing under `/files/`;
- neither the repository the client addressed nor the repository that holds the file opted out (below);
- the request did not come through a Bastion;
- a connection of the object store holds the file and can serve links (below).

Everything else is streamed by Quader as before. A new installation has the switch off.

## The settings page

**Admin area › Settings › Storage** (`/admin/settings/storage`) is shown to whoever holds `settings:read`; changing it and **Test link** need `settings:write`. An auditor reads the state as text.

The section **Download links** shows:

- the state: **Off** ("Quader delivers every download itself."), **On** with the formats whose large downloads now go from the store, or **On, but no link is issued** when no connection can serve one;
- **Can serve links** and **Cannot serve links**: which connections give links now, as the replica that answered the page decides it;
- the switch with what it means, the sentence on speed limits, and the link to this page.

**Details**, closed, holds:

| Field | Value of a new installation | What it does |
|---|---|---|
| **Minimum size in MiB** | `1` | Smaller files are always delivered by Quader. 0 to 1,048,576 |
| **Link valid for, in seconds** | `60` | How long a link works. 10 to 3,600. A link can be passed on while it is valid; a short life is safer |
| **Public address of** a connection | the connection's endpoint | The `https` origin your clients reach the object store at, such as `https://objects.example.com`; no path. Left as the endpoint (or emptied), it is not saved and links follow the endpoint. A Bastion's [direct reads](bastion.md#direct-reads-from-the-object-store) use the same address. An encrypted connection has the field too: it serves no client, and its address is where a Bastion with direct reads on reads its files, each with a link and that file's key. Under its field stand that it is encrypted and why a Bastion cannot read it, where one cannot |

**Save changes** sends only what changed. Values outside their ranges and an address that is not an `https` origin are refused at the field before anything is sent; an address under a host name of Quader is refused by the server at the field. A change is recorded as `storage.download_links.changed` in the audit log with the values before and after, and every replica follows within two seconds. Switching off is part of the restore journal, so a restore to an earlier point keeps links off.

## What a link costs

- **The bytes no longer pass Quader.** Quader does not see them, so it cannot stop a download whose stored object was changed in the bucket; the client's own check of the digest does. This is why only the formats whose clients all check are reached. A streamed download is still checked by Quader.
- **A link is a bearer for its life.** Whoever holds it can fetch the file until it expires, also from another machine; revoking the caller's token does not end it. Keep **Link valid for** short.
- **Speed limits do not slow a link.** A download handed to a link is not slowed by the caller's speed limit; its size is charged to the caller's hourly and daily budgets when the link is issued ([Download limits and usage](download-limits.md)). To bound a caller's volume, use those budgets.
- **Quader counts at the issue.** The download, its size and the audit event `artifact.downloaded` (with the delivery `link`) are recorded when the link is handed out; Quader does not learn whether the client fetched it.
- **The object store's own limits apply.** The store's request and bandwidth limits of your provider or account count the clients' fetches as well.
- **An encrypted connection never serves a client's link**: Quader decrypts and streams its content to clients. Only a Bastion with [direct reads](bastion.md#direct-reads-from-the-object-store) on gets a link to such a file, with that file's key.

## Clients behind a proxy or a firewall

A client that gets a link must reach the object store's **public address** itself, with `https`, without going through Quader. Check before you switch on:

- the clients' network (CI runners, build machines, developer laptops) may connect to the store's address; a firewall that lets the clients reach only Quader breaks every large download of the reached formats;
- a client behind an HTTP proxy needs the store's host on the proxy's allow list, or on its own no-proxy list where the store is internal;
- the clients trust the store's certificate;
- the address is not a host name of Quader nor below one: container clients keep their credential for Quader's host and its subdomains and would send it to the store, which refuses such a link. The page refuses such an address.

**Test link** under each address sends the address as typed (saved or not) and shows a link to a small test file, valid for 60 seconds, with a `curl` command. Open the link in your browser or run the command on a machine of your clients, or of a Bastion's network for its direct reads: when that network reaches the store, the file `quader-link-test.txt` reads `Quader download link test: this link works.` Quader itself sends no request to the address, so only a client of your network can tell. A connection that is failing answers that no test link was made.

## A connection that cannot serve links

Each connection is checked at every start of a replica and when the switch goes on. Under its address, or above the switch when no connection can serve a link, the page says why:

| Reason | What to do |
|---|---|
| The connection is encrypted | Nothing: Quader delivers its content to clients itself; a Bastion's direct reads are not affected |
| The address does not use https | Set an `https` public address in **Details** |
| The address lies under a host of Quader | Choose an address outside Quader's host names |
| The object store did not answer a test link as expected | The store refused the link, or ignored the content type or the file name it asked for; check the store's signature support and the address, then restart a replica or switch the delivery off and on to check again |

While the switch is on, a replica also writes a warning to its log once per connection that cannot serve links.

## A repository's opt-out

A project Owner, or an administrator, keeps a repository's downloads away from links in the repository's settings: the closed section **Download links** after **Signatures** holds **Downloads of this repository may go by signed links of the object store**, checked by default. Uncheck it and **Save changes**: no download of this repository goes by link, whatever the installation's switch says; the change is recorded as `repository.narrowed`. For a virtual repository, the addressed repository and the member that holds the file must both allow it.

The box can be changed while the installation's switch is off: a sentence under it then says that the installation does not hand downloads to links at the moment and that the choice takes effect when an administrator switches them on, so an Owner keeps a repository out before that. It is disabled, with the reason under it, only on a repository of a format not every client of which checks (npm, NuGet, Debian, generic). Through the API the same member is `storage.downloadLinks`:

```shell
# example values: the project, the key, the ETag and the token variable
curl -X PATCH "https://quader.example.com/api/v1/projects/web/repositories/images" \
  -H "Authorization: Bearer ${QUADER_TOKEN}" -H 'If-Match: "7"' \
  -H 'Content-Type: application/merge-patch+json' \
  -d '{"storage": {"downloadLinks": false}}'
```

## Through the API

| Operation | What | Permission |
|---|---|---|
| `GET /api/v1/download-links/settings` | the switch, the details with their `ETag`, each connection's state with its reasons, the downloads reached | `settings:read` |
| `PATCH /api/v1/download-links/settings` | a merge patch of `enabled`, `minSize` (bytes), `linkLifeSeconds` and `publicAddresses` (a connection's `null` returns it to its endpoint), with `If-Match` | `settings:write` |
| `POST /api/v1/download-links/test` | `{"connection", "publicAddress"}`: a test link | `settings:write` |
| `GET /api/v1/download-links/switch` | whether the switch is on and the formats it reaches | every signed-in caller |
