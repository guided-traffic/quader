package s3_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
)

// sseRequest is what the store was asked: the request and the three headers of server-side encryption.
type sseRequest struct {
	what                 string
	mode, key, bucketKey string
}

// TestR_STOR_032_EveryWriteCarriesTheConnectionsEncryption: the object store's own encryption of a connection (sse_mode, sse_kms_key_id,
// sse_bucket_key) is sent on every request that makes an object, PutObject, CreateMultipartUpload and CopyObject, and on the
// CreateMultipartUpload of a copy in parts, as R-STOR-032 requires; with sse_mode none no request carries it, and no request that does not
// make an object (UploadPart, UploadPartCopy, CompleteMultipartUpload, HeadObject, GetObject) carries it in any mode, since S3 refuses or
// ignores it there. The settings come from a connection's element through config.ClientConfig, the path a replica's client takes. Unit,
// milliseconds, the store a fake of a few bytes; no object storage provider is called.
func TestR_STOR_032_EveryWriteCarriesTheConnectionsEncryption(t *testing.T) {
	ctx := context.Background()
	on, off := true, false
	for _, c := range []struct {
		name            string
		conn            config.StorageConnection
		mode, key, bkey string
	}{
		{"none", config.StorageConnection{Bucket: "b", SSEMode: "none"}, "", "", ""},
		{"s3", config.StorageConnection{Bucket: "b", SSEMode: "s3"}, "AES256", "", ""},
		{"kms with a bucket key", config.StorageConnection{Bucket: "b", SSEMode: "kms", SSEKMSKeyID: "arn:aws:kms:eu-central-1:123456789012:key/k1", SSEBucketKey: &on}, "aws:kms", "arn:aws:kms:eu-central-1:123456789012:key/k1", "true"},
		{"kms without a bucket key", config.StorageConnection{Bucket: "b", SSEMode: "kms", SSEKMSKeyID: "k2", SSEBucketKey: &off}, "aws:kms", "k2", "false"},
	} {
		t.Run("R-STOR-032 sse_mode "+c.name, func(t *testing.T) {
			sse := config.ClientConfig(0, c.conn, config.StorageObjectStore{}).SSE
			e := newEnv(t, func(s *config.StorageS3) { s.SSE = sse })
			var mu sync.Mutex
			var seen []sseRequest
			rec := &copyRecorder{}
			e.fake.fail = func(w http.ResponseWriter, r *http.Request) bool {
				q := r.URL.Query()
				what := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/b/")
				switch {
				case q.Has("uploads"):
					what += " CreateMultipartUpload"
				case q.Has("partNumber") && r.Header.Get("x-amz-copy-source") != "":
					what += " UploadPartCopy"
				case q.Has("partNumber"):
					what += " UploadPart"
				case q.Has("uploadId"):
					what += " CompleteMultipartUpload"
				case r.Header.Get("x-amz-copy-source") != "":
					what += " CopyObject"
				}
				mu.Lock()
				seen = append(seen, sseRequest{
					what: what, mode: r.Header.Get("x-amz-server-side-encryption"),
					key: r.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"), bucketKey: r.Header.Get("x-amz-server-side-encryption-bucket-key-enabled"),
				})
				mu.Unlock()
				if q.Has("uploads") || q.Has("partNumber") || q.Has("uploadId") {
					if q.Has("partNumber") && r.Header.Get("x-amz-copy-source") == "" {
						w.Header().Set("ETag", `"p"`)
						return true
					}
					return rec.handle(w, r)
				}
				return false
			}
			data := []byte("0123456789")
			if err := put(e.client, "src", data, blob.Checksum{}); err != nil {
				t.Fatal(err)
			}
			id, err := e.client.CreateMultipart(ctx, "m", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			part, err := e.client.UploadPart(ctx, "m", id, 1, bytes.NewReader(data), int64(len(data)), blob.Checksum{})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.client.CompleteMultipart(ctx, "m", id, []blob.Part{part}); err != nil {
				t.Fatal(err)
			}
			if err := e.client.Copy(ctx, "src", "one", int64(len(data)), nil); err != nil {
				t.Fatal(err)
			}
			e.client.SetCopyLimits(5, 4)
			if err := e.client.Copy(ctx, "src", "parts", int64(len(data)), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := e.client.Head(ctx, "src"); err != nil {
				t.Fatal(err)
			}
			body, _, err := e.client.Get(ctx, "src", nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = body.Close()

			makes := map[string]bool{
				"PUT src": true, "POST m CreateMultipartUpload": true, "PUT one CopyObject": true, "POST parts CreateMultipartUpload": true,
			}
			found := map[string]bool{}
			mu.Lock()
			defer mu.Unlock()
			for _, s := range seen {
				want := sseRequest{what: s.what}
				if makes[s.what] {
					found[s.what] = true
					want.mode, want.key, want.bucketKey = c.mode, c.key, c.bkey
				}
				if s != want {
					t.Errorf("%s carried encryption %q, key %q, bucket key %q; want %q, %q, %q", s.what, s.mode, s.key, s.bucketKey, want.mode, want.key, want.bucketKey)
				}
			}
			for what := range makes {
				if !found[what] {
					t.Errorf("no request %s; the store saw %v", what, seen)
				}
			}
			if len(seen) < 10 {
				t.Errorf("the store saw %d requests, want the eleven of the writes and the reads: %v", len(seen), seen)
			}
		})
	}
}

// TestR_STOR_035_TheStoreAppliedEncryption: the client reads the server-side encryption the store reports for an object
// (x-amz-server-side-encryption and its KMS key) from the answers of HeadObject and GetObject, which the start check's probe records
// as capabilities.sseApplied (R-STOR-032, Q-STOR-519); a store that reports none gives the empty algorithm. Unit, milliseconds, a
// fake store of a few bytes.
func TestR_STOR_035_TheStoreAppliedEncryption(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, alg, key string
	}{
		{"aws:kms with its key", "aws:kms", "arn:aws:kms:eu-central-1:123456789012:key/k1"},
		{"AES256", "AES256", ""},
		{"none reported", "", ""},
	} {
		t.Run("R-STOR-035 "+c.name, func(t *testing.T) {
			e := newEnv(t, nil)
			e.fake.fail = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == http.MethodHead || r.Method == http.MethodGet {
					if c.alg != "" {
						w.Header().Set("x-amz-server-side-encryption", c.alg)
					}
					if c.key != "" {
						w.Header().Set("x-amz-server-side-encryption-aws-kms-key-id", c.key)
					}
				}
				return false
			}
			if err := put(e.client, "probe", []byte("0123456789"), blob.Checksum{}); err != nil {
				t.Fatal(err)
			}
			info, err := e.client.Head(ctx, "probe")
			if err != nil {
				t.Fatal(err)
			}
			body, got, err := e.client.Get(ctx, "probe", nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = body.Close()
			for _, i := range []blob.ObjectInfo{info, got} {
				if i.SSE != c.alg || i.SSEKMSKeyID != c.key {
					t.Errorf("reported %q with key %q, want %q with %q", i.SSE, i.SSEKMSKeyID, c.alg, c.key)
				}
			}
		})
	}
}
