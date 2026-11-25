package testenv

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// VersionKey returns what the host stores with a version row of a format besides the version (api-and-ui.md R-API-029, Q-API-429): the
// sort key of the format's view adapter and whether the format calls the version a pre-release. A test that writes version rows by SQL
// writes both, as the host does; a format without a view adapter has the key of a string that is no version.
func VersionKey(tb testing.TB, format, version string) (key []byte, prerelease bool) {
	tb.Helper()
	f, ok := formats.Lookup(format)
	if !ok {
		tb.Fatalf("no format %q is registered", format)
	}
	key = versionkey.Of(nil, false, version)
	if a := f.View(); a != nil {
		key, prerelease = a.SortKey(version), a.Prerelease(version)
	}
	return key, prerelease
}

// VersionKey is [VersionKey] for the stack's test.
func (s *Stack) VersionKey(format, version string) (key []byte, prerelease bool) {
	s.tb.Helper()
	return VersionKey(s.tb, format, version)
}

// VersionKeys is [VersionKey] for many versions of one format, as the arrays of a statement that writes them by unnest: the versions,
// their keys and their pre-release flags.
func VersionKeys(tb testing.TB, format string, versions []string) (keys [][]byte, prerelease []bool) {
	tb.Helper()
	keys, prerelease = make([][]byte, len(versions)), make([]bool, len(versions))
	for i, v := range versions {
		keys[i], prerelease[i] = VersionKey(tb, format, v)
	}
	return keys, prerelease
}
