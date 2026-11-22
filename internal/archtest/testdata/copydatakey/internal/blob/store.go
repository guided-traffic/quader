package blob

// Store is the blob store; the declaration and a call inside the package are fine.
type Store struct{}

// DataKey stands for the key of one copy.
type DataKey struct{}

// CopyDataKey hands out the data key of a copy.
func (s *Store) CopyDataKey(d string) (DataKey, error) { return DataKey{}, nil }

func (s *Store) inside() { _, _ = s.CopyDataKey("x") }
