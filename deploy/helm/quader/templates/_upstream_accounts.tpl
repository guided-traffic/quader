{{/*
The accounts of the registries of the built-in project upstream (R-HA-073, repositories-and-proxy.md R-REPO-077, Q-HA-493, Q-HA-494):
secrets.upstreamAccounts names, by the key of a repository of the project upstream, an existing Secret and the keys in it. Each entry
is one source of one projected volume, optional, so a Secret or a key that does not exist yet starts every pod and fails that
registry's fetches with 502 upstream_secret_unavailable until it exists. The volume reaches the pods of api, packages and worker (the
main Deployments, their pools and their endpoint pools: everything quader.deployment renders), which fetch from upstreams; never a
scanner, the updater, the migration Job or a backup or restore Job. The files are read at every use, so a rotated Secret reaches every
replica within the kubelet's sync without a restart (R-HA-030), and the configuration file holds their paths and the Secret's name,
never a value.
*/}}

{{/* The directory of the volume; each entry's files are <key>/username and <key>/password below it. */}}
{{- define "quader.upstreamAccountsDir" -}}/var/run/secrets/quader/upstream-accounts{{- end -}}

{{/* An entry's authMode with its default. Argument: the entry. */}}
{{- define "quader.upstreamAccountMode" -}}{{ .authMode | default "basic" }}{{- end -}}

{{/* The volume of the accounts' files in a pod of api, packages or worker; nothing without an entry. Argument: the root context. */}}
{{- define "quader.upstreamAccountsVolume" -}}
{{- $accounts := .Values.secrets.upstreamAccounts | default dict -}}
{{- if $accounts }}
- name: upstream-accounts
  projected:
    defaultMode: 0440
    sources:
      {{- range $key := keys $accounts | sortAlpha }}
      {{- $a := get $accounts $key }}
      - secret:
          name: {{ $a.existingSecret }}
          optional: true
          items:
            {{- if eq (include "quader.upstreamAccountMode" $a) "basic" }}
            - key: {{ $a.usernameKey | default "username" | quote }}
              path: {{ $key }}/username
            {{- end }}
            - key: {{ $a.passwordKey | default "password" | quote }}
              path: {{ $key }}/password
      {{- end }}
{{- end }}
{{- end -}}

{{/* The volume mount of the accounts' files, read-only and without subPath; nothing without an entry. Argument: the root context. */}}
{{- define "quader.upstreamAccountsMount" -}}
{{- if .Values.secrets.upstreamAccounts }}
- name: upstream-accounts
  mountPath: {{ include "quader.upstreamAccountsDir" . }}
  readOnly: true
{{- end }}
{{- end -}}

{{/*
config.repositories.upstream_accounts, set on the dict "managed" of quader.config while an entry exists: per entry auth_mode, the two
paths below quader.upstreamAccountsDir (password_file alone for bearer) and secret_name, the Secret the admin page names. The default
renders nothing. Argument: dict "root" . "managed" <the managed dict>.
*/}}
{{- define "quader.upstreamAccountsManagedConfig" -}}
{{- $accounts := .root.Values.secrets.upstreamAccounts | default dict -}}
{{- if $accounts -}}
{{- $dir := include "quader.upstreamAccountsDir" .root -}}
{{- $out := dict -}}
{{- range $key, $a := $accounts -}}
{{- $mode := include "quader.upstreamAccountMode" $a -}}
{{- $e := dict "auth_mode" $mode "password_file" (printf "%s/%s/password" $dir $key) "secret_name" $a.existingSecret -}}
{{- if eq $mode "basic" -}}
{{- $_ := set $e "username_file" (printf "%s/%s/username" $dir $key) -}}
{{- end -}}
{{- $_ := set $out $key $e -}}
{{- end -}}
{{- $_ := set .managed "repositories" (dict "upstream_accounts" $out) -}}
{{- end -}}
{{- end -}}

{{/*
The checks of the accounts that the schema cannot state: config.repositories.upstream_accounts is the chart's to render, and a bearer
token goes without a username, so usernameKey is refused beside authMode bearer. The key's form, existingSecret and the mode are the
schema's; whether the key names a repository of the project upstream is checked by quader migrate, which fails naming it.
*/}}
{{- define "quader.validateUpstreamAccounts" -}}
{{- if hasKey (dig "repositories" (dict) .Values.config) "upstream_accounts" -}}
{{- fail "quader: config.repositories.upstream_accounts is rendered by the chart from secrets.upstreamAccounts and must not be set; name each account's Secret under secrets.upstreamAccounts" -}}
{{- end -}}
{{- range $key, $a := .Values.secrets.upstreamAccounts | default dict -}}
{{- if and (eq (include "quader.upstreamAccountMode" $a) "bearer") (hasKey $a "usernameKey") -}}
{{- fail (printf "quader: secrets.upstreamAccounts.%s.usernameKey is set with authMode bearer; a bearer token is sent without a username, so leave usernameKey out" $key) -}}
{{- end -}}
{{- end -}}
{{- end -}}
