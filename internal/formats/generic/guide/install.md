The snippet downloads a file into the current directory under the name `--output <name>` gives it, usually the last segment of its path; the page of a file in Quader offers the command with the name and the address filled in, behind **Download**. `--fail` makes curl exit with an error when Quader answers with an error status, instead of saving the error answer as the file, and `--location` follows the redirect that a folder's address without its trailing slash gets. Quader serves a file with its size (`Content-Length`) and with the media type it was uploaded with; a file uploaded without one has the type its name says for Markdown, plain text and logs, JSON, YAML, TOML and CSV, and any other file `application/octet-stream`.

### Checking a download

Every answer that carries a file names its SHA-256 twice: as the `ETag`, in hexadecimal and quoted, and as `Repr-Digest: sha-256=:<base64>:`. With `--header "Want-Repr-Digest: sha-512=1"` the `Repr-Digest` carries the SHA-512 as well. `quader_curl --fail --head "{{.RegistryURL}}<path>"` shows them without downloading, and the hexadecimal SHA-256 is what `sha256sum <file>` (`shasum -a 256 <file>` on macOS) prints for a complete download. The second snippet checks a download against it: `sha256sum -c -` reads the digest and the name, and prints `OK` or fails with `FAILED`. These digests are Quader's own record of the stored bytes; to know that the bytes are the ones their publisher meant, compare them with a digest the publisher gave you.

Quader checks a file against its digest while it serves the whole file. When the stored object fails that check, the transfer breaks off before its end, so a complete download is never a corrupted one; download again.

### Downloading only what changed

`quader_curl --fail --remote-time --time-cond <file> --output <file> "{{.RegistryURL}}<path>"` sends the modification time of your copy (`If-Modified-Since`): while the file has not changed since, Quader answers `304` and curl leaves the copy alone. `--remote-time` gives the copy the modification time Quader reports, so the first download sets it right. Quader answers `If-None-Match` with the `ETag` by `304` as well, and a download whose `If-Match` or `If-Unmodified-Since` does not hold with `412`.

### Resuming and ranges

Quader answers a single byte range (`Range`) with `206`, on every replica. `quader_curl --fail --continue-at - --output <file> "{{.RegistryURL}}<path>"` resumes a broken download where your copy ends; for a copy that is complete already Quader answers `416`, and curl keeps the copy as it is. A resumed copy joins two transfers that Quader does not check against the digest, so compare it with the `ETag` before you use it. `--range 0-99` fetches the first 100 bytes; a request for several ranges gets the whole file.

### Listing a folder

`quader_curl --fail "{{.RegistryURL}}<folder>/"` lists a folder as JSON: `items` holds the entries you may read, each with its `path` relative to the folder, its `kind` and `modifiedAt`, a file also with `size`, `sha256` and `sha1`. A page holds 1,000 entries unless you ask for another number with `?limit=` (at most 10,000), and `nextCursor` is the `?cursor=` of the next page. Without the trailing slash Quader redirects (`301`) to the address with it, which curl follows only with `--location`. A browser gets the listing as a page with links.
