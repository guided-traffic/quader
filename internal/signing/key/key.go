// Package key is the private key of Quader's signing identity (signatures.md R-SIG-046, R-SIG-047, R-SIG-049): the sealed column
// signing_identities.sealed_key registered with the key ring, the seal of a key Quader made or imported, its opening, the read of the
// key file of the Helm values, and the walk of the task secrets-reencrypt over the column. A key is held in a type that renders
// nothing.
//
// The store (internal/signing/store) seals; only the worker's job (internal/signing/job) opens and reads the file, and the composition
// root wires the re-encryption. internal/archtest holds the importers to those three (R-SIG-049).
package key

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/internal/signing"
)

// column is signing_identities.sealed_key: an uploaded identity's key sealed under its row's id (R-SIG-047, R-ARCH-021).
var column = secrets.Register("signing_identities", "id", "sealed_key")

// Column is the sealed column, for the walk of secrets-reencrypt and the key ring's status.
func Column() secrets.Column { return column }

// maxFile bounds the key file of the Helm values: an ECDSA P-256 key in PEM is a few hundred bytes.
const maxFile = 16 << 10

// Private is the identity's private key. It renders nothing: String, GoString, LogValue, MarshalJSON and MarshalText answer the
// redaction marker, so a key that reaches a log, an error or an answer by mistake shows nothing.
type Private struct{ k *ecdsa.PrivateKey }

// Wrap holds a key.
func Wrap(k *ecdsa.PrivateKey) Private { return Private{k: k} }

// Valid reports whether a key is held.
func (p Private) Valid() bool { return p.k != nil }

// Signer is the key as a crypto.Signer, for the signing calls of the job alone.
func (p Private) Signer() crypto.Signer { return p.k }

// Public is the public key, nil when none is held.
func (p Private) Public() *ecdsa.PublicKey {
	if p.k == nil {
		return nil
	}
	return &p.k.PublicKey
}

// KeySHA256 is the SHA-256 of the public key's DER SubjectPublicKeyInfo, as signing_identities.key_sha256 holds it.
func (p Private) KeySHA256() ([32]byte, error) {
	if p.k == nil {
		return [32]byte{}, errors.New("signing key: none held")
	}
	der, err := x509.MarshalPKIXPublicKey(&p.k.PublicKey)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(der), nil
}

// String renders the redaction marker.
func (Private) String() string { return observability.RedactedValue }

// GoString renders the redaction marker.
func (Private) GoString() string { return observability.RedactedValue }

// LogValue implements slog.LogValuer.
func (Private) LogValue() slog.Value { return slog.StringValue(observability.RedactedValue) }

// MarshalJSON renders the redaction marker.
func (Private) MarshalJSON() ([]byte, error) {
	return []byte(`"` + observability.RedactedValue + `"`), nil
}

// MarshalText renders the redaction marker.
func (Private) MarshalText() ([]byte, error) { return []byte(observability.RedactedValue), nil }

func row(id int64) secrets.RowKey { return secrets.Row(strconv.FormatInt(id, 10)) }

// Seal seals a key under the key ring's active key for the row of id (R-SIG-047): the PKCS#8 DER of the key, bound to the table, the
// column and the row, so that it opens under no other row.
func Seal(ring *secrets.KeyRing, id int64, k *ecdsa.PrivateKey) (string, error) {
	if ring == nil {
		return "", errors.New("signing key: no key ring to seal with")
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return "", errors.New("signing key: the key cannot be encoded")
	}
	defer clear(der)
	return ring.Seal(column, row(id), der)
}

// Open opens the sealed key of the row of id. The error names the row, never the key.
func Open(ring *secrets.KeyRing, id int64, sealed string) (Private, error) {
	if ring == nil {
		return Private{}, errors.New("signing key: no key ring to open with")
	}
	der, err := ring.Open(column, row(id), sealed)
	if err != nil {
		return Private{}, fmt.Errorf("signing key of identity %d: %w", id, err)
	}
	defer clear(der)
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return Private{}, fmt.Errorf("signing key of identity %d: the opened value is no PKCS#8 key", id)
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return Private{}, fmt.Errorf("signing key of identity %d: the opened key is no ECDSA key", id)
	}
	return Private{k: ek}, nil
}

// Load reads the sealed key of an uploaded identity that is not retired and opens it (R-SIG-049): the worker's job does, nothing else.
// ok is false when the identity has no sealed key (the files' identity, a retired one).
func Load(ctx context.Context, run meta.Runner, ring *secrets.KeyRing, id int64) (Private, bool, error) {
	var sealed []string
	err := run.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("signing key: not a PostgreSQL transaction")
		}
		var err error
		sealed, err = q.New(ptx).SigningSealedKey(ctx, id)
		return err
	})
	if err != nil || len(sealed) == 0 {
		return Private{}, false, err
	}
	p, err := Open(ring, id, sealed[0])
	return p, err == nil, err
}

// ReadFile reads the key file of the Helm values (signing.identity.key_file, R-SIG-046): one PEM block of an unencrypted PKCS#8 or SEC 1
// key on P-256. The worker's job reads it, nothing else; the error names the file, never the key.
//
// digest is the SHA-256 of the file's bytes, which the watcher compares to see a change; it is answered also when the key does not
// parse, so a refusal is recorded once per content.
func ReadFile(path string) (p Private, digest [32]byte, err error) {
	b, err := readBounded(path)
	if err != nil {
		return Private{}, [32]byte{}, err
	}
	defer clear(b)
	digest = sha256.Sum256(b)
	k, err := signing.ParseKey(b, nil)
	if err != nil {
		return Private{}, digest, err
	}
	return Private{k: k}, digest, nil
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // the operator's configured path
	if err != nil {
		return nil, fmt.Errorf("the key file %s cannot be read: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		clear(b)
		return nil, fmt.Errorf("the key file %s cannot be read: %w", path, err)
	}
	if len(b) > maxFile {
		clear(b)
		return nil, fmt.Errorf("the key file %s is larger than 16 KiB", path)
	}
	return b, nil
}

// ResealResult counts the sealed keys a run of [Reseal] walked: opened and sealed under the active key, already under it, or not
// opened with this replica's ring and left as they are.
type ResealResult struct{ Resealed, Current, Failed int64 }

// Reseal seals every key of signing_identities.sealed_key under the active key of the ring (R-ARCH-021 rotation step 3): the walk of the
// task secrets-reencrypt over the column, in batches of one transaction each that lock their rows, so a concurrent retirement waits.
func Reseal(ctx context.Context, run meta.Runner, ring *secrets.KeyRing, batch int) (ResealResult, error) {
	if batch <= 0 {
		batch = 200
	}
	var res ResealResult
	var after int64
	for {
		var n int
		cursor, step := after, ResealResult{}
		err := run.InTx(ctx, func(tx meta.Tx) error {
			cursor, step = after, ResealResult{}
			ptx, ok := postgres.Unwrap(tx)
			if !ok {
				return errors.New("signing key: not a PostgreSQL transaction")
			}
			qs := q.New(ptx)
			list, err := qs.SigningSealedKeysAfter(ctx, q.SigningSealedKeysAfterParams{AfterID: after, Batch: int64(batch)})
			if err != nil {
				return err
			}
			n = len(list)
			for _, r := range list {
				cursor = r.ID
				sealed, changed, err := ring.Reseal(column, row(r.ID), r.SealedKey)
				switch {
				case err != nil:
					step.Failed++
				case !changed:
					step.Current++
				default:
					if err := qs.SigningUpdateSealedKey(ctx, q.SigningUpdateSealedKeyParams{ID: r.ID, Sealed: sealed}); err != nil {
						return err
					}
					step.Resealed++
				}
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		after = cursor
		res.Resealed += step.Resealed
		res.Current += step.Current
		res.Failed += step.Failed
		if n < batch {
			return res, nil
		}
	}
}
