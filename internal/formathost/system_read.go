package formathost

import (
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/formats"
)

// systemRead reports whether a request is a read of a system subject: the image scanning's pull with a scan token on the scan
// listener (formats/oci.md R-OCI-037, Q-OCI-230). Such a request is no delivery: the host asks no block gate of the vulnerability
// scanning (RefuseImage and the block of a stored version), counts no download and moves no lastDownloadedAt (R-STOR-015,
// R-API-079), records no download event, and is no probe (R-REPO-065). The decision point has already confined it to read of the
// one repository its token names, and never to fill (R-AUTHZ-015).
func systemRead(rq *formats.Request) bool {
	return rq != nil && rq.Subject.Kind == principal.KindSystem
}
