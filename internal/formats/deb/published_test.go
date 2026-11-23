package deb

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/test/debstub/debgen"
)

// undeterminedShadow lets a first upload proceed although a remote member could not be asked, and names one remote member that
// serves the name, as a caller holding manage meets it (R-REPO-048).
type undeterminedShadow struct{ formattest.Resolver }

func (undeterminedShadow) ShadowCheck(context.Context, *formats.Request, string) (formats.ShadowOutcome, error) {
	return formats.ShadowOutcome{Proceed: true, Undetermined: true, Shadowed: []formats.ShadowPair{{Virtual: "it/apt-all", Remote: "it/debian"}}}, nil
}

// TestR_REPO_048_ADebianUploadRecordsTheGuardsOfItsFirstVersion: an upload that gives the repository a name's first version records
// package.published with the shadowing check's outcome, undetermined here, and the remote members it shadows; a later upload of the name
// records its own package.published without them (R-REPO-048, Q-REPO-431).
func TestR_REPO_048_ADebianUploadRecordsTheGuardsOfItsFirstVersion(t *testing.T) {
	tw := newTwin()
	h := formattest.NewHost(New(), formattest.HostOptions{Data: twinPort{twinStore: tw}, Resolver: undeterminedShadow{}})
	for _, v := range []string{"1.0-1", "1.0-2"} {
		d, err := debgen.Build(debgen.Hello("hello", v, "amd64"))
		if err != nil {
			t.Fatal(err)
		}
		if err := uploadDeb(h, "hello", v, "stable", d.Bytes); err != nil {
			t.Fatal(err)
		}
	}
	tw.mu.Lock()
	got := append(tw.Published[:0:0], tw.Published...)
	tw.mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("published %d files, want 2: %+v", len(got), got)
	}
	first, second := got[0], got[1]
	if first.Package != "hello" || first.Version != "1.0-1" || !first.ShadowUndetermined || len(first.Shadowed) != 1 || first.Claim == "" {
		t.Errorf("the first version: %+v", first)
	}
	if first.Path == "" || first.SHA256 == ([32]byte{}) || first.Size == 0 || first.RepoKey == "" {
		t.Errorf("the first version's file: %+v", first)
	}
	if second.Version != "1.0-2" || second.ShadowUndetermined || len(second.Shadowed) != 0 || second.Claim != "" {
		t.Errorf("a later version carries the guards of the first: %+v", second)
	}
}
