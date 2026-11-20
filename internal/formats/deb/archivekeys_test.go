package deb

import (
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/openpgp"
)

// TestQ_REPO_379_TheArchiveKeys holds the archive keys the Debian and Ubuntu presets ship (R-REPO-062, Q-REPO-379): each keyring parses
// as deb.upstreamKeys parses a remote's keys, holds exactly the keys whose fingerprints are pinned here, and no key is within 90 days
// of its expiry, so a release ships a keyring package's newer keys before an archive stops verifying. The fingerprints are those of
// ftp-master.debian.org/keys.html and of keyserver.ubuntu.com (docs/developer/deb.md, The archive keys of the presets).
func TestQ_REPO_379_TheArchiveKeys(t *testing.T) {
	const warnBefore = 90 * 24 * time.Hour
	now := time.Now()
	for _, tc := range []struct {
		name    string
		armored string
		want    []string
	}{
		{"debian-archive-keyring 2025.1", debianArchiveKeyring, []string{
			"1F89983E0081FDE018F3CC9673A4F27B8DD47936", // Debian Archive Automatic Signing Key (11/bullseye)
			"AC530D520F2F3269F5E98313A48449044AAD5C5D", // Debian Security Archive Automatic Signing Key (11/bullseye)
			"A4285295FC7B1A81600062A9605C66F00D6C9793", // Debian Stable Release Key (11/bullseye)
			"4D64FEC119C2029067D6E791F8D2585B8783D481", // Debian Stable Release Key (12/bookworm)
			"B8B80B5B623EAB6AD8775C45B7C5D7D6350947F8", // Debian Archive Automatic Signing Key (12/bookworm)
			"05AB90340C0C5E797F44A8C8254CF3B5AEC0A8F0", // Debian Security Archive Automatic Signing Key (12/bookworm)
			"04B54C3CDCA79751B16BC6B5225629DF75B188BD", // Debian Archive Automatic Signing Key (13/trixie)
			"5E04A1E3223A19A20706E20F9904613D4CCE68C6", // Debian Security Archive Automatic Signing Key (13/trixie)
			"41587F7DB8C774BCCF131416762F67A0B2C39DE4", // Debian Stable Release Key (13/trixie)
		}},
		{"ubuntu-keyring 2023.11.28.1", ubuntuArchiveKeyring, []string{
			"790BC7277767219C42C86F933B4FE6ACC0B21F32", // Ubuntu Archive Automatic Signing Key (2012)
			"843938DF228D22F7B3742BC0D94AA3F0EFE21092", // Ubuntu CD Image Automatic Signing Key (2012)
			"F6ECB3762474EDA9D21B7022871920D1991BC93C", // Ubuntu Archive Automatic Signing Key (2018)
		}},
	} {
		t.Run("Q-REPO-379 "+tc.name, func(t *testing.T) {
			certs, err := openpgp.ParseCertificates([]byte(tc.armored), now, openpgp.Limits{})
			if err != nil {
				t.Fatalf("the keys do not parse as deb.upstreamKeys: %v", err)
			}
			var got []string
			for _, c := range certs {
				got = append(got, c.Fingerprint)
				if !c.Expires.IsZero() && c.Expires.Sub(now) < warnBefore {
					t.Errorf("%s (%s) expires on %s, within 90 days: ship the keyring package's newer keys", c.Fingerprint, c.UserID,
						c.Expires.Format(time.DateOnly))
				}
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tc.want))
			if !slices.Equal(got, want) {
				t.Errorf("fingerprints\n got %v\nwant %v", got, want)
			}
		})
	}
}
