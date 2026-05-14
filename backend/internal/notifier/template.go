package notifier

import (
	"bytes"
	"strings"
	"text/template"
)

// titleTemplates and bodyTemplates render an Event into channel-
// agnostic Subject + Body strings. Channels with richer markup
// (Slack blocks, Discord embeds) call renderTitle / renderBody for
// the default text and may format their own structures on top.
//
// One template per EventType keeps the per-event copy editable
// without a switch statement in every channel adapter.
var titleTemplates = map[EventType]*template.Template{
	EventRunSucceeded:    must("run-succ-t", `✅ Run succeeded: {{.PipelineName}}`),
	EventRunFailed:       must("run-fail-t", `❌ Run failed: {{.PipelineName}}`),
	EventRunCancelled:    must("run-cancel-t", `⛔ Run cancelled: {{.PipelineName}}`),
	EventDeploySucceeded: must("deploy-succ-t", `🚀 Deploy succeeded: {{.AppName}} → {{.Environment}}`),
	EventDeployFailed:    must("deploy-fail-t", `🔥 Deploy failed: {{.AppName}} → {{.Environment}}`),
	EventBuildFailed:     must("build-fail-t", `🔨 Build failed: {{.PipelineName}}`),
}

var bodyTemplates = map[EventType]*template.Template{
	EventRunSucceeded: must("run-succ-b", `Pipeline {{.PipelineName}} (run {{.RunID}}) completed successfully.
{{- if .URL}}
Details: {{.URL}}
{{- end}}`),
	EventRunFailed: must("run-fail-b", `Pipeline {{.PipelineName}} (run {{.RunID}}) failed.
{{- if .Error}}
Error: {{.Error}}
{{- end}}
{{- if .URL}}
Details: {{.URL}}
{{- end}}`),
	EventRunCancelled: must("run-cancel-b", `Pipeline {{.PipelineName}} (run {{.RunID}}) was cancelled.
{{- if .URL}}
Details: {{.URL}}
{{- end}}`),
	EventDeploySucceeded: must("deploy-succ-b", `App {{.AppName}} deployed to {{.Environment}} successfully.
{{- if .URL}}
Details: {{.URL}}
{{- end}}`),
	EventDeployFailed: must("deploy-fail-b", `Deploy of {{.AppName}} to {{.Environment}} failed.
{{- if .Error}}
Error: {{.Error}}
{{- end}}
{{- if .URL}}
Details: {{.URL}}
{{- end}}`),
	EventBuildFailed: must("build-fail-b", `Build for {{.PipelineName}} (run {{.RunID}}) failed.
{{- if .Error}}
Error: {{.Error}}
{{- end}}
{{- if .URL}}
Details: {{.URL}}
{{- end}}`),
}

// renderTitle returns Title() for an Event. A missing template
// falls back to the EventType string so an unknown event type still
// produces something human-readable on the wire.
func renderTitle(e Event) string {
	t, ok := titleTemplates[e.Type]
	if !ok {
		return fallbackText(e, string(e.Type))
	}
	return execTemplate(t, e)
}

func renderBody(e Event) string {
	t, ok := bodyTemplates[e.Type]
	if !ok {
		return fallbackText(e, "")
	}
	return execTemplate(t, e)
}

func execTemplate(t *template.Template, e Event) string {
	var buf bytes.Buffer
	if err := t.Execute(&buf, e); err != nil {
		// Template authors are package-internal; an Execute error
		// means a programmer bug we want to surface immediately
		// during dev. Fall back to a safe placeholder in prod.
		return fallbackText(e, t.Name()+": template error")
	}
	return strings.TrimSpace(buf.String())
}

func fallbackText(e Event, hint string) string {
	parts := []string{string(e.Type)}
	if hint != "" {
		parts = append(parts, hint)
	}
	if e.PipelineName != "" {
		parts = append(parts, "pipeline="+e.PipelineName)
	}
	if e.AppName != "" {
		parts = append(parts, "app="+e.AppName)
	}
	return strings.Join(parts, " ")
}

func must(name, src string) *template.Template {
	return template.Must(template.New(name).Parse(src))
}
