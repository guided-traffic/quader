package config

import (
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// Vulnerabilities is the vulnerabilities section (R-VULN-025): the advisory sources, matching, findings and export of the
// vulnerability scanning. The defaults are the specification's (proposed). The behaviour is docs/developer/vulnerabilities.md's.
// The four maintenance tasks of the scanning are keys of maintenance.tasks, with their defaults there.
type Vulnerabilities struct {
	Enabled     bool          `yaml:"enabled" default:"true" doc:"false: no sync, no evaluation, no block and no client-audit mode local; the stored findings stay as they are."`
	Enforcement string        `yaml:"enforcement" default:"enforce" validate:"oneof=enforce|report" doc:"enforce refuses the downloads the findings block (R-VULN-015); report blocks nothing anywhere: the would-be blocks are counted in quader_vuln_downloads_blocked_total{mode=\"report\"} and recorded with enforced false. The installation-wide override for an emergency and for measuring a policy before enforcing it."`
	StaleAfter  time.Duration `yaml:"stale_after" default:"24h" validate:"min=1m,max=8760h" doc:"An ecosystem whose last successful sync is older than this is stale: evaluation and enforcement continue on the stored data, the report shows a banner and the alert QuaderVulnerabilityDataStale fires. Staleness never affects readiness."`

	Sources  VulnerabilitySources  `yaml:"sources"`
	Matching VulnerabilityMatching `yaml:"matching"`
	Findings VulnerabilityFindings `yaml:"findings"`
	Images   VulnerabilityImages   `yaml:"images"`
	Export   VulnerabilityExport   `yaml:"export"`
}

// VulnerabilitySources are the advisory sources (R-VULN-003). The image scanning's database is no source of this section: it comes
// from a repository of the installation (images.database, R-VULN-044), and a key below sources.trivy is refused as an unknown key.
type VulnerabilitySources struct {
	OSV  VulnerabilityOSV  `yaml:"osv"`
	KEV  VulnerabilityKEV  `yaml:"kev"`
	EPSS VulnerabilityEPSS `yaml:"epss"`
}

// VulnerabilityImages configures the scanning of container images (R-VULN-033 to R-VULN-036).
type VulnerabilityImages struct {
	Enabled             bool          `yaml:"enabled" default:"true" doc:"false: no image is scanned; a repository's images are not_covered. The findings stay as they are, and the chart renders no scanner."`
	Binary              string        `yaml:"binary" default:"/opt/trivy/trivy" doc:"The analyser: the scanner runs the analysis and the detection with it (R-VULN-048). The default is where the chart's init container copies the binary of the analyser's own image (trivy.image, TRIVY_IMAGE of tools/versions.env) into the read-only volume trivy-bin of each scanner pod (R-HA-070); the release image carries no analyser. Read by the scanner only; the worker runs no tool."`
	ScanDir             string        `yaml:"scan_dir" default:"" doc:"Local directory of the scanner's working files below its spool: the analyses and the detections. Empty (the default) is <storage.spool.dir>/imagescan, so the files lie on the volume whose budget counts them (Q-VULN-184): the compressed size of the image plus 64 MiB per analysis, 64 MiB per detection. Lost with the pod without harm. Read by the scanner only; the worker writes nothing of a scan to its disk."`
	Concurrency         int           `yaml:"concurrency" default:"2" validate:"min=1,max=64" doc:"Calls to the scanner one job has in flight (R-VULN-041). The default scanner capacity is two replicas of one slot; a call above the capacity is put off with scanner_busy."`
	RunSlice            time.Duration `yaml:"run_slice" default:"10m" validate:"min=10s,max=24h" doc:"How long one job keeps its slot on the queue vuln before the rest of the work goes to the next job, so that an advisory sync gets its turn."`
	ScanDelay           time.Duration `yaml:"scan_delay" default:"30s" validate:"min=0,max=1h" doc:"Delay before a new manifest is scanned, so that a pull of a remote image has brought its layers."`
	MaxAttempts         int           `yaml:"max_attempts" default:"5" validate:"min=1,max=100" doc:"Attempts at a scan that fails for a reason that may pass (a layer that has not arrived, an analyser that was killed, a scanner that was lost)."`
	Timeout             time.Duration `yaml:"timeout" default:"15m" validate:"min=10s,max=24h" doc:"Time limit of one step of the analyser in the scanner: the analysis and the detection. The worker's call of a detection has the deadline timeout plus 1m (R-VULN-048)."`
	MemoryLimit         ByteSize      `yaml:"memory_limit" default:"512MiB" validate:"min=64MiB" doc:"Soft limit of the analyser's Go heap. The memory limit of the pod is the hard one: an image that passes the scanner's ends that pod's analysis (scanner_lost)."`
	MaxLayers           int           `yaml:"max_layers" default:"256" validate:"min=1" doc:"Images with more layers are not scanned and show scan_failed."`
	MaxImageSize        ByteSize      `yaml:"max_image_size" default:"3GiB" validate:"min=1MiB" doc:"Largest image scanned, compressed, config and layers together. An analysis holds the image's size plus 64 MiB of the scanner's spool budget while it runs; the chart refuses a scanner spool budget (vulnScannerTrivy.spool.sizeLimit minus spool.headroom) below scanner.slots times this limit plus 64 MiB. A larger image shows scan_failed."`
	MaxUncompressedSize ByteSize      `yaml:"max_uncompressed_size" default:"32GiB" validate:"min=1MiB" doc:"Largest size the layers of an image may decompress to together; a larger image is treated as a decompression bomb."`
	MaxEntries          int64         `yaml:"max_entries" default:"5000000" validate:"min=1" doc:"Largest number of file entries in the layers of an image."`

	// The role scanner and its two links (R-VULN-041, R-VULN-042, ADR-212): vulnerabilities_scanner.go.
	PullTimeout time.Duration            `yaml:"pull_timeout" default:"10m" validate:"min=1m,max=1h" doc:"The scanner's pull of one image through the scan listener, and the lifetime of its scan token (R-OCI-037). The worker's call has the deadline pull_timeout plus timeout plus 1m."`
	Scanner     VulnerabilityScannerLink `yaml:"scanner" doc:"The worker's side of the call to the scanner (R-VULN-041)."`
	Registry    VulnerabilityPullLink    `yaml:"registry" doc:"The scanner's side of the pull from the scan listener of packages (R-VULN-042)."`

	// The analyser's database and the repository it lies in (R-VULN-035, R-VULN-044): vulnerabilities_database.go.
	Database VulnerabilityImageDatabase `yaml:"database" doc:"The analyser's vulnerability database: the repository of this installation it lies in and the image in it (R-VULN-044)."`
}

// VulnerabilityOSV configures the OSV source (R-VULN-003 to R-VULN-006).
type VulnerabilityOSV struct {
	Mode                string        `yaml:"mode" default:"online" validate:"oneof=online|repository|off" doc:"online: the worker role fetches the per-ecosystem archives (R-VULN-004); repository: it reads them from a generic local repository of the installation, for an air-gapped installation (R-VULN-005); off: the stored data stays as it is and ages, and is never deleted by turning the source off."`
	BaseURL             string        `yaml:"base_url" default:"https://osv-vulnerabilities.storage.googleapis.com/" doc:"The root of the OSV export bucket; https only. The sync requests <ecosystem>/all.zip, <ecosystem>/modified_id.csv and <ecosystem>/<id>.json below it, never the global archive."`
	Ecosystems          []string      `yaml:"ecosystems" default:"[]" doc:"OSV ecosystems to import. Empty: every ecosystem of a format that has the vulnerability adapter in this release (npm, NuGet). The list may only narrow that set; an ecosystem without an adapter is refused."`
	Repository          string        `yaml:"repository" default:"" doc:"Mode repository: the qualified name <projectKey>/<repoKey> of a generic local repository that holds osv/<ecosystem>/all.zip (R-VULN-005). Whoever holds deploy on it decides the advisory data of the installation."`
	FullResyncThreshold int           `yaml:"full_resync_threshold" default:"2000" validate:"min=1" doc:"Changed records above which an incremental run turns into a full one."`
	Overlap             time.Duration `yaml:"overlap" default:"1h" validate:"min=0,max=168h" doc:"Subtracted from the time a download started to set the watermark, because the exporter writes the archive and the change file at different times (Q-VULN-11)."`
	MaxShrink           float64       `yaml:"max_shrink" default:"0.2" validate:"min=0,max=1" doc:"The share of an ecosystem's stored records a full sync may delete. A larger shrink fails the run with shrink_refused and changes nothing; a manual run with {\"acceptShrink\": true} overrides it once."`
	Timeout             time.Duration `yaml:"timeout" default:"10m" validate:"min=10s,max=24h" doc:"Timeout of one archive download; a record or a part of the change file has 30 s."`
	MaxArchiveSize      ByteSize      `yaml:"max_archive_size" default:"1GiB" validate:"min=1MiB" doc:"Largest archive accepted. The worker's spool (worker.spool.sizeLimit) must hold one archive plus headroom."`
	MaxRecordSize       ByteSize      `yaml:"max_record_size" default:"8MiB" validate:"min=1KiB,max=64MiB" doc:"Largest advisory record, in the archive uncompressed and fetched singly. A larger record is rejected and counted, and the run goes on (R-VULN-004, Q-VULN-116)."`
}

// VulnerabilityKEV configures the CISA known-exploited catalog (R-VULN-008).
type VulnerabilityKEV struct {
	Mode       string `yaml:"mode" default:"online" validate:"oneof=online|repository|off" doc:"online, repository (kev/known_exploited_vulnerabilities.json in the mirror repository) or off; with off every known-exploited flag stays as it was last computed."`
	URL        string `yaml:"url" default:"https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json" doc:"The catalog; https only."`
	Repository string `yaml:"repository" default:"" doc:"Mode repository: the qualified name <projectKey>/<repoKey> of a generic local repository."`
}

// VulnerabilityEPSS is the key of the P1 exploit-probability enrichment (R-VULN-030, Q-VULN-21). Only off is accepted in
// this release; the key exists so that a configuration that switches it on is refused by name, not read as an unknown key.
type VulnerabilityEPSS struct {
	Mode       string `yaml:"mode" default:"off" validate:"oneof=online|repository|off" doc:"Off, and only off is built in this release (P1). The operator who switches EPSS on decides whether the data's terms permit the installation's use: FIRST publishes the EPSS scores without a stated licence, its site reads \"All Rights Reserved\", and it asks for attribution in products that use the scores; Quader makes no statement about what that permits."`
	URL        string `yaml:"url" default:"https://epss.empiricalsecurity.com/epss_scores-current.csv.gz" doc:"The daily EPSS file; https only. Not fetched while mode is off."`
	Repository string `yaml:"repository" default:"" doc:"Mode repository: the qualified name <projectKey>/<repoKey> of a generic local repository."`
}

// VulnerabilityMatching configures which stored versions public advisories apply to (R-VULN-010).
type VulnerabilityMatching struct {
	PublicUpstreams map[string][]string `yaml:"public_upstreams" default:"{\"npm\":[\"registry.npmjs.org\"],\"nuget\":[\"api.nuget.org\",\"www.nuget.org\"]}" doc:"Per format ID, the hosts of the public registries. A remote repository whose upstream host is listed is matched with matchPublicAdvisories auto; any other remote is not_covered until it sets always. Replaces the whole map when set."`
}

// VulnerabilityFindings configures the history of findings (R-VULN-014).
type VulnerabilityFindings struct {
	ResolvedRetention time.Duration `yaml:"resolved_retention" default:"9600h" validate:"min=1h,max=87600h" doc:"A resolved finding and its events are kept this long (400 days) after it resolved; the task vuln-findings-retention deletes older ones. A finding that is re-detected before that is kept. Go durations have no day unit."`
}

// VulnerabilityExport bounds the findings export (R-VULN-020).
type VulnerabilityExport struct {
	MaxRows int `yaml:"max_rows" default:"100000" validate:"min=1" doc:"An export that would exceed this answers 422 export_too_large before anything is streamed."`
}

// vulnerabilityEcosystems are the OSV ecosystems of the formats that implement the vulnerability adapter in this release
// (R-VULN-003, R-VULN-009), spelled as OSV spells them. The configuration is a leaf and cannot read the format registry,
// so the list is fixed here; the work package that adds a format's adapter adds its ecosystem.
func vulnerabilityEcosystems() []string { return []string{"npm", "NuGet"} }

// VulnerabilityEcosystems returns the OSV ecosystems the running release can import.
func VulnerabilityEcosystems() []string { return vulnerabilityEcosystems() }

// EffectiveEcosystems returns the ecosystems the OSV source imports: sources.osv.ecosystems when it narrows the set,
// else every ecosystem of the release (R-VULN-003).
func (o VulnerabilityOSV) EffectiveEcosystems() []string {
	if len(o.Ecosystems) == 0 {
		return VulnerabilityEcosystems()
	}
	return slices.Clone(o.Ecosystems)
}

// Validate checks the rules of R-VULN-025 across the section's fields: base URLs are https only and a refusal names the
// key (INV-20), ecosystems name only ecosystems of the release, mode repository names a repository, and epss stays off.
func (v *Vulnerabilities) Validate(r *Report) {
	o := v.Sources.OSV
	for _, e := range o.Ecosystems {
		if !slices.Contains(vulnerabilityEcosystems(), e) {
			r.Add("sources.osv.ecosystems", "names %q, which no format of this release has a vulnerability adapter for; the ecosystems are %s", e, strings.Join(vulnerabilityEcosystems(), ", "))
		}
	}
	checkHTTPSURL(r, "sources.osv.base_url", o.BaseURL, o.Mode == "online")
	checkHTTPSURL(r, "sources.kev.url", v.Sources.KEV.URL, v.Sources.KEV.Mode == "online")
	checkHTTPSURL(r, "sources.epss.url", v.Sources.EPSS.URL, v.Sources.EPSS.Mode == "online")
	for _, s := range []struct{ key, mode, repo string }{
		{"sources.osv.repository", o.Mode, o.Repository},
		{"sources.kev.repository", v.Sources.KEV.Mode, v.Sources.KEV.Repository},
		{"sources.epss.repository", v.Sources.EPSS.Mode, v.Sources.EPSS.Repository},
	} {
		if s.mode == "repository" && strings.TrimSpace(s.repo) == "" {
			r.Add(s.key, "is required when the source's mode is repository: it names the generic local repository that holds the files (R-VULN-005)")
		}
		if s.repo != "" {
			if _, err := qname.ParseRepo(s.repo); err != nil {
				r.Add(s.key, "%q is not a repository's qualified name <projectKey>/<repoKey> (%v)", s.repo, err)
			}
		}
	}
	if v.Sources.EPSS.Mode != "off" {
		r.Add("sources.epss.mode", "is %q: the exploit-probability enrichment is not built in this release (R-VULN-030, P1); only off is accepted", v.Sources.EPSS.Mode)
	}
	v.validateScannerLink(r)
	for format, hosts := range v.Matching.PublicUpstreams {
		for _, h := range hosts {
			if strings.TrimSpace(h) == "" || strings.ContainsAny(h, "/ ") {
				r.Add("matching.public_upstreams", "the entry of %q, %q, must be a host name without a scheme or a path", format, h)
			}
		}
	}
}

// checkHTTPSURL refuses a URL that is not an https URL with a host, naming the key and never printing a password.
// A URL of a source that is not read (mode off or repository) is still checked when it is not empty, so a typo does not
// wait for the day the mode changes; an empty one is only refused when the source is online.
func checkHTTPSURL(r *Report, key, raw string, needed bool) {
	if strings.TrimSpace(raw) == "" {
		if needed {
			r.Add(key, "is required while the source is online")
		}
		return
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "":
		r.Add(key, "must be an https URL (is %q)", redactUserinfo(raw))
	case u.Scheme != "https":
		r.Add(key, "%q is not an https URL: advisory data is fetched over TLS only (INV-20)", redactUserinfo(raw))
	}
}
