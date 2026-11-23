package blobcrypt

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
)

// The four metadata keys of the record, below the prefix quader- that stage 1 reserves (R-STOR-077). A reader ignores every
// other key below quader-.
const (
	MetaFormat     = "quader-format"
	MetaKey        = "quader-key"
	MetaWrap       = "quader-wrap"
	MetaCommitment = "quader-commitment"
)

// The sizes inside the wrap W = salt ‖ nonce ‖ Seal(K_w, nonce, DEK ‖ digest ‖ u64(L), FMT ‖ " wrap" ‖ u8(len(id)) ‖ id).
const (
	wrapSaltSize  = 16
	wrapPlainSize = KeySize + 32 + 8
	// WrapSize is the size of W: 116 bytes, 155 characters of base64url without padding.
	WrapSize = wrapSaltSize + NonceSize + wrapPlainSize + TagSize
)

// b64 is base64url without padding (RFC 4648 §5), strict, so that one value has one spelling. A function, not a package
// variable: R-ARCH-032 keeps package state out of this package.
func b64() *base64.Encoding { return base64.RawURLEncoding.Strict() }

// Record is the record of one object: the identifier of the storage key, the wrap and the commitment. Metadata spells it as
// the four keys written in the same request as the bytes.
type Record struct {
	KeyID      string
	Wrap       []byte
	Commitment [32]byte
}

// Metadata is the record as the object's metadata.
func (r Record) Metadata() map[string]string {
	return map[string]string{
		MetaFormat:     Format,
		MetaKey:        r.KeyID,
		MetaWrap:       b64().EncodeToString(r.Wrap),
		MetaCommitment: b64().EncodeToString(r.Commitment[:]),
	}
}

// Wrap makes the record of an object whose data key is dk.Key, bound to dk.Digest and dk.Size, under the storage key sk: a
// salt and a nonce drawn from random (crypto/rand.Reader everywhere but in the vectors, which read the salt first), K_w
// derived per wrap, and the commitment of the data key (R-STOR-077). dk.KeyID is not read; the record names sk.
func Wrap(sk *StorageKey, dk DataKey, random io.Reader) (Record, error) {
	if sk == nil || !ValidKeyID(sk.ID) {
		return Record{}, errors.New("blobcrypt: wrap without a storage key")
	}
	if dk.Size < 0 || dk.Size > MaxPlainSize {
		return Record{}, errOutOfRange
	}
	dek, err := dk.Key.bytes()
	if err != nil {
		return Record{}, err
	}
	skb, err := sk.Key.bytes()
	if err != nil {
		return Record{}, err
	}
	w := make([]byte, wrapSaltSize+NonceSize, WrapSize)
	if _, err := io.ReadFull(random, w); err != nil {
		return Record{}, errors.New("blobcrypt: drawing the wrap's salt and nonce: " + err.Error())
	}
	salt, nonce := w[:wrapSaltSize], w[wrapSaltSize:]
	aead, err := newGCM(derive(skb, salt, labelWrap))
	if err != nil {
		return Record{}, err
	}
	plain := make([]byte, 0, wrapPlainSize)
	plain = append(plain, dek...)
	plain = append(plain, dk.Digest[:]...)
	plain = be64(plain, dk.Size)
	w = aead.Seal(w, nonce, plain, wrapAD(nil, sk.ID))
	clear(plain)
	c, err := commitment(dk.Key)
	if err != nil {
		return Record{}, err
	}
	return Record{KeyID: sk.ID, Wrap: w, Commitment: c}, nil
}

// KeyLookup returns the storage key a record names, or nil when the connection holds no key of that identifier after the
// replica read the rows again (R-STOR-083). An error is passed through as it is: a failed lookup is no finding about the
// object and marks nothing, unless it wraps ErrDestroyedKey, the identifier of a destroyed row (R-STOR-101).
type KeyLookup func(id string) (*StorageKey, error)

// Want is what a reader knows of a copy before it opens the record.
type Want struct {
	// Known is true when Digest, Size and StoredSize come from the database: the blob row's sha256 and size and the copy row's
	// stored_size (steps 4 and 5). A reader without a database (the offline tool, the orphan sweep) leaves it false and learns
	// the digest and size from the wrap.
	Known      bool
	Digest     [32]byte
	Size       int64
	StoredSize int64
	// ObjectSize is the size of the object, or the total of a ranged answer, as the store answered it (step 5).
	ObjectSize int64
}

// Open runs steps 1 to 6 of R-STOR-077 on an object's metadata, in their order, and returns the data key with the digest and
// size its wrap binds; each failure is a refusal with the reason of its step. Nothing of the body is read: a refusal here
// comes before the first byte. The caller destroys the returned key when its read is done.
func Open(meta map[string]string, keys KeyLookup, want Want) (DataKey, error) {
	dk, err := openWrap(meta, keys) // steps 1 to 3
	if err != nil {
		return DataKey{}, err
	}
	if err := checkCopy(dk, want); err != nil { // steps 4 and 5
		dk.Key.Destroy()
		return DataKey{}, err
	}
	if err := checkCommitment(meta[MetaCommitment], dk.Key); err != nil { // step 6
		dk.Key.Destroy()
		return DataKey{}, err
	}
	return dk, nil
}

// OpenRecord runs steps 1 to 3 and step 6 of R-STOR-077 on an object's metadata and returns the data key with the digest and size
// its wrap binds, without steps 4 and 5: for a reader that has no copy row to hold the record against and learns the blob from the
// wrap (the orphan sweep, R-STOR-087, Q-STOR-314). The caller compares the stored size with StoredSize of the returned size itself,
// and destroys the key.
func OpenRecord(meta map[string]string, keys KeyLookup) (DataKey, error) {
	dk, err := openWrap(meta, keys)
	if err != nil {
		return DataKey{}, err
	}
	if err := checkCommitment(meta[MetaCommitment], dk.Key); err != nil {
		dk.Key.Destroy()
		return DataKey{}, err
	}
	return dk, nil
}

// openWrap runs steps 1 to 3: the format, the storage key the record names, the wrap opened under it.
func openWrap(meta map[string]string, keys KeyLookup) (DataKey, error) {
	if meta[MetaFormat] != Format { // step 1; a missing key reads as ""
		return DataKey{}, refuse(ReasonFormat, MetaFormat+" is not "+Format)
	}
	id, ok := meta[MetaKey] // step 2
	if !ok || !ValidKeyID(id) {
		return DataKey{}, refuse(ReasonRecord, MetaKey+" is missing or is no key identifier")
	}
	sk, err := keys(id)
	if err != nil {
		return DataKey{}, err
	}
	if sk == nil {
		return DataKey{}, refuse(ReasonUnknownKey, "this connection holds no storage key "+id)
	}
	if sk.ID != id {
		return DataKey{}, errors.New("blobcrypt: the key lookup for " + id + " returned the storage key " + sk.ID)
	}
	return unwrap(meta[MetaWrap], sk) // step 3
}

// CheckRecordForm checks, without a key, that metadata carries a record of the stored format in its form (R-STOR-077): the format's
// identifier, a key identifier, a wrap of WrapSize bytes and a commitment of 32 bytes, both base64url. It is what a reader that holds
// no storage key can check of a record (a backup's deep verification without the key ring, R-BAK-031, Q-BAK-66); it proves nothing
// about the bytes.
func CheckRecordForm(meta map[string]string) error {
	if meta[MetaFormat] != Format {
		return refuse(ReasonFormat, MetaFormat+" is not "+Format)
	}
	if !ValidKeyID(meta[MetaKey]) {
		return refuse(ReasonRecord, MetaKey+" is missing or is no key identifier")
	}
	if w, err := b64().DecodeString(meta[MetaWrap]); err != nil || len(w) != WrapSize {
		return refuse(ReasonRecord, MetaWrap+" is missing or is not "+strconv.Itoa(WrapSize)+" bytes of base64url")
	}
	if c, err := b64().DecodeString(meta[MetaCommitment]); err != nil || len(c) != 32 {
		return refuse(ReasonCommitment, MetaCommitment+" is missing or is not 32 bytes of base64url")
	}
	return nil
}

// unwrap opens W under the storage key it names (step 3).
func unwrap(value string, sk *StorageKey) (DataKey, error) {
	w, err := b64().DecodeString(value)
	if err != nil || len(w) != WrapSize {
		return DataKey{}, refuse(ReasonRecord, MetaWrap+" is missing or is not "+strconv.Itoa(WrapSize)+" bytes of base64url")
	}
	skb, err := sk.Key.bytes()
	if err != nil {
		return DataKey{}, err
	}
	salt, nonce, sealed := w[:wrapSaltSize], w[wrapSaltSize:wrapSaltSize+NonceSize], w[wrapSaltSize+NonceSize:]
	aead, err := newGCM(derive(skb, salt, labelWrap))
	if err != nil {
		return DataKey{}, err
	}
	plain, err := aead.Open(nil, nonce, sealed, wrapAD(nil, sk.ID))
	if err != nil {
		return DataKey{}, refuse(ReasonRecord, MetaWrap+" does not open under the storage key "+sk.ID)
	}
	defer clear(plain)
	size := int64(binary.BigEndian.Uint64(plain[KeySize+32:])) //nolint:gosec // G115: a value above 2^63 turns negative and is refused below
	if size < 0 || size > MaxPlainSize {
		return DataKey{}, refuse(ReasonRecord, MetaWrap+" carries a plaintext size outside the format")
	}
	key, err := KeyFromBytes(plain[:KeySize])
	if err != nil {
		return DataKey{}, err
	}
	dk := DataKey{Key: key, KeyID: sk.ID, Size: size}
	copy(dk.Digest[:], plain[KeySize:KeySize+32])
	return dk, nil
}

// checkCopy runs steps 4 and 5: the digest and size are the copy's, and S(L) is the copy's stored size and the store's.
func checkCopy(dk DataKey, want Want) error {
	if want.Known && (subtle.ConstantTimeCompare(dk.Digest[:], want.Digest[:]) != 1 || dk.Size != want.Size) {
		return refuse(ReasonRecord, "the wrap binds another digest or size than the copy's")
	}
	stored, _ := StoredSize(dk.Size) // unwrap refused a size outside the format
	if want.Known && stored != want.StoredSize {
		return refuse(ReasonSize, "S(L) is "+strconv.FormatInt(stored, 10)+", the copy's stored size "+strconv.FormatInt(want.StoredSize, 10))
	}
	if stored != want.ObjectSize {
		return refuse(ReasonSize, "S(L) is "+strconv.FormatInt(stored, 10)+", the store answered "+strconv.FormatInt(want.ObjectSize, 10))
	}
	return nil
}

// checkCommitment runs step 6: C from the data key equals quader-commitment, compared in constant time.
func checkCommitment(value string, key Key) error {
	got, err := b64().DecodeString(value)
	if err != nil || len(got) != 32 {
		return refuse(ReasonCommitment, MetaCommitment+" is missing or is not 32 bytes of base64url")
	}
	c, err := commitment(key)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(got, c[:]) != 1 {
		return refuse(ReasonCommitment, MetaCommitment+" is not the commitment of the data key")
	}
	return nil
}
