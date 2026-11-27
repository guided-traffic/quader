package history

import (
	"testing"
	"time"
)

// Unit tests of the problems and the cells of a period, derived from records built in memory (storage.md R-STOR-111). The statements
// that read the records are the integration test's (test/integration/blob/history_read_test.go).

func TestR_STOR_111_ThePeriods(t *testing.T) {
	now := time.Date(2026, 11, 26, 20, 39, 22, 0, time.UTC)
	want := map[string]struct {
		cells int
		cell  time.Duration
	}{"24h": {24, time.Hour}, "7d": {28, 6 * time.Hour}, "30d": {30, day}, "90d": {30, 3 * day}, "1y": {52, 7 * day}}
	for name, w := range want {
		p, ok := ParsePeriod(name)
		if !ok || p.Cells != w.cells || p.Cell != w.cell {
			t.Errorf("%s: %+v %v", name, p, ok)
		}
		cells := DeriveCells(p, now, CellRecords{}, []string{"default"})
		if len(cells) != 1 || len(cells[0].Cells) != w.cells || !cells[0].Cells[0].Start.Equal(now.Add(-p.Span())) ||
			!cells[0].Cells[w.cells-1].Start.Add(p.Cell).Equal(now) || cells[0].Cells[0].Tone != ToneNone {
			t.Errorf("%s: the cells are not counted back from now: %+v", name, cells)
		}
	}
	if p, ok := ParsePeriod(""); !ok || p.Name != DefaultPeriod {
		t.Errorf("no name: %+v", p)
	}
	if _, ok := ParsePeriod("2w"); ok {
		t.Error("2w is a period")
	}
}

func TestR_STOR_111_TheProblemsOfAPeriod(t *testing.T) {
	now := time.Date(2026, 11, 26, 20, 39, 22, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	records := ProblemRecords{
		Intervals: []Interval{
			// two replicas cut off from archive, overlapping: one problem with both replicas and both codes
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-0", Started: ago(50 * time.Hour), Ended: ago(49 * time.Hour), Code: "timeout", Occurrences: 3},
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-1", Started: ago(49*time.Hour + 30*time.Minute), Ended: ago(48 * time.Hour), Code: "dns", Occurrences: 1},
			// a later outage of the same connection, still lasting
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-0", Started: ago(10 * time.Minute), Code: "refused", Occurrences: 1},
			// a failing start check with its step
			{Connection: "default", Kind: KindStartCheckFailed, Replica: "web-2", Started: ago(30 * time.Hour), Ended: ago(29 * time.Hour), Code: "access_denied", Step: "write", Occurrences: 1},
		},
		Findings: []DayFindings{
			{
				Connection: "default", Day: ago(72 * time.Hour).Truncate(day), N: 3, Bytes: 3000, First: ago(72 * time.Hour), LastResolved: ago(71 * time.Hour),
				Resolved: 3, Sources: []string{"scrub"}, Reasons: []string{"digest"},
			},
			{
				Connection: "default", Day: ago(5 * time.Hour).Truncate(day), N: 2, Bytes: 2048, First: ago(5 * time.Hour), Open: 1, Acknowledged: 1,
				Unavailable: 1, UnavailableOpen: 1, Sources: []string{"download"}, Reasons: []string{"not_found"},
			},
		},
		Counts: []DayCounts{
			{
				Connection: "default", Kind: "upload_refused", Day: ago(5 * time.Hour).Truncate(day), N: 4, Bytes: 4096,
				FirstHour: ago(5 * time.Hour).Truncate(time.Hour), LastHour: ago(4 * time.Hour).Truncate(time.Hour), Reasons: []string{"timeout"},
			},
			{
				Connection: "default", Kind: "read_unavailable", Day: now.Truncate(day), N: 1, FirstHour: now.Truncate(time.Hour),
				LastHour: now.Truncate(time.Hour), Reasons: []string{"not_found"},
			},
			{
				Connection: "default", Kind: "findings_unlisted", Day: ago(72 * time.Hour).Truncate(day), N: 7, Bytes: 70,
				FirstHour: ago(72 * time.Hour).Truncate(time.Hour), LastHour: ago(72 * time.Hour).Truncate(time.Hour), Reasons: []string{"digest"},
			},
		},
	}
	got := DeriveProblems(records, now)
	if len(got) != 7 {
		t.Fatalf("%d problems: %+v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Started.After(got[i-1].Started) {
			t.Errorf("not newest first at %d: %v after %v", i, got[i].Started, got[i-1].Started)
		}
	}
	by := func(kind, conn string, started time.Time) Problem {
		t.Helper()
		for _, p := range got {
			if p.Kind == kind && p.Connection == conn && p.Started.Equal(started) {
				return p
			}
		}
		t.Fatalf("no %s of %s at %v in %+v", kind, conn, started, got)
		return Problem{}
	}

	t.Run("R-STOR-111 overlapping intervals of two replicas are one outage with both replicas, the occurrences and the codes", func(t *testing.T) {
		p := by(ProblemUnreachable, "archive", ago(50*time.Hour))
		if p.Replicas != 2 || p.Occurrences != 4 || len(p.Codes) != 2 || p.Codes[0] != "dns" || !p.Ended.Equal(ago(48*time.Hour)) ||
			p.Seconds != 2*3600 || p.Tone != ToneWarning || p.Lasting() {
			t.Errorf("%+v", p)
		}
	})
	t.Run("R-STOR-111 an outage that lasts has no end and counts to now", func(t *testing.T) {
		p := by(ProblemUnreachable, "archive", ago(10*time.Minute))
		if !p.Lasting() || p.Seconds != 600 || p.Replicas != 1 {
			t.Errorf("%+v", p)
		}
	})
	t.Run("R-STOR-111 a failing start check names its step", func(t *testing.T) {
		p := by(ProblemStartCheckFailed, "default", ago(30*time.Hour))
		if len(p.Steps) != 1 || p.Steps[0] != "write" || p.Codes[0] != "access_denied" || p.Tone != ToneWarning {
			t.Errorf("%+v", p)
		}
	})
	t.Run("R-STOR-111 a day of findings is one problem, the findings beyond the cap counted in, ended at its last resolution", func(t *testing.T) {
		p := by(ProblemCopiesDamaged, "default", ago(72*time.Hour))
		if p.Count != 10 || p.Bytes != 3070 || p.Resolved != 3 || !p.Ended.Equal(ago(71*time.Hour)) || p.Tone != ToneWarning || p.Sources[0] != "scrub" {
			t.Errorf("%+v", p)
		}
	})
	t.Run("R-STOR-111 a day with an open finding no other copy held lasts and is an error", func(t *testing.T) {
		p := by(ProblemCopiesDamaged, "default", ago(5*time.Hour))
		if !p.Lasting() || p.Tone != ToneError || p.Open != 1 || p.Acknowledged != 1 || p.UnavailableOpen != 1 {
			t.Errorf("%+v", p)
		}
	})
	t.Run("R-STOR-111 refused uploads of a day end with their last hour; reads no copy served in the current hour last", func(t *testing.T) {
		up := by(ProblemUploadsRefused, "default", ago(5*time.Hour).Truncate(time.Hour))
		if up.Lasting() || !up.Ended.Equal(ago(4*time.Hour).Truncate(time.Hour).Add(time.Hour)) || up.Count != 4 || up.Tone != ToneError {
			t.Errorf("%+v", up)
		}
		rd := by(ProblemReadsUnavailable, "default", now.Truncate(time.Hour))
		if !rd.Lasting() || rd.Tone != ToneError {
			t.Errorf("%+v", rd)
		}
	})
	t.Run("R-STOR-111 fall-overs are no problem of their own", func(t *testing.T) {
		only := DeriveProblems(ProblemRecords{Counts: []DayCounts{{Connection: "default", Kind: "read_fallover", N: 9, FirstHour: now, LastHour: now}}}, now)
		if len(only) != 0 {
			t.Errorf("%+v", only)
		}
	})
}

func TestR_STOR_111_TheCellsOfAPeriod(t *testing.T) {
	now := time.Date(2026, 11, 26, 20, 39, 22, 0, time.UTC)
	p, _ := ParsePeriod("24h")
	from := now.Add(-p.Span())
	at := func(cell int, offset time.Duration) time.Time {
		return from.Add(time.Duration(cell)*time.Hour + offset)
	}
	records := CellRecords{
		Intervals: []Interval{
			// two replicas cut off in cell 3 for 20 minutes, overlapping for 10: 30 minutes of the cell, two replicas
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-0", Started: at(3, 0), Ended: at(3, 20*time.Minute)},
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-1", Started: at(3, 10*time.Minute), Ended: at(3, 30*time.Minute)},
			// one that began before the period and ended in its first cell
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-0", Started: from.Add(-time.Hour), Ended: at(0, 15*time.Minute)},
			// one that lasts: the last cell to now
			{Connection: "archive", Kind: KindUnreachable, Replica: "web-0", Started: at(23, 30*time.Minute)},
			{Connection: "default", Kind: KindStartCheckFailed, Replica: "web-0", Started: at(5, 0), Ended: at(6, 30*time.Minute)},
		},
		Counts: []CellCount{
			{Connection: "archive", Kind: "read_fallover", Cell: 3, N: 310},
			{Connection: "archive", Kind: "write_fallover", Cell: 3, N: 41},
			{Connection: "default", Kind: "upload_refused", Cell: 8, N: 2, Bytes: 2048},
			{Connection: "default", Kind: "encrypted_read_refused", Cell: 9, N: 12, UnknownKey: true},
			{Connection: "default", Kind: "findings_unlisted", Cell: 10, N: 5, Bytes: 50},
			{Connection: "forgotten", Kind: "copy_failed", Cell: 11, N: 1},
		},
		Findings: []CellFindings{{Connection: "default", Cell: 10, N: 2, Bytes: 20, UnavailableBlobs: 1}},
		Totals: []FindingTotal{
			{Connection: "default", Source: "scrub", Outcome: "rewritten", N: 1, Bytes: 10},
			{Connection: "default", Source: "download", Outcome: "open", N: 1, Bytes: 10},
		},
	}
	got := DeriveCells(p, now, records, []string{"default", "archive"})
	if len(got) != 3 || got[0].Connection != "default" || got[1].Connection != "archive" || got[2].Connection != "forgotten" {
		t.Fatalf("connections %+v", got)
	}
	arch, def := got[1].Cells, got[0].Cells

	t.Run("R-STOR-111 an outage counts the union of the replicas' intervals within each cell, with the replicas", func(t *testing.T) {
		if arch[3].UnreachableSeconds != 1800 || arch[3].Replicas != 2 || arch[3].Tone != ToneWarning || arch[3].ReadFallovers != 310 || arch[3].WriteFallovers != 41 {
			t.Errorf("cell 3 %+v", arch[3])
		}
		if arch[0].UnreachableSeconds != 900 || arch[0].Replicas != 1 {
			t.Errorf("cell 0 %+v", arch[0])
		}
		if arch[23].UnreachableSeconds != 1800 || arch[22].UnreachableSeconds != 0 || arch[22].Tone != ToneNone {
			t.Errorf("cells 22, 23 %+v %+v", arch[22], arch[23])
		}
	})
	t.Run("R-STOR-111 a failing start check marks every cell it overlaps", func(t *testing.T) {
		if def[5].StartChecksFailed != 1 || def[6].StartChecksFailed != 1 || def[7].StartChecksFailed != 0 || def[5].Tone != ToneWarning {
			t.Errorf("%+v %+v %+v", def[5], def[6], def[7])
		}
	})
	t.Run("R-STOR-111 clients refused make a cell an error; what Quader absorbed a warning", func(t *testing.T) {
		if def[8].Tone != ToneError || def[8].UploadsRefused != 2 {
			t.Errorf("cell 8 %+v", def[8])
		}
		if def[9].Tone != ToneWarning || def[9].ReadsRefused != 12 || def[9].UnknownKeyReads != 12 {
			t.Errorf("cell 9 %+v", def[9])
		}
		if def[10].Tone != ToneError || def[10].DamagedCopies != 7 || def[10].DamagedBytes != 70 || def[10].UnavailableBlobs != 1 {
			t.Errorf("cell 10 %+v", def[10])
		}
	})
	t.Run("R-STOR-111 the findings' totals by outcome and source, the unlisted counted; a forgotten connection keeps its id", func(t *testing.T) {
		f := got[0].Findings
		if f.N != 2 || f.Bytes != 20 || f.ByOutcome["rewritten"] != 1 || f.ByOutcome["open"] != 1 || f.BySource["scrub"] != 1 || f.Unlisted != 5 {
			t.Errorf("%+v", f)
		}
		if got[2].Cells[11].CopiesFailed != 1 || got[2].Cells[11].Tone != ToneWarning {
			t.Errorf("forgotten %+v", got[2].Cells[11])
		}
	})
}
