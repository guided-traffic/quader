// Command trivydbfixture writes the fixture database of the fidelity test of the rewritten analysis (vulnerability-scanning.md R-VULN-043,
// Q-VULN-176): a database of the analyser's schema version 2 with two advisories for Alpine 3.12, a few kilobytes, which
// TestQ_VULN_164_TheRewrittenAnalysisDetectsWhatTheAnalysersOwnDoes in internal/vuln/imagescan copies into the analyser's cache directory.
// It is run once and its output committed beside the test:
//
//	go run ./internal/tools/trivydbfixture -out internal/vuln/imagescan/testdata/analyser/db
//
// The layout is the one the analyser reads with --skip-db-update: <out>/trivy.db, a bbolt file whose bucket "alpine 3.12" holds a bucket
// per source package with one key per advisory (its fixed version), whose bucket "vulnerability" holds the advisories' details, and whose
// bucket "data-source" names the source of the platform; and <out>/metadata.json with the schema version. It is a developer tool, not part
// of the product, and the only user of go.etcd.io/bbolt.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// platform is the bucket of the advisories of Alpine 3.12, the release of the test's image.
const platform = "alpine 3.12"

// advisory is one advisory of the fixture: the source package it affects, the version that fixes it, and its details.
type advisory struct {
	id, pkg, fixed string
	detail         map[string]any
}

// advisories are the fixture's two advisories. The test's image holds four packages; these two name two of them below their fixed
// version, so the detection reports two findings and lists four packages.
var advisories = []advisory{
	{
		id: "CVE-2020-28928", pkg: "musl", fixed: "1.1.24-r10",
		detail: map[string]any{
			"Title":          "musl: wcsnrtombs function can overflow a buffer",
			"Description":    "In musl libc through 1.2.1, wcsnrtombs mishandles particular combinations of destination buffer size and source character limit.",
			"Severity":       "MEDIUM",
			"VendorSeverity": map[string]any{"nvd": 2},
			"CVSS": map[string]any{"nvd": map[string]any{
				"V3Vector": "CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:N/I:N/A:H", "V3Score": 5.5,
			}},
			"CweIDs":           []string{"CWE-787"},
			"References":       []string{"https://www.openwall.com/lists/oss-security/2020/11/20/4"},
			"PublishedDate":    "2020-11-24T18:15:00Z",
			"LastModifiedDate": "2023-11-07T03:21:00Z",
		},
	},
	{
		id: "CVE-2021-28831", pkg: "busybox", fixed: "1.31.1-r20",
		detail: map[string]any{
			"Title":          "busybox: invalid free or segmentation fault via malformed gzip data",
			"Description":    "decompress_gunzip.c in BusyBox through 1.32.1 mishandles the error bit on the huft_build result pointer.",
			"Severity":       "HIGH",
			"VendorSeverity": map[string]any{"nvd": 3},
			"CVSS": map[string]any{"nvd": map[string]any{
				"V3Vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H", "V3Score": 7.5,
			}},
			"CweIDs":           []string{"CWE-755"},
			"References":       []string{"https://git.busybox.net/busybox/commit/?id=f25d254dfd4243698c31a4f3153d4ac72aa9e9bd"},
			"PublishedDate":    "2021-03-19T05:15:00Z",
			"LastModifiedDate": "2023-11-07T03:32:00Z",
		},
	},
}

func main() {
	out := flag.String("out", "internal/vuln/imagescan/testdata/analyser/db", "the directory to write trivy.db and metadata.json to")
	flag.Parse()
	if err := write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "trivydbfixture:", err)
		os.Exit(1)
	}
}

func write(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, "trivy.db")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return err
	}
	err = db.Update(fill)
	if cerr := db.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// The dates lie in the past and NextUpdate too: the analyser reads the database as it is with --skip-db-update and never fetches.
	meta, err := json.Marshal(map[string]any{
		"Version": 2, "NextUpdate": "2026-11-21T00:00:00Z", "UpdatedAt": "2026-11-20T00:00:00Z", "DownloadedAt": "2026-11-20T00:00:00Z",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "metadata.json"), append(meta, '\n'), 0o600)
}

func fill(tx *bolt.Tx) error {
	plat, err := tx.CreateBucket([]byte(platform))
	if err != nil {
		return err
	}
	details, err := tx.CreateBucket([]byte("vulnerability"))
	if err != nil {
		return err
	}
	for _, a := range advisories {
		pkg, err := plat.CreateBucketIfNotExists([]byte(a.pkg))
		if err != nil {
			return err
		}
		if err := putJSON(pkg, a.id, map[string]any{"FixedVersion": a.fixed}); err != nil {
			return err
		}
		if err := putJSON(details, a.id, a.detail); err != nil {
			return err
		}
	}
	sources, err := tx.CreateBucket([]byte("data-source"))
	if err != nil {
		return err
	}
	return putJSON(sources, platform, map[string]any{"ID": "alpine", "Name": "Alpine Secdb", "URL": "https://secdb.alpinelinux.org/"})
}

func putJSON(b *bolt.Bucket, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.Put([]byte(key), raw)
}
