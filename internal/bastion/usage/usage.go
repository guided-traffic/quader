// Package usage is a Bastion replica's buffer of the usage it counted at its place (R-LIM-045, Q-LIM-308): per subject key,
// credential, repository and rule in buckets of five minutes, the requests, refused requests, cached and uncached bytes,
// upstream requests and the limit events per figure. The engine adds what each request costs; the download reporter drains the
// buffer with every flush into the batch's usage member and hands back what it could not send.
//
// The buffer is bounded as the main installation's own is (R-LIM-028): at most iface.MaxUsageRows rows between two drains, and
// what a further key would have made is folded into an overflow row per bucket, repository and subject kind, which counts the
// subjects it folded. A request whose repository the Bastion does not know (the registry root's /v2/ and token routes) counts in
// the budgets but makes no row: the main installation stores a Bastion's usage per repository.
package usage

import (
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// BucketSeconds is the length of a usage bucket (R-LIM-027): five minutes.
const BucketSeconds = 300

// maxFoldedSubjects bounds the subject keys the overflow rows remember to count each subject once; past it every fold of a new
// key counts as a subject, so the count is high rather than the memory unbounded.
const maxFoldedSubjects = 100_000

// Delta is what one request, one refusal or one run of bytes adds to its subject's row.
type Delta struct {
	At time.Time
	// Subject is the subject key (user:<id>, technical_token:<id>, ci:<issuer>:<project>, anonymous:<prefix>), Credential the
	// presented token's ID (empty for an anonymous caller), Repository the qualified name, Rule the rule the subject is charged by.
	Subject, Credential, Repository, Rule string

	Requests, Refused, Cached, Uncached, Upstream int64
	// Limited and WouldLimit name a figure (iface.FigureNames) that refused the request, or in Audit would have.
	Limited, WouldLimit string
}

type rowKey struct {
	bucket                                int64
	subject, credential, repository, rule string
}

type overKey struct {
	bucket           int64
	kind, repository string
}

// Buffer is the usage of a replica since its last drain. It is safe for concurrent use.
type Buffer struct {
	max int

	mu     sync.Mutex
	rows   map[rowKey]*iface.UsageRow
	over   map[overKey]*iface.UsageOverflow
	folded map[overKey]map[string]struct{}
	nfold  int
}

// New returns an empty buffer of at most maxRows rows between two drains; 0 means iface.MaxUsageRows.
func New(maxRows int) *Buffer {
	if maxRows <= 0 || maxRows > iface.MaxUsageRows {
		maxRows = iface.MaxUsageRows
	}
	b := &Buffer{max: maxRows}
	b.reset()
	return b
}

func (b *Buffer) reset() {
	b.rows = map[rowKey]*iface.UsageRow{}
	b.over = map[overKey]*iface.UsageOverflow{}
	b.folded = map[overKey]map[string]struct{}{}
	b.nfold = 0
}

func bucketOf(t time.Time) int64 { return t.Unix() / BucketSeconds * BucketSeconds }

// kindOf is the subject kind of a key, the part before its first colon.
func kindOf(subject string) string {
	k, _, _ := strings.Cut(subject, ":")
	return k
}

// Add adds a delta to its subject's row, or to the overflow row of its kind once the buffer holds its rows.
func (b *Buffer) Add(d Delta) {
	if d.Subject == "" || d.Repository == "" {
		return
	}
	k := rowKey{bucket: bucketOf(d.At), subject: d.Subject, credential: d.Credential, repository: d.Repository, rule: d.Rule}
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.rows[k]
	if r == nil {
		if len(b.rows) >= b.max {
			b.fold(k, d)
			return
		}
		r = &iface.UsageRow{
			BucketStart: time.Unix(k.bucket, 0).UTC(), Subject: d.Subject, Credential: d.Credential, Repository: d.Repository,
			Rule: d.Rule, Limited: map[string]int64{}, WouldLimit: map[string]int64{},
		}
		b.rows[k] = r
	}
	r.Requests += d.Requests
	r.Refused += d.Refused
	r.CachedBytes += d.Cached
	r.UncachedBytes += d.Uncached
	r.UpstreamRequests += d.Upstream
	if d.Limited != "" {
		r.Limited[d.Limited]++
	}
	if d.WouldLimit != "" {
		r.WouldLimit[d.WouldLimit]++
	}
}

// fold adds a delta of a key the buffer has no room for to the overflow row of its bucket, repository and subject kind (Q-LIM-308);
// b.mu is held.
func (b *Buffer) fold(k rowKey, d Delta) {
	ok := overKey{bucket: k.bucket, kind: kindOf(d.Subject), repository: d.Repository}
	o := b.over[ok]
	if o == nil {
		o = &iface.UsageOverflow{BucketStart: time.Unix(k.bucket, 0).UTC(), SubjectKind: ok.kind, Repository: d.Repository}
		b.over[ok] = o
		b.folded[ok] = map[string]struct{}{}
	}
	seen := b.folded[ok]
	if _, known := seen[d.Subject]; !known {
		o.Subjects++
		if b.nfold < maxFoldedSubjects {
			seen[d.Subject] = struct{}{}
			b.nfold++
		}
	}
	o.Requests += d.Requests
	o.Refused += d.Refused
	o.CachedBytes += d.Cached
	o.UncachedBytes += d.Uncached
	o.UpstreamRequests += d.Upstream
}

// Drain returns what was counted since the last drain and empties the buffer.
func (b *Buffer) Drain() iface.UsageReport {
	b.mu.Lock()
	rows, over := b.rows, b.over
	b.reset()
	b.mu.Unlock()
	rep := iface.UsageReport{BucketSeconds: BucketSeconds, Rows: make([]iface.UsageRow, 0, len(rows)), Overflow: make([]iface.UsageOverflow, 0, len(over))}
	for _, r := range rows {
		rep.Rows = append(rep.Rows, *r)
	}
	for _, o := range over {
		rep.Overflow = append(rep.Overflow, *o)
	}
	return rep
}

// Restore adds back a report that could not be sent, so the next drain carries it with what was counted meanwhile. Its rows go
// through the bound again; its overflow rows are added to the overflow.
func (b *Buffer) Restore(rep iface.UsageReport) {
	for _, r := range rep.Rows {
		b.Add(Delta{
			At: r.BucketStart, Subject: r.Subject, Credential: r.Credential, Repository: r.Repository, Rule: r.Rule,
			Requests: r.Requests, Refused: r.Refused, Cached: r.CachedBytes, Uncached: r.UncachedBytes, Upstream: r.UpstreamRequests,
		})
		b.addEvents(r)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, o := range rep.Overflow {
		k := overKey{bucket: bucketOf(o.BucketStart), kind: o.SubjectKind, repository: o.Repository}
		cur := b.over[k]
		if cur == nil {
			c := o
			b.over[k] = &c
			b.folded[k] = map[string]struct{}{}
			continue
		}
		cur.Subjects += o.Subjects
		cur.Requests += o.Requests
		cur.Refused += o.Refused
		cur.CachedBytes += o.CachedBytes
		cur.UncachedBytes += o.UncachedBytes
		cur.UpstreamRequests += o.UpstreamRequests
	}
}

// addEvents adds the limit events of a restored row to its row; a row that was folded loses them, as the overflow counts none.
func (b *Buffer) addEvents(r iface.UsageRow) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := rowKey{bucket: bucketOf(r.BucketStart), subject: r.Subject, credential: r.Credential, repository: r.Repository, rule: r.Rule}
	cur := b.rows[k]
	if cur == nil {
		return
	}
	for f, n := range r.Limited {
		cur.Limited[f] += n
	}
	for f, n := range r.WouldLimit {
		cur.WouldLimit[f] += n
	}
}

// Len is the number of rows held, for the tests.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.rows)
}
