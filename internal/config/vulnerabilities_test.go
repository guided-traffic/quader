package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func loadVuln(t *testing.T, file string, env ...string) (*Loaded, error) {
	t.Helper()
	return Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: minimalEnv(env...), Command: CommandMigrate})
}

// R-VULN-025: the section's defaults are the specification's.
func TestR_VULN_025_TheSectionDefaults(t *testing.T) {
	l, err := loadVuln(t, "")
	if err != nil {
		t.Fatal(err)
	}
	v := l.Config.Vulnerabilities
	if !v.Enabled || v.Enforcement != "enforce" || v.StaleAfter != 24*time.Hour {
		t.Errorf("%+v", v)
	}
	o := v.Sources.OSV
	if o.Mode != "online" || o.BaseURL != "https://osv-vulnerabilities.storage.googleapis.com/" || len(o.Ecosystems) != 0 || o.Repository != "" ||
		o.FullResyncThreshold != 2000 || o.Overlap != time.Hour || o.MaxShrink != 0.2 || o.Timeout != 10*time.Minute ||
		o.MaxArchiveSize != GiB || o.MaxRecordSize != 8*MiB {
		t.Errorf("osv %+v", o)
	}
	k := v.Sources.KEV
	if k.Mode != "online" || k.URL != "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json" || k.Repository != "" {
		t.Errorf("kev %+v", k)
	}
	e := v.Sources.EPSS
	if e.Mode != "off" || e.URL != "https://epss.empiricalsecurity.com/epss_scores-current.csv.gz" {
		t.Errorf("epss %+v", e)
	}
	pu := v.Matching.PublicUpstreams
	if !slices.Equal(pu["npm"], []string{"registry.npmjs.org"}) || !slices.Equal(pu["nuget"], []string{"api.nuget.org", "www.nuget.org"}) || len(pu) != 2 {
		t.Errorf("public_upstreams %v", pu)
	}
	if v.Findings.ResolvedRetention != 9600*time.Hour || v.Export.MaxRows != 100000 {
		t.Errorf("findings %+v export %+v", v.Findings, v.Export)
	}
	if got := o.EffectiveEcosystems(); !slices.Equal(got, []string{"npm", "NuGet"}) {
		t.Errorf("EffectiveEcosystems = %v", got)
	}
}

// R-VULN-025 acceptance: `quader admin config validate` refuses max_shrink: 1.5, ecosystems: [PyPI] on a release without a
// PyPI adapter, and mode: repository without repository.
func TestR_VULN_025_TheRefusals(t *testing.T) {
	cases := []struct {
		name, file string
		env        []string
		keys       []string
	}{
		{"max_shrink above 1", "vulnerabilities:\n  sources:\n    osv:\n      max_shrink: 1.5\n", nil, []string{"vulnerabilities.sources.osv.max_shrink"}},
		{"max_shrink below 0", "vulnerabilities:\n  sources:\n    osv:\n      max_shrink: -0.1\n", nil, []string{"vulnerabilities.sources.osv.max_shrink"}},
		{"an ecosystem without an adapter", "vulnerabilities:\n  sources:\n    osv:\n      ecosystems: [npm, PyPI]\n", nil, []string{"vulnerabilities.sources.osv.ecosystems", "PyPI"}},
		{"the ecosystem is spelled as OSV spells it", "vulnerabilities:\n  sources:\n    osv:\n      ecosystems: [nuget]\n", nil, []string{"vulnerabilities.sources.osv.ecosystems"}},
		{"mode repository without repository", "vulnerabilities:\n  sources:\n    osv:\n      mode: repository\n", nil, []string{"vulnerabilities.sources.osv.repository"}},
		{"kev mode repository without repository", "vulnerabilities:\n  sources:\n    kev:\n      mode: repository\n", nil, []string{"vulnerabilities.sources.kev.repository"}},
		{"a bare key as the mirror repository (Q-AUTHZ-206)", "vulnerabilities:\n  sources:\n    osv:\n      mode: repository\n      repository: vuln-mirror\n", nil, []string{"vulnerabilities.sources.osv.repository", "qualified name"}},
		{"a pattern as the kev repository", "vulnerabilities:\n  sources:\n    kev:\n      mode: off\n      repository: platform/vuln-*\n", nil, []string{"vulnerabilities.sources.kev.repository"}},
		{"an http base url", "vulnerabilities:\n  sources:\n    osv:\n      base_url: http://osv.example.com/\n", nil, []string{"vulnerabilities.sources.osv.base_url", "https"}},
		{"an http kev url", "vulnerabilities:\n  sources:\n    kev:\n      url: http://kev.example.com/k.json\n", nil, []string{"vulnerabilities.sources.kev.url"}},
		{"a base url from the environment", "", []string{"QUADER_VULNERABILITIES_SOURCES_OSV_BASE_URL=ftp://x/"}, []string{"vulnerabilities.sources.osv.base_url"}},
		{"epss is P1 and stays off", "vulnerabilities:\n  sources:\n    epss:\n      mode: online\n", nil, []string{"vulnerabilities.sources.epss.mode", "not built"}},
		{"an unknown enforcement", "vulnerabilities:\n  enforcement: warn\n", nil, []string{"vulnerabilities.enforcement"}},
		{"an unknown source mode", "vulnerabilities:\n  sources:\n    osv:\n      mode: mirror\n", nil, []string{"vulnerabilities.sources.osv.mode"}},
		{"a public upstream with a scheme", "vulnerabilities:\n  matching:\n    public_upstreams:\n      npm: [\"https://registry.npmjs.org\"]\n", nil, []string{"vulnerabilities.matching.public_upstreams"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadVuln(t, c.file, c.env...)
			if err == nil {
				t.Fatal("accepted")
			}
			for _, k := range c.keys {
				if !strings.Contains(err.Error(), k) {
					t.Errorf("the error does not mention %q: %v", k, err)
				}
			}
		})
	}
}

func TestR_VULN_025_WhatIsAccepted(t *testing.T) {
	for name, file := range map[string]string{
		"a narrowed list":                       "vulnerabilities:\n  sources:\n    osv:\n      ecosystems: [NuGet]\n",
		"mode repository with a qualified name": "vulnerabilities:\n  sources:\n    osv:\n      mode: repository\n      repository: platform/vuln-mirror\n    kev:\n      mode: repository\n      repository: platform/vuln-mirror\n",
		"mode off":                              "vulnerabilities:\n  sources:\n    osv:\n      mode: off\n    kev:\n      mode: off\n",
		"report mode":                           "vulnerabilities:\n  enforcement: report\n",
		"an internal mirror's own https":        "vulnerabilities:\n  sources:\n    osv:\n      base_url: https://osv-mirror.corp.example/osv/\n",
		"max_shrink at the bounds":              "vulnerabilities:\n  sources:\n    osv:\n      max_shrink: 1\n",
		"another public upstream list":          "vulnerabilities:\n  matching:\n    public_upstreams:\n      npm: [registry.npmjs.org, registry.npmmirror.com]\n",
	} {
		if _, err := loadVuln(t, file); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The environment override of the specification (INV-15).
	l, err := loadVuln(t, "", "QUADER_VULNERABILITIES_ENFORCEMENT=report", "QUADER_VULNERABILITIES_STALE_AFTER=48h")
	if err != nil || l.Config.Vulnerabilities.Enforcement != "report" || l.Config.Vulnerabilities.StaleAfter != 48*time.Hour {
		t.Fatalf("%v %v", l, err)
	}
}

// R-VULN-044, Q-VULN-171: the database comes from upstream/scanner-trivy and its image aquasec/trivy-db by default, the tag is no
// setting, and the scanning reads through the well-known repository exactly while it is on and the setting names it.
func TestR_VULN_044_TheDatabaseIsNamedByItsRepositoryAndImage(t *testing.T) {
	l, err := loadVuln(t, "")
	if err != nil {
		t.Fatal(err)
	}
	d := l.Config.Vulnerabilities.Images.Database
	if d.Repository != "upstream/scanner-trivy" || d.Image != "aquasec/trivy-db" || d.MaxArchiveSize != 2*GiB || d.Timeout != 15*time.Minute {
		t.Errorf("database %+v", d)
	}
	if !l.Config.Vulnerabilities.ReadsThroughScannerTrivy() {
		t.Error("the defaults do not read through upstream/scanner-trivy")
	}
	for name, file := range map[string]string{
		"the image scanning off":  "vulnerabilities:\n  images:\n    enabled: false\n",
		"the scanning off":        "vulnerabilities:\n  enabled: false\n",
		"a repository of its own": "vulnerabilities:\n  images:\n    database:\n      repository: ops/scanner-db\n",
	} {
		l, err := loadVuln(t, file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if l.Config.Vulnerabilities.ReadsThroughScannerTrivy() {
			t.Errorf("%s: upstream/scanner-trivy reads as in use", name)
		}
	}
	l, err = loadVuln(t, "vulnerabilities:\n  images:\n    database:\n      repository: platform/ghcr\n      image: aquasecurity/trivy-db\n")
	if err != nil {
		t.Fatalf("another registry's name in a repository of its own: %v", err)
	}
	if got := l.Config.Vulnerabilities.Images.Database; got.Repository != "platform/ghcr" || got.Image != "aquasecurity/trivy-db" {
		t.Errorf("%+v", got)
	}
}

// R-VULN-025, R-VULN-044 acceptance: `quader admin config validate` refuses a repository without a project, an image that is no
// image name, the name of another registry with the well-known repository, and a key below sources.trivy, which no longer exists.
func TestR_VULN_044_TheRefusalsOfTheDatabaseSetting(t *testing.T) {
	cases := []struct {
		name, file string
		env        []string
		keys       []string
	}{
		{"a repository without a project", "vulnerabilities:\n  images:\n    database:\n      repository: scanner-trivy\n", nil, []string{"vulnerabilities.images.database.repository", "qualified name"}},
		{"an image that is no image name", "vulnerabilities:\n  images:\n    database:\n      image: Trivy-DB\n", nil, []string{"vulnerabilities.images.database.image", "R-OCI-002"}},
		{"another registry's name with the well-known repository", "vulnerabilities:\n  images:\n    database:\n      image: aquasecurity/trivy-db\n", nil, []string{"vulnerabilities.images.database.image", "upstream/scanner-trivy", "aquasec/trivy-db"}},
		{"the same from the environment", "", []string{"QUADER_VULNERABILITIES_IMAGES_DATABASE_IMAGE=aquasecurity/trivy-db"}, []string{"vulnerabilities.images.database.image"}},
		{"a key below sources.trivy", "vulnerabilities:\n  sources:\n    trivy:\n      mode: online\n", nil, []string{"trivy"}},
		{"a size below the bound", "vulnerabilities:\n  images:\n    database:\n      max_archive_size: 1KiB\n", nil, []string{"vulnerabilities.images.database.max_archive_size"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadVuln(t, c.file, c.env...)
			if err == nil {
				t.Fatal("accepted")
			}
			for _, k := range c.keys {
				if !strings.Contains(err.Error(), k) {
					t.Errorf("the error does not mention %q: %v", k, err)
				}
			}
		})
	}
}
