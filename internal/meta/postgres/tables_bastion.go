package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registrations of the tables of 00032_bastion (R-BAS-004, R-BAK-004), so that the migration test passes
// and every table carries its export class from its migration on.
//
//   - bastion: data. A restore brings back the registrations with their principals and tokens.
//   - bastion_contact: transient. The last contact of a replica says what the replica reported at its last
//     handshake; a restored installation shows none until the Bastions handshake again (R-BAS-010), and a
//     copy of the old ones would show replicas that are gone.
//   - bastion_catalogue, bastion_catalogue_document, bastion_catalogue_state, bastion_catalogue_unit (00146): transient. The public
//     catalogue is derived from the packages, findings and verdicts the backup carries; a restored installation builds it again, and
//     its new state starts the change list above every number handed out before, so a Bastion's cursor answers reset (R-BAS-101,
//     R-BAS-108).
//   - bastion_status (00147): transient. The state each replica reported with its last batch, with the addresses it blocks now
//     (R-LIM-045); the next batch writes it again, and a restore must not bring back addresses of blocks that ended (Q-BAS-443).
//   - bastion_project, repository_bastion (00159): data. What a Bastion may distribute (R-BAS-134): the projects an administrator
//     allowed and the repositories their Owners chose. A restore brings them back as they were at its point; the journaled removals
//     after it are replayed (Q-BAS-521), so a restore never widens what a Bastion distributes.
//
// The registration has no revision column of its own: principal.revision is its ETag (00013).
func init() {
	tables.Register("bastion", tables.Data, tables.Options{})
	tables.Register("bastion_contact", tables.Transient, tables.Options{})
	for _, t := range []string{"bastion_catalogue", "bastion_catalogue_document", "bastion_catalogue_state", "bastion_catalogue_unit"} {
		tables.Register(t, tables.Transient, tables.Options{})
	}
	tables.Register("bastion_status", tables.Transient, tables.Options{})
	tables.Register("bastion_project", tables.Data, tables.Options{})
	tables.Register("repository_bastion", tables.Data, tables.Options{})
}
