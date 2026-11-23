package config

import (
	"strings"
	"testing"
)

// TestR_STOR_075_AnEncryptedConnection is the table of R-STOR-075's configuration: encrypted: true loads with keyed names, keyed_names
// is a detail of an encrypted connection, and the part sizes hold whole segments while an element is encrypted.
func TestR_STOR_075_AnEncryptedConnection(t *testing.T) {
	t.Run("R-STOR-075 encrypted: true alone loads with keyed_names: true; a plain element has no keyed names", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - {id: archive, bucket: a, encrypted: true}\n    - {id: plain, bucket: b}\n")
		if err != nil {
			t.Fatal(err)
		}
		cs := l.Config.Storage.Connections
		if !cs[0].Encrypted || cs[0].KeyedNames == nil || !*cs[0].KeyedNames || !cs[0].UsesKeyedNames() {
			t.Errorf("encrypted element: %+v", cs[0])
		}
		if cs[1].Encrypted || cs[1].KeyedNames != nil || cs[1].UsesKeyedNames() {
			t.Errorf("plain element: %+v", cs[1])
		}
	})
	t.Run("R-STOR-075 keyed_names: false on an encrypted element keeps the names of R-STOR-001", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - {bucket: a, encrypted: true, keyed_names: false}\n")
		if err != nil {
			t.Fatal(err)
		}
		if c := l.Config.Storage.Connections[0]; !c.Encrypted || c.UsesKeyedNames() {
			t.Errorf("%+v", c)
		}
	})
	for _, c := range []struct{ name, yaml, want string }{
		{
			"keyed_names on a plain element", "storage:\n  connections:\n    - {bucket: a, keyed_names: false}\n",
			"storage.connections[0] (default): keyed_names is a detail of an encrypted connection; on a connection that is not encrypted the content is readable anyway",
		},
		{
			"storage.uploads.part_size: 100000 beside an encrypted element",
			"storage:\n  uploads:\n    part_size: 100000\n  object_store:\n    multipart:\n      part_size: 6000000\n  connections:\n    - {id: archive, bucket: a, encrypted: true}\n",
			"storage.uploads.part_size",
		},
		{
			"a multipart part size that holds no whole segments beside an encrypted element",
			"storage:\n  object_store:\n    multipart:\n      part_size: 6000000\n  connections:\n    - {id: archive, bucket: a, encrypted: true}\n",
			"storage.object_store.multipart.part_size",
		},
	} {
		t.Run("R-STOR-075 refused: "+c.name, func(t *testing.T) {
			_, err := loadConnections(t, c.yaml)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an issue containing %q, got %v", c.want, err)
			}
			if strings.Contains(c.want, "part_size") && !strings.Contains(err.Error(), "must be a multiple of 64KiB while connections[0] (archive) is encrypted") {
				t.Errorf("the issue does not name the rule and the connection: %v", err)
			}
		})
	}
	t.Run("R-STOR-075 part sizes that are no multiple of 64 KiB load while no element is encrypted", func(t *testing.T) {
		if _, err := loadConnections(t, "storage:\n  uploads:\n    part_size: 6000000\n  connections:\n    - {bucket: a}\n"); err != nil {
			t.Fatal(err)
		}
	})
}
