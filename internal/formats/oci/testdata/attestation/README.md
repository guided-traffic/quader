# A Docker attestation manifest, as Docker Hub served it

Recorded on 2026-11-24 from `registry-1.docker.io` with an anonymous pull token for `library/ubuntu`, kept byte for byte (each
file's SHA-256 is the digest below). The index is the one the owner's installation held for `upstream/docker-hub` →
`library/ubuntu:26.10`, whose two attestation manifests the image scanning had queued and failed with `layer_format`
([vulnerability-scanning.md](../../../../../docs/spec/vulnerability-scanning.md) R-VULN-033, Q-VULN-269). The fixture of
[attestation_fixture_test.go](../../attestation_fixture_test.go) reads them.

| File | Request | Digest |
|---|---|---|
| `ubuntu-26.10-index.json` | `GET /v2/library/ubuntu/manifests/26.10`, `Accept` the OCI index and the Docker list; `Content-Type: application/vnd.oci.image.index.v1+json` | `sha256:29ac5e68db830ab61b7d77c1ffdb78c73f0631de88a5d77bf4399868f64f38dd` |
| `ubuntu-26.10-amd64.json` | `GET /v2/library/ubuntu/manifests/sha256:ed2128ea…`, the index's `linux/amd64` image | `sha256:ed2128eae94f1dce83f9aafb8e1dc2e3f098b5ce733ed4a13ccaf94330e3e0a8` |
| `ubuntu-26.10-amd64-attestation.json` | `GET /v2/library/ubuntu/manifests/sha256:132abadf…`, the index's entry with `vnd.docker.reference.type: attestation-manifest` and `vnd.docker.reference.digest` the `linux/amd64` image, platform `unknown/unknown` | `sha256:132abadfdec9e3f7c580463d0a1f2ca21c66a735d22db069cdba47656ac58236` |
| `ubuntu-26.10-amd64-attestation-config.json` | `GET /v2/library/ubuntu/blobs/sha256:51da2f5d…`, the attestation manifest's config | `sha256:51da2f5da4fce6048f4196528ecdab5f74ea2ec53a1c6777c42c3595a1168a27` |

What the recording shows:

- The index lists six platform images (`linux/amd64`, `linux/arm/v7`, `linux/arm64/v8`, `linux/ppc64le`, `linux/riscv64`,
  `linux/s390x`), each followed by its attestation manifest with the platform `unknown/unknown`.
- The attestation manifest has no `subject` and no `artifactType`. Its config is of the media type
  `application/vnd.oci.image.config.v1+json` (an image's), with `architecture` and `os` `unknown`, so a selection by media types
  alone takes it for an image.
- Its one layer is of the media type `application/vnd.in-toto+json`, annotation `in-toto.io/predicate-type:
  https://spdx.dev/Document`, 3,770,413 bytes: an SPDX document in an in-toto statement, not a tar stream. The layer is not kept
  here: its media type, in the manifest, is what the recognition reads, and its bytes are what an unpack refuses with
  `layer_format`.
