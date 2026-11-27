package ciworkflows

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// The build context of the release image keeps every committed file a stage of the Containerfile reads (R-ARCH-003). From 22:24 to
// 00:47 of 2026-11-26 the release image did not build and no release followed three pushes: .dockerignore's `internal/webui/dist/*/`
// was meant for the locale folders of a local web build, but Docker cleans a pattern's trailing slash off, so it left out every file
// of the folder, among them the package file embed.go. The unit tests built Go from the checkout and passed; only the job image saw
// it. The check applies .dockerignore with Docker's own parser and matcher (github.com/moby/patternmatcher, which BuildKit reads the
// file with) to `git ls-files`, so the fast jobs catch a pattern that leaves out what the build needs, before the image job.

// contextNeeds reports whether a stage of the Containerfile reads a committed file from the build context: every Go source and
// go.mod and go.sum (the build stage), every file at or below a directory that holds a Go source (what //go:embed can reach), and
// every file below a source the Containerfile copies by name (the web stage's web/ and the committed placeholders of the embedded
// bundles). COPY . . copies the rest, which no stage reads.
type contextNeeds struct {
	packageDirs []string
	copied      []string
}

func newContextNeeds(files []string, containerfile string) contextNeeds {
	var n contextNeeds
	for _, f := range files {
		if strings.HasSuffix(f, ".go") {
			n.packageDirs = append(n.packageDirs, filepath.ToSlash(filepath.Dir(f)))
		}
	}
	slices.Sort(n.packageDirs)
	n.packageDirs = slices.Compact(n.packageDirs)
	n.copied = copySources(containerfile)
	return n
}

// copySources are the paths the Containerfile copies from the build context by name: the arguments of each COPY or ADD without
// --from but the destination, without the flags and without ".", which names the whole context.
func copySources(containerfile string) []string {
	var out []string
	for _, line := range strings.Split(containerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || (fields[0] != "COPY" && fields[0] != "ADD") {
			continue
		}
		args := fields[1:]
		if slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--from=") }) {
			continue
		}
		args = slices.DeleteFunc(args, func(a string) bool { return strings.HasPrefix(a, "--") })
		for _, src := range args[:len(args)-1] {
			if src = strings.TrimSuffix(strings.TrimPrefix(src, "./"), "/"); src != "." && src != "" {
				out = append(out, src)
			}
		}
	}
	return out
}

func (n contextNeeds) needs(file string) bool {
	if strings.HasSuffix(file, ".go") || file == "go.mod" || file == "go.sum" {
		return true
	}
	under := func(dir string) bool { return dir == "." || file == dir || strings.HasPrefix(file, dir+"/") }
	return slices.ContainsFunc(n.packageDirs, under) || slices.ContainsFunc(n.copied, under)
}

// leftOut lists the files of files that a stage needs and the patterns of a .dockerignore leave out of the build context. A file is
// left out when it or a directory above it matches, as BuildKit decides it.
func leftOut(t *testing.T, patterns, files []string, n contextNeeds) []string {
	t.Helper()
	pm, err := patternmatcher.New(patterns)
	if err != nil {
		t.Fatalf("the patterns of .dockerignore: %v", err)
	}
	var out []string
	for _, f := range files {
		if !n.needs(f) {
			continue
		}
		excluded, err := pm.MatchesOrParentMatches(f)
		if err != nil {
			t.Fatalf("matching %s: %v", f, err)
		}
		if excluded {
			out = append(out, f)
		}
	}
	return out
}

// committedFiles are the files of the checkout's index, as the build context of a clean checkout holds them.
func committedFiles(t *testing.T) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	return strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
}

func dockerignorePatterns(t *testing.T, text string) []string {
	t.Helper()
	patterns, err := ignorefile.ReadAll(strings.NewReader(text))
	if err != nil {
		t.Fatalf("reading .dockerignore: %v", err)
	}
	return patterns
}

func TestR_ARCH_003_TheImagesBuildContextHoldsEveryCommittedFileTheBuildReads(t *testing.T) {
	files := committedFiles(t)
	n := newContextNeeds(files, readRepoFile(t, "Containerfile"))
	if !slices.Contains(n.copied, "web") || !slices.Contains(n.copied, "internal/webui/dist") {
		t.Fatalf("the sources the Containerfile copies by name are %v, want web and internal/webui/dist among them", n.copied)
	}
	if missing := leftOut(t, dockerignorePatterns(t, readRepoFile(t, ".dockerignore")), files, n); len(missing) > 0 {
		t.Fatalf(".dockerignore leaves %d committed files out of the build context that a stage of the Containerfile reads:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// The line of the defect, with the exceptions that repaired it taken out, is found: the check would have failed the push of 22:24.
func TestR_ARCH_003_TheBuildContextCheckFindsThePackageFileATrailingSlashLeftOut(t *testing.T) {
	files := committedFiles(t)
	n := newContextNeeds(files, readRepoFile(t, "Containerfile"))
	var lines []string
	for _, l := range strings.Split(readRepoFile(t, ".dockerignore"), "\n") {
		if !strings.Contains(l, "internal/webui/dist/") {
			lines = append(lines, l)
		}
	}
	lines = append(lines, "internal/webui/dist/*/")
	missing := leftOut(t, dockerignorePatterns(t, strings.Join(lines, "\n")), files, n)
	if !slices.Contains(missing, "internal/webui/dist/embed.go") || !slices.Contains(missing, "internal/webui/dist/index.html") {
		t.Fatalf("with internal/webui/dist/*/ the check found %v, want the package file and the placeholder", missing)
	}
}
