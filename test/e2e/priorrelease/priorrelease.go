// Package priorrelease holds the two rules of the upgrade from the previous release (docs/developer/e2e.md#the-upgrade-tests):
// which published release the test starts from (Q-HA-311), and which changes of the migration directory since that release
// the upgrade cannot cross without downtime or a new database (Q-HA-312). It has no build tag so that both rules are
// unit-tested without a cluster, a checkout's tags or the network.
package priorrelease

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var releaseTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// IsReleaseTag says whether tag names a release (vX.Y.Z).
func IsReleaseTag(tag string) bool { return releaseTag.MatchString(tag) }

// Listed returns the versions of chart that a Helm repository index (the repository's index.yaml) lists.
func Listed(index []byte, chart string) (map[string]bool, error) {
	var doc struct {
		Entries map[string][]struct {
			Version string `yaml:"version"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(index, &doc); err != nil {
		return nil, fmt.Errorf("the repository index is not YAML: %w", err)
	}
	if doc.Entries == nil {
		return nil, fmt.Errorf("the repository index has no entries")
	}
	listed := map[string]bool{}
	for _, e := range doc.Entries[chart] {
		listed[e.Version] = true
	}
	return listed, nil
}

// Pick returns the release the upgrade starts from: the newest release tag of tags (the tags merged into the commit under test,
// in any order; other tags are ignored) that does not point at the commit itself (atHead) and whose chart version the
// repository lists. passed names every newer release tag it passed over and why: a release cut minutes ago whose chart the
// release workflow has not published yet, or a release of a name whose packages the repository no longer offers. tag is
// empty when no release tag qualifies.
func Pick(tags, atHead []string, listed map[string]bool) (tag string, passed []string) {
	head := map[string]bool{}
	for _, t := range atHead {
		head[t] = true
	}
	var rel []string
	for _, t := range tags {
		if IsReleaseTag(t) {
			rel = append(rel, t)
		}
	}
	sort.Slice(rel, func(i, j int) bool { return newer(rel[i], rel[j]) })
	for _, t := range rel {
		switch {
		case head[t]:
			passed = append(passed, t+" (points at the commit under test)")
		case !listed[strings.TrimPrefix(t, "v")]:
			passed = append(passed, t+" (the chart repository does not list it)")
		default:
			return t, passed
		}
	}
	return "", passed
}

// newer orders two release tags by their version, the newest first.
func newer(a, b string) bool {
	va, vb := releaseTag.FindStringSubmatch(a), releaseTag.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(va[i])
		y, _ := strconv.Atoi(vb[i])
		if x != y {
			return x > y
		}
	}
	return false
}

// Breaks reads `git diff --name-status <release> HEAD -- <migrations directory>` and returns each change that the upgrade
// from that release cannot cross without downtime or a new database: a step the release applied that was changed, removed or
// renamed (the runner refuses a database whose applied file differs, R-ARCH-026), and an added step that contract says
// carries `-- quader:phase contract` (R-HA-021: one release at a time, or with downtime). contract is asked for added files
// only, with the path as git prints it.
func Breaks(nameStatus string, contract func(path string) (bool, error)) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(nameStatus))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] == "" {
			return nil, fmt.Errorf("a line of git diff --name-status that is not one: %q", line)
		}
		switch status := f[0]; status[0] {
		case 'A':
			c, err := contract(f[1])
			if err != nil {
				return nil, err
			}
			if c {
				out = append(out, filepath.Base(f[1])+" is a contract step added since the release")
			}
		case 'M', 'T':
			out = append(out, filepath.Base(f[1])+" was changed after the release applied it")
		case 'D':
			out = append(out, filepath.Base(f[1])+" was removed after the release applied it")
		case 'R', 'C':
			if len(f) < 3 {
				return nil, fmt.Errorf("a rename or copy line of git diff --name-status without its target: %q", line)
			}
			if status[0] == 'R' {
				out = append(out, filepath.Base(f[1])+" was renamed to "+filepath.Base(f[2])+" after the release applied it")
			} else if c, err := contract(f[2]); err != nil {
				return nil, err
			} else if c {
				out = append(out, filepath.Base(f[2])+" is a contract step added since the release")
			}
		default:
			return nil, fmt.Errorf("git diff --name-status reports the status %q, which this rule does not know: %q", status, line)
		}
	}
	return out, sc.Err()
}
