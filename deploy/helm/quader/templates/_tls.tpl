{{/*
The pairs of the kinds of pod (R-HA-063, Q-HA-345 to Q-HA-349): every kind has a pair and a name of its own, issued by the chart
through tls.certManager.issuerRef (templates/certificates.yaml) or named per kind in tls.existingSecrets.
*/}}

{{/*
The kinds of pod that hold a pair (Q-HA-346), one entry each: "kind" (its name, <fullname>-<kind>.<namespace>.svc, its Secret
<fullname>-tls-<kind> and its key of tls.existingSecrets), "client" (its pair also allows client authentication, because it calls a
link), "scanClient" (the scan listener admits it: a kind of scanner, server.scan_clients) and "on" (the release renders it). A kind
that runs one third-party tool is named <purpose>-scanner-<tool> (Q-HA-372): vuln-scanner-trivy, malware-scanner-clamav. A further
kind of pod is one entry here, one key of tls.existingSecrets in values.yaml and values.schema.json, and its Deployment's mount of
quader.tlsSecret; the Certificates, the checks, the Secrets' validation and the scan listener's names follow from this list. Argument:
the root context. Returns a YAML list.
*/}}
{{- define "quader.tlsKindTable" -}}
{{- $vuln := include "quader.imageScanOn" . | eq "true" -}}
{{- $malware := include "quader.malwareOn" . | eq "true" -}}
{{- $t := list (dict "kind" "api" "client" false "scanClient" false "on" true) -}}
{{- $t = append $t (dict "kind" "packages" "client" false "scanClient" false "on" true) -}}
{{- $t = append $t (dict "kind" "worker" "client" true "scanClient" false "on" true) -}}
{{- $t = append $t (dict "kind" "vuln-scanner-trivy" "client" true "scanClient" true "on" $vuln) -}}
{{- $t = append $t (dict "kind" "malware-scanner-clamav" "client" true "scanClient" true "on" $malware) -}}
{{- toYaml $t -}}
{{- end -}}

{{/* The kinds of pod the release renders, in the order of quader.tlsKindTable. Argument: the root context. Returns a YAML list. */}}
{{- define "quader.tlsKinds" -}}
{{- $k := list -}}
{{- range $e := include "quader.tlsKindTable" . | fromYamlArray -}}
{{- if $e.on -}}{{- $k = append $k $e.kind -}}{{- end -}}
{{- end -}}
{{- toYaml $k -}}
{{- end -}}

{{/* The names the scan listener admits, server.scan_clients (R-HA-066): every kind of scanner the release renders. Argument: the root
context. Returns a YAML list. */}}
{{- define "quader.scanClients" -}}
{{- $n := list -}}
{{- range $e := include "quader.tlsKindTable" . | fromYamlArray -}}
{{- if and $e.on $e.scanClient -}}{{- $n = append $n (include "quader.tlsName" (dict "root" $ "kind" $e.kind)) -}}{{- end -}}
{{- end -}}
{{- toYaml $n -}}
{{- end -}}

{{/* The name of a kind, which its pair carries and every check of it verifies: <fullname>-<kind>.<namespace>.svc. Argument: dict
"root" . "kind" "api". */}}
{{- define "quader.tlsName" -}}
{{- printf "%s-%s.%s.svc" (include "quader.fullname" .root) .kind .root.Release.Namespace -}}
{{- end -}}

{{/* The name every pair carries besides its kind's, which the scrape verifies (Q-HA-349). Argument: the root context. */}}
{{- define "quader.tlsOpsName" -}}
{{- printf "%s-ops.%s.svc" (include "quader.fullname" .) .Release.Namespace -}}
{{- end -}}

{{/* The Secret of a kind's pair: <fullname>-tls-<kind> with tls.certManager.issuerRef, else tls.existingSecrets.<kind>. Argument: dict "root" . "kind". */}}
{{- define "quader.tlsSecret" -}}
{{- if .root.Values.tls.certManager.issuerRef.name -}}
{{- printf "%s-tls-%s" (include "quader.fullname" .root) .kind -}}
{{- else -}}
{{- index .root.Values.tls.existingSecrets .kind -}}
{{- end -}}
{{- end -}}

{{/*
The backend settings a Service the edge routes to carries (R-HA-063, Q-HA-348): HAProxy Ingress reads its backend keys per Service,
so secure-sni with the name of the pair the Service's pods serve and secure-verify-ca-secret go here; Traefik reads the scheme and
the transport of the kind here. The name is the kind's, or, for a further endpoint with a pair of its own, the Service's own name
<Service>.<namespace>.svc. The CA is ingress.backendTLS.caSecret, else the Secret of that pair, whose ca.crt the controller reads.
Argument: dict "root" . "bt" (the effective ingress.backendTLS) "kind" (api | packages) "own" (the Secret of an endpoint's own pair,
or "") "service" (the Service's name). Returns a YAML map, empty without TLS.
*/}}
{{- define "quader.backendServiceAnnotations" -}}
{{- $out := dict -}}
{{- $root := .root -}}
{{- if $root.Values.tls.enabled -}}
{{- $name := include "quader.tlsName" (dict "root" $root "kind" .kind) -}}
{{- $ca := include "quader.tlsSecret" (dict "root" $root "kind" .kind) -}}
{{- if .own -}}
{{- $name = printf "%s.%s.svc" .service $root.Release.Namespace -}}
{{- $ca = .own -}}
{{- end -}}
{{- $ca = .bt.caSecret | default $ca -}}
{{- if and (eq .bt.controller "haproxy") .bt.verify -}}
{{- $_ := set $out "haproxy-ingress.github.io/secure-sni" $name -}}
{{- $_ := set $out "haproxy-ingress.github.io/secure-verify-ca-secret" $ca -}}
{{- end -}}
{{- if eq .bt.controller "traefik" -}}
{{- $_ := set $out "traefik.ingress.kubernetes.io/service.serversscheme" "https" -}}
{{- with index (.bt.serversTransports | default dict) .kind -}}
{{- $_ := set $out "traefik.ingress.kubernetes.io/service.serverstransport" . -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
The Ingress-level backend settings (R-HA-063): HAProxy Ingress's secure-backends and secure-crt-secret, the same for every backend;
and, for ingress-nginx, which reads its proxy settings per Ingress only, those of the one kind whose rules the Ingress holds
(Q-HA-348). Argument: dict "root" . "bt" "kind" (the kind of the Ingress's rules, ingress-nginx only) "own" "service" as above.
Returns a YAML map.
*/}}
{{- define "quader.backendIngressAnnotations" -}}
{{- $out := dict -}}
{{- $root := .root -}}
{{- if $root.Values.tls.enabled -}}
{{- if eq .bt.controller "haproxy" -}}
{{- $hp := "haproxy-ingress.github.io/" -}}
{{- $_ := set $out (print $hp "secure-backends") "true" -}}
{{- with .bt.clientCertSecret -}}
{{- $_ := set $out (print $hp "secure-crt-secret") . -}}
{{- end -}}
{{- end -}}
{{- if eq .bt.controller "ingress-nginx" -}}
{{- $_ := set $out "nginx.ingress.kubernetes.io/backend-protocol" "HTTPS" -}}
{{- if .bt.verify -}}
{{- $name := include "quader.tlsName" (dict "root" $root "kind" .kind) -}}
{{- $ca := include "quader.tlsSecret" (dict "root" $root "kind" .kind) -}}
{{- if .own -}}
{{- $name = printf "%s.%s.svc" .service $root.Release.Namespace -}}
{{- $ca = .own -}}
{{- end -}}
{{- $_ := set $out "nginx.ingress.kubernetes.io/proxy-ssl-verify" "on" -}}
{{- $_ := set $out "nginx.ingress.kubernetes.io/proxy-ssl-secret" (printf "%s/%s" $root.Release.Namespace (.bt.caSecret | default $ca)) -}}
{{- $_ := set $out "nginx.ingress.kubernetes.io/proxy-ssl-name" $name -}}
{{- $_ := set $out "nginx.ingress.kubernetes.io/proxy-ssl-server-name" "on" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
The Ingress objects of an endpoint's rules (Q-HA-348): one for every controller but ingress-nginx with TLS, which gets one per kind,
so that each carries its kind's proxy-ssl-name. The default endpoint's are <fullname> (api) and <fullname>-packages; a further
endpoint's are named after the Service of each kind. Only the Ingress that keeps the one Ingress's name holds the spec.tls, so
cert-manager's ingress-shim sees each TLS Secret once. Argument: dict "root" . "rules" (quader.edgeRules) "name" (the name of the one
Ingress) "endpoint" (a further endpoint's name, or "") "controller". Returns a YAML list of {name, kind, service, tls, rules}.
*/}}
{{- define "quader.ingressGroups" -}}
{{- if and .root.Values.tls.enabled (eq .controller "ingress-nginx") -}}
{{- $out := list -}}
{{- range $kind := list "api" "packages" -}}
{{- $rs := list -}}
{{- range $r := $.rules -}}{{- if eq $r.role $kind -}}{{- $rs = append $rs $r -}}{{- end -}}{{- end -}}
{{- if $rs -}}
{{- $name := ternary (first $rs).service (ternary $.name (printf "%s-%s" $.name $kind) (eq $kind "api")) (ne $.endpoint "") -}}
{{- $out = append $out (dict "name" $name "kind" $kind "service" (first $rs).service "tls" (eq $name $.name) "rules" $rs) -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- else -}}
{{- toYaml (list (dict "name" .name "kind" (first .rules).role "service" (first .rules).service "tls" true "rules" .rules)) -}}
{{- end -}}
{{- end -}}
