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
	// <<< signatures
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

// <<< signatures
