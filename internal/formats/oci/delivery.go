package oci

import (
	"context"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The delivery rules of an OCI remote (remote-curation.md R-CUR-003, formats/oci.md R-OCI-038, Q-CUR-10). The version is the tag: a
// tag the rules refuse is refused by the core before the engine reads it and left out of the tags list here. Content by digest names no
// tag, so the core decides it by the image's decision; that is right for a blob (H-1724) and too wide for a manifest by digest of an
// image with version rules, which this file decides: such a manifest is delivered when a delivered tag's stored document names it,
// directly or through a stored index (oci_references). The rules name an image by the name the cache knows (remoteName: library/<n>
// on Docker Hub). Nothing here runs for a repository without rules: formats.Delivery is nil (R-ARCH-027).

// hasVersionRules reports whether the set holds a version rule of the image.
func hasVersionRules(set *rules.Set, name string) bool { return set.VersionRules(name) }

// manifestUndelivered is the refusal of the rules for the manifest with the digest ref under the image name (R-OCI-038): the image's
// refusal, or, for an image with version rules, the refusal of a manifest that no delivered tag names. The refusal names the digest
// where a version stands. It reads the stored tags list and two rows only for an image with version rules.
func manifestUndelivered(ctx context.Context, rq *formats.Request, name, ref string, kind rules.RefusalKind) error {
	set := formats.Delivery(rq)
	if set == nil {
		return nil
	}
	if err := formats.Undelivered(rq, name, "", kind); err != nil {
		return err
	}
	sum, ok := manifest.ParseDigest(ref)
	if !ok || !hasVersionRules(set, name) {
		return nil
	}
	named, err := namedByDeliveredTag(ctx, rq, set, name, sum)
	if err != nil || named {
		return err
	}
	reason := rc.ReasonBlocked
	if set.Mode() == rc.OnlyAllowed {
		reason = rc.ReasonNotAllowed
	}
	return formats.RefusalOf(rq, set, name, ref, reason, kind)
}

// namedByDeliveredTag reports whether a tag of the stored tags list that the rules deliver has a stored document that is the manifest
// or an index that names it. A remote that holds no tags list of the image holds no tag that could name it.
func namedByDeliveredTag(ctx context.Context, rq *formats.Request, set *rules.Set, name string, digest [32]byte) (bool, error) {
	d, err := rq.Host.Remote().Metadata(ctx, rq, formats.Path{Path: TaglistPath(name)}, "")
	switch {
	case err == nil:
	case errs.KindOf(err) == errs.NotFound || unreachable(err):
		return false, nil
	default:
		return false, err
	}
	if d.Status.Negative {
		return false, nil
	}
	raw, err := d.Bytes(ctx)
	if err != nil {
		return false, err
	}
	list, err := parseTaglist(raw)
	if err != nil {
		return false, err
	}
	var paths []string
	for _, t := range list.Tags {
		if set.Decide(name, t).Delivered {
			paths = append(paths, TagPath(name, t))
		}
	}
	if len(paths) == 0 {
		return false, nil
	}
	st, err := storeOf(rq.Host)
	if err != nil {
		return false, err
	}
	repoID := rq.RepoRef().ID
	targets, err := st.TagTargets(ctx, nil, repoID, paths)
	if err != nil || len(targets) == 0 {
		return false, err
	}
	named := make(map[[32]byte]bool, len(targets))
	for _, t := range targets {
		if t == digest {
			return true, nil
		}
		named[t] = true
	}
	indexes, err := st.ReferencedBy(ctx, nil, repoID, name, digest)
	if err != nil {
		return false, err
	}
	for _, m := range indexes {
		if named[m] {
			return true, nil
		}
	}
	return false, nil
}

// deliveredMask narrows a mask of tags of the image by the rules (R-OCI-038 "Documents"): a tag they refuse is not listed. The host's
// listing decision joins the rules too (Host.PathsAllowed); deciding them here on the name the cache knows keeps the tags list right
// whichever spelling the client typed, and costs nothing without rules.
func deliveredMask(rq *formats.Request, name string, tags []string, mask []bool) {
	dv := formats.DeliveryView(rq)
	if dv == nil {
		return
	}
	for i, t := range tags {
		mask[i] = mask[i] && dv.Version(name, t)
	}
}
