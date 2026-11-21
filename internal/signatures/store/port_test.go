package store_test

import (
	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/signatures/store"
)

// The store is the API's port of signatures on artifacts (R-SIG-034). The assertion sits in a test: the store, a domain package, may
// not import the edge layer.
var _ v1.SignatureStore = (*store.Store)(nil)
