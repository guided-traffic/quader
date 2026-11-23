package blob

import (
	"context"
	"errors"
	"strings"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
)

// The orphan sweep on an encrypted connection (R-STOR-087, Q-STOR-314): an object whose name has a copy row is checked as on a plain
// connection; an object without one is placed by its record: one HeadObject, steps 1 to 3 and 6 of R-STOR-077 (blobcrypt.OpenRecord)
// and the check that its name is the name of the digest its wrap binds (the keyed name under the connection's name key, or the
// digest's name without keyed names). Then stage 1's rules apply by that digest: adopted for a live blob, with the storage key its
// record names, present, or corrupt when its stored size is not S(L); claimed and deleted when no blob row exists and it is older
// than min_age. An object whose record does not open (an unknown or destroyed key, a record of no form, a changed wrap), whose name
// is not its digest's, or whose wrap binds another size than the live blob's, is reported as foreign and never deleted: Quader
// always writes the record with the bytes, so such an object is not Quader's writing of a blob there.

// blobFamily is the key prefix below which a connection's blob objects lie: keyed/ on an encrypted connection with keyed names
// (R-STOR-086), sha256/ otherwise (R-STOR-001).
func blobFamily(conn *Connection) string {
	if conn.KeyedNames() {
		return blobcrypt.KeyedPrefix(conn.Prefix())
	}
	return KeyPrefix(conn.Prefix())
}

// isKeyedName reports whether key has the layout of a keyed name below the connection's prefix: keyed/<m0m1>/<m2m3>/<64 hex of m>.
func isKeyedName(prefix, key string) bool {
	rest, ok := strings.CutPrefix(key, blobcrypt.KeyedPrefix(prefix))
	if !ok || len(rest) != 2+1+2+1+64 || rest[2] != '/' || rest[5] != '/' {
		return false
	}
	m := rest[6:]
	for i := range len(m) {
		if c := m[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return rest[0:2] == m[0:2] && rest[3:5] == m[2:4]
}

// sealedPage decides each object of a page of an encrypted connection (R-STOR-087).
func (s *sweep) sealedPage(ctx context.Context, conn *Connection, items []listed, res *SweepConnectionResult) error {
	if len(items) == 0 {
		return nil
	}
	rows, err := s.lookup(ctx, conn, items)
	if err != nil {
		return err
	}
	var unplaced []listed
	for _, it := range items {
		row := rows[it.o.Key]
		if !row.HasCopy {
			unplaced = append(unplaced, it)
			continue
		}
		// a copy row of a live blob in state deleting is a drain's claim: only a present copy is judged, by its stored size S(L)
		if row.CopyState == CopyPresent && it.o.Size >= 0 && it.o.Size != row.StoredSize {
			it.d = Digest(row.CopySha256)
			if s.mark(ctx, conn, it, row.StoredSize) {
				res.CopiesMarked++
			}
		}
	}
	placed := s.place(ctx, conn, unplaced, res)
	if len(placed) == 0 {
		return nil
	}
	rows, err = s.lookup(ctx, conn, placed)
	if err != nil {
		return err
	}
	var claims []Digest
	for _, it := range placed {
		row := rows[it.o.Key]
		switch {
		case row.HasCopy:
			// a row was written since the first look: the next run judges it
		case row.BlobState == "live":
			if conn.Drain() {
				continue // a draining connection's object is the drain's (Q-STOR-262)
			}
			if size, ok := plainOf(it.stored); !ok || size != row.BlobSize {
				s.foreignKey(conn, res, it.o.Key) // the wrap binds this digest with another size: not this blob's object
				continue
			}
			if s.adoptSealed(ctx, conn, it) {
				res.Adopted++
			}
		case row.BlobState == "deleting":
			// collection or the claim of an earlier sweep owns it
		default:
			if it.o.LastModified.IsZero() || !it.o.LastModified.Before(s.cutoff) {
				continue // younger than minAge, or of no known age: an upload may still insert its row
			}
			res.OrphansFound++
			if s.deletionsOff() {
				continue
			}
			ok, err := s.claim(ctx, conn, it)
			if err != nil {
				return err
			}
			if ok {
				claims = append(claims, it.d)
			}
		}
	}
	if len(claims) == 0 {
		return nil
	}
	deleted, reclaimed := s.r.res.Deleted, s.r.res.BytesReclaimed
	err = s.r.deleteObjects(ctx, claims)
	res.OrphansDeleted += s.r.res.Deleted - deleted
	res.BytesReclaimed += s.r.res.BytesReclaimed - reclaimed
	if s.r.res.Paused {
		s.paused, s.res.Paused = true, true
	}
	return err
}

// place reads the record of every object without a copy row and returns those it places, each with the digest, the storage key and
// the stored size its record names; it reports the others as foreign. An object the store no longer has, or that it does not answer
// for, is left for the next run.
func (s *sweep) place(ctx context.Context, conn *Connection, items []listed, res *SweepConnectionResult) []listed {
	if len(items) == 0 {
		return nil
	}
	keys, _ := conn.sealing()
	var out []listed
	for _, it := range items {
		info, err := conn.Objects().Head(ctx, it.o.Key)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			s.res.Errors++
			s.r.c.log.Warn("the orphan sweep could not read the record of an object; the next run looks again", "connection", conn.ID(), "key", it.o.Key, "error", err)
			continue
		}
		p, ok, err := s.placeOne(ctx, conn, keys, it, info)
		switch {
		case err != nil:
			s.res.Errors++
			s.r.c.log.Warn("the orphan sweep could not open the record of an object; the next run looks again", "connection", conn.ID(), "key", it.o.Key, "error", err)
		case !ok:
			s.foreignKey(conn, res, it.o.Key)
		default:
			out = append(out, p)
		}
	}
	return out
}

// placeOne opens the record of one object (info is its HeadObject) and checks its name against the digest the wrap binds. ok is false
// for an object that cannot be placed; err is a failure that says nothing about the object (the database behind the key lookup).
func (s *sweep) placeOne(ctx context.Context, conn *Connection, keys StorageKeys, it listed, info ObjectInfo) (listed, bool, error) {
	if keys == nil {
		return it, false, errors.New("this replica holds no storage keys of the connection")
	}
	dk, err := blobcrypt.OpenRecord(info.Metadata, keys.Lookup(ctx, conn.ID()))
	if err != nil {
		if _, refused := blobcrypt.ReasonOf(err); refused {
			s.r.c.log.Warn("an object without a copy row has no record this replica opens; it is left alone", "connection", conn.ID(), "key", it.o.Key, "error", err)
			return it, false, nil
		}
		return it, false, err
	}
	defer dk.Key.Destroy()
	var name string
	if conn.KeyedNames() {
		nk, err := keys.NameKey(conn.ID())
		if err != nil {
			return it, false, err
		}
		if name, err = blobcrypt.KeyedName(nk, conn.Prefix(), dk.Digest); err != nil {
			return it, false, err
		}
	} else {
		name = Key(conn.Prefix(), Digest(dk.Digest))
	}
	if name != it.o.Key {
		s.r.c.log.Warn("an object's record names a blob whose name on the connection is another; it is left alone", "connection", conn.ID(), "key", it.o.Key)
		return it, false, nil
	}
	stored, err := blobcrypt.StoredSize(dk.Size)
	if err != nil {
		return it, false, nil //nolint:nilerr // OpenRecord refused a size outside the format; an unplaceable object, not a failure
	}
	it.d, it.keyID, it.stored = Digest(dk.Digest), dk.KeyID, stored
	if info.Size >= 0 {
		it.o.Size = info.Size
	}
	return it, true, nil
}

// adoptSealed adopts a placed object of a live blob (R-STOR-087): present and not verified, so the scrub reads it first, or corrupt
// with a queued repair when the size the store answered is not S(L) of the size its wrap binds; the row names the storage key of its
// record.
func (s *sweep) adoptSealed(ctx context.Context, conn *Connection, it listed) bool {
	if s.o.DryRun {
		return true
	}
	state := CopyPresent
	if it.o.Size != it.stored {
		state = CopyCorrupt
	}
	return s.adoptRow(ctx, conn, it, it.stored, state, it.keyID)
}

// plainOf is the plaintext size a stored size stands for, false for one no plaintext size produces.
func plainOf(stored int64) (int64, bool) {
	n, err := blobcrypt.PlainSize(stored)
	return n, err == nil
}
