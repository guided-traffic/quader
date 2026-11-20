{{/*
The role scanner and the links of the image scanning (R-HA-065, R-HA-066, ADR-212). The scanner is a Deployment of its own,
rendered by templates/scanner.yaml beside the units of templates/_units.tpl: it shares the pod pieces (probes, topology
spread, security context) but none of the secrets, the shared extras or the service account of the other Deployments.
*/}}

{{/* "true" while the chart renders the scanner: config.vulnerabilities.enabled and config.vulnerabilities.images.enabled (the defaults). */}}
{{- define "quader.scannerOn" -}}
{{- $vuln := dig "vulnerabilities" (dict) .Values.config -}}
{{- if and (dig "enabled" true $vuln) (dig "enabled" true (dig "images" (dict) $vuln)) -}}true{{- end -}}
{{- end -}}

{{/* The Secret of the pair the scanner serves and presents: scanner.tls.secretName, else tls.secretName (Q-HA-317). */}}
{{- define "quader.scannerTLSSecret" -}}
{{- .Values.scanner.tls.secretName | default .Values.tls.secretName -}}
{{- end -}}

{{/* The name the links verify in the far end's certificate: the name the chart's certificate carries for the edge (R-HA-037). */}}
{{- define "quader.linkServerName" -}}
{{- printf "%s.%s.svc" (include "quader.fullname" .) .Release.Namespace -}}
{{- end -}}

{{/* The path of the internal CA in the pods of the links (R-ARCH-038). */}}
{{- define "quader.internalCAPath" -}}/var/run/secrets/quader/tls-internal/ca.crt{{- end -}}

{{/*
The worker Deployment that claims the queue vuln (Q-HA-320): the pool that lists it, else the main worker. Returns the unit's pool
("" for the main worker) as YAML: the selector of the scanner's ingress rule is built from it.
*/}}
{{- define "quader.vulnWorkerPool" -}}
{{- $pool := "" -}}
{{- range $p := .Values.worker.pools -}}
{{- if and (has "vuln" ($p.queues | default list)) (not $pool) -}}
{{- $pool = $p.name -}}
{{- end -}}
{{- end -}}
{{- $pool -}}
{{- end -}}

{{/*
The keys of the configuration file the chart manages for the image scanning's links (R-HA-066), set on the dict "managed" of
quader.config. Argument: dict "root" . "managed" <the managed dict>.
*/}}
{{- define "quader.scannerManagedConfig" -}}
{{- $ := .root -}}
{{- if include "quader.scannerOn" $ -}}
{{- $m := .managed -}}
{{- $fullname := include "quader.fullname" $ -}}
{{- $scheme := ternary "https" "http" $.Values.tls.enabled -}}
{{- $links := dict "url" (printf "%s://%s-scanner.%s.svc:8080" $scheme $fullname $.Release.Namespace) -}}
{{- $pull := dict "url" (printf "%s://%s-packages-scan.%s.svc:8082" $scheme $fullname $.Release.Namespace) -}}
{{- if $.Values.tls.enabled -}}
{{- $_ := set $links "server_name" (include "quader.linkServerName" $) -}}
{{- $_ := set $pull "server_name" (include "quader.linkServerName" $) -}}
{{- $srv := get $m "server" | default (dict) -}}
{{- $_ := set $srv "internal_tls" (dict "ca_file" (include "quader.internalCAPath" $)) -}}
{{- $_ := set $m "server" $srv -}}
{{- end -}}
{{- $_ := set $m "vulnerabilities" (dict "images" (dict "scanner" $links "registry" $pull)) -}}
{{- end -}}
{{- end -}}

{{/* The configuration keys the chart manages for the links, refused when set in config (R-HA-029). Returns a YAML list. */}}
{{- define "quader.scannerManagedPaths" -}}
{{- toYaml (list "vulnerabilities.images.scanner.url" "vulnerabilities.images.scanner.server_name" "vulnerabilities.images.registry.url" "vulnerabilities.images.registry.server_name" "server.internal_tls" "server.scan_listen") -}}
{{- end -}}

{{/*
The mounts of the links in a pod of a role (R-HA-066): the internal CA (ca.crt of tls.secretName) in the main packages pods, which
serve the scan listener, and in every worker pod, which calls the scanner. The scanner's own mounts are in templates/scanner.yaml.
Argument: dict "root" . "role" "pool". Returns a YAML list of the entries of quader.secretMounts.
*/}}
{{- define "quader.scannerLinkMounts" -}}
{{- $out := list -}}
{{- $ := .root -}}
{{- if and $.Values.tls.enabled (include "quader.scannerOn" $) (or (eq .role "worker") (and (eq .role "packages") (not .pool))) -}}
{{- $out = append $out (dict "name" "tls-internal" "secret" $.Values.tls.secretName "path" "/var/run/secrets/quader/tls-internal" "items" (list (dict "key" "ca.crt" "path" "ca.crt"))) -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/* "true" when the pods of a role need the chart's pair for a link although they would not load it otherwise: a worker presents it to the scanner (R-ARCH-038). Argument: dict "root" . "role". */}}
{{- define "quader.linkNeedsPair" -}}
{{- if and .root.Values.tls.enabled (include "quader.scannerOn" .root) (eq .role "worker") -}}true{{- end -}}
{{- end -}}

{{/* The environment of the main packages Deployment for the scan listener (R-HA-066, R-ARCH-037). Argument: dict "root" . "role" "pool". */}}
{{- define "quader.scanListenEnv" -}}
{{- if and (include "quader.scannerOn" .root) (eq .role "packages") (not .pool) -}}
- name: QUADER_SERVER_SCAN_LISTEN
  value: ":8082"
{{- end -}}
{{- end -}}

{{/*
The ingress rule of the scan listener in the policy of the main packages Deployment (R-HA-042, R-HA-066): port 8082 from the scanner's
pods only, never from anywhere. Argument: as quader.scanListenEnv. Returns one YAML list item, or nothing.
*/}}
{{- define "quader.scanListenRule" -}}
{{- if and (include "quader.scannerOn" .root) (eq .role "packages") (not .pool) -}}
- ports:
    - protocol: TCP
      port: 8082
  from:
    - podSelector:
        matchLabels:
          {{- include "quader.selectorLabels" (dict "root" .root "component" "scanner") | nindent 10 }}
{{- end -}}
{{- end -}}

{{/* The container port of the scan listener on the main packages Deployment. Argument: as quader.scanListenEnv. */}}
{{- define "quader.scanListenPort" -}}
{{- if and (include "quader.scannerOn" .root) (eq .role "packages") (not .pool) -}}
- name: scan
  containerPort: 8082
  protocol: TCP
{{- end -}}
{{- end -}}

{{/*
The checks of the scanner (R-HA-065, R-HA-029, Q-HA-318): its spool budget holds every slot's largest image plus 64 MiB, its
replicas, and its own pair only with TLS. The worker's check of its spool is in quader.validateRoles. Argument: the root context.
*/}}
{{- define "quader.validateScanner" -}}
{{- $v := .Values -}}
{{- if include "quader.scannerOn" . -}}
{{- $images := dig "vulnerabilities" "images" (dict) $v.config -}}
{{- $slots := dig "scanner" "slots" 1 $images | int64 -}}
{{- $maxImage := include "quader.bytes" (dig "max_image_size" "3GiB" $images) | int64 -}}
{{- $need := mul $slots (add $maxImage 67108864) -}}
{{- $sizeKey := ternary "spool.ephemeral.size" "scanner.spool.sizeLimit" (eq $v.spool.type "ephemeral") -}}
{{- $sizeValue := include "quader.spoolSize" (dict "root" . "role" "scanner" "r" $v.scanner) -}}
{{- $size := include "quader.bytes" $sizeValue | int64 -}}
{{- $headroom := include "quader.bytes" $v.spool.headroom | int64 -}}
{{- $budget := sub $size $headroom -}}
{{- if lt $budget $need -}}
{{- fail (printf "quader: the spool budget of the scanner, %s (%v) minus spool.headroom (%v), is %d bytes and cannot hold config.vulnerabilities.images.scanner.slots (%d) times config.vulnerabilities.images.max_image_size (%v) plus 64 MiB, %d bytes; raise %s or lower max_image_size (Q-HA-318)" $sizeKey $sizeValue $v.spool.headroom $budget $slots (dig "max_image_size" "3GiB" $images) $need $sizeKey) -}}
{{- end -}}
{{- $a := $v.scanner.autoscaling -}}
{{- $min := ternary (int $a.minReplicas) (int $v.scanner.replicaCount) $a.enabled -}}
{{- $minKey := ternary "scanner.autoscaling.minReplicas" "scanner.replicaCount" $a.enabled -}}
{{- if and (lt $min 1) (not (or $v.restore.enabled $v.postRestore.enabled $v.postRestore.hold)) -}}
{{- fail (printf "quader: %s must be at least 1 while the image scanning is on; without a scanner no image is analysed (R-HA-065)" $minKey) -}}
{{- end -}}
{{- if and $v.highAvailability.enforce (lt $min 2) (not (or $v.restore.enabled $v.postRestore.enabled $v.postRestore.hold)) -}}
{{- fail (printf "quader: highAvailability.enforce needs %s of at least 2 (Deployment %s-scanner)" $minKey (include "quader.fullname" .)) -}}
{{- end -}}
{{- if and $a.enabled (lt (int $a.maxReplicas) (int $a.minReplicas)) -}}
{{- fail (printf "quader: scanner.autoscaling.maxReplicas (%v) must not be below scanner.autoscaling.minReplicas (%v)" $a.maxReplicas $a.minReplicas) -}}
{{- end -}}
{{- if and $v.scanner.podDisruptionBudget.minAvailable $v.scanner.podDisruptionBudget.maxUnavailable -}}
{{- fail "quader: scanner.podDisruptionBudget.minAvailable and scanner.podDisruptionBudget.maxUnavailable are mutually exclusive; set one and null the other" -}}
{{- end -}}
{{- end -}}
{{- if and $v.scanner.tls.secretName (not $v.tls.enabled) -}}
{{- fail "quader: scanner.tls.secretName needs tls.enabled: without TLS the links of the image scanning are plain HTTP and no pair is served (Q-HA-322)" -}}
{{- end -}}
{{- end -}}
