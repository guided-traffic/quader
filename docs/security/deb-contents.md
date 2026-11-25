# Debian package contents: the reading of a data archive

Status: built (W-30, package G8; requirement [R-DEB-039](../spec/formats/debian.md#r-deb-039--a-package-files-changelog-copyright-and-file-list-are-read-from-its-data-archive-under-bounds),
decisions [Q-DEB-140](../spec/formats/debian.md#q-deb-140--the-data-archive-is-read-after-the-file-is-stored-by-a-job-under-bounds-of-its-own)
to Q-DEB-145).

This page describes what the data archive of a Debian package file (`data.tar.*` inside the `.deb`) can make Quader do and show. Until
this reader existed Quader never decompressed it (R-DEB-007); it now reads it once per stored file to show the changelog, the copyright
file and the list of files a package installs. Two parties write the archive and neither is trusted: an **uploader** who holds `deploy`
on a local repository, and the **upstream** of a remote repository (its package files are checked against the digest of a parsed index
before they are stored, R-DEB-022, which proves where they came from, not what is inside). The reader of the result is any caller who
may read the package, and for the changelog and the copyright any caller who may also read the repository's wiki. The control
archive's bounds, which the upload applies, are on [deb-index.md](deb-index.md) and in R-DEB-034. The code is
[debfile/contents.go](../../internal/formats/deb/debfile/contents.go) (the reader),
[xzblocks.go](../../internal/formats/untrusted/xzblocks.go) and `NewZstdReaderWindow` in
[zstd.go](../../internal/formats/untrusted/zstd.go) (the bounded decompressors), [contents.go](../../internal/formatdata/deb/contents.go)
(the job, the rows, the task's port) and [deb_contents.go](../../internal/api/v1/deb_contents.go) (the operation).

## Where it runs

| Step | What is decided | Where | Result |
|---|---|---|---|
| 1 | Nothing of the data archive is read in an upload's request or in a remote's fill. The upload's transaction queues the job `deb-contents`; a cached file's job is queued by `deb-remote-describe` after it wrote the file's row | `RecordUpload`, `DescribeCached` | An archive the reader refuses never fails an upload or a fill; the file is stored, indexed and served |
| 2 | The job runs in the queue `default` (not `index`, whose builds a long read would hold back), one package of one repository at a time, coalesced, with no transaction open while it reads | `ReadContents` | A read costs a worker slot, not a database connection |
| 3 | The stored file is read with ranged reads of 4 MiB from the blob store; the bytes are not checked against the file's digest (ranged reads cannot be, R-STOR-013) | `debBlobs` | See H-2136 |
| 4 | An error of the blob store is tried again (5 attempts); only the archive's own faults become a refusal | `debfile.ReadContents` (`source`) | A store that fails for a minute does not turn a package's contents into a permanent refusal |

## What the reader does with the archive

| Step | What is decided | Bound | On crossing it |
|---|---|---|---|
| 1 | The compression: gzip, xz, Zstandard or none; bzip2 and lzma are not read | — | refused, `unsupported_compression` |
| 2 | xz is parsed by Quader block by block: a block header's LZMA2 dictionary is read before the decoder is made, and only a lone LZMA2 filter is accepted. The xz module would otherwise allocate the dictionary a header declares, up to 4 GiB, before decoding a byte | 64 MiB per block | refused, `too_large` |
| 3 | Zstandard's window | 64 MiB per frame | refused, `too_large` |
| 4 | Every byte the decompressor hands out counts, the content of skipped entries included | 2 GiB | refused, `too_large` |
| 5 | Every tar header counts | 200,000 entries | refused, `too_many_entries` |
| 6 | The whole read of one file | 2 minutes | refused, `timeout` |
| 7 | No entry is written anywhere and no name builds a path: a name is a label. A symbolic link, a hard link, a device and a pipe are names in the list; a link's target is kept as text and never resolved, opened or followed | — | — |
| 8 | Only two entries' content is read: `usr/share/doc/<package>/changelog.Debian.gz` (or `changelog.gz`), inflated as gzip only until 64 KiB of it are kept, and `usr/share/doc/<package>/copyright`, its first 64 KiB. A gzip bomb as the changelog costs the 64 KiB it keeps | 64 KiB each | the text is cut and marked |
| 9 | The file list keeps path, size, kind and a link's target | 10,000 entries, 1 MiB of names | the list is cut and its count kept |
| 10 | What is kept is made valid UTF-8 without control characters other than tab and line feed (PostgreSQL's text holds no NUL; a screen shows no escape sequence) | — | replacement characters ([H-2133](../spec/formats/debian.md#h-2133--a-changelog-or-copyright-file-that-is-not-utf-8-shows-replacement-characters)) |
| 11 | A crossed bound refuses the whole archive: nothing of what was read before is kept, so a bomb's first entries never look like a package's list | — | the row says why |

Memory: what is kept (at most 64 KiB, 64 KiB and about 1.5 MiB of list), the decoder's dictionary or window (64 MiB), and the 4 MiB
read buffer, per running job. The bombs of the unit tests are files of kilobytes that inflate past a lowered cap.

## What reaches a browser

| Text | Path | Control |
|---|---|---|
| The changelog, the copyright | the operation `GET …/versions/{version}/contents`, members `changelog` and `copyright` | Plain text in JSON. The screen binds it as text in a `<pre>`: no Markdown, no HTML, no address made a link |
| A file's path, a link's target | the same operation, `files.items` | Text |
| The licences | the copyright's `License` lines, also the version's `licenses` | Text, at most 32 of at most 200 bytes |
| The reason of a refusal | `reason`, `detail` | Only for a caller who reads the repository's configuration (`repositories:read-config`, or an Owner of the project); the sentence is Quader's, with numbers and the compression's name, never the archive's text |

## Who may read it

- The operation answers a version only to a caller who may read it (the package view's scope, R-API-029), and a version of another format
  or one that is hidden is `404`.
- The changelog and the copyright are wiki content (R-AUTHZ-034, Q-DEB-143): a caller who may read the artifacts and not the wiki of the
  repository gets the state, the licences and the file list without them. The wiki of the repository the contents are read from decides.
- The file list and the licences are facts of the artifact, like its size and its control fields.

## What this does not cover

### H-2136 · What the screens show of a package is read from the stored bytes without a digest check

The reader takes the stored file with ranged reads, which are not verified against the file's SHA-256 (R-STOR-013). A party that can write
the object store can change what the screens show of a package's changelog, copyright and files, as it can change the package itself;
clients still verify the package against the signed index and refuse it. Live for every repository; real for a holder of the runtime
object-store credential.

### H-2137 · An uploader can keep the default queue busy with large package files

Each package file costs one job slot of the queue `default` for up to two minutes (the bound of the read), and a worker runs four such
slots by default (`jobs.workers.default`). An uploader who holds `deploy` and sends many large packages, or a remote whose clients fetch
many, delays the other jobs of that queue by as much; nothing caps the reads per repository. Live; the bound per file holds, the sum
does not.
