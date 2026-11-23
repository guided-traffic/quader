package repository

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// scopeDoc validates a remote document of the format "mirrorable" with its mirror's scope, remote.mirror.include ("" for none,
// remote-curation.md R-CUR-017), set into the remote section. A remote has no include and exclude patterns (R-CUR-002).
func scopeDoc(t *testing.T, include, remote string) (Document, *validation) {
	t.Helper()
	if include != "" {
		var section map[string]any
		if err := json.Unmarshal([]byte(remote), &section); err != nil {
			t.Fatal(err)
		}
		mirror, _ := section["mirror"].(map[string]any)
		if mirror == nil {
			mirror = map[string]any{}
		}
		var list []string
		if err := json.Unmarshal([]byte(include), &list); err != nil {
			t.Fatal(err)
		}
		mirror["include"] = list
		section["mirror"] = mirror
		raw, _ := json.Marshal(section)
		remote = string(raw)
	}
	body := `{"key":"m-remote","class":"remote","format":"mirrorable","remote":` + remote + `}`
	d, err := DecodeDocument([]byte(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, v := validate(mirrorCatalog(), &d, true)
	return d, v
}

// restrictedScopeFields returns every violation of a Owner's update from the stored to the next document.
func restrictedScopeFields(t *testing.T, stored, next [2]string) map[string]string {
	t.Helper()
	cur, v := scopeDoc(t, stored[0], stored[1])
	if err := v.err(); err != nil {
		t.Fatalf("stored: %v", err)
	}
	nd, v := scopeDoc(t, next[0], next[1])
	if err := v.err(); err != nil {
		t.Fatalf("next: %v", err)
	}
	restrictedRemote(v, Caller{Restricted: true}, &cur, &nd)
	return fieldCodes(v)
}

// R-REPO-050, R-REPO-052, Q-REPO-142 (Q-REPO-193, remote-curation.md R-CUR-017): a Owner keeps what an administrator chose for a mirror
// that stays a mirror: the scope (remote.mirror.include, kept with the rest of remote.mirror), the url and the extra hosts decide what
// the mirror stores and from where.
func TestR_REPO_050_RestrictedWriteKeepsTheScopeOfAMirror(t *testing.T) {
	const (
		remote = `{"url":"https://registry.example.org/","mode":"mirror"}`
		hosts  = `{"url":"https://registry.example.org/","mode":"mirror","extraHosts":[{"pattern":"cdn.example.org","sendCredentials":false}]}`
		other  = `{"url":"https://partner.example.test/","mode":"mirror"}`
		pull   = `{"url":"https://registry.example.org/","mode":"pullThrough"}`
		paused = `{"url":"https://registry.example.org/","mode":"mirror","mirror":{"paused":true}}`
		acme   = `["@acme/*"]`
	)
	var wide []string
	for c := 'a'; c <= 'z'; c++ {
		wide = append(wide, `"`+string(c)+`*"`)
	}
	widePatterns := "[" + strings.Join(wide, ",") + "]"
	restricted := func(pointers ...string) map[string]string {
		m := map[string]string{}
		for _, p := range pointers {
			m[p] = "mirror_restricted"
		}
		return m
	}
	for _, tc := range []struct {
		name         string
		stored, next [2]string
		want         map[string]string
	}{
		{"widening the scope is refused", [2]string{acme, remote}, [2]string{widePatterns, remote}, restricted("/remote/mirror")},
		{"narrowing it is refused too", [2]string{`["@acme/*","@beta/*"]`, remote}, [2]string{acme, remote}, restricted("/remote/mirror")},
		{"changing the url is refused", [2]string{acme, remote}, [2]string{acme, other}, restricted("/remote/url")},
		{"adding an extra host is refused", [2]string{acme, remote}, [2]string{acme, hosts}, restricted("/remote/extraHosts")},
		{"the same scope in another order is kept", [2]string{`["@acme/*","@beta/*"]`, remote}, [2]string{`["@beta/*","@acme/*"]`, remote}, map[string]string{}},
		{"the stored mirror sent back is kept", [2]string{acme, hosts}, [2]string{acme, hosts}, map[string]string{}},
		{"pausing is allowed", [2]string{acme, remote}, [2]string{acme, paused}, map[string]string{}},
		{"a write that turns the mode back to pullThrough may change the url", [2]string{acme, remote}, [2]string{acme, pull}, map[string]string{}},
		{"a stored pullThrough remote keeps its freedom", [2]string{acme, pull}, [2]string{acme, `{"url":"https://partner.example.test/"}`}, map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := restrictedScopeFields(t, tc.stored, tc.next); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("violations = %v, want %v", got, tc.want)
			}
		})
	}
}

// R-REPO-052 (Q-REPO-194): a url with a query is not the well-known upstream for the bound rule, and a mirror refuses it,
// because the formats' requests drop the query; a remote in pullThrough keeps whatever it stored.
func TestR_REPO_052_AMirrorUrlCarriesNoQuery(t *testing.T) {
	for _, u := range []string{"https://registry.example.org/?", "https://registry.example.org/?x=1", "https://partner.example.test/?token=1"} {
		t.Run(u, func(t *testing.T) {
			_, v := scopeDoc(t, `["@acme/*"]`, `{"url":"`+u+`","mode":"mirror"}`)
			if got := fieldCodes(v)["/remote/url"]; got != "invalid_value" {
				t.Fatalf("/remote/url = %q (%v)", got, fieldCodes(v))
			}
		})
		t.Run("pullThrough "+u, func(t *testing.T) {
			_, v := scopeDoc(t, "", `{"url":"`+u+`"}`)
			if err := v.err(); err != nil {
				t.Fatalf("a stored pullThrough url is refused: %v", err)
			}
		})
	}
	// The well-known upstream written in other spellings is still the well-known upstream and needs its bound.
	for _, u := range []string{"HTTPS://Registry.Example.org:443", "https://registry.example.org"} {
		_, v := scopeDoc(t, "", `{"url":"`+u+`","mode":"mirror"}`)
		if got := fieldCodes(v)["/remote/mode"]; got != "mirror_unbounded" {
			t.Errorf("%s: /remote/mode = %q", u, got)
		}
	}
}
