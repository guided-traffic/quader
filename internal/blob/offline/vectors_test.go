package offline_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/offline"
)

// The offline tool is the second reader of the stored format beside the store's (storage.md R-STOR-079, R-STOR-089): it reads every
// object case of the vector files that tools/blobvectors wrote and tools/blobvectors/check.py checked independently. Each object
// lies in a directory as a pull writes it, at the plain name of its copy's digest (or, for the keyed name's known answer, at that
// name), beside the record file of the vectors, with an export that holds the case's reader keys sealed by a ring of the test.
// Without a database the tool learns the digest from the wrap and the name stands in for the copy row, so the two cases whose
// refusal (record) comes from the copy's digest ("other-name", "other-record") are a name that does not match.

type vectorCase struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Reason     string `json:"reason"`
	NameKey    string `json:"nameKey"`
	Prefix     string `json:"prefix"`
	Digest     string `json:"digest"`
	ObjectName string `json:"objectName"`
	ReaderKeys []struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"readerKeys"`
	Copy *struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"copy"`
	Object string `json:"object"`
	Record string `json:"record"`
}

const vectorDir = "../blobcrypt/testdata"

func loadVectorCases(t *testing.T) []vectorCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(vectorDir, "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []vectorCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

// vectorDirectory lays out one object as a pull writes it and returns the reader of the directory with the case's reader keys
// (and the name key nameKey under nk-default-0a1b2c3d when given).
func vectorDirectory(t *testing.T, c vectorCase, name string, info offline.Info, nameKey string) *offline.Reader {
	t.Helper()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join(vectorDir, c.Object))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := os.ReadFile(filepath.Join(vectorDir, c.Record))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	m, _ := json.Marshal(map[string]any{
		"format": "quader-offline-pull-v1", "pulledAt": "2026-11-23T00:00:00Z", "connection": info.Connection,
		"prefix": info.Prefix, "encrypted": info.Encrypted, "keyedNames": info.KeyedNames,
	})
	for f, b := range map[string][]byte{p: body, p + offline.RecordSuffix: rec, filepath.Join(dir, offline.ManifestFile): m} {
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ringDir, ring := keyRing(t, "2026-12")
	var entries []entry
	for _, k := range c.ReaderKeys {
		raw, _ := hex.DecodeString(k.Key)
		entries = append(entries, entry{id: k.ID, raw: raw})
	}
	if nameKey != "" {
		raw, _ := hex.DecodeString(nameKey)
		entries = append(entries, entry{id: "nk-default-0a1b2c3d", raw: raw})
	}
	ks, err := offline.LoadKeys(writeExport(t, ring, entries...), ringDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ks.Close)
	src, err := offline.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return offline.NewReader(src, ks)
}

func TestR_STOR_079_TheOfflineToolReadsEveryObjectOfTheVectors(t *testing.T) {
	cases := loadVectorCases(t)
	info := offline.Info{Connection: "default", Prefix: "quader", Encrypted: true}
	byName := map[string]vectorCase{}
	read, positive, negative := 0, 0, 0
	for _, c := range cases {
		byName[c.Name] = c
		if c.Kind != "object" {
			continue
		}
		d, ok := blob.ParseDigest(c.Copy.Digest)
		if !ok {
			t.Fatalf("%s: copy digest %q", c.Name, c.Copy.Digest)
		}
		name := blob.Key("quader", d)
		got, tot := verify(t, vectorDirectory(t, c, name, info, ""))
		res := got[name]
		want := c.Reason
		switch {
		case want == "":
			want = offline.StatusOK
			positive++
		case c.Name == "other-name" || c.Name == "other-record":
			want = offline.StatusNameMismatch
			negative++
		default:
			negative++
		}
		if tot.Objects != 1 || res.Status != want || (want == offline.StatusOK && (res.SHA256 != c.Copy.Digest || *res.Size != c.Copy.Size)) {
			t.Errorf("%s: %+v (totals %+v), want %s", c.Name, res, tot, want)
			continue
		}
		read++
	}
	// The keyed name's known answer (R-STOR-086): the object a at that name opens with the name key of the vector; at the keyed name
	// of another digest it is a name that does not match.
	k, a := byName["keyed-name"], byName["a"]
	keyed := offline.Info{Connection: "default", Prefix: k.Prefix, Encrypted: true, KeyedNames: true}
	if got, _ := verify(t, vectorDirectory(t, a, k.ObjectName, keyed, k.NameKey)); got[k.ObjectName].Status != offline.StatusOK || got[k.ObjectName].SHA256 != k.Digest {
		t.Errorf("the keyed name's known answer: %+v", got[k.ObjectName])
	}
	other := "quader/keyed/00/00/0000" + k.ObjectName[len("quader/keyed/99/42/9942"):]
	if got, _ := verify(t, vectorDirectory(t, a, other, keyed, k.NameKey)); got[other].Status != offline.StatusNameMismatch {
		t.Errorf("at another keyed name: %+v", got[other])
	}
	if positive != 8 || negative != 25 || read != 33 {
		t.Errorf("read %d of the object cases as expected (%d positive, %d negative); the vectors hold 8 and 25", read, positive, negative)
	}
	t.Logf("the offline reader read %d object cases (%d genuine, %d refused with their reason) and the keyed name's known answer; the staging tail is no object of a connection", read, positive, negative)
}
