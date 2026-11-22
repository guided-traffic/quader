package repository

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// linkCatalog holds a format with a route whose clients check a digest (oci) and one without (nuget).
func linkCatalog() *catalog.Registry {
	r := catalog.NewRegistry()
	for id, checks := range map[string]bool{"oci": true, "nuget": false} {
		r.Register(catalog.Entry{Facts: catalog.Facts{
			ID: catalog.ID(id), Classes: []Class{Local, Remote, Virtual}, Layout: id, ProtocolBasePath: "/api/" + id + "/{project}/{key}/",
			PatternSubject: SubjectName, PackageNames: true,
		}, Names: lower{}, ClientDigest: checks})
	}
	return r
}

// TestR_STOR_014_TheRepositorysOptOut: storage is a closed object of maxUploadSize and downloadLinks (Q-STOR-283);
// downloadLinks is taken on every class of a format whose clients check a digest and is not_applicable on any other; false is
// a narrowing (repository.narrowed with the part downloadLinks, journaled and replayed) that a project Owner may make and take
// back (Q-STOR-282); the snapshot answers it without a query; on a well-known repository it is an administrator's, on a system
// repository the product's.
func TestR_STOR_014_TheRepositorysOptOut(t *testing.T) {
	validateWith := func(body string) error {
		d, err := DecodeDocument([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		_, v := validate(linkCatalog(), &d, true)
		return v.err()
	}
	for _, class := range []string{"local", "remote", "virtual"} {
		t.Run("R-STOR-014 downloadLinks false is taken on an OCI "+class+" repository", func(t *testing.T) {
			body := `{"key":"oci-x","class":"` + class + `","format":"oci","storage":{"downloadLinks":false,"maxUploadSize":10}}`
			if class == "remote" {
				body = `{"key":"oci-x","class":"remote","format":"oci","remote":{"url":"https://registry-1.docker.io"},"storage":{"downloadLinks":false}}`
			}
			if got := fieldsOf(validateWith(body)); got["/storage/downloadLinks"] != "" || got["/storage"] != "" {
				t.Fatalf("%v", got)
			}
		})
	}
	for _, tc := range []struct {
		name, storage, pointer, code string
	}{
		{"R-STOR-014 Q-STOR-283 the member presignedDownloads is refused at its pointer", `{"presignedDownloads":true}`, "/storage/presignedDownloads", codeInvalidValue},
		{"R-STOR-014 Q-STOR-283 a member storage does not hold is refused", `{"quota":"1TiB"}`, "/storage/quota", codeInvalidValue},
		{"R-STOR-014 downloadLinks is a boolean", `{"downloadLinks":"no"}`, "/storage/downloadLinks", codeInvalidValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := fieldsOf(validateWith(`{"key":"oci-x","class":"local","format":"oci","storage":` + tc.storage + `}`))
			if got[tc.pointer] != tc.code {
				t.Fatalf("%v", got)
			}
		})
	}
	t.Run("R-STOR-014 downloadLinks on a NuGet repository answers not_applicable", func(t *testing.T) {
		if got := fieldsOf(validateWith(`{"key":"nuget-x","class":"local","format":"nuget","storage":{"downloadLinks":true}}`)); got["/storage/downloadLinks"] != errs.CodeNotApplicable {
			t.Fatalf("%v", got)
		}
	})

	s := &Service{cat: linkCatalog()}
	r := Repository{ID: "8", Name: qname.Repo{Project: "web", Key: "oci-local"}, Class: Local, Format: "oci"}
	on, off, absent := json.RawMessage(`{"downloadLinks":true}`), json.RawMessage(`{"downloadLinks":false,"maxUploadSize":5}`), json.RawMessage(nil)
	for _, c := range []struct {
		name    string
		cur, nd json.RawMessage
		narrows bool
	}{
		{"absent to false", absent, off, true},
		{"true to false", on, off, true},
		{"false to false", off, off, false},
		{"false to true", off, on, false},
		{"false to absent", off, absent, false},
	} {
		t.Run("R-STOR-014 Q-STOR-282 "+c.name+" by a project Owner", func(t *testing.T) {
			cur, nd := &Document{Settings: Settings{Storage: c.cur}}, &Document{Settings: Settings{Storage: c.nd}}
			e := s.narrowing(r, cur, nd, nil, nil)
			if got := slices.Contains(e.parts(), partDownloadLinks); got != c.narrows || e.DownloadLinksOff != c.narrows {
				t.Fatalf("narrows %v, entry %+v", got, e)
			}
			if err := restrictedVulnerabilities(Caller{Restricted: true}, cur, nd); err != nil {
				t.Fatalf("an Owner's write is refused: %v", err)
			}
		})
	}
	t.Run("R-STOR-014 the snapshot reads the member, an absent member allows links", func(t *testing.T) {
		for cfg, want := range map[string]bool{
			`{}`: true, `{"storage":{"maxUploadSize":5}}`: true, `{"storage":{"downloadLinks":true}}`: true, `{"storage":{"downloadLinks":false}}`: false,
		} {
			got, err := toRepository(q.Repository{ID: 3, Key: "oci-local", Class: "local", Format: "oci", Config: []byte(cfg), Lifecycle: "active"}, "web", linkCatalog())
			if err != nil || got.DownloadLinks() != want {
				t.Errorf("%s: DownloadLinks %v, %v", cfg, got.DownloadLinks(), err)
			}
		}
	})
	t.Run("R-STOR-014 the replay sets the member false again and keeps maxUploadSize", func(t *testing.T) {
		rp := &replayer{doc: map[string]json.RawMessage{}}
		rp.set.Storage = json.RawMessage(`{"maxUploadSize":5}`)
		rp.downloadLinksOff(true)
		if downloadLinksOf(rp.set.Storage) || !rp.touched || string(rp.doc["storage"]) != `{"downloadLinks":false,"maxUploadSize":5}` {
			t.Fatalf("storage %s, touched %v", rp.doc["storage"], rp.touched)
		}
		again := &replayer{doc: map[string]json.RawMessage{}}
		again.set.Storage = off
		if again.downloadLinksOff(true); again.touched {
			t.Fatal("a member that says false already was written again")
		}
	})
	t.Run("R-STOR-014 R-REPO-069 on a well-known repository an administrator sets downloadLinks, on a system repository nobody", func(t *testing.T) {
		cur, nd := &Document{}, &Document{Settings: Settings{Storage: json.RawMessage(`{"downloadLinks":false}`)}}
		if out, err := managedPointers(wellknown.KindPreset, cur, nd); err != nil || len(out) != 0 {
			t.Fatalf("a preset: %v %v", out, err)
		}
		if out, _ := managedPointers(wellknown.KindSystem, cur, nd); len(out) == 0 {
			t.Fatal("a system repository took the member")
		}
		max := &Document{Settings: Settings{Storage: json.RawMessage(`{"maxUploadSize":5}`)}}
		if out, _ := managedPointers(wellknown.KindPreset, cur, max); len(out) != 1 || out[0].Pointer != "/storage/maxUploadSize" {
			t.Fatalf("maxUploadSize of a well-known repository: %v", out)
		}
	})
}
