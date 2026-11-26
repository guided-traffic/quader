package downloadlinks

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/observability"
)

// The handover per blob to a Bastion at the serve point (storage.md R-STOR-095, R-STOR-096; bastion.md R-BAS-070 to R-BAS-073):
// for a recognized forward that offered to take one, the main installation answers a download with a link of the object store
// instead of the bytes, and for a copy on an encrypted connection with that copy's data key, and the Bastion reads the object
// itself. The forward is decided as the caller's own request up to the point where edge.ServeItem would open the object; there,
// after the gates and Admit, ServeOptions.Handover answers either "stream" or a handover. Every condition that does not hold streams
// the download as without the offer.
//
// The steps and where they are answered: 1 (the offer and the switch) here, from the Recognition and the registrations'
// snapshot; 2 (an artifact) and 3 (the content and the gates) by the serve point, which says so in HandoverDownload; 4 (conditionals
// and ranges) and 6 (Admit) by edge.ServeItem, as for every download; 5 (the size), 7 (the copy: plain copies first, then the first
// encrypted one), 9 (the credential and the signature), 8 (the data key of an encrypted copy, taken after the signature,
// Q-STOR-471) and 10 (its trace, written after the value is encoded and before the answer is noted, Q-STOR-472) here; 11 (the
// issue) by edge.ServeItem (the 200 without a body), here (what is counted and charged) and the marking stage (the class and the
// header).
//
// The data key is the one place where a key leaves the main installation. It exists in this replica's memory from CopyDataKey to
// the encoding of the value: the format's key only until its bytes are copied into the value's iface.HandoverKey, that one until the
// value is encoded, and both are destroyed on every path that does not hand it over (a failed trace, a refused encoding, a panic).
// No log line, error, metric label or audit detail holds it or the link; the encoded value is a string, which Go cannot erase
// (security/bastion-handover.md H-1490).

// HandoverSwitch answers whether the handover of a Bastion registration is on in the replica's snapshot (R-BAS-071):
// *bastionmain.HandoverSwitches implements it.
type HandoverSwitch interface {
	On(bastionID string) bool
}

// DataKeys opens the record of one copy on an encrypted connection and returns that copy's data key (storage.md R-STOR-083):
// *blob.Store implements it, and only this package and internal/blob name its method (R-STOR-096, archtest CheckDataKeyCallers).
// It reads no row: the copy and the blob's size are the ones the request's item lookup read (Q-STOR-470).
type DataKeys interface {
	CopyDataKey(ctx context.Context, d blob.Digest, size int64, cp blob.Copy) (blobcrypt.DataKey, error)
}

// The reasons an offer of a handover was streamed (R-STOR-095): the label reason of
// quader_storage_download_handovers_declined_total, a closed set (R-OPS-007).
const (
	HandoverDeclinedSwitchOff   = "switch_off"
	HandoverDeclinedNotArtifact = "not_artifact"
	HandoverDeclinedGate        = "gate"
	HandoverDeclinedSize        = "size"
	HandoverDeclinedConnection  = "connection"
	HandoverDeclinedCredential  = "credential"
	HandoverDeclinedSigning     = "signing"
	// HandoverDeclinedKey: the data key of the encrypted copy could not be opened (a key the replica does not hold or that was
	// destroyed, a record that does not bind the blob, the store's answer to the HeadObject).
	HandoverDeclinedKey = "key"
	// HandoverDeclinedTrace: the trace bastion.data_key.handed could not be written, so the key is not handed (Q-STOR-344).
	HandoverDeclinedTrace = "trace"
)

// HandoverDeclinedReasons are the reasons in the order of the steps as they run.
func HandoverDeclinedReasons() []string {
	return []string{
		HandoverDeclinedSwitchOff, HandoverDeclinedNotArtifact, HandoverDeclinedGate, HandoverDeclinedSize, HandoverDeclinedConnection,
		HandoverDeclinedCredential, HandoverDeclinedSigning, HandoverDeclinedKey, HandoverDeclinedTrace,
	}
}

// The label kind of quader_storage_download_handovers_total: a handover of a plain copy is a link and no key, one of an encrypted
// copy a link and the copy's data key.
const (
	HandoverKindLink       = "link"
	HandoverKindLinkAndKey = "link_and_key"
)

// HandoverDownload is a download a serve point may hand over (R-STOR-095): the item row with its copies, what the serve point
// decided of steps 2 and 3, the forward's caller, and how it records a served download.
type HandoverDownload struct {
	Item items.Item
	// Artifact is step 2: a stored file the item store serves on a route of the kind Download, or a file of /files/; never a
	// document, a system read, a remote repository's miss or an answer a format writes itself.
	Artifact bool
	// Decided is step 3's gates: every gate that applies passed at its first decision on a decided state (Q-STOR-284).
	Decided bool
	// Actor, Source and Endpoint are the forward's caller (anonymous where it sent no credential), its address and the endpoint the
	// forward arrived on: the trace of a data key names them (R-STOR-096), and the served download is recorded with them.
	Actor    audit.Actor
	Source   audit.Source
	Endpoint string
	// Record counts the download (items.Store.Served) with its statistics flag; the event it is given carries the item, the caller
	// and the handover's delivery.
	Record func(items.ServedEvent)
	// Unit is the unit the item belongs to as the format names it (operations-and-observability.md R-OPS-054): the trace of a data
	// key names it, so that the audit log shows the keys handed for one pull as one row (R-OPS-055).
	Unit items.DownloadUnit
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
		h, keyID, declined := l.handover(ctx, d.Item, rec.Offer.MaxSize)
		// An encrypted copy's data key lives from here to the encoding below; this destroys it on every path, a panic's included.
		defer l.destroy(h.DataKey)
		if declined != "" {
			decline(declined)
			return false
		}
		v, err := iface.EncodeHandover(h)
		l.destroy(h.DataKey) // its bytes are in v now, or nowhere
		if err != nil {
			// The value would be refused by the Bastion on its form; the error names no link and no key (iface.ErrHandover).
			l.o.Log.Warn("a handover could not be encoded; the download streams", "handover", h, "error", err)
			decline(HandoverDeclinedSigning)
			return false
		}
		if h.Encrypted {
			// Step 10: no key leaves without its trace, committed before the answer is noted (Q-STOR-344, Q-STOR-472).
			if err := l.trace(ctx, d, rec, h, keyID); err != nil {
				l.o.Log.Warn("the trace of a data key handed to a Bastion could not be written; the download streams", "handover", h, "error", err)
				decline(HandoverDeclinedTrace)
				return false
			}
		}
		if !bastionmain.NoteHandover(ctx, v) {
			return false
		}
		h.DataKey = iface.HandoverKey{}
		given = h
		return true
	}
	// Step 11: everything is counted when the handover is issued, as for a link (R-STOR-073): the size as cached bytes of the budget
	// key, one download in the statistics and the repository's activity, artifact.downloaded where downloads are audited, the
	// request log's delivery, the metric. A speed figure of the caller is no condition (Q-STOR-343).
	handedOver = func() {
		limits.FromContext(ctx).ChargeLink(d.Item.Size)
		kind := HandoverKindLink
		if given.Encrypted {
			kind = HandoverKindLinkAndKey
		}
		l.o.Metrics.HandedOver(rec.Bastion, kind)
		if ri := observability.RequestInfoFrom(ctx); ri != nil {
			ri.SetDelivery(items.DeliveryHandover)
		}
		if d.Record != nil {
			d.Record(items.ServedEvent{
				Item: d.Item, Actor: d.Actor, Source: d.Source, Endpoint: d.Endpoint, CountStats: true, Delivery: items.DeliveryHandover,
				LinkExpiresAt: given.ExpiresAt, Bastion: rec.Bastion, Connection: given.Connection, Encrypted: given.Encrypted,
			})
		}
	}
	return handover, handedOver
}

// handover is steps 5, 7, 9 and 8 of R-STOR-095 for a download whose earlier conditions held: the size within the links' minSize
// and the offer's max-size (Q-STOR-342); the copy (handoverCopy); a link to that copy's object signed for GET against the
// connection's public address with no override, for linkLifeSeconds bounded by the signing credential (R-STOR-072); and for an
// encrypted copy its data key, taken last, so that a download that streams for its credential or its signature takes no key and
// sends no HeadObject (Q-STOR-471). keyID is the storage key that wrapped the data key, which the trace names. declined names the
// reason of a download that streams instead; a value with a key is never returned with a reason.
func (l *Links) handover(ctx context.Context, it items.Item, maxSize int64) (h iface.Handover, keyID, declined string) {
	set := l.Settings()
	if it.Size < set.MinSize || it.Size > maxSize {
		return iface.Handover{}, "", HandoverDeclinedSize
	}
	copies := it.Copies
	if !it.CopiesRead {
		if l.o.Copies == nil {
			return iface.Handover{}, "", HandoverDeclinedConnection
		}
		var err error
		if copies, err = l.o.Copies(ctx, it.Digest); err != nil {
			return iface.Handover{}, "", HandoverDeclinedConnection
		}
	}
	if l.o.Connections == nil {
		return iface.Handover{}, "", HandoverDeclinedConnection
	}
	cp, c, stored, ok := l.handoverCopy(it, copies)
	if !ok {
		return iface.Handover{}, "", HandoverDeclinedConnection
	}
	opts := blob.PresignOptions{Life: set.LinkLife()}
	if a, ok := set.PublicAddresses[c.ID()]; ok {
		opts.Address = a
	}
	link, err := c.Presign(ctx, cp.ObjectKey, opts)
	switch {
	case errors.Is(err, blob.ErrCircuitOpen):
		return iface.Handover{}, "", HandoverDeclinedConnection
	case errors.Is(err, blob.ErrCredentialExpiring):
		return iface.Handover{}, "", HandoverDeclinedCredential
	case err != nil:
		l.o.Log.Warn("a handover's link could not be signed; the download streams", "connection", c.ID(), "error", storeReason(err))
		return iface.Handover{}, "", HandoverDeclinedSigning
	}
	h = iface.Handover{
		Link: link.URL, ExpiresAt: link.ExpiresAt, Connection: c.ID(), SHA256: it.Digest.Hex(), Size: it.Size, StoredSize: stored,
		Encrypted: c.Encrypted(),
	}
	if !h.Encrypted {
		return h, "", ""
	}
	key, keyID, declined := l.dataKey(ctx, it, cp)
	if declined != "" {
		return iface.Handover{}, "", declined
	}
	h.DataKey = key
	return h, keyID, ""
}

// handoverCopy is step 7 (R-STOR-096, Q-STOR-341): of the copies a read would try now, in their order, the first on a connection
// that can serve a handover (https, no host of Quader, the link check's first part passed, its circuit closed, the copy present)
// and is not encrypted; only when there is none, the first on such a connection that is encrypted. stored is the object's size as
// the value names it: the blob's size for a plain copy, S(size) of the stored format for an encrypted one (R-STOR-076). A copy whose
// row names another stored size is no copy the Bastion could verify and is passed over.
func (l *Links) handoverCopy(it items.Item, copies []blob.Copy) (blob.Copy, *blob.Connection, int64, bool) {
	servable := l.o.Connections.ServableOf(copies)
	sealed, err := blobcrypt.StoredSize(it.Size)
	for _, encrypted := range []bool{false, true} {
		for _, cp := range servable {
			st, known := l.State(cp.Connection)
			c, configured := l.o.Connections.Get(cp.Connection)
			switch {
			case !known || !configured || !st.HandoverUsable || c.Encrypted() != encrypted:
				continue
			case !encrypted && cp.StoredSize != 0 && cp.StoredSize != it.Size:
				continue // a plain copy is the blob's bytes
			case encrypted && (err != nil || cp.StoredSize != sealed):
				continue // an encrypted copy is S(size) bytes
			case encrypted:
				return cp, c, sealed, true
			default:
				return cp, c, it.Size, true
			}
		}
	}
	return blob.Copy{}, nil, 0, false
}

// dataKey is step 8 for the copy cp of an encrypted connection: CopyDataKey opens that copy's record (one HeadObject, steps 1 to 6
// of R-STOR-077) and returns its own data key, whose bytes are copied into the value's HandoverKey; the format's key is destroyed at
// once. A failure is the reason key, and the download streams: the stream's read meets the copy and marks it where it is damaged
// (R-STOR-082). The log line names the connection, the digest and the reason, never a key.
func (l *Links) dataKey(ctx context.Context, it items.Item, cp blob.Copy) (iface.HandoverKey, string, string) {
	if l.o.DataKeys == nil {
		return iface.HandoverKey{}, "", HandoverDeclinedKey
	}
	dk, err := l.o.DataKeys.CopyDataKey(ctx, it.Digest, it.Size, cp)
	if err != nil {
		l.o.Log.Warn("the data key of a copy could not be opened for a handover; the download streams", "connection", cp.Connection,
			"sha256", it.Digest.Hex(), "error", storeReason(err))
		return iface.HandoverKey{}, "", HandoverDeclinedKey
	}
	defer dk.Key.Destroy()
	raw, err := dk.Key.Bytes()
	if err != nil {
		return iface.HandoverKey{}, "", HandoverDeclinedKey
	}
	key, err := iface.NewHandoverKey(raw)
	clear(raw)
	if err != nil {
		return iface.HandoverKey{}, "", HandoverDeclinedKey
	}
	l.keysHeld.Add(1)
	return key, dk.KeyID, ""
}

// destroy overwrites a handed data key of this replica's handovers and counts it as no longer held; a destroyed or absent key is
// left as it is.
func (l *Links) destroy(k iface.HandoverKey) {
	if k.Present() {
		k.Destroy()
		l.keysHeld.Add(-1)
	}
}

// KeysHeld is the number of data keys this replica's handovers hold now: zero whenever no handover of an encrypted copy is being
// decided, since every path destroys its key (R-STOR-096). Tests count with it.
func (l *Links) KeysHeld() int64 { return l.keysHeld.Load() }

// errNoTrail is the trace of a replica built without the audit's recorder: no key leaves without its trace.
var errNoTrail = errors.New("downloadlinks: no audit trail to write the trace of a data key to")

// trace is step 10 for an encrypted copy (R-STOR-096, Q-STOR-344): the transactional event bastion.data_key.handed in a
// transaction of its own, whatever audit.record_downloads says, not journaled. Its actor is the forward's caller, its source the
// caller's address, its resource the registration (bastion, its principal ID), its repository, project and path the item's, and its
// details the registration's name, the Bastion replica that forwarded (Quader-Bastion-Replica), the blob's digest, the connection,
// the storage key that wrapped the data key and the link's expiry; never the key, the wrap or the link.
func (l *Links) trace(ctx context.Context, d HandoverDownload, rec bastionmain.Recognition, h iface.Handover, keyID string) error {
	if l.o.Runner == nil || l.o.Audit == nil {
		return errNoTrail
	}
	actor := d.Actor
	if actor.Kind == "" {
		actor = audit.Actor{Kind: "anonymous"}
	}
	details := map[string]string{
		"bastion": rec.Bastion, "replica": rec.Replica, "sha256": h.SHA256, "connection": h.Connection, "storageKeyId": keyID,
		"linkExpiresAt": h.ExpiresAt.UTC().Format(time.RFC3339),
	}
	// The unit of the item, from 2026-11-26 on (R-OPS-054): the audit log shows the keys of one pull as one row (R-OPS-055).
	if u := d.Unit; u.Package != "" {
		details["package"] = u.Package
		if u.Version != "" {
			details["version"] = u.Version
		}
		if u.Key != "" {
			details["unit"] = u.Key
		}
	}
	ev := audit.Event{
		Type: audit.EventBastionDataKeyHanded, Actor: actor, Outcome: audit.Success, Source: d.Source, Endpoint: d.Endpoint,
		Subject:    audit.Subject{Kind: "bastion", ID: rec.BastionID, Name: rec.Bastion},
		Repository: d.Item.Repository.String(), Project: d.Item.Repository.Project, Path: d.Item.Path,
		Details: marshal(details),
	}
	return l.o.Runner.InTx(ctx, func(tx meta.Tx) error { return l.o.Audit.RecordTx(ctx, tx, ev) })
}
