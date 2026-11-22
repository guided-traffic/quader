package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestR_BAK_030_TheCredentialsFileNamesOneCredentialPerConnection: the file of --journal-credentials-file and --credentials-file
// is a list of elements keyed by the connection (Q-BAK-73), read strictly (unit, about 0.01 s).
func TestR_BAK_030_TheCredentialsFileNamesOneCredentialPerConnection(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "credentials.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	f, err := LoadCredentials(write(t, "connections:\n  - connection: default\n    credentials_source: static\n    access_key_id_file: /j/id\n    secret_access_key_file: /j/secret\n  - connection: secondary\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := f.For("default"); !ok || c.Source != "static" || c.AccessKeyIDFile != "/j/id" {
		t.Errorf("default = %+v, %v", c, ok)
	}
	if c, ok := f.For("secondary"); !ok || c.Source != "default" {
		t.Errorf("secondary = %+v, %v; want the default chain", c, ok)
	}
	if _, ok := f.For("archive"); ok {
		t.Error("a connection the file does not name has a credential")
	}
	for name, c := range map[string]struct{ body, want string }{
		"the flat form of one credential": {"source: static\naccess_key_id_file: /j/id\n", "field source not found"},
		"a misspelled key":                {"connections:\n  - connection: default\n    acess_key_id_file: /j/id\n", "acess_key_id_file"},
		"no connection":                   {"connections: []\n", "names no connection"},
		"static without its files":        {"connections:\n  - connection: default\n    credentials_source: static\n", "access_key_id_file and secret_access_key_file"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadCredentials(write(t, c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}
