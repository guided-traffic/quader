package job

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/sigstore"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
	"github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signatures/trust"
)

// scopes are the scopes of one run, built once (R-SIG-008, R-SIG-012, R-SIG-016): the installation's, and one per project with
// enabled anchors of its own while the switch is on; with the blocks, the anchor epoch they were read under, and what the integrity
// check of a keyless scheme or a bundle is handed (the keys and the Sigstore roots of every scope).
type scopes struct {
	epoch    int64
	inst     *trust.Scope
	projects map[string]*trust.Scope
	// projectIDs are the keys of projects, sorted.
	projectIDs []string
	blocks     []trust.Block
	keys       scheme.Keys
	roots      []scheme.SigstoreRoot
}

// scopes reads the material of the scopes and builds them.
func (j *Job) scopes(ctx context.Context) (*scopes, error) {
	m, err := j.o.Store.TrustMaterial(ctx)
	if err != nil {
		return nil, fmt.Errorf("the anchors of the scopes: %w", err)
	}
	return buildScopes(m, j.o.Log), nil
}

// buildScopes builds the scopes from the material: each Sigstore root is parsed once (the public root in use and each anchor's own);
// an anchor whose material does not parse is skipped and logged.
func buildScopes(m store.TrustMaterial, log *slog.Logger) *scopes {
	sc := &scopes{epoch: m.Epoch, projects: map[string]*trust.Scope{}, blocks: m.Blocks}
	var public *sigstore.Root
	if len(m.PublicRoot) > 0 {
		r, err := sigstore.LoadRoot(m.PublicRoot)
		if err != nil {
			log.Warn("the public Sigstore root in use does not parse; bundles of the public Sigstore are not trusted", "error", err)
		} else {
			public = r
			sc.roots = append(sc.roots, scheme.SigstoreRoot{TrustedRoot: m.PublicRoot})
		}
	}
	own := map[int64]*sigstore.Root{}
	var instAnchors []anchors.Anchor
	byProject := map[string][]anchors.Anchor{}
	for _, a := range m.Anchors {
		if a.Anchor.Parsed == nil {
			log.Warn("a trust anchor's stored material does not parse; the anchor is skipped", "anchor", a.Anchor.ID, "key", a.Anchor.Key,
				"error", a.Err)
			continue
		}
		p := a.Anchor.Parsed
		sc.keys.OpenPGP = append(sc.keys.OpenPGP, p.OpenPGP...)
		sc.keys.Public = append(sc.keys.Public, p.Public...)
		if p.Sigstore != nil && len(p.Sigstore.TrustedRoot) > 0 {
			r, err := sigstore.LoadRoot(p.Sigstore.TrustedRoot)
			if err != nil {
				log.Warn("a Sigstore anchor's trust root does not parse; the anchor is skipped", "anchor", a.Anchor.ID, "error", err)
				continue
			}
			own[a.Anchor.ID] = r
			sc.roots = append(sc.roots, scheme.SigstoreRoot{AnchorID: a.Anchor.ID, TrustedRoot: p.Sigstore.TrustedRoot})
		}
		if a.Project == "" {
			instAnchors = append(instAnchors, a.Anchor)
		} else if m.ProjectAnchors {
			byProject[a.Project] = append(byProject[a.Project], a.Anchor)
		}
	}
	sc.inst = &trust.Scope{Anchors: instAnchors, PublicRoot: public, Roots: own}
	for p, list := range byProject {
		sc.projects[p] = &trust.Scope{Anchors: append(slices.Clone(instAnchors), list...), PublicRoot: public, Roots: own}
		sc.projectIDs = append(sc.projectIDs, p)
	}
	slices.Sort(sc.projectIDs)
	return sc
}

// decision is what the trust step decided of one signature: its rows, or, for a no_key signature an anchor's key decided, its
// integrity verdict (an invalid one has no rows, R-SIG-008).
type decision struct {
	rows      []store.TrustRow
	integrity signatures.State
	reason    string
}

// decide judges a signature in the installation's scope and in the scope of each project that reaches it (R-SIG-008). The integrity
// verdict an anchor's key gives a no_key signature is a fact of the bytes and holds for every scope (R-SIG-012): one scope's invalid
// wins, as a key the issuer names that does not verify is a mismatch whichever scope holds it.
func decide(sig trust.Signature, sc *scopes, reach []string) decision {
	type judged struct {
		project string
		v       trust.Verdict
	}
	all := []judged{{project: "", v: trust.Judge(sig, sc.inst, sc.blocks)}}
	for _, p := range reach {
		if s, ok := sc.projects[p]; ok {
			all = append(all, judged{project: p, v: trust.Judge(sig, s, sc.blocks)})
		}
	}
	var d decision
	for _, x := range all {
		switch x.v.Integrity {
		case signatures.StateInvalid:
			return decision{integrity: signatures.StateInvalid, reason: x.v.Reason}
		case signatures.StateValid:
			d.integrity = signatures.StateValid
		}
	}
	for _, x := range all {
		d.rows = append(d.rows, store.TrustRow{
			SignatureID: sig.ID, Project: x.project, State: x.v.State, AnchorIDs: x.v.AnchorIDs, BlockID: x.v.BlockID,
		})
	}
	return d
}

// applyTrust computes and writes, in tx, the trust rows of the signatures named (sigs, or read by ids when sigs is nil) under the run's
// epoch: the integrity verdict of a no_key signature an anchor's key decided is written first, an invalid one records signature.invalid
// and keeps no trust row.
func applyTrust(ctx context.Context, tx meta.Tx, st Store, sc *scopes, ids []int64, sigs []trust.Signature) error {
	if sigs == nil {
		var err error
		if sigs, err = st.TrustOf(ctx, tx, ids); err != nil {
			return err
		}
	}
	if len(sigs) == 0 {
		return nil
	}
	all := make([]int64, 0, len(sigs))
	for _, s := range sigs {
		all = append(all, s.ID)
	}
	reach, err := st.ReachingProjects(ctx, tx, all, sc.projectIDs)
	if err != nil {
		return err
	}
	var rows []store.TrustRow
	written := make([]int64, 0, len(sigs))
	var invalid []int64
	for _, s := range sigs {
		d := decide(s, sc, reach[s.ID])
		if d.integrity != "" && s.State == signatures.StateNoKey {
			changed, err := st.SetIntegrity(ctx, tx, s.ID, d.integrity, d.reason)
			if err != nil {
				return err
			}
			if d.integrity == signatures.StateInvalid {
				invalid = append(invalid, s.ID)
				if changed {
					if err := st.RecordInvalid(ctx, tx, s.ID); err != nil {
						return err
					}
				}
				continue
			}
		}
		rows = append(rows, d.rows...)
		written = append(written, s.ID)
	}
	if err := st.DeleteTrust(ctx, tx, invalid); err != nil {
		return err
	}
	return st.WriteTrust(ctx, tx, sc.epoch, written, rows)
}

// RunTrust is the handler of signature-trust (R-SIG-012): it builds the scopes once, drops the trust rows of signatures in a state
// without a trust step, then works the valid and no_key signatures whose installation row was computed under an older epoch or is
// missing, in batches of 10,000 in id order, one transaction each. It reads no blob, no item and no upstream. After the slice it
// enqueues the rest.
func (j *Job) RunTrust(ctx context.Context, run Run) error {
	deadline := j.deadline()
	sc, err := j.scopes(ctx)
	if err != nil {
		return err
	}
	var dropped int64
	for {
		n, err := j.o.Store.DropOtherTrust(ctx, batch)
		if err != nil {
			return fmt.Errorf("the trust rows of signatures without a trust step: %w", err)
		}
		dropped += n
		if n < batch {
			break
		}
	}
	var judged int64
	after := int64(0)
	more := false
	for {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if j.past(deadline) {
			more = true
			break
		}
		var n int
		if err := j.o.DB.InTx(ctx, func(tx meta.Tx) error {
			sigs, err := j.o.Store.TrustBatch(ctx, tx, after, sc.epoch, batch)
			if err != nil || len(sigs) == 0 {
				return err
			}
			n, after = len(sigs), sigs[len(sigs)-1].ID
			return applyTrust(ctx, tx, j.o.Store, sc, nil, sigs)
		}); err != nil {
			return fmt.Errorf("the trust step: %w", err)
		}
		if n > 0 && j.o.SignalAll != nil {
			j.o.SignalAll() // a batch of trust rows committed: every waiting download of a gate reads again (R-SIG-067)
		}
		judged += int64(n)
		if n < batch {
			break
		}
	}
	j.gauges(ctx)
	result := map[string]any{"judged": judged, "dropped": dropped, "epoch": sc.epoch}
	if more && j.o.EnqueueTrust != nil {
		if err := j.o.DB.InTx(ctx, func(tx meta.Tx) error { return j.o.EnqueueTrust(ctx, tx) }); err != nil {
			return err
		}
		result["continued"] = true
	}
	return run.SetResult(result)
}

// The code of the event signature.trust_root.failed.
const codeRefreshFailed = "refresh_failed"

// RunTrustRoot is the handler of signature-trust-root (R-SIG-017): skipped while signatures.trust_root.refresh is off; else it fetches
// the TUF metadata through the outbound client, verifies the chain from the embedded TUF root, and stores the two targets when their TUF
// version is newer than the stored one and the release's (the store raises the epoch, wakes the trust job and records
// signature.trust_root.updated). A failure keeps what was there, is kept in the settings and records signature.trust_root.failed at most
// once a day; the run itself ends without an error, so that the task is not retried before its next slot.
func (j *Job) RunTrustRoot(ctx context.Context, run Run) error {
	if !j.o.TrustRoot.Refresh {
		return run.SetResult(map[string]string{"skipped": "signatures.trust_root.refresh is off"})
	}
	url := j.o.TrustRoot.URL
	if url == "" {
		url = shipped.DefaultURL
	}
	r, err := j.refresh(ctx, shipped.RefreshOptions{URL: url, Client: j.o.Client, Now: j.o.Now()})
	if err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		j.o.Log.WarnContext(ctx, "the public Sigstore root was not refreshed", "url", url, "error", err)
		if ferr := j.o.Store.TrustRootFailed(ctx, codeRefreshFailed, err.Error()); ferr != nil {
			return ferr
		}
		j.gauges(ctx)
		return run.SetResult(map[string]string{"failed": err.Error()})
	}
	stored, err := j.o.Store.StoreTrustRoot(ctx, r)
	if err != nil {
		return err
	}
	j.gauges(ctx)
	return run.SetResult(map[string]any{"tufVersion": r.TUFVersion, "stored": stored})
}
