{{/*
One Deployment: a role (R-HA-002), a pool (R-HA-062) or an endpoint pool (R-EP-008). Argument: dict
"root" . "unit" <one entry of quader.units>.
*/}}
{{- define "quader.deployment" -}}
{{- $ := .root }}
{{- $unit := .unit }}
{{- $role := $unit.role }}
{{- $pool := $unit.pool }}
{{- $name := $unit.name }}
{{- $r := $unit.r }}
{{- $ctx := dict "root" $ "role" $role "r" $r "pool" $pool }}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "quader.fullname" $ }}-{{ $name }}
  labels:
    {{- include "quader.labels" (dict "root" $ "component" $role "pool" $pool) | nindent 4 }}
  {{- with (include "quader.commonAnnotations" $) }}
  annotations:
    {{- . | nindent 4 }}
  {{- end }}
spec:
  {{- /*
  With an autoscaler the field carries the live replicas of the Deployment, so helm upgrade neither resets
  the autoscaler's choice nor lets the API server default the field to 1 when autoscaling is switched on
  (R-HA-032, Q-HA-91). lookup returns nothing under helm template and on the first install; the field is
  then left out.
  */}}
  {{- if not (dig "autoscaling" "enabled" false $r) }}
  replicas: {{ $r.replicaCount }}
  {{- else }}
  {{- $live := lookup "apps/v1" "Deployment" $.Release.Namespace (printf "%s-%s" (include "quader.fullname" $) $name) }}
  {{- if $live }}
  replicas: {{ int (dig "spec" "replicas" 1 $live) }}
  {{- end }}
  {{- end }}
  minReadySeconds: {{ $r.minReadySeconds }}
  revisionHistoryLimit: {{ $r.revisionHistoryLimit }}
  strategy:
    {{- toYaml $r.strategy | nindent 4 }}
  selector:
    matchLabels:
      {{- include "quader.selectorLabels" (dict "root" $ "component" $role "pool" $pool) | nindent 6 }}
  template:
    metadata:
      annotations:
        checksum/config: {{ include "quader.config" $ | sha256sum }}
        checksum/encryption-keys: {{ $.Values.secrets.encryption.keys | toJson | sha256sum }}
        {{- with $r.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      labels:
        {{- include "quader.labels" (dict "root" $ "component" $role "pool" $pool) | nindent 8 }}
        quader/serve: "true"
        {{- with $r.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      serviceAccountName: {{ include "quader.serviceAccountName" $ }}
      automountServiceAccountToken: {{ $.Values.serviceAccount.automountServiceAccountToken }}
      enableServiceLinks: false
      terminationGracePeriodSeconds: {{ $r.terminationGracePeriodSeconds }}
      {{- include "quader.imagePullSecrets" $ | nindent 6 }}
      securityContext:
        {{- toYaml $.Values.podSecurityContext | nindent 8 }}
      {{- with $r.priorityClassName }}
      priorityClassName: {{ . }}
      {{- end }}
      {{- with $r.schedulerName }}
      schedulerName: {{ . }}
      {{- end }}
      {{- with $r.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $r.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      tolerations:
        - key: node.kubernetes.io/not-ready
          operator: Exists
          effect: NoExecute
          tolerationSeconds: {{ $r.nodeFailureTolerationSeconds }}
        - key: node.kubernetes.io/unreachable
          operator: Exists
          effect: NoExecute
          tolerationSeconds: {{ $r.nodeFailureTolerationSeconds }}
        {{- with $r.tolerations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      {{- include "quader.topologySpread" $ctx | nindent 6 }}
      containers:
        - name: quader
          image: {{ include "quader.image" $ | quote }}
          imagePullPolicy: {{ $.Values.image.pullPolicy }}
          args:
            - serve
            - --role
            - {{ $role }}
          securityContext:
            {{- toYaml $.Values.securityContext | nindent 12 }}
          ports:
            {{- if include "quader.opensDefault" $ctx }}
            - name: http
              containerPort: 8080
              protocol: TCP
            {{- end }}
            {{- /* The ports of the endpoints this pod serves (R-EP-008); no other pod listens on them. */}}
            {{- range $port := include "quader.unitEndpointPorts" $ctx | fromYamlArray }}
            - containerPort: {{ $port }}
              protocol: TCP
            {{- end }}
            {{- with (include "quader.scanListenPort" $ctx) }}
            {{- . | nindent 12 }}
            {{- end }}
            - name: ops
              containerPort: 8081
              protocol: TCP
          env:
            {{- include "quader.podEnv" $ | nindent 12 }}
            {{- if $pool }}
            - name: QUADER_POOL_NAME
              value: {{ $pool | quote }}
            {{- end }}
            {{- /* The queues a worker claims (R-HA-062): a pool only its own, the main Deployment all but the pools'. Lists travel as JSON (docs/developer/configuration.md). */}}
            {{- if $unit.queues }}
            - name: QUADER_JOBS_QUEUES
              value: {{ $unit.queues | toJson | quote }}
            {{- end }}
            {{- if $unit.excludeQueues }}
            - name: QUADER_JOBS_EXCLUDE_QUEUES
              value: {{ $unit.excludeQueues | toJson | quote }}
            {{- end }}
            {{- with (include "quader.s3EnvFromSecret" $) }}
            {{- . | nindent 12 }}
            {{- end }}
            {{- with (include "quader.scanListenEnv" $ctx) }}
            {{- . | nindent 12 }}
            {{- end }}
            - name: QUADER_RUNTIME_MEMORY_LIMIT_BYTES
              valueFrom:
                resourceFieldRef:
                  resource: limits.memory
                  divisor: "1"
            - name: QUADER_RUNTIME_TERMINATION_GRACE_PERIOD
              value: {{ printf "%ds" (int64 $r.terminationGracePeriodSeconds) | quote }}
            {{- if not (dig "storage" "spool" "max_bytes" "" $.Values.config) }}
            - name: QUADER_STORAGE_SPOOL_MAX_BYTES
              value: {{ include "quader.spoolBudget" $ctx | quote }}
            {{- end }}
            {{- if not (dig "limits" "cpu" "" $r.resources) }}
            - name: GOMAXPROCS
              valueFrom:
                resourceFieldRef:
                  resource: requests.cpu
                  divisor: "1"
            {{- end }}
            - name: TMPDIR
              value: /tmp
            {{- with $.Values.extraEnv }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
            {{- with $r.extraEnv }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
          {{- with $.Values.extraEnvFrom }}
          envFrom:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- include "quader.probes" $ | nindent 10 }}
          resources:
            {{- toYaml $r.resources | nindent 12 }}
          volumeMounts:
            - name: config
              mountPath: /etc/quader/quader.yaml
              subPath: quader.yaml
              readOnly: true
            - name: spool
              mountPath: /var/lib/quader/spool
            - name: tmp
              mountPath: /tmp
            {{- include "quader.secretVolumeMounts" (dict "root" $ "scope" "runtime" "role" $role "pool" $pool) | nindent 12 }}
            {{- with $.Values.extraVolumeMounts }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
      volumes:
        - name: config
          configMap:
            name: {{ include "quader.fullname" $ }}-config
        - name: spool
          {{- if eq $.Values.spool.type "ephemeral" }}
          ephemeral:
            volumeClaimTemplate:
              metadata:
                {{- /* The claim of a generic ephemeral volume gets the labels of this template, not the pod's (R-HA-048): kubectl get pvc -l app.kubernetes.io/instance=<release> lists one per pod. */}}
                labels:
                  {{- include "quader.labels" (dict "root" $ "component" $role "pool" $pool) | nindent 18 }}
              spec:
                accessModes:
                  {{- toYaml $.Values.spool.ephemeral.accessModes | nindent 18 }}
                {{- with $.Values.spool.ephemeral.storageClassName }}
                storageClassName: {{ . }}
                {{- end }}
                resources:
                  requests:
                    storage: {{ $.Values.spool.ephemeral.size }}
          {{- else }}
          emptyDir:
            sizeLimit: {{ include "quader.spoolSize" $ctx }}
            {{- with $.Values.spool.medium }}
            medium: {{ . }}
            {{- end }}
          {{- end }}
        - name: tmp
          emptyDir:
            sizeLimit: {{ $.Values.tmp.sizeLimit }}
        {{- include "quader.secretVolumes" (dict "root" $ "scope" "runtime" "role" $role "pool" $pool) | nindent 8 }}
        {{- with $.Values.extraVolumes }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
{{- end -}}
