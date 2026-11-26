# OCI image facts: what the version page reads from an image and who sees it

An image's version page shows its description, licence and links, its layers, and its configuration with the environment, the labels and
the build history (R-OCI-039 to R-OCI-041 of [formats/oci.md](../spec/formats/oci.md)). The configuration is the content of a blob a pusher
or an upstream wrote, and an environment variable or a label can hold what a builder baked into the image (a token passed as `ENV`, a
URL with a password in a label). This page says how that content is read, who sees it, and where it never goes. The code is
[image_facts.go](../../internal/formats/oci/image_facts.go) (`readStoredConfig`), the job `oci-describe` of
[describe.go](../../internal/formatdata/oci/describe.go), the reader [config.go](../../internal/formats/oci/manifest/config.go), the view [view_facts.go](../../internal/formats/oci/view_facts.go)
(`describeFacts`), `Service.Version` of [internal/packages/view](../../internal/packages/view/service.go) and `MayReadPath` in
[visible.go](../../internal/authz/visible.go). The manifests themselves, and the push and the pull of a blob, are
[oci-manifests.md](oci-manifests.md); a remote's fill is [oci-remote.md](oci-remote.md).

## The reader

- The config blob is read only where the registry holds it: from the repository before a push commits, and from the stored item by the
  job `oci-describe` once its commit queued it (Q-OCI-271). Nothing is fetched from an upstream for it, and nothing of it is executed or
  interpreted beyond its JSON types. The job reads the blob as the system, for a row, never for a caller: who sees the result is decided
  when the page is read.
- It is read by the strict token reader of the manifests (Q-OCI-60): one UTF-8 value, every key once. A key is matched as Docker's
  decoder matches it, exactly or else the one key that equals it without regard to case; two keys that only differ in case, or a value of
  a type the runtime refuses, make the whole configuration unreadable, so the page never shows an environment other than the one a runtime
  takes (Q-OCI-272).
- Bounds: the blob at most 1 MiB, 16 levels, 200,000 values; a kept string at most 4 KiB, cut and marked; 256 environment entries, 256
  labels, 512 history lines; 160 KiB of kept JSON, the rest counted as left out. A NUL is replaced. The row's columns carry a CHECK of
  256 KiB (migration 00133). A blob over the bound is recorded as `too-large` without a byte of it read.
- The facts reach the screen as text: the Angular templates bind them as text, never as HTML, and a value becomes a link only when its
  address is `https` or `http` (`safeHttpUrl`); `javascript:` or `git@…` stay text.

## Who sees it

| Part | Read from | Who sees it |
|---|---|---|
| The description, the licence and the links of the annotations; the layers (digest, media type, size) | the manifests, which every reader of the tag reads | whoever may read the version (R-API-029), in its page; the description also in the package list |
| The configuration (`metadata.configs`: environment, labels, history, entrypoint, command, ports, user, …) and the facts of the labels (`metadata.labeled`) | the config blobs | only a caller who may read every config blob it was read from: the read scope's decision of a `GET` of `<name>/_blobs/<digest>` (`authz.PathReader.MayReadPath`), the same decision the registry makes when it serves the blob (Q-OCI-273); and only while a download of each would be served to that caller now (below) |

- **Held by the gates of a download.** A caller who may read the blob still sees no configuration while a download of it would not be
  served to that caller now: the malware gate (R-MAL-013), the vulnerability block (R-VULN-015), a remote's delivery rules (R-CUR-003)
  or the signature gate (R-SIG-061) refuses it, or a check it would wait for is still running. The question is asked of the serve point
  of stored content, `/files/` (`files.Service.Hold`), which builds the same `sgate.Serve` as its download (`serveOf`) and asks
  `Serve.Hold`: the decision `Run` takes its answer from (`decide`), in the same order, with nothing recorded (no event, no metric) and
  nothing waited for. The members are dropped and `metadata.contentHeld` carries only the code the download would answer; the page shows
  the product's words of that refusal. A question that fails holds the content as `unavailable`: fail closed.
- The view adapter names these members and the blobs' paths (`ViewData.ContentMetadata`, `ContentPaths`); the service, not the format,
  drops the members when one path is refused, as it drops the wiki's members (R-API-058). A caller whose grant excludes the blobs' paths
  reads the version and its layers and none of the configuration.
- **Never in a list or a search:** the package list's summary reads the annotations alone, the versions table asks for no facts
  (`formats.ViewFacts` is asked by the version's page only), and the format's `Find` reads nothing.
- **Never in an audit event or a log:** no event carries a configuration, and the reader and the job log nothing of it; a failure
  to read leaves the row unread without a log line.
- The answer of the version page carries `Cache-Control: no-store` (R-API-029), so a shared cache does not keep a configuration.

## A Helm chart

A chart's Chart.yaml is its config blob and is read, decided and shown as an image's configuration is (R-OCI-042). Its README and its
`values.yaml` are read on demand from the chart's package, a gzip-compressed tar a pusher wrote, by the bounded reader of untrusted
archives ([chart.go](../../internal/formats/oci/chart/chart.go), R-ARCH-028): nothing is unpacked to disk, at most 10,000 entries, 256
MiB declared and 256 KiB read, each file cut at 64 KiB, a link entry never followed and one that points out of the archive refusing the
read; a file below the chart's root is never taken for its README. The README is wiki content (R-AUTHZ-034), rendered and sanitized by
the README renderer as every format's; the values are content of the package's blob, shown as text to a caller who may read that blob.
The package is read only where the repository holds it.

## Tests

- `TestR_OCI_040_RefusesWhatTwoReadersCouldReadDifferently`, `…_BoundsWhatItKeeps` (unit, `internal/formats/oci/manifest`): the strict
  reading and the bounds.
- `TestR_OCI_040_AFileOfAVersionIsDecidedOnItsPath` (unit, `internal/authz`): the path's decision follows a grant's path pattern.
- `TestR_OCI_040_AnImagesFactsStandOnItsPageForWhoMayReadItsConfig` (integration, area `oci`): a person whose grant excludes
  `**/_blobs/**` reads none of `configs` or `labeled`.
- `TestR_OCI_040_AScreenIsHeldByTheDecisionOfTheDownload` (unit, `internal/signatures/gate`): `Hold` answers the code `Run` answers for
  every refusal, and the pending checks' codes, at once, and records no event.
- `TestR_OCI_040_ContentGoesOnlyWhereItsDownloadWouldBeServed` (unit, `internal/packages/view`): a held file drops the members and names
  the code; a failed question holds them as `unavailable`.
- `TestR_OCI_040_ARemotesConfigurationIsReadOnceItIsStored` (integration, area `ociremote`): a remote whose delivery rules block the image
  holds its configuration with `not_allowed_in_repository`, and the pull of the config is refused.

## What this does not cover

### H-2118 · A chart's README is shown while a gate would refuse its package

The README of a Helm chart is wiki content (R-AUTHZ-034), decided by the wiki's read and not by the checks of a download of the chart's
package, which the configuration and the values are held by. A chart whose package the malware gate, a vulnerability block, the delivery
rules or the signature gate refuses still shows its README to a caller who may read the wiki. Live since R-OCI-042; real for a chart
blocked by a remote's rules or by a finding, whose README text a reader of the repository sees while Helm's pull is refused. The text is
rendered and sanitized as every README is, so what it shows is the author's text, not executable content.

### H-2119 · A writer whom the registry lets read past the signature gate sees the configuration held

The registry passes a writer through the signature gate so that a signing tool can read what it signs (Q-SIG-211); `/files/`, whose
decision holds the page's configuration, has no such pass. A person who may push to a repository whose signature gate refuses an unsigned
image sees its configuration held on the page while `docker pull` serves it to them. Live since the hold; real for a writer of a gated
repository before the image is signed; nothing is shown that should not be.
