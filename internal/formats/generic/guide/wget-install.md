The snippet downloads a file into the current directory under the name `--output-document <name>` gives it, usually the last segment of its path; the page of a file in Quader offers the command with the name and the address filled in, behind **Download**. When Quader answers with an error status, wget exits with status 8 and leaves the file it opened empty: delete it before you try again. Quader serves a file with the media type it was uploaded with; a file uploaded without one has the type its name says for Markdown, plain text and logs, JSON, YAML, TOML and CSV, and any other file `application/octet-stream`.

### Checking a download

Every answer that carries a file names its SHA-256 as the `ETag`, in hexadecimal and quoted. The second snippet checks a download against it: `sha256sum -c -` reads the digest and the name, and prints `OK` or fails with `FAILED` (`shasum -a 256 -c` on macOS). These digests are Quader's own record of the stored bytes; to know that the bytes are the ones their publisher meant, compare them with a digest the publisher gave you.

Quader checks a file against its digest while it serves the whole file. When the stored object fails that check, the transfer breaks off before its end, so a complete download is never a corrupted one; download again.

### Resuming

`quader_wget --continue --output-document <name> "{{.RegistryURL}}<path>"` resumes a broken download where your copy ends: Quader answers the byte range wget asks for (`206`), on every replica. A resumed copy joins two transfers that Quader does not check against the digest, so check it before you use it.

### Listing a folder

`quader_wget --output-document - "{{.RegistryURL}}<folder>/"` prints the listing of a folder as JSON: `items` holds the entries you may read, each with its `path` relative to the folder, its `kind` and `modifiedAt`, a file also with `size`, `sha256` and `sha1`. Without the trailing slash Quader redirects (`301`) to the address with it, which wget follows.
