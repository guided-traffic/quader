package webish

import "example.test/fix/internal/blob"

// keyer hides the store behind an interface; its method's name is reserved.
type keyer interface {
	CopyDataKey(d string) (blob.DataKey, error)
}

func viaInterface(k keyer) { _, _ = k.CopyDataKey("x") }
