package postgres

import (
	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/meta"
)

// Registration of the table of 00108_download_links (storage.md R-STOR-014): data, a decision of the administrator like every
// settings row. After a restore the journal switches the delivery off again when it was switched off after the restore point.
func init() {
	tables.Register("download_link_settings", tables.Data, tables.Options{})

	// version is the ETag of the settings (R-API-012); a restore raises it like every client-held revision (R-BAK-022), so a change
	// prepared against the state before the restore is refused with 412.
	meta.RegisterRevisionColumn("download_link_settings", "version")
}
