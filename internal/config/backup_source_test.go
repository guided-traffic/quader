package config

import (
	"strings"
	"testing"
)

// TestR_BAK_030_BackupSourceConnections: backup.source.connections holds one read credential per connection of
// storage.connections, with the rules of R-STOR-031 (unit, about 0.01 s).
func TestR_BAK_030_BackupSourceConnections(t *testing.T) {
	two := "storage:\n  connections:\n    - id: primary\n      bucket: a\n    - id: secondary\n      bucket: b\n"
	t.Run("an element per connection loads; a connection without one has none", func(t *testing.T) {
		l, err := loadConnections(t, two+"backup:\n  source:\n    connections:\n      - connection: secondary\n        credentials_source: static\n"+
			"        access_key_id_file: /s/id\n        secret_access_key_file: /s/secret\n")
		if err != nil {
			t.Fatal(err)
		}
		src := l.Config.Backup.Source.Connections
		if len(src) != 1 || src[0].Connection != "secondary" || src[0].Credentials().Source != "static" || src[0].Credentials().AccessKeyIDFile != "/s/id" {
			t.Fatalf("backup.source.connections = %+v", src)
		}
		if c := (ConnectionCredentials{Connection: "primary"}).Credentials(); c.Source != "default" {
			t.Fatalf("an element without credentials_source reads with %q, want default", c.Source)
		}
	})
	for name, c := range map[string]struct{ elems, want string }{
		"a connection the configuration lacks": {"      - connection: archive\n", "backup.source.connections[0] (archive): connection archive is no connection of storage.connections"},
		"two elements of one connection":       {"      - connection: primary\n      - connection: primary\n", "connection primary has an element already (connections[0])"},
		"static without its files":             {"      - connection: primary\n        credentials_source: static\n", "credentials_source static needs access_key_id_file and secret_access_key_file"},
		"webIdentity without its token":        {"      - connection: primary\n        credentials_source: webIdentity\n", "web_identity_token_file is required"},
		"an unknown source":                    {"      - connection: primary\n        credentials_source: env\n", `credentials_source "env" must be one of default, webIdentity, static`},
		"an id that is none":                   {"      - connection: Primary\n", `connection "Primary" must be the id of a connection`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadConnections(t, two+"backup:\n  source:\n    connections:\n"+c.elems)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}
