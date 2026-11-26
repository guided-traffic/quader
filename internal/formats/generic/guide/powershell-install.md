The snippet downloads a file into the current location under the name `-OutFile` gives it, usually the last segment of its path; the page of a file in Quader offers the command with the address and the name filled in, behind **Download**. When Quader answers with an error status, `Invoke-WebRequest` stops with an error. Windows PowerShell 5.1 draws a progress bar that slows a large download down; `$ProgressPreference = 'SilentlyContinue'` before the command switches it off. Quader serves a file with the media type it was uploaded with; a file uploaded without one has the type its name says for Markdown, plain text and logs, JSON, YAML, TOML and CSV, and any other file `application/octet-stream`.

### Checking a download

Every answer that carries a file names its SHA-256 as the `ETag`, in hexadecimal and quoted. The second snippet checks a download against it: `Get-FileHash` computes the SHA-256 of the copy and the comparison prints `True` when it matches, `False` when it does not. These digests are Quader's own record of the stored bytes; to know that the bytes are the ones their publisher meant, compare them with a digest the publisher gave you.

Quader checks a file against its digest while it serves the whole file. When the stored object fails that check, the transfer breaks off before its end, so a complete download is never a corrupted one; download again.

### Resuming

In PowerShell 7, `-Resume` beside `-OutFile` resumes a broken download where your copy ends: Quader answers the byte range it asks for (`206`), on every replica. A resumed copy joins two transfers that Quader does not check against the digest, so check it before you use it.

### Listing a folder

`(Invoke-WebRequest -Uri "{{.RegistryURL}}<folder>/" -Headers $QuaderHeaders).Content | ConvertFrom-Json` reads the listing of a folder: `items` holds the entries you may read, each with its `path` relative to the folder, its `kind` and `modifiedAt`, a file also with `size`, `sha256` and `sha1`.
