package postgres

import "github.com/guided-traffic/quader/internal/backup/tables"

// Registration of the tables of 00139_package_dependencies (api-and-ui.md R-API-086, R-BAK-004):
//
//   - package_dependencies, package_dependency_reads: transient. Both are derived from the formats' stored rows (npm_versions,
//     nuget_version, deb_binaries, ansible_collection_versions); a restored installation has none, and the task remote-describe reads
//     the stored rows again from its first run after the restore (the worker's start queues it).
func init() {
	tables.Register("package_dependencies", tables.Transient, tables.Options{})
	tables.Register("package_dependency_reads", tables.Transient, tables.Options{})
}
