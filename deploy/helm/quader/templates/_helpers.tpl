{{/*
Name of the chart (app.kubernetes.io/name).
*/}}
{{- define "quader.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 50 | trimSuffix "-" -}}
{{- end -}}

{{/*
<fullname> (R-HA-028): the release name, or <release>-quader when the release name does not
contain the chart name; fullnameOverride wins. Never cut (Q-HA-374): a cut would change the names
without a word and could give two releases the same objects; quader.validateNames refuses a
<fullname> too long for the names the chart builds from it instead.
*/}}
{{- define "quader.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "quader.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "quader.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "quader.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* The ServiceAccount of the backup pods, the restore Job and the post-restore Job (R-HA-060, R-HA-025). Argument: dict "root" . "kind" "backup" | "restore" | "post-restore" "key" <the values key, kind unless "postRestore">. */}}
{{- define "quader.jobServiceAccountName" -}}
{{- $sa := (get .root.Values (.key | default .kind)).serviceAccount -}}
{{- if $sa.create -}}
{{- default (printf "%s-%s" (include "quader.fullname" .root) .kind) $sa.name -}}
{{- else -}}
{{- default "default" $sa.name -}}
{{- end -}}
{{- end -}}

{{/*
Selector labels of one component. Argument: dict "root" . "component" "api".
api, packages and worker pods carry quader/pool (R-HA-028): the empty value for the main
Deployments, so a pool never overlaps their selectors or the role Service's. Selector labels are
immutable, which is why the label has been there from the first release.
*/}}
{{- define "quader.selectorLabels" -}}
app.kubernetes.io/name: {{ include "quader.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- if has .component (list "api" "packages" "worker") }}
quader/pool: {{ .pool | default "" | quote }}
{{- end }}
{{- end -}}

{{/*
Labels on every object. Argument: dict "root" . "component" "api".
*/}}
{{- define "quader.labels" -}}
{{ include "quader.selectorLabels" . }}
app.kubernetes.io/version: {{ .root.Chart.AppVersion | replace "+" "_" | quote }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
helm.sh/chart: {{ include "quader.chart" .root }}
{{- with .root.Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* Annotations on every object. Argument: the root context. */}}
{{- define "quader.commonAnnotations" -}}
{{- with .Values.commonAnnotations }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* The image reference; a digest wins over the tag. */}}
{{- define "quader.image" -}}
{{- $registry := default .Values.image.registry .Values.global.imageRegistry -}}
{{- $repo := .Values.image.repository -}}
{{- $ref := ternary (printf "%s/%s" $registry $repo) $repo (ne $registry "") -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" $ref .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" $ref (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "quader.imagePullSecrets" -}}
{{- with .Values.global.imagePullSecrets }}
imagePullSecrets:
{{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{/*
Bytes of a quantity: an integer, or an integer with a binary unit (Ki, Mi, Gi, Ti, optionally
followed by B). Fails for anything else, so a typo in a size never becomes a wrong budget.
*/}}
{{- define "quader.bytes" -}}
{{- $s := "" -}}
{{- if kindIs "float64" . -}}
{{- $s = printf "%.0f" . -}}
{{- else -}}
{{- $s = toString . | trim -}}
{{- end -}}
{{- if not (regexMatch "^[0-9]+(Ki|Mi|Gi|Ti)?B?$" $s) -}}
{{- fail (printf "quader: %q is not a byte quantity; use an integer or a binary unit such as 512Mi, 4Gi or 2GiB" $s) -}}
{{- end -}}
{{- $n := regexFind "^[0-9]+" $s | int64 -}}
{{- $u := regexFind "[A-Za-z]+$" $s | trimSuffix "B" -}}
{{- $mult := dict "" 1 "Ki" 1024 "Mi" 1048576 "Gi" 1073741824 "Ti" 1099511627776 -}}
{{- mul $n (get $mult $u | int64) -}}
{{- end -}}

{{/*
Bytes of a Kubernetes memory quantity: an integer, a binary unit (Ki, Mi, Gi, Ti) or a decimal one
(k, M, G, T).
*/}}
{{- define "quader.quantityBytes" -}}
{{- $s := "" -}}
{{- if kindIs "float64" . -}}
{{- $s = printf "%.0f" . -}}
{{- else -}}
{{- $s = toString . | trim -}}
{{- end -}}
{{- if not (regexMatch "^[0-9]+(Ki|Mi|Gi|Ti|k|M|G|T)?$" $s) -}}
{{- fail (printf "quader: %q is not a memory quantity the chart can size the replica with; use an integer or a unit such as 512Mi, 2Gi or 2G" $s) -}}
{{- end -}}
{{- $n := regexFind "^[0-9]+" $s | int64 -}}
{{- $mult := dict "" 1 "Ki" 1024 "Mi" 1048576 "Gi" 1073741824 "Ti" 1099511627776 "k" 1000 "M" 1000000 "G" 1000000000 "T" 1000000000000 -}}
{{- mul $n (get $mult (regexFind "[A-Za-z]+$" $s) | int64) -}}
{{- end -}}

{{/*
What one request may hold, for the memory slots of R-ARCH-005 step 4 (Q-ARCH-240): the buffer of a
verified download (storage.download.buffer_size, at least twice hold_back and 4 KiB, rounded up to
a power of two), storage.s3.http.receive_window and 128 KiB. internal/app/memory.go computes the same.
*/}}
{{- define "quader.requestBytes" -}}
{{- $cfg := .Values.config -}}
{{- $buf := include "quader.bytes" (dig "storage" "download" "buffer_size" "256KiB" $cfg) | int64 -}}
{{- $hold := include "quader.bytes" (dig "storage" "download" "hold_back" "64KiB" $cfg) | int64 -}}
{{- $need := max $buf (mul 2 $hold) 4096 -}}
{{- $class := 4096 -}}
{{- range until 30 -}}
{{- if lt $class $need -}}
{{- $class = mul $class 2 -}}
{{- end -}}
{{- end -}}
{{- add $class (include "quader.bytes" (dig "storage" "s3" "http" "receive_window" "256KiB" $cfg) | int64) 131072 -}}
{{- end -}}

{{/*
What one request may make the kernel hold, for the kernel's share of R-ARCH-005 step 4 (Q-ARCH-500): twice
server.send_buffer, which the kernel doubles for its bookkeeping, or 4 MiB, the default most of
net.ipv4.tcp_wmem, when the buffer is left to autotuning. internal/app/memory.go computes the same.
*/}}
{{- define "quader.kernelRequestBytes" -}}
{{- $n := include "quader.bytes" (dig "server" "send_buffer" "128KiB" .Values.config) | int64 -}}
{{- if gt $n 0 -}}
{{- mul 2 $n -}}
{{- else -}}
{{- 4194304 -}}
{{- end -}}
{{- end -}}

{{/*
Seconds of a duration made of whole hours, minutes and seconds (1h30m, 90s, 5s).
*/}}
{{- define "quader.seconds" -}}
{{- $s := toString . | trim -}}
{{- if not (regexMatch "^([0-9]+h)?([0-9]+m)?([0-9]+s)?$" $s) -}}
{{- fail (printf "quader: %q is not a duration of whole hours, minutes and seconds (for example 5s or 1m30s)" $s) -}}
{{- end -}}
{{- $h := regexFind "[0-9]+h" $s | trimSuffix "h" | default "0" | int64 -}}
{{- $m := regexFind "[0-9]+m" $s | trimSuffix "m" | default "0" | int64 -}}
{{- $sec := regexFind "[0-9]+s" $s | trimSuffix "s" | default "0" | int64 -}}
{{- add (mul $h 3600) (mul $m 60) $sec -}}
{{- end -}}

{{/* The size of the spool volume of a role. Argument: dict "root" . "role" "packages", optionally "r" with
the effective values of a Deployment (a pool passes its own). */}}
{{- define "quader.spoolSize" -}}
{{- $v := .root.Values -}}
{{- $r := .r | default (index $v .role) -}}
{{- if eq $v.spool.type "ephemeral" -}}
{{- $v.spool.ephemeral.size -}}
{{- else if $r.spool.sizeLimit -}}
{{- $r.spool.sizeLimit -}}
{{- else -}}
{{- $v.spool.sizeLimit -}}
{{- end -}}
{{- end -}}

{{/*
storage.spool.max_bytes of a role: spool size minus headroom (R-HA-035).
*/}}
{{- define "quader.spoolBudget" -}}
{{- $size := include "quader.bytes" (include "quader.spoolSize" .) | int64 -}}
{{- $headroom := include "quader.bytes" .root.Values.spool.headroom | int64 -}}
{{- if le $size $headroom -}}
{{- fail (printf "quader: spool.headroom (%s) must be smaller than the spool size of %s (%s)" .root.Values.spool.headroom .role (include "quader.spoolSize" .)) -}}
{{- end -}}
{{- sub $size $headroom -}}
{{- end -}}

{{/*
The configuration file (R-HA-030): config merged with the keys the chart manages. The managed
keys win; setting one inside config is refused by quader.validate.
*/}}
{{- define "quader.config" -}}
{{- $s := .Values.secrets -}}
{{- $managed := dict -}}
{{- $_ := set $managed "storage" (dict "spool" (dict "dir" "/var/lib/quader/spool")) -}}
{{- $db := dict "username_file" "/var/run/secrets/quader/database/username" "password_file" "/var/run/secrets/quader/database/password" -}}
{{- if $s.databaseCA.existingSecret -}}
{{- $_ := set $db "ssl_root_cert_file" "/var/run/secrets/quader/database-ca/ca.crt" -}}
{{- end -}}
{{- if $s.databaseClientCert.existingSecret -}}
{{- $_ := set $db "ssl_cert_file" "/var/run/secrets/quader/database-client/tls.crt" -}}
{{- $_ := set $db "ssl_key_file" "/var/run/secrets/quader/database-client/tls.key" -}}
{{- end -}}
{{- $_ := set $managed "database" $db -}}
{{- $vk := dict -}}
{{- if $s.valkey.usernameKey -}}
{{- $_ := set $vk "username_file" "/var/run/secrets/quader/valkey/username" -}}
{{- end -}}
{{- $_ := set $vk "password_file" "/var/run/secrets/quader/valkey/password" -}}
{{- $sentinel := dict -}}
{{- if $s.valkeySentinel.existingSecret -}}
{{- if $s.valkeySentinel.usernameKey -}}
{{- $_ := set $sentinel "username_file" "/var/run/secrets/quader/valkey-sentinel/username" -}}
{{- end -}}
{{- $_ := set $sentinel "password_file" "/var/run/secrets/quader/valkey-sentinel/password" -}}
{{- end -}}
{{- $_ := set $vk "sentinel" $sentinel -}}
{{- $tls := dict -}}
{{- if $s.valkeyCA.existingSecret -}}
{{- $_ := set $tls "ca_file" "/var/run/secrets/quader/valkey-ca/ca.crt" -}}
{{- end -}}
{{- if $s.valkeyClientCert.existingSecret -}}
{{- $_ := set $tls "cert_file" "/var/run/secrets/quader/valkey-client/tls.crt" -}}
{{- $_ := set $tls "key_file" "/var/run/secrets/quader/valkey-client/tls.key" -}}
{{- end -}}
{{- $_ := set $vk "tls" $tls -}}
{{- $_ := set $managed "valkey" $vk -}}
{{- if eq (dig "storage" "s3" "credentials" "source" "" .Values.config) "static" -}}
{{- $_ := set (get $managed "storage") "s3" (dict "credentials" (dict "access_key_id_file" "/var/run/secrets/quader/s3/access-key-id" "secret_access_key_file" "/var/run/secrets/quader/s3/secret-access-key")) -}}
{{- end -}}
{{- if $s.s3CA.existingSecret -}}
{{- $st := get $managed "storage" -}}
{{- $s3 := get $st "s3" | default (dict) -}}
{{- $_ := set $s3 "ca_file" "/var/run/secrets/quader/s3-ca/ca.crt" -}}
{{- $_ := set $st "s3" $s3 -}}
{{- end -}}
{{- if eq (dig "metrics" "auth" "" .Values.config) "static" -}}
{{- $_ := set $managed "metrics" (dict "bearer_token_file" "/var/run/secrets/quader/metrics/token") -}}
{{- end -}}
{{- /*
The backup (R-HA-060, R-HA-030): where the target's credentials and CA are mounted in the backup and restore pods, and the
names the UI shows for a manual run (R-BAK-011). The files exist in those pods only; the path in the shared file is
the whole of what a replica sees of the target's credentials.
*/ -}}
{{- $bk := dict -}}
{{- $bkTarget := dict -}}
{{- if eq (dig "backup" "target" "credentials" "source" "default" .Values.config) "static" -}}
{{- $bkCreds := dict "access_key_id_file" "/var/run/secrets/quader/backup-target/access-key-id" "secret_access_key_file" "/var/run/secrets/quader/backup-target/secret-access-key" -}}
{{- if $s.backupTarget.sessionTokenKey -}}
{{- $_ := set $bkCreds "session_token_file" "/var/run/secrets/quader/backup-target/session-token" -}}
{{- end -}}
{{- $_ := set $bkTarget "credentials" $bkCreds -}}
{{- end -}}
{{- if $s.backupTargetCA.existingSecret -}}
{{- $_ := set $bkTarget "ca_file" "/var/run/secrets/quader/backup-target-ca/ca.crt" -}}
{{- end -}}
{{- if $bkTarget -}}
{{- $_ := set $bk "target" $bkTarget -}}
{{- end -}}
{{- if .Values.backup.enabled -}}
{{- $_ := set $bk "manual_run" (dict "namespace" .Release.Namespace "cronjob" (printf "%s-backup" (include "quader.fullname" .))) -}}
{{- end -}}
{{- if and .Values.backup.enabled (eq (dig "storage" "s3" "credentials" "source" "" .Values.config) "static") -}}
{{- $_ := set $bk "source" (dict "separate_credentials" true "credentials" (dict "source" "static" "access_key_id_file" "/var/run/secrets/quader/backup-source/access-key-id" "secret_access_key_file" "/var/run/secrets/quader/backup-source/secret-access-key")) -}}
{{- end -}}
{{- if $bk -}}
{{- $_ := set $managed "backup" $bk -}}
{{- end -}}
{{- /* Login (R-AUTH-004, R-AUTH-014): the paths of the api pods' mounts; without secrets.breakGlass the account is off. */ -}}
{{- $bg := dict "enabled" false -}}
{{- if $s.breakGlass.existingSecret -}}
{{- $bg = dict "password_file" "/var/run/secrets/quader/break-glass/password" -}}
{{- end -}}
{{- $providers := dict -}}
{{- range $s.oidc -}}
{{- $_ := set $providers .providerId (dict "client_secret_file" (printf "/var/run/secrets/quader/oidc-%s/client-secret" .providerId)) -}}
{{- end -}}
{{- $_ := set $managed "auth" (dict "break_glass" $bg "oidc" (dict "providers" $providers)) -}}
{{- $keys := $s.encryption.keys -}}
{{- if $keys -}}
{{- $files := list -}}
{{- range $keys -}}
{{- $files = append $files (printf "/var/run/secrets/quader/encryption/%s" .) -}}
{{- end -}}
{{- $_ := set $managed "secrets" (dict "encryption_key_files" $files "active_key_id" (first $keys | trimSuffix ".key")) -}}
{{- end -}}
{{- /*
TLS (R-HA-063): server.tls for the client listener, the pod's own kind's pair at the same path in every
pod, and, when the ops listener must not carry the client-certificate requirement, an explicit
server.ops_tls block with the same pair. Otherwise server.ops_tls keeps its default (inherit: true).
Probes and scrapers present no client certificate, which is why a client CA on :8080 never extends to :8081.
*/ -}}
{{- if .Values.tls.enabled -}}
{{- $t := .Values.tls -}}
{{- $block := dict "min_version" $t.minVersion "reload_interval" $t.reloadInterval -}}
{{- $main := list (dict "cert_file" "/var/run/secrets/quader/tls/tls.crt" "key_file" "/var/run/secrets/quader/tls/tls.key") -}}
{{- $srv := deepCopy $block -}}
{{- $_ := set $srv "certificates" $main -}}
{{- if $t.clientCA.secretName -}}
{{- $_ := set $srv "client_ca_file" "/var/run/secrets/quader/tls-client-ca/ca.crt" -}}
{{- $_ := set $srv "client_auth" $t.clientCA.auth -}}
{{- end -}}
{{- $server := dict "tls" $srv -}}
{{- if $t.clientCA.secretName -}}
{{- $ops := deepCopy $block -}}
{{- $_ := set $ops "inherit" false -}}
{{- $_ := set $ops "certificates" $main -}}
{{- $_ := set $server "ops_tls" $ops -}}
{{- end -}}
{{- $_ := set $managed "server" $server -}}
{{- end -}}
{{- /* Endpoints (R-EP-008): server.endpoints is chart-managed; the default endpoint's entry exists once there is a further endpoint or endpoints.default. */ -}}
{{- $eps := include "quader.endpointsConfig" . | fromYaml -}}
{{- if $eps -}}
{{- $srv := get $managed "server" | default (dict) -}}
{{- $_ := set $srv "endpoints" $eps -}}
{{- $_ := set $managed "server" $srv -}}
{{- end -}}
{{- /*
The trusted CA bundle (R-HA-043): its files are added to proxy.outbound.tls.ca_files after those the operator listed in config
(the system roots stay, use_system_roots is not touched), and with useForObjectStore the one file is storage.s3.ca_file.
*/ -}}
{{- $ca := .Values.trustedCA -}}
{{- if or $ca.existingSecret $ca.existingConfigMap -}}
{{- $caFiles := list -}}
{{- range (dig "proxy" "outbound" "tls" "ca_files" (list) .Values.config) -}}
{{- $caFiles = append $caFiles . -}}
{{- end -}}
{{- range $ca.keys -}}
{{- $caFiles = append $caFiles (printf "/etc/quader/ca/%s" .) -}}
{{- end -}}
{{- $_ := set $managed "proxy" (dict "outbound" (dict "tls" (dict "ca_files" $caFiles))) -}}
{{- if $ca.useForObjectStore -}}
{{- $st := get $managed "storage" -}}
{{- $s3 := get $st "s3" | default (dict) -}}
{{- $_ := set $s3 "ca_file" (printf "/etc/quader/ca/%s" (first $ca.keys)) -}}
{{- $_ := set $st "s3" $s3 -}}
{{- end -}}
{{- end -}}
{{- /* The links of both scannings (R-HA-066, R-HA-068, templates/_vuln.tpl and templates/_malware.tpl). */ -}}
{{- include "quader.scannerManagedConfig" (dict "root" . "managed" $managed) -}}
{{- /* The trust anchors declared in signatures.anchors (R-SIG-018, templates/_signatures.tpl). */ -}}
{{- include "quader.signaturesManagedConfig" (dict "root" . "managed" $managed) -}}
{{- $managed | mergeOverwrite (deepCopy .Values.config) | toYaml -}}
{{- end -}}
