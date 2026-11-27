package iface_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The members interface version 8 adds (R-BAS-093, R-BAS-096, R-BAS-101 to R-BAS-103): their round trip through the wire and
// the rules their Validate holds. Data of bytes; no I/O.

func figure(n int64) *int64 { return &n }

// configuration is a configuration of the form the specification's example shows: two rules, an address rule, a ceiling.
func configuration() iface.Configuration {
	return iface.Configuration{
		Revision: 17,
		Switches: iface.Switches{Handover: true, GUI: true},
		Limits: iface.Limits{
			Mode: iface.ModeEnforce, Preset: iface.PresetInternet,
			Place: iface.PlaceTable{
				Rules: map[string]iface.Figures{
					"0193a0c4-anon":   {RequestsPerSecond: figure(50), RequestsBurst: figure(1000), ParallelTransfers: figure(64)},
					"0193a0c6-signed": {},
				},
				AddressRules: []iface.AddressRule{{Prefix: "198.51.100.0/24", Rule: "0193a0c6-signed"}},
				KindRules: iface.KindRules{
					Anonymous: "0193a0c4-anon", User: "0193a0c6-signed", TechnicalToken: "0193a0c6-signed", CI: "0193a0c6-signed", Administrator: "0193a0c6-signed",
				},
			},
			Flood: iface.Flood{
				Anonymous:      iface.FloodClass{RequestsPerSecond: 50, Burst: 1000, MissesPerMinute: 1000},
				Authenticated:  iface.FloodClass{RequestsPerSecond: 200, Burst: 4000, MissesPerMinute: 4000},
				GUI:            iface.FloodRate{RequestsPerSecond: 20, Burst: 100},
				FailedAuth:     iface.FailedAuth{MaxFailures: 20, WindowSeconds: 600, TimeoutSeconds: 900},
				Blocking:       iface.Blocking{Threshold: 3000, WindowSeconds: 60, DurationSeconds: 900, MaxDurationSeconds: 86400},
				ExemptNetworks: []string{"2001:db8::/32"},
			},
			Slots:            iface.Slots{MaxConcurrent: 64, AnonymousShare: 0.25, QueueTimeoutMillis: 2000},
			Throughput:       iface.Throughput{AnonymousShare: 0.25, BytesPerHour: iface.ThroughputHours{Catalogue: figure(1 << 30)}},
			AnonymousCeiling: iface.Ceiling{RequestsPerSecond: figure(100), Burst: figure(1000), RequestsPerHour: figure(100000)},
		},
		Display: iface.Display{TimeFormat: "iso"},
	}
}

// TestR_BAS_093_TheConfigurationTravelsWholeAndItsRulesHold: the configuration is one member of the handshake whose figures are
// null when unlimited, it decodes strictly to what was sent, and Validate refuses every member the specification's rules refuse.
func TestR_BAS_093_TheConfigurationTravelsWholeAndItsRulesHold(t *testing.T) {
	c := configuration()
	if err := c.Validate(); err != nil {
		t.Fatalf("the example configuration is refused: %v", err)
	}
	b, err := iface.Marshal(iface.Handshake{InterfaceVersion: iface.InterfaceVersion, Configuration: c})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"switches":{"handover":true,"gui":true,"docs":false}`, `"requestsPerHour":null`, `"bytesPerSecond":null`,
		`"addressRules":[{"prefix":"198.51.100.0/24","rule":"0193a0c6-signed"}]`, `"queueTimeoutMillis":2000`,
		`"display":{"timeFormat":"iso"}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the handshake %s lacks %s", b, want)
		}
	}
	if strings.Contains(string(b), `"handover":false,"configuration"`) || strings.Contains(string(b), `"boundRepository"`) {
		t.Errorf("a top-level member that moved or is absent is written: %s", b)
	}
	var back iface.Handshake
	if err := iface.DecodeStrict(strings.NewReader(string(b)), &back); err != nil {
		t.Fatal(err)
	}
	if err := back.Configuration.Validate(); err != nil || back.Configuration.Revision != 17 || !back.Configuration.Switches.Handover ||
		*back.Configuration.Limits.Place.Rules["0193a0c4-anon"].ParallelTransfers != 64 || back.Configuration.Limits.Place.Rules["0193a0c6-signed"].BytesPerDay != nil {
		t.Fatalf("decoded %+v: %v", back.Configuration, err)
	}
	if err := iface.DecodeStrict(strings.NewReader(`{"configuration":{"revision":1,"handover":true}}`), &back); err == nil {
		t.Error("a configuration with a member the interface does not know decodes")
	}
	for name, edit := range map[string]func(*iface.Configuration){
		"a negative revision":       func(c *iface.Configuration) { c.Revision = -1 },
		"an unknown mode":           func(c *iface.Configuration) { c.Limits.Mode = "off" },
		"an unknown preset":         func(c *iface.Configuration) { c.Limits.Preset = "home" },
		"a share above 1":           func(c *iface.Configuration) { c.Limits.Slots.AnonymousShare = 1.5 },
		"a share that is no number": func(c *iface.Configuration) { c.Limits.Throughput.AnonymousShare = math.NaN() },
		"a negative figure": func(c *iface.Configuration) {
			c.Limits.Place.Rules["0193a0c6-signed"] = iface.Figures{BytesPerDay: figure(-1)}
		},
		"a zero ceiling":                       func(c *iface.Configuration) { c.Limits.AnonymousCeiling.RequestsPerHour = figure(0) },
		"a ceiling's burst without its rate":   func(c *iface.Configuration) { c.Limits.AnonymousCeiling.RequestsPerSecond = nil },
		"a kind rule that names no rule":       func(c *iface.Configuration) { c.Limits.Place.KindRules.CI = "gone" },
		"an address rule that names no rule":   func(c *iface.Configuration) { c.Limits.Place.AddressRules[0].Rule = "gone" },
		"a prefix with host bits":              func(c *iface.Configuration) { c.Limits.Place.AddressRules[0].Prefix = "198.51.100.7/24" },
		"an exempt address without its length": func(c *iface.Configuration) { c.Limits.Flood.ExemptNetworks = []string{"203.0.113.9"} },
		"a rule ID with a space":               func(c *iface.Configuration) { c.Limits.Place.Rules["a b"] = iface.Figures{} },
		"no rule":                              func(c *iface.Configuration) { c.Limits.Place.Rules = nil },
		"a flood figure of 0":                  func(c *iface.Configuration) { c.Limits.Flood.GUI.Burst = 0 },
		"a block longer than its longest":      func(c *iface.Configuration) { c.Limits.Flood.Blocking.DurationSeconds = 90000 },
		"no slot":                              func(c *iface.Configuration) { c.Limits.Slots.MaxConcurrent = 0 },
		"a negative wait":                      func(c *iface.Configuration) { c.Limits.Slots.QueueTimeoutMillis = -1 },
		"no form of a time":                    func(c *iface.Configuration) { c.Display.TimeFormat = "" },
		"an unknown form of a time":            func(c *iface.Configuration) { c.Display.TimeFormat = "yyyy" },
	} {
		bad := configuration()
		edit(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("a configuration with %s is accepted", name)
		}
	}
}

// TestR_BAS_096_TheSubjectsLimitsTravelInTheCheckAndTheMarking: the subject's key and rule are a member of a token subject in the
// check answer and the header Quader-Bastion-Limits of the marking, the key percent-encoded; a marking that names them for any
// other subject, twice or malformed is refused.
func TestR_BAS_096_TheSubjectsLimitsTravelInTheCheckAndTheMarking(t *testing.T) {
	l := &iface.SubjectLimits{Key: "ci:https://gitlab.example.com:web/app", Rule: "0193a0c6-signed"}
	sub := iface.Subject{Kind: iface.SubjectToken, TokenID: "t1", PrincipalID: "p1", Limits: l}
	b, _ := iface.Marshal(iface.CheckAnswer{Subject: sub})
	if !strings.Contains(string(b), `"limits":{"key":"ci:https://gitlab.example.com:web/app","rule":"0193a0c6-signed"}`) {
		t.Errorf("the check answer %s lacks the subject's limits", b)
	}
	anon, _ := iface.Marshal(iface.CheckAnswer{Subject: iface.Subject{Kind: iface.SubjectAnonymous}})
	if strings.Contains(string(anon), "limits") {
		t.Errorf("an anonymous subject names limits: %s", anon)
	}
	h := http.Header{}
	iface.Marking{Class: iface.ClassArtifact, Subject: sub, Interface: iface.InterfaceVersion}.Set(h)
	if got := h.Get(iface.HeaderLimits); got != "key=ci%3Ahttps%3A//gitlab.example.com%3Aweb/app; rule=0193a0c6-signed" {
		t.Fatalf("%s = %q", iface.HeaderLimits, got)
	}
	m, _, err := iface.ParseMarking(h)
	if err != nil || m.Subject.Limits == nil || *m.Subject.Limits != *l {
		t.Fatalf("ParseMarking: %+v %v", m.Subject, err)
	}
	anonHeader := http.Header{}
	iface.Marking{Class: iface.ClassArtifact, Subject: iface.Subject{Kind: iface.SubjectAnonymous, Limits: l}, Interface: iface.InterfaceVersion}.Set(anonHeader)
	if anonHeader.Get(iface.HeaderLimits) != "" {
		t.Error("an anonymous marking names limits")
	}
	for name, edit := range map[string]func(http.Header){
		"an anonymous subject": func(h http.Header) { h.Set(iface.HeaderSubject, "anonymous") },
		"twice":                func(h http.Header) { h.Add(iface.HeaderLimits, "key=a; rule=b") },
		"no rule":              func(h http.Header) { h.Set(iface.HeaderLimits, "key=user:1") },
		"a rule with a space":  func(h http.Header) { h.Set(iface.HeaderLimits, "key=user:1; rule=a b") },
		"a key not encoded":    func(h http.Header) { h.Set(iface.HeaderLimits, "key=a b; rule=r") },
		"an unknown member":    func(h http.Header) { h.Set(iface.HeaderLimits, "key=user:1; rule=r; place=x") },
	} {
		bad := h.Clone()
		edit(bad)
		if _, _, err := iface.ParseMarking(bad); !errors.Is(err, iface.ErrMarking) {
			t.Errorf("a %s with %s: %v, want ErrMarking", iface.HeaderLimits, name, err)
		}
	}
}

// TestR_BAS_016_ABatchCarriesUsageAndStatusWithoutADownload: the reporting batch's usage and status are always written, as []
// where empty, a batch without an event decodes, and their bounds hold.
func TestR_BAS_016_ABatchCarriesUsageAndStatusWithoutADownload(t *testing.T) {
	b, err := iface.Marshal(iface.DownloadsRequest{Events: []iface.DownloadEvent{}, Status: iface.ReplicaStatus{Replica: "bastion-0"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"usage":{"bucketSeconds":0,"rows":[],"overflow":[]}`, `"blocks":[]`, `"bytesPerSecondByClass":{"anonymous":0,"signedIn":0,"catalogue":0}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the batch %s lacks %s", b, want)
		}
	}
	var back iface.DownloadsRequest
	if err := iface.DecodeStrict(strings.NewReader(string(b)), &back); err != nil || back.Status.Replica != "bastion-0" || back.Usage.Validate() != nil || back.Status.Validate() != nil {
		t.Fatalf("decode: %+v %v", back, err)
	}
	row := iface.UsageRow{BucketStart: time.Unix(0, 0).UTC(), Subject: "user:1", Requests: 3, Limited: map[string]int64{"cachedBytesPerHour": 1}}
	if err := (iface.UsageReport{BucketSeconds: 300, Rows: []iface.UsageRow{row}}).Validate(); err != nil {
		t.Errorf("a valid row is refused: %v", err)
	}
	bad := row
	bad.Limited = map[string]int64{"place": 1}
	for name, u := range map[string]iface.UsageReport{
		"a key that is no figure": {Rows: []iface.UsageRow{bad}},
		"a negative count":        {Rows: []iface.UsageRow{{Requests: -1}}},
		"too many rows":           {Rows: make([]iface.UsageRow, iface.MaxUsageRows+1)},
		"a negative bucket":       {BucketSeconds: -300},
	} {
		if u.Validate() == nil {
			t.Errorf("usage with %s is accepted", name)
		}
	}
	if (iface.ReplicaStatus{Blocks: make([]iface.StatusBlock, iface.MaxStatusBlocks+1)}).Validate() == nil ||
		(iface.ReplicaStatus{Blocks: []iface.StatusBlock{{Address: "203.0.113.7/24"}}}).Validate() == nil {
		t.Error("a status with too many blocks or a block of no canonical prefix is accepted")
	}
}

// TestR_BAS_093_TheContactReportsTheEdgeAndTheCatalogue: the two headers of the contact read what they write and nothing else.
func TestR_BAS_093_TheContactReportsTheEdgeAndTheCatalogue(t *testing.T) {
	for _, e := range []iface.EdgeLimit{{}, {RequestsPerSecond: 100, Connections: 50}, {Connections: 50}} {
		got, err := iface.ParseEdgeLimit(e.Header())
		if err != nil || got != e {
			t.Errorf("%q: %+v %v", e.Header(), got, err)
		}
	}
	if (iface.EdgeLimit{}).Header() != "off" || !(iface.EdgeLimit{}).Off() {
		t.Error("an edge without a figure is not off")
	}
	for _, v := range []string{"", "rps=0", "rps=01", "rps=1; rps=2", "burst=5", "rps=-1", "on"} {
		if _, err := iface.ParseEdgeLimit(v); err == nil {
			t.Errorf("%q is read", v)
		}
	}
	if kept, err := iface.ParseCatalogueKept("true"); err != nil || !kept {
		t.Error("true is not read")
	}
	if _, err := iface.ParseCatalogueKept("yes"); err == nil {
		t.Error("yes is read")
	}
}

// TestR_BAS_103_TheCataloguePathGrammarRoundTrips: every form of a document's path is written and read back to the same value,
// a value is percent-encoded once and only one spelling is read, and dot segments, encoded slashes, empty segments, other
// spellings and unknown forms are refused.
func TestR_BAS_103_TheCataloguePathGrammarRoundTrips(t *testing.T) {
	for want, p := range map[string]iface.CataloguePath{
		"index/0":                                                {Kind: iface.CatalogueIndex},
		"index/12":                                               {Kind: iface.CatalogueIndex, Index: 12},
		"packages/0193a0c4-77":                                   {Kind: iface.CataloguePackage, Package: "0193a0c4-77"},
		"packages/p1/versions":                                   {Kind: iface.CatalogueVersions, Package: "p1"},
		"packages/p1/vulnerabilities":                            {Kind: iface.CataloguePackageVulnerabilities, Package: "p1"},
		"packages/p1/versions/1.0.0%2Bbuild.1":                   {Kind: iface.CatalogueVersion, Package: "p1", Version: "1.0.0+build.1"},
		"packages/p1/versions/1%3A2.3-4/readme":                  {Kind: iface.CatalogueReadme, Package: "p1", Version: "1:2.3-4"},
		"packages/p1/versions/v1/vulnerabilities":                {Kind: iface.CatalogueVulnerabilities, Package: "p1", Version: "v1"},
		"packages/p1/versions/v1/signatures":                     {Kind: iface.CatalogueSignatures, Package: "p1", Version: "v1"},
		"packages/p1/versions/v1/contents":                       {Kind: iface.CatalogueContents, Package: "p1", Version: "v1"},
		"packages/p1/versions/v1/docs":                           {Kind: iface.CatalogueDocs, Package: "p1", Version: "v1"},
		"packages/p1/versions/v1/docs/entries/module/ns.c.ping":  {Kind: iface.CatalogueDocEntry, Package: "p1", Version: "v1", EntryType: "module", EntryName: "ns.c.ping"},
		"packages/p1/versions/v1/docs/files/docs/guide%20one.md": {Kind: iface.CatalogueDocFile, Package: "p1", Version: "v1", File: "docs/guide one.md"},
		"setup/web/npm-all/npm":                                  {Kind: iface.CatalogueSetup, Project: "web", Repository: "npm-all", Client: "npm"},
		"setup/web/npm-all/npm/p1/1.0.0%2Bb":                     {Kind: iface.CatalogueSetup, Project: "web", Repository: "npm-all", Client: "npm", SetupPackage: "p1", SetupVersion: "1.0.0+b"},
		"signatures/objects/" + strings.Repeat("ab", 32):         {Kind: iface.CatalogueSignatureObject, SHA256: strings.Repeat("ab", 32)},
		"signing/trusted-root.json":                              {Kind: iface.CatalogueTrustedRoot},
		"signing/chain.pem":                                      {Kind: iface.CatalogueChain},
		"state":                                                  {Kind: iface.CatalogueState},
		"packages/p1/versions/v1/state":                          {Kind: iface.CatalogueVersionState, Package: "p1", Version: "v1"},
		"files/web/raw/folder":                                   {Kind: iface.CatalogueFolder, Project: "web", Repository: "raw"},
		"files/web/raw/folder/a%20b/c":                           {Kind: iface.CatalogueFolder, Project: "web", Repository: "raw", File: "a b/c"},
		"files/web/raw/file/a/%C3%A4%25%3F.tgz":                  {Kind: iface.CatalogueFile, Project: "web", Repository: "raw", File: "a/ä%?.tgz"},
		"files/web/raw/setup/curl/a/b.zip":                       {Kind: iface.CatalogueFileSetup, Project: "web", Repository: "raw", Client: "curl", File: "a/b.zip"},
	} {
		if got := p.String(); got != want {
			t.Errorf("%+v writes %q, want %q", p, got, want)
		}
		back, err := iface.ParseCataloguePath(want)
		if err != nil || back != p {
			t.Errorf("%q reads %+v %v", want, back, err)
		}
	}
	for _, bad := range []string{
		"", "index", "index/01", "index/-1", "index/x", "packages", "packages/p1/version", "packages/p1/versions/v1/readme/x",
		"packages/p1/versions/v1/docs/entries/module", "packages/p1/versions/v1/docs/files", "packages/p1/versions/v1/changelog",
		"packages/../versions", "packages/./versions", "packages/%2E%2E", "packages/a%2Fb", "packages/a%2fb", "packages//versions",
		"packages/%41", "packages/1.0.0+x", "packages/a%2", "packages/a%00", "signatures/objects/" + strings.Repeat("AB", 32),
		"signatures/objects/abc", "signing/other.pem", "setup/web/npm-all", "setup/web/npm-all/npm/pkg", "/index/0", "index/0/",
		"state/x", "files/web/raw", "files/web/raw/file", "files/web/raw/setup/curl", "files/web/raw/folder/a/../b",
		"files/web/raw/file/a%2Fb", "files/web/raw/file/a//b", "files/web/raw/other/a", "packages/p1/vulnerabilities/x",
		"packages/p1/signatures",
	} {
		if p, err := iface.ParseCataloguePath(bad); err == nil {
			t.Errorf("%q is read as %+v", bad, p)
		}
	}
	docs := 0
	for _, k := range []iface.CatalogueKind{iface.CatalogueReadme, iface.CatalogueDocs, iface.CatalogueDocEntry, iface.CatalogueDocFile, iface.CatalogueContents, iface.CatalogueVersion} {
		if (iface.CataloguePath{Kind: k}).Documentation() {
			docs++
		}
	}
	if docs != 4 {
		t.Errorf("%d kinds are documentation, want the README, the docs tree, its entries and its files", docs)
	}
}

// TestR_BAS_101_TheCatalogueChangeListHoldsItsOrder: the change list and the public set are written with [] where empty, and a
// change list is refused when its sequence goes back, a path is no document's, a document lacks its address or the cursor is
// not the last entry.
func TestR_BAS_101_TheCatalogueChangeListHoldsItsOrder(t *testing.T) {
	empty, _ := iface.Marshal(iface.CatalogueChanges{Cursor: 7})
	pub, _ := iface.Marshal(iface.CataloguePublic{})
	if string(empty) != `{"entries":[],"cursor":7,"more":false,"reset":false}` || string(pub) != `{"projects":[],"repositories":[]}` {
		t.Fatalf("empty answers: %s %s", empty, pub)
	}
	if err := (iface.CatalogueChanges{Cursor: 7}).Validate(7); err != nil {
		t.Errorf("an empty list after its cursor: %v", err)
	}
	sum := strings.Repeat("0f", 32)
	ok := iface.CatalogueChanges{Entries: []iface.CatalogueChange{
		{Path: "index/0", SHA256: sum, Size: 120, Seq: 8}, {Path: "packages/p1", Deleted: true, Seq: 11},
	}, Cursor: 11, More: true}
	if err := ok.Validate(7); err != nil {
		t.Fatalf("a valid list: %v", err)
	}
	for name, c := range map[string]iface.CatalogueChanges{
		"a seq not after the cursor":  {Entries: []iface.CatalogueChange{{Path: "index/0", SHA256: sum, Seq: 7}}, Cursor: 7},
		"a seq that goes back":        {Entries: []iface.CatalogueChange{{Path: "index/0", SHA256: sum, Seq: 9}, {Path: "index/1", SHA256: sum, Seq: 8}}, Cursor: 8},
		"a path of no document":       {Entries: []iface.CatalogueChange{{Path: "../etc", SHA256: sum, Seq: 8}}, Cursor: 8},
		"a document without its sum":  {Entries: []iface.CatalogueChange{{Path: "index/0", Seq: 8}}, Cursor: 8},
		"a deletion with a sum":       {Entries: []iface.CatalogueChange{{Path: "index/0", SHA256: sum, Deleted: true, Seq: 8}}, Cursor: 8},
		"a cursor short of the last":  {Entries: []iface.CatalogueChange{{Path: "index/0", SHA256: sum, Seq: 8}}, Cursor: 7},
		"a sum in upper-case letters": {Entries: []iface.CatalogueChange{{Path: "index/0", SHA256: strings.ToUpper(sum), Seq: 8}}, Cursor: 8},
	} {
		if c.Validate(7) == nil {
			t.Errorf("a change list with %s is accepted", name)
		}
	}
	reset := iface.CatalogueChanges{Entries: []iface.CatalogueChange{{Path: "index/0", SHA256: sum, Seq: 3}}, Cursor: 3, Reset: true}
	if err := reset.Validate(7); err != nil {
		t.Errorf("a reset list that starts again: %v", err)
	}
}

// TestR_BAS_101_TheBatchReadIsFramed: frames written by WriteCatalogueFrame are read back with their compressed bytes, a missing
// document has none, the end of the answer is io.EOF, and a frame that breaks its rules, is cut short or exceeds the bound is an
// error; a batch request names one to the bound of paths of the grammar, none twice.
func TestR_BAS_101_TheBatchReadIsFramed(t *testing.T) {
	sum := strings.Repeat("0f", 32)
	var b bytes.Buffer
	if err := iface.WriteCatalogueFrame(&b, iface.CatalogueFrame{Path: "index/0", SHA256: sum, Size: 120}, []byte("gzipbytes")); err != nil {
		t.Fatal(err)
	}
	if err := iface.WriteCatalogueFrame(&b, iface.CatalogueFrame{Path: "packages/p1", Missing: true}, nil); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(bytes.NewReader(b.Bytes()))
	f, gz, err := iface.ReadCatalogueFrame(r, 1<<20)
	if err != nil || f.Path != "index/0" || f.GzipSize != 9 || string(gz) != "gzipbytes" {
		t.Fatalf("first frame: %+v %q %v", f, gz, err)
	}
	if f, gz, err = iface.ReadCatalogueFrame(r, 1<<20); err != nil || !f.Missing || len(gz) != 0 {
		t.Fatalf("missing frame: %+v %q %v", f, gz, err)
	}
	if _, _, err = iface.ReadCatalogueFrame(r, 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("the end: %v", err)
	}
	for name, raw := range map[string]string{
		"a cut body":                `{"path":"index/0","sha256":"` + sum + `","size":1,"gzipSize":5,"missing":false}` + "\nab",
		"a frame above the bound":   `{"path":"index/0","sha256":"` + sum + `","size":1,"gzipSize":2000000,"missing":false}` + "\n",
		"a path of no document":     `{"path":"../x","sha256":"` + sum + `","size":1,"gzipSize":1,"missing":false}` + "\nx",
		"a missing frame with data": `{"path":"index/0","sha256":"","size":0,"gzipSize":1,"missing":true}` + "\nx",
		"an unknown member":         `{"path":"index/0","sha256":"` + sum + `","size":1,"gzipSize":1,"missing":false,"x":1}` + "\nx",
		"a header without its end":  `{"path":"index/0"`,
	} {
		if _, _, err := iface.ReadCatalogueFrame(bufio.NewReader(strings.NewReader(raw)), 1<<20); err == nil || errors.Is(err, io.EOF) && name != "a header without its end" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if (iface.CatalogueBatch{Paths: []string{"index/0", "state"}}).Validate(2) != nil {
		t.Error("a valid batch is refused")
	}
	for name, batch := range map[string]iface.CatalogueBatch{
		"no path": {}, "too many": {Paths: []string{"index/0", "index/1", "state"}}, "a path twice": {Paths: []string{"state", "state"}},
		"no document's path": {Paths: []string{"x"}},
	} {
		if batch.Validate(2) == nil {
			t.Errorf("a batch with %s is accepted", name)
		}
	}
}
