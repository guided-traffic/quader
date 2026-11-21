package wellknown_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// storedAs is the stored repository a reconcile of d leaves.
func storedAs(d wellknown.Declaration) wellknown.Stored {
	return wellknown.Stored{Key: d.Key, Format: d.Format, Class: d.Class, Kind: d.Kind, Address: d.Address(), Fixed: d.Fixed()}
}

func stepsOf(steps []wellknown.Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, string(s.Op)+" "+s.Key)
	}
	return out
}

// TestR_REPO_067_ThePlanOfTheReconcile holds the steps of R-REPO-067 at the unit tier: what the reconcile writes for each difference
// between the catalog and the stored repositories, without a database. The integration area upstream runs the same against
// PostgreSQL.
func TestR_REPO_067_ThePlanOfTheReconcile(t *testing.T) {
	decls := wellknown.Declared(nil)
	all := make([]wellknown.Stored, 0, len(decls))
	for _, d := range decls {
		all = append(all, storedAs(d))
	}
	with := func(key string, change func(*wellknown.Stored)) []wellknown.Stored {
		out := slices.Clone(all)
		for i := range out {
			if out[i].Key == key {
				change(&out[i])
			}
		}
		return out
	}
	plan := func(t *testing.T, decls []wellknown.Declaration, stored []wellknown.Stored) []wellknown.Step {
		t.Helper()
		steps, err := wellknown.Plan(decls, stored)
		if err != nil {
			t.Fatal(err)
		}
		return steps
	}

	t.Run("R-REPO-067 step 1 an empty project gets one create per declaration, in catalog order: a preset internal, the system repositories private (Q-REPO-388)", func(t *testing.T) {
		steps := plan(t, decls, nil)
		if len(steps) != 14 || steps[0].Op != wellknown.OpCreate || steps[len(steps)-1].Key != "clamav" {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
		for _, s := range steps {
			want := "internal"
			if s.Decl.Kind == wellknown.KindSystem {
				want = "private"
			}
			if s.Op != wellknown.OpCreate || s.Decl.Level != want {
				t.Errorf("%s %s at %q, want %q", s.Op, s.Key, s.Decl.Level, want)
			}
		}
	})
	t.Run("R-REPO-067 a second run against the same catalog writes nothing", func(t *testing.T) {
		if steps := plan(t, decls, all); len(steps) != 0 {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
	})
	t.Run("R-REPO-067 step 2 a changed URL is written; the account stays while its host stays", func(t *testing.T) {
		stored := with("npmjs", func(s *wellknown.Stored) { s.Address.URL = "https://registry.npmjs.org/old/"; s.HasAccount = true })
		steps := plan(t, decls, stored)
		if len(steps) != 1 || steps[0].Op != wellknown.OpWrite || steps[0].Key != "npmjs" || !steps[0].Write.Address || steps[0].Write.RemoveAccount {
			t.Fatalf("steps = %+v", steps)
		}
	})
	t.Run("Q-REPO-377 step 2 an account goes when the product moves its host", func(t *testing.T) {
		stored := with("npmjs", func(s *wellknown.Stored) { s.Address.URL = "https://npm.example.com/"; s.HasAccount = true })
		steps := plan(t, decls, stored)
		if len(steps) != 1 || !steps[0].Write.RemoveAccount {
			t.Fatalf("steps = %+v", steps)
		}
		stored = with("docker-hub", func(s *wellknown.Stored) {
			s.Address.ExtraHosts = []catalog.PresetHost{{Pattern: "auth.docker.io", SendCredentials: false}}
			s.HasAccount = true
		})
		if steps := plan(t, decls, stored); len(steps) != 1 || !steps[0].Write.RemoveAccount {
			t.Fatalf("a host that newly receives the credential keeps the account: %+v", steps)
		}
	})
	t.Run("R-REPO-067 step 3 an administrator's address stays, and the flag goes once the product's address caught up", func(t *testing.T) {
		stored := with("quay", func(s *wellknown.Stored) { s.Address.URL = "https://mirror.example.com/"; s.Overridden = true })
		if steps := plan(t, decls, stored); len(steps) != 0 {
			t.Fatalf("an override is written over: %v", stepsOf(steps))
		}
		stored = with("quay", func(s *wellknown.Stored) { s.Overridden = true })
		if steps := plan(t, decls, stored); len(steps) != 1 || steps[0].Op != wellknown.OpClearOverride {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
	})
	t.Run("R-REPO-067 step 2 the archive keys a preset carries are part of its address", func(t *testing.T) {
		stored := with("debian", func(s *wellknown.Stored) { s.Address.Settings = json.RawMessage(`{"upstreamKeys":"old"}`) })
		if steps := plan(t, decls, stored); len(steps) != 1 || !steps[0].Write.Address {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
		stored = with("debian", func(s *wellknown.Stored) {
			var m map[string]any
			_ = json.Unmarshal(s.Address.Settings, &m)
			m["compression"] = []string{"xz"} // a member the preset does not carry is not compared
			s.Address.Settings, _ = json.Marshal(m)
		})
		if steps := plan(t, decls, stored); len(steps) != 0 {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
	})
	t.Run("R-REPO-070 the fixed members of scanner-trivy are written back, also under an administrator's address", func(t *testing.T) {
		stored := with("scanner-trivy", func(s *wellknown.Stored) {
			s.Fixed.IncludePatterns = []string{"aquasec/*"}
			s.Address.URL = "https://mirror.example.com/"
			s.Overridden = true
		})
		steps := plan(t, decls, stored)
		if len(steps) != 1 || !steps[0].Write.Fixed || steps[0].Write.Address {
			t.Fatalf("steps = %+v", steps)
		}
	})
	t.Run("R-REPO-067 step 5 a preset the catalog no longer declares is retired once, and step 4 brings it back", func(t *testing.T) {
		without := slices.DeleteFunc(slices.Clone(decls), func(d wellknown.Declaration) bool { return d.Key == "galaxy" })
		steps := plan(t, without, all)
		if len(steps) != 1 || steps[0].Op != wellknown.OpRetire || steps[0].Key != "galaxy" {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
		retired := with("galaxy", func(s *wellknown.Stored) { s.Retired = true })
		if steps := plan(t, without, retired); len(steps) != 0 {
			t.Fatalf("a retired repository is retired again: %v", stepsOf(steps))
		}
		steps = plan(t, decls, retired)
		if len(steps) != 1 || steps[0].Op != wellknown.OpUnretire {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
	})
	t.Run("R-REPO-067 step 6 a declared key of another format writes nothing and names the key", func(t *testing.T) {
		stored := with("npmjs", func(s *wellknown.Stored) { s.Format = "oci" })
		_, err := wellknown.Plan(decls, stored)
		if err == nil || !strings.Contains(err.Error(), "upstream/npmjs") {
			t.Fatalf("err = %v", err)
		}
		rec := &recordingStore{stored: stored}
		if _, err := wellknown.Reconcile(context.Background(), rec, decls); err == nil || len(rec.calls) != 0 {
			t.Fatalf("err %v, calls %v", err, rec.calls)
		}
	})
	t.Run("Q-REPO-411 step 6 clamav stored as a remote repository writes nothing and names its class", func(t *testing.T) {
		stored := with("clamav", func(s *wellknown.Stored) { s.Class = catalog.Remote })
		_, err := wellknown.Plan(decls, stored)
		if err == nil || !strings.Contains(err.Error(), "upstream/clamav as a system local repository") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("R-REPO-075 clamav has no address to write back: the stored one is empty as the declaration's", func(t *testing.T) {
		stored := with("clamav", func(s *wellknown.Stored) { s.Address = wellknown.Address{} })
		if steps := plan(t, decls, stored); len(steps) != 0 {
			t.Fatalf("steps = %v", stepsOf(steps))
		}
	})
	t.Run("Q-REPO-414 the level of clamav while its clients are on is the administrator's: the reconcile writes nothing back", func(t *testing.T) {
		for _, private := range []bool{true, false} {
			stored := with("clamav", func(s *wellknown.Stored) { s.Fixed.Private = private })
			if steps := plan(t, decls, stored); len(steps) != 0 {
				t.Fatalf("private %v: steps = %v", private, stepsOf(steps))
			}
		}
		stored := with("scanner-trivy", func(s *wellknown.Stored) { s.Fixed.Private = false })
		if steps := plan(t, decls, stored); len(steps) != 1 || !steps[0].Write.Fixed {
			t.Fatalf("scanner-trivy's fixed level is not written back: %v", stepsOf(steps))
		}
	})
	t.Run("R-REPO-067 Reconcile applies the plan through the store and stops at its first error", func(t *testing.T) {
		rec := &recordingStore{fail: "quay"}
		_, err := wellknown.Reconcile(context.Background(), rec, decls)
		if err == nil || !strings.Contains(err.Error(), "quay") {
			t.Fatalf("err = %v", err)
		}
		if got := rec.calls[len(rec.calls)-1]; got != "create quay" {
			t.Fatalf("calls = %v", rec.calls)
		}
	})
}

// recordingStore records the calls of a reconcile; a create of fail fails.
type recordingStore struct {
	stored []wellknown.Stored
	fail   string
	calls  []string
}

func (r *recordingStore) Stored(context.Context) ([]wellknown.Stored, error) { return r.stored, nil }

func (r *recordingStore) Create(_ context.Context, d wellknown.Declaration) error {
	r.calls = append(r.calls, "create "+d.Key)
	if d.Key == r.fail {
		return errors.New("refused")
	}
	return nil
}

func (r *recordingStore) Write(_ context.Context, d wellknown.Declaration, _ wellknown.Write) error {
	r.calls = append(r.calls, "write "+d.Key)
	return nil
}

func (r *recordingStore) SetOverridden(_ context.Context, key string, _ bool) error {
	r.calls = append(r.calls, "override "+key)
	return nil
}

func (r *recordingStore) Retire(_ context.Context, key string) error {
	r.calls = append(r.calls, "retire "+key)
	return nil
}

func (r *recordingStore) Unretire(_ context.Context, key string) error {
	r.calls = append(r.calls, "unretire "+key)
	return nil
}
