package job

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/key"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// CheckUnreadable is the refusal of files that cannot be read (a Secret not mounted, a path that is no file): the admin page shows it
// like a pair that fails R-SIG-045 (R-SIG-046).
const CheckUnreadable = "unreadable"

// FileStore is what the watcher needs of the store; *store.Store implements it.
type FileStore interface {
	Identities(ctx context.Context) ([]signing.Identity, error)
	RegisterFiles(ctx context.Context, c signing.Checked) (signing.Identity, bool, error)
	RecordRefusal(ctx context.Context, contentSHA [32]byte, r *signing.Refusal, fingerprint string) error
	ConfigurationGone(ctx context.Context) (bool, error)
	Backlog(ctx context.Context) ([]store.Backlog, error)
}

var _ FileStore = (*store.Store)(nil)

// Files are the identity's files of the Helm values (signing.identity.cert_file, key_file, ca_file) and how often they are read again
// (signing.identity.reload_interval).
type Files struct {
	Cert, Key, CA string
	Interval      time.Duration
}

// WatcherOptions configure [NewWatcher].
type WatcherOptions struct {
	Store   FileStore
	Keys    *Holder
	Files   Files
	Metrics *Metrics
	Log     *slog.Logger
	Now     func() time.Time
}

// Watcher is the worker's look at its signing identity (R-SIG-046, R-SIG-057): at the start and every reload_interval it reads the files
// of the Helm values and compares their content, registers a pair that passed R-SIG-045 (its key into the holder), records a refused
// one once per content, lets the identity of the files go at the start when the values name none, and sets the gauges of the identities
// and of the backlog. Nothing rolls: the kubelet swaps the volume and the next look reads it.
type Watcher struct {
	o WatcherOptions

	mu   sync.Mutex
	last [32]byte

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWatcher returns the watcher.
func NewWatcher(o WatcherOptions) *Watcher {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Files.Interval <= 0 {
		o.Files.Interval = time.Minute
	}
	return &Watcher{o: o}
}

// Name implements the composition root's component.
func (w *Watcher) Name() string { return "signing-identity" }

// Start looks once, then every reload_interval. A look that fails is logged; the identity in use keeps signing.
func (w *Watcher) Start(ctx context.Context) error {
	w.Look(ctx, true)
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		t := time.NewTicker(w.o.Files.Interval)
		defer t.Stop()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.C:
				w.Look(lctx, false)
			}
		}
	}()
	return nil
}

// Stop ends the loop.
func (w *Watcher) Stop(context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	return nil
}

// Look reads the files and registers or refuses a changed pair (first: the start, which also lets the identity of the files go when
// the values name none), then sets the gauges.
func (w *Watcher) Look(ctx context.Context, first bool) {
	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch {
	case w.o.Files.Cert != "":
		if err := w.files(lctx); err != nil && ctx.Err() == nil {
			w.o.Log.Warn("the signing identity's files were not registered; the identity in use keeps signing", "error", err)
		}
	case first:
		if changed, err := w.o.Store.ConfigurationGone(lctx); err != nil {
			w.o.Log.Warn("the identity of the files was not let go", "error", err)
		} else if changed {
			w.o.Log.Info("the Helm values name no signing identity any more: the identity of the files is previous")
		}
	}
	w.gauges(lctx)
}

// files reads the three files; an unchanged content is not registered again.
func (w *Watcher) files(ctx context.Context) error {
	certPEM, certErr := readPEM(w.o.Files.Cert)
	caPEM, caErr := readPEM(w.o.Files.CA)
	k, keyDigest, keyErr := key.ReadFile(w.o.Files.Key)
	content := digest(certPEM, caPEM, keyDigest[:], errText(certErr), errText(caErr))
	w.mu.Lock()
	same := content == w.last
	w.mu.Unlock()
	if same {
		return nil
	}
	refusal, fingerprint, checked := w.check(certPEM, caPEM, k, certErr, caErr, keyErr)
	if refusal == nil {
		if err := w.o.Keys.AddFileKey(k); err != nil {
			return err
		}
		id, installed, err := w.o.Store.RegisterFiles(ctx, checked)
		if r := signing.RefusalOf(err); r != nil {
			refusal = r
		} else if err != nil {
			return err
		} else if installed {
			w.o.Log.Info("the signing identity of the files is registered", "identity", id.ID, "state", id.State, "fingerprint", id.Fingerprint())
		}
	}
	if refusal != nil {
		w.o.Log.Warn("the signing identity's files are refused; the identity in use keeps signing", "check", refusal.Check, "reason", refusal.Detail)
		if err := w.o.Store.RecordRefusal(ctx, content, refusal, fingerprint); err != nil {
			return err
		}
	}
	w.mu.Lock()
	w.last = content
	w.mu.Unlock()
	return nil
}

// check holds the files to R-SIG-045 and answers the refusal (nil when they pass), the leaf's fingerprint when it parsed, and the
// checked material.
func (w *Watcher) check(certPEM, caPEM []byte, k key.Private, certErr, caErr, keyErr error) (*signing.Refusal, string, signing.Checked) {
	for _, err := range []error{certErr, caErr} {
		if err != nil {
			return &signing.Refusal{Check: CheckUnreadable, Detail: err.Error()}, "", signing.Checked{}
		}
	}
	chain, err := signing.ParseChain(certPEM, caPEM)
	if err != nil {
		return refusalOf(err), "", signing.Checked{}
	}
	fp := store.Fingerprint(chain.Leaf.Raw)
	if keyErr != nil {
		if r := signing.RefusalOf(keyErr); r != nil {
			return r, fp, signing.Checked{}
		}
		return &signing.Refusal{Check: CheckUnreadable, Detail: keyErr.Error()}, fp, signing.Checked{}
	}
	checked, err := chain.Check(k.Public(), w.o.Now())
	if err != nil {
		return refusalOf(err), fp, signing.Checked{}
	}
	return nil, fp, checked
}

func refusalOf(err error) *signing.Refusal {
	if r := signing.RefusalOf(err); r != nil {
		return r
	}
	return &signing.Refusal{Check: signing.CheckMalformed, Detail: err.Error()}
}

// gauges sets the identities' gauges and the backlog's, and drops the opened keys of identities that stopped signing.
func (w *Watcher) gauges(ctx context.Context) {
	ids, err := w.o.Store.Identities(ctx)
	if err != nil {
		return
	}
	w.o.Keys.Forget(ids)
	loaded := false
	for _, i := range ids {
		if i.State == signing.StateCurrent {
			_, ok, err := w.o.Keys.For(ctx, i)
			loaded = ok && err == nil
		}
	}
	w.o.Metrics.identities(ids, loaded)
	if rows, err := w.o.Store.Backlog(ctx); err == nil {
		w.o.Metrics.work(rows, w.o.Now())
	}
}

// maxPEMFile bounds a certificate file of the Helm values: 8 certificates of 16 KiB in PEM.
const maxPEMFile = 256 << 10

// readPEM reads a certificate file; the empty path is no file and no error.
func readPEM(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	st, err := os.Stat(path)
	switch {
	case err != nil:
		return nil, errors.New("the file " + path + " cannot be read: " + err.Error())
	case st.Size() > maxPEMFile:
		return nil, errors.New("the file " + path + " is larger than 256 KiB")
	}
	b, err := os.ReadFile(path) //nolint:gosec // the operator's configured path
	if err != nil {
		return nil, errors.New("the file " + path + " cannot be read: " + err.Error())
	}
	return b, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// digest is the content of a look: the certificate and CA files, the key file's digest and the read errors, each length-prefixed.
func digest(parts ...any) [32]byte {
	h := sha256.New()
	for _, p := range parts {
		var b []byte
		switch v := p.(type) {
		case []byte:
			b = v
		case string:
			b = []byte(v)
		}
		h.Write([]byte(strconv.Itoa(len(b)) + ":"))
		h.Write(b)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
