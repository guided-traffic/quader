package main

// The packages that own tables register them with internal/backup/tables in
// their init functions (R-BAK-004). `quader migrate` checks the migrated schema
// against that registry, so the command links every owner whether or not its
// own code path reaches them: a table nobody registered would be missing from
// every backup.
import (
	_ "github.com/guided-traffic/quader/internal/blob"
	_ "github.com/guided-traffic/quader/internal/cluster"
	_ "github.com/guided-traffic/quader/internal/formatdata/ansible"
	_ "github.com/guided-traffic/quader/internal/formatdata/clamav"
	_ "github.com/guided-traffic/quader/internal/formatdata/deb"
	_ "github.com/guided-traffic/quader/internal/formatdata/nuget"
	_ "github.com/guided-traffic/quader/internal/formatdata/oci"
	_ "github.com/guided-traffic/quader/internal/jobs"
)
