// Package offline is the reader of `quader admin storage offline` (storage.md R-STOR-089, Q-STOR-316): it lists, reads and
// verifies the objects of an encrypted connection's bucket, or of a directory written by its pull, with the export of the storage
// keys (R-STOR-085) and the files of the key ring that sealed it (R-ARCH-021), and without a database, Valkey or a running Quader.
// It is the last resort when the database is lost and no backup holds the blobs, and the second reader of the stored format beside
// the store's (internal/blob): both read through internal/blob/blobcrypt, this one without the copy rows, so the digest and the size
// come from each object's wrap and the object's name stands in for the database (a keyed name is the name key's MAC of the digest,
// a plain name the digest itself).
//
// Nothing here prints, logs or returns in an error a key, a part of one or a sealed value: the keys live in blobcrypt.Key values,
// whose String and LogValue render nothing, and are destroyed by Keys.Close; a refusal names identifiers, root keys, files and
// object names only. The key ring and the export come from files the operator names, never from an argument. Decrypted content is
// written only by Get, to a temporary file beside the operator's --out that becomes --out only after every segment, the final
// segment and the SHA-256 have passed, and never over a file that exists. Nothing writes to a bucket.
package offline
