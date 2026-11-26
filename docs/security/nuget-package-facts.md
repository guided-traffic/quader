# NuGet package facts: what a package's names, icon and licence file can make the screens show

Status: built (W-30, package G6; requirements [R-NUGET-038](../spec/formats/nuget.md#r-nuget-038--the-icon-the-licence-file-the-frameworks-the-symbols-the-revision-and-the-release-notes-on-a-versions-page)
and [R-NUGET-039](../spec/formats/nuget.md#r-nuget-039--what-a-build-a-compile-or-an-install-runs-of-a-package-is-said-on-its-page),
decisions [Q-NUGET-157](../spec/formats/nuget.md#q-nuget-157--the-icon-and-the-licence-file-on-the-screen-come-from-two-management-operations-under-the-read-check)
to Q-NUGET-159).

This page describes what a `.nupkg` can make a NuGet version's page show beyond its `.nuspec`: the frameworks and the files that run code,
read from the archive's file list, and the icon and the licence file, read out of the stored package. Two parties write the package and
neither is trusted: an **uploader** who holds `deploy` on a local repository, and the **upstream** of a remote repository. The reader is any
caller who may read the version. The bounds of the archive itself are R-NUGET-005's, on [nuget-publish.md](nuget-publish.md). The code is
[nupkg/contents.go](../../internal/formats/nuget/nupkg/contents.go) (the names), [contents.go](../../internal/formatdata/nuget/contents.go)
(the job of a version stored before), `ViewEmbedded` of [view.go](../../internal/formats/nuget/view.go) and
[package_embedded_api.go](../../internal/api/v1/package_embedded_api.go) (the two operations).

## The file list

| Step | What is decided | Where | Result |
|---|---|---|---|
| 1 | Only the names of the central directory are read, the ones `nupkg.Open` checked: no absolute name, no drive, no `..` segment, no control character, at most 65,535 entries and 4,096 bytes a name; no entry is inflated and nothing is written to disk | `nupkg.ContentsOf` | A name is a label |
| 2 | Each list (frameworks, build logic, analyzers, scripts) keeps 20 names; a name over 256 bytes is counted, not kept; a framework is kept only in the shape of a framework folder | `keep`, `tfmShape` | A package of a million names stores 80 and a count |
| 3 | A version stored before is read by the job `nuget-contents` in the queue `default`, one version a job, coalesced, with ranged reads of the end record and the central directory and no transaction open while it reads | `ReadContents` | A read costs a worker slot and two ranged reads |
| 4 | The screens show the names as text in the code font; nothing builds a link or a path from them | `package-version.html` | A name that looks like markup is shown as written |

## The icon and the licence file

| Step | What is decided | Where | Result |
|---|---|---|---|
| 1 | The read check: the version must be one the caller may read (the scope of the package screens); a version it may not read answers the 404 of a version without the file | `embedded`, `EmbeddedOf` | No word that a hidden version exists |
| 2 | The file is the one the `.nuspec` names, read out of the stored `.nupkg` through the reader of R-NUGET-024, at most 1 MiB | `ViewEmbedded`, `EmbeddedFile` | — |
| 3 | An icon is served only as `image/png` or `image/jpeg`, decided from its first bytes, never from its name, the package or the `.nuspec`; an SVG and every other type is the 404 | `iconType` | No script in an SVG reaches a browser |
| 4 | A licence is served only when it is UTF-8 without a NUL, as `text/plain; charset=utf-8` | `licenseType` | It is never rendered as markup |
| 5 | Every answer has `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'; sandbox`, `Content-Disposition: inline`, `Cross-Origin-Resource-Policy: same-origin` | `embedded` | A browser shows a picture or text and nothing else |
| 6 | `Cache-Control: private, no-cache` and a strong `ETag` of the bytes: a shared cache keeps nothing, and the browser asks again each time, so the read check decides every view; a `304` comes after the check | `embedded` | One reader's permission never serves another |
| 7 | No address the package names (`iconUrl`, a licence URL) is fetched by Quader or by the page | `packageIconUrl` | No request leaves the reader's browser to a host a package names |

## What this does not cover

### H-2230 · The icon and the licence file on the screen are read without the checks of a download

The flat container's routes of the same files decide the checks of a download of the `.nupkg` (the malware gate, the vulnerability block,
the signature gate, Q-NUGET-136) before they serve a byte; the management operations `…/icon` and `…/license`, like the README of the same
page (Q-NUGET-48), check only that the reader may read the version. A version whose package the malware gate refuses still shows its icon
(a PNG or a JPEG by its bytes, with `nosniff` and a sandboxing CSP) and its licence text on its page. Live for every NuGet version the gates
hold back; real only for a flaw of the browser's PNG or JPEG decoder, or for a licence text the gate meant to withhold.
