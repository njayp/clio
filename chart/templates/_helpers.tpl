{{- define "clio.fullname" -}}
{{ .Release.Name }}-clio
{{- end }}

{{- define "clio.labels" -}}
app.kubernetes.io/name: clio
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "clio.selectorLabels" -}}
app.kubernetes.io/name: clio
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
