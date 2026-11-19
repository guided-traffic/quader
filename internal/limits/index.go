package limits

import (
	"net/netip"
	"sort"

	"github.com/guided-traffic/quader/internal/qname"
)

// Index is the compiled form of the rules, profiles, project maxima, ceilings and settings (R-LIM-013): resolution runs on
// it in memory and reads no database. It is immutable once built; a change of the area limits builds a new one.
type Index struct {
	profiles map[string]*Profile
	instance *placeRules
	projects map[string]*placeRules     // by project key
	repos    map[qname.Repo]*placeRules // by repository name
	maxima   map[string]*Maximum        // by project key
	ceilings map[int64][2]*Ceiling      // anonymous, signed-in
	settings Settings
	rules    map[string]*Rule // by ID
}

// placeRules are the rules of one place, by the steps of resolution.
type placeRules struct {
	place  Place
	single map[string]*Rule // "user:<id>", "technical_token:<id>", "ci_project:<issuer>:<project>"
	groups map[string]*Rule // by group ID
	addrs  []*Rule          // longest prefix first
	every  map[string]*Rule // by caller kind
}

func newPlaceRules(p Place) *placeRules {
	return &placeRules{place: p, single: map[string]*Rule{}, groups: map[string]*Rule{}, every: map[string]*Rule{}}
}

// NewIndex compiles a snapshot. A rule that names a profile that does not exist, or an address that does not parse, matches
// nothing; the store's foreign keys and checks keep both from happening.
func NewIndex(s Snapshot) *Index {
	x := &Index{
		profiles: map[string]*Profile{}, instance: newPlaceRules(InstancePlace()),
		projects: map[string]*placeRules{}, repos: map[qname.Repo]*placeRules{}, maxima: map[string]*Maximum{},
		ceilings: map[int64][2]*Ceiling{}, settings: s.Settings, rules: map[string]*Rule{},
	}
	if x.settings.Mode == "" {
		x.settings = DefaultSettings()
	}
	for i := range s.Profiles {
		p := s.Profiles[i]
		x.profiles[p.Key] = &p
	}
	for i := range s.Rules {
		r := s.Rules[i]
		if _, ok := x.profiles[r.Profile]; !ok {
			continue
		}
		pr := x.placeOf(r.Place)
		switch r.WhoKind {
		case WhoUser, WhoTechnicalToken, WhoCIProject:
			pr.single[string(r.WhoKind)+":"+r.WhoRef] = &r
		case WhoGroup:
			pr.groups[r.WhoRef] = &r
		case WhoEvery:
			pr.every[r.WhoRef] = &r
		case WhoAddress:
			p, ok := CanonicalPrefix(r.WhoRef)
			if !ok {
				continue
			}
			r.prefix = p
			pr.addrs = append(pr.addrs, &r)
		default:
			continue
		}
		x.rules[r.ID] = &r
	}
	for _, pr := range x.allPlaces() {
		sort.SliceStable(pr.addrs, func(i, j int) bool { return pr.addrs[i].prefix.Bits() > pr.addrs[j].prefix.Bits() })
	}
	for i := range s.Maxima {
		m := s.Maxima[i]
		if _, ok := x.profiles[m.Profile]; ok {
			x.maxima[m.ProjectKey] = &m
		}
	}
	for i := range s.Ceilings {
		c := s.Ceilings[i]
		pair := x.ceilings[c.RepoID]
		if c.Class == CeilingAnonymous {
			pair[0] = &c
		} else {
			pair[1] = &c
		}
		x.ceilings[c.RepoID] = pair
	}
	return x
}

func (x *Index) placeOf(p Place) *placeRules {
	switch p.Kind {
	case PlaceProject:
		pr := x.projects[p.ProjectKey]
		if pr == nil {
			pr = newPlaceRules(p)
			x.projects[p.ProjectKey] = pr
		}
		return pr
	case PlaceRepository:
		pr := x.repos[p.Repo]
		if pr == nil {
			pr = newPlaceRules(p)
			x.repos[p.Repo] = pr
		}
		return pr
	}
	return x.instance
}

func (x *Index) allPlaces() []*placeRules {
	out := []*placeRules{x.instance}
	for _, p := range x.projects {
		out = append(out, p)
	}
	for _, p := range x.repos {
		out = append(out, p)
	}
	return out
}

// Settings are the mode and the block time.
func (x *Index) Settings() Settings {
	if x == nil {
		return DefaultSettings()
	}
	return x.settings
}

// Profile returns a profile by key.
func (x *Index) Profile(key string) (*Profile, bool) {
	if x == nil {
		return nil, false
	}
	p, ok := x.profiles[key]
	return p, ok
}

// Rule returns a rule by ID.
func (x *Index) Rule(id string) (*Rule, bool) {
	if x == nil {
		return nil, false
	}
	r, ok := x.rules[id]
	return r, ok
}

// Ceiling returns the ceiling of a remote repository for a class; ok is false when none is set.
func (x *Index) Ceiling(repoID int64, class CeilingClass) (Ceiling, bool) {
	if x == nil {
		return Ceiling{}, false
	}
	pair := x.ceilings[repoID]
	c := pair[1]
	if class == CeilingAnonymous {
		c = pair[0]
	}
	if c == nil || (c.UncachedBytesPerHour <= 0 && c.UpstreamRequestsPerHour <= 0) {
		return Ceiling{}, false
	}
	return *c, true
}

// Maximum returns a project's maximum.
func (x *Index) Maximum(projectKey string) (*Maximum, bool) {
	if x == nil {
		return nil, false
	}
	m, ok := x.maxima[projectKey]
	return m, ok
}

// figuresOf is a rule's profile with its overrides, before the clamp.
func (x *Index) figuresOf(r *Rule) Figures {
	p := x.profiles[r.Profile]
	if p == nil {
		return Figures{}
	}
	return r.Overrides.Apply(p.Figures)
}

// Step is one step of resolution, for the explanation (R-LIM-035).
type Step struct {
	Place   Place
	Step    string // single, groups, kind
	Matched bool
	Rules   []*Rule
}

// Resolution is the outcome of R-LIM-013 for one request.
type Resolution struct {
	// Place is where resolution stopped; Rule the rule that applied (for several group rules, the first; GroupRules holds
	// them all).
	Place      Place
	Rule       *Rule
	GroupRules []*Rule
	// Figures are the effective figures: the profile, the overrides, the most generous of several group rules, the clamp.
	Figures Figures
	// Clamped marks the figures the clamp of R-LIM-016 lowered.
	Clamped [NumFigures]bool
	// Sources names, per figure that did not come from the rule's profile, override, group:<id> or clamp.
	Sources [NumFigures]string
	// Steps are the steps tried, when the caller asked for them.
	Steps []Step
}

// BudgetKey is the subject at the place (R-LIM-014): user:<id>@instance, user:<id>@project:<uuid>,
// user:<id>@repository:<id>.
func BudgetKey(s Subject, p Place) string { return s.String() + "@" + p.Key() }

// Resolve runs R-LIM-013 for subject s on the repository repo (zero: a request that names no repository, decided at the
// instance). trace records the steps.
func (x *Index) Resolve(s Subject, repo qname.Repo, trace bool) Resolution {
	var res Resolution
	places := make([]*placeRules, 0, 3)
	if !s.Admin && !repo.IsZero() {
		if pr := x.repos[repo]; pr != nil {
			places = append(places, pr)
		}
		if pr := x.projects[repo.Project]; pr != nil {
			places = append(places, pr)
		}
	}
	places = append(places, x.instance)
	for _, pr := range places {
		if x.resolveAt(pr, s, &res, trace) {
			return res
		}
	}
	// The instance holds the built-in `every` rules (R-LIM-012); a store without them leaves the subject unlimited.
	res.Place = InstancePlace()
	return res
}

// resolveAt runs step 3 of R-LIM-013 at one place.
func (x *Index) resolveAt(pr *placeRules, s Subject, res *Resolution, trace bool) bool {
	note := func(step string, matched bool, rules ...*Rule) {
		if trace {
			res.Steps = append(res.Steps, Step{Place: pr.place, Step: step, Matched: matched, Rules: rules})
		}
	}
	// 3.1 the single caller.
	var single *Rule
	switch s.Kind {
	case KindUser:
		single = pr.single[string(WhoUser)+":"+s.Ref]
	case KindTechnicalToken:
		single = pr.single[string(WhoTechnicalToken)+":"+s.Ref]
	case KindCI:
		single = pr.single[string(WhoCIProject)+":"+s.Ref]
	case KindAnonymous:
		if a, ok := addressOfSubject(s.Ref); ok {
			for _, r := range pr.addrs {
				if r.prefix.Contains(a) {
					single = r
					break
				}
			}
		}
	}
	if single != nil {
		note("single", true, single)
		x.apply(res, pr.place, single, s)
		return true
	}
	note("single", false)
	// 3.2 the groups.
	if s.Kind == KindUser || s.Kind == KindTechnicalToken {
		var matched []*Rule
		for _, g := range s.Groups {
			if r := pr.groups[g]; r != nil {
				matched = append(matched, r)
			}
		}
		if len(matched) > 0 {
			sort.SliceStable(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
			note("groups", true, matched...)
			x.applyGroups(res, pr.place, matched, s)
			return true
		}
		note("groups", false)
	}
	// 3.3 the kind.
	if r := pr.every[everyOf(s)]; r != nil {
		note("kind", true, r)
		x.apply(res, pr.place, r, s)
		return true
	}
	note("kind", false)
	return false
}

func (x *Index) apply(res *Resolution, p Place, r *Rule, s Subject) {
	res.Place, res.Rule = p, r
	base := x.figuresOf(r)
	for i := range res.Sources {
		if r.Overrides.Set[i] {
			res.Sources[i] = "override"
		}
	}
	res.Figures = base
	if r.Capped && p.Kind != PlaceInstance {
		cap := x.capOf(r, p, s)
		for i := range base {
			v := minFigure(base[i], cap[i])
			if v != base[i] {
				res.Clamped[i] = true
				res.Sources[i] = "clamp"
			}
			res.Figures[i] = v
		}
	}
}

func (x *Index) applyGroups(res *Resolution, p Place, rules []*Rule, s Subject) {
	res.Place, res.Rule, res.GroupRules = p, rules[0], rules
	var out Figures
	for n, r := range rules {
		var one Resolution
		x.apply(&one, p, r, s)
		if n == 0 {
			out = one.Figures
			res.Clamped = one.Clamped
		} else {
			out = MostGenerous(out, one.Figures)
		}
		if len(rules) > 1 {
			for i := range out {
				if out[i] == one.Figures[i] && res.Sources[i] == "" {
					res.Sources[i] = "group:" + r.WhoRef
				}
			}
		}
	}
	res.Figures = out
}

// instanceFigures is what the instance place gives a subject: step 3 of R-LIM-013 at the instance only.
func (x *Index) instanceFigures(s Subject) Figures {
	var res Resolution
	if x.resolveAt(x.instance, s, &res, false) {
		return res.Figures
	}
	return Figures{}
}

// capOf is the cap of a capped rule per figure (R-LIM-016): max(instance(W, f), maximum(f)), where W is the rule's "who".
func (x *Index) capOf(r *Rule, p Place, s Subject) Figures {
	inst := x.instanceFor(r, s)
	m, ok := x.maxima[p.ProjectKey]
	if !ok {
		return inst
	}
	max := x.maximumFigures(m)
	var out Figures
	for i := range out {
		out[i] = maxFigure(inst[i], max[i])
	}
	return out
}

// maximumFigures is a project's maximum: its profile with its overrides.
func (x *Index) maximumFigures(m *Maximum) Figures {
	p := x.profiles[m.Profile]
	if p == nil {
		return Figures{}
	}
	return m.Overrides.Apply(p.Figures)
}

// instanceFor is instance(W, f) of R-LIM-016 for the "who" of a rule.
func (x *Index) instanceFor(r *Rule, s Subject) Figures {
	inst := x.instance
	figures := func(rule *Rule) Figures {
		if rule == nil {
			return Figures{}
		}
		return x.figuresOf(rule)
	}
	switch r.WhoKind {
	case WhoUser, WhoTechnicalToken, WhoCIProject:
		subj := s
		if subj.Ref != r.WhoRef { // a rule evaluated for a subject it does not name (the API's check at write time)
			subj = Subject{Kind: subjectKindOfWho(r.WhoKind), Ref: r.WhoRef, Groups: s.Groups}
		}
		return x.instanceFigures(subj)
	case WhoGroup:
		if g := inst.groups[r.WhoRef]; g != nil {
			return figures(g)
		}
		return figures(inst.every[EveryUser])
	case WhoAddress:
		p, ok := CanonicalPrefix(r.WhoRef)
		if ok {
			for _, a := range inst.addrs {
				if a.prefix.Bits() <= p.Bits() && a.prefix.Contains(p.Addr()) {
					return figures(a)
				}
			}
		}
		return figures(inst.every[EveryAnonymous])
	case WhoEvery:
		return figures(inst.every[r.WhoRef])
	}
	return Figures{}
}

func subjectKindOfWho(k WhoKind) SubjectKind {
	switch k {
	case WhoTechnicalToken:
		return KindTechnicalToken
	case WhoCIProject:
		return KindCI
	}
	return KindUser
}

// CapFor is the cap a rule of an Owner (capped) is held to at a place, for the check at write time (R-LIM-016): the figures
// it would be clamped to now. groups are the current groups of a user the rule names.
func (x *Index) CapFor(r Rule, groups []string) Figures {
	if r.WhoKind == WhoAddress {
		if p, ok := CanonicalPrefix(r.WhoRef); ok {
			r.prefix = p
		}
	}
	s := Subject{Kind: subjectKindOfWho(r.WhoKind), Ref: r.WhoRef, Groups: groups}
	return x.capOf(&r, r.Place, s)
}

// EffectiveOf is a stored rule's effective figures now and the figures the clamp lowers (R-LIM-032 effective, clamped). For a
// single-caller rule the subject's groups are those passed; the API passes none.
func (x *Index) EffectiveOf(r Rule) (Figures, [NumFigures]bool) {
	var res Resolution
	if r.WhoKind == WhoAddress {
		if p, ok := CanonicalPrefix(r.WhoRef); ok {
			r.prefix = p
		}
	}
	x.apply(&res, r.Place, &r, Subject{Kind: subjectKindOfWho(r.WhoKind), Ref: r.WhoRef})
	return res.Figures, res.Clamped
}

// FiguresOfProfile is a profile's figures with overrides, for the API.
func (x *Index) FiguresOfProfile(key string, o Overrides) (Figures, bool) {
	p, ok := x.Profile(key)
	if !ok {
		return Figures{}, false
	}
	return o.Apply(p.Figures), true
}

// AddressInPrefix reports whether the subject address lies in prefix, for the API's filters.
func AddressInPrefix(ref string, p netip.Prefix) bool {
	a, ok := addressOfSubject(ref)
	return ok && p.Contains(a)
}
