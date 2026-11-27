# What a scanner says of itself

Status: built ([operations-and-observability.md](../spec/operations-and-observability.md) R-OPS-048,
[Q-OPS-223](../spec/operations-and-observability.md#q-ops-223--a-scanner-reports-itself-on-the-scan-listener-and-packages-records-it-under-the-kind-its-certificate-names),
[ADR-224](../decisions.md#adr-224--a-scanner-reports-itself-through-the-scan-listener-and-packages-keeps-its-row)). The perspective of this
page is what the replicas of `vuln-scanner-trivy` and `malware-scanner-clamav` report about themselves, what of it an administrator reads on
the System page, **Settings › Malware scanning** and **Advisory sources**, and what of it can be believed when a scanner is taken over.

## The boundary

A scanner is the pod that parses hostile content (an image's layers with Trivy, every stored file with `clamd`) and holds no secret of the
installation ([image-scanning.md](image-scanning.md), [malware-scanning.md](malware-scanning.md), ADR-212). It cannot write the replica
registry and is given no means to. It reports itself to the one peer it already reaches, the scan listener of `packages`, and `packages`
writes the row:

```
 scanner pod (vuln-scanner-trivy or malware-scanner-clamav)
   PUT /scan/v1/replicas/<uid>, DELETE at its end ──HTTPS, mutual TLS, the certificate of its kind──▶ packages: scan listener
                                                                                                    kind := the name the certificate lists
                                                                                                    bounds, then one statement into
                                                                                                    scanner_replica (PostgreSQL's clock)
 api ── GET /api/v1/system/scanners (settings:read) ──▶ the rows, bound as text by the screens
```

## What is taken from what is verified

- **The kind** is the name the client certificate lists, which the handshake verified against the internal CA:
  `vulnerabilities.images.registry.client_name` is `trivy`, `malware.scan_listener.client_name` is `clamav`
  ([wire_scanner_report.go](../../internal/app/wire_scanner_report.go) `scannerKindOf`). The stage of the scan listener admits the route for
  such a certificate and no token, and refuses a token, the worker's certificate and a request without TLS with `401`
  ([scan.go](../../internal/auth/scan.go) `byScannerCertificate`). The body names no kind; a member it does not know is refused.
- **The times** are PostgreSQL's: when a report came (`heartbeat_at`), the first report (`started_at`) and when the state or its reason
  changed (`state_since`). Whether a row is lost is decided by them, by the rule of the replica registry.
- **A row is tied to its kind.** A report under the UID of a row of the other kind is refused, and a sign-off removes only a row of the
  certificate's kind ([cluster.sql](../../internal/meta/postgres/queries/cluster.sql) `RecordScannerReplica`, `DeleteScannerReplica`).

## What is the scanner's own statement, and how it is bounded

The pod's and the node's name, the build of Quader, the state and its reason, the tool's version, the slots, ClamAV's database version and
the calls a malware scanner says it works on are what the scanner says. The route checks their size and shape and nothing else
([report.go](../../internal/scanreport/report.go) `Validate`): a body of at most 16 KiB, decoded strictly; names of letters, digits, dots,
hyphens and underscores; versions of letters, digits and `. + _ ~ -`; a reason of letters, digits and underscores; slots 1 to 64; a
database only from `clamav`; calls only from `clamav`, at most its slots, each a digest of `sha256:` and 64 hexadecimal digits with a time.
None of these character sets holds markup, the table checks the same lengths, and the screens bind every value as text. A kind has at most 64 live rows
(`scanreport.MaxRowsPerKind`). Nothing decides on a row: no job, gate, count or limit reads the table, and the worker's calls go to the Service, not to a row. The summary
of the malware settings ("2 ready, 1 scan running") counts the rows since Q-MAL-110, so it is the scanners' statement as the fold under it is
(H-1161 names the page).

**A report names digests** since a malware scanner lists its calls
([Q-OPS-460](../spec/operations-and-observability.md#q-ops-460--a-malware-scanners-report-names-the-calls-its-slots-hold)): the blobs it
reads now, which the worker's calls named. `GET /api/v1/system/scanners` answers them to the holder of `settings:read`, who learns which
blob is checked, not where it is stored; the path of a blob is answered by the scan controller's read only to the holder of
`content:metadata-read` ([malware-scanning.md](../spec/malware-scanning.md) R-MAL-041). A taken-over scanner can list digests of its
choosing, at most its slots: a screen that shows a blob being checked that is not, which is the statement H-1161 already names.

The route writes one table, `scanner_replica`, apart from the registry of R-OPS-003, so no reader of the registry (the key rings, the
support bundle, the fetch shares, the rate-limit shares) counts a scanner.

## Without TLS

With `tls.enabled: false` there is no certificate to tell a kind by: the route admits nothing, no scanner is listed, and the screens say
that the scan listener cannot tell the kind (`replicas: "unverified"`). The NetworkPolicy, which admits only the scanner pods to the scan
listener's port, is what remains of the boundary there ([malware-scanning.md](malware-scanning.md) H-1108, [image-scanning.md](image-scanning.md)
H-1043).

## What this does not cover

### H-1161 · A taken-over scanner can make an administrator read made-up replicas of its kind

Component: the route of the scanner replicas on the scan listener ([replicas.go](../../internal/api/scanlisten/replicas.go)). Subject: an
administrator or an auditor who reads the scanner replicas on the System page, **Settings › Malware scanning** or **Advisory sources**.
Live wherever the scanners run, which is the chart's default. Every pod of a kind holds the same certificate, so one that a hostile image or
file takes over (H-1035, H-1105) can report as many replicas of its kind as it likes, up to 64 live rows, with names, nodes, versions,
states, reasons, slots and, for ClamAV, a database version and calls of its choosing: a fleet that looks ready and current while it is not, or rows
that fill the bound so that a new real replica of the kind is refused and not shown until the made-up rows go stale. It cannot report a row
of the other kind, change the kind of a row, write markup, touch the replica registry, or remove a real row without its UID, which no
answer of the scan listener names. What makes it real: an exploit of the scanner's tool; a pod that can do that can already answer false
verdicts and analyses, which the scanning trusts more than this screen.
