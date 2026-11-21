package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/name"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The endpoints of npm's signatures (signatures.md R-SIG-028, Q-SIG-34, Q-SIG-35): the registry's keys and a version's attestations,
// below the registry URL of every class. npm reads the keys of a registry from -/npm/v1/keys (404 means none) and fetches a version's
// attestations at the path of its dist.attestations.url resolved against the registry URL (pacote's lib/registry.js:
// new URL(new URL(url).pathname, registry)), so the path reaches the serving repository.
//
//	-/npm/v1/keys                               a remote: its upstream's keys; a local repository: Quader's own (R-SIG-050), none
//	                                            while no signing identity is current; a virtual one: the union
//	-/npm/v1/attestations/<name>@<version>      a remote: its upstream's; a local repository: a bundle npm can verify; a virtual one:
//	                                            the first member that has them, in the order its tarball is resolved

// KeysPath is the canonical path of a registry's keys document, the path of its endpoint (R-SIG-028).
const KeysPath = "-/npm/v1/keys"

// The variants a remote stores the two documents under.
const (
	keysVariant         = "keys"
	attestationsVariant = "attestations"
)

// keysTarget is the target of the background fetch of a remote's keys document (R-SIG-026).
const keysTarget = "keys"

// AttestationsPath is the canonical path of a version's attestations document (Q-SIG-51).
func AttestationsPath(n name.Name, version string) string {
	return AttestationsPrefix + n.String() + "@" + version
}

// matchKeys matches -/npm/v1/keys.
func matchKeys(escaped string) (formats.Params, bool) { return matchOp("npm", "v1", "keys")(escaped) }

// matchAttestations matches -/npm/v1/attestations/<name>@<version>: the name with its scope separator as "/" or "%2f", the version
// after the last "@".
func matchAttestations(escaped string) (formats.Params, bool) {
	segs := segments(escaped)
	if len(segs) < 5 || !isLiteral(segs[0], "-") || !isLiteral(segs[1], "npm") || !isLiteral(segs[2], "v1") || !isLiteral(segs[3], "attestations") {
		return nil, false
	}
	spec := strings.Join(segs[4:], "/")
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		return nil, false
	}
	escapedName, version := spec[:at], spec[at+1:]
	if _, rest, ok := splitName(strings.Split(escapedName, "/")); !ok || len(rest) != 0 {
		return nil, false
	}
	v, ok := decode(version)
	if !ok {
		return nil, false
	}
	return formats.Params{"name": escapedName, "version": v}, true
}

// isSignatureEndpoint reports whether a path is one of the two endpoints: they are no deferred endpoint (R-NPM-043).
func isSignatureEndpoint(escaped string) bool {
	_, keys := matchKeys(escaped)
	_, att := matchAttestations(escaped)
	return keys || att
}

// signaturePath tells the two documents apart by their canonical path: "keys", or the package and version of an attestations
// document. ok is false for any other path.
func signaturePath(p string) (n name.Name, version string, keys, ok bool) {
	if p == KeysPath {
		return name.Name{}, "", true, true
	}
	spec, cut := strings.CutPrefix(p, AttestationsPrefix)
	if !cut {
		return name.Name{}, "", false, false
	}
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		return name.Name{}, "", false, false
	}
	n, err := name.Parse(spec[:at])
	v, okV := canonicalVersion(spec[at+1:])
	if err != nil || !okV {
		return name.Name{}, "", false, false
	}
	return n, v, false, true
}

// keysDocument is npm's keys answer: {"keys": [{"expires", "keyid", "keytype", "scheme", "key"}]}; the members of a key pass through.
type keysDocument struct {
	Keys []json.RawMessage `json:"keys"`
}

// keyID reads the keyid of a key entry.
func keyID(raw json.RawMessage) string {
	var k struct {
		KeyID string `json:"keyid"`
	}
	if json.Unmarshal(raw, &k) != nil {
		return ""
	}
	return k.KeyID
}

// parseKeys reads a stored or fetched keys document within the bounds of untrusted JSON.
func parseKeys(doc []byte) (keysDocument, error) {
	var d keysDocument
	if err := untrusted.UnmarshalJSON(doc, untrusted.Limits{JSONDepth: 16, JSONMembers: 10_000}, &d); err != nil {
		return keysDocument{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamFailed, "The upstream's keys document cannot be read.", errs.WithCause(err))
	}
	return d, nil
}

// unionKeys is the union of keys documents by keyid, in member order (Q-SIG-34).
func unionKeys(docs []keysDocument) keysDocument {
	out := keysDocument{Keys: []json.RawMessage{}}
	seen := map[string]bool{}
	for _, d := range docs {
		for _, k := range d.Keys {
			id := keyID(k)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out.Keys = append(out.Keys, k)
		}
	}
	return out
}

// handleKeys answers GET -/npm/v1/keys (R-SIG-028, Q-SIG-34). The keys are public: the same document for every caller who may read
// the repository, whatever members of a virtual repository it may read.
func (f *Format) handleKeys(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	if err := rq.Host.Authorize(ctx, rq, formats.Target{Actions: []formats.Action{formats.ActionRead}}); err != nil {
		return err
	}
	var doc keysDocument
	switch rq.Repo.Class {
	case formats.Remote:
		d, err := f.remoteKeys(ctx, rq)
		if err != nil {
			return err
		}
		doc = d
	case formats.Virtual:
		d, err := f.virtualKeys(ctx, rq)
		if err != nil {
			return err
		}
		doc = d
	default:
		d, err := localKeys(ctx, rq.Host)
		if err != nil {
			return err
		}
		doc = d
	}
	if doc.Keys == nil {
		doc.Keys = []json.RawMessage{}
	}
	return writeJSON(w, http.StatusOK, doc)
}

// virtualKeys is the union of the members' keys by keyid, in member order (R-SIG-028, Q-SIG-34, R-SIG-050): a remote member's upstream
// keys, and Quader's own keys for the first enabled local member, which every local member shares. A remote whose upstream cannot be
// asked adds no key; the others still verify.
func (f *Format) virtualKeys(ctx context.Context, rq *formats.Request) (keysDocument, error) {
	var docs []keysDocument
	local := false
	for i := range rq.Repo.Members() {
		m := &rq.Repo.Members()[i]
		switch {
		case m.Disabled:
		case m.Class == formats.Local && !local:
			d, err := localKeys(ctx, rq.Host)
			if err != nil {
				return keysDocument{}, err
			}
			docs, local = append(docs, d), true
		case m.Class == formats.Remote:
			mrq, err := rq.Member(m)
			if err != nil {
				return keysDocument{}, err
			}
			if d, err := f.remoteKeys(ctx, mrq); err == nil {
				docs = append(docs, d)
			}
		}
	}
	return unionKeys(docs), nil
}

// registryKey is an entry of the keys document Quader writes for its own keys, in the order npmjs writes its members.
type registryKey struct {
	Expires *string `json:"expires"`
	KeyID   string  `json:"keyid"`
	KeyType string  `json:"keytype"`
	Scheme  string  `json:"scheme"`
	Key     string  `json:"key"`
}

// localKeys is the keys document of a local repository (R-SIG-050): Quader's keys from the data port, none while no signing identity is
// current (R-SIG-045, H-1176 for an installation without one).
func localKeys(ctx context.Context, h formats.Host) (keysDocument, error) {
	st, err := storeOf(h.FormatData(ID))
	if err != nil {
		return keysDocument{}, err
	}
	keys, err := st.RegistryKeys(ctx)
	if err != nil {
		return keysDocument{}, err
	}
	out := keysDocument{Keys: make([]json.RawMessage, 0, len(keys))}
	for _, k := range keys {
		e := registryKey{KeyID: k.KeyID, KeyType: k.KeyType, Scheme: k.Scheme, Key: k.Key}
		if k.Expires != "" {
			e.Expires = &k.Expires
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return keysDocument{}, err
		}
		out.Keys = append(out.Keys, raw)
	}
	return out, nil
}

// remoteKeys reads a remote's keys document through the engine: stored for the metadata period, an upstream 404 as no keys.
func (f *Format) remoteKeys(ctx context.Context, rq *formats.Request) (keysDocument, error) {
	d, err := rq.Host.Remote().Metadata(ctx, rq, formats.Path{Path: KeysPath}, keysVariant)
	if err != nil {
		if errs.KindOf(err) == errs.NotFound {
			return keysDocument{Keys: []json.RawMessage{}}, nil
		}
		return keysDocument{}, err
	}
	if d.Status.Negative || d.Bytes == nil {
		return keysDocument{Keys: []json.RawMessage{}}, nil
	}
	body, err := d.Bytes(ctx)
	if err != nil {
		return keysDocument{}, err
	}
	return parseKeys(body)
}

// handleAttestations answers GET -/npm/v1/attestations/<name>@<version> (R-SIG-028, Q-SIG-35), decided as a read of the version's
// document.
func (f *Format) handleAttestations(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	n, err := name.Parse(rq.Params.Get("name"))
	if err != nil {
		return err
	}
	version, ok := canonicalVersion(rq.Params.Get("version"))
	if !ok {
		return notFound()
	}
	if err := rq.Host.Authorize(ctx, rq, documentTarget(n, version, formats.ActionRead)); err != nil {
		return err
	}
	if err := hiddenVersion(rq, n, version); err != nil {
		return err
	}
	p := formats.Path{Path: AttestationsPath(n, version)}
	var body []byte
	switch rq.Repo.Class {
	case formats.Remote:
		d, err := rq.Host.Remote().Metadata(ctx, rq, p, attestationsVariant)
		if err != nil {
			return err
		}
		if d.Status.Negative || d.Bytes == nil {
			return notFound()
		}
		if body, err = d.Bytes(ctx); err != nil {
			return err
		}
	case formats.Virtual:
		m, err := rq.Host.Resolver().Metadata(ctx, rq, p, f.Classify(p), attestationsVariant)
		if err != nil {
			return err
		}
		body = m.Bytes
	default:
		doc, found, err := localAttestations(ctx, rq.Host, rq.RepoRef(), n, version)
		if err != nil {
			return err
		}
		if !found {
			return notFound()
		}
		body = doc
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if rq.Method == http.MethodHead {
		return nil
	}
	_, err = w.Write(body)
	return err
}

// localAttestations is the attestations document of a local version: its provenance bundle, when the trust step vouches that it
// verified against the public Sigstore (Q-SIG-35). npm checks provenance against that root alone, so a bundle of a company's own
// Sigstore is stored, checked and shown by Quader and not offered to npm.
func localAttestations(ctx context.Context, h formats.Host, repo formats.RepoRef, n name.Name, version string) ([]byte, bool, error) {
	path := SigstorePath(n, version)
	vouched, err := h.Signatures().PublicProvenance(ctx, repo, []string{path})
	if err != nil {
		return nil, false, err
	}
	predicate, ok := vouched[path]
	if !ok {
		return nil, false, nil
	}
	it, found, err := h.Items().Stat(ctx, repo, formats.Path{Path: path})
	if err != nil || !found {
		return nil, false, err
	}
	rc, err := h.Items().Open(ctx, it)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rc.Close() }()
	bundle, err := untrusted.ReadAll(rc, formats.MaxSignatureRaw)
	if err != nil {
		return nil, false, err
	}
	if !json.Valid(bundle) {
		return nil, false, nil
	}
	doc, err := json.Marshal(attestationsAnswer{Attestations: []attestationEntry{{PredicateType: predicate, Bundle: bundle}}})
	if err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

type attestationsAnswer struct {
	Attestations []attestationEntry `json:"attestations"`
}

type attestationEntry struct {
	PredicateType string          `json:"predicateType"`
	Bundle        json.RawMessage `json:"bundle"`
}

// signatureUpstreamRequest is the upstream request of the two documents: below the remote's URL, the name's scope separator as %2f
// as the public registry writes it.
func signatureUpstreamRequest(ctx context.Context, base *url.URL, n name.Name, version string, keys bool) (*http.Request, error) {
	rest, raw := KeysPath, KeysPath
	if !keys {
		spec := n.String() + "@" + version
		rest, raw = AttestationsPrefix+spec, AttestationsPrefix+url.PathEscape(spec)
	}
	u := *base
	u.User, u.Fragment, u.RawQuery = nil, "", ""
	u.Path = strings.TrimSuffix(base.Path, "/") + "/" + rest
	u.RawPath = strings.TrimSuffix(base.EscapedPath(), "/") + "/" + raw
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(err))
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// checkSignatureDocument checks a fetched keys or attestations document before it is stored: JSON of the right shape within the
// bounds of untrusted content. A document that is not is a 502 and the previous entry stays.
func checkSignatureDocument(doc []byte, keys bool) error {
	if keys {
		_, err := parseKeys(doc)
		return err
	}
	var d attestationsDocument
	if err := untrusted.UnmarshalJSON(doc, untrusted.Limits{}, &d); err != nil {
		return errs.E(errs.UpstreamFailed, errs.CodeUpstreamFailed, "The upstream's attestations document cannot be read.", errs.WithCause(err))
	}
	return nil
}

var _ formats.SignatureFetcher = remoteAdapter{}

// FetchSignatures implements [formats.SignatureFetcher] (R-SIG-026, Q-SIG-33): the target "keys" stores the remote's keys document
// (once per metadata period: a fresh one costs no request); "<name>@<version>" stores the version's attestations when the stored
// package document names dist.attestations for it, and the keys a keyed publish attestation needs.
func (a remoteAdapter) FetchSignatures(ctx context.Context, u discover.Upstream, target string) error {
	if target == keysTarget {
		_, _, err := u.Document(ctx, KeysPath, keysVariant)
		return err
	}
	// A target that names no version of a package has nothing to fetch.
	n, version, keys, ok := signaturePath(AttestationsPrefix + target)
	if !ok || keys || !namesAttestations(ctx, u, n, version) {
		return nil
	}
	if _, _, err := u.Document(ctx, AttestationsPath(n, version), attestationsVariant); err != nil {
		return err
	}
	_, _, err := u.Document(ctx, KeysPath, keysVariant)
	return err
}

// namesAttestations reads the stored package document of the remote, without a request, and reports whether the version's entry
// names dist.attestations.
func namesAttestations(ctx context.Context, u discover.Upstream, n name.Name, version string) bool {
	st, ok := u.(interface {
		StoredDocument(ctx context.Context, path, variant string) ([]byte, bool, error)
	})
	if !ok {
		return false
	}
	doc, found, err := st.StoredDocument(ctx, n.String(), documentVariant)
	if err != nil || !found {
		return false
	}
	var d struct {
		Versions map[string]struct {
			Dist struct {
				Attestations json.RawMessage `json:"attestations"`
			} `json:"dist"`
		} `json:"versions"`
	}
	if err := untrusted.UnmarshalJSON(doc, untrusted.Limits{}, &d); err != nil {
		return false
	}
	att := bytes.TrimSpace(d.Versions[version].Dist.Attestations)
	return len(att) > 0 && !bytes.Equal(att, []byte("null"))
}
