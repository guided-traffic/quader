package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// connectionsEnv is what `quader serve` needs, without a connection: the file under test holds the list.
func connectionsEnv(extra ...string) []string {
	return minimalEnv(append([]string{
		"QUADER_PROCESS_ROLE=web",
		"QUADER_VALKEY_SENTINEL_ADDRESSES=[\"s1:26379\"]",
		"QUADER_VALKEY_SENTINEL_PRIMARY_NAME=quader",
		"QUADER_SECRETS_ENCRYPTION_KEY_FILES=[\"/keys/2026-10.key\"]",
		"QUADER_SECRETS_ACTIVE_KEY_ID=2026-10",
	}, extra...)...)
}

func loadConnections(t *testing.T, file string, env ...string) (*Loaded, error) {
	t.Helper()
	return Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: connectionsEnv(env...), Command: CommandServe})
}

func TestR_STOR_050_ConnectionsInTheConfiguration(t *testing.T) {
	t.Run("R-STOR-050 one element with an id and a bucket loads with every default", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - id: primary\n      bucket: quader-blobs\n")
		if err != nil {
			t.Fatal(err)
		}
		cs := l.Config.Storage.Connections
		if len(cs) != 1 {
			t.Fatalf("%d connections", len(cs))
		}
		c := cs[0]
		if c.ID != "primary" || c.Bucket != "quader-blobs" || c.KeyPrefix() != "quader" || c.PayloadChecksum != "sha256" ||
			c.RequestChecksumCalculation != "when_supported" || c.ResponseChecksumValidation != "when_supported" || !c.BatchDeletes() ||
			c.CredentialsSource != "default" || c.SSEMode != "none" || c.SSEBucketKey == nil || !*c.SSEBucketKey ||
			c.ScrubPeriod != 720*time.Hour || c.ScrubBytesPerSecond != 50*MiB || c.Drain || c.Encrypted || c.ForcePathStyle {
			t.Fatalf("defaults: %+v", c)
		}
		s := l.Config.Storage
		if s.Replication.MinCopies != 1 || s.Reconcile.Concurrency != 4 || s.Reconcile.BytesPerSecond != 100*MiB || s.Reconcile.ServerSide != "auto" ||
			s.Reconcile.QueueLimit != 10000 || s.Reconcile.RetryMaxBackoff != time.Hour || s.Reconcile.RunDuration != 10*time.Minute ||
			s.Scrub.RunDuration != 50*time.Minute || !s.ObjectStore.StartupCheck || s.ObjectStore.CircuitBreaker.FailureThreshold != 5 {
			t.Fatalf("section defaults: %+v", s)
		}
	})
	t.Run("R-STOR-050 an element without an id is default, and its scrub period and throttle are keys of its own", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - bucket: b\n      scrub_period: 48h\n      scrub_bytes_per_second: 2MiB\n      prefix: \"\"\n")
		if err != nil {
			t.Fatal(err)
		}
		c := l.Config.Storage.Connections[0]
		if c.ID != DefaultConnectionID || c.ScrubPeriod != 48*time.Hour || c.ScrubBytesPerSecond != 2*MiB || c.KeyPrefix() != "" {
			t.Fatalf("%+v", c)
		}
	})
	two := func(a, b string) string {
		return "storage:\n  connections:\n    - " + a + "\n    - " + b + "\n"
	}
	cases := []struct{ name, yaml, want string }{ // a name that starts with R- is the requirement it backs; R-STOR-050 otherwise
		{"a second element without an id", two("bucket: a", "bucket: b"), "storage.connections[1] (default): leaves out its id"},
		{"a duplicate id", two("{id: one, bucket: a}", "{id: one, bucket: b}"), "storage.connections[1] (one): id \"one\" is the id of connections[0]"},
		{"an id outside the pattern", "storage:\n  connections:\n    - {id: Primary, bucket: a}\n", "storage.connections[0] (Primary): id \"Primary\" must match"},
		{"nine elements", "storage:\n  connections:\n" + strings.Repeat("    - {bucket: a}\n", 9), "storage.connections holds 9 elements; at most 8"},
		{"bucket with bucket_file", "storage:\n  connections:\n    - {bucket: a, bucket_file: /f}\n", "storage.connections[0] (default): bucket and bucket_file exclude each other"},
		{
			"two elements on one endpoint and bucket with the prefixes quader and quader/a",
			two("{id: one, bucket: a, endpoint: s3.example.com}", "{id: two, bucket: a, endpoint: \"https://S3.example.com/\", prefix: quader/a}"),
			"storage.connections[1] (two): lies at the location of connections[0] (one)",
		},
		{"drain: true on every element", two("{id: one, bucket: a, drain: true}", "{id: two, bucket: b, drain: true}"), "storage.connections every element has drain: true"},
		{
			"min_copies: 3 with two elements", two("{id: one, bucket: a}", "{id: two, bucket: b}") + "  replication:\n    min_copies: 3\n",
			"storage.replication.min_copies (3) must be between 1 and the number of connections without drain: true (2)",
		},
		{"static credentials without the key files", "storage:\n  connections:\n    - {bucket: a, credentials_source: static}\n", "storage.connections[0] (default): credentials_source static needs"},
		{"kms without a key", "storage:\n  connections:\n    - {bucket: a, sse_mode: kms}\n", "storage.connections[0] (default): sse_kms_key_id is required"},
		{"a scrub period below a day", "storage:\n  connections:\n    - {bucket: a, scrub_period: 1h}\n", "storage.connections[0] (default): scrub_period (1h0m0s) must be at least"},
		{"R-STOR-030 a prefix of a slash", "storage:\n  connections:\n    - {bucket: a, prefix: \"/\"}\n", "storage.connections[0] (default): prefix must not start or end with /"},
		{"R-STOR-030 a prefix with a double slash", "storage:\n  connections:\n    - {bucket: a, prefix: \"a//b\"}\n", "storage.connections[0] (default): prefix must not contain //"},
		{"R-STOR-030 an http endpoint", "storage:\n  connections:\n    - {bucket: a, endpoint: \"http://s3.example.com\"}\n", "storage.connections[0] (default): endpoint uses http"},
		{"R-STOR-030 an endpoint of another scheme", "storage:\n  connections:\n    - {bucket: a, endpoint: \"ftp://s3.example.com\"}\n", "storage.connections[0] (default): endpoint must be an absolute https URL"},
		{"no element where a replica serves", "storage:\n  connections: []\n", "storage.connections needs one element at least"},
		{"an element without a bucket where a replica serves", "storage:\n  connections:\n    - {id: one}\n", "storage.connections[0] (one): bucket or bucket_file is required"},
		{"R-STOR-106 a capacity without its unit", "storage:\n  connections:\n    - {bucket: a, capacity: 5}\n", "storage.connections[0] (default): capacity \"5\" is below 1GiB"},
		{"R-STOR-106 a negative capacity", "storage:\n  connections:\n    - {bucket: a, capacity: -1}\n", "storage.connections[0] (default): capacity \"-1\" is not a size"},
	}
	for _, c := range cases {
		name := "R-STOR-050 refused: " + c.name
		if strings.HasPrefix(c.name, "R-") {
			name = c.name + " is refused naming the key"
		}
		t.Run(name, func(t *testing.T) {
			_, err := loadConnections(t, c.yaml)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an issue containing %q, got %v", c.want, err)
			}
		})
	}
	t.Run("R-STOR-050 the checksum and protocol switches, an http endpoint with allow_insecure_endpoint and a host without a scheme load", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - {id: one, bucket: a, endpoint: \"http://s3.local:9000\", allow_insecure_endpoint: true, payload_checksum: md5, request_checksum_calculation: when_required, batch_delete: false}\n    - {id: two, bucket: a, endpoint: \"s3.eu01.example.cloud\", prefix: other}\n")
		if err != nil {
			t.Fatal(err)
		}
		cs := l.Config.Storage.Connections
		if cs[0].PayloadChecksum != "md5" || cs[0].BatchDeletes() || cs[1].Endpoint != "https://s3.eu01.example.cloud" {
			t.Fatalf("%+v", cs)
		}
		if k := l.Sources["storage.connections"].Kind; k != "derived" {
			t.Errorf("source of a list whose endpoint got its scheme = %q, want derived", k)
		}
	})
	t.Run("R-STOR-106 a capacity with its unit loads, and an empty one states none", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - {id: one, bucket: a, capacity: 5TiB}\n    - {id: two, bucket: b, capacity: \"\"}\n")
		if err != nil {
			t.Fatal(err)
		}
		cs := l.Config.Storage.Connections
		if cs[0].CapacityBytes() != int64(5*TiB) || cs[1].CapacityBytes() != 0 {
			t.Fatalf("capacities %d and %d", cs[0].CapacityBytes(), cs[1].CapacityBytes())
		}
	})
	t.Run("R-STOR-050 QUADER_STORAGE_CONNECTIONS holds the whole list as JSON and overrides the file's list", func(t *testing.T) {
		l, err := loadConnections(t, "storage:\n  connections:\n    - {id: file, bucket: a}\n", `QUADER_STORAGE_CONNECTIONS=[{"id":"env","bucket":"b","scrub_period":"36h"}]`)
		if err != nil {
			t.Fatal(err)
		}
		cs := l.Config.Storage.Connections
		if len(cs) != 1 || cs[0].ID != "env" || cs[0].Bucket != "b" || cs[0].ScrubPeriod != 36*time.Hour {
			t.Fatalf("%+v", cs)
		}
	})
	t.Run("R-STOR-050 the backup target at a connection's location is refused, per connection (R-BAK-002)", func(t *testing.T) {
		_, err := loadConnections(t, "storage:\n  connections:\n    - {id: one, bucket: x}\n    - {id: two, bucket: shared, endpoint: s3.example.com}\nbackup:\n  target:\n    bucket: shared\n    endpoint: s3.example.com\n")
		if err == nil || !strings.Contains(err.Error(), "backup.target.bucket (shared) is the bucket of storage.connections[1] (two)") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestR_STOR_050_ResolveConnections(t *testing.T) {
	files := map[string]string{"/s/bucket": " quader-blobs\n", "/s/region": "eu01\n", "/s/endpoint": "object.storage.example\n", "/s/empty": "  \n"}
	read := func(p string) ([]byte, error) {
		if c, ok := files[p]; ok {
			return []byte(c), nil
		}
		return nil, os.ErrNotExist
	}
	t.Run("R-STOR-050 bucket_file, region_file and endpoint_file give the trimmed content as the value, and a host means https", func(t *testing.T) {
		got, err := ResolveConnections([]StorageConnection{{BucketFile: "/s/bucket", RegionFile: "/s/region", EndpointFile: "/s/endpoint"}}, read)
		if err != nil {
			t.Fatal(err)
		}
		if c := got[0]; c.Bucket != "quader-blobs" || c.Region != "eu01" || c.Endpoint != "https://object.storage.example" || c.ID != DefaultConnectionID {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("R-STOR-050 a file that cannot be read or is empty is an error naming the key and the connection", func(t *testing.T) {
		_, err := ResolveConnections([]StorageConnection{{ID: "one", BucketFile: "/s/missing"}, {ID: "two", BucketFile: "/s/empty"}}, read)
		if err == nil || !strings.Contains(err.Error(), "storage.connections[0] (one): bucket_file /s/missing cannot be read") ||
			!strings.Contains(err.Error(), "storage.connections[1] (two): bucket_file /s/empty is empty") || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("R-STOR-050 two connections whose files name one location are refused when the files are read", func(t *testing.T) {
		q := "quader"
		_, err := ResolveConnections([]StorageConnection{
			{ID: "one", Bucket: "quader-blobs", Endpoint: "https://object.storage.example", Prefix: &q},
			{ID: "two", BucketFile: "/s/bucket", EndpointFile: "/s/endpoint"},
		}, read)
		if err == nil || !strings.Contains(err.Error(), "lies at the location of connections[0] (one)") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestStorageConnectionUserinfo(t *testing.T) {
	t.Run("R-OPS-013 the URL keys of a connection and of the shared client refuse user information and hide the password", func(t *testing.T) {
		for _, c := range []struct{ yaml, key string }{
			{"storage:\n  connections:\n    - {bucket: a, endpoint: \"https://ak:s3cr3t-pw@s3.example\"}\n", "storage.connections[0] (default): endpoint"},
			{"storage:\n  connections:\n    - {bucket: a, sts_endpoint: \"https://ak:s3cr3t-pw@sts.example\"}\n", "storage.connections[0] (default): sts_endpoint"},
			{"storage:\n  connections:\n    - {bucket: a}\n  object_store:\n    http:\n      proxy_url: \"http://ak:s3cr3t-pw@proxy.example:3128\"\n", "storage.object_store.http.proxy_url"},
		} {
			_, err := loadConnections(t, c.yaml)
			if err == nil || !strings.Contains(err.Error(), c.key) || strings.Contains(err.Error(), "s3cr3t-pw") {
				t.Errorf("%s: %v", c.key, err)
			}
		}
	})
}

func TestStorageEndpointScheme(t *testing.T) {
	t.Run("R-STOR-030 an endpoint with a scheme and the empty endpoint are left alone, a host means https", func(t *testing.T) {
		for in, want := range map[string]string{"": "", "http://s3.local:9000": "http://s3.local:9000", "https://s3.example.com": "https://s3.example.com", "s3.example.com:9000": "https://s3.example.com:9000"} {
			if got := NormalizeEndpoint(in); got != want {
				t.Errorf("NormalizeEndpoint(%q) = %q, want %q", in, got, want)
			}
		}
	})
}
