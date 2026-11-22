// Package keys holds the storage keys and name keys of the encrypted connections (storage.md R-STOR-083 to R-STOR-085): the rows of
// storage_keys, each 32 bytes from the system's random source sealed by the key ring under its identifier; the drawing at a
// connection's first sight, refused over a location that already holds objects; the keys a replica holds in memory for its life,
// by identifier, refreshed by the change area storage_keys and by a re-read for an identifier it does not hold; a further generation
// on command; the export of the rows as they lie and their import; the walks of the task secrets-reencrypt over the two sealed
// columns of the stage (storage_keys.sealed_key and upload_sessions.sealed_data_key, R-ARCH-021); and the seal of an upload
// session's data key (R-STOR-081).
//
// Nothing here logs, returns in an error or puts in a metric's label a key or a sealed value: a key lives in a blobcrypt.Key, whose
// String and LogValue render nothing, and is destroyed by Holder.Close; a key drawn to be sealed is destroyed as soon as its seal is
// made. The package knows no format and no request; internal/blob reads the keys through blob.StorageKeys.
package keys
