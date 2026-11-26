package deb

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/meta"
)

// RecordPublished writes package.published of a package file an upload stored, with the guards of a first version
// (audit.PublishGuards: the auto claim's outcome, an undetermined shadowing check), and repository.remote_package_shadowed when the
// upload shadows a remote member's name on the strength of manage (R-REPO-048), in the transaction of the upload.
func (s journalStore) RecordPublished(ctx context.Context, tx meta.Tx, in rows.Published) error {
	details := map[string]any{
		"package": in.Package, "version": in.Version,
		"files":  []map[string]any{{"path": in.Path, "sha256": hex.EncodeToString(in.SHA256[:]), "size": in.Size}},
		"format": "deb",
	}
	if in.Unit != "" {
		details["unit"] = in.Unit
	}
	audit.PublishGuards(details, in.Claim, in.ShadowUndetermined)
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if err := s.o.Recorder.RecordTx(ctx, tx, s.event(ctx, audit.EventPackagePublished, in.RepoKey, in.Path, in.Actor, raw, nil)); err != nil {
		return err
	}
	if len(in.Shadowed) == 0 {
		return nil
	}
	exposure := make([]map[string]string, len(in.Shadowed))
	for i, p := range in.Shadowed {
		exposure[i] = map[string]string{"virtual": p.Virtual, "remote": p.Remote}
	}
	raw, err = json.Marshal(map[string]any{"package": in.Package, "exposure": exposure})
	if err != nil {
		return err
	}
	return s.o.Recorder.RecordTx(ctx, tx, s.event(ctx, audit.EventRepositoryRemotePackageShadowed, in.RepoKey, in.Path, in.Actor, raw, nil))
}
