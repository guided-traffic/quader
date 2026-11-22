package keys

import (
	"context"
	"errors"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/secrets"
)

// ResealResult counts what a walk of the task secrets-reencrypt did with the values of a column: sealed again under the active root
// key, already under it, or not opened with this replica's ring and left as they are.
type ResealResult struct{ Resealed, Current, Failed int64 }

// add sums two results.
func (r ResealResult) add(o ResealResult) ResealResult {
	return ResealResult{Resealed: r.Resealed + o.Resealed, Current: r.Current + o.Current, Failed: r.Failed + o.Failed}
}

const defaultBatch = 200

// Reseal seals every storage key and name key of storage_keys.sealed_key, and every open session's data key of
// upload_sessions.sealed_data_key, under the active root key of the ring (R-STOR-084 step 3, R-ARCH-021): the walks of the task
// secrets-reencrypt over the two columns, in batches of one transaction each that lock their rows. The keys inside do not change, so
// no replica reloads anything and no object is read or written.
func Reseal(ctx context.Context, db meta.Runner, rows Rows, ring Resealer, batch int) (ResealResult, error) {
	if rows == nil {
		rows = PG{}
	}
	if batch <= 0 {
		batch = defaultBatch
	}
	keys, err := walk(ctx, db, batch, func(ctx context.Context, tx meta.Tx, after string) ([]SessionKey, error) {
		list, err := rows.SealedAfter(ctx, tx, after, batch)
		out := make([]SessionKey, 0, len(list))
		for _, r := range list {
			out = append(out, SessionKey{ID: r.KeyID, Sealed: r.Sealed})
		}
		return out, err
	}, func(ctx context.Context, tx meta.Tx, v SessionKey) (bool, error) {
		return reseal(ctx, tx, ring, keyColumn, v, rows.UpdateSealed)
	})
	if err != nil {
		return keys, err
	}
	sessions, err := walk(ctx, db, batch, func(ctx context.Context, tx meta.Tx, after string) ([]SessionKey, error) {
		return rows.SessionKeysAfter(ctx, tx, after, batch)
	}, func(ctx context.Context, tx meta.Tx, v SessionKey) (bool, error) {
		return reseal(ctx, tx, ring, sessionColumn, v, rows.UpdateSessionKey)
	})
	return keys.add(sessions), err
}

// reseal seals one value under the active key and writes it; changed is false for a value under it already. A value that does not
// open is an error the walk counts as failed.
func reseal(ctx context.Context, tx meta.Tx, ring Resealer, c secrets.Column, v SessionKey, update func(context.Context, meta.Tx, string, string) error) (bool, error) {
	sealed, changed, err := ring.Reseal(c, secrets.Row(v.ID), v.Sealed)
	if err != nil || !changed {
		return false, err
	}
	return true, update(ctx, tx, v.ID, sealed)
}

// walk pages through a column after the last row key of the page before, one transaction per page.
func walk(ctx context.Context, db meta.Runner, batch int,
	page func(context.Context, meta.Tx, string) ([]SessionKey, error),
	one func(context.Context, meta.Tx, SessionKey) (bool, error),
) (ResealResult, error) {
	var res ResealResult
	after := ""
	for {
		var n int
		cursor, step := after, ResealResult{}
		err := db.InTx(ctx, func(tx meta.Tx) error {
			cursor, step = after, ResealResult{}
			list, err := page(ctx, tx, after)
			if err != nil {
				return err
			}
			n = len(list)
			for _, v := range list {
				cursor = v.ID
				changed, err := one(ctx, tx, v)
				switch {
				case err != nil && isOpenFailure(err):
					step.Failed++
				case err != nil:
					return err
				case changed:
					step.Resealed++
				default:
					step.Current++
				}
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		after = cursor
		res = res.add(step)
		if n < batch {
			return res, nil
		}
	}
}

// isOpenFailure reports whether err is the ring's refusal to open a value (a root key it lacks, a value changed or moved, no
// envelope), which the walk counts and leaves alone, rather than a failure of the database, which ends the walk.
func isOpenFailure(err error) bool {
	return errors.Is(err, secrets.ErrUnknownKey) || errors.Is(err, secrets.ErrAuthentication) || errors.Is(err, secrets.ErrMalformed)
}
