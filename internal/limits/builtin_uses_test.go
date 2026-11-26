package limits

import "testing"

// The cost of one act of a client in the figures of an hour (usage-and-limits.md R-LIM-042): the bytes it reads from the cache and
// from the upstream, its upstream requests, its refused requests. Measured or documented where R-LIM-042's table says so.
type act struct {
	cached, uncached, upstream, refused int64
}

func (a act) times(n int64) act {
	return act{a.cached * n, a.uncached * n, a.upstream * n, a.refused * n}
}

func (a act) plus(b act) act {
	return act{a.cached + b.cached, a.uncached + b.uncached, a.upstream + b.upstream, a.refused + b.refused}
}

var (
	// docker pull of an application image of 1 GB with 40 layers: from the cache, or its first pull through a remote (the tag's
	// revalidation, three manifests and 42 blobs).
	pullApp     = act{cached: 1 * gb}
	pullAppCold = act{uncached: 1 * gb, upstream: 46}
	// The first pull of nvidia/cuda's cudnn devel image (5.9 GB on amd64, Docker Hub's tag API, 2026-11-26).
	pullCUDACold = act{uncached: 5_900 * mb, upstream: 40}
	// npm: an install of 475 packages: npm ci from the cache (tarballs, 186 kB each), and the first install through a remote
	// (a package document and a tarball each, 900 kB together; measured from 559 packages of the web client's lockfile).
	npmCI       = act{cached: 475 * 186_000}
	npmInstCold = act{uncached: 475 * 900_000, upstream: 950}
	// An install of 4,000 packages through a remote on a cold cache.
	npmBigCold = act{uncached: 4000 * 900_000, upstream: 8000}
	// One package document kept fresh for an hour: NuGet's 300 s metadata period, revalidated twelve times.
	nugetFresh = act{upstream: 12}
	// An install that fails on a token without read: npm stops after the 15 requests it has open.
	npmRefused = act{refused: 15}
)

// TestR_LIM_042_TheBuiltInProfilesHoldTheUsesThePagePromises: each built-in profile holds within an hour, below every figure, the
// uses R-LIM-010 promises it, priced by R-LIM-042. A change of a figure that drops a promised use fails here.
func TestR_LIM_042_TheBuiltInProfilesHoldTheUsesThePagePromises(t *testing.T) {
	for _, c := range []struct {
		profile, use string
		cost         act
		dayCached    int64
	}{
		{ProfileAnonymous, "ten first pulls of 1 GB images at once from one address, then twenty more from the cache", pullAppCold.times(10).plus(pullApp.times(20)), 0},
		{ProfileAnonymous, "two first installs of 475 packages at once from one address, and the first pull of a CUDA image", npmInstCold.times(2).plus(pullCUDACold), 0},
		{ProfileAnonymous, "a dozen people behind one address whose expired or narrow token fails an install each", npmRefused.times(12), 0},
		{ProfileAnonymous, "a team behind one address pulling twenty 1 GB images an hour for eight hours", pullApp.times(20), 160 * gb},
		{ProfileRestricted, "one first install of 475 packages and one first pull of a CUDA image", npmInstCold.plus(pullCUDACold), 0},
		{ProfileRestricted, "forty pulls of a 1 GB image from the cache", pullApp.times(40), 0},
		{ProfileStandard, "an install of 4,000 packages on a cold cache, twice (a branch switch)", npmBigCold.times(2), 0},
		{ProfileStandard, "the first pulls of a dozen 1 GB images, a CUDA image and a PyTorch image (3.7 GB)", pullAppCold.times(12).plus(pullCUDACold).plus(act{uncached: 3_700 * mb, upstream: 30}), 0},
		{ProfileStandard, "a compose file of a dozen 1 GB images pulled ten times from the cache, eight hours a day", pullApp.times(120), 960 * gb},
		{ProfileStandard, "a solution of 300 packages over two NuGet feeds kept fresh all hour", nugetFresh.times(600), 0},
		{ProfileStandard, "forty installs that fail on a token without read", npmRefused.times(40), 0},
		{ProfileTechnical, "twenty parallel CI jobs of one token, each installing 475 other packages on a cold cache", npmInstCold.times(20), 0},
		{ProfileTechnical, "2,000 packages over two NuGet feeds kept fresh all hour", nugetFresh.times(4000), 0},
		{ProfileTechnical, "a CI fleet's first pulls after its base images changed: 100 images of 1 GB and eight CUDA images", pullAppCold.times(100).plus(pullCUDACold.times(8)), 0},
		{ProfileTechnical, "2,000 jobs an hour on fresh runners, each pulling a 1 GB image and running npm ci, ten hours a day", pullApp.plus(npmCI).times(2000), 21_800 * gb},
		{ProfileTechnical, "a token that lost read on one repository while twenty jobs fail on it every five minutes", npmRefused.times(20 * 12), 0},
	} {
		t.Run(c.profile+": "+c.use, func(t *testing.T) {
			f := BuiltinProfiles()[c.profile]
			for _, x := range []struct {
				fig  Figure
				need int64
			}{
				{CachedBytesPerHour, c.cost.cached},
				{UncachedBytesPerHour, c.cost.uncached},
				{UpstreamRequestsPerHour, c.cost.upstream},
				{BytesPerDay, max(c.dayCached, c.cost.cached+c.cost.uncached)},
				{RefusedPerHour, c.cost.refused},
			} {
				if f[x.fig] != 0 && x.need >= f[x.fig] {
					t.Errorf("%s needs %d, the figure is %d", x.fig, x.need, f[x.fig])
				}
			}
		})
	}
}

// TestR_LIM_042_TheBuiltInProfilesKeepTheirOrder: per byte, upstream and refused figure Anonymous is at most Restricted, Restricted
// at most Standard, Standard at most Technical, so that a caller never gets more by not signing in and a narrowed caller never more
// than a person.
func TestR_LIM_042_TheBuiltInProfilesKeepTheirOrder(t *testing.T) {
	b := BuiltinProfiles()
	order := []string{ProfileAnonymous, ProfileRestricted, ProfileStandard, ProfileTechnical}
	for _, fig := range []Figure{CachedBytesPerSecond, CachedBytesPerHour, UncachedBytesPerSecond, UncachedBytesPerHour, UpstreamRequestsPerHour, BytesPerDay, RefusedPerHour} {
		for i := 1; i < len(order); i++ {
			if lo, hi := b[order[i-1]][fig], b[order[i]][fig]; lo > hi {
				t.Errorf("%s: %s %d above %s %d", fig, order[i-1], lo, order[i], hi)
			}
		}
	}
}
