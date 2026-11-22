{{/*
The object-store connections in the chart (R-HA-072, storage.md R-STOR-050). config.storage.connections reaches the binary as
the list it is, each element with its id filled in (an element without one is default) and the *_file keys the chart sets from
the entry of secrets.storageConnections with that id. Each field of an entry is a volume of its own, projected with items,
mode 0440 and without subPath, so a rotated Secret reaches the files (R-HA-030):

  entry field                          pods                                           mount path                                  keys set in the element
  existingSecret (credentials_source   api, packages, worker, restore, post-restore   /var/run/secrets/quader/s3-<id>/            access_key_id_file, secret_access_key_file,
  static)                                                                                                                         session_token_file (with sessionTokenKey)
  bucketKey, regionKey, endpointKey    the same and the backup pods                   /var/run/secrets/quader/s3-<id>-location/   bucket_file, region_file, endpoint_file
  ca                                   the same and the backup pods                   /var/run/secrets/quader/s3-<id>-ca/         ca_file
  backupSource                         the backup pods                                /var/run/secrets/quader/s3-<id>-backup/     the first connection's in backup.source (quader.backupSourceConfig)
  journalReader                        restore, post-restore (journalCredentials      /var/run/secrets/quader/s3-<id>-journal/    the journal credentials file (_backup.tpl)
                                       .source static)

trustedCA.useForObjectStore sets the ca_file of every element whose entry has no ca to the one file of trustedCA.keys (R-HA-043).
The migration Job and the scanner pods mount none of them: they read the shared file and touch no object store.
*/}}

{{/* The id of an element of config.storage.connections: its own, or default (storage.md R-STOR-050). Argument: the element. */}}
{{- define "quader.connectionId" -}}
{{- if kindIs "map" . -}}
{{- toString (get . "id" | default "default") -}}
{{- else -}}
default
{{- end -}}
{{- end -}}

{{/* The elements of config.storage.connections as a YAML list (empty when the value is not a list). Argument: the root context. */}}
{{- define "quader.connectionElements" -}}
{{- $l := dig "storage" "connections" (list) .Values.config -}}
{{- if kindIs "slice" $l -}}{{- toYaml $l -}}{{- else -}}[]{{- end -}}
{{- end -}}

{{/* The entry of secrets.storageConnections for a connection, as YAML ({} without one). Argument: dict "root" . "id" <id>. */}}
{{- define "quader.connectionEntry" -}}
{{- $out := dict -}}
{{- range .root.Values.secrets.storageConnections -}}
{{- if and (kindIs "map" .) (eq (toString .connectionId) $.id) -}}
{{- $out = . -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
The keys the chart sets in one element, as a YAML map from key to path (R-HA-072): what its entry provides, and the ca_file of
trustedCA.useForObjectStore. Argument: dict "root" . "element" <the element> "entry" <its entry>.
*/}}
{{- define "quader.connectionManagedKeys" -}}
{{- $el := .element -}}
{{- $e := .entry -}}
{{- $id := include "quader.connectionId" $el -}}
{{- $dir := printf "/var/run/secrets/quader/s3-%s" $id -}}
{{- $out := dict -}}
{{- if and $e.existingSecret (eq (toString (get $el "credentials_source")) "static") -}}
{{- $_ := set $out "access_key_id_file" (printf "%s/access-key-id" $dir) -}}
{{- $_ := set $out "secret_access_key_file" (printf "%s/secret-access-key" $dir) -}}
{{- if $e.sessionTokenKey -}}
{{- $_ := set $out "session_token_file" (printf "%s/session-token" $dir) -}}
{{- end -}}
{{- end -}}
{{- if $e.existingSecret -}}
{{- range $plain := list "bucket" "region" "endpoint" -}}
{{- if get $e (printf "%sKey" $plain) -}}
{{- $_ := set $out (printf "%s_file" $plain) (printf "%s-location/%s" $dir $plain) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $ca := .root.Values.trustedCA -}}
{{- if dig "ca" "existingSecret" "" $e -}}
{{- $_ := set $out "ca_file" (printf "%s-ca/ca.crt" $dir) -}}
{{- else if and $ca.useForObjectStore $ca.keys -}}
{{- $_ := set $out "ca_file" (printf "/etc/quader/ca/%s" (first $ca.keys)) -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
storage.connections of the configuration file: every element with its id and the keys the chart sets; a plain bucket, region or
endpoint that is empty is left out where its file takes its place, because the binary takes one of the two. Argument: the root
context.
*/}}
{{- define "quader.storageConnectionsConfig" -}}
{{- $out := list -}}
{{- range $el := include "quader.connectionElements" $ | fromYamlArray -}}
{{- $c := deepCopy $el -}}
{{- $id := include "quader.connectionId" $el -}}
{{- $_ := set $c "id" $id -}}
{{- $e := include "quader.connectionEntry" (dict "root" $ "id" $id) | fromYaml -}}
{{- $m := include "quader.connectionManagedKeys" (dict "root" $ "element" $el "entry" $e) | fromYaml -}}
{{- range $k, $p := $m -}}
{{- $_ := set $c $k $p -}}
{{- end -}}
{{- range $plain := list "bucket" "region" "endpoint" -}}
{{- if and (hasKey $m (printf "%s_file" $plain)) (hasKey $c $plain) (not (get $c $plain)) -}}
{{- $_ := unset $c $plain -}}
{{- end -}}
{{- end -}}
{{- $out = append $out $c -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
The Secret mounts of the connections in the shape of quader.secretMounts (templates/_secrets.tpl). Argument: dict "root" .
"scope" "runtime" | "migrate" | "backup" | "restore" | "post-restore".
*/}}
{{- define "quader.storageSecretMounts" -}}
{{- $root := .root -}}
{{- $scope := .scope -}}
{{- $writes := has $scope (list "runtime" "restore" "post-restore") -}}
{{- $reads := has $scope (list "runtime" "restore" "post-restore" "backup") -}}
{{- $jcSource := "runtime" -}}
{{- if eq $scope "post-restore" -}}{{- $jcSource = $root.Values.postRestore.journalCredentials.source -}}{{- end -}}
{{- if eq $scope "restore" -}}{{- $jcSource = $root.Values.restore.journalCredentials.source -}}{{- end -}}
{{- $out := list -}}
{{- range $el := include "quader.connectionElements" $root | fromYamlArray -}}
{{- $id := include "quader.connectionId" $el -}}
{{- $e := include "quader.connectionEntry" (dict "root" $root "id" $id) | fromYaml -}}
{{- $name := printf "s3-%s" $id -}}
{{- $dir := printf "/var/run/secrets/quader/%s" $name -}}
{{- if and $writes $e.existingSecret (eq (toString (get $el "credentials_source")) "static") -}}
{{- $items := list (dict "key" ($e.accessKeyIdKey | default "access-key-id") "path" "access-key-id") (dict "key" ($e.secretAccessKeyKey | default "secret-access-key") "path" "secret-access-key") -}}
{{- if $e.sessionTokenKey -}}
{{- $items = append $items (dict "key" $e.sessionTokenKey "path" "session-token") -}}
{{- end -}}
{{- $out = append $out (dict "name" $name "secret" $e.existingSecret "path" $dir "items" $items) -}}
{{- end -}}
{{- if and $reads $e.existingSecret -}}
{{- $items := list -}}
{{- range $plain := list "bucket" "region" "endpoint" -}}
{{- with get $e (printf "%sKey" $plain) -}}
{{- $items = append $items (dict "key" . "path" $plain) -}}
{{- end -}}
{{- end -}}
{{- if $items -}}
{{- $out = append $out (dict "name" (printf "%s-location" $name) "secret" $e.existingSecret "path" (printf "%s-location" $dir) "items" $items) -}}
{{- end -}}
{{- end -}}
{{- $ca := dig "ca" "existingSecret" "" $e -}}
{{- if and $reads $ca -}}
{{- $out = append $out (dict "name" (printf "%s-ca" $name) "secret" $ca "path" (printf "%s-ca" $dir) "items" (list (dict "key" (dig "ca" "key" "" $e | default "ca.crt") "path" "ca.crt"))) -}}
{{- end -}}
{{- /* The backup reads every connection and never writes or deletes there (R-BAK-015, R-BAK-030): the read-only login of the entry, never the runtime credential. */ -}}
{{- $bs := dig "backupSource" "existingSecret" "" $e -}}
{{- if and (eq $scope "backup") $bs -}}
{{- $out = append $out (dict "name" (printf "%s-backup" $name) "secret" $bs "path" (printf "%s-backup" $dir) "items" (list (dict "key" (dig "backupSource" "accessKeyIdKey" "" $e | default "access-key-id") "path" "access-key-id") (dict "key" (dig "backupSource" "secretAccessKeyKey" "" $e | default "secret-access-key") "path" "secret-access-key"))) -}}
{{- end -}}
{{- /* The login that lists object versions, for the journal replay of the restore and post-restore Jobs only (R-HA-023, R-HA-025 step 5); the runtime credential deliberately lacks it. */ -}}
{{- $jr := dig "journalReader" "existingSecret" "" $e -}}
{{- if and (has $scope (list "restore" "post-restore")) (eq $jcSource "static") $jr -}}
{{- $out = append $out (dict "name" (printf "%s-journal" $name) "secret" $jr "path" (printf "%s-journal" $dir) "items" (list (dict "key" (dig "journalReader" "accessKeyIdKey" "" $e | default "access-key-id") "path" "access-key-id") (dict "key" (dig "journalReader" "secretAccessKeyKey" "" $e | default "secret-access-key") "path" "secret-access-key"))) -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
backup.source of the configuration file, as YAML (nothing when the backup reads with the connection's own credential). The backup
reads the first connection of config.storage.connections today (internal/config FirstClient), so while backup.enabled and that
connection's entry has backupSource, separate_credentials names the files of its mount in the backup pods; the read login of every
other connection is mounted already and is named once the backup reads per connection (backup-and-restore.md R-BAK-030, Q-HA-396).
Without a backupSource the backup reads with the connection's own credential, the right one for a workload identity of the backup's
ServiceAccount. Argument: the root context.
*/}}
{{- define "quader.backupSourceConfig" -}}
{{- if .Values.backup.enabled -}}
{{- $id := include "quader.backupConnection" . -}}
{{- $e := include "quader.connectionEntry" (dict "root" . "id" $id) | fromYaml -}}
{{- if dig "backupSource" "existingSecret" "" $e -}}
{{- $dir := printf "/var/run/secrets/quader/s3-%s-backup" $id -}}
{{- toYaml (dict "separate_credentials" true "credentials" (dict "source" "static" "access_key_id_file" (printf "%s/access-key-id" $dir) "secret_access_key_file" (printf "%s/secret-access-key" $dir))) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
The id of the connection the backup reads and whose journal the replay of the restore and post-restore Jobs reads with a credentials
file, until both read every connection (Q-HA-396): the first element of config.storage.connections, as the binary takes it
(internal/config FirstClient). Argument: the root context.
*/}}
{{- define "quader.backupConnection" -}}
{{- include "quader.journalConnection" . -}}
{{- end -}}

{{/* The id of the connection whose journal reader the credentials file names: the first element. Argument: the root context. */}}
{{- define "quader.journalConnection" -}}
{{- include "quader.connectionId" (first (include "quader.connectionElements" . | fromYamlArray) | default (dict)) -}}
{{- end -}}

{{/*
A location as the binary compares two connections (storage.md R-STOR-050 rule 4): the normalised endpoint (an empty one is the
AWS endpoint of the region; rawEndpoint keeps it empty, as the binary compares a connection with the backup target), the bucket
and the prefix without surrounding slashes; empty when the bucket or the endpoint comes from a file, which only the binary reads. Argument: dict "element" <element> "entry" <its entry>.
*/}}
{{- define "quader.connectionLocation" -}}
{{- $el := .element -}}
{{- $e := .entry -}}
{{- $bucketFromFile := or (not (get $el "bucket")) (and $e.existingSecret $e.bucketKey) -}}
{{- $endpointFromFile := or (get $el "endpoint_file") (and $e.existingSecret $e.endpointKey) -}}
{{- $regionFromFile := or (get $el "region_file") (and $e.existingSecret $e.regionKey) -}}
{{- if not (or $bucketFromFile $endpointFromFile (and $regionFromFile (not (get $el "endpoint")))) -}}
{{- $raw := include "quader.normalizeEndpoint" (toString (get $el "endpoint" | default "")) -}}
{{- $ep := $raw -}}
{{- if not $ep -}}{{- $ep = printf "aws:%s" (toString (get $el "region" | default "")) -}}{{- end -}}
{{- toYaml (dict "endpoint" $ep "rawEndpoint" $raw "bucket" (toString (get $el "bucket")) "prefix" (trimAll "/" (toString (get $el "prefix" | default "quader")))) -}}
{{- end -}}
{{- end -}}

{{/* Whether two prefixes are equal or one contains the other ("true" or nothing). Argument: list <prefix> <prefix>. */}}
{{- define "quader.prefixesOverlap" -}}
{{- $a := index . 0 -}}
{{- $b := index . 1 -}}
{{- if or (eq $a "") (eq $b "") (eq $a $b) (hasPrefix (printf "%s/" $a) $b) (hasPrefix (printf "%s/" $b) $a) -}}true{{- end -}}
{{- end -}}

{{/*
The values of before (a single store) that an upgrade may still carry: each is refused with the keys that took its place, before
anything else is checked, so the operator reads the move and not a consequence of it. Argument: the root context.
*/}}
{{- define "quader.validateStorageMoved" -}}
{{- $v := .Values -}}
{{- if hasKey (dig "storage" (dict) $v.config) "s3" -}}
{{- fail "quader: config.storage.s3 is gone: the object store is the list config.storage.connections (storage.md R-STOR-050, R-HA-072). One store is one element: config.storage.connections: [{id: default, bucket: <bucket>, region: <region>, endpoint: <endpoint>, force_path_style: <true|false>, credentials_source: <default|webIdentity|static>}]; credentials.source became credentials_source, and the Secrets of the store are an entry of secrets.storageConnections with connectionId: default" -}}
{{- end -}}
{{- if hasKey $v.secrets "s3" -}}
{{- fail "quader: secrets.s3 is gone: the Secret of a connection is its entry of secrets.storageConnections (R-HA-072): secrets.storageConnections: [{connectionId: default, existingSecret: <Secret>, accessKeyIdKey: <key>, secretAccessKeyKey: <key>, bucketKey: <key>, regionKey: <key>, endpointKey: <key>}], where connectionId is the id of the element of config.storage.connections and every *Key keeps its meaning" -}}
{{- end -}}
{{- if hasKey $v.secrets "s3CA" -}}
{{- fail "quader: secrets.s3CA is gone: a connection's CA is the field ca of its entry of secrets.storageConnections (R-HA-072): secrets.storageConnections: [{connectionId: default, ca: {existingSecret: <Secret>, key: ca.crt}}]" -}}
{{- end -}}
{{- if hasKey $v.secrets "backupSource" -}}
{{- fail "quader: secrets.backupSource is gone: the backup's read login of a connection is the field backupSource of its entry of secrets.storageConnections (R-HA-072): secrets.storageConnections: [{connectionId: default, backupSource: {existingSecret: <Secret>, accessKeyIdKey: access-key-id, secretAccessKeyKey: secret-access-key}}]" -}}
{{- end -}}
{{- if hasKey $v.secrets "journalReader" -}}
{{- fail "quader: secrets.journalReader is gone: the journal reader of a connection is the field journalReader of its entry of secrets.storageConnections (R-HA-072): secrets.storageConnections: [{connectionId: default, journalReader: {existingSecret: <Secret>, accessKeyIdKey: access-key-id, secretAccessKeyKey: secret-access-key}}]" -}}
{{- end -}}
{{- end -}}

{{/* The refusals of R-HA-072 (R-HA-029): every failure names the value and the connection. Argument: the root context. */}}
{{- define "quader.validateStorage" -}}
{{- $v := .Values -}}
{{- $raw := dig "storage" "connections" (list) $v.config -}}
{{- if not (kindIs "slice" $raw) -}}
{{- fail "quader: config.storage.connections must be a list of connections (example: [{bucket: quader-blobs}]) (storage.md R-STOR-050)" -}}
{{- end -}}
{{- $els := include "quader.connectionElements" . | fromYamlArray -}}
{{- if not $els -}}
{{- fail "quader: config.storage.connections is empty: name at least one connection, an element with a bucket (example: [{bucket: quader-blobs}]) (storage.md R-STOR-050)" -}}
{{- end -}}
{{- if gt (len $els) 8 -}}
{{- fail (printf "quader: config.storage.connections has %d elements, at most 8 are allowed (storage.md R-STOR-050)" (len $els)) -}}
{{- end -}}

{{- /* The entries: each names one element, once. */ -}}
{{- $ids := list -}}
{{- range $el := $els -}}
{{- $ids = append $ids (include "quader.connectionId" $el) -}}
{{- end -}}
{{- $entrySeen := dict -}}
{{- range $i, $e := $v.secrets.storageConnections -}}
{{- $cid := toString $e.connectionId -}}
{{- if hasKey $entrySeen $cid -}}
{{- fail (printf "quader: secrets.storageConnections[%d] and [%v] are both entries of connectionId %q; a connection has one entry, which carries all of its Secrets" (get $entrySeen $cid) $i $cid) -}}
{{- end -}}
{{- $_ := set $entrySeen $cid $i -}}
{{- if not (has $cid $ids) -}}
{{- fail (printf "quader: secrets.storageConnections[%d].connectionId %q names no element of config.storage.connections (their ids: %s; an element without id is default)" $i $cid (join ", " $ids)) -}}
{{- end -}}
{{- if and (or $e.bucketKey $e.regionKey $e.endpointKey $e.sessionTokenKey) (not $e.existingSecret) -}}
{{- fail (printf "quader: secrets.storageConnections[%d] (connection %s): bucketKey, regionKey, endpointKey and sessionTokenKey read keys of the Secret existingSecret and need it (example: existingSecret: quader-s3)" $i $cid) -}}
{{- end -}}
{{- end -}}

{{- /* The elements. */ -}}
{{- $seen := dict -}}
{{- $writable := 0 -}}
{{- $locs := list -}}
{{- range $i, $el := $els -}}
{{- if not (kindIs "map" $el) -}}
{{- fail (printf "quader: config.storage.connections[%d] must be a map of the keys of storage.md R-STOR-050 (example: {bucket: quader-blobs})" $i) -}}
{{- end -}}
{{- $id := include "quader.connectionId" $el -}}
{{- $at := printf "config.storage.connections[%d] (%s)" $i $id -}}
{{- if not (regexMatch "^[a-z][a-z0-9-]{0,30}[a-z0-9]$" $id) -}}
{{- fail (printf "quader: config.storage.connections[%d].id %q must match ^[a-z][a-z0-9-]{0,30}[a-z0-9]$ (example: secondary); it names the connection for its whole life (storage.md R-STOR-050)" $i $id) -}}
{{- end -}}
{{- if hasKey $seen $id -}}
{{- fail (printf "quader: config.storage.connections[%v] and [%d] both have the id %q; an element without id is default, so at most one element may leave it out (storage.md R-STOR-050)" (get $seen $id) $i $id) -}}
{{- end -}}
{{- $_ := set $seen $id $i -}}
{{- $e := include "quader.connectionEntry" (dict "root" $ "id" $id) | fromYaml -}}
{{- $m := include "quader.connectionManagedKeys" (dict "root" $ "element" $el "entry" $e) | fromYaml -}}
{{- /* The keys the chart sets for this element, and the static keys' files, which only an entry provides (R-HA-030: secret file paths come from the secrets values). */ -}}
{{- range $k := concat (keys $m) (list "access_key_id_file" "secret_access_key_file" "session_token_file") | uniq | sortAlpha -}}
{{- if hasKey $el $k -}}
{{- fail (printf "quader: config.storage.connections[%d].%s (%s) is managed by the chart: it names the file of a Secret that the entry of secrets.storageConnections or trustedCA.useForObjectStore mounts; remove it" $i $k $id) -}}
{{- end -}}
{{- end -}}
{{- if and (not (get $el "bucket")) (not (hasKey $m "bucket_file")) (not (get $el "bucket_file")) -}}
{{- fail (printf "quader: %s has no bucket: set bucket (example: quader-blobs), or name the key of the bucket in a provisioner's Secret with the entry {connectionId: %s, existingSecret: <Secret>, bucketKey: <key>} of secrets.storageConnections (example: S3_BUCKET)" $at $id) -}}
{{- end -}}
{{- range $plain := list "bucket" "region" "endpoint" -}}
{{- if and (get $el $plain) (or (hasKey $m (printf "%s_file" $plain)) (get $el (printf "%s_file" $plain))) -}}
{{- fail (printf "quader: %s has %s %q and takes its %s from a file as well (%sKey of its entry of secrets.storageConnections, or %s_file): the value comes from one of them; remove one" $at $plain (toString (get $el $plain)) $plain $plain $plain) -}}
{{- end -}}
{{- end -}}
{{- $cs := toString (get $el "credentials_source" | default "default") -}}
{{- if and (eq $cs "static") (not $e.existingSecret) -}}
{{- fail (printf "quader: %s has credentials_source: static and no entry of secrets.storageConnections with existingSecret for it: add {connectionId: %s, existingSecret: <Secret>} with the keys access-key-id and secret-access-key (or name them with accessKeyIdKey and secretAccessKeyKey)" $at $id) -}}
{{- end -}}
{{- if and $e.existingSecret (ne $cs "static") (not (or $e.bucketKey $e.regionKey $e.endpointKey)) -}}
{{- fail (printf "quader: secrets.storageConnections names existingSecret %s for connection %s, but nothing reads it: %s has credentials_source %s, and the entry names no bucketKey, regionKey or endpointKey. Set credentials_source: static to use its keys, or remove existingSecret" $e.existingSecret $id $at $cs) -}}
{{- end -}}
{{- if eq (toString (get $el "encrypted")) "true" -}}
{{- fail (printf "quader: %s: encrypted: true is not supported yet: the encryption of stored content is not built (storage.md R-STOR-050)" $at) -}}
{{- end -}}
{{- if ne (toString (get $el "drain")) "true" -}}
{{- $writable = add1 $writable -}}
{{- end -}}
{{- with include "quader.connectionLocation" (dict "element" $el "entry" $e) | fromYaml -}}
{{- $loc := . -}}
{{- range $o := $locs -}}
{{- if and (eq $o.endpoint $loc.endpoint) (eq $o.bucket $loc.bucket) (include "quader.prefixesOverlap" (list $o.prefix $loc.prefix)) -}}
{{- $where := $loc.endpoint -}}
{{- if hasPrefix "aws:" $where -}}{{- $where = printf "the AWS endpoint of region %q" (trimPrefix "aws:" $where) -}}{{- end -}}
{{- fail (printf "quader: config.storage.connections[%v] (%s) and %s lie at one location: the bucket %s on %s with the prefixes %q and %q, which are equal or contain each other; a location belongs to one connection (storage.md R-STOR-050)" $o.index $o.id $at $loc.bucket $where $o.prefix $loc.prefix) -}}
{{- end -}}
{{- end -}}
{{- $locs = append $locs (merge (dict "index" $i "id" $id) $loc) -}}
{{- end -}}
{{- end -}}
{{- if eq $writable 0 -}}
{{- fail "quader: every element of config.storage.connections has drain: true; at least one connection must take the uploads (storage.md R-STOR-050, R-STOR-051)" -}}
{{- end -}}
{{- $minCopies := dig "storage" "replication" "min_copies" 1 $v.config | int -}}
{{- if or (lt $minCopies 1) (gt $minCopies $writable) -}}
{{- fail (printf "quader: config.storage.replication.min_copies (%d) must be between 1 and the number of connections without drain: true (%d): an upload is answered once that many connections hold it (storage.md R-STOR-053)" $minCopies $writable) -}}
{{- end -}}
{{- end -}}
