package testenv

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// Connection is the element of storage.connections of one connection, default, on the stack's blob bucket below prefix,
// with the static keys of QUADER_TEST_S3_* written into dir (storage.md R-STOR-050). It carries its defaults.
func Connection(tb testing.TB, dir, prefix string) config.StorageConnection {
	tb.Helper()
	o := S3Config(tb)
	p := prefix
	return config.StorageConnection{
		ID: config.DefaultConnectionID, Endpoint: o.Endpoint, Region: o.Region, Bucket: o.BlobsBucket, Prefix: &p,
		ForcePathStyle: true, CAFile: CAFile(tb), CredentialsSource: "static",
		AccessKeyIDFile: WriteFile(tb, dir, "s3-id", o.AccessKey), SecretAccessKeyFile: WriteFile(tb, dir, "s3-secret", o.SecretKey),
	}.WithDefaults()
}

// ConnectionsJSON is the value of QUADER_STORAGE_CONNECTIONS of one connection as Connection makes it: the whole list as one
// JSON key (R-STOR-050).
func ConnectionsJSON(tb testing.TB, dir, prefix string) string {
	tb.Helper()
	return ConnectionsJSONAt(tb, dir, prefix, "")
}

// ConnectionsJSONAt is ConnectionsJSON with another endpoint (a proxy in front of the stack's store); empty keeps the stack's.
func ConnectionsJSONAt(tb testing.TB, dir, prefix, endpoint string) string {
	tb.Helper()
	c := Connection(tb, dir, prefix)
	if endpoint != "" {
		c.Endpoint = endpoint
	}
	b, err := json.Marshal([]map[string]any{{
		"id": c.ID, "endpoint": c.Endpoint, "region": c.Region, "bucket": c.Bucket, "prefix": prefix, "force_path_style": c.ForcePathStyle,
		"ca_file": c.CAFile, "credentials_source": c.CredentialsSource,
		"access_key_id_file": c.AccessKeyIDFile, "secret_access_key_file": c.SecretAccessKeyFile,
	}})
	if err != nil {
		tb.Fatal(err)
	}
	return string(b)
}
