//go:build integration

package debvirtual

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/debstub"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_003_ADebianRemotePassesItsSignedIndexesAndItsVirtualRepositoryDropsTheRefusedRows: a Debian remote passes its upstream's
// signed indexes through unchanged and refuses the files its rules do not deliver; the filtered view is a virtual repository over it,
// whose merge drops the rows its member's rules refuse. The write of the rule enqueues the build of every virtual repository that lists
// the remote, so the next generation lacks the row without a refresh (remote-curation.md R-CUR-003 point 3, R-DEB-036, Q-CUR-9). Data:
// the stub's archive, a few kilobytes.
func TestR_CUR_003_ADebianRemotePassesItsSignedIndexesAndItsVirtualRepositoryDropsTheRefusedRows(t *testing.T) {
	e := newEnv(t, options{})
	testenv.WaitFor(t, "the virtual index to list quader-hello", 90*time.Second, func() bool {
		if s, ok := e.suite(rApt, "stable"); !ok || s.Generation == 0 {
			return false
		}
		_, found := find(e.packages(rApt, "stable", e.admin), "quader-hello", "")
		return found
	})
	hello, _ := find(e.packages(rApt, "stable", e.admin), "quader-hello", "")
	file, ok := strings.CutPrefix(hello.Value("Filename"), "members/"+rRemote+"/")
	if !ok {
		t.Fatalf("the Filename of quader-hello is %q, want one below members/%s/", hello.Value("Filename"), rRemote)
	}
	e.st.MustPutDeliveryRule(e.r, e.admin, rRemote, testenv.PackageRule("quader-hello", "block"))
	e.st.WaitDelivered(t, e.r, e.admin, rRemote, "quader-hello", "", false)

	t.Run("R-CUR-003 the remote's InRelease is the upstream's, byte for byte", func(t *testing.T) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, e.srv.URL+debstub.ArchiveBase+"/dists/stable/InRelease", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		want, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("the stub's InRelease: %d %v", res.StatusCode, err)
		}
		if a := e.get(rRemote, "dists/stable/InRelease", e.alice); a.Status != http.StatusOK || !bytes.Equal(a.Body, want) {
			t.Errorf("the remote's InRelease: %d, %d bytes, equal %v", a.Status, len(a.Body), bytes.Equal(a.Body, want))
		}
	})
	t.Run("R-CUR-003 R-CUR-004 the refused package's file answers 403", func(t *testing.T) {
		a := e.get(rRemote, file, e.alice)
		if a.Status != http.StatusForbidden || codeOf(a) != "not_allowed_in_repository" || a.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %q %s, want the rules' 403", file, a.Status, a.Header.Get("Cache-Control"), a.Body)
		}
	})
	t.Run("R-CUR-003 R-DEB-036 the virtual repository's Packages lacks the refused row and keeps the others", func(t *testing.T) {
		testenv.WaitFor(t, "the virtual index without quader-hello", 30*time.Second, func() bool {
			_, found := find(e.packages(rApt, "stable", e.admin), "quader-hello", "")
			return !found
		})
		if _, ok := find(e.packages(rApt, "stable", e.admin), "libquader1", ""); !ok {
			t.Error("the merge lost a row the rules deliver")
		}
	})
}
