package limits

import "testing"

// TestR_LIM_027_TheVirtualRepositoryAWellKnownMemberServedThroughNamesTheRowWhileItServes: a request of a team's virtual repository that a
// member of the project upstream serves records the virtual repository beside the serving one (Q-LIM-63); a later serving repository,
// or a member recorded without one, leaves it out of the row.
func TestR_LIM_027_TheVirtualRepositoryAWellKnownMemberServedThroughNamesTheRowWhileItServes(t *testing.T) {
	c := &Charge{}
	c.Served(5) // the dispatcher records the addressed virtual repository first
	if got := c.viaOf(c.ServedRepo()); got != 0 {
		t.Fatalf("via of the addressed repository = %d", got)
	}
	c.ServedThrough(6, 5)
	if c.ServedRepo() != 6 || c.viaOf(6) != 5 {
		t.Fatalf("served %d via %d, want 6 via 5", c.ServedRepo(), c.viaOf(6))
	}
	c.Served(6) // the item store records the same member again
	if c.viaOf(c.ServedRepo()) != 5 {
		t.Fatal("recording the same member again dropped the virtual repository")
	}
	c.Served(5) // a fetch of the member found nothing: the addressed repository is put back
	if c.viaOf(c.ServedRepo()) != 0 {
		t.Fatal("the virtual repository names a row the member did not serve")
	}
	var none *Charge
	none.ServedThrough(6, 5) // a request without the limits records nothing
}
