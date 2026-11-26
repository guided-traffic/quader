package queue

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// A client's failed fetch is a row of the queue (R-CUR-011, Q-CUR-19): a fetch of a client that ends with a failure of the classes of
// R-CUR-011 writes, at most once per repository and download per replica and minute, one row of origin client and state failed per
// repository and download, whose occurrences and failure (with the file that failed last) the next failure updates; at most
// proxy.downloads.client_failures_per_repository such rows per repository, the oldest removed. A 404 of the upstream is not recorded:
// the negative cache holds it (R-REPO-028). A download is what a person asked for (operations-and-observability.md R-OPS-049): the
// file's name and version as the format classified it, so the layers of one image that fail are one row of the image, not one per layer.

// clientEvery is the throttle of one repository's download on one replica.
const clientEvery = time.Minute

// clientThrottleMax bounds the throttle's memory: beyond it the entries older than the throttle are dropped, and a replica that still
// has more starts again empty.
const clientThrottleMax = 10000

type clientThrottle struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newClientThrottle() *clientThrottle { return &clientThrottle{last: map[string]time.Time{}} }

// take reports whether the key may be written now, and notes it.
func (c *clientThrottle) take(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.last[key]; ok && now.Sub(t) < clientEvery {
		return false
	}
	if len(c.last) >= clientThrottleMax {
		for k, t := range c.last {
			if now.Sub(t) >= clientEvery {
				delete(c.last, k)
			}
		}
		if len(c.last) >= clientThrottleMax {
			c.last = map[string]time.Time{}
		}
	}
	c.last[key] = now
	return true
}

// ClientFailure records a client's failed fetch of file in a repository: name and version are what the format's classification says
// of the file ("" when it names none), and the download it belongs to; a file of no name is a download of its own. It never fails the
// client's request: an error is logged. A failure of the class upstream_not_found is not recorded.
func (s *Service) ClientFailure(ctx context.Context, repoID int64, name, version, file string, f remotecuration.Failure) {
	m := s.o.Maintenance
	if m == nil || f.Class == remotecuration.ClassUpstreamNotFound || f.Class == "" {
		return
	}
	now := s.o.Now()
	if name == "" {
		name = file
	}
	if !s.clients.take(strconv.FormatInt(repoID, 10)+"\x00"+name+"\x00"+version, now) {
		return
	}
	f.File, f.Attempt, f.At = file, 1, now
	if len(name) > 1024 {
		name = name[:1024]
	}
	if len(version) > 256 {
		version = version[:256]
	}
	err := m.ClientFailure(context.WithoutCancel(ctx), Row{RepoID: repoID, Name: name, Version: version, Failure: &f}, s.o.Settings().ClientFailures)
	if err != nil {
		s.o.Log.Warn("a client's failed fetch could not be recorded in the download queue", "repo_id", repoID, "error", err)
	}
}
