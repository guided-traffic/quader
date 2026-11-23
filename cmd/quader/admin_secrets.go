package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/listq"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/secrets"
)

const keyRingUsage = `usage: quader admin secrets status [--json] [--config <file>]

status  the key ring of the stored secrets (R-ARCH-021): per key ID whether it is active, whether this pod's ring holds it, the sealed
        values under it and its canary (ok, failed, absent); the key IDs of every live replica and whether the rings are compatible.
        It reads the configured key ring, the database and the replica registry, changes nothing, and prints key IDs and counts only,
        never key material, fingerprints, ciphertext or plaintext. The same report as GET /api/v1/system/encryption

exit codes: 0 done, 1 failed, 2 usage or a key ring that does not load, 75 temporary (the database could not be reached)`

// secretsCmd dispatches quader admin secrets <command>.
func secretsCmd(args []string, s streams) error {
	if len(args) >= 1 && args[0] == "status" {
		return secretsStatusCmd(args[1:], s)
	}
	return usageError{keyRingUsage}
}

// secretsStatusCmd is quader admin secrets status (R-ARCH-021): the report of internal/secrets (KeyRing.BuildReport), which
// GET /api/v1/system/encryption renders too, read with the pod's configured key ring.
func secretsStatusCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("admin secrets status", s)
	_, flags, names, err := parseBackup(c, args, 0)
	if err != nil {
		return backupExit(err)
	}
	env, err := openPostRestoreEnv(c, s, flags, names)
	if err != nil {
		return backupExit(err)
	}
	ring, err := secrets.Load(env.cfg.Secrets)
	if err != nil {
		return backupExit(backup.Fail(backup.ExitUsage, backup.CodeConfig, err))
	}
	ctx, stop := env.context()
	defer stop()
	db, err := postgres.Open(ctx, env.cfg.Database, env.instance, meta.RoleWeb, env.log)
	if err != nil {
		return backupExit(err)
	}
	defer db.Close()
	canaries := map[string]string{}
	var rings []secrets.ReplicaRing
	err = db.Read(ctx, func(c q.DBTX) error {
		rows, err := q.New(c).ListSecretKeyCanaries(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			canaries[row.KeyID] = row.Ciphertext
		}
		infos, err := cluster.ListReplicas(ctx, c, env.cfg.System)
		for _, i := range infos {
			role := ""
			if len(i.Roles) > 0 {
				role = i.Roles[0]
			}
			rings = append(rings, secrets.ReplicaRing{Name: i.Name, Role: role, Lost: i.Lost, ActiveKeyID: i.ActiveSecretKeyID, KeyIDs: i.SecretKeyIDs})
		}
		return err
	})
	if err != nil {
		return backupExit(err)
	}
	rep, err := ring.BuildReport(ctx, listq.SealedCounter{DB: db.Main()}, canaries, rings)
	if err != nil {
		return backupExit(err)
	}
	if *asJSON {
		return env.printJSON(rep)
	}
	return writeSecretsStatus(s.stdout, rep)
}

// writeSecretsStatus prints the report as three blocks: the keys, the replicas, the compatibility.
func writeSecretsStatus(out io.Writer, rep secrets.Report) error {
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "active key\t%s\n\n", rep.ActiveKeyID)
	fmt.Fprintln(tw, "KEY ID\tACTIVE\tIN THIS RING\tSEALED VALUES\tCANARY")
	for _, k := range rep.Keys {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k.ID, yes(k.Active), yes(k.Loaded), strconv.FormatInt(k.SealedValues, 10), k.Canary)
	}
	fmt.Fprintln(tw)
	if len(rep.Replicas) == 0 {
		fmt.Fprintln(tw, "no live replica in the registry")
	} else {
		fmt.Fprintln(tw, "REPLICA\tROLE\tACTIVE KEY\tKEY IDS")
		for _, r := range rep.Replicas {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Role, r.ActiveKeyID, strings.Join(r.KeyIDs, ", "))
		}
	}
	fmt.Fprintf(tw, "\nrings compatible\t%s\n", yes(rep.RingsCompatible))
	return tw.Flush()
}
