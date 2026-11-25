package repository

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// The mirror mode of a remote repository (repositories-and-proxy.md R-REPO-050 to R-REPO-052): remote.mode and the
// members of remote.mirror, validated in the pass that validates the rest of the remote section and stored canonical.

// The bounds of remote.mirror (R-REPO-050).
const (
	minMirrorInterval     = 300
	maxMirrorInterval     = 604800
	minFullPassInterval   = 86400
	maxFullPassInterval   = 7776000
	maxMirrorConcurrency  = 64
	maxVersionsPerPackage = 10000
	minMirrorMaxBytes     = 1 << 20
	maxMirrorMaxBytes     = 1 << 62
)

// WarnMirrorUnbounded is the warning of a mirror of an upstream that is not the format's well-known one, without a
// bounding include list and without maxBytes (R-REPO-052, Q-REPO-132).
const WarnMirrorUnbounded = errs.CodeMirrorUnbounded

// mirror resolves remote.mode and remote.mirror.
func (r *remoteResolver) mirror(s *RemoteSettings, in remoteInput) {
	s.Mode = stringOr(in.Mode, ModePullThrough)
	r.oneOf("/remote/mode", s.Mode, ModePullThrough, ModeMirror)
	var m mirrorInput
	if in.Mirror != nil {
		m = *in.Mirror
	}
	d := DefaultMirrorSettings()
	s.Mirror = MirrorSettings{
		Paused:                  boolOr(m.Paused, d.Paused),
		IntervalSeconds:         intOr(m.IntervalSeconds, d.IntervalSeconds),
		FullPassIntervalSeconds: intOr(m.FullPassIntervalSeconds, d.FullPassIntervalSeconds),
		Discovery:               stringOr(m.Discovery, d.Discovery),
		Concurrency:             intOr(m.Concurrency, d.Concurrency),
		MaxVersionsPerPackage:   intOr(m.MaxVersionsPerPackage, d.MaxVersionsPerPackage),
		IncludePrereleases:      boolOr(m.IncludePrereleases, d.IncludePrereleases),
		MaxBytes:                m.MaxBytes,
		Include:                 r.mirrorInclude(m.Include),
	}
	r.mirrorRanges(s.Mirror)
	if s.Mode != ModeMirror {
		return
	}
	if !r.info.Mirror {
		r.notApplicable("/remote/mode", "the "+string(r.info.ID)+" format cannot discover the packages of its upstream, so a remote repository of it cannot be a mirror")
		return
	}
	r.checkMirrorURL(s)
	r.checkMirrorBound(s)
}

// mirrorInclude validates remote.mirror.include (remote-curation.md R-CUR-017): at most 100 name patterns of the format's pattern
// rules, each folded by the format's name rule as a name it is matched against, so the stored scope is canonical.
func (r *remoteResolver) mirrorInclude(in []string) []string {
	if in == nil {
		return []string{}
	}
	validatePatterns(r.v, r.info.PatternSubject, "/remote/mirror/include", in)
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = p
		if r.info.PatternSubject == SubjectName && r.info.Names != nil {
			if n, ok := r.info.Names.Normalize(p); ok {
				out[i] = n
			}
		}
	}
	return out
}

// delivery resolves remote.delivery (remote-curation.md R-CUR-001, R-CUR-020).
func (r *remoteResolver) delivery(s *RemoteSettings, in remoteInput) {
	var d deliveryInput
	if in.Delivery != nil {
		d = *in.Delivery
	}
	def := DefaultDeliverySettings()
	s.Delivery = DeliverySettings{
		Mode:                  stringOr(d.Mode, def.Mode),
		ExamineOnRequest:      boolOr(d.ExamineOnRequest, def.ExamineOnRequest),
		ExaminedRetentionDays: intOr(d.ExaminedRetentionDays, def.ExaminedRetentionDays),
	}
	r.oneOf("/remote/delivery/mode", s.Delivery.Mode, string(rc.AllExceptBlocked), string(rc.OnlyAllowed))
	r.rng("/remote/delivery/examinedRetentionDays", s.Delivery.ExaminedRetentionDays, 1, maxExaminedRetentionDays)
}

// syncs resolves remote.syncs (remote-curation.md R-CUR-020, R-CUR-027): the interval of five minutes to 30 days, the pause, and the
// byte bound of at least 1 MiB or null.
func (r *remoteResolver) syncs(s *RemoteSettings, in remoteInput) {
	var y syncsInput
	if in.Syncs != nil {
		y = *in.Syncs
	}
	def := DefaultSyncSettings()
	s.Syncs = SyncSettings{
		IntervalSeconds: intOr(y.IntervalSeconds, def.IntervalSeconds), Paused: boolOr(y.Paused, def.Paused), MaxBytes: y.MaxBytes,
	}
	r.rng("/remote/syncs/intervalSeconds", s.Syncs.IntervalSeconds, minSyncInterval, maxSyncInterval)
	if b := s.Syncs.MaxBytes; b != nil && (*b < minMirrorMaxBytes || *b > maxMirrorMaxBytes) {
		r.v.add("/remote/syncs/maxBytes", codeOutOfRange, "must be at least 1 MiB (1048576), at most 2^62, or null for no bound of their own")
	}
}

// checkMirrorURL refuses a query in the url of a mirror (Q-REPO-194): the formats' requests drop it, so a url that
// differs from the well-known upstream by a query would be that upstream without the bound of R-REPO-052.
func (r *remoteResolver) checkMirrorURL(s *RemoteSettings) {
	if u, err := url.Parse(s.URL); err == nil && (u.RawQuery != "" || u.ForceQuery) {
		r.v.add("/remote/url", codeInvalidValue, "a mirror's url carries no query: the format's requests do not send it")
	}
}

func (r *remoteResolver) mirrorRanges(m MirrorSettings) {
	r.rng("/remote/mirror/intervalSeconds", m.IntervalSeconds, minMirrorInterval, maxMirrorInterval)
	if f := m.FullPassIntervalSeconds; f != 0 {
		r.rng("/remote/mirror/fullPassIntervalSeconds", f, minFullPassInterval, maxFullPassInterval)
	}
	r.oneOf("/remote/mirror/discovery", m.Discovery, DiscoveryAuto, DiscoveryFeed, DiscoveryListing, DiscoveryNames)
	r.rng("/remote/mirror/concurrency", m.Concurrency, 1, maxMirrorConcurrency)
	r.rng("/remote/mirror/maxVersionsPerPackage", m.MaxVersionsPerPackage, 0, maxVersionsPerPackage)
	if b := m.MaxBytes; b != nil && (*b < minMirrorMaxBytes || *b > maxMirrorMaxBytes) {
		r.v.add("/remote/mirror/maxBytes", codeOutOfRange, "must be at least 1 MiB (1048576), at most 2^62, or null for no bound of its own")
	}
}

// checkMirrorBound applies R-REPO-052: a mirror of one of the format's presets, each a public registry of unbounded size
// (Q-REPO-323), needs a scope (remote.mirror.include) whose patterns each bound it, or maxBytes; any other upstream without either
// is saved with a warning. The delivery rules are no bound: a Maintainer can allow everything (R-CUR-017).
func (r *remoteResolver) checkMirrorBound(s *RemoteSettings) {
	if bounded(s.Mirror) {
		return
	}
	if known, ok := presetUpstream(r.info, s.URL); ok {
		r.v.add("/remote/mode", errs.CodeMirrorUnbounded,
			"a mirror of "+known+" needs a bound: remote.mirror.include with patterns that each have a literal prefix (such as @types/* or Microsoft.Extensions.*), or remote.mirror.maxBytes")
		return
	}
	r.warn(WarnMirrorUnbounded, "/remote/mode",
		"the mirror has neither a scope (remote.mirror.include) with a literal prefix nor remote.mirror.maxBytes: it stores everything the upstream offers and the rules deliver")
}

// UnboundedMirror reports whether a repository is a mirror that the bound of R-REPO-052 would refuse to save today: a mirror of
// one of its format's presets with neither a bounding scope (remote.mirror.include) nor remote.mirror.maxBytes; preset is that
// upstream's URL. A save checks the bound (checkMirrorBound), but a stored document can lack it when it was saved under an earlier
// rule (a remote's include patterns were a mirror's scope before R-CUR-017), so the mirror's dispatch, its pass and its status ask
// this too (Q-CUR-43). cat nil is the process's catalog; a format the catalog does not know has no presets.
func UnboundedMirror(cat *catalog.Registry, r Repository) (preset string, unbounded bool) {
	s, ok := r.Remote()
	if !ok || !s.Mirrored() || bounded(s.Mirror) {
		return "", false
	}
	info, ok := catalog.Or(cat).Lookup(r.Format)
	if !ok {
		return "", false
	}
	return presetUpstream(info, s.URL)
}

// bounded reports whether a mirror's settings carry a bound of R-REPO-052: maxBytes, or a scope whose patterns each bound it.
func bounded(m MirrorSettings) bool {
	return m.MaxBytes != nil || boundingIncludes(m.Include)
}

// presetUpstream returns the URL of the format's preset that raw names, compared with the one predicate of the bound rule
// (discover.SameUpstream); ok is false for an upstream that is no preset.
func presetUpstream(info catalog.Entry, raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	for _, p := range info.UpstreamPresets {
		if discover.SameUpstream(u, p.URL) {
			return p.URL, true
		}
	}
	return "", false
}

// boundingIncludes reports whether a non-empty include list bounds a mirror: every pattern has a literal prefix
// (discover.Bounding).
func boundingIncludes(include []string) bool {
	if len(include) == 0 {
		return false
	}
	for _, p := range include {
		if !discover.Bounding(p) {
			return false
		}
	}
	return true
}

// checkDelegatedMirror applies Q-REPO-142 to a project Owner's write: the mode and the mirror members stay as
// stored (the defaults on a create), except that paused may change and the mode may go back to pullThrough.
func checkDelegatedMirror(v *validation, stored, next *RemoteSettings) {
	base := RemoteSettings{Mode: ModePullThrough, Mirror: DefaultMirrorSettings()}
	if stored != nil {
		base = *stored
	}
	if next.Mode != base.Mode && next.Mode != ModePullThrough {
		v.add("/remote/mode", errs.CodeMirrorRestricted,
			"only a holder of repositories:write-config makes a remote repository a mirror; a project Owner may turn it back to pullThrough")
	}
	if !sameMirrorButPause(base.Mirror, next.Mirror) {
		v.add("/remote/mirror", errs.CodeMirrorRestricted,
			"a project Owner keeps the mirror settings an administrator of Quader chose; it may change paused only")
	}
}

// checkDelegatedMirrorScope applies Q-REPO-193 to a project Owner's update of a repository that is a mirror
// and stays one: where it reads from (remote.url, remote.extraHosts) is the administrator's, as is its scope
// (remote.mirror.include, kept by checkDelegatedMirror with the rest of remote.mirror, R-CUR-017). A write that turns the mode back to
// pullThrough may change them.
func checkDelegatedMirrorScope(v *validation, stored, next *RemoteSettings) {
	if stored == nil || stored.Mode != ModeMirror || next.Mode != ModeMirror {
		return
	}
	const why = "a project Owner keeps what an administrator of Quader chose for a mirror; turn the mode back to pullThrough to change it"
	if stored.URL != next.URL {
		v.add("/remote/url", errs.CodeMirrorRestricted, why)
	}
	if !reflect.DeepEqual(stored.ExtraHosts, next.ExtraHosts) {
		v.add("/remote/extraHosts", errs.CodeMirrorRestricted, why)
	}
}

// checkDelegatedFormatMirror applies Q-OCI-22 to a project Owner's write: what a mirror of the format keeps is the format's member
// `<format>.mirror` (oci.mirror), and it stays as stored, like remote.mirror (the format's defaults on a create), in either mode: an
// Owner who could widen it would decide what a mirror stores, which the rule of Q-REPO-142 reserves to an administrator. cur is the
// stored document, nil on a create.
func checkDelegatedFormatMirror(v *validation, cur, nd *Document) {
	var curMirror json.RawMessage
	if cur != nil {
		curMirror = mirrorMemberOfFormat(cur)
	}
	if !bytes.Equal(curMirror, mirrorMemberOfFormat(nd)) {
		v.add("/"+nd.Format+"/mirror", errs.CodeMirrorRestricted,
			"a project Owner keeps the mirror settings an administrator of Quader chose; the format's mirror member is not its to change")
	}
}

// mirrorMemberOfFormat is the canonical `mirror` member of the settings member of the document's format; nil when there is none.
func mirrorMemberOfFormat(d *Document) json.RawMessage {
	for _, m := range formatMembers() {
		if m.format != d.Format {
			continue
		}
		var doc map[string]json.RawMessage
		if raw := m.read(&d.Settings); len(raw) == 0 || json.Unmarshal(raw, &doc) != nil {
			return nil
		}
		return canonicalObject(doc["mirror"])
	}
	return nil
}

func sameStrings(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func sameMirrorButPause(a, b MirrorSettings) bool {
	sameBytes := (a.MaxBytes == nil) == (b.MaxBytes == nil) && (a.MaxBytes == nil || *a.MaxBytes == *b.MaxBytes)
	sameScope := sameStrings(a.Include, b.Include)
	a.Paused, b.Paused, a.MaxBytes, b.MaxBytes, a.Include, b.Include = false, false, nil, nil, nil, nil
	return sameBytes && sameScope && reflect.DeepEqual(a, b)
}
