{{/*
The signatures on artifacts in the chart (signatures.md R-SIG-018, R-SIG-039): the trust anchors declared in signatures.anchors, rendered
into config.signatures.anchors of the configuration file, the ConfigMap keys their material comes from, mounted read-only into the
migration Job that applies them (quader migrate), and the switch the alerts follow. The material is public (certificates, public keys, a
Sigstore trusted root), so it comes inline or from a ConfigMap, never from a Secret.
*/}}

{{/* "true" while config.signatures.enabled (the default): the alerts of the signatures render. */}}
{{- define "quader.signaturesOn" -}}
{{- if dig "signatures" "enabled" true .Values.config -}}true{{- end -}}
{{- end -}}

{{/* The directory the migration Job finds the material of the declared anchors in: <dir>/<anchor key>/<file>. */}}
{{- define "quader.signatureAnchorsDir" -}}/etc/quader/signature-anchors{{- end -}}

{{/* The declared anchors of the values, a list (empty by default). Argument: the root context. */}}
{{- define "quader.signatureAnchorList" -}}
{{- toYaml (dig "anchors" (list) (.Values.signatures | default dict)) -}}
{{- end -}}

{{/*
The files the migration Job mounts, one per material that names a configMapKeyRef: dict name (the ConfigMap), key (its key) and path
(<anchor key>/<file> below quader.signatureAnchorsDir). The file names are fixed per member: certificates.pem, keys.asc,
key-<index>.pem and trusted_root.json. Argument: the root context.
*/}}
{{- define "quader.signatureAnchorFiles" -}}
{{- $out := list -}}
{{- range $a := include "quader.signatureAnchorList" . | fromYamlArray -}}
{{- $refs := list -}}
{{- with $a.ca -}}{{- with .certificatesFrom -}}{{- $refs = append $refs (dict "ref" .configMapKeyRef "file" "certificates.pem") -}}{{- end -}}{{- end -}}
{{- with $a.openpgp -}}{{- with .keysFrom -}}{{- $refs = append $refs (dict "ref" .configMapKeyRef "file" "keys.asc") -}}{{- end -}}{{- end -}}
{{- with $a.publicKey -}}
{{- range $i, $k := .keys -}}
{{- with $k.pemFrom -}}{{- $refs = append $refs (dict "ref" .configMapKeyRef "file" (printf "key-%d.pem" $i)) -}}{{- end -}}
{{- end -}}
{{- end -}}
{{- with $a.sigstore -}}{{- with .trustedRootFrom -}}{{- $refs = append $refs (dict "ref" .configMapKeyRef "file" "trusted_root.json") -}}{{- end -}}{{- end -}}
{{- range $refs -}}
{{- $out = append $out (dict "name" .ref.name "key" .ref.key "path" (printf "%s/%s" $a.key .file)) -}}
{{- end -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{/*
config.signatures.anchors as the binary reads it (internal/config/signatures.go): a map keyed by the anchor's key, the members in
snake_case, a material from a ConfigMap as the path of its mounted file (*_file). Empty without declared anchors. Argument: the root
context.
*/}}
{{- define "quader.signatureAnchorsConfig" -}}
{{- $dir := include "quader.signatureAnchorsDir" . -}}
{{- $out := dict -}}
{{- range $a := include "quader.signatureAnchorList" . | fromYamlArray -}}
{{- $e := dict "title" $a.title "kind" $a.kind -}}
{{- $at := printf "%s/%s" $dir $a.key -}}
{{- with $a.ca -}}
{{- $b := dict -}}
{{- if .certificatesFrom -}}{{- $_ := set $b "certificates_file" (printf "%s/certificates.pem" $at) -}}{{- else if .certificates -}}{{- $_ := set $b "certificates" .certificates -}}{{- end -}}
{{- with .use -}}{{- $_ := set $b "use" . -}}{{- end -}}
{{- with .subject -}}{{- $_ := set $b "subject" . -}}{{- end -}}
{{- with .subjectAltNames -}}{{- $_ := set $b "subject_alt_names" . -}}{{- end -}}
{{- $_ := set $e "ca" $b -}}
{{- end -}}
{{- with $a.openpgp -}}
{{- $b := dict -}}
{{- if .keysFrom -}}{{- $_ := set $b "keys_file" (printf "%s/keys.asc" $at) -}}{{- else if .keys -}}{{- $_ := set $b "keys" .keys -}}{{- end -}}
{{- $_ := set $e "openpgp" $b -}}
{{- end -}}
{{- with $a.publicKey -}}
{{- $keys := list -}}
{{- range $i, $k := .keys -}}
{{- $b := dict -}}
{{- if $k.pemFrom -}}{{- $_ := set $b "pem_file" (printf "%s/key-%d.pem" $at $i) -}}{{- else if $k.pem -}}{{- $_ := set $b "pem" $k.pem -}}{{- end -}}
{{- with $k.keyid -}}{{- $_ := set $b "keyid" . -}}{{- end -}}
{{- with $k.expires -}}{{- $_ := set $b "expires" . -}}{{- end -}}
{{- $keys = append $keys $b -}}
{{- end -}}
{{- $_ := set $e "public_key" (dict "keys" $keys) -}}
{{- end -}}
{{- with $a.sigstore -}}
{{- $b := dict "issuer" (.issuer | default "") -}}
{{- with .identity -}}{{- $_ := set $b "identity" . -}}{{- end -}}
{{- with .identityPattern -}}{{- $_ := set $b "identity_pattern" . -}}{{- end -}}
{{- with .trustRoot -}}{{- $_ := set $b "trust_root" . -}}{{- end -}}
{{- if .trustedRootFrom -}}{{- $_ := set $b "trusted_root_file" (printf "%s/trusted_root.json" $at) -}}{{- else if .trustedRoot -}}{{- $_ := set $b "trusted_root" .trustedRoot -}}{{- end -}}
{{- $_ := set $e "sigstore" $b -}}
{{- end -}}
{{- $_ := set $out $a.key $e -}}
{{- end -}}
{{- if $out -}}{{- toYaml $out -}}{{- end -}}
{{- end -}}

{{/*
The managed key of the declared anchors, set on the dict "managed" of quader.config while signatures.anchors declares any; the
default renders nothing. Argument: dict "root" . "managed" <the managed dict>.
*/}}
{{- define "quader.signaturesManagedConfig" -}}
{{- $anchors := include "quader.signatureAnchorsConfig" .root | fromYaml -}}
{{- if $anchors -}}
{{- $_ := set .managed "signatures" (dict "anchors" $anchors) -}}
{{- end -}}
{{- end -}}

{{/* The volume mount of the anchors' files in the migration Job's container; nothing without a configMapKeyRef. */}}
{{- define "quader.signatureAnchorsMount" -}}
{{- if include "quader.signatureAnchorFiles" . | fromYamlArray -}}
- name: signature-anchors
  mountPath: {{ include "quader.signatureAnchorsDir" . }}
  readOnly: true
{{- end -}}
{{- end -}}

{{/*
The projected volume of the anchors' files: one source per file, so two anchors may read keys of the same ConfigMap. Mounted without
subPath, read-only, with the mode of the chart's other mounted material.
*/}}
{{- define "quader.signatureAnchorsVolume" -}}
{{- $files := include "quader.signatureAnchorFiles" . | fromYamlArray -}}
{{- if $files -}}
- name: signature-anchors
  projected:
    defaultMode: 0440
    sources:
      {{- range $files }}
      - configMap:
          name: {{ .name }}
          items:
            - key: {{ .key | quote }}
              path: {{ .path }}
      {{- end }}
{{- end -}}
{{- end -}}

{{/*
The checks of the declared anchors that the schema cannot state (R-SIG-018): a key appears once, and config.signatures.anchors is the
chart's to render. The shape of each entry (the key's pattern, the title, the kind with exactly its block, the material inline or from a
configMapKeyRef) is the schema's; the material itself is checked by quader migrate (R-SIG-015).
*/}}
{{- define "quader.validateSignatures" -}}
{{- if hasKey (dig "signatures" (dict) .Values.config) "anchors" -}}
{{- fail "quader: config.signatures.anchors is rendered by the chart from signatures.anchors and must not be set; declare the anchors under signatures.anchors" -}}
{{- end -}}
{{- $seen := dict -}}
{{- range $i, $a := include "quader.signatureAnchorList" . | fromYamlArray -}}
{{- if hasKey $seen $a.key -}}
{{- fail (printf "quader: signatures.anchors[%d].key %q is also the key of signatures.anchors[%d]; an anchor's key is unique" $i $a.key (get $seen $a.key)) -}}
{{- end -}}
{{- $_ := set $seen $a.key $i -}}
{{- end -}}
{{- end -}}
