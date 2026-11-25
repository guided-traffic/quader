# OCI image facts: what the version page reads from an image and who sees it

An image's version page shows its description, licence and links, its layers, and its configuration with the environment, the labels and
the build history (R-OCI-039 to R-OCI-041 of [formats/oci.md](../spec/formats/oci.md)). The configuration is the content of a blob a pusher
or an upstream wrote, and an environment variable or a label can hold what a builder baked into the image (a token passed as `ENV`, a
URL with a password in a label). This page says how that content is read, who sees it, and where it never goes. The code is
[image_facts.go](../../internal/formats/oci/image_facts.go) (`readStoredConfig`, `completeConfig`), the reader
[config.go](../../internal/formats/oci/manifest/config.go), the view [view_facts.go](../../internal/formats/oci/view_facts.go)
(`describeFacts`), `Service.Version` of [internal/packages/view](../../internal/packages/view/service.go) and `MayReadPath` in
[visible.go](../../internal/authz/visible.go). The manifests themselves, and the push and the pull of a blob, are
[oci-manifests.md](oci-manifests.md); a remote's fill is [oci-remote.md](oci-remote.md).

## The reader

- The config blob is read only where the registry holds it: from the repository before a push commits, and from the stored item after
  a fetch of the blob through a remote (Q-OCI-271). Nothing is fetched from an upstream for it, and nothing of it is executed or
  interpreted beyond its JSON types.
- It is read by the strict token reader of the manifests (Q-OCI-60): one UTF-8 value, every key once. A key is matched as Docker's
  decoder matches it, exactly or else the one key that equals it without regard to case; two keys that only differ in case, or a value of
  a type the runtime refuses, make the whole configuration unreadable, so the page never shows an environment other than the one a runtime
  takes (Q-OCI-272).
- Bounds: the blob at most 1 MiB, 16 levels, 200,000 values; a kept string at most 4 KiB, cut and marked; 256 environment entries, 256
  labels, 512 history lines; 160 KiB of kept JSON, the rest counted as left out. A NUL is replaced. The row's columns carry a CHECK of
  256 KiB (migration 00132). A blob over the bound is recorded as `too-large` without a byte of it read.
- The facts reach the screen as text: the Angular templates bind them as text, never as HTML, and a value becomes a link only when its
  address is `https` or `http` (`safeHttpUrl`); `javascript:` or `git@…` stay text.

## Who sees it

| Part | Read from | Who sees it |
|---|---|---|
| The description, the licence and the links of the annotations; the layers (digest, media type, size) | the manifests, which every reader of the tag reads | whoever may read the version (R-API-029), in its page; the description also in the package list |
| The configuration (`metadata.configs`: environment, labels, history, entrypoint, command, ports, user, …) and the facts of the labels (`metadata.labeled`) | the config blobs | only a caller who may read every config blob it was read from: the read scope's decision of a `GET` of `<name>/_blobs/<digest>` (`authz.PathReader.MayReadPath`), the same decision the registry makes when it serves the blob (Q-OCI-273) |

- The view adapter names these members and the blobs' paths (`ViewData.ContentMetadata`, `ContentPaths`); the service, not the format,
  drops the members when one path is refused, as it drops the wiki's members (R-API-058). A caller whose grant excludes the blobs' paths
  reads the version and its layers and none of the configuration.
- **Never in a list or a search:** the package list's summary reads the annotations alone, the versions table asks for no facts
  (`formats.ViewFacts` is asked by the version's page only), and the format's `Find` reads nothing.
- **Never in an audit event or a log:** no event carries a configuration, and the reader and the fetch hook log nothing of it; a failure
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

## What this does not cover

### H-2112 · The page shows a configuration whose blob a gate would refuse to serve

The page decides the configuration on the read permission of the config blob's path, not on the gates a download passes: the malware gate
of R-MAL-013, the vulnerability block of R-VULN-015 and the signature gate of R-SIG-061. An image whose pull a gate refuses still shows its
environment and labels to a caller who may read the blob. Live since R-OCI-040; real for an image blocked for a reason that its
configuration itself reveals (a secret in `ENV` found by a scan), which the person who may read the repository sees on the page while the
registry refuses the pull.

- [H-2111](../spec/formats/oci.md#h-2111--a-remotes-image-whose-config-no-client-fetched-through-the-repository-shows-its-configuration-as-not-read):
  a mirror's images show their configuration as not read until a client pulls them.
- [H-2113](../spec/formats/oci.md#h-2113--an-image-stored-before-the-change-shows-no-layers-and-no-configuration-until-it-is-stored-again):
  an image stored before the change shows neither layers nor configuration.
