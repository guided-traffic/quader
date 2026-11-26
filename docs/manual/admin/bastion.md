# The Quader Bastion

How to run a read-only, internet-facing cache of your installation for clients outside your network: register a Bastion, issue its token, set up the main installation, install the Bastion chart with a minimal or a highly available configuration, check that it works, renew its token without an outage, let it read large files from the object store directly, and what each alert of its monitoring means.

## What a Bastion is

A Bastion is the same Quader image started as `quader serve --role bastion`, installed with its own chart, `quader-bastion`, in its own namespace or cluster. It has no database and no web interface. It serves what clients read from your repositories, for every format, as the main installation's own address serves it: the package paths (`/api/<format>/<repository>/…`: npm, NuGet, Debian (APT), Ansible Galaxy and the ClamAV definitions), the raw files (`/files/<repository>/…`) and the container registry at the root of its own address (`/v2/…`), so that an image is pulled as `<Bastion host>/<project>/<repository>/<image>:<tag>`. Every other path answers `404` with the code `bastion_not_served`. It holds no repository list and no permissions. For every read it asks the main installation whether the caller may read the repository, with the caller's own token, and serves the bytes from its cache when the answer is yes. A write through a Bastion is refused with `403` `bastion_read_only`.

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
| Main installation | A registration and a token (the next two sections); its address in `server.additional_base_urls`; the Bastion's egress address in `bastions.trusted_proxies` |
| Bastion namespace | The Secret of the token; a TLS certificate source for the listener (cert-manager or a Secret); the main installation reachable over HTTPS; a public name and certificate on the Bastion's ingress |
| Network | The Bastion reaches the main installation's endpoint on port 443; the main installation does not reach the Bastion |

## Register a Bastion

1. Open **Admin area › Bastions** and choose **New Bastion**. The screen needs the permission to read endpoints (`endpoints:read`: the roles admin, security-admin, auditor and repository-admin); the actions need the permission to change them (`endpoints:write`: admin and security-admin). Revoking a token alone needs `endpoints:write` or `tokens:revoke`.
2. Enter a **Name** that starts with a lower-case letter and holds lower-case letters, digits and `-`, at most 20 characters (`partners`, for example). The name cannot be changed later. The **Description** is free text.

A registration is a principal of its own kind, `bastion:<name>`. It has no permission policy, no role, no group and no package ownership; a policy, role binding or ownership that names it is refused with `422` `principal_not_allowed`. It never belongs to the group of all authenticated users. A registration can be **disabled**, which refuses its tokens until it is **enabled** again, and **deleted**, which revokes its tokens at once. Each action is an audit event (`bastion.created`, `bastion.updated`, `bastion.deleted`).

## Issue the token

1. On the card of the registration choose **Issue token**.
2. Enter a **Description** that says which rollout or year the token is for, and a **Lifetime in days**. If your sign-in is not recent, Quader asks you to sign in again; your entries are kept, and the dialog opens again with them afterwards. Leave the lifetime empty for the default, `auth.tokens.bst.default_ttl` (`8760h`, one year); the longest allowed is `auth.tokens.bst.max_ttl` (`9480h`), and a longer entry is refused with `invalid_ttl`. A Bastion token always expires.
3. Copy the token from the panel **Your new token** (**Copy**, or **Show** and copy by hand). It has 57 characters and is shown once; Quader stores only its hash. **I have copied it** closes the panel.
4. Put the token into the Secret that the chart reads. The chart never creates a Secret:

```shell
kubectl -n quader-bastion create secret generic quader-bastion-token --from-literal=token='qdr_bst_…'   # example: namespace and name
```

At most `auth.tokens.max_active_per_principal.bst` unrevoked tokens (default `3`) can exist per registration, so a renewal with an overlap fits and a forgotten token does not pile up; a fourth answers `409` `token_limit_reached`. Bastion tokens do not appear on the Tokens screen. They are managed here, on the card of the registration, where each token shows its hint (the first characters, to recognise it), its expiry, and when it last authenticated a request.

A token that expires in less than 30 days carries the mark **Expires in …**, an expired one the mark **Expired**.

## Settings on the main installation

Two settings make the main installation treat the Bastion's requests as the callers' own and write URLs on the Bastion's host. Set them in `config` of the chart `quader`:

```yaml
config:
  bastions:
    trusted_proxies: ["198.51.100.20/32"]                    # example: the address the Bastion connects from
  server:
    trusted_proxies: ["10.244.3.17", "10.244.5.9"]           # example: your own ingress pods, as before; not the Bastion
    additional_base_urls: ["https://packages.example.com"]   # example: the Bastion's config.server.base_url
```

| Setting | Why the Bastion needs it |
|---|---|
| `bastions.trusted_proxies` | The Bastion sends the address of the real client as the only entry of `X-Forwarded-For` and `Forwarded`, and its own host as the forwarded host. The main installation believes them only for a request that carries a valid Bastion token and comes from an address in this list; then its rate limits, its failed-sign-in throttle and its audit trail apply to the client and not to the Bastion. Any other request from that address - another workload behind the same NAT, for example - cannot choose its address or the host its links are written for. List the address the main installation sees the Bastion connect from, after your own ingress: a host address or a small range, never a whole network. Do **not** put it into `server.trusted_proxies`: an address there is believed for every request, and the main installation logs a warning at start when the two lists overlap |
| `server.additional_base_urls` | The Bastion sends its own base URL as the forwarded host. With the URL in this list, every address the main installation renders for the Bastion, such as npm's tarball links, is on the Bastion's host. Without it the main installation answers with its own address, the Bastion detects that, treats the main installation as unreachable and serves no client a document with the wrong addresses |

The entry must equal the Bastion's `config.server.base_url`: the same scheme and host, and the port if one is set. The comparison ignores a trailing slash and upper case in the scheme and host. The page **Set up client** of a repository uses the same list: they offer a Bastion as a place to install from only when its reported address is in it.

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
| `config.bastion.cache.max_bytes` | The bytes the cache of a replica keeps, or of the release with the bucket backend (default `20GiB`, of which `config.bastion.cache.documents_max_bytes`, default `2GiB`, are for metadata). The cache never takes in more: a file that would pass it is sent to its client and not kept, and the cache evicts the least recently read files down to 85 % at once instead of waiting for its regular sweep (`config.bastion.cache.sweep_interval`, default `5m`), so a flood of distinct files does not fill the volume or get the pod evicted. `quader_bastion_cache_no_room_total{area}` counts the files sent without being kept and `quader_bastion_cache_sweeps_total{trigger="size"}` the sweeps the size started. Both rising for long means that the cache churns: more distinct files are read than it holds, so what other clients read is evicted and their reads go to the main installation. Raise `max_bytes` with `cacheVolume.size` if that is your normal load, or look at who reads so much. With the bucket backend and several replicas the bound is an estimate: each replica learns what the others wrote when it lists the bucket |
| `config.bastion.cache.fill_wait_max` | With the bucket backend and Valkey, a replica whose request finds another replica filling the same artifact waits for that fill, sending nothing to its client, up to this time (default `10m`, 30 s to 1 h), and then fetches the artifact itself. A client that gives up before the first byte retries and waits again; with the volume backend and `cacheVolume.routeByPath`, every request for a path reaches one replica and follows its fill |
| `config.valkey.mode: sentinel` | Valkey holds the shared limits, the block list, the fetch coordination and the main installation's answers. It is optional: losing it costs speed, never correctness (the replicas fall back to their own state). Standalone Valkey is for development and refused while `highAvailability.enforce` is on |
| `secrets.valkey`, `secrets.valkeySentinel`, `secrets.valkeyCA` | The user and password of Valkey, the user and password of its Sentinels (`valkey.sentinel.username_file`, `valkey.sentinel.password_file`) and the CA that signed their certificates. A Valkey whose Sentinels require a password needs `secrets.valkeySentinel`; without it the Bastion logs `valkey sentinel authentication is not configured` and cannot reach the Sentinels, so it runs with its own state per replica. Set `usernameKey: ""` for the default user |
| `secrets.signingKey` | Required with the bucket backend or Valkey. Records in the bucket and in Valkey are signed with it, so whoever can write there without the key cannot make the Bastion serve other bytes. Replacing the Secret makes every record unverifiable and costs one refill |
| `secrets.cacheKey` | Optional. With it the Bastion encrypts every object of its cache store, bucket or volume, under keys derived from this one Secret of 32 random bytes (raw or base64, for example `openssl rand -base64 32`); without it the cache is plain. Replacing the Secret rolls the pods and starts an empty cache that refills, and the new pods delete the old key's objects within their first hour; a lost key costs the cache and nothing else. With the volume backend the fills in progress then go to the pod's `/tmp`, which the chart sizes from `config.bastion.cache.spool_max_bytes` |
| `cacheVolume.type: persistent` with `cacheVolume.routeByPath: true` | The alternative without a bucket: a persistent volume per pod (a StatefulSet), and HAProxy Ingress routes a path to the same replica so that the volumes together hold more than one. It needs HAProxy Ingress to accept the chart's backend snippet, and the chart refuses it together with the bucket backend |
| `autoscaling.enabled: true` | The Bastion's request budget towards the main installation is divided by `autoscaling.maxReplicas` instead of `replicaCount` |
| `config.bastion.answers.max_entries` | The main installation's answers a replica keeps in memory, least recently used first (default `200000`): per caller its credential, its read on each repository it reads and, for an image client's registry token or a token narrowed to some packages, its read on each package it reads, about three answers per image pull. Size it to the callers of one `bastion.answers.period` (default `5m`) times the repositories and packages each reads; an answer pushed out costs its caller one request to the main installation at its next read, never a wrong answer |

The chart's `ingress` values carry annotations of HAProxy Ingress (a request limit per client address and a limit of connections). Another ingress controller ignores them: the Bastion's own limits and blocking still hold, the coarse limit in front of them does not. HAProxy Ingress counts per TCP peer address, so behind a load balancer that hides the client address, every client shares one budget until you switch on its proxy protocol or turn `edgeLimit.enabled` off.

## Limits and blocking

A Bastion protects itself and the main installation in two stages: a coarse limit at HAProxy Ingress, and its own limits per client address, which know credentials and misses. Their defaults let one normal command of every client through, and two at once from one address, and still refuse and block an address that floods:

| Value | Default | What it counts per client address | What a normal command needs |
|---|---|---|---|
| `config.bastion.limits.anonymous` | `50` a second, burst `1000` | requests without a credential | an anonymous `docker pull` of an image of 40 layers: 48 requests in 0.3 s; an `npm ci` of 475 packages: 475 in 2.5 s |
| `config.bastion.limits.authenticated` | `200` a second, burst `4000` | requests with a credential | an install of up to 4,000 packages at once |
| `config.bastion.limits.misses.anonymous_per_minute` | `1000` | requests that need the main installation, without a credential | on an empty cache every request is one: the pull above is 48 |
| `config.bastion.limits.misses.authenticated_per_minute` | `4000` | the same, with a credential | as above |
| `config.bastion.blocking.threshold` | `3000` | refused requests in `blocking.window` (`1m`) before the address is blocked for `blocking.duration` (`15m`, doubling up to `24h`) | none: an address is blocked only when it sends, after its burst, at least twice the anonymous rate or 1.25 times the rate with a credential |
| `edgeLimit.requestsPerSecond` | `1000` | HTTP requests in the last second, also on a kept-alive connection, without a burst; refused by the ingress with `429` and no `Retry-After` | an `npm ci` is about 190 a second |
| `edgeLimit.connections` | `256` | requests in flight at once | `npm ci` up to 15, `docker pull` 3, APT up to 10; `dotnet restore` on Linux has no bound of its own, so raise this value if a large restore through the Bastion is refused by the ingress |

The figures count per client address, not per person: many people or build jobs behind one NAT share them. List such an address in `config.bastion.blocking.exempt_cidrs` to exempt it from every limit, timeout and block of the Bastion and from the ingress's limit, or raise the figures. `edgeLimit.requestsPerSecond` must stay above `config.bastion.limits.authenticated.requests_per_second`; the chart refuses it otherwise.

### What a refused client is told

A request the Bastion refuses itself, because its address sent too many requests or misses, is blocked, or is in the timeout after failed sign-ins, is answered `429` (`503` on the ClamAV definitions) with `Retry-After` and a text that names the whole wait, for example "Requests from this address are refused for a while after too many refused requests. Try again in 15 minutes." `Retry-After` carries that wait, except for NuGet and Ansible Galaxy clients: they count a wait against the timeout of the whole command, so they are told at most 30 seconds, as the main installation tells them, and come back every 30 seconds until the wait is over.

## Verify

1. Open **Admin area › Bastions**. Within `bastion.main.handshake_interval` (default `1m`) of the start, every replica appears in the table **Last contact** with, under its name, the client address the main installation saw, its release and its interface version, then the time of its contact (hold the pointer on it for the date and time) and the base URL it reported, which must equal the `server.base_url` you set. A replica that is missing has not completed a handshake: look at its log and at the metrics below. The token's **Last used** column shows the same moment.
2. On every replica the ops listener (port 8081) serves the metrics; the ServiceMonitor of the chart scrapes them. These series say that the handshake works:

| Series | Healthy | Otherwise |
|---|---|---|
| `quader_bastion_main_reachable` | `1` | The replica treats the main installation as unreachable: the handshake or the feed failed |
| `quader_bastion_base_url_mismatch` | `0` | The main installation does not honour the Bastion's base URL: see `server.additional_base_urls` |
| `quader_bastion_interface_mismatch` | `0` | The two sides speak different versions of the interface |
| `quader_bastion_main_token_refused` | `0` | The main installation refuses the token |
| `quader_bastion_token_remaining_seconds` | a positive number | The seconds left of the token in use |

3. From a machine outside your network, read a package through the Bastion with your own token, for example with the snippets that the page **Set up client** of a repository shows for the Bastion (see [Connecting a package client](../user/client-setup.md#install-through-a-bastion)). The first read of a package is a miss and goes to the main installation; the second is served from the cache.

## Renew the token with an overlap

A token is renewed by hand, and the old and the new one work side by side, so no request fails:

1. Before the old token expires, open the card of the registration and choose **Issue token** again. The card now lists both tokens.
2. Put the new token into the Secret of the Bastion (`secrets.mainToken`), the way you manage your other Secrets. The chart never writes it, and the Bastion cannot change a Secret.
3. Every replica re-reads the token file every `bastion.main.token_reload_interval` (default `10s`) and when the file changes, and uses the new token from its next request to the main installation; requests already running finish with the old one. Kubernetes needs up to about a minute to update the mounted Secret. No restart is needed.
4. Wait until **Last used** of the new token shows a recent time. Check that every replica has made contact since the Secret changed (the table **Last contact**).
5. Choose **Revoke** on the old token. A replica that still used it would answer `401` `bastion_token_invalid` to the main installation, treat it as unreachable and set `quader_bastion_main_token_refused` to `1`.

Renew in good time: the Bastion logs a warning at start and then daily while the token has less than `bastion.token.warn_before` left (default `720h`, 30 days), and the alert `QuaderBastionTokenExpiresSoon` fires from the same point.

## Direct reads from the object store

With **Direct reads from the object store** on for a registration, the main installation answers that Bastion's request for a large file with a link to the file in its object store, valid for a short time, instead of the file's bytes, and the Bastion reads the file from the store itself. The main installation still decides whether the caller may read the file, applies the malware and signature checks and the download limits, and counts the download, exactly as for every other request through the Bastion. The Bastion checks every byte against the file's digest before it keeps the file or completes it for a client, and when a direct read fails it fetches the file through the main installation instead, so a client sees at most a delay.

**When to switch it on.** When large files, above all the layers of container images, are read through the Bastion and you want to spare the main installation's bandwidth. In a measurement on a development machine a file of 300 MiB read cold through a Bastion cost the main installation no byte of the file with the direct reads on, and its full size with them off. Small files gain nothing: a file smaller than the **Minimum size** of the download links (`1` MiB unless you change it, **Admin area › Settings › Storage › Details**) always comes through the main installation, and so does every file the Bastion has cached already. The direct reads do not depend on the switch of the clients' download links; they use its minimum size, its link life and the connections' public addresses.

**What it needs**, for every connection of the object store that holds files the Bastion reads:

- **A public address on a host name of its own.** The Bastion refuses a link to a host name it knows as the main installation's (`bastion.main.url`, `bastion.main.packages_url`) or as its own (`server.base_url`), whatever the port, so the store must be reached under another host name, such as `https://objects.example.com` *(example)*. Without a public address a link names the connection's endpoint, which for a store inside the cluster is an address the Bastion cannot reach or one it refuses. Set the address under **Details** of the storage settings, where Quader also refuses an address under one of its own host names.
- **`https` and a certificate the Bastion trusts**, through the system roots or the chart's `trustedCA` (`trustedCA.existingConfigMap` or `trustedCA.existingSecret`). The same bundle vouches for the main installation and for the object store.
- **The Bastion's network reaches the address** on its port. If you restrict the Bastion pods' outgoing connections with a policy of your own, admit the public address of every connection a link may name while the direct reads are on.

Test it before you switch on: **Test link** under the connection's address in the storage settings gives a link to a small test file, valid for 60 seconds, with a `curl` command; run the command on a machine of the Bastion's network, for example in a pod of its namespace. When that network reaches the store, the file reads `Quader download link test: this link works.`

**Switch it on** on the card of the registration in **Admin area › Bastions**: **Direct reads from the object store** (the permission `endpoints:write`). A new registration has it off. Through the API it is `PATCH /api/v1/bastions/<name>` with `{"handover": true}` or `false` and the registration's `ETag` in `If-Match`. Each change is an audit event (`bastion.handover.enabled`, `bastion.handover.disabled`), and a switch-off is part of the restore journal, so a restore to an earlier point keeps it off. The Bastion learns the state at its next handshake (`bastion.main.handshake_interval`, default `1m`). While it is on, the card names each connection whose files a Bastion still gets through the main installation, with the reason, and links to the storage settings where the address is set and tested.

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
bandwidth the Bastion `partners` takes, split into anonymous and signed-in clients. Nothing limits it per Bastion yet: what bounds it is each
client's own download limits on the main installation (they apply to a client behind a Bastion when `bastions.trusted_proxies` lists the
Bastion's egress address) and the Bastion's budget of requests to the main installation (`bastion.main.max_concurrent`).

## What the alerts mean

With `metrics.prometheusRule.enabled` the chart renders one PrometheusRule. Every alert can be left out with `metrics.prometheusRule.disabledAlerts`, and the thresholds come from gauges the Bastion exports, not from chart values.

When the main installation is unreachable for a replica, the replica serves what its cache and its answers allow for the grace period (`bastion.answers.grace_period`, default `15m`, on top of `bastion.answers.period`, default `5m`): cached artifacts and metadata for callers whose token it has already checked, `503` `main_unreachable` with `Retry-After: 30` for a token it has not seen and for anything not cached. After `answers.period` plus `grace_period` it serves nothing and every read answers `503`.

| Alert | Severity | Fires when | What it means and what to do |
|---|---|---|---|
| `QuaderBastionInterfaceMismatch` | critical | A replica reports a different interface version than the main installation, for 1 minute | One side was upgraded without the other. The replica treats the main installation as unreachable. Upgrade the other side to the same release; the alert clears at the next handshake with equal versions. The Bastions screen shows the version each replica reports |
| `QuaderBastionMainUnreachable` | critical | Every replica treats the main installation as unreachable, for 2 minutes | The Bastion cannot reach the main installation, or it refuses the Bastion. Check the main installation, your own egress policy for the Bastion pods if you set one, the egress address, the trust bundle, and whether `QuaderBastionTokenRefused` or `QuaderBastionBaseURLMismatch` fire too. Clients get cached content for the grace period, then `503` |
| `QuaderBastionMainUnreachableSingleReplica` | warning | Some, but not all, replicas treat it as unreachable, for 5 minutes | The fault is in one pod or its node: look at that pod's log and network |
| `QuaderBastionGraceExhausted` | critical | A replica has been unreachable for longer than `answers.period` plus `grace_period` | The replica serves nothing: every read answers `503`. Restore the connection to the main installation |
| `QuaderBastionBaseURLMismatch` | critical | The main installation does not render URLs for the Bastion's host, for 1 minute | The Bastion's base URL is missing from `server.additional_base_urls` of the main installation, or the address the Bastion connects from is not in `bastions.trusted_proxies`, so the main installation ignores the forwarded host. The Bastion refuses to serve rather than hand out the main installation's URLs |
| `QuaderBastionTokenExpiresSoon` | warning | The token in use has less than `bastion.token.warn_before` left, for 1 hour | Renew the token as above |
| `QuaderBastionTokenRefused` | critical | The main installation refuses the token, for 1 minute | The token was revoked, has expired, or its registration is disabled or deleted. Issue a token, put it into the Secret, and enable the registration if you disabled it |
| `QuaderBastionFeedStale` | warning | The last successful poll of the revocation feed is older than 30 seconds, for 2 minutes | The replica may not know a recent revocation. Revoked tokens keep reading from the cache until their answers expire. Check the connection and the load of the main installation |
| `QuaderBastionMainBudgetSaturated` | warning | Requests wait for a slot towards the main installation, for 10 minutes | The budget `bastion.main.max_concurrent` (default `64`, across all replicas) is used up, so some requests answer `503` `bastion_busy`. It happens while the cache is cold or after it was emptied. Raise the budget only after you checked what the main installation tolerates |
| `QuaderBastionAuditEventsDropped` | warning | Download events were dropped | The buffer of download events was full, which happens while the main installation stays unreachable or cannot keep up. Those downloads are missing from the audit trail. Raise `bastion.audit.buffer_events` if outages are long |
| `QuaderBastionCacheCorrupt` | warning | A cached object failed its check | The Bastion deleted it and fetched it again; no client received wrong bytes. Look at the disk or the bucket, and at who can write there |
| `QuaderBastionHandoverFailing` | warning | Direct reads from the object store failed within 15 minutes and none succeeded, for 15 minutes | The Bastion gets large files through the main installation again, so nothing fails for clients, but the main installation's bandwidth is not spared. The Bastion's log names the reason in the line "a handover is replaced by the forward without the offer". Check that the Bastion's network reaches the object store's public address, that its certificate is trusted through the system roots or `trustedCA`, and that the address's host name is not the main installation's ([Direct reads from the object store](#direct-reads-from-the-object-store)); or switch the registration's direct reads off |
| `QuaderBastionValkeyUnavailable` | warning | Valkey is unreachable, for 5 minutes; rendered only with Valkey configured | The replicas use their own state: limits are divided by the number of replicas, blocks are per replica and answers are asked again. Nothing is served wrongly. Restore Valkey |
| `QuaderBastionReplicasNotReady` | warning | Fewer than 2 replicas are available, for 5 minutes | The Bastion has lost its redundancy; a drain or an upgrade can now cut clients off. Find out why pods are not ready |

## Disable or delete a Bastion

**Disable** on the card stops the Bastion's token from working, without deleting anything. The replicas then treat the main installation as unreachable and serve from their cache within the grace period. **Enable** reverses it. **Delete** asks you to type the name, removes the registration and revokes its tokens at once; the Bastion's pods keep running until you remove them, and answer as for an unreachable main installation.

A Bastion that is no longer needed is removed with `helm uninstall`, and its registration deleted here. Remove its address from `server.additional_base_urls` and `bastions.trusted_proxies` afterwards.

## What the Bastion does not do

- It is read-only. Publishing, pushing images, changing tags and unpublishing are done on the main installation.
- A new version, or a security fix, reaches a Bastion cache after its metadata period (`bastion.cache.metadata_revalidate_after`, default `1m`; artifacts after `artifact_revalidate_after`, default `10m`).
- A token revoked on the main installation is dropped at the Bastion through the feed within seconds; while the main installation is unreachable, it can read on from the cache for up to the grace period.
- Download counts and the audit trail of the main installation include what the Bastion serves from its cache, reported in batches, for every format by the main installation's own rule of what a download is (a pull of an image is the download of its manifest and its layers, a `HEAD` is none). A registration may report `bastions.downloads.events_per_second` downloads a second (default `200`, bursts up to `events_burst`, `2000`); beyond that the main installation answers `429` and the Bastion sends the batch again later. What the Bastion's buffer cannot hold meanwhile is recorded as a `bastion.downloads.dropped` event.
- An image client takes a new registry token for every pull. The Bastion asks the main installation once about such a token, and then serves the image's manifests and layers from its cache without asking again: a warm pull of an image of ten layers costs the main installation 2 to 6 requests (measured: Podman 2, Docker 4, containerd 5 to 6), the token request among them, instead of one more per manifest and layer. One of Docker's 4 is its first look at the tag without a credential, which the Bastion passes on while any repository of the main installation is public. Each of these requests is one of the client address's misses a minute at the Bastion (`bastion.limits.misses.authenticated_per_minute`, default `4000`, about a thousand Docker pulls a minute). Many nodes behind one address that pull at once can reach that limit and get `429`, which image clients retry; for such an address raise the limit or list it in `bastion.blocking.exempt_cidrs`.
- A ClamAV installation that the main installation admits by its client certificate alone cannot update through a Bastion: the Bastion serves the ClamAV definitions to the anonymous caller while the repository is public and **ClamAV definition updates** is on, and to a token that may read it. On the definitions the Bastion's own refusals (its limits, its block list and its failed-sign-in timeout) are `503` with `Retry-After`, the status the format uses for a rate limit, never `429`.
- The ops listener serves its metrics without sign-in; the chart's network policy admits it only from the monitoring namespace.

See also [Endpoints and exposure rules](endpoints.md) for the other way to open an installation to outside clients, [Managing tokens](tokens.md) and [Monitoring and operations](operations.md).
