# Scanning untrusted container images

Status: R-VULN-033 to R-VULN-037 are built, and the analysis runs in the role `scanner`
([R-VULN-041 to R-VULN-043, R-VULN-046](../spec/vulnerability-scanning.md#r-vuln-041--the-call-from-the-worker-to-the-scanner),
[ADR-212](../decisions.md#adr-212--the-image-analysis-runs-in-a-role-scanner-that-holds-no-secret); [developer page](../developer/vulnerabilities.md#the-role-scanner)).
The database in a repository of Quader ([R-VULN-044, R-VULN-045](../spec/vulnerability-scanning.md#r-vuln-044--the-databases-repository),
[ADR-214](../decisions.md#adr-214--the-analysers-database-lies-in-a-repository-of-quader)) is built. The detection in the scanner
([R-VULN-048, R-VULN-049](../spec/vulnerability-scanning.md#r-vuln-048--the-detection-in-the-scanner-a-call-that-carries-the-stored-analysis),
[ADR-229](../decisions.md#adr-229--the-scanner-unit-detects-as-well-and-the-analyser-comes-from-its-own-image)) is built: both steps run
in the scanner, the worker runs no tool, and each scanner pod holds the database read-only. The analyser from its own image
([R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod)) is built: the release
image of Quader carries no analyser, and only the scanner pods hold one, copied by their init container into a volume they mount
read-only. The perspective of this page is what an attacker who can push an image, or influence the database, can do to the pods that
analyse and detect it.

## The boundary

An image is whatever its pusher made it, and the analyser is a large parser of formats it did not choose. A remote repository brings the
images of anyone the upstream serves. So the design treats the analyser as code that can be made to misbehave, puts the limits outside it,
and runs it in a pod that holds nothing worth taking.

```
 worker job ──HTTPS, mutual TLS──▶ scanner pod
 runs no tool; keeps the analysis   container quader: the scanner, both steps; the database read-only; not dumpable;
 as answered after a bounded        no secret of PostgreSQL, the object store, Valkey; no key ring; checks every byte;
 check; reads the report in Go      the analyser in a process group of its own
      ▲                             container database (kind trivy-db): writes the database, reads the scan listener
      │                                  │ HTTPS, mutual TLS: the scan token's pull, and the database's layer by the pod's certificate
      │                                  ▼ packages: scan listener (one manifest by digest and its blobs; read, never fill; no block gate)
      └── the package list (analysis) and the report (detection), each at most 256 MiB, untrusted ──▶ PostgreSQL
```

What the scanner enforces before the analyser sees a byte is the table of
[R-VULN-036](../spec/vulnerability-scanning.md#r-vuln-036--scanning-untrusted-layers): the compressed size from the manifest, the digest and
the size of every blob as it is pulled, and one pass over every layer that decompresses it and walks its tar. That pass ends a
decompression bomb, an image of many millions of entries and a `..` in a tar name, in code this repository owns and tests, not in the
analyser. The analyser then runs as a process of its own with an empty environment, in a working directory of its own, its process group
killed with the call; its output is bounded before it is answered.

## What the scanner holds, and what a taken-over analyser can do with it

The scanner's pod mounts its own Secret (the scanner's pair, which it serves and presents, and the internal CA), the configuration file, a
spool and `/tmp`, the database and the analyser's binary (both read-only in the container `quader`), and nothing else; no pod of another kind holds the scanner's pair, and the scanner holds no other kind's: no database, object-store or Valkey credential or CA, no key of the key ring, no
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
store); change the database or what judges the next image (the database is a read-only mount, written by the container `database`); or
read or change the scanner process's memory, which holds the same pair and the tokens of the calls in flight: the scanner makes itself not
dumpable at its start, so the kernel requires `CAP_SYS_PTRACE`, which the pod drops, whatever the node's ptrace scope
([Q-VULN-203](../spec/vulnerability-scanning.md#q-vuln-203--the-scanners-process-is-not-dumpable-so-its-analysers-can-neither-read-nor-change-it);
shown by a unit test on Linux and looked at in a pod of the kind cluster, [developer page](../developer/vulnerabilities.md#the-detection-in-the-scanner-and-the-loader-r-vuln-048-r-vuln-049)).
No analyser runs in a pod with secrets: the worker runs none, and no image but the scanner pod's init container holds one. The analyser's
binary lies on the read-only mount `/opt/trivy` of the volume `trivy-bin`, which the init container `trivy` wrote as the pod's user before
the scanner started; an analyser cannot replace the binary the next call runs.

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
`--offline-scan`), in the scanner for both steps. The only outbound request of the feature is the database's: the database
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
The loader of every scanner pod reads the layer through the scan listener with the pod's certificate, hashes it while it unpacks it and
keeps nothing whose hash is not the digest the listing named; the routes serve the layer of the digest in use only, and every holder of the
scanner's pair can read it, which is public data. A database that does not pass its checks is refused and the last good one stays. The task's fills are recorded as `authz.write.allowed` with the actor `system:image-database`
and the reason `image-database` ([authorization.md](../spec/authorization.md) R-AUTHZ-008), so the audit log says when the repository took a
new layer and from where.

## Why a failed scan is visible

A scan that did not finish, for any reason, leaves the evaluation `scan_failed` or `pending_scan` and never `evaluated`, so an image cannot be
made to look clean by being too large, malformed or slow to analyse, or by ending its scanner. A scan put off for a reason of the installation
(no scanner, a busy or draining one, a scan listener that cannot be reached, a refused token) stays `pending` and spends no attempt;
`QuaderVulnerabilityImageScannerUnavailable` shows it when it lasts. The price is an image that stays unevaluated until an operator acts;
that is the right way round. Each scanner pod is listed with its state on the System page and under **Advisory sources** by its own report
through the scan listener; what of that report can be believed is [scanner-replicas.md](scanner-replicas.md).

## The working space

The analyses write in the scanner's spool, the one writable volume of its pod besides `/tmp`, an `emptyDir` with a size limit. An analysis
holds the compressed size of its image plus 64 MiB of the spool's budget before it writes the first blob, and gives it back on every exit;
one that does not fit is answered `scanner_busy`, one that cannot fit an empty spool `spool_too_small`, and the worker puts either off without
an attempt. The chart refuses a scanner spool budget below `slots × (max_image_size + 64 MiB)`
([Q-HA-318](../spec/ha-and-helm.md#q-ha-318--the-working-space-moves-with-the-analysis-the-chart-checks-the-scanners-spool-and-the-workers-returns-to-5-gi)).

A detection holds 64 MiB of the same budget, and is put off the same way. The worker's spool holds nothing of the image scanning. The
database lies in a volume of its own, `trivy-db`, which the loader counts against its budget by the sizes in the archive's headers before a
byte is written; the chart refuses a volume below 4 GiB, room for the newest database and the one before it
([Q-HA-380](../spec/ha-and-helm.md#q-ha-380--the-database-volume-follows-spooltype-5-gi-by-default-and-the-chart-refuses-less-than-4-gi)).

What this does not do is bound what the analyser itself writes ([H-871](#h-871--the-analysers-own-files-are-bounded-by-a-margin-not-enforced)).

## The detection in the scanner and the analyser from its own image

The owner's decision of 2026-11-21 moves the detection into the scanner unit and the analyser out of every other image
([ADR-229](../decisions.md#adr-229--the-scanner-unit-detects-as-well-and-the-analyser-comes-from-its-own-image)). The detection, the
loader and the process that is not dumpable are built (the picture above), and so is the init container `trivy` that copies the analyser
from its own image into a volume the scanner mounts read-only (2026-11-21); the release image of every other pod carries no analyser.

- **No analyser in a pod with secrets.** The worker sends the stored analysis to `POST /scan/v1/detections` and decodes the report in Go
  under the limits of an advisory (schema version 2, 50,000 advisories of 256 components, texts cleaned and cut), after it has checked that
  the answer names the database and the analyser version of the call
  ([R-VULN-048](../spec/vulnerability-scanning.md#r-vuln-048--the-detection-in-the-scanner-a-call-that-carries-the-stored-analysis)). The
  release image of Quader carries no analyser at all
  ([R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod): the image holds the
  binary of Quader alone, checked by listing the image's files). H-1038 (the detection ran the analyser in the worker) is closed.
- **What judges the next image cannot be changed by this one.** The database lies in a volume that the container `database` writes and the
  container `quader`, which starts the analysers, mounts read-only; so does the analyser's binary, which the init container copied as the
  pod's user (measured: the copy belongs to user 65532, the user of every analyser, so only the read-only mount protects it)
  ([Q-VULN-196](../spec/vulnerability-scanning.md#q-vuln-196--the-scanner-detects-with-a-copy-of-the-database-in-its-pod-written-by-a-container-of-its-own-and-read-only-to-the-analyser),
  [Q-HA-378](../spec/ha-and-helm.md#q-ha-378--the-analysers-binary-is-copied-by-an-init-container-of-its-own-image-into-a-volume-the-scanner-mounts-read-only)).
  The scanner sets itself not dumpable, so an analyser of its user can neither attach to it nor open its memory, whatever the node's ptrace
  rule ([Q-VULN-203](../spec/vulnerability-scanning.md#q-vuln-203--the-scanners-process-is-not-dumpable-so-its-analysers-can-neither-read-nor-change-it));
  with one slot the reaper of Q-ARCH-530 ends what an analyser left before the next call, and each call has a fresh working directory, `HOME`
  and `TMPDIR`. A taken-over analyser can therefore falsify the answer of its own call, and nothing after it; with
  `slots` above 1 the calls of the same moment as well ([H-1037](#h-1037--analyses-that-share-a-scanner-share-its-user)). A taken-over
  scanner process is [H-1181](#h-1181--a-scanner-pod-whose-own-process-is-taken-over-answers-every-detection-it-serves).
- **The kept analysis is the analyser's own.** The worker checks its size, shape and depth and keeps it as answered; it no longer writes it
  anew, because no process of the worker parses it beyond Go's decoder
  ([Q-VULN-198](../spec/vulnerability-scanning.md#q-vuln-198--the-worker-keeps-the-analysis-as-the-scanner-answered-it-after-a-bounded-check-and-the-rewrite-goes)).
  A hostile analysis is sent back to a scanner at each detection; a detection the analyser refuses deletes it, so the next attempt analyses
  the image again.
- **The database's way into the pod.** The loader reads the layer of the database in use from the scan listener with the pod's
  certificate and no token, hashes it while it unpacks, and keeps nothing whose hash is not the digest the listing named
  ([R-VULN-049](../spec/vulnerability-scanning.md#r-vuln-049--the-database-in-the-scanner-pod)). Who decides the database is unchanged
  ([H-813](#h-813--the-integrity-of-the-database-is-that-of-its-repository)); every holder of the scanner's pair can read it, which is
  public data.
- **The init container** runs the publisher's image by its digest with the pod's security context (user 65532, a read-only root file
  system, no privilege escalation, every capability dropped, seccomp `RuntimeDefault`), runs the image's own `cp` and nothing else, mounts
  one volume, holds no Secret, configuration or token, and ends after the copy; a node pulls the image when a pod starts without it
  ([H-1182](#h-1182--a-node-pulls-the-analysers-image-from-a-third-partys-registry-when-a-scanner-pod-starts)). Shown in a pod of the kind
  cluster on 2026-11-21: the copy as user 65532 under these restrictions, and the scanner's analysis and detection with the copied binary
  ([developer page](../developer/vulnerabilities.md#the-analyser-from-its-own-image-r-ha-070)).
- **What the other gaps become.** [H-1003](#h-1003--the-analyser-is-an-upstream-build-with-the-vulnerabilities-of-its-day) stays, with the
  publisher's image of the pin in place of the release archive: the release's container scan no longer sees the analyser, which Renovate
  raises by digest and, for a new version, by a reviewed pull request (Q-HA-381); its accepted finding at `usr/local/bin/trivy` went with
  the path (2026-11-21), the finding itself stays with the analyser. [H-871](#h-871--the-analysers-own-files-are-bounded-by-a-margin-not-enforced) holds for a detection's 64 MiB too.
  [H-872](#h-872--the-database-allowance-of-the-chart-is-a-constant) moves to the scanner's database volume, whose check is 2 × 2 GiB; a
  database that outgrows two copies replaces the newest instead of failing. [H-817](#h-817--one-job-scans-at-a-time) holds, and a new
  database now costs detections in the scanners' slots (R-VULN-048 says how long).

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
of a new image gets it before any finding exists. The setting refuses it instead, at the cost of a failed first pull, whose text tells the
client that the image is not scanned yet and to try again in a minute (R-VULN-016, Q-VULN-272).

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
analysis, only detections in the scanners' slots, which analyses share (R-VULN-048 says how long a cycle takes).

### H-871 · The analyser's own files are bounded by a margin, not enforced

The scanner holds what it writes itself (the layout, from sizes it checked against the digests) and 64 MiB for what the analyser leaves below
its working directory: its cache and temporary files, the analysis and the report. Nothing limits the analyser's writing at the operating
system: `Limits.FileBytes` is declared but not applied (no `RLIMIT_FSIZE`), and the 512 MiB output limit is checked after the file exists.
What makes it real: an image whose analysis writes more than 64 MiB beside the layout, beyond the 2 GiB of `spool.headroom`. It then uses room
the budget gave to another slot of the scanner, and past the volume's limit the kubelet evicts the scanner pod: the call breaks and the worker
spends an attempt (`scanner_lost`). The worker's spool is not touched. That the pinned analyser keeps its files within the margin for an
ordinary image was not measured.

### H-872 · The database allowance of the chart is a constant

The chart checks the scanner pod's database volume (`vulnScannerTrivy.database.sizeLimit`) against 2 × 2 GiB for the newest database and the
one before it (Q-HA-380); the database was 1.7 GiB on 2026-11-21 and changes with every update. At run time the loader holds the exact
sizes, so a database that does not fit beside the newest replaces it (detections answer `database_loading` until it is complete), and one
larger than the whole volume is refused when it is unpacked, not when the chart is installed: every detection then answers
`database_loading`, the images keep their last detections, and the signals are the loader's log, the counter
`quader_vuln_image_scans_total{result="database_loading"}` and `QuaderVulnerabilityImageScannerUnavailable`. What makes it real: a volume
sized exactly to the check and a database that grows past 2 GiB, or past the whole volume.

### H-873 · A refused scan waits an hour after the limit is raised

A scan refused with `spool_too_small` is due again after an hour. An operator who raises `vulnScannerTrivy.spool.sizeLimit`, or lowers
`max_image_size`, sees the images scanned within that hour, not at once; a rescan of the repository (R-VULN-011) brings them forward. What
makes it real: the first install of a scanner or a pool with a spool that is too small.

### H-1003 · The analyser is an upstream build with the vulnerabilities of its day

Component: the analyser every scanner pod runs, the binary of the publisher's image `TRIVY_IMAGE` of `tools/versions.env` by its index
digest, copied by the init container `trivy` ([R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod)).
Subject: the scanner pods and what they hold (above). Live. Quader does not build the analyser: a vulnerability of the Go release or of a
library it was built with stays in every scanner pod until the publisher releases a new build and the pin is raised, whatever Quader's
own toolchain is. Renovate raises a new digest of the same version by itself and opens a new version for review
([Q-HA-381](../spec/ha-and-helm.md#q-ha-381--one-pin-by-digest-the-charts-value-held-equal-by-a-chart-test-and-one-renovate-group-that-reviews-a-new-version)).
Since 2026-11-21 the release image carries no analyser, so the release's container scan, which scans that image, no longer sees one.
Since 2026-11-23 the job `upstream-image-scan` of Test and Release scans `TRIVY_IMAGE` by its digest on every push and pull request and
reports its findings of CRITICAL or HIGH that have a fix in the job summary, with a warning, and in the artifact `trivy-upstream-report`
([Q-ARCH-596](../spec/architecture.md#q-arch-596--the-images-the-chart-runs-are-scanned-in-a-job-of-their-own-that-reports-and-gates-nothing),
[releasing.md](../developer/releasing.md#the-images-the-chart-runs)). It gates nothing: a reported finding stays in every scanner pod until
the publisher's new build and the raise of the pin. Its first scan, on 2026-11-23, found none in the analyser's binary and six in the
image's `git` packages, which the scanner pods do not run. What it leaves out: a finding below HIGH, as the container scan does. Known on
2026-11-19 and still in the pinned build, of MEDIUM and therefore not in that report: CVE-2026-97054, in the HTTP/1 server of `net/http`
(the build of upstream is Go 1.27.3); Quader runs the
analyser as a command for one analysis or one detection, with an empty environment, no listener and no network use of its own (see
above), so a server-side finding does not apply; until 2026-11-21 it was accepted in [.trivyignore.yaml](../../.trivyignore.yaml) at the
analyser's path of the release image, which no longer exists. What is not covered: a finding in the analyser's parsers, which read the
image an attacker pushed, applies in full and is bounded only by the limits outside the analyser that this page describes and by what the
scanner's pod holds; and nothing tells an installation which analyser build its scanner pods run beyond the image's digest in the pod's
spec and the version each scanner reports on the System page. What makes it real: a parser flaw of the pinned build that an image reaches,
before the publisher fixes it and the pin follows.

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

### H-1039 · A remote image whose layers no client brought is not scanned

Component: the scanner's pull from a remote repository ([R-VULN-042](../spec/vulnerability-scanning.md#r-vuln-042--the-scanners-pull-through-the-registry),
Q-VULN-165). Subject: the findings of remote images. Live, as before the role. A manifest that a client pulled while it already had the
layers, or whose background download was not started or refused (R-REPO-065, H-1018), has its layers outside the repository; the scan waits
for them `blobs_pending` for 30 minutes (Q-VULN-282), then ends `layer_missing` until `max_attempts` and shows `scan_failed`. What makes it real: clients that pull a remote image by digest only, or a
remote with `downloadOnProbe` off; the image is then served from Quader without a finding, which `onMissingData: block` refuses instead.

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

### H-1181 · A scanner pod whose own process is taken over answers every detection it serves

Component: the scanner's process in a pod of `vuln-scanner-trivy`, with the detection of
[R-VULN-048](../spec/vulnerability-scanning.md#r-vuln-048--the-detection-in-the-scanner-a-call-that-carries-the-stored-analysis).
Subject: the findings of every image whose detection reaches that pod, and the blocks set in triage on them. Live since the detection in
the scanner is built (2026-11-21). The worker takes a report that names the call's database and analyser version and passes
its bounds; it cannot tell a true report from one that leaves findings out. A scanner process that answers false reports therefore makes
those findings resolve at the next re-detection, and a block set in triage on such a finding lifts (R-VULN-012) until a true detection
finds the match again and restores it, for every image the Service sends to that pod until the pod restarts. Today the same process could falsify only new analyses; with the detection there, every
re-detection after every new database passes through it. What stands between an analyser and the process: the read-only database, the binary
(on the read-only mount of the init container's copy, R-HA-070), the reaper with one slot, and the process not dumpable (Q-VULN-203). What makes it real: a flaw in the scanner's own code that a hostile
image or registry answer reaches (its pull, the checks and the walk of R-VULN-036 are part of the boundary, Q-VULN-167), or a kernel flaw
that lets a process of the same user past the non-dumpable flag; with `slots` above 1, a taken-over analyser reaches the detections of the
same moment without either (H-1037). Nothing compares the detections of two pods.

### H-1182 · A node pulls the analyser's image from a third party's registry when a scanner pod starts

Component: the init container `trivy` of [R-HA-070](../spec/ha-and-helm.md#r-ha-070--the-analyser-from-its-own-image-and-the-database-in-the-scanner-pod).
Subject: the availability of the image scanning, and the integrity of the analyser every scanner pod runs. Live with the defaults since
2026-11-21 (the init container is built); dormant where the image scanning is off or the image comes from a mirror of the installation. The image is the publisher's, `docker.io/aquasec/trivy` by its index digest, so the bytes a node runs
are the pinned ones whatever the registry serves (a tag that was replaced, as the publisher's malicious releases of 2026-03 were, does not
reach a digest pin); the risk left is availability. A node without the image pulls it when a scanner pod is scheduled there (58.5 MB on
arm64, 63.4 MB on amd64; 13.5 and 15.6 s by the nodes of a kind cluster of the development machine, measured 2026-11-21): a registry outage, a limit of anonymous pulls per address, or a node without a way to the registry keeps the pod in
its init phase, and the images wait as `scanner_unavailable` (the alert of R-VULN-024). The pull is the node's, outside every egress rule of
the chart, as the pull of ClamAV's image is. What makes it real: rescheduled scanner pods on nodes that never ran one, during an outage or
behind a registry's limit; a mirror or images loaded on the nodes beforehand avoids it (Q-HA-383).

### H-2406 · The analyser's version of a scan is the scanner's own statement

Component: the members `scannerVersion` of an image's grade and of its manifests
([R-VULN-051](../spec/vulnerability-scanning.md#r-vuln-051--a-grade-names-when-by-which-scanner-and-against-which-database-it-was-checked)),
recorded as `tool_version` from the header `Quader-Analyser-Version` of a detection's answer. Subject: every reader of an image's grade, an
anonymous one included. Dormant, and shown for information only: the worker refuses an answer whose version differs from the stored
analysis's, so a scanner process states a version it was asked for, but a taken-over process can state that version over findings of any
analyser, as it can answer any report ([H-1181](#h-1181--a-scanner-pod-whose-own-process-is-taken-over-answers-every-detection-it-serves));
nothing decides on the version. What makes it real: a compromised pod of `vuln-scanner-trivy`, which then has worse to offer than a wrong
version. The database a grade names is Quader's own record (the digest the job called with, and the build time its run recorded), not the
scanner's statement; a Bastion carries neither (Q-VULN-296).
