{{- define "mailcatcher.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mailcatcher.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "mailcatcher.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "mailcatcher.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "mailcatcher.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mailcatcher.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "mailcatcher.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "mailcatcher.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "mailcatcher.sessionSecretName" -}}
{{- default (printf "%s-session" (include "mailcatcher.fullname" .)) .Values.sessionKey.secretName }}
{{- end }}

{{/* Name of the TLS Secret mounted into the pod, empty when none is configured. */}}
{{- define "mailcatcher.tlsSecretName" -}}
{{- if .Values.tls.certManager.enabled }}{{ include "mailcatcher.fullname" . }}-tls
{{- else }}{{ .Values.tls.secretName }}{{ end }}
{{- end }}

{{/* Reject configurations that cannot work, with a message that says how to fix it. */}}
{{- define "mailcatcher.validate" -}}
{{- if and (not .Values.oidc.issuerUrl) (not .Values.authDisabled) }}
{{- fail "configure oidc.issuerUrl/oidc.clientId, or set authDisabled=true to run without authentication" }}
{{- end }}
{{- if and .Values.imap.enabled (not .Values.mail.hostname) }}
{{- fail "imap.enabled requires mail.hostname (the public DNS name of the mail Service)" }}
{{- end }}
{{- if and .Values.imap.enabled (not .Values.oidc.issuerUrl) }}
{{- fail "imap.enabled requires oidc.issuerUrl: users create their IMAP login after signing in" }}
{{- end }}
{{- if and .Values.gateway.enabled (not .Values.gateway.host) }}
{{- fail "gateway.host is required when gateway.enabled=true (or set gateway.enabled=false)" }}
{{- end }}
{{- if and .Values.oidc.issuerUrl (not .Values.oidc.clientId) }}
{{- fail "oidc.clientId is required when oidc.issuerUrl is set" }}
{{- end }}
{{- if and .Values.oidc.issuerUrl (not .Values.gateway.host) }}
{{- fail "oidc needs gateway.host: it forms the redirect URL https://<host>/auth/callback" }}
{{- end }}
{{- if and .Values.tls.certManager.enabled (not .Values.tls.certManager.issuerRef.name) }}
{{- fail "tls.certManager.issuerRef.name is required when tls.certManager.enabled=true" }}
{{- end }}
{{- if and .Values.tls.certManager.enabled (not (or .Values.tls.certManager.dnsNames .Values.mail.hostname)) }}
{{- fail "tls.certManager needs mail.hostname or tls.certManager.dnsNames" }}
{{- end }}
{{- end }}
