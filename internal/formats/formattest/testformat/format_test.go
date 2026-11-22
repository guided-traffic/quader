package testformat_test

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/formattest/testformat"
)

// TestR_ARCH_010_TheTestFormatPassesTheKit: the conforming variant of the test
// format passes all ten checks, with a handler that tries to delete the content
// headers as well (R-ARCH-007).
func TestR_ARCH_010_TheTestFormatPassesTheKit(t *testing.T) {
	t.Run("conforming", func(t *testing.T) {
		formattest.Run(t, testformat.New(), testformat.Fixture())
	})
	t.Run("a handler that deletes the content headers still sends them", func(t *testing.T) {
		formattest.Run(t, testformat.With(testformat.StripHeaders), testformat.Fixture())
	})
	t.Run("R-BAS-013 a document that varies by a request header it names in Vary passes check 12", func(t *testing.T) {
		formattest.Run(t, testformat.With(testformat.VariesByHeader, testformat.Defect(testformat.VaryUserAgent)), testformat.Fixture())
	})
	t.Run("Q-ARCH-258 a Volatile search that returns package-derived bytes passes, with or without a handler that deletes the headers", func(t *testing.T) {
		formattest.Run(t, testformat.With(testformat.SearchReadsPackages), testformat.Fixture())
		formattest.Run(t, testformat.With(testformat.SearchReadsPackages, testformat.StripHeaders), testformat.Fixture())
	})
}

// TestQ_ARCH_258_OnlyContentRoutesAndVolatileOnesMayReturnPackageBytes: an Admin route that
// answers with the bytes of a stored package fails the check that drives it (check 9, its
// change case); a Volatile search that returns
// package-derived bytes does not (TestR_ARCH_010_TheTestFormatPassesTheKit), and the
// Volatile tarball route of the TarballVolatile defect fails because it serves a stored item.
func TestQ_ARCH_258_OnlyContentRoutesAndVolatileOnesMayReturnPackageBytes(t *testing.T) {
	rep := formattest.Evaluate(testformat.With(testformat.AdminReadsPackages), testformat.Fixture())
	if failed := rep.Failed(); !slices.Equal(failed, []formattest.Check{formattest.CheckRevision}) {
		t.Fatalf("failed checks = %v:\n%v", failed, rep)
	}
	if got := strings.Join(rep.Result(formattest.CheckRevision).Failures, "\n"); !strings.Contains(got, "of kind admin returned package content") {
		t.Fatalf("the check that drives the route does not say so:\n%s", got)
	}
}

// TestQ_ARCH_259_AFormatNotifiesTheAreasItDeclared: the in-memory Host refuses an area the
// descriptor did not declare, as the production writer does: the route that names it answers 500.
func TestQ_ARCH_259_AFormatNotifiesTheAreasItDeclared(t *testing.T) {
	rep := formattest.Evaluate(testformat.With(testformat.NotifiesUndeclared), testformat.Fixture())
	if got := strings.Join(rep.Result(formattest.CheckRevision).Failures, "\n"); !strings.Contains(got, "tag change (Admin route)") || !strings.Contains(got, "answered 500") {
		t.Fatalf("check 9 does not fail the tag change:\n%s\n%v", got, rep)
	}
}

// TestQ_ARCH_260_AFormatNamesItsOwnRepositoryOnly: a format that reads a package of a
// repository that is neither the request's nor a member of it fails the content check, because
// the in-memory Host refuses it as the production Host does.
func TestQ_ARCH_260_AFormatNamesItsOwnRepositoryOnly(t *testing.T) {
	rep := formattest.Evaluate(testformat.With(testformat.ReadsOtherRepository), testformat.Fixture())
	if got := strings.Join(rep.Result(formattest.CheckHeaders).Failures, "\n"); !strings.Contains(got, `content case "package document": status 500, want 200`) {
		t.Fatalf("check 7 does not fail the document route:\n%s\n%v", got, rep)
	}
}

// TestR_ARCH_010_Defects: each defect fails the check the spec names for it and
// no other check, which is the acceptance of R-ARCH-010 and R-ARCH-007: routes
// that are not told apart fail check 1, the tarball route registered as Volatile
// fails check 7, and a tag-changing Admin route that raises no revision fails
// check 9.
func TestR_ARCH_010_Defects(t *testing.T) {
	cases := []struct {
		name   string
		defect testformat.Defect
		check  formattest.Check
		match  string // part of the failure message
		// also are the checks that fail with it because the defect breaks what they
		// stand on: routes that are not told apart cannot be served either.
		also []formattest.Check
	}{
		{
			"R-ARCH-010 /{package} and /{scope}/{name} without a disambiguating rule fail check 1", testformat.AmbiguousRoutes, formattest.CheckAmbiguity, "ambiguous routes",
			[]formattest.Check{formattest.CheckHeaders, formattest.CheckNames, formattest.CheckShared, formattest.CheckBastion},
		},
		{"R-ARCH-010 a Match that cleans the path itself fails check 2", testformat.CleaningMatch, formattest.CheckTraversal, "resolves to route", nil},
		{"R-ARCH-010 a package document classified as content fails check 3", testformat.ContentMetadata, formattest.CheckClassify, "says content", nil},
		{"R-ARCH-010 a rewrite that is not deterministic fails check 4", testformat.NondeterministicRewrite, formattest.CheckRewrite, "not deterministic", nil},
		{"R-ARCH-010 a rewrite that keeps an upstream link fails check 4", testformat.UpstreamLeak, formattest.CheckRewrite, "still links to the upstream", nil},
		{"R-ARCH-010 a merge in which another member wins a version fails check 5", testformat.MergeLastWins, formattest.CheckMerge, "per-version source rule", nil},
		{"R-ARCH-010 an Inspect that accepts crafted input fails check 6", testformat.InspectAccepts, formattest.CheckInspect, "accepted the input", nil},
		{"R-ARCH-010 an Inspect that answers 400 for a bound crossed fails check 6", testformat.InspectWrongKind, formattest.CheckInspect, "want 413 or 422", nil},
		{"R-ARCH-010 an Inspect that holds 640 MiB before it refuses fails check 6", testformat.InspectHeap, formattest.CheckInspect, "the heap grew", nil},
		{
			"R-ARCH-007 the npm-style tarball route registered as Volatile fails check 7", testformat.TarballVolatile, formattest.CheckHeaders, "served a stored item",
			[]formattest.Check{formattest.CheckClassify},
		},
		{"R-ARCH-010 a setup template with a credential option fails check 8", testformat.TemplateCredential, formattest.CheckSetup, "spells a credential", nil},
		{"R-ARCH-010 a client without an install template fails check 8", testformat.NoInstallTemplate, formattest.CheckSetup, "no install template", nil},
		{"R-ARCH-010 R-UI-049 a class without guide sections fails check 8", testformat.NoGuide, formattest.CheckSetup, `class remote, client "tf": no guide section of topic "what"`, nil},
		{"R-ARCH-010 R-UI-049 a guide with a credential in a code block fails check 8", testformat.GuideCredential, formattest.CheckSetup, "spells a credential in a code block", nil},
		{"R-ARCH-009 a tag-changing Admin route that raises no revision fails check 9", testformat.TagWithoutRevision, formattest.CheckRevision, "must raise it", nil},
		{
			"R-ARCH-010 a name rule that keeps the case of a spelling fails check 10", testformat.NamesCaseSensitive, formattest.CheckNames, "normalize to",
			[]formattest.Check{formattest.CheckClassify},
		},
		{"R-ARCH-010 a document path no GET route resolves fails check 10", testformat.WrongDocumentPath, formattest.CheckNames, "does not match a route", nil},
		{"R-BAS-013 a package document that names the caller fails check 12", testformat.VariesByCaller, formattest.CheckShared, "the document differs between", nil},
		{"R-BAS-013 a package document that varies by a request header it does not name in Vary fails check 12", testformat.VariesByHeader, formattest.CheckShared, "the document differs between", nil},
		{"R-AUTHZ-005 a handler that authorizes a package without a path fails check 13", testformat.AuthorizesWithoutPath, formattest.CheckAuthzPaths, "and no path", nil},
		{"R-AUTHZ-005 a handler that authorizes the repository for a package's document fails check 13", testformat.AuthorizesRepositoryLevel, formattest.CheckAuthzPaths, "authorized no target that names the package at a path", nil},
		{"R-AUTHZ-005 a handler that authorizes the package document's path for a file it serves fails check 13", testformat.AuthorizesDocumentForFile, formattest.CheckAuthzPaths, "authorized no read of that path", nil},
		{"R-AUTHZ-005 a handler that forgets the path in a remote and a virtual repository only fails check 13", testformat.AuthorizesWithoutPathOffLocal, formattest.CheckAuthzPaths, "remote document", nil},
		{"R-AUTHZ-005 a handler that authorizes the repository in a remote and a virtual repository only fails check 13", testformat.AuthorizesRepositoryLevelOffLocal, formattest.CheckAuthzPaths, "authorized no target that names the package at a path", nil},
		{"R-AUTHZ-005 a document that lists a version whose file the caller may not read fails check 13", testformat.ListsHiddenVersions, formattest.CheckAuthzPaths, "lists the version", nil},
		{
			// Check 9 fails with it: a removal finds no version row to take, so the package's revision stays.
			"R-AUTHZ-005 a format whose package rows hold no version its documents name fails check 13", testformat.VersionsOutsideRows, formattest.CheckAuthzPaths, "none names a stored version",
			[]formattest.Check{formattest.CheckRevision},
		},
		{"R-AUTHZ-005 a document rendered otherwise on the path that filters it fails check 13", testformat.FiltersDifferently, formattest.CheckAuthzPaths, "when only an unrelated path is refused", nil},
		{
			// Check 19 fails with it: a body the format writes itself carries neither Content-Length nor the content address.
			"R-LIM-036 a format that writes a download's body itself instead of through Host.Serve fails check 16", testformat.WritesOwnDownload,
			formattest.CheckAdmission, "without the admission of Host.Serve",
			[]formattest.Check{formattest.CheckMalware, formattest.CheckBastion},
		},
		{
			"R-MAL-016 R-MAL-029 a format whose errors lose the headers of the core error fails check 17", testformat.RendersBareErrors,
			formattest.CheckMalware, "without Cache-Control: no-store",
			[]formattest.Check{formattest.CheckAdmission},
		},
		{"R-SIG-022 a format whose signature becomes a version fails check 18", testformat.SignatureVersion, formattest.CheckSignatures, "a signature is never a version", nil},
		{"R-SIG-063 a format that names a signature file an artifact fails check 18", testformat.GatesAttachment, formattest.CheckSignatures, "Gate names GateArtifact", nil},
		{"R-SIG-063 a format that names an artifact GateNone fails check 18", testformat.PassesArtifact, formattest.CheckSignatures, "Gate names GateNone", nil},
		{"R-SIG-044 a format that attests a signature file fails check 18", testformat.AttestsSignature, formattest.CheckSignatures, "AttestationSubjects names", nil},
		{
			// Check 1 fails with it: Register refuses a format that declares no signatures, and the kit runs Register's checks there.
			"R-SIG-022 a format that declares no signatures fails check 18", testformat.NoSignatureDeclaration, formattest.CheckSignatures, "Signatures() returns nil",
			[]formattest.Check{formattest.CheckAmbiguity},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			failsAlone(t, testformat.With(c.defect), testformat.Fixture(), c.check, c.match, c.also...)
		})
	}
	t.Run("R-AUTHZ-005 a format whose paths place files in versions and that names no file of a version fails check 13", func(t *testing.T) {
		rep := formattest.Evaluate(testformat.WithoutVersionPath(), testformat.Fixture())
		if failed := rep.Failed(); !slices.Equal(failed, []formattest.Check{formattest.CheckAuthzPaths}) {
			t.Fatalf("failed checks = %v, want check 13 alone:\n%v", failed, rep)
		}
		if got := strings.Join(rep.Result(formattest.CheckAuthzPaths).Failures, "\n"); !strings.Contains(got, "has no formats.VersionPather") {
			t.Fatalf("failures do not name the missing VersionPather:\n%s", got)
		}
	})
	t.Run("R-ARCH-010 the ambiguous variant is also refused by Register's own check", func(t *testing.T) {
		err := formats.Validate(testformat.With(testformat.AmbiguousRoutes))
		if err == nil || !strings.Contains(err.Error(), "ambiguous routes") {
			t.Fatalf("Validate = %v", err)
		}
	})
}

// sharedFixture is testformat.Fixture built once for the tests of checks 19 and 20, which copy it and replace the fields they change, never
// write through them: its crafted inputs cost most of a second to build under the race detector.
var sharedFixture = sync.OnceValue(testformat.Fixture)

// failsAlone asserts that the format fails check and the checks of also, no other, and that the failures of check say match.
func failsAlone(t *testing.T, f formats.Format, fx formattest.Fixture, check formattest.Check, match string, also ...formattest.Check) {
	t.Helper()
	rep := formattest.Evaluate(f, fx)
	want := append([]formattest.Check{check}, also...)
	slices.Sort(want)
	if failed := rep.Failed(); !slices.Equal(failed, want) {
		t.Fatalf("failed checks = %v, want %v:\n%v", failed, want, rep)
	}
	if got := strings.Join(rep.Result(check).Failures, "\n"); !strings.Contains(got, match) {
		t.Fatalf("failures do not say %q:\n%s", match, got)
	}
}

// TestR_BAS_058_KitCheck19: each of the four defects of what a format owes a Bastion fails check 19 and no other check, and the
// fixture's exemptions (NoBastion) hold only with a reason, for a route a Bastion keeps answers of, that a case served and that
// needs them (Q-BAS-184). That npm, NuGet, OCI, Debian, Ansible Galaxy, the ClamAV definitions and generic pass it is each
// format's TestR_ARCH_010_Conformance.
//
// A defect runs every check but check 6, whose subject (Inspect against crafted inputs of gigabytes, half a second a run under the
// race detector) none of the four touches; TestR_ARCH_010_TheTestFormatPassesTheKit runs it on the format they vary. An exemption
// runs check 19 alone. The fixture is built once (sharedFixture): its crafted inputs cost most of a second to build under the race detector.
func TestR_BAS_058_KitCheck19(t *testing.T) {
	notInspect := slices.DeleteFunc(formattest.Checks(), func(c formattest.Check) bool { return c == formattest.CheckInspect })
	base := sharedFixture()
	for _, c := range []struct {
		name   string
		defect testformat.Defect
		match  string
	}{
		{"a download whose ETag is no content address", testformat.DownloadWithoutContentAddress, `the ETag "\"1\"", which is no content address`},
		{"a document with a protocol header it does not declare", testformat.UndeclaredReplayHeader, "carries " + testformat.UndeclaredHeader},
		{"a document that answers its own validator 200", testformat.DocumentIgnoresConditional, "answered 200 to If-None-Match"},
		{"a format whose rate limit is not the status it declares", testformat.RateLimitedStatusDiffers, "WriteError answers a rate limit 429 and catalog.Facts.Bastion says 503"},
	} {
		t.Run("R-BAS-058 "+c.name+" fails check 19 and no other check", func(t *testing.T) {
			rep := formattest.EvaluateChecks(testformat.With(c.defect), base, notInspect...)
			if failed := rep.Failed(); !slices.Equal(failed, []formattest.Check{formattest.CheckBastion}) {
				t.Fatalf("failed checks = %v, want check 19 alone:\n%v", failed, rep)
			}
			if got := strings.Join(rep.Result(formattest.CheckBastion).Failures, "\n"); !strings.Contains(got, c.match) {
				t.Fatalf("failures do not say %q:\n%s", c.match, got)
			}
		})
	}
	check19 := func(f formats.Format, noBastion map[string]string, edit func(*formattest.Fixture)) formattest.Result {
		fx := base // a copy; the edits below replace fields, never change what base's fields hold
		fx.NoBastion = noBastion
		if edit != nil {
			edit(&fx)
		}
		return formattest.EvaluateChecks(f, fx, formattest.CheckBastion).Result(formattest.CheckBastion)
	}
	t.Run("R-BAS-058 a route listed with its reason is exempt from the conditional duty", func(t *testing.T) {
		if res := check19(testformat.With(testformat.DocumentIgnoresConditional), map[string]string{"tf.doc.get": "not built yet", "tf.all.get": "not built yet"}, nil); !res.Passed() {
			t.Fatalf("check 19: %v", res)
		}
	})
	noCase := func(fx *formattest.Fixture) {
		fx.Content = slices.DeleteFunc(slices.Clone(fx.Content), func(c formattest.ContentCase) bool { return c.Path == "-/all" })
	}
	for _, c := range []struct {
		name  string
		f     formats.Format
		entry map[string]string
		edit  func(*formattest.Fixture)
		match string
	}{
		{"an exemption does not cover a header the format does not declare", testformat.With(testformat.UndeclaredReplayHeader), map[string]string{"tf.doc.get": "not built yet"}, nil, "carries " + testformat.UndeclaredHeader},
		{"an exemption without a reason fails check 19", testformat.New(), map[string]string{"tf.doc.get": ""}, nil, "without a reason"},
		{"an exemption for a route a Bastion keeps nothing of fails check 19", testformat.New(), map[string]string{"tf.search.get": "a search"}, nil, "no Download, Metadata or Computed route"},
		{"an exemption for a route whose answers meet both duties fails check 19", testformat.New(), map[string]string{"tf.tarball.get": "stale"}, nil, "whose answers meet the content-address and the conditional duty"},
		{"an exemption for a route no case served fails check 19", testformat.New(), map[string]string{"tf.all.get": "not built yet"}, noCase, "which no content case served"},
		{"a document route without a case that served fails check 19", testformat.New(), nil, noCase, `route "tf.all.get" (computed) has no content case a Bastion keeps`},
	} {
		t.Run("R-BAS-058 "+c.name, func(t *testing.T) {
			if got := strings.Join(check19(c.f, c.entry, c.edit).Failures, "\n"); !strings.Contains(got, c.match) {
				t.Fatalf("check 19 does not say %q:\n%s", c.match, got)
			}
		})
	}
}

// TestR_STOR_071_KitCheck20: a declaration that the format's client checks a digest passes check 20 where the declaring route serves
// the stored blob through Host.Items().Serve under its content address, and each of the two defects fails check 20 and no other check.
// A defect takes the exemption another check grants its route, as the fixture of such a format would: the route of
// DeclaresUnservedBody is in NoContent with cases that expect its 501, the route of DeclaresUnaddressedBody in NoBastion (Q-STOR-296);
// neither exempts it from check 20, which has no exemption of its own. That OCI, npm, NuGet, Debian, Ansible Galaxy, the ClamAV
// definitions and generic pass it is each format's TestR_ARCH_010_Conformance.
//
// As for check 19, a case runs every check but check 6 (Inspect against crafted inputs of gigabytes, which none of the variants
// touches), on the fixture check 19's test built.
func TestR_STOR_071_KitCheck20(t *testing.T) {
	notInspect := slices.DeleteFunc(formattest.Checks(), func(c formattest.Check) bool { return c == formattest.CheckInspect })
	base := sharedFixture()
	t.Run("R-STOR-071 a declaring route served through Items().Serve under its content address passes every check", func(t *testing.T) {
		if rep := formattest.EvaluateChecks(testformat.With(testformat.DeclaresClientDigest), base, notInspect...); len(rep.Failed()) > 0 {
			t.Fatalf("failed checks = %v:\n%v", rep.Failed(), rep)
		}
	})
	unserved := base // a copy; the fields it changes are replaced, never written through
	unserved.Content = slices.Clone(base.Content)
	for i, c := range unserved.Content {
		method := c.Method
		if method == "" {
			method = http.MethodGet
		}
		if r, _, err := testformat.New().Match(method, c.Path); err == nil && r.Name == "tf.tarball.get" {
			unserved.Content[i].Status = http.StatusNotImplemented
		}
	}
	unserved.NoContent = map[string]string{"tf.tarball.get": "a local repository of this variant serves no tarball; its remote class does"}
	unaddressed := base
	unaddressed.NoBastion = map[string]string{"tf.tarball.get": "this variant's tarball carries a revision as its ETag"}
	for _, c := range []struct {
		name   string
		defect testformat.Defect
		fx     formattest.Fixture
		match  string
	}{
		{"a declaring route no case shows serving the stored blob, NoContent notwithstanding", testformat.DeclaresUnservedBody, unserved, `route "tf.tarball.get" declares ClientDigest.Checks and no GET content case answered it 200`},
		{"a declaring route whose answer is no content address, NoBastion notwithstanding", testformat.DeclaresUnaddressedBody, unaddressed, `the ETag "\"1\"", which is no content address`},
	} {
		t.Run("R-STOR-071 "+c.name+" fails check 20 and no other check", func(t *testing.T) {
			rep := formattest.EvaluateChecks(testformat.With(c.defect), c.fx, notInspect...)
			if failed := rep.Failed(); !slices.Equal(failed, []formattest.Check{formattest.CheckLinks}) {
				t.Fatalf("failed checks = %v, want check 20 alone:\n%v", failed, rep)
			}
			if got := strings.Join(rep.Result(formattest.CheckLinks).Failures, "\n"); !strings.Contains(got, c.match) {
				t.Fatalf("failures do not say %q:\n%s", c.match, got)
			}
		})
	}
	t.Run("R-STOR-071 check 20 cannot be skipped while a route declares", func(t *testing.T) {
		fx := base
		fx.Skip = map[formattest.Check]string{formattest.CheckLinks: "not applicable"}
		res := formattest.EvaluateChecks(testformat.With(testformat.DeclaresClientDigest), fx, formattest.CheckLinks).Result(formattest.CheckLinks)
		if got := strings.Join(res.Failures, "\n"); !strings.Contains(got, `route "tf.tarball.get" declares ClientDigest.Checks`) {
			t.Fatalf("check 20 does not refuse the skip: %v", res)
		}
	})
}

// TestR_BAS_086_KitCheck21: each of the two defects of what a format owes a reader of one package fails check 21 and no other check, and
// a format with package names cannot pass it without OtherPackage or skip it. That npm, NuGet, OCI, Debian, Ansible Galaxy and the
// ClamAV definitions pass it, each with its OtherPackage, and that generic skips it, is each format's TestR_ARCH_010_Conformance.
//
// As for checks 19 and 20, a case runs every check but check 6 (Inspect against crafted inputs of gigabytes, which neither defect
// touches), on the fixture check 19's test built.
func TestR_BAS_086_KitCheck21(t *testing.T) {
	notInspect := slices.DeleteFunc(formattest.Checks(), func(c formattest.Check) bool { return c == formattest.CheckInspect })
	base := sharedFixture()
	for _, c := range []struct {
		name   string
		defect testformat.Defect
		match  string
	}{
		{"a package document that lists the other packages while it decides its own alone", testformat.ListsAnotherPackage, `the body differs between a reader of the repository and a reader of "left-pad" alone`},
		{"a package document that asks a decision after its status line", testformat.DecidesAfterItsStatusLine, `WikiReadable of the package "left-pad" at "left-pad" came after the status line`},
	} {
		t.Run("R-BAS-086 "+c.name+" fails check 21 and no other check", func(t *testing.T) {
			rep := formattest.EvaluateChecks(testformat.With(c.defect), base, notInspect...)
			if failed := rep.Failed(); !slices.Equal(failed, []formattest.Check{formattest.CheckPackageShape}) {
				t.Fatalf("failed checks = %v, want check 21 alone:\n%v", failed, rep)
			}
			if got := strings.Join(rep.Result(formattest.CheckPackageShape).Failures, "\n"); !strings.Contains(got, c.match) {
				t.Fatalf("failures do not say %q:\n%s", c.match, got)
			}
		})
	}
	check21 := func(edit func(*formattest.Fixture)) formattest.Result {
		fx := base // a copy; the edits replace fields, never write through them
		edit(&fx)
		return formattest.EvaluateChecks(testformat.New(), fx, formattest.CheckPackageShape).Result(formattest.CheckPackageShape)
	}
	for _, c := range []struct {
		name  string
		edit  func(*formattest.Fixture)
		match string
	}{
		{"a fixture of a format with package names without OtherPackage fails check 21", func(fx *formattest.Fixture) { fx.OtherPackage = nil }, "names no OtherPackage"},
		{"check 21 cannot be skipped by a format with package names", func(fx *formattest.Fixture) {
			fx.Skip = map[formattest.Check]string{formattest.CheckPackageShape: "not applicable"}
		}, "the format has 7 routes"},
		{"an OtherPackage whose seed stores no package row fails check 21", func(fx *formattest.Fixture) {
			fx.OtherPackage = &formattest.OtherPackage{Name: "right-pad", Seed: func(*formattest.Host) error { return nil }}
		}, `stores no package row "right-pad"`},
		{"an OtherPackage that a case reads fails check 21", func(fx *formattest.Fixture) {
			fx.OtherPackage = &formattest.OtherPackage{Name: "left-pad", Seed: func(h *formattest.Host) error { return testformat.Publish(t.Context(), h, "left-pad", "2.0.0") }}
		}, `the answer belongs to "left-pad", the fixture's OtherPackage`},
		{"a fixture whose cases decide no single package fails check 21", func(fx *formattest.Fixture) {
			fx.Content = slices.DeleteFunc(slices.Clone(fx.Content), func(c formattest.ContentCase) bool { return c.Path != "-/all" && c.Path != "-/search" })
		}, "compared no reader of a package"},
	} {
		t.Run("R-BAS-086 "+c.name, func(t *testing.T) {
			if got := strings.Join(check21(c.edit).Failures, "\n"); !strings.Contains(got, c.match) {
				t.Fatalf("check 21 does not say %q:\n%s", c.match, got)
			}
		})
	}
}

func FuzzR_ARCH_028_Match(f *testing.F) { formattest.FuzzMatch(f, testformat.New()) }

func FuzzR_ARCH_028_Inspect(f *testing.F) {
	formattest.FuzzInspect(f, testformat.New(), "left-pad/-/left-pad-1.0.0.tgz", testformat.Tarball("left-pad", "1.0.0"))
}
