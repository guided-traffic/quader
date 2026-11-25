// Package wellknown names the built-in project upstream and the repositories the product declares in it (repositories-and-proxy.md
// R-REPO-066 to R-REPO-070, R-REPO-075, authorization.md R-AUTHZ-041, ADR-209, ADR-211, ADR-222): one remote repository per upstream
// preset of the format catalog, and the system repositories of the product's own work, today scanner-trivy (a remote) and
// clamav (a local repository of ClamAV's definitions).
//
// It is a leaf (the format catalog, qname and the standard library), so the repository model, the project service, the
// authorization engine and the edge read one set of names, states and declarations. The reconcile of `quader migrate` brings the
// stored rows in step with [Declared] (R-REPO-067).
package wellknown

import (
	"encoding/json"
	"slices"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/qname"
)

// The built-in project (R-REPO-066). Migration 00086_upstream.sql creates its row with these values; nothing changes them later
// (the guard AI010).
const (
	// ProjectKey is the key of the built-in project, reserved by its row: a create of a project with it answers 409
	// already_exists like any taken key.
	ProjectKey = "upstream"
	// ProjectID is the fixed ID of the project's row.
	ProjectID = "00000000-0000-0000-0000-000000000100"
	// ProjectDisplayName and ProjectDescription are the project's fixed display name and description.
	ProjectDisplayName = "Upstream"
	ProjectDescription = "The public registries this installation offers to every project."
)

// The system repository of the image scanning's database (R-REPO-070, Q-REPO-373, ADR-211).
const (
	// ScannerTrivyKey is the key of the repository in the project upstream.
	ScannerTrivyKey = "scanner-trivy"
	// ScannerTrivyImage is the image name of the analyser's vulnerability database at its registry: the one name the
	// repository's delivery rule allows and the scanning resolves a tag of.
	ScannerTrivyImage = "aquasec/trivy-db"
	// ScannerTrivyURL is the product's upstream of the repository: the registry that serves ScannerTrivyImage.
	ScannerTrivyURL = "https://mirror.gcr.io/"
	// ScannerTrivyDisplayName names the repository on the admin page and is its description (R-UI-187).
	ScannerTrivyDisplayName = "Image scanning database"
)

// ScannerTrivy is the qualified name of the image scanning's database repository: upstream/scanner-trivy.
var ScannerTrivy = qname.Repo{Project: ProjectKey, Key: ScannerTrivyKey}

// The system repository of ClamAV's definitions (R-REPO-075, Q-REPO-413, ADR-225): a local repository of the format clamav that the
// malware scanning's database task writes in process and that serves the company's ClamAV installations while an administrator has
// switched "ClamAV definition updates" on.
const (
	// ClamavKey is the key of the repository in the project upstream.
	ClamavKey = "clamav"
	// ClamavDisplayName names the repository on the admin page and is its description (R-UI-187).
	ClamavDisplayName = "ClamAV definitions"
)

// Clamav is the qualified name of the repository of ClamAV's definitions: upstream/clamav.
var Clamav = qname.Repo{Project: ProjectKey, Key: ClamavKey}

// The literal sub-paths of /api/v1/upstream-repositories (R-REPO-073, R-API-083): siblings of {key}, so no preset takes them.
const (
	PathSwitchOnAll   = "switch-on-all"
	PathSetVisibility = "set-visibility"
)

// ReservedKeys are the keys of the project upstream no preset of the catalog may take beyond the reserved repository keys of
// R-REPO-002: the literal sub-paths of R-REPO-073 and the keys of the system repositories (R-REPO-062, Q-REPO-387). The catalog
// refuses them at registration (catalog.ReservedPresetIDs); TestReservedKeysAreTheCatalogs holds the two lists together.
func ReservedKeys() []string {
	return []string{PathSwitchOnAll, PathSetVisibility, ScannerTrivyKey, ClamavKey}
}

// IsProject reports whether a project key names the built-in project.
func IsProject(key string) bool { return key == ProjectKey }

// Kind is the kind of a well-known repository, the column upstream_repository.kind.
type Kind string

// The kinds (R-REPO-066).
const (
	// KindPreset is a repository of an upstream preset of the catalog: off until an administrator switches it on.
	KindPreset Kind = "preset"
	// KindSystem is a repository of the product's own work: it has no state of its own and no client path.
	KindSystem Kind = "system"
)

// State is the state of a preset repository (R-REPO-068, Q-REPO-375), derived from what is stored; a system repository has none
// (the empty State).
type State string

// The states.
const (
	// StateActive: disabled false and not retired. Served as by any remote repository; takes part as a member.
	StateActive State = "active"
	// StateSwitchedOff: disabled, and switched on at least once before.
	StateSwitchedOff State = "switched-off"
	// StateNotSwitchedOn: disabled, and never switched on.
	StateNotSwitchedOn State = "not-switched-on"
	// StateRetired: the product no longer declares it (R-REPO-067 step 5). A tombstone: disabled, its content purged.
	StateRetired State = "retired"
)

// States lists the states in the order of R-REPO-068.
func States() []State {
	return []State{StateActive, StateSwitchedOff, StateNotSwitchedOn, StateRetired}
}

// StateOf derives the state of a well-known repository from its stored facts (Q-REPO-375): a retired repository is retired
// whatever else holds, an enabled one is active, and a disabled one is switched off when it was switched on before and not
// switched on otherwise. A system repository has no state.
func StateOf(kind Kind, disabled, switchedOnBefore, retired bool) State {
	switch {
	case kind != KindPreset:
		return ""
	case retired:
		return StateRetired
	case !disabled:
		return StateActive
	case switchedOnBefore:
		return StateSwitchedOff
	default:
		return StateNotSwitchedOn
	}
}

// Declaration is one repository the product declares in the project upstream (R-REPO-066, R-REPO-070). The reconcile creates it
// when it is missing and writes the members the product holds when they differ (R-REPO-067, R-REPO-069).
type Declaration struct {
	// Key is the repository key: the preset's id, or the system repository's key.
	Key string
	// Format is the repository's format.
	Format catalog.ID
	// Class is the repository's class (Q-REPO-411): remote for a preset repository, the system repository's own otherwise
	// (scanner-trivy remote, clamav local). A local repository has no address and no account.
	Class catalog.Class
	Kind  Kind
	// DisplayName is the preset's display name, stored as the repository's description ("Docker Hub").
	DisplayName string
	// Default marks the format's default preset (the repository quick setup lists, Q-REPO-385).
	Default bool
	// URL, ExtraHosts and Settings are the product's address (R-REPO-069): remote.url, remote.extraHosts and the value of the
	// format's settings member (deb.upstreamKeys for an APT archive, Q-REPO-379); Settings is nil when the preset carries none.
	URL        string
	ExtraHosts []catalog.PresetHost
	Settings   json.RawMessage
	// Allow are the package names a system repository delivers, each the product's delivery rule `package <name> allow` in mode
	// onlyAllowed (scanner-trivy delivers only its image, R-REPO-070, remote-curation.md R-CUR-002); nil for a preset, whose delivery
	// mode and rules are an administrator's and the holders' of manage (R-REPO-069), and for a local system repository.
	Allow []string
	// Private is true for a repository whose levels are fixed at private (scanner-trivy); false for clamav, whose level the switch
	// of its clients decides (it is created private, Level).
	Private bool
	// Level is the level both parts of the repository take when the reconcile creates it: PresetLevel for a preset repository
	// (the owner's decision Q-REPO-388), private for a system repository. It is never written again after the create.
	Level string
	// ScanningOff is true for a repository whose member vulnerabilities is fixed at {"enabled": false} where the format has
	// the member (the image scanning does not scan its own database, R-REPO-070).
	ScanningOff bool
	// UnusedRetentionHours is remote.cache.unusedRetentionHours fixed by the product, 0 for none: scanner-trivy keeps a layer 48
	// hours after its last use (R-REPO-070, Q-VULN-175). The cleanup that applies it is R-REPO-046's.
	UnusedRetentionHours int
	// Clients is true for the one system repository an administrator may open to clients (clamav, R-REPO-075, Q-REPO-413): its
	// switch "ClamAV definition updates" (upstream_repository.clients_enabled) and its level while the switch is on are the
	// administrator's; while it is off, the level is private and fixed. No other declaration has it.
	Clients bool
}

// ScannerTrivyRetentionHours is the cache period of scanner-trivy (R-REPO-070): two days, so the repository holds the database in
// use and the few published since.
const ScannerTrivyRetentionHours = 48

// PresetLevel is the level a preset repository is created at, switched off (the owner, 2026-11-20, Q-REPO-388): one switch makes
// it readable by every signed-in subject that is not external. An installation that does not let a non-administrator choose it
// gets the highest level below it that it does, and private where there is none (Q-REPO-393).
const PresetLevel = "internal"

// systemLevel is the level of a system repository, fixed (R-REPO-070).
const systemLevel = "private"

// Name returns the declaration's qualified name, upstream/<key>.
func (d Declaration) Name() qname.Repo { return qname.Repo{Project: ProjectKey, Key: d.Key} }

// Declared lists the repositories the product declares for the formats of the catalog (nil is catalog.Default): one preset
// repository per upstream preset of every registered format, in catalog order (formats by ID, presets in the format's order),
// then the system repositories. A format the catalog does not register declares nothing, so scanner-trivy is declared only while
// the format oci is registered and clamav only while the format clamav is.
func Declared(cat *catalog.Registry) []Declaration {
	var out []Declaration
	for _, e := range catalog.Or(cat).List() {
		for _, p := range e.UpstreamPresets {
			out = append(out, Declaration{
				Key: p.ID, Format: e.ID, Class: catalog.Remote, Kind: KindPreset, DisplayName: p.DisplayName, Default: p.Default,
				URL: p.URL, ExtraHosts: slices.Clone(p.ExtraHosts), Settings: cloneRaw(p.Settings), Level: PresetLevel,
			})
		}
	}
	if _, ok := catalog.Or(cat).Lookup(string(scannerTrivyFormat)); ok {
		out = append(out, scannerTrivy())
	}
	if _, ok := catalog.Or(cat).Lookup(string(clamavFormat)); ok {
		out = append(out, clamav())
	}
	return out
}

// scannerTrivyFormat is the format of scanner-trivy: the database is an OCI artifact.
const scannerTrivyFormat catalog.ID = "oci"

// scannerTrivy is the declaration of R-REPO-070.
func scannerTrivy() Declaration {
	return Declaration{
		Key: ScannerTrivyKey, Format: scannerTrivyFormat, Class: catalog.Remote, Kind: KindSystem, DisplayName: ScannerTrivyDisplayName,
		URL: ScannerTrivyURL, ExtraHosts: []catalog.PresetHost{}, Allow: []string{ScannerTrivyImage},
		Private: true, Level: systemLevel, ScanningOff: true, UnusedRetentionHours: ScannerTrivyRetentionHours,
	}
}

// clamavFormat is the format of upstream/clamav (R-CLAMAV-001).
const clamavFormat catalog.ID = "clamav"

// clamav is the declaration of R-REPO-075: a local repository of the format clamav, created private, no address and no account; the
// member malware stays at its default, which the refusals of R-REPO-069 keep (its content is never scanned, R-MAL-001). Its level is
// not a fixed member: it is private while clients are off and the administrator's while they are on (Q-REPO-414).
func clamav() Declaration {
	return Declaration{
		Key: ClamavKey, Format: clamavFormat, Class: catalog.Local, Kind: KindSystem, DisplayName: ClamavDisplayName,
		Level: systemLevel, Clients: true,
	}
}

// Local reports whether the declared repository is a local one: it has no address and no account (R-REPO-075).
func (d Declaration) Local() bool { return d.Class == catalog.Local }

// Fixed returns what the product fixes on the repository beyond its address: empty for a preset repository.
func (d Declaration) Fixed() Fixed {
	return Fixed{Allow: slices.Clone(d.Allow), Private: d.Private, ScanningOff: d.ScanningOff, UnusedRetentionHours: d.UnusedRetentionHours}
}

// Address returns the product's address of the repository (R-REPO-069).
func (d Declaration) Address() Address {
	return Address{URL: d.URL, ExtraHosts: slices.Clone(d.ExtraHosts), Settings: cloneRaw(d.Settings)}
}

// Lookup returns the declaration of a key among decls.
func Lookup(decls []Declaration, key string) (Declaration, bool) {
	i := slices.IndexFunc(decls, func(d Declaration) bool { return d.Key == key })
	if i < 0 {
		return Declaration{}, false
	}
	return decls[i], true
}

func cloneRaw(b json.RawMessage) json.RawMessage {
	if b == nil {
		return nil
	}
	return slices.Clone(b)
}
