# The Quader Bastion

How to run a read-only, internet-facing cache of your installation for clients outside your network: declare a Bastion in the values of an installation run from Git, or register it and issue its token on the screen, set up the main installation, install the Bastion chart with a minimal or a highly available configuration, check that it works, switch on its web interface for visitors and decide what it shows, renew its token without an outage, let it read large files from the object store directly, and what each alert of its monitoring means.

## What a Bastion is

A Bastion is the same Quader image started as `quader serve --role bastion`, installed with its own chart, `quader-bastion`, in its own namespace or cluster. It has no database, no sign-in and no administration screen. It serves what clients read from your repositories, for every format, as the main installation's own address serves it: the package paths (`/api/<format>/<repository>/…`: npm, NuGet, Debian (APT), Ansible Galaxy and the ClamAV definitions), the raw files (`/files/<repository>/…`) and the container registry at the root of its own address (`/v2/…`), so that an image is pulled as `<Bastion host>/<project>/<repository>/<image>:<tag>`. When you switch it on, it also serves a read-only web interface for visitors at `/` and `/ui/…`, with a sitemap for search engines ([The web interface for visitors](#the-web-interface-for-visitors)); `/robots.txt` is always answered. Every other path answers `404` with the code `not_found`. It holds no repository list and no permissions. For every read it asks the main installation whether the caller may read the repository, with the caller's own token, and serves the bytes from its cache when the answer is yes. A write through a Bastion is refused with `403` `read_only`.

| | The main installation | The Bastion |
|---|---|---|
| Holds | repositories, permissions, tokens, the audit trail | a cache of artifacts and metadata, and short-lived answers of the main installation |
| Serves | everything | reads of packages, images and files, for every format |
| Sign-in | all methods | the caller's token (personal, service-account or CI token) |
| Needs | PostgreSQL, the object store | the main installation, a cache volume or a bucket; Valkey is optional |
| Reachable from | your network and the endpoints you configured | the internet, through its own ingress |

The Bastion has a credential of its own, a token of the type `bst` (prefix `qdr_bst_`) that belongs to a registration on the main installation. The token lets the Bastion ask whether a caller may read a repository, read the feed of revocations and report downloads. It reads no package and cannot issue a token, change a setting or read the audit trail. Several Bastions may exist, each with a registration of its own; at most `bastions.max_registrations` (default `16`) can be registered.

### What a client names

A client names the Bastion's address where it would name the main installation's; the page **Set up client** of a repository renders every format's snippets for a Bastion ([Install through a Bastion](../user/client-setup.md#install-through-a-bastion)). For a Bastion at `https://packages.example.com` *(example)*:

| Client | What it names *(example repositories of the project `web`)* |
|---|---|
| npm, pnpm, Yarn | `https://packages.example.com/api/npm/web/npm-all/`; npm also `replace-registry-host=always` |
| `dotnet`, `nuget` | the service index `https://packages.example.com/api/nuget/web/nuget-all/v3/index.json` |
| Docker, Podman, Kubernetes | `docker login packages.example.com/web/docker-local`, the image `packages.example.com/web/docker-local/myapp:1.0`, an image pull secret for `packages.example.com/web/docker-local` |
| APT | the source `https://packages.example.com/api/deb/web/apt/`, its key `signing-key.asc` read through the Bastion, the token in `/etc/apt/auth.conf.d/` for that address |
| `ansible-galaxy` | the server `https://packages.example.com/api/ansible/web/galaxy/` |
| freshclam | `PrivateMirror https://packages.example.com/api/clamav/upstream/clamav` |

Signed content passes unchanged: APT verifies the release files with the repository's key, and an image client checks each layer, and each manifest it names by digest, against that digest, as at the main installation. Downloads of every format count in the main installation's audit trail as described under [What the Bastion does not do](#what-the-bastion-does-not-do).

### A Docker Engine mirror through a Bastion

A Docker Engine reads Docker Hub images through a mirror at the root of a host ([The Docker Engine mirror on a bound host](container-repositories.md#the-docker-engine-mirror-on-a-bound-host)). A Bastion serves such a mirror when its `config.server.base_url` is a host that the main installation binds to the repository: list the Bastion's host in `endpoints.default.hostRoots` of the chart `quader`, for example `{mirror.example.com: web/docker-hub}` *(example)*, and point the engines at `https://mirror.example.com`. The host is already a host of the main installation's default endpoint through `server.additional_base_urls`. One Bastion serves one bound host; a second bound host is a second release of the chart. A binding you change on the main installation reaches the Bastion at its next handshake (`bastion.main.handshake_interval`, default `1m`).

## What you need

| Where | What |
|---|---|
| Main installation | A registration and a token: declared in the values ([the next section](#declare-the-bastion-in-the-values)), or made on the screen (the two sections after it); the address the Bastion connects from on its card ([The addresses a Bastion connects from](#the-addresses-a-bastion-connects-from)); its address in `server.additional_base_urls` |
| Bastion namespace | The Secret of the token; a TLS certificate source for the listener (cert-manager or a Secret); the main installation reachable over HTTPS; a public name and certificate on the Bastion's ingress |
| Network | The Bastion reaches the main installation's endpoint on port 443; the main installation does not reach the Bastion |

## Declare the Bastion in the values

For an installation run from Git, declare the Bastion in the values of the chart `quader`. The registration and its token then exist after every install and upgrade, also after a fresh database, without anyone registering it again on the screen.

1. **Make the token's value once.** Run `quader admin bastion token generate`; it prints a new Bastion token and needs no configuration and no database, for example `kubectl -n quader exec deploy/quader-api -- /usr/local/bin/quader admin bastion token generate` or `docker run --rm <the Quader image> admin bastion token generate` *(examples)*. **Issue token** on the card of a registration that is not declared gives a value as well. A random string from a secret generator does not work: the value must be a Bastion token (`qdr_bst_`, 57 characters with its checksum), because the Bastion and the main installation both check that before anything else.
2. **Put the value into a Secret in both namespaces**: in the main installation's namespace for its migration Job, and in the Bastion's namespace for `secrets.mainToken` of the chart `quader-bastion`. When both releases run in one namespace, one Secret serves both. The chart never creates a Secret:

```shell
kubectl -n quader create secret generic quader-bastion-token --from-literal=token='qdr_bst_…'   # example: namespace and name
```

3. **Declare it** in the values of the chart `quader`:

```yaml
bastions:
  declared:
    - name: partners                    # example; lower-case letters, digits and -, at most 20 characters
      description: Partner access       # example; optional
      egress: [10.252.0.0/16]           # example; the addresses the Bastion connects from (see below); optional
      token:
        existingSecret: quader-bastion-token   # example; the Secret of step 2
        key: token                      # default
```

4. **Roll out.** The migration Job (`quader migrate`) of the install or upgrade reads the Secret and creates the registration with the preset Internet and every other default of a new registration, or brings an existing one in line: its description, its addresses and its token follow the entry. Only the migration Job mounts the Secret; the database keeps the token's hash, as it does of every token. A missing Secret, or a value that is not a Bastion token, stops the Job with the name of the entry, before any pod rolls.

On the card the registration carries the mark **Helm configuration** (a lock and the words) beside its state; its info sign says that the description, the addresses and its token are set there. Its description is shown and not edited on the card; its addresses stand in their fields with **Add address**, greyed out, and pointing at one (or tapping it) shows "Set in bastions.declared of the Helm configuration."; **Delete** stands greyed out in the head, and pointing at it (or tapping it) shows "Set in bastions.declared of the Helm configuration: remove it there first, then delete it here." Under **Tokens** its token is named "Token of the Helm configuration", with "Secret quader-bastion-token" *(example)* beside its hint and **Never** under **Expires**: the declared token does not expire, it ends when its Secret holds another value. No other token is issued for a declared registration: **Rotate token** stands greyed out with "Its token is set in bastions.declared of the Helm configuration: put a new value into its Secret.", and the API answers an issue with `409 managed_by_config`, since the Secret is the one source of its credential. **Invalidate** on the declared token works as the last resort against a leak; its dialog says that the next upgrade then stops at its migration until the Secret holds a new value. Everything else on the card stays yours: the switches, the limits, **Disable…** and **Enable**. These survive every upgrade; after a fresh database they are the defaults of a new registration again, so set them anew or keep them in your notes.

**Rotate the token** by putting a new value into both Secrets and rolling out the main installation: its migration Job stores the new value and revokes the old one at once, so update the Bastion's Secret first, or at the same time. The Bastion re-reads its token file without a restart ([Renew the token with an overlap](#renew-the-token-with-an-overlap)). A value that was revoked, by a rotation or because it leaked, never works again: putting it back into the Secret stops the migration Job.

**Take a declaration out** of the values to manage the registration on the screen again: the next migration Job revokes the declared token, logs a warning naming the registration, and keeps the registration with everything else. Delete it on the card if it is no longer needed.

After a restore the registration and its token are as at the restore point until the upgrade that turns `restore.enabled` off; its migration Job applies the declaration again ([After the restore](backup-and-restore.md#after-the-restore)).

## Register a Bastion

For an installation not run from Git, or a Bastion you try out, register it on the screen:

1. Open **Admin area › Bastions** and choose **New Bastion**. The screen needs the permission to read endpoints (`endpoints:read`: the roles admin, security-admin, auditor and repository-admin); the actions need the permission to change them (`endpoints:write`: admin and security-admin). Revoking a token alone needs `endpoints:write` or `tokens:revoke`.
2. Enter a **Name** that starts with a lower-case letter and holds lower-case letters, digits and `-`, at most 20 characters (`partners`, for example). The name cannot be changed later. The **Description** is free text.

A registration is a principal of its own kind, `bastion:<name>`. It has no permission policy, no role, no group and no package ownership; a policy, role binding or ownership that names it is refused with `422` `principal_not_allowed`. It never belongs to the group of all authenticated users. A registration can be **disabled**, which tells its Bastion to stop work until it is **enabled** again (see [Disable or delete a Bastion](#disable-or-delete-a-bastion)), and **deleted**, which revokes its tokens at once. Each action is an audit event (`bastion.created`, `bastion.updated`, `bastion.disabled`, `bastion.enabled`, `bastion.deleted`).

## Issue the token

1. On the card of the registration open the section **Tokens**: without a token it says "No token yet." with **Issue token** beside it. Choose **Issue token**. A Bastion you have just created has its card open; on a page of several Bastions, a card folded to its head opens with the chevron before its name.
2. Enter a **Description** that says which rollout or year the token is for, and a **Lifetime in days**. If your sign-in is not recent, Quader asks you to sign in again; your entries are kept, and the dialog opens again with them afterwards. Leave the lifetime empty for the default, `auth.tokens.bst.default_ttl` (`8760h`, one year); the longest allowed is `auth.tokens.bst.max_ttl` (`9480h`), and a longer entry is refused with `invalid_ttl`. A Bastion token always expires.
3. Copy the token from the panel **Your new token** (**Copy**, or **Show** and copy by hand). It has 57 characters and is shown once; Quader stores only its hash. **I have copied it** closes the panel.
4. Put the token into the Secret that the chart reads. The chart never creates a Secret:

```shell
kubectl -n quader-bastion create secret generic quader-bastion-token --from-literal=token='qdr_bst_…'   # example: namespace and name
```

At most `auth.tokens.max_active_per_principal.bst` unrevoked tokens (default `3`) can exist per registration, so a renewal with an overlap fits and a forgotten token does not pile up; a fourth answers `409` `token_limit_reached`. Bastion tokens do not appear on the Tokens screen. They are managed here, on the card of the registration, where each token shows its hint (the first characters, to recognise it), its expiry, and when it last authenticated a request.

The card's section **Tokens** is one line: "The next expires in 12 months" *(example)*, or "None expires". A token that expires in less than 30 days carries the mark **Expires soon** beside its time under **Expires**, an expired one the mark **Expired**; the section's line then turns into a warning, "“partners 2026” expires in 3 weeks." *(example)* or "“partners 2026” has expired.", and the section opens by itself. Without a token it says "No token yet." with **Issue token**. Inside, **Rotate token** issues a new token beside the old one ([Renew the token with an overlap](#renew-the-token-with-an-overlap)), and **Invalidate** on a token ends it at once, for good.

## The addresses a Bastion connects from

The Bastion sends the address of the real client as the only entry of `X-Forwarded-For` and `Forwarded`, and its own host as the forwarded host. The main installation believes them only for a request that carries this registration's token and comes from one of the addresses its card lists under **Addresses this Bastion connects from**; then its rate limits, its failed-sign-in throttle, its blocks and its audit trail apply to the client and not to the Bastion, and so does the check of a client's token that the Bastion asks for. The token of another Bastion from these addresses, any other request from them (another workload behind the same NAT, for example) and this Bastion's token from anywhere else cannot choose the client's address or the host links are written for.

A new registration lists no address. Until you add one, every client through the Bastion counts as the Bastion's own address on the main installation: one budget and one block for all of them, and the audit log names the Bastion's address instead of the client's. After the Bastion's first handshake the card warns at the head of **Limits** with the address each replica connected from, "Its last contact came from 198.51.100.21, which is not listed: callers through it share one budget and one block." *(example)*, and offers **Add 198.51.100.21**: one click lists it. Check the address before you add it: it is the one the main installation saw, after your own ingress, and the card would show the same line for a stolen token used from elsewhere. Where a replica made contact but reported no address the card can offer, the card says "No address this Bastion connects from is listed: all its callers share one budget and one block." instead; before the first contact it says nothing, since there is nothing to add yet.

To edit the list by hand, open **Details** on the card: under **Addresses this Bastion connects from** (its info sign says what the list does) each address or network has a field; **Add address** adds one, **Remove** takes one out, and **Save changes** saves the list (the permission `endpoints:write`; who may only read sees the list as text). An entry is an IPv4 or IPv6 address (`198.51.100.20`, saved as `198.51.100.20/32`) or a network in CIDR form (`198.51.100.16/28`, `2001:db8:4::/64`) *(examples)*; at most 32 entries. List a host address or a small range, never a whole network: a network covering every address (`0.0.0.0/0`, `::/0`) is refused, and so is a network written with bits beyond its prefix (`10.1.2.3/8` is refused with the hint `10.0.0.0/8`). Do **not** put the address into `server.trusted_proxies` as well: an address there is believed for every request, Bastion token or not, and the main installation logs a warning when the two overlap.

Through the API the list is the member `egress` of the registration: `PATCH /api/v1/bastions/<name>` with `{"egress": ["198.51.100.20/32"]}` and the registration's `ETag` in `If-Match` replaces it, `[]` empties it; `POST /api/v1/bastions` takes it at creation. Each change is the audit event `bastion.updated` with the field `egress` and the list before and after. A change reaches every replica of the main installation within a few seconds. A restore to an earlier point brings back the list of that point.

## Settings on the main installation

One setting makes the main installation write URLs on the Bastion's host. Set it in `config` of the chart `quader`:

```yaml
config:
  server:
    trusted_proxies: ["10.244.3.17", "10.244.5.9"]           # example: your own ingress pods, as before; not the Bastion
    additional_base_urls: ["https://packages.example.com"]   # example: the Bastion's config.server.base_url
```

| Setting | Why the Bastion needs it |
|---|---|
| `server.additional_base_urls` | The Bastion sends its own base URL as the forwarded host. With the URL in this list, every address the main installation renders for the Bastion, such as npm's tarball links, is on the Bastion's host. Without it the main installation answers with its own address, the Bastion detects that, treats the main installation as unreachable and serves no client a document with the wrong addresses |

The entry must equal the Bastion's `config.server.base_url`: the same scheme and host, and the port if one is set. The comparison ignores a trailing slash and upper case in the scheme and host. The page **Set up client** of a repository uses the same list: they offer a Bastion as a place to install from only when its reported address is in it.

## What a Bastion may distribute

**Public** is your main installation's level: public on its network, usually your intranet. What leaves it through a Bastion is granted twice. You, as an administrator, allow the projects whose repositories may be distributed through a Bastion; a new Bastion allows none. A project's Owner then chooses, per repository, the Bastions that distribute it, among those that allow the project. Not every repository that is public on the intranet goes to the internet, and a private repository that a partner reads through a Bastion with his token needs the same choice.

**This release stores the two lists; it does not yet hold a Bastion to them.** Until the next step lands, a Bastion still serves what each caller's credential may read on the main installation, as before. The card's section **Projects** and the repository's **Bastions** in its settings are not there yet either: use the API below.

- **Allow a project:** `PUT /api/v1/bastions/<name>/projects/<project>` (the permission `endpoints:write`). It distributes nothing by itself; a second call changes nothing. The audit event is `bastion.project.allowed`.
- **See what is allowed:** `GET /api/v1/bastions/<name>/projects` (`endpoints:read`) lists the projects with the keys of the repositories whose Owner chose the Bastion, and `totals` counts them all.
- **Take a project off:** `DELETE /api/v1/bastions/<name>/projects/<project>`. Its repositories' choices of that Bastion are deleted with it, in the same step; allowing the project again distributes nothing until its Owner chooses again. The audit event `bastion.project.removed` names the repositories whose choice went, and a restore to an earlier point takes the project off again.
- **The project upstream's repositories** are yours to choose, as their other open settings: a Bastion's ClamAV installations update through it only while `upstream/clamav` is distributed there, so allow the project `upstream` and choose the Bastion in `upstream/clamav`.

A repository that moves to another project loses its choices: the new project's Owner chooses again.

## Install the Bastion

The chart `quader-bastion` is published with the chart `quader`, with the same version. These values are rendered into the Bastion's configuration file; `values.yaml` of the chart lists every setting with its default.

### A minimal install

The values a render needs: the Bastion's own URL, the pods of its ingress controller, the main installation's URL, the Secret of the token and a certificate source for the listener.

```yaml
config:
  server:
    base_url: https://packages.example.com          # example: the public address of the Bastion
    trusted_proxies: ["10.244.0.0/16"]              # example: the pods of the ingress controller in front of the Bastion
  bastion:
    main:
      url: https://quader.example.com                # example: the main endpoint of the main installation
secrets:
  mainToken:
    existingSecret: quader-bastion-token             # example: the Secret of the previous section; key "token" is the default
tls:
  certManager:
    issuerRef:
      name: internal-ca                             # example: a ClusterIssuer; or set tls.existingSecret
```

```shell
helm install quader-bastion quader/quader-bastion -n quader-bastion -f values.yaml --version <version>   # example: the version of the main installation
```

This gives two replicas (the default, and the minimum while `highAvailability.enforce` is on) that spread across nodes behind a PodDisruptionBudget, a cache on an ephemeral volume of each pod, no Valkey, an Ingress for HAProxy Ingress and a NetworkPolicy that admits the Bastion's port only from the namespace of the ingress controller (`networkPolicy.ingress.https.from`, default `haproxy-ingress`) and its metrics port only from the namespace `monitoring`. Each replica fills its own cache, so a package is fetched once per replica, and the cache is empty after a restart. The rate limits, counters and blocked addresses work per replica, at the configured value divided by the number of replicas.

`config.server.trusted_proxies` is required while `ingress.enabled` is on (it is by default), and the render fails without it. Behind the Ingress every request reaches the Bastion from a pod of the ingress controller; the Bastion takes the client's address from `X-Forwarded-For` only when the peer is in this list. Without it every caller has the controller's address, so one client's wrong tokens put every unverified credential of every partner into the timeout, a flood blocks the Bastion for everyone, and the anonymous limits apply to the whole internet together. List the address range of the controller's pods as narrowly as you can; the Bastion refuses a list entry that covers every address. With `ingress.enabled: false`, list the addresses of whatever sits in front instead. This setting is the Bastion's own and is not the main installation's `server.trusted_proxies` of the section above.

The chart does not restrict where the Bastion pods connect: its network policy holds rules for incoming connections only, as the policies of the main installation's chart do. Because the Bastion faces the internet, restrict its outgoing connections with a policy of your own. A Bastion needs DNS; the main installation's address on its port (`config.bastion.main.url`, and `config.bastion.main.packages_url` where set); Valkey and its Sentinels if you configure them; the bucket with the bucket backend, and its credential endpoint if the credentials come from a workload identity or the default chain; and the span collector if tracing is on. It needs no upstream registry, no identity provider and no PostgreSQL. Earlier releases of the chart rendered such a policy from the values `networkPolicy.egress`; the chart now refuses those values, so remove the block from your values file before you upgrade and carry what it allowed into a policy of your own.

Both URLs must be `https`. The Bastion refuses to start with a plain `http` URL for the main installation. If the main installation's certificate comes from a private CA, give the Bastion the bundle with `trustedCA.existingConfigMap` or `trustedCA.existingSecret` (one key holding the whole bundle).

### A highly available install

For an installation that clients depend on, share the cache and the counters between the replicas, so that a replica that restarts or is added starts warm and a client that is limited or blocked on one replica is limited on all of them:

```yaml
replicaCount: 3                                     # example; at least 2
config:
  server:
    base_url: https://packages.example.com          # example
    trusted_proxies: ["10.244.0.0/16"]              # example: the pods of the Bastion's ingress controller
  bastion:
    main:
      url: https://quader.example.com                # example
    cache:
      backend: bucket                               # default: volume
      bucket:
        endpoint: https://object.storage.eu01.onstackit.cloud   # example
        region: eu01                                # example
        bucket: quader-bastion-partners              # example
  valkey:
    mode: sentinel                                  # default: none
    sentinel:
      addresses:                                    # example: three or more entries while highAvailability.enforce is on
        - quader-bastion-valkey-sentinel-0:26379
        - quader-bastion-valkey-sentinel-1:26379
        - quader-bastion-valkey-sentinel-2:26379
      primary_name: quader-bastion                   # example
secrets:
  mainToken:
    existingSecret: quader-bastion-token             # example
  signingKey:
    existingSecret: quader-bastion-signing           # example: 32 random bytes, key "key"
  cacheBucket:
    existingSecret: quader-bastion-s3                # example: access-key-id and secret-access-key
  valkey:
    existingSecret: quader-bastion-valkey            # example; default "": no authentication
tls:
  certManager:
    issuerRef:
      name: internal-ca                             # example
metrics:
  serviceMonitor:
    enabled: true                                   # default: false
  prometheusRule:
    enabled: true                                   # default: false
```

| Choice | What it changes |
|---|---|
| `config.bastion.cache.backend: bucket` | The cache lives in an object-store bucket that every replica reads and fills. The bucket and its credentials Secret are yours; the chart creates neither |
| `tmp.sizeLimit` and `config.bastion.cache.spool_max_bytes` | A fill is written to temporary storage while it streams, and each replica holds at most `spool_max_bytes` (default `2GiB`, at least `bastion.cache.max_object_bytes`) of them at once; a fill that would pass it is passed through and not cached. With the bucket backend, or the volume backend with `secrets.cacheKey`, the chart sizes the pod's `/tmp` from it, `spool_max_bytes` plus 256 MiB (2304 MiB by default), and refuses a smaller `tmp.sizeLimit`, because a pod whose temporary volume passes its limit is evicted. With the volume backend `cacheVolume.size` must hold `bastion.cache.max_bytes` plus the spool. Container images bring the largest artifacts: a layer above `max_object_bytes` (default `2GiB`) is passed through on every request. Raise the spool, and `max_object_bytes` with it, if you want artifacts of several GiB cached, and give the nodes the storage. A client that asks for a range of an artifact that is not cached gets its bytes as the fill passes them, the last 64 KiB once the fill is verified, and a second client of the same artifact on the same replica follows the running fill instead of fetching it again |
| `config.bastion.cache.max_bytes` | The bytes the cache of a replica keeps, or of the release with the bucket backend (default `20GiB`, of which `config.bastion.cache.documents_max_bytes`, default `2GiB`, are for metadata and `config.bastion.gui.max_bytes`, default `4GiB`, for the public catalogue of a web interface, reserved whether or not it is on). The artifacts get `max_bytes` less both shares, and that must hold at least one file of `config.bastion.cache.max_object_bytes` (default `2GiB`): the chart refuses a smaller one, and the Bastion does not start with it, naming the four keys, because it would keep no artifact. For a small cache lower the others with it, for example `max_bytes: 2GiB` with `documents_max_bytes: 256MiB`, `gui.max_bytes: 256MiB` and `max_object_bytes: 1GiB` (and `spool_max_bytes` as low as `1GiB` if you like). The cache never takes in more: a file that would pass it is sent to its client and not kept, and the cache evicts down to 85 % at once instead of waiting for its regular sweep (`config.bastion.cache.sweep_interval`, default `5m`), so a flood of distinct files does not fill the volume or get the pod evicted. It evicts what was read only once first, least recently read first; a file read a second time from the cache is kept until then, up to 80 % of the cache (`quader_bastion_cache_protected_bytes{area}` shows how much), so a flood of single requests for distinct files does not evict what your clients read again. `quader_bastion_cache_no_room_total{area}` counts the files sent without being kept and `quader_bastion_cache_sweeps_total{trigger="size"}` the sweeps the size started. Both rising for long means that the cache churns: more distinct files are read than it holds, and what other clients read only once is evicted and read again from the main installation. Raise `max_bytes` with `cacheVolume.size` if that is your normal load, or look at who reads so much. With the bucket backend and several replicas the bound is an estimate: each replica learns what the others wrote when it lists the bucket |
| `config.bastion.cache.fill_wait_max` | With the bucket backend and Valkey, a replica whose request finds another replica filling the same artifact waits for that fill, sending nothing to its client, up to this time (default `10m`, 30 s to 1 h), and then fetches the artifact itself. A client that gives up before the first byte retries and waits again; with the volume backend and `cacheVolume.routeByPath`, every request for a path reaches one replica and follows its fill |
| `config.valkey.mode: sentinel` | Valkey holds the shared limits, the block list, the fetch coordination and the main installation's answers. It is optional: losing it costs speed, never correctness (the replicas fall back to their own state). Standalone Valkey is for development and refused while `highAvailability.enforce` is on |
| `secrets.valkey`, `secrets.valkeySentinel`, `secrets.valkeyCA` | The user and password of Valkey, the user and password of its Sentinels (`valkey.sentinel.username_file`, `valkey.sentinel.password_file`) and the CA that signed their certificates. A Valkey whose Sentinels require a password needs `secrets.valkeySentinel`; without it the Bastion logs `valkey sentinel authentication is not configured` and cannot reach the Sentinels, so it runs with its own state per replica. Set `usernameKey: ""` for the default user |
| `secrets.signingKey` | Required with the bucket backend or Valkey. Records in the bucket and in Valkey are signed with it, so whoever can write there without the key cannot make the Bastion serve other bytes. Replacing the Secret makes every record unverifiable and costs one refill |
| `secrets.cacheKey` | Optional. With it the Bastion encrypts every object of its cache store, bucket or volume, under keys derived from this one Secret of 32 random bytes (raw or base64, for example `openssl rand -base64 32`); without it the cache is plain. Replacing the Secret rolls the pods and starts an empty cache that refills, and the new pods delete the old key's objects within their first hour; a lost key costs the cache and nothing else. With the volume backend the fills in progress then go to the pod's `/tmp`, which the chart sizes from `config.bastion.cache.spool_max_bytes` |
| `cacheVolume.type: persistent` with `cacheVolume.routeByPath: true` | The alternative without a bucket: a persistent volume per pod (a StatefulSet), and HAProxy Ingress routes a path to the same replica so that the volumes together hold more than one. It needs HAProxy Ingress to accept the chart's backend snippet, and the chart refuses it together with the bucket backend |
| `autoscaling.enabled: true` | The Bastion's request budget towards the main installation is divided by `autoscaling.maxReplicas` instead of `replicaCount` |
| `config.bastion.answers.max_entries` | The main installation's answers a replica keeps in memory, least recently used first (default `200000`): per caller its credential, its read on each repository it reads and, for an image client's registry token or a token narrowed to some packages, its read on each package it reads, about three answers per image pull. Size it to the callers of one `bastion.answers.period` (default `5m`) times the repositories and packages each reads; an answer pushed out costs its caller one request to the main installation at its next read, never a wrong answer |

A Bastion has an imprint and a privacy notice of its own: `config.ui.legal.imprint.en`, `.de` and `config.ui.legal.privacy.en`, `.de`
of the chart `quader-bastion`, Markdown, at most 64 KiB each, as for the main installation
([Imprint and privacy notice](configuration.md#imprint-and-privacy-notice)). The Bastion's administrator answers for them; they name
whoever operates this Bastion and are never taken from the main installation. The Bastion renders them when it starts and shows them
in the footer of its web interface while that is on ([Imprint and privacy notice of the web interface](#imprint-and-privacy-notice-of-the-web-interface)).

The chart's `ingress` values carry annotations of HAProxy Ingress (a request limit per client address and a limit of connections). Another ingress controller ignores them: the Bastion's own limits and blocking still hold, the coarse limit in front of them does not. HAProxy Ingress counts per TCP peer address, so behind a load balancer that hides the client address, every client shares one budget until you switch on its proxy protocol or turn `edgeLimit.enabled` off.

## Limits and blocking

A Bastion protects itself and the main installation in two stages: a coarse limit at HAProxy Ingress, and its own limits per client address, which know credentials and misses. The Bastion's own figures are no chart values: they belong to the Bastion's registration at the main installation and reach every replica with its next handshake, without a restart. A replica stores the last figures it received and uses them when it starts while the main installation cannot be reached; a replica without them starts with the strict defaults below until it reaches the main installation. An administrator changes them on the Bastion's card ([Limits on the card](#limits-on-the-card)); the edge limit is the chart's. The defaults let one normal command of every client through, and two at once from one address, and still refuse and block an address that floods:

| Figure | Default | What it counts per client address | What a normal command needs |
|---|---|---|---|
| requests without a credential | `50` a second, burst `1000` | requests without a credential | an anonymous `docker pull` of an image of 40 layers: 48 requests in 0.3 s; an `npm ci` of 475 packages: 475 in 2.5 s |
| requests with a credential | `200` a second, burst `4000` | requests with a credential | an install of up to 4,000 packages at once |
| misses without a credential | `1000` a minute | requests that need the main installation, without a credential | on an empty cache every request is one: the pull above is 48 |
| misses with a credential | `4000` a minute | the same, with a credential | as above |
| blocking | `3000` refusals in a minute | refused requests before the address is blocked for 15 minutes, doubling up to 24 hours | none: an address is blocked only when it sends, after its burst, at least twice the anonymous rate or 1.25 times the rate with a credential |
| `edgeLimit.requestsPerSecond` | `1000` | HTTP requests in the last second, also on a kept-alive connection, without a burst; refused by the ingress with `429` and no `Retry-After` | an `npm ci` is about 190 a second |
| `edgeLimit.connections` | `256` | requests in flight at once | `npm ci` up to 15, `docker pull` 3, APT up to 10; `dotnet restore` on Linux has no bound of its own, so raise this value if a large restore through the Bastion is refused by the ingress |

The figures count per client address, not per person: many people or build jobs behind one NAT share them. Such an address can be exempted from every limit, timeout and block of the Bastion in its registration's exempt networks (**Networks without flood protection** under **Details** on its card), and from the ingress's limit with `edgeLimit.exemptCIDRs`. Keep `edgeLimit.requestsPerSecond` above the Bastion's figure for requests with a credential; the chart no longer checks it, every replica reports the edge figure to the main installation, and the card warns where a figure of the registration lies above it.

Besides the limits per address, a Bastion bounds what all anonymous clients together may cause at the main installation: by default 100 requests a second with a burst of 2,000, and 36,000 an hour, for the whole Bastion. Only requests the Bastion has to send to the main installation count (a file it serves from its cache does not); above the figure an anonymous client is answered `503` `busy` and told when to try again, while signed-in clients and everything cached are still served. The hour holds, for example, 75 cold `npm ci` of 475 packages or 36,000 `docker pull`s of a cached image. The ceiling is the registration's, under **Details** on its card.

### Limits on the card

Each card in **Admin area › Bastions** has a section **Limits**, after the rows of the web interface and the direct reads. Each row has its name on the left and its control on the right (one under the other on a narrow screen); the info sign after a name says what the setting does, on hover, on keyboard focus, or pinned by a tap. In plain view:

- **Warnings** you have to act on, first, one line each: where the ingress refuses fewer requests a second per address than a flood figure of the registration admits ("The ingress refuses more than 60 requests a second per address, below this Bastion's flood figures." *(example)*), and the addresses the Bastion connects from ([The addresses a Bastion connects from](#the-addresses-a-bastion-connects-from)). The warnings of the web interface and of the direct reads stand under their own rows above **Limits** ([Switch it on](#switch-it-on)).
- **Preset**: **Internet** (strict limits for callers from the internet, administrators too; the default of a new registration), **Partner network** (the installation's own profiles for known partners) or **Internal** (no limits, everything counted). A preset sets the five rules for every caller of a kind at this Bastion, the flood figures and the mode. Choosing one opens a dialog that names each rule, figure and mode it replaces, from what to what; the rules for single callers, groups and addresses stay. **Custom** shows that a kind rule or a flood figure differs from every preset.
- **Mode**: **Enforce**, or **Audit**, in which this Bastion counts its limits and refuses nothing by them (its flood protection, the anonymous ceiling, the slots and the throughput still hold), with **Switch to Audit…** or **Switch to Enforce…**, which asks you to sign in again when your sign-in is not recent.
- **Maximum throughput** in MB/s, `200` for a new registration *(default)*; empty is unlimited. Set it to the Bastion's share of the link between the two networks: only what the Bastion does not hold crosses it. Under the field a bar shows the throughput now against it ("132 MB/s of 150 MB/s" *(example)*), in the warning colour close to the limit.
- **Now**: what the Bastion's replicas reported in the last five minutes, added up, on one line: "Cache 14.0 of 20.0 GB · 97 % hits · 9.0 GB protected" *(example)* (and the read rate, "41 MB/s from the main installation", where no maximum throughput is set and so no bar shows it), then "4 callers limited · 1 address blocked" *(example)*, which leads to the Bastion's blocks with the reason of each. With the volume backend each replica has a cache of its own and the cache figures are their sum; with the bucket backend the replicas of a deployment share one cache, which counts once, and the caches of two deployments under one registration add up. "No report in the last 5 minutes" where no replica reported.

Under **Details** (its line says "Web interface, flood protection, addresses, rules"): first **Hide repositories** of the web interface ([What becomes public](#what-becomes-public)); then the flood figures per address (anonymous and signed-in callers and the pages of the web interface, the timeout after failed sign-ins, the blocks, the networks without flood protection), the slots to the main installation, the anonymous ceiling, the anonymous callers' share of the throughput and the GB an hour each class may read; the addresses the Bastion connects from; and **Rules of this Bastion**, the page `/admin/bastions/<name>/limits` with the rules of the Bastion's place and the blocks its replicas reported.

The permission `limits:write` changes the preset, the mode, the throughput and the figures; `endpoints:write` the addresses, and the switches of the web interface and the direct reads in their rows. Whoever holds only one of them sees the others as their values: **On** or **Off**, the preset's name, the mode, "150 MB/s" or **Unlimited** *(examples)*. Every change reaches every replica within a few seconds.

### What a refused client is told

A request the Bastion refuses itself, because its address sent too many requests or misses, is blocked, or is in the timeout after failed sign-ins, is answered `429` (`503` on the ClamAV definitions) with `Retry-After` and a text that names the whole wait, for example "Requests from this address are refused for a while after too many refused requests. Try again in 15 minutes." `Retry-After` carries that wait, except for NuGet and Ansible Galaxy clients: they count a wait against the timeout of the whole command, so they are told at most 30 seconds, as the main installation tells them, and come back every 30 seconds until the wait is over.

### What a Bastion's answers name

Nothing a client receives from a Bastion names your main installation: not its address, not a repository the client did not ask for, not a request ID or a rule of the main installation. A client that lands on the Bastion is meant to know only the Bastion.

- A write (`npm publish`, `docker push`, `dotnet nuget push`) is refused `403` with "This address is read-only: it serves downloads and accepts no uploads." It does not say where to publish: tell your publishers the main installation's address yourself.
- When the Bastion cannot get an answer it says "This service is not available right now; try again later." (`503`), "This service could not answer; try again later." (`502`) or "This service did not answer in time; try again later." (`504`), and "Too many requests are waiting; try again in a moment." when its requests to the main installation are at their limit. A path the Bastion does not serve answers "Nothing is served at this address."
- A failure of a remote repository's upstream reaches the client as "The package could not be fetched right now.", with the code the main installation gave it (for example `upstream_unreachable`), but without the name of the remote repository or the upstream's address. A refusal of the download limits names the figure, the limit and the wait, but no place and no link to the usage overview.
- Problem details have the type `about:blank`. The headers `Quader-Resolved-From` (the member of a virtual repository that answered) and the main installation's `RateLimit` and `RateLimit-Policy` are not passed on.
- An answer of the main installation that redirects to another host than the Bastion's is answered `502` and counted in `quader_bastion_refused_total{reason="foreign_location"}`. The main installation gives no such answer to a Bastion; a count above `0` means that something in front of it does.

What stays: the address of the repository the client asked for, the package it asked for, and whatever the package's authors and your administrators wrote into its content (a homepage, a README, the `Origin` of an APT repository).

## Verify

1. Open **Admin area › Bastions**. Within `bastion.main.handshake_interval` (default `1m`) of the start, the card's head says **Connected** in green, with the host of the base URL its replicas reported in grey after the name, which must be the host of the `server.base_url` you set; before that it says **No contact yet**. The card's section **Replicas** says "Last contact 1 minute ago · version 0.42.0" *(example)*, with the number of replicas the main installation knows beside its name. Open it: every replica stands in the table with, under its name, the client address the main installation saw and its version, then **Last contact** (hold the pointer on it for the date and time). A replica that is missing has not completed a handshake: look at its log and at the metrics below. The line turns into a warning, and the section opens by itself, when no replica has made contact for five minutes while the registration is not disabled (the head then says **Contact lost**), when the replicas run different versions (during a rollout, until it ends), or when one deployment of a pair has made no contact for five minutes beside the other ([A highly available install](#a-highly-available-install)). The token's **Last used** under **Tokens** shows the same moment.

On a page of several Bastions each card is folded to its head, one line with its name, its host, its state and its acts, and opens with the chevron before its name; a card that shows a warning is open, and the page remembers in your browser which cards you opened or folded.
2. On every replica the ops listener (port 8081) serves the metrics; the ServiceMonitor of the chart scrapes them. These series say that the handshake works:

| Series | Healthy | Otherwise |
|---|---|---|
| `quader_bastion_main_reachable` | `1` | The replica treats the main installation as unreachable: the handshake or the feed failed |
| `quader_bastion_base_url_mismatch` | `0` | The main installation does not honour the Bastion's base URL: see `server.additional_base_urls` |
| `quader_bastion_interface_mismatch` | `0` | The two sides speak different versions of the interface |
| `quader_bastion_main_token_refused` | `0` | The main installation refuses the token |
| `quader_bastion_token_remaining_seconds` | a positive number | The seconds left of the token in use |

3. From a machine outside your network, read a package through the Bastion with your own token, for example with the snippets that the page **Set up client** of a repository shows for the Bastion (see [Connecting a package client](../user/client-setup.md#install-through-a-bastion)). The first read of a package is a miss and goes to the main installation; the second is served from the cache.

## The web interface for visitors

A Bastion can show anonymous visitors from the internet a read-only web interface at its own address: the projects, packages, images and files of your public repositories, every version with its open vulnerabilities, its signatures and its malware scan, the documentation if you want it, and the commands to install them. Nobody signs in to it, nothing on its pages changes anything, and no page view reaches the main installation: the main installation builds the public content as a **public catalogue** after every change, each replica of the Bastion keeps a copy in its cache, and the pages are served from that copy alone. A new registration has the web interface off. What a visitor sees is described in [Browsing a Bastion](../user/browsing-a-bastion.md).

### Switch it on

1. Check that the Bastion has made contact and that its `config.server.base_url` is listed in `server.additional_base_urls` of the main installation ([Verify](#verify)). The main installation writes every link of the catalogue on that address, and builds nothing for a Bastion whose address it does not list.
2. Decide what to show. The web interface shows exactly what a visitor without a session reads on the main installation: the repositories whose visibility is **Public**, in projects that are **Public** ([What becomes public](#what-becomes-public)). Read the warning about public remote repositories there first.
3. Open **Admin area › Bastions**. On the card of the Bastion, the first row under its head and description is **Web interface** (its info sign also says that a remote's page lists what your builds fetched through it): switch it on there (permission `endpoints:write`). Switch the row **Documentation** under it on as well if visitors should read the READMEs and the documentation of the packages. The switch starts the first build of the catalogue within a few seconds; the row then says "Last built 3 minutes ago" *(example)* after the switch, "Not built yet" before the first build, or "Built once the Bastion has made contact" while no replica has made contact. When the main installation builds nothing for this Bastion, a warning of one line under the row says why: "Not built: https://packages.example.com is not in config.server.additional_base_urls of the Helm configuration." *(example: the base URL the newest replica reported)*, "Not built: the Bastion reported no base URL.", or "The web interface shows nothing: the Helm configuration admits no request without a credential (endpoints.default.auth.anonymous)." (below). A Bastion that first connects after the switch, or whose `config.server.base_url` changes, has its catalogue built within seconds of that contact; an address added to `config.server.additional_base_urls` counts from the main installation's restart, which builds within seconds as well.
4. Open the Bastion's address, for example `https://packages.example.com/` *(example)*. It leads to `/ui/en/` or `/ui/de/` by the browser's language. A build of a few hundred packages takes seconds, and every replica takes it over within its next round (`config.bastion.gui.poll_interval`, default `10s`).

Through the API the switches are `PATCH /api/v1/bastions/<name>` with `{"switches": {"gui": true, "docs": true}}` and the registration's `ETag` in `If-Match`. **Hide repositories**, the first row of the card's **Details** under **Limits**, shows visitors the projects and their packages without the repositories that hold them; commands and download addresses still name a repository's key, since clients need it. It is off for a new registration; through the API it is `{"switches": {"hideRepositories": true}}`. Each change is the audit event `bastion.configuration.changed` and reaches every replica of the Bastion within a few seconds.

**Switch it off** the same way. Within a few seconds every address of the web interface answers **not found**, `robots.txt` tells every crawler `Disallow: /`, and each replica deletes its copy of the catalogue; the main installation deletes the registration's catalogue in the build the switch starts. **Documentation** off takes the READMEs, the documentation, Debian's changelogs and the search of the documentation off the pages at once; that build deletes them from the catalogue.

### What becomes public

The main installation builds the catalogue by reading its own answers as a visitor without a session reads them, on the Bastion's address. So the web interface shows nothing that the levels do not already make public, and it shows all of it:

| A visitor reads | Left out, whatever the level |
|---|---|
| The projects that hold a public package or a public repository of files, with their display names and descriptions | The projects' owners, members and sizes |
| Every repository whose visibility is **Public**, local, remote and virtual alike, with its description; a remote shows what its cache holds | A remote's upstream address, unless it is one of the well-known public registries (Docker Hub, Quay, ghcr.io, GitLab, npmjs.com, nuget.org, Galaxy); with **Hide repositories** on, every upstream address |
| Each package and version: description, keywords, licences, tags, dependencies, deprecation, files with their sizes and SHA-256, the platforms and layers of an image | Who published, uploaded or released it (the person, the service account, the CI job and its claims) |
| Each version's malware scan and its signatures (kind, signer, issuer, time proof, log, trust) | The titles of your own installation's trust anchors (the trust state stays); the scanner's engine and replica |
| Each version's vulnerability grade and its open findings (needs triage, confirmed, blocked) with identifiers, severities, fixed versions and an image's components | Dismissed and resolved findings, every triage note, who triaged, and any internal state code |
| With **Documentation** on: the README, an Ansible collection's documentation, a Debian package's changelog and copyright; only where the repository's **Wiki visibility** is **Public** (Debian's follow the switch alone) | Download counts and the time of the last download |
| The files and folders of a public generic repository, and the client set-up of every public repository on the Bastion's address, without a sign-in line and without a publish step | The main installation's own addresses: every occurrence of its base URLs in any text is written as the Bastion's |
| Your imprint and privacy notice | Anything of the main installation's configuration |

**A public remote repository shows what your builds pulled.** A public remote of npmjs.com, nuget.org or Docker Hub lists on the web interface, and in its sitemap for search engines, every package and version its cache holds: the dependencies your builds fetched through it, with their versions. If that is not meant to be public, keep the remote at **Internal** and let a public local or virtual repository hold what you publish. The card says so in one line under the switch **Web interface**.

Making a repository **Public** opens it on the main installation as well, to every endpoint that admits requests without a credential ([Public content](sign-in-and-identity.md#public-content-and-requests-without-a-credential)). Downloads through the Bastion are still decided by the main installation for every request; the catalogue only shows.

**While the main installation admits no request without a credential** (`endpoints.default.auth.anonymous: false`), nothing is public, whatever the levels say, and the web interface shows nothing: the main installation builds every Bastion's catalogue empty, each replica of a Bastion empties its copy within a few seconds, and the card says why. The setting applies when the main installation's replicas restart; the first build after that applies it within seconds, in both directions.

### Kept or lost across a restart

Each replica keeps its copy of the catalogue, and the record of what it holds, in its cache backend. Whether that survives a new pod depends on the cache:

| Cache | Kept across a new pod | On the card |
|---|---|---|
| `config.bastion.cache.backend: bucket` | yes, one copy for every replica; with Valkey one replica syncs and the others take its state | no warning |
| `cacheVolume.type: persistent` (a StatefulSet, a claim per pod) | yes, per pod | no warning |
| `cacheVolume.type: ephemeral` *(default)* or `emptyDir` | no: a new pod (an upgrade, a rollout, a drain, an eviction) starts with an empty volume | "A restart during an outage of the main installation empties the web interface; a bucket or persistent volumes keep it." |

A replica that starts without a copy answers every page **Not available right now** ("This page is not available right now. Try again in a few minutes.", status `503` with `Retry-After: 30`) until its first round has read the catalogue from the main installation; the page tries again by itself, and search engines do not index a `503`. While the main installation is reachable that is the seconds of one round. A replica that starts while the main installation is unreachable shows **Not available right now** until the connection returns, and `QuaderBastionCatalogueMissing` fires after 15 minutes. A replica that did not restart serves on, with the age of its copy.

For example, a Bastion with two replicas on `emptyDir` volumes, without Valkey *(example)*:

```yaml
cacheVolume:
  type: emptyDir                  # example; default: ephemeral
  size: 6Gi                       # example; above config.bastion.cache.max_bytes
config:
  bastion:
    cache:
      max_bytes: 4GiB             # example; default: 20GiB
      documents_max_bytes: 512MiB # example; default: 2GiB
      max_object_bytes: 1GiB      # example; default: 2GiB
      spool_max_bytes: 1GiB       # example; default: 2GiB
    gui:
      max_bytes: 512MiB           # example; default: 4GiB
```

Its visitors see this after a restart: during an upgrade the chart replaces one pod after the other, and a visitor whose request reaches the new pod reads **Not available right now** for the seconds until that pod has read the catalogue, then the pages again; each replica fetches the whole catalogue once, through the main installation's catalogue routes. If the main installation is unreachable while a pod is replaced, that pod shows **Not available right now** until the connection returns, while the other one serves its copy with the age it has; once both are replaced, every visitor reads **Not available right now** until the connection returns. The package clients are not affected by the copy: they are served from the cache as the grace period allows ([What the alerts mean](#what-the-alerts-mean)). To keep the pages through such a restart, use the bucket backend (it needs `secrets.signingKey`, which also signs the record of the catalogue) or `cacheVolume.type: persistent`. With the volume backend and without Valkey `secrets.signingKey` is optional; without it the record a replica keeps on its volume is not signed, so whoever can write that volume can change what the replica shows after its next start.

<details>
<summary>The catalogue's share of the cache</summary>

The catalogue is kept compressed in a share of `config.bastion.cache.max_bytes`: `config.bastion.gui.max_bytes` (default `4GiB`), reserved whether or not the web interface is on, so the artifacts get `max_bytes` less `documents_max_bytes` and `gui.max_bytes`, and that must hold one `max_object_bytes` ([A highly available install](#a-highly-available-install)). Measured on a development installation: about 14 documents per public version, about 15 KiB compressed per version in a mix of npm, NuGet, images, Debian, Ansible and files; Ansible collections with documentation take about 70 KiB per version. The lists of each project and each repository add about 120 B per package in every list that holds it (2.3 KiB for a project of 20 packages), an image's tags by content about 0.6 KiB per image *(measured on the development seed, 2026-11-27)*, and a version held by a second public repository about 0.45 KiB per client for its commands there *(derived)*. By that mix the default share holds about 290,000 public versions and 512 MiB about 34,000 *(derived)*. Nothing evicts the catalogue: one above its share is kept whole and `QuaderBastionCatalogueOverBound` fires; raise `gui.max_bytes`, and `cache.max_bytes` with it. The sync reads only what changed, through the Bastion's throughput cap in a class of its own below the clients' misses (**Web interface, GB an hour** under **Details** bounds it per hour; empty, the default, is unbounded).

</details>

### How old the pages are

Each page ends with **As of** and how long ago the replica last completed a round with the main installation (hold the pointer on it for the date and time). The pages are served whatever the age: when a replica cannot reach the main installation, it serves its last copy, and once the copy is older than `config.bastion.gui.stale_after` (default `1h`) a band above every page says "This information is from <date and time>. Newer information is not available right now." Nothing on a page names the main installation. Each replica has its own age, so two pages of one visit can differ.

A change you make while the Bastion cannot reach the main installation, a repository made private included, stays on the pages until the connection returns; the first round after that takes it off. Downloads are still decided by the main installation and end with the grace period of the package API. `QuaderBastionCatalogueStale` fires when a replica's copy is older than `stale_after`.

While the connection works, a change reaches the pages within seconds: the main installation builds 5 s after a change, and a narrowing (a repository made private, a grant removed, a version deleted) makes each replica read the catalogue at once and again 8 s later. A new version, which sends no such notice, appears at the first round of each replica after the build, within `config.bastion.gui.poll_interval` of it.

### Search engines

The pages are made to be found: every page's address answers with its title, its canonical address and a text a crawler reads without running the application (a package's page also with its description and README), all on the Bastion's `config.server.base_url`. A replica serves:

- `/robots.txt`, while the web interface is on:

  ```text
  User-agent: *
  Allow: /ui/
  Disallow: /ui/data/*vulnerabilit
  Disallow: /api/
  Disallow: /files/
  Disallow: /v2/

  Sitemap: https://packages.example.com/sitemap.xml
  ```

  The package paths are disallowed and download links are marked `nofollow`, so a crawler that keeps to the rules costs the main installation nothing. While the web interface is off it says `Disallow: /` to every crawler.
- `/sitemap.xml`, an index of the files `/sitemap/<n>.xml` of at most 50,000 addresses each: the start page, every project's repositories and its list of packages, every public repository's page of those projects (a generic repository's is the root of its files), every package's page, with **Documentation** on every entry of the newest versions' documentation, and the imprint and privacy notice that have a text, each in English and German. A version's own page is not listed; the package's page links it. With **Hide repositories** on, the sitemap lists no project's repositories and no repository's page but the root of a generic repository's files, and a crawler's text of every page names no repository.

The vulnerabilities never enter a search result: their data is disallowed, an address that opens them is marked `noindex`, and the grade and the findings are excluded from snippets. A page whose package became private answers **not found** at once, but a search engine keeps its entry and snippet until it visits the address again, which can take days; use the search engine's removal tool for an address that must go at once.

Every request of a page counts per client address in the class **Pages of the web interface** under **Details** on the card: `20` requests a second with a burst of `100` *(default)*, never against the misses of the package clients. Above it a visitor reads a page **Too many requests** with the wait, which loads itself again.

### Imprint and privacy notice of the web interface

The footer of every page links **Imprint** and **Privacy notice** when the Bastion's chart has a text for them (`config.ui.legal.imprint.en`, `.de`, `config.ui.legal.privacy.en`, `.de` of the chart `quader-bastion`; [Imprint and privacy notice](configuration.md#imprint-and-privacy-notice) explains the texts). They are this Bastion's values, never the main installation's, and you answer for what they say. The pages are served only while the web interface is on, also while a replica shows **Not available right now**.

### Times, grades and Set up client

- **Times.** The pages write every date and time in the installation's **Default time format** (**Admin area › Settings › General**, section **Time format**); a change reaches every Bastion within seconds and its open pages within a minute. A visitor cannot choose another form.
- **Grades.** The table of a package's versions shows each version's grade and open findings in the column **Vulnerabilities**, and a version's page its findings, as the main installation shows them to a visitor without a session.
- **Set up client.** Every panel of commands links **Set up client**: the page of the repository, with a choice of the format's clients, whose snippets name the Bastion's address and need no credential. It has no publish step and no guide.

## Renew the token with an overlap

A token declared in the values is rotated through its Secret ([Declare the Bastion in the values](#declare-the-bastion-in-the-values)). A token issued on the screen is renewed by hand, and the old and the new one work side by side, so no request fails:

1. Before the old token expires, open the card of the registration and, in its section **Tokens**, choose **Rotate token**. The new token works beside the old one; the section now lists both tokens.
2. Put the new token into the Secret of the Bastion (`secrets.mainToken`), the way you manage your other Secrets. The chart never writes it, and the Bastion cannot change a Secret.
3. Every replica re-reads the token file every `bastion.main.token_reload_interval` (default `10s`) and when the file changes, and uses the new token from its next request to the main installation; requests already running finish with the old one. Kubernetes needs up to about a minute to update the mounted Secret. No restart is needed.
4. Wait until **Last used** of the new token shows a recent time (the section **Tokens**). Check that every replica has made contact since the Secret changed (the section **Replicas**, column **Last contact**).
5. Choose **Invalidate** on the old token. A replica that still used it would answer `401` `bastion_token_invalid` to the main installation, treat it as unreachable and set `quader_bastion_main_token_refused` to `1`.

Renew in good time: the Bastion logs a warning at start and then daily while the token has less than `bastion.token.warn_before` left (default `720h`, 30 days), and the alert `QuaderBastionTokenExpiresSoon` fires from the same point.

## Direct reads from the object store

With **Direct reads from the object store** on for a registration, the main installation answers that Bastion's request for a large file with a link to the file in its object store, valid for a short time, instead of the file's bytes, and the Bastion reads the file from the store itself. The main installation still decides whether the caller may read the file, applies the malware and signature checks and the download limits, and counts the download, exactly as for every other request through the Bastion. The Bastion checks every byte against the file's digest before it keeps the file or completes it for a client, and when a direct read fails it fetches the file through the main installation instead, so a client sees at most a delay.

**When to switch it on.** When large files, above all the layers of container images, are read through the Bastion and you want to spare the main installation's bandwidth. In a measurement on a development machine a file of 300 MiB read cold through a Bastion cost the main installation no byte of the file with the direct reads on, and its full size with them off. Small files gain nothing: a file smaller than the **Minimum size** of the download links (`1` MiB unless you change it, **Admin area › Settings › Storage › Details**) always comes through the main installation, and so does every file the Bastion has cached already. The direct reads do not depend on the switch of the clients' download links; they use its minimum size, its link life and the connections' public addresses.

**What it needs**, for every connection of the object store that holds files the Bastion reads:

- **A public address on a host name of its own.** The Bastion refuses a link to a host name it knows as the main installation's (`bastion.main.url`, `bastion.main.packages_url`) or as its own (`server.base_url`), whatever the port, so the store must be reached under another host name, such as `https://objects.example.com` *(example)*. Without a public address a link names the connection's endpoint, which for a store inside the cluster is an address the Bastion cannot reach or one it refuses. Set the address under **Details** of the storage settings, where Quader also refuses an address under one of its own host names.
- **`https` and a certificate the Bastion trusts**, through the system roots or the chart's `trustedCA` (`trustedCA.existingConfigMap` or `trustedCA.existingSecret`). The same bundle vouches for the main installation and for the object store.
- **The Bastion's network reaches the address** on its port. If you restrict the Bastion pods' outgoing connections with a policy of your own, admit the public address of every connection a link may name while the direct reads are on.

Test it before you switch on: **Test link** under the connection's address in the storage settings gives a link to a small test file, valid for 60 seconds, with a `curl` command; run the command on a machine of the Bastion's network, for example in a pod of its namespace. When that network reaches the store, the file reads `Quader download link test: this link works.`

**Switch it on** on the card of the registration in **Admin area › Bastions**: the row **Direct reads from the object store**, under the rows of the web interface (the permission `endpoints:write`; whoever may only read sees **On** or **Off**). A new registration has it off. While a connection of the object store is encrypted, the row says under the switch "Hands this Bastion the key of each file it reads from an encrypted connection.", before you switch it on: read **What you take on** below first. Through the API it is `PATCH /api/v1/bastions/<name>` with `{"switches": {"handover": true}}` or `false` and the registration's `ETag` in `If-Match`. Each change is an audit event (`bastion.handover.enabled`, `bastion.handover.disabled`, beside `bastion.configuration.changed`), and a switch-off is part of the restore journal, so a restore to an earlier point keeps it off. The Bastion learns the state within a few seconds through the feed, at the latest at its next handshake (`bastion.main.handshake_interval`, default `1m`). While it is on, a line under the row names each connection whose files a Bastion still gets through the main installation, with the reason, for example "Files of onprem still come through the main installation: its address does not use https." *(example)*, and a link leads to the storage settings where the address is set and tested.

**Encrypted connections.** For a file that lies only on a connection with `encrypted: true`, the main installation hands the Bastion, with the link, that one file's key, and the Bastion decrypts and checks the file as it reads it and keeps the key only for that read. A file that also lies on a connection that is not encrypted is read from there, without a key. Before a key leaves, the main installation records it in the audit log; when that record cannot be written, the file goes through the main installation instead. Nothing else of the connection's keys leaves: no key of another file and none of the keys that protect them. A connection that is encrypted needs what every other needs (above), and the card names it, as any other, when a Bastion cannot read it.

**What you take on.** Switching on hands a Bastion, which stands in your DMZ, a link per file it fetches, and for a file of an encrypted connection that file's key:

- a link is a bearer for its life: whoever holds it can fetch that one file until it expires (**Link valid for**, 60 seconds unless you change it), also after the caller's token is revoked;
- a link shows the object store's public address, the bucket and the file's object name, so a Bastion that is taken over learns where your files lie and can try other requests against the store; it holds no credential of the store;
- the store's request log, where its operator keeps one, shows the Bastion's egress address and which objects it reads;
- a direct read that fails before its first byte is replaced by a fetch through the main installation, which counts the download and charges its size to the caller's limits a second time; where the cause is the network, the store or a link the Bastion refuses, the pause below keeps this to one such fetch per replica and minute. A read that breaks off midway is finished with a range through the main installation, which charges those bytes again and counts no second download;
- on an encrypted connection, a key per file: a Bastion that is taken over can keep every key it receives from then on, and each one opens that file's encrypted copy wherever it lies, in the bucket, in its older versions and in a backup's copy, for as long as it lies there. Neither a new key ring, nor a new generation of the connection's keys, nor deleting the file changes that key. Whoever holds such keys and can also read the bucket or a backup reads those files; with write access to the bucket as well, they could replace parts of such a file so that a download of a range of it through the main installation serves the forged part, while a whole download and the regular check of the stored copies refuse the file;
- for the moment the answer takes, a handed key also lies in the main installation's memory in a form no program can wipe; whoever can read that memory reads the keys of the whole connection there anyway.

When a Bastion is taken over, switch its direct reads off and revoke its token on its card; the links it already holds stop working when their life is over. The keys it already holds do not: where its holder may also read the bucket of an encrypted connection, its older versions or a backup's target, replace the connection by a new one, which the reconciliation fills with new keys for every file, and take the old one out ([Add or remove a connection](operations.md#add-or-remove-a-connection)).

**Which keys a Bastion received.** Every key handed to a Bastion is the audit event `bastion.data_key.handed`, **Data key of a file handed to a Bastion**, recorded whether or not downloads are audited. In **Admin area › System › Audit log** set **Action** to `bastion.data_key.handed`, or to `bastion.*` for every event of the Bastions: each event names the caller, the file (its repository and path) and the caller's address; its **Resource** is the registration (`bastion` and its ID), and its **Details** name the Bastion (`bastion`), the Bastion replica that asked, the connection and the identifier of the storage key that protected the file's key, never the key. For one Bastion, open one of its events and copy the identifier of its **Resource**, then set **Filters › Resource kind** to `bastion` and **Resource ID** to that identifier, beside the action; through the API the same events are `GET /api/v1/audit/events?action=bastion.data_key.handed&resourceType=bastion&resourceId=<id>`, where `<id>` is the `id` of `GET /api/v1/bastions/<name>`. An event is kept for `audit.retention_days` (`365` unless you change it). When you leave the provider of an encrypted connection, the dry run of `quader admin storage keys destroy` counts the connection's files whose data key a Bastion received, for example `files whose data key was handed to a Bastion: 2 (3 handovers; the audit log goes back to 2025-11-23)`: a file handed twice, or to two Bastion replicas, is one file and two handovers ([Backup and restore](backup-and-restore.md), "Leaving a provider").

**Sizing.** The Bastion offers to take a file from the store only when it would keep it: the offer is bounded by the largest object it keeps (`bastion.cache.max_object_bytes`, default `2GiB`) and by the free room of its spool at the moment of the miss (`bastion.cache.spool_max_bytes` less the fills in progress). A larger file is sent by the main installation as before; for images with large layers raise both values as the table of the [highly available install](#a-highly-available-install) says.

**How to see that it works, and why it does not.** A card without a line about the connections says that the main installation can sign and read a link to each of them; whether the Bastion's network reaches them shows only on the Bastion. The Bastion's metrics:

| Series | What it says |
|---|---|
| `quader_bastion_handover_enabled` | `1` once the handshake says the direct reads are on for this registration |
| `quader_bastion_handovers_total{outcome}` | per direct read: `read` (read from the store and kept), `fallback` (failed before its first byte, fetched through the main installation), `continued` (broke off and was finished through the main installation), `refused` (the bytes were not the file) |
| `quader_bastion_fill_bytes_total{source, class}` | the bytes of the cache's fills from the store (`store`) and from the main installation (`main`), by whether the client that started the fill was anonymous (`anonymous`) or carried a credential (`authenticated`) |
| `quader_bastion_handover_cooldown` | `1` while the replica pauses its direct reads |

On the main installation, a file handed over has `delivery` `handover` and `response_bytes` `0` in the request log and counts in `quader_storage_download_handovers_total{bastion, kind}` (`link`, or `link_and_key` for a file of an encrypted connection); `quader_storage_download_handovers_declined_total{reason}` counts the offers it answered with the bytes and why (`size` for a file below the minimum size or above the Bastion's offer, `connection` for a file without a copy on a connection that can be read, `key` for a file of an encrypted connection whose key could not be opened, `trace` for one whose key could not be recorded in the audit log, `switch_off` for the offers a Bastion still sends after a switch-off until its next handshake). The alert `QuaderBastionHandoverFailing` fires when direct reads failed for 15 minutes and none succeeded. Each fallback is a line `a handover is replaced by the forward without the offer` in the Bastion's log, with the reason, such as a refused host, an expired link, a timeout or the store's status. After a failure of the network or the store, or after a link it refuses, a replica pauses its direct reads for a minute and fetches every miss through the main installation meanwhile; it tries again with the next miss after the pause.

## The traffic between the main installation and a Bastion

Both sides count the bytes that cross between them, so that you can see what a Bastion draws from your network before you decide whether
to limit it:

| Series | Where | What it says |
|---|---|---|
| `quader_bastion_forwarded_bytes_total{bastion, class}` | the main installation | the bytes of the answers it sent to the Bastion registered as `bastion`, by whether the Bastion's client was anonymous (`anonymous`) or carried a credential (`authenticated`); every answer counts, refusals included. A direct read from the object store is not in it: the main installation answers it with an empty body |
| `quader_bastion_forwarded_requests_total{bastion, class}` | the main installation | the requests of that Bastion, by what the answer was (`artifact`, `shared`, `private`, `absent`, `pass`, `handover`, `probe`) |
| `quader_bastion_fill_bytes_total{source, class}` | the Bastion | the bytes it wrote into its cache's fills, from the main installation (`main`) or the object store (`store`), by the class of the client that started the fill |
| `quader_bastion_bytes_served_total{source}` | the Bastion | the bytes it sent to its clients, from its cache (`cache`) or as they came from the main installation (`main`) |

For example, `sum by (class) (rate(quader_bastion_forwarded_bytes_total{bastion="partners"}[5m]))` on the main installation is the
bandwidth the Bastion `partners` takes, split into anonymous and signed-in clients.

**The cap.** Each Bastion reads at most 200 MB/s from the main installation and the object store together, and its anonymous clients' downloads
at most a quarter of it, 50 MB/s; these are the defaults of its registration. Set the figure to the Bastion's share of the real link between the
two networks: only what the Bastion does not hold yet crosses it, what it serves from its cache does not. At the cap the running downloads
slow down and share it, none is cut; a new download whose first bytes would wait longer than the Bastion's queue time (5 s by default) is
answered `503` with `Retry-After`, and what the cache holds is served as before. Without Valkey each replica reads at most the figure divided
by the number of replicas. The Bastion's metrics:

| Series | What it says |
|---|---|
| `quader_bastion_throughput_limit_bytes_per_second{bucket}` | the figure in force, for all clients (`total`) and for anonymous ones (`anonymous`); `0` without a cap |
| `quader_bastion_throughput_wait_seconds_total{class}` | the seconds the replica's downloads waited for the cap, by `anonymous`, `signed_in` and `catalogue`; rising by about one second a second per download means that the cap, not your network, sets the pace |
| `quader_bastion_throughput_refused_total{class, reason}` | the downloads refused at the cap (`busy`) or because a class's hourly volume was used up (`hour`) |

## What the alerts mean

With `metrics.prometheusRule.enabled` the chart renders one PrometheusRule. Every alert can be left out with `metrics.prometheusRule.disabledAlerts`, and the thresholds come from gauges the Bastion exports, not from chart values.

When the main installation is unreachable for a replica, the replica serves what its cache and its answers allow for the grace period (`bastion.answers.grace_period`, default `15m`, on top of `bastion.answers.period`, default `5m`): cached artifacts and metadata for callers whose token it has already checked, `503` `unavailable` with `Retry-After: 30` for a token it has not seen and for anything not cached. After `answers.period` plus `grace_period` it serves nothing and every read answers `503`.

| Alert | Severity | Fires when | What it means and what to do |
|---|---|---|---|
| `QuaderBastionInterfaceMismatch` | critical | A replica reports a different interface version than the main installation, for 1 minute | One side was upgraded without the other. The replica treats the main installation as unreachable. Upgrade the other side to the same release; the alert clears at the next handshake with equal versions. The Bastions screen shows the version each replica reports |
| `QuaderBastionMainUnreachable` | critical | Every replica treats the main installation as unreachable, for 2 minutes | The Bastion cannot reach the main installation, or it refuses the Bastion. Check the main installation, your own egress policy for the Bastion pods if you set one, the egress address, the trust bundle, and whether `QuaderBastionTokenRefused` or `QuaderBastionBaseURLMismatch` fire too. Clients get cached content for the grace period, then `503` |
| `QuaderBastionMainUnreachableSingleReplica` | warning | Some, but not all, replicas treat it as unreachable, for 5 minutes | The fault is in one pod or its node: look at that pod's log and network |
| `QuaderBastionGraceExhausted` | critical | A replica has been unreachable for longer than `answers.period` plus `grace_period` | The replica serves nothing: every read answers `503`. Restore the connection to the main installation |
| `QuaderBastionBaseURLMismatch` | critical | The main installation does not render URLs for the Bastion's host, for 1 minute | The Bastion's base URL is missing from `server.additional_base_urls` of the main installation, or the address the Bastion connects from is not in `bastions.trusted_proxies`, so the main installation ignores the forwarded host. The Bastion refuses to serve rather than hand out the main installation's URLs |
| `QuaderBastionTokenExpiresSoon` | warning | The token in use has less than `bastion.token.warn_before` left, for 1 hour | Renew the token as above |
| `QuaderBastionTokenRefused` | critical | The main installation refuses the token, for 1 minute | The token was revoked, has expired, or its registration was deleted; a disabled registration keeps its token. Issue a token and put it into the Secret |
| `QuaderBastionFeedStale` | warning | The last successful poll of the revocation feed is older than 30 seconds, for 2 minutes | The replica may not know a recent revocation. Revoked tokens keep reading from the cache until their answers expire. Check the connection and the load of the main installation |
| `QuaderBastionMainBudgetSaturated` | warning | Requests wait for a slot towards the main installation, for 10 minutes | The budget of requests to the main installation (default `64` at once across all replicas, a setting of the Bastion's registration) is used up, so some requests answer `503` `busy`. It happens while the cache is cold or after it was emptied. Raise the budget only after you checked what the main installation tolerates |
| `QuaderBastionAuditEventsDropped` | warning | Download events were dropped | The buffer of download events was full, which happens while the main installation stays unreachable or cannot keep up. Those downloads are missing from the audit trail. Raise `bastion.audit.buffer_events` if outages are long |
| `QuaderBastionCacheCorrupt` | warning | A cached object failed its check | The Bastion deleted it and fetched it again; no client received wrong bytes. Look at the disk or the bucket, and at who can write there |
| `QuaderBastionHandoverFailing` | warning | Direct reads from the object store failed within 15 minutes and none succeeded, for 15 minutes | The Bastion gets large files through the main installation again, so nothing fails for clients, but the main installation's bandwidth is not spared. The Bastion's log names the reason in the line "a handover is replaced by the forward without the offer". Check that the Bastion's network reaches the object store's public address, that its certificate is trusted through the system roots or `trustedCA`, and that the address's host name is not the main installation's ([Direct reads from the object store](#direct-reads-from-the-object-store)); or switch the registration's direct reads off |
| `QuaderBastionCatalogueStale` | warning | While the web interface is on, a replica's copy of the public catalogue is older than `config.bastion.gui.stale_after` (default `1h`), for 5 minutes | The replica cannot complete a round with the main installation. Its pages serve on and say how old they are, and a repository made private stays on them until the next round ([How old the pages are](#how-old-the-pages-are)). Check the connection to the main installation, as for `QuaderBastionMainUnreachable` |
| `QuaderBastionCatalogueMissing` | warning | While the web interface is on, a replica holds no copy of the catalogue, for 15 minutes | Its pages answer **Not available right now**. A replica on an `ephemeral` or `emptyDir` volume that started while the main installation was unreachable cannot get a copy until the connection returns ([Kept or lost across a restart](#kept-or-lost-across-a-restart)) |
| `QuaderBastionCatalogueOverBound` | warning | The catalogue holds more than `config.bastion.gui.max_bytes`, for 15 minutes | The catalogue is kept whole and nothing is evicted for it, so the cache may hold more than `config.bastion.cache.max_bytes`. Raise `gui.max_bytes`, and `cache.max_bytes` with it |
| `QuaderBastionValkeyUnavailable` | warning | Valkey is unreachable, for 5 minutes; rendered only with Valkey configured | The replicas use their own state: limits are divided by the number of replicas, blocks are per replica and answers are asked again. Nothing is served wrongly. Restore Valkey |
| `QuaderBastionReplicasNotReady` | warning | Fewer than 2 replicas are available, for 5 minutes | The Bastion has lost its redundancy; a drain or an upgrade can now cut clients off. Find out why pods are not ready |

## Disable or delete a Bastion

**Disable** tells the Bastion to stop work, over the connection it keeps to the main installation, within a few seconds; nothing is deleted and its token keeps working. There are two ways:

- **Soft**: no new request is taken; downloads that have started run to their end.
- **Hard**: no new request is taken, and running downloads are cut.

On the card, **Disable…** at the right of its head asks for the way, **Soft** chosen; while a soft disable lets downloads finish, the head says **Disabling** with "3 downloads still running" *(example)* after it and offers **Disable hard…**, which cuts them. Through the API or the command line:

```shell
quader admin bastion disable artifacts            # example name; soft
quader admin bastion disable artifacts --hard
quader admin bastion enable artifacts
# or: PATCH /api/v1/bastions/artifacts {"disabled": "hard"} with If-Match; {"disabled": null} enables
```

While it is disabled, the Bastion answers every new request `503 Service Unavailable` with `Retry-After: 300` (at most the wait a format's clients accept, 30 seconds for NuGet and Ansible), in the shape the client expects, and its web interface shows "Not available at the moment. Try again later." Nothing in the answer says that the Bastion was disabled: a client and a search engine see a temporary outage and try again later. Its pods stay ready, its cache stays as it is, and its web interface's catalogue is not synchronised until you enable it again; `quader_bastion_disabled{mode="soft"}` or `{mode="hard"}` is 1. From the moment you disable it, the main installation answers none of the Bastion's forwards, checks and catalogue reads, and a hard disable cuts the downloads it is forwarding. Each replica of the Bastion learns the disable at its next handshake, within about 3 seconds, and confirms it with the status it sends every second. The registration's `state` says how far it got, and the card's head says it as one badge: **Disabling** (`disabling`) while a replica has not confirmed yet or soft downloads still run (`running` counts them), **Disabled** (`disabled`; pointing at it shows "Soft, since …" or "Hard, since …") once every replica confirmed and runs nothing, and **Disable not confirmed** (`disableUnconfirmed`, in the warning colour, its card open) when a replica has not confirmed within 15 seconds or none was heard before, for example because the Bastion cannot reach the main installation: it then serves its cache for its grace period, as for an unreachable main installation, and applies the disable at its first contact. The page reads the state every few seconds while a card is disabling or not confirmed. If you suspect the Bastion was taken over, also **Invalidate** its tokens, which ends its connection, and stop its deployment: a Bastion that ignores the disable keeps serving what it holds. **Enable**, the head's first button while it is disabled, reverses the disable the same way. Each change is an audit event (`bastion.disabled` with the way, `bastion.enabled`), and a disable is part of the restore journal, so a restore to an earlier point keeps the Bastion disabled. The registration's catalogue is kept up to date while it is disabled, so an enable finds it current.

A registration declared in the values is disabled and enabled here too; to delete it, take it out of the values first: its **Delete** stays greyed out until then. **Delete** asks you to type the name, removes the registration and revokes its tokens at once; the Bastion's pods keep running until you remove them, and answer as for an unreachable main installation.

A Bastion that is no longer needed is removed with `helm uninstall`, and its registration deleted here. Remove its address from `server.additional_base_urls` afterwards; the addresses it connected from went with the registration.

## What the Bastion does not do

- It is read-only. Publishing, pushing images, changing tags and unpublishing are done on the main installation.
- A new version, or a security fix, reaches a Bastion cache after its metadata period (`bastion.cache.metadata_revalidate_after`, default `1m`; artifacts after `artifact_revalidate_after`, default `10m`). This holds for clients without a token as well: a package's metadata that the main installation shows them in part (a repository whose artifacts are public and whose documentation is not, for example) is kept once for all of them for the same period. An unpublished, deleted or blocked package, and a repository made private, reach the Bastion through its feed within seconds.
- A client without a token gets its registry token from the Bastion itself: an anonymous `docker pull` asks the main installation nothing for its token. That token stands for no one and opens nothing a request without a token would not; the main installation does not accept it, and its count of anonymous registry tokens (`quader_oci_registry_tokens_total{outcome="anonymous"}`) no longer includes pulls through a Bastion. The Bastion counts its own token answers in `quader_bastion_requests_total{class="pass",result="hit"}`.
- A token revoked on the main installation is dropped at the Bastion through the feed within seconds; while the main installation is unreachable, it can read on from the cache for up to the grace period.
- Download counts and the audit trail of the main installation include what the Bastion serves from its cache, reported in batches, for every format by the main installation's own rule of what a download is (a pull of an image is the download of its manifest and its layers, a `HEAD` is none). A registration may report `bastions.downloads.events_per_second` downloads a second (default `200`, bursts up to `events_burst`, `2000`); beyond that the main installation answers `429` and the Bastion sends the batch again later. What the Bastion's buffer cannot hold meanwhile is recorded as a `bastion.downloads.dropped` event.
- An image client takes a new registry token for every pull. The Bastion asks the main installation once about such a token, and then serves the image's manifests and layers from its cache without asking again: a warm pull of an image of ten layers costs the main installation 2 to 6 requests (measured: Podman 2, Docker 4, containerd 5 to 6), the token request among them, instead of one more per manifest and layer. One of Docker's 4 is its first look at the tag without a credential, which the Bastion passes on while any repository of the main installation is public. Each of these requests is one of the client address's misses a minute at the Bastion (`bastion.limits.misses.authenticated_per_minute`, default `4000`, about a thousand Docker pulls a minute). Many nodes behind one address that pull at once can reach that limit and get `429`, which image clients retry; for such an address raise the limit or list it in `bastion.blocking.exempt_cidrs`.
- A ClamAV installation that the main installation admits by its client certificate alone cannot update through a Bastion: the Bastion serves the ClamAV definitions to the anonymous caller while the repository is public and **ClamAV definition updates** is on, and to a token that may read it. On the definitions the Bastion's own refusals (its limits, its block list and its failed-sign-in timeout) are `503` with `Retry-After`, the status the format uses for a rate limit, never `429`.
- The ops listener serves its metrics without sign-in; the chart's network policy admits it only from the monitoring namespace.

See also [Endpoints and exposure rules](endpoints.md) for the other way to open an installation to outside clients, [Managing tokens](tokens.md) and [Monitoring and operations](operations.md).
