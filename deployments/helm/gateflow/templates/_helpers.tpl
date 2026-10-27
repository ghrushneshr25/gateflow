{{- define "gateflow.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "gateflow.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "gateflow.labels" -}}
app.kubernetes.io/name: {{ include "gateflow.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "gateflow.postgresHost" -}}
{{ include "gateflow.fullname" . }}-postgres
{{- end -}}

{{- define "gateflow.kafkaHost" -}}
{{ include "gateflow.fullname" . }}-kafka
{{- end -}}

{{- define "gateflow.dbURL" -}}
{{- $host := include "gateflow.postgresHost" . -}}
postgres://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ $host }}:5432/{{ .Values.postgres.database }}?sslmode=disable
{{- end -}}
