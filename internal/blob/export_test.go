package blob

import (
	"context"
	"log/slog"
)

// MapStoreErr exposes the mapping of a store failure to the answer of a request.
func MapStoreErr(err error) error { return mapStoreErr(err) }

// PlaceByRecord runs the orphan sweep's placing of one object without a copy row on an encrypted connection (R-STOR-087,
// Q-STOR-314): its HeadObject, its record and the check of its name. ok is false for an object the sweep reports as foreign.
func PlaceByRecord(ctx context.Context, conn *Connection, key string) (d Digest, keyID string, stored int64, ok bool, err error) {
	s := &sweep{r: &run{c: &Collector{log: slog.New(slog.DiscardHandler)}}}
	info, err := conn.Objects().Head(ctx, key)
	if err != nil {
		return Digest{}, "", 0, false, err
	}
	keys, _ := conn.sealing()
	it, ok, err := s.placeOne(ctx, conn, keys, listed{o: ObjectInfo{Key: key, Size: info.Size}}, info)
	return it.d, it.keyID, it.stored, ok, err
}
