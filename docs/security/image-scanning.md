# Scanning untrusted container images

Status: R-VULN-033 to R-VULN-037 are built, and the analysis runs in the role `scanner`
([R-VULN-041 to R-VULN-043, R-VULN-046](../spec/vulnerability-scanning.md#r-vuln-041--the-call-from-the-worker-to-the-scanner),
[ADR-212](../decisions.md#adr-212--the-image-analysis-runs-in-a-role-scanner-that-holds-no-secret); [developer page](../developer/vulnerabilities.md#the-role-scanner)).
The database in a repository of Quader ([R-VULN-044, R-VULN-045](../spec/vulnerability-scanning.md#r-vuln-044--the-databases-repository),
[ADR-214](../decisions.md#adr-214--the-analysers-database-lies-in-a-repository-of-quader)) is specified and not built; until it is, the
worker fetches the database through the source `trivy` as before. The perspective of this page is what an attacker who can push an image,
or influence the database, can do to the pods that analyse and detect it.

## The boundary

An image is whatever its pusher made it, and the analyser is a large parser of formats it did not choose. A remote repository brings the
images of anyone the upstream serves. So the design treats the analyser as code that can be made to misbehave, puts the limits outside it,
and runs it in a pod that holds nothing worth taking.

```
 worker job ──HTTPS, mutual TLS──▶ scanner pod ──HTTPS, mutual TLS, scan token──▶ packages: scan listener
 holds the job, the database,      no secret of PostgreSQL, the object store,       one manifest by digest and
 the detection                     Valkey; no key ring; checks every byte;          the blobs of its name; read,
      ▲                            the analyser in a process group of its own       never fill; no block gate
      │
      └──── the package list (CycloneDX, at most 256 MiB) ────┘
 worker: decodes it under limits, writes it anew, stores it, runs the detection on what it wrote ──▶ PostgreSQL
```

What the scanner enforces before the analyser sees a byte is the table of
[R-VULN-036](../spec/vulnerability-scanning.md#r-vuln-036--scanning-untrusted-layers): the compressed size from the manifest, the digest and
the size of every blob as it is pulled, and one pass over every layer that decompresses it and walks its tar. That pass ends a
decompression bomb, an image of many millions of entries and a `..` in a tar name, in code this repository owns and tests, not in the
analyser. The analyser then runs as a process of its own with an empty environment, in a working directory of its own, its process group
killed with the call; its output is bounded before it is answered.

## What the scanner holds, and what a taken-over analyser can do with it

The scanner's pod mounts its own Secret (the scanner's pair, which it serves and presents, and the internal CA), the configuration file, a
spool and `/tmp`, and nothing else; no pod of another kind holds the scanner's pair, and the scanner holds no other kind's: no database, object-store or Valkey credential or CA, no key of the key ring, no
login secret, no trusted CA bundle, no service-account token, and none of the chart's shared extras
([R-HA-065](../spec/ha-and-helm.md#r-ha-065--the-scanner-deployment), [R-ARCH-036](../spec/architecture.md#r-arch-036--the-role-scanner-what-it-serves-builds-reads-and-holds)).
Its process opens no connection but the one to the scan listener and keeps nothing between two calls.

An analyser that a crafted image took over runs as the scanner's user in the scanner's container. What it reaches:

| It can | Bounded by |
|---|---|
| read the image of its call, and its scan token | the call; the token reaches that one manifest and the blobs of its name, for `pull_timeout` ([H-1041](oci-registry.md#h-1041--a-scan-token-is-a-bearer-credential-for-one-image-until-it-runs-out)) |
| write the package list of its own image | nothing: the image's own contents decide that list anyway, so a pusher gains nothing it did not have |
| read the scanner's pair and the internal CA | the pair names the scanner only, and every other side of a link checks the name of the kind it expects ([H-1035](#h-1035--a-taken-over-analyser-holds-the-scanners-pair)) |
| open connections to any address the pod network reaches | the cluster operator's policy; the chart renders no egress rule ([H-1036](#h-1036--the-chart-restricts-no-egress-of-the-scanner)) |
| end its own pod's analysis by memory or by writing | `max_attempts`: the call breaks, the worker records `scanner_lost` and spends an attempt ([H-1044](#h-1044--an-image-can-end-a-scanners-analysis-on-every-attempt)) |
| reach a concurrent analysis of the same pod | only with `slots` above 1 ([H-1037](#h-1037--analyses-that-share-a-scanner-share-its-user)) |

What it cannot: read another repository or push or delete anything (the token's grant and the decision rule of R-OCI-037); reach
PostgreSQL, Valkey or the object store with a credential (there is none in the pod); call the worker (nothing listens for it, and the
worker's pods admit no ingress on a client port); outlive its analysis (the scanner kills the process group with the call and, when no
other analysis runs, every other process of its container, Q-ARCH-530); write the stored analysis of another image (the worker writes the
store); or feed the worker's analyser bytes of its own choosing (the worker writes the document anew,
[H-1038](#h-1038--the-detection-still-runs-the-analyser-in-the-worker)). Whether the analyser can read the scanner process's memory, which
holds the same pair and the tokens of the calls in flight, is the node kernel's ptrace rule for processes of one user; that rule was not
verified on any cluster.

## Who may call the scanner, and how the pull is authorised

The scanner's listener requires a client certificate of the internal CA that lists the worker's name, and the NetworkPolicy admits only the
worker pods that claim the queue `vuln` on its port ([R-VULN-041](../spec/vulnerability-scanning.md#r-vuln-041--the-call-from-the-worker-to-the-scanner),
[Q-VULN-185](../spec/vulnerability-scanning.md#q-vuln-185--each-end-of-a-link-proves-its-kind-the-worker-to-the-scanner-the-scanner-to-the-scan-listener)).
A pod that reaches the port without such a certificate, or with a certificate of the CA for another kind or a wildcard name, gets a refused
handshake and no HTTP answer. The scan listener admits the scanner's name only, the same way. The scanner verifies no token itself: it has no key
to verify with. The scan listener of `packages` does, and it accepts nothing but a scan token there, which only a holder of the key ring can
sign ([R-ARCH-037](../spec/architecture.md#r-arch-037--the-scan-listener-of-packages), R-OCI-037). The token, not the caller, decides what
is read.

A scan reads what the repository holds, a private one included and a blocked image included, and starts no upstream request
([R-VULN-042](../spec/vulnerability-scanning.md#r-vuln-042--the-scanners-pull-through-the-registry)); it counts no download and is limited
by nothing (Q-LIM-66).

## What leaves the installation

Nothing about an image, as long as the analyser behaves as configured. It runs with the flags that switch off its update check, its
telemetry and every database update (`--skip-version-check`, `--disable-telemetry`, `--skip-db-update`, `--skip-java-db-update`,
`--offline-scan`), in the scanner and in the worker. The only outbound request of the feature is the database's: the database
repository's, made by the remote engine of the worker to the repository's upstream under its settings (its address, its account, the egress
rules of [egress.md](egress.md), the outbound proxy), as a background request within the repository's share, and none with a local
repository ([R-VULN-044](../spec/vulnerability-scanning.md#r-vuln-044--the-databases-repository)). It asks for the tag of the database's
image and for its layer, and carries no package name and no digest of an image. That the pinned binary makes no other request under these
flags was read from its options and its log, not measured on the network.

## The database as input

The database decides what is reported about every image. It comes from its repository
([R-VULN-044](../spec/vulnerability-scanning.md#r-vuln-044--the-databases-repository)), so whoever decides that repository's content decides
it ([H-813](#h-813--the-integrity-of-the-database-is-that-of-its-repository)). An archive is read as untrusted input: only `trivy.db` and
`metadata.json` at the root are taken, each within a size bound, and no entry decides a path, so the class of bug behind
GHSA-mcj4-mphf-j9ff (a crafted database writing outside the cache) has nothing to act on. The layer's name decides no path either: the task
takes a digest only as `sha256:` and 64 lower-case hexadecimal digits, and the worker refuses any other digest it reads from the source's row
before it names its cache's directory ([Q-VULN-193](../spec/vulnerability-scanning.md#q-vuln-193--the-layers-digest-is-taken-only-as-sha256-and-64-hexadecimal-digits-in-the-task-and-in-the-worker)).
The worker hashes the layer while it unpacks it and keeps nothing whose hash is not the digest in use. A database that does not pass its
checks is refused and the last good one stays. The task's fills are recorded as `authz.write.allowed` with the actor `system:image-database`
and the reason `image-database` ([authorization.md](../spec/authorization.md) R-AUTHZ-008), so the audit log says when the repository took a
new layer and from where.

## Why a failed scan is visible

A scan that did not finish, for any reason, leaves the evaluation `scan_failed` or `pending_scan` and never `evaluated`, so an image cannot be
made to look clean by being too large, malformed or slow to analyse, or by ending its scanner. A scan put off for a reason of the installation
(no scanner, a busy or draining one, a scan listener that cannot be reached, a refused token) stays `pending` and spends no attempt;
`QuaderVulnerabilityImageScannerUnavailable` shows it when it lasts. The price is an image that stays unevaluated until an operator acts;
that is the right way round.

## The working space

The analyses write in the scanner's spool, the one writable volume of its pod besides `/tmp`, an `emptyDir` with a size limit. An analysis
holds the compressed size of its image plus 64 MiB of the spool's budget before it writes the first blob, and gives it back on every exit;
one that does not fit is answered `scanner_busy`, one that cannot fit an empty spool `spool_too_small`, and the worker puts either off without
an attempt. The chart refuses a scanner spool budget below `slots × (max_image_size + 64 MiB)`
([Q-HA-318](../spec/ha-and-helm.md#q-ha-318--the-working-space-moves-with-the-analysis-the-chart-checks-the-scanners-spool-and-the-workers-returns-to-5-gi)).

The worker's spool holds the unpacked database once, by the sizes in its archive's headers before a byte is written, and 64 MiB per
detection; a detection that does not fit waits two minutes and is then put off (`spool_busy`), and the chart refuses a worker spool budget
below 2 GiB + 512 MiB for the pool that claims `vuln`
([Q-VULN-145](../spec/vulnerability-scanning.md#q-vuln-145--the-working-space-of-the-scanning-is-admitted-through-the-spools-own-budget)).

What this does not do is bound what the analyser itself writes ([H-871](#h-871--the-analysers-own-files-are-bounded-by-a-margin-not-enforced)).

## What this does not cover

### H-810 · Java archives are not analysed

Reading a JAR needs a second database, of Java artifacts, which Quader does not keep, and without it the analyser refuses to work offline on
an image that contains one. The analysis skips `*.jar`, `*.war`, `*.ear` and `*.par`, so vulnerabilities in Java libraries inside an image are
not reported. An image that holds only Java libraries shows `evaluated` with no findings. Keeping the Java database in a repository as the
main database is kept would close this.

### H-811 · The block of an image is the block of its manifest

A pull is refused at the manifest. The layers of a blocked image are not refused, by their digest or through `/files/`, because a layer is
shared between images and is not itself vulnerable or not. A client that already knows the digests can still fetch them. `/files/` of the
manifest item itself is not gated by the pull's check either.

### H-813 · The integrity of the database is that of its repository

Component: the database's repository and the task of [R-VULN-044](../spec/vulnerability-scanning.md#r-vuln-044--the-databases-repository).
Subject: every finding of every image, and every block set on one. Live. The layer is checked against the digest
its manifest names, and the manifest comes over TLS from the repository's upstream, or from whoever pushed it into a local repository.
Nothing checks a signature by the publisher. Who decides what is reported: the publisher; an instance administrator, who sets the upstream
address and the account of `upstream/scanner-trivy` like those of every well-known repository; a holder of `deploy` on a local repository the
setting names; whoever the setting itself names, which is file configuration. What makes it real: a compromised publisher, an upstream address
changed to a registry of an attacker's choosing ([H-1023](egress.md#h-1023--an-address-override-of-a-well-known-repository-moves-every-projects-traffic-to-a-host-of-the-administrators-choice)
for every well-known repository), or a mirror reached by a redirect within the egress rules.

### H-814 · The first pull of a remote image is served before it is scanned

A scan is asynchronous, and a pull-through serves the manifest it fetched. Unless the repository sets `onMissingData: block`, the first client
of a new image gets it before any finding exists. The setting refuses it instead, at the cost of a failed first pull.

### H-815 · The database has no malicious-package source

`kind` is always `vulnerability`, so `blockMalicious` never blocks an image; a block is a triager's decision. An image built to carry malware
is reported only where a CVE describes it.

### H-816 · A narrowed read grant does not reach an image's findings

A finding's version is a digest, and a grant narrowed to a tag is decided by the tag's path, so a caller whose read of a repository is
narrowed by version sees no findings of its images (fail closed). The format has no such grants today (exposure rules are deferred, Q-OCI-23);
the reader of a repository or of a package sees them.

### H-817 · One job scans at a time

One `vuln-image-scan` job runs across the installation, with `concurrency` calls to the scanners at once (2 by default). A backlog of many
images after a new analyser version is worked off at the pace of that one job and of the scanners' capacity; a new database costs no
analysis, only detections in the worker.

### H-871 · The analyser's own files are bounded by a margin, not enforced

The scanner holds what it writes itself (the layout, from sizes it checked against the digests) and 64 MiB for what the analyser leaves below
its working directory: its cache and temporary files, the analysis and the report. Nothing limits the analyser's writing at the operating
system: `Limits.FileBytes` is declared but not applied (no `RLIMIT_FSIZE`), and the 512 MiB output limit is checked after the file exists.
What makes it real: an image whose analysis writes more than 64 MiB beside the layout, beyond the 2 GiB of `spool.headroom`. It then uses room
the budget gave to another slot of the scanner, and past the volume's limit the kubelet evicts the scanner pod: the call breaks and the worker
spends an attempt (`scanner_lost`). The worker's spool is not touched. That the pinned analyser keeps its files within the margin for an
ordinary image was not measured.

### H-872 · The database allowance of the chart is a constant

The chart checks the worker's spool budget against 2 GiB for the unpacked database; the database was 1.6 GiB on 2026-11-10 and changes with
every update. At run time the exact size is held, so a database that outgrows the budget is refused when it is unpacked, not when the chart is
installed: `vuln-image-scan` then ends with a `skipped` result naming the reason, every image stays `pending_scan`, and the signals are that
result, the log and the counter `quader_vuln_image_scans_total{result="spool_busy"}`, which `QuaderVulnerabilityImageScansFailing` covers
after an hour. What makes it real: an installation that sized `worker.spool.sizeLimit` exactly to the check and a database that grows past
2 GiB.

### H-873 · A refused scan waits an hour after the limit is raised

A scan refused with `spool_too_small` is due again after an hour. An operator who raises `vulnScannerTrivy.spool.sizeLimit` or the worker's, or lowers
`max_image_size`, sees the images scanned within that hour, not at once; a rescan of the repository (R-VULN-011) brings them forward. What
makes it real: the first install of a scanner or a pool with a spool that is too small.

### H-1003 · The analyser is an upstream build with the vulnerabilities of its day

The analyser in the image is the upstream release archive at the pin of `tools/versions.env`, checked against its SHA-256; Quader does not
build it. A vulnerability of the Go release or of a library it was built with stays in the image until upstream releases a new build and the
pin is raised, whatever Quader's own toolchain is. The release's container scan reports such a finding, and a finding that does not apply to
how Quader runs the analyser (as a command for one analysis or one detection, with an empty environment, no listener and no network use of its
own, see above) can be accepted for a time in [.trivyignore.yaml](../../.trivyignore.yaml), at the analyser's path only and with a day it ends
([releasing.md](../developer/releasing.md#accepted-findings-of-the-container-scan)). Accepted on 2026-11-19 until 2026-12-19:
CVE-2026-97054, in the HTTP/1 server of `net/http`. What is not covered: a finding in the analyser's parsers, which read the image an attacker
pushed, applies in full and is bounded only by the limits outside the analyser that this page describes and by what the scanner's pod holds;
and nothing tells an installation that already runs an image which analyser build it carries, beyond the image's SBOM.

### H-1035 · A taken-over analyser holds the scanner's pair

Component: the scanner's pod and the pairs per kind of pod ([R-HA-063](../spec/ha-and-helm.md#r-ha-063--chart-tls-wiring),
[Q-HA-345](../spec/ha-and-helm.md#q-ha-345--every-kind-of-pod-has-a-tls-pair-and-a-name-of-its-own-and-the-chart-issues-the-pairs)). Subject:
the scanner's pair, `<fullname>-vuln-scanner-trivy.<namespace>.svc` and `<fullname>-ops.<namespace>.svc`. Live by design: the analyser runs in the pod
that mounts it. With it, a taken-over analyser passes the scan listener's check, which the scanner passes already, and still needs a scan
token for each manifest (ADR-213); it does not pass the scanner's listener (the worker's name only), the edge (the names of `api` and
`packages`) or any link as another kind. As a server it could pose as a scanner toward a worker, which needs a position between the worker
and the Service `<fullname>-vuln-scanner-trivy` (its endpoints, which need the Kubernetes API, or an address the CNI lets it claim), and as an
operations listener toward the scrape ([H-1071](transport-encryption.md#h-1071--every-pair-also-carries-the-operations-name)). What makes it real: a taken-over analyser together with such a position; the scanner has no
service-account token.

What the pair of each other kind allows, for the case that its pod is taken over or its Secret read (each kind's pods hold more than its
pair, so the pair is the smaller loss):

| Kind | Its pair passes as | It does not pass as |
|---|---|---|
| `api` | `api` toward the edge (sessions and logins pass through), with a position in front of `<fullname>-api` | `packages` toward the edge; any client of a link (no `client auth`) |
| `packages` | `packages` toward the edge, and the scan listener toward the scanner, with a position in front of its Service; the scanner checks every digest it pulls, so it can withhold an image but not change one | `api`; any client of a link |
| `worker` | a caller of the scanner, which the NetworkPolicy admits from the pods of the worker of `vuln` only; without a scan token from the key ring a call reads nothing | a server toward the edge; the scanner toward the worker |
| `scanner` | as above | as above |

### H-1036 · The chart restricts no egress of the scanner

Component: the scanner's pod under [ADR-154](../decisions.md#adr-154--network-policies-are-ingress-rules-between-the-charts-pods-each-with-its-port)
and [H-516](egress.md#h-516--the-chart-restricts-no-egress-of-its-pods). Subject: the images being analysed, the scan tokens of the calls in
flight, the TLS pair, and every service the pod network reaches. Live by default. An analyser that an image took over
can open a connection to any address the cluster lets the pod reach: the internet, to send out the image of its call and of later calls on
the same pod (H-1037) and the pair; services in the cluster that admit unauthenticated connections; and the node's or the cloud's metadata
address, where a node identity may be served to any pod. It holds no credential of PostgreSQL, Valkey or the object store to use against them,
and Valkey's ingress example of R-HA-042 does not admit the component `scanner`: the local profile's Valkey policy names its client components,
because the scanner carries the label `quader/serve` that the ops Service selects ([Q-HA-344](../spec/ha-and-helm.md#q-ha-344--the-scanner-carries-the-serve-label-the-valkey-policy-of-the-local-profile-names-valkeys-clients)). The scanner itself needs DNS and the scan listener `8082` of
`packages` and nothing else, so a cluster policy that admits only those two contains it completely. What makes it real: a parser flaw of the
analyser reachable by an image, and a cluster without an egress policy.

### H-1037 · Analyses that share a scanner share its user

Component: a scanner with `config.vulnerabilities.images.scanner.slots` above 1 ([Q-HA-316](../spec/ha-and-helm.md#q-ha-316--the-scanner-runs-as-two-replicas-of-one-slot-with-2-gib-of-memory-and-60-seconds-to-drain)).
Subject: the other analyses of the same pod: their images, their tokens, their results, their memory. Dormant with the default of one slot;
live as soon as an operator raises it. Two analysers of one pod run as one user in one container: one that an image took over can read the
other's layout and token, replace its package list before the scanner answers, and, because the pod is never idle while analyses overlap, keep
a process alive across calls that the reaper of Q-ARCH-530 does not get to. An image that ends its pod's analysis ends the other's too, and
both spend an attempt. What makes it real: more than one slot and a hostile image among the images analysed; raising the replicas instead of
the slots avoids it.

### H-1038 · The detection still runs the analyser in the worker

Component: the detection in the worker ([R-VULN-043](../spec/vulnerability-scanning.md#r-vuln-043--what-the-worker-keeps-of-an-analysis),
Q-VULN-164, Q-VULN-169). Subject: the worker pod and every credential it holds. Live by design. The worker runs `trivy sbom` on a document it
wrote itself from the members it decoded under limits, so the bytes the analyser parses there are Quader's; their values (package names,
versions, `purl`s, licences, properties) are still the image's, up to 4 KiB each. A flaw of the analyser in matching such values against its database
would run in the pod with secrets. What makes it real: a flaw in the analyser's detection path that a value of an image's package database can
reach; the detection's parsing is far smaller than the analysis's, which is why the owner's split puts the analysis in the scanner.

### H-1039 · A remote image whose layers no client brought is not scanned

Component: the scanner's pull from a remote repository ([R-VULN-042](../spec/vulnerability-scanning.md#r-vuln-042--the-scanners-pull-through-the-registry),
Q-VULN-165). Subject: the findings of remote images. Live, as before the role. A manifest that a client pulled while it already had the
layers, or whose background download was not started or refused (R-REPO-065, H-1018), has its layers outside the repository; the scan ends
`layer_missing` until `max_attempts` and then shows `scan_failed`. What makes it real: clients that pull a remote image by digest only, or a
remote with `downloadOnProbe` off; the image is then served from Quader without a finding, which `onMissingData: block` refuses instead.

### H-1042 · The status screen does not list the scanners

Component: the replica registry ([operations-and-observability.md](../spec/operations-and-observability.md) Q-OPS-215). Subject: an operator
who reads only the status screen. Live. A scanner has no database connection and so no registry row; a fleet that is
down shows as scans put off with `scanner_unavailable`, as `QuaderVulnerabilityImageScannerUnavailable` after half an hour, as the
Deployment's state and as missing `quader_scanner_*` series. What makes it real: an installation without the PrometheusRule.

### H-1043 · With `tls.enabled: false` the scanner's links are not authenticated

Component: the links of [R-HA-066](../spec/ha-and-helm.md#r-ha-066--the-links-of-the-image-scanning-inside-the-chart)
([Q-HA-322](../spec/ha-and-helm.md#q-ha-322--with-tlsenabled-false-the-scanners-links-are-plain-as-every-link-of-such-an-installation)).
Subject: the scanner's capacity, the scan tokens and the package lists in transit. Dormant with the chart's default `tls.enabled: true`.
Without TLS the links are plain HTTP and no client certificate is asked: a pod that the NetworkPolicy does not stop (a CNI that does not
enforce it, H-41) can hold the scanner's slots with requests that carry a forged token, until each pull is refused, and a party on the path
reads the tokens and the package lists. A forged token gets nothing from the scan listener, which verifies its signature. What makes it real:
an installation that switched TLS off, on a CNI without NetworkPolicy.

### H-1044 · An image can end a scanner's analysis on every attempt

Component: the scanner pod's limits ([R-HA-065](../spec/ha-and-helm.md#r-ha-065--the-scanner-deployment), Q-VULN-162). Subject: the
scanners' availability. Live. An image built to exceed the pod's memory or spool ends the analysis each time it is
tried, and the kubelet restarts the container; the worker spends an attempt per break (`scanner_lost`) and stops after `max_attempts` (5), and
again for each event that makes the manifest due (a rescan, a new analyser version). Other images wait meanwhile as `scanner_busy` or
`scanner_unavailable`, without spending attempts. What makes it real: a pusher who repeats such images under new digests, which costs the
scanning its capacity but never makes an image read as clean.

### H-1160 · A publisher whose builds keep failing the archive's checks leaves one stored layer per build

Component: the task of [R-VULN-044](../spec/vulnerability-scanning.md#r-vuln-044--the-databases-repository) and its trim
([Q-VULN-194](../spec/vulnerability-scanning.md#q-vuln-194--after-a-run-that-succeeded-the-repositorys-own-cache-period-lets-the-older-layers-and-manifests-go)).
Subject: the object store's room, the availability of every repository it holds. Live, dormant while the publisher is sound. After a run
that succeeded, `upstream/scanner-trivy` keeps the layer in use and the layers of the last 48 hours and lets the older ones go, so it holds
at most about nine layers of about 142 MB (141,735,981 bytes on 2026-11-20). A run that failed trims nothing, on purpose: it has not shown
which layer is good. A layer that the run stored and whose archive then failed the checks of R-VULN-035 (`archive_limit`) stays, and the
next build's run does the same. What makes it real: a publisher that ships broken builds for days, one stored layer per build, four a day,
until a run succeeds and the trim takes them two days after they were stored. A repository of the installation's own keeps every layer it
holds: that is its operator's to keep, and the general cache cleanup of R-REPO-046, which would apply an own remote's period, is not built.
