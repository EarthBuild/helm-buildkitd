{{/*
Annotations for autoscaler component
*/}}
{{- define "buildkitd-stack.autoscaler.annotations" -}}
{{- if .Values.autoscaler.annotations }}
{{ .Values.autoscaler.annotations | toYaml }}
{{- else }}
{}
{{- end }}
{{- end -}}

{{/*
Common labels for autoscaler component
*/}}
{{- define "buildkitd-stack.autoscaler.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name "autoscaler" | trunc 63 | trimSuffix "-" }}
{{ include "buildkitd-stack.autoscaler.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: autoscaler
{{- end -}}

{{/*
Selector labels for autoscaler component
*/}}
{{- define "buildkitd-stack.autoscaler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "buildkitd-stack.autoscaler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Name for the autoscaler component (e.g., "autoscaler")
*/}}
{{- define "buildkitd-stack.autoscaler.name" -}}
{{- default "autoscaler" .Values.autoscaler.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Full name for the autoscaler component (e.g., "myrelease-autoscaler")
*/}}
{{- define "buildkitd-stack.autoscaler.fullname" -}}
{{- if .Values.autoscaler.fullnameOverride -}}
{{- .Values.autoscaler.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "buildkitd-stack.autoscaler.name" . -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Service account name for autoscaler component
*/}}
{{- define "buildkitd-stack.autoscaler.serviceAccountName" -}}
{{- if .Values.autoscaler.serviceAccount.create -}}
    {{- default (include "buildkitd-stack.autoscaler.fullname" .) .Values.autoscaler.serviceAccount.name }}
{{- else -}}
    {{- default "default" .Values.autoscaler.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Define the name of the buildkitd headless service that the autoscaler will target.
This will be created in a subsequent step.
*/}}
{{- define "buildkitd-stack.buildkitd.headlessServiceName" -}}
{{- printf "%s-headless" (include "buildkitd-stack.buildkitd.fullname" .) -}}
{{- end -}}