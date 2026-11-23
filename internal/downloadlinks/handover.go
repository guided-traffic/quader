package downloadlinks

import (
	"context"
	"errors"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/observability"
)

// The handover per blob to a Bastion at the serve point (storage.md R-STOR-095, R-STOR-096; bastion.md R-BAS-070 to R-BAS-073):
// for a recognized forward that offered to take one, the main installation answers a download with a link of the object store
// instead of the bytes, and the Bastion reads the object itself. The forward is decided as the caller's own request up to the point
// where edge.ServeItem would open the object; there, after the gates and Admit, ServeOptions.Handover answers either "stream" or a
// handover. Every condition that does not hold streams the download as without the offer.
//
// The steps and where they are answered: 1 (the offer and the switch) here, from the Recognition and the registrations'
// snapshot; 2 (an artifact) and 3 (the content and the gates) by the serve point, which says so in HandoverDownload; 4 (conditionals
// and ranges) and 6 (Admit) by edge.ServeItem, as for every download; 5 (the size), 7 (the copy) and 9 (the credential and the
// signature) here; 11 (the issue) by edge.ServeItem (the 200 without a body), here (what is counted and charged) and the marking
// stage (the class and the header). Steps 8 and 10, the data key of an encrypted copy and its trace, are not built: an encrypted
// copy is never named, so a blob whose servable copies are all encrypted is streamed.

// HandoverSwitch answers whether the handover of a Bastion registration is on in the replica's snapshot (R-BAS-071):
// *bastionmain.HandoverSwitches implements it.
type HandoverSwitch interface {
	On(bastionID string) bool
}

// The reasons an offer of a handover was streamed (R-STOR-095): the label reason of
// quader_storage_download_handovers_declined_total, a closed set (R-OPS-007). The reasons key and trace of an encrypted copy
// (steps 8 and 10) join it with the data keys.
const (
	HandoverDeclinedSwitchOff   = "switch_off"
	HandoverDeclinedNotArtifact = "not_artifact"
	HandoverDeclinedGate        = "gate"
	HandoverDeclinedSize        = "size"
	HandoverDeclinedConnection  = "connection"
	HandoverDeclinedCredential  = "credential"
	HandoverDeclinedSigning     = "signing"
)

// HandoverDeclinedReasons are the reasons in the order of the steps.
func HandoverDeclinedReasons() []string {
	return []string{
		HandoverDeclinedSwitchOff, HandoverDeclinedNotArtifact, HandoverDeclinedGate, HandoverDeclinedSize, HandoverDeclinedConnection,
		HandoverDeclinedCredential, HandoverDeclinedSigning,
	}
}

// HandoverKindLink is the label kind of quader_storage_download_handovers_total for a handover of a plain copy: a link and no
// key. link_and_key, for an encrypted copy, joins it with the data keys.
const HandoverKindLink = "link"

// HandoverDownload is a download a serve point may hand over (R-STOR-095): the item row with its copies, what the serve point
// decided of steps 2 and 3, and how it records a served download with its caller.
type HandoverDownload struct {
	Item items.Item
	// Artifact is step 2: a stored file the item store serves on a route of the kind Download, or a file of /files/; never a
	// document, a system read, a remote repository's miss or an answer a format writes itself.
	Artifact bool
	// Decided is step 3's gates: every gate that applies passed at its first decision on a decided state (Q-STOR-284).
	Decided bool
	// Record counts the download (items.Store.Served) with the serve point's actor, source, endpoint and statistics flag; the event
	// it is given carries the item and the handover's delivery.
	Record func(items.ServedEvent)
}

// HandoverOptions are ServeOptions.Handover and ServeOptions.HandedOver for a download (R-STOR-095), or two nils when the request
// is no recognized forward that offered a handover and whose answer the marking stage marks (step 1): such a request is not
// counted. An offer that meets a condition that does not hold is counted with the first one in
// quader_storage_download_handovers_declined_total, and the download streams.
func (l *Links) HandoverOptions(ctx context.Context, d HandoverDownload) (handover func() bool, handedOver func()) {
	rec, ok := bastionmain.MarksHandover(ctx)
	if !ok {
		return nil, nil
	}
	decline := func(reason string) { l.o.Metrics.HandoverDeclined(reason) }
	switch {
	case l.o.Handovers == nil || !l.o.Handovers.On(rec.BastionID): // step 1: the registration's switch
		decline(HandoverDeclinedSwitchOff)
		return nil, nil
	case !d.Artifact: // step 2
		decline(HandoverDeclinedNotArtifact)
		return nil, nil
	case d.Item.ContentStatus != blob.StatusOK: // step 3: the content, beside Usable and ServeCheck, which ran before
		decline(HandoverDeclinedConnection)
		return nil, nil
	case !d.Decided: // step 3: the gates on a decided state
		decline(HandoverDeclinedGate)
		return nil, nil
	}
	var given iface.Handover
	handover = func() bool {
		h, declined := l.handover(ctx, d.Item, rec.Offer.MaxSize)
		if declined != "" {
			decline(declined)
			return false
		}
		v, err := iface.EncodeHandover(h)
		if err != nil {
			// The value would be refused by the Bastion on its form; the error names no link (iface.ErrHandover).
			l.o.Log.Warn("a handover could not be encoded; the download streams", "handover", h, "error", err)
			decline(HandoverDeclinedSigning)
			return false
		}
		if !bastionmain.NoteHandover(ctx, v) {
			return false
		}
		given = h
		return true
	}
	// Step 11: everything is counted when the handover is issued, as for a link (R-STOR-073): the size as cached bytes of the budget
	// key, one download in the statistics and the repository's activity, artifact.downloaded where downloads are audited, the
	// request log's delivery, the metric. A speed figure of the caller is no condition (Q-STOR-343).
	handedOver = func() {
		limits.FromContext(ctx).ChargeLink(d.Item.Size)
		l.o.Metrics.HandedOver(rec.Bastion, HandoverKindLink)
		if ri := observability.RequestInfoFrom(ctx); ri != nil {
			ri.SetDelivery(items.DeliveryHandover)
		}
		if d.Record != nil {
			d.Record(items.ServedEvent{
				Item: d.Item, CountStats: true, Delivery: items.DeliveryHandover, LinkExpiresAt: given.ExpiresAt,
				Bastion: rec.Bastion, Connection: given.Connection, Encrypted: given.Encrypted,
			})
		}
	}
	return handover, handedOver
}

// handover is steps 5, 7 and 9 of R-STOR-095 for a download whose earlier conditions held: the size within the links' minSize and
// the offer's max-size (Q-STOR-342); the first copy in the read order on a connection that can serve a handover (R-STOR-096:
// https, no host of Quader, the link check's first part passed, its circuit closed, the copy present) and is not encrypted
// (Q-STOR-341; an encrypted copy needs its data key, which is not built); and a link to that copy's object signed for GET against
// the connection's public address with no override, for linkLifeSeconds bounded by the signing credential (R-STOR-072).
// declined names the reason of a download that streams instead.
func (l *Links) handover(ctx context.Context, it items.Item, maxSize int64) (iface.Handover, string) {
	set := l.Settings()
	if it.Size < set.MinSize || it.Size > maxSize {
		return iface.Handover{}, HandoverDeclinedSize
	}
	copies := it.Copies
	if !it.CopiesRead {
		if l.o.Copies == nil {
			return iface.Handover{}, HandoverDeclinedConnection
		}
		var err error
		if copies, err = l.o.Copies(ctx, it.Digest); err != nil {
			return iface.Handover{}, HandoverDeclinedConnection
		}
	}
	if l.o.Connections == nil {
		return iface.Handover{}, HandoverDeclinedConnection
	}
	for _, cp := range l.o.Connections.ServableOf(copies) {
		st, ok := l.State(cp.Connection)
		c, configured := l.o.Connections.Get(cp.Connection)
		switch {
		case !ok || !configured || !st.HandoverUsable:
			continue
		case c.Encrypted():
			continue // steps 8 and 10: the data key and its trace are not built, so an encrypted copy is never named
		case cp.StoredSize != 0 && cp.StoredSize != it.Size:
			continue // a plain copy is the blob's bytes: one of another length is no copy the Bastion could verify
		}
		opts := blob.PresignOptions{Life: set.LinkLife()}
		if a, ok := set.PublicAddresses[c.ID()]; ok {
			opts.Address = a
		}
		link, err := c.Presign(ctx, cp.ObjectKey, opts)
		switch {
		case err == nil:
			return iface.Handover{
				Link: link.URL, ExpiresAt: link.ExpiresAt, Connection: c.ID(), SHA256: it.Digest.Hex(), Size: it.Size, StoredSize: it.Size,
			}, ""
		case errors.Is(err, blob.ErrCircuitOpen):
			return iface.Handover{}, HandoverDeclinedConnection
		case errors.Is(err, blob.ErrCredentialExpiring):
			return iface.Handover{}, HandoverDeclinedCredential
		default:
			l.o.Log.Warn("a handover's link could not be signed; the download streams", "connection", c.ID(), "error", storeReason(err))
			return iface.Handover{}, HandoverDeclinedSigning
		}
	}
	return iface.Handover{}, HandoverDeclinedConnection
}
