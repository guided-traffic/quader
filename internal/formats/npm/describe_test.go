package npm

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/npmtest"
)

// TestR_NPM_050_TheFillOfATarballQueuesTheDescriptionOfItsPackage: the commit of a tarball a remote cached links it to its version and
// queues the job that writes the version's row of npm_versions; the refresh of a stored document queues it only while the remote holds
// a version of the package. Unit, microseconds.
func TestR_NPM_050_TheFillOfATarballQueuesTheDescriptionOfItsPackage(t *testing.T) {
	f := New()
	twin := npmtest.New()
	w := &fakeWriter{}
	path := "react/-/react-18.0.0.tgz"
	ev := formats.CommitEvent{
		Origin: formats.OriginFill, Repo: formats.RepoRef{ID: 9, Name: qr("npm-remote"), Class: formats.Remote, Format: ID},
		Item: formats.ItemRef{ID: 10, Path: path}, Path: formats.Path{Path: path}, Packages: w, Data: twin,
	}
	if err := f.OnCommit(context.Background(), nil, ev); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(w.calls, " "); got != "EnsureLocked Versions InsertVersion BumpRevision" {
		t.Errorf("calls %q", got)
	}
	if !reflect.DeepEqual(twin.Describes, []string{"9/react"}) {
		t.Errorf("queued %v, want the description of react", twin.Describes)
	}

	twin = npmtest.New()
	fill(t, f, twin, upstreamBody("react", "2026-10-30T11:00:00.000Z", "18.0.0"), formats.FillEvent{Digest: [32]byte{1}, FetchedAt: time.Now()})
	if !reflect.DeepEqual(twin.Describes, []string{"?9/react"}) {
		t.Errorf("the refresh of the document queued %v, want the description of what the remote holds of react", twin.Describes)
	}
}
