# The Quader Bastion

How to run a read-only, internet-facing cache of your installation for clients outside your network: register a Bastion, issue its token, set up the main installation, install the Bastion chart with a minimal or a highly available configuration, check that it works, renew its token without an outage, and what each alert of its monitoring means.

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
| `config.bastion.cache.fill_wait_max` | With the bucket backend and Valkey, a replica whose request finds another replica filling the same artifact waits for that fill, sending nothing to its client, up to this time (default `10m`, 30 s to 1 h), and then fetches the artifact itself. A client that gives up before the first byte retries and waits again; with the volume backend and `cacheVolume.routeByPath`, every request for a path reaches one replica and follows its fill |
| `config.valkey.mode: sentinel` | Valkey holds the shared limits, the block list, the fetch coordination and the main installation's answers. It is optional: losing it costs speed, never correctness (the replicas fall back to their own state). Standalone Valkey is for development and refused while `highAvailability.enforce` is on |
| `secrets.valkey`, `secrets.valkeySentinel`, `secrets.valkeyCA` | The user and password of Valkey, the user and password of its Sentinels (`valkey.sentinel.username_file`, `valkey.sentinel.password_file`) and the CA that signed their certificates. A Valkey whose Sentinels require a password needs `secrets.valkeySentinel`; without it the Bastion logs `valkey sentinel authentication is not configured` and cannot reach the Sentinels, so it runs with its own state per replica. Set `usernameKey: ""` for the default user |
| `secrets.signingKey` | Required with the bucket backend or Valkey. Records in the bucket and in Valkey are signed with it, so whoever can write there without the key cannot make the Bastion serve other bytes. Replacing the Secret makes every record unverifiable and costs one refill |
| `secrets.cacheKey` | Optional. With it the Bastion encrypts every object of its cache store, bucket or volume, under keys derived from this one Secret of 32 random bytes (raw or base64, for example `openssl rand -base64 32`); without it the cache is plain. Replacing the Secret rolls the pods and starts an empty cache that refills, and the new pods delete the old key's objects within their first hour; a lost key costs the cache and nothing else. With the volume backend the fills in progress then go to the pod's `/tmp`, which the chart sizes from `config.bastion.cache.spool_max_bytes` |
| `cacheVolume.type: persistent` with `cacheVolume.routeByPath: true` | The alternative without a bucket: a persistent volume per pod (a StatefulSet), and HAProxy Ingress routes a path to the same replica so that the volumes together hold more than one. It needs HAProxy Ingress to accept the chart's backend snippet, and the chart refuses it together with the bucket backend |
| `autoscaling.enabled: true` | The Bastion's request budget towards the main installation is divided by `autoscaling.maxReplicas` instead of `replicaCount` |
| `config.bastion.answers.max_entries` | The main installation's answers a replica keeps in memory, least recently used first (default `200000`): per caller its credential, its read on each repository it reads and, for an image client's registry token or a token narrowed to some packages, its read on each package it reads, about three answers per image pull. Size it to the callers of one `bastion.answers.period` (default `5m`) times the repositories and packages each reads; an answer pushed out costs its caller one request to the main installation at its next read, never a wrong answer |

The chart's `ingress` values carry annotations of HAProxy Ingress (a request limit per client address and a limit of connections). Another ingress controller ignores them: the Bastion's own limits and blocking still hold, the coarse limit in front of them does not. HAProxy Ingress counts per TCP peer address, so behind a load balancer that hides the client address, every client shares one budget until you switch on its proxy protocol or turn `edgeLimit.enabled` off.

## Verify

1. Open **Admin area › Bastions**. Within `bastion.main.handshake_interval` (default `1m`) of the start, every replica appears in the table **Last contact** with its time, the client address the main installation saw, its release, its interface version and the base URL it reported, which must equal the `server.base_url` you set. A replica that is missing has not completed a handshake: look at its log and at the metrics below. The token's **Last used** column shows the same moment.
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
- An image client takes a new registry token for every pull. The Bastion asks the main installation once about such a token, and then serves the image's manifests and layers from its cache without asking again: a warm pull of an image of ten layers costs the main installation 2 to 6 requests (measured: Podman 2, Docker 4, containerd 5 to 6), the token request among them, instead of one more per manifest and layer. One of Docker's 4 is its first look at the tag without a credential, which the Bastion passes on while any repository of the main installation is public. Each of these requests is one of the client address's misses a minute at the Bastion (`bastion.limits.misses.authenticated_per_minute`, default `300`). Many nodes behind one address that pull at once can reach that limit and get `429`, which image clients retry; for such an address raise the limit or list it in `bastion.blocking.exempt_cidrs`.
- A ClamAV installation that the main installation admits by its client certificate alone cannot update through a Bastion: the Bastion serves the ClamAV definitions to the anonymous caller while the repository is public and **ClamAV definition updates** is on, and to a token that may read it. On the definitions the Bastion's own refusals (its limits, its block list and its failed-sign-in timeout) are `503` with `Retry-After`, the status the format uses for a rate limit, never `429`.
- The ops listener serves its metrics without sign-in; the chart's network policy admits it only from the monitoring namespace.

See also [Endpoints and exposure rules](endpoints.md) for the other way to open an installation to outside clients, [Managing tokens](tokens.md) and [Monitoring and operations](operations.md).
