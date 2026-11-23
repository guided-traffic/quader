package ansible

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
)

// The checks of a download at the version document (signatures.md Q-SIG-241). ansible-galaxy reads a version's document before its
// archive and prints the text of an error of that document (GalaxyError: "(HTTP Code: 403, Message: <detail> Code: <code>)"), while
// of the archive's download it prints only the status line (`HTTP Error 403: Forbidden`, urllib's reason phrase). So the version
// document answers what the archive's download would answer: the refusal or the pending answer of the malware gate, the vulnerability
// block or the signature gate (R-SIG-069), with their texts. An archive that no repository of the request stores yet (a remote's first
// download) is not judged here; its download is, and shows the status alone.

// localArchiveChecks decides the checks of the stored archive of a local repository's version.
func localArchiveChecks(ctx context.Context, rq *formats.Request, ns, name, ver string) error {
	it, found, err := rq.Host.Items().Stat(ctx, rq.RepoRef(), formats.Path{Path: ArtifactPath(ns, name, ver)})
	if err != nil || !found || it.Folder {
		return err
	}
	return rq.Host.Items().Check(ctx, rq, it)
}

// remoteArchiveChecks decides the checks of the archive a remote repository holds in its cache; one it does not hold is not judged.
func remoteArchiveChecks(ctx context.Context, rq *formats.Request, ns, name, ver string) error {
	it, found, err := rq.Host.Remote().Cached(ctx, rq, formats.Path{Path: ArtifactPath(ns, name, ver)})
	if err != nil || !found {
		return err
	}
	return rq.Host.Items().Check(ctx, rq, it)
}

// virtualArchiveChecks decides the checks of the archive the virtual repository would serve first from what its members store.
func (f *Format) virtualArchiveChecks(ctx context.Context, rq *formats.Request, ns, name, ver string) error {
	p := formats.Path{Path: ArtifactPath(ns, name, ver)}
	return rq.Host.Resolver().Check(ctx, rq, p, f.Classify(p))
}
