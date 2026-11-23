package formathost

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-REPO-005, R-REPO-065: the background path admits only what the repository's include and exclude patterns admit. The decision on
// a probe (remote.admits, per file of the probe) and every fill of the job and of a mirror pass (MirrorHost.admitted) ask one check,
// patternsAdmit, the same for every format; a probe whose own request the patterns refuse never reaches it (the read's refusal, the
// integration case TestR_REPO_065_TheSwitchAndThePatternsStopABackgroundDownload). Milliseconds.
func TestR_REPO_065_TheBackgroundPathAdmitsOnlyWhatThePatternsAdmit(t *testing.T) {
	doc := `{"includePatterns":["pkg/**"],"excludePatterns":["pkg/secret/**"],"remote":{"url":"https://upstream.example.org/","mode":"mirror"}}`
	repo, err := repository.Build(7, 1, qr("raw-remote"), repository.Remote, "raw", []byte(doc), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := portFormat{id: "raw"}
	for path, want := range map[string]bool{"pkg/a.bin": true, "pkg/secret/key.bin": false, "other/b.bin": false} {
		if got := patternsAdmit(repo, f, formats.Path{Path: path}); got != want {
			t.Errorf("%s: admitted %v, want %v", path, got, want)
		}
	}
}
