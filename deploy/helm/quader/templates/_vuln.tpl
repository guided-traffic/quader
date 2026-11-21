{{/*
The image scanning's links and checks, and the scan listener both scannings read through (R-HA-065, R-HA-066, R-HA-068, ADR-212). The
image scanning's scanner is a Deployment of its own, <fullname>-vuln-scanner-trivy (Q-HA-372), rendered by
templates/vuln-scanner-trivy.yaml beside the units of templates/_units.tpl: it shares the pod pieces (probes, topology spread, security
context) but none of the secrets, the shared extras or the service account of the other Deployments.
*/}}

{{/* "true" while the image scanning is on, and the chart renders vuln-scanner-trivy: config.vulnerabilities.enabled and
config.vulnerabilities.images.enabled (the defaults). */}}
{{- define "quader.imageScanOn" -}}
{{- $vuln := dig "vulnerabilities" (dict) .Values.config -}}
{{- if and (dig "enabled" true $vuln) (dig "enabled" true (dig "images" (dict) $vuln)) -}}true{{- end -}}
{{- end -}}

{{/* "true" while the main packages pods open the scan listener: either scanning reads through it (R-HA-066, R-HA-068). */}}
{{- define "quader.scanListenOn" -}}
{{- if or (include "quader.imageScanOn" .) (include "quader.malwareOn" .) -}}true{{- end -}}
{{- end -}}

{{/* The path of the internal CA in the pods of the links (R-ARCH-038): the ca.crt of the pod's own Secret, beside its pair (R-HA-063). */}}
{{- define "quader.internalCAPath" -}}/var/run/secrets/quader/tls/ca.crt{{- end -}}

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
{{- $m := .managed -}}
{{- if and (include "quader.scanListenOn" $) $.Values.tls.enabled -}}
{{- $srv := get $m "server" | default (dict) -}}
{{- $_ := set $srv "internal_tls" (dict "ca_file" (include "quader.internalCAPath" $)) -}}
{{- /* The scan listener admits every kind of the role scanner the release renders (quader.tlsKindTable). */ -}}
{{- $_ := set $srv "scan_clients" (include "quader.scanClients" $ | fromYamlArray) -}}
{{- $_ := set $m "server" $srv -}}
{{- end -}}
{{- if include "quader.imageScanOn" $ -}}
{{- $fullname := include "quader.fullname" $ -}}
{{- $scheme := ternary "https" "http" $.Values.tls.enabled -}}
{{- $links := dict "url" (printf "%s://%s-vuln-scanner-trivy.%s.svc:8080" $scheme $fullname $.Release.Namespace) -}}
{{- $pull := dict "url" (printf "%s://%s-packages-scan.%s.svc:8082" $scheme $fullname $.Release.Namespace) -}}
{{- if $.Values.tls.enabled -}}
{{- /* Each end checks the other's kind (R-HA-066, Q-VULN-185): the worker calls the scanner, the scanner pulls from packages. */ -}}
{{- $_ := set $links "server_name" (include "quader.tlsName" (dict "root" $ "kind" "vuln-scanner-trivy")) -}}
{{- $_ := set $links "client_name" (include "quader.tlsName" (dict "root" $ "kind" "worker")) -}}
{{- $_ := set $pull "server_name" (include "quader.tlsName" (dict "root" $ "kind" "packages")) -}}
{{- /* The scan listener records a scanner's report of itself under the kind its certificate names (R-OPS-048, Q-OPS-223). */ -}}
{{- $_ := set $pull "client_name" (include "quader.tlsName" (dict "root" $ "kind" "vuln-scanner-trivy")) -}}
{{- end -}}
{{- $_ := set $m "vulnerabilities" (dict "images" (dict "scanner" $links "registry" $pull)) -}}
{{- end -}}
{{- include "quader.malwareManagedConfig" (dict "root" $ "managed" $m) -}}
{{- end -}}

{{/* The configuration keys the chart manages for the links of both scannings, refused when set in config (R-HA-029). Returns a YAML list. */}}
{{- define "quader.scannerManagedPaths" -}}
{{- toYaml (concat (list "vulnerabilities.images.scanner.url" "vulnerabilities.images.scanner.server_name" "vulnerabilities.images.scanner.client_name" "vulnerabilities.images.registry.url" "vulnerabilities.images.registry.server_name" "vulnerabilities.images.registry.client_name" "server.internal_tls" "server.scan_listen" "server.scan_clients") (include "quader.malwareManagedPaths" . | fromYamlArray)) -}}
{{- end -}}

{{/*
"true" when the pods of a role verify a link with the internal CA (R-HA-066, R-HA-068), so their Secret's ca.crt joins their pair: the
main packages pods, which serve the scan listener, and every worker pod, which calls a scanner, while either scanning is on. The
own mounts of the kinds of the role scanner are in templates/vuln-scanner-trivy.yaml, templates/malware-scanner-clamav.yaml and
templates/malware-updater-freshclam.yaml. Argument: dict "root" . "role" "pool".
*/}}
{{- define "quader.linkNeedsCA" -}}
{{- if and .root.Values.tls.enabled (include "quader.scanListenOn" .root) (or (eq .role "worker") (and (eq .role "packages") (not .pool))) -}}true{{- end -}}
{{- end -}}

{{/* The environment of the main packages Deployment for the scan listener (R-HA-066, R-ARCH-037). Argument: dict "root" . "role" "pool". */}}
{{- define "quader.scanListenEnv" -}}
{{- if and (include "quader.scanListenOn" .root) (eq .role "packages") (not .pool) -}}
- name: QUADER_SERVER_SCAN_LISTEN
  value: ":8082"
{{- end -}}
{{- end -}}

{{/*
The ingress rule of the scan listener in the policy of the main packages Deployment (R-HA-042, R-HA-066, R-HA-068, R-HA-069): port 8082
from the pods of the kinds of the role scanner the release renders (vuln-scanner-trivy, malware-scanner-clamav and, for its seed and its
report, malware-updater-freshclam), never from anywhere. Argument: as quader.scanListenEnv. Returns one YAML list item, or nothing.
*/}}
{{- define "quader.scanListenRule" -}}
{{- if and (include "quader.scanListenOn" .root) (eq .role "packages") (not .pool) -}}
- ports:
    - protocol: TCP
      port: 8082
  from:
    {{- if include "quader.imageScanOn" .root }}
    - podSelector:
        matchLabels:
          {{- include "quader.selectorLabels" (dict "root" .root "component" "vuln-scanner-trivy") | nindent 10 }}
    {{- end }}
    {{- if include "quader.malwareOn" .root }}
    - podSelector:
        matchLabels:
          {{- include "quader.selectorLabels" (dict "root" .root "component" "malware-scanner-clamav") | nindent 10 }}
    {{- end }}
    {{- if include "quader.malwareUpdaterOn" .root }}
    - podSelector:
        matchLabels:
          {{- include "quader.selectorLabels" (dict "root" .root "component" "malware-updater-freshclam") | nindent 10 }}
    {{- end }}
{{- end -}}
{{- end -}}

{{/* The container port of the scan listener on the main packages Deployment. Argument: as quader.scanListenEnv. */}}
{{- define "quader.scanListenPort" -}}
{{- if and (include "quader.scanListenOn" .root) (eq .role "packages") (not .pool) -}}
- name: scan
  containerPort: 8082
  protocol: TCP
{{- end -}}
{{- end -}}

{{/*
The checks of vuln-scanner-trivy (R-HA-065, R-HA-029, Q-HA-318): its spool budget holds every slot's largest image plus 64 MiB, its
replicas. Its pair is checked with the other kinds' (quader.validate, R-HA-063). The worker's check of its spool is in
quader.validateRoles. Argument: the root context.
*/}}
{{- define "quader.validateVulnScannerTrivy" -}}
{{- $v := .Values -}}
{{- if include "quader.imageScanOn" . -}}
{{- $images := dig "vulnerabilities" "images" (dict) $v.config -}}
{{- $slots := dig "scanner" "slots" 1 $images | int64 -}}
{{- $maxImage := include "quader.bytes" (dig "max_image_size" "3GiB" $images) | int64 -}}
{{- $need := mul $slots (add $maxImage 67108864) -}}
{{- $sizeKey := ternary "spool.ephemeral.size" "vulnScannerTrivy.spool.sizeLimit" (eq $v.spool.type "ephemeral") -}}
{{- $sizeValue := include "quader.spoolSize" (dict "root" . "role" "vuln-scanner-trivy" "r" $v.vulnScannerTrivy) -}}
{{- $size := include "quader.bytes" $sizeValue | int64 -}}
{{- $headroom := include "quader.bytes" $v.spool.headroom | int64 -}}
{{- $budget := sub $size $headroom -}}
{{- if lt $budget $need -}}
{{- fail (printf "quader: the spool budget of vuln-scanner-trivy, %s (%v) minus spool.headroom (%v), is %d bytes and cannot hold config.vulnerabilities.images.scanner.slots (%d) times config.vulnerabilities.images.max_image_size (%v) plus 64 MiB, %d bytes; raise %s or lower max_image_size (Q-HA-318)" $sizeKey $sizeValue $v.spool.headroom $budget $slots (dig "max_image_size" "3GiB" $images) $need $sizeKey) -}}
{{- end -}}
{{- $a := $v.vulnScannerTrivy.autoscaling -}}
{{- $min := ternary (int $a.minReplicas) (int $v.vulnScannerTrivy.replicaCount) $a.enabled -}}
{{- $minKey := ternary "vulnScannerTrivy.autoscaling.minReplicas" "vulnScannerTrivy.replicaCount" $a.enabled -}}
{{- if and (lt $min 1) (not (or $v.restore.enabled $v.postRestore.enabled $v.postRestore.hold)) -}}
{{- fail (printf "quader: %s must be at least 1 while the image scanning is on; without a scanner no image is analysed (R-HA-065)" $minKey) -}}
{{- end -}}
{{- if and $v.highAvailability.enforce (lt $min 2) (not (or $v.restore.enabled $v.postRestore.enabled $v.postRestore.hold)) -}}
{{- fail (printf "quader: highAvailability.enforce needs %s of at least 2 (Deployment %s-vuln-scanner-trivy)" $minKey (include "quader.fullname" .)) -}}
{{- end -}}
{{- if and $a.enabled (lt (int $a.maxReplicas) (int $a.minReplicas)) -}}
{{- fail (printf "quader: vulnScannerTrivy.autoscaling.maxReplicas (%v) must not be below vulnScannerTrivy.autoscaling.minReplicas (%v)" $a.maxReplicas $a.minReplicas) -}}
{{- end -}}
{{- if and $v.vulnScannerTrivy.podDisruptionBudget.minAvailable $v.vulnScannerTrivy.podDisruptionBudget.maxUnavailable -}}
{{- fail "quader: vulnScannerTrivy.podDisruptionBudget.minAvailable and vulnScannerTrivy.podDisruptionBudget.maxUnavailable are mutually exclusive; set one and null the other" -}}
{{- end -}}
{{- end -}}
{{- end -}}
