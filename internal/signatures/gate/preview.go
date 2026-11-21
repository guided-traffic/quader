package gate

import (
	"context"
	"slices"
	"time"
)

// The repository's preview before the gate is switched on (signatures.md R-SIG-072, Q-SIG-90): the gate of a setting applied to the
// repository's stored items in batches, without the wait, bounded by signatures.gate.preview_limit items and a time.

// ItemCursor is the position of a batch of stored items: the repository and the path of the last item read.
type ItemCursor struct {
	RepoID int64
	Path   string
}

// StoredItem is a stored file as a preview reads it.
type StoredItem struct {
	ID     int64
	RepoID int64
	Path   string
	Blob   [32]byte
}

// PreviewRequest is one preview: Repo with the setting to judge (switched on by the caller), Members for a virtual Repo (the members the
// caller may read, whose items it judges), at most Limit items until Deadline.
type PreviewRequest struct {
	Repo     Repo
	Members  []Repo
	Limit    int
	Deadline time.Time
	// Batch is the items one read takes; zero is previewBatch.
	Batch int
}

// Example is a refused artifact a preview shows.
type Example struct {
	Holder Repo
	Path   string
	Reason Reason
	itemID int64
}

// Preview is a preview's answer: the items examined, of them the artifacts, how many pass, are pending and are refused per reason, and up
// to MaxExamples refused artifacts, the newest first. Complete is false when the limit or the time ended the reading: the counts are then
// lower bounds.
type Preview struct {
	Examined  int
	Artifacts int
	Pass      int
	Pending   int
	Refused   map[Reason]int
	Complete  bool
	Examples  []Example
}

// The bounds of a preview: the examples it shows and the items of one batch.
const (
	MaxExamples  = 20
	previewBatch = 500
)

// Preview judges the repository's stored items as Assess does, batch by batch: a remote judges its cache, a virtual repository the items
// of its members the request names (R-SIG-072).
func (g *Gate) Preview(ctx context.Context, req PreviewRequest) (Preview, error) {
	p := Preview{Refused: map[Reason]int{}, Complete: true}
	holders := map[int64]Repo{req.Repo.ID: req.Repo}
	ids := []int64{req.Repo.ID}
	if req.Repo.Class == "virtual" {
		holders, ids = map[int64]Repo{}, nil
		for _, m := range req.Members {
			holders[m.ID] = m
			ids = append(ids, m.ID)
		}
	}
	batch := req.Batch
	if batch <= 0 {
		batch = previewBatch
	}
	if len(ids) == 0 || !g.Enabled() {
		return p, nil
	}
	var after ItemCursor
	for {
		if p.Examined >= req.Limit || (!req.Deadline.IsZero() && g.o.Now().After(req.Deadline)) {
			p.Complete = false
			return p, nil
		}
		n := min(batch, req.Limit-p.Examined)
		items, err := g.o.Store.GateItems(ctx, ids, after, n)
		if err != nil {
			return p, err
		}
		if len(items) == 0 {
			return p, nil
		}
		if err := g.previewBatch(ctx, req, holders, items, &p); err != nil {
			return p, err
		}
		last := items[len(items)-1]
		after = ItemCursor{RepoID: last.RepoID, Path: last.Path}
		if len(items) < n {
			return p, nil
		}
	}
}

func (g *Gate) previewBatch(ctx context.Context, req PreviewRequest, holders map[int64]Repo, items []StoredItem, p *Preview) error {
	in := make([]AssessItem, len(items))
	for i, it := range items {
		in[i] = AssessItem{Holder: holders[it.RepoID], Path: it.Path, Blob: it.Blob}
	}
	got, err := g.Assess(ctx, Assessment{Repo: req.Repo, Members: req.Members, Items: in})
	if err != nil {
		return err
	}
	p.Examined += len(items)
	for i, a := range got {
		if !a.Artifact {
			continue
		}
		p.Artifacts++
		switch a.Outcome {
		case Pass:
			p.Pass++
		case Pending:
			p.Pending++
		default:
			p.Refused[a.Reason]++
			p.addExample(Example{Holder: in[i].Holder, Path: in[i].Path, Reason: a.Reason, itemID: items[i].ID})
		}
	}
	return nil
}

// addExample keeps the MaxExamples refused artifacts stored last (the highest item ids), the newest first.
func (p *Preview) addExample(e Example) {
	p.Examples = append(p.Examples, e)
	slices.SortStableFunc(p.Examples, func(a, b Example) int {
		switch {
		case a.itemID > b.itemID:
			return -1
		case a.itemID < b.itemID:
			return 1
		}
		return 0
	})
	if len(p.Examples) > MaxExamples {
		p.Examples = p.Examples[:MaxExamples]
	}
}
