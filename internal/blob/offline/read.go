package offline

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/syncwrite"
)

// The statuses of an object that are not a reason of blobcrypt (format, unknown_key, record, size, commitment, segment, final and
// digest are their own statuses, storage.md R-STOR-077).
const (
	// StatusOK: the record opens with the export's keys and the name is the keyed name of the digest its wrap binds, or the digest
	// itself; for Verify, every segment, the final segment and the SHA-256 passed as well.
	StatusOK = "ok"
	// StatusPlain: an object without a record on a plain layout, whose name carries its digest; for Verify, its SHA-256 is that digest.
	StatusPlain = "plain"
	// StatusNameMismatch: the name is not the keyed name of the digest (or not the digest), or not a name of the layout at all.
	StatusNameMismatch = "name_mismatch"
	// StatusDestroyedKey: the record names a key the export lists as destroyed (R-STOR-101, R-STOR-103).
	StatusDestroyedKey = "destroyed_key"
	// StatusUnread: the store or the file system did not answer; no finding about the object.
	StatusUnread = "unread"
)

// Result is what the tool found of one object. Size and SHA256 are the plaintext's, from the wrap or, for a plain object, from its
// name; KeyID is the storage key the record names. Nothing in it is a key or a sealed value.
type Result struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	StoredSize int64  `json:"storedSize"`
	Size       *int64 `json:"size,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	KeyID      string `json:"keyId,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Sound reports whether the object is readable as it is.
func (r Result) Sound() bool { return r.Status == StatusOK || r.Status == StatusPlain }

// Finding reports whether the object is evidence of damage, of a missing key or of a foreign name: not sound and not unread.
func (r Result) Finding() bool { return !r.Sound() && r.Status != StatusUnread }

// Totals count the results of a walk.
type Totals struct {
	Objects  int            `json:"objects"`
	OK       int            `json:"ok"`
	Plain    int            `json:"plain"`
	Findings int            `json:"findings"`
	Unread   int            `json:"unread"`
	ByStatus map[string]int `json:"byStatus"`
}

func (t *Totals) add(r Result) {
	if t.ByStatus == nil {
		t.ByStatus = map[string]int{}
	}
	t.Objects++
	t.ByStatus[r.Status]++
	switch r.Status {
	case StatusOK:
		t.OK++
	case StatusPlain:
		t.Plain++
	case StatusUnread:
		t.Unread++
	default:
		t.Findings++
	}
}

// DefaultConcurrency is how many objects a walk reads at once unless told.
const DefaultConcurrency = 4

// Reader reads the objects of a source with the keys of an export.
type Reader struct {
	src  Source
	keys *Keys
}

// NewReader returns the reader of src with keys.
func NewReader(src Source, keys *Keys) *Reader { return &Reader{src: src, keys: keys} }

// opened is an object whose record opened and whose name matched: its data key, or for a plain object its digest and size.
type opened struct {
	dk     blobcrypt.DataKey
	plain  bool
	digest [32]byte
	size   int64
}

func (o *opened) close() {
	if o != nil && !o.plain {
		o.dk.Key.Destroy()
	}
}

// check runs what List shows of one object (R-STOR-089): one Head, the record opened with the export's keys (steps 1 to 6 of
// R-STOR-077 against the size the source answered, with the digest and size learnt from the wrap), and the name compared. It reads
// no byte of the body. err is a failure of the source only.
func (r *Reader) check(ctx context.Context, name string) (Result, *opened, error) {
	info := r.src.Info()
	res := Result{Name: name}
	n, ok := parseName(info.Prefix, name)
	if !ok {
		res.Status, res.Detail = StatusNameMismatch, "the name is not <family>/<h0h1>/<h2h3>/<64 hex> below keyed/, sha256/ or pool/sha256/"
		return res, nil, nil
	}
	obj, md, err := r.src.Head(ctx, name)
	if err != nil {
		return res, nil, err
	}
	res.StoredSize = obj.Size
	if _, rec := md[blobcrypt.MetaFormat]; !rec && !n.expectsRecord(info.Encrypted) {
		res.Status, res.SHA256, res.Size = StatusPlain, n.hex, &obj.Size
		op := &opened{plain: true, size: obj.Size}
		_, _ = hex.Decode(op.digest[:], []byte(n.hex))
		return res, op, nil
	}
	if blobcrypt.ValidKeyID(md[blobcrypt.MetaKey]) {
		res.KeyID = md[blobcrypt.MetaKey]
	}
	dk, err := blobcrypt.Open(md, r.keys.lookup, blobcrypt.Want{ObjectSize: obj.Size})
	if err != nil {
		res.Status, res.Detail = refusalStatus(err, res.KeyID)
		if res.Status == "" {
			return res, nil, err
		}
		return res, nil, nil
	}
	size := dk.Size
	res.Size, res.SHA256 = &size, hex.EncodeToString(dk.Digest[:])
	if st, detail := r.keys.checkName(info.Prefix, name, n, dk); st != "" {
		dk.Key.Destroy()
		res.Status, res.Detail = st, detail
		return res, nil, nil
	}
	res.Status = StatusOK
	return res, &opened{dk: dk}, nil
}

// refusalStatus is the status and detail of a record that did not open; an empty status is an error that is no refusal.
func refusalStatus(err error, keyID string) (string, string) {
	var de *destroyedError
	if errors.As(err, &de) {
		return StatusDestroyedKey, de.Error()
	}
	reason, ok := blobcrypt.ReasonOf(err)
	switch {
	case !ok:
		return "", ""
	case reason == blobcrypt.ReasonUnknownKey:
		return string(reason), "the export holds no storage key " + keyID + "; import or name an export that lists it"
	}
	return string(reason), err.Error()
}

// finding is a refusal of an object's body, with its status.
type finding struct{ status, detail string }

func (f *finding) Error() string { return f.status + ": " + f.detail }

// readBody copies the plaintext of an opened object into w: through blobcrypt's reader, which hands out no byte of a segment before
// it authenticated and holds the last back until the final segment and the SHA-256 passed (step 7 of R-STOR-077), or, for a plain
// object, hashing it against the digest its name carries. A refusal is a *finding; any other error is the source's.
func (r *Reader) readBody(ctx context.Context, name string, op *opened, w io.Writer) error {
	body, err := r.src.Open(ctx, name)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if op.plain {
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(w, h), body)
		switch {
		case err != nil:
			return err
		case n != op.size:
			return &finding{string(blobcrypt.ReasonSize), fmt.Sprintf("the body holds %d bytes, the object %d", n, op.size)}
		case subtle.ConstantTimeCompare(h.Sum(nil), op.digest[:]) != 1:
			return &finding{string(blobcrypt.ReasonDigest), "the SHA-256 of the body is not the digest its name carries"}
		}
		return nil
	}
	rd, err := blobcrypt.NewReader(body, op.dk, true)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rd); err != nil {
		if reason, ok := blobcrypt.ReasonOf(err); ok {
			return &finding{string(reason), err.Error()}
		}
		return err
	}
	return nil
}

// verifyOne reads one object whole as the scrub does: check, then the body through readBody.
func (r *Reader) verifyOne(ctx context.Context, name string) Result {
	res, op, err := r.check(ctx, name)
	defer op.close()
	if err != nil {
		res.Status, res.Detail = StatusUnread, err.Error()
		return res
	}
	if op == nil {
		return res
	}
	if err := r.readBody(ctx, name, op, io.Discard); err != nil {
		var f *finding
		if errors.As(err, &f) {
			res.Status, res.Detail = f.status, f.detail
		} else {
			res.Status, res.Detail = StatusUnread, err.Error()
		}
	}
	return res
}

// checkOne is check as a Result: a failure of the source is unread.
func (r *Reader) checkOne(ctx context.Context, name string) Result {
	res, op, err := r.check(ctx, name)
	op.close()
	if err != nil {
		res.Status, res.Detail = StatusUnread, err.Error()
	}
	return res
}

// walk calls fn for every object below the three families of the prefix.
func (r *Reader) walk(ctx context.Context, fn func(Object) error) error {
	for _, f := range families() {
		if err := r.src.Walk(ctx, base(r.src.Info().Prefix)+f.root, fn); err != nil {
			return err
		}
	}
	return nil
}

// List checks every object below keyed/, sha256/ and pool/sha256/ of the prefix, one Head each, and emits a Result per object in
// the order of their names (R-STOR-089 list). The error is the walk's or emit's.
func (r *Reader) List(ctx context.Context, emit func(Result) error) (Totals, error) {
	return r.each(ctx, DefaultConcurrency, r.checkOne, emit)
}

// Verify reads every object whole, concurrency at once, and emits a Result per object in the order of their names (R-STOR-089
// verify).
func (r *Reader) Verify(ctx context.Context, concurrency int, emit func(Result) error) (Totals, error) {
	return r.each(ctx, max(concurrency, 1), r.verifyOne, emit)
}

// each runs one over every object of the walk, n at once, and emits the results in the walk's order.
func (r *Reader) each(ctx context.Context, n int, one func(context.Context, string) Result, emit func(Result) error) (Totals, error) {
	type job struct {
		seq  int
		name string
	}
	type done struct {
		seq int
		res Result
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs, results := make(chan job, n), make(chan done, n)
	var walkErr error
	go func() {
		defer close(jobs)
		seq := 0
		walkErr = r.walk(ctx, func(o Object) error {
			select {
			case jobs <- job{seq: seq, name: o.Name}:
				seq++
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for j := range jobs {
				results <- done{seq: j.seq, res: one(ctx, j.name)}
			}
		})
	}
	go func() { wg.Wait(); close(results) }()
	var t Totals
	var emitErr error
	pending, next := map[int]Result{}, 0
	for d := range results {
		pending[d.seq] = d.res
		for res, ok := pending[next]; ok; res, ok = pending[next] {
			delete(pending, next)
			next++
			t.add(res)
			if emitErr == nil {
				if emitErr = emit(res); emitErr != nil {
					cancel()
				}
			}
		}
	}
	if emitErr != nil {
		return t, emitErr
	}
	return t, walkErr
}

// Locate finds the object of ref: an object name below the prefix, taken as it is, or a digest (64 lowercase hex), whose object is
// looked for at its keyed name under the name key of the connection (with keyed names), at its plain name, and in the pool. A digest
// whose object is in none of them is blob.ErrNotFound; a name that is not of the layout is a Refusal.
func (r *Reader) Locate(ctx context.Context, ref string) (string, error) {
	info := r.src.Info()
	d, isDigest := blob.ParseDigest(ref)
	if !isDigest {
		if _, ok := parseName(info.Prefix, ref); !ok {
			return "", refuse("%q is neither a SHA-256 (64 lowercase hex) nor an object name below %skeyed/, %ssha256/ or %spool/sha256/", ref, base(info.Prefix), base(info.Prefix), base(info.Prefix))
		}
		return ref, nil
	}
	var names []string
	if info.KeyedNames {
		if nk, st, _ := r.keys.nameKey(info.Connection); st == "" {
			if n, err := blobcrypt.KeyedName(nk, info.Prefix, [32]byte(d)); err == nil {
				names = append(names, n)
			}
		}
	}
	names = append(names, blob.Key(info.Prefix, d))
	for _, name := range names {
		switch _, _, err := r.src.Head(ctx, name); {
		case err == nil:
			return name, nil
		case !errors.Is(err, blob.ErrNotFound):
			return "", err
		}
	}
	h := d.Hex()
	var found string
	err := r.src.Walk(ctx, base(info.Prefix)+"pool/sha256/"+h[0:2]+"/"+h[2:4]+"/"+h, func(o Object) error {
		if found == "" {
			found = o.Name
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", blob.ErrNotFound
	}
	return found, nil
}

// Get decrypts the object name into the new file out (R-STOR-089 get): out must not exist; the plaintext goes to a temporary file
// beside it (mode 0600) and becomes out, by a hard link that never replaces a file, only after every segment, the final segment and
// the SHA-256 passed. A refusal of the object writes nothing and is a Result that is not sound; err is a Refusal of out or a failure
// of the source or the file system.
func (r *Reader) Get(ctx context.Context, name, out string) (Result, error) {
	if _, err := os.Lstat(out); err == nil {
		return Result{Name: name}, refuse("--out %s exists; the offline tool writes no file over another", out)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Result{Name: name}, err
	}
	res, op, err := r.check(ctx, name)
	defer op.close()
	if err != nil || op == nil {
		return res, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".partial-*")
	if err != nil {
		return res, err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
		}
		_ = os.Remove(tmp.Name())
	}()
	w := syncwrite.NewWriter(tmp, syncwrite.Every) // flushed every 32 MiB (Q-STOR-486)
	err = r.readBody(ctx, name, op, w)
	if err == nil {
		err = w.Sync()
	}
	if err != nil {
		var f *finding
		if errors.As(err, &f) {
			res.Status, res.Detail = f.status, f.detail
			return res, nil
		}
		return res, err
	}
	done = true
	if err := tmp.Close(); err != nil {
		return res, err
	}
	if err := os.Link(tmp.Name(), out); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return res, refuse("--out %s was created while the object was read; the offline tool writes no file over another", out)
		}
		return res, err
	}
	return res, nil
}
