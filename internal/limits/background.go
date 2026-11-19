package limits

import (
	"context"

	"github.com/guided-traffic/quader/internal/qname"
)

// The charge of a background download that a probe started (R-LIM-037, repositories-and-proxy.md R-REPO-065). The decision is
// taken on the probe's request, on the replica that answered it: step 9a for the probe's caller at that moment. The download
// itself is a job of the worker, often on another replica, so what it is charged to travels with the job as a [Ticket], and the
// worker rebuilds the charge from it with [Engine.Resume] (Q-LIM-54).

// Ticket is the charge of a background download as the job carries it: the probe's subject, place, budget key and mode, the
// remote's ceiling the uncached bytes count against, and the budget key's effective figures, which the usage rows record. It
// holds no secret: the subject's reference is a principal ID, a technical token's ID, a CI project or an address prefix
// (R-LIM-001). The zero Ticket is a download that is charged to nobody (a replica without the limits).
type Ticket struct {
	SubjectKind SubjectKind `json:"subjectKind,omitempty"`
	SubjectRef  string      `json:"subjectRef,omitempty"`
	Credential  string      `json:"credential,omitempty"`
	PlaceKind   string      `json:"placeKind,omitempty"`
	ProjectID   string      `json:"projectId,omitempty"`
	ProjectKey  string      `json:"projectKey,omitempty"`
	PlaceRepoID int64       `json:"placeRepoId,omitempty"`
	PlaceRepo   string      `json:"placeRepo,omitempty"`
	Key         string      `json:"key,omitempty"`
	Mode        Mode        `json:"mode,omitempty"`
	Ceiling     string      `json:"ceiling,omitempty"`
	Addressed   string      `json:"addressed,omitempty"`
	Figures     Figures     `json:"figures"`
}

// Charged reports whether the ticket names a budget key.
func (t Ticket) Charged() bool { return t.Key != "" }

// StartBackground decides a background download for the probe this charge belongs to (R-LIM-037): a charge of the probe's
// subject, place and budget key runs step 9a (R-LIM-022) for the remote repoID at this moment, which counts its upstream request
// for the budget key and the remote's ceiling. In enforce a refusal starts nothing: the error is the refusal, and no background
// download is counted (the limit it met is recorded). In audit a download that 9a would have refused starts and is recorded as
// would_limit. ok is false for a request that carries no charge (not a protocol path, a replica without the limits): the
// download then runs charged to nobody.
func (c *Charge) StartBackground(ctx context.Context, repoID int64) (t Ticket, ok bool, err error) {
	if c == nil || c.e == nil {
		return Ticket{}, false, nil
	}
	bg := c.Background(repoID)
	if err := bg.MayFetch(ctx, repoID, false); err != nil {
		bg.pendBg.Store(0)
		bg.Done()
		return Ticket{}, true, err
	}
	t = bg.ticket()
	bg.Done()
	return t, true, nil
}

// ticket is the charge as a [Ticket].
func (c *Charge) ticket() Ticket {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.Resolution.Place
	t := Ticket{
		SubjectKind: c.Subject.Kind, SubjectRef: c.Subject.Ref, Credential: c.Subject.Credential,
		PlaceKind: p.Kind.String(), ProjectID: p.ProjectID, ProjectKey: p.ProjectKey, PlaceRepoID: p.RepoID,
		Key: c.Key, Mode: c.Mode, Ceiling: c.ceiling, Figures: c.Resolution.Figures,
	}
	if !p.Repo.IsZero() {
		t.PlaceRepo = p.Repo.String()
	}
	if !c.Addressed.IsZero() {
		t.Addressed = c.Addressed.String()
	}
	return t
}

// placeOf is the place a ticket names.
func (t Ticket) placeOf() Place {
	p := Place{ProjectID: t.ProjectID, ProjectKey: t.ProjectKey, RepoID: t.PlaceRepoID}
	switch t.PlaceKind {
	case PlaceProject.String():
		p.Kind = PlaceProject
	case PlaceRepository.String():
		p.Kind = PlaceRepository
	}
	if r, err := qname.ParseRepo(t.PlaceRepo); err == nil {
		p.Repo = r
	}
	return p
}

// Resume rebuilds the charge of a background download on the replica that runs it, from the ticket the probe's replica decided
// (R-LIM-037, Q-LIM-54): the bytes the download reads from the upstream are uncached bytes of the probe's budget key and of the
// remote's ceiling, counted at the flush after they were read; its upstream exchanges after the first, which step 9a counted when
// the probe was answered, are upstream requests of the budget key. It counts no second background download, takes no parallel
// lease, is not paced and is checked against nothing. The remote repository repoID is the one that serves it. Done ends it. A ticket
// that names no budget key gives nil: the download is charged to nobody.
func (e *Engine) Resume(t Ticket, repoID int64) *Charge {
	if e == nil || !t.Charged() {
		return nil
	}
	c := &Charge{
		e:          e,
		Subject:    Subject{Kind: t.SubjectKind, Ref: t.SubjectRef, Credential: t.Credential},
		Resolution: Resolution{Place: t.placeOf(), Figures: t.Figures},
		Key:        t.Key, Mode: t.Mode, bg: true, id: e.nextID(), started: e.now(), ceiling: t.Ceiling,
	}
	if r, err := qname.ParseRepo(t.Addressed); err == nil {
		c.Addressed = r
	}
	c.served.Store(repoID)
	c.prepaid.Store(1)
	e.track(c)
	return c
}

// Exchange counts one upstream exchange of a background download (R-LIM-006, Q-LIM-54): the first was counted by step 9a when
// the probe was answered, every further one is an upstream request of the budget key at the next flush. A charge that is not
// a resumed background download counts every exchange.
func (c *Charge) Exchange() {
	if c == nil {
		return
	}
	if c.prepaid.CompareAndSwap(1, 0) {
		return
	}
	c.pendUpstream.Add(1)
}
