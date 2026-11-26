package authz

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"
)

// R-OPS-056: an allowed write names the unit the audit log joins one act's writes by: its package, else the folder of its path; an
// allowed read and a refusal name none. Unit tier, microseconds.
func TestR_OPS_056_AnAllowedWriteNamesItsUnit(t *testing.T) {
	repo := qname.Repo{Project: "platform", Key: "images"}
	for _, c := range []struct {
		c    Coordinate
		want string
	}{
		{Coordinate{Repository: repo, Package: "acme/app", Path: "acme/app/_blobs/sha256:ab"}, "acme/app"},
		{Coordinate{Repository: repo, Path: "releases/1.0/app.zip"}, "releases/1.0/"},
		{Coordinate{Repository: repo, Path: "app.zip"}, "/"},
		{Coordinate{Repository: repo}, ""},
	} {
		if got := writeUnit(c.c); got != c.want {
			t.Errorf("%+v: %q, want %q", c.c, got, c.want)
		}
	}
	e := &Engine{}
	unitOf := func(name string) any {
		ev := e.decisionEvent(name, Subject{Name: "priya"}, Coordinate{Repository: repo, Package: "acme/app"}, Decision{Outcome: Allow})
		var d map[string]any
		_ = json.Unmarshal(ev.Details, &d)
		return d["unit"]
	}
	if unitOf(EventWriteAllowed) != "acme/app" || unitOf(EventReadAllowed) != nil || unitOf(EventDenied) != nil {
		t.Errorf("the unit of a write %v, of a read %v, of a refusal %v", unitOf(EventWriteAllowed), unitOf(EventReadAllowed), unitOf(EventDenied))
	}
}
