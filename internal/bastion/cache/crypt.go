package cache

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The derivations of R-BAS-061 from the cache key K, each HKDF(K, "", FMT ‖ " bastion …"), and the label of a keyed name.
const (
	labelStorage = blobcrypt.Format + " bastion storage"
	labelKeyID   = blobcrypt.Format + " bastion key id"
	labelNames   = blobcrypt.Format + " bastion names"
	labelName    = blobcrypt.Format + " name"
)

// treePrefix starts the identifier of a cache key, which names its tree below the backend's root: bc- and the hex of eight
// bytes (R-BAS-061).
const treePrefix = "bc-"

var treeName = regexp.MustCompile(`^bc-[0-9a-f]{16}$`)

// The areas of a keyed name: the bytes of an artifact, of a shared document, and an entry (Q-BAS-213).
const (
	nameArtifact byte = 'a'
	nameDocument byte = 'd'
	nameEntry    byte = 'e'
)

// sealer is what the cache key yields (R-BAS-061, Q-BAS-210): the storage key that wraps the data key of every object of the
// tree, its identifier, which names the tree, and the name key. The file's bytes and the key K are cleared once these are
// derived; the storage key and the name key live for the replica's life, as a storage key does in a replica of the main
// installation (H-1309). String, GoString and LogValue render the identifier only.
type sealer struct {
	id      string
	storage *blobcrypt.StorageKey
	names   [blobcrypt.KeySize]byte
}

func (s *sealer) String() string       { return "cache key " + s.id }
func (s *sealer) GoString() string     { return s.String() }
func (s *sealer) LogValue() slog.Value { return slog.StringValue(s.String()) }

// openSealer reads the cache key file of bastion.cache.encryption_key_file at start (R-BAS-061): 32 bytes, raw or base64, by
// the key ring's file rules (R-ARCH-021). An empty path is no key: the cache stays plain. No error names a byte of the file.
func openSealer(path string) (*sealer, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec // the chart-managed path of the mounted Secret
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("the cache key file %s does not exist: mount secrets.cacheKey (R-BAS-061)", path)
		}
		return nil, fmt.Errorf("the cache key file cannot be read: %w", err)
	}
	defer clear(raw)
	k, err := parseKey(raw)
	if err != nil {
		return nil, fmt.Errorf("the cache key file %s %w", path, err)
	}
	defer clear(k)
	return newSealer(k)
}

// errKeyForm says that a key file holds no key of the form; it names nothing of what the file holds.
var errKeyForm = errors.New("holds neither 32 raw bytes nor 32 bytes in base64 (43 or 44 characters), the key ring's file rules (R-ARCH-021)")

// parseKey returns the key a file holds: a file of exactly 32 bytes is the key itself, as a generator of random bytes writes
// it, checked before anything is trimmed; otherwise base64 text of 43 or 44 characters, surrounding white space ignored. The
// caller clears the result.
func parseKey(b []byte) ([]byte, error) {
	if len(b) == blobcrypt.KeySize {
		return bytes.Clone(b), nil
	}
	t := bytes.TrimSpace(b)
	if len(t) != 43 && len(t) != 44 {
		return nil, errKeyForm
	}
	out := make([]byte, max(base64.StdEncoding.DecodedLen(len(t)), base64.RawStdEncoding.DecodedLen(len(t))))
	n, err := base64.StdEncoding.Decode(out, t)
	if err != nil {
		clear(out)
		n, err = base64.RawStdEncoding.Decode(out, t)
	}
	if err != nil || n != blobcrypt.KeySize {
		clear(out)
		return nil, errKeyForm
	}
	return out[:n], nil
}

// newSealer derives the storage key, its identifier and the name key from K (R-BAS-061). Every derived byte that is not kept
// is cleared.
func newSealer(k []byte) (*sealer, error) {
	idBytes, err := derive(k, labelKeyID)
	if err != nil {
		return nil, err
	}
	id := treePrefix + hex.EncodeToString(idBytes[:8])
	clear(idBytes)
	storage, err := derive(k, labelStorage)
	if err != nil {
		return nil, err
	}
	sk, err := blobcrypt.NewStorageKey(id, storage)
	clear(storage)
	if err != nil {
		return nil, err
	}
	names, err := derive(k, labelNames)
	if err != nil {
		sk.Key.Destroy()
		return nil, err
	}
	s := &sealer{id: id, storage: sk}
	copy(s.names[:], names)
	clear(names)
	return s, nil
}

// derive is HKDF-SHA-256 with an empty salt and 32 bytes of output.
func derive(k []byte, info string) ([]byte, error) {
	out, err := hkdf.Key(sha256.New, k, nil, info, blobcrypt.KeySize)
	if err != nil {
		return nil, fmt.Errorf("cache: deriving a key of the cache key: %w", err)
	}
	return out, nil
}

// name is a keyed name of R-BAS-061: the hex of HMAC-SHA-256(name key, FMT ‖ " name" ‖ area ‖ digest), with the area one byte
// (a, d or e) and the digest the 32 bytes of a content address or of the SHA-256 of a cache key.
func (s *sealer) name(area byte, digest []byte) string {
	m := hmac.New(sha256.New, s.names[:])
	m.Write([]byte(labelName))
	m.Write([]byte{area})
	m.Write(digest)
	return hex.EncodeToString(m.Sum(nil))
}

// lookup is the key lookup of blobcrypt.Open: the tree of an identifier holds what its key wrote, so a record that names
// another identifier is unknown here.
func (s *sealer) lookup(id string) (*blobcrypt.StorageKey, error) {
	if id == s.id {
		return s.storage, nil
	}
	return nil, nil
}

// The record lies in front of the object's body, in the same object, because neither backend keeps object metadata
// (Q-BAS-212): frame = FMT ‖ u8(len(id)) ‖ id ‖ W ‖ C, 182 bytes with the identifier of a cache key.
const frameFixed = len(blobcrypt.Format) + 1 + blobcrypt.WrapSize + 32

func frameSize(id string) int64 { return int64(frameFixed + len(id)) }

// encodeFrame is the record of R-STOR-077 as the frame in front of the body.
func encodeFrame(r blobcrypt.Record) []byte {
	b := make([]byte, 0, frameFixed+len(r.KeyID))
	b = append(b, blobcrypt.Format...)
	b = append(b, byte(len(r.KeyID))) //nolint:gosec // G115: an identifier is at most 64 bytes, blobcrypt.Wrap checked it
	b = append(b, r.KeyID...)
	b = append(b, r.Wrap...)
	return append(b, r.Commitment[:]...)
}

// errFrame is an object that does not begin with a frame of the format: too short for one, or with an identifier of no length
// the format allows. It is evidence about the object, like a refusal of blobcrypt.Open.
var errFrame = errors.New("cache: the object does not begin with the record of the stored format")

// readFrame reads the frame of an object whose stored size is total and returns the record as the object's metadata, for
// blobcrypt.Open to check in the order of R-STOR-077, and the frame's length. A body that ends before the frame does although
// total says it is there is the transport's error.
func readFrame(r io.Reader, total int64) (map[string]string, int64, error) {
	head := make([]byte, len(blobcrypt.Format)+1)
	if total < int64(len(head)) {
		return nil, 0, errFrame
	}
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, 0, transport(err)
	}
	n := int(head[len(blobcrypt.Format)])
	if n < 3 || n > 64 || total < int64(frameFixed+n) {
		return nil, 0, errFrame
	}
	rest := make([]byte, n+blobcrypt.WrapSize+32)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, 0, transport(err)
	}
	rec := blobcrypt.Record{KeyID: string(rest[:n]), Wrap: rest[n : n+blobcrypt.WrapSize]}
	copy(rec.Commitment[:], rest[n+blobcrypt.WrapSize:])
	meta := rec.Metadata()
	meta[blobcrypt.MetaFormat] = string(head[:len(blobcrypt.Format)])
	return meta, int64(frameFixed + n), nil
}

// transport turns an early end of a body into io.ErrUnexpectedEOF: no finding about the object.
func transport(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// refused says whether err is a finding about a stored object: a refusal of the format with its reason, a frame that is not
// one, or a plaintext bound to another name. Every reason counts, unknown_key included: the tree of an identifier holds only
// what its key wrote, so an object that names another key was put there from outside (R-BAS-061).
func refused(err error) bool {
	if _, ok := blobcrypt.ReasonOf(err); ok {
		return true
	}
	return errors.Is(err, errFrame) || errors.Is(err, errUnbound)
}

// corruptOf marks a refusal as ErrCorrupt, so that the cache deletes the object and its entry; any other error passes as it
// is.
func corruptOf(err error) error {
	if err != nil && refused(err) {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return err
}

// bound is the plaintext of an entry, of a file of the audit spool and of the probe: the object's name, a line feed and the
// payload, so that an object moved to another name, or of another family, is refused when it is read (Q-BAS-213).
func bound(key string, payload []byte) []byte {
	b := make([]byte, 0, len(key)+1+len(payload))
	b = append(b, key...)
	b = append(b, '\n')
	return append(b, payload...)
}

// unbind returns the payload of a bound plaintext read at key, or false when it was bound to another name.
func unbind(key string, plain []byte) ([]byte, bool) {
	rest, ok := bytes.CutPrefix(plain, []byte(key+"\n"))
	return rest, ok
}
