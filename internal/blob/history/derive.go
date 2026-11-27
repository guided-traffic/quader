package history

import (
	"sort"
	"time"
)

// The problems and the cells of a period, derived at read time from the records (storage.md R-STOR-111, Q-STOR-525): pure functions
// over what ReadProblems and ReadCells read, so that the overview, the list and the page's strips cannot disagree.

// The kinds of a problem (R-STOR-111). Fall-overs are none: they are the symptoms the outages and the damage already name, counted in
// the cells and in the section Availability (Q-STOR-526).
const (
	ProblemUnreachable      = "unreachable"
	ProblemStartCheckFailed = "start_check_failed"
	ProblemCopiesDamaged    = "copies_damaged"
	ProblemUploadsRefused   = "uploads_refused"
	ProblemReadsUnavailable = "reads_unavailable"
)

// The tones of a problem and of a cell (B.14, Q-STOR-528): an error when clients were refused (an upload refused, a read no copy served,
// a blob no other copy held), a warning when Quader absorbed it, none when nothing happened.
const (
	ToneNone    = "none"
	ToneWarning = "warning"
	ToneError   = "error"
)

// Recent is how long after its end a problem still makes the overview's problems ongoing (Q-STOR-529).
const Recent = time.Hour

// Problem is one problem of a period. Ended is zero while it lasts. The figures that do not apply to its kind are zero.
type Problem struct {
	Kind, Connection string
	Started, Ended   time.Time
	Seconds          int64
	Tone             string
	// Replicas and Occurrences are an interval's: the replicas that recorded it and how often the connection fell away.
	Replicas    int
	Occurrences int64
	// Count and Bytes are the copies damaged, the uploads refused or the reads unavailable, and their bytes.
	Count, Bytes int64
	// Resolved, Open, Acknowledged and Unavailable are the findings' (copies_damaged): how many are resolved, open, acknowledged, and how
	// many had no other present copy when found; UnavailableOpen of those are not resolved.
	Resolved, Open, Acknowledged, Unavailable, UnavailableOpen int64
	// Codes are the failure codes of an interval or the reasons of the counts and the findings; Sources the findings' sources; Steps
	// the steps of a failing start check.
	Codes, Sources, Steps []string
}

// Lasting reports whether the problem has not ended.
func (p Problem) Lasting() bool { return p.Ended.IsZero() }

// rank orders tones: an error before a warning before none.
func rank(tone string) int {
	switch tone {
	case ToneError:
		return 2
	case ToneWarning:
		return 1
	}
	return 0
}

// Worse reports whether a is the worse problem of two (H-2433): the worse tone, then the longer, then the more bytes, then the newer.
func Worse(a, b Problem) bool {
	if ra, rb := rank(a.Tone), rank(b.Tone); ra != rb {
		return ra > rb
	}
	if a.Seconds != b.Seconds {
		return a.Seconds > b.Seconds
	}
	if a.Bytes != b.Bytes {
		return a.Bytes > b.Bytes
	}
	return a.Started.After(b.Started)
}

// Newer is the list's order: the latest start first, then by kind and connection.
func Newer(a, b Problem) bool {
	if !a.Started.Equal(b.Started) {
		return a.Started.After(b.Started)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Connection < b.Connection
}

// seconds is the length from start to end, to now while it lasts.
func seconds(start, end, now time.Time) int64 {
	if end.IsZero() {
		end = now
	}
	if d := end.Sub(start); d > 0 {
		return int64(d / time.Second)
	}
	return 0
}

// DeriveProblems derives the problems of a period from its records at now, newest first: the intervals of one connection and kind
// merged across replicas while they overlap; one connection's findings of one UTC day, with the findings beyond the cap counted in;
// one connection's refused uploads and reads no copy served of one UTC day.
func DeriveProblems(r ProblemRecords, now time.Time) []Problem {
	out := mergeIntervals(r.Intervals, now)
	out = append(out, dayFindings(r.Findings, r.Counts, now)...)
	out = append(out, dayCounts(r.Counts, now)...)
	sort.SliceStable(out, func(i, j int) bool { return Newer(out[i], out[j]) })
	return out
}

// mergeIntervals merges the intervals of one connection and kind that overlap, whichever replica recorded them, into one problem each.
func mergeIntervals(in []Interval, now time.Time) []Problem {
	type key struct{ conn, kind string }
	groups := map[key][]Interval{}
	var keys []key
	for _, iv := range in {
		k := key{iv.Connection, iv.Kind}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], iv)
	}
	var out []Problem
	for _, k := range keys {
		ivs := groups[k]
		sort.SliceStable(ivs, func(i, j int) bool { return ivs[i].Started.Before(ivs[j].Started) })
		var cur *problemBuilder
		for _, iv := range ivs {
			end := iv.Ended
			if end.IsZero() {
				end = now
			}
			if cur != nil && !iv.Started.After(cur.end) {
				cur.add(iv, end)
				continue
			}
			if cur != nil {
				out = append(out, cur.problem(now))
			}
			cur = newBuilder(k.conn, k.kind, iv, end)
		}
		if cur != nil {
			out = append(out, cur.problem(now))
		}
	}
	return out
}

// problemBuilder gathers the intervals of one merged problem.
type problemBuilder struct {
	p        Problem
	end      time.Time
	lasting  bool
	replicas map[string]bool
	codes    map[string]bool
	steps    map[string]bool
}

func newBuilder(conn, kind string, iv Interval, end time.Time) *problemBuilder {
	b := &problemBuilder{
		p: Problem{Kind: kind, Connection: conn, Started: iv.Started, Tone: ToneWarning}, end: end,
		replicas: map[string]bool{}, codes: map[string]bool{}, steps: map[string]bool{},
	}
	b.add(iv, end)
	return b
}

func (b *problemBuilder) add(iv Interval, end time.Time) {
	if end.After(b.end) {
		b.end = end
	}
	b.lasting = b.lasting || iv.Ended.IsZero()
	b.replicas[iv.Replica] = true
	b.p.Occurrences += iv.Occurrences
	if iv.Code != "" {
		b.codes[iv.Code] = true
	}
	if iv.Step != "" {
		b.steps[iv.Step] = true
	}
}

func (b *problemBuilder) problem(now time.Time) Problem {
	p := b.p
	if !b.lasting {
		p.Ended = b.end
	}
	p.Seconds = seconds(p.Started, p.Ended, now)
	p.Replicas = len(b.replicas)
	p.Codes, p.Steps, p.Sources = sortedKeys(b.codes), sortedKeys(b.steps), []string{}
	kind := ProblemUnreachable
	if p.Kind == KindStartCheckFailed {
		kind = ProblemStartCheckFailed
	}
	p.Kind = kind
	return p
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dayFindings is one problem per connection and UTC day of findings: lasting while one of them is unresolved, an error when one had no
// other present copy. The findings beyond the day's cap (findings_unlisted) count in, as copies whose state is unknown here.
func dayFindings(in []DayFindings, counts []DayCounts, now time.Time) []Problem {
	type key struct {
		conn string
		day  int64
	}
	unlisted := map[key]DayCounts{}
	for _, c := range counts {
		if c.Kind == "findings_unlisted" {
			unlisted[key{c.Connection, c.Day.Unix()}] = c
		}
	}
	out := make([]Problem, 0, len(in))
	seen := map[key]bool{}
	for _, f := range in {
		k := key{f.Connection, f.Day.Unix()}
		seen[k] = true
		p := Problem{
			Kind: ProblemCopiesDamaged, Connection: f.Connection, Started: f.First, Count: f.N, Bytes: f.Bytes, Resolved: f.Resolved,
			Open: f.Open, Acknowledged: f.Acknowledged, Unavailable: f.Unavailable, UnavailableOpen: f.UnavailableOpen,
			Codes: nonNil(f.Reasons), Sources: nonNil(f.Sources), Steps: []string{}, Tone: ToneWarning,
		}
		if f.Open+f.Acknowledged == 0 {
			p.Ended = f.LastResolved
		}
		if u, ok := unlisted[k]; ok {
			p.Count += u.N
			p.Bytes += u.Bytes
		}
		if p.Unavailable > 0 {
			p.Tone = ToneError
		}
		p.Seconds = seconds(p.Started, p.Ended, now)
		out = append(out, p)
	}
	for k, u := range unlisted {
		if seen[k] {
			continue
		}
		// Only findings beyond a cap on a day whose listed ones have left the period: the counts alone stand for the day.
		end := u.LastHour.Add(time.Hour)
		p := Problem{
			Kind: ProblemCopiesDamaged, Connection: u.Connection, Started: u.FirstHour, Ended: end, Count: u.N, Bytes: u.Bytes,
			Codes: nonNil(u.Reasons), Sources: []string{}, Steps: []string{}, Tone: ToneWarning,
		}
		p.Seconds = seconds(p.Started, p.Ended, now)
		out = append(out, p)
	}
	return out
}

// dayCounts is one problem per connection, kind and UTC day of refused uploads and reads no copy served: from the first hour that
// counted one to the end of the last, lasting while the last is the current hour. Clients were refused, so each is an error.
func dayCounts(in []DayCounts, now time.Time) []Problem {
	var out []Problem
	for _, c := range in {
		kind := ""
		switch c.Kind {
		case "upload_refused":
			kind = ProblemUploadsRefused
		case "read_unavailable":
			kind = ProblemReadsUnavailable
		default:
			continue
		}
		p := Problem{
			Kind: kind, Connection: c.Connection, Started: c.FirstHour, Count: c.N, Bytes: c.Bytes, Codes: nonNil(c.Reasons),
			Sources: []string{}, Steps: []string{}, Tone: ToneError,
		}
		if end := c.LastHour.Add(time.Hour); end.Before(now) || end.Equal(now) {
			p.Ended = end
		}
		p.Seconds = seconds(p.Started, p.Ended, now)
		out = append(out, p)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Cell is one cell of a connection's strip (R-STOR-111): what happened between Start and the next cell's start.
type Cell struct {
	Start time.Time
	Tone  string
	// UnreachableSeconds is how long the connection was unreachable on at least one replica, Replicas on how many replicas;
	// StartChecksFailed the failing start checks' intervals.
	UnreachableSeconds int64
	Replicas           int
	StartChecksFailed  int
	// DamagedCopies and DamagedBytes are the copies found damaged (those beyond the day's cap included); UnavailableBlobs the blobs no
	// other copy held then.
	DamagedCopies, DamagedBytes, UnavailableBlobs int64
	// The counts of what the reads and writes absorbed (R-STOR-110); ReadsRefused the refusals of the stored format, UnknownKeyReads of
	// them those whose key is unknown, which mark no copy.
	ReadFallovers, ReadsUnavailable, WriteFallovers, UploadsRefused, CopiesFailed, ReadsRefused, UnknownKeyReads int64
}

// tone is the cell's tone by Q-STOR-528.
func (c Cell) tone() string {
	switch {
	case c.ReadsUnavailable > 0 || c.UploadsRefused > 0 || c.UnavailableBlobs > 0:
		return ToneError
	case c.UnreachableSeconds > 0 || c.StartChecksFailed > 0 || c.DamagedCopies > 0 || c.ReadFallovers > 0 || c.WriteFallovers > 0 ||
		c.CopiesFailed > 0 || c.ReadsRefused > 0:
		return ToneWarning
	}
	return ToneNone
}

// FindingsTotal is one connection's findings of the period: how many and their bytes, by outcome and by source (R-STOR-111).
type FindingsTotal struct {
	N, Bytes  int64
	ByOutcome map[string]int64
	BySource  map[string]int64
	// Unlisted is the findings beyond the day's cap, counted and not listed (H-2428).
	Unlisted int64
}

// ConnectionCells is one connection's strip of a period and its findings' totals.
type ConnectionCells struct {
	Connection string
	Cells      []Cell
	Findings   FindingsTotal
}

// DeriveCells derives every connection's strip of period p ending at to (Q-STOR-527): the connections of ids in their order, then
// every other one the records name, by id, so that a forgotten connection stays in the history (Q-STOR-532).
func DeriveCells(p Period, to time.Time, r CellRecords, ids []string) []ConnectionCells {
	from := to.Add(-p.Span())
	index := map[string]int{}
	var out []ConnectionCells
	add := func(id string) int {
		if i, ok := index[id]; ok {
			return i
		}
		cells := make([]Cell, p.Cells)
		for i := range cells {
			cells[i].Start = from.Add(time.Duration(i) * p.Cell)
		}
		index[id] = len(out)
		out = append(out, ConnectionCells{Connection: id, Cells: cells, Findings: FindingsTotal{ByOutcome: map[string]int64{}, BySource: map[string]int64{}}})
		return index[id]
	}
	for _, id := range ids {
		add(id)
	}
	var extra []string
	note := func(id string) {
		if _, ok := index[id]; !ok {
			extra = append(extra, id)
			add(id)
		}
	}
	for _, iv := range r.Intervals {
		note(iv.Connection)
	}
	for _, c := range r.Counts {
		note(c.Connection)
	}
	for _, f := range r.Findings {
		note(f.Connection)
	}
	for _, t := range r.Totals {
		note(t.Connection)
	}
	// The connections the records alone name follow by id.
	if len(extra) > 0 {
		head := out[:len(out)-len(extra)]
		tail := append([]ConnectionCells(nil), out[len(out)-len(extra):]...)
		sort.SliceStable(tail, func(i, j int) bool { return tail[i].Connection < tail[j].Connection })
		out = append(head, tail...)
		for i, c := range out {
			index[c.Connection] = i
		}
	}
	inCell := func(cell int) bool { return cell >= 0 && cell < p.Cells }
	for _, c := range r.Counts {
		if !inCell(c.Cell) {
			continue
		}
		addCount(&out[index[c.Connection]], c)
	}
	for _, f := range r.Findings {
		if !inCell(f.Cell) {
			continue
		}
		cell := &out[index[f.Connection]].Cells[f.Cell]
		cell.DamagedCopies += f.N
		cell.DamagedBytes += f.Bytes
		cell.UnavailableBlobs += f.UnavailableBlobs
	}
	for _, t := range r.Totals {
		ft := &out[index[t.Connection]].Findings
		ft.N += t.N
		ft.Bytes += t.Bytes
		ft.ByOutcome[t.Outcome] += t.N
		ft.BySource[t.Source] += t.N
	}
	byConn := map[string][]Interval{}
	for _, iv := range r.Intervals {
		byConn[iv.Connection] = append(byConn[iv.Connection], iv)
	}
	for conn, ivs := range byConn {
		intervalCells(out[index[conn]].Cells, ivs, p.Cell, to)
	}
	for i := range out {
		for j := range out[i].Cells {
			out[i].Cells[j].Tone = out[i].Cells[j].tone()
		}
	}
	return out
}

// addCount adds one connection's counts of a kind in a cell.
func addCount(cc *ConnectionCells, c CellCount) {
	cell := &cc.Cells[c.Cell]
	switch c.Kind {
	case "read_fallover":
		cell.ReadFallovers += c.N
	case "read_unavailable":
		cell.ReadsUnavailable += c.N
	case "write_fallover":
		cell.WriteFallovers += c.N
	case "upload_refused":
		cell.UploadsRefused += c.N
	case "copy_failed":
		cell.CopiesFailed += c.N
	case "encrypted_read_refused":
		cell.ReadsRefused += c.N
		if c.UnknownKey {
			cell.UnknownKeyReads += c.N
		}
	case "findings_unlisted":
		cell.DamagedCopies += c.N
		cell.DamagedBytes += c.Bytes
		cc.Findings.Unlisted += c.N
	}
}

// intervalCells adds one connection's intervals to its cells: the time the union of its unreachable intervals covers a cell, the
// replicas they were recorded on, and the failing start checks that overlap a cell.
func intervalCells(cells []Cell, ivs []Interval, width time.Duration, to time.Time) {
	var unreachable []span
	replicas := make([]map[string]bool, len(cells))
	for _, iv := range ivs {
		end := iv.Ended
		if end.IsZero() || end.After(to) {
			end = to
		}
		for i := range cells {
			cs, ce := cells[i].Start, cells[i].Start.Add(width)
			if !iv.Started.Before(ce) || !end.After(cs) {
				continue
			}
			if iv.Kind == KindStartCheckFailed {
				cells[i].StartChecksFailed++
				continue
			}
			if replicas[i] == nil {
				replicas[i] = map[string]bool{}
			}
			replicas[i][iv.Replica] = true
		}
		if iv.Kind == KindUnreachable {
			unreachable = append(unreachable, span{iv.Started, end})
		}
	}
	for _, s := range union(unreachable) {
		for i := range cells {
			cs, ce := cells[i].Start, cells[i].Start.Add(width)
			a, b := later(s.from, cs), earlier(s.to, ce)
			if b.After(a) {
				cells[i].UnreachableSeconds += int64(b.Sub(a) / time.Second)
			}
		}
	}
	for i := range cells {
		cells[i].Replicas = len(replicas[i])
	}
}

type span struct{ from, to time.Time }

// union merges overlapping spans.
func union(in []span) []span {
	sort.Slice(in, func(i, j int) bool { return in[i].from.Before(in[j].from) })
	var out []span
	for _, s := range in {
		if n := len(out); n > 0 && !s.from.After(out[n-1].to) {
			if s.to.After(out[n-1].to) {
				out[n-1].to = s.to
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
