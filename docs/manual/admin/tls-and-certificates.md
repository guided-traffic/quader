# TLS and certificates

This page explains where Quader uses TLS, which Secrets and files carry the certificates, how client certificates work on Quader's listeners, how Quader verifies PostgreSQL, Valkey and the object store, and how certificates are rotated while Quader keeps running. It ends with a cert-manager example and a list of common failures.

## Where Quader uses TLS

Every connection Quader accepts or makes can be encrypted. The listeners serve plain HTTP until a certificate is configured; the Helm chart turns TLS on for them by default. Outgoing connections verify their peer by default, and PostgreSQL, Valkey, the object store and the span export each need an explicit switch for plaintext.

| Connection | Configured by | Default | Material read again |
|---|---|---|---|
| Clients to the client listener (`:8080`) and each endpoint listener | `server.tls`, `server.endpoints.<name>.tls` | plain HTTP until certificates are set; the chart sets them | every `reload_interval` (10 s) |
| Probes and monitoring to the ops listener (`:8081`) | `server.ops_tls` | the client listener's certificate, no client certificate | with the listener |
| `worker` to the scanner (`:8080` of the role `scanner`) and the scanner to the scan listener of `packages` (`:8082`) | `server.tls`, `server.internal_tls.ca_file`, `vulnerabilities.images.scanner`, `vulnerabilities.images.registry` | mutual TLS with the listener's pair on both sides; the chart sets it | for the next handshake |
| `api`, `packages` and `worker` to PostgreSQL | `database.ssl_mode` and the `ssl_*` files | `verify-full` | on each new connection |
| `api`, `packages` and `worker` to Valkey and the Sentinels | `valkey.tls`, `valkey.sentinel.tls` | `verify-full`, system roots without a CA file | on each handshake |
| `api`, `packages` and `worker` to the object store | `storage.s3.endpoint`, `storage.s3.ca_file` | `https`, the outbound trust store | on use, at most every 10 s |
| Remote repositories to their upstreams | `proxy.outbound.tls` | system roots, TLS 1.2 or later | on use, at most every 10 s |
| Span export (tracing) | `tracing.ca_file` | off | once, at start |

## TLS on Quader's own listeners

### Client listener

`server.tls.certificates` lists one or more certificate and key pairs as PEM files. With no pair the listener speaks plain HTTP; a plain HTTP request to a TLS listener gets `400` with the text "Client sent an HTTP request to an HTTPS server".

| Setting | Values |
|---|---|
| Versions | `min_version` `1.2` (default) or `1.3`; TLS 1.3 is preferred either way |
| TLS 1.2 cipher suites | ECDHE with AES-GCM or ChaCha20-Poly1305 only; no CBC suites, no RSA key exchange |
| ALPN | `h2` and `http/1.1` |
| Certificate choice | the pair whose names hold the requested server name (SNI) exactly; else the first pair whose wildcard covers it; else the first pair |

### Further endpoints

Each further endpoint has its own listener and its own certificate set. Without certificates of its own, an endpoint serves the pairs of `server.tls`. With several pairs (for example an exact host and a wildcard), SNI picks among that endpoint's pairs only; a pair of another endpoint is never offered. `min_version` and `reload_interval` always come from `server.tls`. See [Endpoints and exposure rules](endpoints.md#configure-endpoints-with-the-helm-chart) for the endpoint values.

### Ops listener

With `server.ops_tls.inherit: true` (the default) the ops listener serves the client listener's pairs and asks for no client certificate, because the kubelet's probes cannot present one. With `inherit: false` it has its own block of the same keys. The chart sets this for you: `tls.ops.secretName` gives the ops listener a pair of its own.

If a replica starts while its certificate is missing or broken, the ops listener answers with a temporary certificate that Quader generates in memory (valid for one hour, never written to disk). The probes then reach `/healthz` (200) and `/readyz` (503, check `tls` failing), so the pod is not restarted, and the gauge `quader_tls_temporary_certificate{listener="ops"}` is 1. The client and endpoint listeners refuse handshakes until their pair loads. The temporary certificate chains to no CA, so a scraper that verifies the ops listener fails until the real pair loads.

## Certificates in the Helm chart

Every kind of pod has a certificate and a name of its own: `api`, `packages`, `worker`, `vuln-scanner-trivy` while the image scanning is on, and `malware-scanner-clamav` while the malware scanning is on. A kind that runs one third-party tool carries the tool's name. A pool or an endpoint pool carries its role's. The pair of a kind names `<fullname>-<kind>.<namespace>.svc` and `<fullname>-ops.<namespace>.svc`; the pairs of `worker`, `vuln-scanner-trivy` and `malware-scanner-clamav` also allow client authentication, because they present themselves on the links of the scannings. No pod holds the pair of another kind, so a pod that is taken over cannot pose as one.

The plain path is one value: the cert-manager issuer. The chart then renders a `Certificate` per kind, `<fullname>-tls-<kind>`, with the names and the usages above, and cert-manager writes the Secret of the same name and renews it.

```yaml
tls:
  enabled: true                         # default; false = plain HTTP inside the cluster
  certManager:
    issuerRef:
      kind: ClusterIssuer               # default: ClusterIssuer | Issuer (of the release namespace)
      name: cluster-ca                  # example; required unless existingSecrets names a Secret per kind
      group: cert-manager.io            # default
    duration: 2160h                     # default: 90 days
    renewBefore: 720h                   # default: 30 days
    privateKey:
      algorithm: ECDSA                  # default: ECDSA | RSA; a renewal changes the key too
      size: 256                         # default: ECDSA 256 | 384, RSA 2048 | 3072 | 4096
    extraDnsNames: []                   # default; names every pair carries besides its own, for a client that reaches one pod by port-forward
    extraIpAddresses: []                # default
  minVersion: "1.2"                     # default: 1.2 | 1.3
  reloadInterval: 10s                   # default
  clientCA:
    secretName: ""                      # default: no client certificates on :8080
    key: ca.crt                         # default
    auth: require                       # default: require | verify_if_given
ingress:
  backendTLS:
    controller: haproxy                 # default: haproxy | traefik | ingress-nginx | manual
    verify: true                        # default: the controller verifies each backend under its kind's name
    caSecret: ""                        # default: the Secret of the pair the backend serves, whose ca.crt the controller reads
    clientCertSecret: ""                # default; haproxy: the pair the controller presents when tls.clientCA is set
```

The issuer must be one CA for all kinds: each pod verifies the others against the `ca.crt` of its own Secret. On the first install the pods wait in `ContainerCreating` until cert-manager has written their Secret, which takes seconds.

### Without cert-manager

Name an existing Secret per kind instead of the issuer. The chart never creates them, refuses one Secret named for two kinds, and refuses an issuer and Secrets together.

```yaml
tls:
  existingSecrets:
    api: quader-tls-api                 # example
    packages: quader-tls-packages       # example
    worker: quader-tls-worker           # example
    vuln-scanner-trivy: quader-tls-vuln-scanner-trivy           # example; only while the image scanning is on
    malware-scanner-clamav: quader-tls-malware-scanner-clamav   # example; only while the malware scanning is on
```

Each Secret holds `tls.crt`, `tls.key` and `ca.crt`, issued by one CA, with the names and usages below. With cert-manager and an issuer you choose to manage yourself, these are the Certificates of the kinds that present themselves on a link (release `quader`, namespace `quader`); those of `api` and `packages` have `server auth` only:

```yaml
apiVersion: cert-manager.io/v1          # example
kind: Certificate
metadata:
  name: quader-tls-worker
  namespace: quader
spec:
  secretName: quader-tls-worker
  usages: [digital signature, server auth, client auth]   # client auth for the worker and the scanners only
  dnsNames:
    - quader-worker.quader.svc
    - quader-ops.quader.svc
  issuerRef:
    name: internal-ca                   # example
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1          # example
kind: Certificate
metadata:
  name: quader-tls-vuln-scanner-trivy
  namespace: quader
spec:
  secretName: quader-tls-vuln-scanner-trivy
  usages: [digital signature, server auth, client auth]
  dnsNames:
    - quader-vuln-scanner-trivy.quader.svc
    - quader-ops.quader.svc
  issuerRef:
    name: internal-ca                   # example
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1          # example
kind: Certificate
metadata:
  name: quader-tls-malware-scanner-clamav
  namespace: quader
spec:
  secretName: quader-tls-malware-scanner-clamav
  usages: [digital signature, server auth, client auth]
  dnsNames:
    - quader-malware-scanner-clamav.quader.svc
    - quader-ops.quader.svc
  issuerRef:
    name: internal-ca                   # example
    kind: ClusterIssuer
```

`quader-tls-api` and `quader-tls-packages` look the same with `quader-api.quader.svc` or `quader-packages.quader.svc` and `usages: [digital signature, server auth]`.

### How the ingress controller verifies Quader

The ingress controller connects to Quader over HTTPS and verifies each backend under the name of its kind: `<fullname>-api.<namespace>.svc` for the `api` Services, `<fullname>-packages.<namespace>.svc` for the `packages` Services. A backend that serves another kind's pair is refused.

| Controller | Where the chart puts the name |
|---|---|
| HAProxy Ingress | `secure-sni` and `secure-verify-ca-secret` on each Service the Ingress routes to; `secure-backends` on the Ingress |
| Traefik | `serverstransport` on each Service, from `ingress.backendTLS.serversTransports.api` and `.packages`: create one ServersTransport per kind with that kind's name as `serverName` |
| ingress-nginx | an Ingress per kind (`<fullname>` for `api`, `<fullname>-packages` for `packages`), each with its kind's `proxy-ssl-name`; the first one holds the TLS of the host |
| Gateway API | a `BackendTLSPolicy` per Service with its kind's name (`tls.backendTLSPolicy.enabled`, CA from `caConfigMapName`) |

A further endpoint without a pair of its own is verified the same way. With `endpoints.<name>.tls.existingSecret`, its Services are verified under their own names, `<fullname>-ep-<name>.<namespace>.svc` and `<fullname>-api-ep-<name>.<namespace>.svc`, which that pair must carry.

### The links of the image scanning

While the image scanning is on (`config.vulnerabilities.images.enabled`, the default), the worker calls the scanner and the scanner pulls the image from the scan listener of `packages`. Both links run over mutual TLS: each side presents its own kind's pair, verifies the other against the `ca.crt` of its own Secret, and checks the other's name. The scanner admits only a certificate that lists `<fullname>-worker.<namespace>.svc`, the scan listener only one that lists `<fullname>-vuln-scanner-trivy.<namespace>.svc` or `<fullname>-malware-scanner-clamav.<namespace>.svc`; a wildcard name does not count. The chart sets these names; with the issuer the pairs carry them.

A worker or scanner whose pair lacks `client auth` logs the reason at start; the scanner stays not ready, and scans are put off with `scanner_unavailable` (alert `QuaderVulnerabilityImageScannerUnavailable`). With `tls.enabled: false` both links run in plain HTTP; the scan token and the NetworkPolicies are then what protects them.

Each kind's Secret rotates on its own and reaches its pods without a restart: the worker opens a new connection for every call, and each handshake uses the files as they are.

<details>
<summary>Secrets and where the pods see them</summary>

| Chart value | Keys read | Mounted at | Used for |
|---|---|---|---|
| `tls.certManager` (Secret `<fullname>-tls-<kind>`) or `tls.existingSecrets.<kind>` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/tls/` in the pods of that kind only | client listener, ops listener, scanner's listener, scan listener, and the identity the worker and the scanner present on the links |
| the same Secret | `ca.crt` | `/var/run/secrets/quader/tls/ca.crt` in the `worker`, main `packages`, `vuln-scanner-trivy` and `malware-scanner-clamav` pods | `server.internal_tls.ca_file`: the CA the links of the image scanning verify with |
| `tls.clientCA.secretName` | `ca.crt` | `/var/run/secrets/quader/tls-client-ca/ca.crt` | client certificates on `:8080` (main `api` and `packages` pods only) |
| `endpoints.<name>.tls.existingSecret`, `extraSecrets` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/endpoints/<name>/<n>/` (`n` = position) | endpoint listener |
| `endpoints.<name>.tls.clientCaSecret` | `ca.crt` | `/var/run/secrets/quader/endpoints/<name>/client-ca/ca.crt` | client certificates on the endpoint |
| `secrets.databaseCA` | `ca.crt` | `/var/run/secrets/quader/database-ca/ca.crt` | `database.ssl_root_cert_file` |
| `secrets.databaseClientCert` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/database-client/` | client certificate to PostgreSQL |
| `secrets.valkeyCA` | `ca.crt` | `/var/run/secrets/quader/valkey-ca/ca.crt` | `valkey.tls.ca_file` |
| `secrets.valkeyClientCert` | `tls.crt`, `tls.key` | `/var/run/secrets/quader/valkey-client/` | client certificate to Valkey |
| `secrets.s3CA` | `ca.crt` | `/var/run/secrets/quader/s3-ca/ca.crt` | `storage.s3.ca_file` |

The chart sets these file paths in the configuration itself; setting the same keys under `config` fails the render. The complete list of values is in the [full reference](https://github.com/guided-traffic/quader/blob/main/README.md#-full-reference).

</details>

## Client certificates (mutual TLS)

A listener can ask its clients for a certificate. This is meant for the connection from your ingress controller to Quader, not for user sign-in: a client certificate is checked against the CA bundle and nothing else, it does not identify a user, and a user still needs a session or a token.

| Listener | Setting | Behaviour |
|---|---|---|
| Client listener | `tls.clientCA.secretName` with `auth: require` | a client without a certificate from that CA is refused in the handshake |
| Client listener | `tls.clientCA.secretName` with `auth: verify_if_given` | a certificate is checked if the client presents one |
| Further endpoint | `endpoints.<name>.tls.clientCaSecret` | always required; never inherited from the client listener |
| Ops listener | never set by the chart | the probes cannot present a certificate |

With HAProxy Ingress and `tls.clientCA`, also set `ingress.backendTLS.clientCertSecret` to a Secret with a `tls.crt` and `tls.key` that the CA signed; the render fails otherwise. Quader checks no revocation lists and no OCSP: a revoked client certificate is accepted until it expires, so issue short-lived certificates.

## TLS to PostgreSQL, Valkey and the object store

### PostgreSQL

`database.ssl_mode` is `verify-full` by default: the chain and the host name are checked against `ssl_root_cert_file`, or the system roots without one. For a database with a private CA, such as a CloudNativePG cluster, put its CA into a Secret and name it in `secrets.databaseCA`.

| `ssl_mode` | Needs |
|---|---|
| `verify-full` | a root file, or a server certificate from a public CA |
| `verify-ca` | `ssl_root_cert_file`; checks the chain but not the host name |
| `require` | a root file, or `allow_insecure: true` |
| `disable`, `allow`, `prefer` | `allow_insecure: true` |

A client certificate is optional (`secrets.databaseClientCert`).

### Valkey and Sentinel

`valkey.tls.mode` is `verify-full` (default), `verify-ca` (needs `ca_file`) or `disable` (needs `valkey.allow_insecure`). The Sentinel connections use the same TLS settings while `valkey.sentinel.tls` is `true`. Name the CA of your Valkey certificates in `secrets.valkeyCA`. In Sentinel mode Quader logs a warning at start when the Sentinels have no password or when the data nodes are verified against the system roots; both are warnings, not errors.

### Object store

An endpoint without a scheme is reached over `https`; an `http://` endpoint needs `storage.s3.allow_insecure_endpoint: true`. `secrets.s3CA` renders `storage.s3.ca_file`, which then becomes the whole trust store of the object-store client: the system roots and `proxy.outbound.tls.ca_files` no longer apply to it. Without it, the object store is verified like the upstreams of remote repositories (system roots plus `proxy.outbound.tls.ca_files`).

## Rotating certificates without a restart

### How it works

Quader reads every certificate, key and CA file again and compares the content, not the file time, so both a Kubernetes Secret update and a file replaced on disk are noticed.

- Listener pairs and client CAs are checked every `reload_interval` (10 s). New handshakes use the new certificate; open connections keep the one they negotiated until they close.
- PostgreSQL files apply to the next new connection, Valkey files to the next handshake. Pooled connections are replaced after `database.pool.max_conn_lifetime` and `valkey.max_conn_lifetime` (30 minutes each by default).
- Object-store and upstream CA files apply to the next connection after a check, at most every 10 s.
- A file that does not load (half-written, key and certificate not matching, expired, garbage) leaves the previous material in service, adds one to `quader_tls_reload_errors_total` and logs an error that names the file. A pair whose validity starts in the future waits and is checked again on every pass.
- `tracing.ca_file` is the exception: it is read once at start, so a new collector CA needs a restart.

At start the behaviour is stricter: a listener pair that does not load keeps the replica unready until it loads, and a PostgreSQL, Valkey or `storage.s3.ca_file` file that does not load ends the process.

### Rotate a listener certificate

1. Update the Secret, or let cert-manager renew it.
2. Wait until the kubelet has updated the mounted files and one `reload_interval` has passed.
3. Check that `quader_tls_reloads_total` for the listener has increased and `quader_tls_certificate_expiry_timestamp_seconds` shows the new expiry.

For files outside Kubernetes, replace the files in place. Writing the key and the certificate one after the other is safe (a pass that sees half a rotation keeps the old pair and counts one error); replacing each file by a rename avoids that error.

### Rotate a CA

1. Put the old and the new CA into the bundle that Quader trusts (for example `ca.crt` of `secrets.databaseCA`).
2. Rotate the server certificates to the new CA.
3. Remove the old CA from the bundle.

Each step applies to the next connection or handshake.

### Watch expiry and reloads

| Metric | Meaning |
|---|---|
| `quader_tls_certificate_expiry_timestamp_seconds` | expiry of each served certificate, and the earliest expiry in each CA bundle |
| `quader_tls_reloads_total` | changed files that loaded and replaced the previous content |
| `quader_tls_reload_errors_total` | changed files that did not load; the old material stays in service |

The labels are `kind` (`listener`, `database`, `valkey`, `objectstore`, `outbound`), `listener` (`client`, `client:<endpoint>`, `ops`, empty for outgoing connections) and `name` (the file). Alert on `quader_tls_certificate_expiry_timestamp_seconds - time() < 14 * 86400` and on `increase(quader_tls_reload_errors_total[15m]) > 0`. Quader keeps serving a loaded certificate after it has expired, and the probes do not notice, so the expiry alert is the only warning before clients start refusing the connection.

## cert-manager example

With `tls.certManager.issuerRef` the chart writes the Certificates of Quader's own pods itself (see above). What you still issue are the
certificates of the edge, which carry the public host names: the Ingress's `ingress.tls` Secret, for example through cert-manager's
annotation `cert-manager.io/cluster-issuer` on the Ingress, and those of further endpoints with their hosts and, when the endpoint has a pair
of its own, `<fullname>-ep-<name>.<namespace>.svc` for the ingress controller.

## Common failures

| Symptom | Cause | What to do |
|---|---|---|
| The chart does not render: `tls.certManager.issuerRef.name or tls.existingSecrets is required` | `tls.enabled` is `true` by default | Name your cert-manager issuer in `tls.certManager.issuerRef.name`, or a Secret per kind in `tls.existingSecrets` |
| The chart does not render: two kinds name the same Secret | One pair for several kinds undoes the separation of the kinds | Issue a pair per kind, or let the chart issue them |
| Pods hang in `ContainerCreating` right after the install, waiting for `<fullname>-tls-<kind>` | cert-manager has not issued the Certificate yet, or the issuer refuses it | `kubectl describe certificate <fullname>-tls-<kind>`; fix the issuer named in `tls.certManager.issuerRef` |
| Pods stay unready; `/readyz?verbose=1` shows the `tls` check failing | The listener Secret is missing, uses other key names, or holds a key that does not match the certificate, or a certificate that is expired or not valid yet | Check the Secret's `tls.crt` and `tls.key` and the error log line that names the file |
| The ingress answers with a gateway error | The controller talks plain HTTP to Quader (wrong `ingress.backendTLS.controller`), or cannot verify a backend under its kind's name | Set the controller you run; with named Secrets, make sure each pair holds `<fullname>-<kind>.<namespace>.svc` and its Secret holds `ca.crt` |
| A client gets `400` "Client sent an HTTP request to an HTTPS server" | Plain HTTP sent to a TLS listener | Use `https://` |
| A renewed certificate is not served | The file did not load, or the mount uses `subPath` (a hand-written manifest) | Look for `quader_tls_reload_errors_total` and the error log; mount the Secret as a directory |
| Clients refuse an expired certificate | The renewal stopped producing new content; Quader keeps serving the loaded pair | Fix the issuer; alert on the expiry metric |
| The process ends at start naming a database, Valkey or object-store file | A configured CA or client certificate file is missing or unreadable | Check the Secret named in `secrets.*` |
| The configuration is refused: `verify-ca` without a root file, or plaintext without the insecure switch | The validation rules above | Add the CA file, or set the explicit switch if plaintext is intended |
| Connections to PostgreSQL or Valkey fail with a certificate error | `verify-full` against the system roots for a server with a private CA, or a host name the certificate does not hold | Set `secrets.databaseCA` or `secrets.valkeyCA`; connect by a name in the certificate |
| The object store fails after `secrets.s3CA` was set | That file replaces every other trusted root for the object store | Put the full CA chain of the object store into it |
| The render fails with `tls.clientCA` and HAProxy Ingress | The controller needs a certificate to present | Set `ingress.backendTLS.clientCertSecret` |
| The scanner pods stay unready and image scans show `scanner_unavailable` | A named Secret of `worker` or `vuln-scanner-trivy` lacks `client auth`, or its pair does not list its kind's name, which the other side checks | Issue it with the usages and the name of [the example above](#without-cert-manager); the scanner logs the names it found |
| The `worker`, `packages`, `vuln-scanner-trivy` or `malware-scanner-clamav` pods hang in `ContainerCreating` with a mount error naming `ca.crt` | The Secret of that kind has no `ca.crt` | Let the issuer write `ca.crt` into the Secret, or add it |
