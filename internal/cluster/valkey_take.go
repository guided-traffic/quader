package cluster

import (
	"context"
	"crypto/sha1" //nolint:gosec // G505: SHA-1 names a script in the server's script cache (EVALSHA); it protects nothing
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// The take of a request (R-ARCH-034, usage-and-limits.md R-LIM-017, Q-LIM-57): every window a request must fit (the request
// windows of its budget key, the class of R-API-016 that is in force, or the one window of a limiter) is decided and counted
// in one step, so a refused request is not counted in any of them and the Retry-After of its refusal is the time after which
// the caller's next request fits. Valkey runs the step as one script inside the batch's MULTI/EXEC: the round trip of the
// request stays one. The script reads only the keys it is given, runs only GET, INCRBY and EXPIRE, which the ACL of
// R-ARCH-031 grants, and is replicated to the replicas by its effects.
//
// KEYS: the block marker (when ARGV[3] is 1), then the current and the previous counter of each window.
// ARGV: the units n, now in Unix milliseconds, 1 or 0 for the marker, then per window its limit, the elapsed part of the
// window (0 to 1) and the counter's time to live in seconds.
// Reply: {admitted 1 or 0, the marker or nil, then per window cur, prev, fits 1 or 0}, cur and prev without the units.
const takeScript = `local n = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local ok = 1
local k = 1
local mark = false
if ARGV[3] == '1' then
  mark = redis.call('GET', KEYS[1])
  if mark then
    local e = tonumber(string.match(mark, '|(%d+)$'))
    if e and e > now then ok = 0 end
  end
  k = 2
end
local out = {0, mark}
local a = 4
local w = {}
while KEYS[k] do
  local cur = tonumber(redis.call('GET', KEYS[k]) or '0')
  local prev = tonumber(redis.call('GET', KEYS[k + 1]) or '0')
  local fit = 0
  if prev * (1 - tonumber(ARGV[a + 1])) + cur + n <= tonumber(ARGV[a]) then fit = 1 else ok = 0 end
  out[#out + 1] = cur
  out[#out + 1] = prev
  out[#out + 1] = fit
  w[#w + 1] = {KEYS[k], ARGV[a + 2]}
  k = k + 2
  a = a + 3
end
if ok == 1 then
  for _, c in ipairs(w) do
    redis.call('INCRBY', c[1], n)
    redis.call('EXPIRE', c[1], c[2], 'NX')
  end
end
out[1] = ok
return out
`

// takeScriptSHA names the script in the server's script cache.
var takeScriptSHA = func() string {
	sum := sha1.Sum([]byte(takeScript)) //nolint:gosec // G401: the script cache's name, see the import
	return hex.EncodeToString(sum[:])
}()

// WindowTake is one window of an [OpTake].
type WindowTake struct {
	// Class puts the counter in the family of the limiters of R-ARCH-034 (rl) instead of the download limits' (lim): the class
	// of R-API-016 that stage 7 decides with the request windows, and the window of a limiter's own take.
	Class  bool
	Figure string
	Bucket string
	Window time.Duration
	Index  int64
	// Elapsed is how far into the window the request arrived (0 to 1); Limit is the most the window's estimate, the take's
	// units included, may reach.
	Elapsed float64
	Limit   int64
}

// WindowState is what a take found in one window: the current and the previous counter without the take's units, and whether
// the units fit.
type WindowState struct {
	Cur, Prev int64
	Fits      bool
}

// takeKey forms a counter key of a window of a take.
func (v *Valkey) takeKey(w WindowTake, idx int64) (Key, error) {
	win := max(w.Window, time.Second)
	fam := limFamily
	if w.Class {
		fam = rlFamily
	}
	return v.formKeyTTL(fam, min(2*win, fam.maxTTL), w.Figure, w.Bucket, strconv.FormatInt(idx, 10))
}

// takeArgs are the keys and the arguments of the script for a take.
func (v *Valkey) takeArgs(op LimitOp) (keys, args []string, err error) {
	marker := "0"
	if op.Marker {
		k, err := v.limKey(LimitOp{Kind: OpMarkerGet, Bucket: op.Bucket}, false)
		if err != nil {
			return nil, nil, err
		}
		keys, marker = append(keys, k.name), "1"
	}
	args = []string{strconv.FormatInt(max(1, op.N), 10), strconv.FormatInt(op.Now.UnixMilli(), 10), marker}
	for _, w := range op.Windows {
		cur, err := v.takeKey(w, w.Index)
		if err != nil {
			return nil, nil, err
		}
		prev, err := v.takeKey(w, w.Index-1)
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, cur.name, prev.name)
		args = append(args, strconv.FormatInt(w.Limit, 10), strconv.FormatFloat(w.Elapsed, 'f', -1, 64),
			strconv.FormatInt(ceilSeconds(cur.ttl), 10))
	}
	return keys, args, nil
}

// readTake reads the script's reply.
func readTake(m valkey.ValkeyMessage) (LimitResult, error) {
	var r LimitResult
	arr, err := m.ToArray()
	if err != nil {
		return r, err
	}
	if len(arr) < 2 || (len(arr)-2)%3 != 0 {
		return r, fmt.Errorf("the take returned %d values", len(arr))
	}
	ok, err := arr[0].AsInt64()
	if err != nil {
		return r, err
	}
	r.Admitted = ok == 1
	if !arr[1].IsNil() {
		if r.Value, err = arr[1].ToString(); err != nil {
			return r, err
		}
	}
	for i := 2; i < len(arr); i += 3 {
		var s WindowState
		var fit int64
		if s.Cur, err = arr[i].AsInt64(); err == nil {
			if s.Prev, err = arr[i+1].AsInt64(); err == nil {
				fit, err = arr[i+2].AsInt64()
			}
		}
		if err != nil {
			return r, err
		}
		s.Fits = fit == 1
		r.Windows = append(r.Windows, s)
	}
	return r, nil
}

// takeWithBody sends the takes of a batch that met an empty script cache once more with the script's body (EVAL), which runs
// them and loads the script for the next EVALSHA. Each did nothing the first time, so nothing is counted twice.
func (v *Valkey) takeWithBody(ctx context.Context, b *LimitBatch, missing []int, out []LimitResult) error {
	return v.exec(ctx, "eval", func(ctx context.Context, c valkey.Client) error {
		cmds := make(valkey.Commands, 0, len(missing))
		for _, i := range missing {
			keys, args, err := v.takeArgs(b.Ops[i])
			if err != nil {
				return err
			}
			cmds = append(cmds, c.B().Eval().Script(takeScript).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build())
		}
		for n, res := range c.DoMulti(ctx, cmds...) {
			m, err := res.ToMessage()
			if err != nil {
				return err
			}
			if out[missing[n]], err = readTake(m); err != nil {
				return err
			}
		}
		return nil
	})
}

// markerHolds reports whether a block marker (<block ID>|<end in Unix milliseconds>) holds a block that ends after now, as the
// script reads it.
func markerHolds(v string, now time.Time) bool {
	i := strings.LastIndexByte(v, '|')
	if i < 0 {
		return false
	}
	end, err := strconv.ParseInt(v[i+1:], 10, 64)
	return err == nil && end > now.UnixMilli()
}

// take is the script's step in the replica's memory. The caller holds m.mu.
func (m *MemoryLimits) take(op LimitOp, now time.Time) LimitResult {
	var r LimitResult
	ok := true
	if op.Marker {
		if mk, found := m.markers[op.Bucket]; found && !now.After(mk.expires) {
			r.Value = mk.v
			ok = !markerHolds(mk.v, op.Now)
		}
	}
	n := max(1, op.N)
	for _, w := range op.Windows {
		cw := LimitOp{Figure: w.Figure, Bucket: w.Bucket}
		s := WindowState{Cur: m.counter(memWindowKey(cw, w.Index), now), Prev: m.counter(memWindowKey(cw, w.Index-1), now)}
		s.Fits = float64(s.Prev)*(1-w.Elapsed)+float64(s.Cur)+float64(n) <= float64(w.Limit)
		ok = ok && s.Fits
		r.Windows = append(r.Windows, s)
	}
	if !ok {
		return r
	}
	r.Admitted = true
	for _, w := range op.Windows {
		k := memWindowKey(LimitOp{Figure: w.Figure, Bucket: w.Bucket}, w.Index)
		c, found := m.windows[k]
		if !found || now.After(c.expires) {
			c = &memCounter{expires: now.Add(2 * max(w.Window, time.Second))}
			m.windows[k] = c
		}
		c.v += n
	}
	return r
}

// Return gives back what an admitted take counted: the replica's fallback undoes the take of a request that a check decided
// after it refused (the class of R-API-016 on its own share, R-LIM-019).
func (m *MemoryLimits) Return(op LimitOp) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, w := range op.Windows {
		if c, ok := m.windows[memWindowKey(LimitOp{Figure: w.Figure, Bucket: w.Bucket}, w.Index)]; ok && !now.After(c.expires) {
			c.v = max(0, c.v-max(1, op.N))
		}
	}
}
