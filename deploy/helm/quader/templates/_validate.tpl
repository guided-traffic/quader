{{/*
Fail-fast validation (R-HA-029). Every failure names the offending value. Rows of R-HA-029 that
belong to features this chart does not render yet (pooler, gateway, pools) are added with those
features; the backup and restore rows are in templates/_validate_backup.tpl.
*/}}
{{/*
A Gateway API duration (GEP-2257: a sequence of h, m, s and ms terms, "1h30m", "3600s") in milliseconds. The schema
fixes the form, so every term is a number and a unit.
*/}}
{{- define "quader.gatewayDurationMs" -}}
{{- $ms := 0 -}}
{{- range $t := regexFindAll "[0-9]+(ms|h|m|s)" . -1 -}}
{{- $n := int (regexFind "^[0-9]+" $t) -}}
{{- $u := regexFind "(ms|h|m|s)$" $t -}}
{{- if eq $u "h" -}}{{- $ms = add $ms (mul $n 3600000) -}}
{{- else if eq $u "m" -}}{{- $ms = add $ms (mul $n 60000) -}}
{{- else if eq $u "s" -}}{{- $ms = add $ms (mul $n 1000) -}}
{{- else -}}{{- $ms = add $ms $n -}}
{{- end -}}
{{- end -}}
{{- $ms -}}
{{- end -}}

{{/*
The names the chart builds from <fullname> alone that Kubernetes limits (R-HA-029, Q-HA-374): the Service of every kind of pod of
quader.tlsKindTable and the kind's name in its certificate, DNS labels of at most 63 characters, for every kind of the table whether
this release renders it or not, so that switching a kind on never fails for its name; the Services <fullname>-ops and
<fullname>-packages-scan; the Jobs <fullname>-migrate, <fullname>-restore and <fullname>-post-restore-<8 hex>, whose name is a label
value of their pods; and the CronJob <fullname>-backup, at most 52, because its controller names a Job <CronJob>-<10 digits>. The
tightest of them decides how long <fullname> may be: the render fails naming that name and the longest release name and
fullnameOverride that fit. A name that carries a value of the installation (a pool, an endpoint) is checked with that value
(quader.validatePools, quader.validateEndpoints). Argument: the root context.
*/}}
{{- define "quader.validateNames" -}}
{{- $fullname := include "quader.fullname" . -}}
{{- $names := list -}}
{{- range $e := include "quader.tlsKindTable" . | fromYamlArray -}}
{{- $names = append $names (dict "object" "Service" "suffix" $e.kind "len" (len $e.kind) "max" 63) -}}
{{- end -}}
{{- range $s := list "ops" "packages-scan" -}}
{{- $names = append $names (dict "object" "Service" "suffix" $s "len" (len $s) "max" 63) -}}
{{- end -}}
{{- range $s := list "migrate" "restore" -}}
{{- $names = append $names (dict "object" "Job" "suffix" $s "len" (len $s) "max" 63) -}}
{{- end -}}
{{- $names = append $names (dict "object" "Job" "suffix" "post-restore-<8 hex>" "len" (add (len "post-restore-") 8) "max" 63) -}}
{{- $names = append $names (dict "object" "CronJob" "suffix" "backup" "len" (len "backup") "max" 52) -}}
{{- $room := -1 -}}
{{- $tight := dict -}}
{{- range $n := $names -}}
{{- $r := sub (sub $n.max 1) $n.len -}}
{{- if or (lt $room 0) (lt $r $room) -}}
{{- $room = $r -}}
{{- $tight = $n -}}
{{- end -}}
{{- end -}}
{{- if gt (len $fullname) $room -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- $fits := printf "set fullnameOverride to at most %d characters" $room -}}
{{- if not .Values.fullnameOverride -}}
{{- $alone := sub (sub $room 1) (len $name) -}}
{{- if gt $alone 0 -}}
{{- $fits = printf "choose a release name of at most %d characters, or one of at most %d that contains %s (then <fullname> is the release name), or %s" $alone $room $name $fits -}}
{{- else -}}
{{- $fits = printf "choose a release name of at most %d characters that contains %s, or %s" $room $name $fits -}}
{{- end -}}
{{- end -}}
{{- fail (printf "quader: the %s name %s-%s has %d characters, at most %d are allowed. Every name the chart builds from <fullname> has to fit, which leaves <fullname> (%s, %d characters) at most %d: %s" $tight.object $fullname $tight.suffix (add (len $fullname) 1 $tight.len) $tight.max $fullname (len $fullname) $room $fits) -}}
{{- end -}}
{{- end -}}

{{- define "quader.validate" -}}
{{- $v := .Values -}}
{{- $cfg := $v.config -}}
{{- $ns := .Release.Namespace -}}

{{- include "quader.validateNames" . -}}

{{- if not (dig "server" "base_url" "" $cfg) -}}
{{- fail "quader: config.server.base_url is required (example: https://quader.example.com)" -}}
{{- end -}}
{{- if not (dig "database" "host" "" $cfg) -}}
{{- fail "quader: config.database.host is required (example: quader-db-rw.quader.svc)" -}}
{{- end -}}
{{- if not $v.secrets.database.existingSecret -}}
{{- fail "quader: secrets.database.existingSecret is required; the chart never creates a Secret (example: quader-db-app)" -}}
{{- end -}}
{{- if and (not (dig "storage" "s3" "bucket" "" $cfg)) (not (and $v.secrets.s3.existingSecret $v.secrets.s3.bucketKey)) -}}
{{- fail "quader: config.storage.s3.bucket is required (example: quader-blobs), or name the key of the bucket in the S3 Secret with secrets.s3.existingSecret and secrets.s3.bucketKey (example: S3_BUCKET)" -}}
{{- end -}}
{{- if and (or $v.secrets.s3.bucketKey $v.secrets.s3.regionKey $v.secrets.s3.endpointKey) (not $v.secrets.s3.existingSecret) -}}
{{- fail "quader: secrets.s3.bucketKey, secrets.s3.regionKey and secrets.s3.endpointKey read keys of the S3 Secret and need secrets.s3.existingSecret" -}}
{{- end -}}
{{- if and (eq (dig "storage" "s3" "credentials" "source" "" $cfg) "static") (not $v.secrets.s3.existingSecret) -}}
{{- fail "quader: config.storage.s3.credentials.source: static needs secrets.s3.existingSecret" -}}
{{- end -}}
{{- if or (not $v.secrets.encryption.existingSecret) (not $v.secrets.encryption.keys) -}}
{{- fail (printf "quader: secrets.encryption.existingSecret and secrets.encryption.keys are required. The chart never generates the key (a regenerated key would make every sealed value undecryptable). Create the Secret before the first install:\n\n  kubectl -n %s create secret generic quader-encryption-keys \\\n    --from-file=2026-10.key=<(head -c 32 /dev/urandom | base64)\n\nthen set secrets.encryption.existingSecret=quader-encryption-keys and secrets.encryption.keys={2026-10.key}. The file name without .key is the key ID." $ns) -}}
{{- end -}}

{{- /* Login secrets (R-AUTH-004, R-AUTH-014). */ -}}
{{- $oidcSeen := dict -}}
{{- range $i, $o := $v.secrets.oidc -}}
{{- if not (regexMatch "^[a-z0-9][a-z0-9-]{0,31}$" ($o.providerId | default "")) -}}
{{- fail (printf "quader: secrets.oidc[%d].providerId %q must match ^[a-z0-9][a-z0-9-]{0,31}$ (the provider ID of config.auth.oidc.providers, example: corp)" $i ($o.providerId | default "")) -}}
{{- end -}}
{{- if hasKey $oidcSeen $o.providerId -}}
{{- fail (printf "quader: secrets.oidc has two entries for providerId %q" $o.providerId) -}}
{{- end -}}
{{- $_ := set $oidcSeen $o.providerId true -}}
{{- if not $o.existingSecret -}}
{{- fail (printf "quader: secrets.oidc[%d].existingSecret is required for providerId %q (example: quader-oidc-%s)" $i $o.providerId $o.providerId) -}}
{{- end -}}
{{- if not (hasKey (dig "auth" "oidc" "providers" (dict) $cfg) $o.providerId) -}}
{{- fail (printf "quader: secrets.oidc[%d].providerId %q has no provider of that ID in config.auth.oidc.providers" $i $o.providerId) -}}
{{- end -}}
{{- if dig "auth" "oidc" "providers" $o.providerId "client_secret_file" "" $cfg -}}
{{- fail (printf "quader: config.auth.oidc.providers.%s.client_secret_file is managed by the chart while secrets.oidc names the provider and must not be set" $o.providerId) -}}
{{- end -}}
{{- end -}}

{{- /* A provider with a client secret gets it from secrets.oidc and so only on the api pods: the global extraEnv, extraEnvFrom, extraVolumes and extraVolumeMounts reach every Deployment and would spread it to packages and worker pods (R-AUTH-014, Q-HA-79). */ -}}
{{- range $id, $prov := (dig "auth" "oidc" "providers" (dict) $cfg) -}}
{{- if and (ne (dig "client_auth_method" "client_secret_basic" $prov) "none") (not (hasKey $oidcSeen $id)) -}}
{{- fail (printf "quader: config.auth.oidc.providers.%s has client_auth_method %s and no entry in secrets.oidc: its client secret would have to come through extraEnv, extraEnvFrom, extraVolumes or extraVolumeMounts, which every Deployment receives, and the secret would leave the api pods. Add {providerId: %s, existingSecret: <Secret>} to secrets.oidc, or set client_auth_method: none for a public client" $id (dig "client_auth_method" "client_secret_basic" $prov) $id) -}}
{{- end -}}
{{- end -}}

{{- /* Valkey (R-HA-056). */ -}}
{{- if not $v.secrets.valkey.existingSecret -}}
{{- fail "quader: secrets.valkey.existingSecret is required (example: quader-valkey)" -}}
{{- end -}}
{{- $mode := dig "valkey" "mode" "sentinel" $cfg -}}
{{- if eq $mode "sentinel" -}}
{{- $addrs := dig "valkey" "sentinel" "addresses" (list) $cfg -}}
{{- if not $addrs -}}
{{- fail "quader: config.valkey.sentinel.addresses is required with config.valkey.mode: sentinel" -}}
{{- end -}}
{{- if not (dig "valkey" "sentinel" "primary_name" "" $cfg) -}}
{{- fail "quader: config.valkey.sentinel.primary_name is required with config.valkey.mode: sentinel (example: quader)" -}}
{{- end -}}
{{- if and $v.highAvailability.enforce (lt (len $addrs) 3) -}}
{{- fail "quader: highAvailability.enforce needs at least 3 entries in config.valkey.sentinel.addresses" -}}
{{- end -}}
{{- else if eq $mode "standalone" -}}
{{- if $v.highAvailability.enforce -}}
{{- fail "quader: highAvailability.enforce refuses config.valkey.mode: standalone; use config.valkey.mode: sentinel" -}}
{{- end -}}
{{- if not (dig "valkey" "address" "" $cfg) -}}
{{- fail "quader: config.valkey.address is required with config.valkey.mode: standalone" -}}
{{- end -}}
{{- else -}}
{{- fail (printf "quader: config.valkey.mode %q is not supported; use sentinel (or standalone for development)" $mode) -}}
{{- end -}}
{{- if and (eq (dig "valkey" "tls" "mode" "verify-full" $cfg) "disable") (not (dig "valkey" "allow_insecure" false $cfg)) -}}
{{- fail "quader: config.valkey.tls.mode: disable needs config.valkey.allow_insecure: true" -}}
{{- end -}}
{{- if and (eq (dig "metrics" "auth" "none" $cfg) "static") (not $v.secrets.metricsToken.existingSecret) -}}
{{- fail "quader: config.metrics.auth: static needs secrets.metricsToken.existingSecret" -}}
{{- end -}}

{{- /* TLS (R-HA-063, Q-HA-32). */ -}}
{{- $t := $v.tls -}}
{{- if $t.enabled -}}
{{- /* A pair per kind of pod (R-HA-063, Q-HA-345, Q-HA-347): issued by tls.certManager.issuerRef, or an existing Secret per kind, never both, never one Secret for two kinds. */ -}}
{{- $kinds := include "quader.tlsKinds" . | fromYamlArray -}}
{{- $named := list -}}
{{- range $e := include "quader.tlsKindTable" . | fromYamlArray -}}
{{- $k := $e.kind -}}
{{- with index $t.existingSecrets $k -}}{{- $named = append $named $k -}}{{- end -}}
{{- end -}}
{{- if and $t.certManager.issuerRef.name $named -}}
{{- fail (printf "quader: tls.certManager.issuerRef.name and tls.existingSecrets.%s are both set: the chart issues the pairs with an issuer, or the Secrets name them, not both (R-HA-063)" (first $named)) -}}
{{- end -}}
{{- if not $t.certManager.issuerRef.name -}}
{{- if not $named -}}
{{- fail "quader: tls.certManager.issuerRef.name or tls.existingSecrets is required while tls.enabled is true (default): every kind of pod has a pair of its own (R-HA-063). Name a cert-manager issuer (example: tls.certManager.issuerRef.name: cluster-ca, tls.certManager.issuerRef.kind: ClusterIssuer) and the chart issues them, or name an existing Secret per kind in tls.existingSecrets (api, packages, worker, vuln-scanner-trivy, malware-scanner-clamav), or opt out with tls.enabled: false (plain HTTP inside the cluster)" -}}
{{- end -}}
{{- $seen := dict -}}
{{- range $k := $kinds -}}
{{- $s := index $t.existingSecrets $k -}}
{{- if not $s -}}
{{- fail (printf "quader: tls.existingSecrets.%s is required while tls.existingSecrets names the pairs: every kind of pod the chart renders (%s) has a Secret of its own with tls.crt, tls.key and ca.crt, for <fullname>-%s.<namespace>.svc (R-HA-063)" $k (join ", " $kinds) $k) -}}
{{- end -}}
{{- if hasKey $seen $s -}}
{{- fail (printf "quader: tls.existingSecrets.%s and tls.existingSecrets.%s name the same Secret %s: every kind of pod has a pair of its own (R-HA-063, Q-HA-347)" (get $seen $s) $k $s) -}}
{{- end -}}
{{- $_ := set $seen $s $k -}}
{{- end -}}
{{- end -}}
{{- if $v.ingress.enabled -}}
{{- $bt := $v.ingress.backendTLS -}}
{{- if and $bt.verify (eq $bt.controller "traefik") (not (and $bt.serversTransports.api $bt.serversTransports.packages)) -}}
{{- fail "quader: ingress.backendTLS.serversTransports.api and .packages are required for controller traefik while ingress.backendTLS.verify is true (<namespace>-<name>@kubernetescrd of a ServersTransport per kind, each with that kind's name as serverName, R-HA-063), or set ingress.backendTLS.verify: false" -}}
{{- end -}}
{{- if and $t.clientCA.secretName (eq $bt.controller "haproxy") (not $bt.clientCertSecret) -}}
{{- fail "quader: tls.clientCA requires ingress.backendTLS.clientCertSecret with controller haproxy: quader demands a client certificate and HAProxy Ingress needs a Secret with tls.crt and tls.key to present" -}}
{{- end -}}
{{- end -}}
{{- if and $v.tls.backendTLSPolicy.enabled (not $t.backendTLSPolicy.caConfigMapName) -}}
{{- fail "quader: tls.backendTLSPolicy.caConfigMapName is required with tls.backendTLSPolicy.enabled (a ConfigMap with the CA as ca.crt)" -}}
{{- end -}}
{{- else -}}
{{- if or $t.clientCA.secretName $t.backendTLSPolicy.enabled -}}
{{- fail "quader: tls.clientCA and tls.backendTLSPolicy need tls.enabled: true" -}}
{{- end -}}
{{- end -}}

{{- /* Gateway API exposure (R-HA-038, R-HA-029). */ -}}
{{- if $v.gateway.enabled -}}
{{- if not (.Capabilities.APIVersions.Has "gateway.networking.k8s.io/v1/HTTPRoute") -}}
{{- fail "quader: gateway.enabled needs the Gateway API in the cluster (gateway.networking.k8s.io/v1 HTTPRoute, CRDs v1.2 or later, Standard channel); install the CRDs, or set gateway.enabled: false. With helm template, pass --api-versions gateway.networking.k8s.io/v1/HTTPRoute" -}}
{{- end -}}
{{- if not $v.gateway.parentRefs -}}
{{- fail "quader: gateway.parentRefs is required with gateway.enabled: the Gateway the HTTPRoute attaches to (example: [{name: shared, namespace: gateway-system, sectionName: https}])" -}}
{{- end -}}
{{- $reqMs := int (include "quader.gatewayDurationMs" $v.gateway.timeouts.request) -}}
{{- $backMs := int (include "quader.gatewayDurationMs" $v.gateway.timeouts.backendRequest) -}}
{{- if and (gt $reqMs 0) (gt $backMs $reqMs) -}}
{{- fail (printf "quader: gateway.timeouts.backendRequest (%s) must not exceed gateway.timeouts.request (%s)" $v.gateway.timeouts.backendRequest $v.gateway.timeouts.request) -}}
{{- end -}}
{{- end -}}

{{- /* The ServiceMonitor reads the CA Secret of the scrape from its own namespace (R-HA-045). */ -}}
{{- if and $v.metrics.serviceMonitor.enabled $v.tls.enabled $v.metrics.serviceMonitor.namespace (ne $v.metrics.serviceMonitor.namespace $ns) -}}
{{- fail (printf "quader: metrics.serviceMonitor.namespace (%s) must be the release namespace (%s) while tls.enabled is true: the Prometheus operator reads the CA Secret of the scrape (ca.crt of the Secret of the kind api) from the namespace of the ServiceMonitor" $v.metrics.serviceMonitor.namespace $ns) -}}
{{- end -}}

{{- /* The trusted CA bundle (R-HA-043): one source, files to mount, and the object store's CA is either this bundle or secrets.s3CA. */ -}}
{{- $ca := $v.trustedCA -}}
{{- if and $ca.existingSecret $ca.existingConfigMap -}}
{{- fail "quader: trustedCA.existingSecret and trustedCA.existingConfigMap are both set; name one source (example: trustedCA.existingConfigMap: corp-ca-bundle)" -}}
{{- end -}}
{{- if and (or $ca.existingSecret $ca.existingConfigMap) (not $ca.keys) -}}
{{- fail "quader: trustedCA.keys is empty while a source is named; list the files of the source that hold the PEM bundles (example: [ca-bundle.crt])" -}}
{{- end -}}
{{- if $ca.useForObjectStore -}}
{{- if not (or $ca.existingSecret $ca.existingConfigMap) -}}
{{- fail "quader: trustedCA.useForObjectStore is true, but trustedCA.existingSecret and trustedCA.existingConfigMap are empty; name the source of the bundle" -}}
{{- end -}}
{{- if ne (len $ca.keys) 1 -}}
{{- fail (printf "quader: trustedCA.useForObjectStore needs exactly one file in trustedCA.keys, because storage.s3.ca_file is one file (trustedCA.keys has %d)" (len $ca.keys)) -}}
{{- end -}}
{{- if $v.secrets.s3CA.existingSecret -}}
{{- fail "quader: trustedCA.useForObjectStore and secrets.s3CA.existingSecret both set storage.s3.ca_file; use one of them" -}}
{{- end -}}
{{- end -}}

{{- /* Keys the chart manages (R-HA-030): setting one inside config is refused. */ -}}
{{- $managedPaths := list "storage.spool.dir" "database.username_file" "database.password_file" "database.ssl_root_cert_file" "database.ssl_cert_file" "database.ssl_key_file" "valkey.username_file" "valkey.password_file" "valkey.sentinel.username_file" "valkey.sentinel.password_file" "valkey.tls.ca_file" "valkey.tls.cert_file" "valkey.tls.key_file" "storage.s3.ca_file" "storage.s3.credentials.access_key_id_file" "storage.s3.credentials.secret_access_key_file" "metrics.bearer_token_file" "auth.break_glass.password_file" "secrets.encryption_key_files" "secrets.active_key_id" "server.endpoints" "backup.manual_run" "backup.source" "backup.target.ca_file" "backup.target.credentials.access_key_id_file" "backup.target.credentials.secret_access_key_file" "backup.target.credentials.session_token_file" -}}
{{- if $t.enabled -}}
{{- $managedPaths = concat $managedPaths (list "server.tls" "server.ops_tls") -}}
{{- end -}}
{{- $managedPaths = concat $managedPaths (include "quader.scannerManagedPaths" . | fromYamlArray) -}}
{{- range $path := $managedPaths -}}
{{- $cur := $cfg -}}
{{- $found := true -}}
{{- range splitList "." $path -}}
{{- if and $found (kindIs "map" $cur) (hasKey $cur .) -}}
{{- $cur = index $cur . -}}
{{- else -}}
{{- $found = false -}}
{{- end -}}
{{- end -}}
{{- if $found -}}
{{- fail (printf "quader: config.%s is managed by the chart and must not be set (secret file paths come from the secrets values, TLS comes from the tls values, the spool directory is fixed, endpoints come from the endpoints values)" $path) -}}
{{- end -}}
{{- end -}}

{{- include "quader.validateRoles" . -}}
{{- include "quader.validateVulnScannerTrivy" . -}}
{{- include "quader.validateMalware" . -}}

{{- /* The PrometheusRule (R-HA-045): a name in disabledAlerts that is no alert is a typo. */ -}}
{{- $alertNames := include "quader.rules.names" . | fromJsonArray -}}
{{- range $v.metrics.prometheusRule.disabledAlerts -}}
{{- if not (has . $alertNames) -}}
{{- fail (printf "quader: metrics.prometheusRule.disabledAlerts names %q, which is no alert of the chart (%s)" . (join ", " $alertNames)) -}}
{{- end -}}
{{- end -}}

{{- include "quader.validateEndpoints" . -}}
{{- include "quader.validateBackup" . -}}
{{- end -}}

{{/*
Endpoints (R-EP-008, R-HA-029): every failure names the value. Argument: the root context.
*/}}
{{- define "quader.validateEndpoints" -}}
{{- $v := .Values -}}
{{- $fullname := include "quader.fullname" . -}}
{{- $ports := dict -}}
{{- $hostOwner := dict -}}
{{- $wild := dict -}}
{{- $baseHost := include "quader.baseHost" . -}}
{{- if $baseHost -}}
{{- $_ := set $hostOwner $baseHost "config.server.base_url" -}}
{{- end -}}
{{- range (dig "server" "additional_base_urls" (list) $v.config) -}}
{{- $_ := set $hostOwner ((urlParse .).hostname | lower) "config.server.additional_base_urls" -}}
{{- end -}}
{{- range $h := (dig "default" "hosts" (list) $v.endpoints) -}}
{{- $_ := set $hostOwner (lower $h) "endpoints.default.hosts" -}}
{{- end -}}
{{- $names := include "quader.endpointNames" . | fromYamlArray -}}
{{- range $name := $names -}}
{{- $raw := index $v.endpoints $name -}}
{{- $key := printf "endpoints.%s" $name -}}
{{- if not (regexMatch "^[a-z][a-z0-9-]{0,19}$" $name) -}}
{{- fail (printf "quader: %s: the endpoint name must match ^[a-z][a-z0-9-]{0,19}$" $key) -}}
{{- end -}}
{{- if gt (add (len $fullname) 4 (len $name)) 63 -}}
{{- fail (printf "quader: %s: the Service name %s-ep-%s has %d characters, at most 63 are allowed; shorten the endpoint name or set fullnameOverride" $key $fullname $name (add (len $fullname) 4 (len $name))) -}}
{{- end -}}
{{- $e := include "quader.endpoint" (dict "root" $ "name" $name) | fromYaml -}}
{{- if not (hasKey $raw "port") -}}
{{- fail (printf "quader: %s.port is required (example: 8443)" $key) -}}
{{- end -}}
{{- if or (eq (int $e.port) 8080) (eq (int $e.port) 8081) -}}
{{- fail (printf "quader: %s.port %d is taken by the default client listener (8080) or the operations listener (8081)" $key (int $e.port)) -}}
{{- end -}}
{{- if hasKey $ports (toString (int $e.port)) -}}
{{- fail (printf "quader: %s.port %d is already the port of endpoint %s" $key (int $e.port) (get $ports (toString (int $e.port)))) -}}
{{- end -}}
{{- $_ := set $ports (toString (int $e.port)) $name -}}
{{- if not $e.baseUrl -}}
{{- fail (printf "quader: %s.baseUrl is required (example: https://packages.example.com)" $key) -}}
{{- end -}}
{{- $u := urlParse $e.baseUrl -}}
{{- if or (not (has $u.scheme (list "http" "https"))) (not $u.host) (and $u.path (ne $u.path "/")) $u.query $u.fragment $u.userinfo -}}
{{- fail (printf "quader: %s.baseUrl %q must be an origin: a scheme (http or https), a host and an optional port, nothing else" $key $e.baseUrl) -}}
{{- end -}}
{{- /* Surface: ui belongs to the default endpoint (Q-EP-13). */ -}}
{{- if has "ui" $e.surface -}}
{{- fail (printf "quader: %s.surface lists ui; sign-in and the UI belong to the default endpoint (surface_not_supported)" $key) -}}
{{- end -}}
{{- /* Hosts: no host in two endpoints (R-EP-001). */ -}}
{{- range $h := include "quader.endpointHosts" $e | fromYamlArray -}}
{{- if hasKey $hostOwner $h -}}
{{- fail (printf "quader: %s: the host %s is also a host of %s" $key $h (get $hostOwner $h)) -}}
{{- end -}}
{{- $_ := set $hostOwner $h (printf "endpoints.%s" $name) -}}
{{- end -}}
{{- if $e.wildcardHost -}}
{{- if not (regexMatch "^\\*\\.[a-z0-9-]+(\\.[a-z0-9-]+)+$" $e.wildcardHost) -}}
{{- fail (printf "quader: %s.wildcardHost %q must be *.<domain> with at least two labels in the domain (example: *.packages.example.com)" $key $e.wildcardHost) -}}
{{- end -}}
{{- if hasKey $wild (lower $e.wildcardHost) -}}
{{- fail (printf "quader: %s.wildcardHost %q is also the wildcard host of endpoint %s" $key $e.wildcardHost (get $wild (lower $e.wildcardHost))) -}}
{{- end -}}
{{- $_ := set $wild (lower $e.wildcardHost) $name -}}
{{- end -}}
{{- /* TLS inputs need TLS on quader's listeners (R-HA-063). */ -}}
{{- if and (not $v.tls.enabled) (or $e.tlsSecret $e.tlsExtraSecrets $e.tlsClientCaSecret) -}}
{{- fail (printf "quader: %s.tls.existingSecret, extraSecrets and clientCaSecret need tls.enabled: true" $key) -}}
{{- end -}}
{{- /* The endpoint's Ingress follows the rules of the default one: a merged backendTLS block that verifies with traefik needs a transport per kind it routes to (R-HA-063). */ -}}
{{- if and $v.tls.enabled $e.ingressEnabled -}}
{{- $bt := include "quader.endpointBackendTLS" (dict "root" $ "name" $name) | fromYaml -}}
{{- if and $bt.verify (eq $bt.controller "traefik") -}}
{{- range $role := include "quader.surfaceRoles" (dict "root" $ "name" $name) | fromYamlArray -}}
{{- if not (index ($bt.serversTransports | default dict) $role) -}}
{{- fail (printf "quader: %s.ingress.backendTLS.serversTransports.%s is required for controller traefik while verify is true (set it here or in ingress.backendTLS; <namespace>-<name>@kubernetescrd of a ServersTransport that verifies the name the endpoint's %s pods serve), or set %s.ingress.backendTLS.verify: false" $key $role $role $key) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if and $e.tlsClientCaSecret (eq $bt.controller "haproxy") (not $bt.clientCertSecret) -}}
{{- fail (printf "quader: %s.tls.clientCaSecret requires %s.ingress.backendTLS.clientCertSecret (or ingress.backendTLS.clientCertSecret) with controller haproxy: quader demands a client certificate on this endpoint and HAProxy Ingress needs a Secret with tls.crt and tls.key to present" $key $key) -}}
{{- end -}}
{{- end -}}
{{- /* The Service of a surface with families of both roles, and the pool Deployments, must keep a valid name (R-EP-008). The rules of a pool (replicas, budget, spreading, memory) are checked with every Deployment in quader.validateRoles. */ -}}
{{- $names := include "quader.surfaceRoles" (dict "root" $ "name" $name) | fromYamlArray -}}
{{- if and (eq (len $names) 2) (gt (add (len $fullname) 8 (len $name)) 63) -}}
{{- fail (printf "quader: %s: the Service name %s-api-ep-%s has %d characters, at most 63 are allowed; shorten the endpoint name or set fullnameOverride" $key $fullname $name (add (len $fullname) 8 (len $name))) -}}
{{- end -}}
{{- if and $e.poolEnabled (gt (add (len $fullname) 13 (len $name)) 63) -}}
{{- fail (printf "quader: %s.pool: the Deployment name %s-packages-ep-%s has %d characters, at most 63 are allowed; shorten the endpoint name or set fullnameOverride" $key $fullname $name (add (len $fullname) 13 (len $name))) -}}
{{- end -}}
{{- end -}}
{{- /* An exact host of one endpoint that another endpoint's wildcard host covers. */ -}}
{{- range $wh, $owner := $wild -}}
{{- range $h, $hOwner := $hostOwner -}}
{{- if and (ne $hOwner (printf "endpoints.%s" $owner)) (regexMatch (printf "^[a-z0-9-]+%s$" (trimPrefix "*" $wh | replace "." "\\.")) $h) -}}
{{- fail (printf "quader: endpoints.%s.wildcardHost %q covers the host %s of %s" $owner $wh $h $hOwner) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
