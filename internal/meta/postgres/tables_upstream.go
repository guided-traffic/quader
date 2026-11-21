package postgres

import (
	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/meta"
)

// Registration of the table of 00086_upstream (R-REPO-066, R-BAK-004). upstream_repository is data: a restore brings back which
// repositories of the project upstream the product declared, which were switched on before and which are retired, with the
// repository rows it references. The project's own row is seeded by the migration; the load truncates it with the other rows of
// project and loads the backup's (R-BAK-021 step 4).
//
// registry_presets (00091_registry_presets, R-REPO-076) is data too: the installation's own registries, one row. Its version is the
// ETag a client holds, so a restore raises it by 2^32 like every client-held revision (R-BAK-022).
func init() {
	tables.Register("upstream_repository", tables.Data, tables.Options{})
	tables.Register("registry_presets", tables.Data, tables.Options{})
	meta.RegisterRevisionColumn("registry_presets", "version")
}
