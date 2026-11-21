package app

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
)

// TestR_SIG_049_OnlyTheRolesThatRunJobsBuildASigner: the area signing is on every role with the database, and its key holder, watcher and
// job are built where the role runs jobs (wireSigning keys them on Subsystems.JobLoops): worker and all, never api, packages or web; the
// role scanner builds no area of the infrastructure at all. Without a database the step builds nothing.
func TestR_SIG_049_OnlyTheRolesThatRunJobsBuildASigner(t *testing.T) {
	for role, signer := range map[string]bool{
		config.RoleAPI: false, config.RolePackages: false, config.RoleWeb: false, config.RoleWorker: true, config.RoleAll: true, config.RoleScanner: false,
	} {
		sub, err := SubsystemsFor(role)
		if err != nil {
			t.Fatal(err)
		}
		if sub.JobLoops != signer {
			t.Errorf("%s: JobLoops %v, want a signer %v", role, sub.JobLoops, signer)
		}
		if has := sub.Has(AreaSigning); has != (role != config.RoleScanner) {
			t.Errorf("%s: the area signing is built %v", role, has)
		}
	}
	a := &App{cfg: &config.Config{}}
	out, err := a.wireSigning(context.Background(), infra{})
	if err != nil || out.store != nil || out.keys != nil || out.job != nil {
		t.Errorf("without a database: %+v %v", out, err)
	}
}
