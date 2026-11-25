A remote repository is a pull-through cache of an APT archive{{if .Repository}}: `{{.Repository}}` serves the archive of its upstream address{{end}}. Debian, Ubuntu and every archive in the Debian repository format work. The first request for a file fetches it from the upstream, and every request after that is served from the cache.

### What is passed through

The release documents (`InRelease`, `Release`, `Release.gpg`) are the upstream's, byte for byte. Quader signs nothing of a remote, so the signatures your client checks are the archive's own. The index files are served only while they are the files the stored release lists: a client never reads an index that no release names. By-hash files are fetched once and kept as they are.

### Package files

A package file is fetched on the first request and kept. When the index of the archive lists its digest, the file is checked against it before it is stored, and a file that differs is refused with `502 upstream_integrity_mismatch`. A file the index does not list is stored without a check unless the repository's checksum policy is `require`, which refuses it.

### Suites and components

Nothing has to be configured for them. Whatever suite the source line of the client names is fetched from the upstream: `stable`, `bookworm`, `noble-updates`, a flat directory such as `binary/`, or `./` for a repository whose address is the flat directory itself.

### Delivery rules: use a virtual repository

The release and the indexes of a remote are the upstream's, so APT lists every version the upstream has, also the ones the repository's delivery rules refuse: in the mode Only what is allowed, a version a rule blocks, and a version outside the range of versions a package is allowed with. APT installs the highest version it lists, and the remote refuses its file with `403 Forbidden`; APT shows the status line only. `apt install <package>=<version>` with a version the rules deliver works. A virtual repository over the remote leaves out the rows its member's rules refuse, so APT picks the highest version that is delivered: point the client at the virtual repository when this remote has delivery rules.
