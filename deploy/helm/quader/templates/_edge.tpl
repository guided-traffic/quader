{{/*
The edge's path rules (R-HA-037, Q-HA-71, R-EP-008, Q-EP-71) as data, so that the Ingress of the
default endpoint, the Ingress of each further endpoint and the BackendTLSPolicies read one table. The
rules are fixed, not values: every one is a Prefix match (whole path elements, the longest match
wins), and a pool's paths are the only data that goes in.

Argument: dict "root" . "endpoint" "" (the default endpoint) or the name of a further endpoint.
Returns a YAML list of {path, service, role}, most specific first; role is the kind of the pods behind the Service (R-HA-063). The Service's port is the port named
http for the default endpoint and the endpoint's port number for a further one.
*/}}
{{- define "quader.edgeRules" -}}
{{- $root := .root -}}
{{- $fullname := include "quader.fullname" $root -}}
{{- $rules := list -}}
{{- if not .endpoint -}}
{{- range $role := list "api" "packages" -}}
{{- range $p := (index $root.Values $role).pools -}}
{{- /* routed: false renders the pool without its route, the first step of adding a pool (Q-HA-90). */ -}}
{{- if dig "routed" true $p -}}
{{- range $path := $p.paths -}}
{{- $rules = append $rules (dict "path" $path "service" (printf "%s-%s-%s" $fullname $role $p.name) "role" $role) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $api := printf "%s-api" $fullname -}}
{{- $packages := printf "%s-packages" $fullname -}}
{{- $rules = concat $rules (list (dict "path" "/api/v1" "service" $api "role" "api") (dict "path" "/api" "service" $packages "role" "packages") (dict "path" "/files" "service" $packages "role" "packages") (dict "path" "/v2" "service" $packages "role" "packages") (dict "path" "/.well-known" "service" $packages "role" "packages") (dict "path" "/" "service" $api "role" "api")) -}}
{{- else -}}
{{- $name := .endpoint -}}
{{- $e := include "quader.endpoint" (dict "root" $root "name" $name) | fromYaml -}}
{{- $roles := include "quader.surfaceRoles" (dict "root" $root "name" $name) | fromYamlArray -}}
{{- $svc := printf "%s-ep-%s" $fullname $name -}}
{{- if eq (len $roles) 1 -}}
{{- /* Within one role: the whole host to the one Service. */ -}}
{{- $rules = append $rules (dict "path" "/" "service" $svc "role" (first $roles)) -}}
{{- else -}}
{{- /* A surface with families of both roles: the rows of the table whose families it holds. */ -}}
{{- $api := printf "%s-api-ep-%s" $fullname $name -}}
{{- $s := $e.surface -}}
{{- if or (has "api" $s) (has "ci_exchange" $s) -}}
{{- $rules = append $rules (dict "path" "/api/v1" "service" $api "role" "api") -}}
{{- end -}}
{{- if has "packages" $s -}}
{{- $rules = append $rules (dict "path" "/api" "service" $svc "role" "packages") -}}
{{- end -}}
{{- if has "files" $s -}}
{{- $rules = append $rules (dict "path" "/files" "service" $svc "role" "packages") -}}
{{- end -}}
{{- if has "packages" $s -}}
{{- $rules = concat $rules (list (dict "path" "/v2" "service" $svc "role" "packages") (dict "path" "/.well-known" "service" $svc "role" "packages")) -}}
{{- end -}}
{{- $rules = append $rules (dict "path" "/" "service" $api "role" "api") -}}
{{- end -}}
{{- end -}}
{{- toYaml $rules -}}
{{- end -}}

{{/* The Services the default endpoint's edge names, in order and without repeats, as a YAML list. Argument: the root context. */}}
{{- define "quader.edgeServices" -}}
{{- $out := list -}}
{{- range $r := include "quader.edgeRules" (dict "root" . "endpoint" "") | fromYamlArray -}}
{{- $out = append $out $r.service -}}
{{- end -}}
{{- toYaml (uniq $out) -}}
{{- end -}}
