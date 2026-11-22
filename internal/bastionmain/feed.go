package bastionmain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Reader is what the feed needs of the database: reads with the failover retry. *postgres.DB implements it.
type Reader interface {
	Read(ctx context.Context, fn func(q.DBTX) error) error
}

// nilUUID is the cursor ID of a synthetic cursor: it sorts before every event of that instant.
const nilUUID = "00000000-0000-0000-0000-000000000000"

// behindFactor bounds the entries behind the cursor at this multiple of the page size; more means the Bastion
// has to start again (Q-BAS-49 item 3).
const behindFactor = 4

// The audit actions of the project model that the feed reads. They are names, not constants of the audit package:
// the registry of the audit package stays the one list, and TestR_BAS_015_EveryJournaledEventIsClassified holds
// these to it.
const (
	eventRepositoryMoved      = "repository.moved"
	eventRepositoryDeleted    = "repository.deleted"
	eventProjectMemberRemoved = "authz.project.member_removed"
	eventProjectMemberChanged = "authz.project.member_changed"
	eventClaimsChanged        = "repository.claims_changed"
)

// feedActions are the audit actions that can yield an entry (R-BAS-015). The registry of the audit package
// stays the one list of journaled actions; this is the subset the mapping reads, passed to the query as a text
// array. An action in the journal that is not here is in noFeedEntry with the reason it yields no entry.
func feedActions() []string {
	return []string{
		audit.EventTokenRevoked, audit.EventCITokenRevoked, audit.EventTokenRotated,
		audit.EventUserDisabled, audit.EventUserDeleted, audit.EventSADisabled, audit.EventSADeleted, audit.EventSAUpdated,
		audit.EventGroupMembersChanged, audit.EventUserGroupsSynced, "authz.role.unbound",
		audit.EventGroupDeleted, "authz.policy.deleted", "authz.policy.updated", "authz.settings.changed",
		audit.EventCIIssuerUpdated, audit.EventCIIssuerDisabled, audit.EventCIIssuerDeleted,
		audit.EventCIMappingUpdated, audit.EventCIMappingDisabled, audit.EventCIMappingDeleted,
		"endpoint.exposure.updated", "endpoint.exposure.deleted",
		audit.EventSettingsAuthChanged, audit.EventProjectVisibilityChanged, audit.EventUserExternalChanged,
		eventRepositoryMoved, eventProjectMemberRemoved, eventProjectMemberChanged,
		audit.EventRepositoryDisabled, eventRepositoryDeleted, audit.EventRepositoryNarrowed, eventClaimsChanged,
		audit.EventNPMUnpublish, audit.EventPackageDeleted,
		audit.EventOCIManifestDeleted, audit.EventDebPackageDeleted, audit.EventDebCoordinatesRemoved,
		audit.EventDebSigningKeyPromoted, audit.EventDebSigningKeyRetired, audit.EventAnsibleVersionDeleted,
		audit.EventVulnFindingStatusChanged, audit.EventVulnFindingReopened,
		audit.EventMalwareFound, audit.EventMalwareReleaseWithdrawn,
		audit.EventSignatureReleaseWithdrawn, audit.EventSignatureAnchorRemoved, audit.EventSignatureAnchorUpdated,
		audit.EventSignatureSettingsChanged, audit.EventSignatureSignerBlocked,
	}
}

// FeedActions returns the audit actions the feed reads, for the tests and the documentation.
func FeedActions() []string { return feedActions() }

// noFeedEntry are the events the revocation journal copies that yield no entry, each with the reason it narrows
// nothing a Bastion keeps (R-BAS-015, Q-BAS-120). Every journaled event of the audit registry is either read by
// the feed or listed here; TestR_BAS_015_EveryJournaledEventIsClassified holds the two lists to the registry.
func noFeedEntry() map[string]string {
	return map[string]string{
		"authz.ownership.removed":          "bounds who may publish a package name (R-AUTHZ-018); a read is never decided by ownership",
		"authz.ownership.released":         "bounds who may publish a package name (R-AUTHZ-018); a read is never decided by ownership",
		audit.EventAnsibleTombstoneCleared: "frees the number of a deleted Ansible Galaxy version, which widens what may be published and changes no byte a Bastion holds (Q-BAS-175)",
		audit.EventPackagePublished:        "reserves the version numbers a publish took (R-HA-054); a Bastion never publishes",
		audit.EventProjectDeleted:          "a project is deleted only when it holds no repository and no service account, and its policies are deleted before it, each journaled as authz.policy.deleted (grants)",
		audit.EventBastionDisabled:         "a registration is no caller a Bastion answers; the Bastion learns of its own disable from 401 bastion_token_invalid on its next call (R-BAS-006, Q-BAS-145)",
		audit.EventBastionDeleted:          "a registration is no caller a Bastion answers; its tokens' token.revoked entries come before it, and the Bastion learns of its end from 401 bastion_token_invalid (R-BAS-006, Q-BAS-145)",
		audit.EventGroupUpdated:            "clearing auto-join removes no membership that exists (R-AUTH-019): only users created afterwards stay out of the group; a rename or a description changes no grant",
		audit.EventUserUpdated:             "removes a pending identity link, which no caller holds until a login completes it; a profile change changes no grant",
		audit.EventMalwareSettingsChanged:  "turning the switch on changes which bytes the main installation serves in every repository; a content entry for every repository would empty every Bastion's cache, so a Bastion learns it at its next revalidation (H-1102, Q-BAS-164)",
		// Quader's signing identity (signatures.md R-SIG-048, R-SIG-059): what a withdrawal changes on the wire, npm's keys and the
		// documents that carry the identity's signatures, reaches a Bastion as those documents' new revisions, not as an entry of its own.
		audit.EventSigningRemoved: "stops the signing identity; the keys document and the npm documents it changes reach a Bastion as their new revisions (R-SIG-048)",
		audit.EventSigningRetired: "withdraws a signing identity; the keys document and the npm documents it changes reach a Bastion as their new revisions (R-SIG-048)",
	}
}

// NoFeedEntry returns the journaled events that yield no entry, with their reasons, for the tests and the
// documentation.
func NoFeedEntry() map[string]string { return noFeedEntry() }

// FeedOptions configure [NewFeed].
type FeedOptions struct {
	DB Reader
	// Lookback and MaxPage are bastions.feed.lookback and bastions.feed.max_page.
	Lookback time.Duration
	MaxPage  int
	// Endpoint is the name of the default endpoint, the one endpoint whose exposure change reaches a Bastion
	// ("default" when empty).
	Endpoint string
	Log      *slog.Logger
}

// Feed is the revocation feed of the main installation (R-BAS-015): it maps the journaled audit events to the
// entries of the interface and pages through them. It holds no state.
type Feed struct {
	o FeedOptions
}

// NewFeed returns the feed.
func NewFeed(o FeedOptions) *Feed {
	if o.Endpoint == "" {
		o.Endpoint = "default"
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Feed{o: o}
}

// FeedEvent is a stored audit event as the mapping reads it.
type FeedEvent struct {
	ID           string
	At           time.Time
	Action       string
	ResourceType string
	ResourceID   string
	Repository   string
	Details      []byte
}

// rawEntry is an entry before the lookups: the repositories of a content entry are still the event's own.
type rawEntry struct {
	kind       iface.FeedKind
	tokens     []string
	principals []string
	repository string
	// digest, with kind content, is the hex SHA-256 of a blob whose holders are the repositories of the entry (Q-BAS-164).
	digest string
	// gated, with kind content, names the repositories whose signature gate is on when the feed is read (R-SIG-075, Q-SIG-94).
	gated bool
	// names, with kind principal, are member names that still need their IDs.
	names []string
}

// detailsDoc is the part of an event's details the mapping reads; a document that does not read leaves the
// members empty, and the fallbacks of [mapEvent] apply.
type detailsDoc struct {
	Predecessor string          `json:"predecessor"`
	TokenIDs    []string        `json:"tokenIds"`
	Principal   string          `json:"principal"`
	PrincipalID string          `json:"principalId"`
	Endpoint    string          `json:"endpoint"`
	Removed     json.RawMessage `json:"removed"`
	// Added is the claims a repository.claims_changed event added, read by claimsEntries only: the other events that
	// carry the member shape it differently.
	Added json.RawMessage `json:"added"`
	// Block is "set" in the details of a finding event that set a download block (R-VULN-013).
	Block string `json:"block"`
	// Before and After are the old and the new value of a change event (a level, a flag).
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
	// SHA256 is the blob of a malware event (R-MAL-026).
	SHA256 string `json:"sha256"`
}

// mapper turns one stored event and its parsed details into the entries it yields.
type mapper func(ev FeedEvent, d *detailsDoc, defaultEndpoint string) []rawEntry

// mappers is the table of R-BAS-015: the mapper of each audit action the feed reads. It is built once.
var mappers = sync.OnceValue(func() map[string]mapper {
	t := map[string]mapper{
		audit.EventTokenRevoked:             func(ev FeedEvent, _ *detailsDoc, _ string) []rawEntry { return tokenEntry(ev.ResourceID) },
		audit.EventTokenRotated:             func(_ FeedEvent, d *detailsDoc, _ string) []rawEntry { return tokenEntry(d.Predecessor) },
		audit.EventCITokenRevoked:           func(_ FeedEvent, d *detailsDoc, _ string) []rawEntry { return tokenEntry(d.TokenIDs...) },
		audit.EventUserGroupsSynced:         mapGroupsSynced,
		audit.EventGroupMembersChanged:      mapMembersChanged,
		"authz.role.unbound":                mapRoleUnbound,
		"endpoint.exposure.updated":         mapExposure,
		"endpoint.exposure.deleted":         mapExposure,
		audit.EventSettingsAuthChanged:      mapSettings,
		audit.EventProjectVisibilityChanged: mapVisibility,
		audit.EventUserExternalChanged:      mapExternal,
		audit.EventRepositoryNarrowed:       mapRepository,
		eventClaimsChanged:                  mapRepository,
		audit.EventNPMUnpublish:             mapWithdrawal,
		audit.EventPackageDeleted:           mapWithdrawal,
		// The withdrawals of the formats a Bastion serves since every format reaches it (R-BAS-050, R-BAS-055, Q-BAS-175): a deleted
		// manifest (and the tags and children its cascade took), a withheld Debian package file, a package taken out of a suite, a new
		// primary key and a withdrawn key, and a deleted Galaxy version each change which bytes a URL of the repository is answered with.
		audit.EventOCIManifestDeleted:       mapWithdrawal,
		audit.EventDebPackageDeleted:        mapWithdrawal,
		audit.EventDebCoordinatesRemoved:    mapWithdrawal,
		audit.EventDebSigningKeyPromoted:    mapWithdrawal,
		audit.EventDebSigningKeyRetired:     mapWithdrawal,
		audit.EventAnsibleVersionDeleted:    mapWithdrawal,
		audit.EventVulnFindingReopened:      mapBlock,
		audit.EventVulnFindingStatusChanged: mapBlock,
		audit.EventMalwareFound:             mapBlob,
		audit.EventMalwareReleaseWithdrawn:  mapBlob,
		// The gate per repository of the signatures (signatures.md R-SIG-075, Q-SIG-94): a withdrawn release refuses an artifact in its
		// repository again; what narrows the anchors or blocks a signer refuses artifacts in every repository whose gate is on.
		audit.EventSignatureReleaseWithdrawn: mapWithdrawal,
		audit.EventSignatureAnchorRemoved:    mapGated,
		audit.EventSignatureSignerBlocked:    mapGated,
		audit.EventSignatureAnchorUpdated:    mapAnchorUpdated,
		audit.EventSignatureSettingsChanged:  mapProjectAnchorsOff,
	}
	// serviceaccount.updated narrows the actions of a repository's service account (R-AUTHZ-039): the answers of that account go.
	for _, a := range []string{audit.EventUserDisabled, audit.EventUserDeleted, audit.EventSADisabled, audit.EventSADeleted, audit.EventSAUpdated} {
		t[a] = mapPrincipal
	}
	// A move takes the repository out of its project's roles and policies and changes its address (R-REPO-060), a disabled or
	// deleted repository takes no part in resolution or download any more (R-REPO-004, R-REPO-013), and a removed or re-roled
	// member loses reads on every repository of the project (R-AUTHZ-026): the event names a project or one repository, not the
	// callers or the virtual repositories over it. A content entry drops only the URL answers of a repository, not its shape
	// answers, so only grants is exact enough (Q-BAS-120, Q-AUTHZ-238).
	t["authz.settings.changed"] = mapSettingsChanged
	for _, a := range []string{
		audit.EventGroupDeleted, "authz.policy.deleted", "authz.policy.updated",
		audit.EventCIIssuerUpdated, audit.EventCIIssuerDisabled, audit.EventCIIssuerDeleted,
		audit.EventCIMappingUpdated, audit.EventCIMappingDisabled, audit.EventCIMappingDeleted,
		eventRepositoryMoved, eventProjectMemberRemoved, eventProjectMemberChanged,
		audit.EventRepositoryDisabled, eventRepositoryDeleted,
	} {
		t[a] = func(FeedEvent, *detailsDoc, string) []rawEntry { return grantsEntry() }
	}
	return t
})

func grantsEntry() []rawEntry { return []rawEntry{{kind: iface.FeedGrants}} }

func mapPrincipal(ev FeedEvent, _ *detailsDoc, _ string) []rawEntry {
	if ev.ResourceID == "" {
		return grantsEntry()
	}
	return []rawEntry{{kind: iface.FeedPrincipal, principals: []string{ev.ResourceID}}}
}

// mapGroupsSynced: the subject is the user whose groups were synchronized; additions only widen.
func mapGroupsSynced(ev FeedEvent, d *detailsDoc, _ string) []rawEntry {
	if !hasEntries(d.Removed) {
		return nil
	}
	return mapPrincipal(ev, d, "")
}

func mapMembersChanged(_ FeedEvent, d *detailsDoc, _ string) []rawEntry {
	members := removedMembers(d.Removed)
	if len(members) == 0 {
		return nil
	}
	return []rawEntry{{kind: iface.FeedPrincipal, names: members}}
}

// mapRoleUnbound: a user or service account is named by ID; a group, a CI identity and the built-in principals reach callers
// the entry cannot list.
func mapRoleUnbound(_ FeedEvent, d *detailsDoc, _ string) []rawEntry {
	if (strings.HasPrefix(d.Principal, "user:") || strings.HasPrefix(d.Principal, "sa:")) && d.PrincipalID != "" {
		return []rawEntry{{kind: iface.FeedPrincipal, principals: []string{d.PrincipalID}}}
	}
	return grantsEntry()
}

// mapExposure: only the exposure of the default endpoint reaches a Bastion, which connects to it (Q-BAS-4).
func mapExposure(_ FeedEvent, d *detailsDoc, defaultEndpoint string) []rawEntry {
	if d.Endpoint != "" && d.Endpoint != defaultEndpoint {
		return nil
	}
	return grantsEntry()
}

// mapSettings: the Bastion asks the handshake again (Q-BAS-150); no setting of settings.auth.changed narrows what a Bastion
// holds.
func mapSettings(FeedEvent, *detailsDoc, string) []rawEntry {
	return []rawEntry{{kind: iface.FeedSettings}}
}

// mapSettingsChanged: an access setting of a repository changed. Every permission answer is dropped (grants), except when the
// event only raised levels (every changed member is visibility.artifacts or visibility.wiki, and none lies lower than before): a
// raise opens reads and makes no kept answer wrong. A change of visibility.artifacts also asks the Bastion for the handshake's
// anonymousReads again (settings). A pair that does not read is read as a narrowing (R-AUTHZ-036, R-BAS-015).
func mapSettingsChanged(_ FeedEvent, d *detailsDoc, _ string) []rawEntry {
	var before, after map[string]json.RawMessage
	if json.Unmarshal(d.Before, &before) != nil || json.Unmarshal(d.After, &after) != nil || len(after) == 0 {
		return grantsEntry()
	}
	onlyRaises, artifacts := true, false
	for k, raw := range after {
		var a, b string
		if json.Unmarshal(raw, &a) != nil || json.Unmarshal(before[k], &b) != nil || (k != "visibility.artifacts" && k != "visibility.wiki") || levelRank(a) < levelRank(b) {
			onlyRaises = false
		}
		artifacts = artifacts || k == "visibility.artifacts"
	}
	var out []rawEntry
	if !onlyRaises {
		out = append(out, rawEntry{kind: iface.FeedGrants})
	}
	if artifacts {
		out = append(out, rawEntry{kind: iface.FeedSettings})
	}
	return out
}

// mapVisibility: the Bastion asks the handshake for anonymousReads again, and drops what it kept when the level was lowered
// (R-AUTHZ-036, R-BAS-015).
func mapVisibility(_ FeedEvent, d *detailsDoc, _ string) []rawEntry {
	out := []rawEntry{{kind: iface.FeedSettings}}
	var before, after string
	if json.Unmarshal(d.Before, &before) != nil || json.Unmarshal(d.After, &after) != nil || levelRank(after) < levelRank(before) {
		out = append(out, rawEntry{kind: iface.FeedGrants}) // an unreadable pair is read as a lowering: it drops answers, never keeps one
	}
	return out
}

// levelRank orders the visibility levels from the most restrictive; an unknown level is the most open, so that a pair that
// does not read never counts as a raise.
func levelRank(l string) int {
	switch l {
	case "private":
		return 0
	case "internal":
		return 1
	}
	return 2
}

// mapExternal: a user flagged external is dropped from what a Bastion keeps for it; clearing the flag only widens (R-AUTH-044).
func mapExternal(ev FeedEvent, d *detailsDoc, _ string) []rawEntry {
	var after bool
	if json.Unmarshal(d.After, &after) == nil && !after {
		return nil
	}
	return mapPrincipal(ev, d, "")
}

func mapRepository(ev FeedEvent, d *detailsDoc, _ string) []rawEntry {
	return repositoryEntries(ev, d.Added, grantsEntry())
}

// withdrawal reports whether an action is mapped by [mapWithdrawal] from the event's repository: one that names none yields no
// entry, which the feed logs.
func withdrawal(action string) bool {
	switch action {
	case audit.EventNPMUnpublish, audit.EventPackageDeleted, audit.EventSignatureReleaseWithdrawn,
		audit.EventOCIManifestDeleted, audit.EventDebPackageDeleted, audit.EventDebCoordinatesRemoved,
		audit.EventDebSigningKeyPromoted, audit.EventDebSigningKeyRetired, audit.EventAnsibleVersionDeleted:
		return true
	}
	return false
}

// mapWithdrawal: a withdrawal without a repository yields none (the caller logs it).
func mapWithdrawal(ev FeedEvent, _ *detailsDoc, _ string) []rawEntry {
	if ev.Repository == "" {
		return nil
	}
	return []rawEntry{{kind: iface.FeedContent, repository: ev.Repository}}
}

// mapBlock: a finding event that set a block withdraws a version a Bastion may hold (R-VULN-015); one that lifted a block or
// touched none narrows nothing. The entry is the content of the finding's repository and of the virtual ones above it.
func mapBlock(ev FeedEvent, d *detailsDoc, defaultEndpoint string) []rawEntry {
	if d.Block != "set" {
		return nil
	}
	return mapWithdrawal(ev, d, defaultEndpoint)
}

// mapBlob: a find and a withdrawn release refuse a blob in every repository that holds it (R-MAL-018, Q-BAS-164). The entry is
// the content of those repositories when the feed is read and of the virtual ones above them; an event whose blob does not read
// cannot be narrowed to repositories: grants.
func mapBlob(_ FeedEvent, d *detailsDoc, _ string) []rawEntry {
	if b, err := hex.DecodeString(d.SHA256); err != nil || len(b) != 32 {
		return grantsEntry()
	}
	return []rawEntry{{kind: iface.FeedContent, digest: d.SHA256}}
}

// mapGated: a removed anchor and a block narrow what the gate of every repository whose gate is on passes (R-SIG-066): the entry is the
// content of those repositories when the feed is read and of the virtual ones above them (R-SIG-075, Q-SIG-94).
func mapGated(FeedEvent, *detailsDoc, string) []rawEntry {
	return []rawEntry{{kind: iface.FeedContent, gated: true}}
}

// anchorStateDoc is what the mapping reads of an anchor's state in signature.anchor.updated.
type anchorStateDoc struct {
	Enabled      bool              `json:"enabled"`
	Fingerprints []json.RawMessage `json:"fingerprints"`
}

// mapAnchorUpdated: a change narrows the gates when the anchor was on and is switched off or loses a fingerprint, the journal's rule
// (Q-SIG-42); a change that only widens or renames yields none. A pair that does not read is read as a narrowing.
func mapAnchorUpdated(ev FeedEvent, d *detailsDoc, endpoint string) []rawEntry {
	var before, after anchorStateDoc
	if json.Unmarshal(d.Before, &before) != nil || json.Unmarshal(d.After, &after) != nil {
		return mapGated(ev, d, endpoint)
	}
	if !before.Enabled {
		return nil
	}
	if !after.Enabled {
		return mapGated(ev, d, endpoint)
	}
	kept := map[string]bool{}
	for _, f := range after.Fingerprints {
		kept[compactJSON(f)] = true
	}
	for _, f := range before.Fingerprints {
		if !kept[compactJSON(f)] {
			return mapGated(ev, d, endpoint)
		}
	}
	return nil
}

// mapProjectAnchorsOff: switching project anchors off narrows the gates (R-SIG-019); switching them on widens. A pair that does not read
// is read as a narrowing.
func mapProjectAnchorsOff(ev FeedEvent, d *detailsDoc, endpoint string) []rawEntry {
	var before, after struct {
		ProjectAnchors *bool `json:"projectAnchors"`
	}
	if json.Unmarshal(d.Before, &before) != nil || json.Unmarshal(d.After, &after) != nil || before.ProjectAnchors == nil || after.ProjectAnchors == nil {
		return mapGated(ev, d, endpoint)
	}
	if *before.ProjectAnchors && !*after.ProjectAnchors {
		return mapGated(ev, d, endpoint)
	}
	return nil
}

// compactJSON is a JSON value without insignificant spaces, so that two renderings of one fingerprint compare equal.
func compactJSON(raw json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// mapEvent maps one audit event to the entries it yields (the table of R-BAS-015): none for an event that
// bounds writes or only widens access, one for most. An event whose target cannot be read yields a grants entry, which drops every answer: the
// conservative reading never keeps an answer the event may have narrowed. The one exception is a withdrawal
// without a repository, which yields none (the caller logs it).
func mapEvent(ev FeedEvent, defaultEndpoint string) []rawEntry {
	m, ok := mappers()[ev.Action]
	if !ok {
		return nil
	}
	var d detailsDoc
	if len(ev.Details) > 0 {
		_ = json.Unmarshal(ev.Details, &d)
	}
	return m(ev, &d, defaultEndpoint)
}

// repositoryEntries maps the two journaled events that change what a repository answers a URL with (Q-BAS-146). A narrowed
// repository answers with other bytes than before (patterns, members, pin rules, blocks) and whom it answers is unchanged, so
// the entry is the content of the repository and of the virtual ones above it. repository.claims_changed reads its added list:
// a change that added no claim only removed some, which widens and yields none; a global claim is in effect in every virtual
// repository of the format and refuses names at remote repositories, which a list of repositories cannot name, so it yields
// grants; claims of scope members yield content. An added list that does not read is treated as a global claim, and an event
// without a repository cannot be narrowed to one: grants.
func repositoryEntries(ev FeedEvent, added json.RawMessage, grants []rawEntry) []rawEntry {
	if ev.Action == audit.EventRepositoryNarrowed {
		if ev.Repository == "" {
			return grants
		}
		return []rawEntry{{kind: iface.FeedContent, repository: ev.Repository}}
	}
	var claims []struct {
		Scope string `json:"scope"`
	}
	if json.Unmarshal(added, &claims) != nil {
		return grants
	}
	if len(claims) == 0 {
		return nil
	}
	for _, c := range claims {
		if c.Scope != "members" {
			return grants
		}
	}
	if ev.Repository == "" {
		return grants
	}
	return []rawEntry{{kind: iface.FeedContent, repository: ev.Repository}}
}

func tokenEntry(ids ...string) []rawEntry {
	ids = slices.DeleteFunc(slices.Clone(ids), func(s string) bool { return s == "" })
	if len(ids) == 0 {
		return grantsEntry()
	}
	return []rawEntry{{kind: iface.FeedToken, tokens: ids}}
}

func hasEntries(raw json.RawMessage) bool {
	var a []json.RawMessage
	return json.Unmarshal(raw, &a) == nil && len(a) > 0
}

// removedMembers reads the member names of the removed list of group.members.changed.
func removedMembers(raw json.RawMessage) []string {
	var refs []struct {
		Member string `json:"member"`
	}
	if json.Unmarshal(raw, &refs) != nil {
		return nil
	}
	var out []string
	for _, r := range refs {
		if r.Member != "" && !slices.Contains(out, r.Member) {
			out = append(out, r.Member)
		}
	}
	return out
}

// Position returns the feed position of the handshake (R-BAS-010): the newest journaled event within the
// lookback, or the database clock as a synthetic cursor when there is none. A Bastion that starts from it
// misses nothing: the lookback re-sends the entries of a transaction that commits late.
func (f *Feed) Position(ctx context.Context) (string, error) {
	var out string
	err := f.o.DB.Read(ctx, func(db q.DBTX) (err error) {
		out, err = f.now(ctx, q.New(db))
		return err
	})
	return out, err
}

// now is the feed position of the moment: the newest journaled event within the lookback, or the database clock as a
// synthetic cursor when there is none, or when that event is older than the last restore (a cursor before the restore makes
// the feed answer reset, so it would never settle).
func (f *Feed) now(ctx context.Context, qs *q.Queries) (string, error) {
	st, err := qs.BastionFeedCursorState(ctx, q.BastionFeedCursorStateParams{CursorAt: time.Unix(0, 0).UTC().Format(time.RFC3339Nano), CursorID: nilUUID})
	if err != nil {
		return "", err
	}
	row, err := qs.BastionFeedNow(ctx, q.BastionFeedNowParams{WindowSecs: f.o.Lookback.Seconds(), Actions: feedActions()})
	olderThanRestore := err == nil && st.RestoredUs > 0 && row.OccurredAt.Before(time.UnixMicro(st.RestoredUs))
	switch {
	case err == nil && !olderThanRestore:
		return iface.Cursor{At: row.OccurredAt.UTC(), ID: row.ID}.String(), nil
	case err == nil || postgres.IsNoRows(err):
		return iface.Cursor{At: time.UnixMicro(st.DbNowUs).UTC()}.String(), nil
	}
	return "", err
}

// Read answers one feed request (R-BAS-015): after is the cursor of an earlier answer or a synthetic "t:…", limit
// the page size (0 or above bastions.feed.max_page means the maximum). A cursor that does not parse is a
// 400 invalid_cursor.
func (f *Feed) Read(ctx context.Context, after string, limit int) (iface.FeedAnswer, error) {
	cur, err := iface.ParseCursor(after)
	if err != nil {
		return iface.FeedAnswer{}, errs.E(errs.Invalid, errs.CodeInvalidCursor, "The cursor is not valid. Start again with the cursor of the handshake.", errs.WithCause(err))
	}
	if limit <= 0 || limit > f.o.MaxPage {
		limit = f.o.MaxPage
	}
	var ans iface.FeedAnswer
	err = f.o.DB.Read(ctx, func(db q.DBTX) error {
		ans = iface.FeedAnswer{Entries: []iface.FeedEntry{}, Cursor: after}
		return f.read(ctx, q.New(db), cur, limit, &ans)
	})
	if err != nil {
		return iface.FeedAnswer{}, err
	}
	return ans, nil
}

func (f *Feed) read(ctx context.Context, qs *q.Queries, cur iface.Cursor, limit int, ans *iface.FeedAnswer) error {
	cursorID := cur.ID
	if cur.Synthetic() {
		cursorID = nilUUID
	}
	cursorAt := cur.At.UTC().Format(time.RFC3339Nano)
	st, err := qs.BastionFeedCursorState(ctx, q.BastionFeedCursorStateParams{CursorAt: cursorAt, CursorID: cursorID})
	if err != nil {
		return err
	}
	now := time.UnixMicro(st.DbNowUs).UTC()
	if f.mustReset(cur, st, now) {
		return f.reset(ctx, qs, ans)
	}
	behindMax := behindFactor * f.o.MaxPage
	behind, err := qs.BastionFeedBehind(ctx, q.BastionFeedBehindParams{
		CursorAt: cursorAt, LookbackSecs: f.o.Lookback.Seconds(), CursorID: cursorID, Actions: feedActions(), MaxRows: int32(behindMax + 1), //nolint:gosec // bounded by the configuration range
	})
	if err != nil {
		return err
	}
	if len(behind) > behindMax {
		return f.reset(ctx, qs, ans)
	}
	ahead, err := qs.BastionFeedAhead(ctx, q.BastionFeedAheadParams{
		CursorAt: cursorAt, CursorID: cursorID, Actions: feedActions(), MaxRows: int32(limit + 1), //nolint:gosec // bounded by the configuration range
	})
	if err != nil {
		return err
	}
	events := make([]FeedEvent, 0, len(behind)+len(ahead))
	for _, r := range behind {
		events = append(events, FeedEvent{ID: r.ID, At: r.OccurredAt.UTC(), Action: r.Action, ResourceType: r.ResourceType, ResourceID: r.ResourceID, Repository: r.Repository, Details: r.Details})
	}
	nBehind := len(events)
	for _, r := range ahead {
		events = append(events, FeedEvent{ID: r.ID, At: r.OccurredAt.UTC(), Action: r.Action, ResourceType: r.ResourceType, ResourceID: r.ResourceID, Repository: r.Repository, Details: r.Details})
	}
	mapped := make([][]rawEntry, len(events))
	for i, ev := range events {
		mapped[i] = mapEvent(ev, f.o.Endpoint)
		if i >= nBehind && len(mapped[i]) == 0 && withdrawal(ev.Action) {
			f.o.Log.Warn("a withdrawal event names no repository; it yields no feed entry", "event_id", ev.ID, "action", ev.Action)
		}
	}
	look, err := f.lookups(ctx, qs, mapped)
	if err != nil {
		return err
	}
	// Entries behind the cursor are all sent; the page after it is cut at event boundaries, so the cursor
	// never names an event whose second entry was left out. An event that yields no entry still advances
	// the cursor.
	last, page := -1, 0
	for i := range events {
		es := mapped[i]
		if i >= nBehind {
			if i-nBehind >= limit || (page > 0 && page+len(es) > limit) {
				ans.More = true
				break
			}
			page += len(es)
			last = i
		}
		ans.Entries = append(ans.Entries, look.entries(events[i], es)...)
	}
	if last >= 0 {
		ans.Cursor = iface.Cursor{At: events[last].At, ID: events[last].ID}.String()
	}
	return nil
}

// mustReset applies the three conditions of a reset (R-BAS-015): the event the cursor names no longer exists
// although its time lies within the audit retention (a point-in-time restore, a failover that lost commits), the
// cursor lies in the future beyond the lookback, or it lies before the retention.
func (f *Feed) mustReset(cur iface.Cursor, st q.BastionFeedCursorStateRow, now time.Time) bool {
	if cur.At.After(now.Add(f.o.Lookback)) {
		return true
	}
	if st.RetentionDays > 0 && cur.At.Before(now.Add(-time.Duration(st.RetentionDays)*24*time.Hour)) {
		return true
	}
	// A restore re-applies the narrowings made after its restore point with a journal replay, which writes no audit event, so
	// the feed does not carry them: a Bastion whose cursor lies before the restore has answers that may predate them and
	// starts again (R-BAS-026, R-REPO-061).
	if st.RestoredUs > 0 && cur.At.Before(time.UnixMicro(st.RestoredUs)) {
		return true
	}
	return !cur.Synthetic() && !st.CursorExists
}

// reset answers a feed whose cursor is no longer valid: no entries, the position of now, reset true.
func (f *Feed) reset(ctx context.Context, qs *q.Queries, ans *iface.FeedAnswer) error {
	cur, err := f.now(ctx, qs)
	if err != nil {
		return err
	}
	ans.Cursor = cur
	ans.Entries, ans.More, ans.Reset = []iface.FeedEntry{}, false, true
	return nil
}

// lookups holds the results of the queries the mapped entries need: the virtual closure of every repository
// named by a content entry, the repositories (with their closure) that hold the blob of a content entry, and the IDs
// of the members named by a membership event.
type lookups struct {
	closure map[string][]string
	blobs   map[string][]string
	ids     map[string][]string
	// gated are the repositories whose signature gate is on, with the virtual ones above them, read once when an entry needs them.
	gated []string
}

func (f *Feed) lookups(ctx context.Context, qs *q.Queries, mapped [][]rawEntry) (lookups, error) {
	l := lookups{closure: map[string][]string{}, blobs: map[string][]string{}, ids: map[string][]string{}}
	var repos, names, digests []string
	needGated := false
	for _, es := range mapped {
		for _, e := range es {
			needGated = needGated || e.gated
			if e.repository != "" && !slices.Contains(repos, e.repository) {
				repos = append(repos, e.repository)
			}
			if e.digest != "" && !slices.Contains(digests, e.digest) {
				digests = append(digests, e.digest)
			}
			for _, n := range e.names {
				if n = memberName(n); !slices.Contains(names, n) {
					names = append(names, n)
				}
			}
		}
	}
	for _, name := range repos {
		members, err := qs.BastionVirtualClosure(ctx, []string{name})
		if err != nil {
			return l, err
		}
		// The repository first, then every virtual repository that includes it, all by their qualified names; an
		// unknown name (the repository was deleted) is reported alone.
		out := []string{name}
		for _, m := range members {
			if m != name {
				out = append(out, m)
			}
		}
		l.closure[name] = out
	}
	for _, d := range digests {
		if err := l.blobHolders(ctx, qs, d); err != nil {
			return l, err
		}
	}
	if needGated {
		if err := l.gatedRepositories(ctx, qs); err != nil {
			return l, err
		}
	}
	if len(names) > 0 {
		rows, err := qs.BastionPrincipalIDsByName(ctx, names)
		if err != nil {
			return l, err
		}
		for _, r := range rows {
			l.ids[r.Name] = append(l.ids[r.Name], r.ID)
		}
	}
	return l, nil
}

// gatedRepositories looks up the repositories whose signature gate is on now and the virtual repositories above them, in that order.
func (l *lookups) gatedRepositories(ctx context.Context, qs *q.Queries) error {
	gated, err := qs.BastionGatedRepositories(ctx)
	if err != nil || len(gated) == 0 {
		return err
	}
	closure, err := qs.BastionVirtualClosure(ctx, gated)
	if err != nil {
		return err
	}
	l.gated = slices.Clone(gated)
	for _, n := range closure {
		if !slices.Contains(l.gated, n) {
			l.gated = append(l.gated, n)
		}
	}
	return nil
}

// blobHolders looks up the repositories that hold a blob now and the virtual repositories above them, in that order.
func (l lookups) blobHolders(ctx context.Context, qs *q.Queries, digest string) error {
	raw, _ := hex.DecodeString(digest) // mapBlob let only 64 hexadecimal digits through
	holders, err := qs.BastionBlobHolders(ctx, raw)
	if err != nil || len(holders) == 0 {
		return err
	}
	closure, err := qs.BastionVirtualClosure(ctx, holders)
	if err != nil {
		return err
	}
	out := slices.Clone(holders)
	for _, n := range closure {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	l.blobs[digest] = out
	return nil
}

// memberName is the name a member reference of a membership event has in the principal table: a service account
// is referenced as "sa:<name>", a user by its name, which holds no colon.
func memberName(ref string) string { return strings.TrimPrefix(ref, "sa:") }

// entries builds the wire entries of one event: the first has the event's ID, the others "<ID>:1", "<ID>:2".
func (l lookups) entries(ev FeedEvent, es []rawEntry) []iface.FeedEntry {
	var out []iface.FeedEntry
	for _, e := range es {
		if e.digest != "" && len(l.blobs[e.digest]) == 0 {
			continue // no repository holds the blob any more: no Bastion keeps an answer of it
		}
		if e.gated && len(l.gated) == 0 {
			continue // no repository's gate is on: the change refuses nothing a Bastion keeps
		}
		fe := iface.FeedEntry{ID: ev.ID, At: ev.At, Kind: e.kind}
		if n := len(out); n > 0 {
			fe.ID = ev.ID + ":" + strconv.Itoa(n)
		}
		switch e.kind {
		case iface.FeedToken:
			fe.Tokens = e.tokens
		case iface.FeedPrincipal:
			fe.Principals = e.principals
			if len(e.names) > 0 {
				for _, n := range e.names {
					for _, id := range l.ids[memberName(n)] {
						if !slices.Contains(fe.Principals, id) {
							fe.Principals = append(fe.Principals, id)
						}
					}
				}
				if len(fe.Principals) == 0 {
					// Members that cannot be named by ID any more: every answer is dropped instead.
					fe.Kind = iface.FeedGrants
				}
			}
		case iface.FeedContent:
			fe.Repositories = l.closure[e.repository]
			if e.digest != "" {
				fe.Repositories = l.blobs[e.digest]
			}
			if e.gated {
				fe.Repositories = l.gated
			}
		}
		out = append(out, fe)
	}
	return out
}
