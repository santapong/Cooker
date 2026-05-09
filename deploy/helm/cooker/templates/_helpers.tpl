{{- define "cooker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cooker.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "cooker.labels" -}}
app.kubernetes.io/name: {{ include "cooker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "cooker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cooker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
DB_PASSWORD + DATABASE_URL env block, rendered when database.host is set.
DB_PASSWORD must be declared before DATABASE_URL so Kubernetes' $(VAR)
interpolation resolves it. Shared by deployment.yaml and the retention
CronJob so both go through the same secret reference and sslmode wiring.
*/}}
{{- define "cooker.databaseUrlEnv" -}}
{{- if .Values.database.host }}
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.database.passwordSecretRef.name | quote }}
      key: {{ .Values.database.passwordSecretRef.key | quote }}
- name: DATABASE_URL
  value: "postgres://{{ .Values.database.username }}:$(DB_PASSWORD)@{{ .Values.database.host }}:{{ .Values.database.port }}/{{ .Values.database.name }}?sslmode={{ .Values.postgresql.sslMode }}"
{{- end }}
{{- end }}
