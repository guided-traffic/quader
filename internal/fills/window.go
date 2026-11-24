package fills

import "time"

// SpeedWindow is the window of a running download's speed (Q-OPS-421).
const SpeedWindow = 10 * time.Second

// sample is the count of a download's bytes at a time since its begin.
type sample struct {
	at    time.Duration
	bytes int64
}

// window holds the samples a download's speed is computed from: one per publication, on the replica that reads the body. It keeps
// the newest sample at or before the window's start as the base, and nothing older, so it holds about SpeedWindow / interval + 2
// samples.
type window struct {
	samples []sample
}

// speed records the count at the time since the begin and returns the bytes per second of the last SpeedWindow (R-OPS-050): the
// bytes since the newest sample at or before the window's start, over the time since it. Before SpeedWindow has passed, the begin is the
// base: the bytes over the time since the begin. In the first second there is no speed (ok false).
func (w *window) speed(elapsed time.Duration, bytes int64) (perSecond int64, ok bool) {
	now := sample{at: elapsed, bytes: bytes}
	w.samples = append(w.samples, now)
	if elapsed < time.Second {
		return 0, false
	}
	base := sample{} // the begin
	if cut := elapsed - SpeedWindow; cut > 0 {
		i := -1
		for j, s := range w.samples {
			if s.at <= cut {
				i = j
			}
		}
		if i >= 0 {
			base = w.samples[i]
			w.samples = append(w.samples[:0], w.samples[i:]...)
		}
	}
	span := now.at - base.at
	if span <= 0 {
		return 0, false
	}
	return int64(float64(now.bytes-base.bytes) / span.Seconds()), true
}
