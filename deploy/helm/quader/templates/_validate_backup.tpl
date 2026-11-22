{{/*
Fail-fast validation of the backup CronJob and the restore Job (R-HA-029, R-HA-060). Every failure names the offending
value. Argument: the root context.
*/}}

{{/*
The reason a CronJob schedule is outside the dialect of quader's cron parser (internal/jobs/cron.go, Q-OPS-7), or nothing
when it is inside. Kubernetes accepts more (six fields, ?, @every, TZ=), and a schedule that one scheduler of quader
reads differently from another is the trap this closes; backup.timeZone is the value for a time zone. The parser's
further checks (a value outside its range, an expression that never fires within four years) are Kubernetes's at
apply time. Argument: the schedule.
*/}}
{{- define "quader.cronError" -}}
{{- $s := trim . -}}
{{- $macros := list "@yearly" "@annually" "@monthly" "@weekly" "@daily" "@midnight" "@hourly" -}}
{{- if eq $s "" -}}
it is empty
{{- else if or (hasPrefix "TZ=" $s) (hasPrefix "CRON_TZ=" $s) -}}
time-zone prefixes are not supported; set backup.timeZone
{{- else if or (eq $s "@every") (hasPrefix "@every " $s) (eq $s "@reboot") -}}
{{ first (splitList " " $s) }} is not supported
{{- else if hasPrefix "@" $s -}}
{{- if not (has (lower $s) $macros) -}}
unknown macro
{{- end -}}
{{- else -}}
{{- $parts := regexSplit "\\s+" $s -1 -}}
{{- if ne (len $parts) 5 -}}
{{- if or (eq (len $parts) 6) (eq (len $parts) 7) -}}
{{ len $parts }} fields: a seconds or year field is not supported
{{- else -}}
{{ len $parts }} fields, want 5
{{- end -}}
{{- else -}}
{{- range $i, $p := $parts -}}
{{- if regexMatch "[?#]" $p -}}
field {{ add $i 1 }} ({{ $p }}) uses ? or #, which are not supported
{{- else -}}
{{- range $item := splitList "," (upper $p) -}}
{{- if regexMatch "^([LW]|[0-9]+[LW])$" $item -}}
field {{ add $i 1 }} ({{ $p }}) uses L or W, which are not supported
{{- end -}}
{{- if eq $i 4 -}}
{{- range $v := splitList "-" (first (splitList "/" $item)) -}}
{{- if eq $v "7" -}}
the day of week 7 is not supported; Sunday is 0
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* An endpoint as the binary compares it: no scheme means https (config.NormalizeEndpoint). Argument: the endpoint. */}}
{{- define "quader.normalizeEndpoint" -}}
{{- if and . (not (contains "://" .)) -}}https://{{ . }}{{- else -}}{{ . }}{{- end -}}
{{- end -}}

{{- define "quader.validateBackup" -}}
{{- $v := .Values -}}
{{- $cfg := $v.config -}}
{{- $b := $v.backup -}}
{{- $r := $v.restore -}}
{{- $target := dig "backup" "target" (dict) $cfg -}}
{{- $bucket := dig "bucket" "" $target -}}
{{- $jrMissing := include "quader.journalReadersMissing" . -}}

{{- if and $b.enabled (not $bucket) -}}
{{- fail "quader: config.backup.target.bucket is required while backup.enabled is true: name a bucket of its own, in another account or project if you can (example: quader-backups), or switch the backup off with backup.enabled: false" -}}
{{- end -}}
{{- if and $r.enabled (not $bucket) -}}
{{- fail "quader: config.backup.target.bucket is required while restore.enabled is true: it is the target the backup is read from (example: quader-backups)" -}}
{{- end -}}

{{- if $b.enabled -}}
{{- /* The CronJob's name of at most 52 characters (its controller names a Job <CronJob>-<10 digits>, and the UI's manual run, Q-API-10, adds its own suffix to the same limit) is one row of quader.validateNames. */ -}}
{{- $cronErr := include "quader.cronError" $b.schedule -}}
{{- if $cronErr -}}
{{- fail (printf "quader: backup.schedule %q is not accepted: %s; schedules are five fields (minute hour day-of-month month day-of-week, day of week 0-6 with Sunday = 0) or one of @yearly @annually @monthly @weekly @daily @midnight @hourly, and backup.timeZone sets the zone" $b.schedule $cronErr) -}}
{{- end -}}
{{- end -}}

{{- /* The target must not be inside what the orphan sweep of any connection deletes (R-HA-060, R-BAK-002, R-STOR-026, storage.md
R-STOR-050 rule 5). The rule holds for any configured target, the backup on or off: the configuration check of every replica
applies it as well (internal/config), so the chart refuses what the replicas would refuse at start. A connection whose bucket or
endpoint comes from a provisioner's Secret is the binary's to compare. */ -}}
{{- if $bucket -}}
{{- $tEndpoint := include "quader.normalizeEndpoint" (dig "endpoint" "" $target) -}}
{{- $tp := trimAll "/" (dig "prefix" "quader" $target) -}}
{{- range $i, $el := include "quader.connectionElements" $ | fromYamlArray -}}
{{- $id := include "quader.connectionId" $el -}}
{{- $e := include "quader.connectionEntry" (dict "root" $ "id" $id) | fromYaml -}}
{{- with include "quader.connectionLocation" (dict "element" $el "entry" $e) | fromYaml -}}
{{- if and (eq .bucket $bucket) (eq .rawEndpoint $tEndpoint) -}}
{{- if not (dig "allow_same_bucket" false $target) -}}
{{- fail (printf "quader: config.backup.target.bucket (%s) is the bucket of config.storage.connections[%d] (%s) on the same endpoint: the orphan sweep of that connection deletes every object under its prefix that no row references, backups included. Use a bucket of its own, or accept it for a single-bucket evaluation setup with config.backup.target.allow_same_bucket: true and prefixes that do not contain each other" $bucket $i $id) -}}
{{- end -}}
{{- if include "quader.prefixesOverlap" (list $tp .prefix) -}}
{{- fail (printf "quader: config.backup.target.prefix (%q) and the prefix of config.storage.connections[%d] (%s) (%q) in the shared bucket %s must not contain each other: the orphan sweep of that connection would delete the backups" $tp $i $id .prefix $bucket) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- /* The target's keys are mounted into the backup and restore pods only, so they are needed while one of them is rendered. */ -}}
{{- if and (or $b.enabled $r.enabled) (eq (dig "credentials" "source" "default" $target) "static") (not $v.secrets.backupTarget.existingSecret) -}}
{{- fail "quader: config.backup.target.credentials.source: static needs secrets.backupTarget.existingSecret while backup.enabled or restore.enabled is true (keys secrets.backupTarget.accessKeyIdKey and secretAccessKeyKey)" -}}
{{- end -}}

{{- if and $r.enabled (not $r.backupId) -}}
{{- fail "quader: restore.backupId is required while restore.enabled is true (example: 20261028T020000Z-7f3a, an ID that quader backup list prints)" -}}
{{- end -}}

{{- /* Nothing may write to the installation, and no replica may serve it, while a restore runs or its clean-up has not (R-HA-029, R-HA-060, R-HA-025 step 1). */ -}}
{{- if or $r.enabled $v.postRestore.enabled $v.postRestore.hold -}}
{{- $by := ternary "restore.enabled" (ternary "postRestore.enabled" "postRestore.hold" $v.postRestore.enabled) $r.enabled -}}
{{- range $u := include "quader.units" . | fromYamlArray -}}
{{- if dig "autoscaling" "enabled" false $u.r -}}
{{- fail (printf "quader: %s needs %s.autoscaling.enabled: false (and %s.replicaCount: 0): no replica may run while the restore writes the installation or its clean-up is outstanding; the final helm upgrade without %s starts them" $by $u.key $u.key $by) -}}
{{- end -}}
{{- if gt (int $u.r.replicaCount) 0 -}}
{{- fail (printf "quader: %s needs %s.replicaCount: 0 (it is %v): no replica may run while the restore writes the installation or its clean-up is outstanding, because it would serve the restored database before the journal replay and the new keyspace; the final helm upgrade without %s starts them" $by $u.key $u.r.replicaCount $by) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- /* The post-restore Job after a platform restore (R-HA-025 step 4). */ -}}
{{- $p := $v.postRestore -}}
{{- if $p.enabled -}}
{{- if not (regexMatch "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$" $p.restoredTo) -}}
{{- fail (printf "quader: postRestore.restoredTo is required while postRestore.enabled is true and must be an RFC 3339 time (example: \"2026-10-28T09:15:00Z\"); it is %q" $p.restoredTo) -}}
{{- end -}}
{{- if $r.enabled -}}
{{- fail "quader: postRestore.enabled and restore.enabled are both set: the restore Job runs the clean-up of a platform restore itself (R-BAK-021), and post-restore is for a restore of PostgreSQL by the platform (R-HA-025). Set one" -}}
{{- end -}}
{{- if and (eq $p.journalCredentials.source "static") $jrMissing -}}
{{- fail (printf "quader: postRestore.journalCredentials.source: static needs a journalReader in the entry of secrets.storageConnections of every connection without drain: true, because the journal goes to each of them and the replay reads each (R-BAK-030, Q-HA-399); missing for: %s. Add {connectionId: <id>, journalReader: {existingSecret: <Secret with a login that may list its object versions>}}" $jrMissing) -}}
{{- end -}}
{{- if and $p.skipRevocationJournal (ne $p.journalCredentials.source "runtime") -}}
{{- fail "quader: postRestore.skipRevocationJournal skips the journal replay, so postRestore.journalCredentials.source must stay runtime" -}}
{{- end -}}
{{- end -}}

{{- /* The restore Job's journal reader (M3, Q-HA-159). */ -}}
{{- if $r.enabled -}}
{{- if and (eq $r.journalCredentials.source "static") $jrMissing -}}
{{- fail (printf "quader: restore.journalCredentials.source: static needs a journalReader in the entry of secrets.storageConnections of every connection without drain: true, because the journal goes to each of them and the replay reads each (R-BAK-030, Q-HA-399); missing for: %s. Add {connectionId: <id>, journalReader: {existingSecret: <Secret with a login that may list its object versions>}}" $jrMissing) -}}
{{- end -}}
{{- if and $r.skipRevocationJournal (ne $r.journalCredentials.source "runtime") -}}
{{- fail "quader: restore.skipRevocationJournal skips the journal replay, so restore.journalCredentials.source must stay runtime" -}}
{{- end -}}
{{- end -}}

{{- /* The backup reads every connection whose credential is static with a login of its own (M5, Q-HA-160, R-BAK-030, Q-BAK-64). */ -}}
{{- if $b.enabled -}}
{{- range $i, $el := include "quader.connectionElements" $ | fromYamlArray -}}
{{- $id := include "quader.connectionId" $el -}}
{{- if and (eq (toString (get $el "credentials_source")) "static") (not (dig "backupSource" "existingSecret" "" (include "quader.connectionEntry" (dict "root" $ "id" $id) | fromYaml))) -}}
{{- fail (printf "quader: backup.enabled with config.storage.connections[%d] (%s) credentials_source: static needs backupSource in its entry of secrets.storageConnections: the backup pod must not hold the runtime credential of the connection, which can write and delete there (R-BAK-015); add backupSource: {existingSecret: <Secret with a login that may only read it>} to the entry {connectionId: %s} (keys accessKeyIdKey and secretAccessKeyKey), or switch the backup off with backup.enabled: false" $i $id $id) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
