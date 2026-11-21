package formattest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
)

// CheckSignatures is check 18 (signatures.md R-SIG-022): the format declares its signatures, every kind it declares has a signed
// sample that Detect names with its subject and locator, committing a sample raises no version row that names the signature, and a
// format that declares none detects nothing over every content case.
const CheckSignatures Check = 18

// SignatureCase is a signed sample of one kind a format declares (check 18). The sample is stored at Path: by Commit, through the
// in-memory host the way a client or a remote's fill stores it (an upload route, Ingest().Commit); or, for a document a remote's fill
// stores and no item holds, as Body. The kit builds the event from what was stored and asks Detect.
type SignatureCase struct {
	// Kind is the Name of the declared kind the sample is of.
	Kind string
	// Path is the canonical path of the stored sample.
	Path string
	// Commit stores the sample; nil for a remote's document, which Body holds.
	Commit func(h *Host) error
	// Body is the stored document of a remote (Commit nil), and MediaType its type.
	Body      []byte
	MediaType string
	// Class is the class of the repository the sample is stored in; "" is local for Commit and remote for Body.
	Class formats.Class
	// Want are the candidates Detect must name, each matched by one candidate with the same source, location, part, role, subject
	// (Subject, SubjectPath, SubjectName) and object (Path, Blob, Locator, SHA256 when set, Raw when set); SchemeHint is compared
	// when set.
	Want []formats.SignatureCandidate
	// RemoveSubject, for a format whose signature follows its subject (OCI's referrers and attachment tags, signatures.md R-SIG-025),
	// removes the subject of a committed sample the way a client does; the kit then holds that nothing is stored at Path any more.
	// nil for a signature that stays when its subject goes (a generic file's sibling, a document's entry).
	RemoveSubject func(h *Host) error
}

// ContentOf serves bytes as the content of a [formats.SignatureEvent], for the kit and a format's own tests of its detector.
func ContentOf(b []byte) formats.SignatureContent { return byteContent(b) }

type byteContent []byte

func (b byteContent) Size() int64 { return int64(len(b)) }

func (b byteContent) Bytes(max int64) ([]byte, error) {
	if int64(len(b)) > max {
		return nil, fmt.Errorf("the content is %d bytes, more than %d", len(b), max)
	}
	return b, nil
}

func (b byteContent) ReaderAt() (io.ReaderAt, error) { return bytes.NewReader(b), nil }

// StoredVersion is a live version of the in-memory host with the path of its lead item.
type StoredVersion struct {
	Package, Version, LeadPath string
}

// StoredVersions returns the live versions of the host's packages, for check 18 and a format's own tests.
func (h *Host) StoredVersions() []StoredVersion {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []StoredVersion
	for _, v := range h.vers {
		if v.trashed || v.deleted {
			continue
		}
		sv := StoredVersion{Version: v.version}
		if p, ok := h.pkgByID[v.pkg]; ok {
			sv.Package = p.name
		}
		if it, ok := h.byID[v.lead]; ok {
			sv.LeadPath = it.ref.Path
		}
		out = append(out, sv)
	}
	return out
}

// LiveItems returns the live files below a path prefix with the SHA-256 of their content: what a format's data port twin reads where
// the PostgreSQL port reads the items table (OCI's tags that point at a manifest).
func (h *Host) LiveItems(prefix string) map[string][32]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string][32]byte{}
	for p, rec := range h.items {
		if rec.removed || !strings.HasPrefix(p, prefix) {
			continue
		}
		var d [32]byte
		copy(d[:], rec.digests.SHA256)
		out[p] = d
	}
	return out
}

// SignatureEventOf builds the event the core hands a detector for the item stored at p, or false when nothing is stored there.
func (h *Host) SignatureEventOf(p string, origin formats.Origin) (formats.SignatureEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec, ok := h.items[p]
	if !ok || rec.removed {
		return formats.SignatureEvent{}, false
	}
	return formats.SignatureEvent{
		Origin: origin, Repo: h.ref, Item: rec.ref, Path: formats.Path{Path: p}, MediaType: rec.media, Digests: rec.digests,
		Content: ContentOf(rec.data),
	}, true
}

// signatures is check 18.
func (r *run) signatures() {
	sa := r.f.Signatures()
	if sa == nil {
		r.failf("Signatures() returns nil: every format declares its signatures or why it has none (R-SIG-022)")
		return
	}
	decl := sa.Declaration()
	if decl.None != "" {
		r.signaturesNone(sa)
		return
	}
	cases := map[string]int{}
	for _, c := range r.fx.Signatures {
		cases[c.Kind]++
	}
	for _, k := range decl.Kinds {
		if cases[k.Name] == 0 {
			r.failf("the declared signature kind %q has no case in Fixture.Signatures", k.Name)
		}
	}
	known := map[string]bool{}
	for _, k := range decl.Kinds {
		known[k.Name] = true
	}
	for _, c := range r.fx.Signatures {
		if !known[c.Kind] {
			r.failf("signature case %q names no declared kind", c.Kind)
			continue
		}
		r.signatureCase(sa, c)
	}
}

// signatureCase stores one sample, asks Detect and compares what it names with the case; then holds that no version names it.
func (r *run) signatureCase(sa formats.SignatureAdapter, c SignatureCase) {
	class := c.Class
	if class == "" {
		class = formats.Local
		if c.Commit == nil {
			class = formats.Remote
		}
	}
	h := r.host(class)
	var ev formats.SignatureEvent
	if c.Commit != nil {
		if err := c.Commit(h); err != nil {
			r.failf("signature case %q: storing the sample fails: %v", c.Kind, err)
			return
		}
		var ok bool
		if ev, ok = h.SignatureEventOf(c.Path, formats.OriginUpload); !ok {
			r.failf("signature case %q: nothing is stored at %q after the sample was committed", c.Kind, c.Path)
			return
		}
		if class == formats.Remote {
			ev.Origin = formats.OriginFill
		}
		// A signature embedded in its subject (NuGet's .signature.p7s) is carried by the version's own file, which is a version for that.
		for _, v := range h.StoredVersions() {
			for _, w := range c.Want {
				if w.Source == formats.SignatureSourceEmbedded {
					continue
				}
				if loc, _, _ := strings.Cut(w.Location, "#"); v.LeadPath == loc || v.LeadPath == c.Path {
					r.failf("signature case %q: committing the sample made the version %s %s of the signature at %q (R-SIG-022: a signature is never a version)",
						c.Kind, v.Package, v.Version, v.LeadPath)
				}
			}
		}
	} else {
		ev = formats.SignatureEvent{
			Origin: formats.OriginFill, Repo: h.Ref(), Path: formats.Path{Path: c.Path}, MediaType: c.MediaType,
			Digests: digests(c.Body), Content: ContentOf(c.Body),
		}
	}
	got, err := sa.Detect(context.Background(), ev)
	if err != nil {
		r.failf("signature case %q: Detect fails: %v", c.Kind, err)
		return
	}
	r.signatureFollows(h, c)
	if len(c.Want) == 0 {
		r.failf("signature case %q wants no candidate", c.Kind)
	}
	for _, w := range c.Want {
		if !anyCandidate(got, w) {
			r.failf("signature case %q: Detect names no candidate like %s; it named %s", c.Kind, describeCandidate(w), describeCandidates(got))
		}
	}
	for _, g := range got {
		switch {
		case !formats.SignatureRoleValid(g.Role):
			r.failf("signature case %q: a candidate has the role %q", c.Kind, g.Role)
		case !formats.SignatureSourceValid(g.Source):
			r.failf("signature case %q: a candidate has the source %q", c.Kind, g.Source)
		case g.SchemeHint != "" && !formats.SignatureSchemeValid(g.SchemeHint):
			r.failf("signature case %q: a candidate has the scheme %q", c.Kind, g.SchemeHint)
		case g.Location == "":
			r.failf("signature case %q: a candidate has no location", c.Kind)
		case g.Subject == [32]byte{} && g.SubjectPath == "":
			r.failf("signature case %q: a candidate names no subject", c.Kind)
		case len(g.Object.Raw) > formats.MaxSignatureRaw:
			r.failf("signature case %q: a candidate keeps %d bytes, more than %d", c.Kind, len(g.Object.Raw), formats.MaxSignatureRaw)
		}
	}
}

// signatureFollows removes the subject of a committed sample whose case says the signature follows it, and holds that its carrier went
// with it (R-SIG-025).
func (r *run) signatureFollows(h *Host, c SignatureCase) {
	if c.RemoveSubject == nil || c.Commit == nil {
		return
	}
	if err := c.RemoveSubject(h); err != nil {
		r.failf("signature case %q: removing the subject fails: %v", c.Kind, err)
		return
	}
	if _, ok := h.SignatureEventOf(c.Path, formats.OriginUpload); ok {
		r.failf("signature case %q: the subject was removed and the signature at %q stayed (R-SIG-025: a signature follows its subject)", c.Kind, c.Path)
	}
}

// signaturesNone holds a format without signatures to detecting nothing over every content case it serves.
func (r *run) signaturesNone(sa formats.SignatureAdapter) {
	if len(r.fx.Signatures) > 0 {
		r.failf("the format declares no signatures (%s) and its fixture has signature cases", sa.Declaration().None)
	}
	for _, c := range r.fx.Content {
		h := r.host("")
		if c.Seed != nil {
			if err := c.Seed(h); err != nil {
				continue
			}
		}
		h.mu.Lock()
		paths := make([]string, 0, len(h.items))
		for p := range h.items {
			paths = append(paths, p)
		}
		h.mu.Unlock()
		for _, p := range paths {
			ev, ok := h.SignatureEventOf(p, formats.OriginUpload)
			if !ok {
				continue
			}
			if got, _ := sa.Detect(context.Background(), ev); len(got) > 0 {
				r.failf("the format declares no signatures and Detect names %d at %q", len(got), p)
			}
		}
	}
}

func anyCandidate(got []formats.SignatureCandidate, w formats.SignatureCandidate) bool {
	for _, g := range got {
		if sameCandidate(g, w) {
			return true
		}
	}
	return false
}

func sameCandidate(g, w formats.SignatureCandidate) bool {
	return g.Source == w.Source && g.Location == w.Location && g.Part == w.Part && g.Role == w.Role && g.Subject == w.Subject &&
		g.SubjectPath == w.SubjectPath && g.SubjectName == w.SubjectName && (w.SchemeHint == "" || g.SchemeHint == w.SchemeHint) &&
		sameLocator(g.Object, w.Object) && sameLocator(g.SignedContent, w.SignedContent)
}

func sameLocator(g, w formats.SignatureLocator) bool {
	return g.Path == w.Path && g.Blob == w.Blob && g.Locator == w.Locator && (w.SHA256 == [32]byte{} || g.SHA256 == w.SHA256) &&
		(w.Raw == nil || bytes.Equal(g.Raw, w.Raw))
}

func describeCandidate(c formats.SignatureCandidate) string {
	return fmt.Sprintf("{%s %s part %d %s subject %x%s%s object %s}", c.Source, c.Location, c.Part, c.Role, c.Subject[:4], pathOr(" path ", c.SubjectPath),
		pathOr(" name ", c.SubjectName), describeLocator(c.Object))
}

func describeLocator(l formats.SignatureLocator) string {
	switch {
	case l.Path != "":
		return "path " + l.Path
	case l.Raw != nil:
		return fmt.Sprintf("%d raw bytes", len(l.Raw))
	}
	return fmt.Sprintf("blob %x %s", l.Blob[:4], l.Locator)
}

func describeCandidates(cs []formats.SignatureCandidate) string {
	if len(cs) == 0 {
		return "none"
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = describeCandidate(c)
	}
	return strings.Join(parts, ", ")
}

func pathOr(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}
