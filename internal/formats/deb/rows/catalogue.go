package rows

// The values of the upstream catalogue's part of the data port (remote-curation.md R-CUR-007, formats/debian.md R-DEB-036): what a
// Debian remote's parsed indexes answer to a search, a level and a lookup. Quader searches the indexes it read, as apt search does
// (Q-CUR-16), so the catalogue reads rows and no file.

// CatalogueSuite is a release directory the remote holds a release of, with the binary indexes it has parsed below it.
type CatalogueSuite struct {
	// Dir is the release directory ("dists/trixie").
	Dir string
	// Indexes are the parsed binary indexes relative to Dir ("main/binary-amd64/Packages"), in byte order.
	Indexes []string
}

// CatalogueQuery asks the parsed binary indexes for names.
type CatalogueQuery struct {
	RepoID int64
	// Patterns are the query's words as ILIKE patterns with their wildcards escaped by "\" (CataloguePattern); every one must match.
	// None lists every name.
	Patterns []string
	// NamesOnly matches the name and Provides only (apt-cache --names-only).
	NamesOnly bool
	// Dir and IndexPath restrict the names to one parsed index; "" for every one.
	Dir, IndexPath string
	// After is the last name of the previous page, "" for the first.
	After string
	Limit int
}

// CatalogueHit is a name that a query matched: the versions its rows list and the short description of one of them.
type CatalogueHit struct {
	Name        string
	Versions    []string
	Description string
}
