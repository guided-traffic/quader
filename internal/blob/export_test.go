package blob

// MapStoreErr exposes the mapping of a store failure to the answer of a request.
func MapStoreErr(err error) error { return mapStoreErr(err) }
