package all_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	_ "github.com/guided-traffic/quader/internal/formats/all"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_STOR_007_NoFormatWithAProtocolOfItsOwnTakesARawDeploy holds the rule of R-STOR-007, R-API-025 and Q-ARCH-223 for every
// registered format: /files/ deploys only into a repository whose format has /files/ as its protocol (the generic format), and a
// deploy into any other format's repository is refused with 409 conflict, whose detail names the format's protocol address. The
// files service asks the seam before it reads the body, for a file and for a folder alike (internal/files, the test of the same
// requirement), so the refusal is the whole answer. A format added to Quader is held to the rule here without a test of its own.
func TestR_STOR_007_NoFormatWithAProtocolOfItsOwnTakesARawDeploy(t *testing.T) {
	seam := formathost.NewFilesSeam(func() (formats.Host, error) { return nil, nil }, formathost.Deps{})
	var takes, refuses []string
	for _, f := range formats.List() {
		d := f.Descriptor()
		repo := repository.Repository{Name: qname.Repo{Project: "it", Key: string(d.ID) + "-local"}, Class: repository.Local, Format: string(d.ID)}
		t.Run("R-STOR-007 "+string(d.ID), func(t *testing.T) {
			err := seam.RawDeploy(repo)
			if d.ProtocolBasePath == "/files/{project}/{key}/" {
				if err != nil {
					t.Fatalf("a format whose protocol is /files/ refuses the deploy: %v", err)
				}
				takes = append(takes, string(d.ID))
				return
			}
			e, ok := errs.As(err)
			if !ok || e.Kind != errs.Conflict || e.Code != errs.CodeConflict {
				t.Fatalf("a format with a protocol of its own: err = %v, want 409 conflict", err)
			}
			if want := qname.ProtocolBase(d.ProtocolBasePath, repo.Name); !strings.Contains(e.Detail, want) {
				t.Errorf("the refusal %q does not name the protocol address %s", e.Detail, want)
			}
			refuses = append(refuses, string(d.ID))
		})
	}
	if !slices.Equal(takes, []string{"generic"}) {
		t.Errorf("the formats that take a raw deploy are %v, want [generic] alone", takes)
	}
	for _, id := range []string{"npm", "nuget", "oci", "deb", "ansible", "clamav"} {
		if !slices.Contains(refuses, id) {
			t.Errorf("%s is not among the formats that refuse a raw deploy %v", id, refuses)
		}
	}
}
