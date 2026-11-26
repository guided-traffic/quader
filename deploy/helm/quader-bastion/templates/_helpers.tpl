{{/*
Name of the chart (app.kubernetes.io/name).
*/}}
{{- define "quader-bastion.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 50 | trimSuffix "-" -}}
{{- end -}}

{{/*
<fullname> (R-BAS-045): the release name, or <release>-quader-bastion when the release name does not
contain the chart name; fullnameOverride wins. Cut at 50 characters so that <fullname>-config,
<fullname>-ops and <fullname>-tls stay valid DNS labels.
*/}}
{{- define "quader-bastion.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 50 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 50 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 50 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "quader-bastion.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "quader-bastion.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "quader-bastion.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* The selector labels of the Bastion pods. Selector labels are immutable. */}}
{{- define "quader-bastion.selectorLabels" -}}
app.kubernetes.io/name: {{ include "quader-bastion.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: bastion
{{- end -}}

{{/* Labels on every object. Argument: dict "root" . "component" "bastion". */}}
{{- define "quader-bastion.labels" -}}
app.kubernetes.io/name: {{ include "quader-bastion.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
app.kubernetes.io/version: {{ .root.Chart.AppVersion | replace "+" "_" | quote }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
helm.sh/chart: {{ include "quader-bastion.chart" .root }}
{{- with .root.Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* Annotations on every object. Argument: the root context. */}}
{{- define "quader-bastion.commonAnnotations" -}}
{{- with .Values.commonAnnotations }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* The image reference; a digest wins over the tag. */}}
{{- define "quader-bastion.image" -}}
{{- $registry := default .Values.image.registry .Values.global.imageRegistry -}}
{{- $repo := .Values.image.repository -}}
{{- $ref := ternary (printf "%s/%s" $registry $repo) $repo (ne $registry "") -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" $ref .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" $ref (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "quader-bastion.imagePullSecrets" -}}
{{- with .Values.global.imagePullSecrets }}
imagePullSecrets:
{{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{/* The port of a listen address such as ":8443" or "0.0.0.0:8443". */}}
{{- define "quader-bastion.port" -}}
{{- regexFind "[0-9]+$" (toString .) -}}
{{- end -}}

{{/* The workload kind: a StatefulSet with a persistent cache volume (R-BAS-030), a Deployment otherwise. */}}
{{- define "quader-bastion.workloadKind" -}}
{{- if and (eq (dig "bastion" "cache" "backend" "volume" .Values.config) "volume") (eq .Values.cacheVolume.type "persistent") -}}
StatefulSet
{{- else -}}
Deployment
{{- end -}}
{{- end -}}

{{/*
quader-bastion.bytes: a size as a number of bytes. It reads what the role's byte sizes and a Kubernetes quantity read:
a plain number, KiB MiB GiB TiB, KB MB GB TB, B, and Ki Mi Gi Ti, K M G T. Anything else fails the render.
*/}}
{{- define "quader-bastion.bytes" -}}
{{- $s := toString (kindIs "float64" . | ternary (int64 .) .) | trim -}}
{{- $n := regexFind "^[0-9]+" $s -}}
{{- $u := regexReplaceAll "^[0-9]+" $s "" | trim -}}
{{- $units := dict "" 1 "B" 1 "Ki" 1024 "KiB" 1024 "Mi" 1048576 "MiB" 1048576 "Gi" 1073741824 "GiB" 1073741824 "Ti" 1099511627776 "TiB" 1099511627776 "K" 1000 "KB" 1000 "M" 1000000 "MB" 1000000 "G" 1000000000 "GB" 1000000000 "T" 1000000000000 "TB" 1000000000000 -}}
{{- if or (not $n) (not (hasKey $units $u)) -}}
{{- fail (printf "quader-bastion: %q is not a size (examples: 2GiB, 512Mi, 1073741824)" $s) -}}
{{- end -}}
{{- mul (int64 $n) (index $units $u) -}}
{{- end -}}

{{/* The room next to the spool in the tmp volume: /tmp holds a little more than fills (Q-BAS-131). */}}
{{- define "quader-bastion.tmpHeadroomBytes" -}}268435456{{- end -}}

{{/* The spool of fills in bytes: config.bastion.cache.spool_max_bytes, 2GiB as the role's default. */}}
{{- define "quader-bastion.spoolBytes" -}}
{{- include "quader-bastion.bytes" (dig "bastion" "cache" "spool_max_bytes" "2GiB" .Values.config) -}}
{{- end -}}

{{/*
quader-bastion.spoolInTmp: do the fills go to the pod's tmp volume? Renders "true" or nothing. The bucket backend spools
there; the volume backend does once secrets.cacheKey is set, so that the cache volume holds no plaintext (Q-BAS-214).
*/}}
{{- define "quader-bastion.spoolInTmp" -}}
{{- if or (include "quader-bastion.bucketOn" .) .Values.secrets.cacheKey.existingSecret -}}
true
{{- end -}}
{{- end -}}

{{/*
quader-bastion.tmpSizeLimit: the sizeLimit of the tmp emptyDir (Q-BAS-131). A set tmp.sizeLimit is used as written; the
default is 1Gi where the fills are spooled on the cache volume and, where they are spooled into tmp (the bucket backend, or
the volume backend with secrets.cacheKey), the spool plus the headroom, in whole MiB.
*/}}
{{- define "quader-bastion.tmpSizeLimit" -}}
{{- if .Values.tmp.sizeLimit -}}
{{- .Values.tmp.sizeLimit -}}
{{- else if include "quader-bastion.spoolInTmp" . -}}
{{- $total := add (include "quader-bastion.spoolBytes" . | int64) (include "quader-bastion.tmpHeadroomBytes" . | int64) -}}
{{- printf "%dMi" (div (add $total 1048575) 1048576) -}}
{{- else -}}
1Gi
{{- end -}}
{{- end -}}

{{/* bastion.replicas (R-BAS-025): replicaCount, or autoscaling.maxReplicas with autoscaling. */}}
{{- define "quader-bastion.replicas" -}}
{{- if .Values.autoscaling.enabled -}}
{{- .Values.autoscaling.maxReplicas -}}
{{- else -}}
{{- .Values.replicaCount -}}
{{- end -}}
{{- end -}}

{{/* The Secret of the listener's key pair: tls.existingSecret, or the Certificate's <fullname>-tls. */}}
{{- define "quader-bastion.tlsSecretName" -}}
{{- default (printf "%s-tls" (include "quader-bastion.fullname" .)) .Values.tls.existingSecret -}}
{{- end -}}

{{/* The host of config.server.base_url. */}}
{{- define "quader-bastion.baseHost" -}}
{{- $u := dig "server" "base_url" "" .Values.config -}}
{{- if $u -}}
{{- (urlParse $u).hostname -}}
{{- end -}}
{{- end -}}

{{/* The path of the trust bundle file (the one key of trustedCA.keys), empty without a bundle. */}}
{{- define "quader-bastion.trustedCAFile" -}}
{{- if or .Values.trustedCA.existingSecret .Values.trustedCA.existingConfigMap -}}
{{- printf "/etc/quader/trusted-ca/%s" (first .Values.trustedCA.keys) -}}
{{- end -}}
{{- end -}}

{{/* Is Valkey configured (config.valkey.mode other than none)? Renders "true" or nothing. */}}
{{- define "quader-bastion.valkeyOn" -}}
{{- if ne (dig "valkey" "mode" "none" .Values.config) "none" -}}
true
{{- end -}}
{{- end -}}

{{/* Is the bucket backend selected? Renders "true" or nothing. */}}
{{- define "quader-bastion.bucketOn" -}}
{{- if eq (dig "bastion" "cache" "backend" "volume" .Values.config) "bucket" -}}
true
{{- end -}}
{{- end -}}

{{/*
The configuration file (R-BAS-002): config merged with the keys the chart manages. The managed keys
win; setting one inside config is refused by quader-bastion.validate. The files named here are the
mounts of quader-bastion.secretMounts; none of their contents is part of the checksum, so replacing a
Secret rolls nothing.
*/}}
{{- define "quader-bastion.config" -}}
{{- $s := .Values.secrets -}}
{{- $cfg := .Values.config -}}
{{- $trust := include "quader-bastion.trustedCAFile" . -}}
{{- $main := dict "token_file" "/var/run/secrets/quader/main-token/token" -}}
{{- if $trust -}}
{{- $_ := set $main "ca_file" $trust -}}
{{- end -}}
{{- $bastion := dict "main" $main "replicas" (include "quader-bastion.replicas" . | int) -}}
{{- if $s.signingKey.existingSecret -}}
{{- $_ := set $bastion "signing_key_file" "/var/run/secrets/quader/signing-key/key" -}}
{{- end -}}
{{- $cache := dict -}}
{{- if $s.cacheKey.existingSecret -}}
{{- $_ := set $cache "encryption_key_file" "/var/run/secrets/quader/cache-key/key" -}}
{{- end -}}
{{- if include "quader-bastion.bucketOn" . -}}
{{- $bucket := dict -}}
{{- if $trust -}}
{{- $_ := set $bucket "ca_file" $trust -}}
{{- end -}}
{{- if eq (dig "bastion" "cache" "bucket" "credentials" "source" "static" $cfg) "static" -}}
{{- $_ := set $bucket "credentials" (dict "access_key_id_file" "/var/run/secrets/quader/cache-bucket/access-key-id" "secret_access_key_file" "/var/run/secrets/quader/cache-bucket/secret-access-key") -}}
{{- end -}}
{{- $_ := set $cache "bucket" $bucket -}}
{{- else -}}
{{- /* persistent: whether the replica's copy of the public catalogue survives a restart, which it reports (R-BAS-111). */ -}}
{{- $_ := set $cache "volume" (dict "dir" "/var/cache/quader-bastion" "persistent" (eq .Values.cacheVolume.type "persistent")) -}}
{{- end -}}
{{- $_ := set $bastion "cache" $cache -}}
{{- /* The edge figure the replica reports with every handshake (R-BAS-093). */ -}}
{{- if .Values.edgeLimit.enabled -}}
{{- $_ := set $bastion "edge_limit" (dict "enabled" true "requests_per_second" (.Values.edgeLimit.requestsPerSecond | int) "connections" (.Values.edgeLimit.connections | int)) -}}
{{- else -}}
{{- $_ := set $bastion "edge_limit" (dict "enabled" false) -}}
{{- end -}}
{{- $managed := dict "bastion" $bastion -}}
{{- if include "quader-bastion.valkeyOn" . -}}
{{- $vk := dict -}}
{{- if $s.valkey.existingSecret -}}
{{- if $s.valkey.usernameKey -}}
{{- $_ := set $vk "username_file" "/var/run/secrets/quader/valkey/username" -}}
{{- end -}}
{{- $_ := set $vk "password_file" "/var/run/secrets/quader/valkey/password" -}}
{{- end -}}
{{- if $s.valkeySentinel.existingSecret -}}
{{- $sn := dict "password_file" "/var/run/secrets/quader/valkey-sentinel/password" -}}
{{- if $s.valkeySentinel.usernameKey -}}
{{- $_ := set $sn "username_file" "/var/run/secrets/quader/valkey-sentinel/username" -}}
{{- end -}}
{{- $_ := set $vk "sentinel" $sn -}}
{{- end -}}
{{- if $s.valkeyCA.existingSecret -}}
{{- $_ := set $vk "tls" (dict "ca_file" "/var/run/secrets/quader/valkey-ca/ca.crt") -}}
{{- else if $trust -}}
{{- $_ := set $vk "tls" (dict "ca_file" $trust) -}}
{{- end -}}
{{- $_ := set $managed "valkey" $vk -}}
{{- end -}}
{{- /* The trust bundle also verifies the collector (tracing.ca_file is chart-managed, R-BAS-046). */ -}}
{{- if and $trust (dig "tracing" "enabled" false $cfg) -}}
{{- $_ := set $managed "tracing" (dict "ca_file" $trust) -}}
{{- end -}}
{{- /* TLS (R-BAS-044): server.tls for the client listener; server.ops_tls keeps its default (inherit: true). */ -}}
{{- if .Values.tls.enabled -}}
{{- $_ := set $managed "server" (dict "tls" (dict "certificates" (list (dict "cert_file" "/var/run/secrets/quader/tls/tls.crt" "key_file" "/var/run/secrets/quader/tls/tls.key")))) -}}
{{- end -}}
{{- $managed | mergeOverwrite (deepCopy $cfg) | toYaml -}}
{{- end -}}

{{/*
The Secret and ConfigMap mounts of a pod (R-BAS-002, R-BAS-044) as a YAML list. Each entry names one
existing object, the keys projected and the file name each key gets. They mount read-only as a
directory without subPath, so a replaced Secret reaches the files (the Bastion re-reads the token
and the TLS material), and never become environment variables.
*/}}
{{- define "quader-bastion.secretMounts" -}}
{{- $s := .Values.secrets -}}
{{- $out := list -}}
{{- $out = append $out (dict "name" "main-token" "kind" "secret" "object" $s.mainToken.existingSecret "path" "/var/run/secrets/quader/main-token" "items" (list (dict "key" $s.mainToken.key "path" "token"))) -}}
{{- if $s.signingKey.existingSecret -}}
{{- $out = append $out (dict "name" "signing-key" "kind" "secret" "object" $s.signingKey.existingSecret "path" "/var/run/secrets/quader/signing-key" "items" (list (dict "key" $s.signingKey.key "path" "key"))) -}}
{{- end -}}
{{- if $s.cacheKey.existingSecret -}}
{{- $out = append $out (dict "name" "cache-key" "kind" "secret" "object" $s.cacheKey.existingSecret "path" "/var/run/secrets/quader/cache-key" "items" (list (dict "key" $s.cacheKey.key "path" "key"))) -}}
{{- end -}}
{{- if and (include "quader-bastion.bucketOn" .) (eq (dig "bastion" "cache" "bucket" "credentials" "source" "static" .Values.config) "static") -}}
{{- $out = append $out (dict "name" "cache-bucket" "kind" "secret" "object" $s.cacheBucket.existingSecret "path" "/var/run/secrets/quader/cache-bucket" "items" (list (dict "key" $s.cacheBucket.accessKeyIdKey "path" "access-key-id") (dict "key" $s.cacheBucket.secretAccessKeyKey "path" "secret-access-key"))) -}}
{{- end -}}
{{- if and (include "quader-bastion.valkeyOn" .) $s.valkey.existingSecret -}}
{{- $items := list -}}
{{- if $s.valkey.usernameKey -}}
{{- $items = append $items (dict "key" $s.valkey.usernameKey "path" "username") -}}
{{- end -}}
{{- $items = append $items (dict "key" $s.valkey.passwordKey "path" "password") -}}
{{- $out = append $out (dict "name" "valkey" "kind" "secret" "object" $s.valkey.existingSecret "path" "/var/run/secrets/quader/valkey" "items" $items) -}}
{{- end -}}
{{- if and (include "quader-bastion.valkeyOn" .) $s.valkeySentinel.existingSecret -}}
{{- $snItems := list -}}
{{- if $s.valkeySentinel.usernameKey -}}
{{- $snItems = append $snItems (dict "key" $s.valkeySentinel.usernameKey "path" "username") -}}
{{- end -}}
{{- $snItems = append $snItems (dict "key" $s.valkeySentinel.passwordKey "path" "password") -}}
{{- $out = append $out (dict "name" "valkey-sentinel" "kind" "secret" "object" $s.valkeySentinel.existingSecret "path" "/var/run/secrets/quader/valkey-sentinel" "items" $snItems) -}}
{{- end -}}
{{- if and (include "quader-bastion.valkeyOn" .) $s.valkeyCA.existingSecret -}}
{{- $out = append $out (dict "name" "valkey-ca" "kind" "secret" "object" $s.valkeyCA.existingSecret "path" "/var/run/secrets/quader/valkey-ca" "items" (list (dict "key" $s.valkeyCA.key "path" "ca.crt"))) -}}
{{- end -}}
{{- if .Values.trustedCA.existingSecret -}}
{{- $out = append $out (dict "name" "trusted-ca" "kind" "secret" "object" .Values.trustedCA.existingSecret "path" "/etc/quader/trusted-ca" "items" (list (dict "key" (first .Values.trustedCA.keys) "path" (first .Values.trustedCA.keys)))) -}}
{{- else if .Values.trustedCA.existingConfigMap -}}
{{- $out = append $out (dict "name" "trusted-ca" "kind" "configMap" "object" .Values.trustedCA.existingConfigMap "path" "/etc/quader/trusted-ca" "items" (list (dict "key" (first .Values.trustedCA.keys) "path" (first .Values.trustedCA.keys)))) -}}
{{- end -}}
{{- if .Values.tls.enabled -}}
{{- $out = append $out (dict "name" "tls" "kind" "secret" "object" (include "quader-bastion.tlsSecretName" .) "path" "/var/run/secrets/quader/tls" "items" (list (dict "key" "tls.crt" "path" "tls.crt") (dict "key" "tls.key" "path" "tls.key"))) -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{- define "quader-bastion.secretVolumes" -}}
{{- range fromYamlArray (include "quader-bastion.secretMounts" .) }}
- name: secret-{{ .name }}
  {{- if eq .kind "secret" }}
  secret:
    secretName: {{ .object }}
    defaultMode: 0440
    items:
    {{- range .items }}
    - key: {{ .key }}
      path: {{ .path }}
    {{- end }}
  {{- else }}
  configMap:
    name: {{ .object }}
    defaultMode: 0444
    items:
    {{- range .items }}
    - key: {{ .key }}
      path: {{ .path }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end -}}

{{- define "quader-bastion.secretVolumeMounts" -}}
{{- range fromYamlArray (include "quader-bastion.secretMounts" .) }}
- name: secret-{{ .name }}
  mountPath: {{ .path }}
  readOnly: true
{{- end }}
{{- end -}}

{{/* Probes (R-OPS-001, R-BAS-043) on the ops port; HTTPS while the listeners run TLS. */}}
{{- define "quader-bastion.probes" -}}
{{- range $name := list "startup" "liveness" "readiness" }}
{{- $p := get $.Values.probes $name }}
{{- if $p.enabled }}
{{ $name }}Probe:
  httpGet:
    path: {{ $p.path }}
    port: ops
    {{- if $.Values.tls.enabled }}
    scheme: HTTPS
    {{- end }}
  periodSeconds: {{ $p.periodSeconds }}
  timeoutSeconds: {{ $p.timeoutSeconds }}
  failureThreshold: {{ $p.failureThreshold }}
  {{- if eq $name "readiness" }}
  successThreshold: {{ $p.successThreshold }}
  {{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
Topology spread (R-BAS-043): zone (ScheduleAnyway) and host (DoNotSchedule). Both honour node taints and
compute the skew per revision (matchLabelKeys), so a rollout does not pile new pods onto the nodes the
old ones leave. Non-empty topologySpreadConstraints replace the generated ones.
*/}}
{{- define "quader-bastion.topologySpread" -}}
{{- $t := .Values.topologySpread -}}
{{- if .Values.topologySpreadConstraints }}
topologySpreadConstraints:
{{- toYaml .Values.topologySpreadConstraints | nindent 2 }}
{{- else if $t.enabled }}
topologySpreadConstraints:
{{- range $key, $c := dict "topology.kubernetes.io/zone" $t.zone "kubernetes.io/hostname" $t.host }}
  - topologyKey: {{ $key }}
    maxSkew: {{ $c.maxSkew }}
    whenUnsatisfiable: {{ $c.whenUnsatisfiable }}
    nodeTaintsPolicy: {{ $t.nodeTaintsPolicy }}
    matchLabelKeys:
      - {{ ternary "controller-revision-hash" "pod-template-hash" (eq (include "quader-bastion.workloadKind" $) "StatefulSet") }}
    labelSelector:
      matchLabels:
        {{- include "quader-bastion.selectorLabels" $ | nindent 8 }}
{{- end }}
{{- end }}
{{- end -}}
