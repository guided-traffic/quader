package webish

import (
	"testing"

	"example.test/fix/internal/blob"
)

// A test may call it: the test files are not checked.
func TestCall(t *testing.T) { _, _ = (&blob.Store{}).CopyDataKey("x") }
