// Package dbloader is the loader of the image scanner's database (docs/spec/vulnerability-scanning.md R-VULN-049, Q-VULN-196,
// architecture.md Q-ARCH-564): the role scanner of the kind trivy-db, the container database of a pod of vuln-scanner-trivy. It asks the
// scan listener of packages which database is in use, reads the layer of a database its pod does not hold through the same listener with
// the pod's certificate and no token, hashes it while it unpacks it (imagescan.UnpackDatabase, the code the worker used before), and
// renames the directory <dir>/<64 hex> into place only when it is complete. The scanner's container mounts the directory read-only, so no
// analysis and no detection can change what judges the next image. It keeps the newest database and the one before it, for calls that
// name the previous one while the worker has not read the new one; it removes the older ones before it unpacks a new one. It holds the
// pod's TLS pair and the internal CA and writes nothing but its directory.
package dbloader

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// The waits of the loader's loop (R-VULN-049): it asks the listing every 5 seconds while it holds no database and every 30 seconds once it
// holds one.
const (
	DefaultPollBefore = 5 * time.Second
	DefaultPollAfter  = 30 * time.Second
	// unpackPrefix names the directory a load unpacks into before it is renamed; a loader that starts removes what one that stopped left.
	unpackPrefix = "unpack-"
	// maxListing bounds the listing's answer.
	maxListing = 4 << 10
)

// Listener is the scan listener of packages (vulnerabilities.images.registry.url) and the TLS of the link: the pod's pair as the client
// certificate and the internal CA as the only roots (httpserver.LinkTLS). TLS nil is plain HTTP (an installation without TLS).
type Listener struct {
	URL string
	TLS func() *tls.Config
}

// Options configure [New].
type Options struct {
	Listener Listener
	// Dir is vulnerabilities.images.database.dir, the volume trivy-db of the pod.
	Dir string
	// Budget is the room of the databases' files in Dir (vulnerabilities.images.database.dir_budget; the chart sets it to the volume's
	// size, vulnScannerTrivy.database.sizeLimit). The sizes of the two files of a database are held from the archive's headers before a
	// byte is written.
	Budget int64
	// Timeout bounds one load: the read of the layer and its unpacking (vulnerabilities.images.database.timeout).
	Timeout time.Duration
	// PollBefore and PollAfter are the waits of the loop before and after the first database; zero takes the defaults.
	PollBefore, PollAfter time.Duration
	// HTTP replaces the link's client; tests only.
	HTTP *http.Client
	Log  *slog.Logger
}

// Loader loads the database in use into its directory.
type Loader struct {
	o    Options
	http *http.Client
	mu   sync.Mutex
	// held is the room the databases in Dir hold, by the 64 hexadecimal digits of their digests; pending the room of the load in progress.
	held    map[string]int64
	pending int64
	// loaded is the digest of the newest database the loader completed or found, "" before.
	loaded string
}

// New returns the loader. It removes what an interrupted load left in Dir, and counts the complete databases it finds there against the
// budget at the size they have on the disk: a container that restarts keeps its volume.
func New(o Options) (*Loader, error) {
	switch {
	case o.Listener.URL == "":
		return nil, errors.New("dbloader: the scan listener's address is required (vulnerabilities.images.registry.url)")
	case o.Dir == "":
		return nil, errors.New("dbloader: the database's directory is required (vulnerabilities.images.database.dir)")
	case o.Budget <= 0:
		return nil, errors.New("dbloader: the budget of the database's directory must be positive")
	}
	if o.PollBefore <= 0 {
		o.PollBefore = DefaultPollBefore
	}
	if o.PollAfter <= 0 {
		o.PollAfter = DefaultPollAfter
	}
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Minute
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	l := &Loader{o: o, http: o.HTTP, held: map[string]int64{}}
	if l.http == nil {
		l.http = outbound.LinkClient(outbound.LinkOptions{TLS: o.Listener.TLS})
	}
	o.Listener.URL = strings.TrimSuffix(o.Listener.URL, "/")
	l.o = o
	if err := l.scan(); err != nil {
		return nil, err
	}
	return l, nil
}

// scan reads Dir at start: a leftover of a load goes, a complete database is counted at its size on the disk, anything else is left alone
// and logged (the volume is the loader's alone, so such an entry is an operator's).
func (l *Loader) scan() error {
	if err := os.MkdirAll(l.o.Dir, 0o700); err != nil {
		return fmt.Errorf("the database's directory %s: %w", l.o.Dir, err)
	}
	entries, err := os.ReadDir(l.o.Dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(l.o.Dir, e.Name())
		switch {
		case e.IsDir() && strings.HasPrefix(e.Name(), unpackPrefix):
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("the leftover %s of an interrupted load: %w", path, err)
			}
		case e.IsDir() && imagescan.ValidDigest("sha256:"+e.Name()):
			if _, ok := imagescan.DatabaseIn(path); !ok {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				continue
			}
			l.held[e.Name()] = imagescan.DirBytes(path)
		default:
			l.o.Log.Warn("the database's directory holds an entry the loader did not write; it is left alone", "entry", e.Name())
		}
	}
	l.loaded = l.newestLocked("")
	return nil
}

// Held reports the databases the directory holds, by digest, newest first; for the tests and the logs.
func (l *Loader) Held() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.byAgeLocked()
}

// Bytes is the room the databases hold.
func (l *Loader) Bytes() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var n int64
	for _, b := range l.held {
		n += b
	}
	return n
}

// Run asks the listing until ctx ends: every PollBefore while the loader holds no database, every PollAfter once it holds one. A failed
// round is logged and tried again at the next one.
func (l *Loader) Run(ctx context.Context) {
	for {
		if err := l.Once(ctx); err != nil && ctx.Err() == nil {
			l.o.Log.Warn("the image scanner's database could not be loaded; the loader asks again", "error", err)
		}
		wait := l.o.PollAfter
		l.mu.Lock()
		if l.loaded == "" {
			wait = l.o.PollBefore
		}
		l.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// Once is one round: the listing, and the load of the database in use when the directory does not hold it.
func (l *Loader) Once(ctx context.Context) error {
	listing, err := l.listing(ctx)
	if err != nil {
		return err
	}
	if listing.Digest == "" {
		return nil // no database has been imported yet
	}
	if !imagescan.ValidDigest(listing.Digest) {
		return fmt.Errorf("the scan listener names the database %q, which is no sha256 digest", cut(listing.Digest, 80))
	}
	hex := strings.TrimPrefix(listing.Digest, "sha256:")
	l.mu.Lock()
	_, have := l.held[hex]
	l.mu.Unlock()
	if have {
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, l.o.Timeout)
	defer cancel()
	return l.load(lctx, listing.Digest)
}

// load makes the directory hold the database digest. The databases older than the newest go first; a database that does not fit beside
// the newest replaces it, and detections answer database_loading meanwhile.
func (l *Loader) load(ctx context.Context, digest string) error {
	hex := strings.TrimPrefix(digest, "sha256:")
	l.dropOlder()
	started := time.Now()
	u, err := l.unpack(ctx, digest)
	if errors.Is(err, errNoRoom) {
		newest := l.Held()
		if len(newest) == 0 {
			return err
		}
		l.o.Log.Warn("the database's volume holds one database only: the newest goes before the new one is loaded, and detections answer database_loading until it is complete",
			"removed", newest[0], "database", digest, "budget", l.o.Budget)
		if rerr := l.remove(newest[0]); rerr != nil {
			return rerr
		}
		u, err = l.unpack(ctx, digest)
	}
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.held[hex] = u.Bytes
	l.loaded = hex
	l.mu.Unlock()
	u.Release() // the room moves from the load to the database
	l.o.Log.Info("the image scanner's database is loaded", "database", digest, "built", u.Metadata.UpdatedAt, "bytes", u.Bytes, "took", time.Since(started).Round(time.Millisecond).String())
	return nil
}

// errNoRoom is a database whose files do not fit the room left beside the databases the directory holds.
var errNoRoom = errors.New("the database's volume has no room for the database")

// unpack reads the layer and unpacks it into a directory of its own, renamed to <dir>/<64 hex> once it is complete and checked.
func (l *Loader) unpack(ctx context.Context, digest string) (imagescan.Unpacked, error) {
	hex := strings.TrimPrefix(digest, "sha256:")
	tmp, err := os.MkdirTemp(l.o.Dir, unpackPrefix)
	if err != nil {
		return imagescan.Unpacked{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(tmp)
		}
	}()
	body, err := l.layer(ctx, digest)
	if err != nil {
		return imagescan.Unpacked{}, err
	}
	defer func() { _ = body.Close() }()
	u, err := imagescan.UnpackDatabase(ctx, body, digest, tmp, l.hold)
	if err != nil {
		return imagescan.Unpacked{}, err
	}
	dst := filepath.Join(l.o.Dir, hex)
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		u.Release()
		return imagescan.Unpacked{}, err
	}
	keep = true
	return u, nil
}

// hold takes n bytes of the budget, or answers errNoRoom.
func (l *Loader) hold(n int64) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var used int64
	for _, b := range l.held {
		used += b
	}
	used += l.pending
	if used+n > l.o.Budget {
		return nil, fmt.Errorf("%w: %d bytes held of %d, %d more asked", errNoRoom, used, l.o.Budget, n)
	}
	l.pending += n
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.pending -= n
			l.mu.Unlock()
		})
	}, nil
}

// dropOlder removes every database but the newest: the loader keeps the newest and, once the new one is complete, the one before it.
func (l *Loader) dropOlder() {
	all := l.Held()
	if len(all) < 2 {
		return
	}
	for _, hex := range all[1:] {
		if err := l.remove(hex); err != nil {
			l.o.Log.Warn("an older database could not be removed", "database", hex, "error", err)
		}
	}
}

// remove deletes a database's directory and gives its room back.
func (l *Loader) remove(hex string) error {
	if err := os.RemoveAll(filepath.Join(l.o.Dir, hex)); err != nil {
		return err
	}
	l.mu.Lock()
	delete(l.held, hex)
	if l.loaded == hex {
		l.loaded = l.newestLocked(hex)
	}
	l.mu.Unlock()
	return nil
}

// byAgeLocked lists the databases newest first, by the time the loader wrote them (imagescan.LoadedAt, Q-VULN-213): the newest is the
// database the listing named last, the second the one before it.
func (l *Loader) byAgeLocked() []string {
	type db struct {
		hex    string
		loaded time.Time
	}
	var all []db
	for hex := range l.held {
		at, _ := imagescan.LoadedAt(filepath.Join(l.o.Dir, hex))
		all = append(all, db{hex, at})
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].loaded.Equal(all[j].loaded) {
			return all[i].loaded.After(all[j].loaded)
		}
		return all[i].hex < all[j].hex
	})
	out := make([]string, 0, len(all))
	for _, d := range all {
		out = append(out, d.hex)
	}
	return out
}

// newestLocked is the newest database but skip, "" for none.
func (l *Loader) newestLocked(skip string) string {
	for _, hex := range l.byAgeLocked() {
		if hex != skip {
			return hex
		}
	}
	return ""
}

// listing reads which database is in use.
func (l *Loader) listing(ctx context.Context) (imagescan.DatabaseListing, error) {
	res, err := l.get(ctx, registrytoken.TrivyDatabasePath)
	if err != nil {
		return imagescan.DatabaseListing{}, err
	}
	defer func() { _ = res.Body.Close() }()
	var out imagescan.DatabaseListing
	if err := json.NewDecoder(io.LimitReader(res.Body, maxListing)).Decode(&out); err != nil {
		return imagescan.DatabaseListing{}, fmt.Errorf("the scan listener's listing of the database: %w", err)
	}
	return out, nil
}

// layer opens the layer of digest; the caller closes it. A layer the listener does not serve (no longer in use, or not held by its
// repository) is an error the next round may not have.
func (l *Loader) layer(ctx context.Context, digest string) (io.ReadCloser, error) {
	res, err := l.get(ctx, registrytoken.TrivyDatabasePath+"/"+digest)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (l *Loader) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.o.Listener.URL+path, nil)
	if err != nil {
		return nil, err
	}
	res, err := l.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the scan listener of packages: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, fmt.Errorf("the scan listener answered %s %d", path, res.StatusCode)
	}
	return res, nil
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
