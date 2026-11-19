// Package limits holds the download limits of usage-and-limits.md (walk item W-26): the figures and profiles, the rules
// at the instance, a project and a repository and their resolution (Index), the subject a request is charged to and its
// budget key (Charge), the counters in Valkey and their fallback, the decisions at stage 7, before a download, before an
// upstream exchange (step 9a of R-REPO-025), the pacing of a download, and the per-replica buffers that carry bytes and
// refusals to Valkey every second. The usage store is internal/limits/usage, the store of the settings
// internal/limits/store, the HTTP stage and the counting writer internal/limits/httplimit.
//
// Valkey enforces, PostgreSQL remembers: no request reads or writes PostgreSQL here; the index is compiled in memory and
// rebuilt on a change of the area limits (R-ARCH-018).
package limits

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Figure is one counted quantity of R-LIM-004.
type Figure uint8

// The figures, in the order of the API and the profile table.
const (
	RequestsPerSecond Figure = iota
	RequestsBurst
	RequestsPerHour
	CachedBytesPerSecond
	CachedBytesPerHour
	UncachedBytesPerSecond
	UncachedBytesPerHour
	UpstreamRequestsPerHour
	ParallelDownloads
	BytesPerDay
	RefusedPerHour
	// NumFigures is the number of figures.
	NumFigures
)

var figureNames = [NumFigures]string{
	"requestsPerSecond", "requestsBurst", "requestsPerHour", "cachedBytesPerSecond", "cachedBytesPerHour",
	"uncachedBytesPerSecond", "uncachedBytesPerHour", "upstreamRequestsPerHour", "parallelDownloads", "bytesPerDay",
	"refusedPerHour",
}

// columnNames are the columns of limit_profile in figure order.
var columnNames = [NumFigures]string{
	"requests_per_second", "requests_burst", "requests_per_hour", "cached_bytes_per_second", "cached_bytes_per_hour",
	"uncached_bytes_per_second", "uncached_bytes_per_hour", "upstream_requests_per_hour", "parallel_downloads",
	"bytes_per_day", "refused_per_hour",
}

// String is the figure's API name.
func (f Figure) String() string {
	if f < NumFigures {
		return figureNames[f]
	}
	return "figure(" + strconv.Itoa(int(f)) + ")"
}

// Column is the figure's column in limit_profile.
func (f Figure) Column() string {
	if f < NumFigures {
		return columnNames[f]
	}
	return ""
}

// ParseFigure reads an API name.
func ParseFigure(s string) (Figure, bool) {
	for i, n := range figureNames {
		if n == s {
			return Figure(i), true
		}
	}
	return 0, false
}

// AllFigures lists the figures in order.
func AllFigures() []Figure {
	out := make([]Figure, NumFigures)
	for i := range out {
		out[i] = Figure(i)
	}
	return out
}

// Figures holds a value of every figure. 0 is unlimited (the API's null): every set figure is at least 1 (R-LIM-004).
type Figures [NumFigures]int64

// Unlimited reports whether figure f is unlimited.
func (fs Figures) Unlimited(f Figure) bool { return fs[f] <= 0 }

// Burst is the burst of the request figure: requestsBurst, or requestsPerSecond when the burst is empty (a window of one
// second, R-LIM-004); 0 when requestsPerSecond is unlimited.
func (fs Figures) Burst() int64 {
	if fs[RequestsPerSecond] <= 0 {
		return 0
	}
	if fs[RequestsBurst] > 0 {
		return fs[RequestsBurst]
	}
	return fs[RequestsPerSecond]
}

// AllUnlimited reports whether every figure is unlimited.
func (fs Figures) AllUnlimited() bool {
	for _, v := range fs {
		if v > 0 {
			return false
		}
	}
	return true
}

// MostGenerous is, per figure, the larger value; unlimited is the most generous (owner, decision 2).
func MostGenerous(a, b Figures) Figures {
	var out Figures
	for i := range out {
		switch {
		case a[i] <= 0 || b[i] <= 0:
			out[i] = 0
		default:
			out[i] = max(a[i], b[i])
		}
	}
	return out
}

// minFigure is min with unlimited as the largest value.
func minFigure(a, b int64) int64 {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	}
	return min(a, b)
}

// maxFigure is max with unlimited as the largest value.
func maxFigure(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return max(a, b)
}

// Validate applies the rules of R-LIM-004 to a set of figures: no negative value, a burst only with a rate, and a burst
// not below the rate. It names the first figure that breaks one.
func (fs Figures) Validate() (Figure, string, bool) {
	for i, v := range fs {
		if v < 0 {
			return Figure(i), "must be at least 1, or null for unlimited", false
		}
	}
	if fs[RequestsBurst] > 0 && fs[RequestsPerSecond] <= 0 {
		return RequestsBurst, "needs requestsPerSecond", false
	}
	if fs[RequestsBurst] > 0 && fs[RequestsBurst] < fs[RequestsPerSecond] {
		return RequestsBurst, "must be at least requestsPerSecond", false
	}
	return 0, "", true
}

// MarshalJSON writes every figure by its API name, null for unlimited.
func (fs Figures) MarshalJSON() ([]byte, error) {
	m := make(map[string]*int64, NumFigures)
	for i := range fs {
		if fs[i] > 0 {
			v := fs[i]
			m[figureNames[i]] = &v
		} else {
			m[figureNames[i]] = nil
		}
	}
	return json.Marshal(m)
}

// UnmarshalJSON reads figures by their API names; a missing member and null are unlimited, an unknown member is refused.
func (fs *Figures) UnmarshalJSON(b []byte) error {
	var m map[string]*int64
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*fs = Figures{}
	for k, v := range m {
		f, ok := ParseFigure(k)
		if !ok {
			return fmt.Errorf("unknown figure %q", k)
		}
		if v != nil {
			if *v < 1 {
				return fmt.Errorf("figure %s must be at least 1, or null for unlimited", k)
			}
			fs[f] = *v
		}
	}
	return nil
}

// Overrides set single figures of a profile (R-LIM-011): Set[f] says that f is overridden, Values[f] is its value (0 is
// unlimited).
type Overrides struct {
	Set    [NumFigures]bool
	Values Figures
}

// Apply returns the profile's figures with the overrides.
func (o Overrides) Apply(base Figures) Figures {
	out := base
	for i := range out {
		if o.Set[i] {
			out[i] = o.Values[i]
		}
	}
	return out
}

// Empty reports whether no figure is overridden.
func (o Overrides) Empty() bool {
	for _, s := range o.Set {
		if s {
			return false
		}
	}
	return true
}

// MarshalJSON writes the overridden figures only, null for unlimited.
func (o Overrides) MarshalJSON() ([]byte, error) {
	m := map[string]*int64{}
	for i := range o.Set {
		if !o.Set[i] {
			continue
		}
		if o.Values[i] > 0 {
			v := o.Values[i]
			m[figureNames[i]] = &v
		} else {
			m[figureNames[i]] = nil
		}
	}
	return json.Marshal(m)
}

// UnmarshalJSON reads overrides: a named figure is set, null to unlimited.
func (o *Overrides) UnmarshalJSON(b []byte) error {
	var m map[string]*int64
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*o = Overrides{}
	for k, v := range m {
		f, ok := ParseFigure(k)
		if !ok {
			return fmt.Errorf("unknown figure %q", k)
		}
		o.Set[f] = true
		if v != nil {
			if *v < 1 {
				return fmt.Errorf("figure %s must be at least 1, or null for unlimited", k)
			}
			o.Values[f] = *v
		}
	}
	return nil
}
