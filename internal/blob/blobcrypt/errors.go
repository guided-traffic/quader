package blobcrypt

import "errors"

// Reason names the step of R-STOR-077 that refused a stored object. It is the value of the label reason of
// quader_storage_encrypted_read_failures_total (R-STOR-082).
type Reason string

// The reasons, in the order of the steps that give them.
const (
	// ReasonFormat: quader-format is missing or is not quader-blob-v1 (step 1).
	ReasonFormat Reason = "format"
	// ReasonUnknownKey: quader-key names a storage key this connection does not hold (step 2). It is no evidence about the
	// object: the export's import can bring the key back (Q-STOR-311).
	ReasonUnknownKey Reason = "unknown_key"
	// ReasonDestroyedKey: quader-key names a storage key of this connection whose row is destroyed (step 2, R-STOR-101). Unlike
	// unknown_key it is evidence about the copy: nothing brings that key back, so the copy is marked and repaired (Q-STOR-406). A key
	// lookup returns it, wrapped, and Open passes it through.
	ReasonDestroyedKey Reason = "destroyed_key"
	// ReasonRecord: the record is incomplete, its wrap does not open under the storage key it names, or the digest and size
	// the wrap carries are not the copy's (steps 2 to 4).
	ReasonRecord Reason = "record"
	// ReasonSize: the stored size is not S(L) of the plaintext size, or not the size the store answered (step 5), or a size
	// no plaintext size produces.
	ReasonSize Reason = "size"
	// ReasonCommitment: quader-commitment is missing or is not the commitment of the unwrapped data key (step 6).
	ReasonCommitment Reason = "commitment"
	// ReasonSegment: a data segment or a tail segment does not authenticate at its place (step 7).
	ReasonSegment Reason = "segment"
	// ReasonFinal: the final segment does not authenticate at its place, or its length or digest is not the copy's (step 7).
	ReasonFinal Reason = "final"
	// ReasonDigest: the SHA-256 of the plaintext is not the digest (step 7, on a whole read).
	ReasonDigest Reason = "digest"
)

// MarksCorrupt says whether a failure for this reason is evidence about the object, so that the copy is marked corrupt
// (R-STOR-082): every reason but unknown_key, destroyed_key included (R-STOR-101).
func (r Reason) MarksCorrupt() bool { return r != ReasonUnknownKey }

// Error is the refusal of a stored object with its reason. Its message carries the step and positions, never a key, a nonce
// seed or plaintext.
type Error struct {
	Reason Reason
	detail string
}

func (e *Error) Error() string {
	if e.detail == "" {
		return "stored object refused: " + string(e.Reason)
	}
	return "stored object refused (" + string(e.Reason) + "): " + e.detail
}

// Is makes errors.Is(err, ErrSegment) true for every refusal of that reason.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.detail == "" && t.Reason == e.Reason
}

// The sentinel of each reason, for errors.Is. They are of the interface type error, as the no-state rule of R-ARCH-032 asks
// of a sentinel.
var (
	ErrFormat     error = &Error{Reason: ReasonFormat}
	ErrUnknownKey error = &Error{Reason: ReasonUnknownKey}
	ErrRecord     error = &Error{Reason: ReasonRecord}
	ErrSize       error = &Error{Reason: ReasonSize}
	ErrCommitment error = &Error{Reason: ReasonCommitment}
	ErrSegment    error = &Error{Reason: ReasonSegment}
	ErrFinal      error = &Error{Reason: ReasonFinal}
	ErrDigest     error = &Error{Reason: ReasonDigest}
)

// ErrDestroyedKey is what a key lookup wraps for an identifier whose row is destroyed (R-STOR-101).
var ErrDestroyedKey error = &Error{Reason: ReasonDestroyedKey}

// ReasonOf returns the reason of a refusal anywhere in err's chain; ok is false for any other error (a transport error, a
// failed key lookup, a misuse of the package), which marks nothing.
func ReasonOf(err error) (reason Reason, ok bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason, true
	}
	return "", false
}

func refuse(r Reason, detail string) error { return &Error{Reason: r, detail: detail} }
