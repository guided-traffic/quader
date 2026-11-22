package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// What the product manages of a repository of the project upstream (R-REPO-069, Q-REPO-378, Q-REPO-386): every member of its
// document but the ones that narrow what it serves is the product's or one of the operations of R-REPO-073. A merge patch that changes
// one answers 422 validation_failed with managed_by_product at the member, in the one pass of R-REPO-008; a member sent with its stored
// value passes, so a document read and sent back works.

// managedByProduct is the 409 of an operation on a whole object the product manages.
func managedByProduct(detail string) error {
	return errs.E(errs.Conflict, errs.CodeManagedByProduct, detail)
}

// The members an administrator sets with a merge patch (R-REPO-069): the top-level members, and the members of remote. For a system
// repository the include patterns, vulnerabilities, malware and signatures are the product's as well (R-REPO-070, R-REPO-075). The
// spec's remote.offline is remote.availability.mode. On a well-known repository the whole member signatures is an administrator's:
// signatures.fetch switches the background fetch of its upstream's signatures off, for Docker Hub without an account (R-SIG-026,
// R-SIG-040), and onlyVerified and anchors set its gate (R-SIG-061); one rule for the member (Q-SIG-128, Q-SIG-138).
func patchableTop(k string) bool {
	return k == "adminNotes" || k == "includePatterns" || k == "excludePatterns" || k == "vulnerabilities" || k == "malware" || k == "signatures"
}

// systemFixedTop are the members a system repository of upstream keeps at the product's value: its content is the product's, never
// gated or scanned (R-MAL-001, R-SIG-061).
func systemFixedTop(k string) bool {
	return k == "includePatterns" || k == "vulnerabilities" || k == "malware" || k == "signatures"
}

func identityMember(k string) bool {
	return k == "project" || k == "key" || k == "class" || k == "format" || k == "layout"
}

// managedMembers returns the managed_by_product violations of a write of a repository of the project upstream that is not the
// product's: each member of nd the caller may not change and did change. The identity members are checkUpdate's (immutable).
func (s *Service) managedMembers(ctx context.Context, qs *q.Queries, c Caller, id int64, cur, nd *Document) ([]errs.FieldError, error) {
	if c.product.on {
		return nil, nil
	}
	facts, err := upstreamFactsOf(ctx, qs, id)
	if err != nil {
		return nil, err
	}
	return managedPointers(wellknown.Kind(facts[id].Kind), cur, nd)
}

// managedPointers compares the stored document of a repository of the given kind with the new one.
func managedPointers(kind wellknown.Kind, cur, nd *Document) ([]errs.FieldError, error) {
	before, err := documentMembers(cur)
	if err != nil {
		return nil, err
	}
	after, err := documentMembers(nd)
	if err != nil {
		return nil, err
	}
	var out []errs.FieldError
	for _, k := range unionKeys(before, after) {
		switch {
		case identityMember(k):
		case patchableTop(k) && (kind != wellknown.KindSystem || !systemFixedTop(k)):
		case k == "remote":
			out = append(out, managedRemote(before[k], after[k])...)
		case k == "storage" && kind != wellknown.KindSystem:
			// storage.downloadLinks narrows what the repository serves and is an administrator's (R-STOR-014); the rest of
			// storage is the product's.
			out = append(out, managedObject("/storage", orObject(before[k]), orObject(after[k]), storageDownloadLinks)...)
		case HasFormatMember(k):
			out = append(out, managedObject("/"+k, before[k], after[k], "")...)
		case !sameJSON(before[k], after[k]):
			out = append(out, managedField("/"+k))
		}
	}
	return out, nil
}

// managedRemote compares the remote sections: cacheFillAction and availability.mode (offline) are an administrator's, every other
// member the product's.
func managedRemote(before, after json.RawMessage) []errs.FieldError {
	b, a := objectMembers(before), objectMembers(after)
	var out []errs.FieldError
	for _, k := range unionKeys(b, a) {
		switch {
		case k == "cacheFillAction":
		case k == "availability":
			out = append(out, managedObject("/remote/availability", b[k], a[k], "mode")...)
		case !sameJSON(b[k], a[k]):
			out = append(out, managedField("/remote/"+k))
		}
	}
	return out
}

// managedObject compares the members of an object member and reports each changed one but open; a member that is no object is
// compared as a whole.
func managedObject(ptr string, before, after json.RawMessage, open string) []errs.FieldError {
	b, a := objectMembers(before), objectMembers(after)
	if b == nil || a == nil {
		if sameJSON(before, after) {
			return nil
		}
		return []errs.FieldError{managedField(ptr)}
	}
	var out []errs.FieldError
	for _, k := range unionKeys(b, a) {
		if k != open && !sameJSON(b[k], a[k]) {
			out = append(out, managedField(ptr+"/"+k))
		}
	}
	return out
}

func managedField(ptr string) errs.FieldError {
	return errs.FieldError{
		Pointer: ptr, Code: errs.CodeManagedByProduct,
		Detail: "the member is the product's on a repository of the project upstream; an administrator changes the state, the level, the address and the account through /api/v1/upstream-repositories",
	}
}

// documentMembers is the document as its top-level members.
func documentMembers(d *Document) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	err = json.Unmarshal(raw, &m)
	return m, err
}

// objectMembers reads an object's members; nil for none, null or no object.
// orObject is an absent member as the empty object, so that a member added to it compares member by member.
func orObject(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func objectMembers(raw json.RawMessage) map[string]json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(t, &m) != nil {
		return nil
	}
	return m
}

func unionKeys(a, b map[string]json.RawMessage) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sameJSON compares two JSON values by value; an absent member equals null.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if len(bytes.TrimSpace(a)) > 0 && json.Unmarshal(a, &x) != nil {
		return false
	}
	if len(bytes.TrimSpace(b)) > 0 && json.Unmarshal(b, &y) != nil {
		return false
	}
	ca, _ := json.Marshal(x) // a decoded value always marshals
	cb, _ := json.Marshal(y)
	return bytes.Equal(ca, cb)
}

// productHeldPointer is the member the product fixes on scanner-trivy that the validation refuses on every other repository until
// the cleanup that applies it is built (R-REPO-046, R-REPO-070).
const productHeldPointer = "/remote/cache/unusedRetentionHours"

// dropProductHeld takes the not_applicable of remote.cache.unusedRetentionHours out of a validation when the value is the product's:
// a write of the product (cur nil: a create of the reconcile), or a write that leaves the stored value as it is. The value is stored
// and has no effect until the cleanup of R-REPO-046 exists.
func dropProductHeld(v *validation, cur, nd *Document) {
	if cur != nil {
		b, errB := parseStoredRemote(cur.Remote)
		a, errA := parseStoredRemote(nd.Remote)
		if errB != nil || errA != nil || b.Cache.UnusedRetentionHours != a.Cache.UnusedRetentionHours {
			return
		}
	}
	kept := v.fields[:0]
	for _, f := range v.fields {
		if f.Pointer == productHeldPointer && f.Code == errs.CodeNotApplicable {
			continue
		}
		kept = append(kept, f)
	}
	v.fields = kept
}

// applyPresetSettings gives a remote whose url is a preset's the members of the format's settings member the preset carries and the
// document omits, as the preset's hosts are given (R-REPO-062, Q-REPO-379): a Debian remote of https://deb.debian.org/debian/ takes
// the archive keys of Debian.
func applyPresetSettings(info catalog.Entry, d *Document) {
	rs, err := parseStoredRemote(d.Remote)
	if err != nil {
		return
	}
	preset, ok := info.PresetFor(rs.URL)
	if !ok || len(preset.Settings) == 0 {
		return
	}
	member, ok := formatMemberOf(string(info.ID))
	if !ok {
		return
	}
	want := objectMembers(preset.Settings)
	have := objectMembers(member.read(&d.Settings))
	if have == nil {
		have = map[string]json.RawMessage{}
	}
	changed := false
	for k, v := range want {
		if _, set := have[k]; !set {
			have[k], changed = v, true
		}
	}
	if !changed {
		return
	}
	member.write(&d.Settings, remarshalMember(have, member.read(&d.Settings)))
}

// listedBy returns the qualified names of the virtual repositories whose member lists or pin rules name the repository, at most
// 100, and their number (R-REPO-068, Q-REPO-383).
func listedBy(ctx context.Context, tx meta.Tx, id int64) ([]string, int, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, 0, err
	}
	names, err := listedByNames(ctx, qs, id)
	if err != nil {
		return nil, 0, err
	}
	counts, err := listedByCounts(ctx, qs)
	if err != nil {
		return nil, 0, err
	}
	return names, counts[id].virtuals, nil
}

// listedByNames is the qualified names of the virtual repositories that list the repository, in byte order, at most 100: what
// repository.disabled records and an item about one repository answers (Q-REPO-383, Q-API-363).
func listedByNames(ctx context.Context, qs *q.Queries, id int64) ([]string, error) {
	rows, err := qs.UpstreamListedByNames(ctx, q.UpstreamListedByNamesParams{RepositoryID: id, RowLimit: maxListedBy})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.ProjectKey+"/"+r.RepositoryKey)
	}
	return names, nil
}

// maxListedBy is the most names repository.disabled and an item carry.
const maxListedBy = 100

// listedCount is how many virtual repositories list a repository of the project upstream, and in how many projects.
type listedCount struct{ virtuals, projects int }

// listedByCounts is the listing of every repository of the project upstream that a virtual repository lists, in one query.
func listedByCounts(ctx context.Context, qs *q.Queries) (map[int64]listedCount, error) {
	rows, err := qs.CountUpstreamListedBy(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]listedCount, len(rows))
	for _, r := range rows {
		out[r.RepositoryID] = listedCount{virtuals: int(r.ListedBy), projects: int(r.ListedByProjects)}
	}
	return out, nil
}
