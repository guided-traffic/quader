{{/*
The Secret mounts of a pod (R-HA-030) as a YAML list. Argument: dict "root" . "scope" "runtime"
(the pods of the roles), "migrate" (the migration Job), "backup" (the backup CronJob's pods) or "restore" (the
restore Job's pod) or "post-restore" (the hook Job after a platform restore: the database login of the migration Job, the blob store, and the journal credential). Each entry names one existing Secret,
the keys that are projected and the file name each key gets. Secrets mount read-only with
defaultMode 0440, with items and without subPath, so a rotated Secret reaches the files.
Secrets never become environment variables. The login secrets (secrets.oidc, secrets.breakGlass) are
mounted into the pods of role api (the main Deployment, its pools and its endpoint pools); never into packages,
worker or a Job. The target of the backup (secrets.backupTarget,
secrets.backupTargetCA) mounts into the backup and restore pods only (R-HA-060, R-BAK-011); Valkey into none of the Jobs
(R-BAK-012, R-BAK-022); the key ring into the replicas and the restore Job, which checks that it opens every key ID of the
backup (R-BAK-020).
*/}}
{{- define "quader.secretMounts" -}}
{{- $s := .root.Values.secrets -}}
{{- $out := list -}}
{{- $dbSecret := $s.database -}}
{{- if and (has .scope (list "migrate" "restore" "post-restore")) $s.databaseMigration.existingSecret -}}
{{- $dbSecret = $s.databaseMigration -}}
{{- else if and (eq .scope "backup") $s.databaseBackup.existingSecret -}}
{{- $dbSecret = $s.databaseBackup -}}
{{- end -}}
{{- $job := has .scope (list "backup" "restore") -}}
{{- $ioJob := has .scope (list "backup" "restore" "post-restore") -}}
{{- $out = append $out (dict "name" "database" "secret" $dbSecret.existingSecret "path" "/var/run/secrets/quader/database" "items" (list (dict "key" $dbSecret.usernameKey "path" "username") (dict "key" $dbSecret.passwordKey "path" "password"))) -}}
{{- if $s.databaseCA.existingSecret -}}
{{- $out = append $out (dict "name" "database-ca" "secret" $s.databaseCA.existingSecret "path" "/var/run/secrets/quader/database-ca" "items" (list (dict "key" $s.databaseCA.key "path" "ca.crt"))) -}}
{{- end -}}
{{- /* The trusted CA bundle (R-HA-043) reaches every pod that reads the shared configuration file, because its paths are in it: the roles and all Jobs. A public certificate, never a key. */ -}}
{{- $ca := .root.Values.trustedCA -}}
{{- if or $ca.existingSecret $ca.existingConfigMap -}}
{{- $caItems := list -}}
{{- range $ca.keys -}}
{{- $caItems = append $caItems (dict "key" . "path" .) -}}
{{- end -}}
{{- if $ca.existingSecret -}}
{{- $out = append $out (dict "name" "trusted-ca" "secret" $ca.existingSecret "path" "/etc/quader/ca" "items" $caItems) -}}
{{- else -}}
{{- $out = append $out (dict "name" "trusted-ca" "configMap" $ca.existingConfigMap "path" "/etc/quader/ca" "items" $caItems) -}}
{{- end -}}
{{- end -}}
{{- if $s.databaseClientCert.existingSecret -}}
{{- $out = append $out (dict "name" "database-client" "secret" $s.databaseClientCert.existingSecret "path" "/var/run/secrets/quader/database-client" "items" (list (dict "key" $s.databaseClientCert.certKey "path" "tls.crt") (dict "key" $s.databaseClientCert.keyKey "path" "tls.key"))) -}}
{{- end -}}
{{- if or (eq .scope "runtime") (eq .scope "restore") -}}
{{- /* The key ring is mounted into replicas and the restore Job only: the migration Job and the backup pods never seal or open a value (R-HA-030, R-BAK-012). */ -}}
{{- $encItems := list -}}
{{- range $s.encryption.keys -}}
{{- $encItems = append $encItems (dict "key" . "path" .) -}}
{{- end -}}
{{- $out = append $out (dict "name" "encryption" "secret" $s.encryption.existingSecret "path" "/var/run/secrets/quader/encryption" "items" $encItems) -}}
{{- end -}}
{{- if or (eq .scope "runtime") $ioJob -}}
{{- if and (eq .scope "backup") (eq (dig "storage" "s3" "credentials" "source" "" .root.Values.config) "static") -}}
{{- /* The backup reads the blob bucket and never writes or deletes there (R-BAK-015): it gets the read-only login of secrets.backupSource, not the runtime credential (Q-HA-160). */ -}}
{{- $out = append $out (dict "name" "backup-source" "secret" $s.backupSource.existingSecret "path" "/var/run/secrets/quader/backup-source" "items" (list (dict "key" $s.backupSource.accessKeyIdKey "path" "access-key-id") (dict "key" $s.backupSource.secretAccessKeyKey "path" "secret-access-key"))) -}}
{{- else if eq (dig "storage" "s3" "credentials" "source" "" .root.Values.config) "static" -}}
{{- $out = append $out (dict "name" "s3" "secret" $s.s3.existingSecret "path" "/var/run/secrets/quader/s3" "items" (list (dict "key" $s.s3.accessKeyIdKey "path" "access-key-id") (dict "key" $s.s3.secretAccessKeyKey "path" "secret-access-key"))) -}}
{{- end -}}
{{- if $s.s3CA.existingSecret -}}
{{- $out = append $out (dict "name" "s3-ca" "secret" $s.s3CA.existingSecret "path" "/var/run/secrets/quader/s3-ca" "items" (list (dict "key" $s.s3CA.key "path" "ca.crt"))) -}}
{{- end -}}
{{- end -}}
{{- if $job -}}
{{- /* The backup target: its keys and its CA reach the Jobs that write or read it and no replica (R-HA-060, R-BAK-011). */ -}}
{{- if eq (dig "backup" "target" "credentials" "source" "default" .root.Values.config) "static" -}}
{{- $tItems := list (dict "key" $s.backupTarget.accessKeyIdKey "path" "access-key-id") (dict "key" $s.backupTarget.secretAccessKeyKey "path" "secret-access-key") -}}
{{- if $s.backupTarget.sessionTokenKey -}}
{{- $tItems = append $tItems (dict "key" $s.backupTarget.sessionTokenKey "path" "session-token") -}}
{{- end -}}
{{- $out = append $out (dict "name" "backup-target" "secret" $s.backupTarget.existingSecret "path" "/var/run/secrets/quader/backup-target" "items" $tItems) -}}
{{- end -}}
{{- if $s.backupTargetCA.existingSecret -}}
{{- $out = append $out (dict "name" "backup-target-ca" "secret" $s.backupTargetCA.existingSecret "path" "/var/run/secrets/quader/backup-target-ca" "items" (list (dict "key" $s.backupTargetCA.key "path" "ca.crt"))) -}}
{{- end -}}
{{- end -}}
{{- $jcSource := "runtime" -}}
{{- if eq .scope "post-restore" -}}{{- $jcSource = .root.Values.postRestore.journalCredentials.source -}}{{- end -}}
{{- if eq .scope "restore" -}}{{- $jcSource = .root.Values.restore.journalCredentials.source -}}{{- end -}}
{{- if eq $jcSource "static" -}}
{{- /* The credential that lists object versions, for the journal replay of the post-restore and the restore Job only (R-HA-025 step 5, R-BAK-015, R-HA-023); the runtime credential deliberately lacks it. */ -}}
{{- $out = append $out (dict "name" "journal-reader" "secret" $s.journalReader.existingSecret "path" "/var/run/secrets/quader/journal-reader" "items" (list (dict "key" $s.journalReader.accessKeyIdKey "path" "access-key-id") (dict "key" $s.journalReader.secretAccessKeyKey "path" "secret-access-key"))) -}}
{{- end -}}
{{- if eq .scope "runtime" -}}
{{- $vkItems := list -}}
{{- if $s.valkey.usernameKey -}}
{{- $vkItems = append $vkItems (dict "key" $s.valkey.usernameKey "path" "username") -}}
{{- end -}}
{{- $vkItems = append $vkItems (dict "key" $s.valkey.passwordKey "path" "password") -}}
{{- $out = append $out (dict "name" "valkey" "secret" $s.valkey.existingSecret "path" "/var/run/secrets/quader/valkey" "items" $vkItems) -}}
{{- if $s.valkeySentinel.existingSecret -}}
{{- $snItems := list -}}
{{- if $s.valkeySentinel.usernameKey -}}
{{- $snItems = append $snItems (dict "key" $s.valkeySentinel.usernameKey "path" "username") -}}
{{- end -}}
{{- $snItems = append $snItems (dict "key" $s.valkeySentinel.passwordKey "path" "password") -}}
{{- $out = append $out (dict "name" "valkey-sentinel" "secret" $s.valkeySentinel.existingSecret "path" "/var/run/secrets/quader/valkey-sentinel" "items" $snItems) -}}
{{- end -}}
{{- if $s.valkeyCA.existingSecret -}}
{{- $out = append $out (dict "name" "valkey-ca" "secret" $s.valkeyCA.existingSecret "path" "/var/run/secrets/quader/valkey-ca" "items" (list (dict "key" $s.valkeyCA.key "path" "ca.crt"))) -}}
{{- end -}}
{{- if $s.valkeyClientCert.existingSecret -}}
{{- $out = append $out (dict "name" "valkey-client" "secret" $s.valkeyClientCert.existingSecret "path" "/var/run/secrets/quader/valkey-client" "items" (list (dict "key" $s.valkeyClientCert.certKey "path" "tls.crt") (dict "key" $s.valkeyClientCert.keyKey "path" "tls.key"))) -}}
{{- end -}}
{{- /* The login secrets belong to the pods of role api (R-HA-030, R-AUTH-014, R-AUTH-042, Q-HA-79): the main Deployment, its pools and its endpoint pools; never packages, worker or the migration Job. */ -}}
{{- if eq (.role | default "") "api" -}}
{{- range $s.oidc -}}
{{- $out = append $out (dict "name" (printf "oidc-%s" .providerId) "secret" .existingSecret "path" (printf "/var/run/secrets/quader/oidc-%s" .providerId) "items" (list (dict "key" (.key | default "client-secret") "path" "client-secret"))) -}}
{{- end -}}
{{- if $s.breakGlass.existingSecret -}}
{{- $out = append $out (dict "name" "break-glass" "secret" $s.breakGlass.existingSecret "path" "/var/run/secrets/quader/break-glass" "items" (list (dict "key" $s.breakGlass.key "path" "password"))) -}}
{{- end -}}
{{- end -}}
{{- if eq (dig "metrics" "auth" "" .root.Values.config) "static" -}}
{{- $out = append $out (dict "name" "metrics" "secret" $s.metricsToken.existingSecret "path" "/var/run/secrets/quader/metrics" "items" (list (dict "key" $s.metricsToken.key "path" "token"))) -}}
{{- end -}}
{{- /* Quader's signing identity (R-HA-071, signatures.md R-SIG-049): the pods of the kind worker alone, the main Deployment and its pools. */ -}}
{{- if and (eq (.role | default "") "worker") (include "quader.signingSource" .root) -}}
{{- $out = append $out (dict "name" "signing" "secret" (include "quader.signingSecret" .root) "path" (include "quader.signingDir" .root) "items" (list (dict "key" "tls.crt" "path" "tls.crt") (dict "key" "tls.key" "path" "tls.key") (dict "key" "ca.crt" "path" "ca.crt"))) -}}
{{- end -}}
{{- /*
TLS material of the listeners (R-HA-063, Q-HA-345): every pod of a role holds the pair of its kind (api, packages, worker), which
its operations listener, its client listener and every further endpoint without a pair of its own serve, and never the pair of
another kind; the Jobs serve no listener and hold none. ca.crt joins the pair, in the same volume so that the kubelet swaps both
together, only where a link verifies with it (R-HA-066, quader.linkNeedsCA). The client CA belongs to the client listener of the
default endpoint only.
*/ -}}
{{- $t := .root.Values.tls -}}
{{- if $t.enabled -}}
{{- $role := .role | default "" -}}
{{- $pool := .pool | default "" -}}
{{- $client := include "quader.opensDefault" (dict "role" $role "pool" $pool) | eq "true" -}}
{{- $items := list (dict "key" "tls.crt" "path" "tls.crt") (dict "key" "tls.key" "path" "tls.key") -}}
{{- if include "quader.linkNeedsCA" (dict "root" .root "role" $role "pool" $pool) -}}
{{- $items = append $items (dict "key" "ca.crt" "path" "ca.crt") -}}
{{- end -}}
{{- $out = append $out (dict "name" "tls" "secret" (include "quader.tlsSecret" (dict "root" .root "kind" $role)) "path" "/var/run/secrets/quader/tls" "items" $items) -}}
{{- if and $client $t.clientCA.secretName -}}
{{- $out = append $out (dict "name" "tls-client-ca" "secret" $t.clientCA.secretName "path" "/var/run/secrets/quader/tls-client-ca" "items" (list (dict "key" $t.clientCA.key "path" "ca.crt"))) -}}
{{- end -}}
{{- end -}}
{{- /* The Secrets of the endpoints this pod serves (R-EP-004, R-EP-008); worker pods and the Job serve none. */ -}}
{{- $out = concat $out (include "quader.endpointSecretMounts" (dict "root" .root "role" (.role | default "") "pool" (.pool | default "")) | fromYamlArray) -}}
{{- end -}}
{{- toYaml $out -}}
{{- end -}}

{{- define "quader.secretVolumes" -}}
{{- range fromYamlArray (include "quader.secretMounts" .) }}
- name: secret-{{ .name }}
  {{- if .configMap }}
  configMap:
    name: {{ .configMap }}
    defaultMode: 0440
    items:
    {{- range .items }}
    - key: {{ .key }}
      path: {{ .path }}
    {{- end }}
  {{- else }}
  secret:
    secretName: {{ .secret }}
    defaultMode: 0440
    items:
    {{- range .items }}
    - key: {{ .key }}
      path: {{ .path }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end -}}

{{- define "quader.secretVolumeMounts" -}}
{{- range fromYamlArray (include "quader.secretMounts" .) }}
- name: secret-{{ .name }}
  mountPath: {{ .path }}
  readOnly: true
{{- end }}
{{- end -}}
