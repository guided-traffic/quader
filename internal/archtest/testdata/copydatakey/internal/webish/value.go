package webish

import "example.test/fix/internal/blob"

func value(s *blob.Store) func(string) (blob.DataKey, error) {
	f := s.CopyDataKey
	return f
}
