package job

import (
	"context"
	"sync"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/key"
)

// maxFileKeys is how many keys of the files a worker keeps: the pair in the files and the one before it, which keeps signing until the
// switch after cert-manager replaced the files (R-SIG-048).
const maxFileKeys = 2

// Holder is the worker's keys (R-SIG-049): the keys of the files of the Helm values it loaded (the newest and the one before it), and the
// keys of uploaded identities it opened from the database with the key ring. It is the only holder of a private key in the process; no
// other role builds one.
type Holder struct {
	db   meta.Runner
	ring *secrets.KeyRing

	mu sync.Mutex
	// files are the keys of the files, newest last, by the SHA-256 of their public key.
	files []fileKey
	// opened are the uploaded identities' keys, by identity id.
	opened map[int64]key.Private
}

type fileKey struct {
	sha [32]byte
	k   key.Private
}

// NewHolder returns an empty holder that opens uploaded keys with the ring.
func NewHolder(db meta.Runner, ring *secrets.KeyRing) *Holder {
	return &Holder{db: db, ring: ring, opened: map[int64]key.Private{}}
}

// AddFileKey keeps a key of the files: the newest, and the one before it.
func (h *Holder) AddFileKey(k key.Private) error {
	sha, err := k.KeySHA256()
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, f := range h.files {
		if f.sha == sha {
			h.files = append(h.files[:i], h.files[i+1:]...)
			break
		}
	}
	h.files = append(h.files, fileKey{sha: sha, k: k})
	if len(h.files) > maxFileKeys {
		h.files = h.files[len(h.files)-maxFileKeys:]
	}
	return nil
}

// For answers the key of an identity this worker holds: a key of the files with the identity's public key for an identity of the
// configuration, the opened key of an uploaded one (opened on the first ask). ok is false when the worker does not hold it: an identity
// of the files a worker that started later never loaded, an upload whose key does not open with this replica's ring.
func (h *Holder) For(ctx context.Context, id signing.Identity) (key.Private, bool, error) {
	h.mu.Lock()
	if id.Source == signing.SourceConfiguration {
		defer h.mu.Unlock()
		for _, f := range h.files {
			if f.sha == id.KeySHA256 {
				return f.k, true, nil
			}
		}
		return key.Private{}, false, nil
	}
	if k, ok := h.opened[id.ID]; ok {
		h.mu.Unlock()
		return k, true, nil
	}
	h.mu.Unlock()
	if h.db == nil || h.ring == nil {
		return key.Private{}, false, nil
	}
	k, ok, err := key.Load(ctx, h.db, h.ring, id.ID)
	if err != nil || !ok {
		return key.Private{}, false, err
	}
	if sha, err := k.KeySHA256(); err != nil || sha != id.KeySHA256 {
		return key.Private{}, false, err
	}
	h.mu.Lock()
	h.opened[id.ID] = k
	h.mu.Unlock()
	return k, true, nil
}

// Forget drops the opened keys of identities that no longer sign or wait to (previous, retired, gone), so a retired key leaves the
// worker's memory at the next look.
func (h *Holder) Forget(ids []signing.Identity) {
	keep := map[int64]bool{}
	for _, i := range ids {
		if i.State == signing.StateCurrent || i.State == signing.StateAnnounced {
			keep[i.ID] = true
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id := range h.opened {
		if !keep[id] {
			delete(h.opened, id)
		}
	}
}
