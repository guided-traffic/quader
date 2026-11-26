package formats

import "context"

// Deprecation is a version's or a package's deprecation as its format records it (R-API-085): what the publisher said is no longer
// to be used, and why. The screens show it the same way in every format that has one; the fields a format does not carry are empty.
type Deprecation struct {
	// Package says that the deprecation is of the whole package (an Ansible collection), so every version of it is deprecated; false
	// for a deprecation of the one version (npm, NuGet).
	Package bool
	// Message is the publisher's text, plain text and never markup; "" for none.
	Message string
	// Reasons are the format's reasons (NuGet: Legacy, CriticalBugs, Other), in the order given; nil for a format without reasons.
	Reasons []string
	// Alternate names the package to use instead (NuGet's alternatePackage); nil for none.
	Alternate *DeprecationAlternate
}

// DeprecationAlternate is the package a deprecation names in its place: its name as the format spells it and the range of its
// versions ("" for any; NuGet writes "*").
type DeprecationAlternate struct {
	Package string
	Range   string
}

// MaxDeprecationMessage bounds the message of a deprecation, in characters (R-API-085): npm's limit (R-NPM-018), applied to every
// format that carries a message.
const MaxDeprecationMessage = 1024

// DeprecationSubject says what a format deprecates (R-API-085).
type DeprecationSubject string

const (
	// DeprecateVersion: one version at a time (npm, NuGet).
	DeprecateVersion DeprecationSubject = "version"
	// DeprecatePackage: the whole package, every version of it (an Ansible collection).
	DeprecatePackage DeprecationSubject = "package"
)

// DeprecationFinder is the optional interface of a format whose versions can be deprecated (R-API-085): the package list's filter
// "deprecated" asks it for the stored versions that are, as the view's search asks [ViewAdapter.Find]. [CapabilitiesOf] finds it.
type DeprecationFinder interface {
	// DeprecatedVersions returns the ids of at most limit+1 stored versions in the repositories (by key) that are deprecated, by their
	// own deprecation or by their package's. It does not know what a caller may read: the core intersects the ids with the versions
	// the caller may read before it decides anything.
	DeprecatedVersions(ctx context.Context, ports DataPorts, repos []string, limit int) ([]VersionID, error)
}

// Deprecator is the optional interface of a format whose protocol carries a deprecation that Quader sets in a local repository
// (R-API-085): the management operation hands it a request it built for the repository and the caller, with the format host as Host,
// and the format decides the rest as a protocol write of its own does: the subject, what its protocol can carry, the permission
// (deploy on the package, and the format's ownership rule), the lock, the rows, the package's revision and the audit event, in one
// transaction ([Host.Write]). [CapabilitiesOf] finds it.
type Deprecator interface {
	// DeprecationSubject is what the format deprecates.
	DeprecationSubject() DeprecationSubject
	// Deprecate sets the deprecation of the package or the version a request names, or clears it when d.Deprecation is nil. It returns
	// the deprecation the subject holds afterwards (nil when none), and changed false when the request changed nothing. A request the
	// format's protocol cannot carry (a version for a format that deprecates packages, reasons for one without them) is refused with
	// an error of kind Unprocessable (422) that says why; a package or a version the repository does not hold, or that the caller may not read,
	// is errs.NotFound.
	Deprecate(ctx context.Context, rq *Request, d DeprecationChange) (held *Deprecation, changed bool, err error)
}

// DeprecationChange is one change of a deprecation: the package as the caller named it (the format normalizes it), the version (""
// for a format that deprecates packages) and the deprecation to hold afterwards, nil to clear it.
type DeprecationChange struct {
	Package     string
	Version     string
	Deprecation *Deprecation
}
