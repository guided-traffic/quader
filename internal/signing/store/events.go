package store

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/signing"
)

// actorSigning is the name of the actor of the job's and the files' events (system:signing, R-SIG-057).
const actorSigning = "signing"

// SystemActor is the actor of the job's and the files' events.
func SystemActor() audit.Actor { return audit.Actor{Kind: "system", Name: actorSigning} }

// RemovedEntry is the journal entry of signing.identity.removed (R-SIG-059): the identity of the id and certificate stops signing again.
type RemovedEntry struct {
	ID         int64  `json:"id"`
	CertSHA256 string `json:"certSha256"`
}

// RetiredEntry is the journal entry of signing.identity.retired (R-SIG-059): the identity of the id and certificate is withdrawn again.
type RetiredEntry struct {
	ID         int64  `json:"id"`
	CertSHA256 string `json:"certSha256"`
}

func subject(i signing.Identity) audit.Subject {
	name := i.Fingerprint()
	if name == "" {
		name = i.KeyFingerprint()
	}
	return audit.Subject{Kind: "signing_identity", ID: strconv.FormatInt(i.ID, 10), Name: name}
}

func details(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func keyCreatedEvent(actor audit.Actor, id int64, req signing.KeyRequest, keySHA [32]byte) audit.Event {
	subj := signing.Identity{ID: id, KeySHA256: keySHA}
	return audit.Event{
		Type: audit.EventSigningKeyCreated, Actor: actor, Outcome: audit.Success, Subject: subject(subj),
		Details: details(map[string]any{
			"id": id, "subject": subjectText(req.Subject), "names": names(req.URIs, req.Emails), "keySha256": hex.EncodeToString(keySHA[:]),
		}),
	}
}

func installedEvent(actor audit.Actor, i signing.Identity) audit.Event {
	return audit.Event{
		Type: audit.EventSigningInstalled, Actor: actor, Outcome: audit.Success, Subject: subject(i),
		Details: details(map[string]any{
			"id": i.ID, "source": i.Source, "state": i.State, "subject": i.Subject, "issuer": i.Issuer,
			"notAfter": i.NotAfter.UTC().Format(time.RFC3339), "fingerprint": i.Fingerprint(), "npmKeyId": i.NPMKeyID,
		}),
	}
}

func switchedEvent(actor audit.Actor, id, previous int64) audit.Event {
	return audit.Event{
		Type: audit.EventSigningSwitched, Actor: actor, Outcome: audit.Success,
		Subject: audit.Subject{Kind: "signing_identity", ID: strconv.FormatInt(id, 10)},
		Details: details(map[string]any{"id": id, "previous": previous}),
	}
}

func removedEvent(actor audit.Actor, i signing.Identity) audit.Event {
	return audit.Event{
		Type: audit.EventSigningRemoved, Actor: actor, Outcome: audit.Success, Subject: subject(i),
		Details: details(map[string]any{"id": i.ID, "fingerprint": i.Fingerprint()}),
		Journal: details(RemovedEntry{ID: i.ID, CertSHA256: i.Fingerprint()}),
	}
}

func retiredEvent(actor audit.Actor, i signing.Identity, count int64) audit.Event {
	return audit.Event{
		Type: audit.EventSigningRetired, Actor: actor, Outcome: audit.Success, Subject: subject(i),
		Details: details(map[string]any{"id": i.ID, "fingerprint": i.Fingerprint(), "signatures": count}),
		Journal: details(RetiredEntry{ID: i.ID, CertSHA256: i.Fingerprint()}),
	}
}

func refusedEvent(r *signing.Refusal, fingerprint string) audit.Event {
	return audit.Event{
		Type: audit.EventSigningRefused, Actor: SystemActor(), Outcome: audit.Failure, Reason: r.Check,
		Subject: audit.Subject{Kind: "signing_identity", Name: fingerprint},
		Details: details(map[string]any{"reason": r.Detail, "check": r.Check, "fingerprint": fingerprint}),
	}
}

// FailedEvent is signing.failed of work that reached its last attempt (R-SIG-057).
func FailedEvent(kind, code string, count int) audit.Event {
	return audit.Event{
		Type: audit.EventSigningFailed, Actor: SystemActor(), Outcome: audit.Failure, Reason: code,
		Subject: audit.Subject{Kind: "signing_work", Name: kind},
		Details: details(map[string]any{"kind": kind, "code": code, "count": count}),
	}
}

// subjectText renders a key request's subject as a distinguished name, the most specific attribute first.
func subjectText(s signing.Subject) string {
	out := ""
	for _, a := range []struct{ k, v string }{{"CN", s.CN}, {"OU", s.OU}, {"O", s.O}, {"L", s.L}, {"ST", s.ST}, {"C", s.C}} {
		if a.v == "" {
			continue
		}
		if out != "" {
			out += ","
		}
		out += a.k + "=" + a.v
	}
	return out
}
