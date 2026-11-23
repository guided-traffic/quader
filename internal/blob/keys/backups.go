package keys

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// The backups' part of the destruction (storage.md R-STOR-100, Q-STOR-403): the command reads no pool and holds no credential of the
// backup's target; it judges the backups by the newest retention listing in backup_run.result and the key identifiers each retained
// backup recorded (backup-and-restore.md R-BAK-032: retained[].poolKeyIds and sealedKeyIds). Until the backup records them, every
// retained backup gives null, which counts as unknown and refuses: the check fails closed.

// listing is the part of a run's result the check reads.
type listing struct {
	Retention *struct {
		KeepLast          int   `json:"keepLast"`
		KeepWithinSeconds int64 `json:"keepWithinSeconds"`
	} `json:"retention"`
	Retained []retainedBackup `json:"retained"`
}

// retainedBackup is one entry of retained[]. A null or missing poolKeyIds or sealedKeyIds is unknown.
type retainedBackup struct {
	BackupID     string    `json:"backupId"`
	SnapshotAt   time.Time `json:"snapshotAt"`
	PoolKeyIDs   *[]string `json:"poolKeyIds"`
	SealedKeyIDs *[]string `json:"sealedKeyIds"`
}

// normalTarget is a run's target as s3://<bucket>/<prefix>: a create records "s3://<bucket>/<prefix> (<endpoint>)" and a delete or a
// prune "s3://<bucket>/<prefix>", so the check compares bucket and prefix (Q-STOR-461).
func normalTarget(s string) string {
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	rest, ok := strings.CutPrefix(s, "s3://")
	if !ok {
		return s
	}
	bucket, prefix, _ := strings.Cut(rest, "/")
	return BackupTarget{Bucket: bucket, Prefix: prefix}.String()
}

// judgeBackups runs the check of R-STOR-100 for the keys live and records what the retained backups hold sealed.
func (d *Destruction) judgeBackups(h Holders, live []string, target BackupTarget, now time.Time) {
	if target.Bucket == "" && h.BackupRuns == 0 {
		return // no backup known: the report says so
	}
	held := &d.Remaining.Backups
	held.Recorded = true
	if h.BackupActive {
		d.refuse(HolderBackups, 1, "a backup run that writes the target is active; run the destruction again when it has finished")
	}
	if h.Listing == nil {
		d.refuse(HolderBackups, 0, "no finished backup run is recorded in this database, so no retention listing says which backups are retained (after a restore of a backup the record is empty until the next run); run quader backup prune, which lists the target and records it")
		return
	}
	l := h.Listing
	held.RunID, held.ListedAt, held.Target = l.RunID, timePtr(l.FinishedAt), normalTarget(l.Target)
	var res listing
	if err := json.Unmarshal(l.Result, &res); err != nil || res.Retention == nil {
		d.refuse(HolderBackups, 0, fmt.Sprintf("the newest finished backup run (%s, finished %s) recorded no retention listing (a create with --no-prune, or a run that ended before it listed the target); run quader backup prune",
			l.RunID, l.FinishedAt.UTC().Format(time.RFC3339)))
		return
	}
	if target.Bucket == "" || held.Target != target.String() {
		configured := "no backup target"
		if target.Bucket != "" {
			configured = target.String()
		}
		d.refuse(HolderBackups, 0, fmt.Sprintf("the newest retention listing is of the target %s, and this configuration names %s: backups in another target are not seen (H-1373); run the destruction with the configuration of the target whose backups may hold this connection's objects, after quader backup prune there",
			held.Target, configured))
		return
	}
	retained := slices.Clone(res.Retained)
	sort.SliceStable(retained, func(i, j int) bool { return retained[i].SnapshotAt.After(retained[j].SnapshotAt) })
	keepLast, keepWithin := max(res.Retention.KeepLast, 1), time.Duration(res.Retention.KeepWithinSeconds)*time.Second
	var sealed int64
	sealedKnown := true
	var leave time.Time
	for p, b := range retained {
		when := leavesRetention(p, keepLast, keepWithin, b.SnapshotAt, now)
		switch named := intersect(b.PoolKeyIDs, live); {
		case b.PoolKeyIDs == nil:
			d.refuse(HolderBackup, 1, fmt.Sprintf("backup %s (snapshot %s) records no pool keys (poolKeyIds; its manifest has no storageKeys), so whether its pool holds objects under connection %s's keys is unknown; %s, or delete it: quader backup delete %s",
				b.BackupID, b.SnapshotAt.UTC().Format(time.RFC3339), d.Connection, when, b.BackupID))
		case len(named) > 0:
			d.refuse(HolderBackup, int64(len(named)), fmt.Sprintf("backup %s (snapshot %s) holds pool objects under %s; %s, or delete it: quader backup delete %s",
				b.BackupID, b.SnapshotAt.UTC().Format(time.RFC3339), strings.Join(named, ", "), when, b.BackupID))
		}
		switch {
		case b.SealedKeyIDs == nil:
			sealedKnown = false
		case len(intersect(b.SealedKeyIDs, live)) > 0:
			sealed++
			if keepWithin > 0 && b.SnapshotAt.Add(keepWithin).After(leave) {
				leave = b.SnapshotAt.Add(keepWithin)
			}
		}
	}
	if sealedKnown {
		held.Count = &sealed
		held.LeaveNotBefore = timePtr(leave)
	}
}

// intersect is the identifiers of ids that list names, in the order of ids.
func intersect(list *[]string, ids []string) []string {
	if list == nil {
		return nil
	}
	var out []string
	for _, id := range ids {
		if slices.Contains(*list, id) {
			out = append(out, id)
		}
	}
	return out
}

// leavesRetention says when the retained backup at position p (0 the newest) leaves retention at the earliest: a backup is deleted
// once it is not among the keep_last newest and older than keep_within (backup-and-restore.md R-BAK-016).
func leavesRetention(p, keepLast int, keepWithin time.Duration, snapshot, now time.Time) string {
	var parts []string
	if keepWithin > 0 && snapshot.Add(keepWithin).After(now) {
		parts = append(parts, "not before "+snapshot.Add(keepWithin).UTC().Format(time.RFC3339))
	}
	if n := keepLast - p; n > 0 {
		parts = append(parts, fmt.Sprintf("once %d newer backups exist (keep_last %d)", n, keepLast))
	}
	if len(parts) == 0 {
		return "it leaves retention at the next prune"
	}
	return "it leaves retention " + strings.Join(parts, " and ")
}
