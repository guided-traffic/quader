package rows

// Metadata is the JSON object stored in nuget_version.metadata (R-NUGET-005, the table
// "Metadata extracted from the .nuspec"): what the ingestion extracted of the .nuspec and
// what the registration documents, the search results and the screens render from. The
// ingestion writes it with [Metadata]'s tags, the readers decode it with the same type, so
// the key names exist once. A member that the .nuspec leaves out is absent.
//
// Text values are trimmed and otherwise verbatim; ranges and target frameworks are the
// strings of the document (R-NUGET-009 renders them as they are).
type Metadata struct {
	Title        string `json:"title,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Description  string `json:"description,omitempty"`
	ReleaseNotes string `json:"releaseNotes,omitempty"`
	Copyright    string `json:"copyright,omitempty"`
	Language     string `json:"language,omitempty"`
	// Authors is kept as one string.
	Authors string `json:"authors,omitempty"`
	// Tags are the tags element split on white space.
	Tags       []string `json:"tags,omitempty"`
	ProjectURL string   `json:"projectUrl,omitempty"`
	IconURL    string   `json:"iconUrl,omitempty"`
	LicenseURL string   `json:"licenseUrl,omitempty"`
	License    *License `json:"license,omitempty"`
	// Icon and Readme are the paths of embedded files, as the .nuspec spells them.
	Icon                     string `json:"icon,omitempty"`
	Readme                   string `json:"readme,omitempty"`
	RequireLicenseAcceptance bool   `json:"requireLicenseAcceptance,omitempty"`
	DevelopmentDependency    bool   `json:"developmentDependency,omitempty"`
	Serviceable              bool   `json:"serviceable,omitempty"`
	// MinClientVersion is the attribute of the metadata element, verbatim.
	MinClientVersion string      `json:"minClientVersion,omitempty"`
	Repository       *Repository `json:"repository,omitempty"`
	// DependencyGroups are the groups in document order; a flat dependency list is one
	// group without a target framework.
	DependencyGroups []DependencyGroup `json:"dependencyGroups,omitempty"`
	// PackageTypes are the declared types; none declared is absent, and the package then
	// has the type Dependency (nuget_version.package_types holds it lowercased).
	PackageTypes []PackageType `json:"packageTypes,omitempty"`
	// Deprecation is not of the .nuspec: the deprecation of the version, set through the management operation for a local version
	// and taken from the upstream's registration for a remote's cached one (R-NUGET-036), in the registration's shape.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Contents is not of the .nuspec either: what the file list of the package says (R-NUGET-038, R-NUGET-039), read from the names
	// of its central directory at ingestion and at a remote's fill. Absent for a version stored before the reader existed, which the
	// task remote-describe queues for nuget-contents.
	Contents *Contents `json:"contents,omitempty"`
}

// Contents is what the file list of a package says: the target frameworks its folders name (lower-cased, as NuGet spells a folder),
// and the files that run code when a project uses it, by their package paths in archive order. Each list holds at most 20 names; More
// counts the names past a bound.
type Contents struct {
	Frameworks []string `json:"frameworks"`
	// Build are the .props and .targets files MSBuild imports into every build of a project that references the package.
	Build []string `json:"build"`
	// Analyzers are the assemblies below analyzers/: analyzers and source generators the compiler loads.
	Analyzers []string `json:"analyzers"`
	// Scripts are the PowerShell scripts Visual Studio runs for a packages.config project (tools/init.ps1, install.ps1, uninstall.ps1).
	Scripts []string `json:"scripts"`
	More    int      `json:"more,omitempty"`
	// Unreadable says that the reader of a version stored before it refused the stored package within its bounds of today: the
	// lists say nothing, and the screens show none of them.
	Unreadable bool `json:"unreadable,omitempty"`
}

// Deprecation is a version's deprecation in the shape of the registration's catalog entry (R-NUGET-036): at least one reason, an
// optional message and an optional alternate package, whose range is "*" when any version will do.
type Deprecation struct {
	Reasons          []string          `json:"reasons"`
	Message          string            `json:"message,omitempty"`
	AlternatePackage *AlternatePackage `json:"alternatePackage,omitempty"`
}

// AlternatePackage is the package a deprecation names in its place.
type AlternatePackage struct {
	ID    string `json:"id"`
	Range string `json:"range"`
}

// License is the license element of the .nuspec.
type License struct {
	// Type is "expression" or "file".
	Type    string `json:"type"`
	Version string `json:"version,omitempty"`
	Value   string `json:"value"`
}

// Repository is the repository element of the .nuspec.
type Repository struct {
	Type   string `json:"type,omitempty"`
	URL    string `json:"url,omitempty"`
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// DependencyGroup is a group of dependencies for one target framework.
type DependencyGroup struct {
	// TargetFramework is the attribute verbatim, absent for a group without one.
	TargetFramework string       `json:"targetFramework,omitempty"`
	Dependencies    []Dependency `json:"dependencies,omitempty"`
}

// Dependency is one dependency of a group.
type Dependency struct {
	ID string `json:"id"`
	// Version is the range attribute verbatim, absent when the .nuspec has none.
	Version string `json:"version,omitempty"`
	Include string `json:"include,omitempty"`
	Exclude string `json:"exclude,omitempty"`
}

// PackageType is a declared package type.
type PackageType struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}
