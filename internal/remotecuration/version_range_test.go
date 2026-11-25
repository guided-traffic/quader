package remotecuration

import (
	"strconv"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// numbers orders versions of one number; any other string is no version.
type numbers struct{}

func (numbers) Compare(a, b string) int {
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	if x != y {
		return x - y
	}
	return strings.Compare(a, b)
}

func (numbers) SortKey(v string) []byte {
	_, err := strconv.Atoi(v)
	return versionkey.Of(nil, err == nil, v)
}

// TestR_CUR_026_TheNarrowerOfTwoRanges holds what the replay of a narrowed range keeps (Q-CUR-225): the higher lower bound, the lower
// upper bound, an upper bound included only where both include it, and a range of none where they share no version.
func TestR_CUR_026_TheNarrowerOfTwoRanges(t *testing.T) {
	o := Order{View: numbers{}}
	for _, c := range []struct {
		a, b, want Range
	}{
		{Range{From: "2"}, Range{To: "5"}, Range{From: "2", To: "5"}},
		{Range{From: "2", To: "8", ToInclusive: true}, Range{From: "4", To: "8"}, Range{From: "4", To: "8"}},
		{Range{To: "8", ToInclusive: true}, Range{To: "6", ToInclusive: true}, Range{To: "6", ToInclusive: true}},
		{Range{From: "3"}, Range{From: "1"}, Range{From: "3", ToInclusive: true}},
	} {
		if got := c.a.Intersect(o, c.b); got != c.want {
			t.Errorf("%+v ∩ %+v = %+v, want %+v", c.a, c.b, got, c.want)
		}
		if got := c.b.Intersect(o, c.a); got != c.want {
			t.Errorf("%+v ∩ %+v = %+v, want %+v", c.b, c.a, got, c.want)
		}
	}
	if got := (Range{From: "6"}).Intersect(o, Range{To: "4", ToInclusive: true}); !got.Empty(o) {
		t.Errorf("two ranges without a version in common give %+v, want a range of none", got)
	}
	if (Range{To: "5"}).Contains(o, "latest") || !(Range{}).Contains(o, "latest") {
		t.Error("a string that is no version lies in no range that bounds something, and in the range that bounds nothing")
	}
}
