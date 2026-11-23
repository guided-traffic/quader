package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
)

// The counters of the download limits (R-LIM-017, usage-and-limits.md). Every check of the limits is one batch: the
// operations of a request (the take of the request windows of its budget key with its block marker and the window of its
// R-API-016 class, valkey_take.go), of a download (the hour and day byte windows, the parallel lease) or of a flush (the byte
// and refused charges of an interval, the leases that ended or are renewed, the markers that are due) go to Valkey in one
// MULTI/EXEC, a single pipelined round trip under valkey.command_timeout. The commands are those of R-ARCH-031: GET, SET,
// DELIFEQ, INCRBY, EXPIRE, ZADD, ZREM, ZREMRANGEBYSCORE, ZCARD, and EVALSHA (EVAL after a lost script cache) for the take,
// whose script decides and counts in one step (Q-LIM-57). No DEL is used.
//
// Keys are quader:<ks>:lim:v1:<figure>:<b>:<window index>, quader:<ks>:lim:v1:par:<b> (the leases of a budget key, a
// sorted set scored by the lease's end) and quader:<ks>:lim:v1:blk:<b> (a block marker), with <b> the hash of the budget key
// or ceiling key (R-ARCH-030). A window counter expires two windows after it opened, so the family's longest life is 48
// hours (two days).

// MaxLimitWindow is the longest window of a figure of the download limits: one day.
const MaxLimitWindow = 24 * time.Hour

// limFamily is the key family of the download limits (mechanism in this file).
var limFamily = registerKeyFamily("lim", 1, 2*MaxLimitWindow)

// LimitOpKind is what one operation of a [LimitBatch] does.
type LimitOpKind uint8

// The operations of a batch.
const (
	// OpWindowAdd adds N to the current window of a figure (INCRBY, EXPIRE NX) and reads the previous one (GET).
	OpWindowAdd LimitOpKind = iota + 1
	// OpWindowRead reads the current and the previous window of a figure (GET, GET).
	OpWindowRead
	// OpWindowZero sets the window of a figure to 0 if it exists (SET … XX KEEPTTL): a lifted block resets the refused
	// counters so that the next refused request does not block again at once (R-LIM-025).
	OpWindowZero
	// OpTake adds N to every window of Windows if, with the N units, each window's estimate stays within its limit and the
	// block marker of Bucket (when Marker is set) holds no block that lasts beyond Now; otherwise it adds nothing. It is the
	// request windows' check of stage 7 (R-LIM-017, Q-LIM-57) and a limiter's take (R-ARCH-034): a refused request is not
	// counted. Valkey runs it as one script (valkey_take.go).
	OpTake
	// OpGiveBack undoes an admitted OpTake of the same Windows and N: it subtracts N from the current counter of every window
	// that is not a Peek, never below 0 and never in a counter that has expired. A check of the request's step that decided
	// after the take refused the request (a parallel download at step 9a, Q-LIM-110). It runs in the same script as the take.
	OpGiveBack
	// OpMarkerGet reads a block marker (GET).
	OpMarkerGet
	// OpMarkerSet sets a block marker if none is set (SET NX EX).
	OpMarkerSet
	// OpMarkerDrop removes a block marker if it holds Value (DELIFEQ).
	OpMarkerDrop
	// OpLeaseTake drops the expired leases of a key, adds the lease Member with the score Until, renews the key's expiry and
	// counts the leases (ZREMRANGEBYSCORE, ZADD, EXPIRE, ZCARD).
	OpLeaseTake
	// OpLeaseDrop removes a lease (ZREM).
	OpLeaseDrop
	// OpLeaseRenew moves the end of a lease that still exists (ZADD XX, EXPIRE).
	OpLeaseRenew
	// OpLeaseCount drops the expired leases of a key and counts the rest (ZREMRANGEBYSCORE, ZCARD).
	OpLeaseCount
)

// LimitOp is one operation of a batch. Bucket is the hash of a budget key or ceiling key ([LimitBucket]), never the key
// itself.
type LimitOp struct {
	Kind LimitOpKind
	// Figure names the counter ("rb" requests in the burst window, "rh" requests per hour, …).
	Figure string
	Bucket string
	// Window is the window of a counter; Index the window's index (whole windows since the Unix epoch).
	Window time.Duration
	Index  int64
	// N is what OpWindowAdd and OpTake add and OpGiveBack takes back, at least 1.
	N int64
	// Member, Until and Now are the lease's name, its end and the time leases that ended before are dropped.
	Member string
	Until  time.Time
	Now    time.Time
	// Value and TTL are the marker's value and life.
	Value string
	TTL   time.Duration
	// Windows are the windows of an OpTake; Marker makes it read the block marker of Bucket and take nothing while it holds a
	// block that ends after Now.
	Windows []WindowTake
	Marker  bool
}

// LimitResult is the answer of one operation: Cur and Prev for a window, Count for a lease operation, Value for a marker
// (empty when none is set; for an OpTake the marker it read), Set for OpMarkerSet (whether this batch set it), Admitted and
// Windows for an OpTake.
type LimitResult struct {
	Cur, Prev int64
	Count     int64
	Value     string
	Set       bool
	// Admitted is whether the take added its units; Windows what it found in each of its windows, in the order of the op.
	Admitted bool
	Windows  []WindowState
}

// LimitBatch collects the operations of one round trip. The zero value is empty and ready to use.
type LimitBatch struct {
	Ops []LimitOp
}

// Add appends an operation and returns its index in the results.
func (b *LimitBatch) Add(op LimitOp) int {
	b.Ops = append(b.Ops, op)
	return len(b.Ops) - 1
}

// Len is the number of operations.
func (b *LimitBatch) Len() int { return len(b.Ops) }

// LimitBucket hashes a budget key or a ceiling key into the part of a key that names it (R-ARCH-030: names reach Valkey
// only as hashes).
func LimitBucket(key string) string {
	sum := sha256.Sum256([]byte("lim\x00" + key))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// WindowIndex is the index of the window of length w that holds t, and how far into it t is (0 to 1).
func WindowIndex(t time.Time, w time.Duration) (idx int64, elapsed float64) {
	ms := w.Milliseconds()
	if ms <= 0 {
		ms = 1
	}
	now := t.UnixMilli()
	return now / ms, float64(now%ms) / float64(ms)
}

// LimitStore runs batches: [LimitCounters] against Valkey, [MemoryLimits] in the replica's memory.
type LimitStore interface {
	Run(ctx context.Context, b *LimitBatch) ([]LimitResult, error)
}

// LimitCounters runs the batches of the download limits against Valkey.
type LimitCounters struct{ v *Valkey }

// NewLimitCounters returns the counters on v; nil when v is nil.
func NewLimitCounters(v *Valkey) *LimitCounters {
	if v == nil {
		return nil
	}
	return &LimitCounters{v: v}
}

// Available reports whether Valkey can be used now.
func (c *LimitCounters) Available() bool { return c != nil && c.v.Available() }

// CountDegraded counts a decision of the limits that fell back to the replica's share.
func (c *LimitCounters) CountDegraded() {
	if c != nil {
		c.v.CountDegraded(MechanismRateLimit)
	}
}

// Run sends the batch in one MULTI/EXEC. An empty batch costs nothing.
func (c *LimitCounters) Run(ctx context.Context, b *LimitBatch) ([]LimitResult, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if b == nil || len(b.Ops) == 0 {
		return nil, nil
	}
	return c.v.runLimits(ctx, b)
}

// limCommand is one queued command and the operation it belongs to.
type limCommand struct {
	op   int
	role uint8 // which reply of the operation the command gives (see readReplies)
}

// The roles of the replies.
const (
	roleIgnore uint8 = iota
	roleCur
	rolePrev
	roleCount
	roleValue
	roleSet
	roleTake
)

func (v *Valkey) limKey(op LimitOp, prev bool) (Key, error) {
	switch op.Kind {
	case OpMarkerGet, OpMarkerSet, OpMarkerDrop:
		ttl := op.TTL
		if ttl <= 0 {
			ttl = time.Second
		}
		return v.formKeyTTL(limFamily, min(ttl, limFamily.maxTTL), "blk", op.Bucket)
	case OpLeaseTake, OpLeaseDrop, OpLeaseRenew, OpLeaseCount:
		ttl := op.TTL
		if ttl <= 0 {
			ttl = time.Minute
		}
		return v.formKeyTTL(limFamily, min(ttl, limFamily.maxTTL), "par", op.Bucket)
	}
	idx := op.Index
	if prev {
		idx--
	}
	w := op.Window
	if w < time.Second {
		w = time.Second
	}
	return v.formKeyTTL(limFamily, min(2*w, limFamily.maxTTL), op.Figure, op.Bucket, strconv.FormatInt(idx, 10))
}

// runLimits builds the transaction of a batch and reads its replies. A take whose script the server does not hold (NOSCRIPT:
// a restarted server, or a replica that a failover promoted, holds none) did nothing inside the transaction; it is sent once
// more with the script's body, which also loads it (valkey_take.go).
func (v *Valkey) runLimits(ctx context.Context, b *LimitBatch) ([]LimitResult, error) {
	out := make([]LimitResult, len(b.Ops))
	var missing []int
	err := v.exec(ctx, "multi", func(ctx context.Context, c valkey.Client) error {
		cmds := make(valkey.Commands, 0, 2+4*len(b.Ops))
		roles := make([]limCommand, 0, 4*len(b.Ops))
		cmds = append(cmds, c.B().Multi().Build())
		queue := func(op int, role uint8, cmd valkey.Completed) {
			cmds = append(cmds, cmd)
			roles = append(roles, limCommand{op: op, role: role})
		}
		// A batch is as large as the flush makes it: one drop per download that ended in the interval, so a replica that serves
		// a thousand downloads a second sends a thousand drops in its one flush round trip (R-LIM-017).
		for i, op := range b.Ops {
			if err := v.queueLimitOp(c, i, op, queue); err != nil {
				return err
			}
		}
		cmds = append(cmds, c.B().Exec().Build())
		res := c.DoMulti(ctx, cmds...)
		last := len(res) - 1
		for _, r := range res[:last] {
			if err := r.Error(); err != nil {
				return err
			}
		}
		replies, err := res[last].ToArray()
		if err != nil {
			return err
		}
		if len(replies) != len(roles) {
			return fmt.Errorf("EXEC returned %d replies, want %d", len(replies), len(roles))
		}
		missing, err = readLimitReplies(replies, roles, out)
		return err
	})
	if err == nil && len(missing) > 0 {
		err = v.takeWithBody(ctx, b, missing, out)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// queueLimitOp queues the commands of one operation.
func (v *Valkey) queueLimitOp(c valkey.Client, i int, op LimitOp, queue func(op int, role uint8, cmd valkey.Completed)) error {
	if op.Kind == OpTake || op.Kind == OpGiveBack {
		keys, args, err := v.takeArgs(op)
		if err != nil {
			return err
		}
		queue(i, roleTake, c.B().Evalsha().Sha1(takeScriptSHA).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build())
		return nil
	}
	k, err := v.limKey(op, false)
	if err != nil {
		return err
	}
	switch op.Kind {
	case OpWindowAdd:
		p, err := v.limKey(op, true)
		if err != nil {
			return err
		}
		queue(i, roleCur, c.B().Incrby().Key(k.name).Increment(max(1, op.N)).Build())
		queue(i, roleIgnore, c.B().Expire().Key(k.name).Seconds(ceilSeconds(k.ttl)).Nx().Build())
		queue(i, rolePrev, c.B().Get().Key(p.name).Build())
	case OpWindowRead:
		p, err := v.limKey(op, true)
		if err != nil {
			return err
		}
		queue(i, roleCur, c.B().Get().Key(k.name).Build())
		queue(i, rolePrev, c.B().Get().Key(p.name).Build())
	case OpWindowZero:
		queue(i, roleIgnore, c.B().Arbitrary("SET").Keys(k.name).Args("0", "XX", "KEEPTTL").Build())
	case OpMarkerGet:
		queue(i, roleValue, c.B().Get().Key(k.name).Build())
	case OpMarkerSet:
		if err := checkSize(op.Value); err != nil {
			return err
		}
		queue(i, roleSet, c.B().Set().Key(k.name).Value(op.Value).Nx().ExSeconds(ceilSeconds(k.ttl)).Build())
	case OpMarkerDrop:
		queue(i, roleIgnore, c.B().Arbitrary("DELIFEQ").Keys(k.name).Args(op.Value).Build())
	case OpLeaseTake:
		queue(i, roleIgnore, c.B().Arbitrary("ZREMRANGEBYSCORE").Keys(k.name).Args("-inf", scoreOf(op.Now)).Build())
		queue(i, roleIgnore, c.B().Arbitrary("ZADD").Keys(k.name).Args(scoreOf(op.Until), op.Member).Build())
		queue(i, roleIgnore, c.B().Expire().Key(k.name).Seconds(ceilSeconds(k.ttl)).Build())
		queue(i, roleCount, c.B().Arbitrary("ZCARD").Keys(k.name).Build())
	case OpLeaseDrop:
		queue(i, roleIgnore, c.B().Arbitrary("ZREM").Keys(k.name).Args(op.Member).Build())
	case OpLeaseRenew:
		queue(i, roleIgnore, c.B().Arbitrary("ZADD").Keys(k.name).Args("XX", scoreOf(op.Until), op.Member).Build())
		queue(i, roleIgnore, c.B().Expire().Key(k.name).Seconds(ceilSeconds(k.ttl)).Build())
	case OpLeaseCount:
		queue(i, roleIgnore, c.B().Arbitrary("ZREMRANGEBYSCORE").Keys(k.name).Args("-inf", scoreOf(op.Now)).Build())
		queue(i, roleCount, c.B().Arbitrary("ZCARD").Keys(k.name).Build())
	default:
		return fmt.Errorf("cluster: unknown operation %d of the limits", op.Kind)
	}
	return nil
}

func scoreOf(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

// readLimitReplies reads the replies of EXEC into the results. It returns the takes the server answered NOSCRIPT, which did
// nothing.
func readLimitReplies(replies []valkey.ValkeyMessage, roles []limCommand, out []LimitResult) (missing []int, err error) {
	for n, r := range replies {
		role := roles[n]
		res := &out[role.op]
		switch role.role {
		case roleTake:
			if ve, ok := valkey.IsValkeyErr(r.Error()); ok && ve.IsNoScript() {
				missing = append(missing, role.op)
				continue
			}
			*res, err = readTake(r)
		case roleCur:
			res.Cur, err = optionalCounter(r)
		case rolePrev:
			res.Prev, err = optionalCounter(r)
		case roleCount:
			res.Count, err = r.AsInt64()
		case roleValue:
			if !r.IsNil() {
				res.Value, err = r.ToString()
			}
		case roleSet:
			res.Set = !r.IsNil()
		}
		if err != nil {
			return nil, err
		}
	}
	return missing, nil
}

// MemoryLimits runs the batches of the limits in the replica's memory: the fallback while Valkey cannot be used
// (R-LIM-019), and the counters of the unit tests. The semantics are those of [LimitCounters]: windows by index, markers
// with an expiry, leases in a set scored by their end. It is bounded: when it holds MaxFallbackBuckets keys, those that
// expired are dropped, then arbitrary ones.
type MemoryLimits struct {
	// Now is the clock of the expiry; nil is time.Now.
	Now func() time.Time

	mu      sync.Mutex
	windows map[string]*memCounter
	markers map[string]memMarker
	leases  map[string]map[string]int64
}

type memCounter struct {
	v       int64
	expires time.Time
}

type memMarker struct {
	v       string
	expires time.Time
}

// NewMemoryLimits returns an empty store.
func NewMemoryLimits() *MemoryLimits {
	return &MemoryLimits{windows: map[string]*memCounter{}, markers: map[string]memMarker{}, leases: map[string]map[string]int64{}}
}

func (m *MemoryLimits) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func memWindowKey(op LimitOp, idx int64) string {
	return op.Figure + ":" + op.Bucket + ":" + strconv.FormatInt(idx, 10)
}

// Run implements [LimitStore]; it never fails.
func (m *MemoryLimits) Run(_ context.Context, b *LimitBatch) ([]LimitResult, error) {
	if b == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.bound(now)
	out := make([]LimitResult, len(b.Ops))
	for i, op := range b.Ops {
		out[i] = m.run(op, now)
	}
	return out, nil
}

func (m *MemoryLimits) counter(key string, now time.Time) int64 {
	c, ok := m.windows[key]
	if !ok || now.After(c.expires) {
		return 0
	}
	return c.v
}

func (m *MemoryLimits) run(op LimitOp, now time.Time) LimitResult {
	var r LimitResult
	switch op.Kind {
	case OpTake:
		r = m.take(op, now)
	case OpGiveBack:
		m.giveBack(op, now)
		r.Admitted = true
	case OpWindowAdd:
		k := memWindowKey(op, op.Index)
		c, ok := m.windows[k]
		if !ok || now.After(c.expires) {
			c = &memCounter{expires: now.Add(2 * max(op.Window, time.Second))}
			m.windows[k] = c
		}
		c.v += max(1, op.N)
		r.Cur = c.v
		r.Prev = m.counter(memWindowKey(op, op.Index-1), now)
	case OpWindowRead:
		r.Cur = m.counter(memWindowKey(op, op.Index), now)
		r.Prev = m.counter(memWindowKey(op, op.Index-1), now)
	case OpWindowZero:
		if c, ok := m.windows[memWindowKey(op, op.Index)]; ok {
			c.v = 0
		}
	case OpMarkerGet:
		if mk, ok := m.markers[op.Bucket]; ok && !now.After(mk.expires) {
			r.Value = mk.v
		}
	case OpMarkerSet:
		if mk, ok := m.markers[op.Bucket]; !ok || now.After(mk.expires) {
			m.markers[op.Bucket] = memMarker{v: op.Value, expires: now.Add(op.TTL)}
			r.Set = true
		}
	case OpMarkerDrop:
		if mk, ok := m.markers[op.Bucket]; ok && mk.v == op.Value {
			delete(m.markers, op.Bucket)
		}
	case OpLeaseTake, OpLeaseCount, OpLeaseDrop, OpLeaseRenew:
		r.Count = m.lease(op)
	}
	return r
}

func (m *MemoryLimits) lease(op LimitOp) int64 {
	set := m.leases[op.Bucket]
	if set == nil {
		set = map[string]int64{}
		m.leases[op.Bucket] = set
	}
	switch op.Kind {
	case OpLeaseDrop:
		delete(set, op.Member)
		return int64(len(set))
	case OpLeaseRenew:
		if _, ok := set[op.Member]; ok {
			set[op.Member] = op.Until.UnixMilli()
		}
		return int64(len(set))
	}
	cut := op.Now.UnixMilli()
	for k, end := range set {
		if end <= cut {
			delete(set, k)
		}
	}
	if op.Kind == OpLeaseTake {
		set[op.Member] = op.Until.UnixMilli()
	}
	return int64(len(set))
}

// bound keeps the store within MaxFallbackBuckets keys. The caller holds m.mu.
func (m *MemoryLimits) bound(now time.Time) {
	if len(m.windows) < MaxFallbackBuckets && len(m.markers) < MaxFallbackBuckets && len(m.leases) < MaxFallbackBuckets {
		return
	}
	for k, c := range m.windows {
		if now.After(c.expires) {
			delete(m.windows, k)
		}
	}
	for k, mk := range m.markers {
		if now.After(mk.expires) {
			delete(m.markers, k)
		}
	}
	for k, s := range m.leases {
		if len(s) == 0 {
			delete(m.leases, k)
		}
	}
	for k := range m.windows {
		if len(m.windows) < MaxFallbackBuckets {
			break
		}
		delete(m.windows, k)
	}
}

// Estimate is the sliding-window estimate of R-ARCH-034 for the counters of a window: previous × (1 − elapsed) + current.
func Estimate(cur, prev int64, elapsed float64) float64 {
	return float64(prev)*(1-elapsed) + float64(cur)
}

// RoomAfter is the time from the moment of a check (elapsed into a window of length w, counters cur and prev) after which
// the estimate falls to or below limit-units, if nothing more is charged: the wait of a refusal (R-LIM-020). It solves the
// rule for the rest of the current window and, when that does not reach it, for the next window, whose previous counter is
// cur. It is at most two windows.
func RoomAfter(cur, prev int64, elapsed float64, w time.Duration, limit, units int64) time.Duration {
	lim, u, wf := float64(limit), float64(units), float64(w)
	if need := lim - float64(cur) - u; need >= 0 {
		if prev == 0 {
			return 0
		}
		if x := 1 - need/float64(prev); x < 1 {
			return time.Duration((math.Max(x, elapsed) - elapsed) * wf)
		}
	}
	y := 0.0
	if c := float64(cur); c > 0 {
		y = math.Max(0, 1-(lim-u)/c)
	}
	return time.Duration(((1 - elapsed) + math.Min(y, 1)) * wf)
}
