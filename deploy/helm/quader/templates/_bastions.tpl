{{/*
The Bastion registrations the values declare (docs/spec/bastion.md R-BAS-127, Q-BAS-465 to Q-BAS-469): bastions.declared names each
registration, its description, its networks and the existing Secret of its token, the value the Bastion's chart reads from
secrets.mainToken. The chart renders config.bastions.declared with the token file's path and the Secret's name, never a value, and
projects the Secrets into the migration Job alone, which applies the declaration (quader migrate); no replica, scanner, backup or
restore Job mounts them, because only migrate reads the value and the database keeps its hash. Each source is optional, so a missing
Secret lets the Job start and fail with the entry's name instead of waiting on a volume.
*/}}

{{/* The directory of the volume; each entry's file is <name>/token below it. */}}
{{- define "quader.bastionTokensDir" -}}/var/run/secrets/quader/bastion-tokens{{- end -}}

{{/* The declared registrations of the values, a list (empty by default). Argument: the root context. */}}
{{- define "quader.declaredBastionList" -}}
{{- toYaml (dig "declared" (list) (.Values.bastions | default dict)) -}}
{{- end -}}

{{/*
config.bastions.declared, set on the dict "managed" of quader.config while the list has an entry: per name description, egress,
token_file below quader.bastionTokensDir and secret_name. The default renders nothing. Argument: dict "root" . "managed" <the managed
dict>.
*/}}
{{- define "quader.bastionsManagedConfig" -}}
{{- $list := include "quader.declaredBastionList" .root | fromYamlArray -}}
{{- if $list -}}
{{- $dir := include "quader.bastionTokensDir" .root -}}
{{- $out := dict -}}
{{- range $b := $list -}}
{{- $e := dict "token_file" (printf "%s/%s/token" $dir $b.name) "secret_name" $b.token.existingSecret -}}
{{- with $b.description -}}{{- $_ := set $e "description" . -}}{{- end -}}
{{- with $b.egress -}}{{- $_ := set $e "egress" . -}}{{- end -}}
{{- $_ := set $out $b.name $e -}}
{{- end -}}
{{- $_ := set .managed "bastions" (dict "declared" $out) -}}
{{- end -}}
{{- end -}}

{{/* The volume mount of the token files in the migration Job's container; nothing without an entry. Argument: the root context. */}}
{{- define "quader.bastionTokensMount" -}}
{{- if include "quader.declaredBastionList" . | fromYamlArray -}}
- name: bastion-tokens
  mountPath: {{ include "quader.bastionTokensDir" . }}
  readOnly: true
{{- end -}}
{{- end -}}

{{/*
The projected volume of the token files: one optional source per entry, the key at <name>/token, mounted without subPath, read-only,
with the mode of the chart's other mounted Secrets.
*/}}
{{- define "quader.bastionTokensVolume" -}}
{{- $list := include "quader.declaredBastionList" . | fromYamlArray -}}
{{- if $list -}}
- name: bastion-tokens
  projected:
    defaultMode: 0440
    sources:
      {{- range $list }}
      - secret:
          name: {{ .token.existingSecret }}
          optional: true
          items:
            - key: {{ .token.key | default "token" | quote }}
              path: {{ .name }}/token
      {{- end }}
{{- end -}}
{{- end -}}

{{/*
The checks of the declared registrations that the schema cannot state: a name appears once, and config.bastions.declared is the chart's
to render. The shape of each entry (the name's pattern, the networks, the Secret) is the schema's; the value of the token and the
networks' finer rules are checked by quader migrate, which fails naming the entry.
*/}}
{{- define "quader.validateBastions" -}}
{{- if hasKey (dig "bastions" (dict) .Values.config) "declared" -}}
{{- fail "quader: config.bastions.declared is rendered by the chart from bastions.declared and must not be set; declare the registrations under bastions.declared" -}}
{{- end -}}
{{- $seen := dict -}}
{{- range $i, $b := include "quader.declaredBastionList" . | fromYamlArray -}}
{{- if hasKey $seen $b.name -}}
{{- fail (printf "quader: bastions.declared[%d].name %q is also the name of bastions.declared[%d]; a registration's name is unique" $i $b.name (get $seen $b.name)) -}}
{{- end -}}
{{- $_ := set $seen $b.name $i -}}
{{- end -}}
{{- end -}}
