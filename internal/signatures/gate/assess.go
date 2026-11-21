package gate

import (
	"context"
	"slices"
)

// The gate judged over stored items without a wait (signatures.md R-SIG-072, R-SIG-073; stage 3, package 2): the answers of the API (the
// members refused and gate, the preview before the gate is switched on, the check of a release) ask what one repository's gate decides
// now, in batches, with the same classification, rows and decision as a download, and record nothing.

// Assessment is one repository's gate over stored items.
type Assessment struct {
	// Repo is the gate judged: its Setting is applied as given (the preview and a release judge a setting switched on). Its view is the
	// repository itself, or for a virtual repository Members.
	Repo    Repo
	Members []Repo
	Items   []AssessItem
}

// AssessItem is one stored item: the repository that holds it (Repo itself, or a member of a virtual Repo), its path and its blob.
type AssessItem struct {
	Holder Repo
	Path   string
	Blob   [32]byte
}

// Assessed is the gate's decision of one item. Artifact is false for an item the format names no artifact (an attachment, a layer, a
// signature file whose scheme was read): it passes as it is.
type Assessed struct {
	Judgement
	Artifact bool
}

// maxDigestsPerRead bounds the subjects of one read of rows, so that a batch is several statements of a bounded size.
const maxDigestsPerRead = 1000

// Assess decides the gate of a.Repo over the items (R-SIG-062 to R-SIG-066, R-SIG-070): the formats classify them, the rows of the view
// and the scope decide each artifact, and a release of the repository passes one whose signatures do not. It waits for nothing and
// records nothing; a pending artifact answers Pending. Without the signatures every item passes.
func (g *Gate) Assess(ctx context.Context, a Assessment) ([]Assessed, error) {
	out := make([]Assessed, len(a.Items))
	if !g.Enabled() || len(a.Items) == 0 {
		return out, nil
	}
	digests, err := g.classifyAll(ctx, a.Items)
	if err != nil {
		return nil, err
	}
	snap, err := g.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	vw := viewOf(a.Repo, a.Members)
	var all [][32]byte
	for i, ds := range digests {
		out[i].Artifact = ds != nil
		all = append(all, ds...)
	}
	all = unique(all)
	rows, fetches, err := g.read(ctx, vw, all)
	if err != nil {
		return nil, err
	}
	bySubject := map[[32]byte][]Row{}
	for _, r := range rows {
		bySubject[r.Subject] = append(bySubject[r.Subject], r)
	}
	var chosen map[int64]bool
	if !vw.any {
		chosen = snap.Chosen(vw.chosen, vw.repo.ProjectID)
	}
	var notPassing [][32]byte
	for i, ds := range digests {
		if ds == nil {
			continue
		}
		var own []Row
		for _, d := range ds {
			own = append(own, bySubject[d]...)
		}
		out[i].Judgement = Judge(snap, chosen, ds, own, fetches, false)
		if out[i].Outcome != Pass {
			notPassing = append(notPassing, ds...)
		}
	}
	if len(notPassing) == 0 {
		return out, nil
	}
	released, err := g.o.Store.GateReleased(ctx, a.Repo.ID, unique(notPassing))
	if err != nil {
		return nil, err
	}
	for i, ds := range digests {
		if ds == nil || out[i].Outcome == Pass {
			continue
		}
		if slices.ContainsFunc(ds, func(d [32]byte) bool { return released[d] }) {
			out[i].Judgement = Judgement{Outcome: Pass, Released: true}
		}
	}
	return out, nil
}

// classifyAll asks the formats about the items, one call per holding repository, and answers each item's digests to judge: its blob and
// the digests that cover it, nil for an item that passes as it is.
func (g *Gate) classifyAll(ctx context.Context, items []AssessItem) ([][][32]byte, error) {
	subs := make([]Subject, len(items))
	for i := range subs {
		subs[i] = Subject{Kind: KindArtifact}
	}
	if g.o.Classify != nil {
		byHolder := map[int64][]int{}
		var order []int64
		for i, it := range items {
			if _, ok := byHolder[it.Holder.ID]; !ok {
				order = append(order, it.Holder.ID)
			}
			byHolder[it.Holder.ID] = append(byHolder[it.Holder.ID], i)
		}
		for _, id := range order {
			idx := byHolder[id]
			batch := make([]Item, len(idx))
			for j, i := range idx {
				batch[j] = Item{Path: items[i].Path, Blob: items[i].Blob}
			}
			got, err := g.o.Classify(ctx, items[idx[0]].Holder, batch)
			if err != nil {
				return nil, err
			}
			if len(got) != len(batch) {
				continue // fail closed: every item of the batch stays an artifact
			}
			for j, i := range idx {
				subs[i] = got[j]
			}
		}
	}
	var sigFiles [][32]byte
	for i, s := range subs {
		if s.Kind == KindSignatureFile {
			sigFiles = append(sigFiles, items[i].Blob)
		}
	}
	read := map[[32]byte]bool{}
	if len(sigFiles) > 0 {
		var err error
		if read, err = g.o.Store.GateObjectsRead(ctx, unique(sigFiles), g.o.MaxObjectSize); err != nil {
			return nil, err
		}
	}
	out := make([][][32]byte, len(items))
	for i, s := range subs {
		switch s.Kind {
		case KindNone, KindAttachment:
			continue
		case KindSignatureFile:
			if read[items[i].Blob] {
				continue
			}
			s.Covering = nil
		}
		out[i] = append([][32]byte{items[i].Blob}, s.Covering...)
	}
	return out, nil
}

// viewOf is the one gate an assessment judges: the repository's, over itself or for a virtual repository its members.
func viewOf(r Repo, members []Repo) view {
	v := view{repo: r, chosen: r.Setting.Anchors, any: len(r.Setting.Anchors) == 0}
	seen := []Repo{r}
	if r.Class == "virtual" {
		seen = members
	}
	for _, m := range seen {
		v.repoIDs = append(v.repoIDs, m.ID)
		if m.Class == classRemote {
			v.remotes = append(v.remotes, m.ID)
		}
	}
	return v
}

// read reads the rows and the fetch states of a view for the digests, in reads of a bounded number of subjects.
func (g *Gate) read(ctx context.Context, vw view, digests [][32]byte) ([]Row, []Fetch, error) {
	var rows []Row
	var fetches []Fetch
	for start := 0; start < len(digests); start += maxDigestsPerRead {
		part := digests[start:min(start+maxDigestsPerRead, len(digests))]
		got, err := g.o.Store.GateRows(ctx, vw.repo.ProjectID, vw.repoIDs, part)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, got...)
		if len(vw.remotes) > 0 {
			f, err := g.o.Store.GateFetches(ctx, vw.remotes, part)
			if err != nil {
				return nil, nil, err
			}
			fetches = append(fetches, f...)
		}
	}
	return rows, fetches, nil
}

// unique keeps the first of equal digests.
func unique(ds [][32]byte) [][32]byte {
	seen := make(map[[32]byte]bool, len(ds))
	out := ds[:0:0]
	for _, d := range ds {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}
