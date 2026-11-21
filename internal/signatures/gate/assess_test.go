package gate

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

var (
	blobC = [32]byte{0xc}
	blobD = [32]byte{0xd}
	// filesOther is a second gated repository that holds the same bytes.
	filesOther = Repo{
		ID: 8, Name: qname.Repo{Project: "web", Key: "files-other"}, Class: "local", Format: "generic", ProjectID: "p1",
		Setting: Setting{OnlyVerified: true},
	}
)

// classifyByPath names an item by its path, as a format would: "*.asc" a signature file, "layer" no artifact, "platform" an artifact
// covered by blobD (an index), everything else an artifact.
func classifyByPath(_ context.Context, _ Repo, items []Item) ([]Subject, error) {
	out := make([]Subject, len(items))
	for i, it := range items {
		switch it.Path {
		case "tools/a.bin.asc":
			out[i] = Subject{Kind: KindSignatureFile}
		case "layer":
			out[i] = Subject{Kind: KindNone}
		case "platform":
			out[i] = Subject{Kind: KindArtifact, Covering: [][32]byte{blobD}}
		default:
			out[i] = Subject{Kind: KindArtifact}
		}
	}
	return out, nil
}

// TestR_SIG_070_AReleasePassesInItsRepositoryAlone: a release of the gate's repository passes an unsigned artifact at the download and in
// the API's judgement, whatever its signatures say (a blocked signer's too); the same bytes stay refused in another gated repository; a
// released index passes its platform manifest; a download whose signatures pass reads no release. Unit, milliseconds, rows built by the
// test.
func TestR_SIG_070_AReleasePassesInItsRepositoryAlone(t *testing.T) {
	st := &fakeStore{released: map[int64]map[[32]byte]bool{7: {blobA: true, blobD: true}}}
	g := newGate(st, &recorder{}, Options{Classify: classifyByPath})
	ctx := context.Background()
	decide := func(r Repo, path string, blob [32]byte) *Verdict {
		t.Helper()
		d := dl(r)
		d.Path, d.Blob = path, blob
		v, err := g.Decide(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := decide(filesInternal, "tools/a.bin", blobA); v.Outcome != Pass || !v.Released {
		t.Errorf("released in its repository: %+v", v)
	}
	if v := decide(filesOther, "tools/a.bin", blobA); v.Outcome != Refused || v.Reason != ReasonMissing {
		t.Errorf("the same bytes in another gated repository: %+v", v)
	}
	if v := decide(filesInternal, "platform", blobC); v.Outcome != Pass || !v.Released {
		t.Errorf("a platform manifest of a released index: %+v", v)
	}
	st.set(valid(blobA, TrustBlocked))
	if v := decide(filesInternal, "tools/a.bin", blobA); v.Outcome != Pass || !v.Released {
		t.Errorf("a released artifact of a blocked signer: %+v", v)
	}
	signed := valid(blobB, TrustInternal, 3)
	signed.ID = 42
	st.set(signed)
	st.released = nil
	if v := decide(filesInternal, "tools/b.bin", blobB); v.Outcome != Pass || v.Released || v.Signature != 42 {
		t.Errorf("a signed artifact: %+v", v)
	}
	got, err := g.Assess(ctx, Assessment{Repo: filesInternal, Items: []AssessItem{
		{Holder: filesInternal, Path: "tools/b.bin", Blob: blobB},
		{Holder: filesInternal, Path: "layer", Blob: blobC},
		{Holder: filesInternal, Path: "tools/c.bin", Blob: blobC},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Artifact || got[0].Outcome != Pass || got[0].Signature != 42 || got[1].Artifact || got[2].Outcome != Refused {
		t.Errorf("Assess = %+v", got)
	}
}

// TestR_SIG_072_ThePreviewJudgesTheStoredItemsInBatches: four files read in batches of two, one signed by the release key (internal), one
// by the configuration's anchor, one unsigned and a signature file whose scheme was read: with any anchor two pass and one is missing,
// with only the release key's anchor chosen one passes, one is not_chosen and one is missing; the newest refused artifact comes first;
// a limit below the items answers complete false with lower counts; a virtual repository previews its members' items over their
// attachments. Unit, milliseconds.
func TestR_SIG_072_ThePreviewJudgesTheStoredItemsInBatches(t *testing.T) {
	st := &fakeStore{
		read: map[[32]byte]bool{blobD: true},
		items: []StoredItem{
			{ID: 1, RepoID: 7, Path: "tools/a.bin", Blob: blobA},
			{ID: 4, RepoID: 7, Path: "tools/a.bin.asc", Blob: blobD},
			{ID: 2, RepoID: 7, Path: "tools/b.bin", Blob: blobB},
			{ID: 3, RepoID: 7, Path: "tools/c.bin", Blob: blobC},
		},
	}
	st.set(valid(blobA, TrustInternal, 3), valid(blobB, TrustInternal, 2))
	g := newGate(st, &recorder{}, Options{Classify: classifyByPath})
	ctx := context.Background()
	anyAnchor, err := g.Preview(ctx, PreviewRequest{Repo: filesInternal, Limit: 100, Batch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if anyAnchor.Examined != 4 || anyAnchor.Artifacts != 3 || anyAnchor.Pass != 2 || anyAnchor.Refused[ReasonMissing] != 1 || !anyAnchor.Complete {
		t.Errorf("any anchor: %+v", anyAnchor)
	}
	chosen := filesInternal
	chosen.Setting.Anchors = []string{"installation/release-key"}
	only, err := g.Preview(ctx, PreviewRequest{Repo: chosen, Limit: 100, Batch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if only.Pass != 1 || only.Refused[ReasonNotChosen] != 1 || only.Refused[ReasonMissing] != 1 {
		t.Errorf("only the release key: %+v", only)
	}
	if len(only.Examples) != 2 || only.Examples[0].Path != "tools/c.bin" || only.Examples[1].Reason != ReasonNotChosen {
		t.Errorf("examples: %+v", only.Examples)
	}
	cut, err := g.Preview(ctx, PreviewRequest{Repo: filesInternal, Limit: 2, Batch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if cut.Complete || cut.Examined != 2 {
		t.Errorf("at the limit: %+v", cut)
	}
	// A virtual repository previews the items of the members it is given, and its gate reads their attachments.
	st.items = append(st.items, StoredItem{ID: 5, RepoID: 8, Path: "tools/e.bin", Blob: blobC})
	virtual := Repo{
		ID: 9, Name: qname.Repo{Project: "web", Key: "files-all"}, Class: "virtual", Format: "generic", ProjectID: "p1",
		Setting: Setting{OnlyVerified: true},
	}
	all, err := g.Preview(ctx, PreviewRequest{Repo: virtual, Members: []Repo{filesInternal, filesOther}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	last := st.rowRepos[len(st.rowRepos)-1]
	if all.Examined != 5 || all.Refused[ReasonMissing] != 2 || len(last) != 2 || last[0] != 7 || last[1] != 8 || all.Examples[0].Holder.ID != 8 {
		t.Errorf("a virtual repository: %+v, rows read in %v", all, last)
	}
}

// TestR_SIG_071_TheImpactCountsWhatStopsPassing: removing the release key's anchor makes the artifact it alone knows stop passing in both
// gated repositories that hold it; one also verified by another anchor, and one released in its repository, are not counted; a block of
// the key's fingerprint counts every artifact of the signer, the one another anchor knows too, and asks for the key's spellings; a change
// of material is an upper bound; switching project anchors off counts what only a project anchor knew; an unknown anchor counts nothing.
// Unit, milliseconds.
func TestR_SIG_071_TheImpactCountsWhatStopsPassing(t *testing.T) {
	alone, both, released := valid(blobA, TrustInternal, 3), valid(blobB, TrustInternal, 3, 2), valid(blobC, TrustInternal, 3)
	team := valid(blobD, TrustInternal, 4)
	team.KeyIDs = []string{"TEAMKEY"}
	st := &fakeStore{
		released: map[int64]map[[32]byte]bool{7: {blobC: true}},
		candidates: []Candidate{
			{RepoID: 7, Subject: blobA},
			{RepoID: 7, Subject: blobB},
			{RepoID: 7, Subject: blobC},
			{RepoID: 8, Subject: blobA},
			{RepoID: 7, Subject: blobD},
		},
	}
	st.set(alone, both, released, team)
	g := newGate(st, &recorder{}, Options{})
	ctx := context.Background()
	gated := []Gated{{Repo: filesInternal}, {Repo: filesOther}}
	count := func(ch Change) Impact {
		t.Helper()
		imp, err := g.Impact(ctx, ch, gated, 100)
		if err != nil {
			t.Fatal(err)
		}
		return imp
	}
	if imp := count(Change{Anchor: 3, AnchorChange: AnchorRemove}); imp.Artifacts != 2 || imp.Repositories != 2 || imp.Upper || !imp.Complete {
		t.Errorf("remove the release key: %+v", imp)
	}
	block := count(Change{Block: &Block{Kind: signatures.BlockFingerprint, Value: "abcdef0123456789"}})
	if block.Artifacts != 3 {
		t.Errorf("block the key: %+v", block)
	}
	if q := st.impactQueries[len(st.impactQueries)-1]; len(q.KeyIDs) != 2 {
		t.Errorf("the key's spellings: %v", q.KeyIDs)
	}
	if imp := count(Change{Anchor: 3, AnchorChange: AnchorNarrow}); !imp.Upper || imp.Artifacts != 2 {
		t.Errorf("narrow the release key: %+v", imp)
	}
	if imp := count(Change{ProjectAnchorsOff: true}); imp.Artifacts != 1 || imp.ByRepository[0].Name != filesInternal.Name {
		t.Errorf("project anchors off: %+v", imp)
	}
	if imp := count(Change{Anchor: 99, AnchorChange: AnchorRemove}); imp.Artifacts != 0 {
		t.Errorf("an unknown anchor: %+v", imp)
	}
	if imp, _ := g.Impact(ctx, Change{Anchor: 3, AnchorChange: AnchorRemove}, gated, 2); imp.Complete {
		t.Errorf("beyond the limit: %+v", imp)
	}
}
