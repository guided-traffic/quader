package budget

import (
	"context"
	"time"
)

// pacer paces one transfer to its subject's bytes per second (Q-BAS-330): the subject's transfers on every replica take their
// bytes from one counter per slice of 100 ms, which holds a tenth of the figure; a transfer whose claim finds the slice used up
// still writes the floor's share of the slice (64 KiB a second), so no transfer is ever slower than the floor, and N transfers of
// a subject at its figure each get about an N-th of it.
type pacer struct {
	c    *Charge
	rate int64
	kind string

	idx   int64 // the slice the allowance belongs to
	left  int64 // what the transfer may still write in it
	floor bool  // whether it took the floor's share of the slice
}

const slicesPerSecond = int64(time.Second / Slice)

func perSlice(bytesPerSecond int64) int64 { return max(bytesPerSecond/slicesPerSecond, 1) }

// take waits until n bytes may be written and reports how long it waited.
func (p *pacer) take(ctx context.Context, n int64) time.Duration {
	var waited time.Duration
	for n > 0 {
		now := p.c.b.o.Now()
		idx := now.UnixNano() / int64(Slice)
		if idx != p.idx {
			p.idx, p.left, p.floor = idx, 0, false
		}
		if p.left == 0 {
			limit := perSlice(p.rate)
			want := min(n, max(limit/4, 4<<10), limit)
			if got := p.c.b.o.Counters.Claim(ctx, p.c.name("pace"+p.kind), idx, Slice+10*time.Second, want, limit); got > 0 {
				p.left = got
			} else if !p.floor {
				p.left, p.floor = perSlice(FloorBytesPerSecond), true
			}
		}
		if p.left == 0 {
			// The slice is used up for this transfer: wait for the next one.
			wait := time.Unix(0, (idx+1)*int64(Slice)).Sub(now)
			if err := p.c.b.o.Sleep(ctx, wait); err != nil {
				return waited
			}
			waited += wait
			continue
		}
		step := min(n, p.left)
		p.left -= step
		n -= step
	}
	return waited
}
