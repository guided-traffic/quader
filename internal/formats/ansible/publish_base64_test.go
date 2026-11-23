package ansible

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// base64Publish is the body ansible-core sends: the sha256 part and the archive as a base64 file part in lines of 76 characters.
func base64Publish(t *testing.T, archive []byte) (*bytes.Buffer, string) {
	t.Helper()
	sum := sha256.Sum256(archive)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("sha256", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Content-Transfer-Encoding", "base64")
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="acme-tools-1.0.0.tar.gz"`)
	fw, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.StdEncoding.EncodeToString(archive)
	for len(enc) > 76 {
		_, _ = fw.Write([]byte(enc[:76] + "\n"))
		enc = enc[76:]
	}
	_, _ = fw.Write([]byte(enc + "\n"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

// TestQ_ANS_53_TheArchiveLimitCountsTheDecodedBytesOfABase64Part: ansible.max_archive_bytes bounds the archive, which is the decoded bytes of
// a base64 file part, not the encoded text a third larger: a limit between the two admits the publish, and a limit below the archive refuses
// it with the archive limit's answer (R-ANS-008, R-ANS-031). Limits of kilobytes.
func TestQ_ANS_53_TheArchiveLimitCountsTheDecodedBytesOfABase64Part(t *testing.T) {
	archive := collectiontest.Build(collectionSpec("1.0.0"))
	encoded := int64(base64.StdEncoding.EncodedLen(len(archive)))
	for _, c := range []struct {
		name  string
		limit int64
		want  int
	}{
		{"a limit above the archive and below its encoded text admits it", int64(len(archive)) + 16, http.StatusAccepted},
		{"a limit below the archive refuses it", int64(len(archive)) - 1, http.StatusRequestEntityTooLarge},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.limit >= encoded {
				t.Fatalf("the limit %d does not lie below the encoded text of %d bytes", c.limit, encoded)
			}
			f := New()
			h := formattest.NewHost(f, hostOptions())
			f.Bind(h, &config.Config{Ansible: config.Ansible{
				MaxArchiveBytes: config.ByteSize(c.limit), RoleArchiveMaxBytes: config.ByteSize(256 << 20), ImportTaskKeep: 720 * time.Hour,
			}}, nil)
			body, ct := base64Publish(t, archive)
			r, err := h.Serve(context.Background(), http.MethodPost, "v3/artifacts/collections/", body, false,
				func(r *http.Request) { r.Header.Set("Content-Type", ct) })
			if err != nil {
				t.Fatal(err)
			}
			if r.Code != c.want {
				t.Fatalf("status %d, want %d: %s", r.Code, c.want, r.Body.String())
			}
			if c.want == http.StatusRequestEntityTooLarge {
				if code := errorCode(t, r); code != "payload_too_large" {
					t.Errorf("code %q, want the archive limit's payload_too_large", code)
				}
			}
		})
	}
}
