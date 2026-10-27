{{- define "gateflow-obs.labels" -}}
app.kubernetes.io/name: gateflow-obs
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
