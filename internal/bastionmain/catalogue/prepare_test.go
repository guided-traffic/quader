package catalogue_test

import (
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue/cataloguetest"
)

// TestR_BAS_107_LinksAreTheBastionsAndItsDataPath: a link of the main installation becomes the Bastion's; a link of the management
// API becomes the Bastion's data path where the catalogue holds what it names (a signature object, the anchors) and no value where
// it does not; a version's file downloads through the Bastion's package route, the path of its downloadUrl (Q-BAS-398), and has no
// address where that is on another origin; a link of another host stays; text keeps its words with its links rewritten; a public
// registry's upstream stays and any other upstream goes; bytes that are no text are kept as they are.
func TestR_BAS_107_LinksAreTheBastionsAndItsDataPath(t *testing.T) {
	rw := catalogue.Rewrite{
		Base: "https://packages.example.com", Main: []string{"https://quader.example.com", "https://mirror.example.com", "https://packages.example.com"},
		PublicRegistries: map[string]bool{"registry.npmjs.org": true},
	}
	in := `{"a":"https://quader.example.com/ui/en/projects/acme","b":"/api/v1/signatures/objects/ab12",
		"c":"https://packages.example.com/api/v1/signing/chain.pem","d":"/api/v1/projects/acme/repositories/r/content/x.tgz",
		"e":"https://mirror.example.com/api/v1/packages","f":"https://github.com/acme/sdk",
		"g":"<a href=\"/api/v1/packages\">all</a> see https://quader.example.com/x","upstreamUrl":"https://registry.npmjs.org/x",
		"items":[{"upstreamUrl":"https://npm.corp.example/x"}],
		"files":[{"path":"x/-/x-1.tgz","downloadUrl":"https://packages.example.com/api/npm/acme/r/x/-/x-1.tgz",
			"contentUrl":"/api/v1/projects/acme/repositories/r/content/x/-/x-1.tgz"},
			{"path":"y","downloadUrl":"https://elsewhere.example.org/files/acme/r/y","contentUrl":"/api/v1/projects/acme/repositories/r/content/y"}]}`
	p, err := catalogue.Prepare([]byte(in), "application/json", rw)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"a":"https://packages.example.com/ui/en/projects/acme"`, `"b":"/ui/data/v1/signatures/objects/ab12"`,
		`"c":"https://packages.example.com/ui/data/v1/signing/chain.pem"`, `"d":null`, `"e":null`, `"f":"https://github.com/acme/sdk"`,
		`/ui/data/v1/packages`, `see https://packages.example.com/x`, `"upstreamUrl":"https://registry.npmjs.org/x"`,
		`"items":[{"upstreamUrl":null}]`,
		`"contentUrl":"/api/npm/acme/r/x/-/x-1.tgz"`, `"contentUrl":null,"downloadUrl":"https://elsewhere.example.org/files/acme/r/y"`,
	} {
		if !strings.Contains(string(p.Body), want) {
			t.Errorf("no %s in %s", want, p.Body)
		}
	}
	text, err := catalogue.Prepare([]byte("-----BEGIN CERTIFICATE-----\nfrom https://quader.example.com/api/v1/signing\n"), "application/x-pem-file", rw)
	if err != nil || strings.Contains(string(text.Body), "quader.example.com") || !strings.Contains(string(text.Body), "BEGIN CERTIFICATE") {
		t.Fatalf("text: %s %v", text.Body, err)
	}
	raw := []byte{0x30, 0x82, 'h', 't', 't', 'p'}
	if b, err := catalogue.Prepare(raw, "application/octet-stream", rw); err != nil || string(b.Body) != string(raw) {
		t.Fatalf("bytes: %v %v", b.Body, err)
	}
}

// TestR_BAS_108_ACatalogueBuiltAgainAnswersReset: a catalogue dropped and built again (the web interface switched off and on, or a
// restore, which brings back no catalogue) numbers its change list above every number of the one before, so the cursor a Bastion
// kept answers reset and the whole list; a cursor the catalogue continues does not.
func TestR_BAS_108_ACatalogueBuiltAgainAnswersReset(t *testing.T) {
	ctx := context.Background()
	store, api := cataloguetest.New(), newFake()
	now := int64(1_000)
	b := catalogue.New(catalogue.Options{Store: store, Renderer: api, Main: []string{mainOrigin}, Workers: 1})
	if err := store.Begin(ctx, regID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Run(ctx, []catalogue.Registration{reg(true, false)}); err != nil {
		t.Fatal(err)
	}
	cursor := store.Seq(regID)
	if c, _ := store.Changes(ctx, regID, cursor, 10); c.Reset || len(c.Entries) != 0 {
		t.Fatalf("the catalogue's own cursor: %+v", c)
	}
	if err := store.Drop(ctx, regID); err != nil {
		t.Fatal(err)
	}
	if c, _ := store.Changes(ctx, regID, cursor, 10); !c.Reset || c.Cursor != 0 {
		t.Fatalf("a dropped catalogue: %+v", c)
	}
	if _, err := b.Run(ctx, []catalogue.Registration{reg(true, false)}); err != nil {
		t.Fatal(err)
	}
	c, _ := store.Changes(ctx, regID, cursor, 1000)
	if !c.Reset || len(c.Entries) == 0 || c.Validate(cursor) != nil {
		t.Fatalf("the old cursor on the new catalogue: %+v", c)
	}
}
