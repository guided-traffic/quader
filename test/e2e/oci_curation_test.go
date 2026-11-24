//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestOCICuration_R_CUR_004_WhatDockerShowsOfARemoteThatDeliversOnlyWhatIsAllowed is OCI's one case of the curation of a remote
// repository (remote-curation.md R-CUR-022): what only the real client shows. The rules' decision, the filtered tags list, the queue and
// the requests are the unit tests', the kit's and the integration area ociremote's. A remote of the counting test upstream in mode
// onlyAllowed, docker in one clients pod with its images pruned first (so every layer comes from Quader); the stub's images are a few
// kilobytes:
//
//   - docker pull of a tag the remote does not deliver fails and prints the rules' text after "error from registry:": Docker 29 prints
//     the message of the registry's DENIED error, not its code (R-CUR-004);
//   - after an allow of library/alpine:3 through the API, the pull of that tag succeeds (R-CUR-003, Q-CUR-10: content by digest follows
//     the allowed tag);
//   - library/nginx:1 downloaded ahead through the queue (stored for examination, R-CUR-014) and then allowed with the image is pulled
//     from the cache: the upstream delivered each blob of the client's platform once, to the download (R-CUR-010).
func TestOCICuration_R_CUR_004_WhatDockerShowsOfARemoteThatDeliversOnlyWhatIsAllowed(t *testing.T) {
	fx := ociFx(t)
	defer caseClock(t)()
	s := ociStub(t)
	host := ociRegistryHost(t)
	arch := ociArch(t)
	// A remote of its own, without the local and virtual repositories of upstreamRepos, whose wait for the replicas probes a manifest the
	// rules of this remote refuse: the wait here is the rules' refusal of library/alpine:3 on every replica (a replica that does not know
	// the repository answers NAME_UNKNOWN), and a refusal asks the upstream nothing (R-CUR-003).
	remote := fmt.Sprintf("oci-curation-%s-remote", fx.run)
	fx.adm.do(http.MethodPost, reposRoute, ociRemoteBody(t, remote, onlyAllowed())).want(t, "create "+remote, 201)
	t.Cleanup(func() { deleteRepository(t, fx.adm, remote) })
	ensurePolicy(t, fx.adm, "e2e-oci-curation-"+fx.run, []string{remote}, [2]any{"sa:" + ociPublisher, []string{"read"}})
	refused := func(r reply) bool {
		return r.Status == http.StatusForbidden && strings.Contains(string(r.Body), "is not allowed in")
	}
	awaitOnEveryPackagesReplica(t, fx.token, "the rules of "+remote+" refuse library/alpine:3", "/v2/"+repoRef(remote)+"/library/alpine/manifests/3", refused, "Accept", ociManifestAccept)
	pruneClients(t, 1, ociDocker)
	c := fx.client(t, 0, ociDocker, "curation")
	c.login(host)

	// lists is the predicate of a replica that serves the filtered tags list of an image with the tag.
	lists := func(tag string) func(reply) bool {
		return func(r reply) bool {
			var d struct {
				Tags []string `json:"tags"`
			}
			return r.Status == 200 && json.Unmarshal(r.Body, &d) == nil && slices.Contains(d.Tags, tag)
		}
	}
	tags := func(name string) string { return "/v2/" + repoRef(remote) + "/" + name + "/tags/list" }

	t.Run("R-CUR-004 docker pull of a tag the remote does not deliver fails and prints the rules' text", func(t *testing.T) {
		start := time.Now()
		out, err := c.try("pull", fx.image(t, remote, "library/alpine", "3"))
		noteCuration(t, out, clientCuration{Client: "docker pull", Printed: firstLineWith(out, "is not allowed in"), Exit: exitOf(err), Seconds: time.Since(start).Seconds()})
		if err == nil || !strings.Contains(out, "error from registry: library/alpine 3 is not allowed in "+qn(remote)) {
			t.Fatalf("docker pull of a tag the rules refuse: %v, want the rules' text:\n%s", err, tailLines(out, 20))
		}
	})

	t.Run("R-CUR-003 after an allow of library/alpine:3 its pull succeeds", func(t *testing.T) {
		allowRule(t, fx.adm, remote, "library/alpine", "3")
		awaitOnEveryPackagesReplica(t, fx.token, "the tags of library/alpine list 3", tags("library/alpine"), lists("3"))
		ref := fx.image(t, remote, "library/alpine", "3")
		c.pull(ref)
		if info := s.ociImage(t, "library/alpine", "3"); !c.holds(ref, info.Digest) {
			t.Fatalf("docker holds %q after the pull, the upstream's index is %s", c.digests(ref), info.Digest)
		}
	})

	t.Run("R-CUR-010 R-CUR-014 library/nginx:1 downloaded ahead and then allowed is pulled from the cache", func(t *testing.T) {
		info := s.ociImage(t, "library/nginx", "1")
		blobs := ociPlatformBlobs(t, info, arch)
		fetched := func() []int {
			counts := s.ociCounts(t)
			out := make([]int, len(blobs))
			for i, d := range blobs {
				out[i] = counts.BlobFetches(d)
			}
			return out
		}
		before := fetched()
		downloadAhead(t, fx.adm, remote, "library/nginx", "1")
		afterDownload := fetched()
		for i, d := range blobs {
			if afterDownload[i]-before[i] != 1 {
				t.Fatalf("the download ahead fetched the blob %s %d times, want once", d, afterDownload[i]-before[i])
			}
		}
		allowRule(t, fx.adm, remote, "library/nginx", "")
		awaitOnEveryPackagesReplica(t, fx.token, "the tags of library/nginx list 1", tags("library/nginx"), lists("1"))
		ref := fx.image(t, remote, "library/nginx", "1")
		c.pull(ref)
		if !c.holds(ref, info.Digest) {
			t.Fatalf("docker holds %q after the pull, the upstream's index is %s", c.digests(ref), info.Digest)
		}
		if got := fetched(); !slices.Equal(got, afterDownload) {
			t.Fatalf("the upstream delivered the blobs of library/nginx:1 %v times after the pull, %v after the download ahead: the pull did not come from the cache", got, afterDownload)
		}
	})
}
