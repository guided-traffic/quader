package proxy

import "testing"

// inspecting is an adapter whose only answer the test reads is whether the format has a fill hook for a path.
type inspecting struct {
	Adapter
	hook bool
}

func (a inspecting) InspectsFill(string) bool { return a.hook }

// TestR_REPO_041_TheFirstAnswerOfASmallMissComesFromTheStoredEntry: a fetched artifact whose body fits in the withheld tail is not
// streamed to the leader's client; the stored entry answers it with the headers of a stored answer, the validators included, as it
// answers every later request (Q-NUGET-86: the first answer of a .nuspec streamed the upstream's response without ETag, Accept-Ranges
// or Cache-Control). A larger body streams unless the format inspects it or the host holds the fill.
func TestR_REPO_041_TheFirstAnswerOfASmallMissComesFromTheStoredEntry(t *testing.T) {
	plain := Request{Path: "contoso.util/2.1.0/contoso.util.nuspec", Adapter: inspecting{}}
	for _, n := range []int64{0, 1, 4 << 10, withheldTail} {
		if !fillHeld(plain, n) {
			t.Errorf("a body of %d bytes streams to the leader's client, want it answered from the stored entry", n)
		}
	}
	if fillHeld(plain, withheldTail+1) {
		t.Errorf("a body of %d bytes, past the withheld tail, is held without a hook or a hold", withheldTail+1)
	}
	hook := plain
	hook.Adapter = inspecting{hook: true}
	if !fillHeld(hook, 10<<20) {
		t.Error("an artifact the format inspects before it is stored streams to the leader's client")
	}
	hold := plain
	hold.HoldFill = true
	if !fillHeld(hold, 10<<20) {
		t.Error("a fill the host holds streams to the leader's client")
	}
}
