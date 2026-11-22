# Transport encryption

Status: implemented in M0 for every connection that Quader makes or accepts: the client and ops listeners, the endpoint
listeners, PostgreSQL, Valkey and the Sentinels, the object store, the outbound HTTP clients and the OTLP span export.
Every one of them needs an explicit switch for plaintext, except the listeners, which serve plain HTTP until a certificate is
configured; the chart turns TLS on for them by default and plain HTTP there is a chart choice (`tls.enabled: false`, [H-38](../spec/ha-and-helm.md#h-38--plain-http-between-the-edge-proxy-and-the-replicas)).
The requirement is [INV-20](../spec/README.md#design-invariants) and [ADR-127](../decisions.md#adr-127--transport-encryption-on-every-connection-quaders-listeners-included).

This page says, per connection, what is encrypted, what the default verifies, where the material comes from and how a
rotation reaches a running process. The configuration keys are listed in the reference of [README.md](../../README.md#-full-reference).

## Connections and their defaults

| Connection | Configured by | Default | Verified by | Material reloaded |
|---|---|---|---|---|
| clients to the client listener and each endpoint listener | `server.tls`, `server.endpoints.<name>.tls` | plain HTTP until `certificates` is set | the client, not Quader | every `reload_interval` (`10s`) |
| kubelet and monitoring to the ops listener | `server.ops_tls` | `inherit: true`: the client listener's pairs, no client certificate | the kubelet does not verify probes | with the listener |
| every role (`api`, `packages`, `worker`) and the Jobs to PostgreSQL | `database.ssl_mode`, `ssl_root_cert_file`, `ssl_cert_file`, `ssl_key_file` | `verify-full` | chain and host name against the roots | on each new connection |
| every role to Valkey data nodes | `valkey.tls.mode`, `ca_file`, `cert_file`, `key_file`, `server_name` | `verify-full`, the system roots when no `ca_file` | chain and host name | on each handshake |
| every role to Sentinels | `valkey.sentinel.tls` (follows `valkey.tls`) | `true` | as the data nodes | on each handshake |
| every role and the Jobs of the backup lane to the object store | `storage.connections[].endpoint`, `storage.connections[].ca_file` | an endpoint without a scheme is `https` ([Q-STOR-15](../spec/storage.md#q-stor-15--object-store-endpoint-without-a-scheme)) | `ca_file` only when it is set; else the outbound factory's trust store (the system roots while `proxy.outbound.tls.use_system_roots`, plus `proxy.outbound.tls.ca_files`) | on each check of the outbound transport |
| outbound HTTP clients | `proxy.outbound.tls` | system roots, `min_version` `1.2` | chain and host name | at most every 10 s on use |
| OTLP span export | `tracing.ca_file`, `tracing.protocol` | off until `tracing.enabled` | the system roots, or `ca_file` (refused with an `http://` endpoint) | **once, at start** ([H-69](#h-69--tracingca_file-is-read-once-at-start)) |
| worker to the scanner's listener, scanner to the scan listener of `packages` ([link_tls.go](../../internal/httpserver/link_tls.go), [outbound/link.go](../../internal/outbound/link.go)) | `server.internal_tls.ca_file`, `vulnerabilities.images.scanner`, `vulnerabilities.images.registry` ([R-ARCH-038](../spec/architecture.md#r-arch-038--tls-between-quaders-pods)) | each end's own kind's pair, mutual TLS; plain HTTP with the chart's `tls.enabled: false` ([H-1043](image-scanning.md#h-1043--with-tlsenabled-false-the-scanners-links-are-not-authenticated)) | both ends: the chain against the internal CA, and the other kind's name listed exactly among the certificate's DNS names (`<fullname>-vuln-scanner-trivy`, `-malware-scanner-clamav`, `-worker`, `-packages`, `.<namespace>.svc`; [Q-ARCH-552](../spec/architecture.md#q-arch-552--each-end-of-a-link-requires-one-name-listed-exactly-in-the-other-ends-certificate), [H-1072](#h-1072--the-name-check-is-as-strong-as-the-issuers-rule-of-who-may-request-a-name)) | every `reload_interval`, for the next handshake; the worker opens a connection per call |

Plaintext needs an explicit switch on each of these: `database.allow_insecure` for `disable`, `allow` and `prefer`,
`valkey.allow_insecure` for `valkey.tls.mode: disable` and `valkey.sentinel.tls: false`,
`storage.connections[].allow_insecure_endpoint` for an `http://` object-store endpoint, `tracing.allow_insecure` for an OTLP endpoint
that is not `https://`. With tracing enabled, the endpoint in effect (`tracing.endpoint`, else `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`
or `OTEL_EXPORTER_OTLP_ENDPOINT`) must be `https://`, and `OTEL_EXPORTER_OTLP_INSECURE=true` (or the `TRACES` variant) is refused,
unless `tracing.allow_insecure` is `true`; a `tracing.ca_file` with a plaintext endpoint is refused even then, because it
would have no effect. The defaults and refusals are in the `Validate` functions of [config/database.go](../../internal/config/database.go),
[config/valkey.go](../../internal/config/valkey.go), [config/storage.go](../../internal/config/storage.go) and
[config/trace.go](../../internal/config/trace.go) (`validateTransport`).

## Listener TLS

The listener's TLS is built by [httpserver/tls.go](../../internal/httpserver/tls.go) from a `server.tls` block
(`ListenerTLS`). The block is served when `certificates` is not empty; with none, the listener is plain HTTP.

- Versions: `min_version` `1.2` (default) or `1.3`. TLS 1.3 is preferred either way.
- TLS 1.2 suites: ECDHE with AES-GCM or ChaCha20-Poly1305 only. No CBC suites and no RSA key exchange. The list keeps the
  AES-128-GCM suites HTTP/2 requires ([tls.go](../../internal/httpserver/tls.go), `cipherSuites`).
- ALPN: `h2` and `http/1.1`.
- Certificates by SNI: the pair whose names hold the requested name exactly; else the first pair a wildcard covers that the
  client accepts; else the first pair ([tlsfiles/reloader.go](../../internal/tlsfiles/reloader.go), `Certificate`).
- Client certificates: `client_auth` is `none` (default), `verify_if_given` or `require`; the CA bundle is `client_ca_file`
  and a client certificate is verified against it. A further endpoint never inherits its `server.tls` client rules; with
  `client_ca_file` set, it requires one ([config/endpoints.go](../../internal/config/endpoints.go), `resolveEndpoint`).
- The ops listener with `inherit: true` takes the pairs, `min_version` and `reload_interval` of `server.tls` and no client
  authentication, because the probes cannot present a certificate ([config/tls.go](../../internal/config/tls.go),
  `OpsListenerTLS`). With `inherit: false` it has its own block, and a client CA on the client listener does not extend to
  it.

## Reloading material

Every TLS file of Quader is read again and compared by content, not by time, so a Kubernetes Secret volume (which swaps a
symlink) and a file rewritten in place both work. The mechanism is one package, [internal/tlsfiles](../../internal/tlsfiles/doc.go).

- A **pass** reads each piece (one certificate/key pair or one CA bundle) and hashes its bytes. Unchanged content is a
  no-op. Changed content is parsed: a pair must be complete PEM, must load as an X.509 key pair and must not be expired and must already be valid (a pair whose `notBefore` lies ahead is refused and looked at again on every
  pass, so a pair rotated in early waits instead of breaking handshakes)
  ([reloader.go](../../internal/tlsfiles/reloader.go), `parsePair`, `complete`).
- A pair that fails to parse after a half-written rotation is read once more before it is refused.
- Pieces swap independently; the loaded pieces are published as one immutable snapshot behind an atomic pointer, so a
  handshake never sees half a rotation.
- A **broken rotation** keeps the previous content in service, counts it once in `quader_tls_reload_errors_total` and logs
  an error with the file names. Readiness does not change.
- A **broken start** holds the first readiness: the `tls` check of [wire_tls.go](../../internal/app/wire_tls.go) fails until
  every listener's material has loaded. The listener binds and refuses handshakes until then, except the operations listener,
  which answers with a temporary self-generated certificate so that the HTTPS probes reach `/readyz` instead of restarting the
  pod ([H-68](#h-68--the-operations-listener-serves-a-temporary-certificate-until-its-pair-loads)). A PostgreSQL or Valkey
  client whose configured file does not load at start fails its construction, and the process ends
  ([postgres/connect.go](../../internal/meta/postgres/connect.go), `newDBTLS`; [cluster/valkey_tls.go](../../internal/cluster/valkey_tls.go),
  `newValkeyTLS`). A CA file of `proxy.outbound.tls.ca_files` that does not load at start does not end the process: the
  factory reports it, the `outbound` readiness check holds the first readiness, and the object store waits for the file and
  retries (any other outbound configuration error ends the start) ([app.go](../../internal/app/app.go), `bootstrap`; [wire_blob.go](../../internal/app/wire_blob.go), `wireBlob`).
  `storage.connections[].ca_file` is different: a file that does not load at start is a configuration error of the object store, and
  the process ends.
- A **connection keeps what it negotiated**. A rotation reaches the next handshake or the next connection; open connections
  keep their certificate ([H-65](#h-65--a-rotated-out-certificate-keeps-serving-open-connections)).
- Per listener, the reload interval is `server.tls.reload_interval`; the chart sets it through `tls.reloadInterval`.

The developer procedure, the metric families and the tests are in [tls.md](../developer/tls.md).

### Expiry

| Signal | Source |
|---|---|
| `quader_tls_certificate_expiry_timestamp_seconds{kind,listener,name}` | notAfter of the served leaf of each pair, the earliest of each bundle |
| `quader_tls_reloads_total{kind,listener,name}` | changed content that loaded and replaced the previous one |
| `quader_tls_reload_errors_total{kind,listener,name}` | content that did not load, once per distinct content |

`kind` is `listener`, `client` (the pair a pod presents on a link between Quader's pods), `database`, `valkey`, `objectstore` or
`outbound`. `listener` is `client`, `ops`, or `client:<endpoint>` for a further endpoint; on a link, at both of its ends, the unit or
listener it reaches, `vuln-scanner-trivy`, `malware-scanner-clamav` or `scan` ([Q-HA-373](../spec/ha-and-helm.md#q-ha-373--every-name-an-operator-or-an-auditor-reads-of-a-scanner-unit-carries-its-tool)); and empty for another client ([tlsfiles/metrics.go](../../internal/tlsfiles/metrics.go)). A certificate
that expires is seen only through the gauge: see [H-66](#h-66--a-loaded-certificate-is-served-past-its-expiry).

## Client connections

**PostgreSQL** ([postgres/connect.go](../../internal/meta/postgres/connect.go), [config/database.go](../../internal/config/database.go)).
`ssl_mode` takes `verify-full` (default), `verify-ca`, `require`, `disable`, `allow` or `prefer`. `verify-full` checks the chain
and the host name against the roots (the file in `ssl_root_cert_file`, or the system roots without one). `verify-ca` and
`require` with a root file check the chain only; `verify-ca` without a root file is refused, because the system roots would
be trusted without a host name check. `require` without a root file is refused unless `allow_insecure` is set, because
the password is then sent to whoever answers. The client certificate pair is optional and read on each new connection.

**Valkey and Sentinels** ([cluster/valkey_tls.go](../../internal/cluster/valkey_tls.go), [cluster/valkey.go](../../internal/cluster/valkey.go),
[config/valkey.go](../../internal/config/valkey.go)). `valkey.tls.mode` takes `verify-full` (default, system roots without a
`ca_file`), `verify-ca` or `disable`. `verify-ca` requires `ca_file` (refused at load and at dial). `disable` requires
`allow_insecure`. At start a Sentinel-mode replica logs a warning when the Sentinels have no password, and another when the
data nodes are verified against the system roots ([valkey.go](../../internal/cluster/valkey.go), `warnWeakTrust`). These are
warnings only: no configuration switch turns them into errors yet ([spec ha-and-helm](../spec/ha-and-helm.md#r-ha-056--valkey-with-sentinel-connection-and-supported-topologies)).
A server certificate the replica does not trust, or a handshake the server refuses, is an answer, not silence: a starting replica
stays unready with the `valkey` check `config_error` until it is fixed. A handshake that ends at its deadline is silence, and the
replica becomes ready degraded after `valkey.startup_answer_timeout` ([Q-HA-331](../spec/ha-and-helm.md#q-ha-331--an-answer-is-the-login-or-its-refusal-and-the-first-connection-also-writes-the-probe-key-and-runs-the-take-script)).

**Object store** ([blob/s3/client.go](../../internal/blob/s3/client.go)). Quader's object-store client takes its trust from
`storage.connections[].ca_file` when it is set; that file is the whole trust store of the client, neither the system roots nor
`proxy.outbound.tls.ca_files` are added, and it is reloaded like any other bundle. Without `ca_file` the client uses the
outbound factory's trust store ([outbound/factory.go](../../internal/outbound/factory.go), `Client`), so a bundle in
`proxy.outbound.tls.ca_files` also vouches for the object store. A proxy credential file is refused rather than ignored (`storage.object_store.http.proxy_credentials_file`).

**Outbound HTTP** ([outbound/trust.go](../../internal/outbound/trust.go), [outbound/factory.go](../../internal/outbound/factory.go)).
`proxy.outbound.tls.use_system_roots` (default `true`) and `ca_files` form the trust store; `min_version` is `1.2`
(default) or `1.3`. A client rebuilds its transport at most every 10 s after a change of the store
(`DefaultTrustCheckEvery`), so a rotated bundle applies to the next connection after that check.

## What the chart renders

The chart is [deploy/helm/quader](../../deploy/helm/quader). The defaults below are from [values.yaml](../../deploy/helm/quader/values.yaml).
Every kind of pod has a pair and a name of its own ([R-HA-063](../spec/ha-and-helm.md#r-ha-063--chart-tls-wiring),
[ADR-215](../decisions.md#adr-215--every-kind-of-pod-has-a-tls-pair-and-a-name-of-its-own-which-the-chart-issues)).

| Value | Default | Effect |
|---|---|---|
| `tls.enabled` | `true` | renders the TLS wiring; with `true` and neither `tls.certManager.issuerRef.name` nor `tls.existingSecrets` the render fails |
| `tls.certManager.issuerRef` (`kind`, `name`, `group`) | `ClusterIssuer`, `""`, `cert-manager.io` | renders a cert-manager `Certificate` `<fullname>-tls-<kind>` per kind (`api`, `packages`, `worker`, `scanner`) with the names `<fullname>-<kind>.<namespace>.svc` and `<fullname>-ops.<namespace>.svc`, `server auth`, and `client auth` for `worker` and `scanner` ([certificates.yaml](../../deploy/helm/quader/templates/certificates.yaml)) |
| `tls.certManager.duration`, `renewBefore`, `privateKey`, `extraDnsNames`, `extraIpAddresses` | `2160h`, `720h`, ECDSA 256, `[]`, `[]` | the Certificates' lifetime and key (`rotationPolicy: Always`), and names every pair carries besides its own, which no check relies on |
| `tls.existingSecrets.<kind>` | `""` | an existing Secret per kind instead of the issuer; refused together with the issuer and when two kinds name one Secret |
| `tls.minVersion`, `tls.reloadInterval` | `"1.2"`, `10s` | `server.tls.min_version` and `reload_interval` |
| `tls.clientCA.secretName` | `""` | when set, `server.tls.client_ca_file` and `client_auth` (`require` by default) |
| `ingress.backendTLS.controller` | `haproxy` | where the backend settings go: `haproxy`, `traefik`, `ingress-nginx` or `manual` |
| `ingress.backendTLS.verify` | `true` | the controller verifies each backend under its kind's name; Traefik needs `serversTransports.api` and `.packages`, for the Ingress of the default endpoint and of every further endpoint (its own `ingress.backendTLS` merged over this block) |
| `ingress.backendTLS.caSecret` | `""` | the Secret whose `ca.crt` the controller verifies with; empty is the Secret of the pair the backend serves |
| `secrets.s3CA.existingSecret`, `key` | `""`, `ca.crt` | mounts the CA of the object store at `/var/run/secrets/quader/s3-ca/` and renders `storage.s3.ca_file` |
| `trustedCA.existingSecret` or `existingConfigMap`, `keys`, `useForObjectStore` | `""`, `""`, `[ca-bundle.crt]`, `false` | mounts the listed files of a Secret or ConfigMap at `/etc/quader/ca/` in every pod of the release, appends them to `proxy.outbound.tls.ca_files` (after the operator's own), and with `useForObjectStore` makes the one file `storage.s3.ca_file`; refused together with `secrets.s3CA` |

What the pods get, from [_secrets.tpl](../../deploy/helm/quader/templates/_secrets.tpl): each pod mounts its own kind's Secret read-only at
`/var/run/secrets/quader/tls/` with `tls.crt` and `tls.key`, and `ca.crt` where a link verifies with it (the worker, the main `packages`
pods and the scanner while the image scanning is on); no pod mounts the Secret of another kind, so a pod that is taken over holds no pair
that passes as another kind ([H-1035](image-scanning.md#h-1035--a-taken-over-analyser-holds-the-scanners-pair) lists what each kind's pair
allows). Pools and endpoint pools hold their role's pair; a pod that serves a further endpoint with a pair of its own holds that one too
([H-57](../spec/endpoints.md#h-57--a-further-endpoint-runs-with-every-credential-of-a-web-replica)). The client CA mounts only into the pods
that open the client listener of the default endpoint. The mount has no `subPath`, so the kubelet's swap of the projected files, pair and
CA together, reaches the process; each kind's Secret rotates on its own. The migration Job and the backup and restore pods mount no listener
material.

The trusted CA bundle (R-HA-043, [Q-HA-272](../spec/ha-and-helm.md#q-ha-272--the-trusted-ca-bundle-is-mounted-in-every-pod-of-the-release-its-files-are-appended-to-ca_files-and-one-file-can-be-the-object-stores-ca)) adds trust for every outbound TLS connection of every pod, upstreams, OIDC issuers, GitLab and webhooks: whoever can write the named Secret or ConfigMap can make Quader accept a certificate it signs, so that source is as protected as the issuer of the pairs. The files are public certificates, projected by `items` without `subPath`; PostgreSQL's connection never uses them (`secrets.databaseCA` is a directory of its own), and the Valkey and backup-target CAs stay their own values. With `useForObjectStore` the file is the object store's whole trust store, not an addition (the rule of `storage.s3.ca_file`). A rotation reaches the pods at the kubelet's pace ([H-804](../spec/ha-and-helm.md#h-804--a-change-of-the-ca-source-reaches-the-pods-at-the-kubelets-pace)). Tested by `TestR_HA_043_TrustedCA`.

The probes use `scheme: HTTPS` on the ops port when `tls.enabled` is true ([_pod.tpl](../../deploy/helm/quader/templates/_pod.tpl)).
The client and ops Service ports declare `appProtocol: https` ([services.yaml](../../deploy/helm/quader/templates/services.yaml)).

The edge verifies each backend under the name of its kind, `<fullname>-api.<namespace>.svc` or `<fullname>-packages.<namespace>.svc`
([Q-HA-348](../spec/ha-and-helm.md#q-ha-348--the-edge-verifies-each-backend-under-its-kinds-name-the-setting-goes-where-each-controller-reads-it),
[_tls.tpl](../../deploy/helm/quader/templates/_tls.tpl)). For HAProxy Ingress the chart renders `secure-backends: "true"` and, with a client
certificate, `secure-crt-secret` on the Ingress ([ingress.yaml](../../deploy/helm/quader/templates/ingress.yaml)), and `secure-sni` and
`secure-verify-ca-secret` on each Service it routes to, where HAProxy Ingress reads backend keys per Service; Traefik gets the kind's
ServersTransport on each Service; ingress-nginx, which reads `proxy-ssl-name` per Ingress only, gets an Ingress per kind. A further
endpoint's backends are verified the same way, or under their own Service name when the endpoint has a pair of its own
([endpoint-ingress.yaml](../../deploy/helm/quader/templates/endpoint-ingress.yaml)). The render refuses `clientCA` with HAProxy and no
`clientCertSecret` ([_validate.tpl](../../deploy/helm/quader/templates/_validate.tpl)), and an endpoint `tls.clientCaSecret` with HAProxy and
no `clientCertSecret`. `verify: false` is the only way to render an unverified backend connection with these three controllers.

The `ServiceMonitor` (`metrics.serviceMonitor.enabled`, [servicemonitor.yaml](../../deploy/helm/quader/templates/servicemonitor.yaml))
scrapes the ops port with `scheme: https` when `tls.enabled` is true, and its `tlsConfig` verifies the pods against `ca.crt` of the `api`
kind's Secret and the name `<fullname>-ops.<namespace>.svc`, which every pair carries ([H-1071](#h-1071--every-pair-also-carries-the-operations-name));
with `metrics.auth: static` it sends the bearer token of `secrets.metricsToken`. The Gateway API edge of the default endpoint is an
`HTTPRoute` (`gateway.enabled`, [httproute.yaml](../../deploy/helm/quader/templates/httproute.yaml)), whose rules go to the Service port
`http` of each role. How the Gateway verifies that connection is the `BackendTLSPolicy`, rendered only with `tls.enabled` and
`tls.backendTLSPolicy.enabled` ([backendtlspolicy.yaml](../../deploy/helm/quader/templates/backendtlspolicy.yaml)): one policy per Service
the edge may route to, for the name of the Service's kind, with the CA from a ConfigMap (`tls.backendTLSPolicy.caConfigMapName`, which the
render requires).

## What this does not cover

The gaps of the spec pages that sit on these connections:

- [H-38](../spec/ha-and-helm.md#h-38--plain-http-between-the-edge-proxy-and-the-replicas): plain HTTP between the edge proxy and the replicas when `tls.enabled` is false.
- [H-41](../spec/operations-and-observability.md#h-41--ingress-enforcement-is-verified-on-one-cni-only): the ops listener is fenced by the ingress NetworkPolicy alone, and its enforcement is verified on one CNI.
- [H-53](../spec/operations-and-observability.md#h-53--span-export-bypasses-the-egress-address-policy): the span export bypasses the egress address policy.

### H-64 · Sentinel trust with the chart defaults

Stage: the connection to each Sentinel and the primary it names ([cluster/valkey.go](../../internal/cluster/valkey.go),
`warnWeakTrust`).

Live with the chart defaults: `secrets.valkeySentinel.existingSecret` is empty, so Sentinels are reached without a password,
and `secrets.valkeyCA.existingSecret` is empty, so the data nodes are verified against the system roots. Subject: which
primary a replica uses. Whoever can send a command to a Sentinel can name another primary. A primary that Sentinel names
passes verification with the system roots when its certificate chains to a public root for the name it is dialed by.

What makes the first half real: any pod that reaches a Sentinel port, which the Valkey operator's policy must restrict. The
second half needs a certificate that a public CA issued for the cluster-internal name of a data node, which is not normally
possible. Both are warned at start, not refused.

### H-65 · A rotated-out certificate keeps serving open connections

Stage: every TLS connection that is open when a certificate is rotated, on the listeners and on the clients.

Live on every installation that rotates a certificate while connections are open. The old certificate stays in use until the
connection closes; a compromised key stays usable through it until then. Subject: client connections to the listeners, and
the Quader-side connections to PostgreSQL, Valkey and the object store, which are recycled by their own lifetimes
(`database.pool.max_conn_lifetime`, `valkey.max_conn_lifetime`).

What makes it real: a key compromised before the rotation and an existing session the attacker can keep. Rotation does not
terminate sessions.

### H-66 · A loaded certificate is served past its expiry

Stage: the listener's certificate after its notAfter ([tlsfiles/reloader.go](../../internal/tlsfiles/reloader.go), `passPiece`).

Live when a renewal does not happen. The reloader checks expiry only when the content changes; an unchanged pair stays in
service after its notAfter, and no pass re-checks it. Verifying clients then refuse the handshake, so the effect is an
outage. The probes do not verify the certificate and keep answering. Subject: the availability of every client of the
listener. The signals are the expiry gauge and the alert suggested in [tls.md](../developer/tls.md).

What makes it real: a renewal that stops producing new content, with the gauge not alerted.

### H-67 · No revocation check for client certificates

Stage: the client certificate on a listener with `client_auth` set ([httpserver/tls.go](../../internal/httpserver/tls.go)).

Live on an installation that sets `tls.clientCA` or `server.tls.client_ca_file` (dormant by default, since the chart's
`tls.clientCA.secretName` is empty). Quader checks a client certificate against its CA bundle only. It performs no CRL or OCSP
check and does not staple OCSP responses. A certificate that the CA revokes is accepted until it expires. Subject: the
clients of the mutual-TLS listener.

What makes it real: a revoked client certificate that is still within its validity. The mitigation is a short validity of the
issued certificates and a rotation of the CA bundle, both outside Quader.

### H-68 · The operations listener serves a temporary certificate until its pair loads

Stage: start-up of a replica whose TLS pair for the operations listener is absent or does not load
([tls_temporary.go](../../internal/httpserver/tls_temporary.go), [wire_tls.go](../../internal/app/wire_tls.go), `startTLS`).

The earlier gap, a pod restarted by its HTTPS probes while the Secret is missing, is closed: until the configured pair loads,
the operations listener answers handshakes with a certificate that Quader generates in memory (ECDSA P-256, valid for an hour,
named by the instance, never written to disk). `/healthz` answers 200 and `/readyz` answers 503 with the failing `tls` check, so
the kubelet keeps the container, and the real pair serves from the first pass that loads it. The gauge
`quader_tls_temporary_certificate{listener="ops"}` is 1 meanwhile. The client and endpoint listeners have no temporary
certificate: they refuse handshakes until their pair loads, and the replica is unready, so the edge sends no traffic.

What stays open: live with `tls.enabled: true` whenever the pair is late or broken, for as long as it does. The temporary
certificate chains to no CA, so a scraper that verifies the ops listener fails its scrape, and a scraper that does not
verify (the kubelet does not) cannot tell it from the real one. Subject: what `:8081` returns in that window, which is the
probe answers and, with `metrics.auth: none`, the metrics; both are readable by an on-path attacker who impersonates the
listener. What makes it real: a late or broken Secret together with an attacker on the path to `:8081`, which the ops
NetworkPolicy ([H-41](../spec/operations-and-observability.md#h-41--ingress-enforcement-is-verified-on-one-cni-only)) is meant to prevent.

### H-69 · `tracing.ca_file` is read once at start

Stage: the OTLP exporter's trust store ([observability/otlp.go](../../internal/observability/otlp.go), `newOTLPExporter`).

Dormant: live only with `tracing.enabled` and a `tracing.ca_file` whose CA rotates. The file is read when the exporter is
built, and the exporter keeps that pool. A rotated collector CA is not trusted until the process restarts, and the export fails
in the meantime. The span export itself has no egress policy ([H-53](../spec/operations-and-observability.md#h-53--span-export-bypasses-the-egress-address-policy)).

What makes it real: a collector certificate issued by a new CA while the process keeps the old one.

### H-584 · Behind a transaction-mode pooler Quader verifies the pooler, and the pooler's leg to PostgreSQL is the pooler's own setting

Stage: `database.pooler.mode: transaction` ([R-HA-058](../spec/ha-and-helm.md)); the pooled connection of the main pool, and the control connection through the second pooler.

Dormant: live only with a pooler in front of PostgreSQL. Quader's `database.ssl_mode` (`verify-full` in the dev profile) applies to the connection to the pooler: the
pooler's server certificate must name its Service. The leg from the pooler to PostgreSQL is configured on the pooler (CloudNativePG: the `Pooler`'s own PgBouncer settings),
which Quader can neither set nor check, and the pooler's server certificate is the cluster's (CloudNativePG poolers present the cluster's server certificate; that behaviour is the operator's, taken from CloudNativePG's documentation and not verified in this repository), so the pooler
sits inside the database's trust boundary: whoever can read the pooler's pod can use the credentials it holds for the database.

What makes it real: a pooler whose own `server_tls_sslmode` is weaker than `verify-full`, or a pod of the pooler that an attacker with access to its namespace can read.

### H-797 · The Prometheus of the profile `alerts` serves its interface and API in plain HTTP

Stage: the e2e profile `alerts` ([local-profile.md](../developer/local-profile.md#the-alerts-profile), [Q-HA-263](../spec/ha-and-helm.md)); the dev stack only, never the chart.

Live in the profile `alerts`, dormant everywhere else. The Prometheus that kube-prometheus-stack installs listens on 9090 in plain HTTP, and so does kube-state-metrics:
the tests read the alert state through the API server's Service proxy, and a person reads it with a port-forward. The link that carries Quader's data, the scrape of the ops
listener, is TLS and verified against the dev CA (the ServiceMonitor's `tlsConfig`, `quader-ops.<ns>.svc`, which every kind's pair carries); what the plain interface holds is the
metrics of the replicas, which the ops listener serves without authentication in this profile (`metrics.auth: none`). Anyone with a pod in the cluster can read them, which they could
from the ops port if the chart's NetworkPolicy did not close it to the namespace `monitoring`; the Prometheus's pod sits in that namespace and answers to every namespace.

What makes it real: a dev cluster shared with other workloads, where a pod outside `monitoring` reads the replicas' metrics (repository labels, route names, counts) from the Prometheus
instead of the ops port. A production installation runs its own Prometheus and decides how that is protected.

### H-1071 · Every pair also carries the operations name

Component: the scrape of the ServiceMonitor ([Q-HA-349](../spec/ha-and-helm.md#q-ha-349--every-pair-also-carries-the-operations-name-and-the-scrape-verifies-that-name)).
Subject: the metrics Prometheus reads and, with `config.metrics.auth: static`, the bearer token it sends. Live with
`metrics.serviceMonitor.enabled`. One endpoint scrapes the pods of every kind under `<fullname>-ops.<namespace>.svc`, so the pair of any
kind passes as the operations listener of any pod: a holder of one pair who can put itself between Prometheus and a pod's `8081` answers
false numbers, and receives the metrics token, which reads the metrics of every pod. What makes it real: a stolen pair together with a position
in the pod network in front of a scraped pod.

### H-1072 · The name check is as strong as the issuer's rule of who may request a name

Component: the name checks of the links and of the edge ([R-ARCH-038](../spec/architecture.md#r-arch-038--tls-between-quaders-pods),
[Q-ARCH-552](../spec/architecture.md#q-arch-552--each-end-of-a-link-requires-one-name-listed-exactly-in-the-other-ends-certificate)).
Subject: the identity of every kind. Live with a `ClusterIssuer` that any namespace may use, which is cert-manager's default: whoever may
create a `Certificate` in any namespace of the cluster can have that issuer sign `<fullname>-worker.<namespace>.svc` and pass as the worker
on the scanner's listener, or as `api` or `packages` toward the edge. The names are not secret, the chart cannot see the issuer's policy, and
the check cannot tell such a certificate from the chart's. Dormant with an `Issuer` in the release namespace whose CA signs nothing else, or
with an issuer policy (cert-manager's approver-policy) that binds these names to the release namespace. What makes it real: a shared
`ClusterIssuer`, a user with the right to create Certificates elsewhere in the cluster, and a position in the pod network.

### H-1073 · A CA rollover reaches each kind with its own renewal

Component: the `ca.crt` of each kind's Secret ([Q-HA-347](../spec/ha-and-helm.md#q-ha-347--tlscertmanager-issues-a-certificate-per-kind-tlsexistingsecrets-names-a-secret-per-kind-instead)).
Subject: the links of the image scanning. Live during a rollover of the issuer's CA. cert-manager writes the issuer's current CA into
`ca.crt` when it issues a Secret, so after the CA changes, a kind whose pair was renewed trusts only the new CA while a kind not yet renewed
presents a pair of the old one: the worker refuses the scanner or the reverse until both are renewed, and scans are put off with
`scanner_unavailable` or `registry_unavailable` meanwhile (no attempt is spent). The edge verifies each kind against that kind's own Secret,
so it is not affected. What makes it real: a new CA in the issuer; renewing the four Certificates together (`cmctl renew`) ends it, and a CA
bundle that holds both CAs during the change avoids it.
