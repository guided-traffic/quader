// Package blobcrypt is the stored format of an encrypted connection, quader-blob-v1 (storage.md R-STOR-076 to R-STOR-079,
// the tail segments of R-STOR-081, the keyed names of R-STOR-086; ADR-244).
//
// It knows bytes, keys and records and nothing else: no connection, no database, no object store. Every primitive is the
// standard library's (AES-256-GCM, HKDF-SHA-256, HMAC-SHA-256, SHA-256), and the package imports nothing outside it, which a
// test holds.
//
//   - StoredSize and PlainSize are S(L) and its inverse; PlanRange is the stored bytes a ranged read fetches.
//   - NewSection is the writer: the stored form of a run of an object's segments, encrypted as it is read, with the nonces
//     of R-STOR-080 derived from a seed, so that a retry that seeks back gets byte for byte what it sent before.
//     NewTailSection is the same for a staging tail of an upload session.
//   - Wrap makes an object's record and Open runs steps 1 to 6 of R-STOR-077 on one, in their order, each failure with its
//     named reason; NewReader, NewRangeReader and NewTailReader run step 7 on the body and hand out no byte that did not
//     authenticate.
//   - KeyedName is the object name of a blob on a connection with keyed names.
//
// Keys live in Key values, whose bytes the package owns and clears on Destroy; their String and LogValue render nothing of
// them. Go's AES keeps an expanded key schedule that no code can erase (security page stored-content-keys.md, H-1309).
package blobcrypt
