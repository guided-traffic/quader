package usage

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Period is a period of the overview (R-LIM-034).
type Period string

// The periods.
const (
	PeriodHour Period = "hour"
	PeriodDay  Period = "day"
	Period30d  Period = "30d"
)

// Window is the period's start before now and the granularity it reads.
func (p Period) Window(now time.Time) (since time.Time, granularity string) {
	switch p {
	case PeriodHour:
		return now.Add(-time.Hour), "5m"
	case Period30d:
		return now.Add(-30 * 24 * time.Hour), "1h"
	}
	return now.Add(-24 * time.Hour), "5m"
}

// Scope narrows a read to the rows of some repositories; empty is the instance.
type Scope struct{ Repos []int64 }

func (s Scope) repos() []int64 {
	if s.Repos == nil {
		return []int64{}
	}
	return s.Repos
}

// SubjectUsage is one row of the overview: a subject's consumption in the period and scope.
type SubjectUsage struct {
	Kind                                          limits.SubjectKind
	Ref                                           string
	Requests, Refused, Cached, Uncached, Upstream int64
	Limited, WouldLimit                           int64
	LastLimited, LastWouldLimit                   time.Time
	// Utilisation is the highest ratio of the last 60 minutes and the last 24 hours to the limits the rows recorded, over the
	// subject's places; Figure and Place name it. Unlimited says every hour and day figure is unlimited.
	Utilisation float64
	Figure      string
	Place       string
	Unlimited   bool
}

// Reader reads the usage store for the API.
type Reader struct {
	Runner meta.Runner
	Now    func() time.Time
}

func (r Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r Reader) in(ctx context.Context, fn func(qs *q.Queries) error) error {
	return r.Runner.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return nil
		}
		return fn(q.New(ptx))
	})
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// Subjects reads every subject's consumption in a period and scope, of the kinds asked; ref narrows to one subject.
func (r Reader) Subjects(ctx context.Context, p Period, s Scope, kinds []limits.SubjectKind, ref string) ([]SubjectUsage, error) {
	now := r.now()
	since, g := p.Window(now)
	ks := make([]string, len(kinds))
	for i, k := range kinds {
		ks[i] = string(k)
	}
	byKey := map[string]*SubjectUsage{}
	get := func(kind, ref string) *SubjectUsage {
		k := kind + "\x00" + ref
		u := byKey[k]
		if u == nil {
			u = &SubjectUsage{Kind: limits.SubjectKind(kind), Ref: ref, Unlimited: true}
			byKey[k] = u
		}
		return u
	}
	wanted := map[string]bool{}
	for _, k := range ks {
		wanted[k] = true
	}
	err := r.in(ctx, func(qs *q.Queries) error {
		totals, err := qs.UsageTotals(ctx, q.UsageTotalsParams{Granularity: g, Since: ts(since), Repos: s.repos(), Kinds: ks, SubjectRef: ref})
		if err != nil {
			return err
		}
		for _, t := range totals {
			u := get(t.SubjectKind, t.SubjectRef)
			u.Requests, u.Refused, u.Cached, u.Uncached, u.Upstream = t.Requests, t.Refused, t.CachedBytes, t.UncachedBytes, t.UpstreamRequests
		}
		evs, err := qs.UsageEventTotals(ctx, q.UsageEventTotalsParams{Granularity: g, Since: ts(since), Repos: s.repos(), SubjectRef: ref})
		if err != nil {
			return err
		}
		for _, e := range evs {
			if !wanted[e.SubjectKind] {
				continue
			}
			u := get(e.SubjectKind, e.SubjectRef)
			if e.Mode == string(limits.ModeEnforce) {
				u.Limited, u.LastLimited = e.Events, e.LastAt.Time
			} else {
				u.WouldLimit, u.LastWouldLimit = e.Events, e.LastAt.Time
			}
		}
		places, err := qs.UsageByPlace(ctx, q.UsageByPlaceParams{
			HourSince: ts(now.Add(-time.Hour)), DaySince: ts(now.Add(-24 * time.Hour)),
			Repos: s.repos(), SubjectRef: ref,
		})
		if err != nil {
			return err
		}
		for _, pl := range places {
			if !wanted[pl.SubjectKind] {
				continue
			}
			u := get(pl.SubjectKind, pl.SubjectRef)
			if !pl.Unlimited {
				u.Unlimited = false
			}
			for _, c := range []struct {
				used, limit int64
				fig         limits.Figure
			}{
				{pl.HourRequests, pl.LimitRequestsHour, limits.RequestsPerHour},
				{pl.HourCached, pl.LimitCachedHour, limits.CachedBytesPerHour},
				{pl.HourUncached, pl.LimitUncachedHour, limits.UncachedBytesPerHour},
				{pl.HourUpstream, pl.LimitUpstreamHour, limits.UpstreamRequestsPerHour},
				{pl.DayBytes, pl.LimitBytesDay, limits.BytesPerDay},
			} {
				if c.limit <= 0 {
					continue
				}
				if ratio := float64(c.used) / float64(c.limit); ratio > u.Utilisation || u.Figure == "" {
					u.Utilisation, u.Figure, u.Place = ratio, c.fig.String(), pl.Place
				}
			}
		}
		return nil
	})
	out := make([]SubjectUsage, 0, len(byKey))
	for _, u := range byKey {
		out = append(out, *u)
	}
	return out, err
}

// PlaceLimits are the limits a subject's rows recorded at one place in the last 24 hours: its budget keys.
type PlaceLimits struct {
	Place  string
	Limits limits.Figures
}

// Places reads the places a subject was charged at in the last 24 hours with the hour and day limits the rows recorded.
func (r Reader) Places(ctx context.Context, kind limits.SubjectKind, ref string, s Scope) ([]PlaceLimits, error) {
	now := r.now()
	var out []PlaceLimits
	err := r.in(ctx, func(qs *q.Queries) error {
		rows, err := qs.UsageByPlace(ctx, q.UsageByPlaceParams{
			HourSince: ts(now.Add(-time.Hour)), DaySince: ts(now.Add(-24 * time.Hour)),
			Repos: s.repos(), SubjectRef: ref,
		})
		for _, p := range rows {
			if p.SubjectKind != string(kind) {
				continue
			}
			var f limits.Figures
			f[limits.RequestsPerHour], f[limits.CachedBytesPerHour] = p.LimitRequestsHour, p.LimitCachedHour
			f[limits.UncachedBytesPerHour], f[limits.UpstreamRequestsPerHour] = p.LimitUncachedHour, p.LimitUpstreamHour
			f[limits.BytesPerDay] = p.LimitBytesDay
			out = append(out, PlaceLimits{Place: p.Place, Limits: f})
		}
		return err
	})
	return out, err
}

// Point is one five-minute point of a subject's history.
type Point struct {
	Start                                         time.Time
	Requests, Refused, Cached, Uncached, Upstream int64
}

// History reads a subject's five-minute points of the last 24 hours.
func (r Reader) History(ctx context.Context, kind limits.SubjectKind, ref string, s Scope) ([]Point, error) {
	var out []Point
	err := r.in(ctx, func(qs *q.Queries) error {
		rows, err := qs.UsageHistory(ctx, q.UsageHistoryParams{
			Since: ts(r.now().Add(-24 * time.Hour)), SubjectKind: string(kind),
			SubjectRef: ref, Repos: s.repos(),
		})
		for _, p := range rows {
			out = append(out, Point{
				Start: p.BucketStart.Time, Requests: p.Requests, Refused: p.Refused, Cached: p.CachedBytes,
				Uncached: p.UncachedBytes, Upstream: p.UpstreamRequests,
			})
		}
		return err
	})
	return out, err
}

// RepoUsage is a subject's consumption in one serving repository.
type RepoUsage struct {
	RepoID                                        int64
	ProjectKey, RepoKey                           string
	Requests, Refused, Cached, Uncached, Upstream int64
}

// Repositories reads a subject's consumption per serving repository in a period.
func (r Reader) Repositories(ctx context.Context, p Period, kind limits.SubjectKind, ref string, s Scope) ([]RepoUsage, error) {
	since, g := p.Window(r.now())
	var out []RepoUsage
	err := r.in(ctx, func(qs *q.Queries) error {
		rows, err := qs.UsageRepositories(ctx, q.UsageRepositoriesParams{
			Granularity: g, Since: ts(since), SubjectKind: string(kind),
			SubjectRef: ref, Repos: s.repos(),
		})
		for _, x := range rows {
			out = append(out, RepoUsage{
				RepoID: x.RepositoryID, ProjectKey: x.ProjectKey, RepoKey: x.RepoKey, Requests: x.Requests,
				Refused: x.Refused, Cached: x.CachedBytes, Uncached: x.UncachedBytes, Upstream: x.UpstreamRequests,
			})
		}
		return err
	})
	return out, err
}

// CredentialUsage is a user's consumption through one credential.
type CredentialUsage struct {
	Credential                 string
	Requests, Cached, Uncached int64
}

// Credentials reads a user's consumption per credential in a period.
func (r Reader) Credentials(ctx context.Context, p Period, ref string, s Scope) ([]CredentialUsage, error) {
	since, g := p.Window(r.now())
	var out []CredentialUsage
	err := r.in(ctx, func(qs *q.Queries) error {
		rows, err := qs.UsageCredentials(ctx, q.UsageCredentialsParams{Granularity: g, Since: ts(since), SubjectRef: ref, Repos: s.repos()})
		for _, x := range rows {
			out = append(out, CredentialUsage{Credential: x.CredentialRef, Requests: x.Requests, Cached: x.CachedBytes, Uncached: x.UncachedBytes})
		}
		return err
	})
	return out, err
}

// PreviewEvent is a would-limit count of one subject and figure in the preview's period.
type PreviewEvent struct {
	Kind   limits.SubjectKind
	Ref    string
	Figure string
	Events int64
}

// ModePreview reads the would_limit events of the five-minute buckets of the last 24 hours (R-LIM-033).
func (r Reader) ModePreview(ctx context.Context) ([]PreviewEvent, time.Time, time.Time, error) {
	now := r.now()
	since := now.Add(-24 * time.Hour)
	var out []PreviewEvent
	err := r.in(ctx, func(qs *q.Queries) error {
		rows, err := qs.UsageModePreview(ctx, ts(since))
		for _, x := range rows {
			out = append(out, PreviewEvent{Kind: limits.SubjectKind(x.SubjectKind), Ref: x.SubjectRef, Figure: x.Figure, Events: x.Events})
		}
		return err
	})
	return out, since, now, err
}
