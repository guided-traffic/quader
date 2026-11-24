package collection_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
)

// namesTap wants every member and records the names it was handed.
type namesTap struct{ took []string }

func (r *namesTap) Wants(string) (int64, bool) { return 1 << 10, true }
func (r *namesTap) Take(path string, _ []byte) { r.took = append(r.took, path) }

// linked is a collection the way ansible.posix 2.2.2 is built: a script that a link beside it names, a module behind a link one directory
// up, and the README a link names, as `ansible-galaxy collection build` writes and FILES.json lists them (a file with the digest of its
// target).
func linked() collectiontest.Spec {
	s := spec()
	s.Files["tests/utils/shippable/remote.sh"] = "#!/bin/sh\necho remote\n"
	s.Files["docs/README.md"] = "# the readme behind the link\n"
	delete(s.Files, "README.md")
	s.Links = map[string]string{
		"tests/utils/shippable/aix.sh": "remote.sh",
		"plugins/modules/alias.py":     "x.py",
		"README.md":                    "docs/README.md",
	}
	return s
}

// Q-ANS-91, R-ANS-010: an archive with links whose targets stay inside it is accepted; the links are checked against FILES.json by
// the digest of their targets, they are no files of the count, and nothing is read through them: the tap never sees a link, and the
// README that collection_info names through a link is absent.
func TestR_ANS_010_ACollectionWithContainedLinksIsAcceptedAndNoLinkIsRead(t *testing.T) {
	tap := &namesTap{}
	a, err := collection.ReadArchive(context.Background(), bytes.NewReader(collectiontest.Build(linked())), collection.DefaultLimits(),
		collection.WithTap(tap))
	if err != nil {
		t.Fatal(err)
	}
	// Regular files: x.py, runtime.yml, remote.sh, docs/README.md.
	if a.Files != 4 {
		t.Errorf("Files = %d, want the four regular files", a.Files)
	}
	for _, name := range tap.took {
		if name == "tests/utils/shippable/aix.sh" || name == "plugins/modules/alias.py" || name == "README.md" {
			t.Errorf("the tap was handed the link %s", name)
		}
	}
	if a.ReadmeName != "" {
		t.Errorf("the README reached only through a link is %q, want absent", a.ReadmeName)
	}
	data, found, err := collection.ReadFile(context.Background(), bytes.NewReader(collectiontest.Build(linked())), collection.DefaultLimits(), "README.md", 1<<10)
	if err != nil || found || data != nil {
		t.Errorf("ReadFile of a link = %q %v %v, want absent", data, found, err)
	}
	if data, found, _ := collection.ReadFile(context.Background(), bytes.NewReader(collectiontest.Build(linked())), collection.DefaultLimits(), "docs/README.md", 1<<10); !found || string(data) != "# the readme behind the link\n" {
		t.Errorf("ReadFile of the target itself = %q %v", data, found)
	}
}

// Q-ANS-91: what FILES.json says of a link is held as it is of a file, and a link that leaves the archive is refused by its name.
func TestR_ANS_010_ALinkIsHeldToFilesJSONAndToTheArchive(t *testing.T) {
	relist := func(mutate func(files []any) []any) []byte {
		es := collectiontest.Entries(linked())
		for i := range es {
			if es[i].Name != collection.FilesName {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(es[i].Body, &m); err != nil {
				t.Fatal(err)
			}
			m["files"] = mutate(m["files"].([]any))
			b, _ := json.Marshal(m)
			return collectiontest.Pack(collectiontest.Refile(es, b))
		}
		t.Fatal("no FILES.json")
		return nil
	}
	setSum := func(name, sum string) func([]any) []any {
		return func(files []any) []any {
			for _, f := range files {
				if m := f.(map[string]any); m["name"] == name {
					m["chksum_sha256"] = sum
				}
			}
			return files
		}
	}
	drop := func(name string) func([]any) []any {
		return func(files []any) []any {
			var out []any
			for _, f := range files {
				if f.(map[string]any)["name"] != name {
					out = append(out, f)
				}
			}
			return out
		}
	}
	dangling := collectiontest.Entries(linked())
	for i := range dangling {
		if dangling[i].Name == "plugins/modules/alias.py" {
			dangling[i].Link = "gone.py"
		}
	}
	escaping := linked()
	escaping.Links["plugins/modules/alias.py"] = "../../../x.py"
	through := collectiontest.Entries(linked())
	through = append(through,
		collectiontest.Entry{Name: "sub/up", Type: tar.TypeSymlink, Link: ".."},
		collectiontest.Entry{Name: "chained", Type: tar.TypeSymlink, Link: "sub/up/README.md"})
	below := collectiontest.Entries(linked())
	below = append(below,
		collectiontest.Entry{Name: "linkdir", Type: tar.TypeSymlink, Link: "docs"},
		collectiontest.Entry{Name: "linkdir/extra.md", Body: []byte("x\n")})
	for _, c := range []struct {
		name, reason, says string
		data               []byte
	}{
		{
			"a link listed with another digest than its target's", collection.ReasonMemberChecksum, "alias.py",
			relist(setSum("plugins/modules/alias.py", collectiontest.Sum([]byte("other")))),
		},
		{"a link FILES.json does not list", collection.ReasonUnlistedMember, "aix.sh", relist(drop("tests/utils/shippable/aix.sh"))},
		{"a link to a file the archive does not hold", collection.ReasonMemberMissing, "alias.py", collectiontest.Pack(dangling)},
		{"a link that leaves the archive", collection.ReasonUnreadable, "alias.py", collectiontest.Build(escaping)},
		{"a link resolved through another link", collection.ReasonUnreadable, "chained", collectiontest.Pack(through)},
		{"an entry below a link", collection.ReasonUnreadable, "linkdir/extra.md", collectiontest.Pack(below)},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := read(t, c.data)
			if ce := wantReason(t, err, c.reason); !strings.Contains(ce.Detail, c.says) {
				t.Errorf("the refusal %q does not name %s", ce.Detail, c.says)
			}
		})
	}
}
