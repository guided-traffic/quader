package wellknown_test

import (
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"

	_ "github.com/guided-traffic/quader/internal/formats/all"
)

// retiredPresetIDs are the ids of presets a release of Quader no longer declares (R-REPO-062, Q-REPO-387): their repositories
// stay as tombstones in every installation that had them (R-REPO-067 step 5), so no later preset takes one. A preset taken out of
// a format's list is added here in the same change.
var retiredPresetIDs = []string{}

// TestR_REPO_066_TheProductDeclaresOneRepositoryPerPresetAndTheSystemRepositories: Declared over the registered catalog lists one
// preset repository per upstream preset of every format, in catalog order, and then scanner-trivy and scanner-clamav; every key is a repository key
// of the project upstream, unique, not reserved and not retired.
func TestR_REPO_066_TheProductDeclaresOneRepositoryPerPresetAndTheSystemRepositories(t *testing.T) {
	got := wellknown.Declared(nil)
	var keys []string
	for _, d := range got {
		keys = append(keys, string(d.Format)+":"+d.Key)
	}
	want := []string{
		"ansible:galaxy",
		"deb:debian", "deb:debian-security", "deb:ubuntu", "deb:ubuntu-security", "deb:ubuntu-ports",
		"npm:npmjs",
		"nuget:nuget-org",
		"oci:docker-hub", "oci:quay", "oci:ghcr", "oci:gitlab",
		"oci:scanner-trivy",
		"generic:scanner-clamav",
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("declared %v\nwant     %v", keys, want)
	}
	t.Run("R-REPO-062 each key is a free repository key of the project upstream", func(t *testing.T) {
		seen := map[string]bool{}
		for _, d := range got {
			name := d.Name()
			switch {
			case !name.Valid() || name.Project != wellknown.ProjectKey:
				t.Errorf("%s is no repository name of the project upstream", name)
			case seen[d.Key]:
				t.Errorf("%s is declared twice", d.Key)
			case repository.IsReservedKey(d.Key):
				t.Errorf("%s is a reserved repository key", d.Key)
			case slices.Contains(retiredPresetIDs, d.Key):
				t.Errorf("%s is the id of a retired preset", d.Key)
			case d.Kind == wellknown.KindPreset && slices.Contains(wellknown.ReservedKeys(), d.Key):
				t.Errorf("the preset %s takes a reserved key of the project upstream", d.Key)
			}
			seen[d.Key] = true
		}
	})
	t.Run("R-REPO-066 a preset repository carries its preset", func(t *testing.T) {
		for _, d := range got {
			if d.Kind != wellknown.KindPreset {
				continue
			}
			e, _ := catalog.Default().Lookup(string(d.Format))
			i := slices.IndexFunc(e.UpstreamPresets, func(p catalog.UpstreamPreset) bool { return p.ID == d.Key })
			if i < 0 {
				t.Fatalf("%s names no preset of %s", d.Key, d.Format)
			}
			p := e.UpstreamPresets[i]
			if d.URL != p.URL || d.DisplayName != p.DisplayName || d.Default != p.Default || !slices.Equal(d.ExtraHosts, p.ExtraHosts) ||
				string(d.Settings) != string(p.Settings) || d.IncludePatterns != nil || d.Private || d.ScanningOff || d.UnusedRetentionHours != 0 ||
				d.Class != catalog.Remote {
				t.Errorf("%s = %+v, preset %+v", d.Key, d, p)
			}
		}
	})
	t.Run("R-REPO-070 scanner-trivy is the image scanning's database repository", func(t *testing.T) {
		d, _ := wellknown.Lookup(got, wellknown.ScannerTrivyKey)
		if d.Name() != wellknown.ScannerTrivy || d.Kind != wellknown.KindSystem || d.Format != "oci" || d.Class != catalog.Remote || d.URL != "https://mirror.gcr.io/" ||
			len(d.ExtraHosts) != 0 || !slices.Equal(d.IncludePatterns, []string{wellknown.ScannerTrivyImage}) || !d.Private || !d.ScanningOff || d.UnusedRetentionHours != 48 {
			t.Fatalf("scanner-trivy = %+v", d)
		}
		if wellknown.ScannerTrivy != (qname.Repo{Project: "upstream", Key: "scanner-trivy"}) || wellknown.ScannerTrivyImage != "aquasec/trivy-db" {
			t.Fatalf("names: %v %q", wellknown.ScannerTrivy, wellknown.ScannerTrivyImage)
		}
	})
	t.Run("R-REPO-075 Q-REPO-410 Q-REPO-411 scanner-clamav is the malware scanning's local generic database repository", func(t *testing.T) {
		d := got[len(got)-1]
		if d.Name() != wellknown.ScannerClamav || d.Kind != wellknown.KindSystem || d.Format != "generic" || d.Class != catalog.Local || !d.Local() ||
			d.URL != "" || d.ExtraHosts != nil || d.Settings != nil || d.IncludePatterns != nil || !d.Private || d.Level != "private" ||
			d.ScanningOff || d.UnusedRetentionHours != 0 || d.DisplayName != "Malware scan database" {
			t.Fatalf("scanner-clamav = %+v", d)
		}
		if wellknown.ScannerClamav != (qname.Repo{Project: "upstream", Key: "scanner-clamav"}) {
			t.Fatalf("name: %v", wellknown.ScannerClamav)
		}
	})
	t.Run("R-REPO-066 a catalog without a format declares nothing of it", func(t *testing.T) {
		if d := wellknown.Declared(catalog.NewRegistry()); len(d) != 0 {
			t.Fatalf("an empty catalog declares %+v", d)
		}
	})
}

// TestReservedKeysAreTheCatalogs holds the reserved ids of the catalog, a leaf that imports neither list, to the reserved
// repository keys of R-REPO-002 and the reserved keys of the project upstream (R-REPO-062, Q-REPO-387).
func TestReservedKeysAreTheCatalogs(t *testing.T) {
	want := append(repository.ReservedKeys(), wellknown.ReservedKeys()...)
	got := catalog.ReservedPresetIDs()
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("catalog.ReservedPresetIDs() = %v, want %v", got, want)
	}
}

// TestR_REPO_068_TheStateIsDerivedFromWhatIsStored: the four states of a preset repository and none for a system repository
// (Q-REPO-375).
func TestR_REPO_068_TheStateIsDerivedFromWhatIsStored(t *testing.T) {
	for _, tc := range []struct {
		kind                                wellknown.Kind
		disabled, switchedOnBefore, retired bool
		want                                wellknown.State
	}{
		{wellknown.KindPreset, false, true, false, wellknown.StateActive},
		{wellknown.KindPreset, false, false, false, wellknown.StateActive},
		{wellknown.KindPreset, true, true, false, wellknown.StateSwitchedOff},
		{wellknown.KindPreset, true, false, false, wellknown.StateNotSwitchedOn},
		{wellknown.KindPreset, true, true, true, wellknown.StateRetired},
		{wellknown.KindPreset, false, false, true, wellknown.StateRetired},
		{wellknown.KindSystem, false, false, false, ""},
		{wellknown.KindSystem, true, false, false, ""},
	} {
		if got := wellknown.StateOf(tc.kind, tc.disabled, tc.switchedOnBefore, tc.retired); got != tc.want {
			t.Errorf("StateOf(%s, disabled %v, before %v, retired %v) = %q, want %q", tc.kind, tc.disabled, tc.switchedOnBefore, tc.retired, got, tc.want)
		}
	}
}

// R-VULN-044, Q-VULN-171: the defaults of vulnerabilities.images.database are the well-known repository of the database and the one
// image its include pattern admits; the configuration spells them because it cannot import this package.
func TestR_VULN_044_TheDefaultsAreTheWellKnownRepositorys(t *testing.T) {
	if config.ScannerTrivyRepository != wellknown.ScannerTrivy.String() || config.ScannerTrivyImage != wellknown.ScannerTrivyImage {
		t.Fatalf("config %s %s, wellknown %s %s", config.ScannerTrivyRepository, config.ScannerTrivyImage, wellknown.ScannerTrivy, wellknown.ScannerTrivyImage)
	}
}
