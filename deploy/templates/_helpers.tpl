{{- define "application.pod" -}}
{{- $root := .root -}}
{{- $image := .workload.image | default $root.Values.image -}}
automountServiceAccountToken: {{ if .workload.serviceAccount }}true{{ else }}false{{ end }}
terminationGracePeriodSeconds: {{ .workload.terminationGracePeriodSeconds | default 90 }}
{{- with .workload.serviceAccount }}
serviceAccountName: {{ . }}
{{- end }}
securityContext:
  runAsNonRoot: true
  runAsUser: 10001
  runAsGroup: 10001
  fsGroup: 10001
  seccompProfile:
    type: RuntimeDefault
{{- if $root.Values.image.pullSecret }}
imagePullSecrets:
  - name: {{ $root.Values.image.pullSecret }}
{{- end }}
containers:
  - name: {{ .name }}
    image: {{ printf "%s@%s" $image.repository (required "An immutable image digest is required" $image.digest) | quote }}
    imagePullPolicy: {{ $image.pullPolicy | default "IfNotPresent" }}
    {{- with .workload.command }}
    command: {{ toJson . }}
    {{- end }}
    {{- with .workload.args }}
    args: {{ toJson . }}
    {{- end }}
    {{- with .workload.envFrom }}
    envFrom: {{ toJson . }}
    {{- end }}
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: [ALL]
    resources:
      requests: {{ toJson .workload.requests }}
      limits: {{ toJson .workload.limits }}
    env:
      - {name: APP_NAME, value: {{ $root.Values.app | quote }}}
      - {name: APP_ENVIRONMENT, value: {{ $root.Values.environment | quote }}}
      - {name: WORKLOAD_ROLE, value: {{ .workload.role | quote }}}
      - {name: RELEASE_VERSION, value: {{ $root.Values.releaseVersion | quote }}}
      - {name: OTEL_EXPORTER_OTLP_ENDPOINT, value: {{ $root.Values.telemetryEndpoint | quote }}}
      {{- if and $root.Values.database.enabled .workload.database }}
      - {name: PGHOST, value: {{ $root.Values.database.host | quote }}}
      - {name: PGDATABASE, value: {{ $root.Values.database.name | quote }}}
      - name: PGUSER
        valueFrom:
          secretKeyRef: {name: {{ $root.Values.database.secret }}, key: username}
      - name: PGPASSWORD
        valueFrom:
          secretKeyRef: {name: {{ $root.Values.database.secret }}, key: password}
      {{- end }}
    {{- range $key, $value := .workload.env }}
      - {name: {{ $key | quote }}, value: {{ $value | toString | quote }}}
    {{- end }}
    {{- range $key, $ref := .workload.secretEnv }}
      - name: {{ $key | quote }}
        valueFrom:
          secretKeyRef: {name: {{ $ref.name | quote }}, key: {{ $ref.key | quote }}}
    {{- end }}
    {{- if .workload.http }}
    ports:
      - {name: http, containerPort: {{ .workload.port | default 8080 }}}
    startupProbe:
      httpGet: {path: {{ .workload.healthPath | default "/healthz" }}, port: http}
      failureThreshold: 30
      periodSeconds: 5
    readinessProbe:
      httpGet: {path: {{ .workload.readyPath | default "/readyz" }}, port: http}
      periodSeconds: 10
    livenessProbe:
      httpGet: {path: {{ .workload.healthPath | default "/healthz" }}, port: http}
      periodSeconds: 20
    {{- else if .workload.livenessProbe }}
    livenessProbe: {{ toJson .workload.livenessProbe }}
    {{- else if and $root.Values.database.enabled (eq .workload.role "worker") (not .workload.disableLegacyProbe) }}
    livenessProbe:
      exec:
        command:
          - python
          - -c
          - "import os,psycopg;c=psycopg.connect(host=os.environ['PGHOST'],dbname=os.environ['PGDATABASE'],user=os.environ['PGUSER'],password=os.environ['PGPASSWORD'],connect_timeout=5);assert c.execute('SELECT 1').fetchone()[0]==1;c.close()"
      timeoutSeconds: 7
      periodSeconds: 30
      failureThreshold: 3
    {{- end }}
    volumeMounts:
      - {name: tmp, mountPath: /tmp}
      {{- with .workload.volumeMounts }}
      {{- toYaml . | nindent 6 }}
      {{- end }}
volumes:
  - name: tmp
    emptyDir:
      sizeLimit: 128Mi
  {{- with .workload.volumes }}
  {{- toYaml . | nindent 2 }}
  {{- end }}
{{- end -}}
