package main

import "example.test/fix/internal/blob"

func main() { _, _ = (&blob.Store{}).CopyDataKey("x") }
