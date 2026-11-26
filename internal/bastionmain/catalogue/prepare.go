package catalogue

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The post-processing of a rendered answer into a document of the catalogue (R-BAS-106, R-BAS-107): what the anonymous caller of
// the main installation reads, less what names an internal identity or the infrastructure (the plan's B.10), with every link on the
// Bastion's base URL and below its own data path (B.13), and without a time of its building or checking (R-BAS-103), which goes into
// the version's state document instead.

// stateTimes are the members that hold the time a document's content was built, checked, scanned or synced, and the database a check
// ran with: they change with a rescan or a sync that finds the same, so they leave the document for its state (R-BAS-103; measured
// on 2026-11-26: every npm vulnerabilities document changed every 15 minutes by them alone). database and databaseBuiltAt go only
// where they sit beside scannedAt, the facts of a malware check.
func stateTimes() map[string]bool {
	return map[string]bool{"builtAt": true, "checkedAt": true, "syncedAt": true, "scannedAt": true, "computedAt": true}
}

// downloadCounters are the members that count downloads: they change with every download, so no document carries them
// (Q-BAS-358). A visitor of a Bastion reads no download statistic of the main installation.
func downloadCounters() map[string]bool {
	return map[string]bool{"downloadCount": true, "downloads": true, "lastDownloadAt": true, "lastDownloadedAt": true}
}

// triageMembers are the members of a finding that name its triage: the person, the note and the history (B.10). The anonymous
// answer of the main installation carries none of them already (R-VULN-019 as P3 revised it); the strip holds that for every
// document, so a later member of the same name cannot reach a Bastion.
func triageMembers() map[string]bool {
	return map[string]bool{
		"statusChangedBy": true, "triagedBy": true, "comment": true, "history": true, "dismissalReason": true, "resolvedReason": true,
		"stateCode": true,
	}
}

// originPrincipal are the members of a signature's origin (SignatureOrigin) that name who uploaded the artifact: the principal and
// the CI job's claims (B.10). kind and at stay.
func originPrincipal() map[string]bool {
	return map[string]bool{"principalKind": true, "principalName": true, "ci": true}
}

// Prepared is a document after the post-processing: its bytes, the times taken out of it by JSON pointer, and the signature
// objects it names (their SHA-256).
type Prepared struct {
	Body  []byte
	Times map[string]any
	Refs  []string
}

// Rewrite holds what a document's links are rewritten with (B.13).
type Rewrite struct {
	// Base is the Bastion's base URL, without a trailing slash: the response base URL the document was rendered on.
	Base string
	// Main are the main installation's own origins (server.base_url, every server.additional_base_urls entry, every further
	// endpoint's base URL): an occurrence of one in a document is replaced with Base, so that none reaches a Bastion.
	Main []string
	// PublicRegistries are the hosts of the formats' well-known public registries (the remote presets): a remote's upstream URL
	// stays only when its host is one of them (B.10).
	PublicRegistries map[string]bool
}

// signatureObjectRE finds the signature objects a document names by the main installation's path.
var signatureObjectRE = regexp.MustCompile(`/api/v1/signatures/objects/([0-9a-f]{64})`)

// Prepare post-processes one rendered answer. A JSON answer is decoded, stripped and encoded again with its members in name order;
// any other text has its links rewritten; bytes that are neither are kept as they are (a signature object).
func Prepare(body []byte, contentType string, rw Rewrite) (Prepared, error) {
	refs := signatureRefs(body)
	switch {
	case isJSON(contentType):
		var v any
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return Prepared{}, err
		}
		times := map[string]any{}
		v = strip(v, "", times, rw)
		if top, ok := v.(map[string]any); ok {
			delete(top, "nextCursor") // the main installation's cursor names nothing at a Bastion
		}
		out, err := json.Marshal(v)
		if err != nil {
			return Prepared{}, err
		}
		return Prepared{Body: out, Times: times, Refs: refs}, nil
	case isText(contentType):
		return Prepared{Body: []byte(rw.text(string(body))), Times: map[string]any{}, Refs: refs}, nil
	}
	return Prepared{Body: body, Times: map[string]any{}, Refs: refs}, nil
}

// signatureRefs lists the signature objects named in body, sorted and once each.
func signatureRefs(body []byte) []string {
	seen := map[string]bool{}
	for _, m := range signatureObjectRE.FindAllSubmatch(body, -1) {
		seen[string(m[1])] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func isJSON(ct string) bool {
	mt := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

func isText(ct string) bool {
	mt := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	return strings.HasPrefix(mt, "text/") || mt == "application/x-pem-file" || mt == "application/xml" || strings.HasSuffix(mt, "+xml")
}

// strip walks a decoded JSON value. at is the JSON pointer of v; a time member taken out is recorded in times under its pointer.
func strip(v any, at string, times map[string]any, rw Rewrite) any {
	switch x := v.(type) {
	case map[string]any:
		return stripObject(x, at, times, rw)
	case []any:
		for i := range x {
			x[i] = strip(x[i], at+"/"+strconv.Itoa(i), times, rw)
		}
		return x
	case string:
		return rw.value(x)
	}
	return v
}

// principalRE is a principal's reference as the main installation writes it (user:<name>, sa:<name>, ci:<name>): a value that is
// one names who published or acted, an internal identity (B.10), such as npm's publisher of a version published to a local
// repository.
var principalRE = regexp.MustCompile(`^(user|sa|ci|bastion):[^\s]+$`)

// catalogueLinks are the management API's paths a Bastion serves from its catalogue: the signature objects and the anchors to verify
// them offline (R-BAS-103).
func catalogueLinks() []string { return []string{"/api/v1/signatures/objects/", "/api/v1/signing/"} }

// value post-processes one string member: a principal's reference is no value (nil); a link of the management API is the Bastion's
// data path when the catalogue holds what it names, and no value when it does not (a file's content address, which only the main
// installation serves); any other text has its links rewritten.
func (rw Rewrite) value(s string) any {
	if principalRE.MatchString(s) {
		return nil
	}
	p := s
	for _, m := range append([]string{rw.Base}, rw.Main...) {
		if m != "" && strings.HasPrefix(p, m+"/api/v1/") {
			p = strings.TrimPrefix(p, m)
			break
		}
	}
	if strings.HasPrefix(p, "/api/v1/") && !strings.ContainsAny(p, " \n\t<>\"") {
		for _, l := range catalogueLinks() {
			if strings.HasPrefix(p, l) {
				return rw.text(s)
			}
		}
		return nil
	}
	return rw.text(s)
}

func stripObject(o map[string]any, at string, times map[string]any, rw Rewrite) map[string]any {
	st, counters, triage := stateTimes(), downloadCounters(), triageMembers()
	_, malwareCheck := o["scannedAt"]
	for k, val := range o {
		ptr := at + "/" + escapePointer(k)
		switch {
		case st[k] || (malwareCheck && (k == "database" || k == "databaseBuiltAt")):
			if val != nil {
				times[ptr] = val
			}
			delete(o, k)
		case counters[k] || triage[k]:
			delete(o, k)
		case k == "releasedBy":
			o[k] = nil // the principal who released a blob or a version (B.10); the member is nullable in every shape
		case k == "upstreamUrl" || k == "fetchedFrom":
			o[k] = rw.upstream(val)
		case k == "origin":
			o[k] = stripOrigin(val, ptr, times, rw)
		}
	}
	// An internal trust anchor's title names the company's keys (B.10); the anchors Quader ships keep theirs, the state words stay.
	if scope, ok := o["scope"].(string); ok && scope != "shipped" {
		if _, has := o["title"]; has {
			if _, anchor := o["id"]; anchor {
				delete(o, "title")
			}
		}
	}
	for k, val := range o {
		if k == "origin" {
			continue
		}
		o[k] = strip(val, at+"/"+escapePointer(k), times, rw)
	}
	return o
}

// stripOrigin keeps of a signature's origin what it was and when, never who uploaded it or the CI job's claims, and the upstream
// only when it is a public registry (B.10). A member origin that is no object (a download event's "bastion") is a word and stays.
func stripOrigin(v any, at string, times map[string]any, rw Rewrite) any {
	o, ok := v.(map[string]any)
	if !ok {
		return strip(v, at, times, rw)
	}
	for k := range originPrincipal() {
		delete(o, k)
	}
	if u, has := o["upstreamUrl"]; has {
		if s := rw.upstream(u); s == nil {
			delete(o, "upstreamUrl")
		} else {
			o["upstreamUrl"] = s
		}
	}
	return strip(o, at, times, rw)
}

// upstream keeps an upstream URL whose host is a public registry, and answers nil for any other (B.10).
func (rw Rewrite) upstream(v any) any {
	s, ok := v.(string)
	if !ok || s == "" {
		return v
	}
	u, err := url.Parse(s)
	if err != nil || !rw.PublicRegistries[strings.ToLower(u.Hostname())] {
		return nil
	}
	return s
}

// dataPath is the Bastion's path of the documents a page reads, which replaces the main installation's /api/v1/ (B.13).
const dataPath = "/ui/data/v1/"

// text rewrites the links of one text: every main installation's origin becomes the Bastion's base URL, and the management API's
// path below the base URL, or at the start of a value, becomes the Bastion's data path.
func (rw Rewrite) text(s string) string {
	for _, m := range rw.Main {
		if m != "" && m != rw.Base && strings.Contains(s, m) {
			s = strings.ReplaceAll(s, m, rw.Base)
		}
	}
	if !strings.Contains(s, "/api/v1/") {
		return s
	}
	if rw.Base != "" {
		s = strings.ReplaceAll(s, rw.Base+"/api/v1/", rw.Base+dataPath)
	}
	if strings.HasPrefix(s, "/api/v1/") {
		s = dataPath + strings.TrimPrefix(s, "/api/v1/")
	}
	for _, q := range []string{`"/api/v1/`, `'/api/v1/`, `(/api/v1/`, ` /api/v1/`} {
		s = strings.ReplaceAll(s, q, q[:1]+dataPath)
	}
	return s
}

// escapePointer escapes a member name for a JSON pointer (RFC 6901).
func escapePointer(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}
