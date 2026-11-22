package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/guided-traffic/quader/internal/blob"
)

// The journal pass (R-STOR-056, R-STOR-059, Q-STOR-241): at the end of a full pass the run lists the revocation journal on every
// connection with drain: false that it reaches and copies each object one has and another lacks, so a connection taken in later
// receives the entries shipped before it. The listings are walked side by side in their common order, one page per connection
// at a time, so the pass holds no list of the journal.

// maxJournalObject bounds what the journal pass copies: an entry is a few hundred bytes and an update entry a few kilobytes
// (R-HA-054); a larger object below the journal's directory is not one of Quader's and is left where it is.
const maxJournalObject = 4 << 20

// journalPass runs the journal pass over the targets the worker reaches and reports the objects it copied.
func (rn *run) journalPass(ctx context.Context) (int, error) {
	if rn.o.JournalDir == "" {
		return 0, nil
	}
	var conns []*blob.Connection
	for _, c := range rn.targets() {
		if _, rejecting := c.Rejecting(); !rejecting {
			conns = append(conns, c)
		}
	}
	if len(conns) < 2 {
		return 0, nil
	}
	return copyJournal(ctx, conns, rn.o.JournalDir, rn.log)
}

// copyJournal copies every object below <prefix>/<dir>/ that one of conns has and another lacks, from the first that has it.
func copyJournal(ctx context.Context, conns []*blob.Connection, dir string, log *slog.Logger) (int, error) {
	ls := make([]*journalLister, len(conns))
	for i, c := range conns {
		ls[i] = &journalLister{c: c, prefix: keyIn(c.Prefix(), strings.Trim(dir, "/")) + "/"}
	}
	copied := 0
	for {
		least, found := "", false
		for _, l := range ls {
			k, ok, err := l.head(ctx)
			if err != nil {
				return copied, err
			}
			if ok && (!found || k < least) {
				least, found = k, true
			}
		}
		if !found {
			return copied, nil
		}
		var from *journalLister
		var lacking []*journalLister
		for _, l := range ls {
			if k, ok, _ := l.head(ctx); ok && k == least {
				if from == nil {
					from = l
				}
				l.next()
				continue
			}
			lacking = append(lacking, l)
		}
		for _, to := range lacking {
			ok, err := copyJournalObject(ctx, from, to, least)
			if err != nil {
				log.Warn("a journal object could not be copied to a connection that lacks it", "connection", to.c.ID(), "key", least, "error", err)
				continue
			}
			if ok {
				copied++
			}
		}
	}
}

// copyJournalObject copies the object rel from one connection's journal to another's with a payload checksum. An object above
// maxJournalObject is not copied.
func copyJournalObject(ctx context.Context, from, to *journalLister, rel string) (bool, error) {
	body, _, err := from.c.Objects().Get(ctx, from.prefix+rel, nil)
	if err != nil {
		return false, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxJournalObject+1))
	if err != nil {
		return false, err
	}
	if len(data) > maxJournalObject {
		return false, fmt.Errorf("%d bytes or more below the journal's directory, more than an entry has", len(data))
	}
	h := sha256.Sum256(data)
	err = to.c.Objects().Put(ctx, to.prefix+rel, bytes.NewReader(data), int64(len(data)), blob.Checksum{Algorithm: "sha256", Value: h[:]}, nil)
	return err == nil, err
}

// journalLister walks one connection's journal in key order, a page at a time; keys are relative to its prefix.
type journalLister struct {
	c      *blob.Connection
	prefix string
	page   []string
	token  string
	done   bool
}

// head is the lister's current key; ok is false at the end of its listing.
func (l *journalLister) head(ctx context.Context) (string, bool, error) {
	for len(l.page) == 0 && !l.done {
		pg, err := l.c.Objects().List(ctx, l.prefix, blob.ListOptions{Token: l.token})
		if err != nil {
			return "", false, fmt.Errorf("%s list the journal: %w", l.c.Element(), err)
		}
		for _, o := range pg.Objects {
			l.page = append(l.page, strings.TrimPrefix(o.Key, l.prefix))
		}
		l.token, l.done = pg.Next, pg.Next == ""
	}
	if len(l.page) == 0 {
		return "", false, nil
	}
	return l.page[0], true, nil
}

func (l *journalLister) next() {
	if len(l.page) > 0 {
		l.page = l.page[1:]
	}
}

// keyIn is rel below a connection's prefix (R-STOR-001: an empty prefix puts the key at the top of the bucket).
func keyIn(prefix, rel string) string {
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}
