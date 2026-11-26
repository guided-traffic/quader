package formats

// Client is a native client of a format and the majors tested against it
// (FMT-002, FMT-003).
type Client struct {
	// ID is "npm", "pnpm", "yarn", "yarn-classic", "dotnet", "nuget".
	ID          string
	DisplayName string
	// Versions are the tested majors (R-NPM-045).
	Versions []string
}

// SetupTemplate is one client setup snippet (api-and-ui.md R-API-030).
type SetupTemplate struct {
	// ID is stable: "npm.npmrc.configure".
	ID     string
	Client string
	// Section is "configure", "publish" or "install".
	Section string
	// Lang is "shell" (a POSIX shell), "powershell", "cmd", "ini", "xml" or "yaml".
	// It fixes how the API quotes the package and the version it substitutes into an
	// install template.
	Lang string
	// File is the target file when the snippet is file content: ".npmrc".
	File string
	// Template is text/template over [SetupData]; it never receives a token
	// (R-API-030). The functions shell, powershell, cmd and xml quote a value for a
	// command of that shell and for an XML text or attribute (R-API-030); a value of
	// SetupData that a template places in such a context goes through them. The
	// function schemeless drops the scheme of a URL ("https://host/p/" becomes
	// "//host/p/"): the form in which npm and pnpm bind a credential to a registry
	// (R-NPM-041), so a template derives that key from RegistryURL and the core
	// carries no field for it. The function hostname gives a URL's host name alone, and trimslash drops one trailing slash, for a
	// client that joins the base and a file name with a slash of its own (freshclam's PrivateMirror, R-CLAMAV-006). The function
	// registryhost gives the host and port an image reference starts with, the part of ImagePrefix before its first slash, for a
	// client that signs in to a registry host and not to a path (Helm's `helm registry login`, formats/oci.md R-OCI-042).
	//
	// The credential is one of the literals [SetupTokenRef], [SetupTokenRefWin] and
	// [SetupTokenRefPS], the one the syntax of the file or shell the template writes
	// expands, and the placeholder of the package to install is [SetupPackageRef].
	// The API rewrites them after the template ran, by the credential mode of the
	// request and by the package asked for, so that no template implements the three
	// modes or the quoting.
	Template string
	// VersionSpec is how an install template attaches the version to the package when
	// the request names one: the text that replaces [SetupPackageRef] after the quoted
	// package, with [SetupVersionRef] standing for the quoted version. "" is
	// "@<version>" (npm: pkg@1.2.3); dotnet writes " --version <version>". A request
	// that names no version leaves the package alone. VersionSpec is meaningful for an
	// install template only (kit check 8).
	VersionSpec string
	// PackageExample is what an install template shows in place of
	// [SetupPackageRef] when the request names no package: an example the reader
	// replaces, the same one the format's other snippets write ("myapp:1.0" for an
	// image). "" keeps the placeholder. Meaningful for an install template that
	// places the package only (kit check 8).
	PackageExample string
	// Packages is the prefix of the normalized names an install template is for ("role/" for an Ansible role,
	// ansible.md R-ANS-056), "" for every package. For a package a prefixed install template of the client claims,
	// the client's install templates without a prefix are left out and the package is written by its display name,
	// the name the client knows it by, or by the template's PackageExample when it has none; for any other package, and
	// when the request names none, the prefixed ones are left out.
	Packages string
	// Examples are the literal values of the template that the reader replaces with
	// their own: a file to publish, an image name, a path. The API names each one
	// that stands in a rendered snippet, so that a screen can mark it (R-API-030);
	// every example stands in the template's text (kit check 8).
	Examples []string
}

// SetupTokenRef is how a setup template spells the reference to the token, exactly:
// the environment variable the client expands itself. The API replaces it by the
// reference of the configured variable (credentials=env), by the placeholder the
// browser fills in (credentials=placeholder), or drops the line that holds it
// (credentials=none).
const SetupTokenRef = "${QUADER_TOKEN}" //nolint:gosec // a reference to an environment variable, not a credential

// SetupTokenRefWin is the reference to the token where the syntax is that of Windows:
// a NuGet.Config value, which NuGet expands itself, and a cmd command. The API
// rewrites it like [SetupTokenRef], to "%<variable>%" for credentials=env.
const SetupTokenRefWin = "%QUADER_TOKEN%" //nolint:gosec // a reference to an environment variable, not a credential

// SetupTokenRefPS is the reference to the token in a PowerShell command. The API
// rewrites it like [SetupTokenRef], to "$env:<variable>" for credentials=env.
const SetupTokenRefPS = "$env:QUADER_TOKEN" //nolint:gosec // a reference to an environment variable, not a credential

// SetupTokenRefs are all the spellings of the token reference, in the order the API
// looks for them. A template spells exactly one per credential line.
func SetupTokenRefs() []string {
	return []string{SetupTokenRef, SetupTokenRefWin, SetupTokenRefPS}
}

// SetupVersionRef stands for the version in [SetupTemplate.VersionSpec].
const SetupVersionRef = "<version>"

// SetupPackageRef is how an install template spells the package to install. The
// API replaces it by the quoted name and version when the request names a package,
// and leaves it in place otherwise.
const SetupPackageRef = "<package>"

// SetupPathRef is how an install or publish template of a format whose items are paths (the generic format,
// [SubjectPath]) spells the path of a file below RegistryURL, inside a double-quoted address. When the request names a
// path (formats/generic.md R-GENERIC-017), the API replaces it by the path with every byte outside RFC 3986's unreserved
// characters percent-encoded, segment by segment, so that nothing of a file's name is read by the shell; for a folder by
// the folder's path, a slash and [SetupFileRef]. Without a path it stays in place for the reader.
const SetupPathRef = "<path>"

// SetupFileNameRef is how an install template spells the local name a download is saved under. With a path it becomes the
// path's last segment, quoted as one argument of the template's language, and with "./" before it when it starts with "-".
const SetupFileNameRef = "<name>"

// SetupFileRef is how a publish template spells the local file to upload; it always stays the reader's.
const SetupFileRef = "<file>"

// SetupSHA256Ref is how an install template spells the hexadecimal SHA-256 a download is checked against; the API
// replaces it by the SHA-256 a request names with its path.
const SetupSHA256Ref = "<sha256>"

// SetupData is the only data a setup template sees. It has no credential field,
// so a template that names one fails to execute (kit check 8).
type SetupData struct {
	// RegistryURL is the registry URL of the repository, with its trailing slash.
	RegistryURL string
	// ImagePrefix is "<host>/<projectKey>/<repoKey>" for the format that declares the registry root (the form an
	// image reference starts with, which a template cannot derive from RegistryURL because the address has the
	// "/v2" prefix, R-OCI-030); "" for every other format.
	ImagePrefix string
	Repository  string
	// Project and Key are the project key and the repository key of Repository ("web/apt" is Project "web", Key "apt"): a template that
	// derives a file name from the repository (the Debian format's keyring and source files, R-DEB-029) cannot cut Repository, which is
	// the qualified name. Both are "" in a guide that names no repository (Q-ARCH-420).
	Project  string
	Key      string
	Endpoint string
	// Variant names the variant of a configure snippet the request asked for, in the
	// words of the format: for npm the scope without its "@" ("acme"). "" is the
	// default variant, the one that binds the whole client to the registry.
	Variant  string
	Username string
	// RequiresAuth is true when the repository is not anonymously readable: a
	// snippet then makes the client send its credential without waiting for a
	// challenge (always-auth, npmAlwaysAuth).
	RequiresAuth bool
	// Bastion is true when the snippets are for a quader Bastion (R-BAS-007): a read-only
	// cache in front of the installation, which a lockfile written against another host
	// must resolve through. A configure template may add what the client needs for that.
	Bastion bool
}
