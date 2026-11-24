package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// R-VULN-044: the layer of the database is the one of its media type in the tag's manifest; the manifest is untrusted, so a digest
// that is no sha256 (it names an item and a directory of the worker's cache) and a manifest without that layer are refused.
func TestR_VULN_044_TheDatabasesLayerIsTheOneOfItsMediaType(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	manifest := func(layers ...string) []byte {
		return []byte(`{"schemaVersion":2,"config":{"mediaType":"application/vnd.aquasec.trivy.config.v1+json","digest":"` + digest + `","size":2},"layers":[` + strings.Join(layers, ",") + `]}`)
	}
	layer := func(media, d string, size int) string {
		return fmt.Sprintf(`{"mediaType":%q,"digest":%q,"size":%d}`, media, d, size)
	}
	got, err := databaseLayer(manifest(layer("application/vnd.oci.image.layer.v1.tar", "sha256:"+strings.Repeat("cd", 32), 9), layer(imagescan.LayerMediaType, digest, 141735981)))
	if err != nil || got.Digest != digest || got.Size != 141735981 {
		t.Fatalf("%+v %v", got, err)
	}
	for name, m := range map[string][]byte{
		"no layer of the media type":    manifest(layer("application/vnd.oci.image.layer.v1.tar", digest, 9)),
		"a digest that climbs out":      manifest(layer(imagescan.LayerMediaType, "sha256:../../etc", 9)),
		"a digest of another algorithm": manifest(layer(imagescan.LayerMediaType, "sha512:"+strings.Repeat("ab", 64), 9)),
		"upper-case hexadecimal digits": manifest(layer(imagescan.LayerMediaType, strings.ToUpper(digest), 9)),
		"a negative size":               manifest(layer(imagescan.LayerMediaType, digest, -1)),
		"no JSON":                       []byte("<html>"),
		"a manifest larger than 4 MiB":  append(manifest(), make([]byte, maxDatabaseManifest)...),
	} {
		if _, err := databaseLayer(m); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// R-VULN-044, Q-VULN-174: a refusal of the repository layer maps to the code that names why, and the detail names the repository and
// the reference; any other error is the run's as it is.
func TestR_VULN_044_ARunThatCannotUseTheRepositoryFailsWithItsCode(t *testing.T) {
	ref := "ops/scanner-db aquasec/trivy-db:2"
	for want, cause := range map[string]error{
		errs.CodeDatabaseRepositoryInvalid: fmt.Errorf("%w: the repository ops/scanner-db is virtual", ErrDatabaseRepositoryInvalid),
		errs.CodeDatabaseRepositoryOff:     fmt.Errorf("%w: the repository ops/scanner-db is switched off", ErrDatabaseRepositoryOff),
		errs.CodeUpstreamFailed:            fmt.Errorf("%w: ops/scanner-db cannot reach its registry: circuit open", ErrDatabaseUpstream),
	} {
		e := asError(databaseFailure(ref, cause))
		if e.Code != want || !strings.Contains(e.Detail, ref) || !strings.Contains(e.Detail, "ops/scanner-db") {
			t.Errorf("%v: code %q, detail %q", cause, e.Code, e.Detail)
		}
		// A refusal of the configuration answers its registered kind, a conflict, and an upstream's failure 502.
		if info, _ := errs.Lookup(want); e.Kind != info.Kind {
			t.Errorf("%s has the kind %v, registered %v", want, e.Kind, info.Kind)
		}
		if _, ok := errs.Lookup(want); !ok {
			t.Errorf("the code %q is not in the registry", want)
		}
	}
	// A refusal with what was found is shown as that sentence after the reference, without the sentinel's text.
	refusal := &DatabaseRefusal{Kind: ErrDatabaseMissing, Detail: "nothing was pushed to the local repository under this tag"}
	if e := asError(databaseFailure(ref, refusal)); e.Code != errs.CodeDatabaseMissing || e.Detail != ref+": "+refusal.Detail {
		t.Errorf("a refusal: code %q, detail %q", e.Code, e.Detail)
	}
	other := errors.New("the metadata store failed")
	if got := databaseFailure(ref, other); !errors.Is(got, other) {
		t.Errorf("another error became %v", got)
	}
	if _, ok := errs.Lookup(errs.CodeDatabaseMissing); !ok {
		t.Error("database_missing is not in the registry")
	}
}

// trimRepo is a database repository that records the trim it is asked for; the other methods are not reached by prune.
type trimRepo struct {
	DatabaseRepository
	hours    int
	removed  int
	err      error
	calls    int
	manifest string
	before   time.Time
}

func (r *trimRepo) RetentionHours() int { return r.hours }

func (r *trimRepo) Prune(_ context.Context, _, manifest string, before time.Time) (int, error) {
	r.calls++
	r.manifest, r.before = manifest, before
	return r.removed, r.err
}

// Q-VULN-175, Q-VULN-194: after a run that succeeded, the repository lets the layers that are not in use go after its own cache period,
// 48 hours on upstream/scanner-trivy, counted back from the clock; the manifest the run read is named to stay. A run that failed and a
// repository without a period (a local repository, a remote of an own project) remove nothing. A trim that fails leaves the run a
// success with a note.
func TestR_VULN_044_TheRetentionIsTheRepositorysOwnCachePeriod(t *testing.T) {
	now := time.Date(2026, 11, 20, 23, 0, 0, 0, time.UTC)
	s := &Service{o: Options{Now: func() time.Time { return now }}, log: slog.New(slog.DiscardHandler)}
	manifest := []byte(`{"schemaVersion":2}`)
	sum := sha256.Sum256(manifest)

	repo := &trimRepo{hours: 48, removed: 3}
	res, err := s.prune(t.Context(), Result{}, nil, repo, "aquasec/trivy-db", manifest)
	if err != nil || repo.calls != 1 || res.Deleted != 3 || res.Note != "" {
		t.Fatalf("a run that succeeded: %+v %v, %d trims", res, err, repo.calls)
	}
	if !repo.before.Equal(now.Add(-48*time.Hour)) || repo.manifest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Errorf("the trim's bound %v and kept manifest %s", repo.before, repo.manifest)
	}

	failed := errors.New("database_missing")
	repo = &trimRepo{hours: 48}
	if _, err := s.prune(t.Context(), Result{}, failed, repo, "aquasec/trivy-db", manifest); !errors.Is(err, failed) || repo.calls != 0 {
		t.Errorf("a run that failed: %v, %d trims", err, repo.calls)
	}

	for _, h := range []int{0, -1} {
		repo = &trimRepo{hours: h}
		if _, err := s.prune(t.Context(), Result{}, nil, repo, "aquasec/trivy-db", manifest); err != nil || repo.calls != 0 {
			t.Errorf("%d hours: %v, %d trims", h, err, repo.calls)
		}
	}

	repo = &trimRepo{hours: 48, removed: 1, err: errors.New("the metadata store failed")}
	res, err = s.prune(t.Context(), Result{}, nil, repo, "aquasec/trivy-db", manifest)
	if err != nil || res.Deleted != 1 || !strings.Contains(res.Note, "the metadata store failed") {
		t.Errorf("a trim that failed: %+v %v", res, err)
	}
}
