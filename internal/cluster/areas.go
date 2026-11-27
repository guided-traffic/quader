package cluster

import (
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// AreaClass is how an area leaves the replica (R-ARCH-018).
type AreaClass string

const (
	// ChangeArea carries changes whose propagation an owning page bounds. They
	// go through the change log in PostgreSQL, which carries correctness, and
	// through Valkey, which carries speed.
	ChangeArea AreaClass = "change"
	// SignalArea carries wake-ups and early evictions that client traffic
	// produces. They write nothing to PostgreSQL; delivery is at most once and
	// every consumer stays correct without it.
	SignalArea AreaClass = "signal"
)

// Area is one source of invalidation or wake-up. An area is registered in
// code, by its owning package at initialisation, and a change area also needs
// its cluster_epoch row from the migration that ships it.
type Area struct {
	// Name matches ^[a-z][a-z0-9_]{1,31}$.
	Name  string
	Owner string
	Class AreaClass
	// PayloadVersion is the highest payload version this build understands. A
	// change row with a higher version flushes the area's caches instead of
	// being ignored (rolling upgrades, R-ARCH-018 step 5). Change areas only.
	PayloadVersion int
}

var areaPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

var areas = struct {
	mu sync.RWMutex
	by map[string]Area
}{by: map[string]Area{}}

// RegisterArea registers an area. It panics on a programming error: an invalid
// name, an unknown class, a change area without a payload version, or a
// duplicate.
func RegisterArea(a Area) {
	if !areaPattern.MatchString(a.Name) {
		panic(fmt.Sprintf("cluster: invalid area name %q", a.Name))
	}
	switch a.Class {
	case ChangeArea:
		if a.PayloadVersion < 1 {
			panic(fmt.Sprintf("cluster: change area %q needs a payload version of 1 or more", a.Name))
		}
	case SignalArea:
	default:
		panic(fmt.Sprintf("cluster: area %q has class %q", a.Name, a.Class))
	}
	areas.mu.Lock()
	defer areas.mu.Unlock()
	if _, dup := areas.by[a.Name]; dup {
		panic(fmt.Sprintf("cluster: area %q is registered twice", a.Name))
	}
	areas.by[a.Name] = a
}

// LookupArea returns a registered area.
func LookupArea(name string) (Area, bool) {
	areas.mu.RLock()
	defer areas.mu.RUnlock()
	a, ok := areas.by[name]
	return a, ok
}

// Areas lists the registered areas of a class, sorted by name; the empty class
// lists all of them.
func Areas(class AreaClass) []Area {
	areas.mu.RLock()
	defer areas.mu.RUnlock()
	out := make([]Area, 0, len(areas.by))
	for _, a := range areas.by {
		if class == "" || a.Class == class {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// The areas that exist from the first release (R-ARCH-018). A page that adds an
// area registers it in its own package and seeds the cluster_epoch row of a
// change area in the migration that ships it.
func init() {
	RegisterArea(Area{Name: AreaJobs, Owner: "R-ARCH-016", Class: SignalArea})
	RegisterArea(Area{Name: AreaCache, Owner: "R-ARCH-018", Class: ChangeArea, PayloadVersion: 1})

	// The areas of M1: auth (identity), authz and repo_config. A change area also
	// needs its cluster_epoch row from the migration that ships it.
	// >>> M1 identity
	RegisterArea(Area{Name: AreaAuth, Owner: "R-AUTH-023", Class: ChangeArea, PayloadVersion: 1})
	// <<< M1 identity
	//
	// >>> M1 authz
	RegisterArea(Area{Name: AreaAuthz, Owner: "R-AUTHZ-028", Class: ChangeArea, PayloadVersion: 1})
	// <<< M1 authz
	//
	// >>> M1 repository
	RegisterArea(Area{Name: AreaRepoConfig, Owner: "R-REPO-049", Class: ChangeArea, PayloadVersion: 1})
	// <<< M1 repository
	//
	// The areas of M2 (repositories-and-proxy.md; Q-ARCH-194). Their cluster_epoch
	// rows are seeded by 00014_remote_proxy and 00015_virtual_claims_ownership;
	// metadata_changed is a signal and has none.
	// >>> M2 schema
	RegisterArea(Area{Name: AreaClaims, Owner: "R-REPO-023", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaCacheInvalidation, Owner: "R-REPO-045", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaUpstreamState, Owner: "R-REPO-029", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaMetadataChanged, Owner: "R-REPO-020", Class: SignalArea})
	// <<< M2 schema
	//
	// The areas of the CI federation (gitlab-ci-oidc.md, R-CI-002, R-CI-003, R-CI-009). Their
	// cluster_epoch rows are seeded by 00026_ci_oidc.
	// >>> M4 ci
	RegisterArea(Area{Name: AreaCIMappings, Owner: "R-CI-009", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaCIIssuerKeys, Owner: "R-CI-003", Class: ChangeArea, PayloadVersion: 1})
	// <<< M4 ci
	//
	// The area of the vulnerability scanning (vulnerability-scanning.md, R-VULN-027). Its cluster_epoch row is
	// seeded by 00035_vulnerabilities.
	// >>> M4b vulnerabilities
	RegisterArea(Area{Name: AreaVulnData, Owner: "R-VULN-027", Class: ChangeArea, PayloadVersion: 1})
	// <<< M4b vulnerabilities
	//
	// The signal area of the mirror's yield to client fetches (repositories-and-proxy.md, R-REPO-055).
	// >>> M6b mirror
	RegisterArea(Area{Name: AreaMirrorYield, Owner: "R-REPO-055", Class: SignalArea})
	// <<< M6b mirror
	//
	// The area of the download limits (usage-and-limits.md, R-LIM-010, R-LIM-013). Its cluster_epoch row is seeded by
	// 00081_limits.
	// >>> W-26 limits
	RegisterArea(Area{Name: AreaLimits, Owner: "R-LIM-013", Class: ChangeArea, PayloadVersion: 1})
	// <<< W-26 limits
	//
	// The areas of the malware scanning (malware-scanning.md R-MAL-010, R-MAL-014, R-MAL-019). The cluster_epoch rows of the two
	// change areas are seeded by 00088_malware; malware_verdict is a signal and has none (Q-MAL-44).
	// >>> malware
	RegisterArea(Area{Name: AreaMalwareSettings, Owner: "R-MAL-019", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaMalwareDatabase, Owner: "R-MAL-010", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaMalwareVerdict, Owner: "R-MAL-014", Class: SignalArea})
	// <<< malware
	//
	// The area of signatures on artifacts (signatures.md R-SIG-012, R-SIG-041). Its cluster_epoch row is seeded by
	// 00098_signature_trust.
	// >>> signatures
	RegisterArea(Area{Name: AreaSignatureAnchors, Owner: "R-SIG-012", Class: ChangeArea, PayloadVersion: 1})
	RegisterArea(Area{Name: AreaSignatureVerdict, Owner: "R-SIG-067", Class: SignalArea})
	// <<< signatures
	//
	// The area of Quader's signing identity (signatures.md R-SIG-059). Its cluster_epoch row is seeded by 00101_signing_identity.
	// >>> signing
	RegisterArea(Area{Name: AreaSigningIdentities, Owner: "R-SIG-059", Class: ChangeArea, PayloadVersion: 1})
	// <<< signing
	//
	// The area of the keys of encrypted stored content (storage.md R-STOR-083). Its cluster_epoch row is seeded by 00107_storage_keys.
	// >>> storage keys
	RegisterArea(Area{Name: AreaStorageKeys, Owner: "R-STOR-083", Class: ChangeArea, PayloadVersion: 2})
	// <<< storage keys
}

// Names of the areas of this package.
const (
	// AreaJobs wakes the workers of a queue; the payload names the queue.
	AreaJobs = "jobs"
	// AreaCache carries system settings and flush requests.
	AreaCache = "cache"
)

// Names of the areas of the M1 packages, declared next to their registration (constants
// between the markers).

// >>> M1 identity

// AreaAuth carries the changes that end, narrow or alter a credential, a
// principal or a runtime authentication setting (R-AUTH-023). The payload
// kinds are in docs/developer/identity.md.
const AreaAuth = "auth"

// <<< M1 identity
//
// >>> M1 authz

// AreaAuthz carries policy, role-binding, exposure-rule and authorization-setting
// changes; its epoch is the authorization epoch (R-AUTHZ-028).
const AreaAuthz = "authz"

// <<< M1 authz
//
// >>> M1 repository

// AreaRepoConfig carries repository rows and virtual membership: create, update, delete
// and every change of an input of the grant index (R-REPO-008, R-AUTHZ-028). The payload
// names the repository id, never a grant. The cluster_epoch row is seeded by migration 00010.
const AreaRepoConfig = "repo_config"

// <<< M1 repository
//
// >>> M2 schema

// AreaClaims carries the changes of namespace claims and virtual pin rules: a
// claims write, the auto claim of a first publish and the claim removal of the
// repository delete job (R-REPO-023, R-ARCH-018). The cluster_epoch row is seeded
// by migration 00015.
const AreaClaims = "claims"

// AreaCacheInvalidation carries a cache invalidation of a remote or virtual
// repository and the deletion of folded invalidation rows by the fold job
// (R-REPO-045). The cluster_epoch row is seeded by migration 00014.
const AreaCacheInvalidation = "cache_invalidation"

// AreaUpstreamState carries the transitions of a circuit, the global offline switch
// and the reload of one repository's gone packages (R-REPO-029, R-REPO-031). The
// cluster_epoch row is seeded by migration 00014.
const AreaUpstreamState = "upstream_state"

// AreaMetadataChanged is the signal for the early eviction of merged metadata after a
// change of a package (R-REPO-020); the rev_seq check keeps local members exact without
// it. Its name and payload are declared once, in the catalog leaf the writer of the
// formats host and the virtual engine share.
const AreaMetadataChanged = catalog.MetadataChangedArea

// <<< M2 schema

// >>> M4 ci

// AreaCIMappings carries every write of a CI issuer, of its mappings and of the mapping principals'
// state: create, update, enable, disable, delete and the cascade of an issuer's deletion. Every
// replica reloads its copy of the issuer together with its mappings (R-CI-002, R-CI-009); the
// exchange does not depend on the timing, it compares versions in its transaction (R-CI-007,
// Q-CI-15). The payload names the issuer ID.
const AreaCIMappings = "ci_mappings"

// AreaCIIssuerKeys carries a changed key set of one CI issuer (R-CI-003): written when a fetch stored
// a set whose content differs from the stored one, never for an unchanged set. The payload names
// the issuer ID.
const AreaCIIssuerKeys = "ci_issuer_keys"

// <<< M4 ci

// >>> M4b vulnerabilities

// AreaVulnData carries a changed advisory data state of one ecosystem (R-VULN-027): written once per sync run that
// changed records or the state (a first complete import, a changed `complete`), never per request. Every replica
// reloads its copy of vuln_source_state, the cache `vuln_source_state` of SafetyTTL 60 s, and the rendered NuGet page,
// `vuln_nuget_page`, keyed by the epoch, validates itself against it (R-VULN-018). The payload is
// {"ecosystem": "npm", "epoch": 42}.
const AreaVulnData = "vuln_data"

// <<< M4b vulnerabilities

// >>> M6b mirror

// AreaMirrorYield is the signal a client-serving replica raises when a client fetch of a remote repository has to wait for a
// fetch slot (R-REPO-055, Q-ARCH-286): at most once per second and repository, the payload is the repository's numeric id.
// Every process that runs background fetches of the repository pre-empts those no client of its own joined and starts none
// for 30 s. It is a signal area: nothing is written to PostgreSQL, delivery is at most once, and the background stays correct
// without it because it keeps within its share.
const AreaMirrorYield = "mirror_yield"

// <<< M6b mirror

// >>> W-26 limits

// AreaLimits carries a change of the download limits (R-LIM-010 to R-LIM-016, R-LIM-026): a profile, a rule, a ceiling, a
// project's maximum or the settings (mode, block time). Every replica rebuilds its compiled index of the limits
// (limits.Index), which a request reads instead of the database. The payload is {"what": "<profile|assignment|ceiling|
// maximum|settings>"}.
const AreaLimits = "limits"

// <<< W-26 limits

// >>> malware

// AreaMalwareSettings carries a change of the installation's switch "Only checked artifacts can be downloaded" (R-MAL-019).
// Every replica reloads its snapshot of malware_settings, which the download gate reads instead of the database. The payload is
// {"version": <the row's version>}.
const AreaMalwareSettings = "malware_settings"

// AreaMalwareDatabase carries a change of the signature database in use (R-MAL-010): a new version, a version put back after a
// scanner refused one, a failure of the task. The payload is {"version": "<the version in use>"}.
const AreaMalwareDatabase = "malware_database"

// AreaMalwareVerdict is the signal the job raises after it committed verdicts (R-MAL-014, Q-MAL-44): the payload is the
// lower-case hex SHA-256 of one blob whose verdict was written. A download that waits for that verdict decides again; a waiter on
// a replica that missed the signal reads the row once a second, so nothing depends on its delivery.
const AreaMalwareVerdict = "malware_verdict"

// <<< malware

// >>> signatures

// AreaSignatureAnchors carries a write of a trust anchor, a block, the switch for project Owners or a stored public Sigstore root
// (R-SIG-012, R-SIG-041): every replica refreshes what it holds of them, and the trust job recomputes the trust rows under the raised
// anchor epoch. The payload is {"epoch": <the anchor epoch after the write>}.
const AreaSignatureAnchors = "signature_anchors"

// AreaSignatureVerdict is the signal raised after the commit of a signature's check, of a batch of trust rows or of a background fetch
// (stage 3 of the signatures, R-SIG-067): the payload is the lower-case hex SHA-256 of the subject, "*" for every subject (a batch of the
// trust step). A download that waits at a repository's gate decides again; a waiter on a replica that missed the signal reads again once
// a second, so nothing depends on its delivery.
const AreaSignatureVerdict = "signature_verdict"

// <<< signatures

// >>> signing

// AreaSigningIdentities carries a write of Quader's signing identities (R-SIG-045 to R-SIG-048, R-SIG-059): a key request, an
// installation, a switch, a removal, a retirement. Every replica refreshes its snapshot of the identities, from which npm's keys, the
// trusted root and the chain are answered. The payload is {"id": <the identity>, "state": "<its state after the write>"}.
const AreaSigningIdentities = "signing_identities"

// <<< signing

// >>> storage keys

// AreaStorageKeys carries a write of storage_keys outside a replica's start (R-STOR-083): a new generation and an import. Every replica
// that holds the keys of the connection reads its rows again before it wraps the next object, so a new generation wraps new objects
// within the area's bound; a record that names a key the replica does not hold makes it read the rows itself as well. The payload is
// {"connection": "<id>"} (version 1), and {"connection": "<id>", "destroyed": ["<key id>", …]} (version 2) for a destruction of the
// connection's keys, on which every replica destroys the keys it holds of the connection at once (R-STOR-101); it never carries a key
// or a sealed value.
const AreaStorageKeys = "storage_keys"

// <<< storage keys

// >>> download links

// AreaDownloadLinkSettings carries a change of the installation's switch of the delivery by signed links of the object store and
// of its details (storage.md R-STOR-014): every replica reloads its snapshot of download_link_settings, which the serve point
// reads instead of the database, and decides again which connections can serve links. The payload is
// {"version": <the row's version>}. Its cluster_epoch row is seeded by 00108_download_links.
const AreaDownloadLinkSettings = "download_link_settings"

func init() {
	RegisterArea(Area{Name: AreaDownloadLinkSettings, Owner: "R-STOR-014", Class: ChangeArea, PayloadVersion: 1})
}

// <<< download links

// >>> bastion handover

// AreaBastionHandover carries a change of a Bastion registration's switch of the handover per blob (bastion.md R-BAS-071): every
// replica reloads its snapshot of the registrations whose handover is on, which the serve point reads instead of the database, and
// runs the connections' link checks again when a registration's handover was turned on (storage.md R-STOR-096). The payload is
// {"bastion": "<the registration's principal ID>"}. Its cluster_epoch row is seeded by 00110_bastion_handover.
const AreaBastionHandover = "bastion_handover"

func init() {
	RegisterArea(Area{Name: AreaBastionHandover, Owner: "R-BAS-071", Class: ChangeArea, PayloadVersion: 1})
}

// <<< bastion handover

// >>> bastion egress

// AreaBastionEgress carries a change of the networks a Bastion registration's Bastion connects from (bastion.md R-BAS-017,
// Q-BAS-334): every replica reloads its snapshot of the registrations, which the recognition of a Bastion's request reads instead of
// the database. The payload is {"bastion": "<the registration's principal ID>"}. Its cluster_epoch row is seeded by 00145_bastion_egress.
const AreaBastionEgress = "bastion_egress"

func init() {
	RegisterArea(Area{Name: AreaBastionEgress, Owner: "R-BAS-017", Class: ChangeArea, PayloadVersion: 1})
}

// <<< bastion egress

// >>> bastion configuration

// AreaBastionConfiguration carries a change of a Bastion registration's configuration (bastion.md R-BAS-093): its switches gui and
// docs, the mode, the preset, the flood figures, the slots, the throughput and the anonymous ceiling of its limits, and a registration
// created or deleted. Every replica reloads its snapshot of the registrations, from which the handshake answers the configuration (a
// change of a rule of a Bastion's place or of a profile is the area limits, which the snapshot follows too). The payload is
// {"bastion": "<the registration's principal ID>"}. Its cluster_epoch row is seeded by 00147_bastion_configuration.
const AreaBastionConfiguration = "bastion_configuration"

func init() {
	RegisterArea(Area{Name: AreaBastionConfiguration, Owner: "R-BAS-093", Class: ChangeArea, PayloadVersion: 1})
}

// <<< bastion configuration

// >>> bastion base url

// AreaBastionBaseURL is the signal raised after a Bastion replica's contact that reports a base URL the registration's builder of its
// public catalogue does not read yet: a Bastion that connects for the first time, or that moved to another address (bastion.md
// R-BAS-105, Q-BAS-499). The trigger of the builder starts a run, so a web interface switched on before the Bastion connected is built
// within seconds of the contact instead of at the hourly run. The payload is the registration's principal ID. A signal has no
// cluster_epoch row: a lost one waits for the hourly run.
const AreaBastionBaseURL = "bastion_base_url"

func init() {
	RegisterArea(Area{Name: AreaBastionBaseURL, Owner: "R-BAS-105", Class: SignalArea})
}

// <<< bastion base url

// >>> bastion distribution

// AreaBastionDistribution carries a change of what a Bastion may distribute (bastion.md R-BAS-134): a project allowed for a Bastion or
// taken off its list, a Bastion added to a repository or taken off it, and the choices a repository's move deleted. The public
// catalogue's builder starts a run after it (R-BAS-105). The payload is {"bastion": "<the registration's principal ID>"}. Its
// cluster_epoch row is seeded by 00159_bastion_distribution.
const AreaBastionDistribution = "bastion_distribution"

func init() {
	RegisterArea(Area{Name: AreaBastionDistribution, Owner: "R-BAS-134", Class: ChangeArea, PayloadVersion: 1})
}

// <<< bastion distribution
