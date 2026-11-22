package downloadlinks

import "example.test/fix/internal/blob"

// handover is the one caller outside the store.
func handover(s *blob.Store) { _, _ = s.CopyDataKey("x") }
