package oci

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/meta"
)

// RecordPublished writes package.published of a version an image name was given (a new tag, or the first manifest pushed by its
// digest), with the guards of a first version (audit.PublishGuards: the auto claim's outcome, an undetermined shadowing check), and
// repository.remote_package_shadowed when the version shadows a remote member's name on the strength of manage (R-REPO-048), in the
// transaction of the push.
func (s *Store) RecordPublished(ctx context.Context, tx meta.Tx, in rows.Published) error {
	details := map[string]any{
		"package": in.Name, "version": in.Version,
		"files": []map[string]any{{"path": in.Path, "sha256": hex.EncodeToString(in.SHA256[:]), "size": in.Size}},
		// The image is the unit its files' events name, so that the audit log shows the push as one row (R-OPS-056).
		"format": "oci", "unit": in.Name,
	}
	audit.PublishGuards(details, in.Claim, in.ShadowUndetermined)
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if err := s.o.Recorder.RecordTx(ctx, tx, s.event(ctx, audit.EventPackagePublished, in.RepoKey, in.Name, in.Actor, raw, nil)); err != nil {
		return err
	}
	if len(in.Shadowed) == 0 {
		return nil
	}
	exposure := make([]map[string]string, len(in.Shadowed))
	for i, p := range in.Shadowed {
		exposure[i] = map[string]string{"virtual": p.Virtual, "remote": p.Remote}
	}
	raw, err = json.Marshal(map[string]any{"package": in.Name, "exposure": exposure})
	if err != nil {
		return err
	}
	return s.o.Recorder.RecordTx(ctx, tx, s.event(ctx, audit.EventRepositoryRemotePackageShadowed, in.RepoKey, in.Name, in.Actor, raw, nil))
}
