package triage

import (
	"fmt"
	"strings"

	"github.com/santapong/cooker/internal/model"
)

// logTailBytes caps how much of the stage log travels to the API.
// Failures sit at the end of a log, so the tail carries the signal;
// StageRun.Logs is already head-capped at 1 MiB by the executor.
const logTailBytes = 32 << 10

// systemPrompt pins the advisory contract: explain, suggest, never
// pretend to act. Kept short and stable.
const systemPrompt = "You are a CI/CD failure-triage assistant inside the Cooker pipeline tool. " +
	"Given a failed pipeline stage's configuration summary, error, and log tail, explain the most " +
	"probable root cause and suggest concrete next steps the operator can take. Be concise: a short " +
	"diagnosis paragraph, then a bulleted fix list. You cannot run commands or change anything — " +
	"advice only. If the logs are inconclusive, say so and list what to check next."

// BuildRequest composes the triage request for one failed stage.
// Config secrets never travel: Env values and SecretRefs are
// excluded; only structural fields (dockerfile, context, image,
// namespace, timeout, retry shape) are summarised.
func BuildRequest(stage *model.Stage, sr *model.StageRun) Request {
	var b strings.Builder
	if stage != nil {
		fmt.Fprintf(&b, "Stage: %q (type %s)\n", stage.Name, stage.Type)
		b.WriteString(configSummary(stage))
	}
	if sr != nil {
		if sr.Error != "" {
			fmt.Fprintf(&b, "Stage error: %s\n", sr.Error)
		}
		tail := sr.Logs
		if len(tail) > logTailBytes {
			tail = "…(truncated)…\n" + tail[len(tail)-logTailBytes:]
		}
		if tail != "" {
			fmt.Fprintf(&b, "\nLog tail:\n```\n%s\n```\n", tail)
		} else {
			b.WriteString("\n(no logs captured for this stage)\n")
		}
	}
	b.WriteString("\nWhat is the most likely cause, and what should the operator do next?")
	return Request{System: systemPrompt, Prompt: b.String()}
}

// configSummary renders the non-secret structural config fields.
func configSummary(stage *model.Stage) string {
	cfg := stage.Config
	var b strings.Builder
	add := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "  %s: %s\n", k, v)
		}
	}
	b.WriteString("Config:\n")
	add("dockerfile", cfg.Dockerfile)
	add("context", cfg.Context)
	add("tags", strings.Join(cfg.Tags, ", "))
	add("image", cfg.Image)
	add("repository", cfg.Repository)
	add("registry", cfg.Registry)
	add("namespace", cfg.Namespace)
	add("deployRuntime", cfg.DeployRuntime)
	add("timeout", cfg.Timeout)
	if cfg.Retries > 0 {
		add("retries", fmt.Sprintf("%d", cfg.Retries))
	}
	if len(cfg.Env) > 0 {
		// Names only — values may be sensitive even outside SecretRefs.
		keys := make([]string, 0, len(cfg.Env))
		for k := range cfg.Env {
			keys = append(keys, k)
		}
		add("env (names only)", strings.Join(keys, ", "))
	}
	if len(cfg.SecretRefs) > 0 {
		add("secretRefs (names only)", strings.Join(cfg.SecretRefs, ", "))
	}
	return b.String()
}
