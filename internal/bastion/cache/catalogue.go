package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/metrics"
)

// The public catalogue of the web interface in the cache backend (W-35, R-BAS-110): a tree of its own, which neither area's sweep
// lists, so that a catalogue's documents are never evicted by the artifacts or the documents, and which counts against a share of
// cache.max_bytes of its own (bastion.gui.max_bytes, [Options.CatalogueMaxBytes]):
//
//	catalogue/documents/<h0h1>/<h2h3>/<64 hex>   a document gzip-compressed, named by the SHA-256 of its raw bytes
//	catalogue/state.json                          the state record: the cursor, the time of the last confirmation, the public
//	                                              set, the switches and the document that names every path's document
//
// With a cache key the documents are objects of the stored format below bc-<id>/g/ under keyed names, and the state record is
// bc-<id>/c/catalogue, bound to its name like the stored configuration. The state record is signed with the signing key in the
// domain catalogue where entries are signed (Q-BAS-98's rule), so that a write to the bucket or the volume without the key
// cannot make a replica serve another catalogue (H-2316).
//
// The bytes are opaque here: the catalogue package verifies a document's SHA-256 of its raw bytes after it decompresses it, on
// the way in and on every read (R-BAS-109, R-BAS-113). A listing names a document by its base name, the content address in the
// plain layout and the keyed name with a cache key; [Cache.CatalogueName] gives the base name of a content address.

const (
	dirCatalogue = "catalogue/documents"
	// fileCatalogueState is the state record of the public catalogue (R-BAS-110).
	fileCatalogueState = "catalogue/state.json"
	// maxCatalogueState bounds the read of the state record: the public set of thousands of repositories fits many times.
	maxCatalogueState = 16 << 20
)

// DomainCatalogue is the state record of the public catalogue (R-BAS-110).
const DomainCatalogue Domain = "catalogue"

// nameCatalogue is the area of a catalogue document's keyed name (R-BAS-061's names, Q-BAS-213's areas).
const nameCatalogue byte = 'g'

// CatalogueObject is a stored document of the public catalogue as a listing finds it: its base name ([Cache.CatalogueName]), the
// bytes the store holds for it and the time it was written.
type CatalogueObject struct {
	Name     string
	Size     int64
	Modified time.Time
}

// catalogueKey is the name of a catalogue document, and its base name.
func (b *backend) catalogueKey(sha string) (key, name string, err error) {
	if b.seal == nil {
		if !validHash(sha) {
			return "", "", fmt.Errorf("%w: content address %q", ErrInvalid, shorten(sha))
		}
		return dirCatalogue + "/" + sha[0:2] + "/" + sha[2:4] + "/" + sha, sha, nil
	}
	d, err := rawHash("content address", sha)
	if err != nil {
		return "", "", err
	}
	m := b.seal.name(nameCatalogue, d)
	return b.catalogueDir() + m[0:2] + "/" + m[2:4] + "/" + m, m, nil
}

func (b *backend) catalogueDir() string {
	if b.seal == nil {
		return dirCatalogue + "/"
	}
	return b.seal.id + "/g/"
}

func (b *backend) catalogueState() string {
	if b.seal == nil {
		return fileCatalogueState
	}
	return b.seal.id + "/c/catalogue"
}

// CatalogueName is the base name under which a listing finds the document of a content address.
func (c *Cache) CatalogueName(sha string) (string, error) {
	_, name, err := c.back.catalogueKey(sha)
	return name, err
}

// PutCatalogueDocument stores a document's compressed bytes under the SHA-256 of its raw bytes, which the caller verified.
// Storing an address held already writes it again.
func (c *Cache) PutCatalogueDocument(ctx context.Context, sha string, gz []byte) error {
	key, _, err := c.back.catalogueKey(sha)
	if err != nil {
		return err
	}
	if c.back.seal != nil {
		return c.back.fail(metrics.OpPut, c.back.putSealedBytes(ctx, key, gz))
	}
	return c.back.fail(metrics.OpPut, c.back.s.putBytes(ctx, key, gz))
}

// GetCatalogueDocument reads a document's compressed bytes, at most limit of them. A missing document is [ErrNotFound]; with a
// cache key one the stored format refuses is [ErrCorrupt].
func (c *Cache) GetCatalogueDocument(ctx context.Context, sha string, limit int64) ([]byte, error) {
	key, _, err := c.back.catalogueKey(sha)
	if err != nil {
		return nil, err
	}
	if c.back.seal != nil {
		data, err := c.back.readSealedBytes(ctx, key, limit)
		if err != nil && refused(err) {
			err = fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		return data, c.back.fail(metrics.OpGet, err)
	}
	data, err := readAll(ctx, c.back.s, key, limit)
	return data, c.back.fail(metrics.OpGet, err)
}

// DeleteCatalogueDocument removes the document of a content address; a missing one is not an error.
func (c *Cache) DeleteCatalogueDocument(ctx context.Context, sha string) error {
	_, name, err := c.back.catalogueKey(sha)
	if err != nil {
		return err
	}
	return c.DeleteCatalogueObject(ctx, name)
}

// DeleteCatalogueObject removes a document by the base name a listing found; a missing one is not an error.
func (c *Cache) DeleteCatalogueObject(ctx context.Context, name string) error {
	if !validHash(name) {
		return fmt.Errorf("%w: catalogue object %q", ErrInvalid, shorten(name))
	}
	return c.back.fail(metrics.OpDelete, c.back.s.remove(ctx, c.back.catalogueDir()+name[0:2]+"/"+name[2:4]+"/"+name))
}

// ListCatalogue calls fn for every document of the catalogue's tree.
func (c *Cache) ListCatalogue(ctx context.Context, fn func(CatalogueObject) error) error {
	err := c.back.s.list(ctx, c.back.catalogueDir(), func(key string, size int64, modified time.Time) error {
		if name := base(key); validHash(name) {
			return fn(CatalogueObject{Name: name, Size: size, Modified: modified})
		}
		return nil
	})
	return c.back.fail(metrics.OpList, err)
}

// StoreCatalogueState keeps the state record of the public catalogue: signed in the domain catalogue where entries are signed, and
// with a cache key an object of the stored format bound to its name.
func (c *Cache) StoreCatalogueState(ctx context.Context, payload []byte) error {
	rec := payload
	if c.signer != nil {
		rec = c.signer.Sign(DomainCatalogue, fileCatalogueState, payload)
	}
	key := c.back.catalogueState()
	if c.back.seal != nil {
		return c.back.fail(metrics.OpPut, c.back.putSealedBytes(ctx, key, rec))
	}
	return c.back.fail(metrics.OpPut, c.back.s.putBytes(ctx, key, rec))
}

// LoadCatalogueState reads the state record and returns its payload. A missing record is [ErrNotFound]; one whose signature fails,
// or with a cache key one the stored format refuses, is [ErrBadSignature].
func (c *Cache) LoadCatalogueState(ctx context.Context) ([]byte, error) {
	key := c.back.catalogueState()
	var rec []byte
	var err error
	if c.back.seal != nil {
		rec, err = c.back.readSealedBytes(ctx, key, maxCatalogueState)
		if err != nil && refused(err) {
			err = fmt.Errorf("%w: %w", ErrBadSignature, err)
		}
	} else {
		rec, err = readAll(ctx, c.back.s, key, maxCatalogueState)
	}
	if err != nil {
		return nil, c.back.fail(metrics.OpGet, err)
	}
	if c.signer == nil {
		return rec, nil
	}
	return c.signer.Verify(DomainCatalogue, fileCatalogueState, rec)
}

// DeleteCatalogueState removes the state record; a missing one is not an error.
func (c *Cache) DeleteCatalogueState(ctx context.Context) error {
	return c.back.fail(metrics.OpDelete, c.back.s.remove(ctx, c.back.catalogueState()))
}

// CatalogueMaxBytes is the catalogue's share of cache.max_bytes (bastion.gui.max_bytes).
func (c *Cache) CatalogueMaxBytes() int64 { return c.catalogue }
