package gate

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/malware"
	mgate "github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// Serve is one decision and one wait of a serve point for the three checks of a stored item (signatures.md R-SIG-069, Q-SIG-87): the
// malware gate (malware-scanning.md R-MAL-013), the vulnerability block (vulnerability-scanning.md R-VULN-015) and the signature gate.
// The two serve points of stored content, the format host's itemStore.Serve and /files/, build one and Run it after the format's own
// serve-time check.
type Serve struct {
	// Malware is the malware gate and the download as it knows it; a nil gate refuses nothing.
	Malware   *mgate.Gate
	MalwareOf mgate.Download
	// Enforce is the vulnerability scanning's enforcer and the download with the block it decided from the item lookup; a nil enforcer
	// refuses nothing.
	Enforce *enforce.Enforcer
	VulnOf  enforce.Download
	// Signatures is the signature gate and the download as it knows it; a nil gate, or one that does not apply, refuses nothing.
	Signatures   *Gate
	SignaturesOf Download
}

// Run decides all three checks before it waits (R-SIG-069): when one refuses, the answer is the first refusal in the order
// malware_found, vulnerability_blocked, signature_not_verified, malware_not_checked, vulnerability_data_missing, and nothing waits; when
// none refuses and some are pending, the request waits once for all of them until each has decided or the earliest bound is reached,
// decides again, and a check still pending answers its 503, the malware gate's first. The signature gate reads its rows only when no
// check above it in the order refuses already. The answers, events and metrics of each check stay its own. A download the signature gate
// passed only for a writer is recorded once it is served, when no other check refused it (Q-SIG-240).
func (s Serve) Run(ctx context.Context) error {
	_, err := s.RunDecided(ctx)
	return err
}

// RunDecided is Run that also reports whether every check that applies passed at its first decision on a decided state
// (storage.md R-STOR-070 step 6, Q-STOR-284, Q-STOR-350): the malware scanning off or the blob clean or released, neither a block nor a
// wait of the vulnerability data, and the signature gate off or passed, by a verified signature or for a writer (Q-SIG-211). A download
// that waited, or that a gate let through on a state not decided yet (a blob without a verdict where the malware gate is not strict, a
// block in the mode report), is not: only a decided download may be handed to a signed link of the object store, which cannot be taken
// back during its life. A writer's read is recorded here, once the checks served it (Q-SIG-240), whether the serve point then streams it
// or hands it to a link.
func (s Serve) RunDecided(ctx context.Context) (bool, error) {
	sv, decided, err := s.run(ctx)
	if err == nil && sv != nil && sv.Writer {
		s.Signatures.WriterRead(ctx, sv)
	}
	return decided, err
}

// run is Run's decision and wait; it returns the signature gate's verdict, nil where the gate was not asked, and whether every check
// passed on a decided state.
func (s Serve) run(ctx context.Context) (*Verdict, bool, error) {
	mdec := s.malwareDecision()
	block := s.VulnOf.Block
	if mdec == malware.RefuseFound {
		return nil, false, s.Malware.Refuse(ctx, s.MalwareOf, mdec)
	}
	if block.Refused() && block.Code() == errs.CodeVulnerabilityBlocked {
		return nil, false, s.Enforce.Refuse(ctx, s.VulnOf)
	}
	var sv *Verdict
	if s.Signatures != nil && s.Signatures.Applies(s.SignaturesOf.Addressed, s.SignaturesOf.Holder) {
		v, err := s.Signatures.Decide(ctx, s.SignaturesOf)
		if err != nil {
			return nil, false, err
		}
		sv = v
	}
	if sv != nil && sv.Outcome == Refused {
		return sv, false, s.Signatures.Refuse(ctx, sv)
	}
	if mdec == malware.RefuseNotChecked {
		return sv, false, s.Malware.Refuse(ctx, s.MalwareOf, mdec)
	}
	if block.Active() {
		// vulnerability_data_missing refuses here; a block in the mode report is recorded and serves.
		if err := s.Enforce.Refuse(ctx, s.VulnOf); err != nil {
			return sv, false, err
		}
	}
	mPending, sPending := mdec == malware.Wait, sv != nil && sv.Outcome == Pending
	if !mPending && !sPending {
		return sv, s.malwareDecided() && !block.Active() && (sv == nil || sv.Outcome == Pass), nil
	}
	return sv, false, s.wait(ctx, mPending, sv)
}

// malwareDecided reports whether the malware gate passed the blob on a decided state: the scanning off or not applying (a system
// repository's content, R-MAL-001), or the blob clean or released (R-STOR-070 step 6). A blob without a verdict, unscannable or
// failed without a release is not, also where the gate is not strict and serves it.
func (s Serve) malwareDecided() bool {
	if s.Malware == nil || !s.Malware.Enabled() || s.MalwareOf.System {
		return true
	}
	v := s.MalwareOf.Verdict
	return v.Released() || v.Effective() == malware.StateClean
}

func (s Serve) malwareDecision() malware.Decision {
	if s.Malware == nil {
		return malware.Serve
	}
	return s.Malware.Decide(s.MalwareOf)
}

// waiting is the one wait of the pending checks.
type waiting struct {
	mw *mgate.Wait
	sw *Wait
}

func (w waiting) end(outcome string) {
	if w.mw != nil {
		w.mw.End(outcome)
	}
	if w.sw != nil {
		w.sw.End(outcome)
	}
}

// wait starts the priority work of each pending check and waits for both together (R-SIG-069).
func (s Serve) wait(ctx context.Context, mPending bool, sv *Verdict) error {
	var w waiting
	var err error
	if mPending {
		if w.mw, err = s.Malware.Begin(ctx, s.MalwareOf); err != nil {
			return err
		}
	}
	if sv != nil && sv.Outcome == Pending {
		if w.sw, err = s.Signatures.Begin(ctx, sv); err != nil {
			w.end("pending")
			return err
		}
	}
	deadline, poll := w.bounds()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	var mWake, sWake, mStop, sStop <-chan struct{}
	if w.mw != nil {
		mWake, mStop = w.mw.Wake(), w.mw.Stopped()
	}
	if w.sw != nil {
		sWake, sStop = w.sw.Wake(), w.sw.Stopped()
	}
	mdec := malware.Wait
	sDone := w.sw == nil
	for {
		stopped := false
		select {
		case <-ctx.Done():
			w.end("")
			return ctx.Err()
		case <-mStop:
			stopped = true
		case <-sStop:
			stopped = true
		case <-timer.C:
			return s.stillPending(ctx, w, mdec, sDone, false)
		case <-mWake:
		case <-sWake:
		case <-ticker.C:
		}
		if stopped {
			return s.stillPending(ctx, w, mdec, sDone, true)
		}
		if w.mw != nil && mdec == malware.Wait {
			if d, err := w.mw.Again(ctx); err == nil {
				mdec = d
			}
		}
		sRefused := false
		if !sDone {
			if o, err := w.sw.Again(ctx); err == nil && o != Pending {
				sDone, sRefused = true, o == Refused
			}
		}
		switch {
		case mdec == malware.RefuseFound:
			defer w.end("refused")
			return w.mw.Refuse(ctx, mdec)
		case sRefused:
			defer w.end("refused")
			return w.sw.Refuse(ctx)
		case mdec == malware.RefuseNotChecked:
			defer w.end("refused")
			return w.mw.Refuse(ctx, mdec)
		case (w.mw == nil || mdec == malware.Serve) && sDone:
			w.end("served")
			return nil
		}
	}
}

// bounds are the earliest deadline of the pending checks and the shortest poll.
func (w waiting) bounds() (time.Time, time.Duration) {
	var deadline time.Time
	poll := time.Second
	if w.mw != nil {
		deadline, poll = w.mw.Deadline(), w.mw.Poll()
	}
	if w.sw != nil {
		if deadline.IsZero() || w.sw.Deadline().Before(deadline) {
			deadline = w.sw.Deadline()
		}
		poll = min(poll, w.sw.Poll())
	}
	return deadline, poll
}

// stillPending answers a wait that reached its bound or a drain: the 503 of the check still pending, the malware gate's first.
func (s Serve) stillPending(ctx context.Context, w waiting, mdec malware.Decision, sDone, stopped bool) error {
	defer w.end("pending")
	if w.mw != nil && mdec == malware.Wait {
		return w.mw.Pending(ctx, stopped)
	}
	if !sDone {
		return w.sw.Pending(ctx, stopped)
	}
	return nil
}
