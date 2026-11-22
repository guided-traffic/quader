//go:build e2e

package e2e

// The harness of the end-to-end cases of the delivery by signed links of the object store (storage.md R-STOR-070, R-STOR-071,
// R-STOR-074; docs/developer/download-links.md): one case in the leg of each format whose route declares that its clients check
// a digest (oci, npm, ansible), each with a fresh one-file artifact of kilobytes. The switch is written through the store a
// replica's management API writes through (driver links on|off), the artifact's blob is marked clean so that the malware gate
// passed on a decided state (Q-STOR-284), and its object is overwritten in the bucket behind Quader's back (driver overwrite) to
// show that every client of the leg refuses a link's altered bytes. The caller is the e2e administrator, whose profile Unlimited has
// no hour or day budget that the case's repeated downloads could use up (a speed limit would keep no download from a link, Q-LIM-99).

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// linksOn switches the delivery by signed links on with minSize 0 and switches it off again when the test ends, so that no other
// test of the leg meets it.
func linksOn(t *testing.T) {
	t.Helper()
	if out := driver(t, "links", "on"); !strings.Contains(out, `"enabled": true`) {
		t.Fatalf("driver links on: %s", out)
	}
	t.Cleanup(func() { driver(t, "links", "off") })
}

// blobOf is the SHA-256 in hex of the item at a path below a repository of the project e2e whose path ends in suffix.
func blobOf(t *testing.T, repoKey, suffix string) string {
	t.Helper()
	out := sql(t, `SELECT encode(i.blob_sha256, 'hex') FROM quader.items i JOIN quader.repositories r ON r.id = i.repo_id JOIN quader.project p ON p.id = r.project_id
		WHERE p.key = '`+e2eProject+`' AND r.key = '`+repoKey+`' AND i.kind = 'file' AND i.path LIKE '%`+suffix+`' ORDER BY i.id DESC LIMIT 1`)
	if len(out) != 64 {
		t.Fatalf("no file ending in %s in %s: %q", suffix, repoKey, out)
	}
	return out
}

// markClean gives the blob the verdict clean, the decided state a link needs where the malware scanning is on (Q-STOR-284): the
// case is about the clients' check, not the scanner, whose definitions the dev profile may not have yet.
func markClean(t *testing.T, sha string) {
	t.Helper()
	sql(t, `INSERT INTO quader.malware_scan (sha256, state, holder_repo_id, size, due, scanned_at)
		SELECT i.blob_sha256, 'clean', i.repo_id, i.size, false, now() FROM quader.items i WHERE i.blob_sha256 = decode('`+sha+`', 'hex') LIMIT 1
		ON CONFLICT (sha256) DO UPDATE SET state = 'clean', due = false, scanned_at = now()`)
}

// waitForLink waits until a GET of path with the administrator's token answers 307, which it does once every replica's snapshot
// holds the switch (within 2 s, R-ARCH-018) and the link checks of their starts passed; it returns the link.
func waitForLink(t *testing.T, adm *api, path string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r := adm.get(path)
		if r.Status == http.StatusTemporaryRedirect {
			return r.Header.Get("Location")
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s answers %d, not 307, after 30 s with the switch on (the declined downloads by reason: %s)", path, r.Status, declinedLinks(t))
		}
		time.Sleep(time.Second)
	}
}

// issuedLinks is the number of links the serving replicas issued for a format.
func issuedLinks(t *testing.T, format string) float64 {
	t.Helper()
	return sumSeries(t, "quader_storage_download_links_total", `format="`+format+`"`)
}

// declinedLinks names the declined downloads by reason, for a failure's message.
func declinedLinks(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, reason := range []string{"bastion", "opt_out", "gate", "range", "size", "connection", "credential", "signing"} {
		if n := sumSeries(t, "quader_storage_download_links_declined_total", `reason="`+reason+`"`); n > 0 {
			parts = append(parts, reason+"="+strconv.FormatFloat(n, 'f', -1, 64))
		}
	}
	return strings.Join(parts, ", ")
}
