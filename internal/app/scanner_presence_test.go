package app

import (
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/vuln/report"
)

// R-VULN-039, Q-VULN-277: a live, ready replica of the kind proves a scanner whatever this replica's configuration says; without one, no
// address of the kind or replicas that report themselves mean none, and replicas that cannot report leave it unknown (a row's code then
// speaks). Unit, microseconds.
func TestR_VULN_039_AScannerIsConnectedWhileAReplicaOfItsKindIsReady(t *testing.T) {
	for _, tc := range []struct {
		ready bool
		mode  gen.ScannerKindReplicas
		want  report.Presence
	}{
		{true, gen.ScannerKindReplicasNone, report.PresenceReady},
		{true, gen.ScannerKindReplicasUnverified, report.PresenceReady},
		{true, gen.ScannerKindReplicasReporting, report.PresenceReady},
		{false, gen.ScannerKindReplicasNone, report.PresenceNone},
		{false, gen.ScannerKindReplicasReporting, report.PresenceNone},
		{false, gen.ScannerKindReplicasUnverified, report.PresenceUnknown},
	} {
		if got := presenceOf(tc.ready, tc.mode); got != tc.want {
			t.Errorf("ready %v, replicas %s: %v, want %v", tc.ready, tc.mode, got, tc.want)
		}
	}
}
