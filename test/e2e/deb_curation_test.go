//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/debstub"
)

// TestDebCuration_R_CUR_004_WhatAptShowsOfARemoteThatDeliversOnlyWhatIsAllowed is APT's one case of the curation of a remote repository
// (remote-curation.md R-CUR-022): what only the real client shows. The rules' decision, the virtual repository's filtered index, the
// queue and the requests are the unit tests', the kit's and the integration areas debremote's and debvirtual's. A remote of the counting
// Debian archive test upstream in mode onlyAllowed, APT 3.0 of Debian 13, the stub's packages of about a kilobyte:
//
//   - apt-get update succeeds and apt lists the package: the remote passes its upstream's signed indexes unchanged (Q-CUR-9); the
//     install fails with a bare 403, since the protocol carries no reason (R-CUR-004, gap H-1726);
//   - after an allow of quader-config 1.0-1 through the API, the install succeeds (R-CUR-003);
//   - quader-hello-doc 1.0-1 downloaded ahead through the queue (stored for examination, R-CUR-014) and then allowed with the package is
//     installed from the cache: the upstream served its package file once, to the download (R-CUR-010).
func TestDebCuration_R_CUR_004_WhatAptShowsOfARemoteThatDeliversOnlyWhatIsAllowed(t *testing.T) {
	fx := debFx(t)
	defer caseClock(t)()
	s := debStub(t)
	repo := fx.remoteRepo(t, "curation", debstub.ArchiveBase+"/", onlyAllowed())
	c := fx.client(t, aptDebian13)
	c.configure(repo)
	c.mustUpdate()
	// the path of a package file below the repository and below the stub, by the archive's pool layout
	pool := func(source, name, version string) string {
		return "/pool/main/" + source[:1] + "/" + source + "/" + name + "_" + version + "_all.deb"
	}
	served := func(r reply) bool { return r.Status == 200 }

	t.Run("R-CUR-004 apt lists the package from the upstream's index and fails its download with 403", func(t *testing.T) {
		if out := c.must("apt-cache policy quader-config"); !strings.Contains(out, "Candidate: 1.0-1") {
			t.Fatalf("apt does not list quader-config from the remote's index:\n%s", out)
		}
		start := time.Now()
		out, err := c.install("quader-config")
		noteCuration(t, out, clientCuration{Client: "apt-get install (APT 3.0)", Printed: firstLineWith(out, "403"), Exit: exitOf(err), Seconds: time.Since(start).Seconds()})
		if err == nil || !strings.Contains(out, "403  Forbidden") {
			t.Fatalf("apt-get install of a package the rules refuse: %v, want the download refused with 403:\n%s", err, tailLines(out, 20))
		}
	})

	t.Run("R-CUR-003 after an allow of quader-config 1.0-1 the install succeeds", func(t *testing.T) {
		allowRule(t, fx.adm, repo, "quader-config", "1.0-1")
		awaitOnEveryPackagesReplica(t, fx.token, "the package file of quader-config 1.0-1 is served", debRepoPath(repo)+pool("quader-config", "quader-config", "1.0-1"), served)
		if out, err := c.install("quader-config"); err != nil {
			t.Fatalf("apt-get install quader-config after the allow: %v\n%s", err, tailLines(out, 20))
		}
		if out := c.must(`dpkg-query -W -f '${Version}\n' quader-config`); trimLine(out) != "1.0-1" {
			t.Fatalf("quader-config is installed in version %q, want 1.0-1", trimLine(out))
		}
	})

	t.Run("R-CUR-010 R-CUR-014 quader-hello-doc 1.0-1 downloaded ahead and then allowed is installed from the cache", func(t *testing.T) {
		file := pool("quader-hello", "quader-hello-doc", "1.0-1")
		// a download ahead names its files from the indexes the remote has parsed (R-DEB-023, R-DEB-036): APT read them by their hash at
		// the update above, which enqueued their parse (Q-CUR-162). The rows are read in the database, since the catalogue caches its
		// answers for a while (R-CUR-008) and an answer read before the parse would be the one it keeps.
		parsed := fmt.Sprintf(`SELECT count(*) FROM deb_remote_entries e JOIN repositories r ON r.id = e.repo_id
			WHERE r.key = '%s' AND e.package = 'quader-hello-doc' AND e.version = '1.0-1'`, repo)
		waitFor(t, "the remote's parsed indexes to list quader-hello-doc 1.0-1", time.Minute, 250*time.Millisecond, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			out, err := bakSQL(ctx, parsed)
			fields := strings.Fields(out)
			return err == nil && len(fields) > 0 && fields[len(fields)-1] != "0"
		})
		before := s.debCounts(t).Requests(http.MethodGet, debstub.ArchiveBase+file)
		downloadAhead(t, fx.adm, repo, "quader-hello-doc", "1.0-1")
		if n := s.debCounts(t).Requests(http.MethodGet, debstub.ArchiveBase+file) - before; n != 1 {
			t.Fatalf("the download ahead asked the upstream %d times for %s, want once", n, file)
		}
		allowRule(t, fx.adm, repo, "quader-hello-doc", "")
		awaitOnEveryPackagesReplica(t, fx.token, "the package file of quader-hello-doc 1.0-1 is served", debRepoPath(repo)+file, served)
		if out, err := c.install("quader-hello-doc"); err != nil {
			t.Fatalf("apt-get install quader-hello-doc after the allow: %v\n%s", err, tailLines(out, 20))
		}
		if n := s.debCounts(t).Requests(http.MethodGet, debstub.ArchiveBase+file) - before; n != 1 {
			t.Fatalf("the upstream served %s %d times, want once (the download ahead): the install did not come from the cache", file, n)
		}
	})
}
