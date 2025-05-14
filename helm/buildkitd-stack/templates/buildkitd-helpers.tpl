{{/*
Common labels for buildkitd component
*/}}
{{- define "buildkitd-stack.buildkitd.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name "buildkitd" | trunc 63 | trimSuffix "-" }}
{{ include "buildkitd-stack.buildkitd.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: buildkitd
{{- end -}}

{{/*
Selector labels for buildkitd component
*/}}
{{- define "buildkitd-stack.buildkitd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "buildkitd-stack.buildkitd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Name for the buildkitd component (e.g., "buildkitd")
*/}}
{{- define "buildkitd-stack.buildkitd.name" -}}
{{- default "buildkitd" .Values.buildkitd.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Full name for the buildkitd component (e.g., "myrelease-buildkitd")
*/}}
{{- define "buildkitd-stack.buildkitd.fullname" -}}
{{- if .Values.buildkitd.fullnameOverride -}}
{{- .Values.buildkitd.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "buildkitd-stack.buildkitd.name" . -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Service account name for buildkitd component
This might not be used by the original buildkitd chart but is good practice if needed.
If not used, this can be removed later.
*/}}
{{- define "buildkitd-stack.buildkitd.serviceAccountName" -}}
{{- if .Values.buildkitd.serviceAccount.create -}}
    {{- default (include "buildkitd-stack.buildkitd.fullname" .) .Values.buildkitd.serviceAccount.name }}
{{- else -}}
    {{- default "default" .Values.buildkitd.serviceAccount.name }}
{{- end -}}
{{- end -}}