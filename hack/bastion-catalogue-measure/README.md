# The size of the Bastion's public catalogue: the measurement

The scripts of the measurement W35-P1, run once on 2026-11-26. Its numbers and what they set are in
[docs/developer/bastion.md](../../docs/developer/bastion.md#the-size-of-the-public-catalogue-measured-2026-11-26). It is a measurement,
not a test: nothing here runs in CI, and nothing here enters the product.

The catalogue does not exist yet. The scripts read the documents it will be made of (the document paths of the plan's package P5) from
the reads the main installation already has, against this checkout's instance of the UI development profile.

| File | What it does |
|---|---|
| [run.sh](run.sh) | Runs one script against this checkout's instance (`deploy/ui-dev/instance.sh`; a worktree uses the one it claims, never instance 1), with the instance's CA. |
| [lib.mjs](lib.mjs) | The HTTP client (break-glass session, token or anonymous; a 429 is waited out and the wait counted), archive builders (tar, zip, ar), SQL through the profile's PostgreSQL container. |
| [generate.mjs](generate.mjs) | The generated large set, in one public project `catalogue-measure`: npm, NuGet, Debian, Ansible, generic and OCI, through each format's publish route; npm findings through the profile's OSV mirror, OCI findings by SQL. |
| [measure.mjs](measure.mjs) | Reads every document of every public package and version, one request at a time, and writes one line per document (bytes raw and gzip, SHA-256, request time); then the statements per document kind. |
| [volatile.mjs](volatile.mjs) | Reads the Ansible entries and the vulnerabilities documents again and hashes them without the time members (`builtAt`, `checkedAt`, `syncedAt`, `scannedAt`): run twice, an advisory sync apart, it shows which documents change by their times alone and how far the entries deduplicate without them. |
| [report.mjs](report.mjs) | The tables from what `measure.mjs` wrote: counts, distributions, dedupe, the builder's time, the incremental case, the sync, scaled sizes. |

## Run it

```shell
make ui-renders GREP='^login as anonymous'                  # the instance with the render seed (the global setup seeds it)
make ui-dev-backend &                                       # quader of the instance, without scanners (UI_SCANNERS=0 is the renders' default)
hack/bastion-catalogue-measure/run.sh generate.mjs          # about 5 minutes, then a minute for the advisory evaluation; GEN_SCALE=0.05 for a quick check
hack/bastion-catalogue-measure/run.sh measure.mjs .out/catalogue-measure 1     # 25 to 30 minutes on a loaded machine
hack/bastion-catalogue-measure/run.sh measure.mjs .out/catalogue-measure 2
hack/bastion-catalogue-measure/run.sh volatile.mjs .out/catalogue-measure 1    # about 8 minutes
hack/bastion-catalogue-measure/run.sh volatile.mjs .out/catalogue-measure 2    # after the next advisory sync (every 15 minutes)
hack/bastion-catalogue-measure/run.sh report.mjs .out/catalogue-measure
```

## What the generated set is

| Format | Packages | Versions each | What a version carries |
|---|---:|---:|---|
| npm | 20 | 50 (`1.0.0` to `1.9.4`) | a README of 4 to 8 KiB that changes with the minor (ten READMEs per package), 3 to 10 dependencies; 6 advisories per package over version ranges |
| npm | 200 | 2 | the same README model |
| NuGet | 150 | 3 | half with an embedded README |
| Debian | 100 | 2 | 10 to 640 files (the contents listing), a changelog of 20 entries and a copyright file |
| Ansible | 8 | 50 (`1.0.0` to `5.9.0`) | 25 modules at the start, one more every fifth version, two revised per version (158 distinct module sources in 50 versions), 5 roles with argument specs, a lookup, a README that changes every release and a guide under `docs/` that never does |
| Ansible | 30 | 2 | 8 modules, 2 roles |
| generic | 300 files | | in 30 folders |
| OCI | 30 | 5 tags (4 indexes, `latest` = `2.0.0`) | an index of two platforms, three layers, a Sigstore bundle referrer; 0 to 320 findings per platform manifest |

The artifact bytes are tiny; what counts is the documents. Choices that move the numbers, and why:

- **An npm README changes with the minor, not with every patch.** A README that names the version in its text would make every version's
  README distinct; one that never changes would make one per package. The model sits between.
- **An Ansible module's documentation changes only when the module does** (two of 25 per release, each adding an option with its
  `version_added`). This is the common case of a maintained collection; a release that regenerates every page would cut the dedupe to 1.
- **OCI findings are written by SQL**, as a scan would have left them (scan row, advisory, finding with the image's packages): the profile
  runs no image scanner and must not fetch the scanner's database. Their `grade` reads `N/A` (`no_database`), which changes a few bytes.
- **The vulnerabilities document is read as the break-glass account**: the anonymous caller gets `401` today; P3 opens the read to it with
  the open findings only. Every generated finding is open, so the bytes are those the anonymous caller will get.
