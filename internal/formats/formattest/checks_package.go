package formattest

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
)

// CheckPackageShape is check 21 (bastion.md R-BAS-086, Q-BAS-250): what a reader of only the package a response belongs to receives
// equals what a reader of the whole repository receives, and every rights decision of a content route is made before its status line.
// The main installation names the package of a response from the decisions the request made (R-BAS-081) and marks a document `shared`
// for a reader who is full on that package (R-BAS-082), so a Bastion serves one copy to readers of the repository and to readers of
// the package alike.
const CheckPackageShape Check = 21

// OtherPackage is the second package of check 21: a package of the format beside the one a content case reads, which the check stores
// in the Host after the case's own seed. A response that draws on it while it decides only its own package would reach a reader of
// that package alone through a shared copy.
type OtherPackage struct {
	// Name is the normalized name of the package Seed stores. No content case's answer belongs to it.
	Name string
	// Seed stores the package in the Host's repository (a local one), the way a client would publish it, so that the Host holds a
	// package row of Name.
	Seed func(h *Host) error
}

// responsePackage is the rule of R-BAS-081: the package of a response is P exactly when at least one decision named P, every
// decision named P, and none named no package.
func responsePackage(ds []Decision) (string, bool) {
	p := ""
	for _, d := range ds {
		switch {
		case d.Package == "":
			return "", false
		case p == "":
			p = d.Package
		case d.Package != p:
			return "", false
		}
	}
	return p, p != ""
}

// otherName is the name of the fixture's other package, "" without one.
func otherName(o *OtherPackage) string {
	if o == nil {
		return ""
	}
	return o.Name
}

// lateDecision returns the first decision made after the status line, if any.
func lateDecision(ds []Decision) (Decision, bool) {
	for _, d := range ds {
		if d.AfterStatus {
			return d, true
		}
	}
	return Decision{}, false
}

// onlyPackage is the Allow of a reader of only the package p, the way the decision point answers a subject whose shape on p is full
// (bastion.md R-BAS-080): every target of p, any version, path or part, and the repository-level question (no package and no path,
// which a grant narrowed by package matches, R-AUTHZ-005); a target of another package, or a path in no package (Q-AUTHZ-160), is
// refused as a missing resource. Such a subject's grants carry no path pattern on the repository (condition 2), so the Host's read
// scope says VersionsByPath false for it (HostOptions.NoPathPatterns). prior, the fixture's own Allow, decides what this allows.
func onlyPackage(p string, prior func(formats.Target) error) func(formats.Target) error {
	return func(t formats.Target) error {
		if t.Package != p && (t.Package != "" || t.Path != "") {
			return errs.E(errs.NotFound, errs.CodeNotFound, "The caller may not read the package.")
		}
		if prior != nil {
			return prior(t)
		}
		return nil
	}
}

// packageShapes is check 21. Every GET and HEAD content case below 400 of a Download, Metadata or Computed route is served on a Host
// seeded as the case says and with OtherPackage stored beside it, as the fixture's own caller, a reader of the repository:
//
//   - no decision may be made after the status line: the main installation names the package from the decisions it has when the
//     status line goes out, so a later one would not count;
//   - when the decisions name one package P and nothing else (responsePackage), the case is served again on the same Host, with its
//     renditions forgotten, as a reader of P alone (onlyPackage): status, body, ETag, Content-Type, Content-Encoding, Vary and
//     Cache-Control must be equal, and again no decision may come after the status line.
//
// A response that draws on another package must decide a package-less read as well (Host.Authorize with a target that names no
// package), so that it belongs to no package and only a reader of the repository shares it. A format with package names names
// OtherPackage, and at least one of its cases must be compared, so the check cannot pass on a fixture that exercises nothing; a case
// whose status changes once OtherPackage is stored fails, since it would leave the check nothing to compare.
func (r *run) packageShapes() {
	if len(r.f.Routes()) == 0 {
		r.failf("%s: the format has no routes and the check is not skipped (Skip[%d] needs a reason)", CheckPackageShape, CheckPackageShape)
		return
	}
	other := r.fx.OtherPackage
	if r.d.PackageNames && (other == nil || other.Name == "" || other.Seed == nil) {
		r.failf("%s: the format has package names and the fixture names no OtherPackage with a Name and a Seed, so no case can show that "+
			"an answer decided for one package draws on no other", CheckPackageShape)
		return
	}
	compared, seededOther := 0, false
	for _, c := range r.fx.Content {
		method := c.Method
		if method == "" {
			method = http.MethodGet
		}
		if (method != http.MethodGet && method != http.MethodHead) || (c.Status != 0 && c.Status >= http.StatusBadRequest) {
			continue
		}
		h := r.host("")
		if c.Seed != nil {
			if err := c.Seed(h); err != nil {
				r.failf("content case %q: the seed fails: %v", c.Name, err)
				continue
			}
		}
		if other != nil {
			if !r.seedOther(c, h, other, &seededOther) {
				continue
			}
		}
		if r.comparePackageReader(c, method, h, other) {
			compared++
		}
	}
	if r.d.PackageNames && compared == 0 {
		r.failf("%s: no content case's answer was decided for exactly one package, so the check compared no reader of a package with a reader of "+
			"the repository: give the fixture a case of a package document or a file", CheckPackageShape)
	}
}

// seedOther stores the other package beside a case's seed and holds it to a package row, once per check.
func (r *run) seedOther(c ContentCase, h *Host, other *OtherPackage, checked *bool) bool {
	if err := other.Seed(h); err != nil {
		r.failf("content case %q: the seed of OtherPackage %q fails: %v", c.Name, other.Name, err)
		return false
	}
	if *checked {
		return true
	}
	*checked = true
	if _, found, err := h.Packages().Package(context.Background(), h.Ref(), other.Name); err != nil || !found {
		r.failf("%s: the seed of OtherPackage stores no package row %q (found %v, %v), so the check would compare nothing that draws on it",
			CheckPackageShape, other.Name, found, err)
		return false
	}
	return true
}

// comparePackageReader serves one case as a reader of the repository and, when its answer belongs to one package, as a reader of
// that package alone, and reports whether it compared the two.
func (r *run) comparePackageReader(c ContentCase, method string, h *Host, other *OtherPackage) bool {
	rq, err := h.NewRequest(context.Background(), method, c.Path, bytes.NewReader(c.Body))
	if err != nil || !keptKind(rq.Route.Kind) {
		return false // a case that matches no route is check 7's finding; a Volatile answer is never shared
	}
	what := fmt.Sprintf("content case %q (route %q, %s)", c.Name, rq.Route.Name, rq.Route.Kind)
	full := h.ServeRequest(rq, false)
	if want := cmp.Or(c.Status, http.StatusOK); full.Code != want {
		// Check 7 serves the case without the other package: a status that changes with it is this check's to report.
		r.failf("%s: status %d with OtherPackage %q stored beside the case's seed, want %d", what, full.Code, otherName(other), want)
		return false
	}
	ds := h.Decisions()
	if d, late := lateDecision(ds); late {
		r.failf("%s: %s of the package %q at %q came after the status line, so the main installation could not name the package of the "+
			"answer from it: decide every read before the first byte (R-BAS-081)", what, d.Via, d.Package, d.Path)
		return false
	}
	p, ok := responsePackage(ds)
	if !ok {
		return false // the answer belongs to no package: only a reader of the repository shares it
	}
	if other != nil && p == other.Name {
		r.failf("%s: the answer belongs to %q, the fixture's OtherPackage, so nothing showed that it draws on no other package: name another "+
			"package in OtherPackage", what, p)
		return false
	}
	prior, priorPaths := h.opts.Allow, h.opts.NoPathPatterns
	h.opts.Allow, h.opts.NoPathPatterns = onlyPackage(p, prior), true
	defer func() { h.opts.Allow, h.opts.NoPathPatterns = prior, priorPaths }()
	h.forgetRenditions()
	rq2, err := h.NewRequest(context.Background(), method, c.Path, bytes.NewReader(c.Body))
	if err != nil {
		r.failf("%s: the request cannot be built again: %v", what, err)
		return false
	}
	narrow := h.ServeRequest(rq2, false)
	if d, late := lateDecision(h.Decisions()); late {
		r.failf("%s: for a reader of %q alone, %s of the package %q at %q came after the status line: decide every read before the first byte "+
			"(R-BAS-081)", what, p, d.Via, d.Package, d.Path)
	}
	who := fmt.Sprintf("a reader of the repository and a reader of %q alone", p)
	switch {
	case full.Code != narrow.Code:
		r.failf("%s: status %d and %d for %s; the answer is decided for %q alone, so a Bastion would share it between them: decide a "+
			"package-less read where it draws on other packages (R-BAS-081, R-BAS-086)", what, full.Code, narrow.Code, who, p)
	case !bytes.Equal(full.Body.Bytes(), narrow.Body.Bytes()):
		r.failf("%s: the body differs between %s (%d and %d bytes); the answer is decided for %q alone, so a Bastion would share the "+
			"repository reader's copy with the reader of the package: decide a package-less read where it draws on other packages "+
			"(R-BAS-081, R-BAS-086)", what, who, full.Body.Len(), narrow.Body.Len(), p)
	default:
		for _, hd := range sharedHeaders() {
			if x, y := full.Result().Header.Values(hd), narrow.Result().Header.Values(hd); !equalStrings(x, y) {
				r.failf("%s: header %s is %q and %q for %s", what, hd, x, y, who)
			}
		}
	}
	return true
}
