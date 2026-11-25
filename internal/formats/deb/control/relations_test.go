package control

import (
	"encoding/json"
	"strings"
	"testing"
)

// R-DEB-038: a relation field is read into its groups of alternatives with the qualifier, the constraint, the architecture restriction and
// the build profiles; a field that does not follow the grammar is an error, and the screen shows it as text.
func TestR_DEB_038_RelationsAreReadIntoGroupsOfAlternatives(t *testing.T) {
	got, err := ParseRelations("libc6 (>= 2.34), libssl3t64 (>= 3.0.0) | libssl3, python3:any,\n default-mta | mail-transport-agent," +
		" foo [amd64 !i386] <!nocheck> <stage1>, bar (< 2), baz (>1.0~rc1-1),")
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(got); err != nil {
		t.Fatal(err)
	}
	b := strings.TrimSpace(buf.String())
	want := `[[{"name":"libc6","op":">=","version":"2.34"}],` +
		`[{"name":"libssl3t64","op":">=","version":"3.0.0"},{"name":"libssl3"}],` +
		`[{"name":"python3","arch":"any"}],` +
		`[{"name":"default-mta"},{"name":"mail-transport-agent"}],` +
		`[{"name":"foo","architectures":["amd64","!i386"],"profiles":[["!nocheck"],["stage1"]]}],` +
		`[{"name":"bar","op":"<=","version":"2"}],` +
		`[{"name":"baz","op":">=","version":"1.0~rc1-1"}]]`
	if b != want {
		t.Fatalf("relations =\n%s\nwant\n%s", b, want)
	}
	if got, err := ParseRelations("  "); err != nil || got != nil {
		t.Errorf("empty field = %v, %v", got, err)
	}
	for _, bad := range []string{"Libc6", "libc6 (>= )", "libc6 (2.0)", "libc6 (>= 2.0", "libc6 2.0", "libc6 [amd64", "a", "libc6:Any"} {
		if _, err := ParseRelations(bad); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
	if _, err := ParseRelations(strings.Repeat("ab, ", MaxRelations+1)); err == nil {
		t.Error("a field over the bound was read")
	}
}

// R-DEB-038: the extended description keeps Debian's paragraph rules: " ." is an empty line, a line with a further leading space is
// verbatim, the synopsis is not part of it.
func TestR_DEB_038_TheLongDescriptionKeepsDebiansParagraphRules(t *testing.T) {
	p, err := Parse([]byte("Package: a\nDescription: the synopsis\n First paragraph,\n wrapped.\n .\n It lists:\n  * one\n  * two\n .\n Last.\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := LongDescription(p.Value("Description"))
	want := []DescriptionBlock{
		{Text: "First paragraph,\nwrapped."},
		{Text: "It lists:"},
		{Text: " * one\n * two", Verbatim: true},
		{Text: "Last."},
	}
	if len(got) != len(want) {
		t.Fatalf("blocks = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if LongDescription("only a synopsis") != nil {
		t.Error("a synopsis alone has a long description")
	}
}
