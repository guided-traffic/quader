//go:build integration

package repository

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_REPO_076_TheListIsOneRowReplacedUnderItsVersion: the stored row of the installation's registry presets. A write with the
// version read stores the list in its normal form, raises the version and records settings.registry_presets.changed with the lists
// before and after; a write with the old version is 412; the same list again records nothing and keeps the version; the defaults of a
// new remote repository of the format answer the format's presets in order and those of another class none. One database, a few rows.
func TestR_REPO_076_TheListIsOneRowReplacedUnderItsVersion(t *testing.T) {
	e := newStackEnv(t)
	rep := e.newReplica("presets", stackOpts{})
	ctx := context.Background()
	// The row names who wrote it (updated_by references principal): the caller is a user of the installation.
	e.exec(`INSERT INTO principal (id, kind, name) VALUES ($1, 'user', 'alice') ON CONFLICT DO NOTHING`, caller.Actor.ID)

	empty, err := rep.svc.RegistryPresets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Presets) != 0 {
		t.Fatalf("a new installation holds %v", empty.Presets)
	}
	list := []repository.RegistryPreset{
		{
			Format: "oci", DisplayName: "Harbor", URL: "https://Harbor.Example.com",
			ExtraHosts: []repository.ExtraHost{{Pattern: "Harbor.Example.com", SendCredentials: true}},
		},
		{Format: "npm", DisplayName: "Verdaccio", URL: "https://npm.example.com/"},
		{Format: "oci", DisplayName: "Registry of the build farm", URL: "https://registry.build.example.com/"},
	}
	saved, err := rep.svc.SetRegistryPresets(ctx, caller, empty.Version, list)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version == empty.Version || saved.Presets[0].URL != "https://harbor.example.com/" || saved.Presets[0].ExtraHosts[0].Pattern != "harbor.example.com" {
		t.Fatalf("saved %+v", saved)
	}
	read, err := rep.svc.RegistryPresets(ctx)
	if err != nil || read.Version != saved.Version || len(read.Presets) != 3 || read.Presets[1].ExtraHosts == nil {
		t.Fatalf("read back %+v, %v", read, err)
	}
	evs := e.eventsLike(rep.Name, "settings.registry_presets.changed")
	if len(evs) != 1 || !strings.Contains(evs[0].Details, `"before": []`) || !strings.Contains(evs[0].Details, `"https://harbor.example.com/"`) {
		t.Fatalf("events %+v", evs)
	}

	t.Run("R-REPO-076 a write with the old version is 412", func(t *testing.T) {
		_, err := rep.svc.SetRegistryPresets(ctx, caller, empty.Version, nil)
		wantCode(t, err, http.StatusPreconditionFailed, errs.CodePreconditionFailed)
	})
	t.Run("R-REPO-076 the same list again records nothing and keeps the version", func(t *testing.T) {
		again, err := rep.svc.SetRegistryPresets(ctx, caller, saved.Version, read.Presets)
		if err != nil || again.Version != saved.Version {
			t.Fatalf("again %+v, %v", again, err)
		}
		if n := len(e.eventsLike(rep.Name, "settings.registry_presets.changed")); n != 1 {
			t.Fatalf("%d events", n)
		}
	})
	t.Run("R-REPO-076 the defaults of a remote answer the format's presets in order", func(t *testing.T) {
		def, err := rep.svc.Defaults(ctx, caller, "it", "oci", repository.Remote)
		if err != nil {
			t.Fatal(err)
		}
		if len(def.RegistryPresets) != 2 || def.RegistryPresets[0].DisplayName != "Harbor" || def.RegistryPresets[1].DisplayName != "Registry of the build farm" {
			t.Fatalf("oci remote: %+v", def.RegistryPresets)
		}
		local, err := rep.svc.Defaults(ctx, caller, "it", "oci", repository.Local)
		if err != nil || local.RegistryPresets == nil || len(local.RegistryPresets) != 0 {
			t.Fatalf("oci local: %+v, %v", local.RegistryPresets, err)
		}
	})
}
