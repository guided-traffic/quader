package limits

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
)

// The replica's charge buffer (R-LIM-017): every limits.flush_interval one round trip carries the bytes and refused
// requests of the interval as INCRBY, the end of the downloads that finished (ZREM), the renewal of the leases of downloads
// older than 20 s, the counts of the leases of the keys being paced (ZCARD) and the block markers the last flush found due.
// The consumption goes on to the usage store's buffer. No request waits for it.

// Start runs the flush loop until Stop or ctx ends. It returns at once.
func (e *Engine) Start(ctx context.Context) {
	go func() {
		defer close(e.done)
		t := time.NewTicker(e.o.FlushInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stop:
				return
			case <-t.C:
				e.Flush(context.WithoutCancel(ctx))
			}
		}
	}()
}

// Stop ends the loop and flushes what is left (R-ARCH-024: a drain loses no charge).
func (e *Engine) Stop(ctx context.Context) {
	e.once.Do(func() { close(e.stop) })
	select {
	case <-e.done:
	case <-time.After(time.Second):
	}
	for _, c := range e.snapshot() {
		c.finished.Store(true)
	}
	e.Flush(ctx)
}

func (e *Engine) snapshot() []*Charge {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*Charge, 0, len(e.active))
	for c := range e.active {
		out = append(out, c)
	}
	return out
}

// keyCharge sums the deltas of one budget key in a flush.
type keyCharge struct {
	key      string
	figures  Figures
	cached   int64
	uncached int64
	upstream int64
	refused  int64
	subject  Subject
	place    Place
	mode     Mode
}

// Flush runs one interval of the buffer.
func (e *Engine) Flush(ctx context.Context) {
	now := e.now()
	charges := e.snapshot()
	keys := map[string]*keyCharge{}
	ceilings := map[string]int64{}
	var drops, renews []lease
	paced := map[string]bool{}
	for _, c := range charges {
		// The request itself, its count and the limits it met, is recorded once it has ended: until then the subject (a token
		// request's, R-LIM-003) and the repository that serves it (a virtual repository's member, R-LIM-027) may still change. Its
		// bytes, upstream requests and refusal go on at every flush, under what is known then.
		ended := c.finished.Load()
		finished := ended && c.holds.Load() <= 0
		cached, uncached := c.pendCached.Swap(0), c.pendUncached.Swap(0)
		upstream, refused := c.pendUpstream.Swap(0), c.pendRefused.Swap(0)
		bg := c.pendBg.Swap(0)
		var requests int64
		if ended {
			requests = c.pendRequests.Swap(0)
		}
		c.mu.Lock()
		subj, res, key, mode, ceil := c.Subject, c.Resolution, c.Key, c.Mode, c.ceiling
		var events []event
		if ended {
			events = c.events
			c.events = nil
		}
		l := c.lease
		if finished && l != nil {
			c.lease = nil
		}
		c.mu.Unlock()
		if cached|uncached|upstream|refused != 0 && subj.Kind != KindSystem {
			k := keys[key]
			if k == nil {
				k = &keyCharge{key: key, figures: res.Figures, subject: subj, place: res.Place, mode: mode}
				keys[key] = k
			}
			k.cached += cached
			k.uncached += uncached
			k.upstream += upstream
			k.refused += refused
			if ceil != "" && uncached > 0 {
				ceilings[ceil] += uncached
			}
		}
		e.m.bytes(subj.Kind, cached, uncached)
		if e.o.Usage != nil && (requests|refused|cached|uncached|upstream|bg != 0 || len(events) > 0) {
			served := c.served.Load()
			d := UsageDelta{
				At: now, Subject: subj, Place: res.Place, RepoID: served, ViaRepoID: c.viaOf(served), Requests: requests, Refused: refused,
				Cached: cached, Uncached: uncached, Upstream: upstream, Background: bg, Limits: res.Figures,
			}
			if subj.Kind != KindSystem {
				// The upstream request step 9a counted is the request's first, also the one a background download's decision
				// counted for the probe (R-LIM-037); the store counts every exchange.
				d.Upstream += upstreamOf(c)
			}
			for _, ev := range events {
				d.Events = append(d.Events, EventDelta{Figure: ev.figure, Mode: ev.mode, Count: 1})
			}
			e.o.Usage.Add(d)
		}
		switch {
		case l != nil && finished:
			drops = append(drops, *l)
		case l != nil && now.Sub(l.renewed) >= renewLeasesAfter:
			c.mu.Lock()
			if c.lease != nil {
				c.lease.renewed = now
			}
			c.mu.Unlock()
			renews = append(renews, *l)
		}
		if l != nil && c.download.Load() && !finished {
			paced[key] = true
		}
		if finished {
			e.mu.Lock()
			delete(e.active, c)
			e.mu.Unlock()
		}
	}
	e.flushCounters(ctx, now, keys, ceilings, drops, renews, paced)
	// The round has ended: what was counted before it began is where its round put it (Q-LIM-112). A test that waits for the bytes
	// of a request reads the counter after the request and waits for two more rounds, since the first may have begun before.
	e.m.flushed()
}

// upstreamOf takes the upstream requests step 9a counted for the charge since the last flush.
func upstreamOf(c *Charge) int64 {
	return c.upstreamCounted.Swap(0)
}

func (e *Engine) flushCounters(ctx context.Context, now time.Time, keys map[string]*keyCharge, ceilings map[string]int64,
	drops, renews []lease, paced map[string]bool,
) {
	var b cluster.LimitBatch
	hIdx, hEl := cluster.WindowIndex(now, hour)
	dIdx, _ := cluster.WindowIndex(now, day)
	type rfCheck struct {
		k  *keyCharge
		op int
	}
	var rfs []rfCheck
	add := func(fig, bucket string, w time.Duration, idx, n int64) int {
		return b.Add(cluster.LimitOp{Kind: cluster.OpWindowAdd, Figure: fig, Bucket: bucket, Window: w, Index: idx, N: n})
	}
	for _, k := range keys {
		bucket := cluster.LimitBucket(k.key)
		if k.cached > 0 {
			add(ctrCachedHour, bucket, hour, hIdx, k.cached)
		}
		if k.uncached > 0 {
			add(ctrUncachedHour, bucket, hour, hIdx, k.uncached)
		}
		if k.cached+k.uncached > 0 {
			add(ctrBytesDay, bucket, day, dIdx, k.cached+k.uncached)
		}
		if k.upstream > 0 {
			add(ctrUpstreamHour, bucket, hour, hIdx, k.upstream)
		}
		if k.refused > 0 {
			rfs = append(rfs, rfCheck{k: k, op: add(ctrRefusedHour, bucket, hour, hIdx, k.refused)})
		}
	}
	for key, n := range ceilings {
		add(ctrUncachedHour, cluster.LimitBucket(key), hour, hIdx, n)
	}
	// A lease is given up and renewed in both places it may be held (R-LIM-019, R-LIM-024, engine.go dropLease): on the
	// replica's share here, in Valkey by the batch, with the drops earlier flushes could not send.
	e.dropLocal(drops)
	e.renewLocal(renews, now)
	unsent := e.takeUnsent(now)
	for _, d := range unsent {
		b.Add(cluster.LimitOp{Kind: cluster.OpLeaseDrop, Bucket: d.bucket, Member: d.member, TTL: 2 * e.o.LeaseTTL})
	}
	for _, l := range drops {
		b.Add(e.dropOp(l))
	}
	for _, l := range renews {
		b.Add(cluster.LimitOp{
			Kind: cluster.OpLeaseRenew, Bucket: cluster.LimitBucket(l.key), Member: l.member,
			Until: now.Add(e.o.LeaseTTL), TTL: 2 * e.o.LeaseTTL,
		})
	}
	pacedOps := map[string]int{}
	for key := range paced {
		pacedOps[key] = b.Add(cluster.LimitOp{Kind: cluster.OpLeaseCount, Bucket: cluster.LimitBucket(key), Now: now, TTL: 2 * e.o.LeaseTTL})
	}
	e.mu.Lock()
	due := e.due
	e.due = map[string]*dueBlock{}
	e.mu.Unlock()
	markerOps := map[int]*dueBlock{}
	for _, d := range due {
		ttl := max(time.Second, d.block.Until.Sub(now))
		op := b.Add(cluster.LimitOp{Kind: cluster.OpMarkerSet, Bucket: cluster.LimitBucket(d.key), Value: d.block.MarkerValue(), TTL: ttl})
		markerOps[op] = d
	}
	res, local, err := e.run(ctx, &b)
	if err != nil || local {
		e.keepUnsent(drops)
	} else {
		e.sentUnsent(unsent)
	}
	if err != nil {
		e.o.Log.Warn("the charges of the download limits were not counted", "error", err)
		return
	}
	for key, op := range pacedOps {
		e.clusterLeases(key, res[op].Count)
	}
	for op, d := range markerOps {
		if res[op].Set {
			e.m.blockStarted(d.block.Subject.Kind)
			if e.o.Blocks != nil {
				e.o.Blocks.Started(context.WithoutCancel(ctx), d.block)
			}
		}
	}
	blockSeconds := e.index().Settings().BlockSeconds
	if blockSeconds <= 0 {
		blockSeconds = 900
	}
	for _, rc := range rfs {
		lim := e.scaled(rc.k.figures[RefusedPerHour], local)
		if lim <= 0 {
			continue
		}
		r := res[rc.op]
		est := cluster.Estimate(r.Cur, r.Prev, hEl)
		if est <= float64(lim) {
			continue
		}
		if rc.k.mode != ModeEnforce {
			e.wouldBlock(rc.k, hIdx)
			continue
		}
		blk := Block{
			ID: NewBlockID(), Subject: rc.k.subject, Place: rc.k.place, Started: now, Until: now.Add(time.Duration(blockSeconds) * time.Second),
			Refused: int64(est), Limit: rc.k.figures[RefusedPerHour],
		}
		e.mu.Lock()
		e.due[rc.k.key] = &dueBlock{block: blk, key: rc.k.key}
		e.mu.Unlock()
		e.m.decision(RefusedPerHour.String(), ModeEnforce, "limited")
	}
}

// wouldBlock records, in audit, the block a budget key would have got: once per key and window (R-LIM-025).
func (e *Engine) wouldBlock(k *keyCharge, window int64) {
	e.mu.Lock()
	seen := e.wouldRefused[k.key] == window
	e.wouldRefused[k.key] = window
	if len(e.wouldRefused) > cluster.MaxFallbackBuckets {
		for key, w := range e.wouldRefused {
			if w < window {
				delete(e.wouldRefused, key)
			}
		}
	}
	e.mu.Unlock()
	if seen {
		return
	}
	e.m.decision(RefusedPerHour.String(), ModeAudit, "would_limit")
	if e.o.Usage != nil {
		e.o.Usage.Add(UsageDelta{
			At: e.now(), Subject: k.subject, Place: k.place, Limits: k.figures,
			Events: []EventDelta{{Figure: RefusedPerHour.String(), Mode: ModeAudit, Count: 1}},
		})
	}
}

// LiftBlock removes a block's marker and sets the budget key's refused counters of the current and the previous window to 0,
// so that the next refused request does not block again at once (R-LIM-025).
func (e *Engine) LiftBlock(ctx context.Context, b Block) error {
	key := BudgetKey(b.Subject, b.Place)
	bucket := cluster.LimitBucket(key)
	now := e.now()
	idx, _ := cluster.WindowIndex(now, hour)
	var batch cluster.LimitBatch
	batch.Add(cluster.LimitOp{Kind: cluster.OpMarkerDrop, Bucket: bucket, Value: b.MarkerValue()})
	batch.Add(cluster.LimitOp{Kind: cluster.OpWindowZero, Figure: ctrRefusedHour, Bucket: bucket, Window: hour, Index: idx})
	batch.Add(cluster.LimitOp{Kind: cluster.OpWindowZero, Figure: ctrRefusedHour, Bucket: bucket, Window: hour, Index: idx - 1})
	e.mu.Lock()
	delete(e.due, key)
	e.mu.Unlock()
	_, _, err := e.run(ctx, &batch)
	return err
}

// Live is the live estimate of a budget key's windowed figures, for the detail of the usage API (R-LIM-034): one pipeline of
// reads. used is per figure; parallel is the count of leases.
func (e *Engine) Live(ctx context.Context, key string) (used Figures, err error) {
	now := e.now()
	bucket := cluster.LimitBucket(key)
	var b cluster.LimitBatch
	ops := map[Figure]int{}
	read := func(fig Figure, ctr string, w time.Duration) {
		idx, _ := cluster.WindowIndex(now, w)
		ops[fig] = b.Add(cluster.LimitOp{Kind: cluster.OpWindowRead, Figure: ctr, Bucket: bucket, Window: w, Index: idx})
	}
	read(RequestsPerHour, ctrRequestsHour, hour)
	read(CachedBytesPerHour, ctrCachedHour, hour)
	read(UncachedBytesPerHour, ctrUncachedHour, hour)
	read(UpstreamRequestsPerHour, ctrUpstreamHour, hour)
	read(BytesPerDay, ctrBytesDay, day)
	read(RefusedPerHour, ctrRefusedHour, hour)
	ops[ParallelTransfers] = b.Add(cluster.LimitOp{Kind: cluster.OpLeaseCount, Bucket: bucket, Now: now, TTL: 2 * e.o.LeaseTTL})
	res, _, err := e.run(ctx, &b)
	if err != nil {
		return used, err
	}
	for fig, op := range ops {
		if fig == ParallelTransfers {
			used[fig] = res[op].Count
			continue
		}
		w := hour
		if fig == BytesPerDay {
			w = day
		}
		_, el := cluster.WindowIndex(now, w)
		used[fig] = int64(cluster.Estimate(res[op].Cur, res[op].Prev, el))
	}
	return used, nil
}
