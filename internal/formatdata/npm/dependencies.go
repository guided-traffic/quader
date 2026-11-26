package npm

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/packages/dependents"
)

// npmKinds are the fields of a manifest that name the packages a version depends on, each a kind of its own (R-API-086, Q-API-455): an
// install fetches dependencies, peerDependencies and optionalDependencies, a development checkout devDependencies.
// bundleDependencies names packages that dependencies holds already, so it adds no name.
var npmKinds = [...]string{"dependencies", "peerDependencies", "optionalDependencies", "devDependencies"}

// Dependencies reads the names an npm version depends on from its manifest (R-API-086): each field of npmKinds is a map of name to
// range. A name is matched as npm matches it, exactly (the name rule R-NPM-002, case kept); an alias ("npm:<name>@<range>") depends on
// the package it names, with its range. A name the rule refuses, and a value that is no string, are no dependency.
func Dependencies(manifest []byte) []dependents.Dependency {
	var m map[string]json.RawMessage
	if json.Unmarshal(manifest, &m) != nil {
		return nil
	}
	var out []dependents.Dependency
	for _, kind := range npmKinds {
		var deps map[string]json.RawMessage
		if json.Unmarshal(m[kind], &deps) != nil {
			continue
		}
		for key, raw := range deps {
			var rng string
			if json.Unmarshal(raw, &rng) != nil {
				continue
			}
			target, rangeOf := key, rng
			if alias, ok := strings.CutPrefix(rng, "npm:"); ok {
				target, rangeOf = alias, ""
				if at := strings.LastIndex(alias, "@"); at > 0 {
					target, rangeOf = alias[:at], alias[at+1:]
				}
			}
			if !validName(target) {
				continue
			}
			out = append(out, dependents.Dependency{Name: target, Kind: kind, Range: rangeOf})
		}
	}
	return out
}

// validName is the lookup rule of an npm name (R-NPM-002, the edge's name.Parse, which this domain package may not import): at most one
// "/", which then separates a scope starting with "@" from the name; no empty, "." or ".." segment; no control character, backslash or
// "%"; valid UTF-8; at most 512 bytes. Case is kept.
func validName(s string) bool {
	if s == "" || len(s) > 512 || !utf8.ValidString(s) || strings.ContainsAny(s, "\\%") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	segs := strings.Split(s, "/")
	switch {
	case len(segs) > 2, len(segs) == 2 && (!strings.HasPrefix(segs[0], "@") || segs[0] == "@"):
		return false
	}
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// DependencySource reads the dependencies of npm versions from their stored manifests, for the task remote-describe (R-API-086).
func DependencySource(ctx context.Context, qs *q.Queries, versionIDs []int64) (map[int64][]dependents.Dependency, error) {
	rows, err := qs.PackageDependenciesNPMManifests(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]dependents.Dependency, len(rows))
	for _, r := range rows {
		out[r.VersionID] = Dependencies(r.Manifest)
	}
	return out, nil
}
