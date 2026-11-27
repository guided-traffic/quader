package history

import "time"

// A period of the storage page (storage.md R-STOR-111, api-and-ui.md R-UI-265): the time back from now that the history is read for,
// cut into cells of equal length counted back from now and aligned to no calendar (Q-STOR-527), so that no time zone decides a cell.
type Period struct {
	// Name is the period as the API takes it: 24h, 7d, 30d, 90d or 1y.
	Name string
	// Cells is the number of cells, Cell the length of one.
	Cells int
	Cell  time.Duration
}

const day = 24 * time.Hour

// Periods answers the periods of the page, shortest first (the plan's B.3): 24 cells of an hour, 28 of six hours, 30 of a day, 30 of
// three days, 52 of a week. A year is 52 weeks.
func Periods() []Period {
	return []Period{
		{Name: "24h", Cells: 24, Cell: time.Hour},
		{Name: "7d", Cells: 28, Cell: 6 * time.Hour},
		{Name: "30d", Cells: 30, Cell: day},
		{Name: "90d", Cells: 30, Cell: 3 * day},
		{Name: "1y", Cells: 52, Cell: 7 * day},
	}
}

// DefaultPeriod is the period of a request that names none.
const DefaultPeriod = "30d"

// ParsePeriod is the period of a name; the empty name is DefaultPeriod.
func ParsePeriod(name string) (Period, bool) {
	if name == "" {
		name = DefaultPeriod
	}
	for _, p := range Periods() {
		if p.Name == name {
			return p, true
		}
	}
	return Period{}, false
}

// Span is the period's length: its cells times their length.
func (p Period) Span() time.Duration { return time.Duration(p.Cells) * p.Cell }

// Window is the period ending at now: [from, to) with to = now.
func (p Period) Window(now time.Time) (from, to time.Time) {
	to = now.UTC()
	return to.Add(-p.Span()), to
}
