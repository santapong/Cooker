package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
)

// clientVersion is the cookerctl build version. Overridable at build time
// via -ldflags "-X main.clientVersion=v1.2.3" to mirror the server binary.
var clientVersion = "v0.1.0-dev"

// pollInterval is how often `run --follow` re-reads the run status over
// REST. ~2s keeps the transition feed responsive without hammering the
// server; the run endpoint is a cheap single-row read.
const pollInterval = 2 * time.Second

// usage is the top-level help text. Kept terse; each subcommand prints its
// own flag help via -h.
const usage = `cookerctl — command-line client for a Cooker server

Usage:
  cookerctl [global flags] <command> [args]

Commands:
  version                                 Print client version and the server's
  pipelines list                          List pipelines
  pipelines export <id> [-o file]         Export a pipeline as YAML (stdout or file)
  pipelines import -f <file.yaml>          Import a pipeline from YAML; prints the new id
  pipelines run <id> [--follow]           Trigger a run; --follow polls status to completion
  runs list <pipelineId>                  List runs for a pipeline (newest first)
  runs logs <runId> --pipeline <id> [--stage <stageId>]
                                          Fetch final stage logs for a run

Global flags:
  --server URL    Cooker base URL (default $COOKER_URL or http://localhost:8080)
  --token TOKEN   API token (prefer the COOKER_TOKEN env var; never logged)
  --json          Machine-readable JSON output (list/get commands)

Environment:
  COOKER_URL      Default server base URL
  COOKER_TOKEN    API token (ck_…). Preferred over --token so it never lands in shell history.

Create a token in the UI under Settings → API tokens.
`

// globalConfig is resolved once from flags + env before dispatch.
type globalConfig struct {
	server string
	token  string
	json   bool
}

// run is the testable entry point: it parses argv, dispatches, and
// returns a process exit code. main is a thin wrapper around it. stdout
// and stderr are injected so tests can capture output.
func run(argv []string, stdout, stderr io.Writer) int {
	cfg, rest, err := parseGlobal(argv)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprint(stderr, usage)
		return 2
	}
	if len(rest) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	ctx := context.Background()
	cmd, args := rest[0], rest[1:]

	switch cmd {
	case "version":
		return cmdVersion(ctx, cfg, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "pipelines":
		return dispatchPipelines(ctx, cfg, args, stdout, stderr)
	case "runs":
		return dispatchRuns(ctx, cfg, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// parseGlobal pulls the global flags off the front of argv and returns the
// resolved config plus the remaining (sub)command arguments. Flags may be
// interspersed before the subcommand. The token falls back to COOKER_TOKEN
// and the server to COOKER_URL.
func parseGlobal(argv []string) (globalConfig, []string, error) {
	fs := flag.NewFlagSet("cookerctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render usage ourselves
	server := fs.String("server", "", "Cooker base URL")
	token := fs.String("token", "", "API token (prefer COOKER_TOKEN)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return globalConfig{}, nil, err
	}

	cfg := globalConfig{server: *server, token: *token, json: *asJSON}
	if cfg.server == "" {
		cfg.server = os.Getenv("COOKER_URL")
	}
	if cfg.server == "" {
		cfg.server = "http://localhost:8080"
	}
	if cfg.token == "" {
		cfg.token = os.Getenv("COOKER_TOKEN")
	}
	return cfg, fs.Args(), nil
}

// reportError prints a non-2xx / transport error to stderr and maps it to
// an exit code. A 401 additionally prints the token-creation hint. Returns
// the exit code so callers can `return reportError(...)`.
func reportError(stderr io.Writer, err error) int {
	if errors.Is(err, errUnauthorized) {
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprintln(stderr, "hint: this server rejected the API token. Create one in the UI under Settings → API tokens,")
		fmt.Fprintln(stderr, "      then export it as COOKER_TOKEN (e.g. export COOKER_TOKEN=ck_…).")
		return 1
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// requireToken guards commands that hit authenticated endpoints, failing
// fast with the same hint rather than making a doomed request.
func requireToken(cfg globalConfig, stderr io.Writer) bool {
	if cfg.token == "" {
		fmt.Fprintln(stderr, "error: no API token provided")
		fmt.Fprintln(stderr, "hint: set COOKER_TOKEN (preferred) or pass --token. Create a token in the UI under Settings → API tokens.")
		return false
	}
	return true
}

// --------------------------------------------------------------- version

func cmdVersion(ctx context.Context, cfg globalConfig, stdout, stderr io.Writer) int {
	c := newClient(cfg.server, cfg.token)
	v, err := c.Version(ctx)
	if err != nil {
		// The client version is still useful even if the server is
		// unreachable; print it, then surface the server error.
		fmt.Fprintf(stdout, "Client: %s\n", clientVersion)
		fmt.Fprintf(stderr, "error: could not reach server at %s: %s\n", cfg.server, err)
		return 1
	}
	if cfg.json {
		return printJSON(stdout, stderr, map[string]any{
			"client": clientVersion,
			"server": v,
		})
	}
	fmt.Fprintf(stdout, "Client: %s\n", clientVersion)
	fmt.Fprintf(stdout, "Server: %s (%s, built %s, %s)\n", v.BuildVersion, v.BuildSHA, v.BuildTime, v.GoVersion)
	return 0
}

// ------------------------------------------------------------- pipelines

func dispatchPipelines(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "error: pipelines requires a subcommand: list | export | import | run")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdPipelinesList(ctx, cfg, stdout, stderr)
	case "export":
		return cmdPipelinesExport(ctx, cfg, rest, stdout, stderr)
	case "import":
		return cmdPipelinesImport(ctx, cfg, rest, stdout, stderr)
	case "run":
		return cmdPipelinesRun(ctx, cfg, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown pipelines subcommand %q\n", sub)
		return 2
	}
}

func cmdPipelinesList(ctx context.Context, cfg globalConfig, stdout, stderr io.Writer) int {
	if !requireToken(cfg, stderr) {
		return 1
	}
	c := newClient(cfg.server, cfg.token)
	ps, err := c.ListPipelines(ctx)
	if err != nil {
		return reportError(stderr, err)
	}
	if cfg.json {
		return printJSON(stdout, stderr, ps)
	}
	if len(ps) == 0 {
		fmt.Fprintln(stdout, "No pipelines.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTAGES\tVERSION\tUPDATED")
	for _, p := range ps {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n",
			p.ID, p.Name, len(p.Stages), p.Version, p.UpdatedAt.Format(time.RFC3339))
	}
	return flushErr(tw, stderr)
}

func cmdPipelinesExport(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pipelines export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write YAML to this file instead of stdout")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: cookerctl pipelines export <id> [-o file]")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	id := pos[0]
	c := newClient(cfg.server, cfg.token)
	doc, err := c.ExportPipeline(ctx, id)
	if err != nil {
		return reportError(stderr, err)
	}
	if *out == "" {
		_, _ = stdout.Write(doc)
		return 0
	}
	// 0644: a pipeline document carries no secrets (only secretRefs), so
	// it is safe to write with ordinary file permissions.
	if err := os.WriteFile(*out, doc, 0o644); err != nil {
		fmt.Fprintln(stderr, "error: write file:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Wrote %s (%d bytes)\n", *out, len(doc))
	return 0
}

func cmdPipelinesImport(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pipelines import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "", "path to a pipeline YAML document (- for stdin)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "usage: cookerctl pipelines import -f <file.yaml>")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	var (
		doc []byte
		err error
	)
	if *file == "-" {
		doc, err = io.ReadAll(os.Stdin)
	} else {
		doc, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error: read file:", err)
		return 1
	}
	c := newClient(cfg.server, cfg.token)
	p, err := c.ImportPipeline(ctx, doc)
	if err != nil {
		return reportError(stderr, err)
	}
	if cfg.json {
		return printJSON(stdout, stderr, p)
	}
	fmt.Fprintf(stdout, "Imported pipeline %q\nID: %s\n", p.Name, p.ID)
	return 0
}

func cmdPipelinesRun(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pipelines run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	follow := fs.Bool("follow", false, "poll run status and print stage transitions until the run finishes")
	idemKey := fs.String("idempotency-key", "", "override the generated Idempotency-Key sent with the run trigger")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: cookerctl pipelines run <id> [--follow] [--idempotency-key KEY]")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	pipelineID := pos[0]
	key := *idemKey
	if key == "" {
		// A fresh key per invocation makes a retried `run` (e.g. after a
		// transient network error) safe: the server replays the original
		// 202 instead of spawning a duplicate run.
		key = uuid.NewString()
	}

	c := newClient(cfg.server, cfg.token)
	runRec, err := c.RunPipeline(ctx, pipelineID, runOptions{idempotencyKey: key})
	if err != nil {
		return reportError(stderr, err)
	}
	fmt.Fprintf(stdout, "Run %s started (status: %s)\n", runRec.ID, runRec.Status)

	if !*follow {
		if cfg.json {
			return printJSON(stdout, stderr, runRec)
		}
		fmt.Fprintf(stdout, "Follow with: cookerctl runs logs %s --pipeline %s\n", runRec.ID, pipelineID)
		return 0
	}
	return followRun(ctx, c, cfg, pipelineID, runRec.ID, stdout, stderr)
}

// followRun polls the run-detail endpoint every pollInterval, printing
// each stage's status transition and fetching that stage's logs the
// moment it reaches a terminal state. It returns 0 when the run succeeds
// and 1 when it fails or is cancelled, so the exit code is scriptable.
//
// This is deliberately REST polling, not WebSocket: V1 keeps the
// transport simple (no ticket exchange, no upgrade) at the cost of a few
// seconds of latency.
func followRun(ctx context.Context, c *client, cfg globalConfig, pipelineID, runID string, stdout, stderr io.Writer) int {
	// Stage id → display name, for readable transition lines. A missing
	// pipeline (deleted mid-run) degrades gracefully to bare ids.
	names := map[string]string{}
	if p, err := c.GetPipeline(ctx, pipelineID); err == nil {
		for _, s := range p.Stages {
			names[s.ID] = s.Name
		}
	}
	stageName := func(id string) string {
		if n := names[id]; n != "" {
			return n
		}
		return id
	}

	seen := map[string]model.RunStatus{} // last status printed per stage
	logged := map[string]bool{}          // stages whose logs were fetched

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		run, err := c.GetRun(ctx, pipelineID, runID)
		if err != nil {
			return reportError(stderr, err)
		}

		for _, sr := range run.StageRuns {
			if prev, ok := seen[sr.StageID]; !ok || prev != sr.Status {
				seen[sr.StageID] = sr.Status
				fmt.Fprintf(stdout, "  [%s] %s → %s\n",
					time.Now().Format("15:04:05"), stageName(sr.StageID), sr.Status)
			}
			if isTerminalStage(sr.Status) && !logged[sr.StageID] {
				logged[sr.StageID] = true
				printStageLogs(ctx, c, pipelineID, runID, sr.StageID, stageName(sr.StageID), stdout)
			}
		}

		if isTerminalRun(run.Status) {
			fmt.Fprintf(stdout, "Run %s finished: %s\n", run.ID, run.Status)
			if cfg.json {
				if code := printJSON(stdout, stderr, run); code != 0 {
					return code
				}
			}
			if run.Status == model.RunStatusSuccess {
				return 0
			}
			return 1
		}

		select {
		case <-ctx.Done():
			fmt.Fprintln(stderr, "error:", ctx.Err())
			return 1
		case <-ticker.C:
		}
	}
}

// printStageLogs fetches and prints one stage's final logs, indented under
// a header. A fetch failure is noted but never aborts the follow loop —
// logs are best-effort relative to the run outcome.
func printStageLogs(ctx context.Context, c *client, pipelineID, runID, stageID, name string, stdout io.Writer) {
	logs, err := c.StageLogs(ctx, pipelineID, runID, stageID)
	if err != nil {
		fmt.Fprintf(stdout, "    (logs unavailable for %s: %s)\n", name, err)
		return
	}
	logs = strings.TrimRight(logs, "\n")
	if logs == "" {
		return
	}
	fmt.Fprintf(stdout, "    --- logs: %s ---\n", name)
	for _, line := range strings.Split(logs, "\n") {
		fmt.Fprintf(stdout, "    %s\n", line)
	}
}

// ------------------------------------------------------------------ runs

func dispatchRuns(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "error: runs requires a subcommand: list | logs")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdRunsList(ctx, cfg, rest, stdout, stderr)
	case "logs":
		return cmdRunsLogs(ctx, cfg, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown runs subcommand %q\n", sub)
		return 2
	}
}

func cmdRunsList(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: cookerctl runs list <pipelineId>")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	c := newClient(cfg.server, cfg.token)
	runs, err := c.ListRuns(ctx, args[0])
	if err != nil {
		return reportError(stderr, err)
	}
	if cfg.json {
		return printJSON(stdout, stderr, runs)
	}
	if len(runs) == 0 {
		fmt.Fprintln(stdout, "No runs.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN ID\tSTATUS\tSTAGES\tCREATED")
	for _, r := range runs {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
			r.ID, r.Status, len(r.StageRuns), r.CreatedAt.Format(time.RFC3339))
	}
	return flushErr(tw, stderr)
}

func cmdRunsLogs(ctx context.Context, cfg globalConfig, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runs logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pipelineID := fs.String("pipeline", "", "pipeline id the run belongs to (required)")
	stage := fs.String("stage", "", "limit to a single stage id (default: all stages)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || *pipelineID == "" {
		fmt.Fprintln(stderr, "usage: cookerctl runs logs <runId> --pipeline <id> [--stage <stageId>]")
		return 2
	}
	if !requireToken(cfg, stderr) {
		return 1
	}
	runID := pos[0]
	c := newClient(cfg.server, cfg.token)

	// Resolve the run to know its stages and to build readable names.
	run, err := c.GetRun(ctx, *pipelineID, runID)
	if err != nil {
		return reportError(stderr, err)
	}
	names := map[string]string{}
	if p, perr := c.GetPipeline(ctx, *pipelineID); perr == nil {
		for _, s := range p.Stages {
			names[s.ID] = s.Name
		}
	}

	stageIDs := make([]string, 0, len(run.StageRuns))
	if *stage != "" {
		stageIDs = append(stageIDs, *stage)
	} else {
		for _, sr := range run.StageRuns {
			stageIDs = append(stageIDs, sr.StageID)
		}
	}

	if cfg.json {
		out := make(map[string]string, len(stageIDs))
		for _, sid := range stageIDs {
			logs, lerr := c.StageLogs(ctx, *pipelineID, runID, sid)
			if lerr != nil {
				return reportError(stderr, lerr)
			}
			out[sid] = logs
		}
		return printJSON(stdout, stderr, out)
	}

	for _, sid := range stageIDs {
		logs, lerr := c.StageLogs(ctx, *pipelineID, runID, sid)
		if lerr != nil {
			return reportError(stderr, lerr)
		}
		display := names[sid]
		if display == "" {
			display = sid
		}
		fmt.Fprintf(stdout, "=== %s (%s) ===\n", display, sid)
		if strings.TrimSpace(logs) != "" {
			fmt.Fprintln(stdout, strings.TrimRight(logs, "\n"))
		}
	}
	return 0
}

// --------------------------------------------------------------- helpers

// isTerminalRun reports whether a run-level status will not change again.
func isTerminalRun(s model.RunStatus) bool {
	switch s {
	case model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCancelled:
		return true
	default:
		return false
	}
}

// isTerminalStage reports whether a stage-level status is final. Unlike a
// run, a stage may also be Skipped.
func isTerminalStage(s model.RunStatus) bool {
	switch s {
	case model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCancelled, model.RunStatusSkipped:
		return true
	default:
		return false
	}
}

// printJSON marshals v as indented JSON to stdout, returning an exit code.
func printJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "error: encode json:", err)
		return 1
	}
	return 0
}

// flushErr flushes a tabwriter and maps a flush failure to an exit code.
func flushErr(tw *tabwriter.Writer, stderr io.Writer) int {
	if err := tw.Flush(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// parseInterspersed parses fs allowing flags to appear after positional
// arguments (e.g. `run p1 --follow`), which the stdlib flag package does
// not support — it stops at the first non-flag. We collect leading
// positionals, re-parse the remainder, and repeat. The collected
// positionals are returned in order; on a flag error the underlying
// ContinueOnError surfaces it. This lets the CLI accept the natural
// kubectl-style ordering instead of forcing flags before the id.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		// First remaining arg is a positional (Parse stopped on it).
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}
