package ansible

import "testing"

// TestR_API_029_ACollectionVersionIsAPrereleaseByItsSemanticVersion: the view adapter calls a version a pre-release exactly when its
// semantic version has a pre-release part, build metadata aside, the versions ansible-galaxy installs only when asked for them (Q-ANS-92,
// Q-API-433); a string that is no semantic version is none.
func TestR_API_029_ACollectionVersionIsAPrereleaseByItsSemanticVersion(t *testing.T) {
	a := viewAdapter{}
	for v, want := range map[string]bool{
		"1.0.0-beta.1": true, "2.0.0-rc.1+build.5": true, "1.0.0-0": true,
		"1.0.0": false, "1.0.0+build-5": false, "v1.0.0-rc.1": false, "1.0-rc.1": false, "garbage": false,
	} {
		if got := a.Prerelease(v); got != want {
			t.Errorf("Prerelease(%q) = %v, want %v", v, got, want)
		}
	}
}
