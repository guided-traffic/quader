package app

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/config"
)

// failureLimiter satisfies auth.FailureLimiter for a step that only checks its inputs.
type failureLimiter struct{ auth.FailureLimiter }

func appFor(t *testing.T, role string) *App {
	t.Helper()
	sub, err := SubsystemsFor(role)
	if err != nil {
		t.Fatal(err)
	}
	return &App{sub: sub, parts: &wiring{}, cfg: &config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestR_ARCH_001_AuthAreaWithoutSessions is the open code fact of WP6a.1 on how the authentication area is built without the
// sessions area under typed wiring (Q-ARCH-300): the session store is an optional input, the role decides whether it must be
// there, and the role packages gets a stage over tokens only.
func TestR_ARCH_001_AuthAreaWithoutSessions(t *testing.T) {
	id, lim := identityArea{tokens: &token.Service{}}, limitsArea{failures: failureLimiter{}}

	t.Run("R-ARCH-001 api, web and all stop the start-up when the auth area gets no session store", func(t *testing.T) {
		for _, role := range []string{"api", "web", "all"} {
			_, err := appFor(t, role).wireAuth(t.Context(), id, lim, sessionsArea{})
			if err == nil || !strings.Contains(err.Error(), "auth: no session store") {
				t.Errorf("%s: %v", role, err)
			}
		}
	})

	t.Run("R-ARCH-001 packages builds the auth area without a session store: a stage over tokens only", func(t *testing.T) {
		out, err := appFor(t, "packages").wireAuth(t.Context(), id, lim, sessionsArea{})
		if err != nil {
			t.Fatal(err)
		}
		if out.stage == nil {
			t.Fatal("no stage")
		}
	})

	t.Run("R-ARCH-001 the auth area of packages still needs the token service and the failure limiter", func(t *testing.T) {
		_, err := appFor(t, "packages").wireAuth(t.Context(), identityArea{}, limitsArea{}, sessionsArea{})
		if err == nil || !strings.HasPrefix(err.Error(), "auth: no token service, no failure limiter") {
			t.Errorf("err = %v", err)
		}
	})
}

// TestR_ARCH_001_RequireServingPerRole: what a role must have before its listener opens follows from its areas, so api is not
// asked for the format dispatcher it must not build and packages is not asked for a login handler.
func TestR_ARCH_001_RequireServingPerRole(t *testing.T) {
	cases := map[string]struct{ named, notNamed []string }{
		"api": {
			named:    []string{"login handler", "session store", "CI exchange", "package services", "vulnerability report", "virtual engine", "authentication stage"},
			notNamed: []string{"format dispatcher", "remote engine"},
		},
		"packages": {
			named:    []string{"format dispatcher", "remote engine", "virtual engine", "authentication stage", "files service"},
			notNamed: []string{"login handler", "session store", "CI exchange", "package services", "vulnerability report"},
		},
		"web": {
			named: []string{"login handler", "session store", "CI exchange", "package services", "format dispatcher", "remote engine"},
		},
	}
	for role, tc := range cases {
		t.Run("R-ARCH-001 a "+role+" replica without its areas is not opened, naming what is missing", func(t *testing.T) {
			err := appFor(t, role).requireServing()
			if err == nil || !strings.Contains(err.Error(), "client listener") {
				t.Fatalf("err = %v", err)
			}
			for _, n := range tc.named {
				if !strings.Contains(err.Error(), "no "+n) {
					t.Errorf("%q does not name %q", err, n)
				}
			}
			for _, n := range tc.notNamed {
				if strings.Contains(err.Error(), "no "+n) {
					t.Errorf("%q asks a %s for %q", err, role, n)
				}
			}
		})
	}
}

// TestR_ARCH_001_VirtualEngineOverTheView: the remote engine is an optional input of the virtual engine. A role that builds the
// proxy-engine area must hand it over; the role api has none and builds the read-only view (Q-ARCH-294).
func TestR_ARCH_001_VirtualEngineOverTheView(t *testing.T) {
	for role, wantsEngine := range map[string]bool{"api": false, "packages": true, "web": true, "all": true} {
		_, err := appFor(t, role).wireVirtualEngine(t.Context(), infra{}, virtualArea{}, authzArea{}, repositoryArea{}, formatsArea{},
			invalidationArea{}, vulnArea{}, filesArea{}, proxyEngineArea{})
		if err == nil || !strings.HasPrefix(err.Error(), "virtual-engine: ") {
			t.Fatalf("%s: err = %v", role, err)
		}
		if got := strings.Contains(err.Error(), "no remote engine"); got != wantsEngine {
			t.Errorf("%s: %q names the remote engine: %v, want %v", role, err, got, wantsEngine)
		}
	}
}

// managementNeeds is what an operation of the management API that a format decides needs of a role, per operation (Q-ARCH-1070): the
// Host it writes through, and the virtual engine that Host asks what the caller may read of a repository (formats.CallerVersions).
// The operation is served on the roles with the management API (R-ARCH-001); an operation that adds a need adds a row.
var managementNeeds = map[string][]Area{
	"PUT and DELETE .../packages/{name}/deprecation (R-API-085)": {AreaManagementHost, AreaVirtualEngine},
}

// TestR_ARCH_001_AManagementOperationAFormatDecidesHasItsHostOnEveryManagementRole: every role with the management API enables what
// the operations a format decides need, and the role api, which builds no format host, refuses to start without the virtual engine its
// Host resolves the caller's read scope with; web and all take their format host. A Host without a Resolver answered the deprecation
// with 500 on api (Q-ARCH-1070); the role table alone did not show it, since it lists areas, not what a Host was built with.
func TestR_ARCH_001_AManagementOperationAFormatDecidesHasItsHostOnEveryManagementRole(t *testing.T) {
	for _, role := range []string{"api", "web", "all"} {
		sub, err := SubsystemsFor(role)
		if err != nil {
			t.Fatal(err)
		}
		for op, needs := range managementNeeds {
			for _, area := range needs {
				if !sub.Has(area) {
					t.Errorf("%s: %s needs the area %s", role, op, area)
				}
			}
		}
	}
	_, err := appFor(t, "api").wireManagementHost(t.Context(), infra{}, repositoryArea{}, authzArea{}, formatsArea{}, filesArea{},
		virtualEngineArea{}, formatHostArea{})
	if err == nil || !strings.HasPrefix(err.Error(), "management-host: ") || !strings.Contains(err.Error(), "no virtual engine") {
		t.Errorf("api: err = %v", err)
	}
	for _, role := range []string{"web", "all"} {
		_, err := appFor(t, role).wireManagementHost(t.Context(), infra{}, repositoryArea{}, authzArea{}, formatsArea{}, filesArea{},
			virtualEngineArea{}, formatHostArea{})
		if err == nil || !strings.Contains(err.Error(), "no format host") || strings.Contains(err.Error(), "no virtual engine") {
			t.Errorf("%s: err = %v", role, err)
		}
	}
}
