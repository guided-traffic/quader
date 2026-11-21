# ClamAV's definitions: from the internet to the company's ClamAV installations

Status: specified on 2026-11-21 ([malware-scanning.md](../spec/malware-scanning.md) R-MAL-010, R-MAL-012, R-MAL-032 to
R-MAL-034; [formats/clamav.md](../spec/formats/clamav.md); [repositories-and-proxy.md](../spec/repositories-and-proxy.md) R-REPO-075;
[ha-and-helm.md](../spec/ha-and-helm.md) R-HA-069; the client certificates of [formats/clamav.md](../spec/formats/clamav.md)
R-CLAMAV-010 and [endpoints.md](../spec/endpoints.md) R-EP-008, R-EP-010;
[ADR-225](../decisions.md#adr-225--the-clamav-updater-is-a-deployment-of-its-own-and-clamavs-definitions-are-a-repository-format)). Built:
the endpoint, its switch, its answers and the refused hand delete, the updater with its listener and its seed, and the worker's
verification, and the client certificates with the chart's passthrough (the rows of the table below name their tests by name).

The perspective of this page is the path of ClamAV's signature databases: what each hop trusts of the one before it, what a hop that is
taken over can do to the ones after it (Quader's scanners and every ClamAV of the company that updates from Quader), and what the endpoint
lets anyone who reaches it do. What the scanners do with a set once it is in use is [malware-scanning.md](malware-scanning.md).

## The boundary

```
 ClamAV's network (database.clamav.net, Cloudflare)          or an internal mirror (privateMirror)
      │ HTTPS, anonymous; signed .cvd and .cvd.sign
      ▼
 updater pod malware-updater-freshclam (one replica)
   freshclam container: fetches, checks the signatures itself; no credential, no Secret
   quader container: seeds the directory from the set in use, lists and serves it; its TLS pair, no secret of Quader
      │ HTTPS, mutual TLS: the listener admits the worker's name only
      ▼
 worker (holds PostgreSQL, Valkey, object store): checks the header, the archive's MD5 and ClamAV's signature,
   then stores into upstream/clamav and records the set in use
      │
      ├──▶ scan listener of packages ──▶ malware scanners (by digest, R-MAL-006)
      ├──▶ endpoint /api/clamav/upstream/clamav, while "ClamAV definition updates" is on, at its level ──▶ every ClamAV of the company
      └──▶ the same routes on an endpoint whose TLS ends at Quader and requires a certificate of the company's CAs
           (TLS passthrough at the ingress, or a Service of its own) ──▶ a ClamAV with a client certificate, at internal or public
```

## What the updater holds, and what a taken-over `freshclam` reaches

| Container | Holds | Does not hold |
|---|---|---|
| `freshclam` (ClamAV's image, `_base`) | the database directory it shares with `quader`, its configuration (read-only), the way to ClamAV's network | any TLS pair, any token, any configuration of Quader |
| `quader` (role `scanner`, kind `freshclam`) | its TLS pair and the internal CA, the same directory | any secret of PostgreSQL, the object store or Valkey, any key of the key ring, any token |

A `freshclam` taken over by a hostile answer of the network (or of an internal mirror) holds the pod's network position, which reaches what
any pod of the namespace reaches without a credential, and the shared directory. With the directory it can offer the worker any file under
the names the listing takes. It cannot make the worker store a container ClamAV did not sign: the worker checks every container before it
stores it (below). It can offer an older signed set, which the worker refuses as lower than the set in use, and it can stop offering newer
ones ([H-1165](#h-1165--a-taken-over-updater-can-hold-back-newer-definitions-until-the-age-alarm)). A taken-over `quader` container of
the updater presents the updater's certificate: the scan listener then gives it the database routes, which serve the public signature set,
and the report route, where it can make up rows of its own kind as any taken-over scanner can
([scanner-replicas.md](scanner-replicas.md) H-1161); the blob route never admits it. It shares no pod with the worker any
more, so it no longer stands in the network position of the pod that holds the installation's credentials
([H-1113](malware-scanning.md#h-1113--a-taken-over-freshclam-has-the-updater-pods-network-position)).

## What the worker trusts of the updater

- **The link.** The worker calls the updater over mutual TLS and checks its name; the updater's listener admits the worker's name only. The
  updater calls nothing of the worker and holds no credential that writes to Quader: the worker pulls (R-MAL-010, Q-MAL-94).
- **The files.** Only `main.cvd`, `daily.cvd`, `bytecode.cvd` and the `.cvd.sign` of their versions; the worker parses each header from the
  bytes it fetched, refuses a size above `malware.database.max_file_size`, a version lower than the set in use, a `.cld` and a `.cud`, an
  archive whose MD5 differs from the header's, and a header whose signature does not verify against ClamAV's public key carried in Quader's
  code (R-MAL-033). The test seam for the key is a constructor option, never a configuration key.
- **The parsing.** Only the 512 bytes of a header are parsed before the signature verified; a container's tar is read for its member list
  after that, under bounds (1,000 entries, 2 GiB decompressed), inside the worker. The detached signature is stored as bytes and parsed by
  nobody in Quader.
- **An installation's own repository** (the source `repository`, R-MAL-012) is not verified: whoever may deploy there decides what the
  scanners load and what the endpoint serves ([H-1107](malware-scanning.md#h-1107--the-signatures-are-whatever-the-database-repository-holds)).

## What the endpoint gives, and to whom

- **Off by default.** While **ClamAV definition updates** is off, every protocol request to `upstream/clamav` answers 404, for every
  caller (R-REPO-075).
- **On, at a level.** Public by default, by the owner's decision of 2026-11-21
  ([Q-REPO-415](../spec/repositories-and-proxy.md#q-repo-415--who-reads-the-clamav-definitions-when-they-are-switched-on)): anyone who
  reaches the endpoint downloads the databases ([H-1162](#h-1162--a-public-endpoint-serves-the-definitions-to-anyone-who-reaches-it)). They
  are ClamAV's public files; what they tell a reader is which version the installation uses. At a lower level a client authenticates with a
  token in its URL, which `freshclam` writes into its log ([H-1164](#h-1164--a-clients-token-is-in-its-freshclam-configuration-and-its-log)),
  or, at `internal`, with a client certificate ([below](#what-a-client-certificate-admits)).
- **Read only.** The format has no route that writes, `/files/` deploys nothing into it, and a delete by hand is refused for every caller
  (R-CLAMAV-008, R-REPO-075, Q-REPO-416).
- **No refusal stops a client for long.** The format renders a refusal as `401`, `404` or `503`, never `403` or `429`, which would make every
  stock `freshclam` stop updating for 24 or 4 hours (R-CLAMAV-005). A client that a limit refuses retries at its next check.
- **What a client gets is what the scanners use.** The endpoint serves the set in use, byte for byte as ClamAV signed it, with its detached
  signature; every client's `freshclam` checks it again (Q-MAL-99).

## What a client certificate admits

The owner's decision of 2026-11-21 adds client certificates, so that a ClamAV installation needs no token and nothing stands in front
of it ([formats/clamav.md](../spec/formats/clamav.md) R-CLAMAV-010, Q-CLAMAV-8 to Q-CLAMAV-13).

- **Where the certificate is checked.** Only on a further endpoint with `tls.client_ca_file` and `auth.client_certificates: true`, whose
  TLS ends at Quader: the ingress passes the connection through (HAProxy Ingress's `ssl-passthrough`, rendered by
  `endpoints.<name>.ingress.tlsPassthrough`) or a Service of its own carries it. Go's TLS verifies the chain against the endpoint's CA
  bundle and the validity period in the handshake (`RequireAndVerifyClientCert`, `httpserver.NewListenerTLS`); a connection without a
  certificate that verifies is closed before a request is read. Quader reads no certificate and no outcome of a check from a header, so a
  forwarded header cannot claim a subject. On every other endpoint a client certificate is no credential.
- **Who decides who is in.** The CAs of the bundle, which the operator puts in the Secret `tls.clientCaSecret`; the listener's reloader
  reads the bundle every `server.tls.reload_interval` and takes a CA in or out from the next handshake, without a restart
  ([architecture.md](../spec/architecture.md) R-ARCH-035). No screen and no API of Quader changes it. Every certificate a CA of the
  bundle issued, for whatever purpose, is admitted ([H-1177](#h-1177--every-certificate-the-endpoints-cas-issued-is-admitted)), and a
  revoked one until it expires ([H-1178](#h-1178--a-revoked-certificate-is-admitted-until-it-expires)).
- **What the holder reads.** The certificate subject has no principal, no group, no grant and no role, and is neither internal nor
  external; one fixed rule of the decision point lets it `read` the repositories that admit certificate readers while their artifacts are
  `internal` or `public`, which is `upstream/clamav` while its clients are switched on and nothing else
  ([authorization.md](../spec/authorization.md) R-AUTHZ-002). It never fills, writes or holds a capability; no binding, policy or level of
  another repository widens it; at `private` and while the switch is off it reads nothing; the endpoint's exposure bounds it. A token on
  the same request is decided as a token and never falls through to the certificate.
- **No refusal stops a client for long.** A refused handshake is a failed attempt for `freshclam`; on the HTTP level the format's
  answers stay `404` and `503` (R-CLAMAV-005).
- **What is recorded.** The subject and serial in the request log and the usage store, the issuer in the request log (R-CLAMAV-010); a
  refused handshake only as a line of the process log ([H-1180](#h-1180--a-refused-handshake-leaves-only-a-line-of-the-process-log));
  behind a passthrough the client's address is the controller's
  ([H-1179](#h-1179--behind-a-tls-passthrough-the-clients-address-is-the-ingress-controllers)).
- **The client's key** lies on the ClamAV host in the file `FRESHCLAM_CLIENT_KEY` names; an encrypted key's password is in the unit's
  environment (`FRESHCLAM_CLIENT_KEY_PASSWD`). Nothing of it reaches `freshclam`'s log, unlike a token in the URL (H-1164). Whoever reads the
  key on that host reads the definitions as that host until the certificate expires (H-1178).

| Guarantee | Enforced by | Test |
|---|---|---|
| no container reaches `upstream/clamav` from the updater unless its header's signature verifies against ClamAV's key and its archive has the header's MD5 | the database task's check (R-MAL-033, `verify.go`) | `TestR_MAL_033_AContainerFromTheUpdaterIsVerified`, `TestR_MAL_033_TheLegacySignature` (`internal/malware/database`), `TestR_MAL_010_TheTaskImportsFromTheUpdater` (area `malware`) |
| the updater's listener admits the worker's certificate name only, and lists regular files of known names inside its directory | the updater's listener and its `os.Root` (R-MAL-032) | `TestR_MAL_032_TheListing` (`internal/malware/updater`), `TestR_MAL_010_TheTaskImportsFromTheUpdater` (another kind's certificate refused) |
| the updater's certificate reads the scan listener's database routes only, as its own subject, never the blob route | the stage of the scan listener and `malwareDatabasePeer` (R-MAL-006, R-MAL-032) | `TestR_MAL_006_TheScanListenerAdmitsTheMalwareScannerOnItsRoutes` (`internal/auth`) |
| the updater's pod mounts no secret but its TLS pair, and `freshclam` mounts none | the chart's volume list (R-HA-069) | `TestR_HA_069_*` (`test/chart`) |
| the endpoint answers as for a missing repository for every caller while the switch is off, and no route of the format writes | `UpstreamFacts.Closed` in the dispatcher and `/files/`, and the format's routes (R-REPO-075, R-CLAMAV-008) | `TestR_REPO_070_ASystemRepositoryOfUpstreamAnswersNoClient`, `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint`, `TestR_ARCH_010_Conformance` (`internal/formats/clamav`) |
| no answer of the endpoint is a `403` or a `429` | the format's `WriteError` and the chain's error shape (R-CLAMAV-005) | `TestR_CLAMAV_005_NoAnswerIsA403OrA429` |
| no caller deletes a definition file | `Repository.RefusesHandDelete` in `files.Service.Delete` (R-REPO-075, Q-REPO-416) | `TestQ_REPO_416_NoOneDeletesADefinitionFileByHand`, `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint` |
| a client certificate is a credential only on an endpoint with `auth.client_certificates` and `tls.client_ca_file`, never on the default endpoint, and only one that verifies against the endpoint's CAs reaches a request | the configuration's validation (`validateClientCertificates`), the chart's validation, and the listener's TLS (R-EP-001, R-EP-004, R-EP-008, R-EP-010) | `TestR_CLAMAV_010_ClientCertificatesNeedAFurtherEndpointThatVerifiesThem` (`internal/config`), `TestEndpoints/R-EP-008_Q-EP-96_…` (`test/chart`), `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint` (area `malware`: no certificate, an expired one and one of another CA fail the handshake) |
| the certificate subject reads only repositories that admit certificate readers, at `internal` or `public`, never writes or fills, and is refused at `private` and while the switch is off; the exposure bounds it | the fixed rule of the decision point (R-AUTHZ-002, `decideCertificate` in `internal/authz/certificate.go`) | `TestR_CLAMAV_010_TheCertificateSubjectsRule` (`internal/authz`), `TestR_CLAMAV_009_AStockFreshclamUpdatesFromTheEndpoint` (area `malware`: `200` at internal, `404` at private and while off) |
| a token on a request of that endpoint is decided as the token, and a refused one does not fall through to the certificate | the authentication stage's order (R-AUTH-001, `byClientCertificate` after the token and the session) | `TestR_CLAMAV_010_TheCertificateSubject` (`internal/auth`) |
| the certificate is read from the handshake's verified chain, never from a header | `byClientCertificate` reads `tls.ConnectionState.VerifiedChains` only (Q-CLAMAV-8) | `TestR_CLAMAV_010_TheCertificateSubject` (`internal/auth`) |

## What this does not cover

- [H-1140](../spec/malware-scanning.md#h-1140--no-alert-fires-while-no-signature-database-was-ever-stored): no alert fires while no
  signature database was ever stored.
- [H-1106](malware-scanning.md#h-1106--the-updater-reaches-clamavs-network-outside-the-egress-rules): the updater reaches ClamAV's network
  outside the egress rules.
- [H-516](egress.md#h-516--the-chart-restricts-no-egress-of-its-pods): the chart restricts no egress of its pods, the updater included.

### H-1162 · A public endpoint serves the definitions to anyone who reaches it

Component: the endpoint of `upstream/clamav` ([formats/clamav.md](../spec/formats/clamav.md) R-CLAMAV-004). Subject: the installation's
bandwidth and the knowledge of its signature version. Dormant until an administrator switches **ClamAV definition updates** on at `public`,
the default level (Q-REPO-415). Anyone who reaches the default endpoint downloads the three databases, about 110 MiB at the first
fetch and 22 MiB per new daily, without a credential (the default level, by the owner's decision of 2026-11-21); the request figures of the anonymous subject bound how often per address
([usage-and-limits.md](../spec/usage-and-limits.md) R-LIM-002), and no byte figure refuses a document route. The files are ClamAV's public
databases; the version served tells which signatures the installation's own scanning uses. What makes it real: a default endpoint that is
reachable from the internet, where many addresses can each fetch the set.

### H-1163 · Quader checks the legacy signature and not the detached one

Component: the database task's verification ([malware-scanning.md](../spec/malware-scanning.md) R-MAL-033, Q-MAL-95). Subject: every set
from the updater. Live from the updater on. Quader verifies the RSA signature of a container's header over the archive's MD5 with ClamAV's
key, and passes the PKCS#7 signatures of `.cvd.sign` on without checking them; a forged container would need a second preimage of MD5,
which no published attack gives, and `clamd` and every client's `freshclam` check the detached signature too. What makes it real: a database
that ClamAV publishes without the legacy signature (Quader then refuses it as `signature_invalid` and the set in use ages until Quader checks
`.cvd.sign`), or a practical second-preimage attack on MD5.

### H-1164 · A client's token is in its `freshclam` configuration and its log

Component: the client setup for a level below Public ([formats/clamav.md](../spec/formats/clamav.md) R-CLAMAV-006). Subject: the token a
ClamAV installation uses. Dormant while the level is Public; live for every client of an internal or private endpoint. A stock `freshclam`
has no option for a credential; it sends the user information of its `PrivateMirror` URL as Basic and writes the whole URL into its log at
the level `INFO`, so the token is in `freshclam.conf` and in every log `freshclam` writes, and in whatever collects those logs. ClamAV
documents none of it. What makes it real: logs of the ClamAV hosts that are shipped to a central place readable by more people than the
repository; a token with grants beyond reading `upstream/clamav`. A client that authenticates with a client certificate instead carries no
token (R-CLAMAV-010).

### H-1165 · A taken-over updater can hold back newer definitions until the age alarm

Component: the updater ([ha-and-helm.md](../spec/ha-and-helm.md) R-HA-069) and the database task (R-MAL-010). Subject: Quader's scanning and
every ClamAV of the company that updates from Quader. Live from the updater on. The worker stores only containers ClamAV signed, never one
lower than the set in use, so a taken-over `freshclam` or `quader` container of the updater, or a hostile internal mirror, cannot lower the
set; it can stop offering newer ones. The set in use then ages: `QuaderMalwareDatabaseStale` fires after `stale_after` (48 hours), the
repository's page marks a database older than 7 days as outdated, and the clients' `clamd` warns at 7 days. What makes it real: a flaw in
`freshclam`'s download path exploited by a hostile answer of the network or of the mirror an installation names.

### H-1177 · Every certificate the endpoint's CAs issued is admitted

Component: the endpoint that takes client certificates ([formats/clamav.md](../spec/formats/clamav.md) R-CLAMAV-010,
[endpoints.md](../spec/endpoints.md) R-EP-010). Subject: `upstream/clamav` at `internal`. Dormant until an operator configures an endpoint
with `auth.client_certificates`. The listener admits any certificate that chains to a CA of `tls.client_ca_file` and is in its validity
period; nothing narrows it by subject, by extended key usage or by issuing policy. A company CA that also issues laptops, users' e-mail
certificates or other services' certificates admits all of their holders as ClamAV installations: each can download the definitions and
spend its subject's budget. Certificates of the same subject from two CAs of the bundle share one subject in the usage store. What makes it
real: a broad CA in the bundle instead of one that issues only to the hosts that run ClamAV.

### H-1178 · A revoked certificate is admitted until it expires

Component: the TLS of the endpoint that takes client certificates (`httpserver.NewListenerTLS`). Subject: `upstream/clamav` at
`internal`. Dormant until such an endpoint is configured. Go's handshake checks the chain and the validity period, not a CRL or OCSP, and
Quader keeps no list of refused certificates or serials: a certificate whose key was stolen from a ClamAV host, or of a host taken out of
service, keeps reading the definitions until it expires or its CA leaves the bundle, which refuses every certificate of that CA. What makes
it real: long-lived client certificates and a key that leaves its host.

### H-1179 · Behind a TLS passthrough the client's address is the ingress controller's

Component: the endpoint that takes client certificates, reached through `ingress.tlsPassthrough`
([endpoints.md](../spec/endpoints.md) R-EP-008). Subject: the request log's `remote_addr`, an endpoint rate-limit class keyed `address`,
and every rule that reads the client's address. Dormant until such an endpoint is reached through a passthrough. In TCP mode the
controller sends no forwarded header and Quader reads no PROXY protocol, so every connection comes from the controller's pod: the request
log names the controller, and a class keyed `address` puts every ClamAV installation into one bucket. The certificate subject's budget and
an endpoint class keyed `principal` key on the certificate's subject and are not affected. A Service of its own (`service.type:
LoadBalancer`) keeps the client's address where the load balancer does. What makes it real: an operator who needs the hosts' addresses in
the log, or a class keyed `address` on that endpoint.

### H-1180 · A refused handshake leaves only a line of the process log

Component: the TLS of the endpoint that takes client certificates. Subject: the record of who tried to reach the definitions without a
certificate the endpoint admits. Dormant until such an endpoint is configured. A connection refused in the handshake reaches no request
log, no usage row, no audit event and no metric of Quader; Go's server writes `http: TLS handshake error from <address>: <reason>` to the
process log at `WARN` (`httpserver.Server`'s `ErrorLog`), with the TCP peer's address, which behind a passthrough is the controller's
(H-1179). A ClamAV whose certificate expired is therefore noticed by its own log or by the age of its databases, not by Quader. What makes
it real: a fleet of ClamAV hosts whose certificates expire unnoticed, or probing of the endpoint that nobody reads in the process log.
