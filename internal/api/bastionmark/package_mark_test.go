package bastionmark

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
)

// The marking per package (bastion.md R-BAS-082): the response's package is what the core's notes of the request's decisions
// name (R-BAS-081, here the notes of the rig's handler), and the caller's shape on it is computed at the status line on the
// index of the request's decision. The core's notes themselves are tested in internal/formathost, the image of the registry
// root through the dispatcher in everyformat_package_test.go.

// decide returns notes that record the decisions of a request naming pkgs, in order.
func decide(pkgs ...string) func(context.Context) {
	return func(ctx context.Context) {
		for _, p := range pkgs {
			bastionmain.NotePackage(ctx, p)
		}
	}
}

// TestR_BAS_082_TheMarkingPerPackage: the two headers on a 200 or 304 of a content route whose decisions named one package, with
// the caller's shape on it; the class shared for a document of a package the caller reads in full; no package on a refusal, a 404,
// a pass route, a probe, a response that drew on two packages or on a package-less decision.
func TestR_BAS_082_TheMarkingPerPackage(t *testing.T) {
	const (
		doc  = "/api/mk/web/mk-local/d/left-pad"
		file = "/api/mk/web/mk-local/f/x.bin"
	)
	cases := []struct {
		name         string
		method, path string
		cred         string
		status       int
		repo         iface.Read            // the shape on the repository, before the registry cap
		pkgs         map[string]iface.Read // the decision point's package shapes
		notes        []string
		read, pkgRd  iface.Read
		pkg          string
		class        iface.Class
	}{
		{"a narrowed reader's tarball", "GET", file, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadPartial, iface.ReadFull, "left-pad", iface.ClassArtifact},
		{"a narrowed reader's document is shared", "GET", doc, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadPartial, iface.ReadFull, "left-pad", iface.ClassShared},
		{"a narrowed reader's document validated is shared", "GET", doc, "token", 304, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad", "left-pad"}, iface.ReadPartial, iface.ReadFull, "left-pad", iface.ClassShared},
		{"a HEAD of a narrowed reader's tarball", "HEAD", file, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadPartial, iface.ReadFull, "left-pad", iface.ClassArtifact},
		{"a document of a package the caller reads in part is private", "GET", doc, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadPartial}, []string{"left-pad"}, iface.ReadPartial, iface.ReadPartial, "left-pad", iface.ClassPrivate},
		{"a full reader is full on the package", "GET", doc, "token", 200, iface.ReadFull, nil, []string{"left-pad"}, iface.ReadFull, iface.ReadFull, "left-pad", iface.ClassShared},
		{"a registry token on an image its grant pulls", "GET", doc, "registry", 200, iface.ReadFull, nil, []string{"left-pad"}, iface.ReadPartial, iface.ReadFull, "left-pad", iface.ClassShared},
		{"a registry token on another image is limited to its grant", "GET", doc, "registry", 200, iface.ReadFull, nil, []string{"right-pad"}, iface.ReadPartial, iface.ReadPartial, "right-pad", iface.ClassPrivate},
		{"a registry token whose token reads the package in part", "GET", file, "registry", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadPartial}, []string{"left-pad"}, iface.ReadPartial, iface.ReadPartial, "left-pad", iface.ClassArtifact},
		{"two packages name none", "GET", doc, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull, "right-pad": iface.ReadFull}, []string{"left-pad", "right-pad"}, iface.ReadPartial, "", "", iface.ClassPrivate},
		{"a package-less decision names none", "GET", doc, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad", ""}, iface.ReadPartial, "", "", iface.ClassPrivate},
		{"no decision names none", "GET", doc, "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, nil, iface.ReadPartial, "", "", iface.ClassPrivate},
		{"a name outside the bounds names none", "GET", doc, "token", 200, iface.ReadPartial, map[string]iface.Read{strings.Repeat("x", iface.MaxPackageBytes+1): iface.ReadFull}, []string{strings.Repeat("x", iface.MaxPackageBytes+1)}, iface.ReadPartial, "", "", iface.ClassPrivate},
		{"a 404 carries none", "GET", doc, "token", 404, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadPartial, "", "", iface.ClassPass},
		{"a range carries none", "GET", file, "token", 206, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadPartial, "", "", iface.ClassPass},
		{"a volatile route carries none", "GET", "/api/mk/web/mk-local/s/left-pad", "token", 200, iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadPartial, "", "", iface.ClassPass},
		{"a repository the caller reads none of is partial on the package", "GET", file, "token", 200, iface.ReadNone, map[string]iface.Read{"left-pad": iface.ReadFull}, []string{"left-pad"}, iface.ReadNone, iface.ReadPartial, "left-pad", iface.ClassArtifact},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.shapes.read, r.shapes.pkgs, r.status, r.notes = c.repo, c.pkgs, c.status, decide(c.notes...)
			rec := r.do(c.method, c.path, hRecognize, "forward", hCred, c.cred)
			if rec.Code != c.status {
				t.Fatalf("status %d", rec.Code)
			}
			mk := markingOf(t, rec)
			if mk.Read != c.read || mk.Package != c.pkg || mk.PackageRead != c.pkgRd || mk.Class != c.class {
				t.Fatalf("read %q package %q package read %q class %q; want %q %q %q %q (%v)", mk.Read, mk.Package, mk.PackageRead, mk.Class, c.read, c.pkg, c.pkgRd, c.class, rec.Header())
			}
			// The decision point is asked about the package only for a caller who reads the repository in part, on the index the
			// request decided on.
			asked := c.repo == iface.ReadPartial && c.pkg != ""
			if asked != (len(r.shapes.pkgAsked) == 1) || len(r.shapes.pkgAsked) > 1 {
				t.Fatalf("package shapes asked %v", r.shapes.pkgAsked)
			}
			if asked && (r.shapes.pkgAsked[0] != "web/mk-local|"+c.pkg || len(r.shapes.idx) != 1 || r.shapes.pkgIdx[0] != r.shapes.idx[0] || r.shapes.idx[0] == nil) {
				t.Fatalf("asked %v on %v, the request decided on %v", r.shapes.pkgAsked, r.shapes.pkgIdx, r.shapes.idx)
			}
		})
	}
	t.Run("a refusal of the decision point carries no package", func(t *testing.T) {
		r := newRig(t)
		r.decider.outcome = authz.DenyHidden
		r.notes = decide("left-pad")
		rec := r.do("GET", doc, hRecognize, "forward", hCred, "token")
		if rec.Code != 404 || rec.Header().Get(iface.HeaderPackage) != "" || rec.Header().Get(iface.HeaderPackageRead) != "" {
			t.Fatalf("%d %v", rec.Code, rec.Header())
		}
	})
	t.Run("a registry token's grant pinned to one manifest holds no whole image", func(t *testing.T) {
		g := registryGrant()
		g.Digest = "sha256:" + strings.Repeat("a", 64)
		if g.PullsImage(qr("mk-local"), "left-pad") || !registryGrant().PullsImage(qr("mk-local"), "left-pad") || registryGrant().PullsImage(qr("other"), "left-pad") {
			t.Fatal("PullsImage")
		}
		r := newRig(t)
		if got := r.shapes; got == nil {
			t.Fatal("no resolver")
		}
		m := New(Options{Shapes: r.shapes})
		if got := m.packageRead(iface.ReadFull, nil, g, qr("mk-local"), "left-pad"); got != iface.ReadPartial {
			t.Fatalf("a pinned grant: %s", got)
		}
	})
	t.Run("a shape the decision point cannot compute is partial", func(t *testing.T) {
		r := newRig(t)
		r.shapes.read, r.shapes.pkgs, r.notes = iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, decide("left-pad")
		r.shapes.pkgErr = errors.New("stale")
		if mk := markingOf(t, r.do("GET", doc, hRecognize, "forward", hCred, "token")); mk.Package != "left-pad" || mk.PackageRead != iface.ReadPartial || mk.Class != iface.ClassPrivate {
			t.Fatalf("%+v", mk)
		}
		r = newRig(t)
		r.shapes.err, r.notes = errors.New("stale"), decide("left-pad")
		rec := r.do("GET", doc, hRecognize, "forward", hCred, "token")
		if mk := markingOf(t, rec); mk.Package != "left-pad" || mk.PackageRead != iface.ReadPartial || mk.Class != iface.ClassPass || rec.Header().Get(iface.HeaderRead) != "" {
			t.Fatalf("no shape on the repository: %+v %v", mk, rec.Header())
		}
	})
	t.Run("a probe carries no package", func(t *testing.T) {
		r := newRig(t)
		r.notes = decide("left-pad")
		rec := r.do("POST", "/api/mk/web/mk-local/audit/left-pad", hRecognize, "probe")
		if rec.Header().Get(iface.HeaderPackage) != "" || len(r.shapes.pkgAsked) != 0 {
			t.Fatalf("%v", rec.Header())
		}
	})
}

// TestR_BAS_082_TheTwoHeadersReachNoOrdinaryCaller: a response to a request that is no recognized Bastion request carries neither
// package header, whatever the core noted and whatever a handler set (R-BAS-013).
func TestR_BAS_082_TheTwoHeadersReachNoOrdinaryCaller(t *testing.T) {
	r := newRig(t)
	r.shapes.read, r.shapes.pkgs, r.notes = iface.ReadPartial, map[string]iface.Read{"left-pad": iface.ReadFull}, decide("left-pad")
	r.respond = func(w http.ResponseWriter) {
		w.Header().Set(iface.HeaderPackage, "left-pad")
		w.Header().Set(iface.HeaderPackageRead, "full")
	}
	for _, cred := range []string{"token", "registry", "anon"} {
		for _, path := range []string{"/api/mk/web/mk-local/d/left-pad", "/api/mk/web/mk-local/f/x.bin"} {
			rec := r.do("GET", path, hCred, cred, iface.HeaderAuthorization, "Bearer qdr_bst_nothing", iface.HeaderInterface, "5")
			if got := hasMarking(rec.Header()); len(got) > 0 || rec.Code != 200 {
				t.Fatalf("%s %s: %d %v", cred, path, rec.Code, rec.Header())
			}
		}
	}
	if len(r.shapes.pkgAsked) != 0 {
		t.Fatalf("an ordinary request computed a package shape: %v", r.shapes.pkgAsked)
	}
}
