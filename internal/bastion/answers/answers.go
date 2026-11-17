// Package answers holds the types and the validity rules of the main installation's answers that a Bastion
// keeps (R-BAS-020, Q-BAS-8, Q-BAS-32): what an answer is, when it is usable, and the feed log the rule about
// revocations reads. It is a leaf: it imports the interface package and the standard library, so the engine,
// the shared state (which verifies signed records from Valkey) and the tests judge answers by the same code.
//
// The rules are the specification's, written once:
//
//  1. now < o + period, extended by the grace period only while the main installation is unreachable;
//  2. now < the token's expiry, never extended;
//  3. no feed entry that matches the answer was received after o − [SkewMargin];
//  4. o is not earlier than the start of the replica's feed history plus [SkewMargin].
//
// where o is the Bastion's clock when the request that produced the answer was sent. Work package 4 stores
// answers (memory) and work package 6 stores them signed in Valkey; neither decides validity itself.
package answers

import (
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// SkewMargin is the 15 s of R-BAS-020: the main installation's revocation bound (at most 10 s, R-AUTH-023) and
// the clock skew between replicas.
const SkewMargin = 15 * time.Second

// PruneCutoff returns the cutoff of [FeedLog.Prune] at the given time: entries received before it no answer can
// depend on (period + grace period + [SkewMargin], R-BAS-026).
func PruneCutoff(now time.Time, period, grace time.Duration) time.Time {
	return now.Add(-(period + grace + SkewMargin))
}

// Kind is the kind of an answer (R-BAS-020).
type Kind string

// The kinds of answers.
const (
	// Credential: what the main installation said about a credential (kind, token ID, principal ID, expiry).
	Credential Kind = "credential"
	// Shape: a credential's read shape on a repository.
	Shape Kind = "shape"
	// URL: whether a credential may read a cache key, or the stored refusal.
	URL Kind = "url"
)

// Answer is the part of an answer the validity rules read. The payloads (the subject, the shape, the stored
// refusal) belong to the engine; it embeds or wraps an Answer.
type Answer struct {
	Kind Kind
	// ObtainedAt is o: when the Bastion sent the request the answer came from.
	ObtainedAt time.Time
	// TokenID and PrincipalID identify the credential the answer is about; empty for an anonymous caller or an
	// invalid credential (an answer about those is matched by grants entries only).
	TokenID     string
	PrincipalID string
	// TokenExpiresAt is the token's expiry; nil when the token does not expire or the answer is not about a
	// valid token.
	TokenExpiresAt *time.Time
	// Repository is the qualified repository name "<projectKey>/<repoKey>" of a Shape or URL answer.
	Repository string
}

// Reason says why an answer is not usable.
type Reason string

// The reasons, one per rule; Usable is the empty reason.
const (
	Usable       Reason = ""
	PeriodOver   Reason = "period"
	TokenExpired Reason = "token_expired"
	Revoked      Reason = "revoked"
	BeforeStart  Reason = "history"
)

// State is what the rules are judged against.
type State struct {
	Now    time.Time
	Period time.Duration
	Grace  time.Duration
	// Unreachable is true while the replica treats the main installation as unreachable (R-BAS-027): only then
	// rule 1 is extended by Grace.
	Unreachable bool
	// HistoryStart is the start of the replica's feed history (R-BAS-026); the zero value means no history yet,
	// and no answer is usable.
	HistoryStart time.Time
	// Log is the feed log of rule 3; nil means no entry was received.
	Log *FeedLog
}

// Check applies the four rules to an answer and returns the first that fails.
func (s State) Check(a Answer) Reason {
	limit := s.Period
	if s.Unreachable {
		limit += s.Grace
	}
	if !s.Now.Before(a.ObtainedAt.Add(limit)) {
		return PeriodOver
	}
	if a.TokenExpiresAt != nil && !s.Now.Before(*a.TokenExpiresAt) {
		return TokenExpired
	}
	if s.Log != nil && s.Log.matches(a) {
		return Revoked
	}
	if s.HistoryStart.IsZero() || a.ObtainedAt.Before(s.HistoryStart.Add(SkewMargin)) {
		return BeforeStart
	}
	return Usable
}

// Usable reports whether the answer may be used.
func (s State) Usable(a Answer) bool { return s.Check(a) == Usable }

// FeedLog is the record of the feed entries a replica received, with their receipt times (R-BAS-026). It makes
// every matching answer unusable by rule 3 whatever the order in which an answer and an entry reach the
// replica, which closes the race of an answer fetched before a revocation and stored after the answer was
// evicted. It is safe for concurrent use.
type FeedLog struct {
	mu sync.Mutex
	// seen holds the ID of every applied entry with the entry's own time (the main installation's clock), for as
	// long as the feed may send the entry again; see [FeedLog.Settle].
	seen       map[string]time.Time
	tokens     map[string]time.Time
	principals map[string]time.Time
	repos      map[string]time.Time
	grants     time.Time
}

// NewFeedLog returns an empty log.
func NewFeedLog() *FeedLog {
	return &FeedLog{seen: map[string]time.Time{}, tokens: map[string]time.Time{}, principals: map[string]time.Time{}, repos: map[string]time.Time{}}
}

// Record applies an entry received at the given time and reports whether it was new: an entry whose ID was
// applied before (the lookback of the feed re-sends entries) changes nothing. A reset is not an entry; see
// [FeedLog.Reset].
func (l *FeedLog) Record(e iface.FeedEntry, received time.Time) (applied bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, dup := l.seen[e.ID]; dup {
		return false
	}
	l.seen[e.ID] = e.At
	switch e.Kind {
	case iface.FeedToken:
		for _, id := range e.Tokens {
			l.tokens[id] = received
		}
	case iface.FeedPrincipal:
		for _, id := range e.Principals {
			l.principals[id] = received
		}
	case iface.FeedGrants:
		l.grants = received
	case iface.FeedContent:
		for _, r := range e.Repositories {
			l.repos[r] = received
		}
	case iface.FeedSettings:
		// A settings entry makes the replica handshake again; it invalidates no answer by itself.
	}
	return true
}

// Prune forgets the entries no answer can still depend on: those received before cutoff, which the caller sets to
// now − (period + grace period + [SkewMargin]) (R-BAS-026, [PruneCutoff]). The IDs of applied entries are not
// pruned by time; [FeedLog.Settle] forgets them.
func (l *FeedLog) Prune(cutoff time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range []map[string]time.Time{l.tokens, l.principals, l.repos} {
		for k, t := range m {
			if t.Before(cutoff) {
				delete(m, k)
			}
		}
	}
	if l.grants.Before(cutoff) {
		l.grants = time.Time{}
	}
}

// Settle forgets the IDs of the applied entries the feed can no longer send again, after a read whose cursor
// named the time cursorAt returned the entries got (R-BAS-026, Q-BAS-152). A read returns every entry within the
// lookback behind its cursor (R-BAS-015), and that window is measured from the cursor, which stands still while no
// entry is committed: a quiet feed sends the same entries on every read, for as long as it stays quiet, not for the
// lookback after they were first received. An entry older than the cursor that the read did not return has left the
// window, and the window only moves forward with the cursor, so it cannot come back; its ID goes. Every other ID
// stays: forgetting one while the feed still sends its entry would apply the entry again with a new receipt time
// and make every matching answer obtained since unusable once more. The rule needs neither the main installation's
// lookback nor a comparison of two clocks: both times are the main installation's.
func (l *FeedLog) Settle(cursorAt time.Time, got []iface.FeedEntry) {
	returned := make(map[string]bool, len(got))
	for _, e := range got {
		returned[e.ID] = true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, at := range l.seen {
		if at.Before(cursorAt) && !returned[id] {
			delete(l.seen, id)
		}
	}
}

// Known reports how many IDs of applied entries the log keeps.
func (l *FeedLog) Known() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seen)
}

// Reset forgets every entry, on a reset answer of the feed: the caller drops its answers and takes the reset
// time as the start of its feed history ([State].HistoryStart, rule 4, R-BAS-026).
func (l *FeedLog) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = map[string]time.Time{}
	l.tokens = map[string]time.Time{}
	l.principals = map[string]time.Time{}
	l.repos = map[string]time.Time{}
	l.grants = time.Time{}
}

// ContentSince reports whether a content entry that names the repository was received after the given time. The
// engine asks it of a cache entry with validatedAt − [SkewMargin]: a withdrawal makes the entry unservable also
// while the main installation is unreachable (R-BAS-022, R-BAS-027).
func (l *FeedLog) ContentSince(repo string, after time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t, ok := l.repos[repo]
	return ok && t.After(after)
}

// matches reports rule 3: an entry that matches the answer was received after o − SkewMargin.
func (l *FeedLog) matches(a Answer) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	after := a.ObtainedAt.Add(-SkewMargin)
	hit := func(t time.Time, ok bool) bool { return ok && t.After(after) }
	if a.TokenID != "" {
		t, ok := l.tokens[a.TokenID]
		if hit(t, ok) {
			return true
		}
	}
	if a.PrincipalID != "" {
		t, ok := l.principals[a.PrincipalID]
		if hit(t, ok) {
			return true
		}
	}
	if a.Kind == Shape || a.Kind == URL {
		if l.grants.After(after) {
			return true
		}
	}
	if a.Kind == URL && a.Repository != "" {
		t, ok := l.repos[a.Repository]
		if hit(t, ok) {
			return true
		}
	}
	return false
}
