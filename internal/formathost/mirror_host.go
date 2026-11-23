package formathost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/repository"
)

// MirrorEngine is the remote engine as the mirror's host uses it: the engine of the client path and the two background entry
// points of a pass; *proxy.Engine implements it.
type MirrorEngine interface {
	RemoteEngine
	Fill(ctx context.Context, rq proxy.Request) (proxy.FillOutcome, error)
	Get(ctx context.Context, g proxy.Get) (*proxy.GetResult, error)
	// MarkBackgroundDownloaded records that a probe's background download stored the content of a document (R-REPO-065).
	MarkBackgroundDownloaded(ctx context.Context, repoID int64, path string, digest [32]byte) (bool, error)
}

var _ MirrorEngine = (*proxy.Engine)(nil)

// MirrorHost implements [mirror.Host] (Q-ARCH-288, docs/developer/mirror.md): what the mirror engine needs of the format host and
// the remote engine for the passes of one process. Every request it sends is a background request of the pass: it takes a
// background slot within the pass's ceiling, a client fetch can pre-empt it, its outcomes neither move nor reset the circuit,
// and its upstream log record carries flight_role mirror and the pass's job (R-REPO-053 to R-REPO-057).
//
// A format's own requests during a pass (its gate revalidating a package document, its lister reading a document) are background
// requests too: the context of the pass carries the pass, and the client path's wrapper over the engine builds a background request
// from it (Q-ARCH-311). The Host a format is handed is the format host itself.
type MirrorHost struct {
	h *Host
	e MirrorEngine
}

var _ mirror.Host = (*MirrorHost)(nil)

// NewMirrorHost builds the host of a mirror's passes over the format host and the remote engine.
func NewMirrorHost(h *Host, e MirrorEngine) (*MirrorHost, error) {
	switch {
	case h == nil:
		return nil, errs.E(errs.Internal, errs.CodeInternal, "formathost: the mirror host needs the format host")
	case e == nil:
		return nil, errs.E(errs.Internal, errs.CodeInternal, "formathost: the mirror host needs the remote engine")
	}
	return &MirrorHost{h: h, e: e}, nil
}

// mirrorSubject is the system as the request a format's gate is handed for a background fill names it (R-REPO-057).
var mirrorSubject = authz.MirrorSubject()

// filler is the system work a background request's fills are recorded for (R-AUTHZ-008): a mirror's pass (R-REPO-057), a probe's
// background download (R-REPO-065) or the image scanning's database task (vulnerability-scanning.md R-VULN-044).
type filler int

const (
	fillMirror filler = iota
	fillBackgroundDownload
	fillImageDatabase
)

// who is the actor, the subject and the reason the fill transaction records for the work.
func (f filler) who() (audit.Actor, authz.Subject, string) {
	switch f {
	case fillBackgroundDownload:
		return authz.BackgroundDownloadActor(), authz.BackgroundDownloadSubject(), authz.ReasonBackgroundDownload
	case fillImageDatabase:
		return authz.ImageDatabaseActor(), authz.ImageDatabaseSubject(), authz.ReasonImageDatabase
	}
	return authz.MirrorActor(), mirrorSubject, authz.ReasonMirror
}

// Lister implements [mirror.Host]: the discovery of the format's remote adapter, found through the capabilities Register read.
// Ready implements [mirror.Host]: a built host serves passes until its engine drains; a process that is shutting down starts no
// pass (an error wrapping mirror.ErrShuttingDown).
func (m *MirrorHost) Ready(context.Context) error {
	if d, ok := m.e.(interface{ Draining() bool }); ok && d.Draining() {
		return fmt.Errorf("%w: the remote engine is draining", mirror.ErrShuttingDown)
	}
	return nil
}

func (m *MirrorHost) Lister(format string) (discover.Lister, bool) {
	f, ok := m.h.o.Formats(format)
	if !ok || f.Remote() == nil {
		return nil, false
	}
	l := formats.RemoteCapabilities(formats.ID(format)).Mirror
	return l, l != nil
}

// Admits implements [mirror.Host]. A name is admitted when the repository's include and exclude patterns admit it (R-REPO-005),
// no claim refuses it at the remote (R-REPO-023, R-REPO-025 step 2: a global claim, and a name the format cannot fold) and the
// format has a document for it (the format's name rule). The claims are read through the routing's snapshot index; an error
// there is answered by the first call, so a pass fails at its start rather than skipping names.
func (m *MirrorHost) Admits(ctx context.Context, repo repository.Repository) (func(name string) bool, error) {
	f, ok := m.h.o.Formats(repo.Format)
	if !ok || f.Remote() == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The format of the repository has no remote adapter.")
	}
	refuse := m.h.o.Refusal
	if refuse != nil {
		if _, err := refuse.RemoteRefused(ctx, repo, "probe", nil); err != nil {
			return nil, err
		}
	}
	byName := f.Descriptor().PatternSubject == formats.SubjectName
	hasDocument := formats.RemoteCapabilities(formats.ID(repo.Format)).Names == nil
	scope, err := mirrorScope(repo)
	if err != nil {
		return nil, err
	}
	set := repo.Delivery()
	return func(name string) bool {
		if name == "" {
			return false
		}
		if byName && !repo.Admits(name, false) {
			return false
		}
		// The mirror's scope and the delivery rules (remote-curation.md R-CUR-017): a name the scope does not cover, or of which the
		// rules deliver no version, is never asked for.
		if !scope(name) || !set.Package(name).Delivered {
			return false
		}
		// A format whose upstream has no document per package (an APT archive, which answers the shadowing check through its NameServer)
		// has no document path to test; its lister returns names its own name rule accepted.
		if hasDocument {
			if _, ok := f.PackageDocumentPath(name); !ok {
				return false
			}
		}
		if _, ok := catalog.Default().NormalizeName(repo.Format, name); !ok {
			return false
		}
		if refuse != nil {
			if refused, err := refuse.RemoteRefused(ctx, repo, name, nil); err != nil || refused {
				return false // an index that cannot be read refuses: nothing is asked of the upstream for a name that cannot be checked
			}
		}
		return true
	}, nil
}

// Upstream implements [mirror.Host].
func (m *MirrorHost) Upstream(repo repository.Repository, pass mirror.Pass) (discover.Upstream, error) {
	lister, ok := m.Lister(repo.Format)
	if !ok {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The format of the repository does not discover.")
	}
	rem, err := proxy.NewRemote(repo)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The repository is not a usable remote repository.", errs.WithCause(err))
	}
	return m.newPassUpstream(repo, rem, pass, lister.MirrorHosts)
}

// newPassUpstream builds the upstream of a pass, or of a background run (Host.BackgroundUpstream); hostsOf names the hosts besides
// the upstream's that may be reached for the upstream URL (discover.Lister.MirrorHosts), nil for none.
func (m *MirrorHost) newPassUpstream(repo repository.Repository, rem proxy.Remote, pass mirror.Pass, hostsOf func(*url.URL) []string) (*passUpstream, error) {
	base, err := url.Parse(rem.Settings.URL)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The upstream URL of the repository does not parse.", errs.WithCause(err))
	}
	var hosts []string
	if hostsOf != nil {
		hosts = hostsOf(base)
	}
	return &passUpstream{
		m: m, repo: repo, rem: rem, pass: pass, base: base, hosts: hosts,
		settings: formatSettings(repo.Config, formats.ID(repo.Format)), since: time.Now(),
	}, nil
}

// Fill implements [mirror.Host]: the artifact goes through the format's gate and the remote engine as a background request.
func (m *MirrorHost) Fill(ctx context.Context, repo repository.Repository, pass mirror.Pass, item discover.Item) (mirror.FillResult, error) {
	f, err := m.fillPath(ctx, repo, pass, formats.Path{Path: item.Path})
	return f.res, err
}

// filled is the outcome of fillPath: the result, and the requests that read the artifact back (Artifact).
type filled struct {
	res  mirror.FillResult
	ctx  context.Context
	rq   *formats.Request
	preq proxy.Request
}

// fillPath stores the artifact at a path of the repository through the format's gate and the remote engine as a background request of
// the pass. An error is as mirror.Host documents it: mapFill's and mapGate's.
func (m *MirrorHost) fillPath(ctx context.Context, repo repository.Repository, pass mirror.Pass, p formats.Path) (filled, error) {
	bf, ok := backgroundFrom(ctx)
	if !ok || bf.h != m.h {
		bf = &backgroundFacts{h: m.h, jobID: pass.JobID, ceiling: pass.Ceiling}
		ctx = withBackground(ctx, bf)
	}
	fetch := &remote{h: m.h, e: m.e}
	rq := m.request(ctx, repo, p)
	if !m.admitted(repo, p) {
		// The repository's include and exclude patterns hold for a background fill as for a client's fetch, which the dispatcher
		// and Host.Authorize refuse before the engine is asked (R-REPO-005, R-REPO-065).
		return filled{}, refused(mirror.RefusedPatterns, errs.E(errs.NotFound, errs.CodeNotFound,
			"The patterns or the delivery rules of the repository do not admit "+p.Path+"."))
	}
	preq, err := fetch.request(ctx, rq, p, "", proxy.Artifact)
	if err != nil {
		return filled{}, err
	}
	out := filled{ctx: ctx, rq: rq, preq: preq}
	// A stored artifact costs no upstream request, and no gate: the gate may revalidate the format's document.
	if _, cached, err := m.e.Cached(ctx, preq); err != nil && !proxy.IsLost(err) { // a lost item is filled again (Q-REPO-202)
		return filled{}, mapBackground(err)
	} else if cached {
		out.res = mirror.FillResult{Present: true}
		return out, nil
	}
	expected, err := fetch.gate(ctx, rq, p)
	if err != nil {
		return filled{}, mapGate(err)
	}
	out.preq.Expected = proxy.Digests{SHA256: expected.SHA256, SHA512: expected.SHA512, SHA1: expected.SHA1}
	fo, err := m.e.Fill(ctx, out.preq)
	if err != nil {
		return filled{}, mapFill(err)
	}
	out.res = mirror.FillResult{Present: fo.Present, Bytes: fo.Bytes}
	return out, nil
}

// mirrorScope compiles remote.mirror.include (remote-curation.md R-CUR-017): a name is in scope when a pattern matches it; an empty
// scope is everything the rules deliver. A stored pattern that does not compile was refused by the validation, so it is an error.
func mirrorScope(repo repository.Repository) (func(string) bool, error) {
	rs, _ := repo.Remote()
	if len(rs.Mirror.Include) == 0 {
		return func(string) bool { return true }, nil
	}
	ps := make([]*repository.Pattern, 0, len(rs.Mirror.Include))
	for _, s := range rs.Mirror.Include {
		p, err := repository.CompilePattern(repository.SubjectName, s)
		if err != nil {
			return nil, errs.E(errs.Internal, errs.CodeInternal, "A pattern of the mirror's scope does not compile.", errs.WithCause(err))
		}
		ps = append(ps, p)
	}
	return func(name string) bool {
		for _, p := range ps {
			if p.Match(name) {
				return true
			}
		}
		return false
	}, nil
}

// admitted reports whether the repository's include and exclude patterns admit the artifact at p: its package name for a format whose
// patterns match names, else its path (R-REPO-005), and whether the delivery rules deliver its version: a pass of the mirror fetches
// only what the rules deliver (remote-curation.md R-CUR-017).
// A format whose items have a second spelling (an official image of Docker Hub, `library/nginx` stored, `nginx` typed and written in the
// patterns, Q-OCI-129) is admitted when either spelling is, as the names a pass discovers are.
func (m *MirrorHost) admitted(repo repository.Repository, p formats.Path) bool {
	f, ok := m.h.o.Formats(repo.Format)
	if !ok || !patternsAdmit(repo, f, p) {
		return false
	}
	set := repo.Delivery()
	if set == nil {
		return true
	}
	info := f.Classify(p)
	switch {
	case info.Package == "":
		return true
	case info.Version == "":
		return set.Package(info.Package).Delivered
	}
	return set.Decide(info.Package, info.Version).Delivered
}

// patternsAdmit reports whether the repository's include and exclude patterns admit the item at p, in either of its spellings.
func patternsAdmit(repo repository.Repository, f formats.Format, p formats.Path) bool {
	one := func(p formats.Path) bool {
		if f.Descriptor().PatternSubject == formats.SubjectName {
			name := f.Classify(p).Package
			return name == "" || repo.Admits(name, false)
		}
		return repo.Admits(p.Path, false)
	}
	if one(p) {
		return true
	}
	if sp := formats.CapabilitiesFor(formats.ID(repo.Format)).SourceSpeller; sp != nil {
		if st, ok := repo.Remote(); ok {
			for _, other := range sp.SourceSpellings(st.URL, p) {
				if one(other) {
					return true
				}
			}
		}
	}
	return false
}

// request is the request a format's gate is handed for a path of a pass: no client behind it, no route, the system as subject. Its
// Host is the format host itself, so that the format finds its per-replica state; the requests the gate makes through
// Host.Remote() are background requests because the context carries the pass ([withBackground]).
func (m *MirrorHost) request(ctx context.Context, repo repository.Repository, p formats.Path) *formats.Request {
	hr := (&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, Header: http.Header{}}).WithContext(ctx)
	return &formats.Request{
		Request: hr, Repo: &repo, Route: &formats.Route{Name: "mirror.pass"}, Path: p, Subject: mirrorSubject,
		Host: m.h, Log: m.h.o.Log,
	}
}

// backgroundFacts are what makes a request of the client path's wrapper a background request of a pass, of a probe's background
// download (R-REPO-065: then charge is the probe's, limits.Engine.Resume, and the fills are recorded for system:background-download) or
// of the image scanning's database task (R-VULN-044: no charge, so the engine records the system's, and the fills are recorded for
// system:image-database).
type backgroundFacts struct {
	h       *Host
	jobID   string
	ceiling int
	charge  *limits.Charge
	fill    filler
}

type backgroundKey struct{}

// withBackground marks ctx as the context of a pass's work. The key is unexported: no request of the client path carries it, and
// a format cannot set it.
func withBackground(ctx context.Context, b *backgroundFacts) context.Context {
	return context.WithValue(ctx, backgroundKey{}, b)
}

func backgroundFrom(ctx context.Context) (*backgroundFacts, bool) {
	b, ok := ctx.Value(backgroundKey{}).(*backgroundFacts)
	return b, ok
}

// apply makes preq a background request of the pass: the priority and the pass's facts, the system as caller (the upstream log
// record has no on_behalf_of) and the populate check that is the mirror's: it asks nobody, and the event of the fill names the
// system as actor and mirror as reason (R-REPO-057, Q-REPO-145).
func (b *backgroundFacts) apply(preq *proxy.Request, rq *formats.Request, info formats.PathInfo, p formats.Path) {
	preq.Priority, preq.Ceiling, preq.JobID, preq.Mode = proxy.PriorityBackground, b.ceiling, b.jobID, proxy.Normal
	preq.Charge = b.charge
	actor, subject, reason := b.fill.who()
	preq.Caller = proxy.CallerFacts{Actor: actor}
	preq.Populate = &mirrorPopulate{h: b.h, subject: subject, reason: reason, coord: authz.Coordinate{
		Repository: rq.Repo.Name, Class: rq.Repo.Class, Format: rq.Repo.Format, Kind: authz.KindWrite,
		Path: p.Path, Package: info.Package, Version: info.Version, Actions: []formats.Action{formats.ActionRead},
		Addressed: rq.Addressed(),
	}}
}

// mirrorPopulate is the populate check of a pass's request: the system fills the cache, whatever the repository's cacheFillAction
// says of clients, and the fill transaction records the populate event with the system as actor and mirror as reason. A probe's
// background download records system:background-download and its reason: the populate check was the probe caller's, asked when
// the probe was answered (R-REPO-065).
type mirrorPopulate struct {
	h       *Host
	subject authz.Subject
	reason  string
	coord   authz.Coordinate
}

var _ proxy.Populate = (*mirrorPopulate)(nil)

// May implements [proxy.Populate].
func (*mirrorPopulate) May(context.Context) bool { return true }

// Deny implements [proxy.Populate]; a pass is never refused.
func (*mirrorPopulate) Deny(context.Context) error { return nil }

// Record implements [proxy.Populate]: authz.write.allowed, as a client's fill writes it (R-AUTHZ-008, R-AUTHZ-029).
func (p *mirrorPopulate) Record(ctx context.Context, tx meta.Tx) error {
	return p.h.o.Decider.RecordAllowed(ctx, tx, p.subject, p.coord, authz.Decision{Outcome: authz.Allow, Reason: p.reason})
}

// Endpoint implements [proxy.Populate]: no endpoint, and one that fills.
func (*mirrorPopulate) Endpoint() (string, bool) { return "", true }

// passUpstream is the [discover.Upstream] a format is lent for one pass.
type passUpstream struct {
	m        *MirrorHost
	repo     repository.Repository
	rem      proxy.Remote
	pass     mirror.Pass
	base     *url.URL
	hosts    []string
	settings json.RawMessage
	// since is when this attempt of the pass began: a stored document that was validated before it is not current for the
	// pass's discovery, whatever its period says (see Document).
	since time.Time
	// charge and fill make the requests those of a probe's background download (R-REPO-065: charged to the probe's budget key and
	// recorded for system:background-download) or of the image scanning's database task (R-VULN-044).
	charge *limits.Charge
	fill   filler
}

// facts are the background facts of the upstream's requests.
func (u *passUpstream) facts() *backgroundFacts {
	return &backgroundFacts{h: u.m.h, jobID: u.pass.JobID, ceiling: u.pass.Ceiling, charge: u.charge, fill: u.fill}
}

var _ discover.Upstream = (*passUpstream)(nil)

// Base implements [discover.Upstream].
func (u *passUpstream) Base() *url.URL { b := *u.base; return &b }

// RepositoryID is the id of the repository the pass or run is for: a format whose lister reads rows of its own needs it, and the
// upstream is all the lister is given.
func (u *passUpstream) RepositoryID() int64 { return u.repo.NumericID() }

// Settings implements [discover.Upstream].
func (u *passUpstream) Settings() json.RawMessage { return u.settings }

var _ discover.Bounded = (*passUpstream)(nil)

// VersionBounds implements [discover.Bounded]: the bounds the repository's mirror settings set when the pass began.
func (u *passUpstream) VersionBounds() (int, bool) {
	return u.rem.Settings.Mirror.MaxVersionsPerPackage, u.rem.Settings.Mirror.IncludePrereleases
}

// Get implements [discover.Upstream] through [proxy.Engine.Get]: the host check, the credential rule and the background slot are
// the engine's; a rate-limit answer is returned as the engine's typed error, which discover.AsRateLimited finds.
func (u *passUpstream) Get(ctx context.Context, target *url.URL, accept string, max int64) (*discover.Response, error) {
	res, err := u.m.e.Get(ctx, proxy.Get{
		Remote: u.rem, URL: target, Accept: accept, Max: max, MirrorHosts: u.hosts, Ceiling: u.pass.Ceiling, JobID: u.pass.JobID,
		Auth:     handshakeOf(formats.RemoteCapabilities(formats.ID(u.repo.Format)), &u.repo, u.settings, nil),
		Exchange: exchangerOf(formats.RemoteCapabilities(formats.ID(u.repo.Format)), &u.repo, u.settings),
	})
	if err != nil {
		return nil, mapBackground(err)
	}
	return &discover.Response{Status: res.Status, Header: res.Header, Body: res.Body}, nil
}

var _ discover.Header = (*passUpstream)(nil)

// Head implements [discover.Header]: a HEAD through [proxy.Engine.Get], with the rules of Get (signatures.md R-SIG-026).
func (u *passUpstream) Head(ctx context.Context, target *url.URL, accept string) (*discover.Response, error) {
	res, err := u.m.e.Get(ctx, proxy.Get{
		Remote: u.rem, URL: target, Method: http.MethodHead, Accept: accept, MirrorHosts: u.hosts, Ceiling: u.pass.Ceiling, JobID: u.pass.JobID,
		Auth:     handshakeOf(formats.RemoteCapabilities(formats.ID(u.repo.Format)), &u.repo, u.settings, nil),
		Exchange: exchangerOf(formats.RemoteCapabilities(formats.ID(u.repo.Format)), &u.repo, u.settings),
	})
	if err != nil {
		return nil, mapBackground(err)
	}
	return &discover.Response{Status: res.Status, Header: res.Header}, nil
}

// Document implements [discover.Upstream]: the metadata document through the remote engine, as a background request
// (R-REPO-025 steps 4 to 10). A document the upstream does not have (a negative entry, 404 or 410), one that is gone and a
// package the repository counts as gone are found=false.
//
// A stored document counts as current only if it was validated after the attempt began: the plan of a package that a feed or a
// listing named has to see what the upstream announced, not a copy that is still fresh by its metadata period but older than the
// change (a client may have pulled the document minutes before). Such a document is revalidated once per attempt, one conditional
// request; an artifact that is stored costs none (R-REPO-051).
func (u *passUpstream) Document(ctx context.Context, path, variant string) ([]byte, bool, error) {
	ctx = withBackground(ctx, u.facts())
	p := formats.Path{Path: path}
	rq := u.m.request(ctx, u.repo, p)
	doc, err := (&remote{h: u.m.h, e: u.m.e}).metadataSince(ctx, rq, p, variant, u.since)
	if err != nil {
		if ee, ok := errs.As(err); ok && ee.Code == errs.CodeNotFound {
			return nil, false, nil
		}
		return nil, false, mapBackground(err)
	}
	if doc == nil || doc.Status.Negative || doc.Bytes == nil || !doc.GoneAt.IsZero() {
		return nil, false, nil
	}
	b, err := doc.Bytes(ctx)
	if err != nil {
		return nil, false, mapBackground(err)
	}
	return b, true, nil
}

// stored is the metadata document of the repository as the cache holds it, however old, with no request to the upstream (the cache
// view of the engine, R-REPO-032): found is false when none is stored, the upstream's 404 is stored as negative, or it is gone. A job
// that judges what is stored (the Debian remote's re-verification of its releases after a change of keys, R-DEB-020) reads it so.
func (u *passUpstream) stored(ctx context.Context, path, variant string) ([]byte, bool, error) {
	p := formats.Path{Path: path}
	rq := u.m.request(ctx, u.repo, p)
	rq.CacheView = true
	doc, err := (&remote{h: u.m.h, e: u.m.e}).metadataSince(ctx, rq, p, variant, time.Time{})
	if err != nil {
		if ee, ok := errs.As(err); ok && ee.Code == errs.CodeNotFound {
			return nil, false, nil
		}
		return nil, false, mapBackground(err)
	}
	if doc == nil || doc.Status.Negative || doc.Bytes == nil || !doc.GoneAt.IsZero() {
		return nil, false, nil
	}
	b, err := doc.Bytes(ctx)
	if err != nil {
		return nil, false, mapBackground(err)
	}
	return b, true, nil
}

// Artifact implements [discover.Upstream]: the artifact at path is filled as Fill fills a plan item (the format's gate, the remote
// engine, a background request of the pass; a stored one costs no request) and read back from the store when it is at most max
// bytes. An artifact the upstream does not have is found=false; one above max is stored and answered found=true with a nil body. What
// the call stored is reported to the package's tally (mirror.ReportStored), so that the pass counts it as an item it stored. Any other
// error is the one Fill would return, and the engine treats it as it treats the error of a plan (mirror.Host).
func (u *passUpstream) Artifact(ctx context.Context, path string, max int64) ([]byte, bool, error) {
	f, err := u.m.fillPath(withBackground(ctx, u.facts()), u.repo, u.pass, formats.Path{Path: path})
	if err != nil {
		var refusal *mirror.RefusedError
		if errors.As(err, &refusal) && refusal.Reason == mirror.RefusedNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	mirror.ReportStored(ctx, f.res)
	it, cached, err := u.m.e.Cached(f.ctx, f.preq)
	if err != nil {
		return nil, false, mapBackground(err)
	}
	if !cached {
		return nil, false, nil
	}
	if it.Size > max {
		return nil, true, nil
	}
	rc, err := u.m.h.Items().Open(f.ctx, itemOf(f.rq.RepoRef(), it))
	if err != nil {
		return nil, false, mapBackground(err)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, false, mapBackground(err)
	}
	if int64(len(body)) > max {
		return nil, true, nil
	}
	return body, true, nil
}

// mapBackground turns the outcomes of a background request into the errors the mirror engine reads (mirror.Host): a
// pre-emption wraps mirror.ErrPreempted, a request the shutdown cancelled wraps mirror.ErrShuttingDown, an open circuit and a
// refused spool admission are a *mirror.WaitError, and a rate-limit answer is left as it is (discover.AsRateLimited finds it). Any
// other error is a failure of the item.
func mapBackground(err error) error {
	var w *proxy.BackgroundWait
	switch {
	case err == nil:
		return nil
	case errors.Is(err, proxy.ErrPreempted):
		return fmt.Errorf("%w: %w", mirror.ErrPreempted, err)
	case errors.As(err, &w):
		return &mirror.WaitError{After: w.After, Reason: w.Reason}
	}
	if ee, ok := errs.As(err); ok && ee.Code == errs.CodeShuttingDown {
		return fmt.Errorf("%w: %w", mirror.ErrShuttingDown, err)
	}
	return err
}

// mapFill is mapBackground for the error of the engine's fill, with the refusals that no retry of the pass changes
// (*mirror.RefusedError, docs/developer/mirror.md "What the host maps"): the upstream does not have the artifact (a negative
// entry, 404, 410, a package marked gone), the vulnerability scanning blocks the version, the populate check refuses.
// A repository that is offline (code not_cached_offline) is none of them: the engine reads the code and stops the pass.
func mapFill(err error) error {
	if m := mapBackground(err); m != err { //nolint:errorlint // identity: mapBackground returns err itself when it maps nothing
		return m
	}
	ee, ok := errs.As(err)
	if !ok {
		return err
	}
	switch ee.Code {
	case errs.CodeUpstreamIntegrityMismatch, errs.CodeUpstreamDigestMissing:
		return &mirror.PermanentError{Cause: err}
	case errs.CodeNotFound:
		return refused(mirror.RefusedNotFound, err)
	case errs.CodeVulnerabilityBlocked, errs.CodeVulnerabilityDataMissing:
		return refused(mirror.RefusedBlocked, err)
	case errs.CodePopulateNotPermitted:
		return refused(mirror.RefusedPopulate, err)
	}
	return err
}

// mapGate is mapBackground for the error of the format's artifact gate: a refusal of the format (not found, forbidden: a removed
// version, a mark) is permanent; the gate's own request failing is the failure of the item that it is.
func mapGate(err error) error {
	if m := mapBackground(err); m != err { //nolint:errorlint // identity, as in mapFill
		return m
	}
	if ee, ok := errs.As(err); ok && ee.Code == errs.CodeUpstreamDigestMissing {
		return &mirror.PermanentError{Cause: err} // the document announces nothing the tarball can be compared with
	}
	if k := errs.KindOf(err); k == errs.NotFound || k == errs.Forbidden {
		if ee, ok := errs.As(err); !ok || ee.Code != errs.CodeNotCachedOffline {
			return refused(mirror.RefusedGate, err)
		}
	}
	return err
}

// refused is a *mirror.RefusedError that keeps the engine's error as its cause.
func refused(reason string, cause error) error {
	return &refusedError{RefusedError: mirror.RefusedError{Reason: reason}, cause: cause}
}

// refusedError carries the cause for errors.Is and errors.As; the engine reads the type *mirror.RefusedError through errors.As.
type refusedError struct {
	mirror.RefusedError
	cause error
}

func (e *refusedError) Unwrap() []error { return []error{&e.RefusedError, e.cause} }
