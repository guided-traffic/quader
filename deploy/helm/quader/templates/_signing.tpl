{{/*
Quader's signing identity in the chart (R-HA-071, signatures.md R-SIG-046, Q-HA-385): an existing Secret or the Secret of a cert-manager
Certificate the chart renders (signing-certificate.yaml), at most one; mounted into the pods of the kind worker alone (quader.secretMounts),
and its three files set in config.signing.identity of the configuration file. No pod of api, packages, a scanner, the updater or a Job
mounts it. The alerts of the identity follow config.signing.enabled.
*/}}

{{/* "true" while config.signing.enabled (the default): the four QuaderSigning* alerts render. */}}
{{- define "quader.signingOn" -}}
{{- if dig "signing" "enabled" true .Values.config -}}true{{- end -}}
{{- end -}}

{{/* The directory the worker pods mount the identity's Secret at. */}}
{{- define "quader.signingDir" -}}/var/run/secrets/quader/signing{{- end -}}

{{/* The identity's source: "secret" (signing.identity.existingSecret), "certManager" (an issuer is named) or "" (none). Argument: the root context. */}}
{{- define "quader.signingSource" -}}
{{- $id := (.Values.signing | default dict).identity | default dict -}}
{{- if $id.existingSecret -}}secret
{{- else if dig "certManager" "issuerRef" "name" "" $id -}}certManager
{{- end -}}
{{- end -}}

{{/* The Secret the worker pods mount: the existing one, or <fullname>-signing of the Certificate. Argument: the root context. */}}
{{- define "quader.signingSecret" -}}
{{- $id := (.Values.signing | default dict).identity | default dict -}}
{{- if $id.existingSecret -}}{{ $id.existingSecret }}{{- else -}}{{ include "quader.fullname" . }}-signing{{- end -}}
{{- end -}}

{{/*
config.signing.identity.cert_file, key_file and ca_file, set on the dict "managed" of quader.config while a source is named; the default
renders nothing. The files exist in the worker pods only; the configuration file is shared, and only the roles worker and all read
them (R-SIG-049). Argument: dict "root" . "managed" <the managed dict>.
*/}}
{{- define "quader.signingManagedConfig" -}}
{{- if include "quader.signingSource" .root -}}
{{- $dir := include "quader.signingDir" .root -}}
{{- $id := dict "cert_file" (printf "%s/tls.crt" $dir) "key_file" (printf "%s/tls.key" $dir) "ca_file" (printf "%s/ca.crt" $dir) -}}
{{- $_ := set .managed "signing" (dict "identity" $id) -}}
{{- end -}}
{{- end -}}

{{/*
The checks of the identity's values that the schema cannot state: at most one source, and the three files are the chart's to set (they
fail the render under config, as secrets.encryption_key_files does).
*/}}
{{- define "quader.validateSigning" -}}
{{- $id := (.Values.signing | default dict).identity | default dict -}}
{{- if and $id.existingSecret (dig "certManager" "issuerRef" "name" "" $id) -}}
{{- fail "quader: signing.identity.existingSecret and signing.identity.certManager.issuerRef.name are both set; the signing identity has one source: an existing Secret or a cert-manager Certificate" -}}
{{- end -}}
{{- range $key := list "cert_file" "key_file" "ca_file" -}}
{{- if hasKey (dig "signing" "identity" (dict) $.Values.config) $key -}}
{{- fail (printf "quader: config.signing.identity.%s is managed by the chart and must not be set; name the identity in signing.identity.existingSecret or signing.identity.certManager" $key) -}}
{{- end -}}
{{- end -}}
{{- end -}}
