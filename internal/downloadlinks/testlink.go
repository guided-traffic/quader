package downloadlinks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// The test link of the settings page (storage.md R-STOR-074, Q-UI-952): a link to a small probe object on one connection, signed
// against the address the administrator typed, saved or not, which he opens in his browser or fetches with curl from a client of
// his network. Quader sends no request to the address: what the store answers there is what that network gets. An encrypted
// connection is tested too, since a Bastion reads it by a link (R-STOR-096); its probe is plain.

// The probe and its link (R-STOR-074).
const (
	// TestLinkBody is the probe's content, 44 bytes.
	TestLinkBody = "Quader download link test: this link works.\n"
	// TestLinkFile is the file name the link asks the store's answer to carry.
	TestLinkFile = "quader-link-test.txt"
	// TestLinkLife is the link's life; the probe is deleted when it is over.
	TestLinkLife = time.Minute
	// testLinkFamily is the probes' name below the connection's _probe/ (R-STOR-001's family).
	testLinkFamily = "link-test-"
	// testLinkStale is the age of a probe a stopped replica left, which the next test deletes.
	testLinkStale = time.Hour
	// testLinkSweepPages bounds the listing of the probes left behind: a test is an administrator's click, not a sweep.
	testLinkSweepPages = 10
)

// ErrUnknownConnection is a test of a connection the replica does not configure: 422 at /connection.
var ErrUnknownConnection = errors.New("the installation has no such connection")

// ErrConnectionUnavailable is a test of a connection that is failing on this replica (its circuit is open, or the probe could not
// be written): 409 connection_unavailable.
var ErrConnectionUnavailable = errors.New("the connection is failing on this replica")

// TestLink is the answer of a test: the link, its expiry and the command that fetches it.
type TestLink struct {
	URL       string
	ExpiresAt time.Time
	Command   string
}

// AddressError is an address the test refuses as a save would (422 at /publicAddress).
type AddressError struct{ Err error }

func (e *AddressError) Error() string { return e.Err.Error() }
func (e *AddressError) Unwrap() error { return e.Err }

// TestLink writes the probe on the connection, signs a link to it against address (an https origin, checked as a save checks it,
// with the host rule of this replica) and schedules the probe's deletion when the link's life is over; it first deletes the probes
// a stopped replica left (older than an hour). A connection the replica does not configure is ErrUnknownConnection, a failing one
// ErrConnectionUnavailable, an address a save would refuse an *AddressError.
func (l *Links) TestLink(ctx context.Context, conn, address string) (TestLink, error) {
	if l.o.Connections == nil {
		return TestLink{}, ErrUnknownConnection
	}
	c, ok := l.o.Connections.Get(conn)
	if !ok {
		return TestLink{}, ErrUnknownConnection
	}
	origin, _, err := ParseAddress(address)
	if err != nil {
		return TestLink{}, &AddressError{Err: err}
	}
	if err := CheckAddress(origin, l.o.Rule); err != nil {
		return TestLink{}, &AddressError{Err: err}
	}
	if _, rejecting := c.Rejecting(); rejecting {
		return TestLink{}, fmt.Errorf("%w: its circuit is open", ErrConnectionUnavailable)
	}
	l.sweepTestProbes(ctx, c)
	key := testProbePrefix(c.Prefix()) + randomSuffix()
	body := []byte(TestLinkBody)
	if err := c.Objects().Put(ctx, key, bytes.NewReader(body), int64(len(body)), blob.Checksum{}, nil); err != nil {
		return TestLink{}, fmt.Errorf("%w: the probe could not be written: %s", ErrConnectionUnavailable, storeReason(err))
	}
	link, err := c.Presign(ctx, key, blob.PresignOptions{
		Life: TestLinkLife, ContentType: "text/plain; charset=utf-8", CacheControl: CacheControl, Address: origin,
		ContentDisposition: `attachment; filename="` + TestLinkFile + `"`,
	})
	if err != nil {
		l.deleteProbe(c, key)
		if errors.Is(err, blob.ErrCircuitOpen) {
			return TestLink{}, fmt.Errorf("%w: its circuit is open", ErrConnectionUnavailable)
		}
		return TestLink{}, fmt.Errorf("%w: the link could not be signed: %s", ErrConnectionUnavailable, storeReason(err))
	}
	l.after(time.Until(link.ExpiresAt), func() { l.deleteProbe(c, key) })
	return TestLink{URL: link.URL, ExpiresAt: link.ExpiresAt, Command: "curl -fsS '" + link.URL + "'"}, nil
}

// after runs f after d: time.AfterFunc, or what a test set.
func (l *Links) after(d time.Duration, f func()) {
	if l.afterFunc != nil {
		l.afterFunc(d, f)
		return
	}
	time.AfterFunc(max(d, 0), f)
}

// deleteProbe deletes a test's probe in a context of its own: the request that wrote it is long answered.
func (l *Links) deleteProbe(c *blob.Connection, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Objects().Delete(ctx, key); err != nil {
		l.o.Log.Warn("a test link's probe was not deleted; the next test deletes it after an hour", "connection", c.ID(), "key", key, "error", err)
	}
}

// sweepTestProbes deletes the probes of earlier tests that are older than an hour: a replica that stopped within a link's life
// left its probe. A failure is logged and stops nothing.
func (l *Links) sweepTestProbes(ctx context.Context, c *blob.Connection) {
	prefix := testProbePrefix(c.Prefix())
	cutoff := time.Now().Add(-testLinkStale)
	token := ""
	for range testLinkSweepPages {
		page, err := c.Objects().List(ctx, prefix, blob.ListOptions{Token: token})
		if err != nil {
			l.o.Log.Warn("the probes of earlier test links were not listed", "connection", c.ID(), "error", err)
			return
		}
		for _, o := range page.Objects {
			if !o.LastModified.IsZero() && o.LastModified.Before(cutoff) {
				if err := c.Objects().Delete(ctx, o.Key); err != nil {
					l.o.Log.Warn("a probe of an earlier test link was not deleted", "connection", c.ID(), "key", o.Key, "error", err)
				}
			}
		}
		if token = page.Next; token == "" {
			return
		}
	}
}

// testProbePrefix is <prefix>/_probe/link-test- of a connection.
func testProbePrefix(prefix string) string {
	if prefix == "" {
		return "_probe/" + testLinkFamily
	}
	return strings.TrimSuffix(prefix, "/") + "/_probe/" + testLinkFamily
}

func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// storeReason is what a store's refusal says without a link in it: a link is a bearer and goes into no answer (R-STOR-072).
func storeReason(err error) string {
	var se *blob.StoreError
	if errors.As(err, &se) && se.Err != nil {
		return se.Err.Error()
	}
	return err.Error()
}
