package keys

import "example.test/fix/internal/blob"

// A subpackage of the store is not the store.
func sub(s *blob.Store) { _, _ = s.CopyDataKey("x") }
