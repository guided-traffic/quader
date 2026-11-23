package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/verify"
	"github.com/guided-traffic/quader/internal/buildinfo"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// backupVerifyCmd is quader backup verify (R-BAK-019). Owned by the target work package: it replaces newVerifier.
func backupVerifyCmd(args []string, s streams) error {
	c, asJSON := newBackupFlags("verify", s)
	latest := c.fs.Bool("latest", false, "verify the newest complete backup")
	all := c.fs.Bool("all", false, "verify every complete backup")
	deep := c.fs.Bool("deep", false, "download every pool object and recompute its SHA-256")
	bps := c.fs.Int64("bytes-per-second", 0, "throttle the downloads of --deep; 0 is unlimited")
	ring := c.fs.String("key-ring", "", "with --deep: a directory of the key ring's *.key files, which opens the storage keys the backup holds, so that every encrypted pool object is read and authenticated (R-BAK-031)")
	pos, flags, names, err := parseBackup(c, args, 1)
	if err != nil {
		return err
	}
	selected := 0
	for _, b := range []bool{*latest, *all, len(pos) == 1} {
		if b {
			selected++
		}
	}
	if selected != 1 || *bps < 0 {
		return usageError{backupUsage}
	}
	opts := backup.VerifyOptions{Latest: *latest, All: *all, Deep: *deep, BytesPerSecond: *bps}
	if *ring != "" {
		if !*deep {
			return usageError{backupUsage}
		}
		r, err := keyRingDir(*ring)
		if err != nil {
			return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
		}
		opts.KeyRing = r
	}
	if len(pos) == 1 {
		id, err := format.ParseID(pos[0])
		if err != nil {
			return usageError{err.Error()}
		}
		opts.ID = id
	}
	env, err := openBackupEnv(c, s, flags, names)
	if err != nil {
		return err
	}
	env.json = *asJSON
	verifier, err := newVerifier(env)
	if err != nil {
		return err
	}
	ctx, stop := env.context()
	defer stop()
	report, err := verifier.Verify(ctx, opts)
	if report != nil {
		if env.json {
			if jerr := env.printJSON(report); jerr != nil {
				return jerr
			}
		} else {
			for _, f := range report.Findings {
				if _, perr := fmt.Fprintf(s.stdout, "%s %s\n", f.Kind, f.Object); perr != nil {
					return perr
				}
			}
			if report.NotAuthenticated > 0 {
				if _, perr := fmt.Fprintf(s.stdout, "not_authenticated %d encrypted pool objects: checked by their records, not read; --key-ring reads them\n", report.NotAuthenticated); perr != nil {
					return perr
				}
			}
		}
	}
	return err
}

// newVerifier builds the Verifier. The run is recorded when the database can be reached (R-BAK-019); a database that cannot
// be opened does not stop the verification.
func newVerifier(env *backupEnv) (backup.Verifier, error) {
	ctx, stop := env.context()
	defer stop()
	t, layout, err := openTarget(ctx, env)
	if err != nil {
		return nil, err
	}
	v := &verify.Verifier{
		Store: t, Layout: layout, Timing: backupTiming(env), Target: targetURL(env), Pod: env.instance,
		QuaderVersion: buildinfo.Get().Version, NewRunID: principal.NewID, Log: env.log,
	}
	if db, err := openRunDB(ctx, env); err != nil {
		env.log.Warn("the verification is not recorded: the database is not reachable", "err", err)
	} else {
		v.Runs = backup.NewPGStore(db)
	}
	return v, nil
}

// keyRingDir loads the key ring of the *.key files of dir (R-ARCH-021's format) for opening only: the key a ring seals with is one
// of them, which nothing here seals with.
func keyRingDir(dir string) (*secrets.KeyRing, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.key"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("--key-ring %s holds no *.key file", dir)
	}
	sort.Strings(files)
	return secrets.Load(config.Secrets{EncryptionKeyFiles: files, ActiveKeyID: strings.TrimSuffix(filepath.Base(files[0]), ".key")})
}
