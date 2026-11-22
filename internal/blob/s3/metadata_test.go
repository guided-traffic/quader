package s3_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
)

// metaOf is the x-amz-meta-* headers of a request, lower case, without the prefix.
func metaOf(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if name, ok := strings.CutPrefix(strings.ToLower(k), "x-amz-meta-"); ok && len(v) > 0 {
			out[name] = v[0]
		}
	}
	return out
}

func (f *fakeS3) setMeta(key string, md map[string]string) {
	if f.meta == nil {
		f.meta = map[string]map[string]string{}
	}
	if len(md) == 0 {
		delete(f.meta, key)
		return
	}
	f.meta[key] = md
}

// copyObject answers CopyObject inside the fake's bucket b: the source's bytes, and its metadata kept with the directive COPY
// or replaced by the request's with REPLACE.
func (f *fakeS3) copyObject(w http.ResponseWriter, r *http.Request, dst, src string) {
	src, _ = url.PathUnescape(strings.TrimPrefix(src, "/"))
	src = strings.TrimPrefix(src, "b/")
	replace := strings.EqualFold(r.Header.Get("x-amz-metadata-directive"), "REPLACE")
	f.mu.Lock()
	b, ok := f.objects[src]
	md := f.meta[src]
	if ok && (replace || src != dst) {
		if replace {
			md = metaOf(r.Header)
		}
		f.objects[dst] = append([]byte(nil), b...)
		f.setMeta(dst, md)
	}
	f.mu.Unlock()
	switch {
	case !ok:
		s3Error(w, r, 404, "NoSuchKey")
		return
	case !replace && src == dst:
		s3Error(w, r, 400, "InvalidRequest") // S3 refuses a copy onto itself that changes nothing
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`))
}

func TestR_STOR_062_TheAdapterCarriesObjectMetadata(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-062 metadata written by Put comes back from Head and Get, a copy with nil keeps it and a copy with a value replaces it", func(t *testing.T) {
		e := newEnv(t, nil)
		data := []byte("0123456789")
		if err := e.client.Put(ctx, "k", strings.NewReader(string(data)), int64(len(data)), blob.Checksum{}, blob.Metadata{"quader-key": "wrapped"}); err != nil {
			t.Fatal(err)
		}
		info, err := e.client.Head(ctx, "k")
		if err != nil || info.Metadata["quader-key"] != "wrapped" {
			t.Fatalf("head: %+v %v", info, err)
		}
		body, info, err := e.client.Get(ctx, "k", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = body.Close()
		if info.Metadata["quader-key"] != "wrapped" {
			t.Fatalf("get: %+v", info)
		}
		if err := e.client.Copy(ctx, "k", "kept", int64(len(data)), nil); err != nil {
			t.Fatal(err)
		}
		if info, _ := e.client.Head(ctx, "kept"); info.Metadata["quader-key"] != "wrapped" {
			t.Fatalf("a copy with nil metadata: %+v", info)
		}
		if err := e.client.Copy(ctx, "k", "k", int64(len(data)), blob.Metadata{"quader-key": "rewrapped"}); err != nil {
			t.Fatal(err)
		}
		if info, _ := e.client.Head(ctx, "k"); info.Metadata["quader-key"] != "rewrapped" {
			t.Fatalf("a copy with replaced metadata: %+v", info)
		}
	})
	t.Run("R-STOR-062 CreateMultipart sends the metadata of the object it makes", func(t *testing.T) {
		e := newEnv(t, nil)
		var got map[string]string
		e.fake.fail = func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method != http.MethodPost || !r.URL.Query().Has("uploads") {
				return false
			}
			got = metaOf(r.Header)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>b</Bucket><Key>m</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`))
			return true
		}
		if _, err := e.client.CreateMultipart(ctx, "m", "", blob.Metadata{"quader-key": "wrapped"}); err != nil {
			t.Fatal(err)
		}
		if got["quader-key"] != "wrapped" {
			t.Fatalf("the create request carried %v", got)
		}
	})
	t.Run("R-STOR-062 the adapter reports its location as the location rule compares it", func(t *testing.T) {
		e := newEnv(t, nil)
		endpoint, bucket := e.client.Location()
		if bucket != "b" || endpoint != strings.ToLower(strings.TrimSuffix(e.srv.URL, "/")) {
			t.Fatalf("location %q %q", endpoint, bucket)
		}
	})
}
