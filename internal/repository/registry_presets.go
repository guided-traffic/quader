package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The installation's registry presets (R-REPO-076, Q-REPO-417 to Q-REPO-419): the list an administrator keeps of the company's own
// registries, which the form of a new remote repository offers as buttons in every project. A preset is a value the form puts into its
// fields (Q-REPO-418): no repository refers to it, and a create of its URL stores no host the request does not name. It holds no
// credential: the API reads the body strictly, and a preset has no member that could carry one.

const (
	// MaxRegistryPresetsPerFormat bounds the list per format (R-REPO-076).
	MaxRegistryPresetsPerFormat = 10
	// maxPresetName is the most characters of a preset's display name.
	maxPresetName = 64
)

// RegistryPreset is one entry of the list: the format, the button's label, the upstream URL as R-REPO-024 normalizes it and the further
// hosts with whether each receives the credential.
type RegistryPreset struct {
	Format      string      `json:"format"`
	DisplayName string      `json:"displayName"`
	URL         string      `json:"url"`
	ExtraHosts  []ExtraHost `json:"extraHosts"`
}

// RegistryPresets is the stored list and its version, the ETag source.
type RegistryPresets struct {
	Presets []RegistryPreset
	Version int64
}

// CheckRegistryPresets validates a list and returns it in its stored form: names trimmed, URLs normalized, host patterns trimmed and
// in lower case, never a nil host list. Every problem is a field error of one 422 validation_failed at its pointer (R-REPO-076).
func CheckRegistryPresets(cat *catalog.Registry, in []RegistryPreset) ([]RegistryPreset, error) {
	v := &validation{}
	out := make([]RegistryPreset, 0, len(in))
	perFormat := map[string]int{}
	for i, p := range in {
		at := "/presets/" + strconv.Itoa(i)
		p = checkRegistryPreset(cat, v, at, p, out)
		perFormat[p.Format]++
		out = append(out, p)
	}
	for _, format := range sortedKeys(perFormat) {
		if perFormat[format] > MaxRegistryPresetsPerFormat {
			v.add("/presets", codeOutOfRange, fmt.Sprintf("at most %d presets of the format %s", MaxRegistryPresetsPerFormat, format))
		}
	}
	if err := v.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// checkRegistryPreset validates one preset against the format and the presets before it in the list.
func checkRegistryPreset(cat *catalog.Registry, v *validation, at string, p RegistryPreset, before []RegistryPreset) RegistryPreset {
	p.Format = strings.TrimSpace(p.Format)
	info, known := cat.Lookup(p.Format)
	switch {
	case !known:
		v.add(at+"/format", errs.CodeUnknownFormat, fmt.Sprintf("format %q is not in the format catalog", p.Format))
	case !info.Supports(catalog.Remote):
		v.add(at+"/format", errs.CodeUnsupportedClass, "the "+p.Format+" format has no remote repositories")
	}
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	if n := utf8.RuneCountInString(p.DisplayName); n == 0 || n > maxPresetName {
		v.add(at+"/displayName", codeInvalidValue, fmt.Sprintf("1 to %d characters", maxPresetName))
	}
	if norm, why := NormalizeUpstreamURL(strings.TrimSpace(p.URL)); why != "" {
		v.add(at+"/url", codeInvalidUpstreamURL, why)
	} else {
		p.URL = norm
		if known {
			if wk, ok := info.PresetFor(norm); ok {
				v.add(at+"/url", codeInvalidValue, "is the address of the well-known registry "+wk.DisplayName+", which has its button already")
			}
		}
	}
	for _, o := range before {
		if o.Format != p.Format {
			continue
		}
		if p.DisplayName != "" && strings.EqualFold(o.DisplayName, p.DisplayName) {
			v.add(at+"/displayName", codeInvalidValue, "another preset of the format has this name")
		}
		if p.URL != "" && o.URL == p.URL {
			v.add(at+"/url", codeInvalidValue, "another preset of the format has this address")
		}
	}
	p.ExtraHosts = checkPresetHosts(v, at+"/extraHosts", p.ExtraHosts)
	return p
}

// checkPresetHosts validates the further hosts of a preset by the rules of a remote's extraHosts (R-REPO-024), each pattern once.
func checkPresetHosts(v *validation, at string, in []ExtraHost) []ExtraHost {
	out := make([]ExtraHost, 0, len(in))
	if len(in) > maxExtraHosts {
		v.add(at, codeOutOfRange, "at most "+strconv.Itoa(maxExtraHosts)+" entries")
	}
	seen := map[string]bool{}
	for i, h := range in {
		h.Pattern = strings.ToLower(strings.TrimSpace(h.Pattern))
		if why := checkHostPattern(h.Pattern); why != "" {
			v.add(at+"/"+strconv.Itoa(i)+"/pattern", codeInvalidPattern, why)
		} else if seen[h.Pattern] {
			v.add(at+"/"+strconv.Itoa(i)+"/pattern", codeInvalidValue, "the host is named twice")
		}
		seen[h.Pattern] = true
		out = append(out, h)
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// decodeRegistryPresets reads the stored list; a stored host list is never nil.
func decodeRegistryPresets(raw []byte) ([]RegistryPreset, error) {
	var out []RegistryPreset
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("repository: read the registry presets: %w", err)
	}
	for i := range out {
		out[i].ExtraHosts = nonNilHosts(out[i].ExtraHosts)
	}
	if out == nil {
		out = []RegistryPreset{}
	}
	return out, nil
}

// readRegistryPresets reads the row in the transaction of qs.
func readRegistryPresets(ctx context.Context, qs *q.Queries) (RegistryPresets, error) {
	rows, err := qs.RegistryPresetsGet(ctx)
	if err != nil {
		return RegistryPresets{}, err
	}
	if len(rows) != 1 {
		return RegistryPresets{}, errors.New("repository: the registry presets row is missing")
	}
	list, err := decodeRegistryPresets(rows[0].Presets)
	if err != nil {
		return RegistryPresets{}, err
	}
	return RegistryPresets{Presets: list, Version: rows[0].Version}, nil
}

// RegistryPresets returns the installation's list, read from PostgreSQL and not from a cache (R-REPO-076).
func (s *Service) RegistryPresets(ctx context.Context) (RegistryPresets, error) {
	var out RegistryPresets
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		out, err = readRegistryPresets(ctx, qs)
		return err
	})
	return out, err
}

// SetRegistryPresets replaces the list under the version check: expected is the version the client saw (errs.AnyRevision is the
// deliberate blind write); 412 for a stale one, 422 for a list that fails CheckRegistryPresets. A list that differs from the stored one
// is written with settings.registry_presets.changed in the same transaction; one that does not records nothing and keeps the version.
// No repository changes: nothing follows a preset (Q-REPO-418).
func (s *Service) SetRegistryPresets(ctx context.Context, c Caller, expected int64, in []RegistryPreset) (RegistryPresets, error) {
	list, err := CheckRegistryPresets(s.cat, in)
	if err != nil {
		return RegistryPresets{}, err
	}
	after, err := json.Marshal(list)
	if err != nil {
		return RegistryPresets{}, err
	}
	var out RegistryPresets
	err = s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.RegistryPresetsLock(ctx)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return errors.New("repository: the registry presets row is missing")
		}
		cur := rows[0]
		if expected != errs.AnyRevision && expected != cur.Version {
			return errs.Stale("The registry presets", cur.Version)
		}
		before, err := decodeRegistryPresets(cur.Presets)
		if err != nil {
			return err
		}
		out = RegistryPresets{Presets: list, Version: cur.Version}
		if registryPresetsEqual(before, list) {
			out.Presets = before
			return nil
		}
		version, err := qs.RegistryPresetsSet(ctx, q.RegistryPresetsSetParams{Presets: after, Actor: c.principalID()})
		if err != nil {
			return fmt.Errorf("repository: write the registry presets: %w", err)
		}
		if len(version) == 1 {
			out.Version = version[0]
		}
		return s.rec.RecordTx(ctx, tx, registryPresetsEvent(c, before, list))
	})
	return out, err
}

func registryPresetsEqual(a, b []RegistryPreset) bool {
	return slices.EqualFunc(a, b, func(x, y RegistryPreset) bool {
		return x.Format == y.Format && x.DisplayName == y.DisplayName && x.URL == y.URL && slices.Equal(x.ExtraHosts, y.ExtraHosts)
	})
}

// registryPresetsEvent is settings.registry_presets.changed with the list before and after (R-API-084).
func registryPresetsEvent(c Caller, before, after []RegistryPreset) audit.Event {
	details, err := json.Marshal(map[string][]RegistryPreset{"before": before, "after": after})
	if err != nil {
		panic(err) // strings, booleans and slices of them always marshal
	}
	return audit.Event{
		Type: audit.EventRegistryPresetsChanged, Actor: c.Actor, Source: c.Source, Outcome: audit.Success,
		Subject: audit.Subject{Kind: "settings", Name: "registry_presets"}, Details: details,
	}
}

// registryPresetsOf returns the presets of one format in the list's order, for the defaults of a new remote repository (R-REPO-063).
func registryPresetsOf(ctx context.Context, qs *q.Queries, format string) ([]RegistryPreset, error) {
	all, err := readRegistryPresets(ctx, qs)
	if err != nil {
		return nil, err
	}
	out := []RegistryPreset{}
	for _, p := range all.Presets {
		if p.Format == format {
			out = append(out, p)
		}
	}
	return out, nil
}
