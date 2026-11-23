//go:build integration

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/blob/offline"
)

// The joins of package 7 (storage.md R-STOR-100) and package 6 (R-STOR-089) with the real backups of R-BAK-031's case: the check of
// the backups reads the retention listing these runs record, and the offline tool reads the pool they write.

// backupRefusals runs the dry run of the destruction of archive's keys against the installation's database and returns what the
// backups' check of R-STOR-100 refuses for (the holders backups and backup); the other holders (archive still holds copies and is not
// retired here) are not its subject.
func backupRefusals(t *testing.T, in *sealedInstall, r *rig) []storagekeys.HolderRefusal {
	t.Helper()
	d, err := storagekeys.Destroy(context.Background(), in.web, nil, storagekeys.DestroyOptions{
		Connection: "archive", Installation: "0b7c3f5e-0000-4000-8000-0000000000b7", StaleAfter: time.Minute,
		Backup: storagekeys.BackupTarget{Bucket: r.cfg.Target.Bucket, Prefix: r.cfg.Target.Prefix},
	}, nil)
	if err != nil && !errors.Is(err, storagekeys.ErrHeld) {
		t.Fatal(err)
	}
	var out []storagekeys.HolderRefusal
	for _, f := range d.Refusals {
		if f.Holder == storagekeys.HolderBackups || f.Holder == storagekeys.HolderBackup {
			out = append(out, f)
		}
	}
	return out
}

// offlinePool lists the backup's target with the offline tool as quader admin storage offline list does: a connection element in a file
// naming the target's bucket and prefix with its credential, the export of the storage keys as the command writes it, and the key ring's
// files. It returns the totals.
func offlinePool(t *testing.T, in *sealedInstall, r *rig) offline.Totals {
	t.Helper()
	ctx := context.Background()
	doc, err := storagekeys.Export(ctx, in.web, nil, "0b7c3f5e-0000-4000-8000-0000000000b7", "it-bak-pod", "", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	dir := t.TempDir()
	export, element := filepath.Join(dir, "export.json"), filepath.Join(dir, "target.yaml")
	tc := r.cfg.Target
	el := fmt.Sprintf("id: archive\nbucket: %s\nprefix: %s\nendpoint: %s\nregion: %s\nforce_path_style: %t\nca_file: %s\ncredentials_source: static\naccess_key_id_file: %s\nsecret_access_key_file: %s\nencrypted: true\n",
		tc.Bucket, tc.Prefix, tc.Endpoint, tc.Region, tc.ForcePathStyle, tc.CAFile, tc.Credentials.AccessKeyIDFile, tc.Credentials.SecretAccessKeyFile)
	for f, b := range map[string][]byte{export: raw, element: []byte(el)} {
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	objects, info, err := offline.OpenElement(ctx, element)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := offline.LoadKeys(export, in.ringDir)
	if err != nil {
		t.Fatal(err)
	}
	defer ks.Close()
	tot, err := offline.NewReader(offline.Bucket(objects, info), ks).List(ctx, func(offline.Result) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return tot
}
