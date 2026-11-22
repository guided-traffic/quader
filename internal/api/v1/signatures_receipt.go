package v1

import (
	"encoding/json"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signing/bundle"
	"github.com/guided-traffic/quader/internal/signing/statement"
)

// Quader's receipt attestation in the signatures answers (signatures.md R-SIG-055, R-SIG-056): what its statement says, decoded from
// the bundle the job signing stored, and where the job serves it. The statement is Quader's own, read back by the job before it was
// stored, and bounded (16 KiB, 32 signatures, the CI claims of R-SIG-010); a bundle that does not decode leaves the member out and keeps
// where it is served. The decoded facts are what the receipt claimed at its issue: the screens show them as such, never as the badge
// (Q-SIG-1).

// receiptMembers renders the members attestation, servedAs and servedReason of a receipt of this installation.
func receiptMembers(e *gen.SignatureEntry, r *signatures.Receipt) {
	if r == nil {
		return
	}
	switch r.ServedAs {
	case "referrer", "api":
		as := gen.SignatureEntryServedAs(r.ServedAs)
		e.ServedAs = &as
	}
	switch r.Reason {
	case "hides_signature", "no_referrers_api":
		why := gen.SignatureEntryServedReason(r.Reason)
		e.ServedReason = &why
	}
	e.Attestation = receiptRep(r.Bundle)
}

// receiptRep decodes the statement of a receipt's bundle (statement.Read is Build's inverse); nil for a bundle that does not decode or
// names a state the answer does not know.
func receiptRep(raw []byte) *gen.SignatureReceipt {
	payload, err := bundle.Payload(raw)
	if err != nil {
		return nil
	}
	rec, err := statement.Read(payload)
	if err != nil {
		return nil
	}
	malware := gen.SignatureReceiptMalware{
		State: gen.SignatureReceiptMalwareState(rec.Malware.State), Database: optionalText(rec.Malware.Database),
		DatabaseBuiltAt: optionalTime(rec.Malware.DatabaseBuiltAt), ScannedAt: optionalTime(rec.Malware.ScannedAt), Released: rec.Malware.Released,
	}
	vulns := gen.SignatureReceiptVulnerabilities{
		State: gen.VulnerabilityEvaluation(rec.Vulnerabilities.State), Grade: optionalText(rec.Vulnerabilities.Grade),
		DataAt: optionalTime(rec.Vulnerabilities.DataAt),
	}
	if !malware.State.Valid() || !vulns.State.Valid() || len(rec.Vulnerabilities.Grade) > 8 {
		return nil
	}
	if c := rec.Vulnerabilities.Counts; c != nil {
		vulns.Counts = &gen.SignatureReceiptCounts{Critical: c.Critical, High: c.High, Medium: c.Medium, Low: c.Low, Unknown: c.Unknown}
	}
	out := &gen.SignatureReceipt{
		ReceivedAt: rec.ReceivedAt, IssuedAt: rec.IssuedAt, Origin: receiptOrigin(rec.Origin), Malware: malware, Vulnerabilities: vulns,
		Signatures: make([]gen.SignatureReceiptSignature, 0, len(rec.Signatures)), SignaturesOmitted: rec.SignaturesOmitted,
	}
	for _, s := range rec.Signatures {
		out.Signatures = append(out.Signatures, gen.SignatureReceiptSignature{
			Role: s.Role, Scheme: s.Scheme, Integrity: s.Integrity, Identity: optionalText(s.Identity), Issuer: optionalText(s.Issuer),
		})
	}
	return out
}

// receiptOrigin is the statement's origin in the shape of R-SIG-010's origin: a fill as fetched from its upstream at the fill's time, an
// upload by its principal with a pipeline's claims through the same allowlist as a stored origin (originCIRep).
func receiptOrigin(o statement.Origin) gen.SignatureOrigin {
	if o.Kind == statement.OriginFill {
		return gen.SignatureOrigin{Kind: gen.Fetched, UpstreamUrl: optionalText(o.Upstream), At: optionalTime(o.FetchedAt)}
	}
	out := gen.SignatureOrigin{Kind: gen.Uploaded, PrincipalKind: optionalText(o.PrincipalKind), PrincipalName: optionalText(o.PrincipalName)}
	if len(o.CI) > 0 {
		if raw, err := json.Marshal(o.CI); err == nil {
			out.Ci = originCIRep(raw)
		}
	}
	return out
}
