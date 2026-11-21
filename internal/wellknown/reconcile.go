package wellknown

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/catalog"
)

// The reconcile of `quader migrate` (R-REPO-067, Q-REPO-374): it brings the repositories of the project upstream in step with the
// declarations of the running binary's catalog. What it decides is here and needs no database ([Plan]); the writes are a [Store]'s,
// which the repository service implements over the migration session's transaction with the actor system:catalog.

// Address is the product's part of a repository's address (R-REPO-069): remote.url, remote.extraHosts and the members of the format's
// settings member that a preset carries (deb.upstreamKeys).
type Address struct {
	URL        string
	ExtraHosts []catalog.PresetHost
	// Settings holds the members of the format's settings member; only the members a declaration carries are compared.
	Settings json.RawMessage
}

// Fixed are the members the product fixes on a system repository beyond its address (R-REPO-070).
type Fixed struct {
	IncludePatterns      []string
	Private              bool
	ScanningOff          bool
	UnusedRetentionHours int
}

// Stored is a repository of the project upstream as the reconcile reads it: its identity, its row of upstream_repository and the
// members the product holds, as they are stored.
type Stored struct {
	Key    string
	Format catalog.ID
	Class  catalog.Class
	Kind   Kind
	// Retired, Overridden: retired_at is set; address_overridden is true.
	Retired    bool
	Overridden bool
	// HasAccount: an account is stored (an auth mode other than none, or a password).
	HasAccount bool
	Address    Address
	Fixed      Fixed
}

// Write says what a write of the reconcile changes: the product's address (step 2), the fixed members of a system repository, and
// whether the stored account goes with it because the hosts that receive it moved (Q-REPO-377).
type Write struct {
	Address       bool
	Fixed         bool
	RemoveAccount bool
}

// Store is where the reconcile reads and writes. Every method runs in the one transaction of the reconcile; an error ends it and
// nothing is kept.
type Store interface {
	// Stored lists the repositories of the project upstream.
	Stored(ctx context.Context) ([]Stored, error)
	// Create creates a missing repository with its row (step 1) and records repository.created.
	Create(ctx context.Context, d Declaration) error
	// Write writes the product's members of d that w names in one update (step 2) and records repository.updated with
	// reason catalog.
	Write(ctx context.Context, d Declaration, w Write) error
	// SetOverridden sets address_overridden (step 3 clears it when the product's address caught up), with no event.
	SetOverridden(ctx context.Context, key string, overridden bool) error
	// Retire retires a repository the catalog no longer declares (step 5): retired_at, disabled with repository.disabled, the purge.
	Retire(ctx context.Context, key string) error
	// Unretire clears retired_at of a repository the catalog declares again (step 4); it stays switched off.
	Unretire(ctx context.Context, key string) error
}

// Op is one kind of step of a plan.
type Op string

// The steps of R-REPO-067.
const (
	OpCreate        Op = "create"
	OpUnretire      Op = "unretire"
	OpWrite         Op = "write"
	OpClearOverride Op = "clear-override"
	OpRetire        Op = "retire"
)

// Step is one write of a plan.
type Step struct {
	Op    Op
	Key   string
	Decl  Declaration
	Write Write
}

// Result is what a reconcile did, by step; the keys in the order of the plan.
type Result struct {
	Steps []Step
}

// Keys returns the keys of the steps of one op.
func (r Result) Keys(op Op) []string {
	var out []string
	for _, s := range r.Steps {
		if s.Op == op {
			out = append(out, s.Key)
		}
	}
	return out
}

// Reconcile applies the plan of decls against what s holds. A plan that cannot be applied (step 6) writes nothing.
func Reconcile(ctx context.Context, s Store, decls []Declaration) (Result, error) {
	stored, err := s.Stored(ctx)
	if err != nil {
		return Result{}, err
	}
	steps, err := Plan(decls, stored)
	if err != nil {
		return Result{}, err
	}
	for _, st := range steps {
		if err := apply(ctx, s, st); err != nil {
			return Result{}, fmt.Errorf("wellknown: %s %s: %w", st.Op, st.Key, err)
		}
	}
	return Result{Steps: steps}, nil
}

func apply(ctx context.Context, s Store, st Step) error {
	switch st.Op {
	case OpCreate:
		return s.Create(ctx, st.Decl)
	case OpUnretire:
		return s.Unretire(ctx, st.Key)
	case OpWrite:
		return s.Write(ctx, st.Decl, st.Write)
	case OpClearOverride:
		return s.SetOverridden(ctx, st.Key, false)
	case OpRetire:
		return s.Retire(ctx, st.Key)
	}
	return fmt.Errorf("unknown step %q", st.Op)
}

// Plan decides the steps of R-REPO-067 for the declarations against the stored repositories, in the order of the declarations and
// then the retirements by key. A declaration whose key the project holds as a repository of another format, class or kind (the class
// the declaration names, Q-REPO-411) is step 6:
// an error that names the key, and no step. A second plan against the state the first one left is empty.
func Plan(decls []Declaration, stored []Stored) ([]Step, error) {
	byKey := make(map[string]Stored, len(stored))
	for _, s := range stored {
		byKey[s.Key] = s
	}
	var steps []Step
	declared := map[string]bool{}
	for _, d := range decls {
		declared[d.Key] = true
		s, ok := byKey[d.Key]
		if !ok {
			steps = append(steps, Step{Op: OpCreate, Key: d.Key, Decl: d})
			continue
		}
		if s.Format != d.Format || s.Class != d.Class || s.Kind != d.Kind {
			return nil, fmt.Errorf("wellknown: the catalog declares upstream/%s as a %s %s repository of the format %s, and the project "+
				"upstream holds it as a %s %s repository of the format %s; fix the catalog (R-REPO-067 step 6, Q-REPO-387)",
				d.Key, d.Kind, d.Class, d.Format, s.Kind, s.Class, s.Format)
		}
		if s.Retired {
			steps = append(steps, Step{Op: OpUnretire, Key: d.Key, Decl: d})
		}
		steps = append(steps, planHeld(d, s)...)
	}
	var gone []string
	for _, s := range stored {
		if !declared[s.Key] && !s.Retired {
			gone = append(gone, s.Key)
		}
	}
	sort.Strings(gone)
	for _, k := range gone {
		steps = append(steps, Step{Op: OpRetire, Key: k})
	}
	return steps, nil
}

// planHeld decides steps 2 and 3 for a repository the catalog declares: the product's address while no administrator overrode it,
// the fixed members always, and the override flag once the product's address caught up with the administrator's.
func planHeld(d Declaration, s Stored) []Step {
	addressDiffers := !AddressEqual(d.Address(), s.Address)
	// A preset repository's patterns, level and scanning are an administrator's (R-REPO-069): only a system repository has fixed ones.
	w := Write{Fixed: d.Kind == KindSystem && !FixedEqual(d.Fixed(), s.Fixed)}
	var out []Step
	switch {
	case s.Overridden && !addressDiffers:
		out = append(out, Step{Op: OpClearOverride, Key: d.Key, Decl: d})
	case !s.Overridden && addressDiffers:
		w.Address = true
		w.RemoveAccount = s.HasAccount && !slices.Equal(CredentialHosts(d.Address()), CredentialHosts(s.Address))
	}
	if w.Address || w.Fixed {
		out = append(out, Step{Op: OpWrite, Key: d.Key, Decl: d, Write: w})
	}
	return out
}

// AddressEqual reports whether a stored address is the declared one: the url, the extra hosts in their order, and every member of
// the format's settings member that the declaration carries, compared by value.
func AddressEqual(declared, stored Address) bool {
	if declared.URL != stored.URL || !hostsEqual(declared.ExtraHosts, stored.ExtraHosts) {
		return false
	}
	want := members(declared.Settings)
	have := members(stored.Settings)
	for k, v := range want {
		if !sameJSON(v, have[k]) {
			return false
		}
	}
	return true
}

// FixedEqual reports whether the fixed members of a system repository are the declared ones.
func FixedEqual(declared, stored Fixed) bool {
	return slices.Equal(nonNil(declared.IncludePatterns), nonNil(stored.IncludePatterns)) && declared.Private == stored.Private &&
		declared.ScanningOff == stored.ScanningOff && declared.UnusedRetentionHours == stored.UnusedRetentionHours
}

// CredentialHosts are the hosts that receive a repository's credential (R-REPO-033): the origin of the url and the patterns of the
// extra hosts with sendCredentials, sorted. A url that does not parse is its own text, so a change of it is a change.
func CredentialHosts(a Address) []string {
	origin := a.URL
	if u, err := url.Parse(a.URL); err == nil && u.Host != "" {
		origin = strings.ToLower(u.Scheme + "://" + u.Host)
	}
	out := []string{origin}
	for _, h := range a.ExtraHosts {
		if h.SendCredentials {
			out = append(out, h.Pattern)
		}
	}
	sort.Strings(out[1:])
	return out
}

func hostsEqual(a, b []catalog.PresetHost) bool {
	return slices.Equal(nonNilHosts(a), nonNilHosts(b))
}

func nonNilHosts(h []catalog.PresetHost) []catalog.PresetHost {
	if h == nil {
		return []catalog.PresetHost{}
	}
	return h
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// members reads a settings member as its members; nil for none or one that is no object.
func members(raw json.RawMessage) map[string]json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// sameJSON compares two JSON values by value: the same after decoding, whatever their spacing and member order.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	ca, _ := json.Marshal(x) // a decoded value always marshals
	cb, _ := json.Marshal(y)
	return bytes.Equal(ca, cb)
}
