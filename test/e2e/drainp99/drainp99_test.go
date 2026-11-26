package drainp99

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)

const ms = time.Millisecond

// window is 1,000 documents a minute from `from` whose p99 is p99: 979 of 20 ms and 21 at p99, spread through the minute.
func window(from time.Time, p99 time.Duration) []Sample {
	out := make([]Sample, 0, 1000)
	for i := range 1000 {
		took := 20 * ms
		if i%90 == 45 || i >= 990 {
			took = p99
		}
		out = append(out, Sample{At: from.Add(time.Duration(i) * 60 * ms), Kind: "document", Took: took})
	}
	return out
}

// run is the k6 log of a drain: the warm-up from t0, the drain window from t0+1m, the post-drain window from t0+2m, and one
// tarball a second of 900 ms beside them, which the documents' comparison does not read.
func run(warm, drain, post time.Duration) []Sample {
	var s []Sample
	s = append(s, window(t0, warm)...)
	s = append(s, window(t0.Add(time.Minute), drain)...)
	s = append(s, window(t0.Add(2*time.Minute), post)...)
	for i := range 180 {
		s = append(s, Sample{At: t0.Add(time.Duration(i) * time.Second), Kind: "tarball", Took: 900 * ms})
	}
	return s
}

func judge(s []Sample) Verdict {
	return Judge(s, "document", t0, t0.Add(time.Minute), t0.Add(2*time.Minute), t0.Add(3*time.Minute))
}

// TestR_HA_059_TheDrainIsHeldToTheLargerOfWarmUpAndPostDrain: the documents' p99 of the nightly of 2026-11-26 that missed (269 ms
// in the drain window against a warm-up of 115 ms, beside another job's load) passes when the minute after the drain carries the same
// neighbour (160 ms), and still fails when the minute after it is as quiet as the warm-up: then the drain alone lifted its window.
// A regression of the drain fails beside the neighbour too.
func TestR_HA_059_TheDrainIsHeldToTheLargerOfWarmUpAndPostDrain(t *testing.T) {
	for name, tc := range map[string]struct {
		warm, drain, post time.Duration
		bound             time.Duration
		within            bool
	}{
		"a neighbour lifts the drain and the minute after it": {115 * ms, 269 * ms, 160 * ms, 320 * ms, true},
		"only the drain window is slow":                       {115 * ms, 269 * ms, 112 * ms, 230 * ms, false},
		"the drain regresses beside the neighbour":            {115 * ms, 400 * ms, 160 * ms, 320 * ms, false},
		"a flat night, ratio 1.36 to the warm-up":             {110 * ms, 150 * ms, 108 * ms, 220 * ms, true},
		"a load that only ran before the drain":               {200 * ms, 150 * ms, 90 * ms, 400 * ms, true},
	} {
		t.Run(name, func(t *testing.T) {
			v := judge(run(tc.warm, tc.drain, tc.post))
			if v.Warm.P99 != tc.warm || v.Drain.P99 != tc.drain || v.Post.P99 != tc.post {
				t.Fatalf("p99 warm %s drain %s post %s, want %s %s %s", v.Warm.P99, v.Drain.P99, v.Post.P99, tc.warm, tc.drain, tc.post)
			}
			if v.Bound() != tc.bound || v.Within() != tc.within {
				t.Errorf("bound %s within %t, want %s %t", v.Bound(), v.Within(), tc.bound, tc.within)
			}
			if !v.Enough(1000) || v.Enough(1001) {
				t.Errorf("counts warm %d drain %d post %d, want 1000 each", v.Warm.N, v.Drain.N, v.Post.N)
			}
		})
	}
}

// TestR_HA_059_TheWindowsOfTheDrainComparison: a request belongs to the window it started in; the first eviction opens the drain
// window and its end opens the post-drain window; "" reads every kind.
func TestR_HA_059_TheWindowsOfTheDrainComparison(t *testing.T) {
	s := []Sample{
		{At: t0, Kind: "document", Took: 10 * ms},
		{At: t0.Add(time.Minute), Kind: "document", Took: 30 * ms},
		{At: t0.Add(2 * time.Minute), Kind: "document", Took: 50 * ms},
		{At: t0.Add(2 * time.Minute), Kind: "tarball", Took: 70 * ms},
		{At: t0.Add(3 * time.Minute), Kind: "document", Took: 90 * ms},
	}
	v := judge(s)
	if v.Warm.N != 1 || v.Warm.P99 != 10*ms || v.Drain.N != 1 || v.Drain.P99 != 30*ms || v.Post.N != 1 || v.Post.P99 != 50*ms {
		t.Errorf("windows %+v", v)
	}
	if all := Of(s, "", t0.Add(2*time.Minute), t0.Add(3*time.Minute)); all.N != 2 || all.P99 != 50*ms {
		t.Errorf("every kind after the drain: %+v", all)
	}
	if none := Of(s, "document", t0.Add(time.Hour), t0.Add(2*time.Hour)); none.N != 0 || none.P99 != 0 {
		t.Errorf("an empty window: %+v", none)
	}
}
