package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The stream of a blob into an encrypted connection, for the writers that hold no spool of it (storage.md R-STOR-087,
// backup-and-restore.md R-BAK-031, Q-STOR-440): the reconciliation's copy into an encrypted connection, the scrub's repair of an
// encrypted copy and a restore into an encrypted connection. Each reads the plaintext from somewhere (another connection's reader, a
// backup's pool) and writes it with a new data key under the connection's active storage key, one part in memory, as the commit's
// writer does from the spool (R-STOR-080).

// ErrStreamNotBlob is a stream whose length or SHA-256 is not the blob's: evidence against its source, and nothing of it is completed
// on the target. It comes wrapped in a [*StreamSourceError].
var ErrStreamNotBlob = errors.New("the stream is not the blob")

// StreamSourceError is a failure of reading the stream a writer was given, not of the target: a transport error of the source, a
// refusal of the source's reader (a *blobcrypt.Error with its reason), or [ErrStreamNotBlob]. The caller tells evidence against the
// source from a failure of the target by it.
type StreamSourceError struct{ Err error }

func (e *StreamSourceError) Error() string { return "the source of the stream: " + e.Err.Error() }
func (e *StreamSourceError) Unwrap() error { return e.Err }

// UseKeys gives the connection the storage keys a writer without a store works with (the restore Job, which reads them from the
// backup it restores, R-BAK-031). A connection of a store gets the replica's keys from the store.
func (c *Connection) UseKeys(k StorageKeys) { c.bind(k, nil) }

// WriteSealedStream writes the blob d of size plain bytes, which src yields from its first byte, onto the encrypted connection c
// (R-STOR-080, R-STOR-087): a new data key, the connection's active storage key and its name, never a copy of another object's
// ciphertext (Q-STOR-308). Up to one part of partSize plain bytes it is one PutObject of S(L) bytes with the record; above, a multipart
// upload with the record on its creation, parts of partSize plain bytes (raised to fit 10,000 parts) stored as their segments, the
// last with the final segment. One part is held in memory; the payload checksum is a first pass over the part's sealed bytes and the
// SDK's retry a seek of it. The plaintext is hashed as it passes: a stream that ends early, runs long or does not hash to d is a
// *StreamSourceError with ErrStreamNotBlob, and nothing of it is completed. The completed object is confirmed by HeadObject (its size
// and the record's storage key). The returned row is the copy row the write makes: S(L), the storage key, verified where the
// connection enforces payload checksums (Quader encrypted what it hashed, and the store refused any other bytes).
func (c *Connection) WriteSealedStream(ctx context.Context, d Digest, size int64, src io.Reader, partSize int64) (CopyWrite, error) {
	keys, _ := c.sealing()
	switch {
	case !c.Encrypted():
		return CopyWrite{}, fmt.Errorf("blob: connection %s is not encrypted", c.ID())
	case keys == nil:
		return CopyWrite{}, fmt.Errorf("blob: this replica holds no storage keys of connection %s", c.ID())
	}
	name, err := c.Name(d)
	if err != nil {
		return CopyWrite{}, err
	}
	sk, err := keys.Active(ctx, c.ID())
	if err != nil {
		return CopyWrite{}, fmt.Errorf("blob: the active storage key of connection %s: %w", c.ID(), err)
	}
	stored, err := blobcrypt.StoredSize(size)
	if err != nil {
		return CopyWrite{}, err
	}
	seed, err := blobcrypt.NewSeed(rand.Reader)
	if err != nil {
		return CopyWrite{}, err
	}
	dek, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		return CopyWrite{}, err
	}
	defer dek.Destroy()
	rec, err := blobcrypt.Wrap(sk, blobcrypt.DataKey{Key: dek, Digest: d, Size: size}, rand.Reader)
	if err != nil {
		return CopyWrite{}, err
	}
	md := Metadata(rec.Metadata())
	ps := PartSize(size, partSize)
	if size > ps && ps%blobcrypt.SegmentSize != 0 {
		return CopyWrite{}, fmt.Errorf("connection %s: a part of %d bytes holds no whole segments (R-STOR-075)", c.ID(), ps)
	}
	w := &sealedStream{c: c, name: name, d: d, size: size, src: src, h: sha256.New(), dek: dek, seed: seed, md: md, alg: payloadAlgorithm(c)}
	if size <= ps {
		err = w.one()
	} else {
		err = w.multipart(ctx, ps)
	}
	if err != nil {
		return CopyWrite{}, err
	}
	if size <= ps {
		if err := w.put(ctx); err != nil {
			return CopyWrite{}, err
		}
	}
	if err := confirmSealed(ctx, c, name, stored, md); err != nil {
		return CopyWrite{}, err
	}
	return CopyWrite{Connection: c.ID(), Key: name, StoredSize: stored, Verified: c.Verifies(), StorageKeyID: sk.ID}, nil
}

// sealedStream is one WriteSealedStream.
type sealedStream struct {
	c    *Connection
	name string
	d    Digest
	size int64
	src  io.Reader
	h    hash.Hash
	read int64
	dek  blobcrypt.Key
	seed [32]byte
	md   Metadata
	alg  string
	buf  []byte // the whole blob of a single request
}

// fill reads len(b) bytes of the stream into b and hashes them.
func (w *sealedStream) fill(b []byte) error {
	n, err := io.ReadFull(w.src, b)
	w.h.Write(b[:n])
	w.read += int64(n)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		if n < len(b) && w.read < w.size {
			return &StreamSourceError{Err: fmt.Errorf("%w: it ended after %d of %d bytes", ErrStreamNotBlob, w.read, w.size)}
		}
		return nil
	}
	return &StreamSourceError{Err: err}
}

// end checks that the stream holds nothing more and hashes to the digest.
func (w *sealedStream) end() error {
	var one [1]byte
	for {
		n, err := w.src.Read(one[:])
		if n > 0 {
			return &StreamSourceError{Err: fmt.Errorf("%w: it is longer than %d bytes", ErrStreamNotBlob, w.size)}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &StreamSourceError{Err: err}
		}
	}
	if Digest(w.h.Sum(nil)) != w.d {
		return &StreamSourceError{Err: fmt.Errorf("%w: its SHA-256 is not the digest %s", ErrStreamNotBlob, w.d.Hex())}
	}
	return nil
}

// one reads a blob of at most one part whole and checks it; put sends it.
func (w *sealedStream) one() error {
	w.buf = make([]byte, w.size)
	if err := w.fill(w.buf); err != nil {
		return err
	}
	return w.end()
}

func (w *sealedStream) put(ctx context.Context) error {
	sec, err := blobcrypt.NewSection(w.dek, w.seed, bytes.NewReader(w.buf), 0, w.size, &blobcrypt.Final{Size: w.size, Digest: w.d})
	if err != nil {
		return err
	}
	sum, err := sendChecksum(w.alg, sec)
	if err != nil {
		return err
	}
	return w.c.Objects().Put(ctx, w.name, sec, sec.Size(), sum, w.md)
}

// multipart sends the blob in parts of ps plain bytes; the stream's end and digest are checked before the upload completes.
func (w *sealedStream) multipart(ctx context.Context, ps int64) (err error) {
	objects := w.c.Objects()
	id, err := objects.CreateMultipart(ctx, w.name, w.alg, w.md)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = objects.AbortMultipart(actx, w.name, id) // what is left the orphan sweep aborts (R-STOR-026)
	}()
	buf := make([]byte, ps)
	var parts []Part
	for off, number := int64(0), 1; off < w.size; off, number = off+ps, number+1 {
		n := min(ps, w.size-off)
		if err := w.fill(buf[:n]); err != nil {
			return err
		}
		last := off+n == w.size
		var final *blobcrypt.Final
		if last {
			if err := w.end(); err != nil {
				return err
			}
			final = &blobcrypt.Final{Size: w.size, Digest: w.d}
		}
		sec, err := blobcrypt.NewSection(w.dek, w.seed, bytes.NewReader(buf[:n]), off/blobcrypt.SegmentSize, n, final)
		if err != nil {
			return err
		}
		sum, err := sendChecksum(w.alg, sec)
		if err != nil {
			return err
		}
		p, err := objects.UploadPart(ctx, w.name, id, number, sec, sec.Size(), sum)
		if err != nil {
			return err
		}
		p.Number, p.Size, p.Checksum = number, sec.Size(), sum
		parts = append(parts, p)
	}
	return objects.CompleteMultipart(ctx, w.name, id, parts)
}
