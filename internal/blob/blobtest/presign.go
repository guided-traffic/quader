package blobtest

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// The signed links of the in-memory store (storage.md R-STOR-072): Presign signs a GET with a key of the store's own, and the
// store is an http.RoundTripper that answers the link as an object store would: a bad or expired signature is 403, a request
// that carries a credential beside the signature is 400, and the store's answer carries the three overridden headers unless a
// test told it to ignore one (the store fake of R-STOR-074's link check). Links are path-style: the host is the address's.

// PresignHost is the host of the links Presign returns when no address is given: the store's own endpoint,
// https://mem.invalid.
const PresignHost = "mem.invalid"

// The query parameters of a link, named as Signature Version 4 names them.
const (
	qExpires   = "X-Amz-Expires"
	qDate      = "X-Amz-Date"
	qSignature = "X-Amz-Signature"
)

// linkSigner is the store's signing state.
type linkSigner struct {
	once    sync.Once
	key     []byte
	mu      sync.Mutex
	expires time.Time       // the credential's expiry, zero for none
	ignore  map[string]bool // overrides the store's answer ignores, by query parameter
}

func (l *linkSigner) secret() []byte {
	l.once.Do(func() {
		l.key = make([]byte, 32)
		_, _ = rand.Read(l.key)
	})
	return l.key
}

// SetCredentialExpiry makes the store's signing credential a temporary one that expires at t (the zero time: none), so that
// blob.LinkLife bounds the links it signs (R-STOR-072).
func (s *Store) SetCredentialExpiry(t time.Time) {
	s.links.mu.Lock()
	s.links.expires = t
	s.links.mu.Unlock()
}

// IgnoreOverride makes the store's answer to a link ignore one of the overrides, named by its query parameter
// ("response-content-disposition"), as a store that does not honour it would.
func (s *Store) IgnoreOverride(param string) {
	s.links.mu.Lock()
	if s.links.ignore == nil {
		s.links.ignore = map[string]bool{}
	}
	s.links.ignore[param] = true
	s.links.mu.Unlock()
}

// Do answers a request for one of the store's links as RoundTrip does: the store is the blob.Fetcher of its own links (the link
// check, a test's client), and no request leaves the process.
func (s *Store) Do(req *http.Request) (*http.Response, error) { return s.RoundTrip(req) }

// Presign implements blob.ObjectStore: it counts as the operation "presign" and asks the fault function with it.
func (s *Store) Presign(_ context.Context, key string, o blob.PresignOptions) (blob.Link, error) {
	if err := s.enter("presign", key); err != nil {
		return blob.Link{}, err
	}
	now := s.Now()
	s.links.mu.Lock()
	expires := s.links.expires
	s.links.mu.Unlock()
	life, err := blob.LinkLife(now, o.Life, expires)
	if err != nil {
		return blob.Link{}, err
	}
	addr := o.Address
	if addr == "" {
		addr = "https://" + PresignHost
	}
	u, err := url.Parse(strings.TrimSuffix(addr, "/") + "/" + key)
	if err != nil {
		return blob.Link{}, &blob.StoreError{Op: "PresignGetObject", Key: key, Kind: blob.KindOther, Err: err}
	}
	q := url.Values{}
	q.Set(qExpires, strconv.Itoa(int(life/time.Second)))
	q.Set(qDate, strconv.FormatInt(now.Unix(), 10))
	for param, v := range map[string]string{
		"response-content-type": o.ContentType, "response-content-disposition": o.ContentDisposition, "response-cache-control": o.CacheControl,
	} {
		if v != "" {
			q.Set(param, v)
		}
	}
	q.Set(qSignature, s.links.sign(u.Host, u.Path, q))
	u.RawQuery = q.Encode()
	return blob.Link{URL: u.String(), ExpiresAt: now.Add(life)}, nil
}

// sign is the signature of a GET of the host and path with the query, without its signature parameter.
func (l *linkSigner) sign(host, path string, q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k != qSignature {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	m := hmac.New(sha256.New, l.secret())
	_, _ = io.WriteString(m, "GET\n"+host+"\n"+path+"\n")
	for _, k := range keys {
		_, _ = io.WriteString(m, k+"="+q.Get(k)+"\n")
	}
	return hex.EncodeToString(m.Sum(nil))
}

// RoundTrip answers a link Presign signed, as an object store would: 403 for a signature that does not match or a link past its
// expiry, 400 for a request that carries an Authorization header beside the signature, 405 for a method other than GET, 404
// for a missing object, and 200 with the bytes and the overridden headers the store does not ignore.
func (s *Store) RoundTrip(req *http.Request) (*http.Response, error) {
	answer := func(code int, body []byte) *http.Response {
		return &http.Response{
			Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, StatusCode: code, Status: strconv.Itoa(code) + " " + http.StatusText(code),
			Header: http.Header{}, Request: req, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)),
		}
	}
	if req.Body != nil {
		_ = req.Body.Close()
	}
	q := req.URL.Query()
	switch {
	case req.Method != http.MethodGet:
		return answer(http.StatusMethodNotAllowed, nil), nil
	case req.Header.Get("Authorization") != "":
		return answer(http.StatusBadRequest, []byte("only one auth mechanism allowed")), nil
	case !hmac.Equal([]byte(q.Get(qSignature)), []byte(s.links.sign(req.URL.Host, req.URL.Path, q))):
		return answer(http.StatusForbidden, []byte("SignatureDoesNotMatch")), nil
	}
	date, _ := strconv.ParseInt(q.Get(qDate), 10, 64)
	life, _ := strconv.Atoi(q.Get(qExpires))
	if s.Now().After(time.Unix(date, 0).Add(time.Duration(life) * time.Second)) {
		return answer(http.StatusForbidden, []byte("Request has expired")), nil
	}
	key := strings.TrimPrefix(req.URL.Path, "/")
	s.mu.Lock()
	o, ok := s.objects[key]
	var data []byte
	if ok {
		data = bytes.Clone(o.data)
	}
	s.mu.Unlock()
	if !ok {
		return answer(http.StatusNotFound, nil), nil
	}
	resp := answer(http.StatusOK, data)
	resp.Header.Set("Content-Type", "application/octet-stream")
	resp.Header.Set("ETag", etag(data))
	s.links.mu.Lock()
	defer s.links.mu.Unlock()
	for param, header := range map[string]string{
		"response-content-type": "Content-Type", "response-content-disposition": "Content-Disposition", "response-cache-control": "Cache-Control",
	} {
		if v := q.Get(param); v != "" && !s.links.ignore[param] {
			resp.Header.Set(header, v)
		}
	}
	return resp, nil
}
