package webish

import "example.test/fix/internal/blob"

func call(s *blob.Store) { _, _ = s.CopyDataKey("x") }
