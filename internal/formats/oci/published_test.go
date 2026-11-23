package oci

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_REPO_048_AnOCIPushRecordsTheGuardsOfItsFirstVersion: a push that gives an image name its first version records package.published
// with the shadowing check's outcome, undetermined here, and the remote members it shadows; a later tag records its own
// package.published without them (R-REPO-048, Q-REPO-431).
func TestR_REPO_048_AnOCIPushRecordsTheGuardsOfItsFirstVersion(t *testing.T) {
	fx := newFixture(t, formattest.HostOptions{Resolver: shadowResolver{check: func(string) (formats.ShadowOutcome, error) {
		return formats.ShadowOutcome{Proceed: true, Undetermined: true, Shadowed: []formats.ShadowPair{{Virtual: "it/all", Remote: "it/hub"}}}, nil
	}}})
	fx.image("app", "1", "one")
	fx.image("app", "2", "two")
	fx.twin.mu.Lock()
	got := append(fx.twin.published[:0:0], fx.twin.published...)
	fx.twin.mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("published %d versions, want 2: %+v", len(got), got)
	}
	first, second := got[0], got[1]
	if first.Version != "1" || !first.ShadowUndetermined || len(first.Shadowed) != 1 || first.Shadowed[0].Remote != "it/hub" || first.Claim == "" {
		t.Errorf("the first version: %+v", first)
	}
	if first.Path != TagPath("app", "1") || first.SHA256 == ([32]byte{}) || first.Size == 0 || first.RepoKey == "" {
		t.Errorf("the first version's file: %+v", first)
	}
	if second.Version != "2" || second.ShadowUndetermined || len(second.Shadowed) != 0 || second.Claim != "" {
		t.Errorf("a later version carries the guards of the first: %+v", second)
	}
}
