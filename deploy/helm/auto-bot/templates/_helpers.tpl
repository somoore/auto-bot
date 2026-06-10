{{- define "auto-bot.name" -}}auto-bot{{- end -}}
{{- define "auto-bot.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- define "auto-bot.labels" -}}
app.kubernetes.io/name: auto-bot
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}
{{- define "auto-bot.selectorLabels" -}}
app.kubernetes.io/name: auto-bot
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
